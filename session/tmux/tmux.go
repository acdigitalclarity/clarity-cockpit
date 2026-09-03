package tmux

import (
	"bytes"
	"claude-squad/cmd"
	"claude-squad/log"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

const ProgramClaude = "claude"

const ProgramAider = "aider"
const ProgramGemini = "gemini"

// TmuxSession represents a managed tmux session
type TmuxSession struct {
	// Initialized by NewTmuxSession
	//
	// The name of the tmux session and the sanitized name used for tmux commands.
	sanitizedName string
	program       string
	// ptyFactory is used to create a PTY for the tmux session.
	ptyFactory PtyFactory
	// cmdExec is used to execute commands in the tmux session.
	cmdExec cmd.Executor

	// Initialized by Start or Restore
	//
	// ptmx is a PTY is running the tmux attach command. This can be resized to change the
	// stdout dimensions of the tmux pane. On detach, we close it and set a new one.
	// This should never be nil.
	ptmx *os.File
	// monitor monitors the tmux pane content and sends signals to the UI when it's status changes
	monitor *statusMonitor

	// Initialized by Attach
	// Deinitilaized by Detach
	//
	// Channel to be closed at the very end of detaching. Used to signal callers.
	attachCh chan struct{}
	// attachOutcome is set by the Attach copy-loop goroutine just before
	// attachCh closes: nil for a real Ctrl-Q/Ctrl-] detach, ErrSessionEnded
	// when the pty went away on its own. Read via LastAttachOutcome once the
	// channel Attach returned has closed - that close is the synchronization
	// point, so no separate lock is needed.
	attachOutcome error
	// While attached, we use some goroutines to manage the window size and stdin/stdout. This stuff
	// is used to terminate them on Detach. We don't want them to outlive the attached window.
	ctx    context.Context
	cancel func()
	wg     *sync.WaitGroup
	// stdinRawState is the real terminal's own termios state from just before
	// Attach put it into raw mode, restored on Detach/DetachSafely. Nil when
	// stdin isn't a terminal (tests, piped input) or we're not attached.
	//
	// Needed because Attach's own read loop below forwards stdin byte for
	// byte (including a lone detach key, detected as a single-byte read of
	// 0x11 ctrl-q or 0x1d ctrl-]) - that only works in raw mode. Whatever put
	// the real terminal in raw mode before Attach ran (bubbletea's own
	// Program, for the cockpit's own keyboard; nothing, for a bare terminal)
	// is not guaranteed to still own it for Attach's duration: the cockpit
	// hands the terminal to Attach via tea.Exec (app/attach_exec.go), which
	// releases bubbletea's raw mode and restores whatever cooked state came
	// before the Program started - not raw. Left uncorrected, the real
	// terminal sits in canonical mode for the whole attach: printable keys
	// don't reach the PTY until Enter completes a line, and a lone detach key
	// never completes a line on its own, so it sits in the kernel's line
	// buffer until something else does - never delivered as the single byte
	// this loop's `nr == 1` check needs.
	stdinRawState *term.State

	// Board #325: the stdin-forwarding goroutine below reads with a blocking
	// os.Stdin.Read that cannot be cancelled, so a read started during one
	// attach can return after that attach ended - or after a new one has
	// already started. These fields are how the goroutine (started at most
	// once per TmuxSession, never stacked - see startStdinReader) decides,
	// each time a read actually returns, whether an attach is live RIGHT
	// NOW and if so which pty owns the byte. They persist ACROSS attach
	// cycles (unlike ctx/cancel/wg above, which Detach nils every cycle)
	// because the goroutine itself can outlive a single cycle.
	stdinMu        sync.Mutex
	stdinGen       uint64    // bumped by every Attach call; carried in log lines for debugging, not itself load-bearing for the live/target check below.
	stdinLive      bool      // true only while the attach at stdinGen is still attached; cleared by both Detach and DetachSafely (board #317's two teardown paths).
	stdinTarget    *os.File  // the ptmx to forward to while stdinLive is true.
	stdinNukeUntil time.Time // end of the "nuke first bytes" window for the CURRENT attach cycle; reset by every Attach call.

	// stdinReader is where the goroutine reads from; nil in every real
	// build, where startStdinReader defaults it to os.Stdin. Tests
	// substitute a fake pipe so a byte's timing relative to attach/detach
	// can be controlled without a real terminal.
	stdinReader io.Reader
	// stdinReaderAlive is 1 while the persistent goroutine is running, 0
	// once it has returned (real EOF only - a live attach ending does not
	// stop it, since os.Stdin.Read cannot be interrupted). startStdinReader
	// CAS's this 0->1 to claim the right to spawn; a failed CAS means a
	// goroutine from an earlier cycle is still blocked in Read and must be
	// reused, never joined by a second one.
	stdinReaderAlive int32
	// stdinReaderStarts counts real goroutine launches, for tests to prove
	// a leaked reader was reused rather than duplicated.
	stdinReaderStarts int32
	// stdinProcessed is a test-only hook: when non-nil, stdinForwardLoop
	// sends on it after fully handling each read, giving tests a
	// deterministic point to synchronize on instead of sleeping. Always nil
	// in production.
	stdinProcessed chan struct{}
}

const TmuxPrefix = "claudesquad_"

// tmuxSocketEnvVar names the environment variable an isolated test/rig
// process sets to point every tmux command this package issues at a named
// socket (slice 17c, TMUX SOCKET RULE: every tmux command names its
// socket, TMUX_TMPDIR is never a fence). Unset in every real install, so a
// real seat's own tmux calls stay byte-for-byte what they were before this
// existed - a bare `exec.Command("tmux", args...)` with no -L, targeting
// the one shared ambient default server the whole fleet already relies on.
const tmuxSocketEnvVar = "CLARITY_TMUX_SOCKET"

// socketArgs returns ["-L", name] when tmuxSocketEnvVar names a socket,
// nil otherwise - prepended to every argument list tmuxCmd builds.
func socketArgs() []string {
	if s := os.Getenv(tmuxSocketEnvVar); s != "" {
		return []string{"-L", s}
	}
	return nil
}

// tmuxCmd builds a tmux invocation, applying socketArgs() ahead of args -
// the single choke point every exec.Command("tmux", ...) call in this file
// now goes through, so the socket rule is enforced in one place rather than
// re-applied at each call site.
func tmuxCmd(args ...string) *exec.Cmd {
	return exec.Command("tmux", append(socketArgs(), args...)...)
}

// ErrSessionNotFound is returned when the tmux session backing an instance is gone, which
// happens whenever the tmux server dies (reboot, crash, `tmux kill-server`).
var ErrSessionNotFound = errors.New("tmux session no longer exists")

// ErrSessionEnded is Attach's completion outcome (read via LastAttachOutcome,
// once the channel Attach returned has closed) when the program running
// inside tmux exited on its own - Ctrl-D, `exit`, a crash - before Ctrl-Q or
// Ctrl-] was pressed. Nothing calls Detach in that case, so the goroutine
// that notices the pty went away finishes the teardown itself (board #317).
// A real Ctrl-Q/Ctrl-] detach leaves LastAttachOutcome nil.
var ErrSessionEnded = errors.New("tmux session ended before it was detached")

var whiteSpaceRegex = regexp.MustCompile(`\s+`)

func toClaudeSquadTmuxName(str string) string {
	str = whiteSpaceRegex.ReplaceAllString(str, "")
	str = strings.ReplaceAll(str, ".", "_") // tmux replaces all . with _
	return fmt.Sprintf("%s%s", TmuxPrefix, str)
}

// NewTmuxSession creates a new TmuxSession with the given name and program.
func NewTmuxSession(name string, program string) *TmuxSession {
	return newTmuxSession(name, program, MakePtyFactory(), cmd.MakeExecutor())
}

// NewTmuxSessionWithDeps creates a new TmuxSession with provided dependencies for testing.
func NewTmuxSessionWithDeps(name string, program string, ptyFactory PtyFactory, cmdExec cmd.Executor) *TmuxSession {
	return newTmuxSession(name, program, ptyFactory, cmdExec)
}

func newTmuxSession(name string, program string, ptyFactory PtyFactory, cmdExec cmd.Executor) *TmuxSession {
	return &TmuxSession{
		sanitizedName: toClaudeSquadTmuxName(name),
		program:       program,
		ptyFactory:    ptyFactory,
		cmdExec:       cmdExec,
	}
}

// Start creates and starts a new tmux session, then attaches to it. Program is the command to run in
// the session (ex. claude). workdir is the git worktree directory.
func (t *TmuxSession) Start(workDir string) error {
	// Check if the session already exists
	if t.DoesSessionExist() {
		return fmt.Errorf("tmux session already exists: %s", t.sanitizedName)
	}

	// Create a new detached tmux session and start claude in it
	cmd := tmuxCmd("new-session", "-d", "-s", t.sanitizedName, "-c", workDir, t.program)

	ptmx, err := t.ptyFactory.Start(cmd)
	if err != nil {
		// Cleanup any partially created session if any exists.
		if t.DoesSessionExist() {
			cleanupCmd := tmuxCmd("kill-session", "-t", t.sanitizedName)
			if cleanupErr := t.cmdExec.Run(cleanupCmd); cleanupErr != nil {
				err = fmt.Errorf("%v (cleanup error: %v)", err, cleanupErr)
			}
		}
		return fmt.Errorf("error starting tmux session: %w", err)
	}

	// Poll for session existence with exponential backoff
	timeout := time.After(2 * time.Second)
	sleepDuration := 5 * time.Millisecond
	for !t.DoesSessionExist() {
		select {
		case <-timeout:
			if cleanupErr := t.Close(); cleanupErr != nil {
				err = fmt.Errorf("%v (cleanup error: %v)", err, cleanupErr)
			}
			return fmt.Errorf("timed out waiting for tmux session %s: %v", t.sanitizedName, err)
		default:
			time.Sleep(sleepDuration)
			// Exponential backoff up to 50ms max
			if sleepDuration < 50*time.Millisecond {
				sleepDuration *= 2
			}
		}
	}
	ptmx.Close()

	// Set history limit to enable scrollback (default is 2000, we'll use 10000 for more history)
	historyCmd := tmuxCmd("set-option", "-t", t.sanitizedName, "history-limit", "10000")
	if err := t.cmdExec.Run(historyCmd); err != nil {
		log.InfoLog.Printf("Warning: failed to set history-limit for session %s: %v", t.sanitizedName, err)
	}

	// Enable mouse scrolling for the session
	mouseCmd := tmuxCmd("set-option", "-t", t.sanitizedName, "mouse", "on")
	if err := t.cmdExec.Run(mouseCmd); err != nil {
		log.InfoLog.Printf("Warning: failed to enable mouse scrolling for session %s: %v", t.sanitizedName, err)
	}

	err = t.Restore()
	if err != nil {
		if cleanupErr := t.Close(); cleanupErr != nil {
			err = fmt.Errorf("%v (cleanup error: %v)", err, cleanupErr)
		}
		return fmt.Errorf("error restoring tmux session: %w", err)
	}

	return nil
}

// CheckAndHandleTrustPrompt checks the pane content once for a trust prompt and dismisses it if found.
// Returns true if the prompt was found and handled.
func (t *TmuxSession) CheckAndHandleTrustPrompt() bool {
	content, err := t.CapturePaneContent()
	if err != nil {
		return false
	}

	if strings.HasSuffix(t.program, ProgramClaude) {
		if strings.Contains(content, "Do you trust the files in this folder?") ||
			strings.Contains(content, "new MCP server") {
			if err := t.TapEnter(); err != nil {
				log.ErrorLog.Printf("could not tap enter on trust/MCP screen: %v", err)
			}
			return true
		}
	} else {
		if strings.Contains(content, "Open documentation url for more info") {
			if err := t.TapDAndEnter(); err != nil {
				log.ErrorLog.Printf("could not tap enter on trust screen: %v", err)
			}
			return true
		}
	}
	return false
}

// Restore attaches to an existing session and restores the window size
func (t *TmuxSession) Restore() error {
	// attach-session against a missing session still forks a process successfully, so the
	// PTY start below would report no error while leaving us attached to nothing. Check
	// first so callers can tell "session is gone" apart from "PTY failed".
	if !t.DoesSessionExist() {
		return ErrSessionNotFound
	}

	ptmx, err := t.ptyFactory.Start(tmuxCmd("attach-session", "-t", t.sanitizedName))
	if err != nil {
		return fmt.Errorf("error opening PTY: %w", err)
	}
	t.ptmx = ptmx
	t.monitor = newStatusMonitor()
	return nil
}

type statusMonitor struct {
	// Store hashes to save memory.
	prevOutputHash []byte
}

func newStatusMonitor() *statusMonitor {
	return &statusMonitor{}
}

// hash hashes the string.
func (m *statusMonitor) hash(s string) []byte {
	h := sha256.New()
	// TODO: this allocation sucks since the string is probably large. Ideally, we hash the string directly.
	h.Write([]byte(s))
	return h.Sum(nil)
}

// TapEnter sends an enter keystroke to the tmux pane.
func (t *TmuxSession) TapEnter() error {
	_, err := t.ptmx.Write([]byte{0x0D})
	if err != nil {
		return fmt.Errorf("error sending enter keystroke to PTY: %w", err)
	}
	return nil
}

// TapDAndEnter sends 'D' followed by an enter keystroke to the tmux pane.
func (t *TmuxSession) TapDAndEnter() error {
	_, err := t.ptmx.Write([]byte{0x44, 0x0D})
	if err != nil {
		return fmt.Errorf("error sending enter keystroke to PTY: %w", err)
	}
	return nil
}

func (t *TmuxSession) SendKeys(keys string) error {
	_, err := t.ptmx.Write([]byte(keys))
	return err
}

// HasUpdated checks if the tmux pane content has changed since the last tick. It also returns true if
// the tmux pane has a prompt for aider or claude code.
func (t *TmuxSession) HasUpdated() (updated bool, hasPrompt bool) {
	content, err := t.CapturePaneContent()
	if err != nil {
		log.ErrorLog.Printf("error capturing pane content in status monitor: %v", err)
		return false, false
	}

	// Only set hasPrompt for claude and aider. Use these strings to check for a prompt.
	if t.program == ProgramClaude {
		hasPrompt = strings.Contains(content, "No, and tell Claude what to do differently")
	} else if strings.HasPrefix(t.program, ProgramAider) {
		hasPrompt = strings.Contains(content, "(Y)es/(N)o/(D)on't ask again")
	} else if strings.HasPrefix(t.program, ProgramGemini) {
		hasPrompt = strings.Contains(content, "Yes, allow once")
	}

	if !bytes.Equal(t.monitor.hash(content), t.monitor.prevOutputHash) {
		t.monitor.prevOutputHash = t.monitor.hash(content)
		return true, hasPrompt
	}
	return false, hasPrompt
}

// isDetachByte reports whether a lone byte read from stdin (nr == 1) is one
// of the two keys that end an attach: Ctrl-q (0x11) or Ctrl-] (0x1d, GS). A
// byte matching one of these values inside a longer read (nr != 1) is
// ordinary input being forwarded to the pane, not a detach request.
func isDetachByte(nr int, b byte) bool {
	return nr == 1 && (b == 17 || b == 29)
}

// runAttachCopyLoop copies the attached pty's output to dst until it returns
// EOF or any other read error, then reports how the attach ended: nil when
// ctx is already cancelled (Detach is already in flight and owns teardown),
// ErrSessionEnded otherwise - the pty went away before a detach was
// requested. Split out from the Attach goroutine so the outcome can be
// exercised directly against a fake src, with no real pty or stdin involved.
func (t *TmuxSession) runAttachCopyLoop(ctx context.Context, dst io.Writer, src io.Reader) error {
	_, _ = io.Copy(dst, src)
	select {
	case <-ctx.Done():
		return nil
	default:
		return ErrSessionEnded
	}
}

// LastAttachOutcome reports how the most recent Attach ended, once the
// channel it returned has closed: nil for a real Ctrl-Q/Ctrl-] detach,
// ErrSessionEnded when the program running inside tmux exited on its own.
// Reading before the channel closes is a race; the channel close is the
// synchronization point this relies on.
func (t *TmuxSession) LastAttachOutcome() error {
	return t.attachOutcome
}

func (t *TmuxSession) Attach() (chan struct{}, error) {
	// Captured locally and returned from that copy, never by re-reading
	// t.attachCh at the end of this function: board #317's ended-without-
	// detach path can now finish (DetachSafely, nilling t.attachCh) before
	// this function itself reaches its own return statement, when the pty
	// EOFs about as fast as a goroutine can be scheduled - a data race
	// between that write and a read of the struct field here.
	ch := make(chan struct{})
	t.attachCh = ch
	t.attachOutcome = nil

	// Put the real terminal into raw mode for the duration of the attach -
	// see stdinRawState's own comment for why this loop needs it regardless
	// of what mode the terminal was already in. Mirrors main.go's
	// clarityAttachCmd, which does the same MakeRaw/Restore pairing around
	// its own (non-bubbletea) call to Instance.Attach.
	if term.IsTerminal(int(os.Stdin.Fd())) {
		if oldState, err := term.MakeRaw(int(os.Stdin.Fd())); err != nil {
			log.WarningLog.Printf("could not set stdin to raw mode for attach: %v", err)
		} else {
			t.stdinRawState = oldState
		}
	}

	t.wg = &sync.WaitGroup{}
	t.wg.Add(1)
	t.ctx, t.cancel = context.WithCancel(context.Background())

	// monitorWindowSize adds its own two goroutines to t.wg before returning
	// (it starts them, doesn't wait for them). Called here, before either
	// goroutine below is spawned, so t.wg's count is never at risk of
	// reaching zero prematurely: the copy-loop goroutine below can (board
	// #317) finish and hand off to DetachSafely - which waits on t.wg and
	// then nils t.ctx/t.wg - the moment its own io.Copy returns, which can
	// be near-instant against a pty that's already gone. Calling
	// monitorWindowSize after spawning that goroutine would race its
	// t.wg.Add(2) against DetachSafely's t.wg.Wait()/nil-out.
	t.monitorWindowSize()

	// The first goroutine should terminate when the ptmx is closed. We use the
	// waitgroup to wait for it to finish.
	// The 2nd one returns when you press escape to Detach. It doesn't need to be
	// in the waitgroup because is the goroutine doing the Detaching; it waits for
	// all the other ones.
	go func() {
		defer t.wg.Done()
		// When io.Copy returns, the connection was closed - either a normal
		// detach (Detach already cancelled the context and owns the rest of
		// teardown) or the program running inside tmux exited on its own
		// (Ctrl-D, `exit`, a crash: the context is still live). Board #317:
		// this used to print a red stderr line and stop, leaving attachCh
		// unclosed forever in the second case - nothing else was ever going
		// to call Detach, so the caller's <-ch blocked and the cockpit never
		// came back. Record the outcome instead, and finish the teardown
		// itself when nobody else will.
		outcome := t.runAttachCopyLoop(t.ctx, os.Stdout, t.ptmx)
		t.attachOutcome = outcome
		if outcome != nil {
			// DetachSafely closes attachCh, cancels the context and waits on
			// t.wg - run it in its own goroutine so this one's deferred
			// wg.Done() above fires first; calling it inline here would
			// deadlock DetachSafely's wg.Wait() against this very goroutine.
			go func() {
				if err := t.DetachSafely(); err != nil {
					log.WarningLog.Printf("cleanup after attach ended without detach: %v", err)
				}
			}()
		}
	}()

	// Board #325: record this cycle's live state for the persistent stdin
	// reader to consult, then make sure a reader exists - starting one only
	// if none is already alive. Order matters: the state is published
	// before startStdinReader runs so a reader left over from a leaked
	// cycle (still blocked in Read, never joined by a second one - see
	// startStdinReader) sees the new target the instant it next wakes,
	// rather than a window where it could wake to stale state.
	t.stdinMu.Lock()
	t.stdinGen++
	t.stdinLive = true
	t.stdinTarget = t.ptmx
	t.stdinNukeUntil = time.Now().Add(50 * time.Millisecond)
	t.stdinMu.Unlock()

	t.startStdinReader()

	return ch, nil
}

// startStdinReader launches the persistent stdin-forwarding goroutine the
// first time any Attach call needs one, and is a no-op on every later call
// while that goroutine is still alive (board #325). Without the CAS guard,
// a cycle that ends without a detach byte (board #317: the program inside
// tmux exits on its own) leaves its reader permanently blocked in
// os.Stdin.Read - unable to be cancelled - and the next Attach would spawn
// a second one, stacking readers that race the next keystroke against each
// other; whichever wins can forward it to the wrong pty or call Detach on
// an attach it was never part of. Reusing the stuck reader instead is safe
// because stdinForwardLoop always re-checks stdinLive/stdinTarget against
// the moment a read actually returns, never against whatever was live when
// the read began.
func (t *TmuxSession) startStdinReader() {
	if !atomic.CompareAndSwapInt32(&t.stdinReaderAlive, 0, 1) {
		return
	}
	atomic.AddInt32(&t.stdinReaderStarts, 1)

	reader := io.Reader(os.Stdin)
	if t.stdinReader != nil {
		reader = t.stdinReader
	}
	go t.stdinForwardLoop(reader)
}

// stdinForwardLoop is the persistent stdin-forwarding goroutine body
// (board #325). It returns only on a genuine EOF from src (real stdin
// closing - practically never in production, used by tests to end the
// goroutine deterministically) or right after it processes a real detach
// byte for an attach that was live when the byte was checked - the same
// bounded lifetime the pre-fix code had for a normal Ctrl-Q/Ctrl-] cycle,
// so a well-behaved attach/detach still leaves no background reader racing
// bubbletea's own stdin reads between cycles. Every other outcome (no
// attach live right now, or inside the current attach's nuke window) loops
// back to read the next byte instead of returning, because that is what
// lets a reader stuck from a leaked cycle (see startStdinReader) pick up
// the NEXT attach's live state instead of vanishing along with the leak.
func (t *TmuxSession) stdinForwardLoop(src io.Reader) {
	defer atomic.StoreInt32(&t.stdinReaderAlive, 0)

	buf := make([]byte, 32)
	for {
		nr, err := src.Read(buf)
		if err != nil {
			if err == io.EOF {
				return
			}
			continue
		}

		t.stdinMu.Lock()
		gen := t.stdinGen
		live := t.stdinLive
		target := t.stdinTarget
		nukeUntil := t.stdinNukeUntil
		t.stdinMu.Unlock()

		if !live {
			// No attach is live right now: this byte belongs to no pty -
			// either it arrived in the gap between two attaches, or its
			// owning attach already ended. Forwarding it to whatever
			// t.ptmx happens to be current is exactly the board #325 bug;
			// drop it and keep reading so a later Attach can still reuse
			// this same goroutine.
			t.notifyStdinProcessed()
			continue
		}

		// Nuke the first bytes of this attach cycle, up to 64, to prevent
		// tmux from reading it. When we attach, there tends to be terminal
		// control sequences like ?[?62c0;95;0c or ]10;rgb:f8f8f8. The
		// control sequences depend on the terminal (warp vs iterm). We
		// should use regex ideally but this works well for now. Log this
		// for debugging.
		//
		// There seems to always be control characters, but I think it's
		// possible for there not to be. The heuristic here can be: if
		// there's characters within 50ms of THIS attach starting, assume
		// they are control characters and nuke them.
		if time.Now().Before(nukeUntil) {
			log.InfoLog.Printf("nuked first stdin (gen %d): %s", gen, buf[:nr])
			t.notifyStdinProcessed()
			continue
		}

		// Check for the detach key: Ctrl+] (GS, ASCII 29) or Ctrl+q (ASCII 17).
		// Ctrl-] is offered first because it reaches the terminal in every
		// editor; Ctrl-q alone doesn't - VS Code's integrated terminal binds
		// it to Quick Open View before it ever gets to us.
		if isDetachByte(nr, buf[0]) {
			t.Detach()
			t.notifyStdinProcessed()
			return
		}

		// Forward other input to the pty live right now - never t.ptmx read
		// fresh, which could already belong to a different attach cycle by
		// the time this line runs.
		_, _ = target.Write(buf[:nr])
		t.notifyStdinProcessed()
	}
}

// notifyStdinProcessed signals stdinProcessed, if a test set one, after
// stdinForwardLoop has fully handled one read. Non-blocking so a test that
// isn't currently receiving never stalls the loop.
func (t *TmuxSession) notifyStdinProcessed() {
	if t.stdinProcessed == nil {
		return
	}
	select {
	case t.stdinProcessed <- struct{}{}:
	default:
	}
}

// restoreStdinRawState reverses whatever Attach's own term.MakeRaw call did
// to the real terminal, if anything (stdinRawState is nil when stdin wasn't
// a terminal, or MakeRaw itself failed). Called from both detach paths
// below so the terminal is never left in Attach's raw state past Detach.
func (t *TmuxSession) restoreStdinRawState() {
	if t.stdinRawState == nil {
		return
	}
	if err := term.Restore(int(os.Stdin.Fd()), t.stdinRawState); err != nil {
		log.WarningLog.Printf("could not restore stdin terminal state after detach: %v", err)
	}
	t.stdinRawState = nil
}

// DetachSafely disconnects from the current tmux session without panicking
func (t *TmuxSession) DetachSafely() error {
	// Only detach if we're actually attached
	if t.attachCh == nil {
		return nil // Already detached
	}

	// Board #325: clear the live target as the very first thing, before
	// closing anything below - the stdin-forwarding goroutine must never
	// observe stdinLive still true once this teardown has begun, or it
	// could write to a ptmx that's about to be closed under it.
	t.stdinMu.Lock()
	t.stdinLive = false
	t.stdinTarget = nil
	t.stdinMu.Unlock()

	var errs []error

	defer t.restoreStdinRawState()

	// Close the attached pty session.
	if t.ptmx != nil {
		if err := t.ptmx.Close(); err != nil {
			errs = append(errs, fmt.Errorf("error closing attach pty session: %w", err))
		}
		t.ptmx = nil
	}

	// Clean up attach state
	if t.attachCh != nil {
		close(t.attachCh)
		t.attachCh = nil
	}

	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}

	if t.wg != nil {
		t.wg.Wait()
		t.wg = nil
	}

	t.ctx = nil

	if len(errs) > 0 {
		return fmt.Errorf("errors during detach: %v", errs)
	}
	return nil
}

