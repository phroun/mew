package core

// SubtreeRepaintTracker is implemented by a container that needs to know
// when anything beneath it asked to be repainted. Update() tells every
// tracker above the trinket that changed, so a container can tell "one
// of my descendants moved" from "nothing I own has changed since I last
// painted".
//
// The GPU compositor is the reason this exists: it caches a texture per
// child window, and without a per-window signal it repainted, converted
// and re-uploaded every window's full pixels every frame — including the
// ones nobody had touched.
type SubtreeRepaintTracker interface {
	// NoteSubtreeRepaint records that something in this trinket's
	// subtree (possibly the trinket itself) requested a repaint. Called
	// from Update(), so it must be cheap and safe from any goroutine.
	NoteSubtreeRepaint()

	// SubtreeRepaintRevision returns a counter that changes whenever
	// NoteSubtreeRepaint is called. Only equality across two reads is
	// meaningful — never the value itself.
	SubtreeRepaintRevision() uint64
}

// maxRepaintWalkDepth bounds the ancestor walk. A trinket tree is a few
// dozen deep at worst; the cap is a backstop against a parent cycle
// turning one Update() into a hang.
const maxRepaintWalkDepth = 256

// notifyAncestorsOfRepaint tells the containers above this trinket that
// its subtree changed. Every mutation that sets needsRepaint owes this
// call: the flag alone records only local intent, and a container
// caching rendered pixels (the GPU compositor does, per window) has no
// other way to learn it must redraw.
//
// MUST be called with w.mu released — the walk starts by asking this
// very trinket for its parent, which takes the same lock.
func (w *TrinketBase) notifyAncestorsOfRepaint() {
	noteSubtreeRepaint(w.Self())
}

// notifyAncestorsOfMove reports a change that alters only WHERE this
// trinket sits, not what it draws. A trinket paints in its own local
// coordinates, so its rendered pixels are identical at the new position
// — a container caching THEM keeps them. Containers above still hear
// about it, because their pixels include this trinket at its new place.
//
// This is what makes dragging a window across the desktop free: the GPU
// compositor caches a texture per window and positions it from a uniform
// buffer it rewrites every frame anyway.
//
// Same locking rule as notifyAncestorsOfRepaint: call with w.mu released.
func (w *TrinketBase) notifyAncestorsOfMove() {
	self := w.Self()
	if self == nil {
		return
	}
	noteSubtreeRepaint(self.Parent())
}

// noteSubtreeRepaint tells every tracker from t up to the root that
// something beneath it changed.
//
// EVERY tracker, not just the nearest one. A window nested inside
// another (an MDI child in a pane) paints into its ancestor's surface,
// so stopping at the first tracker would leave the ancestor thinking it
// was clean and the change would never reach the screen.
func noteSubtreeRepaint(t Trinket) {
	for depth := 0; t != nil && depth < maxRepaintWalkDepth; depth++ {
		if tracker, ok := t.(SubtreeRepaintTracker); ok {
			tracker.NoteSubtreeRepaint()
		}
		t = t.Parent()
	}
}
