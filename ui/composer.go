// Package ui: this file is the wired composer (design/cockpit-pane/
// DECISIONS.md item 3, slice 5) - the inline "message <lane>" box the
// Session and Needs-you panes both draw at their own foot. Session pane
// slice 3b already drew this box statically (session.go's
// renderComposerLines, see its own doc comment); this file is the model
// behind it becoming interactive: m opens it focused with a cursor, enter
// sends, esc closes. One Composer is shared by both tabs (app.go owns it),
// since only one row can ever be the current send target at a time.
//
// Slice 16 (the owner, 3 Sep: "this prompt bit doesnt wrap so i cant see
// what im typing over a certain number of characters") widens the box from
// a single scrolling line to a word-wrapping text area: it starts one line
// high, grows as the typed text wraps, up to composerMaxVisibleLines lines,
// then scrolls inside (the tail stays visible, the cursor - always on the
// last typed line - never drops off). The buffer itself (c.text) already
// holds embedded newlines fine; InsertNewline is the shift+enter/alt+enter
// chord's own effect once app.go's key dispatch calls it instead of
// treating Enter as send (that one-line wiring is a different leg's file
// right now - see this leg's own report for the exact case to add).
package ui

import (
	"claude-squad/session/clarity"
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

// confirmKind distinguishes the two non-typing strips slice 18 adds - the
// y-key answer confirmation and the b-key bank confirmation - from ordinary
// typing mode (open==true). Both are rendered by their own dedicated
// renderAnswerConfirm/renderBankConfirm below, never the typing-mode wrap/
// cursor logic wrappedLines/Render already use.
type confirmKind int

const (
	confirmNone confirmKind = iota
	confirmAnswer
	confirmBank
)

// AnswerConfirmFoot/BankConfirmFoot are the two confirm strips' own foot
// text while they are open, verbatim (ANSWER-AND-BANK-SPEC.md "Keys") -
// exported so ui/menu.go's own StateAnswerConfirm/StateBankConfirm can show
// the identical text across the whole bottom bar, the same way
// ComposerFootEditing already does for StateMsg.
const (
	AnswerConfirmFoot = "enter send · e edit · esc cancel"
	BankConfirmFoot   = "enter send · esc cancel"
)

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
	// resultTag correlates the CURRENTLY showing result with an in-flight
	// background follow-up (a board-comment retry, or the bank line's own
	// CONTINUATION-file watch) - "" for an ordinary message send, "issue:
	// <n>" for a y-key answer, "bank" for a b-key bank. UpdateResultIfIssue/
	// UpdateBankResult below only ever refresh c.result when this still
	// matches: once the composer has moved on to something else (a fresh
	// Open/SetResult), a stale retry/watch result silently no-ops rather
	// than overwriting whatever the composer is showing now.
	resultTag string

	// answerIssue is the board issue number an in-progress TYPING-mode edit
	// still belongs to, after the y-key answer strip's own e chord
	// (EditConfirmedAnswer) - 0 for an ordinary m-key message, so Enter in
	// app.go's stateMsg handling can tell "this typed text is still
	// answering issue N" from "this is a plain message" and route to the
	// same two-write flow the un-edited confirm strip would have.
	answerIssue int

	// confirmKind/confirmIssue/confirmText are the y/b confirm strips' own
	// state - see OpenAnswerConfirm/OpenBankConfirm below.
	confirmKind  confirmKind
	confirmIssue int
	confirmText  string
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
	c.resultTag = ""
	c.answerIssue = 0
	c.lane = lane
	c.isExternal = isExternal
	c.confirmKind = confirmNone
	c.confirmIssue = 0
	c.confirmText = ""
}

// OpenForIssue is Open (m on a Needs-you board row, board #295) plus a
// tagged answerIssue: identical typing-mode box, but Enter (app.go's
// stateMsg handling) sees a non-zero AnswerIssue and routes the typed text
// through the same two-write answer flow OpenAnswerConfirm's own enter uses
// (post the comment, then close the issue) rather than an ordinary message
// send with no board write.
func (c *Composer) OpenForIssue(lane string, isExternal bool, issue int) {
	c.Open(lane, isExternal)
	c.answerIssue = issue
}

