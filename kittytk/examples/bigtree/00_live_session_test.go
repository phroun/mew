package main

// A headless run of the whole app against a real display service.
//
// It is here because the app is meant to be RUN, and nothing about "it compiles"
// says the script parses, the names surface, the bundle is accepted, or a hundred
// thousand records make it across. So the test stands up the same service
// kittytk-tui serves, calls `start` -- the binary's own startup, not a copy of it
// -- and then names each source at the tree and reads what came back.
//
// This test imports the display service and trinkets; the bigtree binary does not.

import (
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/style"
)

// startService stands up a headless desktop serving on a socket.
func startService(t *testing.T) (sock string, desktop *trinkets.Desktop, stop func()) {
	t.Helper()
	sock = filepath.Join(t.TempDir(), "display.sock")

	desktop = trinkets.NewDesktop()
	desktop.SetBackend(&nullBackend{})
	desktop.SetOnStartup(func() {
		if _, err := display.Serve(desktop, sock); err != nil {
			t.Errorf("serve: %v", err)
			desktop.Quit()
		}
	})

	exited := make(chan int, 1)
	go func() { exited <- desktop.Run() }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if c, err := (&net.Dialer{}).Dial("unix", sock); err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("display service did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	return sock, desktop, func() {
		desktop.Quit()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			t.Error("desktop did not exit")
		}
	}
}

// onUI runs a function on the thread that owns the trinkets and waits for it,
// because everything this test reads belongs to that thread.
func onUI(desktop *trinkets.Desktop, fn func()) {
	done := make(chan struct{})
	desktop.Post(func() {
		defer close(done)
		fn()
	})
	<-done
}

// theTree finds the app's treeview on the desktop.
func theTree(t *testing.T, desktop *trinkets.Desktop) *trinkets.TreeView {
	t.Helper()
	var found *trinkets.TreeView
	var look func(w core.Trinket)
	look = func(w core.Trinket) {
		if w == nil {
			return
		}
		if tv, ok := w.(*trinkets.TreeView); ok {
			found = tv
			return
		}
		if p, ok := w.(*trinkets.Panel); ok {
			for _, kid := range p.Children() {
				look(kid)
			}
		}
	}
	onUI(desktop, func() {
		for _, a := range desktop.Applications() {
			for _, win := range a.Windows() {
				look(win.Content())
			}
		}
	})
	if found == nil {
		t.Fatal("no treeview on the desktop")
	}
	return found
}

