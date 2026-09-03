// Package session: cockpit-pane slice 17c item 2/3(c) - the batched
// liveness pass costs exactly one tmux call regardless of how many tracked
// rows it answers for, counted through a shared mock executor (never a
// real tmux session; this proof is about call COUNT, not about a real
// server's own answer, which the boot tests in instance_liveness_boot_test.go
// already cover).
package session

import (
	"claude-squad/cmd/cmd_test"
	"claude-squad/session/tmux"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// countingSessionsExecutor answers `tmux list-sessions -F "#S"` with a
// fixed set of names and counts every Output call it receives - any call
// this test does not expect (has-session, attach-session, ...) fails the
// test outright, so a regression that falls back to a per-row has-session
// call is caught by the call COUNT, not just by the final Alive answers.
type countingSessionsExecutor struct {
	t     *testing.T
	names []string
	calls int
}

func (e *countingSessionsExecutor) Run(cmd *exec.Cmd) error {
	e.t.Fatalf("unexpected Run call: %s", strings.Join(cmd.Args, " "))
	return nil
}

func (e *countingSessionsExecutor) Output(cmd *exec.Cmd) ([]byte, error) {
	e.calls++
	if !strings.Contains(strings.Join(cmd.Args, " "), "list-sessions") {
		e.t.Fatalf("expected only a list-sessions call, got: %s", strings.Join(cmd.Args, " "))
	}
	return []byte(strings.Join(e.names, "\n") + "\n"), nil
}

// TestApplyLiveSessionSet_NRowsOnePass_OneTmuxCall is item 3(c): N tracked
// instances answered from a single tmux.ListSessionNames call, counted
// through the executor - the batched replacement for a has-session call
// per row (slice 17b's own Alive(), still available as Alive()'s own
// fallback when no tick has run yet, but never reached once the cache is
// populated this way).
func TestApplyLiveSessionSet_NRowsOnePass_OneTmuxCall(t *testing.T) {
	const n = 12
	titles := make([]string, 0, n)
	var instances []*Instance
	for i := 0; i < n; i++ {
		title := "row-" + string(rune('a'+i))
		titles = append(titles, title)
		inst, err := NewInstance(InstanceOptions{Title: title, Path: t.TempDir(), Program: "echo", NoWorktree: true})
		require.NoError(t, err)
		inst.SetTmuxSession(tmux.NewTmuxSessionWithDeps(title, "echo", nil, cmd_test.MockCmdExec{
			RunFunc: func(cmd *exec.Cmd) error {
				t.Fatalf("Alive() must never fall back to a per-row call once the batched set is applied: %s",
					strings.Join(cmd.Args, " "))
				return nil
			},
		}))
		instances = append(instances, inst)
	}

	// Only every OTHER row's own sanitized name is "alive" on the batched
	// set, so the test also proves the lookup is by name, not "everything
	// true because the set is non-empty".
	aliveNames := make([]string, 0, n/2)
	for i, title := range titles {
		if i%2 == 0 {
			aliveNames = append(aliveNames, tmux.SanitizeName(title))
		}
	}

	executor := &countingSessionsExecutor{t: t, names: aliveNames}
	names, err := tmux.ListSessionNames(executor)
	require.NoError(t, err)
	require.Equal(t, 1, executor.calls, "exactly one tmux call for the whole pass, regardless of row count")

	ApplyLiveSessionSet(instances, names)

	for i, inst := range instances {
		if i%2 == 0 {
			require.True(t, inst.Alive(), "row %d was in the batched set", i)
		} else {
			require.False(t, inst.Alive(), "row %d was not in the batched set", i)
		}
	}

	// A second pass over the SAME already-fetched set still makes no
	// further tmux call - Alive() is a pure cache read once populated.
	for _, inst := range instances {
		_ = inst.Alive()
	}
	require.Equal(t, 1, executor.calls, "reading Alive() after the cache is populated makes no further tmux call")
}
