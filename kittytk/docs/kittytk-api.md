# KittyTK API Reference

Complete API for building KittyTK applications.

## Package Structure

```
KittyTK/
  app/       - Application lifecycle
  backend/   - Terminal rendering
  core/      - Fundamental types, trinket interface, events
  layout/    - Layout managers
  style/     - Colors, themes, schemes
  trinkets/   - UI components
  window/    - Window management
```

## Quick Start

```go
package main

import (
    "github.com/phroun/KittyTK/app"
    "github.com/phroun/KittyTK/backend"
    "github.com/phroun/KittyTK/trinkets"
    "github.com/phroun/KittyTK/window"
)

func main() {
    desktop := trinkets.NewDesktop()
    desktop.SetBackend(backend.NewTUIBackend(backend.DefaultTUIOptions()))

    application := app.New(nil)
    application.SetName("My App")
    desktop.AddApplication(application)

    desktop.SetOnStartup(func() {
        w := window.NewWindow("Hello")
        w.SetContent(trinkets.NewLabel("Hello, World!"))
        application.AddWindow(w)
    })

    desktop.Run()
}
```

---

## Core Types

### Geometry (core/types.go)

```go
// Cell-based coordinates
type Point struct { X, Y int }
type Size struct { Width, Height int }
type Rect struct { X, Y, Width, Height int }
type Margins struct { Top, Right, Bottom, Left int }

// Abstract units (resolution-independent)
type Unit int
type UnitPoint struct { X, Y Unit }
type UnitSize struct { Width, Height Unit }
type UnitRect struct { X, Y, Width, Height Unit }
type UnitMargins struct { Top, Right, Bottom, Left Unit }
```

### CellMetrics

Converts between abstract units and character cells.

```go
metrics := core.DefaultCellMetrics()  // 8x16 units per cell
col, row := metrics.UnitsToCell(unitX, unitY)
```

### Enums

```go
// Alignment
AlignFill, AlignLeft, AlignCenter, AlignRight
AlignTop, AlignMiddle, AlignBottom

// Orientation
Horizontal, Vertical

// SizePolicy
SizeFixed, SizeMinimum, SizeMaximum, SizePreferred, SizeExpanding, SizeIgnored

// FocusPolicy
NoFocus, TabFocus, ClickFocus, StrongFocus, WheelFocus

// WindowState
WindowNormal, WindowStateMaximized, WindowStateMinimized

// MouseButton
NoButton, LeftButton, MiddleButton, RightButton, ScrollUp, ScrollDown
```

---

## Trinket Interface

### core.Trinket

Base interface for all UI elements.

```go
type Trinket interface {
    // Identity
    Name() string
    SetName(string)
    Parent() Trinket
    SetParent(Trinket)

    // Geometry
    Bounds() UnitRect
    SetBounds(UnitRect)
    Size() UnitSize
    SetSize(UnitSize)
    MinimumSize() UnitSize
    SetMinimumSize(UnitSize)
    MaximumSize() UnitSize
    SetMaximumSize(UnitSize)
    SizeHint() UnitSize
    SizePolicy() SizePolicyPair
    SetSizePolicy(SizePolicyPair)

    // State
    IsVisible() bool
    SetVisible(bool)
    Show()
    Hide()
    IsEnabled() bool
    SetEnabled(bool)

    // Focus
    FocusPolicy() FocusPolicy
    SetFocusPolicy(FocusPolicy)
    HasFocus() bool
    SetFocus()
    ClearFocus()

    // Styling
    Style() *style.CellStyle
    SetStyle(*style.CellStyle)
    Scheme() style.SchemeID
    SetScheme(style.SchemeID)

    // Rendering
    Paint(p *Painter)
    Update()
    NeedsRepaint() bool

    // Events (return true to consume)
    HandleKeyPress(KeyPressEvent) bool
    HandleMousePress(MousePressEvent) bool
    HandleMouseRelease(MouseReleaseEvent) bool
    HandleMouseMove(MouseMoveEvent) bool
    HandleMouseWheel(MouseWheelEvent) bool
    HandleFocusIn(FocusEvent) bool
    HandleFocusOut(FocusEvent) bool
}
```

