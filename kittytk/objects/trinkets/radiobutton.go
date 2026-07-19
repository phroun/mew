// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"sync"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// RadioButton is a mutually exclusive option button.
type RadioButton struct {
	core.TrinketBase
	core.AccessibleTrinket

	text     string
	checked  bool
	wordWrap bool
	group    *RadioGroup

	// Callbacks
	onToggled func(checked bool)
}

// NewRadioButton creates a new radio button with the given text.
func NewRadioButton(text string) *RadioButton {
	r := &RadioButton{
		text: text,
	}
	r.TrinketBase = *core.NewTrinketBase()
	r.Init(r) // Enable polymorphic focus handling
	r.SetFocusPolicy(core.StrongFocus)
	r.SetAccessibleRole(core.RoleRadioButton)
	r.SetAccessibleName(text)
	return r
}

// Text returns the radio button text.
func (r *RadioButton) Text() string {
	return r.text
}

// SetText sets the radio button text.
func (r *RadioButton) SetText(text string) {
	r.text = text
	r.SetAccessibleName(text)
	r.Update()
}

// IsChecked returns whether the radio button is checked.
func (r *RadioButton) IsChecked() bool {
	return r.checked
}

// SetChecked sets the checked state.
// This will uncheck other buttons in the same group.
func (r *RadioButton) SetChecked(checked bool) {
	if r.checked == checked {
		return
	}

	if checked && r.group != nil {
		r.group.selectButton(r)
	} else {
		r.checked = checked
		r.Update()
		if r.onToggled != nil {
			r.onToggled(checked)
		}
	}
}

// Group returns the radio group this button belongs to.
func (r *RadioButton) Group() *RadioGroup {
	return r.group
}

// SetOnToggled sets the toggled callback.
func (r *RadioButton) SetOnToggled(handler func(checked bool)) {
	r.onToggled = handler
}

// WordWrap returns whether the label text wraps onto multiple lines.
func (r *RadioButton) WordWrap() bool {
	return r.wordWrap
}

// SetWordWrap enables or disables word wrapping of the label text.
// The indicator is chrome, not text: it stays on the top line and
// wrapped lines hang under the text, not under the indicator.
func (r *RadioButton) SetWordWrap(wrap bool) {
	r.wordWrap = wrap
	r.Update()
}

// SizeHint returns the preferred size.
func (r *RadioButton) SizeHint() core.UnitSize {
	metrics := r.EffectiveCellMetrics()
	font := r.EffectiveFont()
	// Indicator is decorative (3 cells), space is 1 cell, text uses font
	indicatorWidth := metrics.CellWidth * 3 // "( )" = 3 cells
	spaceWidth := metrics.CellWidth         // " " = 1 cell
	textWidth := font.MeasureText(r.text)
	return core.UnitSize{
		Width:  indicatorWidth + spaceWidth + textWidth,
		Height: metrics.TextHeight(1),
	}
}

// HasHeightForWidth returns true when word wrap is enabled.
func (r *RadioButton) HasHeightForWidth() bool {
	return r.wordWrap
}

// HeightForWidth returns the height needed at the given width: the
// text wraps within the width remaining after the indicator chrome.
func (r *RadioButton) HeightForWidth(width core.Unit) core.Unit {
	if !r.wordWrap {
		return r.SizeHint().Height
	}
	metrics := r.EffectiveCellMetrics()
	font := r.EffectiveFont()
	lineCount := len(wrapText(r.text, width-metrics.CellWidth*4, font))
	if lineCount < 1 {
		lineCount = 1
	}
	return core.Unit(lineCount) * metrics.CellHeight
}

// IsInlineTrinket returns true to indicate this is a text-style trinket
// that should receive horizontal margins when in a vertical box layout.
func (r *RadioButton) IsInlineTrinket() bool {
	return true
}

