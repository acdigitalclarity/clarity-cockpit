// Package app: golden tests for view_assembly.go (cockpit slice 20C,
// COCKPIT-CONTRACT.md S2) - proving padTop1/joinHorizontalTop/
// joinVerticalLeft produce byte-identical output to the lipgloss assembly
// they replace in home.View(), for a fixture home with three lanes and one
// selected, with no overlay and with each overlay open in turn (item 3a),
// plus the disagreement fallback (item 3b).
package app

import (
	"claude-squad/ui/overlay"
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/stretchr/testify/require"
)

// oldViewContent is home.View()'s own pre-slice-20C assembly, kept here as
// the oracle exactly as it read on origin/main (0dbcad5) before this
// slice's edit - the plain lipgloss.NewStyle().PaddingTop(1).Render/
// JoinHorizontal/JoinVertical chain, never touched by this slice's own
// helpers. Any divergence between this and the real (*home).View()'s own
// Content is a defect in padTop1/joinHorizontalTop/joinVerticalLeft, not a
// difference in what list.String()/tabbedWindow.String()/menu.String()
// themselves produce - both paths call those same three methods once each.
func oldViewContent(m *home) string {
	if m.splashModel != nil {
		return m.splashModel.View()
	}

	listWithPadding := lipgloss.NewStyle().PaddingTop(1).Render(m.list.String())

	previewContent := m.tabbedWindow.String()
	if m.state == stateNew && m.newLaneOverlay != nil {
		previewContent = overlay.PlaceOverlay(0, 0, m.newLaneOverlay.Render(), previewContent, false, true)
	}
	previewWithPadding := lipgloss.NewStyle().PaddingTop(1).Render(previewContent)
	listAndPreview := lipgloss.JoinHorizontal(lipgloss.Top, listWithPadding, previewWithPadding)

	footer := m.errBox.String()
	if !m.hasErr && m.statusText != "" {
		footer = m.statusBox.String()
	}

	mainView := lipgloss.JoinVertical(
		lipgloss.Left,
		listAndPreview,
		m.menu.String(),
		footer,
	)

	content := mainView
	if m.state == statePrompt {
		content = overlay.PlaceOverlay(0, 0, m.textInputOverlay.Render(), mainView, true, true)
	} else if m.state == stateHelp {
		content = overlay.PlaceOverlay(0, 0, m.textOverlay.Render(), mainView, true, true)
	} else if m.state == stateConfirm {
		content = overlay.PlaceOverlay(0, 0, m.confirmationOverlay.Render(), mainView, true, true)
	}
	return content
}

// viewAssemblyTestHome builds a *home the same shape newComposerTestHome
// does, plus three tracked instances (trackedInstanceWithFakeTmux,
// composer_test.go) added to the list with the second selected - "three
// lanes and one selected", the brief's own fixture shape.
func viewAssemblyTestHome(t *testing.T) *home {
	t.Helper()
	m := newComposerTestHome(t)
	m.list.SetSize(70, 30)

	for i, title := range []string{"lane-a", "lane-b", "lane-c"} {
		inst := trackedInstanceWithFakeTmux(t, title, "$ idle")
		finalize := m.list.AddInstance(inst)
		finalize()
		_ = i
	}
	m.list.SetSelectedInstance(1)

	return m
}

// TestHomeView_MatchesLipglossAssembly_NoOverlay is item 3(a)'s no-overlay
// case.
func TestHomeView_MatchesLipglossAssembly_NoOverlay(t *testing.T) {
	m := viewAssemblyTestHome(t)
	require.Equal(t, oldViewContent(m), m.View().Content)
}

// TestHomeView_MatchesLipglossAssembly_NewLaneOverlay is item 3(a)'s
// stateNew-with-newLaneOverlay case (the "n" key's own three-step dialog,
// which floats over the PREVIEW pane, not the whole screen - a different
// code path through View() than the three whole-screen overlays below).
func TestHomeView_MatchesLipglossAssembly_NewLaneOverlay(t *testing.T) {
	m := viewAssemblyTestHome(t)
	m.state = stateNew
	m.newLaneOverlay = overlay.NewNewLaneOverlay("/tmp/sessions", "/tmp/forge-apps", nil, "")
	require.Equal(t, oldViewContent(m), m.View().Content)
}

