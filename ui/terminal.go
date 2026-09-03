// Package ui: this file is the Terminal tab (design/cockpit-pane/
// DECISIONS.md tab ruling 3, build slice 6). It shows two different things
// depending on the selected row's own kind, resolved once per tick by the
// caller (app.go's instanceChanged, mirroring SessionInfo's own "resolved
// outside the pane" shape) into a TerminalTarget:
//
//   - a TRACKED instance: its own live tmux pane, mirrored exactly the way
//     the dormant PreviewPane (ui/preview.go) used to show it under
//     upstream's old "Preview" tab - instance.Preview()/PreviewFullHistory(),
//     the same capture path, so a pending permission prompt in that lane is
//     visible here even though the Session tab's own transcript view cannot
//     show one. Attaching (Enter, outside this file - app.go) goes straight
//     to the instance's own tmux session, exactly as it does on every other
//     tab: this pane never owns a separate shell for a tracked row.
//   - an EXTERNAL lane (runs in the owner's own terminal): a shell this
//     pane opens lazily the first time the tab is viewed for that lane, as a
//     tmux session named term_<lane> - upstream's own pre-existing
//     mechanism for keeping one shell per instance, reused here verbatim
//     (session name, lazy creation, Restore-or-Start, SetDetachedSize) but
//     keyed by the external lane's own name instead of a tracked instance's
//     title, since an external lane has no *session.Instance at all.
//   - neither selected: the splash's resting frame, same as the Session tab
//     (ui/session.go's own renderResting), never placeholder text.
package ui

import (
	"claude-squad/cmd"
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/session/tmux"
	"claude-squad/ui/splash"
	"fmt"
	"os"
	"strings"
	"sync"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
)

var terminalPaneStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#1a1a1a"), Dark: lipgloss.Color("#dddddd")})

var terminalFooterStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#808080"), Dark: lipgloss.Color("#808080")})

// terminalSession holds one external lane's cached term_<lane> tmux session.
type terminalSession struct {
	tmuxSession *tmux.TmuxSession
	workDir     string
}

// TerminalTargetKind classifies what the Terminal tab is currently showing -
// resolved by the caller (app.go) from whichever row is selected, never
// derived inside this pane.
type TerminalTargetKind int

const (
	// TerminalTargetNone means nothing is selected - the pane shows the
	// splash's resting frame.
	TerminalTargetNone TerminalTargetKind = iota
	// TerminalTargetTracked means a tracked instance is selected - the pane
	// mirrors its own tmux pane.
	TerminalTargetTracked
	// TerminalTargetExternal means an external lane is selected - the pane
	// shows (opening lazily if needed) that lane's own term_<lane> shell.
	TerminalTargetExternal
)

// TerminalTarget is the Terminal tab's own per-tick input, resolved once by
// app.go (instanceChanged) for whichever row is currently selected.
type TerminalTarget struct {
	Kind TerminalTargetKind
	// Instance is set when Kind == TerminalTargetTracked.
	Instance *session.Instance
	// Lane is the external lane's own display name (ExternalLane.Name, the
	// same string the composer and the row list use as the send/select
	// target) - set when Kind == TerminalTargetExternal. It is also the
	// term_<lane> session's own cache key and name suffix.
	Lane string
	// WorkDir is the external lane's own working directory
	// (ExternalLane.WorkDir) - set when Kind == TerminalTargetExternal.
	WorkDir string
}

// TerminalPane renders the Terminal tab: a tracked instance's own tmux
// mirror, an external lane's lazily-opened term_<lane> shell, or the
// splash's resting frame when nothing is selected.
type TerminalPane struct {
	mu            sync.Mutex
	width, height int

	target TerminalTarget // as of the last UpdateContent call

	external map[string]*terminalSession // external lane name -> its term_ session

	content      string
	fallback     bool
	fallbackText string

	live, waiting int // fleet counters for the resting frame (splash.FleetCounts)

	isScrolling bool
	viewport    viewport.Model

	// newSession builds one external lane's own term_<lane> tmux.TmuxSession
	// - tmux.NewTmuxSession by default, overridden in tests (same package,
	// unexported field) to inject a mocked cmd.Executor/PtyFactory the same
	// way session/tmux's own tests do, so the create-on-first-view/reuse/
	// kill-on-close lifecycle can be proven without ever touching a real
	// tmux binary.
	newSession func(name, program string) *tmux.TmuxSession
}

