package display

// An application hosting the far end of one of the display's source names.
//
// **The names all live process side.** That is the model, and it is what makes
// this small: `trinkets.RegisterSource` is where a name stands for something, a
// bundle is found in a store, and neither of those is negotiated over the wire.
// An application does not announce what it serves and there is no statement for
// it to announce with.
//
// What an application does is host the REMOTE END of a name the display already
// knows. `source.ApplicationSource` is that end seen from here: a whole
// `serval.Source` that asks for a scope by writing a statement and takes the
// records back as they arrive. It existed before any of this and needed nothing
// added to it.
//
// So there were two joins missing and no new machinery:
//
//	findSource   a name the process registry does not hold, on a connection, is
//	             that connection's application's -- there is nothing else it
//	             could be, and the application is who to ask
//	relay        an application's results reach the source that asked for them,
//	             rather than only the debug relay
//
// # Why the fallback does not lose the refusal
//
// A name nothing stands for is still refused by the STATEMENT, because the
// fallback is a connection's. A trinket built with no connection at all -- every
// in-process caller, and the vocabulary tests -- goes through `LookupSource` and
// gets the refusal it always got. On a connection there IS somewhere else the
// name could mean something, so asking there is the answer rather than a guess.
//
// # One source per name per connection
//
// Two trinkets naming the same source read one object, so they share its
// queries. The application's records are one body whichever trinket is looking
// at them, and opening a second connection-worth of queries for the second
// reader would pay twice for one answer.
//
// A source lasts as long as the connection. There is nothing to forget it with
// because the connection going is what forgets it.

import (
	"sync"

	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/source"
	"github.com/phroun/kittytk/wire"
	"github.com/phroun/serval"
)

// appSources is what a connection keeps: the far ends it has stood up, by name.
type appSources struct {
	mu   sync.Mutex
	held map[string]*serval.AmendedSource
	ends map[string]*source.ApplicationSource
}

// appSource is the far end of one name on this connection, made if it is not
// made already.
//
// **The name is the one the APPLICATION serves it by**, with the namespace mark
// off. `source:papers` is how a statement says which namespace it means; `papers`
// is what the application called it, and a query quoting the mark back names a
// source the application has not got. See trinkets.LiveName.
func (c *conn) appSource(name string) *serval.AmendedSource {
	c.sources.mu.Lock()
	defer c.sources.mu.Unlock()
	if src := c.sources.held[name]; src != nil {
		return src
	}
	if c.sources.held == nil {
		c.sources.held = map[string]*serval.AmendedSource{}
		c.sources.ends = map[string]*source.ApplicationSource{}
	}
	// What it writes goes out on this connection, which is the whole of what
	// makes it this application's end rather than another's.
	end := source.NewApplicationSource(name, func(text string) error {
		c.send(text)
		return nil
	})
	c.sources.ends[name] = end

	// **Wrapped in an amendment, so a reader can write in it.** A trinket editing a
	// cell holds the edit against the nearest amendment from the top -- see
	// trinkets.TreeView.amendable -- and an application's records reach a view with
	// nothing over them, so there would be nowhere to put one. The edit would show
	// until the next read and then be gone.
	//
	// It is the display's optimistic layer and not the application's data: the app
	// hears the edit as an event, with the record's key on it, and decides. What it
	// says back is a `stale` notice on that key, which this layer takes to mean the
	// child's own record may have moved under the amendment -- so the shadow it kept
	// of it goes and the next read asks again. `Amendments` is how whatever holds the
	// edit eventually saves it out.
	//
	// **The notice does not drop the edit.** An amendment stands until somebody says
	// otherwise, and a source saying its own record changed is not that: the two are
	// different claims about different layers, and a reader who edited a cell should
	// not silently lose the edit because the row underneath it moved. Taking the edit
	// back is `Forget`, and nothing on the wire says it yet.
	//
	// Wrapping costs nothing it had: the hint, the tree fields, the arrival notice and
	// the count all pass through, and a census does while nobody has written in it.
	// That is serval's `wrapping.go`, and it is why this is one line rather than a
	// argument about what gets lost.
	//
	// **And a cache UNDER it, which is what makes an answer that arrives late
	// terminate.** An application answers after its read has returned, so the reader
	// is told when records land and reads again -- that is `Arriving`, and it is the
	// only way a source across a connection can be read at all. What stops the
	// second read being a second QUESTION is the cache: it serves the scope from
	// what the first answer filed, without touching the child, so nothing lands and
	// nothing is told and the reader settles. With no cache in the chain the telling
	// was a cycle -- read, answer, told, read -- turning over about twenty times a
	// second for as long as the window was open.
	//
	// It goes under the amendment because the two hold different things. The cache
	// holds the APPLICATION's records, which is what a notice from the application
	// invalidates; the amendment holds the display's optimistic edits on top, which
	// no notice touches. Values are held per source and order per sequence, so two
	// views sorting one source differently still share every value between them.
	//
	// **It terminates while the answer FITS, and a view still asks for all of it.**
	// `treesource.go` reads with a count of 1<<30 -- the whole sequence, every read --
	// so the cache can only serve the second read where the whole sequence is inside
	// its budget. Measured against the 64 MB default: twenty thousand rows of two
	// fields settles at two reads, fifty thousand never settles and turns over at the
	// speed of a full walk. The rest of the fix is the view asking for a window rather
	// than for everything, which is what `everyTreeRow` is standing in for.
	held := serval.NewCachedSource(end)
	src := serval.NewAmendedSource(held)

	// And a notice reaches both, because they hold different things and each forgets
	// what is its own to forget.
	//
	// The CACHE loses the records and, where a named field decides the sequence, the
	// runs that placed them -- which is the whole of what invalidation costs and the
	// reason `how=` travels. The AMENDMENT loses the shadow it kept of the child's
	// own version of an amended record, so the next read asks again.
	//
	// A notice naming no record is every record of the source, which the cache takes
	// as an extent of neither end; there is no per-key shadow to drop for that, so
	// the amendment hears nothing and the re-read is what corrects it.
	end.OnStale(func(n *wire.Stale) {
		held.Stale(nil, n.Notice())
		if n.ID != nil {
			src.Stale(wire.AsData(n.ID))
		}
	})

	c.sources.held[name] = src
	return src
}

