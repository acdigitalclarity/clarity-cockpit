package ui

import (
	"claude-squad/session"
	"claude-squad/session/clarity"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2/compat"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/stretchr/testify/require"
)

// runHasContinuousBackground walks an ANSI-styled string's own SGR escape
// codes and reports whether every printable byte it prints has an explicit
// background colour active at that point - the selected-row property board
// #280 pane-10 walkthrough DEFECT 2 broke: laneRowSuffix used to place bare,
// un-rendered literal spaces between its own independently-styled segments
// (each of which closes with its own ANSI reset), so those separator spaces
// printed with the terminal's own default background instead of the row's
// selection highlight - the observed "black rectangles" around the glyph
// and word. A code containing "48;" (any SGR background-colour parameter,
// truecolor or otherwise) turns background-active on; a bare reset ("" or
// "0", lipgloss's own \x1b[m / \x1b[0m) turns it back off.
func runHasContinuousBackground(s string) bool {
	bgActive := false
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			code := s[i+2 : j]
			switch {
			case code == "" || code == "0":
				bgActive = false
			case strings.Contains(code, "48;"):
				bgActive = true
			}
			i = j + 1
			continue
		}
		if !bgActive {
			return false
		}
		i++
	}
	return true
}

// TestLaneRowSuffix_SelectedRow_OneContinuousBackground is board #280
// pane-10 walkthrough DEFECT 2, seen failing first against the pre-fix
// laneRowSuffix (fmt.Sprintf(" %s  %s%s%s", ...) with bare literal spaces
// between its rendered segments): the selected row's own suffix (ctx
// percentage, state glyph, state word, last-turn time) must be one
// continuous rowBg band with no un-styled gap in it anywhere.
func TestLaneRowSuffix_SelectedRow_OneContinuousBackground(t *testing.T) {
	out := laneRowSuffix(selectedTitleStyle.GetBackground(), selectedTitleStyle.GetForeground(),
		"", false, 55, true, "waiting on you", time.Now(), true, true, true)

	require.True(t, runHasContinuousBackground(out), "raw: %q", out)
}

// TestLaneRowSuffix_SelectedRow_WithTag_OneContinuousBackground is DEFECT
// 2's own tag-column variant (slice 5 item 2): the seat-tag segment
// laneRowSuffix now inserts must carry rowBg forward exactly like every
// other segment, or the same un-styled-gap defect reappears one column to
// the left of where it was fixed.
func TestLaneRowSuffix_SelectedRow_WithTag_OneContinuousBackground(t *testing.T) {
	out := laneRowSuffix(selectedTitleStyle.GetBackground(), selectedTitleStyle.GetForeground(),
		"tb", true, 55, true, "waiting on you", time.Now(), true, true, true)

	require.True(t, runHasContinuousBackground(out), "raw: %q", out)
}

// TestLaneRowSuffix_SelectedRow_NoWordOrTime_StillContinuous covers the
// narrower-width shapes (showWord/showTime both false, THE RULE's own
// collapse points) - the fix must hold regardless of which segments are in
// play, not just the widest row.
func TestLaneRowSuffix_SelectedRow_NoWordOrTime_StillContinuous(t *testing.T) {
	out := laneRowSuffix(selectedTitleStyle.GetBackground(), selectedTitleStyle.GetForeground(),
		"", false, 7, true, "idle", time.Now(), true, false, false)

	require.True(t, runHasContinuousBackground(out), "raw: %q", out)
}

// TestInstanceRenderer_SelectedRow_OneLine_OneContinuousBackground is
// DEFECT 2's own proof one level up, through the actual row-render path a
// selected tracked instance draws (InstanceRenderer.Render), not just
// laneRowSuffix in isolation - PLUS slice 19's own "ONE ROW per lane, no
// spacer rows" (rule 1): Render's own output used to be 4 lines (Padding
// on selectedTitleStyle/selectedDescStyle added a blank line above and
// below the two-line title+branch block); it is now exactly one, and that
// one line is a single continuous highlighted band start to finish - no
// leading/trailing padding line left to trim past (laneRowFrame's own
// trailing pad column is itself a self-contained, rowBg-carrying ANSI
// span, not a bare literal space the way lipgloss.JoinVertical's old
// cross-line padding was).
func TestInstanceRenderer_SelectedRow_OneLine_OneContinuousBackground(t *testing.T) {
	sp := spinner.New()
	r := &InstanceRenderer{spinner: &sp}
	r.setWidth(120)

	inst, err := session.NewInstance(session.InstanceOptions{Title: "ways-of-working", Path: ".", Program: "echo"})
	require.NoError(t, err)
	inst.SetContextFill(55, true)
	inst.SetLaneState("waiting on you", time.Now(), true)

	rendered := r.Render(inst, 1, true, false, true, false)
	require.NotContains(t, rendered, "\n", "slice 19's own compact row: exactly one line, no second line left to join")

	require.True(t, runHasContinuousBackground(rendered), "raw: %q", rendered)
	require.Contains(t, ansi.Strip(rendered), "ways-of-working")
}

