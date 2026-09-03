package app

import (
	"claude-squad/cmd"
	"claude-squad/config"
	"claude-squad/keys"
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/session/clarity"
	"claude-squad/session/git"
	"claude-squad/ui"
	"claude-squad/ui/overlay"
	"claude-squad/ui/splash"
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"
)

const GlobalInstanceLimit = 10

// Run is the main entrypoint into the application. noSplash skips the
// entrance splash screen and starts directly in the instance list -
// clarity-attach, discover and msg never call Run at all, so they never
// show the splash regardless of this flag.
// NoButterfly hides the tab-bar butterfly; set by main from the flag or config before Run.
var NoButterfly bool

func Run(ctx context.Context, program string, autoYes bool, noSplash bool) error {
	// AltScreen and mouse-cell-motion (scroll) are now declared per-View,
	// see (*home).View below - v2's declarative View fields replace v1's
	// NewProgram options.
	p := tea.NewProgram(newHome(ctx, program, autoYes, noSplash))
	_, err := p.Run()
	return err
}

type state int

const (
	stateDefault state = iota
	// stateNew is the state when the user is creating a new instance.
	stateNew
	// statePrompt is the state when the user is entering a prompt.
	statePrompt
	// stateHelp is the state when a help screen is displayed.
	stateHelp
	// stateConfirm is the state when a confirmation modal is displayed.
	stateConfirm
	// stateMsg is the state when the user is entering text for the m key -
	// send-into-tmux for the selected row, tracked instance or external
	// lane alike (Digital Clarity workspace enhancement).
	stateMsg
	// stateSessionPicker is the v-key turn picker on the Session tab (slice
	// 22, PART B): up/down move the highlight between the selected lane's
	// own loaded turns, c copies the highlighted one, esc leaves - all
	// intercepted here, same shape as stateMsg above, so ordinary list
	// navigation is suspended while the picker is open.
	stateSessionPicker
)

type home struct {
	ctx context.Context

	// -- Storage and Configuration --

	program string
	autoYes bool

	// storage is the interface for saving/loading data to/from the app's state
	storage *session.Storage
	// appConfig stores persistent application configuration
	appConfig *config.Config
	// appState stores persistent application state like seen help screens
	appState config.AppState

	// -- State --

	// state is the current discrete state of the application
	state state
	// newInstanceFinalizer is called when the state is stateNew and then you press enter.
	// It registers the new instance in the list after the instance has been started.
	newInstanceFinalizer func()

	// promptAfterName tracks if we should enter prompt mode after naming
	promptAfterName bool

	// keySent is used to manage underlining menu items
	keySent bool

	// instanceStarting is true while a background instance start is in progress.
	// Prevents double-submission and guards against interacting with a not-yet-started instance.
	instanceStarting bool
	// startingInstance holds a reference to the instance being started in the background.
	startingInstance *session.Instance

	// -- UI Components --

	// list displays the list of instances
	list *ui.List
	// menu displays the bottom menu
	menu *ui.Menu
	// tabbedWindow displays the tabbed window with preview and diff panes
	tabbedWindow *ui.TabbedWindow
	// sessionPane is the SAME *ui.SessionPane instance handed into
	// tabbedWindow's own construction (ui.NewTabbedWindow) - kept here too
	// (slice 22, PART B) so handleCopy/handleCopyTail/handleOpenPicker can
	// reach its own copy/picker methods directly, without TabbedWindow (a
	// different leg's own file, pane-19/pane-21) needing a new passthrough
	// method for each one.
	sessionPane *ui.SessionPane
	// errBox displays error messages
	errBox *ui.ErrBox
	// statusBox displays ephemeral non-error status text - currently just
	// the m-key message-delivery result.
	statusBox *ui.StatusBox
	// hasErr and statusText track whether errBox/statusBox currently hold
	// something to show, so View() can pick between the two footer rows
	// without ui.ErrBox/ui.StatusBox needing their own "is set" getters.
	hasErr     bool
	statusText string
	// global spinner instance. we plumb this down to where it's needed
	spinner spinner.Model
	// textInputOverlay handles text input with state - the "new instance"/
	// "enter prompt" flows only; the m-key composer (stateMsg) is the
	// inline Composer below, not this overlay (design/cockpit-pane/
	// DECISIONS.md slice 5 - the old full-screen overlay drove m before
	// this slice wired the mock-up's own inline box instead).
	textInputOverlay *overlay.TextInputOverlay
	// composer is the shared inline message box (slice 5) both the Session
	// and Needs-you tabs render at their own foot - one instance, since
	// only one row can be the current send target at a time.
	composer *ui.Composer
	// cmdExec runs the composer's external-lane clipboard copy (pbcopy) -
	// the same cmd.Executor seam session/tmux already uses for tmux, so
	// tests can inject a fake without touching the real clipboard.
	cmdExec cmd.Executor
	// boardCache fetches and caches a Needs-you row's board issue body
	// (clarity.BoardCache) - lazily initialized on first use, same pattern
	// as laneTailCache below.
	boardCache *clarity.BoardCache
	// laneTab/needsYouTab remember the user's own last-chosen tab for each
	// row kind (slice 5's "remember the user's own tab choice per row kind
	// so tab does not fight the cursor") - laneTab covers both tracked and
	// external rows (one lane kind), needsYouTab the Needs-you rows.
	laneTab, needsYouTab int
	// prevRowKind is the row kind as of the last selection-changed call -
	// the tab is only force-switched on a KIND TRANSITION (see
	// syncTabToRowKind), never on every Up/Down within the same kind, so a
	// manual Tab press away from the default while browsing several
	// Needs-you rows in a row is not immediately undone by the next Down.
	prevRowKind ui.RowKind
	// textOverlay displays text information
	textOverlay *overlay.TextOverlay
	// confirmationOverlay displays confirmation modals
	confirmationOverlay *overlay.ConfirmationOverlay

	// splashModel is the entrance/idle animation shown before the list -
	// nil once handed off (any key press, or 2s after the entrance
	// completes), and nil from construction entirely when noSplash is set.
	splashModel *splash.Model

	// laneTailCache memoizes clarity.ReadLaneTail per transcript path (see
	// session/clarity/tail_cache.go) - feedTickMsg's handler below reads
	// through it for every tracked instance and external lane, so a
	// transcript unchanged since the last tick is not reparsed. Lazily
	// initialized on first use rather than in newHome, so a *home built
	// directly in a test (skipping newHome) still works.
	laneTailCache *clarity.LaneTailCache

	// transcriptWatcher is the fsnotify seam (slice 14 rule 3) watching the
	// SELECTED lane's own transcript file - nil until the first lane with a
	// resolvable transcript is selected, lazily constructed by
	// retargetTranscriptWatch. Injected directly by tests (same package,
	// see watch_test.go) rather than through a factory - production code
	// only ever constructs the real fsnotifyWatcher, lazily, on first use.
	transcriptWatcher transcriptWatcher
	// transcriptWatchPath is the path transcriptWatcher currently targets
	// ("" = nothing) - retargetTranscriptWatch's own no-op guard against
	// re-Watching the same path on every tick.
	transcriptWatchPath string
	// transcriptWatchGen is bumped on every retarget, tagging every
	// transcriptChangedMsg/watchTranscriptCmd this watch produces so an
	// event from a superseded watch (the selection has already moved on)
	// is recognised as stale and discarded rather than triggering a read
	// for a lane no longer selected.
	transcriptWatchGen uint64
	// transcriptDebounceGen is bumped on every transcriptChangedMsg -
	// transcriptDebounceMsg only performs its read when its own gen still
	// matches this, so a burst of several fsnotify events inside one
	// debounce window collapses into exactly one cache read (rule 3).
	transcriptDebounceGen uint64
}

func newHome(ctx context.Context, program string, autoYes bool, noSplash bool) *home {
	// Load application config
	appConfig := config.LoadConfig()

	// Load application state
	appState := config.LoadState()

	// Initialize storage
	storage, err := session.NewStorage(appState)
	if err != nil {
		fmt.Printf("Failed to initialize storage: %v\n", err)
		os.Exit(1)
	}

	composer := ui.NewComposer()
	sessionPane := ui.NewSessionPane()
	sessionPane.SetComposer(composer)
	needsYouPane := ui.NewNeedsYouPane()
	needsYouPane.SetComposer(composer)

	h := &home{
		ctx:          ctx,
		spinner:      spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		menu:         ui.NewMenu(),
		tabbedWindow: ui.NewTabbedWindow(sessionPane, needsYouPane, ui.NewTerminalPane()),
		sessionPane:  sessionPane,
		errBox:       ui.NewErrBox(),
		statusBox:    ui.NewStatusBox(),
		storage:      storage,
		appConfig:    appConfig,
		program:      program,
		autoYes:      autoYes,
		state:        stateDefault,
		appState:     appState,
		composer:     composer,
		cmdExec:      cmd.MakeExecutor(),
		laneTab:      ui.SessionTab,
		needsYouTab:  ui.NeedsYouTab,
		prevRowKind:  ui.RowKindTracked,
	}
	if NoButterfly {
		h.tabbedWindow.SetButterflyEnabled(false)
	}
	if !noSplash {
		h.splashModel = splash.New()
	}
	h.list = ui.NewList(&h.spinner, autoYes)

	// Load saved instances
	instances, err := storage.LoadInstances()
	if err != nil {
		fmt.Printf("Failed to load instances: %v\n", err)
		os.Exit(1)
	}

	// Add loaded instances to the list
	for _, instance := range instances {
		// Call the finalizer immediately.
		h.list.AddInstance(instance)()
		if autoYes {
			instance.AutoYes = true
		}
	}

	return h
}

// collapsePreviewBelowWidth is the OVERFLOW fix's stated decision: below
// this many terminal columns there simply isn't room for a list column and
// a Preview/Diff pane side by side without either one being squeezed to
// unreadable (or, as observed, the list refusing to shrink and pushing the
// preview pane off-screen entirely). Below it the preview/diff pane is
// COLLAPSED - width 0, not rendered - and the list takes the full width;
// above it the two share the width by fixed proportion. Stacking the two
// vertically instead was the other option the brief named; collapsing was
// chosen because this fork's TabbedWindow/List already both render as a
// single horizontal-join block with no vertical-stack code path, so
// collapsing is the root-cause-sized fix and stacking would be new
// machinery for a case (a terminal under 100 columns) this app is rarely
// run at.
const collapsePreviewBelowWidth = 100

