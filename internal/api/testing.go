package api

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
	"github.com/google/go-github/v69/github"
	"github.com/shurcooL/githubv4"
)

// StubTokenVerifier is a test double for TokenVerifier.
type StubTokenVerifier struct {
	Login string
	Err   error
}

// Verify returns the preconfigured login and error.
func (s *StubTokenVerifier) Verify(_ context.Context, _ string) (string, error) {
	return s.Login, s.Err
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

// ── Mutation stubs ─────────────────────────────────────────────────────────────

// StubPullRequestsService is a test double for PullRequestsService.
type StubPullRequestsService struct {
	CreateReviewFunc     func(ctx context.Context, owner, repo string, number int, review *github.PullRequestReviewRequest) (*github.PullRequestReview, *github.Response, error)
	RequestReviewersFunc func(ctx context.Context, owner, repo string, number int, reviewers github.ReviewersRequest) (*github.PullRequest, *github.Response, error)
	MergeFunc            func(ctx context.Context, owner, repo string, number int, commitMessage string, options *github.PullRequestOptions) (*github.PullRequestMergeResult, *github.Response, error)
}

func NewStubPullRequestsService() *StubPullRequestsService {
	return &StubPullRequestsService{
		CreateReviewFunc:     func(_ context.Context, _, _ string, _ int, _ *github.PullRequestReviewRequest) (*github.PullRequestReview, *github.Response, error) { return nil, nil, nil },
		RequestReviewersFunc: func(_ context.Context, _, _ string, _ int, _ github.ReviewersRequest) (*github.PullRequest, *github.Response, error) { return nil, nil, nil },
		MergeFunc:            func(_ context.Context, _, _ string, _ int, _ string, _ *github.PullRequestOptions) (*github.PullRequestMergeResult, *github.Response, error) { return nil, nil, nil },
	}
}

func (s *StubPullRequestsService) CreateReview(ctx context.Context, owner, repo string, number int, review *github.PullRequestReviewRequest) (*github.PullRequestReview, *github.Response, error) {
	return s.CreateReviewFunc(ctx, owner, repo, number, review)
}

func (s *StubPullRequestsService) RequestReviewers(ctx context.Context, owner, repo string, number int, reviewers github.ReviewersRequest) (*github.PullRequest, *github.Response, error) {
	return s.RequestReviewersFunc(ctx, owner, repo, number, reviewers)
}

func (s *StubPullRequestsService) Merge(ctx context.Context, owner, repo string, number int, commitMessage string, options *github.PullRequestOptions) (*github.PullRequestMergeResult, *github.Response, error) {
	return s.MergeFunc(ctx, owner, repo, number, commitMessage, options)
}

// StubIssuesService is a test double for IssuesService.
type StubIssuesService struct {
	CreateCommentFunc      func(ctx context.Context, owner, repo string, number int, comment *github.IssueComment) (*github.IssueComment, *github.Response, error)
	AddLabelsToIssueFunc   func(ctx context.Context, owner, repo string, number int, labels []string) ([]*github.Label, *github.Response, error)
	RemoveLabelForIssueFunc func(ctx context.Context, owner, repo string, number int, label string) (*github.Response, error)
	EditFunc               func(ctx context.Context, owner, repo string, number int, issue *github.IssueRequest) (*github.Issue, *github.Response, error)
}

func NewStubIssuesService() *StubIssuesService {
	return &StubIssuesService{
		CreateCommentFunc:      func(_ context.Context, _, _ string, _ int, _ *github.IssueComment) (*github.IssueComment, *github.Response, error) { return nil, nil, nil },
		AddLabelsToIssueFunc:   func(_ context.Context, _, _ string, _ int, _ []string) ([]*github.Label, *github.Response, error) { return nil, nil, nil },
		RemoveLabelForIssueFunc: func(_ context.Context, _, _ string, _ int, _ string) (*github.Response, error) { return nil, nil },
		EditFunc:               func(_ context.Context, _, _ string, _ int, _ *github.IssueRequest) (*github.Issue, *github.Response, error) { return nil, nil, nil },
	}
}

func (s *StubIssuesService) CreateComment(ctx context.Context, owner, repo string, number int, comment *github.IssueComment) (*github.IssueComment, *github.Response, error) {
	return s.CreateCommentFunc(ctx, owner, repo, number, comment)
}

func (s *StubIssuesService) AddLabelsToIssue(ctx context.Context, owner, repo string, number int, labels []string) ([]*github.Label, *github.Response, error) {
	return s.AddLabelsToIssueFunc(ctx, owner, repo, number, labels)
}

func (s *StubIssuesService) RemoveLabelForIssue(ctx context.Context, owner, repo string, number int, label string) (*github.Response, error) {
	return s.RemoveLabelForIssueFunc(ctx, owner, repo, number, label)
}

func (s *StubIssuesService) Edit(ctx context.Context, owner, repo string, number int, issue *github.IssueRequest) (*github.Issue, *github.Response, error) {
	return s.EditFunc(ctx, owner, repo, number, issue)
}

// StubGraphQLMutator is a test double for GraphQLMutator.
type StubGraphQLMutator struct {
	MutateFunc func(ctx context.Context, m interface{}, input githubv4.Input, variables map[string]interface{}) error
}

func NewStubGraphQLMutator() *StubGraphQLMutator {
	return &StubGraphQLMutator{
		MutateFunc: func(_ context.Context, _ interface{}, _ githubv4.Input, _ map[string]interface{}) error { return nil },
	}
}

func (s *StubGraphQLMutator) Mutate(ctx context.Context, m interface{}, input githubv4.Input, variables map[string]interface{}) error {
	return s.MutateFunc(ctx, m, input, variables)
}

// ── ETag stubs ────────────────────────────────────────────────────────────────

// StubETagStore is a test double for ETagStore.
type StubETagStore struct {
	GetETagFunc  func(url string) (persistence.ETag, error)
	UpsertETagFunc func(e persistence.ETag) error

	UpsertedETags []persistence.ETag
}

// NewStubETagStore returns a StubETagStore whose default GetETag returns
// sql.ErrNoRows (no cached entry) and whose default UpsertETag succeeds.
func NewStubETagStore() *StubETagStore {
	return &StubETagStore{
		GetETagFunc:  func(_ string) (persistence.ETag, error) { return persistence.ETag{}, sql.ErrNoRows },
		UpsertETagFunc: func(_ persistence.ETag) error { return nil },
	}
}

func (s *StubETagStore) GetETag(url string) (persistence.ETag, error) {
	return s.GetETagFunc(url)
}

func (s *StubETagStore) UpsertETag(e persistence.ETag) error {
	s.UpsertedETags = append(s.UpsertedETags, e)
	return s.UpsertETagFunc(e)
}

// StubAuditLogger is a test double for AuditLogger that records all log calls.
type StubAuditLogger struct {
	Entries []AuditEntry
	LogFunc func(ctx context.Context, action, owner, repo string, number int, details string) error
}

// AuditEntry records a single audit log call for test assertions.
type AuditEntry struct {
	Action  string
	Owner   string
	Repo    string
	Number  int
	Details string
}

func NewStubAuditLogger() *StubAuditLogger {
	return &StubAuditLogger{
		LogFunc: func(_ context.Context, _, _, _ string, _ int, _ string) error { return nil },
	}
}

func (s *StubAuditLogger) Log(ctx context.Context, action, owner, repo string, number int, details string) error {
	s.Entries = append(s.Entries, AuditEntry{Action: action, Owner: owner, Repo: repo, Number: number, Details: details})
	return s.LogFunc(ctx, action, owner, repo, number, details)
}

// ── Poller stubs ──────────────────────────────────────────────────────────────

// FakeTicker is a controllable Ticker for use in tests. It never fires
// automatically; call Tick() to deliver a single tick to the channel.
type FakeTicker struct {
	ch      chan time.Time
	mu      sync.Mutex
	dur     time.Duration
	ResetCh chan time.Duration // receives the duration passed to each Reset call
}

// NewFakeTicker returns a FakeTicker initialised with duration d.
func NewFakeTicker(d time.Duration) *FakeTicker {
	return &FakeTicker{
		ch:      make(chan time.Time, 1),
		dur:     d,
		ResetCh: make(chan time.Duration, 10),
	}
}

func (f *FakeTicker) C() <-chan time.Time { return f.ch }
func (f *FakeTicker) Stop()               {}

// Reset records the new duration (accessible via Duration()) and sends it to ResetCh.
func (f *FakeTicker) Reset(d time.Duration) {
	f.mu.Lock()
	f.dur = d
	f.mu.Unlock()
	select {
	case f.ResetCh <- d:
	default:
	}
}

// Duration returns the most recently set duration (thread-safe).
func (f *FakeTicker) Duration() time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dur
}

