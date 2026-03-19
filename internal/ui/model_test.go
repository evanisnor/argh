package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/evanisnor/argh/internal/eventbus"
)

// ── test doubles ─────────────────────────────────────────────────────────────

// stubSubscriber records subscription calls without actually firing events.
type stubSubscriber struct {
	handlers []func(eventbus.Event)
}

func (s *stubSubscriber) Subscribe(h func(eventbus.Event)) func() {
	s.handlers = append(s.handlers, h)
	return func() {
		// unsubscribe: remove the last handler (sufficient for tests)
		if len(s.handlers) > 0 {
			s.handlers = s.handlers[:len(s.handlers)-1]
		}
	}
}

// publish delivers e to all registered handlers.
func (s *stubSubscriber) publish(e eventbus.Event) {
	for _, h := range s.handlers {
		h(e)
	}
}

// stubSubModel captures the last message it received and exposes it for assertions.
type stubSubModel struct {
	name       string
	content    bool // HasContent return value
	lastMsg    tea.Msg
	viewResult string
}

func newStub(name string, hasContent bool) *stubSubModel {
	return &stubSubModel{name: name, content: hasContent, viewResult: name + "-view"}
}

func (s *stubSubModel) Update(msg tea.Msg) (SubModel, tea.Cmd) {
	s.lastMsg = msg
	return s, nil
}

func (s *stubSubModel) View() string {
	return s.viewResult
}

func (s *stubSubModel) HasContent() bool {
	return s.content
}

// ── helpers ───────────────────────────────────────────────────────────────────

// plainTheme returns a zero-decoration theme so View() output is easy to assert
// on without lipgloss escape codes.
func plainTheme() Theme {
	plain := lipgloss.NewStyle()
	return Theme{
		Dark:            false,
		Header:          plain,
		PanelBorder:     plain,
		PanelTitle:      plain,
		StatusBar:       plain,
		CommandBar:      plain,
		FocusedBorder:   plain,
		UnfocusedBorder: plain,
	}
}

func newTestModel(myPRs, reviewQueue, watches, detail, cmdBar SubModel) (Model, *stubSubscriber) {
	sub := &stubSubscriber{}
	m := NewWithTheme("v0.0.0", "testuser", sub, myPRs, reviewQueue, watches, detail, cmdBar, plainTheme())
	return m, sub
}

// applyMsg drives Update() once and returns the updated model.
func applyMsg(m Model, msg tea.Msg) Model {
	updated, _ := m.Update(msg)
	return updated.(Model)
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestView_ContainsAllPanelRegions verifies View() includes headers for the
// main panels, the command bar, and the application header.
func TestView_ContainsAllPanelRegions(t *testing.T) {
	myPRs := newStub("myPRs", true)
	rq := newStub("reviewQueue", true)
	watches := newStub("watches", true) // has content → should appear
	detail := newStub("detail", false)
	cmdBar := newStub("cmdBar", false)

	m, _ := newTestModel(myPRs, rq, watches, detail, cmdBar)
	// Open detail pane so it appears in the view.
	m.detailOpen = true

	view := m.View()

	regions := []string{
		"argh",
		"testuser",
		"MY PULL REQUESTS",
		"REVIEW QUEUE",
		"WATCHES",
		"DETAIL",
		"> ",
	}
	for _, region := range regions {
		if !strings.Contains(view, region) {
			t.Errorf("View() missing region %q\ngot:\n%s", region, view)
		}
	}
}

// TestView_WatchesPanelAbsentWhenNoContent verifies the Watches panel is omitted
// from View() when the watches sub-model reports HasContent() == false.
func TestView_WatchesPanelAbsentWhenNoContent(t *testing.T) {
	watches := newStub("watches", false) // no watches
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		watches,
		newStub("detail", false),
		newStub("cmdBar", false),
	)

	view := m.View()
	if strings.Contains(view, "WATCHES") {
		t.Errorf("View() should not contain WATCHES when watches has no content\ngot:\n%s", view)
	}
}

// TestView_DetailPaneAbsentWhenCollapsed verifies the detail pane is omitted
// from View() when detailOpen is false (the default).
func TestView_DetailPaneAbsentWhenCollapsed(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)
	// detailOpen is false by default

	view := m.View()
	if strings.Contains(view, "DETAIL") {
		t.Errorf("View() should not contain DETAIL when detail pane is collapsed\ngot:\n%s", view)
	}
}

// TestView_DetailPaneVisibleWhenOpen verifies the DETAIL section appears after
// toggling detailOpen to true.
func TestView_DetailPaneVisibleWhenOpen(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)
	m.detailOpen = true

	view := m.View()
	if !strings.Contains(view, "DETAIL") {
		t.Errorf("View() should contain DETAIL when detail pane is open\ngot:\n%s", view)
	}
}

