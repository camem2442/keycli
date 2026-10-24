package cli

import (
	"fmt"
	"os"

	"github.com/camem2442/keycli/internal/backend/file"
	"github.com/spf13/cobra"
)

func newKeygenCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "keygen",
		Short:  "Generate a new age identity for KC_KEY (prints once to stdout)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			key, err := file.GenerateKCKey()
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, "kc: stdout에 age identity를 출력합니다. KC_KEY 또는 macOS 키체인(kc.age)에 저장하세요.")
			_, err = fmt.Fprint(os.Stdout, key)
			return err
		},
	}
}