### core.Container

Extends Trinket to hold children.

```go
type Container interface {
    Trinket
    Children() []Trinket
    AddChild(Trinket)
    RemoveChild(Trinket)
    ChildAt(UnitPoint) Trinket
    LayoutManager() LayoutManager
    SetLayoutManager(LayoutManager)
}
```

### TrinketBase

Embed in custom trinkets:

```go
type MyTrinket struct {
    core.TrinketBase
    // custom fields
}

func NewMyTrinket() *MyTrinket {
    w := &MyTrinket{}
    w.TrinketBase = *core.NewTrinketBase()
    w.Init(w)  // Pass outer reference
    return w
}
```

Key TrinketBase methods:
```go
BackgroundColor() *style.Color
SetBackgroundColor(*style.Color)
Font() *Font
SetFont(*Font)
EffectiveFont() *Font
```

---

## Events

```go
type KeyPressEvent struct {
    Key       string      // "a", "Enter", "^C", "Alt+F"
    Modifiers KeyModifiers
    Text      string      // Printable character
}

type MousePressEvent struct {
    X, Y   Unit
    Button MouseButton
}

type MouseMoveEvent struct {
    X, Y Unit
}

type MouseWheelEvent struct {
    X, Y      Unit
    Direction int  // positive=up, negative=down
}
```

---

## Style System

### Colors (style/style.go)

```go
// Standard 16 colors
ColorBlack, ColorRed, ColorGreen, ColorYellow
ColorBlue, ColorMagenta, ColorCyan, ColorWhite
ColorBrightBlack ... ColorBrightWhite
ColorDefault  // Terminal default

// 256-color palette
style.Color256(index)  // 0-255

// True color (24-bit)
style.RGB(r, g, b)  // 0-255 each
```

### CellStyle

```go
s := style.DefaultStyle()
s = s.WithFg(style.ColorRed)
s = s.WithBg(style.ColorBlack)
s = s.Bold()
s = s.Underline()
s = s.Reverse()
```

### TextStyle Attributes

```go
StyleNormal, StyleBold, StyleDim, StyleItalic
StyleUnderline, StyleBlink, StyleReverse
StyleStrikethrough, StyleOverline
```

### BorderStyle

```go
BorderNone, BorderSingle, BorderDouble
BorderRounded, BorderHeavy, BorderASCII
```

### Theme

Complete color definitions for all UI elements.

```go
theme := style.DefaultTheme()  // or DarkTheme(), ClassicTheme()
```

### Scheme

Color variants within a theme (default, modal, etc.)

```go
trinket.SetScheme(style.SchemeDefault)
trinket.SetScheme(style.SchemeModal)
```

---

## Fonts

```go
type Font struct {
    Name       string     // "Monday", "Tuesday"
    Style      FontStyle
    Size       int
    Foreground FontColor
    Background FontColor
}

// Predefined
core.FontMonday12   // Standard fixed-width (8 units/char)
core.FontTuesday12  // Double-width (16 units/char)

trinket.SetFont(core.FontTuesday12)
```

---

## Trinkets

### Label

```go
label := trinkets.NewLabel("Hello")
label.SetText("Updated")
label.SetAlignment(core.AlignCenter)
label.SetWordWrap(true)
```

### Button

```go
btn := trinkets.NewButton("Click Me")
btn.SetOnClick(func() { /* handler */ })

// Icon button
iconBtn := trinkets.NewIconButton(&style.Icon{...})

// Checkable button
btn.SetCheckable(true)
btn.SetOnToggled(func(checked bool) { })
```

### Checkbox

```go
cb := trinkets.NewCheckbox("Enable feature")
cb.SetChecked(true)
cb.SetOnToggled(func(checked bool) { })

// Tri-state
cb.SetTriState(true)
cb.SetCheckState(trinkets.StatePartial)
```

