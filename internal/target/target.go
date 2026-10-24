package target

import "context"

// Secret is a name/value pair for push targets (M4+).
type Secret struct {
	Name  string
	Value []byte
}

// Capabilities describes a push target.
type Capabilities struct {
	NeedsNetwork bool
	CanList      bool
}

// Target pushes secrets to remote platforms (network allowed only here).
type Target interface {
	Name() string
	Capabilities() Capabilities
	Push(ctx context.Context, secrets []Secret) error
	List(ctx context.Context) ([]string, error)
}

// Registry holds named targets (populated in later milestones).
var Registry = map[string]Target{}
