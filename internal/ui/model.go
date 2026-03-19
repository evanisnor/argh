// Package ui implements the Bubble Tea root model and terminal layout for argh.
package ui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
)

// Panel identifies which panel currently holds keyboard focus.
type Panel int

const (
	PanelMyPRs Panel = iota
	PanelReviewQueue
	PanelWatches
)

// DBEventMsg wraps an eventbus.Event so it can be delivered as a Bubble Tea message.
type DBEventMsg struct {
	Event eventbus.Event
}

// MoveFocusMsg is sent to the focused panel when the user presses j/k or ↑/↓.
type MoveFocusMsg struct {
	Down bool // true = j/↓, false = k/↑
}

// FocusCommandBarMsg is sent to the command bar when the user presses / or :.
type FocusCommandBarMsg struct{}

// BlurCommandBarMsg is sent to the command bar when the user presses Esc.
type BlurCommandBarMsg struct{}

// OpenPRMsg is sent to the focused panel when the user presses o.
type OpenPRMsg struct{}

// ShowDiffMsg is sent to the focused panel when the user presses d.
type ShowDiffMsg struct{}

// ApprovePRMsg is sent to the Review Queue panel when the user presses a.
type ApprovePRMsg struct{}

// RequestReviewMsg is sent to the focused panel when the user presses r.
type RequestReviewMsg struct{}

// ForceReloadMsg is produced as a command when the user presses R.
type ForceReloadMsg struct{}

// ToggleDNDMsg is produced as a command when the user presses D.
type ToggleDNDMsg struct{}

// Subscriber is the subset of the event bus the root model requires.
type Subscriber interface {
	Subscribe(handler func(eventbus.Event)) func()
}

// DNDToggler toggles and reports Do Not Disturb state.
// The model calls Toggle() when the D key is pressed, and checks IsDND() when
// rendering the header to show the "🔕 DND" indicator.
type DNDToggler interface {
	Toggle()
	IsDND() bool
}

// SubModel is the interface that every panel and pane implements so the root
// model can delegate Update and View calls uniformly.
type SubModel interface {
	// Update handles a message and returns an updated copy of itself plus any Cmd.
	Update(msg tea.Msg) (SubModel, tea.Cmd)
	// View renders the sub-model to a string.
	View() string
	// HasContent reports whether the sub-model has rows to display.
	// Used by the Watches panel to decide whether to show or hide itself.
	HasContent() bool
}

// Theme holds the lipgloss styles derived from the terminal background.
type Theme struct {
	Dark            bool
	Header          lipgloss.Style
	PanelBorder     lipgloss.Style
	PanelTitle      lipgloss.Style
	StatusBar       lipgloss.Style
	CommandBar      lipgloss.Style
	FocusedBorder   lipgloss.Style
	UnfocusedBorder lipgloss.Style
}

// newTheme builds a Theme appropriate for the terminal background.
func newTheme(dark bool) Theme {
	if dark {
		return Theme{
			Dark:            true,
			Header:          lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFDF5")).Background(lipgloss.Color("#1A1A2E")),
			PanelBorder:     lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("#444466")),
			PanelTitle:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#C0C0FF")),
			StatusBar:       lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")),
			CommandBar:      lipgloss.NewStyle().Background(lipgloss.Color("#1A1A2E")).Foreground(lipgloss.Color("#FFFFFF")),
			FocusedBorder:   lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("#7C7CF8")),
			UnfocusedBorder: lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("#444466")),
		}
	}
	return Theme{
		Dark:            false,
		Header:          lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1A1A2E")).Background(lipgloss.Color("#E8E8F0")),
		PanelBorder:     lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("#AAAACC")),
		PanelTitle:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#3030AA")),
		StatusBar:       lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")),
		CommandBar:      lipgloss.NewStyle().Background(lipgloss.Color("#E8E8F0")).Foreground(lipgloss.Color("#000000")),
		FocusedBorder:   lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("#3030AA")),
		UnfocusedBorder: lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("#AAAACC")),
	}
}

