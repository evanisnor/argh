package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/x/ansi"

	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
)

// Clock abstracts time.Now() for testability.
type Clock interface {
	Now() time.Time
}

// realClock is the production Clock implementation.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// PRReader is the data access interface required by the My PRs panel.
type PRReader interface {
	ListPullRequests() ([]persistence.PullRequest, error)
	GetSessionID(prURL string) (string, error)
	ListWatches() ([]persistence.Watch, error)
	ListReviewers(prID string) ([]persistence.Reviewer, error)
}

// prRow holds the display data for a single row in the My PRs panel.
type prRow struct {
	sessionID     string
	pr            persistence.PullRequest
	hasWatches    bool
	approvedCount int
	changesCount  int
	commentCount  int
}

// MyPRsPanel renders the My Pull Requests panel.
type MyPRsPanel struct {
	reader   PRReader
	clock    Clock
	rows     []prRow
	cursor   int
	flashing map[string]bool // PR ID → flash active
	width    int             // allocated terminal width, 0 = no constraint
}

// NewMyPRsPanel creates a new My PRs panel backed by the given reader.
func NewMyPRsPanel(reader PRReader) *MyPRsPanel {
	return newMyPRsPanelWithClock(reader, realClock{})
}

// newMyPRsPanelWithClock creates a new My PRs panel with an injected clock.
func newMyPRsPanelWithClock(reader PRReader, clock Clock) *MyPRsPanel {
	p := &MyPRsPanel{
		reader:   reader,
		clock:    clock,
		flashing: make(map[string]bool),
	}
	_ = p.refresh()
	return p
}

