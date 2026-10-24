package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/camem2442/keycli/internal/version"
	"github.com/spf13/cobra"
)

// Execute runs the kc command tree.
func Execute() int {
	a, err := app.New(contextBackground())
	if err != nil {
		fmt.Fprintf(os.Stderr, "kc: %v\n", err)
		return kcerrors.ExitError
	}
	root := &cobra.Command{
		Use:          "kc",
		Short:        "keycli — personal secrets CLI",
		Version:      version.String(),
		SilenceUsage: true,
	}
	root.AddCommand(newLSCmd(a))
	root.AddCommand(newGetCmd(a))
	root.AddCommand(newSetCmd(a))
	root.AddCommand(newExecCmd(a))
	root.AddCommand(newCpCmd(a))
	root.AddCommand(newRmCmd(a))
	root.AddCommand(newInitCmd())
	root.AddCommand(newPushCmd(a))
	root.AddCommand(newExportCmd(a))
	root.AddCommand(newImportCmd(a))
	root.AddCommand(newStatusCmd(a))
	root.AddCommand(newClipWaitClearCmd())
	root.AddCommand(newLinkCmd(a))
	root.AddCommand(newUnlinkCmd(a))
	root.AddCommand(newSetupGitCmd(a))
	root.AddCommand(newGitCredentialCmd(a))
	root.AddCommand(newRulesCmd(a))
	root.AddCommand(newDoctorCmd(a))
	root.AddCommand(newPluginCmd(a))
	if err := root.Execute(); err != nil {
		var ec ExitCodeError
		if errors.As(err, &ec) {
			return ec.Code
		}
		return kcerrors.Code(mapCobraErr(err))
	}
	return kcerrors.ExitOK
}

func contextBackground() context.Context {
	return context.Background()
}

func mapCobraErr(err error) error {
	if err == nil {
		return nil
	}
	// cobra returns generic errors for usage
	if err.Error() == "unknown command" || err.Error() == "unknown flag" {
		return kcerrors.ErrUsage
	}
	return err
}