// OpenAnswerConfirm loads the y key's confirm strip (RULE 6): text is
// already the card's own recommended response, AnswerText-stripped by the
// caller - the strip shows it verbatim, unedited, until enter sends it or e
// reopens ordinary typing mode with it pre-filled (EditConfirmedAnswer).
// issue is the board issue number the reply AND its follow-up comment both
// target.
func (c *Composer) OpenAnswerConfirm(issue int, lane string, isExternal bool, text string) {
	c.open = false
	c.text = ""
	c.result = ""
	c.resultTag = ""
	c.answerIssue = 0
	c.lane = lane
	c.isExternal = isExternal
	c.confirmKind = confirmAnswer
	c.confirmIssue = issue
	c.confirmText = text
}

// OpenBankConfirm loads the b key's confirm strip: the fixed bank line,
// verbatim, into lane.
func (c *Composer) OpenBankConfirm(lane string, isExternal bool) {
	c.open = false
	c.text = ""
	c.result = ""
	c.resultTag = ""
	c.answerIssue = 0
	c.lane = lane
	c.isExternal = isExternal
	c.confirmKind = confirmBank
	c.confirmIssue = 0
	c.confirmText = clarity.BankLine
}

// IsConfirming/IsAnswerConfirm/IsBankConfirm/ConfirmIssue report the
// current confirm-strip state - app.go's own stateAnswerConfirm/
// stateBankConfirm route on these rather than duplicating the confirmKind
// enum outside this file.
func (c *Composer) IsConfirming() bool    { return c.confirmKind != confirmNone }
func (c *Composer) IsAnswerConfirm() bool { return c.confirmKind == confirmAnswer }
func (c *Composer) IsBankConfirm() bool   { return c.confirmKind == confirmBank }
func (c *Composer) ConfirmIssue() int     { return c.confirmIssue }

// AnswerIssue reports the board issue number a typing-mode edit still
// belongs to (0 for an ordinary m-key message) - see the field's own doc
// comment.
func (c *Composer) AnswerIssue() int { return c.answerIssue }

// EditConfirmedAnswer is the answer strip's own e key (test 6): leaves
// confirm mode and re-enters ordinary typing mode with the confirm text
// pre-filled, so the reply can be changed before it is sent - the board
// issue it targets survives the switch (AnswerIssue), so Enter afterwards
// still runs the same two-write flow, with the edited text.
func (c *Composer) EditConfirmedAnswer() {
	if c.confirmKind != confirmAnswer {
		return
	}
	c.text = c.confirmText
	c.open = true
	c.answerIssue = c.confirmIssue
	c.confirmKind = confirmNone
	c.confirmIssue = 0
	c.confirmText = ""
}

// Close exits editing without sending, clearing the typed text but keeping
// the target/result fields (harmless once closed - Open resets them on the
// next use). Also leaves confirm mode with nothing sent (test 6: "esc
// writes nothing anywhere").
func (c *Composer) Close() {
	c.open = false
	c.text = ""
	c.answerIssue = 0
	c.confirmKind = confirmNone
	c.confirmIssue = 0
	c.confirmText = ""
}

// Lane/IsExternal report the send target captured when Open was last
// called.
func (c *Composer) Lane() string     { return c.lane }
func (c *Composer) IsExternal() bool { return c.isExternal }

// Value is the text a send actually uses: the confirm strip's own fixed
// text while confirming, the typed buffer otherwise.
func (c *Composer) Value() string {
	if c.confirmKind != confirmNone {
		return c.confirmText
	}
	return c.text
}
func (c *Composer) HasResult() bool { return c.result != "" }
func (c *Composer) Result() string  { return c.result }

// Type appends s (a key press's own printable text, or a paste's own
// possibly-multi-line text) to the composer's buffer verbatim - any
// embedded newline a paste already carries survives untouched, same as one
// InsertNewline adds.
func (c *Composer) Type(s string) {
	c.text += s
}

// InsertNewline inserts a literal line break at the end of the typed text -
// the shift+enter/alt+enter chord's own effect (RULE: "enter sends,
// shift+enter ... inserts a newline"). Wiring the actual chord is app.go's
// stateMsg key dispatch, not this file (see this leg's own report); this
// method is the buffer-level half the wiring calls once it exists, and is
// exercised directly by this file's own tests in the meantime.
func (c *Composer) InsertNewline() {
	c.text += "\n"
}

// Backspace removes the last rune of the typed text, rune-aware so a
// multi-byte character is never split. A trailing newline (from
// InsertNewline or a pasted line break) is removed whole, same as any other
// single rune.
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
	c.setResult(text)
	c.resultTag = ""
}