### RadioButton

```go
group := trinkets.NewRadioGroup()
r1 := trinkets.NewRadioButton("Option A")
r2 := trinkets.NewRadioButton("Option B")
group.AddButton(r1)
group.AddButton(r2)
r1.SetOnToggled(func(checked bool) { })
```

### TextInput

```go
input := trinkets.NewTextInput()
input.SetPlaceholder("Enter name...")
input.SetText("default")
input.SetMaxLength(50)
input.SetEchoMode(trinkets.EchoPassword)
input.SetReadOnly(true)
input.SetOnTextChanged(func(text string) { })
input.SetOnReturnPressed(func() { })
```

### ComboBox

```go
combo := trinkets.NewComboBox()
combo.AddItem("First")
combo.AddItem("Second")
combo.SetCurrentIndex(0)
combo.SetEditable(true)
combo.SetOnCurrentChanged(func(index int) { })
```

### ListView

```go
list := trinkets.NewListView()
list.AddItem(trinkets.NewListItem("Item 1"))
list.AddTextItem("Item 2")
list.SetSelectionMode(trinkets.MultiSelection)
list.SetOnItemActivated(func(index int) { })
list.SetOnSelectionChanged(func() { })

// Access items
item := list.Item(0)
selected := list.SelectedIndices()
```

### TreeView

```go
tree := trinkets.NewTreeView()

root := trinkets.NewTreeItem("Documents")
child := trinkets.NewTreeItem("File.txt")
root.AddChild(child)
root.Expanded = true

tree.AddRootItem(root)
tree.SetOnItemActivated(func(item *TreeItem) { })
```

### TabTrinket

```go
tabs := trinkets.NewTabTrinket()
tabs.AddTab("General", generalPanel)
tabs.AddTab("Advanced", advancedPanel)
tabs.SetCurrentIndex(0)
tabs.SetTabPosition(trinkets.TabsTop)  // TabsBottom, TabsLeft, TabsRight
tabs.SetOnCurrentChanged(func(index int) { })
```

### ScrollArea

```go
scroll := trinkets.NewScrollArea()
scroll.SetContent(largePanel)
scroll.SetVerticalScrollBarPolicy(trinkets.ScrollBarAsNeeded)
scroll.SetHorizontalScrollBarPolicy(trinkets.ScrollBarAlwaysOff)
```

### Panel

```go
panel := trinkets.NewPanel()
panel.AddChild(label)
panel.AddChild(button)
panel.SetLayoutManager(layout.NewBoxLayout(core.Vertical))
panel.SetTitle("Settings")
panel.SetBorder(style.BorderSingle)
```

### ProgressBar

```go
progress := trinkets.NewProgressBar()
progress.SetMaximum(100)
progress.SetValue(50)
progress.SetIndeterminate(true)  // Animated unknown progress
```

### Splitters

```go
// Vertical splitter (top/bottom)
vsplit := trinkets.NewVSplitter()
vsplit.SetFirst(topPanel)
vsplit.SetSecond(bottomPanel)
vsplit.SetPosition(0.3)  // 30% top

// Horizontal splitter (left/right)
hsplit := trinkets.NewHSplitter()
hsplit.SetFirst(leftPanel)
hsplit.SetSecond(rightPanel)
```

### Separator

```go
sep := trinkets.NewLineSeparator(core.Horizontal)
sep.SetTitle("Section")  // Optional divider title
```

### Spacer

```go
spacer := trinkets.NewSpacer()         // Expanding
fixed := trinkets.NewFixedSpacer(16)   // Fixed size
```

### Menu System

```go
menu := trinkets.NewMenu("&File")  // & marks accelerator

item := trinkets.NewMenuItem("&Open...")
item.SetShortcut(core.NewShortcut("^O"))
item.SetOnTriggered(func() { })

checkItem := trinkets.NewMenuItem("Show Toolbar")
checkItem.SetCheckable(true)
checkItem.SetChecked(true)

menu.AddItem(item)
menu.AddSeparator()
menu.AddItem(checkItem)
```

