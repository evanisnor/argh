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
	content := renderHelpContent(plainTheme(), "v0.0.0", "testuser")

	shortcuts := []string{
		"Tab",
		"Enter",
		"Esc",
	}
	for _, s := range shortcuts {
		if !strings.Contains(content, s) {
			t.Errorf("overlay missing keyboard shortcut %q; got:\n%s", s, content)
		}
	}
}

// TestOverlayContent_ContainsCommands verifies the overlay includes all
// interactive commands.
func TestOverlayContent_ContainsCommands(t *testing.T) {
	content := renderHelpContent(plainTheme(), "v0.0.0", "testuser")

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
		if !strings.Contains(content, cmd) {
			t.Errorf("overlay missing command %q; got:\n%s", cmd, content)
		}
	}
}

// TestOverlayContent_ContainsWatchSyntax verifies the overlay includes watch
// trigger and action syntax examples.
func TestOverlayContent_ContainsWatchSyntax(t *testing.T) {
	content := renderHelpContent(plainTheme(), "v0.0.0", "testuser")

	if !strings.Contains(content, "Watch Trigger") {
		t.Errorf("overlay missing 'Watch Trigger' section; got:\n%s", content)
	}
	if !strings.Contains(content, "Watch Action") {
		t.Errorf("overlay missing 'Watch Action' section; got:\n%s", content)
	}
	// Check a few trigger atoms.
	for _, trigger := range []string{"ci-pass", "approved", "stale"} {
		if !strings.Contains(content, trigger) {
			t.Errorf("overlay missing trigger atom %q; got:\n%s", trigger, content)
		}
	}
}

// TestRenderHelpContent_DarkTheme verifies renderHelpContent returns non-empty
// output for a dark theme.
func TestRenderHelpContent_DarkTheme(t *testing.T) {
	theme := plainTheme()
	theme.Dark = true

	out := renderHelpContent(theme, "v1.2.3", "testuser")
	if out == "" {
		t.Error("renderHelpContent should return non-empty output for dark theme")
	}
	if !strings.Contains(out, "keyboard reference") {
		t.Errorf("renderHelpContent output missing expected content; got:\n%s", out)
	}
}

// TestRenderHelpContent_LightTheme verifies renderHelpContent returns non-empty
// output for a light theme.
func TestRenderHelpContent_LightTheme(t *testing.T) {
	theme := plainTheme()
	theme.Dark = false

	out := renderHelpContent(theme, "v1.2.3", "testuser")
	if out == "" {
		t.Error("renderHelpContent should return non-empty output for light theme")
	}
}

// TestRenderHelpContent_IncludesVersionAndUsername verifies that the version
// and username are rendered in the help modal header.
func TestRenderHelpContent_IncludesVersionAndUsername(t *testing.T) {
	out := renderHelpContent(plainTheme(), "v9.8.7", "octocat")
	if !strings.Contains(out, "v9.8.7") {
		t.Errorf("renderHelpContent output missing version; got:\n%s", out)
	}
	if !strings.Contains(out, "@octocat") {
		t.Errorf("renderHelpContent output missing username; got:\n%s", out)
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

// TestKey_ScrollDown_ForwardsToViewport verifies that j/↓ scroll the viewport
// while the help overlay is visible.
func TestKey_ScrollDown_ForwardsToViewport(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
	)
	m.helpVisible = true

	// Apply j — model should remain in help mode and not crash.
	m = applyMsg(m, keyRune('j'))
	if !m.helpVisible {
		t.Error("helpVisible should remain true after j")
	}

	// Apply ↓
	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyDown})
	if !m.helpVisible {
		t.Error("helpVisible should remain true after ↓")
	}
}

// TestKey_ScrollUp_ForwardsToViewport verifies that k/↑ scroll the viewport
// while the help overlay is visible.
func TestKey_ScrollUp_ForwardsToViewport(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
	)
	m.helpVisible = true

	m = applyMsg(m, keyRune('k'))
	if !m.helpVisible {
		t.Error("helpVisible should remain true after k")
	}

	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyUp})
	if !m.helpVisible {
		t.Error("helpVisible should remain true after ↑")
	}
}

