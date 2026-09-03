package session

import (
	"claude-squad/log"
	"claude-squad/session/clarity"
	"claude-squad/session/git"
	"claude-squad/session/tmux"
	"errors"
	"path/filepath"

	"fmt"
	"os"
	"strings"
	"time"

	"github.com/atotto/clipboard"
)

type Status int

const (
	// Running is the status when the instance is running and claude is working.
	Running Status = iota
	// Ready is if the claude instance is ready to be interacted with (waiting for user input).
	Ready
	// Loading is if the instance is loading (if we are starting it up or something).
	Loading
	// Paused is if the instance is paused (worktree removed but branch preserved).
	Paused
)

// Instance is a running instance of claude code.
type Instance struct {
	// Title is the title of the instance.
	Title string
	// Path is the path to the workspace.
	Path string
	// Branch is the branch of the instance.
	Branch string
	// Status is the status of the instance.
	Status Status
	// Program is the program to run in the instance.
	Program string
	// Height is the height of the instance.
	Height int
	// Width is the width of the instance.
	Width int
	// CreatedAt is the time the instance was created.
	CreatedAt time.Time
	// UpdatedAt is the time the instance was last updated.
	UpdatedAt time.Time
	// AutoYes is true if the instance should automatically press enter when prompted.
	AutoYes bool
	// Prompt is the initial prompt to pass to the instance on startup
	Prompt string
	// NoWorktree is true for instances that run directly in Path with no git
	// worktree of their own (e.g. clarity-attach, which points at a Clarity
	// session lane's own working directory). Such instances have no branch,
	// no diff, and cannot be paused/resumed/pushed/checked out.
	NoWorktree bool

	// account and modality are the seat identity FRONTDOOR-SPEC.md's "The
	// store" section adds (slice 4) - unexported, with accessors below, so
	// every existing direct-field caller of Instance (Title, Path, ...)
	// stays exactly as it was and this pair is the only part of the store
	// identity that goes through a method. An empty account is itself a
	// valid seat (today's lanes).
	account  string
	modality string

	// DiffStats stores the current git diff statistics
	diffStats *git.DiffStats

	// contextFillPct/contextFillOK cache this instance's lane context-fill
	// gauge (see session/clarity/gauge.go), derived the same way
	// scripts/fleet_dashboard.py's fill_of() does. Set only from the main
	// event loop's metadata tick - same contract as diffStats/SetDiffStats,
	// to avoid data races with View.
	contextFillPct int
	contextFillOK  bool

	// needsKey caches whether this tracked instance's own tmux pane last
	// sampled a Claude Code permission prompt (ANSWER-AND-BANK-SPEC.md item
	// 7) - main event loop only, same contract as laneState/contextFillPct
	// below.
	needsKey bool

	// laneState/laneLastTurn/laneStateOK cache this instance's clarity-
	// derived conversational state (see session/clarity/tail.go's
	// ClassifyState: working/waiting on you/idle/stalled) and the last
	// timestamped record's time. Set only from the main event loop's feed
	// tick, same contract as contextFillPct/SetContextFill above.
	laneState    string
	laneLastTurn time.Time
	laneStateOK  bool
	// laneAnsweredAt caches item 5's own WAITING HELD transition instant
	// (see clarity.LaneTail.AnsweredAt's own doc comment) - set alongside
	// the three fields above on the same feed tick, read by the row's own
	// "ans Nm" abbreviation (ui/list.go) and the Session pane's own
	// "answered N min ago" state line. Zero when the last tick's own
	// classification carried none.
	laneAnsweredAt time.Time

	// selectedBranch is the existing branch to start on (empty = new branch from HEAD)
	selectedBranch string

	// The below fields are initialized upon calling Start().

	started bool
	// tmuxSession is the tmux session for the instance.
	tmuxSession *tmux.TmuxSession
	// gitWorktree is the git worktree for the instance.
	gitWorktree *git.GitWorktree
}

