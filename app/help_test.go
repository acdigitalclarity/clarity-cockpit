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
