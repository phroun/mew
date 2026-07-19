// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// Checkbox is a trinket with a checkable state.
type Checkbox struct {
	core.TrinketBase
	core.AccessibleTrinket

	text     string
	checked  bool
	triState bool // If true, supports indeterminate state
	wordWrap bool
	checkState CheckState

	// Callbacks
	onStateChanged func(state CheckState)
	onToggled      func(checked bool)
}

// CheckState represents the state of a checkbox.
type CheckState int

const (
	Unchecked CheckState = iota
	PartiallyChecked
	Checked
)

// NewCheckbox creates a new checkbox with the given text.
func NewCheckbox(text string) *Checkbox {
	c := &Checkbox{
		text:       text,
		checkState: Unchecked,
	}
	c.TrinketBase = *core.NewTrinketBase()
	c.Init(c) // Enable polymorphic focus handling
	c.SetFocusPolicy(core.StrongFocus)
	c.SetAccessibleRole(core.RoleCheckbox)
	c.SetAccessibleName(text)
	return c
}

// Text returns the checkbox text.
func (c *Checkbox) Text() string {
	return c.text
}

// SetText sets the checkbox text.
func (c *Checkbox) SetText(text string) {
	c.text = text
	c.SetAccessibleName(text)
	c.Update()
}

// IsChecked returns whether the checkbox is checked.
func (c *Checkbox) IsChecked() bool {
	return c.checkState == Checked
}

// SetChecked sets the checked state.
func (c *Checkbox) SetChecked(checked bool) {
	if checked {
		c.SetCheckState(Checked)
	} else {
		c.SetCheckState(Unchecked)
	}
}

// CheckState returns the current check state.
func (c *Checkbox) CheckState() CheckState {
	return c.checkState
}

// SetCheckState sets the check state.
func (c *Checkbox) SetCheckState(state CheckState) {
	if c.checkState == state {
		return
	}

	c.checkState = state
	c.checked = state == Checked
	c.Update()

	if c.onStateChanged != nil {
		c.onStateChanged(state)
	}

	if c.onToggled != nil {
		c.onToggled(state == Checked)
	}
}

// IsTriState returns whether the checkbox supports tri-state.
func (c *Checkbox) IsTriState() bool {
	return c.triState
}

// SetTriState sets whether the checkbox supports tri-state.
func (c *Checkbox) SetTriState(triState bool) {
	c.triState = triState
	if !triState && c.checkState == PartiallyChecked {
		c.SetCheckState(Unchecked)
	}
}

// Toggle toggles the checkbox state.
func (c *Checkbox) Toggle() {
	switch c.checkState {
	case Unchecked:
		c.SetCheckState(Checked)
	case Checked:
		if c.triState {
			c.SetCheckState(PartiallyChecked)
		} else {
			c.SetCheckState(Unchecked)
		}
	case PartiallyChecked:
		c.SetCheckState(Unchecked)
	}
}

// SetOnStateChanged sets the state changed callback.
func (c *Checkbox) SetOnStateChanged(handler func(state CheckState)) {
	c.onStateChanged = handler
}

// SetOnToggled sets the toggled callback.
func (c *Checkbox) SetOnToggled(handler func(checked bool)) {
	c.onToggled = handler
}

// WordWrap returns whether the label text wraps onto multiple lines.
func (c *Checkbox) WordWrap() bool {
	return c.wordWrap
}

// SetWordWrap enables or disables word wrapping of the label text.
// The indicator is chrome, not text: it stays on the top line and
// wrapped lines hang under the text, not under the indicator.
func (c *Checkbox) SetWordWrap(wrap bool) {
	c.wordWrap = wrap
	c.Update()
}

// SizeHint returns the preferred size.
func (c *Checkbox) SizeHint() core.UnitSize {
	metrics := c.EffectiveCellMetrics()
	font := c.EffectiveFont()
	// Indicator is decorative (3 cells), space is 1 cell, text uses font
	indicatorWidth := metrics.CellWidth * 3 // "[ ]" = 3 cells
	spaceWidth := metrics.CellWidth         // " " = 1 cell
	textWidth := font.MeasureText(c.text)
	return core.UnitSize{
		Width:  indicatorWidth + spaceWidth + textWidth,
		Height: metrics.TextHeight(1),
	}
}

// HasHeightForWidth returns true when word wrap is enabled.
func (c *Checkbox) HasHeightForWidth() bool {
	return c.wordWrap
}

