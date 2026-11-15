package ociwif

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	testInstance    = "ocid1.instance.oc1.phx.aaaaexample"
	testCompartment = "ocid1.compartment.oc1..aaaacompartment"
	testTenancy     = "ocid1.tenancy.oc1..aaaatency"
	testDomain      = "ocid1.domain.oc1..aaadomain"
	testAudience    = "https://idcs-example.identity.oraclecloud.com"
)

var testNow = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

type fakeSigner struct {
	token SourceToken
	err   error
	calls int
}

func (f *fakeSigner) SubjectToken(context.Context) (SourceToken, error) {
	f.calls++
	return f.token, f.err
}

type fakeExchanger struct {
	response TokenResponse
	err      error
	calls    int
	last     ExchangeRequest
}

func (f *fakeExchanger) Exchange(_ context.Context, req ExchangeRequest) (TokenResponse, error) {
	f.calls++
	f.last = req
	return f.response, f.err
}

func claims() Claims {
	return Claims{
		Issuer:          Issuer,
		Audience:        []string{testAudience, "https://idcs-example.us-phoenix-1.identity.oraclecloud.com"},
		SubjectType:     SubjectTypeInstance,
		InstanceOCID:    testInstance,
		CompartmentOCID: testCompartment,
		DomainID:        testDomain,
		TenancyOCID:     testTenancy,
		IssuedAt:        testNow.Add(-5 * time.Minute),
		ExpiresAt:       testNow.Add(25 * time.Minute),
	}
}

func source() SourceToken {
	return SourceToken{Raw: "synthetic.subject.token", Claims: claims()}
}

func policy() Policy {
	return Policy{
		Issuer:            Issuer,
		Audience:          testAudience,
		TenancyOCID:       testTenancy,
		DomainID:          testDomain,
		Instances:         []string{testInstance},
		IdentityProvider:  "openai-provider-1",
		ServiceAccount:    "openai-service-account-1",
		MaxSourceLifetime: time.Hour,
		MaxTokenLifetime:  time.Hour,
		MinTokenLifetime:  time.Minute,
		Now:               func() time.Time { return testNow },
	}
}

func okResponse() TokenResponse {
	return TokenResponse{
		AccessToken: "synthetic-openai-access-token",
		TokenType:   "Bearer",
		ExpiresIn:   1800,
	}
}

func acquire(t *testing.T, mutate func(*Policy, *fakeSigner, *fakeExchanger)) (Credential, error, *fakeSigner, *fakeExchanger) {
	t.Helper()
	signer := &fakeSigner{token: source()}
	exchanger := &fakeExchanger{response: okResponse()}
	p := policy()
	if mutate != nil {
		mutate(&p, signer, exchanger)
	}
	credential, err := p.Acquire(context.Background(), signer, exchanger)
	return credential, err, signer, exchanger
}

func TestOCI15SuccessfulSyntheticExchange(t *testing.T) {
	credential, err, signer, exchanger := acquire(t, nil)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if signer.calls != 1 || exchanger.calls != 1 {
		t.Fatalf("signer=%d exchanger=%d", signer.calls, exchanger.calls)
	}
	if exchanger.last.IdentityProvider != "openai-provider-1" || exchanger.last.ServiceAccount != "openai-service-account-1" {
		t.Fatalf("mapping not forwarded: %+v", exchanger.last)
	}
	if exchanger.last.Audience != testAudience {
		t.Fatal("the issued Oracle audience must be forwarded unchanged")
	}
	if !credential.ExpiresAt().Equal(testNow.Add(25 * time.Minute)) {
		t.Fatalf("expiry = %s", credential.ExpiresAt())
	}

	req, _ := http.NewRequest(http.MethodPost, "https://api.openai.com/v1/responses", nil)
	if err := credential.Authorize(req); err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if req.Header.Get("Authorization") != "Bearer synthetic-openai-access-token" {
		t.Fatal("bearer token not attached")
	}

	for _, target := range []string{"https://api.openai.com.evil.example/v1", "http://api.openai.com/v1", "https://example.com/"} {
		bad, _ := http.NewRequest(http.MethodPost, target, nil)
		if err := credential.Authorize(bad); err == nil {
			t.Fatalf("%s must not be authorised", target)
		}
	}
}

func TestOCI01WrongIssuer(t *testing.T) {
	_, err, _, exchanger := acquire(t, func(_ *Policy, s *fakeSigner, _ *fakeExchanger) {
		s.token.Claims.Issuer = "https://identity.example.com/"
	})
	if !errors.Is(err, ErrWrongIssuer) {
		t.Fatalf("got %v", err)
	}
	if exchanger.calls != 0 {
		t.Fatal("no exchange may happen after a claim failure")
	}
}

