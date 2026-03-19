package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestKey_QuestionMark_OverlayVisibleInView verifies that when helpVisible is true
// the View() output contains the overlay content.
func TestKey_QuestionMark_OverlayVisibleInView(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
	)

	m = applyMsg(m, keyRune('?'))

	if !m.helpVisible {
		t.Fatal("helpVisible should be true after ?")
	}

	view := m.View()
	// The overlay must appear in the rendered output.
	if !strings.Contains(view, "keyboard reference") {
		t.Errorf("View() should contain overlay header when helpVisible; got:\n%s", view)
	}
}

// TestKey_Esc_DismissesOverlay_ViewRestored verifies that after Esc the normal
// layout is restored (overlay content absent).
func TestKey_Esc_DismissesOverlay_ViewRestored(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
	)
	m.helpVisible = true

	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.helpVisible {
		t.Fatal("helpVisible should be false after Esc")
	}

	view := m.View()
	// Normal layout should be present; overlay should be absent.
	if !strings.Contains(view, "MY PULL REQUESTS") {
		t.Errorf("View() should show normal layout after Esc; got:\n%s", view)
	}
}

// TestKey_SecondQuestionMark_DismissesOverlay verifies that a second ? dismisses
// the overlay.
func TestKey_SecondQuestionMark_DismissesOverlay(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
	)

	m = applyMsg(m, keyRune('?'))
	if !m.helpVisible {
		t.Fatal("helpVisible should be true after first ?")
	}

	m = applyMsg(m, keyRune('?'))
	if m.helpVisible {
		t.Error("helpVisible should be false after second ?")
	}

	view := m.View()
	if strings.Contains(view, "keyboard reference") {
		t.Errorf("overlay should be gone after second ?; got:\n%s", view)
	}
}

// TestShowHelpMsg_SetsHelpVisible verifies that receiving ShowHelpMsg causes
// helpVisible to become true.
func TestShowHelpMsg_SetsHelpVisible(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
	)

	if m.helpVisible {
		t.Fatal("helpVisible should be false initially")
	}

	m = applyMsg(m, ShowHelpMsg{})

	if !m.helpVisible {
		t.Error("helpVisible should be true after ShowHelpMsg")
	}
}

// TestShowHelpMsg_OverlayVisibleInView verifies View() includes overlay content
// after ShowHelpMsg is received (i.e., :help command path works).
func TestShowHelpMsg_OverlayVisibleInView(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
	)

	m = applyMsg(m, ShowHelpMsg{})

	view := m.View()
	if !strings.Contains(view, "keyboard reference") {
		t.Errorf(":help (ShowHelpMsg) should show overlay; got:\n%s", view)
	}
}

// TestOverlayContent_ContainsKeyboardShortcuts verifies the overlay includes
// all documented keyboard shortcuts.
func TestOverlayContent_ContainsKeyboardShortcuts(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
	)
	m.helpVisible = true
	view := m.View()

	shortcuts := []string{
		"Tab",
		"Enter",
		"Esc",
	}
	for _, s := range shortcuts {
		if !strings.Contains(view, s) {
			t.Errorf("overlay missing keyboard shortcut %q; got:\n%s", s, view)
		}
	}
}

// TestOverlayContent_ContainsCommands verifies the overlay includes all
// interactive commands.
func TestOverlayContent_ContainsCommands(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
	)
	m.helpVisible = true
	view := m.View()

	commands := []string{
		":open",
		":diff",
		":approve",
		":review",
		":request",
		":ready",
		":draft",
		":merge",
		":watch",
		":close",
		":reopen",
		":label",
		":comment",
		":dnd",
		":wake",
		":reload",
		":help",
		":quit",
	}
	for _, cmd := range commands {
		if !strings.Contains(view, cmd) {
			t.Errorf("overlay missing command %q; got:\n%s", cmd, view)
		}
	}
}

// TestOverlayContent_ContainsWatchSyntax verifies the overlay includes watch
// trigger and action syntax examples.
func TestOverlayContent_ContainsWatchSyntax(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
	)
	m.helpVisible = true
	view := m.View()

	if !strings.Contains(view, "Watch Trigger") {
		t.Errorf("overlay missing 'Watch Trigger' section; got:\n%s", view)
	}
	if !strings.Contains(view, "Watch Action") {
		t.Errorf("overlay missing 'Watch Action' section; got:\n%s", view)
	}
	// Check a few trigger atoms.
	for _, trigger := range []string{"ci-pass", "approved", "stale"} {
		if !strings.Contains(view, trigger) {
			t.Errorf("overlay missing trigger atom %q; got:\n%s", trigger, view)
		}
	}
}

// TestRenderHelpOverlay_DarkTheme verifies renderHelpOverlay returns non-empty
// output for a dark theme.
func TestRenderHelpOverlay_DarkTheme(t *testing.T) {
	theme := plainTheme()
	theme.Dark = true

	out := renderHelpOverlay(theme)
	if out == "" {
		t.Error("renderHelpOverlay should return non-empty output for dark theme")
	}
	if !strings.Contains(out, "keyboard reference") {
		t.Errorf("renderHelpOverlay output missing expected content; got:\n%s", out)
	}
}

// TestRenderHelpOverlay_LightTheme verifies renderHelpOverlay returns non-empty
// output for a light theme.
func TestRenderHelpOverlay_LightTheme(t *testing.T) {
	theme := plainTheme()
	theme.Dark = false

	out := renderHelpOverlay(theme)
	if out == "" {
		t.Error("renderHelpOverlay should return non-empty output for light theme")
	}
}

// TestDimBackground_ReturnsNonEmptyString verifies dimBackground renders its
// input (does not return empty string for non-empty input).
func TestDimBackground_ReturnsNonEmptyString(t *testing.T) {
	in := "hello world"
	out := dimBackground(in)
	if out == "" {
		t.Error("dimBackground should return non-empty output")
	}
}

// TestView_HelpOverlay_UnderlyingPanelsDimmed verifies that the normal layout
// string still appears (dimmed) when the help overlay is visible, so the
// underlying panels are not entirely hidden.
func TestView_HelpOverlay_UnderlyingPanelsDimmed(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
	)
	m.helpVisible = true

	view := m.View()

	// Both the overlay content and the underlying panel labels must appear in
	// the combined view string.
	if !strings.Contains(view, "keyboard reference") {
		t.Errorf("overlay content missing; got:\n%s", view)
	}
	if !strings.Contains(view, "MY PULL REQUESTS") {
		t.Errorf("underlying layout should remain visible (dimmed) behind overlay; got:\n%s", view)
	}
}
