// Package ui: items 4 and 5 of COCKPIT-MODALITIES-2026-09-03.md (cockpit
// pane slice 17) - the Session tab's "since you were away" line and its
// "answered N min ago" state-line clause. Kept in its own file, the same
// convention list_frontdoor5_test.go/list_row_band_test.go already follow
// for a slice's own test set within a shared package.
package ui

import (
	"claude-squad/session/clarity"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// --- item 4: since you were away ------------------------------------------

// awayInfo builds a fixtureInfo carrying an away_summary at summaryAt, the
// pane viewed at now.
func awayInfo(summaryAt, now time.Time, text string) *SessionInfo {
	info := fixtureInfo()
	info.Tail.AwaySummary = clarity.AwaySummary{Text: text, At: summaryAt}
	info.Now = now
	return info
}

// awaySummaryLineText returns the rendered "since you were away" line (never
// stripped of ANSI, since one test below asserts on the accent/muted split),
// or "" if none is present.
func awaySummaryLineText(out string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(ansi.Strip(l), "since you were away") {
			return l
		}
	}
	return ""
}

// TestSessionPane_AwaySummary_ShowsOnFirstView is item 4's own core case: a
// lane never looked at before (no awaySeenThrough entry) with a fresh
// away_summary shows the line on the very first render.
func TestSessionPane_AwaySummary_ShowsOnFirstView(t *testing.T) {
	pinHome(t)
	now := time.Date(2026, 9, 3, 9, 0, 0, 0, time.Local)
	s := NewSessionPane()
	s.SetSize(160, 34)
	s.SetInfo(awayInfo(now.Add(-time.Hour), now, "Goal: ship slice 17. Next: proof."))

	out := s.String()
	line := awaySummaryLineText(out)
	require.NotEmpty(t, line, "a fresh away_summary must show on first view")
	require.Contains(t, ansi.Strip(line), "Goal: ship slice 17. Next: proof.")
}

// TestSessionPane_AwaySummary_AbsentWhenNoSummary proves the line is never
// drawn (and never steals a row from fixedTopRows) when the tail carries no
// away_summary at all - fixtureInfo()'s own default.
func TestSessionPane_AwaySummary_AbsentWhenNoSummary(t *testing.T) {
	pinHome(t)
	s := NewSessionPane()
	s.SetSize(160, 34)
	s.SetInfo(fixtureInfo())

	out := s.String()
	require.Empty(t, awaySummaryLineText(out))
}

// TestSessionPane_AwaySummary_DisappearsAfterThreeContinuousSeconds is the
// brief's own "it disappears once the owner views the lane for more than
// three seconds": the same lane, rendered again after the 3-second mark,
// must no longer show the line - on that SAME render, not one tick later.
func TestSessionPane_AwaySummary_DisappearsAfterThreeContinuousSeconds(t *testing.T) {
	pinHome(t)
	base := time.Date(2026, 9, 3, 9, 0, 0, 0, time.Local)
	s := NewSessionPane()
	s.SetSize(160, 34)

	info := awayInfo(base.Add(-time.Hour), base, "Goal: ship slice 17. Next: proof.")
	s.SetInfo(info)
	require.NotEmpty(t, awaySummaryLineText(s.String()), "must still show under 3s in")

	// Still the same lane, 2s later - under the threshold, still showing.
	info2 := awayInfo(base.Add(-time.Hour), base.Add(2*time.Second), "Goal: ship slice 17. Next: proof.")
	s.SetInfo(info2)
	require.NotEmpty(t, awaySummaryLineText(s.String()), "must still show at 2s")

	// 4s in - past the threshold: this render itself must hide it.
	info3 := awayInfo(base.Add(-time.Hour), base.Add(4*time.Second), "Goal: ship slice 17. Next: proof.")
	s.SetInfo(info3)
	require.Empty(t, awaySummaryLineText(s.String()), "must disappear once viewed over 3 continuous seconds")
}

