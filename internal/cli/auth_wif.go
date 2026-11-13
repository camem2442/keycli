package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/camem2442/keycli/internal/delivery"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/spf13/cobra"
)

type wifCredential interface {
	Authorize(*http.Request) error
	ExpiresAt() time.Time
}

type wifCheckRegistration struct {
	Version       int
	DeliveryTOML  string
	Model         string
	Provider      string
	Binding       json.RawMessage
	ApprovedUntil time.Time
	Digest        string
}

var wifModelID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

func newWIFCheckCmd() *cobra.Command {
	var path, digest string
	cmd := &cobra.Command{
		Use:   "wif-check",
		Short: "Check registered workload access to one OpenAI model",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			reg, err := readWIFCheckRegistration(path, digest)
			if err != nil {
				return err
			}
			profiles, err := delivery.ParseProfiles([]byte(reg.DeliveryTOML))
			if err != nil {
				return errors.New("invalid WIF delivery profiles")
			}
			deadline := time.Now().Add(75 * time.Second)
			if reg.ApprovedUntil.Before(deadline) {
				deadline = reg.ApprovedUntil
			}
			ctx, cancel := context.WithDeadline(cmd.Context(), deadline)
			defer cancel()
			credential, err := acquireRegisteredWIF(ctx, profiles, reg)
			if err != nil {
				return err
			}
			return checkWIFModel(ctx, newWIFModelClient(), reg, credential, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&path, "registration", "", "Absolute path to an owner-only workload registration")
	cmd.Flags().StringVar(&digest, "registration-sha256", "", "SHA-256 of the approved registration")
	_ = cmd.MarkFlagRequired("registration")
	_ = cmd.MarkFlagRequired("registration-sha256")
	return cmd
}

func readWIFCheckRegistration(path, digest string) (wifCheckRegistration, error) {
	invalid := errors.New("invalid or mismatched WIF registration")
	approved, err := hex.DecodeString(digest)
	if err != nil || len(approved) != sha256.Size {
		return wifCheckRegistration{}, invalid
	}
	raw, err := effectgateway.ReadPrivateFile(path, 65536)
	if err != nil {
		return wifCheckRegistration{}, invalid
	}
	actual := sha256.Sum256(raw)
	var reg wifCheckRegistration
	if !bytes.Equal(approved, actual[:]) || !effectgateway.DecodePrivateConfig(raw, map[string]any{
		"v": &reg.Version, "delivery_toml": &reg.DeliveryTOML, "model": &reg.Model,
		"provider": &reg.Provider, "binding": &reg.Binding, "approved_until": &reg.ApprovedUntil,
	}) || reg.Version != 1 || !time.Now().Before(reg.ApprovedUntil) || !wifModelID.MatchString(reg.Model) ||
		(reg.Provider != "github-actions-wif" && reg.Provider != "oci-instance-wif") {
		return wifCheckRegistration{}, invalid
	}
	reg.Digest = hex.EncodeToString(actual[:])
	return reg, nil
}

func acquireRegisteredWIF(ctx context.Context, profiles delivery.Profiles, reg wifCheckRegistration) (wifCredential, error) {
	decoder := json.NewDecoder(bytes.NewReader(reg.Binding))
	decoder.DisallowUnknownFields()
	invalid := errors.New("invalid WIF host binding")
	switch reg.Provider {
	case "github-actions-wif":
		var binding delivery.GitHubWIFRegistration
		if decoder.Decode(&binding) != nil {
			return nil, invalid
		}
		exchange, err := delivery.OpenRegisteredGitHubWIF(profiles, binding)
		if err != nil {
			return nil, invalid
		}
		return exchange.Get(ctx)
	case "oci-instance-wif":
		var binding delivery.OCIWIFRegistration
		if decoder.Decode(&binding) != nil {
			return nil, invalid
		}
		host, err := delivery.OpenRegisteredOCIWIF(profiles, binding)
		if err != nil {
			return nil, invalid
		}
		return host.Get(ctx)
	default:
		return nil, invalid
	}
}

func newWIFModelClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
			TLSNextProto:      map[string]func(string, *tls.Conn) http.RoundTripper{},
			DisableKeepAlives: true,
		},
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func checkWIFModel(ctx context.Context, client *http.Client, reg wifCheckRegistration, credential wifCredential, out io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.openai.com/v1/models/"+reg.Model, nil)
	if err != nil || credential.Authorize(req) != nil {
		return errors.New("OpenAI WIF credential could not authorize the model check")
	}
	response, err := client.Do(req)
	if err != nil {
		return errors.New("OpenAI WIF model check transport failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("OpenAI WIF model check returned HTTP %d", response.StatusCode)
	}
	var model struct {
		ID     string `json:"id"`
		Object string `json:"object"`
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	expiry := credential.ExpiresAt()
	if err != nil || len(raw) > 65536 || json.Unmarshal(raw, &model) != nil ||
		model.ID != reg.Model || model.Object != "model" || ctx.Err() != nil || !time.Now().Before(expiry) {
		return errors.New("invalid OpenAI WIF model check result")
	}
	return json.NewEncoder(out).Encode(struct {
		Version            int       `json:"v"`
		Provider           string    `json:"provider"`
		Model              string    `json:"model"`
		ExpiresAt          time.Time `json:"expires_at"`
		HTTPStatus         int       `json:"http_status"`
		RegistrationSHA256 string    `json:"registration_sha256"`
	}{1, reg.Provider, reg.Model, expiry.UTC(), response.StatusCode, reg.Digest})
}
