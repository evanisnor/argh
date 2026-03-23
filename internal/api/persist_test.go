package api

import (
	"errors"
	"testing"
	"time"

	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
	"github.com/shurcooL/githubv4"
)

// ── PersistPR ────────────────────────────────────────────────────────────────

func TestPersistPR_NewPR_EmitsPRUpdated(t *testing.T) {
	store := NewStubPRStore()
	pub := &StubPublisher{}
	pr := persistence.PullRequest{
		ID: "PR_1", Repo: "o/r", Number: 1, Title: "t",
		Status: "open", CIState: "passing", Author: "a",
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		LastActivityAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		GlobalID: "PR_1",
	}
	runs := []CheckRunData{{Name: "ci", Status: "COMPLETED", Conclusion: "SUCCESS"}}
	reviews := []ReviewData{{Login: "bob", State: "APPROVED"}}

	if err := PersistPR(store, pub, pr, runs, reviews); err != nil {
		t.Fatalf("PersistPR error = %v", err)
	}

	if len(store.UpsertedPRs) != 1 {
		t.Fatalf("expected 1 upserted PR, got %d", len(store.UpsertedPRs))
	}
	if len(store.UpsertedCheckRuns) != 1 {
		t.Errorf("expected 1 check run, got %d", len(store.UpsertedCheckRuns))
	}
	if len(store.UpsertedReviewers) != 1 {
		t.Errorf("expected 1 reviewer, got %d", len(store.UpsertedReviewers))
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

func TestPersistPR_CIChanged_EmitsCIChanged(t *testing.T) {
	existing := persistence.PullRequest{
		ID: "PR_1", Repo: "o/r", Number: 1, Title: "t",
		Status: "open", CIState: "running", Author: "a",
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		LastActivityAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		GlobalID: "PR_1",
	}
	store := NewStubPRStore()
	store.GetPullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return existing, nil
	}
	pub := &StubPublisher{}

	pr := existing
	pr.CIState = "passing"

	if err := PersistPR(store, pub, pr, nil, nil); err != nil {
		t.Fatalf("PersistPR error = %v", err)
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
}

func TestPersistPR_OtherChange_EmitsPRUpdated(t *testing.T) {
	existing := persistence.PullRequest{
		ID: "PR_1", Repo: "o/r", Number: 1, Title: "old",
		Status: "open", CIState: "none", Author: "a",
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		LastActivityAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		GlobalID: "PR_1",
	}
	store := NewStubPRStore()
	store.GetPullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return existing, nil
	}
	pub := &StubPublisher{}

	pr := existing
	pr.Title = "new"

	if err := PersistPR(store, pub, pr, nil, nil); err != nil {
		t.Fatalf("PersistPR error = %v", err)
	}

	if len(pub.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(pub.Events))
	}
	if pub.Events[0].Type != eventbus.PRUpdated {
		t.Errorf("event type = %v, want %v", pub.Events[0].Type, eventbus.PRUpdated)
	}
}

func TestPersistPR_NoChanges_NoEvents(t *testing.T) {
	ts := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	existing := persistence.PullRequest{
		ID: "PR_1", Repo: "o/r", Number: 1, Title: "t",
		Status: "open", CIState: "none", Author: "a",
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: ts, LastActivityAt: ts,
		GlobalID: "PR_1",
	}
	store := NewStubPRStore()
	store.GetPullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return existing, nil
	}
	pub := &StubPublisher{}

	if err := PersistPR(store, pub, existing, nil, nil); err != nil {
		t.Fatalf("PersistPR error = %v", err)
	}

	if len(pub.Events) != 0 {
		t.Errorf("expected no events, got %d", len(pub.Events))
	}
}

func TestPersistPR_GetPRUnexpectedError(t *testing.T) {
	dbErr := errors.New("db broken")
	store := NewStubPRStore()
	store.GetPullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return persistence.PullRequest{}, dbErr
	}
	pub := &StubPublisher{}

	err := PersistPR(store, pub, persistence.PullRequest{Repo: "o/r", Number: 1}, nil, nil)
	if !errors.Is(err, dbErr) {
		t.Errorf("error = %v, want to wrap %v", err, dbErr)
	}
}

func TestPersistPR_UpsertPRError(t *testing.T) {
	upsertErr := errors.New("upsert failed")
	store := NewStubPRStore()
	store.UpsertPullRequestFunc = func(pr persistence.PullRequest) error { return upsertErr }
	pub := &StubPublisher{}

	err := PersistPR(store, pub, persistence.PullRequest{Repo: "o/r", Number: 1}, nil, nil)
	if !errors.Is(err, upsertErr) {
		t.Errorf("error = %v, want to wrap %v", err, upsertErr)
	}
}

