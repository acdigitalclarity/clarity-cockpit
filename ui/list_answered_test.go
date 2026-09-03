package ui

import (
	"testing"
	"time"

	"claude-squad/session/clarity"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// TestString_AnsweredRow_ShowsAnsAbbreviation_NoCellCrossesNextColumn is
// item 5's own row-text proof (WAITING HELD, cockpit-pane modalities
// research), seen failing before laneRowSuffix carried answeredAt at all:
// at 164x45 (FRONTDOOR-MOCKUP-164x45.md's own width), a row answered 3
// minutes ago shows "ans 3m" in its time cell in place of the ordinary
// HH:MM, a row answered 40 minutes ago (past the 30-minute window) still
// shows the ordinary HH:MM - and every row, whichever branch fired, stays
// the SAME total rendered width (mirrors ui/overlay/newLane_test.go's own
// "every box line must be exactly N columns" / statusColumnStart pattern -
// there is no fixed field after the time cell here, so the row's own
// TOTAL width standing still across both branches is what proves no cell
// bled into the next one).
func TestString_AnsweredRow_ShowsAnsAbbreviation_NoCellCrossesNextColumn(t *testing.T) {
	l := newTestList()
	l.SetSize(frontdoor5ListWidth164, 45)

	now := time.Now()

	plain := frontdoor5Instance(t, "plain-lane", "ta", "", 10, clarity.StateWorking, frontdoor5Time(9, 0))
	l.AddInstance(plain)

	answered := frontdoor5Instance(t, "answered-lane", "tb", "", 20, clarity.StateIdle, frontdoor5Time(9, 1))
	// 3 minutes ago with a half-minute safety margin below the next whole
	// minute, so int(age.Minutes()) reads 3 regardless of test scheduling
	// jitter between this line and the render below.
	answered.SetAnsweredAt(now.Add(-3*time.Minute - 30*time.Second))
	l.AddInstance(answered)

	staleAnswered := frontdoor5Instance(t, "stale-lane", "m1", "", 30, clarity.StateIdle, frontdoor5Time(11, 26))
	// 40 minutes ago - past laneAnsweredCellMaxAge (30 min), so this row
	// must fall back to the ordinary last-turn HH:MM, exactly like plain-lane.
	staleAnswered.SetAnsweredAt(now.Add(-40 * time.Minute))
	l.AddInstance(staleAnswered)

	stripped := ansi.Strip(l.String())

	answeredLine := rowLineContaining(t, stripped, "2. answered-lane")
	require.Contains(t, answeredLine, "ans 3m", "a fresh (<30min) answered transition shows the row abbreviation")
	require.NotContains(t, answeredLine, "idle", "the ordinary state word is replaced, not just appended alongside")

	staleLine := rowLineContaining(t, stripped, "3. stale-lane")
	require.Contains(t, staleLine, "11:26", "an answered transition aged past 30 minutes falls back to the ordinary HH:MM")
	require.NotContains(t, staleLine, "ans ", "a stale AnsweredAt must never render the abbreviation")

	plainLine := rowLineContaining(t, stripped, "1. plain-lane")
	require.Contains(t, plainLine, "09:00", "a row with no AnsweredAt at all renders the ordinary HH:MM, unchanged")

	// No cell crosses the next column's x: every row's own TOTAL rendered
	// width is identical whichever branch fired for its own time cell.
	require.Equal(t, len([]rune(plainLine)), len([]rune(answeredLine)),
		"the answered row must render at the SAME total width as an ordinary row: %q vs %q", plainLine, answeredLine)
	require.Equal(t, len([]rune(plainLine)), len([]rune(staleLine)),
		"the stale-answered row must render at the SAME total width as an ordinary row: %q vs %q", plainLine, staleLine)
}

// TestLaneAnsweredCellText_Boundaries is laneAnsweredCellText's own unit
// proof: the floor at 1 minute, the exact 30-minute cutoff (>= is excluded,
// item 5's own words: "less than 30 minutes old"), and the zero-value
// no-transition case.
func TestLaneAnsweredCellText_Boundaries(t *testing.T) {
	now := time.Now()
	require.Equal(t, "", laneAnsweredCellText(time.Time{}), "zero AnsweredAt: no transition at all")
	require.Equal(t, "ans 1m", laneAnsweredCellText(now.Add(-10*time.Second)), "under a minute floors to 1, never 0")
	require.Equal(t, "ans 29m", laneAnsweredCellText(now.Add(-29*time.Minute)))
	require.Equal(t, "", laneAnsweredCellText(now.Add(-30*time.Minute)), "exactly 30 minutes is the cutoff, not included")
	require.Equal(t, "", laneAnsweredCellText(now.Add(-45*time.Minute)))
}