func TestOCI02WrongAudience(t *testing.T) {
	_, err, _, _ := acquire(t, func(_ *Policy, s *fakeSigner, _ *fakeExchanger) {
		s.token.Claims.Audience = []string{"https://other.identity.oraclecloud.com"}
	})
	if !errors.Is(err, ErrWrongAudience) {
		t.Fatalf("got %v", err)
	}
}

func TestOCI03WrongTenancy(t *testing.T) {
	_, err, _, _ := acquire(t, func(_ *Policy, s *fakeSigner, _ *fakeExchanger) {
		s.token.Claims.TenancyOCID = "ocid1.tenancy.oc1..other"
	})
	if !errors.Is(err, ErrWrongTenancy) {
		t.Fatalf("got %v", err)
	}
}

func TestOCI04WrongInstance(t *testing.T) {
	_, err, _, _ := acquire(t, func(_ *Policy, s *fakeSigner, _ *fakeExchanger) {
		s.token.Claims.InstanceOCID = "ocid1.instance.oc1.phx.other"
	})
	if !errors.Is(err, ErrWrongInstance) {
		t.Fatalf("got %v", err)
	}
}

func TestOCI05WrongDomainOrCompartment(t *testing.T) {
	_, err, _, _ := acquire(t, func(_ *Policy, s *fakeSigner, _ *fakeExchanger) {
		s.token.Claims.DomainID = "ocid1.domain.oc1..other"
	})
	if !errors.Is(err, ErrWrongDomain) {
		t.Fatalf("domain: got %v", err)
	}
	_, err, _, _ = acquire(t, func(p *Policy, s *fakeSigner, _ *fakeExchanger) {
		p.Instances = nil
		p.Compartments = []string{testCompartment}
		s.token.Claims.CompartmentOCID = "ocid1.compartment.oc1..other"
	})
	if !errors.Is(err, ErrWrongCompartment) {
		t.Fatalf("compartment: got %v", err)
	}
}

func TestOCI06ExpiredSourceToken(t *testing.T) {
	_, err, _, _ := acquire(t, func(_ *Policy, s *fakeSigner, _ *fakeExchanger) {
		s.token.Claims.ExpiresAt = testNow.Add(-time.Second)
	})
	if !errors.Is(err, ErrSourceExpired) {
		t.Fatalf("got %v", err)
	}
}

func TestOCI07LifetimeAndExpiryInconsistency(t *testing.T) {
	_, err, _, _ := acquire(t, func(_ *Policy, s *fakeSigner, _ *fakeExchanger) {
		s.token.Claims.IssuedAt = testNow.Add(-2 * time.Hour)
	})
	if !errors.Is(err, ErrSourceLifetime) {
		t.Fatalf("source lifetime: got %v", err)
	}

	_, err, _, _ = acquire(t, func(_ *Policy, _ *fakeSigner, e *fakeExchanger) {
		e.response = TokenResponse{AccessToken: "synthetic", ExpiresAt: testNow.Add(-time.Minute).Unix(), ExpiresIn: 3600}
	})
	if !errors.Is(err, ErrExpiryInconsistency) {
		t.Fatalf("inconsistent response: got %v", err)
	}

	_, err, _, _ = acquire(t, func(_ *Policy, _ *fakeSigner, e *fakeExchanger) {
		e.response = TokenResponse{AccessToken: "synthetic", ExpiresIn: 7200}
	})
	if !errors.Is(err, ErrExpiryInconsistency) {
		t.Fatalf("too long: got %v", err)
	}

	_, err, _, _ = acquire(t, func(_ *Policy, _ *fakeSigner, e *fakeExchanger) {
		e.response = TokenResponse{AccessToken: "synthetic"}
	})
	if !errors.Is(err, ErrExpiryInconsistency) {
		t.Fatalf("missing expiry: got %v", err)
	}
}

func TestOCI08WrongOpenAIMapping(t *testing.T) {
	_, err, _, exchanger := acquire(t, func(p *Policy, _ *fakeSigner, _ *fakeExchanger) {
		p.ServiceAccount = ""
	})
	if !errors.Is(err, ErrMappingMismatch) {
		t.Fatalf("empty service account: got %v", err)
	}
	if exchanger.calls != 0 {
		t.Fatal("exchange must not run without a mapping")
	}

	_, err, _, _ = acquire(t, func(_ *Policy, _ *fakeSigner, e *fakeExchanger) {
		e.response = TokenResponse{AccessToken: "", ExpiresIn: 60}
	})
	if !errors.Is(err, ErrMappingMismatch) {
		t.Fatalf("empty token: got %v", err)
	}

	_, err, _, _ = acquire(t, func(_ *Policy, _ *fakeSigner, e *fakeExchanger) {
		e.response = TokenResponse{AccessToken: "has space", ExpiresIn: 60}
	})
	if !errors.Is(err, ErrMappingMismatch) {
		t.Fatalf("malformed token: got %v", err)
	}
}

