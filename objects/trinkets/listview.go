// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"fmt"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// ListItem represents an item in a ListView.
type ListItem struct {
	Text    string
	Icon    *style.TextIcon
	Data    interface{} // User data
	Enabled bool
}

// NewListItem creates a new list item.
func NewListItem(text string) *ListItem {
	return &ListItem{
		Text:    text,
		Enabled: true,
	}
}

// ListView displays a scrollable list of items.
type ListView struct {
	core.TrinketBase
	core.AccessibleTrinket

	items        []*ListItem
	currentIndex int
	scrollOffset int

	// Selection mode
	selectionMode SelectionMode
	selectedItems map[int]bool

	// Appearance
	alternateRowColors bool
	showIcons          bool

	// Mouse state
	isDragging            bool
	scrollbarDragging     bool // Whether scrollbar thumb is being dragged
	scrollbarThumbHovered bool // Whether the pointer is over the thumb
	scrollbarDragStart    int  // Y position where drag started
	scrollbarDragOffset   int  // Scroll offset when drag started

	// Smooth (pixel-surface) scrollbar drag: the thumb follows the
	// pointer at unit granularity while scrollOffset snaps to whole
	// rows. scrollbarGrabOff is where the press landed within the
	// thumb; scrollbarThumbPos is the unsnapped thumb origin.
	smoothScrollbarDrag bool
	scrollbarGrabOff    float64
	scrollbarThumbPos   float64

	// Fractional rows carried between trackpad wheel events.
	wheelAccum float64

	// Callbacks
	onCurrentChanged   func(index int)
	onItemActivated    func(index int)
	onSelectionChanged func()
}

// SelectionMode determines how items can be selected.
type SelectionMode int

const (
	SingleSelection SelectionMode = iota
	MultiSelection
	ExtendedSelection
	NoSelection
)

// NewListView creates a new list view.
func NewListView() *ListView {
	l := &ListView{
		currentIndex:  -1,
		selectionMode: SingleSelection,
		selectedItems: make(map[int]bool),
	}
	l.TrinketBase = *core.NewTrinketBase()
	l.Init(l) // Enable polymorphic focus handling
	l.SetFocusPolicy(core.StrongFocus)
	l.SetAccessibleRole(core.RoleList)
	return l
}

// AddItem adds an item to the list.
func (l *ListView) AddItem(item *ListItem) {
	l.items = append(l.items, item)
	if l.currentIndex < 0 && len(l.items) == 1 {
		l.SetCurrentIndex(0)
	}
	l.Update()
}

// AddTextItem adds a text item to the list.
func (l *ListView) AddTextItem(text string) {
	l.AddItem(NewListItem(text))
}

// InsertItem inserts an item at the given index.
func (l *ListView) InsertItem(index int, item *ListItem) {
	if index < 0 {
		index = 0
	}
	if index > len(l.items) {
		index = len(l.items)
	}

	l.items = append(l.items[:index], append([]*ListItem{item}, l.items[index:]...)...)

	// Adjust selection
	newSelected := make(map[int]bool)
	for idx := range l.selectedItems {
		if idx >= index {
			newSelected[idx+1] = true
		} else {
			newSelected[idx] = true
		}
	}
	l.selectedItems = newSelected

	if l.currentIndex >= index {
		l.currentIndex++
	}
	l.Update()
}

// RemoveItem removes an item at the given index.
func (l *ListView) RemoveItem(index int) {
	if index < 0 || index >= len(l.items) {
		return
	}

	l.items = append(l.items[:index], l.items[index+1:]...)

	// Adjust selection
	newSelected := make(map[int]bool)
	for idx := range l.selectedItems {
		if idx < index {
			newSelected[idx] = true
		} else if idx > index {
			newSelected[idx-1] = true
		}
	}
	l.selectedItems = newSelected

	// Adjust current index
	if l.currentIndex == index {
		if l.currentIndex >= len(l.items) {
			l.currentIndex = len(l.items) - 1
		}
		if l.onCurrentChanged != nil {
			l.onCurrentChanged(l.currentIndex)
		}
	} else if l.currentIndex > index {
		l.currentIndex--
	}
	l.Update()
}

// Clear removes all items.
func (l *ListView) Clear() {
	l.items = nil
	l.currentIndex = -1
	l.scrollOffset = 0
	l.selectedItems = make(map[int]bool)
	l.Update()
}

// Count returns the number of items.
func (l *ListView) Count() int {
	return len(l.items)
}

