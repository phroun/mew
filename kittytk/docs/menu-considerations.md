# Menu Bar and MDI Architecture Considerations

This document captures design discussions and considerations for implementing context-sensitive menu bars and MDI (Multiple Document Interface) support in KittyTK.

## Implementation Status

| Feature | Status | Notes |
|---------|--------|-------|
| Context-sensitive menus | ✅ Done | Via ApplicationProvider.MenuBarContent() |
| Context-sensitive status bar | ✅ Done | Via ApplicationProvider.StatusBarContent() |
| Desktop composition | ✅ Done | MenuBar + WindowManager + DockRow + StatusBar |
| Standard app menu items | ✅ Done | Auto-injected Hide/Show All/Quit |
| Action type | ⚠️ Partial | Exists but no registry pattern |
| MDIPane trinket | 🔴 Planned | See below |
| Responder chain | 🔴 Deferred | Current menu-swap approach sufficient |
| Menu merging | 🔴 Deferred | Low priority |
| Menu as model | 🔴 Deferred | Medium priority for future |

---

## Overview

The goal is to support Mac-like context-sensitive menus where the menu bar content can change based on which window or document is active, while also providing flexible MDI support that isn't tightly coupled to a specific container type.

---

## MDIPane Trinket

### Status: 🔴 NOT YET IMPLEMENTED

### Motivation

The Window class should remain focused on being a window (frame, title bar, buttons). Container semantics for managing floating child windows should live in a separate trinket.

Currently, window management is embedded in `Desktop` and `WindowManager`. There's no standalone MDI container that can be embedded in other trinkets.

### Proposed Design

An `MDIPane` trinket that:

1. **Is a regular trinket** - can be placed anywhere (in a Window, TabTrinket tab, Panel, Splitter, etc.)

2. **Has background content** - accepts child trinkets with layout managers for the area behind floating windows

3. **Manages floating windows** - maintains a z-ordered list of Window children that float above the content

4. **Handles input routing**:
   - Hit-test floating windows first (front to back)
   - If no window hit, route to background content
   - Keyboard goes to focused MDI child or falls through to content

5. **Provides the MDI API**:
   - `AddWindow(w *Window)`, `RemoveWindow(w *Window)`
   - `ActiveWindow()`, `SetActiveWindow(w *Window)`
   - `NextWindow()`, `PrevWindow()`
   - `Windows() []*Window` for external UI to query
   - `TileWindows()`, `CascadeWindows()`
   - Callbacks: `OnWindowAdded`, `OnWindowRemoved`, `OnActiveWindowChanged`

6. **Doesn't prescribe UI** - external components (menus, docks, sidebars) use the API

### Usage Patterns

```go
// Classic MDI - pane fills a window
mainWindow.SetContent(mdiPane)

// MDI in a tab
tabTrinket.AddTab("Documents", mdiPane)

// Split view - MDI pane on left, tools on right
splitter.SetFirst(mdiPane)
splitter.SetSecond(toolPanel)

// MDI with built-in toolbar above
panel.AddChild(toolbar)
panel.AddChild(mdiPane) // fills remaining space
```

### Relationship to Desktop

Desktop uses MDIPane-like functionality internally via WindowManager. The MDIPane trinket would:
- Share core logic with WindowManager where possible
- Be usable independently of Desktop
- Enable nested MDI scenarios

---

## Context-Sensitive Menu Bars

### Status: ✅ IMPLEMENTED

### What We Built

Rather than the responder chain pattern (Cocoa-style), we implemented explicit menu swapping:

1. **ApplicationProvider interface** - apps implement `MenuBarContent()` to provide menus
2. **Desktop.windowFocusChanged()** - detects when focus moves to a different app's window
3. **Desktop.updateMenuBarContent()** - clears menu bar and rebuilds from active app's menus
4. **Standard app menu items** - Desktop auto-injects Hide, Hide Others, Show All, Quit into first app menu

### Why This Approach

