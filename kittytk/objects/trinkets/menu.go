// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"strings"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// shortcutNativeSizeNum/Den scale the native-mode shortcut to 80% of the base
// font size: Apple's UI face renders visually larger than the menu's body
// face at the same point size, so the shortcut column is shrunk to sit
// comfortably beside the item text.
const (
	shortcutNativeSizeNum = 4
	shortcutNativeSizeDen = 5
)

// graphicalMenuTrailingUnits is the small gap kept to the right of a
// graphical menu's shortcut, between it and the menu's right edge. Graphical
// menus have only a 1-pixel right stroke (not a whole char border), so this is
// about three-quarters of a cell rather than the two cells cell/TUI menus
// reserve.
func graphicalMenuTrailingUnits(metrics core.CellMetrics) core.Unit {
	return metrics.CellWidth * 3 / 4
}

// shortcutFont returns the font used to draw a menu item's shortcut. In
// macOS-native mode it swaps the family to Apple's UI face (so the ⌃⌥⇧⌘ glyphs
// render in Apple's typeface) and shrinks it to 80%, while keeping the style
// and colors of the base font; otherwise it returns the base unchanged. The
// returned font is a copy, so the shared base font is never mutated, and
// callers measure and draw with this same font so widths stay exact.
func shortcutFont(base *core.Font) *core.Font {
	if base == nil || !core.MacNativeShortcuts() {
		return base
	}
	f := *base
	f.Name = core.MacShortcutFontFamily
	if s := base.Size * shortcutNativeSizeNum / shortcutNativeSizeDen; s > 0 {
		f.Size = s
	}
	return &f
}

// MenuItem represents an item in a menu.
type MenuItem struct {
	Text            string // Display text (with & removed, && converted to &)
	rawText         string // Original text with & markup
	acceleratorChar rune   // The accelerator character (lowercase), 0 if none
	acceleratorPos  int    // Position in display text where accelerator appears, -1 if none
	Shortcut        core.Shortcut
	Icon            *style.TextIcon
	Enabled         bool
	Checkable       bool
	Checked         bool
	Separator       bool // If true, this is a separator line
	// InPlace: activating this item performs its action but KEEPS the
	// menu open, re-rendering the updated content in place (checkable
	// toggles that users flip several times in a row - column choosers,
	// view options). Escape or a click away still dismisses.
	InPlace     bool
	wellKnownID string // system-level role tag (see MenuID* constants), "" if none

	// Submenu
	SubMenu *Menu

	// Callbacks
	OnTriggered func()

	// id is the stable command identity used for dispatch (and, under
	// the display protocol, the wire). Auto-assigned; override with
	// SetID for a semantic, run-stable ID like "file.open".
	id string

	// commands is the registry this item dispatches through once bound
	// (see Menu.BindCommands). Nil = direct closure fallback.
	commands *core.CommandRegistry
}

// NewMenuItem creates a new menu item.
func NewMenuItem(text string) *MenuItem {
	displayText, accel, pos := parseAcceleratorTitle(text)
	return &MenuItem{
		Text:            displayText,
		rawText:         text,
		acceleratorChar: accel,
		acceleratorPos:  pos,
		Enabled:         true,
		id:              core.NextAutoCommandID(),
	}
}

// SetText sets the menu item text with accelerator parsing.
func (m *MenuItem) SetText(text string) {
	displayText, accel, pos := parseAcceleratorTitle(text)
	m.rawText = text
	m.Text = displayText
	m.acceleratorChar = accel
	m.acceleratorPos = pos
}

// AcceleratorChar returns the accelerator character (lowercase) or 0 if none.
func (m *MenuItem) AcceleratorChar() rune {
	return m.acceleratorChar
}

// AcceleratorPos returns the position in the display text where the accelerator
// character appears, or -1 if none.
func (m *MenuItem) AcceleratorPos() int {
	return m.acceleratorPos
}

// NewSeparator creates a separator menu item.
func NewSeparator() *MenuItem {
	return &MenuItem{
		Separator: true,
		id:        core.NextAutoCommandID(),
	}
}

// ID returns the item's stable command identity.
func (m *MenuItem) ID() string {
	return m.id
}

// SetID sets a semantic command ID (e.g. "file.open", see
// core.StandardActions). Set it before the menu is bound to a
// registry; IDs are the dispatch key.
func (m *MenuItem) SetID(id string) *MenuItem {
	if id != "" {
		m.id = id
	}
	return m
}

// SetShortcut sets the keyboard shortcut.
func (m *MenuItem) SetShortcut(shortcut core.Shortcut) *MenuItem {
	m.Shortcut = shortcut
	return m
}

// SetIcon sets the icon.
func (m *MenuItem) SetIcon(icon *style.TextIcon) *MenuItem {
	m.Icon = icon
	return m
}

// SetInPlace marks the item as acting in place: triggering it runs the
// action and keeps the menu open (see the InPlace field).
func (m *MenuItem) SetInPlace(inPlace bool) *MenuItem {
	m.InPlace = inPlace
	return m
}

// SetCheckable sets whether the item is checkable.
func (m *MenuItem) SetCheckable(checkable bool) *MenuItem {
	m.Checkable = checkable
	return m
}

// SetChecked sets the checked state.
func (m *MenuItem) SetChecked(checked bool) *MenuItem {
	m.Checked = checked
	return m
}

// SetEnabled sets whether the item is enabled.
func (m *MenuItem) SetEnabled(enabled bool) *MenuItem {
	m.Enabled = enabled
	return m
}

// SetWellKnownID tags the item with a system-level role identifier (see the
// MenuID* constants). Any string is accepted; only the known ones carry
// meaning.
func (m *MenuItem) SetWellKnownID(id string) *MenuItem {
	m.wellKnownID = id
	return m
}

// WellKnownID returns the item's system-level role tag, or "" if none.
func (m *MenuItem) WellKnownID() string { return m.wellKnownID }

// SetSubMenu sets the submenu.
func (m *MenuItem) SetSubMenu(menu *Menu) *MenuItem {
	m.SubMenu = menu
	return m
}

// SetOnTriggered sets the triggered callback. If the item is already
// bound to a command registry, the registration is refreshed.
func (m *MenuItem) SetOnTriggered(handler func()) *MenuItem {
	m.OnTriggered = handler
	if m.commands != nil {
		m.commands.Register(m.id, handler)
	}
	return m
}

// Trigger triggers the menu item action. When bound to a command
// registry, dispatch goes by stable ID through the registry (the D2
// display-protocol seam); otherwise the direct closure runs.
func (m *MenuItem) Trigger() {
	if m.Checkable {
		m.Checked = !m.Checked
	}
	if m.commands != nil && m.commands.Dispatch(m.id) {
		return
	}
	if m.OnTriggered != nil {
		m.OnTriggered()
	}
}

// bindCommands registers this item's handler under its command ID and
// routes future triggers through the registry. Recurses into submenus.
func (m *MenuItem) bindCommands(reg *core.CommandRegistry) {
	if m.Separator {
		return
	}
	if m.OnTriggered != nil {
		reg.Register(m.id, m.OnTriggered)
	}
	m.commands = reg
	if m.SubMenu != nil {
		m.SubMenu.BindCommands(reg)
	}
}

// BindCommands registers this menu's item handlers (recursively, with
// submenus) in the given registry, keyed by command ID, and routes all
// future triggers through it. This is the D2 seam: menu activation
// becomes "command <ID> triggered" dispatched at one boundary, instead
// of closures invoked from inside UI objects. Applications bind their
// menu bar content automatically (see Application.SetMenuBarContent);
// the desktop binds its system menu.
func (menu *Menu) BindCommands(reg *core.CommandRegistry) {
	if reg == nil {
		return
	}
	for _, item := range menu.items {
		item.bindCommands(reg)
	}
}

// Well-known menu identifiers. An app tags a menu (or menu item) with one
// of these so the system can recognize its role - place it, merge into it,
// or inject standard items - independently of the menu's display title. Any
// string may be stored, but only these carry system meaning.
const (
	MenuIDApp    = "app"    // the app's leading menu (≡/application menu)
	MenuIDFile   = "file"   // File
	MenuIDEdit   = "edit"   // Edit (the system supplies Cut/Copy/Paste/Select All)
	MenuIDSelect = "select" // Select
	MenuIDFormat = "format" // Format
	MenuIDView   = "view"   // View
	MenuIDWindow = "window" // Window (the system manages its window list)
	MenuIDHelp   = "help"   // Help (kept last, after the Window menu)
)

// Menu represents a dropdown menu.
type Menu struct {
	core.TrinketBase
	core.AccessibleTrinket

	title           string // Display title (with & removed, && converted to &)
	rawTitle        string // Original title with & markup
	acceleratorChar rune   // The accelerator character (lowercase), 0 if none
	acceleratorPos  int    // Position in display title where accelerator appears, -1 if none
	items           []*MenuItem
	currentIndex    int
	visible         bool
	wellKnownID     string // system-level role tag (see MenuID* constants), "" if none

	// Position when shown as popup
	popupX, popupY core.Unit

	// graphicalCached records whether the last paint was on a pixel
	// surface. Popup menus are not parented into the trinket tree, so
	// FindGraphicalFrames can't discover the surface; the painter can,
	// and layout/hit-test (which have no painter) read this cache.
	graphicalCached bool
	graphicalKnown  bool

	// strokeGap{X,W}: the horizontal span (in this menu's coordinate
	// space) where the outer stroke omits one edge, so the border merges
	// with the control that opened the menu instead of drawing a line
	// against it - a menu-bar item, or a combobox. Zero width = no gap
	// (context menus, submenus). strokeGapBottom selects the bottom edge
	// (a drop-up) rather than the top (a drop-down).
	strokeGapX      core.Unit
	strokeGapW      core.Unit
	strokeGapBottom bool

	// Parent menu (for submenus)
	parentMenu *Menu
	parentItem *MenuItem

	// Currently open submenu
	activeSubMenu *Menu

	// Scroll state
	scrollOffset    int       // First visible item index
	maxVisible      int       // Max items to show (0 = unlimited)
	scrollHoverTime time.Time // When drag started hovering over scroll indicator
	scrollHoverZone int       // -1 = top indicator, 1 = bottom indicator, 0 = none
	clickedMode     bool      // If true, was opened via click (not drag), release won't dismiss
	screenBottom    core.Unit // Bottom of available screen area (for submenu height calculation)

	// Timer for continuous scroll while hovering over scroll indicators
	scrollTimer        interface{ Stop() }
	scrollTimerStarter func(interval time.Duration, callback func()) interface{ Stop() }
	requestUpdate      func() // Called to request a screen update after timer scroll

	// Callbacks
	onAboutToShow func()
	onAboutToHide func()
	onItemPressed func() // Called when an item is pressed, signals MenuBar to enter drag mode
	onWillTrigger func() // Called just before an item is triggered, to restore window focus

	// Accessibility
	accessibilityManager *core.AccessibilityManager
}

// parseAcceleratorTitle parses a title with & markup.
// Returns: display title, accelerator character (lowercase), position in display title
// Examples: "&File" -> "File", 'f', 0
//
//	"E&xit" -> "Exit", 'x', 1
//	"Save && Exit" -> "Save & Exit", 0, -1
func parseAcceleratorTitle(raw string) (display string, accel rune, pos int) {
	pos = -1
	runes := []rune(raw)
	var result []rune

	for i := 0; i < len(runes); i++ {
		if runes[i] == '&' {
			if i+1 < len(runes) && runes[i+1] == '&' {
				// Escaped ampersand
				result = append(result, '&')
				i++ // Skip next &
			} else if i+1 < len(runes) {
				// Accelerator - next char is the accelerator
				if pos < 0 { // Only use first accelerator
					pos = len(result)
					accel = rune(strings.ToLower(string(runes[i+1]))[0])
				}
				result = append(result, runes[i+1])
				i++ // Skip the accelerator char (we already added it)
			}
			// else: trailing & is just dropped
		} else {
			result = append(result, runes[i])
		}
	}

	display = string(result)
	return
}

// textSegment is one styled run in a left-to-right sequence drawn by
// drawTextSegments.
type textSegment struct {
	text  string
	style style.CellStyle
}

// drawTextSegments draws styled text segments left-to-right starting at
// unit (x, y). On a pixel surface it accumulates the device-pixel advance
// from the single anchor at (x, y) - so each successive segment abuts the
// previous one exactly on the glyphs - instead of re-snapping each
// intermediate unit position through the cell rate, which at a fractional
// font size leaves a gap (or overlap) where the two rates diverge. On a
// cell surface it falls back to whole-unit DrawText advances. Returns the
// total advance in units.
func drawTextSegments(p *core.Painter, x, y core.Unit, font *core.Font, segs ...textSegment) core.Unit {
	_, usePx := p.DrawTextOffset(x, y, 0, 0, "", style.CellStyle{}, font)
	total := core.Unit(0)
	xPx := 0
	for _, seg := range segs {
		if seg.text == "" {
			continue
		}
		if usePx {
			adv, _ := p.DrawTextOffset(x, y, xPx, 0, seg.text, seg.style, font)
			xPx += adv
		} else {
			p.DrawText(x+total, y, seg.text, seg.style, font)
		}
		total += font.MeasureText(seg.text)
	}
	return total
}

// NewMenu creates a new menu.
func NewMenu(title string) *Menu {
	displayTitle, accel, pos := parseAcceleratorTitle(title)
	m := &Menu{
		rawTitle:        title,
		title:           displayTitle,
		acceleratorChar: accel,
		acceleratorPos:  pos,
		currentIndex:    -1,
		maxVisible:      0, // 0 = calculate from available space when shown
	}
	m.TrinketBase = *core.NewTrinketBase()
	// Note: Menu doesn't call Init because it has a Show(x,y) method
	// with different signature than Trinket.Show()
	m.SetFocusPolicy(core.StrongFocus)
	m.SetAccessibleRole(core.RoleMenu)
	m.SetAccessibleName(displayTitle)
	return m
}

