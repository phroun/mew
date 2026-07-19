// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"fmt"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// TreeItem represents an item in a TreeView.
type TreeItem struct {
	// ID is the item's stable object identity, allocated from the
	// same space as trinket ObjectIDs. Protocol-built items keep the
	// wire ID they were created under, so events, set, and destroy
	// address the same number the reply surfaced.
	ID core.ObjectID

	Text     string
	Icon     *style.TextIcon
	Data     interface{} // User data
	Enabled  bool
	Expanded bool
	Parent   *TreeItem
	Children []*TreeItem

	// Values holds this item's data-column cell text, keyed by
	// TreeColumn.ID (see SetValue/Value in treeview_columns.go).
	Values map[string]string
	// numValues caches each cell's numeric equivalent, parsed once in
	// SetValue so numeric sorts never re-convert text per comparison.
	numValues map[string]float64
}

// NewTreeItem creates a new tree item.
func NewTreeItem(text string) *TreeItem {
	return &TreeItem{
		ID:      core.NextObjectID(),
		Text:    text,
		Enabled: true,
	}
}

// AddChild adds a child item.
func (t *TreeItem) AddChild(child *TreeItem) {
	child.Parent = t
	t.Children = append(t.Children, child)
}

// RemoveChild removes a child item.
func (t *TreeItem) RemoveChild(child *TreeItem) {
	for i, c := range t.Children {
		if c == child {
			t.Children = append(t.Children[:i], t.Children[i+1:]...)
			child.Parent = nil
			break
		}
	}
}

// IsLeaf returns whether this item has no children.
func (t *TreeItem) IsLeaf() bool {
	return len(t.Children) == 0
}

// Level returns the nesting level (0 for root items).
func (t *TreeItem) Level() int {
	level := 0
	for p := t.Parent; p != nil; p = p.Parent {
		level++
	}
	return level
}

// TreeView displays a hierarchical tree of items.
type TreeView struct {
	core.TrinketBase
	core.AccessibleTrinket

	rootItems    []*TreeItem
	flatList     []*TreeItem // Flattened list of visible items
	currentIndex int
	scrollOffset int

	// Appearance
	indentWidth int // Characters per indent level

	// Double-click detection
	lastClickTime  int64 // Unix nano
	lastClickIndex int

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

	// Multi-column state (see treeview_columns.go). The tree itself is
	// the KEY column; data columns render item cell values beside it.
	columns    []*TreeColumn
	showHeader bool
	showKey    bool   // key column shown as the first visible column
	keyCaption string // header caption over the key (tree) column
	ledger     bool   // alternate non-selected rows in LedgerOdd/LedgerEven
	treeLines  bool   // connector lines + leaf glyphs in the indent space
	fitWidth   bool // true: squeeze to width (no hscroll); false: pan
	fixedLeft  int  // visible columns pinned outside the hscroll region
	fixedRight int
	keyWidth   int // key column cells in scroll mode (0 = default)
	hScroll    int // horizontal scroll offset in cells

	// Divider drag-resize state (nil colDragCol = the key column).
	// colDragInvert: the divider is sizing the column to its RIGHT
	// (slack lives to the left), so the delta applies negated.
	colDragging   bool
	colDragCol    *TreeColumn
	colDragStartX core.Unit
	colDragStartW int
	colDragInvert bool

	// Composite fit-mode drag: the grabbed line moves by resizing the
	// two columns astride it against the slack pool (the auto-fill
	// key's spare width when the key shows, else the blank width right
	// of the last column), capped so the layout never starts
	// reclaiming from unrelated columns mid-drag - no other line ever
	// moves contrary to the drag direction. Snapshot widths keep each
	// move idempotent from the press state.
	colDragFit        bool
	colDragSlackRight bool        // pool right of the line (key hidden)
	colDragL          *TreeColumn // nil = the key column (divider 0)
	colDragR          *TreeColumn
	colDragLW         int
	colDragRW         int
	colDragPool       int // slack cells consumable before reclaim would kick in

	// Horizontal scrollbar (footer row) drag state.
	hbarDragging     bool
	hbarThumbHovered bool // pointer over the footer thumb (hover color)
	hbarDragStartX   core.Unit
	hbarDragStartHS  int

	// In-place row editing (see treeview_edit.go). The editor is a
	// spun-into-existence TextInput floating over one cell; the tree
	// keeps real focus and forwards input while it is up.
	rowEditing     bool
	editCol        *TreeColumn
	editLastCol    *TreeColumn // resumed on the next edit session
	editItem       *TreeItem
	editOrig       string
	editBox        *TextInput // free-text cells
	editCombo      *ComboBox  // enum cells (closed until Space/click)
	editComboMagic bool       // combo row 0 is the not-in-enum original
	editMouseDown  bool
	keyEditable    bool // SetEditable: the KEY column joins the edit ring
	onCellEdited   func(item *TreeItem, column *TreeColumn, value string)

	// Click-to-edit: a drag-free click on an editable cell of the
	// already-selected row flips straight into edit mode.
	clickEditItem *TreeItem
	clickEditCol  *TreeColumn
	clickEditX    core.Unit
	clickEditY    core.Unit

	// Sort state (visual; the trinket reorders its row list, the app's
	// item order is untouched): sorted=false means unsorted; sortedBy
	// is -1 for the key (tree) column or a declared data-column index;
	// sortDescending flips the direction. Activating a header cycles
	// ascending -> descending -> unsorted.
	sorted          bool
	sortedBy        int
	sortDescending  bool
	onSortRequested func(sorted bool, sortedBy int, descending bool)

	// Column-chooser button/menu state (the [=] in the header corner).
	chooserHovered bool
	chooserOpen    bool
	chooserMenu    *Menu

	// Internal header focus zone: the tree is ONE trinket in the app
	// tab order, but runs its own focus machine (the window title-bar
	// pattern): hzBar lights the whole header as one stop, Enter drills
	// into hzItems (Tab cycles column captions then the chooser), and
	// tabbing past the chooser lands in hzContent - the tree proper.
	headerZone     int // hzContent / hzBar / hzItems
	headerFocusIdx int // index into header stops when hzItems

	// Callbacks
	onCurrentChanged func(item *TreeItem)
	onItemActivated  func(item *TreeItem)
	onItemExpanded   func(item *TreeItem)
	onItemCollapsed  func(item *TreeItem)
}