// ToInstanceData converts an Instance to its serializable form
func (i *Instance) ToInstanceData() InstanceData {
	data := InstanceData{
		Title:      i.Title,
		Path:       i.Path,
		Branch:     i.Branch,
		Status:     i.Status,
		Height:     i.Height,
		Width:      i.Width,
		CreatedAt:  i.CreatedAt,
		UpdatedAt:  time.Now(),
		Program:    i.Program,
		AutoYes:    i.AutoYes,
		NoWorktree: i.NoWorktree,
		Account:    i.account,
		Modality:   i.modality,
	}

	// Only include worktree data if gitWorktree is initialized
	if i.gitWorktree != nil {
		data.Worktree = GitWorktreeData{
			RepoPath:         i.gitWorktree.GetRepoPath(),
			WorktreePath:     i.gitWorktree.GetWorktreePath(),
			SessionName:      i.Title,
			BranchName:       i.gitWorktree.GetBranchName(),
			BaseCommitSHA:    i.gitWorktree.GetBaseCommitSHA(),
			IsExistingBranch: i.gitWorktree.IsExistingBranch(),
		}
	}

	// Only include diff stats if they exist
	if i.diffStats != nil {
		data.DiffStats = DiffStatsData{
			Added:   i.diffStats.Added,
			Removed: i.diffStats.Removed,
			Content: i.diffStats.Content,
		}
	}

	return data
}

// FromInstanceData creates a new Instance from serialized data
func FromInstanceData(data InstanceData) (*Instance, error) {
	instance := &Instance{
		Title:      data.Title,
		Path:       data.Path,
		Branch:     data.Branch,
		Status:     data.Status,
		Height:     data.Height,
		Width:      data.Width,
		CreatedAt:  data.CreatedAt,
		UpdatedAt:  data.UpdatedAt,
		Program:    data.Program,
		NoWorktree: data.NoWorktree,
		account:    data.Account,
		modality:   data.Modality,
		diffStats: &git.DiffStats{
			Added:   data.DiffStats.Added,
			Removed: data.DiffStats.Removed,
			Content: data.DiffStats.Content,
		},
	}

	// A NoWorktree instance (e.g. clarity-attach) has no git worktree to
	// reconstruct - it runs directly in Path, which is not managed by git
	// worktree add/remove at all.
	if !data.NoWorktree {
		instance.gitWorktree = git.NewGitWorktreeFromStorage(
			data.Worktree.RepoPath,
			data.Worktree.WorktreePath,
			data.Worktree.SessionName,
			data.Worktree.BranchName,
			data.Worktree.BaseCommitSHA,
			data.Worktree.IsExistingBranch,
		)
	}

	if instance.Paused() {
		instance.started = true
		instance.tmuxSession = tmux.NewTmuxSession(instance.Title, instance.Program)
	} else {
		if err := instance.Start(false); err != nil {
			return nil, err
		}
	}

	return instance, nil
}

// Options for creating a new instance
type InstanceOptions struct {
	// Title is the title of the instance.
	Title string
	// Path is the path to the workspace.
	Path string
	// Program is the program to run in the instance (e.g. "claude", "aider --model ollama_chat/gemma3:1b")
	Program string
	// If AutoYes is true, then
	AutoYes bool
	// Branch is an existing branch name to start the session on (empty = new branch from HEAD)
	Branch string
	// NoWorktree is true for instances that run directly in Path with no git
	// worktree of their own. See Instance.NoWorktree.
	NoWorktree bool
	// Account is the seat tag this instance registers under (e.g.
	// "team-b"). Empty is itself a valid seat (today's lanes).
	Account string
	// Modality is the lane's declared or autodetected modality (e.g.
	// "bid", "enhancement"). Empty when unknown.
	Modality string
}

