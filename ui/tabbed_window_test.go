package ui

import (
	"claude-squad/session/clarity"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/stretchr/testify/require"
)

// TestTabbedWindow_SessionIsFirstAndDefaultTab is the brief's own named
// requirement: the tabbed window's tabs become Session · Needs you ·
// Terminal, Session first and the default (design/cockpit-pane/
// DECISIONS.md slices 3 and 5).
func TestTabbedWindow_SessionIsFirstAndDefaultTab(t *testing.T) {
	w := NewTabbedWindow(NewSessionPane(), NewNeedsYouPane(), newTerminalPaneWithDeps(&MockPtyFactory{t: t, cmdExec: mockCmdExec("", false)}, mockCmdExec("", false)))
	require.Equal(t, 0, w.GetActiveTab(), "Session must be the default tab")
	require.True(t, w.IsInSessionTab())
	require.Equal(t, SessionTab, 0)
	require.Equal(t, NeedsYouTab, 1)
	require.Equal(t, TerminalTab, 2)
}

// TestTabbedWindow_ScrollKeys_DispatchToSessionPane proves shift+up/down's
// own wiring end to end: TabbedWindow.ScrollUp/ScrollDown, while the
// Session tab is active, actually move the Session pane's own viewport
// (the same keys.KeyShiftUp/KeyShiftDown app.go already binds for
// Preview/Needs-you/Terminal - this slice reuses them, no new binding).
func TestTabbedWindow_ScrollKeys_DispatchToSessionPane(t *testing.T) {
	w := NewTabbedWindow(NewSessionPane(), NewNeedsYouPane(), newTerminalPaneWithDeps(&MockPtyFactory{t: t, cmdExec: mockCmdExec("", false)}, mockCmdExec("", false)))
	w.SetSize(200, 40)
	require.True(t, w.IsInSessionTab())

	base := time.Date(2026, 9, 2, 18, 0, 0, 0, time.Local)
	var turns []clarity.Turn
	for i := 0; i < 60; i++ {
		turns = append(turns, clarity.Turn{Kind: clarity.TurnOwner, At: base.Add(time.Duration(i) * time.Minute), Text: "turn body"})
	}
	w.SetSessionInfo(&SessionInfo{Lane: "x", Tail: clarity.LaneTail{Turns: turns, State: clarity.StateIdle}})

	before := w.String()
	for i := 0; i < 200; i++ {
		w.ScrollUp()
	}
	after := w.String()
	require.NotEqual(t, before, after, "ScrollUp on the Session tab must change what the pane shows")
	require.True(t, strings.Contains(w.String(), "turn body"))
}

// TestTabbedWindow_ScrollKeys_DispatchToNeedsYouPane is the same end-to-end
// wiring proof for the Needs-you tab (slice 5's "scroll with the same keys
// as Session").
func TestTabbedWindow_ScrollKeys_DispatchToNeedsYouPane(t *testing.T) {
	w := NewTabbedWindow(NewSessionPane(), NewNeedsYouPane(), newTerminalPaneWithDeps(&MockPtyFactory{t: t, cmdExec: mockCmdExec("", false)}, mockCmdExec("", false)))
	w.SetSize(30, 20)
	w.SetActiveTab(NeedsYouTab)
	require.True(t, w.IsInNeedsYouTab())

	w.SetNeedsYouInfo(&NeedsYouInfo{
		Item:        clarity.FeedItem{Rank: 1, Title: "t"},
		Lane:        "lane-a",
		Explanation: []clarity.BoardSection{{Text: strings.Repeat("word ", 200)}},
		Options:     []clarity.BoardOption{{Text: "ok"}},
	})

	before := w.String()
	for i := 0; i < 40; i++ {
		w.ScrollDown()
	}
	after := w.String()
	require.NotEqual(t, before, after, "ScrollDown on the Needs-you tab must change what the pane shows")
}

// newButterflyTestWindow gives every butterfly test the same fixture: a
// tabbed window sized wide enough (164, this leg's own proof width) that
// the tab bar is never in the collapsed case.
func newButterflyTestWindow(t *testing.T) *TabbedWindow {
	w := NewTabbedWindow(NewSessionPane(), NewNeedsYouPane(), newTerminalPaneWithDeps(&MockPtyFactory{t: t, cmdExec: mockCmdExec("", false)}, mockCmdExec("", false)))
	w.SetSize(164, 45)
	return w
}

// butterflyBorderLine returns the tab bar's own top border row (the
// "╭───╮" line, ANSI stripped) from a rendered TabbedWindow.String().
func butterflyBorderLine(t *testing.T, s string) string {
	t.Helper()
	lines := strings.Split(s, "\n")
	require.GreaterOrEqual(t, len(lines), 3, "expected at least a spacer/spacer/border row")
	return ansi.Strip(lines[2])
}

