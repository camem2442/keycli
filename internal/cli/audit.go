package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/middleware/audit"
	"github.com/spf13/cobra"
)

func newAuditCmd(a *app.App) *cobra.Command {
	var since string
	var nameFilter string
	var agentFilter string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "View the secret read audit log (no values)",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := audit.NewWriter(a.Home, a.Config, auditWarn)
			path := w.Path()
			data, err := os.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					if asJSON {
						fmt.Println("[]")
						return nil
					}
					return nil
				}
				return err
			}
			var sinceTime time.Time
			if since != "" {
				d, err := time.ParseDuration(since)
				if err != nil {
					return fmt.Errorf("invalid --since: %w", err)
				}
				sinceTime = time.Now().Add(-d)
			}
			var recs []audit.Record
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				var rec audit.Record
				if err := json.Unmarshal([]byte(line), &rec); err != nil {
					continue
				}
				if since != "" {
					ts, err := time.Parse(time.RFC3339Nano, rec.TS)
					if err != nil || ts.Before(sinceTime) {
						continue
					}
				}
				if nameFilter != "" && !strings.EqualFold(rec.Name, nameFilter) {
					continue
				}
				if agentFilter != "" && rec.Agent != agentFilter {
					continue
				}
				recs = append(recs, rec)
			}
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(recs)
			}
			for _, rec := range recs {
				fmt.Printf("%s %s agent=%s name=%s outcome=%s", rec.TS, rec.Command, rec.Agent, rec.Name, rec.Outcome)
				if rec.Source != "" {
					fmt.Printf(" source=%s", rec.Source)
				}
				fmt.Println()
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "Only entries newer than duration (e.g. 1h)")
	cmd.Flags().StringVar(&nameFilter, "name", "", "Filter by secret name")
	cmd.Flags().StringVar(&agentFilter, "agent", "", "Filter by agent id")
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON array output")
	return cmd
}

func auditWarn(msg string) {
	fmt.Fprintln(os.Stderr, msg)
}