// Item returns the item at the given index.
func (l *ListView) Item(index int) *ListItem {
	if index < 0 || index >= len(l.items) {
		return nil
	}
	return l.items[index]
}

// Items returns all items.
func (l *ListView) Items() []*ListItem {
	return l.items
}

// CurrentIndex returns the current item index.
func (l *ListView) CurrentIndex() int {
	return l.currentIndex
}

// SetCurrentIndex sets the current item index.
func (l *ListView) SetCurrentIndex(index int) {
	if index < -1 || index >= len(l.items) {
		return
	}
	if l.currentIndex == index {
		return
	}

	l.currentIndex = index
	l.ensureVisible(index)
	l.Update()

	// Notify parent scroll containers to scroll this item into view.
	// This is needed for keyboard navigation when the ListView is inside
	// a ScrollArea and the selected item moves outside the visible area.
	// For mouse clicks, SetFocusWithoutScroll() prevents unwanted scrolling.
	if index >= 0 {
		metrics := l.EffectiveCellMetrics()

		// Calculate the visual Y position of this item (after internal scrolling)
		// This is where the item appears on screen, relative to the ListView's bounds
		visualRow := index - l.scrollOffset
		itemY := core.Unit(visualRow) * metrics.CellHeight

		itemRect := core.UnitRect{
			X:      0,
			Y:      itemY,
			Width:  l.Bounds().Width,
			Height: metrics.CellHeight,
		}
		l.ScrollRectIntoView(itemRect)
	}

	if l.selectionMode == SingleSelection {
		l.selectedItems = make(map[int]bool)
		if index >= 0 {
			l.selectedItems[index] = true
		}
		if l.onSelectionChanged != nil {
			l.onSelectionChanged()
		}
	}

	// Announce selection change for accessibility
	if index >= 0 && index < len(l.items) {
		if am := core.FindAccessibilityManager(l); am != nil {
			item := l.items[index]
			am.AnnouncePolite(fmt.Sprintf("%s, list item, %d of %d", item.Text, index+1, len(l.items)))
		}
	}

	if l.onCurrentChanged != nil {
		l.onCurrentChanged(index)
	}
}

// CurrentItem returns the current item.
func (l *ListView) CurrentItem() *ListItem {
	return l.Item(l.currentIndex)
}

// SelectionMode returns the selection mode.
func (l *ListView) SelectionMode() SelectionMode {
	return l.selectionMode
}

// SetSelectionMode sets the selection mode.
func (l *ListView) SetSelectionMode(mode SelectionMode) {
	l.selectionMode = mode
	if mode == NoSelection {
		l.selectedItems = make(map[int]bool)
	}
	l.Update()
}

// IsSelected returns whether the item at index is selected.
func (l *ListView) IsSelected(index int) bool {
	return l.selectedItems[index]
}

// SetSelected sets the selection state of an item.
func (l *ListView) SetSelected(index int, selected bool) {
	if index < 0 || index >= len(l.items) {
		return
	}
	if l.selectionMode == NoSelection {
		return
	}

	if l.selectionMode == SingleSelection && selected {
		l.selectedItems = make(map[int]bool)
	}

	if selected {
		l.selectedItems[index] = true
	} else {
		delete(l.selectedItems, index)
	}
	l.Update()

	if l.onSelectionChanged != nil {
		l.onSelectionChanged()
	}
}

// SelectedIndexes returns all selected item indexes.
func (l *ListView) SelectedIndexes() []int {
	var result []int
	for idx := range l.selectedItems {
		result = append(result, idx)
	}
	return result
}

// SelectAll selects all items.
func (l *ListView) SelectAll() {
	if l.selectionMode == SingleSelection || l.selectionMode == NoSelection {
		return
	}

	for i := range l.items {
		l.selectedItems[i] = true
	}
	l.Update()

	if l.onSelectionChanged != nil {
		l.onSelectionChanged()
	}
}

// ClearSelection clears all selections.
func (l *ListView) ClearSelection() {
	l.selectedItems = make(map[int]bool)
	l.Update()

	if l.onSelectionChanged != nil {
		l.onSelectionChanged()
	}
}

// SetAlternateRowColors sets whether to use alternate row colors.
func (l *ListView) SetAlternateRowColors(alternate bool) {
	l.alternateRowColors = alternate
	l.Update()
}