func NewInstance(opts InstanceOptions) (*Instance, error) {
	t := time.Now()

	// Convert path to absolute
	absPath, err := filepath.Abs(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	return &Instance{
		Title:          opts.Title,
		Status:         Ready,
		Path:           absPath,
		Program:        opts.Program,
		Height:         0,
		Width:          0,
		CreatedAt:      t,
		UpdatedAt:      t,
		AutoYes:        false,
		selectedBranch: opts.Branch,
		NoWorktree:     opts.NoWorktree,
		account:        opts.Account,
		modality:       opts.Modality,
	}, nil
}

// Account returns the seat tag this instance is registered under. Empty is
// itself a valid seat (today's lanes) - see FRONTDOOR-SPEC.md "The store".
func (i *Instance) Account() string {
	return i.account
}

// SetAccount sets the seat tag this instance is registered under.
func (i *Instance) SetAccount(account string) {
	i.account = account
}

// Modality returns the instance's declared or autodetected modality (e.g.
// "bid", "enhancement"). Empty when unknown.
func (i *Instance) Modality() string {
	return i.modality
}

// SetModality sets the instance's modality.
func (i *Instance) SetModality(modality string) {
	i.modality = modality
}

func (i *Instance) RepoName() (string, error) {
	if !i.started {
		return "", fmt.Errorf("cannot get repo name for instance that has not been started")
	}
	if i.gitWorktree == nil {
		return "", fmt.Errorf("instance has no git worktree")
	}
	return i.gitWorktree.GetRepoName(), nil
}

// HasWorktree returns true if this instance has its own git worktree.
// clarity-attach instances (NoWorktree) do not.
func (i *Instance) HasWorktree() bool {
	return i.gitWorktree != nil
}

func (i *Instance) SetStatus(status Status) {
	i.Status = status
}

// SetSelectedBranch sets the branch to use when starting the instance.
func (i *Instance) SetSelectedBranch(branch string) {
	i.selectedBranch = branch
}

// firstTimeSetup is true if this is a new instance. Otherwise, it's one loaded from storage.
func (i *Instance) Start(firstTimeSetup bool) error {
	if i.Title == "" {
		return fmt.Errorf("instance title cannot be empty")
	}

	var tmuxSession *tmux.TmuxSession
	if i.tmuxSession != nil {
		// Use existing tmux session (useful for testing)
		tmuxSession = i.tmuxSession
	} else {
		// Create new tmux session
		tmuxSession = tmux.NewTmuxSession(i.Title, i.Program)
	}
	i.tmuxSession = tmuxSession

	if firstTimeSetup && !i.NoWorktree {
		if i.selectedBranch != "" {
			gitWorktree, err := git.NewGitWorktreeFromBranch(i.Path, i.selectedBranch, i.Title)
			if err != nil {
				return fmt.Errorf("failed to create git worktree from branch: %w", err)
			}
			i.gitWorktree = gitWorktree
			i.Branch = i.selectedBranch
		} else {
			gitWorktree, branchName, err := git.NewGitWorktree(i.Path, i.Title)
			if err != nil {
				return fmt.Errorf("failed to create git worktree: %w", err)
			}
			i.gitWorktree = gitWorktree
			i.Branch = branchName
		}
	}

	// Setup error handler to cleanup resources on any error
	var setupErr error
	defer func() {
		if setupErr != nil {
			if cleanupErr := i.Kill(); cleanupErr != nil {
				setupErr = fmt.Errorf("%v (cleanup error: %v)", setupErr, cleanupErr)
			}
		} else {
			i.started = true
		}
	}()

	if !firstTimeSetup {
		// Reuse existing session. If the tmux server died since we last ran (reboot,
		// crash, `tmux kill-server`), the session is gone but the worktree and branch
		// are still on disk. Park the instance as Paused so Resume can rebuild it.
		// Reporting an error here would be worse than useless: LoadInstances aborts on
		// the first failure, so a single dead session would hide every other instance.
		if err := tmuxSession.Restore(); err != nil {
			if errors.Is(err, tmux.ErrSessionNotFound) {
				log.WarningLog.Printf(
					"tmux session for %q no longer exists; pausing instance so it can be resumed", i.Title)
				i.SetStatus(Paused)
				return nil
			}
			setupErr = fmt.Errorf("failed to restore existing session: %w", err)
			return setupErr
		}
	} else if i.NoWorktree {
		// No git worktree: run the tmux session directly in the instance's
		// own working directory (a clarity-attach lane's existing worktree).
		if err := i.tmuxSession.Start(i.Path); err != nil {
			setupErr = fmt.Errorf("failed to start new session: %w", err)
			return setupErr
		}
	} else {
		// Setup git worktree first
		if err := i.gitWorktree.Setup(); err != nil {
			setupErr = fmt.Errorf("failed to setup git worktree: %w", err)
			return setupErr
		}

		// Create new session
		if err := i.tmuxSession.Start(i.gitWorktree.GetWorktreePath()); err != nil {
			// Cleanup git worktree if tmux session creation fails
			if cleanupErr := i.gitWorktree.Cleanup(); cleanupErr != nil {
				err = fmt.Errorf("%v (cleanup error: %v)", err, cleanupErr)
			}
			setupErr = fmt.Errorf("failed to start new session: %w", err)
			return setupErr
		}
	}

	i.SetStatus(Running)

	return nil
}

// Kill terminates the instance and cleans up all resources. For a
// NoWorktree instance i.gitWorktree is always nil (see NoWorktree), so the
// git cleanup block below is naturally skipped - Kill removes the instance
// and its tmux session only, never touching git (slice 8 rule 1).
func (i *Instance) Kill() error {
	if !i.started {
		// If instance was never started, just return success
		return nil
	}

	var errs []error

	// Always try to cleanup both resources, even if one fails
	// Clean up tmux session first since it's using the git worktree
	if i.tmuxSession != nil {
		if err := i.tmuxSession.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close tmux session: %w", err))
		}
	}

	// Then clean up git worktree
	if i.gitWorktree != nil {
		if err := i.gitWorktree.Cleanup(); err != nil {
			errs = append(errs, fmt.Errorf("failed to cleanup git worktree: %w", err))
		}
	}

	return i.combineErrors(errs)
}

