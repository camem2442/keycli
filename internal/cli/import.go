package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"filippo.io/age"
	"github.com/camem2442/keycli/internal/agebundle"
	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/applecsv"
	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/transferguard"
	"github.com/spf13/cobra"
)

func newImportCmd(a *app.App) *cobra.Command {
	var expectedDigest, verifiedRecipient, expectedName string
	var force bool
	var format, inputFile string
	var allowPlaintext, dryRun, keepOTP, keepNotes bool
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import an age bundle from stdin, or an Apple Passwords CSV, into the file store",
		RunE: func(cmd *cobra.Command, args []string) error {
			switch format {
			case "":
			case "csv":
				if cmd.Flags().Changed("expected-sha256") || cmd.Flags().Changed("verified-recipient") ||
					cmd.Flags().Changed("expected-name") {
					return fmt.Errorf("--expected-sha256, --verified-recipient and --expected-name apply to age bundles, not --format csv")
				}
				return importAppleCSV(cmd, a, inputFile, allowPlaintext, dryRun, keepOTP, keepNotes, force)
			default:
				return fmt.Errorf("unknown --format %q; only csv is supported", format)
			}
			if inputFile != "" || allowPlaintext || dryRun || keepOTP || keepNotes {
				return fmt.Errorf("--input-file, --allow-plaintext, --dry-run, --keep-otp and --keep-notes apply only to --format csv")
			}
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
	cmd.Flags().StringVar(&format, "format", "", "csv: read an Apple Passwords CSV instead of an age bundle")
	cmd.Flags().StringVar(&inputFile, "input-file", "", "with --format csv: read this file instead of stdin")
	cmd.Flags().BoolVar(&allowPlaintext, "allow-plaintext", false, "with --format csv: acknowledge that the input is unencrypted")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "with --format csv: print the names that would be imported, write nothing")
	cmd.Flags().BoolVar(&keepOTP, "keep-otp", false, "with --format csv: store each verification code as NAME.otp")
	cmd.Flags().BoolVar(&keepNotes, "keep-notes", false, "with --format csv: also copy the Notes cell into kc metadata (readable by agents via ls --json)")
	return cmd
}

// csvPlan is one secret that a CSV import will write.
type csvPlan struct {
	name  string
	value []byte
	meta  backend.Meta
}

// importAppleCSV turns the six-column Apple Passwords CSV into secrets. The
// password column becomes the value; username, URL and the original notes
// become metadata. Verification codes are secrets too, so they are never
// copied into the note field — that field is readable through `kc ls --json`.
func importAppleCSV(cmd *cobra.Command, a *app.App, inputFile string, allowPlaintext, dryRun, keepOTP, keepNotes, force bool) error {
	if !allowPlaintext {
		return fmt.Errorf("csv input is plaintext; re-run with --allow-plaintext")
	}
	var r io.Reader = cmd.InOrStdin()
	if inputFile != "" {
		f, err := os.Open(inputFile)
		if err != nil {
			return fmt.Errorf("open csv input: %w", err)
		}
		defer f.Close()
		r = f
	}
	rows, err := applecsv.Parse(r)
	if err != nil {
		return err
	}

	items, err := a.Resolver.List(cmd.Context())
	if err != nil {
		return err
	}
	existing := make(map[string]bool, len(items))
	for _, it := range items {
		existing[strings.ToLower(it.Name)] = true
	}

	plans, stats := planCSVImport(rows, a.Resolver.CanonicalName, existing, keepOTP, keepNotes)
	if len(plans) == 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "kc: csv에 가져올 항목이 없습니다 (건너뜀 %d행).\n", stats.skipped)
		return nil
	}
	conflicts := conflictNames(plans, existing)

	if dryRun {
		names := make([]string, 0, len(plans))
		for _, p := range plans {
			names = append(names, p.name)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "kc: dry run: %d secret(s) would be written: %s\n",
			len(names), summarise(names, 20))
		if len(conflicts) > 0 {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"kc: %d name(s) already exist and need --force: %s\n",
				len(conflicts), summarise(conflicts, 10))
		}
		reportCSVSkips(cmd, stats, keepOTP)
		return nil
	}

	if len(conflicts) > 0 && !force {
		return fmt.Errorf("refusing to overwrite %d existing secret(s): %s (re-run with --force)",
			len(conflicts), strings.Join(conflicts, ", "))
	}
	if len(conflicts) > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "kc: --force: overwriting %d existing secret(s): %s\n",
			len(conflicts), strings.Join(conflicts, ", "))
	}

	for _, p := range plans {
		if err := a.Resolver.Set(cmd.Context(), p.name, p.value, p.meta); err != nil {
			return fmt.Errorf("store %s: %w", p.name, err)
		}
	}
	fmt.Fprintf(cmd.ErrOrStderr(),
		"kc: imported %d secret(s) from CSV with no expiry — record rotation ownership; `kc ls` shows expires\n",
		len(plans))
	reportCSVSkips(cmd, stats, keepOTP)
	fmt.Fprintln(cmd.ErrOrStderr(), "kc: delete the source CSV now that the import finished")
	return nil
}

