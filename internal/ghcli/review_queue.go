package ghcli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/evanisnor/argh/internal/api"
	"github.com/evanisnor/argh/internal/persistence"
	"github.com/shurcooL/githubv4"
)

// ghSearchRQPR extends ghSearchPR with commit data for review queue PRs.
type ghSearchRQPR struct {
	ghSearchPR
	Commits []ghCommit `json:"commits"`
}

type ghCommit struct {
	AuthorLogin   string    `json:"authorLogin"`
	CommittedDate time.Time `json:"committedDate"`
	// The gh search prs --json commits returns an object with nested fields.
	// We also support the authors array format.
	Authors []ghCommitAuthor `json:"authors"`
}

type ghCommitAuthor struct {
	Login string `json:"login"`
}

// GHCLIReviewQueueFetcher fetches PRs where the user is a requested reviewer via `gh search prs`.
type GHCLIReviewQueueFetcher struct {
	runner CommandRunner
	store  api.ReviewQueueStore
	bus    api.Publisher
	login  string
}

// NewGHCLIReviewQueueFetcher creates a new GHCLIReviewQueueFetcher.
func NewGHCLIReviewQueueFetcher(runner CommandRunner, store api.ReviewQueueStore, bus api.Publisher, login string) *GHCLIReviewQueueFetcher {
	return &GHCLIReviewQueueFetcher{
		runner: runner,
		store:  store,
		bus:    bus,
		login:  login,
	}
}

// Fetch queries for open PRs where the user is a requested reviewer and persists them.
func (f *GHCLIReviewQueueFetcher) Fetch(ctx context.Context) error {
	args := []string{
		"search", "prs",
		"--review-requested", f.login,
		"--state", "open",
		"--limit", "100",
		"--json", "id,number,title,state,isDraft,url,createdAt,updatedAt,author,repository,statusCheckRollup,reviews,reviewRequests,commits",
	}

	out, err := f.runner.Run(ctx, args)
	if err != nil {
		return fmt.Errorf("fetching review queue via gh: %w", err)
	}

	var prs []ghSearchRQPR
	if err := json.Unmarshal(out, &prs); err != nil {
		return fmt.Errorf("parsing gh search prs output: %w", err)
	}

	slog.Debug("ghcli: fetched review queue prs", "count", len(prs))

	for _, p := range prs {
		repo := p.Repository.NameWithOwner
		if repo == "" {
			continue
		}

		runs := convertStatusChecks(p.StatusCheckRollup)
		reviews := convertReviews(p.Reviews)
		commits := convertCommits(p.Commits)

		prRow := persistence.PullRequest{
			ID:             p.ID,
			Repo:           repo,
			Number:         p.Number,
			Title:          p.Title,
			Status:         api.DerivePRStatus(false, p.IsDraft, reviews),
			CIState:        api.DeriveCIState(runs),
			Draft:          p.IsDraft,
			Author:         p.Author.Login,
			CreatedAt:      p.CreatedAt,
			UpdatedAt:      p.UpdatedAt,
			LastActivityAt: p.UpdatedAt,
			URL:            p.URL,
			GlobalID:       p.ID,
		}

		if err := api.PersistRQPR(f.store, f.bus, prRow, runs, reviews, commits); err != nil {
			return err
		}
	}

	return nil
}

// convertCommits converts gh CLI commit data to the shared CommitData type.
func convertCommits(commits []ghCommit) []api.CommitData {
	var result []api.CommitData
	for _, c := range commits {
		login := c.AuthorLogin
		if login == "" && len(c.Authors) > 0 {
			login = c.Authors[0].Login
		}
		result = append(result, api.CommitData{
			AuthorLogin:   login,
			CommittedDate: githubv4.DateTime{Time: c.CommittedDate},
		})
	}
	return result
}
