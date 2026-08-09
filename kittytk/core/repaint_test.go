package core

import (
	"testing"
	"time"

	"github.com/phroun/kittytk/style"
)

// trackerBox is a container that counts subtree repaint notifications.
// It is a Container only so far as SetParent needs — the walk asks for
// parents, never for children.
type trackerBox struct {
	TrinketBase
	children []Trinket
	notes    uint64
}

func (b *trackerBox) Children() []Trinket            { return b.children }
func (b *trackerBox) AddChild(c Trinket)             { b.children = append(b.children, c); c.SetParent(b) }
func (b *trackerBox) RemoveChild(Trinket)            {}
func (b *trackerBox) ChildAt(UnitPoint) Trinket      { return nil }
func (b *trackerBox) Layout()                        {}
func (b *trackerBox) LayoutManager() LayoutManager   { return nil }
func (b *trackerBox) SetLayoutManager(LayoutManager) {}

func newTrackerBox() *trackerBox {
	b := &trackerBox{}
	b.TrinketBase = *NewTrinketBase()
	b.Init(b)
	return b
}

func (b *trackerBox) NoteSubtreeRepaint()            { b.notes++ }
func (b *trackerBox) SubtreeRepaintRevision() uint64 { return b.notes }

// plainLeaf is a trinket that tracks nothing, so a walk must pass
// straight through it.
type plainLeaf struct{ TrinketBase }

func newPlainLeaf() *plainLeaf {
	l := &plainLeaf{}
	l.TrinketBase = *NewTrinketBase()
	l.Init(l)
	return l
}

// nest wires child into parent, the way a container would.
func nest(parent *trackerBox, child Trinket) {
	parent.AddChild(child)
}

// Update() on a deep trinket must reach EVERY tracker above it, not just
// the nearest. A window nested inside another paints into its ancestor's
// surface, so an ancestor that thought itself clean would never carry
// the change to the screen.
func TestUpdateNotifiesEveryAncestorTracker(t *testing.T) {
	outer := newTrackerBox()
	inner := newTrackerBox()
	leaf := newPlainLeaf()

	nest(outer, inner)
	nest(inner, leaf)

	leaf.Update()

	if outer.notes != 1 {
		t.Errorf("outer tracker got %d notifications, want 1", outer.notes)
	}
	if inner.notes != 1 {
		t.Errorf("inner tracker got %d notifications, want 1", inner.notes)
	}
}

// A tracker is notified when it is itself the trinket that changed.
func TestUpdateNotifiesSelf(t *testing.T) {
	box := newTrackerBox()
	box.Update()
	if box.notes != 1 {
		t.Errorf("tracker got %d notifications for its own Update, want 1", box.notes)
	}
}

// The revision has to move for the mutations that change what a trinket
// paints, not only for an explicit Update(). Each of these sets
// needsRepaint internally, and before the ancestor notification existed
// each one was a way for a cached container to miss a change entirely.
func TestPaintAffectingSettersNotifyAncestors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*plainLeaf)
	}{
		{"SetBounds", func(l *plainLeaf) { l.SetBounds(UnitRect{Width: 10, Height: 10}) }},
		{"SetPos", func(l *plainLeaf) { l.SetPos(UnitPoint{X: 5, Y: 5}) }},
		{"SetSize", func(l *plainLeaf) { l.SetSize(UnitSize{Width: 20, Height: 20}) }},
		{"SetVisible", func(l *plainLeaf) { l.SetVisible(false) }},
		{"SetEnabled", func(l *plainLeaf) { l.SetEnabled(false) }},
		{"SetMargins", func(l *plainLeaf) { l.SetMargins(UnitMargins{Left: 2}) }},
		{"SetBackgroundColor", func(l *plainLeaf) { c := style.RGB(1, 2, 3); l.SetBackgroundColor(&c) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			box := newTrackerBox()
			leaf := newPlainLeaf()
			nest(box, leaf)

			before := box.SubtreeRepaintRevision()
			tc.mutate(leaf)
			if box.SubtreeRepaintRevision() == before {
				t.Errorf("%s did not move the ancestor's repaint revision; "+
					"a container caching rendered pixels would never learn of it", tc.name)
			}
		})
	}
}

// The walk must terminate even if the tree is cyclic — a hang inside
// Update() would freeze the whole UI.
func TestNoteSubtreeRepaintStopsOnCycle(t *testing.T) {
	a := newTrackerBox()
	b := newTrackerBox()
	nest(a, b)
	nest(b, a) // cycle

	done := make(chan struct{})
	go func() {
		noteSubtreeRepaint(a)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("noteSubtreeRepaint did not terminate on a parent cycle")
	}
}

