package text

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// ui-term and ui-fraktur exist as aliases, defaulting to the monospace face
// (same as Monday); an unregistered target falls through to the last
// (guaranteed) family in the list.
func TestFontAliasDefaultsAndFallback(t *testing.T) {
	e := NewEngine()

	monday := e.Measure(&core.Font{Name: "Monday", Size: 12}, "iWi")
	uiterm := e.Measure(&core.Font{Name: "ui-term", Size: 12}, "iWi")
	if uiterm != monday {
		t.Fatalf("ui-term should default to the mono face: ui-term=%v monday=%v", uiterm, monday)
	}

	// A fallback list whose preferred family is unregistered resolves to the
	// registered fallback (Monday), not the engine default.
	e.SetFontAlias("ui-term", "NoSuchFamily", "Monday")
	if got := e.Measure(&core.Font{Name: "ui-term", Size: 12}, "iWi"); got != monday {
		t.Fatalf("ui-term should fall back to Monday when the preferred face is missing: %v vs %v", got, monday)
	}

	// Re-pointing bumps the epoch so caches keyed on the font set flush.
	before := e.Epoch()
	e.SetFontAlias("ui-term", "Noto Sans")
	if e.Epoch() == before {
		t.Fatal("SetFontAlias should bump the engine epoch")
	}
	// Now ui-term is proportional: a mixed-width string measures differently.
	if prop := e.Measure(&core.Font{Name: "ui-term", Size: 12}, "iWi"); prop == monday {
		t.Fatalf("re-pointed ui-term should measure as the proportional face, got mono width %v", prop)
	}
}

// RegisterFontFile registers a face addressable by its given family name, and
// UseFont points an alias at it.
func TestUseFontRegistersAndAliases(t *testing.T) {
	e := NewEngine()
	// Re-point via UseFont to a built-in family (no disk needed): the alias
	// resolves to it, and an unknown name in the list is simply skipped.
	if !e.UseFont("ui-term", "Go Mono") {
		t.Fatal("UseFont should report the preferred family available (Go Mono is embedded)")
	}
	goMono := e.Measure(&core.Font{Name: "Go Mono", Size: 12}, "abc")
	if got := e.Measure(&core.Font{Name: "ui-term", Size: 12}, "abc"); got != goMono {
		t.Fatalf("ui-term should now resolve to Go Mono: %v vs %v", got, goMono)
	}
}
