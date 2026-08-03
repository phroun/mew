package raster

import (
	"image"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// topLeftTiling is the layout the origin-anchored assertions below
// assume: the image at its own size, repeating both ways, phase pinned
// to the corner. The DEFAULT alignment is centred, which is the right
// default but shifts a pattern's phase — so these tests say what they
// mean rather than inheriting it.
var topLeftTiling = core.WallpaperLayout{
	Mode:   core.WallpaperNatural,
	Tiling: core.WallpaperTileBoth,
	Scale:  1,
	Align:  core.WallpaperAlignment{X: 0, Y: 0},
}

// checkerTile builds a w x h tile whose pixel (x,y) has red = x + y*w,
// so a test can tell exactly which texel landed where.
func checkerTile(w, h int) *image.RGBA {
	t := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			o := t.PixOffset(x, y)
			t.Pix[o+0] = uint8(x + y*w)
			t.Pix[o+3] = 255
		}
	}
	return t
}

// The tile repeats across the rect, anchored at the surface origin —
// the same anchoring the compositor's repeat sampler produces, so the
// software and GPU paths show the wallpaper in the same place.
func TestTileImagePxRepeatsFromOrigin(t *testing.T) {
	b, err := New(40, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tile := checkerTile(4, 4)
	b.TileImagePx(core.UnitRect{Width: 40, Height: 24}, tile, topLeftTiling)

	for _, pt := range [][2]int{{0, 0}, {3, 2}, {4, 0}, {7, 3}, {39, 23}, {17, 9}} {
		x, y := pt[0], pt[1]
		want := uint8((x % 4) + (y%4)*4)
		if got := lum(b, x, y); got != int(want) {
			t.Errorf("pixel (%d,%d) = %d, want %d (tile texel %d,%d)",
				x, y, got, want, x%4, y%4)
		}
	}
}

// A rect that is not a whole number of tiles across simply stops
// mid-tile — the last row and column are partial, exactly as a repeat
// sampler leaves them.
func TestTileImagePxHandlesPartialTiles(t *testing.T) {
	b, err := New(10, 6)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b.Clear(style.CellStyle{Bg: style.RGB(255, 255, 255)})
	b.TileImagePx(core.UnitRect{Width: 10, Height: 6}, checkerTile(4, 4), topLeftTiling)

	// 10 is 2.5 tiles: the last column is texel column 1 of a third tile.
	if got, want := lum(b, 9, 0), 1; got != want {
		t.Errorf("pixel (9,0) = %d, want %d", got, want)
	}
	if got, want := lum(b, 5, 5), (1 + 1*4); got != want {
		t.Errorf("pixel (5,5) = %d, want %d", got, want)
	}
}

// Tiling respects the clip, so a wallpaper drawn into a clipped painter
// cannot spill past it.
func TestTileImagePxRespectsClip(t *testing.T) {
	b, err := New(40, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b.Clear(style.CellStyle{Bg: style.RGB(200, 200, 200)})
	b.SetClip(core.UnitRect{Width: 16, Height: 24})
	b.TileImagePx(core.UnitRect{Width: 40, Height: 24}, checkerTile(4, 4), topLeftTiling)

	if got := lum(b, 20, 5); got != 200 {
		t.Errorf("pixel outside the clip = %d, want the untouched 200", got)
	}
	if got := lum(b, 5, 5); got == 200 {
		t.Error("pixel inside the clip was not tiled")
	}
}

// A nil or empty tile draws nothing rather than panicking.
func TestTileImagePxDegenerate(t *testing.T) {
	b, err := New(16, 16)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b.Clear(style.CellStyle{Bg: style.RGB(255, 255, 255)})
	b.TileImagePx(core.UnitRect{Width: 16, Height: 16}, image.NewRGBA(image.Rect(0, 0, 0, 0)), topLeftTiling)
	if got := lum(b, 8, 8); got != 255 {
		t.Errorf("pixel = %d, want 255 (nothing drawn for an empty tile)", got)
	}
}

// ClearTransparent stays INSIDE the clip. A frame repainting only its
// damaged region gets a clipped painter, so a clear that ran wholesale
// would erase chrome the frame is not going to redraw — which is exactly
// how the menu bar and status bar flickered out, with the wallpaper
// showing through where they had been.
func TestClearTransparentRespectsClip(t *testing.T) {
	b, err := New(32, 32)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b.Clear(style.CellStyle{Bg: style.RGB(255, 255, 255)})
	b.SetClip(core.UnitRect{Width: 8, Height: 8})
	b.ClearTransparent()

	for _, pt := range [][2]int{{0, 0}, {4, 4}, {7, 7}} {
		if a := b.img.Pix[b.img.PixOffset(pt[0], pt[1])+3]; a != 0 {
			t.Errorf("pixel (%d,%d) inside the clip: alpha = %d, want 0", pt[0], pt[1], a)
		}
	}
	for _, pt := range [][2]int{{8, 0}, {20, 20}, {31, 31}} {
		if a := b.img.Pix[b.img.PixOffset(pt[0], pt[1])+3]; a != 255 {
			t.Errorf("pixel (%d,%d) outside the clip: alpha = %d, want it untouched (255) — "+
				"a damage-clipped frame would not repaint it", pt[0], pt[1], a)
		}
	}
}

// With no clip set it still clears everything.
func TestClearTransparentClearsAllWhenUnclipped(t *testing.T) {
	b, err := New(32, 32)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b.Clear(style.CellStyle{Bg: style.RGB(255, 255, 255)})
	b.ClearTransparent()

	for _, pt := range [][2]int{{0, 0}, {20, 20}, {31, 31}} {
		if a := b.img.Pix[b.img.PixOffset(pt[0], pt[1])+3]; a != 0 {
			t.Errorf("pixel (%d,%d) alpha = %d, want 0", pt[0], pt[1], a)
		}
	}
}

// The backend advertises both capabilities the painter looks for.
func TestBackendImplementsWallpaperCapabilities(t *testing.T) {
	b, err := New(16, 16)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var _ core.ImageTiler = b
	var _ core.SurfaceClearer = b
}

// An axis that does not tile shows ONE copy and leaves the rest of the
// surface alone — the wallpaper does not wrap round to fill it.
func TestTileImagePxHonoursTilingAxes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		tiling      core.WallpaperTiling
		insideRight bool // is x=30,y=2 painted?
		insideBelow bool // is x=2,y=18 painted?
	}{
		{"both", core.WallpaperTileBoth, true, true},
		{"none", core.WallpaperTileNone, false, false},
		{"horizontal", core.WallpaperTileHorizontal, true, false},
		{"vertical", core.WallpaperTileVertical, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := New(40, 24)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			b.Clear(style.CellStyle{Bg: style.RGB(200, 200, 200)})

			layout := topLeftTiling
			layout.Tiling = tc.tiling
			b.TileImagePx(core.UnitRect{Width: 40, Height: 24}, checkerTile(4, 4), layout)

			// The reference copy itself is always painted.
			if got := lum(b, 1, 1); got == 200 {
				t.Error("the reference copy was not painted at all")
			}
			if painted := lum(b, 30, 2) != 200; painted != tc.insideRight {
				t.Errorf("x=30 painted = %v, want %v", painted, tc.insideRight)
			}
			if painted := lum(b, 2, 18) != 200; painted != tc.insideBelow {
				t.Errorf("y=18 painted = %v, want %v", painted, tc.insideBelow)
			}
		})
	}
}

