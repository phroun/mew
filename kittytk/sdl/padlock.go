//go:build sdl

package sdl

import (
	"runtime"
	"sync"

	sdl3 "github.com/phroun/kittytk/sdl/sdl3"
)

// The pad's lock is a STATE, and a state is not a key.
//
// One cap carries both ideas. HID names usage 0x53 "Keyboard Num Lock and
// Clear"; SDL names the same scancode NUMLOCKCLEAR and comments it "num lock on
// PC, clear on Mac keyboards". Neither will split it, because the difference is
// not a property of the key — it is a property of the system it is plugged
// into.
//
// So the cap is handled by what it does. Alone it moves the lock and is eaten,
// because its whole meaning is the state, and that state already decides what
// eleven other pad caps are called; nobody writes a binding against it. With a
// modifier held it is an ordinary key named Clear — an action, which is what
// bindings are for, and what the cap says on the keyboards that have no lock
// behind it.

// heldModifiers are the modifiers a hand is HOLDING when a key goes down.
//
// Stated as what a held modifier IS, never as what it is not. Masking off the
// latches we know about would fail open: SDL already carries a third latch
// (KMOD_SCROLL) beyond NumLock and CapsLock, and KanaLock and HangulLock are
// waiting behind that. A positive list cannot fail that way. The immediate form
// of the bug is pressing this cap while the lock is already ON, which arrives
// with KMOD_NUM set — read as a modifier, every second press would come out as
// Clear while the first was eaten.
const heldModifiers = sdl3.KMOD_SHIFT | sdl3.KMOD_CTRL | sdl3.KMOD_ALT |
	sdl3.KMOD_GUI | sdl3.KMOD_MODE

// padLock is what this host knows about the number pad's lock.
//
// hasLatch asks something about the SYSTEM, not the keyboard: is there a
// NumLock here at all? macOS has none — the pad is always numeric and the cap
// says Clear — while X11, Wayland and Windows each keep one.
//
// It is SEEDED from the platform rather than measured, which the terminal host
// does not have to do, and the difference is worth writing down. There, the
// "kitty" protocol resolves a dual-legend cap for us: 57406 is the pad's 7 and
// 57423 is the same cap's Home, so a digit arriving while the latch bit is
// clear proves there is no latch, and it proves it on the first pad keystroke.
// SDL deliberately does NOT resolve them — that is the entire reason keypadKey
// takes the lock as an argument — so the same disagreement cannot be observed
// here. The seed is the honest substitute, and an observation still overrules
// it: KMOD_NUM can only be set by a real latch, so seeing it once settles the
// question for good and gets the odd cases right (a Mac keyboard on Linux
// locks; a PC keyboard on a Mac does not, because the OS is what lacks the
// function).
type padLock struct {
	mu       sync.Mutex
	hasLatch bool
	on       bool
}

func newPadLock() *padLock {
	// A locked pad is what the printed legends promise, what a pad with no
	// latch does permanently, and what a pad with one is in almost always.
	return &padLock{hasLatch: runtime.GOOS != "darwin", on: true}
}

// resolve reads what an arriving key says about the lock, then STAMPS the
// answer back into the keysym's KMOD_NUM bit.
//
// Stamping is what keeps this to one place. Every namer downstream — encodeKey,
// bareKey, the pad-typed check — already reads that bit to pick between a
// cap's two legends, and after this they are all reading a bit that has been
// corrected rather than the raw one. On a system with a real latch the stamp
// writes back exactly what was there.
func (l *padLock) resolve(sym *sdl3.Keysym) {
	l.mu.Lock()
	if sym.Mod&sdl3.KMOD_NUM != 0 {
		// Nothing but a real latch produces this bit, so the seed was wrong if
		// it said otherwise, and the OS owns the answer from here on. It is
		// re-read on every key, not just pad keys, because the user can toggle
		// the lock while this process is not focused and come back.
		l.hasLatch, l.on = true, true
	} else if l.hasLatch {
		l.on = false
	}
	on := l.on
	l.mu.Unlock()

	if on {
		sym.Mod |= sdl3.KMOD_NUM
	} else {
		sym.Mod &^= sdl3.KMOD_NUM
	}
}

// toggle moves the lock because the cap was pressed alone, and reports whether
// anything moved.
//
// Only where we own the state. On a system with a real latch the OS has already
// moved it and will say so in the modifier field of the very next event, so
// toggling here as well would race that and could land inverted.
func (l *padLock) toggle() (changed bool, on bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hasLatch {
		return false, l.on
	}
	l.on = !l.on
	return true, l.on
}

// set writes the lock and reports whether it moved.
//
// The write stands where we own the state. Where the OS keeps a real latch it
// is a BELIEF and the next key event overwrites it, which is honest: this
// process cannot move the system's latch, and refusing the write outright
// would leave a host unable to say what it had been asked to say.
func (l *padLock) set(on bool) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.on == on {
		return false
	}
	l.on = on
	return true
}

// locked reports whether the pad's dual-legend caps currently mean their digits.
// Answerable on every system, including those with no NumLock, where it is
// permanently true — so a host can draw an indicator everywhere, which is most
// useful exactly where the OS provides none.
func (l *padLock) locked() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.on
}

// eatsLockCap reports whether this keysym is the lock cap pressed ALONE, in
// which case it is not a key: the caller moves the lock and emits nothing.
func eatsLockCap(sym sdl3.Keysym) bool {
	return sym.Scancode == scanNumLock && sym.Mod&heldModifiers == 0
}
