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
	"claude-squad/cmd"
	"encoding/json"
	"fmt"
	"os"
	osexec "os/exec"
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

	// Account is the seat tag resolveSeat resolves for this lane, in rule
	// order (a)-(d) (BRIEF-FRONTDOOR-3B.md item 1): a lane's own declared
	// Account: line beats a Desktop-entrypoint transcript beats a seat
	// folder's own oauthAccount login beats the honest "default" floor.
	// Never "" - rule (d) is the floor every lane resolves to.
	Account string

	// SeatSource names which rule fired: "declared", "desktop",
	// "folder-login" or "folder" (the SeatSource* constants below). Paired
	// with Account by SeatTag to render the printed "[<tag>]" or
	// "[<tag> <source>]" bracket.
	SeatSource string

	// Modality is read from the lane folder's own .claude/CLAUDE.md
	// Modality: line (WorkDir below), mirroring scripts/clarity's
	// session_modality(). "" when WorkDir is empty, the folder does not
	// exist, or the line is absent.
	Modality string

	// WorkDir is the lane's own working directory, read straight from the
	// transcript's "cwd" field (readTranscriptCwd below) - the same value
	// TrackedExclusionPaths matching already reads, kept here too so the
	// Session pane's header (design/cockpit-pane/DECISIONS.md slice 3) can
	// show an external lane's workdir the same way it shows a tracked
	// instance's. "" when the scan window never found a cwd record.
	WorkDir string

	// State, LastTurn and StateOK are the lane's clarity.ReadLaneTail
	// classification (working/waiting on you/idle/stalled) - populated by
	// the caller (app.go's feedTickMsg handler, via a LaneTailCache) on the
	// same tick as Fill above, never by DiscoverExternalLanes itself, so a
	// plain CLI caller (cs-clarity discover) that has no cache still works
	// exactly as before with these left at their zero values.
	State    string
	LastTurn time.Time
	StateOK  bool
	// AnsweredAt mirrors LaneTail.AnsweredAt (item 5, WAITING HELD) - same
	// caller, same tick, same zero-value contract as State/LastTurn above.
	AnsweredAt time.Time

	// Alive is DiscoverExternalLanes' own liveness signal (item 1, slice
	// 17b): ExternalLaneAlive(Key), computed once per discovery pass on the
	// same tick as everything else above. A stale-but-still-fresh transcript
	// (inside ExternalLiveWindow, which is only ever 90 minutes wide) is not
	// proof a process is still behind it - the 3 Sep 18:47:57 incident. The
	// state-word/sort/attention layers (ui/list.go, app/attention.go) all
	// read this rather than re-deriving it, so a caller can never disagree
	// with discovery about which external rows are actually alive.
	Alive bool
}

// externalTranscriptRow is the pre-dedupe intermediate the discovery loop
// builds before collapsing to one row per lane name.
type externalTranscriptRow struct {
	lane string
	path string
	mod  time.Time
	cwd  string       // "" when the transcript's own cwd field could not be read
	root ProjectsRoot // the root this row's transcript sat under
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

// accountFromLaneDir reads the Account: line from lanePath's own
// .claude/CLAUDE.md, mirroring scripts/clarity's session_account() (grep -i
// '^Account:' | sed 's/^Account:[[:space:]]*//') - the tag `clarity new
// --account <tag>` writes at session creation (BRIEF-FRONTDOOR-3B.md rule
// a). "" when lanePath is empty, the folder does not exist, or the line is
// absent - never an error, since most lanes predate this convention.
func accountFromLaneDir(lanePath string) string {
	if lanePath == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(lanePath, ".claude", "CLAUDE.md"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := claudeMDFieldValue(line, "Account"); ok {
			return v
		}
	}
	return ""
}

// DesktopEntrypoint is the "entrypoint" value the Claude Desktop app writes
// on every conversational record of a session it launched - confirmed
// against a live transcript before this file was written (BRIEF-FRONTDOOR-
// 3B.md's field-name survey). The CLI writes "cli" instead.
const DesktopEntrypoint = "claude-desktop"

// transcriptEntrypoint reads forward from the start of transcriptPath
// looking for the first record carrying a non-empty top-level "entrypoint"
// field, the same forward-scan shape readTranscriptCwd applies to "cwd" -
// the two fields sit on the same early conversational records in practice.
// ok is false when no such record turns up inside the scan budget.
func transcriptEntrypoint(transcriptPath string) (entrypoint string, ok bool) {
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
			Entrypoint string `json:"entrypoint"`
		}
		if err := json.Unmarshal(b, &rec); err != nil {
			continue
		}
		if rec.Entrypoint != "" {
			return rec.Entrypoint, true
		}
	}
	return "", false
}