// Tick delivers one tick to the channel (non-blocking; drops if already full).
func (f *FakeTicker) Tick() {
	select {
	case f.ch <- time.Now():
	default:
	}
}

// StubRateLimitReader is a thread-safe test double for RateLimitReader.
type StubRateLimitReader struct {
	mu    sync.RWMutex
	state RateLimitState
}

// NewStubRateLimitReader returns a StubRateLimitReader with the given remaining quota.
func NewStubRateLimitReader(remaining int) *StubRateLimitReader {
	return &StubRateLimitReader{state: RateLimitState{Remaining: remaining, Limit: githubRateLimit}}
}

// CurrentState returns the current state (thread-safe).
func (s *StubRateLimitReader) CurrentState() RateLimitState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// SetRemaining updates the Remaining field (thread-safe).
func (s *StubRateLimitReader) SetRemaining(remaining int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Remaining = remaining
}

// ── Clock / Sleep stubs ───────────────────────────────────────────────────────

// FakeClock is a controllable Clock for tests. Call Set to advance time.
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewFakeClock returns a FakeClock initialised to t.
func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{now: t}
}

// Now returns the current fake time (thread-safe).
func (f *FakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Set updates the fake clock to t (thread-safe).
func (f *FakeClock) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t
}

// StubSleepScheduleChecker is a thread-safe test double for SleepScheduleChecker.
type StubSleepScheduleChecker struct {
	mu            sync.RWMutex
	inSleepWindow bool
	sleepInterval time.Duration
	wakeCalled    bool
}

