# Multi-Application Desktop Plan

## Overview

Restructure the toolkit so Desktop is the top-level object, with multiple Application objects attached to it. Each Application manages its own windows, but Desktop manages the combined window list and coordinates context-sensitive menus and status bar.

## Key Concepts

### Desktop (top-level)
- Owns the terminal/renderer
- Manages combined list of all windows from all applications
- Provides the "ψ" system menu (always present, upper-left)
- Tracks which Application is "active" based on focused window
- Coordinates menu bar display (system menu + active app's menus)
- Coordinates status bar display (active app's status content)

### Application (per-app)
- Registers with Desktop
- Owns and manages lifecycle of its windows
- Provides menu bar content (displayed when app is active)
- Provides status bar content (displayed when app is active)
- Provides Actions for menus and context menus
- Receives menu/action dispatch when active

### System Menu (Ψ)
- Always visible in upper-left of menu bar
- Contains desktop-level items (not app-specific)
- "Desktop accessories" - small utilities common to all apps
- System settings, about, quit environment, etc.

### Actions (limited scope)
- First-class objects for menu items and context menus only
- NOT used for buttons, toolbars (those remain direct callbacks)
- Defined per-Application
- Contain: ID, text, shortcut, enabled state, handler

## Phased Implementation

### Phase 1: Desktop as Root

**Goal**: Desktop becomes the entry point without breaking existing code.

**Changes**:

1. `Desktop.Run()` - new method that starts the event loop (currently in app.Application)

2. `Desktop.SetApplication(app)` - registers a single application (backward compatible)

3. Move terminal/renderer ownership from app.Application to Desktop

4. app.Application becomes lighter - just window management and callbacks

**Backward compatibility**: Existing code using `app.NewApplication()` continues to work by auto-creating a Desktop internally.

```go
// New way (explicit)
desktop := trinkets.NewDesktop()
myApp := app.NewApplication("MyApp")
desktop.AddApplication(myApp)
desktop.Run()

// Old way (still works - creates desktop implicitly)
myApp := app.NewApplication("MyApp")
myApp.Run() // internally creates desktop, adds self, runs
```

### Phase 2: Application Interface

**Goal**: Define what Application provides to Desktop.

```go
type ApplicationProvider interface {
    // Identity
    Name() string

    // Window ownership
    Windows() []*window.Window
    AddWindow(w *window.Window)
    RemoveWindow(w *window.Window)

    // Menu content (called when app becomes active)
    MenuBarContent() []*Menu  // or menu model

    // Status bar content
    StatusBarContent() []StatusSection

    // Called when app becomes active/inactive
    OnActivate()
    OnDeactivate()
}
```

**Desktop tracking**:
```go
type Desktop struct {
    // ...existing fields...
    applications    []ApplicationProvider
    activeApp       ApplicationProvider
}

func (d *Desktop) windowFocusChanged(w *window.Window) {
    // Find which app owns this window
    owner := d.findApplicationForWindow(w)
    if owner != d.activeApp {
        if d.activeApp != nil {
            d.activeApp.OnDeactivate()
        }
        d.activeApp = owner
        if d.activeApp != nil {
            d.activeApp.OnActivate()
        }
        d.updateMenuBar()
        d.updateStatusBar()
    }
}
```

### Phase 3: System Menu (Ψ)

**Goal**: Add always-present system menu in upper-left.

**Menu bar layout**:
```
[Ψ][File][Edit][View][Window][Help]  Mon Dec 28 15:04
 ^                    ^
 |                    |
 System menu          Active app's menus
```

**System menu contents** (initial):
- About Desktop
- ---- (separator)
- Desktop Accessories submenu (empty for now)
- ---- (separator)
- Quit (exits entire environment)

**Implementation**:
- MenuBar gains `systemMenu *Menu` field
- System menu always rendered first
- App menus rendered after

### Phase 4: Actions (Limited)

**Goal**: Action objects for menu items and context menus.

```go
type Action struct {
    ID        string
    Text      string
    Shortcut  string       // e.g., "Ctrl+S"
    Enabled   func() bool  // dynamic enable state
    OnTrigger func()
}

// Application registers actions
type ApplicationProvider interface {
    // ...existing...
    Actions() []*Action
    ActionByID(id string) *Action
}
```

**Menu items reference actions**:
```go
menu.AddActionItem(app.ActionByID("file.save"))
// Instead of:
menu.AddItem("Save", "Ctrl+S", func() { ... })
```

**Context menus use same actions**:
```go
contextMenu.AddActionItem(app.ActionByID("edit.copy"))
```

**Explicitly NOT supported** (per requirements):
- Buttons do not use Actions (keep direct OnClick callbacks)
- Toolbars do not use Actions (keep direct handlers)

## File Changes Summary

### Modified Files

1. **trinkets/desktop.go**
   - Add `applications []ApplicationProvider`
   - Add `activeApp ApplicationProvider`
   - Add `systemMenu *Menu`
   - Add `Run()` method (move from app.Application)
   - Add `AddApplication()`, `RemoveApplication()`
   - Update `Paint()` to render system menu
   - Add focus change handling to switch active app

2. **app/application.go**
   - Implement `ApplicationProvider` interface
   - Remove terminal/renderer ownership (moves to Desktop)
   - Keep window management
   - Add menu/status content methods
   - Keep `Run()` for backward compatibility (creates implicit Desktop)

3. **trinkets/menubar.go**
   - Add `systemMenu *Menu` field
   - Update `Paint()` to render system menu first
   - Add method to swap app menu content

4. **New: core/action.go** (or app/action.go)
   - Define `Action` type
   - Action registry per application

### New Files

1. **core/action.go** - Action type definition

## Migration Path

1. Existing single-app code continues to work unchanged
2. New multi-app code uses explicit Desktop creation
3. No breaking API changes in Phase 1-2
4. Actions are additive - existing menu creation still works

## Non-Goals (Explicitly Deferred)

- MDI support (separate effort)
- Actions for buttons/toolbars
- Cross-application clipboard/drag-drop
- Plugin/dynamic loading of applications

## Open Questions

1. Should windows be transferable between applications?
2. How to handle modal dialogs that block all apps vs just owner app?
3. Should system menu be customizable by applications?
