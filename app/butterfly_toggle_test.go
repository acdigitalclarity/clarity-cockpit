// Package app: this file tests slice 23B's own key-dispatch requirement -
// design refinement 4 ("b (shift-b, capital) toggles the butterfly live")
// wired through keys.KeyButterflyToggle. ui/tabbed_window_test.go already
// proves ToggleButterflyEnabled itself flips the state; this file proves a
// B keypress reaches it through handleKeyPress.
package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

// tabBarHasButterfly reports whether any of the four rest-cycle glyphs
// (ui/tabbed_window.go's own butterflyFrames: closed, half, open, half)
// appear in the tab bar's current render.
func tabBarHasButterfly(rendered string) bool {
	for _, glyph := range []string{"ʚ", "ɵ", "ɞ"} {
		if strings.Contains(rendered, glyph) {
			return true
		}
	}
	return false
}

// TestKeyButterflyToggle_FlipsAndFlipsBack proves a B keypress toggles the
// tab-bar butterfly off, and a second B keypress toggles it back on.
func TestKeyButterflyToggle_FlipsAndFlipsBack(t *testing.T) {
	h := homeWithMockedTerminal(t, false)
	require.True(t, tabBarHasButterfly(h.tabbedWindow.String()),
		"the butterfly must be visible before any B keypress")

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'B', Text: "B"})
	require.False(t, tabBarHasButterfly(h.tabbedWindow.String()),
		"one B keypress must hide the butterfly")

	pressGlobalKey(h, tea.KeyPressMsg{Code: 'B', Text: "B"})
	require.True(t, tabBarHasButterfly(h.tabbedWindow.String()),
		"a second B keypress must show the butterfly again")
}

// TestKeyButterflyToggle_NoopWhileComposerOpen proves B is a no-op while
// the composer (m key) is open - mirrors how C/v are fenced by the same
// early-return states in handleKeyPress. Opens the composer directly
// (composer_test.go's own TestComposerFlow_TypingAppendsToComposer shape)
// rather than through the m key, so this test does not also depend on a
// selected lane resolving a composerTarget.
func TestKeyButterflyToggle_NoopWhileComposerOpen(t *testing.T) {
	h := homeWithMockedTerminal(t, false)
	h.composer.Open("lane-a", false)
	h.state = stateMsg

	before := tabBarHasButterfly(h.tabbedWindow.String())
	h.handleKeyPress(tea.KeyPressMsg{Code: 'B', Text: "B"})
	require.Equal(t, before, tabBarHasButterfly(h.tabbedWindow.String()),
		"B must not toggle the butterfly while the composer is open")
	require.Equal(t, stateMsg, h.state, "B must not leave the composer state either")
}
