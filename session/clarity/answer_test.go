package clarity

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// -- test 1: AnswerText -----------------------------------------------------

func TestAnswerText_StripsTrailingRecommendedDot(t *testing.T) {
	got := AnswerText("(a) Make both edits yourself, two minutes. Recommended.")
	require.Equal(t, "(a) Make both edits yourself, two minutes.", got)
}

func TestAnswerText_NoTrailingRecommended_LeftAlone(t *testing.T) {
	got := AnswerText("(b) Say \"apply it\" in a fresh session that is allowed to.")
	require.Equal(t, "(b) Say \"apply it\" in a fresh session that is allowed to.", got)
}

func TestAnswerText_RecommendedMidSentence_NotStripped(t *testing.T) {
	// Only a TRAILING " Recommended." is stripped - the word appearing
	// elsewhere in the text is content, not the card's own inline pick.
	got := AnswerText("Recommended reading before you start.")
	require.Equal(t, "Recommended reading before you start.", got)
}

// -- test 2: ChosenOption -----------------------------------------------------

func TestChosenOption_RecommendedMarked_IsChosen(t *testing.T) {
	opts := []BoardOption{
		{Text: "(a) first", Recommended: false},
		{Text: "(b) second, Recommended.", Recommended: true},
	}
	got, idx, ok := ChosenOption(opts)
	require.True(t, ok)
	require.Equal(t, 1, idx)
	require.Equal(t, opts[1], got)
}

func TestChosenOption_NoneMarked_FirstIsChosen(t *testing.T) {
	opts := []BoardOption{
		{Text: "(a) first"},
		{Text: "(b) second"},
	}
	got, idx, ok := ChosenOption(opts)
	require.True(t, ok)
	require.Equal(t, 0, idx)
	require.Equal(t, opts[0], got)
}

func TestChosenOption_NoOptions_NotOK(t *testing.T) {
	_, idx, ok := ChosenOption(nil)
	require.False(t, ok)
	require.Equal(t, -1, idx)
}

// -- board comment body -------------------------------------------------

func TestAnswerCommentBody_Tracked_SentIntoLane(t *testing.T) {
	at := time.Date(2026, 9, 3, 11, 42, 7, 0, time.Local)
	got := AnswerCommentBody("(a) Make both edits yourself, two minutes.", "ways-of-working", false, at)
	require.Equal(t,
		"answered from the cockpit: (a) Make both edits yourself, two minutes.\nsent into ways-of-working at 11:42:07.",
		got)
}

func TestAnswerCommentBody_External_CopiedFor(t *testing.T) {
	at := time.Date(2026, 9, 3, 11, 42, 7, 0, time.Local)
	got := AnswerCommentBody("copy this", "andy-e-bid", true, at)
	require.Equal(t,
		"answered from the cockpit: copy this\ncopied for andy-e-bid (external lane); paste pending.",
		got)
}
