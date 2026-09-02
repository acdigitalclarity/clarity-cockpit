package ui

import (
	"claude-squad/session/clarity"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// fixtureTail builds a LaneTail carrying the three turn kinds (owner,
// assistant, tool) the brief names explicitly, at the header/state values
// PANE-MOCKUP-164x45.md itself shows.
func fixtureTail() clarity.LaneTail {
	base := time.Date(2026, 9, 2, 18, 3, 25, 0, time.Local)
	lastWrite := time.Date(2026, 9, 2, 19, 4, 48, 0, time.Local)
	return clarity.LaneTail{
		Transcript:    "/Users/allencoates/.claude/projects/x/f095c45c-1234-5678.jsonl",
		State:         clarity.StateWorking,
		LastWrite:     lastWrite,
		LastTurn:      lastWrite,
		PendingAgents: 2,
		Model:         "claude-fable-5-1",
		Messages:      616,
		TurnDuration:  81 * time.Second,
		Turns: []clarity.Turn{
			{Kind: clarity.TurnOwner, At: base, Text: "the right pane should show the selected lane's session"},
			{Kind: clarity.TurnAssistant, At: base.Add(16 * time.Second), Text: "Understood: the right pane mirrors the selected lane's live session"},
			{Kind: clarity.TurnTool, At: base.Add(20 * time.Second), Tool: "Bash", Summary: "run the check", Result: clarity.ResultOK, Duration: 2100 * time.Millisecond},
		},
	}
}

// pinHome fixes $HOME for the duration of the test - shortenHome's own
// behaviour is $HOME-relative, and this fork's other tests already assume
// this specific machine's home directory (session/clarity/gauge.go's own
// DefaultClaudeProjectsRoot is hardcoded to it), so pinning it here keeps
// the test deterministic regardless of the sandbox's own ambient $HOME.
func pinHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", "/Users/allencoates")
}

func fixtureInfo() *SessionInfo {
	return &SessionInfo{
		Lane:    "ways-of-working",
		WorkDir: "/Users/allencoates/projects/Clarity/sessions/ways-of-working",
		Branch:  "main",
		Tail:    fixtureTail(),
		CtxPct:  20,
		CtxOK:   true,
		Now:     time.Date(2026, 9, 2, 19, 4, 48, 0, time.Local),
	}
}

func requireWithinWidth(t *testing.T, out string, width int) {
	t.Helper()
	for i, line := range strings.Split(out, "\n") {
		require.LessOrEqualf(t, ansi.StringWidth(line), width,
			"line %d exceeds pane width %d: %q", i, width, line)
	}
}

// TestSessionPane_RenderAt164x45_HeaderAndThreeTurnKinds is the brief's own
// named test: a Session view render at 164x45 from a fixture LaneTail with
// the three turn kinds, asserting the header lines, the tool line
// right-alignment and the width bound.
func TestSessionPane_RenderAt164x45_HeaderAndThreeTurnKinds(t *testing.T) {
	pinHome(t)
	s := NewSessionPane()
	// Wide enough that header line 2's full field set never needs
	// truncating - the width-bound and truncation behaviour itself is
	// covered separately by TestSessionPane_NeverExceedsPaneDimensions at
	// this app's real (narrower) pane widths.
	s.SetSize(160, 34)
	s.SetInfo(fixtureInfo())

	out := s.String()
	requireWithinWidth(t, out, 160)

	lines := strings.Split(out, "\n")
	require.Contains(t, lines[0], "ways-of-working", "header line 1 must carry the lane name")
	require.Contains(t, lines[0], "working", "header line 1 must carry the state word")
	require.Contains(t, lines[0], "2 agent", "header line 1 must carry the pending-agent count")
	require.Contains(t, lines[0], "ctx 20%", "header line 1 must carry the ctx percentage")
	require.Contains(t, lines[0], "▓▓░░░░░░░░", "header line 1's ctx bar must show 2 of 10 cells filled at 20%")
	require.Contains(t, lines[0], "last write 19:04:48", "header line 1 must carry the last-write time")

	require.Contains(t, lines[1], "~/projects/Clarity/sessions/ways-of-working", "header line 2 must carry the shortened workdir")
	require.Contains(t, lines[1], "main", "header line 2 must carry the branch")
	require.Contains(t, lines[1], "fable-5-1", "header line 2 must strip the claude- model prefix")
	require.Contains(t, lines[1], "1M window", "header line 2 must carry the derived context-window word")
	require.Contains(t, lines[1], "turn 1m 21s", "header line 2 must carry the turn duration")
	require.Contains(t, lines[1], "616 msgs", "header line 2 must carry the message count")
	require.Contains(t, lines[1], "session f095c45c", "header line 2 must carry the first 8 chars of the transcript stem")

	var toolLine string
	for _, l := range lines {
		if strings.Contains(l, "▪ Bash") {
			toolLine = l
			break
		}
	}
	require.NotEmpty(t, toolLine, "the tool turn must render its own line")
	require.Contains(t, toolLine, "run the check")
	plain := ansi.Strip(toolLine)
	require.True(t, strings.HasSuffix(strings.TrimRight(plain, " "), "exit 0     2.1s"),
		"the tool line's result/duration must be right-aligned at the line's own end, got %q", plain)
}

