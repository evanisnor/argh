package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
)

// ── test doubles ──────────────────────────────────────────────────────────────

// stubPRReader is a test double for PRReader.
type stubPRReader struct {
	prs               []persistence.PullRequest
	sessionIDs        map[string]string
	watches           []persistence.Watch
	reviewers         map[string][]persistence.Reviewer
	listPRsErr        error
	listWatchesErr    error
}

func newStubPRReader() *stubPRReader {
	return &stubPRReader{
		sessionIDs: make(map[string]string),
		reviewers:  make(map[string][]persistence.Reviewer),
	}
}

func (s *stubPRReader) ListPullRequests() ([]persistence.PullRequest, error) {
	if s.listPRsErr != nil {
		return nil, s.listPRsErr
	}
	return s.prs, nil
}

func (s *stubPRReader) GetSessionID(prURL string) (string, error) {
	id, ok := s.sessionIDs[prURL]
	if !ok {
		return "", fmt.Errorf("not found: %s", prURL)
	}
	return id, nil
}

func (s *stubPRReader) ListWatches() ([]persistence.Watch, error) {
	if s.listWatchesErr != nil {
		return nil, s.listWatchesErr
	}
	return s.watches, nil
}

func (s *stubPRReader) ListReviewers(prID string) ([]persistence.Reviewer, error) {
	return s.reviewers[prID], nil
}

// stubClock is a controllable Clock for tests.
type stubClock struct {
	now time.Time
}

func (c stubClock) Now() time.Time { return c.now }

// ── helpers ───────────────────────────────────────────────────────────────────

var (
	t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t1 = time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC) // 1 day after t0
	t2 = time.Date(2026, 1, 3, 12, 0, 0, 0, time.UTC) // 2 days after t0
)

