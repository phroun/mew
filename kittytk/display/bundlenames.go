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
		return trinkets.LookupSource(name)
	}

	// Asked for each time rather than held from the handshake, because renaming
	// a client in the Connections window moves its directories and a connection
	// holding the old path would read from one the user has moved away from.
	store, err := c.appStore()
	if err != nil {
		return nil, fmt.Errorf("bundle %q: %w", key, err)
	}

	loaded, err := store.LoadBundle(key, version, Sources(trinkets.LiveSources()))
	if err != nil {
		return nil, fmt.Errorf("bundle %q: %w", key, err)
	}

	// What went wrong short of stopping the load -- an optional include that was
	// not there -- is reported and not refused. There is no statement for saying
	// so back across the wire yet, so it is dropped here rather than turned into
	// a failure it is not.
	return loaded.Source, nil
}
