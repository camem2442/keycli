package cli

import (
	"os"
	"time"

	"github.com/camem2442/keycli/internal/clip"
	"github.com/spf13/cobra"
)

func newClipWaitClearCmd() *cobra.Command {
	var checksum string
	cmd := &cobra.Command{
		Use:    "_clip-wait-clear",
		Hidden: true,
		Short:  "Wait and clear clipboard if unchanged (internal)",
		RunE: func(cmd *cobra.Command, args []string) error {
			delay := clip.AutoClearDelay()
			time.Sleep(delay)
			cp := clip.Provider{OSC52Enabled: os.Getenv("KC_CLIP_OSC52") == "1"}
			cur, err := cp.Read()
			if err != nil {
				return nil
			}
			if clip.Checksum(cur) == checksum {
				_ = cp.Clear()
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&checksum, "checksum", "", "Checksum of copied text")
	return cmd
}
