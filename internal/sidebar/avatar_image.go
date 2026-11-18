package sidebar

import (
	"bytes"
	"errors"
	"hash/fnv"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"tmux-agent/internal/tmux"
)

// ─── Image avatar (kitty / Ghostty) ──────────────────────────────────────
//
// On a terminal with kitty graphics the avatar is a real image instead of
// braille: the built-in characters are re-drawn as colored pixel art (the
// same sprites, colored per mood: outline, body, eyes and mouth), or your
// own picture / GIF per mood (@agent_avatar_image, @agent_avatar_image_<mood>).
// Images are uploaded once (kitty.go) and shown through placeholder cells.

const (
	imagesOption   = "@agent_avatar_images" // auto | on | off
	imageOption    = "@agent_avatar_image"  // your image/GIF for every mood
	imageRowsOpt   = imageOption + "_rows"  // your picture's height in lines (default artRows)
	maxImageRows   = 24
	imageScale     = 8   // sprite pixel → image pixels
	builtinFrames  = 12  // frames per mood for built-in sprites
	maxCustomWidth = 448 // downscale bigger pictures
	maxGIFFrames   = 48
)

var moodNames = []string{"sleep", "work", "happy", "alert", "sad"}

// moodRGB colors a built-in sprite's body per mood.
var moodRGB = map[mood]color.RGBA{
	moodSleep: {150, 160, 190, 255},
	moodWork:  {110, 200, 140, 255},
	moodHappy: {100, 205, 215, 255},
	moodAlert: {245, 185, 80, 255},
	moodSad:   {235, 110, 115, 255},
}

func shade(c color.RGBA, f float64) color.RGBA {
	return color.RGBA{uint8(float64(c.R) * f), uint8(float64(c.G) * f), uint8(float64(c.B) * f), 255}
}

// spritePNG renders a built-in sprite frame as colored pixel art: body in
// the mood color, a darker outline, and the face holes (eyes, mouth) dark.
func spritePNG(name string, md mood, f int) []byte {
	draw0, ok := characters[name]
	if !ok {
		draw0 = slime
	}
	c := draw0(md, f)

	// Outside = off pixels reachable from the border; other off pixels are
	// holes cut into the body (eyes, mouth).
	var outside [pxH][pxW]bool
	var stack [][2]int
	for x := 0; x < pxW; x++ {
		stack = append(stack, [2]int{x, 0}, [2]int{x, pxH - 1})
	}
	for y := 0; y < pxH; y++ {
		stack = append(stack, [2]int{0, y}, [2]int{pxW - 1, y})
	}
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		x, y := p[0], p[1]
		if x < 0 || y < 0 || x >= pxW || y >= pxH || c[y][x] || outside[y][x] {
			continue
		}
		outside[y][x] = true
		stack = append(stack, [2]int{x + 1, y}, [2]int{x - 1, y}, [2]int{x, y + 1}, [2]int{x, y - 1})
	}
	isOut := func(x, y int) bool { return x < 0 || y < 0 || x >= pxW || y >= pxH || outside[y][x] }

	body := moodRGB[md]
	edge, hole := shade(body, 0.55), color.RGBA{35, 35, 45, 255}
	img := image.NewRGBA(image.Rect(0, 0, pxW*imageScale, pxH*imageScale))
	for y := 0; y < pxH; y++ {
		for x := 0; x < pxW; x++ {
			var col color.RGBA
			switch {
			case outside[y][x]:
				continue // transparent
			case !c[y][x]:
				col = hole
			case isOut(x-1, y) || isOut(x+1, y) || isOut(x, y-1) || isOut(x, y+1):
				col = edge
			default:
				col = body
			}
			draw.Draw(img, image.Rect(x*imageScale, y*imageScale, (x+1)*imageScale, (y+1)*imageScale),
				&image.Uniform{col}, image.Point{}, draw.Src)
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

// customFrames loads a picture or animated GIF as PNG frames, letterboxed
// to the avatar's cell area so the terminal doesn't stretch it, with each
// frame's duration in ms (GIF delays; 0 for a still picture).
func customFrames(path string, cols, rows int) ([][]byte, []int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	var frames []image.Image
	var delays []int
	if strings.HasSuffix(strings.ToLower(path), ".gif") {
		g, err := gif.DecodeAll(f)
		if err != nil {
			return nil, nil, err
		}
		canvas := image.NewRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
		for i, fr := range g.Image {
			if i >= maxGIFFrames {
				break
			}
			draw.Draw(canvas, fr.Bounds(), fr, fr.Bounds().Min, draw.Over)
			snap := image.NewRGBA(canvas.Bounds())
			draw.Draw(snap, snap.Bounds(), canvas, image.Point{}, draw.Src)
			frames = append(frames, snap)
			d := 0
			if i < len(g.Delay) {
				d = g.Delay[i] * 10
			}
			if d <= 10 { // like browsers: 0/10 ms means "default", 100 ms
				d = 100
			}
			delays = append(delays, d)
			if i < len(g.Disposal) && g.Disposal[i] == gif.DisposalBackground {
				draw.Draw(canvas, fr.Bounds(), image.Transparent, image.Point{}, draw.Src)
			}
		}
	} else {
		img, _, err := image.Decode(f)
		if err != nil {
			return nil, nil, err
		}
		frames, delays = []image.Image{img}, []int{0}
	}
	if len(frames) == 0 {
		return nil, nil, errors.New("no frames")
	}
	var out [][]byte
	for _, fr := range frames {
		var buf bytes.Buffer
		png.Encode(&buf, letterbox(fr, cols, rows))
		out = append(out, buf.Bytes())
	}
	return out, delays, nil
}

// letterbox fits img (downscaled if huge, nearest-neighbor) into the
// avatar's aspect — cols wide, rows tall, a cell ≈ 1:2.
func letterbox(img image.Image, cols, rows int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > maxCustomWidth {
		h, w = h*maxCustomWidth/w, maxCustomWidth
	}
	aspect := float64(cols) / float64(rows*2)
	cw, ch := w, h
	if float64(w)/float64(h) > aspect {
		ch = int(float64(w) / aspect)
	} else {
		cw = int(float64(h) * aspect)
	}
	out := image.NewRGBA(image.Rect(0, 0, cw, ch))
	ox, oy := (cw-w)/2, (ch-h)/2
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.Set(ox+x, oy+y, img.At(b.Min.X+x*b.Dx()/w, b.Min.Y+y*b.Dy()/h))
		}
	}
	return out
}

