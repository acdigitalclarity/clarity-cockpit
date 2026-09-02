package ui

import (
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/session/clarity"
	"errors"
	"fmt"
	"image/color"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

var addedLinesStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#51bd73"), Dark: lipgloss.Color("#51bd73")})

var removedLinesStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#de613e"))

var titleStyle = lipgloss.NewStyle().
	Padding(1, 1, 0, 1).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#1a1a1a"), Dark: lipgloss.Color("#dddddd")})

var listDescStyle = lipgloss.NewStyle().
	Padding(0, 1, 1, 1).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#A49FA5"), Dark: lipgloss.Color("#777777")})

var selectedTitleStyle = lipgloss.NewStyle().
	Padding(1, 1, 0, 1).
	Background(lipgloss.Color("#dde4f0")).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#1a1a1a"), Dark: lipgloss.Color("#1a1a1a")})

var selectedDescStyle = lipgloss.NewStyle().
	Padding(0, 1, 1, 1).
	Background(lipgloss.Color("#dde4f0")).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#1a1a1a"), Dark: lipgloss.Color("#1a1a1a")})

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
	Padding(0, 0, 0, 1).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#5a5a5a"), Dark: lipgloss.Color("#aaaaaa")})

var externalTitleStyle = lipgloss.NewStyle().
	Bold(true).
	Padding(1, 1, 0, 1).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#5a5a5a"), Dark: lipgloss.Color("#999999")})

var externalRowStyle = lipgloss.NewStyle().
	Padding(0, 1, 0, 1).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#777777"), Dark: lipgloss.Color("#999999")})

var externalRowSelectedStyle = lipgloss.NewStyle().
	Padding(0, 1, 0, 1).
	Background(lipgloss.Color("#dde4f0")).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#1a1a1a"), Dark: lipgloss.Color("#1a1a1a")})

// laneStateAccentStyle/laneStateStalledStyle/laneStateIdleStyle are the
// state-word row's own colours (DECISIONS.md, 2 Sep evening, PANE-MOCKUP-*):
// working and waiting on you reuse the selected row's own accent
// (selectedTitleStyle/externalRowSelectedStyle's background colour, #dde4f0
// - the accent already used for the selected row), stalled reuses the
// Needs-you heading's orange (needsYouTitleStyle's foreground), idle is
// dim (externalRowStyle's own dim foreground).
var laneStateAccentStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#dde4f0"), Dark: lipgloss.Color("#dde4f0")})

var laneStateStalledStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#b5581a"), Dark: lipgloss.Color("#e0a458")})

var laneStateIdleStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#777777"), Dark: lipgloss.Color("#999999")})

// laneStateGlyph returns the glyph and style ClassifyState's four words
// render as - working ● and waiting on you ◉ share the accent style,
// stalled ◐ the orange, idle ○ dim. An unknown/not-yet-computed state (the
// empty string, before the first feed tick has classified this lane) draws
// a blank glyph: the row still reserves the column, it simply has nothing
// to show yet, same convention as the ctx gauge's "show nothing, not n/a".
func laneStateGlyph(state string) (string, lipgloss.Style) {
	switch state {
	case clarity.StateWorking:
		return "●", laneStateAccentStyle
	case clarity.StateWaitingYou:
		return "◉", laneStateAccentStyle
	case clarity.StateStalled:
		return "◐", laneStateStalledStyle
	case clarity.StateIdle:
		return "○", laneStateIdleStyle
	default:
		return " ", lipgloss.NewStyle()
	}
}

// laneStateWordWidth is the widest of the four state words ClassifyState
// produces ("waiting on you") - the state field is padded to this width on
// every row so the last-turn time lands in the same column regardless of
// which word a given row shows.
var laneStateWordWidth = len(clarity.StateWaitingYou)

// laneCtxFieldWidth is "ctx 100%" - the widest a context-fill percentage
// ever renders (0-100%, three digits plus a leading 100% edge case), fixed
// so the field's own width never changes row to row; the percentage
// right-aligns inside it (item 1's "ctx percentage right-aligned").
const laneCtxFieldWidth = len("ctx 100%")

// laneTimeWidth is "15:04" - the last-turn time, local, hours:minutes.
const laneTimeWidth = len("15:04")

// laneSuffixWidth is the plain-text width of laneRowSuffix's output for a
// given showWord - kept as an explicit function (not just len() on a
// sample render) so nameCol sizing and the actual render can never drift
// out of step with each other.
func laneSuffixWidth(showWord bool) int {
	// " " + ctx + "  " + glyph + " " + time
	w := 1 + laneCtxFieldWidth + 2 + 1 + 1 + laneTimeWidth
	if showWord {
		// + word + " "
		w += laneStateWordWidth + 1
	}
	return w
}

// laneCtxField renders a lane's "ctx NN%" label right-justified within a
// fixed-width field, so the trailing "%" always lands in the same column
// regardless of how many digits the percentage has (item 1's "ctx
// percentage right-aligned") - the label itself is left-aligned within
// that padding, so "ctx 42%" still appears as a literal, unbroken
// substring rather than gaining an internal gap. When the fill genuinely
// cannot be derived the number is blank (not "n/a", the OWN ROW fix's
// rule) but the "ctx" label still marks the column.
func laneCtxField(pct int, ok bool) string {
	if !ok {
		return runewidth.FillRight("ctx", laneCtxFieldWidth)
	}
	return fmt.Sprintf("%*s", laneCtxFieldWidth, fmt.Sprintf("ctx %d%%", pct))
}

// laneRowSuffix renders the fixed-width tail every lane row (tracked or
// external) shares: right-aligned ctx, the state glyph (and word, unless
// collapsed), then the last-turn time - the FINISH defect's "one table"
// requirement. Every segment carries rowBg/rowFg forward explicitly (the
// same technique the diff-stat badge already uses, see Render() below) so
// the glyph's own state colour does not reset the row's selected-highlight
// background for the characters after it.
func laneRowSuffix(rowBg, rowFg color.Color, pct int, fillOK bool, state string, lastTurn time.Time, turnOK bool, showWord bool) string {
	plain := lipgloss.NewStyle().Background(rowBg).Foreground(rowFg)
	glyph, glyphStyle := laneStateGlyph(state)
	glyphStyle = glyphStyle.Background(rowBg)

	timeText := strings.Repeat(" ", laneTimeWidth)
	if turnOK {
		timeText = lastTurn.Local().Format("15:04")
	}

	ctx := plain.Render(laneCtxField(pct, fillOK))
	timeSeg := plain.Render(timeText)
	if !showWord {
		return fmt.Sprintf(" %s  %s %s", ctx, glyphStyle.Render(glyph), timeSeg)
	}
	word := plain.Foreground(glyphStyle.GetForeground()).Render(runewidth.FillRight(state, laneStateWordWidth))
	return fmt.Sprintf(" %s  %s %s %s", ctx, glyphStyle.Render(glyph), word, timeSeg)
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

	// needsYou holds the "Needs you" feed lines, refreshed once per feed
	// tick by the caller (see app.go's feedTickMsg handling) - this struct
	// never reads the queue file itself, it only renders what it is handed.
	needsYou []string

	// external holds the fleet's live lanes that are NOT tracked Claude
	// Squad instances (see clarity.DiscoverExternalLanes), refreshed on the
	// same tick as the rest of the metadata. Rendered below the tracked
	// instances; selectable and messageable, never attachable or killable.
	external []clarity.ExternalLane

	// selExternal is true when the current selection points into external
	// rather than items - the two lists share one selection cursor that
	// wraps from the bottom of items into the top of external and back.
	selExternal bool

	// collapsed mirrors app.go's own collapsePreviewBelowWidth decision
	// (the terminal itself, not just this list's own column share, is
	// under 100 columns) - item 1's "below 100 columns drop the word, keep
	// the glyph" on every lane row. Set via SetCollapsed, never derived
	// from l.width itself: at every width this app is normally run at, the
	// list's OWN column share is under 100 regardless of whether the
	// terminal is collapsed or not, so l.width alone cannot tell the two
	// apart.
	collapsed bool
}

// SetCollapsed records whether the terminal itself is below
// app.go's collapsePreviewBelowWidth threshold.
func (l *List) SetCollapsed(collapsed bool) {
	l.collapsed = collapsed
}

// SetNeedsYou replaces the "Needs you" feed lines shown above the instance
// list.
func (l *List) SetNeedsYou(lines []string) {
	l.needsYou = lines
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
	w := AdjustPreviewWidth(l.width) - 2
	if w < 1 {
		w = 1
	}
	return w
}

// ansiTruncateRow truncates s to width cells, ansi- and grapheme-aware
// (github.com/charmbracelet/x/ansi.Truncate - never a byte slice, which
// would both miscount wide/multi-byte runes and could sever an escape
// sequence mid-code).
func ansiTruncateRow(s string, width int) string {
	return ansi.Truncate(s, width, "…")
}

// laneNameColMax is the widest the name column is ever allowed to be - long
// enough for this fleet's actual lane names (see the OVERFLOW repro
// capture: 21-27 characters) without eating into the suffix's budget on a
// wide terminal. Shared by both row kinds (item 4's "one table").
const laneNameColMax = 28

// laneRowInnerWidth converts a component's own AdjustPreviewWidth(list
// width) into the row-content budget List.rowInnerWidth() computes for the
// "Needs you"/external section (that -2 is the small left/right padding
// those styles apply). InstanceRenderer's r.width is already
// AdjustPreviewWidth(list width) (see setWidth below), so this is the one
// place both the tracked-instance title line and the external rows derive
// their shared column grid from, instead of two independently-drifting
// calculations.
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
// column pushes past the row's own truncation and off the edge.
func laneNameColWidthFor(rowInner int, showWord bool) int {
	avail := rowInner - laneSuffixWidth(showWord)
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

func (r *InstanceRenderer) setWidth(width int) {
	r.width = AdjustPreviewWidth(width)
}

// ɹ and ɻ are other options.
const branchIcon = "Ꮧ"

func (r *InstanceRenderer) Render(i *session.Instance, idx int, selected bool, hasMultipleRepos bool, showWord bool) string {
	prefix := fmt.Sprintf(" %d. ", idx)
	if idx >= 10 {
		prefix = prefix[:len(prefix)-1]
	}
	titleS := selectedTitleStyle
	descS := selectedDescStyle
	if !selected {
		titleS = titleStyle
		descS = listDescStyle
	}

	// The title line now carries the same state table every lane row
	// shares (item 1): name padded to a column, ctx right-aligned, the
	// clarity-derived state glyph and word, then last-turn time - replacing
	// the plain tmux-status glyph (ready/paused/spinner) this line used to
	// end with, since ClassifyState's four words are a strict superset of
	// what that conveyed (a Paused instance's transcript reads idle or
	// stalled on its own merits).
	nameCol := laneNameColWidthFor(laneRowInnerWidth(r.width), showWord)
	name := runewidth.FillRight(runewidth.Truncate(prefix+i.Title, nameCol, "…"), nameCol)

	pct, fillOK := i.GetContextFill()
	state, lastTurn, turnOK := i.GetLaneState()
	suffix := laneRowSuffix(titleS.GetBackground(), titleS.GetForeground(), pct, fillOK, state, lastTurn, turnOK, showWord)
	// titleS carries its own left/right Padding, added once by wrapping the
	// WHOLE truncated line in it here (not the name alone) - the truncation
	// budget (laneRowInnerWidth) already excludes that padding's width, so
	// truncating first and padding last is the order that keeps the styled
	// result within r.width; truncating an already-padded string instead
	// (as an earlier version of this line did) over-truncates by exactly
	// the padding's own width, eating into the suffix's last-turn time.
	title := titleS.Render(ansiTruncateRow(name+suffix, laneRowInnerWidth(r.width)))

	stat := i.GetDiffStats()

	var diff string
	var addedDiff, removedDiff string
	if stat == nil || stat.Error != nil || stat.IsEmpty() {
		// Don't show diff stats if there's an error or if they don't exist
		addedDiff = ""
		removedDiff = ""
		diff = ""
	} else {
		addedDiff = fmt.Sprintf("+%d", stat.Added)
		removedDiff = fmt.Sprintf("-%d ", stat.Removed)
		diff = lipgloss.JoinHorizontal(
			lipgloss.Center,
			addedLinesStyle.Background(descS.GetBackground()).Render(addedDiff),
			lipgloss.Style{}.Background(descS.GetBackground()).Foreground(descS.GetForeground()).Render(","),
			removedLinesStyle.Background(descS.GetBackground()).Render(removedDiff),
		)
	}

	remainingWidth := r.width
	remainingWidth -= runewidth.StringWidth(prefix)
	remainingWidth -= runewidth.StringWidth(branchIcon)
	remainingWidth -= 2 // for the literal " " and "-" in the branchLine format string

	diffWidth := runewidth.StringWidth(addedDiff) + runewidth.StringWidth(removedDiff)
	if diffWidth > 0 {
		diffWidth += 1
	}

	// Use fixed width for diff stats to avoid layout issues
	remainingWidth -= diffWidth

	branch := i.Branch
	if i.Started() && hasMultipleRepos {
		repoName, err := i.RepoName()
		if err != nil {
			log.ErrorLog.Printf("could not get repo name in instance renderer: %v", err)
		} else {
			branch += fmt.Sprintf(" (%s)", repoName)
		}
	}
	// Don't show branch if there's no space for it. Or show ellipsis if it's too long.
	branchWidth := runewidth.StringWidth(branch)
	if remainingWidth < 0 {
		branch = ""
	} else if remainingWidth < branchWidth {
		if remainingWidth < 3 {
			branch = ""
		} else {
			// We know the remainingWidth is at least 4 and branch is longer than that, so this is safe.
			branch = runewidth.Truncate(branch, remainingWidth-3, "...")
		}
	}
	remainingWidth -= runewidth.StringWidth(branch)

	// Add spaces to fill the remaining width.
	spaces := ""
	if remainingWidth > 0 {
		spaces = strings.Repeat(" ", remainingWidth)
	}

	// clarity-attach instances (session/clarity/attach.go) have no git
	// worktree and so no branch to show. Upstream's "<icon>-<branch>"
	// segment rendered unconditionally, so a branchless row showed a bare
	// Cherokee glyph and hyphen with nothing after it (the OWN ROW defect's
	// garbled second line) - blank space of the same width keeps the diff
	// badge lined up with every other row instead.
	branchSegment := fmt.Sprintf("%s-%s", branchIcon, branch)
	if !i.HasWorktree() {
		branchSegment = strings.Repeat(" ", runewidth.StringWidth(branchSegment))
	}

	branchLine := fmt.Sprintf("%s %s%s %s", strings.Repeat(" ", len(prefix)), branchSegment, spaces, diff)

	// join title and subtitle
	text := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		descS.Render(branchLine),
	)

	return text
}

func (l *List) String() string {
	const titleText = " Instances "
	const autoYesText = " auto-yes "

	// Write the title.
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("\n")

	// Write title line
	// add padding of 2 because the border on list items adds some extra characters
	titleWidth := AdjustPreviewWidth(l.width) + 2
	if !l.autoyes {
		b.WriteString(lipgloss.Place(
			titleWidth, 1, lipgloss.Left, lipgloss.Bottom, mainTitle.Render(titleText)))
	} else {
		title := lipgloss.Place(
			titleWidth/2, 1, lipgloss.Left, lipgloss.Bottom, mainTitle.Render(titleText))
		autoYes := lipgloss.Place(
			titleWidth-(titleWidth/2), 1, lipgloss.Right, lipgloss.Bottom, autoYesStyle.Render(autoYesText))
		b.WriteString(lipgloss.JoinHorizontal(
			lipgloss.Top, title, autoYes))
	}

	b.WriteString("\n")
	b.WriteString("\n")

	// Render the "Needs you" feed, once per feed tick (see app.go) - never
	// a bare empty section when the queue is absent, per the brief. Every
	// row is truncated to the list's own inner width first (the OVERFLOW
	// defect: a feed row can run to 100+ characters, and nothing downstream
	// clips it back down).
	innerWidth := l.rowInnerWidth()
	if len(l.needsYou) > 0 {
		b.WriteString(needsYouTitleStyle.Render(" Needs you "))
		b.WriteString("\n")
		for _, line := range l.needsYou {
			b.WriteString(needsYouLineStyle.Render(ansiTruncateRow(line, innerWidth)))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Render the list. showWord is item 1's "below 100 columns drop the
	// word, keep the glyph" - one decision per render pass, shared by every
	// tracked and external row alike (item 4's "one table").
	showWord := !l.collapsed
	for i, item := range l.items {
		selected := !l.selExternal && i == l.selectedIdx
		b.WriteString(l.renderer.Render(item, i+1, selected, len(l.repos) > 1, showWord))
		if i != len(l.items)-1 {
			b.WriteString("\n\n")
		}
	}

	// Render the external lanes - live on this Mac but not tracked here
	// (see clarity.DiscoverExternalLanes). Message-only: no diff stats, no
	// branch, no attach/kill affordance, because none of that exists for a
	// lane with no tracked tmux session or git worktree.
	if len(l.external) > 0 {
		if len(l.items) > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(externalTitleStyle.Render(" External lanes (message only) "))
		b.WriteString("\n")
		for i, lane := range l.external {
			style := externalRowStyle
			if l.selExternal && i == l.selectedIdx {
				style = externalRowSelectedStyle
			}
			// Pad (or truncate) the name to a fixed column first, so every
			// row's suffix lines up regardless of how long an individual
			// lane name is - then truncate the WHOLE row to the list's
			// inner width, since a name near the column width plus the
			// fixed suffix can still run past a narrow terminal.
			nameCol := l.laneNameColWidth(showWord)
			name := runewidth.FillRight(ansiTruncateRow(lane.Name, nameCol), nameCol)
			suffix := laneRowSuffix(style.GetBackground(), style.GetForeground(),
				lane.Fill.Pct, lane.FillOK, lane.State, lane.LastTurn, lane.StateOK, showWord)
			// style carries its own left/right Padding, added once by
			// wrapping the WHOLE truncated line in it here (not the name
			// alone) - see the matching comment on the tracked-row title
			// line above; the same over-truncation bug applied here first.
			line := name + suffix
			b.WriteString(style.Render(ansiTruncateRow(line, innerWidth)))
			b.WriteString("\n")
		}
	}

	// Cap the block to l.height before handing it to Place: with the "Needs
	// you" feed, every tracked instance and a full external-lane section
	// all present at once, the natural content height can exceed l.height
	// on a short terminal (24 rows, say) - and lipgloss.Place's own
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
	content := b.String()
	if l.height > 0 {
		lines := strings.Split(content, "\n")
		if len(lines) > l.height {
			content = strings.Join(lines[:l.height], "\n")
		}
	}

	return lipgloss.Place(l.width, l.height, lipgloss.Left, lipgloss.Top, content)
}

// Down selects the next item in the list, crossing from the tracked
// instances into the external rows (and wrapping back to the top of items)
// when external rows are present.
func (l *List) Down() {
	if len(l.items) == 0 && len(l.external) == 0 {
		return
	}
	if !l.selExternal {
		if l.selectedIdx < len(l.items)-1 {
			l.selectedIdx++
			return
		}
		if len(l.external) > 0 {
			l.selExternal = true
			l.selectedIdx = 0
			return
		}
		l.selectedIdx = 0
		return
	}
	// Currently on an external row.
	if l.selectedIdx < len(l.external)-1 {
		l.selectedIdx++
		return
	}
	l.selExternal = false
	l.selectedIdx = 0
}

// Kill selects the next item in the list. A no-op when the selection is on
// an external row - there is no tracked instance behind it to kill.
func (l *List) Kill() {
	if l.selExternal || len(l.items) == 0 {
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

	// Unregister the reponame.
	repoName, err := targetInstance.RepoName()
	if err != nil {
		log.ErrorLog.Printf("could not get repo name: %v", err)
	} else {
		l.rmRepo(repoName)
	}

	// Since there's items after this, the selectedIdx can stay the same.
	l.items = append(l.items[:l.selectedIdx], l.items[l.selectedIdx+1:]...)
}

// Attach attaches to the selected tracked instance. Returns an error
// without attaching anything when the selection is on an external row -
// there is no tracked tmux session behind it to attach to.
func (l *List) Attach() (chan struct{}, error) {
	if l.selExternal || len(l.items) == 0 || l.selectedIdx >= len(l.items) {
		return nil, errors.New("cannot attach: no tracked instance is selected")
	}
	targetInstance := l.items[l.selectedIdx]
	return targetInstance.Attach()
}

// Up selects the previous item in the list, crossing from the external rows
// into the tracked instances (and wrapping back to the bottom of external)
// when external rows are present.
func (l *List) Up() {
	if len(l.items) == 0 && len(l.external) == 0 {
		return
	}
	if l.selExternal {
		if l.selectedIdx > 0 {
			l.selectedIdx--
			return
		}
		if len(l.items) > 0 {
			l.selExternal = false
			l.selectedIdx = len(l.items) - 1
			return
		}
		l.selectedIdx = len(l.external) - 1
		return
	}
	// Currently on a tracked instance.
	if l.selectedIdx > 0 {
		l.selectedIdx--
		return
	}
	if len(l.external) > 0 {
		l.selExternal = true
		l.selectedIdx = len(l.external) - 1
		return
	}
	l.selectedIdx = len(l.items) - 1
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
	// The finalizer registers the repo name once the instance is started.
	return func() {
		repoName, err := instance.RepoName()
		if err != nil {
			log.ErrorLog.Printf("could not get repo name: %v", err)
			return
		}

		l.addRepo(repoName)
	}
}

// GetSelectedInstance returns the currently selected tracked instance, or
// nil when the selection is on an external row (or the list is empty) - an
// external row cannot be attached, killed, or otherwise treated as a
// tracked instance, so every caller that already nil-checks this (kill,
// attach, checkout, push, resume, move) gets that guard for free.
func (l *List) GetSelectedInstance() *session.Instance {
	if l.selExternal || len(l.items) == 0 || l.selectedIdx >= len(l.items) {
		return nil
	}
	return l.items[l.selectedIdx]
}

// GetSelectedExternalLane returns the currently selected external lane, or
// ok=false when the selection is on a tracked instance (or nothing is
// selected) - the external-row counterpart to GetSelectedInstance, used by
// the Session tab (design/cockpit-pane/DECISIONS.md slice 3) to build its
// SessionInfo for whichever kind of row is selected.
func (l *List) GetSelectedExternalLane() (clarity.ExternalLane, bool) {
	if !l.selExternal || l.selectedIdx < 0 || l.selectedIdx >= len(l.external) {
		return clarity.ExternalLane{}, false
	}
	return l.external[l.selectedIdx], true
}

// SelectedMsgTarget returns the lane name of the current selection,
// whichever list it is in, plus whether it is an external row - both
// tracked instances and external rows are messageable (the brief's
// requirement), only tracked instances are attachable/killable. ok is
// false when nothing is selected (both lists empty, or the index is out of
// range for its list).
func (l *List) SelectedMsgTarget() (lane string, isExternal bool, ok bool) {
	if l.selExternal {
		if l.selectedIdx < 0 || l.selectedIdx >= len(l.external) {
			return "", false, false
		}
		return l.external[l.selectedIdx].Name, true, true
	}
	if l.selectedIdx < 0 || l.selectedIdx >= len(l.items) {
		return "", false, false
	}
	return l.items[l.selectedIdx].Title, false, true
}

// SetSelectedInstance sets the selected index. Noop if the index is out of bounds.
func (l *List) SetSelectedInstance(idx int) {
	if idx >= len(l.items) {
		return
	}
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