// Paint renders the radio button.
func (r *RadioButton) Paint(p *core.Painter) {
	scheme := r.GetScheme()
	focused := r.HasFocus()

	// Determine style - always use inherited background color (ColorDefault = terminal default)
	inheritedBg := r.EffectiveBackgroundColor()
	var indicatorStyle, labelStyle style.CellStyle
	if !r.IsEnabled() {
		disabledFG := scheme.GetDisabledTextFG()
		indicatorStyle = style.DefaultStyle().WithFg(disabledFG).WithBg(inheritedBg)
		labelStyle = indicatorStyle
	} else if focused {
		indicatorStyle = style.DefaultStyle().WithFg(scheme.GetFocusedRadioButtonFG()).WithBg(inheritedBg)
		labelStyle = style.DefaultStyle().WithFg(scheme.GetFocusedRadioButtonLabelFG()).WithBg(inheritedBg)
	} else {
		indicatorStyle = style.DefaultStyle().WithFg(scheme.GetRadioButtonFG(true)).WithBg(inheritedBg)
		labelStyle = style.DefaultStyle().WithFg(scheme.GetRadioButtonLabelFG(true)).WithBg(inheritedBg)
	}

	if p.Graphical() {
		// The label is pure text: transparent so it never clips
		// neighboring glyphs. The (*) indicator is chrome and keeps
		// its background.
		labelStyle = labelStyle.WithBg(style.ColorTransparent)
	}

	// Draw radio indicator (decorative - use cell-based sizing)
	// Indicator is 3 cells: "(", "*" or " ", ")"
	metrics := r.EffectiveCellMetrics()
	font := r.EffectiveFont()
	var middle rune
	if r.checked {
		middle = '*'
	} else {
		middle = ' '
	}
	p.DrawCell(0, 0, '(', indicatorStyle)
	p.DrawCell(metrics.CellWidth, 0, middle, indicatorStyle)
	p.DrawCell(metrics.CellWidth*2, 0, ')', indicatorStyle)

	// Draw space (decorative, 1 cell) and text (font-based)
	p.DrawCell(metrics.CellWidth*3, 0, ' ', labelStyle) // Space after indicator
	x := metrics.CellWidth * 4                          // After indicator + space (4 cells)

	if !r.wordWrap {
		p.DrawText(x, 0, r.text, labelStyle, font)
		return
	}

	// Word wrap: the indicator is chrome anchored to the top line;
	// wrapped lines hang under the text column.
	textWidth := r.Bounds().Width - x
	y := core.Unit(0)
	for _, line := range wrapText(r.text, textWidth, font) {
		p.DrawText(x, y, line, labelStyle, font)
		y += metrics.CellHeight
	}
}

// HandleKeyPress handles keyboard input.
func (r *RadioButton) HandleKeyPress(event core.KeyPressEvent) bool {
	switch event.Key {
	case " ", "Space", "Enter":
		r.SetChecked(true)
		return true
	case "Up", "Left":
		if r.group != nil {
			r.group.SelectPrevious()
			return true
		}
	case "Down", "Right":
		if r.group != nil {
			r.group.SelectNext()
			return true
		}
	}
	return false
}

// HandleMousePress handles mouse clicks.
func (r *RadioButton) HandleMousePress(event core.MousePressEvent) bool {
	if event.Button == core.LeftButton {
		r.SetFocus()
		r.SetChecked(true)
		return true
	}
	return false
}

// HandleFocusIn is called when focus is gained.
func (r *RadioButton) HandleFocusIn() {
	r.Update()
}

// HandleFocusOut is called when focus is lost.
func (r *RadioButton) HandleFocusOut() {
	r.Update()
}

// AccessibleInfo returns accessibility information.
func (r *RadioButton) AccessibleInfo() core.AccessibleInfo {
	info := r.AccessibleTrinket.AccessibleInfo()
	info.Role = core.RoleRadioButton
	info.Name = r.text

	if r.checked {
		info.State |= core.StateChecked
	}

	if !r.IsEnabled() {
		info.State |= core.StateDisabled
	}

	if r.group != nil {
		info.SetSize = len(r.group.buttons)
		for i, btn := range r.group.buttons {
			if btn == r {
				info.PositionInSet = i + 1
				break
			}
		}
	}

	return info
}

