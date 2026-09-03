// Package clarity: the b-key bank-and-close flow's own pure helpers (slice
// 18, ANSWER-AND-BANK-SPEC.md item 8) - the fixed line the key sends,
// verbatim, and the watcher that spots the CONTINUATION file it produces.
package clarity

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BankLine is the standard bank-and-close instruction b sends, verbatim
// (ANSWER-AND-BANK-SPEC.md "Exact strings") - never generated, never
// edited.
const BankLine = "bank state now: write the continuation from cells, then stop"

// FindContinuationFile scans dir (non-recursive - "Bank watch": the lane's
// own folder, one level) for CONTINUATION-*.md files with an mtime strictly
// after `after`, returning the newest match's own absolute path. ok is
// false when dir cannot be read or nothing matches yet - a caller (app.go's
// feed-tick poll) keeps watching rather than treating either as an error.
func FindContinuationFile(dir string, after time.Time) (path string, ok bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	var bestName string
	var bestMod time.Time
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "CONTINUATION-") || !strings.HasSuffix(name, ".md") {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.ModTime().After(after) {
			continue
		}
		if bestName == "" || info.ModTime().After(bestMod) {
			bestName, bestMod = name, info.ModTime()
		}
	}
	if bestName == "" {
		return "", false
	}
	full := filepath.Join(dir, bestName)
	if abs, err := filepath.Abs(full); err == nil {
		return abs, true
	}
	return full, true
}
