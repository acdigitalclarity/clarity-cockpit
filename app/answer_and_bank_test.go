// Package app: slice 18's own end-to-end tests - the y-key answer flow and
// the b-key bank flow, against ANSWER-AND-BANK-SPEC.md's own numbered test
// list. No test here touches the real tmux server, the real clipboard or
// the real gh binary (test 13) - every dependency is the same fake-factory
// seam composer_test.go's own fixtures already proved (trackedInstance-
// WithFakeTmux, capturingPtyFactory, cmd_test.MockCmdExec).
package app

import (
	"claude-squad/cmd/cmd_test"
	"claude-squad/session"
	"claude-squad/session/clarity"
	"claude-squad/session/tmux"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

// ghExecRecorder is a cmd_test.MockCmdExec-backed fake for both halves of
// board.go's own gh api seam - a plain GET fetch (BoardCache.Get/fetch) and
// the -X POST comment write (BoardCache.PostComment) - branching on the
// command's own args, since both ride the same *BoardCache's exec field.
type ghExecRecorder struct {
	fetchBody string // the GET fetch's own JSON body
	postErr   error  // nil = every POST succeeds; non-nil = every POST fails with this
	postCalls []string
}

func (g *ghExecRecorder) exec() cmd_test.MockCmdExec {
	return cmd_test.MockCmdExec{
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			args := strings.Join(cmd.Args, " ")
			if strings.Contains(args, "-X POST") {
				g.postCalls = append(g.postCalls, args)
				if g.postErr != nil {
					return nil, g.postErr
				}
				return []byte(""), nil
			}
			return []byte(g.fetchBody), nil
		},
	}
}

func newAnswerTestHome(t *testing.T, rec *ghExecRecorder) *home {
	t.Helper()
	h := newComposerTestHome(t)
	h.boardCache = clarity.NewBoardCacheWithDeps(rec.exec(), "acdigitalclarity/clarity-tasks", "gh")
	return h
}

// seedNeedsYouRow puts one board-sourced Needs-you row (issue 277) with a
// tracked instance behind its lane, selected - the shape every test below
// starts from. paneAfterSend is trackedInstanceWithFakeTmux's own pane
// capture fixture (also read by sampleNeedsKey if a test ever ticks the
// feed, though none here do).
func seedNeedsYouRow(t *testing.T, h *home, lane, paneAfterSend string) {
	t.Helper()
	inst := trackedInstanceWithFakeTmux(t, lane, paneAfterSend)
	h.list.AddInstance(inst)
	h.list.SetSelectedInstance(0)
	h.list.SetNeedsYou([]clarity.FeedItem{{Rank: 1, Source: "#277", Lane: "#277", Title: "t"}}, "")
	h.list.Up() // wraps from the tracked row onto the sole Needs-you row
}

// -- test 3: no options -> composer opens empty, zero board calls ---------

func TestAnswerFlow_NoRecommendation_OpensComposerEmpty(t *testing.T) {
	rec := &ghExecRecorder{fetchBody: `{"body":"## What\njust do the thing"}`}
	h := newAnswerTestHome(t, rec)
	seedNeedsYouRow(t, h, "lane-a", "ok\n")
	h.boardCache.Get(277) // seed the cache synchronously, as app.go's fetchBoardCmd would

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'y', Text: "y"})

	require.True(t, h.composer.IsOpen(), "no recommendation - opens empty, ordinary typing mode")
	require.False(t, h.composer.IsConfirming())
	require.Equal(t, "", h.composer.Value())
	require.Empty(t, rec.postCalls, "no board call for a card with no options")
}

// -- test 4: y is a no-op on an answered row and on a lane row ------------

