package cloudflare_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/target"
	"github.com/camem2442/keycli/internal/target/cloudflare"
)

func fakeWrangler(t *testing.T, body string) (script, argvLog, stdinLog string) {
	t.Helper()
	dir := t.TempDir()
	argvLog = filepath.Join(dir, "argv.log")
	stdinLog = filepath.Join(dir, "stdin.log")
	script = filepath.Join(dir, "fake-wrangler.sh")
	content := "#!/bin/sh\necho \"$@\" >> \"" + argvLog + "\"\ncat > \"" + stdinLog + "\"\n" + body
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return
}

func TestPushStdinNotArgv(t *testing.T) {
	script, argvLog, stdinLog := fakeWrangler(t, "")
	err := cloudflare.Push(context.Background(), cloudflare.Options{
		WranglerBin: script, Worker: "my-worker", Env: "staging", Config: "wrangler.toml",
	}, config.Config{}, []target.Secret{{Name: "MY_TOKEN", Value: []byte("super-secret")}})
	if err != nil {
		t.Fatal(err)
	}
	argv, _ := os.ReadFile(argvLog)
	if strings.Contains(string(argv), "super-secret") {
		t.Fatalf("secret leaked to argv: %s", argv)
	}
	want := "secret put MY_TOKEN --name my-worker --env staging --config wrangler.toml"
	if got := strings.TrimSpace(string(argv)); got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
	in, _ := os.ReadFile(stdinLog)
	if string(in) != "super-secret" {
		t.Fatalf("stdin = %q, want raw value without newline", in)
	}
}

func TestPushRequiresWorkerOrConfig(t *testing.T) {
	err := cloudflare.Push(context.Background(), cloudflare.Options{WranglerBin: "/bin/false"}, config.Config{},
		[]target.Secret{{Name: "X", Value: []byte("v")}})
	if err == nil {
		t.Fatal("expected error without worker/config")
	}
}

func TestDryRunDoesNotExec(t *testing.T) {
	script, argvLog, _ := fakeWrangler(t, "")
	err := cloudflare.Push(context.Background(), cloudflare.Options{WranglerBin: script, Worker: "w", DryRun: true},
		config.Config{}, []target.Secret{{Name: "X", Value: []byte("v")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(argvLog); err == nil {
		t.Fatal("wrangler executed during dry-run")
	}
}

func TestFailureScrubsSecret(t *testing.T) {
	script, _, _ := fakeWrangler(t, "echo \"bad value super-secret\" >&2\nexit 1\n")
	err := cloudflare.Push(context.Background(), cloudflare.Options{WranglerBin: script, Worker: "w"},
		config.Config{}, []target.Secret{{Name: "X", Value: []byte("super-secret")}})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("secret leaked in error: %v", err)
	}
}

func TestFailureScrubsSecretWithWhitespaceBoundary(t *testing.T) {
	for _, val := range []string{"fixture-value ", " fixture-value", "fixture-value\n", "\nfixture-value\n"} {
		// echo stdin back to stderr verbatim, then fail
		script, _, _ := fakeWrangler(t, "")
		body := "#!/bin/sh\ncat >&2\nprintf '\\n' >&2\nexit 1\n"
		if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		err := cloudflare.Push(context.Background(), cloudflare.Options{WranglerBin: script, Worker: "w"},
			config.Config{}, []target.Secret{{Name: "X", Value: []byte(val)}})
		if err == nil {
			t.Fatal("expected error")
		}
		if strings.Contains(err.Error(), "fixture-value") {
			t.Fatalf("secret leaked for value %q: %v", val, err)
		}
	}
}
