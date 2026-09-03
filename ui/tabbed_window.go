package ui

import (
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"claude-squad/log"
	"claude-squad/session/clarity"
	"github.com/charmbracelet/x/ansi"
)

func tabBorderWithBottom(left, middle, right string) lipgloss.Border {
	border := lipgloss.RoundedBorder()
	border.BottomLeft = left
	border.Bottom = middle
	border.BottomRight = right
	return border
}

var (
	inactiveTabBorder = tabBorderWithBottom("┴", "─", "┴")
	activeTabBorder   = tabBorderWithBottom("┘", " ", "└")
	highlightColor    = compat.AdaptiveColor{Light: lipgloss.Color("#874BFD"), Dark: lipgloss.Color("#7D56F4")}
	inactiveTabStyle  = lipgloss.NewStyle().
				Border(inactiveTabBorder, true).
				BorderForeground(highlightColor).
				AlignHorizontal(lipgloss.Center)
	activeTabStyle = inactiveTabStyle.
			Border(activeTabBorder, true).
			AlignHorizontal(lipgloss.Center)
	windowStyle = lipgloss.NewStyle().
			BorderForeground(highlightColor).
			Border(lipgloss.NormalBorder(), false, true, true, true)
)

// Butterfly on the tab bar (slice 21, owner's own words: "need some
// serenity while working so many things"; slice 23, "butterfly is cool -
// can we improve it?"). At rest it beats through a four-step cycle -
// closed, half, open, half - rather than slice 21's plain two-glyph
// toggle (design refinement 1). The two endpoints are unchanged from slice
// 21: U+029A ʚ (closed) and U+025E ɞ (open), a mirror pair of lowercase
// IPA letters chosen over the emoji/symbol candidates (rendered side by
// side in a scratch tmux pane at 164 columns, slice 21's own PROOF
// section) for reading as a small, calm, rounded wing pair rather than a
// technical glyph ("⌘") or a single ornament with no natural "closed"
// counterpart ("ꕥ"). The half-beat glyph is new this slice: of the
// candidates the brief names (ʘ, ɵ, θ, ɸ, all proven single-width by
// TestButterfly_FramesAreSingleWidth below), ʘ (U+0298, a circle with a
// centre dot) reads as an eye or a pupil rather than a wing, and θ/ɸ
// (U+03B8, U+0278) are both common Greek letters - exactly the "reads as a
// technical glyph" objection slice 21's own doc comment above raises
// against "⌘", now against a maths symbol instead of a keyboard one. ɵ
// (U+0275, LATIN SMALL LETTER BARRED O - a round body with a single
// horizontal bar through it) stays in the same obscure lowercase-IPA
// register as ʚ/ɞ and reads as the wings held flat and spread mid-beat,
// symmetric between ʚ's left-open curl and ɞ's right-open curl rather than
// leaning toward either - rendered against all four candidates side by
// side (scratch tmux pane, isolated socket, this leg's own PROOF section)
// to make that call. butterflyFrames[1] and [3] are the same glyph - the
// half-beat looks identical rising into the open wingspan and falling back
// out of it, so the cycle only needs three distinct glyphs to draw four
// steps.
var butterflyFrames = [4]string{"ʚ", "ɵ", "ɞ", "ɵ"}

// Named indices into butterflyFrames - butterflyClosedFrame/OpenFrame are
// also the two-state toggle the faster in-flight beat alternates between
// (tickFastBeat below): a flight is quick enough that cycling through the
// half-beat glyphs too would just look busy, not calm, so it skips them.
const (
	butterflyClosedFrame = 0
	butterflyOpenFrame   = 2
)

// butterflyRestFrameTicks is how many 100ms ticks each of the four rest
// frames holds before advancing to the next - design refinement 1's "one
// beat per ~1.5s, easing so the open state holds longest": the four values
// sum to 15 ticks (1.5s) for one full closed-half-open-half cycle, and the
// open frame (index 2) holds twice as long as each of the other three -
// the calmest point in the beat is the one it lingers on.
var butterflyRestFrameTicks = [4]int{3, 3, 6, 3}

// butterflyStyle is a single accent, not the two-tone body/wing scheme the
// design offers as an alternative - two colours flickering on every rest
// flap is itself a form of colour pulsing, which the design's own rule 3
// ("no colour pulsing") rules out; one calm accent is the calmer choice it
// invites picking. Reuses the splash's own "openSkies" teal, the same
// adaptive pair ui/session.go's sessionClaudeStyle already carries
// (SESSION-READING-SPEC.md's colour roles).
var butterflyStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#0f7f83"), Dark: lipgloss.Color("#54E6EA")})

const (
	// butterflyFlapTicksFlying is the faster in-flight flap cadence -
	// "wings beating faster in flight" - shared by a tab-change flight and
	// a meaningful-flight notice alike.
	butterflyFlapTicksFlying = 3
	// butterflyFlightTicks is how many 100ms ticks a flight between tabs
	// takes - "drifts... over about 1.5s on the 100ms tick", and well
	// inside the "ends... within 20 ticks" test bound. A notice's own
	// outbound and inbound legs (below) reuse this same duration.
	butterflyFlightTicks = 15
	// butterflyWobbleCycles is how many full sine wobbles a flight makes
	// end to end - "a gentle wander (a sine wobble of one column)".
	butterflyWobbleCycles = 2.0
)

