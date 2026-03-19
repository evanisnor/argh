package api

import (
	"testing"
	"time"

	"github.com/evanisnor/argh/internal/config"
)

// Monday 2024-01-01 is used as the reference date throughout these tests.
// time.Date(2024, 1, 1, ...) is a Monday.

func TestRealClock_Now(t *testing.T) {
	rc := RealClock{}
	before := time.Now()
	got := rc.Now()
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Errorf("RealClock.Now() = %v, want between %v and %v", got, before, after)
	}
}

func TestSleepSchedule_IsInSleepWindow_TableDriven(t *testing.T) {
	tests := []struct {
		name    string
		windows []config.ScheduleWindow
		now     time.Time
		want    bool
	}{
		// ── Inside / outside ──────────────────────────────────────────────────
		{
			name: "inside window",
			windows: []config.ScheduleWindow{
				{Days: []string{"Monday"}, From: "09:00", To: "17:00"},
			},
			now:  time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC), // Mon 10:00
			want: true,
		},
		{
			name: "outside window — wrong time",
			windows: []config.ScheduleWindow{
				{Days: []string{"Monday"}, From: "09:00", To: "17:00"},
			},
			now:  time.Date(2024, 1, 1, 18, 0, 0, 0, time.UTC), // Mon 18:00
			want: false,
		},
		{
			name: "outside window — wrong day",
			windows: []config.ScheduleWindow{
				{Days: []string{"Tuesday"}, From: "09:00", To: "17:00"},
			},
			now:  time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC), // Monday
			want: false,
		},
		{
			name:    "no windows configured",
			windows: []config.ScheduleWindow{},
			now:     time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			want:    false,
		},

		// ── Boundaries ────────────────────────────────────────────────────────
		{
			name: "boundary start — exactly at From (inclusive)",
			windows: []config.ScheduleWindow{
				{From: "09:00", To: "17:00"},
			},
			now:  time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC),
			want: true,
		},
		{
			name: "boundary end — exactly at To (exclusive)",
			windows: []config.ScheduleWindow{
				{From: "09:00", To: "17:00"},
			},
			now:  time.Date(2024, 1, 1, 17, 0, 0, 0, time.UTC),
			want: false,
		},
		{
			name: "boundary before From",
			windows: []config.ScheduleWindow{
				{From: "09:00", To: "17:00"},
			},
			now:  time.Date(2024, 1, 1, 8, 59, 0, 0, time.UTC),
			want: false,
		},

		// ── AllDay ────────────────────────────────────────────────────────────
		{
			name: "all_day window matches any time",
			windows: []config.ScheduleWindow{
				{Days: []string{"Monday"}, AllDay: true},
			},
			now:  time.Date(2024, 1, 1, 3, 0, 0, 0, time.UTC), // Mon 03:00
			want: true,
		},
		{
			name: "all_day window wrong day",
			windows: []config.ScheduleWindow{
				{Days: []string{"Tuesday"}, AllDay: true},
			},
			now:  time.Date(2024, 1, 1, 3, 0, 0, 0, time.UTC), // Monday
			want: false,
		},

		// ── Multi-day spanning midnight ───────────────────────────────────────
		{
			name: "spanning midnight — in first half (after From)",
			windows: []config.ScheduleWindow{
				{From: "22:00", To: "06:00"},
			},
			now:  time.Date(2024, 1, 1, 23, 0, 0, 0, time.UTC),
			want: true,
		},
		{
			name: "spanning midnight — in second half (before To)",
			windows: []config.ScheduleWindow{
				{From: "22:00", To: "06:00"},
			},
			now:  time.Date(2024, 1, 1, 2, 0, 0, 0, time.UTC),
			want: true,
		},
		{
			name: "spanning midnight — exactly at From (inclusive)",
			windows: []config.ScheduleWindow{
				{From: "22:00", To: "06:00"},
			},
			now:  time.Date(2024, 1, 1, 22, 0, 0, 0, time.UTC),
			want: true,
		},
		{
			name: "spanning midnight — exactly at To (exclusive)",
			windows: []config.ScheduleWindow{
				{From: "22:00", To: "06:00"},
			},
			now:  time.Date(2024, 1, 1, 6, 0, 0, 0, time.UTC),
			want: false,
		},
		{
			name: "spanning midnight — outside (midday)",
			windows: []config.ScheduleWindow{
				{From: "22:00", To: "06:00"},
			},
			now:  time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			want: false,
		},

		// ── Day matching variants ─────────────────────────────────────────────
		{
			name: "day matching — short name (Mon)",
			windows: []config.ScheduleWindow{
				{Days: []string{"Mon"}, From: "09:00", To: "17:00"},
			},
			now:  time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC), // Monday
			want: true,
		},
		{
			name: "day matching — lowercase full name",
			windows: []config.ScheduleWindow{
				{Days: []string{"monday"}, From: "09:00", To: "17:00"},
			},
			now:  time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC), // Monday
			want: true,
		},
		{
			name: "day matching — empty days list matches every day",
			windows: []config.ScheduleWindow{
				{Days: []string{}, From: "09:00", To: "17:00"},
			},
			now:  time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC), // Monday
			want: true,
		},
		{
			name: "day matching — multiple days, match second entry",
			windows: []config.ScheduleWindow{
				{Days: []string{"Tuesday", "Wednesday", "Monday"}, From: "09:00", To: "17:00"},
			},
			now:  time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC), // Monday → third entry
			want: true,
		},

		// ── Invalid time strings ──────────────────────────────────────────────
		{
			name: "invalid From — no colon",
			windows: []config.ScheduleWindow{
				{From: "0900", To: "17:00"},
			},
			now:  time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			want: false,
		},
		{
			name: "invalid To — no colon",
			windows: []config.ScheduleWindow{
				{From: "09:00", To: "1700"},
			},
			now:  time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			want: false,
		},
		{
			name: "invalid From — bad hour",
			windows: []config.ScheduleWindow{
				{From: "xx:00", To: "17:00"},
			},
			now:  time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			want: false,
		},
		{
			name: "invalid To — bad minute",
			windows: []config.ScheduleWindow{
				{From: "09:00", To: "17:xx"},
			},
			now:  time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := NewFakeClock(tt.now)
			s := NewSleepSchedule(tt.windows, 5*time.Minute, clock)
			got := s.IsInSleepWindow()
			if got != tt.want {
				t.Errorf("IsInSleepWindow() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSleepSchedule_Wake_OverridesActiveWindow(t *testing.T) {
	windows := []config.ScheduleWindow{{From: "09:00", To: "17:00"}}
	clock := NewFakeClock(time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)) // inside window
	s := NewSleepSchedule(windows, 5*time.Minute, clock)

	if !s.IsInSleepWindow() {
		t.Fatal("expected IsInSleepWindow() = true before Wake")
	}

	s.Wake()

	if s.IsInSleepWindow() {
		t.Fatal("expected IsInSleepWindow() = false immediately after Wake")
	}
}

func TestSleepSchedule_Wake_ResetsAfterWindowEnds(t *testing.T) {
	windows := []config.ScheduleWindow{{From: "09:00", To: "17:00"}}
	clock := NewFakeClock(time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)) // inside window
	s := NewSleepSchedule(windows, 5*time.Minute, clock)

	s.Wake()

	// Still inside window — woken overrides.
	if s.IsInSleepWindow() {
		t.Fatal("expected false while woken and inside window")
	}

	// Advance clock outside the window.
	clock.Set(time.Date(2024, 1, 1, 18, 0, 0, 0, time.UTC))

	// Exiting the window resets woken=false.
	if s.IsInSleepWindow() {
		t.Fatal("expected false outside window")
	}

	// Return inside the window — woken flag has been reset, so window is active again.
	clock.Set(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	if !s.IsInSleepWindow() {
		t.Fatal("expected true after woken flag reset and back inside window")
	}
}

func TestSleepSchedule_SleepInterval(t *testing.T) {
	s := NewSleepSchedule(nil, 7*time.Minute, NewFakeClock(time.Now()))
	if got := s.SleepInterval(); got != 7*time.Minute {
		t.Errorf("SleepInterval() = %v, want 7m", got)
	}
}

func TestSleepSchedule_StatusText(t *testing.T) {
	s := NewSleepSchedule(nil, 5*time.Minute, NewFakeClock(time.Now()))

	tests := []struct {
		nextPollIn time.Duration
		want       string
	}{
		{0, "💤 sleeping (next poll <1m)"},
		{30 * time.Second, "💤 sleeping (next poll <1m)"},
		{59 * time.Second, "💤 sleeping (next poll <1m)"},
		{time.Minute, "💤 sleeping (next poll in 1m)"},
		{5 * time.Minute, "💤 sleeping (next poll in 5m)"},
		{10 * time.Minute, "💤 sleeping (next poll in 10m)"},
	}

	for _, tt := range tests {
		got := s.StatusText(tt.nextPollIn)
		if got != tt.want {
			t.Errorf("StatusText(%v) = %q, want %q", tt.nextPollIn, got, tt.want)
		}
	}
}
