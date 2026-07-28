package main

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/protocol"
)

// Toggling the 32-unit-rows denomination on and back off must return
// the splitter (and everything else) to its original dimensions.
func TestGridToggleRoundTrips(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, err := raster.New(1280, 800)
	if err != nil {
		t.Fatal(err)
	}
	d := trinkets.NewDesktop()
	d.SetBackend(px)

	ctx := &protocol.BindContext{Dispatch: func(string) {}}
	factory := &idCaptureFactory{
		inner: protocol.NewRegistryFactory(ctx),
		byID:  make(map[uint64]any),
	}
	script, err := protocol.Parse(mainWindowScript())
	if err != nil {
		t.Fatal(err)
	}
	sess := protocol.NewSession()
	reply, err := sess.Execute(script, factory)
	if err != nil {
		t.Fatal(err)
	}
	win := factory.byID[reply.IDs["w"]].(*window.Window)
	spScript, err := protocol.Parse("spx=w.t.s.o.sp")
	if err != nil {
		t.Fatal(err)
	}
	spReply, err := sess.Execute(spScript, factory)
	if err != nil {
		t.Fatal(err)
	}
	sp := factory.byID[spReply.IDs["spx"]].(*trinkets.Splitter)

	tabs := factory.byID[reply.IDs["tabs"]].(*trinkets.TabTrinket)
	for i := 0; i < tabs.Count(); i++ {
		if tabs.TabText(i) == "Selection" {
			tabs.SetCurrentIndex(i)
		}
	}

	d.WindowManager().AddWindow(win)
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 640, Height: 480})
	win.Layout()

	before := sp.Bounds()
	beforePos := sp.Position()

	win.SetCellMetrics(&core.CellMetrics{CellWidth: 8, CellHeight: 32})
	win.Layout()
	during := sp.Bounds()

	win.SetCellMetrics(nil)
	win.Layout()
	after := sp.Bounds()
	afterPos := sp.Position()

	t.Logf("before %+v pos %v", before, beforePos)
	t.Logf("during %+v", during)
	t.Logf("after  %+v pos %v", after, afterPos)
	if before != after {
		t.Errorf("splitter bounds did not round-trip: before %+v after %+v", before, after)
	}
	if beforePos != afterPos {
		t.Errorf("splitter position did not round-trip: before %v after %v", beforePos, afterPos)
	}
}
