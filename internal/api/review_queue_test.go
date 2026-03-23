package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
	"github.com/shurcooL/githubv4"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// singlePageRQClient returns a StubGraphQLClient that delivers exactly one page of rqNodes.
func singlePageRQClient(nodes []rqNode) *StubGraphQLClient {
	return &StubGraphQLClient{
		QueryFunc: func(_ context.Context, q interface{}, _ map[string]interface{}) error {
			rq := q.(*reviewQueueQuery)
			rq.Search.Nodes = nodes
			rq.Search.PageInfo = prSearchPageInfo{HasNextPage: false}
			return nil
		},
	}
}

// baseRQNode builds a minimal rqNode with all required fields set.
func baseRQNode() rqNode {
	return rqNode{
		PullRequest: rqPR{
			ID:     githubv4.String("PR_rq1"),
			Number: githubv4.Int(10),
			Title:  githubv4.String("feat: review me"),
			State:  githubv4.String("OPEN"),
			Author: prSearchPRAuthor{Login: githubv4.String("bob")},
			Repository: prSearchPRRepository{
				NameWithOwner: githubv4.String("owner/repo"),
			},
			CreatedAt: githubv4.DateTime{Time: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)},
			UpdatedAt: githubv4.DateTime{Time: time.Date(2024, 2, 2, 0, 0, 0, 0, time.UTC)},
		},
	}
}

// ── Fetch tests ───────────────────────────────────────────────────────────────

func TestReviewQueueFetcher_Fetch_NewPR_EmitsPRUpdated(t *testing.T) {
	store := NewStubReviewQueueStore()
	pub := &StubPublisher{}
	client := singlePageRQClient([]rqNode{baseRQNode()})

	f := NewReviewQueueFetcher(client, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(store.UpsertedPRs) != 1 {
		t.Fatalf("expected 1 upserted PR, got %d", len(store.UpsertedPRs))
	}
	pr := store.UpsertedPRs[0]
	if pr.Repo != "owner/repo" {
		t.Errorf("Repo = %q, want %q", pr.Repo, "owner/repo")
	}
	if pr.Number != 10 {
		t.Errorf("Number = %d, want %d", pr.Number, 10)
	}
	if pr.Title != "feat: review me" {
		t.Errorf("Title = %q, want %q", pr.Title, "feat: review me")
	}
	if pr.Author != "bob" {
		t.Errorf("Author = %q, want %q", pr.Author, "bob")
	}
	if pr.Status != "open" {
		t.Errorf("Status = %q, want %q", pr.Status, "open")
	}
	if pr.CIState != "none" {
		t.Errorf("CIState = %q, want %q", pr.CIState, "none")
	}
	if pr.ID != "PR_rq1" {
		t.Errorf("ID = %q, want %q", pr.ID, "PR_rq1")
	}

	if len(pub.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(pub.Events))
	}
	if pub.Events[0].Type != eventbus.PRUpdated {
		t.Errorf("event type = %v, want %v", pub.Events[0].Type, eventbus.PRUpdated)
	}
	if pub.Events[0].Before != nil {
		t.Error("Before should be nil for new PR")
	}
}

