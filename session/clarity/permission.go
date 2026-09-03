// Package clarity: the permission-prompt state word's own detector
// (ANSWER-AND-BANK-SPEC.md item 7 / research item 7 "A fifth state word for
// a lane sitting on a permission prompt, sampled from its tmux pane").
package clarity

import "strings"

// PermissionPromptShapeExample is a REAL capture of Claude Code's own
// Bash-tool permission prompt (v2.1.259, scratch instance driven to it with
// a harmless `echo ... > marker.txt` command, isolated tmux socket, 3 Sep
// 2026 - the leg's own PROOF, never invented):
//
//	 Bash command
//	 Tip: auto mode handles these prompts for you — choose "switch to auto mode" below
//
//	   echo hello-permission-probe > marker.txt
//	   Write probe marker file
//
//	 Do you want to proceed?
//	 ❯ 1. Yes
//	   2. Yes, and don't ask again for: echo hello-permission-probe *
//	   3. Yes, and switch to auto mode · auto mode handles these prompts for you
//	   4. No
//
//	 Esc to cancel · Tab to amend
//
// The command/description body above "Do you want to proceed?" varies by
// tool (Bash/Edit/Write each phrase it differently) and the option COUNT
// varies too (a simple yes/no prompt carries fewer than four) - only the
// two lines IsPermissionPrompt anchors on are stable across every tool this
// leg observed, both used here purely as documentation of the shape.
const permissionPromptProceedLine = "Do you want to proceed?"
const permissionPromptEscTrailerPrefix = "Esc to cancel"

// sessionFeedbackSurveyWords are the four numbered option words the
// harness's own end-of-session feedback survey carries together, in order,
// on ONE line (board #315, owner addition 3 Sep 17:5x - "1 Bad 2 Fine 3
// Good 0 Dismiss"; also seen with a "How was this session?" or "session
// feedback" heading line above it). The anchor is the option line itself,
// never the heading - the heading text is not confirmed stable across
// harness versions, but these four words together are the survey's own
// distinguishing shape. No REAL pane capture of this survey was available
// to this leg (unlike PermissionPromptShapeExample's own real Bash-prompt
// capture above) - this word list is built from the owner's description
// alone and is UNTESTED against a live harness survey; re-verify against a
// real capture before relying on it in production.
var sessionFeedbackSurveyWords = []string{"Bad", "Fine", "Good", "Dismiss"}

// isSessionFeedbackSurveyLine reports whether line carries the four
// numbered option words together, in order - a heading line like "How was
// this session?" or "session feedback" carries none of them, so this never
// anchors on the heading.
func isSessionFeedbackSurveyLine(line string) bool {
	pos := 0
	for _, word := range sessionFeedbackSurveyWords {
		idx := strings.Index(line[pos:], word)
		if idx < 0 {
			return false
		}
		pos += idx + len(word)
	}
	return true
}

// IsPermissionPrompt reports whether pane's own last non-blank lines match
// EITHER of the two prompt shapes that block the input line and so read as
// "needs a key" (board #315 item 6 - the session-feedback survey is a
// second such shape, sitting alongside the original permission prompt):
//
//   - the harness's own permission prompt: a "Do you want to proceed?" line
//     followed (within a few lines, allowing for a varying number of
//     numbered options) by an "Esc to cancel ..." trailer. Anchored on
//     those two lines only - never the command/description text above
//     them, which is a different sentence for every tool - and never the
//     number of options, which varies by prompt.
//   - the harness's own session-feedback survey: a line carrying all four
//     numbered option words (isSessionFeedbackSurveyLine).
//
// A capture with neither anchor (ordinary conversation, a closed turn, a
// prompt scrolled out of view) reports false. A pure function over pane -
// no state, no side effect.
func IsPermissionPrompt(pane string) bool {
	lines := nonBlankTail(pane, 12)
	sawProceed := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case isSessionFeedbackSurveyLine(t):
			return true
		case strings.Contains(t, permissionPromptProceedLine):
			sawProceed = true
		case sawProceed && strings.HasPrefix(t, permissionPromptEscTrailerPrefix):
			return true
		}
	}
	return false
}

// nonBlankTail returns up to n of pane's own trailing non-blank lines, in
// their original (oldest-first) order - a raw tmux capture-pane output is
// usually padded with blank lines from the pane's unused height (LastPane-
// Line's own doc comment, msg.go), and the prompt's own shape only ever
// appears in the pane's genuinely-written tail.
func nonBlankTail(pane string, n int) []string {
	all := strings.Split(pane, "\n")
	var nonBlank []string
	for _, l := range all {
		if strings.TrimSpace(l) != "" {
			nonBlank = append(nonBlank, l)
		}
	}
	if len(nonBlank) > n {
		nonBlank = nonBlank[len(nonBlank)-n:]
	}
	return nonBlank
}
