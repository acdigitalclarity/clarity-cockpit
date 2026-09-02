package ui

import (
	"claude-squad/keys"
	"strings"

	"claude-squad/session"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"github.com/charmbracelet/x/ansi"
)

var keyStyle = lipgloss.NewStyle().Foreground(compat.AdaptiveColor{
	Light: lipgloss.Color("#655F5F"),
	Dark:  lipgloss.Color("#7F7A7A"),
})

var descStyle = lipgloss.NewStyle().Foreground(compat.AdaptiveColor{
	Light: lipgloss.Color("#7A7474"),
	Dark:  lipgloss.Color("#9C9494"),
})

var sepStyle = lipgloss.NewStyle().Foreground(compat.AdaptiveColor{
	Light: lipgloss.Color("#DDDADA"),
	Dark:  lipgloss.Color("#3C3C3C"),
})

var actionGroupStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("99"))

var separator = " • "
var verticalSeparator = " │ "

var menuStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("205"))

// MenuState represents different states the menu can be in
type MenuState int

const (
	StateDefault MenuState = iota
	StateEmpty
	StateNewInstance
	StatePrompt
	// StateMsg is the composer's own menu state (board #280, slice 5b,
	// DEFECT 3) - distinct from StatePrompt (upstream's "enter prompt"
	// instance-start overlay), which the composer previously borrowed and
	// so wrongly showed "enter submit name" while a message was being
	// typed.
	StateMsg
)

type Menu struct {
	options       []keys.KeyName
	height, width int
	state         MenuState
	instance      *session.Instance
	activeTab     int

	// keyDown is the key which is pressed. The default is -1.
	keyDown keys.KeyName
}

var defaultMenuOptions = []keys.KeyName{keys.KeyNew, keys.KeyPrompt, keys.KeyMsg, keys.KeyHelp, keys.KeyQuit}
var newInstanceMenuOptions = []keys.KeyName{keys.KeySubmitName}
var promptMenuOptions = []keys.KeyName{keys.KeySubmitName}

func NewMenu() *Menu {
	return &Menu{
		options:   defaultMenuOptions,
		state:     StateEmpty,
		activeTab: 0,
		keyDown:   -1,
	}
}

func (m *Menu) Keydown(name keys.KeyName) {
	m.keyDown = name
}

func (m *Menu) ClearKeydown() {
	m.keyDown = -1
}

// SetState updates the menu state and options accordingly
func (m *Menu) SetState(state MenuState) {
	m.state = state
	m.updateOptions()
}

// SetInstance updates the current instance and refreshes menu options
func (m *Menu) SetInstance(instance *session.Instance) {
	m.instance = instance
	// Only change the state if we're not in a special state (NewInstance,
	// Prompt or the composer's own Msg) - a feed tick's instanceChanged()
	// must never kick the footer out of "enter send · esc cancel" while a
	// message is being typed.
	if m.state != StateNewInstance && m.state != StatePrompt && m.state != StateMsg {
		if m.instance != nil {
			m.state = StateDefault
		} else {
			m.state = StateEmpty
		}
	}
	m.updateOptions()
}

// SetActiveTab updates the currently active tab
func (m *Menu) SetActiveTab(tab int) {
	m.activeTab = tab
	m.updateOptions()
}

// updateOptions updates the menu options based on current state and instance
func (m *Menu) updateOptions() {
	switch m.state {
	case StateEmpty:
		m.options = defaultMenuOptions
	case StateDefault:
		if m.instance != nil {
			// When there is an instance, show that instance's options
			m.addInstanceOptions()
		} else {
			// When there is no instance, show the empty state
			m.options = defaultMenuOptions
		}
	case StateNewInstance:
		m.options = newInstanceMenuOptions
	case StatePrompt:
		m.options = promptMenuOptions
	case StateMsg:
		// String() short-circuits StateMsg before m.options is ever read
		// (the composer's own foot text, not the key-binding groups below).
		m.options = nil
	}
}

