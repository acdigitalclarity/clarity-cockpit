// Package clarity: this file reads the tail of a lane's own Claude Code
// transcript (~/.claude/projects/<encoded>/<session>.jsonl) and turns it
// into a typed stream of Turn values plus a derived working/waiting/idle/
// stalled state word - the foundation the cockpit's right pane (Session and
// Needs-you tabs) reads from. It never scans a whole multi-megabyte
// transcript: ReadLaneTail seeks to the last maxBytes of the file first, so
// cost stays bounded no matter how old or long the session is.
package clarity

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Turn kinds.
const (
	TurnOwner     = "owner"
	TurnAssistant = "assistant"
	TurnTool      = "tool"
)

// Tool result outcomes.
const (
	ResultOK      = "ok"
	ResultError   = "error"
	ResultDenied  = "denied"
	ResultRunning = "running"
)

// State words ClassifyState produces - the only four the cockpit renders.
const (
	StateWorking    = "working"
	StateWaitingYou = "waiting on you"
	StateIdle       = "idle"
	StateStalled    = "stalled"
)

// StateNeedsKey is the permission-prompt sentinel state (ANSWER-AND-BANK-
// SPEC.md item 7 / research item 7) - never returned by ClassifyState (the
// transcript carries no record of a pending prompt at all, DECISIONS.md:
// "A pending permission prompt is invisible in the transcript"). Only a
// tracked instance's own live tmux pane sample sets it (app.go's feed tick,
// session.Instance.SetNeedsKey, IsPermissionPrompt below), overriding
// whatever the transcript-derived word would otherwise read - "ahead of
// every other word", the brief's own phrase - since app.go substitutes it
// directly into the rendered LaneTail.State before the row/header draw.
// External lanes never carry it (no tracked tmux session to sample).
const StateNeedsKey = "needs a key"

// DefaultTailMaxBytes is ReadLaneTail's default read window: the last
// 256 KiB of the transcript, which comfortably covers several turns of a
// normal working session without paying to scan the whole file.
const DefaultTailMaxBytes = 256 * 1024

// DefaultTailTurns is ReadLaneTail's default turn count when the caller
// passes maxTurns <= 0.
const DefaultTailTurns = 20

// scannerMaxLine bounds a single transcript line's size. A tail read is
// already bounded to roughly maxBytes total, but one assistant message
// (embedded thinking signature, a large tool result) can still be a long
// single line; this keeps that case from panicking bufio.Scanner instead
// of silently truncating the read window further.
const scannerMaxLine = 8 * 1024 * 1024

// Turn is one rendered unit of a lane's conversation: an owner message, an
// assistant prose reply, or one tool call. Kind selects which of the
// remaining fields apply.
type Turn struct {
	Kind     string // owner | assistant | tool
	At       time.Time
	Text     string        // owner or assistant prose; thinking is dropped
	Tool     string        // tool name, Kind == tool only
	Summary  string        // the tool call's description or first input line
	Result   string        // ok | error | denied | running, Kind == tool only
	Duration time.Duration // tool_use -> tool_result gap, where derivable
}

// LaneTail is ReadLaneTail's result: the lane's derived state plus its last
// few turns, oldest first.
type LaneTail struct {
	Lane           string
	Transcript     string
	State          string // working | waiting on you | idle | stalled
	StateReason    string // one short sentence naming the record and age
	LastWrite      time.Time
	LastTurn       time.Time // last TIMESTAMPED record, not necessarily a rendered Turn
	OpenTurn       bool
	PendingAgents  int
	Mode           string
	Model          string
	Messages       int
	TurnDuration   time.Duration
	Turns          []Turn
	MalformedLines int // lines that failed to parse as JSON, skipped

	// Truncated is true when there is conversation before the returned Turns
	// that this read never rendered - either because the byte-window seek
	// (maxBytes) started past the file's beginning, or because buildTurns
	// itself dropped older turns to fit maxTurns. The Session pane's "⋯
	// earlier in this session" divider (design/cockpit-pane/DECISIONS.md
	// slice 3) reads this rather than re-deriving it from Turns/Messages.
	Truncated bool

	// AnsweredAt is set (item 5, WAITING HELD) the instant ClassifyState
	// resolves a closed, no-pending-agent turn's own "waiting on you" into
	// StateIdle - either because a newer owner turn now sits in the
	// transcript, or because the cockpit itself sent into this lane after
	// the close (ClassifyState's own sentAt argument). Zero when no such
	// transition has happened (including every other state). The Session
	// pane's state line reads this to show "answered N min ago" in place of
	// the usual "turn closed hh:mm:ss" clause while it is under 30 minutes
	// old (renderStateLine, ui/session.go).
	AnsweredAt time.Time

	// AwaySummary is the newest system/away_summary record still inside this
	// read's own tail window (item 4, SINCE YOU WERE AWAY) - the harness's
	// own "Goal ... Next ..." recap, previously discarded by
	// lastTimestampedRecord's anchor walk along with every other non-
	// turn_duration system record. Zero (At.IsZero()) when the window holds
	// none.
	AwaySummary AwaySummary
}

