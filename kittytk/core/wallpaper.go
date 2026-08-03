package core

import (
	"math"
	"strconv"
	"strings"
)

// WallpaperMode says how a wallpaper tile is SIZED against the surface.
// Repetition is a separate question — see WallpaperLayout.TileX/TileY —
// so "tile at natural size, repeating" is WallpaperNatural with tiling
// on, and "one copy stretched to fill" is WallpaperStretch with it off.
type WallpaperMode int

const (
	// WallpaperNatural draws the tile at its own pixel size. The
	// default, and the only mode where the image's resolution decides
	// how big it looks.
	WallpaperNatural WallpaperMode = iota

	// WallpaperFitBoth scales the whole tile to fit INSIDE the surface,
	// aspect preserved. Nothing is cropped; the surface may show
	// through along one pair of edges unless that axis tiles.
	WallpaperFitBoth

	// WallpaperFitWidth scales so the tile's width matches the
	// surface's, aspect preserved. The height falls where it may.
	WallpaperFitWidth

	// WallpaperFitHeight is WallpaperFitWidth on the other axis.
	WallpaperFitHeight

	// WallpaperCover scales the smallest amount that still covers the
	// whole surface, aspect preserved. The overflow is cropped — the
	// counterpart of FitBoth, which letterboxes instead.
	WallpaperCover

	// WallpaperStretch scales each axis independently to fill the
	// surface exactly. The only mode that distorts the image.
	WallpaperStretch
)

// WallpaperModeNames maps each mode to the name configuration uses.
var WallpaperModeNames = map[WallpaperMode]string{
	WallpaperNatural:   "natural",
	WallpaperFitBoth:   "fit_both",
	WallpaperFitWidth:  "fit_width",
	WallpaperFitHeight: "fit_height",
	WallpaperCover:     "cover",
	WallpaperStretch:   "stretch",
}

// ParseWallpaperMode reads a configured mode name. ok is false for an
// unknown one, so a caller can keep its default and say so rather than
// silently papering the desktop differently than asked.
func ParseWallpaperMode(s string) (WallpaperMode, bool) {
	switch s {
	case "", "natural", "actual", "original", "1:1":
		// NOT "tile": repetition is its own setting now, and a mode
		// named for it would suggest otherwise.
		return WallpaperNatural, true
	case "fit_both", "fit", "contain":
		return WallpaperFitBoth, true
	case "fit_width":
		return WallpaperFitWidth, true
	case "fit_height":
		return WallpaperFitHeight, true
	case "cover", "fill":
		return WallpaperCover, true
	case "stretch":
		return WallpaperStretch, true
	}
	return WallpaperNatural, false
}

// WallpaperTiling says which axes a wallpaper repeats along.
type WallpaperTiling int

const (
	WallpaperTileBoth WallpaperTiling = iota
	WallpaperTileNone
	WallpaperTileHorizontal
	WallpaperTileVertical
)

// ParseWallpaperTiling reads a configured tiling name.
func ParseWallpaperTiling(s string) (WallpaperTiling, bool) {
	switch s {
	case "", "both", "yes", "true":
		return WallpaperTileBoth, true
	case "none", "no", "false", "off":
		return WallpaperTileNone, true
	case "horizontal", "x":
		return WallpaperTileHorizontal, true
	case "vertical", "y":
		return WallpaperTileVertical, true
	}
	return WallpaperTileBoth, false
}

// Axes reports whether this tiling repeats horizontally and vertically.
func (t WallpaperTiling) Axes() (x, y bool) {
	switch t {
	case WallpaperTileNone:
		return false, false
	case WallpaperTileHorizontal:
		return true, false
	case WallpaperTileVertical:
		return false, true
	}
	return true, true
}

// ParseWallpaperFilter reads the filter name. "crisp" keeps hard pixel
// edges (nearest neighbour), "smooth" interpolates (bilinear). smooth is
// what the return value reports.
//
// Crisp is the default because the built-in wallpaper is an 8x8 bitmap
// pattern drawn at 1:1, where interpolation only blurs it.
func ParseWallpaperFilter(s string) (smooth bool, ok bool) {
	switch s {
	case "", "crisp", "nearest", "pixel", "sharp":
		return false, true
	case "smooth", "linear", "bilinear":
		return true, true
	}
	return false, false
}

// WallpaperAlignment anchors a wallpaper copy, as a FRACTION per axis:
// 0 is left/top, 0.5 centre/middle, 1 right/bottom. Fractions rather
// than a left/centre/right enum because the in-between values are free
// and occasionally exactly what someone wants — 0.25 is a quarter of the
// slack, which no set of names would offer.
type WallpaperAlignment struct{ X, Y float64 }

// CenterAlignment is the default: the copy centred on both axes.
var CenterAlignment = WallpaperAlignment{X: 0.5, Y: 0.5}

