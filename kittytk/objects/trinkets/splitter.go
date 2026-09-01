// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// Splitter is a container trinket that divides space between two children
// with a draggable divider.
// For vertical splitters (horizontal divider): ────·· Title ··────
// For horizontal splitters (vertical divider): │ with : handle
type Splitter struct {
	core.TrinketBase
	core.TrinketKeys
	core.AccessibleTrinket

	// Child trinkets
	first  core.Trinket
	second core.Trinket

	// Orientation (Horizontal or Vertical)
	orientation core.Orientation

	// Split position (0.0-1.0 ratio, or absolute if > 1)
	position float64

	// Divider dragging state
	dragging bool
	// Whether the pointer is currently hovering over the divider band.
	hoveringDivider bool
	// Offset of the press point within the divider band, so the
	// divider tracks the grab position instead of jumping its
	// leading edge to the pointer.
	dragOffset core.Unit

	// Optional title displayed in the divider
	title string

	// Background - only fills if explicitly set
	backgroundSet bool
}

// NewSplitter creates a new splitter with the given orientation.
func NewSplitter(orientation core.Orientation) *Splitter {
	s := &Splitter{
		orientation: orientation,
		position:    0.5, // Default to 50/50 split
	}
	s.TrinketBase = *core.NewTrinketBase()
	// A divider is not an "item" and nudging it is not moving a cursor, so a
	// splitter borrows the window RESIZE vocabulary rather than pretending
	// otherwise. The fine forms are its single step and the coarse ones its
	// big step, which is the only reason the bare arrows carry a size meaning
	// at all: nothing else resizes with no modifier held.
	s.SetCommands(
		core.CmdWindowSizeFineUp, core.CmdWindowSizeFineDown,
		core.CmdWindowSizeFineLeft, core.CmdWindowSizeFineRight,
		core.CmdWindowSizeUp, core.CmdWindowSizeDown,
		core.CmdWindowSizeLeft, core.CmdWindowSizeRight,
	)
	s.Init(s)
	s.SetFocusPolicy(core.StrongFocus) // Focusable for keyboard navigation
	s.SetFurtive(true)                 // Furtive: no focus on click, skip for initial focus
	s.SetAccessibleRole(core.RoleSplitter)
	return s
}

// NewHSplitter creates a horizontal splitter (children side by side).
func NewHSplitter() *Splitter {
	return NewSplitter(core.Horizontal)
}

// NewVSplitter creates a vertical splitter (children stacked).
func NewVSplitter() *Splitter {
	return NewSplitter(core.Vertical)
}

// SetFirst sets the first child trinket (left/top).
func (s *Splitter) SetFirst(w core.Trinket) {
	if s.first != nil {
		s.first.SetParent(nil)
	}
	s.first = w
	if w != nil {
		w.SetParent(s)
	}
	s.Update()
}

// First returns the first child trinket.
func (s *Splitter) First() core.Trinket {
	return s.first
}

// SetSecond sets the second child trinket (right/bottom).
func (s *Splitter) SetSecond(w core.Trinket) {
	if s.second != nil {
		s.second.SetParent(nil)
	}
	s.second = w
	if w != nil {
		w.SetParent(s)
	}
	s.Update()
}

// Second returns the second child trinket.
func (s *Splitter) Second() core.Trinket {
	return s.second
}

// SetPosition sets the split position (0.0-1.0 as ratio).
func (s *Splitter) SetPosition(pos float64) {
	if pos < 0 {
		pos = 0
	} else if pos > 1 {
		pos = 1
	}
	s.position = pos
	s.Update()
}

// Position returns the split position.
func (s *Splitter) Position() float64 {
	return s.position
}

// Orientation returns the splitter orientation.
func (s *Splitter) Orientation() core.Orientation {
	return s.orientation
}

// CursorShape implements core.CursorProvider: over the divider (where
// ChildAt returns nil, so the cursor walk stops here) the pointer shows
// the resize cursor for the drag axis - horizontal for a horizontal
// splitter's vertical divider, vertical for a vertical splitter's
// horizontal divider.
func (s *Splitter) CursorShape() core.CursorShape {
	if s.orientation == core.Horizontal {
		return core.CursorResizeH
	}
	return core.CursorResizeV
}

// SetOrientation sets the splitter orientation.
func (s *Splitter) SetOrientation(o core.Orientation) {
	s.orientation = o
	s.Update()
}

