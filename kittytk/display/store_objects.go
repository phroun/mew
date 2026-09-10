package display

// The wire face of the app store, in the verbs the protocol already had.
//
// The store is an OBJECT, one per connection, registered under the name `store`
// the moment the connection opens and handed over by id in the handshake beside
// the application's. An item on it is an object too. So nothing here needs a
// verb of its own:
//
//	sub store store_blob store_done store_data store_gone store_error
//	set store blobs={ new blob key="figaro" type=psl data="..." }
//	ask store inventory                    what is in it
//	set <blob> feed="..."                  append, as a terminal is fed
//	set <blob> data="..."                  replace
//	ask <blob> bytes offset=2048           the chunk that starts there
//	destroy <blob>                         drop it
//
// Every answer names the store as its source, so one subscription covers all of
// them -- and the client subscribes to each event type it wants, as it does for
// any other object.
//
// Asking to hear about the store is NOT asking what is in it. OnSubscribe would
// have been the place to push an inventory, and a terminal does push its grid
// size that way, but that works because it pushes one event OF THE TYPE being
// subscribed to. An inventory is a run of store_blob ending in a store_done, and
// the hook fires on the first subscription of the run -- before the client has
// subscribed to the type that ends it, which would then be filtered out. So the
// inventory is asked for: `ask store inventory`.
//
// A key beginning with the cache mark is discardable; that is the only
// difference. See store.go.

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/phroun/kittytk/protocol"
)

// The events the store raises, all on the store itself so one subscription
// hears everything.
// The questions the store and its blobs answer.
const (
	AskInventory = "inventory" // what the store holds
	AskBytes     = "bytes"     // a blob's contents, a chunk at a time
)

const (
	EventStoreBlob  = "store_blob"  // one blob: what it is and how big
	EventStoreDone  = "store_done"  // the end of an inventory
	EventStoreData  = "store_data"  // one chunk of an item being read back
	EventStoreGone  = "store_gone"  // an item is no longer there
	EventStoreError = "store_error" // what went wrong, and with which key
)

// storeObject is a connection's store as a protocol object. It is registered
// rather than built: `new store` is refused, and the handshake hands the client
// the id of the one it has.
type storeObject struct {
	conn *conn
	id   uint64

	mu    sync.Mutex
	byKey map[string]*blobHandle // the handles this connection has minted
}

func newStoreObject(c *conn, id uint64) *storeObject {
	return &storeObject{conn: c, id: id, byKey: map[string]*blobHandle{}}
}

// ID implements protocol.Object.
func (s *storeObject) ID() uint64 { return s.id }

// Set applies one property. A store has none of its own: what it holds arrives
// through its blobs collection, and what it is asked comes through Ask.
func (s *storeObject) Set(name string, _ *protocol.Value, _ protocol.FlagState) error {
	return fmt.Errorf("a store has no property %q", name)
}

// Ask answers a question put to the store.
func (s *storeObject) Ask(question string, _ []*protocol.Arg) error {
	switch question {
	case AskInventory:
		s.sendInventory()
		return nil
	}
	return fmt.Errorf("a store answers no question called %q", question)
}

// Append adopts what a block built: an item written into the store.
func (s *storeObject) Append(slot string, child protocol.Object) error {
	if slot != "blobs" {
		return fmt.Errorf("a store has no property %q", slot)
	}
	it, ok := targetOf(child).(*blobHandle)
	if !ok {
		return fmt.Errorf("blobs: a store holds blobs, not %T", targetOf(child))
	}
	return s.adopt(it, child.ID())
}

// targetOf reaches the real object behind a wire wrapper. The registry hands
// back its own wrapper for a built object; a host-minted one is itself.
func targetOf(obj protocol.Object) any {
	if tp, ok := obj.(interface{ Target() any }); ok {
		return tp.Target()
	}
	return obj
}

// adopt writes a newly built item and keeps the handle, so the id the client
// holds goes on naming the item after the statement that made it.
func (s *storeObject) adopt(it *blobHandle, id uint64) error {
	store, err := s.conn.appStore()
	if err != nil {
		s.failed("", err)
		return nil // answered as an event; the batch is not at fault
	}
	written, err := store.put(it.key, it.typ, it.pending)
	if err != nil {
		s.failed(it.key, err)
		return nil
	}
	it.attach(s, id, written)

	s.mu.Lock()
	// A key written twice in one connection keeps ONE handle: the id the client
	// last heard for that key is the one that answers for it.
	s.byKey[written.key] = it
	s.mu.Unlock()

	s.itemChanged(it, written)
	return nil
}

