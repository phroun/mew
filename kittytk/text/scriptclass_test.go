package text

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// scriptClass classifies a rune; western and other scripts have no class.
func TestScriptClass(t *testing.T) {
	cases := map[rune]string{
		'A': "", '5': "", 0x00E9 /*é*/ : "",
		0x05D0: "hebrew", 0xFB2A /*heb pres*/ : "hebrew",
		0x0627: "arabic", 0xFEDF: "arabic",
		0x4E00: "cjk", 0xAC00: "cjk", 0x30AB: "cjk", 0x20000: "cjk",
	}
	for r, want := range cases {
		if got := scriptClass(r); got != want {
			t.Errorf("scriptClass(U+%04X) = %q, want %q", r, got, want)
		}
	}
}

// scriptContext derives root+style from a UI font name.
func TestScriptContext(t *testing.T) {
	cases := []struct{ name, root, style string }{
		{"ui-text", "ui-text", ""},
		{"ui-text-sans", "ui-text", "sans"},
		{"ui-text-serif", "ui-text", "serif"},
		{"ui-term", "ui-term", ""},
		{"ui-term-serif", "ui-term", "serif"},
		{"JetBrainsMono", "", ""},
	}
	for _, c := range cases {
		if root, style := scriptContext(c.name); root != c.root || style != c.style {
			t.Errorf("scriptContext(%q) = (%q,%q), want (%q,%q)", c.name, root, style, c.root, c.style)
		}
	}
}

// A ui-term primary (no style) pulls each script's own DEFAULT-style face.
func TestScriptFallbackTermDefault(t *testing.T) {
	db := newFontDB()
	fm := db.fallbackFor(&core.Font{Name: "ui-term"})
	cases := map[rune]string{
		0x4E00: "Noto Sans CJK SC",    // cjk -> sans default
		0x05D0: "Noto Serif Hebrew",   // hebrew -> serif default
		0xFEDF: "Noto Naskh Arabic",   // arabic -> serif(=Naskh) default
	}
	for r, want := range cases {
		if got := familyName(fm.ResolveFace(r)); got != want {
			t.Errorf("ui-term U+%04X -> %q, want %q", r, got, want)
		}
	}
}

// The serif/sans tristate: a serif primary pulls serif script faces, a sans
// primary pulls sans ones — the same root, no font names needed by the caller.
func TestScriptFallbackStyleTristate(t *testing.T) {
	db := newFontDB()
	serif := db.fallbackFor(&core.Font{Name: "ui-text-serif"})
	sans := db.fallbackFor(&core.Font{Name: "ui-text-sans"})
	cases := []struct {
		r                rune
		wantSerif, wantSans string
	}{
		{0x05D0, "Noto Serif Hebrew", "Noto Sans Hebrew"},
		{0xFEDF, "Noto Naskh Arabic", "Noto Kufi Arabic"}, // arabic: serif=Naskh, sans=Kufi
		// The serif CJK is registered as "Noto Serif CJK SC" but the embedded
		// file is the byte-identical Adobe co-release, which self-reports this
		// family name via Describe(); resolution by the Noto name still works.
		{0x4E00, "Source Han Serif SC", "Noto Sans CJK SC"},
	}
	for _, c := range cases {
		if got := familyName(serif.ResolveFace(c.r)); got != c.wantSerif {
			t.Errorf("ui-text-serif U+%04X -> %q, want %q", c.r, got, c.wantSerif)
		}
		if got := familyName(sans.ResolveFace(c.r)); got != c.wantSans {
			t.Errorf("ui-text-sans U+%04X -> %q, want %q", c.r, got, c.wantSans)
		}
	}
}

// Box-drawing / block elements have no serif form of their own; under a serif
// terminal face (Noto Serif, which lacks them) they fall through to Noto Sans
// Mono — the monospace face that fills the cell — never the proportional serif.
func TestBoxDrawingUsesMono(t *testing.T) {
	db := newFontDB()
	fm := db.fallbackFor(&core.Font{Name: "ui-term-serif"})
	for _, r := range []rune{0x2500, 0x2502, 0x250C, 0x2588, 0x2591, 0x2550} {
		if got := familyName(fm.ResolveFace(r)); got != "Noto Sans Mono" {
			t.Errorf("box-drawing U+%04X under ui-term-serif -> %q, want Noto Sans Mono", r, got)
		}
	}
}

// Overriding a single leaf reroutes only that (root,script,style) cell.
func TestScriptAliasLeafOverride(t *testing.T) {
	e := NewEngine()
	e.SetFontAlias("ui-term-arabic-serif", "Noto Kufi Arabic")
	fm := e.db.fallbackFor(&core.Font{Name: "ui-term"}) // arabic default = serif
	if got := familyName(fm.ResolveFace(0xFEDF)); got != "Noto Kufi Arabic" {
		t.Errorf("after overriding ui-term-arabic-serif, Arabic -> %q, want Noto Kufi Arabic", got)
	}
	// ui-text side and Hebrew untouched.
	txt := e.db.fallbackFor(&core.Font{Name: "ui-text"})
	if got := familyName(txt.ResolveFace(0xFEDF)); got != "Noto Naskh Arabic" {
		t.Errorf("ui-text Arabic changed unexpectedly: %q", got)
	}
	if got := familyName(fm.ResolveFace(0x05D0)); got != "Noto Serif Hebrew" {
		t.Errorf("Hebrew changed unexpectedly: %q", got)
	}
}
