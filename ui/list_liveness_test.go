// Package ui: slice 17b's own row-word and sort-rank liveness proof (item
// 2/5, COCKPIT-MODALITIES-2026-09-03.md, WoW ruling 3 Sep 22:3x). Kept in
// its own file, the same convention list_frontdoor5_test.go/
// list_attention_test.go already follow.
package ui

import (
	"claude-squad/cmd/cmd_test"
	"claude-squad/session"
	"claude-squad/session/clarity"
	"claude-squad/session/tmux"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// deadFrontdoor5Instance is frontdoor5Instance's own dead-lane twin: the
// tmux session's own Run always fails, so Alive() reads false regardless
// of Status (the 3 Sep 18:47:57 incident's own shape - a tmux session gone
// with Status never having caught up).
func deadFrontdoor5Instance(t *testing.T, title string, state string, when time.Time) *session.Instance {
	t.Helper()
	inst, err := session.NewInstance(session.InstanceOptions{Title: title, Path: ".", Program: "echo"})
	require.NoError(t, err)
	inst.SetTmuxSession(tmux.NewTmuxSessionWithDeps(title, "echo", nil, cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error { return errors.New("no server running") },
	}))
	inst.SetLaneState(state, when, true)
	return inst
}

// TestString_DeadTrackedRow_ReadsStoppedNotWaiting is item 4(c)'s own
// tracked-row half: a dead tracked lane whose transcript ends in a closed
// owner-waiting turn draws "stopped", never "waiting".
func TestString_DeadTrackedRow_ReadsStoppedNotWaiting(t *testing.T) {
	l := newTestList()
	l.SetSize(120, 40)
	l.AddInstance(deadFrontdoor5Instance(t, "dead-lane", clarity.StateWaitingYou, frontdoor5Time(9, 0)))

	out := ansi.Strip(l.String())
	require.Contains(t, out, "stopped", "a dead tracked row reads stopped")
	require.NotContains(t, out, "waiting", "never waiting, no matter what its transcript says")
}

// TestString_PausedTrackedRow_ReadsPausedWord proves the tracked row's own
// SECOND dead case: explicitly Paused reads its own word, distinct from a
// tmux-session-gone-but-not-yet-Paused row's "stopped".
func TestString_PausedTrackedRow_ReadsPausedWord(t *testing.T) {
	l := newTestList()
	l.SetSize(120, 40)
	inst := deadFrontdoor5Instance(t, "paused-lane", clarity.StateWaitingYou, frontdoor5Time(9, 0))
	inst.SetStatus(session.Paused)
	l.AddInstance(inst)

	out := ansi.Strip(l.String())
	require.Contains(t, out, "paused", "an explicitly Paused row reads its own word")
	require.NotContains(t, out, "waiting")
}

// TestString_DeadExternalRow_ReadsStoppedNotWaiting is item 4(c)'s external
// half.
func TestString_DeadExternalRow_ReadsStoppedNotWaiting(t *testing.T) {
	l := newTestList()
	l.SetSize(120, 40)
	l.SetExternal([]clarity.ExternalLane{
		{Name: "dead-ext", State: clarity.StateWaitingYou, StateOK: true, LastTurn: frontdoor5Time(9, 0), Alive: false},
	})

	out := ansi.Strip(l.String())
	require.Contains(t, out, "stopped")
	require.NotContains(t, out, "waiting")
}

// TestGroupLanesByModality_DeadRowSortsBelowAliveWaiting is item 4(c)'s own
// sort-rank proof: a dead tracked row and a dead external row, both
// carrying a closed owner-waiting transcript, sort BELOW an alive idle
// row within the same catch-all group - the extended rank (waiting,
// stalled, working, idle, then stopped/paused).
func TestGroupLanesByModality_DeadRowSortsBelowAliveWaiting(t *testing.T) {
	aliveWaiting := frontdoor5Instance(t, "row-alive-waiting", "ta", "", 10, clarity.StateWaitingYou, frontdoor5Time(9, 0))
	aliveIdle := frontdoor5Instance(t, "row-alive-idle", "ta", "", 10, clarity.StateIdle, frontdoor5Time(9, 1))
	deadTracked := deadFrontdoor5Instance(t, "row-dead-tracked", clarity.StateWaitingYou, frontdoor5Time(9, 2))
	items := []*session.Instance{deadTracked, aliveIdle, aliveWaiting}

	external := []clarity.ExternalLane{
		{Name: "row-dead-external", State: clarity.StateWaitingYou, StateOK: true, LastTurn: frontdoor5Time(9, 3), Alive: false},
	}

	groups := groupLanesByModality(items, external)
	require.Len(t, groups, 1)

	gotItems := make([]string, len(groups[0].itemIdx))
	for i, idx := range groups[0].itemIdx {
		gotItems[i] = items[idx].Title
	}
	require.Equal(t, []string{"row-alive-waiting", "row-alive-idle", "row-dead-tracked"}, gotItems,
		"the dead tracked row sorts LAST, below alive idle even though its own transcript says waiting")

	gotExternal := make([]string, len(groups[0].externalIdx))
	for i, idx := range groups[0].externalIdx {
		gotExternal[i] = external[idx].Name
	}
	require.Equal(t, []string{"row-dead-external"}, gotExternal)
}
