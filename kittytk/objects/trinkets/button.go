// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"sync/atomic"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// Button is a clickable button trinket.
type Button struct {
	core.TrinketBase
	core.TrinketKeys
	core.AccessibleTrinket

	text         string
	icon         *style.Icon
	iconSize     style.IconSize
	checkable    bool
	checked      bool
	pressed      bool
	hovered      bool // Mouse is over button while pressed
	mouseOver    bool // Pointer is hovering over the button (not pressed)
	spacePressed bool // Space key is being held down
	// animatingPress shows the 250ms press-feedback animation. It is atomic
	// because the headless activation path (no desktop timer) clears it from a
	// timer goroutine while the paint path reads it — see AnimatePress.
	animatingPress atomic.Bool
	flat           bool // No border when not focused/hovered
	isDefault      bool // Default button (shown bold when not focused)
	isCancel       bool // Cancel button (activated by Escape)

	onClick  func()
	onToggle func(checked bool)
}

// NewButton creates a new button with the given text.
// buttonCommands is everything a button can carry out.
var buttonCommands = []string{
	core.CmdTrinketActivate,
	core.CmdTrinketCancel,
}

func NewButton(text string) *Button {
	b := &Button{
		text:     text,
		iconSize: style.IconSmall,
	}
	b.TrinketBase = *core.NewTrinketBase()
	b.SetCommands(buttonCommands...)
	b.Init(b) // Enable polymorphic focus handling
	b.SetFocusPolicy(core.StrongFocus)
	b.SetAccessibleRole(core.RoleButton)
	b.SetAccessibleName(text)
	return b
}

// NewIconButton creates a button with an icon.
func NewIconButton(icon *style.Icon) *Button {
	b := NewButton("")
	b.icon = icon
	if icon != nil {
		b.SetAccessibleName(icon.ID)
	}
	return b
}

// Text returns the button text.
func (b *Button) Text() string {
	return b.text
}

// SetText sets the button text.
func (b *Button) SetText(text string) {
	b.text = text
	b.SetAccessibleName(text)
	b.Update()
}

// Icon returns the button icon.
func (b *Button) Icon() *style.Icon {
	return b.icon
}

// SetIcon sets the button icon.
func (b *Button) SetIcon(icon *style.Icon) {
	b.icon = icon
	b.Update()
}

// SetIconSize sets the icon size.
func (b *Button) SetIconSize(size style.IconSize) {
	b.iconSize = size
	b.Update()
}

// IsCheckable returns whether the button is checkable.
func (b *Button) IsCheckable() bool {
	return b.checkable
}

// SetCheckable makes the button checkable (toggle button).
func (b *Button) SetCheckable(checkable bool) {
	b.checkable = checkable
	b.Update()
}

// IsChecked returns whether the button is checked.
func (b *Button) IsChecked() bool {
	return b.checked
}

// SetChecked sets the checked state.
func (b *Button) SetChecked(checked bool) {
	if b.checked == checked {
		return
	}
	b.checked = checked
	b.Update()
	if b.onToggle != nil {
		b.onToggle(checked)
	}
}

// IsFlat returns whether the button is flat (borderless).
func (b *Button) IsFlat() bool {
	return b.flat
}

// SetFlat makes the button flat.
func (b *Button) SetFlat(flat bool) {
	b.flat = flat
	b.Update()
}

// IsDefault returns whether this is the default button.
func (b *Button) IsDefault() bool {
	return b.isDefault
}

// SetDefault makes this the default button (shown bold when not focused).
func (b *Button) SetDefault(isDefault bool) {
	b.isDefault = isDefault
	b.Update()
}

// IsCancel returns whether this is the cancel button.
func (b *Button) IsCancel() bool {
	return b.isCancel
}

// SetCancel makes this the cancel button (activated by Escape key).
func (b *Button) SetCancel(isCancel bool) {
	b.isCancel = isCancel
}

