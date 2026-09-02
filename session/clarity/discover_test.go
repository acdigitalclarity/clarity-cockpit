package clarity

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// mkTranscriptDir creates a fake ~/.claude/projects/<encoded>/*.jsonl fixture
// under root, with a single transcript file at age `age` (negative = future,
// unused here) and the given content (a valid usage-bearing line by
// default, so ReadFill succeeds and the fixture exercises the same path a
// real transcript does).
func mkTranscriptDir(t *testing.T, root, encodedDir, filename string, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(root, encodedDir)
	require.NoError(t, os.MkdirAll(dir, 0755))
	path := filepath.Join(dir, filename)
	f, err := os.Create(path)
	require.NoError(t, err)
	writeTranscriptLine(t, f, fableUsageLine("claude-sonnet-5", 10_000, 0, 0))
	require.NoError(t, f.Close())
	mt := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(path, mt, mt))
	return path
}

func TestDiscoverExternalLanes_LiveWithinWindow(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)

	mkTranscriptDir(t, root, "-Users-allencoates-projects-Clarity-sessions-fixture-live", "a.jsonl", 5*time.Minute)

	lanes, err := DiscoverExternalLanes(nil)
	require.NoError(t, err)
	require.Len(t, lanes, 1)
	require.Equal(t, "sessions-fixture-live", lanes[0].Name)
	require.True(t, lanes[0].FillOK)
}

func TestDiscoverExternalLanes_ExcludesOlderThan90Minutes(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)

	mkTranscriptDir(t, root, "-Users-allencoates-projects-Clarity-sessions-fixture-stale", "a.jsonl", 91*time.Minute)

	lanes, err := DiscoverExternalLanes(nil)
	require.NoError(t, err)
	require.Empty(t, lanes, "a transcript older than the 90-minute cutoff must not appear")
}

func TestDiscoverExternalLanes_JustInsideWindowIsLive(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)

	mkTranscriptDir(t, root, "-Users-allencoates-projects-Clarity-sessions-fixture-edge", "a.jsonl", 89*time.Minute)

	lanes, err := DiscoverExternalLanes(nil)
	require.NoError(t, err)
	require.Len(t, lanes, 1)
}

func TestDiscoverExternalLanes_ExcludesMemoryPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)

	mkTranscriptDir(t, root, "-Users-allencoates-projects-Clarity-sessions-fixture-mem", "memory-scratch.jsonl", time.Minute)

	lanes, err := DiscoverExternalLanes(nil)
	require.NoError(t, err)
	require.Empty(t, lanes, "a transcript path containing memory must be excluded, same as fleet_dashboard.py")
}

func TestDiscoverExternalLanes_ExcludesSubagentsLane(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)

	mkTranscriptDir(t, root, "subagents-worker-1", "a.jsonl", time.Minute)

	lanes, err := DiscoverExternalLanes(nil)
	require.NoError(t, err)
	require.Empty(t, lanes, "a lane whose derived name starts with subagents must be excluded")
}

func TestDiscoverExternalLanes_ExcludesTrackedTitles(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)

	mkTranscriptDir(t, root, "-Users-allencoates-projects-Clarity-sessions-tracked-lane", "a.jsonl", time.Minute)

	lanes, err := DiscoverExternalLanes(map[string]bool{"sessions-tracked-lane": true})
	require.NoError(t, err)
	require.Empty(t, lanes, "a lane Claude Squad already tracks must never also show as an external row")
}

func TestDiscoverExternalLanes_DedupesToNewestTranscriptPerLane(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)

	encoded := "-Users-allencoates-projects-Clarity-sessions-fixture-dup"
	older := mkTranscriptDir(t, root, encoded, "older.jsonl", 20*time.Minute)
	newer := mkTranscriptDir(t, root, encoded, "newer.jsonl", 5*time.Minute)

	lanes, err := DiscoverExternalLanes(nil)
	require.NoError(t, err)
	require.Len(t, lanes, 1)
	require.Equal(t, newer, lanes[0].TranscriptPath)
	require.NotEqual(t, older, lanes[0].TranscriptPath)
}

func TestDiscoverExternalLanes_SortedNewestFirst(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)

	mkTranscriptDir(t, root, "-Users-allencoates-projects-Clarity-sessions-fixture-older-lane", "a.jsonl", 30*time.Minute)
	mkTranscriptDir(t, root, "-Users-allencoates-projects-Clarity-sessions-fixture-newer-lane", "a.jsonl", 2*time.Minute)

	lanes, err := DiscoverExternalLanes(nil)
	require.NoError(t, err)
	require.Len(t, lanes, 2)
	require.Equal(t, "sessions-fixture-newer-lane", lanes[0].Name)
	require.Equal(t, "sessions-fixture-older-lane", lanes[1].Name)
}

func TestDiscoverExternalLanes_NoMatches(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)

	lanes, err := DiscoverExternalLanes(nil)
	require.NoError(t, err)
	require.Empty(t, lanes)
}

func TestTrackedExclusionNames_CoversBothForms(t *testing.T) {
	names := TrackedExclusionNames("ways-of-working")
	require.Contains(t, names, "ways-of-working")
	require.Contains(t, names, "sessions-ways-of-working")
}

func TestDiscoverExternalLanes_ExcludesTrackedTitleViaSessionsPrefix(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)

	// A real clarity-attach instance titled "ways-of-working" has its
	// transcripts under sessions/ways-of-working, which discover derives as
	// "sessions-ways-of-working" - not the bare title. TrackedExclusionNames
	// is what makes the exclusion actually reach this row.
	mkTranscriptDir(t, root, "-Users-allencoates-projects-Clarity-sessions-ways-of-working", "a.jsonl", time.Minute)

	exclude := make(map[string]bool)
	for _, n := range TrackedExclusionNames("ways-of-working") {
		exclude[n] = true
	}

	lanes, err := DiscoverExternalLanes(exclude)
	require.NoError(t, err)
	require.Empty(t, lanes, "a tracked session lane must not double-list as an external row")
}

func TestMatchesQueriedLane_BareAndSessionsPrefixed(t *testing.T) {
	ext := ExternalLane{Name: "sessions-ways-of-working"}
	require.True(t, MatchesQueriedLane(ext, "ways-of-working"))
	require.True(t, MatchesQueriedLane(ext, "sessions-ways-of-working"))
	require.False(t, MatchesQueriedLane(ext, "some-other-lane"))
}

func TestLaneNameFromTranscriptDir_StripsKnownPrefixes(t *testing.T) {
	require.Equal(t, "sessions-ways-of-working",
		laneNameFromTranscriptDir("/x/-Users-allencoates-projects-Clarity-sessions-ways-of-working"))
	require.Equal(t, "repos-clarity-squad",
		laneNameFromTranscriptDir("/x/-Users-allencoates-repos-clarity-squad"))
}