// ─── which image to show, uploads ────────────────────────────────────────

// imageSet is every image the avatar may show for the current settings.
type imageSet struct {
	key    string            // settings it was built for
	frames map[mood][]uint32 // mood → image ids per frame
	delays map[mood][]int    // mood → each frame's duration in ms (your GIFs; nil = per tick)
	data   map[uint32]func() []byte
	cols   int // cell area the pictures are shown in
	rows   int
}

// imageSize is the cell area for your pictures: @agent_avatar_image_rows
// lines tall (default and minimum artRows = the sprite's area) and square
// (2 cols per line, a cell ≈ 1:2), shrunk to fit maxCols (the sidebar width
// minus the left margin; ≤ 0 = no limit). Built-in sprites keep artCols × artRows.
func imageSize(opt string, maxCols int) (cols, rows int) {
	n, err := strconv.Atoi(strings.TrimSpace(opt))
	n = min(n, maxImageRows)
	if maxCols > 0 {
		n = min(n, maxCols/2)
	}
	if err != nil || n <= artRows {
		return artCols, artRows
	}
	return 2 * n, n
}

func customID(path string, rows int, md mood, frame int) uint32 {
	h := fnv.New32a()
	h.Write([]byte(path + "\x00" + moodNames[md] + "\x00" + strconv.Itoa(rows)))
	return 0x800000 | (h.Sum32()&0x7FFF)<<8 | uint32(frame&0xFF)
}

func builtinID(name string, md mood, frame int) uint32 {
	idx := 0
	for i, n := range characterNames {
		if n == name {
			idx = i
		}
	}
	return 0x100000 | uint32(idx)<<12 | uint32(md)<<8 | uint32(frame)
}