// TestSessionPane_AwaySummary_LaneSwitchRestartsTheClock proves the 3-second
// clock is CONTINUOUS viewing of the same lane, never accumulated wall
// time: switching away and back to lane-a restarts it, so a summary that
// was one second from being marked seen shows again in full.
func TestSessionPane_AwaySummary_LaneSwitchRestartsTheClock(t *testing.T) {
	pinHome(t)
	base := time.Date(2026, 9, 3, 9, 0, 0, 0, time.Local)
	s := NewSessionPane()
	s.SetSize(160, 34)

	laneA := awayInfo(base.Add(-time.Hour), base, "Goal: lane-a work. Next: ship it.")
	laneA.Lane = "lane-a"
	s.SetInfo(laneA)
	require.NotEmpty(t, awaySummaryLineText(s.String()))

	laneANearly := awayInfo(base.Add(-time.Hour), base.Add(2900*time.Millisecond), "Goal: lane-a work. Next: ship it.")
	laneANearly.Lane = "lane-a"
	s.SetInfo(laneANearly)
	require.NotEmpty(t, awaySummaryLineText(s.String()), "still under 3s on lane-a")

	laneB := fixtureInfo()
	laneB.Lane = "lane-b"
	laneB.Now = base.Add(2950 * time.Millisecond)
	s.SetInfo(laneB)
	_ = s.String()

	backToA := awayInfo(base.Add(-time.Hour), base.Add(3*time.Second), "Goal: lane-a work. Next: ship it.")
	backToA.Lane = "lane-a"
	s.SetInfo(backToA)
	require.NotEmpty(t, awaySummaryLineText(s.String()), "the clock must restart on the lane switch, not carry over")
}

// TestSessionPane_AwaySummary_TruncatesToPaneWidth proves a long summary
// never pushes the rendered line past the pane's own width - the OVERFLOW
// convention every other row-truncating helper in this file already follows
// (ansiTruncateRow).
func TestSessionPane_AwaySummary_TruncatesToPaneWidth(t *testing.T) {
	pinHome(t)
	now := time.Date(2026, 9, 3, 9, 0, 0, 0, time.Local)
	long := strings.Repeat("a very long recap sentence that keeps going ", 10)
	s := NewSessionPane()
	s.SetSize(80, 34)
	s.SetInfo(awayInfo(now.Add(-time.Hour), now, long))

	requireWithinWidth(t, s.String(), 80)
}

// --- item 5: answered N min ago -------------------------------------------

// answeredInfo builds a fixtureInfo whose Tail.State/AnsweredAt reflect
// item 5's own post-transition read.
func answeredInfo(state string, answeredAt, now time.Time) *SessionInfo {
	info := fixtureInfo()
	info.Tail.State = state
	info.Tail.AnsweredAt = answeredAt
	info.Tail.PendingAgents = 0
	info.Now = now
	return info
}

// TestSessionPane_StateLine_AnsweredUnderThirtyMinutes is item 5's own named
// display rule: a transition under 30 minutes old reads "answered N min
// ago" in place of the usual "turn closed hh:mm:ss" clause.
func TestSessionPane_StateLine_AnsweredUnderThirtyMinutes(t *testing.T) {
	pinHome(t)
	now := time.Date(2026, 9, 3, 9, 0, 0, 0, time.Local)
	s := NewSessionPane()
	s.SetSize(160, 34)
	s.SetInfo(answeredInfo(clarity.StateIdle, now.Add(-5*time.Minute), now))

	line := ansi.Strip(s.renderStateLine())
	require.Contains(t, line, "answered 5 min ago")
	require.NotContains(t, line, "turn closed", "the answered clause replaces the usual one, never both")
}

// TestSessionPane_StateLine_AnsweredOverThirtyMinutes_FallsBackToOrdinaryClause
// proves the "answered" copy is a transient acknowledgment window, not a
// permanent relabeling of idle: past 30 minutes the state line reads exactly
// as any other idle lane's does.
func TestSessionPane_StateLine_AnsweredOverThirtyMinutes_FallsBackToOrdinaryClause(t *testing.T) {
	pinHome(t)
	now := time.Date(2026, 9, 3, 9, 0, 0, 0, time.Local)
	s := NewSessionPane()
	s.SetSize(160, 34)
	s.SetInfo(answeredInfo(clarity.StateIdle, now.Add(-40*time.Minute), now))

	line := ansi.Strip(s.renderStateLine())
	require.NotContains(t, line, "answered")
	require.Contains(t, line, "turn closed")
}

// TestSessionPane_StateLine_NoAnsweredAt_OrdinaryIdleUnchanged is a
// regression guard: a plain idle lane with no AnsweredAt at all (the
// pre-item-5 shape) must render exactly as before - the zero value never
// accidentally satisfies the "answered" branch.
func TestSessionPane_StateLine_NoAnsweredAt_OrdinaryIdleUnchanged(t *testing.T) {
	pinHome(t)
	now := time.Date(2026, 9, 3, 9, 0, 0, 0, time.Local)
	s := NewSessionPane()
	s.SetSize(160, 34)
	info := fixtureInfo()
	info.Tail.State = clarity.StateIdle
	info.Tail.PendingAgents = 0
	info.Now = now
	s.SetInfo(info)

	line := ansi.Strip(s.renderStateLine())
	require.NotContains(t, line, "answered")
	require.Contains(t, line, "nothing waiting on you")
}
