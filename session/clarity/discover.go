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
	"bufio"
	"bytes"
	"encoding/json"
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
	// Name is the DISPLAYED lane name - laneNameFromTranscriptDir's result
	// with a leading "sessions-" stripped (defect 3: the prefix wasted nine
	// columns on every row and forced truncation that would otherwise not
	// be needed). Key below carries the un-stripped form for matching.
	Name string
	// Key is the full, un-stripped name laneNameFromTranscriptDir derived -
	// what MatchesQueriedLane/TranscriptForLane actually match against, so
	// stripping the prefix for display never changes which lane a caller's
	// `cs-clarity msg <lane>`/lane-tail argument resolves to.
	Key            string
	TranscriptPath string
	LastWrite      time.Time
	Fill           Fill
	FillOK         bool

	// State, LastTurn and StateOK are the lane's clarity.ReadLaneTail
	// classification (working/waiting on you/idle/stalled) - populated by
	// the caller (app.go's feedTickMsg handler, via a LaneTailCache) on the
	// same tick as Fill above, never by DiscoverExternalLanes itself, so a
	// plain CLI caller (cs-clarity discover) that has no cache still works
	// exactly as before with these left at their zero values.
	State    string
	LastTurn time.Time
	StateOK  bool
}

// externalTranscriptRow is the pre-dedupe intermediate the discovery loop
// builds before collapsing to one row per lane name.
type externalTranscriptRow struct {
	lane string
	path string
	mod  time.Time
	cwd  string // "" when the transcript's own cwd field could not be read
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

// TrackedExclusionPaths builds the excludeDirs set DiscoverExternalLanes
// takes, from the working-directory paths of every tracked Claude Squad
// instance (session.Instance.Path). Matching by path rather than by any
// name derived from a transcript directory's encoding is the DEDUPE
// defect's root-cause fix (board fit gate, 2 Sep): a tracked instance
// titled "andy.e-bid" and its transcript directory
// "...-sessions-andy-e-bid" disagree on whether the dot survives encoding,
// so a name-derived exclusion set silently missed it while the lane's own
// working directory - recorded verbatim in the transcript's "cwd" field -
// never does. filepath.Clean normalises a trailing slash; an empty path is
// skipped rather than excluding every lane with an empty cwd.
func TrackedExclusionPaths(paths []string) map[string]bool {
	out := make(map[string]bool, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		out[filepath.Clean(p)] = true
	}
	return out
}

// MatchesQueriedLane reports whether ext is the lane a caller meant by
// queried - either ext's own displayed Name (already stripped of any
// "sessions-" prefix, defect 3), its full un-stripped Key, or queried with
// the same "sessions-" prefix DiscoverExternalLanes' keys carry for
// anything under the Clarity sessions/ tree. This lets `cs-clarity msg
// <lane> ...` and the bash `clarity msg <lane> ...` wrapper accept the same
// bare lane name a human uses everywhere else in this ecosystem (clarity
// attach <lane>, sessions/<lane>/), rather than forcing the
// fleet_dashboard.py-style "sessions-<lane>" form onto the command line.
func MatchesQueriedLane(ext ExternalLane, queried string) bool {
	return ext.Name == queried || ext.Key == queried || ext.Key == "sessions-"+queried
}

// cwdScanMaxLines and cwdScanMaxBytes bound readTranscriptCwd's forward
// scan: a real transcript's "cwd" field appears on its first user/
// assistant/system/attachment record, observed within the first ~50 lines
// in practice (confirmed against a live transcript before this file was
// written) - these budgets are comfortably wider than that without paying
// to scan a whole multi-megabyte transcript when no cwd is ever found.
const cwdScanMaxLines = 500
const cwdScanMaxBytes = 256 * 1024

// readTranscriptCwd reads forward from the start of transcriptPath looking
// for the first record carrying a non-empty top-level "cwd" field - the
// lane's actual working directory, exactly as Claude Code recorded it, and
// so immune to any directory-name-encoding mismatch (see
// TrackedExclusionPaths). ok is false when no such record turns up inside
// the scan budget.
func readTranscriptCwd(transcriptPath string) (cwd string, ok bool) {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return "", false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), scannerMaxLine)

	read := 0
	for line := 0; scanner.Scan() && line < cwdScanMaxLines && read < cwdScanMaxBytes; line++ {
		b := scanner.Bytes()
		read += len(b) + 1
		if len(bytes.TrimSpace(b)) == 0 {
			continue
		}
		var rec struct {
			Cwd string `json:"cwd"`
		}
		if err := json.Unmarshal(b, &rec); err != nil {
			continue
		}
		if rec.Cwd != "" {
			return rec.Cwd, true
		}
	}
	return "", false
}

// DiscoverExternalLanes derives the list of live external lanes: every
// ~/.claude/projects/<encoded>/*.jsonl transcript whose mtime is within
// ExternalLiveWindow, minus any path containing "memory", minus any lane
// name starting with "subagents", minus any lane whose transcript's own
// "cwd" field is already present in excludeDirs (see TrackedExclusionPaths
// - a lane Claude Squad already tracks as an instance is never ALSO shown
// as an external row) - collapsed to the single newest transcript per lane
// name, sorted newest-first. A nil or empty excludeDirs is valid (no
// exclusions).
func DiscoverExternalLanes(excludeDirs map[string]bool) ([]ExternalLane, error) {
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
		if cwd, ok := readTranscriptCwd(r.path); ok && excludeDirs[filepath.Clean(cwd)] {
			continue
		}
		fill, ok := ReadFill(r.path, "")
		out = append(out, ExternalLane{
			Name:           strings.TrimPrefix(r.lane, "sessions-"),
			Key:            r.lane,
			TranscriptPath: r.path,
			LastWrite:      r.mod,
			Fill:           fill,
			FillOK:         ok,
		})
	}
	return out, nil
}