func TestReviewQueueFetcher_Fetch_ExistingPR_CIChanged_EmitsCIChanged(t *testing.T) {
	existing := persistence.PullRequest{
		ID: "PR_rq1", Repo: "owner/repo", Number: 10,
		Title: "feat: review me", Status: "open", CIState: "running",
		Author:         "bob",
		CreatedAt:      time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2024, 2, 2, 0, 0, 0, 0, time.UTC),
		LastActivityAt: time.Date(2024, 2, 2, 0, 0, 0, 0, time.UTC),
		GlobalID:       "PR_rq1",
	}
	store := NewStubReviewQueueStore()
	store.GetPullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return existing, nil
	}
	pub := &StubPublisher{}

	node := baseRQNode()
	node.PullRequest.CheckSuites = prSearchCheckSuiteConnection{
		Nodes: []prSearchCheckSuite{{
			CheckRuns: prSearchCheckRunConnection{
				Nodes: []prSearchCheckRun{{
					Name:       githubv4.String("ci"),
					Status:     githubv4.String("COMPLETED"),
					Conclusion: githubv4.String("SUCCESS"),
				}},
			},
		}},
	}
	client := singlePageRQClient([]rqNode{node})

	f := NewReviewQueueFetcher(client, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(pub.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(pub.Events))
	}
	if pub.Events[0].Type != eventbus.CIChanged {
		t.Errorf("event type = %v, want %v", pub.Events[0].Type, eventbus.CIChanged)
	}
	before := pub.Events[0].Before.(persistence.PullRequest)
	if before.CIState != "running" {
		t.Errorf("Before.CIState = %q, want %q", before.CIState, "running")
	}
	after := pub.Events[0].After.(persistence.PullRequest)
	if after.CIState != "passing" {
		t.Errorf("After.CIState = %q, want %q", after.CIState, "passing")
	}
}

func TestReviewQueueFetcher_Fetch_ExistingPR_NoChanges_NoEvents(t *testing.T) {
	ts := time.Date(2024, 2, 2, 0, 0, 0, 0, time.UTC)
	existing := persistence.PullRequest{
		ID: "PR_rq1", Repo: "owner/repo", Number: 10,
		Title: "feat: review me", Status: "open", CIState: "none",
		Author:         "bob",
		CreatedAt:      time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:      ts,
		LastActivityAt: ts,
		GlobalID:       "PR_rq1",
	}
	store := NewStubReviewQueueStore()
	store.GetPullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return existing, nil
	}
	pub := &StubPublisher{}
	client := singlePageRQClient([]rqNode{baseRQNode()})

	f := NewReviewQueueFetcher(client, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(pub.Events) != 0 {
		t.Fatalf("expected no events, got %d", len(pub.Events))
	}
}

func TestReviewQueueFetcher_Fetch_APIError_ReturnsError(t *testing.T) {
	store := NewStubReviewQueueStore()
	pub := &StubPublisher{}
	apiErr := errors.New("rate limited")
	client := &StubGraphQLClient{
		QueryFunc: func(_ context.Context, _ interface{}, _ map[string]interface{}) error {
			return apiErr
		},
	}

	f := NewReviewQueueFetcher(client, store, pub, "alice")
	err := f.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, apiErr) {
		t.Errorf("error = %v, want to wrap %v", err, apiErr)
	}
	if len(store.UpsertedPRs) != 0 {
		t.Errorf("expected no DB writes, got %d", len(store.UpsertedPRs))
	}
}