// Model is the root Bubble Tea model. It holds references to all sub-models
// and owns the top-level Update dispatch and View composition.
type Model struct {
	version           string
	username          string
	focused           Panel
	myPRs             SubModel
	reviewQueue       SubModel
	watches           SubModel
	detailPane        SubModel
	commandBar        SubModel
	detailOpen        bool
	helpVisible       bool
	commandBarFocused bool
	statusText        string
	statusEventType   eventbus.EventType
	lastEventTime     time.Time
	clock             Clock
	eventCh           chan eventbus.Event
	unsubscribe       func()
	theme             Theme
	dndToggler        DNDToggler // optional; nil = no DND control
}

// New creates a root Model and subscribes to the event bus. Call Init() to
// start receiving events.
func New(version, username string, sub Subscriber,
	myPRs, reviewQueue, watches, detailPane, commandBar SubModel) Model {

	ch := make(chan eventbus.Event, 64)
	unsubscribe := sub.Subscribe(func(e eventbus.Event) {
		// Non-blocking send: drop events when the channel is full to avoid
		// blocking the publisher goroutine.
		select {
		case ch <- e:
		default:
		}
	})

	return Model{
		version:     version,
		username:    username,
		focused:     PanelMyPRs,
		myPRs:       myPRs,
		reviewQueue: reviewQueue,
		watches:     watches,
		detailPane:  detailPane,
		commandBar:  commandBar,
		detailOpen:  false,
		statusText:  "",
		clock:       realClock{},
		eventCh:     ch,
		unsubscribe: unsubscribe,
		theme:       newTheme(lipgloss.HasDarkBackground()),
	}
}

// NewWithTheme creates a root Model using an explicit Theme and Clock. Useful in
// tests to avoid calling lipgloss.HasDarkBackground() and time.Now() directly.
func NewWithTheme(version, username string, sub Subscriber,
	myPRs, reviewQueue, watches, detailPane, commandBar SubModel,
	theme Theme, clock Clock) Model {

	ch := make(chan eventbus.Event, 64)
	unsubscribe := sub.Subscribe(func(e eventbus.Event) {
		select {
		case ch <- e:
		default:
		}
	})

	return Model{
		version:     version,
		username:    username,
		focused:     PanelMyPRs,
		myPRs:       myPRs,
		reviewQueue: reviewQueue,
		watches:     watches,
		detailPane:  detailPane,
		commandBar:  commandBar,
		detailOpen:  false,
		statusText:  "",
		clock:       clock,
		eventCh:     ch,
		unsubscribe: unsubscribe,
		theme:       theme,
	}
}

// WithDNDToggler returns a copy of m with the DND toggler set to t. The header
// shows the "🔕 DND" indicator when the toggler reports DND active, and the D
// key binding calls Toggle().
func (m Model) WithDNDToggler(t DNDToggler) Model {
	m.dndToggler = t
	return m
}

// waitForDBEvent returns a Cmd that blocks until the next event arrives on ch,
// then wraps it in a DBEventMsg.
func waitForDBEvent(ch <-chan eventbus.Event) tea.Cmd {
	return func() tea.Msg {
		return DBEventMsg{Event: <-ch}
	}
}

// Init starts the event bus listener.
func (m Model) Init() tea.Cmd {
	return waitForDBEvent(m.eventCh)
}

