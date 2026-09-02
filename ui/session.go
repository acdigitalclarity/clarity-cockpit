// Package ui: this file is the Session tab (design/cockpit-pane/
// DECISIONS.md slice 3) - the selected lane's own conversation, live from
// clarity.ReadLaneTail via the shared LaneTailCache, or the splash's resting
// frame when nothing is selected. It replaces the old tmux-capture Preview
// pane as the first/default tab; PreviewPane itself is left in place,
// unused for now (see tabbed_window.go's own comment) rather than deleted.
package ui

import (
	"claude-squad/session/clarity"
	"claude-squad/ui/splash"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"github.com/charmbracelet/x/ansi"
)

var sessionMutedStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#7A7474"), Dark: lipgloss.Color("#9C9494")})

var sessionTextStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#1a1a1a"), Dark: lipgloss.Color("#dddddd")})

// sessionFixedBottomRows is the row cost of the bottom rule, the state
// line and the three-line composer box - always reserved, regardless of how
// much of the turns region above them is used (the "newest content pinned
// to the bottom" requirement: these always sit at the pane's own bottom
// edge).
const sessionFixedBottomRows = 5

// sessionTurnIndent is the hanging indent owner/assistant body text wraps
// under, below its own "hh:mm:ss   YOU|CLAUDE" header line.
const sessionTurnIndent = "  "

// SessionInfo is everything one Session-tab render needs for the SELECTED
// lane, tracked or external - resolved once per feed tick (app.go's
// feedTickMsg) and handed to SessionPane.SetInfo. A nil SessionInfo (or
// SetInfo(nil)) means nothing is selected: the pane shows the resting
// frame instead.
type SessionInfo struct {
	// Lane is the lane name as the owner types it - a tracked instance's
	// Title or an external lane's ExternalLane.Name.
	Lane string
	// WorkDir is the lane's own working directory ("" when not derivable -
	// an external lane whose transcript's cwd scan never found one).
	WorkDir string
	// Branch is the tracked instance's git branch, "" when not known
	// (external lane, or a clarity-attach instance with no worktree).
	Branch string
	// Tail is the lane's clarity.ReadLaneTail result, read with a maxTurns
	// large enough to fill the pane (app.go passes 40).
	Tail clarity.LaneTail
	// CtxPct/CtxOK is the same cached context-fill gauge the list row for
	// this lane already shows, so the two never disagree.
	CtxPct int
	CtxOK  bool
	// Now is the feed tick's own clock, threaded through so age-based text
	// ("last write N min ago") is deterministic against the same instant
	// the rest of this info was computed from, not a fresh time.Now() at
	// render time.
	Now time.Time
}

// SessionPane renders the Session tab: the header, the turns (oldest first,
// newest pinned to the bottom, scrollable), the state line and the inert
// composer box - or the splash's resting frame when info is nil.
type SessionPane struct {
	width, height int

	info *SessionInfo

	live, waiting int // fleet counters for the resting frame (splash.FleetCounts)

	viewport viewport.Model // the scrollable turns region only, not the whole pane
}

// NewSessionPane returns an empty SessionPane - SetSize and SetInfo (or
// Clear) still need calling before String() shows anything meaningful,
// same contract as PreviewPane/TerminalPane/DiffPane.
func NewSessionPane() *SessionPane {
	return &SessionPane{viewport: viewport.New()}
}

// SetSize sets the pane's own content dimensions (the tabbed window's
// contentWidth/contentHeight - the same size every tab receives).
func (s *SessionPane) SetSize(width, height int) {
	s.width, s.height = width, height
	s.refreshViewport()
}

// SetInfo replaces the SELECTED lane's data and rebuilds the turns
// viewport, pinned to the bottom (the newest turn is always visible on the
// first render after a change). nil clears the selection - String() then
// shows the resting frame.
func (s *SessionPane) SetInfo(info *SessionInfo) {
	s.info = info
	s.refreshViewport()
}