// ParseWallpaperAlignment reads either names or numbers, in any order:
//
//	center            top left        bottom          right top
//	0.5 0.5           0 0             0.5 1           1 0
//	left 0.25         (named axis, numeric for the other)
//
// Names set their own axis; numbers fill X then Y in the order written.
// An axis nobody mentions stays centred. Separators are spaces, commas
// or dashes, and values are clamped to [0,1] — this positions a copy
// within the surface, so outside that range means nothing.
func ParseWallpaperAlignment(s string) (WallpaperAlignment, bool) {
	a := CenterAlignment
	numeric := 0
	fields := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(s)), func(r rune) bool {
		return r == ' ' || r == ',' || r == '-' || r == '_' || r == '\t'
	})
	for _, f := range fields {
		switch f {
		case "left":
			a.X = 0
		case "right":
			a.X = 1
		case "top":
			a.Y = 0
		case "bottom":
			a.Y = 1
		case "center", "centre", "middle":
			// Already the default on both axes; naming one explicitly
			// ("top center") just leaves the other alone.
		default:
			v, err := strconv.ParseFloat(f, 64)
			if err != nil {
				return CenterAlignment, false
			}
			v = math.Min(math.Max(v, 0), 1)
			switch numeric {
			case 0:
				a.X = v
			case 1:
				a.Y = v
			default:
				return CenterAlignment, false // more numbers than axes
			}
			numeric++
		}
	}
	return a, true
}

// offset places a copy of size draw within a span of size surface.
// Negative slack (a copy larger than the surface) works out too: the
// overflow splits by the same fraction, so 0.5 crops evenly and 0 crops
// entirely off the right/bottom.
func alignOffset(anchor float64, draw, surface int) int {
	return int(math.Round(float64(surface-draw) * anchor))
}

// WallpaperLayout is the whole description of how a tile covers a
// surface: how big to draw one copy, whether to repeat it, and how to
// filter it when the drawn size is not the image's own.
type WallpaperLayout struct {
	Mode   WallpaperMode
	Tiling WallpaperTiling

	// Scale multiplies whatever size Mode arrives at. 1 leaves it alone;
	// 0.5 halves it; 0 or negative is treated as 1 rather than making
	// the wallpaper vanish.
	Scale float64

	// Align anchors the reference copy on each axis. On an axis that
	// TILES this sets the repetition's phase rather than confining it —
	// the copies march outward from here in both directions.
	Align WallpaperAlignment

	// Smooth interpolates when the drawn size differs from the image's
	// (see ParseWallpaperFilter).
	Smooth bool
}

// DefaultWallpaperLayout is the historical behaviour: the tile at its
// own size, repeating on both axes, unfiltered.
var DefaultWallpaperLayout = WallpaperLayout{
	Mode:   WallpaperNatural,
	Tiling: WallpaperTileBoth,
	Scale:  1,
	Align:  CenterAlignment,
}

// Resolve computes where one copy of the tile lands on the surface, in
// pixels: its drawn size, and the origin of the copy the rest repeat
// from.
//
// Align decides the origin on BOTH axes, tiling or not. Where an axis
// does not tile that places the single copy; where it does, it sets the
// phase the repetition marches out from. One rule either way, rather
// than a special case that only shows up once someone turns tiling off.
func (l WallpaperLayout) Resolve(tileW, tileH, surfaceW, surfaceH int) (drawW, drawH, originX, originY int) {
	if tileW <= 0 || tileH <= 0 || surfaceW <= 0 || surfaceH <= 0 {
		return 0, 0, 0, 0
	}

	tw, th := float64(tileW), float64(tileH)
	sw, sh := float64(surfaceW), float64(surfaceH)

	var w, h float64
	switch l.Mode {
	case WallpaperFitBoth:
		k := math.Min(sw/tw, sh/th)
		w, h = tw*k, th*k
	case WallpaperFitWidth:
		k := sw / tw
		w, h = tw*k, th*k
	case WallpaperFitHeight:
		k := sh / th
		w, h = tw*k, th*k
	case WallpaperCover:
		k := math.Max(sw/tw, sh/th)
		w, h = tw*k, th*k
	case WallpaperStretch:
		w, h = sw, sh
	default: // WallpaperNatural
		w, h = tw, th
	}

	scale := l.Scale
	if scale <= 0 {
		scale = 1
	}
	w, h = w*scale, h*scale

	drawW = int(math.Round(w))
	drawH = int(math.Round(h))
	// A tile has to be at least one pixel on each axis or there is
	// nothing to sample and nothing to repeat.
	if drawW < 1 {
		drawW = 1
	}
	if drawH < 1 {
		drawH = 1
	}

	return drawW, drawH,
		alignOffset(l.Align.X, drawW, surfaceW),
		alignOffset(l.Align.Y, drawH, surfaceH)
}
