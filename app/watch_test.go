package app

import (
	"claude-squad/config"
	"claude-squad/session"
	"claude-squad/session/clarity"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeTranscriptWatcher is the injectable transcriptWatcher a test drives
// without ever touching the filesystem or a real fsnotify.Watcher - the
// brief's own "watcher stubbed with an injectable interface" requirement.
type fakeTranscriptWatcher struct {
	mu      sync.Mutex
	watched []string // every path handed to Watch, in call order
	events  chan struct{}
	closed  bool
}

func newFakeTranscriptWatcher() *fakeTranscriptWatcher {
	return &fakeTranscriptWatcher{events: make(chan struct{}, 4)}
}

func (f *fakeTranscriptWatcher) Watch(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.watched = append(f.watched, path)
	return nil
}

func (f *fakeTranscriptWatcher) Events() <-chan struct{} { return f.events }

func (f *fakeTranscriptWatcher) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.events)
	}
	return nil
}

// fire simulates one coalesced fsnotify write event arriving.
func (f *fakeTranscriptWatcher) fire() {
	f.events <- struct{}{}
}

func (f *fakeTranscriptWatcher) lastWatched() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.watched) == 0 {
		return ""
	}
	return f.watched[len(f.watched)-1]
}

func (f *fakeTranscriptWatcher) watchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.watched)
}

// TestTranscriptWatch_RetargetsOnSelectionChange is rule 3's own "the watch
// moves when the selection changes": retargetTranscriptWatch must re-Watch
// the newly selected lane's own transcript, and a call that resolves to the
// SAME path already watched must be a no-op (never re-Watch the identical
// path on every tick).
func TestTranscriptWatch_RetargetsOnSelectionChange(t *testing.T) {
	root := t.TempDir()
	t.Setenv(clarity.ClaudeProjectsRootEnvVar, root)

	h := newComposerTestHome(t)
	now := time.Now()
	a := writeTrackedLaneFixture(t, root, "lane-a", now.Add(-time.Minute))
	b := writeTrackedLaneFixture(t, root, "lane-b", now.Add(-time.Minute))
	h.list.AddInstance(a)()
	h.list.AddInstance(b)()

	fake := newFakeTranscriptWatcher()
	h.transcriptWatcher = fake

	require.Same(t, a, h.list.GetSelectedInstance(), "test setup: lane-a must be selected first")
	cmd := h.retargetTranscriptWatch()
	require.NotNil(t, cmd, "a resolvable transcript must arm the watch loop")
	pathA := fake.lastWatched()
	require.NotEmpty(t, pathA)
	genAfterA := h.transcriptWatchGen

	// Calling it again with the SAME selection must not re-Watch.
	cmd = h.retargetTranscriptWatch()
	require.Nil(t, cmd, "retargeting to the same path is a no-op")
	require.Equal(t, 1, fake.watchCount(), "the same path must never be re-Watched")
	require.Equal(t, genAfterA, h.transcriptWatchGen)

	h.list.Down()
	require.Same(t, b, h.list.GetSelectedInstance(), "test setup: lane-b must now be selected")
	cmd = h.retargetTranscriptWatch()
	require.NotNil(t, cmd)
	require.Equal(t, 2, fake.watchCount())
	require.NotEqual(t, pathA, fake.lastWatched(), "the watch must move to lane-b's own transcript")
	require.Greater(t, h.transcriptWatchGen, genAfterA, "generation must bump so lane-a's own stale event is discarded")
}

