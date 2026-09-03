// Package app: slice 17b's own bell/title liveness proof (item 3,
// COCKPIT-MODALITIES-2026-09-03.md, WoW ruling 3 Sep 22:3x - "a lane with
// no live process behind it ... does not count in the title or the bell").
package app

import (
	"claude-squad/cmd/cmd_test"
	"claude-squad/session"
	"claude-squad/session/clarity"
	"claude-squad/session/tmux"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// deadAttentionTestInstance is attentionTestInstance's own dead-lane
// twin: the tmux session's own Run always fails, so TmuxAlive() reads
// false - the exact shape the 3 Sep 18:47:57 incident left behind (a
// tracked row whose own Status never moved off Running once its tmux
// server died out from under it).
func deadAttentionTestInstance(t *testing.T, title, state string) *session.Instance {
	t.Helper()
	inst, err := session.NewInstance(session.InstanceOptions{
		Title:   title,
		Path:    ".",
		Program: "echo",
	})
	require.NoError(t, err)
	inst.SetTmuxSession(tmux.NewTmuxSessionWithDeps(title, "echo", nil, cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error { return errors.New("no server running") },
	}))
	inst.SetLaneState(state, time.Now(), true)
	return inst
}

// TestUpdateAttention_DeadLanesNeverCountOrRing is item 4(b)'s own required
// proof: two dead lanes (one tracked, one external), both carrying a
// closed owner-waiting transcript, alongside ONE genuinely alive waiting
// lane - the title reads exactly "1 need you" and exactly one bell fires
// on the crossing, never three, never two.
func TestUpdateAttention_DeadLanesNeverCountOrRing(t *testing.T) {
	h := newComposerTestHome(t)

	deadTracked := deadAttentionTestInstance(t, "dead-tracked", clarity.StateWaitingYou)
	h.list.AddInstance(deadTracked)()
	h.list.SetExternal([]clarity.ExternalLane{
		{Name: "dead-external", State: clarity.StateWaitingYou, StateOK: true, LastTurn: time.Now(), Alive: false},
	})

	now := time.Now()
	cmd := h.updateAttention(now)
	require.Nil(t, cmd, "two dead waiting-shaped lanes, no alive one yet: nothing to ring")
	require.Equal(t, attentionDefaultTitle, h.windowTitle, "a dead lane never counts in the title")

	// Now a genuinely alive lane crosses into waiting on you.
	aliveLane := attentionTestInstance(t, "alive-lane", clarity.StateWorking)
	h.list.AddInstance(aliveLane)()
	now = now.Add(time.Second)
	require.Nil(t, h.updateAttention(now), "working is not an attention state yet")

	aliveLane.SetLaneState(clarity.StateWaitingYou, now, true)
	now = now.Add(20 * time.Second)
	cmd = h.updateAttention(now)
	require.NotNil(t, cmd, "the alive lane's own crossing must ring exactly once")
	require.Equal(t, "Clarity Workspace · 1 need you", h.windowTitle, "only the alive lane counts - the two dead ones never do")

	// A second tick with nothing new crossing: no further ring, title
	// unchanged - proves this was an edge, not a level re-firing off the
	// still-present dead rows.
	now = now.Add(time.Second)
	require.Nil(t, h.updateAttention(now))
	require.Equal(t, "Clarity Workspace · 1 need you", h.windowTitle)
}
