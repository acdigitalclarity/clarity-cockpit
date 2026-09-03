//go:build !windows

package tmux

import (
	"claude-squad/log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/term"
)

// monitorWindowSize monitors and handles window resize events while attached.
func (t *TmuxSession) monitorWindowSize() {
	// Board #317: captured once, read-only from here on - never t.ctx
	// directly inside the closures below. The debounce goroutine's own
	// resizeTimer (time.AfterFunc) is NOT covered by t.wg, so DetachSafely
	// can already have nilled t.ctx by the time that timer fires, up to
	// 50ms later; the ended-without-detach path (tmux.go's Attach) can run
	// DetachSafely within a few milliseconds of this very call when the
	// attached program has already exited, so that race is not academic -
	// it panicked on a live attach before this local capture (leg report).
	ctx := t.ctx
	winchChan := make(chan os.Signal, 1)
	signal.Notify(winchChan, syscall.SIGWINCH)
	// Send initial SIGWINCH to trigger the first resize
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGWINCH)

	everyN := log.NewEvery(60 * time.Second)

	doUpdate := func() {
		// Use the current terminal height and width.
		cols, rows, err := term.GetSize(int(os.Stdin.Fd()))
		if err != nil {
			if everyN.ShouldLog() {
				log.ErrorLog.Printf("failed to update window size: %v", err)
			}
		} else {
			if err := t.updateWindowSize(cols, rows); err != nil {
				if everyN.ShouldLog() {
					log.ErrorLog.Printf("failed to update window size: %v", err)
				}
			}
		}
	}
	// Do one at the end of the function to set the initial size.
	defer doUpdate()

	// Debounce resize events
	t.wg.Add(2)
	debouncedWinch := make(chan os.Signal, 1)
	go func() {
		defer t.wg.Done()
		var resizeTimer *time.Timer
		for {
			select {
			case <-ctx.Done():
				return
			case <-winchChan:
				if resizeTimer != nil {
					resizeTimer.Stop()
				}
				resizeTimer = time.AfterFunc(50*time.Millisecond, func() {
					select {
					case debouncedWinch <- syscall.SIGWINCH:
					case <-ctx.Done():
					}
				})
			}
		}
	}()
	go func() {
		defer t.wg.Done()
		defer signal.Stop(winchChan)
		// Handle resize events
		for {
			select {
			case <-ctx.Done():
				return
			case <-debouncedWinch:
				doUpdate()
			}
		}
	}()
}
