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

// TestSessionPane_ScrollPosition_SurvivesInfoRefreshWhenNotAtBottom
// reproduces defect 3 (design/cockpit-pane/DECISIONS.md slice 3b): every
// SetInfo call used to re-pin the viewport to the bottom unconditionally, so
// a mid-scroll read - the exact thing shift+up exists for - was thrown away
// on the next 3-second feed tick. Scrolling away from the bottom, then
// feeding the pane a fresh SetInfo (as app.go's feed tick does every 3s),
// must leave the scrolled-to line offset exactly where it was.
func TestSessionPane_ScrollPosition_SurvivesInfoRefreshWhenNotAtBottom(t *testing.T) {
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

	for i := 0; i < 20; i++ {
		s.ScrollUp()
	}
	require.False(t, s.viewport.AtBottom(), "test setup must actually be scrolled away from the bottom")
	offsetBefore := s.viewport.YOffset()

	// Simulate the 3s feed tick: the same lane's info refreshes (message
	// count ticks up), turns unchanged.
	info2 := fixtureInfo()
	info2.Tail.Turns = turns
	info2.Tail.Messages = info.Tail.Messages + 1
	s.SetInfo(info2)

	require.Equal(t, offsetBefore, s.viewport.YOffset(),
		"a refresh while scrolled away from the bottom must keep the same line offset, clamped, never snap back to the bottom")
}

// TestSessionPane_AtBottom_StaysAtBottomAcrossInfoRefreshWithNewTurn is
// defect 3's other half: when the viewport WAS at the bottom, a refresh that
// appends a new turn (a live tick with fresh conversation) must still pin to
// the bottom, showing the newest turn.
func TestSessionPane_AtBottom_StaysAtBottomAcrossInfoRefreshWithNewTurn(t *testing.T) {
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
	require.True(t, s.viewport.AtBottom(), "test setup must start pinned to the bottom")

	turns2 := append(append([]clarity.Turn{}, turns...), clarity.Turn{
		Kind: clarity.TurnOwner, At: base.Add(60 * time.Minute), Text: "turn number 60",
	})
	info2 := fixtureInfo()
	info2.Tail.Turns = turns2
	s.SetInfo(info2)

	require.Contains(t, s.String(), "turn number 60", "still pinned to bottom, the newest turn appended by a live tick must be visible")
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

// TestSessionPane_HeaderLine2_TruncatesLeftNeverDropsBranchOrModel is
// defect 2's own rule for the Session pane's header line 2: on a pane too
// narrow for the whole "workdir · branch · model · window" left block
// alongside the right-hand msgs/session block, the LEFT-MOST field (the
// working directory - the least essential, since the lane name is already
// on line 1) is what gets truncated, never the branch or model that follow
// it in the same joined string. Before this fix, padRow's own
// keep-the-front/cut-the-tail truncation did the opposite: the long workdir
// path survived whole and everything after it - branch, model, window - was
// the part silently cut off.
func TestSessionPane_HeaderLine2_TruncatesLeftNeverDropsBranchOrModel(t *testing.T) {
	pinHome(t)
	info := fixtureInfo()
	info.WorkDir = "/Users/allencoates/projects/Clarity/sessions/a-very-long-lane-name-that-eats-the-whole-header-line"

	s := NewSessionPane()
	s.SetSize(90, 30)
	s.SetInfo(info)

	line2 := strings.Split(s.String(), "\n")[1]
	require.LessOrEqual(t, ansi.StringWidth(line2), 90)
	require.Contains(t, line2, "main", "header line 2 must never drop the branch to make room for the workdir")
	require.Contains(t, line2, "fable-5-1", "header line 2 must never drop the model to make room for the workdir")
	require.Contains(t, line2, "…", "an overflowing left block truncates with an ellipsis, not a silent cut")
}

// openTurnInfo returns a fixtureInfo with an OPEN working turn (the
// Latency ruling's own precondition for the header glyph to animate) and
// one still-RUNNING tool turn - the shape the Latency ruling's slice 12
// tests exercise: a lane whose transcript's last record is an unmatched
// tool_use, no tool_result yet.
func openTurnInfo(toolAt time.Time, animFrame int, now time.Time) *SessionInfo {
	info := fixtureInfo()
	info.Tail.State = clarity.StateWorking
	info.Tail.OpenTurn = true
	info.Tail.Turns = []clarity.Turn{
		{Kind: clarity.TurnOwner, At: toolAt.Add(-time.Minute), Text: "run the long build"},
		{Kind: clarity.TurnTool, At: toolAt, Tool: "Bash", Summary: "go build ./...", Result: clarity.ResultRunning},
	}
	info.AnimFrame = animFrame
	info.Now = now
	return info
}

// TestSessionPane_HeaderGlyph_AnimatesOnlyWhileTurnOpen is the Latency
// ruling's own header requirement, seen failing before this leg's fix (the
// header always drew laneStateGlyph's static "●" for StateWorking,
// AnimFrame or no): a different AnimFrame value must draw a DIFFERENT
// glyph out of animGlyphFrames while State is working and OpenTurn is true.
func TestSessionPane_HeaderGlyph_AnimatesOnlyWhileTurnOpen(t *testing.T) {
	pinHome(t)
	base := time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)

	s := NewSessionPane()
	s.SetSize(160, 34)
	s.SetInfo(openTurnInfo(base, 0, base))
	line0 := strings.Split(s.String(), "\n")[0]

	s.SetInfo(openTurnInfo(base, 1, base.Add(500*time.Millisecond)))
	line1 := strings.Split(s.String(), "\n")[0]

	require.NotEqual(t, line0, line1, "the header's own glyph column must advance between two session ticks while the turn is open")
	require.Contains(t, line1, animGlyphFrames[1], "AnimFrame 1 must draw animGlyphFrames' own second frame")
}

