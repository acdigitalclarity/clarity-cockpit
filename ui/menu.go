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
	// StateAnswerConfirm/StateBankConfirm are the y/b confirm strips' own
	// menu states (slice 18) - same shape as StateMsg: String() shows the
	// strip's own full-width foot text instead of the ordinary option list.
	StateAnswerConfirm
	StateBankConfirm
)

type Menu struct {
	options       []keys.KeyName
	height, width int
	state         MenuState
	instance      *session.Instance
	// isExternal marks the current selection as an external lane (no
	// tracked tmux session or worktree) - set alongside instance by
	// SetInstance. ↵ attach and m message are dimmed on this row (they
	// still work: m still copies per slice 5) rather than removed, since
	// the footer's own shape must not shift between row kinds.
	isExternal bool
	// isNeedsYou marks the current selection as a Needs-you row (board #280
	// pane-10 walkthrough DEFECT 3) - set alongside instance/isExternal by
	// SetInstance. Before this fix, instanceChanged() passed a Needs-you
	// row's nil instance and false isExternal straight through, which
	// SetInstance read as "nothing selected" (StateEmpty) and so drew the
	// wrong footer ("n new • N new with prompt │ m message • ? help •
	// q quit") instead of the drawn lane-action line. A Needs-you row gets
	// that same drawn line, with ↵ attach and o open folder dimmed (there is
	// no tracked instance or folder to act on) while m message and c copy
	// stay live - the row's own raising lane is still a valid send/copy
	// target (composerTarget resolves it separately).
	isNeedsYou bool
	// isAnswered marks the current selection as an ALREADY-answered
	// Needs-you row (slice 18) - set by SetNeedsYouAnswered, independently
	// of SetInstance (app.go's instanceChanged calls both). The y footer
	// token is absent on such a row (ANSWER-AND-BANK-SPEC.md "the y token
	// is absent on an answered row").
	isAnswered bool
	activeTab  int

	// groupBounds is the CURRENT option list's own [start,end) vertical-
	// separator boundaries (design/cockpit-pane/PANE-MOCKUP-*.md's "│"
	// dividers) - built by updateOptions alongside m.options itself, so
	// String() never has to re-derive group shape from option count (DEFECT:
	// the upstream version hardcoded {0,2},{2,5},{6,8} regardless of what
	// m.options actually held, which silently drifted the moment the list's
	// own length changed - see addInstanceOptions' own comment).
	groupBounds [][2]int

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

// SetInstance updates the current selection and refreshes menu options.
// isExternal marks a selected external lane and isNeedsYou marks a selected
// Needs-you row (GetSelectedInstance returns nil for both, same as "nothing
// selected" - the caller, app.go's instanceChanged, is the only place that
// can tell the three apart): an external or Needs-you row still gets the
// full default option list (per-kind dimming, per DECISIONS.md slice 7 and
// board #280 pane-10 walkthrough DEFECT 3), never the bare StateEmpty one.
func (m *Menu) SetInstance(instance *session.Instance, isExternal, isNeedsYou bool) {
	m.instance = instance
	m.isExternal = isExternal
	m.isNeedsYou = isNeedsYou
	// Only change the state if we're not in a special state (NewInstance,
	// Prompt or the composer's own Msg) - a feed tick's instanceChanged()
	// must never kick the footer out of "enter send · esc cancel" while a
	// message is being typed.
	if m.state != StateNewInstance && m.state != StatePrompt && m.state != StateMsg &&
		m.state != StateAnswerConfirm && m.state != StateBankConfirm {
		if m.instance != nil || m.isExternal || m.isNeedsYou {
			m.state = StateDefault
		} else {
			m.state = StateEmpty
		}
	}
	m.updateOptions()
}

// SetNeedsYouAnswered records whether the currently selected Needs-you row
// has already been answered this session - see the isAnswered field's own
// doc comment.
func (m *Menu) SetNeedsYouAnswered(answered bool) {
	m.isAnswered = answered
	m.updateOptions()
}

// SetActiveTab updates the currently active tab
func (m *Menu) SetActiveTab(tab int) {
	m.activeTab = tab
	m.updateOptions()
}

// emptyGroupBounds is the StateEmpty/StateDefault-with-nothing-selected
// option list's own separator shape - a single "│" after index 1 ("N new
// with prompt"), everywhere else " • " (the render loop's own
// `i != len(options)-1` guard already suppresses any separator after the
// last item, so {2,5}'s own end-1=4 never fires) - unchanged by slice 7,
// since defaultMenuOptions itself was not touched.
var emptyGroupBounds = [][2]int{{0, 2}, {2, 5}}

// updateOptions updates the menu options based on current state and instance
func (m *Menu) updateOptions() {
	switch m.state {
	case StateEmpty:
		m.options = defaultMenuOptions
		m.groupBounds = emptyGroupBounds
	case StateDefault:
		if m.instance != nil || m.isExternal || m.isNeedsYou {
			// A tracked instance, an external lane, or a Needs-you row is
			// selected: show the lane-action options (dimmed per-kind - see
			// String()).
			m.addInstanceOptions()
		} else {
			// Nothing at all is selected.
			m.options = defaultMenuOptions
			m.groupBounds = emptyGroupBounds
		}
	case StateNewInstance:
		m.options = newInstanceMenuOptions
		m.groupBounds = nil
	case StatePrompt:
		m.options = promptMenuOptions
		m.groupBounds = nil
	case StateMsg, StateAnswerConfirm, StateBankConfirm:
		// String() short-circuits these before m.options is ever read (the
		// composer's own strip foot text, not the key-binding groups below).
		m.options = nil
		m.groupBounds = nil
	}
}

// addInstanceOptions builds the redesigned lane-action option list
// (design/cockpit-pane/PANE-MOCKUP-164x45.md's own foot: "↑↓ select • ↵
// attach │ m message • c copy • o open folder │ tab switch tab • ? help •
// q quit"). Upstream's git-worktree group (new/kill/push/checkout/resume)
// is no longer part of this persistent bar - the mock-up the owner approved
// ("looks good", DECISIONS.md) drops it entirely for both a tracked and an
// external row alike; those keys keep their own bindings (n/D/p/r are
// untouched in keys.go) and stay documented in the "?" help overlay
// (app/help.go), simply no longer advertised here. groupBounds is derived
// from the list this function just built, not hardcoded, so a conditional
// entry (KeyShiftUp below) never silently misaligns the "│" dividers the
// way the upstream {0,2},{2,5},{6,8} literal did.
func (m *Menu) addInstanceOptions() {
	// Loading instances only get minimal options
	if m.instance != nil && m.instance.Status == session.Loading {
		m.options = []keys.KeyName{keys.KeyNew, keys.KeyHelp, keys.KeyQuit}
		m.groupBounds = nil
		return
	}

	// group0: ↑↓ select, then the row-kind's own primary action - a
	// Needs-you row never carries ↵ attach at all (nothing to attach TO -
	// ANSWER-AND-BANK-MOCKUP-164x45.md screens 1 and 3 both drop it), y
	// answer replacing it while the row is not yet answered; a lane row
	// (tracked or external) keeps ↵ attach and gains b bank and close
	// alongside it (ANSWER-AND-BANK-SPEC.md "Footer tokens").
	var options []keys.KeyName
	switch {
	case m.isNeedsYou && !m.isAnswered:
		options = []keys.KeyName{keys.KeySelect, keys.KeyAnswer}
	case m.isNeedsYou:
		options = []keys.KeyName{keys.KeySelect}
	default:
		options = []keys.KeyName{keys.KeySelect, keys.KeyEnter, keys.KeyBank}
	}
	group0End := len(options)

	options = append(options, keys.KeyMsg, keys.KeyCopy, keys.KeyOpenFolder)

	// Scroll hint (when in a scrollable tab) - appended before the system
	// group, same position upstream used.
	if m.activeTab == NeedsYouTab || m.activeTab == TerminalTab {
		options = append(options, keys.KeyShiftUp)
	}

	systemStart := len(options)
	options = append(options, keys.KeyTab, keys.KeyHelp, keys.KeyQuit)

	m.options = options
	m.groupBounds = [][2]int{{0, group0End}, {group0End, systemStart}, {systemStart, len(options)}}
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
	switch m.state {
	case StateMsg:
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			menuStyle.Render(composerFootMenuText))
	case StateAnswerConfirm:
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			menuStyle.Render(AnswerConfirmFoot))
	case StateBankConfirm:
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			menuStyle.Render(BankConfirmFoot))
	}

	var s strings.Builder

	groups := m.groupBounds

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

		// ↵ attach and m message are dimmed on an external row
		// (DECISIONS.md slice 7's own "greyed" requirement) - m still works
		// as a clipboard copy (slice 5), ↵ still no-ops outside the Terminal
		// tab and shows the "no terminal yet" footer inside it (app.go), so
		// the dimming is cosmetic only, never a disabled control. A
		// Needs-you row dims o open folder (board #280 pane-10 walkthrough
		// DEFECT 3): there is no folder behind the row itself, but m message
		// and c copy stay live - composerTarget resolves the row's own
		// raising lane as a genuine send/copy target, distinct from o open
		// folder which needs an actual tracked instance. ↵ attach is no
		// longer even IN the option list for a Needs-you row (slice 18: y
		// answer, or nothing at all once answered, takes its place) - there
		// is nothing left here to dim for it.
		dim := (m.isExternal && (k == keys.KeyEnter || k == keys.KeyMsg)) ||
			(m.isNeedsYou && k == keys.KeyOpenFolder)
		if dim {
			localActionStyle = localActionStyle.Faint(true)
			localKeyStyle = localKeyStyle.Faint(true)
			localDescStyle = localDescStyle.Faint(true)
		}

		var inActionGroup bool
		switch m.state {
		case StateEmpty:
			// For empty state, the action group is the first group
			inActionGroup = i <= 1
		default:
			// For other states, the action group is the second group
			inActionGroup = len(groups) > 1 && i >= groups[1][0] && i < groups[1][1]
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
				if i == group[1]-1 {
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
