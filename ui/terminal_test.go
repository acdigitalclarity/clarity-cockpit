package ui

import (
	"claude-squad/cmd/cmd_test"
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/session/tmux"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newMockTmuxSession creates a mock tmux session backed by MockCmdExec.
// The returned session will report as existing and support capture-pane commands.
func newMockTmuxSession(t *testing.T, name string, cmdExec cmd_test.MockCmdExec) *tmux.TmuxSession {
	t.Helper()
	ptyFactory := &MockPtyFactory{
		t:       t,
		cmdExec: cmdExec,
	}
	return tmux.NewTmuxSessionWithDeps(name, "bash", ptyFactory, cmdExec)
}

// recordingCmdExec is mockCmdExec's own shape plus a record of every command
// string it saw - the term_<lane> lifecycle tests (create on first view,
// reuse on the next, kill on Close) need to prove which tmux subcommands
// actually ran, not just that content came back.
type recordingCmdExec struct {
	captureContent string
	sessionExists  bool
	commands       []string
}

func newRecordingCmdExec(captureContent string, sessionExists bool) *recordingCmdExec {
	return &recordingCmdExec{captureContent: captureContent, sessionExists: sessionExists}
}

// exec returns the cmd_test.MockCmdExec this recorder backs - a fresh value
// each call (cheap struct of two closures), all sharing the same underlying
// *recordingCmdExec so every call's command is recorded in one place.
func (r *recordingCmdExec) exec() cmd_test.MockCmdExec {
	return cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			cmdStr := cmd.String()
			r.commands = append(r.commands, cmdStr)
			if strings.Contains(cmdStr, "has-session") {
				if r.sessionExists {
					return nil
				}
				return fmt.Errorf("session does not exist")
			}
			if strings.Contains(cmdStr, "new-session") {
				r.sessionExists = true
				return nil
			}
			return nil
		},
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			cmdStr := cmd.String()
			r.commands = append(r.commands, cmdStr)
			if strings.Contains(cmdStr, "capture-pane") {
				return []byte(r.captureContent), nil
			}
			return []byte(""), nil
		},
	}
}

func (r *recordingCmdExec) newSessionCount(name string) int {
	n := 0
	for _, c := range r.commands {
		if strings.Contains(c, "new-session") && strings.Contains(c, name) {
			n++
		}
	}
	return n
}

func (r *recordingCmdExec) sawCommand(substr string) bool {
	for _, c := range r.commands {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

// mockCmdExec returns a MockCmdExec that simulates a working tmux session.
// captureContent is returned for capture-pane commands.
func mockCmdExec(captureContent string, sessionExists bool) cmd_test.MockCmdExec {
	return cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			cmdStr := cmd.String()
			if strings.Contains(cmdStr, "has-session") {
				if sessionExists {
					return nil
				}
				return fmt.Errorf("session does not exist")
			}
			if strings.Contains(cmdStr, "new-session") {
				return nil
			}
			if strings.Contains(cmdStr, "kill-session") {
				return nil
			}
			return nil
		},
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			cmdStr := cmd.String()
			if strings.Contains(cmdStr, "capture-pane") {
				return []byte(captureContent), nil
			}
			return []byte(""), nil
		},
	}
}

// makeStartedInstance creates a minimal instance that reports as started
// with the given title - its own tmux session's capture-pane calls return
// captureContent, exactly the content the Terminal tab's tracked-row mirror
// (instance.Preview()) must come back with.
func makeStartedInstance(t *testing.T, title string, captureContent string) *session.Instance {
	t.Helper()
	workdir := t.TempDir()
	setupGitRepo(t, workdir)

	random := time.Now().UnixNano() % 10000000
	sessionName := fmt.Sprintf("test-terminal-%s-%d-%d", title, time.Now().UnixNano(), random)

	sessionCreated := false
	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			cmdStr := cmd.String()
			if strings.Contains(cmdStr, "has-session") {
				if sessionCreated {
					return nil
				}
				return fmt.Errorf("session does not exist")
			}
			if strings.Contains(cmdStr, "new-session") {
				sessionCreated = true
				return nil
			}
			return nil
		},
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			cmdStr := cmd.String()
			if strings.Contains(cmdStr, "capture-pane") {
				return []byte(captureContent), nil
			}
			return []byte(""), nil
		},
	}

	instance, err := session.NewInstance(session.InstanceOptions{
		Title:   sessionName,
		Path:    workdir,
		Program: "bash",
		AutoYes: false,
	})
	require.NoError(t, err)

	ptyFactory := &MockPtyFactory{
		t:       t,
		cmdExec: cmdExec,
	}
	tmuxSession := tmux.NewTmuxSessionWithDeps(sessionName, "bash", ptyFactory, cmdExec)
	instance.SetTmuxSession(tmuxSession)

	err = instance.Start(true)
	require.NoError(t, err)

	return instance
}

