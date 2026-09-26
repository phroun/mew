package protocol

// An event is an announcement, and an announcement cannot be answered. So a
// window asking whether it may close was a Go handler returning false -- a veto
// the host could exercise and an application could not, because an application
// only ever reaches the display over the wire.
//
// A DECISION is an object, which is what lets it be answered with a verb that
// already exists. These tests are about the four ways one ends, and about the two
// that are not answers at all: nobody listening, and nobody left to listen.

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// deciding is a connection just real enough to hold a decision: events land in a
// slice, and what is adopted becomes addressable in the session.
type deciding struct {
	t   *testing.T
	ctx *BindContext
	s   *Session

	mu   sync.Mutex
	sent []string
}

func newDeciding(t *testing.T) *deciding {
	t.Helper()
	d := &deciding{t: t, s: NewSession()}
	d.ctx = &BindContext{}
	d.ctx.Emit = func(ev *Event) {
		d.mu.Lock()
		d.sent = append(d.sent, ev.Encode())
		d.mu.Unlock()
	}
	d.ctx.Adopt = func(obj Object) { d.s.Register(obj) }
	d.ctx.Drop = func(id uint64) { d.s.Forget(id) }
	return d
}

// hearing subscribes to an event type, which is what makes a decision possible
// at all.
func (d *deciding) hearing(eventType string) { d.ctx.Subscribe(0, eventType) }

// say runs one statement the way a batch would.
func (d *deciding) say(src string) error {
	d.t.Helper()
	script, err := Parse(src)
	if err != nil {
		d.t.Fatalf("parse %q: %v", src, err)
	}
	_, err = d.s.Execute(script, countingFactory{})
	return err
}

func (d *deciding) events() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.sent...)
}

// asking opens a decision on a `closing` event and reports what came back.
func (d *deciding) asking(within time.Duration) (*Decision, *Verdict) {
	d.t.Helper()
	var got *Verdict
	dec := d.ctx.Deciding(NewEvent("closing").WithUint("window", 17), within,
		func(v Verdict) { got = &v })
	return dec, got
}

// The id rides on the event as an ordinary field, so the event is still an event:
// one more integer on it, and no reply channel anywhere.
func TestADecisionRidesOnTheEventAsAField(t *testing.T) {
	d := newDeciding(t)
	d.hearing("closing")

	dec, verdict := d.asking(Whenever)
	if dec == nil {
		t.Fatal("nothing was asked, though something was listening")
	}
	if verdict != nil {
		t.Errorf("the verdict came back before anybody decided: %+v", *verdict)
	}
	sent := d.events()
	if len(sent) != 1 {
		t.Fatalf("the display sent %d events, want the one question: %v", len(sent), sent)
	}
	want := "event closing window=17 decision="
	if !strings.HasPrefix(sent[0], want) {
		t.Errorf("the question went out as %q, want it to begin %q", sent[0], want)
	}
	// And the id it names is one a statement can reach, which is the whole
	// mechanism: nothing was added to the language, the decision was added to
	// the session.
	if _, ok := d.s.Object(dec.ID()); !ok {
		t.Errorf("the event named decision=%d, and no statement can reach it", dec.ID())
	}
}

// `do <id> allow` and `do <id> deny` are how it is answered, in the vocabulary
// `describe` already publishes for the type.
func TestADecisionIsAnsweredWithAVerbThatAlreadyExists(t *testing.T) {
	for _, tc := range []struct {
		word    string
		allowed bool
	}{
		{DecisionAllow, true},
		{DecisionDeny, false},
	} {
		t.Run(tc.word, func(t *testing.T) {
			d := newDeciding(t)
			d.hearing("closing")

			var got *Verdict
			dec := d.ctx.Deciding(NewEvent("closing").WithUint("window", 17),
				Whenever, func(v Verdict) { got = &v })
			if dec == nil {
				t.Fatal("nothing was asked")
			}

			if err := d.say(statement(dec.ID(), tc.word)); err != nil {
				t.Fatalf("do %s: %v", tc.word, err)
			}
			if got == nil {
				t.Fatalf("%s reached the decision and nobody was called back", tc.word)
			}
			if got.Allowed != tc.allowed {
				t.Errorf("%s came back Allowed=%v, want %v", tc.word, got.Allowed, tc.allowed)
			}
			if !got.Said {
				t.Errorf("%s came back as nobody having said, and somebody did", tc.word)
			}
		})
	}
}

