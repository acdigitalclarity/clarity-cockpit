// Package clarity: this file derives the per-lane context-fill gauge shown
// in the Claude Squad instance list, by reading the SAME transcript files
// and applying the SAME derivation as scripts/fleet_dashboard.py's
// fill_of() (which itself delegates to scripts/hooks/context-fill.py's
// read_fill()) - so the number shown here matches the number the fleet
// dashboard shows for the same lane. This is a Go port of that Python
// logic, not an independent re-derivation; where the two disagree, either
// the Python has a bug or this file does - they are not allowed to just
// drift (board #169's duplicated-logic lesson, applied a second time).
package clarity

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ClaudeProjectsRootEnvVar overrides the default ~/.claude/projects root.
// Set only for tests; the real CLI relies on the default. Superseded by
// ClaudeProjectsRootsEnvVar when that is set, but kept honoured on its own
// exactly as before - every existing test and fixture recipe keys on it.
const ClaudeProjectsRootEnvVar = "CLARITY_CLAUDE_PROJECTS_ROOT"

// ClaudeProjectsRootsEnvVar is the plural, colon-separated sibling of
// ClaudeProjectsRootEnvVar: multiple roots for multiple accounts' config
// directories. Takes precedence over the singular var when set. Set only
// for tests; the real CLI relies on the default-plus-registry branch below.
const ClaudeProjectsRootsEnvVar = "CLARITY_CLAUDE_PROJECTS_ROOTS"

// DefaultClaudeProjectsRoot is the standard location of Claude Code's
// per-project transcript directories on this machine.
const DefaultClaudeProjectsRoot = "/Users/allencoates/.claude/projects"

// KnownWindows mirrors scripts/hooks/context-fill.py's KNOWN_WINDOWS: the
// context-window sizes actually observed in use on this account (never
// invented). 200k is the default account tier; 1M is the Opus/Fable beta
// window. A genuinely new tier needs its own calibration entry before it
// is added here, same rule as the Python source - do not extend this from
// a single unverified observation.
var KnownWindows = []int64{200_000, 1_000_000}

// Fill is the result of one context-fill derivation, mirroring the dict
// read_fill() returns in context-fill.py.
type Fill struct {
	Used   int64
	Window int64
	Pct    int
	Basis  string
}

// ProjectsRoot pairs one discovery root with the seat tag it came from -
// "" when no registry account's config_dir resolves to this root. The
// default root itself carries a tag too, whenever the registry names an
// account whose config_dir is the default's parent (typically "main") -
// IsDefault below is the flag seat resolution (discover.go, slice 3B) reads
// to override that raw tag back to the honest "default" word, since a
// registry label like "main" is an internal bookkeeping name, never what
// the owner should see printed for the machine's own default root.
type ProjectsRoot struct {
	Path    string
	Account string

	// IsDefault is true for the root that stands in for "this machine's
	// default Claude Code config" - DefaultClaudeProjectsRoot itself in the
	// default-plus-registry branch, or the sole root a singular
	// ClaudeProjectsRootEnvVar override produces (that branch exists
	// precisely to let a test stand one root in for the default without
	// touching the real filesystem). The plural ClaudeProjectsRootsEnvVar
	// branch never sets this - every root it names is a genuine distinct
	// seat, none of them "the" default.
	IsDefault bool
}

// claudeProjectsRoots resolves the roots this package glob-searches, in
// precedence order: the plural env var if set; else the singular env var
// if set (today's exact behaviour, unchanged, so every existing test and
// fixture recipe keeps working); else the default root plus one root per
// registry account whose config_dir is not the default's own parent,
// deduplicated, existing directories only. Every root's Account is
// resolved against the registry by matching <config_dir>/projects to the
// root's own path, regardless of which branch produced it.
func claudeProjectsRoots() []ProjectsRoot {
	registry := LoadAccountsRegistry()

	var paths []string
	singularOverride := false
	switch {
	case os.Getenv(ClaudeProjectsRootsEnvVar) != "":
		paths = splitRootsList(os.Getenv(ClaudeProjectsRootsEnvVar))
	case os.Getenv(ClaudeProjectsRootEnvVar) != "":
		paths = []string{os.Getenv(ClaudeProjectsRootEnvVar)}
		singularOverride = true
	default:
		paths = defaultAndRegistryRootPaths(registry)
	}

	roots := make([]ProjectsRoot, 0, len(paths))
	for _, p := range paths {
		isDefault := singularOverride || filepath.Clean(p) == filepath.Clean(DefaultClaudeProjectsRoot)
		roots = append(roots, ProjectsRoot{Path: p, Account: tagForRoot(p, registry), IsDefault: isDefault})
	}
	return roots
}

