package splash

import (
	"embed"
	"fmt"
	"image"
	_ "image/png"
	"math"
	"sync"

	"github.com/charmbracelet/harmonica"
)

// The rasterised Digital Clarity monogram, embedded so the cockpit binary
// stays self-contained (design/splash-80s/OUTRUN-2/OUTRUN2.go does the same
// with its own copy of the same source file,
// design/splash-logo/SPLASH-6/assets/mark-512.png).
//
//go:embed assets/mark-512.png
var assetsFS embed.FS

// markImg is loaded once at package init from the embedded asset. The
// asset is compiled into the binary, so a decode failure here is a build
// defect, not a runtime condition callers need to handle - the same
// reasoning template.Must uses for a parse error on a literal template.
var markImg = mustLoadMark()

func mustLoadMark() image.Image {
	f, err := assetsFS.Open("assets/mark-512.png")
	if err != nil {
		panic(fmt.Sprintf("splash: embedded mark-512.png missing: %v", err))
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		panic(fmt.Sprintf("splash: decode mark-512.png: %v", err))
	}
	return img
}

// -- cell aspect correction --------------------------------------------
// A terminal half-block cell (one text column x one half-row, since the
// upper-half-block glyph carries two independent colours) is not square.
// Ported verbatim from design/splash-80s/OUTRUN-2/OUTRUN2.go, whose own
// comment records the calibration this measurement came from (a 200x20
// grid of 'M' glyphs, headless-Chrome screenshot, bright-pixel bounding
// box): a character cell is ~8.425px wide, one text line ~13.85px tall,
// so a half-block cell is ~1.22x wider than tall. effMarkCols narrows how
// many of the sampled columns actually carry image data so the mapped
// patch renders square in physical pixels, keeping the source PNG's own
// square proportion.
const cellCharWidthPx = 8.425
const cellLineHeightPx = 13.85

func effMarkCols(markRows int) int {
	halfRows := float64(markRows * 2)
	halfRowPx := cellLineHeightPx / 2
	return int(math.Round(halfRows * halfRowPx / cellCharWidthPx))
}

// boxAvg: a box filter over the source image in normalised [0,1] UV space,
// so a 512px raster downsampled to a few dozen terminal cells stays a
// continuous gradient rather than speckling. Ported verbatim from
// OUTRUN2.go (itself SPLASH-6/SPLASH-6.go:283-306's own technique).
func boxAvg(img image.Image, u0, v0, u1, v1 float64) string {
	b := img.Bounds()
	w, h := float64(b.Dx()), float64(b.Dy())
	x0 := int(u0 * w)
	x1 := int(math.Ceil(u1 * w))
	y0 := int(v0 * h)
	y1 := int(math.Ceil(v1 * h))
	if x1 <= x0 {
		x1 = x0 + 1
	}
	if y1 <= y0 {
		y1 = y0 + 1
	}
	var sr, sg, sb, n float64
	for y := y0; y < y1 && y < b.Dy(); y++ {
		for x := x0; x < x1 && x < b.Dx(); x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			sr += float64(r >> 8)
			sg += float64(g >> 8)
			sb += float64(bl >> 8)
			n++
		}
	}
	if n == 0 {
		return spaceCadet
	}
	return fmt.Sprintf("#%02X%02X%02X", clampByte(sr/n), clampByte(sg/n), clampByte(sb/n))
}

// markSample: one box-averaged colour per (column, half-row) cell, plus
// each column's own occupancy footprint (topH/bottomH, in half-row units)
// - the silhouette height the equalizer bars rise toward. Ported verbatim
// from OUTRUN2.go.
type markSample struct {
	cols, halfRows int
	hexAt          [][]string
	topH, bottomH  []int
}

