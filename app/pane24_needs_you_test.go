// Package app: board #295's own tests - the answered-row-leaves-at-once
// close write, and m typed on a Needs-you row reaching the board and the
// lane the same way y's own confirm strip already does.
package app

import (
	"claude-squad/cmd/cmd_test"
	"claude-squad/session/clarity"
	"errors"
	"os/exec"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

// -- test (d): comment then close then remove, in that order -------------

func TestAnswerFlow_CommentThenCloseThenRemove_InOrder(t *testing.T) {
	rec := &ghExecRecorder{fetchBody: `{"body":"## Lane\nways-of-working\n\n## Options\n(a) reply. Recommended."}`}
	h := newAnswerTestHome(t, rec)
	seedNeedsYouRow(t, h, "ways-of-working", "ok\n")
	h.boardCache.Get(277)

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'y', Text: "y"})
	_, cmd := h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)
	msg := cmd()
	result := msg.(composerResultMsg)
	require.NoError(t, result.err)

	require.Equal(t, []string{"POST", "PATCH"}, rec.callOrder, "the comment must land before the close is ever attempted")

	h.Update(msg)
	require.Equal(t, 0, h.list.NumNeedsYou(), "the row is removed only after both writes land")
}

// -- test (e): close failure queues behind the comment, footer says pending --

func TestAnswerFlow_CloseFailure_QueuesForRetry_FooterSaysClosePending(t *testing.T) {
	rec := &ghExecRecorder{
		fetchBody: `{"body":"## Lane\nways-of-working\n\n## Options\n(a) reply. Recommended."}`,
		closeErr:  errors.New("rate limited"),
	}
	h := newAnswerTestHome(t, rec)
	seedNeedsYouRow(t, h, "ways-of-working", "ok\n")
	h.boardCache.Get(277)

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'y', Text: "y"})
	_, cmd := h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg := cmd()
	result := msg.(composerResultMsg)
	require.NoError(t, result.err)
	require.Equal(t, "answered #277 · close pending", result.result)
	require.True(t, result.closePending)

	h.Update(msg)
	require.Equal(t, 1, h.list.NumNeedsYou(), "the row stays (tick-and-dim) until the close lands")
	require.True(t, h.answeredIssues[277])
	p := h.pendingComments[277]
	require.NotNil(t, p)
	require.True(t, p.commentDone, "the comment already posted - only the close is retried")
	require.Len(t, rec.postCalls, 1, "the comment is never reposted")
	require.Len(t, rec.closeCalls, 1)

	// The close finally lands on a later retry.
	rec.closeErr = nil
	p.lastAttempt = time.Now().Add(-2 * clarity.BoardRetryInterval)
	retryCmd := h.retryPendingCommentsCmd(time.Now())
	require.NotNil(t, retryCmd)
	retryMsg := retryCmd()
	h.Update(retryMsg)

	require.Len(t, rec.postCalls, 1, "still exactly one post - the retry only closes")
	require.Len(t, rec.closeCalls, 2)
	require.Equal(t, 0, h.list.NumNeedsYou(), "the row is removed once the close finally lands")
	require.Nil(t, h.pendingComments[277])
	require.Contains(t, h.composer.Result(), "answered #277 · closed · sent into ways-of-working")
}

// -- test (f): m-typed text on a Needs-you row posts the comment and closes --

func TestMKey_NeedsYouRow_TypedTextPostsCommentClosesAndRemoves(t *testing.T) {
	rec := &ghExecRecorder{fetchBody: `{"body":"## Lane\nways-of-working"}`}
	h := newAnswerTestHome(t, rec)
	seedNeedsYouRow(t, h, "ways-of-working", "ok\n")
	h.boardCache.Get(277)

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'm', Text: "m"})
	require.True(t, h.composer.IsOpen())
	require.Equal(t, 277, h.composer.AnswerIssue(), "m on a board-sourced Needs-you row tags the reply to its own issue")

	h.composer.Type("a plain typed reply")
	_, cmd := h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)
	msg := cmd()
	result := msg.(composerResultMsg)
	require.NoError(t, result.err)
	require.True(t, result.closed)
	require.Equal(t, "answered #277 · closed · sent into ways-of-working", result.result)

	require.Len(t, rec.postCalls, 1)
	require.Contains(t, rec.postCalls[0], "body=answered from the cockpit: a plain typed reply\nsent into ways-of-working at ")
	require.Len(t, rec.closeCalls, 1)

	h.Update(msg)
	require.Equal(t, 0, h.list.NumNeedsYou())
}

