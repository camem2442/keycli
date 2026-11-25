package effectgateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/hostbridge"
)

type mockTimerPortFixture struct{
	snapshot MockTimerVersionSnapshot
	calls []string
	loseUpload bool
}
func(f *mockTimerPortFixture)Observe(context.Context)(MockTimerVersionSnapshot,error){
	out:=f.snapshot;out.Versions=append([]MockTimerVersionMetadata(nil),out.Versions...);return out,nil
}
func(f *mockTimerPortFixture)UploadInactive(context.Context)error{
	f.calls=append(f.calls,"upload")
	f.snapshot.Versions=append(f.snapshot.Versions,MockTimerVersionMetadata{
		ID:"22222222-2222-4222-8222-222222222222",Tag:strings.Repeat("a",12),
		Message:strings.Repeat("c",12)+" artifacts:"+strings.Repeat("e",12),
		ArtifactsSHA256:strings.Repeat("e",64),ConfigSHA256:strings.Repeat("f",64)})
	if f.loseUpload{return errors.New("fixture lost response")}
	return nil
}
func(f *mockTimerPortFixture)ActivateExact(_ context.Context,id string)error{
	f.calls=append(f.calls,"activate");f.snapshot.ActiveVersionID=id;return nil
}
func mockTimerFixture(t *testing.T)(*harness,*MockTimerVersionExecutor,*mockTimerPortFixture,Request,Request){
	t.Helper()
	raw,err:=json.Marshal(map[string]any{
		"schema":"mocktimer-worker-version-plan/v1","accountId":"a3f7ae5cc3a22033fa53964e935c413a",
		"workerName":"mocktimer-members-production","head":strings.Repeat("a",40),"previous":strings.Repeat("b",40),
		"sourceFingerprint":strings.Repeat("c",64),"previousFingerprint":strings.Repeat("d",64),
		"artifactsSha256":strings.Repeat("e",64),"configSha256":strings.Repeat("f",64),
		"previousArtifactsSha256":strings.Repeat("a",64),"previousVersionId":"11111111-1111-4111-8111-111111111111",
		"operationId":"mt-fixture","tag":strings.Repeat("a",12),"message":strings.Repeat("c",12)+" artifacts:"+strings.Repeat("e",12),
		"previousTag":strings.Repeat("b",12),"previousMessage":strings.Repeat("d",12)+" artifacts:"+strings.Repeat("a",12),
		"trafficPercent":100,"deployAuthorized":false,
		"planDigest":"171818d52b56a8d91ae3ddabe01a5f13dc1edf91aa02e3119ef3f669e337d539",
	})
	if err!=nil{t.Fatal(err)}
	f:=&mockTimerPortFixture{snapshot:MockTimerVersionSnapshot{AccountID:mockTimerVersionAccount,WorkerName:mockTimerVersionWorker,ActiveVersionID:"11111111-1111-4111-8111-111111111111",TrafficPercent:100,
		Versions:[]MockTimerVersionMetadata{{ID:"11111111-1111-4111-8111-111111111111",Tag:strings.Repeat("b",12),
			Message:strings.Repeat("d",12)+" artifacts:"+strings.Repeat("a",12),ArtifactsSHA256:strings.Repeat("a",64),ConfigSHA256:strings.Repeat("f",64)}}}}
	e,err:=NewMockTimerVersionExecutor(raw,testGrant,strings.Repeat("4",64),f);if err!=nil{t.Fatal(err)}
	h:=newHarness(t);h.gw.Provider=nil;h.gw.MockTimerVersions=e
	makeReq:=func(op string,b hostbridge.MockTimerBindingReview,grant string)Request{
		q:=testRequest();q.Action=hostbridge.ActionStage;q.Operation=op;q.MutationID=b.MutationID;q.TargetDigest=b.TargetDigest;q.PlanDigest=b.PlanDigest;q.GrantDigest=grant;return q
	}
	return h,e,f,makeReq(hostbridge.MockTimerUpload,e.upload,e.uploadGrant),makeReq(hostbridge.MockTimerActivate,e.activation,e.activationGrant)
}
func TestMockTimerVersionExecutorUsesRealGatewayJournal(t *testing.T){
	h,e,f,upload,activate:=mockTimerFixture(t)
	submit:=func(q Request)Receipt{r,err:=h.gw.Submit(context.Background(),Sign(q,[]byte(testKey)));if err!=nil{t.Fatal(err)};return r}
	if r:=submit(upload);r.Status!=StatusApplied||r.ProviderCalls!=1{t.Fatalf("upload: %+v",r)}
	if f.snapshot.ActiveVersionID!=e.upload.PreviousVersionID{t.Fatal("upload changed traffic")}
	if r:=submit(upload);r.Status!=StatusDuplicate||r.ProviderCalls!=0{t.Fatalf("duplicate: %+v",r)}
	// Reconstruct the typed executor; only durable journal and independent
	// observed metadata are needed for activation, not a process-local flag.
	copyExecutor:=*e;h.gw.MockTimerVersions=&copyExecutor
	if r:=submit(activate);r.Status!=StatusApplied||r.ProviderCalls!=1{t.Fatalf("activate: %+v",r)}
	if r:=submit(activate);r.Status!=StatusDuplicate||r.ProviderCalls!=0{t.Fatalf("duplicate: %+v",r)}
	if strings.Join(f.calls,",")!="upload,activate"{t.Fatal(f.calls)}
	h.gw.Operational=true
	if _,err:=h.gw.Submit(context.Background(),Sign(upload,[]byte(testKey)));!errors.Is(err,ErrNotConfigured){t.Fatalf("operational mode opened: %v",err)}
}
func TestMockTimerUnknownUploadCannotActivate(t *testing.T){
	h,_,f,upload,activate:=mockTimerFixture(t);f.loseUpload=true
	r,err:=h.gw.Submit(context.Background(),Sign(upload,[]byte(testKey)))
	if err!=nil||r.Status!=StatusUnknown{t.Fatalf("unknown: %+v %v",r,err)}
	r,err=h.gw.Submit(context.Background(),Sign(activate,[]byte(testKey)))
	if err!=nil||r.Status!=StatusNoEffect||r.ProviderCalls!=0||strings.Join(f.calls,",")!="upload"{t.Fatalf("activation: %+v %v %v",r,err,f.calls)}
}
func TestMockTimerWrongGrantAndUnacknowledgedStageCannotDispatch(t *testing.T){
	h,_,f,upload,activate:=mockTimerFixture(t)
	wrong:=upload;wrong.GrantDigest=activate.GrantDigest
	if _,err:=h.gw.Submit(context.Background(),Sign(wrong,[]byte(testKey)));!errors.Is(err,ErrAdmissionRejected){t.Fatalf("wrong grant: %v",err)}
	r,err:=h.gw.Submit(context.Background(),Sign(activate,[]byte(testKey)))
	if err!=nil||r.Status!=StatusNoEffect||len(f.calls)!=0{t.Fatalf("early activation: %+v %v",r,err)}
}

func TestMockTimerWrongObservedWorkerCannotDispatch(t *testing.T){
 h,_,f,upload,_:=mockTimerFixture(t)
 f.snapshot.WorkerName="mocktimer-r4-rehearsal"
 r,err:=h.gw.Submit(context.Background(),Sign(upload,[]byte(testKey)))
 if err!=nil||r.Status!=StatusNoEffect||len(f.calls)!=0{t.Fatalf("wrong target: %+v %v",r,err)}
}