// listWidthMin/listWidthMax bound listWidthForTerminal's clamp - the list's
// new compact row (defect 2: name/pct/glyph+word/time, no branch/diff-stat
// second line's own width needs) never needs the 40%-of-a-wide-terminal
// share the old row format did, and the Session pane's own header line 2
// (workdir · branch · model · window) needs the room this frees up to stop
// truncating those fields at the sizes this app actually runs at.
const (
	listWidthMin = 38
	listWidthMax = 52
)

// listWidthForTerminal is the DEFECT 2 rule, verbatim: 28% of the terminal
// width, clamped to [listWidthMin, listWidthMax] - replacing the old flat
// 40% split, which gave the list far more than its new compact row format
// needs and starved the pane's own header line of the width it needs to
// show branch and model without truncating (ui/session.go's
// padRowKeepRight). Only called above collapsePreviewBelowWidth; below it
// the list still takes the whole terminal, unchanged.
//
// Rounds rather than truncates (slice 13's own fix, part of "the tabbed
// window and the list must together reach column 164"): 164*0.28=45.92
// truncated to 45 was quietly discarding the list's own fractional column
// rather than giving it to either side - math.Round hands it to whichever
// side it actually belongs to (164 case: the list, landing on 46, matching
// SESSION-READING-SPEC.md's own "List 46 (cols 1-46)"). Every clamped case
// (100, 120, 200 columns) is unaffected, since the fraction never survives
// the min/max clamp either way.
func listWidthForTerminal(width int) int {
	w := int(math.Round(float64(width) * 0.28))
	if w < listWidthMin {
		w = listWidthMin
	}
	if w > listWidthMax {
		w = listWidthMax
	}
	return w
}

// updateHandleWindowSizeEvent sets the sizes of the components.
// The components will try to render inside their bounds.
func (m *home) updateHandleWindowSizeEvent(msg tea.WindowSizeMsg) {
	// List takes listWidthForTerminal's own share, preview takes the rest -
	// above the collapse threshold. (The OVERFLOW defect's real cause was
	// that neither the list's feed rows nor the external-lane rows ever
	// truncated to whatever column width fell out of the split - fixed in
	// ui/list.go - so this ratio is about which side of the split gets the
	// room, not that fix.)
	var listWidth, tabsWidth int
	collapsed := msg.Width < collapsePreviewBelowWidth
	if collapsed {
		listWidth = msg.Width
		tabsWidth = 0
	} else {
		listWidth = listWidthForTerminal(msg.Width)
		tabsWidth = msg.Width - listWidth
	}
	// Item 1's "below 100 columns drop the word, keep the glyph" - the
	// list's own row renderer cannot see msg.Width (only its share of it),
	// so the collapsed/not-collapsed decision is threaded down explicitly.
	m.list.SetCollapsed(collapsed)

	// Menu takes 10% of height, list and window take 90%.
	//
	// menuHeight reserves 2 rows below contentHeight, not 1: one for the
	// error/status footer row, and one for View()'s own
	// lipgloss.NewStyle().PaddingTop(1) that it applies to both the list and
	// the preview pane on top of the content height they were actually
	// given - a row this arithmetic must account for or the whole screen
	// renders exactly one row taller than msg.Height every time (the
	// OVERFLOW defect's vertical half: at every height this was tested at,
	// View() rendered height+1 lines, one row over the terminal's own
	// height budget, which is exactly what pushed the preview box's bottom
	// border off screen at a small height like 24 rows).
	contentHeight := int(float32(msg.Height) * 0.9)
	menuHeight := msg.Height - contentHeight - 2        // -1 padding row, -1 error/status box
	m.errBox.SetSize(int(float32(msg.Width)*0.9), 1)    // error box takes 1 row
	m.statusBox.SetSize(int(float32(msg.Width)*0.9), 1) // status box shares that row

	m.tabbedWindow.SetSize(tabsWidth, contentHeight)
	m.list.SetSize(listWidth, contentHeight)

	if m.textInputOverlay != nil {
		m.textInputOverlay.SetSize(int(float32(msg.Width)*0.6), int(float32(msg.Height)*0.4))
	}
	if m.textOverlay != nil {
		m.textOverlay.SetWidth(int(float32(msg.Width) * 0.6))
	}

	// Skip when collapsed (tabsWidth == 0): TabbedWindow.SetSize leaves its
	// last valid content size in place rather than computing a negative
	// one, and there is nothing useful to re-apply to the real tmux panes
	// underneath while no tab is shown anyway.
	if tabsWidth > 0 {
		contentWidth, contentHeight := m.tabbedWindow.GetContentSize()
		if err := m.list.SetSessionPreviewSize(contentWidth, contentHeight); err != nil {
			log.ErrorLog.Print(err)
		}
	}
	m.menu.SetSize(msg.Width, menuHeight)
}

func (m *home) Init() tea.Cmd {
	// Upon starting, we want to start the spinner. Whenever we get a spinner.TickMsg, we
	// update the spinner, which sends a new spinner.TickMsg. I think this lasts forever lol.
	//
	// The list's own background ticks (spinner, preview, metadata, feed)
	// start immediately, splash or no splash, so the list is already warm
	// with real data the instant the splash hands off - no extra tick
	// needed to populate it (the "no visual glitch" requirement). The
	// splash's own tick and the window-size request are returned alongside
	// them.
	cmds := []tea.Cmd{
		tea.RequestWindowSize,
		m.spinner.Tick,
		func() tea.Msg {
			time.Sleep(100 * time.Millisecond)
			return previewTickMsg{}
		},
		tickUpdateMetadataCmd(m.snapshotActiveInstances(), m.list.GetSelectedInstance()),
		func() tea.Msg { return feedTickMsg{} },
		func() tea.Msg {
			time.Sleep(sessionTickInterval)
			return sessionTickMsg{}
		},
		m.retargetTranscriptWatch(),
	}
	if m.splashModel != nil {
		cmds = append(cmds, m.splashModel.Tick())
	}
	return tea.Batch(cmds...)
}

