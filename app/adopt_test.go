package app

import (
	"claude-squad/config"
	"claude-squad/session"
	"claude-squad/ui"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	"github.com/stretchr/testify/require"
)

// externalWriteInstanceData is the same NoWorktree, Paused shape a
// clarity-attach instance takes (session/clarity/attach.go) - the cheapest
// fixture FromInstanceData can reconstruct without a real tmux session.
func externalWriteInstanceData(title, path string) session.InstanceData {
	now := time.Now()
	return session.InstanceData{
		Title:      title,
		Path:       path,
		Status:     session.Paused,
		Program:    "claude",
		CreatedAt:  now,
		UpdatedAt:  now,
		NoWorktree: true,
	}
}

// TestAdoptUntrackedInstances_PicksUpStoreWriteFromAnotherProcess is defect
// 1's read-side half wired into app.go: a lane written into the store by
// another process (main.go's clarity-attach, ~line 215, run by the clarity
// wrapper while the cockpit is already open) must appear in m.list after
// one adoptUntrackedInstances call - the feed tick's own hook point - with
// no restart of the running cockpit.
func TestAdoptUntrackedInstances_PicksUpStoreWriteFromAnotherProcess(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// This process's own storage, with nothing loaded into m.list yet.
	storage, err := session.NewStorage(config.LoadState())
	require.NoError(t, err)

	s := spinner.New()
	h := &home{
		storage: storage,
		list:    ui.NewList(&s, false),
	}
	require.Equal(t, 0, h.list.NumInstances())

	// A second process (clarity-attach) loads the (empty) store, appends
	// its own instance, and saves - from a fresh config.State, sharing the
	// same on-disk file via HOME.
	otherStorage, err := session.NewStorage(config.LoadState())
	require.NoError(t, err)
	otherInstance, err := session.FromInstanceData(externalWriteInstanceData("andy.e-bid", "/path/andy-e-bid"))
	require.NoError(t, err)
	require.NoError(t, otherStorage.SaveInstances([]*session.Instance{otherInstance}))

	h.adoptUntrackedInstances()

	require.Equal(t, 1, h.list.NumInstances(), "the externally-written lane must be adopted into the list")
	require.Equal(t, "/path/andy-e-bid", h.list.GetInstances()[0].Path)

	// A second call must not double-adopt the same lane now that it is
	// known to this process's own list.
	h.adoptUntrackedInstances()
	require.Equal(t, 1, h.list.NumInstances(), "an already-adopted lane must not be adopted twice")
}
