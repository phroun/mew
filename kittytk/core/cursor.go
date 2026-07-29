package core

// CursorShape identifies a system mouse cursor. The platform maps these
// to its native cursors; targets without cursor support keep the arrow.
type CursorShape int

const (
	// CursorDefault is the ordinary arrow pointer.
	CursorDefault CursorShape = iota
	// CursorText is the text I-beam (hovering an editable text control).
	CursorText
	// CursorResizeH is the horizontal (west-east) resize cursor.
	CursorResizeH
	// CursorResizeV is the vertical (north-south) resize cursor.
	CursorResizeV
	// CursorResizeNWSE is the top-left/bottom-right diagonal resize cursor.
	CursorResizeNWSE
	// CursorResizeNESW is the top-right/bottom-left diagonal resize cursor.
	CursorResizeNESW
)

// CursorProvider is an optional trinket capability: a trinket that wants a
// particular mouse cursor while the pointer is over it (a text field
// returns CursorText, for example). The desktop resolves it on mouse
// move, after resize-edge cursors take precedence.
type CursorProvider interface {
	CursorShape() CursorShape
}

// CursorShaper is an optional container capability: a container that
// routes mouse events through a coordinate transform the generic
// ChildAt+Bounds cursor descent can't reproduce (a nested window's
// title/content split and interior denomination, an MDI pane's window
// placement) answers the cursor query itself, descending exactly as it
// routes events. localX/localY are in the container's own coordinate
// space, as its HandleMouseMove receives them.
type CursorShaper interface {
	CursorShapeAt(localX, localY Unit) CursorShape
}

// CursorShapesSubtree marks a CursorShaper that answers for its WHOLE
// subtree and never re-enters the cursor descent — so it is safe to consult
// even when it is the descent's entry trinket.
//
// The descent skips an entry that is a plain Container, because a window
// delegating into its own content would otherwise recurse forever. That guard
// is about re-entrancy, not about having an opinion: a container that resolves
// the cursor from its own state (an embedded editor that owns the affordance
// across its entire surface) must still be asked, or it silently loses every
// cursor it wanted to set the moment it becomes a window's content.
type CursorShapesSubtree interface {
	CursorShaper
	// CursorShapesSubtree is a marker; its body does nothing.
	CursorShapesSubtree()
}