// combineErrors combines multiple errors into a single error
func (i *Instance) combineErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}

	errMsg := "multiple cleanup errors occurred:"
	for _, err := range errs {
		errMsg += "\n  - " + err.Error()
	}
	return fmt.Errorf("%s", errMsg)
}

func (i *Instance) Preview() (string, error) {
	if !i.started || i.Status == Paused {
		return "", nil
	}
	return i.tmuxSession.CapturePaneContent()
}

func (i *Instance) HasUpdated() (updated bool, hasPrompt bool) {
	if !i.started {
		return false, false
	}
	return i.tmuxSession.HasUpdated()
}

// CheckAndHandleTrustPrompt checks for and dismisses the trust prompt for supported programs.
func (i *Instance) CheckAndHandleTrustPrompt() bool {
	if !i.started || i.tmuxSession == nil {
		return false
	}
	program := i.Program
	if !strings.HasSuffix(program, tmux.ProgramClaude) &&
		!strings.HasSuffix(program, tmux.ProgramAider) &&
		!strings.HasSuffix(program, tmux.ProgramGemini) {
		return false
	}
	return i.tmuxSession.CheckAndHandleTrustPrompt()
}

// TapEnter sends an enter key press to the tmux session if AutoYes is enabled.
func (i *Instance) TapEnter() {
	if !i.started || !i.AutoYes {
		return
	}
	if err := i.tmuxSession.TapEnter(); err != nil {
		log.ErrorLog.Printf("error tapping enter: %v", err)
	}
}

func (i *Instance) Attach() (chan struct{}, error) {
	if !i.started {
		return nil, fmt.Errorf("cannot attach instance that has not been started")
	}
	return i.tmuxSession.Attach()
}

// AttachEndedWithoutDetach reports whether the instance's most recently
// finished attach ended because the program running inside tmux exited on
// its own (Ctrl-D, `exit`, a crash) rather than because Ctrl-Q/Ctrl-] was
// pressed. Call only after the channel Attach returned has closed - board
// #317: the caller should mark such an instance Paused instead of treating
// it as a normal detach. Gated on Started() first, never tmuxSession alone
// (mirrors RequiresCopyOnlySend's own comment above): a construction-only
// test double never calls Start(), so it has no tmux session to check at all.
func (i *Instance) AttachEndedWithoutDetach() bool {
	if !i.started || i.tmuxSession == nil {
		return false
	}
	return errors.Is(i.tmuxSession.LastAttachOutcome(), tmux.ErrSessionEnded)
}

