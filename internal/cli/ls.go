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
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List secret names (never values)",
		RunE: func(cmd *cobra.Command, args []string) error {
			items, err := a.Resolver.List(cmd.Context())
			if err != nil {
				return err
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
				fmt.Println(it.Name)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output (schema v1)")
	return cmd
}