// Idle wander (design refinement 2): every 45-90s at rest the butterfly
// lifts off, drifts a few columns one way, pauses, and returns - never
// while a real flight (tab change or notice) is under way, and never
// leaving the tab bar's own width (butterflyStartWander below clamps it).
const (
	// butterflyWanderMinTicks/MaxTicks bound the random gap between one
	// wander and the next - 45-90s at the shared 100ms tick.
	butterflyWanderMinTicks = 450
	butterflyWanderMaxTicks = 900
	// butterflyWanderMinDriftCols/MaxDriftCols is how far a wander drifts -
	// "three to six columns one way".
	butterflyWanderMinDriftCols = 3
	butterflyWanderMaxDriftCols = 6
	// butterflyWanderTravelTicks is each leg's own duration (out, then
	// back) - shorter than a tab-change flight (butterflyFlightTicks): a
	// few columns is a much smaller trip than tab to tab, and the wander is
	// meant to read as a small aside, not a second flight.
	butterflyWanderTravelTicks = 10
	// butterflyWanderPauseTicks is how long it lingers at the far point
	// before heading back.
	butterflyWanderPauseTicks = 8
)

// butterflyWanderTicksEnvVar lets a manual proof run force the idle wander
// onto a short, fixed schedule instead of waiting out the real 45-90s
// window - read once at construction (NewTabbedWindow), the same
// test/proof-only env-var seam ui/terminal.go's own
// CLARITY_TEST_FORBID_TMUX already uses in this package. Unset (or
// non-positive) in every real run - main.go never sets it.
const butterflyWanderTicksEnvVar = "CLARITY_BUTTERFLY_WANDER_TICKS"

// butterflyNoticeHoverTicks is how long a notice flight hovers over the
// Needs-you tab before heading back - design refinement 3's "about three
// seconds".
const butterflyNoticeHoverTicks = 30

// Notice-flight phases (butterflyNoticePhase) - a new Needs-you row starts
// a short round trip to the Needs-you tab and back, at the faster
// in-flight beat throughout, including the hover.
const (
	butterflyNoticeOut = iota
	butterflyNoticeHover
	butterflyNoticeBack
)

// Idle-wander phases (butterflyWanderPhase).
const (
	butterflyWanderOut = iota
	butterflyWanderPause
	butterflyWanderBack
)

// SessionTab replaces the old PreviewTab (design/cockpit-pane/DECISIONS.md
// slice 3): it is still tab index 0 and still the default, but now shows
// the selected lane's own conversation (ui/session.go) rather than a raw
// tmux capture. NeedsYouTab (slice 5) replaces the old Diff tab in the
// same slot: DiffPane's own upstream content is dropped from the tabs -
// it stays reachable from nothing in this slice (the brief's own words) -
// and the slot now shows the selected Needs-you row's own detail
// (ui/needsyou.go) instead. Both PreviewPane and DiffPane are left in the
// tree, unused by this window, rather than deleted - see NewTabbedWindow's
// own comment.
const (
	SessionTab int = iota
	NeedsYouTab
	TerminalTab
)

type Tab struct {
	Name   string
	Render func(width int, height int) string
}

// TabbedWindow has tabs at the top of a pane which can be selected. The tabs
// take up one rune of height.
type TabbedWindow struct {
	tabs []string

	activeTab int
	height    int
	width     int
	// contentWidth/contentHeight is the content area SetSize computes and
	// hands identically to every tab's own pane - see GetContentSize.
	contentWidth  int
	contentHeight int

	session  *SessionPane
	needsYou *NeedsYouPane
	terminal *TerminalPane

	// Butterfly state (slice 21, extended slice 23) - see the
	// butterflyFrames/butterflyStyle doc comment above and
	// TickButterfly/butterflyPosition below for the state machine.
	// SetButterflyEnabled defaults true (NewTabbedWindow); wiring the
	// --no-butterfly flag and matching config key into it is the caller's
	// job (main.go/config.go), outside this file's own fence.
	butterflyEnabled    bool
	butterflyRestTab    int  // the tab index the butterfly rests over, or flies towards
	butterflyFlying     bool // mid-flight between two tabs (a real tab change)
	butterflyFlightTick int  // 0..butterflyFlightTicks-1 while flying
	butterflyFromCol    int  // column (tab-row coordinate space) the current flight departs from
	butterflyFlapPhase  int  // ticks since the last frame flip
	butterflyFrame      int  // indexes butterflyFrames (0..3)

	// Idle wander (slice 23, design refinement 2) - see startWander/
	// tickWander/butterflyWanderPosition. butterflyRand is seeded from the
	// clock at construction (design's own words); butterflyWanderTicksOverride,
	// non-zero only under butterflyWanderTicksEnvVar, fixes the gap for a
	// manual proof run instead of drawing it from the real 45-90s range.
	butterflyRand                *rand.Rand
	butterflyWanderTicksOverride int
	butterflyTicksUntilWander    int  // ticks remaining until the next wander, counted down only while truly at rest
	butterflyWandering           bool // mid-wander (out, paused, or heading back)
	butterflyWanderPhase         int  // butterflyWanderOut/Pause/Back
	butterflyWanderTick          int  // ticks elapsed in the current wander phase
	butterflyWanderFromCol       int  // the wander's own rest column (where it lifted off, and returns to)
	butterflyWanderToCol         int  // the far column the wander drifts out to

	// Meaningful flights (slice 23, design refinement 3) - see
	// NoticeNeedsYou/tickNotice/butterflyNoticePosition.
	// butterflySeenIssues is the set of board issue numbers the last call
	// has already seen - nil until the first call, which only primes it
	// (see NoticeNeedsYou's own doc comment for why).
	butterflySeenIssues        map[int]bool
	butterflyNoticing          bool // mid-notice (flying out, hovering, or flying back)
	butterflyNoticeNeedsFlight bool // false when the notice started already on the Needs-you tab - hover only, no flight legs
	butterflyNoticePhase       int  // butterflyNoticeOut/Hover/Back
	butterflyNoticeTick        int  // ticks elapsed in the current notice phase
	butterflyNoticeFromCol     int  // the column the outbound leg departs from
	butterflyNoticeReturnTab   int  // the tab that was active when the notice started - where the inbound leg lands
}