// SetLedger turns ledger banding on: non-selected rows alternate the
// scheme's LedgerOdd/LedgerEven colors (1-based: the first row is
// odd). Selection colors are untouched, and the blank area below the
// last item keeps the plain list background.
func (l *ListView) SetLedger(on bool) {
	l.alternateRowColors = on
	l.Update()
}

// SetShowIcons sets whether to show icons.
func (l *ListView) SetShowIcons(show bool) {
	l.showIcons = show
	l.Update()
}

// SetOnCurrentChanged sets the current changed callback.
func (l *ListView) SetOnCurrentChanged(handler func(index int)) {
	l.onCurrentChanged = handler
}

// SetOnItemActivated sets the item activated callback (double-click or Enter).
func (l *ListView) SetOnItemActivated(handler func(index int)) {
	l.onItemActivated = handler
}

// SetOnSelectionChanged sets the selection changed callback.
func (l *ListView) SetOnSelectionChanged(handler func()) {
	l.onSelectionChanged = handler
}

// ensureVisible ensures the given index is visible.
func (l *ListView) ensureVisible(index int) {
	if index < 0 {
		return
	}

	bounds := l.Bounds()
	metrics := l.EffectiveCellMetrics()
	visibleCount := int(bounds.Height / metrics.CellHeight)

	if index < l.scrollOffset {
		l.scrollOffset = index
	} else if index >= l.scrollOffset+visibleCount {
		l.scrollOffset = index - visibleCount + 1
	}
}

// SizeHint returns the preferred size.
func (l *ListView) SizeHint() core.UnitSize {
	metrics := l.EffectiveCellMetrics()
	font := l.EffectiveFont()
	return core.UnitSize{
		Width:  font.MeasureRunes(30),  // Default width for 30 chars
		Height: metrics.TextHeight(10), // 10 items visible
	}
}

// Paint renders the list view.
func (l *ListView) Paint(p *core.Painter) {
	bounds := l.Bounds()
	scheme := l.GetScheme()
	focused := l.HasFocus()
	metrics := l.EffectiveCellMetrics()

	// Draw background using list colors
	bgStyle := style.DefaultStyle().WithFg(scheme.GetListFG()).WithBg(scheme.GetListBG())
	p.FillRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, ' ', bgStyle)

	visibleCount := l.visibleCount() // clamped: never negative

	// Draw items (styles collected for the vertical edge fades).
	rowStyles := make([]style.CellStyle, 0, visibleCount)
	for i := 0; i < visibleCount; i++ {
		itemIndex := l.scrollOffset + i
		if itemIndex >= len(l.items) {
			break
		}

		item := l.items[itemIndex]
		itemY := core.Unit(i) * metrics.CellHeight

		// Determine style
		var s style.CellStyle
		if !item.Enabled {
			s = style.DefaultStyle().WithFg(scheme.GetDisabledTextFG()).WithBg(scheme.GetListBG())
		} else if l.selectedItems[itemIndex] {
			if focused {
				s = scheme.GetFocusedListItem()
			} else {
				s = scheme.GetSelectedListItem()
			}
		} else if l.alternateRowColors {
			// Ledger banding (non-selected rows only), 1-based: the
			// first row is odd.
			if itemIndex%2 == 0 {
				s = scheme.GetLedgerOdd()
			} else {
				s = scheme.GetLedgerEven()
			}
		} else {
			// Unselected items
			s = style.DefaultStyle().WithFg(scheme.GetListFG()).WithBg(scheme.GetListBG())
		}

		rowStyles = append(rowStyles, s)

		// Draw row background
		p.FillRect(core.UnitRect{
			X:      0,
			Y:      itemY,
			Width:  bounds.Width,
			Height: metrics.CellHeight,
		}, ' ', s)

		// Draw current indicator
		x := core.Unit(0)
		if itemIndex == l.currentIndex && focused {
			p.DrawCell(x, itemY, '▸', s)
		}
		x += metrics.CellWidth

		// Draw icon if present
		if l.showIcons && item.Icon != nil {
			// Draw icon (simplified - just first char for now)
			if len(item.Icon.Cells) > 0 {
				cell := item.Icon.Cells[0]
				p.DrawCell(x, itemY, cell.Char, cell.Style)
			}
			x += metrics.CellWidth * 2
		}

		// Draw text using font-aware rendering
		font := l.EffectiveFont()
		availableWidth := bounds.Width - x
		displayText := item.Text
		// Truncate if needed
		for font.MeasureText(displayText) > availableWidth && len(displayText) > 0 {
			displayText = displayText[:len(displayText)-1]
		}
		p.DrawText(x, itemY, displayText, s, font)
	}

	// Vertical edge fades over the content (under the scrollbar).
	l.paintVScrollFades(p, rowStyles, visibleCount)

	// Draw scrollbar if needed
	if len(l.items) > visibleCount {
		l.paintScrollbar(p, visibleCount)
	}
}

