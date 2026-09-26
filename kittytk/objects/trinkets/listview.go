// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"fmt"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
	"github.com/phroun/serval"
)

// ListItem represents an item in a ListView.
type ListItem struct {
	Text string

	// Icon is the NAME of a registered icon (style.RegisterIcon), not a
	// picture. A name nothing has registered draws nothing.
	Icon string

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
	core.TrinketKeys
	core.AccessibleTrinket

	// What this view was told was wrong, and whether it says so itself. See
	// trouble.go.
	troubled

	// The rows this list was given, in the order it was given them. A list told
	// to read a source of its own has none of these and reads that instead; see
	// listsource.go, which is the one mechanism both go through.
	items []*ListItem

	// Where the rows come from, and what is known about where they are.
	// source is a DECLARED one and nil for a list reading its own items; made
	// is the one built out of those items. Two fields rather than one, because
	// a made source assigned over the declared one would then look declared --
	// and a list would stop noticing that its own items had changed.
	source     serval.Source
	made       serval.Source
	set        serval.DataSet
	descriptor serval.DataSetDescriptor
	bones      spine
	restate    bool // a made source is out of date
	// arrivals counts the sources this view has been pointed at, so a
	// subscription taken against one it has left knows to do nothing. See
	// arrival.go.
	arrivals int
	asks     asking // which ask is current; see listdrag.go

	// walks says this sequence will not jump to a POSITION, so the only question it
	// answers is one carrying on from a record.
	//
	// **Told rather than assumed.** It was asked to begin at a position and said it
	// began somewhere else, which is what a source walking its own body does -- there
	// being no index into a sequence somebody else named. An application that honours
	// `from` is never found out this way and is never made to walk.
	//
	// It only ever becomes true. A source that could not skip once will not learn to,
	// and a sequence stated afresh is a fresh spine and a fresh question. See extent.
	walks      bool
	fromSource map[string]*ListItem // rows a source named, by identity

	// Which field a row SHOWS and which it means, empty for the ones a made
	// source writes. They are one field until somebody says otherwise; see
	// SetFields.
	displayField string
	valueField   string
	values       map[string]*serval.Value // what a row means, by identity

	// The current row, as both of the things it is. A blank row is selected by
	// POSITION with no identity yet -- which keyboard scrolling requires, since
	// moving the selection and scrolling are one motion -- and becomes selected
	// by identity when the record arrives.
	currentIndex int
	currentID    *serval.Value

	scrollOffset int

	// Selection mode, and which rows are chosen -- by IDENTITY, because a
	// position means nothing once the rows move. See listchoice.go.
	selectionMode SelectionMode
	chosen        selection

	// Appearance
	ledger    bool
	showIcons bool

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
	}
	l.TrinketBase = *core.NewTrinketBase()
	l.SetCommands(
		core.CmdTrinketItemPrior, core.CmdTrinketItemUp,
		core.CmdTrinketItemNext, core.CmdTrinketItemDown,
		core.CmdTrinketScrollUp, core.CmdTrinketScrollDown,
		core.CmdTrinketPagePrior, core.CmdTrinketPageNext,
		core.CmdTrinketBeg, core.CmdTrinketEnd,
		core.CmdTrinketActivate, core.CmdTrinketSelectAll,
	)
	l.Init(l) // Enable polymorphic focus handling
	l.SetFocusPolicy(core.StrongFocus)
	l.SetAccessibleRole(core.RoleList)
	// A cut cell reads on in place: its tooltip stands exactly where the
	// text is and runs past the boundary that cut it.
	l.SetTooltipSide(core.TooltipOver)
	return l
}

