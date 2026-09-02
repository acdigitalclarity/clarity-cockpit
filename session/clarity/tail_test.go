package clarity

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// --- fixture line builders, matching the real record shapes confirmed
// against a live transcript before this file was written (see the leg's
// report for the quoted samples). ---

func fixtureTimestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func assistantTextLine(ts time.Time, model, text string) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"role":"assistant","model":%q,"content":[{"type":"text","text":%q}]}}`,
		fixtureTimestamp(ts), model, text)
}

func assistantThinkingLine(ts time.Time, model, thinking string) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"role":"assistant","model":%q,"content":[{"type":"thinking","thinking":%q}]}}`,
		fixtureTimestamp(ts), model, thinking)
}

func assistantToolUseLine(ts time.Time, model, id, name, description string) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"role":"assistant","model":%q,"content":[{"type":"tool_use","id":%q,"name":%q,"input":{"command":"x","description":%q}}]}}`,
		fixtureTimestamp(ts), model, id, name, description)
}

func userToolResultLine(ts time.Time, toolUseID string, isError bool, denialKind string) string {
	denial := ""
	if denialKind != "" {
		denial = fmt.Sprintf(`,"toolDenialKind":%q`, denialKind)
	}
	return fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"content":"result","is_error":%v}]}%s}`,
		fixtureTimestamp(ts), toolUseID, isError, denial)
}

