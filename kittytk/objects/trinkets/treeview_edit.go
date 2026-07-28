package trinkets

import (
	"strings"

	"github.com/phroun/kittytk/core"
)

// In-place row editing for the multi-column TreeView.
//
// Enter on a row (when anything is editable) opens the ROW EDITOR: a
// spun-into-existence trinket floating over one cell - a TextInput for
// free-text columns, a ComboBox (closed) for enum columns. The tree
// keeps real focus and forwards input (the popup-menu pattern); once
// the editor is dismissed the plain grid is back.
//
//	Enter   commit the row and dismiss
//	Escape  cancel the current cell (original value stays) and dismiss
//	Tab     commit the cell, edit the next editable column (wraps)
//	S-Tab   commit the cell, edit the previous editable column
//	Up/Down commit the cell, move to that row, keep editing the SAME
//	        column there (even on a combo cell, where arrows would
//	        normally change the value - row navigation wins while the
//	        drop-down is closed)
//	Space   on a combo cell: pop the drop-down open; while open, keys
//	        navigate its items as usual until it closes as normal
//	click elsewhere: the value is accepted, then the click proceeds
//
// Re-entering edit mode returns to the column edited last time. When
// nothing is editable, Enter keeps its classic Space behavior
// (expand/collapse or nothing). The KEY column joins the ring via the
// tree's own SetEditable (editing the item's caption; no enum there).
//
// A CLICK on a cell of the already-selected row also flips straight
// into edit mode (on a drag-free release).

// treeClickEditSlop is how far the pointer may travel between press
// and release before the click stops counting as a click.
const treeClickEditSlop = core.Unit(4)

// treeKeyColumn is the sentinel identifying the KEY (tree) column in
// the edit ring. It never lives in t.columns and is never painted
// from - only its pointer identity matters.
var treeKeyColumn = &TreeColumn{Align: "left"}

// SetEditable makes the KEY (tree) column editable in the row editor,
// exactly like a data column's Editable trait (the editor writes the
// item's caption text; enum choices don't apply to the key).
func (t *TreeView) SetEditable(on bool) {
	t.keyEditable = on
	t.Update()
}

// SetOnCellEdited installs the observer for committed cell edits (only
// fired when the value actually changed). column is treeKeyColumn's
// sentinel identity for key edits; wire consumers see index -1.
func (t *TreeView) SetOnCellEdited(fn func(item *TreeItem, column *TreeColumn, value string)) {
	t.onCellEdited = fn
}

// RowEditing reports whether the in-place row editor is open.
func (t *TreeView) RowEditing() bool { return t.rowEditing }

// editActorTarget implements editActorProvider: while the TEXT editor
// is up, the Edit menu's Cut/Copy/Paste/Select All (and their enabled
// states) operate on it exactly as on a plain TextInput. Enum (combo)
// cells deliberately do NOT participate, and with no editor open the
// tree is not an edit target.
func (t *TreeView) editActorTarget() (editActor, bool) {
	if t.rowEditing && t.editBox != nil {
		return t.editBox, true
	}
	return nil, false
}

// colEditable reports whether col is currently a live edit target.
func (t *TreeView) colEditable(col *TreeColumn) bool {
	if col == treeKeyColumn {
		return t.keyEditable && t.showKey
	}
	return col != nil && col.Editable && !col.Hidden
}

// cellValue reads the raw stored value for a column (the key column
// stores the item's caption).
func (t *TreeView) cellValue(item *TreeItem, col *TreeColumn) string {
	if col == treeKeyColumn {
		return item.Text
	}
	return item.Value(col.ID)
}

// setCellValue writes the raw stored value for a column.
func (t *TreeView) setCellValue(item *TreeItem, col *TreeColumn, v string) {
	if col == treeKeyColumn {
		item.Text = v
		return
	}
	item.SetValue(col.ID, v)
}

// treeCellTextInset is where the caption text begins within a
// tree-hosting cell: the indent, the expander cell, and the icon
// (when the item has one) - mirroring paintTreeCell exactly.
func (t *TreeView) treeCellTextInset(item *TreeItem) core.Unit {
	cw := t.EffectiveCellMetrics().CellWidth
	inset := core.Unit(item.Level()*t.indentWidth+1+treeLeftPadCells) * cw
	if item.Icon != nil && len(item.Icon.Cells) > 0 {
		inset += cw * 2
	}
	return inset
}