// handleFor is the handle for a key, minting and registering one where this
// connection has not named that item yet. The client learns the id from the
// event, so a handle is only ever created here or by a statement that built it.
func (s *storeObject) handleFor(it storeItem) *blobHandle {
	s.mu.Lock()
	held, ok := s.byKey[it.key]
	s.mu.Unlock()
	if ok {
		held.refresh(it)
		return held
	}

	// Built through the connection's own factory, so a minted handle is the
	// same kind of wire object a statement would have made.
	obj, err := s.conn.factory.New("blob")
	if err != nil {
		return nil
	}
	fresh, ok := targetOf(obj).(*blobHandle)
	if !ok {
		return nil
	}
	fresh.attach(s, obj.ID(), it)
	s.conn.session.Register(obj)

	s.mu.Lock()
	s.byKey[it.key] = fresh
	s.mu.Unlock()
	return fresh
}

// forget drops a handle's key, so the name is free to be minted again if the
// item comes back.
func (s *storeObject) forget(key string) {
	s.mu.Lock()
	delete(s.byKey, key)
	s.mu.Unlock()
}

// sendInventory says what the store holds, one answer per item and one saying
// that was all of them. Every item named gets a handle, so what the client is
// told about it can also be addressed.
func (s *storeObject) sendInventory() {
	store, err := s.conn.appStore()
	if err != nil {
		s.failed("", err)
		return
	}
	items := store.list()
	for _, it := range items {
		if h := s.handleFor(it); h != nil {
			s.itemChanged(h, it)
		}
	}
	s.answer(protocol.NewEvent(EventStoreDone).WithInt("count", len(items)))
}

// itemChanged says what an item now is.
func (s *storeObject) itemChanged(h *blobHandle, it storeItem) {
	s.answer(protocol.NewEvent(EventStoreBlob).
		WithUint("blob", h.id).
		WithString("key", it.key).
		WithWord("type", it.typ).
		WithInt("size", int(it.size)))
}

// failed says what was refused and why. A refusal is an answer rather than a
// batch error, so one bad key does not take down the batch it arrived in.
func (s *storeObject) failed(key string, err error) {
	ev := protocol.NewEvent(EventStoreError)
	if key != "" {
		ev = ev.WithString("key", key)
	}
	s.answer(ev.WithString("reason", err.Error()))
}

// answer names the store as the event's source and queues it to go out when the
// batch that asked is done.
//
// It is queued rather than emitted because `set` runs with emission SUPPRESSED
// (D20: a property a client set does not come back to it as an event). That rule
// is right for state -- a checkbox told to be checked must not echo a toggle --
// and these are not echoes: they carry what the client cannot otherwise learn,
// the id a blob is addressed by, its size, its bytes. So they wait for the
// suppression to lift and go out ahead of the reply that ends the batch, the way
// the describe verb's own output does.
func (s *storeObject) answer(ev *protocol.Event) {
	s.conn.queueAnswer(ev.WithUint("store", s.id))
}

// blobHandle is one item as a wire object: what a statement built before the
// store took it, and what the client addresses afterwards.
type blobHandle struct {
	mu      sync.Mutex
	store   *storeObject // nil until the store has taken it
	id      uint64
	key     string
	typ     string
	pending []byte // what a `new item` block carried, until it is written
}

// attach records that the store has taken this item.
func (h *blobHandle) attach(s *storeObject, id uint64, it storeItem) {
	h.mu.Lock()
	h.store, h.id, h.key, h.typ, h.pending = s, id, it.key, it.typ, nil
	h.mu.Unlock()
}

// refresh keeps a held handle's idea of what it names current.
func (h *blobHandle) refresh(it storeItem) {
	h.mu.Lock()
	h.key, h.typ = it.key, it.typ
	h.mu.Unlock()
}

// held is what this handle names, and whether the store has taken it yet.
func (h *blobHandle) held() (*storeObject, string, string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.store, h.key, h.typ, h.store != nil
}

// write is the shared body of the properties that change an item's contents.
func (h *blobHandle) write(what string, data []byte, extend bool) error {
	s, key, typ, ok := h.held()
	if !ok {
		return fmt.Errorf("%s: this item is not in a store yet", what)
	}
	store, err := s.conn.appStore()
	if err != nil {
		s.failed(key, err)
		return nil
	}
	var it storeItem
	if extend {
		it, err = store.appendTo(key, typ, data)
	} else {
		it, err = store.put(key, typ, data)
	}
	if err != nil {
		s.failed(key, err)
		return nil
	}
	s.itemChanged(h, it)
	return nil
}

