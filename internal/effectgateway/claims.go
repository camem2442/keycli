package effectgateway

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// SQLiteCLIClaims reads the existing ClaimStore database in read-only mode.
//
// It deliberately does not re-implement the claim schema or transitions: the
// existing Python SqliteClaimStore remains the authority, and this adapter only
// projects one row into a ClaimSnapshot. Any error is returned to the caller so
// the gateway can fail closed.
type SQLiteCLIClaims struct {
	DBPath  string
	Binary  string        // defaults to "sqlite3"
	Timeout time.Duration // defaults to 3s
}

func (s SQLiteCLIClaims) Snapshot(ctx context.Context, goalID string) (ClaimSnapshot, error) {
	if !journalSafeID(goalID) {
		return ClaimSnapshot{}, errors.New("goal id is not safe for a read-only query")
	}
	binary := s.Binary
	if binary == "" {
		binary = "sqlite3"
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// -readonly opens the file without write intent. The WHERE clause uses an
	// already-validated identifier shape.
	query := fmt.Sprintf(
		"SELECT generation, state, owner, COALESCE(owner_session,'') FROM claims WHERE goal_id=%s;",
		sqlQuote(goalID),
	)
	cmd := exec.CommandContext(ctx, binary, "-readonly", "-noheader", "-separator", "|", s.DBPath, query)
	out, err := cmd.Output()
	if err != nil {
		return ClaimSnapshot{}, fmt.Errorf("claim store read failed: %w", err)
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return ClaimSnapshot{}, errors.New("claim not found")
	}
	parts := strings.Split(line, "|")
	if len(parts) != 4 {
		return ClaimSnapshot{}, errors.New("claim row shape is not recognised")
	}
	generation, convErr := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if convErr != nil || generation < 1 {
		return ClaimSnapshot{}, errors.New("claim generation is not a positive integer")
	}
	return ClaimSnapshot{
		GoalID:     goalID,
		Generation: generation,
		State:      strings.TrimSpace(parts[1]),
		Owner:      strings.TrimSpace(parts[2]),
		Session:    strings.TrimSpace(parts[3]),
	}, nil
}

func sqlQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// journalSafeID matches the identifier shape used for goal ids in this workspace.
func journalSafeID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for i, r := range value {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case (r == '.' || r == '_' || r == '-') && i > 0:
		default:
			return false
		}
	}
	return true
}
