package trinkets

// What a view was told was wrong, and where it says so.
//
// # A refusal is a value and nothing is thrown
//
// Something a view asked for was turned down: a source it named that the application
// does not serve, a sequence that could not be stated, a scope refused mid-answer.
// Every one of those arrives the same way -- as `Complete.Error` on the answer's own
// ending, or as the refusal an `Open` returns -- which is a VALUE, handed back along
// the path the answer would have taken.
//
// Nothing unwinds. There is no path through the program that exists only when
// something has gone wrong, nothing to catch, and no flow control hidden behind a
// call. A refusal is data about the sequence in the same way a record count is, and it
// is held in the same place: on the view, until something says otherwise.
//
// # And it is SHOWN, because otherwise it is silence
//
// A view that was refused and said nothing drew an empty list for ever, which is
// indistinguishable from a source with nothing in it. That was the whole cost of the
// bug this exists to close: a name misspelled in a bundle produced a window that
// simply never filled, with the reason sitting unread on a wire.
//
// So the default is a line the view draws itself, in the theme's own ErrorMessage
// colour, deducting from the area the rows have. It is a default and not a policy: a
// caller with somewhere better to put a refusal says so with SetOnTrouble and the line
// goes away. That is the discoverability path -- a programmer meets the refusal
// because it is on the screen, and decides then whether to handle it.

