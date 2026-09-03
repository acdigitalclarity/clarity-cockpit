// Package app: slice 20's own tests (design/cockpit-pane/DECISIONS.md) -
// the 100ms previewTickMsg used to call instanceChanged (the Terminal tab's
// live-mirror refresh, driving a real tmux capture-pane) on every tick,
// whether or not anything on screen could actually change. The idle-CPU
// profile named the resulting full-screen re-render, not this call itself,
// as the dominant cost (SessionPane.renderResting, cached separately in
// session.go) - but this refresh still moved to the existing 500ms
// sessionTickMsg cadence (item 2 of the brief), so these tests prove that
// move landed and stuck, never the render-time claim.
package app

import (
	"claude-squad/cmd"
	"claude-squad/cmd/cmd_test"
	"claude-squad/config"
	"claude-squad/session/clarity"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"claude-squad/ui"
	"github.com/stretchr/testify/require"
)

// pane20PtyFactory is termPtyFactory's own shape (terminal_and_keys_test.go)
// - a temp file standing in for a live PTY, flipping sessionExists on Start
// so the has-session mock below reports the session live immediately after.
type pane20PtyFactory struct {
	t             *testing.T
	sessionExists *bool
}

func (f *pane20PtyFactory) Start(cmd *exec.Cmd) (*os.File, error) {
	*f.sessionExists = true
	path := filepath.Join(f.t.TempDir(), "ptmx")
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
}

func (f *pane20PtyFactory) Close() {}

// homeWithCountingTerminal is homeWithMockedTerminal's own recipe
// (terminal_and_keys_test.go), copied rather than parameterised (that
// helper's termCmdExec is fixed, and this file's own fence keeps
// terminal_and_keys_test.go untouched) with one addition: captureCalls
// counts every "tmux capture-pane" the Terminal tab's own cmdExec sees, the
// one call previewTickMsg used to make on every 100ms tick.
func homeWithCountingTerminal(t *testing.T) (*home, *int32) {
	t.Helper()
	sp := spinner.New()
	composer := ui.NewComposer()
	sessionPane := ui.NewSessionPane()
	sessionPane.SetComposer(composer)
	needsYouPane := ui.NewNeedsYouPane()
	needsYouPane.SetComposer(composer)

	var captureCalls int32
	sessionExists := false
	termCmdExec := cmd_test.MockCmdExec{
		RunFunc: func(c *exec.Cmd) error {
			if strings.Contains(c.String(), "has-session") {
				if sessionExists {
					return nil
				}
				return os.ErrNotExist
			}
			return nil
		},
		OutputFunc: func(c *exec.Cmd) ([]byte, error) {
			if strings.Contains(c.String(), "capture-pane") {
				atomic.AddInt32(&captureCalls, 1)
				return []byte("$ pwd\n" + t.TempDir()), nil
			}
			return []byte(""), nil
		},
	}
	ptyFactory := &pane20PtyFactory{t: t, sessionExists: &sessionExists}
	terminalPane := ui.NewTerminalPaneWithDeps(ptyFactory, termCmdExec)

	tw := ui.NewTabbedWindow(sessionPane, needsYouPane, terminalPane)
	tw.SetSize(120, 40)

	h := &home{
		ctx:          context.Background(),
		list:         ui.NewList(&sp, false),
		menu:         ui.NewMenu(),
		tabbedWindow: tw,
		sessionPane:  sessionPane,
		errBox:       ui.NewErrBox(),
		statusBox:    ui.NewStatusBox(),
		composer:     composer,
		cmdExec:      cmd.MakeExecutor(),
		laneTab:      ui.SessionTab,
		needsYouTab:  ui.NeedsYouTab,
		prevRowKind:  ui.RowKindTracked,
		appState:     &config.State{HelpScreensSeen: 1 << 2},
	}
	return h, &captureCalls
}

