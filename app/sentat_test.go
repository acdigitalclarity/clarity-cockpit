package app

import (
	"claude-squad/session/clarity"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// TestSentAt_ComposerSendRecordsPerLane is item 5's own send-path proof
// (WAITING HELD, cockpit-pane modalities research): a successful, tracked
// composer send (the m-key plain message, sendComposerCmd -> deliverToLane
// -> Instance.SendPrompt) must be recorded in m.laneSentAt, keyed by the
// lane's own title - composerResultMsg's own handling in Update
// (recordSentAt) is where this actually happens, so this drives the real
// key sequence rather than calling recordSentAt directly.
func TestSentAt_ComposerSendRecordsPerLane(t *testing.T) {
	h := newComposerTestHome(t)
	inst := trackedInstanceWithFakeTmux(t, "sentat-lane", "old output\nack\n\n\n")
	h.list.AddInstance(inst)
	h.list.SetSelectedInstance(0)

	require.Empty(t, h.laneSentAt["sentat-lane"], "seen failing: nothing recorded before any send")

	h.composer.Open("sentat-lane", false)
	h.state = stateMsg
	h.composer.Type("go ahead")

	before := time.Now()
	_, cmd := h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)
	resultMsg := cmd()
	result, ok := resultMsg.(composerResultMsg)
	require.True(t, ok)
	require.NoError(t, result.err)

	h.Update(resultMsg)

	recorded, ok := h.laneSentAt["sentat-lane"]
	require.True(t, ok, "the send must be recorded against the lane's own title")
	require.False(t, recorded.Before(before), "the recorded instant must be no earlier than the send itself")
}

// TestSentAt_ExternalCopyNeverRecorded is the same proof's negative half: a
// composer send to an EXTERNAL lane only copies to the clipboard - nothing
// is actually sent into any tmux session, so it must never suppress that
// lane's own held "waiting on you" the way a tracked send does.
func TestSentAt_ExternalCopyNeverRecorded(t *testing.T) {
	h := newComposerTestHome(t)
	h.list.SetExternal([]clarity.ExternalLane{{Name: "ext-lane"}})
	h.list.Down()

	lane, isExternal, ok := h.composerTarget()
	require.True(t, ok)
	require.True(t, isExternal)
	h.composer.Open(lane, isExternal)
	h.state = stateMsg
	h.composer.Type("paste this yourself")

	_, cmd := h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)
	resultMsg := cmd()
	result, ok := resultMsg.(composerResultMsg)
	require.True(t, ok)
	require.NoError(t, result.err)

	h.Update(resultMsg)

	_, recorded := h.laneSentAt["ext-lane"]
	require.False(t, recorded, "an external (copy-only) send must never populate laneSentAt")
}

// TestSentAt_CockpitSendTurnsHeldWaitingIntoIdle is item 5's own end-to-end
// proof, seen failing first: a lane whose transcript ends in a closed,
// pending-free turn reads "waiting on you" (HELD, no time-only decay) -
// then, with the SAME transcript file left untouched, a real cockpit send
// through the composer's own m-key path is enough to read the very same
// lane as idle/answered on the next tick, bridging the gap before the
// transcript itself ever catches up (ReadLaneTail's own sentAt argument,
// wired through LaneTailCache.Get and m.laneSentAt).
func TestSentAt_CockpitSendTurnsHeldWaitingIntoIdle(t *testing.T) {
	root := t.TempDir()
	t.Setenv(clarity.ClaudeProjectsRootEnvVar, root)

	h := newComposerTestHome(t)
	closedAt := time.Now().Add(-20 * time.Minute)

	inst := trackedInstanceWithFakeTmux(t, "held-lane", "old output\nack\n\n\n")
	dir := filepath.Join(root, clarity.EncodeProjectDir(inst.Path))
	require.NoError(t, os.MkdirAll(dir, 0755))
	transcript := filepath.Join(dir, "t.jsonl")
	line := `{"type":"system","subtype":"turn_duration","timestamp":"` + closedAt.UTC().Format(time.RFC3339) +
		`","durationMs":1000,"messageCount":1,"pendingBackgroundAgentCount":0}` + "\n"
	require.NoError(t, os.WriteFile(transcript, []byte(line), 0644))

	h.list.AddInstance(inst)
	h.list.SetSelectedInstance(0)

	_, tickCmd := h.Update(sessionTickMsg{})
	require.NotNil(t, tickCmd)
	before := ansi.Strip(h.tabbedWindow.String())
	require.Contains(t, before, "waiting on you",
		"seen failing: a closed, pending-free turn with no send recorded must still read waiting on you")

	h.composer.Open("held-lane", false)
	h.state = stateMsg
	h.composer.Type("go ahead")
	_, sendCmd := h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, sendCmd)
	resultMsg := sendCmd()
	result, ok := resultMsg.(composerResultMsg)
	require.True(t, ok)
	require.NoError(t, result.err)
	h.Update(resultMsg)

	require.True(t, h.laneSentAt["held-lane"].After(closedAt), "test setup: the recorded send must be newer than the close")

	_, tickCmd2 := h.Update(sessionTickMsg{})
	require.NotNil(t, tickCmd2)
	after := ansi.Strip(h.tabbedWindow.String())
	require.Contains(t, after, "answered",
		"after the cockpit's own send, the same unchanged transcript must read answered, not waiting")
	require.NotContains(t, after, "waiting on you",
		"the held waiting-on-you clause must be gone once the cockpit itself has answered")
}
