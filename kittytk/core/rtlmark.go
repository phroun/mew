package core

import "sync"

// RTL-mark rendering hint. A host sets it; renderers read it. It lives in core
// rather than the text engine because the TUI backend (which has no font engine
// — glyphs are the outer terminal's concern) needs to read it too, and core is
// the one lightweight package every backend already imports.
//
// Values: "" / "normal" (default), "iterm2", "drift", … Only "drift" is acted
// on so far, by the TUI backend's cell emission.
var (
	rtlMarkModeMu  sync.RWMutex
	rtlMarkModeVal string
)

// SetRtlMarkMode stores the RTL-mark rendering hint.
func SetRtlMarkMode(mode string) {
	rtlMarkModeMu.Lock()
	rtlMarkModeVal = mode
	rtlMarkModeMu.Unlock()
}

// RtlMarkMode returns the stored RTL-mark rendering hint ("" when unset).
func RtlMarkMode() string {
	rtlMarkModeMu.RLock()
	defer rtlMarkModeMu.RUnlock()
	return rtlMarkModeVal
}

// Host bidi. A terminal that runs its OWN bidi over what it is sent orders a
// line the renderer has ALREADY ordered, and the line comes back the wrong way
// round. A host that knows which terminal it is talking to says so here, and
// the cell backend turns each right-to-left run back on the way out so the
// terminal's own pass turns it forward again.
//
// macOS Terminal.app is the one in common use; the stream-order terminals
// (iTerm2, Alacritty, Ghostty, Kitty) leave what they are sent alone. Unset
// means the second kind, which is the safe assumption: a flip sent to a
// terminal that does not reorder is itself the bug.
// The backend settles this from the terminal it finds itself in; a host that
// has learned better -- one that PROBED the terminal, and so can answer for one
// no name recognises -- says so, and its answer wins.
var (
	hostBidiMu       sync.RWMutex
	hostBidiApplies  bool
	hostBidiWordwise bool
	hostBidiSet      bool
	hostBidiSniffed  bool
	hostBidiSniffedW bool
)

// SetHostAppliesBidi records that the host terminal reorders what it is sent.
//
// wordwise says how it segments, which decides where the words land and where
// the attributes go: off for a host that reverses a whole parsed span colour
// and all, on for one that turns each whitespace-separated word in place and
// paints attributes at the physical column.
//
// This is the answer of something that KNOWS -- an application that probed the
// terminal rather than recognising its name -- so it stands over whatever the
// backend worked out for itself.
func SetHostAppliesBidi(applies, wordwise bool) {
	hostBidiMu.Lock()
	hostBidiApplies, hostBidiWordwise, hostBidiSet = applies, wordwise, true
	hostBidiMu.Unlock()
}

// SetSniffedHostBidi records what a backend worked out from the terminal it
// finds itself in. It is the default, and an explicit SetHostAppliesBidi
// replaces it.
func SetSniffedHostBidi(applies, wordwise bool) {
	hostBidiMu.Lock()
	hostBidiSniffed, hostBidiSniffedW = applies, wordwise
	hostBidiMu.Unlock()
}

// ForgetHostBidi drops both answers, leaving the process as it started: nothing
// recognised and nothing said. A backend recognising the terminal it is built
// inside settles it again.
func ForgetHostBidi() {
	hostBidiMu.Lock()
	hostBidiApplies, hostBidiWordwise, hostBidiSet = false, false, false
	hostBidiSniffed, hostBidiSniffedW = false, false
	hostBidiMu.Unlock()
}

// HostAppliesBidi returns whether the host reorders what it is sent, and how it
// segments: what a host has said if one has, and what the backend recognised
// otherwise.
func HostAppliesBidi() (applies, wordwise bool) {
	hostBidiMu.RLock()
	defer hostBidiMu.RUnlock()
	if hostBidiSet {
		return hostBidiApplies, hostBidiWordwise
	}
	return hostBidiSniffed, hostBidiSniffedW
}
