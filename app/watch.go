package app

import (
	"claude-squad/log"
	"claude-squad/session/clarity"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"
)

// transcriptDebounceInterval is rule 3's own number, verbatim: a write
// event re-reads the selected lane's transcript through the cache after
// this debounce, not on every individual write syscall a program's own
// buffering may split one logical append into.
const transcriptDebounceInterval = 50 * time.Millisecond

// transcriptWatcher is the seam over fsnotify.Watcher (rule 3): watches
// exactly one path at a time, retargeted by Watch on every selection
// change. The real implementation (fsnotifyWatcher below) wraps
// *fsnotify.Watcher; tests inject a fake that fires events without
// touching the filesystem at all - the brief's own "watcher stubbed with
// an injectable interface" requirement.
type transcriptWatcher interface {
	// Watch starts watching path, replacing whatever this watcher was
	// previously watching (a watcher only ever tracks one path - the
	// SELECTED lane's own transcript).
	Watch(path string) error
	// Events fires once per coalesced write event fsnotify reports for the
	// current target. Never closed except by Close.
	Events() <-chan struct{}
	// Close stops watching and releases the underlying OS resources.
	Close() error
}

// fsnotifyWatcher is transcriptWatcher's real implementation.
type fsnotifyWatcher struct {
	w      *fsnotify.Watcher
	path   string
	events chan struct{}
}

// newFsnotifyWatcher opens one fsnotify.Watcher and starts its own
// coalescing loop (below) - the loop runs for the lifetime of the *home
// that owns it, torn down only by Close (handleQuit).
func newFsnotifyWatcher() (*fsnotifyWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	fw := &fsnotifyWatcher{w: w, events: make(chan struct{}, 1)}
	go fw.loop()
	return fw, nil
}

// loop forwards every Write/Create event fsnotify reports for the current
// target onto fw.events, coalescing a burst of several writes into one
// pending signal (a full channel is never blocked on - the reader only
// ever needs to know "something changed since it last looked", not how
// many times). Errors are logged, never fatal: the session/feed ticks
// still cover the lane even if the watch itself stops working.
func (fw *fsnotifyWatcher) loop() {
	for {
		select {
		case event, ok := <-fw.w.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			select {
			case fw.events <- struct{}{}:
			default:
			}
		case err, ok := <-fw.w.Errors:
			if !ok {
				return
			}
			log.WarningLog.Printf("transcript watch: %v", err)
		}
	}
}

// Watch retargets fw at path, removing its previous target first (Remove's
// own error on a path that was never added, or is already gone, is not
// worth surfacing - the Add below is what actually matters).
func (fw *fsnotifyWatcher) Watch(path string) error {
	if fw.path != "" {
		_ = fw.w.Remove(fw.path)
	}
	fw.path = path
	return fw.w.Add(path)
}

func (fw *fsnotifyWatcher) Events() <-chan struct{} { return fw.events }

func (fw *fsnotifyWatcher) Close() error { return fw.w.Close() }

// transcriptChangedMsg fires when the watched transcript's own fsnotify
// event arrives. gen ties it to the watch that produced it (bumped by
// retargetTranscriptWatch on every retarget) so a stale watcher's event -
// one that fired after the selection already moved on - is discarded
// rather than triggering a read for a lane no longer selected.
type transcriptChangedMsg struct{ gen uint64 }

// transcriptDebounceMsg fires transcriptDebounceInterval after a
// transcriptChangedMsg - the actual cache read only happens here, keyed by
// its own generation so a burst of several events within the debounce
// window collapses into exactly one read (the LATEST debounce fired wins;
// every earlier one is stale by the time it arrives).
type transcriptDebounceMsg struct{ gen uint64 }

// watchTranscriptCmd blocks on exactly one event from w, tagged with gen -
// the Update loop re-issues this after every event to keep listening (see
// the transcriptChangedMsg case in app.go's Update). A closed channel
// (Close called - selection moved to a lane with no transcript, or the app
// is quitting) ends the loop with no message at all.
func watchTranscriptCmd(w transcriptWatcher, gen uint64) tea.Cmd {
	return func() tea.Msg {
		if _, ok := <-w.Events(); !ok {
			return nil
		}
		return transcriptChangedMsg{gen: gen}
	}
}

// selectedTranscriptPath resolves the CURRENT selection's own transcript
// path exactly as selectedSessionInfo does ("" when nothing is selected, or
// nothing is resolvable yet) - the retarget's own input.
func (m *home) selectedTranscriptPath() string {
	if selected := m.list.GetSelectedInstance(); selected != nil {
		path, ok := clarity.NewestTranscript(selected.Path)
		if !ok {
			return ""
		}
		return path
	}
	if ext, ok := m.list.GetSelectedExternalLane(); ok {
		return ext.TranscriptPath
	}
	return ""
}

// retargetTranscriptWatch is rule 3's own "the watch moves when the
// selection changes": a no-op when the resolved path already matches what
// is currently watched (moving the cursor between two rows backed by the
// same lane, or a call that fires on every tick regardless of whether the
// selection actually moved). Otherwise it bumps transcriptWatchGen (so any
// event still in flight from the PREVIOUS target is recognised as stale by
// the Update cases above) and re-points the one shared watcher at the new
// path - lazily constructing it via newFsnotifyWatcher on first use. A
// construction or Add failure is logged and swallowed: the 500ms session
// tick and the 3s feed tick still cover the lane, just without the
// sub-150ms fsnotify path.
func (m *home) retargetTranscriptWatch() tea.Cmd {
	path := m.selectedTranscriptPath()
	if path == m.transcriptWatchPath {
		return nil
	}
	m.transcriptWatchPath = path
	m.transcriptWatchGen++
	if path == "" {
		return nil
	}
	if m.transcriptWatcher == nil {
		w, err := newFsnotifyWatcher()
		if err != nil {
			log.WarningLog.Printf("transcript watch: %v", err)
			return nil
		}
		m.transcriptWatcher = w
	}
	if err := m.transcriptWatcher.Watch(path); err != nil {
		log.WarningLog.Printf("transcript watch %s: %v", path, err)
		return nil
	}
	return watchTranscriptCmd(m.transcriptWatcher, m.transcriptWatchGen)
}