// Seat resolution sources (BRIEF-FRONTDOOR-3B.md item 1) - the rule that
// resolved a lane's Account tag, paired with it on ExternalLane.SeatSource
// and rendered by SeatTag.
const (
	SeatSourceDeclared    = "declared"     // rule (a): the lane's own Account: line
	SeatSourceDesktop     = "desktop"      // rule (b): newest transcript entrypoint claude-desktop
	SeatSourceFolderLogin = "folder-login" // rule (c): non-default seat folder's own oauthAccount
	SeatSourceFolder      = "folder"       // rule (d): the floor - default root, or an unlogged-in seat root
)

// resolveSeat applies the seat-resolution rule in order (a)-(d): a lane's
// own declared Account: line beats a Desktop-entrypoint transcript beats a
// non-default seat folder's own oauthAccount login beats the honest
// "default" floor - never "main", never any other registry bookkeeping
// name, for the machine's own default root. lanePath is the lane's own
// working directory ("" when unknown, in which case rule (a) never fires);
// transcriptPath is the lane's newest transcript; root is the ProjectsRoot
// that transcript was found under.
func resolveSeat(lanePath, transcriptPath string, root ProjectsRoot) (tag, source string) {
	if declared := accountFromLaneDir(lanePath); declared != "" {
		return declared, SeatSourceDeclared
	}
	if entrypoint, ok := transcriptEntrypoint(transcriptPath); ok && entrypoint == DesktopEntrypoint {
		return "desktop", SeatSourceDesktop
	}
	if !root.IsDefault && root.Account != "" {
		if oauth := ReadSeatOAuthAccount(filepath.Dir(root.Path)); oauth.Present {
			return root.Account, SeatSourceFolderLogin
		}
	}
	if root.IsDefault || root.Account == "" {
		return "default", SeatSourceFolder
	}
	return root.Account, SeatSourceFolder
}

// SeatTag renders the printed seat bracket's contents (without the
// brackets themselves, which callers add): the tag alone when source is
// "declared" or is itself identical to tag (the "desktop"/"desktop" case -
// appending it a second time would say nothing new), otherwise "<tag>
// <source>" - "team-b", "desktop", "team-a folder-login", "default folder".
func SeatTag(tag, source string) string {
	if source == "" || source == SeatSourceDeclared || source == tag {
		return tag
	}
	return tag + " " + source
}

// modalityFromLaneDir reads the Modality: line from lanePath's own
// .claude/CLAUDE.md, mirroring scripts/clarity's session_modality() (grep -i
// '^Modality:' | sed 's/^Modality:[[:space:]]*//'). "" when lanePath is
// empty, the folder does not exist, or the line is absent - never an error,
// since most external lanes predate this convention.
func modalityFromLaneDir(lanePath string) string {
	if lanePath == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(lanePath, ".claude", "CLAUDE.md"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := claudeMDFieldValue(line, "Modality"); ok {
			return v
		}
	}
	return ""
}

// claudeMDFieldValue reports whether line declares field as a "Field:"
// prefix (case-insensitive, matching the shell's grep -i) and returns its
// trimmed value.
func claudeMDFieldValue(line, field string) (value string, ok bool) {
	trimmed := strings.TrimSpace(line)
	prefix := field + ":"
	if len(trimmed) < len(prefix) || !strings.EqualFold(trimmed[:len(prefix)], prefix) {
		return "", false
	}
	return strings.TrimSpace(trimmed[len(prefix):]), true
}

// ExternalLaneAlive is discover.go's own fallback external-lane liveness
// signal (item 1, slice 17b): DiscoverExternalLanes reads no pid, lock or
// heartbeat field from a transcript today - confirmed against a live
// transcript's own JSON key set before this was written (this leg's own
// report quotes the full key set; "pid"/"lock"/"heartbeat" are absent from
// it) - so this falls back to the same session-liveness primitive
// session/instance.go's own dead-lane reload path already relies on:
// `tmux has-session`, the exact command session/tmux's DoesSessionExist
// shells (session/tmux/tmux.go, "-t=<name>" for an exact match, never a
// prefix match), applied here to the external lane's own bare key rather
// than a tracked instance's claudesquad_-prefixed one, since an external
// lane has no such prefix to begin with. tmuxArgs is prepended before
// "has-session" (e.g. "-L", "sockname" for an isolated test socket) - the
// production caller below passes none, exactly like DoesSessionExist,
// which never passes a socket flag either (the whole fleet, tracked and
// external alike, shares tmux's own single ambient default server - the 3
// Sep 18:47:57 incident's own root cause was a bare `tmux kill-server`
// taking that ONE shared server down).
func ExternalLaneAlive(name string, exec cmd.Executor, tmuxArgs ...string) bool {
	args := append(append([]string{}, tmuxArgs...), "has-session", fmt.Sprintf("-t=%s", name))
	return exec.Run(osexec.Command("tmux", args...)) == nil
}

