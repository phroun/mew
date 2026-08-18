//go:build sdl

package sdl

import (
	"reflect"
	"testing"

	"github.com/phroun/kittytk/core"
	sdl3 "github.com/phroun/kittytk/sdl/sdl3"
)

// optionEventLog collects the events a surface was given.
type optionEventLog struct{ events []core.Event }

func (h *optionEventLog) Event(ev core.Event) bool {
	h.events = append(h.events, ev)
	return false
}
func (h *optionEventLog) Frame(*core.Painter)   {}
func (h *optionEventLog) Resized(core.UnitSize) {}

func (h *optionEventLog) keys() []core.KeyPressEvent {
	var out []core.KeyPressEvent
	for _, ev := range h.events {
		if k, ok := ev.(core.KeyPressEvent); ok {
			out = append(out, k)
		}
	}
	return out
}

// This host answers the capability an application type-asserts for.
var _ core.OptionCharSource = (*Platform)(nil)

// pendingPlatform makes a platform holding one Option chord, as the key-down
// path does before the character arrives.
func pendingPlatform(chord string) (*Platform, *optionEventLog) {
	h := &optionEventLog{}
	s := &sdlSurface{handler: h}
	p := &Platform{}
	p.pendingOption = &pendingOptionKey{key: chord, scancode: 4, surface: s}
	return p, h
}

// The chord is dispatched WITH the character it composed, and the pairing is
// remembered.
//
// The chord's name comes from its own key-down — the physical key and the
// modifier held — so the character is evidence about the keyboard rather than
// the thing being decoded to find out which key was pressed.
func TestOptionChordCarriesWhatItComposed(t *testing.T) {
	p, h := pendingPlatform("M-a")
	pending := p.takePendingOption()
	p.dispatchOption(pending, "å")

	keys := h.keys()
	if len(keys) != 1 {
		t.Fatalf("dispatched %d key events, want 1: %v", len(keys), h.events)
	}
	if keys[0].Key != "M-a" || keys[0].Text != "å" {
		t.Errorf("dispatched %+v, want Key M-a with Text å", keys[0])
	}
	if ch, ok := p.OptionChar("M-a"); !ok || ch != "å" {
		t.Errorf("observed %q ok=%v for M-a, want å", ch, ok)
	}
	// And the press is registered under the key-down's scancode, so its
	// release names it the same.
	if name, ok := p.takeHeldKey(4); !ok || name != "M-a" {
		t.Errorf("held key = %q ok=%v, want M-a", name, ok)
	}
}

// A chord that composes nothing is still the keystroke that happened.
func TestOptionChordWithNoComposition(t *testing.T) {
	p, h := pendingPlatform("M-a")
	p.flushPendingOption()

	keys := h.keys()
	if len(keys) != 1 || keys[0].Key != "M-a" {
		t.Fatalf("flushed %v, want one M-a", h.events)
	}
	if keys[0].Text != "a" {
		t.Errorf("Text = %q, want the key's own character", keys[0].Text)
	}
	if _, ok := p.OptionChar("M-a"); ok {
		t.Error("nothing was composed, so nothing should have been observed")
	}
	if p.pendingOption != nil {
		t.Error("the chord is still held after being flushed")
	}
}

// A keyboard that starts composing something else is believed: the memo is
// what this keyboard does NOW, not a record of what it once did.
func TestObservationOverwrites(t *testing.T) {
	p, _ := pendingPlatform("M-a")
	p.dispatchOption(p.takePendingOption(), "å")

	p.pendingOption = &pendingOptionKey{key: "M-a", scancode: 4}
	p.dispatchOption(p.takePendingOption(), "ä")

	if ch, _ := p.OptionChar("M-a"); ch != "ä" {
		t.Errorf("M-a observed as %q, want the character it composes now", ch)
	}
}

// The whole table is available, for a host that wants to show or record what
// this keyboard does.
func TestOptionCharsSnapshot(t *testing.T) {
	p := &Platform{}
	p.noteOptionChar("M-a", "å")
	p.noteOptionChar("M-e", "´") // a dead key's composition counts too

	want := map[string]string{"M-a": "å", "M-e": "´"}
	if got := p.OptionChars(); !reflect.DeepEqual(got, want) {
		t.Errorf("OptionChars = %v, want %v", got, want)
	}

	// A copy, so a caller cannot edit what the platform believes.
	p.OptionChars()["M-a"] = "nonsense"
	if ch, _ := p.OptionChar("M-a"); ch != "å" {
		t.Errorf("the memo was changed through the snapshot: M-a = %q", ch)
	}
}

// Only a bare Option on a printable key composes; nothing else should be held
// waiting for a character that is not coming.
//
// The platform test is separate (macOptionMayCompose adds it), so these
// conditions can be checked wherever the tests happen to run.
func TestWhichKeysWaitForACharacter(t *testing.T) {
	for _, c := range []struct {
		sym  sdl3.Keysym
		want bool
		what string
	}{
		{sdl3.Keysym{Sym: 'a', Mod: sdl3.KMOD_LALT}, true, "Option+a"},
		{sdl3.Keysym{Sym: '5', Mod: sdl3.KMOD_RALT}, true, "Option+5"},
		{sdl3.Keysym{Sym: 'a', Mod: sdl3.KMOD_LALT | sdl3.KMOD_LCTRL}, false,
			"Ctrl is held, and macOS composes nothing for it"},
		{sdl3.Keysym{Sym: 'a', Mod: sdl3.KMOD_LALT | sdl3.KMOD_LGUI}, false,
			"Command is held, and composes nothing"},
		{sdl3.Keysym{Sym: 'a'}, false, "no Option at all"},
		{sdl3.Keysym{Sym: sdl3.K_F1, Mod: sdl3.KMOD_LALT}, false,
			"a named key, which composes no character"},
	} {
		if got := optionComposes(c.sym); got != c.want {
			t.Errorf("%s: waits = %v, want %v", c.what, got, c.want)
		}
	}
}
