package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
		case eventbus.PRUpdated, eventbus.CIChanged, eventbus.ReviewChanged:
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

// View renders the panel content (title/border is added by the root model).
func (p *MyPRsPanel) View() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%d open]\n", len(p.rows)))
	if len(p.rows) == 0 {
		sb.WriteString("  (no open pull requests)")
		return sb.String()
	}
	now := p.clock.Now()
	for i, row := range p.rows {
		sb.WriteString(p.renderRow(row, now, i == p.cursor))
		if i < len(p.rows)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
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

// renderRow formats a single PR row as a string with appropriate styles.
// The title is truncated with a trailing "…" when p.width is set and the full
// row text would exceed the allocated width.
func (p *MyPRsPanel) renderRow(row prRow, now time.Time, focused bool) string {
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

	// Build the row without the title first so we know the fixed-width parts.
	prefix := fmt.Sprintf("%s %s %s #%d ", sid, watchIcon, row.pr.Repo, row.pr.Number)
	suffix := fmt.Sprintf("  %s  %s  %s  %d  %s",
		prStatusDisplay(row.pr.Status, row.pr.Draft),
		prCIDisplay(row.pr.CIState),
		prReviewDisplay(row.approvedCount, row.changesCount),
		row.commentCount,
		formatAge(now.Sub(row.pr.LastActivityAt)),
	)
	title = truncateTitle(title, p.width, len(prefix)+len(suffix))

	text := prefix + title + suffix

	style := lipgloss.NewStyle()
	if row.pr.Draft {
		style = style.Faint(true)
	}
	// Color based on state; higher-priority states override lower ones.
	if row.pr.Status == "approved" || row.pr.CIState == "passing" || row.pr.CIState == "success" {
		style = style.Foreground(lipgloss.Color("#4CAF50")) // green: approved/passing
	}
	if row.pr.CIState == "running" || row.pr.CIState == "in_progress" || row.pr.CIState == "pending" {
		style = style.Foreground(lipgloss.Color("#FFC107")) // yellow: pending/waiting
	}
	if row.changesCount > 0 {
		style = style.Foreground(lipgloss.Color("#FFA07A")) // orange: changes requested
	}
	if row.pr.CIState == "failing" || row.pr.CIState == "failure" {
		style = style.Foreground(lipgloss.Color("#FF6B6B")) // red: CI failing (highest priority)
	}
	if p.flashing[row.pr.ID] {
		style = style.Bold(true)
	}
	if focused {
		style = style.Reverse(true)
	}
	return style.Render(text)
}

// truncateTitle truncates s with a trailing "…" if width > 0 and
// len(prefix)+len(s) would exceed width. Returns s unchanged when width is 0.
func truncateTitle(s string, width, fixedLen int) string {
	if width <= 0 {
		return s
	}
	maxTitle := width - fixedLen
	if maxTitle <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxTitle {
		return s
	}
	if maxTitle <= 1 {
		return "…"
	}
	return string(runes[:maxTitle-1]) + "…"
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
