package ghcli

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/evanisnor/argh/internal/api"
	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
)

func TestGHCLIMyPRsFetcher_Fetch_Success(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		if args[0] == "search" {
			return []byte(myPRsSearchJSON), nil
		}
		if args[0] == "pr" && args[1] == "view" {
			return []byte(myPRsDetailJSON), nil
		}
		if args[0] == "api" && args[1] == "graphql" {
			return []byte(`{"data":{"node":{"mergeQueueEntry":null}}}`), nil
		}
		return nil, fmt.Errorf("unexpected command: %v", args)
	}
	store := api.NewStubPRStore()
	pub := &api.StubPublisher{}

	f := NewGHCLIMyPRsFetcher(runner, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch error = %v", err)
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
	if pr.CIState != "passing" {
		t.Errorf("CIState = %q, want %q", pr.CIState, "passing")
	}
	if pr.Status != "approved" {
		t.Errorf("Status = %q, want %q", pr.Status, "approved")
	}
	if pr.Draft {
		t.Error("Draft should be false")
	}

	if len(pub.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(pub.Events))
	}
	if pub.Events[0].Type != eventbus.PRUpdated {
		t.Errorf("event type = %v, want %v", pub.Events[0].Type, eventbus.PRUpdated)
	}

	// Verify three-phase: search call + pr view call + merge queue graphql call
	if runner.CallCount() != 3 {
		t.Errorf("expected 3 runner calls (search + view + graphql), got %d", runner.CallCount())
	}
}

func TestGHCLIMyPRsFetcher_Fetch_EmptyResult(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		return []byte("[]"), nil
	}
	store := api.NewStubPRStore()
	pub := &api.StubPublisher{}

	f := NewGHCLIMyPRsFetcher(runner, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch error = %v", err)
	}

	if len(store.UpsertedPRs) != 0 {
		t.Errorf("expected 0 upserted PRs, got %d", len(store.UpsertedPRs))
	}
	if len(pub.Events) != 0 {
		t.Errorf("expected 0 events, got %d", len(pub.Events))
	}
}

func TestGHCLIMyPRsFetcher_Fetch_CommandError(t *testing.T) {
	cmdErr := errors.New("command failed")
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
		return nil, cmdErr
	}
	store := api.NewStubPRStore()
	pub := &api.StubPublisher{}

	f := NewGHCLIMyPRsFetcher(runner, store, pub, "alice")
	err := f.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, cmdErr) {
		t.Errorf("error = %v, want to wrap %v", err, cmdErr)
	}
}

func TestGHCLIMyPRsFetcher_Fetch_MalformedJSON(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
		return []byte("{invalid json"), nil
	}
	store := api.NewStubPRStore()
	pub := &api.StubPublisher{}

	f := NewGHCLIMyPRsFetcher(runner, store, pub, "alice")
	err := f.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGHCLIMyPRsFetcher_Fetch_PersistError(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		if args[0] == "search" {
			return []byte(myPRsSearchJSON), nil
		}
		return []byte(myPRsDetailJSON), nil
	}
	upsertErr := errors.New("upsert failed")
	store := api.NewStubPRStore()
	store.UpsertPullRequestFunc = func(pr persistence.PullRequest) error { return upsertErr }
	pub := &api.StubPublisher{}

	f := NewGHCLIMyPRsFetcher(runner, store, pub, "alice")
	err := f.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, upsertErr) {
		t.Errorf("error = %v, want to wrap %v", err, upsertErr)
	}
}

func TestGHCLIMyPRsFetcher_Fetch_SkipsEmptyRepo(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		return []byte(`[{"id":"PR_1","number":1,"repository":{"name":"","nameWithOwner":""}}]`), nil
	}
	store := api.NewStubPRStore()
	pub := &api.StubPublisher{}

	f := NewGHCLIMyPRsFetcher(runner, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch error = %v", err)
	}
	if len(store.UpsertedPRs) != 0 {
		t.Errorf("expected 0 upserted PRs for empty repo, got %d", len(store.UpsertedPRs))
	}
}