// spanMatchesCol matches a layout span to an edit-ring column (the
// key sentinel matches the nil key span).
func spanMatchesCol(sp colSpan, col *TreeColumn) bool {
	if col == treeKeyColumn {
		return sp.col == nil
	}
	return sp.col != nil && sp.col == col
}

// editableColumns returns the edit ring in display order: the key
// column first when the tree itself is editable (and showing it),
// then the visible Editable data columns.
func (t *TreeView) editableColumns() []*TreeColumn {
	var out []*TreeColumn
	for _, c := range t.visibleColumns() {
		if c == nil {
			if t.keyEditable {
				out = append(out, treeKeyColumn)
			}
			continue
		}
		if c.Editable {
			out = append(out, c)
		}
	}
	return out
}

func (t *TreeView) hasEditableColumns() bool { return len(t.editableColumns()) > 0 }

// enterTargetColumn is the column Enter would edit right now: the
// remembered last-edited column when still available, else the first
// editable one (nil when nothing is editable). The paint path marks
// this cell with FocusedListItem on the focused selected row.
func (t *TreeView) enterTargetColumn() *TreeColumn {
	cols := t.editableColumns()
	if len(cols) == 0 {
		return nil
	}
	col := cols[0]
	for _, c := range cols {
		if c == t.editLastCol {
			col = c
			break
		}
	}
	return col
}

// startRowEdit enters row-edit mode on the current item, resuming the
// last-edited column when it is still available, else the first
// editable one. Returns false when there is nothing to edit.
func (t *TreeView) startRowEdit() bool {
	item := t.CurrentItem()
	if item == nil || !t.multiColumn() || t.rowEditing {
		return false
	}
	col := t.enterTargetColumn()
	if col == nil {
		return false
	}
	t.beginCellEdit(item, col)
	return true
}

// beginCellEdit opens the row editor over one cell.
func (t *TreeView) beginCellEdit(item *TreeItem, col *TreeColumn) {
	t.cancelClickEdit()
	t.editItem = item
	t.rowEditing = true
	t.mountEditor(col)
}

// mountEditor spins the right editor trinket for col over the current
// cell: a closed ComboBox for enum columns, a TextInput otherwise.
// Editors are not parented into the tree (SetParent wants a
// Container); the display context and ancestry are handed down the
// way popup menus do it.
func (t *TreeView) mountEditor(col *TreeColumn) {
	t.dropEditorTrinkets()
	t.editCol = col
	t.editLastCol = col
	raw := t.cellValue(t.editItem, col)
	t.editOrig = raw

	cm := t.EffectiveCellMetrics()
	font := t.EffectiveFont()
	origin := func() core.UnitPoint {
		r, _ := t.editorRect()
		return core.UnitPoint{X: r.X, Y: r.Y}
	}

	if col != treeKeyColumn && len(col.Enum) > 0 {
		cb := NewComboBox()
		cb.SetCellMetrics(&cm)
		cb.SetFont(font)
		// Borrowed ancestry: the popup controller walk and the
		// drop-down's screen geometry (drop direction, scrolling)
		// resolve through the tree at the live cell origin - the
		// popup behaves exactly like a natively placed box's.
		cb.SetEmbedHost(t.Self(), origin)
		sel := -1
		for i, o := range col.Enum {
			stored := o.Value
			if col.EnumStore == "key" {
				stored = o.Key
			}
			if stored == raw {
				sel = i
				break
			}
		}
		// The magic head entry: when the stored value is not one of
		// the options it stays visible (and re-selectable) for THIS
		// edit session only - choosing it keeps the cell unchanged,
		// and it vanishes once a listed option is stored and the row
		// edit closes.
		t.editComboMagic = sel < 0
		if t.editComboMagic {
			cb.AddItem(col.displayValue(raw))
		}
		for _, o := range col.Enum {
			cb.AddItem(o.Value)
		}
		if t.editComboMagic {
			cb.SetCurrentIndex(0)
		} else {
			cb.SetCurrentIndex(sel)
		}
		if r, ok := t.editorRect(); ok {
			cb.SetBounds(core.UnitRect{Width: r.Width, Height: r.Height})
		}
		// Focused-looking (the tree keeps real focus and forwards).
		cb.SetFocus()
		t.editCombo = cb
	} else {
		ed := NewTextInput()
		ed.SetCellMetrics(&cm)
		ed.SetFont(font)
		ed.SetText(raw)
		ed.SelectAll()
		ed.SetFocus()
		// Ancestry for the clipboard bridge and the context menu.
		ed.SetEmbedHost(t.Self(), origin)
		t.editBox = ed
	}
	t.ensureEditColVisible()
	t.Update()
}

