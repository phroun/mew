package viewport

import (
	"testing"

	"github.com/phroun/mew/internal/buffer"
)

// A viewport's buffer binding detaches wholesale and re-attaches later with its
// exact caret, scroll, and browse state intact — including edits made to the
// buffer while the binding was stacked, since the detached cursors stay live
// on the buffer and keep sliding. This is the primitive a buffer-swap history
// stack (link following / nav_back) builds on.
func TestDetachAttachBindingSurvivesEdits(t *testing.T) {
	m := NewManager()
	buf1 := buffer.NewFromString("alpha\nbravo\ncharlie\ndelta\n")
	id := m.CreateViewport(ViewportOptions{
		Type: DocViewport, Dock: DockNone, Buffer: buf1, Visible: true, SetFocus: true,
	})
	w := m.GetViewport(id)

	w.SetCursorPos(Position{Line: 2, Rune: 3})
	w.SetViewTop(1)
	w.ViewState.ViewOffsetX = 5
	w.BrowseActive = true

	saved := w.detachBinding()
	if w.Buffer != nil || w.Caret != nil || w.BrowseActive || w.ViewState.ViewOffsetX != 0 {
		t.Fatal("detach must leave the viewport unbound")
	}

	// Bind a second buffer and use it independently.
	buf2 := buffer.NewFromString("second\n")
	w.bindBuffer(buf2)
	w.SetCursorPos(Position{Line: 0, Rune: 3})
	if got := w.CursorPos(); got.Line != 0 || got.Rune != 3 {
		t.Fatalf("second binding caret = %+v", got)
	}

	// Edit the FIRST buffer while its binding is stacked: the detached
	// cursors keep sliding with the edit.
	buf1.InsertLine(0, "zero")

	// Swap back: drop the current binding, restore the saved one.
	cur := w.detachBinding()
	cur.release()
	w.attachBinding(saved)

	if w.Buffer != buf1 {
		t.Fatal("attach must restore the original buffer")
	}
	if got := w.CursorPos(); got.Line != 3 || got.Rune != 3 {
		t.Fatalf("restored caret should have slid with the insert (2 -> 3); got %+v", got)
	}
	if w.ViewState.ViewOffsetY != 2 {
		t.Fatalf("restored view top should have slid 1 -> 2; got %d", w.ViewState.ViewOffsetY)
	}
	if w.ViewState.ViewOffsetX != 5 {
		t.Fatalf("restored horizontal scroll = %d, want 5", w.ViewState.ViewOffsetX)
	}
	if !w.BrowseActive {
		t.Fatal("restored binding should re-arm browse mode")
	}
}

// nothingOutside is the SwapBuffer/ClearNavHistory predicate for pure
// viewport-level tests: no buffer is referenced anywhere beyond this viewport.
func nothingOutside(*buffer.Buffer) bool { return false }

// SwapBuffer + NavHistoryPrior/NavHistoryNext implement browser-style history:
// swapping pushes the departed binding onto the back stack, prior/next shuffle
// bindings between the stacks and the viewport, and a new departure clears the
// forward trail — burying a forward binding in the graveyard when it holds
// its buffer's last reference.
func TestSwapBufferHistory(t *testing.T) {
	m := NewManager()
	bufA := buffer.NewFromString("aaa\naaa\n")
	id := m.CreateViewport(ViewportOptions{
		Type: DocViewport, Dock: DockNone, Buffer: bufA, Visible: true, SetFocus: true,
	})
	w := m.GetViewport(id)
	w.SetCursorPos(Position{Line: 1, Rune: 2})

	bufB := buffer.NewFromString("bbb\n")
	w.SwapBuffer(bufB, nothingOutside)
	if w.Buffer != bufB {
		t.Fatal("swap must bind the new buffer")
	}
	if got := w.CursorPos(); got.Line != 0 || got.Rune != 0 {
		t.Fatalf("fresh binding should start at the buffer top; got %+v", got)
	}
	if p, n := w.NavHistoryDepths(); p != 1 || n != 0 {
		t.Fatalf("depths after swap = (%d,%d), want (1,0)", p, n)
	}
	if sb := w.StackedBuffers(); len(sb) != 1 || sb[0] != bufA {
		t.Fatalf("stacked buffers = %v", sb)
	}

	if w.NavHistoryNext() {
		t.Fatal("next with no forward history must fail")
	}
	if !w.NavHistoryPrior() {
		t.Fatal("prior should succeed")
	}
	if w.Buffer != bufA {
		t.Fatal("prior must restore the original buffer")
	}
	if got := w.CursorPos(); got.Line != 1 || got.Rune != 2 {
		t.Fatalf("prior must restore the caret; got %+v", got)
	}
	if !w.NavHistoryNext() {
		t.Fatal("next should re-advance")
	}
	if w.Buffer != bufB {
		t.Fatal("next must re-bind the destination")
	}

	// Departing anew from a mid-history position clears the forward trail.
	if !w.NavHistoryPrior() {
		t.Fatal("prior (again) should succeed")
	}
	bufC := buffer.NewFromString("ccc\n")
	w.SwapBuffer(bufC, nothingOutside)
	if w.NavHistoryNext() {
		t.Fatal("a new departure must clear the forward history")
	}
	if p, _ := w.NavHistoryDepths(); p != 1 {
		t.Fatalf("back depth after re-departure = %d, want 1 (bufA)", p)
	}
	// The invalidated forward binding (bufB) held its buffer's last
	// reference: it is buried, not released.
	gb := w.GraveyardBuffers()
	if len(gb) != 1 || gb[0] != bufB {
		t.Fatalf("graveyard = %v, want [bufB]", gb)
	}
	if sb := w.StackedBuffers(); len(sb) != 2 {
		t.Fatalf("stacked (back+graveyard) = %v, want bufA and bufB", sb)
	}
}