// paintVScrollFades fades the top/bottom edges when more items lie
// beyond them (pixel surfaces only), banded so each pixel row blends
// toward the background of the ITEM row under it - selection bar,
// ledger band, or plain - the TreeView's horizontal fade, turned
// vertical. No corner treatment: the list only scrolls one way.
func (l *ListView) paintVScrollFades(p *core.Painter, rowStyles []style.CellStyle, visibleCount int) {
	if !p.Graphical() {
		return
	}
	maxScroll := len(l.items) - visibleCount
	showTop := l.scrollOffset > 0
	showBottom := maxScroll > 0 && l.scrollOffset < maxScroll
	if !showTop && !showBottom {
		return
	}
	bounds := l.Bounds()
	metrics := l.EffectiveCellMetrics()
	wtPx := p.UnitSpanPxY(0, metrics.CellHeight) // one row deep
	if hvPx := p.UnitSpanPxY(0, bounds.Height); wtPx > hvPx/2 {
		wtPx = hvPx / 2
	}
	if wtPx <= 0 {
		return
	}
	wPx := p.UnitSpanPxX(0, bounds.Width)
	rowPx := p.UnitSpanPxY(0, metrics.CellHeight)
	totalPx := p.UnitSpanPxY(0, bounds.Height)
	listBG := l.GetScheme().GetListBG()
	bgAt := func(px int) style.Color {
		if rowPx > 0 {
			if idx := px / rowPx; idx >= 0 && idx < len(rowStyles) {
				return rowStyles[idx].Bg
			}
		}
		return listBG
	}
	alphaAt := func(d int) float64 { return 1.0 - (float64(d)+0.5)/float64(wtPx) }
	for j := 0; j < wtPx; j++ {
		a := alphaAt(j)
		if showTop {
			r, g, b := bgAt(j).RGBComponents()
			p.FillRectPixelsAlpha(0, 0, 0, j, wPx, 1, r, g, b, a)
		}
		if showBottom {
			r, g, b := bgAt(totalPx - 1 - j).RGBComponents()
			p.FillRectPixelsAlpha(0, bounds.Height, 0, -j-1, wPx, 1, r, g, b, a)
		}
	}
}

// scrollbarGeometry returns scrollbar dimensions and thumb position.
// Returns: scrollbarX, thumbStart, thumbHeight, trackHeight (all in rows)
func (l *ListView) scrollbarGeometry(visibleCount int) (scrollbarX core.Unit, thumbStart, thumbHeight, trackHeight int) {
	bounds := l.Bounds()
	metrics := l.EffectiveCellMetrics()
	totalItems := len(l.items)

	scrollbarX = bounds.Width - metrics.CellWidth
	trackHeight = visibleCount

	if totalItems <= visibleCount {
		// No scrolling needed - thumb fills track
		thumbStart = 0
		thumbHeight = trackHeight
		return
	}

	// Calculate thumb height - proportional to visible/total, minimum 1 row
	thumbHeight = visibleCount * visibleCount / totalItems
	if thumbHeight < 1 {
		thumbHeight = 1
	}

	// Calculate thumb position
	// The thumb should only be at position 0 when scrollOffset is 0
	// The thumb should only be at the bottom when scrollOffset is at max
	maxScroll := totalItems - visibleCount
	scrollableTrack := trackHeight - thumbHeight

	if maxScroll > 0 && scrollableTrack > 0 {
		// Map scroll position to thumb position, ensuring extremes are only at extremes
		thumbStart = l.scrollOffset * scrollableTrack / maxScroll

		// Ensure thumb doesn't go to extremes unless scroll is at extremes
		if l.scrollOffset > 0 && thumbStart == 0 {
			thumbStart = 1
		}
		if l.scrollOffset < maxScroll && thumbStart >= scrollableTrack {
			thumbStart = scrollableTrack - 1
		}
	}

	return
}

