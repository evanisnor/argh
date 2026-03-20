package ui

import (
	"strings"
	"testing"
)

func TestOverlayAbove(t *testing.T) {
	tests := []struct {
		name    string
		base    string
		overlay string
		want    string
	}{
		{
			name:    "empty overlay returns base unchanged",
			base:    "line1\nline2\ncommand bar",
			overlay: "",
			want:    "line1\nline2\ncommand bar",
		},
		{
			name:    "single overlay line replaces line above command bar",
			base:    "panel1\npanel2\ncmdbar",
			overlay: "SUGGESTION",
			want:    "panel1\nSUGGESTION\ncmdbar",
		},
		{
			name:    "two overlay lines replace two lines above command bar",
			base:    "panel1\npanel2\npanel3\ncmdbar",
			overlay: "SUGG1\nSUGG2",
			want:    "panel1\nSUGG1\nSUGG2\ncmdbar",
		},
		{
			name:    "overlay exactly fills all panel lines",
			base:    "panel1\npanel2\ncmdbar",
			overlay: "SUGG1\nSUGG2",
			want:    "SUGG1\nSUGG2\ncmdbar",
		},
		{
			name:    "overlay taller than available lines is clamped to bottom rows",
			base:    "panel1\ncmdbar",
			overlay: "SUGG1\nSUGG2\nSUGG3",
			want:    "SUGG3\ncmdbar",
		},
		{
			name:    "no panel lines available (only command bar) returns base unchanged",
			base:    "cmdbar",
			overlay: "SUGGESTION",
			want:    "cmdbar",
		},
		{
			name:    "base with empty string is unchanged",
			base:    "",
			overlay: "SUGGESTION",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := overlayAbove(tt.base, tt.overlay)
			if got != tt.want {
				t.Errorf("overlayAbove():\n  got  %q\n  want %q", got, tt.want)
			}
		})
	}
}

func TestOverlayAbove_PreservesCommandBarLine(t *testing.T) {
	base := "header\npanel content\ncommand bar line"
	overlay := "suggestion"
	result := overlayAbove(base, overlay)
	lines := strings.Split(result, "\n")
	if lines[len(lines)-1] != "command bar line" {
		t.Errorf("last line (command bar) must be preserved, got: %q", lines[len(lines)-1])
	}
}

func TestOverlayAbove_OverlayDoesNotGrowBase(t *testing.T) {
	base := "a\nb\nc\nd"
	overlay := "X\nY"
	result := overlayAbove(base, overlay)
	baseLinesCount := len(strings.Split(base, "\n"))
	resultLinesCount := len(strings.Split(result, "\n"))
	if resultLinesCount != baseLinesCount {
		t.Errorf("overlayAbove must not change line count: base=%d result=%d", baseLinesCount, resultLinesCount)
	}
}
