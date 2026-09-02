package ui

import (
	"claude-squad/session/clarity"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testExternalLanes(names ...string) []clarity.ExternalLane {
	out := make([]clarity.ExternalLane, len(names))
	for i, n := range names {
		out[i] = clarity.ExternalLane{Name: n, LastWrite: time.Now()}
	}
	return out
}

func TestDown_CrossesFromItemsIntoExternalAndWraps(t *testing.T) {
	l := newTestList("a", "b")
	l.SetExternal(testExternalLanes("x", "y"))

	require.Equal(t, 0, l.selectedIdx)
	require.False(t, l.selExternal)

	l.Down() // -> "b"
	require.Equal(t, 1, l.selectedIdx)
	require.False(t, l.selExternal)

	l.Down() // -> "x" (crosses into external)
	require.Equal(t, 0, l.selectedIdx)
	require.True(t, l.selExternal)

	l.Down() // -> "y"
	require.Equal(t, 1, l.selectedIdx)
	require.True(t, l.selExternal)

	l.Down() // wraps back to "a"
	require.Equal(t, 0, l.selectedIdx)
	require.False(t, l.selExternal)
}

func TestUp_CrossesFromExternalIntoItemsAndWraps(t *testing.T) {
	l := newTestList("a", "b")
	l.SetExternal(testExternalLanes("x", "y"))

	l.Up() // from "a" wraps up into external's last row "y"
	require.Equal(t, 1, l.selectedIdx)
	require.True(t, l.selExternal)

	l.Up() // -> "x"
	require.Equal(t, 0, l.selectedIdx)
	require.True(t, l.selExternal)

	l.Up() // crosses back into items, landing on "b" (the last item)
	require.Equal(t, 1, l.selectedIdx)
	require.False(t, l.selExternal)
}

func TestGetSelectedInstance_NilWhenExternalSelected(t *testing.T) {
	l := newTestList("a")
	l.SetExternal(testExternalLanes("x"))
	l.Down() // -> external "x"

	require.Nil(t, l.GetSelectedInstance(), "an external row is never a tracked instance")
}

func TestSelectedMsgTarget_TrackedInstance(t *testing.T) {
	l := newTestList("a", "b")
	l.SetSelectedInstance(1)

	lane, isExternal, ok := l.SelectedMsgTarget()
	require.True(t, ok)
	require.False(t, isExternal)
	require.Equal(t, "b", lane)
}

func TestSelectedMsgTarget_ExternalRow(t *testing.T) {
	l := newTestList("a")
	l.SetExternal(testExternalLanes("ways-of-working"))
	l.Down() // -> external row

	lane, isExternal, ok := l.SelectedMsgTarget()
	require.True(t, ok)
	require.True(t, isExternal)
	require.Equal(t, "ways-of-working", lane)
}

func TestSelectedMsgTarget_EmptyList(t *testing.T) {
	l := newTestList()

	_, _, ok := l.SelectedMsgTarget()
	require.False(t, ok)
}

func TestKill_NoOpOnExternalRow(t *testing.T) {
	l := newTestList("a")
	l.SetExternal(testExternalLanes("x"))
	l.Down() // -> external "x"

	l.Kill()

	require.Len(t, l.items, 1, "killing while an external row is selected must not touch the tracked instances")
	require.True(t, l.selExternal, "the external row must still be selected - Kill() must be a genuine no-op")
}

func TestAttach_ErrorsOnExternalRow(t *testing.T) {
	l := newTestList("a")
	l.SetExternal(testExternalLanes("x"))
	l.Down() // -> external "x"

	_, err := l.Attach()
	require.Error(t, err)
}

func TestSetExternal_ClampsSelectionWhenListShrinks(t *testing.T) {
	l := newTestList("a")
	l.SetExternal(testExternalLanes("x", "y"))
	l.Down()
	l.Down() // selectedIdx=1 (external "y")
	require.True(t, l.selExternal)
	require.Equal(t, 1, l.selectedIdx)

	l.SetExternal(testExternalLanes("x")) // shrinks to one row
	require.Equal(t, 0, l.selectedIdx)
	require.True(t, l.selExternal)

	l.SetExternal(nil) // empties out entirely
	require.False(t, l.selExternal)
	require.Equal(t, 0, l.selectedIdx)
}

func TestString_RendersExternalSection(t *testing.T) {
	l := newTestList("a")
	l.SetSize(80, 40)
	l.SetExternal(testExternalLanes("ways-of-working"))

	out := l.String()
	require.Contains(t, out, "External lanes")
	require.Contains(t, out, "ways-of-working")
}

// --- Needs-you rows join the cursor (slice 5) ---------------------------

func testNeedsYouItems(lanes ...string) []clarity.FeedItem {
	out := make([]clarity.FeedItem, len(lanes))
	for i, lane := range lanes {
		out[i] = clarity.FeedItem{Lane: lane, Title: "title for " + lane}
	}
	return out
}

// TestDown_VisitsNeedsYouFirstThenItemsThenExternal is the brief's own
// cursor order, pinned across all three groups at once: needsYou, then
// items, then external. A freshly built List's own zero-value cursor
// starts on the tracked-instance group (unchanged from before slice 5,
// same default TestDown_CrossesFromItemsIntoExternalAndWraps already
// pins) - Down from there visits external next, then wraps around to
// needsYou, matching PROOF (a)'s own "send Down until the first Needs-you
// row is selected" starting from that same default.
func TestDown_VisitsNeedsYouFirstThenItemsThenExternal(t *testing.T) {
	l := newTestList("a")
	l.SetNeedsYou(testNeedsYouItems("#277", "#244"), "")
	l.SetExternal(testExternalLanes("x"))

	require.Equal(t, RowKindTracked, l.SelectedRowKind(), "a fresh list's cursor still starts on the tracked group")

	l.Down() // crosses into external "x"
	require.Equal(t, RowKindExternal, l.SelectedRowKind())
	require.Equal(t, 0, l.selectedIdx)

	l.Down() // wraps into the first Needs-you row
	require.Equal(t, RowKindNeedsYou, l.SelectedRowKind())
	require.Equal(t, 0, l.selectedIdx)

	l.Down() // -> "#244"
	require.Equal(t, RowKindNeedsYou, l.SelectedRowKind())
	require.Equal(t, 1, l.selectedIdx)

	l.Down() // wraps back to the tracked instance "a"
	require.Equal(t, RowKindTracked, l.SelectedRowKind())
	require.Equal(t, 0, l.selectedIdx)
}

// TestUp_FromDefaultWrapsToLastNeedsYouRow mirrors the same three-group
// cycle in reverse from the default tracked-group position.
func TestUp_FromDefaultWrapsToLastNeedsYouRow(t *testing.T) {
	l := newTestList("a")
	l.SetNeedsYou(testNeedsYouItems("#277", "#244"), "")
	l.SetExternal(testExternalLanes("x", "y"))

	l.Up() // from the default tracked row, wraps to the LAST Needs-you row
	require.Equal(t, RowKindNeedsYou, l.SelectedRowKind())
	require.Equal(t, 1, l.selectedIdx)
}

// TestDown_SkipsEmptyNeedsYouGroup is the same-behaviour-when-absent
// guarantee: with no Needs-you rows the cursor cycles items<->external
// exactly as it did before slice 5.
func TestDown_SkipsEmptyNeedsYouGroup(t *testing.T) {
	l := newTestList("a", "b")
	l.SetExternal(testExternalLanes("x", "y"))

	l.Down()
	l.Down() // crosses into external "x"
	require.Equal(t, RowKindExternal, l.SelectedRowKind())
	require.Equal(t, 0, l.selectedIdx)
}

func TestGetSelectedNeedsYou_ReturnsItemWhenSelected(t *testing.T) {
	l := newTestList("a")
	l.SetNeedsYou(testNeedsYouItems("#277"), "")
	l.setGroup(0, 0) // move the cursor onto the sole Needs-you row

	item, ok := l.GetSelectedNeedsYou()
	require.True(t, ok)
	require.Equal(t, "#277", item.Lane)

	_, ok = l.GetSelectedExternalLane()
	require.False(t, ok, "a Needs-you selection is never also an external one")
	require.Nil(t, l.GetSelectedInstance(), "a Needs-you selection is never also a tracked instance")
}

func TestGetSelectedNeedsYou_FalseWhenTrackedSelected(t *testing.T) {
	l := newTestList("a")
	_, ok := l.GetSelectedNeedsYou()
	require.False(t, ok)
}

func TestSelectedMsgTarget_FalseOnNeedsYouRow(t *testing.T) {
	l := newTestList("a")
	l.SetNeedsYou(testNeedsYouItems("#277"), "")
	l.setGroup(0, 0)

	_, _, ok := l.SelectedMsgTarget()
	require.False(t, ok, "a Needs-you row's target is resolved via GetSelectedNeedsYou, not SelectedMsgTarget")
}

func TestSetNeedsYou_ClampsSelectionWhenListShrinks(t *testing.T) {
	l := newTestList("a")
	l.SetNeedsYou(testNeedsYouItems("#1", "#2"), "")
	l.setGroup(0, 1) // second Needs-you row
	require.True(t, l.selNeedsYou)
	require.Equal(t, 1, l.selectedIdx)

	l.SetNeedsYou(testNeedsYouItems("#1"), "") // shrinks to one row
	require.Equal(t, 0, l.selectedIdx)
	require.True(t, l.selNeedsYou)

	l.SetNeedsYou(nil, "feed: queue is empty") // empties out entirely
	require.False(t, l.selNeedsYou)
	require.Equal(t, 0, l.selectedIdx)
}

func TestSetSelectedInstance_ClearsNeedsYouSelection(t *testing.T) {
	l := newTestList("a", "b")
	l.SetNeedsYou(testNeedsYouItems("#277"), "")
	l.setGroup(0, 0)
	require.True(t, l.selNeedsYou)

	l.SetSelectedInstance(1)
	require.False(t, l.selNeedsYou)
	require.Equal(t, "b", l.GetSelectedInstance().Title)
}

func TestString_RendersNeedsYouRowsAndHighlightsSelected(t *testing.T) {
	l := newTestList("a")
	l.SetSize(80, 40)
	l.SetNeedsYou(testNeedsYouItems("#277", "#244"), "")

	out := l.String()
	require.Contains(t, out, "Needs you")
	require.Contains(t, out, "#277 - title for #277")
	require.Contains(t, out, "#244 - title for #244")
}

func TestString_RendersNeedsYouStatusLineWhenNoRows(t *testing.T) {
	l := newTestList("a")
	l.SetSize(80, 40)
	l.SetNeedsYou(nil, "feed: UNCONSTRUCTED - no queue at /tmp/no-such-file")

	out := l.String()
	require.Contains(t, out, "Needs you")
	require.Contains(t, out, "feed: UNCONSTRUCTED - no queue at /tmp/no-such-file")
}
