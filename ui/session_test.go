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
func openTurnInfo(toolAt time.Time, now time.Time) *SessionInfo {
	info := fixtureInfo()
	info.Tail.State = clarity.StateWorking
	info.Tail.OpenTurn = true
	info.Tail.Turns = []clarity.Turn{
		{Kind: clarity.TurnOwner, At: toolAt.Add(-time.Minute), Text: "run the long build"},
		{Kind: clarity.TurnTool, At: toolAt, Tool: "Bash", Summary: "go build ./...", Result: clarity.ResultRunning},
	}
	info.Now = now
	return info
}

// TestSessionPane_HeaderGlyph_AnimatesOnlyWhileTurnOpen is slice 14 rule 1's
// own header requirement, seen failing before this leg's fix (the header's
// glyph only ever advanced on SetInfo, i.e. the 500ms session tick - never
// on the pane's own 100ms TickSpinner): a TickSpinner call must draw a
// DIFFERENT glyph out of spinnerFrames while State is working and OpenTurn
// is true, with NO new SetInfo call at all - proving the animation is
// decoupled from the read.
func TestSessionPane_HeaderGlyph_AnimatesOnlyWhileTurnOpen(t *testing.T) {
	pinHome(t)
	base := time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)

	s := NewSessionPane()
	s.SetSize(160, 34)
	s.SetInfo(openTurnInfo(base, base))
	line0 := strings.Split(s.String(), "\n")[0]

	s.TickSpinner()
	line1 := strings.Split(s.String(), "\n")[0]

	require.NotEqual(t, line0, line1, "the header's own glyph column must advance on TickSpinner while the turn is open")
	require.Contains(t, line1, spinnerFrames[1], "one TickSpinner call must draw spinnerFrames' own second frame")
}

// TestSessionPane_SpinnerAdvancesOnlyOnTickSpinner_NeverOnSetInfo is rule
// 1's other half: two SetInfo calls carrying byte-identical data (an idle
// session tick re-reading an unchanged file) must NOT move the spinner on
// their own - only TickSpinner does.
func TestSessionPane_SpinnerAdvancesOnlyOnTickSpinner_NeverOnSetInfo(t *testing.T) {
	pinHome(t)
	base := time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)

	s := NewSessionPane()
	s.SetSize(160, 34)
	s.SetInfo(openTurnInfo(base, base))
	line0 := strings.Split(s.String(), "\n")[0]

	s.SetInfo(openTurnInfo(base, base)) // a second, identical SetInfo - no TickSpinner in between
	line1 := strings.Split(s.String(), "\n")[0]

	require.Equal(t, line0, line1, "a repeated SetInfo call must never advance the spinner on its own")
}

// TestSessionPane_HeaderGlyph_SettlesWhenTurnCloses is the ruling's other
// half: the instant OpenTurn goes false (ClassifyState's own "turn closed"
// case), the header glyph must stop cycling and settle to laneStateGlyph's
// plain static glyph for that state - regardless of how many times
// TickSpinner has fired.
func TestSessionPane_HeaderGlyph_SettlesWhenTurnCloses(t *testing.T) {
	pinHome(t)
	base := time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)

	s := NewSessionPane()
	s.SetSize(160, 34)
	s.SetInfo(openTurnInfo(base, base))
	s.TickSpinner()
	s.TickSpinner()
	s.TickSpinner()
	openLine := strings.Split(s.String(), "\n")[0]

	closed := openTurnInfo(base, base.Add(time.Second))
	closed.Tail.OpenTurn = false
	closed.Tail.Turns[1].Result = clarity.ResultOK
	closed.Tail.Turns[1].Duration = time.Second
	s.SetInfo(closed)
	s.TickSpinner()
	closedLine := strings.Split(s.String(), "\n")[0]

	staticGlyph, _ := laneStateGlyph(clarity.StateWorking)
	require.Contains(t, closedLine, staticGlyph, "a closed turn must settle to the plain static glyph, never a cycled frame")
	require.NotEqual(t, openLine, closedLine)
}