// RadioGroup manages a group of mutually exclusive radio buttons.
type RadioGroup struct {
	mu      sync.RWMutex
	buttons []*RadioButton
	selected *RadioButton

	// Callbacks
	onSelectionChanged func(*RadioButton)
}

// NewRadioGroup creates a new radio group.
func NewRadioGroup() *RadioGroup {
	return &RadioGroup{}
}

// AddButton adds a radio button to the group.
func (g *RadioGroup) AddButton(button *RadioButton) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Remove from old group if any
	if button.group != nil && button.group != g {
		button.group.RemoveButton(button)
	}

	button.group = g
	g.buttons = append(g.buttons, button)

	// If this is the first button and it's checked, or no button is selected,
	// consider auto-selection
	if button.checked {
		g.selected = button
	}
}

// RemoveButton removes a radio button from the group.
func (g *RadioGroup) RemoveButton(button *RadioButton) {
	g.mu.Lock()
	defer g.mu.Unlock()

	for i, b := range g.buttons {
		if b == button {
			g.buttons = append(g.buttons[:i], g.buttons[i+1:]...)
			button.group = nil
			if g.selected == button {
				g.selected = nil
			}
			break
		}
	}
}

// Buttons returns all buttons in the group.
func (g *RadioGroup) Buttons() []*RadioButton {
	g.mu.RLock()
	defer g.mu.RUnlock()
	result := make([]*RadioButton, len(g.buttons))
	copy(result, g.buttons)
	return result
}

// Selected returns the currently selected button.
func (g *RadioGroup) Selected() *RadioButton {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.selected
}

// selectButton selects a button and unselects others.
func (g *RadioGroup) selectButton(button *RadioButton) {
	g.mu.Lock()
	if g.selected == button {
		g.mu.Unlock()
		return
	}

	oldSelected := g.selected
	g.selected = button
	handler := g.onSelectionChanged
	g.mu.Unlock()

	// Update old button
	if oldSelected != nil {
		oldSelected.checked = false
		oldSelected.Update()
		if oldSelected.onToggled != nil {
			oldSelected.onToggled(false)
		}
	}

	// Update new button
	button.checked = true
	button.Update()
	if button.onToggled != nil {
		button.onToggled(true)
	}

	if handler != nil {
		handler(button)
	}
}

// SelectNext selects the next button in the group.
func (g *RadioGroup) SelectNext() {
	g.mu.RLock()
	buttons := g.buttons
	selected := g.selected
	g.mu.RUnlock()

	if len(buttons) == 0 {
		return
	}

	currentIdx := -1
	for i, b := range buttons {
		if b == selected {
			currentIdx = i
			break
		}
	}

	// Find next enabled button
	for i := 1; i <= len(buttons); i++ {
		nextIdx := (currentIdx + i) % len(buttons)
		if buttons[nextIdx].IsEnabled() {
			buttons[nextIdx].SetFocus()
			g.selectButton(buttons[nextIdx])
			return
		}
	}
}

// SelectPrevious selects the previous button in the group.
func (g *RadioGroup) SelectPrevious() {
	g.mu.RLock()
	buttons := g.buttons
	selected := g.selected
	g.mu.RUnlock()

	if len(buttons) == 0 {
		return
	}

	currentIdx := len(buttons)
	for i, b := range buttons {
		if b == selected {
			currentIdx = i
			break
		}
	}

	// Find previous enabled button
	for i := 1; i <= len(buttons); i++ {
		prevIdx := (currentIdx - i + len(buttons)) % len(buttons)
		if buttons[prevIdx].IsEnabled() {
			buttons[prevIdx].SetFocus()
			g.selectButton(buttons[prevIdx])
			return
		}
	}
}

// SetOnSelectionChanged sets the selection changed callback.
func (g *RadioGroup) SetOnSelectionChanged(handler func(*RadioButton)) {
	g.mu.Lock()
	g.onSelectionChanged = handler
	g.mu.Unlock()
}
