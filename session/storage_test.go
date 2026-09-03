package session

import (
	"claude-squad/config"
	"encoding/json"
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

// pausedInstanceDataWithAccount is pausedInstanceData plus an Account, for
// the seat-identity tests below (BRIEF-FRONTDOOR-4.md item 5b-d).
func pausedInstanceDataWithAccount(title, path, account string) InstanceData {
	data := pausedInstanceData(title, path)
	data.Account = account
	return data
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

// preSlice4InstanceData mirrors InstanceData's exact shape before slice 4
// (FRONTDOOR-SPEC.md "The store") added Account/Modality - the golden
// pre-existing state.json fixture TestInstanceData_RoundTrip_EmptyAccountModality_ByteIdentical
// below round-trips through the widened struct.
type preSlice4InstanceData struct {
	Title     string    `json:"title"`
	Path      string    `json:"path"`
	Branch    string    `json:"branch"`
	Status    Status    `json:"status"`
	Height    int       `json:"height"`
	Width     int       `json:"width"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	AutoYes   bool      `json:"auto_yes"`

	Program    string          `json:"program"`
	Worktree   GitWorktreeData `json:"worktree"`
	DiffStats  DiffStatsData   `json:"diff_stats"`
	NoWorktree bool            `json:"no_worktree,omitempty"`
}

// TestInstanceData_RoundTrip_EmptyAccountModality_ByteIdentical is test (a)
// (BRIEF-FRONTDOOR-4.md item 5): a state.json written before slice 4 has no
// "account"/"modality" keys. Unmarshalling it into the widened InstanceData
// and marshalling it straight back out must reproduce the exact same
// bytes - omitempty on both new fields is what keeps a pre-existing store
// readable byte-for-byte on a round trip.
func TestInstanceData_RoundTrip_EmptyAccountModality_ByteIdentical(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	old := preSlice4InstanceData{
		Title:     "lane-a",
		Path:      "/path/a",
		Status:    Ready,
		CreatedAt: now,
		UpdatedAt: now,
		Program:   "claude",
	}
	oldJSON, err := json.Marshal(old)
	require.NoError(t, err)

	var data InstanceData
	require.NoError(t, json.Unmarshal(oldJSON, &data))
	require.Empty(t, data.Account, "a record from before slice 4 has no account")
	require.Empty(t, data.Modality, "a record from before slice 4 has no modality")

	roundTripped, err := json.Marshal(data)
	require.NoError(t, err)
	require.Equal(t, string(oldJSON), string(roundTripped),
		"omitempty on account/modality must keep a pre-slice-4 state.json byte-identical on round trip")
}

// TestSaveInstances_TwoSeatsThreeInstances_SurviveMergeOnSave is test (b)
// (BRIEF-FRONTDOOR-4.md item 5), the slice 2c merge-on-save shape (see
// TestSaveInstances_PreservesInstanceWrittenByAnotherProcess above)
// extended across seats: process A holds X@main and Y@team-b, process B
// holds X@team-b - the same Title as A's X, a different Account. Each
// process saves from its own stale-relative-to-disk in-memory list; all
// three rows must survive the merge, each keeping its own account.
func TestSaveInstances_TwoSeatsThreeInstances_SurviveMergeOnSave(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	storageA, err := NewStorage(config.LoadState())
	require.NoError(t, err)
	xMain, err := FromInstanceData(pausedInstanceDataWithAccount("X", "/path/x-main", "main"))
	require.NoError(t, err)
	yTeamB, err := FromInstanceData(pausedInstanceDataWithAccount("Y", "/path/y-team-b", "team-b"))
	require.NoError(t, err)
	require.NoError(t, storageA.SaveInstances([]*Instance{xMain, yTeamB}))

	// Process B: a fresh process, loads current disk state, appends its
	// own X@team-b (same Title as A's X, different Account) and saves -
	// exactly what clarity-attach does from outside the running cockpit.
	storageB, err := NewStorage(config.LoadState())
	require.NoError(t, err)
	loadedByB, err := storageB.LoadInstances()
	require.NoError(t, err)
	xTeamB, err := FromInstanceData(pausedInstanceDataWithAccount("X", "/path/x-team-b", "team-b"))
	require.NoError(t, err)
	require.NoError(t, storageB.SaveInstances(append(loadedByB, xTeamB)))

	// Process A's in-memory list is still just its own two rows - stale
	// relative to disk. Its next save must not drop process B's row.
	require.NoError(t, storageA.SaveInstances([]*Instance{xMain, yTeamB}))

	onDisk, err := storageA.LoadInstances()
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"/path/x-main", "/path/y-team-b", "/path/x-team-b"}, instancePaths(onDisk),
		"all three rows across two seats must survive both processes' saves")

	byPath := make(map[string]*Instance, len(onDisk))
	for _, inst := range onDisk {
		byPath[inst.Path] = inst
	}
	require.Equal(t, "main", byPath["/path/x-main"].Account())
	require.Equal(t, "team-b", byPath["/path/y-team-b"].Account())
	require.Equal(t, "team-b", byPath["/path/x-team-b"].Account())
	require.Equal(t, "X", byPath["/path/x-main"].Title)
	require.Equal(t, "X", byPath["/path/x-team-b"].Title)
}

// TestDeleteInstanceByAccountTitle_OnOneSeat_LeavesOtherSeatsRow is test
// (c): deleting X@main must leave X@team-b untouched - the identity fix's
// whole point (FRONTDOOR-SPEC.md "The store").
func TestDeleteInstanceByAccountTitle_OnOneSeat_LeavesOtherSeatsRow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	storage, err := NewStorage(config.LoadState())
	require.NoError(t, err)
	xMain, err := FromInstanceData(pausedInstanceDataWithAccount("X", "/path/x-main", "main"))
	require.NoError(t, err)
	xTeamB, err := FromInstanceData(pausedInstanceDataWithAccount("X", "/path/x-team-b", "team-b"))
	require.NoError(t, err)
	require.NoError(t, storage.SaveInstances([]*Instance{xMain, xTeamB}))

	require.NoError(t, storage.DeleteInstanceByAccountTitle("main", "X"))

	onDisk, err := storage.LoadInstances()
	require.NoError(t, err)
	require.Len(t, onDisk, 1)
	require.Equal(t, "/path/x-team-b", onDisk[0].Path)
	require.Equal(t, "team-b", onDisk[0].Account())
}

// TestDeleteInstance_LegacyTitleOnly_TargetsEmptyAccountRow documents the
// file-fence collision this leg reports: app/app.go (slice 24, running
// concurrently, fenced) still calls the single-arg DeleteInstance(title)
// and cannot be edited here to pass the account it already holds on the
// selected instance. The legacy entry point is kept, but now targets the
// empty-Account row specifically (today's real lanes all have Account
// "") rather than any row matching the title regardless of account, so it
// can no longer clobber a differently-seated same-titled row.
func TestDeleteInstance_LegacyTitleOnly_TargetsEmptyAccountRow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	storage, err := NewStorage(config.LoadState())
	require.NoError(t, err)
	xDefault, err := FromInstanceData(pausedInstanceDataWithAccount("X", "/path/x-default", ""))
	require.NoError(t, err)
	xTeamB, err := FromInstanceData(pausedInstanceDataWithAccount("X", "/path/x-team-b", "team-b"))
	require.NoError(t, err)
	require.NoError(t, storage.SaveInstances([]*Instance{xDefault, xTeamB}))

	require.NoError(t, storage.DeleteInstance("X"))

	onDisk, err := storage.LoadInstances()
	require.NoError(t, err)
	require.Len(t, onDisk, 1)
	require.Equal(t, "/path/x-team-b", onDisk[0].Path, "the legacy title-only delete must not touch a different seat's row")
}

// TestUpdateInstance_OnOneSeat_DoesNotTouchOtherSeatsRow is test (d):
// updating X@team-b must match by Account plus Title, not Title alone, so
// X@main's row is left exactly as it was.
func TestUpdateInstance_OnOneSeat_DoesNotTouchOtherSeatsRow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	storage, err := NewStorage(config.LoadState())
	require.NoError(t, err)
	xMain, err := FromInstanceData(pausedInstanceDataWithAccount("X", "/path/x-main", "main"))
	require.NoError(t, err)
	xTeamB, err := FromInstanceData(pausedInstanceDataWithAccount("X", "/path/x-team-b", "team-b"))
	require.NoError(t, err)
	require.NoError(t, storage.SaveInstances([]*Instance{xMain, xTeamB}))

	updated, err := FromInstanceData(pausedInstanceDataWithAccount("X", "/path/x-team-b", "team-b"))
	require.NoError(t, err)
	updated.SetModality("bid")
	require.NoError(t, storage.UpdateInstance(updated))

	onDisk, err := storage.LoadInstances()
	require.NoError(t, err)
	require.Len(t, onDisk, 2)
	byPath := make(map[string]*Instance, len(onDisk))
	for _, inst := range onDisk {
		byPath[inst.Path] = inst
	}
	require.Equal(t, "bid", byPath["/path/x-team-b"].Modality(), "the team-b row must be updated")
	require.Empty(t, byPath["/path/x-main"].Modality(), "the main row must be untouched")
}
