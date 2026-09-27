package trinkets

// A view hearing that its source's answer has landed.
//
// A source across a connection writes a statement and returns; the records come
// later, on whatever thread reads the connection. Every view here reads
// synchronously -- state the sequence, read it, use what the sink got -- so a view
// over such a source reads before the answer exists and would draw nothing
// forever.
//
// serval's `Arriving` is the source saying so, and this is the view hearing it.
// Nothing polls: a view nobody tells draws what it drew.
//
// # The hop is the VIEW's, and that is the point
//
// The notice fires on whatever thread the records arrived on. A view's rows,
// columns and selection belong to the thread that draws it, so the re-read is
// posted there -- and serval, the sources and the connection know nothing about
// any of it.
//
// **A view is the outermost inch of what is mostly a data question.** That one of
// a data service's readers happens to be drawn on a thread of its own is a fact
// about that reader. Pushing it down would make every source carry a UI's
// constraint to suit its last consumer.
//
// A view with no desktop -- detached, or on a plain surface, which is every test
// in this package -- has no thread to hop to and re-reads where it stands. That is
// the honest answer rather than a special case: there is one thread, and this is
// it.
//
// # Why a token
//
// Two views may share one source, and a notice ADDS rather than replaces, so a
// source outlives the subscriptions taken against it. A view pointed at a second
// source, or at the same one twice, would otherwise be told by both. The token is
// what a stale subscription checks: it was the current one when it was taken, and
// it is not any more, so it does nothing.

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/serval"
)

// hearArrivals asks a source to say when records land, and re-reads when it does.
//
// The reread runs on the thread that owns the view, and nothing happens at all for
// a source that answers before it returns -- which is most of them.
func hearArrivals(w core.Trinket, src serval.Source, token int, current func() int, reread func()) {
	serval.TellOnArrival(src, func() {
		hop(w, func() {
			if current() != token {
				return // a subscription against a source this view has left
			}
			reread()
		})
	})
}

// hop runs fn on the thread that owns this view: the desktop's where there is
// one, and this one where there is not.
func hop(w core.Trinket, fn func()) {
	if d := findDesktopFor(w); d != nil {
		d.Post(fn)
		return
	}
	fn()
}
