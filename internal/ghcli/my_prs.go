package ghcli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/evanisnor/argh/internal/api"
	"github.com/evanisnor/argh/internal/persistence"
)

// ghSearchPR models a single result from `gh search prs --json ...`.
type ghSearchPR struct {
	ID                string              `json:"id"`
	Number            int                 `json:"number"`
	Title             string              `json:"title"`
	State             string              `json:"state"`
	IsDraft           bool                `json:"isDraft"`
	URL               string              `json:"url"`
	CreatedAt         time.Time           `json:"createdAt"`
	UpdatedAt         time.Time           `json:"updatedAt"`
	Author            ghAuthor            `json:"author"`
	Repository        ghRepository        `json:"repository"`
	StatusCheckRollup []ghStatusCheck     `json:"statusCheckRollup"`
	Reviews           []ghReview          `json:"reviews"`
	ReviewRequests    []ghReviewRequest   `json:"reviewRequests"`
}

type ghAuthor struct {
	Login string `json:"login"`
}

type ghRepository struct {
	Name            string `json:"name"`
	NameWithOwner   string `json:"nameWithOwner"`
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

// Fetch queries for open PRs authored by the user and persists them.
func (f *GHCLIMyPRsFetcher) Fetch(ctx context.Context) error {
	args := []string{
		"search", "prs",
		"--author", f.login,
		"--state", "open",
		"--limit", "100",
		"--json", "id,number,title,state,isDraft,url,createdAt,updatedAt,author,repository,statusCheckRollup,reviews,reviewRequests",
	}

	out, err := f.runner.Run(ctx, args)
	if err != nil {
		return fmt.Errorf("fetching my pull requests via gh: %w", err)
	}

	var prs []ghSearchPR
	if err := json.Unmarshal(out, &prs); err != nil {
		return fmt.Errorf("parsing gh search prs output: %w", err)
	}

	for _, p := range prs {
		repo := p.Repository.NameWithOwner
		if repo == "" {
			continue
		}

		runs := convertStatusChecks(p.StatusCheckRollup)
		reviews := convertReviews(p.Reviews)

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

		if err := api.PersistPR(f.store, f.bus, prRow, runs, reviews); err != nil {
			return err
		}
	}

	return nil
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
