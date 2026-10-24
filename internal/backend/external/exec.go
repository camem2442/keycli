package external

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/kcerrors"
)

// Runner executes a plugin binary (injectable in tests).
type Runner interface {
	Run(ctx context.Context, bin string, req Request) (Response, error)
}

// ExecRunner runs one JSON request per subprocess.
type ExecRunner struct {
	Timeout time.Duration
}

func (e ExecRunner) Run(ctx context.Context, bin string, req Request) (Response, error) {
	if req.V == 0 {
		req.V = ProtocolVersion
	}
	body, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")} // no KC_* or secrets in env
	cmd.Stdin = bytes.NewReader(append(body, '\n'))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return Response{}, fmt.Errorf("plugin %s: %w", basename(bin), kcerrors.ErrPluginTimeout)
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return Response{}, fmt.Errorf("plugin %s: %s", basename(bin), msg)
	}
	line := strings.TrimSpace(stdout.String())
	if line == "" {
		return Response{}, fmt.Errorf("plugin %s: empty response", basename(bin))
	}
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	var resp Response
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return Response{}, fmt.Errorf("plugin %s: invalid JSON response: %w", basename(bin), err)
	}
	if resp.V != 0 && resp.V != ProtocolVersion {
		return Response{}, fmt.Errorf("plugin %s: unsupported protocol version %d", basename(bin), resp.V)
	}
	return resp, nil
}

func basename(p string) string {
	p = strings.TrimSuffix(p, string(os.PathSeparator))
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// MapErrno converts plugin errno to Go errors.
func MapErrno(errno string, msg string) error {
	switch errno {
	case "not_found":
		return fmt.Errorf("%w: %s", kcerrors.ErrNotFound, msg)
	case "blocked":
		return kcerrors.ErrBlocked
	case "locked":
		return kcerrors.ErrLocked
	case "denied":
		return kcerrors.ErrDenied
	default:
		if msg == "" {
			msg = errno
		}
		if msg == "" {
			msg = "plugin error"
		}
		return fmt.Errorf("%s", msg)
	}
}

// CheckResponse returns an error when ok is false.
func CheckResponse(resp Response) error {
	if resp.OK {
		return nil
	}
	return MapErrno(resp.Errno, resp.Err)
}

// DecodeValue reads value_base64 from a successful get response.
func DecodeValue(resp Response) ([]byte, error) {
	if err := CheckResponse(resp); err != nil {
		return nil, err
	}
	if resp.ValueBase64 == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(resp.ValueBase64)
}
