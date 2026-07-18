package plugins

import (
	"fmt"
	"strings"

	"github.com/phroun/mew/internal/config"
	"github.com/phroun/mew/internal/window"
)

// ColumnRulerPlugin renders the column ruler line. The ruler is not a window
// of its own: any window with ViewState.ShowRuler enabled gets a ruler drawn
// on its own top line by the screen renderer, which calls RenderContent with
// that window.
type ColumnRulerPlugin struct {
	colors     config.ColorScheme
	indicators config.Indicators

	// rtl inverts the ruler for direction=rtl: columns count from the RIGHT
	// edge of the editor area (where an RTL line begins reading), numbers
	// staying digit-readable at their mirrored positions.
	rtl bool
}

// NewColumnRuler creates a new column ruler plugin.
func NewColumnRuler() *ColumnRulerPlugin {
	return &ColumnRulerPlugin{
		colors:     config.NewColorScheme(),
		indicators: config.DefaultIndicators(),
	}
}

// SetColorScheme sets the layered color scheme used to resolve ruler colors.
func (c *ColumnRulerPlugin) SetColorScheme(cs config.ColorScheme) {
	c.colors = cs
}

// rulerColors holds the resolved colors for one ruler render.
type rulerColors struct {
	fill       string // the fill glyph outside the editor area
	tick       string // "." plain columns
	minor      string // ":" every 5th column
	major      string // "|" every 10th column, and decade numbers
	ends       string // first/last (extent) column numbers
	truncation string // left-truncation indicator
	cursor     string // cursor-column highlight (rulerShowsCursor)
	reset      string
}

// SetIndicators sets the ruler glyphs.
func (c *ColumnRulerPlugin) SetIndicators(ind config.Indicators) {
	c.indicators = ind
}

// SetRTL inverts the ruler for right-to-left base direction.
func (c *ColumnRulerPlugin) SetRTL(rtl bool) {
	c.rtl = rtl
}

// firstRune returns the first rune of s, or fallback if s is empty.
func firstRune(s string, fallback rune) rune {
	for _, r := range s {
		return r
	}
	return fallback
}

// RenderContent renders the column ruler line for the given window, aligned
// to that window's own margins, line-number gutter, and horizontal scroll.
// cursorCols are 1-based SCREEN columns to highlight with the rulerCursor
// color (the caret and its ghost/secondary companions); nil when
// rulerShowsCursor is off.
func (c *ColumnRulerPlugin) RenderContent(w *window.Window, screenWidth int, cursorCols []int) string {
	if w == nil {
		return strings.Repeat(" ", screenWidth)
	}

	// Resolve ruler colors through the window's class/type cascade.
	col := func(name string) string {
		return c.colors.Resolve(w.Class, w.Type.Name(), name)
	}
	rc := rulerColors{
		fill:       col("rulerFill"),
		tick:       col("rulerTick"),
		minor:      col("rulerMinor"),
		major:      col("rulerMajor"),
		ends:       col("rulerEnds"),
		truncation: col("truncation"),
		cursor:     col("rulerCursor"),
		reset:      col("reset"),
	}

	viewOffsetX := w.ViewState.ViewOffsetX

	// Calculate the effective left margin of the window
	lineNumberWidth := 0
	if w.ViewState.ShowLineNumbers {
		lineNumberWidth = w.LineNumWidth
	}
	// Logical margins map to physical sides by direction (Inner = reading
	// start). Under RTL the gutter mirrors to the right, so the editor area
	// begins after the physical left margin and the gutter joins the right
	// reservation.
	physLeft, physRight := w.MarginInner, w.MarginOuter
	if c.rtl {
		physLeft, physRight = w.MarginOuter, w.MarginInner
	}
	effectiveLeftMargin := physLeft + lineNumberWidth
	rightReserved := physRight
	if c.rtl {
		effectiveLeftMargin = physLeft
		rightReserved = physRight + lineNumberWidth
	}

	// Calculate the actual editor area within the screen
	editorAreaWidth := screenWidth - effectiveLeftMargin - rightReserved

	return c.createColumnRuler(effectiveLeftMargin, editorAreaWidth, viewOffsetX, screenWidth, rc, cursorCols)
}