// thinkingLine returns the rendered pane's own "thinking ·" foot line,
// failing the test if none is present.
func thinkingLine(t *testing.T, out string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "thinking ·") {
			return l
		}
	}
	t.Fatalf("no thinking line found in:\n%s", out)
	return ""
}

// openTurnNoToolInfo is openTurnInfo's own sibling for rule 2's test: an
// OPEN working turn whose last record is assistant TEXT, not a tool_use -
// the "model is between records" shape the thinking line is for.
func openTurnNoToolInfo(lastAt, now time.Time) *SessionInfo {
	info := fixtureInfo()
	info.Tail.State = clarity.StateWorking
	info.Tail.OpenTurn = true
	info.Tail.LastTurn = lastAt
	info.Tail.Turns = []clarity.Turn{
		{Kind: clarity.TurnOwner, At: lastAt.Add(-time.Minute), Text: "run the long build"},
		{Kind: clarity.TurnAssistant, At: lastAt, Text: "still thinking about this one"},
	}
	info.Now = now
	return info
}

// TestSessionPane_ThinkingLine_VisibleBetweenRecordsWhileOpen is rule 2's
// own core case, seen failing before this leg's fix (no thinking line
// existed at all): an open turn with no tool running shows "thinking ·
// <elapsed>" at the foot of the turns, elapsed measured from the last
// timestamped record.
func TestSessionPane_ThinkingLine_VisibleBetweenRecordsWhileOpen(t *testing.T) {
	pinHome(t)
	base := time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)

	s := NewSessionPane()
	s.SetSize(160, 34)
	s.SetInfo(openTurnNoToolInfo(base, base.Add(12*time.Second)))

	require.Contains(t, s.String(), "thinking · 12s",
		"the thinking line must show the spinner and the elapsed time since the last record")
}

// TestSessionPane_ThinkingLine_HiddenWhileToolRunning is rule 2's other
// named case: a running tool line is already the indicator, so the
// thinking line must NOT also show alongside it.
func TestSessionPane_ThinkingLine_HiddenWhileToolRunning(t *testing.T) {
	pinHome(t)
	base := time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)

	s := NewSessionPane()
	s.SetSize(160, 34)
	s.SetInfo(openTurnInfo(base, base.Add(5*time.Second)))

	require.NotContains(t, s.String(), "thinking ·",
		"a running tool line is already the indicator; the thinking line must not also show")
}

// TestSessionPane_ThinkingLine_HiddenWhenTurnClosed is rule 2's third named
// case: the instant the turn closes, the thinking line disappears exactly
// as the header glyph settles.
func TestSessionPane_ThinkingLine_HiddenWhenTurnClosed(t *testing.T) {
	pinHome(t)
	base := time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)

	s := NewSessionPane()
	s.SetSize(160, 34)
	info := openTurnNoToolInfo(base, base.Add(12*time.Second))
	info.Tail.OpenTurn = false
	s.SetInfo(info)

	require.NotContains(t, s.String(), "thinking ·")
}

// TestSessionPane_ThinkingLine_DisappearsWhenToolLands is rule 2's own
// "disappears the moment a new record lands": an open turn between records
// shows it; the SAME lane a moment later with a tool_use now in flight must
// no longer show it (the tool line takes over).
func TestSessionPane_ThinkingLine_DisappearsWhenToolLands(t *testing.T) {
	pinHome(t)
	base := time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)

	s := NewSessionPane()
	s.SetSize(160, 34)
	s.SetInfo(openTurnNoToolInfo(base, base.Add(12*time.Second)))
	require.Contains(t, s.String(), "thinking ·")

	s.SetInfo(openTurnInfo(base.Add(13*time.Second), base.Add(14*time.Second)))
	require.NotContains(t, s.String(), "thinking ·")
}

// TestSessionPane_ThinkingLine_SpinnerAdvancesOnTickSpinner is rule 1
// applied to the thinking line's own leading glyph, not just the header:
// TickSpinner alone (no SetInfo) must change it.
func TestSessionPane_ThinkingLine_SpinnerAdvancesOnTickSpinner(t *testing.T) {
	pinHome(t)
	base := time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local)

	s := NewSessionPane()
	s.SetSize(160, 34)
	s.SetInfo(openTurnNoToolInfo(base, base.Add(12*time.Second)))
	line0 := thinkingLine(t, s.String())

	s.TickSpinner()
	line1 := thinkingLine(t, s.String())

	require.NotEqual(t, line0, line1, "TickSpinner must advance the thinking line's own spinner glyph")
}

