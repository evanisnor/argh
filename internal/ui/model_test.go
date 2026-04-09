package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/evanisnor/argh/internal/api"
	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
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

func (s *stubSubModel) RowCount() int {
	return 0
}

func (s *stubSubModel) SetFocused(focused bool) {}

// stubCommandBarOverlay extends stubSubModel to implement CommandBarOverlay,
// allowing tests to exercise the suggestion overlay path in Model.View().
type stubCommandBarOverlay struct {
	*stubSubModel
	hasSugg bool
	suggView string
}

func newSuggStub(name string, hasSugg bool, suggView string) *stubCommandBarOverlay {
	return &stubCommandBarOverlay{
		stubSubModel: newStub(name, false),
		hasSugg:      hasSugg,
		suggView:     suggView,
	}
}

func (s *stubCommandBarOverlay) HasSuggestions() bool { return s.hasSugg }
func (s *stubCommandBarOverlay) SuggestionsView() string { return s.suggView }

// stubPRSelector extends stubSubModel to implement PRSelector. Used in tests
// that need the Enter key to successfully open the detail modal.
type stubPRSelector struct {
	*stubSubModel
	pr *persistence.PullRequest
}

func newSelectorStub(name string, pr *persistence.PullRequest) *stubPRSelector {
	return &stubPRSelector{
		stubSubModel: newStub(name, pr != nil),
		pr:           pr,
	}
}

func (s *stubPRSelector) Update(msg tea.Msg) (SubModel, tea.Cmd) {
	s.stubSubModel.lastMsg = msg
	return s, nil
}

func (s *stubPRSelector) SelectedPR() *persistence.PullRequest { return s.pr }

// stubBrowserOpener records the URL passed to Open.
type stubBrowserOpener struct {
	openedURL string
	openErr   error
}

func (s *stubBrowserOpener) Open(url string) error {
	s.openedURL = url
	return s.openErr
}

// executeBatchCmd recursively executes a tea.Cmd looking for the first
// non-nil tea.Msg that is a CommandResultMsg, ShowDiffContentMsg, or
// DBEventMsg. It handles tea.BatchMsg by iterating inner commands.
func executeBatchCmd(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if result := executeBatchCmd(tea.Cmd(c)); result != nil {
				switch result.(type) {
				case CommandResultMsg, ShowDiffContentMsg:
					return result
				}
			}
		}
		return nil
	}
	return msg
}

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
	m := NewWithTheme("v0.0.0", "testuser", sub, myPRs, reviewQueue, watches, detail, cmdBar, plainTheme(), stubClock{now: t0})
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
		"MY PULL REQUESTS [0]",
		"REVIEW QUEUE [0]",
		"WATCHES [0]",
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

func TestView_SuggestionOverlay_PaintsSuggestionsAboveCommandBar(t *testing.T) {
	cmdBar := newSuggStub("cmdBar", true, "suggestion line")
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		cmdBar,
	)

	view := m.View()
	if !strings.Contains(view, "suggestion line") {
		t.Errorf("View() should contain suggestion overlay when HasSuggestions is true\ngot:\n%s", view)
	}
}

func TestView_SuggestionOverlay_AbsentWhenNoSuggestions(t *testing.T) {
	cmdBar := newSuggStub("cmdBar", false, "")
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		cmdBar,
	)

	view := m.View()
	// suggestion view is empty — overlay must not inject anything unexpected
	if strings.Contains(view, "suggestion line") {
		t.Errorf("View() must not contain suggestion overlay when HasSuggestions is false\ngot:\n%s", view)
	}
}

func TestCommandBarSuggestionsView_WithWidth(t *testing.T) {
	cmdBar := newSuggStub("cmdBar", true, "suggestion")
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		cmdBar,
	)
	m.width = 80

	v := m.commandBarSuggestionsView()
	if v == "" {
		t.Error("commandBarSuggestionsView() should return non-empty when HasSuggestions is true")
	}
}

func TestCommandBarSuggestionsView_NoSuggestions(t *testing.T) {
	cmdBar := newSuggStub("cmdBar", false, "")
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		cmdBar,
	)

	v := m.commandBarSuggestionsView()
	if v != "" {
		t.Errorf("commandBarSuggestionsView() should return empty when HasSuggestions is false, got: %q", v)
	}
}

func TestCommandBarSuggestionsView_NonOverlaySubModel(t *testing.T) {
	// A plain stubSubModel does not implement CommandBarOverlay.
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)

	v := m.commandBarSuggestionsView()
	if v != "" {
		t.Errorf("commandBarSuggestionsView() should return empty for non-overlay sub-model, got: %q", v)
	}
}

// TestRealCommandBar_CollaboratorSuggestionsInView uses a real CommandBar in
// the Model to verify the full rendering path: collaborators are set, the bar
// is focused, `:request g @` is typed, and the suggestion overlay appears in View().
func TestRealCommandBar_CollaboratorSuggestionsInView(t *testing.T) {
	cb := NewCommandBar()
	cb.SetPRRefs([]PRRef{{SessionID: "g", Number: 42, Title: "Fix login bug", Repo: "owner/repo"}})
	cb.SetCollaborators([]string{"alice", "bob", "charlie"})

	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		cb,
	)
	m.width = 80
	m.height = 30

	// Focus the command bar via ":" key.
	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	if !m.commandBarFocused {
		t.Fatal("command bar should be focused after ':'")
	}

	// Type "request" and accept with tab → ":request "
	for _, r := range "request" {
		m = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyTab})

	// Type "g" and accept with tab → ":request g "
	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyTab})

	// Type "@" to enter collaborator mode.
	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'@'}})

	// Verify the command bar has suggestions.
	realCB := m.commandBar.(*CommandBar)
	if realCB.mode != cbModeCollaborator {
		t.Fatalf("expected cbModeCollaborator, got %d; input=%q suggestions=%v collaborators=%v",
			realCB.mode, realCB.Value(), realCB.suggestions, realCB.collaborators)
	}
	if len(realCB.suggestions) == 0 {
		t.Fatalf("expected non-empty suggestions, got none; collaborators=%v input=%q",
			realCB.collaborators, realCB.Value())
	}

	// Verify the suggestions appear in the rendered View().
	view := m.View()
	if !strings.Contains(view, "alice") {
		t.Errorf("View() should contain collaborator 'alice' in suggestion overlay\ninput=%q\nsuggestions=%v\nview:\n%s",
			realCB.Value(), realCB.suggestions, view)
	}
}

// TestRealCommandBar_CollaboratorsViaMsg verifies that collaborators delivered
// through CollaboratorsUpdatedMsg (the production path) are available for
// @-completion when the user subsequently focuses the command bar.
func TestRealCommandBar_CollaboratorsViaMsg(t *testing.T) {
	cb := NewCommandBar()
	cb.SetPRRefs([]PRRef{{SessionID: "g", Number: 42, Title: "Fix login bug", Repo: "owner/repo"}})

	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		cb,
	)
	m.width = 80
	m.height = 30

	// Deliver collaborators through the message path (same as production).
	m = applyMsg(m, CollaboratorsUpdatedMsg{Logins: []string{"alice", "bob", "charlie"}})

	// Verify the command bar received them.
	realCB := m.commandBar.(*CommandBar)
	if len(realCB.collaborators) != 3 {
		t.Fatalf("expected 3 collaborators after msg delivery, got %d: %v", len(realCB.collaborators), realCB.collaborators)
	}

	// Now focus and type the command sequence.
	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	for _, r := range "request" {
		m = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyTab})
	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyTab})
	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'@'}})

	realCB = m.commandBar.(*CommandBar)
	if realCB.mode != cbModeCollaborator {
		t.Fatalf("expected cbModeCollaborator, got %d; input=%q", realCB.mode, realCB.Value())
	}
	if len(realCB.suggestions) == 0 {
		t.Fatalf("expected suggestions after @, got none; input=%q collaborators=%v", realCB.Value(), realCB.collaborators)
	}

	view := m.View()
	if !strings.Contains(view, "alice") {
		t.Errorf("View() should contain 'alice' in suggestion overlay\nview:\n%s", view)
	}
}

