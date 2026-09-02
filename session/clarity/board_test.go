package clarity

import (
	"claude-squad/cmd/cmd_test"
	"errors"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBoardIssueNumber_MatchesBareHashSource(t *testing.T) {
	n, ok := BoardIssueNumber("#277")
	require.True(t, ok)
	require.Equal(t, 277, n)
}

func TestBoardIssueNumber_LaneFileSourceIsNotAnIssue(t *testing.T) {
	_, ok := BoardIssueNumber("sessions/lane-a/STATUS.md:12")
	require.False(t, ok)
}

// realIssueBody is issue #277's actual body (fetched via `gh api
// repos/acdigitalclarity/clarity-tasks/issues/277`, 2 Sep 2026) - the
// board's own six-heading owner-action card shape, with its pick marked
// inline inside "## Options" rather than under a dedicated heading.
const realIssueBody = "## Lane\nways-of-working\n\n## What\nTwo edits in /Users/allencoates/.claude/settings.json.\n1. Move the hook entry whose command runs /Users/allencoates/.claude/hooks/state-claim-warn.py from the PostToolUse list into a Stop list (same command, same shape, no matcher).\n2. Add a SessionStart entry whose command is: python3 /Users/allencoates/projects/Clarity/scripts/specialist_dispatch_check.py\n\n## Where\nThat file, in any editor. Then start one new Claude session and read its boot lines: the specialist line prints on boot, and the campaign's last check (board #214) passes.\n\n## Why\nThe state-claim warning hook was built and tested as an end-of-turn check; wired after every tool call its warning log measures the wrong thing, and campaign A's last ratchet (#214) is correctly refusing to pass until the wiring matches its declaration. The boot line (#266) prints how many ServiceNow-surface legs went to a specialist that day. The lane's sessions are not permitted to edit that file, so they stopped rather than route around it. Backup copies of the file sit beside it (settings.json.bak-20260902-*).\n\n## Options\n(a) Make both edits yourself, two minutes. Recommended.\n(b) Say \"apply it\" in a fresh session that is allowed to.\n\n## Expected reply\n\"done\" on this row, or \"apply it\".\n"

func TestParseBoardBody_RealCardShape_FirstSectionAndInlineRecommendation(t *testing.T) {
	explanation, recommendation := ParseBoardBody(realIssueBody)
	require.Equal(t, "ways-of-working", explanation, "the first \"## \" section's own content, heading stripped")
	require.Equal(t,
		"(a) Make both edits yourself, two minutes. Recommended.\n(b) Say \"apply it\" in a fresh session that is allowed to.",
		recommendation, "the Options paragraph, since it is the first to mention \"recommend\"")
}

func TestParseBoardBody_NoRecommendMention_SaysSo(t *testing.T) {
	_, recommendation := ParseBoardBody("## Lane\nsome-lane\n\n## What\njust do the thing")
	require.Equal(t, "no recommendation on the row", recommendation)
}

func TestParseBoardBody_EmptyBody(t *testing.T) {
	explanation, recommendation := ParseBoardBody("")
	require.Equal(t, "", explanation)
	require.Equal(t, "no recommendation on the row", recommendation)
}

func TestBoardCache_FetchesAndCachesOnSuccess(t *testing.T) {
	calls := 0
	exec := cmd_test.MockCmdExec{
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			calls++
			return []byte(`{"body":"## Lane\nsome-lane"}`), nil
		},
	}
	c := NewBoardCacheWithDeps(exec, "acdigitalclarity/clarity-tasks", "gh")

	first := c.Get(277)
	require.Equal(t, "", first.Err)
	require.Equal(t, "some-lane", first.Explanation)

	second := c.Get(277)
	require.Equal(t, "some-lane", second.Explanation)
	require.Equal(t, 1, calls, "a successful fetch is cached for the rest of the process's life, never re-fetched")
}

func TestBoardCache_GhApiCallShapeIsRESTNeverGraphQL(t *testing.T) {
	var gotArgs []string
	exec := cmd_test.MockCmdExec{
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			gotArgs = cmd.Args
			return []byte(`{"body":""}`), nil
		},
	}
	c := NewBoardCacheWithDeps(exec, "acdigitalclarity/clarity-tasks", "gh")
	c.Get(244)

	require.Equal(t, []string{"gh", "api", "repos/acdigitalclarity/clarity-tasks/issues/244"}, gotArgs,
		"gh api <path> is the REST call the brief names, never `gh issue view` (GraphQL)")
}

