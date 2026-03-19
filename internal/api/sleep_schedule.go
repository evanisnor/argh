package api

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/evanisnor/argh/internal/config"
)

// Clock abstracts time.Now() for testability.
type Clock interface {
	Now() time.Time
}

// RealClock implements Clock using the system clock.
type RealClock struct{}

// Now returns the current time.
func (RealClock) Now() time.Time { return time.Now() }

// SleepScheduleChecker is the interface consumed by Poller to determine
// whether to use a reduced poll interval.
type SleepScheduleChecker interface {
	IsInSleepWindow() bool
	SleepInterval() time.Duration
	Wake()
}

// SleepSchedule evaluates configured time windows to determine whether the
// current time falls within a sleep window.
type SleepSchedule struct {
	windows       []config.ScheduleWindow
	sleepInterval time.Duration
	clock         Clock
	mu            sync.Mutex
	woken         bool
}

// NewSleepSchedule constructs a SleepSchedule from the given windows, reduced
// poll interval, and clock.
func NewSleepSchedule(windows []config.ScheduleWindow, sleepInterval time.Duration, clock Clock) *SleepSchedule {
	return &SleepSchedule{
		windows:       windows,
		sleepInterval: sleepInterval,
		clock:         clock,
	}
}

// SleepInterval returns the configured reduced poll interval.
func (s *SleepSchedule) SleepInterval() time.Duration {
	return s.sleepInterval
}

// Wake overrides the sleep schedule, restoring normal polling until the current
// sleep window ends. The next sleep window will re-engage normally.
func (s *SleepSchedule) Wake() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.woken = true
}

// IsInSleepWindow reports whether the current time falls within any configured
// sleep window. Returns false if Wake() has been called and the current sleep
// window has not yet ended.
func (s *SleepSchedule) IsInSleepWindow() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	t := s.clock.Now()
	inWin := s.inAnyWindow(t)

	if s.woken {
		if !inWin {
			// Exited the window that triggered Wake; reset for next time.
			s.woken = false
		}
		return false
	}

	return inWin
}

// StatusText returns a status bar string for display when in a sleep window.
// nextPollIn is the duration until the next poll.
func (s *SleepSchedule) StatusText(nextPollIn time.Duration) string {
	mins := int(nextPollIn.Minutes())
	if mins < 1 {
		return "💤 sleeping (next poll <1m)"
	}
	return fmt.Sprintf("💤 sleeping (next poll in %dm)", mins)
}

func (s *SleepSchedule) inAnyWindow(t time.Time) bool {
	for _, w := range s.windows {
		if inWindow(t, w) {
			return true
		}
	}
	return false
}

// inWindow reports whether t falls within the given ScheduleWindow.
func inWindow(t time.Time, w config.ScheduleWindow) bool {
	if !matchesDay(t.Weekday(), w.Days) {
		return false
	}
	if w.AllDay {
		return true
	}
	from, err := parseTimeOfDay(w.From)
	if err != nil {
		return false
	}
	to, err := parseTimeOfDay(w.To)
	if err != nil {
		return false
	}
	current := timeOfDayMinutes(t)
	if from <= to {
		return current >= from && current < to
	}
	// Spanning midnight (e.g. From=22:00, To=06:00).
	return current >= from || current < to
}

// matchesDay reports whether weekday is in the days list. An empty list matches
// every day of the week.
func matchesDay(weekday time.Weekday, days []string) bool {
	if len(days) == 0 {
		return true
	}
	name := strings.ToLower(weekday.String())
	short := name[:3]
	for _, d := range days {
		lower := strings.ToLower(d)
		if lower == name || lower == short {
			return true
		}
	}
	return false
}

// parseTimeOfDay parses an "HH:MM" string into minutes since midnight.
func parseTimeOfDay(s string) (int, error) {
	idx := strings.Index(s, ":")
	if idx < 0 {
		return 0, fmt.Errorf("invalid time %q: expected HH:MM", s)
	}
	h, err := strconv.Atoi(s[:idx])
	if err != nil {
		return 0, fmt.Errorf("invalid hour in %q: %w", s, err)
	}
	m, err := strconv.Atoi(s[idx+1:])
	if err != nil {
		return 0, fmt.Errorf("invalid minute in %q: %w", s, err)
	}
	return h*60 + m, nil
}

// timeOfDayMinutes returns the number of minutes since midnight for t.
func timeOfDayMinutes(t time.Time) int {
	return t.Hour()*60 + t.Minute()
}
