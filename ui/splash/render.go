// Package splash is the cockpit's entrance screen: a Bubble Tea v2 model
// ported from the owner-approved mock-up at
// design/splash-80s/OUTRUN-2/OUTRUN2.go (read-only reference; its own
// header traces the machinery further back to design/splash-80s/OUTRUN-1
// and design/splash-logo/SPLASH-6). It ticks at 24fps through a 48-frame
// entrance and then an unbounded idle loop, and hands off to the caller
// (app.go swaps in the instance list) on any key press or automatically
// two seconds after the entrance completes.
//
// Two tunings are applied on top of OUTRUN2.go's own rendering, per
// BRIEF-SPLASH-BUILD.md's "This leg": the mark is scaled up so its tile is
// about 24 rows tall at 120 columns (was 14), proportion kept via the same
// effMarkCols aspect correction (equalizer.go); and the one-column gaps
// between bars close solid on the downbeat rather than staying permanently
// dark (globalBeatEnvelope in equalizer.go), so the logo blazes whole at
// the peak and reads as distinct bars between beats. See layout.go for how
// the 24-row target adapts to a real terminal's actual height without
// pushing the wordmark or fleet counters off-screen.
package splash

import (
	"fmt"
	"math"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

// -- Digital Clarity brand tokens -----------------------------------------
// Source: repos/digital-clarity-design-system/site/src/tokens.css:32-42,
// ported verbatim from design/splash-80s/OUTRUN-2/OUTRUN2.go.
const (
	dollaBillz    = "#73F479" // --dc-dolla-billz    neon green
	openSkies     = "#54E6EA" // --dc-open-skies     cyan
	lucidDreaming = "#EAC4F2" // --dc-lucid-dreaming lavender
	spaceCadet    = "#262C3D" // --dc-space-cadet    midnight ink (ground)
	warmingLight  = "#F2F1EA" // --dc-warming-light  cream
	dcWarning     = "#C25E00" // --dc-warning        amber (tokens.css:61)
	grey060       = "#9294A1" // --dc-grey-060       (tokens.css:53)
	grey070       = "#7B7D8D" // --dc-grey-070       (tokens.css:54)
)

// fps and EntranceFrames set the animation clock: 24 frames per second,
// a 48-frame (2.0s) entrance before the idle loop begins. Ported from
// OUTRUN2.go.
const fps = 24

// EntranceFrames is the length of the entrance in frames (0..47).
const EntranceFrames = 48

// -- canvas ----------------------------------------------------------------
// Ported verbatim from design/splash-80s/OUTRUN-2/OUTRUN2.go (itself
// verbatim from OUTRUN-1/OUTRUN.go).

type cellT struct {
	r  rune
	fg string
	bg string
}

type canvas struct {
	w, h int
	cell [][]cellT
}

func newCanvas(w, h int, bg string) *canvas {
	c := &canvas{w: w, h: h}
	c.cell = make([][]cellT, h)
	for y := range c.cell {
		row := make([]cellT, w)
		for x := range row {
			row[x] = cellT{r: ' ', bg: bg}
		}
		c.cell[y] = row
	}
	return c
}

func (c *canvas) set(y, x int, r rune, fg, bg string) {
	if y < 0 || y >= c.h || x < 0 || x >= c.w {
		return
	}
	if bg == "" {
		bg = c.cell[y][x].bg
	}
	c.cell[y][x] = cellT{r: r, fg: fg, bg: bg}
}

func (c *canvas) text(y, x int, s string, fg, bg string) {
	for i, r := range []rune(s) {
		c.set(y, x+i, r, fg, bg)
	}
}

func (c *canvas) ctext(y int, s string, fg, bg string) {
	x := (c.w - lipgloss.Width(s)) / 2
	c.text(y, x, s, fg, bg)
}

func (c *canvas) render() string {
	var out strings.Builder
	for y := 0; y < c.h; y++ {
		row := c.cell[y]
		x := 0
		for x < c.w {
			fg, bg := row[x].fg, row[x].bg
			start := x
			for x < c.w && row[x].fg == fg && row[x].bg == bg {
				x++
			}
			var b strings.Builder
			for i := start; i < x; i++ {
				b.WriteRune(row[i].r)
			}
			st := lipgloss.NewStyle()
			if fg != "" {
				st = st.Foreground(lipgloss.Color(fg))
			}
			if bg != "" {
				st = st.Background(lipgloss.Color(bg))
			}
			out.WriteString(st.Render(b.String()))
		}
		if y < c.h-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// -- colour helpers ---------------------------------------------------------
// Ported verbatim from OUTRUN2.go.

func lerpHex(a, b string, t float64) string {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	pr, pg, pb := hexToRGB(a)
	qr, qg, qb := hexToRGB(b)
	r := pr + (qr-pr)*t
	g := pg + (qg-pg)*t
	bl := pb + (qb-pb)*t
	return fmt.Sprintf("#%02X%02X%02X", clampByte(r), clampByte(g), clampByte(bl))
}

func hexToRGB(h string) (float64, float64, float64) {
	h = strings.TrimPrefix(h, "#")
	var r, g, b int
	fmt.Sscanf(h, "%02x%02x%02x", &r, &g, &b)
	return float64(r), float64(g), float64(b)
}

func colorDist(a, b string) float64 {
	ar, ag, ab := hexToRGB(a)
	br, bg, bb := hexToRGB(b)
	dr, dg, db := ar-br, ag-bg, ab-bb
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

func clampByte(v float64) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return int(v + 0.5)
}

func easeOutQuad(t float64) float64 {
	if t < 0 {
		return 0
	}
	if t > 1 {
		return 1
	}
	return 1 - (1-t)*(1-t)
}

func clamp01(t float64) float64 {
	if t < 0 {
		return 0
	}
	if t > 1 {
		return 1
	}
	return t
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// -- glyph font (7 rows @120 cols, 5 rows @80 cols) --------------------------
// Ported verbatim from design/splash-80s/OUTRUN-2/OUTRUN2.go (OUTRUN-1's
// chrome-bevel block font, extended there with O, K, P, E, W for
// CLARITY/WORKSPACE).

var bigGlyphs = map[rune][]string{
	'0': {"#####", "#...#", "#...#", "#...#", "#...#", "#...#", "#####"},
	'1': {"..#..", ".##..", "..#..", "..#..", "..#..", "..#..", "#####"},
	'2': {"#####", "....#", "....#", "#####", "#....", "#....", "#####"},
	'3': {"#####", "....#", "....#", "#####", "....#", "....#", "#####"},
	'4': {"#...#", "#...#", "#...#", "#####", "....#", "....#", "....#"},
	'5': {"#####", "#....", "#....", "#####", "....#", "....#", "#####"},
	'6': {"#####", "#....", "#....", "#####", "#...#", "#...#", "#####"},
	'7': {"#####", "....#", "...#.", "..#..", ".#...", ".#...", ".#..."},
	'8': {"#####", "#...#", "#...#", "#####", "#...#", "#...#", "#####"},
	'9': {"#####", "#...#", "#...#", "#####", "....#", "....#", "#####"},
	'C': {".####", "#....", "#....", "#....", "#....", "#....", ".####"},
	'D': {"####.", "#...#", "#...#", "#...#", "#...#", "#...#", "####."},
	'G': {".####", "#....", "#....", "#.###", "#...#", "#...#", ".####"},
	'I': {"#####", "..#..", "..#..", "..#..", "..#..", "..#..", "#####"},
	'T': {"#####", "..#..", "..#..", "..#..", "..#..", "..#..", "..#.."},
	'A': {".###.", "#...#", "#...#", "#####", "#...#", "#...#", "#...#"},
	'L': {"#....", "#....", "#....", "#....", "#....", "#....", "#####"},
	'R': {"####.", "#...#", "#...#", "####.", "#.#..", "#..#.", "#...#"},
	'Y': {"#...#", "#...#", ".#.#.", "..#..", "..#..", "..#..", "..#.."},
	'S': {".####", "#....", "#....", ".####", "....#", "....#", "####."},
	'O': {".###.", "#...#", "#...#", "#...#", "#...#", "#...#", ".###."},
	'K': {"#...#", "#..#.", "#.#..", "##...", "#.#..", "#..#.", "#...#"},
	'P': {"####.", "#...#", "#...#", "####.", "#....", "#....", "#...."},
	'E': {"#####", "#....", "#....", "#####", "#....", "#....", "#####"},
	'W': {"#...#", "#...#", "#...#", "#.#.#", "#.#.#", "#.#.#", ".#.#."},
	' ': {".....", ".....", ".....", ".....", ".....", ".....", "....."},
}

var smallGlyphs = map[rune][]string{
	'0': {"###", "#.#", "#.#", "#.#", "###"},
	'1': {".#.", "##.", ".#.", ".#.", "###"},
	'2': {"###", "..#", "###", "#..", "###"},
	'3': {"###", "..#", "###", "..#", "###"},
	'4': {"#.#", "#.#", "###", "..#", "..#"},
	'5': {"###", "#..", "###", "..#", "###"},
	'6': {"###", "#..", "###", "#.#", "###"},
	'7': {"###", "..#", ".#.", "#..", "#.."},
	'8': {"###", "#.#", "###", "#.#", "###"},
	'9': {"###", "#.#", "###", "..#", "###"},
	'C': {".##", "#..", "#..", "#..", ".##"},
	'D': {"##.", "#.#", "#.#", "#.#", "##."},
	'G': {".##", "#..", "#.#", "#.#", ".##"},
	'I': {"###", ".#.", ".#.", ".#.", "###"},
	'T': {"###", ".#.", ".#.", ".#.", ".#."},
	'A': {".#.", "#.#", "###", "#.#", "#.#"},
	'L': {"#..", "#..", "#..", "#..", "###"},
	'R': {"##.", "#.#", "##.", "#.#", "#.#"},
	'Y': {"#.#", "#.#", ".#.", ".#.", ".#."},
	'S': {".##", "#..", ".#.", "..#", "##."},
	'O': {".#.", "#.#", "#.#", "#.#", ".#."},
	'K': {"#.#", "#.#", "##.", "#.#", "#.#"},
	'P': {"##.", "#.#", "##.", "#..", "#.."},
	'E': {"###", "#..", "###", "#..", "###"},
	'W': {"#.#", "#.#", "#.#", "#.#", ".#."},
	' ': {"...", "...", "...", "...", "..."},
}

func glyphTextWidth(s string, glyphs map[rune][]string, tracking int) int {
	w := 0
	for _, r := range s {
		g, ok := glyphs[r]
		if !ok {
			g = glyphs[' ']
		}
		gw := 0
		for _, line := range g {
			if len(line) > gw {
				gw = len(line)
			}
		}
		w += gw + tracking
	}
	if w > 0 {
		w -= tracking
	}
	return w
}

func glyphAt(s string, glyphs map[rune][]string, tracking int) []int {
	x := 0
	xs := make([]int, 0, len(s))
	for _, r := range s {
		xs = append(xs, x)
		g, ok := glyphs[r]
		if !ok {
			g = glyphs[' ']
		}
		gw := 0
		for _, line := range g {
			if len(line) > gw {
				gw = len(line)
			}
		}
		x += gw + tracking
	}
	return xs
}

// drawChromeGlyph: the two-tone chrome bevel treatment (bright sheen top
// third, shaded underside below). Ported verbatim from OUTRUN2.go.
func drawChromeGlyph(c *canvas, top, left int, r rune, glyphs map[rune][]string, xOffset int, bg string, alpha float64) {
	g, ok := glyphs[r]
	if !ok {
		return
	}
	rows := len(g)
	highlightRows := (rows + 1) / 3
	for row, line := range g {
		var base string
		if row < highlightRows {
			base = lerpHex(openSkies, warmingLight, 0.7)
		} else {
			base = lerpHex(grey070, spaceCadet, 0.35)
		}
		fg := base
		if alpha < 1 {
			fg = lerpHex(bg, base, alpha)
		}
		for col, ch := range line {
			if ch == '#' {
				c.set(top+row, left+xOffset+col, '█', fg, bg)
			}
		}
	}
}

// drawGlowText: digits with a one-cell dim halo behind them. Ported
// verbatim from OUTRUN2.go.
func drawGlowText(c *canvas, top, left int, s string, glyphs map[rune][]string, tracking int, colour, bg string) {
	halo := lerpHex(bg, colour, 0.28)
	x := left
	for _, r := range s {
		g, ok := glyphs[r]
		if !ok {
			g = glyphs[' ']
		}
		w := 0
		for row, line := range g {
			for col, ch := range line {
				if ch == '#' {
					for _, d := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
						c.set(top+row+d[0], x+col+d[1], '█', halo, bg)
					}
				}
				w = col + 1
			}
		}
		for row, line := range g {
			for col, ch := range line {
				if ch == '#' {
					c.set(top+row, x+col, '█', colour, bg)
				}
			}
		}
		x += w + tracking
	}
}

// -- the perspective grid -----------------------------------------------------
// Ported verbatim from OUTRUN2.go (itself verbatim from OUTRUN-1/OUTRUN.go).

func drawGrid(c *canvas, horizon, bottom, cx int, halfSpread, cols int, phase float64, colourT func(dist float64) string) {
	const nLines = 14
	for i := 0; i < nLines; i++ {
		dist := math.Mod(float64(i)-phase, float64(nLines))
		if dist < 0 {
			dist += float64(nLines)
		}
		frac := 1.0 / (dist + 1.2)
		row := horizon + int(math.Round(float64(bottom-horizon)*frac))
		if row <= horizon || row > bottom {
			continue
		}
		spreadFrac := frac
		lineHalf := int(float64(halfSpread) * spreadFrac)
		col := colourT(dist / float64(nLines))
		for x := cx - lineHalf; x <= cx+lineHalf; x++ {
			if x < 0 || x >= cols {
				continue
			}
			c.set(row, x, '─', col, "")
		}
	}
	const nRays = 11
	for i := -nRays / 2; i <= nRays/2; i++ {
		if i == 0 {
			continue
		}
		endX := cx + i*(halfSpread*2/nRays)
		for row := horizon + 1; row <= bottom; row++ {
			t := float64(row-horizon) / float64(bottom-horizon)
			x := cx + int(math.Round(float64(endX-cx)*t))
			if x < 0 || x >= cols {
				continue
			}
			col := colourT(1 - t)
			ch := '│'
			if existing := c.cell[row][x]; existing.r == '─' {
				ch = '┼'
			}
			c.set(row, x, ch, col, "")
		}
	}
}

func trackedLower(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		b.WriteRune(r)
		if i < len(runes)-1 {
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// -- screen assembly ----------------------------------------------------------

// RenderFrame renders one frame of the splash at the given terminal size:
//   - entranceFrame in [0, EntranceFrames) selects an entrance frame (the
//     bars rising into their first beat on a harmonica spring, the
//     wordmark and counters sliding/counting in).
//   - entranceFrame < 0 and idleFrame >= 0 selects an idle-loop frame
//     (worldFrame continues the entrance's own clock, so there is no seam).
//   - entranceFrame < 0 and idleFrame < 0 is the resting/peak frame: every
//     bar and every gap at full brightness, wordmark and counters fully
//     revealed - the frame layoutFor's own tests call "the peak frame".
//
// live and waiting are the fleet numbers (data.go reads them from the same
// sources ui.List already reads, so the splash and the list agree).
func RenderFrame(width, height, entranceFrame, idleFrame, live, waiting int) string {
	lo := layoutFor(width, height)

	isEntrance := entranceFrame >= 0 && entranceFrame < EntranceFrames
	isIdle := !isEntrance && idleFrame >= 0
	resting := !isEntrance && !isIdle

	var worldFrame float64
	gridPhase := 0.0
	wordProgress := 1.0
	countProgress := 1.0
	riseGate := 1.0

	if isEntrance {
		worldFrame = float64(entranceFrame)
		gridPhase = float64(entranceFrame) / 6.0
		riseGate = springTo(entranceFrame+1, fps, 8.0, 0.85, 1.0)
		wordProgress = easeOutQuad(clamp01(float64(entranceFrame-10) / 26.0))
		countProgress = easeOutQuad(clamp01(float64(entranceFrame-30) / 16.0))
	} else if isIdle {
		worldFrame = float64(EntranceFrames + idleFrame)
		gridPhase = float64(EntranceFrames)/6.0 + float64(idleFrame)/6.0
	}

	markTop := 0
	horizonRow := markTop + lo.markRows - 1
	gridTop := markTop + lo.markRows
	gridBottom := gridTop + lo.gridRows - 1
	gapA := 1
	line1Top := gridBottom + gapA + 1
	line1Bottom := line1Top + lo.glyphH - 1
	lineGap := 1
	line2Top := line1Bottom + lineGap + 1
	line2Bottom := line2Top + lo.glyphH - 1
	gapB := 1
	makerRow := line2Bottom + gapB + 1
	rows := makerRow + 1

	c := newCanvas(lo.width, rows, spaceCadet)

	// -- faint lucid-dreaming horizon glow, behind everything --------------
	glowBand := float64(lo.markRows) * 0.9
	for y := 0; y < rows; y++ {
		d := math.Abs(float64(y) - float64(horizonRow))
		t := clamp01(1 - d/glowBand)
		if t <= 0 {
			continue
		}
		g := t * t * 0.18
		for x := 0; x < lo.width; x++ {
			c.set(y, x, ' ', "", lerpHex(spaceCadet, lucidDreaming, g))
		}
	}

	// -- ground grid: reaches down to the bottom rows so the screen is full
	gridColour := func(distFrac float64) string {
		return lerpHex(warmingLight, openSkies, distFrac)
	}
	drawGrid(c, horizonRow, gridBottom, lo.width/2, lo.width/2-2, lo.width, gridPhase, gridColour)

	// -- the mark: ON the horizon, its bottom edge resting there ----------
	markLeft := (lo.width - lo.markCols) / 2
	ms := sampleMarkCached(loadMark(), lo.markCols, lo.markRows)
	drawMarkEqualizer(c, ms, markTop, markLeft, lo.markRows, worldFrame, resting, isEntrance, riseGate, lo.big)

	// -- wordmark: CLARITY / WORKSPACE, chrome-bevel, sliding in from both
	// edges.
	drawWordLine := func(top int, s string) (left, width int) {
		widths := glyphAt(s, lo.glyphs, lo.tracking)
		fullW := glyphTextWidth(s, lo.glyphs, lo.tracking)
		wordLeft := (lo.width - fullW) / 2
		mid := len([]rune(s)) / 2
		if wordProgress > 0.02 {
			for i, r := range []rune(s) {
				left := wordLeft + widths[i]
				var xOff int
				if i < mid {
					xOff = -int(math.Round((1 - wordProgress) * float64(lo.width)))
				} else {
					xOff = int(math.Round((1 - wordProgress) * float64(lo.width)))
				}
				drawChromeGlyph(c, top, left, r, lo.glyphs, xOff, spaceCadet, clamp01(wordProgress*2))
			}
		}
		return wordLeft, fullW
	}
	drawWordLine(line1Top, "CLARITY")
	wordLeft2, wordW2 := drawWordLine(line2Top, "WORKSPACE")

	// -- the live fleet counters: glowing figures either side of the
	// wordmark, vertically centred against the two-line block.
	liveShown := live
	waitShown := waiting
	if isEntrance {
		liveShown = int(math.Round(float64(live) * countProgress))
		waitShown = int(math.Round(float64(waiting) * countProgress))
	}
	liveStr := fmt.Sprintf("%d", liveShown)
	waitStr := fmt.Sprintf("%d", waitShown)
	liveW := glyphTextWidth(liveStr, lo.glyphs, lo.tracking)
	waitW := glyphTextWidth(waitStr, lo.glyphs, lo.tracking)
	liveLabel := "LANES LIVE"
	waitLabel := "NEEDS YOU"
	liveColW := lipgloss.Width(liveLabel)
	if liveW > liveColW {
		liveColW = liveW
	}
	waitColW := lipgloss.Width(waitLabel)
	if waitW > waitColW {
		waitColW = waitW
	}
	sideGap := 4
	if lo.big {
		sideGap = 6
	}
	countersTop := line1Top + (line2Bottom-line1Top+1-lo.glyphH)/2
	labelRow := countersTop + lo.glyphH

	leftBlockRight := wordLeft2 - sideGap
	liveColLeft := leftBlockRight - liveColW
	if liveColLeft < 1 {
		liveColLeft = 1
	}
	waitColLeft := wordLeft2 + wordW2 + sideGap

	if countProgress > 0.01 || !isEntrance {
		drawGlowText(c, countersTop, liveColLeft+(liveColW-liveW)/2, liveStr, lo.glyphs, lo.tracking, dollaBillz, spaceCadet)
		drawGlowText(c, countersTop, waitColLeft+(waitColW-waitW)/2, waitStr, lo.glyphs, lo.tracking, dcWarning, spaceCadet)
		c.text(labelRow, liveColLeft+(liveColW-lipgloss.Width(liveLabel))/2, liveLabel, lerpHex(spaceCadet, dollaBillz, 0.75), "")
		c.text(labelRow, waitColLeft+(waitColW-lipgloss.Width(waitLabel))/2, waitLabel, lerpHex(spaceCadet, dcWarning, 0.75), "")
	}

	// -- maker line: "digital clarity", tracked, dim, at the very foot ----
	maker := trackedLower("digital clarity")
	c.ctext(makerRow, maker, grey060, "")

	return c.render()
}
