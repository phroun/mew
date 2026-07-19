// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// Panel is a container trinket that can hold other trinkets with a layout.
type Panel struct {
	core.TrinketBase
	core.AccessibleTrinket

	children      []core.Trinket
	layoutManager core.LayoutManager

	// fixedWidth, when > 0, pins the panel's SizeHint width; its height
	// still flows from content via height-for-width. This makes a
	// width-constrained box whose content word-wraps at exactly this
	// width instead of widening with the text.
	fixedWidth core.Unit

	// Appearance
	background    style.CellStyle
	backgroundSet bool // true if SetBackground was called
	border        bool
	borderStyle   style.BorderStyle
}

// NewPanel creates a new panel.
func NewPanel() *Panel {
	p := &Panel{
		background: style.DefaultStyle(),
	}
	p.TrinketBase = *core.NewTrinketBase()
	p.Init(p)
	p.SetFocusPolicy(core.NoFocus)
	p.SetAccessibleRole(core.RoleGroup)
	return p
}

// Children returns all child trinkets.
func (p *Panel) Children() []core.Trinket {
	return p.children
}

// AddChild adds a child trinket.
func (p *Panel) AddChild(child core.Trinket) {
	if child == nil {
		return
	}
	child.SetParent(p)
	p.children = append(p.children, child)
	if p.layoutManager != nil {
		if adder, ok := p.layoutManager.(interface{ AddTrinket(core.Trinket) }); ok {
			adder.AddTrinket(child)
		}
	}
	p.Update()
}

// RemoveChild removes a child trinket.
func (p *Panel) RemoveChild(child core.Trinket) {
	for i, c := range p.children {
		if c == child {
			p.children = append(p.children[:i], p.children[i+1:]...)
			child.SetParent(nil)
			if p.layoutManager != nil {
				if remover, ok := p.layoutManager.(interface{ RemoveTrinket(core.Trinket) }); ok {
					remover.RemoveTrinket(child)
				}
			}
			break
		}
	}
	p.Update()
}

// denominations returns the grid-metrics currency of this panel's own
// coordinate space (outer: the parent's, in which bounds live) and of
// its interior (honoring a per-panel override). Equal unless an
// override is set on this panel.
func (p *Panel) denominations() (outer, interior core.CellMetrics) {
	interior = p.EffectiveCellMetrics()
	if p.CellMetricsOverride() == nil {
		return interior, interior
	}
	return core.ParentCellMetrics(p.Self()), interior
}

// toInterior converts a point from the panel's outer currency into its
// interior currency.
func (p *Panel) toInterior(pos core.UnitPoint) core.UnitPoint {
	outer, interior := p.denominations()
	return core.UnitPoint{
		X: core.ExchangeX(pos.X, outer, interior),
		Y: core.ExchangeY(pos.Y, outer, interior),
	}
}

// ChildAt returns the child at the given position (in the panel's
// outer currency).
func (p *Panel) ChildAt(pos core.UnitPoint) core.Trinket {
	return p.childAtInterior(p.toInterior(pos))
}

// childAtInterior hit-tests a point already in the interior currency.
func (p *Panel) childAtInterior(pos core.UnitPoint) core.Trinket {
	for _, child := range p.children {
		if !child.IsVisible() {
			continue
		}
		bounds := child.Bounds()
		if pos.X >= bounds.X && pos.X < bounds.X+bounds.Width &&
			pos.Y >= bounds.Y && pos.Y < bounds.Y+bounds.Height {
			return child
		}
	}
	return nil
}

// Layout arranges children within this container.
func (p *Panel) Layout() {
	if p.layoutManager != nil {
		bounds := p.Bounds()
		// Use local coordinates - children are positioned relative to
		// this container, denominated in the interior currency.
		outer, interior := p.denominations()
		contentBounds := core.UnitRect{
			X:      0,
			Y:      0,
			Width:  core.ExchangeX(bounds.Width, outer, interior),
			Height: core.ExchangeY(bounds.Height, outer, interior),
		}
		if p.border {
			contentBounds = core.UnitRect{
				X:      interior.CellWidth,
				Y:      interior.CellHeight,
				Width:  contentBounds.Width - 2*interior.CellWidth,
				Height: contentBounds.Height - 2*interior.CellHeight,
			}
		}
		p.layoutManager.Layout(p, contentBounds)
	}
}

// LayoutManager returns the layout manager.
func (p *Panel) LayoutManager() core.LayoutManager {
	return p.layoutManager
}

