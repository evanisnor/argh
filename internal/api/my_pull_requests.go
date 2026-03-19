package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
	"github.com/shurcooL/githubv4"
)

// GraphQLClient executes GitHub GraphQL queries.
type GraphQLClient interface {
	Query(ctx context.Context, q interface{}, variables map[string]interface{}) error
}

// PRStore is the persistence interface required by MyPullRequestsFetcher.
type PRStore interface {
	GetPullRequest(repo string, number int) (persistence.PullRequest, error)
	UpsertPullRequest(pr persistence.PullRequest) error
	UpsertReviewer(r persistence.Reviewer) error
	UpsertCheckRun(cr persistence.CheckRun) error
}

// Publisher publishes events to the event bus.
type Publisher interface {
	Publish(e eventbus.Event)
}

// MyPullRequestsFetcher fetches open PRs authored by the authenticated user
// and writes them into the persistence layer, emitting events for changes.
type MyPullRequestsFetcher struct {
	client GraphQLClient
	store  PRStore
	bus    Publisher
	login  string
}

// NewMyPullRequestsFetcher returns a new MyPullRequestsFetcher.
func NewMyPullRequestsFetcher(client GraphQLClient, store PRStore, bus Publisher, login string) *MyPullRequestsFetcher {
	return &MyPullRequestsFetcher{
		client: client,
		store:  store,
		bus:    bus,
		login:  login,
	}
}

// ── GraphQL query structs ─────────────────────────────────────────────────────

type prSearchCheckRun struct {
	Name       githubv4.String
	Status     githubv4.String
	Conclusion githubv4.String
	URL        githubv4.URI
}

type prSearchCheckRunConnection struct {
	Nodes []prSearchCheckRun
}

type prSearchCheckSuite struct {
	CheckRuns prSearchCheckRunConnection `graphql:"checkRuns(first: 50)"`
}

type prSearchCheckSuiteConnection struct {
	Nodes []prSearchCheckSuite
}

type prSearchReviewAuthor struct {
	Login githubv4.String
}

type prSearchReview struct {
	Author prSearchReviewAuthor
	State  githubv4.String
}

type prSearchReviewConnection struct {
	Nodes []prSearchReview
}

type prSearchReviewRequestedReviewerUser struct {
	Login githubv4.String
}

type prSearchReviewRequestedReviewer struct {
	User prSearchReviewRequestedReviewerUser `graphql:"... on User"`
}

type prSearchReviewRequest struct {
	RequestedReviewer prSearchReviewRequestedReviewer
}

type prSearchReviewRequestConnection struct {
	Nodes []prSearchReviewRequest
}

type prSearchPRAuthor struct {
	Login githubv4.String
}

type prSearchPRRepository struct {
	NameWithOwner githubv4.String
}

type prSearchMergeQueueEntry struct {
	ID githubv4.String
}

type prSearchPR struct {
	ID              githubv4.String
	Number          githubv4.Int
	Title           githubv4.String
	State           githubv4.String
	IsDraft         githubv4.Boolean
	URL             githubv4.URI
	CreatedAt       githubv4.DateTime
	UpdatedAt       githubv4.DateTime
	Author          prSearchPRAuthor
	Repository      prSearchPRRepository
	ReviewRequests  prSearchReviewRequestConnection `graphql:"reviewRequests(first: 10)"`
	Reviews         prSearchReviewConnection        `graphql:"reviews(first: 20)"`
	CheckSuites     prSearchCheckSuiteConnection    `graphql:"checkSuites(first: 10)"`
	MergeQueueEntry *prSearchMergeQueueEntry
}

type prSearchNode struct {
	PullRequest prSearchPR `graphql:"... on PullRequest"`
}

type prSearchPageInfo struct {
	HasNextPage githubv4.Boolean
	EndCursor   githubv4.String
}

type myPRsQuery struct {
	Search struct {
		Nodes    []prSearchNode
		PageInfo prSearchPageInfo
	} `graphql:"search(query: $query, type: ISSUE, first: 100, after: $cursor)"`
}

// ── Intermediate types ────────────────────────────────────────────────────────

// checkRunData is an intermediate representation of a GitHub check run.
type checkRunData struct {
	Name       string
	Status     string
	Conclusion string
	URL        string
}

// reviewData is an intermediate representation of a GitHub review.
type reviewData struct {
	Login string
	State string
}

// ── Fetch ─────────────────────────────────────────────────────────────────────

// Fetch queries GitHub for all open PRs authored by the user and persists changes.
// Events are emitted for new or changed PRs.
func (f *MyPullRequestsFetcher) Fetch(ctx context.Context) error {
	cursor := (*githubv4.String)(nil)
	searchQuery := fmt.Sprintf("is:pr is:open author:%s", f.login)

	for {
		var q myPRsQuery
		vars := map[string]interface{}{
			"query":  githubv4.String(searchQuery),
			"cursor": cursor,
		}

		if err := f.client.Query(ctx, &q, vars); err != nil {
			return fmt.Errorf("fetching my pull requests: %w", err)
		}

		for _, node := range q.Search.Nodes {
			p := node.PullRequest
			repo := string(p.Repository.NameWithOwner)
			if repo == "" {
				continue
			}

			runs := extractCheckRuns(p.CheckSuites)
			reviews := extractReviews(p.Reviews)

			prID := string(p.ID)
			prRow := persistence.PullRequest{
				ID:             prID,
				Repo:           repo,
				Number:         int(p.Number),
				Title:          string(p.Title),
				Status:         derivePRStatus(p.MergeQueueEntry != nil, bool(p.IsDraft), reviews),
				CIState:        deriveCIState(runs),
				Draft:          bool(p.IsDraft),
				Author:         string(p.Author.Login),
				CreatedAt:      p.CreatedAt.Time,
				UpdatedAt:      p.UpdatedAt.Time,
				LastActivityAt: p.UpdatedAt.Time,
				URL:            uriString(p.URL),
				GlobalID:       prID,
			}

			if err := f.persistPR(prRow, runs, reviews); err != nil {
				return err
			}
		}

		if !bool(q.Search.PageInfo.HasNextPage) {
			break
		}
		endCursor := q.Search.PageInfo.EndCursor
		cursor = &endCursor
	}

	return nil
}

