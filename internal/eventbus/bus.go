// Package eventbus provides a lightweight in-process publish/subscribe event bus.
// The persistence layer publishes events when rows change; the UI model and
// Watch Engine subscribe to receive them without polling the database.
package eventbus

import "sync"

// EventType identifies the kind of change that occurred.
type EventType string

const (
	// PRUpdated fires when a pull request row changes.
	PRUpdated EventType = "PR_UPDATED"

	// CIChanged fires when a check-run state changes.
	CIChanged EventType = "CI_CHANGED"

	// ReviewChanged fires when a reviewer state changes.
	ReviewChanged EventType = "REVIEW_CHANGED"

	// WatchFired fires when a watch action executes.
	WatchFired EventType = "WATCH_FIRED"

	// RateLimitWarning fires when the GitHub API rate limit remaining drops below 100.
	RateLimitWarning EventType = "RATE_LIMIT_WARNING"

	// SSORequired fires when a GitHub API 403 response includes an X-GitHub-SSO header.
	SSORequired EventType = "SSO_REQUIRED"
)

// Event carries the type of change plus before/after snapshots of the
// changed row. Before is nil for insert events; After is nil for delete events.
type Event struct {
	Type   EventType
	Before any
	After  any
}

// Bus is a thread-safe in-process publish/subscribe event bus.
// The zero value is not usable; call New to create a Bus.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[uint64]func(Event)
	nextID      uint64
	shutdown    bool
}

// New returns a new, empty Bus ready to accept subscriptions.
func New() *Bus {
	return &Bus{
		subscribers: make(map[uint64]func(Event)),
	}
}

// Subscribe registers handler to receive all future events published on the bus.
// It returns an unsubscribe function; calling it removes the handler so that
// subsequent Publish calls no longer deliver events to it.
// Subscribe is safe to call concurrently.
func (b *Bus) Subscribe(handler func(Event)) func() {
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	b.subscribers[id] = handler
	b.mu.Unlock()

	return func() {
		b.mu.Lock()
		delete(b.subscribers, id)
		b.mu.Unlock()
	}
}

// Publish delivers e synchronously to all currently-subscribed handlers.
// Handlers are called outside the internal lock, so they may safely call
// Subscribe or Unsubscribe without deadlocking.
// Publish is a no-op after Shutdown.
func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	if b.shutdown {
		b.mu.RUnlock()
		return
	}
	handlers := make([]func(Event), 0, len(b.subscribers))
	for _, h := range b.subscribers {
		handlers = append(handlers, h)
	}
	b.mu.RUnlock()

	for _, h := range handlers {
		h(e)
	}
}

// Shutdown marks the bus as shut down. All subsequent Publish calls are
// silently dropped. Shutdown is idempotent and safe to call concurrently.
func (b *Bus) Shutdown() {
	b.mu.Lock()
	b.shutdown = true
	b.mu.Unlock()
}