func TestBoardCache_FetchFailure_ReportsReasonAndRetriesAtMostOncePerMinute(t *testing.T) {
	calls := 0
	exec := cmd_test.MockCmdExec{
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			calls++
			return nil, errors.New("rate limited")
		},
	}
	c := NewBoardCacheWithDeps(exec, "acdigitalclarity/clarity-tasks", "gh")

	first := c.Get(1)
	require.Equal(t, "rate limited", first.Err)
	require.Equal(t, 1, calls)

	// Asked again immediately: within BoardRetryInterval, must not re-fire.
	second := c.Get(1)
	require.Equal(t, "rate limited", second.Err)
	require.Equal(t, 1, calls, "a failed fetch must not retry more than once a minute")

	// Force the cached entry stale (older than BoardRetryInterval) and ask
	// again: this time it must retry.
	c.mu.Lock()
	e := c.entries[1]
	e.fetchedAt = time.Now().Add(-2 * BoardRetryInterval)
	c.entries[1] = e
	c.mu.Unlock()

	third := c.Get(1)
	require.Equal(t, "rate limited", third.Err)
	require.Equal(t, 2, calls, "past the retry interval, a failed fetch is tried again")
}

func TestBoardCache_UnparseableJSON(t *testing.T) {
	exec := cmd_test.MockCmdExec{
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			return []byte("not json"), nil
		},
	}
	c := NewBoardCacheWithDeps(exec, "acdigitalclarity/clarity-tasks", "gh")
	got := c.Get(1)
	require.Contains(t, got.Err, "unparseable")
}

func TestBoardCache_Peek_NotYetFetched(t *testing.T) {
	c := NewBoardCacheWithDeps(cmd_test.MockCmdExec{}, "acdigitalclarity/clarity-tasks", "gh")
	_, ok := c.Peek(1)
	require.False(t, ok, "nothing has been fetched yet")
}

func TestBoardCache_Peek_ReturnsCachedResultWithoutFetching(t *testing.T) {
	calls := 0
	exec := cmd_test.MockCmdExec{
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			calls++
			return []byte(`{"body":"## Lane\nsome-lane"}`), nil
		},
	}
	c := NewBoardCacheWithDeps(exec, "acdigitalclarity/clarity-tasks", "gh")
	c.Get(1)
	require.Equal(t, 1, calls)

	got, ok := c.Peek(1)
	require.True(t, ok)
	require.Equal(t, "some-lane", got.Explanation)
	require.Equal(t, 1, calls, "Peek must never itself trigger a fetch")
}

func TestBoardCache_Peek_FailedFetchDueForRetryReportsNotOK(t *testing.T) {
	exec := cmd_test.MockCmdExec{
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) { return nil, errors.New("offline") },
	}
	c := NewBoardCacheWithDeps(exec, "acdigitalclarity/clarity-tasks", "gh")
	c.Get(1)

	c.mu.Lock()
	e := c.entries[1]
	e.fetchedAt = time.Now().Add(-2 * BoardRetryInterval)
	c.entries[1] = e
	c.mu.Unlock()

	_, ok := c.Peek(1)
	require.False(t, ok, "a failed fetch past the retry interval is due another try, not served stale")
}

func TestNewBoardCache_UsesFleetBoardRepo(t *testing.T) {
	c := NewBoardCache()
	require.Equal(t, "acdigitalclarity/clarity-tasks", c.repo)
	require.Equal(t, "gh", c.ghBin)
}

func TestReasonFromExecError_PrefersStderr(t *testing.T) {
	exitErr := &exec.ExitError{Stderr: []byte("HTTP 403: rate limit exceeded\n")}
	require.Equal(t, "HTTP 403: rate limit exceeded", reasonFromExecError(exitErr))
}

func TestReasonFromExecError_FallsBackToErrString(t *testing.T) {
	plain := fmt.Errorf("could not run gh: not found")
	require.Equal(t, "could not run gh: not found", reasonFromExecError(plain))
}
