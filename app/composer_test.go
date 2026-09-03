package app

import (
	"claude-squad/cmd"
	"claude-squad/cmd/cmd_test"
	"claude-squad/session"
	"claude-squad/session/clarity"
	"claude-squad/session/tmux"
	"claude-squad/ui"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// newComposerTestHome builds a minimal *home wired the same way newHome
// does for the pieces this file's tests touch: one shared Composer fed
// into both the Session and Needs-you panes, a tabbed window sized so its
// content area is non-zero, and the tab-by-kind bookkeeping newHome itself
// initializes.
func newComposerTestHome() *home {
	sp := spinner.New()
	composer := ui.NewComposer()
	sessionPane := ui.NewSessionPane()
	sessionPane.SetComposer(composer)
	needsYouPane := ui.NewNeedsYouPane()
	needsYouPane.SetComposer(composer)
	tw := ui.NewTabbedWindow(sessionPane, needsYouPane, ui.NewTerminalPane())
	tw.SetSize(120, 40)

	return &home{
		ctx:          context.Background(),
		list:         ui.NewList(&sp, false),
		menu:         ui.NewMenu(),
		tabbedWindow: tw,
		errBox:       ui.NewErrBox(),
		statusBox:    ui.NewStatusBox(),
		composer:     composer,
		cmdExec:      cmd.MakeExecutor(),
		laneTab:      ui.SessionTab,
		needsYouTab:  ui.NeedsYouTab,
		prevRowKind:  ui.RowKindTracked,
	}
}

// pressGlobalKey drives a key that is bound in keys.GlobalKeyStringsMap
// through handleKeyPress's own two-dispatch shape: handleMenuHighlighting
// intercepts the first pass to drive the menu's keydown highlight and
// re-sends the same message, so the actual case in handleKeyPress's switch
// only runs on the second pass. Never used for a key pressed while
// m.state == stateMsg - the composer's own typing/enter/esc handling short-
// circuits handleMenuHighlighting's intercept entirely (state check comes
// before the keymap lookup), so those are dispatched once, directly.
func pressGlobalKey(h *home, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	h.handleKeyPress(msg)
	return h.handleKeyPress(msg)
}

// capturingPtyFactory is the proven fixture recipe from
// session/msg_delivery_test.go's TestMsgDelivery_SendPromptThenCapturePane,
// reused rather than rederived: a real temp file stands in for the tmux
// PTY, so SendPrompt's writes can be inspected without a live pseudo-
// terminal.
type capturingPtyFactory struct {
	t    *testing.T
	path string
}

func (p *capturingPtyFactory) Start(cmd *exec.Cmd) (*os.File, error) {
	p.path = filepath.Join(p.t.TempDir(), "ptmx")
	return os.OpenFile(p.path, os.O_CREATE|os.O_RDWR, 0644)
}

func (p *capturingPtyFactory) Close() {}

// trackedInstanceWithFakeTmux builds a Started tracked instance backed by a
// mock tmux Executor (paneAfterSend is what CapturePaneContent/Preview
// returns) and the capturingPtyFactory above - the same fake-tmux-runner
// shape msg_delivery_test.go already proved, so SendPrompt/Preview exercise
// the real delivery sequence with no live tmux session.
func trackedInstanceWithFakeTmux(t *testing.T, title, paneAfterSend string) *session.Instance {
	t.Helper()
	cmdExec := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error { return nil },
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			return []byte(paneAfterSend), nil
		},
	}
	inst, err := session.NewInstance(session.InstanceOptions{Title: title, Path: t.TempDir(), Program: "claude"})
	require.NoError(t, err)
	inst.SetTmuxSession(tmux.NewTmuxSessionWithDeps(title, "claude", &capturingPtyFactory{t: t}, cmdExec))
	require.NoError(t, inst.Start(false))
	require.True(t, inst.Started())
	return inst
}

// -- composerTarget resolution --------------------------------------------

func TestComposerTarget_TrackedRow(t *testing.T) {
	h := newComposerTestHome()
	inst, err := session.NewInstance(session.InstanceOptions{Title: "lane-a", Path: ".", Program: "echo"})
	require.NoError(t, err)
	h.list.AddInstance(inst)
	h.list.SetSelectedInstance(0)

	lane, isExternal, ok := h.composerTarget()
	require.True(t, ok)
	require.False(t, isExternal)
	require.Equal(t, "lane-a", lane)
}

