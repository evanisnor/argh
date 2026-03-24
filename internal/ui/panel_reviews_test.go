package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func makeReviewPanel(reader *stubPRReader, username string) *ReviewQueuePanel {
	return newReviewQueuePanelWithClock(reader, username, stubClock{now: t2})
}

// ── urgency formula tests ─────────────────────────────────────────────────────

func TestCalculateUrgency(t *testing.T) {
	tests := []struct {
		name       string
		staleness  time.Duration
		ciState    string
		authorWait time.Duration
		want       int
	}{
		// Base staleness variants.
		{"base <4h, no CI, no author wait", 1 * time.Hour, "", 1 * time.Hour, 1},
		{"base 4h, no CI, no author wait", 4 * time.Hour, "", 1 * time.Hour, 2},
		{"base >24h, no CI, no author wait", 25 * time.Hour, "", 1 * time.Hour, 3},

		// CI modifier: failing adds 1.
		{"base=1 + ci_fail = 2", 1 * time.Hour, "failing", 1 * time.Hour, 2},
		{"base=1 + ci_failure alias = 2", 1 * time.Hour, "failure", 1 * time.Hour, 2},
		{"base=2 + ci_fail = 3", 10 * time.Hour, "failing", 1 * time.Hour, 3},
		{"base=3 + ci_fail clamped to 3", 25 * time.Hour, "failing", 1 * time.Hour, 3},

		// CI modifier: passing subtracts 1.
		{"base=3 + ci_pass = 2", 25 * time.Hour, "passing", 1 * time.Hour, 2},
		{"base=3 + ci_success alias = 2", 25 * time.Hour, "success", 1 * time.Hour, 2},
		{"base=1 + ci_pass clamped to 1", 1 * time.Hour, "passing", 1 * time.Hour, 1},
		{"base=2 + ci_pass = 1", 10 * time.Hour, "passing", 1 * time.Hour, 1},

		// CI modifier: running/none has no effect.
		{"base=2 + ci_running = 2", 10 * time.Hour, "running", 1 * time.Hour, 2},
		{"base=2 + ci_in_progress = 2", 10 * time.Hour, "in_progress", 1 * time.Hour, 2},
		{"base=2 + ci_pending = 2", 10 * time.Hour, "pending", 1 * time.Hour, 2},
		{"base=2 + ci_unknown = 2", 10 * time.Hour, "unknown", 1 * time.Hour, 2},

		// Author wait modifier: >72h adds 1.
		{"base=1 + author_wait_73h = 2", 1 * time.Hour, "", 73 * time.Hour, 2},
		{"base=1 + author_wait_72h no change", 1 * time.Hour, "", 72 * time.Hour, 1},
		{"base=3 + author_wait clamped to 3", 25 * time.Hour, "", 73 * time.Hour, 3},

		// Combined: base=1 + ci_fail + author_wait → clamped to 3 (not 4).
		{"base=1 + ci_fail + author_wait → 3 (clamped)", 1 * time.Hour, "failing", 73 * time.Hour, 3},

		// Combined: base=3 + ci_pass → 2 (not 1, since clamped ≥1).
		{"base=3 + ci_pass = 2 (not below 1)", 25 * time.Hour, "passing", 1 * time.Hour, 2},

		// Combined: base=1 + ci_pass → clamped to 1 (not 0).
		{"base=1 + ci_pass clamped to 1 (not 0)", 1 * time.Hour, "passing", 1 * time.Hour, 1},

		// Zero staleness.
		{"zero staleness → base=1", 0, "", 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateUrgency(tt.staleness, tt.ciState, tt.authorWait)
			if got != tt.want {
				t.Errorf("calculateUrgency(%v, %q, %v) = %d, want %d",
					tt.staleness, tt.ciState, tt.authorWait, got, tt.want)
			}
		})
	}
}

// ── urgency display tests ─────────────────────────────────────────────────────

func TestUrgencyDisplay(t *testing.T) {
	tests := []struct {
		score int
		want  string
	}{
		{1, "●○○"},
		{2, "●●○"},
		{3, "●●●"},
		{0, "●○○"}, // below-range defaults to lowest
		{4, "●○○"}, // above-range falls to default case
	}
	for _, tt := range tests {
		got := urgencyDisplay(tt.score)
		if got != tt.want {
			t.Errorf("urgencyDisplay(%d) = %q, want %q", tt.score, got, tt.want)
		}
	}
}

// ── isLastRequiredReviewer tests ──────────────────────────────────────────────

