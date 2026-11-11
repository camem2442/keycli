package main

import (
	"context"
	"log"
	"net"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/effectgateway/providers"
)

type operationalOptions struct {
	addr, journal, claims, keyOut              string
	goal, owner, session, state                string
	generation                                 int64
	ttl                                        time.Duration
	authorityConfig, productConfig, readConfig string
}

func runOperational(opts operationalOptions) {
	if !literalLoopback(opts.addr) {
		log.Fatal("listen address must be a literal loopback IP and port")
	}
	if opts.journal == "" || opts.claims == "" || opts.keyOut == "" || opts.goal == "" ||
		opts.owner == "" || opts.session == "" || opts.generation < 1 || opts.ttl <= 0 ||
		(opts.state != "ACTIVE" && opts.state != "RECONCILING") {
		log.Fatal("operational gateway requires journal, claims, key output and an exact session identity")
	}
	authority, err := effectgateway.LoadOperationalAuthority(opts.authorityConfig)
	if err != nil || authority.Registry == nil || authority.Admitter == nil || authority.Execution == nil {
		log.Fatal("operational gateway requires protected Authority configuration and guarded execution")
	}
	product, err := providers.LoadProduct(opts.productConfig, authority)
	if err != nil {
		log.Fatal("invalid or unsafe operational provider configuration")
	}
	readAuthority, err := effectgateway.LoadReadAuthority(opts.readConfig)
	if err != nil {
		log.Fatal("invalid or unsafe operational read grant configuration")
	}
	if effectgateway.CheckPrivateClaims(opts.claims) != nil {
		log.Fatal("invalid or unsafe operational claim store")
	}
	if effectgateway.PrepareOperationalJournal(opts.journal) != nil {
		log.Fatal("invalid or unsafe operational journal")
	}
	listener, err := net.Listen("tcp", opts.addr)
	if err != nil {
		log.Fatal("listen failed")
	}
	defer listener.Close()

	claims := effectgateway.SQLiteCLIClaims{DBPath: opts.claims, Binary: "/usr/bin/sqlite3"}
	ring := effectgateway.NewMemoryKeyRing(opts.ttl, nil)
	defer ring.RevokeAll()
	identity := effectgateway.ClaimSnapshot{
		GoalID: opts.goal, Owner: opts.owner, Session: opts.session, Generation: opts.generation, State: opts.state,
	}
	if err := ring.IssueClaim(context.Background(), claims, identity, opts.keyOut); err != nil {
		log.Fatal("operational session issuance failed")
	}
	gateway := &effectgateway.Gateway{
		Operational: true,
		Claims:      claims, Keys: ring, Authority: authority, Journal: journal.Store{Root: opts.journal},
		RotationActions: product, ReadAuthority: readAuthority, RecoveryObserver: product,
	}
	serve(listener, gateway, ring.RevokeAll)
}