func TestComposerTarget_NeedsYouRow_ResolvesToTrackedInstance(t *testing.T) {
	h := newComposerTestHome()
	inst, err := session.NewInstance(session.InstanceOptions{Title: "ways-of-working", Path: ".", Program: "echo"})
	require.NoError(t, err)
	h.list.AddInstance(inst)
	h.list.SetNeedsYou([]clarity.FeedItem{{Rank: 1, Lane: "ways-of-working", Title: "needs your go"}}, "")
	// Move the cursor onto the Needs-you row directly (ui.List's own group
	// cycle is proven in ui/list_external_test.go; this test is about
	// composerTarget's resolution, not cursor movement).
	h.list.Up() // from the default tracked row, wraps to the sole Needs-you row

	lane, isExternal, ok := h.composerTarget()
	require.True(t, ok)
	require.False(t, isExternal, "the row's raising lane matches a tracked instance - it sends, it does not copy")
	require.Equal(t, "ways-of-working", lane)
}

// TestComposerTarget_NeedsYouRow_UnresolvedBoardLane_NoLane is board #280's
// slice 5b DEFECT 2: a board-sourced row's raw item.Lane is the issue
// number string itself ("#277") - never a real send target. With no board
// fetch cached yet (or a failed one), the row's lane does not resolve at
// all, and composerTarget must say so with lane="" rather than falling
// back to that bogus "#277" string.
func TestComposerTarget_NeedsYouRow_UnresolvedBoardLane_NoLane(t *testing.T) {
	h := newComposerTestHome()
	h.list.SetNeedsYou([]clarity.FeedItem{{Rank: 1, Source: "#277", Lane: "#277", Title: "Owner: one settings act"}}, "")
	h.list.Up()

	lane, isExternal, ok := h.composerTarget()
	require.True(t, ok)
	require.True(t, isExternal, "an unresolved lane must never claim a delivery this cockpit cannot confirm")
	require.Equal(t, "", lane, "neither the fetched body's Lane field nor a lane: label resolved")
}

// TestComposerTarget_NeedsYouRow_BoardLane_ResolvesFromFetchedBody is
// DEFECT 2's fix proven the other way: once the board fetch lands, the
// row's real raising lane (the card's own "## Lane" section) is the send
// target, not the issue-number source string.
func TestComposerTarget_NeedsYouRow_BoardLane_ResolvesFromFetchedBody(t *testing.T) {
	h := newComposerTestHome()
	inst, err := session.NewInstance(session.InstanceOptions{Title: "ways-of-working", Path: ".", Program: "echo"})
	require.NoError(t, err)
	h.list.AddInstance(inst)
	h.list.SetNeedsYou([]clarity.FeedItem{{Rank: 1, Source: "#277", Lane: "#277", Title: "t"}}, "")
	h.boardCache = clarity.NewBoardCacheWithDeps(cmd_test.MockCmdExec{
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			return []byte(`{"body":"## Lane\nways-of-working"}`), nil
		},
	}, "acdigitalclarity/clarity-tasks", "gh")
	h.boardCache.Get(277) // seed the cache synchronously, as app.go's fetchBoardCmd would

	h.list.Up()
	lane, isExternal, ok := h.composerTarget()
	require.True(t, ok)
	require.False(t, isExternal, "the fetched lane matches a tracked instance - it sends, it does not copy")
	require.Equal(t, "ways-of-working", lane)
}

// -- tab-follows-row-kind (slice 5) ----------------------------------------

func TestSelectionChanged_NeedsYouRowSwitchesTabToNeedsYou(t *testing.T) {
	h := newComposerTestHome()
	inst, err := session.NewInstance(session.InstanceOptions{Title: "lane-a", Path: ".", Program: "echo"})
	require.NoError(t, err)
	h.list.AddInstance(inst)
	h.list.SetNeedsYou([]clarity.FeedItem{{Rank: 1, Lane: "#1", Title: "t"}}, "")

	require.Equal(t, ui.SessionTab, h.tabbedWindow.GetActiveTab(), "starts on Session, the default tab")

	h.list.Up() // crosses from the default tracked row into the Needs-you row
	h.selectionChanged()

	require.Equal(t, ui.NeedsYouTab, h.tabbedWindow.GetActiveTab(), "selecting a Needs-you row switches the tab to Needs you")
}

