package cloudflare

// Immutable Worker code readback, separate from full Static Assets custody.
// Do not use annotation strings as evidence of executable bytes.
import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"slices"
	"sort"
	"strings"
)

const (
	mockTimerArtifactAccount = "a3f7ae5cc3a22033fa53964e935c413a"
	mockTimerArtifactProduction = "mocktimer-members-production"
	mockTimerArtifactRehearsal = "mocktimer-r4-rehearsal"
	maxArtifactResponse = 24 << 20
	maxArtifactModules = 128
)

var errArtifactRead = errors.New("Worker artifact evidence unavailable or invalid")

type WorkerModuleEvidence struct {
	Name string
	ContentType string
	Size int
	SHA256 string
}

// WorkerArtifactEvidence represents exactly one version-scoped code response.
// It does not contain source bytes, secret bindings, tokens or provider authors.
// Local dist/config digests are intentionally not synthesized from this result.
type WorkerArtifactEvidence struct {
	AccountID, Worker, VersionID string
	DeploymentID, ActiveVersionID string
	MainModule, ModuleInventorySHA256 string
	Modules []WorkerModuleEvidence
	TagPresent, MessagePresent bool
	StaticAssetInventoryVerified bool
	FullConfigVerified bool
	ProviderToArtifactBindingVerified bool
}

func validArtifactModuleName(name string) bool {
	return name != "" && len(name) <= 256 && path.Clean(name) == name &&
		name != "." && name != ".." && !strings.HasPrefix(name, "/") &&
		!strings.HasPrefix(name, "../") && !strings.ContainsAny(name, "\\\x00\r\n")
}

func moduleInventoryDigest(modules []WorkerModuleEvidence) (string, error) {
	if len(modules) == 0 || len(modules) > maxArtifactModules {
		return "", errArtifactRead
	}
	ordered := slices.Clone(modules)
	sort.Slice(ordered, func(i,j int) bool { return ordered[i].Name < ordered[j].Name })
	for i,v := range ordered {
		if !validArtifactModuleName(v.Name) || v.ContentType == "" ||
			len(v.ContentType) > 128 || strings.ContainsAny(v.ContentType, "\x00\r\n") ||
			!stageDigest.MatchString(v.SHA256) || v.Size < 0 ||
			(i>0 && ordered[i-1].Name == v.Name) {
			return "", errArtifactRead
		}
	}
	encoded,err:=json.Marshal(ordered)
	if err!=nil{return "",errArtifactRead}
	sum:=sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]),nil
}

// MatchOriginalWorkerModules compares code modules only against a host-held
// original bundle manifest. It is not an assertion about dist, the complete
// configuration, custody of the original manifest, or provider authority.
func MatchOriginalWorkerModules(observed WorkerArtifactEvidence, expected []WorkerModuleEvidence) bool {
	a,err:=moduleInventoryDigest(observed.Modules)
	b,err2:=moduleInventoryDigest(expected)
	return err==nil && err2==nil && a==observed.ModuleInventorySHA256 && a==b
}

// ObserveMockTimerArtifact reads the real immutable Worker Version endpoint
// with include=modules, bracketed by existing deployment/inventory reads.
// Both Production and R4 targets are hard-coded. It does not implement the
// write port or populate the full artifact/config digests required by Gateway.
func (o *VersionRESTObserver) ObserveMockTimerArtifact(ctx context.Context, worker, id string) (WorkerArtifactEvidence,error) {
	if o==nil || o.client==nil || ctx==nil || ctx.Err()!=nil ||
		(worker!=mockTimerArtifactProduction && worker!=mockTimerArtifactRehearsal) ||
		!versionUUID.MatchString(id) || strings.ToLower(id)!=id {
		return WorkerArtifactEvidence{},errArtifactRead
	}
	before,err:=o.Observe(ctx,mockTimerArtifactAccount,worker)
	if err!=nil || len(before.Versions)!=1 || before.Versions[0].Percentage!=100 ||
		!slices.Contains(before.LatestVersionIDs,id) {
		return WorkerArtifactEvidence{},errArtifactRead
	}
	var raw struct {
		ID string `json:"id"`
		MainModule string `json:"main_module"`
		Annotations map[string]string `json:"annotations"`
		Modules []struct {
			Name string `json:"name"`
			ContentType string `json:"content_type"`
			Content *string `json:"content_base64"`
		} `json:"modules"`
	}
	endpoint:="/accounts/"+mockTimerArtifactAccount+"/workers/workers/"+worker+"/versions/"+id+"?include=modules"
	if err:=o.getBounded(ctx,endpoint,&raw,maxArtifactResponse);err!=nil ||
		raw.ID!=id || !validArtifactModuleName(raw.MainModule) ||
		len(raw.Modules)==0 || len(raw.Modules)>maxArtifactModules {
		return WorkerArtifactEvidence{},errArtifactRead
	}
	modules:=make([]WorkerModuleEvidence,0,len(raw.Modules))
	mainFound:=false
	for _,module:=range raw.Modules {
		if !validArtifactModuleName(module.Name) || module.Content==nil ||
			module.ContentType=="" || module.ContentType=="application/source-map" ||
			strings.ContainsAny(*module.Content,"\r\n") {
			return WorkerArtifactEvidence{},errArtifactRead
		}
		decoded,err:=base64.StdEncoding.Strict().DecodeString(*module.Content)
		if err!=nil{return WorkerArtifactEvidence{},errArtifactRead}
		sum:=sha256.Sum256(decoded)
		size:=len(decoded)
		clearVersionBytes(decoded)
		if module.Name==raw.MainModule { mainFound=true }
		modules=append(modules,WorkerModuleEvidence{module.Name,module.ContentType,size,hex.EncodeToString(sum[:])})
	}
	digest,err:=moduleInventoryDigest(modules)
	if err!=nil || !mainFound {return WorkerArtifactEvidence{},errArtifactRead}
	after,err:=o.Observe(ctx,mockTimerArtifactAccount,worker)
	if err!=nil || before.DeploymentID!=after.DeploymentID ||
		!slices.Equal(before.Versions,after.Versions) || !slices.Equal(before.LatestVersionIDs,after.LatestVersionIDs) {
		return WorkerArtifactEvidence{},errArtifactRead
	}
	sort.Slice(modules,func(i,j int)bool{return modules[i].Name<modules[j].Name})
	return WorkerArtifactEvidence{AccountID:mockTimerArtifactAccount,Worker:worker,VersionID:id,
		DeploymentID:before.DeploymentID,ActiveVersionID:strings.ToLower(before.Versions[0].VersionID),
		MainModule:raw.MainModule,ModuleInventorySHA256:digest,Modules:modules,
		TagPresent:raw.Annotations["workers/tag"]!="",MessagePresent:raw.Annotations["workers/message"]!="",
		StaticAssetInventoryVerified:false,FullConfigVerified:false,ProviderToArtifactBindingVerified:false},nil
}