func sampleMark(img image.Image, cols, rows int) *markSample {
	halfRows := rows * 2
	ms := &markSample{cols: cols, halfRows: halfRows}
	ms.hexAt = make([][]string, cols)
	ms.topH = make([]int, cols)
	ms.bottomH = make([]int, cols)

	effCols := effMarkCols(rows)
	if effCols > cols {
		effCols = cols
	}
	marginLeft := (cols - effCols) / 2

	for c := 0; c < cols; c++ {
		ms.hexAt[c] = make([]string, halfRows)
		ce := c - marginLeft
		if ce < 0 || ce >= effCols {
			ms.topH[c] = -1
			ms.bottomH[c] = -1
			continue
		}
		u0 := float64(ce) / float64(effCols)
		u1 := float64(ce+1) / float64(effCols)
		top, bottom := -1, -1
		for hr := 0; hr < halfRows; hr++ {
			v0 := float64(hr) / float64(halfRows)
			v1 := float64(hr+1) / float64(halfRows)
			hex := boxAvg(img, u0, v0, u1, v1)
			ms.hexAt[c][hr] = hex
			if colorDist(hex, spaceCadet) > 18 {
				if top == -1 {
					top = hr
				}
				bottom = hr
			}
		}
		ms.topH[c] = top
		ms.bottomH[c] = bottom
	}
	return ms
}

// -- the equalizer: per-column envelope ----------------------------------
// Fast attack, slow decay, a per-column phase offset and decay-rate
// jitter, riding a shared ~110bpm beat clock. Ported verbatim from
// OUTRUN2.go.

const bpm = 110.0

func framesPerBeat() float64 { return 60.0 / bpm * float64(fps) } // ~13.09 frames

func hash2(a, b int) uint32 {
	h := uint32(a)*374761393 + uint32(b)*668265263
	h = (h ^ (h >> 13)) * 1274126177
	return h ^ (h >> 16)
}

func frac01(h uint32) float64 {
	return float64(h%100000) / 100000.0
}

func columnEnvelope(worldFrame float64, col int) float64 {
	fpb := framesPerBeat()
	h := hash2(col, 9176)
	attackJitter := (frac01(h) - 0.5) * 0.10 * fpb
	decayRate := 1.5 + frac01(h/97)*2.0
	ampJitter := 0.82 + frac01(h/131)*0.33
	microFreq := 2.6 + float64(h%7)*0.41
	microPhase := frac01(h/271) * 2 * math.Pi

	localF := worldFrame - attackJitter
	cyclePos := math.Mod(localF/fpb, 1.0)
	if cyclePos < 0 {
		cyclePos += 1
	}
	const attackFrac = 0.22
	floor := 0.20 + frac01(h/181)*0.40
	var env float64
	if cyclePos < attackFrac {
		env = cyclePos / attackFrac
	} else {
		d := (cyclePos - attackFrac) / (1 - attackFrac)
		env = floor + math.Pow(clamp01(1-d), decayRate)*(1-floor)
	}
	env *= ampJitter
	micro := 0.05 * env * math.Sin(worldFrame/float64(fps)*2*math.Pi*microFreq+microPhase)
	return clamp01(env + micro)
}

// globalBeatEnvelope is the "gaps closing on the downbeat" tuning: the
// brief asks that the one-column gaps between bars (see isBar below) close
// solid at the peak of the shared beat, so the mark reads as one unbroken
// glow on the downbeat, and falls back to distinct bars - the gap columns
// fully dark, never holding a per-column floor the way bar columns do -
// between beats. It is deliberately the SAME attack/decay shape as
// columnEnvelope with NO per-column jitter and no floor, so every gap
// column closes in lockstep exactly on the beat rather than drifting.
func globalBeatEnvelope(worldFrame float64) float64 {
	fpb := framesPerBeat()
	cyclePos := math.Mod(worldFrame/fpb, 1.0)
	if cyclePos < 0 {
		cyclePos += 1
	}
	const attackFrac = 0.22
	if cyclePos < attackFrac {
		return cyclePos / attackFrac
	}
	d := (cyclePos - attackFrac) / (1 - attackFrac)
	return math.Pow(clamp01(1-d), 2.2)
}

// ghostPull: how far a silhouette cell's colour is pulled toward
// space-cadet when no bar covers it. Ported verbatim from OUTRUN2.go.
const ghostPull = 0.80

