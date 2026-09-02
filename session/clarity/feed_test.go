package clarity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// sampleQueue mirrors the exact shape fleet_triage_rank.py's
// render_queue_markdown() writes: a header row, a separator row, then one
// data row per item, already ranked ascending by class.
const sampleQueue = `| rank | class | source | title |
| --- | --- | --- | --- |
| 1 | blocked-on-owner | sessions/lane-a/STATUS.md | needs your go on the merge |
| 2 | blocked-on-owner | sessions/lane-b/TASKS.md | waiting on a credential |
| 3 | escalation | sessions/lane-c/STATUS.md | hook failing on unrelated content |
| 4 | fyi | sessions/lane-d/STATUS.md | branch pushed, no action needed |
`

func TestParseQueueMarkdown_SkipsHeaderAndSeparator(t *testing.T) {
	items, err := ParseQueueMarkdown([]byte(sampleQueue))
	require.NoError(t, err)
	require.Len(t, items, 4)
	require.Equal(t, "blocked-on-owner", items[0].Class)
	require.Equal(t, "lane-a", items[0].Lane)
	require.Equal(t, "needs your go on the merge", items[0].Title)
	require.Equal(t, "sessions/lane-a/STATUS.md", items[0].Source)
	require.Equal(t, 1, items[0].Rank)
}

func TestParseQueueMarkdown_EmptyQueueHeaderOnly(t *testing.T) {
	header := "| rank | class | source | title |\n| --- | --- | --- | --- |\n"
	items, err := ParseQueueMarkdown([]byte(header))
	require.NoError(t, err)
	require.Empty(t, items, "a header-only queue is zero rows, not an error")
}

func TestParseQueueMarkdown_IgnoresNonTableLines(t *testing.T) {
	data := "some preamble\n" + sampleQueue + "\ntrailing notes\n"
	items, err := ParseQueueMarkdown([]byte(data))
	require.NoError(t, err)
	require.Len(t, items, 4)
}

func TestLaneFromSource_UnexpectedShapeFallsBackToSource(t *testing.T) {
	require.Equal(t, "bare-title-no-path", laneFromSource("bare-title-no-path"))
}

func TestRankItems_BlockedOnOwnerFirst(t *testing.T) {
	items := []FeedItem{
		{Class: "fyi", Title: "fyi one"},
		{Class: "blocked-on-owner", Title: "urgent one"},
		{Class: "escalation", Title: "escalate one"},
		{Class: "blocked-on-owner", Title: "urgent two"},
	}
	ranked := RankItems(items)
	require.Equal(t, []string{"urgent one", "urgent two", "escalate one", "fyi one"},
		[]string{ranked[0].Title, ranked[1].Title, ranked[2].Title, ranked[3].Title},
		"blocked-on-owner sorts first, ties keep arrival order")
}

func TestRankItems_UnknownClassSortsLastNeverDropped(t *testing.T) {
	items := []FeedItem{
		{Class: "mystery", Title: "unknown class"},
		{Class: "blocked-on-owner", Title: "urgent"},
	}
	ranked := RankItems(items)
	require.Len(t, ranked, 2, "an unrecognised class must never be dropped from the feed")
	require.Equal(t, "urgent", ranked[0].Title)
	require.Equal(t, "unknown class", ranked[1].Title)
}

func TestLoadFeed_AbsentFileReturnsTypedError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.md")
	_, err := LoadFeed(path)
	require.Error(t, err)
	var absent *FeedAbsentError
	require.True(t, errors.As(err, &absent))
	require.Equal(t, path, absent.Path)
}

func TestLoadFeed_ParsesRealFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "FLEET-QUEUE.md")
	require.NoError(t, os.WriteFile(path, []byte(sampleQueue), 0644))

	items, err := LoadFeed(path)
	require.NoError(t, err)
	require.Len(t, items, 4)
}

func TestNeedsYou_AbsentQueueReportsUnconstructed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "FLEET-QUEUE.md")
	lines := NeedsYou(path, 5)
	require.Len(t, lines, 1)
	require.Equal(t, "feed: UNCONSTRUCTED - no queue at "+path, lines[0])
}

func TestNeedsYou_TopNRankedPlainWordsLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "FLEET-QUEUE.md")
	require.NoError(t, os.WriteFile(path, []byte(sampleQueue), 0644))

	lines := NeedsYou(path, 2)
	require.Equal(t, []string{
		"lane-a - needs your go on the merge",
		"lane-b - waiting on a credential",
	}, lines)
}

