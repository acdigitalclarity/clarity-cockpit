package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// StatusBox renders a single line of ephemeral, non-error status text -
// used for the m-key message delivery result (the line that landed in the
// target's pane, or the UNCONSTRUCTED line for an external row). Mirrors
// ErrBox's mechanics exactly (SetSize/Clear/String, caller-driven auto-hide
// after a few seconds) but in a neutral style, since "the message landed"
// is not an error and should not read as one.
type StatusBox struct {
	height, width int
	text          string
}

var statusStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
	Light: "#1a7a3c",
	Dark:  "#51bd73",
})

func NewStatusBox() *StatusBox {
	return &StatusBox{}
}

func (s *StatusBox) SetText(text string) {
	s.text = text
}

func (s *StatusBox) Clear() {
	s.text = ""
}

func (s *StatusBox) SetSize(width, height int) {
	s.width = width
	s.height = height
}

func (s *StatusBox) String() string {
	text := s.text
	if text != "" {
		lines := strings.Split(text, "\n")
		text = strings.Join(lines, "//")
		if runewidth.StringWidth(text) > s.width-3 && s.width-3 >= 0 {
			text = runewidth.Truncate(text, s.width-3, "...")
		}
	}
	return lipgloss.Place(s.width, s.height, lipgloss.Center, lipgloss.Center, statusStyle.Render(text))
}
