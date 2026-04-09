package watches

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
)

// ── fakePRReader ──────────────────────────────────────────────────────────────

type fakePRReader struct {
	reviewers    map[string][]persistence.Reviewer  // keyed by prID
	threads      map[string][]persistence.ReviewThread
	reviewersErr error
	threadsErr   error
}

func (f *fakePRReader) ListReviewers(prID string) ([]persistence.Reviewer, error) {
	if f.reviewersErr != nil {
		return nil, f.reviewersErr
	}
	return f.reviewers[prID], nil
}

func (f *fakePRReader) ListReviewThreads(prID string) ([]persistence.ReviewThread, error) {
	if f.threadsErr != nil {
		return nil, f.threadsErr
	}
	return f.threads[prID], nil
}

// ── fakeActionExecutor ────────────────────────────────────────────────────────

type fakeActionExecutor struct {
	mergeErr           error
	markReadyErr       error
	requestReviewErr   error
	postCommentErr     error
	addLabelErr        error
	mergeCalled        bool
	markReadyCalled    bool
	requestCalled      bool
	commentCalled      bool
	labelCalled        bool
	lastMergeMethod    string
	lastRequestedUsers []string
	lastCommentBody    string
	lastLabelName      string
}

func (f *fakeActionExecutor) MergePR(_ context.Context, _ string, _ int, method string) error {
	f.mergeCalled = true
	f.lastMergeMethod = method
	return f.mergeErr
}

func (f *fakeActionExecutor) MarkReadyForReview(_ context.Context, _ string, _ int, _ string) error {
	f.markReadyCalled = true
	return f.markReadyErr
}

func (f *fakeActionExecutor) RequestReview(_ context.Context, _ string, _ int, users []string) error {
	f.requestCalled = true
	f.lastRequestedUsers = users
	return f.requestReviewErr
}

func (f *fakeActionExecutor) PostComment(_ context.Context, _ string, _ int, body string) error {
	f.commentCalled = true
	f.lastCommentBody = body
	return f.postCommentErr
}

func (f *fakeActionExecutor) AddLabel(_ context.Context, _ string, _ int, label string) error {
	f.labelCalled = true
	f.lastLabelName = label
	return f.addLabelErr
}

// ── fakeNotifier ──────────────────────────────────────────────────────────────

type fakeNotifier struct {
	err          error
	notifyCalled bool
	lastTitle    string
	lastBody     string
}

func (f *fakeNotifier) Notify(title, body string) error {
	f.notifyCalled = true
	f.lastTitle = title
	f.lastBody = body
	return f.err
}

// ── fakeEngineEventBus ────────────────────────────────────────────────────────

type fakeEngineEventBus struct {
	mu        sync.Mutex
	handlers  []func(eventbus.Event)
	published []eventbus.Event
}

func (b *fakeEngineEventBus) Subscribe(handler func(eventbus.Event)) func() {
	b.mu.Lock()
	idx := len(b.handlers)
	b.handlers = append(b.handlers, handler)
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		if idx < len(b.handlers) {
			b.handlers = append(b.handlers[:idx], b.handlers[idx+1:]...)
		}
		b.mu.Unlock()
	}
}

func (b *fakeEngineEventBus) Publish(e eventbus.Event) {
	b.mu.Lock()
	b.published = append(b.published, e)
	b.mu.Unlock()
}

// send delivers an event to all subscribed handlers (simulates the real bus).
func (b *fakeEngineEventBus) send(e eventbus.Event) {
	b.mu.Lock()
	handlers := make([]func(eventbus.Event), len(b.handlers))
	copy(handlers, b.handlers)
	b.mu.Unlock()
	for _, h := range handlers {
		h(e)
	}
}

func (b *fakeEngineEventBus) publishedCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.published)
}

func (b *fakeEngineEventBus) lastPublished() eventbus.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.published[len(b.published)-1]
}

// ── fakeEngineAuditLogger ─────────────────────────────────────────────────────

type fakeEngineAuditLogger struct {
	entries []engineAuditEntry
	err     error
}

type engineAuditEntry struct {
	action  string
	owner   string
	repo    string
	number  int
	details string
}

func (f *fakeEngineAuditLogger) Log(_ context.Context, action, owner, repo string, number int, details string) error {
	f.entries = append(f.entries, engineAuditEntry{action: action, owner: owner, repo: repo, number: number, details: details})
	return f.err
}

// ── test helpers ──────────────────────────────────────────────────────────────

// fixedTime, fixedClock are defined in manager_test.go (same package).

func newTestEngine(
	store *fakeWatchStore,
	prReader *fakePRReader,
	executor *fakeActionExecutor,
	notifier *fakeNotifier,
	bus *fakeEngineEventBus,
	audit *fakeEngineAuditLogger,
) *Engine {
	return NewEngine(store, prReader, executor, notifier, bus, audit, fixedClock)
}