// splitRootsList splits a colon-separated roots list, dropping empty
// segments (a stray leading/trailing/doubled colon never yields a bare
// root).
func splitRootsList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ":") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// defaultAndRegistryRootPaths builds the default branch's root list:
// DefaultClaudeProjectsRoot always first, then <config_dir>/projects for
// every registry account whose config_dir is not the default root's own
// parent (that account IS the default root, already present), in tag-sorted
// order for a deterministic result, deduplicated by cleaned path, and only
// when the directory actually exists.
func defaultAndRegistryRootPaths(registry map[string]string) []string {
	paths := []string{DefaultClaudeProjectsRoot}
	seen := map[string]bool{filepath.Clean(DefaultClaudeProjectsRoot): true}
	defaultParent := filepath.Clean(filepath.Dir(DefaultClaudeProjectsRoot))

	tags := make([]string, 0, len(registry))
	for tag := range registry {
		tags = append(tags, tag)
	}
	sort.Strings(tags)

	for _, tag := range tags {
		cfgDir := registry[tag]
		if filepath.Clean(cfgDir) == defaultParent {
			continue
		}
		root := filepath.Clean(filepath.Join(cfgDir, "projects"))
		if seen[root] {
			continue
		}
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		seen[root] = true
		paths = append(paths, root)
	}
	return paths
}

// tagForRoot returns the registry tag whose config_dir's "projects"
// subdirectory matches path (cleaned), or "" when no account resolves to
// it - true of every root sourced from a plain env-var override with no
// matching registry entry, and of a registry-less machine altogether.
func tagForRoot(path string, registry map[string]string) string {
	clean := filepath.Clean(path)
	for tag, cfgDir := range registry {
		if filepath.Clean(filepath.Join(cfgDir, "projects")) == clean {
			return tag
		}
	}
	return ""
}

// EncodeProjectDir mirrors the encoding Claude Code itself applies to a
// session's working directory to name its transcript directory under
// ~/.claude/projects: every path separator becomes a hyphen. E.g.
// "/Users/allencoates/projects/Clarity/sessions/ways-of-working" becomes
// "-Users-allencoates-projects-Clarity-sessions-ways-of-working" (verified
// against the real directory on this machine before this file was written).
func EncodeProjectDir(cwd string) string {
	return strings.ReplaceAll(cwd, string(filepath.Separator), "-")
}

// NewestTranscript returns the most-recently-modified *.jsonl transcript
// under the lane's own project directory, matching
// scripts/fleet_dashboard.py's per-lane "newest transcript" selection
// (mtime descending, any path containing "memory" excluded). It searches
// every root claudeProjectsRoots() names, not just the default, so a lane
// launched on a second account's config directory still resolves under its
// OWN root rather than being read against the default's (empty) copy. ok is
// false when no transcript resolves, at which point the caller reports
// "n/a" - never a stale or wrong-lane number.
func NewestTranscript(lanePath string) (path string, ok bool) {
	encoded := EncodeProjectDir(lanePath)

	var bestPath string
	var bestMod int64 = -1
	for _, root := range claudeProjectsRoots() {
		dir := filepath.Join(root.Path, encoded)
		entries, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
		if err != nil {
			continue
		}
		for _, p := range entries {
			if strings.Contains(p, "memory") {
				continue
			}
			info, statErr := os.Stat(p)
			if statErr != nil {
				continue
			}
			mt := info.ModTime().UnixNano()
			if mt > bestMod {
				bestMod = mt
				bestPath = p
			}
		}
	}
	if bestPath == "" {
		return "", false
	}
	return bestPath, true
}

type usageBlock struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

type messageBlock struct {
	Model string      `json:"model"`
	Usage *usageBlock `json:"usage"`
}

type compactMetadataBlock struct {
	Trigger   string `json:"trigger"`
	PreTokens int64  `json:"preTokens"`
}

type transcriptLine struct {
	Message         *messageBlock         `json:"message"`
	CompactMetadata *compactMetadataBlock `json:"compactMetadata"`
}

