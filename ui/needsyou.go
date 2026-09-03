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

// needsYouFixedBottomRows no longer names a fixed row cost (slice 16, the
// composer wraps: the shared Composer now grows from 3 to up to 7 rows as
// its text wraps) - contentAreaHeight reads the current cost live from the
// Composer's own Height instead, the same wiring ui/session.go's
// turnsAreaHeight uses, so this tab's scrollable region shrinks in step
// (RULE: "the turns viewport above shrinks by the same rows so nothing
// overlaps").

// NeedsYouInfo is everything one Needs-you tab render needs for the
// SELECTED row, resolved once per feed tick (app.go's feedTickMsg) and
// handed to NeedsYouPane.SetInfo. nil means the cursor is not currently on
// a Needs-you row.
type NeedsYouInfo struct {
	// Item is the selected row itself - Rank/Title for line 1, Class fills
	// line 2's "priority" role (session/clarity/feed.go).
	Item clarity.FeedItem
	// Lane is the row's own RESOLVED raising lane - the composer's send
	// target and line 2's own lane name (board #280, slice 5b, DEFECT 2):
	// for a lane-file-sourced row this is Item.Lane itself (always
	// resolves); for a board-sourced row it is the fetched card's own Lane
	// field (its "## Lane" section, falling back to the issue's "lane:"
	// label) - "" when neither resolves, never the raw "#<n>" source
	// string.
	Lane string
	// Explanation/Options/ExpectedReply/Also are the board fetch's own
	// classified fields (clarity.BoardCache, clarity.ParseBoardBody) when
	// Item names a board issue (clarity.BoardIssueNumber); a lane-file row
	// carries none of these (the feed item itself has no body to parse).
	Explanation   []clarity.BoardSection
	Options       []clarity.BoardOption
	ExpectedReply string
	Also          string
	// BoardUnreachable carries the fetch failure reason - rendered as the
	// tab's ENTIRE content instead of the fields above when set, per the
	// brief ("render one plain line").
	BoardUnreachable string
	// Loading is true for the one tick between selecting a row whose board
	// fetch has not resolved yet and the background fetch completing - the
	// fetch itself never blocks this tab's render (app.go dispatches it as
	// a tea.Cmd), so this tick shows a plain "fetching" line instead of a
	// stalled UI.
	Loading bool
	// Answered is true when this row's own y-key answer has already been
	// sent this session (app.go's in-memory answered-marker set, keyed by
	// board issue number) - never derived from Options itself; the option
	// that was actually chosen and sent swaps its → marker for a ✓
	// (ANSWER-AND-BANK-SPEC.md "Answered marker and its lifetime").
	Answered bool
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

// needsYouGutter/needsYouMeasure are the reading layout's own numbers
// (SESSION-READING-SPEC.md, adopted here verbatim per DECISIONS.md slice
// 13's own ruling, carried into this slice by ANSWER-AND-BANK-SPEC.md's
// "Carried in with this slice"): gutter 2 at the wide size, 1 narrow;
// measure is min(96, width - gutter). Duplicated from ui/session.go's own
// SessionPane.gutter/measure (unexported there, a different file/leg, and
// keyed off contentWidth rather than the raw width this tab receives -
// NeedsYouPane carries no separate pad() inset) since there is no shared
// pane-geometry type today - see that file's own doc comment for the
// underlying rule of thumb.
func needsYouGutter(width int) int {
	if width >= sessionWideMinWidth {
		return 2
	}
	return 1
}

func needsYouMeasure(width int) int {
	m := width - needsYouGutter(width)
	if m > 96 {
		m = 96
	}
	if m < 1 {
		m = 1
	}
	return m
}

func (p *NeedsYouPane) contentAreaHeight() int {
	h := p.height - needsYouFixedTopRows - p.composer.Height(p.width)
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

// renderHeaderLine2 is "<lane> · <priority>" - Lane is the row's own
// RESOLVED raising lane (info.Lane, board #280 slice 5b DEFECT 2), never
// the raw "#<n>" board reference; NoLaneLabel when it did not resolve.
// Class fills the "priority" role the feed item itself has no dedicated
// field for.
func (p *NeedsYouPane) renderHeaderLine2() string {
	lane := p.info.Lane
	if lane == "" {
		lane = NoLaneLabel
	}
	return sessionMutedStyle.Render(ansiTruncateRow(fmt.Sprintf("%s · %s", lane, p.info.Item.Class), p.width))
}

// renderComposerLines draws the composer box, targeted at this row's own
// RESOLVED raising lane (info.Lane) whenever the composer is not already
// open on a captured target.
func (p *NeedsYouPane) renderComposerLines() []string {
	return p.composer.Render(p.width, p.info.Lane)
}

// optionMarker/optionIndent are the "recommended option marked" glyph
// (board #280 slice 5b DEFECT 1) and the matching blank indent every other
// option, and every wrapped continuation line, lines up under.
// answeredOptionMarker replaces optionMarker on the option that was
// actually sent, once the row is answered (ANSWER-AND-BANK-SPEC.md
// "Answered marker": "the card's sent option swaps → for ✓").
const optionMarker = "→ "
const answeredOptionMarker = "✓ "

var optionIndent = strings.Repeat(" ", lipgloss.Width(optionMarker))

// buildNeedsYouContentLines renders the scrollable region: a rule, the
// explanation (the card's own What/Where/Why sections, plain-labeled, or
// its free-prose paragraphs when it has no headings), another rule,
// "Recommended response:" and the card's own Options (the recommended one
// marked), then "Expected reply:" and "Also on the row:" when the card
// carries either - or, on a board fetch failure, the single "board
// unreachable" line the brief names, with nothing else. A pending fetch
// (Loading) shows one plain line instead of stalling.
func buildNeedsYouContentLines(info NeedsYouInfo, width int) []string {
	if info.Loading {
		return []string{muteRule(width), "fetching the board row…"}
	}
	measure := needsYouMeasure(width)
	if info.BoardUnreachable != "" {
		lines := []string{muteRule(width)}
		return append(lines, readingProseLines("board unreachable: "+info.BoardUnreachable, measure)...)
	}
	var lines []string
	lines = append(lines, muteRule(width))
	lines = append(lines, renderExplanationLines(info.Explanation, measure)...)
	lines = append(lines, muteRule(width))
	lines = append(lines, "Recommended response:")
	lines = append(lines, renderOptionLines(info.Options, measure, info.Answered)...)
	if info.ExpectedReply != "" {
		lines = append(lines, "", "Expected reply:")
		lines = append(lines, readingProseLines(info.ExpectedReply, measure)...)
	}
	if info.Also != "" {
		lines = append(lines, "", "Also on the row:")
		lines = append(lines, readingProseLines(info.Also, measure)...)
	}
	return lines
}

// readingProseLines wraps text at measure preserving its own paragraphs and
// list items (session.go's own splitProseBlocks/wrapTokens/tokenizeMarkdown/
// renderTokenLine, same package) - the reading layout's own "rule 1: never
// collapseWS a card's body" (DECISIONS.md slice 13, carried into this slice
// per ANSWER-AND-BANK-SPEC.md's "Carried in with this slice"). Blocks are
// left-flush (no gutter indent): the Needs-you tab reserves its own hanging
// indent for wrapped list items only (splitProseBlocks' own marker), unlike
// the Session tab's turns, which additionally nest every line under a tag/
// time label line this tab has no equivalent of.
func readingProseLines(text string, measure int) []string {
	var lines []string
	for _, block := range splitProseBlocks(text) {
		listMarkerWidth := len([]rune(block.marker))
		wrapWidth := measure - listMarkerWidth
		if wrapWidth < 10 {
			wrapWidth = 10
		}
		for i, wline := range wrapTokens(tokenizeMarkdown(block.text), wrapWidth) {
			prefix := ""
			switch {
			case block.marker == "":
			case i == 0:
				prefix = block.marker
			default:
				prefix = strings.Repeat(" ", listMarkerWidth)
			}
			lines = append(lines, prefix+renderTokenLine(wline))
		}
	}
	return lines
}

// renderExplanationLines renders the What/Where/Why sections in order,
// each a small plain-word label followed by its own wrapped text, blank-
// line separated - or, for the no-headings free-prose fallback (a single
// section with an empty Label), just the wrapped text on its own.
func renderExplanationLines(sections []clarity.BoardSection, measure int) []string {
	if len(sections) == 0 {
		return []string{"no explanation on the row"}
	}
	var lines []string
	for i, s := range sections {
		if i > 0 {
			lines = append(lines, "")
		}
		if s.Label != "" {
			lines = append(lines, s.Label)
		}
		lines = append(lines, readingProseLines(s.Text, measure)...)
	}
	return lines
}

// renderOptionLines renders the card's own Options, one per line (wrapped
// and continuation-indented when an option overruns width), the
// recommended one prefixed with optionMarker and every other line - marked
// or not - left-padded to the same column so the list stays aligned.
func renderOptionLines(opts []clarity.BoardOption, measure int, answered bool) []string {
	if len(opts) == 0 {
		return []string{"no recommendation on the row"}
	}
	chosenIdx := -1
	if answered {
		if _, idx, ok := clarity.ChosenOption(opts); ok {
			chosenIdx = idx
		}
	}
	indentWidth := lipgloss.Width(optionMarker)
	var lines []string
	for i, o := range opts {
		marker := optionIndent
		switch {
		case i == chosenIdx:
			marker = answeredOptionMarker
		case o.Recommended:
			marker = optionMarker
		}
		wrapped := wrapPlain(collapseWS(o.Text), maxInt0(measure-indentWidth))
		for j, w := range wrapped {
			if j == 0 {
				lines = append(lines, marker+w)
			} else {
				lines = append(lines, optionIndent+w)
			}
		}
	}
	return lines
}