func (m *Menu) addInstanceOptions() {
	// Loading instances only get minimal options
	if m.instance != nil && m.instance.Status == session.Loading {
		m.options = []keys.KeyName{keys.KeyNew, keys.KeyHelp, keys.KeyQuit}
		return
	}

	// Instance management group
	options := []keys.KeyName{keys.KeyNew, keys.KeyKill}

	// Action group
	actionGroup := []keys.KeyName{keys.KeyEnter, keys.KeySubmit, keys.KeyMsg}
	if m.instance.Status == session.Paused {
		actionGroup = append(actionGroup, keys.KeyResume)
	} else {
		actionGroup = append(actionGroup, keys.KeyCheckout)
	}

	// Navigation group (when in a scrollable tab)
	if m.activeTab == NeedsYouTab || m.activeTab == TerminalTab {
		actionGroup = append(actionGroup, keys.KeyShiftUp)
	}

	// System group
	systemGroup := []keys.KeyName{keys.KeyTab, keys.KeyHelp, keys.KeyQuit}

	// Combine all groups
	options = append(options, actionGroup...)
	options = append(options, systemGroup...)

	m.options = options
}

// SetSize sets the width of the window. The menu will be centered horizontally within this width.
func (m *Menu) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// composerFootMenuText is the footer shown for the whole width of StateMsg
// (board #280, slice 5b, DEFECT 3) - the composer's own box already draws
// this same text on its own border (ComposerFootEditing, ui/composer.go);
// the bottom bar must read identically while the composer is open, never
// the "enter submit name" text StatePrompt's own KeySubmitName binding
// carries for the unrelated "enter prompt" instance-start flow.
const composerFootMenuText = ComposerFootEditing

func (m *Menu) String() string {
	if m.state == StateMsg {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			menuStyle.Render(composerFootMenuText))
	}

	var s strings.Builder

	// Define group boundaries
	groups := []struct {
		start int
		end   int
	}{
		{0, 2}, // Instance management group (n, d)
		{2, 5}, // Action group (enter, submit, pause/resume)
		{6, 8}, // System group (tab, help, q)
	}

	for i, k := range m.options {
		binding := keys.GlobalkeyBindings[k]

		var (
			localActionStyle = actionGroupStyle
			localKeyStyle    = keyStyle
			localDescStyle   = descStyle
		)
		if m.keyDown == k {
			localActionStyle = localActionStyle.Underline(true)
			localKeyStyle = localKeyStyle.Underline(true)
			localDescStyle = localDescStyle.Underline(true)
		}

		var inActionGroup bool
		switch m.state {
		case StateEmpty:
			// For empty state, the action group is the first group
			inActionGroup = i <= 1
		default:
			// For other states, the action group is the second group
			inActionGroup = i >= groups[1].start && i < groups[1].end
		}

		if inActionGroup {
			s.WriteString(localActionStyle.Render(binding.Help().Key))
			s.WriteString(" ")
			s.WriteString(localActionStyle.Render(binding.Help().Desc))
		} else {
			s.WriteString(localKeyStyle.Render(binding.Help().Key))
			s.WriteString(" ")
			s.WriteString(localDescStyle.Render(binding.Help().Desc))
		}

		// Add appropriate separator
		if i != len(m.options)-1 {
			isGroupEnd := false
			for _, group := range groups {
				if i == group.end-1 {
					s.WriteString(sepStyle.Render(verticalSeparator))
					isGroupEnd = true
					break
				}
			}
			if !isGroupEnd {
				s.WriteString(sepStyle.Render(separator))
			}
		}
	}

	menuText := menuStyle.Render(s.String())
	// The FINISH defect's "help footer on one line and truncated to width":
	// menuText already carries per-key/desc ANSI colour codes from the
	// Render calls above, so this needs an ansi-aware truncation (the
	// lipgloss.Place below is a documented no-op once content already
	// exceeds the given width - it never truncates on its own).
	if m.width > 0 && ansi.StringWidth(menuText) > m.width {
		menuText = ansi.Truncate(menuText, m.width, "…")
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, menuText)
}
