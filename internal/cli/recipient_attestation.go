package cli

import (
	"fmt"
	"time"

	"filippo.io/age"
	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/recipientattest"
	"github.com/spf13/cobra"
)

func newRecipientChallengeCmd(a *app.App) *cobra.Command {
	var trustPath, recipient, environment, name, output string
	cmd := &cobra.Command{
		Use:   "recipient-challenge",
		Short: "Create a sender-owned one-use challenge for an independently trusted issuer",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if trustPath == "" || recipient == "" || environment == "" || name == "" || output == "" {
				return fmt.Errorf("recipient-challenge requires --attestation-trust, --to, --environment, --name and --output-file")
			}
			if _, err := age.ParseX25519Recipient(recipient); err != nil {
				return fmt.Errorf("recipient challenge requires an X25519 recipient")
			}
			trust, err := recipientattest.LoadTrust(trustPath)
			if err != nil {
				return err
			}
			canonical, err := a.Resolver.StrictCanonicalName(name)
			if err != nil {
				return err
			}
			request, err := recipientattest.CreateChallenge(a.Home, trust, recipient, environment, canonical, time.Now())
			if err != nil {
				return err
			}
			if err = recipientattest.WriteRequest(output, request); err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "kc: public challenge created; recipient identity is not verified until trusted issuer attestation is checked")
			return nil
		},
	}
	cmd.Flags().StringVar(&trustPath, "attestation-trust", "", "independently provisioned issuer key and environment trust file")
	cmd.Flags().StringVar(&recipient, "to", "", "expected age X25519 recipient")
	cmd.Flags().StringVar(&environment, "environment", "", "expected environment ID, exactly matching trusted configuration")
	cmd.Flags().StringVar(&name, "name", "", "single secret name to bind, without reading its value")
	cmd.Flags().StringVar(&output, "output-file", "", "new public challenge JSON file (0600)")
	return cmd
}

type exportAttestation struct{ envelope, trust, challenge string }

func (o *exportAttestation) flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.envelope, "recipient-attestation", "", "issuer-signed recipient attestation envelope file")
	cmd.Flags().StringVar(&o.trust, "attestation-trust", "", "independently provisioned issuer key and environment trust file")
	cmd.Flags().StringVar(&o.challenge, "attestation-challenge", "", "nonce of the sender-owned one-use challenge")
}

func (o exportAttestation) enabled(cmd *cobra.Command) bool {
	return cmd.Flags().Changed("recipient-attestation") || cmd.Flags().Changed("attestation-trust") || cmd.Flags().Changed("attestation-challenge")
}

func (o exportAttestation) authorize(a *app.App, recipient, canonicalName string) error {
	if o.envelope == "" || o.trust == "" || o.challenge == "" {
		return fmt.Errorf("attested export requires --recipient-attestation, --attestation-trust and --attestation-challenge")
	}
	trust, err := recipientattest.LoadTrust(o.trust)
	if err != nil {
		return err
	}
	data, err := recipientattest.ReadFile(o.envelope)
	if err != nil {
		return err
	}
	return recipientattest.VerifyAndConsume(a.Home, trust, data, o.challenge, recipient, canonicalName, time.Now)
}