// Update dispatches incoming messages to the correct sub-model and re-arms the
// event bus listener after each DBEventMsg.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch ev := msg.(type) {
	case DBEventMsg:
		return m.handleDBEvent(ev.Event)

	case tea.KeyMsg:
		return m.handleKey(ev)

	case CommandResultMsg:
		if ev.Err != nil {
			m.statusText = "error: " + ev.Err.Error()
		} else {
			m.statusText = ev.Status
		}
		m.statusEventType = eventbus.PRUpdated // use neutral colour
		m.lastEventTime = m.clock.Now()
		m.commandBarFocused = false
		var cmd tea.Cmd
		m.commandBar, cmd = m.commandBar.Update(BlurCommandBarMsg{})
		return m, tea.Batch(cmd, waitForDBEvent(m.eventCh))

	case CommandComposeMsg:
		m.statusText = ev.Prompt
		m.statusEventType = eventbus.PRUpdated
		m.lastEventTime = m.clock.Now()
		return m, waitForDBEvent(m.eventCh)

	case ShowHelpMsg:
		m.helpVisible = true
		return m, waitForDBEvent(m.eventCh)

	case ToggleDNDMsg:
		if m.dndToggler != nil {
			m.dndToggler.Toggle()
		}
		return m, waitForDBEvent(m.eventCh)

	case ReviewSuggestionsMsg:
		// Focus the command bar and forward the message so it can update its
		// collaborator list and pre-fill the input.
		m.commandBarFocused = true
		var cmd tea.Cmd
		m.commandBar, cmd = m.commandBar.Update(ev)
		return m, tea.Batch(cmd, waitForDBEvent(m.eventCh))

	default:
		// Forward unrecognised messages to all sub-models.
		var cmds []tea.Cmd
		m.myPRs, _ = m.myPRs.Update(msg)
		m.reviewQueue, _ = m.reviewQueue.Update(msg)
		m.watches, _ = m.watches.Update(msg)
		m.detailPane, _ = m.detailPane.Update(msg)
		m.commandBar, _ = m.commandBar.Update(msg)
		return m, tea.Batch(cmds...)
	}
}

// handleDBEvent routes a bus event to the appropriate sub-model(s) and
// updates status text, then re-arms the event listener.
func (m Model) handleDBEvent(e eventbus.Event) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch e.Type {
	case eventbus.PRUpdated:
		var c1, c2 tea.Cmd
		m.myPRs, c1 = m.myPRs.Update(DBEventMsg{Event: e})
		m.reviewQueue, c2 = m.reviewQueue.Update(DBEventMsg{Event: e})
		cmds = append(cmds, c1, c2)
		m.statusText = statusTextForEvent(e)
		m.statusEventType = e.Type
		m.lastEventTime = m.clock.Now()

	case eventbus.CIChanged:
		var c1, c2 tea.Cmd
		m.myPRs, c1 = m.myPRs.Update(DBEventMsg{Event: e})
		m.reviewQueue, c2 = m.reviewQueue.Update(DBEventMsg{Event: e})
		cmds = append(cmds, c1, c2)
		m.statusText = statusTextForEvent(e)
		m.statusEventType = e.Type
		m.lastEventTime = m.clock.Now()

	case eventbus.ReviewChanged:
		var c1, c2 tea.Cmd
		m.myPRs, c1 = m.myPRs.Update(DBEventMsg{Event: e})
		m.reviewQueue, c2 = m.reviewQueue.Update(DBEventMsg{Event: e})
		cmds = append(cmds, c1, c2)
		m.statusText = statusTextForEvent(e)
		m.statusEventType = e.Type
		m.lastEventTime = m.clock.Now()

	case eventbus.WatchFired:
		var c tea.Cmd
		m.watches, c = m.watches.Update(DBEventMsg{Event: e})
		cmds = append(cmds, c)
		m.statusText = statusTextForEvent(e)
		m.statusEventType = e.Type
		m.lastEventTime = m.clock.Now()

	case eventbus.RateLimitWarning:
		m.statusText = "⚠ API rate limit low"
		m.statusEventType = e.Type
		m.lastEventTime = m.clock.Now()
	}

	// Re-arm the listener so we receive the next event.
	cmds = append(cmds, waitForDBEvent(m.eventCh))
	return m, tea.Batch(cmds...)
}

