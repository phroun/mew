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

	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/source"
)

// appSources is what a connection keeps: the far ends it has stood up, by name.
type appSources struct {
	mu   sync.Mutex
	held map[string]*source.ApplicationSource
}

// appSource is the far end of one name on this connection, made if it is not
// made already.
func (c *conn) appSource(name string) *source.ApplicationSource {
	c.sources.mu.Lock()
	defer c.sources.mu.Unlock()
	if src := c.sources.held[name]; src != nil {
		return src
	}
	if c.sources.held == nil {
		c.sources.held = map[string]*source.ApplicationSource{}
	}
	// What it writes goes out on this connection, which is the whole of what
	// makes it this application's end rather than another's.
	src := source.NewApplicationSource(name, func(text string) error {
		c.send(text)
		return nil
	})
	c.sources.held[name] = src
	return src
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
	live := make([]*source.ApplicationSource, 0, len(c.sources.held))
	for _, src := range c.sources.held {
		live = append(live, src)
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