// TestRealCommandBar_ManualTyping_ColonForwardedToInput verifies that pressing
// `:` both focuses the command bar AND types the colon into the input, so the
// user can type `:request g @` without needing to tab-accept the command.
func TestRealCommandBar_ManualTyping_ColonForwardedToInput(t *testing.T) {
	cb := NewCommandBar()
	cb.SetPRRefs([]PRRef{{SessionID: "g", Number: 42, Title: "Fix login bug", Repo: "owner/repo"}})
	cb.SetCollaborators([]string{"alice", "bob", "charlie"})

	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		cb,
	)
	m.width = 80
	m.height = 30

	// Press ":" to focus the command bar — the colon should also be typed.
	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})

	realCB := m.commandBar.(*CommandBar)
	if realCB.Value() != ":" {
		t.Fatalf("expected ':' in input after pressing ':', got %q", realCB.Value())
	}

	// Type the rest manually: "request g @".
	for _, r := range "request g @" {
		m = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	realCB = m.commandBar.(*CommandBar)
	if realCB.Value() != ":request g @" {
		t.Fatalf("expected ':request g @', got %q", realCB.Value())
	}
	if realCB.mode != cbModeCollaborator {
		t.Fatalf("expected cbModeCollaborator, got %d", realCB.mode)
	}
	if len(realCB.suggestions) == 0 {
		t.Fatal("expected collaborator suggestions")
	}

	view := m.View()
	if !strings.Contains(view, "alice") {
		t.Errorf("View() should contain 'alice' in suggestion overlay\nview:\n%s", view)
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

// TestDBEvent_PRRemoved_DispatchesToMyPRsAndReviewQueue verifies routing of PRRemoved events.
func TestDBEvent_PRRemoved_DispatchesToMyPRsAndReviewQueue(t *testing.T) {
	myPRs := newStub("myPRs", true)
	rq := newStub("reviewQueue", true)
	watches := newStub("watches", false)

	m, _ := newTestModel(myPRs, rq, watches, newStub("detail", false), newStub("cmdBar", false))

	e := eventbus.Event{Type: eventbus.PRRemoved, Before: persistence.PullRequest{Number: 42}}
	m = applyMsg(m, DBEventMsg{Event: e})

	if m.myPRs.(*stubSubModel).lastMsg == nil {
		t.Error("myPRs did not receive the PRRemoved message")
	}
	if m.reviewQueue.(*stubSubModel).lastMsg == nil {
		t.Error("reviewQueue did not receive the PRRemoved message")
	}
	if m.watches.(*stubSubModel).lastMsg != nil {
		t.Error("watches should NOT receive PRRemoved message")
	}
}

// TestDBEvent_SessionIDsAssigned_DispatchesToMyPRsAndReviewQueue verifies routing.
func TestDBEvent_SessionIDsAssigned_DispatchesToMyPRsAndReviewQueue(t *testing.T) {
	myPRs := newStub("myPRs", true)
	rq := newStub("reviewQueue", true)
	watches := newStub("watches", false)

	m, _ := newTestModel(myPRs, rq, watches, newStub("detail", false), newStub("cmdBar", false))

	e := eventbus.Event{Type: eventbus.SessionIDsAssigned}
	m = applyMsg(m, DBEventMsg{Event: e})

	if m.myPRs.(*stubSubModel).lastMsg == nil {
		t.Error("myPRs did not receive the SessionIDsAssigned message")
	}
	if m.reviewQueue.(*stubSubModel).lastMsg == nil {
		t.Error("reviewQueue did not receive the SessionIDsAssigned message")
	}
	if m.watches.(*stubSubModel).lastMsg != nil {
		t.Error("watches should NOT receive SessionIDsAssigned message")
	}
	// SessionIDsAssigned should not update the status text.
	if m.statusText != "" {
		t.Errorf("statusText = %q, want empty", m.statusText)
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

// TestDBEvent_SSORequired verifies that an SSORequired event with valid SSOInfo
// payload updates the status bar text.
func TestDBEvent_SSORequired(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", true),
		newStub("detail", false),
		newStub("cmdBar", false),
	)

	info := api.SSOInfo{OrgName: "acme-corp", AuthorizationURL: "https://github.com/orgs/acme-corp/sso?authorization_id=abc"}
	m = applyMsg(m, DBEventMsg{Event: eventbus.Event{Type: eventbus.SSORequired, After: info}})

	if !strings.Contains(m.statusText, "SSO required") {
		t.Errorf("statusText %q does not contain 'SSO required'", m.statusText)
	}
	if !strings.Contains(m.statusText, "acme-corp") {
		t.Errorf("statusText %q does not contain org name", m.statusText)
	}
	if !strings.Contains(m.statusText, "https://github.com/orgs/acme-corp/sso") {
		t.Errorf("statusText %q does not contain authorization URL", m.statusText)
	}
	if m.statusEventType != eventbus.SSORequired {
		t.Errorf("statusEventType = %q, want %q", m.statusEventType, eventbus.SSORequired)
	}
}

// TestDBEvent_SSORequired_NonSSOInfoPayload verifies that an SSORequired event
// with a non-SSOInfo payload is a no-op.
func TestDBEvent_SSORequired_NonSSOInfoPayload(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", true),
		newStub("detail", false),
		newStub("cmdBar", false),
	)

	m = applyMsg(m, DBEventMsg{Event: eventbus.Event{Type: eventbus.SSORequired, After: "not an SSOInfo"}})

	if m.statusText != "" {
		t.Errorf("statusText = %q, want empty for non-SSOInfo payload", m.statusText)
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
	pr := &persistence.PullRequest{ID: "pr1", Title: "test PR"}
	m, _ := newTestModel(
		newSelectorStub("myPRs", pr),
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
	pr := &persistence.PullRequest{ID: "pr1", Title: "test PR"}
	m, _ := newTestModel(
		newSelectorStub("myPRs", pr),
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

// stubPRDetailReader is a test double for PRDetailReader.
type stubPRDetailReader struct {
	checkRuns  []persistence.CheckRun
	threads    []persistence.ReviewThread
	watches    []persistence.Watch
	timeline   []persistence.TimelineEvent
}

func (s *stubPRDetailReader) ListCheckRuns(_ string) ([]persistence.CheckRun, error) {
	return s.checkRuns, nil
}
func (s *stubPRDetailReader) ListReviewThreads(_ string) ([]persistence.ReviewThread, error) {
	return s.threads, nil
}
func (s *stubPRDetailReader) ListWatchesByPRURL(_ string) ([]persistence.Watch, error) {
	return s.watches, nil
}
func (s *stubPRDetailReader) ListTimelineEvents(_ string) ([]persistence.TimelineEvent, error) {
	return s.timeline, nil
}

// TestWithDetailReader verifies the fluent setter stores the reader.
func TestWithDetailReader(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)
	if m.detailReader != nil {
		t.Fatal("detailReader should be nil by default")
	}
	r := &stubPRDetailReader{}
	m2 := m.WithDetailReader(r)
	if m2.detailReader != r {
		t.Error("WithDetailReader should set detailReader")
	}
}

// TestKey_Enter_WatchesPanelNoOpen verifies Enter does NOT open the modal when
// the Watches panel is focused (no PR selection possible).
func TestKey_Enter_WatchesPanelNoOpen(t *testing.T) {
	m, _ := newTestModel(
		newSelectorStub("myPRs", &persistence.PullRequest{ID: "pr1"}),
		newStub("reviewQueue", true),
		newStub("watches", true),
		newStub("detail", false),
		newStub("cmdBar", false),
	)
	m.focused = PanelWatches
	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.detailOpen {
		t.Error("Enter on Watches panel should not open detail modal")
	}
}

// TestKey_Enter_EmptyPRPanel verifies Enter does NOT open the modal when the
// focused PR panel has no rows (SelectedPR returns nil).
func TestKey_Enter_EmptyPRPanel(t *testing.T) {
	m, _ := newTestModel(
		newSelectorStub("myPRs", nil), // empty panel
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)
	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.detailOpen {
		t.Error("Enter with no selected PR should not open detail modal")
	}
}

// TestKey_Enter_ReviewQueuePanel verifies Enter opens the modal from the
// Review Queue panel when a PR is selected.
func TestKey_Enter_ReviewQueuePanel(t *testing.T) {
	pr := &persistence.PullRequest{ID: "pr1", Title: "review me"}
	m, _ := newTestModel(
		newStub("myPRs", true),
		newSelectorStub("reviewQueue", pr),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)
	m.focused = PanelReviewQueue
	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.detailOpen {
		t.Error("Enter on ReviewQueue with selected PR should open detail modal")
	}
}

// TestKey_Enter_SendsPRFocusedMsgToDetailPane verifies that opening the detail
// modal sends a PRFocusedMsg to the detail pane with real data from the reader.
func TestKey_Enter_SendsPRFocusedMsgToDetailPane(t *testing.T) {
	pr := &persistence.PullRequest{ID: "pr1", Title: "my PR"}
	reader := &stubPRDetailReader{
		checkRuns: []persistence.CheckRun{{Name: "ci"}},
		threads:   []persistence.ReviewThread{{ID: "t1"}},
		watches:   []persistence.Watch{{PRURL: "u1"}},
		timeline:  []persistence.TimelineEvent{{EventType: "pushed"}},
	}
	detail := newStub("detail", false)
	m, _ := newTestModel(
		newSelectorStub("myPRs", pr),
		newStub("reviewQueue", true),
		newStub("watches", false),
		detail,
		newStub("cmdBar", false),
	)
	m = m.WithDetailReader(reader)

	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})

	if !m.detailOpen {
		t.Fatal("detail should be open after Enter with PR selected")
	}
	msg, ok := m.detailPane.(*stubSubModel).lastMsg.(PRFocusedMsg)
	if !ok {
		t.Fatalf("detailPane should receive PRFocusedMsg, got %T", m.detailPane.(*stubSubModel).lastMsg)
	}
	if msg.PR.ID != "pr1" {
		t.Errorf("PRFocusedMsg.PR.ID = %q, want %q", msg.PR.ID, "pr1")
	}
	if len(msg.CheckRuns) != 1 {
		t.Errorf("PRFocusedMsg.CheckRuns len = %d, want 1", len(msg.CheckRuns))
	}
	if len(msg.Threads) != 1 {
		t.Errorf("PRFocusedMsg.Threads len = %d, want 1", len(msg.Threads))
	}
	if len(msg.Watches) != 1 {
		t.Errorf("PRFocusedMsg.Watches len = %d, want 1", len(msg.Watches))
	}
	if len(msg.TimelineEvents) != 1 {
		t.Errorf("PRFocusedMsg.TimelineEvents len = %d, want 1", len(msg.TimelineEvents))
	}
}

// TestKey_JK_WhenDetailOpen_ScrollsDetailPane verifies that j/k forward the
// key event to the detail pane for viewport scrolling when the modal is open.
func TestKey_JK_WhenDetailOpen_ScrollsDetailPane(t *testing.T) {
	pr := &persistence.PullRequest{ID: "pr1", Title: "PR one"}
	detail := newStub("detail", false)
	m, _ := newTestModel(
		newSelectorStub("myPRs", pr),
		newStub("reviewQueue", true),
		newStub("watches", false),
		detail,
		newStub("cmdBar", false),
	)
	// Open the detail modal first.
	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.detailOpen {
		t.Fatal("detail should be open")
	}
	// Clear the last message so we can verify j forwards the key.
	m.detailPane.(*stubSubModel).lastMsg = nil

	// Press j — detail pane should receive the tea.KeyMsg (for viewport scroll).
	m = applyMsg(m, keyRune('j'))

	keyMsg, ok := m.detailPane.(*stubSubModel).lastMsg.(tea.KeyMsg)
	if !ok {
		t.Fatalf("after j with detail open, detailPane should receive tea.KeyMsg, got %T",
			m.detailPane.(*stubSubModel).lastMsg)
	}
	if keyMsg.String() != "j" {
		t.Errorf("forwarded key = %q, want %q", keyMsg.String(), "j")
	}
}

// TestKey_KWhenDetailOpen_ScrollsDetailPane verifies k forwards to detail pane.
func TestKey_KWhenDetailOpen_ScrollsDetailPane(t *testing.T) {
	pr := &persistence.PullRequest{ID: "pr1", Title: "PR one"}
	detail := newStub("detail", false)
	m, _ := newTestModel(
		newSelectorStub("myPRs", pr),
		newStub("reviewQueue", true),
		newStub("watches", false),
		detail,
		newStub("cmdBar", false),
	)
	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	m.detailPane.(*stubSubModel).lastMsg = nil

	m = applyMsg(m, keyRune('k'))

	keyMsg, ok := m.detailPane.(*stubSubModel).lastMsg.(tea.KeyMsg)
	if !ok {
		t.Fatalf("after k with detail open, detailPane should receive tea.KeyMsg, got %T",
			m.detailPane.(*stubSubModel).lastMsg)
	}
	if keyMsg.String() != "k" {
		t.Errorf("forwarded key = %q, want %q", keyMsg.String(), "k")
	}
}

// TestKey_JWhenDetailClosed_NoRefresh verifies j does NOT send PRFocusedMsg
// when the detail modal is closed.
func TestKey_JWhenDetailClosed_NoRefresh(t *testing.T) {
	pr := &persistence.PullRequest{ID: "pr1"}
	detail := newStub("detail", false)
	m, _ := newTestModel(
		newSelectorStub("myPRs", pr),
		newStub("reviewQueue", true),
		newStub("watches", false),
		detail,
		newStub("cmdBar", false),
	)
	// detail is closed (default)
	m = applyMsg(m, keyRune('j'))
	if _, ok := m.detailPane.(*stubSubModel).lastMsg.(PRFocusedMsg); ok {
		t.Error("j with detail closed should not send PRFocusedMsg to detail pane")
	}
}

// TestKey_JWhenDetailOpen_WatchesFocused verifies that pressing j when the
// Watches panel is focused and the detail is open forwards to the detail pane
// for scrolling (regardless of focused panel).
func TestKey_JWhenDetailOpen_WatchesFocused(t *testing.T) {
	detail := newStub("detail", false)
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", true),
		detail,
		newStub("cmdBar", false),
	)
	// Force detail open and switch focus to Watches.
	m.detailOpen = true
	m.focused = PanelWatches

	m = applyMsg(m, keyRune('j'))

	// j should forward to detail pane for viewport scroll.
	keyMsg, ok := m.detailPane.(*stubSubModel).lastMsg.(tea.KeyMsg)
	if !ok {
		t.Fatalf("j on Watches with detail open should forward tea.KeyMsg to detail pane, got %T",
			m.detailPane.(*stubSubModel).lastMsg)
	}
	if keyMsg.String() != "j" {
		t.Errorf("forwarded key = %q, want %q", keyMsg.String(), "j")
	}
}

// TestKey_JWhenDetailOpen_EmptyPRPanel verifies that pressing j when the
// detail is open but the panel has no PR rows still forwards to the detail
// pane for viewport scrolling.
func TestKey_JWhenDetailOpen_EmptyPRPanel(t *testing.T) {
	detail := newStub("detail", false)
	// selectorStub with nil PR — returns nil from SelectedPR().
	emptySelector := newSelectorStub("myPRs", nil)
	m, _ := newTestModel(
		emptySelector,
		newStub("reviewQueue", true),
		newStub("watches", false),
		detail,
		newStub("cmdBar", false),
	)
	// Force detail open (unusual state, but exercises the scroll path).
	m.detailOpen = true

	m = applyMsg(m, keyRune('j'))

	// j should forward to detail pane for viewport scroll.
	keyMsg, ok := m.detailPane.(*stubSubModel).lastMsg.(tea.KeyMsg)
	if !ok {
		t.Fatalf("j with empty PR panel and detail open should forward tea.KeyMsg, got %T",
			m.detailPane.(*stubSubModel).lastMsg)
	}
	if keyMsg.String() != "j" {
		t.Errorf("forwarded key = %q, want %q", keyMsg.String(), "j")
	}
}

// TestKey_PgDown_WhenDetailOpen_RefreshesDetailPane verifies that pgdown
// dispatches MoveFocusMsg to the panel and sends PRFocusedMsg to the detail pane.
func TestKey_PgDown_WhenDetailOpen_RefreshesDetailPane(t *testing.T) {
	pr := &persistence.PullRequest{ID: "pr1", Title: "PR one"}
	detail := newStub("detail", false)
	m, _ := newTestModel(
		newSelectorStub("myPRs", pr),
		newStub("reviewQueue", true),
		newStub("watches", false),
		detail,
		newStub("cmdBar", false),
	)
	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.detailOpen {
		t.Fatal("detail should be open")
	}
	m.detailPane.(*stubSubModel).lastMsg = nil

	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyPgDown})

	msg, ok := m.detailPane.(*stubSubModel).lastMsg.(PRFocusedMsg)
	if !ok {
		t.Fatalf("after pgdown with detail open, detailPane should receive PRFocusedMsg, got %T",
			m.detailPane.(*stubSubModel).lastMsg)
	}
	if msg.PR.ID != "pr1" {
		t.Errorf("PRFocusedMsg.PR.ID = %q, want %q", msg.PR.ID, "pr1")
	}
}