func ownerLine(ts time.Time, text string) string {
	return fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"role":"user","content":%q}}`, fixtureTimestamp(ts), text)
}

func turnDurationLine(ts time.Time, durationMs int64, messageCount, pendingAgents int) string {
	return fmt.Sprintf(`{"type":"system","subtype":"turn_duration","timestamp":%q,"durationMs":%d,"messageCount":%d,"pendingBackgroundAgentCount":%d}`,
		fixtureTimestamp(ts), durationMs, messageCount, pendingAgents)
}

func modeLine(mode string) string {
	return fmt.Sprintf(`{"type":"mode","mode":%q}`, mode)
}

func customTitleLine() string  { return `{"type":"custom-title","customTitle":"fixture-lane"}` }
func agentNameLine() string    { return `{"type":"agent-name","agentName":"fixture-lane"}` }
func lastPromptLine() string   { return `{"type":"last-prompt","leafUuid":"x"}` }
func atisLatchLine() string    { return `{"type":"atis-latch","atis":""}` }
func fileSnapshotLine() string { return `{"type":"file-history-snapshot","messageId":"x"}` }
func ledgerLine() string       { return `{"type":"artifact-autoreact-ledger","v":1}` }
func commentMonitorLine() string {
	return `{"type":"artifact-comment-monitor","v":1}`
}

// prLinkLineWithTimestamp deliberately carries a real timestamp field, the
// way it does on a live transcript - proving ClassifyState skips it by
// TYPE, not merely because it happens to lack one.
func prLinkLineWithTimestamp(ts time.Time) string {
	return fmt.Sprintf(`{"type":"pr-link","prNumber":1,"timestamp":%q}`, fixtureTimestamp(ts))
}

func queueOperationLineWithTimestamp(ts time.Time) string {
	return fmt.Sprintf(`{"type":"queue-operation","operation":"enqueue","timestamp":%q}`, fixtureTimestamp(ts))
}

func writeFixture(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.jsonl")
	var body string
	for _, l := range lines {
		body += l + "\n"
	}
	require.NoError(t, os.WriteFile(path, []byte(body), 0644))
	return path
}

func TestClassifyState_ClosedWithPendingAgents_Working(t *testing.T) {
	now := time.Date(2026, 9, 2, 20, 0, 0, 0, time.UTC)
	path := writeFixture(t, []string{
		assistantTextLine(now.Add(-10*time.Minute), "claude-fable-5-1", "on it"),
		turnDurationLine(now.Add(-1*time.Minute), 5000, 4, 2),
	})
	tail, err := ReadLaneTail(path, DefaultTailMaxBytes, DefaultTailTurns, now)
	require.NoError(t, err)
	require.Equal(t, StateWorking, tail.State)
	require.Equal(t, 2, tail.PendingAgents)
	require.False(t, tail.OpenTurn)
}

func TestClassifyState_ClosedFiveMinutesAgoNoAgents_WaitingOnYou(t *testing.T) {
	now := time.Date(2026, 9, 2, 20, 0, 0, 0, time.UTC)
	path := writeFixture(t, []string{
		turnDurationLine(now.Add(-5*time.Minute), 1000, 3, 0),
	})
	tail, err := ReadLaneTail(path, DefaultTailMaxBytes, DefaultTailTurns, now)
	require.NoError(t, err)
	require.Equal(t, StateWaitingYou, tail.State)
	require.Equal(t, 0, tail.PendingAgents)
}

func TestClassifyState_ClosedTwoHoursAgo_Idle(t *testing.T) {
	now := time.Date(2026, 9, 2, 20, 0, 0, 0, time.UTC)
	path := writeFixture(t, []string{
		turnDurationLine(now.Add(-2*time.Hour), 1000, 3, 0),
	})
	tail, err := ReadLaneTail(path, DefaultTailMaxBytes, DefaultTailTurns, now)
	require.NoError(t, err)
	require.Equal(t, StateIdle, tail.State)
}

func TestClassifyState_OpenTurnFifteenMinutesOld_Stalled(t *testing.T) {
	now := time.Date(2026, 9, 2, 20, 0, 0, 0, time.UTC)
	path := writeFixture(t, []string{
		assistantTextLine(now.Add(-15*time.Minute), "claude-fable-5-1", "still going"),
	})
	tail, err := ReadLaneTail(path, DefaultTailMaxBytes, DefaultTailTurns, now)
	require.NoError(t, err)
	require.Equal(t, StateStalled, tail.State)
	require.True(t, tail.OpenTurn)
}

func TestClassifyState_OpenTurnUnderTenMinutes_Working(t *testing.T) {
	now := time.Date(2026, 9, 2, 20, 0, 0, 0, time.UTC)
	path := writeFixture(t, []string{
		assistantTextLine(now.Add(-7*time.Minute), "claude-fable-5-1", "working through it"),
	})
	tail, err := ReadLaneTail(path, DefaultTailMaxBytes, DefaultTailTurns, now)
	require.NoError(t, err)
	require.Equal(t, StateWorking, tail.State)
	require.True(t, tail.OpenTurn)
}

func TestClassifyState_TrailingBookkeepingRowsSkipped(t *testing.T) {
	now := time.Date(2026, 9, 2, 20, 0, 0, 0, time.UTC)
	closedAt := now.Add(-5 * time.Minute)
	path := writeFixture(t, []string{
		turnDurationLine(closedAt, 1000, 3, 0),
		modeLine("normal"),
		customTitleLine(),
		agentNameLine(),
		lastPromptLine(),
		atisLatchLine(),
		fileSnapshotLine(),
		ledgerLine(),
		commentMonitorLine(),
		// These two carry a real timestamp, far newer than closedAt - and
		// must still be skipped, because the rule walks past them by type.
		prLinkLineWithTimestamp(now.Add(-1 * time.Second)),
		queueOperationLineWithTimestamp(now.Add(-1 * time.Second)),
	})
	tail, err := ReadLaneTail(path, DefaultTailMaxBytes, DefaultTailTurns, now)
	require.NoError(t, err)
	require.Equal(t, StateWaitingYou, tail.State, "must classify off the turn_duration record, not the newer pr-link/queue-operation timestamps")
	require.WithinDuration(t, closedAt, tail.LastTurn, time.Second)
}

func TestBuildTurns_OwnerStringVsToolResultArray(t *testing.T) {
	now := time.Date(2026, 9, 2, 20, 0, 0, 0, time.UTC)
	path := writeFixture(t, []string{
		ownerLine(now.Add(-2*time.Minute), "please check the fixture"),
		assistantToolUseLine(now.Add(-1*time.Minute), "claude-fable-5-1", "toolu_1", "Bash", "run the check"),
		userToolResultLine(now.Add(-50*time.Second), "toolu_1", false, ""),
	})
	tail, err := ReadLaneTail(path, DefaultTailMaxBytes, DefaultTailTurns, now)
	require.NoError(t, err)

	var ownerTurns []Turn
	for _, turn := range tail.Turns {
		if turn.Kind == TurnOwner {
			ownerTurns = append(ownerTurns, turn)
		}
	}
	require.Len(t, ownerTurns, 1, "the tool_result-array user record must never render as an owner turn")
	require.Equal(t, "please check the fixture", ownerTurns[0].Text)
}

func TestBuildTurns_ToolUseMatchedToResult_DeniedAndError(t *testing.T) {
	now := time.Date(2026, 9, 2, 20, 0, 0, 0, time.UTC)
	path := writeFixture(t, []string{
		assistantToolUseLine(now.Add(-4*time.Minute), "claude-fable-5-1", "toolu_denied", "Bash", "rm -rf something"),
		userToolResultLine(now.Add(-3*time.Minute), "toolu_denied", true, "permission-rule"),
		assistantToolUseLine(now.Add(-2*time.Minute), "claude-fable-5-1", "toolu_error", "Read", "read a missing file"),
		userToolResultLine(now.Add(-1*time.Minute), "toolu_error", true, ""),
	})
	tail, err := ReadLaneTail(path, DefaultTailMaxBytes, DefaultTailTurns, now)
	require.NoError(t, err)

	var denied, errored *Turn
	for i := range tail.Turns {
		switch tail.Turns[i].Tool {
		case "Bash":
			denied = &tail.Turns[i]
		case "Read":
			errored = &tail.Turns[i]
		}
	}
	require.NotNil(t, denied)
	require.NotNil(t, errored)
	require.Equal(t, ResultDenied, denied.Result, "toolDenialKind on the user record must mark denied, not error")
	require.Equal(t, ResultError, errored.Result)
	require.Equal(t, time.Minute, errored.Duration)
}

func TestBuildTurns_ThinkingDropped(t *testing.T) {
	now := time.Date(2026, 9, 2, 20, 0, 0, 0, time.UTC)
	path := writeFixture(t, []string{
		assistantThinkingLine(now.Add(-2*time.Minute), "claude-fable-5-1", "internal reasoning that must never render"),
	})
	tail, err := ReadLaneTail(path, DefaultTailMaxBytes, DefaultTailTurns, now)
	require.NoError(t, err)
	require.Empty(t, tail.Turns, "a thinking-only assistant record renders no Turn")
}

func TestReadLaneTail_MalformedLineSkippedAndCounted(t *testing.T) {
	now := time.Date(2026, 9, 2, 20, 0, 0, 0, time.UTC)
	path := writeFixture(t, []string{
		ownerLine(now.Add(-3*time.Minute), "before the garbage"),
		`{this is not valid json`,
		turnDurationLine(now.Add(-2*time.Minute), 1000, 2, 0),
	})
	tail, err := ReadLaneTail(path, DefaultTailMaxBytes, DefaultTailTurns, now)
	require.NoError(t, err)
	require.Equal(t, 1, tail.MalformedLines)
	require.Len(t, tail.Turns, 1)
	require.Equal(t, "before the garbage", tail.Turns[0].Text)
}

func TestReadLaneTail_TailLargerThanMaxBytes_DiscardsPartialFirstLine(t *testing.T) {
	now := time.Date(2026, 9, 2, 20, 0, 0, 0, time.UTC)

	// Build enough filler lines that the file is comfortably over the tiny
	// maxBytes budget below, then measure exactly where each line starts so
	// the cut point can be placed deliberately mid-line, not at a boundary.
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, assistantTextLine(now.Add(-time.Duration(40-i)*time.Hour),
			"claude-fable-5-1", fmt.Sprintf("filler line number %d padded out with extra words to take up real space", i)))
	}
	markerAt := now.Add(-30 * time.Second)
	lines = append(lines, assistantTextLine(markerAt, "claude-fable-5-1", "MARKER-TEXT"))
	lines = append(lines, turnDurationLine(now.Add(-10*time.Second), 500, 6, 0))

	path := writeFixture(t, lines)

	info, err := os.Stat(path)
	require.NoError(t, err)
	totalSize := info.Size()

	// Cut roughly a third of the way in, which by construction of 40
	// same-length filler lines lands inside one of them, never on a
	// newline boundary.
	maxBytes := int(totalSize - totalSize/3)

	tail, err := ReadLaneTail(path, maxBytes, 5, now)
	require.NoError(t, err)
	require.Equal(t, 0, tail.MalformedLines, "the discarded partial first line must never be counted as malformed")
	require.Equal(t, StateWaitingYou, tail.State)

	found := false
	for _, turn := range tail.Turns {
		if turn.Text == "MARKER-TEXT" {
			found = true
		}
	}
	require.True(t, found, "the marker turn after the cut must still parse correctly")
}

func TestClassifyState_EmptyTranscript_IdleWithReason(t *testing.T) {
	now := time.Date(2026, 9, 2, 20, 0, 0, 0, time.UTC)
	path := writeFixture(t, nil)
	tail, err := ReadLaneTail(path, DefaultTailMaxBytes, DefaultTailTurns, now)
	require.NoError(t, err)
	require.Equal(t, StateIdle, tail.State)
	require.Contains(t, tail.StateReason, "no timestamped record")
}

func TestRenderHeaderLine(t *testing.T) {
	now := time.Date(2026, 9, 2, 20, 0, 0, 0, time.UTC)
	tail := LaneTail{
		State:       StateWorking,
		StateReason: "turn closed 1m ago with 2 background agent(s) still pending",
		LastTurn:    now.Add(-1 * time.Minute),
		LastWrite:   now,
	}
	line := RenderHeaderLine("fixture-lane", tail)
	require.Contains(t, line, "fixture-lane")
	require.Contains(t, line, StateWorking)
	require.Contains(t, line, "last turn 19:59:00")
	require.Contains(t, line, "last write 20:00:00")
	require.Contains(t, line, tail.StateReason)
}

func TestRenderTurnLines_WrapsAt100Columns(t *testing.T) {
	turn := Turn{
		Kind: TurnAssistant,
		At:   time.Date(2026, 9, 2, 19, 0, 0, 0, time.UTC),
		Text: "this is a long piece of prose that is deliberately padded out with enough words to force the renderer to wrap it across more than one line at the configured width",
	}
	lines := RenderTurnLines(turn, 100)
	require.Greater(t, len(lines), 1)
	for _, l := range lines {
		require.LessOrEqual(t, len(l), 100)
	}
}

func TestRenderTurnLines_ToolIncludesResultAndDuration(t *testing.T) {
	turn := Turn{
		Kind:     TurnTool,
		At:       time.Date(2026, 9, 2, 19, 0, 0, 0, time.UTC),
		Tool:     "Bash",
		Summary:  "run the check",
		Result:   ResultOK,
		Duration: 3 * time.Second,
	}
	lines := RenderTurnLines(turn, 100)
	require.Len(t, lines, 1)
	require.Contains(t, lines[0], "▪ Bash")
	require.Contains(t, lines[0], "run the check")
	require.Contains(t, lines[0], "[ok 3s]")
}

func TestTranscriptForLane_NoLiveOrResolvableLane(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)
	sessionsRoot := t.TempDir()
	t.Setenv(SessionsRootEnvVar, sessionsRoot)

	_, err := TranscriptForLane("no-such-lane")
	require.Error(t, err)
}

func systemSummaryLine(ts time.Time, subtype string) string {
	return fmt.Sprintf(`{"type":"system","subtype":%q,"timestamp":%q,"content":"the lane wrote a summary of itself"}`, subtype, fixtureTimestamp(ts))
}

// A harness summary record (away_summary, stop_hook_summary, local_command)
// written after the closing turn_duration is not a conversation record and
// must not re-open the turn. Seen live on weekend-run 2 Sep: closed 12:43,
// an away_summary after it, wrongly read as stalled.
func TestClassifyState_SystemSummaryAfterClose_StaysClosed(t *testing.T) {
	now := time.Date(2026, 9, 2, 20, 0, 0, 0, time.UTC)
	closedAt := now.Add(-2 * time.Hour)
	path := writeFixture(t, []string{
		assistantTextLine(closedAt.Add(-time.Minute), "claude-fable-5-1", "done"),
		turnDurationLine(closedAt, 5000, 4, 0),
		systemSummaryLine(closedAt.Add(time.Minute), "away_summary"),
		systemSummaryLine(closedAt.Add(2*time.Minute), "stop_hook_summary"),
	})
	tail, err := ReadLaneTail(path, DefaultTailMaxBytes, DefaultTailTurns, now)
	require.NoError(t, err)
	require.Equal(t, StateIdle, tail.State, "system summaries after the close must not re-open the turn")
	require.False(t, tail.OpenTurn)
	require.WithinDuration(t, closedAt, tail.LastTurn, time.Second)
}
