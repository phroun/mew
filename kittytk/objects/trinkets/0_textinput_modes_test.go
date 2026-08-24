package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// Every option a text field has, reachable from the wire.
//
// Only text and placeholder were registered. A password field could not be
// built over the protocol at all -- the Go setters existed, and the vocabulary
// doc listed the properties, but nothing connected the two.
func TestTextInputModesArriveOverTheWire(t *testing.T) {
	f, _ := buildWithEvents(t, nil, `
new textinput text="plain"
new textinput text="ro" readonly
new textinput placeholder="short" max_length=8
new textinput text="secret" echo=password
new textinput text="secret" echo=password mask="*"
new textinput text="hidden" echo=none
`)
	if len(f.targets) != 6 {
		t.Fatalf("built %d fields, want 6", len(f.targets))
	}
	fields := make([]*TextInput, 6)
	for i := range fields {
		fields[i] = f.targets[i].(*TextInput)
	}

	if fields[0].IsReadOnly() {
		t.Error("a plain field came out read-only")
	}
	if !fields[1].IsReadOnly() {
		t.Error("readonly did not take")
	}
	if got := fields[2].MaxLength(); got != 8 {
		t.Errorf("max_length = %d, want 8", got)
	}
	if got := fields[3].EchoMode(); got != EchoPassword {
		t.Errorf("echo=password gave mode %v", got)
	}
	if got := fields[3].MaskChar(); got != '•' {
		t.Errorf("default mask = %q, want a bullet", got)
	}
	if got := fields[4].MaskChar(); got != '*' {
		t.Errorf(`mask="*" gave %q`, got)
	}
	if got := fields[5].EchoMode(); got != EchoNoEcho {
		t.Errorf("echo=none gave mode %v", got)
	}
}

// A masked field masks what it PAINTS and not what it holds: the content is
// still the content, which is what the change and complete events carry.
func TestMaskingIsPaintOnly(t *testing.T) {
	ti := NewTextInput()
	ti.SetEchoMode(EchoPassword)
	ti.SetMaskChar('*')
	ti.SetText("hunter2")

	if got := ti.Text(); got != "hunter2" {
		t.Errorf("Text() = %q; masking must not touch the content", got)
	}
	if got := string(ti.echo([]rune(ti.Text()))); got != "*******" {
		t.Errorf("painted %q, want seven stars", got)
	}
	// echo=none paints nothing at all, and still holds the text.
	ti.SetEchoMode(EchoNoEcho)
	if got := string(ti.echo([]rune(ti.Text()))); got != "" {
		t.Errorf("echo=none painted %q", got)
	}
	if got := ti.Text(); got != "hunter2" {
		t.Errorf("Text() = %q under echo=none", got)
	}
}

// A read-only field declines every edit, from any direction.
func TestReadOnlyDeclinesEdits(t *testing.T) {
	ti := NewTextInput()
	ti.SetText("fixed")
	ti.SetCursorPosition(5)
	ti.SetReadOnly(true)

	for _, key := range []string{"a", "Space", "Backspace", "FDel"} {
		ti.HandleKeyPress(core.KeyPressEvent{Key: key})
	}
	if got := ti.Text(); got != "fixed" {
		t.Errorf("a read-only field became %q", got)
	}
}

// max_length stops accepting, rather than truncating what is already there.
func TestMaxLengthStopsAccepting(t *testing.T) {
	ti := NewTextInput()
	ti.SetMaxLength(4)
	for _, key := range []string{"a", "b", "c", "d", "e", "f"} {
		ti.HandleKeyPress(core.KeyPressEvent{Key: key})
	}
	if got := ti.Text(); got != "abcd" {
		t.Errorf("text = %q, want abcd — the field should stop at 4", got)
	}
}

// A wrong echo word is refused by name rather than silently ignored.
func TestUnknownEchoWordIsRefused(t *testing.T) {
	err := runScript(t, `new textinput echo=starlight`)
	if err == nil {
		t.Fatal("echo=starlight was accepted")
	}
	if got := err.Error(); got == "" {
		t.Error("empty error")
	} else {
		t.Logf("refused as: %v", err)
	}
}
