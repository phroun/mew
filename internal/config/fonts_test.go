package config

import "testing"

// [fonts] maps a family name to a font file path; [window] fonts_path and
// ui_term configure the search directories and the ui-term alias fallback
// list. LoadFromString has an empty layer base, so absolute paths pass
// through unchanged.
func TestFontConfigParse(t *testing.T) {
	m := NewManager()
	c := m.LoadFromString(`
[fonts]
JetBrainsMono = /usr/share/fonts/jbm.ttf
Comic Mono = "/opt/fonts/comic mono.otf"

[window]
fonts_path = /a/fonts, "/b/more fonts"
ui_term = "JetBrainsMono, Monday"
`)

	if got := c.Fonts["JetBrainsMono"]; got != "/usr/share/fonts/jbm.ttf" {
		t.Errorf("Fonts[JetBrainsMono] = %q", got)
	}
	if got := c.Fonts["Comic Mono"]; got != "/opt/fonts/comic mono.otf" {
		t.Errorf("Fonts[Comic Mono] = %q (quotes should be stripped)", got)
	}
	if len(c.Window.FontsPath) != 2 || c.Window.FontsPath[0] != "/a/fonts" || c.Window.FontsPath[1] != "/b/more fonts" {
		t.Errorf("Window.FontsPath = %v, want [/a/fonts /b/more fonts]", c.Window.FontsPath)
	}
	if len(c.Window.UITerm) != 2 || c.Window.UITerm[0] != "JetBrainsMono" || c.Window.UITerm[1] != "Monday" {
		t.Errorf("Window.UITerm = %v, want [JetBrainsMono Monday]", c.Window.UITerm)
	}
}

// The per-script terminal font aliases parse into their own fallback lists.
func TestFontConfigScriptClasses(t *testing.T) {
	m := NewManager()
	c := m.LoadFromString(`
[window]
ui_term_cjk = "Noto Sans Mono CJK JP, Noto Sans CJK SC"
ui_term_hebrew = SBL Hebrew
ui_term_arabic = "Noto Kufi Arabic"
`)
	if len(c.Window.UITermCJK) != 2 || c.Window.UITermCJK[0] != "Noto Sans Mono CJK JP" {
		t.Errorf("UITermCJK = %v", c.Window.UITermCJK)
	}
	if len(c.Window.UITermHebrew) != 1 || c.Window.UITermHebrew[0] != "SBL Hebrew" {
		t.Errorf("UITermHebrew = %v", c.Window.UITermHebrew)
	}
	if len(c.Window.UITermArabic) != 1 || c.Window.UITermArabic[0] != "Noto Kufi Arabic" {
		t.Errorf("UITermArabic = %v", c.Window.UITermArabic)
	}
}

// A single unquoted ui_term value is a one-element fallback list; a plain
// fonts_path is one directory.
func TestFontConfigSingleValues(t *testing.T) {
	m := NewManager()
	c := m.LoadFromString(`
[window]
ui_term = JetBrainsMono
fonts_path = /only/one
`)
	if len(c.Window.UITerm) != 1 || c.Window.UITerm[0] != "JetBrainsMono" {
		t.Errorf("Window.UITerm = %v, want [JetBrainsMono]", c.Window.UITerm)
	}
	if len(c.Window.FontsPath) != 1 || c.Window.FontsPath[0] != "/only/one" {
		t.Errorf("Window.FontsPath = %v, want [/only/one]", c.Window.FontsPath)
	}
}

// A blank/inherit ui_term or fonts_path clears back to the built-in default,
// and a blank [fonts] entry removes it.
func TestFontConfigClears(t *testing.T) {
	m := NewManager()
	c := m.LoadFromString(`
[fonts]
Gone =

[window]
ui_term =
fonts_path = inherit
`)
	if _, ok := c.Fonts["Gone"]; ok {
		t.Errorf("blank [fonts] entry should be dropped, got %q", c.Fonts["Gone"])
	}
	if c.Window.UITerm != nil {
		t.Errorf("blank ui_term should clear, got %v", c.Window.UITerm)
	}
	if c.Window.FontsPath != nil {
		t.Errorf("inherit fonts_path should clear, got %v", c.Window.FontsPath)
	}
}

// splitFontList handles whole-quoted lists, per-element trimming, and empties.
func TestSplitFontList(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{`"A, B, C"`, []string{"A", "B", "C"}},
		{`A , B`, []string{"A", "B"}},
		{`  Single  `, []string{"Single"}},
		{`A,,B`, []string{"A", "B"}},
		{``, nil},
		{`inherit`, nil},
	}
	for _, tc := range cases {
		got := splitFontList(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("splitFontList(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("splitFontList(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}