func newTestEngineWithClock(
	store *fakeWatchStore,
	prReader *fakePRReader,
	executor *fakeActionExecutor,
	notifier *fakeNotifier,
	bus *fakeEngineEventBus,
	audit *fakeEngineAuditLogger,
	clock func() time.Time,
) *Engine {
	return NewEngine(store, prReader, executor, notifier, bus, audit, clock)
}

func testPR() persistence.PullRequest {
	return persistence.PullRequest{
		ID:             "pr-id-1",
		Repo:           "owner/repo",
		Number:         42,
		Title:          "Fix bug",
		Status:         "open",
		CIState:        "passing",
		URL:            "https://github.com/owner/repo/pull/42",
		GlobalID:       "PR_gid_001",
		LastActivityAt: fixedTime.Add(-2 * time.Hour),
	}
}

func testWatch() persistence.Watch {
	return persistence.Watch{
		ID:          "w1",
		PRURL:       "https://github.com/owner/repo/pull/42",
		PRNumber:    42,
		Repo:        "owner/repo",
		TriggerExpr: "on:ci-pass",
		ActionExpr:  "merge",
		Status:      "waiting",
		CreatedAt:   fixedTime,
	}
}

func emptyPRReader() *fakePRReader {
	return &fakePRReader{
		reviewers: map[string][]persistence.Reviewer{},
		threads:   map[string][]persistence.ReviewThread{},
	}
}

// engineFor creates an Engine with one watch and an empty PR reader.
func engineFor(t *testing.T, w persistence.Watch, executor *fakeActionExecutor, notifier *fakeNotifier) (*Engine, *fakeWatchStore, *fakeEngineEventBus, *fakeEngineAuditLogger) {
	t.Helper()
	if executor == nil {
		executor = &fakeActionExecutor{}
	}
	if notifier == nil {
		notifier = &fakeNotifier{}
	}
	store := &fakeWatchStore{watches: []persistence.Watch{w}}
	bus := &fakeEngineEventBus{}
	audit := &fakeEngineAuditLogger{}
	eng := newTestEngine(store, emptyPRReader(), executor, notifier, bus, audit)
	return eng, store, bus, audit
}

// ── Run: goroutine lifecycle ──────────────────────────────────────────────────

