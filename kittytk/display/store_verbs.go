package display

// The wire face of the app store: four verbs an app uses to keep material on
// the desktop, and the events they answer with.
//
//	store_put    tree=data key="figaro" type=psl data="..."
//	store_append tree=data key="figaro" type=psl data="..."
//	store_list   tree=cache
//	store_get    tree=data key="figaro" offset=2048
//	store_drop   tree=cache key="orchard-thumbnail"
//
// Answers come back as events on the application object, which is the ID the
// handshake already handed the client, so they reach the app through the same
// subscription it uses for everything else:
//
//	sub <appID> store_item store_done store_data store_error
//
// A verb answers rather than replies. The batch's own reply says the statement
// was understood; what the store did with it is a record of its own, and an
// app that has not asked to hear about its store does not.
//
// This knows nothing about bundles. It is where they will be put.

import (
	"fmt"

	"github.com/phroun/kittytk/protocol"
)

// The events the store raises. They are declared on the application type, so
// the vocabulary answers for them and a misspelled subscription is refused
// rather than silently never firing.
const (
	EventStoreItem  = "store_item"  // one item: what it is and how big
	EventStoreDone  = "store_done"  // the end of an inventory
	EventStoreData  = "store_data"  // one chunk of an item being read back
	EventStoreGone  = "store_gone"  // an item is no longer there
	EventStoreError = "store_error" // what went wrong, and with which key
)

// storeVerb runs one store statement and answers with an event. It returns
// false for a verb that is not one of the store's, so the caller can pass the
// statement on.
func (c *conn) storeVerb(stmt *protocol.Statement) bool {
	switch stmt.Verb {
	case "store_put", "store_append", "store_list", "store_get", "store_drop":
	default:
		return false
	}

	tree := argWord(stmt, "tree")
	key := argString(stmt, "key")
	store, err := c.storeIn(tree)
	if err != nil {
		c.storeFailed(tree, key, err)
		return true
	}

	switch stmt.Verb {
	case "store_list":
		items := store.list()
		for _, it := range items {
			c.answer(protocol.NewEvent(EventStoreItem).
				WithUint("app", c.app.ID()).
				WithWord("tree", tree).
				WithString("key", it.key).
				WithWord("type", it.typ).
				WithInt("size", int(it.size)))
		}
		c.answer(protocol.NewEvent(EventStoreDone).
			WithUint("app", c.app.ID()).
			WithWord("tree", tree).
			WithInt("count", len(items)))

	case "store_put", "store_append":
		typ := argWord(stmt, "type")
		data := []byte(argString(stmt, "data"))
		var it storeItem
		if stmt.Verb == "store_put" {
			it, err = store.put(key, typ, data)
		} else {
			it, err = store.appendTo(key, typ, data)
		}
		if err != nil {
			c.storeFailed(tree, key, err)
			return true
		}
		c.answer(protocol.NewEvent(EventStoreItem).
			WithUint("app", c.app.ID()).
			WithWord("tree", tree).
			WithString("key", it.key).
			WithWord("type", it.typ).
			WithInt("size", int(it.size)))

	case "store_drop":
		if err := store.drop(key); err != nil {
			c.storeFailed(tree, key, err)
			return true
		}
		c.answer(protocol.NewEvent(EventStoreGone).
			WithUint("app", c.app.ID()).
			WithWord("tree", tree).
			WithString("key", key))

	case "store_get":
		// One chunk per request, from where the app says it has got to. The
		// app asks again with the offset it has reached and stops at the one
		// marked last, so neither side holds a whole item to make progress
		// and a large one does not arrive as a single statement.
		offset, _ := argInt(stmt, "offset")
		data, it, last, err := store.read(key, int64(offset))
		if err != nil {
			c.storeFailed(tree, key, err)
			return true
		}
		c.answer(protocol.NewEvent(EventStoreData).
			WithUint("app", c.app.ID()).
			WithWord("tree", tree).
			WithString("key", it.key).
			WithWord("type", it.typ).
			WithInt("offset", offset).
			WithInt("size", int(it.size)).
			WithBlob("data", data).
			WithFlag("last", flagOf(last)))
	}
	return true
}

// storeIn is this connection's shelf in one of the two trees.
//
// The folders are looked up per statement rather than held from the handshake:
// renaming a client in the Connections window moves them, and a connection
// holding the old path would go on writing into a folder the user has moved
// away from.
func (c *conn) storeIn(tree string) (*appStore, error) {
	if tree != dataTree && tree != cacheTree {
		return nil, fmt.Errorf("a tree is %s or %s, not %q", dataTree, cacheTree, tree)
	}
	host, app := c.server.known.folders(c.identity, c.appName)
	if host == "" || app == "" {
		return nil, fmt.Errorf("this desktop keeps nothing for %q: it has no identity to keep it under",
			c.appName)
	}
	return newAppStore(storagePath(tree, host, app)), nil
}

// storeFailed answers with what went wrong. A refusal is an event like any
// other answer rather than a batch error, so one bad key does not take down
// the batch it arrived in.
func (c *conn) storeFailed(tree, key string, err error) {
	ev := protocol.NewEvent(EventStoreError).WithUint("app", c.app.ID())
	if tree != "" {
		ev = ev.WithWord("tree", tree)
	}
	if key != "" {
		ev = ev.WithString("key", key)
	}
	c.answer(ev.WithString("reason", err.Error()))
}

// answer sends an event through the connection's own seam, so a client that
// has not subscribed is not sent it.
func (c *conn) answer(ev *protocol.Event) {
	if c.ctx != nil {
		c.ctx.EmitEvent(ev)
	}
}

func flagOf(b bool) protocol.FlagState {
	if b {
		return protocol.FlagTrue
	}
	return protocol.FlagFalse
}

// argWord returns the named word argument of a statement (the data= in
// `store_put tree=data`), or "" if absent.
func argWord(stmt *protocol.Statement, name string) string {
	for _, a := range stmt.Args {
		if a.Name == name && a.Value != nil && a.Value.Kind == protocol.WordValue {
			return a.Value.Word
		}
	}
	return ""
}

// argInt returns the named whole-number argument of a statement.
func argInt(stmt *protocol.Statement, name string) (int, bool) {
	for _, a := range stmt.Args {
		if a.Name == name && a.Value != nil && a.Value.Kind == protocol.NumberValue && a.Value.IsInt {
			return int(a.Value.Number), true
		}
	}
	return 0, false
}
