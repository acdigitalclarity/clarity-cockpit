package ui

import (
	"claude-squad/session"
	"claude-squad/session/clarity"
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// TestString_NeedsYouRow_TruncatesToListWidth is the OVERFLOW defect's own
// "a truncation test for a 200-character feed row": List.String()'s final
// lipgloss.Place is a documented no-op once content already exceeds the
// width it was given (it never truncates) - so a feed row longer than the
// list's own column must be clipped before it ever reaches that call, with
// an ellipsis marking the cut, not a byte slice.
func TestString_NeedsYouRow_TruncatesToListWidth(t *testing.T) {
	l := newTestList("a")
	l.SetSize(80, 40)
	l.SetNeedsYou([]string{strings.Repeat("x", 200)})

	out := l.String()
	for i, line := range strings.Split(out, "\n") {
		require.LessOrEqualf(t, ansi.StringWidth(line), l.width,
			"line %d exceeds the list's own width %d: %q", i, l.width, line)
	}
	require.Contains(t, out, "…", "a truncated 200-char row must carry an ellipsis, not just stop")
}

// TestString_ExternalRow_TruncatesToListWidth is the same defect's other
// named row kind: a long external-lane name must not blow the row (or the
// list's rendered width) out past the list's own column.
func TestString_ExternalRow_TruncatesToListWidth(t *testing.T) {
	l := newTestList("a")
	l.SetSize(80, 40)
	l.SetExternal([]clarity.ExternalLane{
		{Name: strings.Repeat("sessions-very-long-lane-name-", 5), LastWrite: time.Now()},
	})

	out := l.String()
	for i, line := range strings.Split(out, "\n") {
		require.LessOrEqualf(t, ansi.StringWidth(line), l.width,
			"line %d exceeds the list's own width %d: %q", i, l.width, line)
	}
}

// TestString_ExternalRows_ColumnsLineUp is the FINISH defect's "pad the
// lane name to a column so ctx and last write line up": two external rows
// with different-length names must place "ctx" at the same column.
func TestString_ExternalRows_ColumnsLineUp(t *testing.T) {
	l := newTestList()
	l.SetSize(120, 40)
	now := time.Now()
	l.SetExternal([]clarity.ExternalLane{
		{Name: "short", LastWrite: now},
		{Name: "a-much-longer-lane-name", LastWrite: now},
	})

	out := l.String()
	var ctxCols []int
	for _, line := range strings.Split(out, "\n") {
		if idx := strings.Index(line, "ctx "); idx >= 0 {
			ctxCols = append(ctxCols, idx)
		}
	}
	require.Len(t, ctxCols, 2, "expected exactly the two external rows to carry a ctx column")
	require.Equal(t, ctxCols[0], ctxCols[1], "ctx must land in the same column regardless of lane-name length")
}

// TestRender_NoWorktreeInstance_NoGarbledGlyph is the OWN ROW defect's row
// test: a clarity-attach instance (NoWorktree) has no git worktree and so
// no branch - upstream's "<icon>-<branch>" segment must not render as a
// bare, meaningless icon-and-hyphen when there is nothing after it.
func TestRender_NoWorktreeInstance_NoGarbledGlyph(t *testing.T) {
	inst, err := session.NewInstance(session.InstanceOptions{
		Title:      "ways-of-working",
		Path:       ".",
		Program:    "echo",
		NoWorktree: true,
	})
	require.NoError(t, err)
	require.False(t, inst.HasWorktree())

	s := spinner.New()
	l := NewList(&s, false)
	l.AddInstance(inst)
	l.SetSize(80, 40)

	out := l.String()
	require.NotContains(t, out, branchIcon, "no worktree means no branch to show a branch icon for")
}

// TestRender_UndeterminedContextFill_ShowsNothingNotNA is the OWN ROW
// defect's other half: when a tracked instance's context fill genuinely
// cannot be derived, the row shows nothing, not "n/a".
func TestRender_UndeterminedContextFill_ShowsNothingNotNA(t *testing.T) {
	l := newTestList("fresh-instance")
	l.SetSize(80, 40)

	out := l.String()
	require.NotContains(t, out, "n/a", "an undetermined context fill must show nothing, not the old n/a label")
}

// TestString_NeverExceedsListHeight is the OVERFLOW defect's vertical half
// on realistic content: List.String()'s final lipgloss.Place is a
// documented no-op once content already exceeds the HEIGHT it was given
// too (not just width) - so a list with a full "Needs you" feed, several
// tracked instances and a full external-lane section can naturally need
// more rows than a short terminal has, and without an enforced cap the
// block grows past its box and pushes the menu/footer below it off the
// bottom of the screen (reproduced on real fleet data at 80x24).
func TestString_NeverExceedsListHeight(t *testing.T) {
	l := newTestList("a", "b")
	l.SetSize(80, 21) // app.go's own contentHeight at a 24-row terminal
	l.SetNeedsYou([]string{"one", "two", "three", "four", "five"})
	lanes := make([]clarity.ExternalLane, 6)
	for i := range lanes {
		lanes[i] = clarity.ExternalLane{Name: fmt.Sprintf("sessions-lane-%d", i), LastWrite: time.Now()}
	}
	l.SetExternal(lanes)

	out := l.String()
	lines := strings.Split(out, "\n")
	require.LessOrEqualf(t, len(lines), l.height,
		"list rendered %d lines, its own box is only %d rows", len(lines), l.height)
}

// TestRender_KnownContextFill_StillRenders guards against the "show
// nothing" fix accidentally suppressing a KNOWN fill too - once
// SetContextFill has a real value cached, the row must still show it.
func TestRender_KnownContextFill_StillRenders(t *testing.T) {
	l := newTestList("known-fill")
	l.SetSize(80, 40)
	l.items[0].SetContextFill(42, true)

	out := l.String()
	require.Contains(t, out, "ctx 42%")
}

// TestString_TrackedRow_ShowsStateGlyphWordAndLastTurn is item 1's own
// tracked-row test: once SetLaneState has a real value cached, the title
// line carries the state glyph, its word, and the last-turn time (local,
// hh:mm) alongside the existing ctx figure.
func TestString_TrackedRow_ShowsStateGlyphWordAndLastTurn(t *testing.T) {
	l := newTestList("working-lane")
	l.SetSize(120, 40)
	lastTurn := time.Date(2026, 9, 2, 19, 4, 0, 0, time.Local)
	l.items[0].SetLaneState(clarity.StateWorking, lastTurn, true)

	out := l.String()
	require.Contains(t, out, "●", "the working glyph must render")
	require.Contains(t, out, "working", "the state word must render")
	require.Contains(t, out, "19:04", "the last-turn time must render, local hh:mm")
}

// TestString_ExternalRow_ShowsStateGlyphWordAndLastTurn is the same test
// for an external lane row.
func TestString_ExternalRow_ShowsStateGlyphWordAndLastTurn(t *testing.T) {
	l := newTestList()
	l.SetSize(120, 40)
	lastTurn := time.Date(2026, 9, 2, 18, 29, 0, 0, time.Local)
	l.SetExternal([]clarity.ExternalLane{
		{Name: "travel-matrix-m4", LastWrite: time.Now(), Fill: clarity.Fill{Pct: 80}, FillOK: true,
			State: clarity.StateStalled, LastTurn: lastTurn, StateOK: true},
	})

	out := l.String()
	require.Contains(t, out, "◐", "the stalled glyph must render")
	require.Contains(t, out, "stalled", "the state word must render")
	require.Contains(t, out, "18:29", "the last-turn time must render, local hh:mm")
}

// TestString_LaneRows_DropWordWhenCollapsed is item 1's "below 100 columns
// drop the word, keep the glyph": with SetCollapsed(true), the state word
// disappears from both row kinds but the glyph stays.
func TestString_LaneRows_DropWordWhenCollapsed(t *testing.T) {
	l := newTestList("tracked-one")
	l.SetSize(80, 40)
	l.SetCollapsed(true)
	l.items[0].SetLaneState(clarity.StateWorking, time.Now(), true)
	l.SetExternal([]clarity.ExternalLane{
		{Name: "external-one", LastWrite: time.Now(), State: clarity.StateIdle, LastTurn: time.Now(), StateOK: true},
	})

	out := l.String()
	require.Contains(t, out, "●", "the tracked row's glyph must still render")
	require.Contains(t, out, "○", "the external row's glyph must still render")
	require.NotContains(t, out, "working", "the state word must be dropped below the collapse threshold")
	require.NotContains(t, out, "idle", "the state word must be dropped below the collapse threshold")
}

// TestSessionPane_FitsAt120x36And164x45And200x55 is item 5's own FINISH
// requirement gaining a Session-tab case: nothing exceeds the pane at any
// of the three named terminal sizes, using each size's own real pane
// content dimensions (TabbedWindow.SetSize's own arithmetic, not the raw
// terminal size) with a fixture LaneTail carrying all three turn kinds.
func TestSessionPane_FitsAt120x36And164x45And200x55(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{120, 36}, {164, 45}, {200, 55}} {
		t.Run(fmt.Sprintf("%dx%d", sz.w, sz.h), func(t *testing.T) {
			w := NewTabbedWindow(NewSessionPane(), NewDiffPane(), NewTerminalPane())
			w.SetSize(sz.w, int(float32(sz.h)*0.9))
			contentWidth, contentHeight := w.GetContentSize()
			require.Greater(t, contentWidth, 0)
			require.Greater(t, contentHeight, 0)

			now := time.Date(2026, 9, 2, 19, 0, 0, 0, time.Local)
			w.SetSessionInfo(&SessionInfo{
				Lane: "ways-of-working",
				Tail: clarity.LaneTail{
					State:        clarity.StateWorking,
					LastWrite:    now,
					LastTurn:     now,
					Model:        "claude-fable-5-1",
					Messages:     616,
					TurnDuration: 81 * time.Second,
					Turns: []clarity.Turn{
						{Kind: clarity.TurnOwner, At: now, Text: "an owner turn"},
						{Kind: clarity.TurnAssistant, At: now, Text: "an assistant reply"},
						{Kind: clarity.TurnTool, At: now, Tool: "Bash", Summary: "run it", Result: clarity.ResultOK, Duration: 2 * time.Second},
					},
				},
				CtxPct: 20,
				CtxOK:  true,
			})

			out := w.String()
			for i, line := range strings.Split(out, "\n") {
				require.LessOrEqualf(t, ansi.StringWidth(line), sz.w,
					"line %d exceeds terminal width %d: %q", i, sz.w, line)
			}
		})
	}
}

// TestString_TrackedAndExternalRows_ShareCtxColumn is item 4's "one table"
// requirement across row KINDS, not just within external rows
// (TestString_ExternalRows_ColumnsLineUp covers that half): a tracked
// instance's title line and an external lane's row must place "ctx" in the
// same column.
func TestString_TrackedAndExternalRows_ShareCtxColumn(t *testing.T) {
	l := newTestList("a-tracked-lane")
	l.SetSize(120, 40)
	l.SetExternal([]clarity.ExternalLane{{Name: "an-external-lane", LastWrite: time.Now()}})

	out := l.String()
	var ctxCols []int
	for _, line := range strings.Split(out, "\n") {
		// Strip ANSI first: the tracked row and the external row carry
		// different escape sequences (different styles), so a raw
		// strings.Index would compare byte offsets that include those
		// codes, not the rendered column.
		if idx := strings.Index(ansi.Strip(line), "ctx"); idx >= 0 {
			ctxCols = append(ctxCols, idx)
		}
	}
	require.Len(t, ctxCols, 2, "expected exactly the tracked row and the external row to carry a ctx column")
	require.Equal(t, ctxCols[0], ctxCols[1], "ctx must land in the same column for a tracked row and an external row alike")
}
