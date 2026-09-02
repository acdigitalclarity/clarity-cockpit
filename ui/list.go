package ui

import (
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/session/clarity"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"github.com/mattn/go-runewidth"
)

const readyIcon = "● "
const pausedIcon = "⏸ "

var readyStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#51bd73"), Dark: lipgloss.Color("#51bd73")})

var addedLinesStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#51bd73"), Dark: lipgloss.Color("#51bd73")})

var removedLinesStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#de613e"))

var pausedStyle = lipgloss.NewStyle().
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#888888"), Dark: lipgloss.Color("#888888")})

var titleStyle = lipgloss.NewStyle().
	Padding(1, 1, 0, 1).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#1a1a1a"), Dark: lipgloss.Color("#dddddd")})

var listDescStyle = lipgloss.NewStyle().
	Padding(0, 1, 1, 1).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#A49FA5"), Dark: lipgloss.Color("#777777")})

var selectedTitleStyle = lipgloss.NewStyle().
	Padding(1, 1, 0, 1).
	Background(lipgloss.Color("#dde4f0")).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#1a1a1a"), Dark: lipgloss.Color("#1a1a1a")})

var selectedDescStyle = lipgloss.NewStyle().
	Padding(0, 1, 1, 1).
	Background(lipgloss.Color("#dde4f0")).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#1a1a1a"), Dark: lipgloss.Color("#1a1a1a")})

var mainTitle = lipgloss.NewStyle().
	Background(lipgloss.Color("62")).
	Foreground(lipgloss.Color("230"))

var autoYesStyle = lipgloss.NewStyle().
	Background(lipgloss.Color("#dde4f0")).
	Foreground(lipgloss.Color("#1a1a1a"))

var needsYouTitleStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#b5581a"), Dark: lipgloss.Color("#e0a458")})

var needsYouLineStyle = lipgloss.NewStyle().
	Padding(0, 0, 0, 1).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#5a5a5a"), Dark: lipgloss.Color("#aaaaaa")})

var externalTitleStyle = lipgloss.NewStyle().
	Bold(true).
	Padding(1, 1, 0, 1).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#5a5a5a"), Dark: lipgloss.Color("#999999")})

var externalRowStyle = lipgloss.NewStyle().
	Padding(0, 1, 0, 1).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#777777"), Dark: lipgloss.Color("#999999")})

var externalRowSelectedStyle = lipgloss.NewStyle().
	Padding(0, 1, 0, 1).
	Background(lipgloss.Color("#dde4f0")).
	Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#1a1a1a"), Dark: lipgloss.Color("#1a1a1a")})

type List struct {
	items         []*session.Instance
	selectedIdx   int
	height, width int
	renderer      *InstanceRenderer
	autoyes       bool

	// map of repo name to number of instances using it. Used to display the repo name only if there are
	// multiple repos in play.
	repos map[string]int

	// needsYou holds the "Needs you" feed lines, refreshed once per feed
	// tick by the caller (see app.go's feedTickMsg handling) - this struct
	// never reads the queue file itself, it only renders what it is handed.
	needsYou []string

	// external holds the fleet's live lanes that are NOT tracked Claude
	// Squad instances (see clarity.DiscoverExternalLanes), refreshed on the
	// same tick as the rest of the metadata. Rendered below the tracked
	// instances; selectable and messageable, never attachable or killable.
	external []clarity.ExternalLane

	// selExternal is true when the current selection points into external
	// rather than items - the two lists share one selection cursor that
	// wraps from the bottom of items into the top of external and back.
	selExternal bool
}

// SetNeedsYou replaces the "Needs you" feed lines shown above the instance
// list.
func (l *List) SetNeedsYou(lines []string) {
	l.needsYou = lines
}