// dropEditorTrinkets dismisses whichever editor trinket is mounted.
func (t *TreeView) dropEditorTrinkets() {
	if t.editBox != nil {
		t.editBox.ClearFocus()
		t.editBox = nil
	}
	if t.editCombo != nil {
		if t.editCombo.IsOpen() {
			t.editCombo.HidePopup()
		}
		t.editCombo.ClearFocus()
		t.editCombo = nil
	}
	t.editComboMagic = false
}

// ensureEditColVisible scrolls the horizontal column region the
// MINIMUM needed to reveal the edited column (scroll mode only).
func (t *TreeView) ensureEditColVisible() {
	if !t.rowEditing || t.editCol == nil {
		return
	}
	t.ensureColVisible(t.editCol)
}

// ensureColVisible scrolls the horizontal column region the MINIMUM
// needed to reveal col (scroll mode only). Same conservative rule as
// the ScrollArea's EnsureRectVisible: no movement at all when the
// cell is already fully in view; otherwise align the nearer edge,
// prioritizing (never hiding) the left edge.
func (t *TreeView) ensureColVisible(col *TreeColumn) {
	if t.fitWidth || col == nil {
		return
	}
	lay := t.columnLayout()
	cw := t.EffectiveCellMetrics().CellWidth
	for _, sp := range lay.spans {
		if !spanMatchesCol(sp, col) {
			continue
		}
		if sp.fixed {
			return // pinned columns are always in view
		}
		// The span's NATURAL cell offset within the scrolling region
		// (its painted x has the current scroll already applied).
		leftCells := int(lay.scrollL / cw)
		viewCells := int((lay.scrollR - lay.scrollL) / cw)
		start := int(sp.x/cw) - leftCells + t.hScroll
		end := start + int(sp.w/cw)
		hs := t.hScroll
		if start < hs {
			hs = start
		} else if end > hs+viewCells {
			hs = end - viewCells
			if hs > start {
				hs = start // never hide the left edge
			}
		}
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
		return
	}
}

// editorValue resolves what the active editor would store right now
// (ok=false means "keep the original": the combo's magic entry).
func (t *TreeView) editorValue() (string, bool) {
	switch {
	case t.editBox != nil:
		return t.editBox.Text(), true
	case t.editCombo != nil:
		idx := t.editCombo.CurrentIndex()
		off := 0
		if t.editComboMagic {
			if idx == 0 {
				return "", false // the magic entry IS the original
			}
			off = 1
		}
		col := t.editCol
		if idx-off < 0 || idx-off >= len(col.Enum) {
			return "", false
		}
		o := col.Enum[idx-off]
		if col.EnumStore == "key" {
			return o.Key, true
		}
		return o.Value, true
	}
	return "", false
}

// commitCellEdit writes the editor's value onto the cell if it changed
// and reports it; the editor stays up.
func (t *TreeView) commitCellEdit() {
	if !t.rowEditing {
		return
	}
	v, ok := t.editorValue()
	if !ok || v == t.editOrig {
		return
	}
	t.setCellValue(t.editItem, t.editCol, v)
	if t.onCellEdited != nil {
		t.onCellEdited(t.editItem, t.editCol, v)
	}
	// Under an active visual sort the new value can move rows; the
	// trinket re-sorts itself and the selection tracks the item.
	if t.sorted {
		t.resortKeepingSelection()
	}
	// An explicit edit is a user action ON this row: keep it in view
	// unconditionally, even if the new value just sorted it far away.
	t.ensureVisible(t.currentIndex)
}

// endRowEdit dismisses the editor. commit=false is Escape: nothing is
// written, the cell keeps its original value.
func (t *TreeView) endRowEdit(commit bool) {
	if !t.rowEditing {
		return
	}
	if commit {
		t.commitCellEdit()
	}
	t.dropEditorTrinkets()
	t.editItem = nil
	t.editCol = nil
	t.rowEditing = false
	t.editMouseDown = false
	t.Update()
}

// stepEditColumn commits the current cell and moves the editor to the
// next (+1) or previous (-1) editable column, wrapping around. The
// editor trinket is remounted, so text and enum columns mix freely.
func (t *TreeView) stepEditColumn(delta int) {
	cols := t.editableColumns()
	if len(cols) == 0 {
		t.endRowEdit(true)
		return
	}
	idx := 0
	for i, c := range cols {
		if c == t.editCol {
			idx = i
			break
		}
	}
	t.commitCellEdit()
	t.mountEditor(cols[(idx+delta+len(cols))%len(cols)])
}