// SetMaxVisible sets the maximum number of visible items (0 = unlimited).
func (m *Menu) SetMaxVisible(max int) {
	m.maxVisible = max
}

// SetAvailableHeight sets the available height for the menu and calculates maxVisible.
// This should be called before Show() to ensure proper scrolling behavior.
// The menuY parameter is the Y position where the menu will be shown.
func (m *Menu) SetAvailableHeight(availableHeight core.Unit) {
	metrics := m.EffectiveCellMetrics()
	// Calculate how many items can fit, leaving room for scroll indicators if needed
	maxRows := int(availableHeight / metrics.CellHeight)
	if maxRows < 3 {
		maxRows = 3 // Minimum: 1 item + 2 scroll indicators
	}
	// Reserve 2 rows for scroll indicators when there are more items than fit
	if len(m.items) > maxRows {
		m.maxVisible = maxRows - 2
	} else {
		m.maxVisible = 0 // No limit needed, all items fit
	}
}

// SetScreenBottom sets the bottom of the available screen area.
// This is used to calculate available height for submenus.
func (m *Menu) SetScreenBottom(bottom core.Unit) {
	m.screenBottom = bottom
}

// Title returns the menu title.
func (m *Menu) Title() string {
	return m.title
}

// RawTitle returns the original title including any "&" accelerator markup.
func (m *Menu) RawTitle() string {
	return m.rawTitle
}

// SetWellKnownID tags the menu with a system-level role identifier (see the
// MenuID* constants), letting the system recognize its role independently of
// its display title. Any string is accepted; only the known ones carry
// meaning. Returns the menu for chaining.
func (m *Menu) SetWellKnownID(id string) *Menu {
	m.wellKnownID = id
	return m
}

// WellKnownID returns the menu's system-level role tag, or "" if none.
func (m *Menu) WellKnownID() string { return m.wellKnownID }

// SetTitle sets the menu title.
func (m *Menu) SetTitle(title string) {
	displayTitle, accel, pos := parseAcceleratorTitle(title)
	m.rawTitle = title
	m.title = displayTitle
	m.acceleratorChar = accel
	m.acceleratorPos = pos
	m.SetAccessibleName(displayTitle)
}

// AcceleratorChar returns the accelerator character (lowercase) or 0 if none.
func (m *Menu) AcceleratorChar() rune {
	return m.acceleratorChar
}

// AcceleratorPos returns the position in the display title where the accelerator
// character appears, or -1 if none.
func (m *Menu) AcceleratorPos() int {
	return m.acceleratorPos
}

// AddItem adds an item to the menu.
func (m *Menu) AddItem(item *MenuItem) {
	m.items = append(m.items, item)
}

// AddAction adds an action as a menu item.
func (m *Menu) AddAction(action *core.Action) *MenuItem {
	item := NewMenuItem(action.Text)
	item.Shortcut = action.Shortcut
	item.Enabled = action.Enabled
	item.OnTriggered = action.OnTriggered
	m.AddItem(item)
	return item
}

// AddSeparator adds a separator.
func (m *Menu) AddSeparator() {
	m.AddItem(NewSeparator())
}

// AddMenu adds a submenu.
func (m *Menu) AddMenu(submenu *Menu) *MenuItem {
	item := NewMenuItem(submenu.title)
	item.SubMenu = submenu
	submenu.parentMenu = m
	submenu.parentItem = item
	m.AddItem(item)
	return item
}

// InsertItem inserts an item at the given index.
func (m *Menu) InsertItem(index int, item *MenuItem) {
	if index < 0 {
		index = 0
	}
	if index > len(m.items) {
		index = len(m.items)
	}
	m.items = append(m.items[:index], append([]*MenuItem{item}, m.items[index:]...)...)
}

// RemoveItem removes an item.
func (m *Menu) RemoveItem(item *MenuItem) {
	for i, it := range m.items {
		if it == item {
			m.items = append(m.items[:i], m.items[i+1:]...)
			break
		}
	}
}

// Clear removes all items.
func (m *Menu) Clear() {
	m.items = nil
	m.currentIndex = -1
}

// Items returns all items.
func (m *Menu) Items() []*MenuItem {
	return m.items
}

// ItemAt returns the item at the given index.
func (m *Menu) ItemAt(index int) *MenuItem {
	if index < 0 || index >= len(m.items) {
		return nil
	}
	return m.items[index]
}

// CurrentItem returns the currently highlighted item.
func (m *Menu) CurrentItem() *MenuItem {
	return m.ItemAt(m.currentIndex)
}

// SelectFirstItem highlights the first enabled item. Used when a menu is
// opened from the keyboard (Down/Space/Enter on a focused menu bar), so
// navigation starts on a real option instead of no selection.
func (m *Menu) SelectFirstItem() {
	m.currentIndex = m.findNextEnabled(-1)
	m.ensureVisible(m.currentIndex)
	m.announceCurrentItem()
	m.Update()
}

// IsVisible returns whether the menu is visible.
func (m *Menu) IsVisible() bool {
	return m.visible
}

// Show shows the menu at the given position.
func (m *Menu) Show(x, y core.Unit) {
	if m.onAboutToShow != nil {
		m.onAboutToShow()
	}

	m.popupX = x
	m.popupY = y
	m.visible = true
	m.currentIndex = -1 // No item selected until user hovers over one
	m.scrollOffset = 0
	m.scrollHoverZone = 0
	m.scrollHoverTime = time.Time{}
	// Note: Don't call SetFocus() here - the MenuBar retains focus and forwards
	// key events to the active menu. Taking focus would trigger HandleFocusOut
	// on the MenuBar which would close the menu we just opened.
	m.Update()
}

// SetClickedMode sets whether the menu is in clicked mode (release won't dismiss).
func (m *Menu) SetClickedMode(clicked bool) {
	m.clickedMode = clicked
}

// IsClickedMode returns whether the menu is in clicked mode.
func (m *Menu) IsClickedMode() bool {
	return m.clickedMode
}

// SetScrollTimerStarter sets the function used to start scroll timers.
// This should be called before showing the menu.
func (m *Menu) SetScrollTimerStarter(starter func(interval time.Duration, callback func()) interface{ Stop() }) {
	m.scrollTimerStarter = starter
}

// SetRequestUpdate sets the function to call for screen updates from timer callbacks.
func (m *Menu) SetRequestUpdate(fn func()) {
	m.requestUpdate = fn
}

// SetAccessibilityManager sets the accessibility manager for announcements.
func (m *Menu) SetAccessibilityManager(am *core.AccessibilityManager) {
	m.accessibilityManager = am
}

// stopScrollTimer stops any active scroll timer.
func (m *Menu) stopScrollTimer() {
	if m.scrollTimer != nil {
		m.scrollTimer.Stop()
		m.scrollTimer = nil
	}
}

// startScrollTimer starts a repeating timer for continuous scrolling.
func (m *Menu) startScrollTimer(direction int) {
	m.stopScrollTimer()
	if m.scrollTimerStarter == nil {
		return
	}
	m.scrollTimer = m.scrollTimerStarter(50*time.Millisecond, func() {
		// Verify scroll zone is still active (user might have moved mouse)
		if (direction < 0 && m.scrollHoverZone != -1) ||
			(direction > 0 && m.scrollHoverZone != 1) {
			return
		}
		// Scroll if possible
		if direction < 0 && m.canScrollUp() {
			m.scrollUp(1)
		} else if direction > 0 && m.canScrollDown() {
			m.scrollDown(1)
		}
		// Request screen update since timer runs outside normal event loop
		if m.requestUpdate != nil {
			m.requestUpdate()
		}
	})
}

// Hide hides the menu.
func (m *Menu) Hide() {
	m.stopScrollTimer()

	if m.activeSubMenu != nil {
		m.activeSubMenu.Hide()
		m.activeSubMenu = nil
	}

	if m.onAboutToHide != nil {
		m.onAboutToHide()
	}

	m.visible = false
	m.currentIndex = -1
	m.Update()
}

// SetOnAboutToShow sets the about to show callback.
func (m *Menu) SetOnAboutToShow(handler func()) {
	m.onAboutToShow = handler
}

// SetOnAboutToHide sets the about to hide callback.
func (m *Menu) SetOnAboutToHide(handler func()) {
	m.onAboutToHide = handler
}

// setOnWillTrigger sets the callback that is called just before a menu item is triggered.
// This is used by MenuBar to restore the previous window before the action executes.
func (m *Menu) setOnWillTrigger(handler func()) {
	m.onWillTrigger = handler
	// Propagate to submenus
	for _, item := range m.items {
		if item.SubMenu != nil {
			item.SubMenu.setOnWillTrigger(handler)
		}
	}
}

// findNextEnabled finds the next enabled item.
func (m *Menu) findNextEnabled(from int) int {
	for i := 1; i <= len(m.items); i++ {
		idx := (from + i) % len(m.items)
		if idx < 0 {
			idx = len(m.items) + idx
		}
		item := m.items[idx]
		if !item.Separator && item.Enabled {
			return idx
		}
	}
	return -1
}

// findPrevEnabled finds the previous enabled item.
func (m *Menu) findPrevEnabled(from int) int {
	n := len(m.items)
	if n == 0 {
		return -1
	}
	// When from is -1 (nothing selected), treat as 0 so going back wraps to last item
	if from < 0 {
		from = 0
	}
	for i := 1; i <= n; i++ {
		idx := ((from-i)%n + n) % n
		item := m.items[idx]
		if !item.Separator && item.Enabled {
			return idx
		}
	}
	return -1
}

// announceCurrentItem announces the currently selected menu item for accessibility.
func (m *Menu) announceCurrentItem() {
	if m.currentIndex < 0 || m.currentIndex >= len(m.items) {
		return
	}
	item := m.items[m.currentIndex]
	if item.Separator {
		return
	}

	// Use stored accessibility manager, or try parent chain as fallback
	am := m.accessibilityManager
	if am == nil {
		current := m.Parent()
		for current != nil {
			if provider, ok := current.(core.AccessibilityProvider); ok {
				am = provider.AccessibilityManager()
				break
			}
			current = current.Parent()
		}
	}
	if am == nil {
		return
	}

	// Build announcement
	text := item.Text
	extras := []string{}

	if item.Checkable {
		if item.Checked {
			extras = append(extras, "checked")
		} else {
			extras = append(extras, "unchecked")
		}
	}
	if item.SubMenu != nil {
		extras = append(extras, "submenu")
	}
	if item.Shortcut != "" {
		extras = append(extras, item.Shortcut.AccessibilityString())
	}
	if !item.Enabled {
		extras = append(extras, "disabled")
	}

	announcement := text + ", menu item"
	if len(extras) > 0 {
		announcement += ", " + strings.Join(extras, ", ")
	}
	// Arrowing through menu items is navigation: throttle the speech.
	am.AnnounceNavigation(announcement)
}

// calculateSize calculates the menu size.
func (m *Menu) calculateSize() core.UnitSize {
	metrics := m.EffectiveCellMetrics()
	font := m.EffectiveFont()

	// Calculate max width using font for text, cells for decorative elements
	maxWidth := core.Unit(0)
	for _, item := range m.items {
		// Item text uses font measurement
		itemWidth := font.MeasureText(item.Text)

		// Shortcut: spacing (3 cells) + shortcut text (font-based). Measure
		// with the same font used to draw it (native mode swaps in Apple's
		// face) so width and render never disagree.
		if item.Shortcut != "" {
			itemWidth += metrics.CellWidth * 3 // spacing before shortcut
			itemWidth += shortcutFont(font).MeasureText(item.Shortcut.DisplayString())
		}

		// Submenu arrow (3 cells) - decorative
		if item.SubMenu != nil {
			itemWidth += metrics.CellWidth * 3
		}

		if itemWidth > maxWidth {
			maxWidth = itemWidth
		}
	}

	// Add padding (gutter: 3 cells, content space: 1 cell, right border: 1 cell)
	maxWidth += metrics.CellWidth * 5

	// Sum the heights of the visible item rows (thin separators on
	// graphical surfaces are shorter than a text row), plus a full row
	// for each scroll indicator when scrolling.
	g := m.graphicalSurface()
	var height core.Unit
	visible := m.visibleItemCount()
	for i := 0; i < visible; i++ {
		idx := m.scrollOffset + i
		if idx >= len(m.items) {
			break
		}
		height += m.rowHeightAt(idx, g, metrics.CellHeight)
	}
	if m.needsScrolling() {
		height += 2 * metrics.CellHeight // one row per scroll indicator
	}

	return core.UnitSize{
		Width:  maxWidth,
		Height: height,
	}
}

// separatorBandUnits is the height a separator row occupies on graphical
// (pixel) surfaces - a thin band (~6 device px at the usual 2x scale)
// carrying a single hairline, rather than a full text row of dashes.
const separatorBandUnits core.Unit = 3

// graphicalSurface reports whether this dropdown paints on a pixel
// surface, where separators shrink to a thin band and gain hairlines.
// Popup menus aren't parented into the trinket tree, so prefer the value
// the painter observed on the last paint; fall back to a tree walk
// (which succeeds for menus that do have a parent chain).
func (m *Menu) graphicalSurface() bool {
	if m.graphicalKnown {
		return m.graphicalCached
	}
	return core.FindGraphicalFrames(m.Self())
}

// setGraphicalHint lets an owner that CAN see the surface (the MenuBar,
// which is parented to the desktop) tell an unparented popup menu which
// surface it lives on, before its first paint.
func (m *Menu) setGraphicalHint(graphical bool) {
	m.graphicalCached = graphical
	m.graphicalKnown = true
}

