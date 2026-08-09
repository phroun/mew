package core

import (
	"math"
	"testing"
)

// Each mode sizes one copy against the surface. The tile is 100x50 (2:1)
// and the surface 400x400 (1:1), so aspect-preserving modes must differ
// from each other and only stretch may distort.
func TestWallpaperModeSizing(t *testing.T) {
	const tw, th, sw, sh = 100, 50, 400, 400

	for _, tc := range []struct {
		mode         WallpaperMode
		wantW, wantH int
		note         string
	}{
		{WallpaperNatural, 100, 50, "the image's own size"},
		{WallpaperFitBoth, 400, 200, "widest that still fits inside"},
		{WallpaperFitWidth, 400, 200, "width matches, aspect kept"},
		{WallpaperFitHeight, 800, 400, "height matches, aspect kept"},
		{WallpaperCover, 800, 400, "smallest that covers, overflow cropped"},
		{WallpaperStretch, 400, 400, "each axis filled independently"},
	} {
		t.Run(WallpaperModeNames[tc.mode], func(t *testing.T) {
			l := WallpaperLayout{Mode: tc.mode, Scale: 1, Align: CenterAlignment}
			w, h, _, _ := l.Resolve(tw, th, sw, sh)
			if w != tc.wantW || h != tc.wantH {
				t.Errorf("drawn %dx%d, want %dx%d (%s)", w, h, tc.wantW, tc.wantH, tc.note)
			}
		})
	}

	// FitBoth never exceeds the surface; Cover never falls short of it.
	fit := WallpaperLayout{Mode: WallpaperFitBoth, Scale: 1}
	w, h, _, _ := fit.Resolve(tw, th, sw, sh)
	if w > sw || h > sh {
		t.Errorf("fit_both drew %dx%d, larger than the %dx%d surface", w, h, sw, sh)
	}
	cover := WallpaperLayout{Mode: WallpaperCover, Scale: 1}
	w, h, _, _ = cover.Resolve(tw, th, sw, sh)
	if w < sw || h < sh {
		t.Errorf("cover drew %dx%d, smaller than the %dx%d surface", w, h, sw, sh)
	}
}

// Scale multiplies whatever the mode arrived at, so it composes with
// every mode rather than being a mode of its own.
func TestWallpaperScaleMultipliesTheMode(t *testing.T) {
	half := WallpaperLayout{Mode: WallpaperNatural, Scale: 0.5}
	if w, h, _, _ := half.Resolve(100, 50, 400, 400); w != 50 || h != 25 {
		t.Errorf("natural at 0.5 = %dx%d, want 50x25", w, h)
	}

	fitHalf := WallpaperLayout{Mode: WallpaperFitBoth, Scale: 0.5}
	if w, h, _, _ := fitHalf.Resolve(100, 50, 400, 400); w != 200 || h != 100 {
		t.Errorf("fit_both at 0.5 = %dx%d, want half the fitted 400x200", w, h)
	}

	// A missing or nonsense scale means 1, not a vanished wallpaper.
	for _, s := range []float64{0, -1} {
		l := WallpaperLayout{Mode: WallpaperNatural, Scale: s}
		if w, h, _, _ := l.Resolve(100, 50, 400, 400); w != 100 || h != 50 {
			t.Errorf("scale %v = %dx%d, want the unscaled 100x50", s, w, h)
		}
	}

	// And a scale small enough to round to nothing still leaves a pixel
	// to sample and repeat.
	tiny := WallpaperLayout{Mode: WallpaperNatural, Scale: 0.0001}
	if w, h, _, _ := tiny.Resolve(100, 50, 400, 400); w < 1 || h < 1 {
		t.Errorf("a tiny scale collapsed the tile to %dx%d", w, h)
	}
}

// The anchor is a fraction of the slack on each axis, so 0 pins to
// left/top, 1 to right/bottom, and anything between lands proportionally.
func TestWallpaperAlignmentAnchors(t *testing.T) {
	l := WallpaperLayout{Mode: WallpaperNatural, Scale: 1}
	// A 100-wide copy in a 400-wide surface leaves 300 of slack.
	for _, tc := range []struct {
		anchor float64
		wantX  int
	}{
		{0, 0}, {0.5, 150}, {1, 300}, {0.25, 75},
	} {
		l.Align = WallpaperAlignment{X: tc.anchor, Y: tc.anchor}
		_, _, x, y := l.Resolve(100, 50, 400, 400)
		if x != tc.wantX {
			t.Errorf("anchor %v: x = %d, want %d", tc.anchor, x, tc.wantX)
		}
		// The Y axis has 350 of slack, so it must NOT match X — that
		// would mean the axes were sharing a computation.
		if wantY := int(math.Round(float64(350) * tc.anchor)); y != wantY {
			t.Errorf("anchor %v: y = %d, want %d", tc.anchor, y, wantY)
		}
	}

	// A copy LARGER than the surface has negative slack: the anchor
	// splits the overflow, so 0.5 crops evenly on both sides.
	l.Align = CenterAlignment
	_, _, x, _ := l.Resolve(600, 50, 400, 400)
	if x != -100 {
		t.Errorf("an oversized copy centred at x = %d, want -100 (evenly cropped)", x)
	}
}