// stepEditRow accepts the edit, moves the selection up (-1) or down
// (+1), and immediately resumes editing the SAME column on that row.
func (t *TreeView) stepEditRow(delta int) {
	col := t.editCol
	t.endRowEdit(true)
	if ni := t.CurrentIndex() + delta; ni >= 0 && ni < len(t.flatList) {
		t.SetCurrentIndex(ni)
	}
	if it := t.CurrentItem(); it != nil && col != nil {
		t.beginCellEdit(it, col)
	}
}

// handleEditTargetKey rotates the Enter-target column with Left/Right
// while the GRID holds the focus (content zone, no editor up) and
// editing is available: the FocusedListItem highlight walks the
// editable columns - choosing WHERE Enter or a click will edit -
// without entering edit mode, and the chosen column is brought into
// horizontal view conservatively. In an editable grid the plain
// arrows belong to this; expand/collapse keeps Shift+Left/Right,
// +/-, Space, and the mouse. Returns handled.
func (t *TreeView) handleEditTargetKey(event core.KeyPressEvent) bool {
	if event.Key != "Left" && event.Key != "Right" {
		return false
	}
	if event.Modifiers&core.ShiftModifier != 0 {
		return false // Shift+Left/Right are the classic expand/collapse
	}
	if !t.multiColumn() || t.rowEditing || t.headerZone != hzContent {
		return false
	}
	cols := t.editableColumns()
	if len(cols) == 0 {
		return false // not an editable grid: classic tree navigation
	}
	delta := 1
	if event.Key == "Left" {
		delta = -1
	}
	cur := t.enterTargetColumn()
	idx := 0
	for i, c := range cols {
		if c == cur {
			idx = i
			break
		}
	}
	next := cols[(idx+delta+len(cols))%len(cols)]
	t.editLastCol = next
	t.ensureColVisible(next)
	t.Update()
	return true
}

// handleEditKey routes keys while the row editor is up. Everything is
// consumed: navigation belongs to the editor session, the rest belongs
// to the editor trinket.
func (t *TreeView) handleEditKey(event core.KeyPressEvent) bool {
	if !t.rowEditing {
		return false
	}
	if cb := t.editCombo; cb != nil {
		if cb.IsOpen() {
			// The open drop-down owns the keyboard until it closes:
			// Up/Down navigate its items, Enter/Space confirm the
			// highlighted value, Escape reverts. Enter and Escape then
			// also end the row edit (commit / cancel); Space keeps the
			// edit session alive for further changes.
			cb.HandleKeyPress(event)
			if !cb.IsOpen() {
				switch event.Key {
				case "Enter":
					t.endRowEdit(true)
				case "Escape":
					t.endRowEdit(false)
				}
			}
			return true
		}
		switch {
		case event.Key == "Enter":
			t.endRowEdit(true)
		case event.Key == "Escape":
			t.endRowEdit(false)
		case isShiftTab(event):
			t.stepEditColumn(-1)
		case event.Key == "Tab":
			t.stepEditColumn(1)
		case event.Key == "Up":
			// Row navigation, NOT value change - deliberately unlike
			// a native closed combo, matching the text editor's flow.
			t.stepEditRow(-1)
		case event.Key == "Down":
			t.stepEditRow(1)
		case event.Key == " " || event.Key == "Space":
			cb.HandleKeyPress(event) // pops the drop-down open
		}
		return true // a closed choice cell types nothing
	}
	if t.editBox == nil {
		return false
	}
	switch {
	case event.Key == "Enter":
		t.endRowEdit(true)
	case event.Key == "Escape":
		t.endRowEdit(false)
	case isShiftTab(event):
		t.stepEditColumn(-1)
	case event.Key == "Tab":
		t.stepEditColumn(1)
	case event.Key == "Up":
		t.stepEditRow(-1)
	case event.Key == "Down":
		t.stepEditRow(1)
	default:
		t.editBox.HandleKeyPress(event)
	}
	return true
}

