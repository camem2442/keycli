// Command kc-gateway serves the operational gateway or an explicitly selected
// synthetic harness.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/version"
)

type ledgerEntry struct {
	Seq      int    `json:"seq"`
	At       string `json:"at"`
	Mutation string `json:"mutation_id"`
	Target   string `json:"target_digest"`
	Owner    string `json:"owner"`
	Session  string `json:"session"`
	Gen      int64  `json:"generation"`
	PrevHash string `json:"prev_hash"`
	Hash     string `json:"hash"`
}

type ledgerProvider struct {
	mu             sync.Mutex
	path           string
	seq            int
	prev           string
	failAfterApply bool
}

func (l *ledgerProvider) Apply(_ context.Context, req effectgateway.Request) (effectgateway.EffectResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	entry := ledgerEntry{
		Seq: l.seq, At: time.Now().UTC().Format(time.RFC3339Nano),
		Mutation: req.MutationID, Target: req.TargetDigest, Owner: req.Owner,
		Session: req.Session, Gen: req.Generation, PrevHash: l.prev,
	}
	raw, _ := json.Marshal(entry)
	sum := sha256.Sum256(append([]byte(l.prev+"|"), raw...))
	entry.Hash = hex.EncodeToString(sum[:])
	l.prev = entry.Hash
	line, _ := json.Marshal(entry)
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return effectgateway.EffectResult{}, err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return effectgateway.EffectResult{}, err
	}
	if err := f.Sync(); err != nil {
		return effectgateway.EffectResult{}, err
	}
	if l.failAfterApply {
		// The provider applied the effect; the response is "lost".
		return effectgateway.EffectResult{}, fmt.Errorf("simulated lost response")
	}
	return effectgateway.EffectResult{State: effectgateway.StateAcknowledged, ProofDigest: entry.Hash}, nil
}

// allowAdmission stands in for the host bridge in this synthetic harness. Every
// decision is logged so the race evidence shows admission actually ran.
type allowAdmission struct {
	mu sync.Mutex
	n  int
}

func (a *allowAdmission) Bind(_ context.Context, _ effectgateway.Request) (effectgateway.BoundAuthority, error) {
	a.mu.Lock()
	a.n++
	a.mu.Unlock()
	log.Print("synthetic admission allow")
	return a, nil
}

func (*allowAdmission) Preflight(context.Context) error { return nil }

func (*allowAdmission) VerifyExecution(context.Context) error { return nil }

