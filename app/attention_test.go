// Package app: item 3 of COCKPIT-MODALITIES-2026-09-03.md (cockpit pane
// slice 17) - the fleet-wide bell/title tracker in app/attention.go.
package app

import (
	"claude-squad/session"
	"claude-squad/session/clarity"
	"claude-squad/ui"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	"github.com/stretchr/testify/require"
)

// attentionTestInstance builds a tracked instance carrying a resolved
// lane-state reading, the same minimal shape ui package's
// frontdoor5Instance uses (this package cannot import ui's own test
// helpers, so it is duplicated here rather than exported across a
// package boundary purely for a test).
func attentionTestInstance(t *testing.T, title, state string) *session.Instance {
	t.Helper()
	inst, err := session.NewInstance(session.InstanceOptions{
		Title:   title,
		Path:    ".",
		Program: "echo",
	})
	require.NoError(t, err)
	inst.SetLaneState(state, time.Now(), true)
	return inst
}

func attentionTestExternal(name, state string) clarity.ExternalLane {
	return clarity.ExternalLane{Name: name, State: state, StateOK: true, LastTurn: time.Now()}
}

// TestUpdateAttention_EdgeRingsBell_LevelDoesNot is item 3's own core case:
// a lane crossing INTO waiting on you rings the bell exactly once (a
// non-nil tea.Raw Cmd); the SAME lane still waiting on the next tick (a
// level, not an edge) rings nothing.
func TestUpdateAttention_EdgeRingsBell_LevelDoesNot(t *testing.T) {
	h := newComposerTestHome(t)
	inst := attentionTestInstance(t, "lane-a", clarity.StateWorking)
	h.list.AddInstance(inst)()

	now := time.Now()
	cmd := h.updateAttention(now)
	require.Nil(t, cmd, "working is not an attention state: no bell on the first tick")
	require.Equal(t, attentionDefaultTitle, h.windowTitle)

	inst.SetLaneState(clarity.StateWaitingYou, now, true)
	now = now.Add(20 * time.Second) // past the cooldown from a (non-existent) previous bell
	cmd = h.updateAttention(now)
	require.NotNil(t, cmd, "the rising edge into waiting on you must ring the bell")
	require.Contains(t, h.windowTitle, "1 need you")

	// Same tick's worth of state again, unchanged - a LEVEL, must stay silent.
	now = now.Add(20 * time.Second)
	cmd = h.updateAttention(now)
	require.Nil(t, cmd, "an unchanged level must never ring again")
	require.Contains(t, h.windowTitle, "1 need you", "the title still reports it, silently")
}

// TestUpdateAttention_CooldownSuppressesSecondBellWithinTenSeconds proves
// the fleet-wide "at most one bell per 10 seconds" cap: a second lane
// crossing into attention 3 seconds after the first must not ring again.
func TestUpdateAttention_CooldownSuppressesSecondBellWithinTenSeconds(t *testing.T) {
	h := newComposerTestHome(t)
	a := attentionTestInstance(t, "lane-a", clarity.StateWorking)
	b := attentionTestInstance(t, "lane-b", clarity.StateWorking)
	h.list.AddInstance(a)()
	h.list.AddInstance(b)()

	now := time.Now()
	require.Nil(t, h.updateAttention(now))

	a.SetLaneState(clarity.StateWaitingYou, now, true)
	now = now.Add(time.Second)
	require.NotNil(t, h.updateAttention(now), "lane-a's own crossing must ring")

	b.SetLaneState(clarity.StateStalled, now, true)
	now = now.Add(3 * time.Second) // well under the 10s cooldown
	require.Nil(t, h.updateAttention(now), "lane-b's crossing lands inside the cooldown: must stay silent")
	require.Contains(t, h.windowTitle, "2 need you", "the title still counts both, even though the second one rang no bell")

	// Past the cooldown, a THIRD lane crossing must ring again.
	c := attentionTestInstance(t, "lane-c", clarity.StateWorking)
	h.list.AddInstance(c)()
	require.Nil(t, h.updateAttention(now), "lane-c starts working: no crossing yet, no ring")
	c.SetLaneState(clarity.StateStalled, now, true)
	now = now.Add(8 * time.Second) // 11s after the lane-a ring: past the 10s cooldown
	require.NotNil(t, h.updateAttention(now), "a crossing past the cooldown must ring again")
}