### Dialog / MessageBox

```go
dialog := trinkets.NewMessageBox(
    "Confirm",
    "Are you sure?",
    trinkets.ButtonYes | trinkets.ButtonNo,
)
dialog.SetIcon(trinkets.IconQuestion)
dialog.SetOnFinished(func(result trinkets.DialogResult) {
    if result == trinkets.ResultYes { /* ... */ }
})
application.AddWindow(&dialog.Window)
```

### PurfecTerm

Terminal emulator trinket.

```go
term := trinkets.NewPurfecTerm()
term.Start()  // Run default shell

// Or run specific command
term.StartCommand("vim", "file.txt")

// Debug callback
term.SetOnCellClicked(func(info trinkets.CellDebugInfo) {
    // info.Col, info.Row, info.Char
    // info.FgType, info.BgType ("RGB", "256", "Std", "Def")
    // info.Bold, info.Underline, info.Reverse
})
```

---

## Layout System

### BoxLayout

Linear arrangement (horizontal or vertical).

```go
box := layout.NewBoxLayout(core.Vertical)
box.SetSpacing(8)
box.SetContentsMargins(core.UnitMargins{Top: 4, Right: 4, Bottom: 4, Left: 4})

panel.SetLayoutManager(box)
panel.AddChild(label)
panel.AddChild(button)
```

Shortcuts:
```go
layout.NewHBoxLayout()  // Horizontal
layout.NewVBoxLayout()  // Vertical
```

### FlexLayout

CSS Flexbox-style layout.

```go
flex := layout.NewFlexLayout()
flex.SetDirection(layout.FlexRow)  // FlexColumn, FlexRowReverse, FlexColumnReverse
flex.SetWrap(layout.FlexWrapNormal)
flex.SetJustifyContent(layout.FlexJustifySpaceBetween)
flex.SetAlignItems(layout.FlexAlignCenter)
```

### GridLayout

2D grid arrangement.

```go
grid := layout.NewGridLayout(3, 2)  // 3 columns, 2 rows
grid.SetColumnStretch(0, 1)         // Column 0 stretches
grid.SetRowStretch(1, 2)            // Row 1 stretches more
```

### LayoutManager Interface

```go
type LayoutManager interface {
    Layout(container Container, bounds UnitRect)
    SizeHint(container Container) UnitSize
    MinimumSize(container Container) UnitSize
    Spacing() int
    SetSpacing(int)
    ContentsMargins() UnitMargins
    SetContentsMargins(UnitMargins)
}
```

---

## Window System

### Window

```go
w := window.NewWindow("My Window")
w.SetSize(core.UnitSize{Width: 480, Height: 320})  // 60x20 cells
w.SetContent(panel)

// State
w.Maximize()
w.Minimize()
w.Restore()
w.Close()

// Flags
w.SetFlags(core.WindowNoResize | core.WindowModal)

// Position
w.SetBounds(core.UnitRect{X: 80, Y: 32, Width: 480, Height: 320})
w.Move(core.UnitPoint{X: 100, Y: 50})
```

### WindowManager

```go
wm := desktop.WindowManager()
wm.AddWindow(w)
wm.SetActiveWindow(w)
wm.TileWindows()
wm.CascadeWindows()

// Callbacks
wm.SetOnWindowAdded(func(w *window.Window) { })
wm.SetOnActiveWindowChanged(func(w *window.Window) { })
```

### MDIPane

Multi-document interface container (embeddable trinket).

```go
mdi := trinkets.NewMDIPane()
mdi.AddWindow(childWindow)
mdi.SetActiveWindow(childWindow)
mdi.TileWindows()
mdi.CascadeWindows()
mdi.NextWindow()
mdi.PrevWindow()

// Callbacks
mdi.SetOnWindowMinimized(func(w *window.Window) { })
mdi.SetOnWindowRestored(func(w *window.Window) { })
```