func (m *home) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case splash.TickMsg:
		if m.splashModel == nil {
			return m, nil
		}
		cmd := m.splashModel.Update(msg)
		if m.splashModel.Done() {
			m.splashModel = nil
		}
		return m, cmd
	case hideErrMsg:
		m.errBox.Clear()
		m.hasErr = false
	case hideStatusMsg:
		m.statusBox.Clear()
		m.statusText = ""
	case previewTickMsg:
		// The header/thinking-line spinner's own 100ms animation tick
		// (slice 14 rule 1): a bare counter increment, no file read - see
		// SessionPane.TickSpinner's own doc comment.
		m.tabbedWindow.TickSpinner()
		// Slice 21's own tab-bar butterfly rides the same 100ms tick.
		m.tabbedWindow.TickButterfly()
		cmd := m.instanceChanged()
		return m, tea.Batch(
			cmd,
			func() tea.Msg {
				time.Sleep(100 * time.Millisecond)
				return previewTickMsg{}
			},
		)
	case sessionTickMsg:
		// The Latency ruling (design/cockpit-pane/DECISIONS.md): the
		// SELECTED lane's Session tab refreshes on its OWN 500ms tick,
		// never the 3s feedTickMsg cadence every row also runs on. This
		// reads through the same laneTailCache feedTickMsg uses, but for
		// the selected lane only - an unchanged transcript costs exactly
		// one os.Stat (LaneTailCache.Get's own contract), so running this
		// six times as often as the feed tick is cheap.
		if m.laneTailCache == nil {
			m.laneTailCache = clarity.NewLaneTailCache()
		}
		m.updateSessionTabInfo(time.Now())
		// The fsnotify watch (slice 14 rule 3) follows the selection on
		// this 500ms cadence, NEVER on the 100ms previewTickMsg animation
		// tick (rule 1's own "no file read on it" - selectedTranscriptPath
		// resolves via a filesystem glob, and previewTickMsg already ran
		// ten times a second before this leg for unrelated reasons; folding
		// the retarget in there measurably raised idle CPU in this leg's
		// own proof and was removed for exactly that reason).
		watchCmd := m.retargetTranscriptWatch()
		return m, tea.Batch(watchCmd, func() tea.Msg {
			time.Sleep(sessionTickInterval)
			return sessionTickMsg{}
		})
	case keyupMsg:
		m.menu.ClearKeydown()
		return m, nil
	case instanceStartDoneMsg:
		m.instanceStarting = false
		inst := msg.instance
		m.startingInstance = nil

		if msg.err != nil {
			// Start failed — remove the instance from the list and show the error.
			m.list.Kill()
			return m, tea.Batch(tea.RequestWindowSize, m.instanceChanged(), m.handleError(msg.err))
		}

		// Save after successful start.
		if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
			return m, m.handleError(err)
		}

		if m.promptAfterName {
			m.state = statePrompt
			m.menu.SetState(ui.StatePrompt)
			m.textInputOverlay = overlay.NewTextInputOverlay("Enter prompt", "")
			m.promptAfterName = false
		} else {
			m.showHelpScreen(helpStart(inst), nil)
		}

		return m, tea.Batch(tea.RequestWindowSize, m.instanceChanged())
	case metadataUpdateDoneMsg:
		for _, r := range msg.results {
			// Skip instances that were paused while metadata was being computed
			if r.instance.Status == session.Paused {
				continue
			}
			if r.updated {
				r.instance.SetStatus(session.Running)
			} else if r.hasPrompt {
				r.instance.TapEnter()
			} else {
				r.instance.SetStatus(session.Ready)
			}
			if r.diffStats != nil && r.diffStats.Error != nil {
				if !strings.Contains(r.diffStats.Error.Error(), "base commit SHA not set") {
					log.WarningLog.Printf("could not update diff stats: %v", r.diffStats.Error)
				}
				r.instance.SetDiffStats(nil)
			} else {
				r.instance.SetDiffStats(r.diffStats)
			}
			r.instance.SetContextFill(r.fillPct, r.fillOK)
		}
		return m, tickUpdateMetadataCmd(m.snapshotActiveInstances(), m.list.GetSelectedInstance())
	case feedTickMsg:
		// Adopt any instance the store holds but this process's own list
		// does not (defect 1's read-side half, see Storage.UntrackedInstances):
		// a lane clarity-attach registers from outside the running cockpit
		// (main.go, ~line 215) appears here within one feed tick, no
		// restart needed. Runs before the external-lane scan below so a
		// freshly-adopted lane's own transcript is excluded from that scan
		// on this same tick, not shown as an external row for one tick
		// first.
		m.adoptUntrackedInstances()

		if m.laneTailCache == nil {
			m.laneTailCache = clarity.NewLaneTailCache()
		}
		now := time.Now()

		// Exactly one read of the fleet's ranked queue file per tick - see
		// clarity.RankedNeedsYou's doc comment. This self-reschedules the
		// same way previewTickMsg/tickUpdateMetadataCmd do above: message-
		// driven, never a blocking polling loop.
		needsYouItems, needsYouStatus := clarity.RankedNeedsYou(clarity.DefaultFeedPath(), feedTopN)
		m.list.SetNeedsYou(needsYouItems, needsYouStatus)
		// Slice 23 rule 3: fly to Needs you on a genuinely new row - nil-guarded
		// the same way this tick's other tabbedWindow calls below are (a
		// lightweight test home built without one, e.g. fit_test.go's own
		// context-fill fixture, must still take this tick cleanly).
		if m.tabbedWindow != nil {
			m.tabbedWindow.NoticeNeedsYou(needsYouItems)
		}

		// Refresh the external-lane rows on this same tick - exactly one
		// glob per tick (clarity.DiscoverExternalLanes), same cadence as
		// the feed above, never a tick of its own (the brief's requirement).
		// Excluded by working-directory path (DEDUPE defect's root-cause
		// fix), never by a name derived from either side's own transcript
		// directory encoding.
		trackedPaths := make([]string, 0, m.list.NumInstances())
		for _, inst := range m.list.GetInstances() {
			trackedPaths = append(trackedPaths, inst.Path)
		}
		if external, err := clarity.DiscoverExternalLanes(clarity.TrackedExclusionPaths(trackedPaths)); err != nil {
			log.WarningLog.Printf("discover external lanes failed: %v", err)
		} else {
			// The state word every lane row now carries (item 1): read
			// through the shared cache keyed by each lane's own transcript
			// path, so an external lane whose file has not changed since
			// the last tick is not reparsed.
			for i := range external {
				if tail, err := m.laneTailCache.Get(external[i].TranscriptPath, 0, now); err == nil {
					external[i].State = tail.State
					external[i].LastTurn = tail.LastTurn
					external[i].StateOK = true
				}
			}
			m.list.SetExternal(external)
		}

		// Same state derivation for every TRACKED instance, running or
		// paused - a lane's transcript is exactly as file-only-derivable
		// either way (see the context-fill comment just below, which
		// applies for the identical reason).
		for _, inst := range m.list.GetInstances() {
			path, ok := clarity.NewestTranscript(inst.Path)
			if !ok {
				continue
			}
			tail, err := m.laneTailCache.Get(path, 0, now)
			if err != nil {
				continue
			}
			inst.SetLaneState(tail.State, tail.LastTurn, true)
		}

		// Context-fill for Paused tracked instances (the OWN ROW defect's
		// "ctx n/a"): tickUpdateMetadataCmd's 500ms loop only computes
		// context fill for snapshotActiveInstances() (Started and not
		// Paused), because its OTHER two computations - HasUpdated and the
		// diff stats - genuinely need a live tmux session. Context fill
		// does not: clarity.ContextFillForLane reads the instance's own
		// transcript file by path, exactly the same file-only derivation
		// DiscoverExternalLanes' ReadFill call above already does
		// synchronously on this same tick for every external lane. A
		// tracked instance is Paused far more often than an external lane
		// is absent, and its transcript is exactly as file-only-derivable
		// while paused as while running - so this closes that gap the same
		// way, on the same cadence, rather than leaving every paused lane's
		// gauge permanently stuck wherever it was when it was last active.
		for _, inst := range m.list.GetInstances() {
			if inst.Status != session.Paused {
				continue
			}
			pct, ok := inst.ComputeContextFill()
			inst.SetContextFill(pct, ok)
		}

		// The Session tab's own turns/header now refresh on sessionTickMsg's
		// 500ms cadence (the Latency ruling, slice 12) - only the splash's
		// fleet counters (unrelated to the selected lane's own data, and in
		// no hurry to update faster) still ride this 3s tick.
		m.updateSessionFleetCounts()

		// Needs-you tab data (slice 5): re-read on every tick too, so a
		// board fetch that resolved between ticks (or a re-ranked queue
		// changing the selected row's own title/class) shows up without
		// waiting on a key press.
		needsYouCmd := m.refreshNeedsYouTab()

		return m, tea.Batch(needsYouCmd, func() tea.Msg {
			time.Sleep(feedRefreshInterval)
			return feedTickMsg{}
		})
	case tea.MouseWheelMsg:
		// Handle mouse wheel events for scrolling the diff/preview pane
		if msg.Button == tea.MouseWheelDown || msg.Button == tea.MouseWheelUp {
			selected := m.list.GetSelectedInstance()
			if selected == nil || selected.Status == session.Paused {
				return m, nil
			}

			switch msg.Button {
			case tea.MouseWheelUp:
				m.tabbedWindow.ScrollUp()
			case tea.MouseWheelDown:
				m.tabbedWindow.ScrollDown()
			}
		}
		return m, nil
	case branchSearchDebounceMsg:
		// Debounce timer fired — check if this is still the current filter version
		if m.textInputOverlay == nil {
			return m, nil
		}
		if msg.version != m.textInputOverlay.BranchFilterVersion() {
			return m, nil // stale, a newer debounce is pending
		}
		return m, m.runBranchSearch(msg.filter, msg.version)
	case branchSearchResultMsg:
		if m.textInputOverlay != nil {
			m.textInputOverlay.SetBranchResults(msg.branches, msg.version)
		}
		return m, nil
	case tea.KeyPressMsg:
		// Any key press during the splash hands off to the list and does
		// nothing else this tick - the brief's "hand-off... on any key
		// press", not also acted on as a list command.
		if m.splashModel != nil {
			m.splashModel.HandleKey()
			m.splashModel = nil
			return m, nil
		}
		return m.handleKeyPress(msg)
	case tea.WindowSizeMsg:
		m.updateHandleWindowSizeEvent(msg)
		if m.splashModel != nil {
			m.splashModel.SetSize(msg.Width, msg.Height)
		}
		return m, nil
	case error:
		// Handle errors from confirmation actions
		return m, m.handleError(msg)
	case instanceChangedMsg:
		// Handle instance changed after confirmation action
		return m, m.instanceChanged()
	case instanceAttachFinishedMsg:
		// A tracked instance's tea.Exec attach (attachInstanceCmd) returned -
		// detached (ctrl-q) or never started. Either way the terminal is back
		// under bubbletea's own control by the time this message is handled
		// (tea.Exec's callback runs after RestoreTerminal - exec.go).
		m.state = stateDefault
		if msg.err != nil {
			return m, m.handleError(msg.err)
		}
		return m, m.instanceChanged()
	case terminalAttachFinishedMsg:
		// A Terminal-tab external-lane attach (attachTerminalCmd) returned.
		m.state = stateDefault
		if msg.err != nil {
			return m, m.setStatus("no terminal for this lane yet: press tab to Terminal first")
		}
		return m, nil
	case instanceStartedMsg:
		// Select the instance that just started (or failed)
		m.list.SelectInstance(msg.instance)

		if msg.err != nil {
			m.list.Kill()
			return m, tea.Batch(m.handleError(msg.err), m.instanceChanged())
		}

		// Save after successful start
		if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
			return m, m.handleError(err)
		}
		if m.autoYes {
			msg.instance.AutoYes = true
		}

		if msg.promptAfterName {
			m.state = statePrompt
			m.menu.SetState(ui.StatePrompt)
			m.textInputOverlay = m.newPromptOverlay()
		} else {
			// If instance has a prompt (set from Shift+N flow), send it now
			if msg.instance.Prompt != "" {
				if err := msg.instance.SendPrompt(msg.instance.Prompt); err != nil {
					log.ErrorLog.Printf("failed to send prompt: %v", err)
				}
				msg.instance.Prompt = ""
			}
			m.menu.SetState(ui.StateDefault)
			m.showHelpScreen(helpStart(msg.instance), nil)
		}

		return m, tea.Batch(tea.RequestWindowSize, m.instanceChanged())
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case composerResultMsg:
		m.state = stateDefault
		m.menu.SetState(ui.StateDefault)
		if msg.err != nil {
			m.composer.Close()
			return m, m.handleError(msg.err)
		}
		m.composer.SetResult(msg.result)
		return m, nil
	case boardFetchedMsg:
		return m, m.refreshNeedsYouTab()
	case transcriptChangedMsg:
		if msg.gen != m.transcriptWatchGen {
			// Stale: a later retarget (the selection moved on) already
			// superseded the watch this event came from.
			return m, nil
		}
		m.transcriptDebounceGen++
		gen := m.transcriptDebounceGen
		return m, tea.Batch(
			func() tea.Msg {
				time.Sleep(transcriptDebounceInterval)
				return transcriptDebounceMsg{gen: gen}
			},
			watchTranscriptCmd(m.transcriptWatcher, msg.gen),
		)
	case transcriptDebounceMsg:
		if msg.gen != m.transcriptDebounceGen {
			// A newer event superseded this one inside the debounce window
			// (rule 3: several writes collapse into one read).
			return m, nil
		}
		if m.laneTailCache == nil {
			m.laneTailCache = clarity.NewLaneTailCache()
		}
		m.updateSessionTabInfo(time.Now())
		return m, nil
	}
	return m, nil
}

func (m *home) handleQuit() (tea.Model, tea.Cmd) {
	if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
		return m, m.handleError(err)
	}
	// Kill every cached term_ shell (design/cockpit-pane/DECISIONS.md slice
	// 6's own "killed when the cockpit quits, exactly as upstream tears
	// down its term_ shells") - a tracked row's own term_<title> shell and
	// an external lane's own term_<lane> shell alike (slice 15: the
	// Terminal tab is always a shell now, never a mirror of the instance's
	// own Claude session, so CleanupTerminal closes a tracked row's shell
	// too).
	if m.tabbedWindow != nil {
		m.tabbedWindow.CleanupTerminal()
	}
	// Close the fsnotify watch (slice 14 rule 3's own "closes on quit") -
	// its background loop goroutine would otherwise outlive the program.
	if m.transcriptWatcher != nil {
		if err := m.transcriptWatcher.Close(); err != nil {
			log.WarningLog.Printf("transcript watch close: %v", err)
		}
	}
	return m, tea.Quit
}

