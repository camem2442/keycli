package effectgateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// MaxEnvelopeBytes bounds a single submission.
const MaxEnvelopeBytes = 64 << 10

// Handler exposes the gateway over HTTP for the transport tier (portbridge
// exposes this loopback listener to the tailnet; it adds no authentication).
//
// The handler never reads a key from the request: authentication is the HMAC in
// the envelope, checked against a key the gateway issued.
func (g *Gateway) Handler(timeout time.Duration) http.Handler {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok","gateway":true}`+"\n")
	})
	mux.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			writeError(w, http.StatusUnsupportedMediaType, "content-type must be application/json")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, MaxEnvelopeBytes)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var env Envelope
		if err := decoder.Decode(&env); err != nil {
			writeError(w, http.StatusBadRequest, "invalid envelope")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		receipt, err := g.Submit(ctx, env)
		if err != nil {
			status, code := errorStatus(err)
			writeError(w, status, code)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(receipt)
	})
	return mux
}

// errorStatus maps gateway errors to a transport status and a stable code.
func errorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		return http.StatusUnauthorized, "UNAUTHENTICATED"
	case errors.Is(err, ErrExpired):
		return http.StatusUnauthorized, "EXPIRED"
	case errors.Is(err, ErrStaleGeneration):
		return http.StatusConflict, "STALE_GENERATION"
	case errors.Is(err, ErrSessionMismatch):
		return http.StatusConflict, "SESSION_MISMATCH"
	case errors.Is(err, ErrOwnerMismatch):
		return http.StatusConflict, "OWNER_MISMATCH"
	case errors.Is(err, ErrClaimNotActive):
		return http.StatusConflict, "CLAIM_NOT_ACTIVE"
	case errors.Is(err, ErrClaimLeaseExpired):
		return http.StatusConflict, "CLAIM_LEASE_EXPIRED"
	case errors.Is(err, ErrTargetPending):
		return http.StatusConflict, "TARGET_PENDING"
	case errors.Is(err, ErrMutationConflict):
		return http.StatusConflict, "MUTATION_CONFLICT"
	case errors.Is(err, ErrGatewayBusy):
		return http.StatusTooManyRequests, "GATEWAY_BUSY"
	case errors.Is(err, ErrAdmissionRejected):
		return http.StatusForbidden, "ADMISSION_REJECTED"
	case errors.Is(err, ErrReservationUnknown):
		return http.StatusServiceUnavailable, "RESERVATION_UNKNOWN"
	case errors.Is(err, ErrAuthorityUnavail):
		return http.StatusServiceUnavailable, "AUTHORITY_UNAVAILABLE"
	case errors.Is(err, ErrInvalidRequest):
		return http.StatusBadRequest, "INVALID_REQUEST"
	case errors.Is(err, ErrJournalUnavailable):
		return http.StatusServiceUnavailable, "JOURNAL_UNAVAILABLE"
	case errors.Is(err, ErrNotConfigured):
		return http.StatusServiceUnavailable, "GATEWAY_NOT_CONFIGURED"
	default:
		return http.StatusInternalServerError, "INTERNAL"
	}
}

func writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
