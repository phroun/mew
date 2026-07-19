// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"fmt"
	"strings"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// ScrollBar represents a scrollbar trinket.
type ScrollBar struct {
	core.TrinketBase
	core.AccessibleTrinket

	orientation core.Orientation
	minimum     int
	maximum     int
	value       int
	pageStep    int
	singleStep  int

	// Appearance
	tracking bool // Update value while dragging

	// Drag state
	dragging   bool
	dragOffset int

	// Whether the pointer is hovering over the thumb.
	thumbHovered bool

	// Smooth (pixel-surface) drag state: the thumb follows the
	// pointer at unit granularity while the value still snaps to
	// whole steps. grabOff is where the press landed within the
	// thumb; smoothThumbPos is the unsnapped thumb origin in units.
	smoothDrag     bool
	grabOff        float64
	smoothThumbPos float64

	// Callbacks
	onValueChanged func(value int)
}

// NewScrollBar creates a new scrollbar.
func NewScrollBar(orientation core.Orientation) *ScrollBar {
	s := &ScrollBar{
		orientation: orientation,
		minimum:     0,
		maximum:     100,
		value:       0,
		pageStep:    10,
		singleStep:  1,
		tracking:    true,
	}
	s.TrinketBase = *core.NewTrinketBase()
	s.Init(s)
	s.SetFocusPolicy(core.NoFocus)
	s.SetAccessibleRole(core.RoleScrollBar)
	return s
}

// Orientation returns the orientation.
func (s *ScrollBar) Orientation() core.Orientation {
	return s.orientation
}

// Value returns the current value.
func (s *ScrollBar) Value() int {
	return s.value
}

// SetValue sets the current value.
func (s *ScrollBar) SetValue(value int) {
	if value < s.minimum {
		value = s.minimum
	}
	if value > s.maximum {
		value = s.maximum
	}
	if s.value == value {
		return
	}
	s.value = value
	s.Update()
	if s.onValueChanged != nil {
		s.onValueChanged(value)
	}
}

// Minimum returns the minimum value.
func (s *ScrollBar) Minimum() int {
	return s.minimum
}

// SetMinimum sets the minimum value.
func (s *ScrollBar) SetMinimum(min int) {
	s.minimum = min
	if s.value < min {
		s.SetValue(min)
	}
}

// Maximum returns the maximum value.
func (s *ScrollBar) Maximum() int {
	return s.maximum
}

// SetMaximum sets the maximum value.
func (s *ScrollBar) SetMaximum(max int) {
	s.maximum = max
	if s.value > max {
		s.SetValue(max)
	}
}

// SetRange sets the minimum and maximum values.
func (s *ScrollBar) SetRange(min, max int) {
	s.minimum = min
	s.maximum = max
	oldValue := s.value
	if s.value < min {
		s.value = min
	}
	if s.value > max {
		s.value = max
	}
	// Notify if value was clamped
	if s.value != oldValue && s.onValueChanged != nil {
		s.onValueChanged(s.value)
	}
	s.Update()
}

// PageStep returns the page step.
func (s *ScrollBar) PageStep() int {
	return s.pageStep
}

// SetPageStep sets the page step.
func (s *ScrollBar) SetPageStep(step int) {
	s.pageStep = step
}

// SingleStep returns the single step.
func (s *ScrollBar) SingleStep() int {
	return s.singleStep
}

// SetSingleStep sets the single step.
func (s *ScrollBar) SetSingleStep(step int) {
	s.singleStep = step
}

// SetOnValueChanged sets the value changed callback.
func (s *ScrollBar) SetOnValueChanged(handler func(value int)) {
	s.onValueChanged = handler
}

// SizeHint returns the preferred size. On pixel surfaces a
// horizontal bar is one column width tall (the same thickness as a
// vertical bar); on cell surfaces it cannot be thinner than a row.
func (s *ScrollBar) SizeHint() core.UnitSize {
	metrics := s.EffectiveCellMetrics()
	if s.orientation == core.Horizontal {
		height := metrics.CellHeight
		if core.FindSmoothPositioning(s.Self()) {
			height = metrics.CellHeight / 2
		}
		return core.UnitSize{
			Width:  metrics.CellWidth * 20,
			Height: height,
		}
	}
	return core.UnitSize{
		Width:  metrics.CellWidth,
		Height: metrics.CellHeight * 10,
	}
}

// thumbSpanUnits returns the track length, thumb length, and thumb
// origin along the scroll axis in units - the same proportions as the
// cell-based painter but without cell quantization. Mid-drag the
// thumb origin is the smooth (pointer-tracked) position rather than
// the value-derived one.
func (s *ScrollBar) thumbSpanUnits(bounds core.UnitRect, metrics core.CellMetrics) (trackU, thumbU, posU float64, ok bool) {
	if s.maximum <= s.minimum {
		return 0, 0, 0, false
	}
	var trackCells int
	if s.orientation == core.Horizontal {
		trackU = float64(bounds.Width)
		trackCells = metrics.CharsForWidth(bounds.Width)
	} else {
		trackU = float64(bounds.Height)
		trackCells = int(bounds.Height / metrics.CellHeight)
	}
	// The visible amount: pageStep when the owner set one (ScrollArea
	// keeps it at the viewport size in the bar's own denomination -
	// cells or units), the track's cell count as the classic proxy
	// otherwise.
	page := s.pageStep
	if page <= 0 {
		page = trackCells
	}
	totalSpan := s.maximum - s.minimum + page
	if trackU <= 0 || totalSpan <= 0 {
		return 0, 0, 0, false
	}
	thumbU = trackU * float64(page) / float64(totalSpan)
	if thumbU < 8 {
		thumbU = 8
	}
	if thumbU > trackU {
		thumbU = trackU
	}
	scrollable := trackU - thumbU
	maxScroll := s.maximum - s.minimum
	if s.dragging && s.smoothDrag {
		posU = s.smoothThumbPos
	} else if maxScroll > 0 && scrollable > 0 {
		posU = float64(s.value-s.minimum) * scrollable / float64(maxScroll)
	}
	if posU < 0 {
		posU = 0
	}
	if posU > scrollable {
		posU = scrollable
	}
	return trackU, thumbU, posU, true
}

// overThumb reports whether a scrollbar-local point lies on the thumb.
func (s *ScrollBar) overThumb(x, y core.Unit) bool {
	bounds := s.Bounds()
	if x < 0 || y < 0 || x >= bounds.Width || y >= bounds.Height {
		return false
	}
	metrics := s.EffectiveCellMetrics()
	if core.FindSmoothPositioning(s.Self()) {
		if _, thumbU, posU, ok := s.thumbSpanUnits(bounds, metrics); ok {
			pos := float64(y)
			if s.orientation == core.Horizontal {
				pos = float64(x)
			}
			return pos >= posU && pos < posU+thumbU
		}
		return false
	}
	if s.maximum <= s.minimum {
		return false
	}
	var clickPos, trackCells int
	if s.orientation == core.Horizontal {
		clickPos = metrics.UnitsToCellX(x)
		trackCells = metrics.CharsForWidth(bounds.Width)
	} else {
		clickPos = int(y / metrics.CellHeight)
		trackCells = int(bounds.Height / metrics.CellHeight)
	}
	totalItems := s.maximum - s.minimum + trackCells
	if totalItems <= 0 {
		return false
	}
	thumbSize := trackCells * trackCells / totalItems
	if thumbSize < 1 {
		thumbSize = 1
	}
	scrollableTrack := trackCells - thumbSize
	maxScroll := s.maximum - s.minimum
	thumbPos := 0
	if maxScroll > 0 && scrollableTrack > 0 {
		thumbPos = (s.value - s.minimum) * scrollableTrack / maxScroll
	}
	return clickPos >= thumbPos && clickPos < thumbPos+thumbSize
}

