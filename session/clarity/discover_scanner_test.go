// Package clarity: slice 20b's own tests for item 3 (COCKPIT-CONTRACT.md
// brief) - the per-lane `tmux has-session` liveness fallback batched into
// one `tmux list-sessions` call, and the discovery walk itself made
// change-driven via ExternalLaneScanner. Kept in its own file, the
// convention discover_test.go's own siblings already follow.
package clarity

import (
	"claude-squad/cmd/cmd_test"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// countingListSessionsExec is a MockCmdExec whose Output counts every call
// (atomically - DiscoverExternalLanes/Scan are never called concurrently
// in these tests, but atomic keeps the counter honest regardless) and
// answers "#S"-formatted session names from live.
func countingListSessionsExec(t *testing.T, live []string) (cmd_test.MockCmdExec, *int32) {
	t.Helper()
	var calls int32
	return cmd_test.MockCmdExec{
		OutputFunc: func(c *exec.Cmd) ([]byte, error) {
			atomic.AddInt32(&calls, 1)
			require.Contains(t, c.String(), "list-sessions", "the batched liveness call must be list-sessions, never has-session")
			return []byte(strings.Join(live, "\n")), nil
		},
	}, &calls
}

// TestDiscoverExternalLanes_OneListSessionsCallForNLanes is item 4(c): three
// external lanes must resolve their liveness through exactly one tmux
// call, not three - the has-session-per-lane shape ExternalLaneAlive used
// to be called through, per lane, inside the old DiscoverExternalLanes.
func TestDiscoverExternalLanes_OneListSessionsCallForNLanes(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)

	mkTranscriptDir(t, root, "-Users-allencoates-projects-Clarity-sessions-fixture-a", "a.jsonl", time.Minute)
	mkTranscriptDir(t, root, "-Users-allencoates-projects-Clarity-sessions-fixture-b", "a.jsonl", time.Minute)
	mkTranscriptDir(t, root, "-Users-allencoates-projects-Clarity-sessions-fixture-c", "a.jsonl", time.Minute)

	mockExec, calls := countingListSessionsExec(t, []string{"sessions-fixture-a", "sessions-fixture-c"})

	lanes, err := DiscoverExternalLanes(nil, mockExec)
	require.NoError(t, err)
	require.Len(t, lanes, 3)
	require.Equal(t, int32(1), atomic.LoadInt32(calls),
		"three external lanes must issue exactly one tmux list-sessions call, not three has-session calls")

	byName := map[string]bool{}
	for _, l := range lanes {
		byName[l.Name] = l.Alive
	}
	require.True(t, byName["fixture-a"], "fixture-a is in the mocked list-sessions output")
	require.False(t, byName["fixture-b"], "fixture-b is not in the mocked output")
	require.True(t, byName["fixture-c"], "fixture-c is in the mocked output")
}

// TestExternalLaneScanner_SkipsWalkWhenMtimesUnchanged is item 4(b)'s first
// half: two Scan calls with nothing touched on disk between them must walk
// the filesystem exactly once (the hook: ExternalLaneScanner.walkCount).
func TestExternalLaneScanner_SkipsWalkWhenMtimesUnchanged(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)
	mkTranscriptDir(t, root, "-Users-allencoates-projects-Clarity-sessions-fixture-scan", "a.jsonl", time.Minute)

	mockExec, _ := countingListSessionsExec(t, nil)
	var s ExternalLaneScanner

	lanes1, err := s.Scan(nil, mockExec)
	require.NoError(t, err)
	require.Len(t, lanes1, 1)
	require.Equal(t, 1, s.walkCount, "the first call always walks")

	lanes2, err := s.Scan(nil, mockExec)
	require.NoError(t, err)
	require.Len(t, lanes2, 1)
	require.Equal(t, 1, s.walkCount,
		"a second Scan with nothing changed under claudeProjectsRoots() must not walk again")
}

// TestExternalLaneScanner_RewalksWhenLaneDirectoryTouched is item 4(b)'s
// second half: a new session directory (a lane appearing for the first
// time) must trigger exactly one more walk on the very next Scan.
func TestExternalLaneScanner_RewalksWhenLaneDirectoryTouched(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)
	mkTranscriptDir(t, root, "-Users-allencoates-projects-Clarity-sessions-fixture-scan2", "a.jsonl", time.Minute)

	mockExec, _ := countingListSessionsExec(t, nil)
	var s ExternalLaneScanner

	_, err := s.Scan(nil, mockExec)
	require.NoError(t, err)
	require.Equal(t, 1, s.walkCount)

	// A second lane directory appears - this is what a genuinely new
	// external lane starting up looks like on disk.
	mkTranscriptDir(t, root, "-Users-allencoates-projects-Clarity-sessions-fixture-scan3", "a.jsonl", time.Minute)

	lanes, err := s.Scan(nil, mockExec)
	require.NoError(t, err)
	require.Len(t, lanes, 2)
	require.Equal(t, 2, s.walkCount, "a new session directory must trigger exactly one more walk")
}

// TestExternalLaneScanner_RewalksWhenNewTranscriptAddedToExistingLane is
// item 4(b)'s third case: a NEW transcript file appearing inside an
// ALREADY-DISCOVERED lane's own directory (a fresh conversation in a lane
// the scanner has already walked once) changes that lane directory's own
// mtime, not just its root's - proving the fingerprint reads subdirectory
// mtimes too, not only the root's.
func TestExternalLaneScanner_RewalksWhenNewTranscriptAddedToExistingLane(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)
	encoded := "-Users-allencoates-projects-Clarity-sessions-fixture-scan4"
	mkTranscriptDir(t, root, encoded, "first.jsonl", time.Minute)

	mockExec, _ := countingListSessionsExec(t, nil)
	var s ExternalLaneScanner

	_, err := s.Scan(nil, mockExec)
	require.NoError(t, err)
	require.Equal(t, 1, s.walkCount)

	_, err = s.Scan(nil, mockExec)
	require.NoError(t, err)
	require.Equal(t, 1, s.walkCount, "no change yet - still one walk")

	// A second transcript lands in the SAME lane directory - only that
	// subdirectory's own mtime moves, the root directory's own entries are
	// unchanged.
	second := filepath.Join(root, encoded, "second.jsonl")
	require.NoError(t, os.WriteFile(second, []byte("{}\n"), 0644))

	lanes, err := s.Scan(nil, mockExec)
	require.NoError(t, err)
	require.Len(t, lanes, 1, "still one lane - the newest transcript per lane dedupe is unaffected")
	require.Equal(t, 2, s.walkCount, "a new transcript inside an existing lane directory must trigger one more walk")
}
