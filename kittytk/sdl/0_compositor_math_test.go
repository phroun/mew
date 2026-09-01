package sdl

import (
	"image"
	"math"
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
)

func almostEqual(a, b float32) bool {
	d := a - b
	return d < 1e-5 && d > -1e-5
}

// windowNDC places a child window's unit bounds on the surface's NDC
// quad. The full surface maps to the (-1,-1) 2x2 quad; sub-rects scale
// and translate, with the unit y axis (down) flipped to NDC (up).
func TestWindowNDC(t *testing.T) {
	full := core.UnitSize{Width: 800, Height: 600}

	x, y, w, h := windowNDC(core.UnitRect{X: 0, Y: 0, Width: 800, Height: 600}, full)
	if !almostEqual(x, -1) || !almostEqual(y, -1) || !almostEqual(w, 2) || !almostEqual(h, 2) {
		t.Errorf("fullscreen = (%v,%v,%v,%v), want (-1,-1,2,2)", x, y, w, h)
	}

	// Top-left quadrant: upper half of NDC, left half of x.
	x, y, w, h = windowNDC(core.UnitRect{X: 0, Y: 0, Width: 400, Height: 300}, full)
	if !almostEqual(x, -1) || !almostEqual(y, 0) || !almostEqual(w, 1) || !almostEqual(h, 1) {
		t.Errorf("top-left quadrant = (%v,%v,%v,%v), want (-1,0,1,1)", x, y, w, h)
	}

	// Bottom-right quadrant.
	x, y, w, h = windowNDC(core.UnitRect{X: 400, Y: 300, Width: 400, Height: 300}, full)
	if !almostEqual(x, 0) || !almostEqual(y, -1) || !almostEqual(w, 1) || !almostEqual(h, 1) {
		t.Errorf("bottom-right quadrant = (%v,%v,%v,%v), want (0,-1,1,1)", x, y, w, h)
	}

	// A degenerate surface produces a degenerate quad, not NaN/Inf.
	x, y, w, h = windowNDC(core.UnitRect{X: 10, Y: 10, Width: 100, Height: 100}, core.UnitSize{})
	if x != 0 || y != 0 || w != 0 || h != 0 {
		t.Errorf("degenerate surface = (%v,%v,%v,%v), want zeros", x, y, w, h)
	}
}

// The same window bounds against a RESIZED surface must produce different
// NDC — this is the transform the compositor now refreshes every frame.
// Regression coverage for windows keeping stale scale/position after the
// desktop window was resized.
func TestWindowNDCTracksSurfaceResize(t *testing.T) {
	bounds := core.UnitRect{X: 100, Y: 100, Width: 200, Height: 150}

	x1, y1, w1, h1 := windowNDC(bounds, core.UnitSize{Width: 800, Height: 600})
	x2, y2, w2, h2 := windowNDC(bounds, core.UnitSize{Width: 1600, Height: 1200})

	if almostEqual(x1, x2) && almostEqual(y1, y2) && almostEqual(w1, w2) && almostEqual(h1, h2) {
		t.Error("NDC must change when the surface size changes; a stale uniform means a mis-scaled window")
	}
	// Doubling the surface halves the window's NDC extent.
	if !almostEqual(w2, w1/2) || !almostEqual(h2, h1/2) {
		t.Errorf("doubled surface: extent (%v,%v), want (%v,%v)", w2, h2, w1/2, h1/2)
	}
}

func TestOutsetBounds(t *testing.T) {
	got := outsetBounds(core.UnitRect{X: 10, Y: 20, Width: 30, Height: 40}, 2)
	want := core.UnitRect{X: 8, Y: 18, Width: 34, Height: 44}
	if got != want {
		t.Errorf("outsetBounds = %+v, want %+v", got, want)
	}
}

