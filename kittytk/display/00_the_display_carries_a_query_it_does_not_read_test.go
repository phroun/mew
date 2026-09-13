package display_test

// A query put to one application by another, carried by the display.
//
// The display has no orchestrated reason to open a query yet, and does not
// need one to carry the question: `ask host relay` hands the statements over
// and sends back whatever comes. So the whole reverse direction can be driven
// over a real connection -- an application serving, a display routing, a tool
// asking -- before the piece that decides WHEN to ask exists.
//
// It is off unless the display opens it, and that is tested here too: an
// application that can relay can address another application's objects.

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/wire"
)

// servedRelay runs a headless desktop with the debug relay open, and returns
// the socket it is on.
func servedRelay(t *testing.T) string {
	t.Helper()
	_, srv, sock, done := servedDesktop(t)
	t.Cleanup(done)
	srv.SetRelayEnabled(true)
	return sock
}

// served is a connection whose application serves one source of three records.
func served(t *testing.T, sock string) *client.Conn {
	t.Helper()
	conn := dialSocket(t, sock, "servingapp")
	rows := []struct {
		key  int64
		name string
	}{{1, "alpha"}, {2, "beta"}, {3, "gamma"}}
	if _, err := conn.HostSource("letters", func(f *client.Fill) {
		f.Ordered()
		for _, r := range rows {
			if f.Need > 0 && f.Sent() >= f.Need {
				break
			}
			_ = f.Record(r.key, wire.Named("name", r.name))
		}
		_ = f.Exhausted()
	}); err != nil {
		t.Fatal(err)
	}
	return conn
}

// relayed asks the display to carry text to servingapp and collects the
// statements that come back.
func relayed(t *testing.T, conn *client.Conn, text string) ([]string, error) {
	t.Helper()
	var mu sync.Mutex
	var lines []string
	done := make(chan struct{})
	var once sync.Once

	conn.OnHost(client.EventRelay, func(ev *wire.Event) {
		s, ok := ev.Text("text")
		if !ok {
			return
		}
		mu.Lock()
		lines = append(lines, s)
		mu.Unlock()
		if strings.Contains(s, " complete") {
			once.Do(func() { close(done) })
		}
	})
	if err := conn.Host().Ask("relay to=\"servingapp\" text=" + wire.Quote(text)); err != nil {
		return nil, err
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the answer never completed")
	}
	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), lines...), nil
}

func TestTheDisplayCarriesAQueryItDoesNotRead(t *testing.T) {
	sock := servedRelay(t)

	app := served(t, sock)
	defer app.Close()
	tool := dialSocket(t, sock, "asking tool")
	defer tool.Close()

	lines, err := relayed(t, tool, `q=new query source="letters" sort={ name natural } have=0 need=2`)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(lines, "\n")

	// The application named the query, and the reply carrying that name came
	// back before anything that uses it.
	if !strings.HasPrefix(got, "reply q=") {
		t.Fatalf("the first thing back was not the reply:\n%s", got)
	}
	for _, want := range []string{
		`result 1 fields={ key 1; name "alpha" }`,
		`result 1 fields={ key 2; name "beta" }`,
		"result 1 complete ordered exhausted",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing:\n  %s\nin:\n%s", want, got)
		}
	}
	// It was asked for two and sent two: the window was honoured.
	if n := strings.Count(got, "result 1 fields="); n != 2 {
		t.Errorf("%d records came back, want 2:\n%s", n, got)
	}
}

// A refusal comes back the same way an answer does.
func TestARelayedRefusalComesBack(t *testing.T) {
	sock := servedRelay(t)

	app := served(t, sock)
	defer app.Close()
	tool := dialSocket(t, sock, "asking tool")
	defer tool.Close()

	var mu sync.Mutex
	var got string
	seen := make(chan struct{})
	var once sync.Once
	tool.OnHost(client.EventRelay, func(ev *wire.Event) {
		if s, ok := ev.Text("text"); ok {
			mu.Lock()
			got = s
			mu.Unlock()
			once.Do(func() { close(seen) })
		}
	})
	if err := tool.Host().Ask(`relay to="servingapp" text="q=new query source=\"ledgers\" have=0 need=1"`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-seen:
	case <-time.After(5 * time.Second):
		t.Fatal("nothing came back")
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.HasPrefix(got, "error text=") || !strings.Contains(got, "ledgers") {
		t.Errorf("the refusal came back as %q", got)
	}
}

// The relay is shut unless the display opens it, and says so.
func TestTheRelayIsShutUnlessTheDisplayOpensIt(t *testing.T) {
	_, srv, sock, done := servedDesktop(t)
	t.Cleanup(done)
	if srv.RelayEnabled() {
		t.Fatal("the relay was open with nothing opening it")
	}

	app := served(t, sock)
	defer app.Close()
	tool := dialSocket(t, sock, "asking tool")
	defer tool.Close()

	err := tool.Host().Ask(`relay to="servingapp" text="q=new query source=\"letters\" have=0 need=1"`)
	if err == nil || !strings.Contains(err.Error(), "not open") {
		t.Errorf("relaying with the relay shut read %v", err)
	}
}

// Relaying to something that is not connected is refused, by name.
func TestRelayingToNobodyIsRefused(t *testing.T) {
	sock := servedRelay(t)

	tool := dialSocket(t, sock, "asking tool")
	defer tool.Close()

	err := tool.Host().Ask(`relay to="nobody" text="q=new query source=\"x\" have=0 need=1"`)
	if err == nil || !strings.Contains(err.Error(), "nobody") {
		t.Errorf("relaying to nobody read %v", err)
	}
}

// The display does not read what it carries, but it does refuse text that is
// not the language at all -- there is nothing to hand over otherwise.
func TestTheDisplayRefusesTextThatIsNotTheLanguage(t *testing.T) {
	sock := servedRelay(t)

	app := served(t, sock)
	defer app.Close()
	tool := dialSocket(t, sock, "asking tool")
	defer tool.Close()

	err := tool.Host().Ask(`relay to="servingapp" text="q=new query source={ unterminated"`)
	if err == nil {
		t.Error("text that will not parse was carried anyway")
	}
}