func TestGHCLIMyPRsFetcher_CorrectArgs(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
		return []byte("[]"), nil
	}
	store := api.NewStubPRStore()
	pub := &api.StubPublisher{}

	f := NewGHCLIMyPRsFetcher(runner, store, pub, "alice")
	f.Fetch(context.Background())

	call := runner.LastCall()
	if call == nil {
		t.Fatal("expected a call, got nil")
	}
	if call[0] != "search" || call[1] != "prs" {
		t.Errorf("expected [search prs ...], got %v", call)
	}
	if runner.FindCall("--author", "alice") == nil {
		t.Error("expected --author alice in args")
	}
}

func TestGHCLIMyPRsFetcher_Fetch_DetailFetchError_PersistsWithEmptyDetail(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		if args[0] == "search" {
			return []byte(myPRsSearchJSON), nil
		}
		// Detail fetch fails
		return nil, errors.New("pr view failed")
	}
	store := api.NewStubPRStore()
	pub := &api.StubPublisher{}

	f := NewGHCLIMyPRsFetcher(runner, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch should succeed despite detail error, got: %v", err)
	}

	// PR still persisted with empty CI/review data
	if len(store.UpsertedPRs) != 1 {
		t.Fatalf("expected 1 upserted PR, got %d", len(store.UpsertedPRs))
	}
	if store.UpsertedPRs[0].CIState != "none" {
		t.Errorf("CIState = %q, want %q (empty detail)", store.UpsertedPRs[0].CIState, "none")
	}
}

func TestGHCLIMyPRsFetcher_Fetch_DetailMalformedJSON_PersistsWithEmptyDetail(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		if args[0] == "search" {
			return []byte(myPRsSearchJSON), nil
		}
		return []byte("{bad json"), nil
	}
	store := api.NewStubPRStore()
	pub := &api.StubPublisher{}

	f := NewGHCLIMyPRsFetcher(runner, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch should succeed despite malformed detail, got: %v", err)
	}

	if len(store.UpsertedPRs) != 1 {
		t.Fatalf("expected 1 upserted PR, got %d", len(store.UpsertedPRs))
	}
}

func TestFetchPRDetail_Success(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
		return []byte(myPRsDetailJSON), nil
	}

	detail, err := fetchPRDetail(context.Background(), runner, "owner/repo", 42, "statusCheckRollup,reviews,reviewRequests")
	if err != nil {
		t.Fatalf("fetchPRDetail error = %v", err)
	}
	if len(detail.StatusCheckRollup) != 1 {
		t.Errorf("expected 1 status check, got %d", len(detail.StatusCheckRollup))
	}
	if len(detail.Reviews) != 1 {
		t.Errorf("expected 1 review, got %d", len(detail.Reviews))
	}

	call := runner.FindCall("pr", "view")
	if call == nil {
		t.Fatal("expected pr view call")
	}
	if runner.FindCall("--repo", "owner/repo") == nil {
		t.Error("expected --repo owner/repo in args")
	}
}

func TestFetchPRDetail_CommandError(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
		return nil, errors.New("command failed")
	}

	_, err := fetchPRDetail(context.Background(), runner, "owner/repo", 42, "statusCheckRollup")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFetchPRDetail_MalformedJSON(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
		return []byte("not json"), nil
	}

	_, err := fetchPRDetail(context.Background(), runner, "owner/repo", 42, "statusCheckRollup")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── Normalization ───────────────────────────────────────────────────────────