func TestReviewQueueFetcher_Fetch_SkipsEmptyRepo(t *testing.T) {
	store := NewStubReviewQueueStore()
	pub := &StubPublisher{}

	emptyRepoNode := rqNode{
		PullRequest: rqPR{
			ID:     githubv4.String("PR_empty"),
			Number: githubv4.Int(99),
			// Repository.NameWithOwner left as zero value ("").
		},
	}
	client := singlePageRQClient([]rqNode{emptyRepoNode})

	f := NewReviewQueueFetcher(client, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(store.UpsertedPRs) != 0 {
		t.Errorf("expected no DB writes for empty repo, got %d", len(store.UpsertedPRs))
	}
}

func TestReviewQueueFetcher_Fetch_Pagination(t *testing.T) {
	store := NewStubReviewQueueStore()
	pub := &StubPublisher{}

	page := 0
	page1Node := baseRQNode()
	page1Node.PullRequest.ID = githubv4.String("PR_rq_p1")
	page1Node.PullRequest.Number = githubv4.Int(1)

	page2Node := baseRQNode()
	page2Node.PullRequest.ID = githubv4.String("PR_rq_p2")
	page2Node.PullRequest.Number = githubv4.Int(2)

	endCursor := githubv4.String("cursor1")
	client := &StubGraphQLClient{
		QueryFunc: func(_ context.Context, q interface{}, _ map[string]interface{}) error {
			rq := q.(*reviewQueueQuery)
			page++
			if page == 1 {
				rq.Search.Nodes = []rqNode{page1Node}
				rq.Search.PageInfo = prSearchPageInfo{
					HasNextPage: githubv4.Boolean(true),
					EndCursor:   endCursor,
				}
			} else {
				rq.Search.Nodes = []rqNode{page2Node}
				rq.Search.PageInfo = prSearchPageInfo{HasNextPage: false}
			}
			return nil
		},
	}

	f := NewReviewQueueFetcher(client, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if page != 2 {
		t.Errorf("expected 2 pages fetched, got %d", page)
	}
	if len(store.UpsertedPRs) != 2 {
		t.Fatalf("expected 2 upserted PRs, got %d", len(store.UpsertedPRs))
	}
}

// ── Author wait time from timeline events ─────────────────────────────────────

func TestReviewQueueFetcher_Fetch_CommitsStoredAsTimelineEvents(t *testing.T) {
	store := NewStubReviewQueueStore()
	pub := &StubPublisher{}

	commitTime := time.Date(2024, 2, 1, 12, 0, 0, 0, time.UTC)
	node := baseRQNode()
	node.PullRequest.Commits = rqCommitConnection{
		Nodes: []rqCommitNode{{
			Commit: rqCommit{
				Author:        rqCommitAuthor{User: rqCommitAuthorUser{Login: githubv4.String("bob")}},
				CommittedDate: githubv4.DateTime{Time: commitTime},
			},
		}},
	}
	client := singlePageRQClient([]rqNode{node})

	f := NewReviewQueueFetcher(client, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(store.InsertedTimelineEvents) != 1 {
		t.Fatalf("expected 1 timeline event, got %d", len(store.InsertedTimelineEvents))
	}
	te := store.InsertedTimelineEvents[0]
	if te.EventType != "commit" {
		t.Errorf("EventType = %q, want %q", te.EventType, "commit")
	}
	if te.Actor != "bob" {
		t.Errorf("Actor = %q, want %q", te.Actor, "bob")
	}
	if !te.CreatedAt.Equal(commitTime) {
		t.Errorf("CreatedAt = %v, want %v", te.CreatedAt, commitTime)
	}
	if te.PRID != "PR_rq1" {
		t.Errorf("PRID = %q, want %q", te.PRID, "PR_rq1")
	}
}

func TestReviewQueueFetcher_Fetch_MultipleCommitsStoredAsTimelineEvents(t *testing.T) {
	store := NewStubReviewQueueStore()
	pub := &StubPublisher{}

	time1 := time.Date(2024, 2, 1, 10, 0, 0, 0, time.UTC)
	time2 := time.Date(2024, 2, 2, 9, 0, 0, 0, time.UTC)
	node := baseRQNode()
	node.PullRequest.Commits = rqCommitConnection{
		Nodes: []rqCommitNode{
			{Commit: rqCommit{
				Author:        rqCommitAuthor{User: rqCommitAuthorUser{Login: githubv4.String("bob")}},
				CommittedDate: githubv4.DateTime{Time: time1},
			}},
			{Commit: rqCommit{
				Author:        rqCommitAuthor{User: rqCommitAuthorUser{Login: githubv4.String("bob")}},
				CommittedDate: githubv4.DateTime{Time: time2},
			}},
		},
	}
	client := singlePageRQClient([]rqNode{node})

	f := NewReviewQueueFetcher(client, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(store.InsertedTimelineEvents) != 2 {
		t.Fatalf("expected 2 timeline events, got %d", len(store.InsertedTimelineEvents))
	}
}

func TestReviewQueueFetcher_Fetch_NoCommits_NoTimelineEvents(t *testing.T) {
	store := NewStubReviewQueueStore()
	pub := &StubPublisher{}
	client := singlePageRQClient([]rqNode{baseRQNode()})

	f := NewReviewQueueFetcher(client, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(store.InsertedTimelineEvents) != 0 {
		t.Errorf("expected no timeline events, got %d", len(store.InsertedTimelineEvents))
	}
}

// ── Error path tests ──────────────────────────────────────────────────────────

func TestReviewQueueFetcher_Fetch_GetPRUnexpectedError_ReturnsError(t *testing.T) {
	dbErr := errors.New("db broken")
	store := NewStubReviewQueueStore()
	store.GetPullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return persistence.PullRequest{}, dbErr
	}
	pub := &StubPublisher{}
	client := singlePageRQClient([]rqNode{baseRQNode()})

	f := NewReviewQueueFetcher(client, store, pub, "alice")
	err := f.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("error = %v, want to wrap %v", err, dbErr)
	}
}

func TestReviewQueueFetcher_Fetch_UpsertPRError_ReturnsError(t *testing.T) {
	upsertErr := errors.New("upsert failed")
	store := NewStubReviewQueueStore()
	store.UpsertPullRequestFunc = func(pr persistence.PullRequest) error { return upsertErr }
	pub := &StubPublisher{}
	client := singlePageRQClient([]rqNode{baseRQNode()})

	f := NewReviewQueueFetcher(client, store, pub, "alice")
	err := f.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, upsertErr) {
		t.Errorf("error = %v, want to wrap %v", err, upsertErr)
	}
}

func TestReviewQueueFetcher_Fetch_UpsertCheckRunError_ReturnsError(t *testing.T) {
	crErr := errors.New("check run upsert failed")
	store := NewStubReviewQueueStore()
	store.UpsertCheckRunFunc = func(cr persistence.CheckRun) error { return crErr }
	pub := &StubPublisher{}

	node := baseRQNode()
	node.PullRequest.CheckSuites = prSearchCheckSuiteConnection{
		Nodes: []prSearchCheckSuite{{
			CheckRuns: prSearchCheckRunConnection{
				Nodes: []prSearchCheckRun{{
					Name:   githubv4.String("ci"),
					Status: githubv4.String("IN_PROGRESS"),
				}},
			},
		}},
	}
	client := singlePageRQClient([]rqNode{node})

	f := NewReviewQueueFetcher(client, store, pub, "alice")
	err := f.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, crErr) {
		t.Errorf("error = %v, want to wrap %v", err, crErr)
	}
}

func TestReviewQueueFetcher_Fetch_UpsertReviewerError_ReturnsError(t *testing.T) {
	revErr := errors.New("reviewer upsert failed")
	store := NewStubReviewQueueStore()
	store.UpsertReviewerFunc = func(r persistence.Reviewer) error { return revErr }
	pub := &StubPublisher{}

	node := baseRQNode()
	node.PullRequest.Reviews = prSearchReviewConnection{
		Nodes: []prSearchReview{{
			Author: prSearchReviewAuthor{Login: githubv4.String("alice")},
			State:  githubv4.String("APPROVED"),
		}},
	}
	client := singlePageRQClient([]rqNode{node})

	f := NewReviewQueueFetcher(client, store, pub, "alice")
	err := f.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, revErr) {
		t.Errorf("error = %v, want to wrap %v", err, revErr)
	}
}