// AnimatePress shows the pressed state briefly (250ms) then triggers click.
// This provides visual feedback for keyboard-triggered button presses.
func (b *Button) AnimatePress() {
	if !b.IsEnabled() || b.animatingPress.Load() {
		return
	}

	// If already showing pressed state (e.g., space held), just click
	if b.spacePressed || (b.pressed && b.hovered) {
		b.Click()
		return
	}

	// Show pressed state
	b.animatingPress.Store(true)
	b.Update()

	// After 250ms, clear the animation and fire the click — ON THE MAIN THREAD,
	// via the desktop timer, NOT a raw goroutine. onClick may tear down a window,
	// and on macOS SDL window destruction must run on the platform's main thread;
	// the old `go func` ran onClick on a runtime-timer goroutine, so a keyboard-
	// activated button that closed a window crashed with SIGTRAP in
	// SDL_DestroyWindow. Desktop timers self-repost through the platform's
	// PostAfter, so their callbacks run on the main loop.
	if start := b.mainThreadTimer(); start != nil {
		var timer interface{ Stop() }
		timer = start(250*time.Millisecond, func() {
			if timer != nil {
				timer.Stop() // one-shot: stop the repeating timer after the first fire
			}
			b.animatingPress.Store(false)
			b.Update()
			b.Click()
		})
		return
	}

	// No desktop timer provider reachable (headless / tests): there is no SDL
	// here, so no main-thread requirement — a plain timer goroutine is fine and
	// keeps the animation working outside a running desktop.
	go func() {
		time.Sleep(250 * time.Millisecond)
		b.animatingPress.Store(false)
		b.Update()
		b.Click()
	}()
}

// mainThreadTimer walks up the parent chain for a desktop-style timer provider
// and returns a starter whose callback runs on the platform's main thread
// (desktop timers self-repost through PostAfter). Returns nil when none is
// reachable. Used so a keyboard-activated button's deferred click runs on the
// main thread, where SDL window teardown is legal on macOS.
func (b *Button) mainThreadTimer() func(time.Duration, func()) interface{ Stop() } {
	for p := b.Parent(); p != nil; p = p.Parent() {
		if tp, ok := p.(interface {
			StartRepeatingTimer(interval time.Duration, callback func()) *DesktopTimer
		}); ok {
			return func(d time.Duration, cb func()) interface{ Stop() } {
				return tp.StartRepeatingTimer(d, cb)
			}
		}
	}
	return nil
}

// SetOnClick sets the click handler.
func (b *Button) SetOnClick(handler func()) {
	b.onClick = handler
}

// SetOnToggle sets the toggle handler.
func (b *Button) SetOnToggle(handler func(checked bool)) {
	b.onToggle = handler
}

// Click simulates a button click.
func (b *Button) Click() {
	if !b.IsEnabled() {
		return
	}

	if b.checkable {
		b.SetChecked(!b.checked)
	}

	if b.onClick != nil {
		b.onClick()
	}
}

// buttonShadowOffset is how far the drop shadow falls, down and right: half
// a cell's WIDTH, at the default denomination.
//
// The width, because a cell is narrower than it is tall and so it is the
// limiting measure. A shadow thrown by the taller one would reach further
// down than across and read as a smear rather than a shadow.
//
// One distance, and the same on both axes -- but saying that takes an
// exchange per axis rather than one number used twice. A unit is square
// only at the default denomination: a cell keeps its shape whatever it is
// divided into, so dividing it 16 across and 16 down leaves units half as
// wide as they are tall, and the same COUNT on each axis would then fall
// half as far across as down.
var buttonShadowOffset = core.DefaultCellMetrics().UnitsPerCellWidth / 2