// scrollbarUnits returns the scrollbar track length, thumb length,
// and thumb origin in units for pixel surfaces - the same proportions
// as scrollbarGeometry without row quantization. Mid-drag the thumb
// origin is the smooth (pointer-tracked) position.
func (l *ListView) scrollbarUnits(visibleCount int) (trackU, thumbU, posU float64) {
	metrics := l.EffectiveCellMetrics()
	trackU = float64(core.Unit(visibleCount) * metrics.CellHeight)
	totalItems := len(l.items)
	if totalItems <= visibleCount || visibleCount <= 0 {
		return trackU, trackU, 0
	}
	thumbU = trackU * float64(visibleCount) / float64(totalItems)
	if thumbU < 8 {
		thumbU = 8
	}
	if thumbU > trackU {
		thumbU = trackU
	}
	scrollable := trackU - thumbU
	maxScroll := totalItems - visibleCount
	if l.scrollbarDragging && l.smoothScrollbarDrag {
		posU = l.scrollbarThumbPos
	} else if maxScroll > 0 {
		posU = float64(l.scrollOffset) * scrollable / float64(maxScroll)
	}
	if posU < 0 {
		posU = 0
	}
	if posU > scrollable {
		posU = scrollable
	}
	return trackU, thumbU, posU
}

// paintScrollbar draws a vertical scrollbar.
func (l *ListView) paintScrollbar(p *core.Painter, visibleCount int) {
	scheme := l.GetScheme()
	metrics := l.EffectiveCellMetrics()
	trackStyle := scheme.GetScrollbar()
	thumbStyle := scheme.GetScrollbarThumbState(l.scrollbarThumbHovered && p.Graphical())

	// Pixel surfaces: a single hairline stripe blended at 50%
	// opacity behind, and one solid full-opacity rectangle for the
	// thumb, at unit granularity - same treatment as the combobox
	// popup lane.
	if p.Graphical() {
		trackU, thumbU, posU := l.scrollbarUnits(visibleCount)
		laneX := l.Bounds().Width - metrics.CellWidth
		stripeX := laneX + metrics.CellWidth/2
		p.FillRect(core.UnitRect{
			X:      stripeX,
			Y:      0,
			Width:  1,
			Height: core.Unit(trackU + 0.5),
		}, '▒', trackStyle.WithBg(style.ColorTransparent))
		p.FillRect(core.UnitRect{
			X:      laneX + 1,
			Y:      core.Unit(posU + 0.5),
			Width:  metrics.CellWidth - 2,
			Height: core.Unit(thumbU + 0.5),
		}, ' ', thumbStyle.WithBg(thumbStyle.Fg))
		return
	}

	scrollbarX, thumbStart, thumbHeight, trackHeight := l.scrollbarGeometry(visibleCount)

	// Draw scrollbar track
	for i := 0; i < trackHeight; i++ {
		y := core.Unit(i) * metrics.CellHeight
		p.DrawCell(scrollbarX, y, '│', trackStyle)
	}

	// Draw scrollbar thumb
	for i := 0; i < thumbHeight; i++ {
		y := core.Unit(thumbStart+i) * metrics.CellHeight
		p.DrawCell(scrollbarX, y, '█', thumbStyle)
	}
}