func NewTerminalPane() *TerminalPane {
	return &TerminalPane{
		external:   make(map[string]*terminalSession),
		viewport:   viewport.New(),
		newSession: tmux.NewTmuxSession,
	}
}

// NewTerminalPaneWithDeps returns a TerminalPane whose external term_<lane>
// sessions are created through ptyFactory/cmdExec instead of the real tmux
// binary - the cross-package counterpart to session/tmux's own
// NewTmuxSessionWithDeps, for app-level tests that need a Terminal tab
// without ever shelling out for real (app/terminal_and_keys_test.go).
func NewTerminalPaneWithDeps(ptyFactory tmux.PtyFactory, cmdExec cmd.Executor) *TerminalPane {
	tp := NewTerminalPane()
	tp.newSession = func(name, program string) *tmux.TmuxSession {
		return tmux.NewTmuxSessionWithDeps(name, program, ptyFactory, cmdExec)
	}
	return tp
}

func (t *TerminalPane) SetSize(width, height int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.width = width
	t.height = height
	t.viewport.SetWidth(width)
	t.viewport.SetHeight(height)
	if t.target.Kind == TerminalTargetExternal {
		if s, ok := t.external[t.target.Lane]; ok && s.tmuxSession != nil {
			if err := s.tmuxSession.SetDetachedSize(width, height); err != nil {
				log.InfoLog.Printf("terminal pane: failed to set detached size: %v", err)
			}
		}
	}
}

// SetFleetCounts records the resting frame's "lanes live"/"needs you"
// counters, refreshed on the same feed tick as the Session tab's own
// (app.go's updateSessionTabInfo).
func (t *TerminalPane) SetFleetCounts(live, waiting int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.live, t.waiting = live, waiting
}

// setFallbackState sets the terminal pane to display a fallback message.
// The mark is sized to the pane's current width (see ui.FallbackMark) so it
// is never drawn wider than the pane itself. Caller must hold t.mu.
func (t *TerminalPane) setFallbackState(message string) {
	t.fallback = true
	t.fallbackText = lipgloss.JoinVertical(lipgloss.Center, FallbackMark(t.width), "", message)
	t.content = ""
}

// UpdateContent resolves target into this tick's rendered content: a
// tracked instance's own pane capture, an external lane's term_ shell
// capture (creating it first if this is the first tick the tab has been
// viewed for that lane), or nothing at all (String() draws the resting
// frame for TerminalTargetNone).
func (t *TerminalPane) UpdateContent(target TerminalTarget) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.target = target

	switch target.Kind {
	case TerminalTargetTracked:
		return t.updateTrackedLocked(target.Instance)
	case TerminalTargetExternal:
		return t.updateExternalLocked(target.Lane, target.WorkDir)
	default:
		t.fallback = false
		t.content = ""
		return nil
	}
}

// updateTrackedLocked mirrors the selected TRACKED instance's own tmux
// pane - the dormant PreviewPane's own capture path (ui/preview.go's
// UpdateContent), not a separate term_ shell. Caller must hold t.mu.
func (t *TerminalPane) updateTrackedLocked(instance *session.Instance) error {
	if instance == nil {
		t.setFallbackState("Select an instance to open a terminal")
		return nil
	}
	if instance.Status == session.Paused {
		t.setFallbackState("Session is paused. Resume to use terminal.")
		return nil
	}
	if !instance.Started() {
		t.setFallbackState("Instance is not started yet.")
		return nil
	}
	if t.isScrolling {
		// Full-history capture only happens lazily on entering scroll mode
		// (enterScrollModeLocked below) - a mid-scroll tick must not
		// overwrite the viewport's own content.
		return nil
	}

	content, err := instance.Preview()
	if err != nil {
		return fmt.Errorf("terminal pane: failed to capture content: %w", err)
	}
	t.fallback = false
	t.content = content
	return nil
}

