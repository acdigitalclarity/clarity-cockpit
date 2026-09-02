package splash

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

// -- RenderFrame: frame 0, mid-entrance, peak and idle, at both widths ----

func TestRenderFrame_Frame0_HasGrid(t *testing.T) {
	for _, cols := range []int{120, 80} {
		out := RenderFrame(cols, 0, 0, -1, 3, 2)
		require.NotEmpty(t, out)
		require.True(t, strings.ContainsAny(out, "─│┼"), "cols=%d: the perspective grid is drawn on frame 0", cols)
		require.True(t, strings.Contains(out, "▀"), "cols=%d: the mark's silhouette (ghost or bar) is drawn on frame 0", cols)
	}
}

func TestRenderFrame_MidEntrance_HasWordmarkAndGrid(t *testing.T) {
	for _, cols := range []int{120, 80} {
		out := RenderFrame(cols, 0, 24, -1, 3, 2) // 24 of 48 - mid-entrance
		require.NotEmpty(t, out)
		require.True(t, strings.ContainsAny(out, "─│┼"), "cols=%d: grid present mid-entrance", cols)
		require.Greater(t, strings.Count(out, "█"), 0, "cols=%d: the wordmark has started sliding in by frame 24", cols)
	}
}

func TestRenderFrame_PeakFrame_WordmarkAndCountersFullyShown(t *testing.T) {
	for _, cols := range []int{120, 80} {
		peak := RenderFrame(cols, 0, -1, -1, 3, 2) // resting: entrance<0, idle<0
		frame0 := RenderFrame(cols, 0, 0, -1, 3, 2)
		require.NotEmpty(t, peak)
		require.Greater(t, strings.Count(peak, "█"), strings.Count(frame0, "█"),
			"cols=%d: the peak frame shows more of the wordmark/counters lit than frame 0 (wordProgress=0 there)", cols)
	}
}

func TestRenderFrame_IdleFrame_HasWordmark(t *testing.T) {
	for _, cols := range []int{120, 80} {
		out := RenderFrame(cols, 0, -1, 10, 3, 2)
		require.NotEmpty(t, out)
		require.True(t, strings.ContainsAny(out, "─│┼"), "cols=%d: grid present in the idle loop", cols)
		require.Greater(t, strings.Count(out, "█"), 0, "cols=%d: the wordmark is fully revealed by the idle loop", cols)
	}
}

// -- bar heights differ between trough and peak ---------------------------
// (columnEnvelope is the exact quantity drawMarkEqualizer rounds into a bar
// length - see equalizer.go - so testing it directly is a precise,
// deterministic proxy for "the bar heights differ", not a fragile ANSI
// string comparison.)

func TestColumnEnvelope_TroughBelowPeak(t *testing.T) {
	// columnEnvelope carries its own per-column attack-phase jitter (up to
	// +/-5% of a beat, see equalizer.go), so the nominal crest position
	// (cyclePos == attackFrac) isn't exactly on-crest for every column -
	// scan a small window around it and take the observed maximum, the
	// same way a human eye would read "the peak" off a running animation
	// rather than assuming a single idealised frame.
	fpb := framesPerBeat()
	const col = 0
	peak := 0.0
	for f := fpb * 0.15; f <= fpb*0.30; f += fpb * 0.01 {
		if v := columnEnvelope(f, col); v > peak {
			peak = v
		}
	}
	trough := columnEnvelope(fpb*0.95, col) // deep in the decay tail, near the column's own floor

	require.Greater(t, peak, 0.7, "a bar reaches near-full height around the beat's crest")
	require.Greater(t, peak, trough, "bar height falls after the crest, between beats")
	require.Less(t, trough, peak-0.15, "the trough sits meaningfully below the peak, not just jitter-close")
}

func TestRenderFrame_PeakIdleFrameDiffersFromTroughIdleFrame(t *testing.T) {
	// idleFrame 3 -> worldFrame 51 -> cyclePos ~0.9 of a beat: a genuine
	// trough. The resting/peak render (env=1.0 everywhere) must differ from
	// it, at both widths.
	for _, cols := range []int{120, 80} {
		trough := RenderFrame(cols, 0, -1, 3, 3, 2)
		peak := RenderFrame(cols, 0, -1, -1, 3, 2)
		require.NotEqual(t, trough, peak, "cols=%d: a trough idle frame renders differently from the peak", cols)
	}
}

// -- the "gaps closing on the downbeat" tuning -----------------------------

