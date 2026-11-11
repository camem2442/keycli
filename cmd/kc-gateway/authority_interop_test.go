//go:build darwin || linux

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/hostbridge"
)

// Set KEYCLI_TEST_WORKSPACE_INFRA to a workspace-infra checkout containing
// the preview methods and their Python fixture, then run with -count=1.
func TestGatewayPythonAuthorityPreviews(t *testing.T) {
	infra := os.Getenv("KEYCLI_TEST_WORKSPACE_INFRA")
	if infra == "" {
		t.Skip("set KEYCLI_TEST_WORKSPACE_INFRA to the workspace-infra preview checkout")
	}
	infra, err := filepath.Abs(infra)
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("testdata/authority_preview.py")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		note string
	}{
		{"unchanged-head", "EXECUTION_FENCE_UNAVAILABLE"},
		{"changed-head", "EXECUTION_FENCE_REJECTED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := gatewayTempDir(t)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "python3", "-B", script, dir, tc.name)
			cmd.Dir = infra
			cmd.Env = append(os.Environ(), "PYTHONPATH="+infra+string(os.PathListSeparator)+filepath.Join(infra, "tests"), "TMPDIR="+dir)
			cmd.Stderr = os.Stderr
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if cmd.ProcessState == nil {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			}()
			var ready struct {
				DB      string                      `json:"db"`
				Config  string                      `json:"config"`
				Binding hostbridge.AuthorityBinding `json:"binding"`
			}
			if err := json.NewDecoder(stdout).Decode(&ready); err != nil {
				t.Fatalf("Python Authority did not become ready: %v", err)
			}
			runGatewaySubmit(t, dir, ready.DB, ready.Config, ready.Binding, tc.note, nil)
			if err := cmd.Wait(); err != nil {
				t.Fatalf("Python Authority fixture: %v", err)
			}
		})
	}
}
