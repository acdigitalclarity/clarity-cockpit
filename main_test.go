package main

import (
	"claude-squad/session"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolveAttachSeat_FlagWins is test (e) (BRIEF-FRONTDOOR-4.md item 5):
// clarity-attach --account team-b stores team-b, regardless of what (if
// anything) the lane folder itself declares.
func TestResolveAttachSeat_FlagWins(t *testing.T) {
	lanePath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(lanePath, ".claude"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(lanePath, ".claude", "CLAUDE.md"),
		[]byte("# Session\n\nAccount: main\nModality: enhancement\n"), 0644))

	account, modality := resolveAttachSeat("team-b", "bid", lanePath)
	require.Equal(t, "team-b", account, "the --account flag must win over the lane's own declared Account: line")
	require.Equal(t, "bid", modality, "the --modality flag must win over the lane's own declared Modality: line")
}

// TestResolveAttachSeat_FallsBackToLaneDeclaration is test (f): with no
// flag, a lane folder declaring "Account: team-a" registers as team-a - a
// declared lane gets its seat even before the wrapper (scripts/clarity)
// passes --account.
func TestResolveAttachSeat_FallsBackToLaneDeclaration(t *testing.T) {
	lanePath := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(lanePath, ".claude"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(lanePath, ".claude", "CLAUDE.md"),
		[]byte("# Session\n\nAccount: team-a\n"), 0644))

	account, modality := resolveAttachSeat("", "", lanePath)
	require.Equal(t, "team-a", account, "no flag: the lane's own declared Account: line must be read")
	require.Empty(t, modality, "no Modality: line in the fixture, and no flag - modality stays empty")
}

// TestResolveAttachSeat_NoDeclarationNoFlag_BothEmpty guards the floor: a
// plain lane with no .claude/CLAUDE.md and no flags resolves to today's
// shape - both empty, an empty Account being itself a valid seat.
func TestResolveAttachSeat_NoDeclarationNoFlag_BothEmpty(t *testing.T) {
	account, modality := resolveAttachSeat("", "", t.TempDir())
	require.Empty(t, account)
	require.Empty(t, modality)
}

// attachDecisionFixture builds a construction-only *session.Instance for
// attachResumeDecision - it only reads Title/Account()/Status, never
// Start()s or attaches anything, so no tmux/pty is involved.
func attachDecisionFixture(t *testing.T, title, account string, status session.Status) *session.Instance {
	t.Helper()
	inst, err := session.NewInstance(session.InstanceOptions{
		Title:      title,
		Path:       t.TempDir(),
		Program:    "claude",
		NoWorktree: true,
		Account:    account,
	})
	require.NoError(t, err)
	inst.Status = status
	return inst
}

// TestAttachResumeDecision_PausedMatch_ReturnsExistingToResume is board
// #317 item 4's own case: a lane whose program ended on its own lands
// Paused (item 2/1), and clarity-attach must resume it in place rather
// than refusing with "already exists".
func TestAttachResumeDecision_PausedMatch_ReturnsExistingToResume(t *testing.T) {
	paused := attachDecisionFixture(t, "scratchfix-lane", "team-b", session.Paused)
	other := attachDecisionFixture(t, "scratchfix-lane", "team-a", session.Running)

	existing, conflictErr := attachResumeDecision([]*session.Instance{other, paused}, "scratchfix-lane", "team-b")

	require.NoError(t, conflictErr)
	require.Same(t, paused, existing, "the Paused instance for this exact lane+account must be the one returned")
}

// TestAttachResumeDecision_LiveMatch_Conflicts guards the floor: a still-
// live instance for the same lane+account is a real conflict, unchanged
// from clarity-attach's original refusal.
func TestAttachResumeDecision_LiveMatch_Conflicts(t *testing.T) {
	live := attachDecisionFixture(t, "scratchfix-lane", "team-b", session.Running)

	existing, conflictErr := attachResumeDecision([]*session.Instance{live}, "scratchfix-lane", "team-b")

	require.Nil(t, existing)
	require.Error(t, conflictErr)
	require.Contains(t, conflictErr.Error(), "already exists")
}

// TestAttachResumeDecision_NoMatch_CreatesNew is the third branch: no
// instance for this lane+account at all, so the caller creates one.
func TestAttachResumeDecision_NoMatch_CreatesNew(t *testing.T) {
	other := attachDecisionFixture(t, "other-lane", "team-b", session.Paused)

	existing, conflictErr := attachResumeDecision([]*session.Instance{other}, "scratchfix-lane", "team-b")

	require.NoError(t, conflictErr)
	require.Nil(t, existing)
}