// SetExternal replaces the external-lane rows shown below the tracked
// instances. If the current selection was pointing into external and the
// new list is shorter (or empty), the selection is clamped so it never
// points past the end - matching how a killed/removed instance never
// leaves selectedIdx dangling.
func (l *List) SetExternal(lanes []clarity.ExternalLane) {
	l.external = lanes
	if l.selExternal {
		if len(l.external) == 0 {
			l.selExternal = false
			l.selectedIdx = 0
		} else if l.selectedIdx >= len(l.external) {
			l.selectedIdx = len(l.external) - 1
		}
	}
}

// GetExternal returns the current external-lane rows.
func (l *List) GetExternal() []clarity.ExternalLane {
	return l.external
}

func NewList(spinner *spinner.Model, autoYes bool) *List {
	return &List{
		items:    []*session.Instance{},
		renderer: &InstanceRenderer{spinner: spinner},
		repos:    make(map[string]int),
		autoyes:  autoYes,
	}
}

// SetSize sets the height and width of the list.
func (l *List) SetSize(width, height int) {
	l.width = width
	l.height = height
	l.renderer.setWidth(width)
}

// SetSessionPreviewSize sets the height and width for the tmux sessions. This makes the stdout line have the correct
// width and height.
func (l *List) SetSessionPreviewSize(width, height int) (err error) {
	for i, item := range l.items {
		if !item.Started() || item.Paused() {
			continue
		}

		if innerErr := item.SetPreviewSize(width, height); innerErr != nil {
			err = errors.Join(
				err, fmt.Errorf("could not set preview size for instance %d: %v", i, innerErr))
		}
	}
	return
}

func (l *List) NumInstances() int {
	return len(l.items)
}

// InstanceRenderer handles rendering of session.Instance objects
type InstanceRenderer struct {
	spinner *spinner.Model
	width   int
}

func (r *InstanceRenderer) setWidth(width int) {
	r.width = AdjustPreviewWidth(width)
}

// ɹ and ɻ are other options.
const branchIcon = "Ꮧ"