// UpdateThumbHover sets the thumb-hover state from a scrollbar-local
// point, repainting only on change. Containers that own a scrollbar and
// don't forward plain moves to it (they only forward while dragging) call
// this so the thumb still lights up on hover. Returns true on a change.
func (s *ScrollBar) UpdateThumbHover(x, y core.Unit) bool {
	over := s.overThumb(x, y)
	if over != s.thumbHovered {
		s.thumbHovered = over
		s.Update()
		return true
	}
	return false
}

// Paint renders the scrollbar.
func (s *ScrollBar) Paint(p *core.Painter) {
	bounds := s.Bounds()
	scheme := s.GetScheme()
	metrics := s.EffectiveCellMetrics()

	if s.orientation == core.Horizontal {
		s.paintHorizontal(p, bounds, scheme, metrics)
	} else {
		s.paintVertical(p, bounds, scheme, metrics)
	}
}

func (s *ScrollBar) paintHorizontal(p *core.Painter, bounds core.UnitRect, scheme *style.Scheme, metrics core.CellMetrics) {
	// Draw track
	trackStyle := scheme.GetScrollbar()
	p.FillRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, '░', trackStyle)

	// Pixel surfaces paint the thumb at unit granularity so it can
	// sit (and move) between cell boundaries.
	if p.Graphical() {
		if _, thumbU, posU, ok := s.thumbSpanUnits(bounds, metrics); ok {
			thumbStyle := scheme.GetScrollbarThumbState(s.thumbHovered && p.Graphical())
			p.FillRect(core.UnitRect{
				X:      core.Unit(posU + 0.5),
				Width:  core.Unit(thumbU + 0.5),
				Height: bounds.Height,
			}, ' ', thumbStyle.WithBg(thumbStyle.Fg))
		}
		return
	}

	// Calculate thumb using ListView-style formula:
	// thumbSize = visibleCount² / totalItems
	// where visibleCount = trackCells, totalItems = maximum + trackCells (when min=0)
	if s.maximum > s.minimum {
		trackCells := metrics.CharsForWidth(bounds.Width)
		// totalItems = scrollRange + visibleCount = (max - min) + trackCells
		totalItems := s.maximum - s.minimum + trackCells
		thumbSize := trackCells * trackCells / totalItems
		if thumbSize < 1 {
			thumbSize = 1
		}
		if thumbSize > trackCells {
			thumbSize = trackCells
		}

		// thumbPos = scrollOffset * scrollableTrack / maxScroll
		scrollableTrack := trackCells - thumbSize
		maxScroll := s.maximum - s.minimum
		thumbPos := 0
		if maxScroll > 0 && scrollableTrack > 0 {
			thumbPos = (s.value - s.minimum) * scrollableTrack / maxScroll
		}
		if thumbPos < 0 {
			thumbPos = 0
		}

		// Draw thumb
		thumbStyle := scheme.GetScrollbarThumbState(s.thumbHovered && p.Graphical())
		for i := 0; i < thumbSize; i++ {
			x := core.Unit(thumbPos+i) * metrics.CellWidth
			p.DrawCell(x, 0, '█', thumbStyle)
		}
	}
}

func (s *ScrollBar) paintVertical(p *core.Painter, bounds core.UnitRect, scheme *style.Scheme, metrics core.CellMetrics) {
	// Draw track
	trackStyle := scheme.GetScrollbar()
	p.FillRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, '░', trackStyle)

	// Pixel surfaces paint the thumb at unit granularity so it can
	// sit (and move) between cell boundaries.
	if p.Graphical() {
		if _, thumbU, posU, ok := s.thumbSpanUnits(bounds, metrics); ok {
			thumbStyle := scheme.GetScrollbarThumbState(s.thumbHovered && p.Graphical())
			p.FillRect(core.UnitRect{
				Y:      core.Unit(posU + 0.5),
				Width:  bounds.Width,
				Height: core.Unit(thumbU + 0.5),
			}, ' ', thumbStyle.WithBg(thumbStyle.Fg))
		}
		return
	}

	// Calculate thumb using ListView-style formula:
	// thumbSize = visibleCount² / totalItems
	// where visibleCount = trackCells, totalItems = maximum + trackCells (when min=0)
	if s.maximum > s.minimum {
		trackCells := int(bounds.Height / metrics.CellHeight)
		// totalItems = scrollRange + visibleCount = (max - min) + trackCells
		totalItems := s.maximum - s.minimum + trackCells
		thumbSize := trackCells * trackCells / totalItems
		if thumbSize < 1 {
			thumbSize = 1
		}
		if thumbSize > trackCells {
			thumbSize = trackCells
		}

		// thumbPos = scrollOffset * scrollableTrack / maxScroll
		scrollableTrack := trackCells - thumbSize
		maxScroll := s.maximum - s.minimum
		thumbPos := 0
		if maxScroll > 0 && scrollableTrack > 0 {
			thumbPos = (s.value - s.minimum) * scrollableTrack / maxScroll
		}
		if thumbPos < 0 {
			thumbPos = 0
		}

		// Draw thumb
		thumbStyle := scheme.GetScrollbarThumbState(s.thumbHovered && p.Graphical())
		for i := 0; i < thumbSize; i++ {
			y := core.Unit(thumbPos+i) * metrics.CellHeight
			p.FillRect(core.UnitRect{Y: y, Width: bounds.Width, Height: metrics.CellHeight}, '█', thumbStyle)
		}
	}
}