// Clear is SetInfo(nil) under the name app.go's other panes use for "nothing
// selected".
func (s *SessionPane) Clear() {
	s.SetInfo(nil)
}

// SetFleetCounts records the "lanes live"/"needs you" counters the resting
// frame's splash.RenderFrame draws, refreshed on the same feed tick as
// everything else (never read from disk at render time).
func (s *SessionPane) SetFleetCounts(live, waiting int) {
	s.live, s.waiting = live, waiting
}

// fixedTopRows is the header/rule/divider row cost above the turns region -
// 3 rows normally (two header lines, one rule), plus the "⋯ earlier in this
// session" divider when the tail was cut.
func (s *SessionPane) fixedTopRows() int {
	if s.info == nil {
		return 0
	}
	rows := 3
	if s.info.Tail.Truncated {
		rows++
	}
	return rows
}

// turnsAreaHeight is the remaining budget for the scrollable turns region,
// clamped to zero rather than going negative on a pane too short to show
// everything (the resting frame or a fallback mark is what a caller shows
// instead in that case, not this pane's turns view).
func (s *SessionPane) turnsAreaHeight() int {
	h := s.height - s.fixedTopRows() - sessionFixedBottomRows
	if h < 0 {
		h = 0
	}
	return h
}

// refreshViewport rebuilds the turns viewport's content - called whenever
// the size or the selected lane's data changes, never from String() (which
// must not reset a scroll the owner is mid-way through).
//
// DEFECT 3: this used to call GotoBottom() unconditionally on every rebuild,
// so a mid-scroll read (shift+up's own reason to exist) was thrown away on
// the app's 3-second feed tick, which calls SetInfo again with the same
// lane's freshly-read data. The rule: read whether the viewport was AT the
// bottom before this rebuild changes the content underneath it: if it was,
// it stays pinned to the (new) bottom, same as before; otherwise it keeps
// its own line offset, clamped to whatever the new content's own maximum
// now is (SetYOffset's own contract) - never snapping back down.
func (s *SessionPane) refreshViewport() {
	wasAtBottom := s.viewport.TotalLineCount() == 0 || s.viewport.AtBottom()
	prevOffset := s.viewport.YOffset()

	s.viewport.SetWidth(s.width)
	s.viewport.SetHeight(s.turnsAreaHeight())
	if s.info == nil {
		s.viewport.SetContent("")
		return
	}
	lines := buildTurnLines(s.info.Tail.Turns, s.width)
	s.viewport.SetContent(strings.Join(lines, "\n"))
	if wasAtBottom {
		s.viewport.GotoBottom()
	} else {
		s.viewport.SetYOffset(prevOffset)
	}
}

// ScrollUp/ScrollDown scroll the turns region - bound to the app-wide
// shift+up/shift+down keys (keys.KeyShiftUp/KeyShiftDown) TabbedWindow
// already dispatches to the active tab, the same keys Preview/Diff/Terminal
// scroll with. Unlike those panes, the Session pane's turns are already
// fully loaded (maxTurns bounds them up front), so there is no separate
// "enter scroll mode" step - scrolling is always immediate.
func (s *SessionPane) ScrollUp() {
	s.viewport.ScrollUp(1)
}

func (s *SessionPane) ScrollDown() {
	s.viewport.ScrollDown(1)
}

