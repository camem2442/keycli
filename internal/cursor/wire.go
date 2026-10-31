package cursor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// ApplicationUserKey is the Cursor internal storage key holding AI & model configurations.
	ApplicationUserKey = "src.vs.platform.reactivestorage.browser.reactiveStorageServiceImpl.persistentStorage.applicationUser"

	// DefaultDBRelativePath is the relative path from the user's home directory on macOS.
	DefaultDBRelativePath = "Library/Application Support/Cursor/User/globalStorage/state.vscdb"
)

// WireResult describes the changes planned or applied to Cursor.
type WireResult struct {
	DBPath      string
	PreviousURL string
	NewURL      string
	Updated     bool
	IsRunning   bool
	RunningPID  int
	Restarted   bool
}

// DefaultDBPath returns the standard macOS path to Cursor's global storage SQLite DB.
func DefaultDBPath(home string) string {
	return filepath.Join(home, filepath.FromSlash(DefaultDBRelativePath))
}

// ActiveTunnelBaseURL resolves the active tunnel base URL from ~/.config/cursor-vertex-proxy.
// It checks current_url.txt first, then falls back to LAST_TUNNEL_BASE_URL in config.env.
// It cleans accidental whitespace and ensures the URL ends with /v1.
func ActiveTunnelBaseURL(home string) (string, error) {
	proxyDir := filepath.Join(home, ".config", "cursor-vertex-proxy")
	urlFile := filepath.Join(proxyDir, "current_url.txt")
	configFile := filepath.Join(proxyDir, "config.env")

	var rawURL string

	// 1. Try current_url.txt
	if data, err := os.ReadFile(urlFile); err == nil && len(bytes.TrimSpace(data)) > 0 {
		rawURL = string(data)
	} else {
		// 2. Try config.env
		cfgData, err := os.ReadFile(configFile)
		if err != nil {
			return "", fmt.Errorf("no vertex-byok proxy tunnel found (checked %s and %s): start tunnel with 'cursor-vertex-up' or 'vertex-byok-up'", urlFile, configFile)
		}
		re := regexp.MustCompile(`(?m)^LAST_TUNNEL_BASE_URL=(.+)$`)
		matches := re.FindSubmatch(cfgData)
		if len(matches) < 2 {
			return "", fmt.Errorf("LAST_TUNNEL_BASE_URL not found in %s: start tunnel with 'cursor-vertex-up'", configFile)
		}
		rawURL = string(matches[1])
	}

	cleaned := CleanAndValidateBaseURL(rawURL)
	if cleaned == "" {
		return "", fmt.Errorf("invalid tunnel base URL in vertex-byok proxy config")
	}
	return cleaned, nil
}

// CleanAndValidateBaseURL strips comments, quotes, and removes any internal accidental whitespace.
func CleanAndValidateBaseURL(raw string) string {
	val := strings.TrimSpace(raw)
	val = strings.Trim(val, `"'`)
	if idx := strings.Index(val, "#"); idx != -1 {
		val = strings.TrimSpace(val[:idx])
	}
	// remove all internal whitespace (e.g. accidental line wrap / copy-paste space)
	val = regexp.MustCompile(`\s+`).ReplaceAllString(val, "")
	if val == "" {
		return ""
	}
	if !strings.HasPrefix(val, "http://") && !strings.HasPrefix(val, "https://") {
		return ""
	}
	parsed, err := url.Parse(val)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || strings.ContainsAny(val, "\"\\") {
		return ""
	}
	val = strings.TrimRight(val, "/")
	if !strings.HasSuffix(val, "/v1") {
		val = val + "/v1"
	}
	return val
}

// ReadCursorBaseURL extracts the current openAIBaseUrl from Cursor's state.vscdb using sqlite3.
func ReadCursorBaseURL(dbPath string) (string, error) {
	sqliteBin, err := findSQLiteBin()
	if err != nil {
		return "", err
	}

	escapedKey := strings.ReplaceAll(ApplicationUserKey, "'", "''")
	sql := fmt.Sprintf("SELECT json_extract(value, '$.openAIBaseUrl') FROM ItemTable WHERE key = '%s';", escapedKey)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, sqliteBin, "-readonly", dbPath, sql)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("reading openAIBaseUrl from %s: %w", dbPath, err)
	}

	return strings.TrimSpace(string(out)), nil
}

// IsCursorRunning detects if Cursor desktop is currently executing.
func IsCursorRunning() (bool, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "pgrep", "-x", "Cursor").Output()
	return parseCursorProcess(out, err, ctx.Err())
}

func parseCursorProcess(out []byte, err, contextErr error) (bool, int, error) {
	if contextErr != nil {
		return false, 0, fmt.Errorf("observing Cursor: %w", contextErr)
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && len(bytes.TrimSpace(out)) == 0 {
			return false, 0, nil
		}
		return false, 0, fmt.Errorf("observing Cursor: %w", err)
	}
	lines := strings.Fields(string(out))
	if len(lines) > 0 {
		pid, err := strconv.Atoi(lines[0])
		if err == nil && pid > 0 {
			return true, pid, nil
		}
	}
	return false, 0, fmt.Errorf("observing Cursor: invalid pgrep output")
}

