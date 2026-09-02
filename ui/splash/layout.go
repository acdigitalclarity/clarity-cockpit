package splash

// The mock-up's own design height (design/splash-80s/OUTRUN-2/OUTRUN2.go,
// buildScreen) is a static image, unconstrained by any real terminal - the
// PNG it produced at 120 columns runs to dozens of rows. A live Bubble Tea
// View() must never exceed the window height it was given (bubbletea
// redraws in place; a taller View() scrolls the terminal and corrupts the
// next frame), so layoutFor adapts the two tuned targets - a ~24-row mark
// at 120 columns, the mock-up's own grid depth - to whatever height the
// window actually reports, filling it exactly rather than overflowing it.
//
// Priority order when height is tight: the grid (background texture, least
// essential) is trimmed to a small but still-visible minimum first; the
// mark keeps as much of its 24-row target as the remaining budget allows.
// The wordmark, fleet counters and maker line are never trimmed - their
// row cost is fixed (2*glyphH+4) and reserved first, so CLARITY/WORKSPACE
// and the counters are always fully on screen once the entrance has
// reached the idle loop, at any window height a real terminal reports.
const (
	bigMarkCols       = 44 // ported from OUTRUN2.go's own 120-col slot
	bigMarkRowsTarget = 24 // the "about 24 rows tall at 120 columns" tuning
	bigGridRowsTarget = 24 // OUTRUN2.go's own 120-col grid depth

	smallMarkCols       = 28 // OUTRUN2.go's own 80-col slot - "as in the mock-up", untuned
	smallMarkRowsTarget = 9
	smallGridRowsTarget = 16

	minGridRows = 4 // the grid must still read as a grid, not vanish
	minMarkRows = 4 // the mark must still read as a tile, not vanish
)

// layoutParams is the per-frame layout layoutFor resolves once from
// (width, height); RenderFrame builds the canvas and every element's
// position from it.
type layoutParams struct {
	big              bool
	width            int
	glyphs           map[rune][]string
	glyphH, tracking int
	markCols         int
	markRows         int
	gridRows         int
}

// layoutFor resolves markRows/gridRows for the given terminal size. height
// <= 0 means "unconstrained" (used by tests that want the full tuned
// target regardless of a real window) and returns the ideal, un-shrunk
// layout.
func layoutFor(width, height int) layoutParams {
	big := width >= 120
	lo := layoutParams{big: big, width: width}

	markRowsTarget, gridRowsTarget := bigMarkRowsTarget, bigGridRowsTarget
	if big {
		lo.glyphs, lo.glyphH, lo.tracking = bigGlyphs, 7, 2
		lo.markCols = bigMarkCols
	} else {
		lo.glyphs, lo.glyphH, lo.tracking = smallGlyphs, 5, 1
		lo.markCols = smallMarkCols
		markRowsTarget, gridRowsTarget = smallMarkRowsTarget, smallGridRowsTarget
	}

	if height <= 0 {
		lo.markRows, lo.gridRows = markRowsTarget, gridRowsTarget
		return lo
	}

	// overhead is the row cost RenderFrame's own layout arithmetic spends
	// below the grid: gapA(1) + a glyphH-tall word line + lineGap(1) +
	// a second glyphH-tall word line + gapB(1) + the maker row(1) itself.
	overhead := 2*lo.glyphH + 4
	budget := maxInt(height-overhead, 1)

	if markRowsTarget+gridRowsTarget <= budget {
		lo.markRows, lo.gridRows = markRowsTarget, gridRowsTarget
		return lo
	}

	gridRows := minGridRows
	markRows := minInt(budget-gridRows, markRowsTarget)
	markRows = maxInt(markRows, minMarkRows)
	gridRows = maxInt(budget-markRows, 0)

	lo.markRows, lo.gridRows = markRows, gridRows
	return lo
}
