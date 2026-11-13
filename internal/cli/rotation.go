package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/effectclient"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/rotation"
	"github.com/spf13/cobra"
)

const (
	rotationInputExit       = 2
	rotationUnconfirmedExit = 3
	rotationDeniedExit      = 5
	rotationRequestTTL      = time.Minute
	rotationGatewayTimeout  = 20 * time.Second
)

type rotationOutput struct {
	Version   int                   `json:"v"`
	Command   string                `json:"command"`
	Operation string                `json:"operation,omitempty"`
	Plan      *rotationPlanOutput   `json:"plan,omitempty"`
	Rotation  *RotationStatus       `json:"rotation,omitempty"`
	Effect    *rotationEffectOutput `json:"effect,omitempty"`
	Error     *rotationErrorOutput  `json:"error,omitempty"`
}

type rotationErrorOutput struct {
	Code string `json:"code"`
}

type rotationGenerationOutput struct {
	Alias    string                  `json:"alias"`
	Kind     rotation.CredentialKind `json:"kind"`
	Provider string                  `json:"provider"`
	Target   string                  `json:"target"`
	Number   uint64                  `json:"number"`
}

type rotationPlanOutput struct {
	PlanDigest        string                   `json:"plan_digest"`
	Old               rotationGenerationOutput `json:"old"`
	New               rotationGenerationOutput `json:"new"`
	RequiredConsumers []string                 `json:"required_consumers"`
}

type rotationEffectOutput struct {
	MutationID    string `json:"mutation_id"`
	Outcome       string `json:"outcome"`
	JournalState  string `json:"journal_state,omitempty"`
	ProviderCalls *int   `json:"provider_calls,omitempty"`
	ProofDigest   string `json:"proof_digest,omitempty"`
	StatusLookup  bool   `json:"status_lookup"`
}

func newRotationCmd() *cobra.Command {
	group := &cobra.Command{
		Use: "rotation", Short: "Plan, inspect and submit registered credential rotations",
		SilenceErrors: true,
		Args:          rotationArgs(0, 0),
		RunE:          func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	group.SetFlagErrorFunc(func(cmd *cobra.Command, _ error) error {
		return rotationFailure(cmd, rotationInputExit, "INVALID_ARGUMENTS")
	})

	var input string
	plan := &cobra.Command{
		Use: "plan", Short: "Build a credential rotation plan from local metadata",
		SilenceErrors: true, Args: rotationArgs(0, 0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !rotationAbsolutePath(input) {
				return rotationFailure(cmd, rotationInputExit, "INVALID_INPUT_PATH")
			}
			built, err := buildRotationPlan(input)
			if errors.Is(err, errRotationInput) {
				return rotationFailure(cmd, rotationInputExit, "INVALID_PLAN_METADATA")
			}
			if err != nil {
				return rotationFailure(cmd, rotationUnconfirmedExit, "PLAN_METADATA_UNAVAILABLE")
			}
			consumers := make([]string, 0, len(built.Consumers))
			for _, consumer := range built.Consumers {
				consumers = append(consumers, consumer.ID)
			}
			return writeRotation(cmd, rotationOutput{Plan: &rotationPlanOutput{
				PlanDigest: built.ID, Old: rotationGenerationOutput(built.Old), New: rotationGenerationOutput(built.New),
				RequiredConsumers: consumers,
			}}, 0)
		},
	}
	plan.Flags().StringVar(&input, "input", "", "Absolute path to the local rotation metadata file")

	var journalRoot, planID, statusProfile string
	status := &cobra.Command{
		Use: "status [operation-id]", Short: "Inspect a local rotation session or authenticated Gateway operation",
		SilenceErrors: true, Args: rotationArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				if cmd.Flags().Changed("journal") || cmd.Flags().Changed("plan") {
					return rotationFailure(cmd, rotationInputExit, "INVALID_ARGUMENTS")
				}
				return runRotationGateway(cmd, statusProfile, args[0], effectgateway.ReadStatus)
			}
			if cmd.Flags().Changed("profile") || !rotationAbsolutePath(journalRoot) || !journal.ValidDigest(planID) {
				return rotationFailure(cmd, rotationInputExit, "INVALID_ARGUMENTS")
			}
			state, err := loadRotationStatus(cmd.Context(), journalRoot, planID, time.Now())
			if err != nil {
				return rotationFailure(cmd, rotationUnconfirmedExit, "ROTATION_STATUS_UNAVAILABLE")
			}
			return writeRotation(cmd, rotationOutput{Rotation: &state}, 0)
		},
	}
	status.Flags().StringVar(&journalRoot, "journal", "", "Absolute path to the local Gateway journal")
	status.Flags().StringVar(&planID, "plan", "", "Plan digest for local status")
	status.Flags().StringVar(&statusProfile, "profile", "", "Protected writer profile for Gateway status")
	group.AddCommand(plan, status,
		newRotationGatewayCmd(effectclient.Submit, "Submit a registered mutation to the authenticated Gateway"),
		newRotationGatewayCmd(effectgateway.ReadReconcile, "Reconcile an original mutation through the authenticated Gateway"))
	return group
}

