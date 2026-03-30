package ghcli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/evanisnor/argh/internal/api"
)

// GHCLICollaboratorsFetcher fetches repository collaborators via the gh CLI
// and writes them to the persistence layer. It discovers repos from the
// pull_requests table and fetches collaborators for each.
type GHCLICollaboratorsFetcher struct {
	runner CommandRunner
	store  api.CollaboratorStore
}

// NewGHCLICollaboratorsFetcher constructs a GHCLICollaboratorsFetcher.
func NewGHCLICollaboratorsFetcher(runner CommandRunner, store api.CollaboratorStore) *GHCLICollaboratorsFetcher {
	return &GHCLICollaboratorsFetcher{runner: runner, store: store}
}

// ghCollaborator models the subset of the GitHub user object we need.
type ghCollaborator struct {
	Login string `json:"login"`
}

// Fetch discovers repos from the DB and fetches collaborators for each.
func (f *GHCLICollaboratorsFetcher) Fetch(ctx context.Context) error {
	repos, err := f.store.ListDistinctRepos()
	if err != nil {
		return err
	}
	for _, repo := range repos {
		owner, name, err := splitRepo(repo)
		if err != nil {
			slog.Debug("ghcli collaborators: skipping invalid repo", "repo", repo, "err", err)
			continue
		}
		logins, err := f.fetchCollaborators(ctx, owner, name)
		if err != nil {
			slog.Debug("ghcli collaborators: fetch failed", "repo", repo, "err", err)
			continue
		}
		if err := f.store.ReplaceCollaborators(repo, logins); err != nil {
			slog.Debug("ghcli collaborators: store failed", "repo", repo, "err", err)
			continue
		}
	}
	return nil
}

// fetchCollaborators calls the GitHub REST API via gh to list collaborators
// for a single repository. The --paginate flag handles multi-page responses.
func (f *GHCLICollaboratorsFetcher) fetchCollaborators(ctx context.Context, owner, name string) ([]string, error) {
	out, err := f.runner.Run(ctx, []string{
		"api", fmt.Sprintf("repos/%s/%s/collaborators", owner, name),
		"--paginate",
	})
	if err != nil {
		if strings.Contains(err.Error(), "HTTP 403") || strings.Contains(err.Error(), "403") {
			slog.Debug("ghcli collaborators: 403 forbidden, skipping", "repo", owner+"/"+name)
			return nil, nil
		}
		return nil, err
	}
	var users []ghCollaborator
	if err := json.Unmarshal(out, &users); err != nil {
		return nil, fmt.Errorf("parsing collaborators for %s/%s: %w", owner, name, err)
	}
	logins := make([]string, 0, len(users))
	for _, u := range users {
		if u.Login != "" {
			logins = append(logins, u.Login)
		}
	}
	return logins, nil
}
