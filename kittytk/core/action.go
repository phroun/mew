// Package core provides fundamental types for KittyTK.
package core

import (
	"sync"
)

// Action represents a named operation that can be triggered by
// keyboard shortcuts, menu items, buttons, or programmatically.
// Actions support configurable keybindings.
type Action struct {
	mu sync.RWMutex

	// ID is a unique identifier for the action (e.g., "file.save", "edit.copy").
	ID string

	// Text is the human-readable name shown in menus/buttons.
	Text string

	// Description is a longer description for tooltips/accessibility.
	Description string

	// Icon is an optional icon identifier (for future GUI support).
	Icon string

	// Shortcut is the primary keyboard shortcut.
	Shortcut Shortcut

	// AlternateShortcuts are additional shortcuts for this action.
	AlternateShortcuts []Shortcut

	// Enabled controls whether the action can be triggered.
	Enabled bool

	// Visible controls whether the action appears in menus.
	Visible bool

	// Checkable indicates this is a toggle action.
	Checkable bool

	// Checked is the current state if Checkable.
	Checked bool

	// OnTriggered is called when the action is activated.
	OnTriggered func()

	// OnToggled is called when a checkable action changes state.
	OnToggled func(checked bool)
}

// NewAction creates a new action with the given ID and text.
func NewAction(id, text string) *Action {
	return &Action{
		ID:      id,
		Text:    text,
		Enabled: true,
		Visible: true,
	}
}

// WithShortcut sets the primary shortcut and returns the action.
func (a *Action) WithShortcut(s Shortcut) *Action {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Shortcut = s
	return a
}

// WithDescription sets the description and returns the action.
func (a *Action) WithDescription(desc string) *Action {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Description = desc
	return a
}

// WithHandler sets the triggered handler and returns the action.
func (a *Action) WithHandler(handler func()) *Action {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.OnTriggered = handler
	return a
}

// Trigger activates the action if enabled.
func (a *Action) Trigger() {
	a.mu.RLock()
	enabled := a.Enabled
	handler := a.OnTriggered
	checkable := a.Checkable
	a.mu.RUnlock()

	if !enabled {
		return
	}

	if checkable {
		a.Toggle()
	} else if handler != nil {
		handler()
	}
}

// Toggle changes the checked state of a checkable action.
func (a *Action) Toggle() {
	a.mu.Lock()
	if !a.Checkable {
		a.mu.Unlock()
		return
	}
	a.Checked = !a.Checked
	checked := a.Checked
	handler := a.OnToggled
	a.mu.Unlock()

	if handler != nil {
		handler(checked)
	}
}

// SetChecked sets the checked state.
func (a *Action) SetChecked(checked bool) {
	a.mu.Lock()
	if a.Checked == checked {
		a.mu.Unlock()
		return
	}
	a.Checked = checked
	handler := a.OnToggled
	a.mu.Unlock()

	if handler != nil {
		handler(checked)
	}
}

// Shortcut represents a keyboard shortcut using the key handler format.
// Examples: "^Q" (Ctrl+Q), "M-x" (Mega+x), "S-Tab" (Shift+Tab), "F1" (plain key)
type Shortcut string

// NoShortcut represents the absence of a shortcut.
const NoShortcut Shortcut = ""

// NewShortcut creates a shortcut from a key handler format string.
// This is the primary way to create shortcuts.
func NewShortcut(key string) Shortcut {
	return Shortcut(key)
}

// String returns the shortcut in key handler format.
func (s Shortcut) String() string {
	return string(s)
}

// macNativeShortcuts, when set, makes DisplayString render shortcuts with
// macOS's native modifier glyphs (⌃⌥⇧⌘) in canonical order. The host enables
// it only when the ini's [system] native=true and the host OS is macOS; in
// every other case the compact key-handler notation is shown unchanged.
var macNativeShortcuts bool

// SetMacNativeShortcuts toggles macOS-native shortcut glyph rendering for
// menus and tooltips. Called once at host startup.
func SetMacNativeShortcuts(on bool) { macNativeShortcuts = on }

// MacShortcutFontFamily is the font family the graphical renderer uses to draw
// menu shortcuts when macOS-native rendering is on. The text engine registers
// macOS's UI font under this name (best-effort - it falls back to the default
// UI face when that font isn't present, e.g. off macOS), so the native
// modifier glyphs ⌃⌥⇧⌘ appear in Apple's own typeface. Because the same font
// object is used to measure and to draw, switching families never desyncs the
// menu's width machinery.
const MacShortcutFontFamily = "apple-menu"

// MacNativeShortcuts reports whether macOS-native shortcut rendering is on.
func MacNativeShortcuts() bool { return macNativeShortcuts }

// DisplayString returns a human-readable representation of the shortcut
// for display in menus and tooltips.
//
// Deprecated: a Shortcut is a key string like any other. Call DisplayKey.
func (s Shortcut) DisplayString() string { return DisplayKey(string(s)) }

// AccessibilityString returns a fully spelled-out representation of the
// shortcut for screen reader announcements.
//
// Deprecated: a Shortcut is a key string like any other. Call SpeakKey.
func (s Shortcut) AccessibilityString() string { return SpeakKey(string(s)) }

