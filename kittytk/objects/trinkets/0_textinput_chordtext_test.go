package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// watchingHost is a container above a trinket that has watched the keyboard.
// Only the capability under test is real; the rest is what core.Container asks
// for.
type watchingHost struct {
	core.TrinketBase
	observed map[string]string
	children []core.Trinket
}

func (h *watchingHost) KeyChordText(chord string) (string, bool) {
	text, ok := h.observed[chord]
	return text, ok
}

func (h *watchingHost) AllKeyChordText() map[string]string { return h.observed }

func (h *watchingHost) Children() []core.Trinket            { return h.children }
func (h *watchingHost) AddChild(c core.Trinket)             { h.children = append(h.children, c) }
func (h *watchingHost) RemoveChild(core.Trinket)            {}
func (h *watchingHost) ChildAt(core.UnitPoint) core.Trinket { return nil }
func (h *watchingHost) Layout()                             {}
func (h *watchingHost) LayoutManager() core.LayoutManager   { return nil }
func (h *watchingHost) SetLayoutManager(core.LayoutManager) {}

// A chord whose NAME cannot carry what it types is typed from the memo.
//
// macOS Option is the case: the chord is M-b and the character is "∫". The name
// identifies the keystroke and the host, which watched the keyboard produce the
// character, is asked what it typed — the same question mew's own floor asks,
// of the same host.
func TestTextInputTypesFromTheMemo(t *testing.T) {
	host := &watchingHost{observed: map[string]string{"M-b": "∫"}}
	host.Init(host)

	ti := NewTextInput()
	ti.SetParent(host)
	ti.SetFocus()

	ti.HandleKeyPress(core.KeyPressEvent{Key: "M-b"})
	if got := ti.Text(); got != "∫" {
		t.Errorf("M-b typed %q, want the character the host watched it produce", got)
	}
}

// A one-character KeyName IS the character, and needs no host to confirm it.
//
// This is what types where nothing has watched the keyboard at all — the
// terminal backend, where a keystroke arrives already named and there is no
// second event to observe.
func TestTextInputTypesANamedCharacterWithNoHost(t *testing.T) {
	ti := NewTextInput()
	ti.SetFocus()

	ti.HandleKeyPress(core.KeyPressEvent{Key: "x"})
	ti.HandleKeyPress(core.KeyPressEvent{Key: "Y"})
	if got := ti.Text(); got != "xY" {
		t.Errorf("typed %q, want %q", got, "xY")
	}
}

// A chord the host has never seen, whose name is not a character, types
// nothing — the same as a key that produces no text.
func TestTextInputTypesNothingForAnUnobservedChord(t *testing.T) {
	ti := NewTextInput()
	ti.SetFocus()

	if ti.HandleKeyPress(core.KeyPressEvent{Key: "M-q"}) {
		t.Error("an unobserved chord was claimed as typing")
	}
	if got := ti.Text(); got != "" {
		t.Errorf("typed %q, want nothing", got)
	}
}
