// Package core provides fundamental types for KittyTK.
package core

import "sync"

// KeyBindings centralizes all key bindings for the toolkit.
// Key names follow direct-key-handler conventions:
//   - Control+letter: "^A", "^X", "^C", "^V" etc.
//   - Special keys: "Left", "Right", "Up", "Down", "Home", "End",
//     "Enter", "Tab", "Escape", "Backspace", "Delete",
//     "PageUp", "PageDown", "Insert"
//   - Function keys: "F1", "F2", ... "F12"
//   - Alt combinations: "M-" prefix (e.g., "M-x", "M-Tab")
//   - Shift combinations: "S-" prefix (e.g., "S-Tab", "S-Left")
//   - Combined: "M-S-Tab" (Alt+Shift+Tab), "C-S-s" (Ctrl+Shift+s)
type KeyBindings struct {
	mu       sync.RWMutex
	bindings map[string][]string // action -> list of keys
}

// Standard action names for keyboard navigation and editing.
const (
	// Navigation
	ActionMoveLeft     = "move-left"
	ActionMoveRight    = "move-right"
	ActionMoveUp       = "move-up"
	ActionMoveDown     = "move-down"
	ActionMoveHome     = "move-home"
	ActionMoveEnd      = "move-end"
	ActionPageUp       = "page-up"
	ActionPageDown     = "page-down"

	// Selection (navigation with selection)
	ActionSelectLeft  = "select-left"
	ActionSelectRight = "select-right"
	ActionSelectUp    = "select-up"
	ActionSelectDown  = "select-down"
	ActionSelectHome  = "select-home"
	ActionSelectEnd   = "select-end"
	ActionSelectAll   = "select-all"

	// Editing
	ActionBackspace = "backspace"
	ActionDelete    = "delete"
	ActionCut       = "cut"
	ActionCopy      = "copy"
	ActionPaste     = "paste"
	ActionUndo      = "undo"
	ActionRedo      = "redo"
	ActionClearLine = "clear-line"

	// Activation
	ActionActivate = "activate" // Enter, Space
	ActionCancel   = "cancel"   // Escape
	ActionConfirm  = "confirm"  // Enter

	// Focus
	ActionFocusNext = "focus-next"
	ActionFocusPrev = "focus-prev"

	// Tree/List
	ActionExpand      = "expand"
	ActionCollapse    = "collapse"
	ActionExpandAll   = "expand-all"
	ActionCollapseAll = "collapse-all"

	// Menu
	ActionMenuOpen   = "menu-open"
	ActionMenuClose  = "menu-close"
	ActionMenuToggle = "menu-toggle"

	// Window
	ActionWindowClose = "window-close"
	ActionWindowNext  = "window-next"
	ActionWindowPrev  = "window-prev"

	// Application
	ActionQuit          = "quit"
	ActionExitDesktop   = "exit-desktop"
	ActionAppHide       = "app-hide"
	ActionAppHideOthers = "app-hide-others"
	ActionAppShowAll    = "app-show-all"

	// Key pass-through
	ActionPassNextKeyToTrinket = "pass-next-key-to-trinket"
)

// NewKeyBindings creates a new key bindings map with defaults.
func NewKeyBindings() *KeyBindings {
	kb := &KeyBindings{
		bindings: make(map[string][]string),
	}
	kb.SetDefaults()
	return kb
}

