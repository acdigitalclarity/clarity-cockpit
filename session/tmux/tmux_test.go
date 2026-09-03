package tmux

import (
	cmd2 "claude-squad/cmd"
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"claude-squad/cmd/cmd_test"

	"github.com/stretchr/testify/require"
)

type MockPtyFactory struct {
	t *testing.T

	// Array of commands and the corresponding file handles representing PTYs.
	cmds  []*exec.Cmd
	files []*os.File
}

func (pt *MockPtyFactory) Start(cmd *exec.Cmd) (*os.File, error) {
	filePath := filepath.Join(pt.t.TempDir(), fmt.Sprintf("pty-%s-%d", pt.t.Name(), rand.Int31()))
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR, 0644)
	if err == nil {
		pt.cmds = append(pt.cmds, cmd)
		pt.files = append(pt.files, f)
	}
	return f, err
}

func (pt *MockPtyFactory) Close() {}

func NewMockPtyFactory(t *testing.T) *MockPtyFactory {
	return &MockPtyFactory{
		t: t,
	}
}

func TestSanitizeName(t *testing.T) {
	session := NewTmuxSession("asdf", "program")
	require.Equal(t, TmuxPrefix+"asdf", session.sanitizedName)

	session = NewTmuxSession("a sd f . . asdf", "program")
	require.Equal(t, TmuxPrefix+"asdf__asdf", session.sanitizedName)
}

func TestStartTmuxSession(t *testing.T) {
	ptyFactory := NewMockPtyFactory(t)

	created := false
	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			if strings.Contains(cmd.String(), "has-session") && !created {
				created = true
				return fmt.Errorf("session already exists")
			}
			return nil
		},
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			return []byte("output"), nil
		},
	}

	workdir := t.TempDir()
	session := newTmuxSession("test-session", "claude", ptyFactory, cmdExec)

	err := session.Start(workdir)
	require.NoError(t, err)
	require.Equal(t, 2, len(ptyFactory.cmds))
	require.Equal(t, fmt.Sprintf("tmux new-session -d -s claudesquad_test-session -c %s claude", workdir),
		cmd2.ToString(ptyFactory.cmds[0]))
	require.Equal(t, "tmux attach-session -t claudesquad_test-session",
		cmd2.ToString(ptyFactory.cmds[1]))

	require.Equal(t, 2, len(ptyFactory.files))

	// File should be closed.
	_, err = ptyFactory.files[0].Stat()
	require.Error(t, err)
	// File should be open
	_, err = ptyFactory.files[1].Stat()
	require.NoError(t, err)
}

// A tmux server that has gone away (reboot, crash, `tmux kill-server`) takes every session
// with it. attach-session against a missing session still forks successfully, so Restore
// has to check for the session itself or it reports success while attached to nothing.
func TestRestoreReturnsErrSessionNotFoundWhenSessionIsGone(t *testing.T) {
	ptyFactory := NewMockPtyFactory(t)
	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			if strings.Contains(cmd.String(), "has-session") {
				return fmt.Errorf("can't find session")
			}
			return nil
		},
	}

	session := NewTmuxSessionWithDeps("gone", "program", ptyFactory, cmdExec)
	err := session.Restore()

	require.ErrorIs(t, err, ErrSessionNotFound)
	require.Empty(t, ptyFactory.cmds, "should not have opened a PTY for a session that does not exist")
}

// Ctrl-q (0x11) never reaches VS Code's integrated terminal - it's bound to
// Quick Open View there - so the attach loop also detaches on Ctrl-] (0x1d,
// GS), a key no editor intercepts. Both are single-byte reads only; a 0x1d
// arriving as part of a longer read is ordinary input for the pane, not a
// detach request.
func TestIsDetachByte(t *testing.T) {
	require.True(t, isDetachByte(1, 0x11), "a lone ctrl-q byte should detach")
	require.True(t, isDetachByte(1, 0x1d), "a lone ctrl-] byte should detach exactly as ctrl-q does")
	require.False(t, isDetachByte(3, 0x1d), "a 0x1d byte inside a longer read is not a detach request")
	require.False(t, isDetachByte(1, 0x41), "an unrelated single byte should not detach")
}