// statusTextForEvent returns a status bar string for a bus event, extracting
// PR details when available.
func statusTextForEvent(e eventbus.Event) string {
	switch e.Type {
	case eventbus.PRUpdated:
		if pr, ok := e.After.(persistence.PullRequest); ok {
			return fmt.Sprintf("● PR #%d updated", pr.Number)
		}
		return "● PR updated"
	case eventbus.CIChanged:
		if pr, ok := e.After.(persistence.PullRequest); ok {
			symbol := prCIDisplay(pr.CIState)
			return fmt.Sprintf("%s PR #%d CI %s", symbol, pr.Number, pr.CIState)
		}
		return "● CI state changed"
	case eventbus.ReviewChanged:
		if pr, ok := e.After.(persistence.PullRequest); ok {
			return fmt.Sprintf("● PR #%d review changed", pr.Number)
		}
		return "● Review changed"
	case eventbus.WatchFired:
		return "● Watch fired"
	default:
		return ""
	}
}

// formatTimeAgo formats a duration as a human-readable "X ago" string.
func formatTimeAgo(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh ago", int(d.Hours()))
}

// notifColor returns the lipgloss foreground color appropriate for the event type
// and the current status text content.
func notifColor(eventType eventbus.EventType, statusText string) lipgloss.Color {
	switch eventType {
	case eventbus.CIChanged:
		if len(statusText) > 0 && (containsAny(statusText, "passing", "success")) {
			return lipgloss.Color("#4CAF50") // green
		}
		return lipgloss.Color("#FF6B6B") // red
	case eventbus.ReviewChanged:
		if containsAny(statusText, "approved") {
			return lipgloss.Color("#4CAF50") // green
		}
		if containsAny(statusText, "changes") {
			return lipgloss.Color("#FF6B6B") // red
		}
		return lipgloss.Color("#42A5F5") // blue
	case eventbus.WatchFired:
		return lipgloss.Color("#4CAF50") // green
	case eventbus.RateLimitWarning:
		return lipgloss.Color("#FFC107") // yellow
	default:
		return lipgloss.Color("#42A5F5") // blue
	}
}

// containsAny reports whether s contains any of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

// handleKey handles all global key bindings.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		if m.unsubscribe != nil {
			m.unsubscribe()
		}
		return m, tea.Quit

	case "tab":
		m.focused = (m.focused + 1) % 3

	case "enter", "p":
		if m.commandBarFocused {
			var cmd tea.Cmd
			m.commandBar, cmd = m.commandBar.Update(msg)
			return m, tea.Batch(cmd, waitForDBEvent(m.eventCh))
		}
		m.detailOpen = !m.detailOpen

	case "n", "N":
		if m.detailOpen {
			var cmd tea.Cmd
			m.detailPane, cmd = m.detailPane.Update(msg)
			return m, tea.Batch(cmd, waitForDBEvent(m.eventCh))
		}

	case "j", "down":
		return m.dispatchToFocused(MoveFocusMsg{Down: true})

	case "k", "up":
		return m.dispatchToFocused(MoveFocusMsg{Down: false})

	case "/", ":":
		m.commandBarFocused = true
		var cmd tea.Cmd
		m.commandBar, cmd = m.commandBar.Update(FocusCommandBarMsg{})
		return m, tea.Batch(cmd, waitForDBEvent(m.eventCh))

	case "esc":
		if m.helpVisible {
			m.helpVisible = false
		} else if m.commandBarFocused {
			m.commandBarFocused = false
			var cmd tea.Cmd
			m.commandBar, cmd = m.commandBar.Update(BlurCommandBarMsg{})
			return m, tea.Batch(cmd, waitForDBEvent(m.eventCh))
		}

	case "o":
		return m.dispatchToFocused(OpenPRMsg{})

	case "d":
		return m.dispatchToFocused(ShowDiffMsg{})

	case "a":
		if m.focused == PanelReviewQueue {
			var cmd tea.Cmd
			m.reviewQueue, cmd = m.reviewQueue.Update(ApprovePRMsg{})
			return m, tea.Batch(cmd, waitForDBEvent(m.eventCh))
		}

	case "r":
		return m.dispatchToFocused(RequestReviewMsg{})

	case "?":
		m.helpVisible = !m.helpVisible

	case "R":
		return m, tea.Batch(
			func() tea.Msg { return ForceReloadMsg{} },
			waitForDBEvent(m.eventCh),
		)

	case "D":
		return m, tea.Batch(
			func() tea.Msg { return ToggleDNDMsg{} },
			waitForDBEvent(m.eventCh),
		)
	}

	return m, waitForDBEvent(m.eventCh)
}