// SizeHint returns the preferred size.
func (b *Button) SizeHint() core.UnitSize {
	metrics := b.EffectiveCellMetrics()

	// Text measured in THIS button's denomination. Everything else here --
	// the icon, the brackets, the shadow -- is stated in cells, which are a
	// fixed physical size and need no adjusting; the caption was measured at
	// the DEFAULT denomination, so inside a re-denominated window the button
	// sized itself around a caption counted in units of the wrong size.
	textWidth := b.MeasureText(b.text)

	// Add icon width if present (icons use fixed width)
	iconWidth := core.Unit(0)
	if b.icon != nil {
		if b.iconSize == style.IconSmall {
			iconWidth = metrics.TextWidth(3)
		} else {
			iconWidth = metrics.TextWidth(5)
		}
		if len(b.text) > 0 {
			iconWidth += metrics.UnitsPerCellWidth // Space between icon and text
		}
	}

	// Brackets are decorative - use cell-based sizing (2 cells total)
	// Plus the drop shadow's reservation on the right (see StyleInsets)
	bracketWidth := metrics.UnitsPerCellWidth * 2 // 1 cell each for left and right bracket
	insets := b.StyleInsets()

	return core.UnitSize{
		Width:  textWidth + iconWidth + bracketWidth + insets.Horizontal(),
		Height: metrics.TextHeight(1) + insets.Vertical(), // the cap, and the shadow below it
	}
}

// StyleInsets is the room a button keeps for its drop shadow: a cell to the
// right and a row below, the two edges it falls on.
//
// What it RESERVES, which is deliberately not what it paints. The pixel path
// draws a softer shadow half a cell out; reserving the whole cell and the whole
// row on both surfaces is what makes a row of trinkets land identically
// whichever surface is drawing, and what keeps the trinkets beside a button on
// the grid rather than half a row down from it.
//
// It is also what lets the size above be one row plus this, rather than two
// rows with the second unaccounted for: a layout that cannot tell the cap from
// the shadow aligns a one-row neighbour against both of them.
func (b *Button) StyleInsets() core.UnitMargins {
	metrics := b.EffectiveCellMetrics()
	return core.UnitMargins{
		Right:  metrics.UnitsPerCellWidth,
		Bottom: metrics.UnitsPerCellHeight,
	}
}

// IsInlineTrinket returns true to indicate this is a text-style trinket
// that should receive horizontal margins when in a vertical box layout.
func (b *Button) IsInlineTrinket() bool {
	return true
}

