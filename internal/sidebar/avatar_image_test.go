package sidebar

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestPlaceholderRows(t *testing.T) {
	rows := placeholderRows(0x10_02_05, 3, 2)
	if len(rows) != 2 || !strings.HasPrefix(rows[0], "\x1b[38;2;16;2;5m") {
		t.Fatalf("rows = %q", rows)
	}
	if got := strings.Count(rows[1], string(placeholderRune)); got != 3 {
		t.Errorf("%d placeholder cells, want 3", got)
	}
	if !strings.Contains(rows[1], string([]rune{placeholderRune, diacritics[1], diacritics[2]})) {
		t.Error("cell (1,2) must carry row 1 / column 2 diacritics")
	}
	if w := lipgloss.Width(rows[0]); w != 3 {
		t.Errorf("a row of 3 cells must measure 3 wide, got %d", w)
	}
}

func TestKittyUploadIsWrappedAndChunked(t *testing.T) {
	seq := string(kittyUpload(7, bytes.Repeat([]byte{1}, 10000), 14, 4))
	if !strings.HasPrefix(seq, "\x1bPtmux;\x1b\x1b_Ga=T,U=1,f=100,i=7,c=14,r=4,q=2,m=1;") {
		t.Errorf("first chunk = %q…", seq[:60])
	}
	if strings.Count(seq, "\x1bPtmux;") < 3 || !strings.Contains(seq, "m=0;") {
		t.Error("a big image must go in several chunks, the last with m=0")
	}
}

func TestSpritePNGIsColoredPixelArt(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(spritePNG("slime", moodWork, 0)))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != pxW*imageScale || b.Dy() != pxH*imageScale {
		t.Fatalf("size %v", b)
	}
	colors := map[color.RGBA]bool{}
	for y := 0; y < pxH*imageScale; y += imageScale {
		for x := 0; x < pxW*imageScale; x += imageScale {
			r, g, b, a := img.At(x, y).RGBA()
			colors[color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}] = true
		}
	}
	if len(colors) < 4 { // transparent, body, outline, face
		t.Errorf("want transparent + body + outline + face colors, got %d", len(colors))
	}
}

func TestCustomGIFFramesAndImageSet(t *testing.T) {
	dir := t.TempDir()
	pal := color.Palette{color.Transparent, color.RGBA{255, 0, 0, 255}}
	g := &gif.GIF{}
	for i := 0; i < 3; i++ {
		fr := image.NewPaletted(image.Rect(0, 0, 20, 20), pal)
		fr.SetColorIndex(i, i, 1)
		g.Image, g.Delay = append(g.Image, fr), append(g.Delay, []int{10, 50, 10}[i]) // 100, 500, 100 ms
	}
	path := filepath.Join(dir, "pet.gif")
	f, _ := os.Create(path)
	gif.EncodeAll(f, g)
	f.Close()

	frames, delays, err := customFrames(path, artCols, artRows)
	if err != nil || len(frames) != 3 || len(delays) != 3 || delays[1] != 500 {
		t.Fatalf("frames = %d, delays = %v, %v", len(frames), delays, err)
	}
	// Frames follow their own durations (ticks are 200 ms): t=0 → 0,
	// 200 → 1 (the long one), 400 → 1, 600 → 2, 800 → 100 into the loop → 1.
	set := buildImageSet("cat", map[string]string{imageOption: path}, 0)
	ids := set.frames[moodSleep]
	for tick, want := range []int{0, 1, 1, 2, 1} {
		if got, _ := set.frameID(moodSleep, tick); got != ids[want] {
			t.Errorf("tick %d shows frame %d's image, want frame %d", tick, slices.Index(ids, got), want)
		}
	}
	img, _ := png.Decode(bytes.NewReader(frames[0]))
	if ar := float64(img.Bounds().Dx()) / float64(img.Bounds().Dy()); ar < 1.6 || ar > 1.9 {
		t.Errorf("letterboxed aspect %.2f, want ≈ %.2f", ar, float64(artCols)/float64(artRows*2))
	}

	for _, tc := range []struct {
		opt        string
		maxCols    int
		cols, rows int
	}{
		{"12", 0, 24, 12}, {"", 0, artCols, artRows}, {"99", 0, 2 * maxImageRows, maxImageRows},
		{"12", 17, 16, 8}, // narrow sidebar: shrinks to fit
		{"12", 9, artCols, artRows},
	} {
		if c, r := imageSize(tc.opt, tc.maxCols); c != tc.cols || r != tc.rows {
			t.Errorf("imageSize(%q, %d) = %d×%d, want %d×%d", tc.opt, tc.maxCols, c, r, tc.cols, tc.rows)
		}
	}
	big := buildImageSet("cat", map[string]string{imageOption: path, imageRowsOpt: "12"}, 0)
	if big.cols != 24 || big.rows != 12 {
		t.Errorf("rows option: set is %d×%d, want 24×12", big.cols, big.rows)
	}

	set = buildImageSet("cat", map[string]string{imageOption + "_happy": path}, 0)
	if len(set.frames[moodHappy]) != 3 || len(set.frames[moodWork]) != builtinFrames {
		t.Errorf("happy uses the GIF (3 frames), others the built-in sprite: %d / %d", len(set.frames[moodHappy]), len(set.frames[moodWork]))
	}
	if a, _ := set.frameID(moodHappy, 0); a == func() uint32 { b, _ := set.frameID(moodHappy, 1); return b }() {
		t.Error("the GIF must advance one frame per tick")
	}
	if !imagesWanted("auto", true) || imagesWanted("off", true) || !imagesWanted("on", false) {
		t.Error("auto follows the client, on/off force it")
	}
}