// dispatchToFocused sends msg to whichever panel currently holds keyboard focus.
func (m Model) dispatchToFocused(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.focused {
	case PanelMyPRs:
		m.myPRs, cmd = m.myPRs.Update(msg)
	case PanelReviewQueue:
		m.reviewQueue, cmd = m.reviewQueue.Update(msg)
	case PanelWatches:
		m.watches, cmd = m.watches.Update(msg)
	}
	return m, tea.Batch(cmd, waitForDBEvent(m.eventCh))
}

// View composes the full terminal layout.
//
// Layout (top → bottom):
//   - Header bar
//   - My Pull Requests panel
//   - Review Queue panel
//   - Watches panel (omitted when it has no content)
//   - Detail pane (omitted when detailOpen is false)
//   - Command bar
//
// When helpVisible is true the normal layout is dimmed and the help overlay is
// rendered on top.
func (m Model) View() string {
	sections := []string{
		m.headerView(),
		m.panelView("MY PULL REQUESTS", m.myPRs, m.focused == PanelMyPRs),
		m.panelView("REVIEW QUEUE", m.reviewQueue, m.focused == PanelReviewQueue),
	}

	if m.watches.HasContent() {
		sections = append(sections, m.panelView("WATCHES", m.watches, m.focused == PanelWatches))
	}

	if m.detailOpen {
		sections = append(sections, m.detailPaneView())
	}

	sections = append(sections, m.commandBarView())

	normal := lipgloss.JoinVertical(lipgloss.Left, sections...)

	if m.helpVisible {
		return lipgloss.JoinVertical(lipgloss.Left,
			dimBackground(normal),
			renderHelpOverlay(m.theme),
		)
	}

	return normal
}

// headerView renders the top status bar.
func (m Model) headerView() string {
	left := fmt.Sprintf("  argh %s  @%s", m.version, m.username)
	right := "[?] help"
	status := ""
	if m.statusText != "" {
		elapsed := m.clock.Now().Sub(m.lastEventTime)
		color := notifColor(m.statusEventType, m.statusText)
		coloredText := lipgloss.NewStyle().Foreground(color).Render(m.statusText)
		status = "  " + coloredText + " — " + formatTimeAgo(elapsed)
	}
	dnd := ""
	if m.dndToggler != nil && m.dndToggler.IsDND() {
		dnd = "  🔕 DND"
	}
	return m.theme.Header.Render(left + status + dnd + "  " + right)
}

// panelView wraps a sub-model's View() in a titled border.
func (m Model) panelView(title string, sub SubModel, focused bool) string {
	border := m.theme.UnfocusedBorder
	if focused {
		border = m.theme.FocusedBorder
	}
	body := sub.View()
	return border.Render(m.theme.PanelTitle.Render(title) + "\n" + body)
}

// detailPaneView renders the collapsible detail pane.
func (m Model) detailPaneView() string {
	return m.theme.PanelBorder.Render(
		m.theme.PanelTitle.Render("DETAIL") + "\n" + m.detailPane.View(),
	)
}

// commandBarView renders the command bar pinned to the bottom.
func (m Model) commandBarView() string {
	return m.theme.CommandBar.Render("> " + m.commandBar.View())
}
