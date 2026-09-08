package render

import (
	"strconv"
	"strings"

	"github.com/phroun/khatool"
	"github.com/phroun/mew/internal/viewport"
)

// What a cell wears divides in two. Some of it is INK -- the glyph's own
// colour, its weight, its slant -- and travels with the glyph wherever a
// terminal decides to put it. The rest is painted at the CELL: the background,
// reverse video, an underline, a strike. A terminal that reorders what it is
// sent and miscounts while doing it puts those in the wrong place, so on a row
// where that has started they are dropped rather than left to land somewhere
// they do not belong. Dropping makes a picture less informative; leaving them
// makes it wrong.
//
// A blank cell is the one case that loses nothing: with no glyph of its own to
// carry, it can say what its background was by DRAWING it, in the colour it
// would have been given.
//
// Three shades, and which one a cell wears says what kind of region it belongs
// to -- a legend rather than a strength. The full block is deliberately not
// among them: fonts draw it as often as a rectangular bullet as a tile, so a
// run of them can come out beaded instead of continuous, where the three shades
// are drawn as textures and tile.
const (
	selectedBlank = "░" // 25%: the selection, which says itself before anything else
	fallbackBlank = "▒" // 50%: a region with no treatment of its own
	gutterBlank   = "▓" // 75%: the line-number gutter
)

// dropGround returns the style with everything painted at the cell removed and
// everything belonging to the glyph kept. It walks whole SGR sequences, since a
// cell's style may be several of them run together.
func dropGround(style string) string {
	return filterSGR(style, func(p sgrParam) (string, bool) {
		if p.cellPainted {
			return "", false
		}
		return p.text, true
	})
}

// groundAsInk returns the style with the cell's background turned into the
// glyph's colour and everything else painted at the cell removed -- what a
// blank cell wears to draw its own background instead of being given one.
// ok is false when there was no background to draw.
func groundAsInk(style string) (string, bool) {
	found := false
	out := filterSGR(style, func(p sgrParam) (string, bool) {
		switch {
		case p.reset:
			return p.text, true
		case p.blackGround:
			// Black is what a terminal shows where nothing has been painted, so
			// there is nothing here to draw and a shaded cell would only be
			// texture over the ground it already has. It still goes, since a
			// black fill landing on a coloured cell would black that cell out.
			return "", false
		case p.background != "":
			found = true
			return p.background, true
		}
		// The colour and nothing else. Everything left describes a glyph, and
		// the cell has none -- weight in particular would not carry the ground's
		// colour across but change it, drawing a bold blue where the ground was
		// plain blue.
		return "", false
	})
	return out, found
}

// sgrParam is one parameter of an SGR sequence, already gathered with whatever
// follows it (an extended colour spends several).
type sgrParam struct {
	text string // the parameter as written, e.g. "44" or "48;5;27"
	// background holds the same colour said as a FOREGROUND, for a parameter
	// that sets a background; empty otherwise.
	background string
	foreground bool // sets the glyph's colour
	// reset marks the parameter that clears everything, which is what makes a
	// style self-contained: it is kept whatever else a filter drops.
	reset bool
	// blackGround marks a background that IS the ground: what the terminal
	// shows where nothing was painted. It is dropped like any other, but a
	// blank wearing it has nothing to draw in its place.
	blackGround bool
	// cellPainted marks what the terminal paints at the cell rather than on the
	// glyph: a background, reverse video, an underline, a strike. These are the
	// ones that land in the wrong place.
	cellPainted bool
}

// blackLuminance is the ceiling below which a ground counts as the black a
// terminal shows where nothing has been painted. A theme's "black" is rarely
// the pure one -- a near-black is the usual choice -- and a shade drawn over it
// says nothing a reader can see, so what matters is not the value but whether
// there is enough light in it to tell from the ground.
//
// Weighted for the eye rather than averaged: green carries most of what is
// seen, red some, blue very little.
const (
	blackLuminance = 0.14
	weightRed      = 0.22
	weightGreen    = 0.72
	weightBlue     = 0.08
)

func groundIsDark(r, g, b int) bool {
	lum := weightRed*float64(r) + weightGreen*float64(g) + weightBlue*float64(b)
	return lum/255 < blackLuminance
}

// isBlackGround reports whether an extended background is the ground itself.
// Black has spellings a theme is free to choose between -- the basic 40, the
// system palette's 0, and any colour dark enough to be one -- and reading only
// some of them left a theme written another way with a ground that answered as
// a colour, standing a shade on every blank past the drift where the file has a
// space.
func isBlackGround(text string) bool {
	switch {
	case text == "48;5;0":
		// Named black, whatever the terminal happens to draw it as.
		return true
	case strings.HasPrefix(text, "48;5;"):
		n, err := strconv.Atoi(text[len("48;5;"):])
		if err != nil {
			return false
		}
		r, g, b, ok := palette256(n)
		return ok && groundIsDark(r, g, b)
	case strings.HasPrefix(text, "48;2;"):
		f := strings.Split(text[len("48;2;"):], ";")
		if len(f) != 3 {
			return false
		}
		var c [3]int
		for i, s := range f {
			n, err := strconv.Atoi(s)
			if err != nil {
				return false
			}
			c[i] = n
		}
		return groundIsDark(c[0], c[1], c[2])
	}
	return false
}

