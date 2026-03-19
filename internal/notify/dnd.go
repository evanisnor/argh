package notify

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/evanisnor/argh/internal/config"
)

// DNDManager implements DNDChecker (for Notifier) and provides SetDND/Wake
// (implementing ui.DNDController) and Toggle (for the D key binding).
//
// DND is active when any of these are true:
//   - Manual DND is on (set via SetDND or Toggle), and its timer has not expired.
//   - The current time falls within a configured schedule window.
type DNDManager struct {
	windows   []config.ScheduleWindow
	clock     Clock

	mu        sync.Mutex
	manual    bool
	expiresAt time.Time // zero = no expiry
}

// NewDNDManager creates a DNDManager with the given schedule windows and clock.
// Pass nil windows or an empty slice to disable scheduled DND.
func NewDNDManager(windows []config.ScheduleWindow, clock Clock) *DNDManager {
	return &DNDManager{
		windows: windows,
		clock:   clock,
	}
}

// IsDND reports whether DND is currently active (manual/timed or scheduled).
// If a timed DND has expired, the manual flag is cleared before checking.
func (d *DNDManager) IsDND() bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := d.clock.Now()

	if d.manual {
		if !d.expiresAt.IsZero() && now.After(d.expiresAt) {
			// Timed DND has expired.
			d.manual = false
			d.expiresAt = time.Time{}
		} else {
			return true
		}
	}

	return d.inAnyWindow(now)
}

// Toggle flips the manual DND state. If DND is currently on (manual or
// scheduled), calling Toggle disables the manual flag. If DND is off, Toggle
// enables it with no expiry.
func (d *DNDManager) Toggle() {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := d.clock.Now()

	// Check if manual DND is effectively active (timer not yet expired).
	manualActive := d.manual && (d.expiresAt.IsZero() || !now.After(d.expiresAt))

	if manualActive {
		d.manual = false
		d.expiresAt = time.Time{}
	} else {
		d.manual = true
		d.expiresAt = time.Time{}
	}
}

// SetDND activates manual DND for the given duration.
// If manual DND is already active (timer not expired), SetDND acts as a toggle
// (disabling it), which makes :dnd a true toggle command.
// If dur is zero, DND is enabled with no expiry.
func (d *DNDManager) SetDND(dur time.Duration) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := d.clock.Now()
	manualActive := d.manual && (d.expiresAt.IsZero() || !now.After(d.expiresAt))

	if manualActive {
		// Toggle off.
		d.manual = false
		d.expiresAt = time.Time{}
		return nil
	}

	d.manual = true
	if dur > 0 {
		d.expiresAt = now.Add(dur)
	} else {
		d.expiresAt = time.Time{}
	}
	return nil
}

// Wake disables manual DND immediately, regardless of any active timer.
func (d *DNDManager) Wake() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.manual = false
	d.expiresAt = time.Time{}
	return nil
}

// inAnyWindow reports whether t falls within any configured schedule window.
// Must be called with d.mu held.
func (d *DNDManager) inAnyWindow(t time.Time) bool {
	for _, w := range d.windows {
		if dndInWindow(t, w) {
			return true
		}
	}
	return false
}

// dndInWindow reports whether t falls within the given ScheduleWindow.
func dndInWindow(t time.Time, w config.ScheduleWindow) bool {
	if !dndMatchesDay(t.Weekday(), w.Days) {
		return false
	}
	if w.AllDay {
		return true
	}
	from, err := dndParseTimeOfDay(w.From)
	if err != nil {
		return false
	}
	to, err := dndParseTimeOfDay(w.To)
	if err != nil {
		return false
	}
	current := t.Hour()*60 + t.Minute()
	if from <= to {
		return current >= from && current < to
	}
	// Spanning midnight (e.g. From=22:00, To=06:00).
	return current >= from || current < to
}

// dndMatchesDay reports whether weekday is in the days list.
// An empty list matches every day of the week.
func dndMatchesDay(weekday time.Weekday, days []string) bool {
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

// dndParseTimeOfDay parses an "HH:MM" string into minutes since midnight.
func dndParseTimeOfDay(s string) (int, error) {
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
