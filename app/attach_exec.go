package app

import (
	"io"

	tea "charm.land/bubbletea/v2"
)

// attachExec adapts one of the cockpit's own attach flows - list.Attach or
// tabbedWindow.AttachTerminal, both of which start the tmux handoff and hand
// back a channel that closes on Detach (session/tmux/tmux.go Attach/Detach) -
// to bubbletea v2's tea.Exec (charm.land/bubbletea/v2 exec.go ExecCommand).
//
// Root cause (board slice 9, 3 Sep): nothing in this codebase, in either the
// v1 or the v2 Bubble Tea build, ever called Program.ReleaseTerminal/
// RestoreTerminal around an attach. The attach onDismiss callback used to run
// its blocking `<-ch` wait directly inside Update(), while bubbletea v2's own
// input reader (charm.land/bubbletea/v2 tty.go readLoop, started by
// initInputReader and left running until releaseTerminal cancels it) kept
// reading the same stdin fd concurrently with tmux.go Attach's own raw
// os.Stdin.Read() loop - two uncoordinated readers racing for every byte.
// That is the intermittent "keystrokes do nothing, ctrl-q sometimes does
// nothing" reported 3 Sep, reproduced here before this fix: a burst of typed
// text plus an immediate ctrl-q needed a SECOND ctrl-q to detach.
//
// Routing the attach through tea.Exec instead (Run() blocks on the same
// channel; SetStdin/SetStdout/SetStderr are no-ops since tmux.go already
// talks to os.Stdin/os.Stdout directly, which is what p.input/p.output
// default to since Run never passes tea.WithInput/tea.WithOutput) makes the
// framework call p.releaseTerminal before Run and p.RestoreTerminal after -
// the idiomatic v2 handoff documented on ExecProcess, generalised to a
// command that isn't a plain *exec.Cmd.
type attachExec struct {
	start func() (chan struct{}, error)
}

func (a *attachExec) Run() error {
	ch, err := a.start()
	if err != nil {
		return err
	}
	<-ch
	return nil
}

func (a *attachExec) SetStdin(io.Reader)  {}
func (a *attachExec) SetStdout(io.Writer) {}
func (a *attachExec) SetStderr(io.Writer) {}

// instanceAttachFinishedMsg reports that a tracked instance's tea.Exec attach
// (m.list.Attach) has returned - the tmux session was detached (ctrl-q) or
// never started (err set).
type instanceAttachFinishedMsg struct{ err error }

// terminalAttachFinishedMsg reports that a Terminal tab external-lane attach
// (m.tabbedWindow.AttachTerminal) has returned.
type terminalAttachFinishedMsg struct{ err error }

// attachInstanceCmd wraps a tracked instance's Attach in tea.Exec, reporting
// back via instanceAttachFinishedMsg.
func attachInstanceCmd(start func() (chan struct{}, error)) tea.Cmd {
	return tea.Exec(&attachExec{start: start}, func(err error) tea.Msg {
		return instanceAttachFinishedMsg{err: err}
	})
}

// attachTerminalCmd wraps an external lane's Terminal-tab attach in
// tea.Exec, reporting back via terminalAttachFinishedMsg.
func attachTerminalCmd(start func() (chan struct{}, error)) tea.Cmd {
	return tea.Exec(&attachExec{start: start}, func(err error) tea.Msg {
		return terminalAttachFinishedMsg{err: err}
	})
}
