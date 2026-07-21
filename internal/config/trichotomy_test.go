package config

import (
	"path/filepath"
	"testing"
)

// layered builds a config from a user layer plus project layers applied on
// top, without touching the disk.
func layered(t *testing.T, user string, projects ...string) Config {
	t.Helper()
	m := &Manager{configDir: "/std"}
	cfg := DefaultConfig()
	m.applyLayer(&cfg, user, "", false)
	for _, p := range projects {
		m.applyLayer(&cfg, p, "", true)
	}
	return cfg
}

// "default" in a formats-family table restores the BUILT-IN entry that an
// earlier layer had overridden or removed; blank still hard-deletes.
func TestTrichotomyFormats(t *testing.T) {
	cfg := layered(t,
		"[formats]\nc = go\nmd =\n",
		"[formats]\nc = default\nmd = default\nxyz = default\n")
	if cfg.Formats["c"] != "cpp" {
		t.Fatalf("c = default should restore the builtin cpp, got %q", cfg.Formats["c"])
	}
	if cfg.Formats["md"] != "markdown" {
		t.Fatalf("md = default should restore the builtin after a deletion, got %q", cfg.Formats["md"])
	}
	if _, ok := cfg.Formats["xyz"]; ok {
		t.Fatal("default for a key with no builtin deletes it")
	}

	cfg = layered(t, "[formats]\nc = go\n", "[formats]\nc =\n")
	if _, ok := cfg.Formats["c"]; ok {
		t.Fatal("blank still hard-deletes")
	}
}

// Colors: "default" now works ACROSS layers (delete at merge, so the
// level's built-in resurfaces), distinct from "inherit" (defer down the
// class -> type -> global cascade) and from a real value.
func TestTrichotomyColors(t *testing.T) {
	// Project resets the user's global text color to the built-in.
	cfg := layered(t,
		"[colors]\ntext=\"USER\"\nmessages=\"KEEP\"\n",
		"[colors]\ntext = default\n")
	if _, ok := cfg.Colors.Global["text"]; ok {
		t.Fatal("default should delete the merged key so the builtin applies")
	}
	if cfg.Colors.Global["messages"] != "KEEP" {
		t.Fatal("unstated keys must survive")
	}
	if got := cfg.Colors.Resolve("", "main", "text"); got != defaultGlobalColors["text"] {
		t.Fatalf("resolution should reach the builtin, got %q", got)
	}

	// At the class level, "inherit" defers DOWN the cascade while "default"
	// restores the class's own builtin — observably different for the
	// notification class, which has a builtin messages color.
	inh := layered(t,
		"[notification::colors]\nmessages=\"X\"\n",
		"[notification::colors]\nmessages = inherit\n")
	if got := inh.Colors.Resolve("notification", "work", "messages"); got == defaultClassColors["notification"]["messages"] {
		t.Fatal("inherit must skip the class level, not restore its builtin")
	}
	def := layered(t,
		"[notification::colors]\nmessages=\"X\"\n",
		"[notification::colors]\nmessages = default\n")
	if got := def.Colors.Resolve("notification", "work", "messages"); got != defaultClassColors["notification"]["messages"] {
		t.Fatalf("default must restore the class builtin, got %q", got)
	}
}

// Syntax class maps: default deletes (falling through to the builtin
// conventions); blank stays as the mask state.
func TestTrichotomySyntaxMaps(t *testing.T) {
	cfg := layered(t,
		"[syntax]\nComment = syntaxString\n",
		"[syntax]\nComment = default\n")
	if _, ok := cfg.SyntaxMaps[""]["comment"]; ok {
		t.Fatal("default should delete the map entry")
	}

	cfg = layered(t, "", "[syntax.cpp]\nComment =\n")
	if v, ok := cfg.SyntaxMaps["cpp"]["comment"]; !ok || v != "" {
		t.Fatal("blank must persist as the mask state")
	}
}

