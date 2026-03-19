package notify

import (
	"testing"
	"time"

	"github.com/evanisnor/argh/internal/eventbus"
)

// ── Fake clock ────────────────────────────────────────────────────────────────

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) advance(d time.Duration) { c.now = c.now.Add(d) }

// ── Debouncer.Allow unit tests ────────────────────────────────────────────────

func TestDebouncer_Allow_FirstCallAlwaysPermitted(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	d := NewDebouncer(clk)

	if !d.Allow("https://github.com/org/repo/pull/1", eventbus.PRUpdated) {
		t.Error("first Allow call should return true")
	}
}

func TestDebouncer_Allow_SameEventWithin5sSuppressed(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	d := NewDebouncer(clk)

	d.Allow("https://github.com/org/repo/pull/1", eventbus.PRUpdated) // first — passes

	clk.advance(4 * time.Second) // still within 5s window

	if d.Allow("https://github.com/org/repo/pull/1", eventbus.PRUpdated) {
		t.Error("second Allow within 5s should return false")
	}
}

func TestDebouncer_Allow_SameEventAfter5sPermitted(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	d := NewDebouncer(clk)

	d.Allow("https://github.com/org/repo/pull/1", eventbus.PRUpdated) // first — passes

	clk.advance(5 * time.Second) // exactly at window boundary → permitted

	if !d.Allow("https://github.com/org/repo/pull/1", eventbus.PRUpdated) {
		t.Error("Allow after 5s should return true")
	}
}

func TestDebouncer_Allow_DifferentPRsIndependent(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	d := NewDebouncer(clk)

	d.Allow("https://github.com/org/repo/pull/1", eventbus.PRUpdated)
	clk.advance(1 * time.Second)

	// Different PR — should not be affected by the first PR's window.
	if !d.Allow("https://github.com/org/repo/pull/2", eventbus.PRUpdated) {
		t.Error("Allow for different PR should return true regardless of other PR's window")
	}
}

func TestDebouncer_Allow_DifferentEventTypesSamePR(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	d := NewDebouncer(clk)

	d.Allow("https://github.com/org/repo/pull/1", eventbus.PRUpdated)
	clk.advance(1 * time.Second)

	// Different event type for the same PR — independent window.
	if !d.Allow("https://github.com/org/repo/pull/1", eventbus.ReviewChanged) {
		t.Error("Allow for different event type should return true")
	}
}

// ── CI flapping (60-second window) tests ─────────────────────────────────────

func TestDebouncer_Allow_CIWithin60sSuppressed(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	d := NewDebouncer(clk)

	d.Allow("https://github.com/org/repo/pull/1", eventbus.CIChanged) // pass — sent

	clk.advance(10 * time.Second)
	if d.Allow("https://github.com/org/repo/pull/1", eventbus.CIChanged) { // fail — within 60s
		t.Error("CI fail within 60s should be suppressed")
	}

	clk.advance(10 * time.Second)
	if d.Allow("https://github.com/org/repo/pull/1", eventbus.CIChanged) { // pass again — within 60s
		t.Error("CI pass (second) within 60s should be suppressed")
	}
}

func TestDebouncer_Allow_CIAfter60sPermitted(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	d := NewDebouncer(clk)

	d.Allow("https://github.com/org/repo/pull/1", eventbus.CIChanged) // T=0 pass → sent

	clk.advance(10 * time.Second) // T=10 fail within 60s → suppressed
	d.Allow("https://github.com/org/repo/pull/1", eventbus.CIChanged)

	clk.advance(55 * time.Second) // T=65 → >60s since last sent (T=0) → permitted
	if !d.Allow("https://github.com/org/repo/pull/1", eventbus.CIChanged) {
		t.Error("CI Allow after 60s should return true")
	}
}

// ── Integrated Notifier + Debouncer tests ─────────────────────────────────────

func makePRWithURL(repo string, number int, status, ciState, author, url string) interface{} {
	// We return a typed PR using the same helper pattern as notifier_test.go
	// but include a URL field.
	pr := makePR(repo, number, status, ciState, author)
	pr.URL = url
	return pr
}

func TestNotifierDebounce_SameEventWithin5sDropped(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	bus := &fakeBus{}
	sender := &stubSender{}
	debouncer := NewDebouncer(clk)
	n := New(bus, sender, allEnabled(), &stubDND{}, "testuser", debouncer)
	defer n.Close()

	pr1 := makePR("org/repo", 1, "open", "running", "otheruser")
	pr1.URL = "https://github.com/org/repo/pull/1"
	pr2 := makePR("org/repo", 1, "open", "running", "otheruser")
	pr2.URL = "https://github.com/org/repo/pull/1"

	// First PRUpdated — new PR, review requested notification.
	bus.send(eventbus.Event{Type: eventbus.PRUpdated, Before: nil, After: pr1})
	if len(sender.calls) != 1 {
		t.Fatalf("expected 1 notification after first event, got %d", len(sender.calls))
	}

	// Same event within 5s — should be dropped.
	clk.advance(3 * time.Second)
	bus.send(eventbus.Event{Type: eventbus.PRUpdated, Before: nil, After: pr2})
	if len(sender.calls) != 1 {
		t.Errorf("expected notification still at 1 (debounced), got %d", len(sender.calls))
	}
}

