package ui

import (
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"claude-squad/log"
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
		session:  session,
		needsYou: needsYou,
		terminal: terminal,
	}
}

// AdjustPreviewWidth adjusts the width of the preview pane to be 90% of the provided width.
func AdjustPreviewWidth(width int) int {
	return int(float64(width) * 0.9)
}

func (w *TabbedWindow) SetSize(width, height int) {
	w.width = AdjustPreviewWidth(width)
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
	w.session.SetSize(contentWidth, contentHeight)
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
	w.activeTab = (w.activeTab + 1) % len(w.tabs)
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
	w.activeTab = tab
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
		style = style.Width(width - style.GetHorizontalFrameSize())
		renderedTabs = append(renderedTabs, style.Render(t))
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)
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

	return lipgloss.JoinVertical(lipgloss.Left, "\n", row, window)
}
