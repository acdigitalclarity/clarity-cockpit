package clarity

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEncodeProjectDir(t *testing.T) {
	got := EncodeProjectDir("/Users/allencoates/projects/Clarity/sessions/ways-of-working")
	require.Equal(t, "-Users-allencoates-projects-Clarity-sessions-ways-of-working", got)
}

// writeTranscriptLine writes one raw JSONL line to f.
func writeTranscriptLine(t *testing.T, f *os.File, line string) {
	t.Helper()
	_, err := f.WriteString(line + "\n")
	require.NoError(t, err)
}

func TestNewestTranscript_PicksNewestAndSkipsMemory(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)

	lane := "/Users/allencoates/projects/Clarity/sessions/fixture-lane"
	dir := filepath.Join(root, EncodeProjectDir(lane))
	require.NoError(t, os.MkdirAll(dir, 0755))

	older := filepath.Join(dir, "older.jsonl")
	newer := filepath.Join(dir, "newer.jsonl")
	memoryFile := filepath.Join(dir, "memory-scratch.jsonl")
	require.NoError(t, os.WriteFile(older, []byte("{}\n"), 0644))
	require.NoError(t, os.WriteFile(memoryFile, []byte("{}\n"), 0644))
	require.NoError(t, os.WriteFile(newer, []byte("{}\n"), 0644))

	now := time.Now()
	require.NoError(t, os.Chtimes(older, now.Add(-2*time.Hour), now.Add(-2*time.Hour)))
	require.NoError(t, os.Chtimes(memoryFile, now, now)) // newest by mtime, but must be excluded
	require.NoError(t, os.Chtimes(newer, now.Add(-1*time.Hour), now.Add(-1*time.Hour)))

	path, ok := NewestTranscript(lane)
	require.True(t, ok)
	require.Equal(t, newer, path, "the memory-named file is newer but must be excluded")
}

func TestNewestTranscript_NoneResolves(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)

	_, ok := NewestTranscript("/no/such/lane")
	require.False(t, ok)
}

// fableUsageLine builds one transcript line carrying a usage block, in the
// shape context-fill.py reads: {"message": {"model": ..., "usage": {...}}}.
func fableUsageLine(model string, input, cacheRead, cacheCreate int64) string {
	return `{"message":{"model":"` + model + `","usage":{"input_tokens":` +
		itoa(input) + `,"cache_read_input_tokens":` + itoa(cacheRead) +
		`,"cache_creation_input_tokens":` + itoa(cacheCreate) + `}}}`
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}

func TestReadFill_ModelNameHeuristic_Fable1M(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	f, err := os.Create(path)
	require.NoError(t, err)
	writeTranscriptLine(t, f, fableUsageLine("claude-fable-5", 100_000, 50_000, 0))
	require.NoError(t, f.Close())

	fill, ok := ReadFill(path, "")
	require.True(t, ok)
	require.Equal(t, int64(1_000_000), fill.Window)
	require.Equal(t, int64(150_000), fill.Used)
	require.Equal(t, 15, fill.Pct)
	require.Contains(t, fill.Basis, "assumed-1M")
}

func TestReadFill_ModelNameHeuristic_Default200k(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	f, err := os.Create(path)
	require.NoError(t, err)
	writeTranscriptLine(t, f, fableUsageLine("claude-sonnet-5", 40_000, 20_000, 0))
	require.NoError(t, f.Close())

	fill, ok := ReadFill(path, "")
	require.True(t, ok)
	require.Equal(t, int64(200_000), fill.Window)
	require.Equal(t, int64(60_000), fill.Used)
	require.Equal(t, 30, fill.Pct)
	require.Contains(t, fill.Basis, "assumed-200k")
}

func TestReadFill_CompactMetadataOverridesHeuristic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	f, err := os.Create(path)
	require.NoError(t, err)
	// A bare "claude-opus-5" model with no "1m" marker would fall to the
	// 1M heuristic anyway (opus is in the blob check) - use a neutral model
	// name so this test actually isolates the compactMetadata path, not the
	// heuristic.
	writeTranscriptLine(t, f, `{"compactMetadata":{"trigger":"auto","preTokens":993026}}`)
	writeTranscriptLine(t, f, fableUsageLine("claude-sonnet-5", 100_000, 0, 0))
	require.NoError(t, f.Close())

	fill, ok := ReadFill(path, "")
	require.True(t, ok)
	require.Equal(t, int64(1_000_000), fill.Window, "a real auto-compact ceiling must win over the model-name heuristic")
	require.Contains(t, fill.Basis, "transcript (compactMetadata preTokens=993026)")
}

