package main

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/protocol"
)

// The main demo window is protocol text; this guards that the script
// executes, surfaces every key the handlers depend on, and resolves
// them to the right trinket types.
func TestMainWindowScriptBuilds(t *testing.T) {
	ctx := &protocol.BindContext{Dispatch: func(string) {}}
	factory := &idCaptureFactory{
		inner: protocol.NewRegistryFactory(ctx),
		byID:  make(map[uint64]any),
	}
	script, err := protocol.Parse(mainWindowScript())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	reply, err := protocol.NewSession().Execute(script, factory)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if _, ok := factory.byID[reply.IDs["w"]].(*window.Window); !ok {
		t.Errorf("w is %T, want *window.Window", factory.byID[reply.IDs["w"]])
	}
	tw, ok := factory.byID[reply.IDs["tabs"]].(*trinkets.TabTrinket)
	if !ok {
		t.Fatalf("tabs is %T, want *trinkets.TabTrinket", factory.byID[reply.IDs["tabs"]])
	}
	// Eight protocol tabs; MDI joins imperatively at runtime.
	if tw.Count() != 8 {
		t.Errorf("tab count = %d, want 8", tw.Count())
	}

	for key, want := range map[string]string{
		"binput":   "*trinkets.TextInput",
		"wfont":    "*trinkets.Checkbox",
		"dfont":    "*trinkets.Checkbox",
		"grid":     "*trinkets.Checkbox",
		"bgdef":    "*trinkets.RadioButton",
		"bggreen":  "*trinkets.RadioButton",
		"bggray":   "*trinkets.RadioButton",
		"sbgdef":   "*trinkets.RadioButton",
		"sbggreen": "*trinkets.RadioButton",
		"sbggray":  "*trinkets.RadioButton",
	} {
		id, ok := reply.IDs[key]
		if !ok {
			t.Errorf("reply missing key %q", key)
			continue
		}
		if got := typeName(factory.byID[id]); got != want {
			t.Errorf("%s is %s, want %s", key, got, want)
		}
	}
}

func typeName(v any) string {
	switch v.(type) {
	case *trinkets.TextInput:
		return "*trinkets.TextInput"
	case *trinkets.Checkbox:
		return "*trinkets.Checkbox"
	case *trinkets.RadioButton:
		return "*trinkets.RadioButton"
	default:
		return "other"
	}
}

// Paint the protocol-built main window and sanity-check the first tab
// renders its content; then switch tabs WITH THE SET VERB and check
// the Selection tab paints too.
func TestMainWindowPaint(t *testing.T) {
	ctx := &protocol.BindContext{Dispatch: func(string) {}}
	factory := &idCaptureFactory{
		inner: protocol.NewRegistryFactory(ctx),
		byID:  make(map[uint64]any),
	}
	session := protocol.NewSession()
	script, err := protocol.Parse(mainWindowScript())
	if err != nil {
		t.Fatal(err)
	}
	reply, err := session.Execute(script, factory)
	if err != nil {
		t.Fatal(err)
	}
	w := factory.byID[reply.IDs["w"]].(*window.Window)

	g := newGridBackend(62, 20)
	w.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 8 * 62, Height: 16 * 20})
	w.Layout()
	w.Paint(core.NewPainter(g))
	out := g.dump()
	t.Logf("main window (Basic Trinkets tab):\n%s", out)

	// (The text input is the window's first focusable, so it paints
	// focused - fill, no placeholder text - same as the imperative
	// version did.)
	for _, want := range []string{
		"KittyTK Demo",
		"Basic Trinkets",
		"This is a demo of basic trinkets:",
		"OK",
		"Cancel",
		"Apply",
		"Disabled",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("painted output missing %q", want)
		}
	}

	// Flip to the Selection tab via the set verb (session keys
	// persist across batches), re-layout, repaint.
	flip, _ := protocol.Parse(`set tabs selected=1`)
	if _, err := session.Execute(flip, factory); err != nil {
		t.Fatalf("set tabs selected: %v", err)
	}
	w.Layout()
	g2 := newGridBackend(62, 20)
	w.Paint(core.NewPainter(g2))
	out2 := g2.dump()
	t.Logf("main window (Selection tab):\n%s", out2)

	// ("Font Options:" etc. exist but sit below the 20-row test
	// viewport - the panes clip, same as the imperative version.)
	for _, want := range []string{
		"The quick brown fox",
		"Checkboxes:",
		"[x] Enable feature A",
		"Radio buttons:",
		"( ) Option 1",
	} {
		if !strings.Contains(out2, want) {
			t.Errorf("selection tab missing %q", want)
		}
	}
}
