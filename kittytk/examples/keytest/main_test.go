package main

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The whole point of the viewer is that a row is a faithful reading of the
// event, so the rendering is worth pinning: a column that quietly shows the
// same thing for two different events would hide exactly what this exists to
// find.
func TestDescribeRendersTheDistinctionsThatMatter(t *testing.T) {
	press, key, mods, repeat, text, _, isMouse := describe(core.KeyPressEvent{
		Key: "A", Text: "A", Modifiers: core.ShiftModifier,
	})
	if press != "KeyPress" || key != `"A"` || mods != "Shift" || text != `"A"` {
		t.Errorf("shifted press rendered as %s/%s/%s/%s", press, key, mods, text)
	}
	if repeat != "-" {
		t.Errorf("a struck key rendered repeat %q, want %q", repeat, "-")
	}
	if isMouse {
		t.Error("a key press was classified as a mouse event and would be filtered away")
	}

	// A held key and a struck one must not render alike — that difference is
	// what the repeat column is for.
	_, _, _, heldRepeat, _, _, _ := describe(core.KeyPressEvent{Key: "A", Repeat: true})
	if heldRepeat == repeat {
		t.Errorf("held and struck both rendered repeat %q", heldRepeat)
	}

	// A release carries neither text nor a repeat flag, and saying so beats
	// leaving the cells blank, which reads as "not filled in".
	rel, relKey, relMods, relRepeat, relText, _, _ := describe(core.KeyReleaseEvent{
		Key: "Up", Modifiers: core.ControlModifier | core.ShiftModifier,
	})
	if rel != "KeyRelease" || relKey != `"Up"` || relMods != "Ctrl+Shift" {
		t.Errorf("release rendered as %s/%s/%s", rel, relKey, relMods)
	}
	if relRepeat != "-" || relText != "-" {
		t.Errorf("release rendered repeat %q text %q, want both %q", relRepeat, relText, "-")
	}
}

// An empty string and an absent one are different answers, and telling them
// apart is regularly the whole question — an unnamed key release looked
// identical to a key named "" until this distinction existed.
func TestEmptyFieldsAreVisible(t *testing.T) {
	_, key, _, _, text, _, _ := describe(core.KeyPressEvent{Key: "", Text: ""})
	if key != "-" || text != "-" {
		t.Errorf("empty key/text rendered as %q/%q, want %q", key, text, "-")
	}
	_, keyed, _, _, _, _, _ := describe(core.KeyPressEvent{Key: `"`})
	if keyed != `"\""` {
		t.Errorf("a quote character rendered as %s; it must stay distinguishable "+
			"from the quoting itself", keyed)
	}
}

// Mouse events are classified so the checkbox can hide them, and everything
// else must NOT be — a key swept away with the mouse traffic is the one thing
// this viewer cannot afford.
func TestOnlyMouseEventsAreFilterable(t *testing.T) {
	for _, ev := range []core.Event{
		core.MousePressEvent{Button: core.LeftButton},
		core.MouseReleaseEvent{Button: core.RightButton},
		core.MouseMoveEvent{},
		core.MouseWheelEvent{DeltaY: 1},
		core.MouseLeaveEvent{},
	} {
		if _, _, _, _, _, _, isMouse := describe(ev); !isMouse {
			t.Errorf("%T is not filterable; it would flood the log with the "+
				"checkbox off", ev)
		}
	}
	for _, ev := range []core.Event{
		core.KeyPressEvent{Key: "a"},
		core.KeyReleaseEvent{Key: "a"},
		core.PasteEvent{Text: "x"},
		core.ResizeEvent{Cols: 80, Rows: 24},
		core.FocusEvent{Focused: true},
		core.QuitEvent{},
	} {
		if _, _, _, _, _, _, isMouse := describe(ev); isMouse {
			t.Errorf("%T is filterable as a mouse event; it would vanish", ev)
		}
	}
}

// Mega and Micro are named apart. Both have a real claim on "Meta", so neither
// is given it, and a viewer that showed one word for two modifiers would be
// lying about the very thing someone came here to check.
func TestModifiersAreNamedApart(t *testing.T) {
	for _, tc := range []struct {
		mods core.KeyModifiers
		want string
	}{
		{0, "-"},
		{core.ShiftModifier, "Shift"},
		{core.MegaModifier, "Mega"},
		{core.MicroModifier, "Micro"},
		{core.MegaModifier | core.MicroModifier, "Mega+Micro"},
		{core.ControlModifier | core.ShiftModifier | core.MegaModifier, "Ctrl+Mega+Shift"},
		{core.GlyphModifier, "Glyph"},
		{core.SuperModifier | core.HyperModifier, "Super+Hyper"},
	} {
		if got := modString(tc.mods); got != tc.want {
			t.Errorf("modString(%d) = %q, want %q", int(tc.mods), got, tc.want)
		}
	}
}