func TestAnswerFlow_AnsweredRow_NoOp(t *testing.T) {
	rec := &ghExecRecorder{fetchBody: `{"body":"## Options\n(a) do it. Recommended."}`}
	h := newAnswerTestHome(t, rec)
	seedNeedsYouRow(t, h, "lane-a", "ok\n")
	h.boardCache.Get(277)
	h.markAnswered(277)

	cmd := h.handleAnswerKey()
	require.Nil(t, cmd)
	require.False(t, h.composer.IsConfirming())
	require.Empty(t, rec.postCalls)
}

func TestAnswerFlow_LaneRowSelected_NoOp(t *testing.T) {
	rec := &ghExecRecorder{}
	h := newAnswerTestHome(t, rec)
	inst, err := session.NewInstance(session.InstanceOptions{Title: "lane-a", Path: ".", Program: "echo"})
	require.NoError(t, err)
	h.list.AddInstance(inst)
	h.list.SetSelectedInstance(0)

	cmd := h.handleAnswerKey()
	require.Nil(t, cmd)
	require.False(t, h.composer.IsConfirming())
}

// -- test 7: tracked send -------------------------------------------------

func TestAnswerFlow_TrackedSend_SendPromptOnceThenOnePostCommentTwoLineBody(t *testing.T) {
	rec := &ghExecRecorder{fetchBody: `{"body":"## Lane\nways-of-working\n\n## Options\n(a) Make both edits yourself, two minutes. Recommended."}`}
	h := newAnswerTestHome(t, rec)
	seedNeedsYouRow(t, h, "ways-of-working", "some old output\n(a) Make both edits yourself, two minutes.\n\n")
	h.boardCache.Get(277)

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'y', Text: "y"})
	require.True(t, h.composer.IsAnswerConfirm())
	require.Equal(t, "(a) Make both edits yourself, two minutes.", h.composer.Value(), "the trailing Recommended. is stripped")

	_, cmd := h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)
	msg := cmd()
	result, ok := msg.(composerResultMsg)
	require.True(t, ok)
	require.NoError(t, result.err)
	require.Contains(t, result.result, "sent · landed")
	require.Contains(t, result.result, "board #277 commented")

	require.Len(t, rec.postCalls, 1, "exactly one board comment")
	require.Contains(t, rec.postCalls[0], "repos/acdigitalclarity/clarity-tasks/issues/277/comments")
	require.Contains(t, rec.postCalls[0],
		"body=answered from the cockpit: (a) Make both edits yourself, two minutes.\nsent into ways-of-working at ")

	h.Update(msg)
	require.True(t, h.answeredIssues[277], "the row is marked answered")
	require.Equal(t, stateDefault, h.state)
}

// -- test 8: external send -------------------------------------------------

func TestAnswerFlow_ExternalSend_CopiesNeverSendsPrompt_BodyCarriesCopiedFor(t *testing.T) {
	rec := &ghExecRecorder{fetchBody: `{"body":"## Lane\nandy-e-bid\n\n## Options\n(a) reply text. Recommended."}`}
	h := newAnswerTestHome(t, rec)
	h.list.SetExternal([]clarity.ExternalLane{{Name: "andy-e-bid"}})
	h.list.SetNeedsYou([]clarity.FeedItem{{Rank: 1, Source: "#244", Lane: "#244", Title: "t"}}, "")
	h.list.Up() // wraps from the external row onto the Needs-you row
	h.boardCache.Get(244)

	var copiedArgs []string
	h.cmdExec = cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			copiedArgs = cmd.Args
			return nil
		},
	}

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'y', Text: "y"})
	require.True(t, h.composer.IsAnswerConfirm())
	require.True(t, h.composer.IsExternal())

	_, cmd := h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)
	msg := cmd()
	result, ok := msg.(composerResultMsg)
	require.True(t, ok)
	require.NoError(t, result.err)
	require.Equal(t, []string{"pbcopy"}, copiedArgs, "SendPrompt must never be attempted for an external lane")
	require.Contains(t, rec.postCalls[0], "body=answered from the cockpit: (a) reply text.\ncopied for andy-e-bid (external lane); paste pending.")
}

// -- test 9: send failure --------------------------------------------------

