package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"github.com/mattn/go-runewidth"
)

type ErrBox struct {
	height, width int
	err           error
}

var errStyle = lipgloss.NewStyle().Foreground(compat.AdaptiveColor{
	Light: lipgloss.Color("#FF0000"),
	Dark:  lipgloss.Color("#FF0000"),
})

func NewErrBox() *ErrBox {
	return &ErrBox{}
}

func (e *ErrBox) SetError(err error) {
	e.err = err
}

func (e *ErrBox) Clear() {
	e.err = nil
}

func (e *ErrBox) SetSize(width, height int) {
	e.width = width
	e.height = height
}

func (e *ErrBox) String() string {
	var err string
	if e.err != nil {
		err = e.err.Error()
		lines := strings.Split(err, "\n")
		err = strings.Join(lines, "//")
		if runewidth.StringWidth(err) > e.width-3 && e.width-3 >= 0 {
			err = runewidth.Truncate(err, e.width-3, "...")
		}
	}
	return lipgloss.Place(e.width, e.height, lipgloss.Center, lipgloss.Center, errStyle.Render(err))
}
