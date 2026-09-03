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

// IsPermissionPrompt reports whether pane's own last non-blank lines match
// Claude Code's permission-prompt shape: a "Do you want to proceed?" line
// followed (within a few lines, allowing for a varying number of numbered
// options) by an "Esc to cancel ..." trailer. Anchored on those two lines
// only - never the command/description text above them, which is a
// different sentence for every tool - and never the number of options,
// which varies by prompt. A capture with neither anchor (ordinary
// conversation, a closed turn, a prompt scrolled out of view) reports
// false.
func IsPermissionPrompt(pane string) bool {
	lines := nonBlankTail(pane, 12)
	sawProceed := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		switch {
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
