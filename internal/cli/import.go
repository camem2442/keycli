package cli

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"

	"filippo.io/age"
	"github.com/camem2442/keycli/internal/agebundle"
	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/transferguard"
	"github.com/spf13/cobra"
)

func newImportCmd(a *app.App) *cobra.Command {
	var expectedDigest, verifiedRecipient, expectedName string
	var force bool
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import an age bundle from stdin into the file store",
		RunE: func(cmd *cobra.Command, args []string) error {
			guarded := cmd.Flags().Changed("expected-sha256") || cmd.Flags().Changed("verified-recipient") || cmd.Flags().Changed("expected-name")
			if guarded && (expectedDigest == "" || verifiedRecipient == "" || expectedName == "") {
				return fmt.Errorf("guarded import requires --expected-sha256, --verified-recipient and --expected-name")
			}
			fs, err := a.FileStore(cmd.Context())
			if err != nil {
				return err
			}
			id, err := fs.LoadIdentityForImport()
			if err != nil {
				return err
			}
			if guarded {
				x, ok := id.(*age.X25519Identity)
				if !ok {
					return fmt.Errorf("guarded import requires an X25519 identity")
				}
				if err := transferguard.CheckRecipient(x.Recipient().String(), verifiedRecipient); err != nil {
					return err
				}
				data, err := transferguard.ReadVerified(os.Stdin, expectedDigest)
				if err != nil {
					return err
				}
				secrets, err := agebundle.Decrypt(bytes.NewReader(data), id)
				if err != nil {
					return err
				}
				canonicalName := a.Resolver.CanonicalName(expectedName)
				value, err := transferguard.SingleExpected(secrets, canonicalName)
				if err != nil {
					return err
				}
				return fs.SetIfAbsent(cmd.Context(), canonicalName, value, backend.Meta{})
			}
			secrets, err := agebundle.Decrypt(os.Stdin, id)
			if err != nil {
				return err
			}
			// An unguarded import used to overwrite existing names silently.
			// Refuse by default so a bundle cannot replace a live credential.
			items, err := a.Resolver.List(cmd.Context())
			if err != nil {
				return err
			}
			existing := make(map[string]bool, len(items))
			for _, it := range items {
				existing[strings.ToLower(it.Name)] = true
			}
			overwrites := overwriteCandidates(secrets, existing, a.Resolver.CanonicalName)
			if len(overwrites) > 0 && !force {
				return fmt.Errorf("refusing to overwrite %d existing secret(s): %s (re-run with --force)",
					len(overwrites), strings.Join(overwrites, ", "))
			}
			if len(overwrites) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "kc: --force: overwriting %d existing secret(s): %s\n",
					len(overwrites), strings.Join(overwrites, ", "))
			}
			for name, val := range secrets {
				if err := a.Resolver.Set(cmd.Context(), name, val, backend.Meta{}); err != nil {
					return err
				}
			}
			// The bundle carries no expiry, so ownership must be explicit.
			fmt.Fprintf(cmd.ErrOrStderr(),
				"kc: imported %d secret(s) with no expiry — record rotation ownership; `kc ls` shows expires\n", len(secrets))
			return nil
		},
	}
	cmd.Flags().StringVar(&expectedDigest, "expected-sha256", "", "independently verified SHA-256 of bundle")
	cmd.Flags().StringVar(&verifiedRecipient, "verified-recipient", "", "receiver public recipient independently verified by operator")
	cmd.Flags().StringVar(&expectedName, "expected-name", "", "single expected secret name; existing values are not overwritten")
	cmd.Flags().BoolVar(&force, "force", false, "Allow an unguarded import to overwrite existing secret names")
	return cmd
}

// overwriteCandidates returns the sorted canonical names in secrets that
// already exist in the store.
func overwriteCandidates(secrets map[string][]byte, existing map[string]bool, canon func(string) string) []string {
	var out []string
	for name := range secrets {
		c := canon(name)
		if existing[strings.ToLower(c)] {
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}
