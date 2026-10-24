package middleware

import "context"

type ctxKey int

const (
	keyCommand ctxKey = 1
)

// WithCommand annotates a secret read with the invoking kc subcommand (e.g. "get", "exec").
func WithCommand(ctx context.Context, command string) context.Context {
	if command == "" {
		return ctx
	}
	return context.WithValue(ctx, keyCommand, command)
}

// CommandFrom returns the subcommand name if set.
func CommandFrom(ctx context.Context) string {
	if v, ok := ctx.Value(keyCommand).(string); ok {
		return v
	}
	return ""
}
