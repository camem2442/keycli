package delivery

import "testing"

func TestOpenRegisteredOCIWIF(t *testing.T) {
	profiles, err := ParseProfiles([]byte(cloudflareProfile + otherProfiles))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*OCIWIFRegistration)
	}{
		{name: "registered host"},
		{name: "wrong alias", change: func(r *OCIWIFRegistration) { r.InstanceAlias = "other" }},
		{name: "missing instance", change: func(r *OCIWIFRegistration) { r.InstanceOCID = "" }},
		{name: "wrong endpoint", change: func(r *OCIWIFRegistration) { r.IdentityDomainEndpoint = "https://example.invalid/oauth2/v1/token" }},
		{name: "missing audience", change: func(r *OCIWIFRegistration) { r.Audience = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := OCIWIFRegistration{
				Profile: "oracle_ci", TenancyAlias: "fixture-tenancy", InstanceAlias: "fixture-instance",
				IdentityDomainAlias: "fixture-domain", ProviderAlias: "oci-fixture", ServiceAccountAlias: "oci-service",
				Region: "us-phoenix-1", IdentityDomainEndpoint: "https://idcs-fixture.identity.oraclecloud.com/oauth2/v1/token",
				Issuer: "https://identity.oraclecloud.com/", Audience: "https://idcs-fixture.identity.oraclecloud.com",
				TenancyOCID: "ocid1.tenancy.oc1..fixture", InstanceOCID: "ocid1.instance.oc1.phx.fixture", DomainID: "ocid1.domain.oc1..fixture",
				ProviderID: "provider-fixture", ServiceAccountID: "service-fixture",
			}
			if tc.change != nil {
				tc.change(&r)
			}
			h, err := OpenRegisteredOCIWIF(profiles, r)
			if tc.change != nil {
				if h != nil || err == nil {
					t.Fatal("invalid OCI registration accepted")
				}
				return
			}
			if err != nil || h == nil || h.policy.Audience != r.Audience || h.policy.Instances[0] != r.InstanceOCID {
				t.Fatalf("valid registration did not bind OCI host: %v", err)
			}
		})
	}
}