func TestIsLastRequiredReviewer(t *testing.T) {
	tests := []struct {
		name      string
		reviewers []persistence.Reviewer
		username  string
		want      bool
	}{
		{"empty reviewers", nil, "alice", false},
		{"empty username", []persistence.Reviewer{{Login: "alice", State: "PENDING"}}, "", false},
		{"user is only PENDING reviewer", []persistence.Reviewer{
			{Login: "alice", State: "PENDING"},
		}, "alice", true},
		{"user is one of two PENDING reviewers", []persistence.Reviewer{
			{Login: "alice", State: "PENDING"},
			{Login: "bob", State: "PENDING"},
		}, "alice", false},
		{"user has already reviewed (APPROVED)", []persistence.Reviewer{
			{Login: "alice", State: "APPROVED"},
		}, "alice", false},
		{"user not in reviewer list", []persistence.Reviewer{
			{Login: "bob", State: "PENDING"},
		}, "alice", false},
		{"one PENDING (not user), user APPROVED", []persistence.Reviewer{
			{Login: "alice", State: "APPROVED"},
			{Login: "bob", State: "PENDING"},
		}, "alice", false},
		{"user is last pending among mixed states", []persistence.Reviewer{
			{Login: "alice", State: "PENDING"},
			{Login: "bob", State: "APPROVED"},
			{Login: "charlie", State: "CHANGES_REQUESTED"},
		}, "alice", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLastRequiredReviewer(tt.reviewers, tt.username)
			if got != tt.want {
				t.Errorf("isLastRequiredReviewer(%v, %q) = %v, want %v",
					tt.reviewers, tt.username, got, tt.want)
			}
		})
	}
}

// ── isReadyToReview tests ─────────────────────────────────────────────────────

func TestIsReadyToReview(t *testing.T) {
	tests := []struct {
		name      string
		ciState   string
		reviewers []persistence.Reviewer
		want      bool
	}{
		{"CI passing, no reviewers", "passing", nil, true},
		{"CI success alias, no reviewers", "success", nil, true},
		{"CI failing", "failing", nil, false},
		{"CI running", "running", nil, false},
		{"CI empty", "", nil, false},
		{"CI passing with APPROVED reviewer", "passing", []persistence.Reviewer{
			{Login: "alice", State: "APPROVED"},
		}, true},
		{"CI passing with CHANGES_REQUESTED", "passing", []persistence.Reviewer{
			{Login: "alice", State: "CHANGES_REQUESTED"},
		}, false},
		{"CI passing with PENDING reviewer", "passing", []persistence.Reviewer{
			{Login: "alice", State: "PENDING"},
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isReadyToReview(tt.ciState, tt.reviewers)
			if got != tt.want {
				t.Errorf("isReadyToReview(%q, %v) = %v, want %v",
					tt.ciState, tt.reviewers, got, tt.want)
			}
		})
	}
}

// ── panel behaviour tests ─────────────────────────────────────────────────────

// TestReviewQueuePanel_SortedByUrgencyDescending verifies rows are sorted with
// highest urgency first.
func TestReviewQueuePanel_SortedByUrgencyDescending(t *testing.T) {
	reader := newStubPRReader()
	// PR1: staleness <4h → base=1, no CI modifier → urgency=1
	// PR2: staleness >24h → base=3, no CI modifier → urgency=3
	// PR3: staleness 4-24h → base=2, no CI modifier → urgency=2
	pr1 := persistence.PullRequest{
		ID: "pr1", Repo: "repo", Number: 1, Title: "low urgency",
		Status: "open", URL: "https://gh/1",
		LastActivityAt: t2.Add(-1 * time.Hour),  // 1h ago
		CreatedAt:      t2.Add(-1 * time.Hour),
	}
	pr2 := persistence.PullRequest{
		ID: "pr2", Repo: "repo", Number: 2, Title: "high urgency",
		Status: "open", URL: "https://gh/2",
		LastActivityAt: t2.Add(-48 * time.Hour), // 48h ago
		CreatedAt:      t2.Add(-48 * time.Hour),
	}
	pr3 := persistence.PullRequest{
		ID: "pr3", Repo: "repo", Number: 3, Title: "medium urgency",
		Status: "open", URL: "https://gh/3",
		LastActivityAt: t2.Add(-12 * time.Hour), // 12h ago
		CreatedAt:      t2.Add(-12 * time.Hour),
	}
	reader.prs = []persistence.PullRequest{pr1, pr2, pr3}
	reader.sessionIDs["https://gh/1"] = "a"
	reader.sessionIDs["https://gh/2"] = "b"
	reader.sessionIDs["https://gh/3"] = "c"

	panel := makeReviewPanel(reader, "")
	view := panel.View()

	highIdx := strings.Index(view, "high urgency")
	medIdx := strings.Index(view, "medium urgency")
	lowIdx := strings.Index(view, "low urgency")

	if highIdx == -1 || medIdx == -1 || lowIdx == -1 {
		t.Fatalf("expected all three rows in view; got:\n%s", view)
	}
	if highIdx > medIdx {
		t.Errorf("expected 'high urgency' (urgency=3) before 'medium urgency' (urgency=2)")
	}
	if medIdx > lowIdx {
		t.Errorf("expected 'medium urgency' (urgency=2) before 'low urgency' (urgency=1)")
	}
}

