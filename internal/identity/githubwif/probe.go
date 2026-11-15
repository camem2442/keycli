// Package githubwif checks the claim contract for GitHub Actions workloads.
package githubwif

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const (
	Issuer     = "https://token.actions.githubusercontent.com"
	probeEvent = "workflow_dispatch"
)

var errInvalidClaimFixture = errors.New("invalid synthetic GitHub claim fixture: expected exactly seven named string claims")

type Contract struct {
	Audience    string
	Repository  string
	Ref         string
	WorkflowRef string
	Environment string
}

// CompareSyntheticClaims compares a handcrafted projection to the contract.
// Matching fixture values establish neither provenance nor authentication.
func (p Contract) CompareSyntheticClaims(data []byte) error {
	expected := [...]struct{ key, value string }{
		{"iss", Issuer},
		{"aud", p.Audience},
		{"repository", p.Repository},
		{"ref", p.Ref},
		{"workflow_ref", p.WorkflowRef},
		{"environment", p.Environment},
		{"event_name", probeEvent},
	}
	actual, err := decodeClaimFixture(data)
	if err != nil || len(actual) != len(expected) {
		return errInvalidClaimFixture
	}
	var mismatches []string
	for _, claim := range expected {
		value, ok := actual[claim.key]
		if !ok {
			return errInvalidClaimFixture
		}
		if value != claim.value {
			mismatches = append(mismatches, claim.key)
		}
	}
	if len(mismatches) != 0 {
		return fmt.Errorf("synthetic GitHub claim mismatch: %s", strings.Join(mismatches, ", "))
	}
	return nil
}

func decodeClaimFixture(data []byte) (map[string]string, error) {
	if !utf8.Valid(data) {
		return nil, errInvalidClaimFixture
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errInvalidClaimFixture
	}
	claims := make(map[string]string, 7)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || len(claims) == 7 {
			return nil, errInvalidClaimFixture
		}
		if _, exists := claims[key]; exists {
			return nil, errInvalidClaimFixture
		}
		token, err = decoder.Token()
		value, ok := token.(string)
		if err != nil || !ok || value == "" || strings.ContainsRune(value, utf8.RuneError) {
			return nil, errInvalidClaimFixture
		}
		claims[key] = value
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') {
		return nil, errInvalidClaimFixture
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errInvalidClaimFixture
	}
	return claims, nil
}
