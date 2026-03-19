package notify

import (
	"sync"
	"testing"
	"time"

	"github.com/evanisnor/argh/internal/config"
)

// ── fake clock ────────────────────────────────────────────────────────────────

// dndTestClock is a controllable Clock for DND tests. Use Set to advance time.
type dndTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func newDNDTestClock(t time.Time) *dndTestClock {
	return &dndTestClock{now: t}
}

func (f *dndTestClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *dndTestClock) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t
}

// base time used throughout tests: Monday 2024-01-01 12:00 UTC
var dndT0 = time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

// ── IsDND — no windows, no manual ─────────────────────────────────────────────

func TestDNDManager_IsDND_InactiveByDefault(t *testing.T) {
	d := NewDNDManager(nil, newDNDTestClock(dndT0))
	if d.IsDND() {
		t.Error("IsDND() should be false by default")
	}
}

// ── Toggle ────────────────────────────────────────────────────────────────────

func TestDNDManager_Toggle_EnablesThenDisables(t *testing.T) {
	d := NewDNDManager(nil, newDNDTestClock(dndT0))

	d.Toggle()
	if !d.IsDND() {
		t.Error("IsDND() should be true after first Toggle()")
	}

	d.Toggle()
	if d.IsDND() {
		t.Error("IsDND() should be false after second Toggle()")
	}
}

func TestDNDManager_Toggle_ClearsPreviousTimer(t *testing.T) {
	clk := newDNDTestClock(dndT0)
	d := NewDNDManager(nil, clk)

	// Enable via SetDND with a 1h timer.
	if err := d.SetDND(time.Hour); err != nil {
		t.Fatalf("SetDND: %v", err)
	}
	if !d.IsDND() {
		t.Fatal("IsDND() should be true after SetDND")
	}

	// Toggle off.
	d.Toggle()
	if d.IsDND() {
		t.Error("IsDND() should be false after Toggle() when active")
	}

	// Toggle back on — no timer this time.
	d.Toggle()
	if !d.IsDND() {
		t.Error("IsDND() should be true after Toggle() when inactive")
	}

	// Advance far into the future — should still be active (no expiry was set by Toggle).
	clk.Set(dndT0.Add(100 * time.Hour))
	if !d.IsDND() {
		t.Error("IsDND() should still be true (Toggle sets no expiry)")
	}
}

// ── SetDND ────────────────────────────────────────────────────────────────────

func TestDNDManager_SetDND_ActivatesForDuration(t *testing.T) {
	clk := newDNDTestClock(dndT0)
	d := NewDNDManager(nil, clk)

	if err := d.SetDND(2 * time.Hour); err != nil {
		t.Fatalf("SetDND: %v", err)
	}
	if !d.IsDND() {
		t.Error("IsDND() should be true after SetDND")
	}

	// Just before expiry — still active.
	clk.Set(dndT0.Add(2*time.Hour - time.Second))
	if !d.IsDND() {
		t.Error("IsDND() should be true just before expiry")
	}

	// After expiry — inactive.
	clk.Set(dndT0.Add(2*time.Hour + time.Second))
	if d.IsDND() {
		t.Error("IsDND() should be false after timer expires")
	}
}

func TestDNDManager_SetDND_ZeroDuration_NoExpiry(t *testing.T) {
	clk := newDNDTestClock(dndT0)
	d := NewDNDManager(nil, clk)

	if err := d.SetDND(0); err != nil {
		t.Fatalf("SetDND(0): %v", err)
	}
	if !d.IsDND() {
		t.Error("IsDND() should be true after SetDND(0)")
	}

	// Advance far into the future — should still be active.
	clk.Set(dndT0.Add(1000 * time.Hour))
	if !d.IsDND() {
		t.Error("IsDND() should still be true with no expiry")
	}
}

func TestDNDManager_SetDND_TogglesOffWhenAlreadyActive(t *testing.T) {
	clk := newDNDTestClock(dndT0)
	d := NewDNDManager(nil, clk)

	// First :dnd → enable.
	if err := d.SetDND(30 * time.Minute); err != nil {
		t.Fatalf("SetDND: %v", err)
	}
	if !d.IsDND() {
		t.Error("IsDND() should be true after first SetDND")
	}

	// Second :dnd → toggle off.
	if err := d.SetDND(30 * time.Minute); err != nil {
		t.Fatalf("SetDND (second call): %v", err)
	}
	if d.IsDND() {
		t.Error("IsDND() should be false after second SetDND (toggle off)")
	}
}

