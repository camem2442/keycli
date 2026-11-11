package providerlifecycle

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

type tokenStoreFixture struct {
	operations map[string]string
}

func (s tokenStoreFixture) Store(_ context.Context, operationID, tokenID string, value []byte) error {
	if len(value) == 0 {
		return errors.New("empty fixture credential")
	}
	s.operations[operationID] = tokenID
	return errors.New("fixture lost store acknowledgement")
}

func (s tokenStoreFixture) Lookup(_ context.Context, operationID string) (string, bool, error) {
	tokenID, found := s.operations[operationID]
	return tokenID, found, nil
}

func TestCloudflareIssueStoreRecoveryAndFinalAuthorization(t *testing.T) {
	ctx := context.Background()
	account, group, token := strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 32)
	const operationID = "issue-fixture"
	store := tokenStoreFixture{operations: make(map[string]string)}
	var steps []string
	client, err := NewClient(func(*http.Request) error {
		steps = append(steps, "sign")
		return nil
	}, roundTrip(func(*http.Request) (*http.Response, error) {
		steps = append(steps, "send")
		return response(http.StatusCreated, fmt.Sprintf(`{"success":true,"result":{"id":%q,"name":"kc-rot-fixture","value":"synthetic"}}`, token)), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	check := func(_ context.Context, provider, operation, target string) error {
		steps = append(steps, "authorize")
		if provider != "cloudflare" || operation != "create-account-token" || target != account+"/tokens/kc-rot-fixture/permission-group/"+group {
			t.Fatal("unexpected final authorization scope")
		}
		return nil
	}
	if got, err := client.IssueCloudflareAccountToken(ctx, check, account, "kc-rot-fixture", group, time.Now().Add(time.Hour), operationID, store); err == nil || got != (Receipt{}) {
		t.Fatal("lost store acknowledgement must leave issuance uncertain")
	}
	if !slices.Equal(steps, []string{"sign", "authorize", "send"}) {
		t.Fatalf("unexpected send order %v", steps)
	}
	if got, found, err := store.Lookup(ctx, operationID); err != nil || !found || got != token {
		t.Fatal("stored token ID could not be recovered from the operation")
	}
}
