// Package session: cockpit-pane slice 17c's own boot-liveness proof (item
// 1/3). Slice 17b's own Alive() formula (session/instance.go, since
// replaced) ANDed in !i.Paused() unconditionally - correct for the common
// case (a Paused instance's session really is gone) but wrong the moment a
// stored Status of Paused is stale: the 3 Sep 18:47:57 `tmux kill-server`
// incident left several lanes' Status pinned at Paused by Start()'s own
// ErrSessionNotFound branch, and once those sessions came back (or, on the
// home smoke that found this, never actually died at all) the stored word
// never caught up. A freshly booted process reconstructs every stored
// instance via FromInstanceData, which for a Paused record never calls
// Restore at all (see FromInstanceData's own Paused() branch) - so Alive()
// had to be right about such an instance from construction alone, with no
// further Start/Restore call in between. That is exactly what these tests
// exercise, against a REAL tmux session on an isolated rig socket (never
// the default - TMUX SOCKET RULE).
package session

import (
	"claude-squad/session/tmux"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// rigSocket returns a unique, non-default tmux socket name for this test
// process, points every tmux command this package's own machinery issues
// at it (tmux.tmuxSocketEnvVar, via CLARITY_TMUX_SOCKET) for the duration
// of the test, and tears the rig server down on cleanup - kill-server
// scoped to -L name only, never the bare/default form.
func rigSocket(t *testing.T) string {
	t.Helper()
	socket := fmt.Sprintf("p17c-boot-%d-%s", os.Getpid(), sanitizeForSocket(t.Name()))
	t.Setenv("CLARITY_TMUX_SOCKET", socket)
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", socket, "kill-server").Run()
	})
	return socket
}

// sanitizeForSocket strips characters tmux's own socket-name handling (and
// this test's own readability) would rather not see in a filename.
func sanitizeForSocket(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		if r == '/' || r == ' ' {
			out = append(out, '-')
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

// createRigSession creates a detached tmux session named for title (through
// the exact same sanitisation FromInstanceData's own tmux session will look
// up under) directly on the rig socket - the test's own fixture setup, not
// the code under test.
func createRigSession(t *testing.T, socket, title string) {
	t.Helper()
	name := tmux.SanitizeName(title)
	require.NoError(t,
		exec.Command("tmux", "-L", socket, "new-session", "-d", "-s", name, "sleep 600").Run(),
		"rig setup: create the session this instance's own record will point at")
}

// TestFromInstanceData_StoredPausedButSessionAlive_ReadsAlive is item 1's
// own repro, as root-cause-traced against this codebase rather than
// literally as briefed: a Status: Running record whose session exists
// already read Alive() true on main (FromInstanceData's non-Paused branch
// calls Start(false), which Restores the real session and leaves Paused()
// false) - not a repro at all. The scenario that actually reproduces the
// home smoke's "every tracked row reading paused, including two whose
// sessions are alive" is a STORED Paused record whose session survived (or
// came back) - FromInstanceData's Paused() branch never calls Restore, so
// this is the one shape where Alive() had to be right from construction
// alone with no further Start/Restore in between.
func TestFromInstanceData_StoredPausedButSessionAlive_ReadsAlive(t *testing.T) {
	socket := rigSocket(t)
	const title = "boot-alive-lane"
	createRigSession(t, socket, title)

	data := InstanceData{
		Title:      title,
		Path:       t.TempDir(),
		Status:     Paused,
		Program:    "sleep 600",
		Height:     24,
		Width:      80,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		NoWorktree: true,
	}

	instance, err := FromInstanceData(data)
	require.NoError(t, err)

	require.True(t, instance.Alive(),
		"a genuinely live tmux session must read Alive, even when the stored Status says Paused")
}

// TestFromInstanceData_StatusRunning_SessionExists_ReadsAliveAtBoot is the
// brief's own literal item-1 scenario, kept as its own test (rather than
// folded into the repro above) so the Running/session-exists shape stays
// covered even though it was never the failing case on main: a store-
// loaded instance whose session actually exists must read Alive() true
// immediately once FromInstanceData returns, with no further Start/Restore
// call from the test itself.
func TestFromInstanceData_StatusRunning_SessionExists_ReadsAliveAtBoot(t *testing.T) {
	socket := rigSocket(t)
	const title = "boot-running-lane"
	createRigSession(t, socket, title)

	data := InstanceData{
		Title:      title,
		Path:       t.TempDir(),
		Status:     Running,
		Program:    "sleep 600",
		Height:     24,
		Width:      80,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		NoWorktree: true,
	}

	instance, err := FromInstanceData(data)
	require.NoError(t, err)

	require.True(t, instance.Alive(), "a Running record whose session exists reads Alive at boot")
}

// TestFromInstanceData_SessionMissing_ReadsNotAliveAndPaused is item 3(b):
// a store-loaded instance whose session does NOT exist reads not alive,
// and Paused() (the field laneLivenessState reads to choose the row's own
// "paused" word over clarity.StateStopped) is true - the ordinary case
// Start()'s own ErrSessionNotFound branch has always handled, proven here
// unchanged by this slice's fix.
func TestFromInstanceData_SessionMissing_ReadsNotAliveAndPaused(t *testing.T) {
	rigSocket(t) // isolated, empty rig - no session is ever created on it

	data := InstanceData{
		Title:      "boot-dead-lane",
		Path:       t.TempDir(),
		Status:     Running,
		Program:    "sleep 600",
		Height:     24,
		Width:      80,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		NoWorktree: true,
	}

	instance, err := FromInstanceData(data)
	require.NoError(t, err)

	require.False(t, instance.Alive(), "no session exists on the rig socket: not alive")
	require.True(t, instance.Paused(),
		"Start's own ErrSessionNotFound branch parks a missing session as Paused, so the row reads the paused word")
}
