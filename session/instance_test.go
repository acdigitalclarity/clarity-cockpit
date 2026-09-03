package session

import (
	"claude-squad/cmd/cmd_test"
	"claude-squad/log"
	"claude-squad/session/tmux"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	log.Initialize(false)
	defer log.Close()
	os.Exit(m.Run())
}

// nullPtyFactory hands back a throwaway file instead of a real PTY.
type nullPtyFactory struct {
	t     *testing.T
	calls int
}

func (p *nullPtyFactory) Start(cmd *exec.Cmd) (*os.File, error) {
	p.calls++
	return os.OpenFile(filepath.Join(p.t.TempDir(), "pty"), os.O_CREATE|os.O_RDWR, 0644)
}

func (p *nullPtyFactory) Close() {}

// When the tmux server dies between runs, every session goes with it while the worktree
// and branch survive on disk. Restoring such an instance must park it as Paused so the
// user can resume it. Returning an error instead is not an option: LoadInstances aborts on
// the first failure, so one dead session would hide every other instance.
// See https://github.com/smtg-ai/claude-squad/issues/216.
func TestStartPausesInstanceWhenTmuxSessionNoLongerExists(t *testing.T) {
	ptyFactory := &nullPtyFactory{t: t}
	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			if strings.Contains(cmd.String(), "has-session") {
				return fmt.Errorf("can't find session")
			}
			return nil
		},
	}

	instance, err := NewInstance(InstanceOptions{Title: "revived", Path: t.TempDir(), Program: "claude"})
	require.NoError(t, err)
	instance.SetTmuxSession(tmux.NewTmuxSessionWithDeps("revived", "claude", ptyFactory, cmdExec))

	require.NoError(t, instance.Start(false), "a dead tmux session is recoverable, not a startup failure")
	require.Equal(t, Paused, instance.Status)
	require.True(t, instance.Started())
	require.Zero(t, ptyFactory.calls, "should not attach to a session that does not exist")
}

// The happy path is unchanged: an instance whose session survived comes back Running.
func TestStartRestoresInstanceWhenTmuxSessionSurvives(t *testing.T) {
	ptyFactory := &nullPtyFactory{t: t}
	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error { return nil },
	}

	instance, err := NewInstance(InstanceOptions{Title: "alive", Path: t.TempDir(), Program: "claude"})
	require.NoError(t, err)
	instance.SetTmuxSession(tmux.NewTmuxSessionWithDeps("alive", "claude", ptyFactory, cmdExec))

	require.NoError(t, instance.Start(false))
	require.Equal(t, Running, instance.Status)
	require.Equal(t, 1, ptyFactory.calls)
}

// startAwarePtyFactory flips *exists true the moment its PTY "starts" (the
// same shared-bool shape app/terminal_and_keys_test.go's termPtyFactory
// uses) so TmuxSession.Start's own existence-poll loop (session/tmux/
// tmux.go) sees the session as live immediately rather than running out its
// 2-second timeout - unlike nullPtyFactory above, which never touches the
// cmdExec's own session-existence state at all (fine for Restore-only
// tests, not for a first-time Start).
type startAwarePtyFactory struct {
	t      *testing.T
	exists *bool
}

func (p *startAwarePtyFactory) Start(cmd *exec.Cmd) (*os.File, error) {
	*p.exists = true
	return os.OpenFile(filepath.Join(p.t.TempDir(), "pty"), os.O_CREATE|os.O_RDWR, 0644)
}

func (p *startAwarePtyFactory) Close() {}

// sessionAwareCmdExec answers has-session/kill-session against a single
// shared *bool - the minimum a NoWorktree instance's Pause/Resume tests
// need to prove a session was actually closed or actually (re)started,
// rather than the unconditional-success mocks the two Restore-path tests
// above use.
type sessionAwareCmdExec struct {
	exists *bool
}

func (s *sessionAwareCmdExec) exec() cmd_test.MockCmdExec {
	return cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			cmdStr := cmd.String()
			if strings.Contains(cmdStr, "has-session") {
				if *s.exists {
					return nil
				}
				return fmt.Errorf("session does not exist")
			}
			if strings.Contains(cmdStr, "kill-session") {
				*s.exists = false
			}
			return nil
		},
	}
}