// inheritDisplayContext copies the opener's effective grid metrics and
// font onto this popup. Popup menus aren't parented into the trinket
// tree (see the note on the Menu struct), so their EffectiveCellMetrics
// and EffectiveFont would otherwise fall back to the built-in 8x16 / 12pt
// defaults and ignore the host's chosen font_size. The opener (a MenuBar
// or a parent Menu) is parented to the desktop and knows both, so it
// hands them down before the popup lays out - the same reason
// setGraphicalHint exists.
func (m *Menu) inheritDisplayContext(metrics core.CellMetrics, font *core.Font) {
	cm := metrics
	m.SetCellMetrics(&cm)
	m.SetFont(font)
}

// SetStrokeGap marks a horizontal span of one outer-stroke edge to omit
// so the border merges with the control that opened this menu (the edge
// nearest it). x/w are in the menu's coordinate space; bottom selects
// the bottom edge (a drop-up) instead of the top. Passing w <= 0 clears
// the gap (a full frame).
func (m *Menu) SetStrokeGap(x, w core.Unit, bottom bool) {
	m.strokeGapX = x
	m.strokeGapW = w
	m.strokeGapBottom = bottom
}

// paintPopupOuterStroke draws a 1-device-pixel frame just OUTSIDE bounds
// in style s (its background is the stroke color). gapW > 0 omits the
// span [gapX, gapX+gapW) from one horizontal edge - the bottom edge when
// gapBottom (a drop-up), otherwise the top (a drop-down) - so the border
// merges with the control that opened the popup. Graphical only (a no-op
// on cell surfaces, where FillRectPixels returns false).
func paintPopupOuterStroke(p *core.Painter, bounds core.UnitRect, scale int, s style.CellStyle, gapX, gapW core.Unit, gapBottom bool) {
	x, y, w, h := bounds.X, bounds.Y, bounds.Width, bounds.Height
	// Snap the spans to the grid the box fill paints on so the border
	// lands exactly on the fill's edges (no over/undershoot at any
	// font_size).
	hPx := p.UnitSpanPxY(y, y+h)

	// Left and right verticals span the full height plus both corners.
	p.FillRectPixels(x, y, -1, -1, 1, hPx+2, s)
	p.FillRectPixels(x+w, y, 0, -1, 1, hPx+2, s)

	// Horizontal edges between the verticals; the gapped one is split.
	drawEdge := func(edgeY core.Unit, offY int, gapped bool) {
		if !gapped || gapW <= 0 {
			p.FillRectPixels(x, edgeY, 0, offY, p.UnitSpanPxX(x, x+w), 1, s)
			return
		}
		gx, ge := gapX, gapX+gapW
		if gx < x {
			gx = x
		}
		if ge > x+w {
			ge = x + w
		}
		if gx > x {
			p.FillRectPixels(x, edgeY, 0, offY, p.UnitSpanPxX(x, gx), 1, s)
		}
		if ge < x+w {
			p.FillRectPixels(ge, edgeY, 0, offY, p.UnitSpanPxX(ge, x+w), 1, s)
		}
	}
	drawEdge(y, -1, !gapBottom) // top edge (gapped for drop-downs)
	drawEdge(y+h, 0, gapBottom) // bottom edge (gapped for drop-ups)
}

// paintScrollBumper draws a top/bottom scroll indicator row like a
// normal menu row - gutter background, gutter divider, and white content
// - with three indicator glyphs centered in the white content area
// only. glyph is '^'/'v' when that direction can scroll, else '-' for a
// blank bumper. No line-drawing characters.
func (m *Menu) paintScrollBumper(p *core.Painter, y core.Unit, size core.UnitSize, metrics core.CellMetrics, gutterStyle, contentStyle style.CellStyle, g bool, scale int, hairStyle style.CellStyle, glyph rune) {
	gutterWidth := metrics.CellWidth * 3
	p.FillRect(core.UnitRect{X: m.popupX, Y: y, Width: gutterWidth, Height: metrics.CellHeight}, ' ', gutterStyle)
	p.FillRect(core.UnitRect{X: m.popupX + gutterWidth, Y: y, Width: size.Width - gutterWidth, Height: metrics.CellHeight}, ' ', contentStyle)
	if g {
		p.FillRectPixels(m.popupX+gutterWidth, y, -1, 0, 1, p.UnitSpanPxY(y, y+metrics.CellHeight), hairStyle)
	}
	// Center the three glyphs in the white content area only.
	centerX := m.popupX + gutterWidth + (size.Width-gutterWidth)/2
	p.DrawCell(centerX-metrics.CellWidth*2, y, glyph, contentStyle)
	p.DrawCell(centerX, y, glyph, contentStyle)
	p.DrawCell(centerX+metrics.CellWidth*2, y, glyph, contentStyle)
}

// paintOuterStroke draws the menu's 1-pixel outer frame with the edge
// nearest its opening control gapped (see SetStrokeGap).
func (m *Menu) paintOuterStroke(p *core.Painter, size core.UnitSize, scale int, s style.CellStyle) {
	bounds := core.UnitRect{X: m.popupX, Y: m.popupY, Width: size.Width, Height: size.Height}
	paintPopupOuterStroke(p, bounds, scale, s, m.strokeGapX, m.strokeGapW, m.strokeGapBottom)
}

// rowHeightAt returns the vertical space item idx occupies. Separators
// collapse to a thin band on graphical surfaces; everything else (and
// all rows on cell surfaces) is a full text row.
func (m *Menu) rowHeightAt(idx int, graphical bool, cellHeight core.Unit) core.Unit {
	if graphical && idx >= 0 && idx < len(m.items) && m.items[idx].Separator {
		return separatorBandUnits
	}
	return cellHeight
}

// contentTopY returns the Y of the first item row (below the top scroll
// indicator, if any).
func (m *Menu) contentTopY() core.Unit {
	y := m.popupY
	if m.needsScrolling() {
		y += m.EffectiveCellMetrics().CellHeight
	}
	return y
}

// itemTopY returns the top Y of a visible item, walking the variable row
// heights of the items above it in the current scroll window.
func (m *Menu) itemTopY(itemIndex int) core.Unit {
	metrics := m.EffectiveCellMetrics()
	g := m.graphicalSurface()
	y := m.contentTopY()
	for i := m.scrollOffset; i < itemIndex && i < len(m.items); i++ {
		y += m.rowHeightAt(i, g, metrics.CellHeight)
	}
	return y
}

// hitRow maps a Y coordinate to a slot: kind is 0 none, 1 top scroll
// indicator, 2 bottom scroll indicator, 3 an item (itemIndex set).
// It honors the variable row heights of thin separators.
func (m *Menu) hitRow(y core.Unit) (kind, itemIndex int) {
	metrics := m.EffectiveCellMetrics()
	g := m.graphicalSurface()
	cur := m.popupY
	if m.needsScrolling() {
		if y >= cur && y < cur+metrics.CellHeight {
			return 1, -1
		}
		cur += metrics.CellHeight
	}
	visible := m.visibleItemCount()
	for i := 0; i < visible; i++ {
		idx := m.scrollOffset + i
		if idx >= len(m.items) {
			break
		}
		h := m.rowHeightAt(idx, g, metrics.CellHeight)
		if y >= cur && y < cur+h {
			return 3, idx
		}
		cur += h
	}
	if m.needsScrolling() && y >= cur && y < cur+metrics.CellHeight {
		return 2, -1
	}
	return 0, -1
}

// needsScrolling returns true if the menu has more items than maxVisible.
func (m *Menu) needsScrolling() bool {
	return m.maxVisible > 0 && len(m.items) > m.maxVisible
}

// visibleItemCount returns the number of items that can be shown at once.
func (m *Menu) visibleItemCount() int {
	if m.maxVisible <= 0 || len(m.items) <= m.maxVisible {
		return len(m.items)
	}
	return m.maxVisible
}

// canScrollUp returns true if there are items above the visible area.
func (m *Menu) canScrollUp() bool {
	return m.scrollOffset > 0
}

// canScrollDown returns true if there are items below the visible area.
func (m *Menu) canScrollDown() bool {
	return m.scrollOffset+m.visibleItemCount() < len(m.items)
}

// scrollUp scrolls the menu up by the given number of items.
func (m *Menu) scrollUp(count int) {
	m.scrollOffset -= count
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
	m.Update()
}

// scrollDown scrolls the menu down by the given number of items.
func (m *Menu) scrollDown(count int) {
	maxOffset := len(m.items) - m.visibleItemCount()
	m.scrollOffset += count
	if m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}
	m.Update()
}

// scrollPageUp scrolls up by one page.
func (m *Menu) scrollPageUp() {
	m.scrollUp(m.visibleItemCount())
}

// scrollPageDown scrolls down by one page.
func (m *Menu) scrollPageDown() {
	m.scrollDown(m.visibleItemCount())
}

// ensureVisible ensures the given item index is visible.
func (m *Menu) ensureVisible(index int) {
	if index < 0 || !m.needsScrolling() {
		return
	}

	// If item is above visible area, scroll up
	if index < m.scrollOffset {
		m.scrollOffset = index
	}

	// If item is below visible area, scroll down
	visibleEnd := m.scrollOffset + m.visibleItemCount() - 1
	if index > visibleEnd {
		m.scrollOffset = index - m.visibleItemCount() + 1
	}
}

// SizeHint returns the preferred size.
func (m *Menu) SizeHint() core.UnitSize {
	return m.calculateSize()
}

// DropdownBounds returns the bounds of the visible dropdown menu.
// Returns an empty rect if the menu is not visible.
func (m *Menu) DropdownBounds() core.UnitRect {
	if !m.visible {
		return core.UnitRect{}
	}
	size := m.calculateSize()
	return core.UnitRect{
		X:      m.popupX,
		Y:      m.popupY,
		Width:  size.Width,
		Height: size.Height,
	}
}

