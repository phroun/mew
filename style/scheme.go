// Package style provides theming and visual styling for KittyTK.
package style

import "sync"

// Scheme defines a color scheme that can be applied to trinkets.
// A Theme can contain multiple Schemes (e.g., default, modal dialogs, toolbars).
// Each color field is a pointer; nil means use the fallback defined in scheme-plan.txt.
type Scheme struct {
	// =========================================================================
	// Environment Related Colors
	// =========================================================================

	DesktopFill       *CellStyle
	StatusBar         *CellStyle
	StatusBarShortcut *CellStyle
	Dock              *CellStyle
	DockItem          *CellStyle
	FocusedDockItem   *CellStyle
	HoveredDockItem   *CellStyle // nil = HoverBG + HoverFG

	// =========================================================================
	// Window Frame Related Colors
	// =========================================================================

	ActiveWindowBorder     *CellStyle
	InactiveWindowBorder   *CellStyle
	ActiveWindowTitle      *CellStyle // currently bright and bold white on blue
	InactiveWindowTitle    *CellStyle
	ActiveTitleBarButton   *CellStyle
	InactiveTitleBarButton *CellStyle
	FocusedTitleBarButton  *CellStyle
	HoveredTitleBarButton  *CellStyle // nil = HoverBG + HoverFG
	PressedTitleBarButton  *CellStyle
	InactiveWindowFG       *CellStyle // FG only
	ActiveWindowFG         *CellStyle // FG only
	InactiveWindowBG       *CellStyle // BG only
	ActiveWindowBG         *CellStyle // BG only

	// =========================================================================
	// State Related Colors (used as defaults for many other scheme values)
	// =========================================================================

	FocusBG        *CellStyle // normal cyan (BG only, used as default for many)
	FocusFG        *CellStyle // normal black (FG only, used as default for many)
	FocusTextFG    *CellStyle // bright cyan (FG only, used as default for many)
	HoverBG        *CellStyle // purple (BG only, used as default for many Hovered* values)
	HoverFG        *CellStyle // bright yellow (FG only, used as default for many Hovered* values)
	HoverTextFG    *CellStyle // bright yellow (FG only, hover analog of FocusTextFG for text-only highlights)
	DisabledTextFG *CellStyle // FG only
	Selection      *CellStyle // bright white on blue
	Normal         *CellStyle // nil = WindowFG on WindowBG
	LedgerOdd      *CellStyle // ledger-banded odd rows; nil = cyan on very dark gray
	LedgerEven     *CellStyle // ledger-banded even rows; nil = white (silver) on very dark teal
	Header         *CellStyle // column header band; nil = silver on dark yellow (amber)

	// =========================================================================
	// Menu Related Colors
	// =========================================================================

	MenuBar                *CellStyle
	MenuBarMeta            *CellStyle // accelerator keys on menu bar
	MenuBarInfo            *CellStyle // clock, etc.
	HoveredMenuBar         *CellStyle // menu bar item under the pointer; nil = HoverBG + HoverFG
	HoveredMenuBarMeta     *CellStyle // accelerator on hovered menu bar item; nil = MenuBarMeta FG on HoverBG
	ActiveMenuBarItem      *CellStyle
	ActiveMenuBarMeta      *CellStyle // accelerator keys on active menu
	FocusedMenuBarItem     *CellStyle // menu bar has focus but dropdown not shown, or dropdown has no selection
	FocusedMenuBarMeta     *CellStyle // accelerator keys on focused menu bar item
	MenuGutter             *CellStyle
	MenuCheckIcon          *CellStyle
	MenuRadioIcon          *CellStyle
	MenuItemText           *CellStyle
	MenuAccelerator        *CellStyle
	MenuShortcut           *CellStyle
	MenuSeparator          *CellStyle
	MenuSeparatorGutter    *CellStyle
	MenuBarButton          *CellStyle // for [<][>] scroll buttons
	HoveredMenuBarButton   *CellStyle // nil = HoverBG + HoverFG
	DisabledMenuBarButton  *CellStyle
	DisabledMenuGutter     *CellStyle
	DisabledMenuItem       *CellStyle // applies to Text, Shortcut and Accelerator
	DisabledMenuIcon       *CellStyle // applies to Check and Radio
	FocusedMenuCheckIcon   *CellStyle
	FocusedMenuRadioIcon   *CellStyle
	FocusedMenuItemText    *CellStyle
	FocusedMenuAccelerator *CellStyle
	FocusedMenuShortcut    *CellStyle

	// =========================================================================
	// Trinket Group Colors (used as defaults for other things)
	// =========================================================================

	TrinketGroupFG   *CellStyle // bright white (FG only)
	TrinketGroupBG   *CellStyle // black (BG only)
	TrinketContentFG *CellStyle // regular white (FG only)
	TrinketContentBG *CellStyle // black (BG only)

	// =========================================================================
	// Basic Trinket Related Colors
	// =========================================================================

	// Label
	LabelFG         *CellStyle // nil = WindowFG
	LabelBG         *CellStyle // nil = inherit
	DisabledLabelFG *CellStyle // nil = DisabledTextFG
	DisabledLabelBG *CellStyle // nil = inherit

	// Button
	Button                    *CellStyle // black on regular white
	DisabledButtonFG          *CellStyle // nil = DisabledTextFG
	DisabledButtonBG          *CellStyle // nil = inherit
	FocusedButton             *CellStyle // nil = FocusBG + FocusFG
	HoveredButton             *CellStyle // nil = HoverBG + HoverFG
	PressedButton             *CellStyle // nil = WindowBG + WindowFG
	DefaultPaneButtonShadowFG *CellStyle // dim blue, used when inherited BG is ansi 49 (FG only)
	DarkPaneButtonShadowFG    *CellStyle // dark gray, used when inherited BG is black (FG only)
	ButtonShadowFG            *CellStyle // black, used in all other cases (FG only)

	// EditBox / TextInput
	EditBox                       *CellStyle // regular white on black
	DefaultPaneEditBox            *CellStyle // regular white on dark blue, when inherited BG is ansi 49
	DarkPaneEditBox               *CellStyle // regular white on dark blue, when inherited BG is black
	EditBoxPlaceholder            *CellStyle
	DefaultPaneEditBoxPlaceholder *CellStyle // nil = EditBoxPlaceholder
	DarkPaneEditBoxPlaceholder    *CellStyle // nil = EditBoxPlaceholder
	FocusedEditBoxText            *CellStyle // black on dark cyan
	FocusedEditBoxCursor          *CellStyle // black on white (cell block cursor)
	FocusedEditBoxBarCursor       *CellStyle // bright white (graphical bar caret)
	FocusedEditBoxFill            *CellStyle // white on cyan
	// Selection inside an edit box: the focused pair, the resting
	// (unfocused) pair, and the resting pair on a dark pane.
	FocusedEditBoxSelectionFG         *Color // black
	FocusedEditBoxSelectionBG         *Color // silver
	RestingEditBoxSelectionFG         *Color // silver
	RestingEditBoxSelectionBG         *Color // black
	DarkPaneRestingEditBoxSelectionFG *Color // black
	DarkPaneRestingEditBoxSelectionBG *Color // cyan

	// ComboBox
	ComboBox                *CellStyle // nil = same as EditBox
	ComboBoxArrow           *CellStyle // nil = same as EditBox
	DisabledComboBoxFG      *CellStyle // nil = DisabledButtonFG
	DisabledComboBoxArrow   *CellStyle // nil = DisabledComboBoxFG + DisabledComboBoxBG
	DisabledComboBoxBG      *CellStyle // nil = DisabledButtonBG
	FocusedComboBox         *CellStyle // nil = FocusedButton
	FocusedComboBoxArrow    *CellStyle // nil = FocusedComboBox
	PressedComboBox         *CellStyle // nil = PressedButton
	PressedComboBoxArrow    *CellStyle // nil = PressedComboBox
	DropdownItemText        *CellStyle // nil = MenuItemText
	FocusedDropdownItemText *CellStyle // nil = FocusedMenuItemText
	DropdownSeparator       *CellStyle // nil = MenuSeparator
	DisabledDropdownItem    *CellStyle // nil = DisabledMenuItem
	DropdownScrollbar       *CellStyle // nil = DropdownItemText
	DropdownScrollbarThumb  *CellStyle // nil = black on white (like PressedSplitter)

	// CheckBox
	CheckBoxFG             *CellStyle // nil = same as LabelFG
	CheckBoxBG             *CellStyle // nil = inherit
	CheckBoxLabelFG        *CellStyle // nil = same as LabelFG
	FocusedCheckBoxFG      *CellStyle // nil = FocusTextFG
	FocusedCheckBoxBG      *CellStyle // nil = inherit
	FocusedCheckBoxLabelFG *CellStyle // nil = FocusTextFG

	// RadioButton
	RadioButtonFG             *CellStyle // nil = same as LabelFG
	RadioButtonBG             *CellStyle // nil = inherit
	RadioButtonLabelFG        *CellStyle // nil = same as LabelFG
	FocusedRadioButtonFG      *CellStyle // nil = FocusTextFG
	FocusedRadioButtonBG      *CellStyle // nil = inherit
	FocusedRadioButtonLabelFG *CellStyle // nil = FocusTextFG

	// =========================================================================
	// Splitter Related Colors
	// =========================================================================

	Splitter              *CellStyle // dark gray on black
	SplitterHandle        *CellStyle // nil = Splitter
	SplitterTitle         *CellStyle // nil = Splitter
	FocusedSplitter       *CellStyle // nil = FocusBG + FocusFG
	FocusedSplitterHandle *CellStyle // nil = FocusedSplitter
	FocusedSplitterTitle  *CellStyle // nil = FocusedSplitter
	HoveredSplitterHandle *CellStyle // nil = HoverBG + HoverFG
	HoveredSplitterTitle  *CellStyle // nil = HoverBG + HoverFG
	PressedSplitter       *CellStyle // nil = WindowBG + WindowFG (black on white)
	PressedSplitterHandle *CellStyle // nil = PressedSplitter
	PressedSplitterTitle  *CellStyle // nil = PressedSplitter

	// =========================================================================
	// Tab Trinket (Page Control) Related Colors
	// =========================================================================

	TabsFG *CellStyle // nil = WindowFG, currently bright white
	TabsBG *CellStyle // nil = inherit
	// TabsButton and DisabledTabsButton stay split into FG/BG because their
	// background inherits the surrounding pane colour (inheritedBG); the
	// foreground can be themed while the background follows context.
	TabsButtonFG          *CellStyle // nil = TabsFG (FG only)
	TabsButtonBG          *CellStyle // nil = inheritedBG (BG only)
	DisabledTabsButtonFG  *CellStyle // nil = DisabledTextFG (FG only)
	DisabledTabsButtonBG  *CellStyle // nil = inheritedBG (BG only)
	HoveredTabsButton     *CellStyle // nil = HoverBG + HoverFG
	PressedTabsButton     *CellStyle // nil = black on white
	ActiveTabFG           *CellStyle // currently bright and bold yellow, nil = TrinketGroupFG
	ActiveTabBG           *CellStyle // nil = TrinketGroupBG, currently ansi 49
	FocusedTab            *CellStyle // nil = FocusBG + FocusFG
	TabCloseButton        *CellStyle // close button on tabs
	FocusedTabCloseButton *CellStyle
	PressedTabCloseButton *CellStyle

	// =========================================================================
	// List Related Colors (TreeView, ListView)
	// =========================================================================

	ListBG            *CellStyle // nil = TrinketContentBG
	ListFG            *CellStyle // nil = TrinketContentFG
	FocusedListBG     *CellStyle // nil = ListBG
	FocusedListFG     *CellStyle // nil = ListFG
	SelectedListItem  *CellStyle // nil = Selection
	FocusedListRow    *CellStyle // nil = bright yellow on dark gray
	FocusedListItem   *CellStyle // nil = FocusBG + FocusFG
	FocusedListButton *CellStyle // list-borne controls (header/chooser); nil = FocusedButton

	// =========================================================================
	// Scrollbar Colors (ScrollArea, ListView, TreeView)
	// =========================================================================

	Scrollbar             *CellStyle // dark gray on black
	ScrollbarThumb        *CellStyle // regular white on black
	HoveredScrollbarThumb *CellStyle // nil = HoverBG + HoverFG

	// =========================================================================
	// ProgressBar Colors
	// =========================================================================

	ProgressFull      *CellStyle // bright green on dim green
	ProgressEmpty     *CellStyle // dim green on black
	ProgressFullText  *CellStyle // black on dim green
	ProgressEmptyText *CellStyle // bright yellow on black

	// =========================================================================
	// GroupBox / FramedPane Related Colors
	// =========================================================================

	GroupBoxBorder        *CellStyle // nil = WindowFG on inherit
	GroupBoxTitle         *CellStyle // nil = WindowFG on inherit
	FocusedGroupBoxBorder *CellStyle // nil = FocusTextFG on inherit
	FocusedGroupBoxTitle  *CellStyle // nil = FocusTextFG on inherit
}