// TestSessionPane_TickSpinner_NeverDisturbsScrollPosition is rule 4's own
// "the spinner frame alone does not rebuild the turns": scrolling away from
// the bottom and then calling only TickSpinner (never SetInfo again) must
// leave the scroll position exactly where it was - a content rebuild would
// reset it via refreshViewport's own wasAtBottom logic, exactly the
// flicker this rule forbids.
func TestSessionPane_TickSpinner_NeverDisturbsScrollPosition(t *testing.T) {
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
	offsetBefore := s.viewport.YOffset()

	s.TickSpinner()
	_ = s.String() // the same render call path a 100ms tick drives

	require.Equal(t, offsetBefore, s.viewport.YOffset(),
		"TickSpinner alone must never move the scroll position - that only happens on a genuine content rebuild")
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

	s.SetInfo(openTurnInfo(base, base.Add(300*time.Millisecond)))
	early := toolLine(t, s.String())
	require.Contains(t, early, "running", "an unmatched tool_use must show running, not a bare exit/error label")

	s.SetInfo(openTurnInfo(base, base.Add(800*time.Millisecond)))
	mid := toolLine(t, s.String())
	require.NotEqual(t, early, mid, "the elapsed text must change tick to tick while the tool is still running")

	s.SetInfo(openTurnInfo(base, base.Add(1800*time.Millisecond)))
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

// -- slice 13: the reading layout ----------------------------------------

// TestSplitProseBlocks_ParagraphsAndListItems is rule 1's own structural
// test: a blank source line ends a paragraph, a list-marker line always
// starts a new block even without one, and any other non-blank line joins
// the CURRENT block - the defect this replaces (collapseWS) folded all of
// this into one wrappable line.
func TestSplitProseBlocks_ParagraphsAndListItems(t *testing.T) {
	text := "First paragraph line one\nstill first paragraph.\n\n" +
		"Second paragraph.\n\n" +
		"- item one\n- item two spans\nmultiple lines\n" +
		"1. ordered one\n2. ordered two"
	blocks := splitProseBlocks(text)
	require.Len(t, blocks, 6)
	require.Equal(t, proseBlock{marker: "", text: "First paragraph line one still first paragraph."}, blocks[0])
	require.Equal(t, proseBlock{marker: "", text: "Second paragraph."}, blocks[1])
	require.Equal(t, proseBlock{marker: "- ", text: "item one"}, blocks[2])
	require.Equal(t, proseBlock{marker: "- ", text: "item two spans multiple lines"}, blocks[3])
	require.Equal(t, proseBlock{marker: "1. ", text: "ordered one"}, blocks[4])
	require.Equal(t, proseBlock{marker: "2. ", text: "ordered two"}, blocks[5])
}

// TestTokenizeMarkdown_StripsMarkersAndMarksBold is rule 1's other named
// test: "**bold**" is stripped and its words marked bold; "__" and
// backtick markers are stripped with no style change - never a literal
// marker byte surviving into a token.
func TestTokenizeMarkdown_StripsMarkersAndMarksBold(t *testing.T) {
	tokens := tokenizeMarkdown("plain **bold word** back`tick`ed __underlined__ end")
	var texts []string
	var bolds []bool
	for _, tok := range tokens {
		texts = append(texts, tok.text)
		bolds = append(bolds, tok.bold)
		require.NotContains(t, tok.text, "*", "rule 1: never a literal asterisk pair in the pane")
		require.NotContains(t, tok.text, "`")
	}
	require.Equal(t, []string{"plain", "bold", "word", "backticked", "underlined", "end"}, texts)
	require.Equal(t, []bool{false, true, true, false, false, false}, bolds)
}

// TestSessionPane_ProseTurn_WrapsAtMeasureWithHangingIndent is the brief's
// own named test: a fixture Turn.Text wraps at the pane's own measure, a
// plain paragraph hangs under the gutter, and a list item's continuation
// hangs under its own marker instead - a deterministic small width (21,
// which SessionPane's own thresholds resolve to the narrow gutter-1 profile
// - see TestSessionPane_MeasureAndGutter_AtNamedGeometries) so the exact
// wrap boundary is computable by hand.
func TestSessionPane_ProseTurn_WrapsAtMeasureWithHangingIndent(t *testing.T) {
	pinHome(t)
	s := NewSessionPane()
	s.SetSize(21, 30)
	require.Equal(t, 1, s.gutter())
	require.Equal(t, 20, s.measure())

	info := fixtureInfo()
	base := time.Date(2026, 9, 2, 18, 3, 25, 0, time.Local)
	info.Tail.Turns = []clarity.Turn{
		{Kind: clarity.TurnAssistant, At: base, Text: "aaaa bbbb cccc dddd eeee"},
		{Kind: clarity.TurnAssistant, At: base, Text: "- item aaaa bbbb cccc"},
	}
	s.SetInfo(info)

	rawLines := strings.Split(ansi.Strip(s.String()), "\n")
	plain := make([]string, len(rawLines))
	for i, l := range rawLines {
		plain[i] = strings.TrimRight(l, " ") // rows are right-padded to the pane's own width
	}
	// The first turn's paragraph greedy-wraps at width 20: "aaaa bbbb cccc
	// dddd" is 19 columns (one more word would be 24), "eeee" spills to its
	// own continuation line, both hung one column under the gutter.
	require.Contains(t, plain, " aaaa bbbb cccc dddd")
	require.Contains(t, plain, " eeee")

	// The second turn's list item hangs its own continuation under its
	// marker ("- ", 2 columns) rather than the bare gutter: gutter(1) +
	// len("- ")(2) = 3 leading spaces once the item's own text overflows
	// its first line.
	require.Contains(t, plain, " - item aaaa bbbb")
	require.Contains(t, plain, "   cccc")
}

// TestSessionPane_BlankLineBetweenTurns_NoneInside is the Spacing section's
// own rule: exactly one blank line separates two turns, and a multi-
// paragraph turn's own paragraphs never get one between them.
func TestSessionPane_BlankLineBetweenTurns_NoneInside(t *testing.T) {
	pinHome(t)
	base := time.Date(2026, 9, 2, 18, 3, 25, 0, time.Local)
	lines, _ := buildTurnLines([]clarity.Turn{
		{Kind: clarity.TurnOwner, At: base, Text: "paragraph one\n\nparagraph two"},
		{Kind: clarity.TurnAssistant, At: base, Text: "reply"},
	}, 2, 96, base, -1)

	blankCount := 0
	for _, l := range lines {
		if l == "" {
			blankCount++
		}
	}
	require.Equal(t, 1, blankCount, "exactly one blank line must separate the two turns, none inside either")
}

// TestSessionPane_TagLineFormat_AlignsYouAndClaude is the brief's own named
// test: the label line is "%-7s  %s" (tag then time), so YOU's and
// CLAUDE's own timestamps land in the same column.
func TestSessionPane_TagLineFormat_AlignsYouAndClaude(t *testing.T) {
	pinHome(t)
	base := time.Date(2026, 9, 2, 18, 3, 25, 0, time.Local)
	lines, _ := buildTurnLines([]clarity.Turn{
		{Kind: clarity.TurnOwner, At: base, Text: "hi"},
		{Kind: clarity.TurnAssistant, At: base, Text: "hi"},
	}, 2, 96, base, -1)

	you := ansi.Strip(lines[0])
	// lines[1] is YOU's own wrapped body ("hi"), lines[2] the blank
	// separator, lines[3] CLAUDE's label.
	claude := ansi.Strip(lines[3])
	require.Equal(t, "YOU      18:03:25", you)
	require.Equal(t, "CLAUDE   18:03:25", claude)
	require.Equal(t, strings.Index(you, "18:"), strings.Index(claude, "18:"),
		"YOU and CLAUDE's own timestamps must land in the same column")
}

// TestSessionPane_ContinuedLabel_StickyOnMidTurnScroll is the sticky
// header's own named test: scrolling so the viewport's top row lands
// mid-turn replaces that row with "<TAG>  ⋯ continued" in the tag's colour,
// and the label reverts once scrolled back to a turn's own first line.
func TestSessionPane_ContinuedLabel_StickyOnMidTurnScroll(t *testing.T) {
	pinHome(t)
	base := time.Date(2026, 9, 2, 18, 0, 0, 0, time.Local)
	longText := strings.Repeat("word ", 200)

	info := fixtureInfo()
	info.Tail.Turns = []clarity.Turn{
		{Kind: clarity.TurnAssistant, At: base, Text: longText},
	}

	s := NewSessionPane()
	s.SetSize(80, 10) // a small turns area: the one turn overflows it
	s.SetInfo(info)

	for i := 0; i < 500; i++ {
		s.ScrollUp()
	}
	top := strings.Split(ansi.Strip(s.String()), "\n")[3] // header x2 + rule
	require.Equal(t, "CLAUDE   18:00:00", strings.TrimRight(top, " "),
		"scrolled all the way up, the top row IS the turn's own label line - no sticky substitution")

	s.ScrollDown()
	s.ScrollDown()
	mid := strings.Split(ansi.Strip(s.String()), "\n")[3]
	require.Equal(t, "CLAUDE   ⋯ continued", strings.TrimRight(mid, " "),
		"scrolled past the label line, the sticky header must name the scrolled-off turn's own tag")
}

// TestSessionPane_MeasureAndGutter_AtNamedGeometries is SESSION-READING-
// SPEC.md's own two geometry profiles: 116 (164x45's own pane inner width)
// gets padding 1/gutter 2/measure 96 (min(96, 114-2)=96, capped); 80
// (120x36's own pane inner width, no separate padding at this size) gets
// padding 0/gutter 1/measure 79 (80-1, uncapped).
func TestSessionPane_MeasureAndGutter_AtNamedGeometries(t *testing.T) {
	wide := NewSessionPane()
	wide.SetSize(116, 40)
	require.Equal(t, 1, wide.pad())
	require.Equal(t, 2, wide.gutter())
	require.Equal(t, 114, wide.contentWidth())
	require.Equal(t, 96, wide.measure())

	narrow := NewSessionPane()
	narrow.SetSize(80, 30)
	require.Equal(t, 0, narrow.pad())
	require.Equal(t, 1, narrow.gutter())
	require.Equal(t, 80, narrow.contentWidth())
	require.Equal(t, 79, narrow.measure())
}

// TestSessionPane_ChromeReachesPaneEdge_AtNamedGeometries proves the
// pane-widened-to-the-real-right-edge rule at the SessionPane level: the
// header rule (a full contentWidth-plus-padding line) reaches exactly the
// width SetSize was given, at both named geometries.
func TestSessionPane_ChromeReachesPaneEdge_AtNamedGeometries(t *testing.T) {
	pinHome(t)
	for _, w := range []int{116, 80} {
		s := NewSessionPane()
		s.SetSize(w, 30)
		s.SetInfo(fixtureInfo())
		lines := strings.Split(s.String(), "\n")
		require.Equal(t, w, ansi.StringWidth(lines[2]), "the header rule must reach exactly the pane's own received width %d", w)
	}
}

// -- slice 22, PART B: copy from the Session tab -------------------------

// TestTurnCopyText_ProseTurn_TagTimeThenParagraphs is PART B's own named
// shape: an owner/assistant turn copies as "<TAG>  hh:mm:ss", a blank line,
// then the turn's own text with its paragraphs intact (never collapsed to
// one line, never word-wrapped for the clipboard).
func TestTurnCopyText_ProseTurn_TagTimeThenParagraphs(t *testing.T) {
	at := time.Date(2026, 9, 3, 14, 22, 5, 0, time.Local)
	turn := clarity.Turn{Kind: clarity.TurnAssistant, At: at, Text: "paragraph one\n\nparagraph two"}

	got := TurnCopyText(turn, at)
	want := "CLAUDE  14:22:05\n\nparagraph one\n\nparagraph two"
	require.Equal(t, want, got)
}

// TestTurnCopyText_ToolTurn_MarkerSummaryResult is PART B's own named tool
// shape, verbatim: "▪ <tool>  <summary>  <result>".
func TestTurnCopyText_ToolTurn_MarkerSummaryResult(t *testing.T) {
	at := time.Date(2026, 9, 3, 14, 22, 5, 0, time.Local)
	turn := clarity.Turn{Kind: clarity.TurnTool, At: at, Tool: "Bash", Summary: "run the check", Result: clarity.ResultOK, Duration: 2100 * time.Millisecond}

	got := TurnCopyText(turn, at)
	require.Equal(t, "▪ Bash  run the check  exit 0     2.1s", got)
}

// TestTurnCopyText_StripsRawANSI proves the ansi.Strip pass is actually
// wired in: a turn whose own transcript text somehow carries a raw escape
// sequence (a pasted terminal capture, say) never reaches the clipboard
// with it still attached.
func TestTurnCopyText_StripsRawANSI(t *testing.T) {
	at := time.Date(2026, 9, 3, 14, 22, 5, 0, time.Local)
	turn := clarity.Turn{Kind: clarity.TurnOwner, At: at, Text: "\x1b[31mred\x1b[0m plain"}

	got := TurnCopyText(turn, at)
	require.NotContains(t, got, "\x1b[")
	require.Contains(t, got, "red plain")
}

// TestSessionPane_LastTurnCopyText_ReturnsLastTurnAndLineCount proves the c
// key's own source: the SELECTED lane's most recent turn, plus the line
// count the footer names ("copied · last turn (N lines)").
func TestSessionPane_LastTurnCopyText_ReturnsLastTurnAndLineCount(t *testing.T) {
	pinHome(t)
	s := NewSessionPane()
	s.SetSize(116, 40)
	s.SetInfo(fixtureInfo())

	text, lines, ok := s.LastTurnCopyText()
	require.True(t, ok)
	require.Contains(t, text, "▪ Bash  run the check  exit 0     2.1s", "the fixture's own LAST turn is the tool turn")
	require.Equal(t, 1, lines)
}

// TestSessionPane_LastTurnCopyText_NothingSelected_NoOp proves ok=false
// with no SessionInfo set - the c key never claims a copy of nothing.
func TestSessionPane_LastTurnCopyText_NothingSelected_NoOp(t *testing.T) {
	s := NewSessionPane()
	_, _, ok := s.LastTurnCopyText()
	require.False(t, ok)
}

// TestSessionPane_TailCopyText_JoinsEveryLoadedTurn is the C key's own
// source: every turn currently loaded (Tail.Turns), not only the lines
// scrolled into view, blank-line joined, plus the turn count the footer
// names ("copied · N turns").
func TestSessionPane_TailCopyText_JoinsEveryLoadedTurn(t *testing.T) {
	pinHome(t)
	s := NewSessionPane()
	s.SetSize(116, 40)
	s.SetInfo(fixtureInfo())

	text, turns, ok := s.TailCopyText()
	require.True(t, ok)
	require.Equal(t, 3, turns, "the fixture carries all three turn kinds")
	require.True(t, strings.HasPrefix(text, "YOU  18:03:25"), "the first turn leads the joined block")
	require.True(t, strings.HasSuffix(text, "▪ Bash  run the check  exit 0     2.1s"), "the last turn trails the joined block")
}

// TestSessionPane_Picker_OpenHighlightsNewestTurn proves v starts the
// picker on the NEWEST turn (Tail.Turns' own last entry) - the fixture's
// tool turn - marked with the picker's own gutter marker and accent style.
func TestSessionPane_Picker_OpenHighlightsNewestTurn(t *testing.T) {
	pinHome(t)
	s := NewSessionPane()
	s.SetSize(116, 40)
	s.SetInfo(fixtureInfo())

	require.False(t, s.PickerActive())
	require.True(t, s.OpenPicker())
	require.True(t, s.PickerActive())

	out := ansi.Strip(s.String())
	var toolLine string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "▪ Bash") {
			toolLine = l
			break
		}
	}
	require.NotEmpty(t, toolLine, "the tool turn must still render its own line")
	require.True(t, strings.HasPrefix(strings.TrimLeft(toolLine, " "), sessionPickerMarker),
		"the newest (last) turn - the tool turn - starts highlighted, marker and all: %q", toolLine)
	require.Contains(t, toolLine, "run the check")
}

