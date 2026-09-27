package tui

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// A host inside this one paints its own background, and how it names black is
// its business: an index, a palette entry, or channels. Whichever it chose,
// black is the ground the terminal already shows, so a blank wearing it has
// nothing to draw -- and reading only the named one stood a shade on every
// space of a row past the drift.
func TestABlankOnADarkGroundDrawsNoShade(t *testing.T) {
	t.Cleanup(func() {
		core.ForgetHostBidi()
		core.SetRtlMarkMode("")
	})
	core.SetHostAppliesBidi(true, false, true)

	// A pointed letter opens the drift; the spaces after it are the content a
	// hosted editor writes on its own black.
	row := func(bg style.Color) string {
		b, out := newTestTUI(20, 2)
		b.colorDepth = 256
		b.BeginFrame()
		b.DrawText(0, 0, "לִ", style.DefaultStyle(), nil)
		b.DrawText(b.metrics.CellToUnitsX(1), 0, "   ",
			style.DefaultStyle().WithFg(style.ColorWhite).WithBg(bg), nil)
		b.EndFrame()
		return out.String()
	}

	for _, c := range []struct {
		name string
		bg   style.Color
	}{
		{"the named black", style.ColorBlack},
		{"black said in channels", style.RGB(0, 0, 0)},
		{"the cube's own black", style.Color256(16)},
		{"a near-black a theme would write", style.RGB(30, 30, 46)},
		{"the greyscale ramp's foot", style.Color256(232)},
	} {
		if got := row(c.bg); strings.ContainsRune(got, fallbackBlank) {
			t.Errorf("%s: spaces on it became %c: %q", c.name, fallbackBlank, got)
		}
	}

	// A ground with light in it still draws one, so the checks above say
	// something about black rather than about the shade being switched off.
	if got := row(style.ColorBlue); !strings.ContainsRune(got, fallbackBlank) {
		t.Errorf("a blue ground past the drift drew nothing in its place: %q", got)
	}
}