// NewTreeView creates a new tree view.
func NewTreeView() *TreeView {
	t := &TreeView{
		currentIndex:   -1,
		indentWidth:    2,
		lastClickIndex: -1,
		showKey:        true,
		fitWidth:       true,
	}
	t.TrinketBase = *core.NewTrinketBase()
	t.Init(t) // Enable polymorphic focus handling
	t.SetFocusPolicy(core.StrongFocus)
	t.SetAccessibleRole(core.RoleTree)
	return t
}

// AddRootItem adds a root item to the tree.
func (t *TreeView) AddRootItem(item *TreeItem) {
	item.Parent = nil
	t.rootItems = append(t.rootItems, item)
	t.rebuildFlatList()
	if t.currentIndex < 0 && len(t.flatList) > 0 {
		t.SetCurrentIndex(0)
	}
	t.Update()
}

// RemoveRootItem removes a root item from the tree.
func (t *TreeView) RemoveRootItem(item *TreeItem) {
	for i, r := range t.rootItems {
		if r == item {
			t.rootItems = append(t.rootItems[:i], t.rootItems[i+1:]...)
			break
		}
	}
	t.rebuildFlatList()
	if t.currentIndex >= len(t.flatList) {
		t.currentIndex = len(t.flatList) - 1
	}
	t.Update()
}

// Clear removes all items.
func (t *TreeView) Clear() {
	t.rootItems = nil
	t.flatList = nil
	t.currentIndex = -1
	t.scrollOffset = 0
	t.Update()
}

// RootItems returns all root items.
func (t *TreeView) RootItems() []*TreeItem {
	return t.rootItems
}

// CurrentItem returns the currently focused item.
func (t *TreeView) CurrentItem() *TreeItem {
	if t.currentIndex < 0 || t.currentIndex >= len(t.flatList) {
		return nil
	}
	return t.flatList[t.currentIndex]
}

// SetCurrentItem sets the current item.
func (t *TreeView) SetCurrentItem(item *TreeItem) {
	for i, flatItem := range t.flatList {
		if flatItem == item {
			t.SetCurrentIndex(i)
			return
		}
	}
}

// CurrentIndex returns the current index in the flat list.
func (t *TreeView) CurrentIndex() int {
	return t.currentIndex
}

// SetCurrentIndex sets the current index.
func (t *TreeView) SetCurrentIndex(index int) {
	if index < -1 || index >= len(t.flatList) {
		return
	}
	if t.currentIndex == index {
		return
	}

	t.currentIndex = index
	t.ensureVisible(index)
	t.Update()

	// Notify parent scroll containers to scroll this item into view.
	// This is needed for keyboard navigation when the TreeView is inside
	// a ScrollArea and the selected item moves outside the visible area.
	// For mouse clicks, SetFocusWithoutScroll() prevents unwanted scrolling.
	if index >= 0 {
		metrics := t.EffectiveCellMetrics()
		item := t.flatList[index]
		level := item.Level()

		// Calculate the item's content start position (one space before the expand indicator)
		// For root items (level 0), start at X=0
		contentStartCells := level*t.indentWidth + treeLeftPadCells
		if contentStartCells > 0 {
			contentStartCells-- // Show one space of indent
		}

		// Calculate actual content width: expand indicator (2 chars) + text
		expandIndicatorWidth := 2 // "▶ " or "▼ " or "  " (for leaves)
		textWidth := len(item.Text)
		actualContentCells := expandIndicatorWidth + textWidth

		// Calculate the visual Y position of this item (after internal scrolling)
		// This is where the item appears on screen, relative to the TreeView's bounds
		visualRow := index - t.scrollOffset
		itemY := core.Unit(visualRow) * metrics.CellHeight

		itemRect := core.UnitRect{
			X:      core.Unit(contentStartCells) * metrics.CellWidth,
			Y:      itemY,
			Width:  core.Unit(actualContentCells) * metrics.CellWidth,
			Height: metrics.CellHeight,
		}
		t.ScrollRectIntoView(itemRect)
	}

	// Announce selection change for accessibility
	if index >= 0 && index < len(t.flatList) {
		if am := core.FindAccessibilityManager(t); am != nil {
			item := t.flatList[index]
			state := ""
			if !item.IsLeaf() {
				if item.Expanded {
					state = ", expanded"
				} else {
					state = ", collapsed"
				}
			}
			am.AnnouncePolite(fmt.Sprintf("%s, tree item%s, level %d", item.Text, state, item.Level()+1))
		}
	}

	if t.onCurrentChanged != nil && index >= 0 {
		t.onCurrentChanged(t.flatList[index])
	}
}

// ExpandItem expands an item to show its children.
func (t *TreeView) ExpandItem(item *TreeItem) {
	if item.IsLeaf() || item.Expanded {
		return
	}

	// Save currently selected item
	var selectedItem *TreeItem
	if t.currentIndex >= 0 && t.currentIndex < len(t.flatList) {
		selectedItem = t.flatList[t.currentIndex]
	}

	item.Expanded = true
	t.rebuildFlatList()

	// Restore selection by finding the same item in new flat list
	t.restoreSelectionByItem(selectedItem)
	t.Update()

	// Announce expansion for accessibility
	if am := core.FindAccessibilityManager(t); am != nil {
		am.AnnouncePolite(fmt.Sprintf("%s, expanded", item.Text))
	}

	if t.onItemExpanded != nil {
		t.onItemExpanded(item)
	}
}

