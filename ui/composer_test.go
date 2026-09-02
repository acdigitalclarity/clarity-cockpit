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