// Detach disconnects from the current tmux session. It panics if detaching fails. At the moment, there's no
// way to recover from a failed detach.
func (t *TmuxSession) Detach() {
	// Board #317: nothing to detach - DetachSafely already tore this attach
	// cycle down (t.ptmx is nil, same invariant DetachSafely itself checks
	// via t.attachCh). Without this guard, t.ptmx.Close() below returns
	// os.ErrInvalid on a nil receiver and this function panics on a normal,
	// already-finished cycle - reachable from the stdin-reading goroutine's
	// own leftover Ctrl-Q/Ctrl-] handling (see the deferred close below).
	if t.ptmx == nil {
		return
	}

	// Board #325: same reasoning as DetachSafely's own early clear - done
	// before anything below closes or reassigns t.ptmx.
	t.stdinMu.Lock()
	t.stdinLive = false
	t.stdinTarget = nil
	t.stdinMu.Unlock()

	// TODO: control flow is a bit messy here. If there's an error,
	// I'm not sure if we get into a bad state. Needs testing.
	defer func() {
		// Board #317: attachCh can already be nil here - the stdin-reading
		// goroutine that calls Detach on a Ctrl-Q/Ctrl-] byte has no way to
		// be cancelled (os.Stdin.Read is a blocking syscall, not a select),
		// so it outlives an ended-without-detach cycle's own DetachSafely
		// teardown; a keystroke it reads afterwards (meant for the NEXT
		// attach on this same TmuxSession) reaches this same deferred close
		// on an already-nil channel - close(nil) panics unconditionally, so
		// this is checked exactly like DetachSafely already checks it.
		if t.attachCh != nil {
			close(t.attachCh)
		}
		t.attachCh = nil
		t.cancel = nil
		t.ctx = nil
		t.wg = nil
	}()
	defer t.restoreStdinRawState()

	// Close the attached pty session.
	err := t.ptmx.Close()
	if err != nil {
		// This is a fatal error. We can't detach if we can't close the PTY. It's better to just panic and have the
		// user re-invoke the program than to ruin their terminal pane.
		msg := fmt.Sprintf("error closing attach pty session: %v", err)
		log.ErrorLog.Println(msg)
		panic(msg)
	}
	// Attach goroutines should die on EOF due to the ptmx closing. Call
	// t.Restore to set a new t.ptmx.
	if err = t.Restore(); err != nil {
		// This is a fatal error. Our invariant that a started TmuxSession always has a valid ptmx is violated.
		msg := fmt.Sprintf("error closing attach pty session: %v", err)
		log.ErrorLog.Println(msg)
		panic(msg)
	}

	// Cancel goroutines created by Attach.
	t.cancel()
	t.wg.Wait()
}

