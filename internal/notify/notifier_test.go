package notify

import (
	"strings"
	"testing"

	"github.com/evanisnor/argh/internal/config"
	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
)

// ── Test doubles ─────────────────────────────────────────────────────────────

type stubSender struct {
	calls []senderCall
	err   error
}

type senderCall struct {
	title string
	body  string
}

func (s *stubSender) Notify(title, body string) error {
	s.calls = append(s.calls, senderCall{title: title, body: body})
	return s.err
}

type fakeBus struct {
	handler func(eventbus.Event)
}

func (b *fakeBus) Subscribe(handler func(eventbus.Event)) func() {
	b.handler = handler
	return func() { b.handler = nil }
}

func (b *fakeBus) send(e eventbus.Event) {
	if b.handler != nil {
		b.handler(e)
	}
}

type stubDND struct {
	active bool
}

func (d *stubDND) IsDND() bool { return d.active }

// ── Helpers ───────────────────────────────────────────────────────────────────

func allEnabled() config.NotificationsConfig {
	return config.NotificationsConfig{
		CIPass:           true,
		CIFail:           true,
		Approved:         true,
		ChangesRequested: true,
		ReviewRequested:  true,
		Merged:           true,
		WatchTriggered:   true,
	}
}

func makePR(repo string, number int, status, ciState, author string) persistence.PullRequest {
	return persistence.PullRequest{
		Repo:    repo,
		Number:  number,
		Title:   "Test PR",
		Status:  status,
		CIState: ciState,
		Author:  author,
	}
}

func makeNotifier(bus *fakeBus, sender *stubSender, cfg config.NotificationsConfig, dnd *stubDND) *Notifier {
	return New(bus, sender, cfg, dnd, "testuser", nil)
}

// ── CIChanged event tests ─────────────────────────────────────────────────────

func TestNotifier_CIChanged_RunningToPassingFiresNotification(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, allEnabled(), &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 1, "open", "running", "testuser")
	after := makePR("org/repo", 1, "open", "passing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.CIChanged, Before: before, After: after})

	if len(sender.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(sender.calls))
	}
	if !strings.Contains(sender.calls[0].title, "Passing") {
		t.Errorf("expected title to contain 'Passing', got %q", sender.calls[0].title)
	}
}

func TestNotifier_CIChanged_RunningToFailingFiresNotification(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, allEnabled(), &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 2, "open", "running", "testuser")
	after := makePR("org/repo", 2, "open", "failing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.CIChanged, Before: before, After: after})

	if len(sender.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(sender.calls))
	}
	if !strings.Contains(sender.calls[0].title, "Failing") {
		t.Errorf("expected title to contain 'Failing', got %q", sender.calls[0].title)
	}
}

func TestNotifier_CIChanged_RunningToRunningDoesNotFire(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, allEnabled(), &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 3, "open", "running", "testuser")
	after := makePR("org/repo", 3, "open", "running", "testuser")

	bus.send(eventbus.Event{Type: eventbus.CIChanged, Before: before, After: after})

	if len(sender.calls) != 0 {
		t.Errorf("expected no notification for running→running, got %d", len(sender.calls))
	}
}

func TestNotifier_CIChanged_PassingToPassingDoesNotFire(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, allEnabled(), &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 4, "open", "passing", "testuser")
	after := makePR("org/repo", 4, "open", "passing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.CIChanged, Before: before, After: after})

	if len(sender.calls) != 0 {
		t.Errorf("expected no notification for passing→passing, got %d", len(sender.calls))
	}
}

func TestNotifier_CIChanged_ConfigFlagFalseDisablesPassNotification(t *testing.T) {
	cfg := allEnabled()
	cfg.CIPass = false
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, cfg, &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 5, "open", "running", "testuser")
	after := makePR("org/repo", 5, "open", "passing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.CIChanged, Before: before, After: after})

	if len(sender.calls) != 0 {
		t.Errorf("expected no notification when ci_pass=false, got %d", len(sender.calls))
	}
}

