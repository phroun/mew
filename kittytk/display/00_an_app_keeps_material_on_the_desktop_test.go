package display_test

// An app on the far end of a real connection puts material on the desktop,
// asks what it has there, and reads it back -- receiving every answer the way
// it receives any other event.

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
// and so a shelf of its own) with every store answer being collected.
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

	answers := newStoreAnswers()
	for _, ev := range []string{client.StoreItem, client.StoreDone, client.StoreData, client.StoreError} {
		conn.OnStore(ev, answers.add)
	}
	return conn, answers
}

// The round trip: an app writes an item, is told what it now is, and reads
// back the bytes it wrote.
func TestAnAppWritesAnItemAndReadsItBack(t *testing.T) {
	conn, answers := storeApp(t, "Store App")
	want := []byte("(bundle: \"figaro\")")

	if err := conn.Data().Put("objects/figaro", "psl", want); err != nil {
		t.Fatal(err)
	}
	ev := answers.await(t, client.StoreItem)
	if key, _ := ev.Text("key"); key != "objects/figaro" {
		t.Errorf("the answer is about %q", key)
	}
	if typ, _ := ev.Word("type"); typ != "psl" {
		t.Errorf("the item is a %q", typ)
	}
	if size, _ := ev.Int("size"); size != len(want) {
		t.Errorf("the item is %d bytes, want %d", size, len(want))
	}

	if err := conn.Data().Get("objects/figaro", 0); err != nil {
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

	if err := conn.Cache().Put("blob", "bin", want); err != nil {
		t.Fatal(err)
	}
	answers.await(t, client.StoreItem)
	if err := conn.Cache().Get("blob", 0); err != nil {
		t.Fatal(err)
	}
	got, _ := answers.await(t, client.StoreData).Blob("data")
	if !bytes.Equal(got, want) {
		t.Errorf("%d of %d bytes came back", len(got), len(want))
	}
}

// Something too large for one statement is written in pieces and read back in
// pieces: the app appends until it has sent it all, then asks from where it
// has got to until the chunk marked last.
func TestALargeItemGoesAndComesBackInPieces(t *testing.T) {
	conn, answers := storeApp(t, "Big App")
	piece := bytes.Repeat([]byte("abcdefghij"), 300) // 3000 bytes, over one chunk
	var want []byte
	for i := 0; i < 3; i++ {
		if err := conn.Data().Append("big", "bin", piece); err != nil {
			t.Fatal(err)
		}
		answers.await(t, client.StoreItem)
		want = append(want, piece...)
	}

	var got []byte
	for chunks := 0; ; chunks++ {
		if err := conn.Data().Get("big", len(got)); err != nil {
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

// The inventory: one answer per item saying what it is and how big, then one
// saying that was all of them.
func TestAnAppAsksWhatItHasStored(t *testing.T) {
	conn, answers := storeApp(t, "Inventory App")
	for _, it := range []struct {
		key, typ string
		data     string
	}{
		{"notes", "txt", "hello"},
		{"settings", "ini", "[a]\nb=1\n"},
	} {
		if err := conn.Data().Put(it.key, it.typ, []byte(it.data)); err != nil {
			t.Fatal(err)
		}
		answers.await(t, client.StoreItem)
	}
	// The other tree is its own shelf: what is cached is not what is kept.
	if err := conn.Cache().Put("scratch", "txt", []byte("x")); err != nil {
		t.Fatal(err)
	}
	answers.await(t, client.StoreItem)

	if err := conn.Data().List(); err != nil {
		t.Fatal(err)
	}
	found := map[string]int{}
	for i := 0; i < 2; i++ {
		ev := answers.await(t, client.StoreItem)
		key, _ := ev.Text("key")
		size, _ := ev.Int("size")
		found[key] = size
	}
	done := answers.await(t, client.StoreDone)
	if count, _ := done.Int("count"); count != 2 {
		t.Errorf("the inventory says %d items, want the 2 in this tree", count)
	}
	if found["notes"] != 5 || found["settings"] != 8 {
		t.Errorf("the inventory reads as %v", found)
	}
	if _, listed := found["scratch"]; listed {
		t.Error("the data inventory listed what is in the cache")
	}
}

// A statement the store refuses is answered rather than dropped, and says
// which key it was about.
func TestARefusedStatementIsAnswered(t *testing.T) {
	conn, answers := storeApp(t, "Refused App")

	if err := conn.Data().Put("script", "sh", []byte("rm -rf /")); err != nil {
		t.Fatal(err)
	}
	ev := answers.await(t, client.StoreError)
	if key, _ := ev.Text("key"); key != "script" {
		t.Errorf("the refusal is about %q", key)
	}
	if reason, _ := ev.Text("reason"); reason == "" {
		t.Error("the refusal says nothing about what was wrong")
	}

	// And the batch itself was fine: one bad key does not close the door.
	if err := conn.Data().Put("script", "txt", []byte("ok")); err != nil {
		t.Fatal(err)
	}
	answers.await(t, client.StoreItem)
}

// Asking for a key nothing is filed under is answered too, rather than leaving
// the app waiting for a chunk that is never coming.
func TestAskingForWhatIsNotThereIsAnswered(t *testing.T) {
	conn, answers := storeApp(t, "Missing App")
	if err := conn.Data().Get("never stored", 0); err != nil {
		t.Fatal(err)
	}
	if key, _ := answers.await(t, client.StoreError).Text("key"); key != "never stored" {
		t.Errorf("the refusal is about %q", key)
	}
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
	for _, ev := range []string{client.StoreItem, client.StoreData, client.StoreError} {
		conn.OnStore(ev, answers.add)
	}

	want := []byte("kept by a local app")
	if err := conn.Data().Put("notes", "txt", want); err != nil {
		t.Fatal(err)
	}
	if size, _ := answers.await(t, client.StoreItem).Int("size"); size != len(want) {
		t.Errorf("the item is %d bytes, want %d", size, len(want))
	}
	if err := conn.Data().Get("notes", 0); err != nil {
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
	conn.OnType(client.StoreItem, answers.add)
	conn.OnType(client.StoreDone, answers.add)
	conn.OnType(client.StoreData, answers.add)
	conn.OnType(client.StoreError, answers.add)

	if err := conn.Data().Put("notes", "txt", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := conn.Data().List(); err != nil {
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