// createColumnRuler creates the column ruler string.
func (c *ColumnRulerPlugin) createColumnRuler(leftMargin, editorWidth, viewOffsetX, screenWidth int, rc rulerColors, cursorCols []int) string {
	// Configurable ruler glyphs (single rune each).
	fillGlyph := firstRune(c.indicators.RulerFill, '░')
	tickGlyph := firstRune(c.indicators.RulerTick, '.')
	minorGlyph := firstRune(c.indicators.RulerMinor, ':')
	majorGlyph := firstRune(c.indicators.RulerMajor, '|')

	// Build ruler as runes first to support Unicode characters, then apply
	// colors. Each cell carries its own resolved color, tracked by POSITION
	// (not by the rendered glyph), so coloring stays correct even if several
	// glyphs are configured the same.
	ruler := make([]rune, screenWidth)
	cellColor := make([]string, screenWidth)
	for i := range ruler {
		ruler[i] = ' '
		cellColor[i] = rc.fill
	}

	// Mark non-editor areas with the fill glyph.
	for i := 0; i < leftMargin && i < screenWidth; i++ {
		ruler[i] = fillGlyph
	}
	for i := leftMargin + editorWidth; i < screenWidth; i++ {
		ruler[i] = fillGlyph
	}

	// Calculate column range (1-based)
	firstEditorCol := viewOffsetX + 1

	// Create base pattern for editor area.
	for screenX := leftMargin; screenX < leftMargin+editorWidth && screenX < screenWidth; screenX++ {
		editorX := screenX - leftMargin
		colNum := firstEditorCol + editorX

		if colNum%10 == 0 {
			ruler[screenX] = majorGlyph
			cellColor[screenX] = rc.major
		} else if colNum%5 == 0 {
			ruler[screenX] = minorGlyph
			cellColor[screenX] = rc.minor
		} else {
			ruler[screenX] = tickGlyph
			cellColor[screenX] = rc.tick
		}
	}

	// Collect number placements
	type placement struct {
		start    int
		end      int
		text     string
		isExtent bool
	}
	var numberPlacements []placement

	// Find first and last numbers to place
	lastEditorCol := viewOffsetX + editorWidth
	firstNumber := c.findFirstDisplayableNumber(firstEditorCol, lastEditorCol)
	lastNumber := c.findLastDisplayableNumber(firstEditorCol, lastEditorCol)

	// overlaps checks if a placement overlaps with existing placements
	overlaps := func(p placement, placements []placement) bool {
		for _, existing := range placements {
			// Check for overlap with 1-char buffer on each side
			if !(p.end+1 < existing.start || p.start-1 > existing.end) {
				return true
			}
		}
		return false
	}

	// Try to place first number
	if firstNumber > 0 {
		if p := c.tryPlaceNumber(firstNumber, leftMargin, viewOffsetX, editorWidth); p != nil {
			numberPlacements = append(numberPlacements, placement{
				start:    p.start,
				end:      p.end,
				text:     p.text,
				isExtent: true,
			})
		}
	}

	// Try to place last number
	if lastNumber > 0 && lastNumber != firstNumber {
		if p := c.tryPlaceNumber(lastNumber, leftMargin, viewOffsetX, editorWidth); p != nil {
			newPlacement := placement{
				start:    p.start,
				end:      p.end,
				text:     p.text,
				isExtent: true,
			}
			if !overlaps(newPlacement, numberPlacements) {
				numberPlacements = append(numberPlacements, newPlacement)
			}
		}
	}

	// Try to place decade markers
	for col := ((firstEditorCol + 9) / 10) * 10; col <= lastEditorCol; col += 10 {
		if col == firstNumber || col == lastNumber {
			continue
		}

		if p := c.tryPlaceNumber(col, leftMargin, viewOffsetX, editorWidth); p != nil {
			newPlacement := placement{
				start:    p.start,
				end:      p.end,
				text:     p.text,
				isExtent: false,
			}
			if !overlaps(newPlacement, numberPlacements) {
				numberPlacements = append(numberPlacements, newPlacement)
			}
		}
	}

	// Apply number placements to ruler. Extent (first/last) numbers use the
	// ends color; decade numbers use the major color.
	for _, p := range numberPlacements {
		numberColor := rc.major
		if p.isExtent {
			numberColor = rc.ends
		}
		for i, r := range p.text {
			if p.start+i < len(ruler) {
				ruler[p.start+i] = r
				cellColor[p.start+i] = numberColor
			}
		}
	}

	// direction=rtl: mirror the editor area of the ruler so column 1 sits at
	// the RIGHT edge (an RTL line's reading start). The cell mirror reverses
	// digit sequences too, so each maximal digit run is re-reversed in place
	// to stay readable at its mirrored position.
	if c.rtl && editorWidth > 0 {
		lo := leftMargin
		hi := leftMargin + editorWidth
		if hi > screenWidth {
			hi = screenWidth
		}
		if lo < 0 {
			lo = 0
		}
		for i, j := lo, hi-1; i < j; i, j = i+1, j-1 {
			ruler[i], ruler[j] = ruler[j], ruler[i]
			cellColor[i], cellColor[j] = cellColor[j], cellColor[i]
		}
		for i := lo; i < hi; {
			if ruler[i] < '0' || ruler[i] > '9' {
				i++
				continue
			}
			j := i
			for j < hi && ruler[j] >= '0' && ruler[j] <= '9' {
				j++
			}
			for a, b := i, j-1; a < b; a, b = a+1, b-1 {
				ruler[a], ruler[b] = ruler[b], ruler[a]
				cellColor[a], cellColor[b] = cellColor[b], cellColor[a]
			}
			i = j
		}
	}

	// When scrolled right, content is truncated off the left on every line, so
	// show the left-truncation indicator once here rather than per line — at the
	// rightmost cell of the left fill area (just before the ruler/content
	// begins), aligned with the content's left edge.
	if viewOffsetX > 0 {
		if c.rtl {
			// Right-anchored view: the scrolled-past reading head is off the
			// RIGHT edge, so the once-only indicator sits in the right fill.
			ri := leftMargin + editorWidth
			if ri >= 0 && ri < screenWidth {
				ruler[ri] = firstRune(c.indicators.TruncationRight, '>')
				cellColor[ri] = rc.truncation
			}
		} else if leftMargin > 0 && leftMargin <= screenWidth {
			li := leftMargin - 1
			ruler[li] = firstRune(c.indicators.TruncationLeft, '<')
			cellColor[li] = rc.truncation
		}
	}

	// Highlight the cursor column(s) last, so the mark lands on the final
	// SCREEN cell (after RTL mirroring) whatever glyph sits there. Only the
	// cell color changes; the ruler glyph underneath is kept.
	if rc.cursor != "" {
		for _, sc := range cursorCols {
			idx := sc - 1 // 1-based screen column -> 0-based cell
			if idx >= 0 && idx < screenWidth {
				cellColor[idx] = rc.cursor
			}
		}
	}

	// Emit the ruler with each cell's resolved color.
	var coloredRuler strings.Builder
	for i, char := range ruler {
		coloredRuler.WriteString(cellColor[i])
		coloredRuler.WriteRune(char)
	}
	coloredRuler.WriteString(rc.reset)

	return coloredRuler.String()
}

