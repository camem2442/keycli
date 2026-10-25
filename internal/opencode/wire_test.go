package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMergeConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".config", "cursor-vertex-proxy")
	_ = os.MkdirAll(cfgDir, 0o755)
	_ = os.WriteFile(filepath.Join(cfgDir, "config.env"), []byte("PROXY_PORT=9999\n"), 0o600)
	home := dir
	path := filepath.Join(dir, ".config", "opencode", "opencode.json")

	port, err := VertexProxyPort(home)
	if err != nil || port != 9999 {
		t.Fatalf("port: %v %d", err, port)
	}

	out, p, err := WireOpencodeVertex(home, path, false)
	if err != nil {
		t.Fatal(err)
	}
	if p != path {
		t.Fatalf("path %s", p)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	prov, ok := doc["provider"].(map[string]any)
	if !ok {
		t.Fatal("no provider")
	}
	vb, ok := prov[ProviderID].(map[string]any)
	if !ok {
		t.Fatal("no vertex-byok")
	}
	opts, _ := vb["options"].(map[string]any)
	if opts["baseURL"] != "http://127.0.0.1:9999/v1" {
		t.Fatalf("baseURL %v", opts["baseURL"])
	}
}