// HandleKeyPress handles keyboard input.
func (l *ListView) HandleKeyPress(event core.KeyPressEvent) bool {
	switch event.Key {
	case "Up":
		if l.currentIndex > 0 {
			l.SetCurrentIndex(l.currentIndex - 1)
		}
		return true

	case "M-Up", "C-Up", "A-Up":
		// Jump by 5 items, scrolling to maintain relative position
		if l.currentIndex > 0 {
			delta := 5
			newIndex := l.currentIndex - delta
			if newIndex < 0 {
				newIndex = 0
			}
			actualDelta := l.currentIndex - newIndex
			// Scroll by same amount to maintain relative position
			newScroll := l.scrollOffset - actualDelta
			if newScroll < 0 {
				newScroll = 0
			}
			l.scrollOffset = newScroll
			l.SetCurrentIndex(newIndex)
		}
		return true

	case "Down":
		if l.currentIndex < len(l.items)-1 {
			l.SetCurrentIndex(l.currentIndex + 1)
		}
		return true

	case "M-Down", "C-Down", "A-Down":
		// Jump by 5 items, scrolling to maintain relative position
		if l.currentIndex < len(l.items)-1 {
			delta := 5
			newIndex := l.currentIndex + delta
			if newIndex >= len(l.items) {
				newIndex = len(l.items) - 1
			}
			actualDelta := newIndex - l.currentIndex
			// Scroll by same amount to maintain relative position
			visibleCount := l.visibleCount()
			maxScroll := len(l.items) - visibleCount
			if maxScroll < 0 {
				maxScroll = 0
			}
			newScroll := l.scrollOffset + actualDelta
			if newScroll > maxScroll {
				newScroll = maxScroll
			}
			l.scrollOffset = newScroll
			l.SetCurrentIndex(newIndex)
		}
		return true

	case "Home":
		if len(l.items) > 0 {
			l.SetCurrentIndex(0)
		}
		return true

	case "End":
		if len(l.items) > 0 {
			l.SetCurrentIndex(len(l.items) - 1)
		}
		return true

	case "PageUp":
		bounds := l.Bounds()
		metrics := l.EffectiveCellMetrics()
		pageSize := int(bounds.Height / metrics.CellHeight)
		newIndex := l.currentIndex - pageSize
		if newIndex < 0 {
			newIndex = 0
		}
		l.SetCurrentIndex(newIndex)
		return true

	case "PageDown":
		bounds := l.Bounds()
		metrics := l.EffectiveCellMetrics()
		pageSize := int(bounds.Height / metrics.CellHeight)
		newIndex := l.currentIndex + pageSize
		if newIndex >= len(l.items) {
			newIndex = len(l.items) - 1
		}
		l.SetCurrentIndex(newIndex)
		return true

	case "Enter", " ", "Space":
		if l.currentIndex >= 0 && l.onItemActivated != nil {
			l.onItemActivated(l.currentIndex)
		}
		return true

	case "M-a":
		l.SelectAll()
		return true
	}

	return false
}

// visibleCount returns the number of visible rows.
// visibleCount is never negative: a layout squeeze below one row means
// zero visible items, not a negative count.
func (l *ListView) visibleCount() int {
	bounds := l.Bounds()
	metrics := l.EffectiveCellMetrics()
	n := int(bounds.Height / metrics.CellHeight)
	if n < 0 {
		n = 0
	}
	return n
}

// SetBounds resizes the list and re-clamps its scroll offset (the
// embedded base cannot dispatch HandleResize to us - the ScrollArea
// override pattern).
func (l *ListView) SetBounds(bounds core.UnitRect) {
	old := l.Bounds().Size()
	l.TrinketBase.SetBounds(bounds)
	if old != bounds.Size() {
		l.HandleResize(old, bounds.Size())
	}
}

// HandleResize re-clamps the scroll offset: growing the view while
// scrolled down must pull the content back into the freed space
// rather than strand a blank tail behind the vanished scrollbar.
func (l *ListView) HandleResize(oldSize, newSize core.UnitSize) {
	maxScroll := len(l.items) - l.visibleCount()
	if maxScroll < 0 {
		maxScroll = 0
	}
	if l.scrollOffset > maxScroll {
		l.scrollOffset = maxScroll
	}
	if l.scrollOffset < 0 {
		l.scrollOffset = 0
	}
	l.Update()
}

