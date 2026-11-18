package sidebar

import (
	"fmt"
	"math"
)

// ─── Avatar art: braille pixel sprites ───────────────────────────────────
//
// Each character is drawn by code on a pxW×pxH pixel canvas (ellipses,
// triangles, lines; eyes and mouths are holes cut into the body) and
// rendered with braille cells — 2×4 pixels per terminal cell — so a 28×16
// sprite fits in 14 columns × 4 lines. Moods animate the shape itself:
// bounce, squash, shake, blink, grow, bloom, droop…

const (
	pxW       = 28
	pxH       = 16
	artCols   = pxW / 2 // 14
	artRows   = pxH / 4 // 4
	propCols  = 3       // props column right of the sprite (z, ♪, !…)
	artHeight = artRows
)

type mood int

const (
	moodSleep mood = iota // nothing running
	moodWork              // agents running
	moodHappy             // something finished
	moodAlert             // something waits on you
	moodSad               // something failed
)

type canvas [pxH][pxW]bool

func (c *canvas) set(x, y int, on bool) {
	if x >= 0 && x < pxW && y >= 0 && y < pxH {
		c[y][x] = on
	}
}

// ellipse fills (or clears) an ellipse; maxY cuts it off below (a flat
// bottom), use pxH for none.
func (c *canvas) ellipse(cx, cy, rx, ry float64, on bool, maxY int) {
	for y := 0; y < pxH && y <= maxY; y++ {
		for x := 0; x < pxW; x++ {
			dx, dy := (float64(x)+0.5-cx)/rx, (float64(y)+0.5-cy)/ry
			if dx*dx+dy*dy <= 1 {
				c[y][x] = on
			}
		}
	}
}

func (c *canvas) rect(x0, y0, x1, y1 int, on bool) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			c.set(x, y, on)
		}
	}
}

func (c *canvas) line(x0, y0, x1, y1 int, on bool) {
	n := max(abs(x1-x0), abs(y1-y0))
	for i := 0; i <= n; i++ {
		t := 0.0
		if n > 0 {
			t = float64(i) / float64(n)
		}
		c.set(int(math.Round(float64(x0)+t*float64(x1-x0))), int(math.Round(float64(y0)+t*float64(y1-y0))), on)
	}
}

func (c *canvas) triangle(ax, ay, bx, by, qx, qy float64, on bool) {
	sign := func(px, py, x1, y1, x2, y2 float64) float64 { return (px-x2)*(y1-y2) - (x1-x2)*(py-y2) }
	for y := 0; y < pxH; y++ {
		for x := 0; x < pxW; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			d1, d2, d3 := sign(px, py, ax, ay, bx, by), sign(px, py, bx, by, qx, qy), sign(px, py, qx, qy, ax, ay)
			neg, pos := d1 < 0 || d2 < 0 || d3 < 0, d1 > 0 || d2 > 0 || d3 > 0
			if !(neg && pos) {
				c[y][x] = on
			}
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// braille renders the canvas, one braille cell per 2×4 pixels.
func (c *canvas) braille() [artRows]string {
	dots := [4][2]rune{{0x01, 0x08}, {0x02, 0x10}, {0x04, 0x20}, {0x40, 0x80}}
	var out [artRows]string
	for row := 0; row < artRows; row++ {
		line := make([]rune, artCols)
		for col := 0; col < artCols; col++ {
			r := rune(0x2800)
			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					if c[row*4+dy][col*2+dx] {
						r |= dots[dy][dx]
					}
				}
			}
			line[col] = r
		}
		out[row] = string(line)
	}
	return out
}

// face cuts eyes and a mouth into a body for mood md at frame f: eyes at
// (lx,ey) and (rx,ey), mouth centered at (mx,my).
func (c *canvas) face(md mood, f, lx, rx, ey, mx, my int) {
	blink := f%7 == 6
	for _, ex := range []int{lx, rx} {
		switch {
		case md == moodSleep || blink:
			c.rect(ex-1, ey+1, ex+1, ey+1, false) // closed: a line
		case md == moodHappy: // ^
			c.set(ex-1, ey+1, false)
			c.set(ex, ey, false)
			c.set(ex+1, ey+1, false)
		case md == moodAlert: // wide open
			c.rect(ex-1, ey-1, ex+1, ey+1, false)
		case md == moodSad: // slanted down outwards
			if ex == lx {
				c.line(ex-1, ey, ex+1, ey+1, false)
			} else {
				c.line(ex-1, ey+1, ex+1, ey, false)
			}
		default:
			c.rect(ex, ey, ex+1, ey+1, false)
		}
	}
	switch md {
	case moodHappy: // wide smile
		c.rect(mx-2, my, mx+2, my, false)
		c.set(mx-3, my-1, false)
		c.set(mx+3, my-1, false)
	case moodAlert: // "o"
		c.rect(mx-1, my, mx, my+1, false)
	case moodSad: // frown
		c.rect(mx-1, my, mx+1, my, false)
		c.set(mx-2, my+1, false)
		c.set(mx+2, my+1, false)
	case moodSleep:
		c.set(mx, my, false)
	default:
		c.rect(mx-1, my, mx+1, my, false)
	}
}