// An overlay's texture must describe the same physical size as the
// outset quad it is drawn onto, at ANY pixel density — otherwise the
// GPU stretches it (distorted glyphs) and the painted outer stroke
// falls off the texture's right/bottom edge. Regression for the padding
// being applied in raw pixels while the quad outset was in units.
func TestOverlayTexturePxMatchesOutsetQuad(t *testing.T) {
	bounds := core.UnitRect{X: 40, Y: 30, Width: 75, Height: 42}
	const pad = core.Unit(2)

	for _, ppu := range []float64{1.0, 2.0, 1.1666666} {
		w, h, padPxW, padPxH := overlayTexturePx(bounds, ppu, ppu, pad)

		outset := outsetBounds(bounds, pad)
		wantW := int(float64(outset.Width)*ppu + 0.5)
		wantH := int(float64(outset.Height)*ppu + 0.5)

		// Within a pixel of the quad's physical size (independent
		// roundings), never the old pixels-vs-units mismatch that grew
		// with ppu.
		if diff := w - wantW; diff < -1 || diff > 1 {
			t.Errorf("ppu=%v: texture width %d vs quad width %d", ppu, w, wantW)
		}
		if diff := h - wantH; diff < -1 || diff > 1 {
			t.Errorf("ppu=%v: texture height %d vs quad height %d", ppu, h, wantH)
		}

		// The padding scales with density: at 2x, a 2-unit pad is 4px.
		wantPad := int(float64(pad)*ppu + 0.5)
		if padPxW != wantPad || padPxH != wantPad {
			t.Errorf("ppu=%v: padPx = (%d,%d), want %d", ppu, padPxW, padPxH, wantPad)
		}
	}
}

// overlayNDC must pin an overlay quad to WHOLE pixels: the left edge maps
// back through the GPU's NDC→viewport transform to exactly leftPx, and the
// span is exactly wPx — regardless of whether the drawable width is odd (a
// half-pixel NDC centre) or the density fractional. This is the 1:1 blit
// that killed the doubled centre column in composited menus.
func TestOverlayNDCIsPixelExact(t *testing.T) {
	// The GPU maps NDC x in [-1,1] to pixel [0,drawW]: px = (x+1)/2 * drawW.
	toPx := func(ndcX float32, drawW int) float64 {
		return (float64(ndcX) + 1.0) / 2.0 * float64(drawW)
	}

	// Odd drawable width is the parity that put the surface centre on a
	// half-pixel; check a couple of placements including one straddling it.
	cases := []struct {
		leftPx, topPx, wPx, hPx, drawW, drawH int
	}{
		{leftPx: 0, topPx: 0, wPx: 200, hPx: 100, drawW: 1365, drawH: 767},
		{leftPx: 640, topPx: 300, wPx: 181, hPx: 240, drawW: 1365, drawH: 767},
		{leftPx: 683, topPx: 1, wPx: 2, hPx: 2, drawW: 1365, drawH: 767}, // straddles centre
		{leftPx: 12, topPx: 34, wPx: 300, hPx: 260, drawW: 800, drawH: 600},
	}
	for _, c := range cases {
		x, y, w, h := overlayNDC(c.leftPx, c.topPx, c.wPx, c.hPx, c.drawW, c.drawH)

		gotLeft := toPx(x, c.drawW)
		gotRight := toPx(x+w, c.drawW)
		if math.Abs(gotLeft-float64(c.leftPx)) > 1e-3 {
			t.Errorf("%+v: left edge maps to %v px, want %d", c, gotLeft, c.leftPx)
		}
		if math.Abs((gotRight-gotLeft)-float64(c.wPx)) > 1e-3 {
			t.Errorf("%+v: width spans %v px, want %d", c, gotRight-gotLeft, c.wPx)
		}
		// Top edge: NDC y in [-1,1] maps to pixel [drawH,0] (y flipped).
		gotTop := (1.0 - float64(y+h)) / 2.0 * float64(c.drawH)
		gotBot := (1.0 - float64(y)) / 2.0 * float64(c.drawH)
		if math.Abs(gotTop-float64(c.topPx)) > 1e-3 {
			t.Errorf("%+v: top edge maps to %v px, want %d", c, gotTop, c.topPx)
		}
		if math.Abs((gotBot-gotTop)-float64(c.hPx)) > 1e-3 {
			t.Errorf("%+v: height spans %v px, want %d", c, gotBot-gotTop, c.hPx)
		}
	}

	// Degenerate drawable: no divide-by-zero, just a zero quad.
	if x, y, w, h := overlayNDC(0, 0, 10, 10, 0, 0); x != 0 || y != 0 || w != 0 || h != 0 {
		t.Errorf("zero drawable = (%v,%v,%v,%v), want all zero", x, y, w, h)
	}
}

