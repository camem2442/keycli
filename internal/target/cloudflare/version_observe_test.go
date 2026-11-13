package cloudflare

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

type metadataTransport func(*http.Request) (*http.Response, error)

func (f metadataTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, s string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(s)), Header: make(http.Header)}
}

const accountFixture = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const depFixture = "00000000-0000-4000-8000-000000000001"
const prevFixture = "11111111-1111-4111-8111-111111111111"
const newFixture = "22222222-2222-4222-8222-222222222222"

func fixtureObserver(t *testing.T, fail string) *VersionRESTObserver {
	t.Helper()
	o, e := newVersionRESTObserver("fixture-token", metadataTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api.cloudflare.com" || req.Method != "GET" || req.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Fatal("wrong remote read", req.URL.String())
		}
		switch {
		case strings.Contains(req.URL.Path, "/deployments"):
			if fail == "deploy" {
				return response(403, `{}`), nil
			}
			if fail == "traffic" {
				return response(200, `{"success":true,"result":{"deployments":[{"id":"`+depFixture+`","versions":[{"version_id":"`+prevFixture+`","percentage":50}]}]}}`), nil
			}
			return response(200, `{"success":true,"result":{"deployments":[{"id":"`+depFixture+`","versions":[{"version_id":"`+prevFixture+`","percentage":100}]}]}}`), nil
		case strings.HasSuffix(req.URL.Path, "/versions"):
			return response(200, `{"success":true,"result":{"items":[{"id":"`+newFixture+`"},{"id":"`+prevFixture+`"}]}}`), nil
		case strings.Contains(req.URL.Path, "/workers/workers/") && strings.Contains(req.URL.Path, "/versions/"):
			if fail == "beta-forbidden" {
				return response(403, `{}`), nil
			}
			if fail == "beta-wrong-id" {
				return response(200, `{"success":true,"result":{"id":"`+prevFixture+`","annotations":{"workers/tag":"kc-test1"}}}`), nil
			}
			if fail == "beta-no-tag" {
				return response(200, `{"success":true,"result":{"id":"`+newFixture+`"}}`), nil
			}
			return response(200, `{"success":true,"result":{"id":"`+newFixture+`","annotations":{"workers/tag":"kc-test1"}}}`), nil
		case strings.Contains(req.URL.Path, "/workers/scripts/") && strings.Contains(req.URL.Path, "/versions/"):
			return response(200, `{"success":true,"result":{"id":"`+newFixture+`","metadata":{"source":"wrangler"},"resources":{"bindings":[{"name":"OPENAI_API_KEY","type":"secret_text"}]}}}`), nil
		default:
			t.Fatalf("unplanned URL: %s", req.URL.String())
			return nil, nil
		}
	}))
	if e != nil {
		t.Fatal(e)
	}
	return o
}
func TestVersionObserveReadsDeploymentAndVersion(t *testing.T) {
	o := fixtureObserver(t, "")
	s, e := o.Observe(context.Background(), accountFixture, "fixture-worker")
	if e != nil || s.AccountID != accountFixture || s.Worker != "fixture-worker" || s.DeploymentID != depFixture || len(s.LatestVersionIDs) != 2 || s.Versions[0].Percentage != 100 {
		t.Fatalf("invalid observed state: %+v %v", s, e)
	}
	v, e := o.GetVersion(context.Background(), accountFixture, "fixture-worker", newFixture)
	if e != nil || v.ID != newFixture || v.Tag != "kc-test1" || len(v.BindingNames) != 1 || v.BindingNames[0] != "OPENAI_API_KEY" {
		t.Fatalf("invalid version: %+v %v", v, e)
	}
}
func TestVersionTagNeedsIndependentWorkerVersionAPIProof(t *testing.T) {
	for _, scenario := range []string{"beta-forbidden", "beta-wrong-id"} {
		t.Run(scenario, func(t *testing.T) {
			_, err := fixtureObserver(t, scenario).GetVersion(context.Background(), accountFixture, "fixture-worker", newFixture)
			if err == nil {
				t.Fatalf("accepted unverified Worker Version tag: %s", scenario)
			}
		})
	}
}
func TestGetVersionAcceptsIdentifiedVersionWithoutTag(t *testing.T) {
	version, err := fixtureObserver(t, "beta-no-tag").GetVersion(context.Background(), accountFixture, "fixture-worker", newFixture)
	if err != nil || version.ID != newFixture || version.Tag != "" {
		t.Fatalf("expected an identified untagged version, got %+v %v", version, err)
	}
}
func TestVersionObserverRejectsUntrustedScopeAndTraffic(t *testing.T) {
	if _, e := fixtureObserver(t, "").Observe(context.Background(), "bad", "fixture-worker"); e == nil {
		t.Fatal("accepted invalid account")
	}
	if _, e := fixtureObserver(t, "traffic").Observe(context.Background(), accountFixture, "fixture-worker"); e == nil {
		t.Fatal("accepted invalid traffic")
	}
	if _, e := fixtureObserver(t, "deploy").Observe(context.Background(), accountFixture, "fixture-worker"); e == nil {
		t.Fatal("accepted provider error")
	}
	if _, e := newVersionRESTObserver("bad\nsecret", metadataTransport(func(*http.Request) (*http.Response, error) { return response(200, `{}`), nil })); e == nil {
		t.Fatal("accepted header injection")
	}
}
func TestVersionObserverRejectsMalformedAndOversizedProviderResponse(t *testing.T) {
	for _, reply := range []string{`{"success":false,"result":[]}`, `{"success":true,"result":null}`, `{"success":true,"result":"wrong"}`, `{"success":true,"result":[{"id":"11111111-1111-4111-8111-111111111111"}]}`, `{garbage`, string(mustJSON(strings.Repeat("x", 1<<20+1)))} {
		o, e := newVersionRESTObserver("fixture", metadataTransport(func(*http.Request) (*http.Response, error) { return response(200, reply), nil }))
		if e != nil {
			t.Fatal(e)
		}
		if _, e := o.Observe(context.Background(), accountFixture, "fixture-worker"); e == nil {
			t.Fatal("accepted malformed provider response")
		}
	}
}
func mustJSON(s string) []byte { out, _ := json.Marshal(s); return out }
func TestStageVersionTagIsPinnedToArgument(t *testing.T) {
	req, argv, _ := testStage(t, "")
	req.VersionTag = "kc-op-33"
	outcome, err := StageVersionSecret(context.Background(), req, func(context.Context) ([]byte, error) { return []byte("fixture"), nil })
	if err != nil || outcome.State != VersionAcknowledgedUnverified {
		t.Fatal(err)
	}
	data, e := os.ReadFile(argv)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(data), "--tag kc-op-33") {
		t.Fatal("tag absent")
	}
	req.VersionTag = "unsafe;rm"
	if validVersionStageRequest(req) {
		t.Fatal("unsafe tag accepted")
	}
}