// TestButterfly_FramesAreSingleWidth is the brief's own named requirement:
// both wing frames render as exactly one terminal cell, proven by
// StringWidth (go-runewidth's own contract, used here via x/ansi which
// wraps the same grapheme-width table) - no emoji, no combining character.
func TestButterfly_FramesAreSingleWidth(t *testing.T) {
	for _, f := range butterflyFrames {
		require.Equal(t, 1, ansi.StringWidth(f), "butterfly frame %q must be single-width", f)
		require.Equal(t, 1, runewidth.StringWidth(f), "butterfly frame %q must be single-width under go-runewidth too", f)
	}
}

// TestButterfly_RestsCenteredOverActiveTab proves rule 2's rest position:
// with no tab change, the butterfly sits on the border row directly above
// the active tab's own centre column.
func TestButterfly_RestsCenteredOverActiveTab(t *testing.T) {
	w := newButterflyTestWindow(t)
	for i := 0; i < 3; i++ {
		w.TickButterfly()
	}

	col, row := w.butterflyPosition()
	require.Equal(t, 0, row, "at rest the butterfly draws on the border row")
	require.Equal(t, w.butterflyTabCenterCol(SessionTab), col)

	line := butterflyBorderLine(t, w.String())
	runes := []rune(line)
	require.Less(t, col, len(runes))
	require.Contains(t, butterflyFrames, string(runes[col]), "border row must carry a wing glyph at the rest column")
}

// TestButterfly_TabChangeEndsCenteredOnNewTab is the brief's own named
// requirement: a tab change starts a flight that, within 20 ticks, ends
// centred over the new tab (design rule 2).
func TestButterfly_TabChangeEndsCenteredOnNewTab(t *testing.T) {
	w := newButterflyTestWindow(t)
	w.Toggle() // Session -> Needs you
	require.True(t, w.butterflyFlying, "a tab change must start a flight")

	for i := 0; i < 20; i++ {
		w.TickButterfly()
	}
	require.False(t, w.butterflyFlying, "the flight must have settled within 20 ticks")

	col, row := w.butterflyPosition()
	require.Equal(t, 0, row)
	require.Equal(t, w.butterflyTabCenterCol(NeedsYouTab), col)

	line := butterflyBorderLine(t, w.String())
	runes := []rune(line)
	require.Contains(t, butterflyFrames, string(runes[col]))
}

// TestButterfly_FlightPassesThroughTheSpacerRow proves the "lifts off (one
// row up if there is a free row)" branch actually fires mid-flight, not
// just at rest/landing.
func TestButterfly_FlightPassesThroughTheSpacerRow(t *testing.T) {
	w := newButterflyTestWindow(t)
	w.Toggle()

	sawSpacerRow := false
	for i := 0; i < butterflyFlightTicks; i++ {
		w.TickButterfly()
		if _, row := w.butterflyPosition(); row == -1 {
			sawSpacerRow = true
		}
	}
	require.True(t, sawSpacerRow, "a flight must lift onto the free spacer row for at least one tick")
}

// TestButterfly_RestCycleOrderAndCadence is slice 23's own named
// requirement: the rest beat is a four-step cycle - closed, half, open,
// half - each frame holding for its own butterflyRestFrameTicks share
// before advancing (never early, never late), wrapping from the second
// half back to closed, with the open frame proven to hold longest (design
// refinement 1's "easing so the open state holds longest") and the whole
// beat proven to take about 1.5s.
func TestButterfly_RestCycleOrderAndCadence(t *testing.T) {
	w := newButterflyTestWindow(t)
	require.Equal(t, 0, w.butterflyFrame, "must start on the closed frame")
	require.Equal(t, "ʚ", butterflyFrames[0], "index 0 is the closed frame")
	require.Equal(t, "ɞ", butterflyFrames[2], "index 2 is the open frame")
	require.Equal(t, butterflyFrames[1], butterflyFrames[3], "both half-beat frames are the same glyph")

	frameOrder := [4]int{0, 1, 2, 3} // closed, half, open, half
	for i, current := range frameOrder {
		next := frameOrder[(i+1)%len(frameOrder)]
		hold := butterflyRestFrameTicks[current]
		for tick := 0; tick < hold-1; tick++ {
			w.TickButterfly()
			require.Equal(t, current, w.butterflyFrame, "must hold frame %d for its own %d ticks", current, hold)
		}
		w.TickButterfly()
		require.Equal(t, next, w.butterflyFrame, "must advance from frame %d to %d exactly on cadence", current, next)
	}

	require.Greater(t, butterflyRestFrameTicks[2], butterflyRestFrameTicks[0], "the open frame must hold longer than the closed frame")
	require.Greater(t, butterflyRestFrameTicks[2], butterflyRestFrameTicks[1], "the open frame must hold longer than a half frame")
	total := 0
	for _, ticks := range butterflyRestFrameTicks {
		total += ticks
	}
	require.Equal(t, 15, total, "one full four-step beat should take about 1.5s (15 ticks at the 100ms tick)")
}

