package style

import (
	"strconv"
	"strings"
)

// This file holds the standard ANSI/EGA/VGA color definitions the app
// renders with. They are defined as one unified base set with optional
// per-theme (dark/light) overrides that cascade over it, mirroring the
// config-file shape used elsewhere in this family of software. For now
// the two themes are resolved into flat sets here; a config-file loader
// can later overwrite TermPaletteDark / TermPaletteLight before a theme
// is selected, and SetActiveTermTheme toggles between them.

// TermRGB is a plain 24-bit color for the palette definitions,
// independent of the indexed Color type.
type TermRGB struct{ R, G, B uint8 }

// TermPalette is a fully resolved theme: the 16 standard colors in
// VGA/EGA index order (0 black, 1 dark blue, 2 dark green, 3 dark cyan,
// 4 dark red, 5 purple, 6 brown, 7 silver, 8 dark gray, 9 bright blue,
// 10 bright green, 11 bright cyan, 12 bright red, 13 pink, 14 yellow,
// 15 white - the order the config lists them, NOT ANSI SGR order), plus
// the default terminal background and foreground.
type TermPalette struct {
	Colors     [16]TermRGB
	Background TermRGB
	Foreground TermRGB
}

// vgaToANSI maps a VGA/CGA palette index to its ANSI SGR index; red and
// blue swap places between the two orderings. It is its own inverse.
var vgaToANSI = [16]int{0, 4, 2, 6, 1, 5, 3, 7, 8, 12, 10, 14, 9, 13, 11, 15}

// ANSIColors returns the 16 colors reordered into ANSI SGR index order
// (0 black, 1 red, 2 green, 3 yellow, 4 blue, 5 magenta, 6 cyan,
// 7 white, then the bright variants) - the order the indexed Color type
// and the raster backend index by.
func (p TermPalette) ANSIColors() [16]TermRGB {
	var out [16]TermRGB
	for ansiIdx := 0; ansiIdx < 16; ansiIdx++ {
		out[ansiIdx] = p.Colors[vgaToANSI[ansiIdx]]
	}
	return out
}

// hexRGB parses a "#RRGGBB" string. Malformed input yields black.
func hexRGB(s string) TermRGB {
	v, err := strconv.ParseUint(strings.TrimPrefix(s, "#"), 16, 32)
	if err != nil {
		return TermRGB{}
	}
	return TermRGB{uint8(v >> 16), uint8(v >> 8), uint8(v)}
}

// termBase is the unified base palette (VGA order) that both themes
// cascade their overrides over.
var termBase = [16]string{
	"#000000", "#0000AA", "#00AA00", "#00AAAA", // black, dark blue, dark green, dark cyan
	"#C30E49", "#AA00AA", "#AA5500", "#AAAAAA", // dark red, purple, brown, silver
	"#555555", "#5555FF", "#55FF55", "#55FFFF", // dark gray, bright blue, bright green, bright cyan
	"#FF5555", "#FF55FF", "#FFFF55", "#FFFFFF", // bright red, pink, yellow, white
}

// resolveTermPalette applies sparse per-theme color overrides (keyed by
// VGA index) onto the base and sets the theme's background/foreground.
func resolveTermPalette(colorOverrides map[int]string, bg, fg string) TermPalette {
	var p TermPalette
	for i := 0; i < 16; i++ {
		p.Colors[i] = hexRGB(termBase[i])
	}
	for i, hex := range colorOverrides {
		if i >= 0 && i < 16 {
			p.Colors[i] = hexRGB(hex)
		}
	}
	p.Background = hexRGB(bg)
	p.Foreground = hexRGB(fg)
	return p
}

// TermTheme selects which resolved palette is active.
type TermTheme int

const (
	TermThemeDark TermTheme = iota
	TermThemeLight
)

var (
	// TermPaletteDark / TermPaletteLight are the two default resolved
	// theme sets (base + the respective overrides). A config loader can
	// overwrite these before a theme is selected.
	TermPaletteDark = resolveTermPalette(map[int]string{
		1:  "#1846C8", // dark blue
		6:  "#A8501E", // brown
		9:  "#828CFF", // bright blue
		14: "#FFFF00", // yellow
	}, "#001E18", "#D4D4D4")

	TermPaletteLight = resolveTermPalette(map[int]string{
		1:  "#4460B2", // dark blue
		5:  "#A811C8", // purple
		6:  "#B68467", // brown
		8:  "#6F6F6F", // dark gray
		10: "#64E664", // bright green
		11: "#55E6FF", // bright cyan
		12: "#FF505A", // bright red
		14: "#ECC855", // yellow
	}, "#FFFBFB", "#1E1E1E")

	// ActiveTermPalette is the set the app currently renders with. The
	// default theme is dark (term_theme: "dark"); SetActiveTermTheme
	// copies the appropriate palette here.
	ActiveTermPalette = TermPaletteDark

	activeTermTheme = TermThemeDark
)

// activeTermANSI caches ActiveTermPalette reordered into ANSI SGR index
// order, so the renderer resolves a standard color with a single array
// read (no per-lookup reorder). Kept in step with ActiveTermPalette.
var activeTermANSI = ActiveTermPalette.ANSIColors()

// ActiveTermANSIColor returns the active palette's color for ANSI SGR
// index i (0-15). The renderer calls this to resolve standard colors,
// so switching themes takes effect on the next paint of any surface.
func ActiveTermANSIColor(i int) TermRGB {
	if i < 0 || i > 15 {
		return TermRGB{}
	}
	return activeTermANSI[i]
}

// SetActiveTermTheme copies the dark or light palette into the active
// set. The config toggle (and the demo's View menu) call this to switch
// themes at runtime; the change shows on the next repaint.
func SetActiveTermTheme(theme TermTheme) {
	activeTermTheme = theme
	if theme == TermThemeLight {
		ActiveTermPalette = TermPaletteLight
	} else {
		ActiveTermPalette = TermPaletteDark
	}
	activeTermANSI = ActiveTermPalette.ANSIColors()
}

// ActiveTermTheme reports the currently selected theme.
func ActiveTermTheme() TermTheme { return activeTermTheme }
