package window

// `window_closed` is the only way an application learns that one of its own windows
// has gone, and for a long time it was never sent.
//
// The window's wire binding announced the close through SetOnCloseComplete -- a slot
// that holds ONE handler and belongs to whoever is holding the window. The window
// manager assigns it to drop the window from its list; the MDI pane and the tear-off
// host do the same for theirs. All three assign it when the window is ADOPTED, which
// is after it is built, so the binding's handler was replaced a moment after it was
// installed, on every window an application ever creates.
//
// Nothing in the display needed the event, so nothing noticed. What it cost was an
// application sitting in a terminal waiting for a window the desktop had already
// closed -- reported as a demo that had to be killed with ctrl-C.
//
// So these hold the announcement against each of the three things that take the slot.

import (
	"strings"
	"testing"
)

// adopter stands for anything that holds a window and takes the close-complete slot
// for its own removal: a window manager, an MDI pane, a tear-off host. What they do
// with it differs; that they take it is the whole point.
type adopter struct{ removed []*Window }

func (a *adopter) adopt(w *Window) {
	w.SetOnCloseComplete(func() { a.removed = append(a.removed, w) })
}

// The announcement survives being adopted, which is what every window an application
// creates has happen to it.
func TestAClosedWindowIsAnnouncedAfterItIsAdopted(t *testing.T) {
	c := newClosing(t, false)
	holder := &adopter{}
	holder.adopt(c.w) // the manager, taking the slot, as it does

	if !c.w.Close() {
		t.Fatal("the window did not close")
	}
	if !c.saw("window_closed") {
		t.Errorf("the application was never told its window closed: %v", c.sent)
	}
	// And the holder's own removal still happens: the slot is not being fought
	// over, it is not being used for this at all.
	if len(holder.removed) != 1 {
		t.Errorf("whatever was holding the window removed it %d times, want once", len(holder.removed))
	}
}

// Adopted twice -- a window manager, then a tear-off host taking it away -- and the
// announcement still comes. Each adoption replaces the slot; neither touches the
// observers.
func TestTheAnnouncementSurvivesBeingPassedOn(t *testing.T) {
	c := newClosing(t, false)
	first, second := &adopter{}, &adopter{}
	first.adopt(c.w)
	second.adopt(c.w)

	if !c.w.Close() {
		t.Fatal("the window did not close")
	}
	if !c.saw("window_closed") {
		t.Errorf("the application was never told its window closed: %v", c.sent)
	}
	if len(first.removed) != 0 {
		t.Error("the first holder still removed a window it had handed on")
	}
	if len(second.removed) != 1 {
		t.Errorf("the holder that has the window removed it %d times, want once", len(second.removed))
	}
}

// It names the window, because an application with several has no other way to tell
// which one went.
func TestTheAnnouncementNamesTheWindow(t *testing.T) {
	c := newClosing(t, false)
	(&adopter{}).adopt(c.w)
	c.w.Close()

	want := "event window_closed window=" + itoa(uint64(c.w.ObjectID()))
	for _, line := range c.sent {
		if strings.HasPrefix(line, want) {
			return
		}
	}
	t.Errorf("no %q among %v", want, c.sent)
}

// Once per close, however many holders the window passed through. Observers
// ACCUMULATE, which is what makes them survive -- so a binding that registered one on
// every adoption would announce the same close once per holder, and an application
// watching two windows would think it had lost both.
func TestOneCloseIsAnnouncedOnce(t *testing.T) {
	c := newClosing(t, false)
	(&adopter{}).adopt(c.w)
	(&adopter{}).adopt(c.w)
	(&adopter{}).adopt(c.w)

	c.w.Close()

	n := 0
	for _, line := range c.sent {
		if strings.HasPrefix(line, "event window_closed ") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the close was announced %d times, want once: %v", n, c.sent)
	}
}

// A window nobody adopted announces too -- the case that always worked, kept so a fix
// to the adopted path cannot quietly cost the plain one.
func TestAnUnadoptedWindowIsAnnouncedClosed(t *testing.T) {
	c := newClosing(t, false)
	if !c.w.Close() {
		t.Fatal("the window did not close")
	}
	if !c.saw("window_closed") {
		t.Errorf("a window with nothing holding it announced nothing: %v", c.sent)
	}
}
