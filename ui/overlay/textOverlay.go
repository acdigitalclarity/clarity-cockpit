package overlay

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// TextOverlay represents a text screen overlay
type TextOverlay struct {
	// Whether the overlay has been dismissed
	Dismissed bool
	// Callback function to be called when the overlay is dismissed. It
	// returns a tea.Cmd (rather than running its own side effects inline)
	// so a dismiss that hands off the terminal - attaching to a tmux
	// session - can be run through tea.Exec instead of blocking Update()
	// while bubbletea's own input reader is still live (board slice 9).
	OnDismiss func() tea.Cmd
	// Content to display in the overlay
	content string

	width int
}

// NewTextOverlay creates a new text screen overlay with the given title and content
func NewTextOverlay(content string) *TextOverlay {
	return &TextOverlay{
		Dismissed: false,
		content:   content,
	}
}

// HandleKeyPress processes a key press and updates the state.
// Returns true if the overlay should be closed, plus whatever tea.Cmd the
// OnDismiss callback returns (nil if there is no callback or it returns
// none) - the caller must return this Cmd from Update(), not discard it.
func (t *TextOverlay) HandleKeyPress(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	// Close on any key
	t.Dismissed = true
	var cmd tea.Cmd
	if t.OnDismiss != nil {
		cmd = t.OnDismiss()
	}
	return true, cmd
}

// Render renders the text overlay
func (t *TextOverlay) Render(opts ...WhitespaceOption) string {
	// Create styles
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Width(t.width)

	// Apply the border style and return
	return style.Render(t.content)
}

func (t *TextOverlay) SetWidth(width int) {
	t.width = width
}
