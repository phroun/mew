package purfecterm

import "testing"

// SGR 10-20 select the per-cell font slot; reset returns to slot 0.
func TestFontSlotSGR(t *testing.T) {
	b := NewBuffer(20, 2, 100)
	p := NewParser(b)
	p.Parse([]byte("A\x1b[11mB\x1b[20mC\x1b[10mD\x1b[0mE"))

	want := []uint8{0, 1, 10, 0, 0} // A,B,C,D,E
	for i, wf := range want {
		if got := b.GetVisibleCell(i, 0).Font; got != wf {
			t.Errorf("cell %d (%c): Font=%d, want %d", i, b.GetVisibleCell(i, 0).Char, got, wf)
		}
	}
}

// The per-terminal slot map: unset slots inherit slot 0; clearing restores that.
func TestFontSlotMap(t *testing.T) {
	b := NewBuffer(20, 2, 100)
	b.SetFontSlot(0, "JetBrainsMono")
	b.SetFontSlot(3, "ui-fraktur")

	if got := b.GetFontSlot(3); got != "ui-fraktur" {
		t.Fatalf("slot 3 = %q, want ui-fraktur", got)
	}
	if got := b.GetFontSlot(5); got != "JetBrainsMono" { // unset -> inherit slot 0
		t.Fatalf("unset slot 5 should inherit slot 0, got %q", got)
	}
	b.SetFontSlot(3, "") // clear -> inherit slot 0
	if got := b.GetFontSlot(3); got != "JetBrainsMono" {
		t.Fatalf("cleared slot 3 should inherit slot 0, got %q", got)
	}
}

// OSC 7004 configures slots at runtime; SGR then selects them.
func TestFontSlotOSC(t *testing.T) {
	b := NewBuffer(20, 2, 100)
	p := NewParser(b)
	p.Parse([]byte("\x1b]7004;f;2;Comic Mono\x07"))
	if got := b.GetFontSlot(2); got != "Comic Mono" {
		t.Fatalf("OSC 7004 should set slot 2, got %q", got)
	}
	// SGR 12 then selects slot 2 for subsequently painted cells.
	p.Parse([]byte("\x1b[12mZ"))
	if got := b.GetVisibleCell(0, 0).Font; got != 2 {
		t.Fatalf("after OSC 7004 + SGR 12, painted cell Font=%d, want 2", got)
	}
}