func TestReadFill_NoUsageLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(`{"type":"other"}`+"\n"), 0644))

	_, ok := ReadFill(path, "")
	require.False(t, ok)
}

func TestReadFill_MissingFile(t *testing.T) {
	_, ok := ReadFill(filepath.Join(t.TempDir(), "absent.jsonl"), "")
	require.False(t, ok)
}

func TestContextFillForLane_MatchesReadFillOnNewestTranscript(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)

	lane := "/Users/allencoates/projects/Clarity/sessions/fixture-lane-2"
	dir := filepath.Join(root, EncodeProjectDir(lane))
	require.NoError(t, os.MkdirAll(dir, 0755))

	path := filepath.Join(dir, "only.jsonl")
	f, err := os.Create(path)
	require.NoError(t, err)
	writeTranscriptLine(t, f, fableUsageLine("claude-sonnet-5", 100_000, 0, 0))
	require.NoError(t, f.Close())

	fill, ok := ContextFillForLane(lane)
	require.True(t, ok)
	require.Equal(t, 50, fill.Pct)
}

func TestPyRound_HalfToEven(t *testing.T) {
	// Python 3's round() rounds an exact .5 to the nearest EVEN integer,
	// not always up - this is the one place this port could silently
	// disagree with the dashboard it must match, so it is pinned directly.
	require.Equal(t, 2, pyRound(2.5))
	require.Equal(t, 4, pyRound(3.5))
	require.Equal(t, 0, pyRound(0.4))
	require.Equal(t, 1, pyRound(0.6))
}

// TestModelWindowLabel_FableNamesGet1M is the Session pane header's own
// requirement (design/cockpit-pane/DECISIONS.md slice 3, "fable-5-1 · 1M
// window"): a fable/1m/opus-named model derives the 1M word.
func TestModelWindowLabel_FableNamesGet1M(t *testing.T) {
	for _, model := range []string{"claude-fable-5-1", "claude-opus-4", "claude-1m-preview"} {
		label, ok := ModelWindowLabel(model)
		require.True(t, ok, model)
		require.Equal(t, "1M window", label, model)
	}
}

func TestModelWindowLabel_OtherNamesGet200k(t *testing.T) {
	label, ok := ModelWindowLabel("claude-sonnet-5")
	require.True(t, ok)
	require.Equal(t, "200k window", label)
}

func TestModelWindowLabel_EmptyModel_NotDerivable(t *testing.T) {
	_, ok := ModelWindowLabel("")
	require.False(t, ok, "an empty model name has nothing to derive a window word from")
}

// unsetMissingRegistry points AccountsRegistryEnvVar at a path that does not
// exist, so LoadAccountsRegistry's nil-map branch runs rather than the real
// machine's own registry - every claudeProjectsRoots test below wants that
// isolation regardless of which env-var branch it exercises.
func unsetMissingRegistry(t *testing.T) {
	t.Helper()
	t.Setenv(AccountsRegistryEnvVar, filepath.Join(t.TempDir(), "no-registry-here.json"))
}

// TestClaudeProjectsRoots_PluralEnvVarWins is precedence branch 1: the
// plural, colon-separated var, when set, wins outright over everything else.
func TestClaudeProjectsRoots_PluralEnvVarWins(t *testing.T) {
	unsetMissingRegistry(t)
	a, b := t.TempDir(), t.TempDir()
	t.Setenv(ClaudeProjectsRootsEnvVar, a+":"+b)
	t.Setenv(ClaudeProjectsRootEnvVar, "/should/never/be/read")

	roots := claudeProjectsRoots()
	require.Len(t, roots, 2)
	require.Equal(t, a, roots[0].Path)
	require.Equal(t, b, roots[1].Path)
}

