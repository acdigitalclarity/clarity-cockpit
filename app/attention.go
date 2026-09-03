// Package app: item 3 of COCKPIT-MODALITIES-2026-09-03.md (cockpit pane
// slice 17) - a fleet-wide terminal bell and window-title lane count,
// computed once per feedTickMsg tick alongside the rest of that tick's own
// per-lane state derivation, never a second polling loop of its own.
package app

import (
	"claude-squad/session/clarity"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
)

// attentionDefaultTitle is the window title when nothing needs the owner -
// the brief's own "title 'Clarity Workspace' when N is 0".
const attentionDefaultTitle = "Clarity Workspace"

// attentionBellCooldown is "at most one bell per 10 seconds fleet-wide" (the
// brief's own words) - one shared cooldown, not one per lane, so a burst of
// several lanes crossing into attention on the same tick still rings once.
const attentionBellCooldown = 10 * time.Second

// attentionCategory is one lane's own bell/title bucket for a given tick -
// mutually exclusive per lane, the same "ahead of every other word"
// convention NeedsKey() already overrides a row's DISPLAYED state word with
// (ui/list.go's laneStateNeedsKeyStyle): a needs-a-key lane is counted as
// needing a key, never ALSO as waiting/stalled underneath.
type attentionCategory int

const (
	attentionNone attentionCategory = iota
	attentionWaitingOrStalled
	attentionNeedsKey
)

// classifyAttention derives one lane's attentionCategory from its own
// current classifier state and (tracked lanes only) its NeedsKey sample.
func classifyAttention(state string, needsKey bool) attentionCategory {
	if needsKey {
		return attentionNeedsKey
	}
	if state == clarity.StateWaitingYou || state == clarity.StateStalled {
		return attentionWaitingOrStalled
	}
	return attentionNone
}

// updateAttention recomputes the fleet's own bell/title state for one
// feedTickMsg tick, after every lane's state (SetLaneState) and NeedsKey
// sample for THIS tick are already current (called from feedTickMsg's own
// handler, after m.sampleNeedsKey()). It keys each lane by a stable string
// (never an index, which a kill/adopt can shift mid-tick) so a RISING edge
// - last tick attentionNone, this tick attentionWaitingOrStalled - can be
// told apart from a lane that was already ringing last tick (a LEVEL,
// silent, the brief's own "edge, not level"). A lane no longer present
// (killed, or an external lane that vanished) is dropped from the tracker,
// so a future lane reusing the same name can never phantom-ring off a
// stale entry.
//
// m.windowTitle is set here unconditionally, every tick, for View() to hand
// straight to tea.View.WindowTitle. The returned Cmd fires the bell
// (tea.Raw("\a") - bubbletea v2.0.9's own only mechanism that writes
// straight to the terminal unmanaged by the altscreen renderer; tea.Println
// is a documented no-op in altscreen mode, which this app's View() always
// sets - verified at charm.land/bubbletea/v2@v2.0.9/renderer.go and raw.go)
// when at least one lane crossed into attention AND the fleet-wide cooldown
// has elapsed; nil otherwise, safe to hand straight to tea.Batch (nil Cmds
// are compacted away).
func (m *home) updateAttention(now time.Time) tea.Cmd {
	if m.attentionState == nil {
		m.attentionState = make(map[string]attentionCategory)
	}
	seen := make(map[string]bool, len(m.attentionState))
	crossed := false
	n := 0

	for _, inst := range m.list.GetInstances() {
		key := "tracked:" + inst.Title
		seen[key] = true
		state, _, ok := inst.GetLaneState()
		if !ok {
			continue
		}
		cat := classifyAttention(state, inst.NeedsKey())
		if cat != attentionNone {
			n++
		}
		if cat == attentionWaitingOrStalled && m.attentionState[key] != attentionWaitingOrStalled {
			crossed = true
		}
		m.attentionState[key] = cat
	}
	for _, lane := range m.list.GetExternal() {
		key := "external:" + lane.Name
		seen[key] = true
		if !lane.StateOK {
			continue
		}
		// External lanes never carry NeedsKey (no tracked tmux pane to
		// sample - clarity.StateNeedsKey's own doc comment).
		cat := classifyAttention(lane.State, false)
		if cat != attentionNone {
			n++
		}
		if cat == attentionWaitingOrStalled && m.attentionState[key] != attentionWaitingOrStalled {
			crossed = true
		}
		m.attentionState[key] = cat
	}
	for key := range m.attentionState {
		if !seen[key] {
			delete(m.attentionState, key)
		}
	}

	if n == 0 {
		m.windowTitle = attentionDefaultTitle
	} else {
		m.windowTitle = fmt.Sprintf("%s · %d need you", attentionDefaultTitle, n)
	}

	if !crossed {
		return nil
	}
	if !m.lastBellAt.IsZero() && now.Sub(m.lastBellAt) < attentionBellCooldown {
		return nil
	}
	m.lastBellAt = now
	return tea.Raw("\a")
}
