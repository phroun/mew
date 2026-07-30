package config

import (
	"strings"
	"testing"
)

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

// A [fonts] value is an optional path followed by an optional PSL group of
// per-face corrections, keyed by the family on the left of the equals sign —
// so a correction attaches to the FACE and applies however the face is
// reached, unlike the alias-keyed ui_* map.
func TestFontsSectionFaceAdjust(t *testing.T) {
	for _, c := range []struct {
		name, line string
		wantPath   string
		wantAdj    int
		hasAdj     bool
	}{
		{"path and adjustment", "Noto Kufi Arabic = k.ttf (baseline: -6)", "k.ttf", -6, true},
		{"adjustment alone", "Noto Kufi Arabic = (baseline: -6)", "", -6, true},
		{"positive signed", "Noto Kufi Arabic = (baseline: +2)", "", 2, true},
		{"path alone", "Noto Kufi Arabic = k.ttf", "k.ttf", 0, false},
		// A parenthesised tail that is not an adjustment stays part of the path.
		{"parens in a path", "Noto Kufi Arabic = /Fonts (old)/k.ttf", "/Fonts (old)/k.ttf", 0, false},
	} {
		path, adj, has := splitFacePath(c.line[strings.Index(c.line, "=")+1:])
		path = strings.TrimSpace(path)
		if path != c.wantPath || has != c.hasAdj || (has && adj.Baseline != c.wantAdj) {
			t.Errorf("%s: path=%q adj=%+v has=%v, want path=%q baseline=%d has=%v",
				c.name, path, adj, has, c.wantPath, c.wantAdj, c.hasAdj)
		}
	}
}

// The family key is canonical, so a correction cannot miss on spelling.
func TestFaceAdjustKeyIsCanonical(t *testing.T) {
	for _, n := range []string{"Noto Kufi Arabic", "noto-kufi-arabic", "NotoKufiArabic", "noto_kufi_arabic"} {
		if got := canonicalFace(n); got != "notokufiarabic" {
			t.Errorf("canonicalFace(%q) = %q", n, got)
		}
	}
}

// End to end: the correction lands in FontAdjust, and a path-only line leaves
// no stale correction behind.
func TestFontsAdjustParses(t *testing.T) {
	cfg := DefaultConfig()
	m := &Manager{}
	prec := 0
	m.applyLayer(&cfg, "[fonts]\nNoto Kufi Arabic = (baseline: -6)\n", "test", "", false, &prec)
	got := cfg.FontAdjust["notokufiarabic"]
	if !got.HasBaseline || got.Baseline != -6 {
		t.Fatalf("FontAdjust = %+v, want baseline -6", got)
	}
	if _, ok := cfg.Fonts["Noto Kufi Arabic"]; ok {
		t.Error("an adjustment-only line must not register a font path")
	}
}

// The size multiplier parses alongside baseline, accepts either the decimal
// or the percent spelling, and both keys may ride one group.
func TestFaceAdjustSize(t *testing.T) {
	for _, c := range []struct {
		name, val string
		wantScale float64
		wantBase  int
		hasScale  bool
		hasBase   bool
	}{
		{"decimal", "(size: 1.1)", 1.1, 0, true, false},
		{"percent", "(size: 110%)", 1.1, 0, true, false},
		{"shrink", "(size: 0.9)", 0.9, 0, true, false},
		{"both keys", "(baseline: -6, size: 1.25)", 1.25, -6, true, true},
		{"baseline only", "(baseline: -6)", 0, -6, false, true},
		{"nonsense size ignored", "(size: nope)", 0, 0, false, false},
		{"zero refused", "(size: 0)", 0, 0, false, false},
	} {
		_, adj, has := splitFacePath(c.val)
		if has != (c.hasScale || c.hasBase) {
			t.Errorf("%s: recognised=%v, want %v", c.name, has, c.hasScale || c.hasBase)
			continue
		}
		if adj.HasScale != c.hasScale || adj.HasBaseline != c.hasBase {
			t.Errorf("%s: hasScale=%v hasBaseline=%v, want %v/%v",
				c.name, adj.HasScale, adj.HasBaseline, c.hasScale, c.hasBase)
			continue
		}
		if c.hasScale && (adj.Scale < c.wantScale-0.0001 || adj.Scale > c.wantScale+0.0001) {
			t.Errorf("%s: scale %v, want %v", c.name, adj.Scale, c.wantScale)
		}
		if c.hasBase && adj.Baseline != c.wantBase {
			t.Errorf("%s: baseline %d, want %d", c.name, adj.Baseline, c.wantBase)
		}
	}
}
