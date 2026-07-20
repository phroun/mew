package window

import (
	"testing"

	"github.com/phroun/mew/internal/buffer"
)

// A window's buffer binding detaches wholesale and re-attaches later with its
// exact caret, scroll, and browse state intact — including edits made to the
// buffer while the binding was stacked, since the detached cursors stay live
// on the buffer and keep sliding. This is the primitive a buffer-swap history
// stack (link following / nav_back) builds on.
func TestDetachAttachBindingSurvivesEdits(t *testing.T) {
	m := NewManager()
	buf1 := buffer.NewFromString("alpha\nbravo\ncharlie\ndelta\n")
	id := m.CreateWindow(WindowOptions{
		Type: MainBuffer, Dock: DockNone, Buffer: buf1, Visible: true, SetFocus: true,
	})
	w := m.GetWindow(id)

	w.SetCursorPos(Position{Line: 2, Rune: 3})
	w.SetViewTop(1)
	w.ViewState.ViewOffsetX = 5
	w.BrowseActive = true

	saved := w.detachBinding()
	if w.Buffer != nil || w.Caret != nil || w.BrowseActive || w.ViewState.ViewOffsetX != 0 {
		t.Fatal("detach must leave the window unbound")
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

// SwapBuffer + NavHistoryPrior/NavHistoryNext implement browser-style history:
// swapping pushes the departed binding onto the back stack, prior/next shuffle
// bindings between the stacks and the window, and a new departure clears the
// forward trail.
func TestSwapBufferHistory(t *testing.T) {
	m := NewManager()
	bufA := buffer.NewFromString("aaa\naaa\n")
	id := m.CreateWindow(WindowOptions{
		Type: MainBuffer, Dock: DockNone, Buffer: bufA, Visible: true, SetFocus: true,
	})
	w := m.GetWindow(id)
	w.SetCursorPos(Position{Line: 1, Rune: 2})

	bufB := buffer.NewFromString("bbb\n")
	w.SwapBuffer(bufB)
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
	w.SwapBuffer(bufC)
	if w.NavHistoryNext() {
		t.Fatal("a new departure must clear the forward history")
	}
	if p, _ := w.NavHistoryDepths(); p != 1 {
		t.Fatalf("back depth after re-departure = %d, want 1 (bufA)", p)
	}
}

// RemoveWindow releases the active binding's cursors but keeps the Buffer
// reference — the close path inspects it after removal to decide whether the
// buffer is still shown in another window.
func TestRemoveWindowKeepsBufferReference(t *testing.T) {
	m := NewManager()
	buf := buffer.NewFromString("hello\n")
	id := m.CreateWindow(WindowOptions{
		Type: MainBuffer, Dock: DockNone, Buffer: buf, Visible: true, SetFocus: true,
	})
	w := m.GetWindow(id)
	if !m.RemoveWindow(id) {
		t.Fatal("RemoveWindow failed")
	}
	if w.Buffer != buf {
		t.Fatal("removed window must keep its buffer reference")
	}
	if w.Caret != nil || w.viewportAnchor != nil || w.lastEditPoint != nil {
		t.Fatal("removed window must release its cursors")
	}
}
