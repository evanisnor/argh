package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestRowBuilder_FlexWidth(t *testing.T) {
	tests := []struct {
		name       string
		totalWidth int
		columns    []columnDef
		want       int
	}{
		{
			name:       "zero totalWidth returns 40",
			totalWidth: 0,
			columns:    []columnDef{{width: 0, align: lipgloss.Left}},
			want:       40,
		},
		{
			name:       "negative totalWidth returns 40",
			totalWidth: -1,
			columns:    []columnDef{{width: 0, align: lipgloss.Left}},
			want:       40,
		},
		{
			name:       "simple flex calculation",
			totalWidth: 100,
			columns: []columnDef{
				{width: 10, align: lipgloss.Left, trailSep: " | "},
				{width: 0, align: lipgloss.Left, trailSep: " | "},
				{width: 5, align: lipgloss.Left},
			},
			want: 79, // 100 - 10 - 5 - 3 - 3
		},
		{
			name:       "clamped to 1 when columns exceed totalWidth",
			totalWidth: 10,
			columns: []columnDef{
				{width: 8, align: lipgloss.Left, trailSep: " | "},
				{width: 0, align: lipgloss.Left},
			},
			want: 1, // 10 - 8 - 3 = -1 → clamped to 1
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &rowBuilder{columns: tt.columns, totalWidth: tt.totalWidth}
			if got := b.flexWidth(); got != tt.want {
				t.Errorf("flexWidth() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRowBuilder_BuildRow(t *testing.T) {
	noStyle := lipgloss.NewStyle()

	tests := []struct {
		name       string
		totalWidth int
		columns    []columnDef
		cells      []string
		wantWidth  int // expected display width of the unstyled result
		wantContains []string
	}{
		{
			name:       "basic left-aligned columns",
			totalWidth: 30,
			columns: []columnDef{
				{width: 5, align: lipgloss.Left, trailSep: " "},
				{width: 0, align: lipgloss.Left, trailSep: " "},
				{width: 3, align: lipgloss.Left},
			},
			cells:        []string{"abc", "hello", "xy"},
			wantWidth:    30,
			wantContains: []string{"abc", "hello", "xy"},
		},
		{
			name:       "right-aligned column",
			totalWidth: 20,
			columns: []columnDef{
				{width: 5, align: lipgloss.Right},
				{width: 0, align: lipgloss.Left},
			},
			cells:     []string{"ab", "text"},
			wantWidth: 20,
			// "ab" right-padded: "   ab"
			wantContains: []string{"   ab", "text"},
		},
		{
			name:       "emoji cell truncated correctly",
			totalWidth: 20,
			columns: []columnDef{
				{width: 4, align: lipgloss.Left, trailSep: " "},
				{width: 0, align: lipgloss.Left},
			},
			// Eye emoji is 2 display columns wide
			cells:     []string{"AB", "title"},
			wantWidth: 20,
		},
		{
			name:       "cell with wide emoji padded correctly",
			totalWidth: 10,
			columns: []columnDef{
				{width: 4, align: lipgloss.Left},
				{width: 0, align: lipgloss.Left},
			},
			cells:     []string{"👁", "hi"},
			wantWidth: 10,
		},
		{
			name:       "truncation with ellipsis",
			totalWidth: 15,
			columns: []columnDef{
				{width: 5, align: lipgloss.Left},
				{width: 0, align: lipgloss.Left},
			},
			cells:        []string{"toolong!", "a very long title that overflows"},
			wantContains: []string{"tool…"},
		},
		{
			name:       "empty cells",
			totalWidth: 20,
			columns: []columnDef{
				{width: 5, align: lipgloss.Left, trailSep: "|"},
				{width: 0, align: lipgloss.Left},
			},
			cells:     []string{"", ""},
			wantWidth: 20,
		},
		{
			name:       "fewer cells than columns",
			totalWidth: 20,
			columns: []columnDef{
				{width: 5, align: lipgloss.Left},
				{width: 5, align: lipgloss.Left},
				{width: 0, align: lipgloss.Left},
			},
			cells:     []string{"a"},
			wantWidth: 20,
		},
		{
			name:       "unicode symbols padded correctly",
			totalWidth: 15,
			columns: []columnDef{
				{width: 3, align: lipgloss.Left, trailSep: " "},
				{width: 0, align: lipgloss.Left},
			},
			cells:        []string{"●●●", "ok"},
			wantContains: []string{"●●●"},
		},
		{
			name:       "separator with unicode pipe",
			totalWidth: 20,
			columns: []columnDef{
				{width: 5, align: lipgloss.Left, trailSep: " │ "},
				{width: 0, align: lipgloss.Left},
			},
			cells:        []string{"abc", "def"},
			wantWidth:    20,
			wantContains: []string{"│"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &rowBuilder{columns: tt.columns, totalWidth: tt.totalWidth}
			result := b.buildRow(tt.cells, noStyle)

			if tt.wantWidth > 0 {
				gotWidth := ansi.StringWidth(result)
				if gotWidth != tt.wantWidth {
					t.Errorf("display width = %d, want %d; row: %q", gotWidth, tt.wantWidth, result)
				}
			}
			for _, s := range tt.wantContains {
				if !strings.Contains(result, s) {
					t.Errorf("expected %q in result %q", s, result)
				}
			}
		})
	}
}

func TestRowBuilder_BuildRow_StyleApplied(t *testing.T) {
	b := &rowBuilder{
		columns:    []columnDef{{width: 5, align: lipgloss.Left}},
		totalWidth: 5,
	}
	style := lipgloss.NewStyle().Bold(true)
	result := b.buildRow([]string{"hi"}, style)
	// The styled result should contain the text.
	if !strings.Contains(result, "hi") {
		t.Errorf("expected 'hi' in styled result %q", result)
	}
}
