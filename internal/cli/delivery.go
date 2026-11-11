package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/delivery"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/camem2442/keycli/internal/middleware"
	"github.com/spf13/cobra"
)

func newDeliveryCmd() *cobra.Command {
	group := &cobra.Command{
		Use:   "delivery",
		Short: "Plan scoped credential delivery and inspect operations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	var name, path string
	var asJSON bool
	plan := &cobra.Command{
		Use:   "plan",
		Short: "Validate a delivery profile offline",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeliveryPlan(cmd.OutOrStdout(), name, path, asJSON)
		},
	}
	plan.Flags().StringVar(&name, "profile", "", "Exact delivery profile name (required)")
	plan.Flags().StringVar(&path, "config", "", "Delivery TOML file (default ~/.config/kc/delivery.toml)")
	plan.Flags().BoolVar(&asJSON, "json", false, "Emit metadata JSON (v1)")
	_ = plan.MarkFlagRequired("profile")
	group.AddCommand(plan, newDeliveryStatusCmd())
	return group
}

func runDeliveryPlan(out io.Writer, name, path string, asJSON bool) error {
	if strings.TrimSpace(name) == "" {
		return kcerrors.ErrUsage
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return errors.New("cannot locate KeyCLI home directory")
	}
	profiles, err := loadDeliveryProfiles(path)
	if err != nil {
		return err
	}
	cfg, _, err := config.Load(home)
	if err != nil {
		return errors.New("cannot read KeyCLI policy and alias configuration")
	}
	metadata, err := delivery.NewMetadataSnapshot(cfg, middleware.DetectAgent())
	if err != nil {
		return err
	}
	plan, err := delivery.BuildPlan(profiles, name, metadata)
	if errors.Is(err, delivery.ErrUnknownProfile) {
		return fmt.Errorf("%w: %w", kcerrors.ErrUsage, err)
	}
	if err != nil {
		return err
	}
	if asJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(plan)
	}
	target, err := json.Marshal(plan.Target)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "%s (%s)\nEffect: %s\nTarget: %s\nPlan: %s\n",
		plan.Profile, plan.Provider, plan.Effect, target, plan.ID); err != nil {
		return err
	}
	if plan.Source != nil {
		if _, err := fmt.Fprintf(out, "Source: %s\n", plan.Source.CanonicalName); err != nil {
			return err
		}
	}
	if plan.WIF != nil {
		wif, err := json.Marshal(plan.WIF)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "WIF: %s\n", wif); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(out, "Remote verification: %s (%s)\n", plan.RemoteVerification, strings.Join(plan.Unverified, ", "))
	return err
}

func loadDeliveryProfiles(path string) (delivery.Profiles, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return delivery.Profiles{}, errors.New("cannot locate KeyCLI home directory")
		}
		path = filepath.Join(home, ".config", "kc", "delivery.toml")
	}
	data, err := readPrivateDeliveryInput(path, "delivery configuration")
	if err != nil {
		return delivery.Profiles{}, err
	}
	return delivery.ParseProfiles(data)
}

func readPrivateDeliveryInput(path, description string) ([]byte, error) {
	const maxBytes = 1 << 20
	// Opening a named pipe must not block before we can reject its type.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s", description)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("%s must be a regular file with no group or other access (use chmod 600)", description)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil || len(data) > maxBytes {
		return nil, fmt.Errorf("cannot read %s within 1 MiB", description)
	}
	return data, nil
}