// HandleMousePress handles mouse clicks.
func (s *ScrollBar) HandleMousePress(event core.MousePressEvent) bool {
	if event.Button != core.LeftButton {
		return false
	}

	metrics := s.EffectiveCellMetrics()
	bounds := s.Bounds()

	// Pixel surfaces track the thumb at unit granularity: anchor the
	// drag to the grab point within the thumb; the value still snaps
	// to whole steps.
	if core.FindSmoothPositioning(s.Self()) {
		if _, thumbU, posU, ok := s.thumbSpanUnits(bounds, metrics); ok {
			pos := float64(event.Y)
			if s.orientation == core.Horizontal {
				pos = float64(event.X)
			}
			if pos >= posU && pos < posU+thumbU {
				s.dragging = true
				s.smoothDrag = true
				s.grabOff = pos - posU
				s.smoothThumbPos = posU
			} else if pos < posU {
				s.SetValue(s.value - s.pageStep)
			} else {
				s.SetValue(s.value + s.pageStep)
			}
		}
		return true
	}

	if s.orientation == core.Horizontal {
		clickPos := metrics.UnitsToCellX(event.X)
		trackCells := metrics.CharsForWidth(bounds.Width)
		// Use ListView-style formula
		totalItems := s.maximum - s.minimum + trackCells
		thumbSize := trackCells * trackCells / totalItems
		if thumbSize < 1 {
			thumbSize = 1
		}
		scrollableTrack := trackCells - thumbSize
		maxScroll := s.maximum - s.minimum
		thumbPos := 0
		if maxScroll > 0 && scrollableTrack > 0 {
			thumbPos = (s.value - s.minimum) * scrollableTrack / maxScroll
		}

		if clickPos >= thumbPos && clickPos < thumbPos+thumbSize {
			// Start dragging
			s.dragging = true
			s.dragOffset = clickPos - thumbPos
		} else if clickPos < thumbPos {
			// Page up
			s.SetValue(s.value - s.pageStep)
		} else {
			// Page down
			s.SetValue(s.value + s.pageStep)
		}
	} else {
		clickPos := int(event.Y / metrics.CellHeight)
		trackCells := int(bounds.Height / metrics.CellHeight)
		// Use ListView-style formula
		totalItems := s.maximum - s.minimum + trackCells
		thumbSize := trackCells * trackCells / totalItems
		if thumbSize < 1 {
			thumbSize = 1
		}
		scrollableTrack := trackCells - thumbSize
		maxScroll := s.maximum - s.minimum
		thumbPos := 0
		if maxScroll > 0 && scrollableTrack > 0 {
			thumbPos = (s.value - s.minimum) * scrollableTrack / maxScroll
		}

		if clickPos >= thumbPos && clickPos < thumbPos+thumbSize {
			// Start dragging
			s.dragging = true
			s.dragOffset = clickPos - thumbPos
		} else if clickPos < thumbPos {
			// Page up
			s.SetValue(s.value - s.pageStep)
		} else {
			// Page down
			s.SetValue(s.value + s.pageStep)
		}
	}

	return true
}

// HandleMouseMove handles mouse move/drag events.
func (s *ScrollBar) HandleMouseMove(event core.MouseMoveEvent) bool {
	if !s.dragging {
		// Plain hover over the thumb. Don't consume the move. Hover is a
		// no-button affordance: a held button means a drag begun elsewhere is
		// passing over, so clear rather than highlight (off-point clears).
		if event.Buttons == 0 {
			s.UpdateThumbHover(event.X, event.Y)
		} else {
			s.UpdateThumbHover(-1, -1)
		}
		return false
	}

	metrics := s.EffectiveCellMetrics()
	bounds := s.Bounds()

	// Smooth drag: the thumb follows the pointer in units, the value
	// snaps to the nearest whole step.
	if s.smoothDrag {
		if trackU, thumbU, _, ok := s.thumbSpanUnits(bounds, metrics); ok {
			scrollable := trackU - thumbU
			pos := float64(event.Y)
			if s.orientation == core.Horizontal {
				pos = float64(event.X)
			}
			newPos := pos - s.grabOff
			if newPos < 0 {
				newPos = 0
			}
			if newPos > scrollable {
				newPos = scrollable
			}
			s.smoothThumbPos = newPos
			maxScroll := s.maximum - s.minimum
			newValue := s.minimum
			if scrollable > 0 {
				newValue = s.minimum + int(newPos*float64(maxScroll)/scrollable+0.5)
			}
			if s.tracking {
				s.SetValue(newValue)
			}
			// The thumb moves even when the snapped value does not.
			s.Update()
		}
		return true
	}

	if s.orientation == core.Horizontal {
		dragPos := metrics.UnitsToCellX(event.X)
		trackCells := metrics.CharsForWidth(bounds.Width)
		// Use ListView-style formula
		totalItems := s.maximum - s.minimum + trackCells
		thumbSize := trackCells * trackCells / totalItems
		if thumbSize < 1 {
			thumbSize = 1
		}

		scrollableTrack := trackCells - thumbSize
		newThumbPos := dragPos - s.dragOffset
		if newThumbPos < 0 {
			newThumbPos = 0
		}
		if newThumbPos > scrollableTrack {
			newThumbPos = scrollableTrack
		}

		// Convert thumb position to scroll value
		maxScroll := s.maximum - s.minimum
		newValue := s.minimum
		if scrollableTrack > 0 {
			newValue = s.minimum + newThumbPos*maxScroll/scrollableTrack
		}
		if s.tracking {
			s.SetValue(newValue)
		}
	} else {
		dragPos := int(event.Y / metrics.CellHeight)
		trackCells := int(bounds.Height / metrics.CellHeight)
		// Use ListView-style formula
		totalItems := s.maximum - s.minimum + trackCells
		thumbSize := trackCells * trackCells / totalItems
		if thumbSize < 1 {
			thumbSize = 1
		}

		scrollableTrack := trackCells - thumbSize
		newThumbPos := dragPos - s.dragOffset
		if newThumbPos < 0 {
			newThumbPos = 0
		}
		if newThumbPos > scrollableTrack {
			newThumbPos = scrollableTrack
		}

		// Convert thumb position to scroll value
		maxScroll := s.maximum - s.minimum
		newValue := s.minimum
		if scrollableTrack > 0 {
			newValue = s.minimum + newThumbPos*maxScroll/scrollableTrack
		}
		if s.tracking {
			s.SetValue(newValue)
		}
	}

	return true
}

// HandleMouseRelease handles mouse release events.
func (s *ScrollBar) HandleMouseRelease(event core.MouseReleaseEvent) bool {
	if s.dragging {
		s.dragging = false
		s.smoothDrag = false
		s.Update()
		return true
	}
	return false
}

// AccessibleInfo returns accessibility information.
func (s *ScrollBar) AccessibleInfo() core.AccessibleInfo {
	info := s.AccessibleTrinket.AccessibleInfo()
	info.Role = core.RoleScrollBar
	info.ValueMin = string(rune('0' + s.minimum))
	info.ValueMax = string(rune('0' + s.maximum))

	return info
}

// ScrollArea provides a scrollable viewport for a trinket.
type ScrollArea struct {
	core.TrinketBase
	core.AccessibleTrinket

	content       core.Trinket
	scrollX       int
	scrollY       int
	contentWidth  core.Unit
	contentHeight core.Unit

	// Scrollbars
	hScrollBar *ScrollBar
	vScrollBar *ScrollBar

	// Policy
	hScrollBarPolicy ScrollBarPolicy
	vScrollBarPolicy ScrollBarPolicy

	// Sub-unit wheel remainders carried between trackpad events.
	wheelCarryX, wheelCarryY float64

	// Appearance
	trinketResizable bool // If true, content trinket is resized to viewport
}

// ScrollBarPolicy determines when to show scrollbars.
type ScrollBarPolicy int

const (
	ScrollBarAsNeeded ScrollBarPolicy = iota
	ScrollBarAlwaysOn
	ScrollBarAlwaysOff
)

