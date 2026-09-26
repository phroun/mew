package protocol

// A question the display holds open, as an object.

// Most events are announcements: something happened, and whoever cares hears about
// it. A few are not. A window asking to close is the display saying *I am about to
// do this* and stopping until the application says whether it may -- because the
// application is the only one that knows there is unsaved work, and it may need to
// put a dialog in front of a person before it can answer.
//
// **An application only ever reaches the display over the wire**, so a decision it
// cannot make over the wire is a decision it cannot make. A Go-side handler
// returning false, with nothing carrying that across, is a feature that exists for
// the host and not for the applications the host is for.
//
// # It is an object, and nothing about events changes
//
// The display mints a DECISION, hands its id to whoever is listening as an
// ordinary field of an ordinary event, and the application decides it the way it
// does anything else to an object:
//
//	event window_closing window=17 decision=94
//	do 94 allow
//
// So an event stays one-way and stays an announcement. Nothing in the middle grew
// a reply channel, no verb was added, and the three client libraries already say
// `do`. A decision is opted INTO, by the one event type in a hundred that wants
// one, and the other ninety-nine are untouched.
//
// The vocabulary is two words, `allow` and `deny`, because every decidable event
// is the same question: the display is about to do something, and may it? Closing
// a window is one. Drawing a refusal across a list is another -- denying that one
// is an application saying it has shown the reader itself.
//
// # Nobody is made to answer
//
// **Nothing subscribed means the default, at once.** An application that never
// asked to hear about closing is not consulted about closing: the event would reach
// nobody, so there is nobody who could decide it. Deciding returns nil, the event
// does not go out, and the caller does what it would have done unasked.
//
// **A deadline is optional.** Some decisions can be defaulted after a moment; a
// close cannot, because the answer may be a person reading a dialog, so it waits
// Whenever. The rule above and the one below are what keep that from being a hang.
//
// **A connection going means every decision it owed.** Whatever was outstanding
// comes back unsaid the moment the application is gone, because nothing is going to
// arrive after that.
//
// # It never blocks
//
// The display asks and carries on. Whoever wanted the decision is called back
// later -- so the caller is the one that has to hold its own state until then, and
// a window close does exactly that: it declines to close now, and closes itself
// when the answer says it may.

import (
	"fmt"
	"sync"
	"time"
)

// DecisionType is the wire type name a decision is described under, and
// DecisionField the event field its id travels on.
const (
	DecisionType  = "decision"
	DecisionField = "decision"
)

// The two words a decision is decided with.
const (
	DecisionAllow = "allow"
	DecisionDeny  = "deny"
)

// Whenever is the deadline of a decision that has none: it waits as long as it
// takes, which is what a question a person has to answer needs.
const Whenever time.Duration = 0

// A Verdict is what a decision came to.
type Verdict struct {
	// Allowed is what the application said.
	Allowed bool

	// Said is false where nobody did: the deadline passed, or the connection
	// went. The caller takes its own default, which it is the only one that
	// knows -- a close stays open, a refusal gets drawn.
	Said bool
}

// A Decision is one question the display is holding open, addressable over the
// wire for as long as it is open and gone the moment it is decided.
//
// It is an Object like any other, which is the whole point: `do <id> allow` needs
// no verb, no client change and no corpus entry, because `do` already exists.
type Decision struct {
	id  uint64
	ctx *BindContext

	mu    sync.Mutex
	then  func(Verdict)
	timer *time.Timer
}

// ID implements Object: the id the event carried, and the id a statement names.
func (d *Decision) ID() uint64 { return d.id }

// Set implements Object. A decision holds nothing -- it is a question, and the
// only thing to do to a question is answer it.
func (d *Decision) Set(name string, _ *Value, _ FlagState) error {
	return fmt.Errorf("%s: a decision holds no properties; decide it with do ... %s or %s",
		name, DecisionAllow, DecisionDeny)
}

// Append implements Object: nothing goes inside a decision.
func (d *Decision) Append(slot string, _ Object) error {
	return fmt.Errorf("%s: nothing goes inside a decision", slot)
}

// Do decides it. A word this decision does not know is refused and the decision
// stays open, so a misspelling is answered rather than silently counting as one of
// the two things it might have meant.
//
// Deciding a decision TWICE is refused too, and by then it is not even found: it
// stopped being addressable the moment it was decided. That refusal comes from the
// session, which no longer holds the id.
func (d *Decision) Do(action string, _ []*Arg) error {
	switch action {
	case DecisionAllow:
		d.settle(Verdict{Allowed: true, Said: true})
		return nil
	case DecisionDeny:
		d.settle(Verdict{Said: true})
		return nil
	}
	return fmt.Errorf("do: a decision is %s or %s, not %q", DecisionAllow, DecisionDeny, action)
}

