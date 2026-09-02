// Package clarity: this file is the composer's external-lane send path
// (design/cockpit-pane/DECISIONS.md item 3) - a lane the cockpit does not
// track has no tmux session to deliver a keystroke into (ExternalMsgUncon-
// structed's own reasoning, msg.go), so the composer copies the text to the
// system clipboard instead and says so, never claiming a delivery it
// cannot make.
package clarity

import (
	"bytes"
	"claude-squad/cmd"
	"os/exec"
)

// CopyToClipboard copies text to the macOS clipboard via pbcopy, run
// through executor - the same cmd.Executor seam session/tmux already uses
// for the real tmux binary - so a caller can inject a fake in tests without
// ever touching the real clipboard.
func CopyToClipboard(executor cmd.Executor, text string) error {
	command := exec.Command("pbcopy")
	command.Stdin = bytes.NewBufferString(text)
	return executor.Run(command)
}