func TestPersistPR_UpsertCheckRunError(t *testing.T) {
	crErr := errors.New("check run upsert failed")
	store := NewStubPRStore()
	store.UpsertCheckRunFunc = func(cr persistence.CheckRun) error { return crErr }
	pub := &StubPublisher{}

	runs := []CheckRunData{{Name: "ci", Status: "IN_PROGRESS"}}
	err := PersistPR(store, pub, persistence.PullRequest{Repo: "o/r", Number: 1}, runs, nil)
	if !errors.Is(err, crErr) {
		t.Errorf("error = %v, want to wrap %v", err, crErr)
	}
}

func TestPersistPR_UpsertReviewerError(t *testing.T) {
	revErr := errors.New("reviewer upsert failed")
	store := NewStubPRStore()
	store.UpsertReviewerFunc = func(r persistence.Reviewer) error { return revErr }
	pub := &StubPublisher{}

	reviews := []ReviewData{{Login: "bob", State: "APPROVED"}}
	err := PersistPR(store, pub, persistence.PullRequest{Repo: "o/r", Number: 1}, nil, reviews)
	if !errors.Is(err, revErr) {
		t.Errorf("error = %v, want to wrap %v", err, revErr)
	}
}

// ── PersistRQPR ──────────────────────────────────────────────────────────────

func TestPersistRQPR_NewPR_EmitsPRUpdated(t *testing.T) {
	store := NewStubReviewQueueStore()
	pub := &StubPublisher{}
	pr := persistence.PullRequest{
		ID: "PR_1", Repo: "o/r", Number: 1, Title: "t",
		Status: "open", CIState: "none", Author: "a",
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		LastActivityAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		GlobalID: "PR_1",
	}

	if err := PersistRQPR(store, pub, pr, nil, nil, nil); err != nil {
		t.Fatalf("PersistRQPR error = %v", err)
	}

	if len(pub.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(pub.Events))
	}
	if pub.Events[0].Type != eventbus.PRUpdated {
		t.Errorf("event type = %v, want %v", pub.Events[0].Type, eventbus.PRUpdated)
	}
}

func TestPersistRQPR_CIChanged_EmitsCIChanged(t *testing.T) {
	existing := persistence.PullRequest{
		ID: "PR_1", Repo: "o/r", Number: 1, Title: "t",
		Status: "open", CIState: "running", Author: "a",
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		LastActivityAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		GlobalID: "PR_1",
	}
	store := NewStubReviewQueueStore()
	store.GetPullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return existing, nil
	}
	pub := &StubPublisher{}

	pr := existing
	pr.CIState = "passing"

	if err := PersistRQPR(store, pub, pr, nil, nil, nil); err != nil {
		t.Fatalf("PersistRQPR error = %v", err)
	}

	if len(pub.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(pub.Events))
	}
	if pub.Events[0].Type != eventbus.CIChanged {
		t.Errorf("event type = %v, want %v", pub.Events[0].Type, eventbus.CIChanged)
	}
}

func TestPersistRQPR_NoChanges_NoEvents(t *testing.T) {
	ts := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	existing := persistence.PullRequest{
		ID: "PR_1", Repo: "o/r", Number: 1, Title: "t",
		Status: "open", CIState: "none", Author: "a",
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: ts, LastActivityAt: ts,
		GlobalID: "PR_1",
	}
	store := NewStubReviewQueueStore()
	store.GetPullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return existing, nil
	}
	pub := &StubPublisher{}

	if err := PersistRQPR(store, pub, existing, nil, nil, nil); err != nil {
		t.Fatalf("PersistRQPR error = %v", err)
	}

	if len(pub.Events) != 0 {
		t.Errorf("expected no events, got %d", len(pub.Events))
	}
}

func TestPersistRQPR_CommitsStoredAsTimelineEvents(t *testing.T) {
	store := NewStubReviewQueueStore()
	pub := &StubPublisher{}
	pr := persistence.PullRequest{
		ID: "PR_1", Repo: "o/r", Number: 1,
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		LastActivityAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}

	commitTime := time.Date(2024, 2, 1, 12, 0, 0, 0, time.UTC)
	commits := []CommitData{{
		AuthorLogin:   "bob",
		CommittedDate: githubv4.DateTime{Time: commitTime},
	}}

	if err := PersistRQPR(store, pub, pr, nil, nil, commits); err != nil {
		t.Fatalf("PersistRQPR error = %v", err)
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
}

func TestPersistRQPR_GetPRUnexpectedError(t *testing.T) {
	dbErr := errors.New("db broken")
	store := NewStubReviewQueueStore()
	store.GetPullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return persistence.PullRequest{}, dbErr
	}
	pub := &StubPublisher{}

	err := PersistRQPR(store, pub, persistence.PullRequest{Repo: "o/r", Number: 1}, nil, nil, nil)
	if !errors.Is(err, dbErr) {
		t.Errorf("error = %v, want to wrap %v", err, dbErr)
	}
}

