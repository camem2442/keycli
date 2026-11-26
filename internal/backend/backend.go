package backend

import (
	"context"
	"time"
)

// Capabilities describes backend behavior.
type Capabilities struct {
	CanWrite     bool
	CanList      bool
	Interactive  bool
	NeedsNetwork bool
}

// Item is list metadata without a secret value.
type Item struct {
	Name     string
	Note     string
	Expires  *time.Time
	Kind     string
	URL      string
	Username string
}

// Meta accompanies Set operations.
//
// Only the file store persists Kind, URL and Username; backends that cannot
// store metadata (keychain, env, link) ignore them.
type Meta struct {
	Note     string
	Expires  *time.Time
	Kind     string
	URL      string
	Username string
}

// Backend is a named secret store instance.
type Backend interface {
	InstanceName() string
	Kind() string
	Caps() Capabilities
	Available(ctx context.Context) (bool, string, error)
	Get(ctx context.Context, name string) ([]byte, error)
	List(ctx context.Context) ([]Item, error)
	Set(ctx context.Context, name string, value []byte, meta Meta) error
	Delete(ctx context.Context, name string) error
}

// Factory builds a backend from config.
type Factory func(ctx context.Context, instanceName string, spec any) (Backend, error)
