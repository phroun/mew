package display_test

// An application saying its records moved, and the screen following.
//
// The one thing an application says FIRST. Everything else it writes answers a
// query the display put; this does not, because only the application knows its own
// records changed and nothing on the display's end can find out. Invalidation is
// told, never decided -- and both halves of that are here. Being told is ENOUGH:
// the notice crosses, reaches the source it names and no other, and the screen
// follows. And being told is NECESSARY: a settled view asks nothing, so a record
// that changes with nobody saying so is a record the display goes on showing the
// old version of.
//
// The second half is only observable because a cache sits under the application
// source. Without one the telling was a cycle -- a scope completing fires the
// arrival notice, the reader reads again, that read is another query -- turning
// over about twenty times a second for as long as the window was open, and
// picking up every change by accident. See appsources.go.

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

// settled waits for the display to stop asking, and says how many times it had
// asked when it stopped.
//
// A view reads several times while it finds its size, and those reads are not
// what this file is about. What matters is that it STOPS: an application source
// answers after its read returns, so the reader is told and reads again, and it is
// the cache under it that makes the second read an answer rather than a second
// question.
func settled(t *testing.T, rows *shifting) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	was := -1
	for time.Now().Before(deadline) {
		now := rows.reads()
		if now == was {
			return now
		}
		was = now
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("the display never stopped asking; it is at %d reads and still climbing",
		rows.reads())
	return 0
}

// **Both halves of the rule, in order.** A record changes and nothing happens,
// because nothing polls and nothing expires and the display cannot know. Then the
// application says so, and that alone is enough.
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

	// Nothing happens without a notice. The display has settled, so it is not
	// asking, and a change nobody mentions does not reach the screen.
	asked := settled(t, rows)
	rows.set(1, "renamed")
	time.Sleep(250 * time.Millisecond)
	if got := listRows(desktop, lv); len(got) > 1 && got[1] != "second" {
		t.Errorf("the row changed with nobody having said anything: %v", got)
	}
	if now := rows.reads(); now != asked {
		t.Errorf("the display asked again unprompted: %d reads became %d", asked, now)
	}

	// And being told is all it takes.
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

// A view over an application source SETTLES, and that is what makes everything
// above it true.
//
// An application answers after its read has returned, so the reader is told when
// records land and reads again. Nothing about that is optional: it is the only way
// a source across a connection can be read at all. What makes it terminate is the
// cache under the source -- the second read is served from what the first answer
// filed, so it touches no child, nothing lands, and nothing is told.
//
// Without it the telling was a cycle. This is the guard, and it is worth having as
// a test of its own because the failure is silent: everything still works, the
// screen is even more correct than it should be, and the only symptom is an
// application being asked the same question forever.
func TestAViewSettlesRatherThanAskingForever(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop, _, stop := servingDesktop(t, sock)
	defer stop()

	conn, err := client.Dial(sock, "Settling App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	rows := &shifting{rows: []string{"only"}}
	if _, err := conn.ProvideSource("papers", rows.fill); err != nil {
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
	waitForRow(t, desktop, lv, 0, "only")

	// It stops, and it stops SOON: a handful of reads while the view finds its
	// size, not one per round trip for as long as the window is open.
	asked := settled(t, rows)
	if asked > 8 {
		t.Errorf("the view asked %d times before settling", asked)
	}

	// And it stays settled. A second's worth of the old behaviour was about
	// twenty more reads.
	time.Sleep(time.Second)
	if now := rows.reads(); now != asked {
		t.Errorf("a settled view asked %d more times in a second", now-asked)
	}
}