// TestDBEvent_PRUpdated_DispatchesToMyPRsAndReviewQueue verifies that a
// PRUpdated bus event is forwarded to both the My PRs and Review Queue panels.
func TestDBEvent_PRUpdated_DispatchesToMyPRsAndReviewQueue(t *testing.T) {
	myPRs := newStub("myPRs", true)
	rq := newStub("reviewQueue", true)
	watches := newStub("watches", false)

	m, sub := newTestModel(myPRs, rq, watches, newStub("detail", false), newStub("cmdBar", false))

	e := eventbus.Event{Type: eventbus.PRUpdated, After: "some-pr"}
	sub.publish(e)

	m = applyMsg(m, DBEventMsg{Event: e})

	myPRsModel := m.myPRs.(*stubSubModel)
	if myPRsModel.lastMsg == nil {
		t.Error("myPRs did not receive the PRUpdated message")
	}
	rqModel := m.reviewQueue.(*stubSubModel)
	if rqModel.lastMsg == nil {
		t.Error("reviewQueue did not receive the PRUpdated message")
	}
	watchesModel := m.watches.(*stubSubModel)
	if watchesModel.lastMsg != nil {
		t.Error("watches should NOT receive PRUpdated message")
	}
}

// TestDBEvent_CIChanged_DispatchesToMyPRsAndReviewQueue verifies routing of CI events.
func TestDBEvent_CIChanged_DispatchesToMyPRsAndReviewQueue(t *testing.T) {
	myPRs := newStub("myPRs", true)
	rq := newStub("reviewQueue", true)
	watches := newStub("watches", false)

	m, _ := newTestModel(myPRs, rq, watches, newStub("detail", false), newStub("cmdBar", false))

	e := eventbus.Event{Type: eventbus.CIChanged}
	m = applyMsg(m, DBEventMsg{Event: e})

	if m.myPRs.(*stubSubModel).lastMsg == nil {
		t.Error("myPRs did not receive the CIChanged message")
	}
	if m.reviewQueue.(*stubSubModel).lastMsg == nil {
		t.Error("reviewQueue did not receive the CIChanged message")
	}
}

// TestDBEvent_ReviewChanged_DispatchesToMyPRsAndReviewQueue verifies routing of review events.
func TestDBEvent_ReviewChanged_DispatchesToMyPRsAndReviewQueue(t *testing.T) {
	myPRs := newStub("myPRs", true)
	rq := newStub("reviewQueue", true)
	watches := newStub("watches", false)

	m, _ := newTestModel(myPRs, rq, watches, newStub("detail", false), newStub("cmdBar", false))

	e := eventbus.Event{Type: eventbus.ReviewChanged}
	m = applyMsg(m, DBEventMsg{Event: e})

	if m.myPRs.(*stubSubModel).lastMsg == nil {
		t.Error("myPRs did not receive the ReviewChanged message")
	}
	if m.reviewQueue.(*stubSubModel).lastMsg == nil {
		t.Error("reviewQueue did not receive the ReviewChanged message")
	}
}

// TestDBEvent_WatchFired_DispatchesToWatchesOnly verifies that a WatchFired
// event is routed exclusively to the Watches sub-model.
func TestDBEvent_WatchFired_DispatchesToWatchesOnly(t *testing.T) {
	myPRs := newStub("myPRs", true)
	rq := newStub("reviewQueue", true)
	watches := newStub("watches", true)

	m, _ := newTestModel(myPRs, rq, watches, newStub("detail", false), newStub("cmdBar", false))

	e := eventbus.Event{Type: eventbus.WatchFired}
	m = applyMsg(m, DBEventMsg{Event: e})

	if m.watches.(*stubSubModel).lastMsg == nil {
		t.Error("watches did not receive the WatchFired message")
	}
	if m.myPRs.(*stubSubModel).lastMsg != nil {
		t.Error("myPRs should NOT receive WatchFired message")
	}
	if m.reviewQueue.(*stubSubModel).lastMsg != nil {
		t.Error("reviewQueue should NOT receive WatchFired message")
	}
}