func newRotationGatewayCmd(action, help string) *cobra.Command {
	var profile string
	cmd := &cobra.Command{
		Use: action + " <operation-id>", Short: help,
		SilenceErrors: true, Args: rotationArgs(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRotationGateway(cmd, profile, args[0], action)
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "Absolute path to the protected writer profile")
	return cmd
}

func rotationArgs(min, max int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < min || len(args) > max {
			return rotationFailure(cmd, rotationInputExit, "INVALID_ARGUMENTS")
		}
		return nil
	}
}

func rotationFailure(cmd *cobra.Command, code int, reason string) error {
	return writeRotation(cmd, rotationOutput{Error: &rotationErrorOutput{Code: reason}}, code)
}

func writeRotation(cmd *cobra.Command, output rotationOutput, code int) error {
	output.Version, output.Command = 1, cmd.Name()
	if err := json.NewEncoder(cmd.OutOrStdout()).Encode(output); err != nil {
		return ExitCodeError{Code: rotationUnconfirmedExit}
	}
	if code != 0 {
		return ExitCodeError{Code: code}
	}
	return nil
}

func runRotationGateway(cmd *cobra.Command, profilePath, operation, action string) error {
	if !rotationAbsolutePath(profilePath) || !journal.ValidOperationID(operation) {
		return rotationFailure(cmd, rotationInputExit, "INVALID_ARGUMENTS")
	}
	profile, err := loadRotationProfile(profilePath)
	if errors.Is(err, errRotationInput) {
		return rotationFailure(cmd, rotationInputExit, "INVALID_WRITER_PROFILE")
	}
	if err != nil {
		return rotationFailure(cmd, rotationUnconfirmedExit, "WRITER_PROFILE_UNAVAILABLE")
	}
	req, err := profile.operation(operation)
	if err != nil {
		return rotationFailure(cmd, rotationInputExit, "OPERATION_NOT_REGISTERED")
	}
	origin, err := effectclient.LoadOrigin(profile.gatewayConfig)
	if err != nil {
		return rotationFailure(cmd, rotationUnconfirmedExit, "GATEWAY_ORIGIN_UNAVAILABLE")
	}
	key, err := effectgateway.ReadSessionKeyFile(profile.sessionKeyFile)
	if err != nil {
		return rotationFailure(cmd, rotationUnconfirmedExit, "SESSION_KEY_UNAVAILABLE")
	}
	defer clear(key)
	client := effectclient.NewHTTPClient(rotationGatewayTimeout)
	defer client.CloseIdleConnections()
	req.ExpiresAt = time.Now().Add(rotationRequestTTL).UTC()
	result, err := effectclient.Execute(cmd.Context(), client, origin, action, req, key, rotationRequestTTL)
	output := rotationOutput{Operation: operation}
	if errors.Is(err, effectclient.ErrSubmitUnconfirmed) {
		output.Effect = &rotationEffectOutput{MutationID: req.MutationID, Outcome: "SUBMIT_UNCONFIRMED", StatusLookup: result.StatusFallback}
		output.Error = &rotationErrorOutput{Code: "STATUS_UNCONFIRMED"}
		return writeRotation(cmd, output, rotationUnconfirmedExit)
	}
	if err != nil {
		output.Error = &rotationErrorOutput{Code: "GATEWAY_UNAVAILABLE"}
		return writeRotation(cmd, output, rotationUnconfirmedExit)
	}
	if result.HTTPStatus != http.StatusOK {
		code, reason := rotationHTTPError(result)
		output.Error = &rotationErrorOutput{Code: reason}
		return writeRotation(cmd, output, code)
	}
	effect, code := projectRotationEffect(result, action)
	if effect == nil {
		output.Error = &rotationErrorOutput{Code: "INVALID_GATEWAY_RECEIPT"}
	} else {
		output.Effect = effect
	}
	return writeRotation(cmd, output, code)
}

