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

// BoardSection is one labeled part of a Needs-you row's explanation - a
// card's own "## What"/"## Where"/"## Why" heading (Label holds the plain
// word, e.g. "What"), or a single unlabeled section (Label "") holding the
// body's own first paragraphs when the card carries no "## " headings at
// all (board #280's slice 5b, DEFECT 1).
type BoardSection struct {
	Label string
	Text  string
}

// BoardOption is one line of a card's "## Options" list (or its single
// "## Recommendation"/"## Recommended" paragraph, folded into one option
// when the card names no lettered list) - Recommended marks whichever line
// the card itself calls out inline (e.g. "... two minutes. Recommended.").
type BoardOption struct {
	Text        string
	Recommended bool
}

// BoardExplanation is one issue's parsed card, or a fetch failure reason.
type BoardExplanation struct {
	// Lane is the row's raising lane: the "## Lane" section's own content,
	// falling back to the issue's own "lane:<name>" label when the body
	// carries no Lane section; "" when neither resolves (slice 5b, DEFECT
	// 2) - a caller never claims a delivery target this field leaves empty.
	Lane string
	// Explanation is the What/Where/Why sections in reading order, or a
	// single unlabeled section holding the body's own paragraphs when the
	// card carries no headings at all (free prose). Empty when the body has
	// neither.
	Explanation []BoardSection
	// Options is the card's own Options list (or its single Recommendation/
	// Recommended paragraph, as one option) - nil when the card names none.
	Options []BoardOption
	// ExpectedReply is the "## Expected reply" section's own content, ""
	// when the card carries none.
	ExpectedReply string
	// Also holds anything on the row not classified into the fields above
	// (an unrecognised heading, or a preamble before the first heading) -
	// never dropped silently, "" when nothing is left over.
	Also string
	// Err is the fetch failure reason, "" on success. A caller renders
	// "board unreachable: <Err>" and shows none of the fields above.
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
		Body   string `json:"body"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return BoardExplanation{Err: fmt.Sprintf("unparseable gh api response: %v", err)}
	}
	labels := make([]string, len(payload.Labels))
	for i, l := range payload.Labels {
		labels[i] = l.Name
	}
	return ParseBoardBody(payload.Body, labels)
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

// laneLabelPrefix is the board's own "lane:<name>" issue label
// (acdigitalclarity/clarity-tasks convention, confirmed on issues 243, 244
// and 277 via `gh api .../issues/<n> --jq '.labels[].name'`) - ParseBoard-
// Body's fallback when the body itself carries no "## Lane" section.
const laneLabelPrefix = "lane:"

// ParseBoardBody splits a board issue's markdown body (and, for the Lane
// fallback, its GitHub labels) into the Needs-you tab's fields (board #280,
// slice 5b, DEFECT 1 and DEFECT 2). The board's own owner-action card shape
// (README's contract: Lane/What/Where/Why/Options/Expected reply, each a
// "## " heading) is classified heading by heading; a body with no "## "
// heading at all is free prose - its own paragraphs become the explanation,
// with whichever paragraph first mentions "recommend" pulled out as the
// recommendation instead. Anything on the row that classifies into neither
// shape - an unrecognised heading, or text before the first heading - is
// never dropped: it lands in Also.
func ParseBoardBody(body string, labels []string) BoardExplanation {
	var out BoardExplanation
	sections := splitHeadedSections(body)
	if sections == nil {
		out = parseFreeProseBody(body)
	} else {
		for _, s := range sections {
			classifySection(&out, s.heading, s.body)
		}
	}
	if out.Lane == "" {
		out.Lane = laneFromLabels(labels)
	}
	return out
}

// classifySection files one "## Heading" section's content into out,
// matching the board README's own six headings by plain-word name
// (case-insensitive) and routing anything else to Also.
func classifySection(out *BoardExplanation, heading, body string) {
	switch strings.ToLower(strings.TrimSpace(heading)) {
	case "lane":
		out.Lane = firstLine(body)
	case "what":
		if body != "" {
			out.Explanation = append(out.Explanation, BoardSection{Label: "What", Text: body})
		}
	case "where":
		if body != "" {
			out.Explanation = append(out.Explanation, BoardSection{Label: "Where", Text: body})
		}
	case "why":
		if body != "" {
			out.Explanation = append(out.Explanation, BoardSection{Label: "Why", Text: body})
		}
	case "options":
		out.Options = append(out.Options, parseOptions(body)...)
	case "recommendation", "recommended":
		if body != "" {
			out.Options = append(out.Options, BoardOption{Text: body, Recommended: true})
		}
	case "expected reply":
		out.ExpectedReply = body
	default:
		if body != "" {
			out.Also = appendAlso(out.Also, heading, body)
		}
	}
}

// parseFreeProseBody is ParseBoardBody's fallback for a body with no "## "
// heading at all: every paragraph that first mentions "recommend" (case-
// insensitive) becomes a marked option; everything else becomes the
// explanation's own single unlabeled section, in the body's own order.
func parseFreeProseBody(body string) BoardExplanation {
	var out BoardExplanation
	var explanationParas []string
	for _, p := range splitParagraphs(body) {
		if strings.Contains(strings.ToLower(p), "recommend") {
			out.Options = append(out.Options, BoardOption{Text: p, Recommended: true})
			continue
		}
		explanationParas = append(explanationParas, p)
	}
	if len(explanationParas) > 0 {
		out.Explanation = []BoardSection{{Text: strings.Join(explanationParas, "\n\n")}}
	}
	return out
}

// headedSection is one "## Heading" block: the heading text (without the
// "## " marker) and everything up to the next heading or the body's end.
type headedSection struct {
	heading string
	body    string
}

// splitHeadedSections scans body for lines starting with "## " and splits
// it into one headedSection per heading, plus a leading section (heading
// "") for any text before the first heading. Returns nil when the body
// carries no "## " heading at all - ParseBoardBody's own signal to fall
// back to free-prose parsing.
func splitHeadedSections(body string) []headedSection {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var sections []headedSection
	heading := ""
	var cur []string
	sawHeading := false
	flush := func() {
		sections = append(sections, headedSection{heading: heading, body: strings.TrimSpace(strings.Join(cur, "\n"))})
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			flush()
			heading = strings.TrimSpace(strings.TrimPrefix(trimmed, "##"))
			cur = nil
			sawHeading = true
			continue
		}
		cur = append(cur, line)
	}
	flush()
	if !sawHeading {
		return nil
	}
	return sections
}

// parseOptions splits an "## Options" section's own content into one
// BoardOption per non-empty line, marking whichever line mentions
// "recommend" (case-insensitive) - the card's own inline pick ("(a) ...
// Recommended.").
func parseOptions(text string) []BoardOption {
	var opts []BoardOption
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		opts = append(opts, BoardOption{Text: l, Recommended: strings.Contains(strings.ToLower(l), "recommend")})
	}
	return opts
}

// appendAlso accumulates unclassified body text under one running string,
// each block labeled by its own heading (when it had one) so a reader can
// tell where it came from.
func appendAlso(existing, heading, body string) string {
	block := body
	if heading != "" {
		block = heading + ": " + body
	}
	if existing == "" {
		return block
	}
	return existing + "\n\n" + block
}

// laneFromLabels returns the name half of the first "lane:<name>" label,
// "" when none of labels carries that prefix.
func laneFromLabels(labels []string) string {
	for _, l := range labels {
		if strings.HasPrefix(l, laneLabelPrefix) {
			return strings.TrimPrefix(l, laneLabelPrefix)
		}
	}
	return ""
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
