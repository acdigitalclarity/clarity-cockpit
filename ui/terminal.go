// Package ui: this file is the Terminal tab (design/cockpit-pane/
// DECISIONS.md tab ruling 3, build slice 6; slice 15 replaces rule 3 below
// on the owner's own word - "terminal i thought would just be terminal
// rather than this session", 3 Sep). It shows two different things depending
// on the selected row's own kind, resolved once per tick by the caller
// (app.go's instanceChanged, mirroring SessionInfo's own "resolved outside
// the pane" shape) into a TerminalTarget:
//
//   - a TRACKED instance OR an EXTERNAL lane (runs in the owner's own
//     terminal): always the cockpit-owned shell in that lane's own folder, a
//     tmux session named term_<title-or-lane>, opened lazily the first time
//     the tab is viewed for it and closed on quit - upstream's own
//     pre-existing "one shell per instance" mechanism (session name, lazy
//     creation, Restore-or-Start, SetDetachedSize), reused verbatim here for
//     both row kinds alike, keyed by the tracked instance's own Title or the
//     external lane's own Name. A tracked instance's live Claude session is
//     never mirrored here any more (slice 6's old rule); it is reached with
//     Enter (attach, outside this file - app.go) and read on the Session
//     tab instead.
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
	// shows (opening lazily if needed) that instance's own term_<title>
	// shell, never its live Claude pane (slice 15).
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

// TerminalPane renders the Terminal tab: a lazily-opened term_<title> shell
// for a tracked instance, a lazily-opened term_<lane> shell for an external
// lane, or the splash's resting frame when nothing is selected.
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
		newSession: newRealTmuxSession,
	}
}

// newRealTmuxSession is NewTerminalPane's own default newSession - the real
// tmux.NewTmuxSession, gated by CLARITY_TEST_FORBID_TMUX (slice 15b): the
// fit tests in ui/fit_test.go and app/fit_test.go used to render the
// Terminal tab through this exact factory, each creating a real
// claudesquad_term_* session on the default tmux server on every go test
// run. With CLARITY_TEST_FORBID_TMUX=1 (set in TestMain for the ui and app
// packages, never for the real binary) this panics naming the session it
// would have created instead of shelling out, so a construction site added
// later that forgets to route through NewTerminalPaneWithDeps fails loudly
// the first time it is actually exercised, rather than leaving a session
// behind for someone to find and kill by hand.
func newRealTmuxSession(name, program string) *tmux.TmuxSession {
	if os.Getenv("CLARITY_TEST_FORBID_TMUX") == "1" {
		panic(fmt.Sprintf("ui.TerminalPane: real tmux session factory reached for %q under CLARITY_TEST_FORBID_TMUX=1 - this test must be built with NewTerminalPaneWithDeps, never the default NewTerminalPane, wherever it renders the Terminal tab", name))
	}
	return tmux.NewTmuxSession(name, program)
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

// updateTrackedLocked shows the selected TRACKED instance's own term_<title>
// shell (slice 15: the Terminal tab is ALWAYS a shell, for tracked and
// external rows alike - the owner's own word, "terminal i thought would just
// be terminal rather than this session"). Its live Claude session is never
// mirrored here any more, regardless of whether that session is alive,
// paused, or dead - the splash resting frame stays reserved for "nothing
// selected", never "this row has no live Claude session right now". Caller
// must hold t.mu.
func (t *TerminalPane) updateTrackedLocked(instance *session.Instance) error {
	if instance == nil {
		t.setFallbackState("Select an instance to open a terminal")
		return nil
	}
	if !instance.Started() {
		t.setFallbackState("Instance is not started yet.")
		return nil
	}
	return t.updateExternalLocked(instance.Title, instanceWorkDir(instance))
}

// instanceWorkDir resolves a tracked instance's own folder - its git
// worktree path if it has one, else its own Path (a NoWorktree
// clarity-attach lane, which has no worktree at all) - the same resolution
// app.go's own selectedFolderPath uses for the o key, since the term_<title>
// shell must open in the same folder that key would open.
func instanceWorkDir(instance *session.Instance) string {
	if instance.HasWorktree() {
		return instance.GetWorktreePath()
	}
	return instance.Path
}

// updateExternalLocked shows the lane's own term_<lane> shell (lane is a
// tracked instance's own Title when called from updateTrackedLocked, or an
// external lane's own Name), opening it first if this is the first tick the
// tab has been viewed for this lane (ensureExternalSessionLocked). Caller
// must hold t.mu.
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
// session for the given key - upstream's own "one shell per instance"
// mechanism (session name, lazy creation, Restore-or-Start,
// SetDetachedSize), keyed by a tracked instance's own Title or an external
// lane's own Name alike (slice 15). Caller must hold t.mu.
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

// targetShellKeyLocked returns the term_ shell's own map key for whichever
// row is currently selected - a tracked instance's own Title or an external
// lane's own Name (slice 15: both row kinds show a term_ shell alike) - ""
// for TerminalTargetNone, or a tracked target with no Instance set. Caller
// must hold t.mu.
func (t *TerminalPane) targetShellKeyLocked() string {
	switch t.target.Kind {
	case TerminalTargetTracked:
		if t.target.Instance == nil {
			return ""
		}
		return t.target.Instance.Title
	case TerminalTargetExternal:
		return t.target.Lane
	default:
		return ""
	}
}

// enterScrollModeLocked captures the selected row's own term_ shell's full
// session history and enters scroll mode. Caller must hold t.mu.
func (t *TerminalPane) enterScrollModeLocked() error {
	key := t.targetShellKeyLocked()
	if key == "" {
		return nil
	}
	s, ok := t.external[key]
	if !ok || s.tmuxSession == nil || !s.tmuxSession.DoesSessionExist() {
		return nil
	}
	content, err := s.tmuxSession.CapturePaneContentWithOptions("-", "-")
	if err != nil {
		return fmt.Errorf("terminal pane: failed to capture full history: %w", err)
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
