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
	l.SetNeedsYou([]clarity.FeedItem{{Lane: "lane-a", Title: strings.Repeat("x", 200)}}, "")

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
// lane name to a column so the percentage and last write line up": two
// external rows with different-length names must place the pct field's own
// "%" at the same column (defect 2 dropped the "ctx" label this test used
// to key off; "%" is the field's own remaining stable marker).
func TestString_ExternalRows_ColumnsLineUp(t *testing.T) {
	l := newTestList()
	l.SetSize(120, 40)
	now := time.Now()
	l.SetExternal([]clarity.ExternalLane{
		{Name: "short", LastWrite: now, Fill: clarity.Fill{Pct: 1}, FillOK: true},
		{Name: "a-much-longer-lane-name", LastWrite: now, Fill: clarity.Fill{Pct: 1}, FillOK: true},
	})

	out := l.String()
	var pctCols []int
	for _, line := range strings.Split(ansi.Strip(out), "\n") {
		if idx := runeIndexOf(line, '%'); idx >= 0 {
			pctCols = append(pctCols, idx)
		}
	}
	require.Len(t, pctCols, 2, "expected exactly the two external rows to carry a percentage column")
	require.Equal(t, pctCols[0], pctCols[1], "the percentage must land in the same column regardless of lane-name length")
}

