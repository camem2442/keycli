package oauth

import (
	"encoding/json"
	"os"
	"testing"
)

// This is the same byte-identical test vector JSON as workspace-infra,
// ios/PortableAuth and context-platform. Go's pure ParseCallback does not
// implement one-shot consumption, expiration or token exchange; those remain
// caller-owned security gates.
func TestSharedNativeCallbackConformanceV1(t *testing.T) {
	blob, err := os.ReadFile("testdata/runtime-vectors-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Schema string `json:"schema"`
		Native struct {
			Issuer      string `json:"issuer"`
			RedirectURI string `json:"redirect_uri"`
			Resource    string `json:"resource"`
			State       string `json:"state"`
			Verifier    string `json:"verifier"`
			Cases       []struct {
				ID       string `json:"id"`
				Callback string `json:"callback"`
				Expected string `json:"expected"`
			} `json:"cases"`
		} `json:"native_client"`
	}
	if err := json.Unmarshal(blob, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Schema != "personal-auth-runtime-vectors/v1" || len(fixture.Native.Cases) != 12 {
		t.Fatal("unexpected shared vector schema or case count")
	}
	pending := Pending{
		State:        fixture.Native.State,
		CodeVerifier: fixture.Native.Verifier,
		RedirectURI:  fixture.Native.RedirectURI,
		Issuer:       fixture.Native.Issuer,
		Resource:     fixture.Native.Resource,
	}
	seen := make(map[string]bool)
	for _, item := range fixture.Native.Cases {
		if seen[item.ID] || item.ID == "" {
			t.Fatalf("duplicate/invalid shared callback vector ID")
		}
		seen[item.ID] = true
		t.Run(item.ID, func(t *testing.T) {
			code, err := ParseCallback(pending, item.Callback)
			switch item.Expected {
			case "ACCEPT":
				if err != nil || code != "proof" {
					t.Fatalf("expected callback admission, got %v", err)
				}
			case "REJECT":
				if err != ErrCallback || code != "" {
					t.Fatal("unsafe callback was accepted")
				}
			default:
				t.Fatal("unknown shared vector expectation")
			}
		})
	}
}
