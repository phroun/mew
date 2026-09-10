package client

// The app's side of its store on the desktop: a flat set of names, each holding
// one blob.
//
// A key beginning with `#` names something the desktop may throw away at any
// moment, the way `#` names a temporary table in SQL. That is the only
// difference between what is kept and what is cached: one namespace, and the
// name says how long the blob lives.
//
// Nothing here is a verb of its own. The store is an object the display knows
// by name from the moment the connection opens, a blob in it is an object too,
// and the protocol's own verbs do the work:
//
//	conn.OnStore(client.StoreBlob, func(ev *wire.Event) { ... })
//	conn.Store().List()                          // ask store inventory
//	conn.Store().Write("figaro", "psl", bundle)  // set store blobs={ new blob ... }
//	conn.Blob(id).Append(more)                   // set <blob> feed="..."
//	conn.Blob(id).Read(2048)                     // ask <blob> bytes offset=2048
//	conn.Blob(id).Drop()                         // destroy <blob>
//
// Every one of these SENDS. The answers arrive as events on the store, because a
// blob comes back in pieces and a call that returned one of them would have to
// be made again for each.
//
// The desktop decides where any of it physically lives. An app names its
// material and nothing else about it.

import (
	"fmt"

	"github.com/phroun/kittytk/wire"
)

// CacheMark begins the key of a blob the desktop may throw away.
const CacheMark = "#"

// The events the store answers with. All of them name the store as their
// source, so one subscription hears everything.
const (
	StoreBlob  = "store_blob"  // one blob: what it is and how big
	StoreDone  = "store_done"  // the end of an inventory
	StoreData  = "store_data"  // one chunk of a blob being read back
	StoreGone  = "store_gone"  // a blob is no longer there
	StoreError = "store_error" // what went wrong, and with which key
)

// StoreID returns the ObjectID of this connection's store, as reported in the
// handshake. It is 0 for a connection that has none (an in-process one, which
// has no handshake). The display also knows the store by name, so `ask store
// inventory` says the same thing as this id does.
func (c *Conn) StoreID() uint64 { return c.storeID }

// Store is the connection's store as a handle: an object like any other, so
// Set and On reach it directly for anything this type does not wrap.
func (c *Conn) Store() Store { return Store{c.given(c.storeID, wire.StoreName)} }

// Blob is one blob of the store by the id an answer named it with. An app never
// invents one of these: it learns ids from store_blob and store_data events,
// and a blob has no name of its own -- the store's keys name what is IN it,
// not the handles it hands out for writing.
func (c *Conn) Blob(id uint64) Blob { return Blob{Handle{c: c, id: id}} }

// OnStore registers a handler for one of the store's answers and opens the flow
// for it. Subscribing does not ask what is in the store -- List does that -- so
// an app that wants the inventory subscribes to store_blob and store_done and
// then asks.
func (c *Conn) OnStore(event string, fn func(*wire.Event)) { c.Store().On(event, fn) }

// Store is the app's whole store.
type Store struct{ Handle }

// Write puts a blob in the store, replacing whatever the key held.
//
// A key is a NAME, not a path: no slashes, nothing that is only digits, nothing
// unprintable, and `#` only at the front. It is the same name the blob carries
// when it becomes a bundle, and an address reaches into a bundle with slashes
// and positions.
//
// typ is one of txt, psl, bin, ini or conf; the desktop refuses anything else.
// The answer is a store_blob naming the id the blob can be addressed by, which
// is how something larger than one statement is continued -- see Blob.Append.
func (s Store) Write(key, typ string, data []byte) error {
	return s.Set(fmt.Sprintf("blobs={ new blob key=%s type=%s data=%s }",
		wire.Quote(key), typ, wire.QuoteBlob(data)))
}

// List asks what the store holds: a store_blob per blob, then a store_done
// saying how many there were.
func (s Store) List() error { return s.Ask("inventory") }

// Blob is one blob of the store.
type Blob struct{ Handle }

// Append adds to the end of the blob, as a terminal is fed. It is how something
// too large for one statement is written: Write the first piece, then append the
// rest.
func (b Blob) Append(data []byte) error {
	return b.Set("feed=" + wire.QuoteBlob(data))
}

// Replace writes the blob's whole contents again.
func (b Blob) Replace(data []byte) error {
	return b.Set("data=" + wire.QuoteBlob(data))
}

// Read asks for the chunk of the blob that starts at offset. The answer says
// where it starts and whether it is the last; ask again from the end of what
// arrived until it is.
func (b Blob) Read(offset int) error {
	return b.Ask(fmt.Sprintf("bytes offset=%d", offset))
}

// Drop takes the blob out of the store, which is the whole of what it was. It
// answers with a store_gone naming the key.
func (b Blob) Drop() error { return b.Destroy() }