// Paint renders the button.
// TUI button rendering with drop shadow:
//   - Normal:  " OK ▄" on top row, " ▀▀▀" on bottom row (shifted right)
//   - Pressed: "  OK " shifted right by 1, no shadow
//   - Focused: "<OK>" with angle brackets
func (b *Button) Paint(p *core.Painter) {
	bounds := b.Bounds()
	scheme := b.GetScheme()
	focused := b.HasFocus()
	metrics := b.EffectiveCellMetrics()
	font := b.EffectiveFont()

	// Determine if showing pressed visual (pressed and hovering, space held, animating, or checked)
	// Disabled buttons should never show pressed state
	showPressed := b.IsEnabled() && ((b.pressed && b.hovered) || b.spacePressed || b.animatingPress.Load() || b.checked)

	// Get inherited background color for all styles
	inheritedBg := b.EffectiveBackgroundColor()
	paneType := style.GetPaneType(inheritedBg)

	// Determine style - always apply inherited background. GetButtonState
	// bakes in the precedence pressed > focus > hover > normal.
	var s style.CellStyle
	if !b.IsEnabled() {
		s = style.DefaultStyle().WithFg(scheme.GetDisabledButtonFG()).WithBg(inheritedBg)
	} else {
		// Hover is a graphical-only affordance: the cell/TUI path receives no
		// free mouse-move events, so a hover set during a drag could never be
		// cleared and would stick. Only honor it on graphical surfaces.
		hover := b.mouseOver && p.Graphical()
		// TODO: pass actual window active state instead of true.
		s = scheme.GetButtonState(true, focused, hover, showPressed)
		if b.isDefault && !showPressed && !focused && !hover {
			// Default button gets bold text in its resting state.
			s = s.WithAttrs(style.StyleBold)
		}
	}

	// Use custom style if set
	if customStyle := b.Style(); customStyle != nil {
		s = *customStyle
	}

	// Clear style uses inherited background
	clearStyle := style.DefaultStyle().WithBg(inheritedBg)

	// Shadow style based on pane type
	shadowFg := scheme.GetButtonShadowFG(paneType)
	shadowAttrs := style.StyleNormal
	if paneType == style.PaneDefault {
		shadowAttrs = style.StyleDim
	}
	shadowStyle := style.DefaultStyle().WithFg(shadowFg).WithBg(inheritedBg).WithAttrs(shadowAttrs)

	// Calculate button content width
	// Brackets are decorative - use cell-based sizing (1 cell each)
	// Text uses font-based sizing
	leftBracket := ' '
	rightBracket := ' '
	if focused {
		leftBracket = '<'
		rightBracket = '>'
	}
	bracketWidth := metrics.UnitsPerCellWidth * 2 // Each bracket is 1 cell
	textWidth := b.MeasureText(b.text)

	// Icon handling
	iconWidth := core.Unit(0)
	if b.icon != nil {
		var textIcon style.TextIcon
		if b.iconSize == style.IconSmall && b.icon.HasText(style.IconSmall) {
			textIcon = b.icon.TextSmall
		} else if b.icon.HasText(style.IconLarge) {
			textIcon = b.icon.TextLarge
		}
		if textIcon.Width > 0 {
			iconWidth = metrics.TextWidth(textIcon.Width + 1)
		}
	}

	// Total button width (content only, no shadow)
	buttonWidth := bracketWidth + textWidth + iconWidth

	graphical := p.Graphical()

	// Pressed offset: on pixel surfaces the face scoots down-right to
	// land exactly where the shadow rectangle was; cell surfaces keep
	// the classic one-column shift.
	shadowOffX := core.ExchangeX(buttonShadowOffset, core.DefaultCellMetrics(), metrics)
	shadowOffY := core.ExchangeY(buttonShadowOffset, core.DefaultCellMetrics(), metrics)
	xOffset := core.Unit(0)
	// Center the two-row button in any extra vertical space its layout gave it.
	yOffset := b.vInset()
	if showPressed {
		if graphical {
			xOffset = shadowOffX
			yOffset += shadowOffY
		} else {
			xOffset = metrics.UnitsPerCellWidth
		}
	}

	// Clear the entire button area first (to handle pressed state transition)
	p.FillRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, ' ', clearStyle)

	// Pixel surfaces: the drop shadow is one filled rectangle beneath
	// the face, offset down-right by half a column; the face paints
	// over it. The half-block construction below is the cell-surface
	// rendering of the same idea.
	if graphical && !showPressed {
		p.FillRect(core.UnitRect{
			X:      shadowOffX,
			Y:      yOffset + shadowOffY,
			Width:  buttonWidth,
			Height: metrics.UnitsPerCellHeight,
		}, ' ', style.DefaultStyle().WithBg(shadowFg))
	}

	// Draw button background
	if !b.flat || focused || showPressed {
		p.FillRect(core.UnitRect{
			X:      xOffset,
			Y:      yOffset,
			Width:  buttonWidth,
			Height: metrics.UnitsPerCellHeight,
		}, ' ', s)
	}

	// Draw drop shadow (only when not pressed - both enabled and disabled buttons get shadow)
	if !showPressed && !graphical {
		// Bottom half block on right edge of button (top row)
		shadowX := xOffset + buttonWidth
		p.DrawCell(shadowX, yOffset, '▄', shadowStyle)

		// Top half blocks on second row (shifted right by 1 cell)
		// Calculate number of cells needed for the button width
		shadowY := yOffset + metrics.UnitsPerCellHeight
		numShadowCells := int((buttonWidth + metrics.UnitsPerCellWidth - 1) / metrics.UnitsPerCellWidth)
		for i := 0; i < numShadowCells; i++ {
			p.DrawCell(metrics.UnitsPerCellWidth+metrics.CellToUnitsX(i), shadowY, '▀', shadowStyle)
		}
	}

	// Draw icon if present
	if b.icon != nil && iconWidth > 0 {
		var textIcon style.TextIcon
		if b.iconSize == style.IconSmall && b.icon.HasText(style.IconSmall) {
			textIcon = b.icon.TextSmall
		} else if b.icon.HasText(style.IconLarge) {
			textIcon = b.icon.TextLarge
		}

		if textIcon.Width > 0 {
			x := xOffset + metrics.UnitsPerCellWidth // After left bracket (1 cell)
			y := yOffset
			for row := 0; row < textIcon.Height; row++ {
				for col := 0; col < textIcon.Width; col++ {
					cell := textIcon.CellAt(col, row)
					p.DrawCell(x+metrics.CellToUnitsX(col), y+metrics.CellToUnitsY(row),
						cell.Char, cell.Style)
				}
			}
		}
	}

	// Draw left bracket/space (decorative - use DrawCell, not DrawText)
	p.DrawCell(xOffset, yOffset, leftBracket, s)

	// Draw text using font
	if b.text != "" {
		textX := xOffset + metrics.UnitsPerCellWidth + iconWidth // After left bracket (1 cell)
		p.DrawText(textX, yOffset, b.text, s, font)
	}

	// Draw right bracket/space (decorative - use DrawCell, not DrawText)
	rightX := xOffset + buttonWidth - metrics.UnitsPerCellWidth // Before right edge (1 cell)
	p.DrawCell(rightX, yOffset, rightBracket, s)
}