func (m *home) handleMenuHighlighting(msg tea.KeyPressMsg) (cmd tea.Cmd, returnEarly bool) {
	// Handle menu highlighting when you press a button. We intercept it here and immediately return to
	// update the ui while re-sending the keypress. Then, on the next call to this, we actually handle the keypress.
	if m.keySent {
		m.keySent = false
		return nil, false
	}
	// stateSessionPicker (slice 22, PART B) joins stateMsg here - the v-key
	// picker's own up/down/c/esc dispatch is direct and single-pass, same
	// reason stateMsg's composer typing is: the menu has no highlight state
	// for any of those keys while a modal like this owns them outright.
	if m.state == statePrompt || m.state == stateHelp || m.state == stateConfirm || m.state == stateMsg || m.state == stateSessionPicker {
		return nil, false
	}
	// If it's in the global keymap, we should try to highlight it.
	name, ok := keys.GlobalKeyStringsMap[msg.String()]
	if !ok {
		return nil, false
	}

	if m.list.GetSelectedInstance() != nil && m.list.GetSelectedInstance().Paused() && name == keys.KeyEnter {
		return nil, false
	}
	if name == keys.KeyShiftDown || name == keys.KeyShiftUp {
		return nil, false
	}

	// Skip the menu highlighting if the key is not in the map or we are using the shift up and down keys.
	// TODO: cleanup: when you press enter on stateNew, we use keys.KeySubmitName. We should unify the keymap.
	if name == keys.KeyEnter && m.state == stateNew {
		name = keys.KeySubmitName
	}
	m.keySent = true
	return tea.Batch(
		func() tea.Msg { return msg },
		m.keydownCallback(name)), true
}