// withAppSources is the process registry plus this connection's far ends, which is
// what a BUNDLE's includes are resolved against.
//
// **A bundle can reference a live source, and an application's is a live source.**
//
//	includes: ( rows: ( source: "papers" ) )
//	tree:     ( parent: "up", label: "name" )
//
// That is the whole answer to how a wire application gets a HIERARCHY over its own
// records: the document says what shape they are -- which is #55's hint, about the
// records and not about any view -- and references the application for the records
// themselves. No new property, no new statement, and nothing that lets a document
// lay out somebody else's window.
//
// The connection's own win where a name is in both, for the same reason findSource
// prefers the registry the other way about: there, a registered name is the
// display's own and more specific than "ask the app"; here, the caller has already
// resolved which namespace it meant, and a bundle loaded on this connection is
// reading this connection's world.
func (c *conn) withAppSources() SourceSet { return connSources{c} }

// connSources answers for a live name the way this connection does: the process
// registry first, and then the application.
//
// **It stands the far end UP when asked**, which is why a map would not do. A
// bundle's include is what asks, and it asks in the middle of a load -- before
// anything has named `source:papers` for itself, so before there is anything in a
// map to find.
//
// A name nothing registered is taken to be the application's, which is the same
// call findSource makes and carries the same cost: a misspelling in a document
// becomes a query the application never answers rather than a refusal. The
// alternative is asking the application whether it serves a name, and a load
// cannot wait for an answer.
type connSources struct{ c *conn }

func (s connSources) Source(name string) (serval.Source, bool) {
	if src, held := Sources(trinkets.LiveSources()).Source(name); held {
		return src, true
	}
	return s.c.appSource(name), true
}

// inbound offers an application's answers to the sources that asked for them,
// and reports which statements nobody wanted.
//
// **A statement is offered to every live source and taken by at most one.** A
// result is addressed to a QUERY, and a query belongs to one source, so the
// source that recognises the number is the one that asked. Offering to all of
// them rather than keeping a query-to-source table means there is one place that
// knows which queries are its own: the source itself, which already did.
func (c *conn) inbound(batch []*protocol.Statement) []*protocol.Statement {
	c.sources.mu.Lock()
	live := make([]*source.ApplicationSource, 0, len(c.sources.ends))
	for _, end := range c.sources.ends {
		live = append(live, end)
	}
	c.sources.mu.Unlock()
	if len(live) == 0 {
		return batch
	}

	rest := batch[:0:0]
	for _, stmt := range batch {
		taken := false
		for _, src := range live {
			if src.Inbound(stmt) {
				taken = true
				break
			}
		}
		if !taken {
			rest = append(rest, stmt)
		}
	}
	return rest
}