// A drawn caret is also the insertion point — a terminal's cursor is
// where typing goes — so RequestTextCaret answers both questions and an
// input method can anchor on it.
func TestRequestTextCaretIsAlsoAnInputArea(t *testing.T) {
	p := NewPainter(&caretTestBackend{})
	p.ResetTextCaretRequest()
	p.RequestTextCaret(10, 20, 5)

	got := p.TextCaretRequest()
	if !got.Visible {
		t.Error("RequestTextCaret did not ask for a drawn caret")
	}
	if !got.InputArea {
		t.Error("RequestTextCaret did not mark an insertion point; an input method " +
			"would place its candidate window in a corner")
	}
	if !got.Requested() {
		t.Error("Requested() = false for a caret request")
	}
}

// A trinket that paints its OWN caret reports only the insertion point.
// Asking for the platform caret too would paint a second one — on a cell
// surface the terminal cursor plus the trinket's reverse-video block.
func TestRequestTextInputAreaDoesNotDrawACaret(t *testing.T) {
	p := NewPainter(&caretTestBackend{})
	p.ResetTextCaretRequest()
	p.RequestTextInputArea(10, 20)

	got := p.TextCaretRequest()
	if got.Visible {
		t.Error("RequestTextInputArea asked for a drawn caret; the trinket paints its own")
	}
	if !got.InputArea {
		t.Error("RequestTextInputArea did not mark an insertion point")
	}
	if !got.Requested() {
		t.Error("Requested() = false for an input-area request")
	}
}

// Nothing asked: neither a caret to draw nor a place to anchor.
func TestEmptyCaretRequestsNothing(t *testing.T) {
	if (TextCaret{}).Requested() {
		t.Error("the zero TextCaret reports a request")
	}
}

// caretTestBackend is the smallest RenderBackend a Painter needs: these
// tests only exercise the caret request slot, which touches no drawing.
type caretTestBackend struct{}

func (caretTestBackend) Init() error                                                   { return nil }
func (caretTestBackend) Shutdown()                                                     {}
func (caretTestBackend) Size() UnitSize                                                { return UnitSize{Width: 800, Height: 600} }
func (caretTestBackend) Metrics() CellMetrics                                          { return DefaultCellMetrics() }
func (caretTestBackend) BeginFrame()                                                   {}
func (caretTestBackend) EndFrame()                                                     {}
func (caretTestBackend) Clear(style.CellStyle)                                         {}
func (caretTestBackend) SetClip(UnitRect)                                              {}
func (caretTestBackend) DrawCell(Unit, Unit, rune, style.CellStyle)                    {}
func (caretTestBackend) DrawText(x, y Unit, _ string, _ style.CellStyle, _ *Font) Unit { return 0 }
func (caretTestBackend) DrawTextAligned(UnitRect, string, Alignment, Alignment, style.CellStyle, *Font) {
}
func (caretTestBackend) FillRect(UnitRect, rune, style.CellStyle)                     {}
func (caretTestBackend) DrawRect(UnitRect, style.BorderStyle, style.CellStyle)        {}
func (caretTestBackend) DrawHLine(Unit, Unit, Unit, rune, style.CellStyle)            {}
func (caretTestBackend) DrawVLine(Unit, Unit, Unit, rune, style.CellStyle)            {}
func (caretTestBackend) DrawBox(UnitRect, style.BorderStyle, string, style.CellStyle) {}
func (caretTestBackend) PollEvent() Event                                             { return nil }
func (caretTestBackend) WaitEvent() Event                                             { return nil }
func (caretTestBackend) SetCursorVisible(bool)                                        {}
func (caretTestBackend) SetCursorPosition(Unit, Unit)                                 {}
func (caretTestBackend) SetCursorStyle(int)                                           {}
func (caretTestBackend) SupportsColor() bool                                          { return true }
func (caretTestBackend) SupportsMouse() bool                                          { return true }
func (caretTestBackend) SupportsUnicode() bool                                        { return true }
func (caretTestBackend) ColorDepth() int                                              { return 1 << 24 }
func (caretTestBackend) GetClipboard() string                                         { return "" }
func (caretTestBackend) SetClipboard(string)                                          {}
func (caretTestBackend) Beep()                                                        {}