// String renders the pane: the resting frame when nothing is selected, or
// exactly s.height lines of header/turns/state/composer otherwise. Every
// returned line is bounded to s.width (ansi-aware) - the FINISH
// requirement that nothing exceeds the pane's own box.
func (s *SessionPane) String() string {
	if s.width <= 0 || s.height <= 0 {
		h := s.height
		if h < 0 {
			h = 0
		}
		return strings.Repeat("\n", h)
	}
	if s.info == nil {
		return s.renderResting()
	}

	lines := make([]string, 0, s.height)
	lines = append(lines, s.renderHeaderLine1(), s.renderHeaderLine2(), s.rule())
	if s.info.Tail.Truncated {
		lines = append(lines, s.renderEarlierLine())
	}

	turnsHeight := s.turnsAreaHeight()
	s.viewport.SetHeight(turnsHeight)
	s.viewport.SetWidth(s.width)
	body := strings.Split(s.viewport.View(), "\n")
	if turnsHeight <= 0 {
		body = nil
	} else if len(body) < turnsHeight {
		body = append(body, make([]string, turnsHeight-len(body))...)
	} else if len(body) > turnsHeight {
		body = body[:turnsHeight]
	}
	lines = append(lines, body...)

	lines = append(lines, s.rule(), s.renderStateLine())
	lines = append(lines, s.renderComposerLines()...)

	if len(lines) > s.height {
		lines = lines[:s.height]
	} else if len(lines) < s.height {
		lines = append(lines, make([]string, s.height-len(lines))...)
	}
	for i, l := range lines {
		lines[i] = fitRow(l, s.width)
	}
	return strings.Join(lines, "\n")
}

// renderResting draws the splash's resting/peak frame (RenderFrame with
// entranceFrame=idleFrame=-1) sized to this pane, falling back to the plain
// wordmark when the frame would not fit inside the pane's own box - proven
// empirically (rendered, then measured) rather than guessed from a
// threshold, since RenderFrame's own row/column budget depends on width in
// ways this package does not otherwise duplicate.
func (s *SessionPane) renderResting() string {
	frame := splash.RenderFrame(s.width, s.height, -1, -1, s.live, s.waiting)
	if !fitsBox(frame, s.width, s.height) {
		frame = FallbackMark(s.width)
	}
	return lipgloss.Place(s.width, s.height, lipgloss.Center, lipgloss.Center, frame)
}

// fitsBox reports whether every line of content is within width and the
// total line count is within height.
func fitsBox(content string, width, height int) bool {
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		return false
	}
	for _, l := range lines {
		if ansi.StringWidth(l) > width {
			return false
		}
	}
	return true
}

// renderHeaderLine1 is "<lane>  ...  <glyph> <state>[ · N agents]   ctx
// NN%  <bar>   last write hh:mm:ss" - the lane name left, everything else
// right-aligned to the pane's own width (design/cockpit-pane/
// PANE-MOCKUP-164x45.md line 1).
func (s *SessionPane) renderHeaderLine1() string {
	t := s.info.Tail
	glyph, style := laneStateGlyph(t.State)
	g := style.Render(glyph)

	agentSeg := ""
	if t.PendingAgents > 0 {
		agentSeg = fmt.Sprintf(" · %d agent%s", t.PendingAgents, plural(t.PendingAgents))
	}
	ctxSeg := fmt.Sprintf("   %s  %s", ctxBarLabel(s.info.CtxPct, s.info.CtxOK), ctxBarGauge(s.info.CtxPct, s.info.CtxOK))
	writeSeg := ""
	if !t.LastWrite.IsZero() {
		writeSeg = fmt.Sprintf("   last write %s", t.LastWrite.Local().Format("15:04:05"))
	}

	// Progressively drop the least essential trailing clauses (last write,
	// then the agent count) on a pane too narrow for all of them - the
	// lane name and the state/ctx figure are this header's own essentials
	// and stay on screen as long as anything does, rather than the lane
	// name being the first thing padRow's own left-truncation sacrifices.
	candidates := []string{
		g + " " + t.State + agentSeg + ctxSeg + writeSeg,
		g + " " + t.State + agentSeg + ctxSeg,
		g + " " + t.State + ctxSeg,
		g + " " + t.State,
	}
	for _, right := range candidates {
		if ansi.StringWidth(s.info.Lane)+ansi.StringWidth(right)+1 <= s.width {
			return sessionTextStyle.Render(padRow(s.info.Lane, right, s.width))
		}
	}
	return sessionTextStyle.Render(padRow(s.info.Lane, candidates[len(candidates)-1], s.width))
}