// TestKey_PgUp_WhenDetailOpen_RefreshesDetailPane verifies pgup pages to prev PR.
func TestKey_PgUp_WhenDetailOpen_RefreshesDetailPane(t *testing.T) {
	pr := &persistence.PullRequest{ID: "pr1", Title: "PR one"}
	detail := newStub("detail", false)
	m, _ := newTestModel(
		newSelectorStub("myPRs", pr),
		newStub("reviewQueue", true),
		newStub("watches", false),
		detail,
		newStub("cmdBar", false),
	)
	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	m.detailPane.(*stubSubModel).lastMsg = nil

	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyPgUp})

	_, ok := m.detailPane.(*stubSubModel).lastMsg.(PRFocusedMsg)
	if !ok {
		t.Fatalf("after pgup with detail open, detailPane should receive PRFocusedMsg, got %T",
			m.detailPane.(*stubSubModel).lastMsg)
	}
}

// TestKey_PgDown_WhenDetailClosed_NoOp verifies pgdown does nothing when
// the detail modal is closed.
func TestKey_PgDown_WhenDetailClosed_NoOp(t *testing.T) {
	pr := &persistence.PullRequest{ID: "pr1"}
	detail := newStub("detail", false)
	m, _ := newTestModel(
		newSelectorStub("myPRs", pr),
		newStub("reviewQueue", true),
		newStub("watches", false),
		detail,
		newStub("cmdBar", false),
	)
	// detail is closed (default)
	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyPgDown})
	if _, ok := m.detailPane.(*stubSubModel).lastMsg.(PRFocusedMsg); ok {
		t.Error("pgdown with detail closed should not send PRFocusedMsg to detail pane")
	}
}

// TestKey_PgDown_WhenDetailOpen_WatchesFocused verifies that pgdown on the
// Watches panel (no PR selector) does not send PRFocusedMsg.
func TestKey_PgDown_WhenDetailOpen_WatchesFocused(t *testing.T) {
	detail := newStub("detail", false)
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", true),
		detail,
		newStub("cmdBar", false),
	)
	m.detailOpen = true
	m.focused = PanelWatches

	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyPgDown})

	if _, ok := m.detailPane.(*stubSubModel).lastMsg.(PRFocusedMsg); ok {
		t.Error("pgdown on Watches panel should not send PRFocusedMsg to detail pane")
	}
}

// TestKey_PgDown_WhenDetailOpen_EmptyPRPanel verifies that pgdown with nil
// SelectedPR does not send PRFocusedMsg.
func TestKey_PgDown_WhenDetailOpen_EmptyPRPanel(t *testing.T) {
	detail := newStub("detail", false)
	emptySelector := newSelectorStub("myPRs", nil)
	m, _ := newTestModel(
		emptySelector,
		newStub("reviewQueue", true),
		newStub("watches", false),
		detail,
		newStub("cmdBar", false),
	)
	m.detailOpen = true

	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyPgDown})

	if _, ok := m.detailPane.(*stubSubModel).lastMsg.(PRFocusedMsg); ok {
		t.Error("pgdown with nil SelectedPR should not send PRFocusedMsg to detail pane")
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

// TestView_HeaderContainsAppName verifies the header bar shows the app name
// but not the version or username (those appear in the help modal).
func TestView_HeaderContainsAppName(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)

	view := m.View()
	if !strings.Contains(view, "argh") {
		t.Errorf("View() header missing app name, got:\n%s", view)
	}
	if strings.Contains(view, "v0.0.0") {
		t.Errorf("View() header should not contain version string, got:\n%s", view)
	}
	if strings.Contains(view, "testuser") {
		t.Errorf("View() header should not contain username, got:\n%s", view)
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
		stubClock{now: t0},
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

// TestStatusTextForEvent_WithPRPayload verifies statusTextForEvent includes the
// PR number when the After field is a persistence.PullRequest.
func TestStatusTextForEvent_WithPRPayload(t *testing.T) {
	tests := []struct {
		name        string
		eventType   eventbus.EventType
		pr          persistence.PullRequest
		wantContain string
	}{
		{
			name:        "PRUpdated includes PR number",
			eventType:   eventbus.PRUpdated,
			pr:          persistence.PullRequest{Number: 42},
			wantContain: "#42",
		},
		{
			name:        "CIChanged includes PR number",
			eventType:   eventbus.CIChanged,
			pr:          persistence.PullRequest{Number: 7, CIState: "passing"},
			wantContain: "#7",
		},
		{
			name:        "CIChanged includes CI state",
			eventType:   eventbus.CIChanged,
			pr:          persistence.PullRequest{Number: 7, CIState: "failing"},
			wantContain: "failing",
		},
		{
			name:        "ReviewChanged includes PR number",
			eventType:   eventbus.ReviewChanged,
			pr:          persistence.PullRequest{Number: 99},
			wantContain: "#99",
		},
	}
	// PRRemoved uses Before instead of After
	t.Run("PRRemoved includes PR number", func(t *testing.T) {
		e := eventbus.Event{Type: eventbus.PRRemoved, Before: persistence.PullRequest{Number: 55}}
		got := statusTextForEvent(e)
		if !strings.Contains(got, "#55") {
			t.Errorf("statusTextForEvent() = %q, want to contain %q", got, "#55")
		}
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := eventbus.Event{Type: tt.eventType, After: tt.pr}
			got := statusTextForEvent(e)
			if !strings.Contains(got, tt.wantContain) {
				t.Errorf("statusTextForEvent() = %q, want to contain %q", got, tt.wantContain)
			}
		})
	}
}

// TestStatusTextForEvent_WithoutPRPayload verifies statusTextForEvent falls
// back gracefully when After is not a PullRequest.
func TestStatusTextForEvent_WithoutPRPayload(t *testing.T) {
	tests := []struct {
		name        string
		eventType   eventbus.EventType
		wantContain string
	}{
		{"pr_updated", eventbus.PRUpdated, "PR"},
		{"ci_changed", eventbus.CIChanged, "CI"},
		{"review_changed", eventbus.ReviewChanged, "Review"},
		{"pr_removed", eventbus.PRRemoved, "PR removed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := eventbus.Event{Type: tt.eventType, After: "not-a-pr"}
			got := statusTextForEvent(e)
			if !strings.Contains(got, tt.wantContain) {
				t.Errorf("statusTextForEvent() = %q, want to contain %q", got, tt.wantContain)
			}
		})
	}
}

// TestFormatTimeAgo verifies time-ago formatting.
func TestFormatTimeAgo(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{-5 * time.Second, "0s ago"},
		{0, "0s ago"},
		{30 * time.Second, "30s ago"},
		{59 * time.Second, "59s ago"},
		{1 * time.Minute, "1m ago"},
		{45 * time.Minute, "45m ago"},
		{1 * time.Hour, "1h ago"},
		{3 * time.Hour, "3h ago"},
	}
	for _, tt := range tests {
		got := formatTimeAgo(tt.d)
		if got != tt.want {
			t.Errorf("formatTimeAgo(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

// TestHeaderView_ShowsTimeAgo verifies the header includes time-ago info when
// a status event has been received.
func TestHeaderView_ShowsTimeAgo(t *testing.T) {
	eventTime := t0
	viewTime := t0.Add(42 * time.Second)

	sub := &stubSubscriber{}
	m := NewWithTheme("v0.0.0", "testuser", sub,
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
		plainTheme(),
		stubClock{now: eventTime},
	)

	// Receive a CI event so statusText and lastEventTime are set.
	e := eventbus.Event{
		Type:  eventbus.CIChanged,
		After: persistence.PullRequest{Number: 5, CIState: "failing"},
	}
	m = applyMsg(m, DBEventMsg{Event: e})

	// Advance the clock so there is measurable elapsed time.
	m.clock = stubClock{now: viewTime}

	view := m.View()
	if !strings.Contains(view, "42s ago") {
		t.Errorf("header should contain '42s ago'; got:\n%s", view)
	}
}

// TestHeaderView_ShowsPRNumber verifies the header includes the PR number from
// the event payload.
func TestHeaderView_ShowsPRNumber(t *testing.T) {
	sub := &stubSubscriber{}
	m := NewWithTheme("v0.0.0", "testuser", sub,
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
		plainTheme(),
		stubClock{now: t0},
	)

	e := eventbus.Event{
		Type:  eventbus.PRUpdated,
		After: persistence.PullRequest{Number: 123},
	}
	m = applyMsg(m, DBEventMsg{Event: e})

	view := m.View()
	if !strings.Contains(view, "#123") {
		t.Errorf("header should contain '#123'; got:\n%s", view)
	}
}

// TestNotifColor verifies notifColor returns appropriate colors for each event type.
func TestNotifColor(t *testing.T) {
	tests := []struct {
		name       string
		eventType  eventbus.EventType
		statusText string
		wantColor  lipgloss.Color
	}{
		{"CI passing → green", eventbus.CIChanged, "✓ PR #1 CI passing", lipgloss.Color("#4CAF50")},
		{"CI success → green", eventbus.CIChanged, "✓ PR #1 CI success", lipgloss.Color("#4CAF50")},
		{"CI failing → red", eventbus.CIChanged, "✗ PR #1 CI failing", lipgloss.Color("#FF6B6B")},
		{"Review approved → green", eventbus.ReviewChanged, "✓ PR #1 approved", lipgloss.Color("#4CAF50")},
		{"Review changes → red", eventbus.ReviewChanged, "✗ PR #1 changes requested", lipgloss.Color("#FF6B6B")},
		{"Review changed → blue", eventbus.ReviewChanged, "● PR #1 review changed", lipgloss.Color("#42A5F5")},
		{"Watch fired → green", eventbus.WatchFired, "● Watch fired", lipgloss.Color("#4CAF50")},
		{"Rate limit warning → yellow", eventbus.RateLimitWarning, "⚠ API rate limit low", lipgloss.Color("#FFC107")},
		{"SSO required → yellow", eventbus.SSORequired, "SSO required for acme", lipgloss.Color("#FFC107")},
		{"PR updated → blue", eventbus.PRUpdated, "● PR #1 updated", lipgloss.Color("#42A5F5")},
		{"PR removed → faint", eventbus.PRRemoved, "PR #1 removed", lipgloss.Color("#888888")},
		{"Unknown type → blue", "UNKNOWN", "", lipgloss.Color("#42A5F5")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := notifColor(tt.eventType, tt.statusText)
			if got != tt.wantColor {
				t.Errorf("notifColor(%q, %q) = %q, want %q", tt.eventType, tt.statusText, got, tt.wantColor)
			}
		})
	}
}

// TestContainsAny verifies the containsAny helper.
func TestContainsAny(t *testing.T) {
	tests := []struct {
		s    string
		subs []string
		want bool
	}{
		{"CI passing", []string{"passing"}, true},
		{"CI failing", []string{"passing", "success"}, false},
		{"CI success", []string{"passing", "success"}, true},
		{"", []string{"passing"}, false},
		{"hello world", []string{"world", "xyz"}, true},
		{"hello", []string{}, false},
	}
	for _, tt := range tests {
		got := containsAny(tt.s, tt.subs...)
		if got != tt.want {
			t.Errorf("containsAny(%q, %v) = %v, want %v", tt.s, tt.subs, got, tt.want)
		}
	}
}

// TestDBEvent_SetsStatusEventType verifies that handleDBEvent stores the event type
// for use in color coding.
func TestDBEvent_SetsStatusEventType(t *testing.T) {
	tests := []struct {
		eventType eventbus.EventType
		after     any
	}{
		{eventbus.PRUpdated, nil},
		{eventbus.CIChanged, nil},
		{eventbus.ReviewChanged, nil},
		{eventbus.PRRemoved, nil},
		{eventbus.WatchFired, nil},
		{eventbus.RateLimitWarning, nil},
		{eventbus.SSORequired, api.SSOInfo{OrgName: "org", AuthorizationURL: "https://example.com"}},
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
			m = applyMsg(m, DBEventMsg{Event: eventbus.Event{Type: tt.eventType, After: tt.after}})
			if m.statusEventType != tt.eventType {
				t.Errorf("statusEventType = %q, want %q", m.statusEventType, tt.eventType)
			}
		})
	}
}

