// Package core provides fundamental types for KittyTK.
package core

// The following types use int for cell-based coordinates.
// These are used internally by backends for actual rendering.
// Trinkets should use the Unit-based types from units.go for
// resolution-independent layout.

// Point represents a 2D coordinate in cells/pixels.
type Point struct {
	X, Y int
}

// Size represents dimensions.
type Size struct {
	Width, Height int
}

// Rect represents a rectangle with position and size.
type Rect struct {
	X, Y          int
	Width, Height int
}

// NewRect creates a new rectangle.
func NewRect(x, y, width, height int) Rect {
	return Rect{X: x, Y: y, Width: width, Height: height}
}

// Contains checks if a point is inside the rectangle.
func (r Rect) Contains(p Point) bool {
	return p.X >= r.X && p.X < r.X+r.Width && p.Y >= r.Y && p.Y < r.Y+r.Height
}

// Intersects checks if two rectangles overlap.
func (r Rect) Intersects(other Rect) bool {
	return r.X < other.X+other.Width && r.X+r.Width > other.X &&
		r.Y < other.Y+other.Height && r.Y+r.Height > other.Y
}

// Intersection returns the overlapping area of two rectangles.
func (r Rect) Intersection(other Rect) Rect {
	x1 := max(r.X, other.X)
	y1 := max(r.Y, other.Y)
	x2 := min(r.X+r.Width, other.X+other.Width)
	y2 := min(r.Y+r.Height, other.Y+other.Height)
	if x2 <= x1 || y2 <= y1 {
		return Rect{}
	}
	return Rect{X: x1, Y: y1, Width: x2 - x1, Height: y2 - y1}
}

// IsEmpty returns true if the rectangle has no area.
func (r Rect) IsEmpty() bool {
	return r.Width <= 0 || r.Height <= 0
}

// TopLeft returns the top-left corner.
func (r Rect) TopLeft() Point {
	return Point{X: r.X, Y: r.Y}
}

// BottomRight returns the bottom-right corner (exclusive).
func (r Rect) BottomRight() Point {
	return Point{X: r.X + r.Width, Y: r.Y + r.Height}
}

// Size returns the dimensions of the rectangle.
func (r Rect) Size() Size {
	return Size{Width: r.Width, Height: r.Height}
}

// Margins represents spacing around an element.
type Margins struct {
	Top, Right, Bottom, Left int
}

// NewMargins creates uniform margins.
func NewMargins(all int) Margins {
	return Margins{Top: all, Right: all, Bottom: all, Left: all}
}

// NewMarginsVH creates margins with vertical and horizontal values.
func NewMarginsVH(vertical, horizontal int) Margins {
	return Margins{Top: vertical, Right: horizontal, Bottom: vertical, Left: horizontal}
}

// NewMarginsTRBL creates margins with individual values.
func NewMarginsTRBL(top, right, bottom, left int) Margins {
	return Margins{Top: top, Right: right, Bottom: bottom, Left: left}
}

// Horizontal returns the total horizontal margin.
func (m Margins) Horizontal() int {
	return m.Left + m.Right
}

// Vertical returns the total vertical margin.
func (m Margins) Vertical() int {
	return m.Top + m.Bottom
}

// Alignment specifies how content is aligned within its container.
type Alignment int

const (
	AlignFill Alignment = iota // Default: fill available space
	AlignLeft
	AlignCenter
	AlignRight
	AlignTop
	AlignMiddle
	AlignBottom
)

// Orientation specifies layout direction.
type Orientation int

const (
	Horizontal Orientation = iota
	Vertical
)

// SizePolicy controls how a trinket resizes.
type SizePolicy int

const (
	SizeFixed     SizePolicy = iota // Fixed size, doesn't grow or shrink
	SizeMinimum                     // Can grow but not shrink below minimum
	SizeMaximum                     // Can shrink but not grow above maximum
	SizePreferred                   // Can grow or shrink, prefers preferred size
	SizeExpanding                   // Greedily takes all available space
	SizeIgnored                     // Size hint is ignored
)

// SizePolicyPair specifies policies for both dimensions.
type SizePolicyPair struct {
	Horizontal SizePolicy
	Vertical   SizePolicy
}

// NewSizePolicy creates a size policy pair.
func NewSizePolicy(h, v SizePolicy) SizePolicyPair {
	return SizePolicyPair{Horizontal: h, Vertical: v}
}

// FocusPolicy controls how a trinket receives focus.
type FocusPolicy int

const (
	NoFocus      FocusPolicy = iota // Cannot receive focus
	TabFocus                        // Can receive focus via Tab
	ClickFocus                      // Can receive focus via mouse click
	StrongFocus                     // Can receive focus via Tab or click
	WheelFocus                      // Like StrongFocus, plus mouse wheel
)

// WindowFlags control window behavior and appearance.
type WindowFlags int

const (
	WindowDefault     WindowFlags = 0
	WindowFrameless   WindowFlags = 1 << iota // No window frame
	WindowNoTitle                             // No title bar
	WindowNoResize                            // Cannot be resized
	WindowNoMove                              // Cannot be moved
	WindowNoClose                             // No close button
	WindowModal                               // Blocks input to other windows
	WindowStaysOnTop                          // Always on top
	WindowMaximized                           // Start maximized
	WindowMinimized                           // Start minimized
)

// WindowState represents the current state of a window.
type WindowState int

const (
	WindowNormal WindowState = iota
	WindowStateMaximized
	WindowStateMinimized
)

// MouseButton represents a mouse button.
type MouseButton int

const (
	NoButton MouseButton = iota
	LeftButton
	MiddleButton
	RightButton
	ScrollUp
	ScrollDown
)

// KeyModifiers represents active modifier keys.
type KeyModifiers int

const (
	NoModifier    KeyModifiers = 0
	ShiftModifier KeyModifiers = 1 << iota
	ControlModifier
	AltModifier
	MetaModifier
)

// ParseKeyModifiers parses modifier prefixes from a key string.
// Returns the modifiers and the remaining key name.
func ParseKeyModifiers(key string) (KeyModifiers, string) {
	var mods KeyModifiers
	remaining := key

	for {
		switch {
		case len(remaining) > 2 && remaining[:2] == "M-":
			mods |= AltModifier
			remaining = remaining[2:]
		case len(remaining) > 2 && remaining[:2] == "C-":
			mods |= ControlModifier
			remaining = remaining[2:]
		case len(remaining) > 2 && remaining[:2] == "S-":
			mods |= ShiftModifier
			remaining = remaining[2:]
		case len(remaining) > 2 && remaining[:2] == "s-":
			mods |= MetaModifier
			remaining = remaining[2:]
		default:
			// Check for control character notation
			if len(remaining) == 2 && remaining[0] == '^' {
				mods |= ControlModifier
				remaining = string(remaining[1])
			}
			return mods, remaining
		}
	}
}