// TestSessionPane_Picker_UpMovesToOlderTurn_CopiesHighlighted proves
// PickerOlder moves the highlight to the previous (older) turn and
// PickerCopyText then copies THAT turn, not the last one.
func TestSessionPane_Picker_UpMovesToOlderTurn_CopiesHighlighted(t *testing.T) {
	pinHome(t)
	s := NewSessionPane()
	s.SetSize(116, 40)
	s.SetInfo(fixtureInfo())
	require.True(t, s.OpenPicker())

	// Fixture order (oldest first): owner, assistant, tool. Newest (tool)
	// starts highlighted; one PickerOlder step must land on the assistant
	// turn.
	s.PickerOlder()

	text, _, ok := s.PickerCopyText()
	require.True(t, ok)
	require.Contains(t, text, "CLAUDE  18:03:41")
	require.Contains(t, text, "Understood: the right pane mirrors the selected lane's live session")

	out := ansi.Strip(s.String())
	var claudeLine string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "CLAUDE") {
			claudeLine = l
			break
		}
	}
	require.NotEmpty(t, claudeLine)
	require.True(t, strings.HasPrefix(strings.TrimLeft(claudeLine, " "), sessionPickerMarker+"CLAUDE"),
		"the picker's own marker must have moved onto the newly-highlighted turn's label: %q", claudeLine)
}