// SchemeID represents a scheme identifier.
type SchemeID int

const (
	// SchemeInherit means the trinket inherits its scheme from its container.
	SchemeInherit SchemeID = -1
	// SchemeDefault is the default scheme (index 0).
	SchemeDefault SchemeID = 0
)

// SchemeRegistry manages the collection of available schemes.
type SchemeRegistry struct {
	mu      sync.RWMutex
	schemes map[SchemeID]*Scheme
}

// globalRegistry is the default scheme registry.
var globalRegistry = &SchemeRegistry{
	schemes: make(map[SchemeID]*Scheme),
}

// init initializes the default scheme (scheme 0).
func init() {
	globalRegistry.Register(SchemeDefault, DefaultScheme())
}

// GlobalSchemeRegistry returns the global scheme registry.
func GlobalSchemeRegistry() *SchemeRegistry {
	return globalRegistry
}

// Register adds or updates a scheme in the registry.
func (r *SchemeRegistry) Register(id SchemeID, scheme *Scheme) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.schemes[id] = scheme
}

// Get retrieves a scheme by ID. Returns the default scheme if not found.
func (r *SchemeRegistry) Get(id SchemeID) *Scheme {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if scheme, ok := r.schemes[id]; ok {
		return scheme
	}
	// Fall back to default scheme for invalid IDs
	if scheme, ok := r.schemes[SchemeDefault]; ok {
		return scheme
	}
	// Last resort: return an empty scheme
	return &Scheme{}
}

// Has checks if a scheme exists in the registry.
func (r *SchemeRegistry) Has(id SchemeID) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.schemes[id]
	return ok
}

