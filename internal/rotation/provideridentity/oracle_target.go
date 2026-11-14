package provideridentity

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// StageTarget binds the host registration to the Current baseline, exact
// independently observed precondition, and operation's version name.
func (o OracleVaultVersion) StageTarget(etag, versionName string) string {
	return o.oracleTargetPrefix() + "/current/" + strconv.FormatUint(o.Version, 10) +
		"/etag/" + oracleTargetHash(etag) + "/pending/" + versionName
}

// DeletionTarget binds the host registration to one version, its ETag, and
// the exact permanent deletion time represented on the wire.
func (o OracleVaultVersion) DeletionTarget(etag string, deleteAt time.Time) string {
	return o.oracleTargetPrefix() + "/version/" + strconv.FormatUint(o.Version, 10) +
		"/etag/" + oracleTargetHash(etag) + "/delete-at/" + deleteAt.UTC().Format(time.RFC3339)
}

func (o OracleVaultVersion) oracleTargetPrefix() string {
	identity := strings.Join([]string{o.Region, o.SecretID, o.TenancyID, o.CompartmentID, o.VaultID}, "\x00")
	return o.Region + "/identity/" + oracleTargetHash(identity)
}

func oracleTargetHash(etag string) string {
	sum := sha256.Sum256([]byte(etag))
	return hex.EncodeToString(sum[:])
}