// TestReviewQueuePanel_RowCount verifies RowCount() returns the number of review rows.
func TestReviewQueuePanel_RowCount(t *testing.T) {
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
					URL: url, LastActivityAt: t0, CreatedAt: t0,
				})
				reader.sessionIDs[url] = fmt.Sprintf("s%d", i)
			}
			panel := makeReviewPanel(reader, "")
			if got := panel.RowCount(); got != tt.want {
				t.Errorf("RowCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestReviewQueuePanel_HeaderRow verifies the header line contains column labels
// and separator characters.
func TestReviewQueuePanel_HeaderRow(t *testing.T) {
	reader := newStubPRReader()
	reader.prs = []persistence.PullRequest{
		{ID: "pr1", Repo: "repo", Number: 1, Title: "PR",
			Status: "open", URL: "https://gh/1", LastActivityAt: t0, CreatedAt: t0},
	}
	reader.sessionIDs["https://gh/1"] = "a"
	panel := makeReviewPanel(reader, "")
	view := panel.View()

	for _, label := range []string{"REPO", "#", "TITLE", "@", "⚙", "⏱", "!!"} {
		if !strings.Contains(view, label) {
			t.Errorf("header missing label %q in view:\n%s", label, view)
		}
	}
	if !strings.Contains(view, "│") {
		t.Errorf("header missing separator │ in view:\n%s", view)
	}
}

// TestReviewQueuePanel_LastRequiredReviewerHighlight verifies that the
// last-required-reviewer highlight code path is exercised.
func TestReviewQueuePanel_LastRequiredReviewerHighlight(t *testing.T) {
	reader := newStubPRReader()
	reader.prs = []persistence.PullRequest{
		{ID: "pr1", Repo: "repo", Number: 1, Title: "last reviewer PR",
			Status: "open", URL: "https://gh/1",
			LastActivityAt: t0, CreatedAt: t0},
	}
	reader.sessionIDs["https://gh/1"] = "a"
	// alice is the only PENDING reviewer.
	reader.reviewers["pr1"] = []persistence.Reviewer{
		{Login: "alice", State: "PENDING"},
	}

	panel := makeReviewPanel(reader, "alice")
	view := panel.View()

	if !strings.Contains(view, "last reviewer PR") {
		t.Errorf("expected row to appear in view; got:\n%s", view)
	}
	// Verify the row was processed as last-reviewer (no panic, row rendered).
	if len(panel.rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(panel.rows))
	}
	if !panel.rows[0].isLastReviewer {
		t.Error("expected isLastReviewer=true for alice as the only PENDING reviewer")
	}
}

// TestReviewQueuePanel_ReadyToReviewHighlight verifies the ready-to-review
// code path is exercised when CI is passing and no CHANGES_REQUESTED.
func TestReviewQueuePanel_ReadyToReviewHighlight(t *testing.T) {
	reader := newStubPRReader()
	reader.prs = []persistence.PullRequest{
		{ID: "pr1", Repo: "repo", Number: 1, Title: "ready PR",
			Status: "open", CIState: "passing", URL: "https://gh/1",
			LastActivityAt: t0, CreatedAt: t0},
	}
	reader.sessionIDs["https://gh/1"] = "a"

	panel := makeReviewPanel(reader, "")
	view := panel.View()

	if !strings.Contains(view, "ready PR") {
		t.Errorf("expected row to appear in view; got:\n%s", view)
	}
	if len(panel.rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(panel.rows))
	}
	if !panel.rows[0].readyToReview {
		t.Error("expected readyToReview=true for passing CI with no blockers")
	}
}