// Update handles incoming Bubble Tea messages.
func (p *MyPRsPanel) Update(msg tea.Msg) (SubModel, tea.Cmd) {
	switch m := msg.(type) {
	case ResizeMsg:
		p.width = m.Width
	case DBEventMsg:
		switch m.Event.Type {
		case eventbus.PRUpdated, eventbus.CIChanged, eventbus.ReviewChanged, eventbus.PRRemoved:
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

// RowCount returns the number of PR rows in the panel.
func (p *MyPRsPanel) RowCount() int { return len(p.rows) }

// prHeaders are the column header labels for the My PRs table.
var prHeaders = []string{"", "", "REPO", "#", "TITLE", "●", "⚙", "✓✗", "💬", "⏱"}

// prColWidths defines fixed column widths; index 4 (title) is 0 = flex.
var prColWidths = []int{1, 2, 14, 5, 0, 17, 2, 5, 2, 3}

// prBaseStyle returns the base layout style (width + alignment) for a column.
func prBaseStyle(col int) lipgloss.Style {
	s := lipgloss.NewStyle()
	if col < len(prColWidths) && prColWidths[col] > 0 {
		s = s.Width(prColWidths[col])
	}
	if col <= 1 {
		s = s.AlignHorizontal(lipgloss.Right)
	}
	return s
}

// View renders the panel content (title/border is added by the root model).
func (p *MyPRsPanel) View() string {
	if len(p.rows) == 0 {
		return "  (no open pull requests)"
	}

	now := p.clock.Now()
	rows := make([][]string, len(p.rows))
	for i, row := range p.rows {
		rows[i] = p.buildPRCells(row, now)
	}

	sf := func(row, col int) lipgloss.Style {
		base := prBaseStyle(col)
		if row < 0 {
			return base.Faint(true)
		}
		r := p.rows[row]
		if r.pr.Draft {
			base = base.Faint(true)
		}
		if r.pr.Status == "approved" || r.pr.CIState == "passing" || r.pr.CIState == "success" {
			base = base.Foreground(lipgloss.Color("#4CAF50"))
		}
		if r.pr.CIState == "running" || r.pr.CIState == "in_progress" || r.pr.CIState == "pending" {
			base = base.Foreground(lipgloss.Color("#FFC107"))
		}
		if r.changesCount > 0 {
			base = base.Foreground(lipgloss.Color("#FFA07A"))
		}
		if r.pr.CIState == "failing" || r.pr.CIState == "failure" {
			base = base.Foreground(lipgloss.Color("#FF6B6B"))
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
		Headers(prHeaders...).
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

// buildPRCells builds the cell values for a single PR row.
func (p *MyPRsPanel) buildPRCells(row prRow, now time.Time) []string {
	sid := row.sessionID
	if sid == "" {
		sid = "-"
	}
	watchIcon := " "
	if row.hasWatches {
		watchIcon = "👁"
	}
	title := row.pr.Title
	if row.pr.Draft {
		title = "[draft] " + title
	}
	return []string{
		sid,
		watchIcon,
		row.pr.Repo,
		fmt.Sprintf("#%d", row.pr.Number),
		title,
		prStatusDisplay(row.pr.Status, row.pr.Draft),
		prCIDisplay(row.pr.CIState),
		prReviewDisplay(row.approvedCount, row.changesCount),
		fmt.Sprintf("%d", row.commentCount),
		formatAge(now.Sub(row.pr.LastActivityAt)),
	}
}

// SelectedPR returns the PullRequest currently under the cursor, or nil when
// the panel has no rows.
func (p *MyPRsPanel) SelectedPR() *persistence.PullRequest {
	if len(p.rows) == 0 {
		return nil
	}
	pr := p.rows[p.cursor].pr
	return &pr
}

// HasContent reports whether there are any PRs to display.
func (p *MyPRsPanel) HasContent() bool {
	return len(p.rows) > 0
}

// refresh loads PR data from the DB and rebuilds the row list.
func (p *MyPRsPanel) refresh() error {
	prs, err := p.reader.ListPullRequests()
	if err != nil {
		return err
	}
	// Sort by last_activity_at ascending (oldest/stalest first).
	sort.Slice(prs, func(i, j int) bool {
		return prs[i].LastActivityAt.Before(prs[j].LastActivityAt)
	})

	watches, err := p.reader.ListWatches()
	if err != nil {
		return err
	}
	// Build a set of PR URLs that have active (waiting/scheduled) watches.
	watchedURLs := make(map[string]bool)
	for _, w := range watches {
		if w.Status == "waiting" || w.Status == "scheduled" {
			watchedURLs[w.PRURL] = true
		}
	}

	rows := make([]prRow, 0, len(prs))
	for _, pr := range prs {
		sid, _ := p.reader.GetSessionID(pr.URL)
		reviewers, _ := p.reader.ListReviewers(pr.ID)
		approved, changes, comments := 0, 0, 0
		for _, r := range reviewers {
			switch r.State {
			case "APPROVED":
				approved++
			case "CHANGES_REQUESTED":
				changes++
			case "COMMENTED":
				comments++
			}
		}
		rows = append(rows, prRow{
			sessionID:     sid,
			pr:            pr,
			hasWatches:    watchedURLs[pr.URL],
			approvedCount: approved,
			changesCount:  changes,
			commentCount:  comments,
		})
	}
	p.rows = rows
	if p.cursor >= len(p.rows) && len(p.rows) > 0 {
		p.cursor = len(p.rows) - 1
	}
	return nil
}


// truncateTitle truncates s with a trailing "…" if width > 0 and
// fixedLen + displayWidth(s) would exceed width. Uses display-width
// measurement so emoji and wide characters are handled correctly.
// Returns s unchanged when width is 0.
func truncateTitle(s string, width, fixedLen int) string {
	if width <= 0 {
		return s
	}
	maxTitle := width - fixedLen
	if maxTitle <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= maxTitle {
		return s
	}
	if maxTitle <= 1 {
		return "…"
	}
	return ansi.Truncate(s, maxTitle, "…")
}

// prStatusDisplay converts a status string to its display form.
func prStatusDisplay(status string, draft bool) string {
	if draft {
		return "draft"
	}
	switch status {
	case "open":
		return "open"
	case "approved":
		return "approved"
	case "changes_requested", "changes requested":
		return "changes requested"
	case "merge_queued", "merge queued":
		return "merge queued"
	default:
		return status
	}
}

// prCIDisplay converts a CI state string to its display symbol.
func prCIDisplay(state string) string {
	switch state {
	case "passing", "success":
		return "✓"
	case "failing", "failure":
		return "✗"
	case "running", "in_progress", "pending":
		return "⟳"
	default:
		return "—"
	}
}

// prReviewDisplay formats the Reviews column.
func prReviewDisplay(approved, changes int) string {
	if approved == 0 && changes == 0 {
		return "—"
	}
	var parts []string
	if approved > 0 {
		parts = append(parts, fmt.Sprintf("✓%d", approved))
	}
	if changes > 0 {
		parts = append(parts, fmt.Sprintf("✗%d", changes))
	}
	return strings.Join(parts, " ")
}

// formatAge formats a duration as a human-readable age string.
func formatAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