func TestOCI09EndpointCannotBeMutated(t *testing.T) {
	if err := ValidateExchangeEndpoint(OpenAIExchangeEndpoint); err != nil {
		t.Fatalf("pinned endpoint must be accepted: %v", err)
	}
	for _, endpoint := range []string{
		"https://auth.openai.com/oauth/token/../evil",
		"http://auth.openai.com/oauth/token",
		"https://auth.openai.com.evil.example/oauth/token",
		"https://api.openai.com/v1",
		"",
	} {
		if err := ValidateExchangeEndpoint(endpoint); !errors.Is(err, ErrEndpointMutation) {
			t.Fatalf("%q must be refused, got %v", endpoint, err)
		}
	}
}

func TestOCI10NoSecretLeakInErrorsOrFormatting(t *testing.T) {
	secret := "synthetic-super-secret-token"
	s := source()
	s.Raw = secret
	exchanger := &fakeExchanger{response: okResponse()}
	p := policy()
	p.Instances = []string{"ocid1.instance.oc1.phx.other"}
	_, err := p.Acquire(context.Background(), &fakeSigner{token: s}, exchanger)
	if err == nil {
		t.Fatal("expected claim rejection")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("error leaked the subject token")
	}
	if rendered := fmt.Sprintf("%v %+v %s", s, s, &s); strings.Contains(rendered, secret) {
		t.Fatal("Format leaked the subject token")
	}
	if rendered := fmt.Sprintf("%v", okResponse()); strings.Contains(rendered, okResponse().AccessToken) {
		t.Fatal("Format leaked the access token")
	}
	credential, credErr := policy().Acquire(context.Background(), &fakeSigner{token: source()}, &fakeExchanger{response: okResponse()})
	if credErr != nil {
		t.Fatalf("acquire: %v", credErr)
	}
	if rendered := fmt.Sprintf("%v %+v", credential, credential); strings.Contains(rendered, "synthetic-openai-access-token") {
		t.Fatal("Format leaked the OpenAI token")
	}
}

func TestOCI11ExchangeTimeoutIsUnknown(t *testing.T) {
	_, err, _, exchanger := acquire(t, func(_ *Policy, _ *fakeSigner, e *fakeExchanger) {
		e.err = context.DeadlineExceeded
	})
	if !errors.Is(err, ErrExchangeUnknown) {
		t.Fatalf("got %v", err)
	}
	if exchanger.calls != 1 {
		t.Fatalf("exchange calls = %d", exchanger.calls)
	}
}