// TestDBEvent_UpdatesStatusText verifies that each event type sets a non-empty status text.
func TestDBEvent_UpdatesStatusText(t *testing.T) {
	tests := []struct {
		eventType   eventbus.EventType
		wantContain string
	}{
		{eventbus.PRUpdated, "PR"},
		{eventbus.CIChanged, "CI"},
		{eventbus.ReviewChanged, "Review"},
		{eventbus.WatchFired, "Watch"},
		{eventbus.RateLimitWarning, "rate limit"},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			m, _ := newTestModel(
				newStub("myPRs", true),
				newStub("reviewQueue", true),
				newStub("watches", true),
				newStub("detail", false),
				newStub("cmdBar", false),
			)

			m = applyMsg(m, DBEventMsg{Event: eventbus.Event{Type: tt.eventType}})

			if !strings.Contains(m.statusText, tt.wantContain) {
				t.Errorf("statusText %q does not contain %q", m.statusText, tt.wantContain)
			}
		})
	}
}

// TestKey_TabCyclesFocusedPanel verifies Tab cycles through the three panels.
func TestKey_TabCyclesFocusedPanel(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", true),
		newStub("detail", false),
		newStub("cmdBar", false),
	)

	if m.focused != PanelMyPRs {
		t.Fatalf("initial focus should be PanelMyPRs, got %d", m.focused)
	}

	tabMsg := tea.KeyMsg{Type: tea.KeyTab}

	m = applyMsg(m, tabMsg)
	if m.focused != PanelReviewQueue {
		t.Errorf("after 1 tab: want PanelReviewQueue(%d), got %d", PanelReviewQueue, m.focused)
	}

	m = applyMsg(m, tabMsg)
	if m.focused != PanelWatches {
		t.Errorf("after 2 tabs: want PanelWatches(%d), got %d", PanelWatches, m.focused)
	}

	m = applyMsg(m, tabMsg)
	if m.focused != PanelMyPRs {
		t.Errorf("after 3 tabs (wrap): want PanelMyPRs(%d), got %d", PanelMyPRs, m.focused)
	}
}

// TestKey_EnterTogglesDetailPane verifies Enter toggles the detail pane open/closed.
func TestKey_EnterTogglesDetailPane(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)

	if m.detailOpen {
		t.Fatal("detail pane should be closed initially")
	}

	enterMsg := tea.KeyMsg{Type: tea.KeyEnter}

	m = applyMsg(m, enterMsg)
	if !m.detailOpen {
		t.Error("detail pane should be open after pressing Enter")
	}

	m = applyMsg(m, enterMsg)
	if m.detailOpen {
		t.Error("detail pane should be closed after pressing Enter again")
	}
}

// TestKey_PTogglesDetailPane verifies p key toggles the detail pane.
func TestKey_PTogglesDetailPane(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)

	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if !m.detailOpen {
		t.Error("detail pane should be open after pressing p")
	}

	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if m.detailOpen {
		t.Error("detail pane should be closed after pressing p again")
	}
}

// TestInit_ReturnsNonNilCmd verifies Init() returns a non-nil Cmd so that
// the event bus listener is started when the program begins.
func TestInit_ReturnsNonNilCmd(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)

	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return a non-nil Cmd")
	}
}

// TestView_HeaderContainsVersionAndUsername verifies the header bar shows
// the app version and authenticated username.
func TestView_HeaderContainsVersionAndUsername(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)

	view := m.View()
	if !strings.Contains(view, "v0.0.0") {
		t.Errorf("View() header missing version string, got:\n%s", view)
	}
	if !strings.Contains(view, "testuser") {
		t.Errorf("View() header missing username, got:\n%s", view)
	}
}

// TestNewWithTheme_UsesProvidedTheme verifies that NewWithTheme stores the
// provided theme (dark flag is observable via the Theme field).
func TestNewWithTheme_UsesProvidedTheme(t *testing.T) {
	darkTheme := plainTheme()
	darkTheme.Dark = true

	sub := &stubSubscriber{}
	m := NewWithTheme("v1.0.0", "alice", sub,
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
		darkTheme,
	)

	if !m.theme.Dark {
		t.Error("theme.Dark should be true when a dark theme is provided")
	}
}

// TestNew_ConstructsModel verifies that New creates a valid model using the
// automatic theme detection path (lipgloss.HasDarkBackground).
func TestNew_ConstructsModel(t *testing.T) {
	sub := &stubSubscriber{}
	m := New("v1.0.0", "bob", sub,
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)

	if m.version != "v1.0.0" {
		t.Errorf("version = %q, want v1.0.0", m.version)
	}
	if m.username != "bob" {
		t.Errorf("username = %q, want bob", m.username)
	}
	if m.eventCh == nil {
		t.Error("eventCh should not be nil")
	}
	if m.unsubscribe == nil {
		t.Error("unsubscribe should not be nil")
	}
}

