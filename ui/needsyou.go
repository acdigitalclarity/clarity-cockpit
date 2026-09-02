// Package ui: this file is the Needs-you tab (design/cockpit-pane/
// DECISIONS.md item 2, slice 5) - the selected Needs-you row expanded: its
// rank/title, the lane that raised it and its priority, the plain-words
// explanation, and the recommended response, with the same wired composer
// SessionPane draws at its own foot. Renders a plain "nothing selected"
// message when the cursor is not currently on a Needs-you row (the tab can
// still be visited manually via Tab while it holds no row, unlike Session's
// own splash-only rule for that state).
package ui

import (
	"claude-squad/session/clarity"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
)

// needsYouFixedTopRows is the row cost of the two header lines (rank/title,
// lane/priority) - always reserved above the scrollable explanation/
// recommendation region.
const needsYouFixedTopRows = 2

// needsYouFixedBottomRows is the row cost of the three-line composer box,
// same fixed footprint as the Session pane's own (sessionFixedBottomRows
// minus its rule+state line, since this tab has no state line of its own).
const needsYouFixedBottomRows = 3

// NeedsYouInfo is everything one Needs-you tab render needs for the
// SELECTED row, resolved once per feed tick (app.go's feedTickMsg) and
// handed to NeedsYouPane.SetInfo. nil means the cursor is not currently on
// a Needs-you row.
type NeedsYouInfo struct {
	// Item is the selected row itself - Rank/Title for line 1, Lane/Class
	// (there is no separate "priority" field on the feed item; Class fills
	// that role - session/clarity/feed.go) for line 2.
	Item clarity.FeedItem
	// Explanation/Recommendation are the plain-words body and recommended
	// response - from the board fetch (clarity.BoardCache) when Item names
	// a board issue (clarity.BoardIssueNumber), or the lane-file fallback
	// text ("no recommendation on the row") when it does not.
	Explanation    string
	Recommendation string
	// BoardUnreachable carries the fetch failure reason - rendered as the
	// tab's ENTIRE content instead of Explanation/Recommendation when set,
	// per the brief ("render one plain line").
	BoardUnreachable string
	// Loading is true for the one tick between selecting a row whose board
	// fetch has not resolved yet and the background fetch completing - the
	// fetch itself never blocks this tab's render (app.go dispatches it as
	// a tea.Cmd), so this tick shows a plain "fetching" line instead of a
	// stalled UI.
	Loading bool
}

// NeedsYouPane renders the Needs-you tab.
type NeedsYouPane struct {
	width, height int

	info *NeedsYouInfo
	// lastRow remembers which row info last held, so refreshViewport can
	// tell "the same row's data refreshed" (keep the scroll position) from
	// "a different row is now selected" (reset to the top - a fresh row's
	// explanation always starts from its own beginning, never wherever the
	// previous row's scroll happened to be).
	lastRow *clarity.FeedItem

	viewport viewport.Model
	composer *Composer
}

// NewNeedsYouPane returns an empty NeedsYouPane - SetSize and SetInfo still
// need calling before String() shows anything meaningful.
func NewNeedsYouPane() *NeedsYouPane {
	return &NeedsYouPane{viewport: viewport.New(), composer: NewComposer()}
}

// SetComposer wires the shared Composer app.go owns into this pane's own
// render - called once at construction, not per-tick.
func (p *NeedsYouPane) SetComposer(c *Composer) {
	p.composer = c
}

// SetSize sets the pane's own content dimensions (the tabbed window's
// contentWidth/contentHeight - the same size every tab receives).
func (p *NeedsYouPane) SetSize(width, height int) {
	p.width, p.height = width, height
	p.refreshViewport()
}

// SetInfo replaces the selected row's data. nil clears the selection -
// String() then shows the "nothing selected" message.
func (p *NeedsYouPane) SetInfo(info *NeedsYouInfo) {
	p.info = info
	p.refreshViewport()
}

// Clear is SetInfo(nil).
func (p *NeedsYouPane) Clear() {
	p.SetInfo(nil)
}

func (p *NeedsYouPane) contentAreaHeight() int {
	h := p.height - needsYouFixedTopRows - needsYouFixedBottomRows
	if h < 0 {
		h = 0
	}
	return h
}

// sameRow reports whether info's own Item is the same row lastRow last
// held - compared by Rank+Source, the pair a queue rebuild never reassigns
// to a different row (Title/Class can change between ticks on a live
// re-rank; Rank+Source is the row's own stable identity within one queue
// snapshot).
func sameNeedsYouRow(a, b *clarity.FeedItem) bool {
	if a == nil || b == nil {
		return false
	}
	return a.Rank == b.Rank && a.Source == b.Source
}

