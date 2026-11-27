package cloudflare

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// VersionStageRequest is a host-owned, exact-target request. This low-level
// adapter does not authenticate an operator or authorize a Worker mutation.
// A trusted host must bind an authorized operation and journal its dispatch
// before invoking StageVersionSecret.
type VersionStageRequest struct {
	WranglerPath  string
	ConfigPath    string
	WorkDirectory string
	AccountID     string
	Worker        string
	Environment   string
	// UseDefaultEnvironment is an explicit opt-in for Worker configs with no
	// named [env.*]. It verifies the named account and Worker, then keeps those
	// validated bytes in a host-owned read-only config through process start.
	UseDefaultEnvironment bool
	SecretName            string
	VersionTag            string // optional, host-bound operation correlation; visible metadata only
	Env                   []string
}

type VersionStageState string

const (
	VersionNotAttempted           VersionStageState = "NOT_ATTEMPTED"
	VersionUnknownEffect          VersionStageState = "UNKNOWN_EFFECT"
	VersionAcknowledgedUnverified VersionStageState = "ACKNOWLEDGED_UNVERIFIED"
)

type VersionStageResult struct{ State VersionStageState }

var (
	stageWorker     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	stageEnv        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	stageSecret     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	stageTag        = regexp.MustCompile(`^kc-[A-Za-z0-9_-]{1,96}$`)
	stageAccount    = regexp.MustCompile(`^[a-f0-9]{32}$`)
	stageDigest     = regexp.MustCompile(`^[a-f0-9]{64}$`)
	allowedStageEnv = map[string]bool{
		"PATH": true, "HOME": true, "TMPDIR": true, "XDG_CONFIG_HOME": true,
		"CLOUDFLARE_API_TOKEN": true, "CLOUDFLARE_ACCOUNT_ID": true, "CI": true,
		"NODE_EXTRA_CA_CERTS": true, "SSL_CERT_FILE": true,
	}
)

// StageVersionSecret attempts exactly one `wrangler versions secret put`.
// It does not deploy, read current deployments, create a proof of secret-value
// equality, or implement cross-host locking. No ambient process environment is
// inherited. The host owns provider observation and promotion admission.
// load returns a uniquely owned buffer; this adapter clears it on return.
func StageVersionSecret(ctx context.Context, req VersionStageRequest, load func(context.Context) ([]byte, error)) (VersionStageResult, error) {
	return stageVersionSecret(ctx, req, "", load, nil)
}

// StagePinnedVersionSecret verifies the host-pinned Wrangler SHA-256 before
// resolving the secret and again before calling beforeStart. The mandatory
// hook is the last host callback before process start. The caller must keep the
// executable path and its ancestors under trusted control through launch.
func StagePinnedVersionSecret(ctx context.Context, req VersionStageRequest, wranglerSHA256 string, load func(context.Context) ([]byte, error), beforeStart func(context.Context) error) (VersionStageResult, error) {
	if !stageDigest.MatchString(wranglerSHA256) || beforeStart == nil {
		return VersionStageResult{State: VersionNotAttempted}, errors.New("inactive-version command requires an executable pin and start guard")
	}
	return stageVersionSecret(ctx, req, wranglerSHA256, load, beforeStart)
}

