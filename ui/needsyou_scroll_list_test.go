// Package ui: the Needs-you list section's own scroll tests (board #295's
// "at the moment only 5 are listed so its difficult for me to get through
// them") - the five-row cap is gone, every ranked row renders, and once
// there is no room for all of them the section scrolls with the cursor
// instead of truncating the list from the bottom.
package ui

import (
	"claude-squad/session/clarity"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// needsYouRows builds n uniquely-titled board-sourced feed rows, numbered
// from 0, in feed order.
func needsYouRows(n int) []clarity.FeedItem {
	items := make([]clarity.FeedItem, n)
	for i := 0; i < n; i++ {
		source := fmt.Sprintf("#%d", 300+i)
		items[i] = clarity.FeedItem{Rank: i + 1, Source: source, Lane: source, Title: fmt.Sprintf("row%d", i)}
	}
	return items
}

// TestNeedsYouList_AllRowsRenderWhenTheyFit is test (a): with room enough
// for all of them, every one of 13 rows renders - no five-row cap.
func TestNeedsYouList_AllRowsRenderWhenTheyFit(t *testing.T) {
	l := newTestList()
	l.SetSize(80, 60)
	l.SetNeedsYou(needsYouRows(13), "")

	out := ansi.Strip(l.String())
	for i := 0; i < 13; i++ {
		require.Containsf(t, out, fmt.Sprintf("row%d", i), "row %d must render", i)
	}
	require.NotContains(t, out, "more", "every row fits - no scroll marker")
}

// TestNeedsYouList_SelectedRowPastVisibleWindowScrollsIntoView is test (b):
// a short section (room for far fewer than 13 rows) still shows whichever
// row the cursor is currently on, however far down the feed it sits.
func TestNeedsYouList_SelectedRowPastVisibleWindowScrollsIntoView(t *testing.T) {
	l := newTestList()
	l.SetSize(80, 12) // short enough that 13 rows cannot all fit
	l.SetNeedsYou(needsYouRows(13), "")

	// Cursor starts on the tracked/external groups (both empty here) - the
	// first Down() lands on needsYou row 0 (List.Down's own empty-group
	// skip), further Down() calls advance one row at a time.
	l.Down()
	out := ansi.Strip(l.String())
	require.Contains(t, out, "row0", "the first row must be visible at the top")

	for i := 1; i < 13; i++ {
		l.Down()
	}
	out = ansi.Strip(l.String())
	require.Contains(t, out, "row12", "the selected row (the last one) must scroll into view")
	require.NotContains(t, out, "row0", "row0 has scrolled out of the now-bottom-anchored window")
}

// TestNeedsYouList_MoreMarkersAppearAndCountCorrectly is test (c): the "…
// N more" marker shows at whichever edge still hides rows, with the exact
// count of rows hidden past that edge.
func TestNeedsYouList_MoreMarkersAppearAndCountCorrectly(t *testing.T) {
	l := newTestList()
	l.SetSize(80, 12)
	l.SetNeedsYou(needsYouRows(13), "")

	l.Down() // -> row0, the top of the feed
	out := ansi.Strip(l.String())
	require.Contains(t, out, "… ", "a marker must appear when rows are hidden")
	require.Contains(t, out, "more", "top-anchored: a bottom marker for the rows still below")

	for i := 1; i < 13; i++ {
		l.Down() // -> row12, the bottom of the feed
	}
	out = ansi.Strip(l.String())
	require.Contains(t, out, "more", "bottom-anchored: a top marker for the rows still above")

	// Walk to the middle so BOTH edges hide rows at once, and check the
	// two counts are exact: hidden-above + visible + hidden-below == 13.
	for i := 0; i < 6; i++ {
		l.Up()
	}
	out = ansi.Strip(l.String())
	visible := 0
	hiddenAbove, hiddenBelow := 0, 0
	for _, line := range strings.Split(out, "\n") {
		for i := 0; i < 13; i++ {
			if strings.Contains(line, fmt.Sprintf("row%d", i)) {
				visible++
			}
		}
		if strings.Contains(line, "more") {
			var n int
			if _, err := fmt.Sscanf(strings.TrimSpace(line), "… %d more", &n); err == nil {
				if hiddenAbove == 0 {
					hiddenAbove = n
				} else {
					hiddenBelow = n
				}
			}
		}
	}
	require.Greater(t, hiddenAbove, 0, "the row is mid-feed - rows above must be hidden")
	require.Greater(t, hiddenBelow, 0, "the row is mid-feed - rows below must be hidden")
	require.Equal(t, 13, hiddenAbove+visible+hiddenBelow, "hidden-above + visible + hidden-below must account for every row")
}

// TestNeedsYouList_At164x45With13Rows_AllReachableByArrowKeys is the
// brief's own acceptance shape, literally: at 164 columns and the content
// height app.go actually gives the list at a 45-row terminal (90% of 45,
// rounded the way updateHandleWindowSizeEvent does), every one of 13 rows
// is reachable by walking the cursor down one row at a time.
func TestNeedsYouList_At164x45With13Rows_AllReachableByArrowKeys(t *testing.T) {
	l := newTestList()
	terminalHeight := 45
	contentHeight := int(float32(terminalHeight) * 0.9)
	l.SetSize(164, contentHeight)
	l.SetNeedsYou(needsYouRows(13), "")

	l.Down() // -> row0
	for i := 0; i < 13; i++ {
		out := ansi.Strip(l.String())
		require.Containsf(t, out, fmt.Sprintf("row%d", i), "row %d must be reachable by the arrow keys", i)
		if i < 12 {
			l.Down()
		}
	}
}
