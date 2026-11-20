package cloudflare

import (
 "bytes"
 "context"
 "fmt"
 "os"
 "strings"
 "testing"
)

func defaultStageFixture(t *testing.T) (VersionStageRequest, string, string) {
 t.Helper()
 req, argvPath, inputPath := testStage(t, "")
 req.UseDefaultEnvironment = true
 req.Environment = ""
 config := fmt.Sprintf("name = '%s'\naccount_id = '%s'\nmain = '../../src/gateway.js'\n", req.Worker, req.AccountID)
 if err := os.WriteFile(req.ConfigPath, []byte(config), 0600); err != nil { t.Fatal(err) }
 return req, argvPath, inputPath
}

func TestDefaultEnvironmentStageUsesExactPinnedWorkerWithoutEnvFlag(t *testing.T) {
 req, argv, input := defaultStageFixture(t)
 secret := []byte("synthetic-fixture-secret")
 state, err := StageVersionSecret(context.Background(), req,
  func(context.Context) ([]byte, error) { return secret, nil })
 if err != nil || state.State != VersionAcknowledgedUnverified { t.Fatalf("state=%s err=%v", state.State, err) }
 out, err := os.ReadFile(argv)
 if err != nil { t.Fatal(err) }
 want := "versions secret put "+req.SecretName+" --name "+req.Worker+" --config "+req.ConfigPath
 if strings.TrimSpace(string(out)) != want { t.Fatalf("unexpected CLI args: %q", out) }
 got, err := os.ReadFile(input)
 if err != nil || string(got)!="synthetic-fixture-secret" { t.Fatal("secret stdin did not match synthetic fixture") }
 if !bytes.Equal(secret, make([]byte, len(secret))) {t.Fatal("caller-owned secret buffer not cleared")}
}

func TestDefaultEnvironmentCannotStageWithConfigOrEnvironmentDrift(t *testing.T) {
 cases := []struct {
  name string
  mutate func(*VersionStageRequest)
  config string
 }{
  {name:"wrong-worker-name",config:"name = 'different-worker'\naccount_id = '%s'\n"},
  {name:"wrong-account",config:"name = '%s'\naccount_id = 'ffffffffffffffffffffffffffffffff'\n"},
  {name:"named-environment",mutate:func(r *VersionStageRequest){r.Environment="staging"}},
  {name:"unexpected-env-table",config:"name = '%s'\naccount_id = '%s'\n[env.staging]\n"},
  {name:"empty-default-without-opt-in",mutate:func(r *VersionStageRequest){r.UseDefaultEnvironment=false}},
 }
 for _, item := range cases {t.Run(item.name,func(t *testing.T){
  req, argv, _ := defaultStageFixture(t)
  if item.config!="" {
   value := strings.ReplaceAll(item.config,"%s",req.Worker)
   if item.name=="wrong-worker-name"||item.name=="unexpected-env-table"{
     value=strings.Replace(item.config,"%s",req.AccountID,1)
   }else if item.name=="wrong-account"{
     value=strings.Replace(item.config,"%s",req.Worker,1)
   }
   if err:=os.WriteFile(req.ConfigPath,[]byte(value),0600);err!=nil{t.Fatal(err)}
  }
  if item.mutate!=nil{item.mutate(&req)}
  called:=false
  result, err:=StageVersionSecret(context.Background(),req,func(context.Context)([]byte,error){
   called=true;return []byte("synthetic"),nil
  })
  if err==nil||result.State!=VersionNotAttempted||called{t.Fatalf("unexpected stage: state=%s err=%v load=%v",result.State,err,called)}
  if _,err=os.Stat(argv);!os.IsNotExist(err){t.Fatal("unexpected provider stage spawn")}
 })}
}

func TestDefaultEnvironmentPinnedStageRequiresGuard(t *testing.T){
 req, _, _:=defaultStageFixture(t)
 pin:=stageTestPin(t,req.WranglerPath)
 called:=0
 result,err:=StagePinnedVersionSecret(context.Background(),req,pin,
  func(context.Context)([]byte,error){return []byte("synthetic"),nil},
  func(context.Context)error{called++;return nil})
 if err!=nil||result.State!=VersionAcknowledgedUnverified||called!=1{t.Fatalf("result=%s err=%v guarded=%d",result.State,err,called)}
}
