package display_test

// A trinket reading records that are its own APPLICATION's, across a real
// socket.
//
// The whole path, and none of it invented for this: the application serves a
// source the way `docs/hosting-a-query.md` says to, the display stands up the far
// end of that name as a `source.ApplicationSource`, a trinket names it in the wire
// language, and the records come back over the connection they were asked for on.
//
// **The names all live process side.** An application announces nothing; it hosts
// the remote end of a name the display already knows how to want. So what is
// checked here is the two joins that were missing -- a name on a connection
// reaching that connection's application, and the application's results reaching
// the source that asked -- rather than any new statement.

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/serval"
)

// servedRows is what the application answers with: a flat body of records, named
// and sized, which is the least an application can serve.
func servedRows() []struct {
	name string
	size int64
} {
	return []struct {
		name string
		size int64
	}{
		{"alpha.txt", 2048}, {"beta.txt", 310},
		{"gamma.txt", 96}, {"delta.txt", 14022},
	}
}

// serveAll is the simplest correct fill: send everything and say so. Below a size
// the author picks it is also the fastest, which is why it is the first thing
// hosting-a-query.md shows.
func serveAll(f *client.Fill) {
	for i, r := range servedRows() {
		_ = f.Record(serval.NewInt(int64(i)),
			serval.Named("name", r.name),
			serval.Named("size", r.size))
	}
	_ = f.Exhausted()
}