func rotationHTTPError(result effectclient.Result) (int, string) {
	switch result.HTTPStatus {
	case http.StatusUnauthorized, http.StatusForbidden:
		return rotationDeniedExit, "AUTHORIZATION_DENIED"
	case http.StatusBadRequest:
		return rotationInputExit, "GATEWAY_REJECTED_INPUT"
	case http.StatusConflict:
		switch result.ErrorCode {
		case "STALE_GENERATION", "SESSION_MISMATCH", "OWNER_MISMATCH", "CLAIM_NOT_ACTIVE", "CLAIM_LEASE_EXPIRED":
			return rotationDeniedExit, "AUTHORIZATION_DENIED"
		case "MUTATION_CONFLICT":
			return rotationInputExit, "MUTATION_CONFLICT"
		case "TARGET_PENDING":
			return rotationUnconfirmedExit, "TARGET_PENDING"
		}
	case http.StatusServiceUnavailable:
		if result.ErrorCode == "RESERVATION_UNKNOWN" {
			return rotationUnconfirmedExit, "RESERVATION_UNKNOWN"
		}
	case http.StatusNotFound:
		return rotationUnconfirmedExit, "OPERATION_UNAVAILABLE"
	}
	return rotationUnconfirmedExit, "GATEWAY_UNAVAILABLE"
}

func projectRotationEffect(result effectclient.Result, action string) (*rotationEffectOutput, int) {
	r := result.Receipt
	if !journal.ValidOperationID(r.MutationID) || r.ProviderCalls < 0 || r.ProviderCalls > 1 ||
		((action != effectclient.Submit || result.StatusFallback) && r.ProviderCalls != 0) ||
		(r.ProofDigest != "" && !journal.ValidDigest(r.ProofDigest)) {
		return nil, rotationUnconfirmedExit
	}
	effect := &rotationEffectOutput{MutationID: r.MutationID, JournalState: r.OperationState,
		ProofDigest: r.ProofDigest, StatusLookup: result.StatusFallback}
	if !result.StatusFallback {
		effect.ProviderCalls = &r.ProviderCalls
	}
	code := rotationUnconfirmedExit
	switch journal.State(r.OperationState) {
	case journal.Acknowledged:
		if r.Status != effectgateway.StatusApplied || !journal.ValidDigest(r.ProofDigest) {
			return nil, code
		}
		effect.Outcome = "ACKNOWLEDGED_UNVERIFIED"
	case journal.ObservedApplied:
		if r.Status != effectgateway.StatusApplied || !journal.ValidDigest(r.ProofDigest) {
			return nil, code
		}
		effect.Outcome = "OBSERVED_APPLIED"
		code = 0
	case journal.ObservedNoEffect:
		if r.Status != effectgateway.StatusNoEffect || !journal.ValidDigest(r.ProofDigest) || r.ProviderCalls != 0 {
			return nil, code
		}
		effect.Outcome = "OBSERVED_NO_EFFECT"
		if action == effectgateway.ReadReconcile {
			code = 0
		} else if action == effectclient.Submit {
			switch r.Note {
			case "STALE_CLAIM", "SESSION_KEY_INVALID", "REQUEST_EXPIRED", "EXECUTION_FENCE_REJECTED", "EXECUTION_FENCE_EXPIRED":
				code = rotationDeniedExit
			}
		}
	case journal.IntentWritten, journal.Dispatched, journal.UnknownEffect:
		if r.Status != effectgateway.StatusUnknown || r.ProofDigest != "" {
			return nil, code
		}
		effect.Outcome = "UNKNOWN_EFFECT"
	case "":
		if r.Status != effectgateway.StatusDuplicate || r.ProofDigest != "" || r.ProviderCalls != 0 {
			return nil, code
		}
		effect.Outcome = "DUPLICATE"
	default:
		return nil, code
	}
	if action == effectgateway.ReadStatus {
		code = 0
	}
	return effect, code
}