func TestGlobalBeatEnvelope_ClosesOnCrestOpensOnTrough(t *testing.T) {
	fpb := framesPerBeat()
	crest := fpb * 0.22   // attackFrac's own boundary
	trough := fpb * 0.999 // just before the cycle wraps to the next beat

	require.InDelta(t, 1.0, globalBeatEnvelope(crest), 0.01,
		"gap columns close solid on the downbeat, matching the bar columns' own crest")
	require.Less(t, globalBeatEnvelope(trough), 0.05,
		"gap columns go fully dark between beats, so the mark reads as distinct bars")
}

// -- the "mark scaled up to ~24 rows tall at 120 columns" tuning -----------

func TestLayoutFor_MarkReaches24RowsAt120_WhenHeightAllows(t *testing.T) {
	unconstrained := layoutFor(120, 0)
	require.Equal(t, 24, unconstrained.markRows, "the tuned target: ~24 rows tall at 120 columns")
	require.Equal(t, 44, unconstrained.markCols)

	ample := layoutFor(120, 66) // markRows(24)+gridRows(24)+overhead(18) == 66 exactly
	require.Equal(t, 24, ample.markRows)
	require.Equal(t, 24, ample.gridRows)
}

func TestLayoutFor_80ColsKeepsMockupSize(t *testing.T) {
	lo := layoutFor(80, 0)
	require.Equal(t, 9, lo.markRows, "80-column layout stays as in the mock-up - the tuning is 120-col only")
	require.Equal(t, 16, lo.gridRows)
	require.Equal(t, 28, lo.markCols)
	require.False(t, lo.big)
}

func TestLayoutFor_AdaptsToATightWindow(t *testing.T) {
	// The tmux proof runs at 120x36 - too short to hold a 24-row mark, a
	// 24-row grid AND the fixed 18-row word/counter/maker block (48+18=66
	// needed, only 36 available). layoutFor must still produce a layout
	// that fits: the grid shrinks to its minimum first, the mark takes
	// what height remains, and every element the tmux proof checks for
	// (grid, bars, then the wordmark) stays on screen.
	lo := layoutFor(120, 36)
	require.True(t, lo.big)
	require.Equal(t, 4, lo.gridRows, "grid shrinks to its floor first")
	require.Equal(t, 14, lo.markRows, "mark takes what's left of the 36-row budget")
	require.LessOrEqual(t, lo.markRows+lo.gridRows+2*lo.glyphH+4, 36,
		"the full stack through the maker row fits inside the given height")
}

// -- Model: tick progression, hand-off, sizing -----------------------------

func TestModel_TicksThroughEntranceIntoIdle(t *testing.T) {
	m := &Model{}
	for i := 0; i < EntranceFrames-1; i++ {
		cmd := m.Update(TickMsg{})
		require.NotNil(t, cmd, "still self-rescheduling during the entrance")
		require.False(t, m.inIdle, "frame %d: still in the entrance", i+1)
	}
	// One more tick crosses into idle.
	cmd := m.Update(TickMsg{})
	require.NotNil(t, cmd)
	require.True(t, m.inIdle)
	require.False(t, m.Done())
}

func TestModel_HandleKey_MarksDone(t *testing.T) {
	m := &Model{}
	require.False(t, m.Done())
	m.HandleKey()
	require.True(t, m.Done())
	// A tick after hand-off returns no further command - no more scheduling.
	require.Nil(t, m.Update(TickMsg{}))
}

func TestModel_AutoHandoffAfterIdleWindow(t *testing.T) {
	m := &Model{inIdle: true, idleSince: time.Now().Add(-(IdleHandoffAfter + time.Second))}
	cmd := m.Update(TickMsg{})
	require.Nil(t, cmd, "no further ticks are scheduled once the idle window has elapsed")
	require.True(t, m.Done())
}

func TestModel_SetSize_FeedsRenderFrame(t *testing.T) {
	m := New()
	m.SetSize(80, 30)
	out := m.View()
	require.NotEmpty(t, out)
	// At width 80 the small font/mark are used - confirm indirectly via
	// layoutFor agreeing with what View() actually rendered at.
	lo := layoutFor(80, 30)
	require.False(t, lo.big)
}

func TestModel_IgnoresNonTickMessages(t *testing.T) {
	m := &Model{}
	before := *m
	cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	require.Nil(t, cmd)
	require.Equal(t, before, *m, "a non-tick message never mutates the model")
}
