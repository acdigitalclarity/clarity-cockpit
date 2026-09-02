package session

import (
	"claude-squad/config"
	"claude-squad/log"
	"encoding/json"
	"fmt"
	"time"
)

// InstanceData represents the serializable data of an Instance
type InstanceData struct {
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

// GitWorktreeData represents the serializable data of a GitWorktree
type GitWorktreeData struct {
	RepoPath         string `json:"repo_path"`
	WorktreePath     string `json:"worktree_path"`
	SessionName      string `json:"session_name"`
	BranchName       string `json:"branch_name"`
	BaseCommitSHA    string `json:"base_commit_sha"`
	IsExistingBranch bool   `json:"is_existing_branch"`
}

// DiffStatsData represents the serializable data of a DiffStats
type DiffStatsData struct {
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Content string `json:"content"`
}

// Storage handles saving and loading instances using the state interface.
// known tracks the instance paths this process has itself loaded or saved
// - see SaveInstances' doc comment (defect 1, the 2 Sep 20:22 instance-
// store clobber): it is the line between "this process deleted that
// instance on purpose" and "this process never knew that instance
// existed".
type Storage struct {
	state config.InstanceStorage
	known map[string]bool
}

// NewStorage creates a new storage instance
func NewStorage(state config.InstanceStorage) (*Storage, error) {
	return &Storage{
		state: state,
		known: make(map[string]bool),
	}, nil
}

// SaveInstances saves the list of instances to disk. It never simply
// overwrites the store with this process's own in-memory list: it re-reads
// the store immediately before writing (config.State.GetInstances() always
// reads fresh from disk, see its own doc comment) and merges by instance
// PATH. An on-disk record whose path this process has never seen (not in
// `known`) was written by another process after this process's own list
// was last refreshed - e.g. main.go's clarity-attach command (~line 215),
// which loads, appends and saves from a brand-new process while the
// cockpit keeps running - and survives even though it is absent from the
// in-memory list handed to this call. An on-disk record this process HAS
// previously loaded or saved and no longer lists here is a genuine
// deletion (the `D` kill key) and is dropped, exactly as before.
//
// This is the root-cause fix for the 2 Sep 20:22 defect: the cockpit
// process opened before 19:13 still held its stale in-memory list (just
// itself) when the owner quit it; its unconditional overwrite-on-quit
// dropped the lane clarity-attach had registered into the store at 19:13,
// even though that lane's tmux session was still alive.
func (s *Storage) SaveInstances(instances []*Instance) error {
	// Convert instances to InstanceData
	data := make([]InstanceData, 0, len(instances))
	byPath := make(map[string]bool, len(instances))
	for _, instance := range instances {
		if instance.Started() {
			d := instance.ToInstanceData()
			data = append(data, d)
			byPath[d.Path] = true
		}
	}

	if onDisk, err := s.loadRawInstanceData(); err != nil {
		log.WarningLog.Printf("save instances: could not reload on-disk store to merge, writing in-memory list only: %v", err)
	} else {
		for _, d := range onDisk {
			if byPath[d.Path] || s.known[d.Path] {
				continue
			}
			data = append(data, d)
		}
	}

	for _, d := range data {
		s.known[d.Path] = true
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal instances: %w", err)
	}

	return s.state.SaveInstances(jsonData)
}

// loadRawInstanceData reads and unmarshals the on-disk store's raw
// InstanceData records, without constructing Instance objects for them
// (FromInstanceData restores a tracked instance's tmux session, which
// SaveInstances' merge above has no need to pay for).
func (s *Storage) loadRawInstanceData() ([]InstanceData, error) {
	jsonData := s.state.GetInstances()

	var instancesData []InstanceData
	if err := json.Unmarshal(jsonData, &instancesData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal instances: %w", err)
	}
	return instancesData, nil
}

// LoadInstances loads the list of instances from disk
func (s *Storage) LoadInstances() ([]*Instance, error) {
	instancesData, err := s.loadRawInstanceData()
	if err != nil {
		return nil, err
	}

	instances := make([]*Instance, len(instancesData))
	for i, data := range instancesData {
		instance, err := FromInstanceData(data)
		if err != nil {
			return nil, fmt.Errorf("failed to create instance %s: %w", data.Title, err)
		}
		instances[i] = instance
		s.known[data.Path] = true
	}

	return instances, nil
}

// UntrackedInstances returns freshly-constructed Instances for every
// on-disk record whose Path is not in known - the feed tick's adoption
// step (app.go's feedTickMsg handler), defect 1's read-side half: a lane
// registered directly into the store by another process (main.go's
// clarity-attach) appears in the running cockpit within one feed tick,
// with no restart needed. Only records NOT already in known are
// reconstructed via FromInstanceData (which restores a tracked instance's
// tmux session), so an already-tracked lane is never re-restored every
// tick just to be discarded. Each adopted record's path is recorded into
// `known`, exactly as LoadInstances does, so a later SaveInstances treats
// it as this process's own from here on.
func (s *Storage) UntrackedInstances(known map[string]bool) ([]*Instance, error) {
	raw, err := s.loadRawInstanceData()
	if err != nil {
		return nil, err
	}

	var out []*Instance
	for _, data := range raw {
		if known[data.Path] {
			continue
		}
		instance, err := FromInstanceData(data)
		if err != nil {
			return nil, fmt.Errorf("failed to create instance %s: %w", data.Title, err)
		}
		out = append(out, instance)
		s.known[data.Path] = true
	}
	return out, nil
}

// DeleteInstance removes an instance from storage
func (s *Storage) DeleteInstance(title string) error {
	instances, err := s.LoadInstances()
	if err != nil {
		return fmt.Errorf("failed to load instances: %w", err)
	}

	found := false
	newInstances := make([]*Instance, 0)
	for _, instance := range instances {
		data := instance.ToInstanceData()
		if data.Title != title {
			newInstances = append(newInstances, instance)
		} else {
			found = true
		}
	}

	if !found {
		return fmt.Errorf("instance not found: %s", title)
	}

	return s.SaveInstances(newInstances)
}

// UpdateInstance updates an existing instance in storage
func (s *Storage) UpdateInstance(instance *Instance) error {
	instances, err := s.LoadInstances()
	if err != nil {
		return fmt.Errorf("failed to load instances: %w", err)
	}

	data := instance.ToInstanceData()
	found := false
	for i, existing := range instances {
		existingData := existing.ToInstanceData()
		if existingData.Title == data.Title {
			instances[i] = instance
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("instance not found: %s", data.Title)
	}

	return s.SaveInstances(instances)
}

// DeleteAllInstances removes all stored instances
func (s *Storage) DeleteAllInstances() error {
	return s.state.DeleteAllInstances()
}
