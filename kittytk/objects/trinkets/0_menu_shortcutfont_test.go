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
	want := int(float64(base.Size) * core.ShortcutScale())

	// Graphical, native off: the menu's own size, the menu's own face.
	core.SetMacNativeShortcuts(false)
	got := shortcutFont(base, true)
	if got == base {
		t.Fatal("graphical: shortcutFont must return a copy, not the shared base")
	}
	if got.Size != want {
		t.Errorf("graphical, native off: size = %d, want %d (the shortcut scale of %d)", got.Size, want, base.Size)
	}
	if got.Name != base.Name {
		t.Errorf("graphical, native off: family = %q, want the menu's own %q", got.Name, base.Name)
	}
	if got.Style != base.Style {
		t.Errorf("graphical: lost the base's style: got %v, want %v", got.Style, base.Style)
	}

	// Graphical, native on: Apple's face, taken down AGAIN -- the two scales
	// compound, so this is smaller still than the menu's own reduction.
	core.SetMacNativeShortcuts(true)
	got = shortcutFont(base, true)
	wantNative := int(float64(base.Size) * core.ShortcutScale() * core.ShortcutNativeScale())
	if got.Size != wantNative {
		t.Errorf("graphical, native on: size = %d, want %d", got.Size, wantNative)
	}
	if got.Size >= want {
		t.Errorf("native size %d is not smaller than the menu's own %d; the scales are not compounding",
			got.Size, want)
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

// The two scales COMPOUND: the menu's own reduction, and Apple's face taken
// down again on top of it, since that face renders visually larger than the
// menu's at the same point size. At the defaults that is 0.8, or 0.64 in
// native mode -- a 12pt body giving a 9pt shortcut, or a 7pt one.
func TestShortcutScalesCompound(t *testing.T) {
	prev, prevNative := core.MacNativeShortcuts(), core.ShortcutNativeScale()
	prevScale := core.ShortcutScale()
	t.Cleanup(func() {
		core.SetMacNativeShortcuts(prev)
		core.SetShortcutScale(prevScale)
		core.SetShortcutNativeScale(prevNative)
	})

	base := &core.Font{Name: "ui-text", Size: 12}
	for _, c := range []struct {
		scale, native      float64
		wantOff, wantOnMac int
	}{
		{0.8, 0.8, 9, 7}, // the defaults: 9.6 and 7.68, floored
		{1, 0.8, 12, 9},  // no reduction of its own; Apple's face still down
		{0.8, 1, 9, 9},   // the menu's reduction, Apple's face left alone
		{0.5, 0.5, 6, 3}, // a half, then a half again
	} {
		core.SetShortcutScale(c.scale)
		core.SetShortcutNativeScale(c.native)

		core.SetMacNativeShortcuts(false)
		if got := shortcutFont(base, true).Size; got != c.wantOff {
			t.Errorf("scale %v: size = %d, want %d", c.scale, got, c.wantOff)
		}
		core.SetMacNativeShortcuts(true)
		if got := shortcutFont(base, true).Size; got != c.wantOnMac {
			t.Errorf("scale %v x native %v: size = %d, want %d (they compound)",
				c.scale, c.native, got, c.wantOnMac)
		}
	}

	// A cell surface draws one size, its cell's, whatever either scale says.
	core.SetShortcutScale(0.5)
	core.SetMacNativeShortcuts(true)
	if got := shortcutFont(base, false).Size; got != base.Size {
		t.Errorf("cell surface: size = %d, want the base's %d", got, base.Size)
	}
}