// editorRect returns the edited cell's rect in tree-local units.
// ok=false when the cell is currently invisible (scrolled away or the
// column was hidden mid-edit) - the edit stays alive, just unpainted.
func (t *TreeView) editorRect() (core.UnitRect, bool) {
	if !t.rowEditing || t.editCol == nil || t.editItem == nil {
		return core.UnitRect{}, false
	}
	idx := -1
	for i, it := range t.flatList {
		if it == t.editItem {
			idx = i
			break
		}
	}
	row := idx - t.scrollOffset
	if idx < 0 || row < 0 || row >= t.visibleCount() {
		return core.UnitRect{}, false
	}
	metrics := t.EffectiveCellMetrics()
	lay := t.columnLayout()
	host := t.treeHostColumn()
	for _, sp := range lay.spans {
		if !spanMatchesCol(sp, t.editCol) {
			continue
		}
		clip, ok := lay.spanClip(sp, metrics.CellHeight)
		if !ok {
			return core.UnitRect{}, false
		}
		y := lay.headerH + core.Unit(row)*metrics.CellHeight
		r := core.UnitRect{X: clip.X, Y: y, Width: clip.Width, Height: metrics.CellHeight}
		// A tree-hosting cell's editor starts where the caption TEXT
		// starts - past the indent, expander, and icon - so it lines
		// up with the value it replaces.
		if sp.col == nil || (host != nil && sp.col == host) {
			if textX := sp.x + t.treeCellTextInset(t.editItem); textX > r.X {
				d := textX - r.X
				if d >= r.Width {
					return core.UnitRect{}, false // fully in the apparatus clip
				}
				r.X += d
				r.Width -= d
			}
		}
		return r, true
	}
	return core.UnitRect{}, false
}

// paintRowEditor paints the live cell editor over the grid (called at
// the end of paintMulti, above everything).
func (t *TreeView) paintRowEditor(p *core.Painter) {
	if !t.rowEditing {
		return
	}
	r, ok := t.editorRect()
	if !ok {
		return
	}
	switch {
	case t.editBox != nil:
		t.editBox.SetBounds(core.UnitRect{Width: r.Width, Height: r.Height})
		t.editBox.Paint(p.WithOffset(r.X, r.Y))
	case t.editCombo != nil:
		t.editCombo.SetBounds(core.UnitRect{Width: r.Width, Height: r.Height})
		t.editCombo.Paint(p.WithOffset(r.X, r.Y))
	}
}

// handleEditMousePress routes a press while editing: inside the editor
// it goes to the editor trinket (caret/selection, or the combo's
// open/close); anywhere else ACCEPTS the value and lets the press
// proceed normally. Returns handled.
func (t *TreeView) handleEditMousePress(event core.MousePressEvent) bool {
	if !t.rowEditing {
		return false
	}
	if r, ok := t.editorRect(); ok {
		pt := core.UnitPoint{X: event.X, Y: event.Y}
		if pt.X >= r.X && pt.X < r.X+r.Width && pt.Y >= r.Y && pt.Y < r.Y+r.Height {
			ev := event
			ev.X -= r.X
			ev.Y -= r.Y
			t.editMouseDown = true
			switch {
			case t.editBox != nil:
				t.editBox.HandleMousePress(ev)
			case t.editCombo != nil:
				t.editCombo.HandleMousePress(ev)
			}
			return true
		}
	}
	t.endRowEdit(true) // clicking off accepts; the click falls through
	return false
}

// handleEditMouseMove forwards drags that started inside the editor
// (text selection; the combo's hold-and-drag popup mode). Returns
// handled.
func (t *TreeView) handleEditMouseMove(event core.MouseMoveEvent) bool {
	if !t.rowEditing || !t.editMouseDown {
		return false
	}
	if r, ok := t.editorRect(); ok {
		ev := event
		ev.X -= r.X
		ev.Y -= r.Y
		switch {
		case t.editBox != nil:
			t.editBox.HandleMouseMove(ev)
		case t.editCombo != nil:
			t.editCombo.HandleMouseMove(ev)
		}
	}
	return true
}

// handleEditMouseRelease completes an editor-internal press. Returns
// handled.
func (t *TreeView) handleEditMouseRelease(event core.MouseReleaseEvent) bool {
	if !t.editMouseDown {
		return false
	}
	t.editMouseDown = false
	if t.rowEditing {
		if r, ok := t.editorRect(); ok {
			ev := event
			ev.X -= r.X
			ev.Y -= r.Y
			switch {
			case t.editBox != nil:
				t.editBox.HandleMouseRelease(ev)
			case t.editCombo != nil:
				t.editCombo.HandleMouseRelease(ev)
			}
		}
	}
	return true
}

// --- click-to-edit (a click on the already-selected row) ---