// discoverExternalLaneMetadata is DiscoverExternalLanes' own walk half
// (item 3, slice 20b split): every <root>/<encoded>/*.jsonl transcript
// across every root claudeProjectsRoots() names, whose mtime is within
// ExternalLiveWindow, minus any path containing "memory", minus any lane
// name starting with "subagents", minus any lane whose transcript's own
// "cwd" field is already present in excludeDirs (see TrackedExclusionPaths
// - a lane Claude Squad already tracks as an instance is never ALSO shown
// as an external row) - collapsed to the single newest transcript per
// (account, lane name) pair, sorted newest-first. The same lane name under
// two different roots is two rows (different seats) - the dedupe is scoped
// to one root's own lanes, never across roots. A nil or empty excludeDirs
// is valid (no exclusions). Every row's Alive is left at its zero value
// (false) - liveness is the caller's own concern (ExternalLanesAlive
// below), batched once per pass rather than derived per lane here, so a
// caller that already has a fresh lane list (ExternalLaneScanner, an
// unchanged fingerprint) can refresh liveness alone without re-walking.
func discoverExternalLaneMetadata(excludeDirs map[string]bool) ([]ExternalLane, error) {
	now := time.Now()
	var rows []externalTranscriptRow
	for _, root := range claudeProjectsRoots() {
		pattern := filepath.Join(root.Path, "*", "*.jsonl")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
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
			rows = append(rows, externalTranscriptRow{lane: lane, path: p, mod: info.ModTime(), root: root})
		}
	}

	// Newest first, so the per-lane dedupe below keeps each lane's newest
	// transcript - same ordering fleet_dashboard.py's rows.sort() + "seen"
	// set produces.
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].mod.After(rows[j].mod) })

	seen := make(map[string]bool, len(rows))
	out := make([]ExternalLane, 0, len(rows))
	for _, r := range rows {
		key := r.root.Account + "\x00" + r.lane
		if seen[key] {
			continue
		}
		seen[key] = true
		cwd, cwdOK := readTranscriptCwd(r.path)
		if cwdOK && excludeDirs[filepath.Clean(cwd)] {
			continue
		}
		fill, ok := ReadFill(r.path, "")
		seatTag, seatSource := resolveSeat(cwd, r.path, r.root)
		out = append(out, ExternalLane{
			Name:           strings.TrimPrefix(r.lane, "sessions-"),
			Key:            r.lane,
			TranscriptPath: r.path,
			LastWrite:      r.mod,
			Fill:           fill,
			FillOK:         ok,
			WorkDir:        cwd,
			Account:        seatTag,
			SeatSource:     seatSource,
			Modality:       modalityFromLaneDir(cwd),
		})
	}
	return out, nil
}

// ExternalLanesAlive is ExternalLaneAlive's own batched replacement (item 3,
// slice 20b): ONE `tmux list-sessions -F #S` process names every live
// session on the addressed server at once, rather than one `tmux
// has-session` subprocess per external lane - the same liveness ANSWER (a
// session existing under this exact name), one process instead of N.
// tmuxArgs is prepended before "list-sessions" the same way
// ExternalLaneAlive's own tmuxArgs is (e.g. "-L", "sockname" for an
// isolated test socket); the production caller passes none, the same
// ambient default server ExternalLaneAlive always addressed. A server with
// no sessions at all (or not running) reads every name as not alive, never
// an error - exec.Output's own non-zero exit in that case is the expected
// shape of "nothing is live here", not a failure to surface.
func ExternalLanesAlive(names []string, exec cmd.Executor, tmuxArgs ...string) map[string]bool {
	alive := make(map[string]bool, len(names))
	args := append(append([]string{}, tmuxArgs...), "list-sessions", "-F", "#S")
	out, err := exec.Output(osexec.Command("tmux", args...))
	live := make(map[string]bool)
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				live[line] = true
			}
		}
	}
	for _, n := range names {
		alive[n] = live[n]
	}
	return alive
}

