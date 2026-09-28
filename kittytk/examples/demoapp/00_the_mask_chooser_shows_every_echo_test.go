package main

import (
	"testing"

	"github.com/phroun/kittytk/objects/trinkets"
)

// The Text Fields tab's chooser runs the field through every masked echo and
// back, and the mask it picks survives a change of echo: the mask belongs to
// the field, not to the mode. Driven through the app's own wiring and the real
// radio buttons, so a handler pointed at the wrong object shows.
func TestTheMaskChooserShowsEveryEcho(t *testing.T) {
	ui, _, _ := openTabWithUI(t, "Text Fields")
	(&app{ui: ui}).wireMainWindow()

	field, _ := ui.Object("tfmask").Target().(*trinkets.TextInput)
	if field == nil {
		t.Fatal("nothing behind tfmask is a text field")
	}
	pick := func(key string) {
		t.Helper()
		rb, _ := ui.Object(key).Target().(*trinkets.RadioButton)
		if rb == nil {
			t.Fatalf("nothing behind %s is a radio button", key)
		}
		rb.SetChecked(true)
	}

	pick("tfmee")
	if got := field.EchoMode(); got != trinkets.EchoPasswordOnEdit {
		t.Errorf("password_on_edit left the field in mode %v", got)
	}
	// Choosing a mask leaves the echo where it was.
	pick("tfms")
	if got := field.EchoMode(); got != trinkets.EchoPasswordOnEdit {
		t.Errorf("choosing the star put the field in mode %v", got)
	}
	if got := field.MaskChar(); got != '*' {
		t.Errorf("the star mask became %q on changing the echo", got)
	}

	pick("tfmen")
	if got := field.EchoMode(); got != trinkets.EchoNormal {
		t.Errorf("normal left the field in mode %v", got)
	}

	pick("tfmh")
	if got := field.EchoMode(); got != trinkets.EchoNormal {
		t.Errorf("choosing the hash while unmasked put the field in mode %v", got)
	}
	pick("tfmep")
	if got := field.EchoMode(); got != trinkets.EchoPassword {
		t.Errorf("password left the field in mode %v", got)
	}
	if got := field.MaskChar(); got != '#' {
		t.Errorf("the hash mask chosen while unmasked came back as %q", got)
	}

	pick("tfmb")
	if got := field.MaskChar(); got != '•' {
		t.Errorf("the bullet mask came out as %q", got)
	}
}
