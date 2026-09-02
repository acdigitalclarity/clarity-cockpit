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
	"strings"
)

// ClaudeProjectsRootEnvVar overrides the default ~/.claude/projects root.
// Set only for tests; the real CLI relies on the default.
const ClaudeProjectsRootEnvVar = "CLARITY_CLAUDE_PROJECTS_ROOT"

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

func claudeProjectsRoot() string {
	if root := os.Getenv(ClaudeProjectsRootEnvVar); root != "" {
		return root
	}
	return DefaultClaudeProjectsRoot
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
// (mtime descending, any path containing "memory" excluded). ok is false
// when no transcript resolves, at which point the caller reports "n/a" -
// never a stale or wrong-lane number.
func NewestTranscript(lanePath string) (path string, ok bool) {
	dir := filepath.Join(claudeProjectsRoot(), EncodeProjectDir(lanePath))
	entries, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil || len(entries) == 0 {
		return "", false
	}

	var bestPath string
	var bestMod int64 = -1
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