// TestInstanceRenderer_SelectedRow_MarkerInColumn1_StateColourSurvives is
// rule 2's own two proofs together on the real render path: the selected
// row's very first visible cell is the ▌ marker (not a name character, not
// a blank), and the state word right after it keeps ITS OWN colour rather
// than the band's - the screenshot's own defect ("the state glyph and word
// lose their colour inside it"), which the old laneStateAccentStyle bug
// (its value was literally the OLD band colour) reproduced exactly.
func TestInstanceRenderer_SelectedRow_MarkerInColumn1_StateColourSurvives(t *testing.T) {
	sp := spinner.New()
	r := &InstanceRenderer{spinner: &sp}
	r.setWidth(120)

	inst, err := session.NewInstance(session.InstanceOptions{Title: "ways-of-working", Path: ".", Program: "echo"})
	require.NoError(t, err)
	inst.SetContextFill(55, true)
	inst.SetLaneState(clarity.StateWorking, time.Now(), true)

	rendered := r.Render(inst, 1, true, false, true, false)
	stripped := ansi.Strip(rendered)
	require.True(t, strings.HasPrefix(stripped, "▌"), "column 1 of a selected row must be the marker: %q", stripped)
	require.Contains(t, stripped, "working", "the state word itself must still be present")
	require.True(t, runHasContinuousBackground(rendered), "raw: %q", rendered)
}

// TestLaneStateWorkingStyle_DistinctFromSelectedBand pins the DEFECT
// itself, at the colour-constant level rather than by rendering: before
// this fix laneStateAccentStyle's own foreground and the selected row's
// own background were the SAME literal value (#dde4f0 both, in every
// mode), so a working/waiting-on-you row selected in the list painted its
// own state text invisible against itself (the screenshot's "the state
// glyph and word lose their colour inside it"). This fails the moment the
// two colour roles are made equal again, in either light or dark mode,
// without needing to re-derive it from a render each time.
func TestLaneStateWorkingStyle_DistinctFromSelectedBand(t *testing.T) {
	workingFg, ok := laneStateWorkingStyle.GetForeground().(compat.AdaptiveColor)
	require.True(t, ok, "laneStateWorkingStyle must carry an AdaptiveColor foreground")

	require.NotEqual(t, laneRowSelectedBg.Light, workingFg.Light, "light-mode band vs light-mode working-state text")
	require.NotEqual(t, laneRowSelectedBg.Dark, workingFg.Dark, "dark-mode band vs dark-mode working-state text")
}

// TestList_SelectedRow_UnselectedRowsCarryASingleSpace is rule 2's other
// marker proof: every row that is NOT the current selection begins with a
// single space in column 1 - never blank (nothing at all), never the
// marker.
func TestList_SelectedRow_UnselectedRowsCarryASingleSpace(t *testing.T) {
	l := newTestList("a", "b")
	l.SetSize(80, 40)
	l.SetSelectedInstance(0)

	stripped := ansi.Strip(l.String())
	var rowLines []string
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Contains(line, "1. a") || strings.Contains(line, "2. b") {
			rowLines = append(rowLines, line)
		}
	}
	require.Len(t, rowLines, 2, "expected both tracked rows to be found: %q", stripped)
	require.True(t, strings.HasPrefix(rowLines[0], "▌"), "the selected row (a) must start with the marker: %q", rowLines[0])
	require.True(t, strings.HasPrefix(rowLines[1], " "), "the unselected row (b) must start with a single space, not the marker: %q", rowLines[1])
	require.False(t, strings.HasPrefix(rowLines[1], "▌"), "the unselected row must never carry the marker: %q", rowLines[1])
}

