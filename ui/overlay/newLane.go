// Package overlay: this file is the three-step "n" dialog FRONTDOOR-
// SPEC.md's "The overlay: three steps, one box, existing primitives only"
// describes - name, seat, modality, one 84-column box that keeps its own
// frame across all three steps. It owns its own step state, the typed name,
// the seat/modality cursors and the autodetect result; app.go owns
// everything the box does not decide for itself - starting the instance,
// saving to storage, cancelling the whole flow on ctrl-c.
//
// Every fixed label, column width and border shape below is measured
// directly off design/cockpit-pane/FRONTDOOR-MOCKUP-164x45.md screens 1-3 -
// the drawings are the bar, matched column for column, not re-imagined.
package overlay

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// NewLaneStep is one of the three steps the overlay walks through, in
// order.
type NewLaneStep int

const (
	NewLaneStepName NewLaneStep = iota
	NewLaneStepAccount
	NewLaneStepModality
)

// newLaneNameMaxWidth mirrors app.go's own pre-slice-6 stateNew rule
// (today's two rules, unchanged per FRONTDOOR-SPEC.md "Step 1 name"): a
// lane name must fit in 32 display columns.
const newLaneNameMaxWidth = 32

// Box geometry, measured off the mock-up: the outer box is 84 columns wide
// including its own corners (cols 64-147 of a 164-wide screen, centred over
// the 118-wide pane at list-width 46); the step-1 name field is a further
// nested box, 74 columns wide inside the outer box's own 3-space indent.
const (
	newLaneWidth      = 84
	newLaneInnerWidth = newLaneWidth - 2
	newLaneFieldWidth = 74
)

// NewLaneAccountRow is one row of the step-2 seat picker.
type NewLaneAccountRow struct {
	Tag       string
	ConfigDir string
	// CredentialStore is presence only (F2/F3) - never a read of any value
	// inside the seat's own .claude.json.
	CredentialStore bool
	// FillPct/HasLiveLane are the seat's own window figure: the maximum
	// context fill across every lane currently on this seat, or
	// HasLiveLane=false ("idle") when the seat has no live lane at all -
	// never a stale or wrong-lane number.
	FillPct     int
	HasLiveLane bool
	// IsDefault marks the registry policy's own default_account.
	IsDefault       bool
	DefaultModality string
}

// NewLaneModalityRow is one of the five fixed modality rows (step 3).
type NewLaneModalityRow struct {
	Key   string
	Label string
	Desc  string
}

// NewLaneModalities is the fixed five-row list every new-lane overlay's
// step 3 picker walks - never built dynamically. Order and wording match
// FRONTDOOR-MOCKUP-164x45.md screen 3 exactly, including the "general"
// modality's own display label in this workspace ("ways of working").
var NewLaneModalities = []NewLaneModalityRow{
	{Key: "app-pipeline", Label: "app pipeline", Desc: "forge build through the gated state machine"},
	{Key: "project", Label: "project", Desc: "a client engagement in work/"},
	{Key: "enhancement", Label: "enhancement", Desc: "a repo's own backlog"},
	{Key: "bid", Label: "bid", Desc: "an RFI / RFP / ITT response"},
	{Key: "general", Label: "ways of working", Desc: "the fleet's own rules and machinery"},
}

// NewLaneOverlay is the three-step "n" dialog's own model and renderer.
type NewLaneOverlay struct {
	step NewLaneStep
	name string

	sessionsRoot  string
	forgeAppsRoot string

	accounts      []NewLaneAccountRow
	accountCursor int

	modalityCursor int
	detectedRule   string // "" -> "no rule fired"; else "N2, the name ends -bid" and similar
}

// NewNewLaneOverlay builds the overlay at step 1, pre-selecting step 2's
// cursor at the registry policy's own default account (FRONTDOOR-SPEC.md:
// "since step 3 comes after, pre-select the policy default_account and
// re-order nothing").
func NewNewLaneOverlay(sessionsRoot, forgeAppsRoot string, accounts []NewLaneAccountRow, defaultAccountTag string) *NewLaneOverlay {
	cursor := 0
	for i, a := range accounts {
		if a.Tag == defaultAccountTag {
			cursor = i
			break
		}
	}
	return &NewLaneOverlay{
		step:          NewLaneStepName,
		sessionsRoot:  sessionsRoot,
		forgeAppsRoot: forgeAppsRoot,
		accounts:      accounts,
		accountCursor: cursor,
	}
}