func TestNotifier_CIChanged_ConfigFlagFalseDisablesFailNotification(t *testing.T) {
	cfg := allEnabled()
	cfg.CIFail = false
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, cfg, &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 6, "open", "running", "testuser")
	after := makePR("org/repo", 6, "open", "failing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.CIChanged, Before: before, After: after})

	if len(sender.calls) != 0 {
		t.Errorf("expected no notification when ci_fail=false, got %d", len(sender.calls))
	}
}

func TestNotifier_CIChanged_NotificationContainsPRDetails(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, allEnabled(), &stubDND{})
	defer n.Close()

	before := makePR("org/myrepo", 42, "open", "running", "testuser")
	after := makePR("org/myrepo", 42, "open", "passing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.CIChanged, Before: before, After: after})

	if len(sender.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(sender.calls))
	}
	if !strings.Contains(sender.calls[0].title, "org/myrepo") {
		t.Errorf("title missing repo name: %q", sender.calls[0].title)
	}
	if !strings.Contains(sender.calls[0].title, "42") {
		t.Errorf("title missing PR number: %q", sender.calls[0].title)
	}
	if sender.calls[0].body != "Test PR" {
		t.Errorf("body = %q, want %q", sender.calls[0].body, "Test PR")
	}
}

// ── ReviewChanged event tests ─────────────────────────────────────────────────

func TestNotifier_ReviewChanged_ApprovedFiresNotification(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, allEnabled(), &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 10, "open", "passing", "testuser")
	after := makePR("org/repo", 10, "approved", "passing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.ReviewChanged, Before: before, After: after})

	if len(sender.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(sender.calls))
	}
	if !strings.Contains(sender.calls[0].title, "Approved") {
		t.Errorf("expected title to contain 'Approved', got %q", sender.calls[0].title)
	}
}

func TestNotifier_ReviewChanged_ChangesRequestedFiresNotification(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, allEnabled(), &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 11, "open", "passing", "testuser")
	after := makePR("org/repo", 11, "changes requested", "passing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.ReviewChanged, Before: before, After: after})

	if len(sender.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(sender.calls))
	}
	if !strings.Contains(sender.calls[0].title, "Changes Requested") {
		t.Errorf("expected title to contain 'Changes Requested', got %q", sender.calls[0].title)
	}
}

func TestNotifier_ReviewChanged_AlreadyApprovedDoesNotRepeat(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, allEnabled(), &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 12, "approved", "passing", "testuser")
	after := makePR("org/repo", 12, "approved", "passing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.ReviewChanged, Before: before, After: after})

	if len(sender.calls) != 0 {
		t.Errorf("expected no notification when already approved, got %d", len(sender.calls))
	}
}

func TestNotifier_ReviewChanged_ConfigFlagFalseDisablesApprovedNotification(t *testing.T) {
	cfg := allEnabled()
	cfg.Approved = false
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, cfg, &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 13, "open", "passing", "testuser")
	after := makePR("org/repo", 13, "approved", "passing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.ReviewChanged, Before: before, After: after})

	if len(sender.calls) != 0 {
		t.Errorf("expected no notification when approved=false, got %d", len(sender.calls))
	}
}

func TestNotifier_ReviewChanged_ConfigFlagFalseDisablesChangesRequestedNotification(t *testing.T) {
	cfg := allEnabled()
	cfg.ChangesRequested = false
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, cfg, &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 14, "open", "passing", "testuser")
	after := makePR("org/repo", 14, "changes requested", "passing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.ReviewChanged, Before: before, After: after})

	if len(sender.calls) != 0 {
		t.Errorf("expected no notification when changes_requested=false, got %d", len(sender.calls))
	}
}

// ── PRUpdated event tests ─────────────────────────────────────────────────────

func TestNotifier_PRUpdated_NewPRFromOtherAuthorFiresReviewRequested(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, allEnabled(), &stubDND{})
	defer n.Close()

	after := makePR("org/repo", 20, "open", "running", "otheruser")

	bus.send(eventbus.Event{Type: eventbus.PRUpdated, Before: nil, After: after})

	if len(sender.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(sender.calls))
	}
	if !strings.Contains(sender.calls[0].title, "Review Requested") {
		t.Errorf("expected title to contain 'Review Requested', got %q", sender.calls[0].title)
	}
}