// Title returns the splitter divider title.
func (s *Splitter) Title() string {
	return s.title
}

// SetTitle sets the splitter divider title.
func (s *Splitter) SetTitle(title string) {
	s.title = title
	s.Update()
}

// Children returns all child trinkets.
func (s *Splitter) Children() []core.Trinket {
	var children []core.Trinket
	if s.first != nil {
		children = append(children, s.first)
	}
	if s.second != nil {
		children = append(children, s.second)
	}
	return children
}

// AddChild adds a child trinket.
func (s *Splitter) AddChild(child core.Trinket) {
	if s.first == nil {
		s.SetFirst(child)
	} else if s.second == nil {
		s.SetSecond(child)
	}
}

// RemoveChild removes a child trinket.
func (s *Splitter) RemoveChild(child core.Trinket) {
	if s.first == child {
		s.first = nil
	} else if s.second == child {
		s.second = nil
	}
}

// ChildAt returns the child at the given position.
func (s *Splitter) ChildAt(pos core.UnitPoint) core.Trinket {
	dividerRect := s.dividerBounds()
	if dividerRect.Contains(pos) {
		return nil // On divider
	}

	firstBounds, secondBounds := s.childBounds()
	if firstBounds.Contains(pos) && s.first != nil {
		return s.first
	}
	if secondBounds.Contains(pos) && s.second != nil {
		return s.second
	}
	return nil
}

// Layout arranges children.
func (s *Splitter) Layout() {
	firstBounds, secondBounds := s.childBounds()
	if s.first != nil {
		s.first.SetBounds(firstBounds)
		// Force content to re-layout with fresh SizeHints
		if container, ok := s.first.(core.Container); ok {
			container.Layout()
		}
	}
	if s.second != nil {
		s.second.SetBounds(secondBounds)
		// Force content to re-layout with fresh SizeHints
		if container, ok := s.second.(core.Container); ok {
			container.Layout()
		}
	}
}

// LayoutManager returns nil (Splitter manages its own layout).
func (s *Splitter) LayoutManager() core.LayoutManager {
	return nil
}

// SetLayoutManager is a no-op (Splitter manages its own layout).
func (s *Splitter) SetLayoutManager(layout core.LayoutManager) {
	// Splitter manages its own layout
}

// dividerThickness returns the divider band's cross-axis size: one
// layout column for vertical bands; half a row for horizontal bands
// on pixel surfaces (the scrollbar lane dimension - and expressed in
// the Y denomination so re-denominated interiors keep the visual
// proportion); a full row on cell surfaces, which cannot be thinner
// than a character.
func (s *Splitter) dividerThickness() core.Unit {
	metrics := s.EffectiveCellMetrics()
	if s.orientation == core.Horizontal {
		return metrics.UnitsPerCellWidth
	}
	if core.FindSmoothPositioning(s.Self()) {
		return metrics.UnitsPerCellHeight / 2
	}
	return metrics.UnitsPerCellHeight
}

// dividerBounds returns the bounds of the divider bar.
func (s *Splitter) dividerBounds() core.UnitRect {
	bounds := s.Bounds()
	metrics := s.EffectiveCellMetrics()

	// Cell surfaces snap the divider to whole rows/columns; smooth
	// (pixel) surfaces track the split ratio at unit granularity -
	// the same adjustment window drag/resize received.
	smooth := core.FindSmoothPositioning(s.Self())
	dividerSize := s.dividerThickness()

	if s.orientation == core.Horizontal {
		// Horizontal splitter has a vertical divider bar
		totalWidth := bounds.Width - dividerSize
		firstWidth := core.Unit(float64(totalWidth) * s.position)
		if !smooth {
			// Round to cell boundary
			firstWidth = core.Unit(metrics.UnitsToCellX(firstWidth)) * metrics.UnitsPerCellWidth
		}

		return core.UnitRect{
			X:      firstWidth,
			Y:      0,
			Width:  dividerSize,
			Height: bounds.Height,
		}
	}

	// Vertical splitter has a horizontal divider bar
	totalHeight := bounds.Height - dividerSize
	firstHeight := core.Unit(float64(totalHeight) * s.position)
	if !smooth {
		// Round to cell boundary
		firstHeight = core.Unit(metrics.UnitsToCellY(firstHeight)) * metrics.UnitsPerCellHeight
	}

	return core.UnitRect{
		X:      0,
		Y:      firstHeight,
		Width:  bounds.Width,
		Height: dividerSize,
	}
}

