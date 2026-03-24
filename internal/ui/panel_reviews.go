package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
)

// rqHeaders are the column header labels for the Review Queue table.
var rqHeaders = []string{"", "", "REPO", "#", "TITLE", "@", "⚙", "⏱", "!!"}

// rqColWidths defines fixed column widths; index 4 (title) is 0 = flex.
var rqColWidths = []int{1, 2, 14, 5, 0, 12, 2, 3, 3}

// rqBaseStyle returns the base layout style (width + alignment) for a column.
func rqBaseStyle(col int) lipgloss.Style {
	s := lipgloss.NewStyle()
	if col < len(rqColWidths) && rqColWidths[col] > 0 {
		s = s.Width(rqColWidths[col])
	}
	if col <= 1 {
		s = s.AlignHorizontal(lipgloss.Right)
	}
	return s
}

// ReviewReader is the data access interface required by the Review Queue panel.
type ReviewReader interface {
	ListPullRequests() ([]persistence.PullRequest, error)
	GetSessionID(prURL string) (string, error)
	ListWatches() ([]persistence.Watch, error)
	ListReviewers(prID string) ([]persistence.Reviewer, error)
}

// reviewRow holds the display data for a single row in the Review Queue panel.
type reviewRow struct {
	sessionID      string
	pr             persistence.PullRequest
	hasWatches     bool
	urgency        int
	isLastReviewer bool
	readyToReview  bool
}

// ReviewQueuePanel renders the Review Queue panel.
type ReviewQueuePanel struct {
	reader   ReviewReader
	clock    Clock
	username string
	rows     []reviewRow
	cursor   int
	flashing map[string]bool
	width    int // allocated terminal width, 0 = no constraint
}

// NewReviewQueuePanel creates a new Review Queue panel backed by the given reader.
func NewReviewQueuePanel(reader ReviewReader, username string) *ReviewQueuePanel {
	return newReviewQueuePanelWithClock(reader, username, realClock{})
}

// newReviewQueuePanelWithClock creates a new Review Queue panel with an injected clock.
func newReviewQueuePanelWithClock(reader ReviewReader, username string, clock Clock) *ReviewQueuePanel {
	p := &ReviewQueuePanel{
		reader:   reader,
		clock:    clock,
		username: username,
		flashing: make(map[string]bool),
	}
	_ = p.refresh()
	return p
}

// Update handles incoming Bubble Tea messages.
func (p *ReviewQueuePanel) Update(msg tea.Msg) (SubModel, tea.Cmd) {
	switch m := msg.(type) {
	case ResizeMsg:
		p.width = m.Width
	case DBEventMsg:
		switch m.Event.Type {
		case eventbus.PRUpdated, eventbus.CIChanged, eventbus.ReviewChanged, eventbus.PRRemoved, eventbus.SessionIDsAssigned:
			if pr, ok := m.Event.After.(persistence.PullRequest); ok {
				p.flashing[pr.ID] = true
			}
			_ = p.refresh()
		}
	case MoveFocusMsg:
		if m.Down {
			if p.cursor < len(p.rows)-1 {
				p.cursor++
			}
		} else {
			if p.cursor > 0 {
				p.cursor--
			}
		}
	}
	return p, nil
}

// RowCount returns the number of review queue rows in the panel.
func (p *ReviewQueuePanel) RowCount() int { return len(p.rows) }

// View renders the panel content.
func (p *ReviewQueuePanel) View() string {
	if len(p.rows) == 0 {
		return "  (no reviews requested)"
	}

	now := p.clock.Now()
	rows := make([][]string, len(p.rows))
	for i, row := range p.rows {
		rows[i] = p.buildRQCells(row, now)
	}

	sf := func(row, col int) lipgloss.Style {
		base := rqBaseStyle(col)
		if row < 0 {
			return base.Faint(true)
		}
		r := p.rows[row]
		if r.isLastReviewer {
			base = base.Foreground(lipgloss.Color("#FFD700"))
		}
		if r.readyToReview {
			base = base.Foreground(lipgloss.Color("#90EE90"))
		}
		if p.flashing[r.pr.ID] {
			base = base.Bold(true)
		}
		if row == p.cursor {
			base = base.Reverse(true)
		}
		return base
	}

	t := table.New().
		Headers(rqHeaders...).
		Rows(rows...).
		Border(lipgloss.NormalBorder()).
		BorderColumn(true).BorderHeader(true).
		BorderTop(false).BorderBottom(false).
		BorderLeft(false).BorderRight(false).
		Wrap(false).
		StyleFunc(sf)
	if p.width > 0 {
		t = t.Width(p.width)
	}
	return strings.TrimRight(t.Render(), "\n")
}