// ptr is a helper to create a pointer to a CellStyle.
func ptr(s CellStyle) *CellStyle {
	return &s
}

// colorPtr is a helper to create a pointer to a Color.
func colorPtr(c Color) *Color {
	return &c
}

// orColor resolves an optional scheme color against its default.
func orColor(c *Color, def Color) Color {
	if c != nil {
		return *c
	}
	return def
}

// DefaultScheme creates the default scheme (scheme 0) with current hard-coded values.
func DefaultScheme() *Scheme {
	return &Scheme{
		// Environment Related Colors
		DesktopFill:       ptr(DefaultStyle().WithFg(ColorBrightBlack).WithBg(ColorGreen).WithAttrs(StyleDim)),
		StatusBar:         ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite)),
		StatusBarShortcut: ptr(DefaultStyle().WithFg(ColorRed).WithBg(ColorWhite)),
		Dock:              ptr(DefaultStyle().WithFg(ColorBrightCyan).WithBg(ColorBlue)),
		DockItem:          ptr(DefaultStyle().WithFg(ColorBrightCyan).WithBg(ColorBlue)),
		FocusedDockItem:   ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorBrightCyan)),
		HoveredDockItem:   nil, // HoverBG + HoverFG

		// Window Frame Related Colors
		ActiveWindowBorder:   ptr(DefaultStyle().WithFg(ColorBrightCyan).WithBg(ColorBlue)),
		InactiveWindowBorder: ptr(DefaultStyle().WithFg(ColorBrightBlue).WithBg(ColorBlue)),
		ActiveWindowTitle:    ptr(DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlue).Bold()),
		InactiveWindowTitle:  ptr(DefaultStyle().WithFg(ColorWhite).WithBg(ColorBrightBlack).WithAttrs(StyleDim)),
		// No explicit background: each defaults to its matching (active or
		// inactive) title-bar background, so a button blends into the bar it
		// sits on unless a scheme overrides it (see GetTitleBarButton).
		ActiveTitleBarButton:   ptr(DefaultStyle().WithFg(ColorBrightCyan)),
		InactiveTitleBarButton: ptr(DefaultStyle().WithFg(ColorBrightBlue)),
		FocusedTitleBarButton:  ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorCyan)),
		HoveredTitleBarButton:  nil, // HoverBG + HoverFG
		PressedTitleBarButton:  ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite)),
		InactiveWindowFG:       ptr(DefaultStyle().WithFg(ColorWhite)),
		ActiveWindowFG:         ptr(DefaultStyle().WithFg(ColorWhite)),
		InactiveWindowBG:       ptr(DefaultStyle().WithBg(ColorBlack)), // distinct from active for testing
		ActiveWindowBG:         ptr(DefaultStyle().WithBg(ColorBlue)),

		// State Related Colors
		FocusBG:        ptr(DefaultStyle().WithBg(ColorCyan)),
		FocusFG:        ptr(DefaultStyle().WithFg(ColorBlack)),
		FocusTextFG:    ptr(DefaultStyle().WithFg(ColorBrightCyan)),
		HoverBG:        ptr(DefaultStyle().WithBg(ColorMagenta)),
		HoverFG:        ptr(DefaultStyle().WithFg(ColorBrightYellow)),
		HoverTextFG:    ptr(DefaultStyle().WithFg(ColorBrightYellow)),
		DisabledTextFG: ptr(DefaultStyle().WithFg(ColorBrightBlack)),
		Selection:      ptr(DefaultStyle().WithFg(ColorBrightWhite).WithBg(ColorBlue)),
		Normal:         nil, // nil = WindowFG on WindowBG

		// Menu Related Colors
		MenuBar:                ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite)),
		MenuBarMeta:            ptr(DefaultStyle().WithFg(ColorRed).WithBg(ColorWhite)),
		MenuBarInfo:            ptr(DefaultStyle().WithFg(ColorBrightYellow).WithBg(ColorYellow)),
		HoveredMenuBar:         nil, // HoverBG + HoverFG
		HoveredMenuBarMeta:     nil, // MenuBarMeta FG (red) on HoverBG
		ActiveMenuBarItem:      ptr(DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlue)),
		ActiveMenuBarMeta:      ptr(DefaultStyle().WithFg(ColorBrightCyan).WithBg(ColorBlue)),
		FocusedMenuBarItem:     ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorCyan)),
		FocusedMenuBarMeta:     ptr(DefaultStyle().WithFg(ColorRed).WithBg(ColorCyan)),
		MenuGutter:             ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite)),
		MenuCheckIcon:          ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite)),
		MenuRadioIcon:          ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite)),
		MenuItemText:           ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorBrightWhite)),
		MenuAccelerator:        ptr(DefaultStyle().WithFg(ColorRed).WithBg(ColorBrightWhite)),
		MenuShortcut:           ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorBrightWhite)),
		MenuSeparator:          ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorBrightWhite)),
		MenuSeparatorGutter:    ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite)),
		MenuBarButton:          ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite)),
		HoveredMenuBarButton:   nil, // HoverBG + HoverFG
		DisabledMenuBarButton:  ptr(DefaultStyle().WithFg(ColorBrightBlack).WithBg(ColorWhite)),
		DisabledMenuGutter:     ptr(DefaultStyle().WithFg(ColorBrightBlack).WithBg(ColorWhite)),
		DisabledMenuItem:       ptr(DefaultStyle().WithFg(ColorBrightBlack).WithBg(ColorWhite)),
		DisabledMenuIcon:       ptr(DefaultStyle().WithFg(ColorBrightBlack).WithBg(ColorWhite)),
		FocusedMenuCheckIcon:   ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorCyan)),
		FocusedMenuRadioIcon:   ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorCyan)),
		FocusedMenuItemText:    ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorCyan)),
		FocusedMenuAccelerator: ptr(DefaultStyle().WithFg(ColorRed).WithBg(ColorCyan)),
		FocusedMenuShortcut:    ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorCyan)),

		// Trinket Group Colors
		TrinketGroupFG:   ptr(DefaultStyle().WithFg(ColorBrightWhite)),
		TrinketGroupBG:   ptr(DefaultStyle().WithBg(ColorBlack)),
		TrinketContentFG: ptr(DefaultStyle().WithFg(ColorWhite)),
		TrinketContentBG: ptr(DefaultStyle().WithBg(ColorBlack)),

		// Label
		LabelFG:         nil, // WindowFG
		LabelBG:         nil, // inherit
		DisabledLabelFG: nil, // DisabledTextFG
		DisabledLabelBG: nil, // inherit

		// Button
		Button:                    ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite)),
		DisabledButtonFG:          nil, // DisabledTextFG
		DisabledButtonBG:          nil, // inherit
		FocusedButton:             ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorCyan)),
		HoveredButton:             nil, // HoverBG + HoverFG
		PressedButton:             nil, // WindowBG + WindowFG
		DefaultPaneButtonShadowFG: ptr(DefaultStyle().WithFg(ColorBlue).WithAttrs(StyleDim)),
		DarkPaneButtonShadowFG:    ptr(DefaultStyle().WithFg(ColorBrightBlack)),
		ButtonShadowFG:            ptr(DefaultStyle().WithFg(ColorBlack)),

		// EditBox / TextInput
		EditBox:                           ptr(DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlack)),
		DefaultPaneEditBox:                ptr(DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlue)),
		DarkPaneEditBox:                   ptr(DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlue)),
		EditBoxPlaceholder:                ptr(DefaultStyle().WithFg(ColorBrightBlack).WithBg(ColorBlack)),
		DefaultPaneEditBoxPlaceholder:     nil, // EditBoxPlaceholder
		DarkPaneEditBoxPlaceholder:        nil, // EditBoxPlaceholder
		FocusedEditBoxText:                ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorCyan)),
		FocusedEditBoxCursor:              ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite)),
		FocusedEditBoxBarCursor:           ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorBrightWhite)),
		FocusedEditBoxFill:                ptr(DefaultStyle().WithFg(ColorWhite).WithBg(ColorCyan)),
		FocusedEditBoxSelectionFG:         colorPtr(ColorBlack),
		FocusedEditBoxSelectionBG:         colorPtr(ColorWhite),
		RestingEditBoxSelectionFG:         colorPtr(ColorWhite),
		RestingEditBoxSelectionBG:         colorPtr(ColorBlack),
		DarkPaneRestingEditBoxSelectionFG: colorPtr(ColorBlack),
		DarkPaneRestingEditBoxSelectionBG: colorPtr(ColorCyan),

		// ComboBox
		ComboBox:                nil, // EditBox
		ComboBoxArrow:           nil, // EditBox
		DisabledComboBoxFG:      nil, // DisabledButtonFG
		DisabledComboBoxArrow:   nil, // DisabledComboBoxFG + DisabledComboBoxBG
		DisabledComboBoxBG:      nil, // DisabledButtonBG
		FocusedComboBox:         nil, // FocusedButton
		FocusedComboBoxArrow:    nil, // FocusedComboBox
		PressedComboBox:         nil, // PressedButton
		PressedComboBoxArrow:    nil, // PressedComboBox
		DropdownItemText:        nil, // MenuItemText
		FocusedDropdownItemText: nil, // FocusedMenuItemText
		DropdownSeparator:       nil, // MenuSeparator
		DisabledDropdownItem:    nil, // DisabledMenuItem
		DropdownScrollbar:       nil, // DropdownItemText
		DropdownScrollbarThumb:  nil, // black on white (like PressedSplitter)

		// CheckBox
		CheckBoxFG:             nil, // LabelFG
		CheckBoxBG:             nil, // inherit
		CheckBoxLabelFG:        nil, // LabelFG
		FocusedCheckBoxFG:      ptr(DefaultStyle().WithFg(ColorBrightCyan)),
		FocusedCheckBoxBG:      nil, // inherit
		FocusedCheckBoxLabelFG: ptr(DefaultStyle().WithFg(ColorBrightCyan)),

		// RadioButton
		RadioButtonFG:             nil, // LabelFG
		RadioButtonBG:             nil, // inherit
		RadioButtonLabelFG:        nil, // LabelFG
		FocusedRadioButtonFG:      ptr(DefaultStyle().WithFg(ColorBrightCyan)),
		FocusedRadioButtonBG:      nil, // inherit
		FocusedRadioButtonLabelFG: ptr(DefaultStyle().WithFg(ColorBrightCyan)),

		// Splitter
		Splitter:              ptr(DefaultStyle().WithFg(ColorBrightBlack).WithBg(ColorBlack)),
		SplitterHandle:        nil, // Splitter
		SplitterTitle:         nil, // Splitter
		FocusedSplitter:       ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorCyan)),
		FocusedSplitterHandle: nil, // FocusedSplitter
		FocusedSplitterTitle:  nil, // FocusedSplitter
		HoveredSplitterHandle: nil, // HoverBG + HoverFG
		HoveredSplitterTitle:  nil, // HoverBG + HoverFG
		PressedSplitter:       nil, // WindowBG + WindowFG (black on white)
		PressedSplitterHandle: nil, // PressedSplitter
		PressedSplitterTitle:  nil, // PressedSplitter

		// Tab Trinket
		TabsFG:                ptr(DefaultStyle().WithFg(ColorBrightWhite)),
		TabsBG:                ptr(DefaultStyle().WithBg(ColorBlue)), // blue tab bar background
		TabsButtonFG:          nil,                                   // TabsFG
		TabsButtonBG:          nil,                                   // inheritedBG
		DisabledTabsButtonFG:  nil,                                   // DisabledTextFG
		DisabledTabsButtonBG:  nil,                                   // inheritedBG
		HoveredTabsButton:     nil,                                   // HoverBG + HoverFG
		PressedTabsButton:     ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite)),
		ActiveTabFG:           ptr(DefaultStyle().WithFg(ColorBrightYellow).Bold()),
		ActiveTabBG:           ptr(DefaultStyle().WithBg(ColorDefault)), // ansi 49
		FocusedTab:            ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorCyan)),
		TabCloseButton:        nil, // TabsFG + TabsBG
		FocusedTabCloseButton: nil, // FocusedTab
		PressedTabCloseButton: nil, // PressedTabsButton

		// List
		ListBG:           ptr(DefaultStyle().WithBg(ColorBlack)),
		ListFG:           ptr(DefaultStyle().WithFg(ColorWhite)),
		FocusedListBG:    nil, // ListBG
		FocusedListFG:    nil, // ListFG
		SelectedListItem: ptr(DefaultStyle().WithFg(ColorBrightWhite).WithBg(ColorBlue)),
		FocusedListItem:  ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorCyan)),

		// Scrollbar
		Scrollbar:             ptr(DefaultStyle().WithFg(ColorBrightBlack).WithBg(ColorBlack)),
		ScrollbarThumb:        ptr(DefaultStyle().WithFg(ColorWhite).WithBg(ColorBlack)),
		HoveredScrollbarThumb: ptr(DefaultStyle().WithFg(ColorMagenta).WithBg(ColorBlack)), // dark magenta thumb

		// ProgressBar
		ProgressFull:      ptr(DefaultStyle().WithFg(ColorBrightGreen).WithBg(ColorGreen)),
		ProgressEmpty:     ptr(DefaultStyle().WithFg(ColorGreen).WithBg(ColorBlack)),
		ProgressFullText:  ptr(DefaultStyle().WithFg(ColorBlack).WithBg(ColorGreen)),
		ProgressEmptyText: ptr(DefaultStyle().WithFg(ColorBrightYellow).WithBg(ColorBlack)),

		// GroupBox / FramedPane
		GroupBoxBorder:        nil, // WindowFG on inherit
		GroupBoxTitle:         nil, // WindowFG on inherit
		FocusedGroupBoxBorder: nil, // FocusTextFG on inherit
		FocusedGroupBoxTitle:  nil, // FocusTextFG on inherit
	}
}

