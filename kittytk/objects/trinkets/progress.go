// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// ProgressBar displays progress as a horizontal bar.
type ProgressBar struct {
	core.TrinketBase
	core.AccessibleTrinket

	value       int
	minimum     int
	maximum     int
	orientation core.Orientation
	textVisible bool
	format      string // e.g., "%p%" for percentage

	// Indeterminate mode (unknown progress)
	indeterminate bool
	indetTimer    *DesktopTimer // drives the indeterminate sweep's repaints
}

// NewProgressBar creates a new progress bar.
func NewProgressBar() *ProgressBar {
	p := &ProgressBar{
		minimum:     0,
		maximum:     100,
		orientation: core.Horizontal,
		textVisible: true,
		format:      "%p%",
	}
	p.TrinketBase = *core.NewTrinketBase()
	p.Init(p)
	p.SetFocusPolicy(core.NoFocus)
	p.SetAccessibleRole(core.RoleProgressBar)
	return p
}

// Value returns the current value.
func (p *ProgressBar) Value() int {
	return p.value
}

// SetValue sets the current value.
func (p *ProgressBar) SetValue(value int) {
	if value < p.minimum {
		value = p.minimum
	}
	if value > p.maximum {
		value = p.maximum
	}
	if p.value == value {
		return
	}
	p.value = value
	p.Update()
}

// Minimum returns the minimum value.
func (p *ProgressBar) Minimum() int {
	return p.minimum
}

// SetMinimum sets the minimum value.
func (p *ProgressBar) SetMinimum(min int) {
	p.minimum = min
	if p.value < min {
		p.value = min
	}
	p.Update()
}

// Maximum returns the maximum value.
func (p *ProgressBar) Maximum() int {
	return p.maximum
}

// SetMaximum sets the maximum value.
func (p *ProgressBar) SetMaximum(max int) {
	p.maximum = max
	if p.value > max {
		p.value = max
	}
	p.Update()
}

// SetRange sets both minimum and maximum.
func (p *ProgressBar) SetRange(min, max int) {
	p.minimum = min
	p.maximum = max
	if p.value < min {
		p.value = min
	}
	if p.value > max {
		p.value = max
	}
	p.Update()
}

// Orientation returns the orientation.
func (p *ProgressBar) Orientation() core.Orientation {
	return p.orientation
}

// SetOrientation sets the orientation.
func (p *ProgressBar) SetOrientation(orientation core.Orientation) {
	p.orientation = orientation
	p.Update()
}

// IsTextVisible returns whether the text is visible.
func (p *ProgressBar) IsTextVisible() bool {
	return p.textVisible
}

// SetTextVisible sets whether the text is visible.
func (p *ProgressBar) SetTextVisible(visible bool) {
	p.textVisible = visible
	p.Update()
}

// Format returns the text format.
func (p *ProgressBar) Format() string {
	return p.format
}

// SetFormat sets the text format.
// %p = percentage (0-100)
// %v = value
// %m = maximum
func (p *ProgressBar) SetFormat(format string) {
	p.format = format
	p.Update()
}

// IsIndeterminate returns whether the progress bar is in indeterminate mode.
func (p *ProgressBar) IsIndeterminate() bool {
	return p.indeterminate
}

// SetIndeterminate sets whether the progress bar is in indeterminate mode.
func (p *ProgressBar) SetIndeterminate(indeterminate bool) {
	p.indeterminate = indeterminate
	p.Update()
}

// Percentage returns the current percentage (0-100).
func (p *ProgressBar) Percentage() int {
	if p.maximum == p.minimum {
		return 0
	}
	return (p.value - p.minimum) * 100 / (p.maximum - p.minimum)
}

// Reset resets the progress bar to minimum.
func (p *ProgressBar) Reset() {
	p.value = p.minimum
	p.Update()
}

// Advance advances the value by the given amount.
func (p *ProgressBar) Advance(amount int) {
	p.SetValue(p.value + amount)
}

// SizeHint returns the preferred size.
func (p *ProgressBar) SizeHint() core.UnitSize {
	metrics := p.EffectiveCellMetrics()
	// ProgressBar draws block characters (░▓), not text - use cell-based sizing
	if p.orientation == core.Horizontal {
		return core.UnitSize{
			Width:  metrics.TextWidth(20), // 20 cells wide
			Height: metrics.TextHeight(1),
		}
	}
	return core.UnitSize{
		Width:  metrics.TextWidth(2), // 2 cells wide
		Height: metrics.TextHeight(10),
	}
}

// IsInlineTrinket returns true to indicate this is a text-style trinket
// that should receive horizontal margins when in a vertical box layout.
func (p *ProgressBar) IsInlineTrinket() bool {
	return true
}

// Paint renders the progress bar.
func (p *ProgressBar) Paint(painter *core.Painter) {
	// An indeterminate bar animates off wall time, so it must keep requesting
	// repaints on its own - the desktop no longer blindly repaints every tick.
	if p.indeterminate {
		p.ensureIndetTimer()
	} else {
		p.stopIndetTimer()
	}

	bounds := p.Bounds()
	scheme := p.GetScheme()
	metrics := p.EffectiveCellMetrics()

	if p.orientation == core.Horizontal {
		p.paintHorizontal(painter, bounds, scheme, metrics)
	} else {
		p.paintVertical(painter, bounds, scheme, metrics)
	}
}

