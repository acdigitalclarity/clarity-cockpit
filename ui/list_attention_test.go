// Package ui: item 2 of COCKPIT-MODALITIES-2026-09-03.md (cockpit pane
// slice 17) - lane rows sort by attention (waiting on you, stalled,
// working, idle; ties by last turn newest first) within their own modality
// group, and Down/Up walk that same sorted, drawn order for both tracked
// and external rows. Kept in its own file, the same convention
// list_frontdoor5_test.go/list_row_band_test.go already follow.
package ui

import (
	"claude-squad/session"
	"claude-squad/session/clarity"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGroupLanesByModality_SortsByAttentionWithinGroup is item 2's own
// core rank proof: four tracked rows in one no-modality group, added in an
// order that disagrees with the attention rank entirely - the sorted
// itemIdx must read waiting, stalled, working, idle regardless of store
// order.
func TestGroupLanesByModality_SortsByAttentionWithinGroup(t *testing.T) {
	idle := frontdoor5Instance(t, "row-idle", "ta", "", 10, clarity.StateIdle, frontdoor5Time(9, 0))
	working := frontdoor5Instance(t, "row-working", "ta", "", 10, clarity.StateWorking, frontdoor5Time(9, 1))
	waiting := frontdoor5Instance(t, "row-waiting", "ta", "", 10, clarity.StateWaitingYou, frontdoor5Time(9, 2))
	stalled := frontdoor5Instance(t, "row-stalled", "ta", "", 10, clarity.StateStalled, frontdoor5Time(9, 3))
	items := []*session.Instance{idle, working, waiting, stalled}

	groups := groupLanesByModality(items, nil)
	require.Len(t, groups, 1, "no modality declared: one trailing catch-all group")
	got := make([]string, len(groups[0].itemIdx))
	for i, idx := range groups[0].itemIdx {
		got[i] = items[idx].Title
	}
	require.Equal(t, []string{"row-waiting", "row-stalled", "row-working", "row-idle"}, got,
		"waiting on you, stalled, working, idle - the brief's own order, independent of store order")
}

// TestGroupLanesByModality_TiesBreakByLastTurnNewestFirst pins the
// tiebreak rule alone: three rows sharing one state, sorted purely by last
// turn, newest first.
func TestGroupLanesByModality_TiesBreakByLastTurnNewestFirst(t *testing.T) {
	oldest := frontdoor5Instance(t, "row-oldest", "ta", "", 10, clarity.StateWorking, frontdoor5Time(9, 0))
	middle := frontdoor5Instance(t, "row-middle", "ta", "", 10, clarity.StateWorking, frontdoor5Time(9, 5))
	newest := frontdoor5Instance(t, "row-newest", "ta", "", 10, clarity.StateWorking, frontdoor5Time(9, 10))
	items := []*session.Instance{oldest, middle, newest}

	groups := groupLanesByModality(items, nil)
	got := make([]string, len(groups[0].itemIdx))
	for i, idx := range groups[0].itemIdx {
		got[i] = items[idx].Title
	}
	require.Equal(t, []string{"row-newest", "row-middle", "row-oldest"}, got)
}

// TestGroupLanesByModality_NeedsKeyRanksAheadOfWaiting proves the "ahead of
// every other word" convention the glyph already carries (laneStateNeedsKeyStyle)
// extends to the sort: a needs-a-key row outranks a plain waiting-on-you row.
func TestGroupLanesByModality_NeedsKeyRanksAheadOfWaiting(t *testing.T) {
	waiting := frontdoor5Instance(t, "row-waiting", "ta", "", 10, clarity.StateWaitingYou, frontdoor5Time(9, 0))
	needsKey := frontdoor5Instance(t, "row-needs-key", "ta", "", 10, clarity.StateWorking, frontdoor5Time(9, 1))
	needsKey.SetNeedsKey(true)
	items := []*session.Instance{waiting, needsKey}

	groups := groupLanesByModality(items, nil)
	require.Equal(t, "row-needs-key", items[groups[0].itemIdx[0]].Title)
	require.Equal(t, "row-waiting", items[groups[0].itemIdx[1]].Title)
}

// TestGroupLanesByModality_SortsExternalRowsToo proves external lanes are
// sorted by the exact same rule, independent of tracked rows sharing the
// group.
func TestGroupLanesByModality_SortsExternalRowsToo(t *testing.T) {
	external := []clarity.ExternalLane{
		frontdoor5External("ext-idle", "ta", clarity.SeatSourceDeclared, "", 5, clarity.StateIdle, frontdoor5Time(9, 0)),
		frontdoor5External("ext-waiting", "ta", clarity.SeatSourceDeclared, "", 5, clarity.StateWaitingYou, frontdoor5Time(9, 1)),
		frontdoor5External("ext-working", "ta", clarity.SeatSourceDeclared, "", 5, clarity.StateWorking, frontdoor5Time(9, 2)),
	}
	groups := groupLanesByModality(nil, external)
	got := make([]string, len(groups[0].externalIdx))
	for i, idx := range groups[0].externalIdx {
		got[i] = external[idx].Name
	}
	require.Equal(t, []string{"ext-waiting", "ext-working", "ext-idle"}, got)
}

// TestDown_WalksAttentionSortedExternalOrder is item 2's Down/Up proof for
// external rows - drawnExternalOrder's own new capability. Two modality
// groups, external rows split across both, deliberately added in an order
// that disagrees with BOTH the group order and the attention order, so a
// walk that used raw store order or ignored attention would land wrong.
func TestDown_WalksAttentionSortedExternalOrder(t *testing.T) {
	l := newTestList()
	l.SetSize(frontdoor5ListWidth164, 45)
	// tracked rows anchor the two named groups' own first-seen order.
	l.AddInstance(frontdoor5Instance(t, "row-a", "ta", "bid", 10, clarity.StateIdle, frontdoor5Time(9, 0)))
	l.AddInstance(frontdoor5Instance(t, "row-b", "ta", "project", 10, clarity.StateIdle, frontdoor5Time(9, 1)))
	l.SetExternal([]clarity.ExternalLane{
		// project group: idle then waiting - waiting must draw/walk FIRST.
		frontdoor5External("ext-project-idle", "ta", clarity.SeatSourceDeclared, "project", 5, clarity.StateIdle, frontdoor5Time(9, 10)),
		frontdoor5External("ext-project-waiting", "ta", clarity.SeatSourceDeclared, "project", 5, clarity.StateWaitingYou, frontdoor5Time(9, 11)),
		// bid group: one row only.
		frontdoor5External("ext-bid", "ta", clarity.SeatSourceDeclared, "bid", 5, clarity.StateWorking, frontdoor5Time(9, 12)),
	})

	want := []string{"ext-bid", "ext-project-waiting", "ext-project-idle"}
	got := l.drawnExternalOrder()
	names := make([]string, len(got))
	for i, idx := range got {
		names[i] = l.external[idx].Name
	}
	require.Equal(t, want, names, "bid group first, then project group attention-sorted within it")

	// Select the first tracked row, then Down past both tracked rows into
	// external, and confirm the walk visits exactly this order.
	l.SetSelectedInstance(0)
	l.Down() // -> row-b (tracked)
	l.Down() // -> first external row
	for i, wantName := range want {
		ext, ok := l.GetSelectedExternalLane()
		require.Truef(t, ok, "step %d: selection must be on an external row", i)
		require.Equal(t, wantName, ext.Name, "step %d", i)
		require.Equal(t, RowKindExternal, l.SelectedRowKind())
		if i < len(want)-1 {
			l.Down()
		}
	}
}

// TestList_CursorFollowsLaneAcrossAttentionReorder is the brief's own "the
// cursor follows the row it was on when the order changes (track by lane
// key, not index)": select a lane, change ITS OWN state so the attention
// sort moves its row, and confirm the highlight and GetSelectedInstance
// still name the same lane.
func TestList_CursorFollowsLaneAcrossAttentionReorder(t *testing.T) {
	l := newTestList()
	l.SetSize(frontdoor5ListWidth164, 45)
	// b is added FIRST (store index 0) and a SECOND (store index 1), both
	// idle with the SAME last-turn time - a genuine tie, so store/insertion
	// order (b, then a) is what draws today whether or not attention
	// sorting is wired in. Selecting a therefore starts on the SECOND row,
	// discriminating the test: only a working sort moves it to the first.
	tie := frontdoor5Time(9, 0)
	b := frontdoor5Instance(t, "row-b", "ta", "", 10, clarity.StateIdle, tie)
	a := frontdoor5Instance(t, "row-a", "ta", "", 10, clarity.StateIdle, tie)
	l.AddInstance(b)
	l.AddInstance(a)

	require.Equal(t, []string{"row-b", "row-a"}, drawnRowOrder(t, l.String(), []string{"row-a", "row-b"}),
		"test setup: row-b must draw first (store order, tied attention)")
	l.SelectInstance(a)
	require.Equal(t, "row-a", highlightedRowTitle(t, l.String(), []string{"row-a", "row-b"}))

	// Flip row-a to waiting on you - it must now sort ABOVE row-b, whose
	// row visibly moves out from under nothing while a's own highlight
	// follows the lane, not the index.
	a.SetLaneState(clarity.StateWaitingYou, frontdoor5Time(9, 5), true)

	require.Equal(t, []string{"row-a", "row-b"}, drawnRowOrder(t, l.String(), []string{"row-a", "row-b"}),
		"row-a must now draw ABOVE row-b (waiting outranks idle)")
	require.Equal(t, "row-a", highlightedRowTitle(t, l.String(), []string{"row-a", "row-b"}),
		"the highlight must still be on row-a after its own row moved")
	require.Same(t, a, l.GetSelectedInstance())
}