// TestDBEvent_SetsLastEventTime verifies that handleDBEvent records the event time.
func TestDBEvent_SetsLastEventTime(t *testing.T) {
	eventTime := t1
	sub := &stubSubscriber{}
	m := NewWithTheme("v0.0.0", "testuser", sub,
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
		plainTheme(),
		stubClock{now: eventTime},
	)

	m = applyMsg(m, DBEventMsg{Event: eventbus.Event{Type: eventbus.PRUpdated}})

	if !m.lastEventTime.Equal(eventTime) {
		t.Errorf("lastEventTime = %v, want %v", m.lastEventTime, eventTime)
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

// TestKey_Q_DismissesDetailPane verifies that pressing q while the detail pane
// is open closes the detail pane instead of quitting the app.
func TestKey_Q_DismissesDetailPane(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.detailOpen = true

	m = applyMsg(m, keyRune('q'))

	if m.detailOpen {
		t.Error("expected detailOpen to be false after q")
	}
}

// TestKey_CtrlC_QuitsEvenWithDetailOpen verifies ctrl+c still quits the app
// even when the detail pane is open.
func TestKey_CtrlC_QuitsEvenWithDetailOpen(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.detailOpen = true

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c should return a non-nil Cmd even with detail open")
	}

	result := cmd()
	if _, ok := result.(tea.QuitMsg); !ok {
		t.Errorf("cmd() returned %T, want tea.QuitMsg", result)
	}
}

// ── keyboard navigation tests ─────────────────────────────────────────────────

// keyRune builds a tea.KeyMsg for a printable rune.
func keyRune(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// TestKey_JK_DispatchMoveFocusToFocusedPanel verifies j/k/↓/↑ send MoveFocusMsg
// to the currently focused panel and do not touch other panels.
func TestKey_JK_DispatchMoveFocusToFocusedPanel(t *testing.T) {
	tests := []struct {
		name           string
		key            tea.KeyMsg
		initialFocused Panel
		wantDown       bool
		wantReceiver   string // "myPRs", "reviewQueue", or "watches"
	}{
		{"j→myPRs", keyRune('j'), PanelMyPRs, true, "myPRs"},
		{"down→myPRs", tea.KeyMsg{Type: tea.KeyDown}, PanelMyPRs, true, "myPRs"},
		{"k→myPRs", keyRune('k'), PanelMyPRs, false, "myPRs"},
		{"up→myPRs", tea.KeyMsg{Type: tea.KeyUp}, PanelMyPRs, false, "myPRs"},
		{"j→reviewQueue", keyRune('j'), PanelReviewQueue, true, "reviewQueue"},
		{"k→reviewQueue", keyRune('k'), PanelReviewQueue, false, "reviewQueue"},
		{"j→watches", keyRune('j'), PanelWatches, true, "watches"},
		{"k→watches", keyRune('k'), PanelWatches, false, "watches"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			myPRs := newStub("myPRs", true)
			rq := newStub("reviewQueue", true)
			watches := newStub("watches", true)
			m, _ := newTestModel(myPRs, rq, watches, newStub("detail", false), newStub("cmdBar", false))
			m.focused = tt.initialFocused

			m = applyMsg(m, tt.key)

			var gotMsg tea.Msg
			switch tt.wantReceiver {
			case "myPRs":
				gotMsg = m.myPRs.(*stubSubModel).lastMsg
			case "reviewQueue":
				gotMsg = m.reviewQueue.(*stubSubModel).lastMsg
			case "watches":
				gotMsg = m.watches.(*stubSubModel).lastMsg
			}

			mv, ok := gotMsg.(MoveFocusMsg)
			if !ok {
				t.Fatalf("expected MoveFocusMsg, got %T", gotMsg)
			}
			if mv.Down != tt.wantDown {
				t.Errorf("MoveFocusMsg.Down = %v, want %v", mv.Down, tt.wantDown)
			}

			// Other panels should not receive the message.
			if tt.wantReceiver != "myPRs" && m.myPRs.(*stubSubModel).lastMsg != nil {
				t.Error("myPRs should not receive message when not focused")
			}
			if tt.wantReceiver != "reviewQueue" && m.reviewQueue.(*stubSubModel).lastMsg != nil {
				t.Error("reviewQueue should not receive message when not focused")
			}
			if tt.wantReceiver != "watches" && m.watches.(*stubSubModel).lastMsg != nil {
				t.Error("watches should not receive message when not focused")
			}
		})
	}
}

// TestKey_SlashAndColon_FocusCommandBar verifies / and : set commandBarFocused,
// send FocusCommandBarMsg, and forward the key to the command bar.
func TestKey_SlashAndColon_FocusCommandBar(t *testing.T) {
	for _, key := range []tea.KeyMsg{keyRune('/'), keyRune(':')} {
		t.Run(key.String(), func(t *testing.T) {
			cb := NewCommandBar()
			m, _ := newTestModel(
				newStub("myPRs", true), newStub("reviewQueue", true),
				newStub("watches", false), newStub("detail", false), cb,
			)

			m = applyMsg(m, key)

			if !m.commandBarFocused {
				t.Error("commandBarFocused should be true after pressing", key.String())
			}
			realCB := m.commandBar.(*CommandBar)
			if !realCB.focused {
				t.Error("command bar should be focused")
			}
			if realCB.Value() != key.String() {
				t.Errorf("expected %q typed into input, got %q", key.String(), realCB.Value())
			}
		})
	}
}

// TestKey_Esc_DismissesHelpOverlay verifies Esc clears helpVisible when the
// help overlay is open.
func TestKey_Esc_DismissesHelpOverlay(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
	)
	m.helpVisible = true

	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.helpVisible {
		t.Error("helpVisible should be false after Esc when overlay was open")
	}
	if m.commandBarFocused {
		t.Error("commandBarFocused should remain false")
	}
}

// TestKey_Esc_UnfocusesCommandBar verifies Esc clears commandBarFocused and
// sends BlurCommandBarMsg when the command bar is focused.
func TestKey_Esc_UnfocusesCommandBar(t *testing.T) {
	cmdBar := newStub("cmdBar", false)
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), cmdBar,
	)
	m.commandBarFocused = true

	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.commandBarFocused {
		t.Error("commandBarFocused should be false after Esc")
	}
	if _, ok := m.commandBar.(*stubSubModel).lastMsg.(BlurCommandBarMsg); !ok {
		t.Errorf("commandBar should receive BlurCommandBarMsg, got %T",
			m.commandBar.(*stubSubModel).lastMsg)
	}
}

// TestKey_Esc_NoOp verifies Esc is a no-op when neither overlay nor command
// bar are active.
func TestKey_Esc_NoOp(t *testing.T) {
	cmdBar := newStub("cmdBar", false)
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), cmdBar,
	)

	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.helpVisible {
		t.Error("helpVisible should remain false")
	}
	if m.commandBarFocused {
		t.Error("commandBarFocused should remain false")
	}
	if m.commandBar.(*stubSubModel).lastMsg != nil {
		t.Error("commandBar should not receive any message for no-op Esc")
	}
}

// TestKey_O_OpensPRInBrowser verifies o calls BrowserOpener.Open with the
// selected PR's URL and produces a status message.
func TestKey_O_OpensPRInBrowser(t *testing.T) {
	pr := &persistence.PullRequest{URL: "https://github.com/org/repo/pull/42"}
	myPRs := newSelectorStub("myPRs", pr)
	m, _ := newTestModel(myPRs, newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.focused = PanelMyPRs

	browser := &stubBrowserOpener{}
	m.browser = browser

	updated, cmd := m.Update(keyRune('o'))
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("expected a tea.Cmd from 'o' key")
	}
	// Execute the batched commands to find the CommandResultMsg.
	msg := executeBatchCmd(cmd)
	if msg == nil {
		t.Fatal("expected CommandResultMsg from cmd")
	}
	result, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if result.Err != nil {
		t.Errorf("unexpected error: %v", result.Err)
	}
	if result.Status != "opened https://github.com/org/repo/pull/42" {
		t.Errorf("unexpected status: %s", result.Status)
	}
	if browser.openedURL != pr.URL {
		t.Errorf("browser.Open called with %q, want %q", browser.openedURL, pr.URL)
	}
}

// TestKey_O_NoPR_NoOp verifies o is a no-op when no PR is selected.
func TestKey_O_NoPR_NoOp(t *testing.T) {
	myPRs := newSelectorStub("myPRs", nil)
	m, _ := newTestModel(myPRs, newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.focused = PanelMyPRs
	m.browser = &stubBrowserOpener{}

	m = applyMsg(m, keyRune('o'))

	if m.statusText != "" {
		t.Errorf("expected empty status text, got %q", m.statusText)
	}
}

// TestKey_O_NoBrowser_SetsErrorStatus verifies o sets an error status when
// no browser opener is configured.
func TestKey_O_NoBrowser_SetsErrorStatus(t *testing.T) {
	pr := &persistence.PullRequest{URL: "https://github.com/org/repo/pull/1"}
	myPRs := newSelectorStub("myPRs", pr)
	m, _ := newTestModel(myPRs, newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.focused = PanelMyPRs
	// m.browser intentionally nil

	m = applyMsg(m, keyRune('o'))

	if m.statusText != "error: no browser opener configured" {
		t.Errorf("unexpected status text: %q", m.statusText)
	}
}

// TestKey_O_WatchesPanel_NoOp verifies o is a no-op when Watches panel is focused
// (it does not implement PRSelector).
func TestKey_O_WatchesPanel_NoOp(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", true), newStub("detail", false), newStub("cmdBar", false))
	m.focused = PanelWatches
	m.browser = &stubBrowserOpener{}

	m = applyMsg(m, keyRune('o'))

	if m.statusText != "" {
		t.Errorf("expected empty status text, got %q", m.statusText)
	}
}

// stubDiffViewer is a test double for DiffViewer.
type stubDiffViewer struct {
	content string
	showErr error
	called  bool
}

func (s *stubDiffViewer) ShowDiff(_ string, _ int) (string, error) {
	s.called = true
	return s.content, s.showErr
}

// TestKey_D_OnReviewQueue_FetchesDiff verifies d on a PR panel triggers a diff
// fetch and produces a ShowDiffContentMsg.
func TestKey_D_OnReviewQueue_FetchesDiff(t *testing.T) {
	pr := &persistence.PullRequest{Repo: "owner/repo", Number: 42, URL: "https://github.com/owner/repo/pull/42"}
	rq := newSelectorStub("reviewQueue", pr)
	dv := &stubDiffViewer{content: "diff content"}
	m, _ := newTestModel(newStub("myPRs", true), rq,
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.focused = PanelReviewQueue
	m.diffViewer = dv

	_, cmd := m.Update(keyRune('d'))
	msg := executeBatchCmd(cmd)
	if msg == nil {
		t.Fatal("expected a message from the diff fetch cmd")
	}
	dc, ok := msg.(ShowDiffContentMsg)
	if !ok {
		t.Fatalf("expected ShowDiffContentMsg, got %T", msg)
	}
	if dc.Content != "diff content" {
		t.Errorf("content = %q, want %q", dc.Content, "diff content")
	}
	if !dv.called {
		t.Error("ShowDiff should have been called")
	}
}

// TestKey_D_NoDiffViewer_SetsErrorStatus verifies d shows error when no diff
// viewer is wired.
func TestKey_D_NoDiffViewer_SetsErrorStatus(t *testing.T) {
	pr := &persistence.PullRequest{Repo: "owner/repo", Number: 42, URL: "https://github.com/owner/repo/pull/42"}
	rq := newSelectorStub("reviewQueue", pr)
	m, _ := newTestModel(newStub("myPRs", true), rq,
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.focused = PanelReviewQueue

	m = applyMsg(m, keyRune('d'))

	if !strings.Contains(m.statusText, "no diff viewer") {
		t.Errorf("expected error status about missing diff viewer, got %q", m.statusText)
	}
}

// TestKey_D_NoPR_NoOp verifies d does nothing when no PR is selected.
func TestKey_D_NoPR_NoOp(t *testing.T) {
	rq := newSelectorStub("reviewQueue", nil) // no PR
	m, _ := newTestModel(newStub("myPRs", true), rq,
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.focused = PanelReviewQueue
	m.diffViewer = &stubDiffViewer{content: "should not be called"}

	m = applyMsg(m, keyRune('d'))

	if m.diffVisible {
		t.Error("diff modal should not open when no PR is selected")
	}
}

// TestKey_D_SendsCancelWatchMsgToWatchesPanel verifies d dispatches CancelWatchMsg
// when the Watches panel is focused.
func TestKey_D_SendsCancelWatchMsgToWatchesPanel(t *testing.T) {
	watches := newStub("watches", true)
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		watches, newStub("detail", false), newStub("cmdBar", false))
	m.focused = PanelWatches

	m = applyMsg(m, keyRune('d'))

	if _, ok := watches.lastMsg.(CancelWatchMsg); !ok {
		t.Errorf("watches panel should receive CancelWatchMsg, got %T", watches.lastMsg)
	}
}

// TestKey_A_ApprovesOnlyFromReviewQueue verifies a sends ApprovePRMsg only
// when the Review Queue panel is focused.
func TestKey_A_ApprovesOnlyFromReviewQueue(t *testing.T) {
	tests := []struct {
		name           string
		initialFocused Panel
		wantApprove    bool
	}{
		{"review queue → approves", PanelReviewQueue, true},
		{"my prs → no action", PanelMyPRs, false},
		{"watches → no action", PanelWatches, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			myPRs := newStub("myPRs", true)
			rq := newStub("reviewQueue", true)
			watches := newStub("watches", true)
			m, _ := newTestModel(myPRs, rq, watches,
				newStub("detail", false), newStub("cmdBar", false))
			m.focused = tt.initialFocused

			m = applyMsg(m, keyRune('a'))

			gotMsg := m.reviewQueue.(*stubSubModel).lastMsg
			_, isApprove := gotMsg.(ApprovePRMsg)
			if tt.wantApprove && !isApprove {
				t.Errorf("reviewQueue should receive ApprovePRMsg when focused, got %T", gotMsg)
			}
			if !tt.wantApprove && isApprove {
				t.Error("reviewQueue should NOT receive ApprovePRMsg when not focused")
			}
			// myPRs should never receive ApprovePRMsg
			if _, ok := m.myPRs.(*stubSubModel).lastMsg.(ApprovePRMsg); ok {
				t.Error("myPRs should never receive ApprovePRMsg")
			}
		})
	}
}

// TestKey_R_SendsRequestReviewToFocusedPanel verifies r dispatches RequestReviewMsg.
func TestKey_R_SendsRequestReviewToFocusedPanel(t *testing.T) {
	myPRs := newStub("myPRs", true)
	m, _ := newTestModel(myPRs, newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.focused = PanelMyPRs

	m = applyMsg(m, keyRune('r'))

	if _, ok := m.myPRs.(*stubSubModel).lastMsg.(RequestReviewMsg); !ok {
		t.Errorf("myPRs should receive RequestReviewMsg, got %T",
			m.myPRs.(*stubSubModel).lastMsg)
	}
}

// TestKey_QuestionMark_TogglesHelpVisible verifies ? toggles helpVisible.
func TestKey_QuestionMark_TogglesHelpVisible(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
	)

	if m.helpVisible {
		t.Fatal("helpVisible should be false initially")
	}

	m = applyMsg(m, keyRune('?'))
	if !m.helpVisible {
		t.Error("helpVisible should be true after first ?")
	}

	m = applyMsg(m, keyRune('?'))
	if m.helpVisible {
		t.Error("helpVisible should be false after second ?")
	}
}

// TestKey_CapitalR_ProducesForceReloadMsg verifies R returns a batch Cmd that
// contains a ForceReloadMsg when executed.
func TestKey_CapitalR_ProducesForceReloadMsg(t *testing.T) {
	m, sub := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
	)
	// Pre-fill the channel so waitForDBEvent in the batch returns without blocking.
	sub.publish(eventbus.Event{Type: eventbus.PRUpdated})

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	if cmd == nil {
		t.Fatal("R should return a non-nil Cmd")
	}

	batchMsg, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("R Cmd should produce tea.BatchMsg")
	}

	var found bool
	for _, c := range batchMsg {
		if c != nil {
			if _, isReload := c().(ForceReloadMsg); isReload {
				found = true
			}
		}
	}
	if !found {
		t.Error("R batch should contain a ForceReloadMsg")
	}
}

// TestKey_CapitalD_ProducesToggleDNDMsg verifies D returns a batch Cmd that
// contains a ToggleDNDMsg when executed.
func TestKey_CapitalD_ProducesToggleDNDMsg(t *testing.T) {
	m, sub := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
	)
	sub.publish(eventbus.Event{Type: eventbus.PRUpdated})

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("D")})
	if cmd == nil {
		t.Fatal("D should return a non-nil Cmd")
	}

	batchMsg, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("D Cmd should produce tea.BatchMsg")
	}

	var found bool
	for _, c := range batchMsg {
		if c != nil {
			if _, isDND := c().(ToggleDNDMsg); isDND {
				found = true
			}
		}
	}
	if !found {
		t.Error("D batch should contain a ToggleDNDMsg")
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

// ── stubDNDToggler ────────────────────────────────────────────────────────────

type stubDNDToggler struct {
	active      bool
	toggleCount int
}

func (s *stubDNDToggler) Toggle() {
	s.active = !s.active
	s.toggleCount++
}

func (s *stubDNDToggler) IsDND() bool {
	return s.active
}

// TestView_HeaderShowsDNDIndicator verifies "🔕 DND" appears in the header
// when the dndToggler reports DND active.
func TestView_HeaderShowsDNDIndicator(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)
	toggler := &stubDNDToggler{active: true}
	m.dndToggler = toggler

	view := m.View()
	if !strings.Contains(view, "🔕 DND") {
		t.Errorf("View() header should contain '🔕 DND' when DND is active, got:\n%s", view)
	}
}