// ─── Characters ──────────────────────────────────────────────────────────

type sprite func(md mood, f int) canvas

var characters = map[string]sprite{
	"slime":  slime,
	"cat":    cat,
	"sprout": sprout,
	"ghost":  ghost,
	"robot":  robot,
}

// characterNames is the order shown in docs/tests.
var characterNames = []string{"slime", "cat", "sprout", "ghost", "robot"}

// slime: a jelly dome — breathes asleep, bounces at work, jumps for joy,
// shakes when it needs you, melts when sad.
func slime(md mood, f int) canvas {
	var c canvas
	cx, bottom, rx, ry := 13.5, 15.0, 11.0, 10.0
	switch md {
	case moodSleep:
		ry, rx = 7+float64(f%2), 12
	case moodWork:
		bottom -= []float64{0, 1, 2, 1}[f%4]
		ry += []float64{-1, 0, 1, 0}[f%4]
	case moodHappy:
		lift := []float64{0, 3, 4, 2}[f%4]
		bottom -= lift
		if lift == 0 {
			ry, rx = 8, 12.5 // squash on landing
		}
	case moodAlert:
		cx += []float64{0, 1, 0, -1}[f%4]
		ry = 11
	case moodSad:
		ry, rx = 8, 12.5
	}
	c.ellipse(cx, bottom, rx, ry, true, int(bottom))
	ey := int(bottom - ry*0.62)
	c.face(md, f, int(cx-rx*0.38), int(cx+rx*0.38), ey, int(cx), ey+3)
	return c
}

// cat: a cat head — ears twitch, eyes blink, bobs while working, hops when
// happy, ears flatten when sad.
func cat(md mood, f int) canvas {
	var c canvas
	cx, cy := 13.5, 10.5
	switch md {
	case moodSleep:
		cy += 1
	case moodWork:
		cy -= []float64{0, 0.5, 1, 0.5}[f%4]
	case moodHappy:
		cy -= []float64{0, 2, 3, 1}[f%4]
	case moodAlert:
		cx += []float64{0, 1, 0, -1}[f%4]
	}
	c.ellipse(cx, cy, 9, 5.5, true, pxH)
	lt, rt := cy-9.5, cy-9.5 // ear tips
	switch {
	case md == moodSad:
		lt, rt = cy-5.5, cy-5.5
	case md == moodAlert:
		lt, rt = cy-10.5, cy-10.5
	case f%5 == 4:
		lt += 2 // twitch
	}
	c.triangle(cx-8.5, cy-2, cx-7, lt, cx-2.5, cy-4.5, true)
	c.triangle(cx+8.5, cy-2, cx+7, rt, cx+2.5, cy-4.5, true)
	// whiskers
	for _, s := range []float64{-1, 1} {
		x0 := int(cx + s*9)
		c.line(x0, int(cy)+1, x0+int(s*4), int(cy), true)
		c.line(x0, int(cy)+2, x0+int(s*4), int(cy)+3, true)
	}
	c.face(md, f, int(cx-4), int(cx+3), int(cy-1), int(cx), int(cy)+2)
	return c
}

