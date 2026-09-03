// Package app: this file tests slice 6 (Terminal tab wiring at the app
// level - Enter's own row-kind branching), slice 7 (the c copy / o open
// folder keys) and slice 8 (attached/NoWorktree instances - Enter/r's own
// honest handling, o's Path fallback) of design/cockpit-pane/DECISIONS.md.
package app

import (
	"claude-squad/cmd"
	"claude-squad/cmd/cmd_test"
	"claude-squad/config"
	"claude-squad/session"
	"claude-squad/session/clarity"
	"claude-squad/session/tmux"
	"claude-squad/ui"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

// termPtyFactory stands in for a live tmux PTY the same way
// capturingPtyFactory (composer_test.go) does - a temp file - but also
// flips sessionExists to true on a successful Start, which the has-session
// mock below reads, so tmux.Session.Start's own existence-poll loop exits
// immediately instead of running out its 2-second timeout. fail simulates
// the PTY never starting at all (a real tmux new-session failure).
type termPtyFactory struct {
	t             *testing.T
	sessionExists *bool
	fail          bool
}

func (f *termPtyFactory) Start(cmd *exec.Cmd) (*os.File, error) {
	if f.fail {
		return nil, fmt.Errorf("pty start failed (simulated)")
	}
	*f.sessionExists = true
	path := filepath.Join(f.t.TempDir(), "ptmx")
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
}

func (f *termPtyFactory) Close() {}

// homeWithMockedTerminal builds a *home the same shape as
// newComposerTestHome (composer_test.go), except its TabbedWindow's
// TerminalPane is built with ui.NewTerminalPaneWithDeps - so an external
// row's term_<lane> shell is created through a mocked PTY/executor, never
// the real tmux binary. newComposerTestHome itself is left untouched (its
// own tests never exercise the Terminal tab's external-lane path, so it has
// no need for this).
//
// termStartFails, when true, makes the term_<lane> shell's own PTY fail to
// start - the only way DECISIONS.md slice 6's "no terminal for this lane
// yet" branch is actually reachable through the UI: the shell is opened
// LAZILY the moment the Terminal tab is viewed for a lane (the same key
// press/tick that would otherwise create it), so the only gap between
// "viewed" and "has a session" is the underlying tmux session genuinely
// failing to start.
func homeWithMockedTerminal(t *testing.T, termStartFails bool) *home {
	t.Helper()
	sp := spinner.New()
	composer := ui.NewComposer()
	sessionPane := ui.NewSessionPane()
	sessionPane.SetComposer(composer)
	needsYouPane := ui.NewNeedsYouPane()
	needsYouPane.SetComposer(composer)

	sessionExists := false
	termCmdExec := cmd_test.MockCmdExec{
		RunFunc: func(c *exec.Cmd) error {
			cmdStr := c.String()
			if strings.Contains(cmdStr, "has-session") {
				if sessionExists {
					return nil
				}
				return fmt.Errorf("session does not exist")
			}
			return nil
		},
		OutputFunc: func(c *exec.Cmd) ([]byte, error) {
			if strings.Contains(c.String(), "capture-pane") {
				return []byte("$ pwd\n" + t.TempDir()), nil
			}
			return []byte(""), nil
		},
	}
	ptyFactory := &termPtyFactory{t: t, sessionExists: &sessionExists, fail: termStartFails}
	terminalPane := ui.NewTerminalPaneWithDeps(ptyFactory, termCmdExec)

	tw := ui.NewTabbedWindow(sessionPane, needsYouPane, terminalPane)
	tw.SetSize(120, 40)

	return &home{
		ctx:          context.Background(),
		list:         ui.NewList(&sp, false),
		menu:         ui.NewMenu(),
		tabbedWindow: tw,
		errBox:       ui.NewErrBox(),
		statusBox:    ui.NewStatusBox(),
		composer:     composer,
		cmdExec:      cmd.MakeExecutor(),
		laneTab:      ui.SessionTab,
		needsYouTab:  ui.NeedsYouTab,
		prevRowKind:  ui.RowKindTracked,
		// HelpScreensSeen already carries helpTypeInstanceAttach's own mask
		// (app/help.go, 1<<2) - Enter's showHelpScreen call then takes its
		// "already seen" path and calls onDismiss immediately, synchronously,
		// rather than opening the overlay (and, critically, never calling
		// config.SaveState against this machine's real ~/.claude-squad/
		// state.json, which the "not yet seen" path would).
		appState: &config.State{HelpScreensSeen: 1 << 2},
	}
}

// TestKeyEnter_ExternalRow_TerminalTab_NoShellYet_ShowsFooterLine is the
// brief's own TESTS FIRST case: Enter on an external row, Terminal tab
// active, when that lane's term_ shell failed to open - the footer shows
// the "no terminal yet" line, never a claimed attach to the owner's own
// terminal.
//
// Board slice 9: Enter no longer runs the attach inline inside Update() (the
// bug being fixed here was exactly that inline blocking read of os.Stdin
// racing bubbletea's own reader - see app/attach_exec.go). Enter now returns
// a tea.Cmd wrapping tea.Exec, which only a real Program can run (it hands
// bubbletea's own terminal reader off and back on - see the passing
// end-to-end proof in the leg report, not reproducible headless). What IS
// unit-testable here, and is tested in the two steps below: (1) Enter
// dispatches something (a non-nil Cmd) rather than silently no-op'ing, and
// (2) once the executor's result comes back as a terminalAttachFinishedMsg
// (exactly what tea.Exec's callback would send after a failed
// AttachTerminal, termStartFails=true here), Update() sets the same footer
// line the old inline code used to set synchronously.
func TestKeyEnter_ExternalRow_TerminalTab_NoShellYet_ShowsFooterLine(t *testing.T) {
	h := homeWithMockedTerminal(t, true)
	h.list.SetExternal([]clarity.ExternalLane{{Name: "scratchfix-ext", WorkDir: t.TempDir()}})
	h.list.Down() // -> the external row

	pressGlobalKey(h, tea.KeyPressMsg{Code: tea.KeyTab})
	pressGlobalKey(h, tea.KeyPressMsg{Code: tea.KeyTab})
	require.Equal(t, ui.TerminalTab, h.tabbedWindow.GetActiveTab())

	_, cmd := pressGlobalKey(h, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd, "Enter on an attachable external row must dispatch the attach, not no-op")

	// Simulate the framework delivering the executor's outcome: this is
	// exactly the terminalAttachFinishedMsg attachTerminalCmd's callback
	// sends once tea.Exec's blocking Run() returns the AttachTerminal error
	// (attach_exec.go).
	_, _ = h.Update(terminalAttachFinishedMsg{err: fmt.Errorf("no session")})

	require.Equal(t, "no terminal for this lane yet: press tab to Terminal first", h.statusText)
	require.Equal(t, stateDefault, h.state)
}

// TestAttachExec_Run_BlocksUntilChannelClosesThenReturnsNil proves the new
// tea.Exec adapter (attach_exec.go) keeps the current blocking contract:
// Run() does not return until the started attach's own channel closes
// (session/tmux/tmux.go closes it on Detach) - the same handoff moment the
// old inline `<-ch` used to wait on inside Update().
func TestAttachExec_Run_BlocksUntilChannelClosesThenReturnsNil(t *testing.T) {
	ch := make(chan struct{})
	started := make(chan struct{})
	a := &attachExec{start: func() (chan struct{}, error) {
		close(started)
		return ch, nil
	}}

	done := make(chan error, 1)
	go func() { done <- a.Run() }()

	<-started
	select {
	case err := <-done:
		t.Fatalf("Run() returned early with %v before the channel closed", err)
	default:
	}

	close(ch)
	require.NoError(t, <-done)
}

// TestAttachExec_Run_PropagatesStartError proves a start failure (the
// tracked-instance-not-started / AttachTerminal-session-missing case) is
// returned immediately, without ever waiting on a channel that will never
// close.
func TestAttachExec_Run_PropagatesStartError(t *testing.T) {
	wantErr := fmt.Errorf("scratchfix start failed")
	a := &attachExec{start: func() (chan struct{}, error) {
		return nil, wantErr
	}}
	require.ErrorIs(t, a.Run(), wantErr)
}

// TestInstanceAttachFinishedMsg_Error routes the tracked-instance attach
// callback's error through the same handleError path handleKeyPress's old
// inline `if err != nil { m.handleError(err) }` used.
func TestInstanceAttachFinishedMsg_Error(t *testing.T) {
	h := homeWithMockedTerminal(t, false)
	h.state = stateHelp

	wantErr := fmt.Errorf("scratchfix attach failed")
	_, _ = h.Update(instanceAttachFinishedMsg{err: wantErr})

	require.Equal(t, stateDefault, h.state)
	require.True(t, h.hasErr)
}

// TestInstanceAttachFinishedMsg_Success proves a clean detach (ctrl-q, no
// error) returns to the default state without raising an error.
func TestInstanceAttachFinishedMsg_Success(t *testing.T) {
	h := homeWithMockedTerminal(t, false)
	h.state = stateHelp

	_, _ = h.Update(instanceAttachFinishedMsg{err: nil})

	require.Equal(t, stateDefault, h.state)
	require.False(t, h.hasErr)
}

// TestKeyCopy_ComposerOpen_CopiesComposerText proves c copies the
// composer's CURRENT text when it is open, over the Needs-you fallback.
func TestKeyCopy_ComposerOpen_CopiesComposerText(t *testing.T) {
	h := newComposerTestHome()
	var copiedStdin string
	h.cmdExec = cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			buf := make([]byte, 4096)
			n, _ := cmd.Stdin.Read(buf)
			copiedStdin = string(buf[:n])
			return nil
		},
	}
	h.composer.Open("lane-a", false)
	h.composer.Type("draft reply text")

	cmd := h.handleCopy()
	require.NotNil(t, cmd, "setStatus's own 4-second auto-hide timer Cmd")
	require.Equal(t, "draft reply text", copiedStdin)
	require.Equal(t, "copied", h.statusText)
}