// TestAnswerFlow_SendFailure_NoBoardCallRowUnmarkedNotSentFoot exercises
// sendAnswerCmd/deliverToLane's own tracked-not-live branch directly, with
// isExternal hardcoded false - the shape composerTarget's own "tracked but
// not alive" resolution ALREADY reroutes to the copy path before deliver-
// ToLane ever sees it (DEFECT 1, TestComposerFlow_TrackedNoWorktreeInstance
// _NoSession_CopiesNeverSends), so the only way this branch is reached in
// production is a race: the tmux session dies between the confirm strip
// opening and enter being pressed. Calling the lower-level Cmd directly
// pins that branch's own contract without needing to simulate the race.
func TestAnswerFlow_SendFailure_NoBoardCallRowUnmarkedNotSentFoot(t *testing.T) {
	rec := &ghExecRecorder{fetchBody: `{"body":"## Lane\nways-of-working\n\n## Options\n(a) reply. Recommended."}`}
	h := newAnswerTestHome(t, rec)
	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			if strings.Contains(strings.Join(cmd.Args, " "), "has-session") {
				return errors.New("no such session")
			}
			return nil
		},
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) { return []byte("ok\n"), nil },
	}
	inst, err := session.NewInstance(session.InstanceOptions{Title: "ways-of-working", Path: t.TempDir(), Program: "claude"})
	require.NoError(t, err)
	inst.SetTmuxSession(tmux.NewTmuxSessionWithDeps("ways-of-working", "claude", &capturingPtyFactory{t: t}, cmdExec))
	require.NoError(t, inst.Start(false))
	h.list.AddInstance(inst)
	h.list.SetSelectedInstance(0)

	cmd := h.sendAnswerCmd(277, "ways-of-working", false, "(a) reply.")
	msg := cmd()
	result, ok := msg.(composerResultMsg)
	require.True(t, ok)
	require.NoError(t, result.err)
	require.Equal(t, "not sent · ways-of-working is not a live tmux session · nothing written to the board", result.result)
	require.Equal(t, 0, result.issue, "never tagged as an answered issue")
	require.False(t, result.answered)
	require.Empty(t, rec.postCalls, "zero board calls on a send failure")

	h.Update(msg)
	require.False(t, h.answeredIssues[277], "the row stays unmarked")
}

// -- test 10: comment failure -> retry -> abandon --------------------------

func TestAnswerFlow_CommentFailure_RetryThrottledThenAbandonedAfterFiveAttempts(t *testing.T) {
	rec := &ghExecRecorder{
		fetchBody: `{"body":"## Options\n(a) reply. Recommended."}`,
		postErr:   errors.New("rate limited"),
	}
	h := newAnswerTestHome(t, rec)
	seedNeedsYouRow(t, h, "ways-of-working", "ok\n")
	h.boardCache.Get(277)

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'y', Text: "y"})
	_, cmd := h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	msg := cmd()
	result := msg.(composerResultMsg)
	require.NoError(t, result.err)
	require.Contains(t, result.result, "comment pending")
	require.True(t, result.answered, "the reply is still reported sent - the row is marked regardless")

	h.Update(msg)
	require.True(t, h.answeredIssues[277])
	require.Contains(t, h.composer.Result(), "comment pending")
	require.Len(t, rec.postCalls, 1, "the first attempt already happened as part of the send")

	p := h.pendingComments[277]
	require.NotNil(t, p)
	require.Equal(t, 1, p.attempts)

	// Immediately after: within BoardRetryInterval, must not re-fire.
	retryCmd := h.retryPendingCommentsCmd(time.Now())
	require.Nil(t, retryCmd, "retry not attempted before 1 minute")
	require.Equal(t, 1, p.attempts)

	// Force the pending record stale (past BoardRetryInterval) and retry -
	// attempts 2, 3, 4, 5 in turn, each still failing.
	for want := 2; want <= 5; want++ {
		p.lastAttempt = time.Now().Add(-2 * clarity.BoardRetryInterval)
		retryCmd = h.retryPendingCommentsCmd(time.Now())
		require.NotNilf(t, retryCmd, "attempt %d must dispatch", want)
		require.Equal(t, want, p.attempts, "attempts bump at DISPATCH time, on the main thread")
		retryMsg := retryCmd()
		h.Update(retryMsg)
	}

	require.Len(t, rec.postCalls, 5, "one send-time post plus four retries")
	require.Nil(t, h.pendingComments[277], "abandoned after 5 attempts")
	require.Contains(t, h.composer.Result(), "board #277 comment failed · c copies the line")

	// A 6th attempt must never be dispatched.
	require.Nil(t, h.retryPendingCommentsCmd(time.Now().Add(time.Hour)))
}

