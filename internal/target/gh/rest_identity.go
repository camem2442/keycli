package gh

import (
	"context"
	"fmt"
	"strings"
)

const (
	githubReposPerPage = 100
	maxGitHubRepoPages = 20
)

type repositoryIdentity struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
	Owner    struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	} `json:"owner"`
}

func (c *SecretRESTClient) verifyTarget(ctx context.Context, target SecretRESTTarget) error {
	if target.Scope == "organization" {
		return c.verifyOrganization(ctx, target)
	}
	path := "/repos/" + target.Repository
	var repo repositoryIdentity
	if err := c.getJSON(ctx, path, &repo); err != nil {
		return err
	}
	owner, _, _ := strings.Cut(target.Repository, "/")
	if repo.ID != target.RepositoryID || repo.Owner.ID != target.RepositoryOwnerID ||
		!strings.EqualFold(repo.FullName, target.Repository) || !strings.EqualFold(repo.Owner.Login, owner) {
		return errTargetIdentity
	}
	if target.Scope == "environment" {
		var env struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}
		if err := c.getJSON(ctx, path+"/environments/"+target.Environment, &env); err != nil {
			return err
		}
		if env.ID != target.EnvironmentID || !strings.EqualFold(env.Name, target.Environment) {
			return errTargetIdentity
		}
	}
	return nil
}

func validSelectedRepositories(ids []int64) bool {
	if len(ids) == 0 || len(ids) > githubReposPerPage*maxGitHubRepoPages {
		return false
	}
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

func (c *SecretRESTClient) verifyOrganization(ctx context.Context, target SecretRESTTarget) error {
	path := "/orgs/" + target.Organization
	var org struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	if err := c.getJSON(ctx, path, &org); err != nil {
		return err
	}
	if org.ID != target.OrganizationID || !strings.EqualFold(org.Login, target.Organization) {
		return errTargetIdentity
	}

	remaining := make(map[int64]bool, len(target.SelectedRepositoryIDs))
	for _, id := range target.SelectedRepositoryIDs {
		remaining[id] = true
	}
	for page := 1; page <= maxGitHubRepoPages; page++ {
		var repos []repositoryIdentity
		// Construct each page locally rather than following response-supplied URLs.
		pagePath := fmt.Sprintf("%s/repos?type=all&per_page=%d&page=%d", path, githubReposPerPage, page)
		if err := c.getJSON(ctx, pagePath, &repos); err != nil {
			return err
		}
		for _, repo := range repos {
			if !remaining[repo.ID] {
				continue
			}
			if repo.Owner.ID != target.OrganizationID || !strings.EqualFold(repo.Owner.Login, target.Organization) {
				return errTargetIdentity
			}
			delete(remaining, repo.ID)
		}
		if len(remaining) == 0 {
			return nil
		}
		if len(repos) < githubReposPerPage {
			break
		}
	}
	return errTargetIdentity
}
