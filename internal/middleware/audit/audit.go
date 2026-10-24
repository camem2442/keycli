package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/kcerrors"
)

const defaultMaxBytes = 5 * 1024 * 1024

// Record is one append-only audit line (never contains secret values).
type Record struct {
	TS      string `json:"ts"`
	Command string `json:"command"`
	Name    string `json:"name"`
	Source  string `json:"source,omitempty"`
	Outcome string `json:"outcome"`
	Agent   string `json:"agent"`
	PID     int    `json:"pid"`
	PPID    int    `json:"ppid"`
	CWD     string `json:"cwd"`
}

// Writer appends JSONL audit records.
type Writer struct {
	path     string
	maxBytes int64
	required bool
	enabled  bool
	mu       sync.Mutex
	warn     func(string)
}

// NewWriter builds an audit writer from config.
func NewWriter(home string, cfg config.Config, warn func(string)) *Writer {
	path := cfg.Audit.Path
	if path == "" {
		path = filepath.Join(home, ".kc", "audit.log")
	} else {
		path = expandHome(path, home)
	}
	max := cfg.Audit.MaxBytes
	if max <= 0 {
		max = defaultMaxBytes
	}
	enabled := true
	if cfg.Audit.Enabled != nil {
		enabled = *cfg.Audit.Enabled
	}
	return &Writer{
		path:     path,
		maxBytes: max,
		required: cfg.Audit.Required,
		enabled:  enabled,
		warn:     warn,
	}
}

// OutcomeFor maps lookup errors to audit outcome labels.
func OutcomeFor(err error) string {
	if err == nil {
		return "ok"
	}
	switch kcerrors.Code(err) {
	case kcerrors.ExitNotFound:
		return "notfound"
	case kcerrors.ExitDenied:
		return "denied"
	default:
		return "error"
	}
}

// Append writes one record (exported for tests).
func (w *Writer) Append(rec Record) error {
	if !w.enabled {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(w.path), 0o700); err != nil {
		return err
	}
	if err := w.rotateIfNeeded(); err != nil {
		return err
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_ = os.Chmod(w.path, 0o600)
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

// Path returns the log file path.
func (w *Writer) Path() string {
	return w.path
}

// Enabled reports whether audit logging is active.
func (w *Writer) Enabled() bool {
	return w.enabled
}

// Required reports whether failed writes block successful reads.
func (w *Writer) Required() bool {
	return w.required
}

// WarnFunc returns the stderr warning callback.
func (w *Writer) WarnFunc() func(string) {
	return w.warn
}

// NowTS returns an RFC3339Nano UTC timestamp.
func NowTS() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// CWD returns the process working directory.
func CWD() string {
	return cwd()
}

func (w *Writer) rotateIfNeeded() error {
	info, err := os.Stat(w.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Size() < w.maxBytes {
		return nil
	}
	rotated := w.path + ".1"
	_ = os.Remove(rotated)
	if err := os.Rename(w.path, rotated); err != nil {
		return err
	}
	return nil
}

func cwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}

func expandHome(p, home string) string {
	if len(p) >= 2 && p[:2] == "~/" {
		return filepath.Join(home, p[2:])
	}
	if p == "~" {
		return home
	}
	return p
}