// SetLayoutManager sets the layout manager.
func (p *Panel) SetLayoutManager(layout core.LayoutManager) {
	p.layoutManager = layout
	// Add existing children to the new layout
	if adder, ok := layout.(interface{ AddTrinket(core.Trinket) }); ok {
		for _, child := range p.children {
			adder.AddTrinket(child)
		}
	}
	// Let the layout resolve grid metrics through this container's
	// inheritance chain (layouts are not trinkets themselves).
	if ms, ok := layout.(interface{ SetMetricsSource(core.Trinket) }); ok {
		ms.SetMetricsSource(p.Self())
	}
	p.Layout()
	p.Update()
}

// SetBorder enables or disables the border. Enabling defaults the
// border style to single lines if none was set (the zero-value
// BorderStyle would render invisibly).
func (p *Panel) SetBorder(enabled bool) {
	p.border = enabled
	if enabled && p.borderStyle == (style.BorderStyle{}) {
		p.borderStyle = style.BorderSingle
	}
	p.Update()
}

// SetBorderStyle sets the border style.
func (p *Panel) SetBorderStyle(s style.BorderStyle) {
	p.borderStyle = s
	p.Update()
}

// SetBackground sets the background style.
func (p *Panel) SetBackground(s style.CellStyle) {
	p.background = s
	p.backgroundSet = true
	p.Update()
}

// SizeHint returns the preferred size, denominated in the panel's
// outer currency (interior needs are computed in interior units and
// exchanged at the boundary).
func (p *Panel) SizeHint() core.UnitSize {
	outer, interior := p.denominations()
	var sh core.UnitSize
	if p.layoutManager != nil {
		sh = core.ExchangeSize(p.layoutManager.SizeHint(p), interior, outer)
	} else {
		font := p.EffectiveFont()
		sh = core.ExchangeSize(core.UnitSize{
			Width:  font.MeasureRunes(20), // 20 chars wide
			Height: interior.TextHeight(10),
		}, interior, outer)
	}
	if p.fixedWidth > 0 {
		sh.Width = p.fixedWidth
	}
	return sh
}

// SetFixedWidth pins the panel's SizeHint width (0 clears it). Height
// continues to flow from content, so a wrapping label inside grows the
// box taller rather than wider.
func (p *Panel) SetFixedWidth(w core.Unit) {
	p.fixedWidth = w
	p.Update()
}

// MinimumSize returns the minimum size in the outer currency.
func (p *Panel) MinimumSize() core.UnitSize {
	if p.layoutManager != nil {
		outer, interior := p.denominations()
		return core.ExchangeSize(p.layoutManager.MinimumSize(p), interior, outer)
	}
	return core.UnitSize{Width: 16, Height: 16}
}

// HasHeightForWidth reports whether this panel's content height depends
// on its width (i.e. its layout contains height-for-width trinkets).
func (p *Panel) HasHeightForWidth() bool {
	hfw, ok := p.layoutManager.(core.HeightForWidther)
	return ok && hfw.HasHeightForWidth()
}

// HeightForWidth returns the height this panel requires at the given
// width (both in the outer currency), accounting for the border inset.
func (p *Panel) HeightForWidth(width core.Unit) core.Unit {
	hfw, ok := p.layoutManager.(core.HeightForWidther)
	if !ok || !hfw.HasHeightForWidth() {
		return p.SizeHint().Height
	}
	outer, interior := p.denominations()
	inner := core.ExchangeX(width, outer, interior)
	var chrome core.Unit
	if p.border {
		inner -= 2 * interior.CellWidth
		if inner < 0 {
			inner = 0
		}
		chrome = 2 * interior.CellHeight
	}
	return core.ExchangeY(hfw.HeightForWidth(inner)+chrome, interior, outer)
}

// Paint renders the panel.
func (p *Panel) Paint(painter *core.Painter) {
	bounds := p.Bounds()

	// Only draw background if explicitly set (allows parent backgrounds to show through)
	if p.backgroundSet {
		painter.FillRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, ' ', p.background)
	}

	// Draw border if enabled
	if p.border {
		bgStyle := p.background
		if !p.backgroundSet {
			bgStyle = style.DefaultStyle()
		}
		painter.DrawRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, p.borderStyle, bgStyle)
	}

	// Paint children (in the interior denomination)
	outer, interior := p.denominations()
	interiorPainter := painter.WithDenomination(outer, interior)
	for _, child := range p.children {
		if child.IsVisible() {
			childBounds := child.Bounds()
			childPainter := interiorPainter.WithOffset(childBounds.X, childBounds.Y)
			child.Paint(childPainter)
		}
	}
}

