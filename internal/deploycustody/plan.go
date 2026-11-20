// Package deploycustody provides credential-free evidence planning for a
// single product deployment owner. It cannot authenticate hosts, read secrets,
// grant a mutation, run a provider, or replace the existing Effect Gateway.
package deploycustody

import (
	"regexp"
)

const (
	schema = "keycli/deploy-custody-observation/v1"
	repo   = "camem2442/mock-exam-timer"
	target = "cloudflare:mocktimer-members-production"
)

var (
	commitRE  = regexp.MustCompile("^[0-9a-f]{40}$")
	releaseRE = regexp.MustCompile("^release/20[0-9]{6}$")
	idRE      = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9_.:-]{0,95}$")
)

// HostState is a caller-supplied, non-authenticated metadata observation.
// WRITER/NONE/UNKNOWN are roles reported by a host, NOT provider evidence.
type HostState struct {
	HostID     string `json:"host_id"`
	Capability string `json:"capability"`
}

type Observation struct {
	Schema             string      `json:"schema"`
	Repository         string      `json:"repository"`
	ReleaseRef         string      `json:"release_ref"`
	SourceRevision     string      `json:"source_revision"`
	Target             string      `json:"target"`
	OwnerHostID        string      `json:"owner_host_id"`
	InventoryComplete  bool        `json:"inventory_complete"`
	UnresolvedEffect   bool        `json:"unresolved_effect"`
	OwnerSlotAmbiguous bool        `json:"owner_slot_ambiguous"`
	Hosts              []HostState `json:"hosts"`
}

// Verdict deliberately has no "authorized" success: the native host and
// provider owners must still prove custody, grant, credential scope & readback.
type Verdict struct {
	Status                   string `json:"status"`
	HostCount                int    `json:"host_count"`
	Target                   string `json:"target"`
	AuthorizesCredentialRead bool   `json:"authorizes_credential_read"`
	AuthorizesDeploy         bool   `json:"authorizes_deploy"`
	AuthorizesRecovery       bool   `json:"authorizes_recovery"`
}

func Assess(o Observation) Verdict {
	v := Verdict{Status: "HOLD_INVALID_INPUT"}
	if o.Schema != schema || o.Repository != repo || o.Target != target ||
		!commitRE.MatchString(o.SourceRevision) || !releaseRE.MatchString(o.ReleaseRef) ||
		!idRE.MatchString(o.OwnerHostID) || len(o.Hosts) == 0 || len(o.Hosts) > 32 {
		return v
	}
	v.Target = o.Target
	v.HostCount = len(o.Hosts)
	if o.UnresolvedEffect || o.OwnerSlotAmbiguous {
		v.Status = "HOLD_UNKNOWN_EFFECT_RECONCILIATION"
		return v
	}
	if !o.InventoryComplete {
		v.Status = "HOLD_INCOMPLETE_HOST_INVENTORY"
		return v
	}
	seen := make(map[string]bool, len(o.Hosts))
	owners := 0
	writers := 0
	for _, h := range o.Hosts {
		if !idRE.MatchString(h.HostID) || seen[h.HostID] {
			v.Status = "HOLD_INVALID_HOST"
			return v
		}
		seen[h.HostID] = true
		switch h.Capability {
		case "WRITER":
			writers++
			if h.HostID == o.OwnerHostID {
				owners++
			}
		case "NONE":
		case "UNKNOWN":
			v.Status = "HOLD_CREDENTIAL_CAPABILITY_UNKNOWN"
			return v
		default:
			v.Status = "HOLD_INVALID_HOST"
			return v
		}
	}
	if owners != 1 || writers != 1 {
		v.Status = "HOLD_MULTIPLE_OR_UNOWNED_CREDENTIALS"
		return v
	}
	// Supplied observations do not prove exclusive credentials or host identity.
	v.Status = "PLAN_ONLY_NEEDS_NATIVE_ATTESTATION"
	return v
}
