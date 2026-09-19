package display

// The wire face of the app store, in the verbs the protocol already had.
//
// The store is an OBJECT, one per connection, registered under the name `store`
// the moment the connection opens and handed over by id in the handshake beside
// the application's. An item on it is an object too. So nothing here needs a
// verb of its own:
//
//	sub store store_blob store_gone store_error
//	set store blobs={ new blob key="figaro" type=psl data="..." }
//	q1=ask store inventory                 what is in it
//	do <blob> append bytes="..."           add to the end, as a terminal is fed
//	set <blob> data="..."                  replace
//	q2=ask <blob> bytes offset=2048         the chunk that starts there
//	destroy <blob>                         drop it
//
// # A question is answered, and a change is announced
//
// The two halves of that list travel by different verbs, and which one a thing
// takes is settled by whether anybody asked.
//
// An inventory and a chunk were ASKED FOR, so they come back as `answer`,
// quoting the key the question carried. An `event` that responded to an ask
// would be a third meaning for a verb that already has two -- a subscribed
// event, or an object saying something about itself -- and it was one: before
// this, asking what the store held sent a run of store_blob and a store_done,
// which a client had to have subscribed to in advance in order to hear its own
// answer. An inventory is now one answer per item and a completion carrying the
// count; a chunk is a single answer that completes the question it answers.
//
// A change to a blob was not asked for. Writing one or appending to it makes the
// store say what the blob NOW IS, with store_blob, because every change to a
// blob is reported that way and that is the store's own business rather than a
// reply to the statement that caused it. Dropping one says store_gone, and a
// refusal on either path says store_error. Those three stay events, and a client
// subscribes to them as it does for any other object.
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

// The questions the store and its blobs answer, and the one thing a blob does.
// Each is answered with `answer`, not with an event: see the file comment.
const (
	AskInventory = "inventory" // what the store holds
	AskBytes     = "bytes"     // a blob's contents, a chunk at a time

	DoAppend = "append" // add to the end of a blob
)