// HandleKeyPress handles keyboard input.
func (p *Panel) HandleKeyPress(event core.KeyPressEvent) bool {
	// Panels don't handle keys directly
	return false
}

// HandleMousePress handles mouse clicks.
func (p *Panel) HandleMousePress(event core.MousePressEvent) bool {
	// Convert into the interior denomination once; child bounds are
	// interior-denominated.
	interiorPos := p.toInterior(core.UnitPoint{X: event.X, Y: event.Y})
	event.X, event.Y = interiorPos.X, interiorPos.Y

	// Find child under mouse and forward event
	targetChild := p.childAtInterior(interiorPos)

	// Cancel drags on all OTHER children since a new press is happening
	for _, child := range p.children {
		if child == targetChild {
			continue // Don't cancel the child that will receive the press
		}
		if handler, ok := child.(interface {
			HandleMouseRelease(core.MouseReleaseEvent) bool
		}); ok {
			handler.HandleMouseRelease(core.MouseReleaseEvent{Button: event.Button})
		}
	}

	// Forward press to the target child
	if targetChild != nil {
		if handler, ok := targetChild.(interface{ HandleMousePress(core.MousePressEvent) bool }); ok {
			childBounds := targetChild.Bounds()
			childEvent := event
			childEvent.X -= childBounds.X
			childEvent.Y -= childBounds.Y
			return handler.HandleMousePress(childEvent)
		}
	}
	return false
}

// HandleMouseMove handles mouse movement.
func (p *Panel) HandleMouseMove(event core.MouseMoveEvent) bool {
	interiorPos := p.toInterior(core.UnitPoint{X: event.X, Y: event.Y})
	event.X, event.Y = interiorPos.X, interiorPos.Y

	// Forward to all children (needed for drag operations)
	for _, child := range p.children {
		if handler, ok := child.(interface {
			HandleMouseMove(core.MouseMoveEvent) bool
		}); ok {
			childBounds := child.Bounds()
			childEvent := event
			childEvent.X -= childBounds.X
			childEvent.Y -= childBounds.Y
			if handler.HandleMouseMove(childEvent) {
				return true
			}
		}
	}
	return false
}

// HandleMouseWheel forwards a wheel event to the topmost child under
// the pointer.
func (p *Panel) HandleMouseWheel(event core.MouseWheelEvent) bool {
	interiorPos := p.toInterior(core.UnitPoint{X: event.X, Y: event.Y})
	event.X, event.Y = interiorPos.X, interiorPos.Y
	for i := len(p.children) - 1; i >= 0; i-- {
		child := p.children[i]
		b := child.Bounds()
		if !b.Contains(core.UnitPoint{X: event.X, Y: event.Y}) {
			continue
		}
		if handler, ok := child.(interface {
			HandleMouseWheel(core.MouseWheelEvent) bool
		}); ok {
			childEvent := event
			childEvent.X -= b.X
			childEvent.Y -= b.Y
			if handler.HandleMouseWheel(childEvent) {
				return true
			}
		}
	}
	return false
}

// HandleMouseRelease handles mouse button release.
func (p *Panel) HandleMouseRelease(event core.MouseReleaseEvent) bool {
	interiorPos := p.toInterior(core.UnitPoint{X: event.X, Y: event.Y})
	event.X, event.Y = interiorPos.X, interiorPos.Y

	// Forward to all children (needed for drag operations)
	for _, child := range p.children {
		if handler, ok := child.(interface {
			HandleMouseRelease(core.MouseReleaseEvent) bool
		}); ok {
			childBounds := child.Bounds()
			childEvent := event
			childEvent.X -= childBounds.X
			childEvent.Y -= childBounds.Y
			if handler.HandleMouseRelease(childEvent) {
				return true
			}
		}
	}
	return false
}

// SetBounds sets the panel bounds and triggers layout.
func (p *Panel) SetBounds(bounds core.UnitRect) {
	p.TrinketBase.SetBounds(bounds)
	// Always relayout children when bounds are set
	// (font changes may require relayout even if size unchanged)
	p.Layout()
}

// HandleResize is called when the panel is resized.
func (p *Panel) HandleResize(oldSize, newSize core.UnitSize) {
	p.Layout()
}

// AccessibleInfo returns accessibility information.
func (p *Panel) AccessibleInfo() core.AccessibleInfo {
	info := p.AccessibleTrinket.AccessibleInfo()
	info.Role = core.RoleGroup
	return info
}