func TestReviewQueueFetcher_Fetch_InsertTimelineEventError_ReturnsError(t *testing.T) {
	teErr := errors.New("timeline event insert failed")
	store := NewStubReviewQueueStore()
	store.InsertTimelineEventFunc = func(te persistence.TimelineEvent) error { return teErr }
	pub := &StubPublisher{}

	node := baseRQNode()
	node.PullRequest.Commits = rqCommitConnection{
		Nodes: []rqCommitNode{{
			Commit: rqCommit{
				Author:        rqCommitAuthor{User: rqCommitAuthorUser{Login: githubv4.String("bob")}},
				CommittedDate: githubv4.DateTime{Time: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)},
			},
		}},
	}
	client := singlePageRQClient([]rqNode{node})

	f := NewReviewQueueFetcher(client, store, pub, "alice")
	err := f.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, teErr) {
		t.Errorf("error = %v, want to wrap %v", err, teErr)
	}
}

func TestReviewQueueFetcher_Fetch_ExistingPR_OtherChange_EmitsPRUpdated(t *testing.T) {
	existing := persistence.PullRequest{
		ID: "PR_rq1", Repo: "owner/repo", Number: 10,
		Title: "old title", Status: "open", CIState: "none",
		Author:         "bob",
		CreatedAt:      time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2024, 2, 2, 0, 0, 0, 0, time.UTC),
		LastActivityAt: time.Date(2024, 2, 2, 0, 0, 0, 0, time.UTC),
		GlobalID:       "PR_rq1",
	}
	store := NewStubReviewQueueStore()
	store.GetPullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return existing, nil
	}
	pub := &StubPublisher{}

	node := baseRQNode()
	node.PullRequest.Title = githubv4.String("new title")
	client := singlePageRQClient([]rqNode{node})

	f := NewReviewQueueFetcher(client, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(pub.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(pub.Events))
	}
	if pub.Events[0].Type != eventbus.PRUpdated {
		t.Errorf("event type = %v, want %v", pub.Events[0].Type, eventbus.PRUpdated)
	}
}