func TestDNDManager_SetDND_ReEnablesAfterExpiry(t *testing.T) {
	clk := newDNDTestClock(dndT0)
	d := NewDNDManager(nil, clk)

	if err := d.SetDND(time.Hour); err != nil {
		t.Fatalf("SetDND: %v", err)
	}

	// Advance past expiry.
	clk.Set(dndT0.Add(2 * time.Hour))
	if d.IsDND() {
		t.Error("IsDND() should be false after expiry")
	}

	// Call SetDND again — should enable (not "toggle off" since it expired).
	if err := d.SetDND(time.Hour); err != nil {
		t.Fatalf("SetDND (after expiry): %v", err)
	}
	if !d.IsDND() {
		t.Error("IsDND() should be true after re-enabling via SetDND")
	}
}

// ── Wake ─────────────────────────────────────────────────────────────────────

func TestDNDManager_Wake_DisablesManualDND(t *testing.T) {
	d := NewDNDManager(nil, newDNDTestClock(dndT0))

	d.Toggle()
	if !d.IsDND() {
		t.Fatal("IsDND() should be true before Wake")
	}

	if err := d.Wake(); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	if d.IsDND() {
		t.Error("IsDND() should be false after Wake")
	}
}

func TestDNDManager_Wake_ClearsTimer(t *testing.T) {
	clk := newDNDTestClock(dndT0)
	d := NewDNDManager(nil, clk)

	if err := d.SetDND(time.Hour); err != nil {
		t.Fatalf("SetDND: %v", err)
	}
	if err := d.Wake(); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	if d.IsDND() {
		t.Error("IsDND() should be false immediately after Wake, even within timer")
	}
}

func TestDNDManager_Wake_NoOpWhenAlreadyInactive(t *testing.T) {
	d := NewDNDManager(nil, newDNDTestClock(dndT0))
	if err := d.Wake(); err != nil {
		t.Errorf("Wake on inactive DND should return nil, got %v", err)
	}
	if d.IsDND() {
		t.Error("IsDND() should remain false after Wake on inactive DND")
	}
}

// ── Scheduled windows ─────────────────────────────────────────────────────────

func TestDNDManager_ScheduledWindow_ActiveDuringWindow(t *testing.T) {
	windows := []config.ScheduleWindow{
		{Days: []string{"Monday"}, From: "09:00", To: "17:00"},
	}
	clk := newDNDTestClock(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)) // Mon 12:00
	d := NewDNDManager(windows, clk)
	if !d.IsDND() {
		t.Error("IsDND() should be true inside scheduled window")
	}
}

func TestDNDManager_ScheduledWindow_InactiveOutsideWindow(t *testing.T) {
	windows := []config.ScheduleWindow{
		{Days: []string{"Monday"}, From: "09:00", To: "17:00"},
	}
	clk := newDNDTestClock(time.Date(2024, 1, 1, 18, 0, 0, 0, time.UTC)) // Mon 18:00
	d := NewDNDManager(windows, clk)
	if d.IsDND() {
		t.Error("IsDND() should be false outside scheduled window")
	}
}

func TestDNDManager_ScheduledWindow_EndsAutomatically(t *testing.T) {
	windows := []config.ScheduleWindow{
		{Days: []string{"Monday"}, From: "09:00", To: "17:00"},
	}
	clk := newDNDTestClock(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)) // inside
	d := NewDNDManager(windows, clk)
	if !d.IsDND() {
		t.Fatal("IsDND() should be true inside window")
	}

	clk.Set(time.Date(2024, 1, 1, 18, 0, 0, 0, time.UTC)) // outside
	if d.IsDND() {
		t.Error("IsDND() should be false after window ends")
	}
}

func TestDNDManager_ScheduledWindow_AllDay(t *testing.T) {
	windows := []config.ScheduleWindow{
		{Days: []string{"Monday"}, AllDay: true},
	}
	clk := newDNDTestClock(time.Date(2024, 1, 1, 3, 0, 0, 0, time.UTC)) // Mon 03:00
	d := NewDNDManager(windows, clk)
	if !d.IsDND() {
		t.Error("IsDND() should be true during all-day window")
	}
}

