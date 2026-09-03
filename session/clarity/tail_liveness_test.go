// Package clarity: slice 17b's own liveness-layer proof (item 1/2,
// COCKPIT-MODALITIES-2026-09-03.md, WoW ruling 3 Sep 22:3x - "waiting
// persists without decay ... but only for an alive lane"). ClassifyState
// itself stays a pure transcript read (untouched by this slice); the
// layer above it, ApplyLiveness (tail.go), is what a dead lane's own
// closed-waiting-turn transcript must be run through before it is ever
// shown, so the 3 Sep 18:47:57 incident - three tracked lanes whose tmux
// server died still reading "waiting on you" - cannot recur.
package clarity

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestApplyLiveness_DeadLaneClosedWaitingTurn_ReadsStopped_AliveReadsWaiting
// is item 4(a)'s own required proof: the identical transcript - a turn
// closed with no pending agents, the exact shape ClassifyState reads as
// "waiting on you" - reads "stopped" once ApplyLiveness is told the lane is
// dead, and reads "waiting on you" unchanged once it is told the lane is
// alive.
func TestApplyLiveness_DeadLaneClosedWaitingTurn_ReadsStopped_AliveReadsWaiting(t *testing.T) {
	now := time.Date(2026, 9, 3, 22, 0, 0, 0, time.UTC)
	path := writeFixture(t, []string{
		turnDurationLine(now.Add(-5*time.Minute), 1000, 3, 0),
	})
	tail, err := ReadLaneTail(path, DefaultTailMaxBytes, DefaultTailTurns, now)
	require.NoError(t, err)
	require.Equal(t, StateWaitingYou, tail.State, "ClassifyState itself stays pure: this transcript reads waiting regardless of liveness")

	dead := ApplyLiveness(tail.State, false)
	require.Equal(t, StateStopped, dead, "a dead lane never reads waiting on you, no matter how the transcript itself reads")

	alive := ApplyLiveness(tail.State, true)
	require.Equal(t, StateWaitingYou, alive, "an alive lane's own classifier word passes through unchanged")
}

// TestApplyLiveness_DeadLaneStalledTurn_ReadsStopped covers the OTHER
// attention-triggering word (item 3: classifyAttention's own "waiting on
// you OR stalled" pair) - a dead lane must not read stalled either, since
// app/attention.go's own bell/title gate gets there by reading Alive
// directly rather than re-deriving it from the word, but ui/list.go's row
// render goes through this exact function, so it must hold for stalled too.
func TestApplyLiveness_DeadLaneStalledTurn_ReadsStopped(t *testing.T) {
	require.Equal(t, StateStopped, ApplyLiveness(StateStalled, false))
	require.Equal(t, StateStalled, ApplyLiveness(StateStalled, true))
}

// TestApplyLiveness_AliveLane_IdleAndWorkingUnaffected is the brief's own
// "idle and working are unaffected for alive lanes" - the trivial alive-
// side half of that sentence, proven directly since ApplyLiveness has no
// separate branch for them.
func TestApplyLiveness_AliveLane_IdleAndWorkingUnaffected(t *testing.T) {
	require.Equal(t, StateWorking, ApplyLiveness(StateWorking, true))
	require.Equal(t, StateIdle, ApplyLiveness(StateIdle, true))
}
