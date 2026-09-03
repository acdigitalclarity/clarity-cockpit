package app

import (
	"claude-squad/cmd"
	"claude-squad/config"
	"claude-squad/session"
	"claude-squad/session/clarity"
	"claude-squad/ui"
	"claude-squad/ui/overlay"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// newLaneTestHome builds a minimal *home wired the same way newHome does
// for the pieces this file's tests touch: the list, menu, error/status
// boxes and the real (unmocked) cmdExec - the wrapper is never actually
// shelled out to by any test in this file, ctrl-c cancels before that step
// runs, so there is nothing for cmdExec to do.
func newLaneTestHome(t *testing.T) *home {
	t.Helper()
	sp := spinner.New()
	return &home{
		ctx:       context.Background(),
		spinner:   sp,
		list:      ui.NewList(&sp, false),
		menu:      ui.NewMenu(),
		errBox:    ui.NewErrBox(),
		statusBox: ui.NewStatusBox(),
		cmdExec:   cmd.MakeExecutor(),
		program:   "claude",
		state:     stateDefault,
	}
}

// scratchNewLaneEnv points every root the overlay reads at empty, isolated
// scratch directories - no registry, no live lanes, no forge apps - so
// building the overlay never touches this machine's real accounts,
// sessions or transcripts (t.Setenv restores every var on test cleanup).
func scratchNewLaneEnv(t *testing.T) (sessionsRoot string) {
	t.Helper()
	sessionsRoot = t.TempDir()
	t.Setenv(clarity.SessionsRootEnvVar, sessionsRoot)
	t.Setenv(clarity.AccountsRegistryEnvVar, filepath.Join(t.TempDir(), "missing-registry.json"))
	t.Setenv(clarity.ForgeAppsRootEnvVar, t.TempDir())
	t.Setenv(clarity.ClaudeProjectsRootEnvVar, t.TempDir())
	return sessionsRoot
}

func pressKey(h *home, msg tea.KeyPressMsg) {
	h.handleKeyPress(msg)
}

// pressNamedKey drives a key bound in keys.GlobalKeyStringsMap through the
// same two-pass menu-highlight dance pressGlobalKey (composer_test.go) uses.
// Needed only for "n" BEFORE the overlay exists (state is still
// stateDefault, not exempted) - every key pressed once the overlay is open
// (enter/esc/up/down/typing) is single-dispatch, handleMenuHighlighting's
// own exemption for state==stateNew && newLaneOverlay!=nil.
func pressNamedKey(h *home, msg tea.KeyPressMsg) {
	h.handleKeyPress(msg)
	h.handleKeyPress(msg)
}

func ctrlC() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl} }

