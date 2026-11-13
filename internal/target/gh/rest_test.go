package gh

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/nacl/box"
)

type fakeRESTTransport func(*http.Request) (*http.Response, error)

func (f fakeRESTTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func fakeRESTResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

const (
	restTokenFixture = "fixture-token-do-not-echo"
	restRepoFixture  = `{"id":11,"full_name":"example/fixture","owner":{"id":22,"login":"example"}}`
	restEnvFixture   = `{"id":33,"name":"staging"}`
	restOrgFixture   = `{"id":22,"login":"example"}`
	restRepoPath     = "/repos/example/fixture"
	restEnvPath      = restRepoPath + "/environments/staging"
	restOrgPath      = "/orgs/example"
)

func TestPutSecretRESTExactScopeAndEncryptedBytes(t *testing.T) {
	public, private, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyReply := fmt.Sprintf(`{"key_id":"fixture-key","key":"%s"}`, base64.StdEncoding.EncodeToString(public[:]))
	fixture := []byte("synthetic bytes\x00\r\n")
	for _, tc := range []struct {
		name     string
		target   SecretRESTTarget
		root     string
		expected []string
		status   int
	}{
		{
			"repository",
			SecretRESTTarget{Scope: "repository", Repository: "example/fixture", Destination: "FIXTURE", RepositoryID: 11, RepositoryOwnerID: 22},
			restRepoPath + "/actions/secrets",
			[]string{"GET " + restRepoPath},
			http.StatusCreated,
		},
		{
			"environment",
			SecretRESTTarget{Scope: "environment", Repository: "example/fixture", Environment: "staging", Destination: "FIXTURE", RepositoryID: 11, RepositoryOwnerID: 22, EnvironmentID: 33},
			restEnvPath + "/secrets",
			[]string{"GET " + restRepoPath, "GET " + restEnvPath},
			http.StatusNoContent,
		},
		{
			"organization",
			SecretRESTTarget{Scope: "organization", Organization: "example", Destination: "FIXTURE", OrganizationID: 22, SelectedRepositoryIDs: []int64{44, 11}},
			restOrgPath + "/actions/secrets",
			[]string{"GET " + restOrgPath, "GET " + restOrgPath + "/repos?type=all&per_page=100&page=1"},
			http.StatusCreated,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests []string
			client, err := newSecretRESTClient(restTokenFixture, fakeRESTTransport(func(req *http.Request) (*http.Response, error) {
				if req.URL.Scheme != "https" || req.URL.Host != "api.github.com" ||
					req.Header.Get("Authorization") != "Bearer "+restTokenFixture ||
					req.Header.Get("X-GitHub-Api-Version") != githubRESTVersion {
					t.Fatal("request did not bind the fixed GitHub API identity")
				}
				requests = append(requests, req.Method+" "+req.URL.RequestURI())
				switch req.Method + " " + req.URL.Path {
				case "GET " + restRepoPath:
					return fakeRESTResponse(200, restRepoFixture), nil
				case "GET " + restEnvPath:
					return fakeRESTResponse(200, restEnvFixture), nil
				case "GET " + restOrgPath:
					return fakeRESTResponse(200, restOrgFixture), nil
				case "GET " + restOrgPath + "/repos":
					return fakeRESTResponse(200, `[`+restRepoFixture+`,{"id":44,"owner":{"id":22,"login":"example"}}]`), nil
				case "GET " + tc.root + "/public-key":
					return fakeRESTResponse(200, keyReply), nil
				case "PUT " + tc.root + "/FIXTURE":
					if req.GetBody != nil || req.Header.Get("Content-Type") != "application/json" {
						t.Fatal("write body is replayable or not JSON")
					}
					var payload map[string]json.RawMessage
					if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					var ciphertext, keyID string
					if err := json.Unmarshal(payload["encrypted_value"], &ciphertext); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal(payload["key_id"], &keyID); err != nil || keyID != "fixture-key" {
						t.Fatal("wrong provider key ID")
					}
					sealed, err := base64.StdEncoding.DecodeString(ciphertext)
					if err != nil {
						t.Fatal(err)
					}
					opened, ok := box.OpenAnonymous(nil, sealed, public, private)
					if !ok || !bytes.Equal(opened, fixture) {
						t.Fatal("sealed box did not preserve exact input bytes")
					}
					if tc.name == "organization" {
						if len(payload) != 4 || string(payload["visibility"]) != `"selected"` ||
							string(payload["selected_repository_ids"]) != "[11,44]" {
							t.Fatal("organization payload did not constrain visibility")
						}
					} else if len(payload) != 2 {
						t.Fatal("repository/environment payload contained foreign scope fields")
					}
					return fakeRESTResponse(tc.status, ""), nil
				default:
					t.Fatal("unexpected request")
					return nil, nil
				}
			}))
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := client.PutSecret(context.Background(), tc.target, fixture)
			if err != nil || outcome != SecretWriteAcknowledged {
				t.Fatalf("outcome %s, error %v", outcome, err)
			}
			want := append(tc.expected, "GET "+tc.root+"/public-key", "PUT "+tc.root+"/FIXTURE")
			if !reflect.DeepEqual(requests, want) {
				t.Fatalf("requests %v, want %v", requests, want)
			}
		})
	}
}

