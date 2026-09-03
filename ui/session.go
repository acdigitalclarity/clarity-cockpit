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
	"regexp"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"github.com/charmbracelet/x/ansi"
)

var sessionMutedStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#7A7474"), Dark: lipgloss.Color("#9C9494")})

var sessionTextStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#1a1a1a"), Dark: lipgloss.Color("#dddddd")})

// sessionClaudeStyle is the reading layout's own CLAUDE tag colour
// (SESSION-READING-SPEC.md's colour roles: "the splash's openSkies teal",
// ui/splash/render.go:34's Dark value; Light is a new, darker-for-light-bg
// variant since openSkies itself is a dark-ground colour with no light-mode
// counterpart yet defined).
var sessionClaudeStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#0f7f83"), Dark: lipgloss.Color("#54E6EA")})

// sessionRuleStyle is the reading layout's own dimmer-than-muted divider
// colour (SESSION-READING-SPEC.md: "the pair at ui/menu.go:25-26" - menu.go's
// own sepStyle values, repeated here rather than exported from menu.go so
// this file's colour roles stay self-contained) - dim enough that a rule
// never competes with a tag line for attention.
var sessionRuleStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#DDDADA"), Dark: lipgloss.Color("#3C3C3C")})

// sessionFixedBottomRows is the row cost of the bottom rule, the state
// line and the three-line composer box - always reserved, regardless of how
// much of the turns region above them is used (the "newest content pinned
// to the bottom" requirement: these always sit at the pane's own bottom
// edge).
const sessionFixedBottomRows = 5

// sessionWideMinWidth is the pane-inner-width (the value SetSize actually
// receives) threshold above which the reading layout's 1-column-each-side
// padding and 2-column turn gutter apply (SESSION-READING-SPEC.md's own
// rule of thumb: "gutter = 2 at 140 columns or wider, else 1" - stated
// against the outer terminal width, translated here to this package's own
// received width: a 140-column terminal's arithmetic - listWidthForTerminal
// clamp(round(140*0.28),38,52)=39, tabsWidth=101, minus the pane's 2-column
// border - lands on exactly 99).
const sessionWideMinWidth = 99

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

	// turnsSig is the previous SetInfo call's own sessionTurnsSignature -
	// SetInfo skips rebuilding the turns viewport (refreshViewport) when the
	// freshly read info's signature matches it exactly, the FINISH
	// requirement's "no flicker (only re-render when the tail changed or
	// the elapsed second changed)": a 500ms tick on an idle lane reads the
	// identical LaneTail back from the cache every time (one stat, no
	// reparse - LaneTailCache's own contract) and there is nothing new to
	// draw. The header/state lines are unaffected either way - they render
	// straight from s.info fresh on every String() call, never cached.
	turnsSig sessionTurnsSignature

	live, waiting int // fleet counters for the resting frame (splash.FleetCounts)

	// spinnerFrame is the header glyph's and the thinking line's own 100ms
	// animation counter (slice 14, rule 1: "decoupled from the read") -
	// advanced by TickSpinner only, never by SetInfo, so a smooth spinner
	// never depends on - and never triggers - a transcript read or a turns
	// rebuild (rule 4's "the spinner frame alone does not rebuild the
	// turns"). Read fresh by String() every render, same as the rest of
	// s.info's own header fields.
	spinnerFrame int

	viewport viewport.Model // the scrollable turns region only, not the whole pane

	// lineMeta is buildTurnLines' own per-line bookkeeping, in lock-step with
	// the lines last handed to viewport.SetContent: which turn each rendered
	// line belongs to, and - for a prose turn's own continuation lines only,
	// never its first/label line - that turn's tag. String() reads
	// lineMeta[viewport.YOffset()] to draw the sticky "⋯ continued" header
	// (SESSION-READING-SPEC.md) whenever a scroll lands mid-turn.
	lineMeta []sessionTurnLineTag

	// composer is the shared inline message box (slice 5) - app.go owns
	// the single instance and wires it into both this pane and NeedsYouPane
	// via SetComposer, since only one row can be the current send target.
	composer *Composer
}