// findFirstDisplayableNumber finds the first column number that can be displayed.
func (c *ColumnRulerPlugin) findFirstDisplayableNumber(firstCol, lastCol int) int {
	numStr := fmt.Sprintf("%d", firstCol)
	if len(numStr) <= 4 {
		return firstCol
	}

	firstDecade := ((firstCol + 9) / 10) * 10
	if firstDecade <= lastCol {
		return firstDecade
	}

	return 0
}

// findLastDisplayableNumber finds the last column number that can be displayed.
func (c *ColumnRulerPlugin) findLastDisplayableNumber(firstCol, lastCol int) int {
	numStr := fmt.Sprintf("%d", lastCol)
	if len(numStr) <= 4 {
		return lastCol
	}

	lastDecade := (lastCol / 10) * 10
	if lastDecade >= firstCol && lastDecade > 0 {
		return lastDecade
	}

	return 0
}

// tryPlaceNumber attempts to place a column number at the appropriate position.
func (c *ColumnRulerPlugin) tryPlaceNumber(colNum, leftMargin, viewOffsetX, editorWidth int) *struct {
	start    int
	end      int
	text     string
	isExtent bool
} {
	text := fmt.Sprintf("%d", colNum)
	rightEdge := c.getScreenPositionForColumn(colNum, leftMargin, viewOffsetX)
	leftEdge := rightEdge - len(text) + 1

	// Check bounds
	if leftEdge < leftMargin || rightEdge >= leftMargin+editorWidth {
		return nil
	}

	return &struct {
		start    int
		end      int
		text     string
		isExtent bool
	}{
		start: leftEdge,
		end:   rightEdge,
		text:  text,
	}
}

// getScreenPositionForColumn calculates screen position for a column number.
func (c *ColumnRulerPlugin) getScreenPositionForColumn(colNum, leftMargin, viewOffsetX int) int {
	return leftMargin + (colNum - 1 - viewOffsetX)
}