// TestKeyCopy_NeedsYouRowSelected_CopiesFeedLine proves c copies the
// selected Needs-you row's own title and number (clarity.FeedLine - the
// exact text the row itself renders) when the composer is closed.
func TestKeyCopy_NeedsYouRowSelected_CopiesFeedLine(t *testing.T) {
	h := newComposerTestHome()
	h.list.SetNeedsYou([]clarity.FeedItem{{Lane: "#277", Title: "Owner: one settings act"}}, "")
	h.list.Up() // the sole tracked-group cursor wraps to the sole Needs-you row (empty list otherwise)

	var copiedStdin string
	h.cmdExec = cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			buf := make([]byte, 4096)
			n, _ := cmd.Stdin.Read(buf)
			copiedStdin = string(buf[:n])
			return nil
		},
	}

	h.handleCopy()
	require.Equal(t, "#277 - Owner: one settings act", copiedStdin)
	require.Equal(t, "copied", h.statusText)
}

// TestKeyOpenFolder_ExternalRow_OpensWorkDir proves o opens an external
// lane's own WorkDir with macOS `open`, through the stubbed executor, and
// shows the "opened <path>" footer.
func TestKeyOpenFolder_ExternalRow_OpensWorkDir(t *testing.T) {
	h := newComposerTestHome()
	workDir := t.TempDir()
	h.list.SetExternal([]clarity.ExternalLane{{Name: "scratchfix-ext", WorkDir: workDir}})
	h.list.Down() // -> the external row

	var openedArgs []string
	h.cmdExec = cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			openedArgs = cmd.Args
			return nil
		},
	}

	h.handleOpenFolder()

	require.Equal(t, []string{"open", workDir}, openedArgs)
	require.Equal(t, "opened "+workDir, h.statusText)
}

