package oauth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)

func bearer(secret string, expiresIn time.Duration, refresh string) Tokens {
	return Tokens{
		AccessToken:  secret,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresAt:    t0.Add(expiresIn),
		Scope:        "usage:read",
	}
}

func TestPlanUsesFreshAccessToken(t *testing.T) {
	action, err := Plan(bearer("at-1", time.Hour, "rt-1"), t0, Policy{Skew: time.Minute})
	if err != nil || action != UseAccessToken {
		t.Fatalf("want UseAccessToken, got %v %v", action, err)
	}
}

func TestPlanRefreshesWithinSkew(t *testing.T) {
	// 30s of life left, 1m skew: too close to send.
	action, err := Plan(bearer("at-1", 30*time.Second, "rt-1"), t0, Policy{Skew: time.Minute})
	if err != nil || action != Refresh {
		t.Fatalf("want Refresh, got %v %v", action, err)
	}
	// Same token with 2m of life is fine.
	action, err = Plan(bearer("at-2", 2*time.Minute, "rt-1"), t0, Policy{Skew: time.Minute})
	if err != nil || action != UseAccessToken {
		t.Fatalf("want UseAccessToken, got %v %v", action, err)
	}
}

func TestPlanExpiredWithoutRefreshReauthorizes(t *testing.T) {
	action, err := Plan(bearer("at-1", -time.Hour, ""), t0, Policy{})
	if err != nil || action != Reauthorize {
		t.Fatalf("want Reauthorize, got %v %v", action, err)
	}
}

func TestPlanRefreshTokenOnlyAsksForRefresh(t *testing.T) {
	action, err := Plan(Tokens{RefreshToken: "rt-1"}, t0, Policy{})
	if err != nil || action != Refresh {
		t.Fatalf("want Refresh, got %v %v", action, err)
	}
}

func TestPlanRefusesAmbiguousTokenSets(t *testing.T) {
	cases := map[string]Tokens{
		"zero expiry":      {AccessToken: "at", ExpiresAt: time.Time{}, RefreshToken: "rt"},
		"whitespace token": {AccessToken: "at 1", ExpiresAt: t0.Add(time.Hour)},
		"non bearer type":  {AccessToken: "at", TokenType: "MAC", ExpiresAt: t0.Add(time.Hour)},
		"empty everything": {},
	}
	for name, tokens := range cases {
		t.Run(name, func(t *testing.T) {
			action, err := Plan(tokens, t0, Policy{})
			if name == "empty everything" {
				if err != nil || action != Reauthorize {
					t.Fatalf("empty set is a plain reauthorize, got %v %v", action, err)
				}
				return
			}
			if !errors.Is(err, ErrTokenSet) {
				t.Fatalf("want ErrTokenSet, got %v", err)
			}
			if action != Reauthorize {
				t.Fatalf("want Reauthorize, got %v", action)
			}
		})
	}
}

func TestPlanAcceptsCaseInsensitiveBearer(t *testing.T) {
	tokens := bearer("at-1", time.Hour, "rt-1")
	for _, kind := range []string{"bearer", "BEARER", " Bearer "} {
		tokens.TokenType = kind
		if action, err := Plan(tokens, t0, Policy{}); err != nil || action != UseAccessToken {
			t.Fatalf("%q should be accepted, got %v %v", kind, action, err)
		}
	}
}

func TestApplyRefreshRotates(t *testing.T) {
	previous := bearer("at-1", time.Hour, "rt-1")
	next := bearer("at-2", 2*time.Hour, "rt-2")
	adopted, rotation, err := ApplyRefresh(previous, next, t0, Policy{Skew: time.Minute})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if rotation != RotationReplaced || adopted.RefreshToken != "rt-2" {
		t.Fatalf("want rotation to rt-2, got %v %q", rotation, adopted.RefreshToken)
	}
	if adopted.Scope != previous.Scope {
		t.Fatalf("scope must be preserved, got %q", adopted.Scope)
	}
}

func TestApplyRefreshKeepsRefreshWhenServerOmitsIt(t *testing.T) {
	previous := bearer("at-1", time.Hour, "rt-1")
	next := bearer("at-2", 2*time.Hour, "")
	adopted, rotation, err := ApplyRefresh(previous, next, t0, Policy{})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if rotation != RotationNone || adopted.RefreshToken != "rt-1" {
		t.Fatalf("want the old refresh token kept, got %v %q", rotation, adopted.RefreshToken)
	}
}

func TestApplyRefreshDoesNotInventRotation(t *testing.T) {
	previous := bearer("at-1", time.Hour, "rt-1")
	next := bearer("at-2", 2*time.Hour, "rt-1") // same refresh token returned
	_, rotation, err := ApplyRefresh(previous, next, t0, Policy{})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if rotation != RotationNone {
		t.Fatalf("an unchanged refresh token is not rotation, got %v", rotation)
	}
}

func TestApplyRefreshRejectsWidenedScope(t *testing.T) {
	previous := bearer("at-1", time.Hour, "rt-1")
	next := bearer("at-2", 2*time.Hour, "rt-2")
	next.Scope = "usage:read admin:write"
	if _, _, err := ApplyRefresh(previous, next, t0, Policy{}); !errors.Is(err, ErrTokenSet) {
		t.Fatalf("want ErrTokenSet for widened scope, got %v", err)
	}
}

func TestApplyRefreshRejectsUnusableResponses(t *testing.T) {
	previous := bearer("at-1", time.Hour, "rt-1")
	cases := map[string]Tokens{
		"no access token": {RefreshToken: "rt-2", ExpiresAt: t0.Add(time.Hour)},
		"already expired": {AccessToken: "at-2", RefreshToken: "rt-2", ExpiresAt: t0.Add(-time.Second)},
		"zero expiry":     {AccessToken: "at-2", RefreshToken: "rt-2"},
		"non bearer":      {AccessToken: "at-2", RefreshToken: "rt-2", TokenType: "basic", ExpiresAt: t0.Add(time.Hour)},
		"space in token":  {AccessToken: "at 2", RefreshToken: "rt-2", ExpiresAt: t0.Add(time.Hour)},
	}
	for name, next := range cases {
		t.Run(name, func(t *testing.T) {
			adopted, rotation, err := ApplyRefresh(previous, next, t0, Policy{})
			if !errors.Is(err, ErrTokenSet) {
				t.Fatalf("want ErrTokenSet, got %v", err)
			}
			if rotation != RotationNone || adopted.AccessToken != previous.AccessToken {
				t.Fatalf("a refused refresh must not change the stored set")
			}
		})
	}
}

func TestPlanLogoutRevokesOnlyWithRefreshToken(t *testing.T) {
	if plan := PlanLogout(bearer("at-1", time.Hour, "rt-1")); !plan.RevokeRefresh || !plan.ClearStored {
		t.Fatalf("refresh token present must revoke and clear: %+v", plan)
	}
	if plan := PlanLogout(bearer("at-1", time.Hour, "")); plan.RevokeRefresh || !plan.ClearStored {
		t.Fatalf("access-token-only sign-out must clear without revoking: %+v", plan)
	}
}

func TestTokensNeverFormatSecrets(t *testing.T) {
	tokens := bearer("super-secret-access", time.Hour, "super-secret-refresh")
	for _, rendered := range []string{
		tokens.String(), tokens.GoString(), strings.Join([]string{tokens.String(), tokens.GoString()}, " "),
	} {
		if strings.Contains(rendered, "super-secret") {
			t.Fatalf("token value leaked through formatting: %q", rendered)
		}
	}
}
