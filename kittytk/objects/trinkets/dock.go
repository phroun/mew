// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"github.com/phroun/kittytk/core"
)

// DockEntry represents a minimized window in the dock.
type DockEntry struct {
	Title   string
	OnClick func()

	// WindowID is the stable identity of the minimized window. Entries
	// are added/removed by ID: titles are display text, not identity
	// (two windows may share a title).
	WindowID core.ObjectID
}

// DockRow displays minimized windows as clickable buttons.
// It expands to multiple rows if needed and hides when empty.
type DockRow struct {
	core.TrinketBase

	// Minimized window entries
	entries []*DockEntry

	// Layout configuration
	entryWidth int // Width in characters per entry

	// Keyboard navigation
	selectedIndex int // Currently selected entry when focused (-1 = none)

	// Pointer hover
	hoverIndex int // Entry currently under the pointer (-1 = none)

	// Focus transfer callback (called when Tab falls off either end)
	onFocusMenuBar func()
}

// NewDockRow creates a new dock row.
func NewDockRow() *DockRow {
	d := &DockRow{
		entryWidth:    16, // Default 16 chars per entry
		selectedIndex: -1,
		hoverIndex:    -1,
	}
	d.TrinketBase = *core.NewTrinketBase()
	d.Init(d)
	d.SetFocusPolicy(core.StrongFocus)
	return d
}

// AddEntry adds a minimized window entry to the dock.
// The newly added entry becomes the selected item.
func (d *DockRow) AddEntry(entry *DockEntry) {
	d.entries = append(d.entries, entry)
	d.selectedIndex = len(d.entries) - 1
	d.Update()
}

// RemoveEntry removes an entry from the dock.
// After removal, the selection moves to the most recently added entry (last in list).
func (d *DockRow) RemoveEntry(entry *DockEntry) {
	for i, e := range d.entries {
		if e == entry {
			d.entries = append(d.entries[:i], d.entries[i+1:]...)
			// Select the last entry (most recently added)
			if len(d.entries) > 0 {
				d.selectedIndex = len(d.entries) - 1
			} else {
				d.selectedIndex = -1
			}
			d.Update()
			return
		}
	}
}

// Entries returns the dock's current entries in display order.
func (d *DockRow) Entries() []*DockEntry {
	return d.entries
}

// RemoveEntryByID removes an entry by its window's object identity.
// After removal, the selection moves to the most recently added entry.
func (d *DockRow) RemoveEntryByID(id core.ObjectID) {
	for i, e := range d.entries {
		if e.WindowID == id {
			d.entries = append(d.entries[:i], d.entries[i+1:]...)
			if len(d.entries) > 0 {
				d.selectedIndex = len(d.entries) - 1
			} else {
				d.selectedIndex = -1
			}
			d.Update()
			return
		}
	}
}

// RemoveEntryByTitle removes an entry by its title.
// After removal, the selection moves to the most recently added entry (last in list).
//
// Deprecated: titles are display text, not identity - two windows may
// share one. Use RemoveEntryByID.
func (d *DockRow) RemoveEntryByTitle(title string) {
	for i, e := range d.entries {
		if e.Title == title {
			d.entries = append(d.entries[:i], d.entries[i+1:]...)
			// Select the last entry (most recently added)
			if len(d.entries) > 0 {
				d.selectedIndex = len(d.entries) - 1
			} else {
				d.selectedIndex = -1
			}
			d.Update()
			return
		}
	}
}

// Clear removes all entries.
func (d *DockRow) Clear() {
	d.entries = nil
	d.Update()
}

// EntryCount returns the number of entries.
func (d *DockRow) EntryCount() int {
	return len(d.entries)
}

// IsEmpty returns true if the dock has no entries.
func (d *DockRow) IsEmpty() bool {
	return len(d.entries) == 0
}

// SetEntryWidth sets the width per entry in characters.
func (d *DockRow) SetEntryWidth(width int) {
	d.entryWidth = width
	d.Update()
}

// SetOnFocusMenuBar sets the callback for when Tab navigation should transfer to the menu bar.
func (d *DockRow) SetOnFocusMenuBar(callback func()) {
	d.onFocusMenuBar = callback
}

// entriesPerRow returns how many entries fit per row based on current bounds.
func (d *DockRow) entriesPerRow() int {
	bounds := d.Bounds()
	metrics := d.EffectiveCellMetrics()
	entriesPerRow := int(bounds.Width / (core.Unit(d.entryWidth) * metrics.CellWidth))
	if entriesPerRow < 1 {
		entriesPerRow = 1
	}
	return entriesPerRow
}

