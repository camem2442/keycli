package providerlifecycle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

type cancelOnClose struct {
	io.Reader
	cancel context.CancelFunc
}

func (b cancelOnClose) Close() error {
	b.cancel()
	return nil
}

func TestCloudflareIssueKeepsValidatedResponseAfterCallerCancellation(t *testing.T) {
	account, group, token := strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 32)
	for _, tc := range []struct {
		name         string
		responseName string
		wantStored   bool
	}{
		{name: "valid acknowledgement", responseName: "kc-rot-fixture", wantStored: true},
		{name: "wrong token name", responseName: "kc-rot-other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			requestDeadline, _ := ctx.Deadline()
			sends, stores := 0, 0
			client := clientFixture(t, func(req *http.Request) (*http.Response, error) {
				sends++
				if deadline, ok := req.Context().Deadline(); !ok || !deadline.Equal(requestDeadline) || req.Context().Err() != nil {
					t.Fatal("provider request lost the caller's deadline")
				}
				body := fmt.Sprintf(`{"success":true,"result":{"id":%q,"name":%q,"value":"synthetic"}}`, token, tc.responseName)
				res := response(http.StatusCreated, "")
				res.Body = cancelOnClose{Reader: strings.NewReader(body), cancel: cancel}
				return res, nil
			})
			got, err := client.CreateCloudflareAccountToken(ctx, authorized, account, "kc-rot-fixture", group, time.Now().Add(time.Hour),
				func(storeCtx context.Context, value []byte) error {
					stores++
					if ctx.Err() == nil || storeCtx.Err() != nil {
						t.Fatal("storage was canceled with its caller")
					}
					deadline, ok := storeCtx.Deadline()
					if !ok || time.Until(deadline) > 5*time.Second || string(value) != "synthetic" {
						t.Fatal("storage did not receive the issued value with an independent deadline")
					}
					return nil
				})
			if sends != 1 {
				t.Fatalf("unexpected provider calls: %d", sends)
			}
			if tc.wantStored {
				if err != nil || stores != 1 || got.CredentialID != token {
					t.Fatalf("issued token was not stored: calls=%d err=%v", stores, err)
				}
			} else if err == nil || stores != 0 || got != (Receipt{}) {
				t.Fatal("an invalid acknowledgement reached storage")
			}
		})
	}
}

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

func TestCloudflareIssueRechecksExpiryAfterSigning(t *testing.T) {
	ctx := context.Background()
	account, group := strings.Repeat("a", 32), strings.Repeat("b", 32)
	boundary := time.Now().Truncate(time.Second).Add(time.Second)
	expiresAt := boundary.Add(5 * time.Minute)
	signed := false
	client, err := NewClient(func(*http.Request) error {
		signed = true
		time.Sleep(time.Until(boundary) + time.Millisecond)
		return nil
	}, roundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("provider called after the minimum expiry window closed")
		return nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	store := tokenStoreFixture{operations: make(map[string]string)}
	_, err = client.IssueCloudflareAccountToken(ctx, authorized, account, "kc-rot-fixture", group, expiresAt, "expiry-test", store)
	if !signed || err == nil {
		t.Fatalf("expiry was not checked after signing: signed=%t err=%v", signed, err)
	}
}