func TestATrinketReadsItsApplicationsOwnRecords(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop, srv, stop := servingDesktop(t, sock)
	defer stop()
	_ = srv

	conn, err := client.Dial(sock, "Records App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// The application hosts the far end. Nothing goes out on the wire for this:
	// it is a name and a handler, here.
	if _, err := conn.ProvideSource("papers", serveAll); err != nil {
		t.Fatalf("providing: %v", err)
	}

	// And a trinket names it. Nothing in the display's process registry stands for
	// `papers`, so the name is this application's.
	ui, err := conn.Build(`
w=new window title="Papers" width=320 height=200 children={
	lv=new listview source="source:papers" display="name" value="size"
}
wlv=w.lv
`)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if ui.ID("wlv") == 0 {
		t.Fatal("the list was not surfaced")
	}

	// **A list reads when it needs rows, not when it is pointed at a source.** So
	// asking for one is what sends the query; the answer comes back over the
	// connection afterwards, and the arrival notice is what makes the second ask
	// find it. Polling here stands in for the frames a real display would draw.
	lv := waitForList(t, desktop, 5*time.Second)
	if got, want := lv.Count(), len(servedRows()); got != want {
		t.Fatalf("the list holds %d rows, want the %d the application served", got, want)
	}

	// What it shows is what the application said, read through the fields the
	// statement named.
	onUI(desktop, func() {
		if item := lv.Item(0); item == nil || item.Text != "alpha.txt" {
			t.Errorf("row 0 shows %v, want the first record's name", item)
		}
		if v := lv.ValueAt(0); v == nil || !v.IsInt || v.Int != 2048 {
			t.Errorf("row 0 means %v, want its size", v)
		}
	})
}

// **Two trinkets naming one source read one source.** The application's records
// are one body whichever trinket is looking at them, and standing up a second far
// end would pay twice for one answer.
func TestTwoTrinketsNamingOneSourceShareIt(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop, _, stop := servingDesktop(t, sock)
	defer stop()

	conn, err := client.Dial(sock, "Shared App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ProvideSource("papers", serveAll); err != nil {
		t.Fatalf("providing: %v", err)
	}

	if _, err := conn.Build(`
w=new window title="Two lists" width=320 height=240 children={
	p=new panel layout=vbox children={
		one=new listview source="source:papers" display="name"
		two=new listview source="source:papers" display="name"
	}
}
`); err != nil {
		t.Fatalf("build: %v", err)
	}

	var lists []*trinkets.ListView
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		lists = nil
		onUI(desktop, func() {
			for _, a := range desktop.Applications() {
				for _, w := range a.Windows() {
					if p, ok := w.Content().(*trinkets.Panel); ok {
						for _, kid := range p.Children() {
							if lv, ok := kid.(*trinkets.ListView); ok {
								lists = append(lists, lv)
								lv.Item(0) // asking is what sends the query
							}
						}
					}
				}
			}
		})
		if len(lists) == 2 && lists[0].Count() > 0 && lists[1].Count() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(lists) != 2 {
		t.Fatalf("found %d lists, want two", len(lists))
	}
	// Both read the application, and both read the same amount of it.
	//
	// **Not the whole body**, and that is right rather than a shortfall. A list holds
	// the WINDOW it asked for, and `serveAll` ignores the count and sends everything
	// -- which is correct of it, and used to mean the surplus was kept because
	// nothing in the way trimmed it. There is something in the way now: an
	// application's source is wrapped in an amendment so a reader can write in it,
	// and an amendment honours the count. The rest is asked for when something wants
	// it, which is what a window is for.
	if a, b := lists[0].Count(), lists[1].Count(); a != b {
		t.Errorf("the two lists hold %d and %d rows, reading one source", a, b)
	}
	for i, lv := range lists {
		if got, most := lv.Count(), len(servedRows()); got <= 0 || got > most {
			t.Errorf("list %d holds %d rows, want between one and the %d served",
				i, got, most)
		}
	}
	// One source object behind both, which is what sharing means here.
	onUI(desktop, func() {
		if lists[0].Source() == nil || lists[0].Source() != lists[1].Source() {
			t.Error("the two lists read two different sources")
		}
	})
}

// A name the process registry DOES hold is that one, not the application's. The
// fallback is what happens when nothing else stands for a name, and it does not
// get to shadow something that does.
func TestARegisteredNameBeatsTheApplications(t *testing.T) {
	trinkets.RegisterSource("papers", serval.NewListSource([]serval.Row{
		serval.NewRow(serval.NewInt(1), serval.Record{
			serval.Named("name", "the display's own"),
		}),
	}))
	defer trinkets.UnregisterSource("papers")

	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop, _, stop := servingDesktop(t, sock)
	defer stop()

	conn, err := client.Dial(sock, "Shadowed App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ProvideSource("papers", serveAll); err != nil {
		t.Fatalf("providing: %v", err)
	}
	if _, err := conn.Build(`
w=new window title="Papers" width=320 height=200 children={
	lv=new listview source="source:papers" display="name"
}
`); err != nil {
		t.Fatalf("build: %v", err)
	}

	lv := waitForList(t, desktop, 3*time.Second)
	onUI(desktop, func() {
		if got := lv.Count(); got != 1 {
			t.Fatalf("the list holds %d rows, want the one the display registered", got)
		}
		if item := lv.Item(0); item == nil || item.Text != "the display's own" {
			t.Errorf("row 0 shows %v, want the registered source's record", item)
		}
	})
}

// servingDesktop stands up a headless desktop serving on a socket, and hands back
// what it takes to stop it.
func servingDesktop(t *testing.T, sock string) (*trinkets.Desktop, *display.Server, func()) {
	t.Helper()
	desktop := trinkets.NewDesktop()
	desktop.SetBackend(&nullBackend{})

	ready := make(chan *display.Server, 1)
	desktop.SetOnStartup(func() {
		srv, err := display.Serve(desktop, sock)
		if err != nil {
			t.Errorf("serve: %v", err)
			desktop.Quit()
			return
		}
		ready <- srv
	})
	exited := make(chan int, 1)
	go func() { exited <- desktop.Run() }()

	var srv *display.Server
	select {
	case srv = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("the desktop did not start")
	}
	return desktop, srv, func() {
		srv.Close()
		desktop.Quit()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			t.Error("the desktop did not exit")
		}
	}
}

// waitForList finds the list on the desktop and asks it for a row until it has
// one.
//
// **Asking is what sends the query.** A list states its sequence when it is
// pointed at a source and reads a window of it when something wants rows -- so a
// test that only ever counts never asks the application anything. The second ask
// is what the arrival notice makes find something.
func waitForList(t *testing.T, desktop *trinkets.Desktop, within time.Duration) *trinkets.ListView {
	t.Helper()
	var lv *trinkets.ListView
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		onUI(desktop, func() {
			for _, a := range desktop.Applications() {
				for _, w := range a.Windows() {
					if got, ok := w.Content().(*trinkets.ListView); ok {
						lv = got
						lv.Item(0)
					}
				}
			}
		})
		if lv != nil && lv.Count() > 0 {
			return lv
		}
		time.Sleep(20 * time.Millisecond)
	}
	if lv == nil {
		t.Fatal("no list on the desktop")
	}
	return lv
}

