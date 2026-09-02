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
	require.Equal(t, "fixture-live", lanes[0].Name, "the displayed Name drops the sessions- prefix (defect 3)")
	require.Equal(t, "sessions-fixture-live", lanes[0].Key, "Key keeps the full form for matching")
	require.True(t, lanes[0].FillOK)
}

// TestDiscoverExternalLanes_DropsSessionsPrefixFromDisplayName is defect
// 3's own reproduction case: a lane whose transcript directory encodes to
// "sessions-foo-bar" renders as "foo-bar" - the prefix wastes nine columns
// on every external row and forces truncation that would otherwise not be
// needed.
func TestDiscoverExternalLanes_DropsSessionsPrefixFromDisplayName(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)

	mkTranscriptDir(t, root, "-Users-allencoates-projects-Clarity-sessions-foo-bar", "a.jsonl", time.Minute)

	lanes, err := DiscoverExternalLanes(nil)
	require.NoError(t, err)
	require.Len(t, lanes, 1)
	require.Equal(t, "foo-bar", lanes[0].Name)
	require.Equal(t, "sessions-foo-bar", lanes[0].Key)
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

// mkTranscriptDirWithCwd is mkTranscriptDir plus a leading record carrying
// a "cwd" field - the ground truth a real Claude Code transcript records on
// its user/assistant/system/attachment lines (confirmed against a live
// transcript before this file was written: every one of those record types
// carries the session's actual working directory, appearing within the
// first ~50 lines in practice). DiscoverExternalLanes reads this field to
// decide whether a lane is already tracked, rather than re-deriving a name
// from the transcript directory's own (unreliable) encoding.
func mkTranscriptDirWithCwd(t *testing.T, root, encodedDir, filename string, age time.Duration, cwd string) string {
	t.Helper()
	dir := filepath.Join(root, encodedDir)
	require.NoError(t, os.MkdirAll(dir, 0755))
	path := filepath.Join(dir, filename)
	f, err := os.Create(path)
	require.NoError(t, err)
	writeTranscriptLine(t, f, `{"type":"user","cwd":"`+cwd+`"}`)
	writeTranscriptLine(t, f, fableUsageLine("claude-sonnet-5", 10_000, 0, 0))
	require.NoError(t, f.Close())
	mt := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(path, mt, mt))
	return path
}

func TestDiscoverExternalLanes_ExcludesTrackedPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)

	lanePath := "/Users/allencoates/projects/Clarity/sessions/tracked-lane"
	mkTranscriptDirWithCwd(t, root, "-Users-allencoates-projects-Clarity-sessions-tracked-lane", "a.jsonl", time.Minute, lanePath)

	lanes, err := DiscoverExternalLanes(TrackedExclusionPaths([]string{lanePath}))
	require.NoError(t, err)
	require.Empty(t, lanes, "a lane Claude Squad already tracks must never also show as an external row")
}

// TestDiscoverExternalLanes_ExcludesTrackedPathDespiteDotHyphenEncoding
// reproduces the real defect seen at the fit gate: tracked instance
// "2. andy.e-bid" (title carries a dot) also showed as external row
// "sessions-andy-e-bid" (its ~/.claude/projects transcript directory
// encodes the dot as a hyphen - a Claude Code encoding detail this
// package's own EncodeProjectDir does not reproduce, see gauge.go).
// Matching on the transcript's own "cwd" field - the lane's real working
// directory, unaffected by any directory-name encoding - excludes it
// regardless of that mismatch, where the old name-derived match did not.
func TestDiscoverExternalLanes_ExcludesTrackedPathDespiteDotHyphenEncoding(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)

	lanePath := "/Users/allencoates/projects/Clarity/sessions/andy.e-bid"
	encodedDir := "-Users-allencoates-projects-Clarity-sessions-andy-e-bid"
	mkTranscriptDirWithCwd(t, root, encodedDir, "a.jsonl", time.Minute, lanePath)

	lanes, err := DiscoverExternalLanes(TrackedExclusionPaths([]string{lanePath}))
	require.NoError(t, err)
	require.Empty(t, lanes, "a tracked lane must never also show as an external row, even when its transcript directory's own encoding of the lane name differs from the lane's title")
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
	require.Equal(t, "fixture-newer-lane", lanes[0].Name)
	require.Equal(t, "fixture-older-lane", lanes[1].Name)
}

func TestDiscoverExternalLanes_NoMatches(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)

	lanes, err := DiscoverExternalLanes(nil)
	require.NoError(t, err)
	require.Empty(t, lanes)
}

func TestTrackedExclusionPaths_CleansAndSkipsEmpty(t *testing.T) {
	paths := TrackedExclusionPaths([]string{"/a/b/", "", "/a/b"})
	require.Len(t, paths, 1, "a trailing-slash path and its clean form must collapse to one entry, and an empty path must be skipped")
	require.True(t, paths["/a/b"])
}

func TestDiscoverExternalLanes_ExcludesTrackedPathViaSessionsFolder(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)

	// A real clarity-attach instance titled "ways-of-working" has its
	// transcripts under sessions/ways-of-working - TrackedExclusionPaths
	// reaches this row by the lane's own working directory, read from the
	// transcript's "cwd" field, not by re-deriving a name from the
	// transcript directory's encoding.
	lanePath := "/Users/allencoates/projects/Clarity/sessions/ways-of-working"
	mkTranscriptDirWithCwd(t, root, "-Users-allencoates-projects-Clarity-sessions-ways-of-working", "a.jsonl", time.Minute, lanePath)

	lanes, err := DiscoverExternalLanes(TrackedExclusionPaths([]string{lanePath}))
	require.NoError(t, err)
	require.Empty(t, lanes, "a tracked session lane must not double-list as an external row")
}

func TestMatchesQueriedLane_BareAndSessionsPrefixed(t *testing.T) {
	// The shape DiscoverExternalLanes actually produces: Name stripped of
	// the "sessions-" prefix for display, Key keeping the full form for
	// matching (defect 3).
	ext := ExternalLane{Name: "ways-of-working", Key: "sessions-ways-of-working"}
	require.True(t, MatchesQueriedLane(ext, "ways-of-working"), "the displayed (stripped) Name must still match")
	require.True(t, MatchesQueriedLane(ext, "sessions-ways-of-working"), "the full Key must still match")
	require.False(t, MatchesQueriedLane(ext, "some-other-lane"))
}

func TestLaneNameFromTranscriptDir_StripsKnownPrefixes(t *testing.T) {
	require.Equal(t, "sessions-ways-of-working",
		laneNameFromTranscriptDir("/x/-Users-allencoates-projects-Clarity-sessions-ways-of-working"))
	require.Equal(t, "repos-clarity-squad",
		laneNameFromTranscriptDir("/x/-Users-allencoates-repos-clarity-squad"))
}