// drawMarkEqualizer fills the mark's own silhouette from its own footprint
// bottom up toward that column's own silhouette height, in half-block
// cells. Ported from OUTRUN2.go with one change: gap columns (big==true,
// odd columns) no longer stay permanently dark - they follow
// globalBeatEnvelope so the logo blazes solid on the downbeat and reads as
// distinct bars between beats (the brief's second owed tuning).
func drawMarkEqualizer(c *canvas, ms *markSample, top, left, rows int, worldFrame float64, resting, isEntrance bool, riseGate float64, big bool) {
	final := make([][]string, ms.cols)
	for col := 0; col < ms.cols; col++ {
		final[col] = make([]string, ms.halfRows)
		topH, bottomH := ms.topH[col], ms.bottomH[col]
		if topH < 0 {
			continue
		}
		isBar := !big || col%2 == 0
		span := bottomH - topH + 1
		var env float64
		switch {
		case resting:
			env = 1.0 // the true resting/peak frame: every column at full brightness
		case isBar:
			env = columnEnvelope(worldFrame, col)
			if isEntrance {
				env *= riseGate
			}
		default:
			env = globalBeatEnvelope(worldFrame)
			if isEntrance {
				env *= riseGate
			}
		}
		barLen := int(math.Round(env * float64(span)))
		if barLen < 0 {
			barLen = 0
		}
		if barLen > span {
			barLen = span
		}
		barTop := bottomH - barLen + 1
		for hr := topH; hr <= bottomH; hr++ {
			raw := ms.hexAt[col][hr]
			if hr >= barTop {
				final[col][hr] = raw
			} else {
				final[col][hr] = lerpHex(raw, spaceCadet, ghostPull)
			}
		}
	}
	for r := 0; r < rows; r++ {
		upperHR := 2 * r
		lowerHR := 2*r + 1
		for col := 0; col < ms.cols; col++ {
			up := final[col][upperHR]
			lo := ""
			if lowerHR < ms.halfRows {
				lo = final[col][lowerHR]
			}
			if up == "" && lo == "" {
				continue
			}
			if up == "" {
				up = spaceCadet
			}
			if lo == "" {
				lo = spaceCadet
			}
			c.set(top+r, left+col, '▀', up, lo)
		}
	}
}

// springTo runs a harmonica critically/under-damped spring from rest to
// target for exactly n ticks at the given fps and returns the resulting
// position - deterministic re-simulation from frame 0, so any given frame
// number reproduces the same physics a running program would have reached
// by that tick (OUTRUN2.go's own technique, github.com/charmbracelet/harmonica
// v0.2.0, already pinned by this module's own go.mod).
func springTo(n int, fps int, hz, damping, target float64) float64 {
	sp := harmonica.NewSpring(harmonica.FPS(fps), hz, damping)
	pos, vel := 0.0, 0.0
	for i := 0; i < n; i++ {
		pos, vel = sp.Update(pos, vel, target)
	}
	_ = vel
	return pos
}

// loadMark exposes the embedded, decoded mark image to render.go.
func loadMark() image.Image { return markImg }

// markSampleCache memoises sampleMark by (cols, rows): the raster's own
// colours and silhouette never change between frames, only the envelope
// driven by worldFrame does - box-averaging the source PNG fresh on every
// tick (a real per-frame cost, not a one-off) is what made the entrance
// noticeably slower than its intended 2.0s / the idle hand-off drift past
// its intended 4.0s during this leg's own tmux timing sweep. cols/rows
// only change on a terminal resize, so a small mutex-guarded map is ample.
var (
	markSampleCacheMu sync.Mutex
	markSampleCache   = map[[2]int]*markSample{}
)

func sampleMarkCached(img image.Image, cols, rows int) *markSample {
	key := [2]int{cols, rows}
	markSampleCacheMu.Lock()
	defer markSampleCacheMu.Unlock()
	if ms, ok := markSampleCache[key]; ok {
		return ms
	}
	ms := sampleMark(img, cols, rows)
	markSampleCache[key] = ms
	return ms
}
