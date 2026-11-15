package provideridentity

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/rotation"
)

// GitHubRepositorySecret observes one generation-specific repository Actions
// Secret. GitHub GET NEVER reveals the secret value. Old CreatedAt/UpdatedAt
// must be pinned by a trusted host issuer receipt. No server-side compare-and-
// delete exists: the high-risk revoker needs an independent Writer fence.
var githubSecretGenerationName = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,90}_GEN_[1-9][0-9]*$`)

type GitHubRepositorySecret struct {
	Reader                               Reader
	PlanTarget                           string
	Repository                           string
	RepositoryID, OwnerID                int64
	SecretName                           string
	ExpectedCreatedAt, ExpectedUpdatedAt string
}

func (o GitHubRepositorySecret) Verify(ctx context.Context, p rotation.PreparationPlan, handle string) (rotation.VerifiedOldCredential, error) {
	invalid := errors.New("GitHub secret identity unavailable or changed")
	owner, repo, ok := strings.Cut(o.Repository, "/")
	if p.Old.Provider != "github" || p.Old.Kind != rotation.StaticServiceKey || !validPlan(p) || p.Old.Number == 0 ||
		p.Old.Target != o.PlanTarget || o.PlanTarget == "" || !ok || !exactPart(owner) || !exactPart(repo) || owner == "." || owner == ".." || repo == "." || repo == ".." ||
		strings.Contains(repo, "..") || o.RepositoryID <= 0 || o.OwnerID <= 0 || o.SecretName != handle ||
		!strings.HasSuffix(handle, "_GEN_"+strconv.FormatUint(p.Old.Number, 10)) || !githubSecretGenerationName.MatchString(handle) ||
		!eqTime(o.ExpectedCreatedAt, o.ExpectedCreatedAt) || !eqTime(o.ExpectedUpdatedAt, o.ExpectedUpdatedAt) {
		return rotation.VerifiedOldCredential{}, invalid
	}
	var repository struct {
		ID       int64  `json:"id"`
		FullName string `json:"full_name"`
		Owner    struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
		} `json:"owner"`
	}
	if err := o.Reader.read(ctx, "https://api.github.com", "/repos/"+o.Repository, &repository, map[string]string{"X-GitHub-Api-Version": "2026-03-10"}); err != nil {
		return rotation.VerifiedOldCredential{}, invalid
	}
	if repository.ID != o.RepositoryID || repository.Owner.ID != o.OwnerID || !strings.EqualFold(repository.FullName, o.Repository) || !strings.EqualFold(repository.Owner.Login, owner) {
		return rotation.VerifiedOldCredential{}, invalid
	}
	var sec struct {
		Name      string `json:"name"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	}
	if err := o.Reader.read(ctx, "https://api.github.com", "/repos/"+o.Repository+"/actions/secrets/"+handle, &sec, map[string]string{"X-GitHub-Api-Version": "2026-03-10"}); err != nil {
		return rotation.VerifiedOldCredential{}, invalid
	}
	if sec.Name != o.SecretName || !eqTime(sec.CreatedAt, o.ExpectedCreatedAt) || !eqTime(sec.UpdatedAt, o.ExpectedUpdatedAt) {
		return rotation.VerifiedOldCredential{}, invalid
	}
	id := rotation.VerifiedOldCredential{Provider: "github", Kind: "github-actions-secret", PlanDigest: p.ID, PlanTarget: p.Old.Target,
		AccountID: strconv.FormatInt(o.OwnerID, 10), ResourceID: strconv.FormatInt(o.RepositoryID, 10), CredentialID: handle,
		Generation: p.Old.Number, ObservedAt: time.Now().UTC(), ProofDigest: shaMetadata(struct {
			RepoID, OwnerID        int64
			Name, Created, Updated string
		}{o.RepositoryID, o.OwnerID, sec.Name, sec.CreatedAt, sec.UpdatedAt})}
	return id, nil
}
