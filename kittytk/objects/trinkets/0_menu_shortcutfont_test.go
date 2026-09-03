package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// shortcutFont makes two adjustments that answer to different things.
//
// The SIZE is the menu's own idea of a shortcut column -- 80% of the body, so
// it sits quietly beside the item text -- and applies wherever a surface can
// draw two sizes, which is the graphical path. The FAMILY is Apple's UI face
// and belongs to macOS-native mode alone, being there so the ⌃⌥⇧⌘ glyphs
// render in Apple's own typeface.
//
// Either way it returns a copy, never mutating the shared base, and carries
// the base's style through.
func TestShortcutFont(t *testing.T) {
	prev := core.MacNativeShortcuts()
	defer core.SetMacNativeShortcuts(prev)

	base := &core.Font{Name: "ui-text", Size: 12, Style: core.FontStyleBold}
	want := base.Size * 4 / 5

	// Graphical, native off: the menu's own size, the menu's own face.
	core.SetMacNativeShortcuts(false)
	got := shortcutFont(base, true)
	if got == base {
		t.Fatal("graphical: shortcutFont must return a copy, not the shared base")
	}
	if got.Size != want {
		t.Errorf("graphical, native off: size = %d, want %d (80%% of %d)", got.Size, want, base.Size)
	}
	if got.Name != base.Name {
		t.Errorf("graphical, native off: family = %q, want the menu's own %q", got.Name, base.Name)
	}
	if got.Style != base.Style {
		t.Errorf("graphical: lost the base's style: got %v, want %v", got.Style, base.Style)
	}

	// Graphical, native on: the same size, Apple's face.
	core.SetMacNativeShortcuts(true)
	got = shortcutFont(base, true)
	if got.Size != want {
		t.Errorf("graphical, native on: size = %d, want %d", got.Size, want)
	}
	if got.Name != core.MacShortcutFontFamily {
		t.Errorf("graphical, native on: family = %q, want %q", got.Name, core.MacShortcutFontFamily)
	}

	// A cell surface draws one size, its cell's, so the size is left alone --
	// but native mode's face still applies, being what that option is for.
	got = shortcutFont(base, false)
	if got.Size != base.Size {
		t.Errorf("cell, native on: size = %d, want the base's %d (a terminal draws one size)",
			got.Size, base.Size)
	}
	if got.Name != core.MacShortcutFontFamily {
		t.Errorf("cell, native on: family = %q, want %q", got.Name, core.MacShortcutFontFamily)
	}

	// Cell and native off: nothing to adjust at all.
	core.SetMacNativeShortcuts(false)
	if got := shortcutFont(base, false); got != base {
		t.Errorf("cell, native off: want the base font unchanged, got %+v", got)
	}

	if base.Name != "ui-text" || base.Size != 12 {
		t.Errorf("the base font was mutated: %+v", base)
	}

	// A nil base stays nil in every mode.
	for _, graphical := range []bool{false, true} {
		for _, native := range []bool{false, true} {
			core.SetMacNativeShortcuts(native)
			if shortcutFont(nil, graphical) != nil {
				t.Errorf("graphical=%v native=%v: a nil base should stay nil", graphical, native)
			}
		}
	}
}
