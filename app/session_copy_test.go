// Package app: this file tests slice 22 (copy from the Session tab -
// design/cockpit-pane, the owner's own complaint 3 Sep 13:0x: "dude cant
// copy paste from the session, would be useful") at the app level - the c/
// C/v key dispatch, the footer strings, and the mouse-mode choice (PART A).
// ui/session_test.go covers the plain-text shapes and the picker's own
// move/highlight logic; this file proves handleKeyPress wires them up.
package app

import (
	"claude-squad/cmd/cmd_test"
	"claude-squad/session/clarity"
	"claude-squad/ui"
	"os/exec"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

// sessionTabFixture builds a *ui.SessionInfo carrying the three turn kinds
// (owner, assistant, tool), oldest first - the same shape ui/session_test.
// go's own fixtureTail uses, duplicated here since that helper is private
// to the ui package's own test binary.
func sessionTabFixture() *ui.SessionInfo {
	base := time.Date(2026, 9, 3, 14, 0, 0, 0, time.Local)
	return &ui.SessionInfo{
		Lane: "pane-22-fixture",
		Tail: clarity.LaneTail{
			State: clarity.StateWorking,
			Turns: []clarity.Turn{
				{Kind: clarity.TurnOwner, At: base, Text: "please copy this turn"},
				{Kind: clarity.TurnAssistant, At: base.Add(5 * time.Second), Text: "on it"},
				{Kind: clarity.TurnTool, At: base.Add(9 * time.Second), Tool: "Bash", Summary: "run it", Result: clarity.ResultOK, Duration: time.Second},
			},
		},
		Now: base.Add(9 * time.Second),
	}
}

// stubClipboard installs a MockCmdExec on h that captures whatever is
// written to pbcopy's own stdin - the same capture shape terminal_and_keys_
// test.go's TestKeyCopy_* cases already use.
func stubClipboard(h *home) *string {
	var got string
	h.cmdExec = cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			buf := make([]byte, 4096)
			n, _ := cmd.Stdin.Read(buf)
			got = string(buf[:n])
			return nil
		},
	}
	return &got
}

// TestKeyCopy_SessionTab_ComposerClosed_CopiesLastTurn proves c, with the
// composer closed and the Session tab active (homeWithMockedTerminal's own
// default tab), copies the SELECTED lane's LAST turn - the fixture's tool
// turn - and shows the named footer.
func TestKeyCopy_SessionTab_ComposerClosed_CopiesLastTurn(t *testing.T) {
	h := homeWithMockedTerminal(t, false)
	require.True(t, h.tabbedWindow.IsInSessionTab(), "homeWithMockedTerminal's own default tab")
	h.sessionPane.SetInfo(sessionTabFixture())
	got := stubClipboard(h)

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'c', Text: "c"})

	require.Equal(t, "▪ Bash  run it  exit 0     1.0s", *got)
	require.Equal(t, "copied · last turn (1 lines)", h.statusText)
}

// TestKeyCopyTail_SessionTab_CopiesWholeTail proves C (shift-c) copies
// every loaded turn, blank-line joined, with the named "N turns" footer.
func TestKeyCopyTail_SessionTab_CopiesWholeTail(t *testing.T) {
	h := homeWithMockedTerminal(t, false)
	h.sessionPane.SetInfo(sessionTabFixture())
	got := stubClipboard(h)

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'C', Text: "C"})

	require.Contains(t, *got, "YOU  14:00:00")
	require.Contains(t, *got, "please copy this turn")
	require.Contains(t, *got, "▪ Bash  run it  exit 0     1.0s")
	require.Equal(t, "copied · 3 turns", h.statusText)
}

// TestKeyTurnPicker_OpenMoveCopyClose is the v-key's own end-to-end path:
// v opens the picker (starting on the newest turn), up moves the highlight
// to the OLDER turn, c copies that highlighted turn (not the newest one),
// and esc leaves the picker, restoring ordinary key dispatch.
func TestKeyTurnPicker_OpenMoveCopyClose(t *testing.T) {
	h := homeWithMockedTerminal(t, false)
	h.sessionPane.SetInfo(sessionTabFixture())
	got := stubClipboard(h)

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'v', Text: "v"})
	require.Equal(t, stateSessionPicker, h.state)
	require.True(t, h.sessionPane.PickerActive())

	// Newest turn (the tool turn) starts highlighted - up moves to the
	// assistant turn, the one just before it.
	h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyUp})
	h.handleKeyPress(tea.KeyPressMsg{Code: 'c', Text: "c"})
	require.Equal(t, "CLAUDE  14:00:05\n\non it", *got)
	require.Equal(t, "copied · turn (3 lines)", h.statusText)

	h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEsc})
	require.Equal(t, stateDefault, h.state)
	require.False(t, h.sessionPane.PickerActive())
}

// TestKeyTurnPicker_NoTurns_NeverEntersPickerState proves v is a safe no-op
// (never enters stateSessionPicker) when the selected lane has no turns to
// pick from.
func TestKeyTurnPicker_NoTurns_NeverEntersPickerState(t *testing.T) {
	h := homeWithMockedTerminal(t, false)
	pressGlobalKey(h, tea.KeyPressMsg{Code: 'v', Text: "v"})
	require.Equal(t, stateDefault, h.state)
}

// TestView_MouseModeNone proves PART A's own ruling: mouse capture is OFF
// (the wheel scroll case was its only consumer, and cell-motion capture
// cost the terminal's own native drag-select/copy everywhere) - the View's
// own tea.View carries MouseModeNone, not MouseModeCellMotion.
func TestView_MouseModeNone(t *testing.T) {
	h := homeWithMockedTerminal(t, false)
	v := h.View()
	require.Equal(t, tea.MouseModeNone, v.MouseMode)
}
