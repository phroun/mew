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

	// What went wrong short of stopping the load -- an optional include that was
	// not there -- is reported and not refused. There is no statement for saying
	// so back across the wire yet, so it is dropped here rather than turned into
	// a failure it is not.
	return loaded.Source, nil
}
