// Package app: this file tests slice 15 of design/cockpit-pane/DECISIONS.md
// (the Terminal tab is always a shell) - the general help screen must name
// the rule, since it is the owner's own surprise ("terminal i thought would
// just be terminal rather than this session") that the general help screen
// is the natural place to head off.
package app

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHelpGeneral_NamesTerminalTabIsAlwaysAShell is the brief's own "help
// text names the rule" case: the general help screen states that the
// Terminal tab is always a shell in the lane's own folder, and that Enter
// still attaches to the lane's Claude session rather than that shell.
func TestHelpGeneral_NamesTerminalTabIsAlwaysAShell(t *testing.T) {
	content := helpTypeGeneral{}.toContent()

	require.Contains(t, content, "Terminal tab is always a shell",
		"the general help screen must name the slice 15 rule")
	require.Contains(t, content, "attaches to its Claude session",
		"the general help screen must say Enter still reaches the Claude session, not the shell")
	require.True(t, strings.Contains(content, "own folder"),
		"the general help screen must say the shell opens in the lane's own folder")
}

// TestHelpGeneral_ListsSessionCopyKeys is slice 22 PART B's own "the help
// lists the three keys" requirement: c, C and v must all appear in the
// general help screen, each with its own description.
func TestHelpGeneral_ListsSessionCopyKeys(t *testing.T) {
	content := helpTypeGeneral{}.toContent()

	require.Contains(t, content, "last turn", "c's own help line must name the Session tab's last-turn copy")
	require.Contains(t, content, "whole visible transcript", "C's own help line must name the whole-tail copy")
	require.Contains(t, content, "Turn picker", "v's own help line must name the turn picker")
}

// TestHelpGeneral_NamesButterflyToggle is design refinement 4's own "help
// says so" requirement: capital B and what it does must appear in the
// general help screen.
func TestHelpGeneral_NamesButterflyToggle(t *testing.T) {
	content := helpTypeGeneral{}.toContent()

	require.Contains(t, content, "Toggle the tab-bar butterfly",
		"B's own help line must name the butterfly toggle")
}

// TestHelpGeneral_NamesAnsweredRowLeavesAtOnce is board #295's own "help
// says so" requirement: the general help screen states that an answered
// row's comment closes the board issue and leaves the list at once, not
// just that y posts a comment.
func TestHelpGeneral_NamesAnsweredRowLeavesAtOnce(t *testing.T) {
	content := helpTypeGeneral{}.toContent()

	require.Contains(t, content, "the board issue closes, and the row leaves the list at once",
		"the general help screen must name the answered-row-leaves-at-once rule")
}