// childBounds returns the bounds for both children.
func (s *Splitter) childBounds() (core.UnitRect, core.UnitRect) {
	bounds := s.Bounds()
	divider := s.dividerBounds()

	if s.orientation == core.Horizontal {
		return core.UnitRect{
				X:      0,
				Y:      0,
				Width:  divider.X,
				Height: bounds.Height,
			}, core.UnitRect{
				X:      divider.X + divider.Width,
				Y:      0,
				Width:  bounds.Width - divider.X - divider.Width,
				Height: bounds.Height,
			}
	}

	// Vertical
	return core.UnitRect{
			X:      0,
			Y:      0,
			Width:  bounds.Width,
			Height: divider.Y,
		}, core.UnitRect{
			X:      0,
			Y:      divider.Y + divider.Height,
			Width:  bounds.Width,
			Height: bounds.Height - divider.Y - divider.Height,
		}
}

// SizeHint returns a modest fixed preference; splitters are meant to
// be stretched by their layout. (Returning the current bounds made the
// hint a ratchet: layouts could grow the splitter but never shrink it
// back, since stretch distribution treats the hint as a floor.)
func (s *Splitter) SizeHint() core.UnitSize {
	metrics := s.EffectiveCellMetrics()
	return core.UnitSize{
		Width:  metrics.UnitsPerCellWidth * 20,
		Height: metrics.UnitsPerCellHeight * 5,
	}
}

// Paint renders the splitter.
func (sp *Splitter) Paint(p *core.Painter) {
	bounds := sp.Bounds()
	scheme := sp.GetScheme()
	metrics := sp.EffectiveCellMetrics()

	// Only draw background if explicitly set (allows parent backgrounds to show through)
	if sp.backgroundSet {
		p.FillRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, ' ', scheme.GetNormal(true))
	}

	// Update child bounds
	sp.Layout()

	// Draw first child
	if sp.first != nil {
		firstBounds, _ := sp.childBounds()
		firstPainter := p.WithOffset(firstBounds.X, firstBounds.Y).
			WithClip(core.UnitRect{Width: firstBounds.Width, Height: firstBounds.Height})
		sp.first.Paint(firstPainter)
	}

	// Divider style with middot drag handle styling. The state resolvers
	// bake in the precedence pressed(dragging) > focus > hover > normal;
	// by default the handle and title fall back to the plain divider body,
	// so only an explicitly-set hover style changes anything on hover.
	divider := sp.dividerBounds()
	focused := sp.HasFocus()
	// Hover is graphical-only: the cell/TUI path gets no free mouse moves, so
	// a hover set during a drag could never clear and would stick.
	hovered := sp.hoveringDivider && !sp.dragging && p.Graphical()
	dividerStyle := scheme.GetSplitterHandleState(focused, hovered, sp.dragging)
	titleStyle := scheme.GetSplitterTitleState(focused, hovered, sp.dragging)

	if !p.Graphical() {
		if sp.orientation == core.Horizontal {
			// Vertical divider bar with ':' handle
			midY := bounds.Height / 2
			// Round to cell boundary
			midY = (midY / metrics.UnitsPerCellHeight) * metrics.UnitsPerCellHeight
			for y := core.Unit(0); y < bounds.Height; y += metrics.UnitsPerCellHeight {
				ch := '│'
				// Draw drag handle indicator in the middle
				if y == midY {
					ch = ':'
				}
				p.DrawCell(divider.X, y, ch, dividerStyle)
			}
		} else {
			// Horizontal divider bar: ────·· Title ··────
			width := int(bounds.Width / metrics.UnitsPerCellWidth)
			titleRunes := []rune(sp.title)
			titleLen := len(titleRunes)

			if titleLen == 0 {
				// No title: draw line with 4 middots centered
				center := width / 2
				for xi := 0; xi < width; xi++ {
					x := metrics.CellToUnitsX(xi)
					ch := '─'
					// Draw ·· ·· (4 dots) at center
					if xi == center-1 || xi == center || xi == center+1 || xi == center+2 {
						ch = '·'
					}
					p.DrawCell(x, divider.Y, ch, dividerStyle)
				}
			} else {
				// With title: ────·· Title ··────
				middleContent := "·· " + sp.title + " ··"
				middleRunes := []rune(middleContent)
				middleLen := len(middleRunes)
				startMiddle := (width - middleLen) / 2

				for xi := 0; xi < width; xi++ {
					x := metrics.CellToUnitsX(xi)
					var ch rune
					chStyle := dividerStyle
					if xi < startMiddle {
						ch = '─'
					} else if xi < startMiddle+middleLen {
						ch = middleRunes[xi-startMiddle]
						chStyle = titleStyle
					} else {
						ch = '─'
					}
					p.DrawCell(x, divider.Y, ch, chStyle)
				}
			}
		}
	}

	// Draw second child
	if sp.second != nil {
		_, secondBounds := sp.childBounds()
		secondPainter := p.WithOffset(secondBounds.X, secondBounds.Y).
			WithClip(core.UnitRect{Width: secondBounds.Width, Height: secondBounds.Height})
		sp.second.Paint(secondPainter)
	}

	// Pixel surfaces draw the divider last: its caption box may
	// overhang a band thinner than the caption face.
	if p.Graphical() {
		sp.paintDividerGraphical(p, divider, dividerStyle, titleStyle)
	}
}