// TestTranscriptWatch_EventTriggersCacheRead is the brief's own named test:
// an event from the (fake, injected) watcher must, after the debounce,
// re-read the selected lane's transcript through the cache exactly as the
// session tick does - proven here by writing a SECOND, different fixture
// file after the initial retarget and confirming the pane picks it up only
// once the debounce message with the matching generation is processed.
func TestTranscriptWatch_EventTriggersCacheRead(t *testing.T) {
	root := t.TempDir()
	t.Setenv(clarity.ClaudeProjectsRootEnvVar, root)

	h := newComposerTestHome(t)
	now := time.Now()
	inst := writeTrackedLaneFixture(t, root, "watched-lane", now.Add(-time.Minute))
	h.list.AddInstance(inst)()

	fake := newFakeTranscriptWatcher()
	h.transcriptWatcher = fake
	cmd := h.retargetTranscriptWatch()
	require.NotNil(t, cmd)
	gen := h.transcriptWatchGen

	// The lane's own transcript changes on disk (a real append) BEFORE the
	// fsnotify event is delivered - exactly the real ordering.
	openLaneTranscript(t, root, inst.Path, now)

	fake.fire()
	msg := cmd()
	changed, ok := msg.(transcriptChangedMsg)
	require.True(t, ok, "the armed watch cmd must report a transcriptChangedMsg once the fake fires")
	require.Equal(t, gen, changed.gen)

	_, followUp := h.Update(changed)
	require.NotNil(t, followUp, "a transcriptChangedMsg must re-arm the watch and schedule its own debounce")
	require.Equal(t, uint64(1), h.transcriptDebounceGen)

	// The debounce message itself performs the read - never the raw event.
	require.NotContains(t, h.tabbedWindow.String(), "still working on it",
		"test setup: the pane must not already reflect the new content before the debounce fires")

	_, cmd2 := h.Update(transcriptDebounceMsg{gen: h.transcriptDebounceGen})
	require.Nil(t, cmd2, "a debounce read self-terminates, it does not reschedule itself")
	require.Contains(t, h.tabbedWindow.String(), "still working on it",
		"the debounce message must trigger the same cache read sessionTickMsg performs")
}

// TestTranscriptWatch_StaleGenerationIgnored is the guard rule 3's
// generation-tagging exists for: an event or a debounce carrying an OLDER
// generation than the watch's current one (the selection has since moved
// on) must never trigger a read.
func TestTranscriptWatch_StaleGenerationIgnored(t *testing.T) {
	root := t.TempDir()
	t.Setenv(clarity.ClaudeProjectsRootEnvVar, root)

	h := newComposerTestHome(t)
	now := time.Now()
	inst := writeTrackedLaneFixture(t, root, "watched-lane", now.Add(-time.Minute))
	h.list.AddInstance(inst)()

	fake := newFakeTranscriptWatcher()
	h.transcriptWatcher = fake
	require.NotNil(t, h.retargetTranscriptWatch())
	staleGen := h.transcriptWatchGen

	// Selection moves on (or off), bumping the generation past staleGen.
	h.transcriptWatchPath = "" // force the next retarget to actually retarget
	h.transcriptWatchGen++

	_, cmd := h.Update(transcriptChangedMsg{gen: staleGen})
	require.Nil(t, cmd, "a stale-generation event must be discarded, never re-armed or debounced")
	require.Equal(t, uint64(0), h.transcriptDebounceGen, "a stale event must never schedule a debounce read")

	openLaneTranscript(t, root, inst.Path, now)
	_, cmd = h.Update(transcriptDebounceMsg{gen: 999})
	require.Nil(t, cmd)
	require.NotContains(t, h.tabbedWindow.String(), "still working on it",
		"a stale-generation debounce must never perform the read")
}

// TestTranscriptWatch_ClosesOnQuit is rule 3's own "closes on quit": q must
// close the watcher through handleQuit, tearing down its background loop
// rather than leaking it past the program's own lifetime.
func TestTranscriptWatch_ClosesOnQuit(t *testing.T) {
	scratchHome := t.TempDir()
	t.Setenv("HOME", scratchHome) // config.GetConfigDir - never the real ~/.claude-squad store

	h := newComposerTestHome(t)
	state := config.LoadState()
	storage, err := session.NewStorage(state)
	require.NoError(t, err)
	h.storage = storage

	fake := newFakeTranscriptWatcher()
	h.transcriptWatcher = fake

	h.handleQuit()

	require.True(t, fake.closed, "handleQuit must close the transcript watcher")
}