// Paint renders the menu.
func (m *Menu) Paint(p *core.Painter) {
	if !m.visible {
		return
	}

	// Record the surface kind for the layout/hit-test paths, which have
	// no painter of their own (see graphicalSurface). Set before
	// calculateSize so this paint's geometry already reflects it.
	m.graphicalCached = p.Graphical()
	m.graphicalKnown = true

	scheme := m.GetScheme()
	theme := m.Theme() // Still needed for DefaultBorder
	metrics := m.EffectiveCellMetrics()
	font := m.EffectiveFont()
	size := m.calculateSize()
	needsScroll := m.needsScrolling()

	// Draw menu background with border
	menuBounds := core.UnitRect{
		X:      m.popupX,
		Y:      m.popupY,
		Width:  size.Width,
		Height: size.Height,
	}
	menuItemStyle := scheme.GetMenuItemText()
	p.FillRect(menuBounds, ' ', menuItemStyle)
	// Cell surfaces get the box-drawing border; graphical surfaces get
	// the 1-pixel outer stroke drawn at the end instead (the char border
	// draws an inset line that would cut through the scroll bumpers).
	if !p.Graphical() {
		p.DrawRect(menuBounds, theme.DefaultBorder, menuItemStyle)
	}

	// Track Y offset for drawing
	currentY := m.popupY

	// Graphical surfaces get thin separator bands, a hairline separator,
	// and a 1-pixel gutter divider; cell surfaces keep the char idiom.
	g := m.graphicalSurface()
	scale := p.DeviceScale()
	// The hairlines (separator + gutter divider) are drawn in the menu
	// separator's foreground; FillRectPixels fills with the style's bg.
	hairColor := scheme.GetMenuSeparator().Fg
	hairStyle := style.DefaultStyle().WithBg(hairColor)

	// Draw top scroll indicator if needed
	if needsScroll {
		glyph := '-' // blank bumper (nothing above) shows a dash row
		if m.canScrollUp() {
			glyph = '^'
		}
		m.paintScrollBumper(p, currentY, size, metrics, scheme.GetMenuGutter(), menuItemStyle, g, scale, hairStyle, glyph)
		currentY += metrics.CellHeight
	}

	// Draw visible items
	visibleCount := m.visibleItemCount()
	for i := 0; i < visibleCount; i++ {
		itemIndex := m.scrollOffset + i
		if itemIndex >= len(m.items) {
			break
		}
		item := m.items[itemIndex]
		itemY := currentY

		// Determine style using scheme
		var gutterStyle, contentStyle style.CellStyle
		if item.Separator {
			gutterStyle = scheme.GetMenuSeparatorGutter()
			contentStyle = scheme.GetMenuSeparator()
		} else if !item.Enabled {
			gutterStyle = scheme.GetDisabledMenuGutter()
			contentStyle = scheme.GetDisabledMenuItem()
		} else if itemIndex == m.currentIndex {
			gutterStyle = scheme.GetFocusedMenuItemText()
			contentStyle = scheme.GetFocusedMenuItemText()
		} else {
			gutterStyle = scheme.GetMenuGutter()
			contentStyle = scheme.GetMenuItemText()
		}

		// Gutter area: 3 cells (border + checkmark + 1 space)
		gutterWidth := metrics.CellWidth * 3

		// Row height: separators collapse to a thin band on graphical.
		rowH := m.rowHeightAt(itemIndex, g, metrics.CellHeight)

		// Draw gutter background
		p.FillRect(core.UnitRect{
			X:      m.popupX,
			Y:      itemY,
			Width:  gutterWidth,
			Height: rowH,
		}, ' ', gutterStyle)

		// Draw content background
		p.FillRect(core.UnitRect{
			X:      m.popupX + gutterWidth,
			Y:      itemY,
			Width:  size.Width - gutterWidth,
			Height: rowH,
		}, ' ', contentStyle)

		// A 1-pixel divider down the right edge of the gutter, on every
		// row EXCEPT the focused one (its focus fill spans the gutter, so
		// the divider would clash / is overwritten).
		if g && itemIndex != m.currentIndex {
			p.FillRectPixels(m.popupX+gutterWidth, itemY, -1, 0, 1, p.UnitSpanPxY(itemY, itemY+rowH), hairStyle)
		}

		if item.Separator {
			if g {
				// A single hairline centered in the band, drawn only on
				// the white content area, inset 4 device px at each end.
				const marginPx = 4
				bandPx := p.UnitSpanPxY(itemY, itemY+rowH)
				offY := (bandPx - 1) / 2
				wPx := p.UnitSpanPxX(m.popupX+gutterWidth, m.popupX+size.Width) - 2*marginPx
				if wPx > 0 {
					p.FillRectPixels(m.popupX+gutterWidth, itemY, marginPx, offY, wPx, 1, hairStyle)
				}
			} else {
				// Cell surface: the dashed-row idiom, gutter + content.
				for x := m.popupX + metrics.CellWidth; x < m.popupX+size.Width-metrics.CellWidth; x += metrics.CellWidth {
					if x < m.popupX+gutterWidth {
						p.DrawCell(x, itemY, '─', gutterStyle)
					} else {
						p.DrawCell(x, itemY, '─', contentStyle)
					}
				}
			}
			currentY += rowH
			continue
		}

		x := m.popupX + metrics.CellWidth

		// Draw checkmark or icon in gutter area
		if item.Checkable {
			if item.Checked {
				p.DrawCell(x, itemY, '✓', gutterStyle)
			}
		} else if item.Icon != nil && len(item.Icon.Cells) > 0 {
			cell := item.Icon.Cells[0]
			p.DrawCell(x, itemY, cell.Char, cell.Style)
		}
		x += metrics.CellWidth * 2 // Move past checkmark + 1 gutter space

		// Draw a space in content area before text
		p.DrawCell(x, itemY, ' ', contentStyle)
		x += metrics.CellWidth

		// Now draw text with accelerator highlighting using font-aware rendering
		var accelStyle style.CellStyle
		if itemIndex == m.currentIndex {
			accelStyle = scheme.GetFocusedMenuAccelerator()
		} else {
			accelStyle = scheme.GetMenuAccelerator()
		}

		// Draw text in parts: before accel, accel char, after accel
		textRunes := []rune(item.Text)
		if item.Enabled && item.acceleratorPos >= 0 && item.acceleratorPos < len(textRunes) {
			var segs []textSegment
			if item.acceleratorPos > 0 {
				segs = append(segs, textSegment{string(textRunes[:item.acceleratorPos]), contentStyle})
			}
			segs = append(segs, textSegment{string(textRunes[item.acceleratorPos]), accelStyle})
			if item.acceleratorPos < len(textRunes)-1 {
				segs = append(segs, textSegment{string(textRunes[item.acceleratorPos+1:]), contentStyle})
			}
			x += drawTextSegments(p, x, itemY, font, segs...)
		} else {
			// No accelerator or disabled - draw entire text
			p.DrawText(x, itemY, item.Text, contentStyle, font)
			x += font.MeasureText(item.Text)
		}

		// Draw shortcut or submenu arrow at the right (in content area). The
		// menu width is unchanged; only the shortcut hugs closer to the right
		// edge on graphical surfaces (whose right border is a single pixel, not
		// a full char cell), trimming the empty space to its right.
		if item.SubMenu != nil {
			arrowX := m.popupX + size.Width - metrics.CellWidth*2
			p.DrawCell(arrowX, itemY, '▸', contentStyle)
		} else if item.Shortcut != "" {
			shortcutStr := item.Shortcut.DisplayString()
			rightPad := metrics.CellWidth * 2
			if p.Graphical() {
				rightPad = graphicalMenuTrailingUnits(metrics)
			}
			// Native mode renders the shortcut in Apple's UI face at 80%;
			// measure and draw with that same font so the right-alignment is
			// exact, and center the shorter line box within the item's row.
			sf := shortcutFont(font)
			shortcutWidth := sf.MeasureText(shortcutStr)
			shortcutX := m.popupX + size.Width - shortcutWidth - rightPad
			shortcutY := itemY
			if sf != font {
				if dy := (font.LineHeight() - sf.LineHeight()) / 2; dy > 0 {
					shortcutY += dy
				}
			}
			shortcutStyle := contentStyle
			if item.Enabled {
				shortcutStyle = contentStyle.WithAttrs(style.StyleDim)
			}
			p.DrawText(shortcutX, shortcutY, shortcutStr, shortcutStyle, sf)
		}

		currentY += rowH
	}

	// Draw bottom scroll indicator if needed
	if needsScroll {
		glyph := '-' // blank bumper (nothing below) shows a dash row
		if m.canScrollDown() {
			glyph = 'v'
		}
		m.paintScrollBumper(p, currentY, size, metrics, scheme.GetMenuGutter(), menuItemStyle, g, scale, hairStyle, glyph)
	}

	// A 1-pixel frame just outside the menu, in the separator color,
	// with the edge nearest the opening control gapped (graphical only).
	if g {
		m.paintOuterStroke(p, size, scale, hairStyle)
	}

	// Draw active submenu
	if m.activeSubMenu != nil {
		m.activeSubMenu.Paint(p)
	}
}

// HandleKeyPress handles keyboard input.
func (m *Menu) HandleKeyPress(event core.KeyPressEvent) bool {
	// Handle submenu first
	if m.activeSubMenu != nil {
		if m.activeSubMenu.HandleKeyPress(event) {
			return true
		}
	}

	switch event.Key {
	case "Up":
		m.currentIndex = m.findPrevEnabled(m.currentIndex)
		m.ensureVisible(m.currentIndex)
		m.closeSubMenu()
		m.announceCurrentItem()
		m.Update()
		return true

	case "Down":
		m.currentIndex = m.findNextEnabled(m.currentIndex)
		m.ensureVisible(m.currentIndex)
		m.closeSubMenu()
		m.announceCurrentItem()
		m.Update()
		return true

	case "Left":
		if m.parentMenu != nil {
			m.Hide()
			return true
		}
		return false // Let menu bar handle it

	case "Right":
		item := m.CurrentItem()
		if item != nil && item.SubMenu != nil {
			m.openSubMenu(item)
			return true
		}
		return false // Let menu bar handle it

	case "Enter", " ", "Space":
		item := m.CurrentItem()
		if item != nil {
			if item.SubMenu != nil {
				m.openSubMenu(item)
			} else {
				m.triggerItem(item)
			}
			return true
		}

	case "Escape":
		if m.parentMenu != nil {
			// Submenu - hide it and return to parent menu
			m.Hide()
			return true
		}
		// Top-level menu - let menu bar handle closing for proper cleanup
		// (MenuBar.CloseMenu will call Hide on us)
		return false

	case "Home":
		m.currentIndex = m.findNextEnabled(-1)
		m.scrollOffset = 0
		m.closeSubMenu()
		m.Update()
		return true

	case "End":
		m.currentIndex = m.findPrevEnabled(0)
		m.ensureVisible(m.currentIndex)
		m.closeSubMenu()
		m.Update()
		return true

	case "PageUp":
		m.scrollPageUp()
		// Move current index to top of visible area
		if m.currentIndex >= 0 {
			m.currentIndex = m.scrollOffset
			for m.currentIndex < len(m.items) && (m.items[m.currentIndex].Separator || !m.items[m.currentIndex].Enabled) {
				m.currentIndex++
			}
		}
		m.closeSubMenu()
		m.Update()
		return true

	case "PageDown":
		m.scrollPageDown()
		// Move current index to bottom of visible area
		if m.currentIndex >= 0 {
			m.currentIndex = m.scrollOffset + m.visibleItemCount() - 1
			if m.currentIndex >= len(m.items) {
				m.currentIndex = len(m.items) - 1
			}
			for m.currentIndex >= 0 && (m.items[m.currentIndex].Separator || !m.items[m.currentIndex].Enabled) {
				m.currentIndex--
			}
		}
		m.closeSubMenu()
		m.Update()
		return true
	}

	// Check for accelerator keys (single character, case insensitive, no modifiers)
	// These work when a menu is dropped down
	if len(event.Key) == 1 {
		letter := event.Key[0]
		// Match letters and digits without any modifier prefix
		if (letter >= 'a' && letter <= 'z') || (letter >= 'A' && letter <= 'Z') ||
			(letter >= '0' && letter <= '9') {
			key := rune(strings.ToLower(string(letter))[0])
			for i, item := range m.items {
				if !item.Separator && item.acceleratorChar == key {
					m.currentIndex = i
					if !item.Enabled {
						// Disabled items with matching accelerator: do nothing but consume the key
						m.Update()
						return true
					}
					if item.SubMenu != nil {
						m.openSubMenu(item)
					} else {
						m.triggerItem(item)
					}
					return true
				}
			}
		}
	}

	return false
}

// openSubMenu opens a submenu.
func (m *Menu) openSubMenu(item *MenuItem) {
	if item.SubMenu == nil {
		return
	}

	m.closeSubMenu()

	// The submenu shares this menu's surface kind, grid and font.
	item.SubMenu.setGraphicalHint(m.graphicalSurface())
	item.SubMenu.inheritDisplayContext(m.EffectiveCellMetrics(), m.EffectiveFont())

	size := m.calculateSize()

	// Position submenu to the right of current item
	itemIndex := -1
	for i, it := range m.items {
		if it == item {
			itemIndex = i
			break
		}
	}

	// Top of the item row, walking the variable row heights above it.
	subY := m.itemTopY(itemIndex)

	subX := m.popupX + size.Width

	m.activeSubMenu = item.SubMenu
	// Propagate the onItemPressed callback to submenu
	item.SubMenu.onItemPressed = m.onItemPressed
	// Propagate the accessibility manager to submenu
	item.SubMenu.accessibilityManager = m.accessibilityManager
	// Propagate the scroll-timer wiring so a tall submenu auto-scrolls too.
	item.SubMenu.scrollTimerStarter = m.scrollTimerStarter
	item.SubMenu.requestUpdate = m.requestUpdate
	// Calculate available height for submenu based on screen bottom
	if m.screenBottom > 0 {
		availableHeight := m.screenBottom - subY
		item.SubMenu.SetAvailableHeight(availableHeight)
		item.SubMenu.SetScreenBottom(m.screenBottom)
	}
	item.SubMenu.Show(subX, subY)
}

// closeSubMenu closes the active submenu.
func (m *Menu) closeSubMenu() {
	if m.activeSubMenu != nil {
		m.activeSubMenu.Hide()
		m.activeSubMenu = nil
	}
}

// triggerItem triggers a menu item and closes the menu.
func (m *Menu) triggerItem(item *MenuItem) {
	// InPlace items act without closing: run the action and re-render
	// the (possibly toggled) content where it stands.
	if item.InPlace {
		item.Trigger()
		m.Update()
		return
	}

	// Close all menus up to the menu bar
	menu := m
	for menu != nil {
		menu.Hide()
		menu = menu.parentMenu
	}

	// Notify menu bar to restore window focus before action executes
	if m.onWillTrigger != nil {
		m.onWillTrigger()
	}

	// Trigger the action
	item.Trigger()
}

// HandleMousePress handles mouse clicks.
func (m *Menu) HandleMousePress(event core.MousePressEvent) bool {
	if !m.visible {
		return false
	}

	// Check submenu first
	if m.activeSubMenu != nil && m.activeSubMenu.HandleMousePress(event) {
		return true
	}

	size := m.calculateSize()

	// Check if click is in menu bounds
	if event.X >= m.popupX && event.X < m.popupX+size.Width &&
		event.Y >= m.popupY && event.Y < m.popupY+size.Height {

		// Map the Y to a slot honoring variable-height separator rows.
		kind, itemIndex := m.hitRow(event.Y)

		// Check if clicking on scroll indicators
		scrollAmount := m.visibleItemCount() - 1
		if scrollAmount < 1 {
			scrollAmount = 1
		}
		if kind == 1 && m.canScrollUp() { // top indicator
			m.clickedMode = true
			m.scrollUp(scrollAmount)
			return true
		}
		if kind == 2 && m.canScrollDown() { // bottom indicator
			m.clickedMode = true
			m.scrollDown(scrollAmount)
			return true
		}

		if kind == 3 && itemIndex >= 0 && itemIndex < len(m.items) {
			item := m.items[itemIndex]
			if !item.Separator && item.Enabled {
				m.currentIndex = itemIndex
				if item.SubMenu != nil {
					m.openSubMenu(item)
				} else {
					// Signal MenuBar to enter drag mode so release will trigger
					if m.onItemPressed != nil {
						m.onItemPressed()
					}
					m.Update()
				}
			}
		}
		return true
	}

	// Click outside - close menu
	m.Hide()
	return false
}

// HandleMouseMove handles mouse movement for hover-scrolling and item highlighting.
func (m *Menu) HandleMouseMove(event core.MouseMoveEvent) bool {
	if !m.visible {
		m.scrollHoverZone = 0
		return false
	}

	size := m.calculateSize()

	// Check if mouse is in menu bounds
	if event.X < m.popupX || event.X >= m.popupX+size.Width ||
		event.Y < m.popupY || event.Y >= m.popupY+size.Height {
		if m.scrollHoverZone != 0 {
			m.scrollHoverZone = 0
			m.stopScrollTimer()
		}
		// Mouse outside menu - clear selection
		if m.currentIndex != -1 {
			m.currentIndex = -1
			m.Update()
		}
		return false
	}

	// Map the Y to a slot honoring variable-height separator rows.
	kind, itemIndex := m.hitRow(event.Y)

	// Handle scroll-indicator hover zones.
	if kind == 1 && m.canScrollUp() { // top indicator
		if m.scrollHoverZone != -1 {
			m.scrollHoverZone = -1
			m.scrollUp(1)
			m.startScrollTimer(-1)
		}
		return true
	}
	if kind == 2 && m.canScrollDown() { // bottom indicator
		if m.scrollHoverZone != 1 {
			m.scrollHoverZone = 1
			m.scrollDown(1)
			m.startScrollTimer(1)
		}
		return true
	}

	// Not on a scroll indicator - clear scroll state and stop timer.
	if m.scrollHoverZone != 0 {
		m.scrollHoverZone = 0
		m.stopScrollTimer()
	}

	// Highlight the hovered item.
	if kind == 3 && itemIndex >= 0 && itemIndex < len(m.items) {
		item := m.items[itemIndex]
		if !item.Separator && item.Enabled {
			m.currentIndex = itemIndex
			m.Update()
		}
	}

	return true
}

