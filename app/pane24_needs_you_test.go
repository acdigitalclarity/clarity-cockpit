// Package app: board #295's own tests - the answered-row-leaves-at-once
// close write, and m typed on a Needs-you row reaching the board and the
// lane the same way y's own confirm strip already does.
package app

import (
	"claude-squad/session/clarity"
	"errors"
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