// SetDefaults sets all default key bindings.
func (kb *KeyBindings) SetDefaults() {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	// Navigation
	kb.bindings[ActionMoveLeft] = []string{"Left"}
	kb.bindings[ActionMoveRight] = []string{"Right"}
	kb.bindings[ActionMoveUp] = []string{"Up"}
	kb.bindings[ActionMoveDown] = []string{"Down"}
	kb.bindings[ActionMoveHome] = []string{"Home", "^A"} // ^A is Emacs home
	kb.bindings[ActionMoveEnd] = []string{"End", "^E"}   // ^E is Emacs end
	kb.bindings[ActionPageUp] = []string{"PageUp"}
	kb.bindings[ActionPageDown] = []string{"PageDown"}

	// Selection
	kb.bindings[ActionSelectLeft] = []string{"S-Left"}
	kb.bindings[ActionSelectRight] = []string{"S-Right"}
	kb.bindings[ActionSelectUp] = []string{"S-Up"}
	kb.bindings[ActionSelectDown] = []string{"S-Down"}
	kb.bindings[ActionSelectHome] = []string{"S-Home"}
	kb.bindings[ActionSelectEnd] = []string{"S-End"}
	kb.bindings[ActionSelectAll] = []string{"M-a"} // Meta+A (not ^A which is Emacs home)

	// Editing
	kb.bindings[ActionBackspace] = []string{"Backspace", "^H"}
	kb.bindings[ActionDelete] = []string{"Delete", "^D"}
	kb.bindings[ActionCut] = []string{"^X"}
	kb.bindings[ActionCopy] = []string{"^C"}
	kb.bindings[ActionPaste] = []string{"^V"}
	kb.bindings[ActionUndo] = []string{"^Z"}
	kb.bindings[ActionRedo] = []string{"^Y", "^S-Z"}
	kb.bindings[ActionClearLine] = []string{"^U"}

	// Activation
	kb.bindings[ActionActivate] = []string{"Enter", " "}
	kb.bindings[ActionCancel] = []string{"Escape"}
	kb.bindings[ActionConfirm] = []string{"Enter"}

	// Focus
	kb.bindings[ActionFocusNext] = []string{"Tab"}
	kb.bindings[ActionFocusPrev] = []string{"S-Tab"}

	// Tree/List
	kb.bindings[ActionExpand] = []string{"Right", "+"}
	kb.bindings[ActionCollapse] = []string{"Left", "-"}
	kb.bindings[ActionExpandAll] = []string{"*"}
	kb.bindings[ActionCollapseAll] = []string{"/"}

	// Menu
	kb.bindings[ActionMenuOpen] = []string{"Enter", " ", "Down"}
	kb.bindings[ActionMenuClose] = []string{"Escape"}
	kb.bindings[ActionMenuToggle] = []string{"F10"}

	// Window
	kb.bindings[ActionWindowClose] = []string{"M-F4", "^W"}
	kb.bindings[ActionWindowNext] = []string{"M-Tab", "^Tab"}
	kb.bindings[ActionWindowPrev] = []string{"M-S-Tab", "^S-Tab"}

	// Application
	kb.bindings[ActionQuit] = []string{"^Q"}
	kb.bindings[ActionExitDesktop] = []string{"M-^X"}
	kb.bindings[ActionAppHide] = []string{"^H"}
	kb.bindings[ActionAppHideOthers] = []string{"M-^H"}
	kb.bindings[ActionAppShowAll] = []string{} // No default binding

	// Key pass-through (Ctrl+Backslash)
	kb.bindings[ActionPassNextKeyToTrinket] = []string{"^\\"}
}

// Bind sets the keys for an action (replaces existing).
func (kb *KeyBindings) Bind(action string, keys ...string) {
	kb.mu.Lock()
	defer kb.mu.Unlock()
	kb.bindings[action] = keys
}

// AddBinding adds additional keys to an action.
func (kb *KeyBindings) AddBinding(action string, keys ...string) {
	kb.mu.Lock()
	defer kb.mu.Unlock()
	kb.bindings[action] = append(kb.bindings[action], keys...)
}

// Unbind removes a specific key from an action.
func (kb *KeyBindings) Unbind(action, key string) {
	kb.mu.Lock()
	defer kb.mu.Unlock()
	keys := kb.bindings[action]
	for i, k := range keys {
		if k == key {
			kb.bindings[action] = append(keys[:i], keys[i+1:]...)
			return
		}
	}
}

// ClearAction removes all bindings for an action.
func (kb *KeyBindings) ClearAction(action string) {
	kb.mu.Lock()
	defer kb.mu.Unlock()
	delete(kb.bindings, action)
}

// Keys returns all keys bound to an action.
func (kb *KeyBindings) Keys(action string) []string {
	kb.mu.RLock()
	defer kb.mu.RUnlock()
	return kb.bindings[action]
}

// MatchesAction checks if a key matches any binding for an action.
func (kb *KeyBindings) MatchesAction(action, key string) bool {
	kb.mu.RLock()
	defer kb.mu.RUnlock()
	for _, k := range kb.bindings[action] {
		if k == key {
			return true
		}
	}
	return false
}

// FindAction returns the action for a key, or empty string if none.
func (kb *KeyBindings) FindAction(key string) string {
	kb.mu.RLock()
	defer kb.mu.RUnlock()
	for action, keys := range kb.bindings {
		for _, k := range keys {
			if k == key {
				return action
			}
		}
	}
	return ""
}

// AllBindings returns a copy of all bindings.
func (kb *KeyBindings) AllBindings() map[string][]string {
	kb.mu.RLock()
	defer kb.mu.RUnlock()
	result := make(map[string][]string)
	for action, keys := range kb.bindings {
		result[action] = append([]string{}, keys...)
	}
	return result
}

// DefaultKeyBindings is the global default key bindings.
var DefaultKeyBindings = NewKeyBindings()