func stageVersionSecret(ctx context.Context, req VersionStageRequest, wranglerSHA256 string, load func(context.Context) ([]byte, error), beforeStart func(context.Context) error) (VersionStageResult, error) {
	notAttempted := VersionStageResult{State: VersionNotAttempted}
	req.Env = slices.Clone(req.Env)
	if load == nil || !validVersionStageRequest(req) {
		return notAttempted, errors.New("invalid inactive-version target or environment")
	}
	bounded, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err := bounded.Err(); err != nil {
		return notAttempted, err
	}
	var frozen frozenStageConfig
	if req.UseDefaultEnvironment {
		var freezeErr error
		frozen, freezeErr = freezeDefaultStageConfig(req.ConfigPath, req.AccountID, req.Worker)
		if freezeErr != nil {
			return notAttempted, errors.New("invalid inactive-version target or environment")
		}
		defer frozen.remove()
	}
	if wranglerSHA256 != "" && !matchesStageExecutable(bounded, req.WranglerPath, wranglerSHA256) {
		return notAttempted, errors.New("inactive-version executable does not match its pin")
	}
	secret, err := load(bounded)
	defer clearVersionBytes(secret)
	if err != nil {
		return notAttempted, errors.New("cannot resolve inactive-version secret")
	}
	if len(secret) == 0 || len(secret) > 8192 {
		return notAttempted, errors.New("invalid inactive-version secret length")
	}
	if err := bounded.Err(); err != nil {
		return notAttempted, err
	}
	if req.UseDefaultEnvironment && !frozen.unchanged() {
		return notAttempted, errors.New("inactive-version config changed after validation")
	}
	if beforeStart != nil {
		if !matchesStageExecutable(bounded, req.WranglerPath, wranglerSHA256) {
			return notAttempted, errors.New("inactive-version executable does not match its pin")
		}
		if err := beforeStart(bounded); err != nil {
			return notAttempted, errors.New("inactive-version command start was denied")
		}
	}
	configPath := req.ConfigPath
	if req.UseDefaultEnvironment {
		configPath = frozen.path
	}
	args := []string{"versions", "secret", "put", req.SecretName, "--name", req.Worker}
	if !req.UseDefaultEnvironment {
		args = append(args, "--env", req.Environment)
	}
	args = append(args, "--config", configPath)
	if req.VersionTag != "" {
		args = append(args, "--tag", req.VersionTag)
	}
	cmd := exec.CommandContext(bounded, req.WranglerPath, args...)
	cmd.Dir = req.WorkDirectory
	cmd.Env = req.Env
	cmd.Stdin = bytes.NewReader(secret)
	cmd.Stdout = io.Discard // Never expose Wrangler output; it may echo a value.
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if req.UseDefaultEnvironment && !frozen.unchanged() {
		return notAttempted, errors.New("inactive-version config changed after validation")
	}
	if err := cmd.Start(); err != nil {
		return notAttempted, errors.New("could not start inactive-version command")
	}
	if err := cmd.Wait(); err != nil {
		// The provider may have accepted a write before the process failed.
		return VersionStageResult{State: VersionUnknownEffect}, errors.New("inactive-version effect is unknown; reconcile before retry")
	}
	// A successful process exit is not proof of version identity, unchanged
	// active traffic, or equivalence of the stored secret value.
	return VersionStageResult{State: VersionAcknowledgedUnverified}, nil
}

func matchesStageExecutable(ctx context.Context, path, digest string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return false
	}
	hash := sha256.New()
	var buffer [32 * 1024]byte
	for ctx.Err() == nil {
		n, err := file.Read(buffer[:])
		_, _ = hash.Write(buffer[:n])
		if err == io.EOF {
			current, statErr := os.Lstat(path)
			return statErr == nil && os.SameFile(opened, current) && hex.EncodeToString(hash.Sum(nil)) == digest && ctx.Err() == nil
		}
		if err != nil {
			return false
		}
	}
	return false
}

func validVersionStageRequest(r VersionStageRequest) bool {
	if !stageAccount.MatchString(r.AccountID) || !stageWorker.MatchString(r.Worker) ||
		!stageSecret.MatchString(r.SecretName) ||
		(r.VersionTag != "" && !stageTag.MatchString(r.VersionTag)) ||
		!filepath.IsAbs(r.WranglerPath) || !filepath.IsAbs(r.ConfigPath) || !filepath.IsAbs(r.WorkDirectory) ||
		filepath.Clean(r.WranglerPath) != r.WranglerPath || filepath.Clean(r.ConfigPath) != r.ConfigPath ||
		filepath.Clean(r.WorkDirectory) != r.WorkDirectory {
		return false
	}
	if r.UseDefaultEnvironment {
		if r.Environment != "" {
			return false
		}
	} else if !stageEnv.MatchString(r.Environment) {
		return false
	}
	for _, p := range []string{r.WranglerPath, r.ConfigPath} {
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() {
			return false
		} // Reject symlinks.
	}
	if info, err := os.Stat(r.WorkDirectory); err != nil || !info.IsDir() {
		return false
	}
	seen := map[string]bool{}
	var account, token string
	for _, item := range r.Env {
		key, value, ok := strings.Cut(item, "=")
		if !ok || !allowedStageEnv[key] || seen[key] || strings.IndexByte(value, 0) >= 0 || strings.ContainsAny(value, "\r\n") {
			return false
		}
		seen[key] = true
		if key == "CLOUDFLARE_ACCOUNT_ID" {
			account = value
		}
		if key == "CLOUDFLARE_API_TOKEN" {
			token = value
		}
	}
	return account == r.AccountID && token != ""
}