// ActionGroup manages a collection of related actions.
type ActionGroup struct {
	mu      sync.RWMutex
	actions map[string]*Action
	order   []string // Maintains insertion order
}

// NewActionGroup creates a new action group.
func NewActionGroup() *ActionGroup {
	return &ActionGroup{
		actions: make(map[string]*Action),
	}
}

// Add adds an action to the group.
func (g *ActionGroup) Add(action *Action) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, exists := g.actions[action.ID]; !exists {
		g.order = append(g.order, action.ID)
	}
	g.actions[action.ID] = action
}

// Get returns an action by ID.
func (g *ActionGroup) Get(id string) *Action {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.actions[id]
}

// All returns all actions in insertion order.
func (g *ActionGroup) All() []*Action {
	g.mu.RLock()
	defer g.mu.RUnlock()
	result := make([]*Action, 0, len(g.order))
	for _, id := range g.order {
		if a, ok := g.actions[id]; ok {
			result = append(result, a)
		}
	}
	return result
}

// ShortcutMap provides a way to customize keybindings.
// It maps action IDs to shortcuts.
type ShortcutMap struct {
	mu       sync.RWMutex
	bindings map[string][]Shortcut
}

// NewShortcutMap creates a new shortcut map.
func NewShortcutMap() *ShortcutMap {
	return &ShortcutMap{
		bindings: make(map[string][]Shortcut),
	}
}

// Set sets the shortcuts for an action using key handler format strings.
func (m *ShortcutMap) Set(actionID string, keys ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	shortcuts := make([]Shortcut, len(keys))
	for i, key := range keys {
		shortcuts[i] = Shortcut(key)
	}
	m.bindings[actionID] = shortcuts
}

// Get returns the shortcuts for an action.
func (m *ShortcutMap) Get(actionID string) []Shortcut {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.bindings[actionID]
}

// Apply applies this shortcut map to an action group.
func (m *ShortcutMap) Apply(group *ActionGroup) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for actionID, shortcuts := range m.bindings {
		action := group.Get(actionID)
		if action == nil {
			continue
		}
		action.mu.Lock()
		if len(shortcuts) > 0 {
			action.Shortcut = shortcuts[0]
			action.AlternateShortcuts = shortcuts[1:]
		} else {
			action.Shortcut = NoShortcut
			action.AlternateShortcuts = nil
		}
		action.mu.Unlock()
	}
}

// StandardActions provides common action IDs.
var StandardActions = struct {
	// File actions
	New    string
	Open   string
	Save   string
	SaveAs string
	Close  string
	Quit   string

	// Edit actions
	Undo      string
	Redo      string
	Cut       string
	Copy      string
	Paste     string
	Delete    string
	SelectAll string
	Find      string
	Replace   string

	// View actions
	ZoomIn    string
	ZoomOut   string
	ZoomReset string

	// Window actions
	Minimize    string
	Maximize    string
	Restore     string
	CloseWindow string

	// Navigation
	FocusNext string
	FocusPrev string
	Escape    string
	Confirm   string
}{
	New:    "file.new",
	Open:   "file.open",
	Save:   "file.save",
	SaveAs: "file.save_as",
	Close:  "file.close",
	Quit:   "app.quit",

	Undo:      "edit.undo",
	Redo:      "edit.redo",
	Cut:       "edit.cut",
	Copy:      "edit.copy",
	Paste:     "edit.paste",
	Delete:    "edit.delete",
	SelectAll: "edit.select_all",
	Find:      "edit.find",
	Replace:   "edit.replace",

	ZoomIn:    "view.zoom_in",
	ZoomOut:   "view.zoom_out",
	ZoomReset: "view.zoom_reset",

	Minimize:    "window.minimize",
	Maximize:    "window.maximize",
	Restore:     "window.restore",
	CloseWindow: "window.close",

	FocusNext: "focus.next",
	FocusPrev: "focus.prev",
	Escape:    "dialog.escape",
	Confirm:   "dialog.confirm",
}

// DefaultShortcuts returns the default keyboard shortcuts.
// All shortcuts use the key handler format directly.
func DefaultShortcuts() *ShortcutMap {
	m := NewShortcutMap()

	// File - using key handler format: ^x = Ctrl+x
	m.Set(StandardActions.New, "^N")
	m.Set(StandardActions.Open, "^O")
	m.Set(StandardActions.Save, "^S")
	m.Set(StandardActions.SaveAs, "^S-S") // Ctrl+Shift+S
	m.Set(StandardActions.Close, "^W")
	m.Set(StandardActions.Quit, "^Q")

	// Edit
	m.Set(StandardActions.Undo, "^Z")
	m.Set(StandardActions.Redo, "^S-Z", "^Y") // Ctrl+Shift+Z or Ctrl+Y
	m.Set(StandardActions.Cut, "^X")
	m.Set(StandardActions.Copy, "^C")
	m.Set(StandardActions.Paste, "^V")
	m.Set(StandardActions.Delete, "Delete")
	m.Set(StandardActions.SelectAll, "M-a")
	m.Set(StandardActions.Find, "^F")
	m.Set(StandardActions.Replace, "^H")

	// Note: Tab, S-Tab, Escape, Enter are handled by Window's FocusManager
	// and dialog trinkets directly, not through the global shortcut system.

	return m
}
