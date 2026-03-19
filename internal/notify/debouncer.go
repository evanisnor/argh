package notify

import (
	"sync"
	"time"

	"github.com/evanisnor/argh/internal/eventbus"
)

// Clock returns the current time. Injected so tests can control time.
type Clock interface {
	Now() time.Time
}

// RealClock implements Clock using time.Now().
type RealClock struct{}

// Now returns the current wall-clock time.
func (RealClock) Now() time.Time { return time.Now() }

// dedupeKey uniquely identifies a (PR URL, event type) pair for debouncing.
type dedupeKey struct {
	prURL     string
	eventType eventbus.EventType
}

// Debouncer deduplicates and debounces notification dispatch.
//
// A 5-second window is applied per (pr_url, event_type) pair: if the same
// notification type would fire again for the same PR within 5 seconds the
// duplicate is dropped.
//
// CI events (CIChanged) use a 60-second window, which collapses
// pass→fail→pass flapping sequences into a single notification when all
// transitions occur within 60 seconds.
type Debouncer struct {
	mu       sync.Mutex
	clock    Clock
	lastSent map[dedupeKey]time.Time
}

// NewDebouncer creates a Debouncer that uses clock to determine the current time.
func NewDebouncer(clock Clock) *Debouncer {
	return &Debouncer{
		clock:    clock,
		lastSent: make(map[dedupeKey]time.Time),
	}
}

// Allow returns true if a notification for (prURL, eventType) should be sent.
// It records the current time as the last-sent timestamp when it returns true.
// When it returns false the caller must drop the notification.
func (d *Debouncer) Allow(prURL string, eventType eventbus.EventType) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	window := 5 * time.Second
	if eventType == eventbus.CIChanged {
		window = 60 * time.Second
	}

	key := dedupeKey{prURL: prURL, eventType: eventType}
	now := d.clock.Now()

	if last, ok := d.lastSent[key]; ok && now.Sub(last) < window {
		return false
	}

	d.lastSent[key] = now
	return true
}