// buildImageSet lists the images for the character and your image options
// (opts: global tmux options). Pixel data is produced lazily, on upload.
func buildImageSet(character string, opts map[string]string, maxCols int) imageSet {
	set := imageSet{frames: map[mood][]uint32{}, delays: map[mood][]int{}, data: map[uint32]func() []byte{}, cols: artCols, rows: artRows}
	cols, rows := imageSize(opts[imageRowsOpt], maxCols)
	var keys []string
	for md := moodSleep; md <= moodSad; md++ {
		path := strings.TrimSpace(opts[imageOption+"_"+moodNames[md]])
		if path == "" {
			path = strings.TrimSpace(opts[imageOption])
		}
		keys = append(keys, path)
		if path != "" {
			if frames, delays, err := customFrames(path, cols, rows); err == nil {
				// ponytail: a mood left on the built-in sprite gets stretched to this area; set all five moods (or the one-for-all option) to avoid it
				set.cols, set.rows = cols, rows
				set.delays[md] = delays
				for i, fr := range frames {
					id, png := customID(path, rows, md, i), fr
					set.frames[md] = append(set.frames[md], id)
					set.data[id] = func() []byte { return png }
				}
				continue
			}
		}
		for f := 0; f < builtinFrames; f++ {
			id, name, md, f := builtinID(character, md, f), character, md, f
			set.frames[md] = append(set.frames[md], id)
			set.data[id] = func() []byte { return spritePNG(name, md, f) }
		}
	}
	set.key = character + "\x00" + strings.Join(keys, "\x00")
	return set
}

// frameID picks the image for mood at animation tick animFrame (one per
// animInterval): built-in sprites follow the braille timing (one frame per
// 3 ticks), GIFs their own frame durations (a frame shorter than a tick
// may be skipped).
func (s imageSet) frameID(md mood, animFrame int) (uint32, bool) {
	ids := s.frames[md]
	if len(ids) == 0 {
		return 0, false
	}
	if len(ids) == builtinFrames && ids[0]&0x800000 == 0 {
		return ids[(animFrame/3)%len(ids)], true
	}
	delays := s.delays[md]
	total := 0
	for _, d := range delays {
		total += d
	}
	if len(delays) != len(ids) || total == 0 {
		return ids[animFrame%len(ids)], true
	}
	t := animFrame * int(animInterval.Milliseconds()) % total
	for i, d := range delays {
		if t < d {
			return ids[i], true
		}
		t -= d
	}
	return ids[len(ids)-1], true
}

// imageClients reads the attached clients: whether ALL of them can show
// images (kitty/Ghostty — every client sees the same sidebar, so a single
// Alacritty client would see placeholder garbage), and a signature that
// changes when clients come and go — a new client is a terminal that
// doesn't have our images yet.
func imageClients() (graphics bool, sig string) {
	out, ok := tmux.RunTmux("list-clients", "-F", "#{client_pid} #{client_termname}")
	if !ok {
		return false, ""
	}
	return allGraphics(out), out
}

// allGraphics: every "pid termname" line is a kitty or Ghostty client.
func allGraphics(listClients string) bool {
	lines := strings.Fields(strings.TrimSpace(listClients))
	if len(lines) == 0 {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(listClients), "\n") {
		if !strings.Contains(line, "kitty") && !strings.Contains(line, "ghostty") {
			return false
		}
	}
	return true
}

// imagesWanted: @agent_avatar_images on / off, or auto = a graphics client.
func imagesWanted(mode string, graphicsClient bool) bool {
	switch strings.TrimSpace(mode) {
	case "on":
		return true
	case "off":
		return false
	}
	return graphicsClient
}

// imagesUploadedMsg reports ids now in the terminal.
type imagesUploadedMsg struct{ ids []uint32 }

// uploadImages writes the missing images to the terminal (through tmux
// passthrough, on this pane's output). One Write per image keeps each
// escape sequence in one piece.
func uploadImages(set imageSet, have map[uint32]bool, output io.Writer) tea.Cmd {
	var todo []uint32
	for id := range set.data {
		if !have[id] {
			todo = append(todo, id)
		}
	}
	if len(todo) == 0 {
		return nil
	}
	return func() tea.Msg {
		for _, id := range todo {
			output.Write(kittyUpload(id, set.data[id](), set.cols, set.rows))
		}
		return imagesUploadedMsg{ids: todo}
	}
}

// dropImages deletes from the terminal the images of old that next doesn't
// use, and forgets them in have.
func dropImages(old, next imageSet, have map[uint32]bool, output io.Writer) tea.Cmd {
	var gone []uint32
	for id := range old.data {
		if _, keep := next.data[id]; !keep && have[id] {
			gone = append(gone, id)
			delete(have, id)
		}
	}
	if len(gone) == 0 {
		return nil
	}
	return func() tea.Msg {
		for _, id := range gone {
			output.Write(kittyDelete(id))
		}
		return nil
	}
}
