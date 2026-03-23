package ghcli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/evanisnor/argh/internal/api"
	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
	"github.com/shurcooL/githubv4"
)

type ghCommit struct {
	AuthorLogin   string           `json:"authorLogin"`
	CommittedDate time.Time        `json:"committedDate"`
	Authors       []ghCommitAuthor `json:"authors"`
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
// Phase 1: gh search prs for the PR list (supported fields only).
// Phase 2: gh pr view per PR for statusCheckRollup, reviews, reviewRequests, commits.
func (f *GHCLIReviewQueueFetcher) Fetch(ctx context.Context) error {
	args := []string{
		"search", "prs",
		"--review-requested", f.login,
		"--state", "open",
		"--limit", "100",
		"--json", "id,number,title,state,isDraft,url,createdAt,updatedAt,author,repository",
	}

	out, err := f.runner.Run(ctx, args)
	if err != nil {
		return fmt.Errorf("fetching review queue via gh: %w", err)
	}

	var prs []ghSearchPR
	if err := json.Unmarshal(out, &prs); err != nil {
		return fmt.Errorf("parsing gh search prs output: %w", err)
	}

	slog.Debug("ghcli: fetched review queue prs", "count", len(prs))

	seen := make(map[prKey]bool)
	for _, p := range prs {
		repo := p.Repository.NameWithOwner
		if repo == "" {
			continue
		}

		detail, err := fetchPRDetail(ctx, f.runner, repo, p.Number, "statusCheckRollup,reviews,reviewRequests,commits,body")
		if err != nil {
			slog.Error("ghcli: pr detail fetch failed, persisting with empty detail", "repo", repo, "number", p.Number, "error", err)
		}

		runs := convertStatusChecks(detail.StatusCheckRollup)
		reviews := convertReviews(detail.Reviews)
		commits := convertCommits(detail.Commits)
		inMergeQueue := fetchMergeQueueStatus(ctx, f.runner, p.ID)

		prRow := persistence.PullRequest{
			ID:             p.ID,
			Repo:           repo,
			Number:         p.Number,
			Title:          p.Title,
			Body:           detail.Body,
			Status:         api.DerivePRStatus(inMergeQueue, p.IsDraft, reviews),
			CIState:        api.DeriveCIState(runs),
			Draft:          p.IsDraft,
			Author:         p.Author.Login,
			CreatedAt:      p.CreatedAt,
			UpdatedAt:      p.UpdatedAt,
			LastActivityAt: p.UpdatedAt,
			URL:            p.URL,
			GlobalID:       p.ID,
		}

		if err := api.PersistRQPR(f.store, f.bus, prRow, runs, reviews, nil, commits); err != nil {
			return err
		}
		seen[prKey{Repo: repo, Number: p.Number}] = true
	}

	f.cleanupStalePRs(seen)
	return nil
}

// cleanupStalePRs deletes review-queue PRs from the DB that were not seen in the latest fetch.
func (f *GHCLIReviewQueueFetcher) cleanupStalePRs(seen map[prKey]bool) {
	others, err := f.store.ListPullRequestsNotByAuthor(f.login)
	if err != nil {
		return
	}
	for _, pr := range others {
		if !seen[prKey{Repo: pr.Repo, Number: pr.Number}] {
			deleted, err := f.store.DeletePullRequest(pr.Repo, pr.Number)
			if err != nil {
				continue
			}
			f.bus.Publish(eventbus.Event{
				Type:   eventbus.PRRemoved,
				Before: deleted,
				After:  nil,
			})
		}
	}
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
