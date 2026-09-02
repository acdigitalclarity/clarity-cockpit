package ui

import (
	"claude-squad/ui/splash"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// plainFallbackText is the last-resort placeholder for a pane too narrow
// for even the small block font.
const plainFallbackText = "CLARITY WORKSPACE"

// markWidth returns the widest rendered line of a (possibly multi-line)
// block-font mark, ansi-aware (the mark itself carries no escape codes, but
// this stays correct if that ever changes).
func markWidth(mark string) int {
	width := 0
	for _, line := range strings.Split(mark, "\n") {
		if w := ansi.StringWidth(line); w > width {
			width = w
		}
	}
	return width
}

// FallbackMark returns the CLARITY / WORKSPACE placeholder mark sized to
// fit within innerWidth: the splash's 7-row block font when it fits, the
// 5-row small font when the big one doesn't, and plain text as the last
// resort - so an empty preview/terminal pane never draws a mark wider than
// the pane itself (the PLACEHOLDER defect: ui.FallBackText used to be a
// fixed 7-row/53-column block regardless of how narrow the pane actually
// was, so lipgloss's own word-wrap sliced through the block glyphs instead
// of falling back to a smaller mark). innerWidth <= 0 means "not sized yet"
// and returns the big mark, matching String()'s own width==0 short-circuit
// that never renders it anyway.
func FallbackMark(innerWidth int) string {
	big := lipgloss.JoinVertical(lipgloss.Center, splash.Wordmark("CLARITY"), "", splash.Wordmark("WORKSPACE"))
	if innerWidth <= 0 || markWidth(big) <= innerWidth {
		return big
	}
	small := lipgloss.JoinVertical(lipgloss.Center, splash.SmallWordmark("CLARITY"), "", splash.SmallWordmark("WORKSPACE"))
	if markWidth(small) <= innerWidth {
		return small
	}
	return ansi.Truncate(plainFallbackText, innerWidth, "")
}