// TestTerminalPane_Tracked_MirrorsInstanceOwnPane is the brief's own "tab
// content selection by row kind" case for a TRACKED row: the Terminal tab
// shows the instance's OWN tmux pane (the dormant PreviewPane's own capture
// path), never a separate term_<title> shell - upstream's old behaviour,
// dropped by DECISIONS.md's tab ruling 3.
func TestTerminalPane_Tracked_MirrorsInstanceOwnPane(t *testing.T) {
	log.Initialize(false)
	defer log.Close()

	expectedContent := "$ whoami\nuser\n$ ls\nfile1.txt  file2.txt"

	instance := makeStartedInstance(t, "update-content", expectedContent)
	defer func() { _ = instance.Kill() }()

	tp := NewTerminalPane()
	tp.SetSize(80, 30)

	err := tp.UpdateContent(TerminalTarget{Kind: TerminalTargetTracked, Instance: instance})
	require.NoError(t, err)

	tp.mu.Lock()
	require.False(t, tp.fallback, "should not be in fallback mode after successful content update")
	require.Equal(t, expectedContent, tp.content, "content should match the instance's OWN captured pane output")
	require.Empty(t, tp.external, "a tracked row must never open a separate term_ shell - it mirrors its own session")
	tp.mu.Unlock()

	rendered := tp.String()
	require.Contains(t, rendered, "whoami", "rendered output should contain captured content")
}

// TestTerminalPane_Tracked_FallbackStates covers the same not-selected/
// paused/not-started fallback wording the pre-redesign pane already had -
// unchanged by the tracked-vs-external split, since these all still concern
// a tracked instance's own state.
func TestTerminalPane_Tracked_FallbackStates(t *testing.T) {
	log.Initialize(false)
	defer log.Close()

	tp := NewTerminalPane()
	tp.SetSize(80, 30)

	t.Run("nil instance", func(t *testing.T) {
		err := tp.UpdateContent(TerminalTarget{Kind: TerminalTargetTracked, Instance: nil})
		require.NoError(t, err)

		tp.mu.Lock()
		require.True(t, tp.fallback, "should be in fallback mode for nil instance")
		require.Contains(t, tp.fallbackText, "Select an instance", "fallback text should prompt to select instance")
		require.Empty(t, tp.content, "content should be empty in fallback mode")
		tp.mu.Unlock()
	})

	t.Run("paused instance", func(t *testing.T) {
		instance, err := session.NewInstance(session.InstanceOptions{
			Title:   "paused-inst",
			Path:    t.TempDir(),
			Program: "bash",
		})
		require.NoError(t, err)
		instance.SetStatus(session.Paused)

		err = tp.UpdateContent(TerminalTarget{Kind: TerminalTargetTracked, Instance: instance})
		require.NoError(t, err)

		tp.mu.Lock()
		require.True(t, tp.fallback, "should be in fallback mode for paused instance")
		require.Contains(t, tp.fallbackText, "paused", "fallback text should mention paused")
		tp.mu.Unlock()
	})

	t.Run("not started instance", func(t *testing.T) {
		instance, err := session.NewInstance(session.InstanceOptions{
			Title:   "not-started",
			Path:    t.TempDir(),
			Program: "bash",
		})
		require.NoError(t, err)

		err = tp.UpdateContent(TerminalTarget{Kind: TerminalTargetTracked, Instance: instance})
		require.NoError(t, err)

		tp.mu.Lock()
		require.True(t, tp.fallback, "should be in fallback mode for not-started instance")
		require.Contains(t, tp.fallbackText, "not started", "fallback text should indicate not started")
		tp.mu.Unlock()
	})
}

// TestTerminalPane_External_OpensTermShellLazilyThenReuses is the brief's
// own term_<lane> lifecycle test: the first UpdateContent for an external
// lane creates its term_<lane> session (upstream's own "one shell per
// instance" mechanism, reused here for a lane instead of a title); the
// second call for the SAME lane must not create a second one.
func TestTerminalPane_External_OpensTermShellLazilyThenReuses(t *testing.T) {
	log.Initialize(false)
	defer log.Close()

	rec := newRecordingCmdExec("$ pwd\n/scratch/lane-a", false)
	ptyFactory := &MockPtyFactory{t: t, cmdExec: rec.exec()}

	tp := newTerminalPaneWithDeps(ptyFactory, rec.exec())
	tp.SetSize(80, 30)

	target := TerminalTarget{Kind: TerminalTargetExternal, Lane: "scratchfix-lane-a", WorkDir: t.TempDir()}

	require.NoError(t, tp.UpdateContent(target))
	tp.mu.Lock()
	require.False(t, tp.fallback)
	require.Equal(t, "$ pwd\n/scratch/lane-a", tp.content)
	require.Contains(t, tp.external, "scratchfix-lane-a", "the lane's own term_<lane> session must be cached by lane name")
	tp.mu.Unlock()
	require.Equal(t, 1, rec.newSessionCount("term_scratchfix-lane-a"), "the first view must create exactly one term_ session")
	require.True(t, rec.sawCommand("term_scratchfix-lane-a"), "the tmux session must be named term_<lane>")

	// Second view of the same lane: reused, not recreated.
	require.NoError(t, tp.UpdateContent(target))
	require.Equal(t, 1, rec.newSessionCount("term_scratchfix-lane-a"), "a second view of the same lane must reuse the cached session, not create another")
}