func TestAllGraphics(t *testing.T) {
	for in, want := range map[string]bool{
		"123 xterm-kitty\n":                     true,
		"123 xterm-kitty\n456 xterm-ghostty\n":  true,
		"123 xterm-kitty\n456 xterm-256color\n": false, // an Alacritty client also watches
		"":                                      false,
	} {
		if got := allGraphics(in); got != want {
			t.Errorf("allGraphics(%q) = %v, want %v", in, got, want)
		}
	}
}

// A taller picture (@agent_avatar_image_rows) arrives on a refresh after the
// first layout: the list must shrink so the avatar's top isn't pushed off.
func TestTallPictureResizesList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pic.png")
	f, _ := os.Create(path)
	png.Encode(f, image.NewRGBA(image.Rect(0, 0, 16, 16)))
	f.Close()

	m := NewModel("%s")
	m.width, m.height = 37, 44
	m.avatar = AvatarConfig{Character: "slime", Lines: 4}
	m.resizeViewport()
	updated, _ := m.Update(RefreshMsg{Theme: DefaultTheme(), Icons: DefaultIcons(), Avatar: m.avatar,
		Images: true, ImageOpts: map[string]string{imageOption: path, imageRowsOpt: "12"}})
	m = updated.(Model)
	if m.avatarH() != 12+4+2+1 {
		t.Fatalf("avatarH = %d, want the 12-row picture block", m.avatarH())
	}
	if got, want := m.viewport.Height, m.height-footerHeight-m.avatarH(); got != want {
		t.Errorf("list height %d, want %d", got, want)
	}

	// The uploaded picture is centered, with no braille props beside it.
	m.imgHave = map[uint32]bool{}
	for id := range m.imgSet.data {
		m.imgHave[id] = true
	}
	for i, l := range m.renderAvatar(m.width)[:12] {
		if lead := len(l) - len(strings.TrimLeft(l, " ")); lead != (37-24)/2 {
			t.Errorf("picture line %d: %d leading spaces, want %d", i, lead, (37-24)/2)
		}
		rest := strings.Map(func(r rune) rune { // drop placeholder cells and their diacritics
			if r >= 0x300 {
				return -1
			}
			return r
		}, ansi.Strip(l))
		if strings.TrimSpace(rest) != "" {
			t.Errorf("picture line %d has more than the picture: %q", i, rest)
		}
	}

	// A narrow sidebar shrinks the picture so it isn't cut off on the right.
	m.width = 18
	updated, _ = m.Update(RefreshMsg{Theme: DefaultTheme(), Icons: DefaultIcons(), Avatar: m.avatar,
		Images: true, ImageOpts: map[string]string{imageOption: path, imageRowsOpt: "12"}})
	m = updated.(Model)
	if m.imgSet.cols+1 > m.width || m.avatarH() != 8+4+2+1 {
		t.Errorf("width 18: picture %d cols, avatarH %d", m.imgSet.cols, m.avatarH())
	}
	for _, l := range m.renderAvatar(m.width) {
		if w := lipgloss.Width(l); w > m.width {
			t.Errorf("line %d wide in a %d-col sidebar", w, m.width)
		}
	}
}

// Switching pictures frees only the images the new set doesn't use.
func TestDropImagesFreesOnlyUnused(t *testing.T) {
	old := imageSet{data: map[uint32]func() []byte{1: nil, 2: nil}}
	next := imageSet{data: map[uint32]func() []byte{2: nil, 3: nil}}
	have := map[uint32]bool{1: true, 2: true}
	if dropImages(old, next, have, os.Stdout) == nil || have[1] || !have[2] {
		t.Fatalf("want image 1 dropped and 2 kept, have = %v", have)
	}
	if dropImages(next, next, have, os.Stdout) != nil {
		t.Fatal("nothing to drop when the set is the same")
	}
}
