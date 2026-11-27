package cloudflare

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func defaultStageFixture(t *testing.T) (VersionStageRequest, string, string) {
	t.Helper()
	req, argvPath, inputPath := testStage(t, "")
	req.UseDefaultEnvironment = true
	req.Environment = ""
	config := fmt.Sprintf("name = '%s'\naccount_id = '%s'\nmain = '../../src/gateway.js'\n", req.Worker, req.AccountID)
	if err := os.WriteFile(req.ConfigPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	return req, argvPath, inputPath
}

func TestDefaultEnvironmentStageUsesExactPinnedWorkerWithoutEnvFlag(t *testing.T) {
	req, argv, input := defaultStageFixture(t)
	seen := filepath.Join(req.WorkDirectory, "seen-config")
	modePath := filepath.Join(req.WorkDirectory, "seen-mode")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > '" + argv + "'\ncat > '" + input + "'\nprev=\nfor arg in \"$@\"; do\n  if [ \"$prev\" = \"--config\" ]; then\n    cp \"$arg\" '" + seen + "'\n    if stat -f %Sp \"$arg\" >/dev/null 2>&1; then stat -f %Sp \"$arg\"; else stat -c %A \"$arg\"; fi > '" + modePath + "'\n  fi\n  prev=$arg\ndone\n"
	if err := os.WriteFile(req.WranglerPath, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	secret := []byte("synthetic-fixture-secret")
	original, err := os.ReadFile(req.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	state, err := StageVersionSecret(context.Background(), req,
		func(context.Context) ([]byte, error) { return secret, nil })
	if err != nil || state.State != VersionAcknowledgedUnverified {
		t.Fatalf("state=%s err=%v", state.State, err)
	}
	out, err := os.ReadFile(argv)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "versions secret put " + req.SecretName + " --name " + req.Worker + " --config "
	gotArgs := strings.TrimSpace(string(out))
	if !strings.HasPrefix(gotArgs, prefix) || strings.Contains(gotArgs, " --env ") {
		t.Fatalf("unexpected CLI args: %q", gotArgs)
	}
	configArg := strings.TrimPrefix(gotArgs, prefix)
	if configArg == req.ConfigPath || filepath.Dir(configArg) != filepath.Dir(req.ConfigPath) {
		t.Fatalf("child config = %q", configArg)
	}
	seenBytes, err := os.ReadFile(seen)
	if err != nil || !bytes.Equal(seenBytes, original) {
		t.Fatal("child did not read the validated config bytes")
	}
	mode, err := os.ReadFile(modePath)
	if err != nil || strings.TrimSpace(string(mode)) != "-r--------" {
		t.Fatalf("host config mode = %q err=%v", mode, err)
	}
	if _, err := os.Lstat(configArg); !os.IsNotExist(err) {
		t.Fatal("host config snapshot remained after the process exited")
	}
	got, err := os.ReadFile(input)
	if err != nil || string(got) != "synthetic-fixture-secret" {
		t.Fatal("secret stdin did not match synthetic fixture")
	}
	if !bytes.Equal(secret, make([]byte, len(secret))) {
		t.Fatal("caller-owned secret buffer not cleared")
	}
}

func TestDefaultEnvironmentCannotStageWithConfigOrEnvironmentDrift(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*VersionStageRequest)
		config string
	}{
		{name: "wrong-worker-name", config: "name = 'different-worker'\naccount_id = '%s'\n"},
		{name: "wrong-account", config: "name = '%s'\naccount_id = 'ffffffffffffffffffffffffffffffff'\n"},
		{name: "named-environment", mutate: func(r *VersionStageRequest) { r.Environment = "staging" }},
		{name: "unexpected-env-table", config: "name = '%s'\naccount_id = '%s'\n[env.staging]\n"},
		{name: "empty-default-without-opt-in", mutate: func(r *VersionStageRequest) { r.UseDefaultEnvironment = false }},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			req, argv, _ := defaultStageFixture(t)
			if item.config != "" {
				value := strings.ReplaceAll(item.config, "%s", req.Worker)
				if item.name == "wrong-worker-name" {
					value = strings.Replace(item.config, "%s", req.AccountID, 1)
				} else if item.name == "unexpected-env-table" {
					value = fmt.Sprintf(item.config, req.Worker, req.AccountID)
				} else if item.name == "wrong-account" {
					value = strings.Replace(item.config, "%s", req.Worker, 1)
				}
				if err := os.WriteFile(req.ConfigPath, []byte(value), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if item.mutate != nil {
				item.mutate(&req)
			}
			called := false
			result, err := StageVersionSecret(context.Background(), req, func(context.Context) ([]byte, error) {
				called = true
				return []byte("synthetic"), nil
			})
			if err == nil || result.State != VersionNotAttempted || called {
				t.Fatalf("unexpected stage: state=%s err=%v load=%v", result.State, err, called)
			}
			if _, err = os.Stat(argv); !os.IsNotExist(err) {
				t.Fatal("unexpected provider stage spawn")
			}
		})
	}
}

func TestDefaultEnvironmentPinnedStageRequiresGuard(t *testing.T) {
	req, _, _ := defaultStageFixture(t)
	pin := stageTestPin(t, req.WranglerPath)
	called := 0
	result, err := StagePinnedVersionSecret(context.Background(), req, pin,
		func(context.Context) ([]byte, error) { return []byte("synthetic"), nil },
		func(context.Context) error { called++; return nil })
	if err != nil || result.State != VersionAcknowledgedUnverified || called != 1 {
		t.Fatalf("result=%s err=%v guarded=%d", result.State, err, called)
	}
}

func TestDefaultEnvironmentDoesNotStartWhenConfigChangesAfterValidation(t *testing.T) {
	cases := []string{"load", "guard", "snapshot"}
	for _, when := range cases {
		t.Run(when, func(t *testing.T) {
			req, argv, input := defaultStageFixture(t)
			pin := stageTestPin(t, req.WranglerPath)
			secret := []byte("synthetic")
			drift := fmt.Sprintf("name = 'other-worker'\naccount_id = '%s'\n", req.AccountID)
			mutateSource := func() {
				if err := os.WriteFile(req.ConfigPath, []byte(drift), 0600); err != nil {
					t.Fatal(err)
				}
			}
			mutateSnapshot := func() {
				matches, err := filepath.Glob(filepath.Join(filepath.Dir(req.ConfigPath), "kc-stage-config-*.toml"))
				if err != nil || len(matches) != 1 {
					t.Fatalf("host snapshot count=%d err=%v", len(matches), err)
				}
				if err := os.Chmod(matches[0], 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(matches[0], []byte(drift), 0600); err != nil {
					t.Fatal(err)
				}
			}
			loaded := false
			guarded := false
			result, err := StagePinnedVersionSecret(context.Background(), req, pin, func(context.Context) ([]byte, error) {
				loaded = true
				if when == "load" {
					mutateSource()
				}
				if when == "snapshot" {
					mutateSnapshot()
				}
				return secret, nil
			}, func(context.Context) error {
				guarded = true
				if when == "guard" {
					mutateSource()
				}
				return nil
			})
			if err == nil || result.State != VersionNotAttempted || strings.Contains(err.Error(), "other-worker") || strings.Contains(err.Error(), "synthetic") {
				t.Fatalf("state=%s err=%v", result.State, err)
			}
			if !loaded || guarded != (when == "guard") {
				t.Fatalf("loaded=%v guarded=%v", loaded, guarded)
			}
			if _, statErr := os.Stat(argv); !os.IsNotExist(statErr) {
				t.Fatal("process started")
			}
			if _, statErr := os.Stat(input); !os.IsNotExist(statErr) {
				t.Fatal("secret transmitted")
			}
			if !bytes.Equal(secret, make([]byte, len(secret))) {
				t.Fatal("secret retained after refusal")
			}
			left, globErr := filepath.Glob(filepath.Join(filepath.Dir(req.ConfigPath), "kc-stage-config-*.toml"))
			if globErr != nil || len(left) != 0 {
				t.Fatalf("host snapshot leaked: %v %v", left, globErr)
			}
		})
	}
}
