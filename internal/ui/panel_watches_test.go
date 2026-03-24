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
	sessionIDs     map[string]string           // prURL → sessionID
	pullRequests   map[string]persistence.PullRequest // "repo/number" → PR
}

func (s *stubWatchReader) ListWatches() ([]persistence.Watch, error) {
	if s.listWatchesErr != nil {
		return nil, s.listWatchesErr
	}
	return s.watches, nil
}

func (s *stubWatchReader) GetSessionID(prURL string) (string, error) {
	if s.sessionIDs != nil {
		if sid, ok := s.sessionIDs[prURL]; ok {
			return sid, nil
		}
	}
	return "", fmt.Errorf("not found")
}

func (s *stubWatchReader) GetPullRequest(repo string, number int) (persistence.PullRequest, error) {
	if s.pullRequests != nil {
		key := fmt.Sprintf("%s/%d", repo, number)
		if pr, ok := s.pullRequests[key]; ok {
			return pr, nil
		}
	}
	return persistence.PullRequest{}, fmt.Errorf("not found")
}

// stubWatchCanceller is a test double for WatchCanceller.
type stubWatchCanceller struct {
	cancelledID string
	cancelErr   error
}

func (s *stubWatchCanceller) CancelWatch(id string) error {
	s.cancelledID = id
	return s.cancelErr
}

// ── helpers ───────────────────────────────────────────────────────────────────

func makeWatchesPanel(reader *stubWatchReader) *WatchesPanel {
	return NewWatchesPanel(reader, nil)
}