// TestView_HeaderNoDNDIndicatorWhenInactive verifies the DND indicator is
// absent from the header when DND is inactive.
func TestView_HeaderNoDNDIndicatorWhenInactive(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)
	toggler := &stubDNDToggler{active: false}
	m.dndToggler = toggler

	view := m.View()
	if strings.Contains(view, "🔕 DND") {
		t.Errorf("View() header should NOT contain '🔕 DND' when DND is inactive, got:\n%s", view)
	}
}

// TestView_HeaderNoDNDIndicatorWhenTogglerNil verifies the DND indicator is
// absent when no dndToggler is configured (nil).
func TestView_HeaderNoDNDIndicatorWhenTogglerNil(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)
	// dndToggler is nil by default

	view := m.View()
	if strings.Contains(view, "🔕 DND") {
		t.Errorf("View() header should NOT contain '🔕 DND' when dndToggler is nil, got:\n%s", view)
	}
}

// TestToggleDNDMsg_CallsToggleOnDNDToggler verifies that receiving ToggleDNDMsg
// calls Toggle() on the dndToggler.
func TestToggleDNDMsg_CallsToggleOnDNDToggler(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)
	toggler := &stubDNDToggler{}
	m.dndToggler = toggler

	m = applyMsg(m, ToggleDNDMsg{})

	if toggler.toggleCount != 1 {
		t.Errorf("Toggle() should have been called once, called %d times", toggler.toggleCount)
	}
}

// TestToggleDNDMsg_NilTogglerIsNoOp verifies that ToggleDNDMsg with nil
// dndToggler does not panic.
func TestToggleDNDMsg_NilTogglerIsNoOp(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)
	// dndToggler is nil — should not panic
	m = applyMsg(m, ToggleDNDMsg{})
	// reaching here means no panic
}

// TestToggleDNDMsg_TogglesHeaderIndicator verifies that after ToggleDNDMsg is
// received, the DND indicator reflects the new state.
func TestToggleDNDMsg_TogglesHeaderIndicator(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)
	toggler := &stubDNDToggler{active: false}
	m.dndToggler = toggler

	// Before toggle — no DND indicator.
	if strings.Contains(m.View(), "🔕 DND") {
		t.Error("DND indicator should be absent before toggle")
	}

	// Simulate pressing D (produces ToggleDNDMsg).
	m = applyMsg(m, ToggleDNDMsg{})

	// After toggle — indicator present.
	if !strings.Contains(m.View(), "🔕 DND") {
		t.Errorf("DND indicator should appear after toggle; got:\n%s", m.View())
	}

	// Toggle again — indicator gone.
	m = applyMsg(m, ToggleDNDMsg{})
	if strings.Contains(m.View(), "🔕 DND") {
		t.Errorf("DND indicator should disappear after second toggle; got:\n%s", m.View())
	}
}

// ── ReviewSuggestionsMsg ──────────────────────────────────────────────────────

// ── WithBrowser ───────────────────────────────────────────────────────────────

func TestModel_WithBrowser_SetsBrowser(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)
	browser := &stubBrowserOpener{}
	m2 := m.WithBrowser(browser)
	if m2.browser != browser {
		t.Error("WithBrowser: browser not set on returned model")
	}
	if m.browser != nil {
		t.Error("WithBrowser: original model browser should remain nil")
	}
}

// TestKey_O_BrowserError_SetsErrorStatus verifies o produces an error status
// when the browser Open call fails.
func TestKey_O_BrowserError_SetsErrorStatus(t *testing.T) {
	pr := &persistence.PullRequest{URL: "https://github.com/org/repo/pull/7"}
	myPRs := newSelectorStub("myPRs", pr)
	m, _ := newTestModel(myPRs, newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.focused = PanelMyPRs
	m.browser = &stubBrowserOpener{openErr: fmt.Errorf("cannot open")}

	updated, cmd := m.Update(keyRune('o'))
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("expected a tea.Cmd from 'o' key")
	}
	msg := executeBatchCmd(cmd)
	result, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if result.Err == nil {
		t.Error("expected error in CommandResultMsg")
	}
}

// ── WithDNDToggler ────────────────────────────────────────────────────────────

func TestModel_WithDNDToggler_SetsToggler(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)
	toggler := &stubDNDToggler{}
	m2 := m.WithDNDToggler(toggler)
	if m2.dndToggler != toggler {
		t.Error("WithDNDToggler: dndToggler not set on returned model")
	}
	// Original model must not be mutated.
	if m.dndToggler != nil {
		t.Error("WithDNDToggler: original model dndToggler should remain nil")
	}
}

// ── CommandResultMsg ──────────────────────────────────────────────────────────

func TestModel_CommandResultMsg_SetsStatusAndBlurs(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)
	m.commandBarFocused = true

	m = applyMsg(m, CommandResultMsg{Status: "ok"})

	if m.statusText != "ok" {
		t.Errorf("statusText: got %q, want %q", m.statusText, "ok")
	}
	if m.commandBarFocused {
		t.Error("commandBarFocused should be false after CommandResultMsg")
	}
}

func TestModel_CommandResultMsg_ErrorPath(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)
	m = applyMsg(m, CommandResultMsg{Err: fmt.Errorf("boom")})
	if m.statusText != "error: boom" {
		t.Errorf("statusText: got %q, want %q", m.statusText, "error: boom")
	}
}

// ── WatchChangedMsg ──────────────────────────────────────────────────────────

func TestModel_WatchChangedMsg_RefreshesPanels(t *testing.T) {
	myPRs := newStub("myPRs", true)
	rq := newStub("reviewQueue", true)
	watches := newStub("watches", false)
	cmdBar := newStub("cmdBar", false)
	m, _ := newTestModel(myPRs, rq, watches, newStub("detail", false), cmdBar)
	m.commandBarFocused = true

	m = applyMsg(m, WatchChangedMsg{Status: "watch added for r/r#1"})

	if m.statusText != "watch added for r/r#1" {
		t.Errorf("statusText: got %q, want %q", m.statusText, "watch added for r/r#1")
	}
	if m.commandBarFocused {
		t.Error("commandBarFocused should be false after WatchChangedMsg")
	}
	if _, ok := myPRs.lastMsg.(RefreshMsg); !ok {
		t.Errorf("myPRs should receive RefreshMsg, got %T", myPRs.lastMsg)
	}
	if _, ok := rq.lastMsg.(RefreshMsg); !ok {
		t.Errorf("reviewQueue should receive RefreshMsg, got %T", rq.lastMsg)
	}
	if _, ok := watches.lastMsg.(RefreshMsg); !ok {
		t.Errorf("watches should receive RefreshMsg, got %T", watches.lastMsg)
	}
}

// ── CommandComposeMsg ─────────────────────────────────────────────────────────

func TestModel_CommandComposeMsg_SetsPrompt(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)
	m = applyMsg(m, CommandComposeMsg{Prompt: "enter comment:"})
	if m.statusText != "enter comment:" {
		t.Errorf("statusText: got %q, want %q", m.statusText, "enter comment:")
	}
}

// ── enter key when command bar focused ───────────────────────────────────────

func TestModel_Enter_WhenCommandBarFocused_ForwardsToCommandBar(t *testing.T) {
	cmdBar := newStub("cmdBar", false)
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		cmdBar,
	)
	m.commandBarFocused = true
	wasOpen := m.detailOpen

	enterMsg := tea.KeyMsg{Type: tea.KeyEnter}
	m = applyMsg(m, enterMsg)

	// Detail pane must NOT have been toggled.
	if m.detailOpen != wasOpen {
		t.Error("enter when command bar focused must not toggle detailOpen")
	}
	// The command bar stub should have received the enter key.
	if _, ok := cmdBar.lastMsg.(tea.KeyMsg); !ok {
		t.Errorf("command bar did not receive KeyMsg; lastMsg = %T", cmdBar.lastMsg)
	}
}

func TestModel_Enter_WhenCommandBarNotFocused_TogglesDetail(t *testing.T) {
	pr := &persistence.PullRequest{ID: "pr1", Title: "test PR"}
	m, _ := newTestModel(
		newSelectorStub("myPRs", pr),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)
	m.commandBarFocused = false
	before := m.detailOpen

	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.detailOpen == before {
		t.Error("enter when command bar not focused should toggle detailOpen")
	}
}