// AwaySummary is one lane's own newest away_summary record: its free-text
// content (confirmed against live transcripts to already read "Goal ...
// Next ..." prose, never separate structured fields - see latestAwaySummary's
// own doc comment) and the timestamp it was written.
type AwaySummary struct {
	Text string
	At   time.Time
}

// rawRecord is the union of the transcript record fields tail.go relies on,
// confirmed against a real transcript under
// /Users/allencoates/.claude/projects/-Users-allencoates-projects-Clarity-sessions-ways-of-working/
// before this file was written (see the leg's report for the quoted
// samples). Fields absent on a given record type simply decode to zero
// values - json.Unmarshal never errors for that reason alone.
type rawRecord struct {
	Type                        string      `json:"type"`
	Subtype                     string      `json:"subtype"`
	Timestamp                   string      `json:"timestamp"`
	Mode                        string      `json:"mode"`
	ToolDenialKind              string      `json:"toolDenialKind"`
	DurationMs                  int64       `json:"durationMs"`
	MessageCount                int         `json:"messageCount"`
	PendingBackgroundAgentCount int         `json:"pendingBackgroundAgentCount"`
	// Content is the TOP-LEVEL "content" field a system record (away_summary,
	// local_command, queue-operation, bridge_status) carries as a plain
	// string - a different field from rawMessage.Content below, which lives
	// under "message" and is a plain string only for an owner turn. Confirmed
	// against five live away_summary records across three transcripts before
	// this field was added (item 4's own leg report quotes them): every
	// sighting had "content" as a bare JSON string, never an object or array,
	// so a plain Go string decodes it directly with no further parsing.
	Content string      `json:"content"`
	Message *rawMessage `json:"message"`
}

// rawMessage mirrors the .message block on assistant/user records.
// Content is left as raw JSON because it is a plain string for an owner
// turn and an array of content items for everything else (assistant
// content, or a user record carrying tool_result plumbing).
type rawMessage struct {
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
}

