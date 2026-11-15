package githubwif

import (
	"strings"
	"testing"
)

const matchingFixture = `{
	"iss": "https://token.actions.githubusercontent.com",
	"aud": "https://api.openai.com/v1",
	"repository": "example/fixture",
	"ref": "refs/heads/main",
	"workflow_ref": "example/fixture/.github/workflows/probe.yml@refs/heads/main",
	"environment": "ci",
	"event_name": "workflow_dispatch"
}`

func TestCompareSyntheticClaims(t *testing.T) {
	contract := Contract{
		Audience: "https://api.openai.com/v1", Repository: "example/fixture", Ref: "refs/heads/main",
		WorkflowRef: "example/fixture/.github/workflows/probe.yml@refs/heads/main", Environment: "ci",
	}
	if err := contract.CompareSyntheticClaims([]byte(matchingFixture)); err != nil {
		t.Fatalf("matching fixture: %v", err)
	}

	const marker = "fixture-marker-do-not-echo"
	mismatches := map[string]string{
		"iss":          `"iss": "https://token.actions.githubusercontent.com"`,
		"aud":          `"aud": "https://api.openai.com/v1"`,
		"repository":   `"repository": "example/fixture"`,
		"ref":          `"ref": "refs/heads/main"`,
		"workflow_ref": `"workflow_ref": "example/fixture/.github/workflows/probe.yml@refs/heads/main"`,
		"environment":  `"environment": "ci"`,
		"event_name":   `"event_name": "workflow_dispatch"`,
	}
	for field, original := range mismatches {
		t.Run(field, func(t *testing.T) {
			input := strings.Replace(matchingFixture, original, `"`+field+`": "`+marker+`"`, 1)
			err := contract.CompareSyntheticClaims([]byte(input))
			if err == nil || !strings.HasSuffix(err.Error(), ": "+field) || strings.Contains(err.Error(), marker) {
				t.Fatalf("expected only the mismatched field, got %v", err)
			}
		})
	}
	for name, input := range map[string]string{
		"unexpected event": strings.Replace(matchingFixture, "workflow_dispatch", "pull_request_target", 1),
		"duplicate key":    strings.Replace(matchingFixture, `"aud":`, `"aud":"`+marker+`","aud":`, 1),
		"unknown key":      strings.Replace(matchingFixture, `"environment":`, `"`+marker+`":`, 1),
		"missing key":      strings.Replace(matchingFixture, `"environment": "ci",`, "", 1),
		"case variant":     strings.Replace(matchingFixture, `"repository":`, `"Repository":`, 1),
		"array audience":   strings.Replace(matchingFixture, `"https://api.openai.com/v1"`, `["https://api.openai.com/v1"]`, 1),
		"null value":       strings.Replace(matchingFixture, `"environment": "ci"`, `"environment": null`, 1),
		"invalid Unicode":  strings.Replace(matchingFixture, `"environment": "ci"`, `"environment": "\ud800"`, 1),
		"trailing object":  matchingFixture + ` {}`,
		"compact input":    "synthetic.header.signature",
		"quoted compact":   `"synthetic.header.signature"`,
		"malformed JSON":   `{"iss":` + marker + `}`,
	} {
		t.Run(name, func(t *testing.T) {
			err := contract.CompareSyntheticClaims([]byte(input))
			if err == nil || strings.Contains(err.Error(), marker) {
				t.Fatalf("expected sanitized rejection, got %v", err)
			}
		})
	}
}