// Step returns the step currently shown.
func (o *NewLaneOverlay) Step() NewLaneStep { return o.step }

// Name returns the typed lane name.
func (o *NewLaneOverlay) Name() string { return o.name }

// TypeRune appends text to the name field, refusing to grow it past
// newLaneNameMaxWidth display columns - the same guard app.go's own
// stateNew has always applied, checked before the character is added so the
// name can reach exactly 32 columns but never more.
func (o *NewLaneOverlay) TypeRune(text string) error {
	if runewidth.StringWidth(o.name) >= newLaneNameMaxWidth {
		return fmt.Errorf("title cannot be longer than %d characters", newLaneNameMaxWidth)
	}
	o.name += text
	return nil
}

// Backspace removes the last rune of the typed name, a no-op on an empty
// name.
func (o *NewLaneOverlay) Backspace() {
	runes := []rune(o.name)
	if len(runes) == 0 {
		return
	}
	o.name = string(runes[:len(runes)-1])
}

// ValidateName is step 1's submit-time rule: non-empty (the width rule is
// already enforced on every keystroke by TypeRune above, so a stored name
// can never arrive here over width).
func (o *NewLaneOverlay) ValidateName() error {
	if strings.TrimSpace(o.name) == "" {
		return fmt.Errorf("title cannot be empty")
	}
	return nil
}

// FolderPath is step 1's own preview line: the sessions root plus the typed
// name, exactly what `clarity new` would create.
func (o *NewLaneOverlay) FolderPath() string {
	return filepath.Join(o.sessionsRoot, o.name)
}

// NextFromName validates the name and advances to step 2. Call only after
// ValidateName has already been checked by the caller (app.go), which
// decides how a validation failure is shown (handleError's own footer).
func (o *NewLaneOverlay) NextFromName() {
	o.step = NewLaneStepAccount
}

// BackToName returns to step 1, keeping the typed name.
func (o *NewLaneOverlay) BackToName() {
	o.step = NewLaneStepName
}

// SelectedAccount returns the seat currently highlighted at step 2, or the
// zero value when the registry named no accounts at all.
func (o *NewLaneOverlay) SelectedAccount() NewLaneAccountRow {
	if o.accountCursor < 0 || o.accountCursor >= len(o.accounts) {
		return NewLaneAccountRow{}
	}
	return o.accounts[o.accountCursor]
}

// NextFromAccount advances to step 3, running the autodetect ladder against
// the typed name and the now-chosen seat.
func (o *NewLaneOverlay) NextFromAccount() {
	o.step = NewLaneStepModality
	key, rule := o.autodetect()
	o.detectedRule = rule
	o.modalityCursor = 0
	for i, m := range NewLaneModalities {
		if m.Key == key {
			o.modalityCursor = i
			break
		}
	}
}

// DefaultModalityKey runs the same autodetect ladder NextFromAccount uses,
// without advancing the step or moving the modality cursor - front-door
// slice 7's "l" key needs step 3's own default (item 2, "modality from
// step 3 defaults") while the overlay never actually leaves step 2.
func (o *NewLaneOverlay) DefaultModalityKey() string {
	key, _ := o.autodetect()
	return key
}

// BackToAccount returns to step 2, keeping the chosen seat.
func (o *NewLaneOverlay) BackToAccount() {
	o.step = NewLaneStepAccount
}

// SelectedModality returns the modality row currently highlighted at step 3.
func (o *NewLaneOverlay) SelectedModality() NewLaneModalityRow {
	return NewLaneModalities[o.modalityCursor]
}

// DetectedRule is the autodetect result named on step 3's own "Detected"
// line - "" means no rule fired (the general/else floor).
func (o *NewLaneOverlay) DetectedRule() string { return o.detectedRule }

