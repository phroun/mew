package display

// Finding a source for one connection.
//
// A trinket written down in the wire language says what it reads by name, and
// somebody has to turn that name into a source. For a live `source:` name that
// is the process registry and needs nothing from here. For a BUNDLE it needs a
// store -- and a store is the desktop's, kept per host identity and app name.
//
// **Which is why this is per connection and not per process.** The bundle two
// different applications each call `objectLibrary` is two different bundles,
// held in two different directories, and a process-wide answer would hand one
// application the other's. That is the hole the trust boundary exists to close,
// so the boundary is where the answer comes from.
//
// The trinket package holds the names and this holds the knowledge, which is the
// only way round it can be: display imports trinkets.

import (
	"fmt"

	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/wire"
	"github.com/phroun/serval"
)

// findSource is what a name on this connection stands for.
//
// A live name is the process registry's, and the same registry a bundle's own
// `source:` includes reach -- one set of names rather than two. A bundle is this
// connection's store's, found and assembled.
func (c *conn) findSource(name string) (serval.Source, error) {
	key, version, isBundle := trinkets.BundleName(name)
	if !isBundle {
		if src, err := trinkets.LookupSource(name); err == nil {
			return src, nil
		}
		// **Under the name the APPLICATION serves it by**, which is the name with
		// its namespace mark off. `source:papers` is how a statement says which
		// namespace it means; `papers` is what the application called it, and a
		// query quoting the mark back names a source the application has not got.
		live, ok := trinkets.LiveName(name)
		if !ok {
			return nil, fmt.Errorf("source %q: nothing is registered under that name", name)
		}
		// **Nothing in this process stands for it, and this is a connection**, so
		// it is this application's: an application hosts the far end of a name the
		// display knows, and there is nothing else the name could mean here. A
		// trinket with no connection still gets the refusal, which is where a
		// misspelling is caught. See appsources.go.
		return c.appSource(live), nil
	}

	// Asked for each time rather than held from the handshake, because renaming
	// a client in the Connections window moves its directories and a connection
	// holding the old path would read from one the user has moved away from.
	store, err := c.appStore()
	if err != nil {
		return nil, fmt.Errorf("bundle %q: %w", key, err)
	}

	// The process registry AND this connection's far ends, so a bundle can
	// reference the application's own records -- see withAppSources.
	loaded, err := store.LoadBundle(key, version, c.withAppSources())
	if err != nil {
		return nil, fmt.Errorf("bundle %q: %w", key, err)
	}

	// **What went wrong short of stopping the load** -- an optional include that was
	// not there, a cycle whose back edge was dropped -- is a report and not a
	// refusal: the load finished, and turning it into a failure would refuse a
	// window over a complaint its author may already know about.
	//
	// There is still no statement for saying it back across the wire. What there is
	// now is the display's own log, which is where a silent drop used to be: the
	// Event Viewer shows it as an Error against the name that was being loaded, and
	// the complaint survives until somebody opens the window. See Desktop.LogError.
	c.report(name, loaded.Trouble)
	return loaded.Source, nil
}

// report puts a load's complaints where somebody can read them: the desktop's log,
// keyed by the name that was being loaded.
//
// **A complaint with nowhere to go used to go nowhere.** This is the floor under
// that, not the finished answer: the load happened for somebody -- a statement, an
// application -- and telling THEM is a wire shape nobody has designed yet. What is
// certain meanwhile is that a silent drop is the worst of the options, being
// indistinguishable from nothing having gone wrong.
func (c *conn) report(name string, troubles []Trouble) {
	if len(troubles) == 0 || c == nil {
		return
	}
	for _, t := range troubles {
		// **To the application**, on the batch that caused the load: it is that
		// author's mistake and their program that can be changed. Said just before
		// the reply, so it is part of the answer to what they sent rather than news
		// arriving out of the blue. See conn.trouble.
		c.trouble(wire.Trouble{About: name, Text: t.String()})

		// **And to the display's own log**, which is the floor under all of it: a
		// load with no statement behind it -- a preload, a tool -- has nobody to
		// tell, and an application is free to ignore what it is told.
		if c.server != nil && c.server.desktop != nil {
			c.server.desktop.LogError(name, t.String())
		}
	}
}
