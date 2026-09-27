package main

// A source that refuses, so the refusal can be looked at.
//
// A refusal is a VALUE: it travels back along the path the answer would have taken,
// on the answer's own ending, and nothing is thrown. What a view does with one is
// draw it -- a line in the theme's ErrorMessage colour, bright white on red, taken
// out of the rows' own area rather than laid over the first row.
//
// That is a default rather than a policy, so a caller with a status bar to put a
// refusal in says so and the line goes away. This list says nothing, so the line is
// what it does.
//
// Three rows go out before the refusal because THAT is the part worth seeing: the
// rows are still there, a row further down, with the reason standing above them. A
// source that refused outright would draw the same line over an empty list, which is
// the case the line was written for and the less interesting one to look at.

import (
	"github.com/phroun/kittytk/client"
	"github.com/phroun/serval"
)

// refusedSourceName is what the demo serves the refusing source under, and what the
// Lists tab's third list names. One constant, because a name spelled twice is the
// misspelling this whole mechanism exists to make visible.
const refusedSourceName = "demo.refused"

// serveRefusal answers three rows and then turns the rest down.
//
// The reason is the source's OWN words and is not rewritten anywhere on the way:
// whoever refused the question knows why, and a reason passed through is a reason a
// programmer can search their own code for.
func serveRefusal(f *client.Fill) {
	for i, name := range []string{"first row", "second row", "third row"} {
		if err := f.Record(i, serval.Named("name", name)); err != nil {
			return
		}
	}
	_ = f.Fail("the demo refuses on purpose: there is nothing of this source past row three")
}

// provideRefusal serves that source on this app's connection.
//
// Before the build, because the build names it. A name nothing serves is refused
// too -- and now says so on the trinket that named it -- but that is the other
// demonstration.
func (a *app) provideRefusal() error {
	_, err := a.conn.ProvideSource(refusedSourceName, serveRefusal)
	return err
}
