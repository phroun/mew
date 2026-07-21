package trinkets

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/text"
)

const zwjRune = "‍" // U+200D ZERO WIDTH JOINER

// arabicRenderString wraps the base letter in ZWJ per join side, and rebuilds
// lam-alef ligatures as their base pair.
func TestArabicRenderString(t *testing.T) {
	cases := []struct {
		name             string
		base, form       rune
		kashL, kashR     bool
		want             string
	}{
		{"isolated ain", 'ع', 0xFE8B, false, false, "ع"},
		{"initial ain (joins next)", 'ع', 0xFE8F, true, false, "ع" + zwjRune},
		{"final ain (joins prev)", 'ع', 0xFECA, false, true, zwjRune + "ع"},
		{"medial ain (both)", 'ع', 0xFECC, true, true, zwjRune + "ع" + zwjRune},
		{"lam-alef final (joins prev)", 'ل', 0xFEFC, false, true, zwjRune + "لا"},
		{"lam-alef isolated", 'ل', 0xFEFB, false, false, "لا"},
		{"lam-alef hamza-above", 'ل', 0xFEF7, false, false, "لأ"},
	}
	for _, c := range cases {
		if got := arabicRenderString(c.base, c.form, c.kashL, c.kashR); got != c.want {
			t.Errorf("%s: got %q (% X), want %q (% X)", c.name, got, []rune(got), c.want, []rune(c.want))
		}
	}
}

// Shaping the ZWJ-joined base letter yields the SAME rendered glyph as the
// legacy presentation-form codepoint on a face that carries both (the embedded
// Noto Naskh) — proving the ZWJ path is equivalent where the old path worked,
// while also working on faces (macOS system Arabic) that lack the presentation
// block. Compares shaped width and inked-pixel count.
func TestZWJShapingMatchesPresentationForm(t *testing.T) {
	term := NewPurfecTerm()
	term.SetTerminalFontFamily("ui-term")
	term.rotateGfxCaches()
	eng := term.gfxEngine()
	if eng == nil {
		t.Skip("no gfx engine")
	}
	const ppu = 2.0
	f := &core.Font{Name: "ui-term", Size: 22}

	measure := func(s string) (int, int) {
		sp := eng.ShapeRun(f, s)
		w := int(math.Round(float64(sp.Width()) * ppu))
		h := int(math.Round(float64(eng.LineHeight(f)) * ppu))
		ink := 0
		if w > 0 && h > 0 {
			img := image.NewRGBA(image.Rect(0, 0, w, h))
			text.Render(img, sp, 0, 0, ppu, color.RGBA{255, 255, 255, 255})
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					if img.RGBAAt(x, y).A != 0 {
						ink++
					}
				}
			}
		}
		return w, ink
	}

	cases := []struct {
		name         string
		zwj, presved string
	}{
		{"medial ain", zwjRune + "ع" + zwjRune, "ﻌ"},
		{"initial ain", "ع" + zwjRune, "ﻋ"},
		{"medial lam", zwjRune + "ل" + zwjRune, "ﻠ"},
		{"final meem", zwjRune + "م", "ﻢ"},
	}
	for _, c := range cases {
		zw, zi := measure(c.zwj)
		pw, pi := measure(c.presved)
		if zw != pw || zi != pi {
			t.Errorf("%s: ZWJ (w=%d ink=%d) != presentation form (w=%d ink=%d)",
				c.name, zw, zi, pw, pi)
		}
	}
}