// TestReviewQueuePanel_WatchIcon verifies the 👁 icon is present when the PR
// has active watches, and absent when it does not.
func TestReviewQueuePanel_WatchIcon(t *testing.T) {
	t.Run("with active watch", func(t *testing.T) {
		reader := newStubPRReader()
		url := "https://gh/1"
		reader.prs = []persistence.PullRequest{
			{ID: "pr1", Repo: "repo", Number: 1, Title: "watched",
				Status: "open", URL: url, LastActivityAt: t0, CreatedAt: t0},
		}
		reader.sessionIDs[url] = "a"
		reader.watches = []persistence.Watch{
			{ID: "w1", PRURL: url, Status: "waiting"},
		}

		panel := makeReviewPanel(reader, "")
		view := panel.View()

		if !strings.Contains(view, "👁") {
			t.Errorf("expected 👁 icon when watch is active; view:\n%s", view)
		}
	})

	t.Run("without watch", func(t *testing.T) {
		reader := newStubPRReader()
		url := "https://gh/2"
		reader.prs = []persistence.PullRequest{
			{ID: "pr2", Repo: "repo", Number: 2, Title: "unwatched",
				Status: "open", URL: url, LastActivityAt: t0, CreatedAt: t0},
		}
		reader.sessionIDs[url] = "a"

		panel := makeReviewPanel(reader, "")
		view := panel.View()

		if strings.Contains(view, "👁") {
			t.Errorf("did not expect 👁 icon when no active watch; view:\n%s", view)
		}
	})

	t.Run("scheduled watch is active", func(t *testing.T) {
		reader := newStubPRReader()
		url := "https://gh/3"
		reader.prs = []persistence.PullRequest{
			{ID: "pr3", Repo: "repo", Number: 3, Title: "scheduled watch PR",
				Status: "open", URL: url, LastActivityAt: t0, CreatedAt: t0},
		}
		reader.sessionIDs[url] = "a"
		reader.watches = []persistence.Watch{
			{ID: "w2", PRURL: url, Status: "scheduled"},
		}

		panel := makeReviewPanel(reader, "")
		view := panel.View()

		if !strings.Contains(view, "👁") {
			t.Errorf("expected 👁 icon for scheduled watch; view:\n%s", view)
		}
	})

	t.Run("fired watch is not active", func(t *testing.T) {
		reader := newStubPRReader()
		url := "https://gh/4"
		reader.prs = []persistence.PullRequest{
			{ID: "pr4", Repo: "repo", Number: 4, Title: "fired watch PR",
				Status: "open", URL: url, LastActivityAt: t0, CreatedAt: t0},
		}
		reader.sessionIDs[url] = "a"
		reader.watches = []persistence.Watch{
			{ID: "w3", PRURL: url, Status: "fired"},
		}

		panel := makeReviewPanel(reader, "")
		view := panel.View()

		if strings.Contains(view, "👁") {
			t.Errorf("did not expect 👁 icon for fired watch; view:\n%s", view)
		}
	})
}

// TestReviewQueuePanel_DBEventMsg_Flash verifies that a DB event marks the
// affected PR as flashing and triggers a refresh.
func TestReviewQueuePanel_DBEventMsg_Flash(t *testing.T) {
	reader := newStubPRReader()
	pr := persistence.PullRequest{
		ID: "pr1", Repo: "repo", Number: 1, Title: "flash me",
		Status: "open", CIState: "passing", URL: "https://gh/1",
		LastActivityAt: t0, CreatedAt: t0,
	}
	reader.prs = []persistence.PullRequest{pr}
	reader.sessionIDs["https://gh/1"] = "a"

	panel := makeReviewPanel(reader, "")

	events := []eventbus.EventType{eventbus.PRUpdated, eventbus.CIChanged, eventbus.ReviewChanged}
	for _, evType := range events {
		panel.flashing = make(map[string]bool)

		msg := DBEventMsg{Event: eventbus.Event{Type: evType, After: pr}}
		updated, _ := panel.Update(msg)
		updatedPanel := updated.(*ReviewQueuePanel)

		if !updatedPanel.flashing["pr1"] {
			t.Errorf("event type %s: expected PR 'pr1' to be flashing", evType)
		}
	}
}

// TestReviewQueuePanel_DBEventMsg_NoFlashWithoutPR verifies that a DB event
// without a PullRequest in After does not set the flashing state.
func TestReviewQueuePanel_DBEventMsg_NoFlashWithoutPR(t *testing.T) {
	reader := newStubPRReader()
	panel := makeReviewPanel(reader, "")

	msg := DBEventMsg{Event: eventbus.Event{Type: eventbus.PRUpdated, After: "not-a-pr"}}
	updated, _ := panel.Update(msg)
	updatedPanel := updated.(*ReviewQueuePanel)

	if len(updatedPanel.flashing) != 0 {
		t.Errorf("expected no flashing entries, got: %v", updatedPanel.flashing)
	}
}

// TestReviewQueuePanel_DBEventMsg_PRRemoved_RefreshesNoFlash verifies that a PRRemoved
// event triggers a refresh but does not set flash (After is nil).
func TestReviewQueuePanel_DBEventMsg_PRRemoved_RefreshesNoFlash(t *testing.T) {
	reader := newStubPRReader()
	panel := makeReviewPanel(reader, "")

	msg := DBEventMsg{Event: eventbus.Event{
		Type:   eventbus.PRRemoved,
		Before: persistence.PullRequest{ID: "pr1", Number: 1},
		After:  nil,
	}}
	updated, _ := panel.Update(msg)
	updatedPanel := updated.(*ReviewQueuePanel)

	if len(updatedPanel.flashing) != 0 {
		t.Errorf("expected no flashing entries for PRRemoved, got: %v", updatedPanel.flashing)
	}
}