// QuitCursor gracefully asks Cursor desktop to quit.
func QuitCursor(ctx context.Context) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	running, pid, err := IsCursorRunning()
	if err != nil {
		return err
	}
	if !running {
		return nil
	}

	// 1. Try graceful quit via AppleScript
	if err := exec.CommandContext(ctx, "osascript", "-e", `tell application "Cursor" to quit`).Run(); err != nil {
		return fmt.Errorf("requesting Cursor quit: %w", err)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if r, _, err := IsCursorRunning(); err != nil {
			return err
		} else if !r {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}

	// 2. If still alive, send SIGTERM to allow graceful flush
	if r, p, err := IsCursorRunning(); err != nil {
		return err
	} else if r && p > 0 {
		if err := syscall.Kill(p, syscall.SIGTERM); err != nil {
			return fmt.Errorf("stopping Cursor: %w", err)
		}
		deadline = time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) {
			if r, _, err := IsCursorRunning(); err != nil {
				return err
			} else if !r {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
	}

	if r, _, err := IsCursorRunning(); err != nil {
		return err
	} else if r {
		return fmt.Errorf("timed out waiting for Cursor to quit (PID %d still alive)", pid)
	}
	return nil
}

// LaunchCursor opens Cursor desktop application.
func LaunchCursor(ctx context.Context) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return exec.CommandContext(ctx, "open", "-a", "Cursor").Run()
}

// WireCursorVertex inspects or updates Cursor's state.vscdb with the active or provided tunnel URL.
func WireCursorVertex(home, overrideDB, overrideBaseURL string, dryRun, restart bool) (WireResult, error) {
	baseURL := overrideBaseURL
	if baseURL != "" {
		baseURL = CleanAndValidateBaseURL(baseURL)
		if baseURL == "" {
			return WireResult{}, fmt.Errorf("invalid base URL override: must be a valid http(s) URL")
		}
	} else {
		resolved, err := ActiveTunnelBaseURL(home)
		if err != nil {
			return WireResult{}, err
		}
		baseURL = resolved
	}

	dbPath := overrideDB
	if dbPath == "" {
		dbPath = DefaultDBPath(home)
	}

	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return WireResult{}, fmt.Errorf("cursor state database not found at %s (has Cursor been launched?)", dbPath)
		}
		return WireResult{}, fmt.Errorf("accessing cursor database %s: %w", dbPath, err)
	}

	prevURL, err := ReadCursorBaseURL(dbPath)
	if err != nil {
		return WireResult{}, err
	}
	isRunning, runningPID, err := IsCursorRunning()
	if err != nil {
		return WireResult{}, err
	}

	res := WireResult{
		DBPath:      dbPath,
		PreviousURL: prevURL,
		NewURL:      baseURL,
		Updated:     false,
		IsRunning:   isRunning,
		RunningPID:  runningPID,
		Restarted:   false,
	}

	if dryRun {
		return res, nil
	}

	// If restart requested and Cursor is running, gracefully quit before DB write
	if restart && isRunning {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		if err := QuitCursor(ctx); err != nil {
			return res, fmt.Errorf("stopping Cursor before update: %w", err)
		}
	}

	if err := UpdateCursorDB(dbPath, baseURL); err != nil {
		return res, err
	}
	res.Updated = true

	// If restart requested and was running, relaunch Cursor
	if restart && isRunning {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := LaunchCursor(ctx); err != nil {
			return res, fmt.Errorf("restarting Cursor: %w", err)
		}
		res.Restarted = true
	}

	return res, nil
}

// UpdateCursorDB executes an upsert on ItemTable in state.vscdb using sqlite3.
func UpdateCursorDB(dbPath, baseURL string) error {
	sqliteBin, err := findSQLiteBin()
	if err != nil {
		return err
	}

	escapedURL := strings.ReplaceAll(baseURL, "'", "''")
	escapedKey := strings.ReplaceAll(ApplicationUserKey, "'", "''")
	initialBytes, err := json.Marshal(map[string]any{"openAIBaseUrl": baseURL, "useOpenAIKey": true})
	if err != nil {
		return err
	}
	initialJSON := strings.ReplaceAll(string(initialBytes), "'", "''")

	sql := fmt.Sprintf(`INSERT INTO ItemTable (key, value) VALUES ('%s', '%s')
ON CONFLICT(key) DO UPDATE SET value = json_set(ItemTable.value, '$.openAIBaseUrl', '%s', '$.useOpenAIKey', json('true'));
PRAGMA wal_checkpoint(FULL);
`, escapedKey, initialJSON, escapedURL)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, sqliteBin, dbPath)
	cmd.Stdin = strings.NewReader(sql)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sqlite3 update failed on %s: %w (stderr: %s)", dbPath, err, strings.TrimSpace(stderr.String()))
	}

	return nil
}

func findSQLiteBin() (string, error) {
	if bin, err := exec.LookPath("sqlite3"); err == nil {
		return bin, nil
	}
	if _, err := os.Stat("/usr/bin/sqlite3"); err == nil {
		return "/usr/bin/sqlite3", nil
	}
	return "", fmt.Errorf("sqlite3 executable not found in PATH or /usr/bin/sqlite3")
}
