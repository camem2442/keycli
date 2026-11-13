// Command kc-gateway runs the synthetic single effect gateway for S3 acceptance.
//
// It is a test harness, not a product entry point:
//   - the provider is a local append-only ledger (no network, no credentials),
//   - admission is a logged allow-check (the real HostBridge is covered by S2),
//   - the session key is issued here, bound to the verified claim, and written to
//     -key-out (0600) for out-of-band delivery to the writer host.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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

func (a *allowAdmission) AuthorizeEffect(_ context.Context, req effectgateway.Request) error {
	a.mu.Lock()
	a.n++
	a.mu.Unlock()
	log.Printf("synthetic admission provider=%s operation=%s action=%s target_digest=%s", req.Provider, req.Operation, req.Action, req.TargetDigest)
	return nil
}

func main() {
	addr := flag.String("addr", "127.0.0.1:18090", "loopback listen address")
	journalPath := flag.String("journal", "", "journal root (required)")
	claimsPath := flag.String("claims", "", "claim store sqlite path (required, read-only)")
	keyOut := flag.String("key-out", "", "file that receives the issued session key (0600)")
	ledger := flag.String("ledger", "", "synthetic effect ledger path (required)")
	goal := flag.String("goal", "", "goal id the session key is bound to")
	owner := flag.String("owner", "", "claim owner identity")
	session := flag.String("session", "", "claim session id")
	generation := flag.Int64("generation", 0, "claim generation")
	ttl := flag.Duration("session-ttl", 15*time.Minute, "session key lifetime")
	credential := flag.String("credential", "", "synthetic provider credential file the service owns (0600)")
	showVersion := flag.Bool("version", false, "print the build version and source commit")
	lockReport := flag.Bool("lock-report", false, "print the provider lock report and exit")
	providerKind := flag.String("provider", "synthetic", "provider adapter: synthetic|live")
	failAfterApply := flag.Bool("fail-after-apply", false, "apply the effect, then fail (lost response)")
	reconcile := flag.String("reconcile", "", "reconcile this mutation id and exit")
	proof := flag.String("proof", "", "proof digest for -reconcile")
	targetFlag := flag.String("target", "", "target digest (required for -reconcile)")
	planFlag := flag.String("plan", "", "plan digest (required for -reconcile)")
	grantFlag := flag.String("grant", "", "grant digest (required for -reconcile)")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.String())
		return
	}
	if *lockReport {
		fmt.Println(effectgateway.ReportJSON())
		return
	}
	if *providerKind == "live" {
		if err := effectgateway.AllowRealProvider("kc-gateway"); err != nil {
			log.Fatalf("provider refused: %v", err)
		}
		log.Fatal("live provider adapter is unavailable")
	} else if *providerKind != "synthetic" {
		log.Fatalf("unknown -provider %q", *providerKind)
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
			log.Fatal("-reconcile requires -proof, -target, -plan, -grant and -journal")
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

	ring := effectgateway.NewMemoryKeyRing(*ttl, nil)
	key, err := ring.Issue(*goal, *owner, *session, *generation)
	if err != nil {
		log.Fatalf("issue key: %v", err)
	}
	if err := os.WriteFile(*keyOut, []byte(effectgateway.HexKey(key)+"\n"), 0o600); err != nil {
		log.Fatalf("write key: %v", err)
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

	gw := &effectgateway.Gateway{
		Claims:    claims,
		Keys:      ring,
		Bridge:    &allowAdmission{},
		Journal:   journal.Store{Root: *journalPath},
		Provider:  &ledgerProvider{path: *ledger, failAfterApply: *failAfterApply},
		Synthetic: true,
	}
	server := &http.Server{
		Addr:              *addr,
		Handler:           gw.Handler(30 * time.Second),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("gateway listening on %s (loopback only; expose via portbridge preview)", *addr)
	log.Printf("journal=%s claims=%s ledger=%s key=%s", *journalPath, *claimsPath, *ledger, *keyOut)
	log.Printf("session key written (%s); deliver it out of band", filepath.Base(*keyOut))

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		log.Print("shutting down")
		ring.RevokeAll()
		_ = server.Shutdown(context.Background())
	}()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen: %v", err)
	}
	fmt.Println("gateway stopped")
}