---

## Desktop

Root trinket managing windows, menus, and status bar.

```go
desktop := trinkets.NewDesktop()
desktop.SetBackend(backend.NewTUIBackend(backend.DefaultTUIOptions()))

// Applications
desktop.AddApplication(app)

// Startup
desktop.SetOnStartup(func() {
    // Create initial windows here
})

// Run event loop
desktop.Run()

// Components
menuBar := desktop.MenuBar()
statusBar := desktop.StatusBar()
dockRow := desktop.DockRow()
```

### StatusBar

```go
statusBar := desktop.StatusBar()
statusBar.SetText("Ready")

// Styled sections
statusBar.SetSections([]trinkets.StatusSection{
    {Text: "Mode: Normal", Width: 120},
    {Text: "Line 42", Width: -1},  // -1 = stretch
})

// Styled text
statusBar.SetStyledText([]trinkets.StatusTextSpan{
    {Text: "Ready - Press "},
    {Text: "F10", Style: &highlightStyle},
    {Text: " for menu"},
})
```

### DockRow

Minimized window dock.

```go
dock := desktop.DockRow()
dock.AddEntry(&trinkets.DockEntry{
    Title: "Document 1",
    OnClick: func() { mdi.RestoreWindow(win) },
})
```

---

## Application

### app.Application

```go
application := app.New(nil)  // nil backend when Desktop owns it
application.SetName("My App")

// Windows
application.AddWindow(w)
windows := application.Windows()

// Menu/Status content (for multi-app desktops)
application.SetMenuBarContent([]*trinkets.Menu{fileMenu, editMenu})
application.SetStatusBarContent([]trinkets.StatusSection{{Text: "Ready"}})

// Lifecycle callbacks
application.SetOnActivate(func() { /* app gained focus */ })
application.SetOnDeactivate(func() { /* app lost focus */ })
```

### Secondary Applications

For multi-application desktops:

```go
secondary := app.New(nil)
secondary.SetName("Secondary App")
// Set up menus, status, windows...
desktop.AddApplication(secondary)
```

---

## Focus Management

```go
// Trinket focus
trinket.SetFocusPolicy(core.StrongFocus)
trinket.SetFocus()
trinket.ClearFocus()

// Focus manager
fm := window.FocusManager()
fm.NextTrinket()      // Tab forward
fm.PreviousTrinket()  // Tab backward
fm.SetWrapAround(true)

// Callback
fm.SetOnFocusChanged(func(old, new Trinket) { })
```

---

## Accessibility

```go
// On trinkets
trinket.SetAccessibleRole(core.RoleButton)
trinket.SetAccessibleName("Submit Form")
trinket.SetAccessibleDescription("Click to submit the form")

// Announcements
am := desktop.AccessibilityManager()
am.AnnouncePolite("Document saved")
am.AnnounceAssertive("Error: Connection lost")
```

### Accessible Roles

```go
RoleButton, RoleLabel, RoleTextInput, RoleCheckBox
RoleRadioButton, RoleComboBox, RoleList, RoleListItem
RoleTree, RoleTreeItem, RoleTab, RoleTabPanel
RoleMenu, RoleMenuItem, RoleMenuBar, RoleToolBar
RoleScrollBar, RoleProgressBar, RoleSlider
RoleDialog, RoleAlert, RoleWindow, RoleTerminal
```

---

## Actions and Shortcuts

```go
action := core.NewAction("save", "Save")
action.SetShortcut(core.NewShortcut("^S"))
action.SetOnTriggered(func() { saveDocument() })

// Shortcut formats
"^S"        // Ctrl+S
"^+S"       // Ctrl+Shift+S
"Alt+F4"    // Alt+F4
"F1"        // Function key
```

---

## Backend

### TUI Backend

```go
opts := backend.DefaultTUIOptions()
opts.MouseEnabled = true
opts.AltScreen = true

tui := backend.NewTUIBackend(opts)
desktop.SetBackend(tui)
```

