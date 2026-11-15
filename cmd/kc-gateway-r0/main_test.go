package main

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/effectgateway"
)

func TestR0NumericLoopbackOnly(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:18090", "[::1]:18090"} {
		if err := requireLoopback(addr); err != nil {
			t.Fatalf("%s should be local: %v", addr, err)
		}
	}
	for _, addr := range []string{"0.0.0.0:18090", "[::]:18090", "localhost:18090",
		"198.51.100.3:18090", "example.com:18090", "127.0.0.1"} {
		if err := requireLoopback(addr); err == nil {
			t.Fatalf("%s must be rejected", addr)
		}
	}
}

func TestR0StartupOutputPreflight(t *testing.T) {
	for _, mode := range []string{"new", "existing-file", "existing-directory", "dangling-symlink", "invalid-path"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			key := filepath.Join(root, "key")
			want := "new synthetic output already exists"
			switch mode {
			case "new":
				want = "trusted host configuration refused"
			case "existing-file":
				if err := os.WriteFile(key, []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			case "existing-directory":
				if err := os.Mkdir(key, 0700); err != nil {
					t.Fatal(err)
				}
			case "dangling-symlink":
				if err := os.Symlink(filepath.Join(root, "missing"), key); err != nil {
					t.Fatal(err)
				}
			case "invalid-path":
				want = "unobservable output path"
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestR0StartupProcess$")
			cmd.Env = append(os.Environ(), "KC_R0_STARTUP_TEST_ROOT="+root, "KC_R0_STARTUP_TEST_KEY="+key,
				effectgateway.ProviderEffectsEnv+"=")
			if mode == "invalid-path" {
				// Environment entries cannot contain NUL; add it inside the child.
				cmd.Env = append(cmd.Env, "KC_R0_STARTUP_TEST_INVALID=1")
			}
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), want) {
				t.Fatalf("startup error=%v output=%s, want %q", err, out, want)
			}
			_, err = os.Stat(filepath.Join(root, "journal"))
			if mode == "new" && err != nil {
				t.Fatalf("new outputs did not reach host validation: %v", err)
			}
			if mode != "new" && !os.IsNotExist(err) {
				t.Fatalf("rejected output created a journal: %v", err)
			}
		})
	}
}

func TestR0StartupProcess(t *testing.T) {
	root := os.Getenv("KC_R0_STARTUP_TEST_ROOT")
	if root == "" {
		return
	}
	key := os.Getenv("KC_R0_STARTUP_TEST_KEY")
	if os.Getenv("KC_R0_STARTUP_TEST_INVALID") == "1" {
		key += "\x00"
	}
	flag.CommandLine = flag.NewFlagSet("kc-gateway-r0", flag.ExitOnError)
	os.Args = []string{"kc-gateway-r0", "-journal", filepath.Join(root, "journal"),
		"-key-out", key, "-ledger", filepath.Join(root, "ledger"),
		"-claims", filepath.Join(root, "claims.db"), "-state-root", root,
		"-goal", "fixture-goal", "-mutation", "fixture-mutation", "-owner", "fixture-owner", "-session", "fixture-session"}
	main()
	t.Fatal("startup fixture unexpectedly returned")
}