// palette256 is the colour behind an extended-palette index, for the two thirds
// of the palette that are standardised: the 6x6x6 cube from 16 and the
// greyscale ramp from 232. The sixteen below those are the terminal's own to
// draw and have no answer here.
func palette256(n int) (r, g, b int, ok bool) {
	switch {
	case n >= 16 && n <= 231:
		n -= 16
		level := [6]int{0, 95, 135, 175, 215, 255}
		return level[n/36], level[(n/6)%6], level[n%6], true
	case n >= 232 && n <= 255:
		v := 8 + (n-232)*10
		return v, v, v, true
	}
	return 0, 0, 0, false
}

// filterSGR rewrites every SGR sequence in style through keep, which returns
// the text to emit for a parameter and whether to emit it at all. Anything that
// is not an SGR sequence is passed through untouched.
func filterSGR(style string, keep func(sgrParam) (string, bool)) string {
	var out strings.Builder
	for len(style) > 0 {
		start := strings.Index(style, "\x1b[")
		if start < 0 {
			out.WriteString(style)
			break
		}
		out.WriteString(style[:start])
		rest := style[start+2:]
		end := strings.IndexByte(rest, 'm')
		if end < 0 {
			out.WriteString(style[start:]) // not an SGR sequence; leave it alone
			break
		}
		var kept []string
		for _, p := range sgrParams(rest[:end]) {
			if text, ok := keep(p); ok {
				kept = append(kept, text)
			}
		}
		if len(kept) > 0 {
			out.WriteString("\x1b[" + strings.Join(kept, ";") + "m")
		}
		style = rest[end+1:]
	}
	return out.String()
}

// sgrParams splits an SGR sequence's body into parameters, gathering the two
// extended-colour forms (5;n and 2;r;g;b) with the 38/48 that introduces them.
func sgrParams(body string) []sgrParam {
	fields := strings.Split(body, ";")
	var out []sgrParam
	for i := 0; i < len(fields); i++ {
		n, err := strconv.Atoi(fields[i])
		if err != nil {
			out = append(out, sgrParam{text: fields[i]})
			continue
		}
		switch {
		case n == 38 || n == 48:
			// An extended colour: 5;n for the 256 palette, 2;r;g;b for a direct
			// one. Take what belongs to it so the pieces are never split up.
			take := 1
			if i+1 < len(fields) {
				switch fields[i+1] {
				case "5":
					take = 3
				case "2":
					take = 5
				}
			}
			if i+take > len(fields) {
				take = len(fields) - i
			}
			text := strings.Join(fields[i:i+take], ";")
			p := sgrParam{text: text, foreground: n == 38}
			if n == 48 {
				p.cellPainted = true
				p.background = "38" + strings.TrimPrefix(text, "48")
				p.blackGround = isBlackGround(text)
			}
			out = append(out, p)
			i += take - 1
		case n >= 40 && n <= 47, n >= 100 && n <= 107:
			out = append(out, sgrParam{
				text: fields[i], cellPainted: true,
				background:  strconv.Itoa(n - 10),
				blackGround: n == 40,
			})
		case n >= 30 && n <= 37, n >= 90 && n <= 97:
			out = append(out, sgrParam{text: fields[i], foreground: true})
		case n == 4 || n == 7 || n == 9 || n == 21 || n == 53:
			// Underline, reverse, strike, double underline, overline: all drawn
			// at the cell, all misplaced by the same reordering.
			out = append(out, sgrParam{text: fields[i], cellPainted: true})
		case n == 49:
			// The terminal's own background: nothing has been painted, so there
			// is nothing here to draw in its place. It still goes, since asking
			// for it past the drift would clear whatever cell it lands on.
			out = append(out, sgrParam{
				text: fields[i], cellPainted: true, blackGround: true,
			})
		case n == 0:
			out = append(out, sgrParam{text: fields[i], reset: true})
		default:
			out = append(out, sgrParam{text: fields[i]})
		}
	}
	return out
}

// lineDriftsFill reports whether this line carries a right-to-left run whose
// marks survive folding, so a host that miscounts a fill stops placing one
// correctly partway along it.
//
// It is the LINE's answer rather than a position's, for the things drawn after
// all of the content -- the mirrored gutter, which on such a row is past the
// drift wherever the drift began.
func (sr *ScreenRenderer) lineDriftsFill(w *viewport.Viewport, line string) bool {
	if !sr.frame.flipRideSafe || w.ViewState.SuppressRTLCombining {
		return false
	}
	folding := modeFoldsMarks(sr.frame.rtlMarkMode)
	for _, give := range khatool.UnplaceableFillAfterFold(
		[]rune(line), folding, isZeroWidthMark, nil) {
		if give {
			return true
		}
	}
	return false
}
