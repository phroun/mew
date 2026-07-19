package trinkets

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// Multi-column ("details" / "as List") support for TreeView. The tree
// itself - indent, arrows, icons, captions - is the KEY column; data
// columns render one cell value per item beside it. A header row
// (optional) shows captions, draggable dividers resize, and a [=]
// button above the scrollbar drops a show/hide checklist of the
// optional columns. The same layout drives the TUI (cell dividers,
// '=' button) and the pixel path (hairline dividers) - Finder's
// list view / Explorer's Details mode.

// TreeColumn describes one data column.
type TreeColumn struct {
	// ID is the stable key cell values are stored under (TreeItem.SetValue)
	// and the wire addresses.
	ID      string
	Caption string

	// Width is the current width in text cells; Min/MaxWidth bound
	// drag-resizing (MaxWidth 0 = unbounded).
	Width    int
	MinWidth int
	MaxWidth int

	// Align is "left" (default), "center", or "right".
	Align string

	// Resizable allows drag-resizing via the header divider.
	Resizable bool
	// Hidden removes the column from display (toggled by the chooser).
	Hidden bool
	// Optional lists the column in the [=] show/hide chooser.
	Optional bool
	// Sortable makes the header caption clickable: clicks toggle the
	// view's sort indicator on this column and fire the sort-request
	// callback. The VIEW only indicates and requests - reordering the
	// items is the application's job (it owns the data).
	Sortable bool
	// Numeric sorts this column by each cell's numeric equivalent
	// (parsed once when the value is set) instead of by text.
	Numeric bool
	// SortProxy redirects sorting: -1 (the default) means none; a
	// column index means THAT column's values are what actually sort
	// when this column is chosen. Captions stay representative while
	// a hidden column holds the machine-friendly sort values (e.g.
	// "2 KB" displayed, 2048 in a hidden numeric raw-size column).
	SortProxy int
	// Editable lets this column's cells be edited in place: Enter on
	// a row opens the row editor (see treeview_edit.go).
	Editable bool
	// Enum, when non-empty, makes an editable column a CHOICE: the
	// cell editor becomes a ComboBox over these options, and cells
	// always DISPLAY an option's Value. On the wire the column's
	// enum= property points at a collection of option objects.
	Enum []TreeEnumOption
	// EnumStore selects what the data field stores when an option is
	// chosen: "value" (the default) or "key".
	EnumStore string
}

// TreeEnumOption is one enum choice: a stable Key and the shown Value.
type TreeEnumOption struct {
	Key   string
	Value string
}

// displayValue maps a stored cell value to what the cell SHOWS: a
// key-storing enum column looks the key up and shows the option's
// Value; everything else (including a value-storing enum column,
// whose stored text IS the display) shows the raw text. An unknown
// key falls back to the raw text.
func (c *TreeColumn) displayValue(raw string) string {
	if len(c.Enum) == 0 || c.EnumStore != "key" {
		return raw
	}
	for _, o := range c.Enum {
		if o.Key == raw {
			return o.Value
		}
	}
	return raw
}

// NewTreeColumn creates a column with sensible defaults (resizable,
// optional, left-aligned, min width 3).
func NewTreeColumn(id, caption string, width int) *TreeColumn {
	if width < 1 {
		width = 8
	}
	return &TreeColumn{
		ID: id, Caption: caption, Width: width,
		MinWidth: 3, Align: "left", Resizable: true, Optional: true,
		SortProxy: -1, EnumStore: "value",
	}
}

// clampWidth bounds w to the column's Min/MaxWidth.
func (c *TreeColumn) clampWidth(w int) int {
	if w < c.MinWidth {
		w = c.MinWidth
	}
	if c.MaxWidth > 0 && w > c.MaxWidth {
		w = c.MaxWidth
	}
	if w < 1 {
		w = 1
	}
	return w
}

// --- TreeItem cell values ---

// SetValue sets this item's cell text for the given column ID. The
// numeric equivalent is parsed and cached HERE, once, so numeric sorts
// never re-convert text on every comparison.
func (t *TreeItem) SetValue(columnID, text string) {
	if t.Values == nil {
		t.Values = make(map[string]string)
	}
	if t.numValues == nil {
		t.numValues = make(map[string]float64)
	}
	t.Values[columnID] = text
	t.numValues[columnID] = parseNumericValue(text)
}

// Value returns this item's cell text for the given column ID.
func (t *TreeItem) Value(columnID string) string {
	return t.Values[columnID]
}

// NumericValue returns the cached numeric equivalent of this item's
// cell text for the column (0 when the text holds no number). Values
// written through SetValue are pre-parsed; a direct Values map write
// falls back to parsing here.
func (t *TreeItem) NumericValue(columnID string) float64 {
	if v, ok := t.numValues[columnID]; ok {
		return v
	}
	return parseNumericValue(t.Values[columnID])
}

// parseNumericValue extracts a cell's numeric equivalent: commas are
// stripped, leading/trailing non-numeric characters are ignored, and
// what remains must be a minus sign, digits, a potential decimal
// point, and more digits ("$-1,234.56 USD" -> -1234.56). Text with no
// digits at all is 0.
func parseNumericValue(s string) float64 {
	s = strings.ReplaceAll(s, ",", "")
	start := -1
	for i, r := range s {
		if r >= '0' && r <= '9' {
			start = i
			break
		}
	}
	if start < 0 {
		return 0
	}
	// A decimal point and/or minus sign directly before the first
	// digit belongs to the number (".5", "-3", "-.5").
	if start > 0 && s[start-1] == '.' {
		start--
	}
	if start > 0 && s[start-1] == '-' {
		start--
	}
	end := start
	seenDot := false
	for end < len(s) {
		switch ch := s[end]; {
		case ch >= '0' && ch <= '9':
		case ch == '.' && !seenDot:
			seenDot = true
		case ch == '-' && end == start:
		default:
			goto parse
		}
		end++
	}
parse:
	v, err := strconv.ParseFloat(s[start:end], 64)
	if err != nil {
		return 0
	}
	return v
}

// --- TreeView column API ---

// AddColumn appends a data column.
func (t *TreeView) AddColumn(c *TreeColumn) {
	t.columns = append(t.columns, c)
	t.Update()
}

// Columns returns the data columns (declared order, including hidden).
func (t *TreeView) Columns() []*TreeColumn { return t.columns }

