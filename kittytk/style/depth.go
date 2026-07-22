package style

import (
	"fmt"
	"strings"
)

// Depth-aware SGR encoding. Color.FgCode/BgCode emit 24-bit "38;2;r;g;b"
// sequences for RGB colors, which terminals WITHOUT truecolor support
// (macOS Terminal.app most prominently) ignore outright — every RGB-styled
// cell falls back to the default fore/background and the whole UI renders
// monochrome. A depth-limited terminal needs its colors QUANTIZED at
// emission: RGB down to the xterm 256 palette (the 6x6x6 cube or the
// grayscale ramp, whichever is nearer), and further down to the basic 16
// ANSI colors for 16-color terminals. The TUI backend detects the depth
// (COLORTERM/TERM) and serializes every cell through CodeDepth.
//
// NOTE — palette support: the quantizers below assume the CONVENTIONAL
// xterm values for the 256 palette and the basic 16 (ansi16RGB), which is
// only an approximation of what the user's terminal actually shows — real
// terminals re-theme the basic 16 (and some the cube) freely, and KittyTK
// itself has palette knowledge elsewhere (style.ActiveTermPalette /
// ActiveTermANSIColor). If richer color-palette support ever lands, revisit
// this file: quantization should map through the ACTIVE palette's real RGB
// values (and any user-declared preferences) instead of these conventional
// tables, so lower depths pick the perceptually right entries — e.g. a
// theme's dark-red mapping to the terminal's re-themed red rather than the
// nearest conventional slot.

// TrueColorDepth is the colorDepth value meaning full 24-bit emission.
const TrueColorDepth = 16777216

// FgCodeDepth is FgCode quantized to the terminal's color depth.
func (c Color) FgCodeDepth(depth int) string {
	return c.codeDepth(depth, false)
}

// BgCodeDepth is BgCode quantized to the terminal's color depth.
func (c Color) BgCodeDepth(depth int) string {
	return c.codeDepth(depth, true)
}

func (c Color) codeDepth(depth int, bg bool) string {
	if depth >= TrueColorDepth {
		if bg {
			return c.BgCode()
		}
		return c.FgCode()
	}
	// Colorless terminal: attributes only.
	if depth < 16 {
		return ""
	}

	plane, reset := 38, "\033[39m"
	if bg {
		plane, reset = 48, "\033[49m"
	}
	switch {
	case c == ColorDefault:
		return reset
	case c >= 0 && c < 16:
		// Basic colors pass through at any color depth.
		if bg {
			return c.BgCode()
		}
		return c.FgCode()
	case c >= 256+0x1000000:
		// 256-palette index: passes at 256 depth, quantizes to 16 below it.
		idx := int(c) - 256 - 0x1000000
		if depth >= 256 {
			return fmt.Sprintf("\033[%d;5;%dm", plane, idx&0xFF)
		}
		r, g, b := xterm256RGB(idx & 0xFF)
		return ansi16Color(rgbTo16(r, g, b), bg)
	case c >= 256:
		// RGB: quantize to the 256 palette, or the basic 16.
		v := int(c) - 256
		r, g, b := (v>>16)&0xFF, (v>>8)&0xFF, v&0xFF
		if depth >= 256 {
			return fmt.Sprintf("\033[%d;5;%dm", plane, rgbTo256(r, g, b))
		}
		return ansi16Color(rgbTo16(r, g, b), bg)
	}
	return ""
}

// CodeDepth is Code with the colors quantized to the terminal's depth.
func (s CellStyle) CodeDepth(depth int) string {
	if depth >= TrueColorDepth {
		return s.Code()
	}
	var sb strings.Builder
	sb.WriteString("\033[0m") // Reset first
	if s.Attrs != StyleNormal {
		sb.WriteString(s.Attrs.Code())
	}
	if s.Fg != ColorDefault {
		sb.WriteString(s.Fg.FgCodeDepth(depth))
	}
	if s.Bg != ColorDefault {
		sb.WriteString(s.Bg.BgCodeDepth(depth))
	}
	return sb.String()
}

// rgbTo256 maps 24-bit RGB to the nearest xterm-256 palette index: the
// 6x6x6 color cube (16..231) or the grayscale ramp (232..255), whichever
// is closer in RGB space.
func rgbTo256(r, g, b int) int {
	q := func(v int) int { // channel value -> cube level 0..5
		if v < 48 {
			return 0
		}
		if v < 115 {
			return 1
		}
		return (v - 35) / 40
	}
	lv := func(l int) int { // cube level -> channel value
		if l == 0 {
			return 0
		}
		return 55 + l*40
	}
	cr, cg, cb := q(r), q(g), q(b)
	cubeIdx := 16 + 36*cr + 6*cg + cb

	gray := (r + g + b) / 3
	gl := (gray - 3) / 10
	if gl < 0 {
		gl = 0
	}
	if gl > 23 {
		gl = 23
	}
	grayIdx := 232 + gl
	grayVal := 8 + gl*10

	d2 := func(ar, ag, ab int) int {
		dr, dg, db := r-ar, g-ag, b-ab
		return dr*dr + dg*dg + db*db
	}
	if d2(grayVal, grayVal, grayVal) < d2(lv(cr), lv(cg), lv(cb)) {
		return grayIdx
	}
	return cubeIdx
}

// ansi16RGB are the conventional xterm values for the 16 basic colors,
// used only as quantization targets.
var ansi16RGB = [16][3]int{
	{0, 0, 0}, {205, 0, 0}, {0, 205, 0}, {205, 205, 0},
	{0, 0, 238}, {205, 0, 205}, {0, 205, 205}, {229, 229, 229},
	{127, 127, 127}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
	{92, 92, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
}

// rgbTo16 maps RGB to the nearest of the 16 basic ANSI colors.
func rgbTo16(r, g, b int) int {
	best, bestD := 0, 1<<31-1
	for i, c := range ansi16RGB {
		dr, dg, db := r-c[0], g-c[1], b-c[2]
		if d := dr*dr + dg*dg + db*db; d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// ansi16Color renders a basic color index 0..15 as an SGR.
func ansi16Color(idx int, bg bool) string {
	if bg {
		return Color(idx).BgCode()
	}
	return Color(idx).FgCode()
}

// xterm256RGB expands a 256-palette index to its conventional RGB value.
func xterm256RGB(idx int) (r, g, b int) {
	switch {
	case idx < 16:
		c := ansi16RGB[idx]
		return c[0], c[1], c[2]
	case idx < 232:
		v := idx - 16
		lv := func(l int) int {
			if l == 0 {
				return 0
			}
			return 55 + l*40
		}
		return lv(v / 36), lv((v / 6) % 6), lv(v % 6)
	default:
		g := 8 + (idx-232)*10
		return g, g, g
	}
}
