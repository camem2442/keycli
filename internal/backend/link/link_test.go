package link

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/config"
)

func TestLinkKeychainRead(t *testing.T) {
	root := moduleRoot(t)
	fakeSec := filepath.Join(root, "cmd", "fakesecurity")
	bin := t.TempDir()
	out := filepath.Join(bin, "fakesecurity")
	b, err := exec.Command("go", "build", "-o", out, fakeSec).CombinedOutput()
	if err != nil {
		t.Fatal(string(b))
	}
	state := filepath.Join(t.TempDir(), "state.json")
	_ = os.WriteFile(state, []byte(`{"entries":{"linked-svc\tacct":"LINKVAL"}}`), 0o600)

	cfg := config.Config{
		Link: map[string]config.LinkTarget{
			"mine": {Type: "keychain", Service: "linked-svc", Account: "acct"},
		},
	}
	lb := &Link{
		instance: "link",
		links:    cfg.Link,
		runner:   fakeRunner{bin: out, state: state},
		cfg:      cfg,
	}
	val, err := lb.Get(context.Background(), "mine")
	if err != nil || string(val) != "LINKVAL" {
		t.Fatalf("%q %v", val, err)
	}
	src := lb.SourceFor("mine")
	if !strings.HasPrefix(src, "link:keychain:") {
		t.Fatalf("source %q", src)
	}
}

func moduleRoot(t *testing.T) string {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

type fakeRunner struct {
	bin   string
	state string
}

func (f fakeRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, f.bin, args...)
	cmd.Env = append(os.Environ(), "KC_FAKE_SECURITY_STATE="+f.state)
	return cmd.Output()
}

func (f fakeRunner) RunWithStdin(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	return f.Run(ctx, args...)
}
