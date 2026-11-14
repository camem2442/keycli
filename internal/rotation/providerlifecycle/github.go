package providerlifecycle

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// DeleteGitHubGenerationSecret removes a SINGLE generation-named Actions
// repository Secret. Deleting it DOES NOT revoke the external provider key.
// Must be called as the rotation.Finalize revoke callback after its separate
// host grant, live independent provideridentity GET and durable journal intent.
// GitHub has no compare-and-delete; the host must fence all writers across
// machines for this repository+secret. The caller must not retry an unknown effect.
func (c Client) DeleteGitHubGenerationSecret(ctx context.Context, gate HostAuthorize, repo, name string, repoID, ownerID int64, generation uint64) (Receipt, error) {
	return c.deleteGitHubGenerationSecret(ctx, gate, repo, name, repoID, ownerID, generation, nil)
}

// DeleteGitHubGenerationSecretWithFence lets Finalize check its execution
// authority after signing and immediately before dispatching the DELETE.
func (c Client) DeleteGitHubGenerationSecretWithFence(ctx context.Context, gate HostAuthorize, repo, name string, repoID, ownerID int64, generation uint64, beforeSend func(context.Context) error) (Receipt, error) {
	if beforeSend == nil {
		return Receipt{}, errors.New("GitHub final revocation fence required")
	}
	return c.deleteGitHubGenerationSecret(ctx, gate, repo, name, repoID, ownerID, generation, beforeSend)
}

func (c Client) deleteGitHubGenerationSecret(ctx context.Context, gate HostAuthorize, repo, name string, repoID, ownerID int64, generation uint64, beforeSend func(context.Context) error) (Receipt, error) {
	target, err := GitHubGenerationTarget(repo, name, repoID, ownerID, generation)
	if err != nil {
		return Receipt{}, err
	}
	checkTarget := func(ctx context.Context) error {
		return authorize(ctx, gate, "github", "delete-exact-repository-secret", target)
	}
	if err := checkTarget(ctx); err != nil {
		return Receipt{}, err
	}
	if beforeSend == nil {
		beforeSend = checkTarget
	}
	// IDs were bound by provideridentity.GitHubRepositorySecret immediately
	// before calling this function; never infer them from repo spelling.
	headers := map[string]string{"X-GitHub-Api-Version": "2026-03-10", "Accept": "application/vnd.github+json"}
	_, err = c.callBeforeSend(ctx, http.MethodDelete, "https://api.github.com", "/repos/"+repo+"/actions/secrets/"+name, nil, headers, beforeSend, http.StatusNoContent)
	if err != nil {
		return Receipt{}, err
	}
	return receipt("github", "delete-repository-secret", strconv.FormatInt(repoID, 10), name), nil
}

// GitHubGenerationTarget binds an exact generation name to immutable IDs.
func GitHubGenerationTarget(repo, name string, repoID, ownerID int64, generation uint64) (string, error) {
	if !validRepo(repo) || !generationName.MatchString(name) || generation == 0 || repoID <= 0 || ownerID <= 0 ||
		!strings.HasSuffix(name, "_GEN_"+strconv.FormatUint(generation, 10)) {
		return "", errors.New("invalid exact GitHub generation")
	}
	return fmt.Sprintf("repo=%d owner=%d %s/actions/secrets/%s", repoID, ownerID, repo, name), nil
}