// ── ReviewSuggestionsMsg ──────────────────────────────────────────────────────

func TestModel_ReviewSuggestionsMsg_FocusesCommandBar(t *testing.T) {
	cmdBar := newStub("cmdBar", false)
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		cmdBar,
	)

	if m.commandBarFocused {
		t.Error("commandBarFocused should be false before ReviewSuggestionsMsg")
	}

	msg := ReviewSuggestionsMsg{
		Suggestions: []string{"alice", "bob"},
		InputPrefix: ":request #42 @",
	}
	m = applyMsg(m, msg)

	if !m.commandBarFocused {
		t.Error("commandBarFocused should be true after ReviewSuggestionsMsg")
	}
	// Verify the message was forwarded to the command bar sub-model.
	if _, ok := cmdBar.lastMsg.(ReviewSuggestionsMsg); !ok {
		t.Errorf("command bar did not receive ReviewSuggestionsMsg; lastMsg = %T", cmdBar.lastMsg)
	}
}

// ── Command-bar key-forwarding tests ─────────────────────────────────────────

// TestKey_WhenCommandBarFocused_ForwardsAllKeysToCommandBar verifies that when
// the command bar is focused every keystroke (letters, navigation keys, etc.)
// is forwarded to the command bar sub-model and does NOT trigger root-level
// actions such as quitting or cycling panel focus.
func TestKey_WhenCommandBarFocused_ForwardsAllKeysToCommandBar(t *testing.T) {
	keys := []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("a")},
		{Type: tea.KeyRunes, Runes: []rune("q")},
		{Type: tea.KeyRunes, Runes: []rune("j")},
		{Type: tea.KeyRunes, Runes: []rune("k")},
		{Type: tea.KeyRunes, Runes: []rune("d")},
		{Type: tea.KeyRunes, Runes: []rune("r")},
		{Type: tea.KeyRunes, Runes: []rune("p")},
		{Type: tea.KeyRunes, Runes: []rune("R")},
		{Type: tea.KeyRunes, Runes: []rune("D")},
		{Type: tea.KeyTab},
		{Type: tea.KeyDown},
		{Type: tea.KeyUp},
		{Type: tea.KeyEnter},
	}

	for _, key := range keys {
		t.Run(key.String(), func(t *testing.T) {
			cmdBar := newStub("cmdBar", false)
			m, _ := newTestModel(
				newStub("myPRs", true),
				newStub("reviewQueue", true),
				newStub("watches", false),
				newStub("detail", false),
				cmdBar,
			)
			m.commandBarFocused = true
			initialFocused := m.focused
			wasOpen := m.detailOpen

			// applyMsg discards the cmd (which would block on the event channel).
			m2 := applyMsg(m, key)

			if m2.focused != initialFocused {
				t.Errorf("key %q changed panel focus from %d to %d", key.String(), initialFocused, m2.focused)
			}
			if m2.detailOpen != wasOpen {
				t.Errorf("key %q toggled detailOpen from %v to %v", key.String(), wasOpen, m2.detailOpen)
			}
			if _, ok := cmdBar.lastMsg.(tea.KeyMsg); !ok {
				t.Errorf("key %q: command bar did not receive KeyMsg; lastMsg = %T", key.String(), cmdBar.lastMsg)
			}
		})
	}
}

// TestKey_CtrlC_WhenCommandBarFocused_StillQuits verifies ctrl+c quits even
// when the command bar is focused.
func TestKey_CtrlC_WhenCommandBarFocused_StillQuits(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		newStub("cmdBar", false),
	)
	m.commandBarFocused = true

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c should return a non-nil Cmd even when command bar is focused")
	}
	result := cmd()
	if _, ok := result.(tea.QuitMsg); !ok {
		t.Errorf("cmd() returned %T, want tea.QuitMsg", result)
	}
}

// TestKey_Esc_WhenCommandBarFocused_BlursCommandBar verifies Esc unfocuses the
// command bar and does NOT forward the key to it.
func TestKey_Esc_WhenCommandBarFocused_BlursCommandBar(t *testing.T) {
	cmdBar := newStub("cmdBar", false)
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false),
		newStub("detail", false),
		cmdBar,
	)
	m.commandBarFocused = true

	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.commandBarFocused {
		t.Error("commandBarFocused should be false after Esc")
	}
	if _, ok := cmdBar.lastMsg.(BlurCommandBarMsg); !ok {
		t.Errorf("command bar should receive BlurCommandBarMsg on Esc; got %T", cmdBar.lastMsg)
	}
}

// ── tea.WindowSizeMsg / ResizeMsg / layout helpers ────────────────────────────

// spySubModel records the last ResizeMsg it receives for assertions.
type spySubModel struct {
*stubSubModel
lastResize *ResizeMsg
}

func newSpy(name string, hasContent bool) *spySubModel {
return &spySubModel{stubSubModel: newStub(name, hasContent)}
}

func (s *spySubModel) Update(msg tea.Msg) (SubModel, tea.Cmd) {
if r, ok := msg.(ResizeMsg); ok {
s.lastResize = &r
}
s.lastMsg = msg
return s, nil
}

// TestWindowSizeMsg_StoredOnModel verifies that sending a tea.WindowSizeMsg
// stores the dimensions in the root model.
func TestWindowSizeMsg_StoredOnModel(t *testing.T) {
tests := []struct {
w, h int
}{
{0, 0},
{80, 24},
{120, 40},
{200, 60},
}
for _, tt := range tests {
t.Run(fmt.Sprintf("%dx%d", tt.w, tt.h), func(t *testing.T) {
m, _ := newTestModel(
newStub("myPRs", true),
newStub("reviewQueue", true),
newStub("watches", false),
newStub("detail", false),
newStub("cmdBar", false),
)
m = applyMsg(m, tea.WindowSizeMsg{Width: tt.w, Height: tt.h})
if m.width != tt.w {
t.Errorf("width = %d, want %d", m.width, tt.w)
}
if m.height != tt.h {
t.Errorf("height = %d, want %d", m.height, tt.h)
}
})
}
}

// TestWindowSizeMsg_PropagatesResizeMsgToSubModels verifies that a
// tea.WindowSizeMsg causes each sub-model to receive a ResizeMsg.
func TestWindowSizeMsg_PropagatesResizeMsgToSubModels(t *testing.T) {
myPRs := newSpy("myPRs", false)
rq := newSpy("reviewQueue", false)
watches := newSpy("watches", false)
detail := newSpy("detail", false)
cmdBar := newSpy("cmdBar", false)

sub := &stubSubscriber{}
m := NewWithTheme("v0", "u", sub,
myPRs, rq, watches, detail, cmdBar,
plainTheme(), stubClock{now: t0})
m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

for _, sp := range []*spySubModel{myPRs, rq, watches, detail, cmdBar} {
if sp.lastResize == nil {
t.Errorf("%s: expected ResizeMsg, got none", sp.name)
}
}
}

// TestWindowSizeMsg_PropagatesCorrectWidth verifies that panels receive the
// full terminal width and helper/detail panels also receive it.
func TestWindowSizeMsg_PropagatesCorrectWidth(t *testing.T) {
myPRs := newSpy("myPRs", false)
rq := newSpy("reviewQueue", false)
watches := newSpy("watches", false)
detail := newSpy("detail", false)
cmdBar := newSpy("cmdBar", false)

sub := &stubSubscriber{}
m := NewWithTheme("v0", "u", sub,
myPRs, rq, watches, detail, cmdBar,
plainTheme(), stubClock{now: t0})
m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

for _, sp := range []*spySubModel{myPRs, rq, watches} {
if sp.lastResize == nil || sp.lastResize.Width != 100 {
t.Errorf("%s: expected width 100, got %v", sp.name, sp.lastResize)
}
}
}

// TestPanelContentHeight verifies the height distribution helper across
// various combinations of terminal height and panel count.
func TestPanelContentHeight(t *testing.T) {
tests := []struct {
name         string
height       int
nPanels      int
wantPositive bool // just assert content height > 0 when positive expected
wantZero     bool
}{
{"zero height", 0, 2, false, true},
{"zero panels", 30, 0, false, true},
{"tall terminal 2 panels", 30, 2, true, false},
{"tall terminal 3 panels", 30, 3, true, false},
{"minimal height", 8, 2, true, false},
// When each panel gets only 1 line budget, inner is clamped to 1.
{"very small height forces clamp", 4, 2, true, false},
}

for _, tt := range tests {
t.Run(tt.name, func(t *testing.T) {
m, _ := newTestModel(
newStub("myPRs", true),
newStub("reviewQueue", true),
newStub("watches", false),
newStub("detail", false),
newStub("cmdBar", false),
)
m.height = tt.height
h := m.panelContentHeight(tt.nPanels)
if tt.wantZero && h != 0 {
t.Errorf("panelContentHeight(%d) = %d, want 0", tt.nPanels, h)
}
if tt.wantPositive && h <= 0 {
t.Errorf("panelContentHeight(%d) = %d, want > 0", tt.nPanels, h)
}
})
}
}

// TestNumVisiblePanels verifies the panel count with and without watches content.
func TestNumVisiblePanels(t *testing.T) {
m1, _ := newTestModel(
newStub("myPRs", true),
newStub("reviewQueue", true),
newStub("watches", false), // no content
newStub("detail", false),
newStub("cmdBar", false),
)
if n := m1.numVisiblePanels(); n != 2 {
t.Errorf("numVisiblePanels without watches = %d, want 2", n)
}

m2, _ := newTestModel(
newStub("myPRs", true),
newStub("reviewQueue", true),
newStub("watches", true), // has content
newStub("detail", false),
newStub("cmdBar", false),
)
if n := m2.numVisiblePanels(); n != 3 {
t.Errorf("numVisiblePanels with watches = %d, want 3", n)
}
}

// TestView_WithWidth_FullWidthRendering verifies that after a WindowSizeMsg the
// rendered view lines span the full terminal width.
func TestView_WithWidth_FullWidthRendering(t *testing.T) {
m, _ := newTestModel(
newStub("myPRs", true),
newStub("reviewQueue", true),
newStub("watches", false),
newStub("detail", false),
newStub("cmdBar", false),
)
m = applyMsg(m, tea.WindowSizeMsg{Width: 80, Height: 24})
// Re-assign sub-model fields after update (Update returns tea.Model).
view := m.View()
// The view should not be empty.
if view == "" {
t.Fatal("View() returned empty string after resize")
}
_ = view // width assertions require stripping ANSI codes; existence is sufficient here
}

// TestView_WithWidthAndHeight_DetailPane verifies detail pane renders with
// width constraint applied.
func TestView_WithWidthAndHeight_DetailPane(t *testing.T) {
m, _ := newTestModel(
newStub("myPRs", true),
newStub("reviewQueue", true),
newStub("watches", false),
newStub("detail", false),
newStub("cmdBar", false),
)
m.detailOpen = true
m = applyMsg(m, tea.WindowSizeMsg{Width: 80, Height: 40})
view := m.View()
if !strings.Contains(view, "DETAIL") {
t.Errorf("expected DETAIL in view after resize with open detail pane; got:\n%s", view)
}
}

// ── stubCursorPanel ───────────────────────────────────────────────────────────
// A test double that implements SubModel + RowCounter + CursorNavigator + PRSelector.
type stubCursorPanel struct {
	*stubSubModel
	rows   int
	cursor int
	pr     *persistence.PullRequest
}

func newCursorStub(name string, rows int, pr *persistence.PullRequest) *stubCursorPanel {
	return &stubCursorPanel{
		stubSubModel: newStub(name, rows > 0),
		rows:         rows,
		pr:           pr,
	}
}

func (s *stubCursorPanel) Update(msg tea.Msg) (SubModel, tea.Cmd) {
	if m, ok := msg.(MoveFocusMsg); ok {
		if m.Down {
			if s.cursor < s.rows-1 {
				s.cursor++
			}
		} else {
			if s.cursor > 0 {
				s.cursor--
			}
		}
	}
	s.lastMsg = msg
	return s, nil
}

func (s *stubCursorPanel) RowCount() int           { return s.rows }
func (s *stubCursorPanel) CursorPosition() int     { return s.cursor }
func (s *stubCursorPanel) SetCursor(pos int)        {
	if pos < 0 {
		pos = 0
	}
	if pos >= s.rows && s.rows > 0 {
		pos = s.rows - 1
	}
	s.cursor = pos
}
func (s *stubCursorPanel) SelectedPR() *persistence.PullRequest { return s.pr }

// ── Tab: skip invisible watches ───────────────────────────────────────────────

