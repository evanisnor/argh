package ui

import "strings"

// overlayAbove paints the overlay string on top of the rows immediately above
// the last line of base (the command bar). Each overlay line replaces the
// corresponding panel line from the bottom up.
//
// If the overlay is taller than the available panel lines, only the bottom
// portion of the overlay (the lines closest to the command bar) is shown.
func overlayAbove(base, overlay string) string {
	if overlay == "" {
		return base
	}
	baseLines := strings.Split(base, "\n")
	overlayLines := strings.Split(overlay, "\n")

	// The last line is the command bar; everything above it is available.
	cmdBarIdx := len(baseLines) - 1
	available := cmdBarIdx
	if available <= 0 {
		return base
	}

	// Clamp: if the overlay is taller than the available panel lines, take
	// only the bottom portion (closest to the command bar).
	if len(overlayLines) > available {
		overlayLines = overlayLines[len(overlayLines)-available:]
	}

	startIdx := cmdBarIdx - len(overlayLines)
	for i, line := range overlayLines {
		baseLines[startIdx+i] = line
	}

	return strings.Join(baseLines, "\n")
}
