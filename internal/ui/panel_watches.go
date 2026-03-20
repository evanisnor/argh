package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
)

// WatchReader is the data access interface required by the Watches panel.
type WatchReader interface {
	ListWatches() ([]persistence.Watch, error)
}

// watchRow holds the display data for a single row in the Watches panel.
type watchRow struct {
	watch persistence.Watch
}

// WatchesPanel renders the Watches panel.
type WatchesPanel struct {
	reader   WatchReader
	rows     []watchRow
	cursor   int
	flashing map[string]bool // Watch ID → flash active
	width    int             // allocated terminal width, 0 = no constraint
}

// NewWatchesPanel creates a new Watches panel backed by the given reader.
func NewWatchesPanel(reader WatchReader) *WatchesPanel {
	p := &WatchesPanel{
		reader:   reader,
		flashing: make(map[string]bool),
	}
	_ = p.refresh()
	return p
}

// Update handles incoming Bubble Tea messages.
func (p *WatchesPanel) Update(msg tea.Msg) (SubModel, tea.Cmd) {
	switch m := msg.(type) {
	case ResizeMsg:
		p.width = m.Width
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

// View renders the panel content (title/border is added by the root model).
func (p *WatchesPanel) View() string {
	active := 0
	for _, row := range p.rows {
		if row.watch.Status == "waiting" || row.watch.Status == "scheduled" {
			active++
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%d active]\n", active))
	if len(p.rows) == 0 {
		sb.WriteString("  (no active watches)")
		return sb.String()
	}
	for i, row := range p.rows {
		sb.WriteString(p.renderRow(row, i == p.cursor))
		if i < len(p.rows)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// HasContent reports whether there are any watches to display.
// The root model uses this to decide whether to show or hide the Watches panel.
func (p *WatchesPanel) HasContent() bool {
	return len(p.rows) > 0
}

// refresh loads watch data from the DB and rebuilds the row list.
func (p *WatchesPanel) refresh() error {
	watches, err := p.reader.ListWatches()
	if err != nil {
		return err
	}
	rows := make([]watchRow, 0, len(watches))
	for _, w := range watches {
		rows = append(rows, watchRow{watch: w})
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

// renderRow formats a single watch row as a string with appropriate styles.
// The trigger expression is truncated with a trailing "…" when p.width is set
// and the full row text would exceed the allocated width.
func (p *WatchesPanel) renderRow(row watchRow, focused bool) string {
	id := row.watch.ID
	if len(id) > 8 {
		id = id[:8]
	}

	prefix := fmt.Sprintf("%s  %s  #%d  ", id, row.watch.Repo, row.watch.PRNumber)
	suffix := fmt.Sprintf("  %s  %s", row.watch.ActionExpr, watchStatusDisplay(row.watch.Status))
	trigger := truncateTitle(row.watch.TriggerExpr, p.width, len(prefix)+len(suffix))

	text := prefix + trigger + suffix

	style := lipgloss.NewStyle()
	if row.watch.Status == "failed" {
		style = style.Foreground(lipgloss.Color("#FF6B6B"))
	}
	if row.watch.Status == "fired" {
		style = style.Faint(true)
	}
	if p.flashing[row.watch.ID] {
		style = style.Bold(true)
	}
	if focused {
		style = style.Reverse(true)
	}
	return style.Render(text)
}