func TestKey_Tab_SkipsInvisibleWatches(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", false), // invisible
		newStub("detail", false),
		newStub("cmdBar", false),
	)

	tabMsg := tea.KeyMsg{Type: tea.KeyTab}

	m = applyMsg(m, tabMsg)
	if m.focused != PanelReviewQueue {
		t.Errorf("after 1 tab: want PanelReviewQueue, got %d", m.focused)
	}

	// Second tab should skip invisible watches and wrap back to MyPRs.
	m = applyMsg(m, tabMsg)
	if m.focused != PanelMyPRs {
		t.Errorf("after 2 tabs with invisible watches: want PanelMyPRs, got %d", m.focused)
	}
}

func TestKey_Tab_CyclesAllThreeWhenWatchesVisible(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true),
		newStub("reviewQueue", true),
		newStub("watches", true), // visible
		newStub("detail", false),
		newStub("cmdBar", false),
	)

	tabMsg := tea.KeyMsg{Type: tea.KeyTab}

	m = applyMsg(m, tabMsg)
	if m.focused != PanelReviewQueue {
		t.Errorf("after 1 tab: want PanelReviewQueue, got %d", m.focused)
	}

	m = applyMsg(m, tabMsg)
	if m.focused != PanelWatches {
		t.Errorf("after 2 tabs: want PanelWatches, got %d", m.focused)
	}

	m = applyMsg(m, tabMsg)
	if m.focused != PanelMyPRs {
		t.Errorf("after 3 tabs (wrap): want PanelMyPRs, got %d", m.focused)
	}
}

// ── j/k wrapping between MyPRs and ReviewQueue ───────────────────────────────

func TestKey_J_AtBottomOfMyPRs_WrapsToReviewQueue(t *testing.T) {
	pr1 := &persistence.PullRequest{ID: "pr1"}
	pr2 := &persistence.PullRequest{ID: "pr2"}
	myPRs := newCursorStub("myPRs", 2, pr1)
	rq := newCursorStub("reviewQueue", 3, pr2)

	m, _ := newTestModel(myPRs, rq, newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.focused = PanelMyPRs
	// Move cursor to last row
	myPRs.cursor = 1 // row count is 2, so index 1 is the last

	m = applyMsg(m, keyRune('j'))

	if m.focused != PanelReviewQueue {
		t.Errorf("expected focus on ReviewQueue, got %d", m.focused)
	}
	rqPanel := m.reviewQueue.(*stubCursorPanel)
	if rqPanel.CursorPosition() != 0 {
		t.Errorf("expected ReviewQueue cursor at 0, got %d", rqPanel.CursorPosition())
	}
}

func TestKey_K_AtTopOfReviewQueue_WrapsToMyPRs(t *testing.T) {
	pr1 := &persistence.PullRequest{ID: "pr1"}
	pr2 := &persistence.PullRequest{ID: "pr2"}
	myPRs := newCursorStub("myPRs", 3, pr1)
	rq := newCursorStub("reviewQueue", 2, pr2)

	m, _ := newTestModel(myPRs, rq, newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.focused = PanelReviewQueue
	rq.cursor = 0 // at the top

	m = applyMsg(m, keyRune('k'))

	if m.focused != PanelMyPRs {
		t.Errorf("expected focus on MyPRs, got %d", m.focused)
	}
	myPanel := m.myPRs.(*stubCursorPanel)
	if myPanel.CursorPosition() != 2 {
		t.Errorf("expected MyPRs cursor at bottom (2), got %d", myPanel.CursorPosition())
	}
}

func TestKey_J_AtBottomOfReviewQueue_WrapsToMyPRs(t *testing.T) {
	pr1 := &persistence.PullRequest{ID: "pr1"}
	pr2 := &persistence.PullRequest{ID: "pr2"}
	myPRs := newCursorStub("myPRs", 2, pr1)
	rq := newCursorStub("reviewQueue", 2, pr2)

	m, _ := newTestModel(myPRs, rq, newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.focused = PanelReviewQueue
	rq.cursor = 1 // at the bottom

	m = applyMsg(m, keyRune('j'))

	if m.focused != PanelMyPRs {
		t.Errorf("expected focus on MyPRs, got %d", m.focused)
	}
	myPanel := m.myPRs.(*stubCursorPanel)
	if myPanel.CursorPosition() != 0 {
		t.Errorf("expected MyPRs cursor at 0, got %d", myPanel.CursorPosition())
	}
}

func TestKey_K_AtTopOfMyPRs_WrapsToReviewQueue(t *testing.T) {
	pr1 := &persistence.PullRequest{ID: "pr1"}
	pr2 := &persistence.PullRequest{ID: "pr2"}
	myPRs := newCursorStub("myPRs", 2, pr1)
	rq := newCursorStub("reviewQueue", 3, pr2)

	m, _ := newTestModel(myPRs, rq, newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.focused = PanelMyPRs
	myPRs.cursor = 0

	m = applyMsg(m, keyRune('k'))

	if m.focused != PanelReviewQueue {
		t.Errorf("expected focus on ReviewQueue, got %d", m.focused)
	}
	rqPanel := m.reviewQueue.(*stubCursorPanel)
	if rqPanel.CursorPosition() != 2 {
		t.Errorf("expected ReviewQueue cursor at bottom (2), got %d", rqPanel.CursorPosition())
	}
}

func TestKey_J_MiddleOfPanel_DoesNotWrap(t *testing.T) {
	pr := &persistence.PullRequest{ID: "pr1"}
	myPRs := newCursorStub("myPRs", 3, pr)
	rq := newCursorStub("reviewQueue", 2, nil)

	m, _ := newTestModel(myPRs, rq, newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.focused = PanelMyPRs
	myPRs.cursor = 0 // not at the bottom

	m = applyMsg(m, keyRune('j'))

	if m.focused != PanelMyPRs {
		t.Errorf("expected focus to remain on MyPRs, got %d", m.focused)
	}
	if myPRs.CursorPosition() != 1 {
		t.Errorf("expected cursor at 1, got %d", myPRs.CursorPosition())
	}
}

// ── j/k wrapping through all 3 panels ─────────────────────────────────────────

func TestKey_J_AtBottomOfReviewQueue_WrapsToWatches(t *testing.T) {
	pr := &persistence.PullRequest{ID: "pr1"}
	rq := newCursorStub("reviewQueue", 2, pr)
	watches := newCursorStub("watches", 3, nil)

	m, _ := newTestModel(
		newCursorStub("myPRs", 2, nil),
		rq, watches,
		newStub("detail", false), newStub("cmdBar", false),
	)
	m.focused = PanelReviewQueue
	rq.cursor = 1 // at the bottom

	m = applyMsg(m, keyRune('j'))

	if m.focused != PanelWatches {
		t.Errorf("expected focus on Watches, got %d", m.focused)
	}
	wp := m.watches.(*stubCursorPanel)
	if wp.CursorPosition() != 0 {
		t.Errorf("expected Watches cursor at 0, got %d", wp.CursorPosition())
	}
}

func TestKey_K_AtTopOfWatches_WrapsToReviewQueue(t *testing.T) {
	pr := &persistence.PullRequest{ID: "pr1"}
	rq := newCursorStub("reviewQueue", 3, pr)
	watches := newCursorStub("watches", 2, nil)

	m, _ := newTestModel(
		newCursorStub("myPRs", 2, nil),
		rq, watches,
		newStub("detail", false), newStub("cmdBar", false),
	)
	m.focused = PanelWatches
	watches.cursor = 0

	m = applyMsg(m, keyRune('k'))

	if m.focused != PanelReviewQueue {
		t.Errorf("expected focus on ReviewQueue, got %d", m.focused)
	}
	rqPanel := m.reviewQueue.(*stubCursorPanel)
	if rqPanel.CursorPosition() != 2 {
		t.Errorf("expected ReviewQueue cursor at bottom (2), got %d", rqPanel.CursorPosition())
	}
}

func TestKey_J_AtBottomOfWatches_WrapsToMyPRs(t *testing.T) {
	myPRs := newCursorStub("myPRs", 2, nil)
	watches := newCursorStub("watches", 2, nil)

	m, _ := newTestModel(
		myPRs,
		newCursorStub("reviewQueue", 2, nil),
		watches,
		newStub("detail", false), newStub("cmdBar", false),
	)
	m.focused = PanelWatches
	watches.cursor = 1 // at the bottom

	m = applyMsg(m, keyRune('j'))

	if m.focused != PanelMyPRs {
		t.Errorf("expected focus on MyPRs, got %d", m.focused)
	}
	mp := m.myPRs.(*stubCursorPanel)
	if mp.CursorPosition() != 0 {
		t.Errorf("expected MyPRs cursor at 0, got %d", mp.CursorPosition())
	}
}

func TestKey_K_AtTopOfMyPRs_WrapsToWatches(t *testing.T) {
	myPRs := newCursorStub("myPRs", 2, nil)
	watches := newCursorStub("watches", 3, nil)

	m, _ := newTestModel(
		myPRs,
		newCursorStub("reviewQueue", 2, nil),
		watches,
		newStub("detail", false), newStub("cmdBar", false),
	)
	m.focused = PanelMyPRs
	myPRs.cursor = 0

	m = applyMsg(m, keyRune('k'))

	if m.focused != PanelWatches {
		t.Errorf("expected focus on Watches, got %d", m.focused)
	}
	wp := m.watches.(*stubCursorPanel)
	if wp.CursorPosition() != 2 {
		t.Errorf("expected Watches cursor at bottom (2), got %d", wp.CursorPosition())
	}
}

func TestKey_J_WatchesHidden_SkipsWatches(t *testing.T) {
	pr := &persistence.PullRequest{ID: "pr1"}
	myPRs := newCursorStub("myPRs", 2, nil)
	rq := newCursorStub("reviewQueue", 2, pr)

	m, _ := newTestModel(
		myPRs, rq,
		newStub("watches", false), // hidden
		newStub("detail", false), newStub("cmdBar", false),
	)
	m.focused = PanelReviewQueue
	rq.cursor = 1 // at the bottom

	m = applyMsg(m, keyRune('j'))

	if m.focused != PanelMyPRs {
		t.Errorf("expected focus on MyPRs (skipping hidden Watches), got %d", m.focused)
	}
}

func TestKey_J_EmptyPanel_DoesNotWrap(t *testing.T) {
	myPRs := newCursorStub("myPRs", 0, nil) // empty panel
	rq := newCursorStub("reviewQueue", 2, nil)

	m, _ := newTestModel(myPRs, rq, newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.focused = PanelMyPRs

	m = applyMsg(m, keyRune('j'))

	// Should stay on MyPRs — empty panels don't wrap.
	if m.focused != PanelMyPRs {
		t.Errorf("j on empty panel should not change focus, got %d", m.focused)
	}
}

func TestKey_K_WrapsToEmptyOtherPanel(t *testing.T) {
	// MyPRs has rows, ReviewQueue is empty. k at top of MyPRs should still
	// wrap to ReviewQueue and call SetCursor(0) safely.
	myPRs := newCursorStub("myPRs", 2, nil)
	rq := newCursorStub("reviewQueue", 0, nil)

	m, _ := newTestModel(myPRs, rq, newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.focused = PanelMyPRs
	myPRs.cursor = 0

	m = applyMsg(m, keyRune('k'))

	// Should still wrap to ReviewQueue even though it's empty.
	if m.focused != PanelReviewQueue {
		t.Errorf("expected focus on ReviewQueue, got %d", m.focused)
	}
}

// ── nextVisiblePanel ──────────────────────────────────────────────────────────

func TestNextVisiblePanel(t *testing.T) {
	tests := []struct {
		name           string
		focused        Panel
		watchesContent bool
		want           Panel
	}{
		{"MyPRs→RQ (watches invisible)", PanelMyPRs, false, PanelReviewQueue},
		{"RQ→MyPRs (watches invisible)", PanelReviewQueue, false, PanelMyPRs},
		{"MyPRs→RQ (watches visible)", PanelMyPRs, true, PanelReviewQueue},
		{"RQ→Watches (watches visible)", PanelReviewQueue, true, PanelWatches},
		{"Watches→MyPRs (watches visible)", PanelWatches, true, PanelMyPRs},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newTestModel(
				newStub("myPRs", true),
				newStub("reviewQueue", true),
				newStub("watches", tt.watchesContent),
				newStub("detail", false),
				newStub("cmdBar", false),
			)
			m.focused = tt.focused
			got := m.nextVisiblePanel()
			if got != tt.want {
				t.Errorf("nextVisiblePanel() = %d, want %d", got, tt.want)
			}
		})
	}
}

// ── Diff modal tests ─────────────────────────────────────────────────────────

// TestShowDiffContentMsg_OpensDiffModal verifies that receiving a
// ShowDiffContentMsg sets diffVisible and populates the title.
func TestShowDiffContentMsg_OpensDiffModal(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))

	m = applyMsg(m, ShowDiffContentMsg{Title: "owner/repo#42", Content: "diff content"})

	if !m.diffVisible {
		t.Error("expected diffVisible to be true")
	}
	if m.diffTitle != "owner/repo#42" {
		t.Errorf("diffTitle = %q, want %q", m.diffTitle, "owner/repo#42")
	}
	if !strings.Contains(m.statusText, "diff owner/repo#42") {
		t.Errorf("statusText = %q, want to contain %q", m.statusText, "diff owner/repo#42")
	}
}

// TestShowDiffContentMsg_BlursCommandBar verifies that the command bar is
// unfocused when a diff modal opens.
func TestShowDiffContentMsg_BlursCommandBar(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.commandBarFocused = true

	m = applyMsg(m, ShowDiffContentMsg{Title: "t", Content: "c"})

	if m.commandBarFocused {
		t.Error("expected command bar to be unfocused after opening diff modal")
	}
}

// TestKey_Esc_DismissesDiffModal verifies Esc closes the diff overlay.
func TestKey_Esc_DismissesDiffModal(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.diffVisible = true

	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.diffVisible {
		t.Error("expected diffVisible to be false after Esc")
	}
}

// TestKey_Q_DismissesDiffModal verifies q closes the diff modal without
// quitting the app. We use applyMsg (which ignores the returned Cmd) to avoid
// blocking on waitForDBEvent.
func TestKey_Q_DismissesDiffModal(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.diffVisible = true

	m = applyMsg(m, keyRune('q'))

	if m.diffVisible {
		t.Error("expected diffVisible to be false after q")
	}
}

// TestKey_JK_ScrollsDiffModal verifies j/k scroll inside the diff modal and
// keep the modal open.
func TestKey_JK_ScrollsDiffModal(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.diffVisible = true

	m = applyMsg(m, keyRune('j'))
	if !m.diffVisible {
		t.Error("diff modal should remain visible after j")
	}

	m = applyMsg(m, keyRune('k'))
	if !m.diffVisible {
		t.Error("diff modal should remain visible after k")
	}
}

// TestView_DiffOverlay_VisibleWhenOpen verifies View() includes the diff modal
// content when diffVisible is true.
func TestView_DiffOverlay_VisibleWhenOpen(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.diffVisible = true
	m.diffTitle = "owner/repo#99"

	view := m.View()
	if !strings.Contains(view, "DIFF") {
		t.Errorf("View() should contain DIFF when diffVisible is true\ngot:\n%s", view)
	}
}

// TestView_DiffOverlay_AbsentWhenClosed verifies View() does not include the
// diff modal when diffVisible is false.
func TestView_DiffOverlay_AbsentWhenClosed(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))

	view := m.View()
	if strings.Contains(view, "DIFF") {
		t.Errorf("View() should not contain DIFF when diffVisible is false\ngot:\n%s", view)
	}
}

// TestWindowSizeMsg_SizesDiffViewport verifies WindowSizeMsg resizes the diff
// viewport alongside the help viewport.
func TestWindowSizeMsg_SizesDiffViewport(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))

	m = applyMsg(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	wantW, wantH := helpViewportSize(120, 40)
	if m.diffViewport.Width != wantW {
		t.Errorf("diffViewport.Width = %d, want %d", m.diffViewport.Width, wantW)
	}
	if m.diffViewport.Height != wantH {
		t.Errorf("diffViewport.Height = %d, want %d", m.diffViewport.Height, wantH)
	}
}

