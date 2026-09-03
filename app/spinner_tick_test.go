package app

import (
	"claude-squad/session/clarity"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestPreviewTick_AdvancesSpinner_SessionTickDoesNot is slice 14 rule 1's
// own decoupling requirement, seen failing before this leg's fix (the
// header glyph only ever advanced on sessionTickMsg, the 500ms read tick):
// a previewTickMsg (the existing 100ms cadence) must advance the Session
// pane's spinner, and a sessionTickMsg alone (no new file content to read)
// must NOT move it.
func TestPreviewTick_AdvancesSpinner_SessionTickDoesNot(t *testing.T) {
	root := t.TempDir()
	t.Setenv(clarity.ClaudeProjectsRootEnvVar, root)
	t.Setenv(clarity.FeedQueuePathEnvVar, filepath.Join(root, "no-such-queue.json"))

	h := newComposerTestHome(t)
	now := time.Now()
	selected := writeTrackedLaneFixture(t, root, "spinner-lane", now.Add(-time.Minute))
	h.list.AddInstance(selected)()

	// Open the lane's own turn so the header glyph actually animates -
	// otherwise a static state never shows a moving frame either way.
	openLaneTranscript(t, root, selected.Path, now)

	_, cmd := h.Update(sessionTickMsg{})
	require.NotNil(t, cmd)
	line0 := h.tabbedWindow.String()

	_, cmd = h.Update(sessionTickMsg{})
	require.NotNil(t, cmd)
	line1 := h.tabbedWindow.String()
	require.Equal(t, line0, line1, "a second sessionTickMsg alone (same file, same cache) must not move the spinner")

	_, cmd = h.Update(previewTickMsg{})
	require.NotNil(t, cmd)
	line2 := h.tabbedWindow.String()
	require.NotEqual(t, line1, line2, "a previewTickMsg (the 100ms cadence) must advance the spinner")
}

// openLaneTranscript overwrites the fixture transcript so the lane's own
// last record is an OPEN turn (an assistant text with no closing
// turn_duration) - the header glyph only ever animates while a turn is
// genuinely open.
func openLaneTranscript(t *testing.T, root, lanePath string, at time.Time) {
	t.Helper()
	dir := filepath.Join(root, clarity.EncodeProjectDir(lanePath))
	transcript := filepath.Join(dir, "t.jsonl")
	line := `{"type":"assistant","timestamp":"` + at.UTC().Format(time.RFC3339) +
		`","message":{"role":"assistant","model":"claude-fable-5-1","content":[{"type":"text","text":"still working on it"}]}}` + "\n"
	require.NoError(t, os.WriteFile(transcript, []byte(line), 0644))
}
