// Package hostbridge contains a synthetic-only trusted Goal admission port.
// It delegates authority to the original workspace-infra Python Goal checker;
// no independent grant rules, credentials or provider adapters are introduced.
package hostbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// GoalHostPort executes an already pinned, trusted workspace-infra checker.
// All paths must be supplied by the enclosing trusted host, NEVER by a signed
// agent request. It does not authenticate that host or provide same-UID sandbox.
// It must not be connected to a real provider; only a disposable local ledger.
type GoalHostPort struct {
	Python          string
	Script          string
	InfraRoot       string
	InfraSHA        string
	StateRoot       string
	PlanPath        string
	GoalID          string
	MutationID      string
	Owner           string
	Session         string
	Generation      int64
	PlanDigest      string
	GrantDigest     string
	TargetDigest    string
	SourceHead      string
	BaseSHA         string
	Repository      string
	RawTarget       string
	IsolationDigest string
}

type hostVerdict struct {
	Status                    string `json:"status"`
	GoalID                    string `json:"goal_id"`
	MutationID                string `json:"mutation_id"`
	SourceHead                string `json:"source_head"`
	GrantID                   string `json:"grant_id"`
	AuthorizesProviderEffect  bool   `json:"authorizes_provider_effect"`
	AuthorizesOperatingEffect bool   `json:"authorizes_operating_effect"`
}

var r0sha40 = regexp.MustCompile(`^[0-9a-f]{40}$`)
var r0digest = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (p *GoalHostPort) Validate() error { return p.valid() }

func (p *GoalHostPort) valid() error {
	if p == nil || p.Python != "/usr/bin/python3" ||
		!filepath.IsAbs(p.Script) || !filepath.IsAbs(p.InfraRoot) ||
		!filepath.IsAbs(p.StateRoot) || !filepath.IsAbs(p.PlanPath) ||
		p.GoalID == "" || p.MutationID == "" || p.Owner == "" || p.Session == "" ||
		p.Generation < 1 || p.RawTarget != "disposable-only" ||
		p.Repository == "" || !r0sha40.MatchString(p.InfraSHA) ||
		!r0sha40.MatchString(p.SourceHead) || !r0sha40.MatchString(p.BaseSHA) ||
		!r0digest.MatchString(p.IsolationDigest) || p.SourceHead == p.BaseSHA ||
		!r0digest.MatchString(p.PlanDigest) || !r0digest.MatchString(p.GrantDigest) {
		return errors.New("trusted R0 host pins are incomplete")
	}
	if p.TargetDigest != TargetDigest("synthetic", "record-local-test", p.RawTarget) {
		return errors.New("synthetic raw target does not match canonical host digest")
	}
	if filepath.Clean(p.Script) != filepath.Join(filepath.Clean(p.InfraRoot), "tooling", "ha_keycli_goal_hostcheck.py") {
		return errors.New("host checker is not the pinned owner source")
	}
	info, err := os.Lstat(p.Script)
	if err != nil {
		return fmt.Errorf("host checker unavailable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("pinned host checker must be a regular non-symlink file")
	}
	return nil
}

func (p *GoalHostPort) exactIntent(req GatewayIntent) bool {
	return req.GoalID == p.GoalID && req.MutationID == p.MutationID &&
		req.Provider == "synthetic" && req.Operation == "record-local-test" &&
		req.Action == ActionStage && req.TargetDigest == p.TargetDigest &&
		req.PlanDigest == p.PlanDigest && req.GrantDigest == p.GrantDigest &&
		req.Owner == p.Owner && req.Session == p.Session && req.Generation == p.Generation
}

// CheckCurrent always reads the ORIGINAL Goal claim, running mutation lease,
// registry and Grant. Every effect attempt invokes it at least twice.
func (p *GoalHostPort) CheckCurrent(ctx context.Context, req GatewayIntent) error {
	if err := p.valid(); err != nil {
		return err
	}
	if !p.exactIntent(req) {
		return errors.New("unbound signed synthetic intent")
	}
	_, err := p.evaluate(ctx, req)
	return err
}

func (p *GoalHostPort) evaluate(ctx context.Context, req GatewayIntent) (hostVerdict, error) {
	if ctx == nil || ctx.Err() != nil {
		return hostVerdict{}, errors.New("host context unavailable")
	}
	bounded, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	payload, err := json.Marshal(req)
	if err != nil || len(payload) > 4096 {
		return hostVerdict{}, errors.New("invalid bounded host payload")
	}
	cmd := exec.CommandContext(bounded, p.Python, "-I", "-B", p.Script,
		"--infra-root", p.InfraRoot, "--infra-sha", p.InfraSHA,
		"--state-root", p.StateRoot, "--plan", p.PlanPath)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent",
		"PYTHONDONTWRITEBYTECODE=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null", "GIT_TERMINAL_PROMPT=0"}
	out, err := cmd.Output()
	if err != nil || len(out) > 8192 {
		return hostVerdict{}, errors.New("original Goal host evaluator refused or unavailable")
	}
	var result hostVerdict
	if json.Unmarshal(bytes.TrimSpace(out), &result) != nil ||
		result.Status != "AUTHORIZED_SCOPED_SYNTHETIC" || result.GoalID != p.GoalID ||
		result.MutationID != p.MutationID || result.SourceHead != p.SourceHead ||
		result.GrantID == "" || result.AuthorizesProviderEffect ||
		result.AuthorizesOperatingEffect {
		return hostVerdict{}, errors.New("original Goal host evidence is unbound")
	}
	return result, nil
}

