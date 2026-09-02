package clarity

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestLaneTailCache_UnchangedFileNotReparsed proves the cache's own
// contract directly: with the file's mtime and size held fixed, a SECOND
// call to Get must return the FIRST parse's result even after the file's
// content on disk has since changed underneath it - if Get had reparsed,
// it would see the new (working) state instead of the stale (idle) one
// still cached.
func TestLaneTailCache_UnchangedFileNotReparsed(t *testing.T) {
	now := time.Now()
	idleAt := now.Add(-2 * time.Hour)
	path := writeFixture(t, []string{turnDurationLine(idleAt, 1000, 3, 0)})
	info, err := os.Stat(path)
	require.NoError(t, err)

	c := NewLaneTailCache()
	first, err := c.Get(path, now)
	require.NoError(t, err)
	require.Equal(t, StateIdle, first.State)

	// Overwrite with content that would classify as "working" if reparsed -
	// built to the SAME byte length as the original line (same field digit
	// counts) so this write needs no separate truncate, whose own mtime
	// side effect would otherwise re-bump the file past the Chtimes below.
	workingLine := turnDurationLine(now.Add(-time.Second), 1000, 3, 9) + "\n"
	require.Len(t, workingLine, int(info.Size()), "fixture must not change the file's size for this test to isolate mtime/size staleness")
	require.NoError(t, os.WriteFile(path, []byte(workingLine), 0644))
	require.NoError(t, os.Chtimes(path, info.ModTime(), info.ModTime()))

	second, err := c.Get(path, now)
	require.NoError(t, err)
	require.Equal(t, StateIdle, second.State, "mtime and size unchanged: Get must return the cached (stale) result, not reparse")
}

// TestLaneTailCache_ChangedSizeReparses is the cache's other half: once the
// file's size (or mtime) actually changes, Get must reflect the new
// content, not serve the stale cached entry forever.
func TestLaneTailCache_ChangedSizeReparses(t *testing.T) {
	now := time.Now()
	idleAt := now.Add(-2 * time.Hour)
	path := writeFixture(t, []string{turnDurationLine(idleAt, 1000, 3, 0)})

	c := NewLaneTailCache()
	first, err := c.Get(path, now)
	require.NoError(t, err)
	require.Equal(t, StateIdle, first.State)

	workingAt := now.Add(-time.Second)
	require.NoError(t, os.WriteFile(path, []byte(turnDurationLine(workingAt, 1000, 1, 2)+"\n"), 0644))

	second, err := c.Get(path, now)
	require.NoError(t, err)
	require.Equal(t, StateWorking, second.State, "a changed file must be reparsed, not served from the stale cache")
}