// TestUpdateAttention_TitleZeroWhenNothingNeedsHim proves the default title
// with no tracked or external lane in an attention state at all.
func TestUpdateAttention_TitleZeroWhenNothingNeedsHim(t *testing.T) {
	h := newComposerTestHome(t)
	h.list.AddInstance(attentionTestInstance(t, "lane-a", clarity.StateWorking))()
	h.list.SetExternal([]clarity.ExternalLane{attentionTestExternal("ext-a", clarity.StateIdle)})

	cmd := h.updateAttention(time.Now())
	require.Nil(t, cmd)
	require.Equal(t, attentionDefaultTitle, h.windowTitle)
}

// TestUpdateAttention_NeedsKeyCountsOnceNeverDoublesWaiting proves the
// mutual-exclusivity rule (attentionCategory's own doc comment): a lane
// that is BOTH classifier-waiting and sampled needs-a-key counts toward N
// exactly once, as needs-a-key, never twice.
func TestUpdateAttention_NeedsKeyCountsOnceNeverDoublesWaiting(t *testing.T) {
	h := newComposerTestHome(t)
	inst := attentionTestInstance(t, "lane-a", clarity.StateWaitingYou)
	inst.SetNeedsKey(true)
	h.list.AddInstance(inst)()

	h.updateAttention(time.Now())
	require.Contains(t, h.windowTitle, "1 need you")
}

// TestUpdateAttention_ExternalCrossingRingsToo proves the edge trigger
// covers external lanes exactly the same way as tracked ones.
func TestUpdateAttention_ExternalCrossingRingsToo(t *testing.T) {
	h := newComposerTestHome(t)
	lanes := []clarity.ExternalLane{attentionTestExternal("ext-a", clarity.StateWorking)}
	h.list.SetExternal(lanes)

	now := time.Now()
	require.Nil(t, h.updateAttention(now))

	lanes[0].State = clarity.StateStalled
	h.list.SetExternal(lanes)
	now = now.Add(20 * time.Second)
	require.NotNil(t, h.updateAttention(now), "an external lane's own crossing must ring the bell too")
}

// TestUpdateAttention_LaneRemovedDropsFromTracker proves a killed/removed
// lane's own attentionState entry is dropped, not left to phantom-ring a
// future lane that happens to reuse its name.
func TestUpdateAttention_LaneRemovedDropsFromTracker(t *testing.T) {
	h := newComposerTestHome(t)
	inst := attentionTestInstance(t, "lane-a", clarity.StateWaitingYou)
	h.list.AddInstance(inst)()

	now := time.Now()
	require.NotNil(t, h.updateAttention(now), "test setup: the first crossing must ring")

	// Swap in a fresh, empty list - the "lane-a" tmux session is gone
	// (killed) - and let one tick see it absent, which must drop its own
	// tracker entry.
	sp := spinner.New()
	h.list = ui.NewList(&sp, false)
	now = now.Add(20 * time.Second)
	require.Nil(t, h.updateAttention(now), "no lanes at all: nothing to ring")

	// A DIFFERENT lane instance now reuses the exact same title, already
	// waiting on you from its very first tick - this must still read as a
	// crossing (attentionNone -> attentionWaitingOrStalled), never silenced
	// by a stale "already waiting" entry left over from the killed lane.
	inst2 := attentionTestInstance(t, "lane-a", clarity.StateWaitingYou)
	h.list.AddInstance(inst2)()
	now = now.Add(time.Second)
	require.NotNil(t, h.updateAttention(now), "a fresh lane reusing the same name must ring on its OWN first crossing, not be silenced by the dropped lane's stale entry")
}
