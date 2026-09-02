// Package clarity: this file supplies the Needs-you tab's explanation and
// recommendation text (design/cockpit-pane/DECISIONS.md slice 5) for a row
// the feed queue itself carries no body for - fleet_queue_build.py's own
// render() writes a four-column table (rank/class/source/title, see
// feed.go's own doc comment), never a body or a recommendation, so a board-
// sourced row (Source "#<n>", BoardIssueNumber above) always falls through
// to the one gh api REST fetch this file makes, cached per issue number for
// the process's life and throttled to at most one retry per minute on
// failure - never gh issue view, which is GraphQL and a different read
// path than the rest of this fork's board contract.
package clarity

import (
	"claude-squad/cmd"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// boardRepo is the fleet's task board (acdigitalclarity/clarity-tasks) -
// the same repo scripts/fleet_queue_build.py reads.
const boardRepo = "acdigitalclarity/clarity-tasks"

// BoardRetryInterval bounds how often a failed fetch is retried - never
// more than once a minute, per the brief, so an offline gh or a rate limit
// is not re-hit on every 3-second feed tick.
const BoardRetryInterval = time.Minute

// BoardExplanation is one issue's parsed body, or a fetch failure reason.
type BoardExplanation struct {
	Explanation    string
	Recommendation string
	// Err is the fetch failure reason, "" on success. A caller renders
	// "board unreachable: <Err>" and shows neither field above.
	Err string
}

// boardCacheEntry pairs a fetched result with the moment it was fetched
// (success) or last attempted (failure) - Get's own retry throttle keys
// off the latter.
type boardCacheEntry struct {
	result    BoardExplanation
	fetchedAt time.Time
}

// BoardCache fetches and caches one clarity-tasks issue's explanation per
// issue number, for the life of the process ("cache per number for the
// session", the brief's own words) - a successful fetch is never re-
// fetched; a failed one is retried at most once a minute. Safe for
// concurrent use, though the current caller (app.go's single-threaded feed
// tick) never needs that.
type BoardCache struct {
	mu      sync.Mutex
	entries map[int]boardCacheEntry
	exec    cmd.Executor
	repo    string
	ghBin   string
}

// NewBoardCache returns a cache that shells out to the real gh binary
// against the fleet's own board repo.
func NewBoardCache() *BoardCache {
	return NewBoardCacheWithDeps(cmd.MakeExecutor(), boardRepo, "gh")
}

// NewBoardCacheWithDeps is NewBoardCache with every external dependency
// injected, for tests - the same cmd.Executor seam session/tmux already
// uses for the real tmux binary.
func NewBoardCacheWithDeps(executor cmd.Executor, repo, ghBin string) *BoardCache {
	return &BoardCache{entries: make(map[int]boardCacheEntry), exec: executor, repo: repo, ghBin: ghBin}
}

// Peek returns whatever is already cached for issue number n WITHOUT
// fetching - ok is false when nothing has been fetched yet, or the last
// attempt failed and is now due a retry (time.Since >= BoardRetryInterval).
// A caller on the main UI thread (app.go's feed tick) uses this to read the
// cache synchronously and dispatches Get in a background tea.Cmd only when
// Peek says there is nothing to show yet - the fetch itself never blocks
// the render loop.
func (c *BoardCache) Peek(n int) (BoardExplanation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[n]
	if !ok {
		return BoardExplanation{}, false
	}
	if e.result.Err != "" && time.Since(e.fetchedAt) >= BoardRetryInterval {
		return BoardExplanation{}, false
	}
	return e.result, true
}

// Get returns the explanation for issue number n, fetching it (one gh api
// REST call) on the first ask and on any ask more than BoardRetryInterval
// after the last failed attempt. This may block on the gh call - callers on
// the UI thread use Peek first and only call Get from a background tea.Cmd.
func (c *BoardCache) Get(n int) BoardExplanation {
	c.mu.Lock()
	if e, ok := c.entries[n]; ok {
		if e.result.Err == "" || time.Since(e.fetchedAt) < BoardRetryInterval {
			c.mu.Unlock()
			return e.result
		}
	}
	c.mu.Unlock()

	result := c.fetch(n)
	c.mu.Lock()
	c.entries[n] = boardCacheEntry{result: result, fetchedAt: time.Now()}
	c.mu.Unlock()
	return result
}

func (c *BoardCache) fetch(n int) BoardExplanation {
	path := fmt.Sprintf("repos/%s/issues/%d", c.repo, n)
	command := exec.Command(c.ghBin, "api", path)
	out, err := c.exec.Output(command)
	if err != nil {
		return BoardExplanation{Err: reasonFromExecError(err)}
	}
	var payload struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return BoardExplanation{Err: fmt.Sprintf("unparseable gh api response: %v", err)}
	}
	explanation, recommendation := ParseBoardBody(payload.Body)
	return BoardExplanation{Explanation: explanation, Recommendation: recommendation}
}

// reasonFromExecError prefers a failed gh call's own stderr (exec.Cmd's
// Output() populates *exec.ExitError.Stderr when the caller never set its
// own Stderr, which cmd.Executor's real implementation never does) over the
// bare "exit status 1" err.Error() would otherwise give - the same
// stderr-first shape fleet_queue_build.py's own GhFailure message uses.
func reasonFromExecError(err error) string {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return strings.TrimSpace(string(exitErr.Stderr))
	}
	return err.Error()
}

// ParseBoardBody splits a board issue's markdown body into the Needs-you
// tab's two fields. The board's own owner-action card shape (README's
// contract: Lane/What/Where/Why/Options/Expected reply, each a "## "
// heading) always opens with a heading, so the explanation is the FIRST
// such section's own content, heading stripped. The card marks its pick
// inline inside "## Options" ("... two minutes. Recommended.") rather than
// under a dedicated heading, so the recommendation is the first paragraph
// anywhere in the body that mentions "recommend" (case-insensitive) -
// "no recommendation on the row" when nothing does.
func ParseBoardBody(body string) (explanation, recommendation string) {
	paragraphs := splitParagraphs(body)
	if len(paragraphs) > 0 {
		explanation = stripHeading(paragraphs[0])
	}
	for _, p := range paragraphs {
		if strings.Contains(strings.ToLower(p), "recommend") {
			recommendation = stripHeading(p)
			break
		}
	}
	if recommendation == "" {
		recommendation = "no recommendation on the row"
	}
	return explanation, recommendation
}

// splitParagraphs splits body on blank lines, trimming each and dropping
// any that are empty after trimming.
func splitParagraphs(body string) []string {
	raw := strings.Split(strings.ReplaceAll(strings.TrimSpace(body), "\r\n", "\n"), "\n\n")
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// stripHeading removes a leading markdown "## Heading" line from a
// paragraph, leaving just its body text - a card's "## Options" paragraph
// (the heading and its list items share one blank-line-delimited block)
// would otherwise print the literal "##" marker.
func stripHeading(p string) string {
	lines := strings.SplitN(p, "\n", 2)
	first := strings.TrimSpace(lines[0])
	if !strings.HasPrefix(first, "#") {
		return p
	}
	if len(lines) == 2 {
		return strings.TrimSpace(lines[1])
	}
	return strings.TrimSpace(strings.TrimLeft(first, "# "))
}