- Simpler than responder chain
- Clear ownership - each app defines its complete menus
- Easy to understand and debug
- Sufficient for multi-application scenarios

### Original Consideration: Responder Chain

The document originally considered Cocoa-style responder chain where the *same* menu items route to *different handlers* based on focus. This remains a valid alternative if we need:
- Standard commands (Save, Undo) handled by many different trinkets
- More dynamic enable/disable based on focused trinket
- Less menu definition duplication

For now, the menu-swapping approach serves our needs well.

---

## StatusBar Considerations

### Status: ✅ IMPLEMENTED

StatusBar follows the same pattern as MenuBar:

- `ApplicationProvider.StatusBarContent()` - apps provide status sections
- `Desktop.updateStatusBarContent()` - swaps content based on active app
- Focus-driven updates when active application changes

---

## Actions System

### Status: ⚠️ PARTIALLY IMPLEMENTED

### What Exists

In `core/action.go`:
```go
type Action struct {
    Text        string
    Shortcut    Shortcut
    Enabled     bool
    Checkable   bool
    Checked     bool
    OnTriggered func()
}
```

`Menu.AddAction(action *Action)` method exists.

### What's Missing

1. **No ID field** - can't look up actions by identifier
2. **Static Enabled** - should be `func() bool` for dynamic enable/disable
3. **No registry** - no central place to define and look up actions
4. **Not widely used** - most code uses direct menu item creation

### Future Enhancement

Consider evolving to:
```go
type Action struct {
    ID          string
    Text        string
    Shortcut    Shortcut
    Icon        *style.TextIcon
    Enabled     func() bool  // Dynamic!
    Checkable   bool
    Checked     func() bool  // Dynamic!
    OnTriggered func()
}

// Per-application registry
app.RegisterAction(action)
action := app.Action("file.save")
```

---

## Prior Art

### Qt
- Separates `QMenuBar` (display) from `QAction` (commands)
- Actions are abstract - same action appears in menu, toolbar, context menu, shortcut
- `QMdiArea` is an explicit container trinket for MDI children
- On macOS, can automatically move menu bar to system location

### GTK (3/4)
- `GAction` + `GMenu` pattern - menus are declarative data, not trinkets
- `GMenu` is a model, `GtkMenuBar` renders it
- Actions live on `GtkApplication` or `GtkApplicationWindow`

### Cocoa (macOS native)
- **Responder chain**: Menu actions walk up from focused view to find a handler
- `validateMenuItem:` - objects enable/disable menu items dynamically
- Menu bar is app-global, but *which object handles each action* depends on focus

---

## Future Considerations

### Menu as Model (Deferred)

Separating menu definition (model/data) from display (trinket) would enable:
- Same menu data for menu bar and context menu
- Declarative menu definitions
- Easier serialization

```go
type MenuModel struct {
    Items []MenuItemModel
}
menuBar.SetModel(model)
contextMenu.SetModel(model)
```

### Menu Merging (Deferred)

Allowing providers to merge menus (base app menus + document-specific additions) would support:
- Plugin systems adding menu items
- Document types adding specialized menus

---

## Recommendations

### Completed Phases

1. ~~**Phase 1: MDIPane Foundation**~~ - Partially done via WindowManager
2. ~~**Phase 2: Desktop Refactoring**~~ - Done, Desktop uses composition
3. ~~**Phase 3: Action System**~~ - Partial, basic Action type exists
4. ~~**Phase 4: Context-Sensitive Menus**~~ - Done via menu swapping

### Next Steps

1. **MDIPane Trinket** - Extract reusable MDI container from Desktop/WindowManager
2. **Dynamic Action.Enabled** - Convert to `func() bool` for context-sensitive enable/disable
3. **Action Registry** - Add ID field and per-app lookup

---

## Notes

- The menu-swapping approach proved simpler and sufficient vs responder chain
- MDIPane remains valuable for embedding MDI in tabs, splitters, etc.
- Keep the API flexible enough to support different application patterns
