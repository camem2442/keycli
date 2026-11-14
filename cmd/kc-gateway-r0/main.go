// Command kc-gateway-r0 runs a disposable, synthetic-only ledger gateway using
// the REAL hostbridge.Bridge + original workspace-infra Goal Grant/Claim/lease.
// It has no live provider mode, no credential input, and requires pinned host
// source and exact job identity before accepting signed loopback intents.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/hostbridge"
)

type syntheticLedger struct {
	mu   sync.Mutex
	path string
}

func (l *syntheticLedger) Apply(_ context.Context, req effectgateway.Request) (effectgateway.EffectResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if req.Provider != "synthetic" || req.Operation != "record-local-test" || req.Action != hostbridge.ActionStage {
		return effectgateway.EffectResult{}, errors.New("synthetic ledger refuses other effects")
	}
	entry := map[string]any{"kind": "synthetic-r0-ledger", "mutation_id": req.MutationID,
		"goal_id": req.GoalID, "target_digest": req.TargetDigest,
		"plan_digest": req.PlanDigest, "grant_digest": req.GrantDigest,
		"owner": req.Owner, "session": req.Session, "generation": req.Generation}
	data, err := json.Marshal(entry)
	if err != nil {
		return effectgateway.EffectResult{}, err
	}
	sum := sha256.Sum256(data)
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return effectgateway.EffectResult{}, err
	}
	defer f.Close()
	if _, err = f.Write(append(data, '\n')); err != nil {
		return effectgateway.EffectResult{}, err
	}
	if err = f.Sync(); err != nil {
		return effectgateway.EffectResult{}, err
	}
	return effectgateway.EffectResult{State: effectgateway.StateAcknowledged,
		ProofDigest: hex.EncodeToString(sum[:])}, nil
}
func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("gateway must bind numeric loopback only")
	}
	return nil
}

func main() {
	addr := flag.String("addr", "127.0.0.1:18090", "numeric loopback listen address")
	journalRoot := flag.String("journal", "", "new private journal root")
	claimsFile := flag.String("claims", "", "existing read-only Goal claims.db")
	keyFile := flag.String("key-out", "", "new private synthetic signing key output")
	ledgerFile := flag.String("ledger", "", "new one-effect synthetic ledger output")
	goal := flag.String("goal", "", "original Goal ID")
	owner := flag.String("owner", "", "current Writer executor")
	session := flag.String("session", "", "current Writer session")
	generation := flag.Int64("generation", 0, "current Writer generation")
	mutation := flag.String("mutation", "", "exact admitted Goal mutation ID")
	infra := flag.String("infra-root", "", "pinned workspace-infra checkout")
	infraSHA := flag.String("infra-sha", "", "pinned workspace-infra HEAD")
	stateRoot := flag.String("state-root", "", "original disposable Goal STATE root")
	planPath := flag.String("plan-file", "", "host-owned immutable synthetic Goal plan")
	sourceHead := flag.String("source-head", "", "actual disposable Goal Git HEAD")
	baseHead := flag.String("base-head", "", "existing disposable Goal baseline Git HEAD")
	repository := flag.String("repository", "", "host-owned synthetic repository slug")
	planDigest := flag.String("plan-digest", "", "original plan raw SHA256")
	grantDigest := flag.String("grant-digest", "", "original Grant raw SHA256")
	isolationDigest := flag.String("isolation-digest", "", "host-declared synthetic isolation profile SHA256")
	flag.Parse()

	if effectgateway.CurrentLock().Channel != "dev" || os.Getenv(effectgateway.ProviderEffectsEnv) != "" {
		log.Fatal("synthetic R0 cannot run in an operational artifact or provider-effects runtime")
	}
	if requireLoopback(*addr) != nil || *journalRoot == "" || *keyFile == "" || *ledgerFile == "" ||
		*claimsFile == "" || *stateRoot == "" || *goal == "" || *mutation == "" || *owner == "" || *session == "" {
		log.Fatal("incomplete or non-loopback synthetic R0 configuration")
	}
	state, err := filepath.Abs(*stateRoot)
	if err != nil || filepath.Clean(*claimsFile) != filepath.Join(state, "claims.db") {
		log.Fatal("claim path is not owned by the declared Goal STATE root")
	}
	for _, name := range []string{*keyFile, *ledgerFile, *journalRoot} {
		if _, err := os.Lstat(name); err == nil {
			log.Fatal("new synthetic output already exists")
		}
		if !errors.Is(err, os.ErrNotExist) {
			log.Fatal("unobservable output path")
		}
	}
	if err := os.Mkdir(*journalRoot, 0o700); err != nil {
		log.Fatalf("journal root: %v", err)
	}
	target := hostbridge.TargetDigest("synthetic", "record-local-test", "disposable-only")
	port := &hostbridge.GoalHostPort{Python: "/usr/bin/python3",
		Script:    filepath.Join(*infra, "tooling", "ha_keycli_goal_hostcheck.py"),
		InfraRoot: *infra, InfraSHA: *infraSHA, StateRoot: state, PlanPath: *planPath,
		GoalID: *goal, MutationID: *mutation, Owner: *owner, Session: *session,
		Generation: *generation, PlanDigest: *planDigest, GrantDigest: *grantDigest,
		TargetDigest: target, SourceHead: *sourceHead, BaseSHA: *baseHead,
		Repository: *repository, RawTarget: "disposable-only", IsolationDigest: *isolationDigest}
	if err := port.Validate(); err != nil {
		log.Fatalf("trusted host configuration refused: %v", err)
	}
	claims := effectgateway.SQLiteCLIClaims{DBPath: *claimsFile}
	current, err := claims.Snapshot(context.Background(), *goal)
	if err != nil || current.Generation != *generation || current.Owner != *owner ||
		current.Session != *session || current.State != "ACTIVE" {
		log.Fatal("original active Goal Writer is unavailable")
	}
	ring := effectgateway.NewMemoryKeyRing(5*time.Minute, nil)
	key, err := ring.Issue(*goal, *owner, *session, *generation)
	if err != nil {
		log.Fatal("synthetic session key could not be issued")
	}
	if err := effectgateway.WriteSessionKeyFile(*keyFile, key); err != nil {
		log.Fatal("synthetic session key output refused")
	}
	realBridge := &hostbridge.Bridge{Admitter: hostbridge.GoalAdmitter{Port: port}, Identity: port.Identity()}
	binding := &hostbridge.GatewayBinding{Bridge: realBridge, RawTarget: port.RawTarget,
		ExpectedHead: port.SourceHead, PlanDigest: port.PlanDigest, GrantDigest: port.GrantDigest,
		Owner: port.Owner, Session: port.Session, CheckCurrent: port.CheckCurrent}
	gw := &effectgateway.Gateway{Claims: claims, Keys: ring, Authority: effectgateway.GoalHostAuthority{Binding: binding},
		Journal: journal.Store{Root: *journalRoot}, Provider: &syntheticLedger{path: *ledgerFile}}
	server := &http.Server{Addr: *addr, Handler: gw.Handler(20 * time.Second),
		ReadHeaderTimeout: 4 * time.Second, ReadTimeout: 20 * time.Second,
		WriteTimeout: 20 * time.Second, IdleTimeout: 30 * time.Second}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		ring.RevokeAll()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	log.Printf("synthetic R0 hostbridge gateway on numeric loopback; no live provider")
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("R0 listener: %v", err)
	}
	fmt.Println("R0 synthetic gateway stopped")
}