func (m *home) handleKeyPress(msg tea.KeyPressMsg) (mod tea.Model, cmd tea.Cmd) {
	cmd, returnEarly := m.handleMenuHighlighting(msg)
	if returnEarly {
		return m, cmd
	}

	if m.state == stateHelp {
		return m.handleHelpState(msg)
	}

	if m.state == stateNew {
		// Handle quit commands first. Don't handle q because the user might want to type that.
		if msg.String() == "ctrl+c" {
			m.state = stateDefault
			m.promptAfterName = false
			m.list.Kill()
			return m, tea.Sequence(
				tea.RequestWindowSize,
				func() tea.Msg {
					m.menu.SetState(ui.StateDefault)
					return nil
				},
			)
		}

		instance := m.list.GetInstances()[m.list.NumInstances()-1]
		switch msg.Code {
		// Start the instance (enable previews etc) and go back to the main menu state.
		case tea.KeyEnter:
			if len(instance.Title) == 0 {
				return m, m.handleError(fmt.Errorf("title cannot be empty"))
			}

			// If promptAfterName, show prompt+branch overlay before starting
			if m.promptAfterName {
				m.promptAfterName = false
				m.state = statePrompt
				m.menu.SetState(ui.StatePrompt)
				m.textInputOverlay = m.newPromptOverlay()
				// Trigger initial branch search (no debounce, version 0)
				initialSearch := m.runBranchSearch("", m.textInputOverlay.BranchFilterVersion())
				return m, tea.Batch(tea.RequestWindowSize, initialSearch)
			}

			// Set Loading status and finalize into the list immediately
			instance.SetStatus(session.Loading)
			m.newInstanceFinalizer()
			m.promptAfterName = false
			m.state = stateDefault
			m.menu.SetState(ui.StateDefault)

			// Return a tea.Cmd that runs instance.Start in the background
			startCmd := func() tea.Msg {
				err := instance.Start(true)
				return instanceStartedMsg{
					instance:        instance,
					err:             err,
					promptAfterName: false,
				}
			}

			return m, tea.Batch(tea.RequestWindowSize, m.instanceChanged(), startCmd)
		case tea.KeyBackspace:
			runes := []rune(instance.Title)
			if len(runes) == 0 {
				return m, nil
			}
			if err := instance.SetTitle(string(runes[:len(runes)-1])); err != nil {
				return m, m.handleError(err)
			}
		case tea.KeySpace:
			if err := instance.SetTitle(instance.Title + " "); err != nil {
				return m, m.handleError(err)
			}
		case tea.KeyEsc:
			m.list.Kill()
			m.state = stateDefault
			m.instanceChanged()

			return m, tea.Sequence(
				tea.RequestWindowSize,
				func() tea.Msg {
					m.menu.SetState(ui.StateDefault)
					return nil
				},
			)
		default:
			// Any other printable text (was tea.KeyRunes in v1 - v2 has no
			// distinct rune sentinel, printable keys just carry msg.Text).
			if msg.Text != "" {
				if runewidth.StringWidth(instance.Title) >= 32 {
					return m, m.handleError(fmt.Errorf("title cannot be longer than 32 characters"))
				}
				if err := instance.SetTitle(instance.Title + msg.Text); err != nil {
					return m, m.handleError(err)
				}
			}
		}
		return m, nil
	} else if m.state == statePrompt {
		// Handle cancel via ctrl+c before delegating to the overlay
		if msg.String() == "ctrl+c" {
			return m, m.cancelPromptOverlay()
		}

		// Use the new TextInputOverlay component to handle all key events
		shouldClose, branchFilterChanged := m.textInputOverlay.HandleKeyPress(msg)

		// Check if the form was submitted or canceled
		if shouldClose {
			selected := m.list.GetSelectedInstance()
			if selected == nil {
				return m, nil
			}

			if m.textInputOverlay.IsCanceled() {
				return m, m.cancelPromptOverlay()
			}

			if m.textInputOverlay.IsSubmitted() {
				prompt := m.textInputOverlay.GetValue()
				selectedBranch := m.textInputOverlay.GetSelectedBranch()
				selectedProgram := m.textInputOverlay.GetSelectedProgram()

				if !selected.Started() {
					// Shift+N flow: instance not started yet — set branch, start, then send prompt
					if selectedBranch != "" {
						selected.SetSelectedBranch(selectedBranch)
					}
					if selectedProgram != "" {
						selected.Program = selectedProgram
					}
					selected.Prompt = prompt

					// Finalize into list and start
					selected.SetStatus(session.Loading)
					m.newInstanceFinalizer()
					m.textInputOverlay = nil
					m.state = stateDefault
					m.menu.SetState(ui.StateDefault)

					startCmd := func() tea.Msg {
						err := selected.Start(true)
						return instanceStartedMsg{
							instance:        selected,
							err:             err,
							promptAfterName: false,
							selectedBranch:  selectedBranch,
						}
					}

					return m, tea.Batch(tea.RequestWindowSize, m.instanceChanged(), startCmd)
				}

				// Regular flow: instance already running, just send prompt
				if err := selected.SendPrompt(prompt); err != nil {
					return m, m.handleError(err)
				}
			}

			// Close the overlay and reset state
			m.textInputOverlay = nil
			m.state = stateDefault
			return m, tea.Sequence(
				tea.RequestWindowSize,
				func() tea.Msg {
					m.menu.SetState(ui.StateDefault)
					m.showHelpScreen(helpStart(selected), nil)
					return nil
				},
			)
		}

		// Schedule a debounced branch search if the filter changed
		if branchFilterChanged {
			filter := m.textInputOverlay.BranchFilter()
			version := m.textInputOverlay.BranchFilterVersion()
			return m, m.scheduleBranchSearch(filter, version)
		}

		return m, nil
	}

	// Handle the composer (m key): typing, enter to send, esc/ctrl+c to
	// close - the mock-up's own inline box (ui.Composer), not the generic
	// full-screen textInputOverlay the "new instance"/"enter prompt" flows
	// use.
	if m.state == stateMsg {
		if msg.String() == "ctrl+c" || msg.Code == tea.KeyEsc {
			m.composer.Close()
			m.state = stateDefault
			m.menu.SetState(ui.StateDefault)
			return m, nil
		}
		switch msg.Code {
		case tea.KeyEnter:
			lane, isExternal := m.composer.Lane(), m.composer.IsExternal()
			if lane == "" {
				// Neither the board card's own Lane field nor the issue's
				// lane: label resolved (board #280, slice 5b, DEFECT 2) -
				// enter delivers nothing; the composer stays visible and
				// shows why, the same way a successful send shows its own
				// landed foot.
				m.composer.SetResult("no lane to send to")
				m.state = stateDefault
				m.menu.SetState(ui.StateDefault)
				return m, nil
			}
			text := m.composer.Value()
			if strings.TrimSpace(text) == "" {
				m.composer.Close()
				m.state = stateDefault
				m.menu.SetState(ui.StateDefault)
				return m, nil
			}
			return m, m.sendComposerCmd(lane, isExternal, text)
		case tea.KeyBackspace:
			m.composer.Backspace()
			return m, nil
		default:
			if msg.Text != "" {
				m.composer.Type(msg.Text)
			}
			return m, nil
		}
	}

	// Handle confirmation state
	if m.state == stateConfirm {
		shouldClose := m.confirmationOverlay.HandleKeyPress(msg)
		if shouldClose {
			m.state = stateDefault
			m.confirmationOverlay = nil
			return m, nil
		}
		return m, nil
	}

	// Handle the Session tab's turn picker (v key, slice 22 PART B): esc/
	// ctrl+c leave it (never the global quit - same shape as stateMsg's own
	// ctrl+c above), up/down move the highlight, c copies it. Any other key
	// is swallowed - the picker is modal, same as the composer.
	if m.state == stateSessionPicker {
		if msg.String() == "ctrl+c" || msg.Code == tea.KeyEsc {
			m.sessionPane.ClosePicker()
			m.state = stateDefault
			return m, nil
		}
		name, ok := keys.GlobalKeyStringsMap[msg.String()]
		if !ok {
			return m, nil
		}
		switch name {
		case keys.KeyUp:
			m.sessionPane.PickerOlder()
		case keys.KeyDown:
			m.sessionPane.PickerNewer()
		case keys.KeyCopy:
			if text, lines, ok := m.sessionPane.PickerCopyText(); ok {
				if err := clarity.CopyToClipboard(m.cmdExec, text); err != nil {
					return m, m.handleError(err)
				}
				return m, m.setStatus(fmt.Sprintf("copied · turn (%d lines)", lines))
			}
		}
		return m, nil
	}

	// Exit scrolling mode when ESC is pressed and the terminal pane is in
	// scrolling mode. The Session tab has no equivalent "scroll mode" to
	// exit - its turns are already fully loaded (bounded by maxTurns), so
	// shift+up/down scroll it immediately with nothing to reset.
	// Always check for escape key first to ensure it doesn't get intercepted elsewhere
	if msg.Code == tea.KeyEsc {
		if m.tabbedWindow.IsInTerminalTab() && m.tabbedWindow.IsTerminalInScrollMode() {
			m.tabbedWindow.ResetTerminalToNormalMode()
			return m, m.instanceChanged()
		}
	}

	// Handle quit commands first
	if msg.String() == "ctrl+c" || msg.String() == "q" {
		return m.handleQuit()
	}

	name, ok := keys.GlobalKeyStringsMap[msg.String()]
	if !ok {
		return m, nil
	}

	switch name {
	case keys.KeyHelp:
		return m.showHelpScreen(helpTypeGeneral{}, nil)
	case keys.KeyPrompt:
		if m.list.NumInstances() >= GlobalInstanceLimit {
			return m, m.handleError(
				fmt.Errorf("you can't create more than %d instances", GlobalInstanceLimit))
		}

		// Start a background fetch so branches are up to date by the time the picker opens
		fetchCmd := func() tea.Msg {
			currentDir, _ := os.Getwd()
			git.FetchBranches(currentDir)
			return nil
		}

		instance, err := session.NewInstance(session.InstanceOptions{
			Title:   "",
			Path:    ".",
			Program: m.program,
		})
		if err != nil {
			return m, m.handleError(err)
		}

		m.newInstanceFinalizer = m.list.AddInstance(instance)
		m.list.SetSelectedInstance(m.list.NumInstances() - 1)
		m.state = stateNew
		m.menu.SetState(ui.StateNewInstance)
		m.promptAfterName = true

		return m, fetchCmd
	case keys.KeyNew:
		if m.list.NumInstances() >= GlobalInstanceLimit {
			return m, m.handleError(
				fmt.Errorf("you can't create more than %d instances", GlobalInstanceLimit))
		}
		instance, err := session.NewInstance(session.InstanceOptions{
			Title:   "",
			Path:    ".",
			Program: m.program,
		})
		if err != nil {
			return m, m.handleError(err)
		}

		m.newInstanceFinalizer = m.list.AddInstance(instance)
		m.list.SetSelectedInstance(m.list.NumInstances() - 1)
		m.state = stateNew
		m.menu.SetState(ui.StateNewInstance)

		return m, nil
	case keys.KeyUp:
		m.list.Up()
		return m, tea.Batch(m.instanceChanged(), m.selectionChanged())
	case keys.KeyDown:
		m.list.Down()
		return m, tea.Batch(m.instanceChanged(), m.selectionChanged())
	case keys.KeyShiftUp:
		m.tabbedWindow.ScrollUp()
		return m, m.instanceChanged()
	case keys.KeyShiftDown:
		m.tabbedWindow.ScrollDown()
		return m, m.instanceChanged()
	case keys.KeyTab:
		m.tabbedWindow.Toggle()
		m.rememberTabForCurrentRowKind()
		m.menu.SetActiveTab(m.tabbedWindow.GetActiveTab())
		return m, m.instanceChanged()
	case keys.KeyKill:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}

		// Create the kill action as a tea.Cmd
		killAction := func() tea.Msg {
			// clarity-attach instances have no git worktree, so there is no
			// branch checkout state to protect - skip straight to cleanup.
			if selected.HasWorktree() {
				worktree, err := selected.GetGitWorktree()
				if err != nil {
					return err
				}

				checkedOut, err := worktree.IsBranchCheckedOut()
				if err != nil {
					return err
				}

				if checkedOut {
					return fmt.Errorf("instance %s is currently checked out", selected.Title)
				}
			}

			// Delete from storage first
			if err := m.storage.DeleteInstance(selected.Title); err != nil {
				return err
			}

			// Then kill the instance
			m.list.Kill()
			return instanceChangedMsg{}
		}

		// Show confirmation modal
		message := fmt.Sprintf("[!] Kill session '%s'?", selected.Title)
		return m, m.confirmAction(message, killAction)
	case keys.KeySubmit:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}

		// Create the push action as a tea.Cmd
		pushAction := func() tea.Msg {
			if !selected.HasWorktree() {
				return fmt.Errorf("instance %s has no git worktree to push (clarity-attach instance)", selected.Title)
			}
			// Default commit message with timestamp
			commitMsg := fmt.Sprintf("[claudesquad] update from '%s' on %s", selected.Title, time.Now().Format(time.RFC822))
			worktree, err := selected.GetGitWorktree()
			if err != nil {
				return err
			}
			if err = worktree.PushChanges(commitMsg, true); err != nil {
				return err
			}
			return nil
		}

		// Show confirmation modal
		message := fmt.Sprintf("[!] Push changes from session '%s'?", selected.Title)
		return m, m.confirmAction(message, pushAction)
	case keys.KeyCheckout:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}

		// Show help screen before pausing
		return m.showHelpScreen(helpTypeInstanceCheckout{}, func() tea.Cmd {
			if err := selected.Pause(); err != nil {
				return m.handleError(err)
			}
			return m.instanceChanged()
		})
	case keys.KeyMoveUp:
		if m.list.MoveUp() {
			if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
				return m, m.handleError(err)
			}
			return m, m.instanceChanged()
		}
		return m, nil
	case keys.KeyMoveDown:
		if m.list.MoveDown() {
			if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
				return m, m.handleError(err)
			}
			return m, m.instanceChanged()
		}
		return m, nil
	case keys.KeyMsg:
		lane, isExternal, ok := m.composerTarget()
		if !ok {
			return m, nil
		}
		m.composer.Open(lane, isExternal)
		m.state = stateMsg
		m.menu.SetState(ui.StateMsg)
		return m, nil
	case keys.KeyResume:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}
		if selected.NoWorktree && !noWorktreeResumeAllowed(selected) {
			// Resuming a NoWorktree instance spins up a SECOND tmux session
			// running the SAME program in the SAME folder the owner's own
			// terminal already runs it in - only safe when that terminal-
			// side session looks abandoned (slice 8 rule 2; the rule is
			// also stated in the general help screen).
			return m, m.setStatus("the lane is live in your own terminal; nothing to resume")
		}
		if err := selected.Resume(); err != nil {
			return m, m.handleError(err)
		}
		return m, tea.RequestWindowSize
	case keys.KeyEnter:
		// Terminal tab, external row: attach to that lane's own term_<lane>
		// shell if it has one already (opened lazily the first time the tab
		// was viewed for it - UpdateTerminal/ensureExternalSessionLocked),
		// else say so in the footer. This branches BEFORE the tracked-only
		// guard below, since GetSelectedInstance() is nil for an external
		// row and would otherwise fall straight through to the no-op
		// default case (this is the one case in this leg where that no-op
		// is not the right answer - see DECISIONS.md slice 6).
		if m.tabbedWindow.IsInTerminalTab() {
			if ext, ok := m.list.GetSelectedExternalLane(); ok {
				return m.showHelpScreen(helpTypeInstanceAttach{}, func() tea.Cmd {
					return attachTerminalCmd(func() (chan struct{}, error) {
						return m.tabbedWindow.AttachTerminal(ext.Name)
					})
				})
			}
		}
		if m.list.NumInstances() == 0 {
			return m, nil
		}
		selected := m.list.GetSelectedInstance()
		if selected == nil {
			return m, nil
		}
		if selected.NoWorktree && !selected.TmuxAlive() {
			// This lane runs in the owner's own terminal (a Clarity session
			// lane started via clarity-attach) - attaching here would start
			// a SECOND Claude in the same folder (slice 8 rule 2). Say so
			// plainly rather than silently no-op-ing or (the pre-fix bug)
			// walking a git-worktree resume path that does not exist for a
			// NoWorktree instance.
			return m, m.setStatus("this lane runs in your own terminal; tab to Terminal for a shell in its folder")
		}
		if selected.Paused() || selected.Status == session.Loading || !selected.TmuxAlive() {
			return m, nil
		}
		// A tracked row attaches through its own tmux session regardless of
		// which tab is active (DECISIONS.md slice 6: "Enter on a tracked row
		// still attaches, upstream behaviour") - the Terminal tab merely
		// mirrors that same session, it never owns a separate one for a
		// tracked instance.
		return m.showHelpScreen(helpTypeInstanceAttach{}, func() tea.Cmd {
			return attachInstanceCmd(m.list.Attach)
		})
	case keys.KeyCopy:
		return m, m.handleCopy()
	case keys.KeyCopyTail:
		return m, m.handleCopyTail()
	case keys.KeyTurnPicker:
		return m, m.handleOpenPicker()
	case keys.KeyOpenFolder:
		return m, m.handleOpenFolder()
	case keys.KeyButterflyToggle:
		if m.tabbedWindow != nil {
			m.tabbedWindow.ToggleButterflyEnabled()
		}
		return m, nil
	default:
		return m, nil
	}
}

// handleCopy is the c key (design/cockpit-pane/DECISIONS.md slice 7,
// extended by slice 22 PART B): the composer's current text when it is
// open; else the selected Needs-you row's own title and number
// (clarity.FeedLine - the exact text the row itself renders, "#nnn -
// <title>" for a board row) when the cursor sits on one - unchanged from
// slice 7, tab-independent (GetSelectedNeedsYou only ever returns ok=true
// for a Needs-you row's own selection, wherever the cursor happens to be);
// else, on the Session tab, the SELECTED lane's own last turn as plain text
// (the owner's own complaint this slice answers: "cant copy paste from the
// session"). Copied to the system clipboard via the same helper the
// external-lane message path already uses. A no-op when none of the three
// applies - never a claimed copy of nothing.
func (m *home) handleCopy() tea.Cmd {
	if m.composer.IsOpen() {
		text := m.composer.Value()
		if text == "" {
			return nil
		}
		if err := clarity.CopyToClipboard(m.cmdExec, text); err != nil {
			return m.handleError(err)
		}
		return m.setStatus("copied")
	}

	if item, ok := m.list.GetSelectedNeedsYou(); ok {
		text := clarity.FeedLine(item)
		if text == "" {
			return nil
		}
		if err := clarity.CopyToClipboard(m.cmdExec, text); err != nil {
			return m.handleError(err)
		}
		return m.setStatus("copied")
	}

	if m.tabbedWindow == nil || !m.tabbedWindow.IsInSessionTab() {
		return nil
	}
	text, lines, ok := m.sessionPane.LastTurnCopyText()
	if !ok {
		return nil
	}
	if err := clarity.CopyToClipboard(m.cmdExec, text); err != nil {
		return m.handleError(err)
	}
	return m.setStatus(fmt.Sprintf("copied · last turn (%d lines)", lines))
}

