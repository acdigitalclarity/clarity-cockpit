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

func TestParseBoardBody_HeadedShape_ClassifiesEverySection(t *testing.T) {
	got := ParseBoardBody(realIssueBody, nil)

	require.Equal(t, "ways-of-working", got.Lane, "the \"## Lane\" section's own content")
	require.Equal(t, []BoardSection{
		{Label: "What", Text: "Two edits in /Users/allencoates/.claude/settings.json.\n1. Move the hook entry whose command runs /Users/allencoates/.claude/hooks/state-claim-warn.py from the PostToolUse list into a Stop list (same command, same shape, no matcher).\n2. Add a SessionStart entry whose command is: python3 /Users/allencoates/projects/Clarity/scripts/specialist_dispatch_check.py"},
		{Label: "Where", Text: "That file, in any editor. Then start one new Claude session and read its boot lines: the specialist line prints on boot, and the campaign's last check (board #214) passes."},
		{Label: "Why", Text: "The state-claim warning hook was built and tested as an end-of-turn check; wired after every tool call its warning log measures the wrong thing, and campaign A's last ratchet (#214) is correctly refusing to pass until the wiring matches its declaration. The boot line (#266) prints how many ServiceNow-surface legs went to a specialist that day. The lane's sessions are not permitted to edit that file, so they stopped rather than route around it. Backup copies of the file sit beside it (settings.json.bak-20260902-*)."},
	}, got.Explanation, "the What, Where and Why sections, plain-worded labels")
	require.Equal(t, []BoardOption{
		{Text: "(a) Make both edits yourself, two minutes. Recommended.", Recommended: true},
		{Text: "(b) Say \"apply it\" in a fresh session that is allowed to.", Recommended: false},
	}, got.Options, "the Options list, the inline pick marked")
	require.Equal(t, "\"done\" on this row, or \"apply it\".", got.ExpectedReply)
	require.Equal(t, "", got.Also, "every section on this card is classified - nothing left over")
}

func TestParseBoardBody_LaneFallsBackToIssueLabelWhenBodyNamesNone(t *testing.T) {
	got := ParseBoardBody("## What\njust do the thing", []string{"type:owner-action", "lane:some-lane"})
	require.Equal(t, "some-lane", got.Lane)
}

func TestParseBoardBody_LaneUnresolvedWhenNeitherBodyNorLabelNamesOne(t *testing.T) {
	got := ParseBoardBody("## What\njust do the thing", []string{"type:owner-action", "priority:now"})
	require.Equal(t, "", got.Lane)
}

func TestParseBoardBody_UnrecognisedHeadingGoesUnderAlso(t *testing.T) {
	got := ParseBoardBody("## Lane\nsome-lane\n\n## Notes\na side note the card carries with no home of its own", nil)
	require.Equal(t, "Notes: a side note the card carries with no home of its own", got.Also,
		"never dropped silently - filed under Also, heading kept so a reader knows where it came from")
}

// freeProseIssueBody is a plausible board comment with no card structure at
// all (an early or hand-written row, never run through the six-heading
// generator) - ParseBoardBody's other fixture shape, per the brief.
const freeProseIssueBody = "This is a general status update with no card structure. The migration finished overnight and the dashboards are green.\n\nWe recommend rolling the change to production this week rather than waiting on the next review cycle."

func TestParseBoardBody_FreeProseShape_ExplanationIsBodyParagraphsRecommendationPulledOut(t *testing.T) {
	got := ParseBoardBody(freeProseIssueBody, nil)

	require.Equal(t, []BoardSection{{Text: "This is a general status update with no card structure. The migration finished overnight and the dashboards are green."}},
		got.Explanation, "the body's own first paragraphs, unlabeled - no headings to classify by")
	require.Equal(t, []BoardOption{{Text: "We recommend rolling the change to production this week rather than waiting on the next review cycle.", Recommended: true}},
		got.Options, "the paragraph that first mentions \"recommend\"")
	require.Equal(t, "", got.Lane)
	require.Equal(t, "", got.ExpectedReply)
	require.Equal(t, "", got.Also)
}

func TestParseBoardBody_FreeProseShape_NoRecommendMention_OptionsEmpty(t *testing.T) {
	got := ParseBoardBody("Just a plain note, nothing more to say about it.", nil)
	require.Nil(t, got.Options)
	require.Equal(t, []BoardSection{{Text: "Just a plain note, nothing more to say about it."}}, got.Explanation)
}

func TestParseBoardBody_EmptyBody(t *testing.T) {
	got := ParseBoardBody("", nil)
	require.Nil(t, got.Explanation)
	require.Nil(t, got.Options)
	require.Equal(t, "", got.Lane)
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
	require.Equal(t, "some-lane", first.Lane)

	second := c.Get(277)
	require.Equal(t, "some-lane", second.Lane)
	require.Equal(t, 1, calls, "a successful fetch is cached for the rest of the process's life, never re-fetched")
}

func TestBoardCache_LaneFallsBackToIssueLabelFromTheSameFetch(t *testing.T) {
	exec := cmd_test.MockCmdExec{
		OutputFunc: func(cmd *exec.Cmd) ([]byte, error) {
			return []byte(`{"body":"## What\nx","labels":[{"name":"type:owner-action"},{"name":"lane:ways-of-working"}]}`), nil
		},
	}
	c := NewBoardCacheWithDeps(exec, "acdigitalclarity/clarity-tasks", "gh")
	got := c.Get(1)
	require.Equal(t, "ways-of-working", got.Lane, "the issue's own labels ride in on the same gh api response")
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
	require.Equal(t, "some-lane", got.Lane)
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