// TestNewTheme_DarkAndLight verifies newTheme produces distinct themes for
// dark and light terminals.
func TestNewTheme_DarkAndLight(t *testing.T) {
	dark := newTheme(true)
	light := newTheme(false)

	if !dark.Dark {
		t.Error("dark theme should have Dark=true")
	}
	if light.Dark {
		t.Error("light theme should have Dark=false")
	}
}

// TestWaitForDBEvent_ReturnsEventOnChannel verifies that the Cmd returned by
// waitForDBEvent immediately reads from a pre-filled channel.
func TestWaitForDBEvent_ReturnsEventOnChannel(t *testing.T) {
	ch := make(chan eventbus.Event, 1)
	want := eventbus.Event{Type: eventbus.CIChanged}
	ch <- want

	cmd := waitForDBEvent(ch)
	msg := cmd()

	got, ok := msg.(DBEventMsg)
	if !ok {
		t.Fatalf("cmd() returned %T, want DBEventMsg", msg)
	}
	if got.Event.Type != want.Type {
		t.Errorf("event type = %q, want %q", got.Event.Type, want.Type)
	}
}

// TestUpdate_DefaultCaseBroadcastsToAllSubModels verifies that an unrecognised
// message type is forwarded to every sub-model.
func TestUpdate_DefaultCaseBroadcastsToAllSubModels(t *testing.T) {
	myPRs := newStub("myPRs", true)
	rq := newStub("reviewQueue", true)
	watches := newStub("watches", true)
	detail := newStub("detail", false)
	cmdBar := newStub("cmdBar", false)

	m, _ := newTestModel(myPRs, rq, watches, detail, cmdBar)

	type unknownMsg struct{ val int }
	m = applyMsg(m, unknownMsg{val: 42})

	for _, sub := range []struct {
		name string
		got  tea.Msg
	}{
		{"myPRs", m.myPRs.(*stubSubModel).lastMsg},
		{"reviewQueue", m.reviewQueue.(*stubSubModel).lastMsg},
		{"watches", m.watches.(*stubSubModel).lastMsg},
		{"detailPane", m.detailPane.(*stubSubModel).lastMsg},
		{"commandBar", m.commandBar.(*stubSubModel).lastMsg},
	} {
		if sub.got == nil {
			t.Errorf("sub-model %q did not receive the unknown message", sub.name)
		}
	}
}

// TestStatusTextForEvent_Default verifies the default case returns empty string.
func TestStatusTextForEvent_Default(t *testing.T) {
	e := eventbus.Event{Type: "UNKNOWN_TYPE"}
	got := statusTextForEvent(e)
	if got != "" {
		t.Errorf("statusTextForEvent for unknown type = %q, want empty string", got)
	}
}

// TestKey_QuitReturnsQuitCmd verifies that pressing q returns tea.Quit.
func TestKey_QuitReturnsQuitCmd(t *testing.T) {
	m, sub := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)

	initialHandlers := len(sub.handlers)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("Update with 'q' should return a non-nil Cmd")
	}

	// unsubscribe should have been called
	if len(sub.handlers) >= initialHandlers {
		t.Error("unsubscribe should have removed the handler")
	}

	// Execute the cmd; it should return tea.QuitMsg.
	result := cmd()
	if _, ok := result.(tea.QuitMsg); !ok {
		t.Errorf("cmd() returned %T, want tea.QuitMsg", result)
	}
}

// TestKey_CtrlCQuits verifies ctrl+c also triggers a quit.
func TestKey_CtrlCQuits(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("Update with ctrl+c should return a non-nil Cmd")
	}

	result := cmd()
	if _, ok := result.(tea.QuitMsg); !ok {
		t.Errorf("cmd() returned %T, want tea.QuitMsg", result)
	}
}

// TestNew_DropsEventsWhenChannelFull verifies that the subscriber created by New
// does not block when the internal event channel is full (64 buffered slots).
func TestNew_DropsEventsWhenChannelFull(t *testing.T) {
	sub := &stubSubscriber{}
	New("v1.0.0", "test", sub,
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)

	// Publish 70 events synchronously — the first 64 fill the buffer, the
	// remaining 6 hit the default branch and are dropped without blocking.
	for i := 0; i < 70; i++ {
		sub.publish(eventbus.Event{Type: eventbus.PRUpdated})
	}
	// If we reach here the subscriber did not block, which is the assertion.
}

// TestView_HeaderShowsStatusText verifies that a non-empty statusText appears in the header.
func TestView_HeaderShowsStatusText(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)
	m.statusText = "something happened"

	view := m.View()
	if !strings.Contains(view, "something happened") {
		t.Errorf("View() header missing statusText, got:\n%s", view)
	}
}
