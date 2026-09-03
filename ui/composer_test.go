package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestComposer_ClosedByDefault(t *testing.T) {
	c := NewComposer()
	require.False(t, c.IsOpen())
	require.Equal(t, "", c.Value())
}

func TestComposer_OpenFocusesOnLaneWithEmptyText(t *testing.T) {
	c := NewComposer()
	c.Open("ways-of-working", false)

	require.True(t, c.IsOpen())
	require.Equal(t, "ways-of-working", c.Lane())
	require.False(t, c.IsExternal())
	require.Equal(t, "", c.Value())
}

func TestComposer_TypeAndBackspace(t *testing.T) {
	c := NewComposer()
	c.Open("lane-a", false)
	c.Type("scratchfix hello")
	require.Equal(t, "scratchfix hello", c.Value())

	c.Backspace()
	require.Equal(t, "scratchfix hell", c.Value())
}

func TestComposer_BackspaceOnEmptyIsNoOp(t *testing.T) {
	c := NewComposer()
	c.Open("lane-a", false)
	c.Backspace()
	require.Equal(t, "", c.Value())
}

func TestComposer_BackspaceIsRuneAware(t *testing.T) {
	c := NewComposer()
	c.Open("lane-a", false)
	c.Type("café")
	c.Backspace()
	require.Equal(t, "caf", c.Value())
}

func TestComposer_Close_ClearsTextButNotOpenTarget(t *testing.T) {
	c := NewComposer()
	c.Open("lane-a", false)
	c.Type("half-typed")
	c.Close()

	require.False(t, c.IsOpen())
	require.Equal(t, "", c.Value())
}

func TestComposer_OpenAfterCloseStartsEmpty(t *testing.T) {
	c := NewComposer()
	c.Open("lane-a", false)
	c.Type("leftover")
	c.Close()

	c.Open("lane-b", true)
	require.Equal(t, "", c.Value(), "a fresh Open never carries over the previous target's typed text")
	require.True(t, c.IsExternal())
}

func TestComposer_SetResult_ClosesAndClearsText(t *testing.T) {
	c := NewComposer()
	c.Open("lane-a", false)
	c.Type("hello")
	c.SetResult("sent · landed 14:32:07")

	require.False(t, c.IsOpen())
	require.Equal(t, "", c.Value())
	require.True(t, c.HasResult())
	require.Equal(t, "sent · landed 14:32:07", c.Result())
}

func TestComposer_OpenClearsPreviousResult(t *testing.T) {
	c := NewComposer()
	c.Open("lane-a", false)
	c.SetResult("copied · this lane runs in your own terminal, paste it there")
	require.True(t, c.HasResult())

	c.Open("lane-b", false)
	require.False(t, c.HasResult())
}

// -- Render: the mock-up's own three-line box ----------------------------

func TestComposer_Render_IdleFootReadsMMessage(t *testing.T) {
	c := NewComposer()
	lines := c.Render(60, "ways-of-working")
	require.Len(t, lines, 3)
	require.Contains(t, ansi.Strip(lines[0]), "message ways-of-working")
	require.Contains(t, ansi.Strip(lines[2]), ComposerFootIdle)
}

func TestComposer_Render_OpenFootReadsEnterSendEscCancel(t *testing.T) {
	c := NewComposer()
	c.Open("ways-of-working", false)
	lines := c.Render(60, "ways-of-working")
	require.Contains(t, ansi.Strip(lines[2]), ComposerFootEditing)
}

func TestComposer_Render_OpenShowsTypedTextAndCursor(t *testing.T) {
	c := NewComposer()
	c.Open("lane-a", false)
	c.Type("hello")
	lines := c.Render(60, "lane-a")
	require.Contains(t, ansi.Strip(lines[1]), "hello"+composerCursor)
}

func TestComposer_Render_TitleNamesOpenTargetNotCurrentSelection(t *testing.T) {
	c := NewComposer()
	c.Open("captured-lane", false)
	// The current selection has moved on to a different row by render time
	// - the box must still name the row the send is actually addressed to.
	lines := c.Render(60, "some-other-row-now-selected")
	require.Contains(t, ansi.Strip(lines[0]), "captured-lane")
	require.NotContains(t, ansi.Strip(lines[0]), "some-other-row-now-selected")
}

func TestComposer_Render_ResultFootOverridesIdle(t *testing.T) {
	c := NewComposer()
	c.Open("lane-a", false)
	c.SetResult("sent · landed 09:15:30")
	lines := c.Render(60, "lane-a")
	require.Contains(t, ansi.Strip(lines[2]), "sent · landed 09:15:30")
}