// pyRound mirrors Python 3's round(): round-half-to-even, not Go's
// math.Round (half-away-from-zero). The two only disagree on an exact .5
// boundary, which real token-usage percentages essentially never hit, but
// this exists so a hit doesn't quietly desync this gauge from the
// dashboard's number.
func pyRound(x float64) int {
	floor := math.Floor(x)
	diff := x - floor
	switch {
	case diff < 0.5:
		return int(floor)
	case diff > 0.5:
		return int(floor) + 1
	default:
		if int64(floor)%2 == 0 {
			return int(floor)
		}
		return int(floor) + 1
	}
}

// ReadFill is a direct Go port of scripts/hooks/context-fill.py's
// read_fill(): the newest assistant usage block versus the model's
// context window, preferring the harness's own auto-compact preTokens
// (real, governed evidence of where this transcript's ceiling sits) over
// the model-name heuristic whenever the transcript carries that evidence.
// See that file's docstring for the full rationale - this is a port, not
// a re-derivation, and the two must not diverge.
func ReadFill(transcriptPath string, modelHint string) (Fill, bool) {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return Fill{}, false
	}
	defer f.Close()

	var lastUsage *usageBlock
	var model string
	var observedCeiling int64

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, `"usage"`) && !strings.Contains(line, `"compactMetadata"`) {
			continue
		}
		var rec transcriptLine
		if jsonErr := json.Unmarshal([]byte(line), &rec); jsonErr != nil {
			continue
		}
		if rec.CompactMetadata != nil && rec.CompactMetadata.Trigger == "auto" {
			if rec.CompactMetadata.PreTokens > observedCeiling {
				observedCeiling = rec.CompactMetadata.PreTokens
			}
		}
		if rec.Message != nil && rec.Message.Usage != nil {
			lastUsage = rec.Message.Usage
			if rec.Message.Model != "" {
				model = rec.Message.Model
			}
		}
	}
	if lastUsage == nil {
		return Fill{}, false
	}

	used := lastUsage.InputTokens + lastUsage.CacheReadInputTokens + lastUsage.CacheCreationInputTokens

	var window int64
	var basis string
	if observedCeiling > 0 {
		window = KnownWindows[len(KnownWindows)-1]
		for _, w := range KnownWindows {
			if w >= observedCeiling {
				window = w
				break
			}
		}
		basis = fmt.Sprintf("transcript (compactMetadata preTokens=%d)", observedCeiling)
	} else {
		blob := strings.ToLower(model + " " + modelHint)
		if strings.Contains(blob, "1m") || strings.Contains(blob, "fable") ||
			strings.Contains(blob, "opus") || used > 210_000 {
			window = 1_000_000
		} else {
			window = 200_000
		}
		label := fmt.Sprintf("%dk", window/1000)
		if window >= 1_000_000 {
			label = "1M"
		}
		basis = fmt.Sprintf("assumed-%s (model-name heuristic, unverified)", label)
	}

	pct := pyRound(100 * float64(used) / float64(window))
	return Fill{Used: used, Window: window, Pct: pct, Basis: basis}, true
}

// ModelWindowLabel derives the Session pane header's context-window word
// (design/cockpit-pane/DECISIONS.md slice 3, "1M window") purely from the
// model name - the same "1m"/"fable"/"opus" naming heuristic ReadFill
// applies above, minus its used>210_000 fallback signal, which needs an
// actual usage figure this caller does not have. ok is false when model is
// empty - there is nothing to derive a window word from, and the header
// simply omits the field rather than guessing.
func ModelWindowLabel(model string) (label string, ok bool) {
	if model == "" {
		return "", false
	}
	blob := strings.ToLower(model)
	if strings.Contains(blob, "1m") || strings.Contains(blob, "fable") || strings.Contains(blob, "opus") {
		return "1M window", true
	}
	return "200k window", true
}

// ContextFillForLane is the one-call convenience the instance list uses:
// resolve the lane's newest transcript, then derive its fill exactly the
// way fleet_dashboard.py's fill_of() does (no model hint - the dashboard
// doesn't pass one either). ok is false ("n/a") when no transcript
// resolves for lanePath.
func ContextFillForLane(lanePath string) (Fill, bool) {
	path, ok := NewestTranscript(lanePath)
	if !ok {
		return Fill{}, false
	}
	return ReadFill(path, "")
}