// HandleKeyPress handles keyboard input.
func (b *Button) HandleKeyPress(event core.KeyPressEvent) bool {
	// Disabled buttons don't respond to keyboard input
	if !b.IsEnabled() {
		return false
	}

	switch b.KeyCommand(event.Key) {
	case core.CmdTrinketActivate:
		// Triggers with a brief press animation for visual feedback. (It
		// used to latch pressed until a key-release event, but neither
		// backend delivers key releases - the TUI cannot at all - so the
		// button stuck depressed.)
		b.AnimatePress()
		return true
	case core.CmdTrinketCancel:
		// Escape cancels space press first
		if b.spacePressed {
			b.spacePressed = false
			b.Update()
			return true
		}
		// If this is a cancel button, activate it
		if b.isCancel {
			b.AnimatePress()
			return true
		}
	}
	return false
}

// HandleKeyRelease handles key release.
func (b *Button) HandleKeyRelease(event core.KeyReleaseEvent) bool {
	if !b.spacePressed {
		return false
	}
	// A release is never fed to the sequence processor -- a chord is made of
	// presses, and running the release through it would advance a pending
	// prefix a second time -- so this asks the registry directly whether the
	// key that came up is one that activates.
	for _, k := range core.FindKeyRegistry(b).KeysFor(core.CmdTrinketActivate) {
		if k == event.Key {
			b.spacePressed = false
			b.Update()
			b.Click()
			return true
		}
	}
	return false
}

// vInset returns the button's vertical offset within its bounds. A button's
// height is intrinsic - two rows (face + drop shadow) - so when a layout hands
// it extra vertical space (e.g. an H-box stretches it to the row height) the
// button is centered, with the slack split above and below. Cell surfaces
// quantize the top space to whole rows, favoring the top on a tie (an odd row
// of slack goes below, so the button sits one row higher).
func (b *Button) vInset() core.Unit {
	bounds := b.Bounds()
	metrics := b.EffectiveCellMetrics()
	slack := bounds.Height - metrics.UnitsPerCellHeight*2
	if slack <= 0 {
		return 0
	}
	if core.FindSmoothPositioning(b.Self()) {
		return slack / 2
	}
	rows := slack / metrics.UnitsPerCellHeight
	return (rows / 2) * metrics.UnitsPerCellHeight
}