// CollapseItem collapses an item to hide its children.
func (t *TreeView) CollapseItem(item *TreeItem) {
	if !item.Expanded {
		return
	}

	// Save currently selected item
	var selectedItem *TreeItem
	if t.currentIndex >= 0 && t.currentIndex < len(t.flatList) {
		selectedItem = t.flatList[t.currentIndex]
	}

	item.Expanded = false
	t.rebuildFlatList()

	// Restore selection by finding the same item in new flat list
	// If selected item is no longer visible (was in collapsed subtree),
	// select the item that was collapsed
	if !t.restoreSelectionByItem(selectedItem) {
		// Selected item no longer visible, select the collapsed item
		t.restoreSelectionByItem(item)
	}
	t.Update()

	// Announce collapse for accessibility
	if am := core.FindAccessibilityManager(t); am != nil {
		am.AnnouncePolite(fmt.Sprintf("%s, collapsed", item.Text))
	}

	if t.onItemCollapsed != nil {
		t.onItemCollapsed(item)
	}
}

// restoreSelectionByItem finds the given item in the flat list and selects it.
// Returns true if the item was found and selected, false otherwise.
func (t *TreeView) restoreSelectionByItem(item *TreeItem) bool {
	if item == nil {
		return false
	}
	for i, flatItem := range t.flatList {
		if flatItem == item {
			t.currentIndex = i
			return true
		}
	}
	return false
}

// ToggleItem toggles the expanded state of an item.
func (t *TreeView) ToggleItem(item *TreeItem) {
	if item.Expanded {
		t.CollapseItem(item)
	} else {
		t.ExpandItem(item)
	}
}

// ExpandAll expands all items.
func (t *TreeView) ExpandAll() {
	// Save currently selected item
	var selectedItem *TreeItem
	if t.currentIndex >= 0 && t.currentIndex < len(t.flatList) {
		selectedItem = t.flatList[t.currentIndex]
	}

	t.expandRecursive(t.rootItems)
	t.rebuildFlatList()

	// Restore selection by finding the same item
	t.restoreSelectionByItem(selectedItem)
	t.Update()
}

func (t *TreeView) expandRecursive(items []*TreeItem) {
	for _, item := range items {
		item.Expanded = true
		t.expandRecursive(item.Children)
	}
}

// CollapseAll collapses all items.
func (t *TreeView) CollapseAll() {
	// Save currently selected item
	var selectedItem *TreeItem
	if t.currentIndex >= 0 && t.currentIndex < len(t.flatList) {
		selectedItem = t.flatList[t.currentIndex]
	}

	t.collapseRecursive(t.rootItems)
	t.rebuildFlatList()

	// Restore selection - if item is no longer visible, select first root
	if !t.restoreSelectionByItem(selectedItem) && len(t.flatList) > 0 {
		t.currentIndex = 0
	}
	t.Update()
}

func (t *TreeView) collapseRecursive(items []*TreeItem) {
	for _, item := range items {
		item.Expanded = false
		t.collapseRecursive(item.Children)
	}
}

// SetIndentWidth sets the indent width per level.
func (t *TreeView) SetIndentWidth(width int) {
	t.indentWidth = width
	t.Update()
}

// SetOnCurrentChanged sets the current changed callback.
func (t *TreeView) SetOnCurrentChanged(handler func(item *TreeItem)) {
	t.onCurrentChanged = handler
}

// SetOnItemActivated sets the item activated callback.
func (t *TreeView) SetOnItemActivated(handler func(item *TreeItem)) {
	t.onItemActivated = handler
}

// SetOnItemExpanded sets the item expanded callback.
func (t *TreeView) SetOnItemExpanded(handler func(item *TreeItem)) {
	t.onItemExpanded = handler
}

// SetOnItemCollapsed sets the item collapsed callback.
func (t *TreeView) SetOnItemCollapsed(handler func(item *TreeItem)) {
	t.onItemCollapsed = handler
}

// rebuildFlatList rebuilds the flattened list of visible items.
func (t *TreeView) rebuildFlatList() {
	t.flatList = nil
	t.flattenItems(t.rootItems)

	// Clamp scroll offset to valid range after list size changes
	t.clampScrollOffset()
}

// SetBounds resizes the tree and re-clamps its scroll state (the
// embedded base cannot dispatch HandleResize to us - the ScrollArea
// override pattern).
func (t *TreeView) SetBounds(bounds core.UnitRect) {
	old := t.Bounds().Size()
	t.TrinketBase.SetBounds(bounds)
	if old != bounds.Size() {
		t.HandleResize(old, bounds.Size())
	}
}

// HandleResize re-clamps the scroll state: growing the view while
// scrolled down must pull the content back into the freed space (the
// scrollbar vanishes WITH the blank it would explain), and a stale
// horizontal pan snaps back the same way.
func (t *TreeView) HandleResize(oldSize, newSize core.UnitSize) {
	t.clampScrollOffset()
	if t.hScroll > 0 {
		if lay := t.columnLayout(); t.hScroll > lay.maxHScroll {
			t.hScroll = lay.maxHScroll
		}
	}
	t.Update()
}

// clampScrollOffset ensures scrollOffset is within valid bounds.
func (t *TreeView) clampScrollOffset() {
	if len(t.flatList) == 0 {
		t.scrollOffset = 0
		return
	}

	visibleCount := t.visibleCount()
	maxScroll := len(t.flatList) - visibleCount
	if maxScroll < 0 {
		maxScroll = 0
	}
	if t.scrollOffset > maxScroll {
		t.scrollOffset = maxScroll
	}
	if t.scrollOffset < 0 {
		t.scrollOffset = 0
	}
}

func (t *TreeView) flattenItems(items []*TreeItem) {
	for _, item := range t.visualSiblings(items) {
		t.flatList = append(t.flatList, item)
		if item.Expanded && len(item.Children) > 0 {
			t.flattenItems(item.Children)
		}
	}
}

// ensureVisible ensures the given index is visible. Degenerate bounds
// (zero height, e.g. before the first layout) are left alone - there
// is no viewport to scroll into yet, and adjusting would push a bogus
// offset that survives the real layout.
func (t *TreeView) ensureVisible(index int) {
	if index < 0 {
		return
	}
	visibleCount := t.visibleCount()
	if visibleCount <= 0 {
		return
	}
	if index < t.scrollOffset {
		t.scrollOffset = index
	} else if index >= t.scrollOffset+visibleCount {
		t.scrollOffset = index - visibleCount + 1
	}
}

