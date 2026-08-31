package trinkets

import (
	"testing"
)

// A disabled field cannot be changed, down any path.
//
// It could. Cut and Paste guarded on readOnly and not on enabled, so a
// disabled field was editable through its own context menu: right-click, Cut,
// and the value was gone. The keyboard paths had checked both all along, which
// is what made it look like disabled worked.
func TestDisabledFieldCannotBeEdited(t *testing.T) {
	for _, c := range []struct {
		name    string
		prepare func(*TextInput)
	}{
		{"disabled", func(ti *TextInput) { ti.SetEnabled(false) }},
		{"read-only", func(ti *TextInput) { ti.SetReadOnly(true) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			ti := NewTextInput()
			ti.SetText("keep me")
			ti.SelectAll()
			c.prepare(ti)

			ti.Cut()
			if got := ti.Text(); got != "keep me" {
				t.Errorf("Cut emptied it: %q", got)
			}
			ti.pasteText("clobbered")
			if got := ti.Text(); got != "keep me" {
				t.Errorf("paste changed it: %q", got)
			}
			ti.insert("typed")
			if got := ti.Text(); got != "keep me" {
				t.Errorf("insert changed it: %q", got)
			}
		})
	}
}

// ...and its menu says so, rather than offering the two and ignoring them.
func TestDisabledFieldsMenuRefusesTheEdits(t *testing.T) {
	byLabel := func(ti *TextInput) map[string]termMenuItem {
		out := map[string]termMenuItem{}
		for _, it := range ti.contextMenuItems() {
			if !it.separator {
				out[it.label] = it
			}
		}
		return out
	}

	live := byLabel(NewTextInput())
	for _, label := range []string{"Cut", "Copy", "Paste", "Select All"} {
		if live[label].disabled {
			t.Errorf("an ordinary field greys %q", label)
		}
	}

	off := NewTextInput()
	off.SetEnabled(false)
	items := byLabel(off)
	for _, label := range []string{"Cut", "Paste"} {
		if !items[label].disabled {
			t.Errorf("a disabled field still offers %q", label)
		}
	}
	// Copy and Select All only read, and the text is selectable with the
	// mouse either way, so they stay live.
	for _, label := range []string{"Copy", "Select All"} {
		if items[label].disabled {
			t.Errorf("a disabled field greys %q, which only reads", label)
		}
	}

	// Read-only is the same shape: it was showing Cut and Paste too, and they
	// quietly did nothing.
	ro := NewTextInput()
	ro.SetReadOnly(true)
	roItems := byLabel(ro)
	if !roItems["Cut"].disabled || !roItems["Paste"].disabled {
		t.Error("a read-only field still offers Cut/Paste")
	}
}

// Copy still works on a disabled field, because selecting its text with the
// mouse works and copying what you selected is the same act.
func TestCopyStillWorksWhenDisabled(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("readable")
	ti.SelectAll()
	ti.SetEnabled(false)
	if got := ti.SelectedText(); got != "readable" {
		t.Errorf("selection = %q on a disabled field", got)
	}
}
