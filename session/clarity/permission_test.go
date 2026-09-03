package clarity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// realBashPermissionPrompt is the REAL pane capture PermissionPromptShape-
// Example documents - verbatim, from the leg's own scratch-instance PROOF
// (isolated tmux socket, `echo ... > marker.txt`, Claude Code v2.1.259, 3
// Sep 2026). Padded with the trailing blank lines a real capture-pane
// output carries (the pane's unused height), matching msg.go's own
// LastPaneLine assumption about real captures.
const realBashPermissionPrompt = `❯ Run the bash command: echo hello-permission-probe > marker.txt

⏺ Running the probe command now.

  Writing probe marker file
  ⎿  $ echo hello-permission-probe > marker.txt

────────────────────────────────────────────────────────
 Bash command
 Tip: auto mode handles these prompts for you — choose "switch to auto mode" below

   echo hello-permission-probe > marker.txt
   Write probe marker file

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for: echo hello-permission-probe *
   3. Yes, and switch to auto mode · auto mode handles these prompts for you
   4. No

 Esc to cancel · Tab to amend


`

func TestIsPermissionPrompt_RealCapture_MatchesTrue(t *testing.T) {
	require.True(t, IsPermissionPrompt(realBashPermissionPrompt))
}

// realSimpleYesNoPrompt is a plausible fewer-option prompt (a tool that
// offers only Yes/No, no "don't ask again"/"auto mode" lines) - the anchor
// must not depend on the option count.
const realSimpleYesNoPrompt = `⏺ Reading the file now.

 Do you want to proceed?
 ❯ 1. Yes
   2. No

 Esc to cancel · Tab to amend
`

func TestIsPermissionPrompt_FewerOptions_StillMatches(t *testing.T) {
	require.True(t, IsPermissionPrompt(realSimpleYesNoPrompt))
}

func TestIsPermissionPrompt_OrdinaryConversation_False(t *testing.T) {
	pane := "CLAUDE   11:38:12\n  Slice 16 is in. The message box now wraps.\n\n"
	require.False(t, IsPermissionPrompt(pane))
}

func TestIsPermissionPrompt_ProceedLineAloneWithoutEscTrailer_False(t *testing.T) {
	pane := "Do you want to proceed?\nsome unrelated trailing text\n"
	require.False(t, IsPermissionPrompt(pane))
}

func TestIsPermissionPrompt_EscTrailerAloneWithoutProceedLine_False(t *testing.T) {
	pane := "some ordinary help text\nEsc to cancel · Tab to amend\n"
	require.False(t, IsPermissionPrompt(pane))
}

func TestIsPermissionPrompt_EmptyPane_False(t *testing.T) {
	require.False(t, IsPermissionPrompt(""))
}

// sessionFeedbackSurveyPane is board #315's own survey shape (owner
// addition, 3 Sep 17:5x): a heading line above the four numbered options -
// no REAL pane capture of this was available to this leg, built from the
// owner's own description (see permission.go's sessionFeedbackSurveyWords
// doc comment); UNTESTED against a live harness survey.
const sessionFeedbackSurveyPane = `⏺ Session complete.

 How was this session?

 1. Bad   2. Fine   3. Good   0. Dismiss

`

func TestIsPermissionPrompt_SessionFeedbackSurvey_MatchesTrue(t *testing.T) {
	require.True(t, IsPermissionPrompt(sessionFeedbackSurveyPane))
}

// TestIsPermissionPrompt_SessionFeedbackSurvey_HeadingAloneWithoutOptionsLine_False
// pins the anchor to the FOUR-OPTION line, never the heading text above it
// (the brief's own "match the four-option line, not the heading") - a
// heading with no options line at all must not match.
func TestIsPermissionPrompt_SessionFeedbackSurvey_HeadingAloneWithoutOptionsLine_False(t *testing.T) {
	pane := "⏺ Session complete.\n\n How was this session?\n\n"
	require.False(t, IsPermissionPrompt(pane))
}

// TestIsPermissionPrompt_SessionFeedbackSurvey_AlternateHeading_MatchesTrue
// proves the detector anchors on the option words regardless of which
// heading text (or none) sits above them - "session feedback" is the
// brief's own second-named heading variant.
func TestIsPermissionPrompt_SessionFeedbackSurvey_AlternateHeading_MatchesTrue(t *testing.T) {
	pane := "⏺ session feedback\n\n 1. Bad   2. Fine   3. Good   0. Dismiss\n\n"
	require.True(t, IsPermissionPrompt(pane))
}
