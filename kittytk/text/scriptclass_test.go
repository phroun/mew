package text

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// Script-class resolution: a glyph the primary (mono) can't render is routed to
// the ui-term-<class> face for its script — CJK/Hebrew/Arabic — before the
// general chain. The embedded defaults are Noto Sans CJK SC, Noto Serif Hebrew,
// and Noto Naskh Arabic.
func TestScriptClassDefaults(t *testing.T) {
	db := newFontDB()
	fm := fallbackMap{db: db, primary: db.resolve(&core.Font{Name: "Noto Sans Mono"})}
	cases := []struct {
		r    rune
		name string
		want string
	}{
		{0x4E00, "Han 一", "Noto Sans CJK SC"},
		{0xAC00, "Hangul 가", "Noto Sans CJK SC"},
		{0x3042, "Hiragana あ", "Noto Sans CJK SC"},
		{0x05D0, "Hebrew alef", "Noto Serif Hebrew"},
		{0xFEDF, "Arabic lam-initial", "Noto Naskh Arabic"},
	}
	for _, c := range cases {
		if got := familyName(fm.ResolveFace(c.r)); got != c.want {
			t.Errorf("%s (U+%04X) -> %q, want %q", c.name, c.r, got, c.want)
		}
	}
}

// scriptClassAlias classifies runes; non-RTL/CJK scripts have no class.
func TestScriptClassAlias(t *testing.T) {
	cases := map[rune]string{
		'A': "", '5': "", 0x00E9 /*é*/ : "",
		0x05D0: "ui-term-hebrew", 0xFB2A /*heb pres*/ : "ui-term-hebrew",
		0x0627: "ui-term-arabic", 0xFEDF: "ui-term-arabic",
		0x4E00: "ui-term-cjk", 0xAC00: "ui-term-cjk", 0x30AB: "ui-term-cjk",
		0x20000: "ui-term-cjk",
	}
	for r, want := range cases {
		if got := scriptClassAlias(r); got != want {
			t.Errorf("scriptClassAlias(U+%04X) = %q, want %q", r, got, want)
		}
	}
}

// The class aliases are redefinable: pointing ui-term-arabic at Kufi makes
// Arabic resolve there instead of the Naskh default.
func TestScriptClassAliasRedefinable(t *testing.T) {
	e := NewEngine()
	e.SetFontAlias("ui-term-arabic", "Noto Kufi Arabic")
	fm := fallbackMap{db: e.db, primary: e.db.resolve(&core.Font{Name: "Noto Sans Mono"})}
	if got := familyName(fm.ResolveFace(0xFEDF)); got != "Noto Kufi Arabic" {
		t.Errorf("after re-aliasing ui-term-arabic, Arabic -> %q, want Noto Kufi Arabic", got)
	}
	// Hebrew and CJK untouched.
	if got := familyName(fm.ResolveFace(0x05D0)); got != "Noto Serif Hebrew" {
		t.Errorf("Hebrew default changed unexpectedly: %q", got)
	}
}
