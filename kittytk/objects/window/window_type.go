package window

import "github.com/phroun/kittytk/core"

// WindowType classifies a window's role, replacing the old modal/tool flags.
// It governs how a window may be created (over the protocol), whether it joins
// a modal stack, and how it stacks relative to its owner.
type WindowType int

const (
	// WindowTypeNormal is an ordinary application window. Over the protocol a
	// client may only create these when its application declares multiwindow.
	WindowTypeNormal WindowType = iota
	// WindowTypeMain is the application's single main window.
	WindowTypeMain
	// WindowTypeMDIChild is a child window inside an MDI pane.
	WindowTypeMDIChild
	// WindowTypeDialog is an application- or window-level dialog: it floats
	// above its owner but never blocks input and joins no modal stack.
	WindowTypeDialog
	// WindowTypeModal is an application-, window-, or system-level modal: it
	// floats above its owner and joins a modal stack that blocks input.
	WindowTypeModal
	// WindowTypeToolPalette is an application- or window-level tool palette:
	// like a dialog it floats above its owner and blocks nothing, but when
	// focused it brings its whole owner group forward.
	WindowTypeToolPalette
)

// String returns the wire/debug name of the type.
func (t WindowType) String() string {
	switch t {
	case WindowTypeMain:
		return "main"
	case WindowTypeNormal:
		return "normal"
	case WindowTypeMDIChild:
		return "mdichild"
	case WindowTypeDialog:
		return "dialog"
	case WindowTypeModal:
		return "modal"
	case WindowTypeToolPalette:
		return "toolpalette"
	default:
		return "normal"
	}
}

// WindowTypeFromString parses a wire type name, reporting whether it was known.
func WindowTypeFromString(s string) (WindowType, bool) {
	switch s {
	case "main":
		return WindowTypeMain, true
	case "normal":
		return WindowTypeNormal, true
	case "mdichild":
		return WindowTypeMDIChild, true
	case "dialog":
		return WindowTypeDialog, true
	case "modal":
		return WindowTypeModal, true
	case "toolpalette":
		return WindowTypeToolPalette, true
	default:
		return 0, false
	}
}

// IsOwnedOverlay reports whether the type is one that is owned by another
// window and floats above it: dialog, modal, or tool palette. These are the
// types that carry an owner and are forced on top of it in the z-order.
func (t WindowType) IsOwnedOverlay() bool {
	return t == WindowTypeDialog || t == WindowTypeModal || t == WindowTypeToolPalette
}

// Type returns the window's role.
func (w *Window) Type() WindowType {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.windowType
}

// SetType sets the window's role.
func (w *Window) SetType(t WindowType) {
	w.mu.Lock()
	w.windowType = t
	w.mu.Unlock()
}

// Owner returns the resolved owner of an owned-overlay window (the window it
// floats above), or nil when it is application-level (no owner).
func (w *Window) Owner() *Window {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.owner
}

// SetOwner sets the window's owner, resolving up the ownership chain: an owner
// that is itself an owned overlay (dialog/modal/toolpalette) is skipped until a
// non-overlay window is reached, and that is stored. A nil owner (or one that
// resolves to nil) leaves the window application-level.
func (w *Window) SetOwner(owner *Window) {
	for owner != nil && owner != w && owner.Type().IsOwnedOverlay() {
		owner = owner.Owner()
	}
	if owner == w {
		owner = nil
	}
	w.mu.Lock()
	w.owner = owner
	w.mu.Unlock()
}

// OwnerRequestID returns the wire object id an owner= property requested (0 if
// none), for the display layer to resolve into the owner window at adoption.
func (w *Window) OwnerRequestID() uint64 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.ownerRequestID
}

// SetOwnerRequestID records the wire object id from an owner= property.
func (w *Window) SetOwnerRequestID(id uint64) {
	w.mu.Lock()
	w.ownerRequestID = id
	w.mu.Unlock()
}

// AppID returns the ObjectID of the application that owns this window, or 0
// when it has no application (a system window such as an auth prompt).
func (w *Window) AppID() core.ObjectID {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.appID
}

// SetAppID records the owning application's ObjectID. Called by the
// application when it adopts the window, so the manager can scope
// application-level modal blocking to windows of the same app.
func (w *Window) SetAppID(id core.ObjectID) {
	w.mu.Lock()
	w.appID = id
	w.mu.Unlock()
}