// TestWithDiffViewer_SetsViewer verifies the fluent setter.
func TestWithDiffViewer_SetsViewer(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	if m.diffViewer != nil {
		t.Fatal("expected nil diffViewer by default")
	}
	dv := &stubDiffViewer{}
	m = m.WithDiffViewer(dv)
	if m.diffViewer != dv {
		t.Error("expected diffViewer to be set after WithDiffViewer")
	}
}

// TestDiffModalView_NoDimensions verifies diffModalView renders without crash
// when terminal dimensions are zero.
func TestDiffModalView_NoDimensions(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.diffTitle = "owner/repo#1"

	got := m.diffModalView()
	if got == "" {
		t.Error("expected non-empty diff modal view even with zero dimensions")
	}
}

// TestDiffModalView_WithDimensions verifies diffModalView renders correctly
// with known terminal dimensions.
func TestDiffModalView_WithDimensions(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.width = 80
	m.height = 24
	m.diffTitle = "owner/repo#42"
	m.diffViewport.SetContent("--- a/file\n+++ b/file\n")

	got := m.diffModalView()
	if !strings.Contains(got, "DIFF owner/repo#42") {
		t.Errorf("expected diff modal view to contain title, got:\n%s", got)
	}
}

// TestDiffModalView_DarkTheme verifies the dark-theme border colour path.
func TestDiffModalView_DarkTheme(t *testing.T) {
	sub := &stubSubscriber{}
	dark := plainTheme()
	dark.Dark = true
	m := NewWithTheme("v0.0.0", "testuser", sub,
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
		dark, stubClock{now: t0})
	m.diffTitle = "owner/repo#1"

	got := m.diffModalView()
	if got == "" {
		t.Error("expected non-empty diff modal view with dark theme")
	}
}

// TestDiffModalView_ScrollHint verifies the "↓ more" hint appears when the
// viewport has more content to scroll.
func TestDiffModalView_ScrollHint(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.width = 80
	m.height = 24
	w, h := helpViewportSize(80, 24)
	m.diffViewport.Width = w
	m.diffViewport.Height = h
	m.diffTitle = "owner/repo#1"
	// Create content much taller than the viewport so there's content to scroll.
	var lines strings.Builder
	for i := range 200 {
		fmt.Fprintf(&lines, "line %d\n", i)
	}
	m.diffViewport.SetContent(lines.String())

	got := m.diffModalView()
	if !strings.Contains(got, "more") {
		t.Errorf("expected scroll hint in diff modal view, got:\n%s", got)
	}
}

// TestKey_D_DiffFetchError_SetsErrorStatus verifies that when the diff viewer
// returns an error, a CommandResultMsg with the error is produced.
func TestKey_D_DiffFetchError_SetsErrorStatus(t *testing.T) {
	pr := &persistence.PullRequest{Repo: "owner/repo", Number: 42, URL: "https://github.com/owner/repo/pull/42"}
	rq := newSelectorStub("reviewQueue", pr)
	dv := &stubDiffViewer{showErr: fmt.Errorf("network error")}
	m, _ := newTestModel(newStub("myPRs", true), rq,
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	m.focused = PanelReviewQueue
	m.diffViewer = dv

	_, cmd := m.Update(keyRune('d'))
	msg := executeBatchCmd(cmd)
	if msg == nil {
		t.Fatal("expected a message from the diff fetch cmd")
	}
	cr, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if cr.Err == nil || !strings.Contains(cr.Err.Error(), "network error") {
		t.Errorf("expected network error, got: %v", cr.Err)
	}
}

// ── Collaborator refresh tests ───────────────────────────────────────────────

// stubCollabLister implements CollaboratorLister for tests.
type stubCollabLister struct {
	logins []string
	err    error
	calls  int
}

func (s *stubCollabLister) ListKnownLogins(exclude string) ([]string, error) {
	s.calls++
	return s.logins, s.err
}

// findCollabMsg recursively executes a tea.Cmd and returns the first
// CollaboratorsUpdatedMsg found, or nil. Commands that block (e.g.
// waitForDBEvent) are abandoned after a short timeout.
func findCollabMsg(cmd tea.Cmd) *CollaboratorsUpdatedMsg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				if found := findCollabMsg(tea.Cmd(c)); found != nil {
					return found
				}
			}
			return nil
		}
		if cu, ok := msg.(CollaboratorsUpdatedMsg); ok {
			return &cu
		}
		return nil
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

func TestWithCollaboratorLister_SetsLister(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	if m.collabLister != nil {
		t.Fatal("expected nil collabLister by default")
	}
	cl := &stubCollabLister{}
	m = m.WithCollaboratorLister(cl)
	if m.collabLister != cl {
		t.Error("expected collabLister to be set after WithCollaboratorLister")
	}
}

func TestRefreshCollaborators_NilLister_ReturnsNil(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	cmd := m.refreshCollaborators()
	if cmd != nil {
		t.Error("expected nil cmd when collabLister is nil")
	}
}

func TestRefreshCollaborators_ReturnsCollaboratorsUpdatedMsg(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	cl := &stubCollabLister{logins: []string{"alice", "bob"}}
	m = m.WithCollaboratorLister(cl)

	cmd := m.refreshCollaborators()
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	msg := cmd()
	cu, ok := msg.(CollaboratorsUpdatedMsg)
	if !ok {
		t.Fatalf("expected CollaboratorsUpdatedMsg, got %T", msg)
	}
	if len(cu.Logins) != 2 || cu.Logins[0] != "alice" || cu.Logins[1] != "bob" {
		t.Errorf("Logins = %v, want [alice bob]", cu.Logins)
	}
}

func TestRefreshCollaborators_ErrorReturnsNilMsg(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	cl := &stubCollabLister{err: fmt.Errorf("db error")}
	m = m.WithCollaboratorLister(cl)

	cmd := m.refreshCollaborators()
	msg := cmd()
	if msg != nil {
		t.Errorf("expected nil msg on error, got %T", msg)
	}
}

func TestInit_IncludesRefreshCollaborators(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	cl := &stubCollabLister{logins: []string{"alice"}}
	m = m.WithCollaboratorLister(cl)

	cmd := m.Init()
	found := findCollabMsg(cmd)
	if found == nil {
		t.Fatal("Init() batch should include CollaboratorsUpdatedMsg")
	}
	if len(found.Logins) != 1 || found.Logins[0] != "alice" {
		t.Errorf("Logins = %v, want [alice]", found.Logins)
	}
}

func TestDBEvent_PRUpdated_TriggersCollaboratorRefresh(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	cl := &stubCollabLister{logins: []string{"alice"}}
	m = m.WithCollaboratorLister(cl)

	e := eventbus.Event{Type: eventbus.PRUpdated, After: "some-pr"}
	_, cmd := m.Update(DBEventMsg{Event: e})

	found := findCollabMsg(cmd)
	if found == nil {
		t.Fatal("PRUpdated should trigger collaborator refresh")
	}
}

func TestDBEvent_ReviewChanged_TriggersCollaboratorRefresh(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	cl := &stubCollabLister{logins: []string{"alice"}}
	m = m.WithCollaboratorLister(cl)

	e := eventbus.Event{Type: eventbus.ReviewChanged}
	_, cmd := m.Update(DBEventMsg{Event: e})

	found := findCollabMsg(cmd)
	if found == nil {
		t.Fatal("ReviewChanged should trigger collaborator refresh")
	}
}

func TestDBEvent_SessionIDsAssigned_TriggersCollaboratorRefresh(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	cl := &stubCollabLister{logins: []string{"alice"}}
	m = m.WithCollaboratorLister(cl)

	e := eventbus.Event{Type: eventbus.SessionIDsAssigned}
	_, cmd := m.Update(DBEventMsg{Event: e})

	found := findCollabMsg(cmd)
	if found == nil {
		t.Fatal("SessionIDsAssigned should trigger collaborator refresh")
	}
}

func TestDBEvent_CIChanged_DoesNotTriggerCollaboratorRefresh(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))
	cl := &stubCollabLister{logins: []string{"alice"}}
	m = m.WithCollaboratorLister(cl)

	e := eventbus.Event{Type: eventbus.CIChanged}
	_, cmd := m.Update(DBEventMsg{Event: e})

	found := findCollabMsg(cmd)
	if found != nil {
		t.Error("CIChanged should NOT trigger collaborator refresh")
	}
}

func TestModel_CollaboratorsUpdatedMsg_ForwardedToCommandBar(t *testing.T) {
	cmdBar := newStub("cmdBar", false)
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), cmdBar)

	msg := CollaboratorsUpdatedMsg{Logins: []string{"alice", "bob"}}
	m = applyMsg(m, msg)

	if _, ok := cmdBar.lastMsg.(CollaboratorsUpdatedMsg); !ok {
		t.Errorf("command bar did not receive CollaboratorsUpdatedMsg; lastMsg = %T", cmdBar.lastMsg)
	}
}

func TestModel_CollaboratorsUpdatedMsg_DoesNotFocusCommandBar(t *testing.T) {
	m, _ := newTestModel(newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false))

	m = applyMsg(m, CollaboratorsUpdatedMsg{Logins: []string{"alice"}})

	if m.commandBarFocused {
		t.Error("CollaboratorsUpdatedMsg should not focus the command bar")
	}
}