// DiscoverExternalLanes derives the list of live external lanes:
// discoverExternalLaneMetadata's own walk, then ExternalLanesAlive's own
// single batched liveness pass over the resulting Keys. execOverride is
// the test seam (item 4c: "count through the executor") - a MockCmdExec
// that counts Output calls; omitted (the production shape, every existing
// caller) it defaults to cmd.MakeExecutor(), exactly as before this split.
func DiscoverExternalLanes(excludeDirs map[string]bool, execOverride ...cmd.Executor) ([]ExternalLane, error) {
	lanes, err := discoverExternalLaneMetadata(excludeDirs)
	if err != nil {
		return nil, err
	}
	exec := cmd.MakeExecutor()
	if len(execOverride) > 0 {
		exec = execOverride[0]
	}
	names := make([]string, len(lanes))
	for i, l := range lanes {
		names[i] = l.Key
	}
	alive := ExternalLanesAlive(names, exec)
	for i := range lanes {
		lanes[i].Alive = alive[lanes[i].Key]
	}
	return lanes, nil
}

// ExternalLanesScanFingerprint is a cheap, read-only summary of every
// directory discoverExternalLaneMetadata's own glob walk would touch (each
// claudeProjectsRoots() root, plus every one of its immediate
// subdirectories) - built from directory mtimes alone, never opening a
// single *.jsonl file. Two consecutive calls returning the same
// fingerprint mean the walk would enumerate the exact same set of
// transcript files: a new or removed session subdirectory changes its own
// root's mtime; a new or removed transcript inside an EXISTING session
// subdirectory changes that subdirectory's own mtime - both are covered,
// so ExternalLaneScanner.Scan below can skip the walk whenever this string
// is unchanged. A root or subdirectory this process cannot stat is
// recorded as "ERR" rather than silently omitted, so a permissions change
// or a root disappearing still changes the fingerprint.
func ExternalLanesScanFingerprint() string {
	var b strings.Builder
	for _, root := range claudeProjectsRoots() {
		info, err := os.Stat(root.Path)
		if err != nil {
			fmt.Fprintf(&b, "%s=ERR;", root.Path)
			continue
		}
		fmt.Fprintf(&b, "%s@%d;", root.Path, info.ModTime().UnixNano())
		entries, err := os.ReadDir(root.Path)
		if err != nil {
			fmt.Fprintf(&b, "%s/*=ERR;", root.Path)
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			sub := filepath.Join(root.Path, e.Name())
			si, statErr := os.Stat(sub)
			if statErr != nil {
				fmt.Fprintf(&b, "%s=ERR;", sub)
				continue
			}
			fmt.Fprintf(&b, "%s@%d;", sub, si.ModTime().UnixNano())
		}
	}
	return b.String()
}

// ExternalLaneScanner is DiscoverExternalLanes' own change-driven wrapper
// (item 3, slice 20b) - app.go's feedTickMsg calls Scan on its existing 3s
// cadence instead of DiscoverExternalLanes directly. Scan re-walks the
// filesystem only when ExternalLanesScanFingerprint has actually changed
// since the last call; on an unchanged pass it reuses the last walk's own
// lane list and refreshes only their liveness (ExternalLanesAlive - one
// tmux call, never a walk), so a lane's tmux session dying between two
// otherwise-quiet ticks is still caught. The zero value is ready to use.
type ExternalLaneScanner struct {
	fingerprint string
	lanes       []ExternalLane
	primed      bool

	// walkCount is a test-only observation hook (item 4b): the number of
	// times Scan has actually performed discoverExternalLaneMetadata's own
	// walk, never incremented on a fingerprint-unchanged pass.
	walkCount int
}

// Scan returns the current external-lane list, walking the filesystem only
// when ExternalLanesScanFingerprint has changed since the previous call.
// excludeDirs is discoverExternalLaneMetadata's own parameter, re-applied
// on every walk (never cached against a stale exclusion set - a lane
// Claude Squad starts tracking between two ticks must stop appearing here
// on the very next walk, not stay excluded-or-included from whichever set
// happened to be in force the last time the walk actually ran). execOverride
// is DiscoverExternalLanes' own test seam, passed straight through to
// ExternalLanesAlive.
func (s *ExternalLaneScanner) Scan(excludeDirs map[string]bool, execOverride ...cmd.Executor) ([]ExternalLane, error) {
	fp := ExternalLanesScanFingerprint()
	if !s.primed || fp != s.fingerprint {
		lanes, err := discoverExternalLaneMetadata(excludeDirs)
		if err != nil {
			return nil, err
		}
		s.lanes = lanes
		s.fingerprint = fp
		s.primed = true
		s.walkCount++
	}

	exec := cmd.MakeExecutor()
	if len(execOverride) > 0 {
		exec = execOverride[0]
	}
	names := make([]string, len(s.lanes))
	for i, l := range s.lanes {
		names[i] = l.Key
	}
	alive := ExternalLanesAlive(names, exec)
	for i := range s.lanes {
		s.lanes[i].Alive = alive[s.lanes[i].Key]
	}
	return s.lanes, nil
}