// Close terminates the tmux session and cleans up resources
func (t *TmuxSession) Close() error {
	var errs []error

	if t.ptmx != nil {
		if err := t.ptmx.Close(); err != nil {
			errs = append(errs, fmt.Errorf("error closing PTY: %w", err))
		}
		t.ptmx = nil
	}

	cmd := tmuxCmd("kill-session", "-t", t.sanitizedName)
	if err := t.cmdExec.Run(cmd); err != nil {
		errs = append(errs, fmt.Errorf("error killing tmux session: %w", err))
	}

	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}

	errMsg := "multiple errors occurred during cleanup:"
	for _, err := range errs {
		errMsg += "\n  - " + err.Error()
	}
	return errors.New(errMsg)
}

// SetDetachedSize set the width and height of the session while detached. This makes the
// tmux output conform to the specified shape.
func (t *TmuxSession) SetDetachedSize(width, height int) error {
	return t.updateWindowSize(width, height)
}

// updateWindowSize updates the window size of the PTY.
func (t *TmuxSession) updateWindowSize(cols, rows int) error {
	return pty.Setsize(t.ptmx, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
		X:    0,
		Y:    0,
	})
}

func (t *TmuxSession) DoesSessionExist() bool {
	// Using "-t name" does a prefix match, which is wrong. `-t=` does an exact match.
	existsCmd := tmuxCmd("has-session", fmt.Sprintf("-t=%s", t.sanitizedName))
	return t.cmdExec.Run(existsCmd) == nil
}

