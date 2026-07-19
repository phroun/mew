// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// Button is a clickable button trinket.
type Button struct {
	core.TrinketBase
	core.AccessibleTrinket

	text           string
	icon           *style.Icon
	iconSize       style.IconSize
	checkable      bool
	checked        bool
	pressed        bool
	hovered        bool // Mouse is over button while pressed
	mouseOver      bool // Pointer is hovering over the button (not pressed)
	spacePressed   bool // Space key is being held down
	animatingPress bool // Showing press animation (250ms visual feedback)
	flat           bool // No border when not focused/hovered
	isDefault      bool // Default button (shown bold when not focused)
	isCancel       bool // Cancel button (activated by Escape)

	onClick  func()
	onToggle func(checked bool)
}

// NewButton creates a new button with the given text.
func NewButton(text string) *Button {
	b := &Button{
		text:     text,
		iconSize: style.IconSmall,
	}
	b.TrinketBase = *core.NewTrinketBase()
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
	if !b.IsEnabled() || b.animatingPress {
		return
	}

	// If already showing pressed state (e.g., space held), just click
	if b.spacePressed || (b.pressed && b.hovered) {
		b.Click()
		return
	}

	// Show pressed state
	b.animatingPress = true
	b.Update()

	// After 250ms, clear animation and trigger click
	go func() {
		time.Sleep(250 * time.Millisecond)
		b.animatingPress = false
		b.Update()
		b.Click()
	}()
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

// SizeHint returns the preferred size.
func (b *Button) SizeHint() core.UnitSize {
	metrics := b.EffectiveCellMetrics()
	font := b.EffectiveFont()

	// Calculate text width using font measurement
	textWidth := font.MeasureText(b.text)

	// Add icon width if present (icons use fixed width)
	iconWidth := core.Unit(0)
	if b.icon != nil {
		if b.iconSize == style.IconSmall {
			iconWidth = metrics.TextWidth(3)
		} else {
			iconWidth = metrics.TextWidth(5)
		}
		if len(b.text) > 0 {
			iconWidth += metrics.CellWidth // Space between icon and text
		}
	}

	// Brackets are decorative - use cell-based sizing (2 cells total)
	// Plus 1 cell for drop shadow on the right
	bracketWidth := metrics.CellWidth * 2 // 1 cell each for left and right bracket
	shadowWidth := metrics.CellWidth

	return core.UnitSize{
		Width:  textWidth + iconWidth + bracketWidth + shadowWidth,
		Height: metrics.TextHeight(2), // 2 rows: button + shadow row
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
	showPressed := b.IsEnabled() && ((b.pressed && b.hovered) || b.spacePressed || b.animatingPress || b.checked)

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
	bracketWidth := metrics.CellWidth * 2 // Each bracket is 1 cell
	textWidth := font.MeasureText(b.text)

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
	shadowOff := metrics.CellWidth / 2
	xOffset := core.Unit(0)
	// Center the two-row button in any extra vertical space its layout gave it.
	yOffset := b.vInset()
	if showPressed {
		if graphical {
			xOffset = shadowOff
			yOffset += shadowOff
		} else {
			xOffset = metrics.CellWidth
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
			X:      shadowOff,
			Y:      yOffset + shadowOff,
			Width:  buttonWidth,
			Height: metrics.CellHeight,
		}, ' ', style.DefaultStyle().WithBg(shadowFg))
	}

	// Draw button background
	if !b.flat || focused || showPressed {
		p.FillRect(core.UnitRect{
			X:      xOffset,
			Y:      yOffset,
			Width:  buttonWidth,
			Height: metrics.CellHeight,
		}, ' ', s)
	}

	// Draw drop shadow (only when not pressed - both enabled and disabled buttons get shadow)
	if !showPressed && !graphical {
		// Bottom half block on right edge of button (top row)
		shadowX := xOffset + buttonWidth
		p.DrawCell(shadowX, yOffset, '▄', shadowStyle)

		// Top half blocks on second row (shifted right by 1 cell)
		// Calculate number of cells needed for the button width
		shadowY := yOffset + metrics.CellHeight
		numShadowCells := int((buttonWidth + metrics.CellWidth - 1) / metrics.CellWidth)
		for i := 0; i < numShadowCells; i++ {
			p.DrawCell(metrics.CellWidth+metrics.CellToUnitsX(i), shadowY, '▀', shadowStyle)
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
			x := xOffset + metrics.CellWidth // After left bracket (1 cell)
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
		textX := xOffset + metrics.CellWidth + iconWidth // After left bracket (1 cell)
		p.DrawText(textX, yOffset, b.text, s, font)
	}

	// Draw right bracket/space (decorative - use DrawCell, not DrawText)
	rightX := xOffset + buttonWidth - metrics.CellWidth // Before right edge (1 cell)
	p.DrawCell(rightX, yOffset, rightBracket, s)
}

// HandleKeyPress handles keyboard input.
func (b *Button) HandleKeyPress(event core.KeyPressEvent) bool {
	// Disabled buttons don't respond to keyboard input
	if !b.IsEnabled() {
		return false
	}

	switch event.Key {
	case "Enter":
		// Enter triggers with animation for visual feedback
		b.AnimatePress()
		return true
	case " ", "Space":
		// Space triggers like Enter, with the same brief press
		// animation. (It used to latch pressed until a key-release
		// event, but neither backend delivers key releases - the TUI
		// cannot at all - so the button stuck depressed.)
		b.AnimatePress()
		return true
	case "Escape":
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
	switch event.Key {
	case " ", "Space":
		if b.spacePressed {
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
	slack := bounds.Height - metrics.CellHeight*2
	if slack <= 0 {
		return 0
	}
	if core.FindSmoothPositioning(b.Self()) {
		return slack / 2
	}
	rows := slack / metrics.CellHeight
	return (rows / 2) * metrics.CellHeight
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
	h := metrics.CellHeight * 2
	if top+h > bounds.Height {
		h = bounds.Height - top
	}
	if core.FindGraphicalFrames(b.Self()) {
		h -= metrics.CellHeight / 2
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
	b.animatingPress = false
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
