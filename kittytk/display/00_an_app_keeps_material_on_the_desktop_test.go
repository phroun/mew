package display_test

// An app on the far end of a real connection puts material in its store, asks
// what it has there, and reads it back -- through the protocol's own verbs, and
// receiving every answer the way it receives any other event.

import (
	"bytes"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/wire"
)

// storeAnswers collects the events one connection's store raises.
type storeAnswers struct {
	mu   sync.Mutex
	got  []*wire.Event
	came chan struct{}
}

func newStoreAnswers() *storeAnswers {
	return &storeAnswers{came: make(chan struct{}, 256)}
}

func (a *storeAnswers) add(ev *wire.Event) {
	a.mu.Lock()
	a.got = append(a.got, ev)
	a.mu.Unlock()
	select {
	case a.came <- struct{}{}:
	default:
	}
}

// await waits for an event of the given type and hands it back, taking it off
// the list so a second wait finds the next one.
func (a *storeAnswers) await(t *testing.T, typ string) *wire.Event {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		a.mu.Lock()
		for i, ev := range a.got {
			if ev.Type == typ {
				a.got = append(a.got[:i], a.got[i+1:]...)
				a.mu.Unlock()
				return ev
			}
		}
		a.mu.Unlock()
		select {
		case <-a.came:
		case <-deadline:
			t.Fatalf("no %s answer arrived", typ)
		}
	}
}

// storeApp dials a host over tls (which is what gives the client an identity,
// and so a store of its own) with every answer being collected, and asks what is
// in it -- so a test starts from a store it has been told is empty.
func storeApp(t *testing.T, name string) (*client.Conn, *storeAnswers) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(display.KnownStoreEnv, filepath.Join(t.TempDir(), "known"))

	_, srv, stop := startHost(t, display.Config{
		Endpoint:    "tls://127.0.0.1:0",
		PromptLocal: true, // loopback would otherwise be admitted without a word
		Authorize:   func(display.AuthRequest) display.AuthDecision { return display.AuthAllowOnce },
	})
	t.Cleanup(stop)

	conn, err := client.Dial("tls://"+srv.Addr(), name, nil)
	if err != nil {
		t.Fatalf("dial tls: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	if conn.StoreID() == 0 {
		t.Fatal("the handshake handed over no store id")
	}

	answers := newStoreAnswers()
	for _, ev := range []string{client.StoreBlob, client.StoreDone, client.StoreData,
		client.StoreGone, client.StoreError} {
		conn.OnStore(ev, answers.add)
	}
	if err := conn.Store().List(); err != nil {
		t.Fatalf("asking a fresh store what it holds: %v", err)
	}
	if count, _ := answers.await(t, client.StoreDone).Int("count"); count != 0 {
		t.Fatalf("a fresh store already held %d blobs", count)
	}
	return conn, answers
}

// wrote waits for the answer to a write and hands back the item's id, which is
// what everything afterwards addresses it by.
func wrote(t *testing.T, answers *storeAnswers, key string) uint64 {
	t.Helper()
	ev := answers.await(t, client.StoreBlob)
	if got, _ := ev.Text("key"); got != key {
		t.Fatalf("the answer is about %q, not %q", got, key)
	}
	id, ok := ev.Uint("blob")
	if !ok || id == 0 {
		t.Fatalf("the answer for %q names no blob to address it by: %s", key, ev.Encode())
	}
	return id
}

// The round trip: an app writes an item, is told what it now is and how to
// address it, and reads back the bytes it wrote.
func TestAnAppWritesAnItemAndReadsItBack(t *testing.T) {
	conn, answers := storeApp(t, "Store App")
	want := []byte("(bundle: \"figaro\")")

	if err := conn.Store().Write("figaro", "psl", want); err != nil {
		t.Fatal(err)
	}
	ev := answers.await(t, client.StoreBlob)
	if key, _ := ev.Text("key"); key != "figaro" {
		t.Errorf("the answer is about %q", key)
	}
	if typ, _ := ev.Word("type"); typ != "psl" {
		t.Errorf("the item is a %q", typ)
	}
	if size, _ := ev.Int("size"); size != len(want) {
		t.Errorf("the item is %d bytes, want %d", size, len(want))
	}
	id, _ := ev.Uint("blob")
	if id == 0 {
		t.Fatal("the answer names no blob id, so nothing can be done to it")
	}

	if err := conn.Blob(id).Read(0); err != nil {
		t.Fatal(err)
	}
	data := answers.await(t, client.StoreData)
	got, _ := data.Blob("data")
	if !bytes.Equal(got, want) {
		t.Errorf("read back %q, want %q", got, want)
	}
	if data.Flag("last") != wire.FlagTrue {
		t.Error("an item that fits in one chunk did not come back as the last one")
	}
}

