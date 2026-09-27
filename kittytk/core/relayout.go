package core

// Telling a container that what it arranged has changed size.
//
// A trinket that measures its own content answers SizeHint with what it holds
// at the moment it is asked, and its parent arranged its children against the
// answer it gave THEN. Rewrite a caption afterwards and the arrangement still
// describes the words that are gone: the widths belong to the old text, and
// the new text is drawn over whatever stands beside it.
//
// So a setter that changes what a trinket measures owes InvalidateLayout, the
// way one that changes what it draws owes Update. The two are separate because
// they ask for different work: Update says these pixels are wrong, this says
// this shape is wrong, and only the second can move anything else on the
// screen.

// LayoutHost is a container that arranges what it holds. Every one of them on
// the way up is a candidate for the arrangement that has to be done again.
type LayoutHost interface {
	Layout()
}

// LayoutRoot is a container whose own size does not follow from what it holds
// -- a window, the desktop. A child growing inside one changes the arrangement
// within it and nothing outside it, so the walk stops there rather than
// climbing out and re-arranging the whole screen because a label got longer.
type LayoutRoot interface {
	IsLayoutRoot() bool
}

// maxLayoutWalkDepth bounds the ancestor walk, as the repaint walk is bounded:
// a backstop against a parent cycle turning one setter into a hang.
const maxLayoutWalkDepth = 256

// InvalidateLayout arranges again from the outermost container that has any
// say in where this trinket sits.
//
// The OUTERMOST, because a child that changes size changes the size its parent
// asks for, and that its parent's parent asks for, as far up as anything is
// arranged by what it holds. Arranging from there does every level beneath in
// one pass, top down, which is the same pass a resize does.
//
// It arranges now rather than marking the arrangement stale for later. What a
// trinket's bounds say is then true the moment the setter returns -- to a
// click looking for what it landed on, and to anything else that asks -- which
// is worth more here than coalescing a burst of setters into one pass.
//
// MUST be called with the trinket's own lock released: the walk asks this
// trinket for its parent.
func (w *TrinketBase) InvalidateLayout() {
	self := w.Self()
	if self == nil {
		return
	}
	var outermost LayoutHost
	t := Trinket(self)
	for depth := 0; t != nil && depth < maxLayoutWalkDepth; depth++ {
		if host, ok := t.(LayoutHost); ok {
			outermost = host
		}
		if root, ok := t.(LayoutRoot); ok && root.IsLayoutRoot() {
			break
		}
		t = t.Parent()
	}
	if outermost != nil {
		outermost.Layout()
	}
}
