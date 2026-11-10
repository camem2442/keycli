package cloudflare

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
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
	SecretName    string
	VersionTag    string // optional, host-bound operation correlation; visible metadata only
	Env           []string
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
	notAttempted := VersionStageResult{State: VersionNotAttempted}
	if load == nil || !validVersionStageRequest(req) {
		return notAttempted, errors.New("invalid inactive-version target or environment")
	}
	if err := ctx.Err(); err != nil {
		return notAttempted, err
	}
	secret, err := load(ctx)
	if err != nil {
		return notAttempted, errors.New("cannot resolve inactive-version secret")
	}
	defer clearVersionBytes(secret)
	if len(secret) == 0 || len(secret) > 8192 {
		return notAttempted, errors.New("invalid inactive-version secret length")
	}
	if err := ctx.Err(); err != nil {
		return notAttempted, err
	}
	bounded, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	args := []string{"versions", "secret", "put", req.SecretName, "--name", req.Worker, "--env", req.Environment, "--config", req.ConfigPath}
	if req.VersionTag != "" {
		args = append(args, "--tag", req.VersionTag)
	}
	cmd := exec.CommandContext(bounded, req.WranglerPath, args...)
	cmd.Dir = req.WorkDirectory
	cmd.Env = slices.Clone(req.Env)
	cmd.Stdin = bytes.NewReader(secret)
	cmd.Stdout = io.Discard // Never expose Wrangler output; it may echo a value.
	cmd.Stderr = io.Discard
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

func validVersionStageRequest(r VersionStageRequest) bool {
	if !stageAccount.MatchString(r.AccountID) || !stageWorker.MatchString(r.Worker) ||
		!stageEnv.MatchString(r.Environment) || !stageSecret.MatchString(r.SecretName) ||
		(r.VersionTag != "" && !stageTag.MatchString(r.VersionTag)) ||
		!filepath.IsAbs(r.WranglerPath) || !filepath.IsAbs(r.ConfigPath) || !filepath.IsAbs(r.WorkDirectory) ||
		filepath.Clean(r.WranglerPath) != r.WranglerPath || filepath.Clean(r.ConfigPath) != r.ConfigPath ||
		filepath.Clean(r.WorkDirectory) != r.WorkDirectory {
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

func clearVersionBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