// updateExternalLocked shows the EXTERNAL lane's own term_<lane> shell,
// opening it first if this is the first tick the tab has been viewed for
// this lane (ensureExternalSessionLocked). Caller must hold t.mu.
func (t *TerminalPane) updateExternalLocked(lane, workDir string) error {
	if lane == "" {
		t.setFallbackState("Select a lane to open a terminal")
		return nil
	}
	if t.isScrolling {
		return nil
	}
	if err := t.ensureExternalSessionLocked(lane, workDir); err != nil {
		return err
	}
	s, ok := t.external[lane]
	if !ok || s.tmuxSession == nil || !s.tmuxSession.DoesSessionExist() {
		t.setFallbackState("Terminal session not available.")
		return nil
	}

	content, err := s.tmuxSession.CapturePaneContent()
	if err != nil {
		return fmt.Errorf("terminal pane: failed to capture content: %w", err)
	}
	t.fallback = false
	t.content = content
	return nil
}

// ensureExternalSessionLocked creates or reuses the cached term_<lane> tmux
// session for an external lane - upstream's own "one shell per instance"
// mechanism (session name, lazy creation, Restore-or-Start,
// SetDetachedSize), reused verbatim here but keyed by the lane's own name
// rather than a tracked instance's title. Caller must hold t.mu.
func (t *TerminalPane) ensureExternalSessionLocked(lane, workDir string) error {
	if lane == "" || workDir == "" {
		return nil
	}

	if s, ok := t.external[lane]; ok {
		if s.tmuxSession != nil && s.tmuxSession.DoesSessionExist() {
			return nil
		}
		delete(t.external, lane)
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	termName := "term_" + lane
	ts := t.newSession(termName, shell)

	if ts.DoesSessionExist() {
		if err := ts.Restore(); err != nil {
			// Session exists but can't restore, kill it and start fresh
			_ = ts.Close()
			ts = t.newSession(termName, shell)
			if err := ts.Start(workDir); err != nil {
				return fmt.Errorf("terminal pane: failed to start session: %w", err)
			}
		}
	} else {
		if err := ts.Start(workDir); err != nil {
			return fmt.Errorf("terminal pane: failed to start session: %w", err)
		}
	}

	t.external[lane] = &terminalSession{tmuxSession: ts, workDir: workDir}

	if t.width > 0 && t.height > 0 {
		if err := ts.SetDetachedSize(t.width, t.height); err != nil {
			log.InfoLog.Printf("terminal pane: failed to set size: %v", err)
		}
	}

	return nil
}

// Attach attaches to an external lane's own term_<lane> tmux session
// (full-screen) - lane must already have been viewed at least once (its
// session created lazily by UpdateContent/ensureExternalSessionLocked); a
// tracked instance attaches through its own session instead (session.List's
// Attach, the same path Enter uses on every other tab - app.go's KeyEnter
// handler never routes a tracked row through here).
func (t *TerminalPane) Attach(lane string) (chan struct{}, error) {
	t.mu.Lock()
	s, ok := t.external[lane]
	if !ok || s.tmuxSession == nil {
		t.mu.Unlock()
		return nil, fmt.Errorf("no terminal session for lane %q", lane)
	}
	if !s.tmuxSession.DoesSessionExist() {
		t.mu.Unlock()
		return nil, fmt.Errorf("terminal session does not exist")
	}
	ts := s.tmuxSession
	t.mu.Unlock()
	return ts.Attach()
}

// Close kills every cached external term_<lane> session - called when the
// cockpit quits (app.go's handleQuit), exactly as upstream tore down its
// own term_ shells.
func (t *TerminalPane) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for lane, s := range t.external {
		if s.tmuxSession != nil {
			if err := s.tmuxSession.Close(); err != nil {
				log.InfoLog.Printf("terminal pane: failed to close session for %s: %v", lane, err)
			}
		}
	}
	t.external = make(map[string]*terminalSession)
	t.target = TerminalTarget{}
	t.content = ""
	t.fallback = false
	t.fallbackText = ""
}

// CloseForLane kills the cached term_<lane> session for one external lane
// (e.g. when that lane is no longer discovered live) - named for the
// external map this pane now owns (a tracked instance never has an entry
// here, so calling this with a tracked instance's title is a harmless
// no-op).
func (t *TerminalPane) CloseForLane(lane string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.external[lane]; ok {
		if s.tmuxSession != nil {
			if err := s.tmuxSession.Close(); err != nil {
				log.InfoLog.Printf("terminal pane: failed to close session for %s: %v", lane, err)
			}
		}
		delete(t.external, lane)
	}
	if t.target.Kind == TerminalTargetExternal && t.target.Lane == lane {
		t.target = TerminalTarget{}
		t.content = ""
		t.fallback = false
		t.fallbackText = ""
	}
}