// TestMKey_NeedsYouRow_AlreadyAnswered_PlainSendNoBoardWrite is m's own
// no-op boundary: an already-answered row (still shown, tick-and-dim,
// awaiting its close retry) is never re-tagged - m on it sends an ordinary
// message with no second board write.
func TestMKey_NeedsYouRow_AlreadyAnswered_PlainSendNoBoardWrite(t *testing.T) {
	rec := &ghExecRecorder{fetchBody: `{"body":"## Lane\nways-of-working"}`}
	h := newAnswerTestHome(t, rec)
	seedNeedsYouRow(t, h, "ways-of-working", "ok\n")
	h.boardCache.Get(277)
	h.markAnswered(277)

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'm', Text: "m"})
	require.Equal(t, 0, h.composer.AnswerIssue(), "an already-answered row falls through to a plain send")
}

// -- test (g): m on a tracked row (not Needs-you) is unaffected -----------

func TestMKey_TrackedRow_NeverTagsAnIssue_BehavesAsToday(t *testing.T) {
	rec := &ghExecRecorder{}
	h := newAnswerTestHome(t, rec)
	inst := trackedInstanceWithFakeTmux(t, "lane-a", "hello landed\n")
	h.list.AddInstance(inst)
	h.list.SetSelectedInstance(0)

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'm', Text: "m"})
	require.True(t, h.composer.IsOpen())
	require.Equal(t, "lane-a", h.composer.Lane())
	require.Equal(t, 0, h.composer.AnswerIssue(), "a tracked row never carries a board issue")

	h.composer.Type("ordinary message")
	_, cmd := h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)
	msg := cmd()
	result, ok := msg.(composerResultMsg)
	require.True(t, ok)
	require.NoError(t, result.err)
	require.Contains(t, result.result, "sent · landed")
	require.Equal(t, 0, result.issue)
	require.Empty(t, rec.postCalls, "an ordinary message never touches the board")
	require.Empty(t, rec.closeCalls)
}

// -- board #313's own replay defect: a board-shaped feed row (rank/state/
// source/title, no lane column at all - fleet_queue_build.py's real owner-
// action shape) must still reach the board on Enter, whether the row's
// raising lane resolves from the fetched issue's own lane: label (a), never
// resolves at all (b), or the fetch simply has not landed yet when Enter is
// pressed (c). Before this fix, app.go's stateMsg Enter handler checked
// composer.Lane() == "" BEFORE composer.AnswerIssue() != 0 - an unknown
// lane silently dropped the whole answer (no POST, no PATCH), which is
// exactly what the orchestrator's own replay against board #313 hit.

// seedBoardRowUnrelatedTracked seeds one board-sourced Needs-you row (no
// lane column, matching the real fleet_queue_build.py owner-action shape)
// behind an UNRELATED tracked instance - so the row's raising lane resolves
// (or fails to) purely from the fetched issue, never from a same-named
// tracked lane sitting right there by coincidence.
func seedBoardRowUnrelatedTracked(t *testing.T, h *home, source, title string) {
	t.Helper()
	inst := trackedInstanceWithFakeTmux(t, "unrelated-lane", "ok\n")
	h.list.AddInstance(inst)
	h.list.SetSelectedInstance(0)
	h.list.SetNeedsYou([]clarity.FeedItem{{Rank: 1, Source: source, Lane: source, Title: title}}, "")
	h.list.Up() // wraps from the tracked row onto the sole Needs-you row
}

// mockCopyExec is the same real-clipboard guard test 8 (answer_and_bank_
// test.go) already uses: an unresolved-lane row falls through to the copy
// path (composerTarget's own isExternal=true), which must never shell out
// to the real pbcopy inside a test.
func mockCopyExec() (*cmd_test.MockCmdExec, *[]string) {
	var args []string
	exec := &cmd_test.MockCmdExec{RunFunc: func(c *exec.Cmd) error {
		args = c.Args
		return nil
	}}
	return exec, &args
}

