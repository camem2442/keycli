package cursor_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/target"
	"github.com/camem2442/keycli/internal/target/cursor"
)

func TestPushPUT(t *testing.T) {
	var gotMethod, gotAuth, gotPath string
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		user, pass, ok := r.BasicAuth()
		if ok {
			gotAuth = user + ":" + pass
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"secv_x","name":"TOKEN","type":"runtime_secret"}`))
	}))
	defer srv.Close()

	opts := cursor.Options{
		EnvironmentID: "env-1",
		APIKey:        "test-key",
		APIBase:       srv.URL,
		HTTPClient:    srv.Client(),
		Repos:         []string{"github.com/acme/api"},
	}
	err := cursor.Push(context.Background(), opts, nil, config.Config{}, []target.Secret{
		{Name: "TOKEN", Value: []byte("secret-value")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("method %s", gotMethod)
	}
	if gotAuth != "test-key:" {
		t.Fatalf("auth %q", gotAuth)
	}
	if !strings.Contains(gotPath, "/v1/environments/env-1/secrets/TOKEN") {
		t.Fatalf("path %s", gotPath)
	}
	if body["value"] != "secret-value" {
		t.Fatalf("body value %v", body["value"])
	}
}

func TestPushDryRun(t *testing.T) {
	opts := cursor.Options{
		EnvironmentID: "env-1",
		APIKey:        "k",
		DryRun:        true,
		APIBase:       "http://127.0.0.1:1",
	}
	err := cursor.Push(context.Background(), opts, nil, config.Config{}, []target.Secret{
		{Name: "X", Value: []byte("y")},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateSize(t *testing.T) {
	opts := cursor.Options{
		EnvironmentID: "e",
		APIKey:        "k",
		APIBase:       "http://example.com",
	}
	err := cursor.Push(context.Background(), opts, nil, config.Config{}, []target.Secret{
		{Name: "empty", Value: []byte{}},
	})
	if err == nil {
		t.Fatal("expected error for empty value")
	}
}
