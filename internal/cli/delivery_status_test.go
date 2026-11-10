package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/delivery/journal"
)

func TestDeliveryStatusDoesNotOpenApp(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	parent := filepath.Join(home, ".kc")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	store := journal.Store{Root: filepath.Join(parent, "delivery-journal")}
	intent := journal.Intent{OperationID: "fixture", TargetDigest: digest, PlanDigest: digest, GrantDigest: digest}
	if _, err := store.Dispatch(intent, func() (journal.Observation, error) {
		return journal.Observation{State: journal.Acknowledged, ProofDigest: digest}, nil
	}); err != nil {
		t.Fatal(err)
	}
	root := newRoot(func(context.Context) (*app.App, error) {
		t.Fatal("status opened the application graph")
		return nil, nil
	})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"delivery", "status", "--operation-id", "fixture", "--json"})
	var result journal.Record
	if runRoot(root) != 0 || json.Unmarshal(out.Bytes(), &result) != nil ||
		result.Intent != intent || result.State != journal.Acknowledged {
		t.Fatal("could not read local operation status")
	}
}