// handleCopyTail is the C (shift-c) key (slice 22, PART B): copies the
// Session tab's WHOLE loaded transcript tail as plain text - every turn
// currently held (SessionPane.TailCopyText's own contract), not only the
// lines scrolled into view. A no-op off the Session tab, or when nothing is
// selected/the selected lane has no turns yet.
func (m *home) handleCopyTail() tea.Cmd {
	if m.tabbedWindow == nil || !m.tabbedWindow.IsInSessionTab() {
		return nil
	}
	text, turns, ok := m.sessionPane.TailCopyText()
	if !ok {
		return nil
	}
	if err := clarity.CopyToClipboard(m.cmdExec, text); err != nil {
		return m.handleError(err)
	}
	return m.setStatus(fmt.Sprintf("copied · %d turns", turns))
}

// handleOpenPicker is the v key (slice 22, PART B): opens the Session tab's
// turn picker (SessionPane.OpenPicker), entering stateSessionPicker so
// handleKeyPress's own early block above routes up/down/c/esc to it. A
// no-op off the Session tab, or when the selected lane has no turns to pick
// from (OpenPicker's own refusal) - the state is never entered with nothing
// in the picker.
func (m *home) handleOpenPicker() tea.Cmd {
	if m.tabbedWindow == nil || !m.tabbedWindow.IsInSessionTab() {
		return nil
	}
	if !m.sessionPane.OpenPicker() {
		return nil
	}
	m.state = stateSessionPicker
	return nil
}

// handleOpenFolder is the o key (design/cockpit-pane/DECISIONS.md slice 7):
// opens the selected lane's own folder (a tracked instance's worktree path,
// or an external lane's WorkDir) with macOS `open`, through the same
// cmd.Executor seam the clipboard helper uses (so a test can inject a fake
// without ever shelling out for real). A no-op when the selection resolves
// to no path at all (nothing selected, or an external lane whose transcript
// scan never found a cwd).
func (m *home) handleOpenFolder() tea.Cmd {
	path := m.selectedFolderPath()
	if path == "" {
		return nil
	}
	if err := m.cmdExec.Run(exec.Command("open", path)); err != nil {
		return m.handleError(err)
	}
	return m.setStatus(fmt.Sprintf("opened %s", path))
}

// selectedFolderPath resolves the CURRENT selection's own folder: a tracked
// instance's git worktree path, or an external lane's own working
// directory ("" when neither is selected, or an external lane's own cwd
// scan never found one).
func (m *home) selectedFolderPath() string {
	if selected := m.list.GetSelectedInstance(); selected != nil {
		// A NoWorktree instance (slice 8 rule 4) has no git worktree, so
		// GetWorktreePath always returns "" for it - fall back to its own
		// Path (the lane directory it runs in directly) instead of a no-op.
		if !selected.HasWorktree() {
			return selected.Path
		}
		return selected.GetWorktreePath()
	}
	if ext, ok := m.list.GetSelectedExternalLane(); ok {
		return ext.WorkDir
	}
	return ""
}

// noWorktreeResumeAllowed is the r key's own gate for a NoWorktree tracked
// instance (slice 8 rule 2): resuming spins up a SECOND tmux session
// running the SAME program in the SAME folder the owner's own terminal
// already runs it in, so it is only safe when that terminal-side session
// looks abandoned - the lane's own transcript classifies as idle or
// stalled (clarity.ClassifyState: "stalled" already means "no close in
// over 10 minutes", so no separate age check is needed on top of it).
// Anything else - working, waiting on you, or no transcript read yet at
// all - refuses: never risk a duplicate live Claude in one folder.
func noWorktreeResumeAllowed(inst *session.Instance) bool {
	state, _, ok := inst.GetLaneState()
	if !ok {
		return false
	}
	return state == clarity.StateIdle || state == clarity.StateStalled
}

// instanceChanged updates the Terminal tab and the menu based on whichever
// row is currently selected (tracked, external, or neither). Neither the
// Session tab nor the Needs-you tab is touched here - both come from the
// feed tick only (see feedTickMsg's own blocks below), on the SELECTED row
// whichever kind it is.
func (m *home) instanceChanged() tea.Cmd {
	selected := m.list.GetSelectedInstance()
	_, isExternal := m.list.GetSelectedExternalLane()
	_, isNeedsYou := m.list.GetSelectedNeedsYou()

	// Update menu with current instance
	m.menu.SetInstance(selected, isExternal, isNeedsYou)

	if err := m.tabbedWindow.UpdateTerminal(m.terminalTarget()); err != nil {
		return m.handleError(err)
	}
	return nil
}

// terminalTarget resolves the Terminal tab's own per-tick input (design/
// cockpit-pane/DECISIONS.md slice 6) for whichever row is currently
// selected: a tracked instance's own mirror, an external lane's term_ shell,
// or neither (the resting frame).
func (m *home) terminalTarget() ui.TerminalTarget {
	if selected := m.list.GetSelectedInstance(); selected != nil {
		return ui.TerminalTarget{Kind: ui.TerminalTargetTracked, Instance: selected}
	}
	if ext, ok := m.list.GetSelectedExternalLane(); ok {
		return ui.TerminalTarget{Kind: ui.TerminalTargetExternal, Lane: ext.Name, WorkDir: ext.WorkDir}
	}
	return ui.TerminalTarget{Kind: ui.TerminalTargetNone}
}

// composerTarget resolves the CURRENT selection's own send target: the
// lane name and whether the composer must use the clipboard-copy path (no
// tracked tmux session to deliver into). A tracked or external row
// resolves directly via the list's own SelectedMsgTarget; a Needs-you
// row's own RESOLVED raising lane (needsYouRowLane, board #280 slice 5b
// DEFECT 2) is looked up against the tracked instances and external lanes
// the list currently holds instead, since the row names no group at all
// and may not resolve to either - an unresolved match falls back to
// isExternal=true (copy), the safe default: never claim a delivery this
// cockpit cannot confirm. A row whose lane did not resolve AT ALL (neither
// the board card's Lane field nor its lane: label, nor - for a lane-file
// row, which always resolves - anything at all) returns lane="",
// isExternal=true, ok=true: the composer still opens (its own "no lane on
// this row" state), it simply names no target.
func (m *home) composerTarget() (lane string, isExternal bool, ok bool) {
	if item, isNeedsYou := m.list.GetSelectedNeedsYou(); isNeedsYou {
		lane = m.needsYouRowLane(item)
		if lane == "" {
			return "", true, true
		}
		for _, inst := range m.list.GetInstances() {
			if inst.Title == lane {
				// Same DEFECT 1 rule SelectedMsgTarget applies below: a
				// matched tracked instance with no live tmux session (e.g.
				// a Paused NoWorktree lane) is a copy-only target too, not
				// the tracked send path.
				return lane, inst.RequiresCopyOnlySend(), true
			}
		}
		for _, ext := range m.list.GetExternal() {
			if clarity.MatchesQueriedLane(ext, lane) {
				return lane, true, true
			}
		}
		return lane, true, true
	}
	return m.list.SelectedMsgTarget()
}

// needsYouRowLane resolves one Needs-you row's own raising lane (board
// #280, slice 5b, DEFECT 2): a lane-file-sourced row's own item.Lane
// (laneFromSource, session/clarity/feed.go - this always resolves, the
// directory a STATUS.md/TASKS.md lives in); a board-sourced row's fetched
// card Lane field instead (its own "## Lane" section, falling back to the
// issue's "lane:" label - clarity.ParseBoardBody), "" when that fetch has
// not landed yet, failed, or resolved to nothing at all. Never the raw
// "#<n>" board reference item.Lane itself carries for a board row.
func (m *home) needsYouRowLane(item clarity.FeedItem) string {
	n, isBoard := clarity.BoardIssueNumber(item.Source)
	if !isBoard {
		return item.Lane
	}
	if m.boardCache == nil {
		return ""
	}
	cached, ok := m.boardCache.Peek(n)
	if !ok || cached.Err != "" {
		return ""
	}
	return cached.Lane
}

// selectionChanged is the Up/Down keys' own follow-up to list.Up()/Down():
// the tab-follows-row-kind rule (slice 5) and a fresh Needs-you tab read
// for whichever row is now selected, so a newly selected Needs-you row's
// detail (and any board fetch it needs) starts immediately rather than
// waiting up to feedRefreshInterval for the next tick.
func (m *home) selectionChanged() tea.Cmd {
	m.syncTabToRowKind()
	// The fsnotify watch (slice 14 rule 3's own "moves when the selection
	// changes") follows an explicit Up/Down immediately, rather than
	// waiting up to sessionTickInterval for its own 500ms retarget.
	return tea.Batch(m.retargetTranscriptWatch(), m.refreshNeedsYouTab())
}

// syncTabToRowKind is slice 5's "selecting a Needs-you row changes the
// right pane's active tab to Needs you; selecting a lane row returns it to
// Session (remember the user's own tab choice per row kind so tab does not
// fight the cursor)": the tab is force-switched only on a KIND TRANSITION
// (tracked/external <-> Needs-you), to whichever tab that kind last held -
// never on every Up/Down within the same kind, so a manual Tab press away
// from the default while browsing several rows of one kind in a row is not
// immediately undone by the next Down.
func (m *home) syncTabToRowKind() {
	if m.tabbedWindow == nil {
		return
	}
	kind := m.list.SelectedRowKind()
	if kind == m.prevRowKind {
		return
	}
	if kind == ui.RowKindNeedsYou {
		m.tabbedWindow.SetActiveTab(m.needsYouTab)
	} else {
		m.tabbedWindow.SetActiveTab(m.laneTab)
	}
	m.menu.SetActiveTab(m.tabbedWindow.GetActiveTab())
	m.prevRowKind = kind
}

