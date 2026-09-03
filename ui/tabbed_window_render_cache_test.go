// Package ui: slice 20b's own tests for item 2's TabbedWindow half
// (COCKPIT-CONTRACT.md brief - "ui/tabbed_window.go only if String() there
// needs the same treatment"): the profile named this method's own
// lipgloss.Style.Render/Place calls (tab borders, the content window) as
// the largest remaining idle-tick cost once List.String() was cached, so
// String()'s BASE assembly is memoised too - proven both ways: unchanged
// width/height/activeTab/content skips the base rebuild (counted through
// TabbedWindow.baseRenderCalls) even while the butterfly's own rest-beat
// animation keeps changing the drawn bytes, and a genuine base input
// change still rebuilds. Kept in its own file, the convention
// list_render_cache_test.go already follows.
package ui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// newRenderCacheTestWindow builds a TabbedWindow the same way
// tabbed_window_test.go's own tests do (mocked terminal deps, no real
// tmux), sized so the tab bar and window box both have real content.
func newRenderCacheTestWindow(t *testing.T) *TabbedWindow {
	t.Helper()
	w := NewTabbedWindow(NewSessionPane(), NewNeedsYouPane(),
		newTerminalPaneWithDeps(&MockPtyFactory{t: t, cmdExec: mockCmdExec("", false)}, mockCmdExec("", false)))
	w.SetSize(140, 40)
	return w
}

// TestTabbedWindowString_BaseCacheHit_ButterflyStillAnimates is item 4(a)'s
// TabbedWindow half: three ticks of the butterfly's own rest beat (holding
// frame 0 for butterflyRestFrameTicks[0]=3 ticks before flipping - see
// tickRestBeat's own doc comment) change String()'s drawn bytes without
// moving width, height, activeTab or the active pane's own content, so the
// base assembly must not run again.
func TestTabbedWindowString_BaseCacheHit_ButterflyStillAnimates(t *testing.T) {
	w := newRenderCacheTestWindow(t)

	first := w.String()
	require.Equal(t, 1, w.baseRenderCalls, "the first render must build the base once")

	for i := 0; i < 3; i++ {
		w.TickButterfly()
	}
	second := w.String()

	require.NotEqual(t, first, second, "the butterfly's own rest-beat frame must still change the drawn tab bar")
	require.Equal(t, 1, w.baseRenderCalls,
		"a butterfly-only change must not rebuild the base (tab borders, window box)")
}

// TestTabbedWindowString_SizeChange_RebuildsBase proves the cache-miss half:
// a genuine width/height change (the tab borders and window box both
// depend on it) must rebuild the base.
func TestTabbedWindowString_SizeChange_RebuildsBase(t *testing.T) {
	w := newRenderCacheTestWindow(t)

	before := w.String()
	require.Equal(t, 1, w.baseRenderCalls)

	w.SetSize(160, 44)
	after := w.String()

	require.NotEqual(t, before, after, "a size change must change the drawn tab bar and window box")
	require.Equal(t, 2, w.baseRenderCalls, "a size change must rebuild the base")
}

// TestTabbedWindowString_ActiveTabChange_RebuildsBase proves the same for
// the active tab moving - the window's own content switches pane entirely.
func TestTabbedWindowString_ActiveTabChange_RebuildsBase(t *testing.T) {
	w := newRenderCacheTestWindow(t)

	before := w.String()
	require.Equal(t, 1, w.baseRenderCalls)

	w.SetActiveTab(NeedsYouTab)
	after := w.String()

	require.NotEqual(t, before, after, "switching the active tab must change the drawn window content")
	require.Equal(t, 2, w.baseRenderCalls, "an active-tab change must rebuild the base")
}
