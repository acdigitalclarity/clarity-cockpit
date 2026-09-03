package app

import (
	"claude-squad/cmd"
	"claude-squad/session"
	"claude-squad/session/clarity"
	"claude-squad/ui"
	"context"
	"os"
	"path/filepath"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
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

// (f) the program string carries CLAUDE_CONFIG_DIR for a non-default seat
// and nothing for main.
func TestNewLaneProgram_CarriesConfigDirOnlyForNonDefaultSeat(t *testing.T) {
	require.Equal(t,
		"CLAUDE_CONFIG_DIR=/Users/allencoates/.claude-team-b claude",
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
	require.Equal(t, "CLAUDE_CONFIG_DIR="+teamBConfigDir+" claude", inst.Program)
}
