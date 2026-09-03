package ui

import (
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"claude-squad/log"
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
// serenity while working so many things"). Two glyphs read as open and
// closed wings and are both proven single-width (StringWidth == 1,
// tabbed_window_test.go's TestButterfly_FramesAreSingleWidth): U+029A/
// U+025E, a mirror pair of lowercase IPA letters chosen over the emoji/
// symbol candidates (rendered side by side in a scratch tmux pane at 164
// columns, PROOF section of this leg's report) for reading as a small,
// calm, rounded wing-open/wing-closed pair rather than a technical glyph
// ("⌘") or a single ornament with no natural "closed" counterpart ("ꕥ").
var butterflyFrames = [2]string{"ʚ", "ɞ"}

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
	// butterflyFlapTicksRest is how many 100ms ticks separate one rest flap
	// from the next - "flaps once every 1.2s or so".
	butterflyFlapTicksRest = 12
	// butterflyFlapTicksFlying is the faster in-flight flap cadence -
	// "wings beating faster in flight".
	butterflyFlapTicksFlying = 3
	// butterflyFlightTicks is how many 100ms ticks a flight between tabs
	// takes - "drifts... over about 1.5s on the 100ms tick", and well
	// inside the "ends... within 20 ticks" test bound.
	butterflyFlightTicks = 15
	// butterflyWobbleCycles is how many full sine wobbles a flight makes
	// end to end - "a gentle wander (a sine wobble of one column)".
	butterflyWobbleCycles = 2.0
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

	// Butterfly state (slice 21) - see the butterflyFrames/butterflyStyle
	// doc comment above and TickButterfly/butterflyPosition below for the
	// state machine. SetButterflyEnabled defaults true (NewTabbedWindow);
	// wiring the --no-butterfly flag and matching config key into it is the
	// caller's job (main.go/config.go), outside this file's own fence.
	butterflyEnabled    bool
	butterflyRestTab    int  // the tab index the butterfly rests over, or flies towards
	butterflyFlying     bool // mid-flight between two tabs
	butterflyFlightTick int  // 0..butterflyFlightTicks-1 while flying
	butterflyFromCol    int  // column (tab-row coordinate space) the current flight departs from
	butterflyFlapPhase  int  // ticks since the last frame flip
	butterflyFrame      int  // 0 or 1, indexes butterflyFrames
}

// NewTabbedWindow wires the three tabs: Session (slice 3's replacement for
// the old tmux-capture Preview pane), Needs you (slice 5's replacement for
// the old Diff pane) and Terminal (untouched in this slice - see
// DECISIONS.md's build-slice list, item 6). PreviewPane and DiffPane are
// both kept in the tree, dormant - nothing upstream is thrown away, neither
// simply has a tab slot pointed at it any more.
func NewTabbedWindow(session *SessionPane, needsYou *NeedsYouPane, terminal *TerminalPane) *TabbedWindow {
	return &TabbedWindow{
		tabs: []string{
			"Session",
			"Needs you",
			"Terminal",
		},
		session:          session,
		needsYou:         needsYou,
		terminal:         terminal,
		butterflyEnabled: true,
	}
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

// TickButterfly advances the tab-bar butterfly's animation by one 100ms
// tick - the same previewTickMsg tick TickSpinner above rides (app.go's
// only forwarding line for this slice). It only ever touches this small
// state struct, never the pane content underneath, so the cost is the
// same "bare counter increment" TickSpinner's own doc comment claims -
// drawing happens in String() below, once, on whichever tick asks for a
// render.
func (w *TabbedWindow) TickButterfly() {
	if !w.butterflyEnabled {
		return
	}
	flapTicks := butterflyFlapTicksRest
	if w.butterflyFlying {
		flapTicks = butterflyFlapTicksFlying
		w.butterflyFlightTick++
		if w.butterflyFlightTick >= butterflyFlightTicks {
			w.butterflyFlying = false
			w.butterflyFlightTick = 0
		}
	}
	w.butterflyFlapPhase++
	if w.butterflyFlapPhase >= flapTicks {
		w.butterflyFlapPhase = 0
		w.butterflyFrame = 1 - w.butterflyFrame
	}
}

// butterflyOnActiveTabChanged starts a flight from wherever the butterfly
// currently sits (its rest column, or - if a second tab change interrupts
// an earlier flight - its current in-flight column) to the newly active
// tab. A no-op if the active tab did not actually change (Toggle/
// SetActiveTab call this unconditionally; most SetActiveTab calls in
// app.go re-assert the tab that is already active).
func (w *TabbedWindow) butterflyOnActiveTabChanged(prevActiveTab int) {
	if w.activeTab == prevActiveTab || !w.butterflyEnabled {
		return
	}
	if w.width > 0 {
		col, _ := w.butterflyPosition()
		w.butterflyFromCol = col
	}
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

// butterflyPosition returns the butterfly's current column and which row
// it draws on: 0 is the tab bar's own border row (rest, and the start/end
// instant of every flight), -1 is the free spacer row directly above it
// (used only mid-flight - String()'s own "lifts off" row, the design's
// "one row up if there is a free row" branch; the tab bar always has one
// here, see this leg's report). At rest, or once a flight has run its
// course, this is a pure function of butterflyRestTab; mid-flight it
// linearly interpolates from butterflyFromCol with a gentle sine wander
// and a short overshoot that settles back onto the target exactly as the
// flight ends (design rule 2).
func (w *TabbedWindow) butterflyPosition() (col int, row int) {
	target := w.butterflyTabCenterCol(w.butterflyRestTab)
	if !w.butterflyFlying {
		return target, 0
	}

	t := float64(w.butterflyFlightTick) / float64(butterflyFlightTicks)
	base := float64(w.butterflyFromCol) + t*float64(target-w.butterflyFromCol)

	wobble := math.Sin(2*math.Pi*butterflyWobbleCycles*t) * (1 - t)

	overshoot := 0.0
	const overshootStart = 0.8
	if t >= overshootStart {
		dir := 1.0
		if target < w.butterflyFromCol {
			dir = -1.0
		}
		overshoot = 2 * dir * math.Sin(math.Pi*(t-overshootStart)/(1-overshootStart))
	}

	col = int(math.Round(base + wobble + overshoot))
	// Lift off shortly after leaving, land shortly before arriving - the
	// row0 edges are what makes the flight visibly depart from and return
	// to the border row rather than teleporting onto the spacer row.
	row = -1
	if t < 0.12 || t > 0.88 {
		row = 0
	}
	return col, row
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