// TestKeyOpenFolder_NothingSelected_NoOp proves o is a safe no-op (no
// command run, no footer shown) when neither a tracked instance nor an
// external lane is selected.
func TestKeyOpenFolder_NothingSelected_NoOp(t *testing.T) {
	h := newComposerTestHome()

	ran := false
	h.cmdExec = cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			ran = true
			return nil
		},
	}

	cmd := h.handleOpenFolder()
	require.Nil(t, cmd)
	require.False(t, ran, "no folder to open means no `open` command must run")
	require.Empty(t, h.statusText)
}

// noWorktreeAppFixture builds a Started, Paused (no live session) NoWorktree
// instance - the clarity-attach shape (main.go's clarityAttachCmd) - and
// adds it as the sole tracked row in h.list, for the slice 8 tests below.
// The returned *bool tracks the mocked tmux session's own existence, the
// same shared-bool shape session/instance_test.go's sessionAwareCmdExec
// uses, so Resume (rule 2's r gate) can be proven to actually start a
// session rather than just flip Status.
func noWorktreeAppFixture(t *testing.T, h *home, title string) (inst *session.Instance, exists *bool) {
	t.Helper()
	sessionExists := false
	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			cmdStr := cmd.String()
			if strings.Contains(cmdStr, "has-session") {
				if sessionExists {
					return nil
				}
				return fmt.Errorf("session does not exist")
			}
			if strings.Contains(cmdStr, "kill-session") {
				sessionExists = false
			}
			return nil
		},
	}
	ptyFactory := &termPtyFactory{t: t, sessionExists: &sessionExists}

	inst, err := session.NewInstance(session.InstanceOptions{
		Title:      title,
		Path:       t.TempDir(),
		Program:    "claude",
		NoWorktree: true,
	})
	require.NoError(t, err)
	inst.SetTmuxSession(tmux.NewTmuxSessionWithDeps(title, "claude", ptyFactory, cmdExec))
	require.NoError(t, inst.Start(true))
	require.NoError(t, inst.Pause())
	require.False(t, inst.TmuxAlive(), "fixture must start with no live session, the clarity-attach-paused shape")

	h.list.AddInstance(inst)()
	return inst, &sessionExists
}