func TestConvertStatusChecks(t *testing.T) {
	checks := []ghStatusCheck{
		{Name: "build", Status: "COMPLETED", Conclusion: "SUCCESS"},
		{Context: "ci/test", State: "SUCCESS"},
		{Context: "ci/lint", State: "PENDING"},
		{Name: "deploy", State: "FAILURE"},
	}

	runs := convertStatusChecks(checks)

	if len(runs) != 4 {
		t.Fatalf("expected 4 runs, got %d", len(runs))
	}

	// Check run 1: explicit status/conclusion
	if runs[0].Name != "build" || runs[0].Status != "COMPLETED" || runs[0].Conclusion != "SUCCESS" {
		t.Errorf("run[0] = %+v", runs[0])
	}
	// Check run 2: state-based (SUCCESS → COMPLETED/SUCCESS)
	if runs[1].Name != "ci/test" || runs[1].Status != "COMPLETED" || runs[1].Conclusion != "SUCCESS" {
		t.Errorf("run[1] = %+v", runs[1])
	}
	// Check run 3: PENDING → IN_PROGRESS
	if runs[2].Name != "ci/lint" || runs[2].Status != "IN_PROGRESS" {
		t.Errorf("run[2] = %+v", runs[2])
	}
	// Check run 4: FAILURE → COMPLETED/FAILURE
	if runs[3].Name != "deploy" || runs[3].Status != "COMPLETED" || runs[3].Conclusion != "FAILURE" {
		t.Errorf("run[3] = %+v", runs[3])
	}
}

func TestConvertReviews(t *testing.T) {
	reviews := []ghReview{
		{Author: ghAuthor{Login: "bob"}, State: "APPROVED"},
		{Author: ghAuthor{Login: "carol"}, State: "CHANGES_REQUESTED"},
	}

	result := convertReviews(reviews)
	if len(result) != 2 {
		t.Fatalf("expected 2 reviews, got %d", len(result))
	}
	if result[0].Login != "bob" || result[0].State != "APPROVED" {
		t.Errorf("result[0] = %+v", result[0])
	}
	if result[1].Login != "carol" || result[1].State != "CHANGES_REQUESTED" {
		t.Errorf("result[1] = %+v", result[1])
	}
}

func TestNormalizeStatus(t *testing.T) {
	tests := []struct {
		status, state, want string
	}{
		{"COMPLETED", "", "COMPLETED"},
		{"IN_PROGRESS", "", "IN_PROGRESS"},
		{"", "SUCCESS", "COMPLETED"},
		{"", "FAILURE", "COMPLETED"},
		{"", "ERROR", "COMPLETED"},
		{"", "EXPECTED", "COMPLETED"},
		{"", "NEUTRAL", "COMPLETED"},
		{"", "PENDING", "IN_PROGRESS"},
		{"", "unknown", "IN_PROGRESS"},
	}
	for _, tt := range tests {
		got := normalizeStatus(tt.status, tt.state)
		if got != tt.want {
			t.Errorf("normalizeStatus(%q, %q) = %q, want %q", tt.status, tt.state, got, tt.want)
		}
	}
}

func TestNormalizeConclusion(t *testing.T) {
	tests := []struct {
		conclusion, state, want string
	}{
		{"SUCCESS", "", "SUCCESS"},
		{"FAILURE", "", "FAILURE"},
		{"", "SUCCESS", "SUCCESS"},
		{"", "FAILURE", "FAILURE"},
		{"", "ERROR", "FAILURE"},
		{"", "NEUTRAL", "NEUTRAL"},
		{"", "EXPECTED", "NEUTRAL"},
		{"", "PENDING", ""},
		{"", "unknown", ""},
	}
	for _, tt := range tests {
		got := normalizeConclusion(tt.conclusion, tt.state)
		if got != tt.want {
			t.Errorf("normalizeConclusion(%q, %q) = %q, want %q", tt.conclusion, tt.state, got, tt.want)
		}
	}
}

func TestConvertStatusChecks_Empty(t *testing.T) {
	runs := convertStatusChecks(nil)
	if len(runs) != 0 {
		t.Errorf("expected 0 runs for nil input, got %d", len(runs))
	}
}

func TestConvertReviews_Empty(t *testing.T) {
	result := convertReviews(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 reviews for nil input, got %d", len(result))
	}
}

// ── Merge queue detection ───────────────────────────────────────────────────

func TestFetchMergeQueueStatus_InQueue(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
		return []byte(`{"data":{"node":{"mergeQueueEntry":{"id":"MQE_123"}}}}`), nil
	}
	if !fetchMergeQueueStatus(context.Background(), runner, "PR_abc") {
		t.Error("expected true for non-null mergeQueueEntry")
	}
}

