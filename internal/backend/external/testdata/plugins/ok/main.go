package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
)

const v = 1

func main() {
	var req struct {
		V    int    `json:"v"`
		Op   string `json:"op"`
		Name string `json:"name"`
	}
	_ = json.NewDecoder(os.Stdin).Decode(&req)
	switch req.Op {
	case "handshake":
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"v": 1, "ok": true, "protocol": 1,
			"capabilities": map[string]bool{"can_write": true, "can_list": true},
		})
	case "get":
		if req.Name == "found" {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
				"v": 1, "ok": true, "value_base64": base64.StdEncoding.EncodeToString([]byte("sekrit")),
			})
			return
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"v": 1, "ok": false, "errno": "not_found", "error": "missing"})
	case "list":
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"v": 1, "ok": true, "items": []map[string]string{{"name": "found"}},
		})
	default:
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"v": 1, "ok": true})
	}
}
