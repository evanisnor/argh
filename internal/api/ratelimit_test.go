package api

import (
	"errors"
	"testing"
	"time"

	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
)

// ── IntervalMultiplier ────────────────────────────────────────────────────────

func TestIntervalMultiplier(t *testing.T) {
	tests := []struct {
		name      string
		remaining int
		want      int
	}{
		// > 1000 → 1
		{name: "well_above_1000", remaining: 5000, want: 1},
		{name: "just_above_1000", remaining: 1001, want: 1},
		// boundary: exactly 1000 → 2
		{name: "exactly_1000", remaining: 1000, want: 2},
		// 500–999 → 2
		{name: "mid_range_750", remaining: 750, want: 2},
		{name: "just_above_500", remaining: 501, want: 2},
		// boundary: exactly 500 → 2
		{name: "exactly_500", remaining: 500, want: 2},
		// 100–499 → 5
		{name: "mid_range_300", remaining: 300, want: 5},
		{name: "just_above_100", remaining: 101, want: 5},
		// boundary: exactly 100 → 5
		{name: "exactly_100", remaining: 100, want: 5},
		// < 100 → 0
		{name: "just_below_100", remaining: 99, want: 0},
		{name: "very_low", remaining: 10, want: 0},
		{name: "zero", remaining: 0, want: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IntervalMultiplier(tc.remaining)
			if got != tc.want {
				t.Errorf("IntervalMultiplier(%d) = %d, want %d", tc.remaining, got, tc.want)
			}
		})
	}
}

// ── stubRateLimitStore ────────────────────────────────────────────────────────

type stubRateLimitStore struct {
	upserted []persistence.RateLimit
	upsertFn func(rl persistence.RateLimit) error
	getFn    func() (persistence.RateLimit, error)
}

func newStubRateLimitStore() *stubRateLimitStore {
	return &stubRateLimitStore{
		upsertFn: func(rl persistence.RateLimit) error { return nil },
		getFn:    func() (persistence.RateLimit, error) { return persistence.RateLimit{}, nil },
	}
}

func (s *stubRateLimitStore) UpsertRateLimit(rl persistence.RateLimit) error {
	s.upserted = append(s.upserted, rl)
	return s.upsertFn(rl)
}

func (s *stubRateLimitStore) GetRateLimit() (persistence.RateLimit, error) {
	return s.getFn()
}

// ── TrackResponse ─────────────────────────────────────────────────────────────

func TestRateLimitTracker_TrackResponse_PersistsValues(t *testing.T) {
	store := newStubRateLimitStore()
	bus := &StubPublisher{}
	tracker := NewRateLimitTracker(store, bus)

	reset := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	if err := tracker.TrackResponse(3847, reset); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(store.upserted) != 1 {
		t.Fatalf("expected 1 upsert, got %d", len(store.upserted))
	}
	got := store.upserted[0]
	if got.Remaining != 3847 {
		t.Errorf("Remaining: got %d, want 3847", got.Remaining)
	}
	if !got.ResetAt.Equal(reset) {
		t.Errorf("ResetAt: got %v, want %v", got.ResetAt, reset)
	}
}

func TestRateLimitTracker_TrackResponse_MultipleCalls_AllPersisted(t *testing.T) {
	store := newStubRateLimitStore()
	bus := &StubPublisher{}
	tracker := NewRateLimitTracker(store, bus)

	reset := time.Now().UTC()
	if err := tracker.TrackResponse(4000, reset); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := tracker.TrackResponse(3500, reset); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(store.upserted) != 2 {
		t.Fatalf("expected 2 upserts, got %d", len(store.upserted))
	}
	if store.upserted[0].Remaining != 4000 {
		t.Errorf("first call Remaining: got %d, want 4000", store.upserted[0].Remaining)
	}
	if store.upserted[1].Remaining != 3500 {
		t.Errorf("second call Remaining: got %d, want 3500", store.upserted[1].Remaining)
	}
}

func TestRateLimitTracker_TrackResponse_WarningEmitted_WhenBelow100(t *testing.T) {
	tests := []struct {
		name      string
		remaining int
		wantEvent bool
	}{
		{name: "remaining_99_emits_warning", remaining: 99, wantEvent: true},
		{name: "remaining_50_emits_warning", remaining: 50, wantEvent: true},
		{name: "remaining_0_emits_warning", remaining: 0, wantEvent: true},
		{name: "remaining_100_no_warning", remaining: 100, wantEvent: false},
		{name: "remaining_500_no_warning", remaining: 500, wantEvent: false},
		{name: "remaining_5000_no_warning", remaining: 5000, wantEvent: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newStubRateLimitStore()
			bus := &StubPublisher{}
			tracker := NewRateLimitTracker(store, bus)

			if err := tracker.TrackResponse(tc.remaining, time.Now()); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			warningEmitted := false
			for _, e := range bus.Events {
				if e.Type == eventbus.RateLimitWarning {
					warningEmitted = true
					break
				}
			}

			if warningEmitted != tc.wantEvent {
				t.Errorf("remaining=%d: warningEmitted=%v, want %v", tc.remaining, warningEmitted, tc.wantEvent)
			}
		})
	}
}

