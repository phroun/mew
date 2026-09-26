package main

// Standing in for the far end of a name.
//
// The demo's build names a source the APPLICATION serves (see refusing.go), and the
// display resolves such a name by asking the connection it came in on. These tests
// have no connection: they build the same script against an in-process display, where
// a name can only mean something registered in this process. Nothing being on the
// other end of the wire, the whole build is refused -- which is the display being
// right, and is where a misspelled `source:` is caught.
//
// So the process stands in for the application: the same three rows and the same
// refusal, said as a serval source rather than through a Fill. What the tests below
// are about is unaffected either way; what they need is for the name to mean
// something.

import (
	"os"
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/inprocess"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/serval"
)

func TestMain(m *testing.M) {
	trinkets.RegisterSource(refusedSourceName, refusingSource{})
	os.Exit(m.Run())
}

// refusingSource answers three rows and then refuses, which is what serveRefusal
// does over the wire.
type refusingSource struct{}

func (refusingSource) Open(descriptor *serval.DataSetDescriptor) (serval.DataSet, error) {
	return refusingSet{}, nil
}

type refusingSet struct{}

func (refusingSet) Close()                          {}
func (refusingSet) RecordCount() serval.RecordCount { return serval.Exactly(3) }

func (refusingSet) Read(sc *serval.Scope, out serval.Sink) error {
	out.Ordered()
	for i, name := range []string{"first row", "second row", "third row"} {
		if err := out.Record(serval.NewInt(int64(i)), serval.Record{
			serval.Named("name", name),
		}); err != nil {
			return err
		}
	}
	out.Done(serval.Complete{
		Error: "the demo refuses on purpose: there is nothing of this source past row three",
	})
	return nil
}

// **The demo's refusing list is refused, and holds the reason.**
//
// It is in the build to be LOOKED at, so what would spoil it is the list quietly
// working: a source renamed on one side only, or a `display=` field that stops
// matching, leaves three rows and no line, and the tab then demonstrates nothing.
func TestTheDemosRefusedListSaysWhy(t *testing.T) {
	conn := inprocess.New(nil)
	ui, err := conn.Build(mainBuildScript())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	l, _ := ui.Object("lirefused").Target().(*trinkets.ListView)
	if l == nil {
		t.Fatal("the Lists tab has no refused list, or its key is not surfaced")
	}
	l.SetBounds(core.UnitRect{Width: 60 * 8, Height: 5 * 16})

	// The rows it does get arrive, and are still there.
	if got := l.Item(0); got == nil || got.Text != "first row" {
		t.Errorf("the first row reads %v, want the row the source sent before it refused", got)
	}
	// And the refusal is on the list, in the source's own words. The demo hands it
	// to nobody, so the list is the one that draws it -- which is the whole point
	// of its being in the build.
	got := l.Trouble()
	if !got.Any() {
		t.Fatal("the list was refused and holds nothing -- there is nothing to look at")
	}
	if !strings.Contains(got.Reason, "past row three") {
		t.Errorf("it holds %q, which is not what the source said", got.Reason)
	}
}
