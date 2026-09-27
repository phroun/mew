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

// Whether the combining marks that ride a right-to-left letter are SHOWN.
//
// They are what a terminal that reorders miscounts, and on such a host they are
// the only thing a line can give up: a display that wants its selection bars,
// its highlights and its gutter more than its vowels turns this off, and
// pointed Hebrew renders one codepoint per cell the way pre-shaped Arabic does.
// A folding rtl-mark mode keeps the points even so, folded into their letters.
//
// On by default, and answered only where a cell target is drawing -- a pixel
// one composes the marks properly and has no terminal to disagree with.
var (
	rtlCombiningMu   sync.RWMutex
	rtlCombiningShow = true
)

// SetRtlCombining stores whether the marks riding a right-to-left letter are
// shown. Off gives up the marks to keep the fill.
func SetRtlCombining(show bool) {
	rtlCombiningMu.Lock()
	rtlCombiningShow = show
	rtlCombiningMu.Unlock()
}

// RtlCombining reports whether those marks are shown (the default).
func RtlCombining() bool {
	rtlCombiningMu.RLock()
	defer rtlCombiningMu.RUnlock()
	return rtlCombiningShow
}

// RtlMarkFolds reports whether the active hint folds a cluster's points into
// its base's presentation form, leaving nothing free-standing for a renderer to
// misplace. It is what decides whether a pointed line comes out even.
func RtlMarkFolds() bool {
	switch RtlMarkMode() {
	case "iterm2", "compose", "drift":
		return true
	}
	return false
}

// ZeroWidth reports whether a rune takes no cell of its own under the installed
// cell-width rule -- a combining mark, a joiner -- and is not a control, which
// a renderer stands something visible in place of.
func ZeroWidth(r rune) bool {
	return r >= 0x20 && r != 0x7F && CellWidth(r) == 0
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
	hostBidiRideSafe bool
	hostBidiSet      bool
	hostBidiSniffed  bool
	hostBidiSniffedW bool
	hostBidiSniffedR bool
)

// SetHostAppliesBidi records that the host terminal reorders what it is sent.
//
// wordwise says how it segments, which decides where the words land and where
// the attributes go: off for a host that reverses a whole parsed span colour
// and all, on for one that turns each whitespace-separated word in place and
// paints attributes at the physical column. rideSafe says its reordering
// miscounts a background fill (see HostMiscountsFill).
//
// This is the answer of something that KNOWS -- an application that probed the
// terminal rather than recognising its name -- so it stands over whatever the
// backend worked out for itself.
func SetHostAppliesBidi(applies, wordwise, rideSafe bool) {
	hostBidiMu.Lock()
	hostBidiApplies, hostBidiWordwise, hostBidiRideSafe = applies, wordwise, rideSafe
	hostBidiSet = true
	hostBidiMu.Unlock()
}

// SetSniffedHostBidi records what a backend worked out from the terminal it
// finds itself in. It is the default, and an explicit SetHostAppliesBidi
// replaces it.
func SetSniffedHostBidi(applies, wordwise, rideSafe bool) {
	hostBidiMu.Lock()
	hostBidiSniffed, hostBidiSniffedW, hostBidiSniffedR = applies, wordwise, rideSafe
	hostBidiMu.Unlock()
}

// ForgetHostBidi drops both answers, leaving the process as it started: nothing
// recognised and nothing said. A backend recognising the terminal it is built
// inside settles it again.
func ForgetHostBidi() {
	hostBidiMu.Lock()
	hostBidiApplies, hostBidiWordwise, hostBidiRideSafe, hostBidiSet = false, false, false, false
	hostBidiSniffed, hostBidiSniffedW, hostBidiSniffedR = false, false, false
	hostBidiMu.Unlock()
}

// HostAppliesBidi returns whether the host reorders what it is sent, and how it
// segments: what a host has said if one has, and what the backend recognised
// otherwise.
func HostAppliesBidi() (applies, wordwise bool) {
	a, w, _ := hostBidi()
	return a, w
}

// HostMiscountsFill reports whether the host's own reordering MISCOUNTS a
// background fill -- counting codepoints where the grid counts cells, so a
// highlight over a line holding combining marks lands on the wrong cells and
// half-vanishes.
//
// A caller drawing such a line reaches for foreground colour and weight
// instead, which ride each glyph through the reordering intact. Whether the
// line is one of those is khatool.HasZeroWidthAfterFold's question; this is
// only whether the host has the fault at all.
func HostMiscountsFill() bool {
	_, _, r := hostBidi()
	return r
}

func hostBidi() (applies, wordwise, rideSafe bool) {
	hostBidiMu.RLock()
	defer hostBidiMu.RUnlock()
	if hostBidiSet {
		return hostBidiApplies, hostBidiWordwise, hostBidiRideSafe
	}
	return hostBidiSniffed, hostBidiSniffedW, hostBidiSniffedR
}
