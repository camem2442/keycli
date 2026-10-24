package middleware

import (
	"context"
	"fmt"
	"os"

	"github.com/camem2442/keycli/internal/middleware/audit"
)

func auditMiddleware(w *audit.Writer, agent string) Middleware {
	if agent == "" {
		agent = DetectAgent()
	}
	return func(next Lookup) Lookup {
		return func(ctx context.Context, name string) ([]byte, string, error) {
			if w == nil || !w.Enabled() {
				return next(ctx, name)
			}
			val, src, err := next(ctx, name)
			rec := audit.Record{
				TS:      audit.NowTS(),
				Command: CommandFrom(ctx),
				Name:    name,
				Source:  src,
				Outcome: audit.OutcomeFor(err),
				Agent:   agent,
				PID:     os.Getpid(),
				PPID:    os.Getppid(),
				CWD:     audit.CWD(),
			}
			if writeErr := w.Append(rec); writeErr != nil {
				msg := fmt.Sprintf("kc: audit log write failed: %v", writeErr)
				if fn := w.WarnFunc(); fn != nil {
					fn(msg)
				}
				if w.Required() && err == nil {
					return nil, "", writeErr
				}
			}
			return val, src, err
		}
	}
}
