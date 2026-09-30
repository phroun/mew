package main

// The Basic Trinkets tab's "Ask for a Name..." button puts up an input dialog,
// offering the name given last, and shows what came back beside the button. A
// Cancel, or a window closed without an answer, leaves the name as it was.

import (
	"fmt"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/trinkets"
)

func TestTheDemoAsksForAName(t *testing.T) {
	a, _ := denomWindow(t)
	shown := func() string {
		t.Helper()
		l, ok := a.ui.Object("bname").Target().(*trinkets.Label)
		if !ok {
			t.Fatal("bname is not a label")
		}
		return l.Text()
	}
	// asked presses the button and returns the dialog it put up.
	asked := func() *trinkets.InputDialog {
		t.Helper()
		a.press(t, "bask")
		ui, err := a.conn.Build(fmt.Sprintf("d=nq%d", a.nameAsks))
		if err != nil {
			t.Fatalf("finding the dialog: %v", err)
		}
		d, ok := ui.Object("d").Target().(*trinkets.InputDialog)
		if !ok {
			t.Fatalf("the button put up %T, want an input dialog", ui.Object("d").Target())
		}
		return d
	}
	keys := func(d *trinkets.InputDialog, ks ...string) {
		for _, k := range ks {
			text := ""
			if len([]rune(k)) == 1 {
				text = k
			}
			d.HandleKeyPress(core.KeyPressEvent{Key: k, Text: text})
		}
	}

	if got := shown(); got != "No name given yet." {
		t.Errorf("before asking, the tab shows %q", got)
	}

	d := asked()
	if d.Text() != "" {
		t.Errorf("the first ask offers %q, want nothing", d.Text())
	}
	keys(d, "P", "a", "t", "Return")
	if got := shown(); got != "Name: Pat" {
		t.Errorf("after answering Pat, the tab shows %q", got)
	}

	// The next ask offers the name given, and a Cancel keeps it.
	d = asked()
	if d.Text() != "Pat" {
		t.Errorf("the second ask offers %q, want Pat", d.Text())
	}
	keys(d, "X")
	d.Close()
	if got := shown(); got != "Name: Pat" {
		t.Errorf("after closing without an answer, the tab shows %q", got)
	}
	if a.name != "Pat" {
		t.Errorf("after closing without an answer, the name is %q", a.name)
	}
}