// ColumnByID finds a column by its stable ID (nil if absent).
func (t *TreeView) ColumnByID(id string) *TreeColumn {
	for _, c := range t.columns {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// SetShowHeader shows or hides the header row (captions, dividers,
// the [=] chooser button).
func (t *TreeView) SetShowHeader(on bool) { t.showHeader = on; t.Update() }

// SetOnSortRequested installs the sort-request observer: activating a
// sortable header reports the new sort state here (the trinket already
// reordered its visual rows; this is notification, not a duty).
// sorted=false reports a return to the app's order; sortedBy is -1 for
// the key column, else the declared data-column index.
func (t *TreeView) SetOnSortRequested(fn func(sorted bool, sortedBy int, descending bool)) {
	t.onSortRequested = fn
}

// Sorted returns the view's sort state: whether visual sorting is
// active, which column it is on (-1 = the key column, else a declared
// data-column index), and the direction.
func (t *TreeView) Sorted() (sorted bool, sortedBy int, descending bool) {
	return t.sorted, t.sortedBy, t.sortDescending
}

// SetSorted sets the sort state. Sorting is VISUAL, built into the
// trinket: the logical item order (rootItems/Children - the app's
// order) is never touched; the flattened row list is regenerated with
// siblings in sorted order, and paint, hit-testing and scrolling all
// work off that list. Selection tracks the ITEM across the reorder,
// and events keep reporting item identity - the app can stay entirely
// unaware sorting is happening.
func (t *TreeView) SetSorted(sorted bool, sortedBy int, descending bool) {
	t.sorted, t.sortedBy, t.sortDescending = sorted, sortedBy, descending
	t.resortKeepingSelection()
}

// resortKeepingSelection regenerates the visual row list under the
// current sort state, keeping the selected ITEM selected (its visual
// index may change; its identity does not). If the selected item was
// IN VIEW before the operation, the viewport follows it to its new
// row; if the user had scrolled it out of view themselves, the
// viewport stays where they put it.
func (t *TreeView) resortKeepingSelection() {
	cur := t.CurrentItem()
	wasVisible := cur != nil &&
		t.currentIndex >= t.scrollOffset &&
		t.currentIndex < t.scrollOffset+t.visibleCount()
	t.rebuildFlatList()
	if cur != nil {
		t.restoreSelectionByItem(cur)
		if wasVisible {
			t.ensureVisible(t.currentIndex)
		}
	}
	t.Update()
}

// sortTarget resolves which column the sort actually runs on and how:
// the chosen column's SortProxy redirects to the column holding the
// real sort values (the indicator stays on the chosen column), and the
// target's Numeric flag selects float comparison. idx -1 = key column.
func (t *TreeView) sortTarget() (idx int, numeric bool) {
	idx = -1
	if t.sortedBy >= 0 && t.sortedBy < len(t.columns) {
		idx = t.sortedBy
		if p := t.columns[idx].SortProxy; p >= 0 && p < len(t.columns) && p != idx {
			idx = p
		}
		numeric = t.columns[idx].Numeric
	}
	return idx, numeric
}

// sortKeyFor is the string an item sorts by under the given sort
// target column (-1 = the key column's caption text).
func (t *TreeView) sortKeyFor(it *TreeItem, idx int) string {
	if idx >= 0 {
		return it.Value(t.columns[idx].ID)
	}
	return it.Text
}

// visualSiblings returns one sibling run in VISUAL order: the logical
// slice untouched when unsorted; a sorted copy otherwise (stable, so
// equal keys keep the app's order; descending reverses). Text columns
// compare case-insensitively; numeric targets compare the cached
// numeric equivalents. Children sort within their parent - the
// hierarchy is never flattened away, exactly like Finder's list view.
func (t *TreeView) visualSiblings(items []*TreeItem) []*TreeItem {
	if !t.sorted || len(items) < 2 {
		return items
	}
	out := make([]*TreeItem, len(items))
	copy(out, items)
	idx, numeric := t.sortTarget()
	if numeric {
		id := t.columns[idx].ID
		sort.SliceStable(out, func(i, j int) bool {
			a, b := out[i].NumericValue(id), out[j].NumericValue(id)
			if t.sortDescending {
				return b < a
			}
			return a < b
		})
		return out
	}
	sort.SliceStable(out, func(i, j int) bool {
		a := strings.ToLower(t.sortKeyFor(out[i], idx))
		b := strings.ToLower(t.sortKeyFor(out[j], idx))
		if t.sortDescending {
			return b < a
		}
		return a < b
	})
	return out
}

// columnIndex returns c's declared index (-1 for nil = the key column).
func (t *TreeView) columnIndex(c *TreeColumn) int {
	if c == nil {
		return -1
	}
	for i, tc := range t.columns {
		if tc == c {
			return i
		}
	}
	return -1
}

// treeHostColumn returns the data column hosting the tree affordances
// (expander arrow + nesting indent): the FIRST visible data column
// when the key column is hidden. Nesting must stay visible with the
// key off, so that column is forced LEFT-aligned regardless of its
// Align setting - an app that wants a right-aligned first column
// provides a proper key column instead. Nil when the key column is
// visible (it hosts the tree itself).
func (t *TreeView) treeHostColumn() *TreeColumn {
	if t.showKey || !t.anyVisibleData() {
		return nil
	}
	for _, c := range t.columns {
		if !c.Hidden {
			return c
		}
	}
	return nil
}

// sortIndicatorFor reports whether the indicator sits on this span's
// column (nil col = the key column).
func (t *TreeView) sortIndicatorFor(col *TreeColumn) bool {
	return t.sorted && t.sortedBy == t.columnIndex(col)
}

// headerSortClick handles activating a column header (mouse click or
// keyboard): the key column is always sortable; data columns opt in
// via Sortable. Activation CYCLES the sort: ascending -> descending ->
// unsorted (back to the app's order); a different column starts
// ascending.
func (t *TreeView) headerSortClick(col *TreeColumn) {
	if col != nil && !col.Sortable {
		return
	}
	by := t.columnIndex(col)
	sorted, descending := true, false
	if t.sorted && t.sortedBy == by {
		if !t.sortDescending {
			descending = true // second activation: reverse
		} else {
			sorted = false // third: back to unsorted
		}
	}
	t.SetSorted(sorted, by, descending)
	if t.onSortRequested != nil {
		t.onSortRequested(sorted, by, descending)
	}
}

// --- internal header focus zone ---

// Header focus zones: the tree is one trinket in the app tab order
// and runs its own focus machine inside (the window title-bar
// precedent - no changes to the focus manager; the only contract is
// "consume Tab while moving internally, release it at the ends").
const (
	hzContent = iota // the tree rows (the trinket's classic behavior)
	hzBar            // the whole header bar highlighted as ONE stop
	hzItems          // drilled in: cycling column captions + chooser
)

// headerStopCount is the number of drilled-in stops: one per visible
// column plus the chooser button when present.
func (t *TreeView) headerStopCount() int {
	n := len(t.visibleColumns())
	if _, ok := t.chooserButtonRect(); ok {
		n++
	}
	return n
}

// setHeaderZone transitions the internal focus zone and announces it
// for screen readers.
func (t *TreeView) setHeaderZone(zone, idx int) {
	t.headerZone, t.headerFocusIdx = zone, idx
	t.announceHeaderZone()
	t.Update()
}

func (t *TreeView) announceHeaderZone() {
	d, ok := t.desktopAncestor()
	if !ok || d.AccessibilityManager() == nil {
		return
	}
	am := d.AccessibilityManager()
	switch t.headerZone {
	case hzBar:
		am.AnnounceNavigation(fmt.Sprintf("Column header, %d columns. Press Enter to navigate columns.", len(t.visibleColumns())))
	case hzItems:
		am.AnnounceNavigation(t.headerStopLabel(t.headerFocusIdx))
	case hzContent:
		am.AnnounceNavigation("Tree content")
	}
}

// headerStopLabel describes one drilled-in stop for announcements.
func (t *TreeView) headerStopLabel(idx int) string {
	seq := t.visibleColumns()
	if idx >= len(seq) {
		return "Show columns, menu button"
	}
	col := seq[idx]
	name := t.keyCaption
	sortable := true
	if col != nil {
		name = col.Caption
		sortable = col.Sortable
	}
	state := "not sorted"
	if t.sortIndicatorFor(col) {
		if t.sortDescending {
			state = "sorted descending"
		} else {
			state = "sorted ascending"
		}
	} else if !sortable {
		state = "not sortable"
	}
	return fmt.Sprintf("%s column header, %s", name, state)
}

// isShiftTab matches both spellings of a backward Tab.
func isShiftTab(event core.KeyPressEvent) bool {
	return event.Key == "S-Tab" ||
		(event.Key == "Tab" && event.Modifiers&core.ShiftModifier != 0)
}

// handleHeaderFocusKey runs the header zones' keyboard model. Returns
// handled; content-zone keys fall through to the tree's own handling.
//
//	bar:    Enter/Space drill in - Tab -> content - S-Tab releases
//	        backward - Down -> content - Escape -> content
//	items:  Tab/Right next stop (past the chooser -> content) -
//	        S-Tab previous (before the first -> WRAP to the last
//	        stop, so the machine keeps the focus and a Tab from
//	        there exits down into the rows) - Left previous (before
//	        the first -> bar) - Enter/Space activates (sort cycle,
//	        or opens the chooser) - Escape -> bar
func (t *TreeView) handleHeaderFocusKey(event core.KeyPressEvent) bool {
	if t.headerHeight() == 0 {
		return false
	}
	shiftTab := isShiftTab(event)
	switch t.headerZone {
	case hzContent:
		// S-Tab backs into the header bar instead of leaving the
		// trinket; everything else is the content's business.
		if shiftTab {
			t.setHeaderZone(hzBar, 0)
			return true
		}
		return false
	case hzBar:
		switch {
		case shiftTab:
			t.headerZone = hzContent // release backward out of the trinket
			return false
		case event.Key == "Tab":
			t.setHeaderZone(hzContent, 0)
			return true
		case event.Key == "Enter" || event.Key == " " || event.Key == "Space":
			t.setHeaderZone(hzItems, 0)
			return true
		case event.Key == "Down" || event.Key == "Escape":
			t.setHeaderZone(hzContent, 0)
			return true
		}
		return true // the bar swallows other keys (it is the focus)
	case hzItems:
		n := t.headerStopCount()
		switch {
		case shiftTab:
			if t.headerFocusIdx == 0 {
				t.setHeaderZone(hzItems, n-1) // wrap to the chooser end
			} else {
				t.setHeaderZone(hzItems, t.headerFocusIdx-1)
			}
			return true
		case event.Key == "Left":
			if t.headerFocusIdx == 0 {
				t.setHeaderZone(hzBar, 0)
			} else {
				t.setHeaderZone(hzItems, t.headerFocusIdx-1)
			}
			return true
		case event.Key == "Tab" || event.Key == "Right":
			if t.headerFocusIdx+1 >= n {
				t.setHeaderZone(hzContent, 0)
			} else {
				t.setHeaderZone(hzItems, t.headerFocusIdx+1)
			}
			return true
		case event.Key == "Enter" || event.Key == " " || event.Key == "Space":
			seq := t.visibleColumns()
			if t.headerFocusIdx >= len(seq) {
				t.openColumnChooser(true)
			} else {
				t.headerSortClick(seq[t.headerFocusIdx])
				t.announceHeaderZone() // re-announce the new sort state
			}
			return true
		case event.Key == "Escape":
			t.setHeaderZone(hzBar, 0)
			return true
		}
		return true
	}
	return false
}

// SetShowKey controls whether the tree (key) column - the original
// single-column tree - is shown as the first visible column.
func (t *TreeView) SetShowKey(on bool) { t.showKey = on; t.Update() }

// SetLedger turns ledger banding on: non-selected rows alternate the
// scheme's LedgerOdd/LedgerEven colors (1-based: the first row is
// odd). Selection colors are untouched, and the blank area below the
// last item keeps the plain list background.
func (t *TreeView) SetLedger(on bool) { t.ledger = on; t.Update() }

// SetTreeLines turns connector lines on: every item gets a glyph (▪
// for leaves, the usual triangles for branches) and the indent space
// fills with the tree command's ├/└/│/─ segments. Purely a fill of the
// space the layout already reserves - no spacing changes.
func (t *TreeView) SetTreeLines(on bool) { t.treeLines = on; t.Update() }

// treeLinePrefix builds the connector cells for an item's indent
// region: one indentWidth-cell chunk per level, the last carrying the
// item's own ├/└ elbow dash-filled up to the glyph cell, earlier ones
// the │ continuation of any ancestor that still has visual siblings
// below. Length is exactly level*indentWidth (nil at level 0).
func (t *TreeView) treeLinePrefix(item *TreeItem) []rune {
	level := item.Level()
	if level <= 0 {
		return nil
	}
	out := make([]rune, level*t.indentWidth)
	for i := range out {
		out[i] = ' '
	}
	// chain[k] is the ancestor-or-self at level k+1: its connector
	// state fills chunk k (columns [k*indentWidth, (k+1)*indentWidth)).
	chain := make([]*TreeItem, level)
	for n, k := item, level-1; k >= 0; n, k = n.Parent, k-1 {
		chain[k] = n
	}
	for k, n := range chain {
		at := k * t.indentWidth
		if k == level-1 {
			if t.hasNextVisualSibling(n) {
				out[at] = '├'
			} else {
				out[at] = '└'
			}
			for i := at + 1; i < len(out); i++ {
				out[i] = '─'
			}
		} else if t.hasNextVisualSibling(n) {
			out[at] = '│'
		}
	}
	return out
}

// drawTreeLineCell paints one connector cell. The TUI uses the box
// glyphs; pixel surfaces draw the same segments as real 1px lines in
// the glyph's position, spanning the whole cell so consecutive rows
// and columns connect without font gaps.
func (t *TreeView) drawTreeLineCell(p *core.Painter, x, y core.Unit, r rune, s style.CellStyle, metrics core.CellMetrics) {
	if !p.Graphical() {
		p.DrawCell(x, y, r, s)
		return
	}
	cw, ch := metrics.CellWidth, metrics.CellHeight
	cx := x + cw/2 // the glyph's vertical stroke position
	cy := y + ch/2 // the glyph's horizontal stroke position
	fr, fg, fb := s.Fg.RGBComponents()
	vert := func(from, to core.Unit) {
		p.FillRectPixelsAlpha(cx, from, 0, 0, 1, p.UnitSpanPxY(from, to), fr, fg, fb, 1)
	}
	horiz := func(from core.Unit) {
		p.FillRectPixelsAlpha(from, cy, 0, 0, p.UnitSpanPxX(from, x+cw), 1, fr, fg, fb, 1)
	}
	switch r {
	case '│':
		vert(y, y+ch)
	case '├':
		vert(y, y+ch)
		horiz(cx)
	case '└':
		// The elbow's vertical leg meets the horizontal stroke (one
		// extra pixel so the corner is closed).
		p.FillRectPixelsAlpha(cx, y, 0, 0, 1, p.UnitSpanPxY(y, cy)+1, fr, fg, fb, 1)
		horiz(cx)
	case '─':
		horiz(x)
	}
}

// hasNextVisualSibling reports whether another sibling follows item in
// the VISUAL (sorted) order - the state that picks ├ vs └ and runs the
// │ continuation through deeper rows.
func (t *TreeView) hasNextVisualSibling(item *TreeItem) bool {
	siblings := t.rootItems
	if item.Parent != nil {
		siblings = item.Parent.Children
	}
	vs := t.visualSiblings(siblings)
	for i, s := range vs {
		if s == item {
			return i+1 < len(vs)
		}
	}
	return false
}

// SetKeyCaption sets the header caption over the key (tree) column -
// "Name" in a file listing.
func (t *TreeView) SetKeyCaption(s string) { t.keyCaption = s; t.Update() }

// KeyCaption returns the key column's header caption.
func (t *TreeView) KeyCaption() string { return t.keyCaption }

// SetFitWidth selects fit mode: true squeezes columns into the width
// (no horizontal scrolling, the key column absorbs slack); false uses
// natural widths and scrolls horizontally as needed.
func (t *TreeView) SetFitWidth(on bool) {
	t.fitWidth = on
	if on {
		t.hScroll = 0
	}
	t.Update()
}

// SetFixedColumns pins the first left / last right visible columns
// outside the horizontal scrolling region.
func (t *TreeView) SetFixedColumns(left, right int) {
	if left < 0 {
		left = 0
	}
	if right < 0 {
		right = 0
	}
	t.fixedLeft, t.fixedRight = left, right
	t.Update()
}

// SetKeyWidth sets the tree column's width in text cells for scroll
// mode (fit mode sizes it to the leftover space automatically).
func (t *TreeView) SetKeyWidth(cells int) {
	if cells < treeKeyMinCells {
		cells = treeKeyMinCells
	}
	t.keyWidth = cells
	t.Update()
}

const (
	treeKeyMinCells     = 6  // narrowest useful tree column
	treeKeyDefaultCells = 20 // scroll-mode default tree column width

	// treeLeftPadCells is breathing room before the tree apparatus:
	// the tree-host column's content starts one cell in from its left
	// edge, painted in the row's own background like the rest of the
	// glyph/margin area.
	treeLeftPadCells = 1
)

// multiColumn reports whether the multi-column presentation is active
// (any data column declared, or an explicit header request).
func (t *TreeView) multiColumn() bool {
	return len(t.columns) > 0 || t.showHeader
}

// headerHeight is the header row's height (0 when hidden).
func (t *TreeView) headerHeight() core.Unit {
	if !t.multiColumn() || !t.showHeader {
		return 0
	}
	return t.EffectiveCellMetrics().CellHeight
}

// footerHeight is the horizontal scrollbar band's height, reserved
// whenever horizontal scrolling is enabled (scroll mode) so content
// never sits under the bar. The TUI needs a whole display row; pixel
// surfaces use the standard slim scrollbar height (one column's worth
// of units - the same lane size the vertical bar uses sideways).
func (t *TreeView) footerHeight() core.Unit {
	if !t.multiColumn() || t.fitWidth {
		return 0
	}
	metrics := t.EffectiveCellMetrics()
	if core.FindGraphicalFrames(t.Self()) {
		return metrics.CellWidth
	}
	return metrics.CellHeight
}

// colSpan is one visible column's placement for this paint/hit pass.
type colSpan struct {
	col   *TreeColumn // nil = the tree (key) column
	x     core.Unit   // content left edge (post-scroll)
	w     core.Unit   // content width
	divX  core.Unit   // divider column right of the span (-1 = none)
	fixed bool        // pinned outside the horizontal scroll region
}

// treeColLayout is the computed column geometry for one pass.
type treeColLayout struct {
	spans      []colSpan
	headerH    core.Unit
	blankCells int       // trailing blank right of the last span (natural)
	contentW   core.Unit // width left of the scrollbar lane
	scrollL    core.Unit // horizontal scroll region [scrollL, scrollR)
	scrollR    core.Unit
	maxHScroll int // in cells
}

// visibleColumns returns the visible sequence: key column (nil entry)
// first when showKey, then unhidden data columns in declared order.
// A tree with no visible data columns always shows the key column.
func (t *TreeView) visibleColumns() []*TreeColumn {
	var seq []*TreeColumn
	if t.showKey || !t.anyVisibleData() {
		seq = append(seq, nil)
	}
	for _, c := range t.columns {
		if !c.Hidden {
			seq = append(seq, c)
		}
	}
	return seq
}

func (t *TreeView) anyVisibleData() bool {
	for _, c := range t.columns {
		if !c.Hidden {
			return true
		}
	}
	return false
}

// columnLayout computes the visible spans. Widths are cell-quantized
// (the TUI's natural grid; the pixel path shares it so dividers land
// identically on both). Dividers occupy one cell between spans. In
// fit mode the key column absorbs slack and data columns shrink
// toward MinWidth on overflow; in scroll mode natural widths stand
// and the non-fixed spans pan by hScroll cells.
func (t *TreeView) columnLayout() treeColLayout {
	metrics := t.EffectiveCellMetrics()
	cw := metrics.CellWidth
	bounds := t.Bounds()
	lay := treeColLayout{headerH: t.headerHeight()}
	lay.contentW = bounds.Width - cw // scrollbar lane
	if lay.contentW < cw {
		lay.contentW = cw
	}
	contentCells := int(lay.contentW / cw)

	seq := t.visibleColumns()
	n := len(seq)
	widths := make([]int, n) // cells
	keyIdx := -1
	for i, c := range seq {
		if c == nil {
			keyIdx = i
			continue
		}
		widths[i] = c.clampWidth(c.Width)
	}
	// A divider consumes one cell only on cell surfaces (it renders as a
	// full '│' character there); pixel surfaces draw a hairline ON the
	// span boundary and reserve nothing.
	divCells := 1
	if core.FindGraphicalFrames(t.Self()) {
		divCells = 0
	}
	dividers := (n - 1) * divCells
	if dividers < 0 {
		dividers = 0
	}

	if keyIdx >= 0 {
		if t.fitWidth {
			used := dividers
			for i, w := range widths {
				if i != keyIdx {
					used += w
				}
			}
			keyW := contentCells - used
			// Under pressure the key column reclaims data columns'
			// slack in two passes: first down to each column's
			// MEASURED content width (measured with the effective
			// font, so pixel surfaces measure the proportional text
			// as drawn, not a rune count - a declared width that is
			// only padding gives its slack up before anything visibly
			// truncates), then down to the hard MinWidth.
			desired := t.keyWidth
			if desired <= 0 {
				desired = treeKeyDefaultCells
			}
			if keyW < desired {
				keyW += t.reclaimWidths(seq, widths, keyIdx, desired-keyW, true)
			}
			if keyW < treeKeyMinCells {
				keyW += t.reclaimWidths(seq, widths, keyIdx, treeKeyMinCells-keyW, false)
				if keyW < treeKeyMinCells {
					keyW = treeKeyMinCells // genuine overflow: clip
				}
			}
			widths[keyIdx] = keyW
		} else {
			kw := t.keyWidth
			if kw <= 0 {
				kw = treeKeyDefaultCells
			}
			widths[keyIdx] = kw
		}
	} else if t.fitWidth {
		// No key column: shrink data columns on overflow the same way
		// (measured floors first, hard minimums after).
		used := dividers
		for _, w := range widths {
			used += w
		}
		if over := used - contentCells; over > 0 {
			over -= t.reclaimWidths(seq, widths, -1, over, true)
			if over > 0 {
				t.reclaimWidths(seq, widths, -1, over, false)
			}
		}
	}

	// Fixed pinning: clamp counts, and in fit mode everything is fixed.
	fl, fr := t.fixedLeft, t.fixedRight
	if fl+fr > n {
		fl = n
		fr = 0
	}

	// Widths of the pinned flanks (with their trailing/leading dividers).
	leftCells := 0
	for i := 0; i < fl; i++ {
		leftCells += widths[i] + divCells // span + divider
	}
	rightCells := 0
	for i := n - fr; i < n; i++ {
		rightCells += divCells + widths[i] // divider + span
	}
	scrollCells := contentCells - leftCells - rightCells
	if scrollCells < 0 {
		scrollCells = 0
	}
	lay.scrollL = core.Unit(leftCells) * cw
	lay.scrollR = lay.scrollL + core.Unit(scrollCells)*cw

	// Natural width of the scrolling region's spans.
	midCells := 0
	for i := fl; i < n-fr; i++ {
		midCells += widths[i]
		if i < n-fr-1 {
			midCells += divCells // divider between scrolling spans
		}
	}
	if !t.fitWidth && midCells > scrollCells {
		lay.maxHScroll = midCells - scrollCells
	}
	hs := t.hScroll
	if hs > lay.maxHScroll {
		hs = lay.maxHScroll
	}
	if hs < 0 {
		hs = 0
	}

	// Emit spans left to right.
	xCells := 0
	for i := 0; i < n; i++ {
		fixed := i < fl || i >= n-fr
		if i == fl && !fixed || (i == fl && fl < n-fr) {
			// entering the scrolling region
			xCells = leftCells - hs
		}
		if i == n-fr && fr > 0 {
			// After the flank's leading divider: a reserved cell in the
			// TUI, nothing on pixel surfaces (the hairline sits ON the
			// boundary - a hardcoded +1 left a one-cell gap there).
			xCells = contentCells - rightCells + divCells
		}
		sp := colSpan{
			col:   seq[i],
			x:     core.Unit(xCells) * cw,
			w:     core.Unit(widths[i]) * cw,
			fixed: fixed,
			divX:  -1,
		}
		xCells += widths[i]
		if i < n-1 {
			sp.divX = core.Unit(xCells) * cw
			xCells += divCells
		}
		lay.spans = append(lay.spans, sp)
	}
	// Pin the divider between the scroll region and the right flank.
	if fr > 0 && n-fr-1 >= 0 {
		lay.spans[n-fr-1].divX = lay.scrollR
	}
	// The last span before the right flank (the very last span when
	// nothing is pinned right) stretches over any trailing blank width
	// - nothing sits between it and the region's edge, so its content
	// must not ellipsize while free space goes unused. The blank's
	// NATURAL size is recorded first: the fit-mode drag pool feeds on
	// it (widths[] stay natural throughout).
	if idx := n - 1 - fr; idx >= 0 {
		last := &lay.spans[idx]
		if end := last.x + last.w; end < lay.scrollR {
			lay.blankCells = int((lay.scrollR - end) / cw)
			last.w = lay.scrollR - last.x
		}
	}
	return lay
}

// neededCells is the narrowest width (in cells) that still shows this
// column's content: the header caption (plus sort-indicator room) and
// every current row's value, measured with the EFFECTIVE FONT - so on
// pixel surfaces the proportional text is measured as drawn, not as a
// rune count - rounded up to whole cells with half a cell of padding.
// (Measures every row per layout pass; fine at UI scale, cache if a
// huge tree ever makes it hot.)
func (t *TreeView) neededCells(col *TreeColumn) int {
	font := t.EffectiveFont()
	cw := t.EffectiveCellMetrics().CellWidth
	maxW := font.MeasureText(col.Caption)
	if t.sortIndicatorFor(col) {
		maxW += font.MeasureText(" ▲")
	}
	host := t.treeHostColumn() == col // carries expander + indent
	for _, it := range t.flatList {
		w := font.MeasureText(col.displayValue(it.Value(col.ID)))
		if host {
			w += core.Unit(it.Level()*t.indentWidth+1+treeLeftPadCells) * cw
		}
		if w > maxW {
			maxW = w
		}
	}
	cells := int((maxW + cw/2 + cw - 1) / cw) // half-cell pad, ceil
	if col.Editable && len(col.Enum) > 0 {
		// A choice column's editor is a ComboBox: keep room for its
		// drop-down arrow even while not editing, so entering edit
		// mode never truncates the value.
		cells++
	}
	if cells < col.MinWidth {
		cells = col.MinWidth
	}
	return cells
}

// reclaimWidths takes up to need cells from the data columns
// (rightmost first), never below each column's floor: its MEASURED
// content width when measured=true (see neededCells), else the hard
// MinWidth. keyIdx is skipped (-1 = none). Returns the cells reclaimed.
func (t *TreeView) reclaimWidths(seq []*TreeColumn, widths []int, keyIdx, need int, measured bool) int {
	got := 0
	for i := len(seq) - 1; i >= 0 && need > 0; i-- {
		if i == keyIdx || seq[i] == nil {
			continue
		}
		floor := seq[i].MinWidth
		if measured {
			if f := t.neededCells(seq[i]); f > floor {
				floor = f
			}
			if floor > widths[i] {
				floor = widths[i] // a floor never grows a column
			}
		}
		give := widths[i] - floor
		if give > need {
			give = need
		}
		if give > 0 {
			widths[i] -= give
			need -= give
			got += give
		}
	}
	return got
}

// spanClip returns the clip rect for a span (fixed spans clip to
// themselves; scrolling spans additionally clip to the scroll region).
func (l *treeColLayout) spanClip(sp colSpan, height core.Unit) (core.UnitRect, bool) {
	x0, x1 := sp.x, sp.x+sp.w
	if !sp.fixed {
		if x0 < l.scrollL {
			x0 = l.scrollL
		}
		if x1 > l.scrollR {
			x1 = l.scrollR
		}
	}
	if x1 > l.contentW {
		x1 = l.contentW
	}
	if x0 < 0 {
		x0 = 0
	}
	if x1 <= x0 {
		return core.UnitRect{}, false
	}
	return core.UnitRect{X: x0, Y: 0, Width: x1 - x0, Height: height}, true
}

// divVisible reports whether a divider at divX should paint (dividers
// belonging to the scrolling region hide once panned outside it).
func (l *treeColLayout) divVisible(sp colSpan) bool {
	if sp.divX < 0 {
		return false
	}
	if sp.fixed || sp.divX == l.scrollR {
		return sp.divX < l.contentW
	}
	return sp.divX >= l.scrollL && sp.divX < l.scrollR
}

// --- painting ---

// paintMulti renders the multi-column presentation: header, per-column
// rows, dividers, chooser button, scrollbar.
func (t *TreeView) paintMulti(p *core.Painter) {
	bounds := t.Bounds()
	scheme := t.GetScheme()
	focused := t.HasFocus()
	metrics := t.EffectiveCellMetrics()
	font := t.EffectiveFont()
	lay := t.columnLayout()

	bgStyle := style.DefaultStyle().WithFg(scheme.GetListFG()).WithBg(scheme.GetListBG())
	p.FillRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, ' ', bgStyle)

	// Header row, in the scheme's Header band colors (ledger or not).
	// The internal focus zone lights it up: hzBar paints the WHOLE bar
	// as one focused stop; hzItems highlights just the focused caption
	// (or the chooser button) below.
	headerStyle := scheme.GetHeader()
	if lay.headerH > 0 {
		if focused && t.headerZone == hzBar {
			// The header is a CONTROL bar, not a list item: its focus
			// stop wears a button face.
			headerStyle = scheme.GetFocusedListButton()
		}
		if !p.Graphical() {
			headerStyle = headerStyle.Underline()
		}
		p.FillRect(core.UnitRect{Width: bounds.Width, Height: lay.headerH}, ' ', headerStyle)
		for i, sp := range lay.spans {
			clip, ok := lay.spanClip(sp, lay.headerH)
			if !ok {
				continue
			}
			caption := t.keyCaption // the key column's header caption
			if sp.col != nil {
				caption = sp.col.Caption
			}
			spanStyle := headerStyle
			if focused && t.headerZone == hzItems && t.headerFocusIdx == i {
				// Drilled-in focus on this caption (a button face - the
				// caption acts like a sort button here).
				spanStyle = scheme.GetFocusedListButton()
				if !p.Graphical() {
					spanStyle = spanStyle.Underline()
				}
				p.FillRect(core.UnitRect{X: clip.X, Y: 0, Width: clip.Width, Height: lay.headerH}, ' ', spanStyle)
			}
			headerStyle := spanStyle
			cp := p.WithClip(clip)
			// Sort indicator on the active sort column - key included.
			// Same glyph family as the tree's expander ('▼' and its
			// inverse), right-aligned in the span so it reads as the
			// header's affordance, not part of the caption.
			if t.sortIndicatorFor(sp.col) {
				arrow := "▲"
				if t.sortDescending {
					arrow = "▼"
				}
				t.drawAligned(cp, arrow, sp, 0, headerStyle, font, "right")
				// Keep the caption clear of the arrow.
				capSp := sp
				if room := font.MeasureText(arrow) + metrics.CellWidth; capSp.w > room {
					capSp.w -= room
				}
				t.drawAligned(cp, caption, capSp, 0, headerStyle, font, "left")
			} else {
				t.drawAligned(cp, caption, sp, 0, headerStyle, font, "left")
			}
		}
		if p.Graphical() {
			// Hairline under the header (the TUI uses the underline attr).
			fr, fg, fb := scheme.GetListFG().RGBComponents()
			p.FillRectPixelsAlpha(0, lay.headerH, 0, -1,
				p.UnitSpanPxX(0, bounds.Width), 1, fr, fg, fb, 0.6)
		}
		// The chooser button paints AFTER the hscroll edge fades (below):
		// the graphical button is two cells wide, so its left half sits
		// inside the content area the right-edge fade covers.
	}

	// Rows. Row styles are collected for the horizontal-scroll edge
	// fades, which must match each band's own background (header,
	// selected row, plain rows). enterCol is the cell Enter would edit
	// right now - it alone wears FocusedListItem on the focused row.
	var enterCol *TreeColumn
	if focused && t.headerZone == hzContent && !t.chooserOpen && !t.rowEditing {
		enterCol = t.enterTargetColumn()
	}
	visibleCount := t.visibleCount()
	rows := visibleCount
	// GUI: one extra PARTIAL row peeks into the leftover strip / under
	// the footer scrollbar instead of leaving that space blank (the bar
	// overlays it). It never joins visibleCount, so the scrolling math
	// does not treat the clipped row as visible.
	if p.Graphical() && t.scrollOffset+visibleCount < len(t.flatList) &&
		lay.headerH+core.Unit(visibleCount)*metrics.CellHeight < bounds.Height {
		rows++
	}
	// Per-row fade colors for the horizontal-scroll edge fades: usually
	// the row band, but the Enter-target's FocusedListItem segment can
	// sit exactly at a scroll-region edge - the fade there must blend
	// toward the CELL's color, not the band behind it.
	fadeL := make([]style.Color, 0, rows)
	fadeR := make([]style.Color, 0, rows)
	rowBands := make([]style.Color, 0, rows) // full-row band bg (vertical fades)
	for i := 0; i < rows; i++ {
		itemIndex := t.scrollOffset + i
		if itemIndex >= len(t.flatList) {
			break
		}
		item := t.flatList[itemIndex]
		itemY := lay.headerH + core.Unit(i)*metrics.CellHeight

		// While the internal focus sits in the header (bar or drilled
		// items), the column chooser menu is popped down, or the cell
		// editor is up, the selected row shows the NON-focused
		// selection color - the header/menu/editor owns the focus, the
		// row is just selected, and two focus-colored things at once
		// would read as two focuses.
		rowFocused := focused && t.headerZone == hzContent &&
			!t.chooserOpen && !t.rowEditing
		ledgerRow := false
		var s style.CellStyle
		switch {
		case !item.Enabled:
			s = style.DefaultStyle().WithFg(scheme.GetDisabledTextFG()).WithBg(scheme.GetListBG())
		case itemIndex == t.currentIndex && rowFocused:
			if t.hasEditableColumns() {
				// Editable grid: the plain selection bar carries the
				// row; the Enter-target cell alone wears
				// FocusedListItem (painted per-span below).
				s = scheme.GetSelectedListItem()
			} else {
				s = scheme.GetFocusedListItem()
			}
		case itemIndex == t.currentIndex:
			if t.rowEditing {
				// The row UNDER the live editor wears FocusedListRow -
				// distinct from the editor floating over it AND from a
				// plain resting selection.
				s = scheme.GetFocusedListRow()
			} else {
				s = scheme.GetSelectedListItem()
			}
		case t.ledger:
			// Ledger banding (non-selected rows only), 1-based: the
			// first visual row is odd.
			ledgerRow = true
			if itemIndex%2 == 0 {
				s = scheme.GetLedgerOdd()
			} else {
				s = scheme.GetLedgerEven()
			}
		default:
			s = bgStyle
		}
		fadeLBG, fadeRBG := s.Bg, s.Bg
		if itemIndex == t.currentIndex || ledgerRow {
			// Selection and ledger bands span the full row. On pixel
			// surfaces they run under the scrollbar lane too (the slim
			// scrollbar overlays them); the TUI reserves that column
			// for the full-cell scrollbar glyphs.
			rowW := lay.contentW
			if p.Graphical() {
				rowW = bounds.Width
			}
			p.FillRect(core.UnitRect{X: 0, Y: itemY, Width: rowW, Height: metrics.CellHeight}, ' ', s)
		}

		host := t.treeHostColumn()
		for _, sp := range lay.spans {
			clip, ok := lay.spanClip(sp, bounds.Height)
			if !ok {
				continue
			}
			// The Enter-target cell on the focused selected row wears
			// FocusedListItem over the FocusedListRow band. A data
			// column highlights its whole cell; the tree-hosting cell
			// highlights only the caption's CLICKABLE zone (the same
			// zone the mouse resolver uses), leaving the apparatus and
			// the space right of the text in the row style.
			cellStyle := s
			targetSegW := core.Unit(0)
			var targetSegX core.Unit
			if itemIndex == t.currentIndex && rowFocused && enterCol != nil &&
				spanMatchesCol(sp, enterCol) {
				cellStyle = scheme.GetFocusedListItem()
				segX, segW := sp.x, sp.w
				if sp.col == nil || (host != nil && sp.col == host) {
					segX, segW = t.treeCellEditZone(sp, item)
				}
				if segX < clip.X {
					segW -= clip.X - segX
					segX = clip.X
				}
				if end := clip.X + clip.Width; segX+segW > end {
					segW = end - segX
				}
				if segW > 0 {
					p.FillRect(core.UnitRect{X: segX, Y: itemY, Width: segW, Height: metrics.CellHeight}, ' ', cellStyle)
					targetSegX, targetSegW = segX, segW
					// The segment under a fade zone retints that row's
					// fade: blend toward the cell, not the row band.
					if segX <= lay.scrollL && lay.scrollL < segX+segW {
						fadeLBG = cellStyle.Bg
					}
					if segX < lay.scrollR && lay.scrollR <= segX+segW {
						fadeRBG = cellStyle.Bg
					}
				}
			}
			cp := p.WithClip(clip)
			switch {
			case sp.col == nil:
				t.paintTreeCell(cp, item, sp, itemY, s, cellStyle, metrics, font, item.Text)
			case sp.col == host:
				// Key column hidden: this column carries the expander
				// and indent (and is forced left-aligned for it).
				t.paintTreeCell(cp, item, sp, itemY, s, cellStyle, metrics, font, sp.col.displayValue(item.Value(sp.col.ID)))
			default:
				t.drawAligned(cp, sp.col.displayValue(item.Value(sp.col.ID)), sp, itemY, cellStyle, font, sp.col.Align)
			}
			// A CHOICE Enter-target advertises its editor: the combo's
			// down arrow, right-aligned in the highlight (over the
			// content, like a real combo box's arrow).
			if targetSegW > 0 && enterCol != treeKeyColumn && len(enterCol.Enum) > 0 {
				arrow := "▼"
				ax := targetSegX + targetSegW - font.MeasureText(arrow)
				if p.Graphical() {
					ax -= 2
				}
				if ax >= targetSegX {
					cp.DrawText(ax, itemY, arrow, cellStyle, font)
				}
			}
		}
		fadeL = append(fadeL, fadeLBG)
		fadeR = append(fadeR, fadeRBG)
		rowBands = append(rowBands, s.Bg)
	}

	// Dividers, over the rows, down to the footer row (the reserved
	// horizontal-scrollbar row stays clear). Two passes around the edge
	// fades: scrolling-region dividers go UNDER the fades (they fade
	// out with the content they belong to), but the pinned regions'
	// boundary dividers go OVER them - the left fade starts exactly on
	// the last pinned-left column's hairline, and without the second
	// pass scrolling would erase it and merge the two columns.
	divBottom := bounds.Height - t.footerHeight()
	divStyle := style.DefaultStyle().WithFg(scheme.GetScrollbar().Fg).WithBg(scheme.GetListBG())
	paintDividers := func(fixedPass bool) {
		for _, sp := range lay.spans {
			if !lay.divVisible(sp) {
				continue
			}
			if (sp.fixed || sp.divX == lay.scrollR) != fixedPass {
				continue
			}
			if p.Graphical() {
				// No divider cell on pixel surfaces: the hairline sits
				// ON the span boundary.
				fr, fg, fb := scheme.GetListFG().RGBComponents()
				p.FillRectPixelsAlpha(sp.divX, 0, 0, 0,
					1, p.UnitSpanPxY(0, divBottom), fr, fg, fb, 0.35)
			} else {
				for y := core.Unit(0); y < divBottom; y += metrics.CellHeight {
					st := divStyle
					if y < lay.headerH {
						st = st.Underline()
					}
					p.DrawCell(sp.divX, y, '│', st)
				}
			}
		}
	}
	paintDividers(false)

	// Edge fades over the scrolled content (under the scrollbars),
	// banded so each fade matches the background it covers.
	t.paintHScrollFades(p, lay, headerStyle, fadeL, fadeR, bgStyle)
	paintDividers(true)
	t.paintVScrollFades(p, lay, rowBands, bgStyle)
	if lay.headerH > 0 {
		t.paintChooserButton(p, lay, headerStyle)
	}
	t.paintRowEditor(p)

	if t.footerHeight() > 0 {
		t.paintHScrollbar(p, lay)
	}
	if len(t.flatList) > visibleCount {
		t.paintScrollbar(p, visibleCount)
	}
}

// paintHScrollFades draws the ScrollArea-style edge gradients on the
// horizontal scroll region: a two-column fade from opaque at the
// region's edge to transparent inward, on whichever side has more
// content beyond it. Because the rows behind it carry different
// backgrounds, the fade is painted in BANDS - the header with the
// header's background, the selected row with the selection color, the
// other rows and the empty area with the list background - so every
// strip fades into exactly what it covers. Pixel surfaces only.
func (t *TreeView) paintHScrollFades(p *core.Painter, lay treeColLayout, headerStyle style.CellStyle, fadeL, fadeR []style.Color, bgStyle style.CellStyle) {
	if !p.Graphical() || t.fitWidth {
		return
	}
	showLeft := t.hScroll > 0
	showRight := t.hScroll < lay.maxHScroll
	if !showLeft && !showRight {
		return
	}
	metrics := t.EffectiveCellMetrics()
	wtPx := p.UnitSpanPxX(0, metrics.CellWidth*2)
	if regionPx := p.UnitSpanPxX(lay.scrollL, lay.scrollR); wtPx > regionPx/2 {
		wtPx = regionPx / 2
	}
	if wtPx <= 0 {
		return
	}
	bottom := t.Bounds().Height - t.footerHeight()

	// Vertical bands, top to bottom. Each side of a band carries its
	// own fade color: they differ when the Enter-target's cell fill
	// sits at one scroll-region edge but not the other.
	type band struct {
		y0, y1 core.Unit
		l, r   style.Color
	}
	var bands []band
	y := core.Unit(0)
	if lay.headerH > 0 {
		bands = append(bands, band{0, lay.headerH, headerStyle.Bg, headerStyle.Bg})
		y = lay.headerH
	}
	for i := range fadeL {
		bands = append(bands, band{y, y + metrics.CellHeight, fadeL[i], fadeR[i]})
		y += metrics.CellHeight
	}
	if y < bottom {
		bands = append(bands, band{y, bottom, bgStyle.Bg, bgStyle.Bg})
	}

	alphaAt := func(d int) float64 { return 1.0 - (float64(d)+0.5)/float64(wtPx) }
	for _, b := range bands {
		hPx := p.UnitSpanPxY(b.y0, b.y1)
		if hPx <= 0 {
			continue
		}
		lr, lg, lb := b.l.RGBComponents()
		rr, rg, rb := b.r.RGBComponents()
		for j := 0; j < wtPx; j++ {
			a := alphaAt(j)
			if showLeft {
				p.FillRectPixelsAlpha(lay.scrollL, b.y0, j, 0, 1, hPx, lr, lg, lb, a)
			}
			if showRight {
				p.FillRectPixelsAlpha(lay.scrollR, b.y0, -j-1, 0, 1, hPx, rr, rg, rb, a)
			}
		}
	}
}

// paintVScrollFades fades the content region's top/bottom edges when
// more rows lie beyond them (pixel surfaces only), banded so each
// pixel row blends toward the background of the item row under it -
// the ListView treatment, between the header and the footer. Painted
// over the dividers (they fade with their content), under the row
// editor and the scrollbars.
func (t *TreeView) paintVScrollFades(p *core.Painter, lay treeColLayout, rowBands []style.Color, bgStyle style.CellStyle) {
	if !p.Graphical() {
		return
	}
	visibleCount := t.visibleCount()
	maxScroll := len(t.flatList) - visibleCount
	showTop := t.scrollOffset > 0
	showBottom := maxScroll > 0 && t.scrollOffset < maxScroll
	if !showTop && !showBottom {
		return
	}
	bounds := t.Bounds()
	metrics := t.EffectiveCellMetrics()
	top := lay.headerH
	// The bottom fade anchors at the TRUE bottom and runs through the
	// partial peek strip in one continuous gradient (a fade stopping
	// at the footer would let the peek row snap back to full
	// brightness below it); the footer bar overlays the deep end.
	bottom := bounds.Height
	regionPx := p.UnitSpanPxY(top, bottom)
	rowDeep := p.UnitSpanPxY(0, metrics.CellHeight)
	leftover := bottom - (top + core.Unit(t.visibleCount())*metrics.CellHeight)
	if leftover < 0 {
		leftover = 0
	}
	wPx := p.UnitSpanPxX(0, bounds.Width)
	rowPx := rowDeep
	bgAt := func(px int) style.Color {
		if rowPx > 0 {
			if idx := px / rowPx; idx >= 0 && idx < len(rowBands) {
				return rowBands[idx]
			}
		}
		return bgStyle.Bg
	}
	fade := func(deep int, topSide bool) {
		if deep > regionPx/2 {
			deep = regionPx / 2
		}
		if deep <= 0 {
			return
		}
		for j := 0; j < deep; j++ {
			a := 1.0 - (float64(j)+0.5)/float64(deep)
			if topSide {
				r, g, b := bgAt(j).RGBComponents()
				p.FillRectPixelsAlpha(0, top, 0, j, wPx, 1, r, g, b, a)
			} else {
				r, g, b := bgAt(regionPx - 1 - j).RGBComponents()
				p.FillRectPixelsAlpha(0, bottom, 0, -j-1, wPx, 1, r, g, b, a)
			}
		}
	}
	if showTop {
		fade(rowDeep, true)
	}
	if showBottom {
		fade(rowDeep+p.UnitSpanPxY(0, leftover), false)
	}
}

// paintTreeCell draws a tree-hosting cell for one row: indent,
// expander, icon, then the given text (the key column's caption, or -
// with the key hidden - the host data column's value), constrained to
// the span and always left-aligned.
func (t *TreeView) paintTreeCell(p *core.Painter, item *TreeItem, sp colSpan, itemY core.Unit, s, textStyle style.CellStyle, metrics core.CellMetrics, font *core.Font, text string) {
	level := item.Level()
	x := sp.x + core.Unit(level*t.indentWidth+treeLeftPadCells)*metrics.CellWidth
	// Connector lines fill the indent space (never widen it).
	if t.treeLines {
		for ci, r := range t.treeLinePrefix(item) {
			if r != ' ' {
				t.drawTreeLineCell(p, sp.x+core.Unit(ci+treeLeftPadCells)*metrics.CellWidth, itemY, r, s, metrics)
			}
		}
	}
	if !item.IsLeaf() {
		if item.Expanded {
			p.DrawCell(x, itemY, '▼', s)
		} else {
			p.DrawCell(x, itemY, '▸', s)
		}
	} else if t.treeLines {
		p.DrawCell(x, itemY, '▪', s)
	}
	x += metrics.CellWidth
	if item.Icon != nil && len(item.Icon.Cells) > 0 {
		cell := item.Icon.Cells[0]
		p.DrawCell(x, itemY, cell.Char, cell.Style)
		x += metrics.CellWidth * 2
	}
	avail := sp.x + sp.w - x
	if avail < 0 {
		avail = 0
	}
	p.DrawText(x, itemY, ellipsizeText(font, text, avail), textStyle, font)
}

// ellipsizeText fits text into avail, replacing a cut tail with an
// ellipsis - "…" on pixel surfaces, the project's text-mode "..." on
// cells. Rune-safe; returns the text unchanged when it already fits.
func ellipsizeText(font *core.Font, text string, avail core.Unit) string {
	if font.MeasureText(text) <= avail {
		return text
	}
	// The REAL ellipsis rune in both modes: in TUI it costs one cell
	// where "..." would eat three (the dock items do the same).
	// Project-wide unification/configurability of this pattern is a
	// planned later step.
	ell := "…"
	ellW := font.MeasureText(ell)
	runes := []rune(text)
	for len(runes) > 0 && font.MeasureText(string(runes))+ellW > avail {
		runes = runes[:len(runes)-1]
	}
	if len(runes) == 0 {
		if ellW <= avail {
			return ell
		}
		return ""
	}
	return string(runes) + ell
}

// drawAligned draws one cell value inside a span with the column's
// alignment, ellipsized to fit.
func (t *TreeView) drawAligned(p *core.Painter, text string, sp colSpan, y core.Unit, s style.CellStyle, font *core.Font, align string) {
	metrics := t.EffectiveCellMetrics()
	pad := metrics.CellWidth / 2
	avail := sp.w - pad
	if avail < 0 {
		avail = 0
	}
	text = ellipsizeText(font, text, avail)
	tw := font.MeasureText(text)
	x := sp.x
	switch align {
	case "right":
		x = sp.x + sp.w - tw - pad/2
	case "center":
		x = sp.x + (sp.w-tw)/2
	default:
		if p.Graphical() {
			// Pixel surfaces run spans edge to edge (no divider cell);
			// inset left-aligned text off the hairline.
			x = sp.x + pad/2
		}
	}
	if x < sp.x {
		x = sp.x
	}
	p.DrawText(x, y, text, s, font)
}

// chooserButtonRect is the [=] column-chooser button in the header's
// upper-right corner, above the scrollbar. On pixel surfaces it is TWO
// cells wide (a one-cell target is too small to find and fights the
// window-edge resize band); the TUI keeps the one-cell lane.
func (t *TreeView) chooserButtonRect() (core.UnitRect, bool) {
	if t.headerHeight() == 0 || len(t.optionalColumns()) == 0 {
		return core.UnitRect{}, false
	}
	metrics := t.EffectiveCellMetrics()
	w := metrics.CellWidth
	if core.FindGraphicalFrames(t.Self()) {
		w *= 2
	}
	return core.UnitRect{
		X: t.Bounds().Width - w, Y: 0,
		Width: w, Height: t.headerHeight(),
	}, true
}

func (t *TreeView) paintChooserButton(p *core.Painter, lay treeColLayout, headerStyle style.CellStyle) {
	r, ok := t.chooserButtonRect()
	if !ok {
		return
	}
	scheme := t.GetScheme()
	st := headerStyle
	keyFocused := t.HasFocus() && t.headerZone == hzItems &&
		t.headerFocusIdx == len(lay.spans) // the stop after the columns
	switch {
	case t.chooserHovered:
		// Hover color is a MOUSE affordance only.
		st = scheme.GetHoveredButton()
	case keyFocused, t.chooserOpen:
		// Keyboard focus (and the open state): a button face - the
		// chooser is a control, not a list item.
		st = scheme.GetFocusedListButton()
	}
	if p.Graphical() {
		p.FillRect(core.UnitRect{X: r.X, Y: r.Y, Width: r.Width, Height: r.Height}, ' ', st)
		// Three short lines - a crisp "≡" at any pixel size, in the
		// style's foreground so hover recolors it too.
		lineW := r.Width / 2
		x := r.X + (r.Width-lineW)/2
		fr, fg, fb := st.Fg.RGBComponents()
		wPx := p.UnitSpanPxX(x, x+lineW)
		gapPx := p.UnitsToPx(r.Height) / 5
		if gapPx < 2 {
			gapPx = 2
		}
		for i := 0; i < 3; i++ {
			p.FillRectPixelsAlpha(x, r.Y+r.Height/2, 0, (i-1)*gapPx,
				wPx, 1, fr, fg, fb, 1)
		}
		return
	}
	// TUI: ASCII '=' per the project's text-mode conventions.
	p.DrawCell(r.X, r.Y, '=', st)
}

// updateChooserHover tracks pointer presence over the chooser button
// (no-button affordance, same rule as the scrollbar thumb hover).
func (t *TreeView) updateChooserHover(event core.MouseMoveEvent) {
	over := false
	if r, ok := t.chooserButtonRect(); ok && event.Buttons == 0 {
		over = event.X >= r.X && event.X < r.X+r.Width &&
			event.Y >= r.Y && event.Y < r.Y+r.Height
	}
	if over != t.chooserHovered {
		t.chooserHovered = over
		t.Update()
	}
}

// optionalColumns lists the columns the chooser can toggle.
func (t *TreeView) optionalColumns() []*TreeColumn {
	var out []*TreeColumn
	for _, c := range t.columns {
		if c.Optional {
			out = append(out, c)
		}
	}
	return out
}

// --- interactions ---

// findTreePopupController resolves the popup host: the view's own
// controller field, else the nearest ancestor's (the same walk the
// text input's context menu uses - inside an MDI child only an
// ancestor carries the controller).
func (t *TreeView) findTreePopupController() core.PopupController {
	if pc := t.PopupController(); pc != nil {
		return pc
	}
	for current := t.Parent(); current != nil; {
		trinket, ok := current.(core.Trinket)
		if !ok {
			break
		}
		if getter, ok := trinket.(interface {
			PopupController() core.PopupController
		}); ok {
			if pc := getter.PopupController(); pc != nil {
				return pc
			}
		}
		current = trinket.Parent()
	}
	return nil
}

func (t *TreeView) chooserPopupID() string {
	return fmt.Sprintf("treeview-columns-%d", t.ObjectID())
}

// openColumnChooser drops the [=] checklist as a REAL Menu - the same
// gutter, checkmark, and focus styling as every menubar dropdown, and
// the same popup-controller overlay the combobox and context menus
// use. It always opens DOWN and to the LEFT (its right edge on the
// button's right edge): it is an upper-right-corner button, so rightward
// growth would leave the screen. While open the tree keeps focus and
// forwards keys (the menubar pattern), so Up/Down/Space/Escape work and
// the menu's accessibility announcements fire. keyboard=true preselects
// the first item for immediate arrow/space use.
func (t *TreeView) openColumnChooser(keyboard bool) {
	pc := t.findTreePopupController()
	if pc == nil {
		return
	}
	cols := t.optionalColumns()
	if len(cols) == 0 {
		return
	}

	m := NewMenu("Columns")
	// Popup menus are not parented into the trinket tree; hand down the
	// opener's display context (metrics + font), same as the menu bar.
	m.inheritDisplayContext(t.EffectiveCellMetrics(), t.EffectiveFont())
	m.setGraphicalHint(core.FindGraphicalFrames(t.Self()))
	if d, ok := t.desktopAncestor(); ok {
		m.SetAccessibilityManager(d.AccessibilityManager())
	}
	for _, c := range cols {
		col := c
		// InPlace: toggling a column keeps the menu open (users flip
		// several in one visit), re-rendering the checkmarks in place.
		item := NewMenuItem(col.Caption).SetCheckable(true).SetChecked(!col.Hidden).SetInPlace(true)
		item.SetOnTriggered(func() {
			col.Hidden = !col.Hidden
			t.Update()
		})
		m.AddItem(item)
	}

	// Down-and-left from the button.
	btn, _ := t.chooserButtonRect()
	size := m.calculateSize()
	btnOrigin := pc.MapToScreen(t.Self(), core.UnitPoint{X: btn.X, Y: btn.Y})
	btnScreen := core.UnitRect{X: btnOrigin.X, Y: btnOrigin.Y, Width: btn.Width, Height: btn.Height}
	at := pc.MapToScreen(t.Self(), core.UnitPoint{X: btn.X + btn.Width, Y: btn.Y + btn.Height})
	screen := pc.ScreenBounds()
	x := at.X - size.Width
	if x < screen.X {
		x = screen.X
	}
	y := at.Y
	if y+size.Height > screen.Y+screen.Height {
		y = screen.Y + screen.Height - size.Height
	}
	m.Show(x, y)
	if keyboard {
		m.SelectFirstItem()
	}
	bounds := core.UnitRect{X: x, Y: y, Width: size.Width, Height: size.Height}

	t.chooserMenu = m
	t.chooserOpen = true
	pc.RegisterPopup(&core.PopupRequest{
		ID:     t.chooserPopupID(),
		Bounds: bounds,
		Paint:  func(p *core.Painter) { m.Paint(p) },
		HandleMousePress: func(ev core.MousePressEvent) bool {
			pt := core.UnitPoint{X: ev.X, Y: ev.Y}
			if !bounds.Contains(pt) {
				t.closeColumnChooser()
				// A click on the opener button itself is a TOGGLE:
				// swallow it so it doesn't fall through and reopen.
				return btnScreen.Contains(pt)
			}
			return m.HandleMousePress(ev)
		},
		HandleMouseMove: m.HandleMouseMove,
		HandleMouseRelease: func(ev core.MouseReleaseEvent) bool {
			// The press selected; the release triggers (the menu bar's
			// drag-mode contract, played standalone here). InPlace
			// items keep the menu open; others dismiss it.
			if bounds.Contains(core.UnitPoint{X: ev.X, Y: ev.Y}) {
				if item := m.CurrentItem(); item != nil && item.Enabled && !item.Separator {
					item.Trigger()
					if !item.InPlace {
						t.closeColumnChooser()
					}
				}
			}
			return true
		},
		HandleMouseWheel: m.HandleMouseWheel,
		// The host may discard the overlay itself (a press outside every
		// popup clears them all without routing the press here). Reset
		// the open-state, or the button stays lit "focused" and every
		// keystroke keeps going to a menu that no longer exists.
		OnDismiss: func() { t.closeColumnChooser() },
	})
	t.Update()
}

// closeColumnChooser dismisses the chooser menu overlay.
func (t *TreeView) closeColumnChooser() {
	if !t.chooserOpen {
		return
	}
	t.chooserOpen = false
	if t.chooserMenu != nil {
		t.chooserMenu.Hide()
		t.chooserMenu = nil
	}
	if pc := t.findTreePopupController(); pc != nil {
		pc.UnregisterPopup(t.chooserPopupID())
	}
	t.Update()
}

// desktopAncestor walks up for the owning Desktop (accessibility hookup).
func (t *TreeView) desktopAncestor() (*Desktop, bool) {
	for p := core.Container(t.Parent()); p != nil; {
		if d, ok := p.(*Desktop); ok {
			return d, true
		}
		tr, ok := p.(core.Trinket)
		if !ok {
			return nil, false
		}
		p = tr.Parent()
	}
	return nil, false
}

// handleChooserKey routes keys to the open chooser menu (the tree
// retains focus and forwards, exactly like the menu bar): navigation
// and toggling go to the Menu, Escape dismisses, everything else is
// swallowed while the menu is up.
func (t *TreeView) handleChooserKey(event core.KeyPressEvent) bool {
	if !t.chooserOpen || t.chooserMenu == nil {
		return false
	}
	if event.Key == "Escape" {
		t.closeColumnChooser()
		return true
	}
	t.chooserMenu.HandleKeyPress(event)
	// A non-InPlace trigger hides the menu itself; tear the popup down
	// with it (InPlace items keep it open by design).
	if t.chooserMenu != nil && !t.chooserMenu.IsVisible() {
		t.closeColumnChooser()
	}
	return true
}

// dividerGrabZone is the horizontal grab band around a divider line.
func (t *TreeView) dividerGrabZone() (grab0, grab1 core.Unit) {
	cw := t.EffectiveCellMetrics().CellWidth
	if core.FindGraphicalFrames(t.Self()) {
		return -cw / 2, cw / 2 // pixels: half a cell astride the line
	}
	return 0, cw // TUI: the divider cell itself
}

// beginFitDrag arms the composite fit-mode divider drag. In fit mode
// the columns tile the width exactly, so a line can only move by
// trading cells across it against the slack pool - the auto-fill key
// column's spare width when the key shows (slack LEFT of every line),
// else the blank width right of the last column (slack RIGHT).
//
// Slack left: dragging LEFT widens the RIGHT column, funded by the
// pool first and then by narrowing the LEFT column; dragging RIGHT
// narrows the right column, the cells returning to the key. Slack
// right is the mirror: RIGHT widens the left column (blank first,
// then the right column gives way), LEFT narrows it back.
//
// Every step is capped by the pool plus the giving neighbor's own
// room, so the layout never starts reclaiming width from UNRELATED
// columns mid-drag: lines left of the grab move only WITH the drag
// (into consumed slack), lines right of it stay put - none ever moves
// contrary to the drag direction.
func (t *TreeView) beginFitDrag(x core.Unit, lay treeColLayout) bool {
	cw := t.EffectiveCellMetrics().CellWidth
	grab0, grab1 := t.dividerGrabZone()
	for i, sp := range lay.spans {
		if !lay.divVisible(sp) || x < sp.divX+grab0 || x >= sp.divX+grab1 {
			continue
		}
		if i+1 >= len(lay.spans) {
			return false
		}
		left, right := lay.spans[i], lay.spans[i+1]
		slackLeft := lay.spans[0].col == nil // the auto-fill key column
		if slackLeft {
			// The right column is the movement mechanism.
			if right.col == nil || !right.col.Resizable {
				return false
			}
		} else if left.col == nil || !left.col.Resizable {
			// No key: the left column is the mechanism.
			return false
		}
		t.colDragging = true
		t.colDragFit = true
		t.colDragSlackRight = !slackLeft
		t.colDragStartX = x
		t.colDragL = left.col
		t.colDragR = right.col
		// Width snapshots come from the COLUMNS' natural widths (the
		// last span may be stretched over the trailing blank); only
		// the key span's auto width has no column to ask.
		t.colDragLW = int(left.w / cw)
		if left.col != nil {
			t.colDragLW = left.col.clampWidth(left.col.Width)
		}
		t.colDragRW = int(right.w / cw)
		if right.col != nil {
			t.colDragRW = right.col.clampWidth(right.col.Width)
		}
		if slackLeft {
			// Pool: the key's cells above the width the layout defends
			// (below it the key starts reclaiming from other columns,
			// which would move unrelated lines).
			floor := t.keyWidth
			if floor <= 0 {
				floor = treeKeyDefaultCells
			}
			if floor < treeKeyMinCells {
				floor = treeKeyMinCells
			}
			t.colDragPool = int(lay.spans[0].w/cw) - floor
		} else {
			// Pool: the NATURAL trailing blank (recorded before the
			// last span was stretched over it for display).
			t.colDragPool = lay.blankCells
		}
		if t.colDragPool < 0 {
			t.colDragPool = 0
		}
		return true
	}
	return false
}

// applyFitDrag recomputes both neighbor widths from the press-time
// snapshot for the pointer's current position (idempotent per move).
func (t *TreeView) applyFitDrag(x core.Unit) {
	cw := t.EffectiveCellMetrics().CellWidth
	delta := int((x - t.colDragStartX) / cw) // + = rightward
	if !t.colDragSlackRight {
		// Slack pool (the auto key) LEFT of the line; the right
		// column is the mechanism.
		right := t.colDragR
		if delta >= 0 {
			// Rightward: the left column widens with exactly the cells
			// the right column gives up, so the key's auto width never
			// changes and every line but the grabbed one stays put BY
			// CONSTRUCTION (routing the cells through the key instead
			// lets its re-fit nudge unrelated lines backward). Only
			// when the key itself is the left neighbor do the cells
			// return to it - dragging ITS divider is how key slack
			// refills - and a non-resizable left column keeps that
			// classic key-absorb behavior too.
			c := delta
			if room := t.colDragRW - right.MinWidth; c > room {
				c = room
			}
			l := t.colDragL
			transfer := l != nil && l.Resizable
			if transfer && l.MaxWidth > 0 {
				if lim := l.MaxWidth - t.colDragLW; c > lim {
					c = lim
				}
			}
			if c < 0 {
				c = 0
			}
			right.Width = t.colDragRW - c
			if l != nil {
				if transfer {
					l.Width = t.colDragLW + c
				} else {
					l.Width = t.colDragLW
				}
			}
		} else {
			// Leftward: widen the right column - the pool pays first,
			// then the left column narrows toward its minimum.
			m := -delta
			lFree := 0
			if t.colDragL != nil && t.colDragL.Resizable {
				if lFree = t.colDragLW - t.colDragL.MinWidth; lFree < 0 {
					lFree = 0
				}
			}
			if m > t.colDragPool+lFree {
				m = t.colDragPool + lFree
			}
			if right.MaxWidth > 0 && m > right.MaxWidth-t.colDragRW {
				m = right.MaxWidth - t.colDragRW
			}
			if m < 0 {
				m = 0
			}
			fromL := m - t.colDragPool
			if fromL < 0 {
				fromL = 0
			}
			right.Width = t.colDragRW + m
			if t.colDragL != nil {
				t.colDragL.Width = t.colDragLW - fromL
			}
		}
	} else {
		// Slack pool (blank width) RIGHT of the line, key hidden; the
		// left column is the mechanism - the exact mirror.
		left := t.colDragL
		if delta <= 0 {
			c := -delta
			if room := t.colDragLW - left.MinWidth; c > room {
				c = room
			}
			if c < 0 {
				c = 0
			}
			left.Width = t.colDragLW - c
			if t.colDragR != nil {
				t.colDragR.Width = t.colDragRW
			}
		} else {
			m := delta
			rFree := 0
			if t.colDragR != nil && t.colDragR.Resizable {
				if rFree = t.colDragRW - t.colDragR.MinWidth; rFree < 0 {
					rFree = 0
				}
			}
			if m > t.colDragPool+rFree {
				m = t.colDragPool + rFree
			}
			if left.MaxWidth > 0 && m > left.MaxWidth-t.colDragLW {
				m = left.MaxWidth - t.colDragLW
			}
			if m < 0 {
				m = 0
			}
			fromR := m - t.colDragPool
			if fromR < 0 {
				fromR = 0
			}
			left.Width = t.colDragLW + m
			if t.colDragR != nil {
				t.colDragR.Width = t.colDragRW - fromR
			}
		}
	}
	t.Update()
}

// dividerAt returns the column resized by a SCROLL-mode drag starting
// at header position x, and whether the delta INVERTS. It also serves
// as the divider hit-test for the resize cursor in both modes (its
// refusal rules match beginFitDrag's: the movement-mechanism column
// must be resizable). Scroll mode sizes the column LEFT of the line
// (the boundary follows the mouse; columns to the right pan), except
// at the pinned-right flank where the divider IS the pinned column's
// left edge. Fit-mode PRESSES never use the returned sizing - they arm
// the composite beginFitDrag/applyFitDrag path instead.
func (t *TreeView) dividerAt(x core.Unit, lay treeColLayout) (col *TreeColumn, startW int, invert, ok bool) {
	grab0, grab1 := t.dividerGrabZone()
	slackLeft := t.fitWidth && len(lay.spans) > 0 && lay.spans[0].col == nil
	for i, sp := range lay.spans {
		if !lay.divVisible(sp) {
			continue
		}
		if x < sp.divX+grab0 || x >= sp.divX+grab1 {
			continue
		}
		if slackLeft {
			// The auto-fill key column is left of every divider: size
			// the column to the divider's RIGHT, inverted.
			if i+1 >= len(lay.spans) {
				return nil, 0, false, false
			}
			right := lay.spans[i+1].col
			if right == nil || !right.Resizable {
				return nil, 0, false, false
			}
			return right, right.clampWidth(right.Width), true, true
		}
		if i+1 < len(lay.spans) && lay.spans[i+1].fixed && !sp.fixed {
			// The pinned-right boundary: this divider IS the pinned
			// column's left edge (the right flank is laid out from the
			// window's right edge), so it sizes the PINNED column,
			// inverted - not the scrolling column to its left.
			right := lay.spans[i+1].col
			if right == nil || !right.Resizable {
				return nil, 0, false, false
			}
			return right, right.clampWidth(right.Width), true, true
		}
		if sp.col == nil {
			// The key column in scroll mode sizes by its own width.
			kw := t.keyWidth
			if kw <= 0 {
				kw = treeKeyDefaultCells
			}
			return nil, kw, false, true
		}
		if !sp.col.Resizable {
			return nil, 0, false, false
		}
		return sp.col, sp.col.clampWidth(sp.col.Width), false, true
	}
	return nil, 0, false, false
}

// CursorShapeAt implements core.CursorShaper: the pointer shows the
// horizontal resize cursor over a draggable divider in the header band
// (mid-drag it stays put wherever the pointer is).
func (t *TreeView) CursorShapeAt(localX, localY core.Unit) core.CursorShape {
	if t.colDragging {
		return core.CursorResizeH
	}
	headerH := t.headerHeight()
	if headerH == 0 || localY >= headerH || !t.multiColumn() {
		return core.CursorDefault
	}
	if _, _, _, ok := t.dividerAt(localX, t.columnLayout()); ok {
		return core.CursorResizeH
	}
	return core.CursorDefault
}

// handleMultiPress handles presses in the header band (chooser button,
// divider drags). Returns handled.
func (t *TreeView) handleMultiPress(event core.MousePressEvent) bool {
	if !t.multiColumn() {
		return false
	}
	headerH := t.headerHeight()
	if headerH == 0 || event.Y >= headerH {
		return false
	}
	if r, ok := t.chooserButtonRect(); ok && event.X >= r.X && event.X < r.X+r.Width {
		t.openColumnChooser(false)
		return true
	}
	lay := t.columnLayout()
	if t.fitWidth {
		// Fit mode: the composite two-sided drag (see beginFitDrag).
		if t.beginFitDrag(event.X, lay) {
			return true
		}
	} else if col, startW, invert, ok := t.dividerAt(event.X, lay); ok {
		t.colDragging = true
		t.colDragCol = col
		t.colDragStartX = event.X
		t.colDragStartW = startW
		t.colDragInvert = invert
		return true
	}
	// A click on a column caption requests a sort (the key column is
	// always sortable; data columns opt in via Sortable).
	for _, sp := range lay.spans {
		clip, ok := lay.spanClip(sp, lay.headerH)
		if !ok {
			continue
		}
		if event.X >= clip.X && event.X < clip.X+clip.Width {
			t.headerSortClick(sp.col)
			break
		}
	}
	return true // header clicks never fall through to the rows
}

// handleMultiMove continues a divider drag. Returns handled.
func (t *TreeView) handleMultiMove(event core.MouseMoveEvent) bool {
	if !t.colDragging {
		return false
	}
	if t.colDragFit {
		t.applyFitDrag(event.X)
		return true
	}
	cw := t.EffectiveCellMetrics().CellWidth
	deltaCells := int((event.X - t.colDragStartX) / cw)
	if t.colDragInvert {
		deltaCells = -deltaCells
	}
	w := t.colDragStartW + deltaCells
	if t.colDragCol != nil {
		if nw := t.colDragCol.clampWidth(w); nw != t.colDragCol.Width {
			t.colDragCol.Width = nw
			t.Update()
		}
	} else {
		if w < treeKeyMinCells {
			w = treeKeyMinCells
		}
		if w != t.keyWidth {
			t.keyWidth = w
			t.Update()
		}
	}
	return true
}

// handleMultiRelease ends a divider or footer-scrollbar drag.
// Returns handled.
func (t *TreeView) handleMultiRelease(event core.MouseReleaseEvent) bool {
	handled := false
	if t.colDragging {
		t.colDragging = false
		t.colDragCol = nil
		t.colDragFit = false
		t.colDragL, t.colDragR = nil, nil
		handled = true
	}
	if t.hbarDragging {
		t.hbarDragging = false
		// The drag kept the thumb lit; recompute hover from the release
		// point. TUI clears it outright: no move events arrive outside
		// a drag there, so a stale hover would stay lit forever.
		t.hbarThumbHovered = core.FindGraphicalFrames(t.Self()) &&
			t.overHBarThumb(event.X, event.Y)
		handled = true
	}
	return handled
}

// hScrollbarGeometry returns the footer horizontal scrollbar's track
// and thumb in units. The track spans the scrolling region; thumb
// length is proportional to the visible share of the scrollable
// content. ok=false when there is nothing to scroll.
func (t *TreeView) hScrollbarGeometry(lay treeColLayout) (trackX0, trackX1, thumbX0, thumbX1 core.Unit, ok bool) {
	if t.footerHeight() == 0 || lay.maxHScroll <= 0 {
		return 0, 0, 0, 0, false
	}
	cw := t.EffectiveCellMetrics().CellWidth
	trackX0, trackX1 = lay.scrollL, lay.scrollR
	trackCells := int((trackX1 - trackX0) / cw)
	if trackCells <= 0 {
		return 0, 0, 0, 0, false
	}
	totalCells := trackCells + lay.maxHScroll
	thumbCells := trackCells * trackCells / totalCells
	if thumbCells < 1 {
		thumbCells = 1
	}
	scrollable := trackCells - thumbCells
	pos := 0
	if scrollable > 0 {
		pos = t.hScroll * scrollable / lay.maxHScroll
		if t.hScroll > 0 && pos == 0 {
			pos = 1
		}
		if t.hScroll < lay.maxHScroll && pos >= scrollable {
			pos = scrollable - 1
		}
		if pos < 0 {
			pos = 0
		}
	}
	thumbX0 = trackX0 + core.Unit(pos)*cw
	thumbX1 = thumbX0 + core.Unit(thumbCells)*cw
	return trackX0, trackX1, thumbX0, thumbX1, true
}

// paintHScrollbar renders the reserved footer row's horizontal
// scrollbar across the scrolling region.
func (t *TreeView) paintHScrollbar(p *core.Painter, lay treeColLayout) {
	trackX0, trackX1, thumbX0, thumbX1, ok := t.hScrollbarGeometry(lay)
	if !ok {
		return
	}
	scheme := t.GetScheme()
	metrics := t.EffectiveCellMetrics()
	footerH := t.footerHeight()
	y := t.Bounds().Height - footerH
	trackStyle := scheme.GetScrollbar()
	// Hover/drag lighting is graphical-only, like every other thumb:
	// TUI gets no free mouse-move events, so a lit state could never
	// clear there.
	thumbStyle := scheme.GetScrollbarThumbState((t.hbarThumbHovered || t.hbarDragging) && p.Graphical())

	if p.Graphical() {
		// The slim band: bare thumb only, inset a unit on each side.
		// No track stripe - it reads as another column divider next
		// to the real ones.
		p.FillRect(core.UnitRect{X: thumbX0, Y: y + 1, Width: thumbX1 - thumbX0, Height: footerH - 2},
			' ', thumbStyle.WithBg(thumbStyle.Fg))
		return
	}
	// TUI track: the ScrollArea's shaded fill, not a line.
	for x := trackX0; x < trackX1; x += metrics.CellWidth {
		p.DrawCell(x, y, '░', trackStyle)
	}
	for x := thumbX0; x < thumbX1; x += metrics.CellWidth {
		p.DrawCell(x, y, '█', thumbStyle)
	}
}

// overHBarThumb reports whether the point sits on the footer
// horizontal scrollbar's thumb (drives its hover color).
func (t *TreeView) overHBarThumb(x, y core.Unit) bool {
	footerH := t.footerHeight()
	if footerH == 0 || !t.multiColumn() {
		return false
	}
	bounds := t.Bounds()
	if y < bounds.Height-footerH || y >= bounds.Height {
		return false
	}
	_, _, thumbX0, thumbX1, ok := t.hScrollbarGeometry(t.columnLayout())
	return ok && x >= thumbX0 && x < thumbX1
}

// handleHBarPress starts a thumb drag or pages on a track click in
// the footer row. Returns handled.
func (t *TreeView) handleHBarPress(event core.MousePressEvent) bool {
	footerH := t.footerHeight()
	if footerH == 0 {
		return false
	}
	bounds := t.Bounds()
	if event.Y < bounds.Height-footerH {
		return false
	}
	lay := t.columnLayout()
	trackX0, trackX1, thumbX0, thumbX1, ok := t.hScrollbarGeometry(lay)
	if !ok {
		return true // reserved row, nothing to scroll: swallow
	}
	switch {
	case event.X >= thumbX0 && event.X < thumbX1:
		t.hbarDragging = true
		t.hbarDragStartX = event.X
		t.hbarDragStartHS = t.hScroll
	case event.X >= trackX0 && event.X < thumbX0:
		t.scrollHorizontally(-int((trackX1 - trackX0) / t.EffectiveCellMetrics().CellWidth))
	case event.X >= thumbX1 && event.X < trackX1:
		t.scrollHorizontally(int((trackX1 - trackX0) / t.EffectiveCellMetrics().CellWidth))
	}
	return true
}

// handleHBarMove continues a footer thumb drag. Returns handled.
func (t *TreeView) handleHBarMove(event core.MouseMoveEvent) bool {
	if !t.hbarDragging {
		return false
	}
	lay := t.columnLayout()
	trackX0, trackX1, thumbX0, thumbX1, ok := t.hScrollbarGeometry(lay)
	if !ok {
		t.hbarDragging = false
		return true
	}
	cw := t.EffectiveCellMetrics().CellWidth
	trackCells := int((trackX1 - trackX0) / cw)
	thumbCells := int((thumbX1 - thumbX0) / cw)
	scrollable := trackCells - thumbCells
	if scrollable <= 0 {
		return true
	}
	deltaCells := int((event.X - t.hbarDragStartX) / cw)
	hs := t.hbarDragStartHS + deltaCells*lay.maxHScroll/scrollable
	if hs < 0 {
		hs = 0
	}
	if hs > lay.maxHScroll {
		hs = lay.maxHScroll
	}
	if hs != t.hScroll {
		t.hScroll = hs
		t.Update()
	}
	return true
}

// scrollHorizontally pans the scroll region by delta cells (scroll
// mode only), clamped to the content.
func (t *TreeView) scrollHorizontally(deltaCells int) bool {
	if t.fitWidth || !t.multiColumn() {
		return false
	}
	lay := t.columnLayout()
	hs := t.hScroll + deltaCells
	if hs < 0 {
		hs = 0
	}
	if hs > lay.maxHScroll {
		hs = lay.maxHScroll
	}
	if hs == t.hScroll {
		return false
	}
	t.hScroll = hs
	t.Update()
	return true
}