// frozenStageConfig is the default-environment config after its bytes have been
// validated. path is a host-owned mode 0400 copy in the source directory, so
// Wrangler does not read the caller path again. Relative config paths stay
// anchored to that directory.
type frozenStageConfig struct {
	source string
	path   string
	bytes  []byte
}

func (f frozenStageConfig) remove() {
	if f.path != "" {
		_ = os.Remove(f.path)
	}
}

func (f frozenStageConfig) unchanged() bool {
	if f.path == "" || len(f.bytes) == 0 {
		return false
	}
	source, ok := readStableRegular(f.source, 16*1024)
	if !ok || !bytes.Equal(source, f.bytes) {
		return false
	}
	copyBytes, ok := readStableRegular(f.path, 16*1024)
	if !ok || !bytes.Equal(copyBytes, f.bytes) {
		return false
	}
	info, err := os.Lstat(f.path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm() == 0400
}

// freezeDefaultStageConfig accepts a default-env operation only when the exact
// Worker and account are pinned by a bounded regular file with no named
// [env.*]. The validated bytes are copied before secret lookup. Existing
// environment-scoped stage requests are not reinterpreted.
func freezeDefaultStageConfig(path, account, worker string) (frozenStageConfig, error) {
	data, ok := readStableRegular(path, 16*1024)
	if !ok || !defaultConfigBytesMatch(data, account, worker) {
		return frozenStageConfig{}, errors.New("invalid default-environment config")
	}
	pinned := bytes.Clone(data)
	file, err := os.CreateTemp(filepath.Dir(path), "kc-stage-config-*.toml")
	if err != nil {
		return frozenStageConfig{}, err
	}
	frozen := frozenStageConfig{source: path, path: file.Name(), bytes: pinned}
	cleanup := true
	defer func() {
		if cleanup {
			_ = file.Close()
			frozen.remove()
		}
	}()
	if _, err := file.Write(pinned); err != nil {
		return frozenStageConfig{}, err
	}
	if err := file.Sync(); err != nil {
		return frozenStageConfig{}, err
	}
	if err := file.Chmod(0400); err != nil {
		return frozenStageConfig{}, err
	}
	if err := file.Close(); err != nil {
		return frozenStageConfig{}, err
	}
	if !frozen.unchanged() {
		return frozenStageConfig{}, errors.New("invalid default-environment config")
	}
	cleanup = false
	return frozen, nil
}

func defaultConfigBytesMatch(input []byte, account, worker string) bool {
	var cfg struct {
		Name      string                 `toml:"name"`
		AccountID string                 `toml:"account_id"`
		Env       map[string]interface{} `toml:"env"`
	}
	if _, err := toml.Decode(string(input), &cfg); err != nil {
		return false
	}
	return cfg.Name == worker && cfg.AccountID == account && len(cfg.Env) == 0
}

func readStableRegular(path string, max int64) ([]byte, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > max {
		return nil, false
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || opened.Size() != info.Size() {
		return nil, false
	}
	data := make([]byte, opened.Size())
	n, err := io.ReadFull(file, data)
	if err != nil || int64(n) != opened.Size() {
		return nil, false
	}
	var extra [1]byte
	if _, err := file.Read(extra[:]); err != io.EOF {
		return nil, false
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(opened, current) || current.Size() != int64(len(data)) {
		return nil, false
	}
	return data, true
}

func clearVersionBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