// NewScrollArea creates a new scroll area.
func NewScrollArea() *ScrollArea {
	s := &ScrollArea{
		hScrollBarPolicy: ScrollBarAsNeeded,
		vScrollBarPolicy: ScrollBarAsNeeded,
	}
	s.TrinketBase = *core.NewTrinketBase()
	s.Init(s)
	s.SetFocusPolicy(core.StrongFocus)
	s.SetAccessibleRole(core.RoleGroup)

	// Create scrollbars. They are parented so ancestry-based
	// capability lookups (smooth positioning, metrics) resolve.
	s.hScrollBar = NewScrollBar(core.Horizontal)
	s.hScrollBar.SetParent(s)
	s.hScrollBar.SetOnValueChanged(func(value int) {
		s.scrollX = value
		s.Update()
	})

	s.vScrollBar = NewScrollBar(core.Vertical)
	s.vScrollBar.SetParent(s)
	s.vScrollBar.SetOnValueChanged(func(value int) {
		s.scrollY = value
		s.Update()
	})

	return s
}

// scrollBarPercent returns the thumb position along the track as a
// percentage (0 = start, 100 = end), rounded to the nearest integer.
func scrollBarPercent(sb *ScrollBar) int {
	span := sb.Maximum() - sb.Minimum()
	if span <= 0 {
		return 0
	}
	return int(float64(sb.Value()-sb.Minimum())*100.0/float64(span) + 0.5)
}

// Content returns the content trinket.
func (s *ScrollArea) Content() core.Trinket {
	return s.content
}

// SetContent sets the content trinket.
func (s *ScrollArea) SetContent(content core.Trinket) {
	if s.content != nil {
		s.content.SetParent(nil)
	}
	s.content = content
	if content != nil {
		content.SetParent(s)
	}
	s.updateScrollBars()
	s.Update()
}

// Children returns all child trinkets (the content trinket if set).
func (s *ScrollArea) Children() []core.Trinket {
	if s.content != nil {
		return []core.Trinket{s.content}
	}
	return nil
}

// AddChild adds a child trinket (sets as content).
func (s *ScrollArea) AddChild(child core.Trinket) {
	s.SetContent(child)
}

// RemoveChild removes a child trinket.
func (s *ScrollArea) RemoveChild(child core.Trinket) {
	if s.content == child {
		s.SetContent(nil)
	}
}

// ChildAt returns the child at the given position.
func (s *ScrollArea) ChildAt(pos core.UnitPoint) core.Trinket {
	if s.content != nil {
		viewport := s.viewportBounds()
		if pos.X >= viewport.X && pos.X < viewport.X+viewport.Width &&
			pos.Y >= viewport.Y && pos.Y < viewport.Y+viewport.Height {
			return s.content
		}
	}
	return nil
}

// Layout arranges the content within the viewport.
func (s *ScrollArea) Layout() {
	s.updateScrollBars()

	// Force content to re-layout with fresh SizeHints
	// (important when font changes affect trinket sizing)
	if s.content != nil {
		if container, ok := s.content.(core.Container); ok {
			container.Layout()
		}
	}
}

// LayoutManager returns nil (ScrollArea manages its own layout).
func (s *ScrollArea) LayoutManager() core.LayoutManager {
	return nil
}

// SetLayoutManager is a no-op (ScrollArea manages its own layout).
func (s *ScrollArea) SetLayoutManager(layout core.LayoutManager) {
	// ScrollArea manages its own layout, ignore external layout managers
}

// ScrollX returns the horizontal scroll position.
func (s *ScrollArea) ScrollX() int {
	return s.scrollX
}

// SetScrollX sets the horizontal scroll position.
func (s *ScrollArea) SetScrollX(x int) {
	s.hScrollBar.SetValue(x)
}

// ScrollY returns the vertical scroll position.
func (s *ScrollArea) ScrollY() int {
	return s.scrollY
}

// SetScrollY sets the vertical scroll position.
func (s *ScrollArea) SetScrollY(y int) {
	s.vScrollBar.SetValue(y)
}

// ScrollTo scrolls to a specific position.
func (s *ScrollArea) ScrollTo(x, y int) {
	s.SetScrollX(x)
	s.SetScrollY(y)
}

// ScrollOffset returns the current scroll offset in cell units.
// Implements core.ScrollOffsetProvider.
func (s *ScrollArea) ScrollOffset() (x, y int) {
	return s.scrollX, s.scrollY
}

// ScrollOffsetUnits implements core.ScrollOffsetUnitsProvider: the
// current offsets in layout units regardless of denomination.
func (s *ScrollArea) ScrollOffsetUnits() (core.Unit, core.Unit) {
	return s.scrollOffsetUnits()
}

// smoothScroll reports whether this surface scrolls content at unit
// granularity (pixel surfaces). Cell surfaces stay quantized to
// whole rows and columns. Content that WANTS quantized output (the
// terminal) implements its own snapping - the scroll area does not
// impose it.
func (s *ScrollArea) smoothScroll() bool {
	return core.FindSmoothPositioning(s.Self())
}

// scrollOffsetUnits returns the scroll offsets in layout units: the
// scrollbar values are unit-denominated on smooth surfaces and
// cell-denominated otherwise.
func (s *ScrollArea) scrollOffsetUnits() (core.Unit, core.Unit) {
	if s.smoothScroll() {
		return core.Unit(s.scrollX), core.Unit(s.scrollY)
	}
	metrics := s.EffectiveCellMetrics()
	return core.Unit(s.scrollX) * metrics.CellWidth, core.Unit(s.scrollY) * metrics.CellHeight
}

// EnsureVisible scrolls to make a point visible.
func (s *ScrollArea) EnsureVisible(x, y core.Unit) {
	s.EnsureRectVisible(core.UnitRect{X: x, Y: y, Width: 1, Height: 1})
}