// TestHomeView_MatchesLipglossAssembly_PromptOverlay is item 3(a)'s
// statePrompt case.
func TestHomeView_MatchesLipglossAssembly_PromptOverlay(t *testing.T) {
	m := viewAssemblyTestHome(t)
	m.state = statePrompt
	m.textInputOverlay = overlay.NewTextInputOverlay("Prompt", "")
	require.Equal(t, oldViewContent(m), m.View().Content)
}

// TestHomeView_MatchesLipglossAssembly_HelpOverlay is item 3(a)'s
// stateHelp case.
func TestHomeView_MatchesLipglossAssembly_HelpOverlay(t *testing.T) {
	m := viewAssemblyTestHome(t)
	m.state = stateHelp
	m.textOverlay = overlay.NewTextOverlay("help text")
	require.Equal(t, oldViewContent(m), m.View().Content)
}

// TestHomeView_MatchesLipglossAssembly_ConfirmOverlay is item 3(a)'s
// stateConfirm case.
func TestHomeView_MatchesLipglossAssembly_ConfirmOverlay(t *testing.T) {
	m := viewAssemblyTestHome(t)
	m.state = stateConfirm
	m.confirmationOverlay = overlay.NewConfirmationOverlay("Are you sure?")
	require.Equal(t, oldViewContent(m), m.View().Content)
}

// TestJoinHorizontalTop_FallsBackToLipglossOnRaggedInput is item 3(b): a
// left block whose lines are NOT all the same display width (the shape
// ui/list.go's own String() never produces today, per its own
// lipgloss.Place(l.width, l.height, ...) finish - but a future change that
// stopped guaranteeing it must not silently mis-pad) must produce the
// exact same bytes as real lipgloss.JoinHorizontal(lipgloss.Top, ...),
// never joinHorizontalTop's own fast-path arithmetic.
func TestJoinHorizontalTop_FallsBackToLipglossOnRaggedInput(t *testing.T) {
	ragged := "short\na much longer line\nmid"
	other := "right one\nright two"

	got := joinHorizontalTop(ragged, other)
	want := lipgloss.JoinHorizontal(lipgloss.Top, ragged, other)
	require.Equal(t, want, got, "a non-uniform-width left block must fall back to real lipgloss.JoinHorizontal, byte for byte")
}

// TestJoinVerticalLeft_FallsBackToLipglossOnRaggedInput is
// joinVerticalLeft's own counterpart to the fallback test above.
func TestJoinVerticalLeft_FallsBackToLipglossOnRaggedInput(t *testing.T) {
	ragged := "short\na much longer line\nmid"
	other := "middle row"
	third := "bottom"

	got := joinVerticalLeft(ragged, other, third)
	want := lipgloss.JoinVertical(lipgloss.Left, ragged, other, third)
	require.Equal(t, want, got, "a non-uniform-width block must fall back to real lipgloss.JoinVertical, byte for byte")
}

// TestPadTop1_MatchesLipgloss is a direct, non-golden proof of padTop1
// against lipgloss.NewStyle().PaddingTop(1).Render for both a uniform-width
// block (the shape every real caller passes) and a ragged one (padTop1
// needs no fallback - see its own doc comment for why the pad-to-widest
// step is correct either way).
func TestPadTop1_MatchesLipgloss(t *testing.T) {
	cases := []string{
		"single line, no ansi",
		"line one\nline two\nline three",
		"\x1b[31mred line\x1b[0m\nplain line",
		"",
		"short\na much longer line\nmid",
	}
	for _, c := range cases {
		want := lipgloss.NewStyle().PaddingTop(1).Render(c)
		got := padTop1(c)
		require.Equal(t, want, got, "input %q", c)
	}
}

// TestMeasureBlock_TabAndCRLFNormalisation proves measureBlock normalises
// tabs and CRLF the same way lipgloss/v2's own getLines does (get.go) -
// the Terminal tab's own raw pty capture can contain either.
func TestMeasureBlock_TabAndCRLFNormalisation(t *testing.T) {
	s := "a\tb\r\nc"
	mb := measureBlock(s)
	require.Equal(t, []string{"a    b", "c"}, mb.lines)
	require.False(t, strings.Contains(mb.lines[0], "\t"))
}
