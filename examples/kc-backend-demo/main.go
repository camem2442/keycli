// Demo kc-backend plugin: JSON file store under ~/.kc/plugin-data/<backend>/.
// Build: go build -o kc-backend-demo ./examples/kc-backend-demo
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const protocolVersion = 1

type request struct {
	V       int    `json:"v"`
	Op      string `json:"op"`
	Name    string `json:"name"`
	Backend string `json:"backend"`

	ValueBase64 string  `json:"value_base64"`
	Note        string  `json:"note"`
	Expires     *string `json:"expires"`
}

type response struct {
	V   int    `json:"v"`
	OK  bool   `json:"ok"`
	Err string `json:"error,omitempty"`

	Protocol     int          `json:"protocol,omitempty"`
	Capabilities capabilities `json:"capabilities,omitempty"`

	ValueBase64 string `json:"value_base64,omitempty"`
	Items       []item `json:"items,omitempty"`
	Errno       string `json:"errno,omitempty"`
}

type capabilities struct {
	CanWrite     bool `json:"can_write"`
	CanList      bool `json:"can_list"`
	Interactive  bool `json:"interactive"`
	NeedsNetwork bool `json:"needs_network"`
}

type item struct {
	Name    string  `json:"name"`
	Note    string  `json:"note,omitempty"`
	Expires *string `json:"expires,omitempty"`
}

type record struct {
	ValueBase64 string  `json:"value_base64"`
	Note        string  `json:"note,omitempty"`
	Expires     *string `json:"expires,omitempty"`
}

type store map[string]record

func main() {
	os.Exit(run())
}

func run() int {
	line, err := readLine(os.Stdin)
	if err != nil {
		fail("invalid", err.Error())
		return 1
	}
	var req request
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		fail("invalid", "bad request json")
		return 1
	}
	if req.V != protocolVersion {
		fail("invalid", "unsupported protocol version")
		return 1
	}
	resp, err := dispatch(req)
	if err != nil {
		errno := "internal"
		if errors.Is(err, errNotFound) {
			errno = "not_found"
		}
		emit(response{V: protocolVersion, OK: false, Err: err.Error(), Errno: errno})
		return 0
	}
	emit(resp)
	return 0
}

var errNotFound = errors.New("not found")

func dispatch(req request) (response, error) {
	switch req.Op {
	case "handshake":
		return response{
			V:        protocolVersion,
			OK:       true,
			Protocol: protocolVersion,
			Capabilities: capabilities{
				CanWrite: true, CanList: true,
			},
		}, nil
	case "list":
		st, err := loadStore(backendName(req))
		if err != nil {
			return response{}, err
		}
		var items []item
		for name, rec := range st {
			items = append(items, item{Name: name, Note: rec.Note, Expires: rec.Expires})
		}
		return response{V: protocolVersion, OK: true, Items: items}, nil
	case "get":
		st, err := loadStore(backendName(req))
		if err != nil {
			return response{}, err
		}
		rec, ok := st[req.Name]
		if !ok {
			return response{}, errNotFound
		}
		return response{V: protocolVersion, OK: true, ValueBase64: rec.ValueBase64}, nil
	case "set":
		st, err := loadStore(backendName(req))
		if err != nil {
			return response{}, err
		}
		if req.Name == "" {
			return response{}, fmt.Errorf("missing name")
		}
		st[req.Name] = record{ValueBase64: req.ValueBase64, Note: req.Note, Expires: req.Expires}
		if err := saveStore(backendName(req), st); err != nil {
			return response{}, err
		}
		return response{V: protocolVersion, OK: true}, nil
	case "delete":
		st, err := loadStore(backendName(req))
		if err != nil {
			return response{}, err
		}
		if _, ok := st[req.Name]; !ok {
			return response{}, errNotFound
		}
		delete(st, req.Name)
		if err := saveStore(backendName(req), st); err != nil {
			return response{}, err
		}
		return response{V: protocolVersion, OK: true}, nil
	default:
		return response{}, fmt.Errorf("unknown op %q", req.Op)
	}
}

func backendName(req request) string {
	if req.Backend != "" {
		return req.Backend
	}
	return "default"
}

func dataDir(backend string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if h := os.Getenv("HOME"); h != "" {
		home = h
	}
	if backend == "" {
		backend = "default"
	}
	dir := filepath.Join(home, ".kc", "plugin-data", backend)
	return dir, nil
}

func loadStore(backend string) (store, error) {
	dir, err := dataDir(backend)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "store.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return store{}, nil
		}
		return nil, err
	}
	var st store
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return st, nil
}

func saveStore(backend string, st store) error {
	dir, err := dataDir(backend)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, fmt.Sprintf(".store-%d.tmp", time.Now().UnixNano()))
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "store.json"))
}

func readLine(r interface{ Read([]byte) (int, error) }) (string, error) {
	var buf []byte
	for {
		b := make([]byte, 1)
		n, err := r.Read(b)
		if n > 0 {
			if b[0] == '\n' {
				break
			}
			buf = append(buf, b[0])
		}
		if err != nil {
			if len(buf) == 0 {
				return "", err
			}
			break
		}
	}
	return strings.TrimSpace(string(buf)), nil
}

func emit(resp response) {
	_ = json.NewEncoder(os.Stdout).Encode(resp)
}

func fail(errno, msg string) {
	emit(response{V: protocolVersion, OK: false, Err: msg, Errno: errno})
}