func (r *InstanceRenderer) Render(i *session.Instance, idx int, selected bool, hasMultipleRepos bool) string {
	prefix := fmt.Sprintf(" %d. ", idx)
	if idx >= 10 {
		prefix = prefix[:len(prefix)-1]
	}
	titleS := selectedTitleStyle
	descS := selectedDescStyle
	if !selected {
		titleS = titleStyle
		descS = listDescStyle
	}

	// add spinner next to title if it's running
	var join string
	switch i.Status {
	case session.Running, session.Loading:
		join = fmt.Sprintf("%s ", r.spinner.View())
	case session.Ready:
		join = readyStyle.Render(readyIcon)
	case session.Paused:
		join = pausedStyle.Render(pausedIcon)
	default:
	}

	// Cut the title if it's too long
	titleText := i.Title
	widthAvail := r.width - 3 - runewidth.StringWidth(prefix) - 1
	if widthAvail > 0 && runewidth.StringWidth(titleText) > widthAvail {
		titleText = runewidth.Truncate(titleText, widthAvail-3, "...")
	}
	title := titleS.Render(lipgloss.JoinHorizontal(
		lipgloss.Left,
		lipgloss.Place(r.width-3, 1, lipgloss.Left, lipgloss.Center, fmt.Sprintf("%s %s", prefix, titleText)),
		" ",
		join,
	))

	stat := i.GetDiffStats()

	var diff string
	var addedDiff, removedDiff string
	if stat == nil || stat.Error != nil || stat.IsEmpty() {
		// Don't show diff stats if there's an error or if they don't exist
		addedDiff = ""
		removedDiff = ""
		diff = ""
	} else {
		addedDiff = fmt.Sprintf("+%d", stat.Added)
		removedDiff = fmt.Sprintf("-%d ", stat.Removed)
		diff = lipgloss.JoinHorizontal(
			lipgloss.Center,
			addedLinesStyle.Background(descS.GetBackground()).Render(addedDiff),
			lipgloss.Style{}.Background(descS.GetBackground()).Foreground(descS.GetForeground()).Render(","),
			removedLinesStyle.Background(descS.GetBackground()).Render(removedDiff),
		)
	}

	remainingWidth := r.width
	remainingWidth -= runewidth.StringWidth(prefix)
	remainingWidth -= runewidth.StringWidth(branchIcon)
	remainingWidth -= 2 // for the literal " " and "-" in the branchLine format string

	diffWidth := runewidth.StringWidth(addedDiff) + runewidth.StringWidth(removedDiff)
	if diffWidth > 0 {
		diffWidth += 1
	}

	// Use fixed width for diff stats to avoid layout issues
	remainingWidth -= diffWidth

	// Context-fill gauge: the same number scripts/fleet_dashboard.py would
	// show for this instance's lane (see session/clarity/gauge.go), or
	// "n/a" when no transcript resolves.
	fillLabel := "n/a"
	if fillPct, ok := i.GetContextFill(); ok {
		fillLabel = fmt.Sprintf("%d%%", fillPct)
	}
	gauge := fmt.Sprintf("ctx %s", fillLabel)
	gaugeWidth := runewidth.StringWidth(gauge) + 2 // surrounding separator spaces
	remainingWidth -= gaugeWidth

	branch := i.Branch
	if i.Started() && hasMultipleRepos {
		repoName, err := i.RepoName()
		if err != nil {
			log.ErrorLog.Printf("could not get repo name in instance renderer: %v", err)
		} else {
			branch += fmt.Sprintf(" (%s)", repoName)
		}
	}
	// Don't show branch if there's no space for it. Or show ellipsis if it's too long.
	branchWidth := runewidth.StringWidth(branch)
	if remainingWidth < 0 {
		branch = ""
	} else if remainingWidth < branchWidth {
		if remainingWidth < 3 {
			branch = ""
		} else {
			// We know the remainingWidth is at least 4 and branch is longer than that, so this is safe.
			branch = runewidth.Truncate(branch, remainingWidth-3, "...")
		}
	}
	remainingWidth -= runewidth.StringWidth(branch)

	// Add spaces to fill the remaining width.
	spaces := ""
	if remainingWidth > 0 {
		spaces = strings.Repeat(" ", remainingWidth)
	}

	branchLine := fmt.Sprintf("%s %s-%s%s %s %s", strings.Repeat(" ", len(prefix)), branchIcon, branch, spaces, gauge, diff)

	// join title and subtitle
	text := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		descS.Render(branchLine),
	)

	return text
}