// NewTabbedWindow wires the three tabs: Session (slice 3's replacement for
// the old tmux-capture Preview pane), Needs you (slice 5's replacement for
// the old Diff pane) and Terminal (untouched in this slice - see
// DECISIONS.md's build-slice list, item 6). PreviewPane and DiffPane are
// both kept in the tree, dormant - nothing upstream is thrown away, neither
// simply has a tab slot pointed at it any more.
func NewTabbedWindow(session *SessionPane, needsYou *NeedsYouPane, terminal *TerminalPane) *TabbedWindow {
	w := &TabbedWindow{
		tabs: []string{
			"Session",
			"Needs you",
			"Terminal",
		},
		session:                      session,
		needsYou:                     needsYou,
		terminal:                     terminal,
		butterflyEnabled:             true,
		butterflyRand:                rand.New(rand.NewSource(time.Now().UnixNano())),
		butterflyWanderTicksOverride: butterflyWanderTicksOverrideFromEnv(),
	}
	w.butterflyTicksUntilWander = w.nextWanderTicks()
	return w
}

// butterflyWanderTicksOverrideFromEnv reads butterflyWanderTicksEnvVar once
// at construction - see its own doc comment above.
func butterflyWanderTicksOverrideFromEnv() int {
	v := os.Getenv(butterflyWanderTicksEnvVar)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// nextWanderTicks draws the gap, in ticks, until the next idle wander -
// the real 45-90s random range, or the fixed proof-only override.
func (w *TabbedWindow) nextWanderTicks() int {
	if w.butterflyWanderTicksOverride > 0 {
		return w.butterflyWanderTicksOverride
	}
	return butterflyWanderMinTicks + w.butterflyRand.Intn(butterflyWanderMaxTicks-butterflyWanderMinTicks+1)
}

// AdjustPreviewWidth adjusts the width of the preview pane to be 90% of the
// provided width - kept for ui/list.go's own unrelated title-bar width calc
// (list.go:648, a distinct calculation over the LIST's own width, not this
// window's), but no longer used by TabbedWindow.SetSize itself (see its own
// comment - slice 13's "leaves the 10 columns" fix).
func AdjustPreviewWidth(width int) int {
	return int(float64(width) * 0.9)
}

func (w *TabbedWindow) SetSize(width, height int) {
	// Slice 13's own root-cause fix ("the tabbed window and the list must
	// together reach column 164"): this used to shave a further 10% off
	// width via AdjustPreviewWidth, on top of the list already having taken
	// its own share in app.go - so the pane's own box (this value plus its
	// own border, windowStyle.GetHorizontalFrameSize()) landed 10+ columns
	// short of the terminal's real right edge (164 case: stopped at column
	// 154, not 164). width here IS the budget already computed FOR this
	// component (app.go's tabsWidth = the terminal width minus the list's
	// own share) - w.width only needs its own border subtracted, once, so
	// that w.width+GetHorizontalFrameSize() (the box's own total rendered
	// width, windowStyle.Render's own contract - see GetContentSize's doc
	// comment) exactly equals the budget it was given, not 90% of it.
	w.width = width - windowStyle.GetHorizontalFrameSize()
	if w.width < 0 {
		// The collapsed case (app.go's OVERFLOW fix passes width 0 below
		// collapsePreviewBelowWidth): String()'s own "nothing to render" gate
		// checks w.width == 0 exactly, a contract AdjustPreviewWidth(0)==0
		// used to satisfy for free - clamp here so it still does now that
		// this subtracts the border instead of taking 90%.
		w.width = 0
	}
	w.height = height

	// Collapsed (app.go's OVERFLOW fix gives the preview/diff pane zero
	// width below collapsePreviewBelowWidth columns): nothing to size, and
	// the tmux panes underneath must never be asked for a non-positive
	// size - leave the previous valid preview/diff/terminal sizes in place
	// rather than computing negative ones.
	if w.width <= 0 || height <= 0 {
		return
	}

	// Calculate the content height by subtracting:
	// 1. Tab height (including border and padding)
	// 2. Window style vertical frame size
	// 3. Additional padding/spacing (2 for the newline and spacing)
	tabHeight := activeTabStyle.GetVerticalFrameSize() + 1
	contentHeight := height - tabHeight - windowStyle.GetVerticalFrameSize() - 2
	contentWidth := w.width - windowStyle.GetHorizontalFrameSize()

	w.contentWidth, w.contentHeight = contentWidth, contentHeight
	// SessionPane gets w.width (the box's own INTERIOR, before this second
	// border subtraction), not contentWidth: the reading layout's own 1-
	// column-each-side padding at wide sizes (SESSION-READING-SPEC.md's
	// geometry - "inner 116" vs "content 114") already falls out of exactly
	// this arithmetic once SessionPane owns it, so it needs the wider,
	// unreduced figure to divide up itself (ui/session.go's own pad/gutter/
	// measure helpers). Needs-you and Terminal are untouched by this slice
	// and keep the existing contentWidth (also what the real underlying
	// tmux pane is resized to via GetContentSize - see its own doc comment).
	w.session.SetSize(w.width, contentHeight)
	w.needsYou.SetSize(contentWidth, contentHeight)
	w.terminal.SetSize(contentWidth, contentHeight)
}

// GetContentSize returns the content area every tab shares - Session, Needs
// you and Terminal are all sized identically by SetSize above, so this used
// to read PreviewPane's own width/height specifically; now it reads the
// dimensions SetSize itself computed, which is exactly the same number
// regardless of which pane it came from.
func (w *TabbedWindow) GetContentSize() (width, height int) {
	return w.contentWidth, w.contentHeight
}

func (w *TabbedWindow) Toggle() {
	prev := w.activeTab
	w.activeTab = (w.activeTab + 1) % len(w.tabs)
	w.butterflyOnActiveTabChanged(prev)
}

// SetSessionInfo replaces the Session tab's data for the selected lane (nil
// when nothing is selected - the pane then shows the resting frame).
// Unlike UpdateTerminal below, this is never gated on the active tab: the
// data comes from the feed tick's already-cached LaneTail (cheap), and
// gating it would show stale turns for a beat after switching onto the tab.
func (w *TabbedWindow) SetSessionInfo(info *SessionInfo) {
	w.session.SetInfo(info)
}

// SetSessionFleetCounts passes the resting frame's "lanes live"/"needs you"
// counters through to the Session pane.
func (w *TabbedWindow) SetSessionFleetCounts(live, waiting int) {
	w.session.SetFleetCounts(live, waiting)
}

// TickSpinner advances the Session pane's header/thinking-line spinner by
// one frame (slice 14 rule 1) - called once per app.go's 100ms animation
// tick, regardless of which tab is active, the same "always cheap, always
// running" treatment SetSessionInfo above gets.
func (w *TabbedWindow) TickSpinner() {
	w.session.TickSpinner()
}

// SetButterflyEnabled shows or hides the tab-bar butterfly (slice 21).
// Enabled by default (NewTabbedWindow) - the --no-butterfly flag and the
// matching config key are the caller's own wiring, outside this file.
func (w *TabbedWindow) SetButterflyEnabled(enabled bool) {
	w.butterflyEnabled = enabled
}

// ToggleButterflyEnabled flips the butterfly on or off (design refinement
// 4: "b (shift-b, capital) toggles the butterfly live"). Wiring the actual
// keypress to this method is app.go's own key-dispatch table
// (keys/keys.go, app.go's key switch), both outside this file's fence -
// this method is the capability the key-dispatch leg calls, the same way
// it already calls Toggle/SetActiveTab/ScrollUp for the keys it owns.
func (w *TabbedWindow) ToggleButterflyEnabled() {
	w.butterflyEnabled = !w.butterflyEnabled
}

// TickButterfly advances the tab-bar butterfly's animation by one 100ms
// tick - the same previewTickMsg tick TickSpinner above rides (app.go's
// only forwarding line for this slice). It only ever touches this small
// state struct, never the pane content underneath, so the cost is the
// same "bare counter increment" TickSpinner's own doc comment claims -
// drawing happens in String() below, once, on whichever tick asks for a
// render. A notice (slice 23 rule 3) takes priority over a plain tab
// flight, which takes priority over an idle wander, which only ever starts
// while genuinely at rest.
func (w *TabbedWindow) TickButterfly() {
	if !w.butterflyEnabled {
		return
	}

	if w.butterflyFlying || w.butterflyNoticing {
		w.tickFastBeat()
	} else {
		w.tickRestBeat()
	}

	switch {
	case w.butterflyNoticing:
		w.tickNotice()
	case w.butterflyFlying:
		w.tickFlight()
	case w.butterflyWandering:
		w.tickWander()
	default:
		w.butterflyTicksUntilWander--
		if w.butterflyTicksUntilWander <= 0 {
			w.startWander()
		}
	}
}

// tickFastBeat is the faster wing-beat a real flight or a notice draws
// (design rule 1's "wings beating faster in flight"): a plain two-state
// toggle between the closed and open frames, skipping the two half-beat
// frames the slow rest cycle uses below - a flight is over quickly enough
// that a four-step cycle would just look busy, not calm.
func (w *TabbedWindow) tickFastBeat() {
	w.butterflyFlapPhase++
	if w.butterflyFlapPhase >= butterflyFlapTicksFlying {
		w.butterflyFlapPhase = 0
		if w.butterflyFrame == butterflyClosedFrame {
			w.butterflyFrame = butterflyOpenFrame
		} else {
			w.butterflyFrame = butterflyClosedFrame
		}
	}
}

// tickRestBeat is the four-step "closed, half, open, half" beat at rest
// (design refinement 1): each frame holds for its own share of
// butterflyRestFrameTicks before advancing, wrapping back to closed after
// the second half-beat.
func (w *TabbedWindow) tickRestBeat() {
	w.butterflyFlapPhase++
	if w.butterflyFlapPhase >= butterflyRestFrameTicks[w.butterflyFrame] {
		w.butterflyFlapPhase = 0
		w.butterflyFrame = (w.butterflyFrame + 1) % len(butterflyFrames)
	}
}

// tickFlight advances an in-progress tab-change flight, settling it once
// butterflyFlightTicks have elapsed (design rule 2).
func (w *TabbedWindow) tickFlight() {
	w.butterflyFlightTick++
	if w.butterflyFlightTick >= butterflyFlightTicks {
		w.butterflyFlying = false
		w.butterflyFlightTick = 0
	}
}

// tickNotice advances an in-progress notice through its three phases -
// flying out to the Needs-you tab, hovering, then flying back to whichever
// tab was active when the notice started (or, when the notice began
// already on the Needs-you tab, hovering only - butterflyNoticeNeedsFlight
// is false and the Back phase is skipped entirely).
func (w *TabbedWindow) tickNotice() {
	w.butterflyNoticeTick++
	switch w.butterflyNoticePhase {
	case butterflyNoticeOut:
		if w.butterflyNoticeTick >= butterflyFlightTicks {
			w.butterflyNoticePhase = butterflyNoticeHover
			w.butterflyNoticeTick = 0
		}
	case butterflyNoticeHover:
		if w.butterflyNoticeTick >= butterflyNoticeHoverTicks {
			if w.butterflyNoticeNeedsFlight {
				w.butterflyNoticePhase = butterflyNoticeBack
				w.butterflyNoticeTick = 0
			} else {
				w.butterflyNoticing = false
				w.butterflyNoticeTick = 0
			}
		}
	case butterflyNoticeBack:
		if w.butterflyNoticeTick >= butterflyFlightTicks {
			w.butterflyNoticing = false
			w.butterflyNoticeTick = 0
		}
	}
}

// startWander begins one idle-wander trip from the current rest column
// (design refinement 2): a random drift of three to six columns, clamped
// so it never leaves the tab bar's own width. A width of zero (collapsed
// layout) or a clamp that leaves nowhere to go both just reschedule the
// next attempt rather than wander in place.
func (w *TabbedWindow) startWander() {
	if w.width <= 0 {
		w.butterflyTicksUntilWander = w.nextWanderTicks()
		return
	}
	col, _ := w.butterflyPosition()
	drift := butterflyWanderMinDriftCols + w.butterflyRand.Intn(butterflyWanderMaxDriftCols-butterflyWanderMinDriftCols+1)
	if w.butterflyRand.Intn(2) == 0 {
		drift = -drift
	}
	target := col + drift
	maxCol := w.width + windowStyle.GetHorizontalFrameSize() - 1
	if target < 0 {
		target = 0
	}
	if target > maxCol {
		target = maxCol
	}
	if target == col {
		w.butterflyTicksUntilWander = w.nextWanderTicks()
		return
	}
	w.butterflyWandering = true
	w.butterflyWanderPhase = butterflyWanderOut
	w.butterflyWanderTick = 0
	w.butterflyWanderFromCol = col
	w.butterflyWanderToCol = target
}

// tickWander advances an in-progress wander through its three phases -
// drifting out, pausing at the far point, then drifting back - rescheduling
// the next wander once it lands.
func (w *TabbedWindow) tickWander() {
	w.butterflyWanderTick++
	switch w.butterflyWanderPhase {
	case butterflyWanderOut:
		if w.butterflyWanderTick >= butterflyWanderTravelTicks {
			w.butterflyWanderPhase = butterflyWanderPause
			w.butterflyWanderTick = 0
		}
	case butterflyWanderPause:
		if w.butterflyWanderTick >= butterflyWanderPauseTicks {
			w.butterflyWanderPhase = butterflyWanderBack
			w.butterflyWanderTick = 0
		}
	case butterflyWanderBack:
		if w.butterflyWanderTick >= butterflyWanderTravelTicks {
			w.butterflyWandering = false
			w.butterflyWanderTick = 0
			w.butterflyTicksUntilWander = w.nextWanderTicks()
		}
	}
}

// NoticeNeedsYou is app.go's own feed-tick hook (design refinement 3):
// called every feed tick with the freshly ranked Needs-you rows, it starts
// a short flight to the Needs-you tab the moment a row carrying a board
// issue number this call has never seen before appears - "the feed rebuild
// brings a Needs-you row that was not there before". The very first call
// only primes the seen-set and never flies: every row already on the board
// when the cockpit starts would otherwise "arrive" as new the moment the
// first tick runs, sending the butterfly off on a flight nobody asked for.
// A tick that reports the exact same rows as last time (the common case -
// RankedNeedsYou/DiscoverExternalLanes both re-read on the same 3s cadence)
// never flies either - only a number this call has not already recorded.
func (w *TabbedWindow) NoticeNeedsYou(items []clarity.FeedItem) {
	firstCall := w.butterflySeenIssues == nil
	seenNow := make(map[int]bool, len(items))
	isNew := false
	for _, item := range items {
		n, ok := clarity.BoardIssueNumber(item.Source)
		if !ok {
			continue
		}
		seenNow[n] = true
		if !firstCall && !w.butterflySeenIssues[n] {
			isNew = true
		}
	}
	w.butterflySeenIssues = seenNow
	if firstCall || !isNew || !w.butterflyEnabled {
		return
	}
	w.startNotice()
}

// startNotice begins a notice: a hover only, in place, if the Needs-you
// tab is already active ("If the Needs you tab IS the active tab it just
// hovers in place" - design refinement 3), otherwise a full flight out,
// hover, then flight back to whatever tab was active. Cancels any
// in-progress wander or tab flight first, capturing the current column as
// the outbound leg's own departure point the same way
// butterflyOnActiveTabChanged does for a real tab change.
func (w *TabbedWindow) startNotice() {
	if w.activeTab == NeedsYouTab {
		w.butterflyWandering = false
		w.butterflyNoticing = true
		w.butterflyNoticeNeedsFlight = false
		w.butterflyNoticePhase = butterflyNoticeHover
		w.butterflyNoticeTick = 0
		w.butterflyNoticeReturnTab = w.activeTab
		return
	}
	col, _ := w.butterflyPosition()
	w.butterflyWandering = false
	w.butterflyFlying = false
	w.butterflyFlightTick = 0
	w.butterflyNoticing = true
	w.butterflyNoticeNeedsFlight = true
	w.butterflyNoticePhase = butterflyNoticeOut
	w.butterflyNoticeTick = 0
	w.butterflyNoticeFromCol = col
	w.butterflyNoticeReturnTab = w.activeTab
}

// butterflyOnActiveTabChanged starts a flight from wherever the butterfly
// currently sits (its rest column, or - if a second tab change interrupts
// an earlier flight, wander or notice - its current in-flight column) to
// the newly active tab. A no-op if the active tab did not actually change
// (Toggle/SetActiveTab call this unconditionally; most SetActiveTab calls
// in app.go re-assert the tab that is already active).
func (w *TabbedWindow) butterflyOnActiveTabChanged(prevActiveTab int) {
	if w.activeTab == prevActiveTab || !w.butterflyEnabled {
		return
	}
	if w.width > 0 {
		col, _ := w.butterflyPosition()
		w.butterflyFromCol = col
	}
	w.butterflyWandering = false
	w.butterflyNoticing = false
	w.butterflyRestTab = w.activeTab
	w.butterflyFlying = true
	w.butterflyFlightTick = 0
}

// butterflyTabCenterCol returns the column, in the tab row's own
// coordinate space (String()'s totalTabWidth below), directly above the
// centre of tab index tab's name - the rest position, and every flight's
// start and end point.
func (w *TabbedWindow) butterflyTabCenterCol(tab int) int {
	totalTabWidth := w.width + windowStyle.GetHorizontalFrameSize()
	n := len(w.tabs)
	tabWidth := totalTabWidth / n
	width := tabWidth
	if tab == n-1 {
		width = totalTabWidth - tabWidth*(n-1)
	}
	return tab*tabWidth + width/2
}

// butterflyFlightFrac is the shared "in flight" math every kind of
// airborne movement this file animates uses: a fraction t (0..1) of the
// way from fromCol to toCol, with a gentle sine wobble throughout the
// design's own "gentle wander" rule asks for, and - only when overshoot is
// set, the tab-change flights and notice legs but not the idle wander - a
// short overshoot-and-settle in the final stretch (design rule 2). lifted
// reports whether this instant belongs on the free spacer row rather than
// the tab bar's own border row - lift off shortly after leaving, land
// shortly before arriving, so the flight visibly departs from and returns
// to the border row rather than teleporting onto the spacer row.
func butterflyFlightFrac(fromCol, toCol int, t float64, overshoot bool) (col int, lifted bool) {
	base := float64(fromCol) + t*float64(toCol-fromCol)
	wobble := math.Sin(2*math.Pi*butterflyWobbleCycles*t) * (1 - t)

	extra := 0.0
	if overshoot {
		const overshootStart = 0.8
		if t >= overshootStart {
			dir := 1.0
			if toCol < fromCol {
				dir = -1.0
			}
			extra = 2 * dir * math.Sin(math.Pi*(t-overshootStart)/(1-overshootStart))
		}
	}

	col = int(math.Round(base + wobble + extra))
	lifted = t >= 0.12 && t <= 0.88
	return col, lifted
}

// butterflyPosition returns the butterfly's current column and which row
// it draws on: 0 is the tab bar's own border row (rest, and the start/end
// instant of every flight, notice or wander leg), -1 is the free spacer
// row directly above it (String()'s own "lifts off" row, the design's "one
// row up if there is a free row" branch; the tab bar always has one here,
// see this leg's report). A notice takes priority over a plain tab flight,
// which takes priority over an idle wander, which is a pure function of
// butterflyRestTab whenever none of the three is under way.
func (w *TabbedWindow) butterflyPosition() (col int, row int) {
	if w.butterflyNoticing {
		return w.butterflyNoticePosition()
	}
	if w.butterflyFlying {
		return w.butterflyFlightPosition()
	}
	if w.butterflyWandering {
		return w.butterflyWanderPosition()
	}
	return w.butterflyTabCenterCol(w.butterflyRestTab), 0
}

// butterflyFlightPosition is a real tab-change flight's own position -
// unchanged behaviour from slice 21, now expressed via the shared
// butterflyFlightFrac helper.
func (w *TabbedWindow) butterflyFlightPosition() (col int, row int) {
	target := w.butterflyTabCenterCol(w.butterflyRestTab)
	t := float64(w.butterflyFlightTick) / float64(butterflyFlightTicks)
	c, lifted := butterflyFlightFrac(w.butterflyFromCol, target, t, true)
	if lifted {
		return c, -1
	}
	return c, 0
}

// butterflyNoticePosition is a notice's own position across its three
// phases: flying out to the Needs-you tab's centre, hovering there, then
// flying back to whatever tab was active when the notice started.
func (w *TabbedWindow) butterflyNoticePosition() (col int, row int) {
	target := w.butterflyTabCenterCol(NeedsYouTab)
	switch w.butterflyNoticePhase {
	case butterflyNoticeOut:
		t := float64(w.butterflyNoticeTick) / float64(butterflyFlightTicks)
		c, lifted := butterflyFlightFrac(w.butterflyNoticeFromCol, target, t, true)
		if lifted {
			return c, -1
		}
		return c, 0
	case butterflyNoticeBack:
		home := w.butterflyTabCenterCol(w.butterflyNoticeReturnTab)
		t := float64(w.butterflyNoticeTick) / float64(butterflyFlightTicks)
		c, lifted := butterflyFlightFrac(target, home, t, true)
		if lifted {
			return c, -1
		}
		return c, 0
	default: // butterflyNoticeHover
		return target, 0
	}
}

// butterflyWanderPosition is an idle wander's own position across its
// three phases: drifting out, pausing at the far point (lifted, on the
// free spacer row throughout the pause), then drifting back - the wobble
// design refinement 2 asks for ("drifts... with the flight wobble"), but
// no overshoot: a wander is a small aside, not a second flight.
func (w *TabbedWindow) butterflyWanderPosition() (col int, row int) {
	switch w.butterflyWanderPhase {
	case butterflyWanderOut:
		t := float64(w.butterflyWanderTick) / float64(butterflyWanderTravelTicks)
		c, lifted := butterflyFlightFrac(w.butterflyWanderFromCol, w.butterflyWanderToCol, t, false)
		if lifted {
			return c, -1
		}
		return c, 0
	case butterflyWanderPause:
		return w.butterflyWanderToCol, -1
	default: // butterflyWanderBack
		t := float64(w.butterflyWanderTick) / float64(butterflyWanderTravelTicks)
		c, lifted := butterflyFlightFrac(w.butterflyWanderToCol, w.butterflyWanderFromCol, t, false)
		if lifted {
			return c, -1
		}
		return c, 0
	}
}

// butterflyOverlay splices frame (the current wing glyph) into line at
// visible column col, replacing exactly one column so line's own width is
// unchanged (a card-line rule this leg's own tests hold it to: "the tab
// bar line width is unchanged with the butterfly drawn"). ansi.Cut is
// escape-code aware, so the border's own colour on either side of the
// glyph survives untouched.
func (w *TabbedWindow) butterflyOverlay(line string, col int) string {
	width := ansi.StringWidth(line)
	if col < 0 || col >= width {
		return line
	}
	glyph := butterflyStyle.Render(butterflyFrames[w.butterflyFrame])
	return ansi.Cut(line, 0, col) + glyph + ansi.Cut(line, col+1, width)
}

// SetNeedsYouInfo replaces the Needs-you tab's data for the selected row
// (nil when the cursor is not on one - the pane then shows its own plain
// message). Never gated on the active tab, same reasoning as
// SetSessionInfo above.
func (w *TabbedWindow) SetNeedsYouInfo(info *NeedsYouInfo) {
	w.needsYou.SetInfo(info)
}

// UpdateTerminal updates the terminal pane content for target (see
// TerminalTarget's own doc comment) - only while the Terminal tab is
// active, the same "don't do the work if nobody's looking" gate the tab
// always carried.
func (w *TabbedWindow) UpdateTerminal(target TerminalTarget) error {
	if w.activeTab != TerminalTab {
		return nil
	}
	return w.terminal.UpdateContent(target)
}

// SetTerminalFleetCounts passes the resting frame's "lanes live"/"needs
// you" counters through to the Terminal pane, the same way
// SetSessionFleetCounts does for the Session pane.
func (w *TabbedWindow) SetTerminalFleetCounts(live, waiting int) {
	w.terminal.SetFleetCounts(live, waiting)
}

// Add these new methods for handling scroll events
func (w *TabbedWindow) ScrollUp() {
	switch w.activeTab {
	case SessionTab:
		w.session.ScrollUp()
	case NeedsYouTab:
		w.needsYou.ScrollUp()
	case TerminalTab:
		if err := w.terminal.ScrollUp(); err != nil {
			log.InfoLog.Printf("tabbed window failed to scroll terminal up: %v", err)
		}
	}
}

func (w *TabbedWindow) ScrollDown() {
	switch w.activeTab {
	case SessionTab:
		w.session.ScrollDown()
	case NeedsYouTab:
		w.needsYou.ScrollDown()
	case TerminalTab:
		if err := w.terminal.ScrollDown(); err != nil {
			log.InfoLog.Printf("tabbed window failed to scroll terminal down: %v", err)
		}
	}
}

// IsInSessionTab returns true if the Session tab is currently active
func (w *TabbedWindow) IsInSessionTab() bool {
	return w.activeTab == SessionTab
}

// IsInNeedsYouTab returns true if the Needs-you tab is currently active
func (w *TabbedWindow) IsInNeedsYouTab() bool {
	return w.activeTab == NeedsYouTab
}

// IsInTerminalTab returns true if the terminal tab is currently active
func (w *TabbedWindow) IsInTerminalTab() bool {
	return w.activeTab == TerminalTab
}

// GetActiveTab returns the currently active tab index
func (w *TabbedWindow) GetActiveTab() int {
	return w.activeTab
}

// SetActiveTab jumps directly to tab, a no-op outside [0, len(tabs)) - used
// by app.go's tab-follows-row-kind rule (slice 5's "selecting a Needs-you
// row changes the right pane's active tab to Needs you; selecting a lane
// row returns it to Session") to set the tab programmatically, unlike
// Toggle's own one-step cycle.
func (w *TabbedWindow) SetActiveTab(tab int) {
	if tab < 0 || tab >= len(w.tabs) {
		return
	}
	prev := w.activeTab
	w.activeTab = tab
	w.butterflyOnActiveTabChanged(prev)
}

// AttachTerminal attaches to an external lane's own term_<lane> tmux
// session - a tracked row attaches through its own session instead
// (session.List's Attach, app.go's KeyEnter handler), never through here.
func (w *TabbedWindow) AttachTerminal(lane string) (chan struct{}, error) {
	return w.terminal.Attach(lane)
}

// CleanupTerminal closes every cached term_<lane> session - called when the
// cockpit quits (app.go's handleQuit).
func (w *TabbedWindow) CleanupTerminal() {
	w.terminal.Close()
}

// CleanupTerminalForLane closes the cached term_<lane> session for one
// external lane.
func (w *TabbedWindow) CleanupTerminalForLane(lane string) {
	w.terminal.CloseForLane(lane)
}

// IsTerminalInScrollMode returns true if the terminal pane is in scroll mode
func (w *TabbedWindow) IsTerminalInScrollMode() bool {
	return w.terminal.IsScrolling()
}

// ResetTerminalToNormalMode exits scroll mode on the terminal pane
func (w *TabbedWindow) ResetTerminalToNormalMode() {
	w.terminal.ResetToNormalMode()
}

func (w *TabbedWindow) String() string {
	if w.width == 0 || w.height == 0 {
		return ""
	}

	var renderedTabs []string

	totalTabWidth := w.width + windowStyle.GetHorizontalFrameSize()
	tabWidth := totalTabWidth / len(w.tabs)
	lastTabWidth := totalTabWidth - tabWidth*(len(w.tabs)-1)
	tabHeight := activeTabStyle.GetVerticalFrameSize() + 1 // get padding border margin size + 1 for character height

	for i, t := range w.tabs {
		width := tabWidth
		if i == len(w.tabs)-1 {
			width = lastTabWidth
		}

		var style lipgloss.Style
		isFirst, isLast, isActive := i == 0, i == len(w.tabs)-1, i == w.activeTab
		if isActive {
			style = activeTabStyle
		} else {
			style = inactiveTabStyle
		}
		border, _, _, _, _ := style.GetBorder()
		if isFirst && isActive {
			border.BottomLeft = "│"
		} else if isFirst {
			border.BottomLeft = "├"
		} else if isLast && isActive {
			border.BottomRight = "│"
		} else if isLast {
			border.BottomRight = "┤"
		}
		style = style.Border(border)
		// lipgloss/v2's own Width() is border-box (the final rendered width
		// IS the value given, border included) - subtracting the border's
		// own frame size here was double-counting it, rendering every tab 2
		// columns narrower than its own share of totalTabWidth and leaving
		// the tab row's own right edge 6 columns short of the window box
		// below it (3 tabs x 2 columns) - part of slice 13's "the tab bar at
		// col 148" defect, proven empirically against this same lipgloss
		// version (a bordered box's own Render at Width(8) measures exactly
		// 8 columns wide, not 8+frame).
		style = style.Width(width)
		renderedTabs = append(renderedTabs, style.Render(t))
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)

	// Butterfly overlay (slice 21): the two blank rows above the tab bar
	// (this used to be a single "\n" block - see git history - now built
	// explicitly so the top-of-flight spacer row is addressable) and the
	// tab row's own border line (row's first of its three lines - border,
	// names, bottom border). Never drawn over the tab names one line down.
	topSpacer := strings.Repeat(" ", totalTabWidth)
	bottomSpacer := topSpacer
	if w.butterflyEnabled {
		if col, brow := w.butterflyPosition(); brow == -1 {
			bottomSpacer = w.butterflyOverlay(bottomSpacer, col)
		} else if lines := strings.SplitN(row, "\n", 2); len(lines) == 2 {
			row = w.butterflyOverlay(lines[0], col) + "\n" + lines[1]
		}
	}

	var content string
	switch w.activeTab {
	case SessionTab:
		content = w.session.String()
	case NeedsYouTab:
		content = w.needsYou.String()
	case TerminalTab:
		content = w.terminal.String()
	}
	window := windowStyle.Render(
		lipgloss.Place(
			w.width, w.height-2-windowStyle.GetVerticalFrameSize()-tabHeight,
			lipgloss.Left, lipgloss.Top, content))

	return lipgloss.JoinVertical(lipgloss.Left, topSpacer, bottomSpacer, row, window)
}
