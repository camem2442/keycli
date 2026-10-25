package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/camem2442/keycli/internal/app"
	"github.com/spf13/cobra"
)

func newLSCmd(a *app.App) *cobra.Command {
	var asJSON bool
	var showAll bool
	var showStale bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List secret names (never values)",
		RunE: func(cmd *cobra.Command, args []string) error {
			items, err := a.Resolver.List(readCtx(cmd, "ls"))
			if err != nil {
				return err
			}
			if showStale {
				items = a.AnnotateStale(readCtx(cmd, "ls"), items)
			}
			sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
			if asJSON {
				payload := struct {
					V     int         `json:"v"`
					Items interface{} `json:"items"`
				}{V: 1, Items: items}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(payload)
			}
			for _, it := range items {
				if it.PolicyDenied && !showAll {
					continue
				}
				line := it.Name
				if it.StaleExport {
					line += " *"
				}
				if it.PolicyDenied {
					line += " (policy)"
				}
				fmt.Println(line)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output (schema v1)")
	cmd.Flags().BoolVar(&showAll, "all", false, "Include names denied by agent policy")
	cmd.Flags().BoolVar(&showStale, "stale", false, "Read exported secrets to mark stale entries (slow; never reads policy-denied names)")
	return cmd
}
