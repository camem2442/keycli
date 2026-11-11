package cli

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/interactive"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/camem2442/keycli/internal/version"
	"github.com/spf13/cobra"
)

// Execute runs the kc command tree.
func Execute() int {
	return runRoot(newRoot(app.New))
}

func runRoot(root *cobra.Command) int {
	if err := root.Execute(); err != nil {
		var ec ExitCodeError
		if errors.As(err, &ec) {
			return ec.Code
		}
		return kcerrors.Code(mapCobraErr(err))
	}
	return kcerrors.ExitOK
}

func newRoot(openApp func(context.Context) (*app.App, error)) *cobra.Command {
	// Constructors capture the pointer. Populate it only for commands that
	// need the application graph, after Cobra has selected the command.
	a := &app.App{}
	delivery := newDeliveryCmd()
	auth := newAuthCmd()
	rotation := newRotationCmd()
	root := &cobra.Command{
		Use:          "kc",
		Short:        "keycli — personal secrets CLI",
		Version:      version.String(),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if interactive.IsTerminal(os.Stdin) && interactive.IsTerminal(os.Stdout) {
				return interactive.RunManager(cmd.Context(), a, clipboardCopyWithAutoClear)
			}
			return cmd.Help()
		},
	}
	root.AddCommand(newLSCmd(a))
	root.AddCommand(newGetCmd(a))
	root.AddCommand(newSetCmd(a))
	root.AddCommand(newExecCmd(a))
	root.AddCommand(newCpCmd(a))
	root.AddCommand(newRmCmd(a))
	root.AddCommand(newInitCmd(a))
	root.AddCommand(newPushCmd(a))
	root.AddCommand(newExportCmd(a))
	root.AddCommand(newImportCmd(a))
	root.AddCommand(newStatusCmd(a))
	root.AddCommand(newClipWaitClearCmd(a))
	root.AddCommand(newLinkCmd(a))
	root.AddCommand(newUnlinkCmd(a))
	root.AddCommand(newSetupGitCmd(a))
	root.AddCommand(newGitCredentialCmd(a))
	root.AddCommand(newRulesCmd(a))
	root.AddCommand(newDoctorCmd(a))
	root.AddCommand(newPluginCmd(a))
	root.AddCommand(newAuditCmd(a))
	root.AddCommand(newPolicyCmd(a))
	root.AddCommand(newWireCmd(a))
	root.AddCommand(newInteractiveCmd(a))
	root.AddCommand(newMCPCmd(a), delivery, auth, rotation)
	// A malformed policy glob must not brick diagnostics. Reads still fail
	// closed: every command outside the diagnostic set reports the error and
	// exits non-zero, and the policy middleware denies everything.
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		switch cmd.Name() {
		case "help", "completion", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
			return nil
		}
		if cmd == delivery || cmd.Parent() == delivery || cmd == auth || cmd.Parent() == auth ||
			cmd == rotation || cmd.Parent() == rotation {
			return nil
		}
		loaded, err := openApp(cmd.Context())
		if err != nil {
			return err
		}
		*a = *loaded
		if policyDiagnostic(cmd) {
			return nil
		}
		return a.Config.ValidatePolicyPatterns()
	}
	return root
}

func mapCobraErr(err error) error {
	if err == nil {
		return nil
	}
	// cobra/pflag return plain errors for usage; map them to the contract's
	// exit code 64 instead of the generic 1.
	msg := err.Error()
	for _, prefix := range []string{
		"unknown command",
		"unknown flag",
		"unknown shorthand flag",
		"flag needs an argument",
		"required flag(s)",
		"requires at least",
		"requires exactly",
		"accepts ",
		"invalid argument",
	} {
		if strings.HasPrefix(msg, prefix) {
			return kcerrors.ErrUsage
		}
	}
	return err
}

// policyDiagnostic reports whether cmd may run even when the policy config has
// an invalid glob. These commands read no secret values, so an operator can
// still see what is wrong instead of losing every entry point.
func policyDiagnostic(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "doctor", "policy", "rules", "help", "completion":
			return true
		}
	}
	return false
}