// pad is the reading layout's own left/right inset inside the pane's
// received width - 1 column each side at the wide (>= sessionWideMinWidth)
// size, 0 at the narrow size (SESSION-READING-SPEC.md's "Pane padding 1
// column ... pane padding 0" pair).
func (s *SessionPane) pad() int {
	if s.width >= sessionWideMinWidth {
		return 1
	}
	return 0
}

// gutter is the turn body's own hanging indent under its tag/time label
// line - 2 columns wide, 1 narrow (SESSION-READING-SPEC.md's "Turn gutter
// 2" / "gutter 1" pair, and its own rule of thumb: "gutter = 2 at 140
// columns or wider, else 1").
func (s *SessionPane) gutter() int {
	if s.width >= sessionWideMinWidth {
		return 2
	}
	return 1
}

// contentWidth is the chrome's own working width (header lines, rules,
// state line, composer box, and the turns viewport itself all render at
// this width) - the pane's received width minus its own padding on both
// sides.
func (s *SessionPane) contentWidth() int {
	cw := s.width - 2*s.pad()
	if cw < 1 {
		cw = 1
	}
	return cw
}

// measure is the prose wrap width - min(96, content - gutter), the rule of
// thumb SESSION-READING-SPEC.md's geometry section names verbatim.
func (s *SessionPane) measure() int {
	m := s.contentWidth() - s.gutter()
	if m > 96 {
		m = 96
	}
	if m < 1 {
		m = 1
	}
	return m
}

// NewSessionPane returns an empty SessionPane - SetSize and SetInfo (or
// Clear) still need calling before String() shows anything meaningful,
// same contract as PreviewPane/TerminalPane/DiffPane.
func NewSessionPane() *SessionPane {
	return &SessionPane{viewport: viewport.New(), composer: NewComposer()}
}

// SetComposer wires the shared Composer app.go owns into this pane's own
// render - called once at construction, not per-tick.
func (s *SessionPane) SetComposer(c *Composer) {
	s.composer = c
}

// SetSize sets the pane's own content dimensions (the tabbed window's
// contentWidth/contentHeight - the same size every tab receives).
func (s *SessionPane) SetSize(width, height int) {
	s.width, s.height = width, height
	s.refreshViewport()
}

// SetInfo replaces the SELECTED lane's data and rebuilds the turns
// viewport, pinned to the bottom (the newest turn is always visible on the
// first render after a change) - but only when there is actually something
// new to draw (see turnsSig's own doc comment). nil clears the selection -
// String() then shows the resting frame.
func (s *SessionPane) SetInfo(info *SessionInfo) {
	sig := newSessionTurnsSignature(info)
	rebuild := info == nil || s.info == nil || sig != s.turnsSig
	s.info = info
	s.turnsSig = sig
	if rebuild {
		s.refreshViewport()
	}
}

// sessionTurnsSignature is the subset of a SessionInfo that determines
// whether the turns viewport has anything new to draw: the transcript's own
// identity and last-write instant, its message count, its turn count, and -
// the part that changes fastest - the RENDERED elapsed text of every still-
// running tool turn (toolResultLabel's own formatting, not a raw duration
// bucket, so a signature match means the text truly would not have changed
// either). Two SessionInfo values that produce an equal signature would
// render byte-identical turn lines.
type sessionTurnsSignature struct {
	transcript string
	lastWrite  time.Time
	messages   int
	turnCount  int
	running    string
}

