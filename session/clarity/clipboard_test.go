package clarity

import (
	"claude-squad/cmd/cmd_test"
	"io"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCopyToClipboard_RunsPbcopyWithTextOnStdin(t *testing.T) {
	var gotArgs []string
	var gotStdin string
	fake := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			gotArgs = cmd.Args
			b, err := io.ReadAll(cmd.Stdin)
			require.NoError(t, err)
			gotStdin = string(b)
			return nil
		},
	}

	require.NoError(t, CopyToClipboard(fake, "scratchfix hello"))
	require.Equal(t, []string{"pbcopy"}, gotArgs)
	require.Equal(t, "scratchfix hello", gotStdin)
}

func TestCopyToClipboard_PropagatesRunError(t *testing.T) {
	fake := cmd_test.MockCmdExec{
		RunFunc: func(cmd *exec.Cmd) error {
			return exec.ErrNotFound
		},
	}
	require.ErrorIs(t, CopyToClipboard(fake, "text"), exec.ErrNotFound)
}
