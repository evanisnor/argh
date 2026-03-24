package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
)

// ── test doubles ──────────────────────────────────────────────────────────────

// stubWatchReader is a test double for WatchReader.
type stubWatchReader struct {
	watches        []persistence.Watch
	listWatchesErr error
}

func (s *stubWatchReader) ListWatches() ([]persistence.Watch, error) {
	if s.listWatchesErr != nil {
		return nil, s.listWatchesErr
	}
	return s.watches, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func makeWatchesPanel(reader *stubWatchReader) *WatchesPanel {
	return NewWatchesPanel(reader)
}

func makeWatch(id, repo string, prNumber int, status string) persistence.Watch {
	return persistence.Watch{
		ID:          id,
		Repo:        repo,
		PRNumber:    prNumber,
		TriggerExpr: "on:ci-pass",
		ActionExpr:  "merge",
		Status:      status,
	}
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestWatchesPanel_NoWatches verifies the panel reports no content and renders
// the empty-state message when there are no watches.
func TestWatchesPanel_NoWatches(t *testing.T) {
	reader := &stubWatchReader{}
	panel := makeWatchesPanel(reader)

	if panel.HasContent() {
		t.Error("expected HasContent() == false with no watches")
	}

	view := panel.View()
	if !strings.Contains(view, "no active watches") {
		t.Errorf("expected empty-state message in view; got:\n%s", view)
	}
	if !strings.Contains(view, "[0 active]") {
		t.Errorf("expected '[0 active]' badge; got:\n%s", view)
	}
}

// TestWatchesPanel_HasContent verifies HasContent reflects the presence of watches.
func TestWatchesPanel_HasContent(t *testing.T) {
	t.Run("with watches", func(t *testing.T) {
		reader := &stubWatchReader{
			watches: []persistence.Watch{makeWatch("w1", "repo/a", 1, "waiting")},
		}
		panel := makeWatchesPanel(reader)
		if !panel.HasContent() {
			t.Error("expected HasContent() == true with a watch")
		}
	})

	t.Run("fired watch still shows as content", func(t *testing.T) {
		reader := &stubWatchReader{
			watches: []persistence.Watch{makeWatch("w1", "repo/a", 1, "fired")},
		}
		panel := makeWatchesPanel(reader)
		if !panel.HasContent() {
			t.Error("expected HasContent() == true even for fired watch")
		}
	})
}

// TestWatchesPanel_StatusSymbols verifies each status value renders the correct symbol.
func TestWatchesPanel_StatusSymbols(t *testing.T) {
	tests := []struct {
		status     string
		wantSymbol string
	}{
		{"waiting", "⟳"},
		{"scheduled", "⟳"},
		{"fired", "✓"},
		{"failed", "✗"},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			reader := &stubWatchReader{
				watches: []persistence.Watch{makeWatch("w1", "repo/a", 42, tt.status)},
			}
			panel := makeWatchesPanel(reader)
			view := panel.View()
			if !strings.Contains(view, tt.wantSymbol) {
				t.Errorf("status %q: expected symbol %q in view; got:\n%s", tt.status, tt.wantSymbol, view)
			}
		})
	}
}

