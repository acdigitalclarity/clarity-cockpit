// Package clarity: this file reads the fleet's already-ranked "Needs you"
// queue - the markdown table scripts/fleet_triage_rank.py's --out writes
// (render_queue_markdown() there: "| rank | class | source | title |") -
// and turns it into the plain-words lines the instance list shows under
// "Needs you". This file never classifies or ranks anything on its own
// authority; fleet_triage_rank.py already did that (fleet-triage-reader.md
// classifies, fleet_triage_rank.py sorts). Read once per refresh tick,
// never polled in a loop - see the UI's feed tick in app/app.go.
package clarity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// FeedQueuePathEnvVar overrides the default queue file path. Set only for
// tests; the real CLI relies on the default.
const FeedQueuePathEnvVar = "CLARITY_FLEET_QUEUE_PATH"

// DefaultFeedQueuePath is where the fleet's ranked triage queue lives on
// this machine (same workspace-root convention as scripts/fleet_dashboard.py's
// WS constant).
const DefaultFeedQueuePath = "/Users/allencoates/projects/Clarity/FLEET-QUEUE.md"

// classOrder mirrors fleet_triage_rank.py's RANK_ORDER: blocked-on-owner
// first. Applied defensively on read (the file arrives pre-ranked; this is
// a second, cheap guarantee rather than trust alone).
var classOrder = map[string]int{
	"blocked-on-owner": 0,
	"escalation":       1,
	"fyi":              2,
	"unclassified":     3,
}

func classRank(class string) int {
	if r, ok := classOrder[class]; ok {
		return r
	}
	return len(classOrder) // unknown class sorts last, never dropped
}

// FeedItem is one ranked queue row.
type FeedItem struct {
	Rank   int
	Class  string
	Source string
	Title  string
	Lane   string
}

// FeedAbsentError names the queue path that was not found, so the caller
// can render "feed: UNCONSTRUCTED - no queue at <path>" instead of a bare
// empty list - an absent queue is a distinct, reportable state, never the
// same thing as an empty one.
type FeedAbsentError struct{ Path string }

func (e *FeedAbsentError) Error() string {
	return fmt.Sprintf("no queue at %s", e.Path)
}

func feedQueuePath() string {
	if p := os.Getenv(FeedQueuePathEnvVar); p != "" {
		return p
	}
	return DefaultFeedQueuePath
}

// DefaultFeedPath exposes feedQueuePath() (env override or default) to
// callers outside this package (the UI's feed tick).
func DefaultFeedPath() string {
	return feedQueuePath()
}

// laneFromSource derives a lane name from a discovered source path
// (sessions/<lane>/STATUS.md or sessions/<lane>/TASKS.md, per
// fleet_triage_rank.py's discover_status_files()): the parent directory's
// base name. Falls back to the source string itself for an unexpected
// shape, so a line is never dropped for not matching the convention.
func laneFromSource(source string) string {
	base := filepath.Base(filepath.Dir(source))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return source
	}
	return base
}

// ParseQueueMarkdown parses the "| rank | class | source | title |" table
// scripts/fleet_triage_rank.py's render_queue_markdown() writes. Header
// and separator rows are skipped by shape (a separator row's cells are
// each "-"-only; the header row's first cell is literally "rank"), not by
// a fixed line count, so a queue with zero data rows parses to an empty,
// non-error slice rather than erroring.
func ParseQueueMarkdown(data []byte) ([]FeedItem, error) {
	lines := strings.Split(string(data), "\n")
	var items []FeedItem
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "|") {
			continue
		}
		cells := splitRow(line)
		if len(cells) != 4 {
			continue
		}
		if isSeparatorRow(cells) || strings.EqualFold(cells[0], "rank") {
			continue
		}
		rankNum, _ := strconv.Atoi(cells[0])
		item := FeedItem{
			Rank:   rankNum,
			Class:  cells[1],
			Source: cells[2],
			Title:  cells[3],
		}
		item.Lane = laneFromSource(item.Source)
		items = append(items, item)
	}
	return items, nil
}

func splitRow(line string) []string {
	trimmed := strings.Trim(line, "|")
	parts := strings.Split(trimmed, "|")
	cells := make([]string, len(parts))
	for i, p := range parts {
		cells[i] = strings.TrimSpace(p)
	}
	return cells
}

func isSeparatorRow(cells []string) bool {
	for _, c := range cells {
		if strings.Trim(c, "-") != "" {
			return false
		}
	}
	return true
}

// RankItems stable-sorts by class (blocked-on-owner first), mirroring
// fleet_triage_rank.py's rank(). Ties keep arrival order (sort.SliceStable).
// Returns a new slice; the input is never mutated.
func RankItems(items []FeedItem) []FeedItem {
	out := make([]FeedItem, len(items))
	copy(out, items)
	sort.SliceStable(out, func(i, j int) bool {
		return classRank(out[i].Class) < classRank(out[j].Class)
	})
	return out
}

// LoadFeed reads and parses the queue file at path. A missing file returns
// *FeedAbsentError, distinguishable from a genuine parse/read failure.
func LoadFeed(path string) ([]FeedItem, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, &FeedAbsentError{Path: path}
		}
		return nil, err
	}
	return ParseQueueMarkdown(data)
}

// FeedLine renders one ranked item as the "lane name and one plain-words
// line" the brief asks for.
func FeedLine(item FeedItem) string {
	return fmt.Sprintf("%s - %s", item.Lane, item.Title)
}

