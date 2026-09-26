package window

// An application that asked to be consulted about closing, and then said nothing.
//
// Waiting for ever is not patience, it is a hang with better manners: a window that
// cannot be closed, for a reason nobody can see. And forcing the close after a few
// seconds is worse, because an application that subscribed may be holding a
// save-your-work dialog in front of somebody, and no deadline is long enough for
// that.
//
// **The two are indistinguishable from the display.** Which is the whole point: it
// stops guessing and asks the one party who can tell, who is looking at the screen.

import (
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// silentApp is a desktop that records the force-close question and holds it open,
// so a test answers it when it chooses to. It is a core.Container so a Window can
// parent to it.
type silentApp struct {
	*core.TrinketBase
	asked   []*Window
	answer  func(force bool)
	settled []*Window
	closed  []bool
	raised  []string
}

func newSilentApp() *silentApp {
	return &silentApp{TrinketBase: core.NewTrinketBase()}
}

func (s *silentApp) Children() []core.Trinket            { return nil }
func (s *silentApp) AddChild(core.Trinket)               {}
func (s *silentApp) RemoveChild(core.Trinket)            {}
func (s *silentApp) ChildAt(core.UnitPoint) core.Trinket { return nil }
func (s *silentApp) Layout()                             {}
func (s *silentApp) LayoutManager() core.LayoutManager   { return nil }
func (s *silentApp) SetLayoutManager(core.LayoutManager) {}

func (s *silentApp) SurfaceWindow(w *Window) { s.raised = append(s.raised, w.Title()) }

func (s *silentApp) AskForceClose(win *Window, then func(force bool)) {
	s.asked = append(s.asked, win)
	s.answer = then
}

// settled records that a close which was being decided has resolved, and whether the
// window actually went -- which is what a sweep over several windows, a quit, is
// waiting to hear.
func (s *silentApp) CloseDecided(win *Window, closed bool) {
	s.settled = append(s.settled, win)
	s.closed = append(s.closed, closed)
}

// unanswered builds a window the way the wire does, parents it to something that
// can ask a person, and lets the deadline pass without the application answering.
func unanswered(t *testing.T) (*closing, *silentApp) {
	t.Helper()
	// Short enough that a test does not wait five seconds for it.
	was := closeDecision
	closeDecision = 5 * time.Millisecond
	t.Cleanup(func() { closeDecision = was })

	c := newClosing(t, true)
	desk := newSilentApp()
	c.w.SetParent(desk)

	// The deadline's verdict is caught on its way to the connection's thread, so
	// this reads it rather than racing it.
	posted := make(chan func(), 1)
	c.ctx.Post = func(fn func()) { posted <- fn }

	if c.w.Close() {
		t.Fatal("the close went through before the application had answered")
	}
	select {
	case fn := <-posted:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("the deadline never passed, so a silent application holds the window for ever")
	}
	if len(desk.asked) != 1 {
		t.Fatalf("the person was asked %d times, want once", len(desk.asked))
	}
	if desk.asked[0] != c.w {
		t.Error("the question is about the wrong window")
	}
	return c, desk
}

// **The person is asked, and nothing has happened yet.** The window is still there
// while they read the question, which is what makes asking better than either
// waiting for ever or forcing it.
func TestASilentApplicationPutsTheQuestionToThePerson(t *testing.T) {
	c, _ := unanswered(t)
	if !c.w.IsVisible() {
		t.Error("the window closed while the person was being asked about closing it")
	}
	if c.saw("window_closed") {
		t.Errorf("the window announced a close nobody has agreed to: %v", c.sent)
	}
}

// Yes, and it goes -- which is the escape hatch: a window held by a hung
// application can always be got rid of.
func TestForcingTheCloseClosesIt(t *testing.T) {
	c, desk := unanswered(t)
	desk.answer(true)

	if c.w.IsVisible() {
		t.Error("the person forced the close and the window is still open")
	}
	if !c.saw("window_closed") {
		t.Errorf("the forced close was not announced: %v", c.sent)
	}
}

// No, and it stays. The person has decided to let the application take its time,
// and nothing here overrules that afterwards.
func TestDecliningToForceLeavesTheWindowOpen(t *testing.T) {
	c, desk := unanswered(t)
	desk.answer(false)

	if !c.w.IsVisible() {
		t.Error("the person declined to force the close and the window closed anyway")
	}
	if c.saw("window_closed") {
		t.Errorf("a close nobody agreed to was announced: %v", c.sent)
	}
}

// **And the application can still answer after the person was asked**, because
// being asked is not the close being decided. A late `allow` closes the window the
// same as a prompt one.
func TestALateAnswerStillCloses(t *testing.T) {
	c, _ := unanswered(t)

	// The decision is spent -- the deadline settled it -- so the statement names
	// nothing, and that is the honest refusal for it.
	if err := c.say("do " + itoa(c.decision()) + " " + protocol.DecisionAllow); err == nil {
		t.Error("a decision the deadline settled took an answer afterwards")
	}
	// What the application does instead is ask again, and the close goes round
	// again with a question of its own.
	c.sent = nil
	if c.w.Close() {
		t.Error("the second close went through without being decided")
	}
	if !c.saw("window_closing") {
		t.Errorf("the second close did not ask: %v", c.sent)
	}
}

// **An application that DID answer is never reported as silent.** Only an unanswered
// question reaches the person -- a deny is an answer, and putting "Ledger did not
// respond" in front of somebody whose application just said "not yet" would be a lie
// about it.
func TestAnApplicationThatAnsweredIsNotReported(t *testing.T) {
	c := newClosing(t, true)
	desk := newSilentApp()
	c.w.SetParent(desk)

	c.w.Close()
	if err := c.say("do " + itoa(c.decision()) + " " + protocol.DecisionDeny); err != nil {
		t.Fatalf("denying the close: %v", err)
	}
	if len(desk.asked) != 0 {
		t.Errorf("an application that denied the close was reported as not responding (%d times)", len(desk.asked))
	}
	if !c.w.IsVisible() {
		t.Error("the application denied the close and the window closed anyway")
	}
}

// **What was ALLOWED is not what HAPPENED.** A child window of this one can still
// refuse after the application allowed the parent's close, and then the close ends
// with everything still on the screen. A sweep told "closed" for that would carry on
// to the next window and quit over a dialog somebody is still answering.
func TestAChildsRefusalMeansTheCloseDidNotHappen(t *testing.T) {
	c := newClosing(t, true)
	desk := newSilentApp()
	c.w.SetParent(desk)

	child := NewWindow("Unsaved changes")
	child.SetParentWindow(c.w)
	child.SetOnClose(func() bool { return false })

	c.w.Close()
	if err := c.say("do " + itoa(c.decision()) + " " + protocol.DecisionAllow); err != nil {
		t.Fatalf("allowing the close: %v", err)
	}

	if !c.w.IsVisible() {
		t.Error("the parent closed over a child that refused")
	}
	if len(desk.closed) != 1 {
		t.Fatalf("the close was reported %d times, want once", len(desk.closed))
	}
	if desk.closed[0] {
		t.Error("the close was reported as done, though the child refused and nothing closed")
	}
}

// **A window with no desktop under it has nobody to ask**, and stays open. Nothing
// invents an answer on a person's behalf.
func TestWithNobodyToAskTheWindowStaysOpen(t *testing.T) {
	was := closeDecision
	closeDecision = 5 * time.Millisecond
	t.Cleanup(func() { closeDecision = was })

	c := newClosing(t, true)
	posted := make(chan func(), 1)
	c.ctx.Post = func(fn func()) { posted <- fn }

	if c.w.Close() {
		t.Fatal("the close went through before the application had answered")
	}
	select {
	case fn := <-posted:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("the deadline never passed")
	}
	if !c.w.IsVisible() {
		t.Error("a window with nobody to ask closed itself")
	}
}

// **A window destroyed while the question was pending is asked about at all.** The
// application may destroy it, or disconnect and have it closed for it, and then a
// "force this closed?" dialog is a question about nothing -- which a person cannot
// answer sensibly and, being modal, would block the desktop while they tried.
func TestAWindowGoneBeforeTheDeadlineIsNotAskedAbout(t *testing.T) {
	was := closeDecision
	closeDecision = 5 * time.Millisecond
	t.Cleanup(func() { closeDecision = was })

	c := newClosing(t, true)
	desk := newSilentApp()
	c.w.SetParent(desk)
	posted := make(chan func(), 1)
	c.ctx.Post = func(fn func()) { posted <- fn }

	if c.w.Close() {
		t.Fatal("the close went through before the application had answered")
	}
	// Destroyed while the seconds run, which is `destroy` and does not ask.
	if err := c.say("destroy " + itoa(uint64(c.w.ObjectID()))); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if c.w.IsVisible() {
		t.Fatal("the window was destroyed and is still open")
	}
	// **And the close was reported as DONE the moment it went**, not left for the
	// deadline: a sweep waiting on this window carries on rather than sitting there.
	if len(desk.settled) != 1 || !desk.closed[0] {
		t.Errorf("the close was reported as settled=%v closed=%v, want it reported once as closed",
			len(desk.settled), desk.closed)
	}
	if c.w.Deciding() {
		t.Error("the window is gone and still marked as awaiting an answer")
	}

	// Now the deadline arrives, about a window that no longer exists.
	select {
	case fn := <-posted:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("the deadline never fired")
	}
	if len(desk.asked) != 0 {
		t.Errorf("the person was asked to force closed a window that had already gone (%d times)", len(desk.asked))
	}
	if len(desk.settled) != 1 {
		t.Errorf("the close was reported %d times, want once", len(desk.settled))
	}
}

// **An application that DISCONNECTED did not fail to answer, it left.** Its windows
// are about to be closed for it, and naming it in a "did not respond" dialog would
// blame it for going -- and put a question in front of somebody about an application
// that is no longer there to be waited for.
func TestAnApplicationThatLeftIsNotReportedAsSilent(t *testing.T) {
	c := newClosing(t, true)
	desk := newSilentApp()
	c.w.SetParent(desk)

	if c.w.Close() {
		t.Fatal("the close went through before the application had answered")
	}
	c.ctx.Undecided() // what tearing the connection down does

	if len(desk.asked) != 0 {
		t.Errorf("a departed application was reported as not responding (%d times)", len(desk.asked))
	}
	if c.w.Deciding() {
		t.Error("the window is still waiting on an answer from a connection that is gone")
	}
	// It stays open, and the teardown that follows closes it: see
	// TestAWindowLeftBehindClosesWithoutAsking.
	if !c.w.IsVisible() {
		t.Error("the window closed itself on the connection going")
	}
}
