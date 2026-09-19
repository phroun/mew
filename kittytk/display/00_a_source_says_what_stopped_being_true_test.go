package display_test

// An application saying its records moved, and the screen following.
//
// The one thing an application says FIRST. Everything else it writes answers a
// query the display put; this does not, because only the application knows its own
// records changed and nothing on the display's end can find out. Invalidation is
// told, never decided -- so what is tested here is that being told is ENOUGH: the
// notice crosses, it reaches the source it names and no other, and the screen
// follows.
//
// The other half of the rule -- that nothing happens until it is told -- cannot be
// asserted from here yet, because a view over an application source re-reads
// continuously: a scope completing fires the arrival notice, the reader reads
// again, that read is another query, and with no cache in the chain to serve it
// there is nothing to break the cycle. About twenty round trips a second, for as
// long as the window is open. It is not this file's to fix, and what it costs here
// is that "the display did not ask" is not a thing this level can observe. The
// source package asserts it where it holds.

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/serval"
)

// shifting is a body of records the application can change under the display.
type shifting struct {
	mu   sync.Mutex
	rows []string
	read int // how many times the display has asked
}

func (s *shifting) fill(f *client.Fill) {
	s.mu.Lock()
	rows := append([]string(nil), s.rows...)
	s.read++
	s.mu.Unlock()
	for i, name := range rows {
		_ = f.Record(serval.NewInt(int64(i)), serval.Named("name", name))
	}
	_ = f.Exhausted()
}

func (s *shifting) set(at int, name string) {
	s.mu.Lock()
	s.rows[at] = name
	s.mu.Unlock()
}

func (s *shifting) reads() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read
}

// listRows is what the list is showing, read on the UI thread.
func listRows(desktop *trinkets.Desktop, lv *trinkets.ListView) []string {
	var out []string
	onUI(desktop, func() {
		for i := 0; i < lv.Count(); i++ {
			if item := lv.Item(i); item != nil {
				out = append(out, item.Text)
			}
		}
	})
	return out
}

// waitForRow waits for a row to read as want, and says what it read if it never
// does. Time passing is not the mechanism -- the notice is -- but the notice
// crosses a socket, so the assertion has to allow for the crossing.
func waitForRow(t *testing.T, desktop *trinkets.Desktop, lv *trinkets.ListView, at int, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var got []string
	for time.Now().Before(deadline) {
		got = listRows(desktop, lv)
		if at < len(got) && got[at] == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("row %d never read as %q; the list shows %v", at, want, got)
}

// A record changes at the application, the application says so, and the row
// follows -- with nothing on the display's end having asked.
func TestARecordChangesAndTheDisplayIsTold(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop, _, stop := servingDesktop(t, sock)
	defer stop()

	conn, err := client.Dial(sock, "Shifting App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	rows := &shifting{rows: []string{"first", "second", "third"}}
	src, err := conn.ProvideSource("papers", rows.fill)
	if err != nil {
		t.Fatalf("providing: %v", err)
	}
	if _, err := conn.Build(`
w=new window title="Papers" width=320 height=200 children={
	lv=new listview source="source:papers" display="name"
}
`); err != nil {
		t.Fatalf("build: %v", err)
	}

	lv := waitForList(t, desktop, 5*time.Second)
	waitForRow(t, desktop, lv, 1, "second")

	// The application changes a record and says so, and that is all it takes.
	rows.set(1, "renamed")
	if err := src.Stale(serval.NewInt(1), serval.Altered, "name"); err != nil {
		t.Fatalf("saying so: %v", err)
	}
	waitForRow(t, desktop, lv, 1, "renamed")
}

// A notice naming no record is about every record of the source, which is the
// widest thing a source can say and the one it says when it cannot tell what
// moved. Coarsening is always safe.
func TestANoticeNamingNoRecordIsAboutAllOfThem(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop, _, stop := servingDesktop(t, sock)
	defer stop()

	conn, err := client.Dial(sock, "Sweeping App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	rows := &shifting{rows: []string{"one", "two"}}
	src, err := conn.ProvideSource("papers", rows.fill)
	if err != nil {
		t.Fatalf("providing: %v", err)
	}
	if _, err := conn.Build(`
w=new window title="Papers" width=320 height=200 children={
	lv=new listview source="source:papers" display="name"
}
`); err != nil {
		t.Fatalf("build: %v", err)
	}
	lv := waitForList(t, desktop, 5*time.Second)
	waitForRow(t, desktop, lv, 0, "one")

	rows.set(0, "ONE")
	rows.set(1, "TWO")
	if err := src.Stale(nil, serval.Replaced); err != nil {
		t.Fatalf("saying so: %v", err)
	}
	waitForRow(t, desktop, lv, 0, "ONE")
	waitForRow(t, desktop, lv, 1, "TWO")
}

// A notice is something the application SAYS, not something it asks for, so it
// neither runs against the display nor is replied to.
//
// Getting that wrong is not quiet: before `stale` was on the list of things an
// application says, the statement was read as a request, refused as an unknown
// verb, and the refusal came back on the next thing the application tried to do.
// `place` was in the same position and is now on the list too.
func TestANoticeIsSaidRatherThanAsked(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop, _, stop := servingDesktop(t, sock)
	defer stop()

	conn, err := client.Dial(sock, "Two Source App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	papers := &shifting{rows: []string{"kept"}}
	if _, err := conn.ProvideSource("papers", papers.fill); err != nil {
		t.Fatalf("providing: %v", err)
	}
	// A second source, so the notice below names one that is not on screen.
	ledgers, err := conn.ProvideSource("ledgers", (&shifting{rows: []string{"other"}}).fill)
	if err != nil {
		t.Fatalf("providing: %v", err)
	}
	if _, err := conn.Build(`
w=new window title="Papers" width=320 height=200 children={
	lv=new listview source="source:papers" display="name"
}
`); err != nil {
		t.Fatalf("build: %v", err)
	}
	lv := waitForList(t, desktop, 5*time.Second)
	waitForRow(t, desktop, lv, 0, "kept")

	if err := ledgers.Stale(nil, serval.Replaced); err != nil {
		t.Fatalf("saying so: %v", err)
	}
	// Nothing comes back from a notice, so what proves it was not taken for a
	// request is the next thing the application asks going through.
	if _, err := conn.Exec("describe"); err != nil {
		t.Errorf("the connection did not survive a notice: %v", err)
	}
}