// HeightForWidth returns the height needed at the given width: the
// text wraps within the width remaining after the indicator chrome.
func (c *Checkbox) HeightForWidth(width core.Unit) core.Unit {
	if !c.wordWrap {
		return c.SizeHint().Height
	}
	metrics := c.EffectiveCellMetrics()
	font := c.EffectiveFont()
	lineCount := len(wrapText(c.text, width-metrics.CellWidth*4, font))
	if lineCount < 1 {
		lineCount = 1
	}
	return core.Unit(lineCount) * metrics.CellHeight
}

// IsInlineTrinket returns true to indicate this is a text-style trinket
// that should receive horizontal margins when in a vertical box layout.
func (c *Checkbox) IsInlineTrinket() bool {
	return true
}

// Paint renders the checkbox.
func (c *Checkbox) Paint(p *core.Painter) {
	scheme := c.GetScheme()
	focused := c.HasFocus()
	metrics := c.EffectiveCellMetrics()
	font := c.EffectiveFont()

	// Determine style - always use inherited background color (ColorDefault = terminal default)
	inheritedBg := c.EffectiveBackgroundColor()
	var indicatorStyle, labelStyle style.CellStyle
	if !c.IsEnabled() {
		disabledFG := scheme.GetDisabledTextFG()
		indicatorStyle = style.DefaultStyle().WithFg(disabledFG).WithBg(inheritedBg)
		labelStyle = indicatorStyle
	} else if focused {
		indicatorStyle = style.DefaultStyle().WithFg(scheme.GetFocusedCheckBoxFG()).WithBg(inheritedBg)
		labelStyle = style.DefaultStyle().WithFg(scheme.GetFocusedCheckBoxLabelFG()).WithBg(inheritedBg)
	} else {
		indicatorStyle = style.DefaultStyle().WithFg(scheme.GetCheckBoxFG(true)).WithBg(inheritedBg)
		labelStyle = style.DefaultStyle().WithFg(scheme.GetCheckBoxLabelFG(true)).WithBg(inheritedBg)
	}

	if p.Graphical() {
		// The label is pure text: transparent so it never clips
		// neighboring glyphs. The [x] indicator is chrome and keeps
		// its background.
		labelStyle = labelStyle.WithBg(style.ColorTransparent)
	}

	// Draw checkbox indicator (decorative - use cell-based sizing)
	// Indicator is 3 cells: "[", "x" or " " or "-", "]"
	var middle rune
	switch c.checkState {
	case Unchecked:
		middle = ' '
	case Checked:
		middle = 'x'
	case PartiallyChecked:
		middle = '-'
	}
	p.DrawCell(0, 0, '[', indicatorStyle)
	p.DrawCell(metrics.CellWidth, 0, middle, indicatorStyle)
	p.DrawCell(metrics.CellWidth*2, 0, ']', indicatorStyle)

	// Draw space (decorative, 1 cell) and text (font-based)
	p.DrawCell(metrics.CellWidth*3, 0, ' ', labelStyle) // Space after indicator
	x := metrics.CellWidth * 4                          // After indicator + space (4 cells)

	if !c.wordWrap {
		p.DrawText(x, 0, c.text, labelStyle, font)
		return
	}

	// Word wrap: the indicator is chrome anchored to the top line;
	// wrapped lines hang under the text column.
	textWidth := c.Bounds().Width - x
	y := core.Unit(0)
	for _, line := range wrapText(c.text, textWidth, font) {
		p.DrawText(x, y, line, labelStyle, font)
		y += metrics.CellHeight
	}
}

// HandleKeyPress handles keyboard input.
func (c *Checkbox) HandleKeyPress(event core.KeyPressEvent) bool {
	switch event.Key {
	case " ", "Space", "Enter":
		c.Toggle()
		return true
	}
	return false
}

// HandleMousePress handles mouse clicks.
func (c *Checkbox) HandleMousePress(event core.MousePressEvent) bool {
	if event.Button == core.LeftButton {
		c.SetFocus()
		c.Toggle()
		return true
	}
	return false
}

// HandleFocusIn is called when focus is gained.
func (c *Checkbox) HandleFocusIn() {
	c.Update()
}

// HandleFocusOut is called when focus is lost.
func (c *Checkbox) HandleFocusOut() {
	c.Update()
}

// AccessibleInfo returns accessibility information.
func (c *Checkbox) AccessibleInfo() core.AccessibleInfo {
	info := c.AccessibleTrinket.AccessibleInfo()
	info.Role = core.RoleCheckbox
	info.Name = c.text

	switch c.checkState {
	case Checked:
		info.State |= core.StateChecked
	case PartiallyChecked:
		info.State |= core.StateChecked
		info.Value = "indeterminate"
	}

	if !c.IsEnabled() {
		info.State |= core.StateDisabled
	}

	return info
}