func TestFetchMergeQueueStatus_NotInQueue(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
		return []byte(`{"data":{"node":{"mergeQueueEntry":null}}}`), nil
	}
	if fetchMergeQueueStatus(context.Background(), runner, "PR_abc") {
		t.Error("expected false for null mergeQueueEntry")
	}
}

func TestFetchMergeQueueStatus_CommandError(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
		return nil, errors.New("api error")
	}
	if fetchMergeQueueStatus(context.Background(), runner, "PR_abc") {
		t.Error("expected false on command error")
	}
}

func TestFetchMergeQueueStatus_MalformedJSON(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
		return []byte("{bad"), nil
	}
	if fetchMergeQueueStatus(context.Background(), runner, "PR_abc") {
		t.Error("expected false on malformed JSON")
	}
}

func TestFetchMergeQueueStatus_CorrectArgs(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
		return []byte(`{"data":{"node":{"mergeQueueEntry":null}}}`), nil
	}
	fetchMergeQueueStatus(context.Background(), runner, "PR_abc")

	call := runner.FindCall("api", "graphql")
	if call == nil {
		t.Fatal("expected api graphql call")
	}
	if runner.FindCall("nodeID=PR_abc") == nil {
		t.Error("expected nodeID=PR_abc in args")
	}
}

func TestGHCLIMyPRsFetcher_Fetch_MergeQueued(t *testing.T) {
	callCount := 0
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		if args[0] == "search" {
			return []byte(myPRsSearchJSON), nil
		}
		if args[0] == "pr" && args[1] == "view" {
			return []byte(myPRsDetailJSON), nil
		}
		if args[0] == "api" && args[1] == "graphql" {
			callCount++
			return []byte(`{"data":{"node":{"mergeQueueEntry":{"id":"MQE_1"}}}}`), nil
		}
		return nil, fmt.Errorf("unexpected: %v", args)
	}
	store := api.NewStubPRStore()
	pub := &api.StubPublisher{}

	f := NewGHCLIMyPRsFetcher(runner, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch error = %v", err)
	}

	if len(store.UpsertedPRs) != 1 {
		t.Fatalf("expected 1 PR, got %d", len(store.UpsertedPRs))
	}
	if store.UpsertedPRs[0].Status != "merge queued" {
		t.Errorf("Status = %q, want %q", store.UpsertedPRs[0].Status, "merge queued")
	}
	if callCount != 1 {
		t.Errorf("expected 1 graphql call, got %d", callCount)
	}
}

// ── Stale PR cleanup ────────────────────────────────────────────────────────

func TestGHCLIMyPRsFetcher_CleansUpStalePRs(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		if args[0] == "search" {
			return []byte(myPRsSearchJSON), nil
		}
		return []byte(myPRsDetailJSON), nil
	}
	stalePR := persistence.PullRequest{Repo: "owner/old", Number: 99, Author: "alice"}
	keptPR := persistence.PullRequest{Repo: "owner/repo", Number: 42, Author: "alice"}
	store := api.NewStubPRStore()
	store.ListPullRequestsByAuthorFunc = func(author string) ([]persistence.PullRequest, error) {
		return []persistence.PullRequest{stalePR, keptPR}, nil
	}
	store.DeletePullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return stalePR, nil
	}
	pub := &api.StubPublisher{}

	f := NewGHCLIMyPRsFetcher(runner, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch error = %v", err)
	}

	// Only the stale PR should be deleted
	if len(store.DeletedPRKeys) != 1 {
		t.Fatalf("expected 1 deleted PR, got %d", len(store.DeletedPRKeys))
	}
	if store.DeletedPRKeys[0].Repo != "owner/old" || store.DeletedPRKeys[0].Number != 99 {
		t.Errorf("deleted wrong PR: %+v", store.DeletedPRKeys[0])
	}

	// Should have PRUpdated (from persist) + PRRemoved (from cleanup)
	var removed []eventbus.Event
	for _, e := range pub.Events {
		if e.Type == eventbus.PRRemoved {
			removed = append(removed, e)
		}
	}
	if len(removed) != 1 {
		t.Fatalf("expected 1 PRRemoved event, got %d", len(removed))
	}
	if removed[0].Before.(persistence.PullRequest).Repo != "owner/old" {
		t.Errorf("PRRemoved Before = %+v", removed[0].Before)
	}
	if removed[0].After != nil {
		t.Error("PRRemoved After should be nil")
	}
}