func (i *Instance) SetPreviewSize(width, height int) error {
	if !i.started || i.Status == Paused {
		return fmt.Errorf("cannot set preview size for instance that has not been started or " +
			"is paused")
	}
	return i.tmuxSession.SetDetachedSize(width, height)
}

// GetGitWorktree returns the git worktree for the instance
func (i *Instance) GetGitWorktree() (*git.GitWorktree, error) {
	if !i.started {
		return nil, fmt.Errorf("cannot get git worktree for instance that has not been started")
	}
	return i.gitWorktree, nil
}

// GetWorktreePath returns the worktree path for the instance, or empty string if unavailable
func (i *Instance) GetWorktreePath() string {
	if i.gitWorktree == nil {
		return ""
	}
	return i.gitWorktree.GetWorktreePath()
}

func (i *Instance) Started() bool {
	return i.started
}

// SetTitle sets the title of the instance. Returns an error if the instance has started.
// We cant change the title once it's been used for a tmux session etc.
func (i *Instance) SetTitle(title string) error {
	if i.started {
		return fmt.Errorf("cannot change title of a started instance")
	}
	i.Title = title
	return nil
}

func (i *Instance) Paused() bool {
	return i.Status == Paused
}

// TmuxAlive returns true if the tmux session is alive. This is a sanity check before attaching.
func (i *Instance) TmuxAlive() bool {
	return i.tmuxSession.DoesSessionExist()
}

// RequiresCopyOnlySend reports whether this tracked instance has no live
// tmux session to deliver a keystroke into - a Paused (or otherwise
// stopped) instance, most commonly a NoWorktree clarity-attach lane that
// runs in the owner's own terminal (cockpit pane-10 walkthrough DEFECT 1:
// the composer's tracked send path used to be picked for ANY tracked row
// regardless of session state, and errored "not a live tmux session" on
// enter instead of falling back to the clipboard-copy path an external lane
// already uses). Gated on Started() first, never TmuxAlive() alone, since an
// instance that has never been started (never in production - every row a
// real list holds is already started; this guard exists only so a
// construction-only test double, which never calls Start(), does not panic
// dereferencing a nil tmuxSession) has no tmux session to check at all.
func (i *Instance) RequiresCopyOnlySend() bool {
	return i.started && !i.TmuxAlive()
}

// pauseNoWorktree closes a NoWorktree instance's tmux session - there is no
// git worktree to preserve (see Instance.NoWorktree), so Pause here never
// touches git at all: it just stops tracking a live session. The lane
// itself (its Path, on disk) is untouched either way.
func (i *Instance) pauseNoWorktree() error {
	if i.Status == Paused {
		return fmt.Errorf("instance is already paused")
	}
	if i.tmuxSession != nil {
		if err := i.tmuxSession.Close(); err != nil {
			log.ErrorLog.Print(err)
			return fmt.Errorf("failed to close tmux session: %w", err)
		}
	}
	i.SetStatus(Paused)
	return nil
}

