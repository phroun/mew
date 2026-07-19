package raster_test

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// The graphical renderer has ONE text path: shaped and proportional.
// Its measurement (core.TextMeasurer) comes from the same engine, so
// the advance DrawText returns is the advance it painted.
func TestDrawTextMatchesMeasurement(t *testing.T) {
	b, err := raster.New(400, 64)
	if err != nil {
		t.Fatal(err)
	}
	b.Clear(style.DefaultStyle())

	f := core.FontUIText12
	const s = "Proportional text, finally"
	painted := b.DrawText(4, 4, s, style.DefaultStyle(), f)
	measured := b.MeasureText(f, s)
	if painted != measured {
		t.Errorf("painted advance %d != measured %d", painted, measured)
	}
	if painted <= 0 {
		t.Error("no advance painted")
	}
}

func TestUITextIsProportionalHere(t *testing.T) {
	b, err := raster.New(64, 64)
	if err != nil {
		t.Fatal(err)
	}
	// "ui-text" maps to the proportional UI family on the graphical
	// renderer (the text-based system maps it to Monday instead).
	narrow := b.MeasureText(core.FontUIText12, "iii")
	wide := b.MeasureText(core.FontUIText12, "MMM")
	if narrow >= wide {
		t.Errorf("ui-text should be proportional here: iii=%d MMM=%d", narrow, wide)
	}
	// Monospace by explicit choice gives the cell-gridded look back.
	if a, b2 := b.MeasureText(core.FontMonday12, "iii"), b.MeasureText(core.FontMonday12, "MMM"); a != b2 {
		t.Errorf("Monday should stay fixed-pitch: %d != %d", a, b2)
	}
	// LineHeight keeps the toolkit grid: 12pt = 16 units.
	if h := b.LineHeight(core.FontUIText12); h != 16 {
		t.Errorf("12pt line height = %d, want 16", h)
	}
}
