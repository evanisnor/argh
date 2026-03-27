package api

import (
	"context"
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
	UpsertReviewThread(rt persistence.ReviewThread) error
	ListPullRequestsByAuthor(author string) ([]persistence.PullRequest, error)
	DeletePullRequest(repo string, number int) (persistence.PullRequest, error)
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

type prSearchReviewThreadComment struct {
	Body githubv4.String
}

type prSearchReviewThreadCommentConnection struct {
	Nodes []prSearchReviewThreadComment
}

type prSearchReviewThread struct {
	ID         githubv4.String
	IsResolved githubv4.Boolean
	Path       githubv4.String
	Line       githubv4.Int
	Comments   prSearchReviewThreadCommentConnection `graphql:"comments(first: 1)"`
}

type prSearchReviewThreadConnection struct {
	Nodes []prSearchReviewThread
}

type prSearchPR struct {
	ID              githubv4.String
	Number          githubv4.Int
	Title           githubv4.String
	Body            githubv4.String
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
	ReviewThreads   prSearchReviewThreadConnection `graphql:"reviewThreads(first: 50)"`
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

// CheckRunData is an intermediate representation of a GitHub check run.
type CheckRunData struct {
	Name       string
	Status     string
	Conclusion string
	URL        string
}

// ReviewData is an intermediate representation of a GitHub review.
type ReviewData struct {
	Login string
	State string
}

// ReviewThreadData is an intermediate representation of a GitHub review thread.
type ReviewThreadData struct {
	ID       string
	Resolved bool
	Body     string
	Path     string
	Line     int
}

// prKey identifies a PR by repo and number for stale-PR tracking.
type prKey struct {
	Repo   string
	Number int
}

// ── Fetch ─────────────────────────────────────────────────────────────────────

// Fetch queries GitHub for all open PRs authored by the user and persists changes.
// Events are emitted for new or changed PRs. PRs that are no longer returned by
// the API (merged/closed) are deleted from the database and a PRRemoved event is emitted.
func (f *MyPullRequestsFetcher) Fetch(ctx context.Context) error {
	cursor := (*githubv4.String)(nil)
	searchQuery := fmt.Sprintf("is:pr is:open author:%s", f.login)
	seen := make(map[prKey]bool)

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
			threads := extractReviewThreads(p.ReviewThreads)

			prID := string(p.ID)
			prRow := persistence.PullRequest{
				ID:             prID,
				Repo:           repo,
				Number:         int(p.Number),
				Title:          string(p.Title),
				Body:           string(p.Body),
				Status:         DerivePRStatus(p.MergeQueueEntry != nil, bool(p.IsDraft), reviews),
				CIState:        DeriveCIState(runs),
				Draft:          bool(p.IsDraft),
				Author:         string(p.Author.Login),
				CreatedAt:      p.CreatedAt.Time,
				UpdatedAt:      p.UpdatedAt.Time,
				LastActivityAt: p.UpdatedAt.Time,
				URL:            uriString(p.URL),
				GlobalID:       prID,
			}

			if err := f.persistPR(prRow, runs, reviews, threads); err != nil {
				return err
			}
			seen[prKey{Repo: repo, Number: int(p.Number)}] = true
		}

		if !bool(q.Search.PageInfo.HasNextPage) {
			break
		}
		endCursor := q.Search.PageInfo.EndCursor
		cursor = &endCursor
	}

	f.cleanupStalePRs(seen)
	return nil
}

// cleanupStalePRs deletes PRs from the DB that were not seen in the latest fetch.
func (f *MyPullRequestsFetcher) cleanupStalePRs(seen map[prKey]bool) {
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

func extractCheckRuns(suites prSearchCheckSuiteConnection) []CheckRunData {
	var runs []CheckRunData
	for _, suite := range suites.Nodes {
		for _, run := range suite.CheckRuns.Nodes {
			runs = append(runs, CheckRunData{
				Name:       string(run.Name),
				Status:     string(run.Status),
				Conclusion: string(run.Conclusion),
				URL:        uriString(run.URL),
			})
		}
	}
	return runs
}

func extractReviews(conn prSearchReviewConnection) []ReviewData {
	var reviews []ReviewData
	for _, rev := range conn.Nodes {
		reviews = append(reviews, ReviewData{
			Login: string(rev.Author.Login),
			State: string(rev.State),
		})
	}
	return reviews
}

func extractReviewThreads(conn prSearchReviewThreadConnection) []ReviewThreadData {
	var threads []ReviewThreadData
	for _, t := range conn.Nodes {
		body := ""
		if len(t.Comments.Nodes) > 0 {
			body = string(t.Comments.Nodes[0].Body)
		}
		threads = append(threads, ReviewThreadData{
			ID:       string(t.ID),
			Resolved: bool(t.IsResolved),
			Body:     body,
			Path:     string(t.Path),
			Line:     int(t.Line),
		})
	}
	return threads
}

// persistPR delegates to the shared PersistPR function.
func (f *MyPullRequestsFetcher) persistPR(pr persistence.PullRequest, runs []CheckRunData, reviews []ReviewData, threads []ReviewThreadData) error {
	return PersistPR(f.store, f.bus, pr, runs, reviews, threads)
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

// DeriveCIState computes the overall CI state from a slice of check run data.
// Returns one of: "none", "running", "failing", "passing".
func DeriveCIState(runs []CheckRunData) string {
	if len(runs) == 0 {
		return "none"
	}
	for _, run := range runs {
		if run.Status == "COMPLETED" {
			switch run.Conclusion {
			case "FAILURE", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE":
				return "failing"
			}
		}
	}
	for _, run := range runs {
		if run.Status != "COMPLETED" {
			return "running"
		}
	}
	return "passing"
}

// DerivePRStatus computes the PR status label from available signals.
// Returns one of: "merge queued", "draft", "open", "approved", "changes requested".
func DerivePRStatus(inMergeQueue, isDraft bool, reviews []ReviewData) string {
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

// PRsEqual returns true when two PullRequest rows carry identical data.
func PRsEqual(a, b persistence.PullRequest) bool {
	return a.ID == b.ID &&
		a.Title == b.Title &&
		a.Body == b.Body &&
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
