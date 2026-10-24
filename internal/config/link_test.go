package config

import "testing"

func TestParseLinkURI(t *testing.T) {
	lt, err := ParseLinkURI("keychain:local.study.ai.groq/groq_api_key")
	if err != nil || lt.Service != "local.study.ai.groq" || lt.Account != "groq_api_key" {
		t.Fatalf("%+v %v", lt, err)
	}
	lt, err = ParseLinkURI("keychain:only-service")
	if err != nil || lt.Account != "" {
		t.Fatalf("%+v %v", lt, err)
	}
	if _, err := ParseLinkURI("file:x"); err == nil {
		t.Fatal("expected error")
	}
}
