package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/camem2442/keycli/internal/delivery"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/spf13/cobra"
)

func newAuthCmd() *cobra.Command {
	group := &cobra.Command{
		Use:   "auth",
		Short: "Inspect authentication contracts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	var profile, configPath, claimsPath string
	var offline, asJSON bool
	probe := &cobra.Command{
		Use:   "probe",
		Short: "Compare a synthetic GitHub Actions claim fixture offline",
		Long: "Compare exactly seven string claims (iss, aud, repository, ref, workflow_ref, environment, event_name) " +
			"from a handcrafted JSON fixture. The event must be workflow_dispatch. " +
			"Use synthetic data only. This command does not accept JWTs or authenticate to any provider.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !offline || strings.TrimSpace(profile) == "" || strings.TrimSpace(claimsPath) == "" {
				return fmt.Errorf("%w: auth probe requires --offline, --profile, and --claims", kcerrors.ErrUsage)
			}
			profiles, err := loadDeliveryProfiles(configPath)
			if err != nil {
				return err
			}
			data, err := readPrivateDeliveryInput(claimsPath, "synthetic claims file")
			if err != nil {
				return err
			}
			result, err := delivery.ProbeGitHubClaims(profiles, profile, data)
			if errors.Is(err, delivery.ErrUnknownProfile) || errors.Is(err, delivery.ErrUnsupportedClaimProbe) {
				return fmt.Errorf("%w: %w", kcerrors.ErrUsage, err)
			}
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				encoder := json.NewEncoder(out)
				encoder.SetIndent("", "  ")
				return encoder.Encode(result)
			}
			_, err = fmt.Fprintf(out, "Synthetic GitHub claims match profile %s.\nAuthentication: %s\n",
				result.Profile, result.Authentication)
			return err
		},
	}
	probe.Flags().StringVar(&profile, "profile", "", "Exact GitHub Actions WIF delivery profile name (required)")
	probe.Flags().BoolVar(&offline, "offline", false, "Require the offline-only probe mode")
	probe.Flags().StringVar(&claimsPath, "claims", "", "Private JSON file containing synthetic claim strings (required)")
	probe.Flags().StringVar(&configPath, "config", "", "Delivery TOML file (default ~/.config/kc/delivery.toml)")
	probe.Flags().BoolVar(&asJSON, "json", false, "Emit advisory result JSON (v1)")
	for _, flag := range []string{"profile", "offline", "claims"} {
		_ = probe.MarkFlagRequired(flag)
	}
	group.AddCommand(probe, newWIFCheckCmd())
	return group
}
