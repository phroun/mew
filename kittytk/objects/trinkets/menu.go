// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"strings"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// shortcutSizeNum/Den scale a shortcut to 80% of the menu's body size, so the
// column sits quietly beside the item text rather than competing with it.
// Graphical only: a terminal draws one size, its cell's.
const (
	shortcutSizeNum = 4
	shortcutSizeDen = 5
)

// graphicalMenuTrailingUnits is the small gap kept to the right of a
// graphical menu's shortcut, between it and the menu's right edge. Graphical
// menus have only a 1-pixel right stroke (not a whole char border), so this is
// about three-quarters of a cell rather than the two cells cell/TUI menus
// reserve.
func graphicalMenuTrailingUnits(cellW core.Unit) core.Unit {
	return cellW * 3 / 4
}

// shortcutFont returns the font used to draw a menu item's shortcut. In
// macOS-native mode it swaps the family to Apple's UI face (so the ⌃⌥⇧⌘ glyphs
// render in Apple's typeface) and shrinks it to 80%, while keeping the style
// and colors of the base font; otherwise it returns the base unchanged. The
// returned font is a copy, so the shared base font is never mutated, and
// callers measure and draw with this same font so widths stay exact.
func shortcutFont(base *core.Font, graphical bool) *core.Font {
	native := core.MacNativeShortcuts()
	if base == nil || (!graphical && !native) {
		return base
	}
	f := *base
	if native {
		f.Name = core.MacShortcutFontFamily
	}
	if graphical {
		if s := base.Size * shortcutSizeNum / shortcutSizeDen; s > 0 {
			f.Size = s
		}
	}
	return &f
}

// MenuItem represents an item in a menu.
type MenuItem struct {
	Text            string // Display text (with & removed, && converted to &)
	rawText         string // Original text with & markup
	acceleratorChar rune   // The chosen accelerator (lowercase), 0 if none
	acceleratorPos  int    // Its position in the display text, -1 if none
	// Every letter the label offers, in written order. Assignment is greedy
	// across siblings, so a later item may fall back to a backup letter.
	acceleratorCandidates []acceleratorCandidate
	Shortcut              core.Shortcut
	// Command is what this item MEANS, from the toolkit's command vocabulary
	// (core.Cmd*), and is how an item advertises a key without holding one:
	// the column is resolved from whatever keymap is in force where the FOCUS
	// is, at the moment the menu is drawn. So one item shows the Command-key
	// spelling on a Mac and the Control one elsewhere, shows nothing at all
	// while something has taken the keyboard on its own terms, and follows a
	// rebinding with no one having to tell it.
	//
	// A Command overrides a Shortcut on the same item: the item has stopped
	// naming a key and started naming a meaning.
	Command string
	// keyResolver answers "what key means this command, here?" — installed by
	// whoever composed the menu (a desktop's bar asks the desktop's context, a
	// detached window's asks its own). Nil in a menu nobody composed, which
	// falls back to the registry.
	keyResolver func(command string) string
	// ShortcutText is literal text for the item's shortcut column, printed
	// exactly where a bound Shortcut would print. It exists for keys the
	// TOOLKIT does not handle — a hosted application's own bindings, say —
	// which still deserve to be advertised in the menu. With both set the
	// column shows the bound shortcut, a space, then this text, so a command
	// reachable either way advertises both. See MenuItem.ShortcutDisplay.
	ShortcutText string
	Icon         *style.TextIcon
	Enabled      bool
	Checkable    bool
	Checked      bool
	Separator    bool // If true, this is a separator line
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
	displayText, accels := parseAcceleratorTitle(text)
	accel, pos := firstAccelerator(accels)
	return &MenuItem{
		Text:                  displayText,
		rawText:               text,
		acceleratorCandidates: accels,
		acceleratorChar:       accel,
		acceleratorPos:        pos,
		Enabled:               true,
		id:                    core.NextAutoCommandID(),
	}
}