// SanitizedName returns the tmux session name this TmuxSession was built
// for - the same name DoesSessionExist checks and ListSessionNames' own
// result set is keyed by (slice 17c item 2), so a caller can answer
// liveness for many instances against ONE batched name set without asking
// each TmuxSession to run its own has-session call.
func (t *TmuxSession) SanitizedName() string {
	return t.sanitizedName
}

// SanitizeName exposes toClaudeSquadTmuxName for a caller that has a raw
// instance title but no TmuxSession yet (slice 17c item 2) - the exact
// same sanitisation NewTmuxSession applies, so a name looked up this way
// always matches SanitizedName's own result for the same title.
func SanitizeName(title string) string {
	return toClaudeSquadTmuxName(title)
}

// ListSessionNames answers every tmux session currently on the socket with
// ONE call (`tmux list-sessions -F "#S"`) - the batched replacement for a
// has-session call per lane (slice 17c item 2: "one tmux call per pass,
// not per row"). "no server running" (tmux ls's own exit 1 with nothing to
// list, the same case CleanupSessions below already treats as empty, not
// an error) answers an empty set, never an error - a freshly booted
// process with no tmux server running yet has zero live sessions, which is
// simply true, not a failure to find out.
func ListSessionNames(cmdExec cmd.Executor) (map[string]bool, error) {
	listCmd := tmuxCmd("list-sessions", "-F", "#S")
	output, err := cmdExec.Output(listCmd)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return map[string]bool{}, nil
		}
		return nil, fmt.Errorf("failed to list tmux sessions: %w", err)
	}

	names := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimRight(string(output), "\n"), "\n") {
		if line == "" {
			continue
		}
		names[line] = true
	}
	return names, nil
}