// SizeHint returns the preferred size.
func (t *TreeView) SizeHint() core.UnitSize {
	metrics := t.EffectiveCellMetrics()
	font := t.EffectiveFont()
	return core.UnitSize{
		Width:  font.MeasureRunes(40),  // Default width for 40 chars
		Height: metrics.TextHeight(15), // 15 items visible
	}
}

// Paint renders the tree view.
func (t *TreeView) Paint(p *core.Painter) {
	if t.multiColumn() {
		t.paintMulti(p)
		return
	}
	bounds := t.Bounds()
	scheme := t.GetScheme()
	focused := t.HasFocus()
	metrics := t.EffectiveCellMetrics()

	// Draw background using list colors
	bgStyle := style.DefaultStyle().WithFg(scheme.GetListFG()).WithBg(scheme.GetListBG())
	p.FillRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, ' ', bgStyle)

	visibleCount := int(bounds.Height / metrics.CellHeight)

	// GUI: paint one extra partial row into any leftover strip rather
	// than leaving it blank (never counted as visible for scrolling).
	rows := visibleCount
	if p.Graphical() && t.scrollOffset+visibleCount < len(t.flatList) &&
		core.Unit(visibleCount)*metrics.CellHeight < bounds.Height {
		rows++
	}

	// Draw items
	for i := 0; i < rows; i++ {
		itemIndex := t.scrollOffset + i
		if itemIndex >= len(t.flatList) {
			break
		}

		item := t.flatList[itemIndex]
		itemY := core.Unit(i) * metrics.CellHeight
		level := item.Level()

		// Determine style
		var s style.CellStyle
		if !item.Enabled {
			s = style.DefaultStyle().WithFg(scheme.GetDisabledTextFG()).WithBg(scheme.GetListBG())
		} else if itemIndex == t.currentIndex {
			if focused {
				s = scheme.GetFocusedListItem()
			} else {
				s = scheme.GetSelectedListItem()
			}
		} else if t.ledger {
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

		// Draw row background
		p.FillRect(core.UnitRect{
			X:      0,
			Y:      itemY,
			Width:  bounds.Width,
			Height: metrics.CellHeight,
		}, ' ', s)

		// Calculate x position with indent (plus the left breathing pad)
		x := core.Unit(level*t.indentWidth+treeLeftPadCells) * metrics.CellWidth

		// Connector lines fill the indent space (never widen it).
		if t.treeLines {
			for ci, r := range t.treeLinePrefix(item) {
				if r != ' ' {
					t.drawTreeLineCell(p, core.Unit(ci+treeLeftPadCells)*metrics.CellWidth, itemY, r, s, metrics)
				}
			}
		}

		// Draw expand/collapse indicator
		if !item.IsLeaf() {
			if item.Expanded {
				p.DrawCell(x, itemY, '▼', s)
			} else {
				p.DrawCell(x, itemY, '▸', s)
			}
		} else if t.treeLines {
			p.DrawCell(x, itemY, '▪', s)
		} else {
			p.DrawCell(x, itemY, ' ', s)
		}
		x += metrics.CellWidth

		// Draw icon if present
		if item.Icon != nil && len(item.Icon.Cells) > 0 {
			cell := item.Icon.Cells[0]
			p.DrawCell(x, itemY, cell.Char, cell.Style)
			x += metrics.CellWidth * 2
		}

		// Draw text using font-aware rendering
		font := t.EffectiveFont()
		availableWidth := bounds.Width - x
		if availableWidth < 0 {
			availableWidth = 0
		}
		p.DrawText(x, itemY, ellipsizeText(font, item.Text, availableWidth), s, font)
	}

	// Draw scrollbar if needed
	if len(t.flatList) > visibleCount {
		t.paintScrollbar(p, visibleCount)
	}
}

// visibleCount returns the number of visible content rows (the header
// row and the horizontal-scrollbar footer row are not content rows).
// Never negative: squeezed below its own chrome the tree simply has
// zero content rows (a negative count reaches make() in the paint
// path and panics).
func (t *TreeView) visibleCount() int {
	bounds := t.Bounds()
	metrics := t.EffectiveCellMetrics()
	n := int((bounds.Height - t.headerHeight() - t.footerHeight()) / metrics.CellHeight)
	if n < 0 {
		n = 0
	}
	return n
}

// scrollbarGeometry returns scrollbar dimensions and thumb position.
// Returns: scrollbarX, thumbStart, thumbHeight, trackHeight (all in rows)
func (t *TreeView) scrollbarGeometry(visibleCount int) (scrollbarX core.Unit, thumbStart, thumbHeight, trackHeight int) {
	bounds := t.Bounds()
	metrics := t.EffectiveCellMetrics()
	totalItems := len(t.flatList)

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
		thumbStart = t.scrollOffset * scrollableTrack / maxScroll

		// Ensure thumb doesn't go to extremes unless scroll is at extremes
		if t.scrollOffset > 0 && thumbStart == 0 {
			thumbStart = 1
		}
		if t.scrollOffset < maxScroll && thumbStart >= scrollableTrack {
			thumbStart = scrollableTrack - 1
		}
	}

	return
}