// RowCount returns the number of rows needed to display all entries.
func (d *DockRow) RowCount() int {
	if len(d.entries) == 0 {
		return 0
	}

	bounds := d.Bounds()
	metrics := d.EffectiveCellMetrics()

	// How many entries fit per row?
	entriesPerRow := int(bounds.Width / (core.Unit(d.entryWidth) * metrics.CellWidth))
	if entriesPerRow < 1 {
		entriesPerRow = 1
	}

	// Calculate rows needed
	rows := (len(d.entries) + entriesPerRow - 1) / entriesPerRow
	return rows
}

// RequiredHeight returns the height needed to display all entries.
func (d *DockRow) RequiredHeight() core.Unit {
	rows := d.RowCount()
	if rows == 0 {
		return 0
	}
	metrics := d.EffectiveCellMetrics()
	return core.Unit(rows) * metrics.CellHeight
}

// SizeHint returns the preferred size.
func (d *DockRow) SizeHint() core.UnitSize {
	return core.UnitSize{
		Width:  0, // Will stretch to fill
		Height: d.RequiredHeight(),
	}
}

// Paint renders the dock row.
func (d *DockRow) Paint(p *core.Painter) {
	if len(d.entries) == 0 {
		return
	}

	bounds := d.Bounds()
	metrics := d.EffectiveCellMetrics()
	focused := d.HasFocus()
	scheme := d.GetScheme()

	// Dock background style (item resting style)
	dockStyle := scheme.GetDockItem()

	// Draw background
	p.FillRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, ' ', dockStyle)

	// Calculate layout
	entryWidthUnits := core.Unit(d.entryWidth) * metrics.CellWidth
	entriesPerRow := int(bounds.Width / entryWidthUnits)
	if entriesPerRow < 1 {
		entriesPerRow = 1
	}

	// Draw entries
	for i, entry := range d.entries {
		row := i / entriesPerRow
		col := i % entriesPerRow

		x := core.Unit(col) * entryWidthUnits
		y := core.Unit(row) * metrics.CellHeight

		// Choose style based on state; focus takes priority over hover. Hover
		// is graphical-only (the cell/TUI path gets no free moves, so a hover
		// set during a drag could never clear).
		entryStyle := scheme.GetDockItemState(focused && i == d.selectedIndex, i == d.hoverIndex && p.Graphical())

		// Draw entry background (button-like)
		entryRect := core.UnitRect{
			X:      x,
			Y:      y,
			Width:  entryWidthUnits,
			Height: metrics.CellHeight,
		}
		p.FillRect(entryRect, ' ', entryStyle)

		// Draw border characters. The brackets and the slot width stay
		// monospaced (one cell each edge); only the interior title uses the
		// proportional font.
		p.DrawCell(x, y, '[', entryStyle)
		p.DrawCell(x+entryWidthUnits-metrics.CellWidth, y, ']', entryStyle)

		// Draw the title proportionally within the interior (between the
		// brackets), truncating with an ellipsis to fit the interior width.
		font := d.EffectiveFont()
		interiorX := x + metrics.CellWidth
		interiorWidth := entryWidthUnits - 2*metrics.CellWidth

		title := entry.Title
		if font.MeasureText(title) > interiorWidth {
			ellipsis := "…"
			ellipsisW := font.MeasureText(ellipsis)
			runes := []rune(title)
			for len(runes) > 0 && font.MeasureText(string(runes))+ellipsisW > interiorWidth {
				runes = runes[:len(runes)-1]
			}
			title = string(runes) + ellipsis
		}
		// Clip to the interior so proportional text can never spill onto the
		// closing bracket.
		titlePainter := p.WithClip(core.UnitRect{
			X:      interiorX,
			Y:      y,
			Width:  interiorWidth,
			Height: metrics.CellHeight,
		})
		titlePainter.DrawText(interiorX, y, title, entryStyle, font)
	}
}

// SelectedIndex returns the currently selected entry index (-1 if none).
func (d *DockRow) SelectedIndex() int {
	return d.selectedIndex
}

// SetSelectedIndex sets the selected entry index.
func (d *DockRow) SetSelectedIndex(index int) {
	if index < -1 {
		index = -1
	}
	if index >= len(d.entries) {
		index = len(d.entries) - 1
	}
	d.selectedIndex = index
	d.Update()
}

// HandleFocusIn is called when focus is gained.
func (d *DockRow) HandleFocusIn() {
	// Select first entry when gaining focus
	if len(d.entries) > 0 && d.selectedIndex < 0 {
		d.selectedIndex = 0
	}
	d.Update()
}