// scissorPx maps the client area (units) to a framebuffer scissor rect
// (pixels), scaled by density and clamped to the surface — the clip that
// keeps composited windows from painting over the status bar and dock.
func TestScissorPx(t *testing.T) {
	// A client area below a 20-unit menu bar and above an 80-unit
	// status/dock band, at 2x density on a 800x600-unit surface.
	area := core.UnitRect{X: 0, Y: 20, Width: 800, Height: 500}
	x, y, w, h, ok := scissorPx(area, 2, 2, 1600, 1200)
	if !ok || x != 0 || y != 40 || w != 1600 || h != 1000 {
		t.Errorf("scissor = (%d,%d %dx%d, ok=%v), want (0,40 1600x1000, true)", x, y, w, h, ok)
	}

	// Clamped to the surface even if the area overshoots.
	x, y, w, h, ok = scissorPx(core.UnitRect{X: -10, Y: -10, Width: 900, Height: 700}, 2, 2, 1600, 1200)
	if !ok || x != 0 || y != 0 || w != 1600 || h != 1200 {
		t.Errorf("overshoot clamp = (%d,%d %dx%d, ok=%v), want full surface", x, y, w, h, ok)
	}

	// A degenerate area reports not-ok rather than a zero-size scissor.
	if _, _, _, _, ok := scissorPx(core.UnitRect{X: 100, Y: 100}, 2, 2, 1600, 1200); ok {
		t.Error("empty area should not produce a scissor")
	}
}

// Shadow geometry: the quad covers the caster (plus anchor) shifted by
// the cast offset and outset by the blur; the SDF rects land inside it
// at pixel coordinates that keep the shadow visibly displaced.
func TestShadowQuadGeometry(t *testing.T) {
	caster := core.UnitRect{X: 100, Y: 100, Width: 200, Height: 150}
	anchor := core.UnitRect{X: 120, Y: 80, Width: 60, Height: 20}
	spec := shadowSpec{offsetX: 2, offsetY: 3, blur: 8, radius: 4, alpha: 0.35}

	// Union covers both rects.
	u := unionRect(caster, anchor)
	if u.X != 100 || u.Y != 80 || u.Width != 200 || u.Height != 170 {
		t.Errorf("union = %+v, want {100 80 200 170}", u)
	}
	// Empty rects are identities.
	if got := unionRect(caster, core.UnitRect{}); got != caster {
		t.Errorf("union with empty = %+v, want caster", got)
	}

	quad := shadowQuadBounds(caster, anchor, spec)
	want := core.UnitRect{X: 100 + 2 - 8, Y: 80 + 3 - 8, Width: 200 + 16, Height: 170 + 16}
	if quad != want {
		t.Errorf("shadow quad = %+v, want %+v", quad, want)
	}

	// The shifted caster maps into the quad with blur-sized margins at
	// density 2: its min corner sits blur*ppu inside on the axes the
	// union starts at.
	shifted := caster.Translated(spec.offsetX, spec.offsetY)
	minX, minY, maxX, maxY := rectPxIn(quad, shifted, 2, 2)
	if minX != 16 { // (caster.X+2 - quad.X) * 2 = blur*2
		t.Errorf("caster minX = %v, want 16", minX)
	}
	if minY != 56 { // caster is 20 units below the union top: (8+20)*2
		t.Errorf("caster minY = %v, want 56", minY)
	}
	if maxX-minX != 400 || maxY-minY != 300 {
		t.Errorf("caster extent = %vx%v px, want 400x300", maxX-minX, maxY-minY)
	}
}

