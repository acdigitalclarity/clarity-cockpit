// Package clarity: the y-key answer flow's own small pure helpers (slice
// 18, ANSWER-AND-BANK-SPEC.md item 6) - which option a card's own
// recommendation names, the exact text that gets sent, and the exact
// two-line board comment body that follows it. app.go orchestrates the two
// actual writes (the reply, then PostComment above); nothing here performs
// either.
package clarity

import (
	"fmt"
	"strings"
	"time"
)

// answerRecommendedSuffix is the card's own inline pick, verbatim
// (board.go's parseOptions/classifySection: "... two minutes. Recommended."
// or a lone "## Recommendation" paragraph) - AnswerText strips exactly this
// trailing text and nothing else, so the sent reply never repeats a word
// that was only ever meant for the reader of the card, not the lane it is
// sent to.
const answerRecommendedSuffix = " Recommended."

// AnswerText strips a trailing " Recommended." from text (test 1: "leaves
// every other character of the option alone") - a no-op when text does not
// end with exactly that suffix.
func AnswerText(text string) string {
	return strings.TrimSuffix(text, answerRecommendedSuffix)
}

// ChosenOption is the y key's own pick (test 2): the first option marked
// Recommended, the first option when none is marked, or ok=false when opts
// is empty - a card with options but no Options section marked recommended
// still gets an answer (the first one), the y key never refuses just
// because the card itself did not call one out.
func ChosenOption(opts []BoardOption) (option BoardOption, index int, ok bool) {
	if len(opts) == 0 {
		return BoardOption{}, -1, false
	}
	for i, o := range opts {
		if o.Recommended {
			return o, i, true
		}
	}
	return opts[0], 0, true
}

// AnswerCommentBody is the board comment's own exact two-line body
// (ANSWER-AND-BANK-SPEC.md "Exact strings"): "answered from the cockpit:
// <text>" then either "sent into <lane> at hh:mm:ss." (a tracked delivery)
// or "copied for <lane> (external lane); paste pending." (the copy path) -
// at is the SAME moment the reply itself landed (app.go's deliverResult.at),
// never a fresh time.Now() taken separately, so the two never disagree.
func AnswerCommentBody(text, lane string, isExternal bool, at time.Time) string {
	second := fmt.Sprintf("sent into %s at %s.", lane, at.Local().Format("15:04:05"))
	if isExternal {
		second = fmt.Sprintf("copied for %s (external lane); paste pending.", lane)
	}
	return fmt.Sprintf("answered from the cockpit: %s\n%s", text, second)
}
