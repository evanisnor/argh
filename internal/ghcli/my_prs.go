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
)

// ghSearchPR models a single result from `gh search prs --json ...`.
// Only includes fields supported by the search endpoint.
type ghSearchPR struct {
	ID         string       `json:"id"`
	Number     int          `json:"number"`
	Title      string       `json:"title"`
	State      string       `json:"state"`
	IsDraft    bool         `json:"isDraft"`
	URL        string       `json:"url"`
	CreatedAt  time.Time    `json:"createdAt"`
	UpdatedAt  time.Time    `json:"updatedAt"`
	Author     ghAuthor     `json:"author"`
	Repository ghRepository `json:"repository"`
}

type ghAuthor struct {
	Login string `json:"login"`
}

type ghRepository struct {
	Name          string `json:"name"`
	NameWithOwner string `json:"nameWithOwner"`
}

// ghPRDetail models the detail fields fetched via `gh pr view --json ...`.
type ghPRDetail struct {
	Body              string            `json:"body"`
	StatusCheckRollup []ghStatusCheck   `json:"statusCheckRollup"`
	Reviews           []ghReview        `json:"reviews"`
	ReviewRequests    []ghReviewRequest `json:"reviewRequests"`
	Commits           []ghCommit        `json:"commits"`
}

type ghStatusCheck struct {
	Name       string `json:"name"`
	Context    string `json:"context"`
	State      string `json:"state"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	DetailURL  string `json:"detailsUrl"`
}

type ghReview struct {
	Author ghAuthor `json:"author"`
	State  string   `json:"state"`
}

type ghReviewRequest struct {
	Login string `json:"login"`
	Name  string `json:"name"`
}

// ghMergeQueueResponse models the GraphQL response for merge queue status.
type ghMergeQueueResponse struct {
	Data struct {
		Node struct {
			MergeQueueEntry *struct {
				ID string `json:"id"`
			} `json:"mergeQueueEntry"`
		} `json:"node"`
	} `json:"data"`
}

// fetchMergeQueueStatus queries whether a PR is in the merge queue via
// `gh api graphql`. Returns true if mergeQueueEntry is present, false otherwise.
// Errors are logged and treated as "not in merge queue" to avoid blocking the fetch.
func fetchMergeQueueStatus(ctx context.Context, runner CommandRunner, nodeID string) bool {
	query := `query($nodeID: ID!) { node(id: $nodeID) { ... on PullRequest { mergeQueueEntry { id } } } }`
	args := []string{
		"api", "graphql",
		"-f", "query=" + query,
		"-F", "nodeID=" + nodeID,
	}

	out, err := runner.Run(ctx, args)
	if err != nil {
		slog.Debug("ghcli: merge queue query failed", "nodeID", nodeID, "error", err)
		return false
	}

	var resp ghMergeQueueResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		slog.Debug("ghcli: merge queue response parse failed", "nodeID", nodeID, "error", err)
		return false
	}

	return resp.Data.Node.MergeQueueEntry != nil
}

// fetchPRDetail fetches detail fields for a single PR via `gh pr view`.
func fetchPRDetail(ctx context.Context, runner CommandRunner, repo string, number int, fields string) (ghPRDetail, error) {
	args := []string{
		"pr", "view", fmt.Sprintf("%d", number),
		"--repo", repo,
		"--json", fields,
	}

	out, err := runner.Run(ctx, args)
	if err != nil {
		return ghPRDetail{}, fmt.Errorf("fetching PR detail for %s#%d: %w", repo, number, err)
	}

	var detail ghPRDetail
	if err := json.Unmarshal(out, &detail); err != nil {
		return ghPRDetail{}, fmt.Errorf("parsing PR detail for %s#%d: %w", repo, number, err)
	}
	return detail, nil
}

// GHCLIMyPRsFetcher fetches the authenticated user's open PRs via `gh search prs`.
type GHCLIMyPRsFetcher struct {
	runner CommandRunner
	store  api.PRStore
	bus    api.Publisher
	login  string
}

// NewGHCLIMyPRsFetcher creates a new GHCLIMyPRsFetcher.
func NewGHCLIMyPRsFetcher(runner CommandRunner, store api.PRStore, bus api.Publisher, login string) *GHCLIMyPRsFetcher {
	return &GHCLIMyPRsFetcher{
		runner: runner,
		store:  store,
		bus:    bus,
		login:  login,
	}
}

// prKey identifies a PR by repo and number for stale-PR tracking.
type prKey struct {
	Repo   string
	Number int
}

// Fetch queries for open PRs authored by the user and persists them.
// Phase 1: gh search prs for the PR list (supported fields only).
// Phase 2: gh pr view per PR for statusCheckRollup, reviews, reviewRequests.
// After a successful fetch, PRs in the DB that were not returned by the API
// are deleted and PRRemoved events are emitted.
func (f *GHCLIMyPRsFetcher) Fetch(ctx context.Context) error {
	args := []string{
		"search", "prs",
		"--author", f.login,
		"--state", "open",
		"--limit", "100",
		"--json", "id,number,title,state,isDraft,url,createdAt,updatedAt,author,repository",
	}

	out, err := f.runner.Run(ctx, args)
	if err != nil {
		return fmt.Errorf("fetching my pull requests via gh: %w", err)
	}

	var prs []ghSearchPR
	if err := json.Unmarshal(out, &prs); err != nil {
		return fmt.Errorf("parsing gh search prs output: %w", err)
	}

	slog.Debug("ghcli: fetched my prs", "count", len(prs))

	seen := make(map[prKey]bool)
	for _, p := range prs {
		repo := p.Repository.NameWithOwner
		if repo == "" {
			continue
		}

		detail, err := fetchPRDetail(ctx, f.runner, repo, p.Number, "statusCheckRollup,reviews,reviewRequests,body")
		if err != nil {
			slog.Error("ghcli: pr detail fetch failed, persisting with empty detail", "repo", repo, "number", p.Number, "error", err)
		}

		runs := convertStatusChecks(detail.StatusCheckRollup)
		reviews := convertReviews(detail.Reviews)
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

		if err := api.PersistPR(f.store, f.bus, prRow, runs, reviews); err != nil {
			return err
		}
		seen[prKey{Repo: repo, Number: p.Number}] = true
	}

	f.cleanupStalePRs(seen)
	return nil
}

// cleanupStalePRs deletes PRs from the DB that were not seen in the latest fetch.
func (f *GHCLIMyPRsFetcher) cleanupStalePRs(seen map[prKey]bool) {
	owned, err := f.store.ListPullRequestsByAuthor(f.login)
	if err != nil {
		return
	}
	for _, pr := range owned {
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

// convertStatusChecks converts gh CLI status checks to the shared CheckRunData type.
func convertStatusChecks(checks []ghStatusCheck) []api.CheckRunData {
	var runs []api.CheckRunData
	for _, c := range checks {
		name := c.Name
		if name == "" {
			name = c.Context
		}
		runs = append(runs, api.CheckRunData{
			Name:       name,
			Status:     normalizeStatus(c.Status, c.State),
			Conclusion: normalizeConclusion(c.Conclusion, c.State),
			URL:        c.DetailURL,
		})
	}
	return runs
}

// convertReviews converts gh CLI reviews to the shared ReviewData type.
func convertReviews(reviews []ghReview) []api.ReviewData {
	var result []api.ReviewData
	for _, r := range reviews {
		result = append(result, api.ReviewData{
			Login: r.Author.Login,
			State: r.State,
		})
	}
	return result
}

// normalizeStatus maps gh CLI status/state values to GraphQL-style status values.
func normalizeStatus(status, state string) string {
	if status != "" {
		return status
	}
	switch state {
	case "SUCCESS", "FAILURE", "ERROR", "EXPECTED", "NEUTRAL":
		return "COMPLETED"
	case "PENDING":
		return "IN_PROGRESS"
	default:
		return "IN_PROGRESS"
	}
}

// normalizeConclusion maps gh CLI conclusion/state values to GraphQL-style conclusion values.
func normalizeConclusion(conclusion, state string) string {
	if conclusion != "" {
		return conclusion
	}
	switch state {
	case "SUCCESS":
		return "SUCCESS"
	case "FAILURE", "ERROR":
		return "FAILURE"
	case "NEUTRAL", "EXPECTED":
		return "NEUTRAL"
	default:
		return ""
	}
}