// rememberTabForCurrentRowKind records a manual Tab press (keys.KeyTab)
// against whichever row kind is currently selected, so syncTabToRowKind
// restores it the next time the cursor returns to that kind.
func (m *home) rememberTabForCurrentRowKind() {
	if m.list.SelectedRowKind() == ui.RowKindNeedsYou {
		m.needsYouTab = m.tabbedWindow.GetActiveTab()
	} else {
		m.laneTab = m.tabbedWindow.GetActiveTab()
	}
}

// refreshNeedsYouTab rebuilds the Needs-you tab's data for whichever row is
// currently selected (nil when it is not a Needs-you row). The board fetch
// itself never runs on this (the UI) thread: a cache hit (clarity.Board-
// Cache.Peek) renders immediately, a miss renders one Loading tick and
// returns a tea.Cmd that fetches in the background and reports back via
// boardFetchedMsg.
func (m *home) refreshNeedsYouTab() tea.Cmd {
	if m.tabbedWindow == nil {
		return nil
	}
	item, ok := m.list.GetSelectedNeedsYou()
	if !ok {
		m.tabbedWindow.SetNeedsYouInfo(nil)
		return nil
	}
	info := &ui.NeedsYouInfo{Item: item}
	n, isBoard := clarity.BoardIssueNumber(item.Source)
	if !isBoard {
		// A lane-file-sourced row (fleet_queue_build.py's lane_rows()) names
		// no board issue at all - there is nothing to fetch a recommendation
		// from, and the feed item itself carries no body/recommendation
		// fields either (session/clarity/feed.go's own FeedItem shape). Its
		// own Lane always resolves (laneFromSource never returns "").
		info.Lane = item.Lane
		m.tabbedWindow.SetNeedsYouInfo(info)
		return nil
	}
	if m.boardCache == nil {
		m.boardCache = clarity.NewBoardCache()
	}
	if cached, ok := m.boardCache.Peek(n); ok {
		if cached.Err != "" {
			info.BoardUnreachable = cached.Err
		} else {
			info.Lane = cached.Lane
			info.Explanation = cached.Explanation
			info.Options = cached.Options
			info.ExpectedReply = cached.ExpectedReply
			info.Also = cached.Also
		}
		m.tabbedWindow.SetNeedsYouInfo(info)
		return nil
	}
	info.Loading = true
	m.tabbedWindow.SetNeedsYouInfo(info)
	return m.fetchBoardCmd(n)
}

// fetchBoardCmd runs BoardCache.Get(n) - the one gh api call - in the
// background; the fetched result lands in the cache itself, so the
// returned message carries nothing but a "go re-read the cache" signal
// (boardFetchedMsg), never a stale copy of what was selected when the
// fetch started.
func (m *home) fetchBoardCmd(n int) tea.Cmd {
	return func() tea.Msg {
		m.boardCache.Get(n)
		return boardFetchedMsg{}
	}
}

type keyupMsg struct{}

// keydownCallback clears the menu option highlighting after 500ms.
func (m *home) keydownCallback(name keys.KeyName) tea.Cmd {
	m.menu.Keydown(name)
	return func() tea.Msg {
		select {
		case <-m.ctx.Done():
		case <-time.After(500 * time.Millisecond):
		}

		return keyupMsg{}
	}
}

// hideErrMsg implements tea.Msg and clears the error text from the screen.
type hideErrMsg struct{}

// hideStatusMsg implements tea.Msg and clears the status text from the screen.
type hideStatusMsg struct{}

// composerResultMsg carries the result of a composer send back to Update:
// either the foot text to show ("sent · landed hh:mm:ss" / "copied · ..."),
// or a delivery error.
type composerResultMsg struct {
	result string
	err    error
}

// boardFetchedMsg signals that a background board-issue fetch (fetchBoard-
// Cmd) has landed in clarity.BoardCache - the Needs-you tab's own data is
// re-read from the cache (refreshNeedsYouTab), never carried on this
// message itself, so a stale row (the selection having moved on while the
// fetch was in flight) is never rendered.
type boardFetchedMsg struct{}

// previewTickMsg implements tea.Msg and triggers a preview update
type previewTickMsg struct{}

type instanceChangedMsg struct{}

type instanceStartedMsg struct {
	instance        *session.Instance
	err             error
	promptAfterName bool
	selectedBranch  string
}

// branchSearchDebounceMsg fires after the debounce interval to trigger a search.
type branchSearchDebounceMsg struct {
	filter  string
	version uint64
}

// branchSearchResultMsg carries search results back to Update.
type branchSearchResultMsg struct {
	branches []string
	version  uint64
}

const branchSearchDebounce = 150 * time.Millisecond

// scheduleBranchSearch returns a debounced tea.Cmd: sleeps, then triggers a search message.
func (m *home) scheduleBranchSearch(filter string, version uint64) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(branchSearchDebounce)
		return branchSearchDebounceMsg{filter: filter, version: version}
	}
}

// runBranchSearch returns a tea.Cmd that performs the git search in the background.
func (m *home) runBranchSearch(filter string, version uint64) tea.Cmd {
	return func() tea.Msg {
		currentDir, _ := os.Getwd()
		branches, err := git.SearchBranches(currentDir, filter)
		if err != nil {
			log.WarningLog.Printf("branch search failed: %v", err)
			return nil
		}
		return branchSearchResultMsg{branches: branches, version: version}
	}
}

// instanceMetaResult holds the results of a single instance's metadata update,
// computed in a background goroutine.
type instanceMetaResult struct {
	instance  *session.Instance
	updated   bool
	hasPrompt bool
	diffStats *git.DiffStats
	fillPct   int
	fillOK    bool
}

// feedTickMsg fires once per "Needs you" feed refresh tick.
type feedTickMsg struct{}

// feedRefreshInterval is how often the feed re-reads the queue file -
// distinct from the 100ms preview tick and the 500ms metadata tick, since
// the fleet queue changes far less often than either.
const feedRefreshInterval = 3 * time.Second

// sessionTickMsg fires once per Session-tab refresh tick - the Latency
// ruling's own cadence (design/cockpit-pane/DECISIONS.md, 3 Sep 09:2x): the
// SELECTED lane's turns, header state glyph and running-tool elapsed text
// all need to move roughly six times as often as the 3s feed tick that
// refreshes every OTHER row, without dragging that fleet-wide work along
// with it - see the sessionTickMsg case in Update, which touches only the
// selected lane's own LaneTail (through the same laneTailCache, so an
// unchanged file still costs one os.Stat, not a reparse).
type sessionTickMsg struct{}

// sessionTickInterval is the Latency ruling's own number, verbatim: 500ms.
const sessionTickInterval = 500 * time.Millisecond

// feedTopN is how many ranked entries the "Needs you" section shows.
const feedTopN = 5

// metadataUpdateDoneMsg is sent when the background metadata update completes.
type metadataUpdateDoneMsg struct {
	results []instanceMetaResult
}

// instanceStartDoneMsg is sent when the background instance start completes.
type instanceStartDoneMsg struct {
	instance *session.Instance
	err      error
}

// runInstanceStartCmd returns a Cmd that performs the expensive instance.Start(true)
// in a background goroutine so the main event loop stays responsive.
func runInstanceStartCmd(instance *session.Instance) tea.Cmd {
	return func() tea.Msg {
		err := instance.Start(true)
		return instanceStartDoneMsg{instance: instance, err: err}
	}
}

// adoptUntrackedInstances folds any instance present in the store but not
// yet in this process's own list into m.list - defect 1's read-side half
// (see session.Storage.UntrackedInstances' doc comment for the write-side
// merge this pairs with). Called once per feedTickMsg, so a lane the
// clarity wrapper registers from outside the running cockpit shows up
// within one feedRefreshInterval, with no restart.
func (m *home) adoptUntrackedInstances() {
	if m.storage == nil {
		// A *home built directly in a test (skipping newHome, see the
		// laneTailCache field's own doc comment) has no storage to read -
		// nothing to adopt from.
		return
	}

	known := make(map[string]bool, m.list.NumInstances())
	for _, inst := range m.list.GetInstances() {
		known[inst.Path] = true
	}

	adopted, err := m.storage.UntrackedInstances(known)
	if err != nil {
		log.WarningLog.Printf("adopt untracked instances: %v", err)
		return
	}
	for _, inst := range adopted {
		m.list.AddInstance(inst)()
	}
}

// sessionMaxTurns bounds the Session tab's own LaneTail read - large enough
// to fill the pane at every size this app targets (120x36 through 200x55,
// design/cockpit-pane/PANE-MOCKUP-*.md), well past the list rows' bare
// default (they only ever look at State/LastTurn/PendingAgents, never
// Turns).
const sessionMaxTurns = 40

// updateSessionTabInfo resolves the SELECTED row's own LaneTail (tracked or
// external, whichever the list's cursor currently sits on) and hands it to
// the Session pane, or clears it when nothing is selected - the pane then
// falls back to the splash's resting frame. Called once per sessionTickMsg
// (design/cockpit-pane/DECISIONS.md's Latency ruling, slice 12) - the
// selected lane's own 500ms cadence, never the 3s feedTickMsg every row
// also runs on and never the 100ms preview cadence.
func (m *home) updateSessionTabInfo(now time.Time) {
	if m.tabbedWindow == nil {
		// A *home built directly in a test (skipping newHome, see
		// laneTailCache's own doc comment) has no tabbed window to update.
		return
	}
	info := m.selectedSessionInfo(now)
	m.tabbedWindow.SetSessionInfo(info)
}

// updateSessionFleetCounts refreshes the splash resting frame's own "lanes
// live"/"needs you" counters - unrelated to the selected lane's own data,
// so it stays on feedTickMsg's 3s cadence rather than moving to the
// selected-lane-only sessionTickMsg alongside updateSessionTabInfo above.
func (m *home) updateSessionFleetCounts() {
	if m.tabbedWindow == nil {
		return
	}
	live, needsYou := splash.FleetCounts()
	m.tabbedWindow.SetSessionFleetCounts(live, needsYou)
	m.tabbedWindow.SetTerminalFleetCounts(live, needsYou)
}