// HandleFocusOut is called when focus is lost.
func (m *Menu) HandleFocusOut() {
	// Only hide if focus didn't go to a submenu
	if m.activeSubMenu == nil || !m.activeSubMenu.HasFocus() {
		m.Hide()
	}
}

// AccessibleInfo returns accessibility information.
func (m *Menu) AccessibleInfo() core.AccessibleInfo {
	info := m.AccessibleTrinket.AccessibleInfo()
	info.Role = core.RoleMenu
	info.Name = m.title
	info.SetSize = len(m.items)

	if m.currentIndex >= 0 && m.currentIndex < len(m.items) {
		item := m.items[m.currentIndex]
		info.PositionInSet = m.currentIndex + 1
		info.Value = item.Text
	}

	return info
}

// MenuBar is a horizontal bar of menus.
type MenuBar struct {
	core.TrinketBase
	core.AccessibleTrinket

	menus          []*Menu
	currentIndex   int
	activeMenu     *Menu
	hoverIndex     int // Top-level item under the pointer (-1 = none)
	hoverScrollBtn int // Overflow scroll button under the pointer (-1 left, +1 right, 0 none)

	// modalBlocked reports whether this menu bar is disabled by a modal (the
	// app it represents is modally blocked). A blocked bar shows no hover
	// highlight on its items. nil means never blocked.
	modalBlocked func() bool

	// Appearance
	showShortcuts bool
	hideCalendar  bool // when true, omit the right-hand date/time area

	// graphicalCached records whether the last paint was on a pixel
	// surface; measurement (dateTimeWidth) has no painter and reads it.
	graphicalCached bool

	// Scroll state for overflow handling
	scrollOffset int // Index of first visible menu

	// Accelerator display state
	// Accelerators are shown when:
	// - Menu bar has focus and no menu is down, OR
	// - No keybinding conflict exists for the accelerator key
	acceleratorsActive bool // True when menu bar focused with no menu down

	// Drag tracking for click-and-drag menu navigation
	mouseDown  bool // Mouse button is held down
	dragging   bool // Actually dragging (mouse moved while down)
	mouseDownX core.Unit
	mouseDownY core.Unit

	// Callback when a menu is opened
	onMenuOpen func()

	// Callback when menu bar is dismissed without action (e.g., Escape)
	onMenuDismiss func()

	// Callback when Tab navigation should transfer to the dock
	onFocusDock func()

	// Fallback scroll-timer + update wiring for dropdowns, used when the
	// bar's parent doesn't provide them (a detached window's own menu bar,
	// whose parent is the Window rather than the Desktop). The desktop
	// wires these to its own timer system and the torn surface's repaint.
	scrollTimerStarter func(interval time.Duration, callback func()) interface{ Stop() }
	requestUpdate      func()
}

// SetModalBlockedChecker wires a predicate reporting whether this menu bar is
// disabled by a modal. A blocked bar suppresses item hover highlighting.
func (m *MenuBar) SetModalBlockedChecker(fn func() bool) {
	m.modalBlocked = fn
}

// isModalBlocked reports whether the menu bar is currently modal-blocked.
func (m *MenuBar) isModalBlocked() bool {
	return m.modalBlocked != nil && m.modalBlocked()
}

// SetScrollTimerStarter installs a fallback repeating-timer starter for
// this bar's dropdowns, used when the parent can't provide one.
func (m *MenuBar) SetScrollTimerStarter(fn func(interval time.Duration, callback func()) interface{ Stop() }) {
	m.scrollTimerStarter = fn
}

// SetRequestUpdate installs a fallback screen-update requester for this
// bar's dropdowns, used when the parent can't provide one.
func (m *MenuBar) SetRequestUpdate(fn func()) {
	m.requestUpdate = fn
}

// NewMenuBar creates a new menu bar.
func NewMenuBar() *MenuBar {
	m := &MenuBar{
		currentIndex:  -1,
		hoverIndex:    -1,
		showShortcuts: true,
	}
	m.TrinketBase = *core.NewTrinketBase()
	m.Init(m)
	m.SetFocusPolicy(core.StrongFocus)
	m.SetAccessibleRole(core.RoleMenuBar)
	return m
}

// SetOnMenuOpen sets a callback that is called when a menu is opened.
func (m *MenuBar) SetOnMenuOpen(callback func()) {
	m.onMenuOpen = callback
}

// SetOnMenuDismiss sets a callback that is called when the menu bar is dismissed without action.
func (m *MenuBar) SetOnMenuDismiss(callback func()) {
	m.onMenuDismiss = callback
}

// SetOnFocusDock sets a callback for when Tab navigation should transfer to the dock.
func (m *MenuBar) SetOnFocusDock(callback func()) {
	m.onFocusDock = callback
}

// calculateTotalMenusWidth returns the total width needed for all menus.
func (m *MenuBar) calculateTotalMenusWidth() core.Unit {
	total := core.Unit(0)
	for _, menu := range m.menus {
		total += m.menuTitleWidth(menu.title)
	}
	return total
}

// SetHideCalendar controls whether the right-hand date/time (calendar)
// area is shown. A detached window's own menu bar hides it.
func (m *MenuBar) SetHideCalendar(hide bool) {
	m.hideCalendar = hide
	m.Update()
}

// dateTimeFormat is the clock layout; it is always 18 characters wide,
// so a monospace measurement of the template matches any rendered time.
const dateTimeFormat = " Mon Jan 02 15:04 "

// dateTimeFont returns the face the clock renders in on graphical
// surfaces: a monospace family at 80% of the UI font size, which reads
// as a compact clock and frees horizontal space for the menus, and
// scales with font_size. Returns nil on text surfaces, where the clock
// stays one cell per character.
func (m *MenuBar) dateTimeFont() *core.Font {
	if !m.graphicalCached {
		return nil
	}
	base := core.FontMonday12.Size
	if ef := m.EffectiveFont(); ef != nil && ef.Size > 0 {
		base = ef.Size // the desktop's font_size
	}
	f := *core.FontMonday12    // monospace, deliberately not the UI face
	f.Size = (base*8 + 5) / 10 // ~80% of the UI font size, rounded
	return &f
}

// dateTimeWidth returns the width reserved for the date/time display,
// or zero when the calendar is hidden.
func (m *MenuBar) dateTimeWidth() core.Unit {
	if m.hideCalendar {
		return 0
	}
	if f := m.dateTimeFont(); f != nil {
		return f.MeasureText(dateTimeFormat)
	}
	metrics := m.EffectiveCellMetrics()
	// " Mon Jan 02 15:04 " = 18 chars
	return 18 * metrics.CellWidth
}

// scrollButtonWidth returns the width of each scroll button.
func (m *MenuBar) scrollButtonWidth() core.Unit {
	return m.EffectiveCellMetrics().TextWidth(3) // [<] or [>]
}

// menusNeedScrolling returns true if menus don't fit and need scroll buttons.
func (m *MenuBar) menusNeedScrolling() bool {
	bounds := m.Bounds()
	availableWidth := bounds.Width - m.dateTimeWidth()
	return m.calculateTotalMenusWidth() > availableWidth
}

// canScrollLeft returns true if there are menus to the left.
func (m *MenuBar) canScrollLeft() bool {
	return m.scrollOffset > 0
}

// canScrollRight returns true if there are more menus to show on the right.
func (m *MenuBar) canScrollRight() bool {
	if m.scrollOffset >= len(m.menus)-1 {
		return false
	}
	return !m.isLastMenuFullyVisible()
}

// isLastMenuFullyVisible returns true if the last menu is completely visible.
func (m *MenuBar) isLastMenuFullyVisible() bool {
	bounds := m.Bounds()

	scrollButtonsWidth := core.Unit(0)
	if m.menusNeedScrolling() {
		scrollButtonsWidth = m.scrollButtonWidth() * 2 // [<][>]
	}
	leftEllipseWidth := core.Unit(0)
	if m.scrollOffset > 0 {
		leftEllipseWidth = m.ellipsisWidth() // "..."
	}

	availableWidth := bounds.Width - m.dateTimeWidth() - scrollButtonsWidth

	x := leftEllipseWidth
	for i := m.scrollOffset; i < len(m.menus); i++ {
		menuWidth := m.menuTitleWidth(m.menus[i].title)
		x += menuWidth
		if x > availableWidth {
			return false
		}
	}
	return true
}

// ensureMenuVisible adjusts scroll offset to make the given menu index visible.
func (m *MenuBar) ensureMenuVisible(index int) {
	if index < 0 || index >= len(m.menus) || !m.menusNeedScrolling() {
		return
	}

	// If menu is to the left of visible area, scroll left
	if index < m.scrollOffset {
		m.scrollOffset = index
		return
	}

	// Check if menu is visible from current scroll position
	bounds := m.Bounds()

	scrollButtonsWidth := m.scrollButtonWidth() * 2
	leftEllipseWidth := core.Unit(0)
	if m.scrollOffset > 0 {
		leftEllipseWidth = m.ellipsisWidth() // "..."
	}

	availableWidth := bounds.Width - m.dateTimeWidth() - scrollButtonsWidth

	// Calculate position of the target menu
	x := leftEllipseWidth
	for i := m.scrollOffset; i <= index; i++ {
		menuWidth := m.menuTitleWidth(m.menus[i].title)
		if i == index {
			// Check if this menu fits
			if x+menuWidth > availableWidth {
				// Need to scroll right - increment scroll offset until it fits
				for m.scrollOffset < index {
					m.scrollOffset++
					// Recalculate with new scroll offset
					leftEllipseWidth = m.ellipsisWidth() // "..." (always present when scrolled)
					x = leftEllipseWidth
					for j := m.scrollOffset; j <= index; j++ {
						mw := m.menuTitleWidth(m.menus[j].title)
						if j == index && x+mw <= availableWidth {
							return
						}
						x += mw
					}
				}
			}
		}
		x += menuWidth
	}
}

// announceCurrentMenu announces the currently selected menu for accessibility.
func (m *MenuBar) announceCurrentMenu() {
	if m.currentIndex < 0 || m.currentIndex >= len(m.menus) {
		return
	}
	menu := m.menus[m.currentIndex]
	if am := core.FindAccessibilityManager(m); am != nil {
		// Arrowing across the menu bar is navigation: throttle the speech.
		am.AnnounceNavigation(menu.title + ", menu")
	}
}

// clampScrollOffset adjusts the scroll offset when the container is resized.
// It ensures we don't have unnecessary empty space on the right when we could
// show more menus, and resets to 0 when scrolling is no longer needed.
func (m *MenuBar) clampScrollOffset() {
	// If no menus or scrolling not needed, reset to 0
	if len(m.menus) == 0 || !m.menusNeedScrolling() {
		m.scrollOffset = 0
		return
	}

	// Calculate how much space we have for menus
	bounds := m.Bounds()
	scrollButtonsWidth := m.scrollButtonWidth() * 2
	availableWidth := bounds.Width - m.dateTimeWidth() - scrollButtonsWidth

	// Try to reduce scroll offset while still fitting all visible menus
	for m.scrollOffset > 0 {
		// Calculate width needed if we show one more menu on the left
		testOffset := m.scrollOffset - 1
		leftEllipseWidth := core.Unit(0)
		if testOffset > 0 {
			leftEllipseWidth = m.ellipsisWidth() // "..."
		}

		x := leftEllipseWidth
		fitsWithMoreMenus := true
		for i := testOffset; i < len(m.menus); i++ {
			menuWidth := m.menuTitleWidth(m.menus[i].title)
			// Reserve space for right ellipsis if not the last menu
			rightEllipsisWidth := core.Unit(0)
			if i < len(m.menus)-1 {
				rightEllipsisWidth = m.ellipsisWidth()
			}
			if x+menuWidth+rightEllipsisWidth > availableWidth {
				fitsWithMoreMenus = false
				break
			}
			x += menuWidth
		}

		if fitsWithMoreMenus {
			m.scrollOffset = testOffset
		} else {
			break
		}
	}
}

// hasAcceleratorConflict checks if a menu accelerator key conflicts with any
// registered keybinding (e.g., Alt+key is used for something else).
func (m *MenuBar) hasAcceleratorConflict(accel rune) bool {
	if accel == 0 {
		return false
	}
	// Check if M-<letter> is bound to any action
	key := "M-" + string(accel)
	action := core.DefaultKeyBindings.FindAction(key)
	return action != ""
}

// ShouldShowAccelerator returns whether the accelerator for a menu should be
// highlighted in red. Returns true if:
// - The menu bar has focus and no menu is dropped down, OR
// - There is no keybinding conflict for this accelerator
func (m *MenuBar) ShouldShowAccelerator(menu *Menu) bool {
	if menu.acceleratorChar == 0 {
		return false
	}
	// Always show when menu bar is focused with no menu down
	if m.acceleratorsActive {
		return true
	}
	// Otherwise, only show if there's no keybinding conflict
	return !m.hasAcceleratorConflict(menu.acceleratorChar)
}

// AcceleratorsActive returns whether accelerator highlighting is currently active.
func (m *MenuBar) AcceleratorsActive() bool {
	return m.acceleratorsActive
}

// setAcceleratorsActive updates the accelerators active state.
func (m *MenuBar) setAcceleratorsActive(active bool) {
	if m.acceleratorsActive != active {
		m.acceleratorsActive = active
		m.Update()
	}
}

// AddMenu adds a menu to the bar.
func (m *MenuBar) AddMenu(menu *Menu) {
	m.menus = append(m.menus, menu)
	m.Update()
}