// Bytes are bytes. A payload holding every value there is comes back holding
// every value there is, which a text encoding of it would not.
func TestBinaryMaterialSurvivesTheWire(t *testing.T) {
	conn, answers := storeApp(t, "Binary App")
	want := make([]byte, 256)
	for i := range want {
		want[i] = byte(i)
	}

	if err := conn.Store().Write("blob", "bin", want); err != nil {
		t.Fatal(err)
	}
	id := wrote(t, answers, "blob")
	if err := conn.Blob(id).Read(0); err != nil {
		t.Fatal(err)
	}
	got, _ := answers.await(t, client.StoreData).Blob("data")
	if !bytes.Equal(got, want) {
		t.Errorf("%d of %d bytes came back", len(got), len(want))
	}
}

// Something too large for one statement is written in pieces and read back in
// pieces: the app feeds the item it made until it has sent it all, then reads
// from where it has got to until the chunk marked last.
func TestALargeItemGoesAndComesBackInPieces(t *testing.T) {
	conn, answers := storeApp(t, "Big App")
	piece := bytes.Repeat([]byte("abcdefghij"), 300) // 3000 bytes, over one chunk

	if err := conn.Store().Write("big", "bin", piece); err != nil {
		t.Fatal(err)
	}
	id := wrote(t, answers, "big")
	want := append([]byte(nil), piece...)
	for i := 0; i < 2; i++ {
		if err := conn.Blob(id).Append(piece); err != nil {
			t.Fatal(err)
		}
		wrote(t, answers, "big")
		want = append(want, piece...)
	}

	var got []byte
	for chunks := 0; ; chunks++ {
		if err := conn.Blob(id).Read(len(got)); err != nil {
			t.Fatal(err)
		}
		ev := answers.await(t, client.StoreData)
		if at, _ := ev.Int("offset"); at != len(got) {
			t.Fatalf("chunk %d starts at %d; the app had %d bytes", chunks, at, len(got))
		}
		if size, _ := ev.Int("size"); size != len(want) {
			t.Errorf("chunk %d says the item is %d bytes; it is %d", chunks, size, len(want))
		}
		data, _ := ev.Blob("data")
		got = append(got, data...)
		if ev.Flag("last") == wire.FlagTrue {
			if chunks < 3 {
				t.Errorf("%d bytes came back in %d chunks", len(want), chunks+1)
			}
			break
		}
	}
	if !bytes.Equal(got, want) {
		t.Errorf("the pieces came back to %d bytes of %d", len(got), len(want))
	}
}

// The inventory: one answer per blob saying what it is, how big, and how to
// address it, then one saying that was all of them. It is asked for -- `ask
// <store> inventory` -- because subscribing says what an app wants to HEAR
// about, which is not the same as asking what is there.
func TestAnAppAsksWhatItHasStored(t *testing.T) {
	conn, answers := storeApp(t, "Inventory App")
	for _, it := range []struct {
		key, typ, data string
	}{
		{"notes", "txt", "hello"},
		{"settings", "ini", "[a]\nb=1\n"},
		{client.CacheMark + "scratch", "txt", "x"},
	} {
		if err := conn.Store().Write(it.key, it.typ, []byte(it.data)); err != nil {
			t.Fatal(err)
		}
		wrote(t, answers, it.key)
	}

	if err := conn.Store().List(); err != nil {
		t.Fatal(err)
	}
	found := map[string]int{}
	for i := 0; i < 3; i++ {
		ev := answers.await(t, client.StoreBlob)
		key, _ := ev.Text("key")
		size, _ := ev.Int("size")
		found[key] = size
	}
	if count, _ := answers.await(t, client.StoreDone).Int("count"); count != 3 {
		t.Errorf("the inventory says %d items, want 3", count)
	}
	// One inventory, both halves: what is cached is named alongside what is
	// kept, and the mark on the key is what tells them apart.
	if found["notes"] != 5 || found["settings"] != 8 || found[client.CacheMark+"scratch"] != 1 {
		t.Errorf("the inventory reads as %v", found)
	}
}

