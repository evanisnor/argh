package api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
	"github.com/shurcooL/githubv4"
)

// ── uriString ────────────────────────────────────────────────────────────────

func TestURIString_NilURL(t *testing.T) {
	if got := uriString(githubv4.URI{}); got != "" {
		t.Errorf("zero URI = %q, want empty string", got)
	}
}

func TestURIString_NonNilURL(t *testing.T) {
	var uri githubv4.URI
	if err := json.Unmarshal([]byte(`"https://github.com/owner/repo/pull/1"`), &uri); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	want := "https://github.com/owner/repo/pull/1"
	if got := uriString(uri); got != want {
		t.Errorf("uriString = %q, want %q", got, want)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// singlePageClient returns a StubGraphQLClient that delivers exactly one page of nodes.
func singlePageClient(nodes []prSearchNode) *StubGraphQLClient {
	return &StubGraphQLClient{
		QueryFunc: func(_ context.Context, q interface{}, _ map[string]interface{}) error {
			mq := q.(*myPRsQuery)
			mq.Search.Nodes = nodes
			mq.Search.PageInfo = prSearchPageInfo{HasNextPage: false}
			return nil
		},
	}
}

// basePRNode builds a minimal prSearchNode with all required fields set.
func basePRNode() prSearchNode {
	return prSearchNode{
		PullRequest: prSearchPR{
			ID:     githubv4.String("PR_abc"),
			Number: githubv4.Int(42),
			Title:  githubv4.String("fix: a bug"),
			Body:   githubv4.String("This PR fixes a bug"),
			State:  githubv4.String("OPEN"),
			Author: prSearchPRAuthor{Login: githubv4.String("alice")},
			Repository: prSearchPRRepository{
				NameWithOwner: githubv4.String("owner/repo"),
			},
			CreatedAt: githubv4.DateTime{Time: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
			UpdatedAt: githubv4.DateTime{Time: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)},
		},
	}
}

// ── Fetch integration tests ───────────────────────────────────────────────────

func TestMyPullRequestsFetcher_Fetch_NewPR_EmitsPRUpdated(t *testing.T) {
	store := NewStubPRStore()
	pub := &StubPublisher{}
	client := singlePageClient([]prSearchNode{basePRNode()})

	f := NewMyPullRequestsFetcher(client, store, pub, "alice")
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
	if pr.Number != 42 {
		t.Errorf("Number = %d, want %d", pr.Number, 42)
	}
	if pr.Title != "fix: a bug" {
		t.Errorf("Title = %q, want %q", pr.Title, "fix: a bug")
	}
	if pr.Body != "This PR fixes a bug" {
		t.Errorf("Body = %q, want %q", pr.Body, "This PR fixes a bug")
	}
	if pr.Author != "alice" {
		t.Errorf("Author = %q, want %q", pr.Author, "alice")
	}
	if pr.Status != "open" {
		t.Errorf("Status = %q, want %q", pr.Status, "open")
	}
	if pr.CIState != "none" {
		t.Errorf("CIState = %q, want %q", pr.CIState, "none")
	}
	if pr.Draft != false {
		t.Errorf("Draft = %v, want false", pr.Draft)
	}
	if pr.ID != "PR_abc" {
		t.Errorf("ID = %q, want %q", pr.ID, "PR_abc")
	}
	if pr.GlobalID != "PR_abc" {
		t.Errorf("GlobalID = %q, want %q", pr.GlobalID, "PR_abc")
	}

	if len(pub.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(pub.Events))
	}
	if pub.Events[0].Type != eventbus.PRUpdated {
		t.Errorf("event type = %v, want %v", pub.Events[0].Type, eventbus.PRUpdated)
	}
	if pub.Events[0].Before != nil {
		t.Errorf("Before should be nil for new PR")
	}
}

func TestMyPullRequestsFetcher_Fetch_ExistingPR_CIChanged_EmitsCIChanged(t *testing.T) {
	existing := persistence.PullRequest{
		ID: "PR_abc", Repo: "owner/repo", Number: 42,
		Title: "fix: a bug", Status: "open", CIState: "running",
		Author: "alice",
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		LastActivityAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		GlobalID: "PR_abc",
	}
	store := NewStubPRStore()
	store.GetPullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return existing, nil
	}
	pub := &StubPublisher{}

	// Node has COMPLETED/SUCCESS check run → CI state becomes "passing".
	node := basePRNode()
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
	client := singlePageClient([]prSearchNode{node})

	f := NewMyPullRequestsFetcher(client, store, pub, "alice")
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

func TestMyPullRequestsFetcher_Fetch_ExistingPR_OtherChange_EmitsPRUpdated(t *testing.T) {
	existing := persistence.PullRequest{
		ID: "PR_abc", Repo: "owner/repo", Number: 42,
		Title: "old title", Status: "open", CIState: "none",
		Author: "alice",
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		LastActivityAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		GlobalID: "PR_abc",
	}
	store := NewStubPRStore()
	store.GetPullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return existing, nil
	}
	pub := &StubPublisher{}

	// Node has new title → PRUpdated (not CIChanged since CI state didn't change).
	node := basePRNode()
	node.PullRequest.Title = githubv4.String("new title")
	client := singlePageClient([]prSearchNode{node})

	f := NewMyPullRequestsFetcher(client, store, pub, "alice")
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

func TestMyPullRequestsFetcher_Fetch_ExistingPR_NoChanges_NoEvents(t *testing.T) {
	ts := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	existing := persistence.PullRequest{
		ID: "PR_abc", Repo: "owner/repo", Number: 42,
		Title: "fix: a bug", Body: "This PR fixes a bug",
		Status: "open", CIState: "none",
		Author: "alice",
		CreatedAt:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:      ts,
		LastActivityAt: ts,
		GlobalID: "PR_abc",
	}
	store := NewStubPRStore()
	store.GetPullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return existing, nil
	}
	pub := &StubPublisher{}
	client := singlePageClient([]prSearchNode{basePRNode()})

	f := NewMyPullRequestsFetcher(client, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(pub.Events) != 0 {
		t.Fatalf("expected no events, got %d", len(pub.Events))
	}
}

func TestMyPullRequestsFetcher_Fetch_APIError_ReturnsError(t *testing.T) {
	store := NewStubPRStore()
	pub := &StubPublisher{}
	apiErr := errors.New("rate limited")
	client := &StubGraphQLClient{
		QueryFunc: func(_ context.Context, _ interface{}, _ map[string]interface{}) error {
			return apiErr
		},
	}

	f := NewMyPullRequestsFetcher(client, store, pub, "alice")
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

func TestMyPullRequestsFetcher_Fetch_GetPRUnexpectedError_ReturnsError(t *testing.T) {
	dbErr := errors.New("db broken")
	store := NewStubPRStore()
	store.GetPullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return persistence.PullRequest{}, dbErr
	}
	pub := &StubPublisher{}
	client := singlePageClient([]prSearchNode{basePRNode()})

	f := NewMyPullRequestsFetcher(client, store, pub, "alice")
	err := f.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("error = %v, want to wrap %v", err, dbErr)
	}
}