// Pause stops the tmux session and removes the worktree, preserving the branch
func (i *Instance) Pause() error {
	if !i.started {
		return fmt.Errorf("cannot pause instance that has not been started")
	}
	if i.NoWorktree {
		return i.pauseNoWorktree()
	}
	if i.Status == Paused {
		return fmt.Errorf("instance is already paused")
	}

	var errs []error

	// If the worktree is orphaned (path or .git missing), git cannot operate
	// on it. Skip dirty check and Remove, prune any lingering metadata, then
	// transition to Paused so the user can recover via Resume.
	if valid, err := i.gitWorktree.IsValidWorktree(); err != nil {
		errs = append(errs, fmt.Errorf("failed to validate worktree: %w", err))
		log.ErrorLog.Print(err)
	} else if !valid {
		log.WarningLog.Printf("worktree at %s is orphaned; skipping dirty check and remove",
			i.gitWorktree.GetWorktreePath())
		if err := i.tmuxSession.DetachSafely(); err != nil {
			errs = append(errs, fmt.Errorf("failed to detach tmux session: %w", err))
			log.ErrorLog.Print(err)
		}
		// Drop any leftover directory so a future Resume's `git worktree add` won't conflict.
		if err := os.RemoveAll(i.gitWorktree.GetWorktreePath()); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove orphaned worktree directory: %w", err))
			log.ErrorLog.Print(err)
		}
		if err := i.gitWorktree.Prune(); err != nil {
			errs = append(errs, fmt.Errorf("failed to prune git worktrees: %w", err))
			log.ErrorLog.Print(err)
		}
		i.SetStatus(Paused)
		_ = clipboard.WriteAll(i.gitWorktree.GetBranchName())
		return i.combineErrors(errs)
	}

	// Check if there are any changes to commit
	if dirty, err := i.gitWorktree.IsDirty(); err != nil {
		errs = append(errs, fmt.Errorf("failed to check if worktree is dirty: %w", err))
		log.ErrorLog.Print(err)
	} else if dirty {
		// Commit changes locally (without pushing to GitHub)
		commitMsg := fmt.Sprintf("[claudesquad] update from '%s' on %s (paused)", i.Title, time.Now().Format(time.RFC822))
		if err := i.gitWorktree.CommitChanges(commitMsg); err != nil {
			errs = append(errs, fmt.Errorf("failed to commit changes: %w", err))
			log.ErrorLog.Print(err)
			// Return early if we can't commit changes to avoid corrupted state
			return i.combineErrors(errs)
		}
	}

	// Detach from tmux session instead of closing to preserve session output
	if err := i.tmuxSession.DetachSafely(); err != nil {
		errs = append(errs, fmt.Errorf("failed to detach tmux session: %w", err))
		log.ErrorLog.Print(err)
		// Continue with pause process even if detach fails
	}

	// Check if worktree exists before trying to remove it
	if _, err := os.Stat(i.gitWorktree.GetWorktreePath()); err == nil {
		// Remove worktree but keep branch
		if err := i.gitWorktree.Remove(); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove git worktree: %w", err))
			log.ErrorLog.Print(err)
			return i.combineErrors(errs)
		}

		// Only prune if remove was successful
		if err := i.gitWorktree.Prune(); err != nil {
			errs = append(errs, fmt.Errorf("failed to prune git worktrees: %w", err))
			log.ErrorLog.Print(err)
			return i.combineErrors(errs)
		}
	}

	i.SetStatus(Paused)
	_ = clipboard.WriteAll(i.gitWorktree.GetBranchName())

	if err := i.combineErrors(errs); err != nil {
		log.ErrorLog.Print(err)
		return err
	}
	return nil
}

// resumeNoWorktree creates (or reuses) a NoWorktree instance's tmux session
// directly in its own Path - there is no git worktree to set up (see
// Instance.NoWorktree). The caller (app.go's r key, slice 8 rule 2) owns
// the state-based gate deciding whether resuming is safe to attempt at all
// (never spin up a second Claude in a folder the owner's own terminal is
// still driving) - this method itself always resumes when called.
func (i *Instance) resumeNoWorktree() error {
	if i.tmuxSession.DoesSessionExist() {
		if err := i.tmuxSession.Restore(); err != nil {
			log.ErrorLog.Print(err)
			return fmt.Errorf("failed to restore existing session: %w", err)
		}
	} else {
		if err := i.tmuxSession.Start(i.Path); err != nil {
			log.ErrorLog.Print(err)
			return fmt.Errorf("failed to start new session: %w", err)
		}
	}
	i.SetStatus(Running)
	return nil
}