// refreshViewport rebuilds the explanation/recommendation viewport's
// content - called whenever the size or the selected row's data changes.
// Resets to the top on a genuinely different row; preserves the scroll
// offset when the same row's data merely refreshed (a board fetch
// resolving, or a plain resize).
func (p *NeedsYouPane) refreshViewport() {
	prevOffset := p.viewport.YOffset()
	var curRow *clarity.FeedItem
	if p.info != nil {
		item := p.info.Item
		curRow = &item
	}
	keepOffset := sameNeedsYouRow(curRow, p.lastRow)

	p.viewport.SetWidth(p.width)
	p.viewport.SetHeight(p.contentAreaHeight())
	if p.info == nil {
		p.viewport.SetContent("")
		p.lastRow = nil
		return
	}
	lines := buildNeedsYouContentLines(*p.info, p.width)
	p.viewport.SetContent(strings.Join(lines, "\n"))
	if keepOffset {
		p.viewport.SetYOffset(prevOffset)
	} else {
		p.viewport.GotoTop()
	}
	p.lastRow = curRow
}

// ScrollUp/ScrollDown scroll the explanation/recommendation region - the
// same shift+up/shift+down keys the Session tab scrolls with (the brief's
// "scroll with the same keys as Session").
func (p *NeedsYouPane) ScrollUp() {
	p.viewport.ScrollUp(1)
}

func (p *NeedsYouPane) ScrollDown() {
	p.viewport.ScrollDown(1)
}

// String renders the pane: the "nothing selected" message when info is
// nil, or exactly p.height lines of header/content/composer otherwise -
// every line bounded to p.width (the FINISH requirement).
func (p *NeedsYouPane) String() string {
	if p.width <= 0 || p.height <= 0 {
		h := p.height
		if h < 0 {
			h = 0
		}
		return strings.Repeat("\n", h)
	}
	if p.info == nil {
		return p.renderNothingSelected()
	}

	lines := make([]string, 0, p.height)
	lines = append(lines, p.renderHeaderLine1(), p.renderHeaderLine2())

	contentHeight := p.contentAreaHeight()
	p.viewport.SetHeight(contentHeight)
	p.viewport.SetWidth(p.width)
	body := strings.Split(p.viewport.View(), "\n")
	if contentHeight <= 0 {
		body = nil
	} else if len(body) < contentHeight {
		body = append(body, make([]string, contentHeight-len(body))...)
	} else if len(body) > contentHeight {
		body = body[:contentHeight]
	}
	lines = append(lines, body...)
	lines = append(lines, p.renderComposerLines()...)

	if len(lines) > p.height {
		lines = lines[:p.height]
	} else if len(lines) < p.height {
		lines = append(lines, make([]string, p.height-len(lines))...)
	}
	for i, l := range lines {
		lines[i] = fitRow(l, p.width)
	}
	return strings.Join(lines, "\n")
}

// renderNothingSelected draws a plain centred message - shown whenever the
// tab is visited (manually, via Tab) while the cursor is not on a
// Needs-you row.
func (p *NeedsYouPane) renderNothingSelected() string {
	return lipgloss.Place(p.width, p.height, lipgloss.Center, lipgloss.Center,
		sessionMutedStyle.Render("select a Needs-you row to see it here"))
}

// renderHeaderLine1 is "<rank>. <title>" - the row's own position in the
// ranked queue, not the board issue number line 2 carries.
func (p *NeedsYouPane) renderHeaderLine1() string {
	item := p.info.Item
	return sessionTextStyle.Render(ansiTruncateRow(fmt.Sprintf("%d. %s", item.Rank, item.Title), p.width))
}

// renderHeaderLine2 is "<lane> · <priority>" - Lane is the row's raising
// lane (for a board-sourced row this is the issue number string itself,
// "#277" - laneFromSource's own fallback, session/clarity/feed.go; there is
// no separate lane name to derive from a bare issue reference), Class fills
// the "priority" role the feed item itself has no dedicated field for.
func (p *NeedsYouPane) renderHeaderLine2() string {
	item := p.info.Item
	return sessionMutedStyle.Render(ansiTruncateRow(fmt.Sprintf("%s · %s", item.Lane, item.Class), p.width))
}

// renderComposerLines draws the composer box, targeted at this row's own
// raising lane whenever the composer is not already open on a captured
// target.
func (p *NeedsYouPane) renderComposerLines() []string {
	return p.composer.Render(p.width, p.info.Item.Lane)
}

// buildNeedsYouContentLines renders the scrollable region: a rule, the
// explanation, another rule, "Recommended response:" and the
// recommendation - or, on a board fetch failure, the single "board
// unreachable" line the brief names, with nothing else (RECOMMEND
// response omitted, never left dangling above an empty answer). A pending
// fetch (Loading) shows one plain line instead of stalling.
func buildNeedsYouContentLines(info NeedsYouInfo, width int) []string {
	if info.Loading {
		return []string{muteRule(width), "fetching the board row…"}
	}
	if info.BoardUnreachable != "" {
		lines := []string{muteRule(width)}
		return append(lines, wrapPlain(collapseWS("board unreachable: "+info.BoardUnreachable), width)...)
	}
	var lines []string
	lines = append(lines, muteRule(width))
	lines = append(lines, wrapPlain(collapseWS(info.Explanation), width)...)
	lines = append(lines, muteRule(width))
	lines = append(lines, "Recommended response:")
	lines = append(lines, wrapPlain(collapseWS(info.Recommendation), width)...)
	return lines
}
