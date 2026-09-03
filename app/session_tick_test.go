package app

import (
	"claude-squad/session"
	"claude-squad/session/clarity"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// writeTrackedLaneFixture builds a Started tracked instance backed by a
// throwaway working directory and a one-line turn_duration transcript under
// the test's own CLARITY_CLAUDE_PROJECTS_ROOT (the same recipe fit_test.go's
// feed-tick tests already use) - enough for clarity.NewestTranscript and
// LaneTailCache.Get to resolve real state from, without a live tmux session.
func writeTrackedLaneFixture(t *testing.T, root, title string, at time.Time) *session.Instance {
	t.Helper()
	lanePath := t.TempDir()
	dir := filepath.Join(root, clarity.EncodeProjectDir(lanePath))
	require.NoError(t, os.MkdirAll(dir, 0755))
	transcript := filepath.Join(dir, "t.jsonl")
	line := `{"type":"system","subtype":"turn_duration","timestamp":"` + at.UTC().Format(time.RFC3339) +
		`","durationMs":1000,"messageCount":1,"pendingBackgroundAgentCount":0}` + "\n"
	require.NoError(t, os.WriteFile(transcript, []byte(line), 0644))

	inst, err := session.NewInstance(session.InstanceOptions{Title: title, Path: lanePath, Program: "echo"})
	require.NoError(t, err)
	return inst
}

// TestSessionTick_RefreshesSelectedLaneOnly_RowsUntouched is the Latency
// ruling's own scope rule, seen failing before this leg's fix (there was no
// sessionTickMsg at all - the Session tab only ever refreshed on the 3s
// feedTickMsg, which ALSO recomputes every row's own state): a
// sessionTickMsg must (a) refresh the SELECTED lane's own Session pane
// content and (b) leave every OTHER row's cached lane state exactly as it
// was - proving the fast tick never drags the fleet-wide feedTickMsg work
// along with it.
func TestSessionTick_RefreshesSelectedLaneOnly_RowsUntouched(t *testing.T) {
	root := t.TempDir()
	t.Setenv(clarity.ClaudeProjectsRootEnvVar, root)

	h := newComposerTestHome()
	now := time.Now()
	selected := writeTrackedLaneFixture(t, root, "selected-lane", now.Add(-time.Minute))
	other := writeTrackedLaneFixture(t, root, "other-lane", now.Add(-time.Minute))
	h.list.AddInstance(selected)()
	h.list.AddInstance(other)()

	require.Same(t, selected, h.list.GetSelectedInstance(), "test setup: the first-added row must be the selected one")

	_, _, ok := selected.GetLaneState()
	require.False(t, ok, "no lane state cached yet for either row")
	_, _, ok = other.GetLaneState()
	require.False(t, ok)

	_, cmd := h.Update(sessionTickMsg{})
	require.NotNil(t, cmd, "sessionTickMsg self-reschedules")

	require.Contains(t, h.tabbedWindow.String(), "selected-lane",
		"the Session pane must carry the SELECTED lane's own data after one session tick")

	_, _, ok = other.GetLaneState()
	require.False(t, ok, "sessionTickMsg must never touch a row it did not select - that is feedTickMsg's own job, still on its 3s cadence")
}

// TestSessionTick_SelfReschedulesAt500ms is the Latency ruling's own cadence
// number, verbatim.
func TestSessionTick_SelfReschedulesAt500ms(t *testing.T) {
	require.Equal(t, 500*time.Millisecond, sessionTickInterval)
}

// TestFeedTick_StillUpdatesOtherRows_SessionTickDoesNot is the same rule
// from the opposite direction: a feedTickMsg (the 3s tick) DOES update
// every row's own lane state, confirming that job never silently moved to
// sessionTickMsg - only its OWN work (fleet counts, needs-you, external
// discovery) does.
func TestFeedTick_StillUpdatesOtherRows_SessionTickDoesNot(t *testing.T) {
	root := t.TempDir()
	t.Setenv(clarity.ClaudeProjectsRootEnvVar, root)
	t.Setenv(clarity.FeedQueuePathEnvVar, filepath.Join(root, "no-such-queue.json"))

	h := newComposerTestHome()
	now := time.Now()
	selected := writeTrackedLaneFixture(t, root, "selected-lane", now.Add(-time.Minute))
	other := writeTrackedLaneFixture(t, root, "other-lane", now.Add(-time.Minute))
	h.list.AddInstance(selected)()
	h.list.AddInstance(other)()

	_, _, ok := other.GetLaneState()
	require.False(t, ok)

	_, cmd := h.Update(sessionTickMsg{})
	require.NotNil(t, cmd)
	_, _, ok = other.GetLaneState()
	require.False(t, ok, "sessionTickMsg alone must not update the other row")

	_, cmd = h.Update(feedTickMsg{})
	require.NotNil(t, cmd)
	_, _, ok = other.GetLaneState()
	require.True(t, ok, "feedTickMsg must still update every row, unchanged by this leg")
}