// SetAnswerResult is SetResult tagged for the y-key answer flow: a later
// board-comment retry (UpdateResultIfIssue) can refresh this exact foot
// text, as long as the composer has not moved on to something else since.
func (c *Composer) SetAnswerResult(text string, issue int) {
	c.setResult(text)
	c.resultTag = fmt.Sprintf("issue:%d", issue)
}

// SetBankResult is SetResult tagged for the b-key bank flow - the
// CONTINUATION-file watch (UpdateBankResult) can refresh it once the file
// appears.
func (c *Composer) SetBankResult(text string) {
	c.setResult(text)
	c.resultTag = "bank"
}

// UpdateResultIfIssue refreshes the foot text IF it is still showing the
// answer flow's own result for issue (a board-comment retry landing, or
// finally being abandoned) - a no-op, returning false, once the composer
// has moved on (a fresh Open/SetResult since).
func (c *Composer) UpdateResultIfIssue(issue int, text string) bool {
	if c.resultTag != fmt.Sprintf("issue:%d", issue) {
		return false
	}
	c.result = text
	return true
}

// UpdateBankResult refreshes the foot text IF it is still showing the bank
// flow's own result (the CONTINUATION-file watch landing) - a no-op,
// returning false, once the composer has moved on.
func (c *Composer) UpdateBankResult(text string) bool {
	if c.resultTag != "bank" {
		return false
	}
	c.result = text
	return true
}

// setResult is the shared core SetResult/SetAnswerResult/SetBankResult all
// call - every ephemeral compose-in-progress field clears, whichever mode
// (typing or confirming) was active.
func (c *Composer) setResult(text string) {
	c.result = text
	c.open = false
	c.text = ""
	c.answerIssue = 0
	c.confirmKind = confirmNone
	c.confirmIssue = 0
	c.confirmText = ""
}

// composerCursor is the mock-up's own cursor glyph (PANE-MOCKUP-164x45.md:
// "▸ █").
const composerCursor = "█"

// composerPromptPrefix marks the buffer's own first wrapped line - every
// continuation line (a word-wrap break or an explicit InsertNewline) is
// indented by the same width instead, so typed text lines up under the
// arrow rather than restarting at the left margin (the same continuation-
// indent idiom ui/needsyou.go's optionIndent already uses for wrapped
// options).
const composerPromptPrefix = "▸ "

// composerMaxVisibleLines is the RULE's own cap ("grows ... to a maximum of
// five lines, then scrolls inside"): once the wrapped text needs more rows
// than this, only the last composerMaxVisibleLines are shown - the cursor,
// always on the last wrapped line, is always among them, so nothing extra
// needs tracking to keep it visible.
const composerMaxVisibleLines = 5

// copyOnlySuffix marks the composer title on a lane with no live tmux
// session to deliver into (cockpit pane-10 walkthrough DEFECT 1) - shown
// from the moment the box opens, before any text is typed, so the owner
// knows enter will copy rather than send.
const copyOnlySuffix = " · copy only"

