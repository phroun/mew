package config

import "testing"

// [options.<grammar>] sections parse into OptionOverlays with the trichotomy:
// a real value overrides, "default" resolves to the shipped default, and
// "inherit"/blank defer to the base [options] (absent from the overlay).
// syntax/syntaxDetect are excluded from overlays.
func TestOptionOverlayParsing(t *testing.T) {
	m := NewManager()
	c := m.LoadFromString(`[options]
tabSize=8
showLineNumbers=true

[options.cpp]
tabSize=2
showInvisibles=true
showLineNumbers=default
showColumnRuler=inherit
direction=
syntax=go
syntaxDetect=true
`)
	get := func(key string) (string, bool) { return c.ResolveOptionOverlay("", "cpp", "", key) }
	if v, _ := get("tabsize"); v != "2" {
		t.Errorf("tabsize override = %q, want 2", v)
	}
	if v, _ := get("showinvisibles"); v != "true" {
		t.Errorf("showinvisibles override = %q, want true", v)
	}
	// "default" resolves to the shipped default value for the option.
	shipped := defaultSectionValues("options")["showLineNumbers"]
	if v, _ := get("showlinenumbers"); v != shipped {
		t.Errorf("showlinenumbers=default resolved to %q, want shipped default %q", v, shipped)
	}
	// "inherit" and blank defer down the cascade: not supplied here.
	if _, ok := get("showcolumnruler"); ok {
		t.Error("inherit should defer down the cascade (not supplied)")
	}
	if _, ok := get("direction"); ok {
		t.Error("blank should defer down the cascade (not supplied)")
	}
	// syntax / syntaxDetect are excluded from the cascade entirely.
	if _, ok := get("syntax"); ok {
		t.Error("syntax must be excluded from an options overlay")
	}
	if _, ok := get("syntaxdetect"); ok {
		t.Error("syntaxDetect must be excluded from an options overlay")
	}
}

// The cascade resolves most-specific-first with precedence class > grammar >
// type, across all section forms.
func TestOptionCascadePrecedence(t *testing.T) {
	m := NewManager()
	c := m.LoadFromString(`[options]
tabSize=1

[options.cpp]
tabSize=2
[options.doc]
tabSize=3
[options.cpp.doc]
tabSize=4
[myclass.options]
tabSize=5
[myclass.options.cpp]
tabSize=6
`)
	cases := []struct {
		class, grammar, bufType, want string
	}{
		{"myclass", "cpp", "doc", "6"}, // [myclass.options.cpp] (class beats grammar+type)
		{"", "cpp", "doc", "4"},        // [options.cpp.doc]
		{"", "cpp", "tool", "2"},       // [options.cpp] (no cpp.tool)
		{"", "go", "doc", "3"},         // [options.doc] (type only)
		{"myclass", "go", "tool", "5"}, // [myclass.options] (class, no grammar/type match)
	}
	for _, tc := range cases {
		got, ok := c.ResolveOptionOverlay(tc.class, tc.grammar, tc.bufType, "tabsize")
		if !ok || got != tc.want {
			t.Errorf("Resolve(%q,%q,%q) = %q ok=%v, want %q", tc.class, tc.grammar, tc.bufType, got, ok, tc.want)
		}
	}
	// A grammar/type with no section at all falls through (base applies).
	if _, ok := c.ResolveOptionOverlay("", "go", "tool", "tabsize"); ok {
		t.Error("go/work should not resolve (fall through to base)")
	}
}

// A mapping set is refined by the class/grammar/type cascade with the same
// precedence as options (class > grammar > type).
func TestMappingSetGrammarCascade(t *testing.T) {
	m := NewManager()
	c := m.LoadFromString("[mappings.mew]\nk\t=base\n" +
		"[mappings.mew.cpp]\nk\t=grammar\n" +
		"[mappings.mew.doc]\nk\t=type\n" +
		"[mappings.mew.cpp.doc]\nk\t=grammartype\n" +
		"[panel.mappings.mew]\nk\t=class\n")
	get := func(class, grammar, bufType string) string {
		return c.ResolveMappingSet("mew", class, grammar, bufType, "", nil)["k"]
	}
	if got := get("", "", ""); got != "base" {
		t.Errorf("no qualifiers: got %q, want base", got)
	}
	if got := get("", "cpp", ""); got != "grammar" {
		t.Errorf("grammar: got %q, want grammar", got)
	}
	if got := get("", "", "doc"); got != "type" {
		t.Errorf("type: got %q, want type", got)
	}
	if got := get("", "cpp", "doc"); got != "grammartype" {
		t.Errorf("grammar+type: got %q, want grammartype", got)
	}
	// class outranks grammar and type even when they are more qualified.
	if got := get("panel", "cpp", "doc"); got != "class" {
		t.Errorf("class should win: got %q, want class", got)
	}
}

// A config with no [options.<grammar>] sections leaves OptionOverlays empty.
func TestNoOptionOverlays(t *testing.T) {
	m := NewManager()
	c := m.LoadFromString("[options]\ntabSize=4\n")
	if len(c.OptionOverlays) != 0 {
		t.Errorf("expected no overlays, got %v", c.OptionOverlays)
	}
}
