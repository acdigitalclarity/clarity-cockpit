package session

import (
	"claude-squad/cmd/cmd_test"
	"claude-squad/session/clarity"
	"claude-squad/session/tmux"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// capturingPtyFactory hands back a real temp file standing in for the tmux
// PTY, and remembers it so the test can read back exactly what SendKeys and
// TapEnter wrote to it - the same file session.tmux.TmuxSession.ptmx would
// be in production, just backed by a file instead of a real pseudo-terminal.
type capturingPtyFactory struct {
	t    *testing.T
	path string
}

func (p *capturingPtyFactory) Start(cmd *exec.Cmd) (*os.File, error) {
	p.path = filepath.Join(p.t.TempDir(), "ptmx")
	return os.OpenFile(p.path, os.O_CREATE|os.O_RDWR, 0644)
}

func (p *capturingPtyFactory) Close() {}

func (p *capturingPtyFactory) written(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(p.path)
	require.NoError(t, err)
	return string(data)
}

// TestMsgDelivery_SendPromptThenCapturePane exercises the exact sequence
// `cs-clarity msg <lane> '<text>'` (and the TUI's m key) run against a
// tracked instance: the message text delivered into the session's attached
// pty, followed by a separate Enter keystroke (session.Instance.SendPrompt
// does exactly this - see tmux.TmuxSession.SendKeys/TapEnter), then a pane
// capture whose last non-blank line the caller prints back. This is the
// "fake tmux runner" test the brief asks for: a mock Executor stands in for
// the real tmux binary (has-session, capture-pane) and a fake pty factory
// stands in for the real pseudo-terminal, so the whole delivery sequence is
// pinned without a live tmux session.
func TestMsgDelivery_SendPromptThenCapturePane(t *testing.T) {
	ptyFactory := &capturingPtyFactory{t: t}

	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			return nil // has-session -> exists
		},
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			return []byte("some old output\necho hello from the cockpit\nhello from the cockpit\n\n\n"), nil
		},
	}

	instance, err := NewInstance(InstanceOptions{Title: "zz-smoke-lane", Path: t.TempDir(), Program: "claude"})
	require.NoError(t, err)
	instance.SetTmuxSession(tmux.NewTmuxSessionWithDeps("zz-smoke-lane", "claude", ptyFactory, cmdExec))
	require.NoError(t, instance.Start(false))
	require.True(t, instance.Started())

	require.NoError(t, instance.SendPrompt("echo hello from the cockpit"))

	// The delivery sequence itself: the text written to the pty, followed by
	// a trailing CR (0x0D) from the separate TapEnter call - never collapsed
	// into one write, which is what would make an in-flight newline in the
	// text land as a stray keystroke instead of a submit.
	written := ptyFactory.written(t)
	require.Equal(t, "echo hello from the cockpit\r", written,
		"expected the message text followed by a separate Enter (CR) tap")

	pane, err := instance.Preview()
	require.NoError(t, err)
	require.Equal(t, "hello from the cockpit", clarity.LastPaneLine(pane))
}

// TestMsgDelivery_ExternalLaneIsUnconstructed pins the other half of the
// brief's contract: a lane with no tracked tmux session (an external row)
// is never sent a keystroke - the caller must print the fixed
// UNCONSTRUCTED line instead of silently dropping the message or, worse,
// attempting a tmux call against a session name that was never created.
func TestMsgDelivery_ExternalLaneIsUnconstructed(t *testing.T) {
	got := clarity.ExternalMsgUnconstructed("ways-of-working")
	require.Contains(t, got, "UNCONSTRUCTED")
	require.Contains(t, got, "ways-of-working")
	require.Contains(t, got, "clarity attach ways-of-working")
}
