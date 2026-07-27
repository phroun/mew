package config

import "testing"

// A [window] ui_* value that names another UI alias may be written with
// underscores, matching the spelling of the key it sits beside: the internal
// alias is hyphenated, but ui_term_hebrew = ui_term_hebrew_sans should read
// naturally rather than forcing a spelling change across the equals sign.
func TestUIAliasTargetsAcceptUnderscores(t *testing.T) {
	for _, c := range []struct {
		name string
		in   []string
		want []string
	}{
		{"underscored ui target", []string{"ui_term_hebrew_sans"}, []string{"ui-term-hebrew-sans"}},
		{"hyphenated already", []string{"ui-term-hebrew-sans"}, []string{"ui-term-hebrew-sans"}},
		{"mixed spelling", []string{"ui_term-hebrew_sans"}, []string{"ui-term-hebrew-sans"}},
		{"case insensitive prefix", []string{"UI_Term_Serif"}, []string{"UI-Term-Serif"}},
		{"fallback list", []string{"ui_text_serif", "Noto Serif"},
			[]string{"ui-text-serif", "Noto Serif"}},
		// A real family may contain underscores and must survive verbatim —
		// only entries naming the ui tree are rewritten.
		{"plain family untouched", []string{"My_Custom_Mono"}, []string{"My_Custom_Mono"}},
		{"family merely containing ui", []string{"Fluid_UI_Mono"}, []string{"Fluid_UI_Mono"}},
	} {
		got := normalizeUITargets(append([]string(nil), c.in...))
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v, want %v", c.name, got, c.want)
				break
			}
		}
	}
}

// End to end through the config parser: the underscored target lands in
// FontAliases hyphenated, ready for the font engine.
func TestUIAliasUnderscoredTargetParses(t *testing.T) {
	cfg := DefaultConfig()
	m := &Manager{}
	prec := 0
	m.applyLayer(&cfg, "[window]\nui_term_hebrew = ui_term_hebrew_sans\n", "test", "", false, &prec)
	got := cfg.Window.FontAliases["ui-term-hebrew"]
	if len(got) != 1 || got[0] != "ui-term-hebrew-sans" {
		t.Fatalf("ui-term-hebrew = %v, want [ui-term-hebrew-sans]", got)
	}
}
