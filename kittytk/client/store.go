package client

// The app's side of the desktop's store: put material there, ask what is
// there, read it back.
//
// Every one of these SENDS. The answers arrive as events on the application
// object, because a large item comes back in pieces and a call that returned
// one of them would have to be made again for each -- so the app registers for
// the answers once, with OnStore, and then asks:
//
//	conn.OnStore(client.StoreData, func(ev *wire.Event) { ... })
//	conn.Data().Put("objects/figaro", "psl", bundle)
//	conn.Data().Get("objects/figaro", 0)
//
// The desktop decides where any of it physically lives. An app names its
// material and nothing else about it.

import (
	"fmt"

	"github.com/phroun/kittytk/wire"
)

// The two trees. What is under data is kept until the user says otherwise;
// what is under cache the desktop may throw away at any moment, so nothing an
// app cannot rebuild belongs there.
const (
	DataTree  = "data"
	CacheTree = "cache"
)

// The events the desktop answers a store statement with.
const (
	StoreItem  = "store_item"  // one item: what it is and how big
	StoreDone  = "store_done"  // the end of an inventory
	StoreData  = "store_data"  // one chunk of an item being read back
	StoreError = "store_error" // what went wrong, and with which key
)

// Store is one tree of one app's material.
type Store struct {
	c    *Conn
	tree string
}

// Data is what the app keeps; Cache is what it can afford to lose.
func (c *Conn) Data() Store  { return Store{c: c, tree: DataTree} }
func (c *Conn) Cache() Store { return Store{c: c, tree: CacheTree} }

// App is the connection's own application object -- what the store's answers
// are raised on, and what app-wide properties are set through.
func (c *Conn) App() Handle { return Handle{c: c, id: c.AppID()} }

// OnStore registers a handler for one of the store's answers and opens the
// flow for it.
func (c *Conn) OnStore(event string, fn func(*wire.Event)) { c.App().On(event, fn) }

// Put writes an item, replacing whatever the key held. typ is one of txt, psl,
// bin, ini or conf; the desktop refuses anything else.
func (s Store) Put(key, typ string, data []byte) error {
	return s.send("store_put", key, typ, data)
}

// Append adds to the end of an item, making it first if there is none. It is
// how something too large for one statement is written: send it in pieces.
func (s Store) Append(key, typ string, data []byte) error {
	return s.send("store_append", key, typ, data)
}

// List asks for the inventory: a store_item for each, then a store_done.
func (s Store) List() error {
	_, err := s.c.Exec(fmt.Sprintf("store_list tree=%s", s.tree))
	return err
}

// Get asks for one chunk of an item, starting at offset. The answer says where
// it starts and whether it is the last; ask again from the end of what
// arrived until it is.
func (s Store) Get(key string, offset int) error {
	_, err := s.c.Exec(fmt.Sprintf("store_get tree=%s key=%s offset=%d",
		s.tree, wire.Quote(key), offset))
	return err
}

func (s Store) send(verb, key, typ string, data []byte) error {
	_, err := s.c.Exec(fmt.Sprintf("%s tree=%s key=%s type=%s data=%s",
		verb, s.tree, wire.Quote(key), typ, wire.QuoteBlob(data)))
	return err
}