// paintDividerGraphical draws the pixel-surface divider: the band
// fills its whole rectangle, a hairline runs its full length, and the
// grab trinketry (title caption or dots) sits exactly centered in a
// cleared mid-section - no cell quantization anywhere.
// atLeast returns v clamped up to the floor (never below it).
func atLeast(v, floor core.Unit) core.Unit {
	if v < floor {
		return floor
	}
	return v
}

func (sp *Splitter) paintDividerGraphical(p *core.Painter, divider core.UnitRect, dividerStyle, titleStyle style.CellStyle) {
	line := dividerStyle.WithBg(dividerStyle.Fg)
	p.FillRect(divider, ' ', dividerStyle)

	// Hairline thickness and dot sizes are screen-space quantities:
	// inside a re-denominated interior, a 1-local-unit line would
	// scale below one pixel and vanish.
	hairW := p.ScreenWidthToLocal(1)
	if hairW < 1 {
		hairW = 1
	}
	hairH := p.ScreenHeightToLocal(1)
	if hairH < 1 {
		hairH = 1
	}

	if sp.orientation == core.Horizontal {
		// Vertical band: hairline down the middle, broken by the ':'
		// grab dots at the exact center. The gap and the dots are screen-space
		// sizes converted into local units, the same as the hairlines above:
		// a screen unit is a fixed number of device pixels at a given zoom, so
		// these track font_size and stay put under re-denomination. The
		// constants are the sizes in screen units.
		dotW := atLeast(p.ScreenWidthToLocal(2), hairW)
		dotH := atLeast(p.ScreenHeightToLocal(2), hairH)
		// The dots sit ON the line, so both are centered by the one
		// expression. Centering the line on (Width-hairW)/2 while placing
		// the dots at Width/2-dotW/2 truncates two different halvings on a
		// grid whose coarseness is the denomination's: at 4x8 that put the
		// dots a whole local unit -- two device pixels -- to the right of
		// the line they are meant to straddle.
		centered := func(w core.Unit) core.Unit { return divider.X + (divider.Width-w)/2 }
		lineX := centered(hairW)
		cy := divider.Y + divider.Height/2
		gapHalf := atLeast(p.ScreenHeightToLocal(6), hairH)
		p.FillRect(core.UnitRect{X: lineX, Y: divider.Y, Width: hairW, Height: cy - gapHalf - divider.Y}, ' ', line)
		p.FillRect(core.UnitRect{X: lineX, Y: cy + gapHalf, Width: hairW, Height: divider.Y + divider.Height - cy - gapHalf}, ' ', line)
		p.FillRect(core.UnitRect{X: centered(dotW), Y: cy - dotH - hairH, Width: dotW, Height: dotH}, ' ', line)
		p.FillRect(core.UnitRect{X: centered(dotW), Y: cy + hairH, Width: dotW, Height: dotH}, ' ', line)
		return
	}

	// Horizontal band: hairline across, broken by the centered caption
	// (title or the classic four grab dots) in the 75% caption face.
	lineY := divider.Y + (divider.Height-hairH)/2
	label := "····"
	if sp.title != "" {
		label = "·· " + sp.title + " ··"
	}
	base := sp.EffectiveFont()
	font := captionFont75(base)
	// The measured width is screen-space; convert into this painter's local
	// units so centering holds inside re-denominated interiors. The line the
	// caption occupies is three quarters of a grid row, already local.
	w := p.ScreenWidthToLocal(font.MeasureText(label))
	h := core.LineUnits(font, base, sp.EffectiveCellMetrics())
	pad := p.ScreenWidthToLocal(4)
	boxW := w + pad*2
	if boxW > divider.Width {
		boxW = divider.Width
	}
	boxX := divider.X + (divider.Width-boxW)/2
	p.FillRect(core.UnitRect{X: divider.X, Y: lineY, Width: boxX - divider.X, Height: hairH}, ' ', line)
	p.FillRect(core.UnitRect{X: boxX + boxW, Y: lineY, Width: divider.X + divider.Width - boxX - boxW, Height: hairH}, ' ', line)
	// Caption box on the band background, text exactly centered on
	// the band's centerline.
	boxY := divider.Y + (divider.Height-h)/2
	p.FillRect(core.UnitRect{X: boxX, Y: boxY, Width: boxW, Height: h}, ' ', titleStyle)
	p.DrawText(boxX+pad, boxY, label, titleStyle, font)
}

