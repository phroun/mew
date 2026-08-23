package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
)

// ShortcutDisplay is the single source for the shortcut column: the bound
// shortcut, the literal text, or both joined by a space.
func TestMenuItemShortcutDisplay(t *testing.T) {
	cases := []struct {
		name     string
		shortcut core.Shortcut
		text     string
		want     string
	}{
		{"neither", "", "", ""},
		{"shortcut only", core.NewShortcut("^N"), "", core.NewShortcut("^N").DisplayString()},
		{"text only", "", "^B H", "^B H"},
		{"both concatenated", core.NewShortcut("^N"), "^B H",
			core.NewShortcut("^N").DisplayString() + " ^B H"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			it := &MenuItem{Shortcut: tc.shortcut, ShortcutText: tc.text}
			if got := it.ShortcutDisplay(); got != tc.want {
				t.Fatalf("ShortcutDisplay = %q, want %q", got, tc.want)
			}
		})
	}
}

// The width calculation accounts for the WHOLE string, so a long custom text
// widens the menu just as a long shortcut would — nothing can be drawn into
// space the menu did not reserve.
func TestMenuWidthCountsShortcutText(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	plain := NewMenu("m")
	plain.AddItem(NewMenuItem("Item"))
	base := plain.calculateSize().Width

	withText := NewMenu("m")
	item := NewMenuItem("Item")
	item.SetShortcutText("^B H is a long binding")
	withText.AddItem(item)
	wide := withText.calculateSize().Width

	if wide <= base {
		t.Fatalf("width with custom shortcut text = %v, want more than the bare %v", wide, base)
	}

	// And a bound shortcut PLUS text is wider still than the text alone.
	both := NewMenu("m")
	bi := NewMenuItem("Item")
	bi.SetShortcut(core.NewShortcut("^N"))
	bi.SetShortcutText("^B H is a long binding")
	both.AddItem(bi)
	if w := both.calculateSize().Width; w <= wide {
		t.Fatalf("width with shortcut+text = %v, want more than text alone %v", w, wide)
	}
}

// A bound shortcut and custom text both survive into the display string that
// every consumer — width, paint, accessibility — reads.
func TestMenuShortcutTextSetter(t *testing.T) {
	item := NewMenuItem("Using mew")
	if got := item.SetShortcutText("^B H"); got != item {
		t.Fatal("SetShortcutText should chain")
	}
	if item.ShortcutText != "^B H" {
		t.Fatalf("ShortcutText = %q", item.ShortcutText)
	}
	if !strings.Contains(item.ShortcutDisplay(), "^B H") {
		t.Fatalf("ShortcutDisplay = %q, want the custom text", item.ShortcutDisplay())
	}
}