func TestMyPullRequestsFetcher_Fetch_UpsertPRError_ReturnsError(t *testing.T) {
	upsertErr := errors.New("upsert failed")
	store := NewStubPRStore()
	store.UpsertPullRequestFunc = func(pr persistence.PullRequest) error { return upsertErr }
	pub := &StubPublisher{}
	client := singlePageClient([]prSearchNode{basePRNode()})

	f := NewMyPullRequestsFetcher(client, store, pub, "alice")
	err := f.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, upsertErr) {
		t.Errorf("error = %v, want to wrap %v", err, upsertErr)
	}
}

func TestMyPullRequestsFetcher_Fetch_UpsertCheckRunError_ReturnsError(t *testing.T) {
	crErr := errors.New("check run upsert failed")
	store := NewStubPRStore()
	store.UpsertCheckRunFunc = func(cr persistence.CheckRun) error { return crErr }
	pub := &StubPublisher{}

	node := basePRNode()
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
	client := singlePageClient([]prSearchNode{node})

	f := NewMyPullRequestsFetcher(client, store, pub, "alice")
	err := f.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, crErr) {
		t.Errorf("error = %v, want to wrap %v", err, crErr)
	}
}

func TestMyPullRequestsFetcher_Fetch_UpsertReviewerError_ReturnsError(t *testing.T) {
	revErr := errors.New("reviewer upsert failed")
	store := NewStubPRStore()
	store.UpsertReviewerFunc = func(r persistence.Reviewer) error { return revErr }
	pub := &StubPublisher{}

	node := basePRNode()
	node.PullRequest.Reviews = prSearchReviewConnection{
		Nodes: []prSearchReview{{
			Author: prSearchReviewAuthor{Login: githubv4.String("bob")},
			State:  githubv4.String("APPROVED"),
		}},
	}
	client := singlePageClient([]prSearchNode{node})

	f := NewMyPullRequestsFetcher(client, store, pub, "alice")
	err := f.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, revErr) {
		t.Errorf("error = %v, want to wrap %v", err, revErr)
	}
}

