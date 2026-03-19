package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func focusBar(t *testing.T, cb *CommandBar) {
	t.Helper()
	sm, _ := cb.Update(FocusCommandBarMsg{})
	*cb = *sm.(*CommandBar)
}

func typeInto(t *testing.T, cb *CommandBar, s string) {
	t.Helper()
	for _, r := range s {
		sm, _ := cb.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		*cb = *sm.(*CommandBar)
	}
}

func pressKey(t *testing.T, cb *CommandBar, key string) {
	t.Helper()
	var msg tea.KeyMsg
	switch key {
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	sm, _ := cb.Update(msg)
	*cb = *sm.(*CommandBar)
}

func samplePRRefs() []PRRef {
	return []PRRef{
		{SessionID: "a", Number: 42, Title: "Fix login bug", Repo: "myrepo"},
		{SessionID: "b", Number: 99, Title: "Add dark mode", Repo: "myrepo"},
		{SessionID: "c", Number: 7, Title: "Refactor auth", Repo: "myrepo"},
	}
}

// ── NewCommandBar ─────────────────────────────────────────────────────────────

func TestNewCommandBar(t *testing.T) {
	cb := NewCommandBar()
	if cb == nil {
		t.Fatal("expected non-nil CommandBar")
	}
	if cb.focused {
		t.Error("expected unfocused by default")
	}
	if cb.histCursor != -1 {
		t.Errorf("expected histCursor=-1, got %d", cb.histCursor)
	}
}

// ── HasContent ────────────────────────────────────────────────────────────────

func TestCommandBar_HasContent(t *testing.T) {
	cb := NewCommandBar()
	if !cb.HasContent() {
		t.Error("HasContent should always return true")
	}
}

// ── SetPRRefs / SetCollaborators / Value ──────────────────────────────────────

func TestCommandBar_SetPRRefsAndCollaborators(t *testing.T) {
	cb := NewCommandBar()
	cb.SetPRRefs(samplePRRefs())
	cb.SetCollaborators([]string{"alice", "bob"})
	if len(cb.prRefs) != 3 {
		t.Errorf("expected 3 PR refs, got %d", len(cb.prRefs))
	}
	if len(cb.collaborators) != 2 {
		t.Errorf("expected 2 collaborators, got %d", len(cb.collaborators))
	}
	if cb.Value() != "" {
		t.Errorf("expected empty value, got %q", cb.Value())
	}
}

// ── View ──────────────────────────────────────────────────────────────────────

func TestCommandBar_View_Unfocused(t *testing.T) {
	cb := NewCommandBar()
	v := cb.View()
	if v != "/ or : for commands" {
		t.Errorf("unexpected unfocused view: %q", v)
	}
}

func TestCommandBar_View_Focused_NoSuggestions(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	typeInto(t, cb, ":zzznomatch")
	v := cb.View()
	// When there are no suggestions, the view should have only one line (no "\n").
	if strings.Contains(v, "\n") {
		t.Errorf("expected single-line view with no suggestions, got: %q", v)
	}
}

func TestCommandBar_View_Focused_WithSuggestions(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	typeInto(t, cb, ":me")
	v := cb.View()
	if !strings.Contains(v, ":merge") {
		t.Errorf("expected :merge in suggestions, view: %q", v)
	}
}

func TestCommandBar_View_Focused_WithHint(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	// Type a full command + space to enter PR mode which shows a hint.
	typeInto(t, cb, ":merge ")
	v := cb.View()
	if !strings.Contains(v, ":merge") {
		t.Errorf("expected hint in view, got: %q", v)
	}
}

func TestCommandBar_View_SuggestionCursorHighlighted(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	typeInto(t, cb, ":me")
	v := cb.View()
	// The top suggestion should be marked with "> ".
	if !strings.Contains(v, "> ") {
		t.Errorf("expected '>  ' cursor in suggestion list, view: %q", v)
	}
}

func TestCommandBar_View_MoreThanMaxSuggestions(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	// Empty input → all commands shown, but View should limit to maxSuggestions.
	v := cb.View()
	lines := strings.Split(v, "\n")
	// First line is the input; remaining are suggestions capped at maxSuggestions.
	suggLines := lines[1:]
	if len(suggLines) > maxSuggestions {
		t.Errorf("expected at most %d suggestion lines, got %d", maxSuggestions, len(suggLines))
	}
}

// ── Focus / Blur via messages ─────────────────────────────────────────────────

func TestCommandBar_FocusMessage(t *testing.T) {
	cb := NewCommandBar()
	sm, _ := cb.Update(FocusCommandBarMsg{})
	result := sm.(*CommandBar)
	if !result.focused {
		t.Error("expected focused after FocusCommandBarMsg")
	}
}

func TestCommandBar_BlurMessage(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	typeInto(t, cb, ":merge")
	sm, _ := cb.Update(BlurCommandBarMsg{})
	result := sm.(*CommandBar)
	if result.focused {
		t.Error("expected unfocused after BlurCommandBarMsg")
	}
	if result.Value() != "" {
		t.Errorf("expected empty value after blur, got %q", result.Value())
	}
	if result.hint != "" {
		t.Errorf("expected empty hint after blur, got %q", result.hint)
	}
	if result.mode != cbModeCommand {
		t.Errorf("expected cbModeCommand after blur, got %d", result.mode)
	}
	if len(result.suggestions) != 0 {
		t.Errorf("expected no suggestions after blur, got %d", len(result.suggestions))
	}
}

// ── Update: unhandled message ─────────────────────────────────────────────────

func TestCommandBar_Update_UnknownMsg(t *testing.T) {
	cb := NewCommandBar()
	sm, cmd := cb.Update("some random string")
	if sm == nil {
		t.Error("expected non-nil SubModel")
	}
	if cmd != nil {
		t.Error("expected nil cmd for unknown message")
	}
}

// ── Key handling: not focused ─────────────────────────────────────────────────

func TestCommandBar_Key_NotFocused(t *testing.T) {
	cb := NewCommandBar()
	// Should be a no-op when not focused.
	sm, cmd := cb.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if sm == nil {
		t.Error("expected non-nil SubModel")
	}
	if cmd != nil {
		t.Error("expected nil cmd when not focused")
	}
}

// ── Tab key: accept top suggestion ───────────────────────────────────────────

func TestCommandBar_Tab_NoSuggestions(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	typeInto(t, cb, ":zzznomatch")
	before := cb.Value()
	pressKey(t, cb, "tab")
	if cb.Value() != before {
		t.Errorf("tab with no suggestions should not change input; before=%q after=%q", before, cb.Value())
	}
}

func TestCommandBar_Tab_AcceptsTopCommandSuggestion(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	typeInto(t, cb, ":mer")
	pressKey(t, cb, "tab")
	// Should have accepted ":merge" and appended a space.
	if !strings.HasPrefix(cb.Value(), ":merge ") {
		t.Errorf("expected ':merge ' prefix after tab, got %q", cb.Value())
	}
}

func TestCommandBar_Tab_AcceptsPRSuggestion(t *testing.T) {
	cb := NewCommandBar()
	cb.SetPRRefs(samplePRRefs())
	focusBar(t, cb)
	typeInto(t, cb, ":merge ")
	// Now in PR mode — type "a" to match session ID "a".
	typeInto(t, cb, "a")
	pressKey(t, cb, "tab")
	if !strings.Contains(cb.Value(), "a ") {
		t.Errorf("expected PR ref 'a' accepted in value, got %q", cb.Value())
	}
}

func TestCommandBar_Tab_AcceptsCollaboratorSuggestion(t *testing.T) {
	cb := NewCommandBar()
	cb.SetPRRefs(samplePRRefs())
	cb.SetCollaborators([]string{"alice", "bob"})
	focusBar(t, cb)
	// :request #42 @ali → should complete to alice
	typeInto(t, cb, ":request #42 @ali")
	pressKey(t, cb, "tab")
	if !strings.Contains(cb.Value(), "@alice") {
		t.Errorf("expected @alice after tab completion, got %q", cb.Value())
	}
}

func TestCommandBar_Tab_CollaboratorMode_NoAtSign(t *testing.T) {
	cb := NewCommandBar()
	cb.SetCollaborators([]string{"alice"})
	focusBar(t, cb)
	// Manually force collaborator mode without @ in input to test the branch.
	cb.mode = cbModeCollaborator
	cb.suggestions = []string{"alice"}
	cb.input.SetValue(":request #42 alice")
	pressKey(t, cb, "tab")
	// No @ in value means the collaborator accept is a no-op for SetValue.
	// Value should be unchanged since atIdx < 0.
	if !strings.HasPrefix(cb.Value(), ":request") {
		t.Errorf("expected :request prefix, got %q", cb.Value())
	}
}

// ── Up/Down: suggestion navigation ───────────────────────────────────────────

func TestCommandBar_Up_WithSuggestions_MovesUp(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	typeInto(t, cb, ":me")
	// Move down first to have room to go up.
	pressKey(t, cb, "down")
	before := cb.suggCursor
	pressKey(t, cb, "up")
	if cb.suggCursor != before-1 {
		t.Errorf("expected suggCursor=%d, got %d", before-1, cb.suggCursor)
	}
}

func TestCommandBar_Up_WithSuggestions_AtTopNoChange(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	typeInto(t, cb, ":me")
	cb.suggCursor = 0
	pressKey(t, cb, "up")
	if cb.suggCursor != 0 {
		t.Errorf("expected suggCursor=0, got %d", cb.suggCursor)
	}
}

func TestCommandBar_Down_WithSuggestions_MovesDown(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	typeInto(t, cb, ":me")
	before := cb.suggCursor
	pressKey(t, cb, "down")
	if cb.suggCursor != before+1 {
		t.Errorf("expected suggCursor=%d, got %d", before+1, cb.suggCursor)
	}
}

func TestCommandBar_Down_WithSuggestions_AtBottomNoChange(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	typeInto(t, cb, ":me")
	cb.suggCursor = len(cb.suggestions) - 1
	pressKey(t, cb, "down")
	if cb.suggCursor != len(cb.suggestions)-1 {
		t.Errorf("expected cursor pinned at %d, got %d", len(cb.suggestions)-1, cb.suggCursor)
	}
}

// ── Up/Down: history navigation ───────────────────────────────────────────────

func TestCommandBar_History_UpEntersHistory(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	// Add something to history manually.
	cb.history = []string{":reload", ":help"}
	typeInto(t, cb, ":merge 1")
	// No suggestions for ":merge 1" without prRefs — should navigate history.
	cb.suggestions = nil
	pressKey(t, cb, "up")
	if cb.Value() != ":help" {
		t.Errorf("expected ':help' from history, got %q", cb.Value())
	}
	if cb.histCursor != 1 {
		t.Errorf("expected histCursor=1, got %d", cb.histCursor)
	}
}

func TestCommandBar_History_UpAgainGoesBack(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	cb.history = []string{":reload", ":help"}
	cb.suggestions = nil
	pressKey(t, cb, "up") // goes to :help (index 1)
	pressKey(t, cb, "up") // goes to :reload (index 0)
	if cb.Value() != ":reload" {
		t.Errorf("expected ':reload' from history, got %q", cb.Value())
	}
}

func TestCommandBar_History_UpAtStartStays(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	cb.history = []string{":reload"}
	cb.suggestions = nil
	pressKey(t, cb, "up") // goes to :reload (index 0)
	pressKey(t, cb, "up") // already at 0, should stay
	if cb.Value() != ":reload" {
		t.Errorf("expected ':reload', got %q", cb.Value())
	}
	if cb.histCursor != 0 {
		t.Errorf("expected histCursor=0, got %d", cb.histCursor)
	}
}

func TestCommandBar_History_EmptyHistoryUpNoOp(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	cb.suggestions = nil
	pressKey(t, cb, "up")
	if cb.histCursor != -1 {
		t.Errorf("expected histCursor=-1 with empty history, got %d", cb.histCursor)
	}
}

func TestCommandBar_History_DownRestoresSavedInput(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	cb.history = []string{":reload", ":help"}
	cb.suggestions = nil
	// Type something, then go back in history, then forward.
	typeInto(t, cb, ":diff")
	cb.suggestions = nil
	pressKey(t, cb, "up") // saves ":diff", goes to :help
	pressKey(t, cb, "down") // should restore saved input ":diff"
	if cb.Value() != ":diff" {
		t.Errorf("expected restored input ':diff', got %q", cb.Value())
	}
	if cb.histCursor != -1 {
		t.Errorf("expected histCursor=-1 after forward past end, got %d", cb.histCursor)
	}
}

func TestCommandBar_History_DownForwardInHistory(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	cb.history = []string{":reload", ":help"}
	cb.suggestions = nil
	pressKey(t, cb, "up") // → :help (index 1)
	pressKey(t, cb, "up") // → :reload (index 0)
	pressKey(t, cb, "down") // → :help (index 1)
	if cb.Value() != ":help" {
		t.Errorf("expected ':help', got %q", cb.Value())
	}
}

func TestCommandBar_History_DownWhenNotNavigating(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	cb.history = []string{":reload"}
	cb.suggestions = nil
	// histCursor is -1, pressing down should be a no-op.
	pressKey(t, cb, "down")
	if cb.histCursor != -1 {
		t.Errorf("expected histCursor=-1, got %d", cb.histCursor)
	}
}

// ── Enter: commit to history ──────────────────────────────────────────────────

func TestCommandBar_Enter_AddsToHistory(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	typeInto(t, cb, ":reload")
	pressKey(t, cb, "enter")
	if len(cb.history) != 1 || cb.history[0] != ":reload" {
		t.Errorf("expected history=[':reload'], got %v", cb.history)
	}
	if cb.histCursor != -1 {
		t.Errorf("expected histCursor=-1, got %d", cb.histCursor)
	}
}

func TestCommandBar_Enter_EmptyInputNotAddedToHistory(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	pressKey(t, cb, "enter")
	if len(cb.history) != 0 {
		t.Errorf("expected empty history, got %v", cb.history)
	}
}

// ── Esc ───────────────────────────────────────────────────────────────────────

func TestCommandBar_Esc_NoOp(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	typeInto(t, cb, ":merge")
	before := cb.Value()
	pressKey(t, cb, "esc")
	// Esc is handled by root model; command bar itself does nothing.
	if cb.Value() != before {
		t.Errorf("expected value unchanged after esc, got %q", cb.Value())
	}
	if !cb.focused {
		t.Error("expected command bar still focused after esc (root model handles blur)")
	}
}

// ── Regular typing resets history cursor ─────────────────────────────────────

func TestCommandBar_Typing_ResetsHistoryCursor(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	cb.history = []string{":reload"}
	cb.suggestions = nil
	pressKey(t, cb, "up") // enters history nav
	typeInto(t, cb, "x")  // should reset histCursor
	if cb.histCursor != -1 {
		t.Errorf("expected histCursor reset to -1 after typing, got %d", cb.histCursor)
	}
}

// ── computeSuggestions ────────────────────────────────────────────────────────

func TestComputeSuggestions_CommandMode_FuzzyMatch(t *testing.T) {
	mode, hint, suggestions := computeSuggestions(":mer", nil, nil)
	if mode != cbModeCommand {
		t.Errorf("expected cbModeCommand, got %d", mode)
	}
	found := false
	for _, s := range suggestions {
		if s == ":merge" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected :merge in suggestions %v", suggestions)
	}
	if hint != ":merge [#pr]" {
		t.Errorf("expected hint ':merge [#pr]', got %q", hint)
	}
}

func TestComputeSuggestions_CommandMode_EmptyInput(t *testing.T) {
	mode, hint, suggestions := computeSuggestions("", nil, nil)
	if mode != cbModeCommand {
		t.Errorf("expected cbModeCommand, got %d", mode)
	}
	if hint != "" {
		t.Errorf("expected empty hint for empty input, got %q", hint)
	}
	if len(suggestions) != len(commandList) {
		t.Errorf("expected all %d commands, got %d", len(commandList), len(suggestions))
	}
}

func TestComputeSuggestions_CommandMode_NoMatch(t *testing.T) {
	_, hint, suggestions := computeSuggestions(":zzznomatch", nil, nil)
	if len(suggestions) != 0 {
		t.Errorf("expected no suggestions, got %v", suggestions)
	}
	if hint != "" {
		t.Errorf("expected empty hint, got %q", hint)
	}
}

func TestComputeSuggestions_UnknownCommand(t *testing.T) {
	mode, hint, suggestions := computeSuggestions(":unknown arg", nil, nil)
	if mode != cbModeCommand {
		t.Errorf("expected cbModeCommand for unknown command, got %d", mode)
	}
	if hint != "" || len(suggestions) != 0 {
		t.Errorf("expected empty hint/suggestions for unknown command, got hint=%q sugg=%v", hint, suggestions)
	}
}

func TestComputeSuggestions_PRMode(t *testing.T) {
	refs := samplePRRefs()
	mode, hint, suggestions := computeSuggestions(":merge ", refs, nil)
	if mode != cbModePR {
		t.Errorf("expected cbModePR, got %d", mode)
	}
	if hint == "" {
		t.Error("expected non-empty hint in PR mode")
	}
	if len(suggestions) == 0 {
		t.Errorf("expected PR suggestions for empty arg, got none")
	}
}

func TestComputeSuggestions_CollaboratorMode(t *testing.T) {
	refs := samplePRRefs()
	collabs := []string{"alice", "bob", "charlie"}
	mode, hint, suggestions := computeSuggestions(":request #42 @ali", refs, collabs)
	if mode != cbModeCollaborator {
		t.Errorf("expected cbModeCollaborator, got %d", mode)
	}
	if hint == "" {
		t.Error("expected non-empty hint in collaborator mode")
	}
	found := false
	for _, s := range suggestions {
		if s == "alice" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'alice' in collaborator suggestions %v", suggestions)
	}
}

func TestComputeSuggestions_RequestWithoutAt(t *testing.T) {
	refs := samplePRRefs()
	collabs := []string{"alice"}
	// :request with no @ — should fall into PR mode, not collaborator mode.
	mode, _, _ := computeSuggestions(":request a", refs, collabs)
	if mode != cbModePR {
		t.Errorf("expected cbModePR when no @ present in :request, got %d", mode)
	}
}

func TestComputeSuggestions_NoNeedsPR_NoNeedsCollaborator(t *testing.T) {
	// :reload has no PR or collaborator need.
	mode, hint, suggestions := computeSuggestions(":reload extra", nil, nil)
	if mode != cbModeCommand {
		t.Errorf("expected cbModeCommand for :reload, got %d", mode)
	}
	if hint == "" {
		t.Error("expected non-empty signature hint for :reload")
	}
	if len(suggestions) != 0 {
		t.Errorf("expected no suggestions for :reload, got %v", suggestions)
	}
}

// ── prMatches ─────────────────────────────────────────────────────────────────

func TestPRMatches_EmptyFragment_ReturnsAll(t *testing.T) {
	refs := samplePRRefs()
	out := prMatches("", refs)
	if len(out) != len(refs) {
		t.Errorf("expected all %d refs for empty fragment, got %d", len(refs), len(out))
	}
}

func TestPRMatches_EmptyFragment_EmptySessionIDFallsBackToNumber(t *testing.T) {
	refs := []PRRef{
		{SessionID: "", Number: 10, Title: "no session", Repo: "r"},
	}
	out := prMatches("", refs)
	if len(out) != 1 || out[0] != "#10" {
		t.Errorf("expected ['#10'], got %v", out)
	}
}

func TestPRMatches_EmptyRefs(t *testing.T) {
	out := prMatches("a", nil)
	if len(out) != 0 {
		t.Errorf("expected no matches with empty refs, got %v", out)
	}
}

func TestPRMatches_NumberExactMatch(t *testing.T) {
	refs := samplePRRefs()
	out := prMatches("#42", refs)
	if len(out) != 1 || out[0] != "#42" {
		t.Errorf("expected ['#42'], got %v", out)
	}
}

func TestPRMatches_NumberNoExactMatchReturnsAll(t *testing.T) {
	refs := samplePRRefs()
	out := prMatches("#999", refs)
	// No exact match — all refs returned as #number.
	if len(out) != len(refs) {
		t.Errorf("expected %d entries for unmatched number, got %d", len(refs), len(out))
	}
}

func TestPRMatches_HashPrefix_InvalidNumber(t *testing.T) {
	refs := samplePRRefs()
	// "#abc" — strconv.Atoi fails, fall through to "list all".
	out := prMatches("#abc", refs)
	if len(out) != len(refs) {
		t.Errorf("expected all refs for invalid number, got %v", out)
	}
}

func TestPRMatches_ExactSessionID(t *testing.T) {
	refs := samplePRRefs()
	out := prMatches("b", refs)
	if len(out) != 1 || out[0] != "b" {
		t.Errorf("expected ['b'], got %v", out)
	}
}

func TestPRMatches_FuzzyTitle(t *testing.T) {
	refs := samplePRRefs()
	out := prMatches("login", refs) // matches "Fix login bug"
	if len(out) == 0 {
		t.Error("expected fuzzy title match, got none")
	}
	// Should return the session ID "a" for the matched PR.
	if out[0] != "a" {
		t.Errorf("expected session ID 'a', got %q", out[0])
	}
}

func TestPRMatches_FuzzyTitle_NoMatch(t *testing.T) {
	refs := samplePRRefs()
	out := prMatches("xyzzyabcdef", refs)
	if len(out) != 0 {
		t.Errorf("expected no matches, got %v", out)
	}
}

func TestPRMatches_FuzzyTitle_EmptySessionIDFallsBackToNumber(t *testing.T) {
	refs := []PRRef{
		{SessionID: "", Number: 55, Title: "My feature", Repo: "repo"},
	}
	out := prMatches("feature", refs)
	if len(out) != 1 || out[0] != "#55" {
		t.Errorf("expected ['#55'] for empty session ID, got %v", out)
	}
}

// ── fuzzyFilterStrings ────────────────────────────────────────────────────────

func TestFuzzyFilterStrings_EmptyPattern_ReturnsAll(t *testing.T) {
	items := []string{"alpha", "beta", "gamma"}
	out := fuzzyFilterStrings("", items)
	if len(out) != len(items) {
		t.Errorf("expected all items for empty pattern, got %v", out)
	}
}

func TestFuzzyFilterStrings_Filtered(t *testing.T) {
	items := []string{":merge", ":review", ":reload"}
	out := fuzzyFilterStrings(":mer", items)
	found := false
	for _, s := range out {
		if s == ":merge" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected ':merge' in filtered results %v", out)
	}
}

// ── commandSignatureHint ──────────────────────────────────────────────────────

func TestCommandSignatureHint_Empty(t *testing.T) {
	h := commandSignatureHint("")
	if h != "" {
		t.Errorf("expected empty hint for empty input, got %q", h)
	}
}

func TestCommandSignatureHint_NoMatch(t *testing.T) {
	h := commandSignatureHint(":zzznomatch")
	if h != "" {
		t.Errorf("expected empty hint for no match, got %q", h)
	}
}

func TestCommandSignatureHint_Match(t *testing.T) {
	h := commandSignatureHint(":mer")
	if h == "" {
		t.Error("expected non-empty hint for ':mer'")
	}
}

// ── allCommandNames ───────────────────────────────────────────────────────────

func TestAllCommandNames(t *testing.T) {
	names := allCommandNames()
	if len(names) != len(commandList) {
		t.Errorf("expected %d command names, got %d", len(commandList), len(names))
	}
}

// ── findCommandDef ────────────────────────────────────────────────────────────

func TestFindCommandDef_Found(t *testing.T) {
	def := findCommandDef(":merge")
	if def == nil {
		t.Fatal("expected to find :merge")
	}
	if def.name != ":merge" {
		t.Errorf("expected :merge, got %q", def.name)
	}
}

func TestFindCommandDef_NotFound(t *testing.T) {
	def := findCommandDef(":notacommand")
	if def != nil {
		t.Errorf("expected nil, got %+v", def)
	}
}

// ── refreshSuggestions: cursor clamp ─────────────────────────────────────────

func TestRefreshSuggestions_ClampsCursor(t *testing.T) {
	cb := NewCommandBar()
	focusBar(t, cb)
	typeInto(t, cb, ":me") // a few suggestions
	cb.suggCursor = 100    // force out-of-range cursor
	cb.refreshSuggestions()
	if cb.suggCursor != 0 {
		t.Errorf("expected suggCursor clamped to 0, got %d", cb.suggCursor)
	}
}