func TestDNDManager_ScheduledWindow_SpansMidnight(t *testing.T) {
	windows := []config.ScheduleWindow{
		{From: "22:00", To: "06:00"},
	}
	tests := []struct {
		name string
		t    time.Time
		want bool
	}{
		{"23:00 in window", time.Date(2024, 1, 1, 23, 0, 0, 0, time.UTC), true},
		{"02:00 in window", time.Date(2024, 1, 1, 2, 0, 0, 0, time.UTC), true},
		{"12:00 outside window", time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewDNDManager(windows, newDNDTestClock(tt.t))
			if got := d.IsDND(); got != tt.want {
				t.Errorf("IsDND() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDNDManager_ManualAndScheduledBothActive(t *testing.T) {
	windows := []config.ScheduleWindow{
		{From: "09:00", To: "17:00"},
	}
	clk := newDNDTestClock(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))
	d := NewDNDManager(windows, clk)

	// Both scheduled and manual are on.
	d.Toggle()
	if !d.IsDND() {
		t.Error("IsDND() should be true with both manual and scheduled")
	}

	// Wake only turns off manual; scheduled still active.
	if err := d.Wake(); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	if !d.IsDND() {
		t.Error("IsDND() should still be true (scheduled window still active after Wake)")
	}
}

// ── dndInWindow helper ────────────────────────────────────────────────────────

func TestDndInWindow_WrongDay(t *testing.T) {
	w := config.ScheduleWindow{Days: []string{"Tuesday"}, From: "09:00", To: "17:00"}
	mon := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC) // Monday
	if dndInWindow(mon, w) {
		t.Error("dndInWindow should be false for wrong day")
	}
}

func TestDndInWindow_InvalidFrom(t *testing.T) {
	w := config.ScheduleWindow{From: "0900", To: "17:00"}
	got := dndInWindow(dndT0, w)
	if got {
		t.Error("dndInWindow with invalid From should return false")
	}
}

func TestDndInWindow_InvalidTo(t *testing.T) {
	w := config.ScheduleWindow{From: "09:00", To: "1700"}
	got := dndInWindow(dndT0, w)
	if got {
		t.Error("dndInWindow with invalid To should return false")
	}
}

func TestDndInWindow_BoundaryStart(t *testing.T) {
	w := config.ScheduleWindow{From: "09:00", To: "17:00"}
	// Exactly at From — inclusive.
	at := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)
	if !dndInWindow(at, w) {
		t.Error("dndInWindow should be true exactly at From (inclusive)")
	}
}

func TestDndInWindow_BoundaryEnd(t *testing.T) {
	w := config.ScheduleWindow{From: "09:00", To: "17:00"}
	// Exactly at To — exclusive.
	at := time.Date(2024, 1, 1, 17, 0, 0, 0, time.UTC)
	if dndInWindow(at, w) {
		t.Error("dndInWindow should be false exactly at To (exclusive)")
	}
}

func TestDndMatchesDay_EmptyListMatchesAll(t *testing.T) {
	if !dndMatchesDay(time.Monday, nil) {
		t.Error("empty days list should match any weekday")
	}
}

func TestDndMatchesDay_ShortName(t *testing.T) {
	if !dndMatchesDay(time.Monday, []string{"Mon"}) {
		t.Error("short day name 'Mon' should match Monday")
	}
}

func TestDndMatchesDay_NoMatch(t *testing.T) {
	if dndMatchesDay(time.Tuesday, []string{"Monday"}) {
		t.Error("Tuesday should not match Monday")
	}
}

func TestDndParseTimeOfDay_InvalidNoColon(t *testing.T) {
	_, err := dndParseTimeOfDay("0900")
	if err == nil {
		t.Error("parseTimeOfDay should return error for missing colon")
	}
}

func TestDndParseTimeOfDay_InvalidHour(t *testing.T) {
	_, err := dndParseTimeOfDay("xx:00")
	if err == nil {
		t.Error("parseTimeOfDay should return error for non-numeric hour")
	}
}

func TestDndParseTimeOfDay_InvalidMinute(t *testing.T) {
	_, err := dndParseTimeOfDay("09:xx")
	if err == nil {
		t.Error("parseTimeOfDay should return error for non-numeric minute")
	}
}