// TestKey_PageDown_ForwardsToViewport verifies pgdown scrolls the viewport.
func TestKey_PageDown_ForwardsToViewport(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
	)
	m.helpVisible = true

	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyPgDown})
	if !m.helpVisible {
		t.Error("helpVisible should remain true after pgdown")
	}
}

// TestKey_PageUp_ForwardsToViewport verifies pgup scrolls the viewport.
func TestKey_PageUp_ForwardsToViewport(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
	)
	m.helpVisible = true

	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyPgUp})
	if !m.helpVisible {
		t.Error("helpVisible should remain true after pgup")
	}
}

// TestHelpModalView_NoDimensionsRendersWithoutSizeConstraint verifies that
// helpModalView renders even when width/height are zero (no WindowSizeMsg yet).
func TestHelpModalView_NoDimensionsRendersWithoutSizeConstraint(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
	)
	// width and height remain 0 (no WindowSizeMsg).
	out := m.helpModalView()
	if out == "" {
		t.Error("helpModalView should return non-empty output even without terminal size")
	}
}

// TestHelpModalView_WithDimensions verifies that helpModalView applies width/height
// constraints when terminal dimensions are known.
func TestHelpModalView_WithDimensions(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
	)
	m = applyMsg(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	out := m.helpModalView()
	if out == "" {
		t.Error("helpModalView should return non-empty output with terminal size")
	}
}

// TestHelpModalView_AtBottomNoMoreIndicator verifies that the "↓ more" hint is
// absent when the viewport is scrolled to the bottom.
func TestHelpModalView_AtBottomNoMoreIndicator(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
	)
	m.helpViewport.GotoBottom()
	out := m.helpModalView()
	if strings.Contains(out, "↓ more") {
		t.Errorf("helpModalView should not show '↓ more' when at bottom; got:\n%s", out)
	}
}

// TestHelpViewportSize_ZeroInput verifies that zero dimensions return 0,0.
func TestHelpViewportSize_ZeroInput(t *testing.T) {
	w, h := helpViewportSize(0, 0)
	if w != 0 || h != 0 {
		t.Errorf("helpViewportSize(0,0) = %d,%d; want 0,0", w, h)
	}
	w, h = helpViewportSize(100, 0)
	if w != 0 || h != 0 {
		t.Errorf("helpViewportSize(100,0) = %d,%d; want 0,0", w, h)
	}
}

// TestHelpViewportSize_MinimumClamp verifies that tiny terminals clamp to 1×1.
func TestHelpViewportSize_MinimumClamp(t *testing.T) {
	// Width and height so small that 75%−4 would be ≤0.
	w, h := helpViewportSize(1, 1)
	if w < 1 {
		t.Errorf("helpViewportSize width should be at least 1; got %d", w)
	}
	if h < 1 {
		t.Errorf("helpViewportSize height should be at least 1; got %d", h)
	}
}

// TestHelpViewportSize_NormalTerminal verifies expected dimensions for a
// typical 120×40 terminal.
func TestHelpViewportSize_NormalTerminal(t *testing.T) {
	w, h := helpViewportSize(120, 40)
	// 120*3/4 - 4 = 86; 40*3/4 - 4 = 26
	if w != 86 {
		t.Errorf("helpViewportSize width = %d; want 86", w)
	}
	if h != 26 {
		t.Errorf("helpViewportSize height = %d; want 26", h)
	}
}

// TestWindowSizeMsg_SizesHelpViewport verifies that a WindowSizeMsg correctly
// resizes the help viewport.
func TestWindowSizeMsg_SizesHelpViewport(t *testing.T) {
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), newStub("detail", false), newStub("cmdBar", false),
	)

	m = applyMsg(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	wantW, wantH := helpViewportSize(120, 40)
	if m.helpViewport.Width != wantW {
		t.Errorf("helpViewport.Width = %d; want %d", m.helpViewport.Width, wantW)
	}
	if m.helpViewport.Height != wantH {
		t.Errorf("helpViewport.Height = %d; want %d", m.helpViewport.Height, wantH)
	}
}