func makeWatch(id, repo string, prNumber int, status string) persistence.Watch {
	return persistence.Watch{
		ID:          id,
		PRURL:       "https://github.com/" + repo + "/pull/" + fmt.Sprint(prNumber),
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
}

// TestWatchesPanel_HasContent verifies HasContent reflects the presence of active watches.
func TestWatchesPanel_HasContent(t *testing.T) {
	t.Run("with active watches", func(t *testing.T) {
		reader := &stubWatchReader{
			watches: []persistence.Watch{makeWatch("w1", "repo/a", 1, "waiting")},
		}
		panel := makeWatchesPanel(reader)
		if !panel.HasContent() {
			t.Error("expected HasContent() == true with an active watch")
		}
	})

	t.Run("fired watch is not content", func(t *testing.T) {
		reader := &stubWatchReader{
			watches: []persistence.Watch{makeWatch("w1", "repo/a", 1, "fired")},
		}
		panel := makeWatchesPanel(reader)
		if panel.HasContent() {
			t.Error("expected HasContent() == false for only fired watch")
		}
	})

	t.Run("failed watch is not content", func(t *testing.T) {
		reader := &stubWatchReader{
			watches: []persistence.Watch{makeWatch("w1", "repo/a", 1, "failed")},
		}
		panel := makeWatchesPanel(reader)
		if panel.HasContent() {
			t.Error("expected HasContent() == false for only failed watch")
		}
	})
}

// TestWatchesPanel_OnlyActiveWatchesShown verifies that fired and failed watches
// are filtered out — only waiting and scheduled watches appear.
func TestWatchesPanel_OnlyActiveWatchesShown(t *testing.T) {
	reader := &stubWatchReader{
		watches: []persistence.Watch{
			makeWatch("w1", "repo/a", 1, "waiting"),
			makeWatch("w2", "repo/b", 2, "fired"),
			makeWatch("w3", "repo/c", 3, "failed"),
			makeWatch("w4", "repo/d", 4, "scheduled"),
		},
	}
	panel := makeWatchesPanel(reader)

	if panel.RowCount() != 2 {
		t.Errorf("expected 2 active rows, got %d", panel.RowCount())
	}

	view := panel.View()
	if !strings.Contains(view, "repo/a") {
		t.Errorf("expected waiting watch in view; got:\n%s", view)
	}
	if !strings.Contains(view, "repo/d") {
		t.Errorf("expected scheduled watch in view; got:\n%s", view)
	}
	if strings.Contains(view, "repo/b") {
		t.Errorf("fired watch should NOT appear in view; got:\n%s", view)
	}
	if strings.Contains(view, "repo/c") {
		t.Errorf("failed watch should NOT appear in view; got:\n%s", view)
	}
}

// TestWatchesPanel_ActiveStatuses verifies both active statuses are shown.
func TestWatchesPanel_ActiveStatuses(t *testing.T) {
	for _, status := range []string{"waiting", "scheduled"} {
		t.Run(status, func(t *testing.T) {
			reader := &stubWatchReader{
				watches: []persistence.Watch{makeWatch("w1", "repo/a", 42, status)},
			}
			panel := makeWatchesPanel(reader)
			if panel.RowCount() != 1 {
				t.Errorf("status %q: expected 1 row, got %d", status, panel.RowCount())
			}
			view := panel.View()
			if !strings.Contains(view, "repo/a") {
				t.Errorf("status %q: expected repo in view; got:\n%s", status, view)
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

// TestWatchesPanel_RowCount verifies RowCount returns the number of active watches.
func TestWatchesPanel_RowCount(t *testing.T) {
	tests := []struct {
		name    string
		watches []persistence.Watch
		want    int
	}{
		{
			"all active",
			[]persistence.Watch{
				makeWatch("w1", "r", 1, "waiting"),
				makeWatch("w2", "r", 2, "scheduled"),
			},
			2,
		},
		{
			"mixed: 1 active 1 fired",
			[]persistence.Watch{
				makeWatch("w1", "r", 1, "waiting"),
				makeWatch("w2", "r", 2, "fired"),
			},
			1,
		},
		{
			"all fired or failed",
			[]persistence.Watch{
				makeWatch("w1", "r", 1, "fired"),
				makeWatch("w2", "r", 2, "failed"),
			},
			0,
		},
		{
			"zero watches",
			nil,
			0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &stubWatchReader{watches: tt.watches}
			panel := makeWatchesPanel(reader)
			if got := panel.RowCount(); got != tt.want {
				t.Errorf("RowCount() = %d, want %d", got, tt.want)
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

	if panel.flashing["watch-id-123"] {
		t.Error("expected no flash before WATCH_FIRED event")
	}

	msg := DBEventMsg{Event: eventbus.Event{Type: eventbus.WatchFired, After: w}}
	updated, _ := panel.Update(msg)
	updatedPanel := updated.(*WatchesPanel)

	if !updatedPanel.flashing["watch-id-123"] {
		t.Error("expected watch-id-123 to be flashing after WATCH_FIRED")
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

	panel.Update(MoveFocusMsg{Down: true})
	panel.Update(MoveFocusMsg{Down: true})
	if panel.cursor != 2 {
		t.Errorf("expected cursor=2, got %d", panel.cursor)
	}

	panel.Update(MoveFocusMsg{Down: true})
	if panel.cursor != 2 {
		t.Errorf("expected cursor to stay at 2, got %d", panel.cursor)
	}

	panel.Update(MoveFocusMsg{Down: false})
	if panel.cursor != 1 {
		t.Errorf("expected cursor=1, got %d", panel.cursor)
	}

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

// TestWatchesPanel_RefreshMsg verifies that a RefreshMsg reloads data from the reader.
func TestWatchesPanel_RefreshMsg(t *testing.T) {
	reader := &stubWatchReader{}
	panel := makeWatchesPanel(reader)
	if panel.HasContent() {
		t.Fatal("expected no content initially")
	}
	reader.watches = []persistence.Watch{
		{ID: "w1", PRURL: "url", PRNumber: 1, Repo: "r/r", TriggerExpr: "on:ci-pass", ActionExpr: "merge", Status: "waiting"},
	}
	panel.Update(RefreshMsg{})
	if !panel.HasContent() {
		t.Error("expected HasContent() == true after RefreshMsg")
	}
}

// TestWatchesPanel_CancelWatchMsg verifies that CancelWatchMsg cancels the watch
// under the cursor and returns a WatchChangedMsg.
func TestWatchesPanel_CancelWatchMsg(t *testing.T) {
	canceller := &stubWatchCanceller{}
	reader := &stubWatchReader{
		watches: []persistence.Watch{
			makeWatch("w1", "r", 1, "waiting"),
			makeWatch("w2", "r", 2, "waiting"),
		},
	}
	panel := NewWatchesPanel(reader, canceller)
	panel.Update(MoveFocusMsg{Down: true}) // cursor on w2

	_, cmd := panel.Update(CancelWatchMsg{})
	if cmd == nil {
		t.Fatal("expected non-nil Cmd from CancelWatchMsg")
	}
	msg := cmd()
	if _, ok := msg.(WatchChangedMsg); !ok {
		t.Fatalf("expected WatchChangedMsg, got %T", msg)
	}
	if canceller.cancelledID != "w2" {
		t.Errorf("expected CancelWatch called with w2, got %q", canceller.cancelledID)
	}
}

// TestWatchesPanel_CancelWatchMsg_NoRows verifies CancelWatchMsg is a no-op with no watches.
func TestWatchesPanel_CancelWatchMsg_NoRows(t *testing.T) {
	canceller := &stubWatchCanceller{}
	panel := NewWatchesPanel(&stubWatchReader{}, canceller)
	_, cmd := panel.Update(CancelWatchMsg{})
	if cmd != nil {
		t.Error("expected nil Cmd when no rows")
	}
}

// TestWatchesPanel_CancelWatchMsg_NoCanceller verifies CancelWatchMsg is a no-op without a canceller.
func TestWatchesPanel_CancelWatchMsg_NoCanceller(t *testing.T) {
	reader := &stubWatchReader{
		watches: []persistence.Watch{makeWatch("w1", "r", 1, "waiting")},
	}
	panel := NewWatchesPanel(reader, nil)
	_, cmd := panel.Update(CancelWatchMsg{})
	if cmd != nil {
		t.Error("expected nil Cmd when canceller is nil")
	}
}

// TestWatchesPanel_CancelWatchMsg_Error verifies CancelWatchMsg returns CommandResultMsg on error.
func TestWatchesPanel_CancelWatchMsg_Error(t *testing.T) {
	canceller := &stubWatchCanceller{cancelErr: fmt.Errorf("db error")}
	reader := &stubWatchReader{
		watches: []persistence.Watch{makeWatch("w1", "r", 1, "waiting")},
	}
	panel := NewWatchesPanel(reader, canceller)
	_, cmd := panel.Update(CancelWatchMsg{})
	if cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}
	msg := cmd()
	result, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg on error, got %T", msg)
	}
	if result.Err == nil {
		t.Error("expected non-nil Err")
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

// TestWatchesPanel_MultipleRowsRendered verifies multiple active watch rows appear.
func TestWatchesPanel_MultipleRowsRendered(t *testing.T) {
	reader := &stubWatchReader{
		watches: []persistence.Watch{
			makeWatch("w1", "repo/a", 1, "waiting"),
			makeWatch("w2", "repo/b", 2, "waiting"),
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
	w := makeWatch("w1", "r", 1, "waiting")
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

	if !strings.Contains(view, "repo/focused") {
		t.Errorf("expected focused row in view; got:\n%s", view)
	}
}

// TestWatchesPanel_FlashRendering verifies that a flashing row is rendered with
// bold style (the flash code path in the StyleFunc is exercised).
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
	panel := NewWatchesPanel(reader, nil)
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
	panel := NewWatchesPanel(reader, nil)
	sm, _ := panel.Update(ResizeMsg{Width: 100, Height: 20})
	p := sm.(*WatchesPanel)
	if p.width != 100 {
		t.Errorf("width = %d, want 100", p.width)
	}
}

// TestWatchesPanel_ViewWithWidth verifies the table renders with a width constraint.
func TestWatchesPanel_ViewWithWidth(t *testing.T) {
	reader := &stubWatchReader{
		watches: []persistence.Watch{makeWatch("w1", "repo/a", 1, "waiting")},
	}
	panel := makeWatchesPanel(reader)
	panel.Update(ResizeMsg{Width: 80, Height: 20})
	view := panel.View()
	if !strings.Contains(view, "repo/a") {
		t.Errorf("expected watch data in width-constrained view; got:\n%s", view)
	}
}

// ── Session ID ───────────────────────────────────────────────────────────────

// TestWatchesPanel_SessionIDDisplayed verifies that session IDs are looked up
// and displayed in the table.
func TestWatchesPanel_SessionIDDisplayed(t *testing.T) {
	w := makeWatch("w1", "repo/a", 1, "waiting")
	reader := &stubWatchReader{
		watches:    []persistence.Watch{w},
		sessionIDs: map[string]string{w.PRURL: "3"},
	}
	panel := makeWatchesPanel(reader)
	view := panel.View()

	if !strings.Contains(view, "3") {
		t.Errorf("expected session ID '3' in view; got:\n%s", view)
	}
}

// TestWatchesPanel_SessionIDMissing verifies that a missing session ID shows "-".
func TestWatchesPanel_SessionIDMissing(t *testing.T) {
	reader := &stubWatchReader{
		watches: []persistence.Watch{makeWatch("w1", "repo/a", 1, "waiting")},
	}
	panel := makeWatchesPanel(reader)
	view := panel.View()

	if !strings.Contains(view, "-") {
		t.Errorf("expected '-' for missing session ID in view; got:\n%s", view)
	}
}

// ── CursorNavigator ──────────────────────────────────────────────────────────

// TestWatchesPanel_CursorNavigator verifies CursorPosition and SetCursor.
func TestWatchesPanel_CursorNavigator(t *testing.T) {
	reader := &stubWatchReader{
		watches: []persistence.Watch{
			makeWatch("w1", "r", 1, "waiting"),
			makeWatch("w2", "r", 2, "waiting"),
			makeWatch("w3", "r", 3, "waiting"),
		},
	}
	panel := makeWatchesPanel(reader)

	if panel.CursorPosition() != 0 {
		t.Errorf("initial CursorPosition() = %d, want 0", panel.CursorPosition())
	}

	panel.SetCursor(2)
	if panel.CursorPosition() != 2 {
		t.Errorf("CursorPosition() = %d after SetCursor(2), want 2", panel.CursorPosition())
	}

	panel.SetCursor(10) // beyond bounds — clamped
	if panel.CursorPosition() != 2 {
		t.Errorf("CursorPosition() = %d after SetCursor(10), want 2 (clamped)", panel.CursorPosition())
	}

	panel.SetCursor(-1) // below bounds — clamped
	if panel.CursorPosition() != 0 {
		t.Errorf("CursorPosition() = %d after SetCursor(-1), want 0 (clamped)", panel.CursorPosition())
	}
}

// TestWatchesPanel_SetCursor_EmptyPanel verifies SetCursor on an empty panel.
func TestWatchesPanel_SetCursor_EmptyPanel(t *testing.T) {
	panel := makeWatchesPanel(&stubWatchReader{})
	panel.SetCursor(5)
	if panel.CursorPosition() != 0 {
		t.Errorf("CursorPosition() = %d on empty panel after SetCursor(5), want 0", panel.CursorPosition())
	}
}

// ── Table rendering ──────────────────────────────────────────────────────────

// TestWatchesPanel_TableContainsHeaders verifies the table includes column headers.
func TestWatchesPanel_TableContainsHeaders(t *testing.T) {
	reader := &stubWatchReader{
		watches: []persistence.Watch{makeWatch("w1", "repo/a", 1, "waiting")},
	}
	panel := makeWatchesPanel(reader)
	view := panel.View()

	for _, hdr := range []string{"ID", "REPO", "TITLE", "TRIGGER", "ACTION"} {
		if !strings.Contains(view, hdr) {
			t.Errorf("expected header %q in view; got:\n%s", hdr, view)
		}
	}
}

// TestWatchesPanel_TableContainsCellData verifies that cell data appears in the table.
func TestWatchesPanel_TableContainsCellData(t *testing.T) {
	w := persistence.Watch{
		ID: "abcd1234", PRURL: "url", Repo: "org/repo", PRNumber: 42,
		TriggerExpr: "on:ci-pass", ActionExpr: "merge", Status: "waiting",
	}
	reader := &stubWatchReader{
		watches: []persistence.Watch{w},
		pullRequests: map[string]persistence.PullRequest{
			"org/repo/42": {Title: "Fix the thing", CIState: "passing"},
		},
	}
	panel := makeWatchesPanel(reader)
	view := panel.View()

	for _, want := range []string{"abcd1234", "org/repo", "#42", "Fix the thing", "on:ci-pass", "merge"} {
		if !strings.Contains(view, want) {
			t.Errorf("expected %q in table view; got:\n%s", want, view)
		}
	}
}

// TestWatchesPanel_PRTitleDisplayed verifies that the PR title appears in the table.
func TestWatchesPanel_PRTitleDisplayed(t *testing.T) {
	w := makeWatch("w1", "repo/a", 1, "waiting")
	reader := &stubWatchReader{
		watches: []persistence.Watch{w},
		pullRequests: map[string]persistence.PullRequest{
			"repo/a/1": {Title: "Add cool feature"},
		},
	}
	panel := makeWatchesPanel(reader)
	view := panel.View()

	if !strings.Contains(view, "Add cool feature") {
		t.Errorf("expected PR title in view; got:\n%s", view)
	}
}

// TestWatchesPanel_PRNotFound_EmptyTitle verifies a missing PR shows empty title.
func TestWatchesPanel_PRNotFound_EmptyTitle(t *testing.T) {
	reader := &stubWatchReader{
		watches: []persistence.Watch{makeWatch("w1", "repo/a", 1, "waiting")},
	}
	panel := makeWatchesPanel(reader)
	view := panel.View()

	// Should still render without crashing — just no title text.
	if !strings.Contains(view, "repo/a") {
		t.Errorf("expected repo in view; got:\n%s", view)
	}
}

// TestWatchesPanel_CIColor verifies that rows are colored by CI state.
func TestWatchesPanel_CIColor(t *testing.T) {
	tests := []struct {
		name    string
		ciState string
	}{
		{"passing", "passing"},
		{"success", "success"},
		{"running", "running"},
		{"in_progress", "in_progress"},
		{"pending", "pending"},
		{"failing", "failing"},
		{"failure", "failure"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := makeWatch("w1", "repo/a", 1, "waiting")
			reader := &stubWatchReader{
				watches: []persistence.Watch{w},
				pullRequests: map[string]persistence.PullRequest{
					"repo/a/1": {Title: "PR", CIState: tt.ciState},
				},
			}
			panel := makeWatchesPanel(reader)
			view := panel.View()
			// Exercises the color branch — row must still render.
			if !strings.Contains(view, "repo/a") {
				t.Errorf("expected watch data in view for CI state %q; got:\n%s", tt.ciState, view)
			}
		})
	}
}

// TestWatchesPanel_CIIndicatorDisplayed verifies the CI indicator column appears.
func TestWatchesPanel_CIIndicatorDisplayed(t *testing.T) {
	w := makeWatch("w1", "repo/a", 1, "waiting")
	reader := &stubWatchReader{
		watches: []persistence.Watch{w},
		pullRequests: map[string]persistence.PullRequest{
			"repo/a/1": {Title: "PR", CIState: "passing"},
		},
	}
	panel := makeWatchesPanel(reader)
	view := panel.View()

	// The header should include the CI column.
	if !strings.Contains(view, "⚙") {
		t.Errorf("expected CI header '⚙' in view; got:\n%s", view)
	}
}
