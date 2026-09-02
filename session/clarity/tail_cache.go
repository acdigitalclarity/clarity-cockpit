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
// path, plus the file stat and maxTurns it was computed from.
type cachedTail struct {
	modTime  time.Time
	size     int64
	maxTurns int
	tail     LaneTail
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
// read, OR the cached entry was computed with fewer turns than maxTurns now
// asks for (a caller that needs more history than the last one, e.g. the
// Session pane's 40 versus the list rows' bare default, must not be served
// a narrower cached slice). maxTurns <= 0 means ReadLaneTail's own
// DefaultTailTurns, same as passing it straight through. now is passed
// through to ReadLaneTail's own age-based classification, not used for
// cache freshness itself.
func (c *LaneTailCache) Get(transcriptPath string, maxTurns int, now time.Time) (LaneTail, error) {
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
	if ok && entry.modTime.Equal(info.ModTime()) && entry.size == info.Size() && entry.maxTurns >= wantTurns {
		return entry.tail, nil
	}

	tail, err := ReadLaneTail(transcriptPath, 0, maxTurns, now)
	if err != nil {
		return LaneTail{}, err
	}

	c.mu.Lock()
	c.entries[transcriptPath] = cachedTail{modTime: info.ModTime(), size: info.Size(), maxTurns: wantTurns, tail: tail}
	c.mu.Unlock()
	return tail, nil
}