// Resume recreates the worktree and restarts the tmux session
func (i *Instance) Resume() error {
	if !i.started {
		return fmt.Errorf("cannot resume instance that has not been started")
	}
	if i.NoWorktree {
		return i.resumeNoWorktree()
	}
	if i.Status != Paused {
		return fmt.Errorf("can only resume paused instances")
	}

	// Check if branch is checked out
	if checked, err := i.gitWorktree.IsBranchCheckedOut(); err != nil {
		log.ErrorLog.Print(err)
		return fmt.Errorf("failed to check if branch is checked out: %w", err)
	} else if checked {
		return fmt.Errorf("cannot resume: branch is checked out, please switch to a different branch")
	}

	// Setup git worktree. Setup removes and re-adds the worktree from the branch, which
	// throws away anything uncommitted in it. After a normal Pause the directory is gone
	// and that is exactly what we want; but an instance paused because its tmux session
	// died still has its worktree — and the work in it — sitting on disk, so leave it be.
	if valid, err := i.gitWorktree.IsValidWorktree(); err != nil || !valid {
		if err != nil {
			log.WarningLog.Printf("could not validate worktree at %s, recreating it: %v",
				i.gitWorktree.GetWorktreePath(), err)
		}
		if err := i.gitWorktree.Setup(); err != nil {
			log.ErrorLog.Print(err)
			return fmt.Errorf("failed to setup git worktree: %w", err)
		}
	}

	// Check if tmux session still exists from pause, otherwise create new one
	if i.tmuxSession.DoesSessionExist() {
		// Session exists, just restore PTY connection to it
		if err := i.tmuxSession.Restore(); err != nil {
			log.ErrorLog.Print(err)
			// If restore fails, fall back to creating new session
			if err := i.tmuxSession.Start(i.gitWorktree.GetWorktreePath()); err != nil {
				log.ErrorLog.Print(err)
				// Cleanup git worktree if tmux session creation fails
				if cleanupErr := i.gitWorktree.Cleanup(); cleanupErr != nil {
					err = fmt.Errorf("%v (cleanup error: %v)", err, cleanupErr)
					log.ErrorLog.Print(err)
				}
				return fmt.Errorf("failed to start new session: %w", err)
			}
		}
	} else {
		// Create new tmux session
		if err := i.tmuxSession.Start(i.gitWorktree.GetWorktreePath()); err != nil {
			log.ErrorLog.Print(err)
			// Cleanup git worktree if tmux session creation fails
			if cleanupErr := i.gitWorktree.Cleanup(); cleanupErr != nil {
				err = fmt.Errorf("%v (cleanup error: %v)", err, cleanupErr)
				log.ErrorLog.Print(err)
			}
			return fmt.Errorf("failed to start new session: %w", err)
		}
	}

	i.SetStatus(Running)
	return nil
}

// UpdateDiffStats updates the git diff statistics for this instance
func (i *Instance) UpdateDiffStats() error {
	if !i.started {
		i.diffStats = nil
		return nil
	}

	if i.Status == Paused {
		// Keep the previous diff stats if the instance is paused
		return nil
	}

	if i.NoWorktree {
		// No git worktree, so no diff to compute.
		i.diffStats = nil
		return nil
	}

	stats := i.gitWorktree.Diff()
	if stats.Error != nil {
		if strings.Contains(stats.Error.Error(), "base commit SHA not set") {
			// Worktree is not fully set up yet, not an error
			i.diffStats = nil
			return nil
		}
		return fmt.Errorf("failed to get diff stats: %w", stats.Error)
	}

	i.diffStats = stats
	return nil
}

// ComputeDiff runs the expensive git diff I/O and returns the result without
// mutating instance state. Safe to call from a background goroutine.
func (i *Instance) ComputeDiff() *git.DiffStats {
	if !i.started || i.Status == Paused || i.NoWorktree {
		return nil
	}
	return i.gitWorktree.Diff()
}

// ComputeDiffNumstat runs a lightweight git diff --numstat and returns only the
// added/removed line counts (Content is left empty). Safe to call from a
// background goroutine. Use this for instances whose full diff content is not
// currently needed so we avoid keeping large diffs in memory.
func (i *Instance) ComputeDiffNumstat() *git.DiffStats {
	if !i.started || i.Status == Paused || i.NoWorktree {
		return nil
	}
	return i.gitWorktree.DiffNumstat()
}

// SetDiffStats sets the diff statistics on the instance. Should be called from
// the main event loop to avoid data races with View.
func (i *Instance) SetDiffStats(stats *git.DiffStats) {
	i.diffStats = stats
}

// GetDiffStats returns the current git diff statistics
func (i *Instance) GetDiffStats() *git.DiffStats {
	return i.diffStats
}