// NeedsYou renders the whole "Needs you" block: the top n ranked entries
// from the queue at path, or the UNCONSTRUCTED line when the queue is
// absent or unparseable - never a silently empty list, per the brief. This
// does exactly one read of path per call; the caller (a periodic UI tick)
// decides the cadence, this function never loops or polls on its own.
func NeedsYou(path string, n int) []string {
	items, status := RankedNeedsYou(path, n)
	if status != "" {
		return []string{status}
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, FeedLine(item))
	}
	return lines
}

// RankedNeedsYou is NeedsYou's structured counterpart (board #280's own
// slice 5): the ranked FeedItems themselves, capped to n, for a caller (the
// list's own selectable Needs-you rows) that needs each row's fields, not
// just its rendered line. status carries the same absent/parse-error/empty
// text NeedsYou renders as its sole line, "" when the queue read cleanly -
// items is nil whenever status is non-empty, so a caller never has to
// juggle both a status line AND a stale/partial item slice at once.
func RankedNeedsYou(path string, n int) (items []FeedItem, status string) {
	loaded, err := LoadFeed(path)
	if err != nil {
		var absent *FeedAbsentError
		if errors.As(err, &absent) {
			return nil, fmt.Sprintf("feed: UNCONSTRUCTED - no queue at %s", absent.Path)
		}
		return nil, fmt.Sprintf("feed: UNCONSTRUCTED - could not parse queue at %s: %v", path, err)
	}
	ranked := RankItems(loaded)
	if len(ranked) == 0 {
		return nil, "feed: queue is empty"
	}
	if n > 0 && len(ranked) > n {
		ranked = ranked[:n]
	}
	return ranked, ""
}

// boardIssueSourceRe matches a feed item's Source field when it names a
// board issue directly - fleet_queue_build.py's board_rows() writes
// "#<number>" - as opposed to a lane-file source
// ("sessions/lane/STATUS.md:12", that same script's lane_rows()), which
// carries no issue number to fetch.
var boardIssueSourceRe = regexp.MustCompile(`^#(\d+)$`)

// BoardIssueNumber extracts the issue number from a feed item's Source
// field. ok is false when source is not a bare "#<n>" board reference (a
// lane-file source, or anything else) - the Needs-you tab's own signal for
// "there is no board issue here to fetch a recommendation from".
func BoardIssueNumber(source string) (int, bool) {
	m := boardIssueSourceRe.FindStringSubmatch(source)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// builtLinePrefix is the literal prefix scripts/fleet_queue_build.py writes
// as FLEET-QUEUE.md's first line: "built: <ISO time> source: board+lanes"
// (board #280). ParseQueueMarkdown above already skips this line when
// parsing rows - it does not start with "|" - so no change was needed there;
// this half reads it back out on its own.
const builtLinePrefix = "built: "

// builtLineSourceSep marks where the ISO timestamp ends and the "source:"
// tag begins on the built: line.
const builtLineSourceSep = " source:"

// StaleAfter is how old the built: timestamp may be before the feed reports
// itself stale. The builder's timer fires every 5 minutes
// (docs/ops/launchd/com.digitalclarity.fleet-queue.plist), so 10 minutes is
// two missed ticks, not one - a single slow tick is not yet a problem worth
// surfacing.
const StaleAfter = 10 * time.Minute

// ParseBuiltAt extracts the ISO-8601 (RFC 3339) timestamp from a
// FLEET-QUEUE.md's first line. Returns ok=false when the line is absent,
// does not carry the "built: " prefix, or does not parse as a timestamp -
// a hand-edited or legacy queue file (no builder timestamp) reports the
// same "line absent" state as a genuinely missing file, so the caller never
// has to special-case which.
func ParseBuiltAt(data []byte) (t time.Time, ok bool) {
	first := string(data)
	if nl := strings.IndexByte(first, '\n'); nl >= 0 {
		first = first[:nl]
	}
	first = strings.TrimSpace(first)
	if !strings.HasPrefix(first, builtLinePrefix) {
		return time.Time{}, false
	}
	rest := strings.TrimPrefix(first, builtLinePrefix)
	tsStr := rest
	if idx := strings.Index(rest, builtLineSourceSep); idx >= 0 {
		tsStr = rest[:idx]
	}
	tsStr = strings.TrimSpace(tsStr)
	parsed, err := time.Parse(time.RFC3339, tsStr)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// FeedHeaderStatus reports the "Needs you" section's age qualifier against
// `now`: "" when the queue's built: timestamp is fresh (within StaleAfter),
// "STALE <N>m" when it is older, "UNCONSTRUCTED" when the queue file is
// missing or carries no built: line at all (never machinery-built, or a
// read error other than not-exist).
func FeedHeaderStatus(path string, now time.Time) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "UNCONSTRUCTED"
	}
	builtAt, ok := ParseBuiltAt(data)
	if !ok {
		return "UNCONSTRUCTED"
	}
	age := now.Sub(builtAt)
	if age > StaleAfter {
		return fmt.Sprintf("STALE %dm", int(age.Minutes()))
	}
	return ""
}

// NeedsYouTitle renders the "Needs you" section title with its age
// qualifier appended in parentheses when the feed is not fresh:
// "Needs you (STALE 23m)" / "Needs you (UNCONSTRUCTED)" / plain
// "Needs you" when the last build is within StaleAfter.
func NeedsYouTitle(path string, now time.Time) string {
	status := FeedHeaderStatus(path, now)
	if status == "" {
		return "Needs you"
	}
	return fmt.Sprintf("Needs you (%s)", status)
}
