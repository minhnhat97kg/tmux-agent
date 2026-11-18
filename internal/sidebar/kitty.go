package sidebar

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
)

// ─── kitty graphics protocol, Unicode-placeholder mode ───────────────────
//
// The only way images survive tmux: the image is uploaded through tmux's
// passthrough with a *virtual* placement (U=1), and shown by printing
// placeholder cells — U+10EEEE plus two combining diacritics that encode the
// cell's row and column, in a foreground color that encodes the image id.
// To tmux these are ordinary text, so they move with the pane like any
// other character. Needs `allow-passthrough on|all` (tmux ≥ 3.3) and a
// terminal that implements placeholders (kitty ≥ 0.28, Ghostty).

const placeholderRune = '\U0010EEEE'

// diacritics are the first entries of kitty's rowcolumn-diacritics.txt:
// diacritics[n] encodes row/column n.
var diacritics = []rune{
	0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F, 0x0346, 0x034A, 0x034B, 0x034C, 0x0350, 0x0351, 0x0352, 0x0357,
	0x035B, 0x0363, 0x0364, 0x0365, 0x0366, 0x0367, 0x0368, 0x0369, 0x036A, 0x036B, 0x036C, 0x036D, 0x036E, 0x036F, 0x0483, 0x0484,
	0x0485, 0x0486, 0x0487, 0x0592, 0x0593, 0x0594, 0x0595, 0x0597, 0x0598, 0x0599, 0x059C, 0x059D, 0x059E, 0x059F, 0x05A0, 0x05A1,
	0x05A8, 0x05A9, 0x05AB, 0x05AC, 0x05AF, 0x05C4, 0x0610, 0x0611, 0x0612, 0x0613, 0x0614, 0x0615, 0x0616, 0x0617, 0x0657, 0x0658,
}

// kittyUpload returns the escape sequences that transmit a PNG as image id
// with a virtual placement over cols×rows cells, each chunk wrapped in tmux
// passthrough. Quiet (q=2): the terminal sends no reply into the pane.
func kittyUpload(id uint32, png []byte, cols, rows int) []byte {
	payload := base64.StdEncoding.EncodeToString(png)
	var out bytes.Buffer
	for first := true; first || payload != ""; first = false {
		n := min(4096, len(payload))
		chunk := payload[:n]
		payload = payload[n:]
		more := 0
		if payload != "" {
			more = 1
		}
		var ctrl string
		if first {
			ctrl = fmt.Sprintf("a=T,U=1,f=100,i=%d,c=%d,r=%d,q=2,m=%d", id, cols, rows, more)
		} else {
			ctrl = fmt.Sprintf("m=%d", more)
		}
		out.WriteString(tmuxPassthrough("\x1b_G" + ctrl + ";" + chunk + "\x1b\\"))
	}
	return out.Bytes()
}

// kittyDelete frees an image (and its placements) in the terminal.
func kittyDelete(id uint32) []byte {
	return []byte(tmuxPassthrough(fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id)))
}

// tmuxPassthrough wraps seq so tmux forwards it to the outer terminal:
// DCS tmux; <seq with every ESC doubled> ST.
func tmuxPassthrough(seq string) string {
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// placeholderRows returns rows lines of cols placeholder cells that show
// image id (24-bit id → 24-bit foreground color).
func placeholderRows(id uint32, cols, rows int) []string {
	color := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", id>>16&0xFF, id>>8&0xFF, id&0xFF)
	out := make([]string, rows)
	for r := 0; r < rows; r++ {
		var b strings.Builder
		b.WriteString(color)
		for c := 0; c < cols; c++ {
			b.WriteRune(placeholderRune)
			b.WriteRune(diacritics[r])
			b.WriteRune(diacritics[c])
		}
		b.WriteString("\x1b[39m")
		out[r] = b.String()
	}
	return out
}