// ComputeContextFill runs the (file-read-only) context-fill derivation for
// this instance's lane and returns the result without mutating instance
// state. Safe to call from a background goroutine, same contract as
// ComputeDiff/ComputeDiffNumstat above. ok is false ("n/a") when no
// transcript resolves for this instance's Path.
func (i *Instance) ComputeContextFill() (pct int, ok bool) {
	fill, ok := clarity.ContextFillForLane(i.Path)
	if !ok {
		return 0, false
	}
	return fill.Pct, true
}

// SetContextFill caches the context-fill gauge on the instance. Should be
// called from the main event loop only, to avoid data races with View -
// same contract as SetDiffStats.
func (i *Instance) SetContextFill(pct int, ok bool) {
	i.contextFillPct = pct
	i.contextFillOK = ok
}

// GetContextFill returns the cached context-fill gauge.
func (i *Instance) GetContextFill() (pct int, ok bool) {
	return i.contextFillPct, i.contextFillOK
}

// SetLaneState caches the instance's clarity-derived conversational state
// and last-turn time. Should be called from the main event loop only, same
// contract as SetContextFill.
func (i *Instance) SetLaneState(state string, lastTurn time.Time, ok bool) {
	i.laneState = state
	i.laneLastTurn = lastTurn
	i.laneStateOK = ok
}

// GetLaneState returns the cached clarity-derived state. ok is false before
// the first feed tick has computed it for this instance.
func (i *Instance) GetLaneState() (state string, lastTurn time.Time, ok bool) {
	return i.laneState, i.laneLastTurn, i.laneStateOK
}

// SetAnsweredAt caches item 5's own WAITING HELD transition instant. Should
// be called from the main event loop only, same contract as SetLaneState -
// a separate pair of accessors rather than extra return values on
// SetLaneState/GetLaneState, so every existing caller of those two (the
// main event loop and every test across the app/ui packages) stays
// untouched.
func (i *Instance) SetAnsweredAt(at time.Time) {
	i.laneAnsweredAt = at
}

// GetAnsweredAt returns the cached WAITING HELD transition instant - the
// zero value when the last feed tick's own classification carried none.
func (i *Instance) GetAnsweredAt() time.Time {
	return i.laneAnsweredAt
}

// SetNeedsKey caches whether this instance's own tmux pane last sampled a
// permission prompt - called from the main event loop only (app.go's feed
// tick), same contract as SetContextFill/SetLaneState.
func (i *Instance) SetNeedsKey(needsKey bool) {
	i.needsKey = needsKey
}

// NeedsKey returns the cached permission-prompt sample.
func (i *Instance) NeedsKey() bool {
	return i.needsKey
}

// SendPrompt sends a prompt to the tmux session
func (i *Instance) SendPrompt(prompt string) error {
	if !i.started {
		return fmt.Errorf("instance not started")
	}
	if i.tmuxSession == nil {
		return fmt.Errorf("tmux session not initialized")
	}
	if err := i.tmuxSession.SendKeys(prompt); err != nil {
		return fmt.Errorf("error sending keys to tmux session: %w", err)
	}

	// Brief pause to prevent carriage return from being interpreted as newline
	time.Sleep(100 * time.Millisecond)
	if err := i.tmuxSession.TapEnter(); err != nil {
		return fmt.Errorf("error tapping enter: %w", err)
	}

	return nil
}

// PreviewFullHistory captures the entire tmux pane output including full scrollback history
func (i *Instance) PreviewFullHistory() (string, error) {
	if !i.started || i.Status == Paused {
		return "", nil
	}
	return i.tmuxSession.CapturePaneContentWithOptions("-", "-")
}

// SetTmuxSession sets the tmux session for testing purposes
func (i *Instance) SetTmuxSession(session *tmux.TmuxSession) {
	i.tmuxSession = session
}

// SendKeys sends keys to the tmux session
func (i *Instance) SendKeys(keys string) error {
	if !i.started || i.Status == Paused {
		return fmt.Errorf("cannot send keys to instance that has not been started or is paused")
	}
	return i.tmuxSession.SendKeys(keys)
}
