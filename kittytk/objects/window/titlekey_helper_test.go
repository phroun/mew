package window

import "github.com/phroun/kittytk/core"

// titleKey presses one key at a window's focused title bar, resolving it
// through the keymap exactly as HandleKeyPress does. Tests name the key the
// way a backend emits it -- composed, with its modifiers in the string.
func titleKey(w *Window, key string) bool {
	event := core.KeyPressEvent{Key: key}
	return w.handleTitleBarKey(event, w.KeyCommand(key))
}