// renderHeaderLine2 is "<workdir> · <branch> · <model> · <window>   ...
// turn <duration> · <N> msgs · session <id8>" (mock-up line 2) - any
// left-side field that is not known/derivable is simply left out, never
// shown blank or as "n/a".
func (s *SessionPane) renderHeaderLine2() string {
	info := s.info
	t := info.Tail

	var left []string
	if info.WorkDir != "" {
		left = append(left, shortenHome(info.WorkDir))
	}
	if info.Branch != "" {
		left = append(left, info.Branch)
	}
	if model := strings.TrimPrefix(t.Model, "claude-"); model != "" {
		left = append(left, model)
	}
	if label, ok := clarity.ModelWindowLabel(t.Model); ok {
		left = append(left, label)
	}

	var right string
	if t.TurnDuration > 0 {
		right = fmt.Sprintf("turn %s · ", formatDurationSpaced(t.TurnDuration))
	}
	right += fmt.Sprintf("%d msgs", t.Messages)
	if stem := sessionIDStem(t.Transcript); stem != "" {
		right += fmt.Sprintf(" · session %s", stem)
	}

	return sessionMutedStyle.Render(padRowKeepRight(strings.Join(left, " · "), right, s.width))
}

// rule is a full-width horizontal divider, dim.
func (s *SessionPane) rule() string {
	return sessionMutedStyle.Render(strings.Repeat("─", s.width))
}

// renderEarlierLine is "⋯ earlier in this session · N messages · shift+↑ to
// scroll back", shown only when info.Tail.Truncated.
func (s *SessionPane) renderEarlierLine() string {
	text := fmt.Sprintf("⋯  earlier in this session · %d messages · shift+↑ to scroll back", s.info.Tail.Messages)
	return sessionMutedStyle.Render(ansiTruncateRow(text, s.width))
}

// renderStateLine is the pane's own foot summary: "<glyph> <state> · turn
// closed hh:mm:ss · N background agents in flight · nothing waiting on
// you" (working/idle), "· waiting on you" (StateWaitingYou), or "stalled:
// open turn, last write N min ago" (StateStalled - there is no closed turn
// to report, so the whole clause changes shape).
func (s *SessionPane) renderStateLine() string {
	t := s.info.Tail
	glyph, style := laneStateGlyph(t.State)
	g := style.Render(glyph)

	if t.State == clarity.StateStalled {
		return sessionTextStyle.Render(fmt.Sprintf("%s %s · stalled: open turn, last write %s ago", g, t.State, minutesAgo(t.LastWrite, s.info.Now)))
	}

	trailing := "nothing waiting on you"
	if t.State == clarity.StateWaitingYou {
		trailing = "waiting on you"
	}
	return sessionTextStyle.Render(fmt.Sprintf("%s %s · turn closed %s · %d background agents in flight · %s",
		g, t.State, t.LastTurn.Local().Format("15:04:05"), t.PendingAgents, trailing))
}

// renderComposerLines draws the inert composer box exactly as the mock-up
// (design/cockpit-pane/PANE-MOCKUP-164x45.md lines 38-40), greyed - except
// its foot text, which the brief deliberately overrides to "m message · esc
// back" (the mock's "enter send · esc cancel" describes slice 5's WIRED
// composer; this slice draws the box only, no input handling).
func (s *SessionPane) renderComposerLines() []string {
	width := s.width
	title := fmt.Sprintf(" message %s ", s.info.Lane)
	top := "┌" + title + strings.Repeat("─", maxInt0(width-2-lipgloss.Width(title))) + "┐"

	prompt := "▸ █"
	mid := "│ " + prompt + strings.Repeat(" ", maxInt0(width-4-lipgloss.Width(prompt))) + " │"

	foot := " m message · esc back "
	bottom := "└" + strings.Repeat("─", maxInt0(width-2-lipgloss.Width(foot))) + foot + "─┘"

	return []string{
		sessionMutedStyle.Render(fitRow(top, width)),
		sessionMutedStyle.Render(fitRow(mid, width)),
		sessionMutedStyle.Render(fitRow(bottom, width)),
	}
}