// HandleMousePress handles mouse button presses.
func (s *Splitter) HandleMousePress(event core.MousePressEvent) bool {
	if event.Button != core.LeftButton {
		// The divider only drags with the left button, but other
		// buttons still belong to the children (context menus).
		firstBounds, secondBounds := s.childBounds()
		pos := core.UnitPoint{X: event.X, Y: event.Y}
		if firstBounds.Contains(pos) && s.first != nil {
			e := event
			e.X -= firstBounds.X
			e.Y -= firstBounds.Y
			return s.first.HandleMousePress(e)
		}
		if secondBounds.Contains(pos) && s.second != nil {
			e := event
			e.X -= secondBounds.X
			e.Y -= secondBounds.Y
			return s.second.HandleMousePress(e)
		}
		return false
	}

	// Check if click is on divider
	divider := s.dividerBounds()
	if s.orientation == core.Horizontal {
		// Hit area is just the divider itself (no extension into child areas)
		if divider.Contains(core.UnitPoint{X: event.X, Y: event.Y}) {
			s.dragging = true
			s.dragOffset = event.X - divider.X
			s.Update()
			return true
		}
	} else {
		// Hit area is just the divider itself
		if divider.Contains(core.UnitPoint{X: event.X, Y: event.Y}) {
			s.dragging = true
			s.dragOffset = event.Y - divider.Y
			s.Update()
			return true
		}
	}

	// Forward to children
	firstBounds, secondBounds := s.childBounds()
	pos := core.UnitPoint{X: event.X, Y: event.Y}

	if firstBounds.Contains(pos) && s.first != nil {
		// Cancel any drag on the other child since a new press is happening elsewhere
		if s.second != nil {
			if handler, ok := s.second.(interface {
				HandleMouseRelease(core.MouseReleaseEvent) bool
			}); ok {
				handler.HandleMouseRelease(core.MouseReleaseEvent{Button: event.Button})
			}
		}
		localEvent := event
		localEvent.X -= firstBounds.X
		localEvent.Y -= firstBounds.Y
		return s.first.HandleMousePress(localEvent)
	}

	if secondBounds.Contains(pos) && s.second != nil {
		// Cancel any drag on the other child since a new press is happening elsewhere
		if s.first != nil {
			if handler, ok := s.first.(interface {
				HandleMouseRelease(core.MouseReleaseEvent) bool
			}); ok {
				handler.HandleMouseRelease(core.MouseReleaseEvent{Button: event.Button})
			}
		}
		localEvent := event
		localEvent.X -= secondBounds.X
		localEvent.Y -= secondBounds.Y
		return s.second.HandleMousePress(localEvent)
	}

	// Click is not on either child (maybe on divider that wasn't handled, or outside)
	// Cancel drags on both children
	if s.first != nil {
		if handler, ok := s.first.(interface {
			HandleMouseRelease(core.MouseReleaseEvent) bool
		}); ok {
			handler.HandleMouseRelease(core.MouseReleaseEvent{Button: event.Button})
		}
	}
	if s.second != nil {
		if handler, ok := s.second.(interface {
			HandleMouseRelease(core.MouseReleaseEvent) bool
		}); ok {
			handler.HandleMouseRelease(core.MouseReleaseEvent{Button: event.Button})
		}
	}

	return false
}