// Alignment anchors the single copy. With tiling off, a 4x4 tile in a
// 40x24 surface lands left, centred or right as the anchor says.
func TestTileImagePxAlignsTheCopy(t *testing.T) {
	for _, tc := range []struct {
		anchor float64
		wantX  int
	}{
		{0, 0},    // left edge
		{0.5, 18}, // (40-4)/2
		{1, 36},   // flush right
	} {
		b, err := New(40, 24)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		b.Clear(style.CellStyle{Bg: style.RGB(200, 200, 200)})

		layout := core.WallpaperLayout{
			Mode:   core.WallpaperNatural,
			Tiling: core.WallpaperTileNone,
			Scale:  1,
			Align:  core.WallpaperAlignment{X: tc.anchor, Y: 0},
		}
		b.TileImagePx(core.UnitRect{Width: 40, Height: 24}, checkerTile(4, 4), layout)

		// Texel (0,0) of the tile has red 0, and the background is 200,
		// so the copy's own left column is findable.
		if got := lum(b, tc.wantX, 0); got != 0 {
			t.Errorf("anchor %v: pixel at x=%d = %d, want the tile's first texel (0)",
				tc.anchor, tc.wantX, got)
		}
		if tc.wantX > 0 && lum(b, tc.wantX-1, 0) != 200 {
			t.Errorf("anchor %v: painted left of the copy at x=%d", tc.anchor, tc.wantX-1)
		}
	}
}

// Scale resamples the tile. At 2x, each source texel covers a 2x2 block,
// so the copy is twice as wide and neighbouring pixels repeat.
func TestTileImagePxScales(t *testing.T) {
	b, err := New(40, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	layout := topLeftTiling
	layout.Scale = 2
	b.TileImagePx(core.UnitRect{Width: 40, Height: 24}, checkerTile(4, 4), layout)

	// Source texel (0,0) is red 0 and (1,0) is red 1. Doubled, x=0..1
	// are texel 0 and x=2..3 are texel 1.
	if lum(b, 0, 0) != 0 || lum(b, 1, 0) != 0 {
		t.Errorf("2x: x=0,1 = %d,%d, want both texel 0", lum(b, 0, 0), lum(b, 1, 0))
	}
	if lum(b, 2, 0) != 1 || lum(b, 3, 0) != 1 {
		t.Errorf("2x: x=2,3 = %d,%d, want both texel 1", lum(b, 2, 0), lum(b, 3, 0))
	}
	// And the scaled copy repeats every 8 px rather than every 4.
	if lum(b, 8, 0) != 0 {
		t.Errorf("2x: x=8 = %d, want the scaled copy to repeat there", lum(b, 8, 0))
	}
}

// Stretch fills the surface with exactly one copy, whatever the tile's
// aspect — the only mode that distorts.
func TestTileImagePxStretchFillsTheSurface(t *testing.T) {
	b, err := New(40, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b.Clear(style.CellStyle{Bg: style.RGB(200, 200, 200)})

	layout := core.WallpaperLayout{
		Mode:   core.WallpaperStretch,
		Tiling: core.WallpaperTileNone,
		Scale:  1,
		Align:  core.CenterAlignment,
	}
	b.TileImagePx(core.UnitRect{Width: 40, Height: 24}, checkerTile(4, 4), layout)

	for _, pt := range [][2]int{{0, 0}, {39, 0}, {0, 23}, {39, 23}, {20, 12}} {
		if lum(b, pt[0], pt[1]) == 200 {
			t.Errorf("stretch left (%d,%d) unpainted; it must cover the surface", pt[0], pt[1])
		}
	}
}
