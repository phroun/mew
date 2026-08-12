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
	CmdAppMinimize          = "app_minimize"
	CmdGUIScaleDown         = "gui_scale_down"
	CmdGUIScaleUp           = "gui_scale_up"
	CmdGUIScaleReset        = "gui_scale_reset"

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
	CmdTrinketPagePrior = "trinket_page_prior"
	CmdTrinketPageNext  = "trinket_page_next"
	CmdTrinketBeg       = "trinket_beg"
	CmdTrinketEnd       = "trinket_end"

	// Extending a selection: every movement has a with-selection twin, which
	// is a modifier axis rather than a set of unrelated actions.
	CmdTrinketSelUp     = "trinket_sel_up"
	CmdTrinketSelDown   = "trinket_sel_down"
	CmdTrinketSelLeft   = "trinket_sel_left"
	CmdTrinketSelRight  = "trinket_sel_right"
	CmdTrinketSelBeg    = "trinket_sel_beg"
	CmdTrinketSelEnd    = "trinket_sel_end"
	CmdTrinketSelectAll = "trinket_select_all"

	// Scrolling WITHOUT moving the selection, which is why it is not an item
	// movement.
	CmdTrinketScrollUp   = "trinket_scroll_up"
	CmdTrinketScrollDown = "trinket_scroll_down"

	// Trees and other things that nest.
	CmdTrinketExpand      = "trinket_expand"
	CmdTrinketCollapse    = "trinket_collapse"
	CmdTrinketExpandAll   = "trinket_expand_all"
	CmdTrinketCollapseAll = "trinket_collapse_all"
	CmdTrinketEnclosing   = "trinket_enclosing"

	// Editing, where a trinket holds text.
	CmdTrinketDelPrior = "trinket_del_prior"
	CmdTrinketDelNext  = "trinket_del_next"
	CmdTrinketDelLine  = "trinket_del_line"

	// The rest.
	CmdTrinketActivate = "trinket_activate"
	CmdTrinketCancel   = "trinket_cancel"
	CmdTrinketOpen     = "trinket_open"
)
