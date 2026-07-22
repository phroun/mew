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
[options/doc]
tabSize=3
[options/doc.cpp]
tabSize=4
[myclass::options]
tabSize=5
[myclass::options.cpp]
tabSize=6
`)
	cases := []struct {
		class, grammar, bufType, want string
	}{
		{"myclass", "cpp", "doc", "6"}, // [myclass::options.cpp] (class beats grammar+type)
		{"", "cpp", "doc", "4"},        // [options/doc.cpp]
		{"", "cpp", "tool", "2"},       // [options.cpp] (no cpp+tool)
		{"", "go", "doc", "3"},         // [options/doc] (type only)
		{"myclass", "go", "tool", "5"}, // [myclass::options] (class, no grammar/type match)
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
	c := m.LoadFromString("[mappings:mew]\nk\t=base\n" +
		"[mappings:mew.cpp]\nk\t=grammar\n" +
		"[mappings:mew/doc]\nk\t=type\n" +
		"[mappings:mew/doc.cpp]\nk\t=grammartype\n" +
		"[panel::mappings:mew]\nk\t=class\n")
	get := func(class, grammar, bufType string) string {
		km, _ := c.ResolveMappingSet("mew", class, grammar, bufType, "", nil)
		return km["k"]
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

// parseSectionHeader splits the unified section grammar on its four distinct
// separators, so no component is ambiguous with another. The key case: "." is
// a grammar and "/" is a buffer type, so [options.tool] is a grammar named
// "tool" while [options/tool] is the tool buffer type.
func TestParseSectionHeader(t *testing.T) {
	cases := []struct {
		name                              string
		class, family, set, bufType, gram string
	}{
		{"options", "", "options", "", "", ""},
		{"options.tool", "", "options", "", "", "tool"}, // grammar, not type
		{"options/tool", "", "options", "", "tool", ""}, // type, not grammar
		{"options.cpp", "", "options", "", "", "cpp"},
		{"options/doc.cpp", "", "options", "", "doc", "cpp"},
		{"myclass::options", "myclass", "options", "", "", ""},
		{"myclass::options/tool.cpp", "myclass", "options", "", "tool", "cpp"},
		{"colors", "", "colors", "", "", ""},
		{"colors/tool", "", "colors", "", "tool", ""},
		{"modebar::colors", "modebar", "colors", "", "", ""},
		{"syntax", "", "syntax", "", "", ""},
		{"syntax.cpp", "", "syntax", "", "", "cpp"},
		{"mappings:mew", "", "mappings", "mew", "", ""},
		{"mappings:mew.cpp", "", "mappings", "mew", "", "cpp"},
		{"mappings:mew/doc.cpp", "", "mappings", "mew", "doc", "cpp"},
		{"panel::mappings:mew/tool.go", "panel", "mappings", "mew", "tool", "go"},
		{"formats.txt", "", "formats", "", "", "txt"},
	}
	for _, tc := range cases {
		h := parseSectionHeader(tc.name)
		if h.class != tc.class || h.family != tc.family || h.set != tc.set ||
			h.bufType != tc.bufType || h.grammar != tc.gram {
			t.Errorf("parseSectionHeader(%q) = {class:%q family:%q set:%q type:%q grammar:%q}, want {class:%q family:%q set:%q type:%q grammar:%q}",
				tc.name, h.class, h.family, h.set, h.bufType, h.grammar,
				tc.class, tc.family, tc.set, tc.bufType, tc.gram)
		}
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