// The mark on a key is the only difference, and it decides which of the two
// directories the bytes sit in -- so Clear Cache can take one and leave the
// other, and the same name unmarked is a different item.
func TestTheCacheMarkDecidesWhereTheBytesGo(t *testing.T) {
	conn, answers := storeApp(t, "Marked App")

	if err := conn.Store().Write("report", "txt", []byte("kept")); err != nil {
		t.Fatal(err)
	}
	wrote(t, answers, "report")
	if err := conn.Store().Write(client.CacheMark+"report", "txt", []byte("cached")); err != nil {
		t.Fatal(err)
	}
	cachedID := wrote(t, answers, client.CacheMark+"report")

	// Two items, not one written twice.
	if err := conn.Store().List(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		answers.await(t, client.StoreBlob)
	}
	if count, _ := answers.await(t, client.StoreDone).Int("count"); count != 2 {
		t.Errorf("a marked key and an unmarked one came to %d items, want 2", count)
	}

	// And the marked one holds what was written to it, not the other's bytes.
	if err := conn.Blob(cachedID).Read(0); err != nil {
		t.Fatal(err)
	}
	got, _ := answers.await(t, client.StoreData).Blob("data")
	if string(got) != "cached" {
		t.Errorf("the marked item reads as %q", got)
	}
}

// An app can take an item out of its store with the verb that already means
// that. Without it a store only ever grows, and the app that filled it has no
// way to empty it.
func TestAnAppTakesAnItemOutOfItsStore(t *testing.T) {
	conn, answers := storeApp(t, "Tidy App")

	var spent uint64
	for _, key := range []string{"keep", "spent"} {
		if err := conn.Store().Write(key, "txt", []byte(key)); err != nil {
			t.Fatal(err)
		}
		id := wrote(t, answers, key)
		if key == "spent" {
			spent = id
		}
	}

	if err := conn.Blob(spent).Drop(); err != nil {
		t.Fatal(err)
	}
	gone := answers.await(t, client.StoreGone)
	if key, _ := gone.Text("key"); key != "spent" {
		t.Errorf("the answer is about %q", key)
	}
	if id, _ := gone.Uint("blob"); id != spent {
		t.Errorf("the answer names blob %d, not the %d that was dropped", id, spent)
	}

	if err := conn.Store().List(); err != nil {
		t.Fatal(err)
	}
	if key, _ := answers.await(t, client.StoreBlob).Text("key"); key != "keep" {
		t.Errorf("the inventory still lists %q", key)
	}
	if count, _ := answers.await(t, client.StoreDone).Int("count"); count != 1 {
		t.Errorf("the store holds %d items after one of two was dropped", count)
	}

	// A dropped blob is not an object any more, so asking it anything is
	// refused outright rather than answered -- there is nothing left to answer.
	if err := conn.Blob(spent).Read(0); err == nil {
		t.Error("a dropped blob was still addressable")
	}
}

// A statement the store refuses is answered rather than dropped, and says which
// key it was about. The batch itself is fine: one bad key does not close the
// door on the rest of it.
func TestARefusedStatementIsAnswered(t *testing.T) {
	conn, answers := storeApp(t, "Refused App")

	for _, bad := range []struct{ key, typ string }{
		{"script", "sh"},          // not a type the desktop stores
		{"objects/figaro", "txt"}, /* a path, not a name */
		{"7", "txt"},              // a name that could be a record index
	} {
		if err := conn.Store().Write(bad.key, bad.typ, []byte("x")); err != nil {
			t.Fatal(err)
		}
		ev := answers.await(t, client.StoreError)
		if key, _ := ev.Text("key"); key != bad.key && bad.typ == "sh" {
			t.Errorf("the refusal is about %q, not %q", key, bad.key)
		}
		if reason, _ := ev.Text("reason"); reason == "" {
			t.Errorf("%q was refused without saying why", bad.key)
		}
	}

	if err := conn.Store().Write("script", "txt", []byte("ok")); err != nil {
		t.Fatal(err)
	}
	wrote(t, answers, "script")
}

