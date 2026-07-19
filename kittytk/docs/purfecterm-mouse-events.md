# PurfecTerm Mouse Event Forwarding

## Overview

Added mouse event forwarding from KittyTK to embedded PurfecTerm CLI instances.

**Initial Commit:** `a7b29ed` - "Add mouse event forwarding to PurfecTerm trinket"
**Drag Fix Commit:** (pending) - "Fix mouse drag events by tracking held button state"
**Date:** 2026-03-10
**Branch:** `claude/document-menu-colors-Dd2pR`

## Bug Fix: Drag Events

The initial implementation used `event.Buttons & core.LeftButton` to detect drag events,
but this doesn't work because:
1. `MouseButton` is defined with `iota` (sequential integers 0,1,2,3...), not bit flags
2. The backend doesn't populate the `Buttons` field in `MouseMoveEvent` for drag events

**Fix:** Added `heldButton` field to `PurfecTerm` struct to track which button is held
between press and release events. `HandleMouseMove` now uses this tracked state instead
of relying on the event's `Buttons` field.

## Changes Made

### File: `trinkets/purfecterm.go`

#### Added import
```go
import (
    "fmt"  // NEW
    // ... existing imports
)
```

#### Replaced `HandleMousePress` (was lines 267-274)

**BEFORE:**
```go
// HandleMousePress handles mouse clicks to focus the terminal.
func (t *PurfecTerm) HandleMousePress(event core.MousePressEvent) bool {
    if event.Button == core.LeftButton {
        t.SetFocus()
        return true
    }
    return false
}
```

**AFTER:**
```go
// HandleMousePress handles mouse clicks to focus the terminal and forward to CLI.
func (t *PurfecTerm) HandleMousePress(event core.MousePressEvent) bool {
    t.SetFocus()
    if t.terminal == nil {
        return true
    }

    // Convert unit coordinates to 1-based cell coordinates for CLI adapter
    metrics := core.DefaultCellMetrics()
    cellX := int(event.X/metrics.CellWidth) + 1
    cellY := int(event.Y/metrics.CellHeight) + 1

    // Send position update first
    t.terminal.HandleKeyString(fmt.Sprintf("Mouse@%d,%d", cellX, cellY))

    // Send button press
    var buttonStr string
    switch event.Button {
    case core.LeftButton:
        buttonStr = "MouseLeftPress"
    case core.MiddleButton:
        buttonStr = "MouseMiddlePress"
    case core.RightButton:
        buttonStr = "MouseRightPress"
    default:
        return true
    }
    t.terminal.HandleKeyString(buttonStr)
    t.Update()
    return true
}
```

#### Added new methods (after HandleMousePress)

```go
// HandleMouseRelease handles mouse button releases.
func (t *PurfecTerm) HandleMouseRelease(event core.MouseReleaseEvent) bool {
    if t.terminal == nil {
        return false
    }

    // Convert unit coordinates to 1-based cell coordinates
    metrics := core.DefaultCellMetrics()
    cellX := int(event.X/metrics.CellWidth) + 1
    cellY := int(event.Y/metrics.CellHeight) + 1

    // Send position update first
    t.terminal.HandleKeyString(fmt.Sprintf("Mouse@%d,%d", cellX, cellY))

    // Send button release
    var buttonStr string
    switch event.Button {
    case core.LeftButton:
        buttonStr = "MouseLeftRelease"
    case core.MiddleButton:
        buttonStr = "MouseMiddleRelease"
    case core.RightButton:
        buttonStr = "MouseRightRelease"
    default:
        return false
    }
    t.terminal.HandleKeyString(buttonStr)
    t.Update()
    return true
}

// HandleMouseMove handles mouse movement/drag events.
func (t *PurfecTerm) HandleMouseMove(event core.MouseMoveEvent) bool {
    if t.terminal == nil {
        return false
    }

    // Convert unit coordinates to 1-based cell coordinates
    metrics := core.DefaultCellMetrics()
    cellX := int(event.X/metrics.CellWidth) + 1
    cellY := int(event.Y/metrics.CellHeight) + 1

    // Check if any button is held for drag events
    if event.Buttons&core.LeftButton != 0 {
        t.terminal.HandleKeyString(fmt.Sprintf("MouseLeftDrag@%d,%d", cellX, cellY))
        t.Update()
        return true
    }
    if event.Buttons&core.RightButton != 0 {
        t.terminal.HandleKeyString(fmt.Sprintf("MouseRightDrag@%d,%d", cellX, cellY))
        t.Update()
        return true
    }

    // Plain movement (for mouse tracking modes)
    t.terminal.HandleKeyString(fmt.Sprintf("Mouse@%d,%d", cellX, cellY))
    t.Update()
    return true
}

// HandleMouseWheel handles scroll wheel events.
func (t *PurfecTerm) HandleMouseWheel(event core.MouseWheelEvent) bool {
    if t.terminal == nil {
        return false
    }

    // Convert unit coordinates to 1-based cell coordinates
    metrics := core.DefaultCellMetrics()
    cellX := int(event.X/metrics.CellWidth) + 1
    cellY := int(event.Y/metrics.CellHeight) + 1

    // Send position update first
    t.terminal.HandleKeyString(fmt.Sprintf("Mouse@%d,%d", cellX, cellY))

    // Send scroll event based on direction
    if event.DeltaY < 0 {
        t.terminal.HandleKeyString("MouseScrollUp")
    } else if event.DeltaY > 0 {
        t.terminal.HandleKeyString("MouseScrollDown")
    }
    t.Update()
    return true
}
```

## How to Revert

To restore the original behavior (mouse only sets focus, no forwarding):

### Option 1: Git revert
```bash
git revert a7b29ed
```

### Option 2: Manual revert

1. Remove `"fmt"` from imports in `trinkets/purfecterm.go`

2. Replace the entire `HandleMousePress` function with:
```go
// HandleMousePress handles mouse clicks to focus the terminal.
func (t *PurfecTerm) HandleMousePress(event core.MousePressEvent) bool {
    if event.Button == core.LeftButton {
        t.SetFocus()
        return true
    }
    return false
}
```

3. Delete these functions entirely:
   - `HandleMouseRelease`
   - `HandleMouseMove`
   - `HandleMouseWheel`

## Technical Notes

- Coordinates are converted from KittyTK units to 1-based cell coordinates
- The CLI adapter expects: `Mouse@x,y` position update sent before press/release/scroll events
- Drag events include position in the event string: `MouseLeftDrag@x,y`
- The CLI adapter handles mouse mode detection (1000/1002/1003) internally

## Potential Issues

If coloring or rendering breaks after this change, it's likely unrelated to the mouse events themselves, but could indicate:
- The `t.Update()` calls triggering unexpected repaints
- Some interaction with focus handling (`t.SetFocus()` now called for all buttons, not just left)