// **Nothing subscribed means the default, at once.** An application that never
// asked to hear about closing is not consulted about closing: the event would
// reach nobody, so there is nobody who could decide it. Deciding says so by
// handing back nothing, and the event does not go out either -- a question nobody
// can hear is not worth asking.
func TestNobodyListeningIsNobodyDeciding(t *testing.T) {
	d := newDeciding(t)

	dec, verdict := d.asking(Whenever)
	if dec != nil {
		t.Error("a decision was opened for a connection that is not listening, so it will never be answered")
	}
	if verdict != nil {
		t.Errorf("a verdict came back for a question nobody was asked: %+v", *verdict)
	}
	if sent := d.events(); len(sent) != 0 {
		t.Errorf("the question went out to nobody: %v", sent)
	}
}

// A SUPPRESSED connection is not listening either, whatever it subscribed to: the
// event would be dropped on its way out (D20), so the question would reach nobody
// and the decision would be held for ever. It is the same hang as nobody having
// subscribed, arriving by the other door.
func TestASuppressedConnectionIsNotListening(t *testing.T) {
	d := newDeciding(t)
	d.hearing("closing")

	var dec *Decision
	var verdict *Verdict
	d.ctx.Suppressed(func() {
		dec = d.ctx.Deciding(NewEvent("closing").WithUint("window", 17), Whenever,
			func(v Verdict) { verdict = &v })
	})
	if dec != nil {
		t.Error("a decision was opened inside a suppression, so the question was dropped and it will never be answered")
	}
	if verdict != nil {
		t.Errorf("a verdict came back for a question that went nowhere: %+v", *verdict)
	}
	if sent := d.events(); len(sent) != 0 {
		t.Errorf("a suppressed connection emitted %v", sent)
	}
}

// Whenever means NO timer, not a timer of nought. A deadline of nought would fire
// at once and default the very question that must wait for a person.
func TestWheneverSetsNoDeadline(t *testing.T) {
	d := newDeciding(t)
	d.hearing("closing")

	done := make(chan Verdict, 1)
	dec := d.ctx.Deciding(NewEvent("closing").WithUint("window", 17), Whenever,
		func(v Verdict) { done <- v })
	if dec == nil {
		t.Fatal("nothing was asked")
	}
	select {
	case v := <-done:
		t.Fatalf("a decision that waits as long as it takes settled itself: %+v", v)
	case <-time.After(50 * time.Millisecond):
	}
	if _, ok := d.s.Object(dec.ID()); !ok {
		t.Error("a decision with no deadline stopped being addressable on its own")
	}
}

// A decision ends ONCE, whichever two of the four ways arrive together. An answer
// stops the deadline, but a deadline already on its way to the connection's thread
// cannot be recalled -- so the guarantee is in the settling and not in the timer,
// and this asks the settling for it directly.
func TestADecisionSettlesOnce(t *testing.T) {
	d := newDeciding(t)
	d.hearing("closing")

	var verdicts []Verdict
	dec := d.ctx.Deciding(NewEvent("closing").WithUint("window", 17), Whenever,
		func(v Verdict) { verdicts = append(verdicts, v) })

	dec.settle(Verdict{Allowed: true, Said: true})
	dec.settle(Verdict{}) // the deadline, arriving too late to be recalled
	dec.settle(Verdict{}) // and the connection going, after that

	if len(verdicts) != 1 {
		t.Fatalf("whoever asked was called back %d times: %+v", len(verdicts), verdicts)
	}
	if !verdicts[0].Allowed || !verdicts[0].Said {
		t.Errorf("the answer lost to what came after it: %+v", verdicts[0])
	}
}