func TestNotifierDebounce_SameEventAfter5sSent(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	bus := &fakeBus{}
	sender := &stubSender{}
	debouncer := NewDebouncer(clk)
	n := New(bus, sender, allEnabled(), &stubDND{}, "testuser", debouncer)
	defer n.Close()

	pr := makePR("org/repo", 1, "open", "running", "otheruser")
	pr.URL = "https://github.com/org/repo/pull/1"

	bus.send(eventbus.Event{Type: eventbus.PRUpdated, Before: nil, After: pr})
	if len(sender.calls) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(sender.calls))
	}

	clk.advance(5 * time.Second)
	bus.send(eventbus.Event{Type: eventbus.PRUpdated, Before: nil, After: pr})
	if len(sender.calls) != 2 {
		t.Errorf("expected 2 notifications after 5s, got %d", len(sender.calls))
	}
}

func TestNotifierDebounce_CIPassFailPassWithin60sSingleNotification(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	bus := &fakeBus{}
	sender := &stubSender{}
	debouncer := NewDebouncer(clk)
	n := New(bus, sender, allEnabled(), &stubDND{}, "testuser", debouncer)
	defer n.Close()

	url := "https://github.com/org/repo/pull/42"

	running := makePR("org/repo", 42, "open", "running", "testuser")
	running.URL = url
	passing := makePR("org/repo", 42, "open", "passing", "testuser")
	passing.URL = url
	failing := makePR("org/repo", 42, "open", "failing", "testuser")
	failing.URL = url

	// T=0: running → passing
	bus.send(eventbus.Event{Type: eventbus.CIChanged, Before: running, After: passing})
	if len(sender.calls) != 1 {
		t.Fatalf("expected 1 notification after pass, got %d", len(sender.calls))
	}

	// T=10s: passing → failing (within 60s — suppressed)
	clk.advance(10 * time.Second)
	bus.send(eventbus.Event{Type: eventbus.CIChanged, Before: passing, After: failing})
	if len(sender.calls) != 1 {
		t.Errorf("CI fail within 60s should be suppressed; got %d notifications", len(sender.calls))
	}

	// T=20s: failing → passing (within 60s — suppressed)
	clk.advance(10 * time.Second)
	bus.send(eventbus.Event{Type: eventbus.CIChanged, Before: failing, After: passing})
	if len(sender.calls) != 1 {
		t.Errorf("CI pass (second) within 60s should be suppressed; got %d notifications", len(sender.calls))
	}
}

func TestNotifierDebounce_CIPassFailPassAfter60sTwoNotifications(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	bus := &fakeBus{}
	sender := &stubSender{}
	debouncer := NewDebouncer(clk)
	n := New(bus, sender, allEnabled(), &stubDND{}, "testuser", debouncer)
	defer n.Close()

	url := "https://github.com/org/repo/pull/42"

	running := makePR("org/repo", 42, "open", "running", "testuser")
	running.URL = url
	passing := makePR("org/repo", 42, "open", "passing", "testuser")
	passing.URL = url
	failing := makePR("org/repo", 42, "open", "failing", "testuser")
	failing.URL = url

	// T=0: running → passing
	bus.send(eventbus.Event{Type: eventbus.CIChanged, Before: running, After: passing})
	if len(sender.calls) != 1 {
		t.Fatalf("expected 1 notification after first pass, got %d", len(sender.calls))
	}

	// T=10s: passing → failing (within 60s — suppressed)
	clk.advance(10 * time.Second)
	bus.send(eventbus.Event{Type: eventbus.CIChanged, Before: passing, After: failing})

	// T=65s: failing → passing (>60s since last sent — permitted)
	clk.advance(55 * time.Second) // total T=65s
	bus.send(eventbus.Event{Type: eventbus.CIChanged, Before: failing, After: passing})
	if len(sender.calls) != 2 {
		t.Errorf("expected 2 notifications (pass at 0, pass at 65s), got %d", len(sender.calls))
	}
}

// ── RealClock tests ───────────────────────────────────────────────────────────

func TestRealClock_NowIsNonZero(t *testing.T) {
	c := RealClock{}
	if c.Now().IsZero() {
		t.Error("RealClock.Now() should return a non-zero time")
	}
}