// composerWrap word-wraps text to width, paragraph by paragraph, treating
// every embedded "\n" (an InsertNewline chord, or a multi-line paste
// already carrying its own breaks) as a hard line break rather than
// whitespace to collapse - unlike ui/session.go's own wrapPlain (used for
// read-only prose, where a typed newline carries no meaning worth keeping),
// a composer's own line breaks are content the owner put there on purpose
// and the sent text must keep (RULE: "the sent text keeps its newlines").
// Never returns an empty slice - an empty paragraph (including "" itself)
// still costs one blank row, so the box always has at least its one
// starting line.
func composerWrap(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, para := range strings.Split(text, "\n") {
		out = append(out, composerWrapParagraph(para, width)...)
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}

// composerWrapParagraph greedily wraps one newline-free paragraph's words
// into lines no wider than width - a single word longer than width still
// gets its own (overflowing) line rather than being split mid-word, same
// rule wrapPlain documents for its own read-only case. An empty paragraph
// (no words) still returns one empty line, so a run of consecutive
// newlines each keep their own blank row.
func composerWrapParagraph(p string, width int) []string {
	words := strings.Fields(p)
	if len(words) == 0 {
		return []string{""}
	}
	lines := make([]string, 0, 4)
	cur := words[0]
	for _, w := range words[1:] {
		if lipgloss.Width(cur)+1+lipgloss.Width(w) > width {
			lines = append(lines, cur)
			cur = w
		} else {
			cur += " " + w
		}
	}
	return append(lines, cur)
}

// wrappedLines is the buffer's own word-wrapped rows at the given render
// width, UNCLIPPED (the full wrap, before the five-line visible cap) -
// idle/result (not open) is always the single legacy bare-cursor row,
// unchanged from before slice 16 (a closed composer never held typed text
// to wrap in the first place, since Close/SetResult both clear it).
func (c *Composer) wrappedLines(width int) []string {
	if !c.open {
		return []string{""}
	}
	inner := width - 4 - lipgloss.Width(composerPromptPrefix)
	if inner < 1 {
		inner = 1
	}
	return composerWrap(c.text, inner)
}

// Height reports how many rows Render(width, ...) returns for the
// composer's CURRENT text, without a full render - the pane layout's own
// budget query (ui/session.go's turnsAreaHeight, ui/needsyou.go's
// contentAreaHeight shrink the scrollable region above by exactly this,
// RULE: "the turns viewport above shrinks by the same rows so nothing
// overlaps"). Idle/result is always 3 (top + one bare-cursor row + bottom,
// the pre-slice-16 fixed footprint); open grows with the wrapped line
// count, capped at composerMaxVisibleLines+2.
func (c *Composer) Height(width int) int {
	if c.confirmKind != confirmNone {
		n := len(c.confirmContentLines(width))
		if n < 1 {
			n = 1
		}
		return n + 2
	}
	n := len(c.wrappedLines(width))
	if n > composerMaxVisibleLines {
		n = composerMaxVisibleLines
	}
	if n < 1 {
		n = 1
	}
	return n + 2
}

// Render draws the composer's box at the given width: the "message <lane>"
// title, one to composerMaxVisibleLines rows of typed text (word-wrapped,
// tail-clipped once it outgrows the cap, cursor trailing on the last row
// while open, a bare cursor on the single idle row otherwise), and the
// foot - idle, editing, or the transient result text SetResult last
// recorded. lane is the CURRENT selection's own target name, used for the
// title/prompt whenever the composer is not already open on a captured
// target (so the inert box still names whichever row would receive a
// message right now, matching slice 3b's own static behaviour before m is
// ever pressed).
func (c *Composer) Render(width int, lane string) []string {
	switch c.confirmKind {
	case confirmAnswer:
		return c.renderAnswerConfirm(width)
	case confirmBank:
		return c.renderBankConfirm(width)
	}
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

	wrapped := c.wrappedLines(width)
	total := len(wrapped)
	visible := wrapped
	firstAbsIdx := 0
	if total > composerMaxVisibleLines {
		firstAbsIdx = total - composerMaxVisibleLines
		visible = wrapped[firstAbsIdx:]
	}
	prefixWidth := lipgloss.Width(composerPromptPrefix)
	contentRows := make([]string, len(visible))
	for i, ln := range visible {
		absIdx := firstAbsIdx + i
		prefix := strings.Repeat(" ", prefixWidth)
		if absIdx == 0 {
			prefix = composerPromptPrefix
		}
		row := prefix
		if c.open {
			row += ln
			if absIdx == total-1 {
				row += composerCursor
			}
		} else {
			row += composerCursor
		}
		contentRows[i] = "│ " + row + strings.Repeat(" ", maxInt0(width-4-lipgloss.Width(row))) + " │"
	}

	foot := " " + ComposerFootIdle + " "
	switch {
	case c.open:
		foot = " " + ComposerFootEditing + " "
	case c.result != "":
		foot = " " + c.result + " "
	}
	bottom := "└" + strings.Repeat("─", maxInt0(width-2-lipgloss.Width(foot))) + foot + "─┘"

	lines := make([]string, 0, 2+len(contentRows))
	lines = append(lines, sessionMutedStyle.Render(fitRow(top, width)))
	for _, r := range contentRows {
		lines = append(lines, sessionMutedStyle.Render(fitRow(r, width)))
	}
	lines = append(lines, sessionMutedStyle.Render(fitRow(bottom, width)))
	return lines
}

// -- confirm strips (slice 18: y answer, b bank) --------------------------

// confirmWrapBlock word-wraps text to innerWidth, giving the FIRST wrapped
// row firstPrefix and every continuation row a same-width blank pad instead
// - the same continuation-indent idiom composerPromptPrefix already uses
// for the typing mode's own first line, reused here for every line of the
// confirm strip (the mock-up's own "▸ <text>" / two-space-indented follow-on
// lines, ANSWER-AND-BANK-MOCKUP-164x45.md).
func confirmWrapBlock(text, firstPrefix string, innerWidth int) []string {
	prefixWidth := lipgloss.Width(firstPrefix)
	wrapWidth := innerWidth - prefixWidth
	if wrapWidth < 1 {
		wrapWidth = 1
	}
	pad := strings.Repeat(" ", prefixWidth)
	wrapped := composerWrap(text, wrapWidth)
	out := make([]string, len(wrapped))
	for i, w := range wrapped {
		if i == 0 {
			out[i] = firstPrefix + w
		} else {
			out[i] = pad + w
		}
	}
	return out
}

// confirmTargetLine is the answer strip's own second line: "into <lane> ·
// live tmux · the reply is sent" (tracked) or "into <lane> · your own
// terminal · the reply is copied" (external) - ANSWER-AND-BANK-MOCKUP-
// 164x45.md screen 2.
func (c *Composer) confirmTargetLine() string {
	lane := c.lane
	if lane == "" {
		lane = NoLaneLabel
	}
	if c.isExternal {
		return fmt.Sprintf("into %s · your own terminal · the reply is copied", lane)
	}
	return fmt.Sprintf("into %s · live tmux · the reply is sent", lane)
}

// confirmBoardLine is the answer strip's own third line: the board comment
// that follows the reply, named up front so nothing is written blind.
func (c *Composer) confirmBoardLine() string {
	return fmt.Sprintf("board #%d · comment: answered from the cockpit: %s", c.confirmIssue, c.confirmText)
}

// confirmContentLines builds the confirm strip's own content rows
// (unbordered - Height and renderConfirmBox both call this, so the two can
// never drift out of step): three wrapped blocks for the answer strip (the
// text, the target line, the board line), one for the bank strip (the bank
// line alone).
func (c *Composer) confirmContentLines(width int) []string {
	innerWidth := width - 4 // "│ " + " │"
	if innerWidth < 1 {
		innerWidth = 1
	}
	switch c.confirmKind {
	case confirmAnswer:
		var lines []string
		lines = append(lines, confirmWrapBlock(c.confirmText, composerPromptPrefix, innerWidth)...)
		lines = append(lines, confirmWrapBlock(c.confirmTargetLine(), "  ", innerWidth)...)
		lines = append(lines, confirmWrapBlock(c.confirmBoardLine(), "  ", innerWidth)...)
		return lines
	case confirmBank:
		return confirmWrapBlock(c.confirmText, composerPromptPrefix, innerWidth)
	default:
		return nil
	}
}

// renderConfirmBox draws one confirm strip's box: title-bearing top border,
// the content rows, and a foot-bearing bottom border - the same three-part
// shape Render's own typing-mode box uses, factored out since both confirm
// kinds share it exactly (only the title/foot text differ).
func (c *Composer) renderConfirmBox(width int, title, foot string) []string {
	top := "┌" + title + strings.Repeat("─", maxInt0(width-2-lipgloss.Width(title))) + "┐"

	rows := c.confirmContentLines(width)
	contentRows := make([]string, len(rows))
	for i, r := range rows {
		contentRows[i] = "│ " + r + strings.Repeat(" ", maxInt0(width-4-lipgloss.Width(r))) + " │"
	}

	footText := " " + foot + " "
	bottom := "└" + strings.Repeat("─", maxInt0(width-2-lipgloss.Width(footText))) + footText + "─┘"

	lines := make([]string, 0, 2+len(contentRows))
	lines = append(lines, sessionMutedStyle.Render(fitRow(top, width)))
	for _, r := range contentRows {
		lines = append(lines, sessionMutedStyle.Render(fitRow(r, width)))
	}
	lines = append(lines, sessionMutedStyle.Render(fitRow(bottom, width)))
	return lines
}

// renderAnswerConfirm draws " answer #<n> " (ANSWER-AND-BANK-MOCKUP-
// 164x45.md screen 2).
func (c *Composer) renderAnswerConfirm(width int) []string {
	title := fmt.Sprintf(" answer #%d ", c.confirmIssue)
	return c.renderConfirmBox(width, title, AnswerConfirmFoot)
}

// renderBankConfirm draws " bank <lane> " or " bank <lane> · copy only "
// (screen 4, and the "A lane in your own terminal" bullet).
func (c *Composer) renderBankConfirm(width int) []string {
	lane := c.lane
	if lane == "" {
		lane = NoLaneLabel
	}
	title := fmt.Sprintf(" bank %s ", lane)
	if c.isExternal && c.lane != "" {
		title = fmt.Sprintf(" bank %s%s ", lane, copyOnlySuffix)
	}
	return c.renderConfirmBox(width, title, BankConfirmFoot)
}
