package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/viewport"
)

// The tombstone names the closed buffer and offers a Re-open link back to a real
// file; an Untitled buffer (no filename) gets no link, and mew:/ surfaces are not
// reopenable.
func TestClosedPlaceholderContent(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")

	buf := e.newClosedPlaceholder("box:/notes.txt")
	got := buf.GetContent()
	if !strings.Contains(got, "The buffer box:/notes.txt has been closed.") {
		t.Errorf("placeholder should name the closed buffer; got %q", got)
	}
	if !strings.Contains(got, "[[box:/notes.txt|Re-open]]") {
		t.Errorf("a named buffer should offer a Re-open link; got %q", got)
	}
	if buf.GetFilename() != closedPlaceholderURL {
		t.Errorf("placeholder address = %q, want %q", buf.GetFilename(), closedPlaceholderURL)
	}

	u := e.newClosedPlaceholder("")
	if strings.Contains(u.GetContent(), "Re-open") {
		t.Errorf("an Untitled buffer must not offer Re-open; got %q", u.GetContent())
	}
	if !strings.Contains(u.GetContent(), "The buffer Untitled has been closed.") {
		t.Errorf("untitled placeholder text wrong; got %q", u.GetContent())
	}

	if reopenableName("mew:/buffers") {
		t.Error("generated surfaces are not reopenable")
	}
	if reopenableName("") {
		t.Error("an empty name is not reopenable")
	}
	if !reopenableName("/tmp/x.txt") {
		t.Error("a real path should be reopenable")
	}
}

// buffer_close retires the buffer everywhere: the active view mirrors
// viewport_close (removed here, since a sibling viewport keeps mew running), and
// a reference parked in another viewport's back-history becomes a mew:/closed
// tombstone rather than lingering or vanishing.
func TestBufferCloseTombstonesHistory(t *testing.T) {
	e, w1 := newTestEditor(t, "A\n")
	fileA := w1.Buffer

	// A second document viewport with fileA parked in its back-history: it shows
	// fileB now, having swapped away from fileA.
	w2id := e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Type: viewport.DocViewport, Dock: viewport.DockNone, Visible: true, Buffer: fileA,
	})
	w2 := e.ViewportManager.GetViewport(w2id)
	e.swapBuffer(w2, buffer.NewFromString("B\n"))

	stacked := func() []*buffer.Buffer { return w2.StackedBuffers() }
	found := func(bufs []*buffer.Buffer, pred func(*buffer.Buffer) bool) bool {
		for _, b := range bufs {
			if pred(b) {
				return true
			}
		}
		return false
	}
	if !found(stacked(), func(b *buffer.Buffer) bool { return b == fileA }) {
		t.Fatal("precondition: fileA should be parked in w2's history")
	}

	// Close fileA everywhere, from its focused active view.
	e.ViewportManager.SetFocus(w1.ID)
	e.executeCommand("buffer_close")

	if e.ViewportManager.GetViewport(w1.ID) != nil {
		t.Error("the active view of the closed buffer should have closed (mirror viewport_close)")
	}
	if found(stacked(), func(b *buffer.Buffer) bool { return b == fileA }) {
		t.Error("fileA must not remain referenced in history after buffer_close")
	}
	if !found(stacked(), func(b *buffer.Buffer) bool { return b.GetFilename() == closedPlaceholderURL }) {
		t.Error("fileA's history slot should have become a mew:/closed tombstone")
	}
}

// Re-opening a closed buffer restores it over EVERY history slot that held its
// (shared) tombstone, not just the one the reader clicked.
func TestReopenRestoresAllTombstones(t *testing.T) {
	e, w1 := newTestEditor(t, "A\n")
	fileA := w1.Buffer

	pick := func(v *viewport.Viewport, pred func(*buffer.Buffer) bool) *buffer.Buffer {
		for _, b := range v.StackedBuffers() {
			if pred(b) {
				return b
			}
		}
		return nil
	}
	isTomb := func(b *buffer.Buffer) bool { return b.GetFilename() == closedPlaceholderURL }

	mk := func() *viewport.Viewport {
		id := e.ViewportManager.CreateViewport(viewport.ViewportOptions{
			Type: viewport.DocViewport, Dock: viewport.DockNone, Visible: true, Buffer: fileA,
		})
		v := e.ViewportManager.GetViewport(id)
		e.swapBuffer(v, buffer.NewFromString("other\n")) // fileA -> v's back history
		return v
	}
	w2, w3 := mk(), mk()

	e.ViewportManager.SetFocus(w1.ID)
	e.executeCommand("buffer_close")

	tomb := pick(w2, isTomb)
	if tomb == nil {
		t.Fatal("w2 should hold a tombstone after close")
	}
	if pick(w3, isTomb) != tomb {
		t.Fatal("both viewports should share the one tombstone for the name")
	}

	buf := buffer.NewFromString("reopened\n")
	if n := e.reopenTombstoneEverywhere(tomb, buf, false, false, false); n != 2 {
		t.Errorf("re-open should restore both slots, got %d", n)
	}
	for _, v := range []*viewport.Viewport{w2, w3} {
		if pick(v, func(b *buffer.Buffer) bool { return b == tomb }) != nil {
			t.Error("no tombstone should remain in history after re-open")
		}
		if pick(v, func(b *buffer.Buffer) bool { return b == buf }) == nil {
			t.Error("the re-opened buffer should be restored in history")
		}
	}
}