// hitRect returns the button's local click/hover region. It follows the
// intrinsic two-row footprint at its centered offset (the extra vertical space
// a layout grants is inert). On graphical surfaces the drop shadow only reaches
// partway into the second row, so the dead bottom half-row is trimmed; cell
// surfaces use the full two rows.
func (b *Button) hitRect() core.UnitRect {
	bounds := b.Bounds()
	metrics := b.EffectiveCellMetrics()
	top := b.vInset()
	h := metrics.UnitsPerCellHeight * 2
	if top+h > bounds.Height {
		h = bounds.Height - top
	}
	if core.FindGraphicalFrames(b.Self()) {
		h -= metrics.UnitsPerCellHeight / 2
	}
	return core.UnitRect{X: 0, Y: top, Width: bounds.Width, Height: h}
}

// inHitBox reports whether a local point falls in the button's hit region.
func (b *Button) inHitBox(x, y core.Unit) bool {
	r := b.hitRect()
	return x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height
}

// HandleMousePress handles mouse clicks.
func (b *Button) HandleMousePress(event core.MousePressEvent) bool {
	if event.Button == core.LeftButton {
		// A press in the button's dead zone (the excluded bottom half-row on
		// graphical surfaces) isn't on the button - let it fall through.
		if !b.inHitBox(event.X, event.Y) {
			return false
		}
		// Disabled buttons don't respond to mouse input
		if !b.IsEnabled() {
			return true // Consume event but don't do anything
		}
		b.SetFocus() // Focus on mouse down
		b.pressed = true
		b.hovered = true
		b.Update()
		return true
	}
	return false
}

// HandleMouseMove handles mouse movement: it tracks a plain pointer-hover
// highlight when the button is idle, and the pressed-and-over state during
// a press.
func (b *Button) HandleMouseMove(event core.MouseMoveEvent) bool {
	// Hover and drag use the same hit box as the click path (full bounds on
	// cell surfaces; full bounds minus the dead bottom half-row on graphical
	// surfaces), so all three stop at the same edge.
	overBounds := b.inHitBox(event.X, event.Y)

	if !b.pressed {
		// Plain hover is a no-button affordance: while any button is held, a
		// drag begun elsewhere is merely passing over, so treat the pointer as
		// "not inside" - this both suppresses new hover and clears any set
		// before the pointer went down.
		inside := overBounds && event.Buttons == 0
		// Don't consume the move, so sibling widgets can still clear their own
		// hover as the pointer leaves them.
		if b.IsEnabled() && inside != b.mouseOver {
			b.mouseOver = inside
			b.Update()
		}
		return false
	}

	// This button owns the press: stay pressed as the pointer drags around,
	// and only drop the pressed look once the pointer leaves the hit box. The
	// held button is ours, so ignore event.Buttons here - re-entering the same
	// button during the same drag lights it back up as pressed.
	if overBounds != b.hovered {
		b.hovered = overBounds
		b.Update()
	}

	return true // Capture mouse while pressed
}

// HandleMouseRelease handles mouse release.
func (b *Button) HandleMouseRelease(event core.MouseReleaseEvent) bool {
	if b.pressed {
		wasHovered := b.hovered
		b.pressed = false
		b.hovered = false
		b.Update()

		// Only trigger click if mouse was still on the button
		if wasHovered {
			b.Click()
		}
		return true
	}
	return false
}

// HandleFocusIn is called when focus is gained.
func (b *Button) HandleFocusIn() {
	b.Update()
}

// HandleFocusOut is called when focus is lost.
func (b *Button) HandleFocusOut() {
	b.pressed = false
	b.hovered = false
	b.mouseOver = false
	b.spacePressed = false
	b.animatingPress.Store(false)
	b.Update()
}

// AccessibleInfo returns accessibility information.
func (b *Button) AccessibleInfo() core.AccessibleInfo {
	info := b.AccessibleTrinket.AccessibleInfo()
	info.Role = core.RoleButton
	info.Name = b.text
	if b.checkable {
		if b.checked {
			info.State |= core.StateChecked
		}
	}
	if b.pressed {
		info.State |= core.StatePressed
	}
	if !b.IsEnabled() {
		info.State |= core.StateDisabled
	}
	return info
}