// =========================================================================
// Resolver Methods - Handle nil fallbacks per scheme-plan.txt
// =========================================================================

// or returns the first non-nil style, or a terminal default style.
func or(styles ...*CellStyle) CellStyle {
	for _, s := range styles {
		if s != nil {
			return *s
		}
	}
	// Terminal defaults: FG=39, BG=49
	return DefaultStyle()
}

// orFG returns the foreground from the first non-nil style.
func orFG(styles ...*CellStyle) Color {
	for _, s := range styles {
		if s != nil {
			return s.Fg
		}
	}
	return ColorDefault // terminal default 39
}

// orBG returns the background from the first non-nil style.
func orBG(styles ...*CellStyle) Color {
	for _, s := range styles {
		if s != nil {
			return s.Bg
		}
	}
	return ColorDefault // terminal default 49
}

// --- Environment Colors ---

func (s *Scheme) GetDesktopFill() CellStyle       { return or(s.DesktopFill) }
func (s *Scheme) GetStatusBar() CellStyle         { return or(s.StatusBar) }
func (s *Scheme) GetStatusBarShortcut() CellStyle { return or(s.StatusBarShortcut) }
func (s *Scheme) GetDock() CellStyle              { return or(s.Dock) }
func (s *Scheme) GetDockItem() CellStyle          { return or(s.DockItem) }