// TestSessionPane_HeaderGlyph_SettlesWhenTurnCloses is the ruling's other
// half: the instant OpenTurn goes false (ClassifyState's own "turn closed"
// case), the header glyph must stop cycling and settle to laneStateGlyph's
// plain static glyph for that state - regardless of what AnimFrame now is.
func TestSessionPane_HeaderGlyph_SettlesWhenTurnCloses(t *testing.T) {
	pinHome(t)
	base := time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)

	s := NewSessionPane()
	s.SetSize(160, 34)
	s.SetInfo(openTurnInfo(base, 3, base))
	openLine := strings.Split(s.String(), "\n")[0]

	closed := openTurnInfo(base, 4, base.Add(time.Second))
	closed.Tail.OpenTurn = false
	closed.Tail.Turns[1].Result = clarity.ResultOK
	closed.Tail.Turns[1].Duration = time.Second
	s.SetInfo(closed)
	closedLine := strings.Split(s.String(), "\n")[0]

	staticGlyph, _ := laneStateGlyph(clarity.StateWorking)
	require.Contains(t, closedLine, staticGlyph, "a closed turn must settle to the plain static glyph, never a cycled frame")
	require.NotEqual(t, openLine, closedLine)
}

// TestSessionPane_RunningToolLine_ElapsedAdvancesBetweenTicks is the
// Latency ruling's tool-line requirement, seen failing before this leg's
// fix (toolResultLabel showed a bare "running" forever - Duration is only
// ever set from a matched tool_result, never for a still-open call): two
// SetInfo calls a real elapsed second apart must show a strictly LARGER
// elapsed figure on the unmatched tool's own line, counting up from its own
// timestamp, not the lane's last stat.
func TestSessionPane_RunningToolLine_ElapsedAdvancesBetweenTicks(t *testing.T) {
	pinHome(t)
	base := time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)

	s := NewSessionPane()
	s.SetSize(160, 34)

	s.SetInfo(openTurnInfo(base, 0, base.Add(300*time.Millisecond)))
	early := toolLine(t, s.String())
	require.Contains(t, early, "running", "an unmatched tool_use must show running, not a bare exit/error label")

	s.SetInfo(openTurnInfo(base, 1, base.Add(800*time.Millisecond)))
	mid := toolLine(t, s.String())
	require.NotEqual(t, early, mid, "the elapsed text must change tick to tick while the tool is still running")

	s.SetInfo(openTurnInfo(base, 2, base.Add(1800*time.Millisecond)))
	later := toolLine(t, s.String())
	require.NotEqual(t, mid, later, "elapsed must keep advancing a further second later")
}

// toolLine returns the rendered pane's own "▪ Bash" line, failing the test
// if none is present.
func toolLine(t *testing.T, out string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "▪ Bash") {
			return l
		}
	}
	t.Fatalf("no tool line found in:\n%s", out)
	return ""
}

// TestSessionPane_SetInfo_SkipsRebuildWhenSignatureUnchanged is the FINISH
// requirement's own "no flicker" clause: an idle lane whose file has not
// changed (LaneTailCache serves the identical LaneTail back every tick, the
// cache's own contract) must render the exact same turns content on a
// second SetInfo call - never a spurious diff a terminal renderer would
// have to repaint for no visible reason.
func TestSessionPane_SetInfo_SkipsRebuildWhenSignatureUnchanged(t *testing.T) {
	pinHome(t)
	s := NewSessionPane()
	s.SetSize(160, 34)

	info1 := fixtureInfo()
	s.SetInfo(info1)
	out1 := s.String()

	// A second, distinct SessionInfo value carrying byte-identical Tail
	// content (as two ticks reading the same unchanged file through the
	// cache would) and a slightly later Now within the same rendered
	// second - the idle-lane case, no running tool to advance.
	info2 := fixtureInfo()
	info2.Now = info1.Now.Add(500 * time.Millisecond)
	s.SetInfo(info2)
	out2 := s.String()

	require.Equal(t, out1, out2, "an unchanged lane must render byte-identical output across ticks")
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