// -- small rendering helpers, package-private to this file --------------

func maxInt0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// fitRow pads or ansi-truncates s to exactly width cells - the same "never
// exceed, never fall short" guarantee ui/list.go's own rows carry.
func fitRow(s string, width int) string {
	w := ansi.StringWidth(s)
	switch {
	case w > width:
		return ansiTruncateRow(s, width)
	case w < width:
		return s + strings.Repeat(" ", width-w)
	default:
		return s
	}
}

// padRow places left flush left and right flush right within width,
// truncating left (never right - right carries the more important
// state/result information) when both together would overflow.
func padRow(left, right string, width int) string {
	leftW := ansi.StringWidth(left)
	rightW := ansi.StringWidth(right)
	if leftW+rightW+1 > width {
		avail := width - rightW - 1
		if avail < 0 {
			avail = 0
			left = ""
		} else {
			left = ansiTruncateRow(left, avail)
		}
		leftW = ansi.StringWidth(left)
	}
	gap := width - leftW - rightW
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// padRowKeepRight is padRow's mirror for header line 2 (DEFECT 2): when
// left and right together overflow width, the LEFT side is truncated from
// its own FRONT with an ellipsis prefix, keeping its tail - branch/model/
// window, joined onto the end of the same string - visible as long as any
// reasonable width remains at all. padRow itself (tool lines, header line
// 1) keeps the front of its own left text and cuts the tail instead, which
// is the right choice there (the lane name/tool name lead); here the most
// useful fields trail, so the truncation direction has to flip.
func padRowKeepRight(left, right string, width int) string {
	leftW := ansi.StringWidth(left)
	rightW := ansi.StringWidth(right)
	if leftW+rightW+1 > width {
		avail := width - rightW - 1
		if avail < 0 {
			avail = 0
			left = ""
		} else {
			left = ansiTruncateLeftRow(left, avail)
		}
		leftW = ansi.StringWidth(left)
	}
	gap := width - leftW - rightW
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// ctxBarLabel/ctxBarGauge render the header's "ctx NN%  ▓▓░░░░░░░░" pair -
// a ten-cell bar, one ▓ per 10% filled, matching the mock-up exactly (ctx
// 20% -> two filled cells).
func ctxBarLabel(pct int, ok bool) string {
	if !ok {
		return "ctx"
	}
	return fmt.Sprintf("ctx %d%%", pct)
}

func ctxBarGauge(pct int, ok bool) string {
	filled := 0
	if ok {
		filled = pct / 10
		if filled < 0 {
			filled = 0
		}
		if filled > 10 {
			filled = 10
		}
	}
	return strings.Repeat("▓", filled) + strings.Repeat("░", 10-filled)
}

// shortenHome replaces the user's home directory prefix with "~", the same
// shortening a shell prompt applies - "" and paths outside $HOME pass
// through unchanged.
func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// sessionIDStem is the transcript file's own basename, extension stripped,
// truncated to its first 8 characters - "session <id8>" in the header.
func sessionIDStem(transcriptPath string) string {
	if transcriptPath == "" {
		return ""
	}
	stem := strings.TrimSuffix(filepath.Base(transcriptPath), filepath.Ext(transcriptPath))
	r := []rune(stem)
	if len(r) > 8 {
		r = r[:8]
	}
	return string(r)
}

// formatDurationSpaced renders a duration the header's "turn <duration>"
// field wants: "1m 21s", "45s", "1h 2m" - space-separated units, unlike the
// tool line's own tighter formatDurationTight.
func formatDurationSpaced(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) - h*60
	sec := int(d.Seconds()) - h*3600 - m*60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %ds", m, sec)
	default:
		return fmt.Sprintf("%ds", sec)
	}
}