func (t *TerminalPane) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	width := t.width
	height := t.height

	if width == 0 || height == 0 {
		return strings.Repeat("\n", height)
	}

	if t.isScrolling {
		return t.viewport.View()
	}

	if t.target.Kind == TerminalTargetNone {
		return t.renderRestingLocked()
	}

	fallback := t.fallback
	fallbackText := t.fallbackText
	content := t.content

	if fallback {
		// 3 = tab bar height (border + padding + text), 4 = window style frame (top/bottom border + padding)
		availableHeight := height - 3 - 4
		fallbackLines := len(strings.Split(fallbackText, "\n"))
		totalPadding := availableHeight - fallbackLines
		topPadding := 0
		bottomPadding := 0
		if totalPadding > 0 {
			topPadding = totalPadding / 2
			bottomPadding = totalPadding - topPadding
		}

		var lines []string
		if topPadding > 0 {
			lines = append(lines, strings.Repeat("\n", topPadding))
		}
		lines = append(lines, fallbackText)
		if bottomPadding > 0 {
			lines = append(lines, strings.Repeat("\n", bottomPadding))
		}

		return terminalPaneStyle.
			Width(width).
			Align(lipgloss.Center).
			Render(strings.Join(lines, ""))
	}

	// Normal mode: show captured content
	lines := strings.Split(content, "\n")

	if height > 0 {
		if len(lines) > height {
			lines = lines[len(lines)-height:]
		} else {
			padding := height - len(lines)
			lines = append(lines, make([]string, padding)...)
		}
	}

	contentStr := strings.Join(lines, "\n")
	return terminalPaneStyle.Width(width).Render(contentStr)
}

// renderRestingLocked draws the splash's resting/peak frame, the same way
// the Session tab does when nothing is selected (ui/session.go's own
// renderResting) - never placeholder text. Caller must hold t.mu.
func (t *TerminalPane) renderRestingLocked() string {
	frame := splash.RenderFrame(t.width, t.height, -1, -1, t.live, t.waiting)
	if !fitsBox(frame, t.width, t.height) {
		frame = FallbackMark(t.width)
	}
	return lipgloss.Place(t.width, t.height, lipgloss.Center, lipgloss.Center, frame)
}

// enterScrollModeLocked captures the full pane history (tracked instance)
// or full session history (external lane's term_ shell) and enters scroll
// mode. Caller must hold t.mu.
func (t *TerminalPane) enterScrollModeLocked() error {
	var content string
	var err error

	switch t.target.Kind {
	case TerminalTargetTracked:
		if t.target.Instance == nil {
			return nil
		}
		content, err = t.target.Instance.PreviewFullHistory()
		if err != nil {
			return fmt.Errorf("terminal pane: failed to capture full history: %w", err)
		}
	case TerminalTargetExternal:
		s, ok := t.external[t.target.Lane]
		if !ok || s.tmuxSession == nil || !s.tmuxSession.DoesSessionExist() {
			return nil
		}
		content, err = s.tmuxSession.CapturePaneContentWithOptions("-", "-")
		if err != nil {
			return fmt.Errorf("terminal pane: failed to capture full history: %w", err)
		}
	default:
		return nil
	}

	footer := terminalFooterStyle.Render("ESC to exit scroll mode")
	contentWithFooter := lipgloss.JoinVertical(lipgloss.Left, content, footer)
	t.viewport.SetContent(contentWithFooter)
	t.viewport.GotoBottom()
	t.isScrolling = true
	return nil
}

// ScrollUp enters scroll mode (if not already) and scrolls up.
func (t *TerminalPane) ScrollUp() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.isScrolling {
		return t.enterScrollModeLocked()
	}
	t.viewport.ScrollUp(1)
	return nil
}

// ScrollDown enters scroll mode (if not already) and scrolls down.
func (t *TerminalPane) ScrollDown() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.isScrolling {
		return t.enterScrollModeLocked()
	}
	t.viewport.ScrollDown(1)
	return nil
}

// ResetToNormalMode exits scroll mode and restores normal content display.
func (t *TerminalPane) ResetToNormalMode() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.isScrolling {
		return
	}
	t.isScrolling = false
	t.viewport.SetContent("")
	t.viewport.GotoTop()
}

// IsScrolling returns whether the terminal pane is in scroll mode.
func (t *TerminalPane) IsScrolling() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.isScrolling
}
