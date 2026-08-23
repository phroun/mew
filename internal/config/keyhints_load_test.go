package config

import (
	"strings"
	"testing"
)

// loadHinted parses a config in a declared environment, restoring whatever the
// process actually runs on afterwards.
func loadHinted(t *testing.T, env KeymapEnvironment, content string) Config {
	t.Helper()
	prev := CurrentKeymapEnvironment()
	SetKeymapEnvironment(env)
	t.Cleanup(func() { SetKeymapEnvironment(prev) })
	return NewManager().LoadFromString(content)
}

// The hint never reaches the keymap: two platforms' spellings of one binding
// have to merge and override each other as though the hints were not written,
// so what a mapping is filed under is the key as pressed.
func TestHintedMappingIsFiledUnderThePressedKey(t *testing.T) {
	cfg := loadHinted(t, KeymapEnvironment{OS: "darwin"}, `
[mappings:mew]
(mac) s-c = clipboard_copy
^C        = clipboard_copy
`)
	if cfg.Mappings["s-c"] != "clipboard_copy" {
		t.Errorf("s-c = %q, want clipboard_copy — the hint is not part of the key", cfg.Mappings["s-c"])
	}
	for k := range cfg.Mappings {
		if strings.Contains(k, "(") {
			t.Errorf("keymap holds %q; a hint must not survive into a map key", k)
		}
	}
	if got := cfg.MappingOrigins["s-c"].EnvWeight; got != 1 {
		t.Errorf("s-c EnvWeight = %d, want 1", got)
	}
	if got := cfg.MappingOrigins["^C"].EnvWeight; got != 0 {
		t.Errorf("^C EnvWeight = %d, want 0 for an unhinted binding", got)
	}
}

// A requiring hint drops the line as it is READ, which is the only place it can
// be dropped: once a key is in the map, a later layer inherits and overrides
// it, and "this platform never had that binding" stops being expressible.
func TestRequiringHintNeverEntersTheKeymap(t *testing.T) {
	content := `
[mappings:mew]
(only_mac) s-c  = clipboard_copy
(never_mac) ^C  = clipboard_copy
`
	onMac := loadHinted(t, KeymapEnvironment{OS: "darwin"}, content)
	if onMac.Mappings["s-c"] == "" {
		t.Error("s-c is unbound on a Mac, but (only_mac) holds there")
	}
	if _, ok := onMac.Mappings["^C"]; ok {
		t.Error("^C is bound on a Mac despite (never_mac)")
	}

	elsewhere := loadHinted(t, KeymapEnvironment{OS: "windows"}, content)
	if _, ok := elsewhere.Mappings["s-c"]; ok {
		t.Error("s-c is bound off a Mac despite (only_mac)")
	}
	if elsewhere.Mappings["^C"] == "" {
		t.Error("^C is unbound off a Mac, but (never_mac) holds there")
	}
}

// A plain hint moves no bindings, only their weight: both keys work on both
// platforms, which is the entire difference between the two hint families.
func TestPlainHintBindsEverywhere(t *testing.T) {
	content := `
[mappings:mew]
(mac) s-c = clipboard_copy
^C        = clipboard_copy
`
	for _, os := range []string{"darwin", "windows"} {
		cfg := loadHinted(t, KeymapEnvironment{OS: os}, content)
		if cfg.Mappings["s-c"] == "" || cfg.Mappings["^C"] == "" {
			t.Errorf("on %s: s-c=%q ^C=%q, want both bound", os, cfg.Mappings["s-c"], cfg.Mappings["^C"])
		}
	}
}

// The desktop the (kde)/(gnome) family is tested against comes from [window]
// host_type in this same file, which may be written AFTER the mappings that
// depend on it. It has to be found first, or a keymap would silently depend on
// the order its sections happen to sit in.
func TestHostTypeIsFoundBeforeTheMappingsThatNeedIt(t *testing.T) {
	cfg := loadHinted(t, KeymapEnvironment{OS: "linux", Desktop: "gnome"}, `
[mappings:mew]
(kde) ^W = window_close

[window]
host_type = plasma
`)
	if got := cfg.MappingOrigins["^W"].EnvWeight; got != 1 {
		t.Errorf("^W EnvWeight = %d, want 1 — host_type=plasma should have made (kde) match", got)
	}
}

// A keymap still written the old way binds cleanly and never fires, which is
// the worst way for a config to be wrong. It gets a line in the startup log
// naming the file, the line, and the new spelling.
func TestStaleLevelWordIsReported(t *testing.T) {
	cfg := loadHinted(t, KeymapEnvironment{OS: "linux"}, `
[pty::mappings]
capture ^C = tinput_key
`)
	if len(cfg.Warnings) != 1 {
		t.Fatalf("warnings = %v, want one about the bare level word", cfg.Warnings)
	}
	w := cfg.Warnings[0].String()
	for _, want := range []string{"capture", "(capture)"} {
		if !strings.Contains(w, want) {
			t.Errorf("warning %q does not mention %q", w, want)
		}
	}
	if cfg.Warnings[0].Line == 0 {
		t.Error("the warning names no line; it is meant to be followed to the offending one")
	}
}

// A hint nobody recognizes is reported too, and the binding is left visibly
// wrong rather than quietly landing on the right key — a typo that silently
// worked would be found only on the platform it was meant for.
func TestUnknownHintIsReportedAtLoad(t *testing.T) {
	cfg := loadHinted(t, KeymapEnvironment{OS: "darwin"}, `
[mappings:mew]
(mak) s-c = clipboard_copy
`)
	if len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0].Text, "mak") {
		t.Fatalf("warnings = %v, want one naming (mak)", cfg.Warnings)
	}
	if _, ok := cfg.Mappings["s-c"]; ok {
		t.Error("the typo bound s-c anyway; it must not look like it worked")
	}
}

// An ordinary config says nothing, so the startup log never opens on a clean
// start. A channel that chatters is one people learn to close unread.
func TestACleanConfigWarnsAboutNothing(t *testing.T) {
	cfg := loadHinted(t, KeymapEnvironment{OS: "darwin"}, `
[mappings:mew]
(mac) s-c   = clipboard_copy
(capture) ^C = tinput_key
^W          = window_close
`)
	if len(cfg.Warnings) != 0 {
		t.Errorf("warnings = %v, want none", cfg.Warnings)
	}
	if StartupLog(cfg.Warnings) != "" {
		t.Error("a clean config produced a startup log; it must not open at all")
	}
}

// The log reads like a compiler's: where, then what. Its first line is the
// title, as a plain buffer shows no grammar to decorate it with.
func TestStartupLogShape(t *testing.T) {
	text := StartupLog([]ConfigWarning{
		{Source: "/home/u/.mew/editor.conf", Line: 12, Text: "something went wrong"},
	})
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if lines[0] != StartupLogTitle {
		t.Errorf("first line = %q, want %q", lines[0], StartupLogTitle)
	}
	if last := lines[len(lines)-1]; last != "/home/u/.mew/editor.conf 12: something went wrong" {
		t.Errorf("entry = %q, want file, line, then what happened", last)
	}
}
