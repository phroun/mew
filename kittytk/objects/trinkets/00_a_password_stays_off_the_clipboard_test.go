package trinkets

// A field that keeps its content off the screen keeps it from everywhere else
// it could be read off: the clipboard, and a screen reader.

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

var concealing = []EchoMode{EchoPassword, EchoPasswordOnEdit, EchoNoEcho}

func TestAConcealedFieldCopiesNothing(t *testing.T) {
	for _, mode := range concealing {
		ti, clip := newClippedInput("hunter2")
		ti.SetEchoMode(mode)
		clip.SetClipboard("before")
		ti.SelectAll()

		ti.Copy()
		if got := clip.Clipboard(); got != "before" {
			t.Errorf("mode %v: Copy put %q on the clipboard", mode, got)
		}
		ti.Cut()
		if got := clip.Clipboard(); got != "before" {
			t.Errorf("mode %v: Cut put %q on the clipboard", mode, got)
		}
		// Nor does Cut take the text away: a cut that reaches no clipboard is
		// a delete nobody asked for.
		if got := ti.Text(); got != "hunter2" {
			t.Errorf("mode %v: Cut left the field holding %q", mode, got)
		}
	}
}

// The field that shows its content still copies and cuts it.
func TestAPlainFieldStillCopies(t *testing.T) {
	ti, clip := newClippedInput("hello")
	ti.SelectAll()
	ti.Copy()
	if got := clip.Clipboard(); got != "hello" {
		t.Errorf("Copy put %q on the clipboard, want hello", got)
	}
	ti.Cut()
	if ti.Text() != "" {
		t.Errorf("Cut left %q", ti.Text())
	}
}

// The right-click menu greys Cut and Copy on a concealed field, and offers
// them on a plain one.
func TestTheContextMenuWithholdsCutAndCopy(t *testing.T) {
	for _, mode := range append([]EchoMode{EchoNormal}, concealing...) {
		ti, _ := newClippedInput("hunter2")
		ti.SetEchoMode(mode)
		for _, it := range ti.contextMenuItems() {
			if it.label != "Cut" && it.label != "Copy" {
				continue
			}
			if want := mode != EchoNormal; it.disabled != want {
				t.Errorf("mode %v: %s disabled=%v, want %v", mode, it.label, it.disabled, want)
			}
		}
	}
}

// The Edit menu asks the same question of whatever has the focus.
func TestTheEditMenuKnowsAConcealedField(t *testing.T) {
	for _, mode := range append([]EchoMode{EchoNormal}, concealing...) {
		ti := NewTextInput()
		ti.SetEchoMode(mode)
		if got, want := concealed(ti), mode != EchoNormal; got != want {
			t.Errorf("mode %v: concealed = %v, want %v", mode, got, want)
		}
	}
}

// A screen reader is told what the screen shows: the mask, or nothing.
func TestAScreenReaderHearsTheMask(t *testing.T) {
	cases := []struct {
		mode EchoMode
		want string
	}{
		{EchoNormal, "hunter2"},
		{EchoPassword, "•••••••"},
		{EchoPasswordOnEdit, "•••••••"},
		{EchoNoEcho, ""},
	}
	for _, c := range cases {
		ti := NewTextInput()
		ti.SetText("hunter2")
		ti.SetEchoMode(c.mode)
		if got := ti.AccessibleInfo().Value; got != c.want {
			t.Errorf("mode %v: a screen reader is told %q, want %q", c.mode, got, c.want)
		}
	}

	// Nor the character an on-edit field is showing for a moment.
	_, ti := onEditField(t)
	ti.HandleKeyPress(core.KeyPressEvent{Key: "x"})
	if got := ti.AccessibleInfo().Value; got != "•" {
		t.Errorf("with x revealed a screen reader is told %q, want the mask", got)
	}
}
