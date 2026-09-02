// Package clarity: small helpers shared by the `cs-clarity msg` subcommand
// and the TUI's `m` key. The actual tmux delivery (send-keys, a brief
// pause, then Enter) already lives on session.Instance.SendPrompt - this
// file only adds the two things that path is missing: the fixed
// UNCONSTRUCTED line for a lane that runs outside tmux, and picking the
// last non-blank line out of a captured pane so the caller sees the
// message landed rather than a screenful of scrollback.
package clarity

import (
	"fmt"
	"strings"
)

// ExternalMsgUnconstructed renders the line printed by `cs-clarity msg` (and
// the TUI's m key) when the target lane resolves to an external row rather
// than a Claude Squad tracked instance. An external lane was started
// outside the cockpit (a bare Terminal/iTerm tab, not `clarity attach`), so
// there is no tmux session to deliver a keystroke into - this says so
// plainly instead of pretending the message landed.
func ExternalMsgUnconstructed(lane string) string {
	return fmt.Sprintf("msg: UNCONSTRUCTED - %s runs outside tmux; open it with clarity attach %s", lane, lane)
}

// LastPaneLine returns the last non-blank line of a captured tmux pane -
// what a caller of `msg` wants echoed back to confirm delivery landed. A
// raw capture-pane output is usually padded with trailing blank lines from
// the pane's unused height, so a bare "last line" would almost always be
// empty; this walks backward past those to find the last line that
// actually has content. Returns "" for a capture that is entirely blank.
func LastPaneLine(pane string) string {
	lines := strings.Split(pane, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimRight(lines[i], " \t\r")
		if line != "" {
			return line
		}
	}
	return ""
}
