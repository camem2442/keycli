// Package effectclient sends signed requests to the operational KeyCLI gateway.
package effectclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/effectgateway"
)

const Submit = "submit"

var (
	ErrTransport         = errors.New("gateway response unavailable")
	ErrSubmitUnconfirmed = errors.New("submit response unavailable and original mutation status could not be confirmed")
)

// Result is the outcome of a gateway exchange. Receipt is populated for a
// successful HTTP response with a validated receipt.
type Result struct {
	HTTPStatus     int
	Receipt        effectgateway.Receipt
	ErrorCode      string
	StatusFallback bool
}

// LoadOrigin reads the configured HTTPS origin from a protected file.
func LoadOrigin(path string) (url.URL, error) {
	invalid := errors.New("invalid protected gateway origin configuration")
	raw, err := effectgateway.ReadPrivateFile(path, 8<<10)
	if err != nil {
		return url.URL{}, invalid
	}
	var origin string
	var version int
	if !effectgateway.DecodePrivateConfig(raw, map[string]any{"origin": &origin, "v": &version}) || version != 1 {
		return url.URL{}, invalid
	}
	endpoint, err := url.Parse(origin)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" ||
		endpoint.Opaque != "" || endpoint.User != nil || endpoint.RawPath != "" ||
		(endpoint.Path != "" && endpoint.Path != "/") || endpoint.ForceQuery ||
		endpoint.RawQuery != "" || strings.ContainsAny(origin, "#%") || strings.HasSuffix(endpoint.Host, ":") {
		return url.URL{}, invalid
	}
	if port := endpoint.Port(); port != "" {
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil || number == 0 {
			return url.URL{}, invalid
		}
	}
	return url.URL{Scheme: endpoint.Scheme, Host: endpoint.Host}, nil
}

// NewHTTPClient uses the system trust store and connects directly to the
// configured gateway, with redirects disabled.
func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:       (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
			ForceAttemptHTTP2: true,
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// Execute sends one signed operation. After an ambiguous intermediary
// response or a connected submit with no usable response, it reads the same
// mutation's status once.
// The caller sets req.ExpiresAt. The fallback uses a fresh expiry from ttl.
func Execute(ctx context.Context, client *http.Client, origin url.URL, operation string, req effectgateway.Request, key []byte, ttl time.Duration) (Result, error) {
	var connected atomic.Bool
	var envelope any
	requestCtx := ctx
	switch operation {
	case Submit:
		envelope = effectgateway.Sign(req, key)
		requestCtx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
			GotConn: func(httptrace.GotConnInfo) { connected.Store(true) },
		})
	case effectgateway.ReadStatus, effectgateway.ReadReconcile:
		envelope = signRead(operation, req, key, req.ExpiresAt)
	default:
		return Result{}, errors.New("operation must be submit, status or reconcile")
	}
	result, err := send(requestCtx, client, origin, operation, req.MutationID, envelope)
	if err == nil {
		intermediaryFailure := result.HTTPStatus == http.StatusBadGateway ||
			result.HTTPStatus == http.StatusGatewayTimeout ||
			(result.HTTPStatus == http.StatusServiceUnavailable && result.ErrorCode == "")
		if operation != Submit || !intermediaryFailure {
			return result, nil
		}
	} else if operation != Submit || (!connected.Load() && result.HTTPStatus != http.StatusOK) {
		return result, ErrTransport
	}

	// Submission may have reached the provider. Only read the original mutation.
	result, err = send(ctx, client, origin, effectgateway.ReadStatus, req.MutationID,
		signRead(effectgateway.ReadStatus, req, key, time.Now().Add(ttl).UTC()))
	result.StatusFallback = true
	if err != nil || result.HTTPStatus != http.StatusOK {
		return result, ErrSubmitUnconfirmed
	}
	return result, nil
}