// TestSessionPane_ComposerRendersEvenWithNoSessionInfo pins the PROOF (b)
// scratchfix defect: a tracked lane whose program keeps no Claude
// transcript at all (a `cat`-backed lane; SetInfo(nil) is exactly what
// app.go's selectedSessionInfo returns for it) must still show the
// composer once it is open - the resting frame's own "no SessionInfo" path
// must not silently swallow it.
func TestSessionPane_ComposerRendersEvenWithNoSessionInfo(t *testing.T) {
	s := NewSessionPane()
	composer := NewComposer()
	s.SetComposer(composer)
	s.SetSize(60, 20)
	s.SetInfo(nil) // no transcript for this lane - the resting frame's own case

	composer.Open("scratchfix-pane5-msg", false)
	out := ansi.Strip(s.String())
	require.Contains(t, out, "message scratchfix-pane5-msg", "the composer must render even when there is no SessionInfo to show")
	require.Contains(t, out, ComposerFootEditing)
}

func TestSessionPane_RestingFrameHasNoComposerWhenClosed(t *testing.T) {
	s := NewSessionPane()
	s.SetComposer(NewComposer())
	s.SetSize(60, 20)
	s.SetInfo(nil)

	out := ansi.Strip(s.String())
	require.NotContains(t, out, "message", "the plain resting frame carries no composer box until m is pressed")
}

// TestComposer_Render_CopyOnlyTitle_ShownBeforeTyping is board #280 pane-10
// walkthrough DEFECT 1, seen failing first: a lane resolved copy-only (a
// tracked instance with no live tmux session, or a genuine external lane)
// must say so in the title the moment the box opens, before any text is
// typed - never only after enter is pressed and the send fails.
func TestComposer_Render_CopyOnlyTitle_ShownBeforeTyping(t *testing.T) {
	c := NewComposer()
	c.Open("ways-of-working", true)

	require.Contains(t, ansi.Strip(c.Render(60, "ways-of-working")[0]), "message ways-of-working · copy only")
}

// TestComposer_Render_TrackedLaneTitleCarriesNoCopyOnlySuffix is the fix's
// other half: a genuinely sendable tracked lane never carries the suffix.
func TestComposer_Render_TrackedLaneTitleCarriesNoCopyOnlySuffix(t *testing.T) {
	c := NewComposer()
	c.Open("ways-of-working", false)

	require.NotContains(t, ansi.Strip(c.Render(60, "ways-of-working")[0]), "copy only")
}

// TestComposer_Render_NoLaneRow_NeverCarriesCopyOnlySuffix pins the fix
// against DEFECT 2's own no-lane state (composerTarget's isExternal=true
// fallback when a Needs-you row's lane does not resolve at all) - the title
// stays exactly "(no lane on this row)", never "... · copy only" appended
// to a target that does not exist to begin with.
func TestComposer_Render_NoLaneRow_NeverCarriesCopyOnlySuffix(t *testing.T) {
	c := NewComposer()
	c.Open("", true)

	title := ansi.Strip(c.Render(60, "")[0])
	require.Contains(t, title, NoLaneLabel)
	require.NotContains(t, title, "copy only")
}

// -- Render: slice 16, the composer wraps ---------------------------------
//
// The owner (3 Sep 11:2x, verbatim): "also this prompt bit doesnt wrap so i
// cant see what im typing over a certain number of characters". These pin
// the RULE's own behaviours: word-wrap growth, the five-line cap with an
// always-visible cursor, an inserted newline surviving into the sent
// value, and the two panes' scrollable region shrinking to make room.

// composerWordsOfLen220 is a 220-character message built from 22 nine-
// letter words - the PROOF's own paste length, chosen so it wraps to
// exactly three lines at the width the RULE's own test names (100, giving
// an inner wrap width of 100-4-2=94 columns, 9 words per line).
func composerWordsOfLen220() string {
	return strings.TrimSpace(strings.Repeat("aaaaaaaaa ", 22))
}

func TestComposer_Render_WrapsAtWordBoundaries_ThreeLinesAtWidth100(t *testing.T) {
	c := NewComposer()
	c.Open("ways-of-working", false)
	c.Type(composerWordsOfLen220())

	lines := c.Render(100, "ways-of-working")
	// top border + N content rows + bottom border.
	require.Len(t, lines, 5, "a 220-char message must wrap to 3 content rows at width 100")
	require.Contains(t, ansi.Strip(lines[3]), composerCursor, "the cursor must be visible on the last wrapped row")
	for _, l := range lines {
		require.LessOrEqual(t, ansi.StringWidth(l), 100)
	}
}