func TestSelectionChanged_LaneRowReturnsTabToSession(t *testing.T) {
	h := newComposerTestHome()
	inst, err := session.NewInstance(session.InstanceOptions{Title: "lane-a", Path: ".", Program: "echo"})
	require.NoError(t, err)
	h.list.AddInstance(inst)
	h.list.SetNeedsYou([]clarity.FeedItem{{Rank: 1, Lane: "#1", Title: "t"}}, "")

	h.list.Up()
	h.selectionChanged()
	require.Equal(t, ui.NeedsYouTab, h.tabbedWindow.GetActiveTab())

	h.list.Down() // back onto the tracked row
	h.selectionChanged()
	require.Equal(t, ui.SessionTab, h.tabbedWindow.GetActiveTab(), "selecting a lane row returns the tab to Session")
}

func TestSelectionChanged_RemembersManualTabChoicePerRowKind(t *testing.T) {
	h := newComposerTestHome()
	inst, err := session.NewInstance(session.InstanceOptions{Title: "lane-a", Path: ".", Program: "echo"})
	require.NoError(t, err)
	h.list.AddInstance(inst)
	h.list.SetNeedsYou([]clarity.FeedItem{{Rank: 1, Lane: "#1", Title: "t"}}, "")

	// While on the tracked (lane-kind) row, manually tab to Terminal.
	h.tabbedWindow.SetActiveTab(ui.TerminalTab)
	h.rememberTabForCurrentRowKind()

	// Cross to the Needs-you row and back - the lane kind's own remembered
	// tab (Terminal) must be restored, not the Session default.
	h.list.Up()
	h.selectionChanged()
	require.Equal(t, ui.NeedsYouTab, h.tabbedWindow.GetActiveTab())

	h.list.Down()
	h.selectionChanged()
	require.Equal(t, ui.TerminalTab, h.tabbedWindow.GetActiveTab(), "the lane kind's own last manual tab choice must be restored")
}

func TestSelectionChanged_WithinSameKindDoesNotFightManualTab(t *testing.T) {
	h := newComposerTestHome()
	h.list.SetNeedsYou([]clarity.FeedItem{
		{Rank: 1, Lane: "#1", Title: "one"},
		{Rank: 2, Lane: "#2", Title: "two"},
	}, "")
	h.list.Up() // -> the last (second) Needs-you row
	h.selectionChanged()
	require.Equal(t, ui.NeedsYouTab, h.tabbedWindow.GetActiveTab())

	// Manually tab away while still on a Needs-you row.
	h.tabbedWindow.SetActiveTab(ui.TerminalTab)
	h.rememberTabForCurrentRowKind()

	// Moving between two Needs-you rows (same kind) must not force it back.
	h.list.Up()
	h.selectionChanged()
	require.Equal(t, ui.TerminalTab, h.tabbedWindow.GetActiveTab(),
		"a manual tab choice survives moving within the same row kind")
}

// -- refreshNeedsYouTab / board fetch (async, never blocks the UI thread) --

func TestRefreshNeedsYouTab_LaneFileSourced_NoRecommendation(t *testing.T) {
	h := newComposerTestHome()
	h.list.SetNeedsYou([]clarity.FeedItem{{Rank: 1, Source: "sessions/lane-a/STATUS.md:3", Lane: "lane-a", Title: "t"}}, "")
	h.list.Up()

	cmd := h.refreshNeedsYouTab()
	require.Nil(t, cmd, "a lane-file-sourced row has no board issue to fetch")
}

func TestRefreshNeedsYouTab_BoardIssue_CacheMissDispatchesFetch(t *testing.T) {
	h := newComposerTestHome()
	h.list.SetNeedsYou([]clarity.FeedItem{{Rank: 1, Source: "#277", Lane: "#277", Title: "t"}}, "")
	h.list.Up()

	fetched := false
	h.boardCache = clarity.NewBoardCacheWithDeps(cmd_test.MockCmdExec{
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			fetched = true
			return []byte(`{"body":"## Lane\nsome explanation"}`), nil
		},
	}, "acdigitalclarity/clarity-tasks", "gh")

	cmd := h.refreshNeedsYouTab()
	require.NotNil(t, cmd, "a cache miss dispatches a background fetch")
	require.False(t, fetched, "the fetch must not run on the UI thread yet")

	msg := cmd()
	require.Equal(t, boardFetchedMsg{}, msg)
	require.True(t, fetched)
}

