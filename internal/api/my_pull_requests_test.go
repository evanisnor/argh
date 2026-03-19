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
		Title: "fix: a bug", Status: "open", CIState: "none",
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

// ── deriveCIState ─────────────────────────────────────────────────────────────

func TestDeriveCIState(t *testing.T) {
	tests := []struct {
		name string
		runs []checkRunData
		want string
	}{
		{
			name: "no runs → none",
			runs: nil,
			want: "none",
		},
		{
			name: "empty runs → none",
			runs: []checkRunData{},
			want: "none",
		},
		{
			name: "all COMPLETED SUCCESS → passing",
			runs: []checkRunData{
				{Status: "COMPLETED", Conclusion: "SUCCESS"},
				{Status: "COMPLETED", Conclusion: "NEUTRAL"},
			},
			want: "passing",
		},
		{
			name: "one IN_PROGRESS → running",
			runs: []checkRunData{
				{Status: "COMPLETED", Conclusion: "SUCCESS"},
				{Status: "IN_PROGRESS"},
			},
			want: "running",
		},
		{
			name: "QUEUED → running",
			runs: []checkRunData{{Status: "QUEUED"}},
			want: "running",
		},
		{
			name: "COMPLETED FAILURE → failing",
			runs: []checkRunData{{Status: "COMPLETED", Conclusion: "FAILURE"}},
			want: "failing",
		},
		{
			name: "COMPLETED TIMED_OUT → failing",
			runs: []checkRunData{{Status: "COMPLETED", Conclusion: "TIMED_OUT"}},
			want: "failing",
		},
		{
			name: "COMPLETED ACTION_REQUIRED → failing",
			runs: []checkRunData{{Status: "COMPLETED", Conclusion: "ACTION_REQUIRED"}},
			want: "failing",
		},
		{
			name: "COMPLETED STARTUP_FAILURE → failing",
			runs: []checkRunData{{Status: "COMPLETED", Conclusion: "STARTUP_FAILURE"}},
			want: "failing",
		},
		{
			name: "COMPLETED SKIPPED → passing",
			runs: []checkRunData{{Status: "COMPLETED", Conclusion: "SKIPPED"}},
			want: "passing",
		},
		{
			name: "mixed COMPLETED: one fail → failing",
			runs: []checkRunData{
				{Status: "COMPLETED", Conclusion: "SUCCESS"},
				{Status: "COMPLETED", Conclusion: "FAILURE"},
			},
			want: "failing",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deriveCIState(tt.runs); got != tt.want {
				t.Errorf("deriveCIState() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ── derivePRStatus ────────────────────────────────────────────────────────────

func TestDerivePRStatus(t *testing.T) {
	tests := []struct {
		name         string
		inMergeQueue bool
		isDraft      bool
		reviews      []reviewData
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
			reviews:      []reviewData{{State: "CHANGES_REQUESTED"}},
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
			reviews: []reviewData{{State: "APPROVED"}},
			want:    "draft",
		},
		{
			name: "changes requested",
			reviews: []reviewData{
				{State: "CHANGES_REQUESTED"},
				{State: "APPROVED"},
			},
			want: "changes requested",
		},
		{
			name:    "approved",
			reviews: []reviewData{{State: "APPROVED"}},
			want:    "approved",
		},
		{
			name:    "open — no reviews",
			reviews: nil,
			want:    "open",
		},
		{
			name:    "open — commented only",
			reviews: []reviewData{{State: "COMMENTED"}},
			want:    "open",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := derivePRStatus(tt.inMergeQueue, tt.isDraft, tt.reviews); got != tt.want {
				t.Errorf("derivePRStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ── prsEqual ─────────────────────────────────────────────────────────────────

func TestPRsEqual(t *testing.T) {
	base := persistence.PullRequest{
		ID: "PR_1", Repo: "r/r", Number: 1, Title: "t",
		Status: "open", CIState: "none", Draft: false, Author: "a",
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		LastActivityAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		URL: "https://github.com/r/r/pull/1", GlobalID: "PR_1",
	}

	if !prsEqual(base, base) {
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
			if prsEqual(base, d.b) {
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
