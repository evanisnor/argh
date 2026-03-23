package ghcli

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/evanisnor/argh/internal/api"
	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
)

func TestGHCLIReviewQueueFetcher_Fetch_Success(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		if args[0] == "search" {
			return []byte(rqSearchJSON), nil
		}
		if args[0] == "pr" && args[1] == "view" {
			return []byte(rqDetailJSON), nil
		}
		return nil, fmt.Errorf("unexpected command: %v", args)
	}
	store := api.NewStubReviewQueueStore()
	pub := &api.StubPublisher{}

	f := NewGHCLIReviewQueueFetcher(runner, store, pub, "alice")
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
	if pr.Number != 10 {
		t.Errorf("Number = %d, want %d", pr.Number, 10)
	}
	if pr.Author != "bob" {
		t.Errorf("Author = %q, want %q", pr.Author, "bob")
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

	if len(pub.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(pub.Events))
	}
	if pub.Events[0].Type != eventbus.PRUpdated {
		t.Errorf("event type = %v, want %v", pub.Events[0].Type, eventbus.PRUpdated)
	}

	// Verify two-phase: search call + pr view call
	if runner.CallCount() != 2 {
		t.Errorf("expected 2 runner calls (search + view), got %d", runner.CallCount())
	}
}

func TestGHCLIReviewQueueFetcher_Fetch_EmptyResult(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		return []byte("[]"), nil
	}
	store := api.NewStubReviewQueueStore()
	pub := &api.StubPublisher{}

	f := NewGHCLIReviewQueueFetcher(runner, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch error = %v", err)
	}
	if len(store.UpsertedPRs) != 0 {
		t.Errorf("expected 0 PRs, got %d", len(store.UpsertedPRs))
	}
}

func TestGHCLIReviewQueueFetcher_Fetch_CommandError(t *testing.T) {
	cmdErr := errors.New("command failed")
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
		return nil, cmdErr
	}
	store := api.NewStubReviewQueueStore()
	pub := &api.StubPublisher{}

	f := NewGHCLIReviewQueueFetcher(runner, store, pub, "alice")
	err := f.Fetch(context.Background())
	if !errors.Is(err, cmdErr) {
		t.Errorf("error = %v, want to wrap %v", err, cmdErr)
	}
}

func TestGHCLIReviewQueueFetcher_Fetch_MalformedJSON(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
		return []byte("not json"), nil
	}
	store := api.NewStubReviewQueueStore()
	pub := &api.StubPublisher{}

	f := NewGHCLIReviewQueueFetcher(runner, store, pub, "alice")
	err := f.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGHCLIReviewQueueFetcher_Fetch_PersistError(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		if args[0] == "search" {
			return []byte(rqSearchJSON), nil
		}
		return []byte(rqDetailJSON), nil
	}
	upsertErr := errors.New("upsert failed")
	store := api.NewStubReviewQueueStore()
	store.UpsertPullRequestFunc = func(pr persistence.PullRequest) error { return upsertErr }
	pub := &api.StubPublisher{}

	f := NewGHCLIReviewQueueFetcher(runner, store, pub, "alice")
	err := f.Fetch(context.Background())
	if !errors.Is(err, upsertErr) {
		t.Errorf("error = %v, want to wrap %v", err, upsertErr)
	}
}

func TestGHCLIReviewQueueFetcher_Fetch_SkipsEmptyRepo(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		return []byte(`[{"id":"PR_1","number":1,"repository":{"name":"","nameWithOwner":""}}]`), nil
	}
	store := api.NewStubReviewQueueStore()
	pub := &api.StubPublisher{}

	f := NewGHCLIReviewQueueFetcher(runner, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch error = %v", err)
	}
	if len(store.UpsertedPRs) != 0 {
		t.Errorf("expected 0 PRs for empty repo, got %d", len(store.UpsertedPRs))
	}
}

func TestGHCLIReviewQueueFetcher_CorrectArgs(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
		return []byte("[]"), nil
	}
	store := api.NewStubReviewQueueStore()
	pub := &api.StubPublisher{}

	f := NewGHCLIReviewQueueFetcher(runner, store, pub, "alice")
	f.Fetch(context.Background())

	if runner.FindCall("--review-requested", "alice") == nil {
		t.Error("expected --review-requested alice in args")
	}
}

func TestGHCLIReviewQueueFetcher_Fetch_DetailFetchError_PersistsWithEmptyDetail(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		if args[0] == "search" {
			return []byte(rqSearchJSON), nil
		}
		return nil, errors.New("pr view failed")
	}
	store := api.NewStubReviewQueueStore()
	pub := &api.StubPublisher{}

	f := NewGHCLIReviewQueueFetcher(runner, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch should succeed despite detail error, got: %v", err)
	}

	if len(store.UpsertedPRs) != 1 {
		t.Fatalf("expected 1 upserted PR, got %d", len(store.UpsertedPRs))
	}
	if store.UpsertedPRs[0].CIState != "none" {
		t.Errorf("CIState = %q, want %q (empty detail)", store.UpsertedPRs[0].CIState, "none")
	}
}

// ── convertCommits ──────────────────────────────────────────────────────────

func TestConvertCommits(t *testing.T) {
	commitTime := time.Date(2024, 2, 1, 12, 0, 0, 0, time.UTC)
	commits := []ghCommit{
		{AuthorLogin: "bob", CommittedDate: commitTime},
	}

	result := convertCommits(commits)
	if len(result) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(result))
	}
	if result[0].AuthorLogin != "bob" {
		t.Errorf("AuthorLogin = %q, want %q", result[0].AuthorLogin, "bob")
	}
	if !result[0].CommittedDate.Time.Equal(commitTime) {
		t.Errorf("CommittedDate = %v, want %v", result[0].CommittedDate.Time, commitTime)
	}
}