// sendChunk answers with the slice of this item that starts at offset.
func (h *blobHandle) sendChunk(offset int) error {
	s, key, _, ok := h.held()
	if !ok {
		return fmt.Errorf("read: this item is not in a store yet")
	}
	store, err := s.conn.appStore()
	if err != nil {
		s.failed(key, err)
		return nil
	}
	data, it, last, err := store.read(key, int64(offset))
	if err != nil {
		s.failed(key, err)
		return nil
	}
	s.answer(protocol.NewEvent(EventStoreData).
		WithUint("blob", h.id).
		WithString("key", it.key).
		WithWord("type", it.typ).
		WithInt("offset", offset).
		WithInt("size", int(it.size)).
		WithBlob("data", data).
		WithFlag("last", flagOf(last)))
	return nil
}

// Ask answers a question put to the blob.
func (h *blobHandle) Ask(question string, args []*protocol.Arg) error {
	switch question {
	case AskBytes:
		offset := 0
		for _, a := range args {
			if a.Name == "offset" && a.Value != nil && a.Value.Kind == protocol.NumberValue {
				offset = int(a.Value.Number)
			}
		}
		return h.sendChunk(offset)
	}
	return fmt.Errorf("a blob answers no question called %q", question)
}

// destroy takes the blob out of the store, which is what `destroy <blob>` means
// for something whose whole existence is being in one.
func (h *blobHandle) destroy() error {
	s, key, _, ok := h.held()
	if !ok {
		return nil // never in a store; there is nothing to take it out of
	}
	store, err := s.conn.appStore()
	if err != nil {
		s.failed(key, err)
		return nil
	}
	if err := store.drop(key); err != nil {
		s.failed(key, err)
		return nil
	}
	s.forget(key)
	s.answer(protocol.NewEvent(EventStoreGone).
		WithUint("blob", h.id).
		WithString("key", key))
	return nil
}

func flagOf(b bool) protocol.FlagState {
	if b {
		return protocol.FlagTrue
	}
	return protocol.FlagFalse
}

