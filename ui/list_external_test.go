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
