package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"filippo.io/age"
	"github.com/camem2442/keycli/internal/backend/file"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "init",
		Aliases: []string{"keygen"},
		Short:   "Generate an age identity and print the public recipient (age1…)",
		RunE: func(cmd *cobra.Command, args []string) error {
			key, err := file.GenerateKCKey()
			if err != nil {
				return err
			}
			id, err := age.ParseX25519Identity(key)
			if err != nil {
				return err
			}
			home, _ := os.UserHomeDir()
			if home == "" {
				home = "."
			}
			if os.Getenv("KC_KEY") == "" {
				path := filepath.Join(home, ".kc", "receiver.key")
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
					if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
						if werr := os.WriteFile(path, []byte(key), 0o600); werr == nil {
							fmt.Fprintf(cmd.ErrOrStderr(), "kc: age identity를 %s 에 저장했습니다 (KC_KEY 대체).\n", path)
						}
					}
				}
			}
			rec := id.Recipient()
			fmt.Fprintln(cmd.ErrOrStderr(), "kc: 맥에서 kc export --to 로 아래 공개 수신자(age1…)에 암호화해 보내세요.")
			fmt.Println(rec.String())
			if runtime.GOOS == "darwin" {
				fmt.Fprintln(cmd.ErrOrStderr(), "kc: macOS에서는 키체인 kc.age 항목 또는 KC_KEY 환경변수도 사용할 수 있습니다.")
			}
			return nil
		},
	}
}
