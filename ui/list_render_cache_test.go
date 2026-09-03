// Package ui: slice 20b's own tests for item 2 (COCKPIT-CONTRACT.md brief)
// - List.String() memoised against a cheap fingerprint of its own inputs,
// proven both ways: an unchanged fingerprint returns the cached bytes
// without walking a single row (counted through InstanceRenderer.renderCalls
// and List.externalRenderCalls, the row renderer's own test hooks), and a
// changed state word or selection still re-renders. Kept in its own file,
// the convention list_liveness_test.go/list_frontdoor5_test.go already
// follow.
package ui

import (
	"claude-squad/session/clarity"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestListString_CacheHit_NoRowRendererCalls is item 4(a)'s own cache-hit
// half: two consecutive String() calls with nothing changed must call the
// row renderer (InstanceRenderer.Render and renderExternalRow) exactly
// once each, not twice - the second call is a pure cache read.
func TestListString_CacheHit_NoRowRendererCalls(t *testing.T) {
	l := newTestList()
	l.SetSize(frontdoor5ListWidth164, 45)
	l.AddInstance(frontdoor5Instance(t, "cache-row", "ta", "", 10, clarity.StateWorking, frontdoor5Time(9, 0)))
	l.SetExternal([]clarity.ExternalLane{
		{Name: "cache-ext", State: clarity.StateIdle, StateOK: true, Alive: true, LastTurn: frontdoor5Time(9, 1)},
	})

	first := l.String()
	require.Equal(t, 1, l.renderer.renderCalls, "the first render must call the tracked-row renderer once")
	require.Equal(t, 1, l.externalRenderCalls, "the first render must call the external-row renderer once")

	second := l.String()
	require.Equal(t, first, second, "an unchanged fingerprint must return byte-identical output")
	require.Equal(t, 1, l.renderer.renderCalls,
		"a second String() call with nothing changed must not call the tracked-row renderer again")
	require.Equal(t, 1, l.externalRenderCalls,
		"a second String() call with nothing changed must not call the external-row renderer again")
}

// TestListString_StateWordChange_ReRenders is item 4(a)'s own cache-miss
// half: a tracked row's state word changing between two calls must produce
// different output and a fresh row-renderer call, never the stale cached
// bytes.
func TestListString_StateWordChange_ReRenders(t *testing.T) {
	l := newTestList()
	l.SetSize(frontdoor5ListWidth164, 45)
	inst := frontdoor5Instance(t, "state-change-row", "ta", "", 10, clarity.StateWorking, frontdoor5Time(9, 0))
	l.AddInstance(inst)

	before := l.String()
	require.Equal(t, 1, l.renderer.renderCalls)

	inst.SetLaneState(clarity.StateWaitingYou, frontdoor5Time(9, 5), true)
	after := l.String()

	require.NotEqual(t, before, after, "a changed state word must change the rendered row")
	require.Equal(t, 2, l.renderer.renderCalls, "a changed state word must re-render, not read the cache")
}

// TestListString_SelectionChange_ReRenders proves the same for a moved
// selection (the row highlight band) - two rows, Down moves the cursor
// from the first to the second with every OTHER field held fixed.
func TestListString_SelectionChange_ReRenders(t *testing.T) {
	l := newTestList()
	l.SetSize(frontdoor5ListWidth164, 45)
	l.AddInstance(frontdoor5Instance(t, "sel-row-a", "ta", "", 10, clarity.StateWorking, frontdoor5Time(9, 0)))
	l.AddInstance(frontdoor5Instance(t, "sel-row-b", "ta", "", 10, clarity.StateWorking, frontdoor5Time(9, 1)))

	before := l.String()
	renders := l.renderer.renderCalls
	require.Equal(t, 2, renders, "two tracked rows must each render once")

	l.Down()
	after := l.String()

	require.NotEqual(t, before, after, "a moved selection must change the drawn highlight band")
	require.Equal(t, 2*renders, l.renderer.renderCalls,
		"a selection change must re-render both rows, never read the stale cache")
}