func (s *Scheme) GetFocusedDockItem() CellStyle { return or(s.FocusedDockItem) }

func (s *Scheme) GetHoveredDockItem() CellStyle {
	if s.HoveredDockItem != nil {
		return *s.HoveredDockItem
	}
	return s.hover()
}

// GetDockItemState resolves a dock item's style, with focus taking
// priority over hover.
func (s *Scheme) GetDockItemState(focused, hovered bool) CellStyle {
	switch {
	case focused:
		return s.GetFocusedDockItem()
	case hovered:
		return s.GetHoveredDockItem()
	default:
		return s.GetDockItem()
	}
}

// --- Window Colors ---

func (s *Scheme) GetWindowBorder(active bool) CellStyle {
	if active {
		return or(s.ActiveWindowBorder)
	}
	return or(s.InactiveWindowBorder)
}

func (s *Scheme) GetWindowTitle(active bool) CellStyle {
	if active {
		return or(s.ActiveWindowTitle)
	}
	return or(s.InactiveWindowTitle)
}

// titleBarButton resolves the plain (active/inactive) title-bar button
// style, defaulting its background to the matching title-bar background
// when the scheme leaves it unspecified (ColorDefault) - so a button blends
// into the bar it sits on.
func (s *Scheme) titleBarButton(active bool) CellStyle {
	var st CellStyle
	if active {
		st = or(s.ActiveTitleBarButton)
	} else {
		st = or(s.InactiveTitleBarButton)
	}
	if st.Bg == ColorDefault {
		st = st.WithBg(s.GetWindowTitle(active).Bg)
	}
	return st
}

func (s *Scheme) GetTitleBarButton(active, focused, pressed bool) CellStyle {
	if pressed {
		return or(s.PressedTitleBarButton)
	}
	if focused {
		return or(s.FocusedTitleBarButton)
	}
	return s.titleBarButton(active)
}

func (s *Scheme) GetHoveredTitleBarButton() CellStyle {
	if s.HoveredTitleBarButton != nil {
		return *s.HoveredTitleBarButton
	}
	return s.hover()
}

// GetTitleBarButtonState resolves a title-bar button's style with the
// precedence pressed > focus > hover > active/inactive.
func (s *Scheme) GetTitleBarButtonState(active, focused, hovered, pressed bool) CellStyle {
	if pressed {
		return or(s.PressedTitleBarButton)
	}
	if focused {
		return or(s.FocusedTitleBarButton)
	}
	if hovered {
		return s.GetHoveredTitleBarButton()
	}
	return s.titleBarButton(active)
}

func (s *Scheme) GetWindowFG(active bool) Color {
	if active {
		return orFG(s.ActiveWindowFG)
	}
	return orFG(s.InactiveWindowFG)
}

func (s *Scheme) GetWindowBG(active bool) Color {
	if active {
		return orBG(s.ActiveWindowBG)
	}
	return orBG(s.InactiveWindowBG)
}

// --- State Colors ---

func (s *Scheme) GetFocusBG() Color        { return orBG(s.FocusBG) }
func (s *Scheme) GetFocusFG() Color        { return orFG(s.FocusFG) }
func (s *Scheme) GetFocusTextFG() Color    { return orFG(s.FocusTextFG) }
func (s *Scheme) GetHoverBG() Color        { return orBG(s.HoverBG) }
func (s *Scheme) GetHoverFG() Color        { return orFG(s.HoverFG) }
func (s *Scheme) GetHoverTextFG() Color    { return orFG(s.HoverTextFG, s.FocusTextFG) }
func (s *Scheme) GetDisabledTextFG() Color { return orFG(s.DisabledTextFG) }
func (s *Scheme) GetSelection() CellStyle  { return or(s.Selection) }

// hover is the default Hovered* style: HoverFG on HoverBG. Trinkets should
// prefer the more specific Get*State resolvers, which give Focus (and
// Pressed) priority over hover when an item is in more than one state.
func (s *Scheme) hover() CellStyle {
	return DefaultStyle().WithFg(s.GetHoverFG()).WithBg(s.GetHoverBG())
}

func (s *Scheme) GetNormal(active bool) CellStyle {
	if s.Normal != nil {
		return *s.Normal
	}
	// nil = WindowFG on WindowBG
	return DefaultStyle().WithFg(s.GetWindowFG(active)).WithBg(s.GetWindowBG(active))
}

// GetLedgerOdd returns the ledger banding style for odd rows
// (1-based: the first row) in list/tree views with ledger mode on.
func (s *Scheme) GetLedgerOdd() CellStyle {
	if s.LedgerOdd != nil {
		return *s.LedgerOdd
	}
	return DefaultStyle().WithFg(ColorCyan).WithBg(RGB(26, 26, 26))
}

// GetLedgerEven returns the ledger banding style for even rows.
func (s *Scheme) GetLedgerEven() CellStyle {
	if s.LedgerEven != nil {
		return *s.LedgerEven
	}
	return DefaultStyle().WithFg(ColorWhite).WithBg(RGB(12, 44, 44))
}

// GetHeader returns the column header band style (details views).
func (s *Scheme) GetHeader() CellStyle {
	if s.Header != nil {
		return *s.Header
	}
	return DefaultStyle().WithFg(ColorWhite).WithBg(ColorYellow)
}

// --- Menu Colors ---