// EnsureRectVisible scrolls to make a rectangle visible within the viewport.
// Prioritizes showing the left/top edge of the rectangle.
func (s *ScrollArea) EnsureRectVisible(rect core.UnitRect) {
	viewport := s.viewportBounds()
	metrics := s.EffectiveCellMetrics()

	// Smooth surfaces scroll the minimum distance in units.
	if s.smoothScroll() {
		if rect.X < core.Unit(s.scrollX) {
			s.SetScrollX(int(rect.X))
		} else if rect.X+rect.Width > core.Unit(s.scrollX)+viewport.Width {
			newX := rect.X + rect.Width - viewport.Width
			if newX > rect.X {
				newX = rect.X // never hide the left edge
			}
			s.SetScrollX(int(newX))
		}
		if rect.Y < core.Unit(s.scrollY) {
			s.SetScrollY(int(rect.Y))
		} else if rect.Y+rect.Height > core.Unit(s.scrollY)+viewport.Height {
			newY := rect.Y + rect.Height - viewport.Height
			if newY > rect.Y {
				newY = rect.Y // never hide the top edge
			}
			s.SetScrollY(int(newY))
		}
		return
	}

	// Calculate cell positions
	cellX := metrics.UnitsToCellX(rect.X)
	cellY := int(rect.Y / metrics.CellHeight)
	cellWidth := metrics.CharsForWidth(rect.Width)
	cellHeight := int(rect.Height / metrics.CellHeight)
	if cellWidth < 1 {
		cellWidth = 1
	}
	if cellHeight < 1 {
		cellHeight = 1
	}

	viewCellWidth := metrics.CharsForWidth(viewport.Width)
	viewCellHeight := int(viewport.Height / metrics.CellHeight)

	// Adjust horizontal scroll if needed - prioritize showing left edge
	if cellX < s.scrollX {
		// Left edge is not visible - scroll left to show it
		s.SetScrollX(cellX)
	} else if cellX+cellWidth > s.scrollX+viewCellWidth && cellX >= s.scrollX {
		// Right edge extends past viewport but left edge is visible
		// Scroll right, but never hide the left edge
		newScrollX := cellX + cellWidth - viewCellWidth
		if newScrollX > cellX {
			// Would hide left edge - just show left edge instead
			newScrollX = cellX
		}
		s.SetScrollX(newScrollX)
	}

	// Adjust vertical scroll if needed - prioritize showing top edge
	if cellY < s.scrollY {
		// Top edge is not visible - scroll up to show it
		s.SetScrollY(cellY)
	} else if cellY+cellHeight > s.scrollY+viewCellHeight && cellY >= s.scrollY {
		// Bottom edge extends past viewport but top edge is visible
		// Scroll down, but never hide the top edge
		newScrollY := cellY + cellHeight - viewCellHeight
		if newScrollY > cellY {
			// Would hide top edge - just show top edge instead
			newScrollY = cellY
		}
		s.SetScrollY(newScrollY)
	}
}

// ScrollChildIntoView scrolls to make a descendant trinket visible.
// Implements core.ScrollIntoViewHandler for automatic focus scrolling.
func (s *ScrollArea) ScrollChildIntoView(child core.Trinket) {
	if s.content == nil {
		return
	}

	// Calculate the child's position relative to our content
	// by walking up from the child to our content trinket
	childBounds := child.Bounds()
	offsetX := childBounds.X
	offsetY := childBounds.Y

	// Check if this is a proxy from ScrollRectIntoView - if so, the parent
	// will be the ScrollArea itself and the bounds already contain the
	// content-relative position (no need to accumulate more offsets)
	parent := child.Parent()
	if parent == s {
		// Bounds already contain content-relative coordinates
		s.EnsureRectVisible(core.UnitRect{
			X:      offsetX,
			Y:      offsetY,
			Width:  childBounds.Width,
			Height: childBounds.Height,
		})
		return
	}

	// Walk up the parent chain until we reach our content trinket
	current := parent
	for current != nil {
		// Stop if we've reached our content trinket
		if trinket, ok := current.(core.Trinket); ok {
			if trinket == s.content {
				break
			}
			// Accumulate the offset from this parent
			parentBounds := trinket.Bounds()
			offsetX += parentBounds.X
			offsetY += parentBounds.Y
			current = trinket.Parent()
		} else {
			break
		}
	}

	// Ensure the calculated rectangle is visible
	s.EnsureRectVisible(core.UnitRect{
		X:      offsetX,
		Y:      offsetY,
		Width:  childBounds.Width,
		Height: childBounds.Height,
	})
}

// HorizontalScrollBarPolicy returns the horizontal scrollbar policy.
func (s *ScrollArea) HorizontalScrollBarPolicy() ScrollBarPolicy {
	return s.hScrollBarPolicy
}

// SetHorizontalScrollBarPolicy sets the horizontal scrollbar policy.
func (s *ScrollArea) SetHorizontalScrollBarPolicy(policy ScrollBarPolicy) {
	s.hScrollBarPolicy = policy
	s.updateScrollBars()
	s.Update()
}

// VerticalScrollBarPolicy returns the vertical scrollbar policy.
func (s *ScrollArea) VerticalScrollBarPolicy() ScrollBarPolicy {
	return s.vScrollBarPolicy
}

// SetVerticalScrollBarPolicy sets the vertical scrollbar policy.
func (s *ScrollArea) SetVerticalScrollBarPolicy(policy ScrollBarPolicy) {
	s.vScrollBarPolicy = policy
	s.updateScrollBars()
	s.Update()
}

// IsTrinketResizable returns whether the content trinket is resized to viewport.
func (s *ScrollArea) IsTrinketResizable() bool {
	return s.trinketResizable
}

// SetTrinketResizable sets whether the content trinket is resized to viewport.
func (s *ScrollArea) SetTrinketResizable(resizable bool) {
	s.trinketResizable = resizable
	s.Update()
}

// hScrollBarHeight returns the horizontal scrollbar's lane height:
// half a row on pixel surfaces (the same thickness as the vertical
// bar's column at standard metrics, expressed in the Y denomination
// so re-denominated interiors keep the proportion), a full cell row
// on cell surfaces where a bar cannot be thinner than a character.
func (s *ScrollArea) hScrollBarHeight() core.Unit {
	metrics := s.EffectiveCellMetrics()
	if core.FindSmoothPositioning(s.Self()) {
		return metrics.CellHeight / 2
	}
	return metrics.CellHeight
}

// viewportBounds returns the viewport bounds (excluding scrollbars).
func (s *ScrollArea) viewportBounds() core.UnitRect {
	bounds := s.Bounds()
	metrics := s.EffectiveCellMetrics()

	width := bounds.Width
	height := bounds.Height

	// Calculate scrollbar needs based on raw bounds to avoid recursion
	needsV, needsH := s.calculateScrollBarNeeds()

	if needsV {
		width -= metrics.CellWidth
	}
	if needsH {
		height -= s.hScrollBarHeight()
	}

	return core.UnitRect{Width: width, Height: height}
}

// calculateScrollBarNeeds determines if scrollbars are needed without recursion.
// Returns (needsVertical, needsHorizontal).
func (s *ScrollArea) calculateScrollBarNeeds() (bool, bool) {
	bounds := s.Bounds()
	metrics := s.EffectiveCellMetrics()

	// First pass: check if scrollbars needed with full bounds
	needsV := false
	needsH := false

	switch s.vScrollBarPolicy {
	case ScrollBarAlwaysOff:
		needsV = false
	case ScrollBarAlwaysOn:
		needsV = true
	default: // ScrollBarAsNeeded
		needsV = s.contentHeight > bounds.Height
	}

	switch s.hScrollBarPolicy {
	case ScrollBarAlwaysOff:
		needsH = false
	case ScrollBarAlwaysOn:
		needsH = true
	default: // ScrollBarAsNeeded
		needsH = s.contentWidth > bounds.Width
	}

	// Second pass: if one scrollbar is shown, it reduces space for the other
	if needsV && s.hScrollBarPolicy == ScrollBarAsNeeded {
		needsH = s.contentWidth > (bounds.Width - metrics.CellWidth)
	}
	if needsH && s.vScrollBarPolicy == ScrollBarAsNeeded {
		needsV = s.contentHeight > (bounds.Height - s.hScrollBarHeight())
	}

	return needsV, needsH
}

func (s *ScrollArea) needsHScrollBar() bool {
	_, needsH := s.calculateScrollBarNeeds()
	return needsH
}