// TestReviewQueuePanel_DBEventMsg_SessionIDsAssigned_RefreshesNoFlash verifies that a
// SessionIDsAssigned event triggers a refresh but does not set flash.
func TestReviewQueuePanel_DBEventMsg_SessionIDsAssigned_RefreshesNoFlash(t *testing.T) {
	reader := newStubPRReader()
	panel := makeReviewPanel(reader, "")

	msg := DBEventMsg{Event: eventbus.Event{Type: eventbus.SessionIDsAssigned}}
	updated, _ := panel.Update(msg)
	updatedPanel := updated.(*ReviewQueuePanel)

	if len(updatedPanel.flashing) != 0 {
		t.Errorf("expected no flashing entries for SessionIDsAssigned, got: %v", updatedPanel.flashing)
	}
}

// TestReviewQueuePanel_MoveFocus verifies j/k navigation stays within bounds.
func TestReviewQueuePanel_MoveFocus(t *testing.T) {
	reader := newStubPRReader()
	for i := 0; i < 3; i++ {
		url := fmt.Sprintf("https://gh/%d", i)
		reader.prs = append(reader.prs, persistence.PullRequest{
			ID: fmt.Sprintf("pr%d", i), Repo: "repo", Number: i,
			Title: "PR", Status: "open",
			URL: url, LastActivityAt: t0, CreatedAt: t0,
		})
		reader.sessionIDs[url] = fmt.Sprintf("%c", 'a'+i)
	}

	panel := makeReviewPanel(reader, "")

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

// TestReviewQueuePanel_EmptyView verifies the empty-state message when no PRs.
func TestReviewQueuePanel_EmptyView(t *testing.T) {
	reader := newStubPRReader()
	panel := makeReviewPanel(reader, "")
	view := panel.View()
	if !strings.Contains(view, "no reviews requested") {
		t.Errorf("expected empty-state message; got:\n%s", view)
	}
}

// TestReviewQueuePanel_HasContent verifies HasContent reflects row presence.
func TestReviewQueuePanel_HasContent(t *testing.T) {
	t.Run("no PRs", func(t *testing.T) {
		reader := newStubPRReader()
		panel := makeReviewPanel(reader, "")
		if panel.HasContent() {
			t.Error("expected HasContent() == false with no PRs")
		}
	})
	t.Run("with PRs", func(t *testing.T) {
		reader := newStubPRReader()
		reader.prs = []persistence.PullRequest{
			{ID: "pr1", Repo: "repo", Number: 1, Title: "PR", Status: "open",
				URL: "https://gh/1", LastActivityAt: t0, CreatedAt: t0},
		}
		reader.sessionIDs["https://gh/1"] = "a"
		panel := makeReviewPanel(reader, "")
		if !panel.HasContent() {
			t.Error("expected HasContent() == true with one PR")
		}
	})
}

// TestReviewQueuePanel_SessionIDFallback verifies that a missing session ID
// shows "-".
func TestReviewQueuePanel_SessionIDFallback(t *testing.T) {
	reader := newStubPRReader()
	reader.prs = []persistence.PullRequest{
		{ID: "pr1", Repo: "repo", Number: 1, Title: "no session",
			Status: "open", URL: "https://gh/1", LastActivityAt: t0, CreatedAt: t0},
	}
	// No session ID registered for the URL.

	panel := makeReviewPanel(reader, "")
	view := panel.View()
	if !strings.Contains(view, "-") {
		t.Errorf("expected '-' fallback session ID in view; got:\n%s", view)
	}
}

// TestReviewQueuePanel_CursorClamping verifies the cursor is clamped when PRs
// are refreshed to a shorter list.
func TestReviewQueuePanel_CursorClamping(t *testing.T) {
	reader := newStubPRReader()
	for i := 0; i < 3; i++ {
		url := fmt.Sprintf("https://gh/%d", i)
		reader.prs = append(reader.prs, persistence.PullRequest{
			ID: fmt.Sprintf("pr%d", i), Repo: "repo", Number: i,
			Title: "PR", Status: "open",
			URL: url, LastActivityAt: t0, CreatedAt: t0,
		})
		reader.sessionIDs[url] = fmt.Sprintf("%c", 'a'+i)
	}
	panel := makeReviewPanel(reader, "")

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

// TestReviewQueuePanel_FlashRendering verifies that a flashing PR row is rendered.
func TestReviewQueuePanel_FlashRendering(t *testing.T) {
	reader := newStubPRReader()
	pr := persistence.PullRequest{
		ID: "pr1", Repo: "repo", Number: 1, Title: "flash render",
		Status: "open", CIState: "passing", URL: "https://gh/1",
		LastActivityAt: t0, CreatedAt: t0,
	}
	reader.prs = []persistence.PullRequest{pr}
	reader.sessionIDs["https://gh/1"] = "a"

	panel := makeReviewPanel(reader, "")

	// Mark as flashing by sending a DBEventMsg, then call View().
	panel.Update(DBEventMsg{Event: eventbus.Event{Type: eventbus.PRUpdated, After: pr}})
	view := panel.View()

	if !strings.Contains(view, "flash render") {
		t.Errorf("expected 'flash render' in view after flash; got:\n%s", view)
	}
}

// TestReviewQueuePanel_FocusedRowHighlight verifies the focused row is rendered.
func TestReviewQueuePanel_FocusedRowHighlight(t *testing.T) {
	reader := newStubPRReader()
	reader.prs = []persistence.PullRequest{
		{ID: "pr1", Repo: "repo", Number: 1, Title: "focused PR",
			Status: "open", URL: "https://gh/1", LastActivityAt: t0, CreatedAt: t0},
	}
	reader.sessionIDs["https://gh/1"] = "a"

	panel := makeReviewPanel(reader, "")
	panel.SetFocused(true)
	// cursor starts at 0 → focused row code path is exercised.
	view := panel.View()
	if !strings.Contains(view, "focused PR") {
		t.Errorf("expected 'focused PR' in view; got:\n%s", view)
	}
}

// TestReviewQueuePanel_RefreshErrors verifies that refresh errors are handled
// gracefully.
func TestReviewQueuePanel_RefreshErrors(t *testing.T) {
	t.Run("ListPullRequests error", func(t *testing.T) {
		reader := newStubPRReader()
		reader.listPRsErr = fmt.Errorf("db error")
		panel := newReviewQueuePanelWithClock(reader, "", stubClock{now: t2})
		if panel.HasContent() {
			t.Error("expected HasContent() == false after ListPullRequests error")
		}
	})

	t.Run("ListWatches error", func(t *testing.T) {
		reader := newStubPRReader()
		reader.prs = []persistence.PullRequest{
			{ID: "pr1", Repo: "repo", Number: 1, Title: "PR",
				Status: "open", URL: "https://gh/1", LastActivityAt: t0, CreatedAt: t0},
		}
		reader.listWatchesErr = fmt.Errorf("watches db error")
		panel := newReviewQueuePanelWithClock(reader, "", stubClock{now: t2})
		if panel.HasContent() {
			t.Error("expected HasContent() == false after ListWatches error")
		}
	})
}

// TestReviewQueuePanel_RefreshMsg verifies that a RefreshMsg reloads data from the reader.
func TestReviewQueuePanel_RefreshMsg(t *testing.T) {
	reader := newStubPRReader()
	panel := newReviewQueuePanelWithClock(reader, "", stubClock{now: t2})
	if panel.HasContent() {
		t.Fatal("expected no content initially")
	}
	reader.prs = []persistence.PullRequest{
		{ID: "pr1", Repo: "repo", Number: 1, Title: "PR", Status: "open", URL: "https://gh/1", LastActivityAt: t0},
	}
	panel.Update(RefreshMsg{})
	if !panel.HasContent() {
		t.Error("expected HasContent() == true after RefreshMsg")
	}
}

// TestNewReviewQueuePanel verifies the exported constructor creates a usable panel.
func TestNewReviewQueuePanel(t *testing.T) {
	reader := newStubPRReader()
	reader.prs = []persistence.PullRequest{
		{ID: "pr1", Repo: "repo", Number: 1, Title: "PR",
			Status: "open", URL: "https://gh/1", LastActivityAt: t0, CreatedAt: t0},
	}
	reader.sessionIDs["https://gh/1"] = "a"

	panel := NewReviewQueuePanel(reader, "alice")
	if panel == nil {
		t.Fatal("expected non-nil panel")
	}
	if !panel.HasContent() {
		t.Error("expected HasContent() == true")
	}
}

// TestReviewQueuePanel_UrgencySymbolsInView verifies the urgency dot display
// symbols appear in the rendered view.
func TestReviewQueuePanel_UrgencySymbolsInView(t *testing.T) {
	tests := []struct {
		name           string
		lastActivityAt time.Time
		ciState        string
		createdAt      time.Time
		wantSymbol     string
	}{
		{
			name:           "urgency 1",
			lastActivityAt: t2.Add(-1 * time.Hour),
			ciState:        "passing",
			createdAt:      t2.Add(-1 * time.Hour),
			wantSymbol:     "●○○",
		},
		{
			name:           "urgency 2",
			lastActivityAt: t2.Add(-12 * time.Hour),
			ciState:        "",
			createdAt:      t2.Add(-12 * time.Hour),
			wantSymbol:     "●●○",
		},
		{
			name:           "urgency 3",
			lastActivityAt: t2.Add(-48 * time.Hour),
			ciState:        "",
			createdAt:      t2.Add(-48 * time.Hour),
			wantSymbol:     "●●●",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := newStubPRReader()
			reader.prs = []persistence.PullRequest{
				{ID: "pr1", Repo: "repo", Number: 1, Title: "PR",
					Status: "open", CIState: tt.ciState,
					URL: "https://gh/1",
					LastActivityAt: tt.lastActivityAt,
					CreatedAt:      tt.createdAt},
			}
			reader.sessionIDs["https://gh/1"] = "a"
			panel := makeReviewPanel(reader, "")
			view := panel.View()
			if !strings.Contains(view, tt.wantSymbol) {
				t.Errorf("expected urgency symbol %q in view, got:\n%s", tt.wantSymbol, view)
			}
		})
	}
}

// ── ResizeMsg ────────────────────────────────────────────────────────────────

// TestReviewQueuePanel_ResizeMsg verifies that a ResizeMsg updates the panel's
// allocated width.
func TestReviewQueuePanel_ResizeMsg(t *testing.T) {
	reader := newStubPRReader()
	panel := makeReviewPanel(reader, "me")
	sm, _ := panel.Update(ResizeMsg{Width: 80, Height: 15})
	p := sm.(*ReviewQueuePanel)
	if p.width != 80 {
		t.Errorf("width = %d, want 80", p.width)
	}
}

// TestReviewQueuePanel_TitleTruncation verifies that very long titles are
// truncated when a narrow width is set via ResizeMsg.
func TestReviewQueuePanel_TitleTruncation(t *testing.T) {
	reader := newStubPRReader()
	longTitle := strings.Repeat("y", 200)
	reader.prs = []persistence.PullRequest{
		{ID: "p1", Repo: "r", Number: 1, Title: longTitle, URL: "u1",
			Author: "alice", LastActivityAt: t0, CreatedAt: t0},
	}
	reader.sessionIDs["u1"] = "a"
	panel := makeReviewPanel(reader, "me")
	panel.Update(ResizeMsg{Width: 60, Height: 10})
	view := panel.View()
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, longTitle) {
			t.Errorf("long title not truncated in view line: %q", line)
		}
	}
}