// topRows waits for the tree's top level to hold what is wanted, and hands back
// what it holds.
//
// **Waiting rather than asking once**, because a tree over an application's records
// walks on a thread of its own and tells when it is done: the rows exist at the
// moment the walk finishes, which is not the moment the source was named. Polling
// here stands in for the frames a real display would draw.
func topRows(t *testing.T, desktop *trinkets.Desktop, tv *trinkets.TreeView, want int) []*trinkets.TreeItem {
	t.Helper()
	var items []*trinkets.TreeItem
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		onUI(desktop, func() { items = tv.RootItems() })
		if len(items) == want {
			return items
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the tree's top level holds %d rows, want %d", len(items), want)
	return nil
}

func TestBigTreeRunsOverTheService(t *testing.T) {
	sock, desktop, stop := startService(t)
	defer stop()

	a, err := start(sock)
	if err != nil {
		t.Fatalf("starting: %v", err)
	}
	defer a.conn.Close()

	// Every name the wiring addresses has to be one the script surfaced. A name
	// that is not resolves to id 0, and id 0 is quiet: nothing it sets or
	// subscribes happens, so a whole control row can be wired to nothing and
	// merely look inert.
	for _, name := range []string{
		"w", "tree", "said", "bflat", "bdeep",
		"sizec", "kindc", "modc", "tagsc", "rawc",
		"showkey", "hscroll", "pinl", "pinr", "ledger", "lines",
	} {
		if a.ui.ID(name) == 0 {
			t.Errorf("the script never surfaces %q", name)
		}
	}

	// The authored rows, which are what a treeview has always been: five top rows,
	// and the cells the values batch wrote.
	tv := theTree(t, desktop)
	items := topRows(t, desktop, tv, 5)
	onUI(desktop, func() {
		if items[2].Text != "PC12" {
			t.Errorf("top row 2 shows %q, want the authored folder", items[2].Text)
		}
		if items[2].IsLeaf() {
			t.Error("the authored folder says it is a leaf")
		}
	})

	// --- one flat level of a hundred thousand -----------------------------
	//
	// A plain source read as a tree of one generation, which is ordinary rather
	// than a special case.
	if err := a.point("source:flat", "one flat level", a.flat); err != nil {
		t.Fatalf("naming the flat source: %v", err)
	}
	flat := topRows(t, desktop, tv, flatRows)
	onUI(desktop, func() {
		if flat[0].Text != "item 000000" {
			t.Errorf("the first flat row shows %q, want the first record's name", flat[0].Text)
		}
		if !flat[0].IsLeaf() {
			t.Error("a flat row says it has children")
		}
		// **A data column called `kind` reads the record's own field**, which is a
		// name a tree would otherwise take for itself. Here because a Details tree
		// with a Kind column is what this app is, and its every cell came back empty.
		if got := flat[0].Value("kind"); got != "Text" {
			t.Errorf("the Kind column reads %q, want the record's own", got)
		}
	})

	// Sorting a hundred thousand rows is the APPLICATION's to do: the header click
	// sends `sort=` down with the query, and an app that ignores it draws the arrow
	// and changes nothing -- while `Ordered` claims the rows are in the sequence's
	// order, so the display believes it and does not sort them either.
	onUI(desktop, func() { tv.SetSorted(true, -1, true) })
	waitFor(t, desktop, 10*time.Second, func() bool {
		rows := tv.RootItems()
		return len(rows) == flatRows && rows[0].Text == "item 099999"
	}, "the flat level sorted by name, descending")

	// Put it back the way the window declared it, because **a sort survives a change
	// of source**: it is the view's state and not the sequence's, and what follows
	// reads the deep body in the order its containers are named.
	onUI(desktop, func() { tv.SetSorted(true, -1, false) })
	waitFor(t, desktop, 10*time.Second, func() bool {
		rows := tv.RootItems()
		return len(rows) == flatRows && rows[0].Text == "item 000000"
	}, "the flat level sorted by name, ascending again")

	// --- a hundred thousand over three levels -----------------------------
	//
	// Through a bundle, because the SHAPE is the document's saying and the records
	// are the application's. Only the top level is in the sequence with nothing
	// open, which is why this is a hundred and not a hundred thousand.
	if err := a.point("bundle:deeptree", "a three-level hierarchy", a.deep); err != nil {
		t.Fatalf("naming the bundle: %v", err)
	}
	deep := topRows(t, desktop, tv, deepDirs)
	onUI(desktop, func() {
		if deep[0].Text != "volume 000" {
			t.Errorf("the first deep row shows %q, want the first container", deep[0].Text)
		}
		// The hierarchy is the bundle's: the application served records with a `dir`
		// in them and said nothing at all about shape.
		if deep[0].IsLeaf() {
			t.Error("a container says it is a leaf; the records said it has children")
		}
	})

	// **And it opens**, which is the other half of a hierarchy and is not implied by
	// the twisty being drawn. A level with a standing is marked by its PATH, so a
	// view asking by identity marks a node that is not there: the twisty was drawn
	// from a real child count, the mark went into the set, and nothing moved.
	onUI(desktop, func() { tv.ExpandItem(deep[0]) })
	waitFor(t, desktop, 20*time.Second, func() bool {
		top := tv.RootItems()
		return len(top) == deepDirs && len(top[0].Children) == deepSubs
	}, "the first container's children")
}

// waitFor polls a condition on the thread that owns the trinkets, because a tree
// over an application's records walks on a thread of its own and tells when it is
// done -- so the rows exist a moment after the asking, not during it.
func waitFor(t *testing.T, desktop *trinkets.Desktop, within time.Duration,
	yet func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		done := false
		onUI(desktop, func() { done = yet() })
		if done {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("waited %s for %s", within, what)
}

// The two bodies are what the app serves, and the sizes are what the demo claims.
// Checked here because "a hundred thousand rows" is the whole premise, and a
// generator that quietly made ninety thousand would still look fine on screen.
func TestBothBodiesAreAHundredThousandRows(t *testing.T) {
	if got := len(flatBody()); got != flatRows {
		t.Errorf("the flat body holds %d rows, want %d", got, flatRows)
	}
	deep := deepBody()
	want := deepDirs * (1 + deepSubs*(1+deepFiles))
	if len(deep) != want {
		t.Fatalf("the deep body holds %d rows, want %d", len(deep), want)
	}
	if want != flatRows+deepDirs+deepDirs*deepSubs {
		t.Errorf("the deep body is %d rows against the flat body's %d;"+
			" the two are meant to be the same hundred thousand files"+
			" plus the containers they sit in", want, flatRows)
	}

	// The top level is what descends by LOCATION: a row with no container is at the
	// top, and every other row names the one it is in.
	tops := 0
	for _, r := range deep {
		if r.dir == "" {
			tops++
		}
	}
	if tops != deepDirs {
		t.Errorf("%d deep rows have no container, want %d", tops, deepDirs)
	}
}

// nullBackend is a headless RenderBackend.
type nullBackend struct{ mu sync.Mutex }

func (n *nullBackend) Init() error { return nil }
func (n *nullBackend) Shutdown()   {}
func (n *nullBackend) Metrics() core.CellMetrics {
	return core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}
}
func (n *nullBackend) Size() core.UnitSize {
	return core.UnitSize{Width: 8 * 120, Height: 16 * 40}
}
func (n *nullBackend) BeginFrame()                                          {}
func (n *nullBackend) EndFrame()                                            {}
func (n *nullBackend) Clear(style.CellStyle)                                {}
func (n *nullBackend) SetClip(core.UnitRect)                                {}
func (n *nullBackend) DrawCell(core.Unit, core.Unit, rune, style.CellStyle) {}
func (n *nullBackend) DrawText(x, y core.Unit, t string, s style.CellStyle, f *core.Font) core.Unit {
	return 0
}
func (n *nullBackend) DrawTextAligned(core.UnitRect, string, core.HSide, core.VAlign, style.CellStyle, *core.Font) {
}
func (n *nullBackend) FillRect(core.UnitRect, rune, style.CellStyle)                     {}
func (n *nullBackend) DrawRect(core.UnitRect, style.BorderStyle, style.CellStyle)        {}
func (n *nullBackend) DrawHLine(core.Unit, core.Unit, core.Unit, rune, style.CellStyle)  {}
func (n *nullBackend) DrawVLine(core.Unit, core.Unit, core.Unit, rune, style.CellStyle)  {}
func (n *nullBackend) DrawBox(core.UnitRect, style.BorderStyle, string, style.CellStyle) {}
func (n *nullBackend) PollEvent() core.Event                                             { return nil }
func (n *nullBackend) WaitEvent() core.Event                                             { return nil }
func (n *nullBackend) SetCursorVisible(bool)                                             {}
func (n *nullBackend) SetCursorPosition(core.Unit, core.Unit)                            {}
func (n *nullBackend) SetCursorStyle(int)                                                {}
func (n *nullBackend) SupportsColor() bool                                               { return true }
func (n *nullBackend) SupportsMouse() bool                                               { return true }
func (n *nullBackend) SupportsUnicode() bool                                             { return true }
func (n *nullBackend) ColorDepth() int                                                   { return 256 }
func (n *nullBackend) GetClipboard() string                                              { return "" }
func (n *nullBackend) SetClipboard(string)                                               {}
func (n *nullBackend) Beep()                                                             {}