// MoveUp/MoveDown drive whichever picker the current step owns; a no-op on
// step 1 (the name field has no picker).
func (o *NewLaneOverlay) MoveUp() {
	switch o.step {
	case NewLaneStepAccount:
		if o.accountCursor > 0 {
			o.accountCursor--
		}
	case NewLaneStepModality:
		if o.modalityCursor > 0 {
			o.modalityCursor--
		}
	}
}

func (o *NewLaneOverlay) MoveDown() {
	switch o.step {
	case NewLaneStepAccount:
		if o.accountCursor < len(o.accounts)-1 {
			o.accountCursor++
		}
	case NewLaneStepModality:
		if o.modalityCursor < len(NewLaneModalities)-1 {
			o.modalityCursor++
		}
	}
}

// autodetect runs the N-rule ladder (FRONTDOOR-SPEC.md "Autodetect: the
// ladder") against the lane's own name and the chosen seat's registry
// default - the only rungs reachable for a NEW lane, whose folder does not
// exist yet (the F-rules read an existing lane's own CLAUDE.md/STATUS.md
// and can never fire here). First match wins. N1 (a --modality flag) is
// not reachable from inside the cockpit at all - there is no rung for it
// below, the ladder simply starts at N2.
func (o *NewLaneOverlay) autodetect() (key string, rule string) {
	if strings.HasSuffix(o.name, "-bid") {
		return "bid", "N2, the name ends -bid"
	}
	if o.forgeAppsRoot != "" && o.name != "" {
		if info, err := os.Stat(filepath.Join(o.forgeAppsRoot, o.name)); err == nil && info.IsDir() {
			return "app-pipeline", "N3, repos/clarity-forge/apps/" + o.name + " exists"
		}
	}
	if acc := o.SelectedAccount(); acc.DefaultModality != "" {
		return acc.DefaultModality, "N4, the registry's default modality for " + acc.Tag
	}
	return "general", ""
}

// ---- rendering ----