// InsertMenu inserts a menu at the given index.
func (m *MenuBar) InsertMenu(index int, menu *Menu) {
	if index < 0 {
		index = 0
	}
	if index > len(m.menus) {
		index = len(m.menus)
	}
	m.menus = append(m.menus[:index], append([]*Menu{menu}, m.menus[index:]...)...)
	m.Update()
}

// RemoveMenu removes a menu.
func (m *MenuBar) RemoveMenu(menu *Menu) {
	for i, mm := range m.menus {
		if mm == menu {
			m.menus = append(m.menus[:i], m.menus[i+1:]...)
			break
		}
	}
	m.Update()
}

// Clear removes all menus.
func (m *MenuBar) Clear() {
	m.menus = nil
	m.currentIndex = -1
	m.activeMenu = nil
	m.Update()
}

// Menus returns all menus.
func (m *MenuBar) Menus() []*Menu {
	return m.menus
}

// MenuAt returns the menu at the given index.
func (m *MenuBar) MenuAt(index int) *Menu {
	if index < 0 || index >= len(m.menus) {
		return nil
	}
	return m.menus[index]
}

// ActiveMenu returns the currently open menu.
func (m *MenuBar) ActiveMenu() *Menu {
	return m.activeMenu
}

// IsMenuOpen reports whether a dropdown is currently open. A detached
// window hosting this bar routes all mouse input here while it is.
func (m *MenuBar) IsMenuOpen() bool {
	return m.activeMenu != nil
}

// HandleShortcut checks the bar's menus (recursively) for an item whose
// accelerator matches the event and triggers it, returning true on a
// match. This lets a detached window's own menu bar service its app
// shortcuts (Cut/Copy/Paste, etc.) the same way the desktop bar does
// while docked, even when no dropdown is open.
func (m *MenuBar) HandleShortcut(event core.KeyPressEvent) bool {
	for _, menu := range m.menus {
		if menuShortcutMatch(menu, event) {
			return true
		}
	}
	return false
}

// menuShortcutMatch recursively looks for an enabled item in menu whose
// shortcut matches event, triggering the first hit.
func menuShortcutMatch(menu *Menu, event core.KeyPressEvent) bool {
	if menu == nil {
		return false
	}
	for _, item := range menu.Items() {
		if item == nil || item.Separator || !item.Enabled {
			continue
		}
		if item.Shortcut != "" && item.Shortcut.Matches(event) {
			item.Trigger()
			return true
		}
		if item.SubMenu != nil && menuShortcutMatch(item.SubMenu, event) {
			return true
		}
	}
	return false
}

// OpenMenu opens a menu by index.
func (m *MenuBar) OpenMenu(index int) {
	if index < 0 || index >= len(m.menus) {
		return
	}

	m.CloseMenu()
	m.currentIndex = index
	m.activeMenu = m.menus[index]
	// The MenuBar is parented to the desktop, so it can see the surface
	// kind and the host's grid/font; hand them to the (unparented)
	// dropdown before it lays out.
	m.activeMenu.setGraphicalHint(core.FindGraphicalFrames(m.Self()))
	m.activeMenu.inheritDisplayContext(m.EffectiveCellMetrics(), m.EffectiveFont())
	m.acceleratorsActive = false // Disable bar accelerators when menu is down

	// Set up callback so when user presses on a menu item, we enter drag mode
	// This allows click-to-open then drag-to-select behavior
	m.activeMenu.onItemPressed = func() {
		m.mouseDown = true
		m.dragging = true
	}

	// Set up callback to restore window focus before menu action executes
	m.activeMenu.setOnWillTrigger(func() {
		// Clean up menu bar state
		m.activeMenu = nil
		m.currentIndex = -1
		m.acceleratorsActive = false
		m.ClearFocus()
		// Restore previous window focus
		if m.onMenuDismiss != nil {
			m.onMenuDismiss()
		}
	})

	// Ensure the menu is visible before opening (scroll if needed)
	m.ensureMenuVisible(index)

	// Notify that a menu is opening
	if m.onMenuOpen != nil {
		m.onMenuOpen()
	}

	// Calculate position (after scrolling so position is correct)
	metrics := m.EffectiveCellMetrics()
	itemX := m.calculateMenuX(index)
	itemWidth := m.menuTitleWidth(m.menus[index].title)
	y := metrics.CellHeight

	// Horizontal placement (popupX is in the menu bar's local space, where
	// 0 is the surface's left edge and the bar spans its full width):
	//   - Normally the dropdown is left-aligned to its menu-bar item.
	//   - If a left-aligned dropdown would run past the surface's right
	//     edge, right-align it so its right edge meets the item's right
	//     edge instead.
	//   - If even right-aligned it would fall off the left edge (a very
	//     narrow surface), pin its left edge to the surface's left edge.
	x := itemX
	dropWidth := m.activeMenu.calculateSize().Width
	surfaceWidth := m.Bounds().Width
	if itemX+dropWidth > surfaceWidth {
		x = itemX + itemWidth - dropWidth
		if x < 0 {
			x = 0
		}
	}

	// Calculate available height from desktop client area and set up timer
	if parent := m.Parent(); parent != nil {
		if desktop, ok := parent.(interface{ ClientArea() core.UnitRect }); ok {
			clientArea := desktop.ClientArea()
			screenBottom := clientArea.Y + clientArea.Height
			// Available height is from menu bar bottom to bottom of client area
			availableHeight := screenBottom - y
			m.activeMenu.SetAvailableHeight(availableHeight)
			m.activeMenu.SetScreenBottom(screenBottom)
		}
		// Set up scroll timer starter and update requester if desktop supports them
		if timerProvider, ok := parent.(interface {
			StartRepeatingTimer(interval time.Duration, callback func()) *DesktopTimer
			RequestUpdate()
		}); ok {
			m.activeMenu.SetScrollTimerStarter(func(interval time.Duration, callback func()) interface{ Stop() } {
				return timerProvider.StartRepeatingTimer(interval, callback)
			})
			m.activeMenu.SetRequestUpdate(timerProvider.RequestUpdate)
		} else if m.scrollTimerStarter != nil {
			// Parent can't provide timers (a detached window's menu bar):
			// fall back to the wiring the desktop set on the bar itself.
			m.activeMenu.SetScrollTimerStarter(m.scrollTimerStarter)
			m.activeMenu.SetRequestUpdate(m.requestUpdate)
		}
	}

	// Set up accessibility manager for menu item announcements
	if am := core.FindAccessibilityManager(m); am != nil {
		m.activeMenu.SetAccessibilityManager(am)
	}

	m.activeMenu.Show(x, y)

	// Gap the dropdown's top stroke across the parent menu-bar item, so
	// the border merges into the bar rather than underlining the item.
	// The gap tracks the item (itemX), not the possibly right-aligned
	// dropdown; paintPopupOuterStroke clamps it to the dropdown's span.
	m.activeMenu.SetStrokeGap(itemX, itemWidth, false)

	// Announce the menu for accessibility
	m.announceCurrentMenu()

	m.Update()
}

// CloseMenu closes the active menu but keeps the menu bar focused.
func (m *MenuBar) CloseMenu() {
	wasOpen := m.activeMenu != nil
	if m.activeMenu != nil {
		m.activeMenu.Hide()
		m.activeMenu = nil
	}
	// Re-enable accelerators if focused (menu bar retains focus while menu is open)
	if m.HasFocus() {
		m.acceleratorsActive = true
		// Keep currentIndex if we just closed a menu (for continued navigation)
		if !wasOpen {
			m.currentIndex = -1
		}
	} else {
		m.currentIndex = -1
	}
	m.Update()
}

// CloseMenuAndUnfocus closes the active menu and unfocuses the menu bar.
// This also calls onMenuDismiss which may restore the previous active window.
func (m *MenuBar) CloseMenuAndUnfocus() {
	if m.activeMenu != nil {
		m.activeMenu.Hide()
		m.activeMenu = nil
	}
	m.currentIndex = -1
	m.acceleratorsActive = false
	m.ClearFocus()
	m.Update()

	// Notify that the menu bar was dismissed
	if m.onMenuDismiss != nil {
		m.onMenuDismiss()
	}
}

// CloseMenuWithoutRestore closes the active menu and unfocuses the menu bar
// WITHOUT calling onMenuDismiss. This is used when a menu action was triggered
// that may have created a new window - we don't want to restore the old window.
// Also used by DeactivateMenuBar when a new window becomes active.
func (m *MenuBar) CloseMenuWithoutRestore() {
	if m.activeMenu != nil {
		m.activeMenu.Hide()
		m.activeMenu = nil
	}
	m.currentIndex = -1
	m.acceleratorsActive = false
	m.ClearFocus()
	m.Update()
	// Note: intentionally not calling onMenuDismiss
}

// calculateMenuX calculates the x position of a menu (accounting for scroll offset).
// menuBarLeftInset is the small left indent applied to the menu items on
// graphical surfaces, so the outline stroke drawn around the active item
// has its left edge clear of the very left pixel column. Clicks anywhere
// in this indent still activate the first item (Fitts's law - see
// HandleMousePress), so nothing on the left edge is dead. Zero on cell
// surfaces, where there is no stroke and a sub-cell indent can't render.
const menuBarLeftInset core.Unit = 2

// leftInset returns the item indent for the surface of the last paint:
// menuBarLeftInset on graphical surfaces, 0 on cell surfaces.
func (m *MenuBar) leftInset() core.Unit {
	if m.graphicalCached {
		return menuBarLeftInset
	}
	return 0
}

func (m *MenuBar) calculateMenuX(index int) core.Unit {

	// Start past the left indent, and the left ellipsis if scrolled.
	x := m.leftInset()
	if m.scrollOffset > 0 {
		x += m.ellipsisWidth() // "..."
	}

	// Calculate position from scroll offset using font-aware width
	for i := m.scrollOffset; i < index; i++ {
		x += m.menuTitleWidth(m.menus[i].title)
	}
	return x
}

// SizeHint returns the preferred size.
func (m *MenuBar) SizeHint() core.UnitSize {
	metrics := m.EffectiveCellMetrics()
	font := m.EffectiveFont()

	width := core.Unit(0)
	for _, menu := range m.menus {
		// Menu width: space (1 cell) + title (font) + space (1 cell)
		width += metrics.CellWidth*2 + font.MeasureText(menu.title)
	}

	return core.UnitSize{
		Width:  width,
		Height: metrics.CellHeight,
	}
}

// menuTitleWidth returns the width of a menu title including surrounding spaces.
func (m *MenuBar) menuTitleWidth(title string) core.Unit {
	metrics := m.EffectiveCellMetrics()
	font := m.EffectiveFont()
	// Menu width: space (1 cell) + title (font) + space (1 cell)
	return metrics.CellWidth*2 + font.MeasureText(title)
}

// ellipsisText is the overflow marker (three periods, not the unicode
// glyph), and ellipsisWidth its width in the menu bar's proportional
// font - so it measures and renders the same as the menu titles.
const ellipsisText = "..."

func (m *MenuBar) ellipsisWidth() core.Unit {
	return m.EffectiveFont().MeasureText(ellipsisText)
}

// drawEllipsis paints the overflow marker in the menu bar's proportional
// font at (x, 0) and returns its width.
func (m *MenuBar) drawEllipsis(p *core.Painter, x core.Unit, s style.CellStyle) core.Unit {
	font := m.EffectiveFont()
	p.DrawText(x, 0, ellipsisText, s, font)
	return font.MeasureText(ellipsisText)
}