func TestConvertCommits_FallsBackToAuthors(t *testing.T) {
	commits := []ghCommit{
		{Authors: []ghCommitAuthor{{Login: "carol"}}, CommittedDate: time.Now()},
	}

	result := convertCommits(commits)
	if len(result) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(result))
	}
	if result[0].AuthorLogin != "carol" {
		t.Errorf("AuthorLogin = %q, want %q", result[0].AuthorLogin, "carol")
	}
}

func TestConvertCommits_Empty(t *testing.T) {
	result := convertCommits(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 commits for nil input, got %d", len(result))
	}
}

// ── Stale PR cleanup ────────────────────────────────────────────────────────

func TestGHCLIReviewQueueFetcher_CleansUpStalePRs(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		if args[0] == "search" {
			return []byte(rqSearchJSON), nil
		}
		return []byte(rqDetailJSON), nil
	}
	stalePR := persistence.PullRequest{Repo: "owner/old", Number: 55, Author: "bob"}
	keptPR := persistence.PullRequest{Repo: "owner/repo", Number: 10, Author: "bob"}
	store := api.NewStubReviewQueueStore()
	store.ListPullRequestsNotByAuthorFunc = func(author string) ([]persistence.PullRequest, error) {
		return []persistence.PullRequest{stalePR, keptPR}, nil
	}
	store.DeletePullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return stalePR, nil
	}
	pub := &api.StubPublisher{}

	f := NewGHCLIReviewQueueFetcher(runner, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch error = %v", err)
	}

	// Only the stale PR should be deleted
	if len(store.DeletedPRKeys) != 1 {
		t.Fatalf("expected 1 deleted PR, got %d", len(store.DeletedPRKeys))
	}
	if store.DeletedPRKeys[0].Repo != "owner/old" || store.DeletedPRKeys[0].Number != 55 {
		t.Errorf("deleted wrong PR: %+v", store.DeletedPRKeys[0])
	}

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

func TestGHCLIReviewQueueFetcher_NoCleanupOnSearchError(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
		return nil, errors.New("search failed")
	}
	store := api.NewStubReviewQueueStore()
	store.ListPullRequestsNotByAuthorFunc = func(author string) ([]persistence.PullRequest, error) {
		t.Error("ListPullRequestsNotByAuthor should not be called when search fails")
		return nil, nil
	}
	pub := &api.StubPublisher{}

	f := NewGHCLIReviewQueueFetcher(runner, store, pub, "alice")
	_ = f.Fetch(context.Background())

	if len(store.DeletedPRKeys) != 0 {
		t.Errorf("expected 0 deletions, got %d", len(store.DeletedPRKeys))
	}
}

func TestGHCLIReviewQueueFetcher_CleanupListError(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		if args[0] == "search" {
			return []byte("[]"), nil
		}
		return nil, fmt.Errorf("unexpected: %v", args)
	}
	store := api.NewStubReviewQueueStore()
	store.ListPullRequestsNotByAuthorFunc = func(author string) ([]persistence.PullRequest, error) {
		return nil, errors.New("list failed")
	}
	pub := &api.StubPublisher{}

	f := NewGHCLIReviewQueueFetcher(runner, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch error = %v", err)
	}

	if len(store.DeletedPRKeys) != 0 {
		t.Errorf("expected 0 deletions on list error, got %d", len(store.DeletedPRKeys))
	}
}

func TestGHCLIReviewQueueFetcher_CleanupDeleteError(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		if args[0] == "search" {
			return []byte("[]"), nil
		}
		return nil, fmt.Errorf("unexpected: %v", args)
	}
	stalePR := persistence.PullRequest{Repo: "owner/gone", Number: 33, Author: "bob"}
	store := api.NewStubReviewQueueStore()
	store.ListPullRequestsNotByAuthorFunc = func(author string) ([]persistence.PullRequest, error) {
		return []persistence.PullRequest{stalePR}, nil
	}
	store.DeletePullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return persistence.PullRequest{}, errors.New("delete failed")
	}
	pub := &api.StubPublisher{}

	f := NewGHCLIReviewQueueFetcher(runner, store, pub, "alice")
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch error = %v", err)
	}

	if len(store.DeletedPRKeys) != 1 {
		t.Fatalf("expected 1 delete attempt, got %d", len(store.DeletedPRKeys))
	}
	for _, e := range pub.Events {
		if e.Type == eventbus.PRRemoved {
			t.Error("should not emit PRRemoved when delete fails")
		}
	}
}

// ── Test fixtures ───────────────────────────────────────────────────────────

// Search result: only fields supported by gh search prs
const rqSearchJSON = `[
  {
    "id": "PR_rq1",
    "number": 10,
    "title": "feat: review me",
    "state": "OPEN",
    "isDraft": false,
    "url": "https://github.com/owner/repo/pull/10",
    "createdAt": "2024-02-01T00:00:00Z",
    "updatedAt": "2024-02-02T00:00:00Z",
    "author": {"login": "bob"},
    "repository": {"name": "repo", "nameWithOwner": "owner/repo"}
  }
]`

// Detail result from gh pr view --json
const rqDetailJSON = `{
  "statusCheckRollup": [],
  "reviews": [],
  "reviewRequests": [{"login": "alice"}],
  "commits": [
    {"authors": [{"login": "bob"}], "committedDate": "2024-02-01T12:00:00Z"}
  ]
}`
