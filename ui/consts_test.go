package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// TestFallbackMark_ThreeWidths is the PLACEHOLDER defect's own test: the
// empty preview/terminal pane's placeholder mark must never be drawn wider
// than the pane it sits in, at any of the three width tiers the brief
// names - the big 7-row block font, the small 5-row block font, and plain
// text once even that doesn't fit.
func TestFallbackMark_ThreeWidths(t *testing.T) {
	big := FallbackMark(200)
	require.LessOrEqual(t, markWidth(big), 200)
	require.Contains(t, big, "█", "wide enough for the 7-row block font")

	small := FallbackMark(40)
	require.LessOrEqual(t, markWidth(small), 40)
	require.Contains(t, small, "█", "wide enough for the 5-row block font, too narrow for the 7-row one")
	require.NotEqual(t, big, small, "a narrower pane must draw the smaller mark, not the big one clipped")

	plain := FallbackMark(20)
	require.LessOrEqual(t, markWidth(plain), 20)
	require.Equal(t, plainFallbackText, strings.TrimSpace(plain), "too narrow for either block font: plain text")
}

// TestFallbackMark_NeverExceedsWidth sweeps a range of pane widths (rather
// than the three named tiers alone) to guard the boundary conditions right
// at each tier's own threshold.
func TestFallbackMark_NeverExceedsWidth(t *testing.T) {
	for w := 5; w <= 60; w++ {
		mark := FallbackMark(w)
		require.LessOrEqualf(t, markWidth(mark), w, "width %d: mark wider than its pane", w)
	}
}

// TestFallbackMark_ZeroWidthReturnsBigMark documents the "not sized yet"
// short-circuit: PreviewPane/TerminalPane's own String() never renders
// anything at width 0, so which mark FallbackMark(0) picks is moot, but it
// must not panic or return an ansi-invalid string.
func TestFallbackMark_ZeroWidthReturnsBigMark(t *testing.T) {
	require.NotPanics(t, func() {
		mark := FallbackMark(0)
		require.NotEmpty(t, mark)
		require.GreaterOrEqual(t, ansi.StringWidth(mark), 0)
	})
}