// padRight right-pads s with spaces to width display columns; s already at
// or over width is returned unchanged (every fixed label below is measured
// to fit its own field, so this only ever adds trailing space).
func padRight(s string, width int) string {
	w := runewidth.StringWidth(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// titleBar renders "╭─ left ────── right ─╮" at exactly newLaneWidth
// columns - the box's own top border, carrying "New lane[ · name[ · seat]]"
// on the left and "step N of 3" on the right.
func titleBar(left, right string) string {
	dashes := newLaneWidth - 8 - runewidth.StringWidth(left) - runewidth.StringWidth(right)
	if dashes < 1 {
		dashes = 1
	}
	return "╭─ " + left + " " + strings.Repeat("─", dashes) + " " + right + " ─╮"
}

// footerBarLine renders "╰─ left ──────╯" at exactly newLaneWidth columns -
// the box's own bottom border, carrying its own per-step key list (the main
// footer never changes - FRONTDOOR-SPEC.md "The overlay").
func footerBarLine(left string) string {
	dashes := newLaneWidth - 5 - runewidth.StringWidth(left)
	if dashes < 1 {
		dashes = 1
	}
	return "╰─ " + left + " " + strings.Repeat("─", dashes) + "╯"
}

// wrapContent wraps one interior line (padded to newLaneInnerWidth) in the
// box's own side borders.
func wrapContent(s string) string {
	return "│" + padRight(s, newLaneInnerWidth) + "│"
}

func nameFieldTop() string {
	return "   ┌" + strings.Repeat("─", newLaneFieldWidth) + "┐   "
}

func nameFieldBottom() string {
	return "   └" + strings.Repeat("─", newLaneFieldWidth) + "┘   "
}

func nameFieldRow(content string) string {
	return "   │" + padRight(content, newLaneFieldWidth) + "│   "
}

// truncateFolderLine keeps the folder path's own TAIL - the lane name at
// the end, the part that actually varies - when prefix+path would overflow
// the box's interior width, ellipsis-cutting the front instead. Same idiom
// ui/session.go's own header line 2 uses for a long working-directory path
// (ansi.TruncateLeft: cut n cells off the front, prepend the ellipsis).
// Never reached at the sessions root's own real depth with a short name -
// only a long root or a name near the 32-column cap can trigger it - but a
// box whose one overflowing line poisons every OTHER line's placement is
// worse than a path that loses its own front.
func truncateFolderLine(prefix, path string, width int) string {
	avail := width - runewidth.StringWidth(prefix)
	if avail < 1 {
		avail = 1
	}
	if runewidth.StringWidth(path) <= avail {
		return prefix + path
	}
	const ellipsis = "…"
	n := runewidth.StringWidth(path) - avail + runewidth.StringWidth(ellipsis)
	if n < 0 {
		n = 0
	}
	return prefix + ansi.TruncateLeft(path, n, ellipsis)
}

func (o *NewLaneOverlay) renderNameStep() []string {
	return []string{
		"",
		"   Name",
		nameFieldTop(),
		nameFieldRow(" ▸ " + o.name + "█"),
		nameFieldBottom(),
		"",
		truncateFolderLine("   Folder   ", o.FolderPath(), newLaneInnerWidth),
		"            does not exist yet · it will be created with repos and work",
		"            symlinked, as clarity new already does",
		"",
		"",
	}
}

// displayHome shows a config dir relative to $HOME with the "~" shorthand
// the mock-up uses, exactly like every shell prompt does - never a read of
// anything under it.
func displayHome(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// wordForCount spells out a small count the way the mock-up's own decision-
// aid note does ("...of the three signed-in seats."); a count outside this
// range falls back to the bare numeral rather than growing the table
// forever for a case this workspace's own registry never reaches.
func wordForCount(n int) string {
	words := []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}
	if n >= 0 && n < len(words) {
		return words[n]
	}
	return fmt.Sprintf("%d", n)
}

// mostWindowLeftNote is the account step's own decision-aid line - which
// signed-in seat has the most window left. "" (a blank line) when fewer
// than two signed-in seats carry a live gauge, or when two tie exactly:
// there is nothing honest to recommend in either case.
func (o *NewLaneOverlay) mostWindowLeftNote() string {
	type cand struct {
		tag string
		pct int
	}
	var signedIn []cand
	for _, a := range o.accounts {
		if a.CredentialStore && a.HasLiveLane {
			signedIn = append(signedIn, cand{a.Tag, a.FillPct})
		}
	}
	if len(signedIn) < 2 {
		return ""
	}
	best := signedIn[0]
	tie := false
	for _, c := range signedIn[1:] {
		switch {
		case c.pct < best.pct:
			best = c
			tie = false
		case c.pct == best.pct:
			tie = true
		}
	}
	if tie {
		return ""
	}
	return fmt.Sprintf("%s has the most window left of the %s signed-in seats.", best.tag, wordForCount(len(signedIn)))
}

// accountDirFieldWidth is the step-2 config-dir cell's own column width -
// measured off the mock-up row layout renderAccountRow builds (prefix 5 +
// tag 9 + dir 23 + status 12 + tail 33 = newLaneInnerWidth).
const accountDirFieldWidth = 23

// truncateAccountDir keeps a seat's config-dir cell within its own column
// width, ellipsis-cutting the FRONT and keeping the TAIL - front-door slice
// 7 item 6a, the owner's own first-use defect: an unclipped cell (e.g. a
// scratch registry's deep config dir, longer than displayHome's "~" shorcut
// ever shortens a non-$HOME path to) ran past its own column and into the
// status column's own x. The tail is what tells seats apart; the shared
// prefix is not. Same ellipsis-cut-the-front idiom as truncateFolderLine
// above.
func truncateAccountDir(s string, width int) string {
	if runewidth.StringWidth(s) <= width {
		return s
	}
	const ellipsis = "…"
	n := runewidth.StringWidth(s) - width + runewidth.StringWidth(ellipsis)
	if n < 0 {
		n = 0
	}
	return ansi.TruncateLeft(s, n, ellipsis)
}

func (o *NewLaneOverlay) renderAccountRow(acc NewLaneAccountRow, selected bool) string {
	prefix := "     "
	if selected {
		prefix = "   ▸ "
	}
	tagField := padRight(acc.Tag, 9)
	dirField := padRight(truncateAccountDir(displayHome(acc.ConfigDir), accountDirFieldWidth), accountDirFieldWidth)

	var statusField, tailField string
	if acc.CredentialStore {
		statusField = padRight("signed in", 12)
		fillLabel := "idle"
		if acc.HasLiveLane {
			fillLabel = fmt.Sprintf("%d%% of window used", acc.FillPct)
		}
		tail := fillLabel
		if acc.IsDefault {
			tail += "   default"
		}
		tailField = padRight(tail, 33)
	} else {
		statusField = padRight("no login", 12)
		tailField = padRight("l runs /login in the pane", 33)
	}
	return prefix + tagField + dirField + statusField + tailField
}

func (o *NewLaneOverlay) renderAccountStep() []string {
	lines := []string{
		"",
		"   Account   which login this lane runs on",
		"",
	}
	for i, acc := range o.accounts {
		lines = append(lines, o.renderAccountRow(acc, i == o.accountCursor))
	}
	lines = append(lines, "")
	if note := o.mostWindowLeftNote(); note != "" {
		lines = append(lines, "   "+note)
	} else {
		lines = append(lines, "")
	}
	lines = append(lines, "")
	return lines
}

func (o *NewLaneOverlay) detectedLine() string {
	sel := o.SelectedModality()
	if o.detectedRule == "" {
		return fmt.Sprintf("Detected  %s · no rule fired · ↑↓ overrides it", sel.Label)
	}
	return fmt.Sprintf("Detected  %s · rule %s · ↑↓ overrides it", sel.Label, o.detectedRule)
}

func (o *NewLaneOverlay) renderModalityStep() []string {
	lines := []string{
		"",
		"   Modality   how this lane works, and which heading it sits under",
		"",
	}
	for i, mrow := range NewLaneModalities {
		prefix := "     "
		if i == o.modalityCursor {
			prefix = "   ▸ "
		}
		lines = append(lines, prefix+padRight(mrow.Label, 19)+mrow.Desc)
	}
	lines = append(lines, "")
	lines = append(lines, "   "+o.detectedLine())
	lines = append(lines, "")
	return lines
}

func (o *NewLaneOverlay) renderProgressLine() string {
	m1, m2, m3 := "○", "○", "○"
	switch o.step {
	case NewLaneStepName:
		m1 = "●"
	case NewLaneStepAccount:
		m1, m2 = "✓", "●"
	case NewLaneStepModality:
		m1, m2, m3 = "✓", "✓", "●"
	}
	return fmt.Sprintf("   %s 1 name    %s 2 account    %s 3 modality", m1, m2, m3)
}

func (o *NewLaneOverlay) footerKeys() string {
	switch o.step {
	case NewLaneStepName:
		return "enter next · esc cancel"
	case NewLaneStepAccount:
		return "↑↓ pick · enter next · l log in · esc back"
	default:
		return "↑↓ pick · enter start · esc back"
	}
}

// Render draws the box exactly as FRONTDOOR-MOCKUP-164x45.md screens 1-3
// draw it - the caller (app.go's View) places it over the pane only, never
// the whole screen, so the list stays visible.
func (o *NewLaneOverlay) Render() string {
	right := fmt.Sprintf("step %d of 3", int(o.step)+1)

	var left string
	switch o.step {
	case NewLaneStepName:
		left = "New lane"
	case NewLaneStepAccount:
		left = "New lane · " + o.name
	default:
		left = "New lane · " + o.name + " · " + o.SelectedAccount().Tag
	}

	var body []string
	switch o.step {
	case NewLaneStepName:
		body = o.renderNameStep()
	case NewLaneStepAccount:
		body = o.renderAccountStep()
	case NewLaneStepModality:
		body = o.renderModalityStep()
	}
	body = append(body, o.renderProgressLine())
	body = append(body, "")

	var b strings.Builder
	b.WriteString(titleBar(left, right))
	for _, l := range body {
		b.WriteByte('\n')
		b.WriteString(wrapContent(l))
	}
	b.WriteByte('\n')
	b.WriteString(footerBarLine(o.footerKeys()))
	return b.String()
}
