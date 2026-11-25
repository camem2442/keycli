package effectgateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"regexp"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/hostbridge"
)

// MockTimerVersionMetadata is supplied by an independently trusted observer.
// The hashes must refer to verified bytes/config, not merely remote annotations.
type MockTimerVersionMetadata struct {
	ID, Tag, Message, ArtifactsSHA256, ConfigSHA256 string
}

const mockTimerVersionAccount = "a3f7ae5cc3a22033fa53964e935c413a"
const mockTimerVersionWorker = "mocktimer-members-production"
var mockTimerVersionUUID = regexp.MustCompile(`^[a-f0-9]{8}(-[a-f0-9]{4}){3}-[a-f0-9]{12}package effectgateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"regexp"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/hostbridge"
)

// MockTimerVersionMetadata is supplied by an independently trusted observer.
// The hashes must refer to verified bytes/config, not merely remote annotations.
type MockTimerVersionMetadata struct {
	ID, Tag, Message, ArtifactsSHA256, ConfigSHA256 string
}

)

type MockTimerVersionSnapshot struct {
	AccountID, WorkerName string
	ActiveVersionID string
	TrafficPercent int
	Versions []MockTimerVersionMetadata
}

// MockTimerVersionPort is host-owned, fixed to one Worker and one exact build.
// No arbitrary command, account, token or artifact path enters from a Request.
// A real port is NOT enrolled by this source; current tests use fixture ports.
type MockTimerVersionPort interface {
	Observe(context.Context) (MockTimerVersionSnapshot, error)
	UploadInactive(context.Context) error
	ActivateExact(context.Context, string) error
}

// MockTimerVersionExecutor participates in Gateway.Submit's signed claim,
// authority, durable journal and execution fence. Non-operational only until
// real registry/observer enrollment and same-resource production fencing pass.
type MockTimerVersionExecutor struct {
	upload, activation hostbridge.MockTimerBindingReview
	uploadGrant, activationGrant string
	port MockTimerVersionPort
}

func NewMockTimerVersionExecutor(plan []byte, uploadGrant, activationGrant string, port MockTimerVersionPort) (*MockTimerVersionExecutor, error) {
	up, err := hostbridge.ReviewMockTimerVersionPlan(plan, hostbridge.MockTimerUpload)
	if err != nil { return nil, err }
	ac, err := hostbridge.ReviewMockTimerVersionPlan(plan, hostbridge.MockTimerActivate)
	if err != nil || port == nil || !journal.ValidDigest(uploadGrant) || !journal.ValidDigest(activationGrant) ||
		up.ResourceDigest != ac.ResourceDigest || up.TargetDigest == ac.TargetDigest || up.MutationID == ac.MutationID {
		return nil, ErrAdmissionRejected
	}
	return &MockTimerVersionExecutor{upload:up,activation:ac,uploadGrant:uploadGrant,activationGrant:activationGrant,port:port},nil
}

func (e *MockTimerVersionExecutor) matches(req Request) bool {
	if e == nil || e.port == nil || req.Provider != "cloudflare" || req.Action != hostbridge.ActionStage {
		// "stage" is the existing synthetic action class. The real activation
		// classification remains disabled; it must never inherit a secret grant.
		return false
	}
	var expected hostbridge.MockTimerBindingReview
	var grant string
	switch req.Operation {
	case hostbridge.MockTimerUpload:
		expected,grant=e.upload,e.uploadGrant
	case hostbridge.MockTimerActivate:
		expected,grant=e.activation,e.activationGrant
	default:
		return false
	}
	return req.MutationID==expected.MutationID && req.PlanDigest==expected.PlanDigest &&
		req.TargetDigest==expected.TargetDigest && req.GrantDigest==grant
}

func versionSnapshotValid(s MockTimerVersionSnapshot) bool {
	if s.AccountID!=mockTimerVersionAccount || s.WorkerName!=mockTimerVersionWorker || s.TrafficPercent!=100 || len(s.Versions)==0 || len(s.Versions)>100 {return false}
	seen:=map[string]bool{}
	for _,v:=range s.Versions {
		if !mockTimerVersionUUID.MatchString(v.ID) || seen[v.ID] {return false}
		seen[v.ID]=true
	}
	return seen[s.ActiveVersionID]
}

func findVersion(s MockTimerVersionSnapshot, id string) (MockTimerVersionMetadata,bool) {
	for _,v:=range s.Versions {if v.ID==id{return v,true}}
	return MockTimerVersionMetadata{},false
}

func candidateMatches(v MockTimerVersionMetadata, p hostbridge.MockTimerBindingReview) bool {
	return v.ID!=p.PreviousVersionID && v.Tag==p.Tag && v.Message==p.Message &&
		v.ArtifactsSHA256==p.ArtifactsSHA256 && v.ConfigSHA256==p.ConfigSHA256
}

func previousMatches(s MockTimerVersionSnapshot,p hostbridge.MockTimerBindingReview) bool {
	v,ok:=findVersion(s,p.PreviousVersionID)
	return versionSnapshotValid(s)&&s.ActiveVersionID==p.PreviousVersionID&&ok&&
		v.Tag==p.PreviousTag&&v.Message==p.PreviousMessage&&v.ArtifactsSHA256==p.PreviousArtifactsSHA256&&v.ConfigSHA256==p.ConfigSHA256
}

func stageProof(p hostbridge.MockTimerBindingReview,v MockTimerVersionMetadata) string {
	sum:=sha256.Sum256([]byte(strings.Join([]string{"mocktimer-inactive-version/v1",p.PlanDigest,
		p.PreviousVersionID,v.ID,v.ArtifactsSHA256,v.ConfigSHA256}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (e *MockTimerVersionExecutor) execute(ctx context.Context,req Request,store journal.Store,
	fence func(context.Context)error)(journal.Observation,bool,error){
	no:=func(code string)(journal.Observation,bool,error){return journal.Observation{},false,fenceRejection(code)}
	unknown:=func()(journal.Observation,bool,error){return journal.Observation{},true,journal.ErrUnknownEffect}
	if !e.matches(req)||fence==nil{return no("MOCKTIMER_REGISTRATION_MISMATCH")}
	p:=e.upload
	// All preparation runs inside the existing journal writer lock.
	before,err:=e.port.Observe(ctx)
	if err!=nil||!previousMatches(before,p){return no("MOCKTIMER_PREVIOUS_UNVERIFIED")}
	if req.Operation==hostbridge.MockTimerUpload {
		for _,v:=range before.Versions{if candidateMatches(v,p){return no("MOCKTIMER_CANDIDATE_ALREADY_EXISTS")}}
		if len(before.Versions)>=100{return no("MOCKTIMER_VERSION_INVENTORY_FULL")}
		if err:=fence(ctx);err!=nil{return journal.Observation{},false,err}
		if err:=e.port.UploadInactive(ctx);err!=nil{return unknown()}
		after,err:=e.port.Observe(ctx)
		if err!=nil||!previousMatches(after,p){return unknown()}
		var added []MockTimerVersionMetadata
		for _,v:=range after.Versions{if _,ok:=findVersion(before,v.ID);!ok{added=append(added,v)}}
		if len(added)!=1||!candidateMatches(added[0],p){return unknown()}
		for _,v:=range before.Versions{if next,ok:=findVersion(after,v.ID);!ok||next!=v{return unknown()}}
		return journal.Observation{State:journal.Acknowledged,ProofDigest:stageProof(p,added[0])},true,nil
	}
	// The activation is a new signed mutation. It must independently observe
	// the exact staged version and the original durable upload acknowledgement.
	stage,err:=store.Status(p.MutationID)
	if err!=nil||stage.State!=journal.Acknowledged||stage.PlanDigest!=p.PlanDigest||
		stage.TargetDigest!=p.TargetDigest||stage.GrantDigest!=e.uploadGrant {
		return no("MOCKTIMER_UPLOAD_NOT_ACKNOWLEDGED")
	}
	var candidates []MockTimerVersionMetadata
	for _,v:=range before.Versions{if candidateMatches(v,p){candidates=append(candidates,v)}}
	if len(candidates)!=1||stage.ProofDigest!=stageProof(p,candidates[0]){
		return no("MOCKTIMER_STAGED_IDENTITY_UNVERIFIED")
	}
	v:=candidates[0]
	if err:=fence(ctx);err!=nil{return journal.Observation{},false,err}
	if err:=e.port.ActivateExact(ctx,v.ID);err!=nil{return unknown()}
	after,err:=e.port.Observe(ctx)
	if err!=nil||!versionSnapshotValid(after)||after.ActiveVersionID!=v.ID{return unknown()}
	if observed,ok:=findVersion(after,v.ID);!ok||observed!=v{return unknown()}
	sum:=sha256.Sum256([]byte("mocktimer-active-version/v1\x00"+p.PlanDigest+"\x00"+stage.ProofDigest))
	return journal.Observation{State:journal.Acknowledged,ProofDigest:hex.EncodeToString(sum[:])},true,nil
}