// HandleMousePress handles mouse clicks.
func (l *ListView) HandleMousePress(event core.MousePressEvent) bool {
	if event.Button != core.LeftButton {
		return false
	}

	// Clear any stale drag state from previous incomplete drags
	l.isDragging = false
	l.scrollbarDragging = false

	bounds := l.Bounds()

	// Check if click is within our bounds
	if event.X < 0 || event.Y < 0 || event.X >= bounds.Width || event.Y >= bounds.Height {
		return false
	}

	l.SetFocusWithoutScroll() // Use without-scroll variant since click proves visibility
	metrics := l.EffectiveCellMetrics()

	// Check if click is on scrollbar
	scrollbarX, thumbStart, thumbHeight, _ := l.scrollbarGeometry(l.visibleCount())
	if event.X >= scrollbarX && len(l.items) > l.visibleCount() {
		clickedRow := int(event.Y / metrics.CellHeight)

		// Pixel surfaces anchor the drag to the grab point within
		// the unit-granular thumb.
		if core.FindSmoothPositioning(l.Self()) {
			_, thumbU, posU := l.scrollbarUnits(l.visibleCount())
			pos := float64(event.Y)
			if pos >= posU && pos < posU+thumbU {
				l.scrollbarDragging = true
				l.smoothScrollbarDrag = true
				l.isDragging = false
				l.scrollbarGrabOff = pos - posU
				l.scrollbarThumbPos = posU
				return true
			}
			// Track click falls through to page up/down below,
			// keyed off the smooth thumb position.
			if pos < posU {
				clickedRow = thumbStart - 1
			} else {
				clickedRow = thumbStart + thumbHeight
			}
		}

		// Check if on thumb
		if clickedRow >= thumbStart && clickedRow < thumbStart+thumbHeight {
			// Start scrollbar drag - clear content drag flag
			l.scrollbarDragging = true
			l.isDragging = false
			l.scrollbarDragStart = clickedRow
			l.scrollbarDragOffset = l.scrollOffset
			return true
		}

		// Click on track - page up or page down
		visibleCount := l.visibleCount()
		if clickedRow < thumbStart {
			// Page up
			l.scrollOffset -= visibleCount
			if l.scrollOffset < 0 {
				l.scrollOffset = 0
			}
		} else {
			// Page down
			maxScroll := len(l.items) - visibleCount
			l.scrollOffset += visibleCount
			if l.scrollOffset > maxScroll {
				l.scrollOffset = maxScroll
			}
		}
		l.Update()
		return true
	}

	// Click on list content (before scrollbar)
	if event.X >= scrollbarX {
		return false // Click is past the content area
	}

	// Calculate which item was clicked
	clickedRow := int(event.Y / metrics.CellHeight)
	clickedIndex := l.scrollOffset + clickedRow

	// Only start content drag if click is on a valid item
	contentWidth := bounds.Width - metrics.CellWidth
	if event.X >= 0 && event.X < contentWidth && clickedIndex >= 0 && clickedIndex < len(l.items) {
		// Start content drag - clear scrollbar drag flag
		l.isDragging = true
		l.scrollbarDragging = false
		l.SetCurrentIndex(clickedIndex)
		return true
	}

	// Click is in content area but not on a valid item
	return false
}

// overScrollbarThumb reports whether a widget-local point lies on the
// vertical scrollbar thumb.
func (l *ListView) overScrollbarThumb(x, y core.Unit) bool {
	visibleCount := l.visibleCount()
	if len(l.items) <= visibleCount {
		return false
	}
	bounds := l.Bounds()
	if x < 0 || y < 0 || x >= bounds.Width || y >= bounds.Height {
		return false
	}
	scrollbarX, thumbStart, thumbHeight, _ := l.scrollbarGeometry(visibleCount)
	if x < scrollbarX {
		return false
	}
	if core.FindSmoothPositioning(l.Self()) {
		_, thumbU, posU := l.scrollbarUnits(visibleCount)
		pos := float64(y)
		return pos >= posU && pos < posU+thumbU
	}
	row := int(y / l.EffectiveCellMetrics().CellHeight)
	return row >= thumbStart && row < thumbStart+thumbHeight
}

// HandleMouseMove handles mouse drag to sweep selection.
func (l *ListView) HandleMouseMove(event core.MouseMoveEvent) bool {
	// Track scrollbar-thumb hover regardless of focus/drag state. The
	// thumb stays lit while a drag is in progress even if the pointer
	// slips off it.
	// Hover is a no-button affordance: while a button is held (a drag begun
	// elsewhere passing over) don't light the thumb - unless this list owns
	// the scrollbar drag.
	if over := l.scrollbarDragging || (event.Buttons == 0 && l.overScrollbarThumb(event.X, event.Y)); over != l.scrollbarThumbHovered {
		l.scrollbarThumbHovered = over
		l.Update()
	}

	// If we don't have focus, we shouldn't be processing drags
	// (another trinket got the click and we have stale drag state)
	if !l.HasFocus() {
		l.isDragging = false
		l.scrollbarDragging = false
		return false
	}

	metrics := l.EffectiveCellMetrics()

	// Handle scrollbar thumb drag
	// Note: Once drag is captured on press, we don't check horizontal bounds during drag
	if l.scrollbarDragging {
		// Smooth drag: the thumb follows the pointer in units, the
		// scroll offset snaps to the nearest whole row.
		if l.smoothScrollbarDrag {
			visibleCount := l.visibleCount()
			trackU, thumbU, _ := l.scrollbarUnits(visibleCount)
			scrollable := trackU - thumbU
			newPos := float64(event.Y) - l.scrollbarGrabOff
			if newPos < 0 {
				newPos = 0
			}
			if newPos > scrollable {
				newPos = scrollable
			}
			l.scrollbarThumbPos = newPos
			maxScroll := len(l.items) - visibleCount
			newOffset := 0
			if scrollable > 0 && maxScroll > 0 {
				newOffset = int(newPos*float64(maxScroll)/scrollable + 0.5)
			}
			l.scrollOffset = newOffset
			// The thumb moves even when the snapped offset does not.
			l.Update()
			return true
		}

		currentRow := int(event.Y / metrics.CellHeight)
		rowDelta := currentRow - l.scrollbarDragStart

		visibleCount := l.visibleCount()
		totalItems := len(l.items)
		maxScroll := totalItems - visibleCount

		if maxScroll > 0 {
			_, _, thumbHeight, trackHeight := l.scrollbarGeometry(visibleCount)
			scrollableTrack := trackHeight - thumbHeight

			if scrollableTrack > 0 {
				// Convert row delta to scroll offset delta
				scrollDelta := rowDelta * maxScroll / scrollableTrack
				newOffset := l.scrollbarDragOffset + scrollDelta

				// Clamp
				if newOffset < 0 {
					newOffset = 0
				} else if newOffset > maxScroll {
					newOffset = maxScroll
				}

				if newOffset != l.scrollOffset {
					l.scrollOffset = newOffset
					l.Update()
				}
			}
		}
		return true
	}

	// Handle list item drag
	// Note: Once drag is captured on press, we don't check horizontal bounds during drag
	if !l.isDragging {
		return false
	}

	row := int(event.Y / metrics.CellHeight)
	index := l.scrollOffset + row

	// Clamp to valid range
	if index < 0 {
		index = 0
	} else if index >= len(l.items) {
		index = len(l.items) - 1
	}

	if index >= 0 && index != l.currentIndex {
		l.SetCurrentIndex(index)
	}

	return true
}

