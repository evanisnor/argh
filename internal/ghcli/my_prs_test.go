package ghcli

import (
	"context"
	"errors"
	"testing"

	"github.com/evanisnor/argh/internal/api"
	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
)

func TestGHCLIMyPRsFetcher_Fetch_Success(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
		return []byte(myPRsJSON), nil
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
}

func TestGHCLIMyPRsFetcher_Fetch_EmptyResult(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
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
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
		return []byte(myPRsJSON), nil
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
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
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

// ── Test fixtures ───────────────────────────────────────────────────────────

const myPRsJSON = `[
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
    "repository": {"name": "repo", "nameWithOwner": "owner/repo"},
    "statusCheckRollup": [
      {"name": "ci", "status": "COMPLETED", "conclusion": "SUCCESS"}
    ],
    "reviews": [
      {"author": {"login": "bob"}, "state": "APPROVED"}
    ],
    "reviewRequests": []
  }
]`