// buildRQCells builds the cell values for a single review queue row.
func (p *ReviewQueuePanel) buildRQCells(row reviewRow, now time.Time) []string {
	sid := row.sessionID
	if sid == "" {
		sid = "-"
	}
	watchIcon := " "
	if row.hasWatches {
		watchIcon = "👁"
	}
	return []string{
		sid,
		watchIcon,
		row.pr.Repo,
		fmt.Sprintf("#%d", row.pr.Number),
		row.pr.Title,
		"@" + row.pr.Author,
		prCIDisplay(row.pr.CIState),
		formatAge(now.Sub(row.pr.LastActivityAt)),
		urgencyDisplay(row.urgency),
	}
}

// CursorPosition returns the current cursor index within the panel.
func (p *ReviewQueuePanel) CursorPosition() int { return p.cursor }

// SetCursor moves the cursor to the given position, clamped to valid bounds.
func (p *ReviewQueuePanel) SetCursor(pos int) {
	if len(p.rows) == 0 {
		p.cursor = 0
		return
	}
	if pos < 0 {
		pos = 0
	}
	if pos >= len(p.rows) {
		pos = len(p.rows) - 1
	}
	p.cursor = pos
}

// SelectedPR returns the PullRequest currently under the cursor, or nil when
// the panel has no rows.
func (p *ReviewQueuePanel) SelectedPR() *persistence.PullRequest {
	if len(p.rows) == 0 {
		return nil
	}
	pr := p.rows[p.cursor].pr
	return &pr
}

// HasContent reports whether there are any PRs to display.
func (p *ReviewQueuePanel) HasContent() bool {
	return len(p.rows) > 0
}

// refresh loads PR data from the DB and rebuilds the row list.
func (p *ReviewQueuePanel) refresh() error {
	prs, err := p.reader.ListPullRequests()
	if err != nil {
		return err
	}

	watches, err := p.reader.ListWatches()
	if err != nil {
		return err
	}
	watchedURLs := make(map[string]bool)
	for _, w := range watches {
		if w.Status == "waiting" || w.Status == "scheduled" {
			watchedURLs[w.PRURL] = true
		}
	}

	now := p.clock.Now()
	rows := make([]reviewRow, 0, len(prs))
	for _, pr := range prs {
		if p.username != "" && pr.Author == p.username {
			continue
		}
		sid, _ := p.reader.GetSessionID(pr.URL)
		reviewers, _ := p.reader.ListReviewers(pr.ID)

		staleness := now.Sub(pr.LastActivityAt)
		authorWait := now.Sub(pr.CreatedAt)
		urgency := calculateUrgency(staleness, pr.CIState, authorWait)

		rows = append(rows, reviewRow{
			sessionID:      sid,
			pr:             pr,
			hasWatches:     watchedURLs[pr.URL],
			urgency:        urgency,
			isLastReviewer: isLastRequiredReviewer(reviewers, p.username),
			readyToReview:  isReadyToReview(pr.CIState, reviewers),
		})
	}

	// Sort by urgency descending.
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].urgency > rows[j].urgency
	})

	p.rows = rows
	if p.cursor >= len(p.rows) && len(p.rows) > 0 {
		p.cursor = len(p.rows) - 1
	}
	return nil
}

// calculateUrgency computes the urgency score (clamped 1–3).
func calculateUrgency(staleness time.Duration, ciState string, authorWait time.Duration) int {
	// Base score from staleness.
	base := 1
	if staleness >= 24*time.Hour {
		base = 3
	} else if staleness >= 4*time.Hour {
		base = 2
	}

	// CI modifier.
	switch ciState {
	case "failing", "failure":
		base++
	case "passing", "success":
		base--
	}

	// Author wait modifier.
	if authorWait > 72*time.Hour {
		base++
	}

	// Clamp to 1–3.
	if base < 1 {
		base = 1
	}
	if base > 3 {
		base = 3
	}
	return base
}

// isLastRequiredReviewer returns true if username is the only PENDING reviewer.
func isLastRequiredReviewer(reviewers []persistence.Reviewer, username string) bool {
	if username == "" {
		return false
	}
	pendingCount := 0
	userIsPending := false
	for _, r := range reviewers {
		if r.State == "PENDING" {
			pendingCount++
			if r.Login == username {
				userIsPending = true
			}
		}
	}
	return userIsPending && pendingCount == 1
}

// isReadyToReview returns true when CI is passing and there are no blockers.
func isReadyToReview(ciState string, reviewers []persistence.Reviewer) bool {
	if ciState != "passing" && ciState != "success" {
		return false
	}
	for _, r := range reviewers {
		if r.State == "CHANGES_REQUESTED" {
			return false
		}
	}
	return true
}

// urgencyDisplay returns the urgency dot display string.
func urgencyDisplay(score int) string {
	switch score {
	case 3:
		return "●●●"
	case 2:
		return "●●○"
	default:
		return "●○○"
	}
}

