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
