package ui

import (
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"claude-squad/log"
	"claude-squad/session"
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
// tmux capture. The old PreviewPane type is left in the tree, unused by
// this window for now - see NewTabbedWindow's own comment.
const (
	SessionTab int = iota
	DiffTab
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
	diff     *DiffPane
	terminal *TerminalPane
}

// NewTabbedWindow wires the three tabs: Session (this fork's slice-3
// replacement for the old tmux-capture Preview pane), Diff and Terminal
// (both untouched in this slice - see DECISIONS.md's build-slice list,
// items 5 and 6). PreviewPane's own type/tests are kept in the tree,
// dormant, for a later slice to relocate its tmux-mirror capability under
// the Terminal tab per DECISIONS.md's tab-3 definition - nothing upstream
// is thrown away, it simply has no tab slot pointed at it right now.
func NewTabbedWindow(session *SessionPane, diff *DiffPane, terminal *TerminalPane) *TabbedWindow {
	return &TabbedWindow{
		tabs: []string{
			"Session",
			"Diff",
			"Terminal",
		},
		session:  session,
		diff:     diff,
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
	w.diff.SetSize(contentWidth, contentHeight)
	w.terminal.SetSize(contentWidth, contentHeight)
}

// GetContentSize returns the content area every tab shares - Session, Diff
// and Terminal are all sized identically by SetSize above, so this used to
// read PreviewPane's own width/height specifically; now it reads the
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
// Unlike UpdateDiff/UpdateTerminal below, this is never gated on the active
// tab: the data comes from the feed tick's already-cached LaneTail (cheap),
// and gating it would show stale turns for a beat after switching onto the
// tab.
func (w *TabbedWindow) SetSessionInfo(info *SessionInfo) {
	w.session.SetInfo(info)
}

// SetSessionFleetCounts passes the resting frame's "lanes live"/"needs you"
// counters through to the Session pane.
func (w *TabbedWindow) SetSessionFleetCounts(live, waiting int) {
	w.session.SetFleetCounts(live, waiting)
}

func (w *TabbedWindow) UpdateDiff(instance *session.Instance) {
	if w.activeTab != DiffTab {
		return
	}
	w.diff.SetDiff(instance)
}

// UpdateTerminal updates the terminal pane content. Only updates when terminal tab is active.
func (w *TabbedWindow) UpdateTerminal(instance *session.Instance) error {
	if w.activeTab != TerminalTab {
		return nil
	}
	return w.terminal.UpdateContent(instance)
}

// Add these new methods for handling scroll events
func (w *TabbedWindow) ScrollUp() {
	switch w.activeTab {
	case SessionTab:
		w.session.ScrollUp()
	case DiffTab:
		w.diff.ScrollUp()
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
	case DiffTab:
		w.diff.ScrollDown()
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

// IsInDiffTab returns true if the diff tab is currently active
func (w *TabbedWindow) IsInDiffTab() bool {
	return w.activeTab == DiffTab
}

// IsInTerminalTab returns true if the terminal tab is currently active
func (w *TabbedWindow) IsInTerminalTab() bool {
	return w.activeTab == TerminalTab
}

// GetActiveTab returns the currently active tab index
func (w *TabbedWindow) GetActiveTab() int {
	return w.activeTab
}

// AttachTerminal attaches to the terminal tmux session
func (w *TabbedWindow) AttachTerminal() (chan struct{}, error) {
	return w.terminal.Attach()
}

// CleanupTerminal closes the terminal session
func (w *TabbedWindow) CleanupTerminal() {
	w.terminal.Close()
}

// CleanupTerminalForInstance closes the cached terminal session for the given instance title.
func (w *TabbedWindow) CleanupTerminalForInstance(title string) {
	w.terminal.CloseForInstance(title)
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
	case DiffTab:
		content = w.diff.String()
	case TerminalTab:
		content = w.terminal.String()
	}
	window := windowStyle.Render(
		lipgloss.Place(
			w.width, w.height-2-windowStyle.GetVerticalFrameSize()-tabHeight,
			lipgloss.Left, lipgloss.Top, content))

	return lipgloss.JoinVertical(lipgloss.Left, "\n", row, window)
}