// test (a): the fetched issue's lane: label resolves to a TRACKED instance -
// posts, closes, removes, footer names the lane by its literal delivery
// text ("sent into <lane>").
func TestMKey_BoardRowNoLaneColumn_LabelLaneResolvesTracked_PostsClosesRemoves(t *testing.T) {
	rec := &ghExecRecorder{fetchBody: `{"body":"## Options\n(a) x.","labels":[{"name":"lane:fake-lane"},{"name":"type:owner-action"}]}`}
	h := newAnswerTestHome(t, rec)
	inst := trackedInstanceWithFakeTmux(t, "fake-lane", "ok\n")
	h.list.AddInstance(inst)
	h.list.SetSelectedInstance(0)
	h.list.SetNeedsYou([]clarity.FeedItem{{Rank: 1, Source: "#313", Lane: "#313", Title: "Scratch row 13"}}, "")
	h.list.Up()
	h.boardCache.Get(313) // the fetch has landed before m is pressed

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'm', Text: "m"})
	require.True(t, h.composer.IsOpen())
	require.Equal(t, 313, h.composer.AnswerIssue())
	title := h.composer.Render(80, "")[0]
	require.Contains(t, title, "answer #313 · to fake-lane", "never the generic message/NoLaneLabel title once an issue is tagged")
	require.NotContains(t, title, "no lane on this row")

	h.composer.Type("test answer")
	_, cmd := h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)
	msg := cmd()
	result := msg.(composerResultMsg)
	require.NoError(t, result.err)
	require.True(t, result.closed)
	require.Equal(t, "answered #313 · closed · sent into fake-lane", result.result)
	require.Len(t, rec.postCalls, 1)
	require.Contains(t, rec.postCalls[0], "body=answered from the cockpit: test answer\nsent into fake-lane at ")
	require.Len(t, rec.closeCalls, 1)

	h.Update(msg)
	require.Equal(t, 0, h.list.NumNeedsYou())
}

// test (b): neither the card's own Lane section, its lane: label, nor a
// matching tracked/external row ever resolves a lane - posts, closes,
// removes, footer says "copied (no lane known)".
func TestMKey_BoardRowNoLaneColumn_LaneNeverResolves_PostsClosesFootersNoLaneKnown(t *testing.T) {
	rec := &ghExecRecorder{fetchBody: `{"body":"## Options\n(a) x."}`}
	h := newAnswerTestHome(t, rec)
	copyExec, copiedArgs := mockCopyExec()
	h.cmdExec = *copyExec
	seedBoardRowUnrelatedTracked(t, h, "#314", "Scratch row 14")
	h.boardCache.Get(314) // the fetch has landed - and still resolves nothing

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'm', Text: "m"})
	require.True(t, h.composer.IsOpen())
	require.Equal(t, 314, h.composer.AnswerIssue())
	title := h.composer.Render(80, "")[0]
	require.Contains(t, title, "answer #314 (no lane known)")
	require.NotContains(t, title, "no lane on this row")

	h.composer.Type("test answer")
	_, cmd := h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)
	msg := cmd()
	result := msg.(composerResultMsg)
	require.NoError(t, result.err)
	require.True(t, result.closed)
	require.Equal(t, "answered #314 · closed · copied (no lane known)", result.result)
	require.Equal(t, []string{"pbcopy"}, *copiedArgs, "no lane known - the reply still goes somewhere the owner can read it")
	require.Len(t, rec.postCalls, 1)
	require.Len(t, rec.closeCalls, 1)

	h.Update(msg)
	require.Equal(t, 0, h.list.NumNeedsYou())
}

// test (c): Enter arrives BEFORE the board fetch has landed at all (no
// boardCache.Get call yet - this is the orchestrator's exact replay timing,
// board #313: m, type, Enter inside a handful of seconds) - the comment and
// close still go, using whatever lane resolution is known at that instant
// (none, here).
func TestMKey_BoardRowNoLaneColumn_EnterBeforeFetchLands_StillPostsAndCloses(t *testing.T) {
	rec := &ghExecRecorder{fetchBody: `{"body":"## Lane\nfake-lane"}`}
	h := newAnswerTestHome(t, rec)
	copyExec, copiedArgs := mockCopyExec()
	h.cmdExec = *copyExec
	seedBoardRowUnrelatedTracked(t, h, "#315", "Scratch row 15")
	// Deliberately no h.boardCache.Get(315) here - the fetch has not landed.

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'm', Text: "m"})
	require.True(t, h.composer.IsOpen())
	require.Equal(t, 315, h.composer.AnswerIssue())
	title := h.composer.Render(80, "")[0]
	require.Contains(t, title, "answer #315 (no lane known)", "the fetch has not landed - nothing to resolve yet")

	h.composer.Type("test answer")
	_, cmd := h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)
	msg := cmd()
	result := msg.(composerResultMsg)
	require.NoError(t, result.err)
	require.True(t, result.closed, "the answer needs only the issue number - it never waits on the fetch")
	require.Equal(t, "answered #315 · closed · copied (no lane known)", result.result)
	require.Equal(t, []string{"pbcopy"}, *copiedArgs)
	require.Len(t, rec.postCalls, 1)
	require.Contains(t, rec.postCalls[0], "body=answered from the cockpit: test answer")
	require.Len(t, rec.closeCalls, 1)
	_, fetched := h.boardCache.Peek(315)
	require.False(t, fetched, "the fetch never ran - the answer did not wait on it")

	h.Update(msg)
	require.Equal(t, 0, h.list.NumNeedsYou())
}