func TestMyPullRequestsFetcher_Fetch_SkipsEmptyRepo(t *testing.T) {
	store := NewStubPRStore()
	pub := &StubPublisher{}

	emptyRepoNode := prSearchNode{
		PullRequest: prSearchPR{
			ID:     githubv4.String("PR_empty"),
			Number: githubv4.Int(1),
			// Repository.NameWithOwner left as zero value ("").
		},
	}
	client := singlePageClient([]prSearchNode{emptyRepoNode})

	f := NewMyPullRequestsFetcher(client, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(store.UpsertedPRs) != 0 {
		t.Errorf("expected no DB writes for empty repo, got %d", len(store.UpsertedPRs))
	}
}

func TestMyPullRequestsFetcher_Fetch_Pagination(t *testing.T) {
	store := NewStubPRStore()
	pub := &StubPublisher{}

	page := 0
	page1Node := basePRNode()
	page1Node.PullRequest.ID = githubv4.String("PR_p1")
	page1Node.PullRequest.Number = githubv4.Int(1)

	page2Node := basePRNode()
	page2Node.PullRequest.ID = githubv4.String("PR_p2")
	page2Node.PullRequest.Number = githubv4.Int(2)

	endCursor := githubv4.String("cursor1")
	client := &StubGraphQLClient{
		QueryFunc: func(_ context.Context, q interface{}, _ map[string]interface{}) error {
			mq := q.(*myPRsQuery)
			page++
			if page == 1 {
				mq.Search.Nodes = []prSearchNode{page1Node}
				mq.Search.PageInfo = prSearchPageInfo{
					HasNextPage: githubv4.Boolean(true),
					EndCursor:   endCursor,
				}
			} else {
				mq.Search.Nodes = []prSearchNode{page2Node}
				mq.Search.PageInfo = prSearchPageInfo{HasNextPage: false}
			}
			return nil
		},
	}

	f := NewMyPullRequestsFetcher(client, store, pub, "alice")
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

func TestMyPullRequestsFetcher_Fetch_CheckRunsAndReviewersStored(t *testing.T) {
	store := NewStubPRStore()
	pub := &StubPublisher{}

	node := basePRNode()
	node.PullRequest.CheckSuites = prSearchCheckSuiteConnection{
		Nodes: []prSearchCheckSuite{{
			CheckRuns: prSearchCheckRunConnection{
				Nodes: []prSearchCheckRun{
					{Name: githubv4.String("build"), Status: githubv4.String("COMPLETED"), Conclusion: githubv4.String("SUCCESS")},
					{Name: githubv4.String("test"), Status: githubv4.String("COMPLETED"), Conclusion: githubv4.String("FAILURE")},
				},
			},
		}},
	}
	node.PullRequest.Reviews = prSearchReviewConnection{
		Nodes: []prSearchReview{
			{Author: prSearchReviewAuthor{Login: githubv4.String("bob")}, State: githubv4.String("APPROVED")},
		},
	}
	client := singlePageClient([]prSearchNode{node})

	f := NewMyPullRequestsFetcher(client, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(store.UpsertedCheckRuns) != 2 {
		t.Errorf("expected 2 check runs, got %d", len(store.UpsertedCheckRuns))
	}
	if len(store.UpsertedReviewers) != 1 {
		t.Errorf("expected 1 reviewer, got %d", len(store.UpsertedReviewers))
	}
	if store.UpsertedReviewers[0].Login != "bob" {
		t.Errorf("reviewer login = %q, want %q", store.UpsertedReviewers[0].Login, "bob")
	}
	// CI state should be "failing" since one check failed.
	if store.UpsertedPRs[0].CIState != "failing" {
		t.Errorf("CIState = %q, want %q", store.UpsertedPRs[0].CIState, "failing")
	}
}

// ── DeriveCIState ─────────────────────────────────────────────────────────────

func TestDeriveCIState(t *testing.T) {
	tests := []struct {
		name string
		runs []CheckRunData
		want string
	}{
		{
			name: "no runs → none",
			runs: nil,
			want: "none",
		},
		{
			name: "empty runs → none",
			runs: []CheckRunData{},
			want: "none",
		},
		{
			name: "all COMPLETED SUCCESS → passing",
			runs: []CheckRunData{
				{Status: "COMPLETED", Conclusion: "SUCCESS"},
				{Status: "COMPLETED", Conclusion: "NEUTRAL"},
			},
			want: "passing",
		},
		{
			name: "one IN_PROGRESS → running",
			runs: []CheckRunData{
				{Status: "COMPLETED", Conclusion: "SUCCESS"},
				{Status: "IN_PROGRESS"},
			},
			want: "running",
		},
		{
			name: "QUEUED → running",
			runs: []CheckRunData{{Status: "QUEUED"}},
			want: "running",
		},
		{
			name: "COMPLETED FAILURE → failing",
			runs: []CheckRunData{{Status: "COMPLETED", Conclusion: "FAILURE"}},
			want: "failing",
		},
		{
			name: "COMPLETED TIMED_OUT → failing",
			runs: []CheckRunData{{Status: "COMPLETED", Conclusion: "TIMED_OUT"}},
			want: "failing",
		},
		{
			name: "COMPLETED ACTION_REQUIRED → failing",
			runs: []CheckRunData{{Status: "COMPLETED", Conclusion: "ACTION_REQUIRED"}},
			want: "failing",
		},
		{
			name: "COMPLETED STARTUP_FAILURE → failing",
			runs: []CheckRunData{{Status: "COMPLETED", Conclusion: "STARTUP_FAILURE"}},
			want: "failing",
		},
		{
			name: "COMPLETED SKIPPED → passing",
			runs: []CheckRunData{{Status: "COMPLETED", Conclusion: "SKIPPED"}},
			want: "passing",
		},
		{
			name: "mixed COMPLETED: one fail → failing",
			runs: []CheckRunData{
				{Status: "COMPLETED", Conclusion: "SUCCESS"},
				{Status: "COMPLETED", Conclusion: "FAILURE"},
			},
			want: "failing",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeriveCIState(tt.runs); got != tt.want {
				t.Errorf("DeriveCIState() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ── DerivePRStatus ────────────────────────────────────────────────────────────

func TestDerivePRStatus(t *testing.T) {
	tests := []struct {
		name         string
		inMergeQueue bool
		isDraft      bool
		reviews      []ReviewData
		want         string
	}{
		{
			name:         "merge queued takes precedence over draft",
			inMergeQueue: true,
			isDraft:      true,
			want:         "merge queued",
		},
		{
			name:         "merge queued takes precedence over reviews",
			inMergeQueue: true,
			reviews:      []ReviewData{{State: "CHANGES_REQUESTED"}},
			want:         "merge queued",
		},
		{
			name:    "draft",
			isDraft: true,
			want:    "draft",
		},
		{
			name:    "draft with approved review",
			isDraft: true,
			reviews: []ReviewData{{State: "APPROVED"}},
			want:    "draft",
		},
		{
			name: "changes requested",
			reviews: []ReviewData{
				{State: "CHANGES_REQUESTED"},
				{State: "APPROVED"},
			},
			want: "changes requested",
		},
		{
			name:    "approved",
			reviews: []ReviewData{{State: "APPROVED"}},
			want:    "approved",
		},
		{
			name:    "open — no reviews",
			reviews: nil,
			want:    "open",
		},
		{
			name:    "open — commented only",
			reviews: []ReviewData{{State: "COMMENTED"}},
			want:    "open",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DerivePRStatus(tt.inMergeQueue, tt.isDraft, tt.reviews); got != tt.want {
				t.Errorf("DerivePRStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ── PRsEqual ─────────────────────────────────────────────────────────────────

func TestPRsEqual(t *testing.T) {
	base := persistence.PullRequest{
		ID: "PR_1", Repo: "r/r", Number: 1, Title: "t",
		Status: "open", CIState: "none", Draft: false, Author: "a",
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		LastActivityAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		URL: "https://github.com/r/r/pull/1", GlobalID: "PR_1",
	}

	if !PRsEqual(base, base) {
		t.Error("identical PRs should be equal")
	}

	differ := func(mutate func(*persistence.PullRequest)) persistence.PullRequest {
		cp := base
		mutate(&cp)
		return cp
	}

	diffs := []struct {
		name string
		b    persistence.PullRequest
	}{
		{"ID", differ(func(p *persistence.PullRequest) { p.ID = "PR_2" })},
		{"Title", differ(func(p *persistence.PullRequest) { p.Title = "other" })},
		{"Body", differ(func(p *persistence.PullRequest) { p.Body = "different body" })},
		{"Status", differ(func(p *persistence.PullRequest) { p.Status = "draft" })},
		{"CIState", differ(func(p *persistence.PullRequest) { p.CIState = "failing" })},
		{"Draft", differ(func(p *persistence.PullRequest) { p.Draft = true })},
		{"Author", differ(func(p *persistence.PullRequest) { p.Author = "b" })},
		{"URL", differ(func(p *persistence.PullRequest) { p.URL = "other" })},
		{"GlobalID", differ(func(p *persistence.PullRequest) { p.GlobalID = "PR_2" })},
		{"CreatedAt", differ(func(p *persistence.PullRequest) { p.CreatedAt = time.Now() })},
		{"UpdatedAt", differ(func(p *persistence.PullRequest) { p.UpdatedAt = time.Now() })},
		{"LastActivityAt", differ(func(p *persistence.PullRequest) { p.LastActivityAt = time.Now() })},
	}
	for _, d := range diffs {
		t.Run(d.name, func(t *testing.T) {
			if PRsEqual(base, d.b) {
				t.Errorf("PRs with different %s should not be equal", d.name)
			}
		})
	}
}

// ── Status field mapping ──────────────────────────────────────────────────────

func TestMyPullRequestsFetcher_Fetch_MergeQueued(t *testing.T) {
	store := NewStubPRStore()
	pub := &StubPublisher{}

	node := basePRNode()
	node.PullRequest.MergeQueueEntry = &prSearchMergeQueueEntry{ID: "MQE_1"}
	client := singlePageClient([]prSearchNode{node})

	f := NewMyPullRequestsFetcher(client, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if store.UpsertedPRs[0].Status != "merge queued" {
		t.Errorf("Status = %q, want %q", store.UpsertedPRs[0].Status, "merge queued")
	}
}

func TestMyPullRequestsFetcher_Fetch_DraftPR(t *testing.T) {
	store := NewStubPRStore()
	pub := &StubPublisher{}

	node := basePRNode()
	node.PullRequest.IsDraft = githubv4.Boolean(true)
	client := singlePageClient([]prSearchNode{node})

	f := NewMyPullRequestsFetcher(client, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	pr := store.UpsertedPRs[0]
	if pr.Status != "draft" {
		t.Errorf("Status = %q, want %q", pr.Status, "draft")
	}
	if !pr.Draft {
		t.Error("Draft should be true")
	}
}

// ── Stale PR cleanup ─────────────────────────────────────────────────────────

func TestMyPullRequestsFetcher_Fetch_CleansUpStalePRs(t *testing.T) {
	store := NewStubPRStore()
	pub := &StubPublisher{}

	// API returns PR #42 only.
	client := singlePageClient([]prSearchNode{basePRNode()})

	// DB has PR #42 and stale PR #99 authored by alice.
	stalePR := persistence.PullRequest{
		ID: "PR_stale", Repo: "owner/repo", Number: 99, Title: "stale",
		Author: "alice", Status: "open", CIState: "none",
	}
	store.ListPullRequestsByAuthorFunc = func(author string) ([]persistence.PullRequest, error) {
		return []persistence.PullRequest{
			{ID: "PR_abc", Repo: "owner/repo", Number: 42, Author: "alice"},
			stalePR,
		}, nil
	}
	store.DeletePullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return stalePR, nil
	}

	f := NewMyPullRequestsFetcher(client, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	// Should have deleted only the stale PR.
	if len(store.DeletedPRKeys) != 1 {
		t.Fatalf("expected 1 deletion, got %d", len(store.DeletedPRKeys))
	}
	if store.DeletedPRKeys[0].Number != 99 {
		t.Errorf("deleted PR number = %d, want 99", store.DeletedPRKeys[0].Number)
	}

	// Should have emitted PRRemoved event.
	var foundRemoved bool
	for _, e := range pub.Events {
		if e.Type == eventbus.PRRemoved {
			foundRemoved = true
			if pr, ok := e.Before.(persistence.PullRequest); !ok || pr.Number != 99 {
				t.Errorf("PRRemoved Before = %v, want PR #99", e.Before)
			}
			if e.After != nil {
				t.Errorf("PRRemoved After = %v, want nil", e.After)
			}
		}
	}
	if !foundRemoved {
		t.Error("expected PRRemoved event")
	}
}

func TestMyPullRequestsFetcher_Fetch_NoCleanupOnAPIError(t *testing.T) {
	store := NewStubPRStore()
	pub := &StubPublisher{}

	client := &StubGraphQLClient{
		QueryFunc: func(_ context.Context, _ interface{}, _ map[string]interface{}) error {
			return errors.New("api error")
		},
	}

	store.ListPullRequestsByAuthorFunc = func(author string) ([]persistence.PullRequest, error) {
		return []persistence.PullRequest{
			{ID: "PR_stale", Repo: "owner/repo", Number: 99, Author: "alice"},
		}, nil
	}

	f := NewMyPullRequestsFetcher(client, store, pub, "alice")
	_ = f.Fetch(context.Background()) // expected to fail

	// Should NOT have deleted anything because API call failed.
	if len(store.DeletedPRKeys) != 0 {
		t.Errorf("expected 0 deletions on API error, got %d", len(store.DeletedPRKeys))
	}
}

func TestMyPullRequestsFetcher_Fetch_CleanupListError(t *testing.T) {
	store := NewStubPRStore()
	pub := &StubPublisher{}

	client := singlePageClient([]prSearchNode{basePRNode()})

	store.ListPullRequestsByAuthorFunc = func(author string) ([]persistence.PullRequest, error) {
		return nil, errors.New("list error")
	}

	f := NewMyPullRequestsFetcher(client, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	// List error → no deletions, no panic.
	if len(store.DeletedPRKeys) != 0 {
		t.Errorf("expected 0 deletions on list error, got %d", len(store.DeletedPRKeys))
	}
}

func TestMyPullRequestsFetcher_Fetch_CleanupDeleteError(t *testing.T) {
	store := NewStubPRStore()
	pub := &StubPublisher{}

	client := singlePageClient([]prSearchNode{basePRNode()})

	store.ListPullRequestsByAuthorFunc = func(author string) ([]persistence.PullRequest, error) {
		return []persistence.PullRequest{
			{ID: "PR_abc", Repo: "owner/repo", Number: 42, Author: "alice"},
			{ID: "PR_stale", Repo: "owner/repo", Number: 99, Author: "alice"},
		}, nil
	}
	store.DeletePullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return persistence.PullRequest{}, errors.New("delete error")
	}

	f := NewMyPullRequestsFetcher(client, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	// Delete error → no event emitted, but fetch still succeeds.
	for _, e := range pub.Events {
		if e.Type == eventbus.PRRemoved {
			t.Error("expected no PRRemoved event on delete error")
		}
	}
}

func TestExtractReviewThreads(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		conn := prSearchReviewThreadConnection{}
		got := extractReviewThreads(conn)
		if len(got) != 0 {
			t.Errorf("expected 0 threads, got %d", len(got))
		}
	})

	t.Run("with comment", func(t *testing.T) {
		conn := prSearchReviewThreadConnection{
			Nodes: []prSearchReviewThread{
				{
					ID:         "RT_1",
					IsResolved: true,
					Path:       "main.go",
					Line:       42,
					Comments: prSearchReviewThreadCommentConnection{
						Nodes: []prSearchReviewThreadComment{
							{Body: "nit: rename this"},
						},
					},
				},
			},
		}
		got := extractReviewThreads(conn)
		if len(got) != 1 {
			t.Fatalf("expected 1 thread, got %d", len(got))
		}
		if got[0].ID != "RT_1" {
			t.Errorf("ID = %q, want %q", got[0].ID, "RT_1")
		}
		if !got[0].Resolved {
			t.Error("Resolved = false, want true")
		}
		if got[0].Body != "nit: rename this" {
			t.Errorf("Body = %q, want %q", got[0].Body, "nit: rename this")
		}
		if got[0].Path != "main.go" {
			t.Errorf("Path = %q, want %q", got[0].Path, "main.go")
		}
		if got[0].Line != 42 {
			t.Errorf("Line = %d, want %d", got[0].Line, 42)
		}
	})

	t.Run("no comments", func(t *testing.T) {
		conn := prSearchReviewThreadConnection{
			Nodes: []prSearchReviewThread{
				{
					ID:   "RT_2",
					Path: "lib.go",
					Line: 10,
				},
			},
		}
		got := extractReviewThreads(conn)
		if len(got) != 1 {
			t.Fatalf("expected 1 thread, got %d", len(got))
		}
		if got[0].Body != "" {
			t.Errorf("Body = %q, want empty string", got[0].Body)
		}
	})
}
