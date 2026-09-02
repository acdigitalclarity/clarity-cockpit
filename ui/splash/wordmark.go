package splash

import "strings"

// Wordmark renders s in the splash's 7-row block font as plain text rows
// (no colour), one tracking column between letters. Letters outside the
// font render as a blank cell. Used by ui.FallbackMark so the empty-pane
// placeholder shows the same CLARITY / WORKSPACE letterforms as the splash.
func Wordmark(s string) string {
	return renderMark(s, bigGlyphs, 1)
}

// SmallWordmark renders s in the splash's 5-row small font (the same font
// layoutFor switches to below the big mark's 120-column threshold) - used by
// ui.FallbackMark when a preview/terminal pane's inner width is too narrow
// for the 7-row Wordmark but still wide enough to show letterforms (the
// PLACEHOLDER defect: an empty pane must never draw a mark wider than
// itself).
func SmallWordmark(s string) string {
	return renderMark(s, smallGlyphs, 1)
}

// renderMark is the shared block-font renderer behind Wordmark and
// SmallWordmark: same tracking/blank-cell/replacement rules, parameterised
// only by which glyph set to draw from.
func renderMark(s string, glyphs map[rune][]string, tracking int) string {
	rows := len(glyphs[' '])
	lines := make([]strings.Builder, rows)
	for i, r := range s {
		g, ok := glyphs[r]
		if !ok {
			g = glyphs[' ']
		}
		for row := 0; row < rows; row++ {
			if i > 0 {
				lines[row].WriteString(strings.Repeat(" ", tracking))
			}
			cells := g[row]
			lines[row].WriteString(strings.NewReplacer("#", "█", ".", " ").Replace(cells))
		}
	}
	out := make([]string, rows)
	for row := range lines {
		out[row] = lines[row].String()
	}
	return strings.Join(out, "\n")
}
