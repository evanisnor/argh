package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// overlayModal renders the modal string as a centered floating overlay on top
// of the base view. Lines outside the modal region are dimmed per-line to
// create a muted background effect while the modal appears in full colour.
//
// When termWidth or termHeight is zero (e.g. before the first WindowSizeMsg),
// the modal is stacked below the base unchanged so tests without a terminal
// size still see the modal content.
func overlayModal(base, modal string, termWidth, termHeight int) string {
	if termWidth == 0 || termHeight == 0 || modal == "" {
		return base + "\n" + modal
	}

	modalHeight := lipgloss.Height(modal)
	topPad := (termHeight - modalHeight) / 2
	if topPad < 0 {
		topPad = 0
	}

	// Dim each base line independently so that per-line replacement of the
	// modal rows does not break the surrounding faint escape sequences.
	dimStyle := lipgloss.NewStyle().Faint(true)
	baseLines := strings.Split(base, "\n")
	for i, l := range baseLines {
		baseLines[i] = dimStyle.Render(l)
	}

	// Place the modal centred within the full terminal area.
	placed := lipgloss.Place(termWidth, termHeight, lipgloss.Center, lipgloss.Center, modal)
	placedLines := strings.Split(placed, "\n")

	// Replace only the rows occupied by the modal box; everything else keeps
	// the dimmed base content.
	result := make([]string, len(baseLines))
	copy(result, baseLines)
	for i := topPad; i < topPad+modalHeight && i < len(result) && i < len(placedLines); i++ {
		result[i] = placedLines[i]
	}

	return strings.Join(result, "\n")
}
