package splash

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// frameInterval is one 24fps tick - the same time.Sleep-based
// self-rescheduling idiom app.go already uses for previewTickMsg and
// feedTickMsg, rather than tea.Tick/tea.Every (kept consistent with this
// codebase's own style).
const frameInterval = time.Second / fps

// IdleHandoffAfter is how long the idle loop runs before the splash hands
// off to the list on its own, with no key pressed - "or automatically
// after the entrance plus 2 seconds of idle" per the brief.
const IdleHandoffAfter = 2 * time.Second

// TickMsg is the splash's own animation tick. Exported so app.go's Update
// can type-switch on it directly (the same shape as spinner.TickMsg).
type TickMsg struct{}

// Model is the splash's own state: a frame counter and a phase, never the
// rendered string - View() calls RenderFrame fresh off the current frame
// number every time, the same way OUTRUN2.go's --frame/--idle flags do.
type Model struct {
	width, height int

	frame     int  // entrance frame counter, 0..EntranceFrames-1
	inIdle    bool // true once the entrance has completed
	idleFrame int  // continuous idle counter (worldFrame = EntranceFrames+idleFrame)
	idleSince time.Time

	handoff bool // true once the caller should swap in the list

	live, waiting int // fleet numbers, read once at New() (data.go)
}

// New constructs a splash model with today's fleet numbers already loaded,
// ready to tick from frame 0.
func New() *Model {
	live, waiting := fleetCounts()
	return &Model{live: live, waiting: waiting}
}

// SetSize records the terminal size the next View() renders at. Called
// from app.go's tea.WindowSizeMsg handler alongside the list's own
// SetSize, so the list is already correctly sized the instant the splash
// hands off - no extra tick needed to size it.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
}

// Tick returns the splash's first (or next) animation tick command.
func (m *Model) Tick() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(frameInterval)
		return TickMsg{}
	}
}

// Update advances the model on its own TickMsg and returns the next
// self-rescheduled tick command, or nil once the splash has handed off (no
// further ticks are scheduled - the caller drops the model on Done()).
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if _, ok := msg.(TickMsg); !ok {
		return nil
	}
	if m.handoff {
		return nil
	}

	if !m.inIdle {
		m.frame++
		if m.frame >= EntranceFrames {
			m.inIdle = true
			m.idleSince = time.Now()
			m.idleFrame = 0
		}
	} else {
		m.idleFrame++
		if time.Since(m.idleSince) >= IdleHandoffAfter {
			m.handoff = true
			return nil
		}
	}
	return m.Tick()
}

// HandleKey marks the splash for hand-off - "any key press" per the brief.
// The key itself is otherwise consumed: it does nothing else this tick.
func (m *Model) HandleKey() {
	m.handoff = true
}

// Done reports whether the caller should drop this model and show the list.
func (m *Model) Done() bool {
	return m.handoff
}

// View renders the current frame at the model's last-known size.
func (m *Model) View() string {
	entranceFrame, idleFrame := -1, -1
	if !m.inIdle {
		entranceFrame = m.frame
	} else {
		idleFrame = m.idleFrame
	}
	return RenderFrame(m.width, m.height, entranceFrame, idleFrame, m.live, m.waiting)
}