// HandleFocusOut is called when focus is lost.
func (d *DockRow) HandleFocusOut() {
	d.selectedIndex = -1
	d.Update()
}

// HandleKeyPress handles keyboard input.
func (d *DockRow) HandleKeyPress(event core.KeyPressEvent) bool {
	if len(d.entries) == 0 {
		return false
	}

	entriesPerRow := d.entriesPerRow()

	switch event.Key {
	case "Left":
		if d.selectedIndex > 0 {
			d.selectedIndex--
			d.Update()
		}
		return true

	case "Right":
		if d.selectedIndex < len(d.entries)-1 {
			d.selectedIndex++
			d.Update()
		}
		return true

	case "Up":
		// Move to same column in previous row
		if d.selectedIndex >= entriesPerRow {
			d.selectedIndex -= entriesPerRow
			d.Update()
		}
		return true

	case "Down":
		// Move to same column in next row
		newIndex := d.selectedIndex + entriesPerRow
		if newIndex < len(d.entries) {
			d.selectedIndex = newIndex
			d.Update()
		}
		return true

	case "Tab":
		if event.Modifiers&core.ShiftModifier != 0 {
			// Shift+Tab: move to previous item, or to menu bar if at start
			if d.selectedIndex > 0 {
				d.selectedIndex--
				d.Update()
			} else if d.onFocusMenuBar != nil {
				d.onFocusMenuBar()
			}
		} else {
			// Tab: move to next item, or to menu bar if at end
			if d.selectedIndex < len(d.entries)-1 {
				d.selectedIndex++
				d.Update()
			} else if d.onFocusMenuBar != nil {
				d.onFocusMenuBar()
			}
		}
		return true

	case "Home":
		d.selectedIndex = 0
		d.Update()
		return true

	case "End":
		d.selectedIndex = len(d.entries) - 1
		d.Update()
		return true

	case "Enter", " ", "Space":
		// Activate selected entry
		if d.selectedIndex >= 0 && d.selectedIndex < len(d.entries) {
			entry := d.entries[d.selectedIndex]
			if entry.OnClick != nil {
				entry.OnClick()
			}
		}
		return true
	}

	return false
}

// HandleMousePress handles mouse clicks.
func (d *DockRow) HandleMousePress(event core.MousePressEvent) bool {
	if len(d.entries) == 0 {
		return false
	}

	metrics := d.EffectiveCellMetrics()
	bounds := d.Bounds()

	// Calculate which entry was clicked
	entryWidthUnits := core.Unit(d.entryWidth) * metrics.CellWidth
	entriesPerRow := int(bounds.Width / entryWidthUnits)
	if entriesPerRow < 1 {
		entriesPerRow = 1
	}

	row := int(event.Y / metrics.CellHeight)
	col := int(event.X / entryWidthUnits)

	index := row*entriesPerRow + col
	if index >= 0 && index < len(d.entries) {
		entry := d.entries[index]
		if entry.OnClick != nil {
			entry.OnClick()
		}
		return true
	}

	return false
}

// entryAt maps a pointer position to the entry index under it, or -1 when
// the pointer is over dock background or a dead slot past the last column.
func (d *DockRow) entryAt(x, y core.Unit) int {
	if len(d.entries) == 0 {
		return -1
	}
	metrics := d.EffectiveCellMetrics()
	bounds := d.Bounds()
	if x < 0 || y < 0 || x >= bounds.Width || y >= bounds.Height {
		return -1
	}
	entryWidthUnits := core.Unit(d.entryWidth) * metrics.CellWidth
	entriesPerRow := int(bounds.Width / entryWidthUnits)
	if entriesPerRow < 1 {
		entriesPerRow = 1
	}
	row := int(y / metrics.CellHeight)
	col := int(x / entryWidthUnits)
	if col >= entriesPerRow {
		return -1 // dead space past the last column of a row
	}
	index := row*entriesPerRow + col
	if index < 0 || index >= len(d.entries) {
		return -1
	}
	return index
}

// HandleMouseMove tracks which entry the pointer is hovering over so the
// dock can highlight it.
func (d *DockRow) HandleMouseMove(event core.MouseMoveEvent) bool {
	// Hover is a no-button affordance: a held button means a drag begun
	// elsewhere is passing over, so clear rather than highlight an entry.
	idx := -1
	if event.Buttons == 0 {
		idx = d.entryAt(event.X, event.Y)
	}
	if idx != d.hoverIndex {
		d.hoverIndex = idx
		d.Update()
	}
	return false
}
