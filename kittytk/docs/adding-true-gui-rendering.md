# Adding True GUI Rendering to KittyTK

## Overview

This document analyzes the architectural challenges and solutions for adding native GUI rendering backends (macOS, Windows, Web) to KittyTK, allowing the same application code to compile against either TUI or native GUI targets.

## The Core Challenge: Two Rendering Paradigms

```
┌─────────────────────────────────────────────────────────────┐
│                     Application Code                         │
│   Button, TextInput, ListView, Window, Layout...            │
└─────────────────────────────────────────────────────────────┘
                              │
                    ┌─────────┴─────────┐
                    ▼                   ▼
          ┌─────────────────┐   ┌─────────────────┐
          │   TUI Backend   │   │   GUI Backend   │
          │                 │   │                 │
          │ Cell grid       │   │ Native windows  │
          │ Box-drawing     │   │ Native controls │
          │ ANSI colors     │   │ System fonts    │
          │ Terminal I/O    │   │ OS event loop   │
          └─────────────────┘   └─────────────────┘
```

## Current Architecture Strengths

The existing architecture is already well-prepared for multi-backend support:

### 1. Abstract Unit System ✅
```go
type Unit int  // Not "cell" or "pixel" - abstract!
```
`CellMetrics` already handles the translation. A GUI backend would use different metrics (e.g., 1:1 for pixels, or scaled for HiDPI).

### 2. Clean RenderBackend Interface ✅
The interface is backend-agnostic. A native backend would implement the same methods.

### 3. Transform/Clipping System ✅
The `Painter` with transforms and clipping works for any rendering target.

### 4. True Color Support ✅
`RGB(r, g, b)` already stores 24-bit color. GUI backends would ignore ANSI codes and use the raw RGB values.

---

## Key Architectural Decisions

### Decision 1: Where Does Trinket Rendering Live?

**Current (TUI-embedded):**
```go
func (b *Button) Paint(p *core.Painter) {
    // Button knows HOW to draw itself with cells
    p.DrawText(x, y, "[", style)
    p.DrawText(x+1, y, b.text, style)
    p.DrawText(x+len(b.text)+1, y, "]", style)
}
```

**Native GUI Approach - Option A: Backend Provides Trinket Renderers**
```go
func (b *Button) Paint(p *core.Painter) {
    // Button describes WHAT it is, backend draws it
    p.Backend().DrawButton(b.Bounds(), b.text, b.state)
}

// TUI backend:
func (t *TUIBackend) DrawButton(bounds, text, state) {
    t.DrawText("[" + text + "]")  // Cell-based
}

// macOS backend:
func (m *MacBackend) DrawButton(bounds, text, state) {
    // Create/update NSButton
}
```

**Native GUI Approach - Option B: Trinkets Are Backend-Specific**
```go
// Core defines interface
type Button interface {
    Trinket
    SetText(string)
    OnClick(func())
}

// TUI implementation
type tuiButton struct { ... }  // Draws with cells

// macOS implementation
type macButton struct {
    nsButton *cocoa.NSButton  // Wraps native control
}
```

**Recommendation:** Option A is more practical - keep trinket logic unified, delegate rendering.

---

### Decision 2: Layout - Cells vs Pixels

**The Problem:**
```go
// Current: Trinket sizes in "units" that map to cells
button.SetSize(core.UnitSize{Width: 80, Height: 16})  // 10 chars × 1 row
```

In native GUI:
- Buttons have intrinsic sizes based on font + padding
- Text wraps based on pixel width
- HiDPI scaling affects everything

**Solution: Abstract Size Hints**

```go
type SizePolicy int
const (
    SizeFixed      SizePolicy = iota  // Exact size
    SizeMinimum                        // At least this big
    SizePreferred                      // Ideally this big
    SizeExpanding                      // Take available space
)

type Trinket interface {
    // Returns preferred size - backend interprets units differently
    SizeHint() UnitSize

    // How should this trinket grow/shrink?
    SizePolicy() (horizontal, vertical SizePolicy)

    // Minimum usable size
    MinimumSize() UnitSize
}
```

TUI backend: Units = cells
GUI backend: Units = pixels (or DIPs for HiDPI)

**The layout managers would work the same way** - they just get different unit values from the backend's metrics.

---

### Decision 3: The Window Abstraction

**Current:**
```go
// Your Window is a trinket that draws a frame
type Window struct {
    core.TrinketBase
    title string
    content Trinket
    // ... draws title bar, borders, handles resize
}
```

**Native GUI:**
Windows ARE the native window. You don't draw the chrome - the OS does.

