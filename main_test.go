package main

import (
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
