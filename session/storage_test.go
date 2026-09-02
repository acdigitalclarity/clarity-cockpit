package session

import (
	"claude-squad/config"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// pausedInstanceData builds a NoWorktree, already-Paused InstanceData
// fixture - the same shape a clarity-attach instance takes (session/
// clarity/attach.go), and the cheapest one FromInstanceData can
// reconstruct without touching a real tmux session (see instance.go's
// FromInstanceData: a Paused instance skips Start entirely).
func pausedInstanceData(title, path string) InstanceData {
	now := time.Now()
	return InstanceData{
		Title:      title,
		Path:       path,
		Status:     Paused,
		Program:    "claude",
		CreatedAt:  now,
		UpdatedAt:  now,
		NoWorktree: true,
	}
}

func instancePaths(instances []*Instance) []string {
	out := make([]string, len(instances))
	for i, inst := range instances {
		out[i] = inst.Path
	}
	return out
}

// TestSaveInstances_PreservesInstanceWrittenByAnotherProcess reproduces the
// 2 Sep 20:22 instance-store clobber: process A (the cockpit) loads the
// store, then process B (main.go's clarity-attach, run from a fresh
// process by the clarity wrapper) loads the CURRENT disk state, appends
// its own instance and saves. Process A's in-memory list never learned
// about process B's instance - its next save must not drop it.
func TestSaveInstances_PreservesInstanceWrittenByAnotherProcess(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	storageA, err := NewStorage(config.LoadState())
	require.NoError(t, err)
	instA, err := FromInstanceData(pausedInstanceData("lane-a", "/path/a"))
	require.NoError(t, err)
	require.NoError(t, storageA.SaveInstances([]*Instance{instA}))

	// Process B: a brand-new process (fresh config.State), loads the
	// current store, appends its own instance, saves - exactly what
	// clarity-attach does.
	storageB, err := NewStorage(config.LoadState())
	require.NoError(t, err)
	loadedByB, err := storageB.LoadInstances()
	require.NoError(t, err)
	instB, err := FromInstanceData(pausedInstanceData("lane-b", "/path/b"))
	require.NoError(t, err)
	require.NoError(t, storageB.SaveInstances(append(loadedByB, instB)))

	// Process A's in-memory list is still just instA - stale relative to
	// disk. Before the fix this overwrites the store back down to one
	// instance; the fix must preserve lane-b.
	require.NoError(t, storageA.SaveInstances([]*Instance{instA}))

	onDisk, err := storageA.LoadInstances()
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"/path/a", "/path/b"}, instancePaths(onDisk),
		"process A's stale save must not clobber the instance process B wrote from outside")
}

// TestSaveInstances_HonoursDeletionOfKnownInstance guards the merge fix's
// other half: an instance this process itself loaded/saved and then
// deliberately drops from its in-memory list (a D-kill) must actually be
// removed from disk, not resurrected as though it were an unknown external
// write.
func TestSaveInstances_HonoursDeletionOfKnownInstance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	storage, err := NewStorage(config.LoadState())
	require.NoError(t, err)
	instA, err := FromInstanceData(pausedInstanceData("lane-a", "/path/a"))
	require.NoError(t, err)
	instB, err := FromInstanceData(pausedInstanceData("lane-b", "/path/b"))
	require.NoError(t, err)
	require.NoError(t, storage.SaveInstances([]*Instance{instA, instB}))

	// This process now knows both a and b - dropping instA from the list
	// it saves must delete it, not merge it back in.
	require.NoError(t, storage.SaveInstances([]*Instance{instB}))

	onDisk, err := storage.LoadInstances()
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"/path/b"}, instancePaths(onDisk))
}

// TestUntrackedInstances_AdoptsOnDiskRecordNotInKnownSet is the feed tick's
// adoption step (defect 1's read-side half): a lane another process wrote
// directly to the store must be reconstructable by path, without this
// process ever having reloaded its whole list.
func TestUntrackedInstances_AdoptsOnDiskRecordNotInKnownSet(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	storageA, err := NewStorage(config.LoadState())
	require.NoError(t, err)
	instA, err := FromInstanceData(pausedInstanceData("lane-a", "/path/a"))
	require.NoError(t, err)
	require.NoError(t, storageA.SaveInstances([]*Instance{instA}))

	storageB, err := NewStorage(config.LoadState())
	require.NoError(t, err)
	loadedByB, err := storageB.LoadInstances()
	require.NoError(t, err)
	instB, err := FromInstanceData(pausedInstanceData("lane-b", "/path/b"))
	require.NoError(t, err)
	require.NoError(t, storageB.SaveInstances(append(loadedByB, instB)))

	adopted, err := storageA.UntrackedInstances(map[string]bool{"/path/a": true})
	require.NoError(t, err)
	require.Len(t, adopted, 1)
	require.Equal(t, "/path/b", adopted[0].Path)

	// A second call with the same known set must not re-adopt lane-b once
	// the caller has folded it into its own list (known would then include
	// it) - simulated here by passing it as known this time.
	adoptedAgain, err := storageA.UntrackedInstances(map[string]bool{"/path/a": true, "/path/b": true})
	require.NoError(t, err)
	require.Empty(t, adoptedAgain)
}