// TestSessionPane_Picker_OlderAtStart_NoOp proves PickerOlder is a no-op
// once the highlight is already on the OLDEST turn (index 0) - never wraps
// or panics past the start of the transcript.
func TestSessionPane_Picker_OlderAtStart_NoOp(t *testing.T) {
	pinHome(t)
	s := NewSessionPane()
	s.SetSize(116, 40)
	s.SetInfo(fixtureInfo())
	require.True(t, s.OpenPicker())
	s.PickerOlder() // tool -> assistant
	s.PickerOlder() // assistant -> owner (oldest)
	s.PickerOlder() // no-op: already oldest

	text, _, ok := s.PickerCopyText()
	require.True(t, ok)
	require.Contains(t, text, "YOU  18:03:25")
}

// TestSessionPane_Picker_NewerAtEnd_NoOp mirrors the above at the newest
// end - PickerNewer never moves past Tail.Turns' own last entry.
func TestSessionPane_Picker_NewerAtEnd_NoOp(t *testing.T) {
	pinHome(t)
	s := NewSessionPane()
	s.SetSize(116, 40)
	s.SetInfo(fixtureInfo())
	require.True(t, s.OpenPicker()) // starts on the newest (tool) turn
	s.PickerNewer()                 // no-op: already newest

	text, _, ok := s.PickerCopyText()
	require.True(t, ok)
	require.Contains(t, text, "▪ Bash")
}

// TestSessionPane_Picker_Close_ReturnsToOrdinaryRender proves esc (via
// ClosePicker) leaves the picker: PickerActive reports false and no marker
// remains in the rendered turns.
func TestSessionPane_Picker_Close_ReturnsToOrdinaryRender(t *testing.T) {
	pinHome(t)
	s := NewSessionPane()
	s.SetSize(116, 40)
	s.SetInfo(fixtureInfo())
	require.True(t, s.OpenPicker())

	s.ClosePicker()
	require.False(t, s.PickerActive())
	out := ansi.Strip(s.String())
	require.NotContains(t, out, sessionPickerMarker)

	_, _, ok := s.PickerCopyText()
	require.False(t, ok, "PickerCopyText must refuse once the picker is closed")
}

// TestSessionPane_Picker_NothingToPickFrom_NoOp proves OpenPicker refuses
// (returns false, never enters picker state) when the selected lane has no
// turns yet - v never opens an empty picker.
func TestSessionPane_Picker_NothingToPickFrom_NoOp(t *testing.T) {
	s := NewSessionPane()
	s.SetSize(116, 40)
	require.False(t, s.OpenPicker())
	require.False(t, s.PickerActive())
}
