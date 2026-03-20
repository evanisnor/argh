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

// ReviewQueueStore is the persistence interface required by ReviewQueueFetcher.
type ReviewQueueStore interface {
	GetPullRequest(repo string, number int) (persistence.PullRequest, error)
	UpsertPullRequest(pr persistence.PullRequest) error
	UpsertReviewer(r persistence.Reviewer) error
	UpsertCheckRun(cr persistence.CheckRun) error
	InsertTimelineEvent(te persistence.TimelineEvent) error
}

// ReviewQueueFetcher fetches open PRs where the authenticated user is a
// requested reviewer and writes them into the persistence layer, emitting
// events for changes.
type ReviewQueueFetcher struct {
	client GraphQLClient
	store  ReviewQueueStore
	bus    Publisher
	login  string
}

// NewReviewQueueFetcher returns a new ReviewQueueFetcher.
func NewReviewQueueFetcher(client GraphQLClient, store ReviewQueueStore, bus Publisher, login string) *ReviewQueueFetcher {
	return &ReviewQueueFetcher{
		client: client,
		store:  store,
		bus:    bus,
		login:  login,
	}
}

// ── GraphQL query structs ─────────────────────────────────────────────────────

type rqCommitAuthorUser struct {
	Login githubv4.String
}

type rqCommitAuthor struct {
	User rqCommitAuthorUser
}

type rqCommit struct {
	Author        rqCommitAuthor
	CommittedDate githubv4.DateTime
}

type rqCommitNode struct {
	Commit rqCommit
}

type rqCommitConnection struct {
	Nodes []rqCommitNode
}

// rqPR mirrors prSearchPR with the addition of a commits connection for
// author-wait-time calculation.
type rqPR struct {
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
	Commits         rqCommitConnection `graphql:"commits(last: 5)"`
}

type rqNode struct {
	PullRequest rqPR `graphql:"... on PullRequest"`
}

type reviewQueueQuery struct {
	Search struct {
		Nodes    []rqNode
		PageInfo prSearchPageInfo
	} `graphql:"search(query: $query, type: ISSUE, first: 100, after: $cursor)"`
}

// ── Intermediate types ────────────────────────────────────────────────────────

// CommitData is an intermediate representation of a GitHub commit author event.
type CommitData struct {
	AuthorLogin   string
	CommittedDate githubv4.DateTime
}

// ── Fetch ─────────────────────────────────────────────────────────────────────

// Fetch queries GitHub for all open PRs where the user is a requested reviewer
// and persists changes. Events are emitted for new or changed PRs.
func (f *ReviewQueueFetcher) Fetch(ctx context.Context) error {
	cursor := (*githubv4.String)(nil)
	searchQuery := fmt.Sprintf("is:pr is:open review-requested:%s", f.login)

	for {
		var q reviewQueueQuery
		vars := map[string]interface{}{
			"query":  githubv4.String(searchQuery),
			"cursor": cursor,
		}

		if err := f.client.Query(ctx, &q, vars); err != nil {
			return fmt.Errorf("fetching review queue: %w", err)
		}

		for _, node := range q.Search.Nodes {
			p := node.PullRequest
			repo := string(p.Repository.NameWithOwner)
			if repo == "" {
				continue
			}

			runs := extractCheckRuns(p.CheckSuites)
			reviews := extractReviews(p.Reviews)
			commits := extractRQCommits(p.Commits)

			prID := string(p.ID)
			prRow := persistence.PullRequest{
				ID:             prID,
				Repo:           repo,
				Number:         int(p.Number),
				Title:          string(p.Title),
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

			if err := f.persistRQPR(prRow, runs, reviews, commits); err != nil {
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

func extractRQCommits(conn rqCommitConnection) []CommitData {
	var commits []CommitData
	for _, node := range conn.Nodes {
		commits = append(commits, CommitData{
			AuthorLogin:   string(node.Commit.Author.User.Login),
			CommittedDate: node.Commit.CommittedDate,
		})
	}
	return commits
}

// persistRQPR writes a PR and its associated data to the DB and emits events on changes.
func (f *ReviewQueueFetcher) persistRQPR(pr persistence.PullRequest, runs []CheckRunData, reviews []ReviewData, commits []CommitData) error {
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

	for _, c := range commits {
		te := persistence.TimelineEvent{
			PRID:        pr.ID,
			EventType:   "commit",
			Actor:       c.AuthorLogin,
			CreatedAt:   c.CommittedDate.Time,
			PayloadJSON: "{}",
		}
		if err := f.store.InsertTimelineEvent(te); err != nil {
			return fmt.Errorf("inserting commit timeline event: %w", err)
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
	} else if !PRsEqual(existing, pr) {
		f.bus.Publish(eventbus.Event{
			Type:   eventbus.PRUpdated,
			Before: existing,
			After:  pr,
		})
	}

	return nil
}
