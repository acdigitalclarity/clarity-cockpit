package ui

import (
	"claude-squad/session"
	"strings"
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
	m.SetInstance(inst, false)

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

// mockUpFooterText is PANE-MOCKUP-164x45.md/PANE-MOCKUP-120x36.md's own
// footer line, verbatim (design/cockpit-pane/DECISIONS.md slice 7) - the
// exact text a tracked row's default menu must render.
const mockUpFooterText = "↑↓ select • ↵ attach │ m message • c copy • o open folder │ tab switch tab • ? help • q quit"

// TestMenu_TrackedRow_FooterMatchesMockUpExactly is slice 7's own KEYS
// requirement: c copy and o open folder land in ui/menu.go's option list at
// exactly the positions the owner-approved mock-up foot shows, replacing
// upstream's git-worktree group (new/kill/push/checkout/resume - still
// bound in keys.go, simply no longer advertised on this bar).
func TestMenu_TrackedRow_FooterMatchesMockUpExactly(t *testing.T) {
	m := NewMenu()
	inst, err := session.NewInstance(session.InstanceOptions{Title: "lane-a", Path: ".", Program: "echo"})
	require.NoError(t, err)
	m.SetInstance(inst, false)
	m.SetSize(200, 1)

	require.Equal(t, mockUpFooterText, strings.TrimSpace(ansi.Strip(m.String())))
}

// TestMenu_ExternalRow_FooterSameShapeButDimmed is slice 7's own "↵ attach
// and m message greyed on an external row" requirement: the footer's TEXT
// is unchanged (m still works as copy, per slice 5 - the row is never
// missing an option, only dimmed), but the ↵ attach/m message segments
// carry the Faint SGR attribute an ordinary tracked-row render does not.
func TestMenu_ExternalRow_FooterSameShapeButDimmed(t *testing.T) {
	tracked := NewMenu()
	inst, err := session.NewInstance(session.InstanceOptions{Title: "lane-a", Path: ".", Program: "echo"})
	require.NoError(t, err)
	tracked.SetInstance(inst, false)
	tracked.SetSize(200, 1)

	external := NewMenu()
	external.SetInstance(nil, true)
	external.SetSize(200, 1)

	require.Equal(t, mockUpFooterText, strings.TrimSpace(ansi.Strip(external.String())),
		"external row must keep the same footer text as a tracked row")
	require.NotEqual(t, tracked.String(), external.String(),
		"the external row's raw (unstripped) render must differ from the tracked row's - the Faint style on ↵ attach/m message must actually change the emitted ANSI, not just be a no-op call")
}

// TestMenu_NothingSelected_IsStateEmptyNotDimmed proves SetInstance(nil,
// false) - genuinely nothing selected, the empty-list case - is still
// StateEmpty's own short option list, distinct from an external row
// (SetInstance(nil, true)), which the fix above must not collapse into.
func TestMenu_NothingSelected_IsStateEmptyNotDimmed(t *testing.T) {
	m := NewMenu()
	m.SetInstance(nil, false)

	require.Equal(t, StateEmpty, m.state)
	require.NotContains(t, ansi.Strip(m.String()), "open folder",
		"nothing selected shows the bare empty-state menu, not the lane-action one")
}
