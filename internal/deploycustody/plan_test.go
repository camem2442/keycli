package deploycustody

import "testing"

func fixture() Observation {
	return Observation{
		Schema:            schema,
		Repository:        repo,
		ReleaseRef:        "release/20261120",
		SourceRevision:    "aabbccddeeff00112233445566778899aabbccdd",
		Target:            target,
		OwnerHostID:       "owner-flip3",
		InventoryComplete: true,
		Hosts: []HostState{
			{HostID: "owner-flip3", Capability: "WRITER"},
			{HostID: "mac", Capability: "NONE"},
			{HostID: "vm", Capability: "NONE"},
		},
	}
}

func TestCandidateNeverAuthorizesEffects(t *testing.T) {
	v := Assess(fixture())
	if v.Status != "PLAN_ONLY_NEEDS_NATIVE_ATTESTATION" || v.HostCount != 3 {
		t.Fatalf("unexpected verdict: %+v", v)
	}
	if v.AuthorizesCredentialRead || v.AuthorizesDeploy || v.AuthorizesRecovery {
		t.Fatalf("metadata must not grant credentials or effects: %+v", v)
	}
}

func TestConflictingSecondWriterBlocks(t *testing.T) {
	f := fixture()
	f.Hosts[1].Capability = "WRITER"
	if v := Assess(f); v.Status != "HOLD_MULTIPLE_OR_UNOWNED_CREDENTIALS" {
		t.Fatalf("%+v", v)
	}
}

func TestUnobservedHostCapabilityBlocks(t *testing.T) {
	f := fixture()
	f.Hosts[1].Capability = "UNKNOWN"
	if v := Assess(f); v.Status != "HOLD_CREDENTIAL_CAPABILITY_UNKNOWN" {
		t.Fatalf("%+v", v)
	}
}

func TestIncompleteInventoryBlocks(t *testing.T) {
	f := fixture()
	f.InventoryComplete = false
	if v := Assess(f); v.Status != "HOLD_INCOMPLETE_HOST_INVENTORY" {
		t.Fatalf("%+v", v)
	}
}

func TestAmbiguousLiveEffectCannotAutoRecover(t *testing.T) {
	f := fixture()
	f.UnresolvedEffect = true
	if v := Assess(f); v.Status != "HOLD_UNKNOWN_EFFECT_RECONCILIATION" || v.AuthorizesRecovery {
		t.Fatalf("%+v", v)
	}
	f.UnresolvedEffect = false
	f.OwnerSlotAmbiguous = true
	if v := Assess(f); v.Status != "HOLD_UNKNOWN_EFFECT_RECONCILIATION" {
		t.Fatalf("%+v", v)
	}
}

func TestDuplicateHostsCannotAssertExclusiveCustody(t *testing.T) {
	f := fixture()
	f.Hosts[1].HostID = f.OwnerHostID
	if v := Assess(f); v.Status != "HOLD_INVALID_HOST" {
		t.Fatalf("%+v", v)
	}
}

func TestWrongOwnerCannotClaimCredential(t *testing.T) {
	f := fixture()
	f.OwnerHostID = "absent-host"
	if v := Assess(f); v.Status != "HOLD_MULTIPLE_OR_UNOWNED_CREDENTIALS" {
		t.Fatalf("%+v", v)
	}
}

func TestWrongProviderAndTargetRejected(t *testing.T) {
	for _, value := range []string{"cloudflare:other", "github:mocktimer", ""} {
		f := fixture()
		f.Target = value
		if v := Assess(f); v.Status != "HOLD_INVALID_INPUT" || v.AuthorizesDeploy {
			t.Fatalf("%+v", v)
		}
	}
}

func TestWrongRepositoryRevisionAndReleaseRejected(t *testing.T) {
	f := fixture()
	f.Repository = "camem2442/other"
	if v := Assess(f); v.Status != "HOLD_INVALID_INPUT" {
		t.Fatalf("%+v", v)
	}
	f = fixture()
	f.SourceRevision = "main"
	if v := Assess(f); v.Status != "HOLD_INVALID_INPUT" {
		t.Fatalf("%+v", v)
	}
	f = fixture()
	f.ReleaseRef = "feature/dev"
	if v := Assess(f); v.Status != "HOLD_INVALID_INPUT" {
		t.Fatalf("%+v", v)
	}
}

func TestBooleanHostFlagCannotBeForgedIntoGrant(t *testing.T) {
	f := fixture()
	f.InventoryComplete = true // still self-reported, NOT authentication.
	if v := Assess(f); v.AuthorizesDeploy || v.AuthorizesCredentialRead {
		t.Fatalf("%+v", v)
	}
}
