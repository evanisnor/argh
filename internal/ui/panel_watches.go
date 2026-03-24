package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
)

// WatchReader is the data access interface required by the Watches panel.
type WatchReader interface {
	ListWatches() ([]persistence.Watch, error)
	GetSessionID(prURL string) (string, error)
	GetPullRequest(repo string, number int) (persistence.PullRequest, error)
}

// WatchCanceller cancels a watch by ID.
type WatchCanceller interface {
	CancelWatch(id string) error
}

// CancelWatchMsg is dispatched to the watches panel when the user presses d.
type CancelWatchMsg struct{}

// watchRow holds the display data for a single row in the Watches panel.
type watchRow struct {
	watch     persistence.Watch
	sessionID string
	pr        *persistence.PullRequest
}

// WatchesPanel renders the Watches panel.
type WatchesPanel struct {
	reader    WatchReader
	canceller WatchCanceller
	rows      []watchRow
	cursor    int
	focused   bool
	flashing  map[string]bool // Watch ID → flash active
	width     int             // allocated terminal width, 0 = no constraint
}

// SetFocused sets whether this panel currently holds keyboard focus.
func (p *WatchesPanel) SetFocused(focused bool) { p.focused = focused }

// NewWatchesPanel creates a new Watches panel backed by the given reader.
func NewWatchesPanel(reader WatchReader, canceller WatchCanceller) *WatchesPanel {
	p := &WatchesPanel{
		reader:    reader,
		canceller: canceller,
		flashing:  make(map[string]bool),
	}
	_ = p.refresh()
	return p
}

// Update handles incoming Bubble Tea messages.
func (p *WatchesPanel) Update(msg tea.Msg) (SubModel, tea.Cmd) {
	switch m := msg.(type) {
	case ResizeMsg:
		p.width = m.Width
	case CancelWatchMsg:
		if len(p.rows) == 0 || p.canceller == nil {
			return p, nil
		}
		id := p.rows[p.cursor].watch.ID
		return p, func() tea.Msg {
			if err := p.canceller.CancelWatch(id); err != nil {
				return CommandResultMsg{Err: fmt.Errorf("cancel watch: %w", err)}
			}
			return WatchChangedMsg{Status: fmt.Sprintf("watch %s cancelled", id)}
		}
	case RefreshMsg:
		_ = p.refresh()
	case DBEventMsg:
		if m.Event.Type == eventbus.WatchFired {
			if w, ok := m.Event.After.(persistence.Watch); ok {
				p.flashing[w.ID] = true
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

// RowCount returns the number of watch rows in the panel.
func (p *WatchesPanel) RowCount() int { return len(p.rows) }

// CursorPosition returns the current cursor index within the panel.
func (p *WatchesPanel) CursorPosition() int { return p.cursor }

// SetCursor moves the cursor to the given position, clamped to valid bounds.
func (p *WatchesPanel) SetCursor(pos int) {
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

// watchHeaders are the column header labels for the Watches table.
var watchHeaders = []string{"ID", "", "REPO", "#", "TITLE", "TRIGGER", "ACTION", "⚙"}

// watchColWidths defines fixed column widths; index 4 (title) is 0 = flex.
var watchColWidths = []int{8, 1, 14, 5, 0, 12, 8, 2}

// watchBaseStyle returns the base layout style (width + alignment) for a column.
func watchBaseStyle(widths []int, col int) lipgloss.Style {
	s := lipgloss.NewStyle()
	if col < len(widths) && widths[col] > 0 {
		s = s.Width(widths[col])
	}
	if col == 1 {
		s = s.AlignHorizontal(lipgloss.Right)
	}
	return s
}

// View renders the panel content (title/border is added by the root model).
func (p *WatchesPanel) View() string {
	if len(p.rows) == 0 {
		return "  (no active watches)"
	}

	rows := make([][]string, len(p.rows))
	for i, row := range p.rows {
		rows[i] = p.buildWatchCells(row)
	}

	widths := fitColWidths(watchColWidths, watchHeaders, rows, 2, 3, 5, 6)

	sf := func(row, col int) lipgloss.Style {
		base := watchBaseStyle(widths, col)
		if row < 0 {
			return base.Faint(true)
		}
		r := p.rows[row]
		if r.pr != nil {
			ci := r.pr.CIState
			if ci == "passing" || ci == "success" {
				base = base.Foreground(lipgloss.Color("#4CAF50"))
			}
			if ci == "running" || ci == "in_progress" || ci == "pending" {
				base = base.Foreground(lipgloss.Color("#FFC107"))
			}
			if ci == "failing" || ci == "failure" {
				base = base.Foreground(lipgloss.Color("#FF6B6B"))
			}
		}
		if p.flashing[r.watch.ID] {
			base = base.Bold(true)
		}
		if row == p.cursor && p.focused {
			base = base.Reverse(true)
		}
		return base
	}

	t := table.New().
		Headers(watchHeaders...).
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

// buildWatchCells builds the cell values for a single watch row.
func (p *WatchesPanel) buildWatchCells(row watchRow) []string {
	id := row.watch.ID
	if len(id) > 8 {
		id = id[:8]
	}
	sid := row.sessionID
	if sid == "" {
		sid = "-"
	}
	title := ""
	ciDisplay := ""
	if row.pr != nil {
		title = row.pr.Title
		ciDisplay = prCIDisplay(row.pr.CIState)
	}
	return []string{
		id,
		sid,
		row.watch.Repo,
		fmt.Sprintf("#%d", row.watch.PRNumber),
		title,
		row.watch.TriggerExpr,
		row.watch.ActionExpr,
		ciDisplay,
	}
}

// HasContent reports whether there are any watches to display.
// The root model uses this to decide whether to show or hide the Watches panel.
func (p *WatchesPanel) HasContent() bool {
	return len(p.rows) > 0
}

// refresh loads watch data from the DB and rebuilds the row list.
// Only active watches (status "waiting" or "scheduled") are included.
func (p *WatchesPanel) refresh() error {
	watches, err := p.reader.ListWatches()
	if err != nil {
		return err
	}
	rows := make([]watchRow, 0, len(watches))
	for _, w := range watches {
		if w.Status != "waiting" && w.Status != "scheduled" {
			continue
		}
		sid, _ := p.reader.GetSessionID(w.PRURL)
		var pr *persistence.PullRequest
		if found, err := p.reader.GetPullRequest(w.Repo, w.PRNumber); err == nil {
			pr = &found
		}
		rows = append(rows, watchRow{watch: w, sessionID: sid, pr: pr})
	}
	p.rows = rows
	if p.cursor >= len(p.rows) && len(p.rows) > 0 {
		p.cursor = len(p.rows) - 1
	}
	return nil
}

// watchStatusDisplay converts a watch status string to its display symbol.
func watchStatusDisplay(status string) string {
	switch status {
	case "waiting", "scheduled":
		return "⟳"
	case "fired":
		return "✓"
	case "failed":
		return "✗"
	default:
		return status
	}
}
