package config

import (
	"strings"
	"testing"
)

// The built-in defaults must include working key mappings even when no
// config file is ever read (regression: DefaultConfig once returned empty
// mappings, leaving headless/embedded sessions with dead keys).
func TestDefaultConfigHasBuiltinMappings(t *testing.T) {
	c := DefaultConfig()
	if len(c.Mappings) == 0 {
		t.Fatal("DefaultConfig should carry the built-in key mappings")
	}
	if c.Mappings["^K F"] != "find" {
		t.Fatalf("^K F should map to find, got %q", c.Mappings["^K F"])
	}
	if c.Mappings["^C"] == "" {
		t.Fatal("^C should be mapped")
	}
}

func TestGeneralParsing(t *testing.T) {
	m := NewManager()
	c := m.LoadFromString(`[options]
showLineNumbers=false
tabSize=8
searchIgnoreCase=true
searchWrap=false
searchRegex=true
modebarLocation=bottom
promptTimeout=60
scriptTimeout=0
`)
	g := c.General
	if g.ShowLineNumbers || g.TabSize != 8 {
		t.Fatalf("basic options: %+v", g)
	}
	if !g.SearchIgnoreCase || g.SearchWrap || !g.SearchRegex {
		t.Fatalf("search options: %+v", g)
	}
	if g.ModebarLocation != "bottom" {
		t.Fatalf("modebarLocation: %q", g.ModebarLocation)
	}
	if g.PromptTimeout != 60 || g.ScriptTimeout != 0 {
		t.Fatalf("timeouts: prompt=%d script=%d", g.PromptTimeout, g.ScriptTimeout)
	}
}

func TestGeneralDefaultsWhenAbsent(t *testing.T) {
	m := NewManager()
	c := m.LoadFromString("[general]\n")
	g := c.General
	if g.ModebarLocation != "top" {
		t.Fatalf("modebarLocation default: %q", g.ModebarLocation)
	}
	if !g.SearchWrap {
		t.Fatal("searchWrap should default true")
	}
	if g.PromptTimeout != 300 || g.ScriptTimeout != 300 {
		t.Fatalf("timeout defaults: prompt=%d script=%d", g.PromptTimeout, g.ScriptTimeout)
	}
}

func TestInvalidValuesKeepDefaults(t *testing.T) {
	m := NewManager()
	c := m.LoadFromString(`[options]
modebarLocation=sideways
promptTimeout=-5
`)
	if c.General.ModebarLocation != "top" {
		t.Fatalf("invalid modebarLocation should keep default: %q", c.General.ModebarLocation)
	}
	if c.General.PromptTimeout != 300 {
		t.Fatalf("negative promptTimeout should keep default: %d", c.General.PromptTimeout)
	}
}

// Section headers tolerate trailing comments, and mid-line comments use #.
func TestSectionHeaderTrailingComment(t *testing.T) {
	m := NewManager()
	c := m.LoadFromString("[options] # settings live here\ntabSize=6 # six wide\n")
	if c.General.TabSize != 6 {
		t.Fatalf("tabSize with comments: %d", c.General.TabSize)
	}
}

// The generated default config must parse cleanly back to the same
// effective defaults (it is the documentation of record).
func TestGeneratedConfigRoundTrip(t *testing.T) {
	m := NewManager()
	c := m.LoadFromString(m.generateDefaultConfig())
	d := DefaultConfig().General
	if c.General != d {
		t.Fatalf("generated config drifted from defaults:\n got %+v\nwant %+v", c.General, d)
	}
	if len(c.Mappings) == 0 || c.Mappings["^K F"] != "find" {
		t.Fatal("generated config mappings missing")
	}
}

// The [storage] section is honored only from locally loaded config.
func TestStorageScratchParsing(t *testing.T) {
	m := NewManager()
	c := m.LoadFromString("[storage]\nscratch=/tmp/somewhere\n")
	if c.Storage.Scratch != "/tmp/somewhere" {
		t.Fatalf("scratch: %q", c.Storage.Scratch)
	}
}

func TestColorSchemeResolveCascade(t *testing.T) {
	m := NewManager()
	c := m.LoadFromString(strings.Join([]string{
		"[colors]",
		`text="\e[0m"`,
		"[colors.doc]",
		`text="\e[1m"`,
	}, "\n"))
	// Buffer-type layer overrides global for main buffers...
	if got := c.Colors.Resolve("", "doc", "text"); got != "\x1b[1m" {
		t.Fatalf("main text color: %q", got)
	}
	// ...and the global layer resolves directly. (A type with a built-in
	// default color keeps it unless the user overrides at that level: the
	// cascade is class -> buffer type -> global, merged per level over the
	// built-in defaults.)
	if got := c.Colors.Resolve("", "", "text"); got != "\x1b[0m" {
		t.Fatalf("global text color: %q", got)
	}
}
