package ui

import (
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/session/clarity"
	"errors"
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// removedLinesStyle is also ui/session.go's own removed-text colour - kept
// here since this file declared it first; addedLinesStyle (its old pair)
// is gone with slice 19's diff badge (see InstanceRenderer.Render's own
// comment: a compact one-row lane carries no room for it, and the
// mock-up's own target row - PANE-MOCKUP-164x45.md - never showed one).
var removedLinesStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#de613e"))

// laneRowSelectedBg/laneRowSelectedFg are the selected row's own band
// (owner, 3 Sep 11:3x, "the highlight of the active session is a bit
// naff" - design/cockpit-pane/CURRENT-ROW-HIGHLIGHT-2026-09-03.png shows
// the old #dde4f0 pale-lavender background as a two-row inverted block
// with a ragged right edge). laneRowSelectedBg is a dark tint of the same
// accent teal laneRowMarkerStyle draws the ▌ marker in below - rule 2's
// "a subtle full-width band (a dark tint of the accent, not the inverted
// pale block)". laneRowSelectedFg is the app's own ordinary text colour
// (the same pair titleStyle already carries), never a foreground forced to
// one value regardless of light/dark mode the way the old selectedTitleStyle
// was - that fixed #1a1a1a is what made the pale block read as "inverted"
// in the first place.
var laneRowSelectedBg = compat.AdaptiveColor{Light: lipgloss.Color("#cdeef0"), Dark: lipgloss.Color("#153a3d")}
var laneRowSelectedFg = compat.AdaptiveColor{Light: lipgloss.Color("#1a1a1a"), Dark: lipgloss.Color("#dddddd")}

// titleStyle/selectedTitleStyle now carry no Padding at all: the marker
// column (laneRowMarker) and the row's own single trailing space
// (laneRowFrame) take over the left/right columns Padding used to add, and
// every row is exactly one line - rule 1's "ONE ROW per lane ... no spacer
// rows" - so the old top/bottom Padding that produced a blank line above
// and below each tracked instance (four instances, twenty rows, the
// owner's own screenshot) is gone with it.
var titleStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#1a1a1a"), Dark: lipgloss.Color("#dddddd")})

var selectedTitleStyle = lipgloss.NewStyle().
	Background(laneRowSelectedBg).
	Foreground(laneRowSelectedFg)

var mainTitle = lipgloss.NewStyle().
	Background(lipgloss.Color("62")).
	Foreground(lipgloss.Color("230"))

var autoYesStyle = lipgloss.NewStyle().
	Background(lipgloss.Color("#dde4f0")).
	Foreground(lipgloss.Color("#1a1a1a"))

var needsYouTitleStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#b5581a"), Dark: lipgloss.Color("#e0a458")})

var needsYouLineStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#5a5a5a"), Dark: lipgloss.Color("#aaaaaa")})

// needsYouLineSelectedStyle is the current Needs-you row's own highlight -
// rule 3's "keep their existing selection treatment but use the same ▌
// marker and band rule for consistency": the same band colour every
// selected row now shares (laneRowSelectedBg), so the one cursor reads as
// one style regardless of which of the three groups it is currently in.
var needsYouLineSelectedStyle = lipgloss.NewStyle().
	Background(laneRowSelectedBg).
	Foreground(laneRowSelectedFg)

var externalTitleStyle = lipgloss.NewStyle().
	Bold(true).
	Padding(1, 1, 0, 1).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#5a5a5a"), Dark: lipgloss.Color("#999999")})

var externalRowStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#777777"), Dark: lipgloss.Color("#999999")})

var externalRowSelectedStyle = lipgloss.NewStyle().
	Background(laneRowSelectedBg).
	Foreground(laneRowSelectedFg)

// modalityHeadingStyle is a modality group heading's own style (slice 5
// item 1) - bold, the same muted grey externalTitleStyle already uses for
// "External lanes", but with NO padding: FRONTDOOR-MOCKUP-164x45.md screen
// 4 draws a group heading directly against the row above it, no blank
// line, unlike externalTitleStyle's own top Padding(1) (that heading is a
// one-off divider between the whole tracked and external blocks, not a
// heading repeated once per group).
var modalityHeadingStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#5a5a5a"), Dark: lipgloss.Color("#999999")})

// fleetLineStyle is the per-seat fleet line's own style (slice 5 item 3) -
// muted like needsYouLineStyle: plain informational text under the
// "Instances" title, never a heading and never selectable.
var fleetLineStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#5a5a5a"), Dark: lipgloss.Color("#aaaaaa")})

// laneRowMarkerStyle draws the ▌ marker rule 2 gives every selected row -
// "a left marker ▌ in the accent (the splash teal used for CLAUDE tags)",
// the same colour pair as ui/session.go's sessionClaudeStyle, repeated here
// (this package's own convention for a shared colour role - #b5581a/
// #e0a458 above is already duplicated the same way between
// needsYouTitleStyle and laneStateStalledStyle) rather than exported,
// since session.go and list.go are legs owned separately (pane-14/pane-19).
var laneRowMarkerStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#0f7f83"), Dark: lipgloss.Color("#54E6EA")})

// laneBranchSuffixFg is the branch suffix's own faint colour (rule 1: "a
// faint suffix") - the same dim tone the old, now-removed second line
// (branchLine, via listDescStyle) used for exactly this text.
var laneBranchSuffixFg = compat.AdaptiveColor{Light: lipgloss.Color("#A49FA5"), Dark: lipgloss.Color("#777777")}

// laneStateWorkingStyle/laneStateWaitingStyle/laneStateStalledStyle/
// laneStateIdleStyle are the state-word row's own colours (rule 2: "working
// teal, waiting orange, stalled orange, idle muted" - test that the state
// colour survives inside the selected row). DEFECT (the owner's own
// screenshot, "the state glyph and word lose their colour inside it"):
// working and waiting on you used to share ONE style
// (laneStateAccentStyle) whose colour, #dde4f0, was the exact same value
// as the selected row's own OLD background - so on a selected row that
// text painted itself the same colour as the band under it and vanished.
// Split three ways here: working gets its own teal (the splash/reading-
// layout accent, same pair as laneRowMarkerStyle above), waiting on you
// shares stalled's orange (both are attention states a lane cannot clear
// without the owner), and neither collides with laneRowSelectedBg's new,
// deliberately distinct dark tint.
var laneStateWorkingStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#0f7f83"), Dark: lipgloss.Color("#54E6EA")})

// laneStateAccentStyle is kept under its old name as an alias for
// laneStateWorkingStyle - ui/session.go's own animated header glyph
// (headerGlyph, a pane-14 file this leg does not touch) still references
// it by this name. The DEFECT this file's state-colour split fixes (see
// laneStateWorkingStyle's own comment above) was always this style's old
// VALUE, #dde4f0, never its name.
var laneStateAccentStyle = laneStateWorkingStyle

var laneStateWaitingStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#b5581a"), Dark: lipgloss.Color("#e0a458")})

var laneStateStalledStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#b5581a"), Dark: lipgloss.Color("#e0a458")})

var laneStateIdleStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#777777"), Dark: lipgloss.Color("#999999")})

// laneStateNeedsKeyStyle is clarity.StateNeedsKey's own colour - bold on
// the waiting/stalled orange, since a live permission prompt is the single
// most urgent thing a row can say (ANSWER-AND-BANK-SPEC.md item 7: "ahead
// of every other word").
var laneStateNeedsKeyStyle = laneStateWaitingStyle.Bold(true)

// laneStateGlyph returns the glyph and style ClassifyState's four words
// render as - working ● its own teal, waiting on you ◉ and stalled ◐ share
// the orange, idle ○ dim. An unknown/not-yet-computed state (the empty
// string, before the first feed tick has classified this lane) draws a
// blank glyph: the row still reserves the column, it simply has nothing to
// show yet, same convention as the ctx gauge's "show nothing, not n/a".
func laneStateGlyph(state string) (string, lipgloss.Style) {
	switch state {
	case clarity.StateWorking:
		return "●", laneStateWorkingStyle
	case clarity.StateWaitingYou:
		return "◉", laneStateWaitingStyle
	case clarity.StateStalled:
		return "◐", laneStateStalledStyle
	case clarity.StateIdle:
		return "○", laneStateIdleStyle
	case clarity.StateNeedsKey:
		return "◆", laneStateNeedsKeyStyle
	default:
		return " ", lipgloss.NewStyle()
	}
}

// laneStateDisplayWord is the WORD a lane row itself shows for state - the
// row's own vocabulary is exactly working/waiting/idle/stalled (THE RULE),
// never ClassifyState's raw StateWaitingYou value ("waiting on you"), which
// only the Session pane's header and state line render in full
// (renderHeaderLine1/renderStateLine in session.go - unchanged by this fix).
func laneStateDisplayWord(state string) string {
	if state == clarity.StateWaitingYou {
		return "waiting"
	}
	return state
}

// laneStateWordWidth is the widest of the row's own four display words
// (laneStateDisplayWord's output: working/waiting/idle/stalled - "working",
// "waiting" and "stalled" are all 7, "idle" is 4). DEFECT: this used to be
// len(clarity.StateWaitingYou), the Session pane's own 14-character "waiting
// on you" phrase that a row never actually renders - reserving 15 columns
// (14 + the separating space) for a word column that only ever shows 7,
// starving the name column by exactly that much on every row.
var laneStateWordWidth = len(clarity.StateWorking)

// lanePctFieldWidth is "100%" - the widest a context-fill percentage ever
// renders (0-100%, the 100% edge case), fixed so the field's own width
// never changes row to row; the percentage right-aligns inside it (item 1's
// "ctx percentage right-aligned"). DEFECT 2 dropped the "ctx" label this
// field used to carry (design/cockpit-pane/DECISIONS.md slice 3b: "the
// percentage without the ctx label") - it stays on the Session pane's own
// header line 1 (ctxBarLabel in session.go), a different component with
// room to spare; the lane row does not.
const lanePctFieldWidth = len("100%")

// laneTimeWidth is "15:04" - the last-turn time, local, hours:minutes.
const laneTimeWidth = len("15:04")

// laneShowTimeMinWidth is THE RULE's own second collapse point, distinct
// from app.go's collapsePreviewBelowWidth (which drops the state WORD): a
// row-inner width under this many columns drops the last-turn TIME entirely
// instead - the mock-up's 120-column rows (PANE-MOCKUP-120x36.md) carry no
// time field at all - so the name column keeps that room rather than the
// clock does.
const laneShowTimeMinWidth = 42

// laneShowTime reports whether a row built from a rowInner budget this wide
// carries the last-turn time field.
func laneShowTime(rowInner int) bool {
	return rowInner >= laneShowTimeMinWidth
}

// laneSuffixWidth is the plain-text width of laneRowSuffix's output for a
// given showWord/showTime pair - kept as an explicit function (not just
// len() on a sample render) so nameCol sizing and the actual render can
// never drift out of step with each other.
func laneSuffixWidth(showWord, showTime bool) int {
	// " " + pct + "  " + glyph
	w := 1 + lanePctFieldWidth + 2 + 1
	if showWord {
		// + " " + word
		w += 1 + laneStateWordWidth
	}
	if showTime {
		// + " " + time
		w += 1 + laneTimeWidth
	}
	return w
}

// laneTagFieldWidth is the seat-tag column's own width (slice 5 item 2,
// FRONTDOOR-SPEC.md "The list": "The account tag costs three columns...
// the name field keeps its full 20") - three columns, left-justified,
// blank when the row carries no tag text at all.
const laneTagFieldWidth = 3

// laneTagGapWidth is the pct-to-glyph gap when the tag column is showing -
// one column narrower than the plain two-space gap laneRowSuffix uses
// without it (the spec's own "the tag takes one of the two spaces before
// the glyph"). Reverse-engineered against FRONTDOOR-MOCKUP-164x45.md screen
// 4's own byte positions (this leg's report): the tag sits directly after
// the row's existing name-to-pct separator, the pct field itself keeps its
// full right-justified width, and it is this gap that narrows to make room.
const laneTagGapWidth = 1

// laneTagNetCost is the tag column's own net width cost against a row's
// total budget once the gap it narrows is credited back - laneTagFieldWidth
// minus the one column laneTagGapWidth reclaims from the ordinary two-space
// gap.
const laneTagNetCost = laneTagFieldWidth - (2 - laneTagGapWidth)

// laneShowTagMinWidth reserves the tag column only when the row's own name
// column would sit at its full, untruncated laneNameColMax (20) anyway -
// "the name field keeps its full 20": below this rowInner budget the tag
// drops entirely rather than steal room from an already-shrinking name
// column. Verified against FRONTDOOR-MOCKUP-164x45.md's own 120-column
// screen, which carries no tag column at all.
var laneShowTagMinWidth = laneNameColMax + laneSuffixWidth(true, true) + laneTagNetCost

// laneShowTag reports whether a row built from a rowInner budget this wide
// carries the seat-tag column.
func laneShowTag(rowInner int) bool {
	return rowInner >= laneShowTagMinWidth
}

// lanePctField renders a lane's bare "NN%" right-justified within a
// fixed-width field, so the trailing "%" always lands in the same column
// regardless of how many digits the percentage has (item 1's "ctx
// percentage right-aligned", now without the "ctx" label - defect 2). When
// the fill genuinely cannot be derived the field is blank (not "n/a", the
// OWN ROW fix's rule, and no longer a bare "ctx" placeholder either, since
// that label no longer prints at all).
func lanePctField(pct int, ok bool) string {
	if !ok {
		return strings.Repeat(" ", lanePctFieldWidth)
	}
	return fmt.Sprintf("%*s", lanePctFieldWidth, fmt.Sprintf("%d%%", pct))
}

// laneRowSuffix renders the fixed-width tail every lane row (tracked or
// external) shares: the seat tag when showTag is set (slice 5 item 2, muted
// grey, blank field when tag is ""), right-aligned ctx, the state glyph
// (and word, unless collapsed), then the last-turn time (unless the row is
// too narrow to carry it - laneShowTime) - the FINISH defect's "one table"
// requirement. Every segment carries rowBg/rowFg forward explicitly (the
// same technique laneRowMarker and laneRowFrame below use for the rest of
// the row) so the glyph's own state colour does not reset the row's
// selected-highlight background for the characters after it.
func laneRowSuffix(rowBg, rowFg color.Color, tag string, showTag bool, pct int, fillOK bool, state string, lastTurn time.Time, turnOK bool, showWord, showTime bool) string {
	plain := lipgloss.NewStyle().Background(rowBg).Foreground(rowFg)
	glyph, glyphStyle := laneStateGlyph(state)
	glyphStyle = glyphStyle.Background(rowBg)

	// The tag column, when present, narrows the ordinary two-space
	// pct-to-glyph gap to one column (laneTagGapWidth) rather than the pct
	// field itself, which keeps its own full right-justified width either
	// way (laneShowTagMinWidth's own derivation).
	var tagSeg string
	gapWidth := 2
	if showTag {
		tagStyle := externalRowStyle.Background(rowBg)
		tagField := runewidth.FillRight(ansiTruncateRow(tag, laneTagFieldWidth), laneTagFieldWidth)
		tagSeg = tagStyle.Render(tagField)
		gapWidth = laneTagGapWidth
	}

	pctSeg := plain.Render(lanePctField(pct, fillOK))

	var wordSeg string
	if showWord {
		word := plain.Foreground(glyphStyle.GetForeground()).
			Render(runewidth.FillRight(laneStateDisplayWord(state), laneStateWordWidth))
		wordSeg = plain.Render(" ") + word
	}

	var timeSeg string
	if showTime {
		timeText := strings.Repeat(" ", laneTimeWidth)
		if turnOK {
			timeText = lastTurn.Local().Format("15:04")
		}
		timeSeg = plain.Render(" ") + plain.Render(timeText)
	}

	// Every literal separator space between segments is rendered through
	// plain too (DEFECT 2, board #280 pane-10 walkthrough): each segment
	// above already closes with its own ANSI reset, so a bare, un-rendered
	// " " placed between them (the pre-fix fmt.Sprintf(" %s  %s%s%s", ...))
	// prints with the terminal's own default background rather than rowBg -
	// a black gap on a selected (highlighted) row. Rendering the separators
	// through the same plain style keeps the whole suffix one continuous
	// rowBg band with no un-styled character in it.
	return plain.Render(" ") + tagSeg + pctSeg + plain.Render(strings.Repeat(" ", gapWidth)) + glyphStyle.Render(glyph) + wordSeg + timeSeg
}

// laneRowMarker returns the styled single-column cell every selectable row
// now begins with (rule 2: "a left marker ▌ in the accent ... Unselected
// rows have a single space in column 1"; rule 3 extends this to Needs-you
// rows too). It is rendered as its own fully self-contained ANSI span
// (opens, prints the one cell, resets) rather than folded into a larger
// wrapped string: laneRowFrame below concatenates it directly against
// content that opens its own styling immediately after, so the marker's
// own trailing reset is never followed by a bare, un-styled character -
// the same continuous-band property DEFECT 2 (board #280 pane-10
// walkthrough) required of laneRowSuffix's own separators.
func laneRowMarker(rowBg color.Color, selected bool) string {
	if selected {
		return laneRowMarkerStyle.Background(rowBg).Render("▌")
	}
	return lipgloss.NewStyle().Background(rowBg).Render(" ")
}

// laneRowFrame wraps a row's own already-styled, already-truncated content
// (every one of its segments carrying rowBg/rowFg forward explicitly, the
// laneRowSuffix convention) with the marker cell and the row's own single
// trailing padding column - the two columns Padding used to add on every
// row's style before slice 19 moved the marker into column 1 and dropped
// the second line entirely. Both frame pieces are self-contained ANSI
// spans of their own, so concatenating marker+content+pad leaves no
// un-styled byte anywhere in the line.
func laneRowFrame(rowBg, rowFg color.Color, selected bool, content string) string {
	plain := lipgloss.NewStyle().Background(rowBg).Foreground(rowFg)
	return laneRowMarker(rowBg, selected) + content + plain.Render(" ")
}

// laneNameFieldParts builds a tracked-instance row's own name-column
// content: the "N. title" text, an optional faint " · branch" suffix, and
// the trailing spaces that pad the whole thing out to nameCol - split into
// three return values so InstanceRenderer.Render can style each part on
// its own (bold name, faint branch, plain pad) rather than one flat
// string. Rule 1's own branch rule: "moves into the name column as a faint
// suffix (' · branch') only if it fits, else dropped" - the branch is
// shown WHOLE or not at all, it is never itself truncated to make room
// (unlike the name, which still truncates with an ellipsis exactly as
// before when the name alone overruns nameCol).
func laneNameFieldParts(prefix, title, branch string, hasWorktree bool, nameCol int) (base, branchSuffix, pad string) {
	full := prefix + title
	if hasWorktree && branch != "" {
		candidate := full + " · " + branch
		if runewidth.StringWidth(candidate) <= nameCol {
			branchSuffix = " · " + branch
		}
	}
	if branchSuffix == "" {
		full = runewidth.Truncate(full, nameCol, "…")
	}
	used := runewidth.StringWidth(full) + runewidth.StringWidth(branchSuffix)
	padN := nameCol - used
	if padN < 0 {
		padN = 0
	}
	return full, branchSuffix, strings.Repeat(" ", padN)
}

type List struct {
	items         []*session.Instance
	selectedIdx   int
	height, width int
	renderer      *InstanceRenderer
	autoyes       bool

	// map of repo name to number of instances using it. Used to display the repo name only if there are
	// multiple repos in play.
	repos map[string]int

	// needsYou holds the ranked "Needs you" feed rows, refreshed once per
	// feed tick by the caller (see app.go's feedTickMsg handling) - this
	// struct never reads the queue file itself, it only renders and
	// selects what it is handed. Selectable (slice 5): the cursor visits
	// these rows first, then items, then external (Down/Up below).
	needsYou []clarity.FeedItem
	// needsYouStatus carries the queue's own absent/parse-error/empty text
	// (clarity.RankedNeedsYou's second return) when there are no selectable
	// rows to show instead - never a silently empty section.
	needsYouStatus string

	// answeredIssues is the y-key answer flow's own in-memory marker set
	// (ANSWER-AND-BANK-SPEC.md "Answered marker and its lifetime") - board
	// issue numbers answered this session, refreshed by app.go every feed
	// tick (and immediately on a successful send); never persisted, never
	// read as truth by anything but the row/card tick-and-dim render below.
	answeredIssues map[int]bool

	// external holds the fleet's live lanes that are NOT tracked Claude
	// Squad instances (see clarity.DiscoverExternalLanes), refreshed on the
	// same tick as the rest of the metadata. Rendered below the tracked
	// instances; selectable and messageable, never attachable or killable.
	external []clarity.ExternalLane

	// selExternal is true when the current selection points into external
	// rather than items; selNeedsYou is true when it points into needsYou
	// instead. At most one of the two is ever true - three groups sharing
	// one cursor (needsYou, then items, then external - RowKind's own
	// order), wrapping at either end (Down/Up below).
	selExternal bool
	selNeedsYou bool

	// collapsed mirrors app.go's own collapsePreviewBelowWidth decision
	// (the terminal itself, not just this list's own column share, is
	// under 100 columns) - item 1's "below 100 columns drop the word, keep
	// the glyph" on every lane row. Set via SetCollapsed, never derived
	// from l.width itself: at every width this app is normally run at, the
	// list's OWN column share is under 100 regardless of whether the
	// terminal is collapsed or not, so l.width alone cannot tell the two
	// apart.
	collapsed bool

	// accountsRegistry mirrors clarity.LoadAccountsRegistry()'s own tag ->
	// config_dir map (slice 5 item 3) - loaded once per feed tick by app.go
	// and pushed here via SetAccountsRegistry, never read by String()
	// itself, the same push convention SetExternal/SetNeedsYou already
	// follow. Only the tag SET matters to the fleet line below; the config
	// dir values are not read here.
	accountsRegistry map[string]string
}

// SetCollapsed records whether the terminal itself is below
// app.go's collapsePreviewBelowWidth threshold.
func (l *List) SetCollapsed(collapsed bool) {
	l.collapsed = collapsed
}

// SetAccountsRegistry replaces the registry.json seat-tag set the fleet
// line under "Instances" reads on the next render (slice 5 item 3).
func (l *List) SetAccountsRegistry(registry map[string]string) {
	l.accountsRegistry = registry
}

// SetAnsweredIssues replaces the answered-marker set the needsYou row
// render below reads.
func (l *List) SetAnsweredIssues(set map[int]bool) {
	l.answeredIssues = set
}

// isAnsweredItem reports whether item's own board issue number (a lane-file
// row never resolves one and so is never answered) is in the current
// answered-marker set.
func (l *List) isAnsweredItem(item clarity.FeedItem) bool {
	if len(l.answeredIssues) == 0 {
		return false
	}
	n, ok := clarity.BoardIssueNumber(item.Source)
	return ok && l.answeredIssues[n]
}

// SetNeedsYou replaces the "Needs you" feed rows shown above the instance
// list (items) and their status line (status - shown instead of any rows
// when the queue is absent, unparseable or empty; "" when it read cleanly).
// Mirrors SetExternal's own clamp: if the current selection was pointing
// into needsYou and the new slice is shorter (or empty), the selection is
// clamped so it never points past the end.
func (l *List) SetNeedsYou(items []clarity.FeedItem, status string) {
	l.needsYou = items
	l.needsYouStatus = status
	if l.selNeedsYou {
		if len(l.needsYou) == 0 {
			l.selNeedsYou = false
			l.selectedIdx = 0
		} else if l.selectedIdx >= len(l.needsYou) {
			l.selectedIdx = len(l.needsYou) - 1
		}
	}
}

// RemoveNeedsYouIssue drops the Needs-you row sourced from board issue n
// from the CURRENTLY held feed slice (this is the row-removal rule: "removes the row
// from the list on the same tick, without waiting for the feed rebuild") -
// called the instant a close lands, rather than waiting for the next feed
// tick's SetNeedsYou to rebuild the section. Clamps the cursor exactly the
// way SetNeedsYou already does when the queue shrinks out from under the
// current selection. Returns false when no row carries n (already removed,
// or never a board row at all).
func (l *List) RemoveNeedsYouIssue(n int) bool {
	idx := -1
	for i, it := range l.needsYou {
		if issue, ok := clarity.BoardIssueNumber(it.Source); ok && issue == n {
			idx = i
			break
		}
	}
	if idx == -1 {
		return false
	}
	l.needsYou = append(l.needsYou[:idx], l.needsYou[idx+1:]...)
	if l.selNeedsYou {
		if len(l.needsYou) == 0 {
			l.selNeedsYou = false
			l.selectedIdx = 0
		} else if l.selectedIdx >= len(l.needsYou) {
			l.selectedIdx = len(l.needsYou) - 1
		}
	}
	return true
}

// SetExternal replaces the external-lane rows shown below the tracked
// instances. If the current selection was pointing into external and the
// new list is shorter (or empty), the selection is clamped so it never
// points past the end - matching how a killed/removed instance never
// leaves selectedIdx dangling.
func (l *List) SetExternal(lanes []clarity.ExternalLane) {
	l.external = lanes
	if l.selExternal {
		if len(l.external) == 0 {
			l.selExternal = false
			l.selectedIdx = 0
		} else if l.selectedIdx >= len(l.external) {
			l.selectedIdx = len(l.external) - 1
		}
	}
}

// GetExternal returns the current external-lane rows.
func (l *List) GetExternal() []clarity.ExternalLane {
	return l.external
}

func NewList(spinner *spinner.Model, autoYes bool) *List {
	return &List{
		items:    []*session.Instance{},
		renderer: &InstanceRenderer{spinner: spinner},
		repos:    make(map[string]int),
		autoyes:  autoYes,
	}
}

// SetSize sets the height and width of the list.
func (l *List) SetSize(width, height int) {
	l.width = width
	l.height = height
	l.renderer.setWidth(width)
}

// SetSessionPreviewSize sets the height and width for the tmux sessions. This makes the stdout line have the correct
// width and height.
func (l *List) SetSessionPreviewSize(width, height int) (err error) {
	for i, item := range l.items {
		if !item.Started() || item.Paused() {
			continue
		}

		if innerErr := item.SetPreviewSize(width, height); innerErr != nil {
			err = errors.Join(
				err, fmt.Errorf("could not set preview size for instance %d: %v", i, innerErr))
		}
	}
	return
}

func (l *List) NumInstances() int {
	return len(l.items)
}

// NumNeedsYou returns the number of Needs-you rows currently held - slice
// 24's own same-tick removal (RemoveNeedsYouIssue) is proven by this count
// dropping without waiting for a fresh SetNeedsYou.
func (l *List) NumNeedsYou() int {
	return len(l.needsYou)
}

// Width returns the column width SetSize last gave this list - app.go's own
// TestUpdateHandleWindowSizeEvent_ListGetsNewProportion reads this to prove
// the app-level split, not just listWidthForTerminal in isolation.
func (l *List) Width() int {
	return l.width
}

// rowInnerWidth is the width budget for one "Needs you" feed line or
// external-lane row's raw content, ansi-aware and truncated with an
// ellipsis when a line runs over it (ansiTruncateRow, below). It mirrors
// InstanceRenderer's own r.width (AdjustPreviewWidth(l.width)) minus the
// small left/right padding needsYouLineStyle and externalRowStyle apply, so
// the STYLED line never exceeds l.width - the OVERFLOW defect's root cause
// was exactly this budget going unenforced for these two row kinds:
// lipgloss.Place (List.String()'s own final wrapper) is a documented no-op
// once content already exceeds the width it was given, it never truncates.
func (l *List) rowInnerWidth() int {
	w := l.width - 2
	if w < 1 {
		w = 1
	}
	return w
}

// ansiTruncateRow truncates s to width cells, ansi- and grapheme-aware
// (github.com/charmbracelet/x/ansi.Truncate - never a byte slice, which
// would both miscount wide/multi-byte runes and could sever an escape
// sequence mid-code). Keeps s's own FRONT, ellipsis-cuts the tail.
func ansiTruncateRow(s string, width int) string {
	return ansi.Truncate(s, width, "…")
}

// ansiTruncateLeftRow is ansiTruncateRow's mirror: it keeps s's own TAIL,
// ellipsis-cutting the front instead - session.go's header line 2 uses this
// (DEFECT 2) so a long working-directory path is what gets sacrificed, never
// the branch/model/window that follow it in the same joined string.
// ansi.TruncateLeft(s, n, prefix) removes n CELLS from the front and adds
// prefix, so hitting an exact target width needs n = currentWidth - width +
// len(prefix) - proven against ansi.TruncateLeft's own behaviour before this
// was written (see the leg's report).
func ansiTruncateLeftRow(s string, width int) string {
	w := ansi.StringWidth(s)
	if w <= width {
		return s
	}
	const prefix = "…"
	n := w - width + runewidth.StringWidth(prefix)
	if n < 0 {
		n = 0
	}
	return ansi.TruncateLeft(s, n, prefix)
}

// laneNameColMax is the widest the name column is ever allowed to be -
// defect 2's own "name column padded to 20 (truncate with … beyond)", down
// from the old 28 (which the old, wider 40%-of-terminal list column could
// spare; the new, narrower listWidthForTerminal share cannot). Shared by
// both row kinds (item 4's "one table").
const laneNameColMax = 20

// laneRowInnerWidth converts a component's own width into the row-content
// budget List.rowInnerWidth() computes for the "Needs you"/external
// section (that -2 is the small left/right padding those styles apply).
// InstanceRenderer's r.width is the SAME list width (see setWidth below,
// defect 2: this used to run every list row through AdjustPreviewWidth's
// own 0.9 factor first - a function named, and still used, for the
// TABBED WINDOW's preview-pane sizing, reused here purely by historical
// accident per the git history, and one the new compact row format cannot
// afford to keep discarding 10% of its own, already-narrower column to),
// so this is the one place both the tracked-instance title line and the
// external rows derive their shared column grid from, instead of two
// independently-drifting calculations.
func laneRowInnerWidth(componentWidth int) int {
	w := componentWidth - 2
	if w < 1 {
		w = 1
	}
	return w
}

// laneNameColWidthFor returns the fixed column width a lane row's name is
// padded/truncated to within a rowInner budget, so every row's
// ctx/glyph/word/time line up under each other regardless of individual
// lane-name length or whether the row is a tracked instance's "N. name" or
// a bare external lane name (the FINISH defect's "pad the lane name to a
// column" requirement, generalized to both row kinds). It shrinks below
// laneNameColMax on a narrow list column so the suffix - the state
// information that matters most - is never the part a too-wide fixed name
// column pushes past the row's own truncation and off the edge. showTime is
// derived from rowInner itself (laneShowTime), the same budget the caller
// must use when it goes on to render laneRowSuffix, so the two can never
// disagree about whether the time field is in play.
func laneNameColWidthFor(rowInner int, showWord bool) int {
	avail := rowInner - laneSuffixWidth(showWord, laneShowTime(rowInner))
	const minCol = 6
	if avail < minCol {
		avail = minCol
	}
	if avail > laneNameColMax {
		avail = laneNameColMax
	}
	return avail
}

// laneNameColWidth is laneNameColWidthFor applied to this List's own
// rowInnerWidth() - the external-lane section's own budget.
func (l *List) laneNameColWidth(showWord bool) int {
	return laneNameColWidthFor(l.rowInnerWidth(), showWord)
}

// InstanceRenderer handles rendering of session.Instance objects
type InstanceRenderer struct {
	spinner *spinner.Model
	width   int
}

// setWidth records the list's own column width directly (defect 2: this
// used to run it through AdjustPreviewWidth's 0.9 factor first - see
// laneRowInnerWidth's own comment on why that coupling is gone).
func (r *InstanceRenderer) setWidth(width int) {
	r.width = width
}

func (r *InstanceRenderer) Render(i *session.Instance, idx int, selected bool, hasMultipleRepos bool, showWord bool, showTag bool) string {
	prefix := fmt.Sprintf("%d. ", idx)
	if idx >= 10 {
		prefix = prefix[:len(prefix)-1]
	}
	rowS := titleStyle
	if selected {
		rowS = selectedTitleStyle
	}
	rowBg, rowFg := rowS.GetBackground(), rowS.GetForeground()
	plain := lipgloss.NewStyle().Background(rowBg).Foreground(rowFg)

	// The row carries the same state table every lane row shares (item 1):
	// name padded to a column, ctx right-aligned, the clarity-derived state
	// glyph and word, then last-turn time - and, since slice 19, that is
	// the WHOLE row: rule 1's "ONE ROW per lane, no spacer rows" folds the
	// old second line (branch + diff stats) into this one, so a fleet of
	// four instances takes four rows, not twenty (the owner's own
	// screenshot). Diff stats have no home left to go: this compact row
	// has no room for them and the mock-up's own target row
	// (PANE-MOCKUP-164x45.md) never carried one - dropped, not moved.
	rowInner := laneRowInnerWidth(r.width)
	nameCol := laneNameColWidthFor(rowInner, showWord)

	// A NoWorktree instance (slice 8) has no git worktree and so no repo
	// name to look up at all - RepoName() would return its own "no git
	// worktree" error every render tick for an entirely expected condition,
	// not a real failure, so this is skipped rather than logged.
	branch := i.Branch
	if i.Started() && i.HasWorktree() && hasMultipleRepos {
		repoName, err := i.RepoName()
		if err != nil {
			log.ErrorLog.Printf("could not get repo name in instance renderer: %v", err)
		} else {
			branch += fmt.Sprintf(" (%s)", repoName)
		}
	}
	base, branchSuffix, pad := laneNameFieldParts(prefix, i.Title, branch, i.HasWorktree(), nameCol)

	// The name itself is bold on the selected row (rule 2: "the name in
	// bold in the normal foreground"); the branch suffix stays faint
	// regardless of selection, the same dim tone the old, now-removed
	// second line used for it.
	nameStyle := plain
	if selected {
		nameStyle = nameStyle.Bold(true)
	}
	branchStyle := plain.Foreground(laneBranchSuffixFg)

	pct, fillOK := i.GetContextFill()
	state, lastTurn, turnOK := i.GetLaneState()
	if i.NeedsKey() {
		// Overrides the transcript-derived word, never the cached value
		// itself (SetLaneState stays the pure transcript read - the r-key
		// resume gate and anything else reading GetLaneState still sees the
		// real state underneath).
		state = clarity.StateNeedsKey
	}
	suffix := laneRowSuffix(rowBg, rowFg, i.Account(), showTag, pct, fillOK, state, lastTurn, turnOK, showWord, laneShowTime(rowInner))

	content := nameStyle.Render(base) + branchStyle.Render(branchSuffix) + plain.Render(pad) + suffix
	return laneRowFrame(rowBg, rowFg, selected, ansiTruncateRow(content, rowInner))
}

// externalRowTag is the seat-tag TEXT an external lane's row shows (slice 5
// item 2) - the bare seat, never SeatTag's own "<tag> <source>" bracket
// text (that belongs to lane-tail/discover, not this row): a "default
// folder" lane (resolveSeat's own unlogged-in-default floor) shows no tag
// at all; every other source shows the seat alone.
func externalRowTag(lane clarity.ExternalLane) string {
	if lane.Account == "default" && lane.SeatSource == clarity.SeatSourceFolder {
		return ""
	}
	return lane.Account
}

// modalityHeading title-cases just the first rune of a declared modality
// value for its bare group-heading string ("app pipeline" -> "App
// pipeline", "ways of working" -> "Ways of working") - the value itself,
// never a fixed vocabulary table, since a heading exists for whatever
// modality a lane actually declares (FRONTDOOR-MOCKUP-164x45.md screen 4's
// own "Ways of working" heading is not one of the four forge/project/bid/
// enhancement buckets the modality picker also offers).
func modalityHeading(modality string) string {
	if modality == "" {
		return ""
	}
	r := []rune(modality)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// laneGroup is one modality bucket's own row set for String()'s grouped
// render (slice 5 item 1): the tracked-instance and external-lane indices
// (into l.items/l.external, in their own existing order) that carry this
// modality. modality is "" for the trailing catch-all bucket, which keeps
// today's plain, un-headed presentation for both row kinds (a tracked row
// with no heading at all, an external row under the old "External lanes
// (message only)" title) rather than a bare heading of its own.
type laneGroup struct {
	modality    string
	itemIdx     []int
	externalIdx []int
}

// groupLanesByModality buckets tracked instances and external lanes by
// their own declared Modality()/Modality, in FIRST-SEEN order scanning the
// tracked instances first, then the external lanes. This is what makes
// FRONTDOOR-MOCKUP-164x45.md screen 4's own heading order (Ways of working,
// App pipeline, Enhancement, Project, Bid) fall out of the fleet's own item
// order, rather than a fixed canonical list this fork would have to keep in
// step with the modality picker's own five rows by hand - the two do not
// actually agree on order (see this leg's report), and the drawing is the
// bar. Lanes with no modality declared land in one trailing, un-headed
// catch-all group, always the last element of the returned slice.
func groupLanesByModality(items []*session.Instance, external []clarity.ExternalLane) []laneGroup {
	order := make([]string, 0, 4)
	index := make(map[string]int, 4)
	for _, it := range items {
		m := it.Modality()
		if m == "" {
			continue
		}
		if _, ok := index[m]; !ok {
			index[m] = len(order)
			order = append(order, m)
		}
	}
	for _, ext := range external {
		m := ext.Modality
		if m == "" {
			continue
		}
		if _, ok := index[m]; !ok {
			index[m] = len(order)
			order = append(order, m)
		}
	}

	groups := make([]laneGroup, len(order)+1)
	for i, m := range order {
		groups[i].modality = m
	}
	catchAll := len(order)

	for i, it := range items {
		g := catchAll
		if m := it.Modality(); m != "" {
			g = index[m]
		}
		groups[g].itemIdx = append(groups[g].itemIdx, i)
	}
	for i, ext := range external {
		g := catchAll
		if m := ext.Modality; m != "" {
			g = index[m]
		}
		groups[g].externalIdx = append(groups[g].externalIdx, i)
	}
	return groups
}

// renderExternalRow renders one external-lane row (message-only: no diff
// stats, no branch, no attach/kill affordance) - shared by every modality
// group's own external members and by the trailing catch-all's "External
// lanes (message only)" block, so the two paths can never drift out of step
// on column layout.
func (l *List) renderExternalRow(lane clarity.ExternalLane, selected, showWord, showTag bool, innerWidth int) string {
	style := externalRowStyle
	if selected {
		style = externalRowSelectedStyle
	}
	rowBg, rowFg := style.GetBackground(), style.GetForeground()
	plain := lipgloss.NewStyle().Background(rowBg).Foreground(rowFg)
	nameStyle := plain
	if selected {
		nameStyle = nameStyle.Bold(true)
	}
	// Pad (or truncate) the name to a fixed column first, so every row's
	// suffix lines up regardless of how long an individual lane name is -
	// then truncate the WHOLE row to the list's inner width, since a name
	// near the column width plus the fixed suffix can still run past a
	// narrow terminal.
	nameCol := l.laneNameColWidth(showWord)
	name := runewidth.FillRight(ansiTruncateRow(lane.Name, nameCol), nameCol)
	suffix := laneRowSuffix(rowBg, rowFg, externalRowTag(lane), showTag,
		lane.Fill.Pct, lane.FillOK, lane.State, lane.LastTurn, lane.StateOK, showWord, laneShowTime(innerWidth))
	line := nameStyle.Render(name) + suffix
	content := ansiTruncateRow(line, innerWidth)
	return laneRowFrame(rowBg, rowFg, selected, content)
}

// fleetLine renders the per-seat fill line under "Instances" (slice 5 item
// 3): "<tag> <pct>%" for every registry seat with at least one live lane
// (a tracked instance or external lane whose own Account matches it),
// joined by " · ", sorted by tag for a stable render. The figure is the
// MAXIMUM context fill across that seat's own lanes - research finding F7,
// "the harness sums nothing" - never their sum; a seat with no live lane at
// all is omitted rather than shown at 0%. "" when no registry has been set
// (SetAccountsRegistry never called, or the registry is empty) - the line
// itself is then dropped, never rendered blank.
func (l *List) fleetLine() string {
	if len(l.accountsRegistry) == 0 {
		return ""
	}
	tags := make([]string, 0, len(l.accountsRegistry))
	for tag := range l.accountsRegistry {
		tags = append(tags, tag)
	}
	sort.Strings(tags)

	parts := make([]string, 0, len(tags))
	for _, tag := range tags {
		maxPct := 0
		live := false
		for _, inst := range l.items {
			if inst.Account() != tag {
				continue
			}
			live = true
			if pct, ok := inst.GetContextFill(); ok && pct > maxPct {
				maxPct = pct
			}
		}
		for _, ext := range l.external {
			if ext.Account != tag {
				continue
			}
			live = true
			if ext.FillOK && ext.Fill.Pct > maxPct {
				maxPct = ext.Fill.Pct
			}
		}
		if live {
			parts = append(parts, fmt.Sprintf("%s %d%%", tag, maxPct))
		}
	}
	return strings.Join(parts, " · ")
}

func (l *List) String() string {
	const titleText = " Instances "
	const autoYesText = " auto-yes "

	// Write the title.
	var header strings.Builder
	header.WriteString("\n")
	header.WriteString("\n")

	// Write title line
	// add padding of 2 because the border on list items adds some extra characters
	titleWidth := AdjustPreviewWidth(l.width) + 2
	if !l.autoyes {
		header.WriteString(lipgloss.Place(
			titleWidth, 1, lipgloss.Left, lipgloss.Bottom, mainTitle.Render(titleText)))
	} else {
		title := lipgloss.Place(
			titleWidth/2, 1, lipgloss.Left, lipgloss.Bottom, mainTitle.Render(titleText))
		autoYes := lipgloss.Place(
			titleWidth-(titleWidth/2), 1, lipgloss.Right, lipgloss.Bottom, autoYesStyle.Render(autoYesText))
		header.WriteString(lipgloss.JoinHorizontal(
			lipgloss.Top, title, autoYes))
	}

	header.WriteString("\n")
	// The per-seat fleet line (slice 5 item 3) sits directly under the
	// title, no blank line either side, per FRONTDOOR-MOCKUP-164x45.md
	// screen 4's rows 4-6 (Instances, fleet line, Needs you all adjacent).
	// When there is nothing to show (no registry loaded, or no seat
	// qualifies) this writes nothing, leaving the ORIGINAL "\n\n" - one
	// blank line under the title - exactly as before slice 5.
	if fleet := l.fleetLine(); fleet != "" {
		rowBg, rowFg := fleetLineStyle.GetBackground(), fleetLineStyle.GetForeground()
		plain := lipgloss.NewStyle().Background(rowBg).Foreground(rowFg)
		content := plain.Render(ansiTruncateRow(fleet, l.rowInnerWidth()))
		header.WriteString(laneRowFrame(rowBg, rowFg, false, content))
	}
	header.WriteString("\n")

	// Render the list (Instances, then External lanes) FIRST, into its own
	// block - the Instances and External sections keep
	// their place below": these two never scroll or shrink to make room for
	// the Needs-you feed, only the Needs-you section itself does (below).
	// showWord is item 1's "below 100 columns drop the word, keep the
	// glyph" - one decision per render pass, shared by every tracked and
	// external row alike (item 4's "one table"). showTag is slice 5 item
	// 2's own collapse point, dropping the whole seat-tag column below 120
	// columns (laneShowTagMinWidth). One row per lane, no blank line
	// between them (rule 1: "no spacer rows" - the owner's own screenshot
	// showed four instances taking twenty rows).
	innerWidth := l.rowInnerWidth()
	showWord := !l.collapsed
	showTag := laneShowTag(innerWidth)
	var rest strings.Builder

	// Group both tracked instances and external lanes by modality (slice 5
	// item 1) - groupLanesByModality's own doc comment explains the order.
	// The trailing element is always the no-modality catch-all.
	groups := groupLanesByModality(l.items, l.external)
	namedGroups := groups[:len(groups)-1]
	catchAll := groups[len(groups)-1]

	for _, g := range namedGroups {
		if len(g.itemIdx) == 0 && len(g.externalIdx) == 0 {
			continue
		}
		rest.WriteString(modalityHeadingStyle.Render(" " + modalityHeading(g.modality) + " "))
		rest.WriteString("\n")
		for _, idx := range g.itemIdx {
			item := l.items[idx]
			selected := !l.selExternal && !l.selNeedsYou && idx == l.selectedIdx
			rest.WriteString(l.renderer.Render(item, idx+1, selected, len(l.repos) > 1, showWord, showTag))
			rest.WriteString("\n")
		}
		for _, idx := range g.externalIdx {
			selected := l.selExternal && idx == l.selectedIdx
			rest.WriteString(l.renderExternalRow(l.external[idx], selected, showWord, showTag, innerWidth))
			rest.WriteString("\n")
		}
	}

	// The catch-all's own tracked rows keep today's plain presentation: no
	// heading at all, directly under "Instances".
	for _, idx := range catchAll.itemIdx {
		item := l.items[idx]
		selected := !l.selExternal && !l.selNeedsYou && idx == l.selectedIdx
		rest.WriteString(l.renderer.Render(item, idx+1, selected, len(l.repos) > 1, showWord, showTag))
		rest.WriteString("\n")
	}

	// The catch-all's own external lanes keep today's "External lanes
	// (message only)" heading - live on this Mac but not tracked here (see
	// clarity.DiscoverExternalLanes). Message-only: no diff stats, no
	// branch, no attach/kill affordance, because none of that exists for a
	// lane with no tracked tmux session or git worktree.
	if len(catchAll.externalIdx) > 0 {
		// externalTitleStyle carries its own top Padding(1) - that IS the
		// rule 1 blank line between the tracked-instance block and this
		// heading (a second, explicit "\n" here on top of it was the old
		// "\n\n" spacer bug's own other half: two blank-line sources
		// stacking into the owner's screenshot).
		rest.WriteString(externalTitleStyle.Render(" External lanes (message only) "))
		rest.WriteString("\n")
		for _, idx := range catchAll.externalIdx {
			selected := l.selExternal && idx == l.selectedIdx
			rest.WriteString(l.renderExternalRow(l.external[idx], selected, showWord, showTag, innerWidth))
			rest.WriteString("\n")
		}
	}

	// The Needs-you feed's own budget is whatever l.height leaves once the
	// header and the (never-shrinking) rest block above are accounted for -
	// see renderNeedsYouBlock's own doc comment for how that budget is
	// spent (every row when it fits, a scrolled window with "… N more"
	// markers when it does not).
	budget := l.height - strings.Count(header.String(), "\n") - strings.Count(rest.String(), "\n")
	needsYouBlock := l.renderNeedsYouBlock(budget, innerWidth)

	content := header.String() + needsYouBlock + rest.String()

	// Cap the block to l.height before handing it to Place: with the "Needs
	// you" feed, every tracked instance and a full external-lane section
	// all present at once, the natural content height can still exceed
	// l.height by a line or two of rounding - and lipgloss.Place's own
	// documented behaviour is a no-op ("If the given height is shorter
	// than the content height... this will be a noöp") rather than a
	// crop, so an unenforced budget here let the list's block grow past
	// its box and push the menu/footer below it off the bottom of the
	// terminal entirely (the OVERFLOW defect's vertical half, on real
	// fleet data: needsYou + 2 tracked instances + 6 external rows at
	// 80x24). External-lane rows are the lowest-priority content (added
	// last, message-only), so they are what a tight terminal loses first;
	// cutting on whole lines (never mid-row) keeps every ANSI style
	// sequence intact.
	if l.height > 0 {
		lines := strings.Split(content, "\n")
		if len(lines) > l.height {
			content = strings.Join(lines[:l.height], "\n")
		}
	}

	return lipgloss.Place(l.width, l.height, lipgloss.Left, lipgloss.Top, content)
}

// renderNeedsYouBlock draws the "Needs you" section within budget lines
// (title + rows/status + one trailing blank, matching the fixed section
// this replaces) - never a bare empty section when the queue is absent
// (l.needsYouStatus), per the brief. Every row is truncated to width first
// (the OVERFLOW defect: a feed row can run to 100+ characters). When every
// row fits within budget they all render, in feed order (every feed row
// renders now, never a fixed five-row cap); when they do not, the section
// SCROLLS instead of truncating - the window always includes the current
// selection, and a
// one-line "… N more" marker (styled like the section title) appears at
// whichever edge (or edges) still hides rows.
func (l *List) renderNeedsYouBlock(budget, width int) string {
	if len(l.needsYou) == 0 && l.needsYouStatus == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(needsYouTitleStyle.Render(" Needs you "))
	b.WriteString("\n")

	rowsBudget := budget - 2 // this block's own title line + trailing blank
	if l.needsYouStatus != "" {
		rowsBudget--
	}

	if l.needsYouStatus != "" {
		rowBg, rowFg := needsYouLineStyle.GetBackground(), needsYouLineStyle.GetForeground()
		plain := lipgloss.NewStyle().Background(rowBg).Foreground(rowFg)
		content := plain.Render(ansiTruncateRow(l.needsYouStatus, width))
		b.WriteString(laneRowFrame(rowBg, rowFg, false, content))
		b.WriteString("\n")
	}

	sel := 0
	if l.selNeedsYou {
		sel = l.selectedIdx
	}
	start, count, topMore, bottomMore := needsYouScrollWindow(len(l.needsYou), rowsBudget, sel)

	if topMore {
		b.WriteString(needsYouMoreMarker(start, width))
		b.WriteString("\n")
	}
	for i := start; i < start+count; i++ {
		item := l.needsYou[i]
		selected := l.selNeedsYou && i == l.selectedIdx
		style := needsYouLineStyle
		if selected {
			style = needsYouLineSelectedStyle
		}
		answered := l.isAnsweredItem(item)
		if answered {
			// Ticked and dimmed (ANSWER-AND-BANK-SPEC.md) - the row's own
			// selected/unselected background is kept, only the foreground
			// dims to the shared muted tone.
			style = style.Foreground(sessionMutedStyle.GetForeground())
		}
		rowBg, rowFg := style.GetBackground(), style.GetForeground()
		plain := lipgloss.NewStyle().Background(rowBg).Foreground(rowFg)
		text := clarity.FeedLine(item)
		if answered {
			text = "✓ " + text
		}
		content := plain.Render(ansiTruncateRow(text, width))
		b.WriteString(laneRowFrame(rowBg, rowFg, selected, content))
		b.WriteString("\n")
	}
	if bottomMore {
		b.WriteString(needsYouMoreMarker(len(l.needsYou)-(start+count), width))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	return b.String()
}

// needsYouMoreMarker is the "… N more" edge marker,
// styled like the section's own title so it reads as part of the same
// section rather than another row.
func needsYouMoreMarker(hidden, width int) string {
	text := fmt.Sprintf("… %d more", hidden)
	return needsYouTitleStyle.Render(ansiTruncateRow(text, width))
}

// needsYouScrollWindow picks which of n rows are visible within budget
// lines, given the currently selected index sel: every row when they all
// fit (start 0, count n, no markers); otherwise a window that always
// includes sel, shrunk by one line per edge that still hides rows so the
// markers themselves stay inside budget. n>budget (the only case a window
// is needed at all) means the window can touch AT MOST one edge on its
// own - touching neither means both markers apply, so capacity is re-cut
// to budget-2 and the window recomputed once more.
func needsYouScrollWindow(n, budget, sel int) (start, count int, topMore, bottomMore bool) {
	if budget <= 0 {
		return 0, 0, false, n > 0
	}
	if n <= budget {
		return 0, n, false, false
	}
	if sel < 0 {
		sel = 0
	} else if sel >= n {
		sel = n - 1
	}
	start, end := needsYouClampWindow(n, budget-1, sel)
	topMore = start > 0
	bottomMore = end < n
	if topMore && bottomMore {
		start, end = needsYouClampWindow(n, budget-2, sel)
		topMore = start > 0
		bottomMore = end < n
	}
	return start, end - start, topMore, bottomMore
}

// needsYouClampWindow returns a [start, end) window of capacity rows (never
// less than 1) that contains sel and never runs past [0, n).
func needsYouClampWindow(n, capacity, sel int) (start, end int) {
	if capacity < 1 {
		capacity = 1
	}
	start = sel - capacity + 1
	if start < 0 {
		start = 0
	}
	if maxStart := n - capacity; maxStart >= 0 && start > maxStart {
		start = maxStart
	}
	end = start + capacity
	if end > n {
		end = n
	}
	return start, end
}

// RowKind identifies which of the three selectable groups the cursor
// currently sits in - the brief's own cursor order: Needs-you rows, then
// tracked instances, then external lanes.
type RowKind int

const (
	RowKindNeedsYou RowKind = iota
	RowKindTracked
	RowKindExternal
)

// groupLens returns the three groups' lengths in cursor order
// (RowKindNeedsYou, RowKindTracked, RowKindExternal) - the single source
// both Down/Up and currentGroup/setGroup below cycle over, so a group that
// is empty is transparently skipped without three copies of the same
// "is this group present" logic.
func (l *List) groupLens() [3]int {
	return [3]int{len(l.needsYou), len(l.items), len(l.external)}
}

// currentGroup reports which group index (0/1/2, matching groupLens' own
// order) the cursor is currently in.
func (l *List) currentGroup() int {
	if l.selNeedsYou {
		return 0
	}
	if l.selExternal {
		return 2
	}
	return 1
}

// setGroup moves the cursor to group g at idx, updating the two booleans
// that jointly encode it.
func (l *List) setGroup(g, idx int) {
	l.selNeedsYou = g == 0
	l.selExternal = g == 2
	l.selectedIdx = idx
}

// SelectedRowKind reports which group the cursor is currently in.
func (l *List) SelectedRowKind() RowKind {
	switch l.currentGroup() {
	case 0:
		return RowKindNeedsYou
	case 2:
		return RowKindExternal
	default:
		return RowKindTracked
	}
}

// Down selects the next row, cycling needsYou -> items -> external and
// wrapping back to whichever group is first non-empty - an empty group is
// skipped entirely, so this reduces to the pre-slice-5 two-group behaviour
// (items <-> external) whenever needsYou is empty.
func (l *List) Down() {
	lens := l.groupLens()
	if lens[0]+lens[1]+lens[2] == 0 {
		return
	}
	g := l.currentGroup()
	if l.selectedIdx < lens[g]-1 {
		l.selectedIdx++
		return
	}
	for step := 1; step <= 3; step++ {
		next := (g + step) % 3
		if lens[next] > 0 {
			l.setGroup(next, 0)
			return
		}
	}
}

// Kill selects the next item in the list. A no-op when the selection is on
// an external or Needs-you row - there is no tracked instance behind either
// to kill.
func (l *List) Kill() {
	if l.selExternal || l.selNeedsYou || len(l.items) == 0 {
		return
	}
	targetInstance := l.items[l.selectedIdx]

	// Kill the tmux session
	if err := targetInstance.Kill(); err != nil {
		log.ErrorLog.Printf("could not kill instance: %v", err)
	}

	// If you delete the last one in the list, select the previous one.
	if l.selectedIdx == len(l.items)-1 {
		defer l.Up()
	}

	// Unregister the reponame. A NoWorktree instance (slice 8) was never
	// registered under a repo name in the first place (see AddInstance's
	// own HasWorktree guard below) - skip straight past, no worktree error.
	if targetInstance.HasWorktree() {
		repoName, err := targetInstance.RepoName()
		if err != nil {
			log.ErrorLog.Printf("could not get repo name: %v", err)
		} else {
			l.rmRepo(repoName)
		}
	}

	// Since there's items after this, the selectedIdx can stay the same.
	l.items = append(l.items[:l.selectedIdx], l.items[l.selectedIdx+1:]...)
}

// Attach attaches to the selected tracked instance. Returns an error
// without attaching anything when the selection is on an external or
// Needs-you row - there is no tracked tmux session behind either to attach
// to.
func (l *List) Attach() (chan struct{}, error) {
	if l.selExternal || l.selNeedsYou || len(l.items) == 0 || l.selectedIdx >= len(l.items) {
		return nil, errors.New("cannot attach: no tracked instance is selected")
	}
	targetInstance := l.items[l.selectedIdx]
	return targetInstance.Attach()
}

// Up selects the previous row, cycling the same three groups Down does in
// reverse (external -> items -> needsYou, wrapping), an empty group again
// skipped entirely.
func (l *List) Up() {
	lens := l.groupLens()
	if lens[0]+lens[1]+lens[2] == 0 {
		return
	}
	g := l.currentGroup()
	if l.selectedIdx > 0 {
		l.selectedIdx--
		return
	}
	for step := 1; step <= 3; step++ {
		prev := ((g-step)%3 + 3) % 3
		if lens[prev] > 0 {
			l.setGroup(prev, lens[prev]-1)
			return
		}
	}
}

func (l *List) addRepo(repo string) {
	if _, ok := l.repos[repo]; !ok {
		l.repos[repo] = 0
	}
	l.repos[repo]++
}

func (l *List) rmRepo(repo string) {
	if _, ok := l.repos[repo]; !ok {
		log.ErrorLog.Printf("repo %s not found", repo)
		return
	}
	l.repos[repo]--
	if l.repos[repo] == 0 {
		delete(l.repos, repo)
	}
}

// AddInstance adds a new instance to the list. It returns a finalizer function that should be called when the instance
// is started. If the instance was restored from storage or is paused, you can call the finalizer immediately.
// When creating a new one and entering the name, you want to call the finalizer once the name is done.
func (l *List) AddInstance(instance *session.Instance) (finalize func()) {
	l.items = append(l.items, instance)
	// The finalizer registers the repo name once the instance is started. A
	// NoWorktree instance (slice 8) has no repo to register at all - this is
	// the expected shape for it, not a failure, so it is a plain no-op
	// rather than a logged "no git worktree" error.
	return func() {
		if !instance.HasWorktree() {
			return
		}
		repoName, err := instance.RepoName()
		if err != nil {
			log.ErrorLog.Printf("could not get repo name: %v", err)
			return
		}

		l.addRepo(repoName)
	}
}

// GetSelectedInstance returns the currently selected tracked instance, or
// nil when the selection is on an external or Needs-you row (or the list is
// empty) - neither can be attached, killed, or otherwise treated as a
// tracked instance, so every caller that already nil-checks this (kill,
// attach, checkout, push, resume, move) gets that guard for free.
func (l *List) GetSelectedInstance() *session.Instance {
	if l.selExternal || l.selNeedsYou || len(l.items) == 0 || l.selectedIdx >= len(l.items) {
		return nil
	}
	return l.items[l.selectedIdx]
}

// GetSelectedExternalLane returns the currently selected external lane, or
// ok=false when the selection is on a tracked or Needs-you row (or nothing
// is selected) - the external-row counterpart to GetSelectedInstance, used
// by the Session tab (design/cockpit-pane/DECISIONS.md slice 3) to build
// its SessionInfo for whichever kind of row is selected.
func (l *List) GetSelectedExternalLane() (clarity.ExternalLane, bool) {
	if !l.selExternal || l.selectedIdx < 0 || l.selectedIdx >= len(l.external) {
		return clarity.ExternalLane{}, false
	}
	return l.external[l.selectedIdx], true
}

// GetSelectedNeedsYou returns the currently selected Needs-you row, or
// ok=false when the selection is on a tracked or external row (or nothing
// is selected) - the Needs-you counterpart to GetSelectedExternalLane, used
// by the Needs-you tab (slice 5) to build its own render and by the
// composer to resolve "the row's raising lane" as its send target.
func (l *List) GetSelectedNeedsYou() (clarity.FeedItem, bool) {
	if !l.selNeedsYou || l.selectedIdx < 0 || l.selectedIdx >= len(l.needsYou) {
		return clarity.FeedItem{}, false
	}
	return l.needsYou[l.selectedIdx], true
}

// SelectedMsgTarget returns the lane name of the current selection,
// whichever list it is in, plus whether it is an external row - both
// tracked instances and external rows are messageable (the brief's
// requirement), only tracked instances are attachable/killable. ok is
// false when nothing is selected (both lists empty, the index is out of
// range for its list, or the selection is on a Needs-you row - that row's
// own raising lane is resolved separately, via GetSelectedNeedsYou, since
// it may name a lane this list does not track at all).
//
// A tracked row whose own tmux session has gone away (RequiresCopyOnlySend
// - most commonly a Paused NoWorktree clarity-attach lane, which runs in
// the owner's own terminal) resolves isExternal=true here too, exactly like
// a genuine external row (cockpit pane-10 walkthrough DEFECT 1: the tracked
// send path used to be picked for this row regardless, and errored "not a
// live tmux session" on enter). This is checked against the instance's own
// live session state, never assumed from its Status field alone.
func (l *List) SelectedMsgTarget() (lane string, isExternal bool, ok bool) {
	if l.selNeedsYou {
		return "", false, false
	}
	if l.selExternal {
		if l.selectedIdx < 0 || l.selectedIdx >= len(l.external) {
			return "", false, false
		}
		return l.external[l.selectedIdx].Name, true, true
	}
	if l.selectedIdx < 0 || l.selectedIdx >= len(l.items) {
		return "", false, false
	}
	inst := l.items[l.selectedIdx]
	return inst.Title, inst.RequiresCopyOnlySend(), true
}

// SetSelectedInstance sets the selected index into the tracked-instance
// group, clearing any external/Needs-you selection - a caller of this
// always means "select a tracked instance". Noop if the index is out of
// bounds.
func (l *List) SetSelectedInstance(idx int) {
	if idx >= len(l.items) {
		return
	}
	l.selExternal = false
	l.selNeedsYou = false
	l.selectedIdx = idx
}

// SelectInstance finds and selects the given instance in the list.
func (l *List) SelectInstance(target *session.Instance) {
	for i, inst := range l.items {
		if inst == target {
			l.SetSelectedInstance(i)
			return
		}
	}
}

// MoveUp swaps the selected instance with the one above it.
func (l *List) MoveUp() bool {
	if l.selectedIdx <= 0 || len(l.items) < 2 {
		return false
	}
	l.items[l.selectedIdx], l.items[l.selectedIdx-1] = l.items[l.selectedIdx-1], l.items[l.selectedIdx]
	l.selectedIdx--
	return true
}

// MoveDown swaps the selected instance with the one below it.
func (l *List) MoveDown() bool {
	if l.selectedIdx >= len(l.items)-1 || len(l.items) < 2 {
		return false
	}
	l.items[l.selectedIdx], l.items[l.selectedIdx+1] = l.items[l.selectedIdx+1], l.items[l.selectedIdx]
	l.selectedIdx++
	return true
}

// GetInstances returns all instances in the list
func (l *List) GetInstances() []*session.Instance {
	return l.items
}
