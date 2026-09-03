// Package clarity resolves the working directory for a Clarity workspace
// session lane, used by the `clarity-attach` subcommand added on top of
// upstream Claude Squad. A clarity session lane already IS an isolated
// working directory (sessions/<lane>/); clarity-attach registers a Claude
// Squad instance pointed at that directory directly, with no new git
// worktree, so the lane's own worktree-per-lane bookkeeping is untouched.
package clarity

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SessionsRootEnvVar overrides the default Clarity sessions root. Set only
// for tests; the real CLI relies on the default.
const SessionsRootEnvVar = "CLARITY_SESSIONS_ROOT"

// DefaultSessionsRoot is the standard location of Clarity workspace session
// lanes on this machine.
const DefaultSessionsRoot = "/Users/allencoates/projects/Clarity/sessions"

// SessionsRoot returns the Clarity workspace sessions root, honouring
// SessionsRootEnvVar for tests exactly the way ResolveLanePath already does
// - the new-lane overlay's step 1 (FRONTDOOR-SPEC.md "below it, the folder
// that will be created") needs the bare root to build its preview line
// before a name even exists, so it cannot call ResolveLanePath itself
// (which requires a non-empty lane name).
func SessionsRoot() string {
	root := os.Getenv(SessionsRootEnvVar)
	if root == "" {
		root = DefaultSessionsRoot
	}
	return root
}

// ResolveLanePath returns the absolute working directory for a Clarity
// session lane, without touching the filesystem. It rejects empty lane
// names and any lane name that could escape the sessions root (path
// separators, "." or ".." segments), since a lane name comes from a
// command-line argument and must not be treated as a path.
func ResolveLanePath(lane string) (string, error) {
	if strings.TrimSpace(lane) == "" {
		return "", fmt.Errorf("clarity lane name must not be empty")
	}
	if lane != filepath.Base(lane) {
		return "", fmt.Errorf("clarity lane name %q must be a bare name, not a path", lane)
	}
	if lane == "." || lane == ".." {
		return "", fmt.Errorf("clarity lane name %q is not a valid lane", lane)
	}

	return filepath.Join(SessionsRoot(), lane), nil
}

// ForgeAppsRootEnvVar overrides the default clarity-forge apps directory.
// Set only for tests; the real CLI relies on the default.
const ForgeAppsRootEnvVar = "CLARITY_FORGE_APPS_ROOT"

// DefaultForgeAppsRoot is where clarity-forge's own scaffolded apps live -
// autodetect rule N3 (FRONTDOOR-SPEC.md "Autodetect: the ladder").
const DefaultForgeAppsRoot = "/Users/allencoates/projects/Clarity/repos/clarity-forge/apps"

// ForgeAppsRoot returns the clarity-forge apps root, honouring
// ForgeAppsRootEnvVar for tests.
func ForgeAppsRoot() string {
	root := os.Getenv(ForgeAppsRootEnvVar)
	if root == "" {
		root = DefaultForgeAppsRoot
	}
	return root
}

// ClarityWrapperEnvVar overrides the `clarity` wrapper script's own path.
// Set only for tests; the real CLI relies on the default.
const ClarityWrapperEnvVar = "CLARITY_WRAPPER_BIN"

// DefaultClarityWrapperPath is the wrapper's real location on this machine.
const DefaultClarityWrapperPath = "/Users/allencoates/projects/Clarity/scripts/clarity"

// ClarityWrapperPath returns the `clarity` wrapper script's path, honouring
// ClarityWrapperEnvVar for tests. The new-lane overlay's "Starting" step
// (FRONTDOOR-SPEC.md item 2) shells out to this rather than reproducing the
// wrapper's own folder-creation logic a second time.
func ClarityWrapperPath() string {
	path := os.Getenv(ClarityWrapperEnvVar)
	if path == "" {
		path = DefaultClarityWrapperPath
	}
	return path
}

// ResolveExistingLaneDir resolves the lane path and confirms it exists and
// is a directory, returning an error naming exactly what was checked
// otherwise.
func ResolveExistingLaneDir(lane string) (string, error) {
	path, err := ResolveLanePath(lane)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("clarity lane directory not found at %s: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("clarity lane path %s exists but is not a directory", path)
	}

	return path, nil
}
