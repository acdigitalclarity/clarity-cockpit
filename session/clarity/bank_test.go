package clarity

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBankLine_IsExactlyTheStandardInstruction(t *testing.T) {
	require.Equal(t, "bank state now: write the continuation from cells, then stop", BankLine)
}

// test 12: b sends the bank line verbatim; the watcher reports the newest
// post-send CONTINUATION-*.md in the lane folder and ignores one written
// before the send.

func writeFileAt(t *testing.T, path, content string, mtime time.Time) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	require.NoError(t, os.Chtimes(path, mtime, mtime))
}

func TestFindContinuationFile_IgnoresFileWrittenBeforeSend(t *testing.T) {
	dir := t.TempDir()
	before := time.Now().Add(-time.Hour)
	writeFileAt(t, filepath.Join(dir, "CONTINUATION-old.md"), "old", before)

	sendAt := time.Now()
	_, ok := FindContinuationFile(dir, sendAt)
	require.False(t, ok, "a file written before the send must not count as the answer")
}

func TestFindContinuationFile_FindsFileWrittenAfterSend(t *testing.T) {
	dir := t.TempDir()
	sendAt := time.Now()

	after := sendAt.Add(time.Minute)
	writeFileAt(t, filepath.Join(dir, "CONTINUATION-2026-09-03-1147.md"), "new", after)

	path, ok := FindContinuationFile(dir, sendAt)
	require.True(t, ok)
	require.Equal(t, "CONTINUATION-2026-09-03-1147.md", filepath.Base(path))
	require.True(t, filepath.IsAbs(path), "the foot shows the absolute path in full")
}

func TestFindContinuationFile_NewestMatchWins(t *testing.T) {
	dir := t.TempDir()
	sendAt := time.Now()

	writeFileAt(t, filepath.Join(dir, "CONTINUATION-a.md"), "a", sendAt.Add(1*time.Minute))
	writeFileAt(t, filepath.Join(dir, "CONTINUATION-b.md"), "b", sendAt.Add(2*time.Minute))

	path, ok := FindContinuationFile(dir, sendAt)
	require.True(t, ok)
	require.Equal(t, "CONTINUATION-b.md", filepath.Base(path))
}

func TestFindContinuationFile_NonRecursive_IgnoresSubdirectory(t *testing.T) {
	dir := t.TempDir()
	sendAt := time.Now()
	sub := filepath.Join(dir, "subdir")
	require.NoError(t, os.Mkdir(sub, 0755))
	writeFileAt(t, filepath.Join(sub, "CONTINUATION-nested.md"), "nested", sendAt.Add(time.Minute))

	_, ok := FindContinuationFile(dir, sendAt)
	require.False(t, ok, "the watch is non-recursive - a file one level down must not count")
}

func TestFindContinuationFile_IgnoresNonMatchingNames(t *testing.T) {
	dir := t.TempDir()
	sendAt := time.Now()
	writeFileAt(t, filepath.Join(dir, "NOTES.md"), "x", sendAt.Add(time.Minute))
	writeFileAt(t, filepath.Join(dir, "CONTINUATION-not-markdown.txt"), "x", sendAt.Add(time.Minute))

	_, ok := FindContinuationFile(dir, sendAt)
	require.False(t, ok)
}

func TestFindContinuationFile_MissingDir_NotOK(t *testing.T) {
	_, ok := FindContinuationFile(filepath.Join(t.TempDir(), "does-not-exist"), time.Now())
	require.False(t, ok)
}