func TestOCI12NoAutomaticRetryAfterUnknownResult(t *testing.T) {
	signer := &fakeSigner{token: source()}
	exchanger := &fakeExchanger{err: errors.New("connection reset before response")}
	p := policy()
	for i := 0; i < 3; i++ {
		if _, err := p.Acquire(context.Background(), signer, exchanger); !errors.Is(err, ErrExchangeUnknown) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if exchanger.calls != 3 {
		t.Fatalf("each caller attempt is one exchange, got %d", exchanger.calls)
	}
	// The package itself never retries: one Acquire is one exchange call.
	single := &fakeExchanger{err: errors.New("timeout")}
	if _, err := p.Acquire(context.Background(), signer, single); !errors.Is(err, ErrExchangeUnknown) {
		t.Fatalf("got %v", err)
	}
	if single.calls != 1 {
		t.Fatalf("Acquire must call the exchanger exactly once, got %d", single.calls)
	}
}

func TestOCI13WrongSignerOrigin(t *testing.T) {
	for _, subject := range []string{"user", "client", "", "instance-principal"} {
		_, err, _, exchanger := acquire(t, func(_ *Policy, s *fakeSigner, _ *fakeExchanger) {
			s.token.Claims.SubjectType = subject
		})
		if !errors.Is(err, ErrWrongSubjectType) {
			t.Fatalf("%q: got %v", subject, err)
		}
		if exchanger.calls != 0 {
			t.Fatal("exchange must not run for a non-instance subject")
		}
	}
}

func TestOCI14CrossAccountIdentityMismatch(t *testing.T) {
	_, err, _, _ := acquire(t, func(p *Policy, s *fakeSigner, _ *fakeExchanger) {
		p.TenancyOCID = "ocid1.tenancy.oc1..different"
		s.token.Claims.TenancyOCID = testTenancy
	})
	if !errors.Is(err, ErrWrongTenancy) {
		t.Fatalf("got %v", err)
	}
}

func TestMissingComponentsAndUnavailableSigner(t *testing.T) {
	p := policy()
	if _, err := p.Acquire(context.Background(), nil, &fakeExchanger{}); !errors.Is(err, ErrMissingComponent) {
		t.Fatalf("nil signer: %v", err)
	}
	if _, err := p.Acquire(context.Background(), &fakeSigner{}, nil); !errors.Is(err, ErrMissingComponent) {
		t.Fatalf("nil exchanger: %v", err)
	}
	if _, err := p.Acquire(context.Background(), &fakeSigner{err: errors.New("metadata unreachable")}, &fakeExchanger{}); !errors.Is(err, ErrSourceUnavailable) {
		t.Fatalf("signer error: %v", err)
	}
	if _, err := p.Acquire(context.Background(), &fakeSigner{token: SourceToken{Raw: "", Claims: claims()}}, &fakeExchanger{}); !errors.Is(err, ErrSourceUnavailable) {
		t.Fatalf("empty token: %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Acquire(cancelled, &fakeSigner{token: source()}, &fakeExchanger{}); !errors.Is(err, ErrSourceUnavailable) {
		t.Fatalf("cancelled context: %v", err)
	}
}

func TestAcquireExpiryWithExchangeElapsed(t *testing.T) {
	for _, tt := range []struct {
		name     string
		in       int64
		absolute time.Duration
		elapsed  time.Duration
		source   time.Duration
		want     time.Duration
		failure  error
	}{
		{"relative from start", 120, 0, 30 * time.Second, time.Hour, 2 * time.Minute, nil},
		{"both with delayed issuance", 120, 3 * time.Minute, 61 * time.Second, time.Hour, 3 * time.Minute, nil},
		{"issuance after response", 120, 3 * time.Minute, 10 * time.Second, time.Hour, 0, ErrExpiryInconsistency},
		{"expiry bounded by source", 120, 0, time.Second, 90 * time.Second, 90 * time.Second, nil},
		{"minimum checked after source cap", 120, 0, 45 * time.Second, 90 * time.Second, 0, ErrExpiryInconsistency},
		{"minimum checked after exchange", 120, 0, 75 * time.Second, time.Hour, 0, ErrExpiryInconsistency},
		{"already expired", 120, 0, 121 * time.Second, time.Hour, 0, ErrExpiryInconsistency},
		{"source expired during exchange", 120, 0, 90 * time.Second, 90 * time.Second, 0, ErrSourceExpired},
		{"negative relative with absolute", -1, 3 * time.Minute, time.Second, time.Hour, 0, ErrExpiryInconsistency},
		{"duration overflow", (1 << 63) - 1, 0, time.Second, time.Hour, 0, ErrExpiryInconsistency},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cred, err, _, _ := acquire(t, func(p *Policy, s *fakeSigner, e *fakeExchanger) {
				s.token.Claims.ExpiresAt = testNow.Add(tt.source)
				p.MaxSourceLifetime = 2 * time.Hour
				p.Now = func() time.Time {
					if e.calls == 0 {
						return testNow
					}
					return testNow.Add(tt.elapsed)
				}
				e.response.ExpiresIn = tt.in
				if tt.absolute != 0 {
					e.response.ExpiresAt = testNow.Add(tt.absolute).Unix()
				}
			})
			if !errors.Is(err, tt.failure) {
				t.Fatalf("got %v, want %v", err, tt.failure)
			}
			if err == nil && !cred.ExpiresAt().Equal(testNow.Add(tt.want)) {
				t.Fatalf("expiry = %s, want %s", cred.ExpiresAt(), testNow.Add(tt.want))
			}
		})
	}
}

func TestAcquireRejectsMissingAndFutureSourceIssuance(t *testing.T) {
	for _, issued := range []time.Time{{}, testNow.Add(time.Second)} {
		_, err, _, exchange := acquire(t, func(_ *Policy, s *fakeSigner, _ *fakeExchanger) {
			s.token.Claims.IssuedAt = issued
		})
		if !errors.Is(err, ErrSourceLifetime) || exchange.calls != 0 {
			t.Fatalf("invalid issuance entered exchange: err=%v calls=%d", err, exchange.calls)
		}
	}
}
