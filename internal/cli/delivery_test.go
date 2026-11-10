package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/delivery"
)

func TestDeliveryPlanNeverOpensApp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KC_CONFIG", filepath.Join(home, "config.toml"))
	t.Setenv("KC_AGENT", "ci")
	path := filepath.Join(home, "delivery.toml")
	if err := os.WriteFile(path, []byte(`schema_version = 1
[profiles.fixture]
provider = "cloudflare-workers"
account_alias = "dev"
worker = "fixture-worker"
environment = "staging"
source = "fixture_ref"
destination = "DEST"
effect = "inactive-version"
`), 0600); err != nil {
		t.Fatal(err)
	}
	invalid := filepath.Join(home, "invalid.toml")
	const marker = "do-not-reflect-this-input"
	if err := os.WriteFile(invalid, []byte("schema_version = "+marker), 0600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		args []string
		code int
	}{
		{"plan", []string{"plan", "--profile", "fixture", "--config", path, "--json"}, 0},
		{"help", []string{"plan", "--help"}, 0},
		{"group help", []string{"--help"}, 0},
		{"empty group", nil, 0},
		{"unknown subcommand", []string{"stage"}, 64},
		{"missing profile", []string{"plan"}, 64},
		{"unknown profile", []string{"plan", "--profile", "other", "--config", path}, 64},
		{"wrong flag", []string{"plan", "--remote-read"}, 64},
		{"malformed config", []string{"plan", "--profile", "fixture", "--config", invalid}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := newRoot(func(context.Context) (*app.App, error) {
				t.Fatal("delivery command initialized the application")
				return nil, nil
			})
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs(append([]string{"delivery"}, tc.args...))
			if code := runRoot(root); code != tc.code {
				t.Fatalf("exit %d, want %d: %s", code, tc.code, stderr.String())
			}
			if strings.Contains(stdout.String()+stderr.String(), marker) {
				t.Fatal("malformed input reflected in output")
			}
			if tc.name == "plan" {
				var plan delivery.DeliveryPlan
				if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil || plan.Target.Worker != "fixture-worker" || plan.AuthorizesExecution {
					t.Fatalf("unexpected advisory plan: %v", err)
				}
			}
		})
	}
	if _, err := os.Stat(filepath.Join(home, ".kc")); !os.IsNotExist(err) {
		t.Fatalf("delivery touched the KeyCLI store: %v", err)
	}
}

func TestDeliveryDiscoveryNeverOpensApp(t *testing.T) {
	for _, args := range [][]string{
		{"help", "delivery", "plan"},
		{"__complete", "delivery", "plan", "--p"},
	} {
		root := newRoot(func(context.Context) (*app.App, error) {
			t.Fatal("command discovery initialized the application")
			return nil, nil
		})
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.SetArgs(args)
		if code := runRoot(root); code != 0 {
			t.Fatalf("%v exited %d", args, code)
		}
	}
}

func TestReadPrivateDeliveryInputRejectsPublicAndSpecialFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "delivery.toml")
	if err := os.WriteFile(path, []byte("schema_version = 1"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateDeliveryInput(path, "delivery configuration"); err == nil {
		t.Fatal("delivery config readable by other users accepted")
	}
	if _, err := readPrivateDeliveryInput("/dev/null", "delivery configuration"); err == nil {
		t.Fatal("special file accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, 1<<20+1); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateDeliveryInput(path, "delivery configuration"); err == nil {
		t.Fatal("oversized delivery config accepted")
	}
}
