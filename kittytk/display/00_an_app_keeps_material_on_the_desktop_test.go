package display_test

// An app on the far end of a real connection puts material in its store, asks
// what it has there, and reads it back -- through the protocol's own verbs.
//
// Both flows are here, and the difference between them is the point. Asking what
// the store holds, or for a blob's bytes, is a QUESTION: it is answered, the
// answers quote the key the ask carried, and nothing need have subscribed. Writing
// a blob is not a question, so what the blob now is arrives as a store_blob event,
// which an app hears only if it asked to -- and which is where the id to continue a
// large write with comes from.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/wire"
)

// storeAnnouncements collects the events one connection's store raises: a blob
// written, one dropped, one refused. The inventory is not among them.
type storeAnnouncements struct {
	mu   sync.Mutex
	got  []*wire.Event
	came chan struct{}
}

func newStoreAnnouncements() *storeAnnouncements {
	return &storeAnnouncements{came: make(chan struct{}, 256)}
}

func (a *storeAnnouncements) add(ev *wire.Event) {
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
func (a *storeAnnouncements) await(t *testing.T, typ string) *wire.Event {
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
			t.Fatalf("the store never said %s", typ)
		}
	}
}

// inventory asks what the store holds and hands back every answer, the completion
// last. It waits for that completion, which arrives whether or not a blob came
// before it -- so an empty store is something to read rather than something to
// time out on.
func inventory(t *testing.T, conn *client.Conn) []*wire.Answer {
	t.Helper()
	held := newGathered()
	if err := conn.Store().List(held.take); err != nil {
		t.Fatalf("asking what the store holds: %v", err)
	}
	return held.wait(t)
}

// stored is the inventory read as key to size, and how many it said there were.
func stored(t *testing.T, conn *client.Conn) (map[string]int, int) {
	t.Helper()
	answers := inventory(t, conn)
	sizes := map[string]int{}
	for _, ans := range answers[:len(answers)-1] {
		key, _ := ans.Text("key")
		size, _ := ans.Int("size")
		sizes[key] = size
	}
	count, _ := answers[len(answers)-1].Int("count")
	return sizes, count
}

// chunk is the slice of a blob that starts at offset.
//
// ONE answer, which is the whole of it: a read asks for the slice that starts
// here, and reading further on is a fresh question asked from where this one
// reached.
func chunk(t *testing.T, conn *client.Conn, id uint64, offset int) *wire.Answer {
	t.Helper()
	read := newGathered()
	if err := conn.Blob(id).Read(offset, read.take); err != nil {
		t.Fatalf("reading from %d: %v", offset, err)
	}
	answers := read.wait(t)
	if len(answers) != 1 {
		t.Fatalf("a read was answered %d times; one chunk completes the question", len(answers))
	}
	if answers[0].Error != "" {
		t.Fatalf("reading from %d was refused: %s", offset, answers[0].Error)
	}
	return answers[0]
}

// storeApp dials a host over tls (which is what gives the client an identity, and
// so a store of its own), subscribes to what the store announces, and asks what is
// in it -- so a test starts from a store it has been told is empty.
func storeApp(t *testing.T, name string) (*client.Conn, *storeAnnouncements) {
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

	said := newStoreAnnouncements()
	for _, ev := range []string{client.StoreBlob, client.StoreGone, client.StoreError} {
		conn.OnStore(ev, said.add)
	}
	if _, count := stored(t, conn); count != 0 {
		t.Fatalf("a fresh store already held %d blobs", count)
	}
	return conn, said
}

// wrote waits for the store's report of a write and hands back the blob's id,
// which is what everything afterwards addresses it by.
func wrote(t *testing.T, said *storeAnnouncements, key string) uint64 {
	t.Helper()
	ev := said.await(t, client.StoreBlob)
	if got, _ := ev.Text("key"); got != key {
		t.Fatalf("the store spoke about %q, not %q", got, key)
	}
	id, ok := ev.Uint("blob")
	if !ok || id == 0 {
		t.Fatalf("what the store said about %q names no blob to address it by: %s", key, ev.Encode())
	}
	return id
}