// selectedSessionInfo builds the ui.SessionInfo for whichever row is
// selected, or nil when nothing is (list empty, or the cursor is on neither
// list's rows).
func (m *home) selectedSessionInfo(now time.Time) *ui.SessionInfo {
	if selected := m.list.GetSelectedInstance(); selected != nil {
		path, ok := clarity.NewestTranscript(selected.Path)
		if !ok {
			return nil
		}
		tail, err := m.laneTailCache.Get(path, sessionMaxTurns, now)
		if err != nil {
			return nil
		}
		branch := ""
		if selected.HasWorktree() {
			branch = selected.Branch
		}
		ctxPct, ctxOK := selected.GetContextFill()
		return &ui.SessionInfo{
			Lane:    selected.Title,
			WorkDir: selected.Path,
			Branch:  branch,
			Tail:    tail,
			CtxPct:  ctxPct,
			CtxOK:   ctxOK,
			Now:     now,
		}
	}

	if ext, ok := m.list.GetSelectedExternalLane(); ok {
		tail, err := m.laneTailCache.Get(ext.TranscriptPath, sessionMaxTurns, now)
		if err != nil {
			return nil
		}
		return &ui.SessionInfo{
			Lane:    ext.Name,
			WorkDir: ext.WorkDir,
			Tail:    tail,
			CtxPct:  ext.Fill.Pct,
			CtxOK:   ext.FillOK,
			Now:     now,
		}
	}

	return nil
}

// snapshotActiveInstances returns the currently active (started, not paused)
// instances. Called on the main thread so the filtering doesn't race with
// state mutations.
func (m *home) snapshotActiveInstances() []*session.Instance {
	var out []*session.Instance
	for _, inst := range m.list.GetInstances() {
		if inst.Started() && !inst.Paused() {
			out = append(out, inst)
		}
	}
	return out
}

// tickUpdateMetadataCmd returns a self-chaining Cmd that sleeps 500ms, then performs
// expensive metadata I/O (tmux capture, git diff) in parallel background goroutines.
// Because it only re-schedules after completing, overlapping ticks are impossible.
// The active instances slice should be snapshotted on the main thread via
// snapshotActiveInstances() before being passed here.
//
// Only the selected instance gets a full diff (with Content); the rest get a
// lightweight numstat-only summary. This keeps per-instance memory bounded
// since the diff pane only ever renders the selected one.
func tickUpdateMetadataCmd(active []*session.Instance, selected *session.Instance) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(500 * time.Millisecond)

		if len(active) == 0 {
			return metadataUpdateDoneMsg{}
		}

		results := make([]instanceMetaResult, len(active))
		var wg sync.WaitGroup
		for idx, inst := range active {
			wg.Add(1)
			go func(i int, instance *session.Instance) {
				defer wg.Done()
				r := &results[i]
				r.instance = instance
				r.updated, r.hasPrompt = instance.HasUpdated()
				if instance == selected {
					r.diffStats = instance.ComputeDiff()
				} else {
					r.diffStats = instance.ComputeDiffNumstat()
				}
				r.fillPct, r.fillOK = instance.ComputeContextFill()
			}(idx, inst)
		}
		wg.Wait()

		return metadataUpdateDoneMsg{results: results}
	}
}

// handleError handles all errors which get bubbled up to the app. sets the error message. We return a callback tea.Cmd that returns a hideErrMsg message
// which clears the error message after 3 seconds.
func (m *home) handleError(err error) tea.Cmd {
	log.ErrorLog.Printf("%v", err)
	m.errBox.SetError(err)
	m.hasErr = true
	return func() tea.Msg {
		select {
		case <-m.ctx.Done():
		case <-time.After(3 * time.Second):
		}

		return hideErrMsg{}
	}
}

// setStatus shows ephemeral, non-error status text in the footer (the
// m-key message-delivery result) for a few seconds, mirroring handleError's
// shape but through the neutral statusBox rather than the red errBox.
func (m *home) setStatus(text string) tea.Cmd {
	m.statusBox.SetText(text)
	m.statusText = text
	return func() tea.Msg {
		select {
		case <-m.ctx.Done():
		case <-time.After(4 * time.Second):
		}

		return hideStatusMsg{}
	}
}

// sendComposerCmd delivers text to lane in the background and reports the
// result as a composerResultMsg the composer's own foot then shows: for a
// tracked instance, SendPrompt followed by a pane capture to confirm the
// line landed ("sent · landed hh:mm:ss" - the capture succeeding is the
// confirmation, same as the pre-slice-5 m key's own delivery path); for an
// external lane, a clipboard copy instead ("copied · this lane runs in
// your own terminal, paste it there") - never a claimed delivery this
// cockpit cannot confirm.
func (m *home) sendComposerCmd(lane string, isExternal bool, text string) tea.Cmd {
	return func() tea.Msg {
		if isExternal {
			if err := clarity.CopyToClipboard(m.cmdExec, text); err != nil {
				return composerResultMsg{err: fmt.Errorf("could not copy to clipboard: %w", err)}
			}
			return composerResultMsg{result: "copied · this lane runs in your own terminal, paste it there"}
		}
		for _, inst := range m.list.GetInstances() {
			if inst.Title != lane {
				continue
			}
			if !inst.Started() || inst.Paused() || !inst.TmuxAlive() {
				return composerResultMsg{err: fmt.Errorf("%q is not a live tmux session", lane)}
			}
			if err := inst.SendPrompt(text); err != nil {
				return composerResultMsg{err: fmt.Errorf("failed to send message to %q: %w", lane, err)}
			}
			if _, err := inst.Preview(); err != nil {
				return composerResultMsg{err: fmt.Errorf("message sent to %q but pane capture failed: %w", lane, err)}
			}
			return composerResultMsg{result: fmt.Sprintf("sent · landed %s", time.Now().Local().Format("15:04:05"))}
		}
		return composerResultMsg{err: fmt.Errorf("no such tracked instance %q", lane)}
	}
}

func (m *home) newPromptOverlay() *overlay.TextInputOverlay {
	return overlay.NewTextInputOverlayWithBranchPicker("Enter prompt", "", m.appConfig.GetProfiles())
}

// cancelPromptOverlay cancels the prompt overlay, cleaning up unstarted instances.
func (m *home) cancelPromptOverlay() tea.Cmd {
	selected := m.list.GetSelectedInstance()
	if selected != nil && !selected.Started() {
		m.list.Kill()
	}
	m.textInputOverlay = nil
	m.state = stateDefault
	return tea.Sequence(
		tea.RequestWindowSize,
		func() tea.Msg {
			m.menu.SetState(ui.StateDefault)
			return nil
		},
	)
}

// confirmAction shows a confirmation modal and stores the action to execute on confirm
func (m *home) confirmAction(message string, action tea.Cmd) tea.Cmd {
	m.state = stateConfirm

	// Create and show the confirmation overlay using ConfirmationOverlay
	m.confirmationOverlay = overlay.NewConfirmationOverlay(message)
	// Set a fixed width for consistent appearance
	m.confirmationOverlay.SetWidth(50)

	// Set callbacks for confirmation and cancellation
	m.confirmationOverlay.OnConfirm = func() {
		m.state = stateDefault
		// Execute the action if it exists
		if action != nil {
			_ = action()
		}
	}

	m.confirmationOverlay.OnCancel = func() {
		m.state = stateDefault
	}

	return nil
}

func (m *home) View() tea.View {
	if m.splashModel != nil {
		v := tea.NewView(m.splashModel.View())
		v.AltScreen = true
		return v
	}

	listWithPadding := lipgloss.NewStyle().PaddingTop(1).Render(m.list.String())
	previewWithPadding := lipgloss.NewStyle().PaddingTop(1).Render(m.tabbedWindow.String())
	listAndPreview := lipgloss.JoinHorizontal(lipgloss.Top, listWithPadding, previewWithPadding)

	// The error box and status box share one footer row: an error always
	// wins if somehow both are set (it should never lose visibility behind
	// a status message), otherwise show the status if there is one.
	footer := m.errBox.String()
	if !m.hasErr && m.statusText != "" {
		footer = m.statusBox.String()
	}

	// lipgloss.Left, not Center: listAndPreview (TabbedWindow renders a few
	// columns narrower than the width it is given, see tabbed_window.go)
	// comes out narrower than menu.String()'s own full-width row, and
	// JoinVertical pads every block up to the widest one using this
	// alignment - Center split that shortfall either side of listAndPreview,
	// pushing the list's own one-space-margined " Instances " title right
	// by half of it (the MARGIN defect: column 5-6 at 164 wide instead of
	// column 1). Left leaves the shortfall entirely on the right, where
	// nothing is drawn anyway.
	mainView := lipgloss.JoinVertical(
		lipgloss.Left,
		listAndPreview,
		m.menu.String(),
		footer,
	)

	content := mainView
	if m.state == statePrompt {
		if m.textInputOverlay == nil {
			log.ErrorLog.Printf("text input overlay is nil")
		}
		content = overlay.PlaceOverlay(0, 0, m.textInputOverlay.Render(), mainView, true, true)
	} else if m.state == stateHelp {
		if m.textOverlay == nil {
			log.ErrorLog.Printf("text overlay is nil")
		}
		content = overlay.PlaceOverlay(0, 0, m.textOverlay.Render(), mainView, true, true)
	} else if m.state == stateConfirm {
		if m.confirmationOverlay == nil {
			log.ErrorLog.Printf("confirmation overlay is nil")
		}
		content = overlay.PlaceOverlay(0, 0, m.confirmationOverlay.Render(), mainView, true, true)
	}

	v := tea.NewView(content)
	// v1's tea.WithAltScreen() / tea.WithMouseCellMotion() program options -
	// v2 declares them per-View instead (see Run above).
	v.AltScreen = true
	// Mouse capture is OFF (slice 22, PART A - the owner's own complaint:
	// "cant copy paste from the session"). Finding: the mouse wheel scroll
	// case (tea.MouseWheelMsg, ~line 623) was the ONLY consumer of mouse
	// events anywhere in this app - cell-motion capture bought that one
	// feature at the cost of the terminal's own native drag-select and copy
	// EVERYWHERE, which is the very thing the owner reached for and could
	// not get. Ruling: drag-select matters more than the wheel, so the
	// mouse is released; shift+↑/shift+↓ (keys.KeyShiftUp/KeyShiftDown,
	// already wired to the same ScrollUp/ScrollDown the wheel case called)
	// remain the documented way to scroll every pane (app/help.go). The
	// wheel case itself is left in place, dormant - same convention as
	// KeyCheckout (keys/keys.go) and PreviewPane/DiffPane (ui/
	// tabbed_window.go's own comment) - rather than deleted, in case mouse
	// capture is ever worth its cost again. MouseModeNone is bubbletea's
	// own zero value; set explicitly here rather than left implicit, so a
	// future reader sees the choice, not an omission.
	v.MouseMode = tea.MouseModeNone
	return v
}