func (l *List) String() string {
	const titleText = " Instances "
	const autoYesText = " auto-yes "

	// Write the title.
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("\n")

	// Write title line
	// add padding of 2 because the border on list items adds some extra characters
	titleWidth := AdjustPreviewWidth(l.width) + 2
	if !l.autoyes {
		b.WriteString(lipgloss.Place(
			titleWidth, 1, lipgloss.Left, lipgloss.Bottom, mainTitle.Render(titleText)))
	} else {
		title := lipgloss.Place(
			titleWidth/2, 1, lipgloss.Left, lipgloss.Bottom, mainTitle.Render(titleText))
		autoYes := lipgloss.Place(
			titleWidth-(titleWidth/2), 1, lipgloss.Right, lipgloss.Bottom, autoYesStyle.Render(autoYesText))
		b.WriteString(lipgloss.JoinHorizontal(
			lipgloss.Top, title, autoYes))
	}

	b.WriteString("\n")
	b.WriteString("\n")

	// Render the "Needs you" feed, once per feed tick (see app.go) - never
	// a bare empty section when the queue is absent, per the brief.
	if len(l.needsYou) > 0 {
		b.WriteString(needsYouTitleStyle.Render(" Needs you "))
		b.WriteString("\n")
		for _, line := range l.needsYou {
			b.WriteString(needsYouLineStyle.Render(line))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Render the list.
	for i, item := range l.items {
		selected := !l.selExternal && i == l.selectedIdx
		b.WriteString(l.renderer.Render(item, i+1, selected, len(l.repos) > 1))
		if i != len(l.items)-1 {
			b.WriteString("\n\n")
		}
	}

	// Render the external lanes - live on this Mac but not tracked here
	// (see clarity.DiscoverExternalLanes). Message-only: no diff stats, no
	// branch, no attach/kill affordance, because none of that exists for a
	// lane with no tracked tmux session or git worktree.
	if len(l.external) > 0 {
		if len(l.items) > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(externalTitleStyle.Render(" External lanes (message only) "))
		b.WriteString("\n")
		for i, lane := range l.external {
			style := externalRowStyle
			if l.selExternal && i == l.selectedIdx {
				style = externalRowSelectedStyle
			}
			fillLabel := "n/a"
			if lane.FillOK {
				fillLabel = fmt.Sprintf("%d%%", lane.Fill.Pct)
			}
			line := fmt.Sprintf("%s  ctx %-5s last write %s", lane.Name, fillLabel, lane.LastWrite.Format("15:04:05"))
			b.WriteString(style.Render(line))
			b.WriteString("\n")
		}
	}

	return lipgloss.Place(l.width, l.height, lipgloss.Left, lipgloss.Top, b.String())
}

// Down selects the next item in the list, crossing from the tracked
// instances into the external rows (and wrapping back to the top of items)
// when external rows are present.
func (l *List) Down() {
	if len(l.items) == 0 && len(l.external) == 0 {
		return
	}
	if !l.selExternal {
		if l.selectedIdx < len(l.items)-1 {
			l.selectedIdx++
			return
		}
		if len(l.external) > 0 {
			l.selExternal = true
			l.selectedIdx = 0
			return
		}
		l.selectedIdx = 0
		return
	}
	// Currently on an external row.
	if l.selectedIdx < len(l.external)-1 {
		l.selectedIdx++
		return
	}
	l.selExternal = false
	l.selectedIdx = 0
}

// Kill selects the next item in the list. A no-op when the selection is on
// an external row - there is no tracked instance behind it to kill.
func (l *List) Kill() {
	if l.selExternal || len(l.items) == 0 {
		return
	}
	targetInstance := l.items[l.selectedIdx]

	// Kill the tmux session
	if err := targetInstance.Kill(); err != nil {
		log.ErrorLog.Printf("could not kill instance: %v", err)
	}

	// If you delete the last one in the list, select the previous one.
	if l.selectedIdx == len(l.items)-1 {
		defer l.Up()
	}

	// Unregister the reponame.
	repoName, err := targetInstance.RepoName()
	if err != nil {
		log.ErrorLog.Printf("could not get repo name: %v", err)
	} else {
		l.rmRepo(repoName)
	}

	// Since there's items after this, the selectedIdx can stay the same.
	l.items = append(l.items[:l.selectedIdx], l.items[l.selectedIdx+1:]...)
}

// Attach attaches to the selected tracked instance. Returns an error
// without attaching anything when the selection is on an external row -
// there is no tracked tmux session behind it to attach to.
func (l *List) Attach() (chan struct{}, error) {
	if l.selExternal || len(l.items) == 0 || l.selectedIdx >= len(l.items) {
		return nil, errors.New("cannot attach: no tracked instance is selected")
	}
	targetInstance := l.items[l.selectedIdx]
	return targetInstance.Attach()
}

// Up selects the previous item in the list, crossing from the external rows
// into the tracked instances (and wrapping back to the bottom of external)
// when external rows are present.
func (l *List) Up() {
	if len(l.items) == 0 && len(l.external) == 0 {
		return
	}
	if l.selExternal {
		if l.selectedIdx > 0 {
			l.selectedIdx--
			return
		}
		if len(l.items) > 0 {
			l.selExternal = false
			l.selectedIdx = len(l.items) - 1
			return
		}
		l.selectedIdx = len(l.external) - 1
		return
	}
	// Currently on a tracked instance.
	if l.selectedIdx > 0 {
		l.selectedIdx--
		return
	}
	if len(l.external) > 0 {
		l.selExternal = true
		l.selectedIdx = len(l.external) - 1
		return
	}
	l.selectedIdx = len(l.items) - 1
}

func (l *List) addRepo(repo string) {
	if _, ok := l.repos[repo]; !ok {
		l.repos[repo] = 0
	}
	l.repos[repo]++
}

func (l *List) rmRepo(repo string) {
	if _, ok := l.repos[repo]; !ok {
		log.ErrorLog.Printf("repo %s not found", repo)
		return
	}
	l.repos[repo]--
	if l.repos[repo] == 0 {
		delete(l.repos, repo)
	}
}

// AddInstance adds a new instance to the list. It returns a finalizer function that should be called when the instance
// is started. If the instance was restored from storage or is paused, you can call the finalizer immediately.
// When creating a new one and entering the name, you want to call the finalizer once the name is done.
func (l *List) AddInstance(instance *session.Instance) (finalize func()) {
	l.items = append(l.items, instance)
	// The finalizer registers the repo name once the instance is started.
	return func() {
		repoName, err := instance.RepoName()
		if err != nil {
			log.ErrorLog.Printf("could not get repo name: %v", err)
			return
		}

		l.addRepo(repoName)
	}
}

// GetSelectedInstance returns the currently selected tracked instance, or
// nil when the selection is on an external row (or the list is empty) - an
// external row cannot be attached, killed, or otherwise treated as a
// tracked instance, so every caller that already nil-checks this (kill,
// attach, checkout, push, resume, move) gets that guard for free.
func (l *List) GetSelectedInstance() *session.Instance {
	if l.selExternal || len(l.items) == 0 || l.selectedIdx >= len(l.items) {
		return nil
	}
	return l.items[l.selectedIdx]
}

// SelectedMsgTarget returns the lane name of the current selection,
// whichever list it is in, plus whether it is an external row - both
// tracked instances and external rows are messageable (the brief's
// requirement), only tracked instances are attachable/killable. ok is
// false when nothing is selected (both lists empty, or the index is out of
// range for its list).
func (l *List) SelectedMsgTarget() (lane string, isExternal bool, ok bool) {
	if l.selExternal {
		if l.selectedIdx < 0 || l.selectedIdx >= len(l.external) {
			return "", false, false
		}
		return l.external[l.selectedIdx].Name, true, true
	}
	if l.selectedIdx < 0 || l.selectedIdx >= len(l.items) {
		return "", false, false
	}
	return l.items[l.selectedIdx].Title, false, true
}

// SetSelectedInstance sets the selected index. Noop if the index is out of bounds.
func (l *List) SetSelectedInstance(idx int) {
	if idx >= len(l.items) {
		return
	}
	l.selectedIdx = idx
}

// SelectInstance finds and selects the given instance in the list.
func (l *List) SelectInstance(target *session.Instance) {
	for i, inst := range l.items {
		if inst == target {
			l.SetSelectedInstance(i)
			return
		}
	}
}

// MoveUp swaps the selected instance with the one above it.
func (l *List) MoveUp() bool {
	if l.selectedIdx <= 0 || len(l.items) < 2 {
		return false
	}
	l.items[l.selectedIdx], l.items[l.selectedIdx-1] = l.items[l.selectedIdx-1], l.items[l.selectedIdx]
	l.selectedIdx--
	return true
}

// MoveDown swaps the selected instance with the one below it.
func (l *List) MoveDown() bool {
	if l.selectedIdx >= len(l.items)-1 || len(l.items) < 2 {
		return false
	}
	l.items[l.selectedIdx], l.items[l.selectedIdx+1] = l.items[l.selectedIdx+1], l.items[l.selectedIdx]
	l.selectedIdx++
	return true
}

// GetInstances returns all instances in the list
func (l *List) GetInstances() []*session.Instance {
	return l.items
}
