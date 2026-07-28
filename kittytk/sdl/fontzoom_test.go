//go:build sdl

package sdl

import (
	"testing"

	sdl2 "github.com/veandco/go-sdl2/sdl"
)

func guiKey(sym sdl2.Keycode, extraMod uint16) sdl2.Keysym {
	return sdl2.Keysym{Sym: sym, Mod: sdl2.KMOD_LGUI | extraMod}
}

// The zoom chords: Command/Meta with "+"/"=" grows a point, "-" shrinks,
// "0" returns to the configured default; keypad variants count; Shift rides
// along ("+" IS Shift+"=" on common layouts); Ctrl or Alt in the chord makes
// it an ordinary key again, as does a chord with no GUI modifier at all.
func TestZoomTargetChords(t *testing.T) {
	cases := []struct {
		name string
		sym  sdl2.Keysym
		want int
		ok   bool
	}{
		{"cmd equals", guiKey(sdl2.K_EQUALS, 0), 25, true},
		{"cmd shift equals (plus)", guiKey(sdl2.K_EQUALS, sdl2.KMOD_LSHIFT), 25, true},
		{"cmd plus", guiKey(sdl2.K_PLUS, 0), 25, true},
		{"cmd keypad plus", guiKey(sdl2.K_KP_PLUS, 0), 25, true},
		{"cmd minus", guiKey(sdl2.K_MINUS, 0), 23, true},
		{"cmd keypad minus", guiKey(sdl2.K_KP_MINUS, 0), 23, true},
		{"cmd zero", guiKey(sdl2.K_0, 0), 14, true},
		{"cmd keypad zero", guiKey(sdl2.K_KP_0, 0), 14, true},
		{"right-gui works too", sdl2.Keysym{Sym: sdl2.K_EQUALS, Mod: sdl2.KMOD_RGUI}, 25, true},
		{"no gui modifier", sdl2.Keysym{Sym: sdl2.K_EQUALS}, 0, false},
		{"ctrl breaks the chord", guiKey(sdl2.K_EQUALS, sdl2.KMOD_LCTRL), 0, false},
		{"alt breaks the chord", guiKey(sdl2.K_MINUS, sdl2.KMOD_LALT), 0, false},
		{"other key", guiKey(sdl2.K_a, 0), 0, false},
	}
	for _, c := range cases {
		got, ok := zoomTarget(c.sym, 24, 14)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: zoomTarget = (%d, %v), want (%d, %v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

// The dynamic size is capped to 4..100pt — stepping past either end holds,
// and a default outside the range restores to the nearest bound.
func TestZoomTargetCaps(t *testing.T) {
	if got, _ := zoomTarget(guiKey(sdl2.K_MINUS, 0), minFontPt, 12); got != minFontPt {
		t.Errorf("minus at the floor = %d, want %d", got, minFontPt)
	}
	if got, _ := zoomTarget(guiKey(sdl2.K_EQUALS, 0), maxFontPt, 12); got != maxFontPt {
		t.Errorf("plus at the ceiling = %d, want %d", got, maxFontPt)
	}
	if got, _ := zoomTarget(guiKey(sdl2.K_0, 0), 24, 2); got != minFontPt {
		t.Errorf("reset to an under-floor default = %d, want %d", got, minFontPt)
	}
}

// SetFontSize records the configured default the Cmd/Meta+0 chord restores,
// and bounds configured values to the zoom range; zero keeps its "unset, use
// the raster base" meaning.
func TestSetFontSizeRecordsDefault(t *testing.T) {
	p := New("t", 100, 100)
	p.SetFontSize(24)
	if p.fontSize != 24 || p.defaultFontSize != 24 {
		t.Fatalf("fontSize=%d default=%d, want 24/24", p.fontSize, p.defaultFontSize)
	}
	p.SetFontSize(500)
	if p.fontSize != maxFontPt {
		t.Errorf("oversize config = %d, want the %dpt cap", p.fontSize, maxFontPt)
	}
	p.SetFontSize(1)
	if p.fontSize != minFontPt {
		t.Errorf("undersize config = %d, want the %dpt floor", p.fontSize, minFontPt)
	}
	p.SetFontSize(0)
	if p.fontSize != 0 || p.defaultFontSize != 0 {
		t.Errorf("zero must stay the unset sentinel, got %d/%d", p.fontSize, p.defaultFontSize)
	}
}

// The full chord path on a platform with no windows yet: fontZoomKey consumes
// the chords and walks the live size, and Cmd/Meta+0 returns to the
// configured default rather than merely stepping toward it.
func TestFontZoomKeyWalksAndResets(t *testing.T) {
	p := New("t", 100, 100)
	p.SetFontSize(24)

	if !p.fontZoomKey(guiKey(sdl2.K_EQUALS, 0)) || p.fontSize != 25 {
		t.Fatalf("zoom in: consumed with size 25, got %d", p.fontSize)
	}
	for i := 0; i < 5; i++ {
		p.fontZoomKey(guiKey(sdl2.K_EQUALS, 0))
	}
	if p.fontSize != 30 {
		t.Fatalf("five more steps: %d, want 30", p.fontSize)
	}
	if !p.fontZoomKey(guiKey(sdl2.K_0, 0)) || p.fontSize != 24 {
		t.Fatalf("reset: want the configured 24, got %d", p.fontSize)
	}
	if p.fontZoomKey(sdl2.Keysym{Sym: sdl2.K_EQUALS}) {
		t.Fatal("a bare '=' must not be consumed")
	}

	// An unconfigured platform zooms from the 12pt raster base.
	q := New("t", 100, 100)
	if !q.fontZoomKey(guiKey(sdl2.K_MINUS, 0)) || q.fontSize != 11 {
		t.Fatalf("unconfigured zoom out: want 11, got %d", q.fontSize)
	}
	if !q.fontZoomKey(guiKey(sdl2.K_0, 0)) || q.fontSize != 12 {
		t.Fatalf("unconfigured reset: want the 12pt base, got %d", q.fontSize)
	}
}