func TestRestoreAttachesWhenSessionExists(t *testing.T) {
	ptyFactory := NewMockPtyFactory(t)
	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error { return nil },
	}

	session := NewTmuxSessionWithDeps("alive", "program", ptyFactory, cmdExec)
	require.NoError(t, session.Restore())
	require.Len(t, ptyFactory.cmds, 1)
	require.Contains(t, ptyFactory.cmds[0].String(), "attach-session")
}

// TestRunAttachCopyLoop_EOFBeforeCancel_YieldsEndedAndWritesNothingToStderr
// is board #317's own goroutine-level proof: a pty reader that hits EOF
// before the attach's context is cancelled - the program running inside
// tmux exited on its own, nobody pressed Ctrl-Q/Ctrl-] - must report
// ErrSessionEnded and must never write the old red "Session terminated
// without detaching" line to stderr. That line used to print AND leave the
// channel Attach returned unclosed forever, since nothing else was ever
// going to call Detach to close it - the actual hang the owner hit.
func TestRunAttachCopyLoop_EOFBeforeCancel_YieldsEndedAndWritesNothingToStderr(t *testing.T) {
	session := NewTmuxSessionWithDeps("ended", "program", NewMockPtyFactory(t), cmd_test.MockCmdExec{})

	r, w, err := os.Pipe()
	require.NoError(t, err)
	origStderr := os.Stderr
	os.Stderr = w

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	outcome := session.runAttachCopyLoop(ctx, io.Discard, strings.NewReader(""))

	require.NoError(t, w.Close())
	os.Stderr = origStderr
	captured, err := io.ReadAll(r)
	require.NoError(t, err)

	require.ErrorIs(t, outcome, ErrSessionEnded)
	require.Empty(t, captured, "the copy loop must never write to stderr")
}

// TestRunAttachCopyLoop_CtxAlreadyCancelled_YieldsNil is the normal-detach
// counterpart: Detach cancels the context before the copy loop's read
// unblocks, so the outcome is nil - a real Ctrl-Q/Ctrl-] detach, not an
// ended session.
func TestRunAttachCopyLoop_CtxAlreadyCancelled_YieldsNil(t *testing.T) {
	session := NewTmuxSessionWithDeps("detached", "program", NewMockPtyFactory(t), cmd_test.MockCmdExec{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	outcome := session.runAttachCopyLoop(ctx, io.Discard, strings.NewReader(""))
	require.NoError(t, outcome)
}

// TestDetach_AfterDetachSafely_DoesNotPanic reproduces a crash found on a
// live attach (leg report, board #317): the stdin-reading goroutine that
// calls Detach on a Ctrl-Q/Ctrl-] byte has no way to be cancelled
// (os.Stdin.Read is a blocking syscall, not a select), so it can outlive an
// ended-without-detach cycle's own DetachSafely teardown and call Detach a
// second time on the same TmuxSession once DetachSafely has already nilled
// t.ptmx/t.attachCh - "panic: error closing attach pty session: invalid
// argument" then "panic: close of nil channel" on HEAD before this fix.
func TestDetach_AfterDetachSafely_DoesNotPanic(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "ptmx")
	require.NoError(t, err)

	session := NewTmuxSessionWithDeps("stale-detach", "program", NewMockPtyFactory(t), cmd_test.MockCmdExec{})
	session.ptmx = tmpFile
	session.attachCh = make(chan struct{})
	session.wg = &sync.WaitGroup{}
	session.ctx, session.cancel = context.WithCancel(context.Background())

	require.NoError(t, session.DetachSafely())

	require.NotPanics(t, func() { session.Detach() },
		"a second, stale Detach call after DetachSafely has already torn the cycle down must be a no-op, never a panic")
}
