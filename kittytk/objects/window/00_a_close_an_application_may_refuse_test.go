package window

// `SetOnClose(func() bool)` was a veto the HOST could exercise and an application
// could not. An application reaches the display only over the wire, and the wire
// binding emitted `window_closed` -- past tense, after the fact, with nothing to
// answer. So the feature existed for the one caller that was not the point of it.
//
// A close now carries a DECISION, and these tests are about the three shapes that
// has: nobody listening closes at once, an allow closes the window a moment later,
// and a deny leaves it open.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/protocol"
)

// closing is a window built the way the wire builds one, with somewhere for its
// events to go and somewhere for its decisions to be addressed from.
type closing struct {
	t   *testing.T
	w   *Window
	ctx *protocol.BindContext
	s   *protocol.Session

	sent []string
}

func newClosing(t *testing.T, listening bool) *closing {
	t.Helper()
	c := &closing{t: t, s: protocol.NewSession()}
	c.ctx = &protocol.BindContext{}
	c.ctx.Emit = func(ev *protocol.Event) { c.sent = append(c.sent, ev.Encode()) }
	c.ctx.Adopt = func(obj protocol.Object) { c.s.Register(obj) }
	c.ctx.Drop = func(id uint64) { c.s.Forget(id) }

	obj, err := protocol.NewRegistryFactory(c.ctx).New("window")
	if err != nil {
		t.Fatalf("building a window the way the wire does: %v", err)
	}
	holder, ok := obj.(interface{ Target() any })
	if !ok {
		t.Fatal("the factory handed back something with no target")
	}
	c.w = holder.Target().(*Window)
	c.w.SetTitle("Document")
	c.s.Register(obj)

	// Always listening for the close that HAPPENED, so these tests can tell a
	// window that shut from one that is waiting. Listening for the close being
	// ASKED about is the thing under test: it is what makes a close askable.
	c.ctx.Subscribe(0, "window_closed")
	if listening {
		c.ctx.Subscribe(0, "window_closing")
	}
	return c
}

// decision reads the id the question went out carrying.
func (c *closing) decision() uint64 {
	c.t.Helper()
	for _, line := range c.sent {
		if !strings.HasPrefix(line, "event window_closing ") {
			continue
		}
		ev, err := protocol.ParseEvent(line)
		if err != nil {
			c.t.Fatalf("the question did not parse: %v", err)
		}
		id, ok := ev.Uint(protocol.DecisionField)
		if !ok {
			c.t.Fatalf("the question carries no %s=: %q", protocol.DecisionField, line)
		}
		return id
	}
	c.t.Fatalf("no window_closing went out: %v", c.sent)
	return 0
}

// say runs one statement the way a batch would.
func (c *closing) say(src string) error {
	c.t.Helper()
	script, err := protocol.Parse(src)
	if err != nil {
		c.t.Fatalf("parse %q: %v", src, err)
	}
	_, err = c.s.Execute(script, protocol.NewRegistryFactory(c.ctx))
	return err
}

func (c *closing) saw(eventType string) bool {
	for _, line := range c.sent {
		if strings.HasPrefix(line, "event "+eventType+" ") {
			return true
		}
	}
	return false
}

// **Nobody listening closes at once.** An application that never asked to hear
// about closing is not consulted about closing, and a window that waited for it
// anyway would be a window that never shut.
func TestAWindowNobodyIsListeningAboutClosesAtOnce(t *testing.T) {
	c := newClosing(t, false)

	if !c.w.Close() {
		t.Error("the close was declined with nobody to decide it")
	}
	if c.w.IsVisible() {
		t.Error("the window stayed open with nobody to keep it open")
	}
	if c.saw("window_closing") {
		t.Errorf("a question went out to nobody: %v", c.sent)
	}
	if !c.saw("window_closed") {
		t.Errorf("the close was not announced: %v", c.sent)
	}
}