// TestReviewQueuePanel_ExcludesOwnPRs verifies that PRs authored by the current
// user are excluded from the review queue display.
func TestReviewQueuePanel_ExcludesOwnPRs(t *testing.T) {
	tests := []struct {
		name       string
		username   string
		prs        []persistence.PullRequest
		wantCount  int
		wantTitles []string
		wantAbsent []string
	}{
		{
			name:     "excludes PR authored by current user",
			username: "alice",
			prs: []persistence.PullRequest{
				{ID: "pr1", Repo: "repo", Number: 1, Title: "alice PR",
					Author: "alice", Status: "open", URL: "u1",
					LastActivityAt: t0, CreatedAt: t0},
				{ID: "pr2", Repo: "repo", Number: 2, Title: "bob PR",
					Author: "bob", Status: "open", URL: "u2",
					LastActivityAt: t0, CreatedAt: t0},
			},
			wantCount:  1,
			wantTitles: []string{"bob PR"},
			wantAbsent: []string{"alice PR"},
		},
		{
			name:     "includes all PRs when none are by current user",
			username: "alice",
			prs: []persistence.PullRequest{
				{ID: "pr1", Repo: "repo", Number: 1, Title: "bob PR",
					Author: "bob", Status: "open", URL: "u1",
					LastActivityAt: t0, CreatedAt: t0},
				{ID: "pr2", Repo: "repo", Number: 2, Title: "charlie PR",
					Author: "charlie", Status: "open", URL: "u2",
					LastActivityAt: t0, CreatedAt: t0},
			},
			wantCount:  2,
			wantTitles: []string{"bob PR", "charlie PR"},
		},
		{
			name:     "excludes all PRs when all are by current user",
			username: "alice",
			prs: []persistence.PullRequest{
				{ID: "pr1", Repo: "repo", Number: 1, Title: "alice PR 1",
					Author: "alice", Status: "open", URL: "u1",
					LastActivityAt: t0, CreatedAt: t0},
				{ID: "pr2", Repo: "repo", Number: 2, Title: "alice PR 2",
					Author: "alice", Status: "open", URL: "u2",
					LastActivityAt: t0, CreatedAt: t0},
			},
			wantCount:  0,
			wantAbsent: []string{"alice PR 1", "alice PR 2"},
		},
		{
			name:     "empty username skips filtering",
			username: "",
			prs: []persistence.PullRequest{
				{ID: "pr1", Repo: "repo", Number: 1, Title: "any PR",
					Author: "anyone", Status: "open", URL: "u1",
					LastActivityAt: t0, CreatedAt: t0},
			},
			wantCount:  1,
			wantTitles: []string{"any PR"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := newStubPRReader()
			reader.prs = tt.prs
			for _, pr := range tt.prs {
				reader.sessionIDs[pr.URL] = "s"
			}
			panel := makeReviewPanel(reader, tt.username)
			if len(panel.rows) != tt.wantCount {
				t.Errorf("row count = %d, want %d", len(panel.rows), tt.wantCount)
			}
			view := panel.View()
			for _, title := range tt.wantTitles {
				if !strings.Contains(view, title) {
					t.Errorf("expected %q in view; got:\n%s", title, view)
				}
			}
			for _, title := range tt.wantAbsent {
				if strings.Contains(view, title) {
					t.Errorf("did not expect %q in view; got:\n%s", title, view)
				}
			}
		})
	}
}