### RenderBackend Interface

For custom backends:

```go
type RenderBackend interface {
    Init() error
    Shutdown()
    Size() (width, height int)
    BeginFrame()
    EndFrame()
    Clear()
    DrawCell(x, y int, ch rune, style style.CellStyle)
    DrawText(x, y int, text string, style style.CellStyle, font *Font)
    FillRect(r Rect, ch rune, style style.CellStyle)
    SetClip(r Rect)
    PollEvent() Event
    SetCursorPosition(x, y int)
    SetCursorVisible(bool)
}
```

---

## Painter

High-level drawing API used in Paint() methods.

```go
func (w *MyTrinket) Paint(p *core.Painter) {
    bounds := w.Bounds()
    s := style.DefaultStyle().WithFg(style.ColorWhite)

    p.FillRect(bounds, ' ', s)
    p.DrawText(bounds.X, bounds.Y, "Hello", s, nil)
    p.DrawBox(bounds, style.BorderSingle, "Title", s)
    p.DrawHLine(bounds.X, bounds.Y+16, 80, '-', s)
}
```

Key methods:
```go
DrawCell(x, y Unit, ch rune, style CellStyle)
DrawText(x, y Unit, text string, style CellStyle, font *Font)
DrawTextAligned(bounds UnitRect, text string, hAlign, vAlign Alignment, style CellStyle, font *Font)
FillRect(bounds UnitRect, ch rune, style CellStyle)
DrawRect(bounds UnitRect, border BorderStyle, style CellStyle)
DrawBox(bounds UnitRect, border BorderStyle, title string, style CellStyle)
DrawHLine(x, y, width Unit, ch rune, style CellStyle)
DrawVLine(x, y, height Unit, ch rune, style CellStyle)
SetClip(bounds UnitRect)
PushTransform(Transform)
PopTransform()
```

---

## Common Patterns

### Creating Custom Trinkets

```go
type MyTrinket struct {
    core.TrinketBase
    value int
    onChange func(int)
}

func NewMyTrinket() *MyTrinket {
    w := &MyTrinket{}
    w.TrinketBase = *core.NewTrinketBase()
    w.Init(w)
    w.SetFocusPolicy(core.StrongFocus)
    return w
}

func (w *MyTrinket) SizeHint() core.UnitSize {
    return core.UnitSize{Width: 80, Height: 16}
}

func (w *MyTrinket) Paint(p *core.Painter) {
    // Custom rendering
}

func (w *MyTrinket) HandleKeyPress(e core.KeyPressEvent) bool {
    if e.Key == "Enter" {
        w.value++
        if w.onChange != nil {
            w.onChange(w.value)
        }
        w.Update()
        return true
    }
    return false
}
```

### Event Filters

Process events before trinkets:

```go
desktop.AddEventFilter(func(event core.Event) bool {
    if ke, ok := event.(core.KeyPressEvent); ok {
        if ke.Key == "F12" {
            showDebugPanel()
            return true  // Consume event
        }
    }
    return false  // Let event propagate
})
```

### Popup Overlays

```go
popup := &core.PopupRequest{
    ID:     "my-popup",
    Bounds: core.UnitRect{X: 100, Y: 50, Width: 200, Height: 100},
    Paint: func(p *core.Painter) {
        // Render popup content
    },
}
popupController.RegisterPopup(popup)
// Later: popupController.UnregisterPopup("my-popup")
```

---

## Unit System

Standard cell metrics: 8 units wide, 16 units tall per character cell.

```go
metrics := core.DefaultCellMetrics()

// Window sized for 60x20 cells
w.SetSize(core.UnitSize{
    Width:  60 * metrics.CellWidth,   // 480 units
    Height: 20 * metrics.CellHeight,  // 320 units
})

// Convert units to cells
col, row := metrics.UnitsToCell(x, y)

// Convert cells to units
x, y := metrics.CellsToUnits(col, row)
```
