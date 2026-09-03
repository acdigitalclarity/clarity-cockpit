package keys

import (
	"charm.land/bubbles/v2/key"
)

type KeyName int

const (
	KeyUp KeyName = iota
	KeyDown
	KeyEnter
	KeyNew
	KeyKill
	KeyQuit
	KeyReview
	KeyPush
	KeySubmit

	KeyTab        // Tab is a special keybinding for switching between panes.
	KeySubmitName // SubmitName is a special keybinding for submitting the name of a new instance.

	KeyCheckout
	KeyResume
	KeyPrompt // New key for entering a prompt
	KeyHelp   // Key for showing help screen

	// Diff keybindings
	KeyShiftUp
	KeyShiftDown

	// Reorder keybindings
	KeyMoveUp
	KeyMoveDown

	// KeyMsg opens a one-line prompt that sends text into the selected
	// row's live tmux prompt - a tracked instance or an external (cockpit-
	// external) lane alike (Digital Clarity workspace enhancement).
	KeyMsg

	// KeyCopy copies the composer's current text (when open) or the
	// selected Needs-you row's title and number (when closed) to the
	// system clipboard (design/cockpit-pane/DECISIONS.md slice 7). Takes
	// over "c" from the upstream KeyCheckout binding below, which the
	// redesigned footer (PANE-MOCKUP-164x45.md/PANE-MOCKUP-120x36.md) no
	// longer shows - KeyCheckout's own handler stays in app.go, dormant,
	// same as PreviewPane/DiffPane (tabbed_window.go's own comment).
	KeyCopy
	// KeyCopyTail is C (shift-c): copies the WHOLE visible transcript tail
	// (every turn currently loaded, not only what is scrolled into view) as
	// plain text (slice 22, PART B) - only meaningful on the Session tab,
	// app.go's own gate, this map carries no tab awareness.
	KeyCopyTail
	// KeyTurnPicker is v: opens the Session tab's turn picker (slice 22,
	// PART B) - app.go's own stateSessionPicker then intercepts up/down/c/
	// esc directly, dispatching them to the picker instead of ordinary list
	// navigation; they are never looked up fresh against this map while it
	// is open, they are still KeyUp/KeyDown/KeyCopy by design (the picker
	// moves with the SAME keys ordinary list navigation uses).
	KeyTurnPicker
	// KeyOpenFolder opens the selected lane's own folder (a tracked
	// instance's worktree path or an external lane's WorkDir) with macOS
	// `open`. Takes over "o" from its upstream role as a plain alias for
	// KeyEnter (kept as one binding, "enter" only, below).
	KeyOpenFolder
	// KeyButterflyToggle is B (shift-b): toggles the tab-bar butterfly on
	// or off live (design refinement 4) - app.go's key switch calls
	// TabbedWindow.ToggleButterflyEnabled() directly, no dedicated
	// handler, the same shape KeyCheckout's dormant entry shows is fine
	// for a one-line call.
	KeyButterflyToggle
	// KeySelect is a display-only menu entry ("↑↓ select") - it is never
	// looked up in GlobalKeyStringsMap (Up/Down already dispatch via their
	// own KeyUp/KeyDown), it exists purely so ui/menu.go can render the
	// mock-up's combined "↑↓ select" token as one option alongside the
	// real key-bound ones.
	KeySelect
)

// GlobalKeyStringsMap is a global, immutable map string to keybinding.
var GlobalKeyStringsMap = map[string]KeyName{
	"up":         KeyUp,
	"k":          KeyUp,
	"down":       KeyDown,
	"j":          KeyDown,
	"shift+up":   KeyShiftUp,
	"shift+down": KeyShiftDown,
	"J":          KeyMoveDown,
	"K":          KeyMoveUp,
	"N":          KeyPrompt,
	"enter":      KeyEnter,
	"n":          KeyNew,
	"D":          KeyKill,
	"q":          KeyQuit,
	"tab":        KeyTab,
	"r":          KeyResume,
	"p":          KeySubmit,
	"?":          KeyHelp,
	"m":          KeyMsg,
	"c":          KeyCopy,
	"C":          KeyCopyTail,
	"v":          KeyTurnPicker,
	"o":          KeyOpenFolder,
	"B":          KeyButterflyToggle,
}

// GlobalkeyBindings is a global, immutable map of KeyName tot keybinding.
var GlobalkeyBindings = map[KeyName]key.Binding{
	KeyUp: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	KeyDown: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	KeyShiftUp: key.NewBinding(
		key.WithKeys("shift+up"),
		key.WithHelp("shift+↑", "scroll"),
	),
	KeyShiftDown: key.NewBinding(
		key.WithKeys("shift+down"),
		key.WithHelp("shift+↓", "scroll"),
	),
	KeyEnter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("↵", "attach"),
	),
	KeyNew: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "new"),
	),
	KeyKill: key.NewBinding(
		key.WithKeys("D"),
		key.WithHelp("D", "kill"),
	),
	KeyHelp: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	KeyQuit: key.NewBinding(
		key.WithKeys("q"),
		key.WithHelp("q", "quit"),
	),
	KeySubmit: key.NewBinding(
		key.WithKeys("p"),
		key.WithHelp("p", "push branch"),
	),
	KeyPrompt: key.NewBinding(
		key.WithKeys("N"),
		key.WithHelp("N", "new with prompt"),
	),
	// KeyCheckout is no longer reachable from any key ("c" now dispatches to
	// KeyCopy, GlobalKeyStringsMap above) - its own handler (app.go) and
	// help screen (app/help.go's helpTypeInstanceCheckout) are left in
	// place, dormant, the same way this fork already leaves PreviewPane and
	// DiffPane in the tree once a slice moves past them
	// (ui/tabbed_window.go's own comment).
	KeyCheckout: key.NewBinding(
		key.WithHelp("c", "checkout"),
	),
	KeyTab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "switch tab"),
	),
	KeyResume: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "resume"),
	),

	KeyMoveUp: key.NewBinding(
		key.WithKeys("K"),
		key.WithHelp("K", "move up"),
	),
	KeyMoveDown: key.NewBinding(
		key.WithKeys("J"),
		key.WithHelp("J", "move down"),
	),
	KeyMsg: key.NewBinding(
		key.WithKeys("m"),
		key.WithHelp("m", "message"),
	),
	KeyCopy: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "copy"),
	),
	KeyCopyTail: key.NewBinding(
		key.WithKeys("C"),
		key.WithHelp("C", "copy tail"),
	),
	KeyTurnPicker: key.NewBinding(
		key.WithKeys("v"),
		key.WithHelp("v", "pick turn"),
	),
	KeyOpenFolder: key.NewBinding(
		key.WithKeys("o"),
		key.WithHelp("o", "open folder"),
	),
	KeyButterflyToggle: key.NewBinding(
		key.WithKeys("B"),
		key.WithHelp("B", "toggle butterfly"),
	),
	KeySelect: key.NewBinding(
		key.WithHelp("↑↓", "select"),
	),

	// -- Special keybindings --

	KeySubmitName: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "submit name"),
	),
}