// RemoveViewport releases the active binding's cursors but keeps the Buffer
// reference — the close path inspects it after removal to decide whether the
// buffer is still shown in another viewport.
func TestRemoveViewportKeepsBufferReference(t *testing.T) {
	m := NewManager()
	buf := buffer.NewFromString("hello\n")
	id := m.CreateViewport(ViewportOptions{
		Type: DocViewport, Dock: DockNone, Buffer: buf, Visible: true, SetFocus: true,
	})
	w := m.GetViewport(id)
	if !m.RemoveViewport(id) {
		t.Fatal("RemoveViewport failed")
	}
	if w.Buffer != buf {
		t.Fatal("removed viewport must keep its buffer reference")
	}
	if w.Caret != nil || w.viewportAnchor != nil || w.lastEditPoint != nil {
		t.Fatal("removed viewport must release its cursors")
	}
}

// Swapping to the buffer already shown is a no-op: no history slot is added and
// the caret is left exactly where it was.
func TestSwapBufferToCurrentIsNoop(t *testing.T) {
	m := NewManager()
	bufA := buffer.NewFromString("aaa\naaa\n")
	id := m.CreateViewport(ViewportOptions{
		Type: DocViewport, Dock: DockNone, Buffer: bufA, Visible: true, SetFocus: true,
	})
	w := m.GetViewport(id)
	w.SetCursorPos(Position{Line: 1, Rune: 2})

	w.SwapBuffer(bufA, nothingOutside)
	if w.Buffer != bufA {
		t.Fatal("swap to the current buffer must keep it bound")
	}
	if p, n := w.NavHistoryDepths(); p != 0 || n != 0 {
		t.Fatalf("swap to the current buffer must not grow history; depths = (%d,%d)", p, n)
	}
	if got := w.CursorPos(); got.Line != 1 || got.Rune != 2 {
		t.Fatalf("no-op swap must keep the caret; got %+v", got)
	}
}

// A transient buffer between two visits to the same document collapses: opening
// a transient surface over A stacks A, and swapping back to A reuses that back
// binding (restoring A's caret) instead of stacking A back-to-back with itself.
func TestSwapBufferCollapsesAcrossTransient(t *testing.T) {
	m := NewManager()
	bufA := buffer.NewFromString("aaa\naaa\n")
	id := m.CreateViewport(ViewportOptions{
		Type: DocViewport, Dock: DockNone, Buffer: bufA, Visible: true, SetFocus: true,
	})
	w := m.GetViewport(id)
	w.SetCursorPos(Position{Line: 1, Rune: 2})

	surface := buffer.NewFromString("== list ==\n")
	surface.SetTransient(true)
	w.SwapBuffer(surface, nothingOutside) // navBack=[A@(1,2)], active=surface
	if p, _ := w.NavHistoryDepths(); p != 1 {
		t.Fatalf("opening the surface should stack A; back depth = %d", p)
	}

	w.SwapBuffer(bufA, nothingOutside) // transient released; A is the back top -> reuse
	if w.Buffer != bufA {
		t.Fatal("should be back on A")
	}
	if p, n := w.NavHistoryDepths(); p != 0 || n != 0 {
		t.Fatalf("A must not sit back-to-back; depths = (%d,%d), want (0,0)", p, n)
	}
	if got := w.CursorPos(); got.Line != 1 || got.Rune != 2 {
		t.Fatalf("the reused binding must restore A's caret; got %+v", got)
	}
}
