package delivery

import (
	"slices"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/config"
)

const gitHubSecretProfile = `schema_version = 1
[profiles.fixture]
provider = "github-actions-secret"
source = "fixture_credential"
destination = "FIXTURE_DESTINATION"
effect = "immediate-secret-update"
`

func TestGitHubSecretPlanSeparatesWriteScopes(t *testing.T) {
	metadata, err := NewMetadataSnapshot(config.Config{}, "ci")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		scope, fields string
		want          TargetRef
	}{
		{"repository", "repository = \"example/fixture\"\n", TargetRef{Repository: "example/fixture"}},
		{"environment", "repository = \"example/fixture\"\nenvironment = \"staging\"\n", TargetRef{Repository: "example/fixture", Environment: "staging"}},
		{"organization", "organization = \"example\"\nselected_repository_ids = [19, 2]\n", TargetRef{Organization: "example", SelectedRepositoryIDs: []int64{2, 19}}},
	} {
		t.Run(tc.scope, func(t *testing.T) {
			input := gitHubSecretProfile + "scope = \"" + tc.scope + "\"\n" + tc.fields
			profiles, err := ParseProfiles([]byte(input))
			if err != nil {
				t.Fatal(err)
			}
			plan, err := BuildPlan(profiles, "fixture", metadata)
			if err != nil {
				t.Fatal(err)
			}
			target := plan.Target
			if target.Scope != tc.scope || target.Repository != tc.want.Repository ||
				target.Environment != tc.want.Environment || target.Organization != tc.want.Organization ||
				!slices.Equal(target.SelectedRepositoryIDs, tc.want.SelectedRepositoryIDs) ||
				plan.Source == nil || plan.WIF != nil || plan.Effect != "immediate-secret-update" || plan.AuthorizesExecution {
				t.Fatalf("unexpected GitHub Secret plan: %+v", plan)
			}
			if tc.scope == "organization" {
				reordered, err := ParseProfiles([]byte(strings.Replace(input, "[19, 2]", "[2, 19]", 1)))
				if err != nil {
					t.Fatal(err)
				}
				other, err := BuildPlan(reordered, "fixture", metadata)
				if err != nil || other.ID != plan.ID {
					t.Fatal("equivalent repository selection changed plan identity")
				}
			}
		})
	}
}

func TestGitHubSecretPlanRejectsBroadOrMixedScope(t *testing.T) {
	for name, fields := range map[string]string{
		"missing selected IDs":      "scope = \"organization\"\norganization = \"example\"\n",
		"empty selected IDs":        "scope = \"organization\"\norganization = \"example\"\nselected_repository_ids = []\n",
		"duplicate ID":              "scope = \"organization\"\norganization = \"example\"\nselected_repository_ids = [2, 2]\n",
		"zero ID":                   "scope = \"organization\"\norganization = \"example\"\nselected_repository_ids = [0]\n",
		"environment on repository": "scope = \"repository\"\nrepository = \"example/fixture\"\nenvironment = \"\"\n",
		"environment missing":       "scope = \"environment\"\nrepository = \"example/fixture\"\n",
		"mixed org":                 "scope = \"repository\"\nrepository = \"example/fixture\"\norganization = \"example\"\n",
		"unsupported scope":         "scope = \"all\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseProfiles([]byte(gitHubSecretProfile + fields)); err == nil {
				t.Fatal("unsafe Secret scope accepted")
			}
		})
	}
}
