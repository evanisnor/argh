// Package ui implements the Bubble Tea root model and terminal layout for argh.
package ui

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/evanisnor/argh/internal/api"
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

// CommandBarOverlay is the optional interface implemented by the command bar
// sub-model to expose its suggestion pane for overlay rendering. The root
// model uses a type assertion to check for this; other sub-models need not
// implement it.
type CommandBarOverlay interface {
	// HasSuggestions reports whether there are autocomplete suggestions to show.
	HasSuggestions() bool
	// SuggestionsView renders the suggestion list as a plain string (no width
	// or background styling applied; the caller handles that).
	SuggestionsView() string
}

// RowCounter is the optional interface implemented by panels that can report
// the number of data rows they contain. The root model uses a type assertion
// to append the count to the panel title, e.g. "MY PULL REQUESTS [3]".
type RowCounter interface {
	RowCount() int
}

// Focusable is the optional interface implemented by panels that need to know
// whether they currently hold keyboard focus. The root model calls SetFocused
// before each View() so the panel can gate visual indicators like cursor
// highlighting on focus state.
type Focusable interface {
	SetFocused(focused bool)
}

// PRSelector is the optional interface implemented by panels that hold a list
// of pull requests with a cursor. The root model uses a type assertion to check
// for this when handling the Enter key so that the detail modal is only opened
// when there is an actual PR selection available.
type PRSelector interface {
	SelectedPR() *persistence.PullRequest
}

// CursorNavigator is the optional interface implemented by panels that support
// cursor-based navigation with wrapping between panels. Only MyPRsPanel and
// ReviewQueuePanel implement this; the Watches panel does not participate in
// cross-panel wrapping.
type CursorNavigator interface {
	CursorPosition() int
	SetCursor(pos int)
}

