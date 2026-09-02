package ui

import (
	"claude-squad/session/clarity"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestTabbedWindow_SessionIsFirstAndDefaultTab is the brief's own named
// requirement: the tabbed window's tabs become Session · Diff · Terminal,
// Session first and the default (design/cockpit-pane/DECISIONS.md slice 3).
func TestTabbedWindow_SessionIsFirstAndDefaultTab(t *testing.T) {
	w := NewTabbedWindow(NewSessionPane(), NewDiffPane(), NewTerminalPane())
	require.Equal(t, 0, w.GetActiveTab(), "Session must be the default tab")
	require.True(t, w.IsInSessionTab())
	require.Equal(t, SessionTab, 0)
	require.Equal(t, DiffTab, 1)
	require.Equal(t, TerminalTab, 2)
}

// TestTabbedWindow_ScrollKeys_DispatchToSessionPane proves shift+up/down's
// own wiring end to end: TabbedWindow.ScrollUp/ScrollDown, while the
// Session tab is active, actually move the Session pane's own viewport
// (the same keys.KeyShiftUp/KeyShiftDown app.go already binds for
// Preview/Diff/Terminal - this slice reuses them, no new binding).
func TestTabbedWindow_ScrollKeys_DispatchToSessionPane(t *testing.T) {
	w := NewTabbedWindow(NewSessionPane(), NewDiffPane(), NewTerminalPane())
	w.SetSize(200, 40)
	require.True(t, w.IsInSessionTab())

	base := time.Date(2026, 9, 2, 18, 0, 0, 0, time.Local)
	var turns []clarity.Turn
	for i := 0; i < 60; i++ {
		turns = append(turns, clarity.Turn{Kind: clarity.TurnOwner, At: base.Add(time.Duration(i) * time.Minute), Text: "turn body"})
	}
	w.SetSessionInfo(&SessionInfo{Lane: "x", Tail: clarity.LaneTail{Turns: turns, State: clarity.StateIdle}})

	before := w.String()
	for i := 0; i < 200; i++ {
		w.ScrollUp()
	}
	after := w.String()
	require.NotEqual(t, before, after, "ScrollUp on the Session tab must change what the pane shows")
	require.True(t, strings.Contains(w.String(), "turn body"))
}