func TestComposer_Height_MatchesRenderLineCount(t *testing.T) {
	c := NewComposer()
	c.Open("lane-a", false)
	c.Type(composerWordsOfLen220())

	require.Equal(t, len(c.Render(100, "lane-a")), c.Height(100))
}

func TestComposer_Render_GrowthCapsAtFiveLines(t *testing.T) {
	c := NewComposer()
	c.Open("lane-a", false)
	// composerWrapParagraph packs one 9-letter word per "aaaaaaaaa " unit,
	// 9 fit per 94-column line at width 100 - 90 words is 10 wrapped lines,
	// well past the five-line cap.
	c.Type(strings.TrimSpace(strings.Repeat("aaaaaaaaa ", 90)))

	lines := c.Render(100, "lane-a")
	require.Len(t, lines, composerMaxVisibleLines+2, "growth must cap at five content rows, never more")
	require.Contains(t, ansi.Strip(lines[len(lines)-2]), composerCursor, "the cursor stays on the always-visible last row once the box scrolls")
}

func TestComposer_InsertNewline_SurvivesIntoValue(t *testing.T) {
	c := NewComposer()
	c.Open("lane-a", false)
	c.Type("first line")
	c.InsertNewline()
	c.Type("second line")

	require.Equal(t, "first line\nsecond line", c.Value(), "a newline inserted with the chord must survive into the sent text")
}

func TestComposer_Render_InsertedNewlineShowsAsTwoRows(t *testing.T) {
	c := NewComposer()
	c.Open("lane-a", false)
	c.Type("first")
	c.InsertNewline()
	c.Type("second")

	lines := c.Render(60, "lane-a")
	require.Len(t, lines, 4, "top + two content rows (one per side of the inserted newline) + bottom")
	require.Contains(t, ansi.Strip(lines[1]), "first")
	require.Contains(t, ansi.Strip(lines[2]), "second"+composerCursor)
}

// TestSessionPane_TurnsViewportShrinksWithComposerGrowth is the RULE's own
// "the turns viewport above shrinks by the same rows so nothing overlaps",
// read through ui/session.go's own turnsAreaHeight (unexported, same
// package) rather than a full String() diff - the pane layout wiring this
// leg's brief scopes in alongside composer.go itself.
func TestSessionPane_TurnsViewportShrinksWithComposerGrowth(t *testing.T) {
	pinHome(t)
	s := NewSessionPane()
	composer := NewComposer()
	s.SetComposer(composer)
	s.SetSize(100, 30)
	s.SetInfo(fixtureInfo())

	baseline := s.turnsAreaHeight()

	composer.Open("ways-of-working", false)
	oneLine := s.turnsAreaHeight()
	require.Equal(t, baseline, oneLine, "opening on empty text is still the legacy one-line box - no shrink yet")

	composer.Type(composerWordsOfLen220())
	grown := s.turnsAreaHeight()
	require.Equal(t, oneLine-2, grown, "a 1-row -> 3-row composer must cost the turns viewport exactly its 2 extra rows")
}

// TestNeedsYouPane_ContentAreaShrinksWithComposerGrowth is the same wiring
// on the Needs-you tab's own scrollable region (ui/needsyou.go's
// contentAreaHeight).
func TestNeedsYouPane_ContentAreaShrinksWithComposerGrowth(t *testing.T) {
	p := NewNeedsYouPane()
	composer := NewComposer()
	p.SetComposer(composer)
	p.SetSize(100, 30)
	p.SetInfo(&NeedsYouInfo{Lane: "ways-of-working"})

	baseline := p.contentAreaHeight()

	composer.Open("ways-of-working", false)
	composer.Type(composerWordsOfLen220())
	grown := p.contentAreaHeight()

	require.Equal(t, baseline-2, grown, "a 1-row -> 3-row composer must cost the content area exactly its 2 extra rows")
}

func TestComposer_Render_NeverExceedsWidth(t *testing.T) {
	c := NewComposer()
	c.Open(strings.Repeat("a-very-long-lane-name-", 5), false)
	c.Type(strings.Repeat("x", 200))
	for _, w := range []int{20, 60, 120, 200} {
		for _, line := range c.Render(w, "lane-a") {
			require.LessOrEqualf(t, ansi.StringWidth(line), w, "width %d: %q", w, line)
		}
	}
}