// PRDetailReader is the data-access interface the root model uses to populate
// the detail pane when a PR is selected. It is satisfied by *persistence.DB.
type PRDetailReader interface {
	ListCheckRuns(prID string) ([]persistence.CheckRun, error)
	ListReviewThreads(prID string) ([]persistence.ReviewThread, error)
	ListWatches() ([]persistence.Watch, error)
	ListTimelineEvents(prID string) ([]persistence.TimelineEvent, error)
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

// ResizeMsg is sent to every sub-model when the root model receives a
// tea.WindowSizeMsg. Width and Height are the dimensions allocated to that
// sub-model (Width is always the full terminal width; Height is the sub-model's
// share of the available vertical space).
type ResizeMsg struct {
	Width  int
	Height int
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
	helpViewport      viewport.Model
	commandBarFocused bool
	statusText        string
	statusEventType   eventbus.EventType
	lastEventTime     time.Time
	clock             Clock
	eventCh           chan eventbus.Event
	unsubscribe       func()
	theme             Theme
	browser           BrowserOpener  // optional; nil = browser not available
	dndToggler        DNDToggler     // optional; nil = no DND control
	detailReader      PRDetailReader // optional; nil = detail pane not populated
	width             int        // terminal width, 0 until first tea.WindowSizeMsg
	height            int        // terminal height, 0 until first tea.WindowSizeMsg
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

	t := newTheme(lipgloss.HasDarkBackground())
	vp := viewport.New(80, 20)
	vp.SetContent(renderHelpContent(t, version, username))

	return Model{
		version:      version,
		username:     username,
		focused:      PanelMyPRs,
		myPRs:        myPRs,
		reviewQueue:  reviewQueue,
		watches:      watches,
		detailPane:   detailPane,
		commandBar:   commandBar,
		detailOpen:   false,
		helpViewport: vp,
		statusText:   "",
		clock:        realClock{},
		eventCh:      ch,
		unsubscribe:  unsubscribe,
		theme:        t,
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

	vp2 := viewport.New(80, 20)
	vp2.SetContent(renderHelpContent(theme, version, username))

	return Model{
		version:      version,
		username:     username,
		focused:      PanelMyPRs,
		myPRs:        myPRs,
		reviewQueue:  reviewQueue,
		watches:      watches,
		detailPane:   detailPane,
		commandBar:   commandBar,
		detailOpen:   false,
		helpViewport: vp2,
		statusText:   "",
		clock:        clock,
		eventCh:      ch,
		unsubscribe:  unsubscribe,
		theme:        theme,
	}
}

// WithBrowser returns a copy of m with the browser opener set to b. The 'o'
// key binding uses this to open the focused PR's URL in the system browser.
func (m Model) WithBrowser(b BrowserOpener) Model {
	m.browser = b
	return m
}

// WithDNDToggler returns a copy of m with the DND toggler set to t. The header
// shows the "🔕 DND" indicator when the toggler reports DND active, and the D
// key binding calls Toggle().
func (m Model) WithDNDToggler(t DNDToggler) Model {
	m.dndToggler = t
	return m
}

// WithDetailReader returns a copy of m with the detail reader set to r.
// When set, the detail pane is populated with real PR data (check runs,
// review threads, watches, timeline) when the user opens it.
func (m Model) WithDetailReader(r PRDetailReader) Model {
	m.detailReader = r
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

	case tea.WindowSizeMsg:
		m.width = ev.Width
		m.height = ev.Height
		vpW, vpH := helpViewportSize(m.width, m.height)
		m.helpViewport.Width = vpW
		m.helpViewport.Height = vpH
		m.helpViewport.SetContent(renderHelpContent(m.theme, m.version, m.username))
		m.propagateResize()
		return m, waitForDBEvent(m.eventCh)

	case ShowHelpMsg:
		m.helpVisible = true
		m.helpViewport.GotoTop()
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

	case eventbus.PRRemoved:
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

	case eventbus.SessionIDsAssigned:
		var c1, c2 tea.Cmd
		m.myPRs, c1 = m.myPRs.Update(DBEventMsg{Event: e})
		m.reviewQueue, c2 = m.reviewQueue.Update(DBEventMsg{Event: e})
		cmds = append(cmds, c1, c2)

	case eventbus.RateLimitWarning:
		m.statusText = "⚠ API rate limit low"
		m.statusEventType = e.Type
		m.lastEventTime = m.clock.Now()

	case eventbus.SSORequired:
		if info, ok := e.After.(api.SSOInfo); ok {
			m.statusText = fmt.Sprintf("SSO required for %s - authorize: %s", info.OrgName, info.AuthorizationURL)
			m.statusEventType = e.Type
			m.lastEventTime = m.clock.Now()
		}
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
	case eventbus.PRRemoved:
		if pr, ok := e.Before.(persistence.PullRequest); ok {
			return fmt.Sprintf("PR #%d removed", pr.Number)
		}
		return "PR removed"
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
	case eventbus.SSORequired:
		return lipgloss.Color("#FFC107") // yellow
	case eventbus.PRRemoved:
		return lipgloss.Color("#888888") // faint/neutral
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
	slog.Debug("model.handleKey", "key", msg.String(), "commandBarFocused", m.commandBarFocused)

	// When the help overlay is visible, scroll keys are routed to the
	// viewport; ? and Esc dismiss the overlay; all other keys are swallowed.
	if m.helpVisible {
		switch msg.String() {
		case "?", "esc":
			m.helpVisible = false
		case "j", "down", "pgdown":
			m.helpViewport, _ = m.helpViewport.Update(msg)
		case "k", "up", "pgup":
			m.helpViewport, _ = m.helpViewport.Update(msg)
		}
		return m, waitForDBEvent(m.eventCh)
	}

	// When the command bar is focused, every keystroke goes directly to it
	// so the textinput receives every character. Only ctrl+c (quit) and esc
	// (blur) are kept as root-model concerns.
	if m.commandBarFocused {
		switch msg.String() {
		case "ctrl+c", "esc":
			// handled by the switch below
		default:
			slog.Debug("model.handleKey: forwarding to command bar", "key", msg.String())
			var cmd tea.Cmd
			m.commandBar, cmd = m.commandBar.Update(msg)
			return m, tea.Batch(cmd, waitForDBEvent(m.eventCh))
		}
	}

	switch msg.String() {
	case "q", "ctrl+c":
		if m.unsubscribe != nil {
			m.unsubscribe()
		}
		return m, tea.Quit

	case "tab":
		m.focused = m.nextVisiblePanel()

	case "enter", "p":
		if m.detailOpen {
			m.detailOpen = false
		} else {
			m.detailOpen, _ = m.tryOpenDetail()
		}

	case "n", "N":
		if m.detailOpen {
			var cmd tea.Cmd
			m.detailPane, cmd = m.detailPane.Update(msg)
			return m, tea.Batch(cmd, waitForDBEvent(m.eventCh))
		}

	case "j", "down":
		if m.detailOpen {
			var cmd tea.Cmd
			m.detailPane, cmd = m.detailPane.Update(msg)
			return m, tea.Batch(cmd, waitForDBEvent(m.eventCh))
		}
		return m.handlePanelCursorMove(true)

	case "k", "up":
		if m.detailOpen {
			var cmd tea.Cmd
			m.detailPane, cmd = m.detailPane.Update(msg)
			return m, tea.Batch(cmd, waitForDBEvent(m.eventCh))
		}
		return m.handlePanelCursorMove(false)

	case "pgdown":
		if m.detailOpen {
			result, cmd := m.dispatchToFocused(MoveFocusMsg{Down: true})
			m = result.(Model)
			m.refreshDetailForCursor()
			return m, tea.Batch(cmd, waitForDBEvent(m.eventCh))
		}

	case "pgup":
		if m.detailOpen {
			result, cmd := m.dispatchToFocused(MoveFocusMsg{Down: false})
			m = result.(Model)
			m.refreshDetailForCursor()
			return m, tea.Batch(cmd, waitForDBEvent(m.eventCh))
		}

	case "/", ":":
		slog.Debug("model.handleKey: activating command bar", "key", msg.String())
		m.commandBarFocused = true
		var cmd tea.Cmd
		m.commandBar, cmd = m.commandBar.Update(FocusCommandBarMsg{})
		return m, tea.Batch(cmd, waitForDBEvent(m.eventCh))

	case "esc":
		if m.commandBarFocused {
			m.commandBarFocused = false
			var cmd tea.Cmd
			m.commandBar, cmd = m.commandBar.Update(BlurCommandBarMsg{})
			return m, tea.Batch(cmd, waitForDBEvent(m.eventCh))
		}

	case "o":
		sel := m.focusedPRSelector()
		if sel == nil {
			return m, waitForDBEvent(m.eventCh)
		}
		pr := sel.SelectedPR()
		if pr == nil {
			return m, waitForDBEvent(m.eventCh)
		}
		if m.browser == nil {
			m.statusText = "error: no browser opener configured"
			m.statusEventType = eventbus.PRUpdated
			m.lastEventTime = m.clock.Now()
			return m, waitForDBEvent(m.eventCh)
		}
		url := pr.URL
		return m, tea.Batch(func() tea.Msg {
			if err := m.browser.Open(url); err != nil {
				return CommandResultMsg{Err: err}
			}
			return CommandResultMsg{Status: fmt.Sprintf("opened %s", url)}
		}, waitForDBEvent(m.eventCh))

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
		m.helpVisible = true
		m.helpViewport.GotoTop()

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

// nextVisiblePanel returns the next panel in the Tab cycle, skipping the
// Watches panel when it has no content.
func (m Model) nextVisiblePanel() Panel {
	next := m.focused + 1
	if next == PanelWatches && !m.watches.HasContent() {
		next++
	}
	if next > PanelWatches {
		next = PanelMyPRs
	}
	return next
}

// focusedPRSubModel returns the SubModel for the currently focused PR panel.
// Only called after otherPRPanel confirms the focus is on MyPRs or ReviewQueue.
func (m Model) focusedPRSubModel() SubModel {
	if m.focused == PanelMyPRs {
		return m.myPRs
	}
	return m.reviewQueue
}

// otherPRPanel returns the "other" PR panel when the focused panel is MyPRs
// or ReviewQueue. Returns -1 when the focused panel is Watches (no wrapping).
func (m Model) otherPRPanel() Panel {
	switch m.focused {
	case PanelMyPRs:
		return PanelReviewQueue
	case PanelReviewQueue:
		return PanelMyPRs
	default:
		return -1
	}
}

// subModelForPanel returns the SubModel for the given panel.
func (m Model) subModelForPanel(p Panel) SubModel {
	if p == PanelMyPRs {
		return m.myPRs
	}
	return m.reviewQueue
}

// handlePanelCursorMove checks whether j/k should wrap between MyPRs and
// ReviewQueue. If the focused panel implements CursorNavigator and the cursor
// is at a boundary, it switches focus to the other PR panel. Otherwise it
// dispatches a normal MoveFocusMsg.
func (m Model) handlePanelCursorMove(down bool) (tea.Model, tea.Cmd) {
	other := m.otherPRPanel()
	if other < 0 {
		// Watches panel — no wrapping, just dispatch normally.
		result, cmd := m.dispatchToFocused(MoveFocusMsg{Down: down})
		m = result.(Model)
		return m, tea.Batch(cmd, waitForDBEvent(m.eventCh))
	}

	focused := m.focusedPRSubModel()
	nav, isNav := focused.(CursorNavigator)
	rc, isRC := focused.(RowCounter)
	if !isNav || !isRC || rc.RowCount() == 0 {
		// Panel doesn't support wrapping or is empty — dispatch normally.
		result, cmd := m.dispatchToFocused(MoveFocusMsg{Down: down})
		m = result.(Model)
		return m, tea.Batch(cmd, waitForDBEvent(m.eventCh))
	}

	if down && nav.CursorPosition() == rc.RowCount()-1 {
		// At the bottom — wrap to the other panel's top.
		otherSub := m.subModelForPanel(other)
		if otherNav, ok := otherSub.(CursorNavigator); ok {
			otherNav.SetCursor(0)
		}
		m.focused = other
		return m, waitForDBEvent(m.eventCh)
	}

	if !down && nav.CursorPosition() == 0 {
		// At the top — wrap to the other panel's bottom.
		otherSub := m.subModelForPanel(other)
		otherRC, hasRC := otherSub.(RowCounter)
		if otherNav, ok := otherSub.(CursorNavigator); ok && hasRC && otherRC.RowCount() > 0 {
			otherNav.SetCursor(otherRC.RowCount() - 1)
		}
		m.focused = other
		return m, waitForDBEvent(m.eventCh)
	}

	// Not at a boundary — dispatch normal move.
	result, cmd := m.dispatchToFocused(MoveFocusMsg{Down: down})
	m = result.(Model)
	return m, tea.Batch(cmd, waitForDBEvent(m.eventCh))
}

// focusedPRSelector returns the focused panel as a PRSelector if the currently
// focused panel supports PR selection, otherwise returns nil.
func (m Model) focusedPRSelector() PRSelector {
	switch m.focused {
	case PanelMyPRs:
		if s, ok := m.myPRs.(PRSelector); ok {
			return s
		}
	case PanelReviewQueue:
		if s, ok := m.reviewQueue.(PRSelector); ok {
			return s
		}
	}
	return nil
}

// tryOpenDetail attempts to open the detail modal for the currently focused PR.
// Returns (true, cmd) when a PR is selected and the detail reader is available;
// (false, nil) otherwise (wrong panel, empty panel, or no reader wired up).
func (m *Model) tryOpenDetail() (bool, tea.Cmd) {
	sel := m.focusedPRSelector()
	if sel == nil {
		return false, nil
	}
	pr := sel.SelectedPR()
	if pr == nil {
		return false, nil
	}
	msg := m.buildPRFocusedMsg(pr)
	m.detailPane, _ = m.detailPane.Update(msg)
	return true, nil
}

// refreshDetailForCursor re-populates the detail pane with the PR currently
// under the cursor. Called after cursor movement when the detail modal is open.
func (m *Model) refreshDetailForCursor() {
	sel := m.focusedPRSelector()
	if sel == nil {
		return
	}
	pr := sel.SelectedPR()
	if pr == nil {
		return
	}
	msg := m.buildPRFocusedMsg(pr)
	m.detailPane, _ = m.detailPane.Update(msg)
}

// buildPRFocusedMsg fetches all detail data for pr from detailReader and
// returns a populated PRFocusedMsg. If detailReader is nil, the message
// contains only the PR itself with empty slices.
func (m Model) buildPRFocusedMsg(pr *persistence.PullRequest) PRFocusedMsg {
	msg := PRFocusedMsg{PR: *pr}
	if m.detailReader == nil {
		return msg
	}
	msg.CheckRuns, _ = m.detailReader.ListCheckRuns(pr.ID)
	msg.Threads, _ = m.detailReader.ListReviewThreads(pr.ID)
	msg.Watches, _ = m.detailReader.ListWatches()
	msg.TimelineEvents, _ = m.detailReader.ListTimelineEvents(pr.ID)
	return msg
}

// propagateResize sends each sub-model a ResizeMsg with its allocated width and
// height. Width is always the full terminal width. Height is split evenly among
// the visible panels (2 panels by default, 3 when watches has content).
// The detail pane receives modal-sized dimensions (75% of terminal minus border
// overhead). The command bar receives the full terminal dimensions.
func (m *Model) propagateResize() {
	n := m.numVisiblePanels()
	panelH := m.panelContentHeight(n)
	panelW := m.width

	m.myPRs, _ = m.myPRs.Update(ResizeMsg{Width: panelW, Height: panelH})
	m.reviewQueue, _ = m.reviewQueue.Update(ResizeMsg{Width: panelW, Height: panelH})
	m.watches, _ = m.watches.Update(ResizeMsg{Width: panelW, Height: panelH})

	// Send modal-sized dimensions: 75% of terminal minus border overhead
	// (2 cols for left/right border, 3 rows for top border + title + bottom border).
	modalContentW := m.width*3/4 - 2
	modalContentH := m.height*3/4 - 3
	if modalContentW < 1 {
		modalContentW = 1
	}
	if modalContentH < 1 {
		modalContentH = 1
	}
	m.detailPane, _ = m.detailPane.Update(ResizeMsg{Width: modalContentW, Height: modalContentH})
	m.commandBar, _ = m.commandBar.Update(ResizeMsg{Width: m.width, Height: m.height})
}

// numVisiblePanels returns the count of main panels that will be rendered.
// The watches panel only renders when it has content; the detail pane is not
// counted here because it overlaps with panel space.
func (m Model) numVisiblePanels() int {
	n := 2 // My PRs + Review Queue always shown
	if m.watches.HasContent() {
		n++
	}
	return n
}

// panelContentHeight returns the inner content height (excluding the border
// top/bottom lines and the title line) allocated to each panel when there are
// n visible panels. Returns 0 when m.height is not yet known.
//
// Budget: m.height - 1 (header) - 1 (command bar) distributed across n panels,
// each of which has 2 border rows + 1 title row = 3 overhead rows.
func (m Model) panelContentHeight(n int) int {
	if m.height == 0 || n == 0 {
		return 0
	}
	const headerLines = 1
	const cmdBarLines = 1
	available := m.height - headerLines - cmdBarLines
	perPanel := available / n
	const panelOverhead = 3 // top border + title + bottom border
	inner := perPanel - panelOverhead
	if inner < 1 {
		inner = 1
	}
	return inner
}

// View composes the full terminal layout.
//
// Layout (top → bottom):
//   - Header bar
//   - My Pull Requests panel
//   - Review Queue panel
//   - Watches panel (omitted when it has no content)
//   - Command bar
//
// When detailOpen is true the detail pane is rendered as a centred floating
// modal overlaid on the dimmed panel layout rather than as a stacked section.
//
// When helpVisible is true the normal layout is dimmed and the help overlay is
// rendered on top.
//
// When m.width and m.height are non-zero (after the first tea.WindowSizeMsg),
// every element is constrained to fill the full terminal width and panels share
// the available vertical space evenly.
func (m Model) View() string {
	n := m.numVisiblePanels()
	panelH := m.panelContentHeight(n)

	sections := []string{
		m.headerView(),
		m.panelView("MY PULL REQUESTS", m.myPRs, m.focused == PanelMyPRs, panelH),
		m.panelView("REVIEW QUEUE", m.reviewQueue, m.focused == PanelReviewQueue, panelH),
	}

	if m.watches.HasContent() {
		sections = append(sections, m.panelView("WATCHES", m.watches, m.focused == PanelWatches, panelH))
	}

	sections = append(sections, m.commandBarView())

	normal := lipgloss.JoinVertical(lipgloss.Left, sections...)

	if sugg := m.commandBarSuggestionsView(); sugg != "" {
		normal = overlayAbove(normal, sugg)
	}

	if m.detailOpen {
		normal = overlayModal(normal, m.detailPaneView(), m.width, m.height)
	}

	if m.helpVisible {
		return overlayModal(normal, m.helpModalView(), m.width, m.height)
	}

	return normal
}

// headerView renders the top status bar spanning the full terminal width.
func (m Model) headerView() string {
	left := "  argh"
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
	style := m.theme.Header
	if m.width > 0 {
		style = style.Width(m.width)
	}
	return style.Render(left + status + dnd)
}

// panelView wraps a sub-model's View() in a titled border.
// contentHeight is the inner content height (rows of body text plus title line,
// excluding border rows). When contentHeight is 0 no height constraint is
// applied and the panel renders at natural height.
func (m Model) panelView(title string, sub SubModel, focused bool, contentHeight int) string {
	if rc, ok := sub.(RowCounter); ok {
		title = fmt.Sprintf("%s [%d]", title, rc.RowCount())
	}
	border := m.theme.UnfocusedBorder
	if focused {
		border = m.theme.FocusedBorder
	}
	if m.width > 0 {
		// Width sets the inner (content) width; NormalBorder adds 1 char on each
		// side, so the total outer width equals m.width.
		border = border.Width(m.width - 2)
	}
	if contentHeight > 0 {
		// Height sets the inner height. NormalBorder adds 1 line top and 1 line
		// bottom. +1 accounts for the title line inside the panel.
		border = border.Height(contentHeight + 1)
	}
	if f, ok := sub.(Focusable); ok {
		f.SetFocused(focused)
	}
	body := sub.View()
	return border.Render(m.theme.PanelTitle.Render(title) + "\n" + body)
}

// detailPaneView renders the detail pane as a floating modal box. The box uses
// a rounded border in the focused accent colour and occupies 75% of the
// terminal width and height.
func (m Model) detailPaneView() string {
	borderColor := lipgloss.Color("#7C7CF8")
	if !m.theme.Dark {
		borderColor = lipgloss.Color("#3030AA")
	}
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor)
	if m.width > 0 && m.height > 0 {
		modalW := m.width * 3 / 4
		modalH := m.height * 3 / 4
		style = style.Width(modalW - 2).Height(modalH - 3)
	}
	return style.Render(m.theme.PanelTitle.Render("DETAIL") + "\n" + m.detailPane.View())
}

// helpModalView renders the help overlay as a centered floating modal. The box
// uses a rounded border and contains the scrollable viewport.
func (m Model) helpModalView() string {
	borderColor := lipgloss.Color("#7C7CF8")
	if !m.theme.Dark {
		borderColor = lipgloss.Color("#3030AA")
	}
	w, h := helpViewportSize(m.width, m.height)
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1)
	if w > 0 && h > 0 {
		style = style.Width(w).Height(h)
	}
	scrollPct := m.helpViewport.ScrollPercent()
	footer := lipgloss.NewStyle().Faint(true).Render(
		"  j/k scroll · ? or Esc dismiss" +
			func() string {
				if scrollPct < 1.0 {
					return " · ↓ more"
				}
				return ""
			}(),
	)
	return style.Render(m.helpViewport.View() + "\n" + footer)
}

// helpViewportSize returns the inner content dimensions for the help viewport
// given terminal width and height. Returns 0,0 when dimensions are unknown.
func helpViewportSize(termWidth, termHeight int) (int, int) {
	if termWidth == 0 || termHeight == 0 {
		return 0, 0
	}
	// Modal occupies 75% of the terminal; borders + padding subtract ~4 chars
	// wide and ~4 lines tall from the available space.
	w := termWidth*3/4 - 4
	h := termHeight*3/4 - 4
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
}

// commandBarView renders the command bar pinned to the bottom spanning the full
// terminal width.
func (m Model) commandBarView() string {
	style := m.theme.CommandBar
	if m.width > 0 {
		style = style.Width(m.width)
	}
	return style.Render("> " + m.commandBar.View())
}

// commandBarSuggestionsView returns the styled suggestion overlay content, or
// an empty string when there are no suggestions or the command bar does not
// implement CommandBarOverlay.
func (m Model) commandBarSuggestionsView() string {
	cb, ok := m.commandBar.(CommandBarOverlay)
	if !ok || !cb.HasSuggestions() {
		return ""
	}
	v := cb.SuggestionsView()
	style := m.theme.CommandBar
	if m.width > 0 {
		style = style.Width(m.width)
	}
	return style.Render(v)
}