func main() {
	flag.CommandLine.Init("kc-gateway", flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)
	addr := flag.String("addr", "127.0.0.1:18090", "loopback listen address")
	journalPath := flag.String("journal", "", "journal root (required)")
	claimsPath := flag.String("claims", "", "claim store sqlite path (required, read-only)")
	keyOut := flag.String("key-out", "", "file that receives the issued session key (0600)")
	ledger := flag.String("ledger", "", "synthetic effect ledger path (required)")
	goal := flag.String("goal", "", "goal id the session key is bound to")
	owner := flag.String("owner", "", "claim owner identity")
	session := flag.String("session", "", "claim session id")
	sessionState := flag.String("session-state", "ACTIVE", "trusted host session state: ACTIVE|RECONCILING")
	generation := flag.Int64("generation", 0, "claim generation")
	ttl := flag.Duration("session-ttl", 15*time.Minute, "session key lifetime")
	credential := flag.String("credential", "", "synthetic provider credential file the service owns (0600)")
	showVersion := flag.Bool("version", false, "print the build version and source commit")
	lockReport := flag.Bool("lock-report", false, "print the provider lock report and exit")
	providerKind := flag.String("provider", "", "provider adapter: synthetic|live")
	failAfterApply := flag.Bool("fail-after-apply", false, "apply the effect, then fail (lost response)")
	reconcile := flag.String("synthetic-reconcile", "", "settle a synthetic fixture mutation and exit")
	proof := flag.String("proof", "", "fixture proof digest for -synthetic-reconcile")
	targetFlag := flag.String("target", "", "fixture target digest for -synthetic-reconcile")
	planFlag := flag.String("plan", "", "fixture plan digest for -synthetic-reconcile")
	grantFlag := flag.String("grant", "", "fixture grant digest for -synthetic-reconcile")
	authorityConfig := flag.String("authority-config", "", "absolute path to the protected host Authority configuration")
	productConfig := flag.String("product-config", "", "absolute path to the protected provider configuration")
	readConfig := flag.String("read-config", "", "absolute path to the protected read grant configuration")
	if err := flag.CommandLine.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			flag.CommandLine.SetOutput(os.Stderr)
			flag.CommandLine.PrintDefaults()
			return
		}
		log.Fatal("invalid gateway options")
	}

	if *showVersion {
		fmt.Println(version.String())
		return
	}
	if *lockReport {
		fmt.Println(effectgateway.ReportJSON())
		return
	}
	seen := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { seen[f.Name] = true })
	if flag.NArg() != 0 {
		log.Fatal("unexpected positional arguments")
	}
	if *providerKind == "live" {
		if err := effectgateway.AllowRealProvider("kc-gateway"); err != nil {
			log.Fatal("provider effects are locked")
		}
		for _, option := range []string{"ledger", "credential", "fail-after-apply", "synthetic-reconcile", "proof", "target", "plan", "grant"} {
			if seen[option] {
				log.Fatal("synthetic options are unavailable in operational mode")
			}
		}
		if *authorityConfig == "" || *productConfig == "" || *readConfig == "" {
			log.Fatal("operational gateway requires Authority, product and read grant configuration")
		}
		runOperational(operationalOptions{
			addr: *addr, journal: *journalPath, claims: *claimsPath, keyOut: *keyOut,
			goal: *goal, owner: *owner, session: *session, state: *sessionState, generation: *generation, ttl: *ttl,
			authorityConfig: *authorityConfig, productConfig: *productConfig, readConfig: *readConfig,
		})
		return
	}
	if *providerKind != "synthetic" {
		log.Fatal("select -provider live or -provider synthetic explicitly")
	}
	if seen["product-config"] || seen["read-config"] || seen["session-state"] {
		log.Fatal("operational options are unavailable in synthetic mode")
	}

	// A supplied empty path must not fall back to synthetic admission.
	useLocalAuthority := seen["authority-config"]
	var authority effectgateway.Authority
	if useLocalAuthority {
		if *reconcile != "" || *failAfterApply {
			log.Fatal("-authority-config cannot be combined with synthetic reconcile or lost-response simulation")
		}
		local, err := effectgateway.LoadLocalAuthority(*authorityConfig)
		if err != nil {
			log.Fatal(err)
		}
		authority = local
	} else {
		authority = &allowAdmission{}
	}

	if !literalLoopback(*addr) {
		log.Fatal("listen address must be a literal loopback IP and port")
	}
	if *journalPath == "" {
		log.Fatal("-journal is required")
	}
	if *reconcile == "" && (*claimsPath == "" || *ledger == "" || *keyOut == "" || *goal == "" ||
		*owner == "" || *session == "" || *generation < 1) {
		log.Fatal("journal, claims, ledger, key-out, goal, owner, session and generation are required")
	}
	if err := os.MkdirAll(*journalPath, 0o700); err != nil {
		log.Fatalf("journal: %v", err)
	}

	if *reconcile != "" {
		if *proof == "" || *targetFlag == "" || *planFlag == "" || *grantFlag == "" || *journalPath == "" {
			log.Fatal("-synthetic-reconcile requires -proof, -target, -plan, -grant and -journal")
		}
		store := journal.Store{Root: *journalPath}
		intent := journal.Intent{OperationID: *reconcile, TargetDigest: *targetFlag, PlanDigest: *planFlag, GrantDigest: *grantFlag}
		record, err := store.Reconcile(intent, func(current journal.Record) (journal.Observation, error) {
			return journal.Observation{State: journal.ObservedApplied, ProofDigest: *proof}, nil
		})
		if err != nil {
			log.Fatalf("reconcile: %v", err)
		}
		fmt.Printf("reconciled %s state=%s\n", record.OperationID, record.State)
		return
	}

	claims := effectgateway.SQLiteCLIClaims{DBPath: *claimsPath}
	snapshot, err := claims.Snapshot(context.Background(), *goal)
	if err != nil {
		log.Fatalf("claim snapshot: %v", err)
	}
	if snapshot.Generation != *generation || snapshot.Owner != *owner ||
		snapshot.Session != *session || snapshot.State != "ACTIVE" {
		log.Fatalf("claim does not match the requested session: %+v", snapshot)
	}

	if *credential != "" {
		info, statErr := os.Stat(*credential)
		if statErr != nil {
			log.Fatalf("credential: %v", statErr)
		}
		if info.Mode().Perm()&0o077 != 0 {
			log.Fatalf("credential must not be group/world accessible: %o", info.Mode().Perm())
		}
		log.Printf("credential readable by service uid only (mode=%o size=%d)", info.Mode().Perm(), info.Size())
	}

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal("listen failed")
	}
	defer listener.Close()

	ring := effectgateway.NewMemoryKeyRing(*ttl, nil)
	key, err := ring.Issue(*goal, *owner, *session, *generation)
	if err != nil {
		log.Fatalf("issue key: %v", err)
	}
	if err := effectgateway.WriteSessionKeyFile(*keyOut, key); err != nil {
		log.Fatalf("write key: %v", err)
	}

	gw := &effectgateway.Gateway{
		Claims:    claims,
		Keys:      ring,
		Authority: authority,
		Journal:   journal.Store{Root: *journalPath},
		Provider:  &ledgerProvider{path: *ledger, failAfterApply: *failAfterApply},
	}
	serve(listener, gw, ring.RevokeAll)
}

func literalLoopback(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsLoopback() || ip.Zone() != "" || port == "" || len(port) > 5 {
		return false
	}
	for _, digit := range port {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	_, err = strconv.ParseUint(port, 10, 16)
	return err == nil
}

func serve(listener net.Listener, gateway *effectgateway.Gateway, revoke func()) {
	server := &http.Server{
		Handler:           gateway.Handler(30 * time.Second),
		ErrorLog:          log.New(io.Discard, "", 0),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	defer revoke()
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		revoke()
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()

	log.Print("gateway listening on loopback")
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatal("gateway serve failed")
	}
	stop()
	<-done
}