func extractCheckRuns(suites prSearchCheckSuiteConnection) []checkRunData {
	var runs []checkRunData
	for _, suite := range suites.Nodes {
		for _, run := range suite.CheckRuns.Nodes {
			runs = append(runs, checkRunData{
				Name:       string(run.Name),
				Status:     string(run.Status),
				Conclusion: string(run.Conclusion),
				URL:        uriString(run.URL),
			})
		}
	}
	return runs
}

func extractReviews(conn prSearchReviewConnection) []reviewData {
	var reviews []reviewData
	for _, rev := range conn.Nodes {
		reviews = append(reviews, reviewData{
			Login: string(rev.Author.Login),
			State: string(rev.State),
		})
	}
	return reviews
}

// persistPR writes a PR and its associated data to the DB and emits events on changes.
func (f *MyPullRequestsFetcher) persistPR(pr persistence.PullRequest, runs []checkRunData, reviews []reviewData) error {
	existing, err := f.store.GetPullRequest(pr.Repo, pr.Number)
	isNew := errors.Is(err, sql.ErrNoRows)
	if err != nil && !isNew {
		return fmt.Errorf("reading existing PR %s#%d: %w", pr.Repo, pr.Number, err)
	}

	ciChanged := !isNew && existing.CIState != pr.CIState

	if err := f.store.UpsertPullRequest(pr); err != nil {
		return fmt.Errorf("upserting PR %s#%d: %w", pr.Repo, pr.Number, err)
	}

	for _, run := range runs {
		cr := persistence.CheckRun{
			PRID:       pr.ID,
			Name:       run.Name,
			State:      run.Status,
			Conclusion: run.Conclusion,
			URL:        run.URL,
		}
		if err := f.store.UpsertCheckRun(cr); err != nil {
			return fmt.Errorf("upserting check run %s: %w", run.Name, err)
		}
	}

	for _, rev := range reviews {
		r := persistence.Reviewer{
			PRID:  pr.ID,
			Login: rev.Login,
			State: rev.State,
		}
		if err := f.store.UpsertReviewer(r); err != nil {
			return fmt.Errorf("upserting reviewer %s: %w", rev.Login, err)
		}
	}

	if isNew {
		f.bus.Publish(eventbus.Event{
			Type:   eventbus.PRUpdated,
			Before: nil,
			After:  pr,
		})
	} else if ciChanged {
		f.bus.Publish(eventbus.Event{
			Type:   eventbus.CIChanged,
			Before: existing,
			After:  pr,
		})
	} else if !prsEqual(existing, pr) {
		f.bus.Publish(eventbus.Event{
			Type:   eventbus.PRUpdated,
			Before: existing,
			After:  pr,
		})
	}

	return nil
}

// uriString safely extracts the string representation of a githubv4.URI,
// returning an empty string when the underlying URL pointer is nil.
func uriString(u githubv4.URI) string {
	if u.URL == nil {
		return ""
	}
	return u.URL.String()
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// deriveCIState computes the overall CI state from a slice of check run data.
// Returns one of: "none", "running", "failing", "passing".
func deriveCIState(runs []checkRunData) string {
	if len(runs) == 0 {
		return "none"
	}
	for _, run := range runs {
		if run.Status != "COMPLETED" {
			return "running"
		}
	}
	for _, run := range runs {
		switch run.Conclusion {
		case "FAILURE", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE":
			return "failing"
		}
	}
	return "passing"
}

// derivePRStatus computes the PR status label from available signals.
// Returns one of: "merge queued", "draft", "open", "approved", "changes requested".
func derivePRStatus(inMergeQueue, isDraft bool, reviews []reviewData) string {
	if inMergeQueue {
		return "merge queued"
	}
	if isDraft {
		return "draft"
	}
	hasChangesRequested := false
	hasApproved := false
	for _, r := range reviews {
		switch r.State {
		case "CHANGES_REQUESTED":
			hasChangesRequested = true
		case "APPROVED":
			hasApproved = true
		}
	}
	if hasChangesRequested {
		return "changes requested"
	}
	if hasApproved {
		return "approved"
	}
	return "open"
}

// prsEqual returns true when two PullRequest rows carry identical data.
func prsEqual(a, b persistence.PullRequest) bool {
	return a.ID == b.ID &&
		a.Title == b.Title &&
		a.Status == b.Status &&
		a.CIState == b.CIState &&
		a.Draft == b.Draft &&
		a.Author == b.Author &&
		a.URL == b.URL &&
		a.GlobalID == b.GlobalID &&
		a.CreatedAt.Equal(b.CreatedAt) &&
		a.UpdatedAt.Equal(b.UpdatedAt) &&
		a.LastActivityAt.Equal(b.LastActivityAt)
}