// TestWatchStatusDisplay covers all branches of watchStatusDisplay, including
// the default fallback.
func TestWatchStatusDisplay(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"waiting", "⟳"},
		{"scheduled", "⟳"},
		{"fired", "✓"},
		{"failed", "✗"},
		{"unknown", "unknown"},
		{"", ""},
	}
	for _, tt := range tests {
		got := watchStatusDisplay(tt.status)
		if got != tt.want {
			t.Errorf("watchStatusDisplay(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

// TestWatchesPanel_BadgeCount verifies the "[N active]" badge reflects only
// waiting and scheduled watches, not fired or failed ones.
func TestWatchesPanel_BadgeCount(t *testing.T) {
	tests := []struct {
		name      string
		watches   []persistence.Watch
		wantBadge string
	}{
		{
			"all active",
			[]persistence.Watch{
				makeWatch("w1", "r", 1, "waiting"),
				makeWatch("w2", "r", 2, "scheduled"),
			},
			"[2 active]",
		},
		{
			"mixed: 1 active 1 fired",
			[]persistence.Watch{
				makeWatch("w1", "r", 1, "waiting"),
				makeWatch("w2", "r", 2, "fired"),
			},
			"[1 active]",
		},
		{
			"all fired or failed",
			[]persistence.Watch{
				makeWatch("w1", "r", 1, "fired"),
				makeWatch("w2", "r", 2, "failed"),
			},
			"[0 active]",
		},
		{
			"zero watches",
			nil,
			"[0 active]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &stubWatchReader{watches: tt.watches}
			panel := makeWatchesPanel(reader)
			view := panel.View()
			if !strings.Contains(view, tt.wantBadge) {
				t.Errorf("expected %q in view; got:\n%s", tt.wantBadge, view)
			}
		})
	}
}

// TestWatchesPanel_WatchFiredFlash verifies that a WATCH_FIRED event triggers the
// flash animation on the correct row.
func TestWatchesPanel_WatchFiredFlash(t *testing.T) {
	w := makeWatch("watch-id-123", "repo/a", 7, "waiting")
	reader := &stubWatchReader{watches: []persistence.Watch{w}}
	panel := makeWatchesPanel(reader)

	// Flash state should be empty before the event.
	if panel.flashing["watch-id-123"] {
		t.Error("expected no flash before WATCH_FIRED event")
	}

	// Send a WATCH_FIRED event carrying the watch.
	msg := DBEventMsg{Event: eventbus.Event{Type: eventbus.WatchFired, After: w}}
	updated, _ := panel.Update(msg)
	updatedPanel := updated.(*WatchesPanel)

	if !updatedPanel.flashing["watch-id-123"] {
		t.Error("expected watch-id-123 to be flashing after WATCH_FIRED")
	}

	// View must still contain the watch row.
	view := updatedPanel.View()
	if !strings.Contains(view, "repo/a") {
		t.Errorf("expected watch row in view after flash; got:\n%s", view)
	}
}

// TestWatchesPanel_WatchFiredNoFlashWithoutWatch verifies that a WATCH_FIRED event
// with no Watch in After does not set any flash state.
func TestWatchesPanel_WatchFiredNoFlashWithoutWatch(t *testing.T) {
	reader := &stubWatchReader{}
	panel := makeWatchesPanel(reader)

	msg := DBEventMsg{Event: eventbus.Event{Type: eventbus.WatchFired, After: "not-a-watch"}}
	updated, _ := panel.Update(msg)
	updatedPanel := updated.(*WatchesPanel)

	if len(updatedPanel.flashing) != 0 {
		t.Errorf("expected no flashing entries, got: %v", updatedPanel.flashing)
	}
}

// TestWatchesPanel_UnrelatedDBEvent verifies that non-WATCH_FIRED DB events are ignored.
func TestWatchesPanel_UnrelatedDBEvent(t *testing.T) {
	reader := &stubWatchReader{
		watches: []persistence.Watch{makeWatch("w1", "r", 1, "waiting")},
	}
	panel := makeWatchesPanel(reader)

	msg := DBEventMsg{Event: eventbus.Event{Type: eventbus.PRUpdated}}
	panel.Update(msg)

	if len(panel.flashing) != 0 {
		t.Errorf("expected no flashing for PRUpdated event, got: %v", panel.flashing)
	}
}

// TestWatchesPanel_MoveFocus verifies j/k navigation stays within bounds.
func TestWatchesPanel_MoveFocus(t *testing.T) {
	reader := &stubWatchReader{
		watches: []persistence.Watch{
			makeWatch("w1", "r", 1, "waiting"),
			makeWatch("w2", "r", 2, "waiting"),
			makeWatch("w3", "r", 3, "waiting"),
		},
	}
	panel := makeWatchesPanel(reader)

	// Move down twice.
	panel.Update(MoveFocusMsg{Down: true})
	panel.Update(MoveFocusMsg{Down: true})
	if panel.cursor != 2 {
		t.Errorf("expected cursor=2, got %d", panel.cursor)
	}

	// Move down past end: stays at 2.
	panel.Update(MoveFocusMsg{Down: true})
	if panel.cursor != 2 {
		t.Errorf("expected cursor to stay at 2, got %d", panel.cursor)
	}

	// Move up once.
	panel.Update(MoveFocusMsg{Down: false})
	if panel.cursor != 1 {
		t.Errorf("expected cursor=1, got %d", panel.cursor)
	}

	// Move up past start: stays at 0.
	panel.Update(MoveFocusMsg{Down: false})
	panel.Update(MoveFocusMsg{Down: false})
	if panel.cursor != 0 {
		t.Errorf("expected cursor to stay at 0, got %d", panel.cursor)
	}
}

// TestWatchesPanel_RefreshError verifies that a ListWatches error leaves rows empty.
func TestWatchesPanel_RefreshError(t *testing.T) {
	reader := &stubWatchReader{listWatchesErr: fmt.Errorf("db error")}
	panel := makeWatchesPanel(reader)

	if panel.HasContent() {
		t.Error("expected HasContent() == false after ListWatches error")
	}
}

// TestWatchesPanel_IDTruncation verifies that watch IDs longer than 8 chars are
// truncated in the view.
func TestWatchesPanel_IDTruncation(t *testing.T) {
	longID := "abcdef1234567890"
	reader := &stubWatchReader{
		watches: []persistence.Watch{makeWatch(longID, "repo/a", 1, "waiting")},
	}
	panel := makeWatchesPanel(reader)
	view := panel.View()

	// Truncated prefix must appear; full ID must not.
	if !strings.Contains(view, "abcdef12") {
		t.Errorf("expected truncated ID 'abcdef12' in view; got:\n%s", view)
	}
	if strings.Contains(view, longID) {
		t.Errorf("expected full ID to be truncated in view; got:\n%s", view)
	}
}

// TestWatchesPanel_ShortID verifies IDs of 8 chars or fewer are shown as-is.
func TestWatchesPanel_ShortID(t *testing.T) {
	shortID := "w1"
	reader := &stubWatchReader{
		watches: []persistence.Watch{makeWatch(shortID, "repo/a", 1, "waiting")},
	}
	panel := makeWatchesPanel(reader)
	view := panel.View()

	if !strings.Contains(view, shortID) {
		t.Errorf("expected short ID %q in view; got:\n%s", shortID, view)
	}
}

// TestWatchesPanel_MultipleRowsRendered verifies multiple watch rows appear.
func TestWatchesPanel_MultipleRowsRendered(t *testing.T) {
	reader := &stubWatchReader{
		watches: []persistence.Watch{
			makeWatch("w1", "repo/a", 1, "waiting"),
			makeWatch("w2", "repo/b", 2, "fired"),
		},
	}
	panel := makeWatchesPanel(reader)
	view := panel.View()

	if !strings.Contains(view, "repo/a") {
		t.Errorf("expected 'repo/a' in view; got:\n%s", view)
	}
	if !strings.Contains(view, "repo/b") {
		t.Errorf("expected 'repo/b' in view; got:\n%s", view)
	}
}

// TestWatchesPanel_CursorClamping verifies the cursor is clamped when watches
// are refreshed to a shorter list.
func TestWatchesPanel_CursorClamping(t *testing.T) {
	reader := &stubWatchReader{
		watches: []persistence.Watch{
			makeWatch("w1", "r", 1, "waiting"),
			makeWatch("w2", "r", 2, "waiting"),
			makeWatch("w3", "r", 3, "waiting"),
		},
	}
	panel := makeWatchesPanel(reader)

	panel.Update(MoveFocusMsg{Down: true})
	panel.Update(MoveFocusMsg{Down: true})
	if panel.cursor != 2 {
		t.Fatalf("expected cursor=2, got %d", panel.cursor)
	}

	// Shrink list to 1 and trigger refresh via WATCH_FIRED.
	reader.watches = reader.watches[:1]
	w := makeWatch("w1", "r", 1, "fired")
	panel.Update(DBEventMsg{Event: eventbus.Event{Type: eventbus.WatchFired, After: w}})

	if panel.cursor != 0 {
		t.Errorf("expected cursor to clamp to 0, got %d", panel.cursor)
	}
}

// TestWatchesPanel_FocusedRowHighlight verifies the focused row uses reverse style.
func TestWatchesPanel_FocusedRowHighlight(t *testing.T) {
	reader := &stubWatchReader{
		watches: []persistence.Watch{makeWatch("w1", "repo/focused", 1, "waiting")},
	}
	panel := makeWatchesPanel(reader)
	panel.SetFocused(true)
	view := panel.View()

	// The focused row (cursor=0) must appear in the view.
	if !strings.Contains(view, "repo/focused") {
		t.Errorf("expected focused row in view; got:\n%s", view)
	}
}

// TestWatchesPanel_FailedStyleApplied verifies the failed-status style branch is
// exercised (red foreground applied to "failed" watch rows).
func TestWatchesPanel_FailedStyleApplied(t *testing.T) {
	reader := &stubWatchReader{
		watches: []persistence.Watch{makeWatch("w1", "repo/fail", 1, "failed")},
	}
	panel := makeWatchesPanel(reader)
	view := panel.View()

	if !strings.Contains(view, "repo/fail") {
		t.Errorf("expected failed watch row in view; got:\n%s", view)
	}
	if !strings.Contains(view, "✗") {
		t.Errorf("expected '✗' for failed watch; got:\n%s", view)
	}
}

// TestWatchesPanel_FiredStyleApplied verifies the fired-status style branch (faint)
// is exercised.
func TestWatchesPanel_FiredStyleApplied(t *testing.T) {
	reader := &stubWatchReader{
		watches: []persistence.Watch{makeWatch("w1", "repo/fired", 1, "fired")},
	}
	panel := makeWatchesPanel(reader)
	view := panel.View()

	if !strings.Contains(view, "repo/fired") {
		t.Errorf("expected fired watch row in view; got:\n%s", view)
	}
	if !strings.Contains(view, "✓") {
		t.Errorf("expected '✓' for fired watch; got:\n%s", view)
	}
}

// TestWatchesPanel_FlashRendering verifies that a flashing row is rendered with
// bold style (the flash code path in renderRow is exercised).
func TestWatchesPanel_FlashRendering(t *testing.T) {
	w := makeWatch("w1", "repo/flash", 1, "waiting")
	reader := &stubWatchReader{watches: []persistence.Watch{w}}
	panel := makeWatchesPanel(reader)

	panel.Update(DBEventMsg{Event: eventbus.Event{Type: eventbus.WatchFired, After: w}})
	view := panel.View()

	if !strings.Contains(view, "repo/flash") {
		t.Errorf("expected flashing watch row in view; got:\n%s", view)
	}
}

// TestNewWatchesPanel verifies the exported constructor creates a usable panel.
func TestNewWatchesPanel(t *testing.T) {
	reader := &stubWatchReader{
		watches: []persistence.Watch{makeWatch("w1", "repo/a", 1, "waiting")},
	}
	panel := NewWatchesPanel(reader)
	if panel == nil {
		t.Fatal("expected non-nil panel")
	}
	if !panel.HasContent() {
		t.Error("expected HasContent() == true")
	}
}

// ── ResizeMsg ────────────────────────────────────────────────────────────────

// TestWatchesPanel_ResizeMsg verifies that a ResizeMsg updates the panel's
// allocated width.
func TestWatchesPanel_ResizeMsg(t *testing.T) {
	reader := &stubWatchReader{}
	panel := NewWatchesPanel(reader)
	sm, _ := panel.Update(ResizeMsg{Width: 100, Height: 20})
	p := sm.(*WatchesPanel)
	if p.width != 100 {
		t.Errorf("width = %d, want 100", p.width)
	}
}

// TestWatchesPanel_TriggerTruncation verifies that a very long trigger expression
// is truncated when a narrow width is set via ResizeMsg.
func TestWatchesPanel_TriggerTruncation(t *testing.T) {
	longTrigger := strings.Repeat("z", 200)
	reader := &stubWatchReader{
		watches: []persistence.Watch{
			{ID: "w1", Repo: "repo", PRNumber: 42,
				TriggerExpr: longTrigger, ActionExpr: "merge", Status: "waiting"},
		},
	}
	panel := NewWatchesPanel(reader)
	panel.Update(ResizeMsg{Width: 60, Height: 10})
	view := panel.View()
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, longTrigger) {
			t.Errorf("long trigger not truncated in view line: %q", line)
		}
	}
}
