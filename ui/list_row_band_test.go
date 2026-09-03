package ui

import (
	"claude-squad/session"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	"github.com/charmbracelet/x/ansi"
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
		55, true, "waiting on you", time.Now(), true, true, true)

	require.True(t, runHasContinuousBackground(out), "raw: %q", out)
}

// TestLaneRowSuffix_SelectedRow_NoWordOrTime_StillContinuous covers the
// narrower-width shapes (showWord/showTime both false, THE RULE's own
// collapse points) - the fix must hold regardless of which segments are in
// play, not just the widest row.
func TestLaneRowSuffix_SelectedRow_NoWordOrTime_StillContinuous(t *testing.T) {
	out := laneRowSuffix(selectedTitleStyle.GetBackground(), selectedTitleStyle.GetForeground(),
		7, true, "idle", time.Now(), true, false, false)

	require.True(t, runHasContinuousBackground(out), "raw: %q", out)
}

// trimAfterLastEscape cuts off any plain text after the last ANSI escape
// sequence in s - lipgloss.JoinVertical (Render's own final step, joining
// the title line above the branch line) pads a shorter line out to the
// tallest line's own width with bare literal spaces, same shape as DEFECT
// 2 itself but a separate, pre-existing lipgloss behaviour past the row's
// actual visible content, not the "around the glyph and word" gap this
// defect names - excluded here so the check is scoped to the content
// laneRowSuffix actually renders.
func trimAfterLastEscape(s string) string {
	idx := strings.LastIndex(s, "\x1b[")
	if idx < 0 {
		return s
	}
	end := strings.IndexByte(s[idx:], 'm')
	if end < 0 {
		return s
	}
	return s[:idx+end+1]
}

// TestInstanceRenderer_SelectedRow_TitleLineOneContinuousBackground is the
// same DEFECT 2 proof one level up, through the actual row-render path a
// selected tracked instance draws (InstanceRenderer.Render), not just
// laneRowSuffix in isolation - the PROOF (b) shape: the selected row's own
// title line (name + suffix, wrapped once in selectedTitleStyle) must be
// one continuous highlighted band. Render's own output is 4 lines (Padding
// on selectedTitleStyle/selectedDescStyle adds a blank line above and
// below) - line index 1 is the actual "N. name ... time" content line.
func TestInstanceRenderer_SelectedRow_TitleLineOneContinuousBackground(t *testing.T) {
	sp := spinner.New()
	r := &InstanceRenderer{spinner: &sp}
	r.setWidth(120)

	inst, err := session.NewInstance(session.InstanceOptions{Title: "ways-of-working", Path: ".", Program: "echo"})
	require.NoError(t, err)
	inst.SetContextFill(55, true)
	inst.SetLaneState("waiting on you", time.Now(), true)

	rendered := r.Render(inst, 1, true, false, true)
	lines := strings.Split(rendered, "\n")
	require.Greater(t, len(lines), 1, "raw: %q", rendered)
	titleLine := trimAfterLastEscape(lines[1])

	require.True(t, runHasContinuousBackground(titleLine), "raw: %q", titleLine)
	require.Contains(t, ansi.Strip(titleLine), "ways-of-working")
}