// rawContentItem is one element of an assistant/user content array: a text
// block, a tool_use call, or a tool_result reply.
type rawContentItem struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`          // tool_use
	Name      string          `json:"name"`        // tool_use
	Input     json.RawMessage `json:"input"`       // tool_use
	ToolUseID string          `json:"tool_use_id"` // tool_result
	IsError   bool            `json:"is_error"`    // tool_result
}

// harnessRecordTags are the fixed tags the harness itself writes at the
// start of an injected user record (a task-notification, a system-reminder,
// a slash-command echo, a local-command result, a cross-session relay) -
// confirmed against a live transcript's own task-notification body
// (/Users/allencoates/.claude/projects/-Users-allencoates-projects-Clarity-sessions-ways-of-working/0027514b-8a29-48c8-b98e-ff6b81b4ecf4.jsonl,
// grep "<task-notification>" in a user record; see the leg's report for the
// quoted sample) before this file was written. A user string record that
// starts with one of these, after trimming, is a harness note the owner
// never typed - never an owner Turn, DEFECT 1's own rule.
var harnessRecordTags = []string{
	"<system-reminder>",
	"<task-notification>",
	"<command-name>",
	"<command-message>",
	"<local-command-stdout>",
	"<local-command-caveat>",
	"<cross-session-message>",
}

// isHarnessTaggedText reports whether s, trimmed, starts with one of
// harnessRecordTags.
func isHarnessTaggedText(s string) bool {
	trimmed := strings.TrimSpace(s)
	for _, tag := range harnessRecordTags {
		if strings.HasPrefix(trimmed, tag) {
			return true
		}
	}
	return false
}

// isHarnessTaggedUserRecord reports whether r is a "user" record whose
// message content is a plain string starting with a harness tag - the same
// test buildTurns applies before rendering an owner Turn, reused here so
// ClassifyState's anchor walk and the turn stream can never disagree about
// which records are harness notes.
func isHarnessTaggedUserRecord(r rawRecord) bool {
	if r.Type != "user" || r.Message == nil {
		return false
	}
	text, _, isString := parseContentItems(r.Message.Content)
	return isString && isHarnessTaggedText(text)
}

// untimestampedBookkeepingTypes are the record types ClassifyState's
// backward walk steps past, verified on 2 Sep against five live lanes by
// the design leg (DECISIONS.md, slice 1). A record of one of these types is
// never the anchor ClassifyState reads its age from, even on the rare
// occasion it happens to carry its own timestamp field (pr-link and
// queue-operation both do, in practice) - the rule is by type, not by
// field presence, so it never depends on that detail staying true.
var untimestampedBookkeepingTypes = map[string]bool{
	"artifact-autoreact-ledger": true,
	"artifact-comment-monitor":  true,
	"mode":                      true,
	"custom-title":              true,
	"agent-name":                true,
	"last-prompt":               true,
	"atis-latch":                true,
	"pr-link":                   true,
	"file-history-snapshot":     true,
	"queue-operation":           true,
}

// TranscriptForLane resolves the transcript path for a lane name, reusing
// this package's existing discovery rather than re-deriving it: first the
// live external-lane scan (discover.go), which already finds a session
// lane like "ways-of-working" under its encoded "sessions-" name, then a
// direct lookup under the Clarity sessions root for a lane that exists but
// is not currently live enough to appear in the external scan.
func TranscriptForLane(lane string) (string, error) {
	if external, err := DiscoverExternalLanes(nil); err == nil {
		for _, ext := range external {
			if MatchesQueriedLane(ext, lane) {
				return ext.TranscriptPath, nil
			}
		}
	}
	if lanePath, err := ResolveExistingLaneDir(lane); err == nil {
		if path, ok := NewestTranscript(lanePath); ok {
			return path, nil
		}
	}
	return "", fmt.Errorf("no live or resolvable transcript found for lane %q", lane)
}

// ReadLaneTail reads the last maxBytes (default DefaultTailMaxBytes) of
// transcriptPath, parsing forward line by line as JSON from the seek
// point. The first line after a mid-file seek is always a partial line
// straddling the cut and is discarded before parsing, never counted as
// malformed. Any other line that fails to parse as JSON is skipped and
// counted in MalformedLines. maxTurns <= 0 uses DefaultTailTurns.
//
// sentAt is item 5's own "the cockpit sent into that lane" signal - the
// last time THIS process sent a prompt into this lane, if any, kept by the
// caller (never derived from the transcript itself, since a cockpit-sent
// prompt earns no immediate write there). Variadic and optional so every
// existing call site (main.go's lane-tail CLI, tail_cache.go's cache
// refresh, this file's own tests) keeps compiling and reading exactly as
// before with no sent-time to report; a caller that tracks per-lane send
// times passes exactly one.
func ReadLaneTail(transcriptPath string, maxBytes int, maxTurns int, now time.Time, sentAt ...time.Time) (LaneTail, error) {
	var sent time.Time
	if len(sentAt) > 0 {
		sent = sentAt[0]
	}
	if maxBytes <= 0 {
		maxBytes = DefaultTailMaxBytes
	}
	if maxTurns <= 0 {
		maxTurns = DefaultTailTurns
	}

	f, err := os.Open(transcriptPath)
	if err != nil {
		return LaneTail{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return LaneTail{}, err
	}

	truncated := info.Size() > int64(maxBytes)
	if truncated {
		if _, err := f.Seek(info.Size()-int64(maxBytes), io.SeekStart); err != nil {
			return LaneTail{}, err
		}
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), scannerMaxLine)

	var records []rawRecord
	malformed := 0
	discardNext := truncated
	for scanner.Scan() {
		line := scanner.Bytes()
		if discardNext {
			discardNext = false
			continue
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var rec rawRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			malformed++
			continue
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return LaneTail{}, err
	}

	state := ClassifyState(records, now, sent)
	mode, model, messages := deriveMeta(records)
	turns, turnsTrimmed := buildTurns(records, maxTurns)

	return LaneTail{
		Transcript:     transcriptPath,
		State:          state.State,
		StateReason:    state.Reason,
		LastWrite:      info.ModTime(),
		LastTurn:       state.LastTurn,
		OpenTurn:       state.OpenTurn,
		PendingAgents:  state.PendingAgents,
		Mode:           mode,
		Model:          model,
		Messages:       messages,
		TurnDuration:   state.TurnDuration,
		Turns:          turns,
		MalformedLines: malformed,
		Truncated:      truncated || turnsTrimmed,
		AnsweredAt:     state.AnsweredAt,
		AwaySummary:    latestAwaySummary(records),
	}, nil
}

// stateResult is ClassifyState's return value.
type stateResult struct {
	State         string
	Reason        string
	LastTurn      time.Time
	OpenTurn      bool
	PendingAgents int
	TurnDuration  time.Duration

	// AnsweredAt is set only on the two "waiting held, then answered"
	// transitions item 5 adds - see LaneTail.AnsweredAt's own doc comment.
	AnsweredAt time.Time
}

// ClassifyState applies the state rule ruled by the design leg (2 Sep,
// DECISIONS.md slice 1), amended by item 5 (WAITING HELD, cockpit-pane
// modalities research, 3 Sep): walk backwards past
// untimestampedBookkeepingTypes to the last timestamped record. If it is
// type system/turn_duration the turn is CLOSED: pendingBackgroundAgentCount
// > 0 and closed 10 minutes ago or less -> working; pending > 0 and closed
// over 10 minutes ago -> stalled (same age cap as the open-turn branch, so a
// closed turn with a pending agent never reads working indefinitely); no
// pending agents -> "waiting on you", HELD there by no time cap at all
// (DEFECT, the old 30-minute decay: a lane that answered while the owner
// was in a meeting read identical to one that finished and banked, DECISIONS.md
// "Leaving and coming back") UNLESS sentAt (the cockpit's own last-send time
// for this lane, the caller's to track) is newer than the close, in which
// case it reads idle, answered by the cockpit send. Any other record type is
// an OPEN turn: an owner-authored reply (not harness-tagged) whose own
// immediately preceding anchor-eligible record was exactly that kind of
// pending-free close also reads idle, answered - "a newer owner turn
// appears in that transcript (he answered, from any surface)"; every other
// open turn is unchanged: written within 10 minutes -> working; over 10
// minutes -> stalled.
func ClassifyState(records []rawRecord, now time.Time, sentAt time.Time) stateResult {
	anchorIdx, ok := lastTimestampedRecordIndex(records)
	if !ok {
		return stateResult{State: StateIdle, Reason: "no timestamped record found in the tail window"}
	}
	anchor := records[anchorIdx]
	anchorAt, err := parseTimestamp(anchor.Timestamp)
	if err != nil {
		return stateResult{State: StateIdle, Reason: "no timestamped record found in the tail window"}
	}
	age := now.Sub(anchorAt)

	if anchor.Type == "system" && anchor.Subtype == "turn_duration" {
		duration := time.Duration(anchor.DurationMs) * time.Millisecond
		pending := anchor.PendingBackgroundAgentCount
		switch {
		case pending > 0 && age > 10*time.Minute:
			// The dead-lane-resume fix (3 Sep incident): a pending background
			// agent never ages the lane past 10 minutes with no newer write -
			// mirrors the open-turn branch's own 10-minute cap below, so a
			// lane whose builder never checked back in (or whose tmux server
			// died taking the builder with it) reads stalled, not working,
			// once nothing has written to the transcript in that long.
			return stateResult{
				State:         StateStalled,
				Reason:        fmt.Sprintf("turn closed %s ago with %d background agent(s) still pending, over 10m with no write", roundAge(age), pending),
				LastTurn:      anchorAt,
				PendingAgents: pending,
				TurnDuration:  duration,
			}
		case pending > 0:
			return stateResult{
				State:         StateWorking,
				Reason:        fmt.Sprintf("turn closed %s ago with %d background agent(s) still pending", roundAge(age), pending),
				LastTurn:      anchorAt,
				PendingAgents: pending,
				TurnDuration:  duration,
			}
		case !sentAt.IsZero() && sentAt.After(anchorAt):
			// Item 5's second trigger: the cockpit itself sent into this lane
			// after the close, but the transcript has not caught up with a
			// write reflecting it yet - waiting on you is answered, not by a
			// transcript record but by the send the caller already knows
			// about (sentAt), so it must not keep nagging until the
			// transcript proves it independently.
			return stateResult{
				State:        StateIdle,
				Reason:       fmt.Sprintf("answered %s ago (sent from the cockpit)", roundAge(now.Sub(sentAt))),
				LastTurn:     anchorAt,
				TurnDuration: duration,
				AnsweredAt:   sentAt,
			}
		default:
			// HELD: no time cap. A closed turn with no pending agents and no
			// answer yet stays "waiting on you" no matter how old - the owner
			// has not looked, and an elapsed clock is not evidence he has.
			return stateResult{
				State:        StateWaitingYou,
				Reason:       fmt.Sprintf("turn closed %s ago, no pending agents", roundAge(age)),
				LastTurn:     anchorAt,
				TurnDuration: duration,
			}
		}
	}

	// OPEN branch: the anchor is not a closed turn_duration record. Item 5's
	// first trigger - an owner-authored reply that itself follows a
	// pending-free close - reads idle/answered rather than falling through
	// to the ordinary working/stalled age split below, which would otherwise
	// read the owner's OWN reply as though it were assistant activity.
	if isOwnerAuthoredRecord(anchor) && anchorIdx > 0 {
		if prevIdx, ok := lastTimestampedRecordIndex(records[:anchorIdx]); ok {
			prev := records[prevIdx]
			if prev.Type == "system" && prev.Subtype == "turn_duration" && prev.PendingBackgroundAgentCount == 0 {
				return stateResult{
					State:      StateIdle,
					Reason:     fmt.Sprintf("answered %s ago", roundAge(age)),
					LastTurn:   anchorAt,
					AnsweredAt: anchorAt,
				}
			}
		}
	}

	if age <= 10*time.Minute {
		return stateResult{
			State:    StateWorking,
			Reason:   fmt.Sprintf("open turn last written %s ago", roundAge(age)),
			LastTurn: anchorAt,
			OpenTurn: true,
		}
	}
	return stateResult{
		State:    StateStalled,
		Reason:   fmt.Sprintf("open turn last written %s ago, over 10m with no close", roundAge(age)),
		LastTurn: anchorAt,
		OpenTurn: true,
	}
}

// isOwnerAuthoredRecord reports whether r is a genuine owner-typed message -
// a "user" record whose message content is a plain string that is NOT one of
// harnessRecordTags (isHarnessTaggedUserRecord's own negation, named
// separately here since ClassifyState's own "a newer owner turn" rule reads
// more plainly as a positive test).
func isOwnerAuthoredRecord(r rawRecord) bool {
	if r.Type != "user" || r.Message == nil {
		return false
	}
	text, _, isString := parseContentItems(r.Message.Content)
	return isString && !isHarnessTaggedText(text)
}

// lastTimestampedRecordIndex walks records backward, skipping any record of
// an untimestampedBookkeepingTypes type, a harness-tagged user record that
// is not itself the very last record in the tail, or one that carries no
// parseable timestamp - and returns the POSITION of the first (i.e.
// chronologically last) one found. Item 5's owner-turn transition needs the
// anchor's position, not just its value, so it can search the slice
// strictly before it (records[:anchorIdx]) for the record the owner's reply
// is itself answering, using this exact same walk a second time.
//
// DEFECT 1's anchor choice: a harness-tagged user record (task-notification,
// system-reminder, etc.) is skipped like any other bookkeeping record when
// something else follows it, exactly the away_summary/stop_hook_summary
// treatment above - it never interrupts the search for the real last
// conversational record. But when the tagged record IS the last record in
// the tail (nothing follows it - the live case this rule was written for: a
// task-notification landing after the owner's real last turn), it still
// counts as the anchor, read as an open turn on its own timestamp. The
// alternative - never letting a tagged record anchor at all - would leave a
// lane that just received a notification with no anchor whatsoever once
// every earlier record is bookkeeping too, which is a worse answer than
// treating the harness's own write to the transcript as evidence the lane
// is live, even though that write never renders as a Turn.
func lastTimestampedRecordIndex(records []rawRecord) (int, bool) {
	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]
		if untimestampedBookkeepingTypes[r.Type] {
			continue
		}
		if r.Type == "system" && r.Subtype != "turn_duration" {
			// away_summary, stop_hook_summary, local_command: harness notes,
			// not conversation; they never open or close a turn.
			continue
		}
		if isHarnessTaggedUserRecord(r) && i != len(records)-1 {
			continue
		}
		if strings.TrimSpace(r.Timestamp) == "" {
			continue
		}
		if _, err := parseTimestamp(r.Timestamp); err != nil {
			continue
		}
		return i, true
	}
	return -1, false
}

// parseTimestamp parses a transcript record's timestamp field, which is
// always RFC 3339 with a "Z" suffix and millisecond fraction in practice
// (e.g. "2026-09-02T18:34:33.207Z") - RFC3339Nano accepts that fraction
// being present or absent.
func parseTimestamp(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

// roundAge renders a duration the way the header line's reason wants it:
// whole seconds under a minute, whole minutes under an hour, hours and
// minutes beyond that.
func roundAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		h := int(d.Hours())
		m := int(d.Minutes()) - h*60
		return fmt.Sprintf("%dh%dm", h, m)
	}
}

// latestAwaySummary scans the whole tail window (not just the
// classification anchor - lastTimestampedRecordIndex skips every
// away_summary record outright, item 4's own DEFECT) for the newest
// system/away_summary record and returns its content and timestamp. Records
// with an unparseable timestamp are skipped rather than aborting the scan,
// so a single malformed away_summary can never hide an older, good one.
// Zero (At.IsZero()) when the window holds none.
func latestAwaySummary(records []rawRecord) AwaySummary {
	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]
		if r.Type != "system" || r.Subtype != "away_summary" {
			continue
		}
		at, err := parseTimestamp(r.Timestamp)
		if err != nil {
			continue
		}
		return AwaySummary{Text: r.Content, At: at}
	}
	return AwaySummary{}
}

// deriveMeta scans the whole tail window (not just the classification
// anchor) for the most recent mode, model and turn_duration message count,
// so a lane mid-way through an open turn still reports the model and
// message count its last closed turn established.
func deriveMeta(records []rawRecord) (mode, model string, messages int) {
	for _, r := range records {
		switch {
		case r.Type == "mode" && r.Mode != "":
			mode = r.Mode
		case r.Type == "assistant" && r.Message != nil && r.Message.Model != "":
			model = r.Message.Model
		case r.Type == "system" && r.Subtype == "turn_duration":
			messages = r.MessageCount
		}
	}
	return mode, model, messages
}

// parseContentItems decodes a message's .content field, which is either a
// plain string (an owner turn) or an array of content items (assistant
// prose/tool_use, or a user record's tool_result plumbing - never an owner
// turn). isString distinguishes the two so a tool_result-only user record
// is never mistaken for something the owner typed.
func parseContentItems(raw json.RawMessage) (text string, items []rawContentItem, isString bool) {
	if len(raw) == 0 {
		return "", nil, false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil, true
	}
	var arr []rawContentItem
	if err := json.Unmarshal(raw, &arr); err == nil {
		return "", arr, false
	}
	return "", nil, false
}

// toolOutcome is one tool_result matched back to its tool_use by ID.
type toolOutcome struct {
	Result string
	At     time.Time
}

// indexToolResults builds a tool_use_id -> outcome map from every
// tool_result in records, so buildTurns can attach the eventual result to
// the tool_use call that produced it regardless of how far apart the two
// records fall. toolDenialKind on the user record marks denied; is_error
// on the content item marks error; anything else that resolved is ok.
func indexToolResults(records []rawRecord) map[string]toolOutcome {
	out := make(map[string]toolOutcome)
	for _, r := range records {
		if r.Type != "user" || r.Message == nil {
			continue
		}
		_, items, isString := parseContentItems(r.Message.Content)
		if isString {
			continue
		}
		at, _ := parseTimestamp(r.Timestamp)
		for _, item := range items {
			if item.Type != "tool_result" || item.ToolUseID == "" {
				continue
			}
			result := ResultOK
			switch {
			case r.ToolDenialKind != "":
				result = ResultDenied
			case item.IsError:
				result = ResultError
			}
			out[item.ToolUseID] = toolOutcome{Result: result, At: at}
		}
	}
	return out
}

// toolSummaryMaxLen is the tool line summary's own outer bound - every
// branch below is truncated ansi-aware to this width before it is ever
// handed to a Turn, regardless of which rule produced it.
const toolSummaryMaxLen = 100

// toolSummaryFieldMaxLen is the narrower bound the Bash-command and
// fallback-compact-rendering branches apply to their own field, ahead of
// the outer toolSummaryMaxLen truncation above.
const toolSummaryFieldMaxLen = 80

// toolSummary renders a tool_use call's one-line summary (defect 2, seen on
// a real Write turn: its raw input JSON - a whole file's content - printed
// as the summary). The rule, in order: the input's "description" field
// when present; else, for Write/Edit/Read, the file path's last two path
// segments; else, for Bash, the command's first line; else the first line
// of a compact (whitespace-stripped) rendering of the input - never the
// raw input itself. Every branch is truncated ansi-aware to
// toolSummaryMaxLen before it reaches a Turn.
func toolSummary(tool string, raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ansi.Truncate(firstLine(compactJSON(raw)), toolSummaryMaxLen, "…")
	}

	if descRaw, ok := fields["description"]; ok {
		var desc string
		if json.Unmarshal(descRaw, &desc) == nil && desc != "" {
			return ansi.Truncate(firstLine(desc), toolSummaryMaxLen, "…")
		}
	}

	switch tool {
	case "Write", "Edit", "Read":
		if pathRaw, ok := fields["file_path"]; ok {
			var path string
			if json.Unmarshal(pathRaw, &path) == nil && path != "" {
				return ansi.Truncate(lastTwoPathSegments(path), toolSummaryMaxLen, "…")
			}
		}
	case "Bash":
		if cmdRaw, ok := fields["command"]; ok {
			var cmd string
			if json.Unmarshal(cmdRaw, &cmd) == nil && cmd != "" {
				return ansi.Truncate(firstLine(cmd), toolSummaryFieldMaxLen, "…")
			}
		}
	}

	return ansi.Truncate(firstLine(compactJSON(raw)), toolSummaryFieldMaxLen, "…")
}

// lastTwoPathSegments returns path's final two "/"-separated segments (or
// fewer, if path has fewer than two) - enough to identify the file without
// the long, mostly-shared prefix a full absolute path carries in this
// workspace (/Users/allencoates/projects/Clarity/...).
func lastTwoPathSegments(path string) string {
	path = filepath.ToSlash(strings.TrimRight(path, "/"))
	parts := strings.Split(path, "/")
	if len(parts) <= 2 {
		return strings.Join(parts, "/")
	}
	return strings.Join(parts[len(parts)-2:], "/")
}

// compactJSON strips insignificant whitespace from raw, so a fallback
// summary is always a single line even when the source input was pretty-
// printed - never a byte-for-byte dump of a possibly large input.
func compactJSON(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}

// firstLine returns s's first line, trimmed.
func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

// buildTurns walks records in file order (oldest first) and renders each
// owner message, assistant text block and tool_use call as one Turn,
// dropping thinking blocks and tool_result plumbing (the latter is folded
// into its matching tool_use Turn via indexToolResults instead of
// rendering as its own row). The result is trimmed to the last maxTurns;
// trimmed reports whether that trim actually dropped anything, so a caller
// (ReadLaneTail) can tell its own Truncated flag apart from "the whole
// session fit".
func buildTurns(records []rawRecord, maxTurns int) (turns []Turn, trimmed bool) {
	outcomes := indexToolResults(records)
	for _, r := range records {
		if r.Message == nil {
			continue
		}
		at, _ := parseTimestamp(r.Timestamp)

		switch r.Type {
		case "user":
			text, _, isString := parseContentItems(r.Message.Content)
			// DEFECT 1: a string-content user record is an owner turn only
			// when it is not one of the harness's own injected notes
			// (task-notification, system-reminder, etc.) - those are
			// harness bookkeeping, skipped from the turn stream entirely,
			// never rendered as if the owner had typed them.
			if isString && !isHarnessTaggedText(text) {
				turns = append(turns, Turn{Kind: TurnOwner, At: at, Text: text})
			}

		case "assistant":
			_, items, _ := parseContentItems(r.Message.Content)
			for _, item := range items {
				switch item.Type {
				case "text":
					turns = append(turns, Turn{Kind: TurnAssistant, At: at, Text: item.Text})
				case "tool_use":
					turn := Turn{
						Kind:    TurnTool,
						At:      at,
						Tool:    item.Name,
						Summary: toolSummary(item.Name, item.Input),
						Result:  ResultRunning,
					}
					if outcome, ok := outcomes[item.ID]; ok {
						turn.Result = outcome.Result
						if !at.IsZero() && !outcome.At.IsZero() && outcome.At.After(at) {
							turn.Duration = outcome.At.Sub(at)
						}
					}
					turns = append(turns, turn)
				}
			}
		}
	}

	if maxTurns > 0 && len(turns) > maxTurns {
		turns = turns[len(turns)-maxTurns:]
		trimmed = true
	}
	return turns, trimmed
}

// turnLabel renders a Turn's kind as the fixed prefix the header/turn line
// format uses: YOU, CLAUDE, or the tool marker "▪ <tool>".
func turnLabel(t Turn) string {
	switch t.Kind {
	case TurnOwner:
		return "YOU"
	case TurnAssistant:
		return "CLAUDE"
	case TurnTool:
		return "▪ " + t.Tool
	default:
		return t.Kind
	}
}

// turnBody returns the text a turn line renders: prose for owner/assistant,
// the tool call's summary for a tool turn.
func turnBody(t Turn) string {
	if t.Kind == TurnTool {
		return t.Summary
	}
	return t.Text
}

// collapseWhitespace folds any run of whitespace (including newlines) into
// a single space, so multi-line prose renders as one wrappable line.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// wrapWords greedily wraps words into lines no wider than width (a single
// word longer than width still gets its own line, unbroken).
func wrapWords(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	lines := make([]string, 0, 4)
	cur := words[0]
	for _, w := range words[1:] {
		if len(cur)+1+len(w) > width {
			lines = append(lines, cur)
			cur = w
		} else {
			cur += " " + w
		}
	}
	return append(lines, cur)
}

// RenderHeaderLine renders a LaneTail's header line: "<lane>  <state>  last
// turn <hh:mm:ss>  last write <hh:mm:ss>  <reason>" - lane is passed
// separately from lt.Lane so a caller can print the bare name the owner
// typed even when lt.Lane carries the encoded discovery form.
func RenderHeaderLine(lane string, lt LaneTail) string {
	return fmt.Sprintf("%s  %s  last turn %s  last write %s  %s",
		lane, lt.State, lt.LastTurn.Local().Format("15:04:05"), lt.LastWrite.Local().Format("15:04:05"), lt.StateReason)
}

// RenderTurnLines renders one Turn as "hh:mm:ss  YOU|CLAUDE|▪ <tool>  <text
// or summary>  [<result> <duration>]", word-wrapped to width columns with
// continuation lines hanging-indented under the text column.
func RenderTurnLines(t Turn, width int) []string {
	label := turnLabel(t)
	prefix := fmt.Sprintf("%s  %s  ", t.At.Local().Format("15:04:05"), label)

	body := collapseWhitespace(turnBody(t))
	if t.Kind == TurnTool {
		result := t.Result
		if t.Duration > 0 {
			result = fmt.Sprintf("%s %s", result, t.Duration.Round(time.Second))
		}
		body = strings.TrimSpace(body + fmt.Sprintf("  [%s]", result))
	}

	avail := width - len(prefix)
	if avail < 20 {
		avail = 20
	}
	wrapped := wrapWords(body, avail)
	if len(wrapped) == 0 {
		return []string{strings.TrimRight(prefix, " ")}
	}

	indent := strings.Repeat(" ", len(prefix))
	lines := make([]string, len(wrapped))
	for i, w := range wrapped {
		if i == 0 {
			lines[i] = prefix + w
		} else {
			lines[i] = indent + w
		}
	}
	return lines
}
