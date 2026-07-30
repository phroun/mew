package text

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// An optical size multiplier scales the shaped run: 1.1 renders the face at
// 110% of the size its own metrics give it, so a face that reads small beside
// the base face can be balanced without touching the base face.
func TestSizeScaleWidensTheRun(t *testing.T) {
	e := NewEngine()
	f := &core.Font{Name: "Noto Sans Mono", Size: 12}

	base := e.ShapeRun(f, "MMMM").Width()
	if base <= 0 {
		t.Skip("face unavailable")
	}

	e.SetSizeScale("Noto Sans Mono", 1.5)
	up := e.ShapeRun(f, "MMMM").Width()
	if up <= base {
		t.Errorf("scale 1.5: width %v did not grow from %v", up, base)
	}

	e.SetSizeScale("Noto Sans Mono", 0.5)
	down := e.ShapeRun(f, "MMMM").Width()
	if down >= base {
		t.Errorf("scale 0.5: width %v did not shrink from %v", down, base)
	}

	// 1 removes it.
	e.SetSizeScale("Noto Sans Mono", 1)
	if back := e.ShapeRun(f, "MMMM").Width(); back != base {
		t.Errorf("scale 1 should restore the natural width: %v, want %v", back, base)
	}
}

// The multiplier reaches a face chosen by per-glyph FALLBACK, which is the
// case it exists for: the shaping size is computed once from the requested
// font, so scaling only that would leave every script face untouched.
func TestSizeScaleReachesFallbackFace(t *testing.T) {
	e := NewEngine()
	// Request the ordinary terminal face; the Hebrew glyphs resolve through
	// the per-glyph script fallback, exactly as a terminal cell does.
	f := &core.Font{Name: "ui-term", Size: 12}
	base := e.ShapeRun(f, "שלום").Width()
	if base <= 0 {
		t.Skip("hebrew face unavailable")
	}

	e.SetSizeScale("Noto Serif Hebrew", 1.5)
	defer e.SetSizeScale("Noto Serif Hebrew", 1)

	if got := e.ShapeRun(f, "שלום").Width(); got <= base {
		t.Errorf("a fallback face's scale was ignored: width %v, want more than %v", got, base)
	}
	// The base face is untouched by a script face's multiplier.
	latin := e.ShapeRun(f, "MMMM").Width()
	e.SetSizeScale("Noto Serif Hebrew", 2.0)
	if got := e.ShapeRun(f, "MMMM").Width(); got != latin {
		t.Errorf("scaling the hebrew face changed latin: %v, want %v", got, latin)
	}
}

// Lookup follows the alias chain, like the baseline correction.
func TestSizeScaleFollowsAliases(t *testing.T) {
	e := NewEngine()
	e.SetSizeScale("Noto Kufi Arabic", 1.25)
	defer e.SetSizeScale("Noto Kufi Arabic", 1)
	for _, n := range []string{"Noto Kufi Arabic", "notokufiarabic", "ui-term-arabic-sans"} {
		if got := e.SizeScale(n); got != 1.25 {
			t.Errorf("SizeScale(%q) = %v, want 1.25", n, got)
		}
	}
	if got := e.SizeScale("Noto Sans Mono"); got != 1 {
		t.Errorf("an unscaled face reports %v, want 1", got)
	}
}
