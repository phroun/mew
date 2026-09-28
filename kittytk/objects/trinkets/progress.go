// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"strconv"
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
	caption     string // text shown on the bar; "" shows the percentage

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
	}
	p.TrinketBase = *core.NewTrinketBase()
	p.Init(p)
	p.SetFocusPolicy(core.NoFocus)
	p.SetAccessibleRole(core.RoleProgressBar)
	p.applyOrientationPolicy()
	return p
}

// applyOrientationPolicy fixes the axis a bar does not grow along: a horizontal
// bar is one line of text tall and cannot be more, however deep the row it is
// put in, while it takes whatever width it is given. A vertical one is the same
// the other way round -- two cells across, growing down the page.
func (p *ProgressBar) applyOrientationPolicy() {
	if p.orientation == core.Horizontal {
		p.SetSizePolicy(core.NewSizePolicy(core.SizePreferred, core.SizeFixed))
		return
	}
	p.SetSizePolicy(core.NewSizePolicy(core.SizeFixed, core.SizePreferred))
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
	p.applyOrientationPolicy()
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

// Caption returns the text shown on the bar, "" when it shows the percentage.
func (p *ProgressBar) Caption() string {
	return p.caption
}

// SetCaption sets the text shown on the bar in place of the percentage. It is
// shown as written: nothing in it is expanded, so an application that wants
// "2M / 250M" sends that string with each update, in the same statement as the
// value that changed. An empty caption shows the percentage.
func (p *ProgressBar) SetCaption(caption string) {
	p.caption = caption
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

// SizeHint returns the preferred size. A horizontal bar's width is the
// fallback for when nothing sets one (see defaultSizeCells).
func (p *ProgressBar) SizeHint() core.UnitSize {
	metrics := p.EffectiveCellMetrics()
	if p.orientation == core.Horizontal {
		return core.UnitSize{
			Width:  metrics.UnitsPerCellWidth * defaultSizeCells,
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

	totalCells := metrics.CharsForWidth(bounds.Width)

	// A bar fills from the LEADING edge, so it grows the way its direction
	// reads. Cells are counted from there and turned into an x here; the box
	// is the whole cells the bar covers rather than its bounds, so the part
	// too narrow for a cell stays at the far end in both directions.
	span := core.Unit(totalCells) * metrics.UnitsPerCellWidth
	cellX := func(i int) core.Unit {
		return core.LeadingX(p, span, core.Unit(i)*metrics.UnitsPerCellWidth, metrics.UnitsPerCellWidth)
	}

	// Draw incomplete background first
	for i := 0; i < totalCells; i++ {
		painter.DrawCell(cellX(i), 0, '░', incompleteStyle)
	}

	if p.indeterminate {
		// The moving block's position comes from wall time, not the
		// repaint count, so its speed doesn't fluctuate with input
		// activity (mouse moves repaint far more often than the tick).
		blockSize := 5
		pos := indeterminateSweepPos(totalCells, blockSize)
		for i := 0; i < blockSize && pos+i < totalCells; i++ {
			painter.DrawCell(cellX(pos+i), 0, '▓', completedStyle)
		}
	} else {
		// Calculate filled portion
		filledCells := totalCells * p.Percentage() / 100

		// Draw filled portion
		for i := 0; i < filledCells; i++ {
			painter.DrawCell(cellX(i), 0, '▓', completedStyle)
		}
	}

	// Draw text in center
	if p.textVisible && !p.indeterminate {
		p.paintBarText(painter, bounds, totalCells, scheme, metrics)
	}
}

// paintBarText centres the bar's text - its caption, or the percentage when it
// has none - over the bar, in the filled part's text colour where it lies over
// the fill and the empty part's colour elsewhere.
//
// The text is drawn WHOLE, twice: once in each colour, each copy clipped to its
// side of the fill's edge. Drawing it as one run leaves shaping, bidi, marks
// that ride a letter, wide characters and proportional glyphs to the text
// engine, which already gets them right; the clip is what splits the colours,
// so on a pixel surface a letter the edge runs through changes colour partway
// across. Text too long for the bar is elided rather than drawn past its end.
func (p *ProgressBar) paintBarText(painter *core.Painter, bounds core.UnitRect, totalCells int, scheme *style.Scheme, metrics core.CellMetrics) {
	cellW := metrics.UnitsPerCellWidth
	span := core.Unit(totalCells) * cellW
	shown, _ := p.ElideText(p.barText(), span)
	run := p.CellRun(shown)
	if run == "" {
		return
	}
	// Centred, and on a whole cell, so a cell target draws it on the cells it
	// measured it by.
	x := (span - p.MeasureText(run)) / 2 / cellW * cellW
	if x < 0 {
		x = 0
	}

	// The fill covers whole cells from the leading edge; the empty part is the
	// rest of the bar.
	filled := core.Unit(totalCells*p.Percentage()/100) * cellW
	fillX := core.LeadingX(p, span, 0, filled)
	fillBox := core.UnitRect{X: fillX, Width: filled, Height: bounds.Height}
	emptyBox := core.UnitRect{X: filled, Width: span - filled, Height: bounds.Height}
	if fillX != 0 {
		emptyBox.X = 0
	}

	font := p.EffectiveFont()
	if filled > 0 {
		painter.WithClip(fillBox).DrawText(x, 0, run, scheme.GetProgressFullText(), font)
	}
	if filled < span {
		painter.WithClip(emptyBox).DrawText(x, 0, run, scheme.GetProgressEmptyText(), font)
	}
}

func (p *ProgressBar) paintVertical(painter *core.Painter, bounds core.UnitRect, scheme *style.Scheme, metrics core.CellMetrics) {
	// Get progress bar styles from scheme
	completedStyle := scheme.GetProgressFull()
	incompleteStyle := scheme.GetProgressEmpty()

	totalCells := int(bounds.Height / metrics.UnitsPerCellHeight)

	// Draw incomplete background first (entire bar)
	for i := 0; i < totalCells; i++ {
		y := core.Unit(i) * metrics.UnitsPerCellHeight
		painter.FillRect(core.UnitRect{
			Y:      y,
			Width:  bounds.Width,
			Height: metrics.UnitsPerCellHeight,
		}, '░', incompleteStyle)
	}

	// Calculate filled portion (from bottom)
	filledCells := totalCells * p.Percentage() / 100

	// Draw filled portion from bottom
	for i := 0; i < filledCells; i++ {
		y := bounds.Height - core.Unit(i+1)*metrics.UnitsPerCellHeight
		painter.FillRect(core.UnitRect{
			Y:      y,
			Width:  bounds.Width,
			Height: metrics.UnitsPerCellHeight,
		}, '▓', completedStyle)
	}
}

// barText is what the bar shows: its caption, or the percentage when it has
// none.
func (p *ProgressBar) barText() string {
	if p.caption != "" {
		return p.caption
	}
	return p.percentText()
}

// percentText is the bar's percentage as text, "0%" to "100%".
func (p *ProgressBar) percentText() string {
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
	info.Value = p.percentText()
	info.ValueMin = strconv.Itoa(p.minimum)
	info.ValueMax = strconv.Itoa(p.maximum)

	if p.indeterminate {
		info.State |= core.StateBusy
	}

	if !p.IsEnabled() {
		info.State |= core.StateDisabled
	}

	return info
}