// treeCellEditZone is the caption's clickable/highlight zone within a
// tree-hosting span: origin at the text start (past indent, expander,
// icon), width = the DISPLAYED text (the ellipsis, when cut,
// excluded), with a two-cell minimum, clamped to the span. Shared by
// the mouse resolver and the Enter-target highlight so they can never
// disagree.
func (t *TreeView) treeCellEditZone(sp colSpan, item *TreeItem) (x0, w core.Unit) {
	metrics := t.EffectiveCellMetrics()
	textX := sp.x + t.treeCellTextInset(item)
	avail := sp.x + sp.w - textX
	if avail <= 0 {
		return textX, 0
	}
	text := item.Text
	if sp.col != nil {
		text = sp.col.displayValue(item.Value(sp.col.ID))
	}
	font := t.EffectiveFont()
	shown := strings.TrimSuffix(ellipsizeText(font, text, avail), "…")
	zone := font.MeasureText(shown)
	if min := 2 * metrics.CellWidth; zone < min {
		zone = min
	}
	if zone > avail {
		zone = avail
	}
	return textX, zone
}

// editableColumnAt resolves which editable column the point x (in a
// content row) addresses: the column under x when it is editable -
// with the tree-hosting cell counting only its caption's DISPLAYED
// text zone (the ellipsis, when cut, excluded; two-cell minimum so
// blank or tiny captions stay reachable), the Finder/Explorer
// convention. Left of that (indent/expander/icon) and right of it
// resolve to nil: the classic click/double-click regions. Selection
// clicks and drags use this to move the Enter target with the mouse.
func (t *TreeView) editableColumnAt(x core.Unit, item *TreeItem) *TreeColumn {
	if !t.multiColumn() || item == nil {
		return nil
	}
	metrics := t.EffectiveCellMetrics()
	lay := t.columnLayout()
	host := t.treeHostColumn()
	for _, sp := range lay.spans {
		col := sp.col
		if col == nil {
			if !t.keyEditable {
				continue
			}
			col = treeKeyColumn
		} else if !col.Editable {
			continue
		}
		clip, ok := lay.spanClip(sp, metrics.CellHeight)
		if !ok || x < clip.X || x >= clip.X+clip.Width {
			continue
		}
		if sp.col == nil || (host != nil && sp.col == host) {
			zx, zw := t.treeCellEditZone(sp, item)
			if x < zx || x >= zx+zw {
				continue
			}
		}
		return col
	}
	return nil
}

// noteClickEditPress records, at press time, whether this click landed
// on an editable cell of the ALREADY selected row.
func (t *TreeView) noteClickEditPress(event core.MousePressEvent) {
	t.clickEditItem = nil
	if !t.multiColumn() || t.rowEditing {
		return
	}
	headerH := t.headerHeight()
	if event.Y < headerH {
		return
	}
	metrics := t.EffectiveCellMetrics()
	row := t.scrollOffset + int((event.Y-headerH)/metrics.CellHeight)
	if row != t.currentIndex || row < 0 || row >= len(t.flatList) {
		return
	}
	item := t.flatList[row]
	col := t.editableColumnAt(event.X, item)
	if col == nil {
		return
	}
	t.clickEditItem = item
	t.clickEditCol = col
	t.clickEditX, t.clickEditY = event.X, event.Y
}

// armClickEdit begins the edit IMMEDIATELY on a drag-free release over
// the press-time candidate - the second click on an already-selected
// row flips straight into edit mode, no double-click settle delay.
func (t *TreeView) armClickEdit(event core.MouseReleaseEvent) {
	if t.clickEditItem == nil {
		return
	}
	item, col := t.clickEditItem, t.clickEditCol
	t.clickEditItem = nil
	dx, dy := event.X-t.clickEditX, event.Y-t.clickEditY
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	if dx > treeClickEditSlop || dy > treeClickEditSlop {
		return // a drag, not a click
	}
	if t.rowEditing || t.CurrentItem() != item || !t.colEditable(col) {
		return
	}
	t.beginCellEdit(item, col)
	// MOUSE entry into a choice cell pops the drop-down immediately -
	// the pointer came here to pick a value. (Keyboard entry leaves it
	// closed so Tab and Up/Down can still pass to other cells/rows.)
	if t.editCombo != nil {
		t.editCombo.HandleKeyPress(core.KeyPressEvent{Key: " "})
	}
}

// cancelClickEdit drops any press-time click-to-edit candidate.
func (t *TreeView) cancelClickEdit() {
	t.clickEditItem = nil
}
