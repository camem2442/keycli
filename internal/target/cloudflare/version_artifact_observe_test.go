package cloudflare

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
)

func artifactObserverFixture(t *testing.T, scenario string) *VersionRESTObserver {
	t.Helper()
	deployments:=0
	o,err:=newVersionRESTObserver("fixture",metadataTransport(func(r *http.Request)(*http.Response,error){
		if r.Method!="GET" || r.URL.Host!="api.cloudflare.com" || !strings.Contains(r.URL.Path,mockTimerArtifactAccount+"/workers/") ||
			!strings.Contains(r.URL.Path,mockTimerArtifactRehearsal) {t.Fatal("unexpected target or effect")}
		switch {
		case strings.HasSuffix(r.URL.Path,"/deployments"):
			deployments++
			dep:=depFixture
			if scenario=="drift"&&deployments>1{dep=newFixture}
			return response(200,"{\"success\":true,\"result\":{\"deployments\":[{\"id\":\""+dep+"\",\"versions\":[{\"version_id\":\""+prevFixture+"\",\"percentage\":100}]}]}}"),nil
		case strings.HasSuffix(r.URL.Path,"/versions"):
			return response(200,"{\"success\":true,\"result\":{\"items\":[{\"id\":\""+newFixture+"\"},{\"id\":\""+prevFixture+"\"}]}}"),nil
		case strings.HasSuffix(r.URL.Path,"/versions/"+newFixture):
			if r.URL.Query().Get("include")!="modules"{t.Fatal("missing module request")}
			id,name,encoded:=newFixture,"index.js","YWJj"
			if scenario=="wrong-id"{id=prevFixture}
			if scenario=="unsafe-name"{name="../index.js"}
			if scenario=="invalid-base64"{encoded="%%"}
			entry:="{\"name\":\""+name+"\",\"content_type\":\"application/javascript+module\",\"content_base64\":\""+encoded+"\"}"
			if scenario=="duplicate"{entry+=","+entry}
			return response(200,"{\"success\":true,\"result\":{\"id\":\""+id+"\",\"main_module\":\"index.js\",\"modules\":["+entry+"]}}"),nil
		default:
			t.Fatal("unexpected endpoint");return nil,nil
		}
	}))
	if err!=nil{t.Fatal(err)}
	return o
}

func TestMockTimerArtifactReadsExactVersionCodeWithoutFullProof(t *testing.T){
	o:=artifactObserverFixture(t,"")
	v,err:=o.ObserveMockTimerArtifact(context.Background(),mockTimerArtifactRehearsal,newFixture)
	if err!=nil{t.Fatal(err)}
	sum:=sha256.Sum256([]byte("abc"))
	want:=[]WorkerModuleEvidence{{"index.js","application/javascript+module",3,hex.EncodeToString(sum[:])}}
	if v.VersionID!=newFixture||v.ActiveVersionID!=prevFixture||!MatchOriginalWorkerModules(v,want)||
		v.ProviderToArtifactBindingVerified||v.StaticAssetInventoryVerified||v.FullConfigVerified{
		t.Fatalf("unexpected evidence: %+v",v)
	}
	want[0].SHA256=strings.Repeat("0",64)
	if MatchOriginalWorkerModules(v,want){t.Fatal("accepted changed bytes")}
}
func TestMockTimerArtifactRejectsDriftMalformedAndUnboundTargets(t *testing.T){
	for _,scenario:=range []string{"drift","wrong-id","unsafe-name","invalid-base64","duplicate"}{
		t.Run(scenario,func(t *testing.T){
			if _,err:=artifactObserverFixture(t,scenario).ObserveMockTimerArtifact(context.Background(),mockTimerArtifactRehearsal,newFixture);err==nil{t.Fatal("accepted invalid evidence")}
		})
	}
	if _,err:=artifactObserverFixture(t,"").ObserveMockTimerArtifact(context.Background(),"unreviewed-worker",newFixture);err==nil{t.Fatal("accepted other worker")}
}