// HandleMouseRelease handles mouse release.
// Only consumes the event when a drag was actually in progress:
// containers broadcast releases to every child, so an unconditional
// true here would starve sibling trinkets of their release.
func (l *ListView) HandleMouseRelease(event core.MouseReleaseEvent) bool {
	if l.isDragging || l.scrollbarDragging {
		l.isDragging = false
		l.scrollbarDragging = false
		l.smoothScrollbarDrag = false
		l.Update()
		return true
	}
	return false
}

// HandleMouseWheel handles mouse wheel scrolling.
func (l *ListView) HandleMouseWheel(event core.MouseWheelEvent) bool {
	if len(l.items) == 0 {
		return false
	}

	visibleCount := l.visibleCount()
	maxScroll := len(l.items) - visibleCount
	if maxScroll <= 0 {
		return false
	}

	// 3 rows per notch; trackpad precise deltas accumulate so slow
	// two-finger pans still move one row at a time.
	rows := 0
	if event.PreciseY != 0 {
		l.wheelAccum += event.PreciseY * 3
		rows = int(l.wheelAccum)
		l.wheelAccum -= float64(rows)
	} else if event.DeltaY < 0 {
		rows = -3
	} else if event.DeltaY > 0 {
		rows = 3
	}
	l.scrollOffset += rows
	if l.scrollOffset < 0 {
		l.scrollOffset = 0
	}
	if l.scrollOffset > maxScroll {
		l.scrollOffset = maxScroll
	}

	core.ClaimWheelGesture(event, l.HandleMouseWheel)
	l.Update()
	return true
}

// HandleFocusIn is called when focus is gained.
func (l *ListView) HandleFocusIn() {
	// Auto-select first item if nothing is selected
	if l.currentIndex < 0 && len(l.items) > 0 {
		l.SetCurrentIndex(0)
	}
	l.Update()
}

// HandleFocusOut is called when focus is lost.
func (l *ListView) HandleFocusOut() {
	// Clear any active drag state when focus is lost
	l.isDragging = false
	l.scrollbarDragging = false
	l.Update()
}

// AccessibleInfo returns accessibility information.
func (l *ListView) AccessibleInfo() core.AccessibleInfo {
	info := l.AccessibleTrinket.AccessibleInfo()
	info.Role = core.RoleList
	info.SetSize = len(l.items)

	if l.currentIndex >= 0 {
		info.PositionInSet = l.currentIndex + 1
		if l.currentIndex < len(l.items) {
			info.Value = l.items[l.currentIndex].Text
		}
	}

	if l.selectionMode == MultiSelection || l.selectionMode == ExtendedSelection {
		info.State |= core.StateMultiSelectable
	}

	if !l.IsEnabled() {
		info.State |= core.StateDisabled
	}

	return info
}