import (
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// troubleDecision is how long a view waits for an application to say it has handled
// a refusal, before drawing the line anyway.
//
// **A close waits as long as it takes and this does not**, and the difference is who
// is answering. A close may be waiting on a PERSON reading a dialog. This is waiting
// on a PROGRAM that has everything it needs the moment it is asked: it either
// recognises the refusal and has somewhere to put it, or it does not. Nothing here
// is worth a reader staring at an empty area for.
//
// So the deadline is long enough for a round trip and far short of a reader noticing
// -- and where it passes, the line is DRAWN. That is the safe half: an application
// that asked to decide and then said nothing is not an application that has shown
// the reason anywhere, and the reason is the whole point.
const troubleDecision = 250 * time.Millisecond

// A Trouble is one thing a view was told was wrong.
//
// The zero Trouble is no trouble, which is what `Any` reads.
type Trouble struct {
	// Reason is what the source said, in its own words. This library does not
	// rewrite it: whoever refused the question knows why, and a reason passed through
	// is a reason a programmer can search their own code for.
	Reason string

	// At is the position the view was asking about, and -1 where it was not asking
	// about one -- a sequence that could not be stated at all has no position.
	At int
}

// Any reports whether there is a trouble here.
func (t Trouble) Any() bool { return t.Reason != "" }

// troubled is what a view keeps about a refusal.
//
// Embedded rather than written twice: a refusal is the same thing to a list and to a
// tree, and the only part that differs is where each one paints it.
type troubled struct {
	trouble   Trouble
	onTrouble func(Trouble) bool

	// announce is the wire's own hearing of the same thing: the `trouble` event,
	// set when this view is bound to a connection. Separate from onTrouble because
	// they belong to two different callers -- whoever built this view in Go, and
	// whoever is holding the other end of a socket -- and either may want it.
	//
	// It reports whether the application is being ASKED rather than merely told,
	// which is a thing only it can know: it holds the subscriptions.
	announce func(Trouble) bool

	// handled says the last refusal was taken off this view's hands: a handler
	// answered that it has dealt with it, so there is nothing for the line to say.
	// It is about THIS refusal and is reconsidered for the next one, which is what
	// makes it different from `quiet`.
	handled bool

	// deciding says an application is being asked whether it has handled this
	// refusal, and has not answered yet.
	//
	// **The line waits, rather than appearing and being taken away.** A Go handler
	// answers inside took and there is nothing to wait for; an application answers
	// over a wire, a round trip later, which is several frames. Drawing first and
	// retracting would put a red line on the screen and snatch it back, for every
	// refusal, on every application that handles its own -- and a refusal is
	// already something that arrived asynchronously, so a line that appears a
	// moment after it does is not late, it is the line appearing.
	deciding bool

	// quiet says somebody would rather this view did not draw its own line.
	//
	// **Asked for, never inferred.** Being TOLD about a refusal and choosing where
	// it is SHOWN are two decisions: an application that subscribes to the event to
	// write it in a log still wants its reader to see the line, and one with a
	// status bar of its own wants the line gone whether or not anything is
	// listening. Reading the one as the other -- which this did, silencing the line
	// the moment a handler was set -- makes a visible thing move for an invisible
	// reason.
	quiet bool
}

// Trouble is what this view was last told was wrong, and the zero Trouble where it has
// not been told anything.
//
// It is the last one and not a list of them. A view asks the same question again when
// it is refused, so a list would be the same refusal a hundred times over; what a
// reader needs to know is that this view is not showing what it was asked to show, and
// why.
func (v *troubled) Trouble() Trouble { return v.trouble }

// SetOnTrouble is told when a question this view asked is refused, and answers
// whether it has HANDLED it.
//
// **True means handled, and a handled refusal is not drawn.** It is the shape a
// window's close handler has -- the caller is asked, and what it answers decides what
// happens next -- and it is per refusal rather than a standing preference: a handler
// that puts a missing source in its own status bar and answers true for that, and
// answers false for one it does not recognise, gets the line for the second and not
// the first.
//
// False means told, not handled: the view draws the line as it would have anyway. A
// caller that wants no line ever says so once with SetShowsTrouble.
func (v *troubled) SetOnTrouble(fn func(Trouble) bool) { v.onTrouble = fn }

// SetShowsTrouble says whether this view draws its own refusal. It does, until
// somebody with somewhere better to put it -- a status bar, a dialog, a log -- says
// otherwise.
//
// **The default is to draw it**, because the case this exists for is the one where
// nobody has thought about refusals at all: a view that was turned down and said
// nothing draws an empty area for ever, which is what a source with nothing in it
// looks like.
func (v *troubled) SetShowsTrouble(show bool) { v.quiet = !show }

// ShowsTrouble reports whether this view draws its own refusals.
func (v *troubled) ShowsTrouble() bool { return !v.quiet }

// announceTrouble is how the wire binding hears a refusal. Not exported: an
// application reaches this through the `trouble` event, and a Go caller through
// SetOnTrouble.
//
// fn answers whether the application is being ASKED about this refusal rather than
// simply told -- see the `deciding` field. Only the binding can know that, because
// only it knows whether anything subscribed.
func (v *troubled) announceTrouble(fn func(Trouble) bool) { v.announce = fn }

// decidedTrouble records what an application decided about a refusal it was asked
// about, and reports whether the line changed -- so a caller redraws only when
// there is something different to draw.
//
// **The refusal is named, because the answer may be about one that is no longer
// true.** A view that was refused, asked, and then answered with records has
// nothing to draw a line about, and a verdict arriving about the old refusal must
// not bring it back.
func (v *troubled) decidedTrouble(t Trouble, handled bool) bool {
	if !v.deciding || v.trouble != t {
		return false
	}
	was := v.showsTrouble()
	v.deciding = false
	v.handled = handled
	return v.showsTrouble() != was
}

// took records a refusal and hands it on, and reports whether anything changed.
//
// **The same refusal twice is not news.** A view that was refused asks again, and is
// refused again, and a handler told every time would be told once a frame.
func (v *troubled) took(t Trouble) bool {
	if v.trouble == t {
		return false
	}
	v.trouble = t
	// **Only a refusal is announced, and not its going away.** An answer arriving is
	// the ordinary thing happening; what is news is a view being unable to show what
	// it was asked to.
	//
	// Both are cleared here and not only where they are set, so that "these are
	// about the trouble this view is holding NOW" is true at every moment and not
	// just at the ones anybody looks. A refusal going away sets neither below,
	// because there is nothing to announce and nobody to ask.
	v.handled = false
	v.deciding = false
	if t.Any() {
		if v.onTrouble != nil && v.onTrouble(t) {
			// Taken off this view's hands, for this refusal. The next one is asked
			// about again.
			v.handled = true
		}
		if v.announce != nil {
			// The application may be being ASKED, and an application that is being
			// asked has not answered yet, so the line waits for it.
			v.deciding = v.announce(t)
		}
	}
	return true
}

// untroubled forgets a refusal, which is what an answer that arrives does.
func (v *troubled) untroubled() bool { return v.took(Trouble{}) }

// showsTrouble reports whether there is a line to draw right now: something was
// refused, nobody has asked this view to keep quiet, nobody answered that they have
// handled this one, and nobody is still being asked.
func (v *troubled) showsTrouble() bool {
	return v.trouble.Any() && !v.quiet && !v.handled && !v.deciding
}

// troubleHeight is what the line takes out of the rows' own area, and nought where
// there is no line.
func (v *troubled) troubleHeight(metrics core.CellMetrics) core.Unit {
	if !v.showsTrouble() {
		return 0
	}
	return metrics.UnitsPerCellHeight
}

// troubleRow is where the line goes: one row at the LEADING edge of the rows' own
// area, the caller having already taken its height out of what the rows get.
//
// Above the rows and not below them, because it reads as what stands INSTEAD of them.
// A refusal is very often why there are no rows at all, and a message at the foot of
// an empty area is a message nobody looks for.
func troubleRow(content core.UnitRect, h core.Unit) core.UnitRect {
	return core.UnitRect{X: content.X, Y: content.Y, Width: content.Width, Height: h}
}

// paintTrouble draws the line: one row, the theme's own ErrorMessage colour, and the
// reason elided to the width rather than wrapped -- a line that grew would move the
// rows under it as the reason changed.
func paintTrouble(p *core.Painter, w *core.TrinketBase, scheme *style.Scheme,
	at core.UnitRect, why string) {

	s := scheme.GetErrorMessage()
	p.FillRect(at, ' ', s)
	shown, _ := w.ElideText(why, at.Width)
	p.DrawTextAligned(at, shown, core.ResolveHAlignFor(core.AlignLayoutNatural, w, nil),
		core.AlignMiddle, s, w.EffectiveFont())
}