func makePanel(reader *stubPRReader) *MyPRsPanel {
	return newMyPRsPanelWithClock(reader, "", stubClock{now: t2})
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestMyPRsPanel_Sorting verifies that PRs with older last_activity_at appear first.
func TestMyPRsPanel_Sorting(t *testing.T) {
	reader := newStubPRReader()
	reader.prs = []persistence.PullRequest{
		{ID: "pr1", Repo: "repo/a", Number: 1, Title: "newer PR", Status: "open", URL: "https://gh/a/1", LastActivityAt: t1},
		{ID: "pr2", Repo: "repo/b", Number: 2, Title: "older PR", Status: "open", URL: "https://gh/b/2", LastActivityAt: t0},
	}
	reader.sessionIDs["https://gh/a/1"] = "b"
	reader.sessionIDs["https://gh/b/2"] = "a"

	panel := makePanel(reader)
	view := panel.View()

	// "older PR" (last_activity_at=t0) must appear before "newer PR" (last_activity_at=t1).
	olderIdx := strings.Index(view, "older PR")
	newerIdx := strings.Index(view, "newer PR")
	if olderIdx == -1 {
		t.Fatal("expected 'older PR' in view")
	}
	if newerIdx == -1 {
		t.Fatal("expected 'newer PR' in view")
	}
	if olderIdx > newerIdx {
		t.Errorf("expected older PR (index %d) to appear before newer PR (index %d)", olderIdx, newerIdx)
	}
}

// TestMyPRsPanel_DraftRendering verifies that draft PRs show "draft" in the status column.
func TestMyPRsPanel_DraftRendering(t *testing.T) {
	reader := newStubPRReader()
	reader.prs = []persistence.PullRequest{
		{ID: "pr1", Repo: "repo", Number: 1, Title: "my draft", Status: "open", Draft: true,
			URL: "https://gh/1", LastActivityAt: t0},
	}
	reader.sessionIDs["https://gh/1"] = "a"

	panel := makePanel(reader)
	view := panel.View()

	if !strings.Contains(view, "my draft") {
		t.Errorf("expected title 'my draft' in view")
	}
	// Draft status column shows "draft".
	if !strings.Contains(view, "draft") {
		t.Errorf("expected 'draft' status in view")
	}
}

// TestMyPRsPanel_FailingCIHighlight verifies that a failing-CI row goes through
// the highlight code path (the colour style branch is exercised for coverage).
func TestMyPRsPanel_FailingCIHighlight(t *testing.T) {
	reader := newStubPRReader()
	reader.prs = []persistence.PullRequest{
		{ID: "pr1", Repo: "repo", Number: 1, Title: "failing CI PR", Status: "open",
			CIState: "failing", URL: "https://gh/1", LastActivityAt: t0},
	}
	reader.sessionIDs["https://gh/1"] = "a"

	panel := makePanel(reader)
	view := panel.View()

	if !strings.Contains(view, "failing CI PR") {
		t.Errorf("expected row to appear in view")
	}
	// CI display symbol for "failing" is "✗".
	if !strings.Contains(view, "✗") {
		t.Errorf("expected '✗' CI symbol in view")
	}
}

// TestMyPRsPanel_WatchIcon verifies the 👁 icon is present when the PR has
// active watches, and absent when it does not.
func TestMyPRsPanel_WatchIcon(t *testing.T) {
	t.Run("with active watch", func(t *testing.T) {
		reader := newStubPRReader()
		url := "https://gh/1"
		reader.prs = []persistence.PullRequest{
			{ID: "pr1", Repo: "repo", Number: 1, Title: "watched", Status: "open",
				URL: url, LastActivityAt: t0},
		}
		reader.sessionIDs[url] = "a"
		reader.watches = []persistence.Watch{
			{ID: "w1", PRURL: url, Status: "waiting"},
		}

		panel := makePanel(reader)
		view := panel.View()

		if !strings.Contains(view, "👁") {
			t.Errorf("expected 👁 icon when watch is active; view:\n%s", view)
		}
	})

	t.Run("without watch", func(t *testing.T) {
		reader := newStubPRReader()
		url := "https://gh/2"
		reader.prs = []persistence.PullRequest{
			{ID: "pr2", Repo: "repo", Number: 2, Title: "unwatched", Status: "open",
				URL: url, LastActivityAt: t0},
		}
		reader.sessionIDs[url] = "a"
		// no watches

		panel := makePanel(reader)
		view := panel.View()

		if strings.Contains(view, "👁") {
			t.Errorf("did not expect 👁 icon when no active watch; view:\n%s", view)
		}
	})

	t.Run("fired watch is not active", func(t *testing.T) {
		reader := newStubPRReader()
		url := "https://gh/3"
		reader.prs = []persistence.PullRequest{
			{ID: "pr3", Repo: "repo", Number: 3, Title: "fired watch PR", Status: "open",
				URL: url, LastActivityAt: t0},
		}
		reader.sessionIDs[url] = "a"
		reader.watches = []persistence.Watch{
			{ID: "w2", PRURL: url, Status: "fired"},
		}

		panel := makePanel(reader)
		view := panel.View()

		if strings.Contains(view, "👁") {
			t.Errorf("did not expect 👁 icon for fired watch; view:\n%s", view)
		}
	})

	t.Run("scheduled watch is active", func(t *testing.T) {
		reader := newStubPRReader()
		url := "https://gh/4"
		reader.prs = []persistence.PullRequest{
			{ID: "pr4", Repo: "repo", Number: 4, Title: "scheduled watch PR", Status: "open",
				URL: url, LastActivityAt: t0},
		}
		reader.sessionIDs[url] = "a"
		reader.watches = []persistence.Watch{
			{ID: "w3", PRURL: url, Status: "scheduled"},
		}

		panel := makePanel(reader)
		view := panel.View()

		if !strings.Contains(view, "👁") {
			t.Errorf("expected 👁 icon for scheduled watch; view:\n%s", view)
		}
	})
}

// TestMyPRsPanel_RowCount verifies RowCount() returns the number of PR rows.
func TestMyPRsPanel_RowCount(t *testing.T) {
	tests := []struct {
		name    string
		prCount int
		want    int
	}{
		{"zero PRs", 0, 0},
		{"one PR", 1, 1},
		{"three PRs", 3, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := newStubPRReader()
			for i := 0; i < tt.prCount; i++ {
				url := fmt.Sprintf("https://gh/%d", i)
				reader.prs = append(reader.prs, persistence.PullRequest{
					ID: fmt.Sprintf("pr%d", i), Repo: "repo", Number: i,
					Title: fmt.Sprintf("PR %d", i), Status: "open",
					URL: url, LastActivityAt: t0,
				})
				reader.sessionIDs[url] = fmt.Sprintf("s%d", i)
			}
			panel := makePanel(reader)
			if got := panel.RowCount(); got != tt.want {
				t.Errorf("RowCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestMyPRsPanel_HeaderRow verifies the header line contains column labels and
// separator characters.
func TestMyPRsPanel_HeaderRow(t *testing.T) {
	reader := newStubPRReader()
	reader.prs = []persistence.PullRequest{
		{ID: "pr1", Repo: "repo", Number: 1, Title: "PR",
			Status: "open", URL: "https://gh/1", LastActivityAt: t0},
	}
	reader.sessionIDs["https://gh/1"] = "a"
	panel := makePanel(reader)
	view := panel.View()

	for _, label := range []string{"REPO", "#", "TITLE", "●", "⚙", "✓✗", "💬", "⏱"} {
		if !strings.Contains(view, label) {
			t.Errorf("header missing label %q in view:\n%s", label, view)
		}
	}
	if !strings.Contains(view, "│") {
		t.Errorf("header missing separator │ in view:\n%s", view)
	}
}

// TestMyPRsPanel_StatusValues verifies all five status values render correctly.
func TestMyPRsPanel_StatusValues(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		draft      bool
		wantStatus string
	}{
		{"draft", "open", true, "draft"},
		{"open", "open", false, "open"},
		{"approved", "approved", false, "approved"},
		{"changes_requested", "changes_requested", false, "changes requested"},
		{"merge_queued", "merge_queued", false, "merge queued"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := newStubPRReader()
			reader.prs = []persistence.PullRequest{
				{ID: "pr1", Repo: "repo", Number: 1, Title: "test PR",
					Status: tt.status, Draft: tt.draft,
					URL: "https://gh/1", LastActivityAt: t0},
			}
			reader.sessionIDs["https://gh/1"] = "a"
			panel := makePanel(reader)
			view := panel.View()
			if !strings.Contains(view, tt.wantStatus) {
				t.Errorf("status %q: expected %q in view, got:\n%s", tt.status, tt.wantStatus, view)
			}
		})
	}
}

// TestMyPRsPanel_CIStates verifies all four CI states render the correct symbol.
func TestMyPRsPanel_CIStates(t *testing.T) {
	tests := []struct {
		name       string
		ciState    string
		wantSymbol string
	}{
		{"passing", "passing", "✓"},
		{"success alias", "success", "✓"},
		{"failing", "failing", "✗"},
		{"failure alias", "failure", "✗"},
		{"running", "running", "⟳"},
		{"in_progress alias", "in_progress", "⟳"},
		{"pending alias", "pending", "⟳"},
		{"none (empty)", "", "—"},
		{"none (unknown)", "unknown", "—"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := newStubPRReader()
			reader.prs = []persistence.PullRequest{
				{ID: "pr1", Repo: "repo", Number: 1, Title: "test PR",
					Status: "open", CIState: tt.ciState,
					URL: "https://gh/1", LastActivityAt: t0},
			}
			reader.sessionIDs["https://gh/1"] = "a"
			panel := makePanel(reader)
			view := panel.View()
			if !strings.Contains(view, tt.wantSymbol) {
				t.Errorf("CI state %q: expected symbol %q in view, got:\n%s", tt.ciState, tt.wantSymbol, view)
			}
		})
	}
}

// TestMyPRsPanel_HasContent verifies HasContent reflects row presence.
func TestMyPRsPanel_HasContent(t *testing.T) {
	t.Run("no PRs", func(t *testing.T) {
		reader := newStubPRReader()
		panel := makePanel(reader)
		if panel.HasContent() {
			t.Error("expected HasContent() == false with no PRs")
		}
	})
	t.Run("with PRs", func(t *testing.T) {
		reader := newStubPRReader()
		reader.prs = []persistence.PullRequest{
			{ID: "pr1", Repo: "repo", Number: 1, Title: "PR", Status: "open",
				URL: "https://gh/1", LastActivityAt: t0},
		}
		reader.sessionIDs["https://gh/1"] = "a"
		panel := makePanel(reader)
		if !panel.HasContent() {
			t.Error("expected HasContent() == true with one PR")
		}
	})
}

// TestMyPRsPanel_DBEventMsg_Flash verifies that a DB event marks the affected
// PR as flashing and triggers a refresh.
func TestMyPRsPanel_DBEventMsg_Flash(t *testing.T) {
	reader := newStubPRReader()
	pr := persistence.PullRequest{
		ID: "pr1", Repo: "repo", Number: 1, Title: "flash me",
		Status: "open", CIState: "passing", URL: "https://gh/1", LastActivityAt: t0,
	}
	reader.prs = []persistence.PullRequest{pr}
	reader.sessionIDs["https://gh/1"] = "a"

	panel := makePanel(reader)

	events := []eventbus.EventType{eventbus.PRUpdated, eventbus.CIChanged, eventbus.ReviewChanged}
	for _, evType := range events {
		// Reset flashing state.
		panel.flashing = make(map[string]bool)

		msg := DBEventMsg{Event: eventbus.Event{Type: evType, After: pr}}
		updated, _ := panel.Update(msg)
		updatedPanel := updated.(*MyPRsPanel)

		if !updatedPanel.flashing["pr1"] {
			t.Errorf("event type %s: expected PR 'pr1' to be flashing", evType)
		}
	}
}

// TestMyPRsPanel_DBEventMsg_NoFlashWithoutPR verifies that a DB event without
// a PullRequest in After does not set the flashing state.
func TestMyPRsPanel_DBEventMsg_NoFlashWithoutPR(t *testing.T) {
	reader := newStubPRReader()
	panel := makePanel(reader)

	msg := DBEventMsg{Event: eventbus.Event{Type: eventbus.PRUpdated, After: "not-a-pr"}}
	updated, _ := panel.Update(msg)
	updatedPanel := updated.(*MyPRsPanel)

	if len(updatedPanel.flashing) != 0 {
		t.Errorf("expected no flashing entries, got: %v", updatedPanel.flashing)
	}
}

// TestMyPRsPanel_DBEventMsg_PRRemoved_RefreshesNoFlash verifies that a PRRemoved
// event triggers a refresh but does not set flash (After is nil).
func TestMyPRsPanel_DBEventMsg_PRRemoved_RefreshesNoFlash(t *testing.T) {
	reader := newStubPRReader()
	panel := makePanel(reader)

	msg := DBEventMsg{Event: eventbus.Event{
		Type:   eventbus.PRRemoved,
		Before: persistence.PullRequest{ID: "pr1", Number: 1},
		After:  nil,
	}}
	updated, _ := panel.Update(msg)
	updatedPanel := updated.(*MyPRsPanel)

	if len(updatedPanel.flashing) != 0 {
		t.Errorf("expected no flashing entries for PRRemoved, got: %v", updatedPanel.flashing)
	}
}

// TestMyPRsPanel_DBEventMsg_SessionIDsAssigned_RefreshesNoFlash verifies that a
// SessionIDsAssigned event triggers a refresh but does not set flash.
func TestMyPRsPanel_DBEventMsg_SessionIDsAssigned_RefreshesNoFlash(t *testing.T) {
	reader := newStubPRReader()
	panel := makePanel(reader)

	msg := DBEventMsg{Event: eventbus.Event{Type: eventbus.SessionIDsAssigned}}
	updated, _ := panel.Update(msg)
	updatedPanel := updated.(*MyPRsPanel)

	if len(updatedPanel.flashing) != 0 {
		t.Errorf("expected no flashing entries for SessionIDsAssigned, got: %v", updatedPanel.flashing)
	}
}

// TestMyPRsPanel_MoveFocus verifies j/k navigation stays within bounds.
func TestMyPRsPanel_MoveFocus(t *testing.T) {
	reader := newStubPRReader()
	for i := 0; i < 3; i++ {
		url := fmt.Sprintf("https://gh/%d", i)
		reader.prs = append(reader.prs, persistence.PullRequest{
			ID: fmt.Sprintf("pr%d", i), Repo: "repo", Number: i,
			Title: "PR", Status: "open",
			URL: url, LastActivityAt: t0,
		})
		reader.sessionIDs[url] = fmt.Sprintf("%c", 'a'+i)
	}

	panel := makePanel(reader)

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

// TestMyPRsPanel_ReviewsColumn verifies the reviews column is rendered correctly.
func TestMyPRsPanel_ReviewsColumn(t *testing.T) {
	tests := []struct {
		name      string
		reviewers []persistence.Reviewer
		wantStr   string
	}{
		{"no reviews", nil, "—"},
		{"one approved", []persistence.Reviewer{{State: "APPROVED"}}, "✓1"},
		{"one changes requested", []persistence.Reviewer{{State: "CHANGES_REQUESTED"}}, "✗1"},
		{"both", []persistence.Reviewer{{State: "APPROVED"}, {State: "CHANGES_REQUESTED"}}, "✓1 ✗1"},
		{"commented reviewer counts as comment", []persistence.Reviewer{{State: "COMMENTED"}}, "—"},
		{"pending reviewer ignored in reviews", []persistence.Reviewer{{State: "PENDING"}}, "—"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := newStubPRReader()
			reader.prs = []persistence.PullRequest{
				{ID: "pr1", Repo: "repo", Number: 1, Title: "PR",
					Status: "open", URL: "https://gh/1", LastActivityAt: t0},
			}
			reader.sessionIDs["https://gh/1"] = "a"
			reader.reviewers["pr1"] = tt.reviewers
			panel := makePanel(reader)
			view := panel.View()
			if !strings.Contains(view, tt.wantStr) {
				t.Errorf("expected %q in view, got:\n%s", tt.wantStr, view)
			}
		})
	}
}

// TestMyPRsPanel_ChangesRequestedHighlight verifies that the changes-requested
// highlight code path is exercised when changesCount > 0.
func TestMyPRsPanel_ChangesRequestedHighlight(t *testing.T) {
	reader := newStubPRReader()
	reader.prs = []persistence.PullRequest{
		{ID: "pr1", Repo: "repo", Number: 1, Title: "needs changes",
			Status: "changes_requested", URL: "https://gh/1", LastActivityAt: t0},
	}
	reader.sessionIDs["https://gh/1"] = "a"
	reader.reviewers["pr1"] = []persistence.Reviewer{{State: "CHANGES_REQUESTED"}}

	panel := makePanel(reader)
	view := panel.View()

	if !strings.Contains(view, "needs changes") {
		t.Errorf("expected row to appear in view; got:\n%s", view)
	}
}

// TestMyPRsPanel_FocusedRowHighlight verifies the focused row is rendered
// (reverse style) when the cursor is on that row.
func TestMyPRsPanel_FocusedRowHighlight(t *testing.T) {
	reader := newStubPRReader()
	reader.prs = []persistence.PullRequest{
		{ID: "pr1", Repo: "repo", Number: 1, Title: "focused PR",
			Status: "open", URL: "https://gh/1", LastActivityAt: t0},
	}
	reader.sessionIDs["https://gh/1"] = "a"

	panel := makePanel(reader)
	panel.SetFocused(true)
	// cursor starts at 0 → focused row code path is exercised
	view := panel.View()
	if !strings.Contains(view, "focused PR") {
		t.Errorf("expected 'focused PR' in view; got:\n%s", view)
	}
}

// TestMyPRsPanel_EmptyView verifies the empty-state message appears when no PRs.
func TestMyPRsPanel_EmptyView(t *testing.T) {
	reader := newStubPRReader()
	panel := makePanel(reader)
	view := panel.View()
	if !strings.Contains(view, "no open pull requests") {
		t.Errorf("expected empty-state message; got:\n%s", view)
	}
}

// TestMyPRsPanel_SessionIDFallback verifies that a missing session ID shows "-".
func TestMyPRsPanel_SessionIDFallback(t *testing.T) {
	reader := newStubPRReader()
	reader.prs = []persistence.PullRequest{
		{ID: "pr1", Repo: "repo", Number: 1, Title: "no session",
			Status: "open", URL: "https://gh/1", LastActivityAt: t0},
	}
	// No session ID registered for the URL.

	panel := makePanel(reader)
	view := panel.View()
	if !strings.Contains(view, "-") {
		t.Errorf("expected '-' fallback session ID in view; got:\n%s", view)
	}
}

// ── unit tests for helper functions ──────────────────────────────────────────

func TestPrStatusDisplay(t *testing.T) {
	tests := []struct {
		status string
		draft  bool
		want   string
	}{
		{"open", true, "draft"},
		{"open", false, "open"},
		{"approved", false, "approved"},
		{"changes_requested", false, "changes requested"},
		{"changes requested", false, "changes requested"},
		{"merge_queued", false, "merge queued"},
		{"merge queued", false, "merge queued"},
		{"unknown_status", false, "unknown_status"},
	}
	for _, tt := range tests {
		got := prStatusDisplay(tt.status, tt.draft)
		if got != tt.want {
			t.Errorf("prStatusDisplay(%q, %v) = %q, want %q", tt.status, tt.draft, got, tt.want)
		}
	}
}

func TestPrCIDisplay(t *testing.T) {
	tests := []struct {
		state string
		want  string
	}{
		{"passing", "✓"},
		{"success", "✓"},
		{"failing", "✗"},
		{"failure", "✗"},
		{"running", "⟳"},
		{"in_progress", "⟳"},
		{"pending", "⟳"},
		{"", "—"},
		{"other", "—"},
	}
	for _, tt := range tests {
		got := prCIDisplay(tt.state)
		if got != tt.want {
			t.Errorf("prCIDisplay(%q) = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestPrReviewDisplay(t *testing.T) {
	tests := []struct {
		approved int
		changes  int
		want     string
	}{
		{0, 0, "—"},
		{1, 0, "✓1"},
		{0, 1, "✗1"},
		{2, 3, "✓2 ✗3"},
	}
	for _, tt := range tests {
		got := prReviewDisplay(tt.approved, tt.changes)
		if got != tt.want {
			t.Errorf("prReviewDisplay(%d, %d) = %q, want %q", tt.approved, tt.changes, got, tt.want)
		}
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{-1 * time.Minute, "0m"},
		{0, "0m"},
		{30 * time.Minute, "30m"},
		{59 * time.Minute, "59m"},
		{1 * time.Hour, "1h"},
		{23 * time.Hour, "23h"},
		{24 * time.Hour, "1d"},
		{48 * time.Hour, "2d"},
	}
	for _, tt := range tests {
		got := formatAge(tt.d)
		if got != tt.want {
			t.Errorf("formatAge(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

// TestRealClock_Now verifies that realClock.Now() returns a non-zero time.
func TestRealClock_Now(t *testing.T) {
	c := realClock{}
	now := c.Now()
	if now.IsZero() {
		t.Error("expected non-zero time from realClock.Now()")
	}
}

// TestNewMyPRsPanel verifies the exported constructor creates a usable panel.
func TestNewMyPRsPanel(t *testing.T) {
	reader := newStubPRReader()
	reader.prs = []persistence.PullRequest{
		{ID: "pr1", Repo: "repo", Number: 1, Title: "PR",
			Status: "open", URL: "https://gh/1", LastActivityAt: t0, Author: "me"},
	}
	reader.sessionIDs["https://gh/1"] = "a"

	panel := NewMyPRsPanel(reader, "me")
	if panel == nil {
		t.Fatal("expected non-nil panel")
	}
	if !panel.HasContent() {
		t.Error("expected HasContent() == true")
	}
}

// TestMyPRsPanel_ApprovedGreenHighlight verifies that an approved PR exercises
// the green color code path.
func TestMyPRsPanel_ApprovedGreenHighlight(t *testing.T) {
	reader := newStubPRReader()
	reader.prs = []persistence.PullRequest{
		{ID: "pr1", Repo: "repo", Number: 1, Title: "approved PR",
			Status: "approved", CIState: "passing", URL: "https://gh/1", LastActivityAt: t0},
	}
	reader.sessionIDs["https://gh/1"] = "a"
	panel := makePanel(reader)
	view := panel.View()
	if !strings.Contains(view, "approved PR") {
		t.Errorf("expected 'approved PR' in view; got:\n%s", view)
	}
}

// TestMyPRsPanel_RunningCIYellowHighlight verifies that a PR with running CI
// exercises the yellow color code path.
func TestMyPRsPanel_RunningCIYellowHighlight(t *testing.T) {
	reader := newStubPRReader()
	reader.prs = []persistence.PullRequest{
		{ID: "pr1", Repo: "repo", Number: 1, Title: "running CI PR",
			Status: "open", CIState: "running", URL: "https://gh/1", LastActivityAt: t0},
		{ID: "pr2", Repo: "repo", Number: 2, Title: "in_progress CI PR",
			Status: "open", CIState: "in_progress", URL: "https://gh/2", LastActivityAt: t0},
		{ID: "pr3", Repo: "repo", Number: 3, Title: "pending CI PR",
			Status: "open", CIState: "pending", URL: "https://gh/3", LastActivityAt: t0},
	}
	reader.sessionIDs["https://gh/1"] = "a"
	reader.sessionIDs["https://gh/2"] = "b"
	reader.sessionIDs["https://gh/3"] = "c"
	panel := makePanel(reader)
	view := panel.View()
	for _, title := range []string{"running CI PR", "in_progress CI PR", "pending CI PR"} {
		if !strings.Contains(view, title) {
			t.Errorf("expected %q in view; got:\n%s", title, view)
		}
	}
}

// TestMyPRsPanel_RefreshErrors verifies that refresh errors are returned and
// the panel gracefully handles them (rows unchanged).
func TestMyPRsPanel_RefreshErrors(t *testing.T) {
	t.Run("ListPullRequests error", func(t *testing.T) {
		reader := newStubPRReader()
		reader.listPRsErr = fmt.Errorf("db error")
		// Calling newMyPRsPanelWithClock ignores the error from refresh.
		panel := newMyPRsPanelWithClock(reader, "", stubClock{now: t2})
		if panel.HasContent() {
			t.Error("expected HasContent() == false after ListPullRequests error")
		}
	})

	t.Run("ListWatches error", func(t *testing.T) {
		reader := newStubPRReader()
		reader.prs = []persistence.PullRequest{
			{ID: "pr1", Repo: "repo", Number: 1, Title: "PR",
				Status: "open", URL: "https://gh/1", LastActivityAt: t0},
		}
		reader.listWatchesErr = fmt.Errorf("watches db error")
		panel := newMyPRsPanelWithClock(reader, "", stubClock{now: t2})
		if panel.HasContent() {
			t.Error("expected HasContent() == false after ListWatches error")
		}
	})
}

// TestMyPRsPanel_CursorClamping verifies the cursor is clamped when PRs are
// refreshed to a shorter list.
func TestMyPRsPanel_CursorClamping(t *testing.T) {
	reader := newStubPRReader()
	for i := 0; i < 3; i++ {
		url := fmt.Sprintf("https://gh/%d", i)
		reader.prs = append(reader.prs, persistence.PullRequest{
			ID: fmt.Sprintf("pr%d", i), Repo: "repo", Number: i,
			Title: "PR", Status: "open",
			URL: url, LastActivityAt: t0,
		})
		reader.sessionIDs[url] = fmt.Sprintf("%c", 'a'+i)
	}
	panel := makePanel(reader)

	// Move cursor to position 2.
	panel.Update(MoveFocusMsg{Down: true})
	panel.Update(MoveFocusMsg{Down: true})
	if panel.cursor != 2 {
		t.Fatalf("expected cursor=2, got %d", panel.cursor)
	}

	// Shrink the list to 1 PR and trigger a refresh via DBEventMsg.
	reader.prs = reader.prs[:1]
	pr := persistence.PullRequest{ID: "pr0", Repo: "repo", Number: 0}
	panel.Update(DBEventMsg{Event: eventbus.Event{Type: eventbus.PRUpdated, After: pr}})

	if panel.cursor != 0 {
		t.Errorf("expected cursor to clamp to 0 after list shrinks, got %d", panel.cursor)
	}
}

// TestMyPRsPanel_FlashRendering verifies that a flashing PR row is rendered
// (the Bold style branch in renderRow is exercised).
func TestMyPRsPanel_FlashRendering(t *testing.T) {
	reader := newStubPRReader()
	pr := persistence.PullRequest{
		ID: "pr1", Repo: "repo", Number: 1, Title: "flash render",
		Status: "open", CIState: "passing", URL: "https://gh/1", LastActivityAt: t0,
	}
	reader.prs = []persistence.PullRequest{pr}
	reader.sessionIDs["https://gh/1"] = "a"

	panel := makePanel(reader)

	// Mark as flashing by sending a DBEventMsg, then call View().
	panel.Update(DBEventMsg{Event: eventbus.Event{Type: eventbus.PRUpdated, After: pr}})
	view := panel.View()

	if !strings.Contains(view, "flash render") {
		t.Errorf("expected 'flash render' in view after flash; got:\n%s", view)
	}
}

// ── ResizeMsg / truncateTitle ─────────────────────────────────────────────────

// TestMyPRsPanel_ResizeMsg verifies that a ResizeMsg updates the panel's width.
func TestMyPRsPanel_ResizeMsg(t *testing.T) {
	reader := newStubPRReader()
	panel := makePanel(reader)
	sm, _ := panel.Update(ResizeMsg{Width: 120, Height: 20})
	p := sm.(*MyPRsPanel)
	if p.width != 120 {
		t.Errorf("width = %d, want 120", p.width)
	}
}

// TestTruncateTitle covers the full branch matrix of truncateTitle.
func TestTruncateTitle(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		width    int
		fixedLen int
		want     string
	}{
		{"zero width passthrough", "hello world", 0, 0, "hello world"},
		{"fits exactly", "hello", 10, 5, "hello"},
		{"fits with room", "hi", 20, 5, "hi"},
		{"truncated normal", "hello world extra", 15, 3, "hello world…"},
		{"maxTitle exactly 1", "hi", 6, 5, "…"},
		{"maxTitle zero", "hi", 5, 5, ""},
		{"negative maxTitle", "hi", 3, 5, ""},
		{"unicode title", "αβγδεζηθι", 12, 4, "αβγδεζη…"},
		{"emoji title", "👁fix: repair the thing", 12, 0, "👁fix: repai…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateTitle(tt.s, tt.width, tt.fixedLen)
			if got != tt.want {
				t.Errorf("truncateTitle(%q, %d, %d) = %q, want %q",
					tt.s, tt.width, tt.fixedLen, got, tt.want)
			}
		})
	}
}

// TestMyPRsPanel_TitleTruncation verifies that very long titles are truncated
// in the rendered row when a narrow width is set via ResizeMsg.
func TestMyPRsPanel_TitleTruncation(t *testing.T) {
	reader := newStubPRReader()
	longTitle := strings.Repeat("x", 200)
	reader.prs = []persistence.PullRequest{
		{ID: "p1", Repo: "r", Number: 1, Title: longTitle, URL: "u1", LastActivityAt: t0},
	}
	reader.sessionIDs["u1"] = "a"
	panel := makePanel(reader)
	panel.Update(ResizeMsg{Width: 60, Height: 10})
	view := panel.View()
	for _, line := range strings.Split(view, "\n") {
		// A raw rune-length check is imperfect against styled strings, but the
		// title should not appear in full in any single line.
		if strings.Contains(line, longTitle) {
			t.Errorf("long title not truncated in view line: %q", line)
		}
	}
}

// TestMyPRsPanel_SelectedPR verifies that SelectedPR returns the PR under the
// cursor or nil when the panel is empty.
func TestMyPRsPanel_SelectedPR(t *testing.T) {
	t.Run("empty panel returns nil", func(t *testing.T) {
		panel := makePanel(newStubPRReader())
		if got := panel.SelectedPR(); got != nil {
			t.Errorf("SelectedPR() = %v, want nil", got)
		}
	})

	t.Run("returns PR at cursor 0", func(t *testing.T) {
		reader := newStubPRReader()
		reader.prs = []persistence.PullRequest{
			{ID: "p1", Repo: "r", Number: 1, Title: "first", URL: "u1", LastActivityAt: t0},
			{ID: "p2", Repo: "r", Number: 2, Title: "second", URL: "u2", LastActivityAt: t0.Add(time.Second)},
		}
		panel := makePanel(reader)
		got := panel.SelectedPR()
		if got == nil {
			t.Fatal("SelectedPR() = nil, want non-nil")
		}
		if got.ID != "p1" {
			t.Errorf("SelectedPR().ID = %q, want %q", got.ID, "p1")
		}
	})

	t.Run("returns PR at cursor after down move", func(t *testing.T) {
		reader := newStubPRReader()
		reader.prs = []persistence.PullRequest{
			{ID: "p1", Repo: "r", Number: 1, Title: "first", URL: "u1", LastActivityAt: t0},
			{ID: "p2", Repo: "r", Number: 2, Title: "second", URL: "u2", LastActivityAt: t0.Add(time.Second)},
		}
		panel := makePanel(reader)
		panel.Update(MoveFocusMsg{Down: true})
		got := panel.SelectedPR()
		if got == nil {
			t.Fatal("SelectedPR() = nil, want non-nil")
		}
		if got.ID != "p2" {
			t.Errorf("SelectedPR().ID = %q, want %q", got.ID, "p2")
		}
	})
}

// TestMyPRsPanel_ExcludesOtherAuthorPRs verifies that PRs not authored by the
// current user are excluded from the panel when a username is set.
func TestMyPRsPanel_ExcludesOtherAuthorPRs(t *testing.T) {
	tests := []struct {
		name     string
		username string
		prs      []persistence.PullRequest
		wantIDs  []string
	}{
		{
			name:     "matching author included",
			username: "me",
			prs: []persistence.PullRequest{
				{ID: "pr1", Author: "me", Repo: "repo", Number: 1, Title: "my PR",
					Status: "open", URL: "https://gh/1", LastActivityAt: t0},
			},
			wantIDs: []string{"pr1"},
		},
		{
			name:     "non-matching author excluded",
			username: "me",
			prs: []persistence.PullRequest{
				{ID: "pr1", Author: "other", Repo: "repo", Number: 1, Title: "other PR",
					Status: "open", URL: "https://gh/1", LastActivityAt: t0},
			},
			wantIDs: nil,
		},
		{
			name:     "mixed set",
			username: "me",
			prs: []persistence.PullRequest{
				{ID: "pr1", Author: "me", Repo: "repo", Number: 1, Title: "mine",
					Status: "open", URL: "https://gh/1", LastActivityAt: t0},
				{ID: "pr2", Author: "other", Repo: "repo", Number: 2, Title: "theirs",
					Status: "open", URL: "https://gh/2", LastActivityAt: t1},
				{ID: "pr3", Author: "me", Repo: "repo", Number: 3, Title: "also mine",
					Status: "open", URL: "https://gh/3", LastActivityAt: t2},
			},
			wantIDs: []string{"pr1", "pr3"},
		},
		{
			name:     "empty username bypasses filter",
			username: "",
			prs: []persistence.PullRequest{
				{ID: "pr1", Author: "anyone", Repo: "repo", Number: 1, Title: "PR",
					Status: "open", URL: "https://gh/1", LastActivityAt: t0},
			},
			wantIDs: []string{"pr1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := newStubPRReader()
			reader.prs = tt.prs
			for _, pr := range tt.prs {
				reader.sessionIDs[pr.URL] = "s"
			}
			panel := newMyPRsPanelWithClock(reader, tt.username, stubClock{now: t2})
			if len(panel.rows) != len(tt.wantIDs) {
				t.Fatalf("got %d rows, want %d", len(panel.rows), len(tt.wantIDs))
			}
			for i, wantID := range tt.wantIDs {
				if panel.rows[i].pr.ID != wantID {
					t.Errorf("row[%d].pr.ID = %q, want %q", i, panel.rows[i].pr.ID, wantID)
				}
			}
		})
	}
}

// ── CursorPosition / SetCursor tests ──────────────────────────────────────────

func TestMyPRsPanel_CursorPosition(t *testing.T) {
	reader := newStubPRReader()
	reader.prs = []persistence.PullRequest{
		{ID: "pr1", URL: "u1", LastActivityAt: t0},
		{ID: "pr2", URL: "u2", LastActivityAt: t1},
	}
	reader.sessionIDs["u1"] = "a"
	reader.sessionIDs["u2"] = "b"
	panel := makePanel(reader)

	if panel.CursorPosition() != 0 {
		t.Errorf("initial CursorPosition = %d, want 0", panel.CursorPosition())
	}

	panel.Update(MoveFocusMsg{Down: true})
	if panel.CursorPosition() != 1 {
		t.Errorf("after j CursorPosition = %d, want 1", panel.CursorPosition())
	}
}

func TestMyPRsPanel_SetCursor(t *testing.T) {
	reader := newStubPRReader()
	reader.prs = []persistence.PullRequest{
		{ID: "pr1", URL: "u1", LastActivityAt: t0},
		{ID: "pr2", URL: "u2", LastActivityAt: t1},
		{ID: "pr3", URL: "u3", LastActivityAt: t2},
	}
	reader.sessionIDs["u1"] = "a"
	reader.sessionIDs["u2"] = "b"
	reader.sessionIDs["u3"] = "c"
	panel := makePanel(reader)

	tests := []struct {
		name    string
		set     int
		wantPos int
	}{
		{"set to 0", 0, 0},
		{"set to 1", 1, 1},
		{"set to last", 2, 2},
		{"negative clamped to 0", -1, 0},
		{"beyond end clamped", 10, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			panel.SetCursor(tt.set)
			if panel.CursorPosition() != tt.wantPos {
				t.Errorf("SetCursor(%d) → CursorPosition() = %d, want %d",
					tt.set, panel.CursorPosition(), tt.wantPos)
			}
		})
	}
}

func TestMyPRsPanel_SetCursor_EmptyPanel(t *testing.T) {
	reader := newStubPRReader()
	panel := makePanel(reader)

	panel.SetCursor(5)
	if panel.CursorPosition() != 0 {
		t.Errorf("SetCursor on empty panel: CursorPosition = %d, want 0", panel.CursorPosition())
	}
}