func TestGHCLIMyPRsFetcher_NoCleanupOnSearchError(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
		return nil, errors.New("search failed")
	}
	store := api.NewStubPRStore()
	store.ListPullRequestsByAuthorFunc = func(author string) ([]persistence.PullRequest, error) {
		t.Error("ListPullRequestsByAuthor should not be called when search fails")
		return nil, nil
	}
	pub := &api.StubPublisher{}

	f := NewGHCLIMyPRsFetcher(runner, store, pub, "alice")
	_ = f.Fetch(context.Background())

	if len(store.DeletedPRKeys) != 0 {
		t.Errorf("expected 0 deletions, got %d", len(store.DeletedPRKeys))
	}
}

func TestGHCLIMyPRsFetcher_CleanupListError(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		if args[0] == "search" {
			return []byte("[]"), nil
		}
		return nil, fmt.Errorf("unexpected: %v", args)
	}
	store := api.NewStubPRStore()
	store.ListPullRequestsByAuthorFunc = func(author string) ([]persistence.PullRequest, error) {
		return nil, errors.New("list failed")
	}
	pub := &api.StubPublisher{}

	f := NewGHCLIMyPRsFetcher(runner, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch error = %v", err)
	}

	if len(store.DeletedPRKeys) != 0 {
		t.Errorf("expected 0 deletions on list error, got %d", len(store.DeletedPRKeys))
	}
}

func TestGHCLIMyPRsFetcher_CleanupDeleteError(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		if args[0] == "search" {
			return []byte("[]"), nil
		}
		return nil, fmt.Errorf("unexpected: %v", args)
	}
	stalePR := persistence.PullRequest{Repo: "owner/gone", Number: 77, Author: "alice"}
	store := api.NewStubPRStore()
	store.ListPullRequestsByAuthorFunc = func(author string) ([]persistence.PullRequest, error) {
		return []persistence.PullRequest{stalePR}, nil
	}
	store.DeletePullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return persistence.PullRequest{}, errors.New("delete failed")
	}
	pub := &api.StubPublisher{}

	f := NewGHCLIMyPRsFetcher(runner, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch error = %v", err)
	}

	// Delete was attempted
	if len(store.DeletedPRKeys) != 1 {
		t.Fatalf("expected 1 delete attempt, got %d", len(store.DeletedPRKeys))
	}
	// No PRRemoved event because delete failed
	for _, e := range pub.Events {
		if e.Type == eventbus.PRRemoved {
			t.Error("should not emit PRRemoved when delete fails")
		}
	}
}

// ── Test fixtures ───────────────────────────────────────────────────────────

// Search result: only fields supported by gh search prs
const myPRsSearchJSON = `[
  {
    "id": "PR_abc",
    "number": 42,
    "title": "fix: a bug",
    "state": "OPEN",
    "isDraft": false,
    "url": "https://github.com/owner/repo/pull/42",
    "createdAt": "2024-01-01T00:00:00Z",
    "updatedAt": "2024-01-02T00:00:00Z",
    "author": {"login": "alice"},
    "repository": {"name": "repo", "nameWithOwner": "owner/repo"}
  }
]`

// Detail result from gh pr view --json
const myPRsDetailJSON = `{
  "statusCheckRollup": [
    {"name": "ci", "status": "COMPLETED", "conclusion": "SUCCESS"}
  ],
  "reviews": [
    {"author": {"login": "bob"}, "state": "APPROVED"}
  ],
  "reviewRequests": []
}`