func (p *ProgressBar) paintHorizontal(painter *core.Painter, bounds core.UnitRect, scheme *style.Scheme, metrics core.CellMetrics) {
	// Get progress bar styles from scheme
	completedStyle := scheme.GetProgressFull()
	incompleteStyle := scheme.GetProgressEmpty()

	// Draw incomplete background first
	for i := 0; i < metrics.CharsForWidth(bounds.Width); i++ {
		x := core.Unit(i) * metrics.CellWidth
		painter.DrawCell(x, 0, '░', incompleteStyle)
	}

	totalCells := metrics.CharsForWidth(bounds.Width)

	if p.indeterminate {
		// The moving block's position comes from wall time, not the
		// repaint count, so its speed doesn't fluctuate with input
		// activity (mouse moves repaint far more often than the tick).
		blockSize := 5
		pos := indeterminateSweepPos(totalCells, blockSize)
		for i := 0; i < blockSize && pos+i < totalCells; i++ {
			x := core.Unit(pos+i) * metrics.CellWidth
			painter.DrawCell(x, 0, '▓', completedStyle)
		}
	} else {
		// Calculate filled portion
		filledCells := totalCells * p.Percentage() / 100

		// Draw filled portion
		for i := 0; i < filledCells; i++ {
			x := core.Unit(i) * metrics.CellWidth
			painter.DrawCell(x, 0, '▓', completedStyle)
		}
	}

	// Draw text in center
	if p.textVisible && !p.indeterminate {
		text := p.formatText()
		textLen := len(text)
		startX := (totalCells - textLen) / 2
		if startX < 0 {
			startX = 0
		}

		// Get text styles from scheme
		activeTextStyle := scheme.GetProgressFullText()
		inactiveTextStyle := scheme.GetProgressEmptyText()

		filledCells := totalCells * p.Percentage() / 100
		for i, ch := range text {
			x := core.Unit(startX+i) * metrics.CellWidth
			// Use appropriate style based on position
			var s style.CellStyle
			if startX+i < filledCells {
				s = activeTextStyle
			} else {
				s = inactiveTextStyle
			}
			painter.DrawCell(x, 0, ch, s)
		}
	}
}

func (p *ProgressBar) paintVertical(painter *core.Painter, bounds core.UnitRect, scheme *style.Scheme, metrics core.CellMetrics) {
	// Get progress bar styles from scheme
	completedStyle := scheme.GetProgressFull()
	incompleteStyle := scheme.GetProgressEmpty()

	totalCells := int(bounds.Height / metrics.CellHeight)

	// Draw incomplete background first (entire bar)
	for i := 0; i < totalCells; i++ {
		y := core.Unit(i) * metrics.CellHeight
		painter.FillRect(core.UnitRect{
			Y:      y,
			Width:  bounds.Width,
			Height: metrics.CellHeight,
		}, '░', incompleteStyle)
	}

	// Calculate filled portion (from bottom)
	filledCells := totalCells * p.Percentage() / 100

	// Draw filled portion from bottom
	for i := 0; i < filledCells; i++ {
		y := bounds.Height - core.Unit(i+1)*metrics.CellHeight
		painter.FillRect(core.UnitRect{
			Y:      y,
			Width:  bounds.Width,
			Height: metrics.CellHeight,
		}, '▓', completedStyle)
	}
}

func (p *ProgressBar) formatText() string {
	// Format percentage properly (handles 0-100)
	pct := p.Percentage()
	if pct >= 100 {
		return "100%"
	} else if pct >= 10 {
		return string(rune('0'+pct/10)) + string(rune('0'+pct%10)) + "%"
	} else {
		return string(rune('0'+pct)) + "%"
	}
}

// AnimateIndeterminate requests a repaint while in indeterminate
// mode. Call it periodically; the block's position itself is derived
// from wall time (indeterminateSweepPos), so the call cadence only
// affects smoothness, never speed.
func (p *ProgressBar) AnimateIndeterminate() {
	if p.indeterminate {
		p.Update()
	}
}

// ensureIndetTimer starts a ~20Hz repaint timer while indeterminate, so the
// sweep keeps advancing without the desktop's old blind per-tick repaint.
// Started lazily from Paint once the bar can reach a desktop timer source.
func (p *ProgressBar) ensureIndetTimer() {
	if p.indetTimer != nil {
		return
	}
	d := findDesktopFor(p)
	if d == nil {
		return
	}
	p.indetTimer = d.StartRepeatingTimer(50*time.Millisecond, p.AnimateIndeterminate)
}

func (p *ProgressBar) stopIndetTimer() {
	if p.indetTimer != nil {
		p.indetTimer.Stop()
		p.indetTimer = nil
	}
}

// indeterminateEpoch anchors the sweep to wall time.
var indeterminateEpoch = time.Now()

// indeterminateSweepPos places the indeterminate block for a bar of
// totalCells at this instant: a triangle wave (bounce) at a fixed
// cells-per-second rate, identical on cell and pixel surfaces.
func indeterminateSweepPos(totalCells, blockSize int) int {
	travel := totalCells - blockSize
	if travel <= 0 {
		return 0
	}
	const cellsPerSecond = 12
	ph := int(time.Since(indeterminateEpoch).Milliseconds()*cellsPerSecond/1000) % (2 * travel)
	if ph > travel {
		ph = 2*travel - ph
	}
	return ph
}

// AccessibleInfo returns accessibility information.
func (p *ProgressBar) AccessibleInfo() core.AccessibleInfo {
	info := p.AccessibleTrinket.AccessibleInfo()
	info.Role = core.RoleProgressBar
	info.Value = p.formatText()
	info.ValueMin = string(rune('0' + p.minimum))
	info.ValueMax = string(rune('0' + p.maximum))

	if p.indeterminate {
		info.State |= core.StateBusy
	}

	if !p.IsEnabled() {
		info.State |= core.StateDisabled
	}

	return info
}
