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
		Item:        clarity.FeedItem{Rank: 1, Class: "blocked-on-owner", Source: "#277", Lane: "#277", Title: "Owner: one settings act"},
		Lane:        "ways-of-working",
		Explanation: []clarity.BoardSection{{Text: "the plain-words explanation"}},
		Options:     []clarity.BoardOption{{Text: "do the thing", Recommended: true}},
	})

	out := ansi.Strip(p.String())
	require.Contains(t, out, "1. Owner: one settings act", "line 1: row number and title")
	require.Contains(t, out, "ways-of-working · blocked-on-owner", "line 2: the RESOLVED raising lane and its priority, never the raw \"#277\"")
	require.Contains(t, out, "the plain-words explanation")
	require.Contains(t, out, "Recommended response:")
	require.Contains(t, out, "do the thing")
}

// TestNeedsYouPane_UnresolvedLane_HeaderShowsNoLaneLabel is board #280's
// slice 5b DEFECT 2 half seen on the tab's own header line, not just the
// composer: a board row whose Lane never resolved never falls back to the
// raw issue-number source string either.
func TestNeedsYouPane_UnresolvedLane_HeaderShowsNoLaneLabel(t *testing.T) {
	p := NewNeedsYouPane()
	p.SetSize(80, 20)
	p.SetInfo(&NeedsYouInfo{
		Item: clarity.FeedItem{Rank: 1, Class: "blocked-on-owner", Source: "#277", Lane: "#277", Title: "t"},
	})

	out := ansi.Strip(p.String())
	require.Contains(t, out, NoLaneLabel+" · blocked-on-owner")
	require.NotContains(t, out, "#277")
}

// TestNeedsYouPane_ExplanationLabelsWhatWhereWhy is board #280's slice 5b
// DEFECT 1: the What/Where/Why sections render as small plain-word labels
// over their own wrapped text, and a marked option is visibly distinct
// from an unmarked one.
func TestNeedsYouPane_ExplanationLabelsWhatWhereWhy(t *testing.T) {
	p := NewNeedsYouPane()
	p.SetSize(80, 30)
	p.SetInfo(&NeedsYouInfo{
		Item: clarity.FeedItem{Rank: 1, Class: "blocked-on-owner", Title: "t"},
		Lane: "ways-of-working",
		Explanation: []clarity.BoardSection{
			{Label: "What", Text: "do the thing"},
			{Label: "Where", Text: "over there"},
			{Label: "Why", Text: "because reasons"},
		},
		Options: []clarity.BoardOption{
			{Text: "option a", Recommended: true},
			{Text: "option b", Recommended: false},
		},
		ExpectedReply: "a yes or no",
	})

	out := ansi.Strip(p.String())
	require.Contains(t, out, "What")
	require.Contains(t, out, "do the thing")
	require.Contains(t, out, "Where")
	require.Contains(t, out, "over there")
	require.Contains(t, out, "Why")
	require.Contains(t, out, "because reasons")
	require.Contains(t, out, optionMarker+"option a", "the recommended option is marked")
	require.Contains(t, out, optionIndent+"option b", "an unmarked option lines up under the marked one")
	require.Contains(t, out, "Expected reply:")
	require.Contains(t, out, "a yes or no")
}

// TestNeedsYouPane_AlsoOnTheRow_NeverDropsUnclassifiedText is the other
// half of DEFECT 1's rule: text the parser could not classify is never
// silently dropped.
func TestNeedsYouPane_AlsoOnTheRow_NeverDropsUnclassifiedText(t *testing.T) {
	p := NewNeedsYouPane()
	p.SetSize(80, 20)
	p.SetInfo(&NeedsYouInfo{
		Item: clarity.FeedItem{Rank: 1, Class: "blocked-on-owner", Title: "t"},
		Lane: "ways-of-working",
		Also: "Notes: a stray note the card carries",
	})

	out := ansi.Strip(p.String())
	require.Contains(t, out, "Also on the row:")
	require.Contains(t, out, "a stray note the card carries")
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
		Item:        clarity.FeedItem{Rank: 1, Title: "t", Lane: "#1", Class: "blocked-on-owner"},
		Lane:        "lane-a",
		Explanation: []clarity.BoardSection{{Text: strings.Repeat("word ", 30)}},
		Options:     []clarity.BoardOption{{Text: "ok"}},
	})

	for i, line := range strings.Split(ansi.Strip(p.String()), "\n") {
		require.LessOrEqualf(t, ansi.StringWidth(line), 20, "line %d: %q", i, line)
	}
}

func TestNeedsYouPane_ClearShowsNothingSelectedAgain(t *testing.T) {
	p := NewNeedsYouPane()
	p.SetSize(80, 20)
	p.SetInfo(&NeedsYouInfo{Item: clarity.FeedItem{Rank: 1, Title: "t"}, Options: []clarity.BoardOption{{Text: "r"}}})
	p.Clear()

	require.Contains(t, p.String(), "select a Needs-you row")
}

func TestNeedsYouPane_ScrollPreservedAcrossSameRowRefresh(t *testing.T) {
	p := NewNeedsYouPane()
	p.SetSize(20, 8) // narrow/short pane so the content overflows and scrolling matters
	info := &NeedsYouInfo{
		Item:        clarity.FeedItem{Rank: 1, Source: "#1", Title: "t"},
		Lane:        "lane-a",
		Explanation: []clarity.BoardSection{{Text: strings.Repeat("word ", 40)}},
		Options:     []clarity.BoardOption{{Text: "ok"}},
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
		Explanation: []clarity.BoardSection{{Text: strings.Repeat("word ", 40)}},
	})
	p.ScrollDown()
	p.ScrollDown()
	require.Greater(t, p.viewport.YOffset(), 0)

	p.SetInfo(&NeedsYouInfo{
		Item:        clarity.FeedItem{Rank: 2, Source: "#2", Title: "different row"},
		Explanation: []clarity.BoardSection{{Text: strings.Repeat("word ", 40)}},
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
				Lane: "ways-of-working",
				Explanation: []clarity.BoardSection{
					{Label: "What", Text: "Two edits in a settings file, described in plain words across several sentences that will need to wrap."},
				},
				Options:       []clarity.BoardOption{{Text: "Make both edits yourself, two minutes.", Recommended: true}},
				ExpectedReply: "\"done\" on this row, or \"apply it\".",
			})

			out := w.String()
			for i, line := range strings.Split(out, "\n") {
				require.LessOrEqualf(t, ansi.StringWidth(line), sz.w,
					"line %d exceeds terminal width %d: %q", i, sz.w, line)
			}
		})
	}
}