// SetText sets the menu item text with accelerator parsing.
func (m *MenuItem) SetText(text string) {
	displayText, accels := parseAcceleratorTitle(text)
	accel, pos := firstAccelerator(accels)
	m.rawText = text
	m.Text = displayText
	m.acceleratorCandidates = accels
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

// ShortcutDisplay is what prints in the item's shortcut column: the bound
// shortcut, the literal ShortcutText, or — when both are set — the shortcut
// followed by a space and the text. Empty when the item advertises neither.
//
// Every consumer goes through this (width measurement, painting, the
// accessibility announcement), so the column can never render something the
// menu did not make room for.
func (m *MenuItem) ShortcutDisplay() string {
	bound := ""
	switch {
	case m.Command != "":
		// A command names a meaning, not a key: ask what key means it HERE.
		// Nothing means it here is a real answer -- while a guest has the
		// keyboard, this item genuinely has no key -- so the column is blank
		// rather than advertising something that would not work.
		if key := m.resolveCommandKey(); key != "" {
			bound = core.DisplayKey(key)
		}
	case m.Shortcut != "":
		bound = core.DisplayKey(string(m.Shortcut))
	}
	switch {
	case bound != "" && m.ShortcutText != "":
		return bound + " " + m.ShortcutText
	case bound != "":
		return bound
	default:
		return m.ShortcutText
	}
}

// resolveCommandKey asks what key means this item's command right now: the
// composer's resolver where there is one, else the registry, which is the best
// a menu nobody composed can do.
func (m *MenuItem) resolveCommandKey() string {
	if m.keyResolver != nil {
		return m.keyResolver(m.Command)
	}
	return core.DefaultKeyRegistry().KeyForCommand(m.Command)
}

// SetCommand names what this item MEANS, so its key column is resolved rather
// than stored (see the Command field). It replaces SetShortcut: an item that
// names a command does not name a key.
func (m *MenuItem) SetCommand(command string) *MenuItem {
	m.Command = command
	return m
}

// SetShortcutText sets literal text for the item's shortcut column (see
// ShortcutText).
func (m *MenuItem) SetShortcutText(text string) *MenuItem {
	m.ShortcutText = text
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

// Well-known ITEM roles, for the standard items the system would otherwise
// synthesize into an edit menu. An app that wants those items under its own
// captions, or in its own position among its own items, declares them itself
// and tags each with its role: the system then wires the standard BEHAVIOUR
// onto the app's item - the focused-trinket handler, the host shortcut, and
// the enable/disable rules - instead of prepending a second set.
//
// This is the item-level half of the well-known contract: a menu's tag says
// what a MENU is for, an item's tag says what an ITEM is for, and in both
// cases the app keeps naming and placement while the system keeps behaviour.
// A role the app does not claim is synthesized as before, so claiming some
// and not others is fine.
const (
	ItemIDCut       = "cut"       // Cut to the system clipboard
	ItemIDCopy      = "copy"      // Copy to the system clipboard
	ItemIDPaste     = "paste"     // Paste from the system clipboard
	ItemIDSelectAll = "selectall" // Select the focused trinket's whole content
)

// standardEditItemRole reports whether id is a well-known edit-item role the
// system supplies behaviour for.
func standardEditItemRole(id string) bool {
	switch id {
	case ItemIDCut, ItemIDCopy, ItemIDPaste, ItemIDSelectAll:
		return true
	}
	return false
}

// Menu represents a dropdown menu.
type Menu struct {
	core.TrinketBase
	core.TrinketKeys
	core.AccessibleTrinket

	title           string // Display title (with & removed, && converted to &)
	rawTitle        string // Original title with & markup
	acceleratorChar rune   // The chosen accelerator (lowercase), 0 if none
	acceleratorPos  int    // Its position in the display title, -1 if none
	// Every letter the title offers, in written order (see parseAcceleratorTitle).
	acceleratorCandidates []acceleratorCandidate
	items                 []*MenuItem
	currentIndex          int
	visible               bool
	wellKnownID           string // system-level role tag (see MenuID* constants), "" if none
	anchor                string // untagged menus: the well-known slot to sit after

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
	// keyResolver answers "which key means this command, here?" for this
	// menu's items -- installed by whoever composed the menu (see
	// SetKeyResolver), and handed on to items and submenus.
	keyResolver func(command string) string
	parentItem  *MenuItem

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
	// prepared records that onAboutToShow has already run for this opening,
	// so it runs exactly once however early the opener needs it.
	prepared      bool
	onAboutToHide func()
	onItemPressed func() // Called when an item is pressed, signals MenuBar to enter drag mode
	onWillTrigger func() // Called just before an item is triggered, to restore window focus

	// Accessibility
	accessibilityManager *core.AccessibilityManager
}

// acceleratorCandidate is one letter a title offers as its accelerator, and
// where that letter sits in the display text.
type acceleratorCandidate struct {
	Char rune // lowercase
	Pos  int  // index in the display text
}

// parseAcceleratorTitle parses a title with & markup and returns the display
// text alongside every accelerator the title offers, in the order written.
//
// A title may mark more than one letter, which reads as a preference list:
// "&Hel&p" offers 'h' first and 'p' as a backup. Assignment is greedy and
// left to right — across the siblings at one level, each takes the first
// letter no earlier sibling has claimed — so four items all marked "&A&B&C"
// take A, B and C, and the fourth is left without one. A backup is what keeps
// a menu reachable when its first choice is spoken for.
//
// Examples: "&File"        -> "File",        [{f 0}]
//
//	"E&xit"        -> "Exit",        [{x 1}]
//	"&Hel&p"       -> "Help",        [{h 0} {p 3}]
//	"Save && Exit" -> "Save & Exit", []
func parseAcceleratorTitle(raw string) (display string, accels []acceleratorCandidate) {
	runes := []rune(raw)
	var result []rune

	for i := 0; i < len(runes); i++ {
		if runes[i] == '&' {
			if i+1 < len(runes) && runes[i+1] == '&' {
				// Escaped ampersand
				result = append(result, '&')
				i++ // Skip next &
			} else if i+1 < len(runes) {
				// Accelerator - next char is one of the offered letters
				accels = append(accels, acceleratorCandidate{
					Char: rune(strings.ToLower(string(runes[i+1]))[0]),
					Pos:  len(result),
				})
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

// firstAccelerator reports the leading candidate, which is what a title means
// when nothing has claimed its letters yet. Zero and -1 when none is offered.
func firstAccelerator(accels []acceleratorCandidate) (rune, int) {
	if len(accels) == 0 {
		return 0, -1
	}
	return accels[0].Char, accels[0].Pos
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
func drawTextSegments(p *core.Painter, x, y core.Unit, font *core.Font, metrics core.CellMetrics, segs ...textSegment) core.Unit {
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
		total += font.MeasureTextIn(seg.text, metrics)
	}
	return total
}

// NewMenu creates a new menu.
func NewMenu(title string) *Menu {
	displayTitle, accels := parseAcceleratorTitle(title)
	accel, pos := firstAccelerator(accels)
	m := &Menu{
		rawTitle:              title,
		title:                 displayTitle,
		acceleratorCandidates: accels,
		acceleratorChar:       accel,
		acceleratorPos:        pos,
		currentIndex:          -1,
		maxVisible:            0, // 0 = calculate from available space when shown
	}
	m.TrinketBase = *core.NewTrinketBase()
	// A dropped-down menu is a list that runs vertically, with Left and Right
	// crossing between it and its submenus. The bare accelerator letters are
	// NOT bindings -- they are ordinary typing matched against the item
	// titles, which is why they are not declared here.
	m.SetCommands(
		core.CmdTrinketItemPrior, core.CmdTrinketItemUp,
		core.CmdTrinketItemNext, core.CmdTrinketItemDown,
		core.CmdTrinketItemLeft, core.CmdTrinketItemRight,
		core.CmdTrinketPagePrior, core.CmdTrinketPageNext,
		core.CmdTrinketBeg, core.CmdTrinketEnd,
		core.CmdTrinketActivate, core.CmdTrinketCancel,
	)
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
	mm := m.menuMetrics()
	// Calculate how many items can fit, leaving room for scroll indicators if needed
	maxRows := int(availableHeight / mm.RowH)
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

// SetAnchor places an UNTAGGED menu immediately after a well-known slot
// rather than in the trailing custom block — "after: file" puts it between
// the file menu and the edit menu. Menus sharing an anchor keep their
// declared order, and an anchor on a menu that already carries a well-known
// tag is ignored (its role fixes its place).
//
// The anchor names a canonical SLOT, not a live menu, so placement is stable
// whether or not the app declares the neighbour: anchoring after "file" in an
// app with no file menu still lands ahead of edit rather than teleporting.
//
// This is how an app departs from the standard layout without leaving the
// standard vocabulary — it can say "after the file menu", never "third" — so
// the canonical roles stay the frame of reference even for a bar that is
// deliberately ordered some other way.
func (m *Menu) SetAnchor(id string) *Menu {
	m.anchor = id
	return m
}

// Anchor returns the well-known slot this menu is placed after, or "".
func (m *Menu) Anchor() string { return m.anchor }

// SetTitle sets the menu title.
func (m *Menu) SetTitle(title string) {
	displayTitle, accels := parseAcceleratorTitle(title)
	accel, pos := firstAccelerator(accels)
	m.rawTitle = title
	m.title = displayTitle
	m.acceleratorCandidates = accels
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
	m.handKeyResolverTo(item)
}

// SetKeyResolver installs what this menu's items ask when they need to know
// which key means their command right now. Whoever composed the menu knows
// which context to ask -- a desktop's bar asks the desktop's, a detached
// window's bar asks that window's -- and both of those follow the FOCUS, so
// what an item advertises is what would actually happen if you pressed it.
//
// It reaches everything under the menu, submenus included, and everything
// added later.
func (m *Menu) SetKeyResolver(fn func(command string) string) {
	m.keyResolver = fn
	for _, item := range m.items {
		m.handKeyResolverTo(item)
	}
}

// handKeyResolverTo gives one item (and its submenu) this menu's resolver.
func (m *Menu) handKeyResolverTo(item *MenuItem) {
	if item == nil {
		return
	}
	item.keyResolver = m.keyResolver
	if item.SubMenu != nil && item.SubMenu != m {
		item.SubMenu.SetKeyResolver(m.keyResolver)
	}
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
	m.prepareToShow()

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
	m.prepared = false
	m.Update()
}

// prepareToShow runs the about-to-show handler, once per opening.
//
// A menu that fills or relabels itself in that handler decides its own SIZE
// there, and its size is what its opener places it by -- whether a dropdown
// clears the surface's right edge, where a submenu's left edge goes, how far
// a chooser has to be pushed back on screen. Firing it inside Show, after the
// opener had already measured, meant every one of those decisions was made
// against the contents of the PREVIOUS opening: wrong the first time a menu
// dropped, and right afterwards only because the handler had left the right
// items behind.
//
// So the openers call this before they measure, and Show calls it too for
// anything that shows a menu without measuring first. The flag clears on
// Hide.
func (m *Menu) prepareToShow() {
	if m.prepared {
		return
	}
	m.prepared = true
	if m.onAboutToShow != nil {
		m.onAboutToShow()
	}
}

// SetOnAboutToShow sets the about to show callback.
func (m *Menu) SetOnAboutToShow(handler func()) {
	m.onAboutToShow = handler
}

// OnAboutToShow returns the about-to-show callback, or nil if none is set.
// It exists so a menu REBUILT from a declared one (see systemEditMenu, which
// moves an app's items into a menu of its own) can carry the app's callback
// across instead of dropping it: the app uses that hook to refresh its items
// against live state, and losing it silently blanks whatever it maintained.
func (m *Menu) OnAboutToShow() func() { return m.onAboutToShow }

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
		extras = append(extras, core.SpeakKey(string(item.Shortcut)))
	}
	if item.ShortcutText != "" {
		extras = append(extras, item.ShortcutText)
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
	mm := m.menuMetrics()

	// Calculate max width using font for text, cells for decorative elements
	g := m.graphicalSurface()
	maxWidth := core.Unit(0)
	for _, item := range m.items {
		// Item text uses font measurement, counted in THIS menu's
		// denomination -- Font.MeasureText answers at the default one, which
		// is a different currency the moment the menu sits in a window that
		// carries an override. The cell-based padding around it needs no
		// such treatment: a cell is a fixed physical size.
		itemWidth := mm.TextWidth(item.Text)

		// Shortcut: spacing (3 cells) + shortcut text (font-based). Measure
		// with the same font used to draw it -- 80% on a graphical surface,
		// Apple's face in native mode -- so width and render never disagree.
		if sc := item.ShortcutDisplay(); sc != "" {
			itemWidth += mm.CellW * 3 // spacing before shortcut
			itemWidth += mm.Width(sc, shortcutFont(mm.Font, g))
		}

		// Submenu arrow (3 cells) - decorative
		if item.SubMenu != nil {
			itemWidth += mm.CellW * 3
		}

		if itemWidth > maxWidth {
			maxWidth = itemWidth
		}
	}

	// Add padding (gutter: 3 cells, content space: 1 cell, right border: 1 cell)
	maxWidth += mm.CellW * 5

	// Sum the heights of the visible item rows (thin separators on
	// graphical surfaces are shorter than a text row), plus a full row
	// for each scroll indicator when scrolling.
	var height core.Unit
	visible := m.visibleItemCount()
	for i := 0; i < visible; i++ {
		idx := m.scrollOffset + i
		if idx >= len(m.items) {
			break
		}
		height += m.rowHeightAt(idx, g, mm.RowH)
	}
	if m.needsScrolling() {
		height += 2 * mm.RowH // one row per scroll indicator
	}

	return core.UnitSize{
		Width:  maxWidth,
		Height: height,
	}
}

// separatorBandFraction gives the height a separator row occupies on
// graphical (pixel) surfaces, as a fraction of a cell: 3/16, a thin band
// (~6 device px at the usual 2x scale) carrying a single hairline, rather
// than a full text row of dashes.
//
// Against the menu's ROW rather than as a raw unit count, so the band keeps
// its proportion whatever denomination the menu is counted in and whatever
// core.MenuScale shortens the row to. Three units was 3/16 of a cell only at
// the default denomination.
func separatorBandUnits(rowH core.Unit) core.Unit {
	if h := rowH * 3 / 16; h > 0 {
		return h
	}
	return 1
}

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
func (m *Menu) paintScrollBumper(p *core.Painter, y core.Unit, size core.UnitSize, mm MenuMetrics, gutterStyle, contentStyle style.CellStyle, g bool, scale int, hairColor style.Color, hairStyle style.CellStyle, glyph rune) {
	gutterWidth := mm.CellW * 3
	paintGutterBackground(p, core.UnitRect{X: m.popupX, Y: y, Width: gutterWidth, Height: mm.RowH}, gutterStyle, g)
	p.FillRect(core.UnitRect{X: m.popupX + gutterWidth, Y: y, Width: size.Width - gutterWidth, Height: mm.RowH}, ' ', contentStyle)
	if g {
		paintGutterDivider(p, m.popupX+gutterWidth, y, p.UnitSpanPxY(y, y+mm.RowH), hairColor, hairStyle)
	}
	// Center the three glyphs in the white content area only.
	centerX := m.popupX + gutterWidth + (size.Width-gutterWidth)/2
	mm.DrawGlyph(p, centerX-mm.CellW*2, y, glyph, contentStyle)
	mm.DrawGlyph(p, centerX, y, glyph, contentStyle)
	mm.DrawGlyph(p, centerX+mm.CellW*2, y, glyph, contentStyle)
}

// paintGutterBackground fills a menu row's gutter span.
//
// On the graphical path the gutter's own colour is laid at MenuGutterAlpha
// over the menu background already beneath it -- the whole-menu fill, white
// in the usual scheme -- so the gutter reads as a shaded band of the menu
// rather than a separate panel butted against it. blend is false where that
// is not the gutter's colour to soften (a focused row's selection fill) or
// where the surface cannot blend at all, and the fill is then solid, which
// is where it stood.
func paintGutterBackground(p *core.Painter, r core.UnitRect, st style.CellStyle, blend bool) {
	if blend {
		gr, gg, gb := st.Bg.RGBComponents()
		if p.FillRectPixelsAlpha(r.X, r.Y, 0, 0,
			p.UnitSpanPxX(r.X, r.X+r.Width), p.UnitSpanPxY(r.Y, r.Y+r.Height),
			gr, gg, gb, MenuGutterAlpha) {
			return
		}
	}
	p.FillRect(r, ' ', st)
}

// paintGutterDivider draws the single-pixel rule down the right edge of a
// menu's gutter, on the gutter's own last pixel column.
//
// Inked at MenuSeparatorAlpha over the gutter it sits on, like the rule
// between two groups of items: a division between the gutter and the
// content, not a line drawn down the menu. Opaque where the surface cannot
// blend, since a divider nobody can see is worse than one drawn too strongly.
func paintGutterDivider(p *core.Painter, x, y core.Unit, hPx int, hairColor style.Color, hairStyle style.CellStyle) {
	r, g, b := hairColor.RGBComponents()
	if !p.FillRectPixelsAlpha(x, y, -1, 0, 1, hPx, r, g, b, MenuSeparatorAlpha) {
		p.FillRectPixels(x, y, -1, 0, 1, hPx, hairStyle)
	}
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
func (m *Menu) rowHeightAt(idx int, graphical bool, rowH core.Unit) core.Unit {
	if graphical && idx >= 0 && idx < len(m.items) && m.items[idx].Separator {
		return separatorBandUnits(rowH)
	}
	return rowH
}

// contentTopY returns the Y of the first item row (below the top scroll
// indicator, if any).
func (m *Menu) contentTopY() core.Unit {
	y := m.popupY
	if m.needsScrolling() {
		y += m.menuMetrics().RowH
	}
	return y
}

// itemTopY returns the top Y of a visible item, walking the variable row
// heights of the items above it in the current scroll window.
func (m *Menu) itemTopY(itemIndex int) core.Unit {
	mm := m.menuMetrics()
	g := m.graphicalSurface()
	y := m.contentTopY()
	for i := m.scrollOffset; i < itemIndex && i < len(m.items); i++ {
		y += m.rowHeightAt(i, g, mm.RowH)
	}
	return y
}

// hitRow maps a Y coordinate to a slot: kind is 0 none, 1 top scroll
// indicator, 2 bottom scroll indicator, 3 an item (itemIndex set).
// It honors the variable row heights of thin separators.
func (m *Menu) hitRow(y core.Unit) (kind, itemIndex int) {
	mm := m.menuMetrics()
	g := m.graphicalSurface()
	cur := m.popupY
	if m.needsScrolling() {
		if y >= cur && y < cur+mm.RowH {
			return 1, -1
		}
		cur += mm.RowH
	}
	visible := m.visibleItemCount()
	for i := 0; i < visible; i++ {
		idx := m.scrollOffset + i
		if idx >= len(m.items) {
			break
		}
		h := m.rowHeightAt(idx, g, mm.RowH)
		if y >= cur && y < cur+h {
			return 3, idx
		}
		cur += h
	}
	if m.needsScrolling() && y >= cur && y < cur+mm.RowH {
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
	mm := m.menuMetrics()
	font := mm.Font
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
		m.paintScrollBumper(p, currentY, size, mm, scheme.GetMenuGutter(), menuItemStyle, g, scale, hairColor, hairStyle, glyph)
		currentY += mm.RowH
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
		// A focused row's gutter carries the SELECTION's colour, not a
		// gutter colour, so it is laid solid: softening a highlight is not
		// what softening the gutter means.
		gutterIsOwnColor := true
		if item.Separator {
			gutterStyle = scheme.GetMenuSeparatorGutter()
			contentStyle = scheme.GetMenuSeparator()
		} else if !item.Enabled {
			gutterStyle = scheme.GetDisabledMenuGutter()
			contentStyle = scheme.GetDisabledMenuItem()
		} else if itemIndex == m.currentIndex {
			gutterStyle = scheme.GetFocusedMenuItemText()
			contentStyle = scheme.GetFocusedMenuItemText()
			gutterIsOwnColor = false
		} else {
			gutterStyle = scheme.GetMenuGutter()
			contentStyle = scheme.GetMenuItemText()
		}

		// Gutter area: 3 cells (border + checkmark + 1 space)
		gutterWidth := mm.CellW * 3

		// Row height: separators collapse to a thin band on graphical.
		rowH := m.rowHeightAt(itemIndex, g, mm.RowH)

		// Draw gutter background
		paintGutterBackground(p, core.UnitRect{
			X:      m.popupX,
			Y:      itemY,
			Width:  gutterWidth,
			Height: rowH,
		}, gutterStyle, g && gutterIsOwnColor)

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
			paintGutterDivider(p, m.popupX+gutterWidth, itemY, p.UnitSpanPxY(itemY, itemY+rowH), hairColor, hairStyle)
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
					// Half-strength ink over the row's own background: a
					// rule that divides the items without ruling a line
					// through the menu. Opaque where the surface cannot
					// blend, since a separator nobody can see is worse than
					// one drawn too strongly.
					hr, hg, hb := hairColor.RGBComponents()
					if !p.FillRectPixelsAlpha(m.popupX+gutterWidth, itemY, marginPx, offY, wPx, 1,
						hr, hg, hb, MenuSeparatorAlpha) {
						p.FillRectPixels(m.popupX+gutterWidth, itemY, marginPx, offY, wPx, 1, hairStyle)
					}
				}
			} else {
				// Cell surface: the dashed-row idiom, gutter + content.
				for x := m.popupX + mm.CellW; x < m.popupX+size.Width-mm.CellW; x += mm.CellW {
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

		x := m.popupX + mm.CellW

		// Draw checkmark or icon in gutter area. On the graphical path the
		// tick draws OVER the gutter rather than laying a cell of its own:
		// the gutter's background is already there, blended over the menu,
		// which a cell of the flat gutter colour would stamp back out around
		// the tick.
		//
		// Only there. A terminal cell holds ONE background attribute, so a
		// transparent one is not the gutter showing through -- it is the
		// terminal's own default cell, and the tick came out sitting in a
		// hole in the gutter.
		tickStyle := gutterStyle
		if g {
			tickStyle = gutterStyle.WithBg(style.ColorTransparent)
		}
		if item.Checkable {
			if item.Checked {
				mm.DrawGlyph(p, x, itemY, '✓', tickStyle)
			}
		} else if item.Icon != nil && len(item.Icon.Cells) > 0 {
			cell := item.Icon.Cells[0]
			mm.DrawGlyph(p, x, itemY, cell.Char, cell.Style)
		}
		x += mm.CellW * 2 // Move past checkmark + 1 gutter space

		// Draw a space in content area before text
		mm.DrawGlyph(p, x, itemY, ' ', contentStyle)
		x += mm.CellW

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
			x += drawTextSegments(p, x, itemY+mm.YOff, font, m.EffectiveCellMetrics(), segs...)
		} else {
			// No accelerator or disabled - draw entire text
			p.DrawText(x, itemY+mm.YOff, item.Text, contentStyle, font)
			x += mm.TextWidth(item.Text)
		}

		// Draw shortcut or submenu arrow at the right (in content area). The
		// menu width is unchanged; only the shortcut hugs closer to the right
		// edge on graphical surfaces (whose right border is a single pixel, not
		// a full char cell), trimming the empty space to its right.
		if item.SubMenu != nil {
			arrowX := m.popupX + size.Width - mm.CellW*2
			mm.DrawGlyph(p, arrowX, itemY, '▸', contentStyle)
		} else if shortcutStr := item.ShortcutDisplay(); shortcutStr != "" {
			rightPad := mm.CellW * 2
			if p.Graphical() {
				rightPad = graphicalMenuTrailingUnits(mm.CellW)
			}
			// Native mode renders the shortcut in Apple's UI face at 80%;
			// measure and draw with that same font so the right-alignment is
			// exact, and center the shorter line box within the item's row.
			sf := shortcutFont(font, g)
			shortcutWidth := mm.Width(shortcutStr, sf)
			shortcutX := m.popupX + size.Width - shortcutWidth - rightPad
			shortcutY := itemY + mm.GlyphYOff(sf)
			shortcutStyle := contentStyle
			if item.Enabled {
				// Dim on either surface: a terminal renders the reduced
				// intensity from the attribute, and a pixel surface works the
				// colour out from it (see the raster backend's styleColors).
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
		m.paintScrollBumper(p, currentY, size, mm, scheme.GetMenuGutter(), menuItemStyle, g, scale, hairColor, hairStyle, glyph)
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

	switch m.KeyCommand(event.Key) {
	case core.CmdTrinketItemPrior, core.CmdTrinketItemUp:
		m.currentIndex = m.findPrevEnabled(m.currentIndex)
		m.ensureVisible(m.currentIndex)
		m.closeSubMenu()
		m.announceCurrentItem()
		m.Update()
		return true

	case core.CmdTrinketItemNext, core.CmdTrinketItemDown:
		m.currentIndex = m.findNextEnabled(m.currentIndex)
		m.ensureVisible(m.currentIndex)
		m.closeSubMenu()
		m.announceCurrentItem()
		m.Update()
		return true

	case core.CmdTrinketItemLeft:
		if m.parentMenu != nil {
			m.Hide()
			return true
		}
		return false // Let menu bar handle it

	case core.CmdTrinketItemRight:
		item := m.CurrentItem()
		if item != nil && item.SubMenu != nil {
			m.openSubMenu(item)
			return true
		}
		return false // Let menu bar handle it

	case core.CmdTrinketActivate:
		item := m.CurrentItem()
		if item != nil {
			if item.SubMenu != nil {
				m.openSubMenu(item)
			} else {
				m.triggerItem(item)
			}
			return true
		}

	case core.CmdTrinketCancel:
		if m.parentMenu != nil {
			// Submenu - hide it and return to parent menu
			m.Hide()
			return true
		}
		// Top-level menu - let menu bar handle closing for proper cleanup
		// (MenuBar.CloseMenu will call Hide on us)
		return false

	case core.CmdTrinketBeg:
		m.currentIndex = m.findNextEnabled(-1)
		m.scrollOffset = 0
		m.closeSubMenu()
		m.Update()
		return true

	case core.CmdTrinketEnd:
		m.currentIndex = m.findPrevEnabled(0)
		m.ensureVisible(m.currentIndex)
		m.closeSubMenu()
		m.Update()
		return true

	case core.CmdTrinketPagePrior:
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

	case core.CmdTrinketPageNext:
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
	item.SubMenu.prepareToShow()

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
	core.TrinketKeys
	core.AccessibleTrinket

	menus        []*Menu
	currentIndex int
	activeMenu   *Menu

	// keyResolver answers "which key means this command, here?" for every item
	// on this bar, handed down to its menus (see SetKeyResolver).
	keyResolver func(command string) string
	// acceleratorChord is the pattern a chord accelerator is formed from,
	// with "*" standing in for a menu's mnemonic ([window] accelerator_chord).
	acceleratorChord string
	// keyContext is the set of actions available right now. Accelerators are
	// formed against it: a chord it already claims is not the accelerator's to
	// take. A nil context claims nothing, so accelerators are all live — which
	// is how the toolkit behaves before a host installs one.
	keyContext *core.KeyContext
	// accelAssignments is the per-menu outcome, recomputed when the menu list
	// changes or the context moves on.
	accelAssignments []acceleratorAssignment
	accelRevision    uint64
	accelStale       bool
	hoverIndex       int // Top-level item under the pointer (-1 = none)
	hoverScrollBtn   int // Overflow scroll button under the pointer (-1 left, +1 right, 0 none)

	// modalBlocked reports whether this menu bar is disabled by a modal (the
	// app it represents is modally blocked). A blocked bar shows no hover
	// highlight on its items. nil means never blocked.
	modalBlocked func() bool

	// Appearance
	showShortcuts bool
	hideCalendar  bool // when true, omit the right-hand date/time area

	// graphicalCached records whether the last paint was on a pixel
	// surface; measurement (dateTimeWidth) has no painter and reads it, and
	// so does everything about HOVER — which is a pointer affordance and does
	// not exist on a cell surface, where the only "move" a click produces is
	// the position report that precedes it.
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
	onFocusDock func(forward bool)

	// onFocusChanged, when set, is told each time the bar takes or gives up
	// the keyboard. The desktop uses it to lend the bar a row it does not
	// normally get on a chrome-free single-app screen.
	onFocusChanged func(focused bool)

	// Callback when Tab navigation should leave a bar that has no dock to
	// hand off to - a window's own menu bar (a detached main window's chrome,
	// so solo and torn-off too). forward is false for Shift+Tab. Returns
	// whether focus actually moved; when it doesn't, the key falls through
	// rather than being swallowed. The desktop's bar leaves this nil and uses
	// onFocusDock for both directions.
	onFocusOut func(forward bool) bool

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
	// A menu bar runs horizontally, so Left and Right walk it; Down drops the
	// current menu open, which is the same act as Enter or Space. F10 is the
	// bar's own toggle, and Tab either way leaves it. The bare accelerator
	// letters are ordinary typing matched against the menu titles, and the
	// chord accelerators are FORMED from the configured pattern rather than
	// bound, so neither is declared here.
	m.SetCommands(
		core.CmdTrinketItemLeft, core.CmdTrinketItemRight,
		core.CmdTrinketItemPrior, core.CmdTrinketItemNext,
		core.CmdTrinketItemDown,
		core.CmdTrinketActivate, core.CmdTrinketCancel,
		core.CmdAppMenu,
		core.CmdFocusNext, core.CmdFocusPrior,
	)
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

// SetOnFocusOut sets the handler for Tab (forward=true) and Shift+Tab
// (forward=false) leaving a menu bar that has no dock to hand off to - a
// window's own bar, which a detached, solo, or torn-off window carries. It
// reports whether focus moved; false leaves the key unhandled. Windows wire
// this in SetWindowMenuBar so Shift+Tab reaches the title bar and Tab reaches
// the content, the same chain Tab walks off either end of.
func (m *MenuBar) SetOnFocusOut(callback func(forward bool) bool) {
	m.onFocusOut = callback
}

// SetOnFocusDock sets a callback for when Tab navigation should transfer
// out of the bar toward the rest of the desktop chrome. forward reports the
// direction (Tab true, Shift+Tab false), so the desktop can route through
// the themed title bar when one is present rather than always to the dock.
func (m *MenuBar) SetOnFocusDock(callback func(forward bool)) {
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
	if bf := m.menuMetrics().Font; bf != nil && bf.Size > 0 {
		base = bf.Size // the desktop's font_size, at the menu scale
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
	mm := m.menuMetrics()
	if f := m.dateTimeFont(); f != nil {
		return mm.Width(dateTimeFormat, f)
	}
	// " Mon Jan 02 15:04 " = 18 chars
	return 18 * mm.CellW
}

// scrollButtonWidth returns the width of each scroll button.
func (m *MenuBar) scrollButtonWidth() core.Unit {
	return m.menuMetrics().CellW * 3 // [<] or [>]
}

// menusNeedScrolling returns true if menus don't fit and need scroll buttons.
func (m *MenuBar) menusNeedScrolling() bool {
	bounds := m.Bounds()
	availableWidth := bounds.Width - m.dateTimeWidth()
	return m.calculateTotalMenusWidth() > availableWidth
}

// menusRightLimit returns the x coordinate where top-level menu hit areas
// end. When the bar overflows, titles extend beneath the [<][>] scroll
// buttons and the clock, and a press or hover there must reach the
// buttons — never the hidden title — so the limit is the buttons' left
// edge. In a bar too narrow to lay the buttons out at all (their origin
// would be negative and painting clips them away) no limit applies.
func (m *MenuBar) menusRightLimit() core.Unit {
	bounds := m.Bounds()
	if m.menusNeedScrolling() {
		buttonsX := bounds.Width - m.dateTimeWidth() - m.scrollButtonWidth()*2
		if buttonsX >= 0 {
			return buttonsX
		}
	}
	return bounds.Width
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
// registered keybinding (e.g., Mega+key is used for something else).
// SetAcceleratorChord sets the pattern chord accelerators are formed from,
// with "*" standing in for a menu's mnemonic. Blank forms none.
func (m *MenuBar) SetAcceleratorChord(pattern string) {
	m.acceleratorChord = pattern
	m.InvalidateAccelerators()
}

// SetKeyContext hands the bar the set of actions available right now, which is
// what accelerators are formed against and what decides which of them are
// live. A nil context claims nothing.
func (m *MenuBar) SetKeyContext(ctx *core.KeyContext) {
	m.keyContext = ctx
	m.InvalidateAccelerators()
}

// ToggleMenuFocus is what the menu key does: take the keyboard, or give it
// back when the bar already had it.
//
// Exported so anything wanting the bar focused can SAY so, rather than
// synthesising the keystroke that happens to mean it today. A host that
// rebinds app_menu would leave a faked "F10" resolving to nothing, and the
// bar would quietly stop answering.
func (m *MenuBar) ToggleMenuFocus() {
	if m.HasFocus() {
		m.CloseMenuAndUnfocus()
	} else {
		m.SetFocus()
		if m.currentIndex < 0 && len(m.menus) > 0 {
			m.currentIndex = 0
		}
	}
	m.Update()
}

// InvalidateAccelerators marks the assignment for recomputation — the menu
// list changed, or the situation did.
func (m *MenuBar) InvalidateAccelerators() {
	m.accelStale = true
	m.Update()
}

// refreshAccelerators recomputes which letter each menu shows and whether it
// is live, then publishes the live ones into the context.
//
// Staleness is a revision comparison rather than a subscription: the context
// bumps a revision whenever it is rebuilt, and anything that repaints notices
// on its own. Nothing has to remember to notify the menu bar, which is what
// makes an accelerator light up by itself when the trinket that was claiming
// its chord loses focus.
func (m *MenuBar) refreshAccelerators() {
	rev := m.keyContext.Revision()
	if !m.accelStale && rev == m.accelRevision && len(m.accelAssignments) == len(m.menus) {
		return
	}
	m.accelStale = false
	m.accelRevision = rev

	cands := make([][]acceleratorCandidate, len(m.menus))
	for i, menu := range m.menus {
		cands[i] = menu.acceleratorCandidates
	}
	pattern := m.acceleratorChord
	ctx := m.keyContext
	// Last time's accelerators go FIRST. The clash test below asks whether
	// something has already claimed a chord, and an accelerator this bar
	// formed itself is not something else -- leaving them in place made the
	// bar read its own assignment as a clash and mute every accelerator it
	// had, from the second refresh onward. This also drops entries for menus
	// that have since gone.
	ctx.ClearAccelerators()
	// The keymap where this BAR sits, which is what decides whether a chord is
	// already spoken for. Not the keymap in force where the focus is: an
	// accelerator that moved to a different letter every time the focus
	// changed would be worse than no accelerator at all.
	reg := core.FindKeyRegistry(m)
	m.accelAssignments = assignAccelerators(cands, func(ch rune) bool {
		key := formAcceleratorKey(pattern, ch)
		if key == "" {
			return false
		}
		// Claimed by this situation, or spoken for by the keymap at large --
		// M-a means select-all whether or not anything is offering it here,
		// and a menu that wants a chord takes one nothing else has.
		return ctx.Claims(key) || reg.Binds(key)
	})

	for i, menu := range m.menus {
		a := m.accelAssignments[i]
		// The chosen letter follows the assignment, so every existing lookup
		// — the bare letters a focused bar answers to, accessibility — uses
		// the letter that is actually underlined.
		menu.acceleratorChar, menu.acceleratorPos = a.Char, a.Pos
		if a.Active {
			if key := formAcceleratorKey(pattern, a.Char); key != "" && ctx != nil {
				ctx.Add(key, core.CommandAppAccelerator)
			}
		}
	}
}

// acceleratorAssignmentFor returns the outcome for a menu, recomputing first
// if the situation has moved on.
func (m *MenuBar) acceleratorAssignmentFor(menu *Menu) acceleratorAssignment {
	m.refreshAccelerators()
	for i, mm := range m.menus {
		if mm == menu && i < len(m.accelAssignments) {
			return m.accelAssignments[i]
		}
	}
	return acceleratorAssignment{Pos: -1}
}

// ShouldShowAccelerator reports whether a menu's accelerator is drawn in the
// accelerator colour — that is, whether the chord actually reaches it.
//
// A focused bar with no menu down shows every accelerator lit regardless: the
// bare letters it answers to are ordinary typing, not chords, so nothing can
// have claimed them.
func (m *MenuBar) ShouldShowAccelerator(menu *Menu) bool {
	a := m.acceleratorAssignmentFor(menu)
	if a.Char == 0 {
		return false
	}
	if m.acceleratorsActive {
		return true
	}
	return a.Active
}

// ShouldUnderlineAccelerator reports whether a menu's letter is marked at all.
//
// It is false only when an earlier sibling took every letter this menu offered
// — that letter is not this menu's to advertise, and the sibling is showing it
// lit on the same bar. A letter claimed by something in the CONTEXT is still
// this menu's, so it stays marked and merely stops being coloured, and it
// starts answering again on its own when the claim goes away.
func (m *MenuBar) ShouldUnderlineAccelerator(menu *Menu) bool {
	return m.acceleratorAssignmentFor(menu).Char != 0
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
	if m.keyResolver != nil && menu != nil {
		menu.SetKeyResolver(m.keyResolver)
	}
	m.InvalidateAccelerators()
}

// SetKeyResolver installs what every item on this bar asks when it needs to
// know which key means its command right now (see Menu.SetKeyResolver). It
// reaches the menus the bar has and the ones it is given later, which matters
// because a bar is recomposed from scratch whenever its app's menus change.
func (m *MenuBar) SetKeyResolver(fn func(command string) string) {
	m.keyResolver = fn
	for _, menu := range m.menus {
		if menu != nil {
			menu.SetKeyResolver(fn)
		}
	}
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
	m.InvalidateAccelerators()
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

// ActivateCommand triggers the item on this bar that names the given command,
// and reports whether one did. The command is one the caller ALREADY resolved:
// a key is fed to a context once per keystroke, so this takes the answer
// rather than asking again -- feeding it twice would advance a chord's prefix
// twice and lose the chord.
func (m *MenuBar) ActivateCommand(command string) bool {
	if m == nil || command == "" {
		return false
	}
	for _, menu := range m.menus {
		if menuActivateCommand(menu, command) {
			return true
		}
	}
	return false
}

// menuActivateCommand looks through a menu and its submenus for an available
// item naming the command, and triggers the first it finds. Trigger routes
// through the command registry exactly as a click does, so a key and a click
// are the same act.
func menuActivateCommand(menu *Menu, command string) bool {
	if menu == nil || command == "" {
		return false
	}
	for _, item := range menu.Items() {
		if item == nil || item.Separator || !item.Enabled {
			continue
		}
		if item.Command == command {
			item.Trigger()
			return true
		}
		if item.SubMenu != nil && menuActivateCommand(item.SubMenu, command) {
			return true
		}
	}
	return false
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
		if item.Shortcut != "" && core.SameKey(string(item.Shortcut), event.Key) {
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
	// Before anything measures it: the handler may change what is in it.
	m.activeMenu.prepareToShow()
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
	itemX := m.calculateMenuX(index)
	itemWidth := m.menuTitleWidth(m.menus[index].title)
	y := m.menuMetrics().RowH

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
// leftInset is the small left indent applied to the menu items on graphical
// surfaces: the window frame's own border thickness. It exists so the
// outline stroke drawn around the active item has its left edge clear of the
// frame beside it. Clicks anywhere in this indent still activate the first
// item (Fitts's law - see HandleMousePress), so nothing on the left edge is
// dead. Zero on cell surfaces, where there is no stroke and a sub-cell indent
// cannot render.
//
// The FRAME's thickness, and so untouched by core.MenuScale. What the indent
// clears is the frame, which does not shrink because the menus inside it do:
// scaled down with them, it stopped clearing anything and the first item's
// stroke was clipped by the window edge. It is a quarter of a cell at the
// default 2-pixel border, which is what it has always been.
func (m *MenuBar) leftInset() core.Unit {
	if !m.graphicalHere() {
		return 0
	}
	inset, _ := core.FindFrameBorderUnitsIn(m.Self(), m.EffectiveCellMetrics())
	return inset
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
	width := core.Unit(0)
	for _, menu := range m.menus {
		width += m.menuTitleWidth(menu.title)
	}

	return core.UnitSize{
		Width:  width,
		Height: m.menuMetrics().RowH,
	}
}

// menuTitleWidth returns the width of a menu title including surrounding spaces.
func (m *MenuBar) menuTitleWidth(title string) core.Unit {
	mm := m.menuMetrics()
	// Menu width: space (1 cell) + title (font) + space (1 cell).
	//
	// The pad is a cell, which is a fixed physical size at a given zoom, so
	// it needs no adjusting. The title is proportional text, and MeasureText
	// counts it in THIS bar's denomination -- Font.MeasureText answers at the
	// default one, which is a different currency the moment a window carries
	// an override, and the bar came out stretched by exactly that difference.
	return mm.CellW*2 + mm.TextWidth(title)
}

// elidedTitlePrefix returns how many leading runes of a title fit within
// budget, measured in the bar's font — proportional on graphical
// surfaces, cell-width on terminals. The elided (last partially visible)
// title must measure the same way it renders, or the prefix is cut at
// the wrong glyph.
func elidedTitlePrefix(font *core.Font, metrics core.CellMetrics, title []rune, budget core.Unit) int {
	visible := 0
	for visible < len(title) &&
		font.MeasureTextIn(string(title[:visible+1]), metrics) <= budget {
		visible++
	}
	return visible
}

// ellipsisText is the overflow marker (three periods, not the unicode
// glyph), and ellipsisWidth its width in the menu bar's proportional
// font - so it measures and renders the same as the menu titles.
const ellipsisText = "..."

func (m *MenuBar) ellipsisWidth() core.Unit {
	return m.menuMetrics().TextWidth(ellipsisText)
}

// drawEllipsis paints the overflow marker in the menu bar's proportional
// font at (x, 0) and returns its width.
func (m *MenuBar) drawEllipsis(p *core.Painter, x core.Unit, s style.CellStyle) core.Unit {
	mm := m.menuMetrics()
	p.DrawText(x, mm.YOff, ellipsisText, s, mm.Font)
	return mm.TextWidth(ellipsisText)
}

// Paint renders the menu bar (without dropdown - use PaintDropdown for that).
func (m *MenuBar) Paint(p *core.Painter) {
	bounds := m.Bounds()
	scheme := m.GetScheme()

	// Remember the surface kind for measurement paths (dateTimeWidth has
	// no painter of its own). Set before the kit resolves, since a cell
	// surface pins the menu scale to 1.0.
	m.graphicalCached = p.Graphical()

	mm := m.menuMetrics()
	metrics := m.EffectiveCellMetrics()
	font := mm.Font

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
			if m.hoverScrollBtn == -1 && m.graphicalCached {
				leftStyle = scheme.GetHoveredMenuBarButton()
			}
			mm.DrawGlyph(p, leftButtonX, 0, '[', leftStyle)
			mm.DrawGlyph(p, leftButtonX+mm.CellW, 0, '<', leftStyle)
			mm.DrawGlyph(p, leftButtonX+2*mm.CellW, 0, ']', leftStyle)
		} else {
			mm.DrawGlyph(p, leftButtonX, 0, ' ', inactiveButtonStyle)
			mm.DrawGlyph(p, leftButtonX+mm.CellW, 0, '<', inactiveButtonStyle)
			mm.DrawGlyph(p, leftButtonX+2*mm.CellW, 0, ' ', inactiveButtonStyle)
		}

		// Draw right button: [>] when active, " > " when inactive
		rightButtonX := leftButtonX + 3*mm.CellW
		if m.canScrollRight() {
			rightStyle := activeButtonStyle
			if m.hoverScrollBtn == 1 && m.graphicalCached {
				rightStyle = scheme.GetHoveredMenuBarButton()
			}
			mm.DrawGlyph(p, rightButtonX, 0, '[', rightStyle)
			mm.DrawGlyph(p, rightButtonX+mm.CellW, 0, '>', rightStyle)
			mm.DrawGlyph(p, rightButtonX+2*mm.CellW, 0, ']', rightStyle)
		} else {
			mm.DrawGlyph(p, rightButtonX, 0, ' ', inactiveButtonStyle)
			mm.DrawGlyph(p, rightButtonX+mm.CellW, 0, '>', inactiveButtonStyle)
			mm.DrawGlyph(p, rightButtonX+2*mm.CellW, 0, ' ', inactiveButtonStyle)
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
			} else if i == m.hoverIndex && m.graphicalCached {
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
					Height: mm.RowH,
				}, ' ', s)

				// Draw title with accelerator highlighting using font-aware rendering
				textX := x + mm.CellW
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
					drawTextSegments(p, textX, mm.YOff, font, metrics, segs...)
				} else {
					p.DrawText(textX, mm.YOff, menu.title, s, font)
				}

				// Draw the ellipsis after the menu (in normal style); the
				// painter clips it to the bar's bounds.
				m.drawEllipsis(p, x+menuWidth, menuBarStyle)
			} else {
				// Not selected, or not enough room for the full menu: show the
				// longest title prefix that fits, then "...". Fit and paint
				// both use the bar's proportional font — the old cell-by-cell
				// path measured AND drew the elided title monospace, visibly
				// different from every other title.
				ellipsisWidth := m.ellipsisWidth() // "..."
				titleRunes := []rune(menu.title)

				// Budget for title characters: leading space + prefix + "..."
				visible := elidedTitlePrefix(font, metrics, titleRunes,
					remainingWidth-mm.CellW-ellipsisWidth)

				if visible > 0 {
					// Draw space before text
					mm.DrawGlyph(p, x, 0, ' ', s)
					textX := x + mm.CellW

					// Accelerator highlighting splits the prefix into segments.
					var segs []textSegment
					if showAccel && menu.acceleratorPos >= 0 && menu.acceleratorPos < visible {
						if menu.acceleratorPos > 0 {
							segs = append(segs, textSegment{string(titleRunes[:menu.acceleratorPos]), s})
						}
						segs = append(segs, textSegment{string(titleRunes[menu.acceleratorPos]), accelStyle})
						if menu.acceleratorPos < visible-1 {
							segs = append(segs, textSegment{string(titleRunes[menu.acceleratorPos+1 : visible]), s})
						}
					} else {
						segs = []textSegment{{string(titleRunes[:visible]), s}}
					}
					// The ellipsis rides in the SAME run as the prefix, in the
					// menu style (never the accelerator colour). Placed by
					// drawTextSegments' returned advance instead, it landed
					// where the UNIT measurement said the text ended while the
					// text had been drawn to where the PIXEL advance put it,
					// and the bar showed through the difference.
					segs = append(segs, textSegment{ellipsisText, s})
					drawTextSegments(p, textX, mm.YOff, font, metrics, segs...)
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
		} else if i == m.hoverIndex && m.graphicalCached {
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
			Height: mm.RowH,
		}, ' ', s)

		// Draw title with accelerator highlighting using font-aware rendering
		textX := x + mm.CellW // Start after leading space
		showAccel := m.ShouldShowAccelerator(menu)

		// Draw text in parts: before accel, accel char, after accel. A letter
		// the chord no longer reaches keeps its underline in the ordinary text
		// style, so the user can see whose it is and that it is not answering.
		markAccel := showAccel || m.ShouldUnderlineAccelerator(menu)
		if !showAccel {
			accelStyle = s.Underline()
		}
		titleRunes := []rune(menu.title)
		if markAccel && menu.acceleratorPos >= 0 && menu.acceleratorPos < len(titleRunes) {
			var segs []textSegment
			if menu.acceleratorPos > 0 {
				segs = append(segs, textSegment{string(titleRunes[:menu.acceleratorPos]), s})
			}
			segs = append(segs, textSegment{string(titleRunes[menu.acceleratorPos]), accelStyle})
			if menu.acceleratorPos < len(titleRunes)-1 {
				segs = append(segs, textSegment{string(titleRunes[menu.acceleratorPos+1:]), s})
			}
			drawTextSegments(p, textX, mm.YOff, font, metrics, segs...)
		} else {
			// No accelerator - draw entire text
			p.DrawText(textX, mm.YOff, menu.title, s, font)
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
			Height: mm.RowH,
		}, ' ', dateTimeStyle)

		if f := m.dateTimeFont(); f != nil {
			p.DrawText(dateTimeX, mm.GlyphYOff(f), dateTimeStr, dateTimeStyle, f)
		} else {
			for i, ch := range dateTimeStr {
				mm.DrawGlyph(p, dateTimeX+core.Unit(i)*mm.CellW, 0, ch, dateTimeStyle)
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
			Height: mm.RowH,
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

// ActiveMenuTitleBounds returns the bar-row rect of the open menu's
// title — the dropdown's anchor. The compositor unions it into the
// dropdown's shadow so title and menu cast one shape. Zero rect when no
// menu is open.
func (m *MenuBar) ActiveMenuTitleBounds() core.UnitRect {
	if m.activeMenu == nil || m.currentIndex < 0 || m.currentIndex >= len(m.menus) {
		return core.UnitRect{}
	}
	return core.UnitRect{
		X:      m.calculateMenuX(m.currentIndex),
		Y:      0,
		Width:  m.menuTitleWidth(m.menus[m.currentIndex].title),
		Height: m.menuMetrics().RowH,
	}
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

	// Resolved once: the accelerator blocks below run after this switch and
	// must not re-feed the same keystroke to the sequence processor.
	cmd := m.KeyCommand(event.Key)

	switch cmd {
	case core.CmdTrinketItemLeft, core.CmdTrinketItemPrior:
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

	case core.CmdTrinketItemRight, core.CmdTrinketItemNext:
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

	case core.CmdTrinketActivate, core.CmdTrinketItemDown:
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

	case core.CmdTrinketCancel:
		if m.activeMenu != nil {
			// First escape: close menu but keep menu bar focused
			m.CloseMenu()
		} else {
			// Second escape: unfocus menu bar
			m.CloseMenuAndUnfocus()
		}
		return true

	case core.CmdAppMenu:
		m.ToggleMenuFocus()
		return true

	case core.CmdFocusNext, core.CmdFocusPrior:
		// Tab and Shift+Tab both leave the bar, but where to depends on whose
		// bar it is. The desktop's bar hands either direction to the dock (the
		// only other chrome out there). A window's own bar - which a detached,
		// solo, or torn-off window carries, and which has no dock beside it -
		// steps into that window's chain instead: back to the title bar,
		// forward into the content. Without the second case both keys fell
		// through to the focused trinket, and a full-screen one (the mew
		// editor consumes every key) swallowed them, leaving the bar a focus
		// trap you could enter with F10 but not Tab out of.
		if m.activeMenu != nil {
			break // a dropdown is open: Tab stays inside it
		}
		if m.onFocusDock != nil {
			m.onFocusDock(cmd == core.CmdFocusNext)
			return true
		}
		if m.onFocusOut != nil {
			if m.onFocusOut(cmd == core.CmdFocusNext) {
				m.CloseMenuWithoutRestore()
				return true
			}
		}
	}

	// A formed chord accelerator. The display has always deferred to a clash;
	// the dispatch did not, so a shadowed accelerator still fired and, being
	// resolved above the active window, beat the very binding it was supposed
	// to be yielding to. Both sides read the same assignment now, so an
	// accelerator that is not lit does not answer.
	if m.acceleratorChord != "" {
		m.refreshAccelerators()
		for i := range m.menus {
			a := m.accelAssignments[i]
			if !a.Active || a.Char == 0 {
				continue
			}
			if formAcceleratorKey(m.acceleratorChord, a.Char) == event.Key {
				m.SetFocus()
				m.OpenMenu(i)
				return true
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
	bounds := m.Bounds()

	// Check active menu first - if clicking on an item in the dropdown
	if m.activeMenu != nil && !m.mouseDown {
		if m.activeMenu.HandleMousePress(event) {
			return true
		}
	}

	// Check if click is in menu bar
	if event.Y < m.menuMetrics().RowH {
		// Check for scroll button clicks if scrolling is needed
		needsScrolling := m.menusNeedScrolling()
		if needsScrolling {
			buttonWidth := m.scrollButtonWidth()
			dateTimeX := bounds.Width - m.dateTimeWidth()
			leftButtonX := dateTimeX - buttonWidth*2

			// Check [<] button
			if event.X >= leftButtonX && event.X < leftButtonX+buttonWidth {
				if m.canScrollLeft() {
					m.scrollOffset--
					m.Update()
				}
				return true
			}

			// Check [>] button
			rightButtonX := leftButtonX + buttonWidth
			if event.X >= rightButtonX && event.X < rightButtonX+buttonWidth {
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
		// ellipsis when scrolled). Titles overflowing beneath the scroll
		// buttons or the clock are not clickable there.
		x := m.leftInset()
		if m.scrollOffset > 0 {
			x += m.ellipsisWidth() // "..."
		}

		for i := m.scrollOffset; event.X < m.menusRightLimit() && i < len(m.menus); i++ {
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

// OpenHelpMenu drops the Help menu open with its first available item
// highlighted, and reports whether there was one to open.
//
// Help is found by its well-known role rather than by its title, so it is
// still Help in a localised menu bar. Everything else is what the keyboard
// already does: the menu is scrolled into view, opened, and stepped into once,
// which is the menu key followed by Down.
func (m *MenuBar) OpenHelpMenu() bool {
	for i, menu := range m.menus {
		if menu == nil || menu.WellKnownID() != MenuIDHelp {
			continue
		}
		m.SetFocus()
		m.ensureMenuVisible(i)
		m.OpenMenu(i)
		if m.activeMenu != nil {
			m.activeMenu.SelectFirstItem()
		}
		return true
	}
	return false
}

// SetOnFocusChanged installs an observer told each time the bar takes or gives
// up the keyboard.
func (m *MenuBar) SetOnFocusChanged(fn func(focused bool)) {
	m.onFocusChanged = fn
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
	if m.onFocusChanged != nil {
		m.onFocusChanged(true)
	}
	m.Update()
}

// HandleFocusOut is called when focus is lost.
func (m *MenuBar) HandleFocusOut() {
	m.CloseMenu()
	m.dragging = false
	m.currentIndex = -1
	m.acceleratorsActive = false
	if m.onFocusChanged != nil {
		m.onFocusChanged(false)
	}
	m.Update()
}

// menuItemAt maps a pointer position to the top-level menu index under
// it, or -1 when the pointer is not over a menu title within the bar row.
func (m *MenuBar) menuItemAt(px, py core.Unit) int {
	if py < 0 || py >= m.menuMetrics().RowH {
		return -1
	}
	if px >= m.menusRightLimit() {
		// Over the scroll buttons or the clock, even if a title
		// overflows beneath them.
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
	if py < 0 || py >= m.menuMetrics().RowH {
		return 0
	}
	bounds := m.Bounds()
	buttonWidth := m.scrollButtonWidth()
	dateTimeX := bounds.Width - m.dateTimeWidth()
	leftButtonX := dateTimeX - buttonWidth*2
	if px >= leftButtonX && px < leftButtonX+buttonWidth {
		return -1
	}
	rightButtonX := leftButtonX + buttonWidth
	if px >= rightButtonX && px < rightButtonX+buttonWidth {
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
		// button held.
		//
		// GRAPHICAL SURFACES ONLY. A cell surface has no pointer travel to
		// speak of: the position report that arrives before a click is the
		// only "move" there is, so opening on it means the click that follows
		// lands on a menu that is ALREADY open — and HandleMousePress reads
		// that as the toggle it is, closing the menu the click was meant to
		// open. What the user sees is a menu that refuses to open and a
		// highlight where the dropdown should be.
		if m.graphicalCached && m.hoverIndex >= 0 && m.hoverIndex < len(m.menus) &&
			m.menus[m.hoverIndex] != m.activeMenu {
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
		// Only start dragging if moved at least half a cell. A pointer
		// distance, not menu content, so it stays on the surface's own cell
		// rather than following core.MenuScale.
		if dx >= metrics.UnitsPerCellWidth/2 || dy >= metrics.UnitsPerCellHeight/2 {
			m.dragging = true
		} else {
			return true // Not dragging yet, consume but don't act
		}
	}

	// Check if mouse is in menu bar - switch menus and deselect dropdown item
	if event.Y < m.menuMetrics().RowH {
		// Deselect current item in dropdown since we're back on the menu bar
		if m.activeMenu != nil && m.activeMenu.currentIndex != -1 {
			m.activeMenu.currentIndex = -1
			m.activeMenu.Update()
		}

		// Find which menu the mouse is over (past the left indent, and the
		// ellipsis when scrolled). A drag across the scroll buttons or the
		// clock must not open the title hidden beneath them.
		x := m.leftInset()
		if m.scrollOffset > 0 {
			x += m.ellipsisWidth() // "..."
		}

		for i := m.scrollOffset; event.X < m.menusRightLimit() && i < len(m.menus); i++ {
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

// ActivateAcceleratorSequence opens the menu whose formed accelerator is this
// sequence, and reports whether one matched.
//
// The sequence is compared whole, so the mnemonic's position within the chord
// does not matter: a pattern of "^X * Return" forms "^X h Return" for &Help and
// the same substitution identifies it coming back. Only a live accelerator
// answers — a muted one is not published and is not matched here either.
func (m *MenuBar) ActivateAcceleratorSequence(seq string) bool {
	if seq == "" || m.acceleratorChord == "" {
		return false
	}
	m.refreshAccelerators()
	for i := range m.menus {
		a := m.accelAssignments[i]
		if !a.Active || a.Char == 0 {
			continue
		}
		if formAcceleratorKey(m.acceleratorChord, a.Char) == seq {
			m.SetFocus()
			m.OpenMenu(i)
			return true
		}
	}
	return false
}