// A name containing letters that are ALSO global shortcuts (q=quit, n=new,
// r=resume, b=bank) must land in order and in full - found live during this
// slice's own capture (item 4): before handleMenuHighlighting's own
// exemption, each such letter was intercepted by the menu-highlight dance
// instead of reaching the overlay at all, dropping it from the typed name.
func TestNewLaneOverlay_TypingNeverHitsGlobalShortcuts(t *testing.T) {
	scratchNewLaneEnv(t)
	h := newLaneTestHome(t)

	pressNamedKey(h, tea.KeyPressMsg{Code: 'n', Text: "n"})
	require.NotNil(t, h.newLaneOverlay)

	name := "q3-tender-bid"
	for _, r := range name {
		pressKey(h, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	require.Equal(t, name, h.newLaneOverlay.Name(), "every character, including global-shortcut letters, must land in order")
}

// (c) ctrl-c at step 3 leaves no folder and no store row.
func TestNewLaneOverlay_CtrlCAtStep3_CreatesNothing(t *testing.T) {
	sessionsRoot := scratchNewLaneEnv(t)
	h := newLaneTestHome(t)

	pressNamedKey(h, tea.KeyPressMsg{Code: 'n', Text: "n"})
	require.Equal(t, stateNew, h.state)
	require.NotNil(t, h.newLaneOverlay)

	for _, r := range "acme-bid" {
		pressKey(h, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	// Enter/esc/up/down are single-dispatch while the overlay owns the
	// keyboard (handleMenuHighlighting's own exemption) - never
	// pressNamedKey's double-dispatch, which would double-advance the step.
	pressKey(h, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, overlayStepAccount(t, h), 1)

	pressKey(h, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, overlayStepAccount(t, h), 2)

	pressKey(h, ctrlC())

	require.Nil(t, h.newLaneOverlay, "ctrl-c must clear the overlay")
	require.Equal(t, stateDefault, h.state, "ctrl-c must return to the default state")
	require.Zero(t, h.list.NumInstances(), "ctrl-c must register nothing")

	entries, err := os.ReadDir(sessionsRoot)
	require.NoError(t, err)
	require.Empty(t, entries, "ctrl-c at step 3 must leave no folder under the scratch sessions root")
}

// overlayStepAccount is a small test-only accessor so this file does not
// need to import the overlay package's own NewLaneStep type just to assert
// on it.
func overlayStepAccount(t *testing.T, h *home) int {
	t.Helper()
	require.NotNil(t, h.newLaneOverlay)
	return int(h.newLaneOverlay.Step())
}

// (f) the program string carries clarity.EnvUnsetPrefix and CLAUDE_CONFIG_DIR
// for a non-default seat - the pane's tmux server environment carries the
// owner's shell ANTHROPIC_API_KEY, which outranks the seat's own claude.ai
// login unless cleared first (observed on the max-2 seat) - and nothing for
// main.
func TestNewLaneProgram_CarriesConfigDirOnlyForNonDefaultSeat(t *testing.T) {
	require.Equal(t,
		clarity.EnvUnsetPrefix+" CLAUDE_CONFIG_DIR=/Users/allencoates/.claude-team-b claude",
		newLaneProgram("claude", "/Users/allencoates/.claude-team-b"))

	require.Equal(t, "claude", newLaneProgram("claude", "/Users/allencoates/.claude"),
		"the machine's own default config dir must never be prefixed")

	require.Equal(t, "claude", newLaneProgram("claude", ""),
		"an unset config dir (no declared seat) must never be prefixed")
}

// (e) the started lane's CLAUDE.md carries Account:/Modality: lines and the
// instance record carries both. Runs the REAL `clarity` wrapper (never a
// fake) on a scratch CLARITY_ROOT/registry, exactly per the brief's own
// "the overlay must work on the real registry and the real wrapper" - only
// the ROOTS are scratch, never the binary. inst.Start is never called, so
// no tmux session is created by this test.
func TestClarityWrapperNew_WritesAccountAndModality_InstanceCarriesBoth(t *testing.T) {
	clarityRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(clarityRoot, "CLAUDE.md"), []byte("# root\n"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(clarityRoot, ".claude", "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(clarityRoot, "repos"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(clarityRoot, "work"), 0755))

	teamBConfigDir := filepath.Join(clarityRoot, ".claude-team-b")
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	registryJSON := `{"accounts":{"team-b":{"config_dir":"` + teamBConfigDir + `"}},"policy":{"default_account":"main"}}`
	require.NoError(t, os.WriteFile(registryPath, []byte(registryJSON), 0644))

	t.Setenv("CLARITY_ROOT", clarityRoot)
	t.Setenv(clarity.AccountsRegistryEnvVar, registryPath)
	t.Setenv(clarity.SessionsRootEnvVar, filepath.Join(clarityRoot, "sessions"))

	require.NoError(t, clarityWrapperNew(cmd.MakeExecutor(), "p2p-supply-chain", "team-b", "project"))

	lanePath, err := clarity.ResolveExistingLaneDir("p2p-supply-chain")
	require.NoError(t, err)

	claudeMd, err := os.ReadFile(filepath.Join(lanePath, ".claude", "CLAUDE.md"))
	require.NoError(t, err)
	require.Contains(t, string(claudeMd), "Account: team-b")
	require.Contains(t, string(claudeMd), "Modality: project")

	inst, err := session.NewInstance(session.InstanceOptions{
		Title:      "p2p-supply-chain",
		Path:       lanePath,
		Program:    newLaneProgram("claude", teamBConfigDir),
		NoWorktree: true,
		Account:    "team-b",
		Modality:   "project",
	})
	require.NoError(t, err)
	require.Equal(t, "team-b", inst.Account(), "the instance record must carry the account")
	require.Equal(t, "project", inst.Modality(), "the instance record must carry the modality")
	require.Equal(t, clarity.EnvUnsetPrefix+" CLAUDE_CONFIG_DIR="+teamBConfigDir+" claude", inst.Program)
}

// fixtureInstance builds a tracked instance carrying a title and modality
// only - board #315's own fixture, this file's counterpart to
// ui/list_frontdoor5_test.go's frontdoor5Instance (that helper lives in
// package ui, unreachable from here).
func fixtureInstance(t *testing.T, title, modality string) *session.Instance {
	t.Helper()
	inst, err := session.NewInstance(session.InstanceOptions{
		Title:      title,
		Path:       t.TempDir(),
		Program:    "echo",
		NoWorktree: true,
		Modality:   modality,
	})
	require.NoError(t, err)
	return inst
}

// highlightedRowTitleApp is app package's own copy of ui/list_test.go's
// highlightedRowTitle - parses the render's own "▌" marker line rather than
// reading l.selectedIdx, so this is independent proof of which row the
// screen visibly highlights.
func highlightedRowTitleApp(t *testing.T, render string, titles []string) string {
	t.Helper()
	for _, line := range strings.Split(ansi.Strip(render), "\n") {
		if !strings.HasPrefix(line, "▌") {
			continue
		}
		for _, title := range titles {
			if strings.Contains(line, title) {
				return title
			}
		}
	}
	return ""
}

// TestNewLaneFinish_SelectionLandsOnDrawnPosition is board #315's own
// message-level proof: this starts from EXACTLY the state startNewLane
// itself already sets synchronously on the overlay's finishing enter
// (app.go's startNewLane, stateDefault + newLaneOverlay=nil, before the
// wrapper/tmux work even begins) and drives the async newLaneStartedMsg
// that completes the flow - the overlay's own three-step UI is covered by
// this file's other tests already. Four ungrouped existing lanes (store
// index 0-3) plus two grouped ones (4-5, "enhancement"/"project") mirror
// the owner's own fleet at the moment of his first wizard run; the new lane
// joins the SAME "enhancement" group as build-night, appended at store
// index 6 - exactly where AddInstance+SetSelectedInstance(NumInstances()-1)
// (app.go's newLaneStartedMsg handler) leaves it.
func TestNewLaneFinish_SelectionLandsOnDrawnPosition(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	storage, err := session.NewStorage(config.LoadState())
	require.NoError(t, err)

	h := newComposerTestHome(t)
	h.storage = storage
	h.state = stateDefault
	h.newLaneOverlay = nil

	h.list.AddInstance(fixtureInstance(t, "existing-1", ""))
	h.list.AddInstance(fixtureInstance(t, "existing-2", ""))
	h.list.AddInstance(fixtureInstance(t, "existing-3", ""))
	h.list.AddInstance(fixtureInstance(t, "existing-4", ""))
	h.list.AddInstance(fixtureInstance(t, "build-night", "enhancement"))
	h.list.AddInstance(fixtureInstance(t, "p2p-supply-chain", "project"))
	h.list.SetSize(46, 40)

	newInst := fixtureInstance(t, "repro315", "enhancement")

	model, _ := h.Update(newLaneStartedMsg{instance: newInst})
	h2, ok := model.(*home)
	require.True(t, ok)

	require.Equal(t, stateDefault, h2.state, "the finishing enter must leave stateDefault")
	require.Nil(t, h2.newLaneOverlay, "the finishing enter must clear the overlay pointer")
	require.Same(t, newInst, h2.list.GetSelectedInstance(), "the selection must be the new instance")

	titles := []string{"existing-1", "existing-2", "existing-3", "existing-4", "build-night", "p2p-supply-chain", "repro315"}
	stripped := ansi.Strip(h2.list.String())
	require.Equal(t, "repro315", highlightedRowTitleApp(t, stripped, titles),
		"the highlight band must be on the new instance's own row")

	// The new row sits INSIDE its modality group's render block: after the
	// " Enhancement " heading and before the next heading (" Project ") -
	// never above every heading (board #315's own "rendered at the TOP of
	// the list, outside the grouped order").
	headingIdx := strings.Index(stripped, " Enhancement ")
	nextHeadingIdx := strings.Index(stripped, " Project ")
	rowIdx := strings.Index(stripped, "repro315")
	require.Greater(t, headingIdx, -1)
	require.Greater(t, nextHeadingIdx, -1)
	require.True(t, rowIdx > headingIdx && rowIdx < nextHeadingIdx,
		"the new lane's row must render between its own heading and the next one")

	// One Down must move to the NEXT drawn row, staying on a tracked row -
	// board #315's own "arrow keys were dead" / "Session tab showed the
	// splash": the old code read the new instance's raw store index (the
	// list's own last index) as the tracked group's own END, so a Down from
	// there fell straight out into the (empty) external group, leaving
	// GetSelectedInstance nil - exactly what the Session tab reads as its
	// splash resting frame (app.go's selectedSessionInfo).
	h2.list.Down()
	require.Equal(t, ui.RowKindTracked, h2.list.SelectedRowKind(),
		"one Down after the finishing enter must stay on a tracked row, never fall into external/needs-you")
	sel := h2.list.GetSelectedInstance()
	require.NotNil(t, sel, "GetSelectedInstance must never go nil here - a nil selection is what the Session tab reads as the splash")
	// The next DRAWN row after repro315 (drawnItemOrder: build-night,
	// repro315, p2p-supply-chain, existing-1..4) is p2p-supply-chain - a
	// raw-store-order Down instead would wrap off the end (repro315 sits at
	// the highest store index) straight back to store index 0
	// ("existing-1"), which this pins against directly rather than settling
	// for "any other row".
	require.Equal(t, "p2p-supply-chain", sel.Title,
		"one Down must land on the NEXT DRAWN row, not wrap to raw store index 0")
	stripped2 := ansi.Strip(h2.list.String())
	require.Equal(t, "p2p-supply-chain", highlightedRowTitleApp(t, stripped2, titles),
		"the highlight band must have moved to the same row the selection did")
}

// (4b) loginProgram's own exact string - front-door slice 7 item 2, quoted
// verbatim from the brief: "CLAUDE_CONFIG_DIR=<config_dir> claude /login".
func TestLoginProgram_ExactString(t *testing.T) {
	require.Equal(t, "CLAUDE_CONFIG_DIR=/Users/allencoates/.claude-team-b claude /login",
		loginProgram("claude", "/Users/allencoates/.claude-team-b"))
}

// loginOverlayAtStep2 walks a fresh overlay to step 2 with the given rows,
// the same way the real "n" flow does (NextFromName), so handleLoginKey's
// own tests drive the actual step-2 handler rather than reaching into the
// overlay's internals.
func loginOverlayAtStep2(t *testing.T, rows []overlay.NewLaneAccountRow, name string) *overlay.NewLaneOverlay {
	t.Helper()
	o := overlay.NewNewLaneOverlay(t.TempDir(), "", rows, "")
	require.NoError(t, o.TypeRune(name))
	o.NextFromName()
	require.Equal(t, overlay.NewLaneStepAccount, o.Step())
	return o
}

// (4c) l on a seat that already has a credential store does nothing but
// name why on the foot - the overlay stays open, nothing is added to the
// list.
func TestHandleLoginKey_AlreadyLoggedIn_NoOpBesidesFoot(t *testing.T) {
	scratchNewLaneEnv(t)
	h := newLaneTestHome(t)

	seatDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(seatDir, ".credentials.json"), []byte("{}"), 0600))
	rows := []overlay.NewLaneAccountRow{{Tag: "signed-in-seat", ConfigDir: seatDir, CredentialStore: true}}
	h.newLaneOverlay = loginOverlayAtStep2(t, rows, "acme-project")
	h.state = stateNew

	model, gotCmd := h.handleNewLaneKey(tea.KeyPressMsg{Code: 'l', Text: "l"})
	h2, ok := model.(*home)
	require.True(t, ok)

	require.NotNil(t, h2.newLaneOverlay, "the overlay must stay open - l on a signed-in seat is a no-op besides the foot")
	require.Equal(t, stateNew, h2.state)
	require.Zero(t, h2.list.NumInstances(), "nothing must be created for a seat that already has a store")
	require.NotNil(t, gotCmd, "setStatus must still return its own hide-after-a-few-seconds cmd")
	require.Equal(t, "already logged in", h2.statusText)
}

// (4b) l on a no-store seat closes the overlay synchronously (the
// background half - the instance and its exact program string - is proven
// separately below, never by running the returned cmd here, which would
// reach real tmux via inst.Start(true); TestClarityWrapperNew_... above
// sets the same precedent for the normal flow).
func TestHandleLoginKey_NoStore_ClosesOverlaySynchronously(t *testing.T) {
	scratchNewLaneEnv(t)
	h := newLaneTestHome(t)

	rows := []overlay.NewLaneAccountRow{{Tag: "fresh-seat", ConfigDir: t.TempDir(), CredentialStore: false}}
	h.newLaneOverlay = loginOverlayAtStep2(t, rows, "acme-project")
	h.state = stateNew

	model, gotCmd := h.handleNewLaneKey(tea.KeyPressMsg{Code: 'l', Text: "l"})
	h2, ok := model.(*home)
	require.True(t, ok)

	require.Nil(t, h2.newLaneOverlay, "l on a no-store seat must close the overlay")
	require.Equal(t, stateDefault, h2.state)
	require.NotNil(t, gotCmd, "l on a no-store seat must return the background start cmd")
}

// (4b continued) the wrapper + NewInstance half of the same "l" flow, run
// against the REAL clarity wrapper on a scratch CLARITY_ROOT/registry -
// TestClarityWrapperNew_WritesAccountAndModality_InstanceCarriesBoth's own
// shape (above, this file), for the login program instead of the normal
// one. inst.Start is never called, so no tmux session is created by this
// test.
func TestHandleLoginKey_NoStore_InstanceCarriesExactLoginProgram(t *testing.T) {
	clarityRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(clarityRoot, "CLAUDE.md"), []byte("# root\n"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(clarityRoot, ".claude", "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(clarityRoot, "repos"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(clarityRoot, "work"), 0755))

	seatConfigDir := filepath.Join(clarityRoot, ".claude-fresh-seat")
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	registryJSON := `{"accounts":{"fresh-seat":{"config_dir":"` + seatConfigDir + `"}},"policy":{"default_account":"main"}}`
	require.NoError(t, os.WriteFile(registryPath, []byte(registryJSON), 0644))

	t.Setenv("CLARITY_ROOT", clarityRoot)
	t.Setenv(clarity.AccountsRegistryEnvVar, registryPath)
	t.Setenv(clarity.SessionsRootEnvVar, filepath.Join(clarityRoot, "sessions"))

	require.NoError(t, clarityWrapperNew(cmd.MakeExecutor(), "q3-login-lane", "fresh-seat", "project"))

	lanePath, err := clarity.ResolveExistingLaneDir("q3-login-lane")
	require.NoError(t, err)

	inst, err := session.NewInstance(session.InstanceOptions{
		Title:      "q3-login-lane",
		Path:       lanePath,
		Program:    loginProgram("claude", seatConfigDir),
		NoWorktree: true,
		Account:    "fresh-seat",
		Modality:   "project",
	})
	require.NoError(t, err)
	require.Equal(t, "CLAUDE_CONFIG_DIR="+seatConfigDir+" claude /login", inst.Program,
		"the instance's own program string must match the spec's own quoted format exactly")
}

// (4b message-level) newLaneLoginStartedMsg's own handler: registers the
// instance, selects it, records it as a pending login and sets the foot
// text - the async counterpart to TestNewLaneFinish_SelectionLandsOnDrawnPosition
// above, never running inst.Start (fixtureInstance never calls it either).
func TestNewLaneLoginStartedMsg_RegistersPendingLoginAndSetsFoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	storage, err := session.NewStorage(config.LoadState())
	require.NoError(t, err)

	h := newComposerTestHome(t)
	h.storage = storage
	h.state = stateDefault
	h.newLaneOverlay = nil

	inst := fixtureInstance(t, "q3-login-lane", "project")

	model, _ := h.Update(newLaneLoginStartedMsg{instance: inst, normalProgram: "claude"})
	h2, ok := model.(*home)
	require.True(t, ok)

	require.Same(t, inst, h2.list.GetSelectedInstance(), "the new login instance must be selected")
	require.Equal(t, "log in, then enter to start", h2.statusText)
	normalProgram, pending := h2.pendingLogins[inst]
	require.True(t, pending, "the instance must be tracked as a pending login")
	require.Equal(t, "claude", normalProgram)
}

// (4d) after the login pane is marked done (completePendingLogin, called
// from the instanceAttachFinishedMsg handler once the owner detaches), the
// instance's own Program flips to the normal launch string and the pending
// entry is forgotten - proven both directly and through the message that
// wires it in production.
func TestCompletePendingLogin_FlipsProgramAndForgetsEntry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	storage, err := session.NewStorage(config.LoadState())
	require.NoError(t, err)

	h := newComposerTestHome(t)
	h.storage = storage
	h.pendingLogins = map[*session.Instance]string{}

	inst := fixtureInstance(t, "q3-login-lane", "project")
	inst.Program = "CLAUDE_CONFIG_DIR=/Users/allencoates/.claude-fresh-seat claude /login"
	h.list.AddInstance(inst)
	h.list.SetSelectedInstance(0)
	h.pendingLogins[inst] = "CLAUDE_CONFIG_DIR=/Users/allencoates/.claude-fresh-seat claude"

	model, _ := h.Update(instanceAttachFinishedMsg{})
	h2, ok := model.(*home)
	require.True(t, ok)

	require.Equal(t, "CLAUDE_CONFIG_DIR=/Users/allencoates/.claude-fresh-seat claude", inst.Program,
		"the next start must use the normal launch string, never the login one")
	_, stillPending := h2.pendingLogins[inst]
	require.False(t, stillPending, "a completed login must be forgotten, not re-flipped on a later attach")
}

// completePendingLogin must be a true no-op for an ordinary attach - an
// instance never in pendingLogins keeps whatever Program it already had.
func TestCompletePendingLogin_NoOpForOrdinaryAttach(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	storage, err := session.NewStorage(config.LoadState())
	require.NoError(t, err)

	h := newComposerTestHome(t)
	h.storage = storage
	h.pendingLogins = map[*session.Instance]string{}

	inst := fixtureInstance(t, "ordinary-lane", "project")
	inst.Program = "claude"
	h.list.AddInstance(inst)
	h.list.SetSelectedInstance(0)

	h.completePendingLogin()

	require.Equal(t, "claude", inst.Program)
}
