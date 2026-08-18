//go:build sdl

package sdl

import (
	"sort"
	"sync"

	"github.com/phroun/kittytk/core"
	sdl3 "github.com/phroun/kittytk/sdl/sdl3"
)

// The keyboard states this host can see, published the way the toolkit
// publishes them everywhere (see core/modes.go).
//
// This host asks the window system rather than waiting to be told, which the
// terminal host cannot do: out there the latches ride in on keystrokes, so
// Caps Lock is only ever as fresh as the last one. Here SDL keeps the modifier
// state and answers whenever asked — but only on the platform thread, so the
// answer is cached at the moments an answer is worth having (a key, and the
// return of focus, which is when a latch moved while we were not looking).

// modeState is the part of the modes list this host keeps itself. The pad's
// lock lives in padLock, which owns it for the key-naming path too.
type modeState struct {
	mu sync.Mutex

	caps      bool
	capsKnown bool

	focus      bool
	focusKnown bool

	// extra are states a HOST published through SetMode, stored and reported
	// without this package knowing what any of them mean.
	extra map[string]string
}

// Modes implements core.ModeSource.
func (p *Platform) Modes() []core.Mode {
	modes := []core.Mode{{Name: core.ModeNumLock, Value: modeValue(p.padLock.locked())}}

	p.modes.mu.Lock()
	if p.modes.capsKnown {
		modes = append(modes, core.Mode{Name: core.ModeCapsLock, Value: modeValue(p.modes.caps)})
	}
	if p.modes.focusKnown {
		modes = append(modes, core.Mode{Name: core.ModeFocus, Value: modeValue(p.modes.focus)})
	}
	for name, value := range p.modes.extra {
		modes = append(modes, core.Mode{Name: name, Value: value})
	}
	p.modes.mu.Unlock()

	sort.Slice(modes, func(i, j int) bool { return modes[i].Name < modes[j].Name })
	return modes
}

// Mode implements core.ModeSource.
func (p *Platform) Mode(name string) (string, bool) {
	if name == core.ModeNumLock {
		return modeValue(p.padLock.locked()), true
	}
	p.modes.mu.Lock()
	defer p.modes.mu.Unlock()
	switch name {
	case core.ModeCapsLock:
		if !p.modes.capsKnown {
			return "", false
		}
		return modeValue(p.modes.caps), true
	case core.ModeFocus:
		if !p.modes.focusKnown {
			return "", false
		}
		return modeValue(p.modes.focus), true
	}
	value, ok := p.modes.extra[name]
	return value, ok
}

// SetMode implements core.ModeSource.
//
// A name this host does not keep becomes a mode of its own; the empty value
// removes it again. The states this host DOES keep go on being watched, so a
// write to one of them is a belief held until the window system contradicts it
// — which is exactly what moving the simulated pad lock needs on a Mac, and
// what the next keystroke undoes anywhere with a real latch.
func (p *Platform) SetMode(name, value string) bool {
	changed, mode := p.setMode(name, value)
	if changed {
		p.announceMode(mode)
	}
	return changed
}

func (p *Platform) setMode(name, value string) (bool, core.Mode) {
	switch name {
	case core.ModeNumLock, core.ModeCapsLock, core.ModeFocus:
		if value != core.ModeOn && value != core.ModeOff {
			return false, core.Mode{}
		}
		on := value == core.ModeOn
		if name == core.ModeNumLock {
			return p.padLock.set(on), core.Mode{Name: name, Value: value}
		}
		p.modes.mu.Lock()
		defer p.modes.mu.Unlock()
		if name == core.ModeCapsLock {
			if p.modes.capsKnown && p.modes.caps == on {
				return false, core.Mode{}
			}
			p.modes.caps, p.modes.capsKnown = on, true
		} else {
			if p.modes.focusKnown && p.modes.focus == on {
				return false, core.Mode{}
			}
			p.modes.focus, p.modes.focusKnown = on, true
		}
		return true, core.Mode{Name: name, Value: value}
	}

	p.modes.mu.Lock()
	defer p.modes.mu.Unlock()
	if value == "" {
		if _, ok := p.modes.extra[name]; !ok {
			return false, core.Mode{}
		}
		delete(p.modes.extra, name)
		return true, core.Mode{Name: name, Value: ""}
	}
	if p.modes.extra == nil {
		p.modes.extra = make(map[string]string)
	}
	if p.modes.extra[name] == value {
		return false, core.Mode{}
	}
	p.modes.extra[name] = value
	return true, core.Mode{Name: name, Value: value}
}

// noteCapsLock reads the window system's Caps Lock and reports whether it
// moved. Called where an answer is worth having: a key event, and focus
// returning to us — the one moment a latch can have moved unobserved.
func (p *Platform) noteCapsLock(mod uint16) bool {
	on := mod&sdl3.KMOD_CAPS != 0
	p.modes.mu.Lock()
	defer p.modes.mu.Unlock()
	if p.modes.capsKnown && p.modes.caps == on {
		return false
	}
	p.modes.caps, p.modes.capsKnown = on, true
	return true
}

// noteFocus records whether this host has the keyboard.
func (p *Platform) noteFocus(focused bool) bool {
	p.modes.mu.Lock()
	defer p.modes.mu.Unlock()
	if p.modes.focusKnown && p.modes.focus == focused {
		return false
	}
	p.modes.focus, p.modes.focusKnown = focused, true
	return true
}

// announceMode delivers the change to every surface, so a trinket drawing an
// indicator repaints wherever it is. A keyboard state belongs to the process,
// not to one window: the pad's lock is the same lock in all of them.
func (p *Platform) announceMode(m core.Mode) {
	for _, w := range p.wins {
		if w == nil || w.surface == nil || w.surface.handler == nil {
			continue
		}
		w.surface.handler.Event(core.ModeEvent{Mode: m})
		w.surface.Invalidate(core.UnitRect{})
	}
}

// modeValue spells a latch.
func modeValue(on bool) string {
	if on {
		return core.ModeOn
	}
	return core.ModeOff
}
