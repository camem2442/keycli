package keychain

import (
	"errors"
	"fmt"
	"testing"

	"github.com/camem2442/keycli/internal/kcerrors"
)

func TestMapSecurityExit(t *testing.T) {
	sandboxStderr := "security: SecKeychainSearchCreateFromAttributes: One or more parameters passed to a function were not valid.\n" +
		"security: SecKeychainSearchCopyNext: The specified item could not be found in the keychain.\n"

	cases := []struct {
		name     string
		err      error
		wantCode int
		wantSame bool
	}{
		{
			name:     "plain not found stays exit 2",
			err:      &SecurityError{ExitCode: 44, Stderr: "security: The specified item could not be found in the keychain.\n", Op: "find-generic-password"},
			wantCode: kcerrors.ExitNotFound,
		},
		{
			name:     "sandboxed not found maps to exit 3",
			err:      &SecurityError{ExitCode: 44, Stderr: sandboxStderr, Op: "find-generic-password"},
			wantCode: kcerrors.ExitBlocked,
		},
		{
			name:     "user interaction not allowed maps to exit 3",
			err:      &SecurityError{ExitCode: 51, Stderr: "security: User interaction is not allowed.", Op: "add-generic-password"},
			wantCode: kcerrors.ExitBlocked,
		},
		{
			name:     "generic sandbox failure maps to exit 3",
			err:      &SecurityError{ExitCode: 128, Stderr: "operation blocked by sandbox", Op: "find-generic-password"},
			wantCode: kcerrors.ExitBlocked,
		},
		{
			name:     "exit 1 with sandbox stderr maps to exit 3",
			err:      &SecurityError{ExitCode: 1, Stderr: "sandbox denied mach-lookup", Op: "find-generic-password"},
			wantCode: kcerrors.ExitBlocked,
		},
		{
			name:     "unknown exit code is preserved",
			err:      &SecurityError{ExitCode: 36, Stderr: "security: something else", Op: "add-generic-password"},
			wantCode: kcerrors.ExitError,
			wantSame: true,
		},
		{
			name:     "non-security error is preserved",
			err:      errors.New("boom"),
			wantCode: kcerrors.ExitError,
			wantSame: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MapSecurityExit(tc.err)
			if got == nil {
				t.Fatalf("MapSecurityExit(%v) = nil", tc.err)
			}
			if code := kcerrors.Code(got); code != tc.wantCode {
				t.Fatalf("MapSecurityExit(%v) code = %d, want %d (err=%v)", tc.err, code, tc.wantCode, got)
			}
			if tc.wantSame && !errors.Is(got, tc.err) && got.Error() != tc.err.Error() {
				t.Fatalf("MapSecurityExit(%v) = %v, want original error", tc.err, got)
			}
		})
	}
}

func TestMapSecurityExitNil(t *testing.T) {
	if got := MapSecurityExit(nil); got != nil {
		t.Fatalf("MapSecurityExit(nil) = %v, want nil", got)
	}
}

func TestMapSecurityExitWrapped(t *testing.T) {
	sandboxStderr := "security: SecKeychainSearchCreateFromAttributes: One or more parameters passed to a function were not valid.\n"
	err := fmt.Errorf("lookup failed: %w", &SecurityError{ExitCode: 44, Stderr: sandboxStderr, Op: "find-generic-password"})
	got := MapSecurityExit(err)
	if code := kcerrors.Code(got); code != kcerrors.ExitBlocked {
		t.Fatalf("wrapped sandbox error code = %d, want %d", code, kcerrors.ExitBlocked)
	}
}
