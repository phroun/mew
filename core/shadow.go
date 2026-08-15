package core

// DropShadowStyle describes one drop-shadow look, all lengths in units
// so a shadow scales with density like everything else it sits under.
//
// Shadows reach the screen two ways. The GPU compositor evaluates them
// analytically for the layers it composites — desktop windows, menu
// dropdowns, popups. Anything painted INSIDE a layer's own surface
// (an MDI child lives in its parent window's texture) has no layer of
// its own to sit above, so it paints its shadow through Painter's
// DropShadow into that same surface. Both read these styles, so the two
// paths cannot drift apart in look.
type DropShadowStyle struct {
	OffsetX Unit // cast down-right
	OffsetY Unit
	Blur    Unit    // falloff distance around the caster
	Radius  Unit    // caster corner rounding
	Alpha   float64 // peak opacity
}

// WindowDropShadow is the soft, larger shadow under a window;
// OverlayDropShadow the tighter one under menus, popups and combo lists,
// which sit closer to what they cover.
var (
	WindowDropShadow  = DropShadowStyle{OffsetX: 2, OffsetY: 3, Blur: 8, Radius: 4, Alpha: 0.35}
	OverlayDropShadow = DropShadowStyle{OffsetX: 1, OffsetY: 2, Blur: 4, Radius: 2, Alpha: 0.40}
)

// DropShadowDrawer is an optional RenderBackend capability: lay a soft
// drop shadow for a rounded rectangle given in DEVICE pixels. The rect
// is the caster ALREADY shifted by the cast offset; the shadow fades
// from alpha at the rect's edge to nothing blurPx away. The caster's own
// footprint is painted too — the caller draws the caster over it, and a
// caster with rounded corners wants shadow showing in the notches.
// Cell surfaces omit this; there is no pixel there to shade.
type DropShadowDrawer interface {
	DrawDropShadowPx(xPx, yPx, wPx, hPx int, radiusPx, blurPx, alpha float64)
}

// DropShadow paints a drop shadow for the rounded rect r (in local
// coordinates) in the given style, respecting the clip. Call it just
// before painting r itself: the shadow covers the caster's own footprint
// and the caster paints over it.
//
// Returns false on backends that cannot shade pixels (cell surfaces),
// where a drop shadow has no meaning and the caller simply paints on.
func (p *Painter) DropShadow(r UnitRect, style DropShadowStyle) bool {
	ds, ok := p.backend.(DropShadowDrawer)
	if !ok || r.Width <= 0 || r.Height <= 0 || style.Alpha <= 0 {
		return false
	}

	// Anchor the cast rect the same way every pixel-precise fill does,
	// and size it by the SPAN between its snapped edges so the shadow
	// lines up with the caster the caller is about to paint.
	x, y := r.X+style.OffsetX, r.Y+style.OffsetY
	sx, sy := p.toScreen(x, y)
	ax, ay := p.deviceAnchor(sx, sy)
	wPx := p.UnitSpanPxX(x, x+r.Width)
	hPx := p.UnitSpanPxY(y, y+r.Height)
	if wPx <= 0 || hPx <= 0 {
		return false
	}

	// Radius and blur are lengths, not positions: convert them by span
	// from the origin so they track font_size like the rect does.
	radiusPx := float64(p.UnitSpanPxX(0, style.Radius))
	blurPx := float64(p.UnitSpanPxX(0, style.Blur))

	p.applyClip()
	ds.DrawDropShadowPx(ax, ay, wPx, hPx, radiusPx, blurPx, style.Alpha)
	return true
}
