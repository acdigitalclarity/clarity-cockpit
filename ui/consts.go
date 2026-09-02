package ui

import (
	"claude-squad/ui/splash"

	"charm.land/lipgloss/v2"
)

// FallBackText is the placeholder drawn in an empty preview or terminal pane:
// the product wordmark in the splash's own block font.
var FallBackText = lipgloss.JoinVertical(lipgloss.Center, splash.Wordmark("CLARITY"), "", splash.Wordmark("WORKSPACE"))
