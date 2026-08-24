package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A disabled trinket does not take focus, however it is asked.
//
// Tab already skipped one: the focus manager refuses a disabled trinket. But
// SetFocus ran ahead of the manager and unconditionally -- it marked the
// trinket focused and announced HandleFocusIn, and only then did the manager
// decline to record it. So a disabled field CLICKED came up looking focused,
// caret and all, while Tab went on stepping over it. The two halves disagreed
// about the same trinket.
func TestDisabledTrinketRefusesFocus(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("disabled")
	ti.SetEnabled(false)

	ti.SetFocus()
	if ti.HasFocus() {
		t.Error("a disabled field took focus from SetFocus")
	}

	// ...and from a click, which is the way it was actually reachable.
	ti.HandleMousePress(core.MousePressEvent{Button: core.LeftButton})
	if ti.HasFocus() {
		t.Error("a disabled field took focus from a mouse click")
	}

	// Enabling it again restores the ordinary behaviour, so this is a refusal
	// and not a latch.
	ti.SetEnabled(true)
	ti.SetFocus()
	if !ti.HasFocus() {
		t.Error("an enabled field would not take focus")
	}
}

// A hidden one likewise: the manager's rule is enabled AND visible, and both
// halves now say the same thing.
func TestHiddenTrinketRefusesFocus(t *testing.T) {
	b := NewButton("hidden")
	b.SetVisible(false)
	b.SetFocus()
	if b.HasFocus() {
		t.Error("a hidden button took focus")
	}
}

// The manager and the trinket agree, which is the property that was broken:
// whatever the manager would refuse, the trinket refuses too.
func TestFocusManagerAndTrinketAgree(t *testing.T) {
	fm := core.NewFocusManager(NewPanel())
	for _, c := range []struct {
		name    string
		prepare func(*TextInput)
	}{
		{"disabled", func(ti *TextInput) { ti.SetEnabled(false) }},
		{"hidden", func(ti *TextInput) { ti.SetVisible(false) }},
	} {
		ti := NewTextInput()
		c.prepare(ti)

		viaManager := fm.SetFocusedTrinket(ti)
		ti.SetFocus()
		viaTrinket := ti.HasFocus()

		if viaManager || viaTrinket {
			t.Errorf("%s: manager accepted=%v, trinket took it=%v; both should refuse",
				c.name, viaManager, viaTrinket)
		}
	}
}