// TestSessionPane_RestingFrame_WhenNothingSelected is item 3's own test:
// with no SessionInfo set, the pane shows the splash's resting frame, never
// placeholder prose.
func TestSessionPane_RestingFrame_WhenNothingSelected(t *testing.T) {
	s := NewSessionPane()
	s.SetSize(115, 34)
	s.SetFleetCounts(5, 2)

	out := s.String()
	requireWithinWidth(t, out, 115)
	require.NotContains(t, out, "No agents running yet", "the Session tab must never show placeholder prose")
	require.NotContains(t, out, "waiting on you", "an unselected pane must not carry conversation state text")
}

// TestSessionPane_NewestTurnPinnedToBottom is the brief's own named
// behaviour: with more turns than the turns region can show, the LATEST
// turn is always visible and an early one has scrolled off.
func TestSessionPane_NewestTurnPinnedToBottom(t *testing.T) {
	base := time.Date(2026, 9, 2, 18, 0, 0, 0, time.Local)
	var turns []clarity.Turn
	for i := 0; i < 60; i++ {
		turns = append(turns, clarity.Turn{
			Kind: clarity.TurnOwner,
			At:   base.Add(time.Duration(i) * time.Minute),
			Text: fmt.Sprintf("turn number %d", i),
		})
	}

	info := fixtureInfo()
	info.Tail.Turns = turns

	s := NewSessionPane()
	s.SetSize(80, 20) // a small pane: the turns region cannot hold 60 turns
	s.SetInfo(info)

	out := s.String()
	require.Contains(t, out, "turn number 59", "the newest turn must always be visible")
	require.NotContains(t, out, "turn number 0", "an early turn must have scrolled off a too-small pane")
}

// TestSessionPane_ScrollUp_RevealsEarlierTurns proves shift+up's own effect
// end to end: scrolling up brings an earlier turn back into view.
func TestSessionPane_ScrollUp_RevealsEarlierTurns(t *testing.T) {
	base := time.Date(2026, 9, 2, 18, 0, 0, 0, time.Local)
	var turns []clarity.Turn
	for i := 0; i < 60; i++ {
		turns = append(turns, clarity.Turn{
			Kind: clarity.TurnOwner,
			At:   base.Add(time.Duration(i) * time.Minute),
			Text: fmt.Sprintf("turn number %d", i),
		})
	}
	info := fixtureInfo()
	info.Tail.Turns = turns

	s := NewSessionPane()
	s.SetSize(80, 20)
	s.SetInfo(info)
	require.NotContains(t, s.String(), "turn number 0")

	// 60 turns * 2 lines each = 120 content lines; the viewport's own max
	// offset off a 12-row turns area is 108 - comfortably inside 500.
	for i := 0; i < 500; i++ {
		s.ScrollUp()
	}
	require.Contains(t, s.String(), "turn number 0", "scrolling all the way up must reveal the earliest turn")
}

// TestSessionPane_EarlierDivider_ShownOnlyWhenTruncated is the header's own
// conditional row.
func TestSessionPane_EarlierDivider_ShownOnlyWhenTruncated(t *testing.T) {
	info := fixtureInfo()
	info.Tail.Truncated = true

	s := NewSessionPane()
	s.SetSize(115, 34)
	s.SetInfo(info)
	require.Contains(t, s.String(), "earlier in this session")

	info2 := fixtureInfo()
	info2.Tail.Truncated = false
	s.SetInfo(info2)
	require.NotContains(t, s.String(), "earlier in this session")
}

// TestSessionPane_HeaderLine1_LaneNameSurvivesNarrowPane reproduces a real
// defect caught in the PROOF capture at 120x36 (real pane content width 62):
// the full right-hand block (state + agent count + ctx bar + last write) is
// wider than 62 on its own, and padRow's plain left-truncation zeroed the
// lane name out entirely to make room. The header must drop its own least
// essential clauses (last write, then the agent count) before it ever
// sacrifices the lane name.
func TestSessionPane_HeaderLine1_LaneNameSurvivesNarrowPane(t *testing.T) {
	info := fixtureInfo()
	s := NewSessionPane()
	s.SetSize(62, 30)
	s.SetInfo(info)

	line1 := strings.Split(s.String(), "\n")[0]
	require.Contains(t, line1, "ways-of-working", "the lane name must survive even when the full right block does not fit")
	require.LessOrEqual(t, ansi.StringWidth(line1), 62)
}

// TestSessionPane_NeverExceedsPaneDimensions is the FINISH requirement at
// the three named sizes' own pane-content dimensions.
func TestSessionPane_NeverExceedsPaneDimensions(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{62, 26}, {87, 34}, {177, 46}} {
		t.Run(fmt.Sprintf("%dx%d", sz.w, sz.h), func(t *testing.T) {
			s := NewSessionPane()
			s.SetSize(sz.w, sz.h)
			s.SetInfo(fixtureInfo())
			out := s.String()
			lines := strings.Split(out, "\n")
			require.LessOrEqualf(t, len(lines), sz.h, "rendered %d lines, pane is only %d rows", len(lines), sz.h)
			requireWithinWidth(t, out, sz.w)
		})
	}
}
