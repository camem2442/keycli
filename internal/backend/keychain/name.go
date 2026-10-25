package keychain

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/camem2442/keycli/internal/kcerrors"
)

const (
	secretServicePrefix = "kc.secret."
	ageIdentityService  = "kc.age"
)

// secretNameRE is the safe name alphabet for keychain services. Whitespace,
// quotes, backslashes and control characters are rejected so a name can never
// inject another line into `security -i`.
var secretNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:#-]*$`)

// ValidateSecretName rejects names that cannot be represented safely as a
// keychain service. kc.age is reserved for the age identity.
func ValidateSecretName(name string) error {
	if !secretNameRE.MatchString(name) {
		return fmt.Errorf("%w: invalid secret name %q", kcerrors.ErrUsage, name)
	}
	if strings.EqualFold(name, ageIdentityService) {
		return fmt.Errorf("%w: %q is reserved for the kc age identity", kcerrors.ErrUsage, ageIdentityService)
	}
	return nil
}

// writeService maps a secret name to kc.secret.<name>. Names that already
// carry the prefix are accepted unchanged so existing items stay reachable.
func (k *Keychain) writeService(name string) (string, error) {
	if err := ValidateSecretName(name); err != nil {
		return "", err
	}
	service := name
	if !strings.HasPrefix(service, secretServicePrefix) {
		service = secretServicePrefix + service
	}
	if strings.EqualFold(service, ageIdentityService) {
		return "", fmt.Errorf("%w: %q is reserved for the kc age identity", kcerrors.ErrUsage, ageIdentityService)
	}
	if strings.TrimPrefix(service, secretServicePrefix) == "" {
		return "", fmt.Errorf("%w: empty secret name", kcerrors.ErrUsage)
	}
	return service, nil
}