func TestPersistRQPR_UpsertPRError(t *testing.T) {
	upsertErr := errors.New("upsert failed")
	store := NewStubReviewQueueStore()
	store.UpsertPullRequestFunc = func(pr persistence.PullRequest) error { return upsertErr }
	pub := &StubPublisher{}

	err := PersistRQPR(store, pub, persistence.PullRequest{Repo: "o/r", Number: 1}, nil, nil, nil)
	if !errors.Is(err, upsertErr) {
		t.Errorf("error = %v, want to wrap %v", err, upsertErr)
	}
}

func TestPersistRQPR_UpsertCheckRunError(t *testing.T) {
	crErr := errors.New("check run upsert failed")
	store := NewStubReviewQueueStore()
	store.UpsertCheckRunFunc = func(cr persistence.CheckRun) error { return crErr }
	pub := &StubPublisher{}

	runs := []CheckRunData{{Name: "ci", Status: "IN_PROGRESS"}}
	err := PersistRQPR(store, pub, persistence.PullRequest{Repo: "o/r", Number: 1}, runs, nil, nil)
	if !errors.Is(err, crErr) {
		t.Errorf("error = %v, want to wrap %v", err, crErr)
	}
}

func TestPersistRQPR_UpsertReviewerError(t *testing.T) {
	revErr := errors.New("reviewer upsert failed")
	store := NewStubReviewQueueStore()
	store.UpsertReviewerFunc = func(r persistence.Reviewer) error { return revErr }
	pub := &StubPublisher{}

	reviews := []ReviewData{{Login: "bob", State: "APPROVED"}}
	err := PersistRQPR(store, pub, persistence.PullRequest{Repo: "o/r", Number: 1}, nil, reviews, nil)
	if !errors.Is(err, revErr) {
		t.Errorf("error = %v, want to wrap %v", err, revErr)
	}
}

func TestPersistRQPR_InsertTimelineEventError(t *testing.T) {
	teErr := errors.New("timeline event insert failed")
	store := NewStubReviewQueueStore()
	store.InsertTimelineEventFunc = func(te persistence.TimelineEvent) error { return teErr }
	pub := &StubPublisher{}

	commits := []CommitData{{
		AuthorLogin:   "bob",
		CommittedDate: githubv4.DateTime{Time: time.Now()},
	}}
	err := PersistRQPR(store, pub, persistence.PullRequest{Repo: "o/r", Number: 1}, nil, nil, commits)
	if !errors.Is(err, teErr) {
		t.Errorf("error = %v, want to wrap %v", err, teErr)
	}
}

func TestPersistRQPR_OtherChange_EmitsPRUpdated(t *testing.T) {
	existing := persistence.PullRequest{
		ID: "PR_1", Repo: "o/r", Number: 1, Title: "old",
		Status: "open", CIState: "none", Author: "a",
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		LastActivityAt: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		GlobalID: "PR_1",
	}
	store := NewStubReviewQueueStore()
	store.GetPullRequestFunc = func(repo string, number int) (persistence.PullRequest, error) {
		return existing, nil
	}
	pub := &StubPublisher{}

	pr := existing
	pr.Title = "new"

	if err := PersistRQPR(store, pub, pr, nil, nil, nil); err != nil {
		t.Fatalf("PersistRQPR error = %v", err)
	}

	if len(pub.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(pub.Events))
	}
	if pub.Events[0].Type != eventbus.PRUpdated {
		t.Errorf("event type = %v, want %v", pub.Events[0].Type, eventbus.PRUpdated)
	}
}

// ── Stub defaults ────────────────────────────────────────────────────────────

func TestStubPRStore_DefaultsSucceed(t *testing.T) {
	s := NewStubPRStore()

	prs, err := s.ListPullRequestsByAuthor("anyone")
	if err != nil || len(prs) != 0 {
		t.Errorf("ListPullRequestsByAuthor default: prs=%d, err=%v", len(prs), err)
	}

	_, err = s.DeletePullRequest("o/r", 1)
	if err != nil {
		t.Errorf("DeletePullRequest default: err=%v", err)
	}
}

func TestStubReviewQueueStore_DefaultsSucceed(t *testing.T) {
	s := NewStubReviewQueueStore()

	prs, err := s.ListPullRequestsNotByAuthor("anyone")
	if err != nil || len(prs) != 0 {
		t.Errorf("ListPullRequestsNotByAuthor default: prs=%d, err=%v", len(prs), err)
	}

	_, err = s.DeletePullRequest("o/r", 1)
	if err != nil {
		t.Errorf("DeletePullRequest default: err=%v", err)
	}
}
