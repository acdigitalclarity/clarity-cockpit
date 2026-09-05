// Package app: view assembly helpers for home.View() (cockpit slice 20C,
// COCKPIT-CONTRACT.md's own S2 item - the idle-CPU remainder measured at
// the home.View() level after slice 20 and slice 20B cut TabbedWindow's own
// render cost and the external-lane discovery walk). pprof on origin/main
// (0dbcad5) put the rest of View()'s own cumulative time in three call
// sites: the two lipgloss.NewStyle().PaddingTop(1).Render calls over the
// list and preview strings, and the lipgloss.JoinHorizontal that lays them
// side by side, plus the lipgloss.JoinVertical that stacks the result over
// the menu and footer rows - app/app.go View(), lines 3488-3522 on that
// commit.
//
// Every one of the three call sites re-measures display width with
// github.com/charmbracelet/x/ansi.StringWidth line by line
// (lipgloss/v2's own getLines/alignTextHorizontal, style.go and join.go at
// charm.land/lipgloss/v2@v2.0.6 - read in full before writing this file),
// and the chain re-measures the SAME lines more than once: PaddingTop's own
// Render call already pads every line to the block's own widest, then
// JoinHorizontal's own getLines call walks the padded result again to find
// that same widest line, and JoinVertical's does the same a third time over
// its own three arguments.
//
// The three functions below replace those three call sites with a single
// measurement pass per block (measureBlock), reproducing the exact
// documented lipgloss algorithm line for line - never a cached frame, never
// memoised on state or wall clock (the 4 Sep 00:1x ways-of-working ruling
// on slice 20B: a memoised render carries a stale-render risk for a gain
// inside the load noise, so this slice does not repeat that shape). Every
// call still re-measures this frame's own content from scratch; the saving
// is structural, not a cache. A block whose lines are not already uniform
// width - never expected in production, since ui/list.go's own String()
// finishes with lipgloss.Place(l.width, l.height, ...) and
// ui/tabbed_window.go's own String() is built from Width()-forced,
// Place()-padded pieces (both read in full for this slice) - falls back to
// the real lipgloss functions unchanged, so a future change to either file
// that stops guaranteeing fixed-width lines degrades to today's exact
// bytes and cost rather than producing a wrong frame.
package app

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// measuredBlock is one rendered string's own lines, each line's own
// github.com/charmbracelet/x/ansi.StringWidth (the exact function
// lipgloss/v2's getLines calls), the block's own widest line, and whether
// every line already equals that widest - computed once per block and
// reused by every assembly step below, rather than asking lipgloss to redo
// the same getLines walk a second or third time over content this package
// already measured.
type measuredBlock struct {
	lines   []string
	widths  []int
	widest  int
	uniform bool
}

// measureBlock splits s into lines the same way lipgloss/v2's own getLines
// does (get.go: tabs expanded to four spaces, CRLF normalised to LF first,
// so a raw Terminal-tab pty capture measures identically here and there),
// then measures every line's display width with the same
// ansi.StringWidth call getLines makes.
func measureBlock(s string) measuredBlock {
	s = strings.ReplaceAll(s, "\t", "    ")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")

	mb := measuredBlock{lines: lines, widths: make([]int, len(lines)), uniform: true}
	for i, l := range lines {
		w := ansi.StringWidth(l)
		mb.widths[i] = w
		if w > mb.widest {
			mb.widest = w
		}
	}
	for _, w := range mb.widths {
		if w != mb.widest {
			mb.uniform = false
			break
		}
	}
	return mb
}