// Announcing what follows from a verdict opens emission, and PUTS IT BACK: the
// rest of the batch that carried the answer is still a batch the client asked for,
// and must not start echoing at it halfway through.
func TestAVerdictLeavesTheSuppressionAsItFoundIt(t *testing.T) {
	d := newDeciding(t)
	d.hearing("closing")
	d.hearing("closed")

	dec := d.ctx.Deciding(NewEvent("closing").WithUint("window", 17), Whenever,
		func(Verdict) {
			// What a verdict leads to is announced, suppression or no.
			d.ctx.EmitEvent(NewEvent("closed").WithUint("window", 17))
		})

	before := len(d.events())
	d.ctx.Suppressed(func() {
		if err := d.say(statement(dec.ID(), DecisionAllow)); err != nil {
			t.Fatalf("allowing: %v", err)
		}
		if got := len(d.events()); got != before+1 {
			t.Errorf("the verdict's announcement was suppressed as an echo (%d events, want %d)", got, before+1)
		}
		// Still inside the batch, and still suppressed.
		d.ctx.EmitEvent(NewEvent("closed").WithUint("window", 99))
		if got := len(d.events()); got != before+1 {
			t.Errorf("the suppression was not put back: the batch is echoing (%d events)", got)
		}
	})
}

// A connection that holds no objects of the display's own has nowhere for the id
// to point. The event is still an event and still goes out; it just cannot carry
// a question.
func TestAConnectionThatAdoptsNothingAsksNothing(t *testing.T) {
	d := newDeciding(t)
	d.hearing("closing")
	d.ctx.Adopt = nil

	dec, _ := d.asking(Whenever)
	if dec != nil {
		t.Error("a decision was opened with nowhere to be addressed from")
	}
	sent := d.events()
	if len(sent) != 1 {
		t.Fatalf("the display sent %d events, want the plain announcement: %v", len(sent), sent)
	}
	if strings.Contains(sent[0], DecisionField+"=") {
		t.Errorf("the event carries a decision nothing can reach: %q", sent[0])
	}
}

// **A decided decision stops being addressable.** So a second answer is not a
// second verdict -- it is a statement naming nothing, refused by the session,
// which is the truth about it.
func TestADecisionIsAnsweredOnce(t *testing.T) {
	d := newDeciding(t)
	d.hearing("closing")

	calls := 0
	dec := d.ctx.Deciding(NewEvent("closing").WithUint("window", 17), Whenever,
		func(Verdict) { calls++ })

	if err := d.say(statement(dec.ID(), DecisionAllow)); err != nil {
		t.Fatalf("the first answer: %v", err)
	}
	err := d.say(statement(dec.ID(), DecisionDeny))
	if err == nil {
		t.Error("a decision already decided took a second answer")
	} else if !strings.Contains(err.Error(), "no object with id") {
		t.Errorf("the second answer was refused with %q, want it to say the id names nothing", err)
	}
	if calls != 1 {
		t.Errorf("whoever asked was called back %d times, want exactly once", calls)
	}
}

// A word a decision does not know is refused and the decision stays OPEN, so a
// wrong word is answered rather than silently counting as one of the two things it
// might have meant.
//
// The word here is `tile`, and it has to be a word SOME type declares: an object
// the host registered has no type to check an action against, so the session falls
// back to asking whether anything at all does one by that name. A word nothing
// declares never reaches the decision -- the session turns it away first -- and
// would prove only that check, which has its own tests.
func TestAWordADecisionDoesNotKnowLeavesItOpen(t *testing.T) {
	d := newDeciding(t)
	d.hearing("closing")

	var got *Verdict
	dec := d.ctx.Deciding(NewEvent("closing").WithUint("window", 17), Whenever,
		func(v Verdict) { got = &v })

	err := d.say(statement(dec.ID(), "tile"))
	if err == nil {
		t.Error("a decision took a word that is neither allow nor deny")
	} else if !strings.Contains(err.Error(), DecisionAllow) {
		t.Errorf("the refusal was %q, want it to say what a decision does take", err)
	}
	if got != nil {
		t.Errorf("the wrong word decided it: %+v", *got)
	}
	// Still there, so the application can say what it meant.
	if err := d.say(statement(dec.ID(), DecisionAllow)); err != nil {
		t.Fatalf("the corrected answer: %v", err)
	}
	if got == nil || !got.Allowed {
		t.Errorf("the corrected answer came back %+v, want allowed", got)
	}
}

