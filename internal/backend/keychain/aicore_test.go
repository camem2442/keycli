package keychain_test

import (
	"context"
	"testing"

	"github.com/camem2442/keycli/internal/backend/keychain"
)

func TestParseAICoreName(t *testing.T) {
	n, ok := keychain.ParseAICoreName("groq#2")
	if !ok || n.Provider != "groq" || n.Slot != 2 {
		t.Fatalf("%+v %v", n, ok)
	}
	_, ok = keychain.ParseAICoreName("github")
	if ok {
		t.Fatal("github is not ai-core")
	}
}

func TestAICoreEnvFirst(t *testing.T) {
	t.Setenv("OPENAI_API_KEYS", "a,b")
	val, err := keychain.AICoreLookup(context.Background(), keychain.ExecRunner{}, keychain.AICoreName{Provider: "openai", Slot: 1})
	if err != nil || string(val) != "a" {
		t.Fatalf("got %q %v", val, err)
	}
}