// The round trip: an app writes a blob, is told what it now is and how to
// address it, and reads back the bytes it wrote.
func TestAnAppWritesAnItemAndReadsItBack(t *testing.T) {
	conn, said := storeApp(t, "Store App")
	want := []byte("(bundle: \"figaro\")")

	if err := conn.Store().Write("figaro", "psl", want); err != nil {
		t.Fatal(err)
	}
	ev := said.await(t, client.StoreBlob)
	if key, _ := ev.Text("key"); key != "figaro" {
		t.Errorf("the store spoke about %q", key)
	}
	if typ, _ := ev.Word("type"); typ != "psl" {
		t.Errorf("the blob is a %q", typ)
	}
	if size, _ := ev.Int("size"); size != len(want) {
		t.Errorf("the blob is %d bytes, want %d", size, len(want))
	}
	id, _ := ev.Uint("blob")
	if id == 0 {
		t.Fatal("what the store said names no blob id, so nothing can be done to it")
	}

	one := chunk(t, conn, id, 0)
	got, _ := one.Blob("data")
	if !bytes.Equal(got, want) {
		t.Errorf("read back %q, want %q", got, want)
	}
	if one.Flag("last") != wire.FlagTrue {
		t.Error("a blob that fits in one chunk did not come back as the last one")
	}
}

// Bytes are bytes. A payload holding every value there is comes back holding
// every value there is, which a text encoding of it would not.
func TestBinaryMaterialSurvivesTheWire(t *testing.T) {
	conn, said := storeApp(t, "Binary App")
	want := make([]byte, 256)
	for i := range want {
		want[i] = byte(i)
	}

	if err := conn.Store().Write("blob", "bin", want); err != nil {
		t.Fatal(err)
	}
	id := wrote(t, said, "blob")
	got, _ := chunk(t, conn, id, 0).Blob("data")
	if !bytes.Equal(got, want) {
		t.Errorf("%d of %d bytes came back", len(got), len(want))
	}
}

