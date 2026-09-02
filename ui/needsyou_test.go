package ui

import (
	"claude-squad/session/clarity"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestNeedsYouPane_NothingSelected_ShowsPlainMessage(t *testing.T) {
	p := NewNeedsYouPane()
	p.SetSize(80, 20)

	out := p.String()
	require.Contains(t, out, "select a Needs-you row")
}

func TestNeedsYouPane_RendersRankTitleLaneAndPriority(t *testing.T) {
	p := NewNeedsYouPane()
	p.SetSize(80, 20)
	p.SetInfo(&NeedsYouInfo{
		Item:           clarity.FeedItem{Rank: 1, Class: "blocked-on-owner", Source: "#277", Lane: "#277", Title: "Owner: one settings act"},
		Explanation:    "the plain-words explanation",
		Recommendation: "do the thing",
	})

	out := ansi.Strip(p.String())
	require.Contains(t, out, "1. Owner: one settings act", "line 1: row number and title")
	require.Contains(t, out, "#277 · blocked-on-owner", "line 2: the raising lane and its priority")
	require.Contains(t, out, "the plain-words explanation")
	require.Contains(t, out, "Recommended response:")
	require.Contains(t, out, "do the thing")
}

func TestNeedsYouPane_BoardUnreachable_RendersOnlyThatLine(t *testing.T) {
	p := NewNeedsYouPane()
	p.SetSize(80, 20)
	p.SetInfo(&NeedsYouInfo{
		Item:             clarity.FeedItem{Rank: 1, Title: "some row", Lane: "#1", Class: "blocked-on-owner"},
		BoardUnreachable: "rate limited",
	})

	out := ansi.Strip(p.String())
	require.Contains(t, out, "board unreachable: rate limited")
	require.NotContains(t, out, "Recommended response:", "a fetch failure renders one plain line, nothing else")
}

func TestNeedsYouPane_Loading_ShowsFetchingLine(t *testing.T) {
	p := NewNeedsYouPane()
	p.SetSize(80, 20)
	p.SetInfo(&NeedsYouInfo{
		Item:    clarity.FeedItem{Rank: 1, Title: "some row", Lane: "#1", Class: "blocked-on-owner"},
		Loading: true,
	})

	out := ansi.Strip(p.String())
	require.Contains(t, out, "fetching")
}

func TestNeedsYouPane_WrapsExplanationToWidth(t *testing.T) {
	p := NewNeedsYouPane()
	p.SetSize(20, 20)
	p.SetInfo(&NeedsYouInfo{
		Item:           clarity.FeedItem{Rank: 1, Title: "t", Lane: "#1", Class: "blocked-on-owner"},
		Explanation:    strings.Repeat("word ", 30),
		Recommendation: "ok",
	})

	for i, line := range strings.Split(ansi.Strip(p.String()), "\n") {
		require.LessOrEqualf(t, ansi.StringWidth(line), 20, "line %d: %q", i, line)
	}
}

func TestNeedsYouPane_ClearShowsNothingSelectedAgain(t *testing.T) {
	p := NewNeedsYouPane()
	p.SetSize(80, 20)
	p.SetInfo(&NeedsYouInfo{Item: clarity.FeedItem{Rank: 1, Title: "t"}, Recommendation: "r"})
	p.Clear()

	require.Contains(t, p.String(), "select a Needs-you row")
}

func TestNeedsYouPane_ScrollPreservedAcrossSameRowRefresh(t *testing.T) {
	p := NewNeedsYouPane()
	p.SetSize(20, 8) // narrow/short pane so the content overflows and scrolling matters
	info := &NeedsYouInfo{
		Item:           clarity.FeedItem{Rank: 1, Source: "#1", Title: "t"},
		Explanation:    strings.Repeat("word ", 40),
		Recommendation: "ok",
	}
	p.SetInfo(info)
	p.ScrollDown()
	p.ScrollDown()
	offsetAfterScroll := p.viewport.YOffset()
	require.Greater(t, offsetAfterScroll, 0, "test fixture must actually scroll")

	// Same row, refreshed data (e.g. a re-render tick with unchanged
	// content) - the scroll position must survive.
	p.SetInfo(info)
	require.Equal(t, offsetAfterScroll, p.viewport.YOffset())
}

func TestNeedsYouPane_ScrollResetsOnDifferentRow(t *testing.T) {
	p := NewNeedsYouPane()
	p.SetSize(20, 8)
	p.SetInfo(&NeedsYouInfo{
		Item:        clarity.FeedItem{Rank: 1, Source: "#1", Title: "t"},
		Explanation: strings.Repeat("word ", 40),
	})
	p.ScrollDown()
	p.ScrollDown()
	require.Greater(t, p.viewport.YOffset(), 0)

	p.SetInfo(&NeedsYouInfo{
		Item:        clarity.FeedItem{Rank: 2, Source: "#2", Title: "different row"},
		Explanation: strings.Repeat("word ", 40),
	})
	require.Equal(t, 0, p.viewport.YOffset(), "a genuinely different row starts scrolled to the top")
}

// TestNeedsYouPane_FitsAt120x36And164x45And200x55 is the FINISH
// requirement's own Needs-you case (the brief's "fit tests gain a
// Needs-you case").
func TestNeedsYouPane_FitsAt120x36And164x45And200x55(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{120, 36}, {164, 45}, {200, 55}} {
		t.Run(fmt.Sprintf("%dx%d", sz.w, sz.h), func(t *testing.T) {
			w := NewTabbedWindow(NewSessionPane(), NewNeedsYouPane(), NewTerminalPane())
			w.SetSize(sz.w, int(float32(sz.h)*0.9))
			w.SetActiveTab(NeedsYouTab)
			contentWidth, contentHeight := w.GetContentSize()
			require.Greater(t, contentWidth, 0)
			require.Greater(t, contentHeight, 0)

			w.SetNeedsYouInfo(&NeedsYouInfo{
				Item: clarity.FeedItem{
					Rank: 1, Class: "blocked-on-owner", Source: "#277", Lane: "#277",
					Title: "Owner: one settings act - move state-claim-warn to Stop and add the specialist boot line",
				},
				Explanation:    "Two edits in a settings file, described in plain words across several sentences that will need to wrap.",
				Recommendation: "Make both edits yourself, two minutes. Recommended.",
			})

			out := w.String()
			for i, line := range strings.Split(out, "\n") {
				require.LessOrEqualf(t, ansi.StringWidth(line), sz.w,
					"line %d exceeds terminal width %d: %q", i, sz.w, line)
			}
		})
	}
}