// newNoWorktreeFixture builds a Started, Running NoWorktree instance backed
// by startAwarePtyFactory/sessionAwareCmdExec - the cheapest fixture that
// exercises the REAL Start(true) path (not SetTmuxSession's
// already-started shortcut the Restore tests above use), so Pause/Resume
// below run against the exact NoWorktree instance shape clarity-attach
// produces (main.go's clarityAttachCmd).
func newNoWorktreeFixture(t *testing.T, title string) (*Instance, *bool) {
	t.Helper()
	exists := new(bool)
	ptyFactory := &startAwarePtyFactory{t: t, exists: exists}
	cmdExec := &sessionAwareCmdExec{exists: exists}

	instance, err := NewInstance(InstanceOptions{
		Title:      title,
		Path:       t.TempDir(),
		Program:    "claude",
		NoWorktree: true,
	})
	require.NoError(t, err)
	instance.SetTmuxSession(tmux.NewTmuxSessionWithDeps(title, "claude", ptyFactory, cmdExec.exec()))
	require.NoError(t, instance.Start(true))
	require.Equal(t, Running, instance.Status)
	require.False(t, instance.HasWorktree(), "a NoWorktree instance never gets a git worktree")
	return instance, exists
}

// TestNoWorktreeInstance_Pause_ClosesSessionNeverTouchesGitWorktree is
// slice 8 rule 1's own Pause test. i.gitWorktree is nil for the instance's
// whole life (NewInstance never sets it for NoWorktree, see
// FromInstanceData too) - a worktree call in this path would nil-pointer-
// panic rather than silently pass, so completing without a panic while
// actually closing the session IS the proof no git worktree function ran.
func TestNoWorktreeInstance_Pause_ClosesSessionNeverTouchesGitWorktree(t *testing.T) {
	instance, exists := newNoWorktreeFixture(t, "scratchfix-pause")

	require.NoError(t, instance.Pause())
	require.Equal(t, Paused, instance.Status)
	require.False(t, *exists, "Pause must actually close the tmux session, not just refuse")

	err := instance.Pause()
	require.Error(t, err, "pausing an already-paused instance still refuses, same as a worktree instance")
	require.Contains(t, err.Error(), "already paused")
	require.NotContains(t, err.Error(), "worktree")
}

// TestNoWorktreeInstance_Resume_StartsNewSessionNeverTouchesGitWorktree
// covers the "creates" half of rule 1's "creates (or reuses)": after Pause
// has closed the session, Resume must start a fresh one in the instance's
// own Path - never a worktree error, never a worktree operation (same
// nil-gitWorktree guarantee as the Pause test above).
func TestNoWorktreeInstance_Resume_StartsNewSessionNeverTouchesGitWorktree(t *testing.T) {
	instance, exists := newNoWorktreeFixture(t, "scratchfix-resume-new")
	require.NoError(t, instance.Pause())
	require.False(t, *exists)

	require.NoError(t, instance.Resume())
	require.Equal(t, Running, instance.Status)
	require.True(t, *exists, "Resume must start a fresh tmux session")
	require.False(t, instance.HasWorktree(), "still no git worktree after Resume")
}

// TestNoWorktreeInstance_Resume_ReusesExistingSessionNeverTouchesGitWorktree
// covers the "reuses" half: if the tmux session is still alive when Resume
// runs (Status Paused but the session itself never actually died), Resume
// must reconnect to it rather than erroring or creating a second one.
func TestNoWorktreeInstance_Resume_ReusesExistingSessionNeverTouchesGitWorktree(t *testing.T) {
	instance, exists := newNoWorktreeFixture(t, "scratchfix-resume-reuse")
	instance.SetStatus(Paused) // Status says paused; the session itself is left alive.
	require.True(t, *exists)

	require.NoError(t, instance.Resume())
	require.Equal(t, Running, instance.Status)
	require.True(t, *exists, "the existing session must still be alive, not killed and recreated")
}

// TestNoWorktreeInstance_Kill_NoGitWorktreeCleanup is rule 1's Kill case:
// i.gitWorktree is nil for a NoWorktree instance, so Kill's own git-cleanup
// block (session/instance.go's Kill, guarded by "if i.gitWorktree != nil")
// is a structural no-op here - Kill removes the tmux session only.
func TestNoWorktreeInstance_Kill_NoGitWorktreeCleanup(t *testing.T) {
	instance, exists := newNoWorktreeFixture(t, "scratchfix-kill")

	require.NoError(t, instance.Kill())
	require.False(t, *exists, "Kill must close the tmux session")
}
