// Package notify subscribes to DB events and dispatches macOS system
// notifications according to the configured per-event flags and DND state.
package notify

import (
	"fmt"

	"github.com/evanisnor/argh/internal/config"
	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
)

// Sender dispatches OS-level system notifications.
type Sender interface {
	Notify(title, body string) error
}

// DNDChecker reports whether Do Not Disturb is currently active.
type DNDChecker interface {
	IsDND() bool
}

// NoDND is a DNDChecker that always reports DND as inactive.
// Use it before the real DND implementation (task 32) is wired in.
type NoDND struct{}

func (NoDND) IsDND() bool { return false }

// Bus is the subset of eventbus.Bus used by the Notifier.
type Bus interface {
	Subscribe(handler func(eventbus.Event)) func()
}

// Notifier subscribes to the event bus and dispatches macOS system
// notifications. Each instance holds one bus subscription; call Close
// to unsubscribe.
type Notifier struct {
	sender    Sender
	cfg       config.NotificationsConfig
	dnd       DNDChecker
	debouncer *Debouncer
	// login is the authenticated user's GitHub login, used to distinguish
	// "my PR" events from "review requested" events.
	login string
	unsub func()
}

// New creates a Notifier, subscribes it to bus, and returns it.
// Pass a non-nil Debouncer to enable notification deduplication and
// CI-flapping collapse; pass nil to disable debouncing.
func New(bus Bus, sender Sender, cfg config.NotificationsConfig, dnd DNDChecker, login string, debouncer *Debouncer) *Notifier {
	n := &Notifier{
		sender:    sender,
		cfg:       cfg,
		dnd:       dnd,
		debouncer: debouncer,
		login:     login,
	}
	n.unsub = bus.Subscribe(n.handle)
	return n
}

// send dispatches a notification unless the debouncer suppresses it.
func (n *Notifier) send(prURL string, eventType eventbus.EventType, title, body string) {
	if n.debouncer != nil && !n.debouncer.Allow(prURL, eventType) {
		return
	}
	_ = n.sender.Notify(title, body)
}

// Close unsubscribes from the event bus.
func (n *Notifier) Close() {
	if n.unsub != nil {
		n.unsub()
	}
}

func (n *Notifier) handle(e eventbus.Event) {
	if n.dnd.IsDND() {
		return
	}
	switch e.Type {
	case eventbus.CIChanged:
		n.handleCIChanged(e)
	case eventbus.ReviewChanged:
		n.handleReviewChanged(e)
	case eventbus.PRUpdated:
		n.handlePRUpdated(e)
	case eventbus.WatchFired:
		n.handleWatchFired(e)
	}
}

// handleCIChanged fires CI pass or fail notifications on state transitions.
// No notification is sent when the CI state did not change (e.g. running→running).
func (n *Notifier) handleCIChanged(e eventbus.Event) {
	after, ok := e.After.(persistence.PullRequest)
	if !ok {
		return
	}
	before, hasBefore := e.Before.(persistence.PullRequest)

	prevState := ""
	if hasBefore {
		prevState = before.CIState
	}

	switch after.CIState {
	case "passing":
		if prevState == "passing" {
			return // no transition — already passing
		}
		if !n.cfg.CIPass {
			return
		}
		n.send(after.URL, e.Type,
			fmt.Sprintf("✓ CI Passing — %s #%d", after.Repo, after.Number),
			after.Title,
		)
	case "failing":
		if prevState == "failing" {
			return // no transition — already failing
		}
		if !n.cfg.CIFail {
			return
		}
		n.send(after.URL, e.Type,
			fmt.Sprintf("✗ CI Failing — %s #%d", after.Repo, after.Number),
			after.Title,
		)
	}
}

// handleReviewChanged fires approval or changes-requested notifications
// from ReviewChanged events (emitted when reviewer state is updated).
func (n *Notifier) handleReviewChanged(e eventbus.Event) {
	after, ok := e.After.(persistence.PullRequest)
	if !ok {
		return
	}
	before, hasBefore := e.Before.(persistence.PullRequest)

	switch after.Status {
	case "approved":
		if hasBefore && before.Status == "approved" {
			return
		}
		if !n.cfg.Approved {
			return
		}
		n.send(after.URL, e.Type,
			fmt.Sprintf("✓ Approved — %s #%d", after.Repo, after.Number),
			after.Title,
		)
	case "changes requested":
		if hasBefore && before.Status == "changes requested" {
			return
		}
		if !n.cfg.ChangesRequested {
			return
		}
		n.send(after.URL, e.Type,
			fmt.Sprintf("✗ Changes Requested — %s #%d", after.Repo, after.Number),
			after.Title,
		)
	}
}

// handlePRUpdated fires review-requested, approval, changes-requested,
// merged, and closed notifications from PRUpdated events.
func (n *Notifier) handlePRUpdated(e eventbus.Event) {
	after, ok := e.After.(persistence.PullRequest)
	if !ok {
		return
	}
	before, hasBefore := e.Before.(persistence.PullRequest)

	// New PR appearing in the review queue (not authored by the current user)
	// means a review was requested.
	if !hasBefore {
		if after.Author != n.login && n.cfg.ReviewRequested {
			n.send(after.URL, e.Type,
				fmt.Sprintf("👀 Review Requested — %s #%d", after.Repo, after.Number),
				after.Title,
			)
		}
		return
	}

	if before.Status == after.Status {
		return
	}

	switch after.Status {
	case "approved":
		if n.cfg.Approved {
			n.send(after.URL, e.Type,
				fmt.Sprintf("✓ Approved — %s #%d", after.Repo, after.Number),
				after.Title,
			)
		}
	case "changes requested":
		if n.cfg.ChangesRequested {
			n.send(after.URL, e.Type,
				fmt.Sprintf("✗ Changes Requested — %s #%d", after.Repo, after.Number),
				after.Title,
			)
		}
	case "merged":
		if n.cfg.Merged {
			n.send(after.URL, e.Type,
				fmt.Sprintf("✓ Merged — %s #%d", after.Repo, after.Number),
				after.Title,
			)
		}
	case "closed":
		if n.cfg.Merged {
			n.send(after.URL, e.Type,
				fmt.Sprintf("✗ Closed — %s #%d", after.Repo, after.Number),
				after.Title,
			)
		}
	}
}

// handleWatchFired fires a notification when a watch action is executed.
func (n *Notifier) handleWatchFired(e eventbus.Event) {
	if !n.cfg.WatchTriggered {
		return
	}
	after, ok := e.After.(persistence.Watch)
	if !ok {
		return
	}
	n.send(after.PRURL, e.Type,
		fmt.Sprintf("⚡ Watch Fired — %s #%d", after.Repo, after.PRNumber),
		fmt.Sprintf("Trigger: %s → Action: %s", after.TriggerExpr, after.ActionExpr),
	)
}
