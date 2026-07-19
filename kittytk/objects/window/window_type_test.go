package window

import "testing"

func TestWindowTypeFromString(t *testing.T) {
	cases := map[string]WindowType{
		"main":        WindowTypeMain,
		"normal":      WindowTypeNormal,
		"mdichild":    WindowTypeMDIChild,
		"dialog":      WindowTypeDialog,
		"modal":       WindowTypeModal,
		"toolpalette": WindowTypeToolPalette,
	}
	for s, want := range cases {
		got, ok := WindowTypeFromString(s)
		if !ok || got != want {
			t.Errorf("WindowTypeFromString(%q) = %v,%v; want %v,true", s, got, ok, want)
		}
		if got.String() != s {
			t.Errorf("%v.String() = %q, want %q", want, got.String(), s)
		}
	}
	if _, ok := WindowTypeFromString("bogus"); ok {
		t.Error("unknown type should not parse")
	}
}

func TestIsOwnedOverlay(t *testing.T) {
	overlay := []WindowType{WindowTypeDialog, WindowTypeModal, WindowTypeToolPalette}
	for _, ty := range overlay {
		if !ty.IsOwnedOverlay() {
			t.Errorf("%v should be an owned overlay", ty)
		}
	}
	for _, ty := range []WindowType{WindowTypeMain, WindowTypeNormal, WindowTypeMDIChild} {
		if ty.IsOwnedOverlay() {
			t.Errorf("%v should not be an owned overlay", ty)
		}
	}
}

// SetOwner resolves up the ownership chain: an owner that is itself a
// dialog/modal/toolpalette is skipped until a non-overlay window is reached,
// and that non-overlay window is stored.
func TestSetOwnerResolvesChain(t *testing.T) {
	base := NewWindow("base") // normal window - the real owner
	dlg := NewWindow("dlg")
	dlg.SetType(WindowTypeDialog)
	dlg.SetOwner(base)

	tool := NewWindow("tool")
	tool.SetType(WindowTypeToolPalette)
	// Owner is the dialog, which is itself an overlay: must resolve to base.
	tool.SetOwner(dlg)
	if tool.Owner() != base {
		t.Errorf("tool owner = %v, want base (resolved through the dialog)", tool.Owner())
	}

	// A modal owned by the tool palette also resolves to base.
	modal := NewWindow("modal")
	modal.SetType(WindowTypeModal)
	modal.SetOwner(tool)
	if modal.Owner() != base {
		t.Errorf("modal owner = %v, want base (resolved through tool->dialog)", modal.Owner())
	}

	// A window owning itself resolves to no owner.
	base.SetOwner(base)
	if base.Owner() != nil {
		t.Error("self-owner should resolve to nil")
	}
}
