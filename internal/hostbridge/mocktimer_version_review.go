package hostbridge

// Review-only wire contract shared with MockTimer and workspace-infra.
// These operations deliberately remain absent from operationTable and the
// production Product loader until the real provider adapter is accepted.
import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
)

const (
	mockTimerAccount = "a3f7ae5cc3a22033fa53964e935c413a"
	mockTimerWorker = "mocktimer-members-production"
	MockTimerUpload = "upload-worker-artifact-version"
	MockTimerActivate = "activate-worker-artifact-version"
)

var (
	mockTimerSHA = regexp.MustCompile("^[a-f0-9]{40}$")
	mockTimerUUID = regexp.MustCompile("^[a-f0-9]{8}(-[a-f0-9]{4}){3}-[a-f0-9]{12}$")
	mockTimerOperationID = regexp.MustCompile("^[a-zA-Z0-9][a-zA-Z0-9_-]{0,95}$")
	errMockTimerPlan = errors.New("invalid MockTimer version plan")
)

type mockTimerVersionBody struct {
	Schema string `json:"schema"`
	AccountID string `json:"accountId"`
	WorkerName string `json:"workerName"`
	Head string `json:"head"`
	Previous string `json:"previous"`
	SourceFingerprint string `json:"sourceFingerprint"`
	PreviousFingerprint string `json:"previousFingerprint"`
	ArtifactsSHA256 string `json:"artifactsSha256"`
	ConfigSHA256 string `json:"configSha256"`
	PreviousArtifactsSHA256 string `json:"previousArtifactsSha256"`
	PreviousVersionID string `json:"previousVersionId"`
	OperationID string `json:"operationId"`
	Tag string `json:"tag"`
	Message string `json:"message"`
	PreviousTag string `json:"previousTag"`
	PreviousMessage string `json:"previousMessage"`
	TrafficPercent int `json:"trafficPercent"`
	DeployAuthorized bool `json:"deployAuthorized"`
}

type mockTimerVersionPlan struct {
	mockTimerVersionBody
	PlanDigest string `json:"planDigest"`
}

// MockTimerBindingReview is not RegisteredOperation, a grant or an execution
// decision. The proposed common resource must be enrolled by the real owner.
type MockTimerBindingReview struct {
	ResourceDigest string
	TargetDigest string
	PlanDigest string
	MutationID string
	CanRegister bool
	AuthorizesExecution bool
}

func mockTimerHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ReviewMockTimerVersionPlan verifies the exact ASCII JSON plan and projects
// matching proposed digests. Unknown/duplicate fields and real authority are
// refused. It has no credential or provider access.
func ReviewMockTimerVersionPlan(raw []byte, operation string) (MockTimerBindingReview, error) {
	var result MockTimerBindingReview
	if len(raw) == 0 || len(raw) > 8192 || (operation != MockTimerUpload && operation != MockTimerActivate) {
		return result, errMockTimerPlan
	}
	// Go's default decoder accepts duplicate object keys. Reject them first.
	d := json.NewDecoder(bytes.NewReader(raw))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return result, errMockTimerPlan
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok {
			return result, errMockTimerPlan
		}
		if _, exists := fields[name]; exists {
			return result, errMockTimerPlan
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(value, []byte("null")) {
			return result, errMockTimerPlan
		}
		fields[name] = value
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') {
		return result, errMockTimerPlan
	}
	if _, err := d.Token(); err != io.EOF {
		return result, errMockTimerPlan
	}
	var p mockTimerVersionPlan
	strict := json.NewDecoder(bytes.NewReader(raw))
	strict.DisallowUnknownFields()
	if len(fields) != 20 || strict.Decode(&p) != nil ||
		p.Schema != "mocktimer-worker-version-plan/v1" || p.AccountID != mockTimerAccount ||
		p.WorkerName != mockTimerWorker || p.TrafficPercent != 100 || p.DeployAuthorized ||
		!mockTimerSHA.MatchString(p.Head) || !mockTimerSHA.MatchString(p.Previous) ||
		p.Head == p.Previous || !mockTimerUUID.MatchString(p.PreviousVersionID) ||
		p.PreviousVersionID == "22222222-2222-4222-8222-222222222222" ||
		!mockTimerOperationID.MatchString(p.OperationID) {
		return result, errMockTimerPlan
	}
	for _, value := range []string{p.SourceFingerprint, p.PreviousFingerprint, p.ArtifactsSHA256, p.ConfigSHA256, p.PreviousArtifactsSHA256, p.PlanDigest} {
		if !sha256Hex.MatchString(value) {
			return result, errMockTimerPlan
		}
	}
	if p.Tag != p.Head[:12] || p.PreviousTag != p.Previous[:12] ||
		p.Message != p.SourceFingerprint[:12]+" artifacts:"+p.ArtifactsSHA256[:12] ||
		p.PreviousMessage != p.PreviousFingerprint[:12]+" artifacts:"+p.PreviousArtifactsSHA256[:12] {
		return result, errMockTimerPlan
	}
	body, err := json.Marshal(p.mockTimerVersionBody)
	if err != nil || mockTimerHash(body) != p.PlanDigest {
		return result, errMockTimerPlan
	}
	// Enforce exact key spelling (Go struct decode is case insensitive).
	var canonical map[string]json.RawMessage
	complete, _ := json.Marshal(p)
	if json.Unmarshal(complete, &canonical) != nil || len(canonical) != len(fields) {
		return result, errMockTimerPlan
	}
	for name := range canonical {
		if _, ok := fields[name]; !ok {
			return result, errMockTimerPlan
		}
	}
	resource := []byte(`{"provider":"cloudflare","account_id":"a3f7ae5cc3a22033fa53964e935c413a","resolved_worker_name":"mocktimer-members-production","binding_name":"worker-artifact"}`)
	suffix := "-upload"
	if operation == MockTimerActivate {
		suffix = "-activate"
	}
	return MockTimerBindingReview{
		ResourceDigest: mockTimerHash(resource),
		TargetDigest: TargetDigest("cloudflare", operation, mockTimerAccount+"/"+mockTimerWorker),
		PlanDigest: p.PlanDigest, MutationID: p.OperationID+suffix,
		CanRegister: false, AuthorizesExecution: false,
	}, nil
}
