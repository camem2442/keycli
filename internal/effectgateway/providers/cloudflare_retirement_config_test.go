package providers

import (
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/hostbridge"
	"github.com/camem2442/keycli/internal/rotation"
)

func TestRetirementEvidenceRequiresEnrolledSignerAndExactOperation(t *testing.T) {
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	registration := effectgateway.RegisteredOperation{
		Identity: hostbridge.Identity{OperationID: "rotation-retirement", MutationID: "revoke-old"},
		Provider: "cloudflare", ProviderOperation: cloudflareDeleteOperation,
		CanonicalTarget: strings.Repeat("a", 32) + "/tokens/" + strings.Repeat("b", 32),
		PlanDigest:      strings.Repeat("c", 64),
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "evidence.json")
	for _, tc := range []struct {
		name, operation, mutation string
		key                       ed25519.PublicKey
		want                      bool
	}{
		{"exact operation and mutation", "rotation-retirement", "revoke-old", public, true},
		{"unregistered operation", "rotation-other", "revoke-old", public, false},
		{"unregistered mutation", "rotation-retirement", "revoke-other", public, false},
		{"unenrolled signer", "rotation-retirement", "revoke-old", other, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{
				"operation_id": tc.operation, "mutation_id": tc.mutation, "plan_digest": registration.PlanDigest,
				"target_digest": registration.Binding().TargetDigest,
				"activation":    rotation.ActivationEvidence{}, "consumers": []rotation.ConsumerEvidence{},
			})
			if err != nil {
				t.Fatal(err)
			}
			signature := ed25519.Sign(private, append([]byte(retirementEvidenceDomain), payload...))
			envelope, err := json.Marshal(map[string][]byte{"payload": payload, "signature": signature})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, envelope, 0600); err != nil {
				t.Fatal(err)
			}
			_, err = readRetirementEvidence(path, tc.key, registration)
			if (err == nil) != tc.want {
				t.Fatalf("unexpected evidence result: %v", err)
			}
		})
	}
}