// sprout: a seedling in a pot — grows at work, blooms when happy, waves its
// leaves when it needs you, wilts when sad, droops asleep.
func sprout(md mood, f int) canvas {
	var c canvas
	// pot
	for y := 11; y <= 15; y++ {
		in := (y - 11) / 2
		c.rect(7+in, y, 20-in, y, true)
	}
	c.rect(6, 11, 21, 11, true) // rim
	c.face(md, f, 11, 16, 12, 13, 14)

	h := map[mood]int{moodSleep: 4, moodWork: 4 + f%4, moodHappy: 7, moodAlert: 6, moodSad: 5}[md]
	top := 10 - h
	if md == moodSad { // bent over
		c.line(13, 10, 17, top+2, true)
		c.ellipse(19, float64(top+4), 2.5, 1.2, true, pxH)
		c.ellipse(15, float64(top+6), 2.5, 1.2, true, pxH)
		return c
	}
	c.rect(13, top, 14, 10, true)
	ly := float64(top + 2)
	sway := 0.0
	switch md {
	case moodSleep:
		ly += 3 // drooping leaves
	case moodWork:
		sway = []float64{0, 1, 0, -1}[f%4]
	case moodAlert:
		sway = []float64{-2, 2}[f%2] // waving
	}
	c.ellipse(10, ly+sway, 3.2, 1.4, true, pxH)
	c.ellipse(17.5, ly-sway, 3.2, 1.4, true, pxH)
	if md == moodHappy { // bloom
		r := []float64{1.5, 2.5, 3, 2.5}[f%4]
		c.ellipse(13.5, float64(top)-1, r+0.5, r*0.8, true, pxH)
		c.ellipse(13.5, float64(top)-1, 0.9, 0.7, false, pxH)
	}
	return c
}

// ghost: floats up and down with a rippling hem.
func ghost(md mood, f int) canvas {
	var c canvas
	dy := []int{0, -1, -2, -1}[f%4]
	cx := 13.5
	if md == moodAlert {
		cx += []float64{0, 1, 0, -1}[f%4]
	}
	if md == moodSleep {
		dy = 1
	}
	c.ellipse(cx, float64(7+dy), 8, 7, true, 7+dy)
	c.rect(int(cx-8), 7+dy, int(cx+7), 12+dy, true)
	for x := int(cx - 8); x <= int(cx+7); x++ {
		if (x/2+f)%2 == 0 {
			c.set(x, 13+dy, true)
		}
	}
	c.face(md, f, int(cx-3), int(cx+2), 5+dy, int(cx), 9+dy)
	return c
}

// robot: a boxy head with a blinking antenna light and a chattering mouth.
func robot(md mood, f int) canvas {
	var c canvas
	dy := 0
	if md == moodHappy {
		dy = -[]int{0, 2, 1, 0}[f%4]
	}
	sx := 0
	if md == moodAlert {
		sx = []int{0, 1, 0, -1}[f%4]
	}
	c.rect(6+sx, 4+dy, 21+sx, 13+dy, true)
	c.set(6+sx, 4+dy, false)
	c.set(21+sx, 4+dy, false)
	c.rect(4+sx, 7+dy, 5+sx, 10+dy, true)   // ears
	c.rect(22+sx, 7+dy, 23+sx, 10+dy, true) //
	c.rect(13+sx, 1+dy, 14+sx, 3+dy, true)  // antenna
	light := md != moodSleep && (md != moodAlert || f%2 == 0)
	c.rect(12+sx, 0+dy, 15+sx, 0+dy, light)
	c.face(md, f, 10+sx, 17+sx, 7+dy, 13+sx, 11+dy)
	if md == moodWork { // chattering teeth
		for x := 10 + sx; x <= 17+sx; x++ {
			c.set(x, 11+dy, (x+f)%2 == 0)
		}
	}
	return c
}

// prop is something drifting beside the sprite: braille row, text.
type prop struct {
	row  int
	text string
}

var moodProps = map[mood][]prop{
	moodSleep: {{2, "z"}, {1, " z"}, {0, " Z"}, {0, "  Z"}, {0, ""}, {0, ""}},
	moodWork:  {{3, "·"}, {3, "··"}, {3, "···"}, {3, ""}},
	moodHappy: {{1, "♪"}, {0, " ♫"}, {1, "  ♪"}, {0, "♫"}},
	moodAlert: {{0, "!!"}, {0, ""}, {1, "!!"}, {0, "!"}},
	moodSad:   {{1, "'"}, {2, " '"}, {3, " ."}, {3, ""}},
}

// avatarArt renders character name for mood at frame f: the braille
// sprite lines and, separately, the prop column (colored differently).
func avatarArt(name string, md mood, f int) (art, props [artRows]string) {
	draw, ok := characters[name]
	if !ok {
		draw = slime
	}
	c := draw(md, f)
	art = c.braille()
	for r := range props {
		props[r] = fmt.Sprintf("%-*s", propCols, "")
	}
	if ps := moodProps[md]; len(ps) > 0 {
		p := ps[f%len(ps)]
		props[p.row] = fmt.Sprintf("%-*s", propCols, p.text)
	}
	return art, props
}