// settle calls whoever wanted the decision back exactly once, whichever of the
// four ways it ends: allowed, denied, timed out, or the connection gone.
func (d *Decision) settle(v Verdict) {
	d.mu.Lock()
	then := d.then
	d.then = nil
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	d.mu.Unlock()
	if then == nil {
		return
	}
	d.ctx.forget(d)
	// **What follows from a verdict is not an echo of it**, so it is announced.
	//
	// `do` suppresses emission, because a change the client asked for should not
	// come back at it as an event (D20). But the client did not ask for this
	// change: the display did, and the client only took the obstacle away. It is
	// the same reason an `ask` runs outside the suppression -- an answer to a
	// question the display put is the point of having asked.
	//
	// And it is not decoration. Allowing a close does not mean the window closed:
	// a child window can still refuse, and the close ends with everything still on
	// the screen. An application that allowed a close therefore cannot work out
	// what happened, and `window_closed` is the only thing that tells it.
	d.ctx.announcing(func() { then(v) })
}

// Deciding asks, and hands back the decision the event went out carrying -- or nil
// where nobody is listening, and then the event did not go out either and the
// caller should do what it would have done unasked.
//
// `within` of Whenever waits as long as it takes. `then` runs exactly once, on
// whatever thread the answer arrives on or on the connection's own thread where a
// deadline or a disconnection decided it.
func (c *BindContext) Deciding(ev *Event, within time.Duration, then func(Verdict)) *Decision {
	if c == nil || ev == nil || then == nil {
		return nil
	}
	// **Nobody addressable means nobody can decide.** A connection that holds no
	// objects of the display's own has nowhere for the id to point, so there is no
	// question to put and the event stays the announcement it was.
	if c.Adopt == nil {
		c.EmitEvent(ev)
		return nil
	}
	if !c.listening(ev) {
		// Said here rather than by a deadline, because waiting for an answer that
		// has nowhere to come from is the difference between a window that shuts
		// and one that hangs.
		return nil
	}

	d := &Decision{id: virtualIDSource(), ctx: c, then: then}
	c.mu.Lock()
	if c.decisions == nil {
		c.decisions = map[uint64]*Decision{}
	}
	c.decisions[d.id] = d
	c.mu.Unlock()
	c.Adopt(d)

	if within > Whenever {
		d.mu.Lock()
		d.timer = time.AfterFunc(within, func() {
			c.onThread(func() { d.settle(Verdict{}) })
		})
		d.mu.Unlock()
	}

	// The id rides on the event as an ordinary field, which is what makes this one
	// decidable and leaves every other event alone.
	c.EmitEvent(ev.WithUint(DecisionField, d.id))
	return d
}

// Undecided brings back everything outstanding unsaid, which is what a connection
// going means: nothing is going to arrive after that.
//
// **And it is final**: nothing is asked on this connection again. That is not
// tidiness, it is the same rule as the one about nobody listening. Tearing a
// connection down closes the windows it left behind, and a window that asked the
// departed application whether it might close would wait for ever -- so the
// application would be removed with its windows still on the screen. A connection
// that is gone decides nothing, and every close after this goes through at once.
func (c *BindContext) Undecided() {
	if c == nil {
		return
	}
	c.mu.Lock()
	held := c.decisions
	c.decisions = nil
	c.gone = true
	c.mu.Unlock()
	for _, d := range held {
		d.settle(Verdict{})
	}
}

// forget drops a decided decision, from the connection's own table and from the
// ids a statement can name. It is not `destroy`: the application did not ask for
// it to go, it went because it was answered.
func (c *BindContext) forget(d *Decision) {
	c.mu.Lock()
	delete(c.decisions, d.id)
	c.mu.Unlock()
	if c.Drop != nil {
		c.Drop(d.id)
	}
}

// listening reports whether an event would actually reach anybody right now: the
// same two gates EmitEvent applies, plus the connection still being there, all
// asked before the event is built into a question.
func (c *BindContext) listening(ev *Event) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.gone && c.suppress == 0 && (ev.Type == "command" || c.subscribedLocked(ev))
}

// announcing runs fn with emission open, whatever depth of suppression it was
// called at, and puts the suppression back afterwards. It is the inverse of
// Suppressed, and like Suppressed it is only ever called on the thread the
// connection does its work on -- a decision is minted where the thing being
// decided happens and decided inside a batch, and both of those run there.
func (c *BindContext) announcing(fn func()) {
	c.mu.Lock()
	held := c.suppress
	c.suppress = 0
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.suppress = held
		c.mu.Unlock()
	}()
	fn()
}

// onThread runs fn where the connection runs its own work, so a verdict a timer
// decided arrives where one an application decided would have.
func (c *BindContext) onThread(fn func()) {
	if c.Post != nil {
		c.Post(fn)
		return
	}
	fn()
}

// The type, so `describe` says what a decision is and the actions it takes are
// checked like any others.
//
// Hosted: the display mints them, a script never does. Virtual with it, which is
// the marker for a wire type that is a RECORD rather than a trinket -- a decision
// has no size, no colours and nothing to enable, so the common properties a
// non-virtual type carries would every one of them fail against it.
func init() {
	RegisterType(DecisionType, &TypeSpec{
		Hosted:  true,
		Virtual: true,
		ID:      func(target any) uint64 { return target.(*Decision).ID() },
		Does: map[string]DoDesc{
			DecisionAllow: NewDoDesc(
				"Let the display do the thing it asked about. The event that carried " +
					"this decision says what that thing is."),
			DecisionDeny: NewDoDesc(
				"Do not. A decision denied is one the application has taken on itself: " +
					"the window stays open, the refusal is not drawn."),
		},
	})
}