func (s *ScrollArea) needsVScrollBar() bool {
	needsV, _ := s.calculateScrollBarNeeds()
	return needsV
}

func (s *ScrollArea) updateScrollBars() {
	if s.content == nil {
		return
	}

	hint := s.content.SizeHint()
	s.contentWidth = hint.Width
	s.contentHeight = hint.Height

	viewport := s.viewportBounds()
	metrics := s.EffectiveCellMetrics()

	// Smooth surfaces denominate the scrollbars in units: content
	// scrolls at pixel granularity, stepping one cell per wheel or
	// arrow notch.
	if s.smoothScroll() {
		maxScrollX := int(s.contentWidth - viewport.Width)
		if maxScrollX < 0 {
			maxScrollX = 0
		}
		s.hScrollBar.SetRange(0, maxScrollX)
		s.hScrollBar.SetPageStep(int(viewport.Width))
		s.hScrollBar.SetSingleStep(int(metrics.CellWidth))

		maxScrollY := int(s.contentHeight - viewport.Height)
		if maxScrollY < 0 {
			maxScrollY = 0
		}
		s.vScrollBar.SetRange(0, maxScrollY)
		s.vScrollBar.SetPageStep(int(viewport.Height))
		s.vScrollBar.SetSingleStep(int(metrics.CellHeight))
		return
	}

	// Update horizontal scrollbar using ListView-style calculation
	// visible = viewport cells, total = content cells
	viewCellWidth := metrics.CharsForWidth(viewport.Width)
	contentCellWidth := metrics.CharsForWidth(s.contentWidth)
	maxScrollX := contentCellWidth - viewCellWidth
	if maxScrollX < 0 {
		maxScrollX = 0
	}
	s.hScrollBar.SetRange(0, maxScrollX)
	s.hScrollBar.SetPageStep(viewCellWidth)
	s.hScrollBar.SetSingleStep(1)

	// Update vertical scrollbar using ListView-style calculation
	viewCellHeight := int(viewport.Height / metrics.CellHeight)
	contentCellHeight := int(s.contentHeight / metrics.CellHeight)
	maxScrollY := contentCellHeight - viewCellHeight
	if maxScrollY < 0 {
		maxScrollY = 0
	}
	s.vScrollBar.SetRange(0, maxScrollY)
	s.vScrollBar.SetPageStep(viewCellHeight)
	s.vScrollBar.SetSingleStep(1)
}

// SizeHint returns the preferred size.
func (s *ScrollArea) SizeHint() core.UnitSize {
	metrics := s.EffectiveCellMetrics()
	font := s.EffectiveFont()
	return core.UnitSize{
		Width:  font.MeasureRunes(30), // 30 chars wide
		Height: metrics.TextHeight(10),
	}
}

// paintEdgeFades overlays a gradient on each viewport edge that has more
// content beyond it - a scroll affordance on the graphical path only. The
// fade is 1 row tall on the top/bottom and 2 columns wide on the left/right,
// running from fully transparent on its inner edge to fully opaque (the
// scroll-area background) on the outer edge. Where two fades meet at a corner
// they are mitered: the corner square is split along its diagonal so the two
// gradients abut instead of double-darkening (they meet at equal opacity, so
// the seam is invisible).
func (s *ScrollArea) paintEdgeFades(p *core.Painter, viewport core.UnitRect) {
	if !p.Graphical() {
		return
	}

	// An edge fades when it can scroll further that way (more content lies
	// beyond it) and its scrollbar is actually in play.
	vScroll := s.needsVScrollBar()
	hScroll := s.needsHScrollBar()
	showTop := vScroll && s.vScrollBar.Value() > s.vScrollBar.Minimum()
	showBottom := vScroll && s.vScrollBar.Value() < s.vScrollBar.Maximum()
	showLeft := hScroll && s.hScrollBar.Value() > s.hScrollBar.Minimum()
	showRight := hScroll && s.hScrollBar.Value() < s.hScrollBar.Maximum()
	if !showTop && !showBottom && !showLeft && !showRight {
		return
	}

	metrics := s.EffectiveCellMetrics()
	wvPx := p.UnitSpanPxX(0, viewport.Width)
	hvPx := p.UnitSpanPxY(0, viewport.Height)
	if wvPx <= 0 || hvPx <= 0 {
		return
	}
	// Fade thickness: one row deep on the top/bottom, two columns on the
	// left/right - clamped so opposing fades can't cross a small viewport.
	htPx := p.UnitSpanPxY(0, metrics.CellHeight)
	wtPx := p.UnitSpanPxX(0, metrics.CellWidth*2)
	if htPx > hvPx/2 {
		htPx = hvPx / 2
	}
	if wtPx > wvPx/2 {
		wtPx = wvPx / 2
	}
	if htPx <= 0 || wtPx <= 0 {
		return
	}

	r, g, b := s.EffectiveBackgroundColor().RGBComponents()

	// Corner ownership (matched exactly by the two loops below so the fades
	// partition each corner square along its diagonal): the horizontal fade
	// owns pixel (col, row) when col*htPx >= row*wtPx; the vertical fade owns
	// the complement. The insets gate on the perpendicular fade being present.
	//
	// alphaAt gives the opacity of a fade at device-depth d (0 = outer edge)
	// of thickness t: opaque at the outer edge, transparent at the inner one.
	alphaAt := func(d, t int) float64 {
		return 1.0 - (float64(d)+0.5)/float64(t)
	}

	// Top fade: rows from the top edge inward.
	if showTop {
		for k := 0; k < htPx; k++ {
			x0, x1 := 0, wvPx
			if showLeft {
				x0 = (k*wtPx + htPx - 1) / htPx // ceil(k*wtPx/htPx)
			}
			if showRight {
				x1 = wvPx - (k*wtPx+htPx-1)/htPx
			}
			if x1 > x0 {
				p.FillRectPixelsAlpha(0, 0, x0, k, x1-x0, 1, r, g, b, alphaAt(k, htPx))
			}
		}
	}
	// Bottom fade: rows from the bottom edge inward.
	if showBottom {
		for k := 0; k < htPx; k++ {
			x0, x1 := 0, wvPx
			if showLeft {
				x0 = (k*wtPx + htPx - 1) / htPx
			}
			if showRight {
				x1 = wvPx - (k*wtPx+htPx-1)/htPx
			}
			if x1 > x0 {
				p.FillRectPixelsAlpha(0, 0, x0, hvPx-1-k, x1-x0, 1, r, g, b, alphaAt(k, htPx))
			}
		}
	}
	// Left fade: columns from the left edge inward.
	if showLeft {
		for j := 0; j < wtPx; j++ {
			y0, y1 := 0, hvPx
			if showTop {
				y0 = j*htPx/wtPx + 1 // floor(j*htPx/wtPx)+1
			}
			if showBottom {
				y1 = hvPx - (j*htPx/wtPx + 1)
			}
			if y1 > y0 {
				p.FillRectPixelsAlpha(0, 0, j, y0, 1, y1-y0, r, g, b, alphaAt(j, wtPx))
			}
		}
	}
	// Right fade: columns from the right edge inward.
	if showRight {
		for j := 0; j < wtPx; j++ {
			y0, y1 := 0, hvPx
			if showTop {
				y0 = j*htPx/wtPx + 1
			}
			if showBottom {
				y1 = hvPx - (j*htPx/wtPx + 1)
			}
			if y1 > y0 {
				p.FillRectPixelsAlpha(0, 0, wvPx-1-j, y0, 1, y1-y0, r, g, b, alphaAt(j, wtPx))
			}
		}
	}
}