func TestEngine_Run_ExitsOnContextCancel(t *testing.T) {
	bus := &fakeEngineEventBus{}
	eng := newTestEngine(&fakeWatchStore{}, emptyPRReader(), &fakeActionExecutor{}, &fakeNotifier{}, bus, &fakeEngineAuditLogger{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		eng.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
		// goroutine exited cleanly
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after context cancellation")
	}
}

func TestEngine_Run_UnsubscribesOnExit(t *testing.T) {
	bus := &fakeEngineEventBus{}
	eng := newTestEngine(&fakeWatchStore{}, emptyPRReader(), &fakeActionExecutor{}, &fakeNotifier{}, bus, &fakeEngineAuditLogger{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		eng.Run(ctx)
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()
	<-done

	// After Run exits, sending events should not invoke any handler.
	before := bus.publishedCount()
	bus.send(eventbus.Event{Type: eventbus.PRUpdated, After: testPR()})
	after := bus.publishedCount()
	if after != before {
		t.Error("handler should be unsubscribed after Run exits")
	}
}

// ── Run: event routing ────────────────────────────────────────────────────────

func TestEngine_Run_PRUpdatedEvent_MatchingTrigger(t *testing.T) {
	pr := testPR() // CIState=passing
	w := testWatch() // on:ci-pass → merge

	store := &fakeWatchStore{watches: []persistence.Watch{w}}
	executor := &fakeActionExecutor{}
	bus := &fakeEngineEventBus{}
	audit := &fakeEngineAuditLogger{}

	eng := newTestEngine(store, emptyPRReader(), executor, &fakeNotifier{}, bus, audit)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eng.Run(ctx)
	time.Sleep(10 * time.Millisecond)

	bus.send(eventbus.Event{Type: eventbus.PRUpdated, After: pr})
	time.Sleep(20 * time.Millisecond)

	if !executor.mergeCalled {
		t.Error("MergePR should have been called")
	}
	if store.lastStatus != "fired" {
		t.Errorf("watch status = %q, want %q", store.lastStatus, "fired")
	}
	if bus.publishedCount() != 1 {
		t.Errorf("expected 1 WATCH_FIRED event, got %d", bus.publishedCount())
	}
	if bus.lastPublished().Type != eventbus.WatchFired {
		t.Errorf("event type = %q, want %q", bus.lastPublished().Type, eventbus.WatchFired)
	}
	if len(audit.entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(audit.entries))
	}
	if audit.entries[0].action != "watch-fired" {
		t.Errorf("audit action = %q, want %q", audit.entries[0].action, "watch-fired")
	}
}

func TestEngine_Run_CIChangedEvent_MatchingTrigger(t *testing.T) {
	pr := testPR()
	w := testWatch()

	store := &fakeWatchStore{watches: []persistence.Watch{w}}
	executor := &fakeActionExecutor{}
	bus := &fakeEngineEventBus{}

	eng := newTestEngine(store, emptyPRReader(), executor, &fakeNotifier{}, bus, &fakeEngineAuditLogger{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eng.Run(ctx)
	time.Sleep(10 * time.Millisecond)

	bus.send(eventbus.Event{Type: eventbus.CIChanged, After: pr})
	time.Sleep(20 * time.Millisecond)

	if !executor.mergeCalled {
		t.Error("MergePR should have been called on CIChanged event")
	}
}

func TestEngine_Run_ReviewChangedEvent_MatchingTrigger(t *testing.T) {
	pr := testPR()
	w := testWatch()
	w.TriggerExpr = "on:approved"
	w.ActionExpr = "notify"

	store := &fakeWatchStore{watches: []persistence.Watch{w}}
	prReader := &fakePRReader{
		reviewers: map[string][]persistence.Reviewer{
			"pr-id-1": {{PRID: "pr-id-1", Login: "alice", State: "APPROVED"}},
		},
		threads: map[string][]persistence.ReviewThread{},
	}
	notifier := &fakeNotifier{}
	bus := &fakeEngineEventBus{}

	eng := newTestEngine(store, prReader, &fakeActionExecutor{}, notifier, bus, &fakeEngineAuditLogger{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eng.Run(ctx)
	time.Sleep(10 * time.Millisecond)

	bus.send(eventbus.Event{Type: eventbus.ReviewChanged, After: pr})
	time.Sleep(20 * time.Millisecond)

	if !notifier.notifyCalled {
		t.Error("Notify should have been called on ReviewChanged event")
	}
}

func TestEngine_Run_OtherEventType_Ignored(t *testing.T) {
	store := &fakeWatchStore{watches: []persistence.Watch{testWatch()}}
	executor := &fakeActionExecutor{}
	bus := &fakeEngineEventBus{}

	eng := newTestEngine(store, emptyPRReader(), executor, &fakeNotifier{}, bus, &fakeEngineAuditLogger{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eng.Run(ctx)
	time.Sleep(10 * time.Millisecond)

	bus.send(eventbus.Event{Type: eventbus.WatchFired, After: testPR()})
	bus.send(eventbus.Event{Type: eventbus.RateLimitWarning})
	time.Sleep(20 * time.Millisecond)

	if executor.mergeCalled {
		t.Error("MergePR should not be called for non-PR-state events")
	}
}

func TestEngine_Run_NonPRAfterType_Ignored(t *testing.T) {
	store := &fakeWatchStore{watches: []persistence.Watch{testWatch()}}
	executor := &fakeActionExecutor{}
	bus := &fakeEngineEventBus{}

	eng := newTestEngine(store, emptyPRReader(), executor, &fakeNotifier{}, bus, &fakeEngineAuditLogger{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eng.Run(ctx)
	time.Sleep(10 * time.Millisecond)

	bus.send(eventbus.Event{Type: eventbus.PRUpdated, After: "not a PullRequest"})
	time.Sleep(20 * time.Millisecond)

	if executor.mergeCalled {
		t.Error("MergePR should not be called when After is not a PullRequest")
	}
}

// ── evaluateWatchesForPR: guard cases ─────────────────────────────────────────

func TestEngine_EvaluateWatches_ListWatchesError_NoAction(t *testing.T) {
	store := &fakeWatchStore{listErr: errors.New("db error")}
	executor := &fakeActionExecutor{}
	bus := &fakeEngineEventBus{}

	eng := newTestEngine(store, emptyPRReader(), executor, &fakeNotifier{}, bus, &fakeEngineAuditLogger{})
	eng.evaluateWatchesForPR(context.Background(), testPR())

	if executor.mergeCalled {
		t.Error("MergePR should not be called when ListWatches fails")
	}
	if bus.publishedCount() != 0 {
		t.Error("no events should be published when ListWatches fails")
	}
}

func TestEngine_EvaluateWatches_BuildSnapshotError_NoAction(t *testing.T) {
	store := &fakeWatchStore{watches: []persistence.Watch{testWatch()}}
	prReader := &fakePRReader{reviewersErr: errors.New("db error")}
	executor := &fakeActionExecutor{}

	eng := newTestEngine(store, prReader, executor, &fakeNotifier{}, &fakeEngineEventBus{}, &fakeEngineAuditLogger{})
	eng.evaluateWatchesForPR(context.Background(), testPR())

	if executor.mergeCalled {
		t.Error("MergePR should not be called when buildSnapshot fails")
	}
}

func TestEngine_EvaluateWatches_WrongPRURL_Skipped(t *testing.T) {
	w := testWatch()
	w.PRURL = "https://github.com/owner/repo/pull/99"
	store := &fakeWatchStore{watches: []persistence.Watch{w}}
	executor := &fakeActionExecutor{}

	eng := newTestEngine(store, emptyPRReader(), executor, &fakeNotifier{}, &fakeEngineEventBus{}, &fakeEngineAuditLogger{})
	eng.evaluateWatchesForPR(context.Background(), testPR())

	if executor.mergeCalled {
		t.Error("MergePR should not be called for a watch on a different PR URL")
	}
}

func TestEngine_EvaluateWatches_NonWaitingStatus_Skipped(t *testing.T) {
	for _, status := range []string{"fired", "failed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			w := testWatch()
			w.Status = status
			store := &fakeWatchStore{watches: []persistence.Watch{w}}
			executor := &fakeActionExecutor{}

			eng := newTestEngine(store, emptyPRReader(), executor, &fakeNotifier{}, &fakeEngineEventBus{}, &fakeEngineAuditLogger{})
			eng.evaluateWatchesForPR(context.Background(), testPR())

			if executor.mergeCalled {
				t.Errorf("MergePR should not be called for watch with status=%q", status)
			}
		})
	}
}

func TestEngine_EvaluateWatches_NonMatchingTrigger_NoAction(t *testing.T) {
	pr := testPR()
	pr.CIState = "failing" // watch is on:ci-pass; won't match

	eng, store, bus, _ := engineFor(t, testWatch(), &fakeActionExecutor{}, nil)
	eng.evaluateWatchesForPR(context.Background(), pr)

	if store.updateCalled {
		t.Error("UpdateWatchStatus should not be called when trigger does not fire")
	}
	if bus.publishedCount() != 0 {
		t.Error("no events should be published when trigger does not fire")
	}
}

// ── buildSnapshot ─────────────────────────────────────────────────────────────

func TestEngine_BuildSnapshot_ReviewersError(t *testing.T) {
	prReader := &fakePRReader{reviewersErr: errors.New("db error")}
	eng := newTestEngine(&fakeWatchStore{}, prReader, &fakeActionExecutor{}, &fakeNotifier{}, &fakeEngineEventBus{}, &fakeEngineAuditLogger{})

	_, err := eng.buildSnapshot(testPR())
	if err == nil {
		t.Fatal("expected error from ListReviewers failure")
	}
}

func TestEngine_BuildSnapshot_ThreadsError(t *testing.T) {
	prReader := &fakePRReader{
		reviewers:  map[string][]persistence.Reviewer{},
		threadsErr: errors.New("db error"),
	}
	eng := newTestEngine(&fakeWatchStore{}, prReader, &fakeActionExecutor{}, &fakeNotifier{}, &fakeEngineEventBus{}, &fakeEngineAuditLogger{})

	_, err := eng.buildSnapshot(testPR())
	if err == nil {
		t.Fatal("expected error from ListReviewThreads failure")
	}
}

func TestEngine_BuildSnapshot_NoThreads_AllResolvedTrue(t *testing.T) {
	eng := newTestEngine(&fakeWatchStore{}, emptyPRReader(), &fakeActionExecutor{}, &fakeNotifier{}, &fakeEngineEventBus{}, &fakeEngineAuditLogger{})

	snapshot, err := eng.buildSnapshot(testPR())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !snapshot.AllThreadsResolved {
		t.Error("AllThreadsResolved should be true when there are no threads")
	}
}

func TestEngine_BuildSnapshot_UnresolvedThread_AllResolvedFalse(t *testing.T) {
	prReader := &fakePRReader{
		reviewers: map[string][]persistence.Reviewer{},
		threads: map[string][]persistence.ReviewThread{
			"pr-id-1": {{PRID: "pr-id-1", ID: "t1", Resolved: false}},
		},
	}
	eng := newTestEngine(&fakeWatchStore{}, prReader, &fakeActionExecutor{}, &fakeNotifier{}, &fakeEngineEventBus{}, &fakeEngineAuditLogger{})

	snapshot, err := eng.buildSnapshot(testPR())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshot.AllThreadsResolved {
		t.Error("AllThreadsResolved should be false when any thread is unresolved")
	}
}

func TestEngine_BuildSnapshot_AllThreadsResolved(t *testing.T) {
	prReader := &fakePRReader{
		reviewers: map[string][]persistence.Reviewer{},
		threads: map[string][]persistence.ReviewThread{
			"pr-id-1": {
				{PRID: "pr-id-1", ID: "t1", Resolved: true},
				{PRID: "pr-id-1", ID: "t2", Resolved: true},
			},
		},
	}
	eng := newTestEngine(&fakeWatchStore{}, prReader, &fakeActionExecutor{}, &fakeNotifier{}, &fakeEngineEventBus{}, &fakeEngineAuditLogger{})

	snapshot, err := eng.buildSnapshot(testPR())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !snapshot.AllThreadsResolved {
		t.Error("AllThreadsResolved should be true when all threads are resolved")
	}
}

func TestEngine_BuildSnapshot_ApprovalCount(t *testing.T) {
	prReader := &fakePRReader{
		reviewers: map[string][]persistence.Reviewer{
			"pr-id-1": {
				{PRID: "pr-id-1", Login: "alice", State: "APPROVED"},
				{PRID: "pr-id-1", Login: "bob", State: "CHANGES_REQUESTED"},
				{PRID: "pr-id-1", Login: "carol", State: "APPROVED"},
			},
		},
		threads: map[string][]persistence.ReviewThread{},
	}
	eng := newTestEngine(&fakeWatchStore{}, prReader, &fakeActionExecutor{}, &fakeNotifier{}, &fakeEngineEventBus{}, &fakeEngineAuditLogger{})

	snapshot, err := eng.buildSnapshot(testPR())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshot.ApprovalCount != 2 {
		t.Errorf("ApprovalCount = %d, want 2", snapshot.ApprovalCount)
	}
}

func TestEngine_BuildSnapshot_ClockInjected(t *testing.T) {
	futureTime := fixedTime.Add(48 * time.Hour)
	clock := func() time.Time { return futureTime }

	eng := newTestEngineWithClock(&fakeWatchStore{}, emptyPRReader(), &fakeActionExecutor{}, &fakeNotifier{}, &fakeEngineEventBus{}, &fakeEngineAuditLogger{}, clock)

	snapshot, err := eng.buildSnapshot(testPR())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshot.Now != futureTime {
		t.Errorf("snapshot.Now = %v, want %v", snapshot.Now, futureTime)
	}
}

// ── evaluateWatch: error paths ────────────────────────────────────────────────

func TestEngine_EvaluateWatch_InvalidTriggerInDB_StatusFailed(t *testing.T) {
	w := testWatch()
	w.TriggerExpr = "bad-trigger" // missing "on:" prefix

	eng, store, bus, audit := engineFor(t, w, nil, nil)
	eng.evaluateWatchesForPR(context.Background(), testPR())

	if store.lastStatus != "failed" {
		t.Errorf("watch status = %q, want %q", store.lastStatus, "failed")
	}
	if bus.publishedCount() != 1 {
		t.Errorf("expected 1 WATCH_FIRED event, got %d", bus.publishedCount())
	}
	if bus.lastPublished().Type != eventbus.WatchFired {
		t.Errorf("event type = %q, want %q", bus.lastPublished().Type, eventbus.WatchFired)
	}
	if len(audit.entries) == 0 {
		t.Fatal("expected audit entry for failed watch")
	}
	if audit.entries[0].action != "watch-failed" {
		t.Errorf("audit action = %q, want %q", audit.entries[0].action, "watch-failed")
	}
}

func TestEngine_EvaluateWatch_InvalidActionInDB_StatusFailed(t *testing.T) {
	w := testWatch()
	w.ActionExpr = "" // empty is invalid

	eng, store, bus, _ := engineFor(t, w, nil, nil)
	eng.evaluateWatchesForPR(context.Background(), testPR())

	if store.lastStatus != "failed" {
		t.Errorf("watch status = %q, want %q", store.lastStatus, "failed")
	}
	if bus.publishedCount() != 1 {
		t.Errorf("expected 1 WATCH_FIRED event on invalid action, got %d", bus.publishedCount())
	}
}

func TestEngine_EvaluateWatch_ActionFails_StatusFailed(t *testing.T) {
	executor := &fakeActionExecutor{mergeErr: errors.New("API rate limited")}
	eng, store, bus, audit := engineFor(t, testWatch(), executor, nil)
	eng.evaluateWatchesForPR(context.Background(), testPR())

	if store.lastStatus != "failed" {
		t.Errorf("watch status = %q, want %q", store.lastStatus, "failed")
	}
	if bus.publishedCount() != 1 {
		t.Errorf("expected 1 WATCH_FIRED event, got %d", bus.publishedCount())
	}
	if len(audit.entries) == 0 {
		t.Fatal("expected audit entry for action failure")
	}
	if audit.entries[0].action != "watch-failed" {
		t.Errorf("audit action = %q, want %q", audit.entries[0].action, "watch-failed")
	}
}

// ── executeAction: all action types ──────────────────────────────────────────

func TestEngine_ExecuteAction_Merge_DefaultMethod(t *testing.T) {
	w := testWatch()
	w.ActionExpr = "merge"

	executor := &fakeActionExecutor{}
	eng, store, _, _ := engineFor(t, w, executor, nil)
	eng.evaluateWatchesForPR(context.Background(), testPR())

	if !executor.mergeCalled {
		t.Fatal("MergePR should have been called")
	}
	if executor.lastMergeMethod != "" {
		t.Errorf("merge method = %q, want empty string (repo default)", executor.lastMergeMethod)
	}
	if store.lastStatus != "fired" {
		t.Errorf("watch status = %q, want %q", store.lastStatus, "fired")
	}
}

func TestEngine_ExecuteAction_MergeSquash(t *testing.T) {
	w := testWatch()
	w.ActionExpr = "merge:squash"

	executor := &fakeActionExecutor{}
	eng, _, _, _ := engineFor(t, w, executor, nil)
	eng.evaluateWatchesForPR(context.Background(), testPR())

	if !executor.mergeCalled {
		t.Fatal("MergePR should have been called")
	}
	if executor.lastMergeMethod != "squash" {
		t.Errorf("merge method = %q, want %q", executor.lastMergeMethod, "squash")
	}
}

func TestEngine_ExecuteAction_Ready(t *testing.T) {
	w := testWatch()
	w.ActionExpr = "ready"

	executor := &fakeActionExecutor{}
	eng, _, _, _ := engineFor(t, w, executor, nil)
	eng.evaluateWatchesForPR(context.Background(), testPR())

	if !executor.markReadyCalled {
		t.Fatal("MarkReadyForReview should have been called")
	}
}

func TestEngine_ExecuteAction_Request(t *testing.T) {
	w := testWatch()
	w.ActionExpr = "request:@alice"

	executor := &fakeActionExecutor{}
	eng, _, _, _ := engineFor(t, w, executor, nil)
	eng.evaluateWatchesForPR(context.Background(), testPR())

	if !executor.requestCalled {
		t.Fatal("RequestReview should have been called")
	}
	if len(executor.lastRequestedUsers) != 1 || executor.lastRequestedUsers[0] != "alice" {
		t.Errorf("requested users = %v, want [alice]", executor.lastRequestedUsers)
	}
}

func TestEngine_ExecuteAction_Review(t *testing.T) {
	w := testWatch()
	w.ActionExpr = "review:alice,bob"

	executor := &fakeActionExecutor{}
	eng, _, _, _ := engineFor(t, w, executor, nil)
	eng.evaluateWatchesForPR(context.Background(), testPR())

	if !executor.requestCalled {
		t.Fatal("RequestReview should have been called")
	}
	if len(executor.lastRequestedUsers) != 2 {
		t.Fatalf("expected 2 users, got %v", executor.lastRequestedUsers)
	}
	if executor.lastRequestedUsers[0] != "alice" || executor.lastRequestedUsers[1] != "bob" {
		t.Errorf("requested users = %v, want [alice bob]", executor.lastRequestedUsers)
	}
}

func TestEngine_ExecuteAction_Review_StripsAtPrefix(t *testing.T) {
	w := testWatch()
	w.ActionExpr = "review:@alice,@bob"

	executor := &fakeActionExecutor{}
	eng, _, _, _ := engineFor(t, w, executor, nil)
	eng.evaluateWatchesForPR(context.Background(), testPR())

	if !executor.requestCalled {
		t.Fatal("RequestReview should have been called")
	}
	if executor.lastRequestedUsers[0] != "alice" || executor.lastRequestedUsers[1] != "bob" {
		t.Errorf("@ prefix should be stripped: got %v", executor.lastRequestedUsers)
	}
}

func TestEngine_ExecuteAction_Comment(t *testing.T) {
	w := testWatch()
	w.ActionExpr = "comment:LGTM!"

	executor := &fakeActionExecutor{}
	eng, _, _, _ := engineFor(t, w, executor, nil)
	eng.evaluateWatchesForPR(context.Background(), testPR())

	if !executor.commentCalled {
		t.Fatal("PostComment should have been called")
	}
	if executor.lastCommentBody != "LGTM!" {
		t.Errorf("comment body = %q, want %q", executor.lastCommentBody, "LGTM!")
	}
}

func TestEngine_ExecuteAction_Label(t *testing.T) {
	w := testWatch()
	w.ActionExpr = "label:ready"

	executor := &fakeActionExecutor{}
	eng, _, _, _ := engineFor(t, w, executor, nil)
	eng.evaluateWatchesForPR(context.Background(), testPR())

	if !executor.labelCalled {
		t.Fatal("AddLabel should have been called")
	}
	if executor.lastLabelName != "ready" {
		t.Errorf("label name = %q, want %q", executor.lastLabelName, "ready")
	}
}

func TestEngine_ExecuteAction_Notify(t *testing.T) {
	w := testWatch()
	w.ActionExpr = "notify"

	notifier := &fakeNotifier{}
	store := &fakeWatchStore{watches: []persistence.Watch{w}}
	eng := newTestEngine(store, emptyPRReader(), &fakeActionExecutor{}, notifier, &fakeEngineEventBus{}, &fakeEngineAuditLogger{})
	eng.evaluateWatchesForPR(context.Background(), testPR())

	if !notifier.notifyCalled {
		t.Fatal("Notify should have been called")
	}
	if !strings.Contains(notifier.lastTitle, "owner/repo") {
		t.Errorf("notification title %q should contain repo name", notifier.lastTitle)
	}
	pr := testPR()
	if notifier.lastBody != pr.Title {
		t.Errorf("notification body = %q, want %q", notifier.lastBody, pr.Title)
	}
}

func TestEngine_ExecuteAction_NotifyError_StatusFailed(t *testing.T) {
	w := testWatch()
	w.ActionExpr = "notify"

	notifier := &fakeNotifier{err: errors.New("notification service unavailable")}
	store := &fakeWatchStore{watches: []persistence.Watch{w}}
	bus := &fakeEngineEventBus{}
	eng := newTestEngine(store, emptyPRReader(), &fakeActionExecutor{}, notifier, bus, &fakeEngineAuditLogger{})
	eng.evaluateWatchesForPR(context.Background(), testPR())

	if store.lastStatus != "failed" {
		t.Errorf("watch status = %q, want %q", store.lastStatus, "failed")
	}
}

func TestEngine_ExecuteAction_UnknownType_ReturnsError(t *testing.T) {
	eng := newTestEngine(&fakeWatchStore{}, emptyPRReader(), &fakeActionExecutor{}, &fakeNotifier{}, &fakeEngineEventBus{}, &fakeEngineAuditLogger{})

	err := eng.executeAction(context.Background(), Action{Type: "unknown-type"}, testPR())
	if err == nil {
		t.Fatal("expected error for unknown action type")
	}
}

// ── time-based trigger ────────────────────────────────────────────────────────

func TestEngine_StaleTimeTrigger_ElapsedBeyondDuration_Fires(t *testing.T) {
	pr := testPR()
	pr.LastActivityAt = fixedTime.Add(-25 * time.Hour) // 25h ago; clock=fixedTime → stale

	w := testWatch()
	w.TriggerExpr = "on:24h-stale"
	w.ActionExpr = "notify"

	notifier := &fakeNotifier{}
	store := &fakeWatchStore{watches: []persistence.Watch{w}}
	eng := newTestEngine(store, emptyPRReader(), &fakeActionExecutor{}, notifier, &fakeEngineEventBus{}, &fakeEngineAuditLogger{})
	eng.evaluateWatchesForPR(context.Background(), pr)

	if !notifier.notifyCalled {
		t.Error("Notify should have been called when elapsed ≥ 24h")
	}
	if store.lastStatus != "fired" {
		t.Errorf("watch status = %q, want %q", store.lastStatus, "fired")
	}
}

func TestEngine_StaleTimeTrigger_NotElapsed_DoesNotFire(t *testing.T) {
	pr := testPR()
	pr.LastActivityAt = fixedTime.Add(-1 * time.Hour) // only 1h ago

	w := testWatch()
	w.TriggerExpr = "on:24h-stale"
	w.ActionExpr = "notify"

	notifier := &fakeNotifier{}
	store := &fakeWatchStore{watches: []persistence.Watch{w}}
	eng := newTestEngine(store, emptyPRReader(), &fakeActionExecutor{}, notifier, &fakeEngineEventBus{}, &fakeEngineAuditLogger{})
	eng.evaluateWatchesForPR(context.Background(), pr)

	if notifier.notifyCalled {
		t.Error("Notify should not be called when elapsed < 24h")
	}
	if store.updateCalled {
		t.Error("UpdateWatchStatus should not be called when trigger does not fire")
	}
}

// ── multiple watches ──────────────────────────────────────────────────────────

func TestEngine_MultipleWatches_EachEvaluatedIndependently(t *testing.T) {
	pr := testPR() // CIState=passing

	w1 := testWatch()
	w1.ID = "w1"
	w1.TriggerExpr = "on:ci-pass"
	w1.ActionExpr = "merge"

	w2 := testWatch()
	w2.ID = "w2"
	w2.TriggerExpr = "on:ci-fail" // will NOT fire
	w2.ActionExpr = "notify"

	w3 := testWatch()
	w3.ID = "w3"
	w3.TriggerExpr = "on:ci-pass"
	w3.ActionExpr = "comment:done"

	store := &fakeWatchStore{watches: []persistence.Watch{w1, w2, w3}}
	executor := &fakeActionExecutor{}
	notifier := &fakeNotifier{}
	bus := &fakeEngineEventBus{}

	eng := newTestEngine(store, emptyPRReader(), executor, notifier, bus, &fakeEngineAuditLogger{})
	eng.evaluateWatchesForPR(context.Background(), pr)

	if !executor.mergeCalled {
		t.Error("w1: MergePR should have been called")
	}
	if !executor.commentCalled {
		t.Error("w3: PostComment should have been called")
	}
	if notifier.notifyCalled {
		t.Error("w2: Notify should not have been called (ci-fail does not match)")
	}
	if bus.publishedCount() != 2 {
		t.Errorf("expected 2 WATCH_FIRED events (w1+w3), got %d", bus.publishedCount())
	}
}

// ── combined actions ──────────────────────────────────────────────────────────

func TestEngine_CombinedActions_BothExecuted(t *testing.T) {
	w := testWatch()
	w.ActionExpr = "comment:LGTM! + notify"

	executor := &fakeActionExecutor{}
	notifier := &fakeNotifier{}
	store := &fakeWatchStore{watches: []persistence.Watch{w}}
	eng := newTestEngine(store, emptyPRReader(), executor, notifier, &fakeEngineEventBus{}, &fakeEngineAuditLogger{})
	eng.evaluateWatchesForPR(context.Background(), testPR())

	if !executor.commentCalled {
		t.Error("PostComment should have been called")
	}
	if !notifier.notifyCalled {
		t.Error("Notify should have been called")
	}
	if store.lastStatus != "fired" {
		t.Errorf("watch status = %q, want %q", store.lastStatus, "fired")
	}
}

func TestEngine_CombinedActions_FirstFails_SecondNotExecuted(t *testing.T) {
	w := testWatch()
	w.ActionExpr = "merge + notify"

	executor := &fakeActionExecutor{mergeErr: errors.New("merge conflict")}
	notifier := &fakeNotifier{}
	store := &fakeWatchStore{watches: []persistence.Watch{w}}
	bus := &fakeEngineEventBus{}
	eng := newTestEngine(store, emptyPRReader(), executor, notifier, bus, &fakeEngineAuditLogger{})
	eng.evaluateWatchesForPR(context.Background(), testPR())

	if notifier.notifyCalled {
		t.Error("Notify should not have been called when merge failed")
	}
	if store.lastStatus != "failed" {
		t.Errorf("watch status = %q, want %q", store.lastStatus, "failed")
	}
}

// ── splitRepo ─────────────────────────────────────────────────────────────────

func TestSplitRepo(t *testing.T) {
	tests := []struct {
		full      string
		wantOwner string
		wantRepo  string
	}{
		{"owner/repo", "owner", "repo"},
		{"acme/my-service", "acme", "my-service"},
		{"noslash", "noslash", ""},
		{"owner/repo/extra", "owner", "repo/extra"},
	}
	for _, tt := range tests {
		t.Run(tt.full, func(t *testing.T) {
			owner, repo := splitRepo(tt.full)
			if owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

// ── audit log details ─────────────────────────────────────────────────────────

func TestEngine_AuditLog_OwnerRepoParsedFromWatchRepo(t *testing.T) {
	w := testWatch()
	w.Repo = "myorg/myrepo"

	audit := &fakeEngineAuditLogger{}
	store := &fakeWatchStore{watches: []persistence.Watch{w}}
	eng := newTestEngine(store, emptyPRReader(), &fakeActionExecutor{}, &fakeNotifier{}, &fakeEngineEventBus{}, audit)
	eng.evaluateWatchesForPR(context.Background(), testPR())

	if len(audit.entries) == 0 {
		t.Fatal("expected audit entry")
	}
	if audit.entries[0].owner != "myorg" {
		t.Errorf("audit owner = %q, want %q", audit.entries[0].owner, "myorg")
	}
	if audit.entries[0].repo != "myrepo" {
		t.Errorf("audit repo = %q, want %q", audit.entries[0].repo, "myrepo")
	}
	if audit.entries[0].number != 42 {
		t.Errorf("audit number = %d, want 42", audit.entries[0].number)
	}
}