// TestKeyEnter_TrackedNoWorktreeInstance_NoSession_ShowsFooterLine is
// slice 8 rule 2's own Enter test, seen failing against the pre-fix code
// (which silently no-op'd): Enter on a tracked NoWorktree row with no live
// session must show the "runs in your own terminal" footer, never a silent
// no-op and never a resume prompt.
func TestKeyEnter_TrackedNoWorktreeInstance_NoSession_ShowsFooterLine(t *testing.T) {
	h := newComposerTestHome()
	noWorktreeAppFixture(t, h, "scratchfix-attached")

	pressGlobalKey(h, tea.KeyPressMsg{Code: tea.KeyEnter})

	require.Equal(t, "this lane runs in your own terminal; tab to Terminal for a shell in its folder", h.statusText)
}

// TestKeyResume_NoWorktreeInstance_Idle_ResumesSession is slice 8 rule 2's
// "allowed" branch: an idle transcript means the owner's own terminal
// looks abandoned, so r is allowed to start a fresh session.
func TestKeyResume_NoWorktreeInstance_Idle_ResumesSession(t *testing.T) {
	h := newComposerTestHome()
	inst, exists := noWorktreeAppFixture(t, h, "scratchfix-idle")
	inst.SetLaneState(clarity.StateIdle, time.Now(), true)

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'r', Text: "r"})

	require.True(t, *exists, "an idle lane's r must actually start a session")
	require.Equal(t, session.Running, inst.Status)
	require.Empty(t, h.statusText, "a successful resume shows no refusal footer")
}

// TestKeyResume_NoWorktreeInstance_Working_RefusesWithFooter is slice 8
// rule 2's "refused" branch: a lane whose transcript reads working (still
// live in the owner's own terminal) must never get a second session - r
// shows the "nothing to resume" footer instead, and starts nothing.
func TestKeyResume_NoWorktreeInstance_Working_RefusesWithFooter(t *testing.T) {
	h := newComposerTestHome()
	inst, exists := noWorktreeAppFixture(t, h, "scratchfix-working")
	inst.SetLaneState(clarity.StateWorking, time.Now(), true)

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'r', Text: "r"})

	require.False(t, *exists, "a working lane must never get a second session")
	require.Equal(t, session.Paused, inst.Status)
	require.Equal(t, "the lane is live in your own terminal; nothing to resume", h.statusText)
}

// TestKeyResume_NoWorktreeInstance_NoTranscriptYet_RefusesWithFooter is
// rule 2's fail-closed default: no transcript read at all (GetLaneState's
// own ok=false, before the first feed tick has run) refuses, the same as
// an actively working lane - never assumed idle just because nothing has
// been read yet.
func TestKeyResume_NoWorktreeInstance_NoTranscriptYet_RefusesWithFooter(t *testing.T) {
	h := newComposerTestHome()
	_, exists := noWorktreeAppFixture(t, h, "scratchfix-no-transcript")

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'r', Text: "r"})

	require.False(t, *exists)
	require.Equal(t, "the lane is live in your own terminal; nothing to resume", h.statusText)
}

// TestKeyOpenFolder_TrackedNoWorktreeInstance_OpensItsPath is slice 8
// rule 4's own o test, seen failing against the pre-fix code
// (GetWorktreePath returns "" for a NoWorktree instance, so o silently did
// nothing): o on a NoWorktree tracked row must open its own Path, falling
// back off the (always-empty, for this instance) worktree path.
func TestKeyOpenFolder_TrackedNoWorktreeInstance_OpensItsPath(t *testing.T) {
	h := newComposerTestHome()
	inst, _ := noWorktreeAppFixture(t, h, "scratchfix-openfolder")

	var openedArgs []string
	h.cmdExec = cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			openedArgs = cmd.Args
			return nil
		},
	}

	h.handleOpenFolder()

	require.Equal(t, []string{"open", inst.Path}, openedArgs)
	require.Equal(t, "opened "+inst.Path, h.statusText)
}
