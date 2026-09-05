package core

import "testing"

// A size nobody gave a value carries -1, and never zero -- because zero is a
// size a caller may mean. The two constants share that value and are named
// for what they say where they are used.
func TestAnAbsentSizeIsMinusOneAndNotZero(t *testing.T) {
	if Unbounded != -1 || BasisAuto != -1 {
		t.Errorf("Unbounded = %d and BasisAuto = %d, want -1", Unbounded, BasisAuto)
	}
}

// A fresh trinket has no maximum, and it says so with the sentinel rather
// than a large number standing in for one.
func TestAFreshTrinketHasNoMaximum(t *testing.T) {
	w := NewTrinketBase()
	if got := w.MaximumSize(); got.Width != Unbounded || got.Height != Unbounded {
		t.Errorf("a new trinket's maximum is %v, want both axes unbounded", got)
	}

	// And a maximum of zero is kept as written: it is a real answer, not an
	// absence, so nothing may quietly read it as "no limit".
	w.SetMaximumSize(UnitSize{})
	if got := w.MaximumSize(); got.Width != 0 || got.Height != 0 {
		t.Errorf("a maximum of zero came back as %v", got)
	}
}

// The flex hints a child gets when it says nothing take the basis from the
// child, so a zero-valued struct cannot be mistaken for a basis of zero.
func TestDefaultFlexHintsTakeTheBasisFromTheChild(t *testing.T) {
	got := DefaultFlexHints()
	if got.Basis != BasisAuto {
		t.Errorf("the default basis is %d, want BasisAuto", got.Basis)
	}
	if got.Shrink != 1 || got.ShrinkSet {
		t.Errorf("the default hints are %+v, want ordinary shrinking that nobody wrote", got)
	}
	if (FlexHints{}).Basis == BasisAuto {
		t.Error("the zero value already reads as BasisAuto, so the default buys nothing")
	}
}
