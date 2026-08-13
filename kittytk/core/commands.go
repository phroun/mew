package core

// The command vocabulary. A command names what a key MEANS; who acts on it is
// the existing dispatch chain's business.
//
// The families are deliberately separate, because the actions are: moving a
// window is not moving a cursor, and moving a cursor is not extending a
// selection. A trinket that resizes something — a splitter — borrows the
// window resize names rather than pretending a divider is an "item".
const (
	// Windows and the desktop.
	CmdWindowClose          = "window_close"
	CmdWindowMaximizeToggle = "window_maximize_toggle"
	CmdWindowNext           = "window_next"
	CmdWindowPrior          = "window_prior"
	CmdWindowMDINext        = "window_mdi_next"
	CmdWindowMDIPrior       = "window_mdi_prior"
	CmdWindowCancelResize   = "window_cancel_resize"
	CmdAppMenu              = "app_menu"
	// Summoning the bar and going straight to Help: the same act as app_menu,
	// carried one step further -- the Help menu selected, scrolled into view,
	// dropped open, and its first available item highlighted, as if the user
	// had pressed the menu key and then Down. An application with no Help menu
	// gets exactly app_menu, so the key is never dead.
	CmdAppHelp     = "app_help"
	CmdAppMinimize = "app_minimize"
	// Quitting ONE application: it closes that app's windows and takes the
	// app off the desktop. The desktop itself survives -- it is the thing
	// other apps are still running on -- unless there is no desktop to go
	// back to, which is solo mode, and this was the last app left.
	CmdAppQuit = "app_quit"
	// Hiding: the app's own windows go away and come back, and — the two that
	// reach BEYOND the app — everyone else's do. Hide is something an
	// application does to itself; Hide Others and Show All are things the
	// SESSION does on its behalf, which is a distinction worth keeping in
	// sight when these become utterable rather than merely bindable.
	CmdAppHide       = "app_hide"
	CmdAppHideOthers = "app_hide_others"
	CmdAppShowAll    = "app_show_all"
	// Leaving the desktop itself: the system menu's Exit Desktop, which ends
	// the desktop rather than any application on it.
	CmdDesktopExit   = "desktop_exit"
	CmdGUIScaleDown  = "gui_scale_down"
	CmdGUIScaleUp    = "gui_scale_up"
	CmdGUIScaleReset = "gui_scale_reset"

	// Focus, which belongs to no one trinket.
	CmdFocusNext  = "focus_next"
	CmdFocusPrior = "focus_prior"

	// Window geometry. The fine forms are a single step; the plain forms are
	// the coarse one. A splitter uses the size family for its divider.
	CmdWindowMoveUp        = "window_move_up"
	CmdWindowMoveDown      = "window_move_down"
	CmdWindowMoveLeft      = "window_move_left"
	CmdWindowMoveRight     = "window_move_right"
	CmdWindowMoveFineUp    = "window_move_fine_up"
	CmdWindowMoveFineDown  = "window_move_fine_down"
	CmdWindowMoveFineLeft  = "window_move_fine_left"
	CmdWindowMoveFineRight = "window_move_fine_right"
	CmdWindowSizeUp        = "window_size_up"
	CmdWindowSizeDown      = "window_size_down"
	CmdWindowSizeLeft      = "window_size_left"
	CmdWindowSizeRight     = "window_size_right"
	CmdWindowSizeFineUp    = "window_size_fine_up"
	CmdWindowSizeFineDown  = "window_size_fine_down"
	CmdWindowSizeFineLeft  = "window_size_fine_left"
	CmdWindowSizeFineRight = "window_size_fine_right"

	// Moving within a trinket. The prior/next and up/down forms are synonyms
	// wherever both make sense — a list steps in one dimension and does not
	// care which word names it — while a grid like the dock means them
	// separately: prior/next walk the sequence, up/down cross rows.
	CmdTrinketItemPrior = "trinket_item_prior"
	CmdTrinketItemNext  = "trinket_item_next"
	CmdTrinketItemUp    = "trinket_item_up"
	CmdTrinketItemDown  = "trinket_item_down"
	CmdTrinketItemLeft  = "trinket_item_left"
	CmdTrinketItemRight = "trinket_item_right"
	// Crossing columns and NOTHING else, for a keymap that wants a left which
	// never collapses. The arrows are the generic movement and a tree spends
	// them on collapse-or-walk-up; these are unbound by default and exist to
	// be mapped by anyone who would rather separate the two.
	CmdTrinketColumnLeft  = "trinket_column_left"
	CmdTrinketColumnRight = "trinket_column_right"
	CmdTrinketPagePrior   = "trinket_page_prior"
	CmdTrinketPageNext    = "trinket_page_next"
	CmdTrinketBeg         = "trinket_beg"
	CmdTrinketEnd         = "trinket_end"
	// Going to the beginning, EXCEPT when the caret is already there with
	// nothing selected — the one case where going to the beginning would do
	// nothing at all — where it selects everything instead. This is the Emacs
	// home-cycle ^A does, and it is its own command rather than a wrinkle
	// inside trinket_beg so that a plain Home stays a plain Home. Bound ahead
	// of trinket_beg on the keys that want it, since first-listed wins.
	CmdTrinketBegOrSelectAll = "trinket_beg_or_select_all"

	// Extending a selection: every movement has a with-selection twin, which
	// is a modifier axis rather than a set of unrelated actions.
	CmdTrinketSelUp     = "trinket_sel_up"
	CmdTrinketSelDown   = "trinket_sel_down"
	CmdTrinketSelLeft   = "trinket_sel_left"
	CmdTrinketSelRight  = "trinket_sel_right"
	CmdTrinketSelBeg    = "trinket_sel_beg"
	CmdTrinketSelEnd    = "trinket_sel_end"
	CmdTrinketSelectAll = "trinket_select_all"
	// The clipboard, which acts on the FOCUSED trinket through the same
	// editActor capability select_all does — the standard Edit menu's items
	// are these commands, and what they act on is whatever has the keyboard.
	CmdTrinketCut   = "trinket_cut"
	CmdTrinketCopy  = "trinket_copy"
	CmdTrinketPaste = "trinket_paste"

	// Scrolling WITHOUT moving the selection, which is why it is not an item
	// movement.
	CmdTrinketScrollUp   = "trinket_scroll_up"
	CmdTrinketScrollDown = "trinket_scroll_down"

	// Trees and other things that nest.
	//
	// Expand and collapse are NOT synonyms for item_right and item_left, even
	// where one key reaches both. They are the more specific meaning — a
	// structural change to the tree — while the item movement is generic
	// travel across columns. A trinket answering to both gives them separate
	// cases and lets its context decide which is on offer, rather than
	// folding them together because a single arrow happens to reach either.
	CmdTrinketExpand   = "trinket_expand"
	CmdTrinketCollapse = "trinket_collapse"
	// The classic tree arrow, named so it can be reached on its own key.
	// collapse_or_enclosing collapses an expanded branch and otherwise walks
	// out to the enclosing one -- trinket_collapse plus trinket_enclosing in
	// the order a tree has always done them; expand_or_descend is its mirror,
	// expanding a closed branch and otherwise stepping into the first child.
	//
	// These exist because trinket_item_left means something ELSE in an
	// editable grid, where the plain arrows walk the edit-target column. The
	// shifted arrows keep the classic movement there, and needed a name of
	// their own to say so.
	CmdTrinketCollapseOrEnclosing = "trinket_collapse_or_enclosing"
	CmdTrinketExpandOrDescend     = "trinket_expand_or_descend"
	CmdTrinketExpandAll           = "trinket_expand_all"
	CmdTrinketCollapseAll         = "trinket_collapse_all"
	CmdTrinketEnclosing           = "trinket_enclosing"

	// Editing, where a trinket holds text.
	CmdTrinketDelPrior = "trinket_del_prior"
	CmdTrinketDelNext  = "trinket_del_next"
	CmdTrinketDelLine  = "trinket_del_line"

	// Sorting and the column chooser. Before these, the only way to reach
	// either from the keyboard was a long walk through the header focus zones,
	// so every one of them is newly mappable rather than a rename of something
	// that already had a key.
	//
	// The plain forms set a direction outright; the toggle forms turn that
	// direction off again when it is already in force, so one key can do both.
	// The mode forms walk the cycle a header activation walks: ascending,
	// descending, off.
	CmdTrinketSortAscending        = "trinket_sort_ascending"
	CmdTrinketToggleSortAscending  = "trinket_toggle_sort_ascending"
	CmdTrinketSortDescending       = "trinket_sort_descending"
	CmdTrinketToggleSortDescending = "trinket_toggle_sort_descending"
	CmdTrinketSortOff              = "trinket_sort_off"
	CmdTrinketSortModeNext         = "trinket_sort_mode_next"
	CmdTrinketSortModePrior        = "trinket_sort_mode_prior"
	CmdTrinketChooser              = "trinket_chooser"

	// Expanding or collapsing whichever it currently is, in one key.
	CmdTrinketExpandedToggle = "trinket_expanded_toggle"

	// Beginning an in-place edit, which is not the same as activating. A tree
	// view's Enter opens the row editor where there is an editable column;
	// its Space refuses to begin a TEXT edit and expands or collapses the
	// branch instead, which is what activate means there.
	CmdTrinketEdit = "trinket_edit"

	// A terminal's SCROLLBACK, which is its own thing and not a trinket
	// movement: these move the VIEW over history the child process cannot see
	// and never reach the child at all. They are separate names because a
	// terminal is the one trinket whose job is to pass keys through, so a
	// binding it shares with lists and trees would take a key away from
	// everything else to give it to the scrollback.
	CmdTerminalScrollUp        = "terminal_scroll_up"
	CmdTerminalScrollDown      = "terminal_scroll_down"
	CmdTerminalScrollPagePrior = "terminal_scroll_page_prior"
	CmdTerminalScrollPageNext  = "terminal_scroll_page_next"
	CmdTerminalScrollBeg       = "terminal_scroll_beg"
	CmdTerminalScrollEnd       = "terminal_scroll_end"

	// The rest.
	CmdTrinketActivate = "trinket_activate"
	CmdTrinketCancel   = "trinket_cancel"
	CmdTrinketOpen     = "trinket_open"
)
