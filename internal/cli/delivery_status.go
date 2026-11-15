package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/spf13/cobra"
)

func newDeliveryStatusCmd() *cobra.Command {
	var id string
	var asJSON bool
	cmd := &cobra.Command{
		Use: "status", Short: "Read a local delivery operation without contacting providers",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if id == "" {
				return kcerrors.ErrUsage
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return errors.New("cannot locate KeyCLI home directory")
			}
			store := journal.Store{Root: filepath.Join(home, ".kc", "delivery-journal")}
			record, err := store.Status(id)
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(record)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), record.State)
			return err
		},
	}
	cmd.Flags().StringVar(&id, "operation-id", "", "Exact operation ID (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit local journal metadata")
	_ = cmd.MarkFlagRequired("operation-id")
	return cmd
}