// Paint renders the scroll area.
func (s *ScrollArea) Paint(p *core.Painter) {
	bounds := s.Bounds()
	scheme := s.GetScheme()
	metrics := s.EffectiveCellMetrics()

	// Draw background using inherited background color
	inheritedBg := s.EffectiveBackgroundColor()
	bgStyle := scheme.GetNormal(true).WithBg(inheritedBg)
	p.FillRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, ' ', bgStyle)

	viewport := s.viewportBounds()

	// Draw content
	if s.content != nil {
		scrollOffsetX, scrollOffsetY := s.scrollOffsetUnits()

		contentBounds := core.UnitRect{
			X:      -scrollOffsetX,
			Y:      -scrollOffsetY,
			Width:  s.contentWidth,
			Height: s.contentHeight,
		}

		if s.trinketResizable {
			contentBounds.Width = viewport.Width
			contentBounds.Height = viewport.Height
		}

		s.content.SetBounds(core.UnitRect{
			Width:  contentBounds.Width,
			Height: contentBounds.Height,
		})

		// Create clipped painter
		contentPainter := p.WithOffset(contentBounds.X, contentBounds.Y).
			WithClip(core.UnitRect{
				X:      scrollOffsetX,
				Y:      scrollOffsetY,
				Width:  viewport.Width,
				Height: viewport.Height,
			})
		s.content.Paint(contentPainter)
	}

	// Fade the content toward the scroll-area background on any edge that has
	// more content beyond it. Painted over the content (even an MDI window)
	// but under the scrollbars, and it never touches event handling.
	s.paintEdgeFades(p, viewport)

	// Draw vertical scrollbar (use offset painter since scrollbar paints at 0,0)
	if s.needsVScrollBar() {
		s.vScrollBar.SetBounds(core.UnitRect{
			X:      0,
			Y:      0,
			Width:  metrics.CellWidth,
			Height: viewport.Height,
		})
		s.vScrollBar.Paint(p.WithOffset(viewport.Width, 0))
	}

	// Draw horizontal scrollbar (use offset painter since scrollbar paints at 0,0)
	if s.needsHScrollBar() {
		s.hScrollBar.SetBounds(core.UnitRect{
			X:      0,
			Y:      0,
			Width:  viewport.Width,
			Height: s.hScrollBarHeight(),
		})
		s.hScrollBar.Paint(p.WithOffset(0, viewport.Height))
	}

	// Draw corner if both scrollbars visible (sized to the lanes, not
	// a full cell: the horizontal lane may be thinner than a row)
	if s.needsHScrollBar() && s.needsVScrollBar() {
		p.FillRect(core.UnitRect{
			X:      viewport.Width,
			Y:      viewport.Height,
			Width:  metrics.CellWidth,
			Height: s.hScrollBarHeight(),
		}, ' ', scheme.GetScrollbar())
	}
}

// HandleKeyPress handles keyboard input.
func (s *ScrollArea) HandleKeyPress(event core.KeyPressEvent) bool {
	// Pass to content first
	if s.content != nil && s.content.HandleKeyPress(event) {
		return true
	}

	switch event.Key {
	case "Up":
		s.SetScrollY(s.scrollY - s.vScrollBar.SingleStep())
		return true
	case "Down":
		s.SetScrollY(s.scrollY + s.vScrollBar.SingleStep())
		return true
	case "Left":
		s.SetScrollX(s.scrollX - s.hScrollBar.SingleStep())
		return true
	case "Right":
		s.SetScrollX(s.scrollX + s.hScrollBar.SingleStep())
		return true
	case "PageUp":
		s.SetScrollY(s.scrollY - s.vScrollBar.PageStep())
		return true
	case "PageDown":
		s.SetScrollY(s.scrollY + s.vScrollBar.PageStep())
		return true
	case "Home":
		s.SetScrollY(0)
		return true
	case "End":
		s.SetScrollY(s.vScrollBar.Maximum())
		return true
	}

	return false
}

// SetBounds sets the scroll area bounds and triggers layout.
func (s *ScrollArea) SetBounds(bounds core.UnitRect) {
	s.TrinketBase.SetBounds(bounds)
	// Always relayout when bounds are set
	// (font changes may require relayout even if size unchanged)
	s.Layout()
}

// HandleResize is called when the scroll area is resized.
func (s *ScrollArea) HandleResize(oldSize, newSize core.UnitSize) {
	// Update scrollbar ranges when viewport size changes
	s.updateScrollBars()
}

// HandleMousePress handles mouse clicks.
func (s *ScrollArea) HandleMousePress(event core.MousePressEvent) bool {
	viewport := s.viewportBounds()

	// Check if click is on vertical scrollbar
	if s.needsVScrollBar() && event.X >= viewport.Width {
		// Clear horizontal scrollbar drag state
		s.hScrollBar.dragging = false
		return s.vScrollBar.HandleMousePress(core.MousePressEvent{
			X:      event.X - viewport.Width,
			Y:      event.Y,
			Button: event.Button,
		})
	}

	// Check if click is on horizontal scrollbar
	if s.needsHScrollBar() && event.Y >= viewport.Height {
		// Clear vertical scrollbar drag state
		s.vScrollBar.dragging = false
		return s.hScrollBar.HandleMousePress(core.MousePressEvent{
			X:      event.X,
			Y:      event.Y - viewport.Height,
			Button: event.Button,
		})
	}

	// Click is on content area - clear both scrollbar drag states
	s.vScrollBar.dragging = false
	s.hScrollBar.dragging = false

	// Pass to content (copy the event so Modifiers - e.g. Shift for
	// shift-click selection - survive the offset translation).
	if s.content != nil {
		scrollOffsetX, scrollOffsetY := s.scrollOffsetUnits()
		le := event
		le.X = event.X + scrollOffsetX
		le.Y = event.Y + scrollOffsetY
		return s.content.HandleMousePress(le)
	}

	return false
}

