package api

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
)

// StubCommandExecutor is a test double for CommandExecutor.
// It returns preconfigured responses for specific commands.
type StubCommandExecutor struct {
	// Responses maps "name arg1 arg2 ..." to the bytes to return.
	Responses map[string][]byte
	// Errors maps "name arg1 arg2 ..." to the error to return.
	Errors map[string]error
}

// NewStubCommandExecutor creates a StubCommandExecutor with empty maps.
func NewStubCommandExecutor() *StubCommandExecutor {
	return &StubCommandExecutor{
		Responses: make(map[string][]byte),
		Errors:    make(map[string]error),
	}
}

// Output returns the preconfigured response for the command or an error.
func (s *StubCommandExecutor) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	key := commandKey(name, args...)
	if err, ok := s.Errors[key]; ok {
		return nil, err
	}
	if resp, ok := s.Responses[key]; ok {
		return resp, nil
	}
	return nil, fmt.Errorf("stub: no response configured for %q", key)
}

func commandKey(name string, args ...string) string {
	parts := append([]string{name}, args...)
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += " "
		}
		result += p
	}
	return result
}

// ── GraphQL / PR stubs ────────────────────────────────────────────────────────

// StubGraphQLClient is a test double for GraphQLClient.
type StubGraphQLClient struct {
	QueryFunc func(ctx context.Context, q interface{}, variables map[string]interface{}) error
}

// Query delegates to QueryFunc.
func (s *StubGraphQLClient) Query(ctx context.Context, q interface{}, variables map[string]interface{}) error {
	return s.QueryFunc(ctx, q, variables)
}

// StubPRStore is a test double for PRStore.
// Callers configure behaviour via function fields; captured writes are
// recorded in UpsertedPRs, UpsertedCheckRuns, and UpsertedReviewers.
type StubPRStore struct {
	GetPullRequestFunc    func(repo string, number int) (persistence.PullRequest, error)
	UpsertPullRequestFunc func(pr persistence.PullRequest) error
	UpsertReviewerFunc    func(r persistence.Reviewer) error
	UpsertCheckRunFunc    func(cr persistence.CheckRun) error

	UpsertedPRs       []persistence.PullRequest
	UpsertedCheckRuns []persistence.CheckRun
	UpsertedReviewers []persistence.Reviewer
}

// NewStubPRStore returns a StubPRStore whose defaults succeed and report every
// PR as new (GetPullRequest returns sql.ErrNoRows).
func NewStubPRStore() *StubPRStore {
	return &StubPRStore{
		GetPullRequestFunc:    func(repo string, number int) (persistence.PullRequest, error) { return persistence.PullRequest{}, sql.ErrNoRows },
		UpsertPullRequestFunc: func(pr persistence.PullRequest) error { return nil },
		UpsertReviewerFunc:    func(r persistence.Reviewer) error { return nil },
		UpsertCheckRunFunc:    func(cr persistence.CheckRun) error { return nil },
	}
}

func (s *StubPRStore) GetPullRequest(repo string, number int) (persistence.PullRequest, error) {
	return s.GetPullRequestFunc(repo, number)
}

func (s *StubPRStore) UpsertPullRequest(pr persistence.PullRequest) error {
	s.UpsertedPRs = append(s.UpsertedPRs, pr)
	return s.UpsertPullRequestFunc(pr)
}

func (s *StubPRStore) UpsertCheckRun(cr persistence.CheckRun) error {
	s.UpsertedCheckRuns = append(s.UpsertedCheckRuns, cr)
	return s.UpsertCheckRunFunc(cr)
}

func (s *StubPRStore) UpsertReviewer(r persistence.Reviewer) error {
	s.UpsertedReviewers = append(s.UpsertedReviewers, r)
	return s.UpsertReviewerFunc(r)
}

// StubPublisher is a test double for Publisher that records all published events.
type StubPublisher struct {
	Events []eventbus.Event
}

// Publish records the event.
func (s *StubPublisher) Publish(e eventbus.Event) {
	s.Events = append(s.Events, e)
}

// StubReviewQueueStore is a test double for ReviewQueueStore.
// It embeds StubPRStore for the shared PR methods and adds InsertTimelineEvent.
type StubReviewQueueStore struct {
	*StubPRStore
	InsertTimelineEventFunc func(te persistence.TimelineEvent) error

	InsertedTimelineEvents []persistence.TimelineEvent
}

// NewStubReviewQueueStore returns a StubReviewQueueStore whose defaults succeed
// and report every PR as new (GetPullRequest returns sql.ErrNoRows).
func NewStubReviewQueueStore() *StubReviewQueueStore {
	return &StubReviewQueueStore{
		StubPRStore:             NewStubPRStore(),
		InsertTimelineEventFunc: func(te persistence.TimelineEvent) error { return nil },
	}
}

func (s *StubReviewQueueStore) InsertTimelineEvent(te persistence.TimelineEvent) error {
	s.InsertedTimelineEvents = append(s.InsertedTimelineEvents, te)
	return s.InsertTimelineEventFunc(te)
}