// ── No-duplication test ───────────────────────────────────────────────────────

// TestReviewQueueFetcher_Fetch_NoDuplication verifies that a PR already in the
// DB (e.g. from the My Pull Requests query) is updated in-place, not duplicated.
// The primary key is (repo, number); UpsertPullRequest handles deduplication.
func TestReviewQueueFetcher_Fetch_NoDuplication(t *testing.T) {
	// Simulate a PR already in the DB with identical data.
	ts := time.Date(2024, 2, 2, 0, 0, 0, 0, time.UTC)
	existing := persistence.PullRequest{
		ID: "PR_rq1", Repo: "owner/repo", Number: 10,
		Title: "feat: review me", Status: "open", CIState: "none",
		Author:         "bob",
		CreatedAt:      time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:      ts,
		LastActivityAt: ts,
		GlobalID:       "PR_rq1",
	}
	store := NewStubReviewQueueStore()
	store.GetPullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return existing, nil
	}
	pub := &StubPublisher{}
	client := singlePageRQClient([]rqNode{baseRQNode()})

	f := NewReviewQueueFetcher(client, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	// UpsertPullRequest is called once (for the update), no duplicate inserts.
	if len(store.UpsertedPRs) != 1 {
		t.Errorf("expected 1 upserted PR (update in-place), got %d", len(store.UpsertedPRs))
	}
	// No event emitted because nothing changed.
	if len(pub.Events) != 0 {
		t.Errorf("expected no events for unchanged PR, got %d", len(pub.Events))
	}
}

// ── extractRQCommits ──────────────────────────────────────────────────────────

func TestExtractRQCommits_EmptyConnection(t *testing.T) {
	result := extractRQCommits(rqCommitConnection{})
	if len(result) != 0 {
		t.Errorf("expected 0 commits, got %d", len(result))
	}
}

func TestExtractRQCommits_ExtractsLoginAndDate(t *testing.T) {
	commitTime := time.Date(2024, 3, 10, 8, 0, 0, 0, time.UTC)
	conn := rqCommitConnection{
		Nodes: []rqCommitNode{{
			Commit: rqCommit{
				Author:        rqCommitAuthor{User: rqCommitAuthorUser{Login: githubv4.String("carol")}},
				CommittedDate: githubv4.DateTime{Time: commitTime},
			},
		}},
	}
	result := extractRQCommits(conn)
	if len(result) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(result))
	}
	if result[0].AuthorLogin != "carol" {
		t.Errorf("AuthorLogin = %q, want %q", result[0].AuthorLogin, "carol")
	}
	if !result[0].CommittedDate.Time.Equal(commitTime) {
		t.Errorf("CommittedDate = %v, want %v", result[0].CommittedDate.Time, commitTime)
	}
}