// HandleMouseMove handles mouse move/drag events.
func (s *ScrollArea) HandleMouseMove(event core.MouseMoveEvent) bool {
	viewport := s.viewportBounds()

	// Forward to scrollbars if dragging
	if s.vScrollBar.dragging {
		return s.vScrollBar.HandleMouseMove(core.MouseMoveEvent{
			X: event.X - viewport.Width,
			Y: event.Y,
		})
	}

	if s.hScrollBar.dragging {
		return s.hScrollBar.HandleMouseMove(core.MouseMoveEvent{
			X: event.X,
			Y: event.Y - viewport.Height,
		})
	}

	// Not dragging: keep each bar's thumb-hover state in sync (the area
	// only forwards drags to the bars, so hover would never reach them).
	// Hover is a no-button affordance: while a button is held (a drag begun
	// elsewhere passing over), clear rather than highlight (off-point clears).
	if event.Buttons == 0 {
		s.vScrollBar.UpdateThumbHover(event.X-viewport.Width, event.Y)
		s.hScrollBar.UpdateThumbHover(event.X, event.Y-viewport.Height)
	} else {
		s.vScrollBar.UpdateThumbHover(-1, -1)
		s.hScrollBar.UpdateThumbHover(-1, -1)
	}

	// Forward to content trinket. Copy the event so Buttons and
	// Modifiers survive - a drag-select needs Buttons&LeftButton set, and
	// dropping it left text controls unable to extend a selection while
	// scrolled (the move handler bailed on the missing button).
	if s.content != nil {
		le := event
		inViewport := event.X >= 0 && event.Y >= 0 &&
			event.X < viewport.Width && event.Y < viewport.Height
		if inViewport {
			scrollOffsetX, scrollOffsetY := s.scrollOffsetUnits()
			le.X = event.X + scrollOffsetX
			le.Y = event.Y + scrollOffsetY
		} else {
			// Over a scrollbar lane (or outside the viewport): the content
			// under it must not hover, so send an out-of-bounds move to
			// clear its hover instead of a valid content coordinate.
			le.X, le.Y = -1, -1
		}
		return s.content.HandleMouseMove(le)
	}

	return false
}

// HandleMouseWheel scrolls the viewport. The content under the
// pointer gets first claim (the topmost scrollable wins); the area
// scrolls itself only when it has a scrollbar on the wheel's axis.
func (s *ScrollArea) HandleMouseWheel(event core.MouseWheelEvent) bool {
	viewport := s.viewportBounds()
	if s.content != nil && event.X >= 0 && event.X < viewport.Width &&
		event.Y >= 0 && event.Y < viewport.Height {
		if handler, ok := s.content.(interface {
			HandleMouseWheel(core.MouseWheelEvent) bool
		}); ok {
			offX, offY := s.scrollOffsetUnits()
			contentEvent := event
			contentEvent.X += offX
			contentEvent.Y += offY
			if handler.HandleMouseWheel(contentEvent) {
				return true
			}
		}
	}

	return s.scrollSelfWheel(event)
}

// scrollSelfWheel scrolls the area itself and is ALSO the latched
// gesture handler: it must never re-route to content, or a child
// scrolling under the pointer mid-gesture would steal the scroll
// from the container the gesture started on.
func (s *ScrollArea) scrollSelfWheel(event core.MouseWheelEvent) bool {
	// Horizontal when Shift is held (classic wheel) or when the event
	// itself is predominantly horizontal (trackpad two-finger pan).
	absf := func(f float64) float64 {
		if f < 0 {
			return -f
		}
		return f
	}
	nativeHoriz := absf(event.PreciseX) > absf(event.PreciseY) ||
		(event.PreciseX == 0 && event.PreciseY == 0 &&
			event.DeltaX != 0 && event.DeltaY == 0)
	horizontal := event.Modifiers&core.ShiftModifier != 0 || nativeHoriz
	if horizontal {
		if !s.needsHScrollBar() {
			return false
		}
	} else if !s.needsVScrollBar() {
		return false
	}

	metrics := s.EffectiveCellMetrics()
	if s.smoothScroll() {
		// Unit-granular: precise (trackpad) deltas map to fractions
		// of the 3-cells-per-notch step, with sub-unit remainders
		// carried between events so slow pans stay smooth.
		if horizontal {
			delta := float64(event.DeltaY) // Shift+vertical wheel
			if nativeHoriz {
				delta = float64(event.DeltaX)
				if event.PreciseX != 0 {
					delta = event.PreciseX
				}
			} else if event.PreciseY != 0 {
				delta = event.PreciseY
			}
			s.wheelCarryX += delta * 3 * float64(metrics.CellWidth)
			step := int(s.wheelCarryX)
			s.wheelCarryX -= float64(step)
			s.SetScrollX(s.scrollX + step)
		} else {
			delta := float64(event.DeltaY)
			if event.PreciseY != 0 {
				delta = event.PreciseY
			}
			s.wheelCarryY += delta * 3 * float64(metrics.CellHeight)
			step := int(s.wheelCarryY)
			s.wheelCarryY -= float64(step)
			s.SetScrollY(s.scrollY + step)
		}
	} else if horizontal {
		dx := event.DeltaY // Shift+vertical wheel
		if nativeHoriz {
			dx = event.DeltaX
		}
		s.SetScrollX(s.scrollX + dx*3)
	} else {
		s.SetScrollY(s.scrollY + event.DeltaY*3)
	}
	core.ClaimWheelGesture(event, s.scrollSelfWheel)
	return true
}

// HandleMouseRelease handles mouse release events.
func (s *ScrollArea) HandleMouseRelease(event core.MouseReleaseEvent) bool {
	viewport := s.viewportBounds()

	// Forward to scrollbars
	if s.vScrollBar.dragging {
		return s.vScrollBar.HandleMouseRelease(core.MouseReleaseEvent{
			X:      event.X - viewport.Width,
			Y:      event.Y,
			Button: event.Button,
		})
	}

	if s.hScrollBar.dragging {
		return s.hScrollBar.HandleMouseRelease(core.MouseReleaseEvent{
			X:      event.X,
			Y:      event.Y - viewport.Height,
			Button: event.Button,
		})
	}

	// Forward to content trinket (copy the event to preserve Modifiers).
	if s.content != nil {
		scrollOffsetX, scrollOffsetY := s.scrollOffsetUnits()
		le := event
		le.X = event.X + scrollOffsetX
		le.Y = event.Y + scrollOffsetY
		return s.content.HandleMouseRelease(le)
	}

	return false
}

// HandleFocusIn is called when focus is gained.
func (s *ScrollArea) HandleFocusIn() {
	s.Update()
}

// HandleFocusOut is called when focus is lost.
func (s *ScrollArea) HandleFocusOut() {
	// Clear any active scrollbar drag states when focus is lost
	s.vScrollBar.dragging = false
	s.hScrollBar.dragging = false
	s.Update()
}

// AccessibleInfo returns accessibility information.
// AccessibleInfo announces the scroll area with the thumb position of
// each visible scrollbar: "scroll area, horizontal N percent, vertical M
// percent", 0 percent at the left/top and 100 percent at the right/bottom
// (rounded to the nearest integer). A direction is included only when its
// scrollbar is actually visible. The role is folded into the name so the
// announcement doesn't end in a trailing "group".
func (s *ScrollArea) AccessibleInfo() core.AccessibleInfo {
	s.updateScrollBars()
	needsV, needsH := s.calculateScrollBarNeeds()

	parts := []string{"scroll area"}
	if needsH {
		parts = append(parts, fmt.Sprintf("horizontal %d percent", scrollBarPercent(s.hScrollBar)))
	}
	if needsV {
		parts = append(parts, fmt.Sprintf("vertical %d percent", scrollBarPercent(s.vScrollBar)))
	}
	return core.AccessibleInfo{Name: strings.Join(parts, ", "), Role: core.RoleNone}
}