func newSessionTurnsSignature(info *SessionInfo) sessionTurnsSignature {
	if info == nil {
		return sessionTurnsSignature{}
	}
	var running strings.Builder
	for i, t := range info.Tail.Turns {
		if t.Kind == clarity.TurnTool && t.Result == clarity.ResultRunning {
			fmt.Fprintf(&running, "|%d:%s", i, formatDurationTight(info.Now.Sub(t.At)))
		}
	}
	return sessionTurnsSignature{
		transcript: info.Tail.Transcript,
		lastWrite:  info.Tail.LastWrite,
		messages:   info.Tail.Messages,
		turnCount:  len(info.Tail.Turns),
		running:    running.String(),
	}
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

// TickSpinner advances the header/thinking-line spinner by one frame - a
// bare counter increment, called once per app.go's 100ms animation tick
// (slice 14 rule 1). It never touches s.info, s.turnsSig or the viewport,
// so it never triggers refreshViewport - the spinner's own smoothness is
// entirely independent of when the transcript was last actually read.
func (s *SessionPane) TickSpinner() {
	s.spinnerFrame++
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

	s.viewport.SetWidth(s.contentWidth())
	s.viewport.SetHeight(s.turnsAreaHeight())
	if s.info == nil {
		s.viewport.SetContent("")
		s.lineMeta = nil
		return
	}
	lines, meta := buildTurnLines(s.info.Tail.Turns, s.gutter(), s.measure(), s.info.Now)
	s.lineMeta = meta
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
		// DEFECT (slice 5's own scratchfix proof, PROOF (b)): a tracked
		// instance whose program keeps no Claude transcript at all (the
		// proof's own `cat`-backed lane; equally any non-Claude program)
		// never has a SessionInfo to show - selectedSessionInfo returns nil
		// for it exactly as it does for "nothing selected" at all. Without
		// this branch the composer, already open and holding its own
		// captured target (Composer.Open captures lane/isExternal
		// independently of SessionInfo), would silently never render: m
		// would flip m.state to stateMsg and the composer's data model
		// would update, but the pane kept returning the bare resting frame
		// with no box drawn at all. The resting frame still shows (never a
		// placeholder line, per DECISIONS.md's own "no placeholder text"
		// rule) - only its own foot rows now make room for the composer
		// whenever it has something to show.
		if s.composer.IsOpen() || s.composer.HasResult() {
			return s.renderRestingWithComposer()
		}
		return s.renderResting()
	}

	lines := make([]string, 0, s.height)
	lines = append(lines, s.renderHeaderLine1(), s.renderHeaderLine2(), s.rule())
	if s.info.Tail.Truncated {
		lines = append(lines, s.renderEarlierLine())
	}

	turnsHeight := s.turnsAreaHeight()
	// The thinking line (rule 2) is drawn fresh every String() call, never
	// stored in the viewport's own cached content (turnsSig/refreshViewport)
	// - it steals one row off the BOTTOM of the turns region for itself, so
	// its own spinner frame changing every 100ms never needs (and never
	// triggers) a viewport content rebuild (rule 4).
	showThinking := s.thinkingLineVisible() && turnsHeight > 0
	viewportHeight := turnsHeight
	if showThinking {
		viewportHeight--
	}
	s.viewport.SetHeight(viewportHeight)
	s.viewport.SetWidth(s.contentWidth())
	body := strings.Split(s.viewport.View(), "\n")
	if viewportHeight <= 0 {
		body = nil
	} else if len(body) < viewportHeight {
		body = append(body, make([]string, viewportHeight-len(body))...)
	} else if len(body) > viewportHeight {
		body = body[:viewportHeight]
	}
	// Sticky continued header (SESSION-READING-SPEC.md): when the viewport's
	// own top visible row is a prose turn's CONTINUATION (never its own
	// first/label line - lineMeta's own tag is "" there), that row is
	// replaced by the scrolled-off turn's own tag, so a scrolled read always
	// names its speaker.
	if len(body) > 0 {
		if off := s.viewport.YOffset(); off >= 0 && off < len(s.lineMeta) {
			if tag := s.lineMeta[off].tag; tag != "" {
				body[0] = fitRow(renderContinuedLabel(tag), s.contentWidth())
			}
		}
	}
	if showThinking {
		body = append(body, s.renderThinkingLine())
	}
	lines = append(lines, body...)

	lines = append(lines, s.rule(), s.renderStateLine())
	lines = append(lines, s.renderComposerLines()...)

	if len(lines) > s.height {
		lines = lines[:s.height]
	} else if len(lines) < s.height {
		lines = append(lines, make([]string, s.height-len(lines))...)
	}
	pad := strings.Repeat(" ", s.pad())
	for i, l := range lines {
		lines[i] = fitRow(pad+l+pad, s.width)
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

// renderRestingWithComposer is renderResting's own layout, shrunk to leave
// room for the composer's fixed 3-line box at the foot - the "no
// SessionInfo, but the composer is engaged" case (see String()'s own
// comment). lane is passed as "" since a composer reaching this branch is
// always open or holding a result, both of which render from their own
// captured target (Composer.Render's own doc comment), never the fallback
// argument.
func (s *SessionPane) renderRestingWithComposer() string {
	composerLines := s.composer.Render(s.width, "")
	restHeight := s.height - len(composerLines)
	if restHeight < 0 {
		restHeight = 0
	}

	frame := splash.RenderFrame(s.width, restHeight, -1, -1, s.live, s.waiting)
	if !fitsBox(frame, s.width, restHeight) {
		frame = FallbackMark(s.width)
	}
	rest := lipgloss.Place(s.width, restHeight, lipgloss.Center, lipgloss.Center, frame)

	lines := strings.Split(rest, "\n")
	lines = append(lines, composerLines...)
	for i, l := range lines {
		lines[i] = fitRow(l, s.width)
	}
	return strings.Join(lines, "\n")
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

// spinnerFrames is the header's and the thinking line's own braille cycle
// (slice 14 rule 1, "the Claude Code look" - the owner's own reference,
// 3 Sep 11:0x), one frame per 100ms animation tick (TickSpinner), shown
// only while a turn is genuinely open. Replaces the old four-glyph
// animGlyphFrames cycle (retired, slice 14): that cycle advanced once per
// 500ms session tick and looked "jerky" (the owner's own word) for exactly
// that reason.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// headerGlyph is renderHeaderLine1's own glyph: laneStateGlyph's static
// glyph for every state except an OPEN working turn (State working, Tail.
// OpenTurn true), which instead cycles spinnerFrames by s.spinnerFrame -
// the header IS the loading indicator while work is genuinely in flight,
// and settles back to the plain "●" the instant ClassifyState reports the
// turn closed (OpenTurn false), same tick. s.spinnerFrame advances on its
// own 100ms tick (TickSpinner), never on the transcript's own read cadence,
// so the animation is smooth regardless of how often the lane's data is
// actually re-read.
func (s *SessionPane) headerGlyph() (string, lipgloss.Style) {
	t := s.info.Tail
	if t.State == clarity.StateWorking && t.OpenTurn {
		frame := spinnerFrames[s.spinnerFrame%len(spinnerFrames)]
		return frame, laneStateAccentStyle
	}
	return laneStateGlyph(t.State)
}

// lastTurnIsRunningTool reports whether turns' own last (most recently
// appended, i.e. most recent) entry is an unmatched tool_use - the Latency
// ruling's own "a tool line IS running" test (buildTurns appends a tool
// Turn at its tool_use record's own position, which stays put even once a
// later tool_result resolves it in place - so an unresolved call is always
// the list's own last entry for as long as it stays open).
func lastTurnIsRunningTool(turns []clarity.Turn) bool {
	if len(turns) == 0 {
		return false
	}
	last := turns[len(turns)-1]
	return last.Kind == clarity.TurnTool && last.Result == clarity.ResultRunning
}

// thinkingLineVisible is rule 2's own gate: the selected lane's turn is
// open, State is genuinely Working (never Stalled - a stalled turn gets its
// own state-line clause instead, never this footer), and no tool line is
// currently running (a running tool's own "running <elapsed>" is already
// the indicator - showing both would say the same thing twice).
func (s *SessionPane) thinkingLineVisible() bool {
	t := s.info.Tail
	if t.State != clarity.StateWorking || !t.OpenTurn {
		return false
	}
	return !lastTurnIsRunningTool(t.Turns)
}

// formatElapsedShort renders the thinking line's own "since the last
// timestamped record" clock: whole seconds under a minute ("12s"), whole
// minutes beyond that ("3m") - session/clarity/tail.go's roundAge shape,
// duplicated here since that helper is private to its own package and this
// is the only other place that needs it.
func formatElapsedShort(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

// renderThinkingLine draws rule 2's own foot line: the spinner (accent)
// followed by "thinking · <elapsed>" (muted) - elapsed measured from
// Tail.LastTurn, the last TIMESTAMPED record ClassifyState anchored its
// open-turn read on, against s.info.Now (deterministic in tests, same
// convention as minutesAgo/renderStateLine).
func (s *SessionPane) renderThinkingLine() string {
	t := s.info.Tail
	frame := laneStateAccentStyle.Render(spinnerFrames[s.spinnerFrame%len(spinnerFrames)])
	rest := sessionMutedStyle.Render(fmt.Sprintf(" thinking · %s", formatElapsedShort(s.info.Now.Sub(t.LastTurn))))
	return fitRow(frame+rest, s.contentWidth())
}

// renderHeaderLine1 is "<lane>  ...  <glyph> <state>[ · N agents]   ctx
// NN%  <bar>   last write hh:mm:ss" - the lane name left, everything else
// right-aligned to the pane's own width (design/cockpit-pane/
// PANE-MOCKUP-164x45.md line 1).
func (s *SessionPane) renderHeaderLine1() string {
	t := s.info.Tail
	glyph, style := s.headerGlyph()
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
	cw := s.contentWidth()
	for _, right := range candidates {
		if ansi.StringWidth(s.info.Lane)+ansi.StringWidth(right)+1 <= cw {
			return sessionTextStyle.Render(padRow(s.info.Lane, right, cw))
		}
	}
	return sessionTextStyle.Render(padRow(s.info.Lane, candidates[len(candidates)-1], cw))
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

	return sessionMutedStyle.Render(padRowKeepRight(strings.Join(left, " · "), right, s.contentWidth()))
}

// rule is a full-content-width horizontal divider, dim (colour role: NEW
// sessionRuleStyle, dimmer than sessionMutedStyle so a rule never competes
// with a tag line).
func (s *SessionPane) rule() string {
	return muteRule(s.contentWidth())
}

// muteRule is a full-width horizontal divider, dim - shared by SessionPane
// and NeedsYouPane (needsyou.go) so the two tabs' dividers never drift out
// of style.
func muteRule(width int) string {
	return sessionRuleStyle.Render(strings.Repeat("─", width))
}

// renderEarlierLine is "⋯ earlier in this session · N messages · shift+↑ to
// scroll back", shown only when info.Tail.Truncated.
func (s *SessionPane) renderEarlierLine() string {
	text := fmt.Sprintf("⋯  earlier in this session · %d messages · shift+↑ to scroll back", s.info.Tail.Messages)
	return sessionMutedStyle.Render(ansiTruncateRow(text, s.contentWidth()))
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

// renderComposerLines draws the composer box exactly as the mock-up
// (design/cockpit-pane/PANE-MOCKUP-164x45.md lines 38-40) - WIRED (slice
// 5): the shared Composer's own Render, targeted at this pane's selected
// lane whenever the composer is not already open on a captured target.
func (s *SessionPane) renderComposerLines() []string {
	return s.composer.Render(s.contentWidth(), s.info.Lane)
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

// sessionTurnLineTag is buildTurnLines' own per-rendered-line bookkeeping -
// see SessionPane.lineMeta's own doc comment.
type sessionTurnLineTag struct {
	turnIdx int
	tag     string // "" unless this line is a prose turn's own CONTINUATION
}

// buildTurnLines renders every Turn in order (oldest first) into the plain,
// already-styled lines the turns viewport holds: owner/assistant turns as a
// tag/time label line plus paragraph/list-aware wrapped body (rule 1), one
// blank line between turns and never inside one (SESSION-READING-SPEC.md's
// Spacing section); tool turns as one line each, their own result styled by
// outcome and right-aligned to the measure's own right edge. meta is
// returned in lock-step with lines - see SessionPane.lineMeta.
func buildTurnLines(turns []clarity.Turn, gutter, measure int, now time.Time) (lines []string, meta []sessionTurnLineTag) {
	for i, t := range turns {
		if i > 0 {
			lines = append(lines, "")
			meta = append(meta, sessionTurnLineTag{turnIdx: i})
		}
		var turnLines []string
		var tag string
		switch t.Kind {
		case clarity.TurnOwner:
			tag = "YOU"
			turnLines = renderProseTurn(t, tag, needsYouTitleStyle, gutter, measure)
		case clarity.TurnAssistant:
			tag = "CLAUDE"
			turnLines = renderProseTurn(t, tag, sessionClaudeStyle, gutter, measure)
		case clarity.TurnTool:
			turnLines = []string{renderToolTurn(t, gutter, measure, now)}
		}
		for j, l := range turnLines {
			lines = append(lines, l)
			m := sessionTurnLineTag{turnIdx: i}
			if j > 0 {
				m.tag = tag
			}
			meta = append(meta, m)
		}
	}
	return lines, meta
}

// renderContinuedLabel is the sticky top-of-viewport marker String() swaps
// in for lineMeta's own continuation rows: "<TAG>  ⋯ continued" in the
// tag's own colour, at content offset 0 like every real label line.
func renderContinuedLabel(tag string) string {
	style := needsYouTitleStyle
	if tag == "CLAUDE" {
		style = sessionClaudeStyle
	}
	return style.Render(fmt.Sprintf("%-7s  ⋯ continued", tag))
}

// renderProseTurn renders one owner/assistant turn: the "%-7s  %s" tag/time
// label line (SESSION-READING-SPEC.md's own format string - tag first so
// YOU and CLAUDE's times land in one column), then the turn's own text
// preserving paragraphs and list items (rule 1 - collapseWS folding them
// into one paragraph was the defect this replaces), word-wrapped to measure
// with a gutter-wide hanging indent under each line, list items hanging
// under their own marker instead.
func renderProseTurn(t clarity.Turn, tag string, tagStyle lipgloss.Style, gutter, measure int) []string {
	label := tagStyle.Render(fmt.Sprintf("%-7s  ", tag)) + sessionMutedStyle.Render(t.At.Local().Format("15:04:05"))
	lines := []string{label}

	indent := strings.Repeat(" ", gutter)
	for _, block := range splitProseBlocks(t.Text) {
		markerWidth := len([]rune(block.marker))
		wrapWidth := measure - gutter - markerWidth
		if wrapWidth < 10 {
			wrapWidth = 10
		}
		for i, wline := range wrapTokens(tokenizeMarkdown(block.text), wrapWidth) {
			prefix := indent
			switch {
			case block.marker == "":
				// plain paragraph, gutter indent only
			case i == 0:
				prefix = indent + block.marker
			default:
				prefix = indent + strings.Repeat(" ", markerWidth)
			}
			lines = append(lines, prefix+renderTokenLine(wline))
		}
	}
	return lines
}

// proseBlock is one paragraph or list item split out of a turn's raw text -
// see splitProseBlocks.
type proseBlock struct {
	marker string // "" for a plain paragraph, else "- ", "* " or "N. " as rendered
	text   string
}

// orderedListMarkerRe matches an ordered-list source line's own "N. " lead.
var orderedListMarkerRe = regexp.MustCompile(`^(\d+)\.\s+(.*)$`)

// listMarker reports whether line opens a list item (SESSION-READING-
// SPEC.md: "a source line opening '- ', '* ' or 'N. ' becomes a list
// item"), returning the marker exactly as rendered and the remainder.
func listMarker(line string) (marker, rest string, ok bool) {
	switch {
	case strings.HasPrefix(line, "- "):
		return "- ", strings.TrimPrefix(line, "- "), true
	case strings.HasPrefix(line, "* "):
		return "* ", strings.TrimPrefix(line, "* "), true
	}
	if m := orderedListMarkerRe.FindStringSubmatch(line); m != nil {
		return m[1] + ". ", m[2], true
	}
	return "", "", false
}

// splitProseBlocks walks text's own source lines - never collapsed to one
// paragraph (rule 1) - into paragraph/list-item blocks: a blank source line
// ends the current block, and a list-marker line always starts a new one
// even without a blank line before it. Any other non-blank line joins the
// CURRENT block with a single space, so a paragraph or item hard-wrapped in
// the source still renders as one flowing, re-wrappable unit.
func splitProseBlocks(text string) []proseBlock {
	var blocks []proseBlock
	var cur *proseBlock
	flush := func() {
		if cur != nil && strings.TrimSpace(cur.text) != "" {
			blocks = append(blocks, *cur)
		}
		cur = nil
	}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			flush()
			continue
		}
		if marker, rest, ok := listMarker(line); ok {
			flush()
			cur = &proseBlock{marker: marker, text: rest}
			continue
		}
		if cur == nil {
			cur = &proseBlock{text: line}
		} else {
			cur.text += " " + line
		}
	}
	flush()
	return blocks
}

// proseToken is one word of a rendered block, carrying whether it fell
// inside a "**bold**" span.
type proseToken struct {
	text string
	bold bool
}

// tokenizeMarkdown strips "**", "__" and backtick markers from s and splits
// what remains into words, marking each word bold if it fell inside a
// "**...**" span (SESSION-READING-SPEC.md: "strip markdown emphasis and
// code markers ... apply bold to what ** wrapped") - no marker byte
// survives into a token's own text (rule 1's "never a literal asterisk pair
// in the pane").
func tokenizeMarkdown(s string) []proseToken {
	var out []proseToken
	bold := false
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, proseToken{text: buf.String(), bold: bold})
			buf.Reset()
		}
	}
	r := []rune(s)
	for i := 0; i < len(r); i++ {
		switch {
		case r[i] == '*' && i+1 < len(r) && r[i+1] == '*':
			flush()
			bold = !bold
			i++
		case r[i] == '_' && i+1 < len(r) && r[i+1] == '_':
			flush()
			i++
		case r[i] == '`':
			// stripped, no style change
		case unicode.IsSpace(r[i]):
			flush()
		default:
			buf.WriteRune(r[i])
		}
	}
	flush()
	return out
}

// wrapTokens greedily wraps tokens into lines no wider than width, at word
// boundaries - a single word longer than width still gets its own
// (overflowing) line, never split mid-word (mirrors wrapPlain's own rule).
func wrapTokens(tokens []proseToken, width int) [][]proseToken {
	if len(tokens) == 0 {
		return nil
	}
	lines := make([][]proseToken, 0, 4)
	cur := []proseToken{tokens[0]}
	curLen := len([]rune(tokens[0].text))
	for _, tok := range tokens[1:] {
		wlen := len([]rune(tok.text))
		if curLen+1+wlen > width {
			lines = append(lines, cur)
			cur = []proseToken{tok}
			curLen = wlen
		} else {
			cur = append(cur, tok)
			curLen += 1 + wlen
		}
	}
	return append(lines, cur)
}

// renderTokenLine joins one wrapped line's tokens back into a styled
// string - sessionTextStyle for plain words, bold for a "**...**" span,
// never a second hue (colour role rule 2). Runs of CONSECUTIVE same-bold
// tokens are rendered in one Style.Render call (their own words plain-space
// joined first), not one call per word: a per-word Render would interleave
// an ANSI escape between every word, and a caller reading the rendered
// text back for a literal phrase (a live turn's own words, or a test
// fixture's) would never find it as one contiguous substring.
func renderTokenLine(tokens []proseToken) string {
	var b strings.Builder
	for i := 0; i < len(tokens); {
		bold := tokens[i].bold
		j := i
		var words []string
		for j < len(tokens) && tokens[j].bold == bold {
			words = append(words, tokens[j].text)
			j++
		}
		if i > 0 {
			b.WriteString(" ")
		}
		style := sessionTextStyle
		if bold {
			style = style.Bold(true)
		}
		b.WriteString(style.Render(strings.Join(words, " ")))
		i = j
	}
	return b.String()
}

// toolIndentFor returns the tool line's own content offset: 2 further than
// the turn gutter at the wide (gutter 2) size, so a tool line visibly nests
// under its turn's prose; the narrow (gutter 1) size has no room to spare
// and nests at the gutter itself (SESSION-READING-SPEC.md: "Tool lines
// indent 2 further (content offset 4)" at 164x45 vs "Tool indent 1
// (gutter + 0)" at 120x36).
func toolIndentFor(gutter int) int {
	if gutter >= 2 {
		return gutter + 2
	}
	return gutter
}

// toolSummaryCap is the Truncation section's own two named figures - 66 at
// measure 96/gutter 2, 50 at measure 79/gutter 1 (SESSION-READING-SPEC.md's
// own arithmetic for the narrow case: "measure - 1 - 2 - 8 - 16 - 2 = 50").
func toolSummaryCap(gutter, rowWidth int) int {
	capWidth := rowWidth - 28 // "▪ " (2) + tool name (8) + gap (2) + result block (16)
	if gutter == 1 {
		capWidth-- // SESSION-READING-SPEC.md 120x36's own extra "-1" term
	}
	if capWidth < 1 {
		capWidth = 1
	}
	return capWidth
}

// renderToolTurn is the tool row: "▪ <tool>  <summary>" left, the result/
// duration right-aligned to the measure's own right edge, indented
// toolIndentFor(gutter) columns so it nests under its turn's prose
// (SESSION-READING-SPEC.md's Truncation section: tool name capped to 8
// columns, summary capped per toolSummaryCap, the result block ending at
// the measure's own right edge).
func renderToolTurn(t clarity.Turn, gutter, measure int, now time.Time) string {
	indent := toolIndentFor(gutter)
	rowWidth := gutter + measure - indent
	if rowWidth < 20 {
		rowWidth = 20
	}

	name := t.Tool
	if r := []rune(name); len(r) > 8 {
		name = string(r[:7]) + "…"
	}
	summary := t.Summary
	if capWidth := toolSummaryCap(gutter, rowWidth); len([]rune(summary)) > capWidth {
		if r := []rune(summary); capWidth > 1 {
			summary = string(r[:capWidth-1]) + "…"
		} else {
			summary = "…"
		}
	}

	left := sessionMutedStyle.Render(fmt.Sprintf("▪ %-8s%s", name, summary))
	right := toolResultStyled(t, now)
	return strings.Repeat(" ", indent) + padRow(left, right, rowWidth)
}

// toolResultLabel is the tool line's right-hand PLAIN text: "exit 0
// 2.1s", "running   4m12s", "denied", "error" - the exact four shapes the
// brief names, duration shown only alongside a real (ok/running) outcome. A
// ResultRunning turn (the Latency ruling's own case: an unmatched tool_use,
// no tool_result yet) never carries a stored Duration - buildTurns only
// ever sets that from a matched outcome - so its elapsed time is computed
// HERE, live, from now (the session tick's own clock) minus the tool's own
// timestamp: "running <elapsed>", counting up on every 500ms tick rather
// than being frozen at whatever it read when the file was last (re)parsed.
func toolResultLabel(t clarity.Turn, now time.Time) string {
	switch t.Result {
	case clarity.ResultOK:
		dur := formatDurationTight(t.Duration)
		if dur == "" {
			return "exit 0"
		}
		return "exit 0     " + dur
	case clarity.ResultRunning:
		elapsed := now.Sub(t.At)
		if elapsed < 0 {
			elapsed = 0
		}
		dur := formatDurationTight(elapsed)
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

// toolResultStyled is toolResultLabel's own text, styled by outcome
// (colour roles): ok is the default and never earns colour, running
// borrows the lane rows' own amber "moving" tint (needsYouTitleStyle,
// unbolded), error/denied get the removed-lines red.
func toolResultStyled(t clarity.Turn, now time.Time) string {
	text := toolResultLabel(t, now)
	switch t.Result {
	case clarity.ResultRunning:
		return needsYouTitleStyle.Bold(false).Render(text)
	case clarity.ResultDenied, clarity.ResultError:
		return removedLinesStyle.Render(text)
	default:
		return sessionMutedStyle.Render(text)
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
