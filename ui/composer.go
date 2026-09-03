// Package ui: this file is the wired composer (design/cockpit-pane/
// DECISIONS.md item 3, slice 5) - the inline "message <lane>" box the
// Session and Needs-you panes both draw at their own foot. Session pane
// slice 3b already drew this box statically (session.go's
// renderComposerLines, see its own doc comment); this file is the model
// behind it becoming interactive: m opens it focused with a cursor, enter
// sends, esc closes. One Composer is shared by both tabs (app.go owns it),
// since only one row can ever be the current send target at a time.
package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// ComposerFootIdle/ComposerFootEditing are the composer foot's two steady-
// state strings, verbatim (the brief's own words) - "m message" while
// closed, "enter send · esc cancel" while open and being typed into. A
// third, transient state (a just-sent result) overrides the foot with
// whatever SetResult was given until the composer is opened again.
const (
	ComposerFootIdle    = "m message"
	ComposerFootEditing = "enter send · esc cancel"
)

// NoLaneLabel is the composer's own title/target text (and the Needs-you
// header line 2's own fallback, ui/needsyou.go) for a Needs-you row whose
// raising lane resolved to neither the board card's own "## Lane" section
// nor the issue's "lane:" label (board #280, slice 5b, DEFECT 2) - the
// composer still opens, but names no send target and enter delivers
// nothing (app.go's stateMsg Enter handler checks Lane() == "" itself).
const NoLaneLabel = "(no lane on this row)"

// Composer is the shared model behind both panes' inline message box: is it
// open, what has been typed, and (once a send resolves) the result text the
// foot shows instead of the idle/editing prompt.
type Composer struct {
	open bool
	text string

	// lane/isExternal are the send target, captured at Open() time so the
	// send still goes to the right row even if the list's selection moves
	// before the composer closes (the same reasoning the old textInput-
	// Overlay-based m key already carried in its own msgTargetLane/
	// msgTargetExternal fields).
	lane       string
	isExternal bool

	// result is the foot's transient post-send text ("sent · landed
	// 14:32:07" / "copied · ..."), cleared the next time Open is called.
	result string
}

// NewComposer returns a closed, empty composer.
func NewComposer() *Composer {
	return &Composer{}
}

// IsOpen reports whether the composer is currently focused for typing.
func (c *Composer) IsOpen() bool {
	return c.open
}

// Open focuses the composer on lane, clearing any previous text or result -
// a fresh compose always starts from an empty line, even if the last one
// was left mid-type by an esc (Close already clears text; this guards the
// case where the previous session ended by result instead).
func (c *Composer) Open(lane string, isExternal bool) {
	c.open = true
	c.text = ""
	c.result = ""
	c.lane = lane
	c.isExternal = isExternal
}

// Close exits editing without sending, clearing the typed text but keeping
// the target/result fields (harmless once closed - Open resets them on the
// next use).
func (c *Composer) Close() {
	c.open = false
	c.text = ""
}

// Lane/IsExternal report the send target captured when Open was last
// called.
func (c *Composer) Lane() string     { return c.lane }
func (c *Composer) IsExternal() bool { return c.isExternal }
func (c *Composer) Value() string    { return c.text }
func (c *Composer) HasResult() bool  { return c.result != "" }
func (c *Composer) Result() string   { return c.result }

// Type appends s (a key press's own printable text) to the composer's
// single-line buffer.
func (c *Composer) Type(s string) {
	c.text += s
}

// Backspace removes the last rune of the typed text, rune-aware so a
// multi-byte character is never split.
func (c *Composer) Backspace() {
	r := []rune(c.text)
	if len(r) == 0 {
		return
	}
	c.text = string(r[:len(r)-1])
}

// SetResult records the foot's transient post-send text and ends editing -
// the composer stays visually present (Open() still returns lane/
// isExternal for the box's own title) until the next Open call replaces it.
func (c *Composer) SetResult(text string) {
	c.result = text
	c.open = false
	c.text = ""
}

// composerCursor is the mock-up's own cursor glyph (PANE-MOCKUP-164x45.md:
// "▸ █").
const composerCursor = "█"

// copyOnlySuffix marks the composer title on a lane with no live tmux
// session to deliver into (cockpit pane-10 walkthrough DEFECT 1) - shown
// from the moment the box opens, before any text is typed, so the owner
// knows enter will copy rather than send.
const copyOnlySuffix = " · copy only"

// Render draws the composer's three-line box at the given width: the
// "message <lane>" title, the typed text with a trailing cursor while
// open (a bare cursor when empty), and the foot - idle, editing, or the
// transient result text SetResult last recorded. lane is the CURRENT
// selection's own target name, used for the title/prompt whenever the
// composer is not already open on a captured target (so the inert box
// still names whichever row would receive a message right now, matching
// slice 3b's own static behaviour before m is ever pressed).
func (c *Composer) Render(width int, lane string) []string {
	displayLane := lane
	displayExternal := false
	if c.open || c.result != "" {
		displayLane = c.lane
		displayExternal = c.isExternal
	}
	noLane := displayLane == ""
	if noLane {
		displayLane = NoLaneLabel
	}
	title := fmt.Sprintf(" message %s ", displayLane)
	if displayExternal && !noLane {
		title = fmt.Sprintf(" message %s%s ", displayLane, copyOnlySuffix)
	}
	top := "┌" + title + strings.Repeat("─", maxInt0(width-2-lipgloss.Width(title))) + "┐"

	prompt := "▸ " + composerCursor
	if c.open {
		prompt = "▸ " + c.text + composerCursor
	}
	mid := "│ " + prompt + strings.Repeat(" ", maxInt0(width-4-lipgloss.Width(prompt))) + " │"

	foot := " " + ComposerFootIdle + " "
	switch {
	case c.open:
		foot = " " + ComposerFootEditing + " "
	case c.result != "":
		foot = " " + c.result + " "
	}
	bottom := "└" + strings.Repeat("─", maxInt0(width-2-lipgloss.Width(foot))) + foot + "─┘"

	return []string{
		sessionMutedStyle.Render(fitRow(top, width)),
		sessionMutedStyle.Render(fitRow(mid, width)),
		sessionMutedStyle.Render(fitRow(bottom, width)),
	}
}