type csvStats struct {
	skipped    int
	noPassword int
	otp        int
}

// planCSVImport builds the write set: one secret per row with a password, an
// extra NAME.otp per row with a verification code when keepOTP is set.
//
// Names are canonicalised before the conflict check because Resolver.Set
// canonicalises on write: a title that matches an alias ("gh" → "github") must
// not slip past the --force guard. Suffixed names avoid names that already
// exist so a generated "name-2" never overwrites an entry nobody confirmed.
func planCSVImport(rows []applecsv.Row, canonical func(string) string, existing map[string]bool, keepOTP, keepNotes bool) ([]csvPlan, csvStats) {
	var (
		plans []csvPlan
		stats csvStats
		used  = make(map[string]bool, len(rows))
	)
	for _, row := range rows {
		if strings.TrimSpace(row.Password) == "" {
			stats.skipped++
			stats.noPassword++
			continue
		}
		name := assignedName(applecsv.NameFromTitle(row.Title), canonical, existing, used)
		used[name] = true
		plans = append(plans, csvPlan{
			name:  name,
			value: []byte(row.Password),
			meta: backend.Meta{
				Kind:     "login",
				URL:      row.URL,
				Username: row.Username,
				Note:     csvNote(row, keepNotes),
			},
		})
		if !applecsv.IsOTPAuth(row.OTPAuth) {
			continue
		}
		if !keepOTP {
			stats.otp++
			continue
		}
		otpName := assignedName(name+".otp", canonical, existing, used)
		used[otpName] = true
		plans = append(plans, csvPlan{
			name:  otpName,
			value: []byte(row.OTPAuth),
			meta: backend.Meta{
				Kind: "otp",
				Note: "otpauth from Apple Passwords entry " + name,
			},
		})
	}
	return plans, stats
}

// assignedName returns the canonical name for a title. The first row that asks
// for a name gets it even when the store already holds it, so the caller can
// report it as a conflict; later rows and generated names step around both the
// file and the store.
func assignedName(base string, canonical func(string) string, existing, used map[string]bool) string {
	name := canonical(base)
	if !used[name] {
		return name
	}
	for i := 2; ; i++ {
		candidate := canonical(fmt.Sprintf("%s-%d", base, i))
		if !used[candidate] && !existing[candidate] {
			return candidate
		}
	}
}

// csvNote keeps the Notes cell only when asked: it is readable through
// `kc ls --json`, and people do keep recovery codes and seeds there.
func csvNote(row applecsv.Row, keepNotes bool) string {
	if keepNotes {
		return row.Notes
	}
	return ""
}

func conflictNames(plans []csvPlan, existing map[string]bool) []string {
	var out []string
	for _, p := range plans {
		if existing[strings.ToLower(p.name)] {
			out = append(out, p.name)
		}
	}
	sort.Strings(out)
	return out
}

func reportCSVSkips(cmd *cobra.Command, stats csvStats, keepOTP bool) {
	if stats.noPassword > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "kc: skipped %d row(s) without a password\n", stats.noPassword)
	}
	if stats.otp > 0 && !keepOTP {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"kc: %d verification code(s) were not imported (kc has no TOTP support); re-run with --keep-otp to store them as NAME.otp\n",
			stats.otp)
	}
}

func summarise(names []string, limit int) string {
	if len(names) <= limit {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, … (+%d)", strings.Join(names[:limit], ", "), len(names)-limit)
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
