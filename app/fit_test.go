package app

import (
	"claude-squad/session"
	"claude-squad/session/clarity"
	"claude-squad/ui"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// TestView_ListStartsAtOneSpaceMargin is the MARGIN defect's own test: at
// 164 wide the list's own " Instances " title line already carries exactly
// one leading space, but View()'s outer lipgloss.JoinVertical(lipgloss.
// Center, ...) padded listAndPreview (156 wide - TabbedWindow renders 8
// columns narrower than the 99 it was given) up to the taller rows'
// full 164, centering the shortfall and pushing the whole list 4 columns
// right (column 5, close to the brief's observed "column 6" at the real
// fleet's own preview content). Left-aligning that join instead of
// centering it removes the manufactured indent at every width, leaving
// only the one space already baked into " Instances ".
func TestView_ListStartsAtOneSpaceMargin(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{120, 36}, {164, 45}} {
		t.Run(fmt.Sprintf("%dx%d", sz.w, sz.h), func(t *testing.T) {
			h := newHome(context.Background(), "true", false, true)
			h.updateHandleWindowSizeEvent(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
			v := h.View()
			var firstNonBlank string
			for _, line := range strings.Split(v.Content, "\n") {
				if strings.TrimSpace(ansi.Strip(line)) != "" {
					firstNonBlank = line
					break
				}
			}
			require.NotEmpty(t, firstNonBlank, "View() must render at least one non-blank line")
			plain := ansi.Strip(firstNonBlank)
			require.True(t, strings.HasPrefix(plain, " Instances"),
				"first non-blank line must lead with the list's own one-space margin, got %q", plain)
			require.False(t, strings.HasPrefix(plain, "  Instances"),
				"exactly one leading space, not more, got %q", plain)
		})
	}
}

// TestView_NoLineExceedsWidth is the brief's own named test: a View() at
// 164x45 and 120x36 (plus the collapse-threshold case at 80x24 and a wide
// case at 200x55, the four PROOF capture sizes) whose every rendered line
// has ansi.StringWidth <= the terminal's own width. Before the OVERFLOW
// fix, an unbounded "Needs you"/external-lane row made List.String()'s
// actual width exceed the column app.go had given it, which
// lipgloss.JoinHorizontal then carried straight through to the whole
// screen (see ui/fit_test.go for the row-level reproduction of the same
// defect).
func TestView_NoLineExceedsWidth(t *testing.T) {
	sizes := []struct{ w, h int }{
		{80, 24},
		{120, 36},
		{164, 45},
		{200, 55},
	}
	for _, sz := range sizes {
		t.Run(fmt.Sprintf("%dx%d", sz.w, sz.h), func(t *testing.T) {
			h := newHome(context.Background(), "true", false, true)
			h.updateHandleWindowSizeEvent(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
			v := h.View()
			for i, line := range strings.Split(v.Content, "\n") {
				require.LessOrEqualf(t, ansi.StringWidth(line), sz.w,
					"line %d exceeds terminal width %d: %q", i, sz.w, line)
			}
		})
	}
}

// TestView_HeightNeverExceedsTerminal is the OVERFLOW defect's vertical
// half: the whole screen (list+preview, menu, footer) must render exactly
// within the given height at 24, 36, 45 and 60 rows (the brief's named
// heights) - before the fix, View() rendered height+1 lines at every size
// tested, because the arithmetic that splits msg.Height between content and
// menu never accounted for View()'s own PaddingTop(1) on the list/preview
// block.
func TestView_HeightNeverExceedsTerminal(t *testing.T) {
	for _, hgt := range []int{24, 36, 45, 60} {
		t.Run(fmt.Sprintf("h=%d", hgt), func(t *testing.T) {
			h := newHome(context.Background(), "true", false, true)
			h.updateHandleWindowSizeEvent(tea.WindowSizeMsg{Width: 120, Height: hgt})
			v := h.View()
			lines := strings.Split(v.Content, "\n")
			require.LessOrEqualf(t, len(lines), hgt,
				"rendered %d lines, terminal is only %d rows tall", len(lines), hgt)
		})
	}
}

// TestView_PreviewBottomBorderVisible checks the same four heights for the
// concrete symptom the brief named: the preview/diff pane's own bottom
// border row must appear somewhere in the rendered output, not be pushed
// past the terminal height the way the extra overflow row did before the
// fix.
func TestView_PreviewBottomBorderVisible(t *testing.T) {
	for _, hgt := range []int{24, 36, 45, 60} {
		t.Run(fmt.Sprintf("h=%d", hgt), func(t *testing.T) {
			h := newHome(context.Background(), "true", false, true)
			h.updateHandleWindowSizeEvent(tea.WindowSizeMsg{Width: 120, Height: hgt})
			require.Contains(t, h.tabbedWindow.String(), "└",
				"the preview pane's bottom-left border corner must render within its given height")
		})
	}
}

// TestUpdateHandleWindowSizeEvent_CollapsesBelowThreshold documents the
// OVERFLOW fix's stated decision: below collapsePreviewBelowWidth columns
// the preview/diff pane is collapsed (zero width) rather than fought over,
// and the list takes the full terminal width instead.
func TestUpdateHandleWindowSizeEvent_CollapsesBelowThreshold(t *testing.T) {
	h := newHome(context.Background(), "true", false, true)
	h.updateHandleWindowSizeEvent(tea.WindowSizeMsg{Width: 80, Height: 24})
	require.Empty(t, h.tabbedWindow.String(), "below the collapse threshold the preview/diff pane renders nothing")

	h.updateHandleWindowSizeEvent(tea.WindowSizeMsg{Width: 120, Height: 36})
	require.NotEmpty(t, h.tabbedWindow.String(), "at/above the collapse threshold the preview/diff pane renders")
}

// TestFeedTick_ComputesContextFillForPausedInstances is the OWN ROW
// defect's "ctx n/a" root cause, exercised at the app level:
// tickUpdateMetadataCmd only ever computes context fill for
// snapshotActiveInstances() (Started and not Paused), so a Paused tracked
// instance's gauge was stuck at its zero value forever. feedTickMsg's
// handler now closes that gap the same way DiscoverExternalLanes already
// derives every external lane's fill on this same tick, regardless of any
// "paused" concept - which does not exist for an external lane at all.
func TestFeedTick_ComputesContextFillForPausedInstances(t *testing.T) {
	root := t.TempDir()
	t.Setenv(clarity.ClaudeProjectsRootEnvVar, root)
	t.Setenv(clarity.FeedQueuePathEnvVar, filepath.Join(root, "no-such-queue.json"))

	lanePath := t.TempDir()
	dir := filepath.Join(root, clarity.EncodeProjectDir(lanePath))
	require.NoError(t, os.MkdirAll(dir, 0755))
	transcript := filepath.Join(dir, "t.jsonl")
	line := `{"message":{"model":"claude-sonnet-5","usage":{"input_tokens":100000,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}` + "\n"
	require.NoError(t, os.WriteFile(transcript, []byte(line), 0644))

	inst, err := session.NewInstance(session.InstanceOptions{
		Title:      "paused-lane",
		Path:       lanePath,
		Program:    "echo",
		NoWorktree: true,
	})
	require.NoError(t, err)
	inst.SetStatus(session.Paused)

	_, ok := inst.GetContextFill()
	require.False(t, ok, "no fill cached yet")

	sp := spinner.New()
	h := &home{
		ctx:  context.Background(),
		list: ui.NewList(&sp, false),
	}
	h.list.AddInstance(inst)

	_, cmd := h.Update(feedTickMsg{})
	require.NotNil(t, cmd, "feedTickMsg self-reschedules")

	pct, ok := inst.GetContextFill()
	require.True(t, ok, "a Paused tracked instance's context fill must be derivable from its own transcript, not stuck at n/a")
	require.Equal(t, 50, pct)
}

// TestFeedTick_ComputesLaneStateForTrackedInstance is item 1's own app-level
// test: the state word every lane row now carries comes from
// clarity.ReadLaneTail, computed on this same feed tick for every tracked
// instance (not just Paused ones, since a Running instance's row needs the
// word just as much) - GetLaneState must report it after one tick.
func TestFeedTick_ComputesLaneStateForTrackedInstance(t *testing.T) {
	root := t.TempDir()
	t.Setenv(clarity.ClaudeProjectsRootEnvVar, root)
	t.Setenv(clarity.FeedQueuePathEnvVar, filepath.Join(root, "no-such-queue.json"))

	lanePath := t.TempDir()
	dir := filepath.Join(root, clarity.EncodeProjectDir(lanePath))
	require.NoError(t, os.MkdirAll(dir, 0755))
	transcript := filepath.Join(dir, "t.jsonl")
	ts := time.Now().UTC().Format(time.RFC3339)
	line := `{"type":"system","subtype":"turn_duration","timestamp":"` + ts +
		`","durationMs":1000,"messageCount":3,"pendingBackgroundAgentCount":2}` + "\n"
	require.NoError(t, os.WriteFile(transcript, []byte(line), 0644))

	inst, err := session.NewInstance(session.InstanceOptions{
		Title:   "working-lane",
		Path:    lanePath,
		Program: "echo",
	})
	require.NoError(t, err)

	_, _, ok := inst.GetLaneState()
	require.False(t, ok, "no lane state cached yet")

	sp := spinner.New()
	h := &home{
		ctx:  context.Background(),
		list: ui.NewList(&sp, false),
	}
	h.list.AddInstance(inst)

	_, cmd := h.Update(feedTickMsg{})
	require.NotNil(t, cmd, "feedTickMsg self-reschedules")

	state, _, ok := inst.GetLaneState()
	require.True(t, ok, "a tracked instance's lane state must be derivable from its own transcript after one feed tick")
	require.Equal(t, clarity.StateWorking, state)
}