func TestRefreshNeedsYouTab_BoardUnreachable_AfterFailedFetch(t *testing.T) {
	h := newComposerTestHome()
	h.list.SetNeedsYou([]clarity.FeedItem{{Rank: 1, Source: "#1", Lane: "#1", Title: "t"}}, "")
	h.list.Up()

	h.boardCache = clarity.NewBoardCacheWithDeps(cmd_test.MockCmdExec{
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) { return nil, errors.New("rate limited") },
	}, "acdigitalclarity/clarity-tasks", "gh")

	cmd := h.refreshNeedsYouTab() // dispatches the fetch
	require.NotNil(t, cmd)
	cmd() // runs it synchronously in this test, populating the cache

	cmd2 := h.refreshNeedsYouTab() // now a cache hit
	require.Nil(t, cmd2)
}

// -- composer send: tracked lane (SendPrompt) and external (clipboard) ----

func TestComposerFlow_TrackedLane_SendsPromptAndShowsLandedFoot(t *testing.T) {
	h := newComposerTestHome()
	inst := trackedInstanceWithFakeTmux(t, "zz-smoke-lane", "some old output\nhello landed\n\n\n")
	h.list.AddInstance(inst)
	h.list.SetSelectedInstance(0)

	h.composer.Open("zz-smoke-lane", false)
	h.state = stateMsg
	h.composer.Type("hello from the composer")

	_, cmd := h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd, "Enter with non-empty text dispatches the send")

	resultMsg := cmd()
	result, ok := resultMsg.(composerResultMsg)
	require.True(t, ok)
	require.NoError(t, result.err)
	require.Contains(t, result.result, "sent · landed")

	h.Update(resultMsg)
	require.False(t, h.composer.IsOpen())
	require.Equal(t, result.result, h.composer.Result())
	require.Equal(t, stateDefault, h.state)
}

func TestComposerFlow_ExternalLane_CopiesToClipboardNeverClaimsDelivery(t *testing.T) {
	h := newComposerTestHome()
	h.list.SetExternal([]clarity.ExternalLane{{Name: "mcp-and-ideation"}})
	h.list.Down() // -> the external row

	var copiedArgs []string
	h.cmdExec = cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			copiedArgs = cmd.Args
			return nil
		},
	}

	lane, isExternal, ok := h.composerTarget()
	require.True(t, ok)
	require.True(t, isExternal)
	h.composer.Open(lane, isExternal)
	h.state = stateMsg
	h.composer.Type("scratchfix hello")

	_, cmd := h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)

	resultMsg := cmd()
	result, ok := resultMsg.(composerResultMsg)
	require.True(t, ok)
	require.NoError(t, result.err)
	require.Equal(t, "copied · this lane runs in your own terminal, paste it there", result.result)
	require.Equal(t, []string{"pbcopy"}, copiedArgs, "the external send path must run pbcopy, never SendPrompt")
}

// TestComposerFlow_TrackedNoWorktreeInstance_NoSession_CopiesNeverSends is
// board #280 pane-10 walkthrough DEFECT 1, seen failing first: the owner's
// exact PROOF (a) shape - a NoWorktree, Paused, NO tmux session tracked
// instance (clarity-attach's own lane, run in the owner's own terminal) -
// pressing m, typing, pressing enter must copy to the clipboard with the
// "runs in your own terminal" note, never route through the tracked send
// path and error "not a live tmux session".
func TestComposerFlow_TrackedNoWorktreeInstance_NoSession_CopiesNeverSends(t *testing.T) {
	h := newComposerTestHome()
	noWorktreeAppFixture(t, h, "scratchfix-pane10-attached")

	// m opens the composer on the current selection - composerTarget must
	// resolve this row as copy-only (isExternal=true) even though it is a
	// tracked instance, not a genuine external row.
	pressGlobalKey(h, tea.KeyPressMsg{Code: 'm', Text: "m"})
	require.True(t, h.composer.IsOpen())
	require.Equal(t, "scratchfix-pane10-attached", h.composer.Lane())
	require.True(t, h.composer.IsExternal(), "no live tmux session - copy-only, resolved before any text is typed")
	require.Contains(t, ansi.Strip(strings.Join(h.composer.Render(120, ""), "\n")),
		"message scratchfix-pane10-attached · copy only",
		"the title says copy-only from the moment the box opens, before enter is ever pressed")

	var copiedArgs []string
	h.cmdExec = cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			copiedArgs = cmd.Args
			return nil
		},
	}
	h.composer.Type("scratchfix copy test")

	_, cmd := h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.NotNil(t, cmd)

	resultMsg := cmd()
	result, ok := resultMsg.(composerResultMsg)
	require.True(t, ok)
	require.NoError(t, result.err)
	require.Equal(t, "copied · this lane runs in your own terminal, paste it there", result.result)
	require.Equal(t, []string{"pbcopy"}, copiedArgs, "must copy, never attempt a tmux send-keys")
}