**Solution: Split Window Concept**

```go
// Platform window (OS-level)
type PlatformWindow interface {
    SetTitle(string)
    SetSize(w, h int)
    SetPosition(x, y int)
    Show()
    Hide()
    Close()

    // Content area where trinkets render
    ContentBounds() UnitRect
}

// Your current Window becomes "InternalWindow" for MDI
type InternalWindow struct {
    core.TrinketBase
    // For TUI: draws frame, title bar, etc.
    // For GUI MDI: could be a panel with decorations
}
```

**For TUI:** Desktop creates one PlatformWindow (the terminal), renders everything including window chrome.

**For GUI:** Each top-level Window maps to a native OS window. MDI children render inside.

---

### Decision 4: Event Routing

**Current:**
```go
// Desktop receives all events, routes to windows/trinkets
func (d *Desktop) HandleEvent(e core.Event) {
    // Route to active window, then to focused trinket
}
```

**Native GUI:**
Each native window receives its own events from the OS.

**Solution: Event Bridge**

```go
type EventBridge interface {
    // Called by backend when events arrive
    DeliverEvent(target Trinket, event Event)
}

// TUI: Single event stream, Desktop routes
// GUI: Each native window delivers to its content tree
```

---

### Decision 5: The Painter Abstraction

**Current Painter is TUI-centric:**
```go
DrawCell(x, y, ch rune, style)      // Character-specific
DrawText(x, y, text, style)          // Assumes monospace
DrawRect(r, border BorderStyle, s)   // Uses box-drawing chars
```

**Native GUI Painter:**
```go
type Painter interface {
    // Primitive operations (both backends implement)
    FillRect(r UnitRect, color Color)
    DrawLine(x1, y1, x2, y2 Unit, color Color, width Unit)

    // Text (backends handle fonts differently)
    DrawText(x, y Unit, text string, font Font, color Color)
    MeasureText(text string, font Font) UnitSize

    // High-level (backends render appropriately)
    DrawBorder(r UnitRect, style BorderStyle, color Color)

    // Trinket-specific (backends provide native look)
    DrawButtonFrame(r UnitRect, state ButtonState)
    DrawCheckMark(r UnitRect, checked bool)
    DrawScrollbarTrack(r UnitRect, orientation Orientation)
    DrawScrollbarThumb(r UnitRect, orientation Orientation)
}
```

**TUI Painter:** Implements high-level methods with cells
**GUI Painter:** Implements with native drawing or actual native controls

---

## The Deeper Challenges

### 🔴 Challenge: Text Measurement

**TUI:** `len("Hello")` = 5 cells. Done.

**GUI:** "Hello" width depends on:
- Font family
- Font size
- Font weight
- Kerning
- The specific glyphs

**This affects EVERYTHING:**
- Button width calculation
- Text truncation with "..."
- Word wrapping
- Layout decisions

**Solution:**
```go
type TextMeasurer interface {
    MeasureText(text string, style TextStyle) UnitSize
    MeasureRune(r rune, style TextStyle) Unit
}

// Trinkets MUST use this, not assume widths
func (label *Label) SizeHint() UnitSize {
    measurer := label.GetTextMeasurer()  // From backend
    return measurer.MeasureText(label.text, label.style)
}
```

---

### 🔴 Challenge: Focus & Keyboard Navigation

**TUI:** Tab moves between trinkets. Clear focus ring (highlighting).

**GUI:**
- Native focus behavior varies by platform
- macOS: "Full Keyboard Access" setting
- Some controls don't accept keyboard focus natively
- Focus rings rendered by OS

**Solution:** Your focus manager stays, but GUI backend may need to sync with native focus system.

---

### 🔴 Challenge: Native Controls vs Custom Drawing

**Three approaches for GUI backend:**

1. **Full Custom Drawing** (like TUI, but with pixels)
   - Draw everything yourself
   - Consistent cross-platform look
   - Lose native feel

2. **Native Control Wrapping** (like wxTrinkets)
   - Map each trinket to native control
   - True native look
   - Behavioral differences between platforms
   - Some trinkets have no native equivalent

3. **Hybrid** (like Qt)
   - Custom drawing styled to match native
   - QStyle system for platform-specific appearance
   - Best of both worlds, most work

**Recommendation for KittyTK:** Start with #1 (custom drawing), style it nicely. Native wrapping is enormous effort.

---

### 🟡 Challenge: Menus

**TUI:** Menu is a trinket you draw.

**macOS:** Menu bar belongs to the system. `NSMenu` API required.