func TestPutSecretRESTFailsBeforeWriteOnWrongIdentity(t *testing.T) {
	target := SecretRESTTarget{Scope: "environment", Repository: "example/fixture", Environment: "staging", Destination: "FIXTURE", RepositoryID: 11, RepositoryOwnerID: 22, EnvironmentID: 33}
	org := SecretRESTTarget{Scope: "organization", Organization: "example", Destination: "FIXTURE", OrganizationID: 22, SelectedRepositoryIDs: []int64{11}}
	for _, tc := range []struct {
		name    string
		target  SecretRESTTarget
		replies map[string]string
	}{
		{"repository replaced", target, map[string]string{restRepoPath: strings.Replace(restRepoFixture, ":11", ":12", 1)}},
		{"repository transferred", target, map[string]string{restRepoPath: strings.Replace(restRepoFixture, ":22", ":23", 1)}},
		{"environment replaced", target, map[string]string{restRepoPath: restRepoFixture, restEnvPath: `{"id":34,"name":"staging"}`}},
		{"organization replaced", org, map[string]string{restOrgPath: `{"id":23,"login":"example"}`}},
		{"selected repo not visible", org, map[string]string{restOrgPath: restOrgFixture, restOrgPath + "/repos": "[]"}},
		{"selected repo moved", org, map[string]string{restOrgPath: restOrgFixture, restOrgPath + "/repos": `[{"id":11,"owner":{"id":23,"login":"another"}}]`}},
		{"mixed scope", SecretRESTTarget{Scope: "organization", Organization: "example", Destination: "FIXTURE", OrganizationID: 22, RepositoryID: 11, SelectedRepositoryIDs: []int64{11}}, nil},
		{"duplicate IDs", SecretRESTTarget{Scope: "organization", Organization: "example", Destination: "FIXTURE", OrganizationID: 22, SelectedRepositoryIDs: []int64{11, 11}}, nil},
		{"unsafe path", SecretRESTTarget{Scope: "repository", Repository: "example/..", Destination: "FIXTURE", RepositoryID: 11, RepositoryOwnerID: 22}, nil},
		{"low-order key", target, map[string]string{restRepoPath: restRepoFixture, restEnvPath: restEnvFixture, restEnvPath + "/secrets/public-key": `{"key_id":"fixture","key":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := newSecretRESTClient(restTokenFixture, fakeRESTTransport(func(req *http.Request) (*http.Response, error) {
				body, ok := tc.replies[req.URL.Path]
				if req.Method != http.MethodGet || !ok {
					t.Fatal("unexpected request")
				}
				return fakeRESTResponse(200, body), nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := client.PutSecret(context.Background(), tc.target, []byte("synthetic"))
			if err == nil || outcome != SecretNotWritten {
				t.Fatalf("outcome %s, error %v", outcome, err)
			}
		})
	}
}

func TestPutSecretRESTDoesNotFollowRedirectOrReplayUncertainWrite(t *testing.T) {
	pub, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path := restRepoPath + "/actions/secrets"
	key := fmt.Sprintf(`{"key_id":"fixture-key","key":"%s"}`, base64.RawStdEncoding.EncodeToString(pub[:]))
	target := SecretRESTTarget{Scope: "repository", Repository: "example/fixture", Destination: "FIXTURE", RepositoryID: 11, RepositoryOwnerID: 22}
	for _, kind := range []string{"redirect preflight", "redirect write", "transport timeout", "unexpected provider response"} {
		t.Run(kind, func(t *testing.T) {
			var writes int
			client, err := newSecretRESTClient(restTokenFixture, fakeRESTTransport(func(req *http.Request) (*http.Response, error) {
				redirect := func() *http.Response {
					response := fakeRESTResponse(302, restTokenFixture)
					response.Header.Set("Location", "https://example.invalid/never-send")
					return response
				}
				if req.URL.Host != "api.github.com" {
					t.Fatal("followed redirect")
				}
				switch req.Method + " " + req.URL.Path {
				case "GET " + restRepoPath:
					if kind == "redirect preflight" {
						return redirect(), nil
					}
					return fakeRESTResponse(200, restRepoFixture), nil
				case "GET " + path + "/public-key":
					return fakeRESTResponse(200, key), nil
				case "PUT " + path + "/FIXTURE":
					writes++
					switch kind {
					case "redirect write":
						return redirect(), nil
					case "transport timeout":
						return nil, errors.New(restTokenFixture)
					default:
						return fakeRESTResponse(500, restTokenFixture), nil
					}
				}
				t.Fatal("unexpected request")
				return nil, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := client.PutSecret(context.Background(), target, []byte("synthetic"))
			wantOutcome, wantWrites := SecretWriteUnknown, 1
			if kind == "redirect preflight" {
				wantOutcome, wantWrites = SecretNotWritten, 0
			}
			if err == nil || strings.Contains(err.Error(), restTokenFixture) || writes != wantWrites || outcome != wantOutcome {
				t.Fatalf("outcome %s, writes %d, error %v", outcome, writes, err)
			}
		})
	}
}

func TestPutSecretRESTFromLoadsAfterKeyAndClearsOwnedBytes(t *testing.T) {
	public, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path := restRepoPath + "/actions/secrets"
	key := fmt.Sprintf(`{"key_id":"fixture-key","key":"%s"}`, base64.StdEncoding.EncodeToString(public[:]))
	target := SecretRESTTarget{Scope: "repository", Repository: "example/fixture", Destination: "FIXTURE", RepositoryID: 11, RepositoryOwnerID: 22}
	for _, kind := range []string{"success", "loader error", "bad key"} {
		t.Run(kind, func(t *testing.T) {
			var calls []string
			client, err := newSecretRESTClient(restTokenFixture, fakeRESTTransport(func(req *http.Request) (*http.Response, error) {
				switch req.Method + " " + req.URL.Path {
				case "GET " + restRepoPath:
					calls = append(calls, "repository")
					return fakeRESTResponse(200, restRepoFixture), nil
				case "GET " + path + "/public-key":
					calls = append(calls, "public key")
					if kind == "bad key" {
						return fakeRESTResponse(200, "{}"), nil
					}
					return fakeRESTResponse(200, key), nil
				case "PUT " + path + "/FIXTURE":
					calls = append(calls, "write")
					return fakeRESTResponse(204, ""), nil
				}
				t.Fatal("unexpected request")
				return nil, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			owned := []byte("synthetic")
			outcome, err := client.PutSecretFrom(context.Background(), target, func(ctx context.Context) ([]byte, error) {
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("loader context lacked deadline")
				}
				calls = append(calls, "load")
				if kind == "loader error" {
					return owned, errors.New(restTokenFixture)
				}
				return owned, nil
			})
			want := []string{"repository", "public key"}
			if kind != "bad key" {
				want = append(want, "load")
				if !bytes.Equal(owned, make([]byte, len(owned))) {
					t.Fatal("adapter retained owned input")
				}
			}
			if kind == "success" {
				want = append(want, "write")
				if outcome != SecretWriteAcknowledged || err != nil {
					t.Fatalf("outcome %s, error %v", outcome, err)
				}
			} else if outcome != SecretNotWritten || err == nil || strings.Contains(err.Error(), restTokenFixture) {
				t.Fatalf("outcome %s, error %v", outcome, err)
			}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("calls %v, want %v", calls, want)
			}
		})
	}
}

func TestVerifyTargetRESTChecksAllPagesWithoutFollowingLink(t *testing.T) {
	repoList := make([]string, 0, 100)
	for i := 1; i <= 100; i++ {
		repoList = append(repoList, fmt.Sprintf(`{"id":%d,"owner":{"id":22,"login":"example"}}`, i))
	}
	target := SecretRESTTarget{Scope: "organization", Organization: "example", Destination: "FIXTURE", OrganizationID: 22, SelectedRepositoryIDs: []int64{1, 101}}
	var requests []string
	client, err := newSecretRESTClient(restTokenFixture, fakeRESTTransport(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.URL.RequestURI())
		if req.Method != "GET" || req.URL.Host != "api.github.com" {
			t.Fatal("read-only verification made an unexpected request")
		}
		switch req.URL.RequestURI() {
		case restOrgPath:
			return fakeRESTResponse(200, restOrgFixture), nil
		case restOrgPath + "/repos?type=all&per_page=100&page=1":
			response := fakeRESTResponse(200, "["+strings.Join(repoList, ",")+"]")
			response.Header.Set("Link", `<https://example.invalid/never-send>; rel="next"`)
			return response, nil
		case restOrgPath + "/repos?type=all&per_page=100&page=2":
			return fakeRESTResponse(200, `[{"id":101,"owner":{"id":22,"login":"example"}}]`), nil
		}
		t.Fatal("unexpected request")
		return nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.VerifyTarget(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 3 {
		t.Fatalf("read-only verification made %d requests", len(requests))
	}
}

func TestCreateRepositoryGenerationChecksFenceAtWriteBoundary(t *testing.T) {
	public, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyReply := fmt.Sprintf(`{"key_id":"fixture-key","key":"%s"}`, base64.StdEncoding.EncodeToString(public[:]))
	target := SecretRESTTarget{Scope: "repository", Repository: "example/fixture", Destination: "FIXTURE_GEN_2", RepositoryID: 11, RepositoryOwnerID: 22}
	const secretPath = restRepoPath + "/actions/secrets"
	for _, tc := range []struct {
		name string
		want SecretWriteOutcome
	}{
		{"created", SecretWriteAcknowledged},
		{"already exists", SecretNotWritten},
		{"appeared during preparation", SecretNotWritten},
		{"identity changed", SecretNotWritten},
		{"fence denied", SecretNotWritten},
		{"provider reply lost", SecretWriteUnknown},
		{"overwrote concurrent generation", SecretWriteUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			repoReads, secretReads := 0, 0
			client, err := newSecretRESTClient(restTokenFixture, fakeRESTTransport(func(req *http.Request) (*http.Response, error) {
				calls = append(calls, req.Method+" "+req.URL.Path)
				switch req.Method + " " + req.URL.Path {
				case "GET " + restRepoPath:
					repoReads++
					if repoReads == 2 && tc.name == "identity changed" {
						return fakeRESTResponse(http.StatusOK, strings.Replace(restRepoFixture, "11", "12", 1)), nil
					}
					return fakeRESTResponse(http.StatusOK, restRepoFixture), nil
				case "GET " + secretPath + "/public-key":
					return fakeRESTResponse(http.StatusOK, keyReply), nil
				case "GET " + secretPath + "/" + target.Destination:
					secretReads++
					if tc.name == "already exists" || tc.name == "appeared during preparation" && secretReads == 2 {
						return fakeRESTResponse(http.StatusOK, "{}"), nil
					}
					return fakeRESTResponse(http.StatusNotFound, "{}"), nil
				case "PUT " + secretPath + "/" + target.Destination:
					switch tc.name {
					case "provider reply lost":
						return nil, io.ErrUnexpectedEOF
					case "overwrote concurrent generation":
						return fakeRESTResponse(http.StatusNoContent, ""), nil
					default:
						return fakeRESTResponse(http.StatusCreated, ""), nil
					}
				default:
					t.Fatalf("unexpected request %s %s", req.Method, req.URL.Path)
					return nil, nil
				}
			}))
			if err != nil {
				t.Fatal(err)
			}
			value := []byte("test value")
			outcome, err := client.CreateRepositoryGenerationFrom(context.Background(), target,
				func(context.Context) ([]byte, error) {
					calls = append(calls, "load")
					return value, nil
				}, func(context.Context) error {
					calls = append(calls, "fence")
					if tc.name == "fence denied" {
						return io.ErrClosedPipe
					}
					return nil
				})
			if outcome != tc.want || (err != nil) != (tc.want != SecretWriteAcknowledged) {
				t.Fatalf("outcome=%s error=%v", outcome, err)
			}
			want := []string{"GET " + restRepoPath, "GET " + secretPath + "/public-key", "GET " + secretPath + "/" + target.Destination}
			if tc.name != "already exists" {
				want = append(want, "load", "GET "+restRepoPath)
			}
			if tc.name != "identity changed" && tc.name != "already exists" {
				want = append(want, "GET "+secretPath+"/"+target.Destination)
				if tc.name != "appeared during preparation" {
					want = append(want, "fence")
					if tc.name != "fence denied" {
						want = append(want, "PUT "+secretPath+"/"+target.Destination)
					}
				}
			}
			if !slices.Equal(calls, want) || bytes.Equal(value, make([]byte, len(value))) == (tc.name == "already exists") {
				t.Fatalf("calls=%v want=%v, value cleared=%t", calls, want, bytes.Equal(value, make([]byte, len(value))))
			}
		})
	}
}
