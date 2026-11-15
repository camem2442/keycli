// Package rotation supplies a pure advisory gate for static service key retirement.
// No credential values, provider calls, authority grants or revocation occur here.
package rotation

import (
	"errors"
	"regexp"
	"time"
)

// Only static service keys use this lifecycle. Federated access tokens and
// GitHub job tokens are deliberately excluded from generation rotation.
type CredentialKind string

const StaticServiceKey CredentialKind = "static-service-key"

type Generation struct {
	Alias    string
	Kind     CredentialKind
	Provider string
	Target   string
	Number   uint64
}

type Consumer struct {
	ID       string
	Required bool
}

type ConsumerObservation struct {
	ID             string
	Generation     uint64
	SeenAt         time.Time
	EvidenceDigest string
	TrustedByHost  bool
	UnknownEffect  bool
}

type RetirementInput struct {
	Old                  Generation
	New                  Generation
	Consumers            []Consumer
	Observations         []ConsumerObservation
	StageVerified        bool
	PromotionVerified    bool
	SourceAndTargetBound bool
	UnknownRemoteEffects bool
	NewActivatedAt       time.Time
	GraceUntil           time.Time
	Now                  time.Time
}

type RetirementStatus string

const (
	RetirementHold      RetirementStatus = "HOLD"
	RetirementCandidate RetirementStatus = "READY_FOR_SEPARATE_REVOKER"
)

type RetirementDecision struct {
	Status               RetirementStatus
	Reason               string
	AuthorizesRevocation bool
}

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,127}$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// EvaluateRetirement computes a candidate decision only. Even a READY result
// cannot be passed as authorization to a credential issuer or revoker.
func EvaluateRetirement(in RetirementInput) (RetirementDecision, error) {
	hold := func(reason string) (RetirementDecision, error) {
		return RetirementDecision{Status: RetirementHold, Reason: reason, AuthorizesRevocation: false}, nil
	}
	if in.Old.Kind != StaticServiceKey || in.New.Kind != StaticServiceKey ||
		!namePattern.MatchString(in.Old.Alias) || !namePattern.MatchString(in.New.Alias) ||
		!namePattern.MatchString(in.Old.Provider) || !namePattern.MatchString(in.Old.Target) ||
		in.Old.Alias != in.New.Alias || in.Old.Provider != in.New.Provider || in.Old.Target != in.New.Target ||
		in.Old.Number == 0 || in.New.Number <= in.Old.Number || in.Now.IsZero() {
		return RetirementDecision{}, errors.New("invalid rotation generations or target identity")
	}
	if !in.StageVerified || !in.PromotionVerified || !in.SourceAndTargetBound || in.UnknownRemoteEffects {
		return hold("new generation is not fully verified")
	}
	if in.NewActivatedAt.IsZero() || !in.GraceUntil.After(in.NewActivatedAt) || in.Now.Before(in.GraceUntil) {
		return hold("rollback grace window remains open")
	}
	if len(in.Consumers) == 0 {
		return hold("no enumerated consumers")
	}
	requirements := make(map[string]bool, len(in.Consumers))
	for _, c := range in.Consumers {
		if !namePattern.MatchString(c.ID) || !c.Required || requirements[c.ID] {
			return RetirementDecision{}, errors.New("invalid, optional, or duplicate consumer")
		}
		requirements[c.ID] = true
	}
	actual := make(map[string]ConsumerObservation, len(in.Observations))
	for _, obs := range in.Observations {
		if _, ok := requirements[obs.ID]; !ok || actual[obs.ID].ID != "" {
			return RetirementDecision{}, errors.New("unknown or duplicate consumer observation")
		}
		actual[obs.ID] = obs
	}
	for id := range requirements {
		obs, ok := actual[id]
		if !ok || !obs.TrustedByHost || obs.UnknownEffect || obs.Generation != in.New.Number ||
			!digestPattern.MatchString(obs.EvidenceDigest) || obs.SeenAt.Before(in.NewActivatedAt) ||
			obs.SeenAt.After(in.Now) || in.Now.Sub(obs.SeenAt) > 15*time.Minute {
			return hold("required consumer is not freshly attested to the new generation")
		}
	}
	return RetirementDecision{Status: RetirementCandidate, Reason: "owner revocation admission required", AuthorizesRevocation: false}, nil
}
