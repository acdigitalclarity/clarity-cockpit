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

	root := os.Getenv(SessionsRootEnvVar)
	if root == "" {
		root = DefaultSessionsRoot
	}

	return filepath.Join(root, lane), nil
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