// bgraPixels swaps R<->B, keeps G/A, and pads rows to the GPU's 256-byte
// upload alignment.
func TestBGRAPixels(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	// Pixel (0,0): R=1 G=2 B=3 A=4; pixel (2,1): R=9 G=8 B=7 A=255.
	copy(img.Pix[0:4], []byte{1, 2, 3, 4})
	i := img.PixOffset(2, 1)
	copy(img.Pix[i:i+4], []byte{9, 8, 7, 255})

	data, bytesPerRow := bgraPixels(img)

	if bytesPerRow != 256 {
		t.Errorf("bytesPerRow = %d, want 256 (3px rows round up to one alignment block)", bytesPerRow)
	}
	if len(data) != int(bytesPerRow)*2 {
		t.Errorf("len(data) = %d, want %d", len(data), int(bytesPerRow)*2)
	}
	if data[0] != 3 || data[1] != 2 || data[2] != 1 || data[3] != 4 {
		t.Errorf("pixel (0,0) BGRA = %v, want [3 2 1 4]", data[0:4])
	}
	j := bytesPerRow + 2*4
	if data[j] != 7 || data[j+1] != 8 || data[j+2] != 9 || data[j+3] != 255 {
		t.Errorf("pixel (2,1) BGRA = %v, want [7 8 9 255]", data[j:j+4])
	}
}

// A rounded window carries its shape in its own pixels: the corners are
// cleared (premultiplied, so every channel goes to zero — an
// alpha-only clear would leave an additive black fringe), the curve is
// antialiased, and the interior is untouched.
func TestPunchRoundedCorners(t *testing.T) {
	const w, h, r = 40, 30, 8
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i+0], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 200, 100, 50, 255
	}

	punchRoundedCorners(img, r)

	at := func(x, y int) (uint8, uint8, uint8, uint8) {
		o := img.PixOffset(x, y)
		return img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3]
	}

	// Every extreme corner pixel is fully cleared, all channels.
	for _, pt := range [][2]int{{0, 0}, {w - 1, 0}, {0, h - 1}, {w - 1, h - 1}} {
		cr, cg, cb, ca := at(pt[0], pt[1])
		if cr|cg|cb|ca != 0 {
			t.Errorf("corner (%d,%d) = (%d,%d,%d,%d), want all zero", pt[0], pt[1], cr, cg, cb, ca)
		}
	}

	// The interior is untouched.
	if cr, cg, cb, ca := at(w/2, h/2); cr != 200 || cg != 100 || cb != 50 || ca != 255 {
		t.Errorf("center = (%d,%d,%d,%d), want the original color", cr, cg, cb, ca)
	}
	// Well inside the corner's arc is also untouched.
	if _, _, _, ca := at(r, r); ca != 255 {
		t.Errorf("inside the arc alpha = %d, want 255", ca)
	}

	// The curve is antialiased: at least one partially covered pixel.
	partial := false
	for j := 0; j < r; j++ {
		for i := 0; i < r; i++ {
			if _, _, _, a := at(i, j); a > 0 && a < 255 {
				partial = true
			}
		}
	}
	if !partial {
		t.Error("no partially covered pixels: the corner curve is not antialiased")
	}

	// A zero radius is a no-op.
	plain := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := range plain.Pix {
		plain.Pix[i] = 255
	}
	punchRoundedCorners(plain, 0)
	for i, v := range plain.Pix {
		if v != 255 {
			t.Fatalf("zero radius modified pixel %d", i)
		}
	}
}

