package api

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/go-github/v69/github"
)

// CollaboratorsService abstracts GitHub REST read operations for repository collaborators.
type CollaboratorsService interface {
	ListCollaborators(ctx context.Context, owner, repo string, opts *github.ListCollaboratorsOptions) ([]*github.User, *github.Response, error)
}

// CollaboratorStore persists collaborator data.
type CollaboratorStore interface {
	ListDistinctRepos() ([]string, error)
	ReplaceCollaborators(repo string, logins []string) error
}

// CollaboratorsFetcher fetches repository collaborators from GitHub and writes
// them to the persistence layer. It discovers repos from the pull_requests table
// and fetches collaborators for each.
type CollaboratorsFetcher struct {
	collabs CollaboratorsService
	store   CollaboratorStore
}

// NewCollaboratorsFetcher constructs a CollaboratorsFetcher.
func NewCollaboratorsFetcher(collabs CollaboratorsService, store CollaboratorStore) *CollaboratorsFetcher {
	return &CollaboratorsFetcher{collabs: collabs, store: store}
}

// Fetch discovers repos from the DB and fetches collaborators for each.
func (f *CollaboratorsFetcher) Fetch(ctx context.Context) error {
	repos, err := f.store.ListDistinctRepos()
	if err != nil {
		return err
	}
	for _, repo := range repos {
		owner, name, err := parseRepo(repo)
		if err != nil {
			slog.Debug("collaborators: skipping invalid repo", "repo", repo, "err", err)
			continue
		}
		logins, err := f.fetchAllCollaborators(ctx, owner, name)
		if err != nil {
			slog.Debug("collaborators: fetch failed", "repo", repo, "err", err)
			continue
		}
		if err := f.store.ReplaceCollaborators(repo, logins); err != nil {
			slog.Debug("collaborators: store failed", "repo", repo, "err", err)
			continue
		}
	}
	return nil
}

// fetchAllCollaborators pages through all collaborators for a repository.
func (f *CollaboratorsFetcher) fetchAllCollaborators(ctx context.Context, owner, name string) ([]string, error) {
	var allLogins []string
	opts := &github.ListCollaboratorsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	for {
		users, resp, err := f.collabs.ListCollaborators(ctx, owner, name, opts)
		if err != nil {
			// 403 means we don't have permission — return what we have from other sources.
			if resp != nil && resp.StatusCode == 403 {
				slog.Debug("collaborators: 403 forbidden, skipping", "repo", strings.Join([]string{owner, name}, "/"))
				return nil, nil
			}
			return nil, err
		}
		for _, u := range users {
			if u.GetLogin() != "" {
				allLogins = append(allLogins, u.GetLogin())
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return allLogins, nil
}
