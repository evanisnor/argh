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

// RowCount returns the number of review queue rows in the panel.
func (p *ReviewQueuePanel) RowCount() int { return len(p.rows) }

// Column widths for the Review Queue table layout.
const (
	rqColSID     = 1
	rqColSpace   = 1
	rqColWatch   = 2
	rqColRepo    = 14
	rqColNumber  = 5
	rqColAuthor  = 12
	rqColCI      = 2
	rqColAge     = 3
	rqColUrgency = 3
	rqColSep     = 3 // " │ "
	rqNumSeps    = 7
	rqFixedWidth = rqColSID + rqColSpace + rqColWatch + rqColRepo + rqColNumber +
		rqColAuthor + rqColCI + rqColAge + rqColUrgency +
		rqNumSeps*rqColSep
)

// View renders the panel content.
func (p *ReviewQueuePanel) View() string {
	if len(p.rows) == 0 {
		return "  (no reviews requested)"
	}
	var sb strings.Builder
	sb.WriteString(p.renderHeader())
	sb.WriteString("\n")
	now := p.clock.Now()
	for i, row := range p.rows {
		sb.WriteString(p.renderRow(row, now, i == p.cursor))
		if i < len(p.rows)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// renderHeader builds the column header line for the Review Queue table.
func (p *ReviewQueuePanel) renderHeader() string {
	sep := " │ "
	titleWidth := p.titleWidth()
	header := fmt.Sprintf("%*s %*s %-*s%s%-*s%s%-*s%s%-*s%s%-*s%s%-*s%s%-*s",
		rqColSID, "",
		rqColWatch, "",
		rqColRepo, "REPO", sep,
		rqColNumber, "#", sep,
		titleWidth, "TITLE", sep,
		rqColAuthor, "@", sep,
		rqColCI, "⚙", sep,
		rqColAge, "⏱", sep,
		rqColUrgency, "!!",
	)
	return lipgloss.NewStyle().Faint(true).Render(header)
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

// titleWidth returns the flex title column width based on the panel's allocated width.
func (p *ReviewQueuePanel) titleWidth() int {
	if p.width <= 0 {
		return 40 // generous default before first resize
	}
	w := p.width - rqFixedWidth
	if w < 1 {
		w = 1
	}
	return w
}

// renderRow formats a single review queue row as a table row with fixed-width columns.
func (p *ReviewQueuePanel) renderRow(row reviewRow, now time.Time, focused bool) string {
	sid := row.sessionID
	if sid == "" {
		sid = "-"
	}

	watchIcon := " "
	if row.hasWatches {
		watchIcon = "👁"
	}

	sep := " │ "
	titleWidth := p.titleWidth()
	title := truncateTitle(row.pr.Title, titleWidth, 0)
	titleRunes := []rune(title)
	if len(titleRunes) < titleWidth {
		title = title + strings.Repeat(" ", titleWidth-len(titleRunes))
	}

	author := "@" + row.pr.Author

	text := fmt.Sprintf("%*s %*s %-*s%s%-*s%s%s%s%-*s%s%-*s%s%-*s%s%-*s",
		rqColSID, sid,
		rqColWatch, watchIcon,
		rqColRepo, truncateTitle(row.pr.Repo, rqColRepo, 0), sep,
		rqColNumber, fmt.Sprintf("#%d", row.pr.Number), sep,
		title, sep,
		rqColAuthor, truncateTitle(author, rqColAuthor, 0), sep,
		rqColCI, prCIDisplay(row.pr.CIState), sep,
		rqColAge, formatAge(now.Sub(row.pr.LastActivityAt)), sep,
		rqColUrgency, urgencyDisplay(row.urgency),
	)

	style := lipgloss.NewStyle()
	if row.isLastReviewer {
		style = style.Foreground(lipgloss.Color("#FFD700"))
	}
	if row.readyToReview {
		style = style.Foreground(lipgloss.Color("#90EE90"))
	}
	if p.flashing[row.pr.ID] {
		style = style.Bold(true)
	}
	if focused {
		style = style.Reverse(true)
	}
	return style.Render(text)
}