// The compositor's shadow specs are core's styles, narrowed to the
// float32 the shader's uniform block carries. Shadows reach the screen
// two ways — composited here, and painted into a surface by
// core.Painter.DropShadow for anything nested inside a layer (an MDI
// child in its parent window's texture) — and a window's shadow must
// look the same either way.
func TestShadowSpecsTrackCoreStyles(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec shadowSpec
		want core.DropShadowStyle
	}{
		{"window", windowShadowSpec, core.WindowDropShadow},
		{"overlay", overlayShadowSpec, core.OverlayDropShadow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.spec.offsetX != tc.want.OffsetX || tc.spec.offsetY != tc.want.OffsetY {
				t.Errorf("offset = (%v,%v), want (%v,%v)",
					tc.spec.offsetX, tc.spec.offsetY, tc.want.OffsetX, tc.want.OffsetY)
			}
			if tc.spec.blur != tc.want.Blur {
				t.Errorf("blur = %v, want %v", tc.spec.blur, tc.want.Blur)
			}
			if tc.spec.radius != tc.want.Radius {
				t.Errorf("radius = %v, want %v", tc.spec.radius, tc.want.Radius)
			}
			if tc.spec.alpha != float32(tc.want.Alpha) {
				t.Errorf("alpha = %v, want %v", tc.spec.alpha, tc.want.Alpha)
			}
		})
	}

	// The overlay style is the tighter of the two: closer to what it
	// covers, and less blurred.
	if core.OverlayDropShadow.Blur >= core.WindowDropShadow.Blur {
		t.Errorf("overlay blur %v is not tighter than window blur %v",
			core.OverlayDropShadow.Blur, core.WindowDropShadow.Blur)
	}
	if core.OverlayDropShadow.OffsetY >= core.WindowDropShadow.OffsetY {
		t.Errorf("overlay cast %v is not closer than window cast %v",
			core.OverlayDropShadow.OffsetY, core.WindowDropShadow.OffsetY)
	}
}

// The per-window texture cache: a window nobody has touched keeps its
// texture, and everything that changes its pixels forces a repaint.
// A false negative here is stale content on screen; a false positive
// only costs what the compositor used to spend unconditionally.
func TestNeedsRepaint(t *testing.T) {
	base := paintSignature{
		revision: 7, hasRevision: true,
		fontSize: 12, metrics: core.DefaultCellMetrics(),
		widthPx: 400, heightPx: 300,
	}
	fresh := time.Duration(0)
	hb := compositorHeartbeat

	changed := func(mutate func(*paintSignature)) paintSignature {
		s := base
		mutate(&s)
		return s
	}

	for _, tc := range []struct {
		name string
		now  paintSignature
		want bool
	}{
		{"unchanged", base, false},
		{"subtree repainted", changed(func(s *paintSignature) { s.revision++ }), true},
		{"resized", changed(func(s *paintSignature) { s.widthPx = 401 }), true},
		{"font zoom", changed(func(s *paintSignature) { s.fontSize = 14 }), true},
		{"denomination change", changed(func(s *paintSignature) { s.metrics.UnitsPerCellWidth = 99 }), true},
		{"reports no revision", changed(func(s *paintSignature) { s.hasRevision = false }), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := needsRepaint(base, tc.now, fresh, hb, false, false); got != tc.want {
				t.Errorf("needsRepaint = %v, want %v", got, tc.want)
			}
		})
	}

	// A never-painted surface has no signature to trust.
	if !needsRepaint(paintSignature{}, base, fresh, hb, false, false) {
		t.Error("a surface that has never been painted must repaint")
	}
	// A fresh or resized surface holds nothing usable yet.
	if !needsRepaint(base, base, fresh, hb, true, false) {
		t.Error("a dirty surface must repaint even with an identical signature")
	}
	// The escape hatch.
	if !needsRepaint(base, base, fresh, hb, false, true) {
		t.Error("KITTYTK_COMPOSITOR_REPAINT=always must force a repaint")
	}
}

// Position is deliberately absent from the signature: a window's
// placement lives in its uniform buffer, rewritten every frame, so
// dragging a window across the desktop must not repaint its texture.
func TestNeedsRepaintIgnoresPosition(t *testing.T) {
	sig := paintSignature{
		revision: 3, hasRevision: true,
		fontSize: 12, metrics: core.DefaultCellMetrics(),
		widthPx: 400, heightPx: 300,
	}
	// Same window, same size, same content — only the quad moved, which
	// the signature cannot even express.
	if needsRepaint(sig, sig, 0, compositorHeartbeat, false, false) {
		t.Error("a window that only moved must not repaint its texture")
	}
}