// Something too large for one statement is written in pieces and read back in
// pieces: the app feeds the blob it made until it has sent it all, then reads
// from where it has got to until the chunk marked last. Each read is its own
// question, and the answer to it completes.
func TestALargeItemGoesAndComesBackInPieces(t *testing.T) {
	conn, said := storeApp(t, "Big App")
	piece := bytes.Repeat([]byte("abcdefghij"), 300) // 3000 bytes, over one chunk

	if err := conn.Store().Write("big", "bin", piece); err != nil {
		t.Fatal(err)
	}
	id := wrote(t, said, "big")
	want := append([]byte(nil), piece...)
	for i := 0; i < 2; i++ {
		if err := conn.Blob(id).Append(piece); err != nil {
			t.Fatal(err)
		}
		wrote(t, said, "big")
		want = append(want, piece...)
	}

	var got []byte
	for chunks := 0; ; chunks++ {
		ans := chunk(t, conn, id, len(got))
		if at, _ := ans.Int("offset"); at != len(got) {
			t.Fatalf("chunk %d starts at %d; the app had %d bytes", chunks, at, len(got))
		}
		if size, _ := ans.Int("size"); size != len(want) {
			t.Errorf("chunk %d says the blob is %d bytes; it is %d", chunks, size, len(want))
		}
		data, _ := ans.Blob("data")
		got = append(got, data...)
		if ans.Flag("last") == wire.FlagTrue {
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
// address it, then one saying that was all of them and how many there were.
//
// It is asked for rather than subscribed to, and that is why it can be answered at
// all: a run of events ending in a second event type could only reach an app that
// had subscribed to both before asking, and the end of the run was exactly what it
// had not got to yet.
func TestAnAppAsksWhatItHasStored(t *testing.T) {
	conn, said := storeApp(t, "Inventory App")
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
		wrote(t, said, it.key)
	}

	found, count := stored(t, conn)
	if count != 3 {
		t.Errorf("the inventory says %d blobs, want 3", count)
	}
	// One inventory, both halves: what is cached is named alongside what is
	// kept, and the mark on the key is what tells them apart.
	if found["notes"] != 5 || found["settings"] != 8 || found[client.CacheMark+"scratch"] != 1 {
		t.Errorf("the inventory reads as %v", found)
	}
}

// Every answer quotes the question it is answering, which is what lets an app put
// two questions and know which is which. Answers-as-events could not: an event
// says its type and its source, and two reads of two blobs are the same type from
// the same store.
func TestTwoQuestionsAtOnceAreToldApart(t *testing.T) {
	conn, said := storeApp(t, "Two Questions App")
	for _, it := range []struct{ key, data string }{
		{"first", "one"},
		{"second", "two and a bit"},
	} {
		if err := conn.Store().Write(it.key, "txt", []byte(it.data)); err != nil {
			t.Fatal(err)
		}
		wrote(t, said, it.key)
	}
	ids := map[string]uint64{}
	for _, ans := range inventory(t, conn) {
		if key, ok := ans.Text("key"); ok {
			ids[key], _ = ans.Uint("blob")
		}
	}
	if len(ids) != 2 {
		t.Fatalf("the inventory named %v", ids)
	}

	// Both questions go before either is answered, so nothing but the key they
	// quote could sort the answers out.
	one, two := newGathered(), newGathered()
	if err := conn.Blob(ids["first"]).Read(0, one.take); err != nil {
		t.Fatal(err)
	}
	if err := conn.Blob(ids["second"]).Read(0, two.take); err != nil {
		t.Fatal(err)
	}

	first, second := one.wait(t), two.wait(t)
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("the two reads were answered %d and %d times", len(first), len(second))
	}
	if first[0].To == second[0].To {
		t.Fatalf("both answers quote %q, so nothing tells them apart", first[0].To)
	}
	if got, _ := first[0].Blob("data"); string(got) != "one" {
		t.Errorf("the first read came back with %q", got)
	}
	if got, _ := second[0].Blob("data"); string(got) != "two and a bit" {
		t.Errorf("the second read came back with %q", got)
	}
}

// The mark on a key is the only difference, and it decides which of the two
// directories the bytes sit in -- so Clear Cache can take one and leave the
// other, and the same name unmarked is a different blob.
func TestTheCacheMarkDecidesWhereTheBytesGo(t *testing.T) {
	conn, said := storeApp(t, "Marked App")

	if err := conn.Store().Write("report", "txt", []byte("kept")); err != nil {
		t.Fatal(err)
	}
	wrote(t, said, "report")
	if err := conn.Store().Write(client.CacheMark+"report", "txt", []byte("cached")); err != nil {
		t.Fatal(err)
	}
	cachedID := wrote(t, said, client.CacheMark+"report")

	// Two blobs, not one written twice.
	if _, count := stored(t, conn); count != 2 {
		t.Errorf("a marked key and an unmarked one came to %d blobs, want 2", count)
	}

	// And the marked one holds what was written to it, not the other's bytes.
	got, _ := chunk(t, conn, cachedID, 0).Blob("data")
	if string(got) != "cached" {
		t.Errorf("the marked blob reads as %q", got)
	}
}

// An app can take a blob out of its store with the verb that already means that.
// Without it a store only ever grows, and the app that filled it has no way to
// empty it.
func TestAnAppTakesAnItemOutOfItsStore(t *testing.T) {
	conn, said := storeApp(t, "Tidy App")

	var spent uint64
	for _, key := range []string{"keep", "spent"} {
		if err := conn.Store().Write(key, "txt", []byte(key)); err != nil {
			t.Fatal(err)
		}
		id := wrote(t, said, key)
		if key == "spent" {
			spent = id
		}
	}

	if err := conn.Blob(spent).Drop(); err != nil {
		t.Fatal(err)
	}
	gone := said.await(t, client.StoreGone)
	if key, _ := gone.Text("key"); key != "spent" {
		t.Errorf("the store spoke about %q", key)
	}
	if id, _ := gone.Uint("blob"); id != spent {
		t.Errorf("it names blob %d, not the %d that was dropped", id, spent)
	}

	found, count := stored(t, conn)
	if count != 1 {
		t.Errorf("the store holds %d blobs after one of two was dropped", count)
	}
	if _, ok := found["keep"]; !ok || len(found) != 1 {
		t.Errorf("the inventory reads as %v", found)
	}

	// A dropped blob is not an object any more, so asking it anything is
	// refused outright rather than answered -- there is nothing left to answer.
	if err := conn.Blob(spent).Read(0, func(*wire.Answer) {}); err == nil {
		t.Error("a dropped blob was still addressable")
	}
}

// A statement the store refuses is announced rather than dropped, and says which
// key it was about. The batch itself is fine: one bad key does not close the door
// on the rest of it.
func TestARefusedStatementIsAnswered(t *testing.T) {
	conn, said := storeApp(t, "Refused App")

	for _, bad := range []struct{ key, typ string }{
		{"script", "sh"},          // not a type the desktop stores
		{"objects/figaro", "txt"}, /* a path, not a name */
	} {
		if err := conn.Store().Write(bad.key, bad.typ, []byte("x")); err != nil {
			t.Fatal(err)
		}
		ev := said.await(t, client.StoreError)
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
	wrote(t, said, "script")
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

	said := newStoreAnnouncements()
	for _, ev := range []string{client.StoreBlob, client.StoreError} {
		conn.OnStore(ev, said.add)
	}
	if _, count := stored(t, conn); count != 0 {
		t.Fatalf("a fresh store already held %d blobs", count)
	}

	want := []byte("kept by a local app")
	if err := conn.Store().Write("notes", "txt", want); err != nil {
		t.Fatal(err)
	}
	id := wrote(t, said, "notes")
	got, _ := chunk(t, conn, id, 0).Blob("data")
	if !bytes.Equal(got, want) {
		t.Errorf("read back %q, want %q", got, want)
	}
}

// The line between the two flows, stated as the one thing that distinguishes
// them: a subscription is needed to HEAR, and not to be ANSWERED.
//
// So an app that subscribed to nothing is not told about a write -- that is the
// store's own report, and D19 sends an event only to whoever asked for it. But its
// own question is answered regardless, because it asked: an answer belongs to one
// question and has no subscription filter to pass. That is what made answering
// with events wrong, and it is the whole of what changed.
func TestAnAnswerNeedsNoSubscriptionButAnEventDoes(t *testing.T) {
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

	said := newStoreAnnouncements()
	// Registered on the connection's dispatcher WITHOUT subscribing, so anything
	// that arrives is something the desktop sent unasked.
	conn.OnType(client.StoreBlob, said.add)
	conn.OnType(client.StoreGone, said.add)
	conn.OnType(client.StoreError, said.add)

	if err := conn.Store().Write("notes", "txt", []byte("hello")); err != nil {
		t.Fatal(err)
	}

	// The question, which is answered -- and by the time it completes the write's
	// statement has been executed and replied to, so an event for it would have
	// travelled already.
	found, count := stored(t, conn)
	if count != 1 || found["notes"] != 5 {
		t.Errorf("the inventory reads as %v (%d)", found, count)
	}

	said.mu.Lock()
	defer said.mu.Unlock()
	if len(said.got) != 0 {
		t.Errorf("an app that subscribed to nothing was told %d things by its store", len(said.got))
	}
}

// The store is not something the wire builds, and the vocabulary says so. It
// takes blobs, it raises the store's events, and `new store` is refused.
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
	// The events it describes are the ones it RAISES, and only those. A client
	// subscribes from this list, so a name on it that nothing ever says is a
	// subscription that waits for good.
	var events []string
	for _, ev := range store.Events {
		events = append(events, ev.Name)
	}
	sort.Strings(events)
	if got := strings.Join(events, ","); got != "store_blob,store_error,store_gone" {
		t.Errorf("the store describes %q", got)
	}
}

// The inventory carries each blob's hash, which is what an app compares against
// its own copy to decide whether it needs to upload at all. That is the whole
// point of enumerating: read the list, work out what differs, send only that.
func TestTheInventoryCarriesAHashToCompareAgainst(t *testing.T) {
	conn, said := storeApp(t, "Comparing App")
	const notes = "hello"
	if err := conn.Store().Write("notes", "txt", []byte(notes)); err != nil {
		t.Fatal(err)
	}
	wrote(t, said, "notes")

	// An app appending builds the blob up, and one part way through has no hash to
	// give: size says how much has landed, and that is what a resume needs.
	if err := conn.Store().Write("log", "txt", []byte("one ")); err != nil {
		t.Fatal(err)
	}
	id := wrote(t, said, "log")
	if err := conn.Blob(id).Append([]byte("two")); err != nil {
		t.Fatal(err)
	}
	wrote(t, said, "log")

	found := map[string]string{}
	for _, ans := range inventory(t, conn) {
		if ans.Complete {
			continue
		}
		key, _ := ans.Text("key")
		hash, _ := ans.Text("hash")
		found[key] = hash
	}

	// The app hashes its own copy the same way and the two agree, so it knows
	// it has nothing to send.
	want := sha256.Sum256([]byte(notes))
	if got := found["notes"]; got != hex.EncodeToString(want[:]) {
		t.Errorf("the inventory says %q, and the app's own copy hashes to %x", got, want)
	}
	// The appended blob was settled by the inventory itself, over the whole of it
	// rather than over the piece that arrived last.
	whole := sha256.Sum256([]byte("one two"))
	if got := found["log"]; got != hex.EncodeToString(whole[:]) {
		t.Errorf("the appended blob reads as %q", got)
	}
}
