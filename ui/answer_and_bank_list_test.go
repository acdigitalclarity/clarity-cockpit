// Package ui: slice 18's own list-row tests - the permission-prompt state
// word overriding a tracked row's glyph/word, and the answered-marker tick
// and dim on a Needs-you row (ANSWER-AND-BANK-SPEC.md).
package ui

import (
	"claude-squad/session/clarity"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestLaneRow_NeedsKey_OverridesWorkingWordWithNeedsAKey(t *testing.T) {
	l := newTestList("cockpit")
	l.SetSize(120, 20)
	l.items[0].SetLaneState(clarity.StateWorking, time.Now(), true)
	l.items[0].SetNeedsKey(true)

	out := ansi.Strip(l.String())
	require.Contains(t, out, "needs a key")
	require.NotContains(t, out, " working ", "the transcript word must be REPLACED, not shown alongside it")
}

func TestLaneRow_NoNeedsKey_ShowsOrdinaryWord(t *testing.T) {
	l := newTestList("cockpit")
	l.SetSize(120, 20)
	l.items[0].SetLaneState(clarity.StateWorking, time.Now(), true)

	out := ansi.Strip(l.String())
	require.NotContains(t, out, "needs a key")
}

// needsYouFixtureItem mirrors a real board-sourced feed row's own shape
// (fleet_queue_build.py's lane_rows(): Lane carries the raw "#<n>" issue
// reference for a board row, never the resolved raising lane - see app.go's
// needsYouRowLane) - the same shape TestComposerTarget_NeedsYouRow_...
// fixtures already use.
func needsYouFixtureItem(source string) clarity.FeedItem {
	return clarity.FeedItem{Rank: 1, Source: source, Lane: source, Title: "Owner: one settings act"}
}

func TestNeedsYouRow_Answered_TickAndDim(t *testing.T) {
	l := newTestList()
	l.SetSize(60, 20)
	l.SetNeedsYou([]clarity.FeedItem{needsYouFixtureItem("#277")}, "")
	l.SetAnsweredIssues(map[int]bool{277: true})

	out := ansi.Strip(l.String())
	require.Contains(t, out, "✓ #277")
}

func TestNeedsYouRow_NotAnswered_NoTick(t *testing.T) {
	l := newTestList()
	l.SetSize(60, 20)
	l.SetNeedsYou([]clarity.FeedItem{needsYouFixtureItem("#277")}, "")

	out := ansi.Strip(l.String())
	require.NotContains(t, out, "✓")
}

func TestNeedsYouRow_LaneFileSourced_NeverAnswered(t *testing.T) {
	l := newTestList()
	l.SetSize(60, 20)
	item := clarity.FeedItem{Rank: 1, Source: "sessions/lane-a/STATUS.md:3", Lane: "lane-a", Title: "t"}
	l.SetNeedsYou([]clarity.FeedItem{item}, "")
	l.SetAnsweredIssues(map[int]bool{277: true}) // unrelated issue in the set

	out := ansi.Strip(l.String())
	require.NotContains(t, out, "✓")
}
