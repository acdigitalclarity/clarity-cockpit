package ui

import (
	"claude-squad/session"
	"claude-squad/session/clarity"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func newTestList(titles ...string) *List {
	s := spinner.New()
	l := NewList(&s, false)
	for _, t := range titles {
		inst, _ := session.NewInstance(session.InstanceOptions{
			Title:   t,
			Path:    ".",
			Program: "echo",
		})
		l.AddInstance(inst)
	}
	return l
}

func TestMoveUp(t *testing.T) {
	l := newTestList("a", "b", "c")
	l.SetSelectedInstance(1) // select "b"

	moved := l.MoveUp()
	require.True(t, moved)
	require.Equal(t, 0, l.selectedIdx)
	require.Equal(t, "b", l.items[0].Title)
	require.Equal(t, "a", l.items[1].Title)
	require.Equal(t, "c", l.items[2].Title)
}

func TestMoveUp_AtTop(t *testing.T) {
	l := newTestList("a", "b", "c")
	l.SetSelectedInstance(0)

	moved := l.MoveUp()
	require.False(t, moved)
	require.Equal(t, 0, l.selectedIdx)
	require.Equal(t, "a", l.items[0].Title)
}

func TestMoveDown(t *testing.T) {
	l := newTestList("a", "b", "c")
	l.SetSelectedInstance(1) // select "b"

	moved := l.MoveDown()
	require.True(t, moved)
	require.Equal(t, 2, l.selectedIdx)
	require.Equal(t, "a", l.items[0].Title)
	require.Equal(t, "c", l.items[1].Title)
	require.Equal(t, "b", l.items[2].Title)
}

func TestMoveDown_AtBottom(t *testing.T) {
	l := newTestList("a", "b", "c")
	l.SetSelectedInstance(2)

	moved := l.MoveDown()
	require.False(t, moved)
	require.Equal(t, 2, l.selectedIdx)
	require.Equal(t, "c", l.items[2].Title)
}

func TestMoveWithSingleItem(t *testing.T) {
	l := newTestList("only")
	l.SetSelectedInstance(0)

	require.False(t, l.MoveUp())
	require.False(t, l.MoveDown())
}

// highlightedRowTitle returns which of titles' own row currently carries
// the render's own selection marker ("▌", laneRowMarker) - parsed straight
// out of the STRIPPED render text, never off an index, so this is
// independent proof of "which row the screen visibly highlights" rather
// than a restatement of l.selectedIdx.
func highlightedRowTitle(t *testing.T, render string, titles []string) string {
	t.Helper()
	for _, line := range strings.Split(ansi.Strip(render), "\n") {
		if !strings.HasPrefix(line, "▌") {
			continue
		}
		for _, title := range titles {
			if strings.Contains(line, title) {
				return title
			}
		}
	}
	return ""
}

// drawnRowOrder returns titles in the exact top-to-bottom order their own
// rows appear in render - parsed the same way highlightedRowTitle is, but
// over every row rather than just the highlighted one, so this is the
// render's OWN ground truth for what order the screen actually draws,
// independent of groupLanesByModality's return value or any store index.
func drawnRowOrder(t *testing.T, render string, titles []string) []string {
	t.Helper()
	var order []string
	for _, line := range strings.Split(ansi.Strip(render), "\n") {
		for _, title := range titles {
			if strings.Contains(line, title) {
				order = append(order, title)
				break
			}
		}
	}
	return order
}

// TestDown_WalksDrawnOrder is board #315's own root-cause proof:
// groupLanesByModality/String() draw named modality groups above the
// no-modality catch-all, but before this fix Down/Up walked l.items in
// plain STORE order. This fixture stores the catch-all rows FIRST and the
// grouped ones LAST - exactly the owner's own fleet shape (four ungrouped
// lanes, then build-night/p2p-supply-chain, the two that picked up a
// modality) - so the drawn order and the store order actively disagree. A
// lone external lane with no modality is present purely so an old, broken
// Down() that falls off the end of raw store order lands somewhere
// OBSERVABLY wrong (selExternal=true, GetSelectedInstance nil) instead of
// coincidentally wrapping back to store index 0 and looking correct by
// chance - a bare two-bucket cycle is a rotation of itself either way.
// Selection starts on build-night, exactly where AddInstance+
// SetSelectedInstance(NumInstances()-1) lands it right after the wizard's
// own finishing enter (app.go's newLaneStartedMsg handler) when build-night
// was the just-created, last-appended lane.
func TestDown_WalksDrawnOrder(t *testing.T) {
	l := newTestList()
	l.SetSize(frontdoor5ListWidth164, 45)
	u1 := frontdoor5Instance(t, "row-u1", "ta", "", 10, clarity.StateIdle, frontdoor5Time(9, 0))
	u2 := frontdoor5Instance(t, "row-u2", "ta", "", 10, clarity.StateIdle, frontdoor5Time(9, 1))
	u3 := frontdoor5Instance(t, "row-u3", "ta", "", 10, clarity.StateIdle, frontdoor5Time(9, 2))
	u4 := frontdoor5Instance(t, "row-u4", "ta", "", 10, clarity.StateIdle, frontdoor5Time(9, 3))
	buildNight := frontdoor5Instance(t, "row-build-night", "ta", "enhancement", 10, clarity.StateIdle, frontdoor5Time(9, 4))
	p2p := frontdoor5Instance(t, "row-p2p", "ta", "project", 10, clarity.StateIdle, frontdoor5Time(9, 5))
	l.AddInstance(u1)
	l.AddInstance(u2)
	l.AddInstance(u3)
	l.AddInstance(u4)
	l.AddInstance(buildNight)
	l.AddInstance(p2p)
	l.SetExternal([]clarity.ExternalLane{
		frontdoor5External("ext-lane", "ta", clarity.SeatSourceDeclared, "", 5, clarity.StateIdle, frontdoor5Time(9, 6)),
	})

	titles := []string{"row-u1", "row-u2", "row-u3", "row-u4", "row-build-night", "row-p2p"}
	drawnOrder := drawnRowOrder(t, l.String(), titles)
	require.Equal(t, []string{"row-build-night", "row-p2p", "row-u1", "row-u2", "row-u3", "row-u4"}, drawnOrder,
		"named modality groups draw above the no-modality catch-all")

	l.SelectInstance(buildNight)

	for step, wantTitle := range drawnOrder {
		render := l.String()
		gotSelected := l.GetSelectedInstance()
		require.NotNilf(t, gotSelected, "step %d: selection must stay on a tracked row, never fall to external", step)
		gotHighlighted := highlightedRowTitle(t, render, titles)
		require.Equal(t, gotSelected.Title, gotHighlighted, "step %d: selection and the highlighted row must always agree", step)
		require.Equal(t, wantTitle, gotSelected.Title, "step %d: Down must walk the render's OWN drawn order, not raw store order", step)
		require.Equal(t, RowKindTracked, l.SelectedRowKind(), "step %d: must never fall into the external group mid-walk", step)
		if step < len(drawnOrder)-1 {
			l.Down()
		}
	}
}