// CapturePaneContent captures the content of the tmux pane
func (t *TmuxSession) CapturePaneContent() (string, error) {
	// Add -e flag to preserve escape sequences (ANSI color codes)
	cmd := tmuxCmd("capture-pane", "-p", "-e", "-J", "-t", t.sanitizedName)
	output, err := t.cmdExec.Output(cmd)
	if err != nil {
		return "", fmt.Errorf("error capturing pane content: %v", err)
	}
	return string(output), nil
}

// CapturePaneContentWithOptions captures the pane content with additional options
// start and end specify the starting and ending line numbers (use "-" for the start/end of history)
func (t *TmuxSession) CapturePaneContentWithOptions(start, end string) (string, error) {
	// Add -e flag to preserve escape sequences (ANSI color codes)
	cmd := tmuxCmd("capture-pane", "-p", "-e", "-J", "-S", start, "-E", end, "-t", t.sanitizedName)
	output, err := t.cmdExec.Output(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to capture tmux pane content with options: %v", err)
	}
	return string(output), nil
}

// CleanupSessions kills all tmux sessions that start with "session-"
func CleanupSessions(cmdExec cmd.Executor) error {
	// First try to list sessions
	cmd := tmuxCmd("ls")
	output, err := cmdExec.Output(cmd)

	// If there's an error and it's because no server is running, that's fine
	// Exit code 1 typically means no sessions exist
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil // No sessions to clean up
		}
		return fmt.Errorf("failed to list tmux sessions: %v", err)
	}

	re := regexp.MustCompile(fmt.Sprintf(`%s.*:`, TmuxPrefix))
	matches := re.FindAllString(string(output), -1)
	for i, match := range matches {
		matches[i] = match[:strings.Index(match, ":")]
	}

	for _, match := range matches {
		log.InfoLog.Printf("cleaning up session: %s", match)
		if err := cmdExec.Run(tmuxCmd("kill-session", "-t", match)); err != nil {
			return fmt.Errorf("failed to kill tmux session %s: %v", match, err)
		}
	}
	return nil
}