// padTop1 reproduces lipgloss.NewStyle().PaddingTop(1).Render(s) for the
// two call sites in home.View() that use it (item 2a) - both call the
// style with no other property set (no colour, no width, no border), which
// style.go's own Render reduces to exactly this: the "core text" pass is a
// no-op for a style with nothing else set (ansi.Style's own zero value has
// len(s)==0, and Styled(str) on that returns str unchanged - verified
// against charm.land/lipgloss/v2@v2.0.6/style.go and
// github.com/charmbracelet/x/ansi@v0.11.8/style.go's own Styled); topPadding
// prepends one "\n"; the alignment pass that follows then pads every line
// (the new blank one included) up to the block's own widest line, since
// width is 0 (unset) and colorWhitespace's own zero-value ansi.Style is
// likewise a no-op, so the padding spaces are always plain, unstyled
// spaces. measureBlock already has every line's own width and the widest
// among them, so this rebuilds the identical bytes without a second
// getLines/alignTextHorizontal walk over the same content, and needs no
// fallback: the pad-to-widest step is correct whether or not the input
// happens to already be uniform width.
func padTop1(s string) string {
	mb := measureBlock(s)

	var b strings.Builder
	b.Grow(mb.widest + 1 + len(s) + len(mb.lines)*4)
	b.WriteString(strings.Repeat(" ", mb.widest))
	b.WriteByte('\n')
	for i, line := range mb.lines {
		b.WriteString(line)
		if d := mb.widest - mb.widths[i]; d > 0 {
			b.WriteString(strings.Repeat(" ", d))
		}
		if i < len(mb.lines)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// joinHorizontalTop reproduces lipgloss.JoinHorizontal(lipgloss.Top, left,
// right) for the one call site in home.View() (item 2b): join.go's own
// switch pads the shorter block with blank lines appended at the BOTTOM
// (Top alignment) up to the taller block's line count, then lays every row
// side by side, each half right-padded to ITS OWN block's widest line
// (never the other block's).
//
// ui/list.go's own String() always finishes with
// lipgloss.Place(l.width, l.height, lipgloss.Left, lipgloss.Top, content)
// (read in full for this slice), which pads every line to l.width exactly -
// so every real call from home.View() has l.uniform true on this function's
// left argument. The fast path below trusts that (no per-line padding
// arithmetic - every line is already at its own block's widest, so the
// pad amount is always zero) and, when either side is NOT already uniform
// width, falls back to the real lipgloss.JoinHorizontal unchanged rather
// than risk a hand-rolled ragged-block case this slice never needs to get
// right - see the falls-back test in view_assembly_test.go for the proof
// that this produces byte-identical output to real lipgloss even then.
func joinHorizontalTop(left, right string) string {
	l := measureBlock(left)
	r := measureBlock(right)
	if !l.uniform || !r.uniform {
		return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}

	height := max(len(l.lines), len(r.lines))
	blankLeft := strings.Repeat(" ", l.widest)
	blankRight := strings.Repeat(" ", r.widest)

	var b strings.Builder
	b.Grow(len(left) + len(right) + height*2)
	for i := 0; i < height; i++ {
		if i < len(l.lines) {
			b.WriteString(l.lines[i])
		} else {
			b.WriteString(blankLeft)
		}
		if i < len(r.lines) {
			b.WriteString(r.lines[i])
		} else {
			b.WriteString(blankRight)
		}
		if i < height-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// joinVerticalLeft reproduces lipgloss.JoinVertical(lipgloss.Left,
// blocks...) for the one call site in home.View() (item 2c): join.go's own
// implementation pads every line of every block to the SINGLE widest line
// across ALL blocks (unlike joinHorizontalTop's per-block widths above),
// left-aligned, so the padding lands entirely on the right.
//
// The two zero/one-argument shapes are reproduced exactly as join.go's own
// JoinVertical special-cases them (an empty call returns "", a single
// block returns it completely unchanged - never padded, even if its own
// lines are ragged), though home.View()'s own call site always passes
// exactly three. As with joinHorizontalTop, a block whose lines are not
// already uniform width falls back to the real lipgloss.JoinVertical
// unchanged.
func joinVerticalLeft(blocks ...string) string {
	if len(blocks) == 0 {
		return ""
	}
	if len(blocks) == 1 {
		return blocks[0]
	}

	measured := make([]measuredBlock, len(blocks))
	maxWidth := 0
	allUniform := true
	totalLines := 0
	for i, s := range blocks {
		measured[i] = measureBlock(s)
		if !measured[i].uniform {
			allUniform = false
		}
		if measured[i].widest > maxWidth {
			maxWidth = measured[i].widest
		}
		totalLines += len(measured[i].lines)
	}
	if !allUniform {
		return lipgloss.JoinVertical(lipgloss.Left, blocks...)
	}

	var b strings.Builder
	written := 0
	for _, mb := range measured {
		pad := ""
		if d := maxWidth - mb.widest; d > 0 {
			pad = strings.Repeat(" ", d)
		}
		for _, line := range mb.lines {
			b.WriteString(line)
			b.WriteString(pad)
			written++
			if written < totalLines {
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}