// Mappings: a project layer unbinds with default or blank; the user layer's
// own keyword entries are dropped too.
func TestTrichotomyMappings(t *testing.T) {
	cfg := layered(t,
		"[mappings:mew]\n^Q\t=user_cmd\n^R\t=other_cmd\n",
		"[mappings:mew]\n^Q\t=default\n^W\t=nop\n")
	if _, ok := cfg.Mappings["^Q"]; ok {
		t.Fatal("project default should unbind the user's key")
	}
	if cfg.Mappings["^R"] != "other_cmd" || cfg.Mappings["^W"] != "nop" {
		t.Fatalf("other bindings should survive/merge: %v", cfg.Mappings)
	}

	cfg = layered(t, "[mappings:mew]\n^Q\t=user_cmd\n^T\t=\n")
	if _, ok := cfg.Mappings["^T"]; ok {
		t.Fatal("a blank user binding is an unbind, not an empty command")
	}
}

// Scalars: blank ints/bools inherit instead of smuggling in the parse
// fallback; "default" restores the shipped value; syntax="" keeps meaning
// "highlighting off".
func TestTrichotomyScalars(t *testing.T) {
	cfg := layered(t,
		"[options]\ntabSize=3\nsyntax=go\n",
		"[options]\ntabSize=\n")
	if cfg.General.TabSize != 3 {
		t.Fatalf("blank tabSize must inherit, got %d", cfg.General.TabSize)
	}

	cfg = layered(t,
		"[options]\ntabSize=3\n",
		"[options]\ntabSize=default\n")
	if cfg.General.TabSize != 4 {
		t.Fatalf("tabSize=default must restore the shipped 4, got %d", cfg.General.TabSize)
	}

	cfg = layered(t,
		"[options]\nsyntax=go\n",
		"[options]\nsyntax=\n")
	if cfg.General.Syntax != "" {
		t.Fatalf("blank syntax stays meaningful (off), got %q", cfg.General.Syntax)
	}

	cfg = layered(t,
		"[options]\nshowLineNumbers=false\n",
		"[options]\nshowLineNumbers=\n")
	if cfg.General.ShowLineNumbers {
		t.Fatal("blank bool must inherit the user's false")
	}
}

// Indicators: blank inherits; default restores the shipped glyph.
func TestTrichotomyIndicators(t *testing.T) {
	cfg := layered(t,
		"[indicators]\ncursorGhost=\"!\"\n",
		"[indicators]\ncursorGhost=\n")
	if cfg.Indicators.CursorGhost != "!" {
		t.Fatalf("blank indicator must inherit, got %q", cfg.Indicators.CursorGhost)
	}
	cfg = layered(t,
		"[indicators]\ncursorGhost=\"!\"\n",
		"[indicators]\ncursorGhost=default\n")
	if cfg.Indicators.CursorGhost != "|" {
		t.Fatalf("default indicator must restore the shipped |, got %q", cfg.Indicators.CursorGhost)
	}
}

// Storage: default resets to the system temp location; relative paths still
// resolve against their layer.
func TestTrichotomyStorage(t *testing.T) {
	m := &Manager{configDir: "/std"}
	cfg := DefaultConfig()
	m.applyLayer(&cfg, "[storage]\nscratch=backups\n", filepath.Join("/proj", ".mew"), true)
	if cfg.Storage.Scratch != filepath.Join("/proj", ".mew", "backups") {
		t.Fatalf("relative scratch: %q", cfg.Storage.Scratch)
	}
	m.applyLayer(&cfg, "[storage]\nscratch=default\n", "", true)
	if cfg.Storage.Scratch != "" {
		t.Fatalf("scratch=default must reset to system temp, got %q", cfg.Storage.Scratch)
	}
}

// A QUOTED keyword is the literal text, not the keyword.
func TestTrichotomyQuotingEscapes(t *testing.T) {
	cfg := layered(t, "[formats]\nweird = \"default\"\n")
	if cfg.Formats["weird"] != "default" {
		t.Fatalf("quoted keyword must be literal, got %q", cfg.Formats["weird"])
	}
}
