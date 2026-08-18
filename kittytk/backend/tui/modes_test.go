package tui

import (
	"testing"

	"github.com/phroun/direct-key-handler/keyboard"
	"github.com/phroun/kittytk/core"
)

// The toolkit and the key layer must name the states the same.
//
// This backend passes modes straight through without translating the tokens,
// and a consumer written against the toolkit asks the key layer's map by the
// toolkit's name. One rename on either side and every lookup would miss —
// quietly, since an unknown mode is reported as "cannot tell" rather than as an
// error.
func TestModeTokensMatchTheKeyLayer(t *testing.T) {
	for _, c := range []struct{ toolkit, key, what string }{
		{core.ModeNumLock, keyboard.ModeNumLock, "num"},
		{core.ModeCapsLock, keyboard.ModeCapsLock, "caps"},
		{core.ModeFocus, keyboard.ModeFocus, "focus"},
		{core.ModeOn, keyboard.ModeOn, "on"},
		{core.ModeOff, keyboard.ModeOff, "off"},
	} {
		if c.toolkit != c.key {
			t.Errorf("%s: toolkit says %q, key layer says %q", c.what, c.toolkit, c.key)
		}
	}
}

// A backend whose keyboard has not started answers "cannot tell" rather than
// inventing an off.
func TestModesBeforeTheKeyboardStarts(t *testing.T) {
	b := &TUIBackend{}
	if got := b.Modes(); len(got) != 0 {
		t.Errorf("modes = %v before the keyboard exists, want none", got)
	}
	if v, ok := b.Mode(core.ModeNumLock); ok {
		t.Errorf("num = %q ok=true before the keyboard exists", v)
	}
	if b.SetMode("overtype", core.ModeOn) {
		t.Error("a write was accepted with nowhere to keep it")
	}
}

// The backend satisfies the capability a consumer type-asserts for.
var _ core.ModeSource = (*TUIBackend)(nil)