func (s *Scheme) GetMenuBar() CellStyle           { return or(s.MenuBar) }
func (s *Scheme) GetMenuBarMeta() CellStyle       { return or(s.MenuBarMeta) }
func (s *Scheme) GetMenuBarInfo() CellStyle       { return or(s.MenuBarInfo) }
func (s *Scheme) GetActiveMenuBarItem() CellStyle { return or(s.ActiveMenuBarItem) }
func (s *Scheme) GetActiveMenuBarMeta() CellStyle { return or(s.ActiveMenuBarMeta) }
func (s *Scheme) GetFocusedMenuBarItem() CellStyle {
	return or(s.FocusedMenuBarItem, s.FocusedMenuItemText)
}
func (s *Scheme) GetFocusedMenuBarMeta() CellStyle {
	return or(s.FocusedMenuBarMeta, s.FocusedMenuAccelerator)
}
func (s *Scheme) GetMenuGutter() CellStyle            { return or(s.MenuGutter) }
func (s *Scheme) GetMenuCheckIcon() CellStyle         { return or(s.MenuCheckIcon) }
func (s *Scheme) GetMenuRadioIcon() CellStyle         { return or(s.MenuRadioIcon) }
func (s *Scheme) GetMenuItemText() CellStyle          { return or(s.MenuItemText) }
func (s *Scheme) GetMenuAccelerator() CellStyle       { return or(s.MenuAccelerator) }
func (s *Scheme) GetMenuShortcut() CellStyle          { return or(s.MenuShortcut) }
func (s *Scheme) GetMenuSeparator() CellStyle         { return or(s.MenuSeparator) }
func (s *Scheme) GetMenuSeparatorGutter() CellStyle   { return or(s.MenuSeparatorGutter) }
func (s *Scheme) GetMenuBarButton() CellStyle         { return or(s.MenuBarButton) }
func (s *Scheme) GetDisabledMenuBarButton() CellStyle { return or(s.DisabledMenuBarButton) }
func (s *Scheme) GetHoveredMenuBarButton() CellStyle {
	if s.HoveredMenuBarButton != nil {
		return *s.HoveredMenuBarButton
	}
	return s.hover()
}
func (s *Scheme) GetHoveredMenuBar() CellStyle {
	if s.HoveredMenuBar != nil {
		return *s.HoveredMenuBar
	}
	return s.hover()
}
func (s *Scheme) GetHoveredMenuBarMeta() CellStyle {
	if s.HoveredMenuBarMeta != nil {
		return *s.HoveredMenuBarMeta
	}
	// Bright green accelerator on the hover background.
	return DefaultStyle().WithFg(ColorBrightGreen).WithBg(s.GetHoverBG())
}
func (s *Scheme) GetDisabledMenuGutter() CellStyle     { return or(s.DisabledMenuGutter) }
func (s *Scheme) GetDisabledMenuItem() CellStyle       { return or(s.DisabledMenuItem) }
func (s *Scheme) GetDisabledMenuIcon() CellStyle       { return or(s.DisabledMenuIcon) }
func (s *Scheme) GetFocusedMenuCheckIcon() CellStyle   { return or(s.FocusedMenuCheckIcon) }
func (s *Scheme) GetFocusedMenuRadioIcon() CellStyle   { return or(s.FocusedMenuRadioIcon) }
func (s *Scheme) GetFocusedMenuItemText() CellStyle    { return or(s.FocusedMenuItemText) }
func (s *Scheme) GetFocusedMenuAccelerator() CellStyle { return or(s.FocusedMenuAccelerator) }
func (s *Scheme) GetFocusedMenuShortcut() CellStyle    { return or(s.FocusedMenuShortcut) }

// --- Trinket Group Colors ---

func (s *Scheme) GetTrinketGroupFG() Color   { return orFG(s.TrinketGroupFG) }
func (s *Scheme) GetTrinketGroupBG() Color   { return orBG(s.TrinketGroupBG) }
func (s *Scheme) GetTrinketContentFG() Color { return orFG(s.TrinketContentFG) }
func (s *Scheme) GetTrinketContentBG() Color { return orBG(s.TrinketContentBG) }

// --- Label Colors ---

func (s *Scheme) GetLabelFG(active bool) Color {
	if s.LabelFG != nil {
		return s.LabelFG.Fg
	}
	return s.GetWindowFG(active)
}

func (s *Scheme) GetDisabledLabelFG() Color {
	return orFG(s.DisabledLabelFG, s.DisabledTextFG)
}

// --- Button Colors ---

func (s *Scheme) GetButton() CellStyle { return or(s.Button) }

func (s *Scheme) GetDisabledButtonFG() Color {
	return orFG(s.DisabledButtonFG, s.DisabledTextFG)
}

func (s *Scheme) GetFocusedButton() CellStyle {
	if s.FocusedButton != nil {
		return *s.FocusedButton
	}
	return DefaultStyle().WithFg(s.GetFocusFG()).WithBg(s.GetFocusBG())
}

func (s *Scheme) GetPressedButton(active bool) CellStyle {
	if s.PressedButton != nil {
		return *s.PressedButton
	}
	return DefaultStyle().WithFg(s.GetWindowFG(active)).WithBg(s.GetWindowBG(active))
}

func (s *Scheme) GetHoveredButton() CellStyle {
	if s.HoveredButton != nil {
		return *s.HoveredButton
	}
	return s.hover()
}

// GetButtonState resolves a button's style with the precedence
// pressed > focus > hover > normal.
func (s *Scheme) GetButtonState(active, focused, hovered, pressed bool) CellStyle {
	switch {
	case pressed:
		return s.GetPressedButton(active)
	case focused:
		return s.GetFocusedButton()
	case hovered:
		return s.GetHoveredButton()
	default:
		return s.GetButton()
	}
}

// PaneType indicates the type of pane for button shadow selection.
type PaneType int

const (
	PaneDefault PaneType = iota // ANSI 49 background
	PaneDark                    // Black background
	PaneOther                   // Other background color
)

// GetPaneType determines the PaneType from an inherited background color.
func GetPaneType(bg Color) PaneType {
	if bg == ColorDefault {
		return PaneDefault
	}
	if bg == ColorBlack {
		return PaneDark
	}
	return PaneOther
}

func (s *Scheme) GetButtonShadowFG(pane PaneType) Color {
	switch pane {
	case PaneDefault:
		return orFG(s.DefaultPaneButtonShadowFG)
	case PaneDark:
		return orFG(s.DarkPaneButtonShadowFG)
	default:
		return orFG(s.ButtonShadowFG)
	}
}

// --- EditBox Colors ---

func (s *Scheme) GetEditBox(pane PaneType) CellStyle {
	switch pane {
	case PaneDefault:
		return or(s.DefaultPaneEditBox, s.EditBox)
	case PaneDark:
		return or(s.DarkPaneEditBox, s.EditBox)
	default:
		return or(s.EditBox)
	}
}

func (s *Scheme) GetEditBoxPlaceholder(pane PaneType) CellStyle {
	switch pane {
	case PaneDefault:
		return or(s.DefaultPaneEditBoxPlaceholder, s.EditBoxPlaceholder)
	case PaneDark:
		return or(s.DarkPaneEditBoxPlaceholder, s.EditBoxPlaceholder)
	default:
		return or(s.EditBoxPlaceholder)
	}
}

func (s *Scheme) GetFocusedEditBoxText() CellStyle   { return or(s.FocusedEditBoxText) }
func (s *Scheme) GetFocusedEditBoxCursor() CellStyle { return or(s.FocusedEditBoxCursor) }
func (s *Scheme) GetFocusedEditBoxFill() CellStyle   { return or(s.FocusedEditBoxFill) }

// GetFocusedEditBoxBarCursor returns the color for the graphical bar
// caret (a brighter white than the cell block cursor, for contrast),
// falling back to the block cursor color when unset.
func (s *Scheme) GetFocusedEditBoxBarCursor() CellStyle {
	return or(s.FocusedEditBoxBarCursor, s.FocusedEditBoxCursor)
}

// GetEditBoxSelection returns the selection colors inside an edit
// box: black on silver while focused, silver on black at rest, and
// black on cyan for a resting box on a dark pane.
func (s *Scheme) GetEditBoxSelection(focused bool, pane PaneType) CellStyle {
	if focused {
		return DefaultStyle().
			WithFg(orColor(s.FocusedEditBoxSelectionFG, ColorBlack)).
			WithBg(orColor(s.FocusedEditBoxSelectionBG, ColorWhite))
	}
	if pane == PaneDark {
		return DefaultStyle().
			WithFg(orColor(s.DarkPaneRestingEditBoxSelectionFG, ColorBlack)).
			WithBg(orColor(s.DarkPaneRestingEditBoxSelectionBG, ColorCyan))
	}
	return DefaultStyle().
		WithFg(orColor(s.RestingEditBoxSelectionFG, ColorWhite)).
		WithBg(orColor(s.RestingEditBoxSelectionBG, ColorBlack))
}

