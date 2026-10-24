// Command fakesecurity emulates macOS security(1) for Linux CI.
// References: man security(1); errSecItemNotFound (-25300) → exit 44 in common scripts;
// sandbox errors often include "User interaction is not allowed."
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type state struct {
	Entries map[string]string `json:"entries"`
}

func main() {
	dispatch(os.Args)
}

func dispatch(argv []string) {
	logArgv(argv)
	if os.Getenv("KC_FAKE_SECURITY_SANDBOX") == "1" {
		fail(128, "User interaction is not allowed.")
	}
	if len(argv) < 2 {
		fail(64, "usage: security command")
	}
	if argv[1] == "-i" {
		runInteractive()
		return
	}
	sub := argv[1]
	args := argv[2:]
	switch sub {
	case "find-generic-password":
		findPassword(args)
	case "add-generic-password":
		addPassword(args)
	case "delete-generic-password":
		deletePassword(args)
	case "dump-keychain":
		dumpKeychain()
	default:
		fail(1, "unknown security command")
	}
}

func runInteractive() {
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line == "quit" || line == "exit" {
			continue
		}
		dispatch(append([]string{"security"}, splitFields(line)...))
	}
}

func findPassword(args []string) {
	if passwordInArgv(args) {
		fail(2, "password must not appear in argv")
	}
	svc := flagVal(args, "-s")
	acct := flagVal(args, "-a")
	st := loadState()
	if v, ok := st.Entries[entryKey(svc, acct)]; ok {
		os.Stdout.WriteString(v)
		return
	}
	fail(44, "security: SecKeychainSearchCopyNext: The specified item could not be found in the keychain.")
}

func addPassword(args []string) {
	if passwordInArgv(args) {
		fail(2, "password must not appear in argv")
	}
	svc := flagVal(args, "-s")
	acct := flagVal(args, "-a")
	if strings.HasPrefix(svc, "local.study.ai.") {
		fail(51, "User canceled the operation.")
	}
	body, _ := io.ReadAll(os.Stdin)
	if len(body) == 0 {
		fail(1, "missing password on stdin")
	}
	st := loadState()
	if st.Entries == nil {
		st.Entries = map[string]string{}
	}
	st.Entries[entryKey(svc, acct)] = string(body)
	saveState(st)
}

func deletePassword(args []string) {
	svc := flagVal(args, "-s")
	acct := flagVal(args, "-a")
	st := loadState()
	key := entryKey(svc, acct)
	if _, ok := st.Entries[key]; !ok {
		fail(44, "security: SecKeychainSearchCopyNext: The specified item could not be found in the keychain.")
	}
	delete(st.Entries, key)
	saveState(st)
}

func dumpKeychain() {
	st := loadState()
	fmt.Println(`keychain: "/Fake/Keychains/login.keychain-db"`)
	for key := range st.Entries {
		svc, _ := splitKey(key)
		fmt.Println("service:", svc)
	}
}

func entryKey(service, account string) string {
	return service + "\t" + account
}

func splitKey(key string) (string, string) {
	svc, acct, _ := strings.Cut(key, "\t")
	return svc, acct
}

func flagVal(args []string, flag string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

func passwordInArgv(args []string) bool {
	for i := 0; i < len(args); i++ {
		if args[i] == "-w" && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			return true
		}
	}
	return false
}

func statePath() string {
	if p := os.Getenv("KC_FAKE_SECURITY_STATE"); p != "" {
		return p
	}
	return filepath.Join(os.TempDir(), "kc-fake-security.json")
}

func argvLogPath() string {
	if p := os.Getenv("KC_FAKE_SECURITY_ARGV_LOG"); p != "" {
		return p
	}
	return filepath.Join(os.TempDir(), "kc-fake-security-argv.log")
}

func loadState() state {
	data, err := os.ReadFile(statePath())
	if err != nil {
		return state{Entries: map[string]string{}}
	}
	var st state
	if json.Unmarshal(data, &st) != nil || st.Entries == nil {
		return state{Entries: map[string]string{}}
	}
	return st
}

func saveState(st state) {
	data, _ := json.Marshal(st)
	_ = os.WriteFile(statePath(), data, 0o600)
}

func logArgv(argv []string) {
	f, err := os.OpenFile(argvLogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, strings.Join(argv, " "))
}

func fail(code int, msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(code)
}

func splitFields(s string) []string {
	return strings.Fields(s)
}