// An app on the same machine keeps material too. A local app reaches the
// desktop over a unix socket, which is how mew's own app arrives, and a store
// only remote apps could use would be no use to the one using it most.
func TestALocalAppKeepsMaterialToo(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(display.KnownStoreEnv, filepath.Join(t.TempDir(), "known"))

	sock := filepath.Join(t.TempDir(), "d.sock")
	_, _, stop := startHost(t, display.Config{Endpoint: sock})
	defer stop()

	conn, err := client.Dial(sock, "Socket App", nil)
	if err != nil {
		t.Fatalf("dial unix: %v", err)
	}
	defer conn.Close()

	answers := newStoreAnswers()
	for _, ev := range []string{client.StoreBlob, client.StoreData, client.StoreDone, client.StoreError} {
		conn.OnStore(ev, answers.add)
	}
	if err := conn.Store().List(); err != nil {
		t.Fatal(err)
	}
	answers.await(t, client.StoreDone)

	want := []byte("kept by a local app")
	if err := conn.Store().Write("notes", "txt", want); err != nil {
		t.Fatal(err)
	}
	id := wrote(t, answers, "notes")
	if err := conn.Blob(id).Read(0); err != nil {
		t.Fatal(err)
	}
	got, _ := answers.await(t, client.StoreData).Blob("data")
	if !bytes.Equal(got, want) {
		t.Errorf("read back %q, want %q", got, want)
	}
}

// The store's answers are events like any other: an app that has not asked to
// hear them is not sent them.
func TestAnAppThatDidNotSubscribeHearsNothing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(display.KnownStoreEnv, filepath.Join(t.TempDir(), "known"))

	_, srv, stop := startHost(t, display.Config{
		Endpoint:    "tls://127.0.0.1:0",
		PromptLocal: true,
		Authorize:   func(display.AuthRequest) display.AuthDecision { return display.AuthAllowOnce },
	})
	defer stop()

	conn, err := client.Dial("tls://"+srv.Addr(), "Quiet App", nil)
	if err != nil {
		t.Fatalf("dial tls: %v", err)
	}
	defer conn.Close()

	answers := newStoreAnswers()
	// Registered on the connection's dispatcher WITHOUT subscribing, so
	// anything that arrives is something the desktop sent unasked.
	conn.OnType(client.StoreBlob, answers.add)
	conn.OnType(client.StoreDone, answers.add)
	conn.OnType(client.StoreData, answers.add)
	conn.OnType(client.StoreError, answers.add)

	if err := conn.Store().Write("notes", "txt", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := conn.Store().List(); err != nil {
		t.Fatal(err)
	}
	// Both statements have been executed and replied to by now; an answer
	// would have travelled ahead of the reply.
	answers.mu.Lock()
	defer answers.mu.Unlock()
	if len(answers.got) != 0 {
		t.Errorf("an app that subscribed to nothing was sent %d store answers", len(answers.got))
	}
}

// The store is not something the wire builds, and the vocabulary says so. It
// takes items, it raises the store's events, and `new store` is refused.
func TestTheStoreIsAddressedNotBuilt(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	_, _, stop := startHost(t, display.Config{Endpoint: sock})
	defer stop()

	conn, err := client.Dial(sock, "Reader App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Exec("s=new store"); err == nil {
		t.Error("`new store` was accepted; a connection arrives with the one it has")
	}

	vocab, err := conn.Describe()
	if err != nil {
		t.Fatal(err)
	}
	var store *wire.TypeInfo
	for i, ty := range vocab.Types {
		if ty.Name == "store" {
			store = &vocab.Types[i]
		}
	}
	if store == nil {
		t.Fatal("the vocabulary does not mention the store at all")
	}
	if !store.Hosted {
		t.Error("the vocabulary offers `new store`")
	}
	var props []string
	for _, p := range store.Props {
		props = append(props, p.Name)
	}
	if !contains(props, "blobs") {
		t.Errorf("the store does not describe what it holds; it describes %v", props)
	}
	// And what it can be ASKED, which is where a question belongs rather than
	// among the properties.
	var asks []string
	for _, a := range store.Asks {
		asks = append(asks, a.Name)
	}
	if !contains(asks, "inventory") {
		t.Errorf("the store answers no inventory question; it answers %v", asks)
	}
}