// --- a bundle wrapping the application ------------------------------------

// serveTree is an application serving records that are a HIERARCHY: each one
// carries its parent's key, which is the commonest shape there is.
//
//	usr            key 0
//	  local        key 1, up 0
//	etc            key 2
func serveTree(f *client.Fill) {
	rows := []struct {
		key  int64
		name string
		up   any
		kids int64
	}{
		{0, "usr", nil, 1},
		{1, "local", int64(0), 0},
		{2, "etc", nil, 0},
	}
	// **The filter is honoured**, which is the application's job and not the
	// display's. A tree asks each level for the rows under one parent -- the top
	// level being `eq up undefined` -- and an application that sent everything
	// regardless would have every record land at every level.
	for _, r := range rows {
		fields := serval.Record{
			serval.Named("name", r.name),
			serval.Named("kids", r.kids),
		}
		if r.up != nil {
			fields = append(fields, serval.Named("up", r.up))
		}
		key := serval.NewInt(r.key)
		if !serval.Match(key, fields, f.Descriptor.Filter) {
			continue
		}
		if err := f.Record(key, fields...); err != nil {
			return
		}
	}
	_ = f.Exhausted()
}

// **A bundle declares the SHAPE and references the application for the records.**
//
// That is the whole answer to how a wire application gets a hierarchy over its own
// records, and it needed no new property and no new statement. The document says
// what its records ARE -- which is what a tree hint is, about the records and never
// about a view -- and an include names the live source they come from. An
// application's source is a source like any other, so a bundle wraps it exactly as
// it wraps anything else.
func TestABundleDeclaresTheShapeAndTheAppServesTheRecords(t *testing.T) {
	// This deadlocked once, and what fixed it is worth naming because the shape of
	// it comes back for every reader whose re-read asks a question.
	//
	// The arrival notice reaches the view on the connection's READ thread, and the
	// hop runs the re-read inline when there is no desktop to post to. A tree's
	// re-read is not a redraw -- it flattens, which asks each level -- so asking
	// from the thread that reads the answers meant waiting for itself.
	//
	// The answer was not to make the hop cleverer. A tree whose levels may answer
	// later now flattens on a goroutine of its own and tells when it is done
	// (serval's `treeDataSet.build`), so the thread that asked is never the thread
	// that waits, whichever thread that happens to be.
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop, _, stop := servingDesktop(t, sock)
	defer stop()

	conn, err := client.Dial(sock, "Shaped App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ProvideSource("rows", serveTree); err != nil {
		t.Fatalf("providing: %v", err)
	}

	// The bundle is the application's own, kept in its own store the way any
	// other is. It holds no records at all -- it says what shape somebody else's
	// are, and where to get them.
	if err := conn.Store().Write("papers-1.0.0", "psl", []byte(`(
  _bundle: (
    key: "papers", version: "1.0.0",
    includes: ( rows: ( source: "rows" ) ),
    tree: ( parent: "up", label: "name", children: "kids" )
  )
)`)); err != nil {
		t.Fatalf("keeping the bundle: %v", err)
	}

	if _, err := conn.Build(`
w=new window title="Papers" width=420 height=240 children={
	tv=new treeview source="bundle:papers" caption="Name" showheader treelines
}
`); err != nil {
		t.Fatalf("build: %v", err)
	}

	var tv *trinkets.TreeView
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		onUI(desktop, func() {
			for _, a := range desktop.Applications() {
				for _, w := range a.Windows() {
					if got, ok := w.Content().(*trinkets.TreeView); ok {
						tv = got
					}
				}
			}
		})
		if tv != nil && len(tv.RootItems()) == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if tv == nil {
		t.Fatal("no tree on the desktop")
	}

	onUI(desktop, func() {
		items := tv.RootItems()
		if len(items) != 2 {
			t.Fatalf("the tree holds %d top rows, want the two with no parent", len(items))
		}
		// The hierarchy is the BUNDLE's saying: the application said nothing about
		// shape, it only served records with an `up` field in them.
		var usr *trinkets.TreeItem
		for _, it := range items {
			if it.Text == "usr" {
				usr = it
			}
		}
		if usr == nil {
			t.Fatalf("the top rows are %q and %q", items[0].Text, items[1].Text)
		}
		if usr.IsLeaf() {
			t.Error("usr says it is a leaf; the records said it has a child")
		}
	})
}
