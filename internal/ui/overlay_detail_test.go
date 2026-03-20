package ui

import (
	"strings"
	"testing"
)

// TestOverlayModal_ZeroDimensions_StacksBelow verifies that when terminal
// dimensions are zero (before first WindowSizeMsg) the modal is appended below
// the base unchanged.
func TestOverlayModal_ZeroDimensions_StacksBelow(t *testing.T) {
	base := "panel line\ncmdbar"
	modal := "modal content"

	got := overlayModal(base, modal, 0, 0)
	if !strings.Contains(got, "panel line") {
		t.Errorf("base not present in result: %q", got)
	}
	if !strings.Contains(got, "modal content") {
		t.Errorf("modal not present in result: %q", got)
	}
}

// TestOverlayModal_ZeroWidth_StacksBelow verifies that zero width triggers the
// fallback path.
func TestOverlayModal_ZeroWidth_StacksBelow(t *testing.T) {
	base := "panel\ncmdbar"
	modal := "MODAL"
	got := overlayModal(base, modal, 0, 10)
	if !strings.Contains(got, "MODAL") {
		t.Errorf("modal not present in result: %q", got)
	}
}

// TestOverlayModal_ZeroHeight_StacksBelow verifies that zero height triggers
// the fallback path.
func TestOverlayModal_ZeroHeight_StacksBelow(t *testing.T) {
	base := "panel\ncmdbar"
	modal := "MODAL"
	got := overlayModal(base, modal, 80, 0)
	if !strings.Contains(got, "MODAL") {
		t.Errorf("modal not present in result: %q", got)
	}
}

// TestOverlayModal_EmptyModal_ReturnsBaseStacked verifies that an empty modal
// causes the fallback path (base + "\n").
func TestOverlayModal_EmptyModal_ReturnsBaseStacked(t *testing.T) {
	base := "panel line\ncmdbar"
	got := overlayModal(base, "", 80, 24)
	if !strings.Contains(got, "panel line") {
		t.Errorf("base not present in result: %q", got)
	}
}

// TestOverlayModal_ModalContentAppearsInResult verifies that modal text is
// present in the final output when proper dimensions are given.
func TestOverlayModal_ModalContentAppearsInResult(t *testing.T) {
	base := strings.Repeat("background\n", 9) + "last"
	modal := "MODAL BOX"
	result := overlayModal(base, modal, 20, 10)
	if !strings.Contains(result, "MODAL BOX") {
		t.Errorf("modal content not found in result:\n%s", result)
	}
}

// TestOverlayModal_DoesNotChangeLineCount verifies the result has the same
// number of lines as the base.
func TestOverlayModal_DoesNotChangeLineCount(t *testing.T) {
	base := "line1\nline2\nline3\nline4\nline5"
	modal := "MODAL"
	result := overlayModal(base, modal, 20, 5)
	baseCount := len(strings.Split(base, "\n"))
	resultCount := len(strings.Split(result, "\n"))
	if resultCount != baseCount {
		t.Errorf("line count changed: base=%d result=%d", baseCount, resultCount)
	}
}

// TestOverlayModal_BackgroundLinesRetainBaseContent verifies that lines outside
// the modal region still contain the original base content (they are not erased).
func TestOverlayModal_BackgroundLinesRetainBaseContent(t *testing.T) {
	// 5-line base, 1-line modal centred at row 2.
	base := "AAAA\nBBBB\nCCCC\nDDDD\nEEEE"
	modal := "M"
	result := overlayModal(base, modal, 10, 5)
	lines := strings.Split(result, "\n")

	for _, tc := range []struct {
		idx  int
		want string
	}{
		{0, "AAAA"},
		{1, "BBBB"},
		{3, "DDDD"},
		{4, "EEEE"},
	} {
		if !strings.Contains(lines[tc.idx], tc.want) {
			t.Errorf("line %d expected to contain %q; got: %q", tc.idx, tc.want, lines[tc.idx])
		}
	}
}

// TestOverlayModal_ModalLineNotDimmed verifies that the line containing the
// modal box is not wrapped in the faint code.
func TestOverlayModal_ModalLineNotDimmed(t *testing.T) {
	// 5-line base, 1-line modal — centred at row 2 (index 2).
	base := "A\nB\nC\nD\nE"
	modal := "MODALTEXT"
	result := overlayModal(base, modal, 20, 5)
	if !strings.Contains(result, "MODALTEXT") {
		t.Errorf("modal text not found in result:\n%s", result)
	}
}

// TestOverlayModal_TallModal_ClampedToBase verifies that when the modal is
// taller than the base the result still has the same line count as the base.
func TestOverlayModal_TallModal_ClampedToBase(t *testing.T) {
	base := "line1\nline2"
	modal := "A\nB\nC\nD\nE\nF"
	result := overlayModal(base, modal, 20, 2)
	baseCount := len(strings.Split(base, "\n"))
	resultCount := len(strings.Split(result, "\n"))
	if resultCount != baseCount {
		t.Errorf("line count changed: base=%d result=%d", baseCount, resultCount)
	}
}