func TestComposerFlow_EscClosesWithoutSending(t *testing.T) {
	h := newComposerTestHome()
	h.composer.Open("lane-a", false)
	h.state = stateMsg
	h.composer.Type("half-typed")

	h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEsc})

	require.False(t, h.composer.IsOpen())
	require.Equal(t, "", h.composer.Value())
	require.Equal(t, stateDefault, h.state)
}

func TestComposerFlow_EmptyTextEnterClosesWithoutSending(t *testing.T) {
	h := newComposerTestHome()
	h.composer.Open("lane-a", false)
	h.state = stateMsg

	_, cmd := h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Nil(t, cmd, "an empty message sends nothing")
	require.False(t, h.composer.IsOpen())
}

func TestComposerFlow_MOpensComposerFocusedOnSelectedLane(t *testing.T) {
	h := newComposerTestHome()
	inst, err := session.NewInstance(session.InstanceOptions{Title: "lane-a", Path: ".", Program: "echo"})
	require.NoError(t, err)
	h.list.AddInstance(inst)
	h.list.SetSelectedInstance(0)

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'm', Text: "m"})

	require.True(t, h.composer.IsOpen())
	require.Equal(t, "lane-a", h.composer.Lane())
	require.Equal(t, stateMsg, h.state)
}

func TestComposerFlow_TypingAppendsToComposer(t *testing.T) {
	h := newComposerTestHome()
	h.composer.Open("lane-a", false)
	h.state = stateMsg

	h.handleKeyPress(tea.KeyPressMsg{Code: 'h', Text: "h"})
	h.handleKeyPress(tea.KeyPressMsg{Code: 'i', Text: "i"})
	require.Equal(t, "hi", h.composer.Value())

	h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyBackspace})
	require.Equal(t, "h", h.composer.Value())
}

// TestComposerFlow_MOpensStateMsg_NotStatePrompt is board #280's slice 5b
// DEFECT 3: the composer's own menu state is StateMsg, never the upstream
// "enter prompt" instance-start overlay's StatePrompt it used to borrow.
func TestComposerFlow_MOpensStateMsg_NotStatePrompt(t *testing.T) {
	h := newComposerTestHome()
	inst, err := session.NewInstance(session.InstanceOptions{Title: "lane-a", Path: ".", Program: "echo"})
	require.NoError(t, err)
	h.list.AddInstance(inst)
	h.list.SetSelectedInstance(0)

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'm', Text: "m"})

	require.Equal(t, "enter send · esc cancel", strings.TrimSpace(ansi.Strip(h.menu.String())),
		"the footer while the composer is open, exactly - never StatePrompt's borrowed \"enter submit name\"")
}

// TestComposerFlow_NoLaneRow_TitleAndEnterMessage is board #280's slice 5b
// DEFECT 2's composer-open half: a Needs-you row whose lane resolved to
// neither the board card's Lane field nor its lane: label still opens the
// composer, named "(no lane on this row)", and enter delivers nothing.
func TestComposerFlow_NoLaneRow_TitleAndEnterMessage(t *testing.T) {
	h := newComposerTestHome()
	h.list.SetNeedsYou([]clarity.FeedItem{{Rank: 1, Source: "#277", Lane: "#277", Title: "t"}}, "")
	h.list.Up()

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'm', Text: "m"})
	require.True(t, h.composer.IsOpen())
	require.Equal(t, "", h.composer.Lane())
	require.Contains(t, strings.Join(h.composer.Render(80, ""), "\n"), "message (no lane on this row)")

	h.composer.Type("hi")
	_, cmd := h.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Nil(t, cmd, "no lane to send to - enter never dispatches a send")
	require.False(t, h.composer.IsOpen())
	require.Equal(t, "no lane to send to", h.composer.Result())
}