func signRead(operation string, req effectgateway.Request, key []byte, expires time.Time) effectgateway.ReadEnvelope {
	return effectgateway.SignRead(effectgateway.ReadRequest{
		GoalID: req.GoalID, MutationID: req.MutationID, Owner: req.Owner, Session: req.Session,
		Generation: req.Generation, Action: operation, ExpiresAt: expires,
	}, key)
}

func send(ctx context.Context, client *http.Client, origin url.URL, operation, mutation string, envelope any) (Result, error) {
	origin.Path = "/" + operation
	body, err := json.Marshal(envelope)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin.String(), bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.GetBody = nil
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	result := Result{HTTPStatus: resp.StatusCode}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode >= http.StatusBadRequest {
			result.ErrorCode = readErrorCode(resp.Body)
		}
		return result, nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, effectgateway.MaxEnvelopeBytes+1))
	if err != nil || len(raw) > effectgateway.MaxEnvelopeBytes {
		return result, errors.New("gateway receipt unavailable")
	}
	var wire struct {
		effectgateway.Receipt
		ProviderCalls *int `json:"provider_calls"`
	}
	if json.Unmarshal(raw, &wire) != nil || wire.ProviderCalls == nil {
		return result, errors.New("invalid gateway receipt")
	}
	wire.Receipt.ProviderCalls = *wire.ProviderCalls
	if wire.MutationID != mutation || !ValidReceipt(wire.Receipt, operation) {
		return result, errors.New("invalid gateway receipt")
	}
	result.Receipt = wire.Receipt
	return result, nil
}

// ValidReceipt checks the journal and effect fields expected from an operation.
func ValidReceipt(r effectgateway.Receipt, operation string) bool {
	if !journal.ValidOperationID(r.MutationID) || r.ProviderCalls < 0 || r.ProviderCalls > 1 ||
		(operation != Submit && r.ProviderCalls != 0) {
		return false
	}
	switch journal.State(r.OperationState) {
	case journal.Acknowledged, journal.ObservedApplied:
		return r.Status == effectgateway.StatusApplied && journal.ValidDigest(r.ProofDigest)
	case journal.ObservedNoEffect:
		return r.Status == effectgateway.StatusNoEffect && journal.ValidDigest(r.ProofDigest) && r.ProviderCalls == 0
	case journal.IntentWritten, journal.Dispatched, journal.UnknownEffect:
		return r.Status == effectgateway.StatusUnknown && r.ProofDigest == ""
	case "":
		return operation == Submit && r.Status == effectgateway.StatusDuplicate && r.ProofDigest == "" && r.ProviderCalls == 0
	default:
		return false
	}
}

// readErrorCode accepts only the fixed gateway error codes. Server-provided
// text remains in the response body.
func readErrorCode(body io.Reader) string {
	const maxErrorBytes = 1024
	raw, err := io.ReadAll(io.LimitReader(body, maxErrorBytes+1))
	if err != nil || len(raw) > maxErrorBytes {
		return ""
	}
	var code string
	if !effectgateway.DecodePrivateConfig(raw, map[string]any{"error": &code}) {
		return ""
	}
	switch code {
	case "METHOD_NOT_ALLOWED", "UNSUPPORTED_MEDIA_TYPE", "INVALID_ENVELOPE",
		"UNAUTHENTICATED", "EXPIRED", "STALE_GENERATION", "SESSION_MISMATCH",
		"OWNER_MISMATCH", "CLAIM_NOT_ACTIVE", "CLAIM_LEASE_EXPIRED", "TARGET_PENDING",
		"MUTATION_CONFLICT", "GATEWAY_BUSY", "ADMISSION_REJECTED", "RESERVATION_UNKNOWN",
		"AUTHORITY_UNAVAILABLE", "INVALID_REQUEST", "JOURNAL_UNAVAILABLE",
		"OPERATION_UNAVAILABLE", "PROVIDER_LOCKED", "GATEWAY_NOT_CONFIGURED", "INTERNAL":
		return code
	default:
		return ""
	}
}
