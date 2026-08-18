package core

// The modes a keyboard is IN, as opposed to the keys it sends.
//
// A latch like the pad's lock is not a keystroke and not a chord: it is a
// standing state that decides what other keys MEAN, and something on screen
// usually has to say so. Status bars have shown CAPS and NUM since Lotus 1-2-3.
//
// Each host knows a different set — the terminal backend learns what the kitty
// protocol tells it, a graphical host can ask the window system directly — so
// the states are published under stable tokens with token values and a consumer
// draws whichever it recognizes, however it likes. A host can publish a state
// of its own with SetMode, and it is reported beside the rest.
//
// A state the host cannot determine is ABSENT rather than off. Those are two
// different pictures on screen: "the lock is off" and "we cannot see the lock
// from here".

// Mode is one state, named by a token and valued by a token.
type Mode struct {
	Name  string
	Value string
}

// The states a host may publish. A host reports only what it can see.
const (
	ModeNumLock  = "num"   // the pad's lock: are the dual-legend caps digits?
	ModeCapsLock = "caps"  // Caps Lock
	ModeFocus    = "focus" // does this host have the keyboard?
)

// The two values a latch takes. A mode that is not a latch may use others.
const (
	ModeOn  = "on"
	ModeOff = "off"
)

// ModeSource is an optional host capability: the keyboard states it can see.
//
// Both hosts implement it — the terminal backend by passing through to the key
// layer, the graphical host from the window system — so one indicator can be
// drawn against either without knowing which is underneath.
type ModeSource interface {
	// Modes returns every state the host can answer for, sorted by name.
	Modes() []Mode

	// Mode returns one state's value. ok is false when the host cannot tell,
	// which a display should draw greyed rather than off.
	Mode(name string) (string, bool)

	// SetMode writes a state and reports whether anything changed. A name the
	// host does not keep becomes a mode of its own, so an application state —
	// insert/overtype, a recording light — can be published here and drawn by
	// the same indicator. Setting such a mode to the empty string removes it.
	SetMode(name, value string) bool
}

// ModeEvent reports that a state moved, so a trinket drawing an indicator
// repaints when it does rather than only when something else happens to.
type ModeEvent struct {
	Mode
}

func (ModeEvent) isEvent() {}
