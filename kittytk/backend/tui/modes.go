package tui

import "github.com/phroun/kittytk/core"

// The keyboard states this backend can see, which are exactly the ones the key
// layer can see: it reads the terminal, and the terminal is all there is out
// here. So this passes straight through, translating one package's Mode into
// the other's — the two vocabularies use the same tokens deliberately, so a
// consumer can be written once against either host.
//
// The graphical host answers the same three from the window system instead,
// where it can ask rather than wait to be told. See sdl/modes.go.

// Modes implements core.ModeSource.
func (t *TUIBackend) Modes() []core.Mode {
	if t.keyboard == nil {
		return nil
	}
	from := t.keyboard.Modes()
	modes := make([]core.Mode, len(from))
	for i, m := range from {
		modes[i] = core.Mode{Name: m.Name, Value: m.Value}
	}
	return modes
}

// Mode implements core.ModeSource.
func (t *TUIBackend) Mode(name string) (string, bool) {
	if t.keyboard == nil {
		return "", false
	}
	return t.keyboard.Mode(name)
}

// SetMode implements core.ModeSource.
//
// Before the keyboard is started there is nowhere to keep a mode, so a write is
// refused rather than silently dropped — a host publishing its own state can
// tell the difference and try again once the backend is running.
func (t *TUIBackend) SetMode(name, value string) bool {
	if t.keyboard == nil {
		return false
	}
	return t.keyboard.SetMode(name, value)
}