// HandleMouseMove handles mouse movement for dragging.
func (s *Splitter) HandleMouseMove(event core.MouseMoveEvent) bool {
	if s.dragging {
		bounds := s.Bounds()

		if s.orientation == core.Horizontal {
			dividerSize := s.dividerThickness()
			totalWidth := bounds.Width - dividerSize
			if totalWidth > 0 {
				newPos := float64(event.X-s.dragOffset) / float64(totalWidth)
				if newPos < 0.1 {
					newPos = 0.1
				} else if newPos > 0.9 {
					newPos = 0.9
				}
				s.position = newPos
				s.Update()
			}
		} else {
			dividerSize := s.dividerThickness()
			totalHeight := bounds.Height - dividerSize
			if totalHeight > 0 {
				newPos := float64(event.Y-s.dragOffset) / float64(totalHeight)
				if newPos < 0.1 {
					newPos = 0.1
				} else if newPos > 0.9 {
					newPos = 0.9
				}
				s.position = newPos
				s.Update()
			}
		}

		return true
	}

	// Track pointer hover over the divider band (highlights the grab
	// handle). Hover is a no-button affordance: a held button means a drag
	// begun elsewhere is passing over, so don't highlight (and clear any set).
	pos := core.UnitPoint{X: event.X, Y: event.Y}
	over := event.Buttons == 0 && s.dividerBounds().Contains(pos)
	if over != s.hoveringDivider {
		s.hoveringDivider = over
		s.Update()
	}

	firstBounds, secondBounds := s.childBounds()

	// A held button means a drag/selection in a child: forward the real
	// move to both panes so a gesture that leaves its pane keeps tracking.
	// A plain hover move goes only to the pane that actually contains the
	// pointer; the other pane (and both, when the pointer is on the
	// divider) get an out-of-bounds move so obscured/clipped content or the
	// far pane doesn't also highlight.
	hovering := event.Buttons == 0
	forward := func(child core.Trinket, cb core.UnitRect) bool {
		handler, ok := child.(interface {
			HandleMouseMove(core.MouseMoveEvent) bool
		})
		if !ok {
			return false
		}
		fx, fy := event.X, event.Y
		if hovering && (over || !cb.Contains(pos)) {
			fx, fy = -1, -1
		}
		le := event
		le.X = fx - cb.X
		le.Y = fy - cb.Y
		return handler.HandleMouseMove(le)
	}

	r1, r2 := false, false
	if s.first != nil {
		r1 = forward(s.first, firstBounds)
	}
	if s.second != nil {
		r2 = forward(s.second, secondBounds)
	}
	return r1 || r2
}

// HandleMouseWheel forwards a wheel event to the pane under the
// pointer.
func (s *Splitter) HandleMouseWheel(event core.MouseWheelEvent) bool {
	firstBounds, secondBounds := s.childBounds()
	pos := core.UnitPoint{X: event.X, Y: event.Y}
	forward := func(w core.Trinket, b core.UnitRect) bool {
		if w == nil || !b.Contains(pos) {
			return false
		}
		handler, ok := w.(interface {
			HandleMouseWheel(core.MouseWheelEvent) bool
		})
		if !ok {
			return false
		}
		localEvent := event
		localEvent.X -= b.X
		localEvent.Y -= b.Y
		return handler.HandleMouseWheel(localEvent)
	}
	if forward(s.first, firstBounds) {
		return true
	}
	return forward(s.second, secondBounds)
}

// HandleMouseRelease handles mouse button releases.
func (s *Splitter) HandleMouseRelease(event core.MouseReleaseEvent) bool {
	if s.dragging {
		s.dragging = false
		s.Update()
		return true
	}

	// Forward to children (needed for drag operations within children)
	firstBounds, secondBounds := s.childBounds()

	if s.first != nil {
		if handler, ok := s.first.(interface {
			HandleMouseRelease(core.MouseReleaseEvent) bool
		}); ok {
			localEvent := event
			localEvent.X -= firstBounds.X
			localEvent.Y -= firstBounds.Y
			if handler.HandleMouseRelease(localEvent) {
				return true
			}
		}
	}

	if s.second != nil {
		if handler, ok := s.second.(interface {
			HandleMouseRelease(core.MouseReleaseEvent) bool
		}); ok {
			localEvent := event
			localEvent.X -= secondBounds.X
			localEvent.Y -= secondBounds.Y
			if handler.HandleMouseRelease(localEvent) {
				return true
			}
		}
	}

	return false
}