// An application that IS listening is asked, and the close it has not yet allowed
// is declined: Close answers now and the application answers later. The window
// stays on the screen in the meantime, which is what lets the answer be a person
// reading a dialog.
func TestAWindowBeingDecidedDoesNotCloseYet(t *testing.T) {
	c := newClosing(t, true)

	if c.w.Close() {
		t.Error("Close reported success while the application was still deciding")
	}
	if !c.w.IsVisible() {
		t.Error("the window closed before the application said it could")
	}
	if !c.saw("window_closing") {
		t.Errorf("nothing was asked of an application that was listening: %v", c.sent)
	}
	if c.saw("window_closed") {
		t.Errorf("the window announced a close it had not done: %v", c.sent)
	}
}

// `do <id> allow` and the window closes itself, which is the whole point: the
// decision outlives the Close that could not wait for it.
func TestAnAllowedCloseCloses(t *testing.T) {
	c := newClosing(t, true)
	c.w.Close()

	if err := c.say("do " + itoa(c.decision()) + " " + protocol.DecisionAllow); err != nil {
		t.Fatalf("allowing the close: %v", err)
	}
	if c.w.IsVisible() {
		t.Error("the application allowed the close and the window is still open")
	}
	if !c.saw("window_closed") {
		t.Errorf("the window closed without saying so: %v", c.sent)
	}
}

// **An allowed close is still announced**, though `do` suppresses emission (D20).
// The client did not ask for the window to close -- the display did, and the client
// only took the obstacle away. And allowing is not the same as closing: a child
// window can still refuse, so an application that allowed a close cannot work out
// what happened, and this is the only thing that tells it.
func TestAnAllowedCloseIsStillAnnounced(t *testing.T) {
	c := newClosing(t, true)
	c.w.Close()
	d := c.decision()
	c.sent = nil

	if err := c.say("do " + itoa(d) + " " + protocol.DecisionAllow); err != nil {
		t.Fatalf("allowing the close: %v", err)
	}
	if !c.saw("window_closed") {
		t.Errorf("the close was suppressed as an echo of the statement that allowed it: %v", c.sent)
	}
}

// `do <id> deny` and it stays. Nothing is left waiting, and the next close asks
// again -- a refusal is about this close, not about the window.
func TestADeniedCloseStaysOpenAndAsksAgain(t *testing.T) {
	c := newClosing(t, true)
	c.w.Close()
	first := c.decision()

	if err := c.say("do " + itoa(first) + " " + protocol.DecisionDeny); err != nil {
		t.Fatalf("denying the close: %v", err)
	}
	if !c.w.IsVisible() {
		t.Error("the application denied the close and the window closed anyway")
	}
	if c.saw("window_closed") {
		t.Errorf("a denied close was announced as a close: %v", c.sent)
	}
	// And it is no longer awaiting an answer, which is what a sweep over several
	// windows reads to tell a refusal from an answer still coming. Left set, this
	// window would look like it was still deciding for ever.
	if c.w.Deciding() {
		t.Error("the window is still marked as awaiting an answer after the application gave one")
	}

	// Asked again, with a decision of its own: the first one is spent.
	c.sent = nil
	c.w.Close()
	if second := c.decision(); second == first {
		t.Errorf("the second close reused decision %d, so a stale answer would decide it", second)
	}
}

// **Permission is for the close that asked for it, and is not kept.** A window
// shown again after an allowed close asks again, because the answer was about the
// unsaved work of a moment ago and there may be different unsaved work now.
func TestPermissionIsNotKeptForTheNextClose(t *testing.T) {
	c := newClosing(t, true)
	c.w.Close()
	if err := c.say("do " + itoa(c.decision()) + " " + protocol.DecisionAllow); err != nil {
		t.Fatalf("allowing the close: %v", err)
	}

	c.w.Show()
	c.sent = nil
	if c.w.Close() {
		t.Error("the second close went through on the first one's permission")
	}
	if !c.saw("window_closing") {
		t.Errorf("the second close did not ask: %v", c.sent)
	}
	if !c.w.IsVisible() {
		t.Error("the window closed again without being allowed to")
	}
}