// GoalAdmitter translates the OWNED original Goal local_test grant into a
// synthetic ledger admission, never an external provider capability.
type GoalAdmitter struct{ Port *GoalHostPort }

func (a GoalAdmitter) Admit(req Request) (Decision, error) {
	p := a.Port
	if p == nil {
		return Decision{}, ErrNoAdmitter
	}
	if err := p.valid(); err != nil {
		return Decision{}, err
	}
	if req.GoalID != p.GoalID || req.OperationID != p.MutationID ||
		req.WriterGeneration != p.Generation || req.HeadSHA != p.SourceHead ||
		req.BaseSHA != p.BaseSHA || req.Repository != p.Repository ||
		req.Provider != "synthetic" || req.Action != ActionStage ||
		req.TargetDigest != p.TargetDigest ||
		req.OperationDigest != OperationDigest(p.MutationID, p.TargetDigest) ||
		req.CredentialIsolation != "synthetic-no-credential" ||
		req.CredentialIsolationDigest != p.IsolationDigest ||
		len(req.Paths) != 0 || req.BudgetCost != 0 {
		return Decision{}, errors.New("host bridge request is not the original scoped synthetic intent")
	}
	now := time.Now()
	template := GatewayIntent{
		GoalID: p.GoalID, MutationID: p.MutationID, Provider: "synthetic",
		Operation: "record-local-test", Action: ActionStage,
		TargetDigest: p.TargetDigest, PlanDigest: p.PlanDigest, GrantDigest: p.GrantDigest,
		Generation: p.Generation, Owner: p.Owner, Session: p.Session,
		ExpiresAt: now.Add(30 * time.Second).UTC(),
	}
	proof, err := p.evaluate(context.Background(), template)
	if err != nil {
		return Decision{}, err
	}
	return Decision{Status: StatusAdmitted, Reason: "ORIGINAL_GOAL_LOCAL_TEST_SCOPED",
		GrantID: proof.GrantID, AuthorizesExecution: true, ProviderMutation: true,
		ExpiresAt: now.Add(8 * time.Second), Bound: req}, nil
}

func (p *GoalHostPort) Identity() Identity {
	return Identity{GoalID: p.GoalID, OperationID: p.MutationID,
		MutationID: p.MutationID, WriterOwner: p.Owner, WriterSession: p.Session,
		Repository: p.Repository, HeadSHA: p.SourceHead, BaseSHA: p.BaseSHA,
		WriterGeneration: p.Generation, HostIdentity: strings.ToLower(p.Owner),
		CredentialIsolation: "synthetic-no-credential", IsolationDigest: p.IsolationDigest,
		BudgetCost: 0, Paths: []string{}}
}