// IsDragging returns whether the divider is being dragged.
func (s *Splitter) IsDragging() bool {
	return s.dragging
}

// HandleKeyPress handles keyboard input.
func (s *Splitter) HandleKeyPress(event core.KeyPressEvent) bool {
	// If the splitter itself has focus, handle arrow keys for divider adjustment
	if s.HasFocus() {
		bounds := s.Bounds()
		metrics := s.EffectiveCellMetrics()

		// Calculate step sizes
		// Normal: small step (1 cell equivalent in position terms)
		// Large step (10 cells horizontal, 4 cells vertical) for modified keys
		var smallStep, largeStep float64
		if s.orientation == core.Horizontal {
			totalWidth := float64(bounds.Width - s.dividerThickness()) // Subtract divider
			if totalWidth > 0 {
				smallStep = float64(metrics.UnitsPerCellWidth) / totalWidth
				largeStep = float64(metrics.UnitsPerCellWidth*10) / totalWidth
			} else {
				smallStep = 0.02
				largeStep = 0.1
			}
		} else {
			totalHeight := float64(bounds.Height - s.dividerThickness())
			if totalHeight > 0 {
				smallStep = float64(metrics.UnitsPerCellHeight) / totalHeight
				largeStep = float64(metrics.UnitsPerCellHeight*4) / totalHeight
			} else {
				smallStep = 0.02
				largeStep = 0.1
			}
		}

		// The fine size is the small step and the coarse size the large one.
		// A step across the divider's own axis is all a splitter can do, so
		// the wrong axis falls through untouched rather than being swallowed.
		switch s.KeyCommand(event.Key) {
		case core.CmdWindowSizeFineLeft:
			if s.orientation == core.Horizontal {
				s.adjustPosition(-smallStep)
				return true
			}
		case core.CmdWindowSizeLeft:
			if s.orientation == core.Horizontal {
				s.adjustPosition(-largeStep)
				return true
			}
		case core.CmdWindowSizeFineRight:
			if s.orientation == core.Horizontal {
				s.adjustPosition(smallStep)
				return true
			}
		case core.CmdWindowSizeRight:
			if s.orientation == core.Horizontal {
				s.adjustPosition(largeStep)
				return true
			}
		case core.CmdWindowSizeFineUp:
			if s.orientation == core.Vertical {
				s.adjustPosition(-smallStep)
				return true
			}
		case core.CmdWindowSizeUp:
			if s.orientation == core.Vertical {
				s.adjustPosition(-largeStep)
				return true
			}
		case core.CmdWindowSizeFineDown:
			if s.orientation == core.Vertical {
				s.adjustPosition(smallStep)
				return true
			}
		case core.CmdWindowSizeDown:
			if s.orientation == core.Vertical {
				s.adjustPosition(largeStep)
				return true
			}
		}
	}

	// Forward to focused child
	if s.first != nil && s.first.HasFocus() {
		return s.first.HandleKeyPress(event)
	}
	if s.second != nil && s.second.HasFocus() {
		return s.second.HandleKeyPress(event)
	}
	return false
}

// adjustPosition adjusts the split position by the given delta.
func (s *Splitter) adjustPosition(delta float64) {
	newPos := s.position + delta
	if newPos < 0.1 {
		newPos = 0.1
	} else if newPos > 0.9 {
		newPos = 0.9
	}
	s.position = newPos
	s.Update()
}

// HandleFocusIn is called when focus is gained.
func (s *Splitter) HandleFocusIn() {
	s.Update()
}

// HandleFocusOut is called when focus is lost.
func (s *Splitter) HandleFocusOut() {
	s.Update()
}

// CollectFocusChain implements core.FocusChainProvider to ensure the splitter
// appears between its first and second children in the focus order.
func (s *Splitter) CollectFocusChain(collector func(core.Trinket)) {
	// First child and its descendants
	if s.first != nil {
		collector(s.first)
	}

	// Splitter itself (between children)
	collector(s)

	// Second child and its descendants
	if s.second != nil {
		collector(s.second)
	}
}

// AccessibleInfo returns accessibility information.
func (s *Splitter) AccessibleInfo() core.AccessibleInfo {
	info := s.AccessibleTrinket.AccessibleInfo()
	info.Role = core.RoleSplitter
	info.Value = ""
	return info
}