// -- test 11: answered marker clears only when the queue drops the issue --

func TestAnsweredMarker_ClearsOnlyWhenQueueNoLongerCarriesIt(t *testing.T) {
	h := newComposerTestHome(t)
	h.markAnswered(277)
	require.True(t, h.answeredIssues[277])

	// The queue still carries #277 on this tick - the marker survives.
	h.pruneAnsweredIssues([]clarity.FeedItem{{Rank: 1, Source: "#277"}, {Rank: 2, Source: "#244"}})
	require.True(t, h.answeredIssues[277])

	// The rebuilt queue no longer carries #277 - now it drops.
	h.pruneAnsweredIssues([]clarity.FeedItem{{Rank: 1, Source: "#244"}})
	require.False(t, h.answeredIssues[277])
}

// -- test 12: b sends the bank line verbatim; watcher ignores a pre-send file --

func TestBankFlow_SendsVerbatimLine_WatcherIgnoresPreSendFileFindsPostSendOne(t *testing.T) {
	h := newComposerTestHome(t)
	dir := t.TempDir()
	inst := trackedInstanceWithFakeTmux(t, "cockpit", "ok\n")
	inst.Path = dir
	h.list.AddInstance(inst)
	h.list.SetSelectedInstance(0)

	// A CONTINUATION file written BEFORE the send - must never be reported.
	before := filepath.Join(dir, "CONTINUATION-old.md")
	require.NoError(t, os.WriteFile(before, []byte("old"), 0644))
	oldTime := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(before, oldTime, oldTime))

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'b', Text: "b"})
	require.True(t, h.composer.IsBankConfirm())
	require.Equal(t, clarity.BankLine, h.composer.Value())

	// The composer's own Value() (asserted above, before send) is exactly
	// what sendBankCmd hands to deliverToLane/SendPrompt - trackedInstance-
	// WithFakeTmux's own capturingPtyFactory (composer_test.go) already
	// proves that exact-text delivery path against a real tmux SendKeys/
	// TapEnter sequence; this test's own job is the watcher half.
	_, cmd := h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)
	msg := cmd()
	result := msg.(composerResultMsg)
	require.NoError(t, result.err)
	require.True(t, result.bank)
	h.Update(msg)
	require.NotNil(t, h.bankWatch)

	// No file yet - the watch stays armed.
	h.checkBankWatch(time.Now())
	require.NotNil(t, h.bankWatch)

	// A file written AFTER the send - the watch reports it and clears.
	after := filepath.Join(dir, "CONTINUATION-2026-09-03-1147.md")
	require.NoError(t, os.WriteFile(after, []byte("new"), 0644))
	newTime := time.Now().Add(time.Second)
	require.NoError(t, os.Chtimes(after, newTime, newTime))

	h.checkBankWatch(newTime.Add(time.Second))
	require.Nil(t, h.bankWatch, "the watch stops on the first hit")
	require.Contains(t, h.composer.Result(), "banked · ")
	require.Contains(t, h.composer.Result(), "CONTINUATION-2026-09-03-1147.md")
	require.NotContains(t, h.composer.Result(), "CONTINUATION-old.md")
}