// Paint renders the menu bar (without dropdown - use PaintDropdown for that).
func (m *MenuBar) Paint(p *core.Painter) {
	bounds := m.Bounds()
	scheme := m.GetScheme()
	metrics := m.EffectiveCellMetrics()
	font := m.EffectiveFont()

	// Remember the surface kind for measurement paths (dateTimeWidth has
	// no painter of its own).
	m.graphicalCached = p.Graphical()

	// A modally-blocked bar is disabled: drop any hover highlight even if the
	// modal appeared without an intervening mouse move to clear it.
	if m.isModalBlocked() {
		m.hoverIndex = -1
		m.hoverScrollBtn = 0
	}

	// Clamp scroll offset if container was resized and more menus can now fit
	m.clampScrollOffset()

	menuBarStyle := scheme.GetMenuBar()

	// Draw background
	p.FillRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, ' ', menuBarStyle)

	// Calculate if we need scroll buttons
	needsScrolling := m.menusNeedScrolling()

	// Draw date/time on the far right edge first (to know where menus must
	// stop). When the calendar is hidden it reserves no width, so menus and
	// the overflow ellipsis run to the full right edge.
	now := time.Now()
	dateTimeStr := now.Format(dateTimeFormat)
	dateTimeStyle := scheme.GetMenuBarInfo()
	dateTimeWidth := m.dateTimeWidth()
	dateTimeX := bounds.Width - dateTimeWidth

	// Draw scroll buttons just left of date/time if needed
	scrollButtonsWidth := core.Unit(0)
	if needsScrolling {
		scrollButtonsWidth = m.scrollButtonWidth() * 2 // [<][>] or  <  >

		// Button styles: active vs disabled scroll buttons
		activeButtonStyle := scheme.GetMenuBarButton()
		inactiveButtonStyle := scheme.GetDisabledMenuBarButton()

		// Draw left button: [<] when active, " < " when inactive
		leftButtonX := dateTimeX - scrollButtonsWidth
		if m.canScrollLeft() {
			leftStyle := activeButtonStyle
			if m.hoverScrollBtn == -1 {
				leftStyle = scheme.GetHoveredMenuBarButton()
			}
			p.DrawCell(leftButtonX, 0, '[', leftStyle)
			p.DrawCell(leftButtonX+metrics.CellWidth, 0, '<', leftStyle)
			p.DrawCell(leftButtonX+2*metrics.CellWidth, 0, ']', leftStyle)
		} else {
			p.DrawCell(leftButtonX, 0, ' ', inactiveButtonStyle)
			p.DrawCell(leftButtonX+metrics.CellWidth, 0, '<', inactiveButtonStyle)
			p.DrawCell(leftButtonX+2*metrics.CellWidth, 0, ' ', inactiveButtonStyle)
		}

		// Draw right button: [>] when active, " > " when inactive
		rightButtonX := leftButtonX + 3*metrics.CellWidth
		if m.canScrollRight() {
			rightStyle := activeButtonStyle
			if m.hoverScrollBtn == 1 {
				rightStyle = scheme.GetHoveredMenuBarButton()
			}
			p.DrawCell(rightButtonX, 0, '[', rightStyle)
			p.DrawCell(rightButtonX+metrics.CellWidth, 0, '>', rightStyle)
			p.DrawCell(rightButtonX+2*metrics.CellWidth, 0, ']', rightStyle)
		} else {
			p.DrawCell(rightButtonX, 0, ' ', inactiveButtonStyle)
			p.DrawCell(rightButtonX+metrics.CellWidth, 0, '>', inactiveButtonStyle)
			p.DrawCell(rightButtonX+2*metrics.CellWidth, 0, ' ', inactiveButtonStyle)
		}
	}

	// Available width for menus
	availableWidth := dateTimeX - scrollButtonsWidth

	// Items begin past a small left indent on graphical surfaces (so the
	// active item's outline stroke clears the left edge); the left
	// ellipsis, when scrolled, sits in that same indented origin.
	x := m.leftInset()
	if m.scrollOffset > 0 {
		x += m.drawEllipsis(p, x, menuBarStyle)
	}

	// Draw visible menus
	for i := m.scrollOffset; i < len(m.menus); i++ {
		menu := m.menus[i]
		menuWidth := m.menuTitleWidth(menu.title)

		// Reserve space for right ellipsis if there are more menus after this one
		rightEllipsisWidth := core.Unit(0)
		if i < len(m.menus)-1 {
			rightEllipsisWidth = m.ellipsisWidth() // "..."
		}

		// Check if this menu fits (with room for right ellipsis if needed)
		if x+menuWidth+rightEllipsisWidth > availableWidth {
			// Menu doesn't fit fully
			remainingWidth := availableWidth - x

			// Determine style for this menu. Selection (focus/active)
			// takes priority over hover.
			var s style.CellStyle
			var accelStyle style.CellStyle
			isSelected := i == m.currentIndex
			if isSelected {
				// Use Active style when dropdown is open with item selected,
				// Focused style when dropdown not open or has no selection
				if m.activeMenu != nil && m.activeMenu.currentIndex != -1 {
					s = scheme.GetActiveMenuBarItem()
					accelStyle = scheme.GetActiveMenuBarMeta()
				} else {
					s = scheme.GetFocusedMenuBarItem()
					accelStyle = scheme.GetFocusedMenuBarMeta()
				}
			} else if i == m.hoverIndex {
				s = scheme.GetHoveredMenuBar()
				accelStyle = scheme.GetHoveredMenuBarMeta()
			} else {
				s = menuBarStyle
				accelStyle = scheme.GetMenuBarMeta()
			}
			showAccel := m.ShouldShowAccelerator(menu)

			// If this is the selected menu, try to show the full menu text
			// with ellipsis OUTSIDE the selected area
			if isSelected && remainingWidth >= menuWidth {
				// We can fit the full menu, just not the ellipsis after it
				// Draw the full menu in selected style
				p.FillRect(core.UnitRect{
					X:      x,
					Y:      0,
					Width:  menuWidth,
					Height: metrics.CellHeight,
				}, ' ', s)

				// Draw title with accelerator highlighting using font-aware rendering
				textX := x + metrics.CellWidth
				titleRunes := []rune(menu.title)
				if showAccel && menu.acceleratorPos >= 0 && menu.acceleratorPos < len(titleRunes) {
					var segs []textSegment
					if menu.acceleratorPos > 0 {
						segs = append(segs, textSegment{string(titleRunes[:menu.acceleratorPos]), s})
					}
					segs = append(segs, textSegment{string(titleRunes[menu.acceleratorPos]), accelStyle})
					if menu.acceleratorPos < len(titleRunes)-1 {
						segs = append(segs, textSegment{string(titleRunes[menu.acceleratorPos+1:]), s})
					}
					drawTextSegments(p, textX, 0, font, segs...)
				} else {
					p.DrawText(textX, 0, menu.title, s, font)
				}

				// Draw the ellipsis after the menu (in normal style); the
				// painter clips it to the bar's bounds.
				m.drawEllipsis(p, x+menuWidth, menuBarStyle)
			} else {
				// Not selected, or not enough room for full menu - show partial with ellipsis
				ellipsisWidth := m.ellipsisWidth() // "..."

				// Calculate how many chars we can show: space + chars + "..."
				// Need at least 4 chars width for " X..." (space, one char, ellipsis)
				if remainingWidth >= 4*metrics.CellWidth {
					// Draw space before text
					p.DrawCell(x, 0, ' ', s)
					textX := x + metrics.CellWidth

					// Calculate how many title chars we can show
					charsAvailable := int((remainingWidth - metrics.CellWidth - ellipsisWidth) / metrics.CellWidth)
					titleRunes := []rune(menu.title)
					for idx := 0; idx < charsAvailable && idx < len(titleRunes); idx++ {
						charStyle := s
						if showAccel && idx == menu.acceleratorPos {
							charStyle = accelStyle
						}
						p.DrawCell(textX, 0, titleRunes[idx], charStyle)
						textX += metrics.CellWidth
					}
					// Draw ellipsis in the menu style (never accelerator color)
					m.drawEllipsis(p, textX, s)
				} else if remainingWidth >= ellipsisWidth {
					// Just show "..." to indicate more menus
					m.drawEllipsis(p, x, menuBarStyle)
				}
			}
			break
		}

		// Determine style. Selection (focus/active) takes priority over
		// hover.
		var s style.CellStyle
		var accelStyle style.CellStyle
		isSelected := i == m.currentIndex
		if isSelected {
			// Use Active style when dropdown is open with item selected,
			// Focused style when dropdown not open or has no selection
			if m.activeMenu != nil && m.activeMenu.currentIndex != -1 {
				s = scheme.GetActiveMenuBarItem()
				accelStyle = scheme.GetActiveMenuBarMeta()
			} else {
				s = scheme.GetFocusedMenuBarItem()
				accelStyle = scheme.GetFocusedMenuBarMeta()
			}
		} else if i == m.hoverIndex {
			s = scheme.GetHoveredMenuBar()
			accelStyle = scheme.GetHoveredMenuBarMeta()
		} else {
			s = menuBarStyle
			accelStyle = scheme.GetMenuBarMeta()
		}

		// Draw background
		p.FillRect(core.UnitRect{
			X:      x,
			Y:      0,
			Width:  menuWidth,
			Height: metrics.CellHeight,
		}, ' ', s)

		// Draw title with accelerator highlighting using font-aware rendering
		textX := x + metrics.CellWidth // Start after leading space
		showAccel := m.ShouldShowAccelerator(menu)

		// Draw text in parts: before accel, accel char, after accel
		titleRunes := []rune(menu.title)
		if showAccel && menu.acceleratorPos >= 0 && menu.acceleratorPos < len(titleRunes) {
			var segs []textSegment
			if menu.acceleratorPos > 0 {
				segs = append(segs, textSegment{string(titleRunes[:menu.acceleratorPos]), s})
			}
			segs = append(segs, textSegment{string(titleRunes[menu.acceleratorPos]), accelStyle})
			if menu.acceleratorPos < len(titleRunes)-1 {
				segs = append(segs, textSegment{string(titleRunes[menu.acceleratorPos+1:]), s})
			}
			drawTextSegments(p, textX, 0, font, segs...)
		} else {
			// No accelerator - draw entire text
			p.DrawText(textX, 0, menu.title, s, font)
		}

		x += menuWidth
	}

	// Draw date/time background and text (unless the calendar is hidden).
	// The background always fills the full bar height; on graphical
	// surfaces the clock text renders in a compact 80% monospace face
	// (vertically centered), while text mode keeps one cell per char.
	if !m.hideCalendar {
		p.FillRect(core.UnitRect{
			X:      dateTimeX,
			Y:      0,
			Width:  dateTimeWidth,
			Height: metrics.CellHeight,
		}, ' ', dateTimeStyle)

		if f := m.dateTimeFont(); f != nil {
			y := (metrics.CellHeight - f.LineHeight()) / 2
			if y < 0 {
				y = 0
			}
			p.DrawText(dateTimeX, y, dateTimeStr, dateTimeStyle, f)
		} else {
			for i, ch := range dateTimeStr {
				p.DrawCell(dateTimeX+core.Unit(i)*metrics.CellWidth, 0, ch, dateTimeStyle)
			}
		}
	}

	// When a menu is popped down, frame its parent bar item with the same
	// 1-pixel separator-color stroke, so item + dropdown read as one
	// outline. Drawn before the dropdown (which paints later), so the
	// dropdown covers the bottom edge; the top edge falls above the
	// canvas. Graphical only.
	if p.Graphical() && m.activeMenu != nil && m.activeMenu.visible &&
		m.currentIndex >= 0 && m.currentIndex < len(m.menus) {
		itemRect := core.UnitRect{
			X:      m.calculateMenuX(m.currentIndex),
			Y:      0,
			Width:  m.menuTitleWidth(m.menus[m.currentIndex].title),
			Height: metrics.CellHeight,
		}
		lineStyle := style.DefaultStyle().WithBg(scheme.GetMenuSeparator().Fg)
		paintPopupOuterStroke(p, itemRect, p.DeviceScale(), lineStyle, 0, 0, false)
	}
}

// PaintDropdown renders the active menu dropdown (call after windows for correct z-order).
func (m *MenuBar) PaintDropdown(p *core.Painter) {
	if m.activeMenu != nil {
		m.activeMenu.Paint(p)
	}
}

// ActiveMenuBounds returns the bounds of the active dropdown menu.
// Returns an empty rect if no menu is open.
func (m *MenuBar) ActiveMenuBounds() core.UnitRect {
	if m.activeMenu == nil {
		return core.UnitRect{}
	}
	return m.activeMenu.DropdownBounds()
}

// HandleKeyPress handles keyboard input.
func (m *MenuBar) HandleKeyPress(event core.KeyPressEvent) bool {
	// Handle active menu first
	if m.activeMenu != nil {
		if m.activeMenu.HandleKeyPress(event) {
			// If the menu was hidden (item triggered), clean up without restoring previous window
			// Note: activeMenu may have been set to nil by DeactivateMenuBar if the action
			// created a new window, so check for nil first
			if m.activeMenu != nil && !m.activeMenu.IsVisible() {
				m.CloseMenuWithoutRestore()
			}
			return true
		}
	}

	switch event.Key {
	case "Left":
		if len(m.menus) > 0 {
			newIndex := m.currentIndex - 1
			if newIndex < 0 {
				newIndex = len(m.menus) - 1
			}
			if m.activeMenu != nil {
				m.OpenMenu(newIndex)
			} else {
				m.currentIndex = newIndex
				m.ensureMenuVisible(newIndex)
				m.announceCurrentMenu()
				m.Update()
			}
		}
		return true

	case "Right":
		if len(m.menus) > 0 {
			newIndex := m.currentIndex + 1
			if newIndex >= len(m.menus) {
				newIndex = 0
			}
			if m.activeMenu != nil {
				m.OpenMenu(newIndex)
			} else {
				m.currentIndex = newIndex
				m.ensureMenuVisible(newIndex)
				m.announceCurrentMenu()
				m.Update()
			}
		}
		return true

	case "Enter", " ", "Space", "Down":
		if m.currentIndex >= 0 {
			if m.activeMenu != nil {
				m.CloseMenu()
			} else {
				m.OpenMenu(m.currentIndex)
				// Opening from the keyboard lands focus on the first valid
				// option (mouse-opening leaves nothing selected until hover).
				if m.activeMenu != nil {
					m.activeMenu.SelectFirstItem()
				}
			}
		}
		return true

	case "Escape":
		if m.activeMenu != nil {
			// First escape: close menu but keep menu bar focused
			m.CloseMenu()
		} else {
			// Second escape: unfocus menu bar
			m.CloseMenuAndUnfocus()
		}
		return true

	case "F10":
		// Toggle menu bar focus
		if m.HasFocus() {
			m.CloseMenuAndUnfocus()
		} else {
			m.SetFocus()
			if m.currentIndex < 0 && len(m.menus) > 0 {
				m.currentIndex = 0
			}
		}
		m.Update()
		return true

	case "Tab":
		// Tab/Shift+Tab: transfer focus to dock (if available and no menu is open)
		if m.activeMenu == nil && m.onFocusDock != nil {
			m.onFocusDock()
			return true
		}
	}

	// Check Alt+key shortcuts (M-<letter> format, lowercase only - no shift)
	if strings.HasPrefix(event.Key, "M-") && len(event.Key) == 3 {
		letter := event.Key[2]
		// Only match lowercase (M-f not M-F) to avoid shift combinations
		if letter >= 'a' && letter <= 'z' {
			key := rune(letter)
			for i, menu := range m.menus {
				if menu.acceleratorChar == key {
					m.SetFocus()
					m.OpenMenu(i)
					return true
				}
			}
		}
	}

	// Check accessibility keys: when menu bar is focused with accelerators active,
	// single letter keys (no modifiers) activate menus
	if m.HasFocus() && m.activeMenu == nil && m.acceleratorsActive && len(event.Key) == 1 {
		letter := event.Key[0]
		// Accept both uppercase and lowercase single letters (no modifier prefix)
		if (letter >= 'a' && letter <= 'z') || (letter >= 'A' && letter <= 'Z') {
			key := rune(strings.ToLower(event.Key)[0])
			for i, menu := range m.menus {
				if menu.acceleratorChar == key {
					m.OpenMenu(i)
					return true
				}
			}
		}
	}

	return false
}

