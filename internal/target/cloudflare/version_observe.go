package cloudflare

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

// VersionRESTObserver reads version and deployment metadata from Cloudflare.
// It cannot read secret values, grant mutations, or prove that a secret works.
// The token must be narrowly scoped by the trusted host.
type VersionRESTObserver struct {
	client *http.Client
	token  string
}

type VersionPercentage struct {
	VersionID  string  `json:"version_id"`
	Percentage float64 `json:"percentage"`
}

type DeploymentSnapshot struct {
	AccountID        string
	Worker           string
	DeploymentID     string
	Versions         []VersionPercentage
	LatestVersionIDs []string
}

type VersionMetadata struct {
	ID           string
	Tag          string
	BindingNames []string
}

var versionUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

const cloudflareAPIOrigin = "https://api.cloudflare.com/client/v4"

func NewVersionRESTObserver(token string) (*VersionRESTObserver, error) {
	transport := &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSNextProto:        map[string]func(string, *tls.Conn) http.RoundTripper{},
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return newVersionRESTObserver(token, transport)
}

func newVersionRESTObserver(token string, transport http.RoundTripper) (*VersionRESTObserver, error) {
	if token == "" || len(token) > 4096 || strings.ContainsAny(token, "\r\n ") || transport == nil {
		return nil, errors.New("invalid Cloudflare read-only observer credential")
	}
	return &VersionRESTObserver{token: token, client: &http.Client{
		Transport: transport, Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// Observe reads one CURRENT deployment and the newest version IDs. Any missing,
// ambiguous, truncated or malformed provider response fails closed.
func (o *VersionRESTObserver) Observe(ctx context.Context, accountID, worker string) (DeploymentSnapshot, error) {
	if o == nil || o.client == nil || !stageAccount.MatchString(accountID) || !stageWorker.MatchString(worker) {
		return DeploymentSnapshot{}, errors.New("invalid Cloudflare observer target")
	}
	base := "/accounts/" + accountID + "/workers/scripts/" + worker
	var deployments struct {
		Deployments []struct {
			ID       string              `json:"id"`
			Versions []VersionPercentage `json:"versions"`
		} `json:"deployments"`
	}
	if err := o.get(ctx, base+"/deployments?page=1&per_page=2", &deployments); err != nil {
		return DeploymentSnapshot{}, err
	}
	if len(deployments.Deployments) == 0 || len(deployments.Deployments) > 2 || !versionUUID.MatchString(deployments.Deployments[0].ID) {
		return DeploymentSnapshot{}, errors.New("Cloudflare current deployment unavailable")
	}
	current := deployments.Deployments[0]
	if !validWeights(current.Versions) {
		return DeploymentSnapshot{}, errors.New("invalid Cloudflare active traffic distribution")
	}
	var versions []struct {
		ID string `json:"id"`
	}
	if err := o.get(ctx, base+"/versions?page=1&per_page=100", &versions); err != nil {
		return DeploymentSnapshot{}, err
	}
	if len(versions) == 0 || len(versions) > 100 {
		return DeploymentSnapshot{}, errors.New("Cloudflare Worker versions unavailable")
	}
	ids := make([]string, 0, len(versions))
	seen := map[string]bool{}
	for _, v := range versions {
		id := strings.ToLower(v.ID)
		if !versionUUID.MatchString(id) || seen[id] {
			return DeploymentSnapshot{}, errors.New("invalid Cloudflare version identity")
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return DeploymentSnapshot{AccountID: accountID, Worker: worker, DeploymentID: strings.ToLower(current.ID), Versions: slices.Clone(current.Versions), LatestVersionIDs: ids}, nil
}

func validWeights(versions []VersionPercentage) bool {
	if len(versions) < 1 || len(versions) > 2 {
		return false
	}
	seen := map[string]bool{}
	total := 0.0
	for _, v := range versions {
		id := strings.ToLower(v.VersionID)
		if !versionUUID.MatchString(id) || seen[id] || v.Percentage <= 0 || v.Percentage > 100 {
			return false
		}
		seen[id] = true
		total += v.Percentage
	}
	return total >= 99.999999 && total <= 100.000001
}

// GetVersion reads a specific version's tag and binding names. This is not
// proof that a secret value equals the requested source value.
func (o *VersionRESTObserver) GetVersion(ctx context.Context, accountID, worker, versionID string) (VersionMetadata, error) {
	if o == nil || o.client == nil || !stageAccount.MatchString(accountID) || !stageWorker.MatchString(worker) || !versionUUID.MatchString(versionID) {
		return VersionMetadata{}, errors.New("invalid version lookup target")
	}
	var result struct {
		ID       string `json:"id"`
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
		Resources struct {
			Bindings json.RawMessage `json:"bindings"`
		} `json:"resources"`
	}
	if err := o.get(ctx, "/accounts/"+accountID+"/workers/scripts/"+worker+"/versions/"+versionID, &result); err != nil {
		return VersionMetadata{}, err
	}
	if !strings.EqualFold(result.ID, versionID) {
		return VersionMetadata{}, errors.New("Cloudflare version detail identity mismatch")
	}
	var bindings []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if len(result.Resources.Bindings) > 0 {
		if err := json.Unmarshal(result.Resources.Bindings, &bindings); err != nil {
			return VersionMetadata{}, errors.New("Cloudflare version bindings cannot be authenticated")
		}
	}
	names := []string{}
	for _, b := range bindings {
		if b.Name != "" && b.Type == "secret_text" {
			names = append(names, b.Name)
		}
	}
	slices.Sort(names)
	return VersionMetadata{ID: strings.ToLower(result.ID), Tag: result.Metadata.Annotations["workers/tag"], BindingNames: names}, nil
}

func (o *VersionRESTObserver) get(ctx context.Context, path string, dst any) error {
	u, err := url.Parse(cloudflareAPIOrigin + path)
	if err != nil || u.Host != "api.cloudflare.com" || u.Scheme != "https" {
		return errors.New("invalid Cloudflare GET endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return errors.New("cannot create Cloudflare observation")
	}
	req.Header.Set("Authorization", "Bearer "+o.token)
	req.Header.Set("Accept", "application/json")
	resp, err := o.client.Do(req)
	if err != nil {
		return errors.New("Cloudflare observation transport failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Cloudflare observation returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil || len(data) > 1<<20 {
		return errors.New("Cloudflare response exceeded limit")
	}
	var envelope struct {
		Success bool            `json:"success"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || !envelope.Success || len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return errors.New("Cloudflare observation response invalid")
	}
	if err := json.Unmarshal(envelope.Result, dst); err != nil {
		return errors.New("Cloudflare observation structure unsupported")
	}
	return nil
}
