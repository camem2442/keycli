package hostbridge

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMockTimerVersionPlanSharedVector(t *testing.T) {
	p := mockTimerVersionPlan{mockTimerVersionBody: mockTimerVersionBody{
		Schema: "mocktimer-worker-version-plan/v1", AccountID: mockTimerAccount, WorkerName: mockTimerWorker,
		Head: strings.Repeat("a",40), Previous: strings.Repeat("b",40),
		SourceFingerprint: strings.Repeat("c",64), PreviousFingerprint: strings.Repeat("d",64),
		ArtifactsSHA256: strings.Repeat("e",64), ConfigSHA256: strings.Repeat("f",64),
		PreviousArtifactsSHA256: strings.Repeat("a",64),
		PreviousVersionID: "11111111-1111-4111-8111-111111111111", OperationID: "mt-fixture",
		Tag: strings.Repeat("a",12), Message: strings.Repeat("c",12)+" artifacts:"+strings.Repeat("e",12),
		PreviousTag: strings.Repeat("b",12), PreviousMessage: strings.Repeat("d",12)+" artifacts:"+strings.Repeat("a",12),
		TrafficPercent: 100, DeployAuthorized: false,
	}, PlanDigest: "171818d52b56a8d91ae3ddabe01a5f13dc1edf91aa02e3119ef3f669e337d539"}
	raw, err := json.Marshal(p)
	if err != nil { t.Fatal(err) }
	for _, tc := range []struct{ operation, target, mutation string }{
		{MockTimerUpload, "ce6893f9de3ed4bb0c5f6c8d2752a0055a6165931bfd617dc19357664f196a18", "mt-fixture-upload"},
		{MockTimerActivate, "fffef1fb3413556f14c86a3949b6ff362d92f311b508ac20f241e038bcc01fb9", "mt-fixture-activate"},
	} {
		got, err := ReviewMockTimerVersionPlan(raw,tc.operation)
		if err != nil || got.TargetDigest != tc.target || got.MutationID != tc.mutation ||
			got.ResourceDigest != "f2cf51f2ea7aa8e59c01c82bbc851ad37c4dc0318449cc14f1eafac446c53ce8" ||
			got.PlanDigest != p.PlanDigest || got.CanRegister || got.AuthorizesExecution {
			t.Fatalf("review mismatch: %+v %v",got,err)
		}
		if _, _, err := ClassifyOperation("cloudflare",tc.operation); err == nil {
			t.Fatal("review-only operation unexpectedly registered")
		}
	}
	for _, bad := range []string{
		strings.Replace(string(raw),"\"deployAuthorized\":false","\"deployAuthorized\":true",1),
		strings.Replace(string(raw),"\"deployAuthorized\":false","\"deployAuthorized\":false,\"deployAuthorized\":false",1),
		strings.Replace(string(raw),"\"deployAuthorized\":false","\"DeployAuthorized\":false",1),
		strings.Replace(string(raw),"\"trafficPercent\":100","\"trafficPercent\":50",1),
	} {
		if _, err := ReviewMockTimerVersionPlan([]byte(bad),MockTimerUpload); err == nil { t.Fatal("invalid plan accepted") }
	}
}