// TestTerminalPane_None_ShowsRestingFrame is the brief's own "none =
// resting frame" case: nothing selected shows the splash's resting frame,
// the same as the Session tab (ui/session.go), never placeholder text.
func TestTerminalPane_None_ShowsRestingFrame(t *testing.T) {
	log.Initialize(false)
	defer log.Close()

	tp := NewTerminalPane()
	tp.SetSize(80, 30)
	tp.SetFleetCounts(5, 2)

	require.NoError(t, tp.UpdateContent(TerminalTarget{Kind: TerminalTargetNone}))

	out := tp.String()
	require.NotContains(t, out, "Select an instance", "the resting frame must never show the old tracked-row placeholder text")
	require.NotContains(t, out, "Terminal session not available")
}

// TestTerminalPane_Close_KillsExternalSessions is the brief's own lifecycle
// test's other half: Close (app.go's handleQuit) kills every cached
// term_<lane> session.
func TestTerminalPane_Close_KillsExternalSessions(t *testing.T) {
	log.Initialize(false)
	defer log.Close()

	rec := newRecordingCmdExec("content", false)
	ptyFactory := &MockPtyFactory{t: t, cmdExec: rec.exec()}
	tp := newTerminalPaneWithDeps(ptyFactory, rec.exec())
	tp.SetSize(80, 30)

	target := TerminalTarget{Kind: TerminalTargetExternal, Lane: "scratchfix-close", WorkDir: t.TempDir()}
	require.NoError(t, tp.UpdateContent(target))
	require.True(t, rec.sawCommand("new-session"))

	tp.Close()

	require.True(t, rec.sawCommand("kill-session"), "Close must kill the cached term_<lane> session")
	tp.mu.Lock()
	require.Empty(t, tp.external, "Close must clear the external session cache")
	tp.mu.Unlock()
}

// TestTerminalPane_Attach_External_ErrorsWithoutAView is the "Enter on an
// external row without a shell" case's own pane-level half: attaching to a
// lane that has never been viewed on the Terminal tab (no term_ session
// created yet) must error, never silently attach to nothing - app.go turns
// this error into the "no terminal for this lane yet" footer line.
func TestTerminalPane_Attach_External_ErrorsWithoutAView(t *testing.T) {
	log.Initialize(false)
	defer log.Close()

	tp := NewTerminalPane()
	tp.SetSize(80, 30)

	_, err := tp.Attach("never-viewed-lane")
	require.Error(t, err)
}

// TestTerminalPane_Caching preserves the pre-redesign suite's own
// switching-between-lanes case, adapted to two EXTERNAL lanes (the only
// row kind that still caches a session per name).
func TestTerminalPane_Caching(t *testing.T) {
	log.Initialize(false)
	defer log.Close()

	tp := NewTerminalPane()
	tp.SetSize(80, 30)

	content1 := "session-1-content"
	cmdExec1 := mockCmdExec(content1, true)
	ts1 := newMockTmuxSession(t, "cache-test-1", cmdExec1)

	content2 := "session-2-content"
	cmdExec2 := mockCmdExec(content2, true)
	ts2 := newMockTmuxSession(t, "cache-test-2", cmdExec2)

	tp.mu.Lock()
	tp.external["lane-1"] = &terminalSession{tmuxSession: ts1, workDir: t.TempDir()}
	tp.external["lane-2"] = &terminalSession{tmuxSession: ts2, workDir: t.TempDir()}
	tp.mu.Unlock()

	err := tp.UpdateContent(TerminalTarget{Kind: TerminalTargetExternal, Lane: "lane-1", WorkDir: t.TempDir()})
	require.NoError(t, err)
	tp.mu.Lock()
	require.Equal(t, content1, tp.content)
	tp.mu.Unlock()

	err = tp.UpdateContent(TerminalTarget{Kind: TerminalTargetExternal, Lane: "lane-2", WorkDir: t.TempDir()})
	require.NoError(t, err)
	tp.mu.Lock()
	require.Equal(t, content2, tp.content)
	tp.mu.Unlock()

	err = tp.UpdateContent(TerminalTarget{Kind: TerminalTargetExternal, Lane: "lane-1", WorkDir: t.TempDir()})
	require.NoError(t, err)
	tp.mu.Lock()
	require.Equal(t, content1, tp.content, "should get cached session content when switching back")
	require.Len(t, tp.external, 2, "both sessions should be cached")
	tp.mu.Unlock()
}