// TestClaudeProjectsRoots_SingularEnvVarAloneBehavesAsBefore is precedence
// branch 2, and the item (e) requirement: with the plural var unset, the
// singular var alone still resolves to exactly one root, unchanged.
func TestClaudeProjectsRoots_SingularEnvVarAloneBehavesAsBefore(t *testing.T) {
	unsetMissingRegistry(t)
	root := t.TempDir()
	t.Setenv(ClaudeProjectsRootEnvVar, root)

	roots := claudeProjectsRoots()
	require.Len(t, roots, 1)
	require.Equal(t, root, roots[0].Path)
}

// TestClaudeProjectsRoots_DefaultAloneWhenNoRegistry is precedence branch 3:
// neither env var set and no registry resolves to just the default root.
func TestClaudeProjectsRoots_DefaultAloneWhenNoRegistry(t *testing.T) {
	unsetMissingRegistry(t)

	roots := claudeProjectsRoots()
	require.Len(t, roots, 1)
	require.Equal(t, DefaultClaudeProjectsRoot, roots[0].Path)
	require.Empty(t, roots[0].Account)
}

// TestClaudeProjectsRoots_DefaultPlusRegistryAccounts is precedence branch
// 4: neither env var set, but the registry names accounts beyond the
// default's own parent - each becomes an extra root, tagged, existing
// directories only, the default's own account skipped as a duplicate.
func TestClaudeProjectsRoots_DefaultPlusRegistryAccounts(t *testing.T) {
	unsetMissingRegistry(t)

	teamXDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(teamXDir, "projects"), 0755))
	teamGhostDir := t.TempDir() // no "projects" subdir created - must be skipped

	registryPath := filepath.Join(t.TempDir(), "registry.json")
	writeRegistry(t, registryPath, `{
		"accounts": {
			"main": {"config_dir": "`+filepath.Dir(DefaultClaudeProjectsRoot)+`"},
			"team-x": {"config_dir": "`+teamXDir+`"},
			"team-ghost": {"config_dir": "`+teamGhostDir+`"}
		}
	}`)
	t.Setenv(AccountsRegistryEnvVar, registryPath)

	roots := claudeProjectsRoots()

	var byPath = map[string]string{}
	for _, r := range roots {
		byPath[r.Path] = r.Account
	}
	require.Equal(t, "main", byPath[DefaultClaudeProjectsRoot], "the default root itself picks up its registry tag")
	require.Equal(t, "team-x", byPath[filepath.Join(teamXDir, "projects")], "a registry account with an existing projects dir becomes an extra root")
	require.NotContains(t, byPath, filepath.Join(teamGhostDir, "projects"), "a registry account whose projects dir does not exist is never added")
	require.Len(t, roots, 2, "main is the default root, not a second copy of it")
}

// TestClaudeProjectsRoots_DedupesSameConfigDirAcrossTags proves the
// "deduplicated" clause: two tags resolving to the same projects path never
// produce two roots.
func TestClaudeProjectsRoots_DedupesSameConfigDirAcrossTags(t *testing.T) {
	unsetMissingRegistry(t)

	sharedDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(sharedDir, "projects"), 0755))

	registryPath := filepath.Join(t.TempDir(), "registry.json")
	writeRegistry(t, registryPath, `{
		"accounts": {
			"team-x": {"config_dir": "`+sharedDir+`"},
			"team-y": {"config_dir": "`+sharedDir+`"}
		}
	}`)
	t.Setenv(AccountsRegistryEnvVar, registryPath)

	roots := claudeProjectsRoots()
	require.Len(t, roots, 2, "the default root plus one deduplicated extra, not two")
}

// TestNewestTranscript_ResolvesUnderLaneOwnRootNotDefault is item 4's own
// requirement: a lane's transcript under a NON-default root still resolves,
// proving the gauge no longer assumes every lane lives under the default.
func TestNewestTranscript_ResolvesUnderLaneOwnRootNotDefault(t *testing.T) {
	unsetMissingRegistry(t)
	rootA, rootB := t.TempDir(), t.TempDir()
	t.Setenv(ClaudeProjectsRootsEnvVar, rootA+":"+rootB)

	lane := "/Users/allencoates/projects/Clarity/sessions/fixture-cross-root"
	dir := filepath.Join(rootB, EncodeProjectDir(lane))
	require.NoError(t, os.MkdirAll(dir, 0755))
	path := filepath.Join(dir, "a.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0644))

	got, ok := NewestTranscript(lane)
	require.True(t, ok)
	require.Equal(t, path, got)
}
