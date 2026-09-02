package ui

import (
	"claude-squad/session"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// TestMenu_StateMsg_FooterIsExactlyComposerText is board #280's slice 5b
// DEFECT 3: the composer's own menu state shows exactly "enter send · esc
// cancel", never StatePrompt's borrowed "enter submit name" (upstream's
// unrelated new-instance name-prompt overlay - the composer used to reuse
// its menu state and so inherited its footer text too).
func TestMenu_StateMsg_FooterIsExactlyComposerText(t *testing.T) {
	m := NewMenu()
	m.SetState(StateMsg)

	require.Equal(t, "enter send · esc cancel", ansi.Strip(m.String()))
}

// TestMenu_StatePrompt_StillReadsSubmitName proves the fix is additive:
// the real "enter prompt" instance-start overlay (StatePrompt) keeps its
// own upstream wording - only the composer moved off it.
func TestMenu_StatePrompt_StillReadsSubmitName(t *testing.T) {
	m := NewMenu()
	m.SetState(StatePrompt)

	require.Equal(t, "enter submit name", ansi.Strip(m.String()))
}

// TestMenu_SetInstance_NeverOverridesStateMsg is the feed-tick guard: a
// periodic instanceChanged() call while a message is being typed must
// never kick the footer out of "enter send · esc cancel".
func TestMenu_SetInstance_NeverOverridesStateMsg(t *testing.T) {
	m := NewMenu()
	m.SetState(StateMsg)

	inst, err := session.NewInstance(session.InstanceOptions{Title: "lane-a", Path: ".", Program: "echo"})
	require.NoError(t, err)
	m.SetInstance(inst)

	require.Equal(t, StateMsg, m.state)
	require.Equal(t, "enter send · esc cancel", ansi.Strip(m.String()))
}

// TestMenu_StateMsg_ThenClose_PreviousStateReturns is the other half of
// DEFECT 3's rule: once the composer closes, the ordinary menu returns
// (app.go's own SetState(ui.StateDefault) on close/send-complete).
func TestMenu_StateMsg_ThenClose_PreviousStateReturns(t *testing.T) {
	m := NewMenu()
	m.SetState(StateDefault)
	m.SetState(StateMsg)
	require.Equal(t, "enter send · esc cancel", ansi.Strip(m.String()))

	m.SetState(StateDefault)
	require.NotContains(t, ansi.Strip(m.String()), "enter send")
}