// TestPreviewTick_NoCaptureCall_WhenTerminalTabIdle is item (a): once the
// Terminal tab's own term_<lane> shell already exists (primed below, the
// same lazy-open the tab always did), a previewTickMsg - no animating lane,
// no selection change, no resize - must issue no further capture-pane call
// at all. Seen failing before this leg's fix: previewTickMsg's own case
// called m.instanceChanged() -> TabbedWindow.UpdateTerminal ->
// TerminalPane.UpdateContent on every 100ms tick unconditionally, so the
// capture count kept climbing by one per tick even with nothing to show.
func TestPreviewTick_NoCaptureCall_WhenTerminalTabIdle(t *testing.T) {
	h, captureCalls := homeWithCountingTerminal(t)
	h.list.SetExternal([]clarity.ExternalLane{{Name: "pane20-ext", WorkDir: t.TempDir()}})
	h.list.Down() // -> the external row

	pressGlobalKey(h, tea.KeyPressMsg{Code: tea.KeyTab})
	pressGlobalKey(h, tea.KeyPressMsg{Code: tea.KeyTab})
	require.Equal(t, ui.TerminalTab, h.tabbedWindow.GetActiveTab(), "test setup: Terminal must be the active tab")

	// Prime the term_ shell the same way the very first tick always did -
	// one direct instanceChanged call, exactly what a selection/tab-switch
	// site issues today (unchanged by this leg). instanceChanged returns a
	// nil Cmd on the ordinary success path (non-nil only on its own
	// handleError branch) - the capture happens synchronously inside the
	// call itself, not via the returned Cmd. The two Tab presses above
	// already switched the active tab through instanceChanged's own
	// terminalTarget-driven refresh (each key press's own change-signal
	// call site, unrelated to previewTickMsg), so the exact count reaching
	// here is not asserted - only that it stops moving from here on.
	h.instanceChanged()
	primed := atomic.LoadInt32(captureCalls)
	require.Greater(t, primed, int32(0), "priming must have captured at least once")

	for i := 0; i < 5; i++ {
		_, cmd := h.Update(previewTickMsg{})
		require.NotNil(t, cmd, "previewTickMsg must still self-reschedule")
	}

	require.Equal(t, primed, atomic.LoadInt32(captureCalls),
		"a previewTickMsg on an idle Terminal tab must issue no capture-pane call")
}

// TestPreviewTick_StillAdvancesSpinner_WorkingLane is item (b): a
// previewTickMsg must still advance the header/thinking-line spinner while
// the selected lane is genuinely working (open turn) - unchanged by this
// leg (TickSpinner/TickButterfly stayed on previewTickMsg's own 100ms case,
// only instanceChanged moved off it). This never gates spinner ticking on
// animation state, so it passes on both the pre- and post-fix code -
// included for item (b)'s own completeness as a non-regression check, not
// a fixed-bug reproduction the way (a) and (c) are.
func TestPreviewTick_StillAdvancesSpinner_WorkingLane(t *testing.T) {
	root := t.TempDir()
	t.Setenv(clarity.ClaudeProjectsRootEnvVar, root)

	h := newComposerTestHome(t)
	now := time.Now()
	selected := writeTrackedLaneFixture(t, root, "pane20-spinner-lane", now.Add(-time.Minute))
	h.list.AddInstance(selected)()
	openLaneTranscript(t, root, selected.Path, now)

	_, cmd := h.Update(sessionTickMsg{})
	require.NotNil(t, cmd)
	before := h.tabbedWindow.String()

	_, cmd = h.Update(previewTickMsg{})
	require.NotNil(t, cmd)
	after := h.tabbedWindow.String()

	require.NotEqual(t, before, after, "previewTickMsg must still advance the spinner for a working lane")
}

// TestSessionTick_RefreshesTerminalTab is item (c)'s first half: the
// preview content refresh now runs on sessionTickMsg's own 500ms cadence -
// a sessionTickMsg with the Terminal tab active must issue exactly one
// fresh capture-pane call.
func TestSessionTick_RefreshesTerminalTab(t *testing.T) {
	h, captureCalls := homeWithCountingTerminal(t)
	h.list.SetExternal([]clarity.ExternalLane{{Name: "pane20-ext2", WorkDir: t.TempDir()}})
	h.list.Down()
	pressGlobalKey(h, tea.KeyPressMsg{Code: tea.KeyTab})
	pressGlobalKey(h, tea.KeyPressMsg{Code: tea.KeyTab})
	require.Equal(t, ui.TerminalTab, h.tabbedWindow.GetActiveTab())

	h.instanceChanged()
	primed := atomic.LoadInt32(captureCalls)

	_, cmd := h.Update(sessionTickMsg{})
	require.NotNil(t, cmd, "sessionTickMsg must still self-reschedule")

	require.Equal(t, primed+1, atomic.LoadInt32(captureCalls),
		"sessionTickMsg must refresh the Terminal tab's own capture exactly once")
}

// TestInstanceChanged_StillRefreshesOnDemand is item (c)'s second half -
// "or on a change signal": the direct call sites (selection change, tab
// switch, instance start/kill - every other instanceChanged() call site in
// app.go, none of them touched by this leg) still refresh immediately, with
// no wait for either tick.
func TestInstanceChanged_StillRefreshesOnDemand(t *testing.T) {
	h, captureCalls := homeWithCountingTerminal(t)
	h.list.SetExternal([]clarity.ExternalLane{{Name: "pane20-ext3", WorkDir: t.TempDir()}})
	h.list.Down()
	pressGlobalKey(h, tea.KeyPressMsg{Code: tea.KeyTab})
	pressGlobalKey(h, tea.KeyPressMsg{Code: tea.KeyTab})

	before := atomic.LoadInt32(captureCalls)
	h.instanceChanged()
	require.Equal(t, before+1, atomic.LoadInt32(captureCalls),
		"a direct instanceChanged call (a selection/tab-switch change signal) must still refresh immediately")
}
