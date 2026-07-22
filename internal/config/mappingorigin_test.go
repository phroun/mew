package config

import (
	"os"
	"testing"
)

// Provenance (source file, line, @author, load-order precedence) survives the
// @include flatten. @author is stateful going forward, resets to blank when an
// include is entered, and resumes in the parent afterward.
func TestMappingProvenanceThroughInclude(t *testing.T) {
	m := &Manager{configDir: "/std"}
	m.SetIncludeReader(func(path string) ([]byte, error) {
		if path == "more.conf" {
			// Included file: starts with a BLANK author (does not inherit the
			// parent's "Alice"), then declares Carol partway through.
			return []byte("[mappings:mew]\n" +
				"^D\t=cmd_d\n" +
				"@author Carol\n" +
				"^E\t=cmd_e\n"), nil
		}
		return nil, os.ErrNotExist
	})

	cfg := m.LoadFromString(
		"@author Alice\n" + // line 1
			"[mappings:mew]\n" + // line 2
			"^A\t=cmd_a\n" + // line 3  (Alice)
			"@include \"more.conf\"\n" + // line 4
			"^B\t=cmd_b\n" + // line 5  (Alice resumes)
			"@author Bob\n" + // line 6
			"^C\t=cmd_c\n") // line 7  (Bob)

	want := map[string]struct {
		source string
		line   int
		author string
	}{
		"^A": {"<config>", 3, "Alice"},
		"^B": {"<config>", 5, "Alice"},     // author resumed after the include
		"^C": {"<config>", 7, "Bob"},       // author changed mid-file
		"^D": {"more.conf", 2, "Customized"}, // blank @author -> load default
		"^E": {"more.conf", 4, "Carol"},    // include declared its own author
	}
	for seq, w := range want {
		o, ok := cfg.MappingOrigins[seq]
		if !ok {
			t.Fatalf("%s: no origin recorded", seq)
		}
		if o.Source != w.source || o.Line != w.line || o.Author != w.author {
			t.Errorf("%s origin = {%q, %d, %q}, want {%q, %d, %q}",
				seq, o.Source, o.Line, o.Author, w.source, w.line, w.author)
		}
	}

	// Precedence follows the flattened load order: ^A, then the include's ^D and
	// ^E, then ^B and ^C.
	order := []string{"^A", "^D", "^E", "^B", "^C"}
	for i := 1; i < len(order); i++ {
		prev, cur := cfg.MappingOrigins[order[i-1]], cfg.MappingOrigins[order[i]]
		if !(prev.Precedence < cur.Precedence) {
			t.Errorf("precedence: %s (%d) should precede %s (%d)",
				order[i-1], prev.Precedence, order[i], cur.Precedence)
		}
	}
}

// A key rebound later in the load carries the LATER binding's provenance (last
// configured wins), and a project layer outranks the user layer.
func TestMappingProvenanceLastConfiguredWins(t *testing.T) {
	m := &Manager{configDir: "/std"}
	cfg := DefaultConfig()
	prec := 0
	m.applyLayer(&cfg, "[mappings:mew]\n^A\t=first\n^A\t=second\n", "<user>", "", false, &prec)
	if cfg.Mappings["^A"] != "second" {
		t.Fatalf("last binding should win: %q", cfg.Mappings["^A"])
	}
	userPrec := cfg.MappingOrigins["^A"].Precedence

	m.applyLayer(&cfg, "[mappings:mew]\n^A\t=project\n", "<proj>", "", true, &prec)
	po := cfg.MappingOrigins["^A"]
	if cfg.Mappings["^A"] != "project" {
		t.Fatalf("project layer should override: %q", cfg.Mappings["^A"])
	}
	if po.Source != "<proj>" || po.Precedence <= userPrec {
		t.Fatalf("project origin should outrank user: %+v (user prec %d)", po, userPrec)
	}
}