func TestRateLimitTracker_TrackResponse_WarningEventCarriesState(t *testing.T) {
	store := newStubRateLimitStore()
	bus := &StubPublisher{}
	tracker := NewRateLimitTracker(store, bus)

	reset := time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)
	if err := tracker.TrackResponse(42, reset); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(bus.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(bus.Events))
	}
	e := bus.Events[0]
	if e.Type != eventbus.RateLimitWarning {
		t.Errorf("event type: got %q, want %q", e.Type, eventbus.RateLimitWarning)
	}
	state, ok := e.After.(RateLimitState)
	if !ok {
		t.Fatalf("event After is not RateLimitState: %T", e.After)
	}
	if state.Remaining != 42 {
		t.Errorf("state.Remaining: got %d, want 42", state.Remaining)
	}
}

func TestRateLimitTracker_TrackResponse_StoreError_Propagated(t *testing.T) {
	store := newStubRateLimitStore()
	store.upsertFn = func(rl persistence.RateLimit) error {
		return errors.New("db write failed")
	}
	bus := &StubPublisher{}
	tracker := NewRateLimitTracker(store, bus)

	err := tracker.TrackResponse(5000, time.Now())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── CurrentState ──────────────────────────────────────────────────────────────

func TestRateLimitTracker_CurrentState_DefaultState(t *testing.T) {
	store := newStubRateLimitStore()
	bus := &StubPublisher{}
	tracker := NewRateLimitTracker(store, bus)

	state := tracker.CurrentState()
	if state.Limit != 5000 {
		t.Errorf("default Limit: got %d, want 5000", state.Limit)
	}
	if state.Remaining != 0 {
		t.Errorf("default Remaining: got %d, want 0", state.Remaining)
	}
}

func TestRateLimitTracker_CurrentState_ReflectsLastTrack(t *testing.T) {
	store := newStubRateLimitStore()
	bus := &StubPublisher{}
	tracker := NewRateLimitTracker(store, bus)

	reset := time.Date(2026, 3, 19, 15, 0, 0, 0, time.UTC)
	if err := tracker.TrackResponse(2500, reset); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := tracker.CurrentState()
	if state.Remaining != 2500 {
		t.Errorf("Remaining: got %d, want 2500", state.Remaining)
	}
	if !state.Reset.Equal(reset) {
		t.Errorf("Reset: got %v, want %v", state.Reset, reset)
	}
	if state.Limit != 5000 {
		t.Errorf("Limit: got %d, want 5000", state.Limit)
	}
}

// ── StatusBar ─────────────────────────────────────────────────────────────────

func TestRateLimitTracker_StatusBar(t *testing.T) {
	tests := []struct {
		name      string
		remaining int
		want      string
	}{
		// ●●●● when remaining/limit >= 0.75 (3750/5000)
		{name: "full_dots_at_5000", remaining: 5000, want: "API ●●●● 5,000/5,000"},
		{name: "full_dots_at_3750", remaining: 3750, want: "API ●●●● 3,750/5,000"},
		// ●●●○ when remaining/limit >= 0.50 (2500/5000) and < 0.75 (3750/5000)
		{name: "three_dots_at_3749", remaining: 3749, want: "API ●●●○ 3,749/5,000"},
		{name: "three_dots_at_2500", remaining: 2500, want: "API ●●●○ 2,500/5,000"},
		// ●●○○ when remaining/limit >= 0.25 (1250/5000)
		{name: "two_dots_at_2499", remaining: 2499, want: "API ●●○○ 2,499/5,000"},
		{name: "two_dots_at_1250", remaining: 1250, want: "API ●●○○ 1,250/5,000"},
		// ●○○○ when remaining > 0 but < 0.25
		{name: "one_dot_at_1249", remaining: 1249, want: "API ●○○○ 1,249/5,000"},
		{name: "one_dot_at_100", remaining: 100, want: "API ●○○○ 100/5,000"},
		{name: "one_dot_at_1", remaining: 1, want: "API ●○○○ 1/5,000"},
		// ○○○○ when remaining is 0
		{name: "no_dots_at_0", remaining: 0, want: "API ○○○○ 0/5,000"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newStubRateLimitStore()
			bus := &StubPublisher{}
			tracker := NewRateLimitTracker(store, bus)
			// TrackResponse may emit warning for low values; ignore error and event here.
			_ = tracker.TrackResponse(tc.remaining, time.Now())

			got := tracker.StatusBar()
			if got != tc.want {
				t.Errorf("StatusBar() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRateLimitTracker_StatusBar_DefaultBeforeAnyTrack(t *testing.T) {
	store := newStubRateLimitStore()
	bus := &StubPublisher{}
	tracker := NewRateLimitTracker(store, bus)

	// Before any TrackResponse call: Remaining=0, Limit=5000 → ○○○○
	got := tracker.StatusBar()
	want := "API ○○○○ 0/5,000"
	if got != want {
		t.Errorf("StatusBar() = %q, want %q", got, want)
	}
}

// ── rateLimitDots (internal) ──────────────────────────────────────────────────

func TestRateLimitDots_ZeroLimit(t *testing.T) {
	// limit <= 0 must return all-empty dots without dividing by zero.
	got := rateLimitDots(0, 0)
	if got != "○○○○" {
		t.Errorf("rateLimitDots(0, 0) = %q, want %q", got, "○○○○")
	}
}

// ── formatInt (internal) ──────────────────────────────────────────────────────

func TestFormatInt_Negative(t *testing.T) {
	got := formatInt(-1500)
	want := "-1,500"
	if got != want {
		t.Errorf("formatInt(-1500) = %q, want %q", got, want)
	}
}