// **A close is asked about PER WINDOW, not per application.** An application with a
// document window holding unsaved work and a palette holding nothing subscribes for
// the document alone, and the palette goes on closing at once. Every other test here
// subscribes to every window at once, which would pass either way.
func TestSubscribingToOneWindowLeavesTheOthersAlone(t *testing.T) {
	s := protocol.NewSession()
	ctx := &protocol.BindContext{}
	var sent []string
	ctx.Emit = func(ev *protocol.Event) { sent = append(sent, ev.Encode()) }
	ctx.Adopt = func(obj protocol.Object) { s.Register(obj) }
	ctx.Drop = func(id uint64) { s.Forget(id) }

	f := protocol.NewRegistryFactory(ctx)
	build := func(title string) *Window {
		obj, err := f.New("window")
		if err != nil {
			t.Fatalf("building a window: %v", err)
		}
		w := obj.(interface{ Target() any }).Target().(*Window)
		w.SetTitle(title)
		s.Register(obj)
		return w
	}
	document := build("Unsaved Work")
	palette := build("Palette")

	// One window's close, and one window's only.
	ctx.Subscribe(uint64(document.ObjectID()), "window_closing")

	if !palette.Close() {
		t.Error("the palette's close was declined, though nobody asked to decide it")
	}
	if palette.IsVisible() {
		t.Error("the palette stayed open waiting for an answer nobody wanted to give")
	}
	for _, line := range sent {
		if strings.HasPrefix(line, "event window_closing ") {
			t.Errorf("closing the palette asked about it: %q", line)
		}
	}

	if document.Close() {
		t.Error("the document closed without the application being consulted")
	}
	if !document.IsVisible() {
		t.Error("the document closed while the application was deciding")
	}
	var asked []string
	for _, line := range sent {
		if strings.HasPrefix(line, "event window_closing ") {
			asked = append(asked, line)
		}
	}
	if len(asked) != 1 {
		t.Fatalf("%d windows were asked about, want the one that was subscribed to: %v", len(asked), sent)
	}
	if want := "window=" + itoa(uint64(document.ObjectID())); !strings.Contains(asked[0], want) {
		t.Errorf("the question is about the wrong window: %q, want %s", asked[0], want)
	}
}

// **Destroying is not asking.** The statement came from the application, and
// putting its own order back to it as a question would want an answer inside the
// batch that gave the order.
func TestDestroyingAWindowDoesNotAskToClose(t *testing.T) {
	c := newClosing(t, true)

	if err := c.say("destroy " + itoa(uint64(c.w.ObjectID()))); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if c.w.IsVisible() {
		t.Error("a destroyed window is still open, so it is waiting to be allowed")
	}
	if c.saw("window_closing") {
		t.Errorf("the application was asked permission for its own order: %v", c.sent)
	}
}

// A connection going settles what it owed, and the window stays open on it: the
// safe half of an unsaved-work question is the one where nothing is lost.
func TestACloseOutlivedByNothingStaysOpen(t *testing.T) {
	c := newClosing(t, true)
	c.w.Close()

	c.ctx.Undecided()
	if !c.w.IsVisible() {
		t.Error("the application went while deciding and the window closed itself")
	}
}

// **And then the window can be closed**, which is what tearing a connection down
// does next. A departed application decides nothing, so the close that follows
// goes through at once -- otherwise it would wait for an answer from a socket that
// is shut, and the application would be removed with its windows still up.
func TestAWindowLeftBehindClosesWithoutAsking(t *testing.T) {
	c := newClosing(t, true)
	c.ctx.Undecided()
	c.sent = nil

	if !c.w.Close() {
		t.Error("a window left behind by a departed application declined to close")
	}
	if c.w.IsVisible() {
		t.Error("the window stayed up after the application that owned it went")
	}
	if c.saw("window_closing") {
		t.Errorf("a departed application was asked for permission: %v", c.sent)
	}
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