func TestNotifier_PRUpdated_NewPRFromSelfDoesNotFireReviewRequested(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, allEnabled(), &stubDND{})
	defer n.Close()

	after := makePR("org/repo", 21, "open", "running", "testuser")

	bus.send(eventbus.Event{Type: eventbus.PRUpdated, Before: nil, After: after})

	if len(sender.calls) != 0 {
		t.Errorf("expected no notification for own PR, got %d", len(sender.calls))
	}
}

func TestNotifier_PRUpdated_ConfigFlagFalseDisablesReviewRequestedNotification(t *testing.T) {
	cfg := allEnabled()
	cfg.ReviewRequested = false
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, cfg, &stubDND{})
	defer n.Close()

	after := makePR("org/repo", 22, "open", "running", "otheruser")

	bus.send(eventbus.Event{Type: eventbus.PRUpdated, Before: nil, After: after})

	if len(sender.calls) != 0 {
		t.Errorf("expected no notification when review_requested=false, got %d", len(sender.calls))
	}
}

func TestNotifier_PRUpdated_StatusChangeToApprovedFiresNotification(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, allEnabled(), &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 30, "open", "passing", "testuser")
	after := makePR("org/repo", 30, "approved", "passing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.PRUpdated, Before: before, After: after})

	if len(sender.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(sender.calls))
	}
	if !strings.Contains(sender.calls[0].title, "Approved") {
		t.Errorf("expected 'Approved' in title, got %q", sender.calls[0].title)
	}
}

func TestNotifier_PRUpdated_StatusChangeToChangesRequestedFiresNotification(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, allEnabled(), &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 31, "open", "passing", "testuser")
	after := makePR("org/repo", 31, "changes requested", "passing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.PRUpdated, Before: before, After: after})

	if len(sender.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(sender.calls))
	}
	if !strings.Contains(sender.calls[0].title, "Changes Requested") {
		t.Errorf("expected 'Changes Requested' in title, got %q", sender.calls[0].title)
	}
}

func TestNotifier_PRUpdated_StatusChangeToMergedFiresNotification(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, allEnabled(), &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 32, "open", "passing", "testuser")
	after := makePR("org/repo", 32, "merged", "passing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.PRUpdated, Before: before, After: after})

	if len(sender.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(sender.calls))
	}
	if !strings.Contains(sender.calls[0].title, "Merged") {
		t.Errorf("expected 'Merged' in title, got %q", sender.calls[0].title)
	}
}

func TestNotifier_PRUpdated_StatusChangeToClosedFiresNotification(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, allEnabled(), &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 33, "open", "passing", "testuser")
	after := makePR("org/repo", 33, "closed", "passing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.PRUpdated, Before: before, After: after})

	if len(sender.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(sender.calls))
	}
	if !strings.Contains(sender.calls[0].title, "Closed") {
		t.Errorf("expected 'Closed' in title, got %q", sender.calls[0].title)
	}
}

func TestNotifier_PRUpdated_SameStatusNoNotification(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, allEnabled(), &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 34, "open", "passing", "testuser")
	after := makePR("org/repo", 34, "open", "passing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.PRUpdated, Before: before, After: after})

	if len(sender.calls) != 0 {
		t.Errorf("expected no notification for same status, got %d", len(sender.calls))
	}
}

func TestNotifier_PRUpdated_MergedConfigFalseDisablesMergedNotification(t *testing.T) {
	cfg := allEnabled()
	cfg.Merged = false
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, cfg, &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 35, "open", "passing", "testuser")
	after := makePR("org/repo", 35, "merged", "passing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.PRUpdated, Before: before, After: after})

	if len(sender.calls) != 0 {
		t.Errorf("expected no notification when merged=false, got %d", len(sender.calls))
	}
}

// ── WatchFired event tests ────────────────────────────────────────────────────

