// Package clarity: this file derives the fleet's "external" lanes - live
// Claude Code sessions on this Mac that were NOT started through the
// cockpit (`clarity new`/`clarity open` before cs-clarity existed, or a
// plain `claude` in a bare Terminal tab) and so have no Claude Squad
// instance/tmux session tracking them. It is a Go port of
// scripts/fleet_dashboard.py's lane-discovery loop in main() - same glob,
// same 90-minute liveness cutoff, same "memory"/"subagents" exclusions,
// same encoded-cwd-prefix stripping, same newest-transcript-per-lane
// dedupe - so `cs-clarity discover` never disagrees with the fleet
// dashboard about which lanes are live (board #169's duplicated-logic
// lesson, applied here the same way gauge.go applies it to the fill number).
package clarity

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ExternalLiveWindow mirrors fleet_dashboard.py's hardcoded 5400-second
// (90-minute) cutoff on a lane's newest transcript mtime.
const ExternalLiveWindow = 90 * time.Minute

// ExternalLane is one lane discovered directly from ~/.claude/projects
// transcripts rather than from Claude Squad's own instance storage. External
// rows can be messaged (the brief's requirement) but never attached or
// killed - there is no tracked tmux session or git worktree behind them.
type ExternalLane struct {
	Name           string
	TranscriptPath string
	LastWrite      time.Time
	Fill           Fill
	FillOK         bool
}

// externalTranscriptRow is the pre-dedupe intermediate the discovery loop
// builds before collapsing to one row per lane name.
type externalTranscriptRow struct {
	lane string
	path string
	mod  time.Time
}

// laneNameFromTranscriptDir mirrors fleet_dashboard.py's lane derivation
// exactly: os.path.basename(os.path.dirname(f)).replace(A, "").replace(B, "")
// where A is the encoded prefix for a project under this workspace and B is
// the encoded prefix for a project directly under the home directory. Order
// matters - A is checked (and stripped) before B, same as the Python source,
// so a workspace-rooted lane is never partially stripped by B first.
func laneNameFromTranscriptDir(dir string) string {
	lane := filepath.Base(dir)
	lane = strings.ReplaceAll(lane, "-Users-allencoates-projects-Clarity-", "")
	lane = strings.ReplaceAll(lane, "-Users-allencoates-", "")
	return lane
}

// TrackedExclusionNames returns every external-lane name a tracked Claude
// Squad instance titled `title` could plausibly correspond to, for building
// the excludeTitles set DiscoverExternalLanes takes. A clarity-attach
// instance's Title is the bare Clarity session lane name (e.g.
// "ways-of-working"), but that same lane's transcript directory encodes its
// full path under sessions/ - so laneNameFromTranscriptDir derives
// "sessions-ways-of-working" for it, not the bare title (fleet_dashboard.py
// keeps that "sessions-"/"repos-"/etc. prefix deliberately, to disambiguate
// what kind of directory a lane lives in across the whole ecosystem - see
// this file's doc comment). Without this, every already-tracked Clarity
// session lane would ALSO show up as its own external row the moment it
// went live, which is exactly the double-listing the brief's "never also
// shown as an external row" requirement rules out. This covers the
// Clarity-session-lane case (clarity new/open via clarity-attach); a
// tracked instance backed by an ordinary git worktree elsewhere in the
// ecosystem is a known gap this does not close.
func TrackedExclusionNames(title string) []string {
	return []string{title, "sessions-" + title}
}

// MatchesQueriedLane reports whether ext is the lane a caller meant by
// queried - either the exact discovered name, or queried with the same
// "sessions-" prefix DiscoverExternalLanes' names carry for anything under
// the Clarity sessions/ tree (see TrackedExclusionNames). This lets `cs-
// clarity msg <lane> ...` and the bash `clarity msg <lane> ...` wrapper
// accept the same bare lane name a human uses everywhere else in this
// ecosystem (clarity attach <lane>, sessions/<lane>/), rather than forcing
// the fleet_dashboard.py-style "sessions-<lane>" form onto the command line.
func MatchesQueriedLane(ext ExternalLane, queried string) bool {
	return ext.Name == queried || ext.Name == "sessions-"+queried
}

// DiscoverExternalLanes derives the list of live external lanes: every
// ~/.claude/projects/<encoded>/*.jsonl transcript whose mtime is within
// ExternalLiveWindow, minus any path containing "memory", minus any lane
// name starting with "subagents", minus any lane name already present in
// excludeTitles (a lane Claude Squad already tracks as an instance is never
// ALSO shown as an external row) - collapsed to the single newest
// transcript per lane name, sorted newest-first. A nil or empty
// excludeTitles is valid (no exclusions).
func DiscoverExternalLanes(excludeTitles map[string]bool) ([]ExternalLane, error) {
	pattern := filepath.Join(claudeProjectsRoot(), "*", "*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var rows []externalTranscriptRow
	for _, p := range matches {
		if strings.Contains(p, "memory") {
			continue
		}
		info, statErr := os.Stat(p)
		if statErr != nil {
			continue
		}
		if now.Sub(info.ModTime()) > ExternalLiveWindow {
			continue
		}
		lane := laneNameFromTranscriptDir(filepath.Dir(p))
		if strings.HasPrefix(lane, "subagents") {
			continue
		}
		rows = append(rows, externalTranscriptRow{lane: lane, path: p, mod: info.ModTime()})
	}

	// Newest first, so the per-lane dedupe below keeps each lane's newest
	// transcript - same ordering fleet_dashboard.py's rows.sort() + "seen"
	// set produces.
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].mod.After(rows[j].mod) })

	seen := make(map[string]bool, len(rows))
	out := make([]ExternalLane, 0, len(rows))
	for _, r := range rows {
		if seen[r.lane] {
			continue
		}
		seen[r.lane] = true
		if excludeTitles[r.lane] {
			continue
		}
		fill, ok := ReadFill(r.path, "")
		out = append(out, ExternalLane{
			Name:           r.lane,
			TranscriptPath: r.path,
			LastWrite:      r.mod,
			Fill:           fill,
			FillOK:         ok,
		})
	}
	return out, nil
}