// --- ComboBox Colors ---

func (s *Scheme) GetComboBox(pane PaneType) CellStyle {
	if s.ComboBox != nil {
		return *s.ComboBox
	}
	return s.GetEditBox(pane)
}

func (s *Scheme) GetComboBoxArrow(pane PaneType) CellStyle {
	if s.ComboBoxArrow != nil {
		return *s.ComboBoxArrow
	}
	return s.GetEditBox(pane)
}

func (s *Scheme) GetDisabledComboBoxFG() Color {
	return orFG(s.DisabledComboBoxFG, s.DisabledButtonFG, s.DisabledTextFG)
}

func (s *Scheme) GetFocusedComboBox() CellStyle {
	if s.FocusedComboBox != nil {
		return *s.FocusedComboBox
	}
	return s.GetFocusedButton()
}

func (s *Scheme) GetFocusedComboBoxArrow() CellStyle {
	if s.FocusedComboBoxArrow != nil {
		return *s.FocusedComboBoxArrow
	}
	return s.GetFocusedComboBox()
}

func (s *Scheme) GetPressedComboBox(active bool) CellStyle {
	if s.PressedComboBox != nil {
		return *s.PressedComboBox
	}
	return s.GetPressedButton(active)
}

func (s *Scheme) GetPressedComboBoxArrow(active bool) CellStyle {
	if s.PressedComboBoxArrow != nil {
		return *s.PressedComboBoxArrow
	}
	return s.GetPressedComboBox(active)
}

func (s *Scheme) GetDropdownItemText() CellStyle {
	return or(s.DropdownItemText, s.MenuItemText)
}

func (s *Scheme) GetFocusedDropdownItemText() CellStyle {
	return or(s.FocusedDropdownItemText, s.FocusedMenuItemText)
}

func (s *Scheme) GetDropdownSeparator() CellStyle {
	return or(s.DropdownSeparator, s.MenuSeparator)
}

func (s *Scheme) GetDisabledDropdownItem() CellStyle {
	return or(s.DisabledDropdownItem, s.DisabledMenuItem)
}

func (s *Scheme) GetDropdownScrollbar() CellStyle {
	if s.DropdownScrollbar != nil {
		return *s.DropdownScrollbar
	}
	return s.GetDropdownItemText()
}

func (s *Scheme) GetDropdownScrollbarThumb() CellStyle {
	if s.DropdownScrollbarThumb != nil {
		return *s.DropdownScrollbarThumb
	}
	// Default to black on white (like PressedSplitter)
	return DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite)
}

// --- CheckBox Colors ---

func (s *Scheme) GetCheckBoxFG(active bool) Color {
	if s.CheckBoxFG != nil {
		return s.CheckBoxFG.Fg
	}
	return s.GetLabelFG(active)
}

func (s *Scheme) GetCheckBoxLabelFG(active bool) Color {
	if s.CheckBoxLabelFG != nil {
		return s.CheckBoxLabelFG.Fg
	}
	return s.GetLabelFG(active)
}

func (s *Scheme) GetFocusedCheckBoxFG() Color {
	return orFG(s.FocusedCheckBoxFG, s.FocusTextFG)
}

func (s *Scheme) GetFocusedCheckBoxLabelFG() Color {
	return orFG(s.FocusedCheckBoxLabelFG, s.FocusTextFG)
}

// --- RadioButton Colors ---

func (s *Scheme) GetRadioButtonFG(active bool) Color {
	if s.RadioButtonFG != nil {
		return s.RadioButtonFG.Fg
	}
	return s.GetLabelFG(active)
}

func (s *Scheme) GetRadioButtonLabelFG(active bool) Color {
	if s.RadioButtonLabelFG != nil {
		return s.RadioButtonLabelFG.Fg
	}
	return s.GetLabelFG(active)
}

func (s *Scheme) GetFocusedRadioButtonFG() Color {
	return orFG(s.FocusedRadioButtonFG, s.FocusTextFG)
}

func (s *Scheme) GetFocusedRadioButtonLabelFG() Color {
	return orFG(s.FocusedRadioButtonLabelFG, s.FocusTextFG)
}

// --- Splitter Colors ---

func (s *Scheme) GetSplitter() CellStyle { return or(s.Splitter) }

func (s *Scheme) GetSplitterHandle() CellStyle {
	return or(s.SplitterHandle, s.Splitter)
}

func (s *Scheme) GetSplitterTitle() CellStyle {
	return or(s.SplitterTitle, s.Splitter)
}

func (s *Scheme) GetFocusedSplitter() CellStyle {
	if s.FocusedSplitter != nil {
		return *s.FocusedSplitter
	}
	return DefaultStyle().WithFg(s.GetFocusFG()).WithBg(s.GetFocusBG())
}

func (s *Scheme) GetFocusedSplitterHandle() CellStyle {
	return or(s.FocusedSplitterHandle, s.FocusedSplitter)
}

func (s *Scheme) GetFocusedSplitterTitle() CellStyle {
	return or(s.FocusedSplitterTitle, s.FocusedSplitter)
}

func (s *Scheme) GetPressedSplitter() CellStyle {
	if s.PressedSplitter != nil {
		return *s.PressedSplitter
	}
	// Default to black on white (like pressed window frame buttons)
	return DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite)
}

func (s *Scheme) GetPressedSplitterHandle() CellStyle {
	if s.PressedSplitterHandle != nil {
		return *s.PressedSplitterHandle
	}
	return s.GetPressedSplitter()
}

func (s *Scheme) GetPressedSplitterTitle() CellStyle {
	if s.PressedSplitterTitle != nil {
		return *s.PressedSplitterTitle
	}
	return s.GetPressedSplitter()
}

func (s *Scheme) GetHoveredSplitterHandle() CellStyle {
	if s.HoveredSplitterHandle != nil {
		return *s.HoveredSplitterHandle
	}
	return s.hover()
}

func (s *Scheme) GetHoveredSplitterTitle() CellStyle {
	if s.HoveredSplitterTitle != nil {
		return *s.HoveredSplitterTitle
	}
	return s.hover()
}

// GetSplitterHandleState resolves the splitter handle style with the
// precedence pressed > focus > hover > normal.
func (s *Scheme) GetSplitterHandleState(focused, hovered, pressed bool) CellStyle {
	switch {
	case pressed:
		return s.GetPressedSplitterHandle()
	case focused:
		return s.GetFocusedSplitterHandle()
	case hovered:
		return s.GetHoveredSplitterHandle()
	default:
		return s.GetSplitterHandle()
	}
}

// GetSplitterTitleState resolves the splitter title style with the
// precedence pressed > focus > hover > normal.
func (s *Scheme) GetSplitterTitleState(focused, hovered, pressed bool) CellStyle {
	switch {
	case pressed:
		return s.GetPressedSplitterTitle()
	case focused:
		return s.GetFocusedSplitterTitle()
	case hovered:
		return s.GetHoveredSplitterTitle()
	default:
		return s.GetSplitterTitle()
	}
}

// --- Tab Trinket Colors ---

func (s *Scheme) GetTabsFG(active bool) Color {
	if s.TabsFG != nil {
		return s.TabsFG.Fg
	}
	return s.GetWindowFG(active)
}

