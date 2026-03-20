package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// columnDef describes a single column in a table row.
type columnDef struct {
	width    int               // fixed display-width; 0 = flex (fills remaining)
	align    lipgloss.Position // lipgloss.Left or lipgloss.Right
	trailSep string           // separator appended after this column ("" for none)
}

// rowBuilder builds fixed-width table rows using display-width-correct padding.
type rowBuilder struct {
	columns    []columnDef
	totalWidth int
}

// flexWidth returns the computed width for the single flex (width=0) column.
// If totalWidth is <= 0, returns 40 as a generous default.
func (b *rowBuilder) flexWidth() int {
	if b.totalWidth <= 0 {
		return 40
	}
	used := 0
	for _, c := range b.columns {
		used += c.width
		used += ansi.StringWidth(c.trailSep)
	}
	w := b.totalWidth - used
	if w < 1 {
		w = 1
	}
	return w
}

// buildRow composes a single row string from cells, truncating and padding each
// cell to its column's display-width, then applies rowStyle to the result.
func (b *rowBuilder) buildRow(cells []string, rowStyle lipgloss.Style) string {
	var sb strings.Builder
	flex := b.flexWidth()

	for i, col := range b.columns {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}

		w := col.width
		if w == 0 {
			w = flex
		}

		// Truncate to column width.
		if ansi.StringWidth(cell) > w {
			cell = ansi.Truncate(cell, w, "…")
		}

		// Pad to exact display-width.
		padding := w - ansi.StringWidth(cell)
		if padding > 0 {
			spaces := strings.Repeat(" ", padding)
			if col.align == lipgloss.Right {
				cell = spaces + cell
			} else {
				cell = cell + spaces
			}
		}

		sb.WriteString(cell)
		sb.WriteString(col.trailSep)
	}

	return rowStyle.Render(sb.String())
}
