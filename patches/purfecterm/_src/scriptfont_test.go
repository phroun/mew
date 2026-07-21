package purfecterm

import "testing"

// ScriptClass buckets runes for the script-font map.
func TestScriptClass(t *testing.T) {
	cases := map[rune]string{
		'A': "", '5': "", 0x2500 /*box*/ : "",
		0x05D0: "hebrew", 0xFB2A: "hebrew",
		0x0627: "arabic", 0xFEDF: "arabic",
		0x4E00: "cjk", 0xAC00: "cjk", 0x30AB: "cjk", 0x20000: "cjk",
	}
	for r, want := range cases {
		if got := ScriptClass(r); got != want {
			t.Errorf("ScriptClass(U+%04X) = %q, want %q", r, got, want)
		}
	}
}

// The per-terminal script-font map: set, read (case-insensitive class), clear
// one, clear all.
func TestScriptFontMap(t *testing.T) {
	b := NewBuffer(20, 4, 100)
	b.SetScriptFont("hebrew", "Noto Serif Hebrew")
	b.SetScriptFont("CJK", "Noto Sans CJK SC") // class lower-cased

	if got := b.GetScriptFont("hebrew"); got != "Noto Serif Hebrew" {
		t.Fatalf("hebrew = %q", got)
	}
	if got := b.GetScriptFont("cjk"); got != "Noto Sans CJK SC" {
		t.Fatalf("cjk = %q", got)
	}
	if got := b.GetScriptFont("arabic"); got != "" {
		t.Fatalf("unset arabic should be empty, got %q", got)
	}
	b.SetScriptFont("hebrew", "") // clear one
	if got := b.GetScriptFont("hebrew"); got != "" {
		t.Fatalf("cleared hebrew should be empty, got %q", got)
	}
	b.ClearScriptFonts()
	if got := b.GetScriptFont("cjk"); got != "" {
		t.Fatalf("ClearScriptFonts should empty cjk, got %q", got)
	}
}

// OSC 7005 configures the map: s (set), sd (clear one), sda (clear all).
func TestScriptFontOSC(t *testing.T) {
	b := NewBuffer(20, 4, 100)
	p := NewParser(b)
	p.Parse([]byte("\x1b]7005;s;arabic;Noto Naskh Arabic\x07"))
	if got := b.GetScriptFont("arabic"); got != "Noto Naskh Arabic" {
		t.Fatalf("OSC set arabic -> %q", got)
	}
	p.Parse([]byte("\x1b]7005;s;cjk;Noto Serif CJK SC\x07"))
	p.Parse([]byte("\x1b]7005;sd;arabic\x07"))
	if got := b.GetScriptFont("arabic"); got != "" {
		t.Fatalf("OSC sd;arabic should clear, got %q", got)
	}
	if got := b.GetScriptFont("cjk"); got != "Noto Serif CJK SC" {
		t.Fatalf("cjk should remain, got %q", got)
	}
	p.Parse([]byte("\x1b]7005;sda\x07"))
	if got := b.GetScriptFont("cjk"); got != "" {
		t.Fatalf("OSC sda should clear all, got %q", got)
	}
}