**Windows:** Menu bar is part of window, but still native `HMENU`.

**Solution:**
```go
// Menu definition stays the same
menu := trinkets.NewMenu("&File")
menu.AddItem(...)

// Backend decides HOW to render
type MenuBackend interface {
    CreateMenu(menu *Menu) PlatformMenu
    ShowContextMenu(menu *Menu, x, y int)
}

// TUI: Renders as popup trinket
// macOS: Creates NSMenu, attaches to NSApplication
// Windows: Creates HMENU, attaches to HWND
```

---

### 🟡 Challenge: Dialogs

**TUI:** Dialogs are windows you draw.

**GUI:** File dialogs, message boxes, color pickers are native.

**Solution:**
```go
type DialogBackend interface {
    ShowMessageBox(title, message string, buttons []string) int
    ShowFileOpenDialog(filters []FileFilter) (string, error)
    ShowFileSaveDialog(filters []FileFilter, defaultName string) (string, error)
}

// Fallback for TUI: Use your current dialog trinkets
// GUI: Call native APIs
```

---

## Proposed Architecture

```
┌────────────────────────────────────────────────────────────────┐
│                        Application                              │
│  - Creates trinkets (Button, Label, etc.)                       │
│  - Defines layout                                               │
│  - Handles business logic                                       │
└────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌────────────────────────────────────────────────────────────────┐
│                     Trinket Layer (shared)                       │
│  - Trinket interfaces and base implementations                   │
│  - Layout managers                                              │
│  - Focus management                                             │
│  - Event handling logic                                         │
│  - NO RENDERING CODE - just state & behavior                   │
└────────────────────────────────────────────────────────────────┘
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
┌──────────────────┐ ┌──────────────────┐ ┌──────────────────┐
│   TUI Renderer   │ │  macOS Renderer  │ │ Windows Renderer │
│                  │ │                  │ │                  │
│ - Cell-based     │ │ - NSView/CALayer │ │ - HWND/Direct2D  │
│ - Box drawing    │ │ - Native menus   │ │ - Native menus   │
│ - ANSI output    │ │ - NSFont         │ │ - GDI+/DirectWrite│
│ - Terminal I/O   │ │ - Cocoa events   │ │ - Win32 messages │
└──────────────────┘ └──────────────────┘ └──────────────────┘
```

---

## What Needs to Change

| Component | Current State | Needs For GUI |
|-----------|--------------|---------------|
| `Trinket.Paint()` | Draws directly | Delegate to renderer |
| `Painter` | Cell operations | Abstract + trinket ops |
| Size calculation | Assumes cells | Query text measurer |
| `Window` | Draws chrome | Split platform/internal |
| `Desktop` | Single surface | Platform window manager |
| `Menu` | Trinket-based | Platform menu support |
| Event loop | Poll terminal | Platform event bridge |
| Colors | ANSI codes | RGB extraction |

---

## Incremental Path Forward

1. **Phase 1:** Extract rendering from trinkets into `TrinketRenderer` interface
2. **Phase 2:** Add `TextMeasurer` abstraction, trinkets use it
3. **Phase 3:** Split `Window` into `PlatformWindow` + `InternalWindow`
4. **Phase 4:** Create SDL/OpenGL backend (custom drawing, cross-platform)
5. **Phase 5:** Create native macOS backend (Cocoa)
6. **Phase 6:** Create native Windows backend (Win32/Direct2D)
7. **Phase 7:** Create web backend (Canvas/WebGL via WebAssembly)

**Phase 4 (SDL) is the best proving ground** - it forces all the abstractions without native complexity.

---

## Difficulty Assessment

| Aspect | Difficulty | Current State |
|--------|-----------|---------------|
| Coordinate system | 🟢 Easy | Already abstracted |
| Event handling | 🟢 Easy | Already abstracted |
| Colors | 🟢 Easy | Just add RGB extraction |
| Clipping/transforms | 🟢 Easy | Already abstracted |
| Text rendering | 🟡 Medium | Need font metrics |
| Box drawing | 🟡 Medium | Need vector alternative |
| Trinket visuals | 🟡 Medium | Need paint delegates |
| Native integration | 🔴 Hard | Platform-specific work |

---

## Summary

The KittyTK architecture is well-designed for multi-backend support. A basic GUI backend (custom drawing with consistent style) could be built with **moderate effort**. Full native look-and-feel would require significant additional work but is achievable.

The key insight is that the cell-based renderer is a TUI-only aspect. The GUI version would transform the same layout information and trinkets into appropriate native OS windows with native content rendering.
