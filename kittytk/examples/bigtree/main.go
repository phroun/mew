// Command bigtree is the demo's Details tab with a hundred thousand rows behind
// it, as a display-protocol APPLICATION.
//
//	terminal 1:  go run ./cmd/kittytk-tui          (or -tags sdl ./cmd/kittytk-sdl)
//	terminal 2:  go run ./examples/bigtree
//
// The tree opens on fifteen authored rows -- what a treeview has always been --
// and two buttons point it at a source instead:
//
//	Flat 100,000   one level of a hundred thousand siblings, named `source:flat`
//	Deep 100,000   a hundred thousand rows over three levels, through a bundle
//	               that says what shape they are
//
// # What this is for watching
//
// The rows are the APPLICATION's, and every one of them crosses the socket only
// if something asks for it. So what is on show is how much the tree asks for:
//
//   - The flat body is one level of a hundred thousand. A tree flattening it for
//     a window of forty rows asks that level for forty-one, and the spare one is
//     what tells an exact count from a floor. Watch it open at once.
//   - The deep body is many small levels instead of one enormous one, so what it
//     exercises is the pre-order walk and the child counts rather than the
//     window. Opening a node asks one level of a hundred.
//
// A caveat worth knowing before drawing conclusions from the flat case: the views
// still read with the whole walk rather than a window, so the tree currently pulls
// more than the forty rows it draws. The level read is windowed; the view's read
// is not, yet.
//
// # There is no button back to the authored rows
//
// `SetSource(nil)` is what says "read your own items again", and the wire language
// has no way to say it -- `source=` names a source and a name naming nothing is
// refused. Worth noticing rather than working around: it is the one direction the
// property does not go.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/protocol"
)

func main() {
	path := client.DefaultSocketPath()
	a, err := start(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		fmt.Fprintln(os.Stderr, "start a desktop first: go run ./cmd/kittytk-tui (or -tags sdl ./cmd/kittytk-sdl)")
		os.Exit(1)
	}
	defer a.conn.Close()

	quit := make(chan struct{})
	a.ui.Window("w").OnClosed(func() { close(quit) })
	select {
	case <-quit:
	case <-a.conn.Closed():
	}
}

// app is the handful of handles the wiring needs, and the two bodies' sizes so a
// naming can say how many rows it just pointed at.
type app struct {
	conn *client.Conn
	ui   *client.UI
	tree client.Handle
	said client.Handle

	flat, deep int
}

// start is the whole of standing this application up, so that the live-session
// test drives what the binary does rather than something like it.
func start(path string) (*app, error) {
	conn, err := client.Dial(path, "KittyTK Big Tree", nil)
	if err != nil {
		return nil, fmt.Errorf("cannot reach display service at %s: %w", path, err)
	}

	// Hosting the far ends. **Nothing goes out on the wire for this**: a source is
	// a name and a handler, here, and the display already knows how to want a name.
	// Building the bodies is this application's own cost, and is paid up front so
	// that a button press is not also a hundred thousand allocations.
	flat, deep := flatBody(), deepBody()
	for name, body := range map[string][]row{"flat": flat, "deep": deep} {
		if _, err := conn.ProvideSource(name, serve(body)); err != nil {
			conn.Close()
			return nil, err
		}
	}

	// The document that says what shape the deep body is. It holds no records --
	// an include names the application for those -- so it is small, and it is kept
	// in this application's own store like any other blob.
	if err := conn.Store().Write("deeptree-1.0.0", "psl", []byte(deepBundle())); err != nil {
		conn.Close()
		return nil, fmt.Errorf("keeping the bundle: %w", err)
	}

	ui, err := conn.Build(buildScript())
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("build: %w", err)
	}
	if _, err := conn.Exec(valuesScript(func(name string) uint64 {
		return ui.Object(name).ID()
	})); err != nil {
		conn.Close()
		return nil, fmt.Errorf("filling the authored rows: %w", err)
	}

	a := &app{
		conn: conn, ui: ui,
		tree: ui.Object("tree"), said: ui.Object("said"),
		flat: len(flat), deep: len(deep),
	}
	a.wireSources()
	a.wireToggles()
	return a, nil
}

// wireSources hangs the two namings off the two buttons.
func (a *app) wireSources() {
	a.ui.Button("bflat").OnClick(func() {
		_ = a.point("source:flat", "one flat level", a.flat)
	})
	a.ui.Button("bdeep").OnClick(func() {
		_ = a.point("bundle:deeptree", "a three-level hierarchy", a.deep)
	})
}

// point is the whole of what a button does: name a source at the tree.
//
// **One property, set from here, and the rows are somebody else's.** No records go
// with it and no shape either -- the flat name is read as one generation because
// that is what a plain source is, and the bundle carries the deep body's hint
// because shape is a saying about records and the document is what says it.
//
// The timing is around the SET, which is the display resolving the name and the
// tree stating its sequence. It is not how long a hundred thousand rows take to
// arrive: they arrive when something wants them, which is the point.
func (a *app) point(name, what string, rows int) error {
	start := time.Now()
	if err := a.tree.Set("source=" + protocol.Quote(name)); err != nil {
		a.say(fmt.Sprintf("Reading %s: refused -- %v", what, err))
		return err
	}
	a.say(fmt.Sprintf("Reading %s: %d rows, pointed in %s",
		what, rows, time.Since(start).Round(time.Millisecond)))
	return nil
}

// say puts a line in the window's label, which is where this app narrates. A
// desktop status bar would do as well; a label is in the window whatever else is
// on the screen.
func (a *app) say(text string) { _ = a.said.Set("caption=" + protocol.Quote(text)) }

// wireToggles is the demo's own feature row, unchanged, because a hundred thousand
// rows are a reason to look at pinned columns and the ledger again rather than a
// reason to drop them.
func (a *app) wireToggles() {
	flag := func(on, off string) func(protocol.FlagState) {
		return func(s protocol.FlagState) {
			if s == protocol.FlagTrue {
				_ = a.tree.Set(on)
			} else {
				_ = a.tree.Set(off)
			}
		}
	}
	a.ui.Checkbox("showkey").OnToggle(flag("showkey", "!showkey"))
	a.ui.Checkbox("hscroll").OnToggle(flag("!fit_width", "fit_width"))
	a.ui.Checkbox("ledger").OnToggle(flag("ledger", "!ledger"))
	a.ui.Checkbox("lines").OnToggle(flag("treelines", "!treelines"))
	a.ui.Checkbox("pinl").OnToggle(func(s protocol.FlagState) {
		n := 0
		if s == protocol.FlagTrue {
			n = 2
		}
		_ = a.tree.Set(fmt.Sprintf("fixed_begin=%d", n))
	})
	a.ui.Checkbox("pinr").OnToggle(func(s protocol.FlagState) {
		n := 0
		if s == protocol.FlagTrue {
			n = 1
		}
		_ = a.tree.Set(fmt.Sprintf("fixed_end=%d", n))
	})
}
