package ui

import (
	"claude-squad/session/clarity"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestTabbedWindow_SessionIsFirstAndDefaultTab is the brief's own named
// requirement: the tabbed window's tabs become Session · Needs you ·
// Terminal, Session first and the default (design/cockpit-pane/
// DECISIONS.md slices 3 and 5).
func TestTabbedWindow_SessionIsFirstAndDefaultTab(t *testing.T) {
	w := NewTabbedWindow(NewSessionPane(), NewNeedsYouPane(), NewTerminalPane())
	require.Equal(t, 0, w.GetActiveTab(), "Session must be the default tab")
	require.True(t, w.IsInSessionTab())
	require.Equal(t, SessionTab, 0)
	require.Equal(t, NeedsYouTab, 1)
	require.Equal(t, TerminalTab, 2)
}

// TestTabbedWindow_ScrollKeys_DispatchToSessionPane proves shift+up/down's
// own wiring end to end: TabbedWindow.ScrollUp/ScrollDown, while the
// Session tab is active, actually move the Session pane's own viewport
// (the same keys.KeyShiftUp/KeyShiftDown app.go already binds for
// Preview/Needs-you/Terminal - this slice reuses them, no new binding).
func TestTabbedWindow_ScrollKeys_DispatchToSessionPane(t *testing.T) {
	w := NewTabbedWindow(NewSessionPane(), NewNeedsYouPane(), NewTerminalPane())
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

// TestTabbedWindow_ScrollKeys_DispatchToNeedsYouPane is the same end-to-end
// wiring proof for the Needs-you tab (slice 5's "scroll with the same keys
// as Session").
func TestTabbedWindow_ScrollKeys_DispatchToNeedsYouPane(t *testing.T) {
	w := NewTabbedWindow(NewSessionPane(), NewNeedsYouPane(), NewTerminalPane())
	w.SetSize(30, 20)
	w.SetActiveTab(NeedsYouTab)
	require.True(t, w.IsInNeedsYouTab())

	w.SetNeedsYouInfo(&NeedsYouInfo{
		Item:        clarity.FeedItem{Rank: 1, Title: "t"},
		Lane:        "lane-a",
		Explanation: []clarity.BoardSection{{Text: strings.Repeat("word ", 200)}},
		Options:     []clarity.BoardOption{{Text: "ok"}},
	})

	before := w.String()
	for i := 0; i < 40; i++ {
		w.ScrollDown()
	}
	after := w.String()
	require.NotEqual(t, before, after, "ScrollDown on the Needs-you tab must change what the pane shows")
}