// TestButterfly_IdleWander_TriggersWithinWindowStaysInBoundsAndReturns is
// slice 23's own named requirement for design refinement 2: forced due
// on the very next tick (butterflyTicksUntilWander is this test's own
// seam - the real window is 45-90s, far too long to wait out here), the
// wander must lift off, drift three to six columns, pause, stay within the
// tab bar's own width throughout, and return exactly to its own tab.
func TestButterfly_IdleWander_TriggersWithinWindowStaysInBoundsAndReturns(t *testing.T) {
	w := newButterflyTestWindow(t)
	w.butterflyTicksUntilWander = 1

	restCol, restRow := w.butterflyPosition()
	require.Equal(t, 0, restRow)
	maxCol := w.width + windowStyle.GetHorizontalFrameSize() - 1

	var (
		started, ended, sawSpacerRow, sawPeak bool
		peakCol                               int
	)
	for i := 0; i < 2*butterflyWanderTravelTicks+butterflyWanderPauseTicks+5; i++ {
		w.TickButterfly()
		if w.butterflyWandering {
			started = true
			col, row := w.butterflyPosition()
			require.GreaterOrEqual(t, col, 0, "a wander must never leave the tab bar's own width")
			require.LessOrEqual(t, col, maxCol, "a wander must never leave the tab bar's own width")
			if row == -1 {
				sawSpacerRow = true
			}
			if w.butterflyWanderPhase == butterflyWanderPause {
				sawPeak = true
				peakCol = col
			}
		} else if started {
			ended = true
			break
		}
	}

	require.True(t, started, "the wander must trigger within its own window")
	require.True(t, sawSpacerRow, "a wander must lift onto the free spacer row")
	require.True(t, sawPeak, "a wander must pause at its far point")
	drift := peakCol - restCol
	if drift < 0 {
		drift = -drift
	}
	require.GreaterOrEqual(t, drift, butterflyWanderMinDriftCols, "a wander must drift at least three columns")
	require.LessOrEqual(t, drift, butterflyWanderMaxDriftCols, "a wander must drift at most six columns")
	require.True(t, ended, "the wander must complete within this window")
	col, row := w.butterflyPosition()
	require.Equal(t, restCol, col, "a completed wander must return to its own tab")
	require.Equal(t, 0, row, "a completed wander must land back on the border row")
}

// TestButterfly_NoticeNeedsYou_FliesToNeedsYouAndReturns is slice 23's own
// named requirement for design refinement 3: a genuinely new board issue
// number starts a flight to the Needs-you tab, a hover, then a flight back
// to whichever tab was active - and the active tab itself never changes.
func TestButterfly_NoticeNeedsYou_FliesToNeedsYouAndReturns(t *testing.T) {
	w := newButterflyTestWindow(t)
	require.True(t, w.IsInSessionTab())

	homeCol, _ := w.butterflyPosition()
	needsYouCol := w.butterflyTabCenterCol(NeedsYouTab)

	w.NoticeNeedsYou([]clarity.FeedItem{{Source: "#100"}})
	require.False(t, w.butterflyNoticing, "the first call only primes the seen-set, it must never fly")

	w.NoticeNeedsYou([]clarity.FeedItem{{Source: "#100"}, {Source: "#200"}})
	require.True(t, w.butterflyNoticing, "a new Needs-you row must start a notice")

	for i := 0; i < butterflyFlightTicks; i++ {
		w.TickButterfly()
	}
	col, row := w.butterflyPosition()
	require.Equal(t, needsYouCol, col, "the outbound leg must end centred over Needs you")
	require.Equal(t, 0, row)

	for i := 0; i < butterflyNoticeHoverTicks; i++ {
		w.TickButterfly()
		require.True(t, w.butterflyNoticing, "must still be noticing through the hover")
		c, r := w.butterflyPosition()
		require.Equal(t, needsYouCol, c, "must hold over Needs you for the whole hover")
		require.Equal(t, 0, r)
	}

	for i := 0; i < butterflyFlightTicks; i++ {
		w.TickButterfly()
	}
	require.False(t, w.butterflyNoticing, "the notice must end once the inbound leg settles")
	col, row = w.butterflyPosition()
	require.Equal(t, homeCol, col, "the inbound leg must return to the tab that was active")
	require.Equal(t, 0, row)
	require.True(t, w.IsInSessionTab(), "NoticeNeedsYou must never change the active tab itself")
}