// TestReviewQueuePanel_SelectedPR verifies that SelectedPR returns the PR under
// the cursor or nil when the panel is empty.
func TestReviewQueuePanel_SelectedPR(t *testing.T) {
	t.Run("empty panel returns nil", func(t *testing.T) {
		panel := makeReviewPanel(newStubPRReader(), "me")
		if got := panel.SelectedPR(); got != nil {
			t.Errorf("SelectedPR() = %v, want nil", got)
		}
	})

	t.Run("returns PR at cursor 0", func(t *testing.T) {
		reader := newStubPRReader()
		reader.prs = []persistence.PullRequest{
			{ID: "p1", Repo: "r", Number: 1, Title: "first", URL: "u1",
				Author: "alice", LastActivityAt: t0, CreatedAt: t0},
		}
		panel := makeReviewPanel(reader, "me")
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
			{ID: "p1", Repo: "r", Number: 1, Title: "first", URL: "u1",
				Author: "alice", LastActivityAt: t0, CreatedAt: t0},
			{ID: "p2", Repo: "r", Number: 2, Title: "second", URL: "u2",
				Author: "bob", LastActivityAt: t0.Add(time.Second), CreatedAt: t0},
		}
		panel := makeReviewPanel(reader, "me")
		panel.Update(MoveFocusMsg{Down: true})
		got := panel.SelectedPR()
		if got == nil {
			t.Fatal("SelectedPR() = nil, want non-nil")
		}
		// ReviewQueue sorts by urgency; just verify a valid PR is returned.
		if got.ID == "" {
			t.Errorf("SelectedPR().ID is empty")
		}
	})
}

