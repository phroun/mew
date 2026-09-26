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
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

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
	onTrouble func(Trouble)
}

// Trouble is what this view was last told was wrong, and the zero Trouble where it has
// not been told anything.
//
// It is the last one and not a list of them. A view asks the same question again when
// it is refused, so a list would be the same refusal a hundred times over; what a
// reader needs to know is that this view is not showing what it was asked to show, and
// why.
func (v *troubled) Trouble() Trouble { return v.trouble }

// SetOnTrouble is told when a question this view asked is refused.
//
// **Setting one turns the drawn line off.** A caller that has somewhere better to put
// a refusal -- a status bar, a dialog, a log -- is saying that this view should not
// speak for itself, and a view that both drew the line and called the handler would be
// saying it twice.
//
// Nil puts the line back.
func (v *troubled) SetOnTrouble(fn func(Trouble)) { v.onTrouble = fn }

// took records a refusal and hands it on, and reports whether anything changed.
//
// **The same refusal twice is not news.** A view that was refused asks again, and is
// refused again, and a handler told every time would be told once a frame.
func (v *troubled) took(t Trouble) bool {
	if v.trouble == t {
		return false
	}
	v.trouble = t
	if t.Any() && v.onTrouble != nil {
		v.onTrouble(t)
	}
	return true
}

// untroubled forgets a refusal, which is what an answer that arrives does.
func (v *troubled) untroubled() bool { return v.took(Trouble{}) }

// showsTrouble reports whether this view draws its own refusal, which it does unless
// somebody said they would rather handle it.
func (v *troubled) showsTrouble() bool { return v.trouble.Any() && v.onTrouble == nil }

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
