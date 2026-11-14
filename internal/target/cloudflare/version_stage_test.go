package cloudflare

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testStage(t *testing.T, body string) (VersionStageRequest, string, string) {
	t.Helper()
	dir := t.TempDir()
	argv := filepath.Join(dir, "argv.txt")
	input := filepath.Join(dir, "input.txt")
	exe := filepath.Join(dir, "wrangler")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > '" + argv + "'\ncat > '" + input + "'\n" + body + "\n"
	if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "wrangler.toml")
	if err := os.WriteFile(cfg, []byte("name = 'fixture'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	account := strings.Repeat("a", 32)
	return VersionStageRequest{
		WranglerPath: exe, ConfigPath: cfg, WorkDirectory: dir,
		AccountID: account, Worker: "fixture-worker", Environment: "staging", SecretName: "OPENAI_API_KEY",
		Env: []string{"PATH=/usr/bin:/bin", "CLOUDFLARE_ACCOUNT_ID=" + account, "CLOUDFLARE_API_TOKEN=synthetic"},
	}, argv, input
}
func TestVersionStageOneInactiveCommand(t *testing.T) {
	req, argvPath, stdinPath := testStage(t, "echo 'pretend-secret' >&2; exit 0")
	value := []byte{0x41, 0x42, 0x0a, 0x00, 0xff}
	outcome, err := StageVersionSecret(context.Background(), req, func(context.Context) ([]byte, error) { return value, nil })
	if err != nil || outcome.State != VersionAcknowledgedUnverified {
		t.Fatalf("state=%s err=%v", outcome.State, err)
	}
	argv, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "versions secret put OPENAI_API_KEY --name fixture-worker --env staging --config " + req.ConfigPath
	if strings.TrimSpace(string(argv)) != want {
		t.Fatalf("argv mismatch: %q", argv)
	}
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stdin, []byte{0x41, 0x42, 0x0a, 0x00, 0xff}) {
		t.Fatal("stdin changed")
	}
	if !bytes.Equal(value, make([]byte, 5)) {
		t.Fatal("owned secret buffer was not cleared")
	}
}
func TestVersionStageDeniedBeforeSecretLookup(t *testing.T) {
	req, argv, _ := testStage(t, "")
	req.AccountID = strings.Repeat("b", 32)
	invoked := false
	outcome, err := StageVersionSecret(context.Background(), req, func(context.Context) ([]byte, error) { invoked = true; return []byte("x"), nil })
	if err == nil || outcome.State != VersionNotAttempted || invoked {
		t.Fatalf("invalid target accepted state=%s", outcome.State)
	}
	if _, e := os.Stat(argv); !os.IsNotExist(e) {
		t.Fatal("spawned invalid target")
	}
}
func TestVersionStageUnknownEffectAndRedaction(t *testing.T) {
	req, _, _ := testStage(t, "echo 'pretend-secret' >&2; exit 1")
	secret := []byte("pretend-secret")
	state, err := StageVersionSecret(context.Background(), req, func(context.Context) ([]byte, error) { return secret, nil })
	if state.State != VersionUnknownEffect || err == nil || strings.Contains(err.Error(), "pretend-secret") {
		t.Fatalf("not fail-closed: state=%s err=%v", state.State, err)
	}
}
func TestVersionStageNoAmbientCredentials(t *testing.T) {
	req, _, _ := testStage(t, "test -z \"$UNRELATED_SECRET\" || exit 7")
	t.Setenv("UNRELATED_SECRET", "ambient-private")
	outcome, err := StageVersionSecret(context.Background(), req, func(context.Context) ([]byte, error) { return []byte("x"), nil })
	if outcome.State != VersionAcknowledgedUnverified || err != nil {
		t.Fatalf("ambient env leaked: %s %v", outcome.State, err)
	}
}
func TestVersionStageCancelledBeforeLookup(t *testing.T) {
	req, argv, _ := testStage(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	invoked := false
	outcome, err := StageVersionSecret(ctx, req, func(context.Context) ([]byte, error) { invoked = true; return []byte("x"), nil })
	if err == nil || invoked || outcome.State != VersionNotAttempted {
		t.Fatal("canceled operation progressed")
	}
	if _, e := os.Stat(argv); !os.IsNotExist(e) {
		t.Fatal("spawned canceled operation")
	}
}
func TestVersionStageRejectsSymlinkAndExtraEnvironment(t *testing.T) {
	req, _, _ := testStage(t, "")
	req.Env = append(req.Env, "GITHUB_TOKEN=unexpected")
	if validVersionStageRequest(req) {
		t.Fatal("accepted ambient GitHub token")
	}
	req.Env = req.Env[:len(req.Env)-1]
	link := filepath.Join(req.WorkDirectory, "link")
	if err := os.Symlink(req.WranglerPath, link); err != nil {
		t.Fatal(err)
	}
	req.WranglerPath = link
	if validVersionStageRequest(req) {
		t.Fatal("accepted symlink binary")
	}
}

func stageTestPin(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func TestPinnedVersionStageChecksExecutableBeforeLoadAndStart(t *testing.T) {
	for _, changeDuringLoad := range []bool{false, true} {
		t.Run(fmt.Sprint(changeDuringLoad), func(t *testing.T) {
			req, argv, _ := testStage(t, "")
			pin := stageTestPin(t, req.WranglerPath)
			replace := func() {
				if err := os.WriteFile(req.WranglerPath, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if !changeDuringLoad {
				replace()
			}
			loadCalled := false
			result, err := StagePinnedVersionSecret(context.Background(), req, pin, func(context.Context) ([]byte, error) {
				loadCalled = true
				replace()
				return []byte("fixture"), nil
			}, func(context.Context) error {
				t.Fatal("guard reached after executable pin failed")
				return nil
			})
			if err == nil || result.State != VersionNotAttempted || loadCalled != changeDuringLoad {
				t.Fatalf("state=%s load=%v err=%v", result.State, loadCalled, err)
			}
			if _, err := os.Stat(argv); !os.IsNotExist(err) {
				t.Fatal("spawned after executable pin failed")
			}
		})
	}
}

func TestPinnedVersionStageGuardControlsStart(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprint(allow), func(t *testing.T) {
			req, argv, _ := testStage(t, "test -z \"$UNRELATED_SECRET\"")
			pin := stageTestPin(t, req.WranglerPath)
			loaded := false
			guardCalls := 0
			result, err := StagePinnedVersionSecret(context.Background(), req, pin, func(ctx context.Context) ([]byte, error) {
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("secret loader did not receive a bounded context")
				}
				loaded = true
				return []byte("fixture"), nil
			}, func(ctx context.Context) error {
				if _, ok := ctx.Deadline(); !ok || !loaded {
					t.Fatal("guard ran without a deadline or before loading")
				}
				guardCalls++
				if _, err := os.Stat(argv); !os.IsNotExist(err) {
					t.Fatal("process started before guard")
				}
				if !allow {
					return fmt.Errorf("synthetic private detail")
				}
				return nil
			})
			if guardCalls != 1 {
				t.Fatalf("guard ran %d times", guardCalls)
			}
			if allow {
				if err != nil || result.State != VersionAcknowledgedUnverified {
					t.Fatalf("state=%s err=%v", result.State, err)
				}
				return
			}
			if err == nil || result.State != VersionNotAttempted || strings.Contains(err.Error(), "synthetic private detail") {
				t.Fatalf("state=%s err=%v", result.State, err)
			}
			if _, err := os.Stat(argv); !os.IsNotExist(err) {
				t.Fatal("process started after guard denial")
			}
		})
	}
}