// findMenuByAccelerator finds a menu by its accelerator character.
func (m *MenuBar) findMenuByAccelerator(key rune) int {
	key = rune(strings.ToLower(string(key))[0])
	for i, menu := range m.menus {
		if menu.acceleratorChar == key {
			return i
		}
	}
	return -1
}

// HandleMousePress handles mouse clicks.
func (m *MenuBar) HandleMousePress(event core.MousePressEvent) bool {
	metrics := m.EffectiveCellMetrics()
	bounds := m.Bounds()

	// Check active menu first - if clicking on an item in the dropdown
	if m.activeMenu != nil && !m.mouseDown {
		if m.activeMenu.HandleMousePress(event) {
			return true
		}
	}

	// Check if click is in menu bar
	if event.Y < metrics.CellHeight {
		// Check for scroll button clicks if scrolling is needed
		needsScrolling := m.menusNeedScrolling()
		if needsScrolling {
			dateTimeWidth := m.dateTimeWidth()
			scrollButtonsWidth := m.scrollButtonWidth() * 2
			dateTimeX := bounds.Width - dateTimeWidth
			leftButtonX := dateTimeX - scrollButtonsWidth

			// Check [<] button
			if event.X >= leftButtonX && event.X < leftButtonX+3*metrics.CellWidth {
				if m.canScrollLeft() {
					m.scrollOffset--
					m.Update()
				}
				return true
			}

			// Check [>] button
			rightButtonX := leftButtonX + 3*metrics.CellWidth
			if event.X >= rightButtonX && event.X < rightButtonX+3*metrics.CellWidth {
				if m.canScrollRight() {
					m.scrollOffset++
					m.Update()
				}
				return true
			}
		}

		// Check for click on left ellipsis ("...") to scroll left and open
		// that menu. When scrolled it is the leftmost element, so its hit
		// area reaches the very left edge (through the item indent too).
		if m.scrollOffset > 0 {
			if event.X >= 0 && event.X < m.leftInset()+m.ellipsisWidth() {
				// Track mouse down for potential drag (same as clicking a menu)
				m.mouseDown = true
				m.mouseDownX = event.X
				m.mouseDownY = event.Y
				m.dragging = false

				m.scrollOffset--
				// Open the menu that was just scrolled into view
				m.OpenMenu(m.scrollOffset)
				return true
			}
		}

		// Find which menu was clicked (past the left indent, and the
		// ellipsis when scrolled).
		x := m.leftInset()
		if m.scrollOffset > 0 {
			x += m.ellipsisWidth() // "..."
		}

		for i := m.scrollOffset; i < len(m.menus); i++ {
			menu := m.menus[i]
			menuWidth := m.menuTitleWidth(menu.title)
			// Fitts's law: with nothing scrolled off to its left, the first
			// item's hit area reaches the very left edge, so a click in the
			// indent (or the top-left corner) still activates it.
			left := x
			if i == m.scrollOffset && m.scrollOffset == 0 {
				left = 0
			}
			if event.X >= left && event.X < x+menuWidth {
				// Track mouse down for potential drag
				m.mouseDown = true
				m.mouseDownX = event.X
				m.mouseDownY = event.Y
				m.dragging = false

				if m.activeMenu == menu {
					// Toggle - close if same menu clicked
					m.CloseMenu()
				} else {
					m.OpenMenu(i)
				}
				return true
			}
			x += menuWidth
		}

		// Clicked on empty part of menu bar
		m.CloseMenu()
		m.mouseDown = false
		m.dragging = false
		return true
	}

	// Click below menu bar
	if event.Y >= 0 && event.Y < bounds.Height && m.activeMenu == nil {
		return true
	}

	// Click outside - if menu was open, dismiss and unfocus completely
	if m.activeMenu != nil {
		m.CloseMenuAndUnfocus()
		m.mouseDown = false
		m.dragging = false
		return true
	}

	return false
}

// HandleFocusIn is called when focus is gained.
func (m *MenuBar) HandleFocusIn() {
	if m.currentIndex < 0 && len(m.menus) > 0 {
		m.currentIndex = 0
	}
	// Enable accelerator display when focused with no menu down
	if m.activeMenu == nil {
		m.acceleratorsActive = true
	}
	m.Update()
}

// HandleFocusOut is called when focus is lost.
func (m *MenuBar) HandleFocusOut() {
	m.CloseMenu()
	m.dragging = false
	m.currentIndex = -1
	m.acceleratorsActive = false
	m.Update()
}

// menuItemAt maps a pointer position to the top-level menu index under
// it, or -1 when the pointer is not over a menu title within the bar row.
func (m *MenuBar) menuItemAt(px, py core.Unit) int {
	metrics := m.EffectiveCellMetrics()
	if py < 0 || py >= metrics.CellHeight {
		return -1
	}
	x := m.leftInset()
	if m.scrollOffset > 0 {
		x += m.ellipsisWidth()
	}
	for i := m.scrollOffset; i < len(m.menus); i++ {
		menu := m.menus[i]
		menuWidth := m.menuTitleWidth(menu.title)
		// Fitts's law: the first item's hit area reaches the left edge
		// (matches HandleMousePress) when nothing is scrolled off.
		left := x
		if i == m.scrollOffset && m.scrollOffset == 0 {
			left = 0
		}
		if px >= left && px < x+menuWidth {
			return i
		}
		x += menuWidth
	}
	return -1
}

// scrollButtonAt maps a pointer position to an overflow scroll button:
// -1 for [<], +1 for [>], 0 for neither.
func (m *MenuBar) scrollButtonAt(px, py core.Unit) int {
	if !m.menusNeedScrolling() {
		return 0
	}
	metrics := m.EffectiveCellMetrics()
	if py < 0 || py >= metrics.CellHeight {
		return 0
	}
	bounds := m.Bounds()
	dateTimeX := bounds.Width - m.dateTimeWidth()
	leftButtonX := dateTimeX - m.scrollButtonWidth()*2
	if px >= leftButtonX && px < leftButtonX+3*metrics.CellWidth {
		return -1
	}
	rightButtonX := leftButtonX + 3*metrics.CellWidth
	if px >= rightButtonX && px < rightButtonX+3*metrics.CellWidth {
		return 1
	}
	return 0
}

// HandleMouseMove handles mouse movement during drag.
func (m *MenuBar) HandleMouseMove(event core.MouseMoveEvent) bool {
	// A modally-blocked bar is disabled: it never highlights an item under the
	// pointer. Clear any lingering hover and stop before tracking a new one.
	if m.isModalBlocked() {
		if m.hoverIndex != -1 || m.hoverScrollBtn != 0 {
			m.hoverIndex = -1
			m.hoverScrollBtn = 0
			m.Update()
		}
		return false
	}

	// Track pointer hover over top-level items so the bar highlights the
	// item under the cursor even when no dropdown is open. Selection
	// (focus/active) still wins in Paint.
	if hi := m.menuItemAt(event.X, event.Y); hi != m.hoverIndex {
		m.hoverIndex = hi
		m.Update()
	}
	if sb := m.scrollButtonAt(event.X, event.Y); sb != m.hoverScrollBtn {
		m.hoverScrollBtn = sb
		m.Update()
	}

	// If no active menu, nothing more to do
	if m.activeMenu == nil {
		return false
	}

	// Even when not dragging, forward to menu for hover scroll handling
	if !m.mouseDown {
		// A dropdown is already open, so hovering a different top-level menu
		// drops it down instead of merely highlighting it - the same
		// menu-to-menu switch the drag path performs, but without needing the
		// button held. (Only graphical surfaces deliver bare hover moves.)
		if m.hoverIndex >= 0 && m.hoverIndex < len(m.menus) && m.menus[m.hoverIndex] != m.activeMenu {
			m.OpenMenu(m.hoverIndex)
			return true
		}
		// Just forward to menu for hover-based scrolling
		m.activeMenu.HandleMouseMove(event)
		return false // Don't consume - we're not in drag mode
	}

	metrics := m.EffectiveCellMetrics()

	// Detect if we've started dragging (moved enough from initial click)
	if !m.dragging {
		dx := event.X - m.mouseDownX
		dy := event.Y - m.mouseDownY
		if dx < 0 {
			dx = -dx
		}
		if dy < 0 {
			dy = -dy
		}
		// Only start dragging if moved at least half a cell
		if dx >= metrics.CellWidth/2 || dy >= metrics.CellHeight/2 {
			m.dragging = true
		} else {
			return true // Not dragging yet, consume but don't act
		}
	}

	// Check if mouse is in menu bar - switch menus and deselect dropdown item
	if event.Y < metrics.CellHeight {
		// Deselect current item in dropdown since we're back on the menu bar
		if m.activeMenu != nil && m.activeMenu.currentIndex != -1 {
			m.activeMenu.currentIndex = -1
			m.activeMenu.Update()
		}

		// Find which menu the mouse is over (past the left indent, and the
		// ellipsis when scrolled).
		x := m.leftInset()
		if m.scrollOffset > 0 {
			x += m.ellipsisWidth() // "..."
		}

		for i := m.scrollOffset; i < len(m.menus); i++ {
			menu := m.menus[i]
			menuWidth := m.menuTitleWidth(menu.title)
			// Fitts's law: the first item's hit area reaches the left edge
			// (matches HandleMousePress) when nothing is scrolled off.
			left := x
			if i == m.scrollOffset && m.scrollOffset == 0 {
				left = 0
			}
			if event.X >= left && event.X < x+menuWidth {
				if m.activeMenu != menu {
					m.OpenMenu(i)
				}
				return true
			}
			x += menuWidth
		}
		return true
	}

	// Check if mouse is in dropdown menu - forward to menu for scroll/highlight handling
	if m.activeMenu != nil && m.activeMenu.visible {
		// Forward to Menu.HandleMouseMove for scroll indicator handling
		m.activeMenu.HandleMouseMove(event)
		return true
	}

	return true
}

// HandleMouseRelease handles mouse release during drag.
// HandleMouseWheel scrolls the active dropdown when it overflows.
func (m *MenuBar) HandleMouseWheel(event core.MouseWheelEvent) bool {
	// An open dropdown OWNS the wheel: it scrolls its own items and the
	// gesture never falls through to pan the bar underneath (those are
	// two separate things). It is consumed even when the dropdown is too
	// short to scroll, so the bar below it stays put.
	if menu := m.activeMenu; menu != nil && menu.visible {
		if menu.needsScrolling() {
			down := event.DeltaY > 0 || event.PreciseY > 0
			up := event.DeltaY < 0 || event.PreciseY < 0
			if down && menu.canScrollDown() {
				menu.scrollDown(1)
			} else if up && menu.canScrollUp() {
				menu.scrollUp(1)
			}
			m.Update()
		}
		return true
	}

	// With no dropdown open, a wheel or two-finger pan over an overflowing bar steps
	// the first visible menu - the same gesture the tab strip uses. The
	// horizontal axis wins when present (two-finger pans are often
	// diagonal); precise deltas contribute sign only (whole-menu steps).
	if !m.menusNeedScrolling() {
		return false
	}
	step := event.DeltaY
	if event.DeltaX != 0 {
		step = event.DeltaX
	} else if event.PreciseX != 0 || event.PreciseY != 0 {
		p := event.PreciseY
		if event.PreciseX != 0 {
			p = event.PreciseX
		}
		if p < 0 {
			step = -1
		} else if p > 0 {
			step = 1
		}
	}
	if step == 0 {
		return false
	}
	if !m.canScrollLeft() && !m.canScrollRight() {
		return false
	}
	if step < 0 && m.canScrollLeft() {
		m.scrollOffset--
	} else if step > 0 && m.canScrollRight() {
		m.scrollOffset++
	}
	m.Update()
	return true
}

func (m *MenuBar) HandleMouseRelease(event core.MouseReleaseEvent) bool {
	wasMouseDown := m.mouseDown
	wasDragging := m.dragging

	// Always clear mouse state
	m.mouseDown = false
	m.dragging = false

	// If we weren't in mouse-down mode, nothing to do
	if !wasMouseDown {
		return false
	}

	// If not dragging (just a click), leave menu open for further interaction
	if !wasDragging {
		return true // Consume the release event but don't dismiss
	}

	// Check if release is on a dropdown menu item - trigger it
	if m.activeMenu != nil && m.activeMenu.visible {
		size := m.activeMenu.calculateSize()
		if event.X >= m.activeMenu.popupX && event.X < m.activeMenu.popupX+size.Width &&
			event.Y >= m.activeMenu.popupY && event.Y < m.activeMenu.popupY+size.Height {
			kind, itemIndex := m.activeMenu.hitRow(event.Y)
			if kind == 3 && itemIndex >= 0 && itemIndex < len(m.activeMenu.items) {
				item := m.activeMenu.items[itemIndex]
				if !item.Separator && item.Enabled {
					if item.SubMenu != nil {
						m.activeMenu.currentIndex = itemIndex
						m.activeMenu.openSubMenu(item)
					} else {
						m.activeMenu.triggerItem(item)
						// Note: triggerItem's onWillTrigger callback handles cleanup
						// and restores the previous window before the action executes
					}
					return true
				}
			}
		}
	}

	// Release not on a menu item - dismiss menu
	m.CloseMenu()
	return true
}

// IsDragging returns whether a menu drag is in progress.
func (m *MenuBar) IsDragging() bool {
	return m.dragging
}

// AccessibleInfo returns accessibility information.
func (m *MenuBar) AccessibleInfo() core.AccessibleInfo {
	info := m.AccessibleTrinket.AccessibleInfo()
	info.Role = core.RoleMenuBar
	info.SetSize = len(m.menus)

	if m.currentIndex >= 0 && m.currentIndex < len(m.menus) {
		info.PositionInSet = m.currentIndex + 1
		info.Value = m.menus[m.currentIndex].title
	}

	return info
}
