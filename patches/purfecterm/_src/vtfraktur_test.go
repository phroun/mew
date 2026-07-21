package purfecterm

import "testing"

// The fraktur slot (SGR 20 / slot 10) resolves to the reserved VTFRAKTUR family
// name by default, so a renderer recognizes the VT100 fraktur request; an
// explicit mapping still wins, and clearing restores the default.
func TestVTFrakturSlotDefault(t *testing.T) {
	b := NewBuffer(20, 2, 100)
	if got := b.GetFontSlot(VTFrakturSlot); got != VTFrakturFont {
		t.Fatalf("unset fraktur slot = %q, want %q", got, VTFrakturFont)
	}

	// SGR 20 paints into the fraktur slot.
	p := NewParser(b)
	p.Parse([]byte("\x1b[20mZ"))
	if got := b.GetVisibleCell(0, 0).Font; got != VTFrakturSlot {
		t.Fatalf("SGR 20 should select the fraktur slot, got %d", got)
	}

	b.SetFontSlot(VTFrakturSlot, "MyFraktur") // explicit override wins
	if got := b.GetFontSlot(VTFrakturSlot); got != "MyFraktur" {
		t.Fatalf("explicit fraktur slot = %q", got)
	}
	b.SetFontSlot(VTFrakturSlot, "") // clear -> back to the VTFRAKTUR default
	if got := b.GetFontSlot(VTFrakturSlot); got != VTFrakturFont {
		t.Fatalf("cleared fraktur slot = %q, want %q", got, VTFrakturFont)
	}
}