// The events the store raises, all on the store itself so one subscription hears
// everything. None of them answers a question: each is the store saying what has
// happened to a blob, which nobody asked.
const (
	EventStoreBlob  = "store_blob"  // one blob: what it is and how big
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
func (s *storeObject) Ask(question string, _ []*protocol.Arg, out *protocol.Answers) error {
	switch question {
	case AskInventory:
		s.sendInventory(out)
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
func (s *storeObject) sendInventory(out *protocol.Answers) {
	store, err := s.conn.appStore()
	if err != nil {
		out.Fail("%s", err.Error())
		return
	}
	items := store.list()
	for _, it := range items {
		if h := s.handleFor(it); h != nil {
			out.Send(itemArgs(h, it)...)
		}
	}
	out.Done(protocol.Named("count", len(items)))
}

// itemArgs is what an item IS: the arguments an inventory answers with, and the
// fields the store's own report of a change carries.
//
// One shape for both, because they say the same thing about the same item. What
// differs is why it was said -- an inventory was asked for, a change was not -- and
// that is what decides whether it travels as an answer or as an event.
func itemArgs(h *blobHandle, it storeItem) []*protocol.Arg {
	args := []*protocol.Arg{
		{Name: "blob", Value: protocol.NewInt(int64(h.id))},
		protocol.Named("key", it.key),
		{Name: "type", Value: &protocol.Value{Kind: protocol.WordValue, Word: it.typ}},
		{Name: "size", Value: protocol.NewInt(int64(it.size))},
	}
	// The hash is left out where there is none, which is what an item part way
	// through being appended to has. Absent means "not settled", not "empty": an
	// app reads it as nothing to compare against and carries on uploading, which is
	// the safe way round.
	if it.hash != "" {
		args = append(args, protocol.Named("hash", it.hash))
	}
	return args
}

// itemChanged says what an item now is.
//
// The hash is left out where there is none, which is what an item part way
// through being appended to has. Absent means "not settled", not "empty": an
// app reads it as nothing to compare against and carries on uploading, which is
// the safe way round.
func (s *storeObject) itemChanged(h *blobHandle, it storeItem) {
	ev := protocol.NewEvent(EventStoreBlob).
		WithUint("blob", h.id).
		WithString("key", it.key).
		WithWord("type", it.typ).
		WithInt("size", int(it.size))
	if it.hash != "" {
		ev = ev.WithString("hash", it.hash)
	}
	s.announce(ev)
}

// failed says what was refused and why. A refusal is an answer rather than a
// batch error, so one bad key does not take down the batch it arrived in.
func (s *storeObject) failed(key string, err error) {
	ev := protocol.NewEvent(EventStoreError)
	if key != "" {
		ev = ev.WithString("key", key)
	}
	s.announce(ev.WithString("reason", err.Error()))
}

// announce names the store as the event's source and queues it to go out when
// the batch that provoked it is done.
//
// It is queued rather than emitted because `set` runs with emission SUPPRESSED
// (D20: a property a client set does not come back to it as an event). That rule
// is right for state -- a checkbox told to be checked must not echo a toggle --
// and these are not echoes: they carry what the client cannot otherwise learn,
// the id a blob is addressed by and its size. So they wait for the suppression to
// lift and go out ahead of the reply that ends the batch, the way the describe
// verb's own output does.
func (s *storeObject) announce(ev *protocol.Event) {
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
	// The store says what the blob now is, for a write and an append alike:
	// every change to a blob is reported that way, and that is the store's
	// business rather than an answer to the statement that caused it. `do`
	// expects nothing back; what it changes still reports itself.
	s.itemChanged(h, it)
	return nil
}

// sendChunk answers with the slice of this item that starts at offset.
func (h *blobHandle) sendChunk(offset int, out *protocol.Answers) error {
	s, key, _, ok := h.held()
	if !ok {
		return fmt.Errorf("read: this item is not in a store yet")
	}
	store, err := s.conn.appStore()
	if err != nil {
		out.Fail("%s", err.Error())
		return nil
	}
	data, it, last, err := store.read(key, int64(offset))
	if err != nil {
		out.Fail("%s", err.Error())
		return nil
	}
	// **The chunk COMPLETES the question, and the next offset is a new one.**
	// A read asks for the slice that starts here; it is answered with that slice
	// and nothing more. `last` says whether there is another to ask for, which is
	// a fact about the item rather than about this answer.
	args := []*protocol.Arg{
		{Name: "blob", Value: protocol.NewInt(int64(h.id))},
		protocol.Named("key", it.key),
		{Name: "type", Value: &protocol.Value{Kind: protocol.WordValue, Word: it.typ}},
		{Name: "offset", Value: protocol.NewInt(int64(offset))},
		{Name: "size", Value: protocol.NewInt(int64(it.size))},
		protocol.Blob("data", data),
	}
	if last {
		args = append(args, &protocol.Arg{Name: "last", Flag: protocol.FlagTrue})
	}
	out.Done(args...)
	return nil
}

// Do performs an action on the blob. Nothing ANSWERS it -- an action that
// wanted an answer would be a question -- but the store goes on reporting what
// the blob is with store_blob, the way it does for every other change to one,
// and a refusal arrives as store_error.
func (h *blobHandle) Do(action string, args []*protocol.Arg) error {
	switch action {
	case DoAppend:
		data, err := blobArg(action, "bytes", args)
		if err != nil {
			return err
		}
		return h.write(action, data, true)
	}
	return fmt.Errorf("a blob does nothing called %q", action)
}

// blobArg reads one named string argument, which carries bytes.
func blobArg(call, name string, args []*protocol.Arg) ([]byte, error) {
	for _, a := range args {
		if a.Name != name {
			continue
		}
		if a.Value == nil || a.Value.Kind != protocol.StringValue {
			return nil, fmt.Errorf("%s: %s= expects a string", call, name)
		}
		return []byte(a.Value.Str), nil
	}
	return nil, fmt.Errorf("%s: expected %s=", call, name)
}

// Ask answers a question put to the blob.
func (h *blobHandle) Ask(question string, args []*protocol.Arg, out *protocol.Answers) error {
	switch question {
	case AskBytes:
		offset := 0
		for _, a := range args {
			if a.Name == "offset" && a.Value != nil && a.Value.Kind == protocol.NumberValue {
				offset = int(a.Value.Int)
			}
		}
		return h.sendChunk(offset, out)
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
	s.announce(protocol.NewEvent(EventStoreGone).
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
			AskInventory: protocol.NewAskDesc(
				"What the store holds: one answer per item, carrying blob=, key=, " +
					"type=, size= and hash= as store_blob does, then a completion " +
					"carrying count=. A store holding nothing answers the completion " +
					"alone, which is an answer and not a refusal.").
				Answering(protocol.AnswerVerb),
		},
		Events: map[string]protocol.EventDesc{
			EventStoreBlob: protocol.NewEventDesc("One item, as the store now has it: what writing one or appending to it reports. An inventory says the same things, but it was asked for, so it comes back as an answer.").
				Field("store", "uint", "The store the item is in.").
				Field("blob", "uint", "The blob, addressable from here on.").
				Field("key", "string", "What the app calls it. A leading # means the desktop may throw it away.").
				Field("type", "enum", "txt, psl, bin, ini or conf.").
				Field("size", "int", "Its size in bytes. This is the cursor of an upload in progress: how much has landed, and so where to carry on from.").
				Field("hash", "string", "sha256 over the item's bytes, lowercase hex. Compare it against your own copy to decide whether to upload at all. Absent where the item has been appended to and not asked about since, which reads as nothing to compare rather than as empty. Hash WHAT YOU SEND: an app that rewrites line endings on the way out and hashes what it read will never match, and will upload every time."),
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
		},
		Does: map[string]protocol.DoDesc{
			DoAppend: protocol.NewDoDesc("Add to the end of the blob, as a terminal is fed. How anything larger than one statement is written; the store reports what the blob then is with store_blob.").
				Arg("bytes", "blob", "What to add, every byte outside printable ASCII escaped."),
		},
		Asks: map[string]protocol.AskDesc{
			AskBytes: protocol.NewAskDesc(
				"The blob's contents, one chunk at a time. The chunk that starts at "+
					"offset= COMPLETES the question, carrying blob=, key=, type=, "+
					"offset=, size=, data= and last=; reading the next chunk is a new "+
					"question, asked from the offset this one reached.").
				Arg("offset", "int", "Where to read from; 0 for the start.").
				Answering(protocol.AnswerVerb),
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