// ── CursorPosition / SetCursor tests ──────────────────────────────────────────

func TestReviewQueuePanel_CursorPosition(t *testing.T) {
	reader := newStubPRReader()
	reader.prs = []persistence.PullRequest{
		{ID: "pr1", URL: "u1", Author: "alice", LastActivityAt: t0, CreatedAt: t0},
		{ID: "pr2", URL: "u2", Author: "bob", LastActivityAt: t1, CreatedAt: t0},
	}
	reader.sessionIDs["u1"] = "a"
	reader.sessionIDs["u2"] = "b"
	panel := makeReviewPanel(reader, "me")

	if panel.CursorPosition() != 0 {
		t.Errorf("initial CursorPosition = %d, want 0", panel.CursorPosition())
	}

	panel.Update(MoveFocusMsg{Down: true})
	if panel.CursorPosition() != 1 {
		t.Errorf("after j CursorPosition = %d, want 1", panel.CursorPosition())
	}
}

func TestReviewQueuePanel_SetCursor(t *testing.T) {
	reader := newStubPRReader()
	reader.prs = []persistence.PullRequest{
		{ID: "pr1", URL: "u1", Author: "alice", LastActivityAt: t0, CreatedAt: t0},
		{ID: "pr2", URL: "u2", Author: "bob", LastActivityAt: t1, CreatedAt: t0},
		{ID: "pr3", URL: "u3", Author: "charlie", LastActivityAt: t2, CreatedAt: t0},
	}
	reader.sessionIDs["u1"] = "a"
	reader.sessionIDs["u2"] = "b"
	reader.sessionIDs["u3"] = "c"
	panel := makeReviewPanel(reader, "me")

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

func TestReviewQueuePanel_SetCursor_EmptyPanel(t *testing.T) {
	reader := newStubPRReader()
	panel := makeReviewPanel(reader, "me")

	panel.SetCursor(5)
	if panel.CursorPosition() != 0 {
		t.Errorf("SetCursor on empty panel: CursorPosition = %d, want 0", panel.CursorPosition())
	}
}
