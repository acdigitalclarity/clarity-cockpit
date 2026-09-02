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

// needsYouLineSelectedStyle is the current Needs-you row's own highlight -
// the same background externalRowSelectedStyle already uses, so the one
// cursor reads as one style regardless of which of the three groups it is
// currently in (the brief's "the current highlight style").
var needsYouLineSelectedStyle = lipgloss.NewStyle().
	Padding(0, 0, 0, 1).
	Background(lipgloss.Color("#dde4f0")).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#1a1a1a"), Dark: lipgloss.Color("#1a1a1a")})

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
// external) shares: right-aligned ctx, the state glyph (and word, unless
// collapsed), then the last-turn time (unless the row is too narrow to
// carry it - laneShowTime) - the FINISH defect's "one table" requirement.
// Every segment carries rowBg/rowFg forward explicitly (the same technique
// the diff-stat badge already uses, see Render() below) so the glyph's own
// state colour does not reset the row's selected-highlight background for
// the characters after it.
func laneRowSuffix(rowBg, rowFg color.Color, pct int, fillOK bool, state string, lastTurn time.Time, turnOK bool, showWord, showTime bool) string {
	plain := lipgloss.NewStyle().Background(rowBg).Foreground(rowFg)
	glyph, glyphStyle := laneStateGlyph(state)
	glyphStyle = glyphStyle.Background(rowBg)

	pctSeg := plain.Render(lanePctField(pct, fillOK))

	var wordSeg string
	if showWord {
		word := plain.Foreground(glyphStyle.GetForeground()).
			Render(runewidth.FillRight(laneStateDisplayWord(state), laneStateWordWidth))
		wordSeg = " " + word
	}

	var timeSeg string
	if showTime {
		timeText := strings.Repeat(" ", laneTimeWidth)
		if turnOK {
			timeText = lastTurn.Local().Format("15:04")
		}
		timeSeg = " " + plain.Render(timeText)
	}

	return fmt.Sprintf(" %s  %s%s%s", pctSeg, glyphStyle.Render(glyph), wordSeg, timeSeg)
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
}

// SetCollapsed records whether the terminal itself is below
// app.go's collapsePreviewBelowWidth threshold.
func (l *List) SetCollapsed(collapsed bool) {
	l.collapsed = collapsed
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
	rowInner := laneRowInnerWidth(r.width)
	nameCol := laneNameColWidthFor(rowInner, showWord)
	name := runewidth.FillRight(runewidth.Truncate(prefix+i.Title, nameCol, "…"), nameCol)

	pct, fillOK := i.GetContextFill()
	state, lastTurn, turnOK := i.GetLaneState()
	suffix := laneRowSuffix(titleS.GetBackground(), titleS.GetForeground(), pct, fillOK, state, lastTurn, turnOK, showWord, laneShowTime(rowInner))
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

	// laneRowInnerWidth(r.width), not r.width itself: descS (below) carries
	// the same left/right Padding as titleS, so the branch line's own fill
	// target has to leave the same 2 columns of room the title line already
	// does via this same helper - a pre-existing gap in this budget (it
	// always started from the bare r.width) that stayed invisible while
	// r.width was itself discounted 10% by AdjustPreviewWidth, and became a
	// real overflow the moment defect 2 removed that discount to give the
	// list column back its own full width.
	remainingWidth := laneRowInnerWidth(r.width)
	remainingWidth -= runewidth.StringWidth(prefix)
	remainingWidth -= runewidth.StringWidth(branchIcon)
	// 3, not 2: the branchLine format string below ("%s %s%s %s") carries
	// TWO literal separator spaces (one before branchSegment, one before
	// diff) plus the "-" inside branchSegment's own "<icon>-<branch>" -
	// this budget only ever subtracted one of the two spaces, a pre-
	// existing gap that stayed invisible while r.width was itself
	// discounted 10% by AdjustPreviewWidth and became a real one-column
	// overflow the moment defect 2 removed that discount.
	remainingWidth -= 3

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
	// clips it back down). Rows are selectable (slice 5): the current one
	// carries the same highlight style external rows already use.
	innerWidth := l.rowInnerWidth()
	if len(l.needsYou) > 0 || l.needsYouStatus != "" {
		b.WriteString(needsYouTitleStyle.Render(" Needs you "))
		b.WriteString("\n")
		if l.needsYouStatus != "" {
			b.WriteString(needsYouLineStyle.Render(ansiTruncateRow(l.needsYouStatus, innerWidth)))
			b.WriteString("\n")
		}
		for i, item := range l.needsYou {
			style := needsYouLineStyle
			if l.selNeedsYou && i == l.selectedIdx {
				style = needsYouLineSelectedStyle
			}
			b.WriteString(style.Render(ansiTruncateRow(clarity.FeedLine(item), innerWidth)))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Render the list. showWord is item 1's "below 100 columns drop the
	// word, keep the glyph" - one decision per render pass, shared by every
	// tracked and external row alike (item 4's "one table").
	showWord := !l.collapsed
	for i, item := range l.items {
		selected := !l.selExternal && !l.selNeedsYou && i == l.selectedIdx
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
				lane.Fill.Pct, lane.FillOK, lane.State, lane.LastTurn, lane.StateOK, showWord, laneShowTime(innerWidth))
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
	return l.items[l.selectedIdx].Title, false, true
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
