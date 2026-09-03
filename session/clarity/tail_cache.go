// Package clarity: this file memoizes ReadLaneTail per transcript path, so
// a caller that re-derives every lane's state on a fixed tick (app.go's
// feedTickMsg, every 3s) never re-parses a transcript that has not changed
// since the last tick.
package clarity

import (
	"os"
	"sync"
	"time"
)

// cachedTail is one LaneTailCache entry: the LaneTail last computed for a
// path, plus the file stat, maxTurns and sentAt it was computed from.
type cachedTail struct {
	modTime  time.Time
	size     int64
	maxTurns int
	// sentAt is the sentAt this entry was classified with (item 5, WAITING
	// HELD) - part of the freshness check below alongside modTime/size,
	// since it feeds ClassifyState directly (ReadLaneTail's own sentAt
	// argument) and a caller recording a fresh send must see the entry
	// invalidated even though the transcript FILE itself has not changed
	// (a cockpit-sent prompt earns no immediate write there).
	sentAt time.Time
	tail   LaneTail
}

// LaneTailCache memoizes ReadLaneTail per transcript path, keyed by the
// file's mtime and size (the brief's requirement) - a change to either
// invalidates the entry, an unchanged file returns the cached LaneTail
// without opening it again. Safe for concurrent use, though the current
// caller (app.go's single-threaded feed tick) never needs that.
type LaneTailCache struct {
	mu      sync.Mutex
	entries map[string]cachedTail
}

// NewLaneTailCache returns an empty cache.
func NewLaneTailCache() *LaneTailCache {
	return &LaneTailCache{entries: make(map[string]cachedTail)}
}

// Get returns the LaneTail for transcriptPath, re-reading it (via
// ReadLaneTail) only when the file's mtime or size differs from the last
// read, the cached entry was computed with fewer turns than maxTurns now
// asks for (a caller that needs more history than the last one, e.g. the
// Session pane's 40 versus the list rows' bare default, must not be served
// a narrower cached slice), OR sentAt differs from the value the cached
// entry was classified with (item 5, WAITING HELD - a caller recording a
// fresh cockpit send must see this take effect on the very next Get, not
// wait for the transcript file itself to change). maxTurns <= 0 means
// ReadLaneTail's own DefaultTailTurns, same as passing it straight through.
// now is passed through to ReadLaneTail's own age-based classification, not
// used for cache freshness itself. sentAt is passed straight through to
// ReadLaneTail's own sentAt argument - the zero value for a caller with
// nothing recorded for this lane.
func (c *LaneTailCache) Get(transcriptPath string, maxTurns int, now time.Time, sentAt time.Time) (LaneTail, error) {
	info, err := os.Stat(transcriptPath)
	if err != nil {
		return LaneTail{}, err
	}
	wantTurns := maxTurns
	if wantTurns <= 0 {
		wantTurns = DefaultTailTurns
	}

	c.mu.Lock()
	entry, ok := c.entries[transcriptPath]
	c.mu.Unlock()
	if ok && entry.modTime.Equal(info.ModTime()) && entry.size == info.Size() && entry.maxTurns >= wantTurns && entry.sentAt.Equal(sentAt) {
		return entry.tail, nil
	}

	tail, err := ReadLaneTail(transcriptPath, 0, maxTurns, now, sentAt)
	if err != nil {
		return LaneTail{}, err
	}

	c.mu.Lock()
	c.entries[transcriptPath] = cachedTail{modTime: info.ModTime(), size: info.Size(), maxTurns: wantTurns, sentAt: sentAt, tail: tail}
	c.mu.Unlock()
	return tail, nil
}
