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
	"sort"
	"strconv"
	"strings"
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
	items, err := LoadFeed(path)
	if err != nil {
		var absent *FeedAbsentError
		if errors.As(err, &absent) {
			return []string{fmt.Sprintf("feed: UNCONSTRUCTED - no queue at %s", absent.Path)}
		}
		return []string{fmt.Sprintf("feed: UNCONSTRUCTED - could not parse queue at %s: %v", path, err)}
	}
	ranked := RankItems(items)
	if len(ranked) == 0 {
		return []string{"feed: queue is empty"}
	}
	if n > 0 && len(ranked) > n {
		ranked = ranked[:n]
	}
	lines := make([]string, 0, len(ranked))
	for _, item := range ranked {
		lines = append(lines, FeedLine(item))
	}
	return lines
}