// The two types, registered so the vocabulary answers for them and a
// subscription on the store's id can be checked. A store is hosted -- the
// connection arrives with one; a blob is built, which is what makes
// `set <store> blobs={ new blob ... }` the way to write one.
func init() {
	protocol.RegisterType("store", &protocol.TypeSpec{
		Hosted: true,
		ID:     func(target any) uint64 { return target.(*storeObject).ID() },
		Props: map[string]protocol.Property{
			"blobs": protocol.NewCollection(func(parent, child any) error {
				return fmt.Errorf("blobs: a store adopts what a block builds through its own Append")
			}).Tip("Blobs written into the store.").Members("blob"),
		},
		Asks: map[string]protocol.AskDesc{
			AskInventory: protocol.NewAskDesc("What the store holds.").
				Answering(EventStoreBlob, EventStoreDone),
		},
		Events: map[string]protocol.EventDesc{
			EventStoreBlob: protocol.NewEventDesc("One item: what an inventory lists, and what a write answers with.").
				Field("store", "uint", "The store the item is in.").
				Field("blob", "uint", "The blob, addressable from here on.").
				Field("key", "string", "What the app calls it. A leading # means the desktop may throw it away.").
				Field("type", "enum", "txt, psl, bin, ini or conf.").
				Field("size", "int", "Its size in bytes."),
			EventStoreDone: protocol.NewEventDesc("The end of an inventory: every item has been sent.").
				Field("store", "uint", "The store the inventory is of.").
				Field("count", "int", "How many items were listed."),
			EventStoreData: protocol.NewEventDesc("One chunk of an item. Set read= again from the offset reached until the chunk marked last.").
				Field("store", "uint", "The store the item is in.").
				Field("blob", "uint", "The blob the chunk is of.").
				Field("key", "string", "What the app calls it.").
				Field("type", "enum", "txt, psl, bin, ini or conf.").
				Field("offset", "int", "Where in the item this chunk starts.").
				Field("size", "int", "The whole item's size in bytes.").
				Field("data", "string", "The chunk's bytes, every one of them escaped that is not printable ASCII.").
				Field("last", "flag", "Set on the chunk that ends the item."),
			EventStoreGone: protocol.NewEventDesc("An item is no longer in the store: what destroying one answers with.").
				Field("store", "uint", "The store it was in.").
				Field("blob", "uint", "The blob that is gone.").
				Field("key", "string", "The key that now holds nothing."),
			EventStoreError: protocol.NewEventDesc("Something the store refused, and why.").
				Field("store", "uint", "The store that refused it.").
				Field("key", "string", "The key it was about, where it named one.").
				Field("reason", "string", "What was wrong with it."),
		},
	})

	itemProp := func(name string, apply func(*blobHandle, *protocol.Value, protocol.FlagState) error) protocol.Property {
		return protocol.NewProperty("string", func(_ *protocol.BindContext, target any, v *protocol.Value, f protocol.FlagState) error {
			return apply(target.(*blobHandle), v, f)
		})
	}
	protocol.RegisterType("blob", &protocol.TypeSpec{
		Virtual: true,
		New:     func() any { return &blobHandle{} },
		Destroy: func(target any) error { return target.(*blobHandle).destroy() },
		Props: map[string]protocol.Property{
			"key": itemProp("key", func(h *blobHandle, v *protocol.Value, f protocol.FlagState) error {
				s, err := protocol.AsString("key", v, f)
				if err != nil {
					return err
				}
				if _, _, _, ok := h.held(); ok {
					return fmt.Errorf("key: an item in a store keeps the name it was written under")
				}
				h.mu.Lock()
				h.key = strings.TrimSpace(s)
				h.mu.Unlock()
				return nil
			}).Tip("What the app calls the item. A leading # means the desktop may throw it away."),
			"type": protocol.NewProperty("enum", func(_ *protocol.BindContext, target any, v *protocol.Value, f protocol.FlagState) error {
				w, err := protocol.AsWord("type", v, f)
				if err != nil {
					return err
				}
				h := target.(*blobHandle)
				h.mu.Lock()
				h.typ = w
				h.mu.Unlock()
				return nil
			}).Tip("What the item is; the file it lives in takes this as its extension.").
				OneOf(storeTypeList()...),
			"data": itemProp("data", func(h *blobHandle, v *protocol.Value, f protocol.FlagState) error {
				s, err := protocol.AsString("data", v, f)
				if err != nil {
					return err
				}
				if _, _, _, ok := h.held(); !ok {
					h.mu.Lock()
					h.pending = []byte(s)
					h.mu.Unlock()
					return nil
				}
				return h.write("data", []byte(s), false)
			}).Tip("The item's whole contents, replacing what it held."),
			"feed": itemProp("feed", func(h *blobHandle, v *protocol.Value, f protocol.FlagState) error {
				s, err := protocol.AsString("feed", v, f)
				if err != nil {
					return err
				}
				return h.write("feed", []byte(s), true)
			}).As("stream").Tip("Append bytes to the item, as a terminal is fed."),
		},
		Asks: map[string]protocol.AskDesc{
			AskBytes: protocol.NewAskDesc("The blob's contents, one chunk at a time.").
				Arg("offset", "int", "Where to read from; 0 for the start.").
				Answering(EventStoreData),
		},
	})
}

// storeObjectIDs numbers the store objects. A store is not a trinket and has no
// ObjectID of its own, so it takes one from a source of its own, well above the
// range trinkets and virtual objects use.
var storeObjectIDs atomic.Uint64

func storeObjectID() uint64 { return storeIDBase + storeObjectIDs.Add(1) }

// storeIDBase keeps store ids clear of every other kind of wire id on a
// connection: nothing else counts from here.
const storeIDBase = 1 << 40

// appStore is the connection's material on disk: the two directories the
// desktop keeps for this client and app, behind one flat namespace.
//
// Resolved per use rather than held from the handshake: renaming a client in the
// Connections window moves the directories, and a connection holding the old
// path would go on writing into one the user has moved away from.
func (c *conn) appStore() (*appStore, error) {
	host, app := c.server.known.folders(c.identity, c.appName)
	if host == "" || app == "" {
		return nil, fmt.Errorf("this desktop keeps nothing for %q: it has no identity to keep it under",
			c.appName)
	}
	return newAppStore(host, app), nil
}

// queueAnswer holds an event until the batch that provoked it is over.
func (c *conn) queueAnswer(ev *protocol.Event) {
	c.answerMu.Lock()
	c.answers = append(c.answers, ev)
	c.answerMu.Unlock()
}

// flushAnswers sends what the store has to say, now that whatever suppressed
// emission has finished. Each still passes the subscription filter, so a client
// that has not asked to hear about its store is not sent any of it.
func (c *conn) flushAnswers() {
	c.answerMu.Lock()
	pending := c.answers
	c.answers = nil
	c.answerMu.Unlock()
	if c.ctx == nil {
		return
	}
	for _, ev := range pending {
		c.ctx.EmitEvent(ev)
	}
}