func (s *Scheme) GetTabsBG() Color {
	if s.TabsBG != nil {
		return s.TabsBG.Bg
	}
	return ColorDefault // inherit from parent
}

// GetTabsBar returns the complete style for the tab bar area.
func (s *Scheme) GetTabsBar(active bool) CellStyle {
	return DefaultStyle().WithFg(s.GetTabsFG(active)).WithBg(s.GetTabsBG())
}

// GetActiveTab returns the complete style for the active tab (FG + BG + Attrs).
func (s *Scheme) GetActiveTab() CellStyle {
	return DefaultStyle().
		WithFg(s.GetActiveTabFG()).
		WithBg(s.GetActiveTabBG()).
		WithAttrs(s.GetActiveTabAttrs())
}

func (s *Scheme) GetTabsButton(active bool, inheritedBG Color) CellStyle {
	fg := s.GetTabsFG(active)
	if s.TabsButtonFG != nil {
		fg = s.TabsButtonFG.Fg
	}
	bg := inheritedBG
	if s.TabsButtonBG != nil {
		bg = s.TabsButtonBG.Bg
	}
	return DefaultStyle().WithFg(fg).WithBg(bg)
}

// GetHoveredTabsButton is the tab scroll button ([<]/[>]) style under the
// pointer, defaulting to the HoverFG/HoverBG pair.
func (s *Scheme) GetHoveredTabsButton() CellStyle {
	if s.HoveredTabsButton != nil {
		return *s.HoveredTabsButton
	}
	return s.hover()
}

func (s *Scheme) GetPressedTabsButton() CellStyle {
	if s.PressedTabsButton != nil {
		return *s.PressedTabsButton
	}
	return DefaultStyle().WithFg(ColorBlack).WithBg(ColorWhite)
}

func (s *Scheme) GetDisabledTabsButton(inheritedBG Color) CellStyle {
	fg := s.GetDisabledTextFG()
	if s.DisabledTabsButtonFG != nil {
		fg = s.DisabledTabsButtonFG.Fg
	}
	bg := inheritedBG
	if s.DisabledTabsButtonBG != nil {
		bg = s.DisabledTabsButtonBG.Bg
	}
	return DefaultStyle().WithFg(fg).WithBg(bg)
}

func (s *Scheme) GetActiveTabFG() Color {
	if s.ActiveTabFG != nil {
		return s.ActiveTabFG.Fg
	}
	return s.GetTrinketGroupFG()
}

func (s *Scheme) GetActiveTabBG() Color {
	if s.ActiveTabBG != nil {
		return s.ActiveTabBG.Bg
	}
	return s.GetTrinketGroupBG()
}

func (s *Scheme) GetActiveTabAttrs() TextStyle {
	if s.ActiveTabFG != nil {
		return s.ActiveTabFG.Attrs
	}
	return StyleNormal
}

func (s *Scheme) GetFocusedTab() CellStyle {
	if s.FocusedTab != nil {
		return *s.FocusedTab
	}
	return DefaultStyle().WithFg(s.GetFocusFG()).WithBg(s.GetFocusBG())
}

// --- List Colors ---

func (s *Scheme) GetListBG() Color {
	if s.ListBG != nil {
		return s.ListBG.Bg
	}
	return s.GetTrinketContentBG()
}

func (s *Scheme) GetListFG() Color {
	if s.ListFG != nil {
		return s.ListFG.Fg
	}
	return s.GetTrinketContentFG()
}

func (s *Scheme) GetFocusedListBG() Color {
	return orBG(s.FocusedListBG, s.ListBG)
}

func (s *Scheme) GetFocusedListFG() Color {
	return orFG(s.FocusedListFG, s.ListFG)
}

func (s *Scheme) GetSelectedListItem() CellStyle {
	return or(s.SelectedListItem, s.Selection)
}

// GetFocusedListRow is the focused-and-selected ROW band in an
// editing-capable list/tree: the whole row lights in this style while
// FocusedListItem marks only the cell the Enter key would edit.
func (s *Scheme) GetFocusedListRow() CellStyle {
	if s.FocusedListRow != nil {
		return *s.FocusedListRow
	}
	return DefaultStyle().WithFg(ColorBrightYellow).WithBg(ColorBrightBlack)
}

func (s *Scheme) GetFocusedListItem() CellStyle {
	if s.FocusedListItem != nil {
		return *s.FocusedListItem
	}
	return DefaultStyle().WithFg(s.GetFocusFG()).WithBg(s.GetFocusBG())
}

// GetFocusedListButton styles a list/tree's own CONTROL stops (the
// header bar, sort captions, the chooser button) when the internal
// focus sits on them - they are buttons, not list items, and wear the
// button face unless a scheme overrides it.
func (s *Scheme) GetFocusedListButton() CellStyle {
	if s.FocusedListButton != nil {
		return *s.FocusedListButton
	}
	return s.GetFocusedButton()
}

// --- Scrollbar Colors ---

func (s *Scheme) GetScrollbar() CellStyle      { return or(s.Scrollbar) }
func (s *Scheme) GetScrollbarThumb() CellStyle { return or(s.ScrollbarThumb) }

func (s *Scheme) GetHoveredScrollbarThumb() CellStyle {
	if s.HoveredScrollbarThumb != nil {
		return *s.HoveredScrollbarThumb
	}
	return s.hover()
}

// GetScrollbarThumbState resolves the scrollbar thumb style; the thumb has
// no focus state, so hover is the only elevated state.
func (s *Scheme) GetScrollbarThumbState(hovered bool) CellStyle {
	if hovered {
		return s.GetHoveredScrollbarThumb()
	}
	return s.GetScrollbarThumb()
}

// --- ProgressBar Colors ---

func (s *Scheme) GetProgressFull() CellStyle      { return or(s.ProgressFull) }
func (s *Scheme) GetProgressEmpty() CellStyle     { return or(s.ProgressEmpty) }
func (s *Scheme) GetProgressFullText() CellStyle  { return or(s.ProgressFullText) }
func (s *Scheme) GetProgressEmptyText() CellStyle { return or(s.ProgressEmptyText) }

// --- GroupBox Colors ---

func (s *Scheme) GetGroupBoxBorder(active bool, inheritedBG Color) CellStyle {
	if s.GroupBoxBorder != nil {
		return *s.GroupBoxBorder
	}
	return DefaultStyle().WithFg(s.GetWindowFG(active)).WithBg(inheritedBG)
}

func (s *Scheme) GetGroupBoxTitle(active bool, inheritedBG Color) CellStyle {
	if s.GroupBoxTitle != nil {
		return *s.GroupBoxTitle
	}
	return DefaultStyle().WithFg(s.GetWindowFG(active)).WithBg(inheritedBG)
}

func (s *Scheme) GetFocusedGroupBoxBorder(inheritedBG Color) CellStyle {
	if s.FocusedGroupBoxBorder != nil {
		return *s.FocusedGroupBoxBorder
	}
	return DefaultStyle().WithFg(s.GetFocusTextFG()).WithBg(inheritedBG)
}

func (s *Scheme) GetFocusedGroupBoxTitle(inheritedBG Color) CellStyle {
	if s.FocusedGroupBoxTitle != nil {
		return *s.FocusedGroupBoxTitle
	}
	return DefaultStyle().WithFg(s.GetFocusTextFG()).WithBg(inheritedBG)
}