// **A deadline is optional, and where there is one it comes back UNSAID.** The
// caller takes its own default from that, which it is the only one that knows: a
// close stays open, a refusal gets drawn.
func TestADeadlineComesBackWithNobodyHavingSaid(t *testing.T) {
	d := newDeciding(t)
	d.hearing("closing")

	done := make(chan Verdict, 1)
	dec := d.ctx.Deciding(NewEvent("closing").WithUint("window", 17), time.Millisecond,
		func(v Verdict) { done <- v })
	if dec == nil {
		t.Fatal("nothing was asked")
	}

	select {
	case v := <-done:
		if v.Said {
			t.Errorf("the deadline came back as somebody having said: %+v", v)
		}
		if v.Allowed {
			t.Errorf("the deadline came back allowed: %+v", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the deadline passed and nobody was called back")
	}
	// And it is gone: an answer that arrives after the deadline names nothing.
	if _, ok := d.s.Object(dec.ID()); ok {
		t.Error("a decision the deadline settled is still addressable")
	}
}

// **A connection going means every decision it owed.** Whatever was outstanding
// comes back unsaid the moment the application is gone, because nothing is going
// to arrive after that -- and a caller still holding its own state would hold it
// for ever.
func TestAConnectionGoingSettlesWhatItOwed(t *testing.T) {
	d := newDeciding(t)
	d.hearing("closing")

	var verdicts []Verdict
	for range 3 {
		d.ctx.Deciding(NewEvent("closing").WithUint("window", 17), Whenever,
			func(v Verdict) { verdicts = append(verdicts, v) })
	}

	d.ctx.Undecided()
	if len(verdicts) != 3 {
		t.Fatalf("%d of 3 outstanding decisions came back; the rest are held for ever", len(verdicts))
	}
	for i, v := range verdicts {
		if v.Said || v.Allowed {
			t.Errorf("decision %d came back %+v, want nobody having said", i, v)
		}
	}
	// And going twice is not going wrong.
	d.ctx.Undecided()
	if len(verdicts) != 3 {
		t.Errorf("a second teardown called somebody back again (%d)", len(verdicts))
	}
}

// The deadline's verdict arrives where an application's would have: on the thread
// the connection does its own work on. A window close runs on the desktop thread,
// and a timer that closed it from its own goroutine would be touching the window
// tree from outside.
func TestADeadlineIsDeliveredOnTheConnectionsThread(t *testing.T) {
	d := newDeciding(t)
	d.hearing("closing")

	posted := make(chan func(), 1)
	d.ctx.Post = func(fn func()) { posted <- fn }

	done := make(chan Verdict, 1)
	d.ctx.Deciding(NewEvent("closing").WithUint("window", 17), time.Millisecond,
		func(v Verdict) { done <- v })

	var fn func()
	select {
	case fn = <-posted:
	case <-time.After(2 * time.Second):
		t.Fatal("the deadline passed without being posted to the connection's thread")
	}
	select {
	case v := <-done:
		t.Fatalf("the timer called back on its own goroutine: %+v", v)
	default:
	}
	fn()
	select {
	case <-done:
	default:
		t.Error("running what was posted did not deliver the verdict")
	}
}

// statement renders the answer an application would send.
func statement(id uint64, word string) string {
	return "do " + itoa(id) + " " + word
}