// scrollbarUnits returns the scrollbar track length, thumb length,
// and thumb origin in units for pixel surfaces - the same proportions
// as scrollbarGeometry without row quantization. Mid-drag the thumb
// origin is the smooth (pointer-tracked) position.
func (t *TreeView) scrollbarUnits(visibleCount int) (trackU, thumbU, posU float64) {
	metrics := t.EffectiveCellMetrics()
	trackU = float64(core.Unit(visibleCount) * metrics.CellHeight)
	totalItems := len(t.flatList)
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
	if t.scrollbarDragging && t.smoothScrollbarDrag {
		posU = t.scrollbarThumbPos
	} else if maxScroll > 0 {
		posU = float64(t.scrollOffset) * scrollable / float64(maxScroll)
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
func (t *TreeView) paintScrollbar(p *core.Painter, visibleCount int) {
	scheme := t.GetScheme()
	metrics := t.EffectiveCellMetrics()
	trackStyle := scheme.GetScrollbar()
	thumbStyle := scheme.GetScrollbarThumbState(t.scrollbarThumbHovered && p.Graphical())

	// Pixel surfaces: a single hairline stripe blended at 50%
	// opacity behind, and one solid full-opacity rectangle for the
	// thumb, at unit granularity - same treatment as the combobox
	// popup lane.
	// The track starts below the header row (when one is shown).
	headerH := t.headerHeight()

	if p.Graphical() {
		// No track stripe: the hairline reads as another column
		// divider next to the real ones. The bare thumb is the bar.
		_, thumbU, posU := t.scrollbarUnits(visibleCount)
		laneX := t.Bounds().Width - metrics.CellWidth
		p.FillRect(core.UnitRect{
			X:      laneX + 1,
			Y:      headerH + core.Unit(posU+0.5),
			Width:  metrics.CellWidth - 2,
			Height: core.Unit(thumbU + 0.5),
		}, ' ', thumbStyle.WithBg(thumbStyle.Fg))
		return
	}

	scrollbarX, thumbStart, thumbHeight, trackHeight := t.scrollbarGeometry(visibleCount)

	// Draw scrollbar track (the ScrollArea's shaded fill, not a line).
	for i := 0; i < trackHeight; i++ {
		y := headerH + core.Unit(i)*metrics.CellHeight
		p.DrawCell(scrollbarX, y, '░', trackStyle)
	}

	// Draw scrollbar thumb
	for i := 0; i < thumbHeight; i++ {
		y := headerH + core.Unit(thumbStart+i)*metrics.CellHeight
		p.DrawCell(scrollbarX, y, '█', thumbStyle)
	}
}

// HandleKeyPress handles keyboard input.
func (t *TreeView) HandleKeyPress(event core.KeyPressEvent) bool {
	// The open column-chooser menu takes keys first (the tree retains
	// focus and forwards - the menu bar pattern).
	if t.handleChooserKey(event) {
		return true
	}
	// The open row editor takes everything next (see treeview_edit.go).
	if t.handleEditKey(event) {
		return true
	}
	// The internal header focus zone consumes its navigation (including
	// the content zone's S-Tab back into the bar) before content keys.
	if t.handleHeaderFocusKey(event) {
		return true
	}
	// In an editable grid, Left/Right rotate the Enter-target column
	// (the FocusedListItem cell) without opening the editor.
	if t.handleEditTargetKey(event) {
		return true
	}

	current := t.CurrentItem()

	switch event.Key {
	case "Up":
		if t.currentIndex > 0 {
			t.SetCurrentIndex(t.currentIndex - 1)
		}
		return true

	case "M-Up", "C-Up", "A-Up":
		// Jump by 5 items, scrolling to maintain relative position
		if t.currentIndex > 0 {
			delta := 5
			newIndex := t.currentIndex - delta
			if newIndex < 0 {
				newIndex = 0
			}
			actualDelta := t.currentIndex - newIndex
			// Scroll by same amount to maintain relative position
			newScroll := t.scrollOffset - actualDelta
			if newScroll < 0 {
				newScroll = 0
			}
			t.scrollOffset = newScroll
			t.SetCurrentIndex(newIndex)
		}
		return true

	case "Down":
		if t.currentIndex < len(t.flatList)-1 {
			t.SetCurrentIndex(t.currentIndex + 1)
		}
		return true

	case "M-Down", "C-Down", "A-Down":
		// Jump by 5 items, scrolling to maintain relative position
		if t.currentIndex < len(t.flatList)-1 {
			delta := 5
			newIndex := t.currentIndex + delta
			if newIndex >= len(t.flatList) {
				newIndex = len(t.flatList) - 1
			}
			actualDelta := newIndex - t.currentIndex
			// Scroll by same amount to maintain relative position
			visibleCount := t.visibleCount()
			maxScroll := len(t.flatList) - visibleCount
			if maxScroll < 0 {
				maxScroll = 0
			}
			newScroll := t.scrollOffset + actualDelta
			if newScroll > maxScroll {
				newScroll = maxScroll
			}
			t.scrollOffset = newScroll
			t.SetCurrentIndex(newIndex)
		}
		return true

	// Shift+Left/Right always mean the classic tree navigation, even
	// on editable grids where the plain arrows rotate the Enter-target
	// column (handleEditTargetKey lets shifted arrows through).
	case "Left", "S-Left":
		if current != nil {
			if current.Expanded && !current.IsLeaf() {
				t.CollapseItem(current)
			} else if current.Parent != nil {
				t.SetCurrentItem(current.Parent)
			}
		}
		return true

	case "Right", "S-Right":
		if current != nil {
			if !current.Expanded && !current.IsLeaf() {
				t.ExpandItem(current)
			} else if current.Expanded && len(current.Children) > 0 {
				t.SetCurrentItem(current.Children[0])
			}
		}
		return true

	case "Home":
		if len(t.flatList) > 0 {
			t.SetCurrentIndex(0)
		}
		return true

	case "End":
		if len(t.flatList) > 0 {
			t.SetCurrentIndex(len(t.flatList) - 1)
		}
		return true

	case "PageUp":
		bounds := t.Bounds()
		metrics := t.EffectiveCellMetrics()
		pageSize := int(bounds.Height / metrics.CellHeight)
		newIndex := t.currentIndex - pageSize
		if newIndex < 0 {
			newIndex = 0
		}
		t.SetCurrentIndex(newIndex)
		return true

	case "PageDown":
		bounds := t.Bounds()
		metrics := t.EffectiveCellMetrics()
		pageSize := int(bounds.Height / metrics.CellHeight)
		newIndex := t.currentIndex + pageSize
		if newIndex >= len(t.flatList) {
			newIndex = len(t.flatList) - 1
		}
		t.SetCurrentIndex(newIndex)
		return true

	case "Enter":
		// With editable columns, Enter opens the in-place row editor;
		// without any, it behaves exactly like Space.
		if t.startRowEdit() {
			// Entering edit DIRECTLY on a choice cell pops its
			// drop-down - the arrowed target advertised a picker and
			// Enter accepted the offer. (Tabbing to a choice cell
			// from another column stays closed.)
			if t.editCombo != nil {
				t.editCombo.HandleKeyPress(core.KeyPressEvent{Key: " "})
			}
			return true
		}
		if current != nil {
			if !current.IsLeaf() {
				t.ToggleItem(current)
			}
			if t.onItemActivated != nil {
				t.onItemActivated(current)
			}
		}
		return true

	case " ", "Space":
		// On a CHOICE Enter-target, Space enters edit and pops the
		// drop-down (a text target keeps Space's classic toggle -
		// Space never begins a text edit).
		if col := t.enterTargetColumn(); col != nil && col != treeKeyColumn &&
			len(col.Enum) > 0 && t.headerZone == hzContent && t.startRowEdit() {
			if t.editCombo != nil {
				t.editCombo.HandleKeyPress(core.KeyPressEvent{Key: " "})
			}
			return true
		}
		if current != nil {
			if !current.IsLeaf() {
				t.ToggleItem(current)
			}
			if t.onItemActivated != nil {
				t.onItemActivated(current)
			}
		}
		return true

	case "*":
		// Expand all
		t.ExpandAll()
		return true

	case "-":
		// Collapse current
		if current != nil && !current.IsLeaf() {
			t.CollapseItem(current)
		}
		return true

	case "+":
		// Expand current
		if current != nil && !current.IsLeaf() {
			t.ExpandItem(current)
		}
		return true
	}

	return false
}

// HandleMousePress handles mouse clicks.
func (t *TreeView) HandleMousePress(event core.MousePressEvent) bool {
	// A right-click inside the open row editor belongs to the editor:
	// it opens the TextInput's own context menu (Cut/Copy/Paste/...).
	if event.Button == core.RightButton && t.rowEditing && t.editBox != nil {
		if r, ok := t.editorRect(); ok &&
			event.X >= r.X && event.X < r.X+r.Width &&
			event.Y >= r.Y && event.Y < r.Y+r.Height {
			ev := event
			ev.X -= r.X
			ev.Y -= r.Y
			return t.editBox.HandleMousePress(ev)
		}
	}
	if event.Button != core.LeftButton {
		return false
	}

	// Clear any stale drag state from previous incomplete drags
	t.isDragging = false
	t.scrollbarDragging = false

	// Check if click is within our bounds
	bounds := t.Bounds()
	if event.X < 0 || event.Y < 0 || event.X >= bounds.Width || event.Y >= bounds.Height {
		return false
	}

	t.SetFocusWithoutScroll() // Use without-scroll variant since click proves visibility
	metrics := t.EffectiveCellMetrics()

	// Row editor: a press inside it edits text; a press anywhere else
	// ACCEPTS the value and the click proceeds. Also note whether this
	// press is a click-to-edit candidate (an editable cell of the
	// already-selected row).
	if t.handleEditMousePress(event) {
		return true
	}
	t.noteClickEditPress(event)

	// Header band: chooser button and divider drags (multi-column).
	if t.handleMultiPress(event) {
		return true
	}
	// Footer band: the reserved horizontal-scrollbar row.
	if t.handleHBarPress(event) {
		return true
	}
	headerH := t.headerHeight()
	contentY := event.Y - headerH

	// Check if click is on scrollbar
	scrollbarX, thumbStart, thumbHeight, _ := t.scrollbarGeometry(t.visibleCount())
	if event.X >= scrollbarX && len(t.flatList) > t.visibleCount() {
		clickedRow := int(contentY / metrics.CellHeight)

		// Pixel surfaces anchor the drag to the grab point within
		// the unit-granular thumb.
		if core.FindSmoothPositioning(t.Self()) {
			_, thumbU, posU := t.scrollbarUnits(t.visibleCount())
			pos := float64(contentY)
			if pos >= posU && pos < posU+thumbU {
				t.scrollbarDragging = true
				t.smoothScrollbarDrag = true
				t.isDragging = false
				t.scrollbarGrabOff = pos - posU
				t.scrollbarThumbPos = posU
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
			t.scrollbarDragging = true
			t.isDragging = false
			t.scrollbarDragStart = clickedRow
			t.scrollbarDragOffset = t.scrollOffset
			return true
		}

		// Click on track - page up or page down
		visibleCount := t.visibleCount()
		if clickedRow < thumbStart {
			// Page up
			t.scrollOffset -= visibleCount
			if t.scrollOffset < 0 {
				t.scrollOffset = 0
			}
		} else {
			// Page down
			maxScroll := len(t.flatList) - visibleCount
			t.scrollOffset += visibleCount
			if t.scrollOffset > maxScroll {
				t.scrollOffset = maxScroll
			}
		}
		t.Update()
		return true
	}

	// Click on tree content (before scrollbar)
	if event.X >= scrollbarX {
		return false // Click is past the content area
	}

	// Calculate which item was clicked
	clickedRow := int(contentY / metrics.CellHeight)
	clickedIndex := t.scrollOffset + clickedRow

	// Only process if click is on a valid item
	contentWidth := bounds.Width - metrics.CellWidth
	if event.X >= 0 && event.X < contentWidth && contentY >= 0 && clickedIndex >= 0 && clickedIndex < len(t.flatList) {
		item := t.flatList[clickedIndex]
		level := item.Level()

		// Check if clicked on expand/collapse indicator. In the
		// multi-column presentation the tree lives in its host span -
		// the key column, or (key hidden) the first visible data
		// column - which may be panned; offset accordingly.
		keyX := core.Unit(0)
		if t.multiColumn() {
			lay := t.columnLayout()
			host := t.treeHostColumn()
			keyX = core.Unit(-1)
			for _, sp := range lay.spans {
				if sp.col == nil || (host != nil && sp.col == host) {
					keyX = sp.x
					break
				}
			}
			if keyX < 0 {
				keyX = contentWidth // no tree host in view: no indicator hit
			}
		}
		indicatorX := keyX + core.Unit(level*t.indentWidth+treeLeftPadCells)*metrics.CellWidth
		if event.X >= indicatorX && event.X < indicatorX+metrics.CellWidth {
			if !item.IsLeaf() {
				t.ToggleItem(item)
				return true
			}
		}

		// A content click re-zones the internal header focus to content.
		t.headerZone = hzContent

		// Start content drag - clear scrollbar drag flag
		t.isDragging = true
		t.scrollbarDragging = false

		// Check for double-click (400ms threshold)
		now := time.Now().UnixNano()
		isDoubleClick := t.lastClickIndex == clickedIndex &&
			(now-t.lastClickTime) < int64(400*time.Millisecond)

		// Update click tracking
		t.lastClickTime = now
		t.lastClickIndex = clickedIndex

		if isDoubleClick {
			// A double click on an EDITABLE cell belongs to click-to-
			// edit (the release opens the editor): suppress the expand/
			// collapse so both don't fire at once. The classic toggle
			// stays on non-editable parts of the row and on the tree
			// cell's indent/expander region left of the caption text
			// (noteClickEditPress never claims those).
			if t.clickEditItem != nil {
				t.lastClickIndex = -1
				return true
			}
			// Double-click: toggle expand/collapse if not leaf, then activate
			if !item.IsLeaf() {
				t.ToggleItem(item)
			}
			if t.onItemActivated != nil {
				t.onItemActivated(item)
			}
			// Reset double-click state
			t.lastClickIndex = -1
			return true
		}

		t.SetCurrentIndex(clickedIndex)
		// The click also selects the COLUMN as the Enter target when
		// it lands on an editable cell; a click on a non-editable
		// cell keeps the previous target and changes only the row.
		if col := t.editableColumnAt(event.X, item); col != nil {
			t.editLastCol = col
		}
		return true
	}

	// Click is in content area but not on a valid item
	return false
}

// overScrollbarThumb reports whether a widget-local point lies on the
// vertical scrollbar thumb.
func (t *TreeView) overScrollbarThumb(x, y core.Unit) bool {
	visibleCount := t.visibleCount()
	if len(t.flatList) <= visibleCount {
		return false
	}
	bounds := t.Bounds()
	if x < 0 || y < 0 || x >= bounds.Width || y >= bounds.Height {
		return false
	}
	scrollbarX, thumbStart, thumbHeight, _ := t.scrollbarGeometry(visibleCount)
	if x < scrollbarX {
		return false
	}
	contentY := y - t.headerHeight() // the track starts below the header
	if core.FindSmoothPositioning(t.Self()) {
		_, thumbU, posU := t.scrollbarUnits(visibleCount)
		pos := float64(contentY)
		return pos >= posU && pos < posU+thumbU
	}
	row := int(contentY / t.EffectiveCellMetrics().CellHeight)
	return row >= thumbStart && row < thumbStart+thumbHeight
}

// HandleMouseMove handles mouse drag to sweep selection.
func (t *TreeView) HandleMouseMove(event core.MouseMoveEvent) bool {
	// Track scrollbar-thumb hover. Hover is a no-button affordance: while a
	// button is held (a drag begun elsewhere passing over) don't light the
	// thumb - unless this tree owns the scrollbar drag.
	if over := t.scrollbarDragging || (event.Buttons == 0 && t.overScrollbarThumb(event.X, event.Y)); over != t.scrollbarThumbHovered {
		t.scrollbarThumbHovered = over
		t.Update()
	}
	// Footer horizontal scrollbar thumb: the same hover convention.
	if over := t.hbarDragging || (event.Buttons == 0 && t.overHBarThumb(event.X, event.Y)); over != t.hbarThumbHovered {
		t.hbarThumbHovered = over
		t.Update()
	}
	// Chooser-button hover follows the standard convention.
	t.updateChooserHover(event)

	// If we don't have focus, we shouldn't be processing drags
	// (another trinket got the click and we have stale drag state)
	if !t.HasFocus() {
		t.isDragging = false
		t.scrollbarDragging = false
		t.colDragging = false
		t.colDragCol = nil
		t.colDragFit = false
		t.colDragL, t.colDragR = nil, nil
		t.hbarDragging = false
		return false
	}

	// Text-selection drag inside the row editor.
	if t.handleEditMouseMove(event) {
		return true
	}
	// Column divider drag (multi-column resize).
	if t.handleMultiMove(event) {
		return true
	}
	// Footer horizontal-scrollbar thumb drag.
	if t.handleHBarMove(event) {
		return true
	}

	metrics := t.EffectiveCellMetrics()
	contentY := event.Y - t.headerHeight()

	// Handle scrollbar thumb drag
	// Note: Once drag is captured on press, we don't check horizontal bounds during drag
	if t.scrollbarDragging {
		// Smooth drag: the thumb follows the pointer in units, the
		// scroll offset snaps to the nearest whole row.
		if t.smoothScrollbarDrag {
			visibleCount := t.visibleCount()
			trackU, thumbU, _ := t.scrollbarUnits(visibleCount)
			scrollable := trackU - thumbU
			newPos := float64(contentY) - t.scrollbarGrabOff
			if newPos < 0 {
				newPos = 0
			}
			if newPos > scrollable {
				newPos = scrollable
			}
			t.scrollbarThumbPos = newPos
			maxScroll := len(t.flatList) - visibleCount
			newOffset := 0
			if scrollable > 0 && maxScroll > 0 {
				newOffset = int(newPos*float64(maxScroll)/scrollable + 0.5)
			}
			t.scrollOffset = newOffset
			// The thumb moves even when the snapped offset does not.
			t.Update()
			return true
		}

		currentRow := int(contentY / metrics.CellHeight)
		rowDelta := currentRow - t.scrollbarDragStart

		visibleCount := t.visibleCount()
		totalItems := len(t.flatList)
		maxScroll := totalItems - visibleCount

		if maxScroll > 0 {
			_, _, thumbHeight, trackHeight := t.scrollbarGeometry(visibleCount)
			scrollableTrack := trackHeight - thumbHeight

			if scrollableTrack > 0 {
				// Convert row delta to scroll offset delta
				scrollDelta := rowDelta * maxScroll / scrollableTrack
				newOffset := t.scrollbarDragOffset + scrollDelta

				// Clamp
				if newOffset < 0 {
					newOffset = 0
				} else if newOffset > maxScroll {
					newOffset = maxScroll
				}

				if newOffset != t.scrollOffset {
					t.scrollOffset = newOffset
					t.Update()
				}
			}
		}
		return true
	}

	// Handle tree item drag
	// Note: Once drag is captured on press, we don't check horizontal bounds during drag
	if !t.isDragging {
		return false
	}

	row := int(contentY / metrics.CellHeight)
	index := t.scrollOffset + row

	// Clamp to valid range
	if index < 0 {
		index = 0
	} else if index >= len(t.flatList) {
		index = len(t.flatList) - 1
	}

	if index >= 0 {
		if index != t.currentIndex {
			t.SetCurrentIndex(index)
		}
		// Drag-selection also tracks the COLUMN target over editable
		// cells (non-editable cells keep the previous target). It
		// never auto-enters edit mode: armClickEdit's slop check
		// rejects a moved release, combo cells included.
		if col := t.editableColumnAt(event.X, t.flatList[index]); col != nil {
			t.editLastCol = col
		}
	}

	return true
}

// HandleMouseRelease handles mouse release.
// Only consumes the event when a drag was actually in progress:
// containers broadcast releases to every child, so an unconditional
// true here would starve sibling trinkets of their release.
func (t *TreeView) HandleMouseRelease(event core.MouseReleaseEvent) bool {
	if t.handleEditMouseRelease(event) {
		return true
	}
	// A drag-free release over the press-time candidate flips the
	// cell straight into edit mode.
	t.armClickEdit(event)
	if t.handleMultiRelease(event) {
		return true
	}
	if t.isDragging || t.scrollbarDragging {
		if t.scrollbarDragging {
			// Same stale-hover guard as the footer thumb: recompute from
			// the release point, and clear outright in TUI where no move
			// events arrive to do it later.
			t.scrollbarThumbHovered = core.FindGraphicalFrames(t.Self()) &&
				t.overScrollbarThumb(event.X, event.Y)
		}
		t.isDragging = false
		t.scrollbarDragging = false
		t.smoothScrollbarDrag = false
		t.Update()
		return true
	}
	return false
}

// HandleMouseWheel handles mouse wheel scrolling.
func (t *TreeView) HandleMouseWheel(event core.MouseWheelEvent) bool {
	// While a choice editor's drop-down is popped open, the tree does
	// NOT scroll - a grid moving under an anchored popup is nothing
	// but edge cases. The popup itself still takes the wheel (routed
	// to the overlay, not through here) when its items overflow.
	if t.rowEditing && t.editCombo != nil && t.editCombo.IsOpen() {
		return true
	}
	if len(t.flatList) == 0 {
		return false
	}

	// Horizontal wheel pans the column scroll region (scroll mode).
	if event.DeltaX != 0 && t.scrollHorizontally(event.DeltaX*2) {
		core.ClaimWheelGesture(event, t.HandleMouseWheel)
		return true
	}

	visibleCount := t.visibleCount()
	maxScroll := len(t.flatList) - visibleCount
	if maxScroll <= 0 {
		return false
	}

	// 3 rows per notch; trackpad precise deltas accumulate so slow
	// two-finger pans still move one row at a time.
	rows := 0
	if event.PreciseY != 0 {
		t.wheelAccum += event.PreciseY * 3
		rows = int(t.wheelAccum)
		t.wheelAccum -= float64(rows)
	} else if event.DeltaY < 0 {
		rows = -3
	} else if event.DeltaY > 0 {
		rows = 3
	}
	t.scrollOffset += rows
	if t.scrollOffset < 0 {
		t.scrollOffset = 0
	}
	if t.scrollOffset > maxScroll {
		t.scrollOffset = maxScroll
	}

	core.ClaimWheelGesture(event, t.HandleMouseWheel)
	t.Update()
	return true
}

// HandleFocusIn is called when focus is gained.
func (t *TreeView) HandleFocusIn() {
	// Auto-select first item if nothing is selected
	if t.currentIndex < 0 && len(t.flatList) > 0 {
		t.SetCurrentIndex(0)
	}
	// With a header, focus lands on the header BAR first (one stop);
	// Enter drills in, Tab moves on to the content. A content click
	// re-zones to content immediately (see HandleMousePress).
	if t.headerHeight() > 0 {
		t.setHeaderZone(hzBar, 0)
	} else {
		t.headerZone = hzContent
	}
	t.Update()
}

// HandleFocusOut is called when focus is lost.
func (t *TreeView) HandleFocusOut() {
	// Clear any active drag state when focus is lost
	t.isDragging = false
	t.scrollbarDragging = false
	t.headerZone = hzContent
	t.closeColumnChooser()
	// Focus moving elsewhere accepts an in-flight row edit.
	t.endRowEdit(true)
	t.cancelClickEdit()
	t.Update()
}

// AccessibleInfo returns accessibility information.
func (t *TreeView) AccessibleInfo() core.AccessibleInfo {
	info := t.AccessibleTrinket.AccessibleInfo()
	info.Role = core.RoleTree
	info.SetSize = len(t.flatList)

	if t.currentIndex >= 0 && t.currentIndex < len(t.flatList) {
		item := t.flatList[t.currentIndex]
		info.PositionInSet = t.currentIndex + 1
		info.Value = item.Text
		info.Level = item.Level() + 1

		if item.Expanded {
			info.State |= core.StateExpanded
		} else if !item.IsLeaf() {
			info.State |= core.StateCollapsed
		}
	}

	if !t.IsEnabled() {
		info.State |= core.StateDisabled
	}

	return info
}
