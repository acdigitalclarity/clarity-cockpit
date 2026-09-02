package splash

import "strings"

// Wordmark renders s in the splash's 7-row block font as plain text rows
// (no colour), one tracking column between letters. Letters outside the
// font render as a blank cell. Used by ui.FallBackText so the empty-pane
// placeholder shows the same CLARITY / WORKSPACE letterforms as the splash.
func Wordmark(s string) string {
	const tracking = 1
	rows := len(bigGlyphs[' '])
	lines := make([]strings.Builder, rows)
	for i, r := range s {
		g, ok := bigGlyphs[r]
		if !ok {
			g = bigGlyphs[' ']
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
