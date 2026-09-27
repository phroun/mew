package display_test

// A refusal crossing the wire, all the way to the trinket that asked.
//
// An application turns a scope down with `Fail`, which is `error=` on the answer's
// own ending. Every step between here and the list is somebody's code -- the client
// writes the statement, the display's application source reads it, a cache sits
// under that source, and the list's sink is what finally holds it -- and a refusal
// dropped at any one of them is a list that draws an empty area for ever.
//
// So this is the whole path in one test, over a real socket: what the source said,
// read back off the trinket a reader is looking at.

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/serval"
)

// serveThenRefuse answers three rows and turns the rest down, which is the demo's
// refusing source (examples/demoapp/refusing.go) said here.
func serveThenRefuse(f *client.Fill) {
	for i, name := range []string{"first row", "second row", "third row"} {
		if err := f.Record(serval.NewInt(int64(i)), serval.Named("name", name)); err != nil {
			return
		}
	}
	_ = f.Fail("there is nothing of this source past row three")
}

func TestARefusalReachesTheTrinketThatAsked(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop, _, stop := servingDesktop(t, sock)
	defer stop()

	conn, err := client.Dial(sock, "Refusing App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.ProvideSource("papers", serveThenRefuse); err != nil {
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

	// The rows it did send arrive, the same as any other answer's.
	lv := waitForList(t, desktop, 5*time.Second)
	onUI(desktop, func() {
		if item := lv.Item(0); item == nil || item.Text != "first row" {
			t.Errorf("row 0 shows %v, want the first row the application sent", item)
		}
	})

	// And the reason arrives with them. Polling because the answer lands on the
	// connection's own thread: what is being waited for is the refusal being there
	// at all, not how quickly.
	var got trinkets.Trouble
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		onUI(desktop, func() {
			lv.Item(0)
			got = lv.Trouble()
		})
		if got.Any() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !got.Any() {
		t.Fatal("the application refused the scope and the list holds nothing:" +
			" a reader sees three rows and no reason they are all there is")
	}
	if got.Reason != "there is nothing of this source past row three" {
		t.Errorf("the list holds %q, want what the source said", got.Reason)
	}
}
