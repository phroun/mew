package display_test

// Where an application's records are written into the view that asked for them.
//
// The display had two inbound paths and only one of them went through the door. A
// REQUEST was posted to the desktop thread and waited on; an ANSWER -- the records a
// query asked for -- ran on the connection's own reader, straight through the source,
// the cache under it and into the spine of the list showing it. A trinket being
// painted by the desktop thread, written by a socket.
//
// It read as a distinction about what the statements ARE: an answer is not a request,
// answers nothing and needs no reply. True, and it says nothing about what they
// TOUCH.
//
// So this holds the desktop thread and watches where the records go. Nothing about it
// needs the race detector to see the fault -- which is the point of writing it, since
// `go test` alone is what runs.

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/serval"
)

// theList finds the one list on the desktop, on the desktop's own thread.
func theList(t *testing.T, desktop *trinkets.Desktop) *trinkets.ListView {
	t.Helper()
	var lv *trinkets.ListView
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		onUI(desktop, func() {
			for _, a := range desktop.Applications() {
				for _, w := range a.Windows() {
					if got, ok := w.Content().(*trinkets.ListView); ok {
						lv = got
					}
				}
			}
		})
		if lv != nil {
			return lv
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no list on the desktop")
	return nil
}

func TestAnAnswerIsAppliedOnTheDesktopThread(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop, _, stop := servingDesktop(t, sock)
	defer stop()

	conn, err := client.Dial(sock, "Slow App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// The application serves nothing until it is told to, so the answer can be made
	// to arrive at a moment of this test's choosing.
	serve := make(chan struct{})
	if _, err := conn.ProvideSource("papers", func(f *client.Fill) {
		<-serve
		for i, name := range []string{"alpha", "beta", "gamma"} {
			if err := f.Record(serval.NewInt(int64(i)), serval.Named("name", name)); err != nil {
				return
			}
		}
		_ = f.Exhausted()
	}); err != nil {
		t.Fatalf("providing: %v", err)
	}
	if _, err := conn.Build(`
w=new window title="Papers" width=320 height=200 children={
	lv=new listview source="source:papers" display="name"
}
wlv=w.lv
`); err != nil {
		t.Fatalf("build: %v", err)
	}

	lv := theList(t, desktop)
	// Asking is what sends the query. The application is holding its answer.
	onUI(desktop, func() { lv.Item(0) })

	// Now take the desktop thread away and keep it. Everything that belongs on it
	// is stuck behind this, which is the whole test: while it is held, nothing may
	// write what it draws.
	holding := make(chan struct{})
	release := make(chan struct{})
	// The count comes back down a channel rather than in a variable: it is read on
	// the desktop thread and wanted on this one, which is the very crossing this
	// test is about.
	held := make(chan int, 1)
	go func() {
		desktop.Post(func() {
			close(holding)
			<-release
			// Read here, on the thread, so reading is not itself the thing
			// that races.
			held <- lv.Count()
		})
	}()
	<-holding

	// The answer goes out while the desktop thread is held, and is read by the
	// display's own reader well inside this.
	close(serve)
	time.Sleep(250 * time.Millisecond)

	close(release)

	// It must still be empty: the records arrived, and the thread that writes them
	// into the list was busy.
	deadline := time.Now().Add(5 * time.Second)
	select {
	case n := <-held:
		if n != 0 {
			t.Errorf("the list held %d rows while the desktop thread was held: the "+
				"records were written into it by something else", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the desktop thread never came back")
	}

	// And once the thread is free they land, so nothing was dropped by waiting.
	for time.Now().Before(deadline) {
		n := 0
		onUI(desktop, func() { lv.Item(0); n = lv.Count() })
		if n > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("the records never reached the list once the desktop thread was free")
}