// ── Stale PR cleanup ─────────────────────────────────────────────────────────

func TestReviewQueueFetcher_Fetch_CleansUpStalePRs(t *testing.T) {
	store := NewStubReviewQueueStore()
	pub := &StubPublisher{}

	// API returns PR #10 only.
	client := singlePageRQClient([]rqNode{baseRQNode()})

	// DB has PR #10 (by bob) and stale PR #99 (by carol).
	stalePR := persistence.PullRequest{
		ID: "PR_stale", Repo: "owner/repo", Number: 99, Title: "stale",
		Author: "carol", Status: "open", CIState: "none",
	}
	store.ListPullRequestsNotByAuthorFunc = func(author string) ([]persistence.PullRequest, error) {
		return []persistence.PullRequest{
			{ID: "PR_rq1", Repo: "owner/repo", Number: 10, Author: "bob"},
			stalePR,
		}, nil
	}
	store.DeletePullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return stalePR, nil
	}

	f := NewReviewQueueFetcher(client, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(store.DeletedPRKeys) != 1 {
		t.Fatalf("expected 1 deletion, got %d", len(store.DeletedPRKeys))
	}
	if store.DeletedPRKeys[0].Number != 99 {
		t.Errorf("deleted PR number = %d, want 99", store.DeletedPRKeys[0].Number)
	}

	var foundRemoved bool
	for _, e := range pub.Events {
		if e.Type == eventbus.PRRemoved {
			foundRemoved = true
		}
	}
	if !foundRemoved {
		t.Error("expected PRRemoved event")
	}
}

func TestReviewQueueFetcher_Fetch_NoCleanupOnAPIError(t *testing.T) {
	store := NewStubReviewQueueStore()
	pub := &StubPublisher{}

	client := &StubGraphQLClient{
		QueryFunc: func(_ context.Context, _ interface{}, _ map[string]interface{}) error {
			return errors.New("api error")
		},
	}

	store.ListPullRequestsNotByAuthorFunc = func(author string) ([]persistence.PullRequest, error) {
		return []persistence.PullRequest{{ID: "PR_stale", Repo: "owner/repo", Number: 99}}, nil
	}

	f := NewReviewQueueFetcher(client, store, pub, "alice")
	_ = f.Fetch(context.Background())

	if len(store.DeletedPRKeys) != 0 {
		t.Errorf("expected 0 deletions on API error, got %d", len(store.DeletedPRKeys))
	}
}

func TestReviewQueueFetcher_Fetch_CleanupListError(t *testing.T) {
	store := NewStubReviewQueueStore()
	pub := &StubPublisher{}

	client := singlePageRQClient([]rqNode{baseRQNode()})
	store.ListPullRequestsNotByAuthorFunc = func(author string) ([]persistence.PullRequest, error) {
		return nil, errors.New("list error")
	}

	f := NewReviewQueueFetcher(client, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(store.DeletedPRKeys) != 0 {
		t.Errorf("expected 0 deletions on list error, got %d", len(store.DeletedPRKeys))
	}
}

func TestReviewQueueFetcher_Fetch_CleanupDeleteError(t *testing.T) {
	store := NewStubReviewQueueStore()
	pub := &StubPublisher{}

	client := singlePageRQClient([]rqNode{baseRQNode()})
	store.ListPullRequestsNotByAuthorFunc = func(author string) ([]persistence.PullRequest, error) {
		return []persistence.PullRequest{
			{ID: "PR_rq1", Repo: "owner/repo", Number: 10, Author: "bob"},
			{ID: "PR_stale", Repo: "owner/repo", Number: 99, Author: "carol"},
		}, nil
	}
	store.DeletePullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return persistence.PullRequest{}, errors.New("delete error")
	}

	f := NewReviewQueueFetcher(client, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	for _, e := range pub.Events {
		if e.Type == eventbus.PRRemoved {
			t.Error("expected no PRRemoved event on delete error")
		}
	}
}
