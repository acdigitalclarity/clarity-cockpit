// Package ui: slice 18's own Composer tests - the y-key answer confirm
// strip and the b-key bank confirm strip (ANSWER-AND-BANK-SPEC.md's own
// test list, items 5 and 6).
package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestComposer_OpenAnswerConfirm_SetsIssueTargetAndText(t *testing.T) {
	c := NewComposer()
	c.OpenAnswerConfirm(277, "ways-of-working", false, "(a) Make both edits yourself, two minutes.")

	require.True(t, c.IsConfirming())
	require.True(t, c.IsAnswerConfirm())
	require.False(t, c.IsBankConfirm())
	require.Equal(t, 277, c.ConfirmIssue())
	require.Equal(t, "ways-of-working", c.Lane())
	require.False(t, c.IsExternal())
	require.Equal(t, "(a) Make both edits yourself, two minutes.", c.Value())
	require.False(t, c.IsOpen(), "confirming is not typing mode")
}

func TestComposer_OpenBankConfirm_SetsBankLineVerbatim(t *testing.T) {
	c := NewComposer()
	c.OpenBankConfirm("cockpit", false)

	require.True(t, c.IsBankConfirm())
	require.False(t, c.IsAnswerConfirm())
	require.Equal(t, "bank state now: write the continuation from cells, then stop", c.Value())
	require.Equal(t, 0, c.ConfirmIssue())
}

func TestComposer_RenderAnswerConfirm_ShowsTitleTargetAndBoardLine(t *testing.T) {
	c := NewComposer()
	c.OpenAnswerConfirm(277, "ways-of-working", false, "(a) Make both edits yourself, two minutes.")

	out := strings.Join(c.Render(120, ""), "\n")
	plain := ansi.Strip(out)
	require.Contains(t, plain, "answer #277")
	require.Contains(t, plain, "(a) Make both edits yourself, two minutes.")
	require.Contains(t, plain, "into ways-of-working · live tmux · the reply is sent")
	require.Contains(t, plain, "board #277 · comment: answered from the cockpit: (a) Make both edits yourself, two minutes.")
	require.Contains(t, plain, AnswerConfirmFoot)
}

func TestComposer_RenderAnswerConfirm_ExternalLane_SaysCopied(t *testing.T) {
	c := NewComposer()
	c.OpenAnswerConfirm(244, "andy-e-bid", true, "some reply")

	plain := ansi.Strip(strings.Join(c.Render(120, ""), "\n"))
	require.Contains(t, plain, "into andy-e-bid · your own terminal · the reply is copied")
}

func TestComposer_RenderBankConfirm_TrackedLane(t *testing.T) {
	c := NewComposer()
	c.OpenBankConfirm("cockpit", false)

	plain := ansi.Strip(strings.Join(c.Render(120, ""), "\n"))
	require.Contains(t, plain, "bank cockpit")
	require.NotContains(t, plain, "copy only")
	require.Contains(t, plain, "bank state now: write the continuation from cells, then stop")
	require.Contains(t, plain, BankConfirmFoot)
}

func TestComposer_RenderBankConfirm_ExternalLane_CopyOnlyTitle(t *testing.T) {
	c := NewComposer()
	c.OpenBankConfirm("andy-e-bid", true)

	plain := ansi.Strip(strings.Join(c.Render(120, ""), "\n"))
	require.Contains(t, plain, "bank andy-e-bid · copy only")
}

// test 5: rendered at 114 and 80, every line bounded to the width.
func TestComposer_ConfirmStrips_NeverExceedWidth(t *testing.T) {
	for _, width := range []int{114, 80} {
		answer := NewComposer()
		answer.OpenAnswerConfirm(277, "ways-of-working", false,
			"(a) Make both edits yourself, two minutes, and confirm the campaign's boot line prints before you close the row.")
		for _, line := range answer.Render(width, "") {
			require.LessOrEqualf(t, ansi.StringWidth(line), width, "answer strip width %d: %q", width, line)
		}

		bank := NewComposer()
		bank.OpenBankConfirm("cockpit", false)
		for _, line := range bank.Render(width, "") {
			require.LessOrEqualf(t, ansi.StringWidth(line), width, "bank strip width %d: %q", width, line)
		}
	}
}

// test 6: e reopens the same box in typing mode with the text pre-filled.
func TestComposer_EditConfirmedAnswer_ReopensTypingModePrefilled(t *testing.T) {
	c := NewComposer()
	c.OpenAnswerConfirm(277, "ways-of-working", false, "(a) Make both edits yourself, two minutes.")

	c.EditConfirmedAnswer()

	require.True(t, c.IsOpen())
	require.False(t, c.IsConfirming())
	require.Equal(t, "(a) Make both edits yourself, two minutes.", c.Value())
	require.Equal(t, 277, c.AnswerIssue(), "the board issue survives the e chord, so Enter still runs the two-write flow")
	require.Equal(t, "ways-of-working", c.Lane())
}

// test 6: esc writes nothing anywhere - the composer-level half is that
// Close leaves no confirm/typing state at all to act on.
func TestComposer_Close_FromAnswerConfirm_ClearsEverything(t *testing.T) {
	c := NewComposer()
	c.OpenAnswerConfirm(277, "ways-of-working", false, "(a) text")

	c.Close()

	require.False(t, c.IsOpen())
	require.False(t, c.IsConfirming())
	require.Equal(t, "", c.Value())
	require.Equal(t, 0, c.ConfirmIssue())
}

func TestComposer_EditConfirmedAnswer_OnBankConfirm_NoOp(t *testing.T) {
	c := NewComposer()
	c.OpenBankConfirm("cockpit", false)

	c.EditConfirmedAnswer()

	require.True(t, c.IsBankConfirm(), "e is only meaningful on the answer strip")
}

// -- result tagging / retry refresh ---------------------------------------

func TestComposer_SetAnswerResult_UpdateResultIfIssue_RefreshesMatchingTag(t *testing.T) {
	c := NewComposer()
	c.OpenAnswerConfirm(277, "ways-of-working", false, "text")
	c.SetAnswerResult("sent · landed 11:42:07 · board #277 comment pending", 277)

	ok := c.UpdateResultIfIssue(277, "sent · landed 11:42:07 · board #277 commented")
	require.True(t, ok)
	require.Equal(t, "sent · landed 11:42:07 · board #277 commented", c.Result())
}

func TestComposer_UpdateResultIfIssue_NoOpOnceComposerMovedOn(t *testing.T) {
	c := NewComposer()
	c.OpenAnswerConfirm(277, "ways-of-working", false, "text")
	c.SetAnswerResult("sent · landed 11:42:07 · board #277 comment pending", 277)

	c.Open("some-other-lane", false) // the composer moved on to a fresh compose

	ok := c.UpdateResultIfIssue(277, "sent · landed 11:42:07 · board #277 commented")
	require.False(t, ok, "a stale retry must never overwrite an unrelated in-progress compose")
}

func TestComposer_SetBankResult_UpdateBankResult_Refreshes(t *testing.T) {
	c := NewComposer()
	c.OpenBankConfirm("cockpit", false)
	c.SetBankResult("bank sent 11:44:02 · watching for CONTINUATION-*.md")

	ok := c.UpdateBankResult("banked · /path/to/CONTINUATION-2026-09-03-1147.md")
	require.True(t, ok)
	require.Equal(t, "banked · /path/to/CONTINUATION-2026-09-03-1147.md", c.Result())
}
