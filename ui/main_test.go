package ui

import (
	"os"
	"testing"
)

// TestMain sets CLARITY_TEST_FORBID_TMUX for every test in this package
// (slice 15b): NewTerminalPane's own default newSession (ui/terminal.go's
// newRealTmuxSession) panics under this env var instead of shelling out to
// the real tmux binary, naming the session it would have created - the
// guard against the class of defect the fit tests in ui/fit_test.go used
// to carry (every go test run left real claudesquad_term_* sessions on the
// default tmux server). Every test that exercises the Terminal tab must
// construct its own TerminalPane with NewTerminalPaneWithDeps; this var is
// never set for the real binary (main.go never sets it), so
// NewTerminalPane's default constructor is unaffected there.
func TestMain(m *testing.M) {
	os.Setenv("CLARITY_TEST_FORBID_TMUX", "1")
	os.Exit(m.Run())
}
