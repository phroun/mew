package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/protocol"
)

// A terminal emits its grid size once, when the grid first fits its paint.
// A client that subscribes to `resize` after that (the build reply has to
// round-trip before it can) would miss it and leave its PTY at the default,
// so the shell mis-wraps its prompt (the stray zsh `%` EOL marks). The
// terminal must re-emit its current size when `resize` is subscribed, so a
// late subscriber still learns it.
func TestPurfecTermResizeReEmittedOnSubscribe(t *testing.T) {
	var events []*protocol.Event
	ctx := &protocol.BindContext{
		Emit: func(ev *protocol.Event) { events = append(events, ev) },
	}
	f := &captureFactory{inner: protocol.NewRegistryFactory(ctx)}
	script, err := protocol.Parse("t=new terminal")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := protocol.NewSession().Execute(script, f); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var term *PurfecTerm
	for _, tg := range f.targets {
		if pt, ok := tg.(*PurfecTerm); ok {
			term = pt
		}
	}
	if term == nil {
		t.Fatal("terminal target not found")
	}
	if term.Terminal() == nil {
		t.Skip("no terminal")
	}

	// Nothing emitted yet: the build subscribed to nothing, so any size
	// emit during bind was filtered.
	events = nil

	// A client subscribes to resize (as the PTY driver does after build).
	f.Subscribe(trinketID(term), "resize")

	var got *protocol.Event
	for _, ev := range events {
		if ev.Type == "resize" {
			got = ev
		}
	}
	if got == nil {
		t.Fatal("subscribing to resize did not re-emit the current size")
	}
	cols, _ := got.Int("cols")
	rows, _ := got.Int("rows")
	if cols <= 0 || rows <= 0 {
		t.Errorf("re-emitted resize has a degenerate size %dx%d", cols, rows)
	}
}