// NewStubSleepScheduleChecker returns a StubSleepScheduleChecker that is
// never in a sleep window and has a 5-minute sleep interval by default.
func NewStubSleepScheduleChecker() *StubSleepScheduleChecker {
	return &StubSleepScheduleChecker{sleepInterval: 5 * time.Minute}
}

// IsInSleepWindow returns the configured in-sleep-window state (thread-safe).
func (s *StubSleepScheduleChecker) IsInSleepWindow() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.inSleepWindow
}

// SleepInterval returns the configured sleep interval (thread-safe).
func (s *StubSleepScheduleChecker) SleepInterval() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sleepInterval
}

// Wake records that Wake was called (thread-safe).
func (s *StubSleepScheduleChecker) Wake() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wakeCalled = true
}

// SetInSleepWindow updates the in-sleep-window state (thread-safe).
func (s *StubSleepScheduleChecker) SetInSleepWindow(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inSleepWindow = v
}

// SetSleepInterval updates the sleep interval (thread-safe).
func (s *StubSleepScheduleChecker) SetSleepInterval(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sleepInterval = d
}

// WasWakeCalled reports whether Wake was called (thread-safe).
func (s *StubSleepScheduleChecker) WasWakeCalled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.wakeCalled
}

// StubFetcher is a test double for Fetcher.
// FetchFunc is called by Fetch; it is set before the poller starts so there
// is no write race on the field itself.
type StubFetcher struct {
	FetchFunc func(ctx context.Context) error
}

// NewStubFetcher returns a StubFetcher that succeeds by default.
func NewStubFetcher() *StubFetcher {
	return &StubFetcher{
		FetchFunc: func(_ context.Context) error { return nil },
	}
}

// Fetch delegates to FetchFunc.
func (s *StubFetcher) Fetch(ctx context.Context) error {
	return s.FetchFunc(ctx)
}

// ── Device Flow stubs ─────────────────────────────────────────────────────────

// StubDeviceFlowClient is a test double for DeviceFlowClient.
type StubDeviceFlowClient struct {
	RequestCodeFunc func(ctx context.Context, clientID string, scopes []string) (*DeviceCodeResponse, error)
	PollTokenFunc   func(ctx context.Context, clientID string, deviceCode string, interval time.Duration) (*TokenResponse, error)
}

func (s *StubDeviceFlowClient) RequestCode(ctx context.Context, clientID string, scopes []string) (*DeviceCodeResponse, error) {
	return s.RequestCodeFunc(ctx, clientID, scopes)
}

func (s *StubDeviceFlowClient) PollToken(ctx context.Context, clientID string, deviceCode string, interval time.Duration) (*TokenResponse, error) {
	return s.PollTokenFunc(ctx, clientID, deviceCode, interval)
}
