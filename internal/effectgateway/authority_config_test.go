//go:build darwin || linux

package effectgateway

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostAuthorityConfigRejectsUnsafeInputs(t *testing.T) {
	base, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(base, "kc-ac-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketDir := filepath.Join(dir, "socket")
	if err := os.Mkdir(socketDir, 0700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(socketDir, "authority.sock")
	operation := fmt.Sprintf(`{"goal_id":"goal","mutation_id":"mut","operation":"op","writer_generation":1,
		"owner":"owner","session":"session","provider":"cloudflare","provider_operation":"create-account-token",
		"canonical_target":"fixture/account","resource_digest":%q,"plan_digest":%q,"grant_digest":%q}`,
		strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64))
	document := fmt.Sprintf(`{"v":1,"socket_path":%q,"socket_uid":%d,"registrations":[%s]}`, socket, os.Geteuid(), operation)
	path := filepath.Join(dir, "authority.json")
	write := func(t *testing.T, raw string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	write(t, document, 0600)
	// A protected, absent endpoint may start; the client checks it on each use.
	a, err := LoadLocalAuthority(path)
	if err != nil {
		t.Fatal(err)
	}
	if a.Admitter == nil || a.Execution != nil {
		t.Fatal("startup without guarded settings must compose only the v1 advisory client")
	}
	guardDir := filepath.Join(dir, "guarded")
	if err := os.Mkdir(guardDir, 0700); err != nil {
		t.Fatal(err)
	}
	guardSocket := filepath.Join(guardDir, "authority.sock")
	guarded := fmt.Sprintf(`{"socket_path":%q,"socket_uid":%d,"socket_pid":%d}`, guardSocket, os.Geteuid(), os.Getpid())
	withGuard := func(settings string) string {
		return document[:len(document)-1] + `,"guarded_execution":` + settings + "}"
	}
	write(t, withGuard(guarded), 0600)
	guardedAuthority, err := LoadLocalAuthority(path)
	if err != nil || guardedAuthority.Execution == nil || guardedAuthority.Admitter == nil {
		t.Fatalf("guarded startup under protected parent: %v", err)
	}
	for _, change := range []struct{ name, raw string }{
		{"oversize", document + strings.Repeat(" ", maxAuthorityConfig)},
		{"trailing", document + "{}"},
		{"duplicate field", strings.Replace(document, `"v":1`, `"v":1,"v":1`, 1)},
		{"case folded field", strings.Replace(document, `"v":1`, `"V":1`, 1)},
		{"null uid", strings.Replace(document, fmt.Sprintf(`"socket_uid":%d`, os.Geteuid()), `"socket_uid":null`, 1)},
		{"duplicate mutation", strings.Replace(document, operation, operation+","+operation, 1)},
		{"unknown registration field", strings.Replace(document, `"goal_id":`, `"extra":0,"goal_id":`, 1)},
		{"invalid resource digest", strings.Replace(document, strings.Repeat("a", 64), "x", 1)},
		{"noncanonical socket", strings.Replace(document, socket, socketDir+"/./authority.sock", 1)},
		{"null guarded execution", withGuard("null")},
		{"malformed guarded execution", withGuard("[]")},
		{"duplicate guarded execution", strings.TrimSuffix(withGuard(guarded), "}") + `,"guarded_execution":` + guarded + "}"},
		{"case folded guarded execution", strings.Replace(withGuard(guarded), `"guarded_execution"`, `"Guarded_execution"`, 1)},
		{"missing guarded PID", withGuard(fmt.Sprintf(`{"socket_path":%q,"socket_uid":%d}`, guardSocket, os.Geteuid()))},
		{"zero guarded PID", withGuard(fmt.Sprintf(`{"socket_path":%q,"socket_uid":%d,"socket_pid":0}`, guardSocket, os.Geteuid()))},
		{"out of range guarded PID", withGuard(fmt.Sprintf(`{"socket_path":%q,"socket_uid":%d,"socket_pid":2147483648}`, guardSocket, os.Geteuid()))},
		{"string guarded PID", withGuard(fmt.Sprintf(`{"socket_path":%q,"socket_uid":%d,"socket_pid":"1"}`, guardSocket, os.Geteuid()))},
		{"null guarded UID", withGuard(fmt.Sprintf(`{"socket_path":%q,"socket_uid":null,"socket_pid":1}`, guardSocket))},
		{"duplicate guarded UID", withGuard(strings.Replace(guarded, `"socket_uid":`, `"socket_uid":1,"socket_uid":`, 1))},
		{"unknown guarded field", withGuard(`{"extra":false,` + guarded[1:])},
		{"relative guarded socket", withGuard(strings.Replace(guarded, guardSocket, "guarded/authority.sock", 1))},
	} {
		t.Run(change.name, func(t *testing.T) {
			write(t, change.raw, 0600)
			if _, err := LoadLocalAuthority(path); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	if _, err := a.Registry.Resolve(context.Background(), "goal", "mut"); err != nil {
		t.Fatal("startup registration changed after file edits")
	}
	if _, err := a.Registry.Resolve(context.Background(), "goal", "other"); err == nil {
		t.Fatal("unregistered mutation resolved")
	}
	write(t, document, 0640)
	if _, err := LoadLocalAuthority(path); err == nil {
		t.Fatal("group-readable configuration accepted")
	}
	write(t, document, 0600)
	for name, link := range map[string]func(string, string) error{"symlink": os.Symlink, "hardlink": os.Link} {
		t.Run(name, func(t *testing.T) {
			alias := filepath.Join(dir, "alias")
			if err := link(path, alias); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(alias)
			if _, err := LoadLocalAuthority(alias); err == nil {
				t.Fatal("linked configuration accepted")
			}
		})
	}
	parentAlias := filepath.Join(dir, "parent-alias")
	if err := os.Symlink(socketDir, parentAlias); err != nil {
		t.Fatal(err)
	}
	write(t, strings.Replace(document, socket, filepath.Join(parentAlias, "authority.sock"), 1), 0600)
	if _, err := LoadLocalAuthority(path); err == nil {
		t.Fatal("symlink socket ancestor accepted")
	}
	write(t, document, 0600)
	if err := os.Symlink(path, socket); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLocalAuthority(path); err == nil {
		t.Fatal("symlink socket accepted")
	}
	if err := os.Remove(socket); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(socketDir, 0770); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLocalAuthority(path); err == nil {
		t.Fatal("shared writable socket parent accepted")
	}
	if err := os.Chmod(socketDir, 0700); err != nil {
		t.Fatal(err)
	}
	unsafeGuardDir := filepath.Join(dir, "unsafe-guarded")
	if err := os.Mkdir(unsafeGuardDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafeGuardDir, 0770); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		settings string
	}{
		{"symlink guarded ancestor", strings.Replace(guarded, guardSocket, filepath.Join(parentAlias, "authority.sock"), 1)},
		{"shared writable guarded parent", strings.Replace(guarded, guardSocket, filepath.Join(unsafeGuardDir, "authority.sock"), 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			write(t, withGuard(tc.settings), 0600)
			if _, err := LoadLocalAuthority(path); err == nil {
				t.Fatal("unsafe guarded socket path accepted")
			}
		})
	}
	write(t, withGuard(guarded), 0600)
	if err := os.Symlink(path, guardSocket); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLocalAuthority(path); err == nil {
		t.Fatal("symlink guarded socket accepted")
	}
	if err := os.Remove(guardSocket); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", guardSocket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err := os.Chmod(guardSocket, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLocalAuthority(path); err != nil {
		t.Fatalf("protected guarded socket rejected: %v", err)
	}
	wrongUID := strings.Replace(guarded, fmt.Sprintf(`"socket_uid":%d`, os.Geteuid()), fmt.Sprintf(`"socket_uid":%d`, uint32(os.Geteuid())+1), 1)
	write(t, withGuard(wrongUID), 0600)
	if _, err := LoadLocalAuthority(path); err == nil {
		t.Fatal("guarded socket owned by a different UID accepted")
	}
	write(t, withGuard(guarded), 0600)
	if err := os.Chmod(guardSocket, 0660); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLocalAuthority(path); err == nil {
		t.Fatal("guarded socket with group access accepted")
	}

}