// TestButterfly_NoticeNeedsYou_HoversInPlaceWhenNeedsYouIsActive is the
// brief's own named branch of design refinement 3: "If the Needs you tab
// IS the active tab it just hovers in place" - no flight legs at all.
func TestButterfly_NoticeNeedsYou_HoversInPlaceWhenNeedsYouIsActive(t *testing.T) {
	w := newButterflyTestWindow(t)
	w.SetActiveTab(NeedsYouTab)
	for i := 0; i < butterflyFlightTicks; i++ {
		w.TickButterfly()
	}
	require.False(t, w.butterflyFlying, "the tab-change flight from SetActiveTab must have settled first")
	col, _ := w.butterflyPosition()

	w.NoticeNeedsYou([]clarity.FeedItem{{Source: "#100"}})
	w.NoticeNeedsYou([]clarity.FeedItem{{Source: "#100"}, {Source: "#200"}})
	require.True(t, w.butterflyNoticing)
	require.False(t, w.butterflyNoticeNeedsFlight, "already on Needs you - hover only, no flight legs")

	for i := 0; i < butterflyNoticeHoverTicks-1; i++ {
		w.TickButterfly()
		c, r := w.butterflyPosition()
		require.Equal(t, col, c, "must not move while hovering in place")
		require.Equal(t, 0, r)
	}
	w.TickButterfly()
	require.False(t, w.butterflyNoticing, "a hover-only notice must end after its own hover ticks")
}

// TestButterfly_ToggleButterflyEnabled_Flips is design refinement 4's own
// named requirement: capital B toggles the butterfly live. Wiring the
// actual keypress is the key-dispatch leg's own job (app.go/keys.go,
// outside this file's fence) - this proves the capability it will call.
func TestButterfly_ToggleButterflyEnabled_Flips(t *testing.T) {
	w := newButterflyTestWindow(t)
	require.True(t, w.butterflyEnabled)
	w.ToggleButterflyEnabled()
	require.False(t, w.butterflyEnabled)
	w.ToggleButterflyEnabled()
	require.True(t, w.butterflyEnabled)
}

// TestButterfly_TabBarLineWidthUnchanged proves the overlay is a
// replacement, never an insertion: the border row's visible width with the
// butterfly drawn equals its width with the butterfly hidden.
func TestButterfly_TabBarLineWidthUnchanged(t *testing.T) {
	shown := newButterflyTestWindow(t)
	hidden := newButterflyTestWindow(t)
	hidden.SetButterflyEnabled(false)

	shownLine := butterflyBorderLine(t, shown.String())
	hiddenLine := butterflyBorderLine(t, hidden.String())
	require.NotEqual(t, shownLine, hiddenLine, "the butterfly must actually change the border row")
	require.Equal(t, ansi.StringWidth(hiddenLine), ansi.StringWidth(shownLine),
		"drawing the butterfly must not change the tab bar row's own width")
}

// TestButterfly_HiddenUnderFlag proves the --no-butterfly flag/config path
// (SetButterflyEnabled(false)): no wing glyph anywhere in the render, and
// ticking it is a no-op.
func TestButterfly_HiddenUnderFlag(t *testing.T) {
	w := newButterflyTestWindow(t)
	w.SetButterflyEnabled(false)
	w.Toggle()
	for i := 0; i < butterflyFlightTicks; i++ {
		w.TickButterfly()
	}

	s := w.String()
	for _, f := range butterflyFrames {
		require.NotContains(t, s, f, "no butterfly frame may appear while disabled")
	}
	require.False(t, w.butterflyFlying, "TickButterfly must be a no-op while disabled")
}

// TestButterfly_HiddenInCollapsedLayout proves design rule 1's "if the tab
// bar is too narrow (collapsed layout) it is not drawn": at the collapsed
// width (SetSize's own w.width==0 branch, app.go's OVERFLOW fix), String()
// renders nothing at all - the butterfly included, since there is no tab
// bar row left to draw it on.
func TestButterfly_HiddenInCollapsedLayout(t *testing.T) {
	w := newButterflyTestWindow(t)
	w.SetSize(0, 45)
	require.Empty(t, w.String(), "a collapsed tab bar draws nothing, butterfly included")
}