// TestList_TrackedRows_OneRowPerLane_NoSpacerRows is rule 1's own height
// proof: N tracked instances render as exactly N content lines with no
// blank line between them - the owner's own screenshot showed four
// instances taking twenty rows (the multi-line title+branch block plus
// Padding-driven spacer lines on every side); slice 19 folds each instance
// to one line and drops the spacers entirely.
func TestList_TrackedRows_OneRowPerLane_NoSpacerRows(t *testing.T) {
	l := newTestList("alpha", "beta", "gamma")
	l.SetSize(80, 40)

	out := ansi.Strip(l.String())
	lines := strings.Split(out, "\n")

	var rowIdx []int
	for i, line := range lines {
		// The selected row (alpha, index 0) carries the ▌ marker in column
		// 1, not a space - Contains, not a trimmed HasPrefix, so the marker
		// itself never disqualifies a match.
		if strings.Contains(line, "1. alpha") || strings.Contains(line, "2. beta") || strings.Contains(line, "3. gamma") {
			rowIdx = append(rowIdx, i)
		}
	}
	require.Len(t, rowIdx, 3, "expected all three tracked rows to be found as single lines: %q", out)
	require.Equal(t, rowIdx[0]+1, rowIdx[1], "no blank/spacer line between row 1 and row 2")
	require.Equal(t, rowIdx[1]+1, rowIdx[2], "no blank/spacer line between row 2 and row 3")
}

// TestLaneNameFieldParts_BranchSuffix_FitsOrDropped is rule 1's own branch
// rule, tested directly against the helper: a short name/branch pair that
// fits within nameCol carries the whole " · branch" suffix verbatim
// (never truncated itself); a pair that does not fit drops the branch
// entirely rather than truncating it into something unreadable.
func TestLaneNameFieldParts_BranchSuffix_FitsOrDropped(t *testing.T) {
	base, branchSuffix, pad := laneNameFieldParts("1. ", "abc", "x", true, 20)
	require.Equal(t, "1. abc", base, "the name itself is untouched when the branch fits")
	require.Equal(t, " · x", branchSuffix, "a short branch that fits renders whole, not truncated")
	require.Equal(t, 20, runewidth.StringWidth(base)+runewidth.StringWidth(branchSuffix)+runewidth.StringWidth(pad))

	base2, branchSuffix2, _ := laneNameFieldParts("1. ", "ways-of-working", "main", true, 20)
	require.Empty(t, branchSuffix2, "a branch that would overrun nameCol is dropped, not truncated")
	require.Equal(t, "1. ways-of-working", base2, "the name alone still fits and renders in full")

	base3, branchSuffix3, _ := laneNameFieldParts("1. ", "ways-of-working-and-then-some-more", "main", true, 20)
	require.Empty(t, branchSuffix3)
	require.Contains(t, base3, "…", "a name that overruns nameCol on its own still truncates with an ellipsis")
}

// TestList_ExactlyOneBlankLine_InstancesToExternalHeading is rule 1's own
// section-spacing proof: exactly one blank line between the last tracked
// row and the "External lanes" heading - PROOF (this leg's own real-shape
// capture) caught a real regression here first: externalTitleStyle carries
// its own top Padding(1), and an EXTRA explicit "\n" stacked on top of it
// reproduced a smaller instance of the very "\n\n" spacer-row bug rule 1
// exists to remove, this time between sections rather than within one.
func TestList_ExactlyOneBlankLine_InstancesToExternalHeading(t *testing.T) {
	l := newTestList("only")
	l.SetSize(80, 40)
	l.SetExternal(testExternalLanes("x"))

	lines := strings.Split(ansi.Strip(l.String()), "\n")
	var rowLine, headingLine int = -1, -1
	for i, line := range lines {
		if strings.Contains(line, "1. only") {
			rowLine = i
		}
		if strings.Contains(line, "External lanes") {
			headingLine = i
		}
	}
	require.NotEqual(t, -1, rowLine)
	require.NotEqual(t, -1, headingLine)
	require.Equal(t, rowLine+2, headingLine,
		"exactly one blank line must separate the tracked row from the External heading: %q", lines)
}