// runeIndexOf returns the RUNE (not byte) index of r's first occurrence in
// s - a plain strings.Index/IndexRune returns a byte offset, which is the
// wrong measure the moment a row's own name column contains a multi-byte
// truncation ellipsis ("…" is 3 UTF-8 bytes but 1 column), exactly the case
// TestString_ExternalRows_ColumnsLineUp and
// TestString_TrackedAndExternalRows_ShareCtxColumn both exercise.
func runeIndexOf(s string, r rune) int {
	for i, c := range []rune(s) {
		if c == r {
			return i
		}
	}
	return -1
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
	l.SetNeedsYou([]clarity.FeedItem{
		{Lane: "lane-a", Title: "one"}, {Lane: "lane-b", Title: "two"}, {Lane: "lane-c", Title: "three"},
		{Lane: "lane-d", Title: "four"}, {Lane: "lane-e", Title: "five"},
	}, "")
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
// DEFECT 2 dropped the "ctx" label from lane rows (it stays on the Session
// pane's own header, a different component) - the row shows the bare
// percentage.
func TestRender_KnownContextFill_StillRenders(t *testing.T) {
	l := newTestList("known-fill")
	l.SetSize(80, 40)
	l.items[0].SetContextFill(42, true)

	out := l.String()
	require.Contains(t, out, "42%")
	require.NotContains(t, out, "ctx", "the lane row's own percentage field must not carry the ctx label (defect 2)")
}

// TestLaneRow_PercentFieldBlankWhenUnknown is defect 2's other half: an
// undetermined fill shows a blank field, never "ctx" (which no longer
// prints at all on a lane row) and never "n/a".
func TestLaneRow_PercentFieldBlankWhenUnknown(t *testing.T) {
	l := newTestList("fresh-instance")
	l.SetSize(80, 40)

	out := l.String()
	require.NotContains(t, out, "ctx")
	require.NotContains(t, out, "n/a")
}

// TestLaneNameColumn_PaddedTo20AndTruncatedBeyond is defect 2's own name-
// column rule: on a terminal wide enough that the responsive shrink never
// kicks in, a short name is padded out to exactly 20 columns and a longer
// one truncates to 20 with an ellipsis - never the old 28-wide column.
func TestLaneNameColumn_PaddedTo20AndTruncatedBeyond(t *testing.T) {
	l := newTestList()
	l.SetSize(300, 40) // deliberately huge: rowInner is never the constraint
	now := time.Now()
	l.SetExternal([]clarity.ExternalLane{
		{Name: "short", LastWrite: now},
		{Name: "a-name-well-past-twenty-characters-long", LastWrite: now},
	})

	require.Equal(t, 20, l.laneNameColWidth(true), "the name column must cap at 20, not the old 28")

	out := ansi.Strip(l.String())
	require.Contains(t, out, "short               ", "a short name pads to exactly 20 columns")
	require.Contains(t, out, "a-name-well-past-tw…", "a name over 20 columns truncates to 20 with an ellipsis")
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
			w := NewTabbedWindow(NewSessionPane(), NewNeedsYouPane(), NewTerminalPane())
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
// instance's title line and an external lane's row must place the
// percentage field in the same column (see that test's own comment on why
// "%" is the marker this now keys off, not "ctx").
func TestString_TrackedAndExternalRows_ShareCtxColumn(t *testing.T) {
	l := newTestList("a-tracked-lane")
	l.SetSize(120, 40)
	l.items[0].SetContextFill(1, true)
	l.SetExternal([]clarity.ExternalLane{{Name: "an-external-lane", LastWrite: time.Now(), Fill: clarity.Fill{Pct: 1}, FillOK: true}})

	out := l.String()
	var pctCols []int
	for _, line := range strings.Split(out, "\n") {
		// Strip ANSI first: the tracked row and the external row carry
		// different escape sequences (different styles), so a raw
		// strings.Index would compare byte offsets that include those
		// codes, not the rendered column.
		if idx := runeIndexOf(ansi.Strip(line), '%'); idx >= 0 {
			pctCols = append(pctCols, idx)
		}
	}
	require.Len(t, pctCols, 2, "expected exactly the tracked row and the external row to carry a percentage column")
	require.Equal(t, pctCols[0], pctCols[1], "the percentage must land in the same column for a tracked row and an external row alike")
}

// TestLaneRow_NarrowInnerWidth_KeepsFullNameWithTimeAndPaddedWord is the
// pane-3b2 defect's own repro: the orchestrator's 164x45 capture of this
// worktree's PRE-FIX build truncated a lane named "ways-of-working" to
// "ways-of-w…" because laneStateWordWidth was measuring StateWaitingYou's
// own 14-char "waiting on you" phrase, reserving 15 columns for a word
// column that only ever shows 7 - starving the name column. At an inner
// width of 44 (SetSize(46, ...); laneRowInnerWidth subtracts the row
// styles' 2-column padding) that name must now render in full, "working"
// occupies its padded-to-7 column, and the last-turn time still shows since
// 44 sits at/above laneShowTimeMinWidth.
func TestLaneRow_NarrowInnerWidth_KeepsFullNameWithTimeAndPaddedWord(t *testing.T) {
	l := newTestList("ways-of-working")
	l.SetSize(46, 40)
	require.Equal(t, 44, laneRowInnerWidth(l.width), "test fixture must exercise exactly the inner width named in the brief")

	lastTurn := time.Date(2026, 9, 2, 22, 30, 0, 0, time.Local)
	l.items[0].SetLaneState(clarity.StateWorking, lastTurn, true)

	out := ansi.Strip(l.String())
	require.Contains(t, out, "ways-of-working", "the full 15-character lane name must render, not truncated")
	require.NotContains(t, out, "ways-of-w…", "the pane-3b2 defect's own truncated form must not reappear")
	require.Contains(t, out, "working 22:30", "the word (padded to 7 - a no-op pad for \"working\" itself) is followed by the last-turn time")
}

// TestLaneRow_UnderTimeThreshold_DropsTimeKeepsFullName is THE RULE's other
// named threshold: below laneShowTimeMinWidth (42) inner columns, the
// last-turn time is dropped ENTIRELY (not blanked to a same-width gap) so
// the name column keeps the room instead - the mock-up's 120-column rows
// (PANE-MOCKUP-120x36.md) carry no time field at all. At inner width 37
// (SetSize(39, ...)) the lane name still renders whole even though the row
// column budget is narrower than the 44-wide case above, because dropping
// the time frees exactly the room the name needs.
func TestLaneRow_UnderTimeThreshold_DropsTimeKeepsFullName(t *testing.T) {
	l := newTestList("ways-of-working")
	l.SetSize(39, 40)
	require.Equal(t, 37, laneRowInnerWidth(l.width), "test fixture must exercise exactly the inner width named in the brief")

	lastTurn := time.Date(2026, 9, 2, 22, 30, 0, 0, time.Local)
	l.items[0].SetLaneState(clarity.StateWorking, lastTurn, true)

	out := ansi.Strip(l.String())
	require.Contains(t, out, "ways-of-working", "the full lane name must still render once the time is dropped for room")
	require.NotContains(t, out, "22:30", "below laneShowTimeMinWidth the time is dropped entirely, never shown")
}

// TestLaneRow_WaitingOnYou_RendersWaitingNotFullPhrase is THE RULE's word-
// vocabulary requirement: a lane row's four words are exactly
// working/waiting/idle/stalled, so a state of clarity.StateWaitingYou
// ("waiting on you") renders as the short "waiting" on the row - the
// Session pane's own header and state line (session.go) are the ones that
// keep the full phrase, unchanged by this leg.
func TestLaneRow_WaitingOnYou_RendersWaitingNotFullPhrase(t *testing.T) {
	l := newTestList("a-lane")
	l.SetSize(120, 40)
	l.items[0].SetLaneState(clarity.StateWaitingYou, time.Now(), true)

	out := l.String()
	require.Contains(t, out, "waiting", "the row shows the short word for waiting on you")
	require.NotContains(t, out, "waiting on you", "the row never renders the Session pane's own full phrase")
}
