package api

import (
	"fmt"
	"time"

	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
)

// RateLimitStore is the persistence interface required by RateLimitTracker.
type RateLimitStore interface {
	UpsertRateLimit(rl persistence.RateLimit) error
	GetRateLimit() (persistence.RateLimit, error)
}

// RateLimitState holds the current known GitHub API rate limit state.
type RateLimitState struct {
	Remaining int
	Limit     int
	Reset     time.Time
}

// githubRateLimit is the standard GitHub GraphQL API rate limit.
const githubRateLimit = 5000

// IntervalMultiplier returns the polling interval multiplier based on remaining
// API quota:
//
//	remaining > 1000 → 1  (normal)
//	500–999          → 2  (half speed)
//	100–499          → 5  (one-fifth speed)
//	< 100            → 0  (pause)
func IntervalMultiplier(remaining int) int {
	switch {
	case remaining > 1000:
		return 1
	case remaining >= 500:
		return 2
	case remaining >= 100:
		return 5
	default:
		return 0
	}
}

// RateLimitTracker persists rate limit state and emits warnings when quota is
// critically low.
type RateLimitTracker struct {
	store RateLimitStore
	bus   Publisher
	state RateLimitState
}

// NewRateLimitTracker returns a new RateLimitTracker.
func NewRateLimitTracker(store RateLimitStore, bus Publisher) *RateLimitTracker {
	return &RateLimitTracker{
		store: store,
		bus:   bus,
		state: RateLimitState{Limit: githubRateLimit},
	}
}

// TrackResponse records a rate limit observation: it persists the values to the
// DB and emits a RateLimitWarning event when remaining drops below 100.
func (t *RateLimitTracker) TrackResponse(remaining int, reset time.Time) error {
	t.state.Remaining = remaining
	t.state.Reset = reset

	if err := t.store.UpsertRateLimit(persistence.RateLimit{
		Remaining: remaining,
		ResetAt:   reset,
	}); err != nil {
		return fmt.Errorf("persisting rate limit: %w", err)
	}

	if remaining < 100 {
		t.bus.Publish(eventbus.Event{
			Type:  eventbus.RateLimitWarning,
			After: t.state,
		})
	}

	return nil
}

// CurrentState returns the most recently tracked rate limit state.
func (t *RateLimitTracker) CurrentState() RateLimitState {
	return t.state
}

// StatusBar returns a formatted status string suitable for display in the
// status bar, e.g. "API ●●●○ 3,847/5,000".
func (t *RateLimitTracker) StatusBar() string {
	dots := rateLimitDots(t.state.Remaining, t.state.Limit)
	return fmt.Sprintf("API %s %s/%s", dots, formatInt(t.state.Remaining), formatInt(t.state.Limit))
}

// rateLimitDots returns a 4-dot indicator string proportional to remaining quota.
// All four dots filled at ≥75%, three at ≥50%, two at ≥25%, one below that.
func rateLimitDots(remaining, limit int) string {
	if limit <= 0 {
		return "○○○○"
	}
	ratio := float64(remaining) / float64(limit)
	switch {
	case ratio >= 0.75:
		return "●●●●"
	case ratio >= 0.50:
		return "●●●○"
	case ratio >= 0.25:
		return "●●○○"
	case ratio > 0:
		return "●○○○"
	default:
		return "○○○○"
	}
}

// formatInt formats an integer with comma thousand separators.
func formatInt(n int) string {
	if n < 0 {
		return "-" + formatInt(-n)
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return formatInt(n/1000) + "," + fmt.Sprintf("%03d", n%1000)
}
