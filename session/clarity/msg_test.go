package clarity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExternalMsgUnconstructed_NamesLaneAndAttachRoute(t *testing.T) {
	got := ExternalMsgUnconstructed("ways-of-working")
	require.Equal(t,
		"msg: UNCONSTRUCTED - ways-of-working runs outside tmux; open it with clarity attach ways-of-working",
		got)
}

func TestLastPaneLine_SkipsTrailingBlankLines(t *testing.T) {
	pane := "some old output\necho hello from the cockpit\nhello from the cockpit\n\n\n\n"
	require.Equal(t, "hello from the cockpit", LastPaneLine(pane))
}

func TestLastPaneLine_SingleLine(t *testing.T) {
	require.Equal(t, "only line", LastPaneLine("only line"))
}

func TestLastPaneLine_AllBlank(t *testing.T) {
	require.Equal(t, "", LastPaneLine("\n\n   \n"))
}

func TestLastPaneLine_TrimsTrailingWhitespaceOnTheLastContentLine(t *testing.T) {
	require.Equal(t, "hello", LastPaneLine("hello   \n\n"))
}

func TestLastPaneLine_Empty(t *testing.T) {
	require.Equal(t, "", LastPaneLine(""))
}