func TestNotifier_WatchFired_FiresNotification(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, allEnabled(), &stubDND{})
	defer n.Close()

	w := persistence.Watch{
		ID:          "w1",
		Repo:        "org/repo",
		PRNumber:    99,
		TriggerExpr: "on:ci-pass",
		ActionExpr:  "merge",
		Status:      "fired",
	}

	bus.send(eventbus.Event{Type: eventbus.WatchFired, After: w})

	if len(sender.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(sender.calls))
	}
	if !strings.Contains(sender.calls[0].title, "Watch Fired") {
		t.Errorf("expected 'Watch Fired' in title, got %q", sender.calls[0].title)
	}
	if !strings.Contains(sender.calls[0].body, "on:ci-pass") {
		t.Errorf("expected trigger in body, got %q", sender.calls[0].body)
	}
	if !strings.Contains(sender.calls[0].body, "merge") {
		t.Errorf("expected action in body, got %q", sender.calls[0].body)
	}
}

func TestNotifier_WatchFired_ConfigFlagFalseDisablesNotification(t *testing.T) {
	cfg := allEnabled()
	cfg.WatchTriggered = false
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, cfg, &stubDND{})
	defer n.Close()

	w := persistence.Watch{
		ID:          "w2",
		Repo:        "org/repo",
		PRNumber:    100,
		TriggerExpr: "on:ci-pass",
		ActionExpr:  "merge",
		Status:      "fired",
	}

	bus.send(eventbus.Event{Type: eventbus.WatchFired, After: w})

	if len(sender.calls) != 0 {
		t.Errorf("expected no notification when watch_triggered=false, got %d", len(sender.calls))
	}
}

// ── DND suppression tests ─────────────────────────────────────────────────────

func TestNotifier_DNDActive_SuppressesAllNotifications(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	dnd := &stubDND{active: true}
	n := makeNotifier(bus, sender, allEnabled(), dnd)
	defer n.Close()

	events := []eventbus.Event{
		{
			Type:   eventbus.CIChanged,
			Before: makePR("org/repo", 1, "open", "running", "testuser"),
			After:  makePR("org/repo", 1, "open", "passing", "testuser"),
		},
		{
			Type:   eventbus.ReviewChanged,
			Before: makePR("org/repo", 2, "open", "passing", "testuser"),
			After:  makePR("org/repo", 2, "approved", "passing", "testuser"),
		},
		{
			Type:  eventbus.PRUpdated,
			After: makePR("org/repo", 3, "open", "running", "otheruser"),
		},
		{
			Type: eventbus.WatchFired,
			After: persistence.Watch{
				Repo:        "org/repo",
				PRNumber:    4,
				TriggerExpr: "on:ci-pass",
				ActionExpr:  "merge",
			},
		},
	}

	for _, e := range events {
		bus.send(e)
	}

	if len(sender.calls) != 0 {
		t.Errorf("expected no notifications when DND active, got %d", len(sender.calls))
	}
}

func TestNotifier_DNDInactive_AllowsNotifications(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	dnd := &stubDND{active: false}
	n := makeNotifier(bus, sender, allEnabled(), dnd)
	defer n.Close()

	before := makePR("org/repo", 1, "open", "running", "testuser")
	after := makePR("org/repo", 1, "open", "passing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.CIChanged, Before: before, After: after})

	if len(sender.calls) != 1 {
		t.Errorf("expected 1 notification when DND inactive, got %d", len(sender.calls))
	}
}

// ── Close / unsubscribe tests ─────────────────────────────────────────────────

func TestNotifier_Close_UnsubscribesFromBus(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, allEnabled(), &stubDND{})

	n.Close()

	// After Close, the handler should be nil — events should not be delivered.
	if bus.handler != nil {
		t.Error("expected handler to be nil after Close")
	}
}

func TestNotifier_IgnoresUnknownEventType(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, allEnabled(), &stubDND{})
	defer n.Close()

	bus.send(eventbus.Event{Type: eventbus.RateLimitWarning})

	if len(sender.calls) != 0 {
		t.Errorf("expected no notification for RateLimitWarning, got %d", len(sender.calls))
	}
}

func TestNotifier_IgnoresMalformedEventPayload(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, allEnabled(), &stubDND{})
	defer n.Close()

	// Send events with wrong payload types — should not panic or notify.
	bus.send(eventbus.Event{Type: eventbus.CIChanged, Before: "wrong", After: "type"})
	bus.send(eventbus.Event{Type: eventbus.ReviewChanged, Before: 42, After: 99})
	bus.send(eventbus.Event{Type: eventbus.PRUpdated, Before: "wrong", After: "type"})
	bus.send(eventbus.Event{Type: eventbus.WatchFired, After: "not-a-watch"})

	if len(sender.calls) != 0 {
		t.Errorf("expected no notifications for malformed payloads, got %d", len(sender.calls))
	}
}