func TestParseWallpaperAlignment(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want WallpaperAlignment
	}{
		{"", CenterAlignment},
		{"center", CenterAlignment},
		{"top left", WallpaperAlignment{X: 0, Y: 0}},
		{"left top", WallpaperAlignment{X: 0, Y: 0}}, // order does not matter
		{"bottom right", WallpaperAlignment{X: 1, Y: 1}},
		{"top", WallpaperAlignment{X: 0.5, Y: 0}}, // unmentioned axis stays centred
		{"right", WallpaperAlignment{X: 1, Y: 0.5}},
		{"0 1", WallpaperAlignment{X: 0, Y: 1}}, // numbers fill X then Y
		{"0.25 0.75", WallpaperAlignment{X: 0.25, Y: 0.75}},
		{"top-left", WallpaperAlignment{X: 0, Y: 0}}, // dash separated
		{"bottom, center", WallpaperAlignment{X: 0.5, Y: 1}},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := ParseWallpaperAlignment(tc.in)
			if !ok {
				t.Fatalf("ParseWallpaperAlignment(%q) reported failure", tc.in)
			}
			if got != tc.want {
				t.Errorf("= %+v, want %+v", got, tc.want)
			}
		})
	}

	for _, bad := range []string{"sideways", "top diagonal", "0.1 0.2 0.3"} {
		if _, ok := ParseWallpaperAlignment(bad); ok {
			t.Errorf("ParseWallpaperAlignment(%q) accepted a name it cannot honour", bad)
		}
	}
}

func TestParseWallpaperMode(t *testing.T) {
	for in, want := range map[string]WallpaperMode{
		"":           WallpaperNatural,
		"natural":    WallpaperNatural,
		"1:1":        WallpaperNatural,
		"fit_both":   WallpaperFitBoth,
		"contain":    WallpaperFitBoth,
		"fit_width":  WallpaperFitWidth,
		"fit_height": WallpaperFitHeight,
		"cover":      WallpaperCover,
		"stretch":    WallpaperStretch,
	} {
		if got, ok := ParseWallpaperMode(in); !ok || got != want {
			t.Errorf("ParseWallpaperMode(%q) = %v ok=%v, want %v", in, got, ok, want)
		}
	}
	if _, ok := ParseWallpaperMode("squish"); ok {
		t.Error("an unknown mode was accepted; the caller cannot then report it")
	}
}

func TestParseWallpaperTilingAndFilter(t *testing.T) {
	for in, want := range map[string][2]bool{
		"":           {true, true},
		"both":       {true, true},
		"none":       {false, false},
		"horizontal": {true, false},
		"vertical":   {false, true},
	} {
		tiling, ok := ParseWallpaperTiling(in)
		if !ok {
			t.Fatalf("ParseWallpaperTiling(%q) reported failure", in)
		}
		if x, y := tiling.Axes(); x != want[0] || y != want[1] {
			t.Errorf("tiling %q = (%v,%v), want (%v,%v)", in, x, y, want[0], want[1])
		}
	}
	if _, ok := ParseWallpaperTiling("diagonal"); ok {
		t.Error("an unknown tiling was accepted")
	}

	// Crisp is the default: the built-in wallpaper is an 8x8 bitmap at
	// 1:1, which interpolation only blurs.
	if smooth, ok := ParseWallpaperFilter(""); !ok || smooth {
		t.Errorf("default filter = smooth %v ok %v, want crisp", smooth, ok)
	}
	if smooth, ok := ParseWallpaperFilter("smooth"); !ok || !smooth {
		t.Errorf("smooth filter = %v ok %v", smooth, ok)
	}
	if _, ok := ParseWallpaperFilter("blurry"); ok {
		t.Error("an unknown filter was accepted")
	}
}

// A degenerate tile or surface resolves to nothing rather than dividing
// by zero.
func TestWallpaperResolveDegenerate(t *testing.T) {
	l := DefaultWallpaperLayout
	for _, tc := range [][4]int{{0, 50, 400, 400}, {100, 0, 400, 400}, {100, 50, 0, 400}, {100, 50, 400, 0}} {
		if w, h, _, _ := l.Resolve(tc[0], tc[1], tc[2], tc[3]); w != 0 || h != 0 {
			t.Errorf("Resolve%v = %dx%d, want 0x0", tc, w, h)
		}
	}
}
