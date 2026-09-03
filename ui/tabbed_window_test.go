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

// TestButterfly_FrameAlternatesAtRestCadence proves the rest flap cadence
// (design rule 1: "flaps once every 1.2s or so", butterflyFlapTicksRest
// ticks at 100ms each) and that nothing moves faster than that at rest.
func TestButterfly_FrameAlternatesAtRestCadence(t *testing.T) {
	w := newButterflyTestWindow(t)
	start := w.butterflyFrame

	for i := 0; i < butterflyFlapTicksRest-1; i++ {
		w.TickButterfly()
		require.Equal(t, start, w.butterflyFrame, "must not flip before the rest cadence elapses")
	}
	w.TickButterfly()
	require.NotEqual(t, start, w.butterflyFrame, "must flip exactly on the rest cadence")
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