// A cached texture is refreshed on a heartbeat, so a change this cache
// cannot see costs at most a moment's staleness rather than freezing the
// window's pixels for good.
func TestNeedsRepaintHeartbeat(t *testing.T) {
	sig := paintSignature{revision: 1, hasRevision: true}

	if needsRepaint(sig, sig, compositorHeartbeat-time.Millisecond, compositorHeartbeat, false, false) {
		t.Error("repainted before the heartbeat was due")
	}
	if !needsRepaint(sig, sig, compositorHeartbeat, compositorHeartbeat, false, false) {
		t.Error("heartbeat came due and the texture was not refreshed")
	}
}

// The heartbeats stagger. Every window is first painted in the same
// frame, so one shared interval would keep the whole desk in lockstep
// and put a full repaint of EVERY window into the same frame once a
// second — the stutter the cache exists to remove.
func TestHeartbeatIntervalStaggers(t *testing.T) {
	// Ids as they actually arrive: pointer values, 16-byte aligned, so
	// their low bits carry no entropy at all.
	var ids []uint32
	for i := uint32(0); i < 16; i++ {
		ids = append(ids, 0x0a000000+i*16)
	}

	seen := map[time.Duration]bool{}
	for _, id := range ids {
		d := heartbeatInterval(id)
		if d < compositorHeartbeat || d > compositorHeartbeat+compositorHeartbeatSpread {
			t.Errorf("interval for id %#x = %v, outside [%v, %v]",
				id, d, compositorHeartbeat, compositorHeartbeat+compositorHeartbeatSpread)
		}
		seen[d] = true
	}
	if len(seen) < len(ids)/2 {
		t.Errorf("%d distinct intervals across %d aligned window ids; "+
			"the phase is not spreading them", len(seen), len(ids))
	}

	// And it is a pure function of the id: a window's turn must not
	// wander from frame to frame.
	if heartbeatInterval(ids[0]) != heartbeatInterval(ids[0]) {
		t.Error("heartbeatInterval is not deterministic")
	}
}

// A caret asked for inside a compositor layer is in that LAYER's
// coordinates — the layer paints into a texture of its own, at that
// texture's origin. The OS needs it relative to the window, so it has to
// be shifted by where the layer sits.
func TestCaretInSurface(t *testing.T) {
	layer := core.UnitRect{X: 120, Y: 64, Width: 300, Height: 200}

	got := caretInSurface(core.TextCaret{Visible: true, X: 16, Y: 32, Style: 5}, layer)
	want := core.TextCaret{Visible: true, X: 136, Y: 96, Style: 5}
	if got != want {
		t.Errorf("caretInSurface = %+v, want %+v", got, want)
	}

	// An insertion point with no drawn caret shifts the same way: a text
	// field paints its own caret and reports only where typing goes.
	area := caretInSurface(core.TextCaret{InputArea: true, X: 16, Y: 32}, layer)
	if want := (core.TextCaret{InputArea: true, X: 136, Y: 96}); area != want {
		t.Errorf("caretInSurface of an input-area request = %+v, want %+v", area, want)
	}

	// No request stays no request, and carries no stale position: a
	// position with nothing asking for it would anchor an input method's
	// candidate window at a caret that is not there.
	if got := caretInSurface(core.TextCaret{X: 16, Y: 32}, layer); got != (core.TextCaret{}) {
		t.Errorf("caretInSurface of an empty request = %+v, want the zero value", got)
	}

	// A layer at the origin is the identity.
	caret := core.TextCaret{Visible: true, X: 8, Y: 4}
	if got := caretInSurface(caret, core.UnitRect{Width: 100, Height: 100}); got != caret {
		t.Errorf("caretInSurface at the origin = %+v, want %+v unchanged", got, caret)
	}
}