// ── NoDND tests ───────────────────────────────────────────────────────────────

func TestNoDND_AlwaysReturnsFalse(t *testing.T) {
	d := NoDND{}
	if d.IsDND() {
		t.Error("NoDND.IsDND() should always return false")
	}
}

// ── Additional branch coverage tests ─────────────────────────────────────────

func TestNotifier_CIChanged_FailingToFailingDoesNotFire(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, allEnabled(), &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 50, "open", "failing", "testuser")
	after := makePR("org/repo", 50, "open", "failing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.CIChanged, Before: before, After: after})

	if len(sender.calls) != 0 {
		t.Errorf("expected no notification for failing→failing, got %d", len(sender.calls))
	}
}

func TestNotifier_ReviewChanged_AlreadyChangesRequestedDoesNotRepeat(t *testing.T) {
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, allEnabled(), &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 51, "changes requested", "passing", "testuser")
	after := makePR("org/repo", 51, "changes requested", "passing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.ReviewChanged, Before: before, After: after})

	if len(sender.calls) != 0 {
		t.Errorf("expected no notification when already changes requested, got %d", len(sender.calls))
	}
}

func TestNotifier_PRUpdated_ConfigFlagFalseDisablesApprovedNotification(t *testing.T) {
	cfg := allEnabled()
	cfg.Approved = false
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, cfg, &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 52, "open", "passing", "testuser")
	after := makePR("org/repo", 52, "approved", "passing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.PRUpdated, Before: before, After: after})

	if len(sender.calls) != 0 {
		t.Errorf("expected no notification when approved=false, got %d", len(sender.calls))
	}
}

func TestNotifier_PRUpdated_ConfigFlagFalseDisablesChangesRequestedNotification(t *testing.T) {
	cfg := allEnabled()
	cfg.ChangesRequested = false
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, cfg, &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 53, "open", "passing", "testuser")
	after := makePR("org/repo", 53, "changes requested", "passing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.PRUpdated, Before: before, After: after})

	if len(sender.calls) != 0 {
		t.Errorf("expected no notification when changes_requested=false, got %d", len(sender.calls))
	}
}

func TestNotifier_PRUpdated_ConfigFlagFalseDisablesClosedNotification(t *testing.T) {
	cfg := allEnabled()
	cfg.Merged = false
	bus := &fakeBus{}
	sender := &stubSender{}
	n := makeNotifier(bus, sender, cfg, &stubDND{})
	defer n.Close()

	before := makePR("org/repo", 54, "open", "passing", "testuser")
	after := makePR("org/repo", 54, "closed", "passing", "testuser")

	bus.send(eventbus.Event{Type: eventbus.PRUpdated, Before: before, After: after})

	if len(sender.calls) != 0 {
		t.Errorf("expected no notification when merged=false for closed PR, got %d", len(sender.calls))
	}
}

// ── BeeepSender tests ─────────────────────────────────────────────────────────

func TestNewBeeepSender_ReturnsInitializedSender(t *testing.T) {
	sender := NewBeeepSender()
	if sender.notifyFn == nil {
		t.Error("NewBeeepSender should initialize notifyFn")
	}
}

func TestBeeepSender_NotifyCallsUnderlyingFunction(t *testing.T) {
	var gotTitle, gotBody string
	var gotIcon any
	sender := BeeepSender{
		notifyFn: func(title, message string, icon any) error {
			gotTitle = title
			gotBody = message
			gotIcon = icon
			return nil
		},
	}

	if err := sender.Notify("Test Title", "Test Body"); err != nil {
		t.Fatalf("Notify returned unexpected error: %v", err)
	}

	if gotTitle != "Test Title" {
		t.Errorf("title = %q, want %q", gotTitle, "Test Title")
	}
	if gotBody != "Test Body" {
		t.Errorf("body = %q, want %q", gotBody, "Test Body")
	}
	if gotIcon != "" {
		t.Errorf("icon = %v, want empty string", gotIcon)
	}
}