// AddItem adds an item to the list.
func (l *ListView) AddItem(item *ListItem) {
	l.items = append(l.items, item)
	l.touched()
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
	l.touched()

	// A made source keys its rows BY POSITION, so an insert rewrites the keys
	// after it and what is chosen has to move with them. A declared source's
	// keys are its own and are not positions, so nothing renumbers them.
	if l.source == nil {
		l.chosen.shift(index, 1)
	}

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
	l.touched()

	// The removed row's key goes, and the keys after it come down one -- a made
	// source keying its rows by position. A declared source's keys are its own.
	if l.source == nil {
		l.chosen.drop(serval.NewInt(int64(index)))
		l.chosen.shift(index+1, -1)
	}

	// Adjust current index
	if l.currentIndex == index {
		if l.currentIndex >= l.Count() {
			l.currentIndex = l.Count() - 1
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
	l.touched()
	l.setCurrent(-1, nil)
	l.scrollOffset = 0
	l.chosen.clear()
	l.Update()
}

// Count is how many rows the list has.
//
// It asks the sequence rather than counting a slice, so a list reading a source
// answers the same way a list holding its own items does. Stating a sequence
// counts it, which is why this costs nothing to ask repeatedly.
//
// A source that will not count itself leaves this as far as anything has been
// placed, which is what there is to draw and is the honest figure.
func (l *ListView) Count() int {
	set := l.sequence()
	if set == nil {
		return 0
	}
	l.bones.learn(serval.Complete{Total: serval.CountOf(set)})
	return l.bones.rows()
}

// Item is the row at a position, and nil for one off either end.
//
// Nil is also what a BLANK row answers -- one the list knows is there and knows
// nothing else about yet, which is every row of a source-backed list until the
// answer arrives. A caller drawing rows should ask for the stretch it wants
// first; a caller asking one at a time gets one question per row.
func (l *ListView) Item(index int) *ListItem {
	if index < 0 || index >= l.Count() {
		return nil
	}
	l.extent(index, 1)
	return l.rowAt(index)
}

// Items is the items this list was GIVEN, which for a list reading a source of
// its own is none of them. A source's rows are not items and have no existence
// here beyond the ones the list has been told about.
func (l *ListView) Items() []*ListItem {
	return l.items
}

// CurrentIndex returns the current item index.
func (l *ListView) CurrentIndex() int {
	return l.currentIndex
}

// SetCurrentIndex sets the current item index.
func (l *ListView) SetCurrentIndex(index int) {
	if index < -1 || index >= l.Count() {
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
		itemY := l.rowsTop() + core.Unit(visualRow)*metrics.UnitsPerCellHeight

		itemRect := core.UnitRect{
			X:      0,
			Y:      itemY,
			Width:  l.Bounds().Width,
			Height: metrics.UnitsPerCellHeight,
		}
		l.ScrollRectIntoView(itemRect)
	}

	if l.selectionMode == SingleSelection {
		// The row is read first, because choosing one means NAMING it. Without
		// this the selection quietly does not record: the current row moves, and
		// nothing is chosen, because the list had not yet been told what stands
		// there.
		//
		// A row that still cannot be named -- a source that has not answered --
		// leaves the selection empty rather than holding the row before it, and
		// resolve fills it in when the record arrives.
		l.chosen.clear()
		if index >= 0 {
			l.extent(index, 1)
			if id, ok := l.bones.idAt(index); ok {
				l.chosen.only(id)
			}
		}
		if l.onSelectionChanged != nil {
			l.onSelectionChanged()
		}
	}

	// Announce selection change for accessibility
	if index >= 0 && index < l.Count() {
		if am := core.FindAccessibilityManager(l); am != nil {
			// A blank row is announced as its place and nothing else: it is a
			// row that is there, and saying so is truer than saying nothing
			// while a screen reader waits for the record to arrive.
			said := ""
			if item := l.Item(index); item != nil {
				said = item.Text + ", "
			}
			am.AnnouncePolite(fmt.Sprintf("%slist item, %d of %d", said, index+1, l.Count()))
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
		l.chosen.clear()
	}
	l.Update()
}

// SetLedger turns ledger banding on: non-selected rows alternate the
// scheme's LedgerOdd/LedgerEven colors (1-based: the first row is
// odd). Selection colors are untouched, and the blank area below the
// last item keeps the plain list background.
func (l *ListView) SetLedger(on bool) {
	l.ledger = on
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

	visibleCount := l.visibleCount()

	// A list with no room yet shows nothing, so there is nothing to bring into
	// view. Scrolling by the arithmetic below instead puts the list one row
	// down before it has been laid out -- which is where a list built by a
	// script starts, since the first item added makes itself current.
	if visibleCount <= 0 {
		return
	}

	if index < l.scrollOffset {
		l.scrollOffset = index
	} else if index >= l.scrollOffset+visibleCount {
		l.scrollOffset = index - visibleCount + 1
	}
}

// SizeHint returns the size a list asks for when nothing sets one (see
// defaultSizeCells).
func (l *ListView) SizeHint() core.UnitSize {
	metrics := l.EffectiveCellMetrics()
	return core.UnitSize{
		Width:  metrics.UnitsPerCellWidth * defaultSizeCells,
		Height: metrics.UnitsPerCellHeight * defaultSizeCells,
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

	// One question for the whole screenful, asked before anything is drawn OR
	// MEASURED. Asking per row would be a question per row, and a source that has
	// to go and find out would be asked thirty times for one frame.
	//
	// Before the measuring, because the answer can change what there is to measure:
	// a refusal takes a row out of the rows' own area, and a frame that measured
	// first would draw itself as though nothing had been refused. The screenful it
	// asks for is a row out either way, which is what a screenful is.
	l.ask(l.scrollOffset, l.visibleCount())

	visibleCount := l.visibleCount() // clamped: never negative

	// A refusal is drawn FIRST and takes its row out of what the rows below have:
	// it is not an overlay, it stands where a row would have stood. See trouble.go.
	top := l.rowsTop()
	if top > 0 {
		paintTrouble(p, &l.TrinketBase, scheme,
			troubleRow(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, top),
			l.trouble.Reason)
	}

	// Draw items (styles collected for the vertical edge fades).
	rowStyles := make([]style.CellStyle, 0, visibleCount)
	for i := 0; i < visibleCount; i++ {
		itemIndex := l.scrollOffset + i
		if itemIndex >= l.Count() {
			break
		}

		// A row the list cannot name yet is drawn BLANK -- its place, its
		// banding, its selection bar, and no text. That is what keeps a drag
		// smooth: the rows are where they will be, and the words catch up.
		item := l.rowAt(itemIndex)
		if item == nil {
			item = blankRow
		}
		itemY := top + core.Unit(i)*metrics.UnitsPerCellHeight

		// Determine style
		var s style.CellStyle
		if !item.Enabled {
			s = style.DefaultStyle().WithFg(scheme.GetDisabledTextFG()).WithBg(scheme.GetListBG())
		} else if l.IsSelected(itemIndex) {
			if focused {
				s = scheme.GetFocusedListItem()
			} else {
				s = scheme.GetSelectedListItem()
			}
		} else if l.ledger {
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
			Height: metrics.UnitsPerCellHeight,
		}, ' ', s)

		// The row's chrome reads from the LIST's leading edge: the current
		// item's arrow, then the icon, then the text. The arrow points into
		// the row, so it turns over with the row.
		arrow := '▸'
		if core.ChromeMirrored(l) {
			arrow = '◂'
		}

		// Draw current indicator
		x := core.Unit(0)
		if itemIndex == l.currentIndex && focused {
			p.DrawCell(core.LeadingX(l, bounds.Width, x, metrics.UnitsPerCellWidth), itemY, arrow, s)
		}
		x += metrics.UnitsPerCellWidth

		// Draw icon if present
		if l.showIcons && item.Icon != "" {
			// Draw icon (simplified - just first char for now)
			if icon, ok := style.IconText(item.Icon, style.IconSmall); ok && len(icon.Cells) > 0 {
				cell := icon.Cells[0]
				p.DrawCell(core.LeadingX(l, bounds.Width, x, metrics.UnitsPerCellWidth*2), itemY, cell.Char, cell.Style)
			}
			x += metrics.UnitsPerCellWidth * 2
		}
		_ = x

		// Draw text, ellipsized to the room left beside the indicator, the
		// icon and the scrollbar's own column -- through the same function the
		// tree cuts its cells with, rather than a second way of doing it here.
		font := l.EffectiveFont()
		availableWidth := bounds.Width - x
		if l.showsScrollbar() {
			availableWidth -= metrics.UnitsPerCellWidth
		}
		if availableWidth < 0 {
			availableWidth = 0
		}
		// Cut to fit first, prepared for the cell target after: what is
		// trimmed is the text, and what is drawn is the run made from what is
		// left of it.
		// The row's own mode says where the cut goes, and a list told not to
		// elide draws the whole row and lets the surface clip it.
		cut, _ := l.ElideText(item.Text, availableWidth)
		shown := l.CellRun(cut)

		// The room is the list's; where the text sits IN it is the item's own
		// (see itemTextSide), so a Hebrew name and an English one in the same
		// list each start on the side its script begins on.
		textX := core.LeadingX(l, bounds.Width, x, availableWidth)
		if l.itemTextSide(item) == core.SideRight {
			textX += availableWidth - l.MeasureText(shown)
		}
		p.DrawText(textX, itemY, shown, s, font)
	}

	// Vertical edge fades over the content (under the scrollbar).
	l.paintVScrollFades(p, rowStyles, visibleCount)

	// Draw scrollbar if needed
	if l.Count() > visibleCount {
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
	maxScroll := l.Count() - visibleCount
	showTop := l.scrollOffset > 0
	showBottom := maxScroll > 0 && l.scrollOffset < maxScroll
	if !showTop && !showBottom {
		return
	}
	bounds := l.Bounds()
	metrics := l.EffectiveCellMetrics()
	// The fade belongs to the ROWS' area: a refusal line above them is not an edge
	// there is more list beyond, so it is not faded into.
	top := l.rowsTop()
	rowsHeight := bounds.Height - top
	wtPx := p.UnitSpanPxY(0, metrics.UnitsPerCellHeight) // one row deep
	if hvPx := p.UnitSpanPxY(0, rowsHeight); wtPx > hvPx/2 {
		wtPx = hvPx / 2
	}
	if wtPx <= 0 {
		return
	}
	wPx := p.UnitSpanPxX(0, bounds.Width)
	rowPx := p.UnitSpanPxY(0, metrics.UnitsPerCellHeight)
	totalPx := p.UnitSpanPxY(0, rowsHeight)
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
			p.FillRectPixelsAlpha(0, top, 0, j, wPx, 1, r, g, b, a)
		}
		if showBottom {
			r, g, b := bgAt(totalPx - 1 - j).RGBComponents()
			p.FillRectPixelsAlpha(0, bounds.Height, 0, -j-1, wPx, 1, r, g, b, a)
		}
	}
}

// laneX is where the scrollbar's column sits: the TRAILING edge of the list,
// which is the right of one that reads left to right and the left of one that
// reads the other way.
func (l *ListView) laneX() core.Unit {
	w := l.Bounds().Width
	lane := l.EffectiveCellMetrics().UnitsPerCellWidth
	return core.LeadingX(l, w, w-lane, lane)
}

// onLane reports whether a list-local x is in that column. The lane is one
// column wherever it sits, so what puts a press on it is being IN the column
// rather than past its near edge.
func (l *ListView) onLane(x core.Unit) bool {
	at := l.laneX()
	return x >= at && x < at+l.EffectiveCellMetrics().UnitsPerCellWidth
}

// showsScrollbar reports whether there is a bar to hit at all.
func (l *ListView) showsScrollbar() bool {
	return l.Count() > l.visibleCount()
}

// itemTextSide is where one item's text begins inside the room it is given.
//
// An item's text follows its OWN language, not the list's: a list of names may
// hold Hebrew and English together, and each reads from the side its own script
// begins on. A string with nothing strongly directional in it -- a number, a
// file size, a date -- has no opinion and takes the list's direction, so a
// column of figures still lines up with everything around it.
func (l *ListView) itemTextSide(item *ListItem) core.HSide {
	dir, _ := textDirectionOf(core.DirInherit, item.Text)
	return core.ResolveHAlign(core.AlignTextNatural, dir, core.FindEffectiveDirection(l))
}

// scrollbarGeometry returns scrollbar dimensions and thumb position.
// Returns: scrollbarX, thumbStart, thumbHeight, trackHeight (all in rows)
func (l *ListView) scrollbarGeometry(visibleCount int) (scrollbarX core.Unit, thumbStart, thumbHeight, trackHeight int) {
	totalItems := l.Count()

	scrollbarX = l.laneX()
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
		// A thumb resting at the foot of its track says THIS IS THE END OF THE
		// SEQUENCE, and a length that is only a floor cannot say that: there is
		// more below than anybody has counted. Keeping it a row short is a small
		// true signal in place of a confident false one.
		if l.thumbFloor() && thumbStart >= scrollableTrack {
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
	trackU = float64(core.Unit(visibleCount) * metrics.UnitsPerCellHeight)
	totalItems := l.Count()
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
	if l.thumbFloor() && posU >= scrollable && scrollable > 0 {
		// The same on a pixel surface: a floor does not reach the bottom.
		posU = scrollable - 1
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
	thumbStyle := scheme.GetScrollbarThumbState(false, l.scrollbarThumbHovered && p.Graphical())

	// Pixel surfaces: a single hairline stripe blended at 50%
	// opacity behind, and one solid full-opacity rectangle for the
	// thumb, at unit granularity - same treatment as the combobox
	// popup lane.
	// The bar runs beside the ROWS, so it begins where they do.
	top := l.rowsTop()

	if p.Graphical() {
		trackU, thumbU, posU := l.scrollbarUnits(visibleCount)
		laneX := l.laneX()
		stripeX := laneX + metrics.UnitsPerCellWidth/2
		p.FillRect(core.UnitRect{
			X:      stripeX,
			Y:      top,
			Width:  1,
			Height: core.Unit(trackU + 0.5),
		}, '▒', trackStyle.WithBg(style.ColorTransparent))
		p.FillRect(core.UnitRect{
			X:      laneX + 1,
			Y:      top + core.Unit(posU+0.5),
			Width:  metrics.UnitsPerCellWidth - 2,
			Height: core.Unit(thumbU + 0.5),
		}, ' ', thumbStyle.WithBg(thumbStyle.Fg))
		return
	}

	scrollbarX, thumbStart, thumbHeight, trackHeight := l.scrollbarGeometry(visibleCount)

	// Draw scrollbar track
	for i := 0; i < trackHeight; i++ {
		y := top + core.Unit(i)*metrics.UnitsPerCellHeight
		p.DrawCell(scrollbarX, y, '│', trackStyle)
	}

	// Draw scrollbar thumb
	for i := 0; i < thumbHeight; i++ {
		y := top + core.Unit(thumbStart+i)*metrics.UnitsPerCellHeight
		p.DrawCell(scrollbarX, y, '█', thumbStyle)
	}
}

// HandleKeyPress handles keyboard input.
func (l *ListView) HandleKeyPress(event core.KeyPressEvent) bool {
	switch l.KeyCommand(event.Key) {
	case core.CmdTrinketItemPrior, core.CmdTrinketItemUp:
		if l.currentIndex > 0 {
			l.SetCurrentIndex(l.currentIndex - 1)
		}
		return true

	case core.CmdTrinketScrollUp:
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

	case core.CmdTrinketItemNext, core.CmdTrinketItemDown:
		if l.currentIndex < l.Count()-1 {
			l.SetCurrentIndex(l.currentIndex + 1)
		}
		return true

	case core.CmdTrinketScrollDown:
		// Jump by 5 items, scrolling to maintain relative position
		if l.currentIndex < l.Count()-1 {
			delta := 5
			newIndex := l.currentIndex + delta
			if newIndex >= l.Count() {
				newIndex = l.Count() - 1
			}
			actualDelta := newIndex - l.currentIndex
			// Scroll by same amount to maintain relative position
			visibleCount := l.visibleCount()
			maxScroll := l.Count() - visibleCount
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

	case core.CmdTrinketBeg:
		if l.Count() > 0 {
			l.SetCurrentIndex(0)
		}
		return true

	case core.CmdTrinketEnd:
		if l.Count() > 0 {
			l.SetCurrentIndex(l.Count() - 1)
		}
		return true

	case core.CmdTrinketPagePrior:
		pageSize := l.visibleCount()
		newIndex := l.currentIndex - pageSize
		if newIndex < 0 {
			newIndex = 0
		}
		l.SetCurrentIndex(newIndex)
		return true

	case core.CmdTrinketPageNext:
		pageSize := l.visibleCount()
		newIndex := l.currentIndex + pageSize
		if newIndex >= l.Count() {
			newIndex = l.Count() - 1
		}
		l.SetCurrentIndex(newIndex)
		return true

	case core.CmdTrinketActivate:
		if l.currentIndex >= 0 && l.onItemActivated != nil {
			l.onItemActivated(l.currentIndex)
		}
		return true

	case core.CmdTrinketSelectAll:
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
	n := int((bounds.Height - l.rowsTop()) / metrics.UnitsPerCellHeight)
	if n < 0 {
		n = 0
	}
	return n
}

// rowsTop is where the rows' own area begins: under the refusal line where there is
// one, and at the trinket's own top edge where there is not.
//
// Everything to do with the rows is reckoned from here rather than from the bounds --
// which row a press lands on, where the bar's thumb sits, where a row is painted -- so
// a refusal appearing moves the rows down and takes one off the end, rather than
// covering the first one over.
func (l *ListView) rowsTop() core.Unit {
	return l.troubleHeight(l.EffectiveCellMetrics())
}

// rowUnder is the visible row a list-local y lands on, counted from the rows' own top
// edge, and -1 where it lands on the refusal line above them instead.
func (l *ListView) rowUnder(y core.Unit) int {
	metrics := l.EffectiveCellMetrics()
	if metrics.UnitsPerCellHeight <= 0 {
		return -1
	}
	if y -= l.rowsTop(); y < 0 {
		return -1
	}
	return int(y / metrics.UnitsPerCellHeight)
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
	maxScroll := l.Count() - l.visibleCount()
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

	// The refusal line is not a row and is not the bar: it is something to READ.
	// A press on it does nothing rather than choosing the row under it.
	if l.rowUnder(event.Y) < 0 {
		return false
	}

	l.SetFocusWithoutScroll() // Use without-scroll variant since click proves visibility

	// Check if click is on scrollbar
	_, thumbStart, thumbHeight, _ := l.scrollbarGeometry(l.visibleCount())
	if l.showsScrollbar() && l.onLane(event.X) {
		clickedRow := l.rowUnder(event.Y)

		// Pixel surfaces anchor the drag to the grab point within
		// the unit-granular thumb.
		if core.FindSmoothPositioning(l.Self()) {
			_, thumbU, posU := l.scrollbarUnits(l.visibleCount())
			pos := float64(event.Y - l.rowsTop())
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
			maxScroll := l.Count() - visibleCount
			l.scrollOffset += visibleCount
			if l.scrollOffset > maxScroll {
				l.scrollOffset = maxScroll
			}
		}
		l.Update()
		return true
	}

	// Everything that is not the bar's own column is content. Where there is
	// no bar that is the whole row, including the column one would have taken.

	// Calculate which item was clicked
	clickedRow := l.rowUnder(event.Y)
	clickedIndex := l.scrollOffset + clickedRow

	// Only start content drag if click is on a valid item
	if clickedIndex >= 0 && clickedIndex < l.Count() {
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
	if l.Count() <= visibleCount {
		return false
	}
	bounds := l.Bounds()
	if x < 0 || y < 0 || x >= bounds.Width || y >= bounds.Height {
		return false
	}
	_, thumbStart, thumbHeight, _ := l.scrollbarGeometry(visibleCount)
	if !l.onLane(x) {
		return false
	}
	if core.FindSmoothPositioning(l.Self()) {
		_, thumbU, posU := l.scrollbarUnits(visibleCount)
		pos := float64(y - l.rowsTop())
		return pos >= posU && pos < posU+thumbU
	}
	row := l.rowUnder(y)
	return row >= thumbStart && row < thumbStart+thumbHeight
}

// HandleMouseMove handles mouse drag to sweep selection.
func (l *ListView) HandleMouseMove(event core.MouseMoveEvent) bool {
	// A trinket that answers moves itself still owes the offer of what it
	// could not show; the base makes it for everything that does not.
	l.TrackTooltipHover(core.UnitPoint{X: event.X, Y: event.Y})
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

	// Handle scrollbar thumb drag
	// Note: Once drag is captured on press, we don't check horizontal bounds during drag
	if l.scrollbarDragging {
		// Smooth drag: the thumb follows the pointer in units, the
		// scroll offset snaps to the nearest whole row.
		if l.smoothScrollbarDrag {
			visibleCount := l.visibleCount()
			trackU, thumbU, _ := l.scrollbarUnits(visibleCount)
			scrollable := trackU - thumbU
			newPos := float64(event.Y-l.rowsTop()) - l.scrollbarGrabOff
			if newPos < 0 {
				newPos = 0
			}
			if newPos > scrollable {
				newPos = scrollable
			}
			l.scrollbarThumbPos = newPos
			maxScroll := l.Count() - visibleCount
			newOffset := 0
			if scrollable > 0 && maxScroll > 0 {
				newOffset = int(newPos*float64(maxScroll)/scrollable + 0.5)
			}
			l.scrollOffset = newOffset
			// The thumb moves even when the snapped offset does not.
			l.Update()
			return true
		}

		currentRow := l.rowUnder(event.Y)
		rowDelta := currentRow - l.scrollbarDragStart

		visibleCount := l.visibleCount()
		totalItems := l.Count()
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

	row := l.rowUnder(event.Y)
	index := l.scrollOffset + row

	// Clamp to valid range
	if index < 0 {
		index = 0
	} else if index >= l.Count() {
		index = l.Count() - 1
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
	if l.Count() == 0 {
		return false
	}

	visibleCount := l.visibleCount()
	maxScroll := l.Count() - visibleCount
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
	if l.currentIndex < 0 && l.Count() > 0 {
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
	info.SetSize = l.Count()

	if l.currentIndex >= 0 {
		info.PositionInSet = l.currentIndex + 1
		if item := l.rowAt(l.currentIndex); item != nil {
			info.Value = item.Text
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

// rowTextX is where a row's text begins: past the current-item indicator, and
// past the icon where one is shown. It is the same arithmetic the painting
// does, kept in one place so what is measured is what is drawn.
func (l *ListView) rowTextX(metrics core.CellMetrics, item *ListItem) core.Unit {
	x := metrics.UnitsPerCellWidth
	if l.showIcons && item.Icon != "" {
		x += metrics.UnitsPerCellWidth * 2
	}
	return x
}

// TooltipAt answers for the ROW under the pointer rather than for the list as
// a whole: a list is made of parts, and the part being read is the one with
// more to say than fits in it.
func (l *ListView) TooltipAt(local core.UnitPoint) (string, core.UnitRect, bool) {
	if s := l.Tooltip(); s != "" {
		b := l.Bounds()
		return s, core.UnitRect{Width: b.Width, Height: b.Height}, true
	}
	metrics := l.EffectiveCellMetrics()
	bounds := l.Bounds()
	if metrics.UnitsPerCellHeight <= 0 || local.Y < 0 || local.Y >= bounds.Height {
		return "", core.UnitRect{}, false
	}
	row := l.rowUnder(local.Y)
	at := l.scrollOffset + row
	if row < 0 || at < 0 || at >= l.Count() {
		return "", core.UnitRect{}, false
	}
	item := l.rowAt(at)
	if item == nil {
		return "", core.UnitRect{}, false // a blank row has nothing to say yet
	}
	x := l.rowTextX(metrics, item)
	avail := bounds.Width - x
	if l.showsScrollbar() {
		avail -= metrics.UnitsPerCellWidth
	}
	if item.Text == "" || l.MeasureText(l.CellRun(item.Text)) <= avail {
		return "", core.UnitRect{}, false
	}
	return item.Text, core.UnitRect{
		X:      x,
		Y:      l.rowsTop() + core.Unit(row)*metrics.UnitsPerCellHeight,
		Width:  avail,
		Height: metrics.UnitsPerCellHeight,
	}, true
}
