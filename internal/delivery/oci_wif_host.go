package delivery

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/identity/ociwif"
)

// OCIWIFRegistration holds the immutable instance and provider mapping from
// the admitted host's configuration. Workload input cannot select these fields.
type OCIWIFRegistration struct {
	Profile                                          string
	TenancyAlias, InstanceAlias, IdentityDomainAlias string
	ProviderAlias, ServiceAccountAlias               string
	Region, IdentityDomainEndpoint, Issuer, Audience string
	TenancyOCID, InstanceOCID, DomainID              string
	ProviderID, ServiceAccountID                     string
}

type OCIWIFHost struct {
	policy    ociwif.Policy
	signer    ociwif.Signer
	exchanger ociwif.Exchanger
}

// OpenRegisteredOCIWIF checks the selected profile and opens the fixed OCI and
// OpenAI routes. Only Get contacts either provider.
func OpenRegisteredOCIWIF(profiles Profiles, reg OCIWIFRegistration) (*OCIWIFHost, error) {
	p, ok := profiles.entries[reg.Profile]
	if !ok {
		return nil, ErrUnknownProfile
	}
	if p.Provider != ociWIF || p.TenancyAlias != reg.TenancyAlias ||
		p.InstanceAlias != reg.InstanceAlias || p.IdentityDomainAlias != reg.IdentityDomainAlias ||
		p.WIFProviderAlias != reg.ProviderAlias || p.ServiceAccountAlias != reg.ServiceAccountAlias {
		return nil, errors.New("OCI WIF registration does not match the profile")
	}
	if reg.Issuer != ociwif.Issuer ||
		!strings.HasPrefix(reg.TenancyOCID, "ocid1.tenancy.oc1..") ||
		!strings.HasPrefix(reg.InstanceOCID, "ocid1.instance.oc1.") ||
		!strings.HasPrefix(reg.DomainID, "ocid1.domain.oc1..") {
		return nil, errors.New("invalid OCI WIF registration identity")
	}
	for _, value := range []string{reg.Audience, reg.TenancyOCID, reg.InstanceOCID, reg.DomainID, reg.ProviderID, reg.ServiceAccountID} {
		if value == "" || strings.IndexFunc(value, func(ch rune) bool { return ch < 33 || ch > 126 }) >= 0 {
			return nil, errors.New("invalid OCI WIF registration value")
		}
	}
	signer, err := ociwif.NewInstanceSigner(reg.Region, reg.IdentityDomainEndpoint)
	if err != nil {
		return nil, err
	}
	return &OCIWIFHost{
		policy: ociwif.Policy{
			Issuer: reg.Issuer, Audience: reg.Audience, DomainID: reg.DomainID,
			TenancyOCID: reg.TenancyOCID, Instances: []string{reg.InstanceOCID},
			IdentityProvider: reg.ProviderID, ServiceAccount: reg.ServiceAccountID,
			MaxSourceLifetime: time.Hour, MaxTokenLifetime: time.Hour,
		},
		signer: signer, exchanger: ociwif.NewOpenAIExchanger(),
	}, nil
}

func (h *OCIWIFHost) Get(ctx context.Context) (ociwif.Credential, error) {
	if h == nil {
		return ociwif.Credential{}, ociwif.ErrMissingComponent
	}
	return h.policy.Acquire(ctx, h.signer, h.exchanger)
}
