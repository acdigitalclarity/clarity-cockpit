package clarity

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestLaneTailCache_UnchangedFileNotReparsed proves the cache's own
// contract directly: with the file's mtime and size held fixed, a SECOND
// call to Get must return the FIRST parse's result even after the file's
// content on disk has since changed underneath it - if Get had reparsed,
// it would see the new (working) state instead of the stale (waiting on
// you, item 5's held state for an old closed turn) one still cached.
func TestLaneTailCache_UnchangedFileNotReparsed(t *testing.T) {
	now := time.Now()
	staleAt := now.Add(-2 * time.Hour)
	path := writeFixture(t, []string{turnDurationLine(staleAt, 1000, 3, 0)})
	info, err := os.Stat(path)
	require.NoError(t, err)

	c := NewLaneTailCache()
	first, err := c.Get(path, 0, now, time.Time{})
	require.NoError(t, err)
	require.Equal(t, StateWaitingYou, first.State)

	// Overwrite with content that would classify as "working" if reparsed -
	// built to the SAME byte length as the original line (same field digit
	// counts) so this write needs no separate truncate, whose own mtime
	// side effect would otherwise re-bump the file past the Chtimes below.
	workingLine := turnDurationLine(now.Add(-time.Second), 1000, 3, 9) + "\n"
	require.Len(t, workingLine, int(info.Size()), "fixture must not change the file's size for this test to isolate mtime/size staleness")
	require.NoError(t, os.WriteFile(path, []byte(workingLine), 0644))
	require.NoError(t, os.Chtimes(path, info.ModTime(), info.ModTime()))

	second, err := c.Get(path, 0, now, time.Time{})
	require.NoError(t, err)
	require.Equal(t, StateWaitingYou, second.State, "mtime and size unchanged: Get must return the cached (stale) result, not reparse")
}

// TestLaneTailCache_ChangedSizeReparses is the cache's other half: once the
// file's size (or mtime) actually changes, Get must reflect the new
// content, not serve the stale cached entry forever.
func TestLaneTailCache_ChangedSizeReparses(t *testing.T) {
	now := time.Now()
	staleAt := now.Add(-2 * time.Hour)
	path := writeFixture(t, []string{turnDurationLine(staleAt, 1000, 3, 0)})

	c := NewLaneTailCache()
	first, err := c.Get(path, 0, now, time.Time{})
	require.NoError(t, err)
	require.Equal(t, StateWaitingYou, first.State)

	workingAt := now.Add(-time.Second)
	require.NoError(t, os.WriteFile(path, []byte(turnDurationLine(workingAt, 1000, 1, 2)+"\n"), 0644))

	second, err := c.Get(path, 0, now, time.Time{})
	require.NoError(t, err)
	require.Equal(t, StateWorking, second.State, "a changed file must be reparsed, not served from the stale cache")
}

// TestLaneTailCache_WiderMaxTurnsReparses is the Session pane's own
// requirement: a caller asking for more turns than the cached entry was
// built with (list rows read the bare default; the Session pane asks for
// 40, see design/cockpit-pane/DECISIONS.md slice 3) must get a fresh read
// carrying that many turns, never the narrower cached slice.
func TestLaneTailCache_WiderMaxTurnsReparses(t *testing.T) {
	now := time.Date(2026, 9, 2, 20, 0, 0, 0, time.UTC)
	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, ownerLine(now.Add(-time.Duration(10-i)*time.Minute), fmt.Sprintf("turn %d", i)))
	}
	path := writeFixture(t, lines)

	c := NewLaneTailCache()
	first, err := c.Get(path, 3, now, time.Time{})
	require.NoError(t, err)
	require.Len(t, first.Turns, 3, "the narrower request must be served exactly that many turns")

	second, err := c.Get(path, 8, now, time.Time{})
	require.NoError(t, err)
	require.Len(t, second.Turns, 8, "a wider request against the same unchanged file must reparse for the extra history, not return the narrower cached slice")
}

// fortyTurnFixtureLines builds a representative fixture at the Session
// pane's own read size (app/app.go's sessionMaxTurns == 40) - the shape the
// Latency ruling's rule 1 asks to have its per-tick cost measured against,
// rather than a single-line toy fixture.
func fortyTurnFixtureLines(now time.Time) []string {
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, ownerLine(now.Add(-time.Duration(40-i)*time.Minute),
			fmt.Sprintf("turn number %d, a bit more realistic in length than a bare word", i)))
	}
	return lines
}

// BenchmarkLaneTailCache_Get_UnchangedFile measures rule 1's own claim ("an
// unchanged file costs one stat") at that 40-turn size: the file's mtime
// and size never change across b.N calls, so every call after the first
// must be served from the cache - an os.Stat plus a map lookup, never a
// reparse of the fixture.
func BenchmarkLaneTailCache_Get_UnchangedFile(b *testing.B) {
	now := time.Now()
	path := filepath.Join(b.TempDir(), "bench.jsonl")
	body := ""
	for _, l := range fortyTurnFixtureLines(now) {
		body += l + "\n"
	}
	require.NoError(b, os.WriteFile(path, []byte(body), 0644))

	c := NewLaneTailCache()
	if _, err := c.Get(path, 40, now, time.Time{}); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.Get(path, 40, now, time.Time{}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLaneTailCache_Get_ChangedFile is rule 1's other half: the file
// grows by one line before every call (a genuine new transcript record
// every tick), forcing a real reparse each time - the ceiling the unchanged
// case above is measured against.
func BenchmarkLaneTailCache_Get_ChangedFile(b *testing.B) {
	now := time.Now()
	path := filepath.Join(b.TempDir(), "bench.jsonl")
	base := fortyTurnFixtureLines(now)

	c := NewLaneTailCache()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		body := ""
		for _, l := range base {
			body += l + "\n"
		}
		body += ownerLine(now.Add(time.Duration(i)*time.Millisecond), fmt.Sprintf("live turn %d", i)) + "\n"
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if _, err := c.Get(path, 40, now, time.Time{}); err != nil {
			b.Fatal(err)
		}
	}
}