// TestTerminalPane_Scrolling preserves the pre-redesign suite's own
// scroll-mode case, adapted to an external lane (the tracked path's own
// scroll now goes through instance.PreviewFullHistory instead, exercised by
// TestTerminalPane_Tracked_MirrorsInstanceOwnPane's fixture indirectly via
// the shared enterScrollModeLocked code path).
func TestTerminalPane_Scrolling(t *testing.T) {
	log.Initialize(false)
	defer log.Close()

	const numLines = 100
	lines := make([]string, numLines)
	for i := range numLines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	fullContent := strings.Join(lines, "\n")

	cmdExec := mockCmdExec(fullContent, true)
	ts := newMockTmuxSession(t, "scroll-test", cmdExec)

	tp := NewTerminalPane()
	tp.SetSize(80, 30)
	tp.mu.Lock()
	tp.external["scroll-lane"] = &terminalSession{tmuxSession: ts, workDir: t.TempDir()}
	tp.target = TerminalTarget{Kind: TerminalTargetExternal, Lane: "scroll-lane", WorkDir: t.TempDir()}
	tp.mu.Unlock()

	require.False(t, tp.IsScrolling(), "should not be scrolling initially")

	err := tp.ScrollUp()
	require.NoError(t, err)
	require.True(t, tp.IsScrolling(), "should be in scroll mode after ScrollUp")

	viewContent := tp.viewport.View()
	require.NotEmpty(t, viewContent, "viewport should have content in scroll mode")

	err = tp.ScrollDown()
	require.NoError(t, err)
	require.True(t, tp.IsScrolling(), "should still be in scroll mode after ScrollDown")

	tp.ResetToNormalMode()
	require.False(t, tp.IsScrolling(), "should not be scrolling after ResetToNormalMode")
}

// TestTerminalPane_CloseForLane preserves the pre-redesign suite's own
// per-lane cleanup case (CloseForInstance, renamed CloseForLane - the
// external map it now operates on).
func TestTerminalPane_CloseForLane(t *testing.T) {
	log.Initialize(false)
	defer log.Close()

	tp := NewTerminalPane()
	tp.SetSize(80, 30)

	content := "some content"
	cmdExec := mockCmdExec(content, true)
	ts1 := newMockTmuxSession(t, "close-test-1", cmdExec)
	ts2 := newMockTmuxSession(t, "close-test-2", cmdExec)

	tp.mu.Lock()
	tp.external["lane-1"] = &terminalSession{tmuxSession: ts1, workDir: t.TempDir()}
	tp.external["lane-2"] = &terminalSession{tmuxSession: ts2, workDir: t.TempDir()}
	tp.mu.Unlock()

	tp.mu.Lock()
	require.Len(t, tp.external, 2)
	tp.mu.Unlock()

	tp.CloseForLane("lane-1")

	tp.mu.Lock()
	require.Len(t, tp.external, 1, "should have only 1 session after closing lane-1")
	_, exists := tp.external["lane-1"]
	require.False(t, exists, "lane-1 session should be removed")
	_, exists = tp.external["lane-2"]
	require.True(t, exists, "lane-2 session should still exist")
	tp.mu.Unlock()

	// Closing a non-existent lane should not panic
	tp.CloseForLane("non-existent")

	tp.mu.Lock()
	require.Len(t, tp.external, 1, "non-existent close should not affect existing sessions")
	tp.mu.Unlock()
}

// newTerminalPaneWithDeps builds a TerminalPane whose external term_<lane>
// sessions are created through the given pty factory/executor - TerminalPane's
// own newSession field (unexported, same package) is the injection seam,
// mirroring session/tmux's own NewTmuxSessionWithDeps pattern, so the
// create-on-first-view/reuse/kill-on-close lifecycle can be proven without
// ever touching a real tmux binary.
func newTerminalPaneWithDeps(ptyFactory tmux.PtyFactory, cmdExec cmd_test.MockCmdExec) *TerminalPane {
	tp := NewTerminalPane()
	tp.newSession = func(name, program string) *tmux.TmuxSession {
		return tmux.NewTmuxSessionWithDeps(name, program, ptyFactory, cmdExec)
	}
	return tp
}