// formatDurationTight renders a tool turn's result duration: "2.1s" under a
// minute (one decimal, matching the mock-up), "4m12s" at or past it -
// tighter than formatDurationSpaced since it sits right-aligned against a
// result word ("exit 0     2.1s") rather than standing alone.
func formatDurationTight(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	d = d.Round(time.Second)
	m := int(d.Minutes())
	sec := int(d.Seconds()) - m*60
	return fmt.Sprintf("%dm%ds", m, sec)
}

// minutesAgo renders the stalled state line's "last write N min ago" -
// against info.Now when set (deterministic in tests), else the real clock.
func minutesAgo(at, now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	mins := int(now.Sub(at).Minutes())
	if mins < 1 {
		mins = 1
	}
	return fmt.Sprintf("%d min", mins)
}

// -- turn rendering -------------------------------------------------------

// buildTurnLines renders every Turn in order (oldest first) into the plain
// lines the turns viewport holds - owner/assistant turns as a header line
// plus wrapped, indented body lines; tool turns as one line each with the
// result right-aligned.
func buildTurnLines(turns []clarity.Turn, width int) []string {
	var lines []string
	for _, t := range turns {
		switch t.Kind {
		case clarity.TurnOwner:
			lines = append(lines, renderProseTurn(t, "YOU", width)...)
		case clarity.TurnAssistant:
			lines = append(lines, renderProseTurn(t, "CLAUDE", width)...)
		case clarity.TurnTool:
			lines = append(lines, renderToolTurn(t, width))
		}
	}
	return lines
}

// renderProseTurn is "hh:mm:ss   YOU|CLAUDE" on its own line, then the
// turn's text collapsed to one paragraph and word-wrapped, each wrapped
// line indented under it (design/cockpit-pane/PANE-MOCKUP-164x45.md's own
// owner/assistant turns, e.g. "18:03:25   YOU" then two indented lines).
func renderProseTurn(t clarity.Turn, label string, width int) []string {
	lines := []string{fmt.Sprintf("%s   %s", t.At.Local().Format("15:04:05"), label)}
	wrapWidth := width - len(sessionTurnIndent)
	if wrapWidth < 10 {
		wrapWidth = 10
	}
	for _, w := range wrapPlain(collapseWS(t.Text), wrapWidth) {
		lines = append(lines, sessionTurnIndent+w)
	}
	return lines
}

// renderToolTurn is "▪ <tool>  <summary>" with the result and duration
// right-aligned to the pane's width in one line, per the mock-up's tool
// rows ("▪ Bash   ...                exit 0     2.1s").
func renderToolTurn(t clarity.Turn, width int) string {
	left := fmt.Sprintf("▪ %s  %s", t.Tool, t.Summary)
	return padRow(left, toolResultLabel(t), width)
}

// toolResultLabel is the tool line's right-hand field: "exit 0     2.1s",
// "running   4m12s", "denied", "error" - the exact four shapes the brief
// names, duration shown only alongside a real (ok/running) outcome.
func toolResultLabel(t clarity.Turn) string {
	dur := formatDurationTight(t.Duration)
	switch t.Result {
	case clarity.ResultOK:
		if dur == "" {
			return "exit 0"
		}
		return "exit 0     " + dur
	case clarity.ResultRunning:
		if dur == "" {
			return "running"
		}
		return "running   " + dur
	case clarity.ResultDenied:
		return "denied"
	case clarity.ResultError:
		return "error"
	default:
		return t.Result
	}
}

// collapseWS folds any run of whitespace (including newlines) into a
// single space, so multi-line prose renders as one wrappable paragraph.
func collapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// wrapPlain greedily wraps words into lines no wider than width - a single
// word longer than width still gets its own (overflowing) line rather than
// being split mid-word.
func wrapPlain(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	lines := make([]string, 0, 4)
	cur := words[0]
	for _, w := range words[1:] {
		if len(cur)+1+len(w) > width {
			lines = append(lines, cur)
			cur = w
		} else {
			cur += " " + w
		}
	}
	return append(lines, cur)
}