func TestNeedsYou_EmptyQueueIsNotAnEmptySlice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "FLEET-QUEUE.md")
	header := "| rank | class | source | title |\n| --- | --- | --- | --- |\n"
	require.NoError(t, os.WriteFile(path, []byte(header), 0644))

	lines := NeedsYou(path, 5)
	require.Equal(t, []string{"feed: queue is empty"}, lines)
}

func TestDefaultFeedPath_EnvOverride(t *testing.T) {
	t.Setenv(FeedQueuePathEnvVar, "/tmp/custom-queue.md")
	require.Equal(t, "/tmp/custom-queue.md", DefaultFeedPath())
}

// --- built: line age (board #280) -------------------------------------

func TestParseBuiltAt_ValidLine(t *testing.T) {
	data := []byte("built: 2026-09-02T15:10:41Z source: board+lanes\n" + sampleQueue)
	got, ok := ParseBuiltAt(data)
	require.True(t, ok)
	require.True(t, got.Equal(time.Date(2026, 9, 2, 15, 10, 41, 0, time.UTC)))
}

func TestParseBuiltAt_LineAbsentReturnsNotOK(t *testing.T) {
	_, ok := ParseBuiltAt([]byte(sampleQueue)) // legacy queue, no built: line
	require.False(t, ok)
}

func TestParseBuiltAt_UnparseableTimestampReturnsNotOK(t *testing.T) {
	data := []byte("built: not-a-timestamp source: board+lanes\n" + sampleQueue)
	_, ok := ParseBuiltAt(data)
	require.False(t, ok)
}

func TestFeedHeaderStatus_FreshWithinTenMinutesIsBlank(t *testing.T) {
	now := time.Date(2026, 9, 2, 15, 20, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "FLEET-QUEUE.md")
	require.NoError(t, os.WriteFile(path,
		[]byte("built: 2026-09-02T15:13:00Z source: board+lanes\n"+sampleQueue), 0644))

	require.Equal(t, "", FeedHeaderStatus(path, now), "7 minutes old is inside StaleAfter")
}

// STALE state: built: line present but older than 10 minutes.
func TestFeedHeaderStatus_StaleAfterTenMinutes(t *testing.T) {
	now := time.Date(2026, 9, 2, 15, 33, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "FLEET-QUEUE.md")
	require.NoError(t, os.WriteFile(path,
		[]byte("built: 2026-09-02T15:10:00Z source: board+lanes\n"+sampleQueue), 0644))

	require.Equal(t, "STALE 23m", FeedHeaderStatus(path, now))
}

// UNCONSTRUCTED state, absent-file variant: the queue was never built.
func TestFeedHeaderStatus_UnconstructedOnMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.md")
	require.Equal(t, "UNCONSTRUCTED", FeedHeaderStatus(path, time.Now()))
}

// UNCONSTRUCTED state, no-built-line variant: a legacy/hand-edited queue.
func TestFeedHeaderStatus_UnconstructedOnMissingBuiltLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "FLEET-QUEUE.md")
	require.NoError(t, os.WriteFile(path, []byte(sampleQueue), 0644))

	require.Equal(t, "UNCONSTRUCTED", FeedHeaderStatus(path, time.Now()))
}

func TestNeedsYouTitle_Fresh(t *testing.T) {
	now := time.Date(2026, 9, 2, 15, 20, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "FLEET-QUEUE.md")
	require.NoError(t, os.WriteFile(path,
		[]byte("built: 2026-09-02T15:18:00Z source: board+lanes\n"+sampleQueue), 0644))

	require.Equal(t, "Needs you", NeedsYouTitle(path, now))
}

func TestNeedsYouTitle_Stale(t *testing.T) {
	now := time.Date(2026, 9, 2, 15, 33, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "FLEET-QUEUE.md")
	require.NoError(t, os.WriteFile(path,
		[]byte("built: 2026-09-02T15:10:00Z source: board+lanes\n"+sampleQueue), 0644))

	require.Equal(t, "Needs you (STALE 23m)", NeedsYouTitle(path, now))
}

func TestNeedsYouTitle_Unconstructed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.md")
	require.Equal(t, "Needs you (UNCONSTRUCTED)", NeedsYouTitle(path, time.Now()))
}
