package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// shortcutFont swaps the family to Apple's UI face only in native mode, keeps a
// copy (never mutating the base), and preserves size/style/color.
func TestShortcutFont(t *testing.T) {
	prev := core.MacNativeShortcuts()
	defer core.SetMacNativeShortcuts(prev)

	base := &core.Font{Name: "ui-text", Size: 12, Style: core.FontStyleBold}

	core.SetMacNativeShortcuts(false)
	if got := shortcutFont(base); got != base {
		t.Errorf("native off: shortcutFont should return the base font unchanged, got %+v", got)
	}

	core.SetMacNativeShortcuts(true)
	got := shortcutFont(base)
	if got == base {
		t.Fatal("native on: shortcutFont must return a copy, not the shared base")
	}
	if got.Name != core.MacShortcutFontFamily {
		t.Errorf("native font family = %q, want %q", got.Name, core.MacShortcutFontFamily)
	}
	if want := base.Size * 4 / 5; got.Size != want {
		t.Errorf("native font size = %d, want %d (80%% of %d)", got.Size, want, base.Size)
	}
	if got.Size >= base.Size {
		t.Errorf("native font size %d should be smaller than base %d", got.Size, base.Size)
	}
	if got.Style != base.Style {
		t.Errorf("native font lost style: got %v, want %v", got.Style, base.Style)
	}
	if base.Name != "ui-text" {
		t.Errorf("base font was mutated: Name = %q", base.Name)
	}

	// A nil base stays nil regardless of mode.
	if shortcutFont(nil) != nil {
		t.Error("shortcutFont(nil) should be nil")
	}
}
