// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"fmt"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// ComboBox is a drop-down selection trinket.
type ComboBox struct {
	core.TrinketBase
	core.AccessibleTrinket

	items        []string
	currentIndex int
	editable     bool
	editText     string
	placeholer   string

	// Drop-down state
	isOpen     bool
	hoverIndex int // Index of item currently hovered (-1 for none)

	// Scroll state for drop-down
	scrollOffset     int
	maxVisible       int  // User-configured maximum (0 = auto-size to screen)
	popupVisibleRows int            // Actual visible rows for current popup (calculated from screen space)
	popupDropUp      bool           // popup opened above the box (drop-up) rather than below
	popupControlRect core.UnitRect  // the originating control's screen rect (for its outer stroke)

	// popupScreenMetrics is the screen/desktop denomination captured
	// when the popup opens; popup-space geometry, painting, and input
	// all use it (popups are desktop-surface overlays).
	popupScreenMetrics core.CellMetrics

	// Mouse interaction state
	mouseDown     bool       // Mouse button is held down
	dragging      bool       // Actually dragging (mouse moved while down)
	clickMode     bool       // True = click-to-open mode (popup stays open), False = hold-and-drag mode
	mouseDownX    core.Unit  // Initial mouse X position
	mouseDownY    core.Unit  // Initial mouse Y position
	originalIndex int        // Index before popup opened (for cancel on release outside)
	scrollHoverZone int      // -1 = hovering top scroll, 1 = bottom scroll, 0 = none

	// Scrollbar interaction state (click mode only)
	scrollbarDragging   bool  // Whether scrollbar thumb is being dragged
	scrollbarDragStartY int   // Row where drag started

	// Smooth (pixel-surface) popup scrollbar drag: the thumb follows
	// the pointer at unit granularity while scrollOffset snaps to
	// whole items. sbGrabOff is where the press landed within the
	// thumb; sbThumbPos is the unsnapped thumb origin.
	sbSmoothDrag bool
	sbGrabOff    float64
	sbThumbPos   float64

	// Fractional rows carried between trackpad wheel events.
	wheelAccum float64
	scrollbarDragOffset int   // Scroll offset when drag started

	// Timer for scroll repeating
	scrollTimer        *DesktopTimer
	scrollTimerStarter func(interval time.Duration, callback func()) *DesktopTimer
	requestUpdate      func()

	// Callbacks
	onCurrentIndexChanged func(index int)
	onCurrentTextChanged  func(text string)
	onActivated           func(index int)

	// Embedded-host bridge (SetEmbedHost): an unparented box hosted
	// inside another trinket (the TreeView's enum cell editor) borrows
	// the host's ancestry - the popup-controller walk and the screen
	// mapping that places/directs the drop-down. embedOrigin reports
	// this box's current origin in the host's local space.
	embedHost   core.Trinket
	embedOrigin func() core.UnitPoint
}

// SetEmbedHost lends this (unparented) box a host trinket's ancestry:
// the popup controller and the drop-down's screen geometry resolve as
// if the box sat at origin() within the host, so the popup drops the
// same direction (and scrolls the same way) as a natively placed box.
func (c *ComboBox) SetEmbedHost(host core.Trinket, origin func() core.UnitPoint) {
	c.embedHost = host
	c.embedOrigin = origin
}

// mapToScreen maps a box-local point to screen space, through the
// embed host when one is set.
func (c *ComboBox) mapToScreen(pc core.PopupController, local core.UnitPoint) core.UnitPoint {
	if c.embedHost != nil && c.embedOrigin != nil {
		o := c.embedOrigin()
		return pc.MapToScreen(c.embedHost, core.UnitPoint{X: o.X + local.X, Y: o.Y + local.Y})
	}
	return pc.MapToScreen(c.Self(), local)
}

// NewComboBox creates a new combo box.
func NewComboBox() *ComboBox {
	c := &ComboBox{
		currentIndex:    -1,
		hoverIndex:      -1,
		maxVisible:      0, // 0 = auto-size to available screen space
		originalIndex:   -1,
		scrollHoverZone: 0,
	}
	c.TrinketBase = *core.NewTrinketBase()
	c.Init(c) // Enable polymorphic focus handling
	c.SetFocusPolicy(core.StrongFocus)
	c.SetAccessibleRole(core.RoleComboBox)
	return c
}

// effectiveMaxVisible returns the number of visible rows to use for popup calculations.
// When the popup is open, this returns popupVisibleRows (calculated from screen space).
// Otherwise, it returns maxVisible (user-configured) or a default for initial calculations.
func (c *ComboBox) effectiveMaxVisible() int {
	if c.isOpen && c.popupVisibleRows > 0 {
		return c.popupVisibleRows
	}
	if c.maxVisible > 0 {
		return c.maxVisible
	}
	// Default for initial calculations before popup is registered
	return 20
}

// SetScrollTimerStarter sets the function used to create scroll timers.
// This should be called by the parent trinket (e.g., Desktop) to enable
// timer-based scrolling.
func (c *ComboBox) SetScrollTimerStarter(starter func(interval time.Duration, callback func()) *DesktopTimer) {
	c.scrollTimerStarter = starter
}

// SetRequestUpdate sets the function to call when the trinket needs redrawing.
func (c *ComboBox) SetRequestUpdate(fn func()) {
	c.requestUpdate = fn
}

// findTimerProvider walks up the parent chain to find a timer provider.
func (c *ComboBox) findTimerProvider() interface {
	StartRepeatingTimer(interval time.Duration, callback func()) *DesktopTimer
} {
	// Walk up parent chain looking for a trinket with StartRepeatingTimer
	current := c.Parent()
	for current != nil {
		if trinket, ok := current.(core.Trinket); ok {
			if provider, ok := trinket.(interface {
				StartRepeatingTimer(interval time.Duration, callback func()) *DesktopTimer
			}); ok {
				return provider
			}
			current = trinket.Parent()
		} else {
			break
		}
	}
	return nil
}

// startScrollTimer starts a repeating timer for scrolling.
func (c *ComboBox) startScrollTimer(direction int) {
	c.stopScrollTimer()

	// Find timer provider through parent chain
	timerProvider := c.findTimerProvider()
	if timerProvider == nil && c.scrollTimerStarter == nil {
		return
	}

	callback := func() {
		// Timer is stopped when leaving the scroll zone, so just scroll if we can
		if direction < 0 && c.canScrollUp() {
			c.scrollUp(1)
			// If we can no longer scroll up, the scroll indicator disappears and
			// is replaced by an item. Update hover to that item since mouse is there.
			if !c.canScrollUp() {
				c.hoverIndex = c.scrollOffset
			}
			if c.requestUpdate != nil {
				c.requestUpdate()
			}
		} else if direction > 0 && c.canScrollDown() {
			c.scrollDown(1)
			// If we can no longer scroll down, the scroll indicator disappears and
			// is replaced by an item. Update hover to that item since mouse is there.
			if !c.canScrollDown() {
				lastVisible := c.scrollOffset + c.visibleItemCount() - 1
				if lastVisible >= len(c.items) {
					lastVisible = len(c.items) - 1
				}
				c.hoverIndex = lastVisible
			}
			if c.requestUpdate != nil {
				c.requestUpdate()
			}
		}
	}

	if timerProvider != nil {
		c.scrollTimer = timerProvider.StartRepeatingTimer(50*time.Millisecond, callback)
	} else if c.scrollTimerStarter != nil {
		c.scrollTimer = c.scrollTimerStarter(50*time.Millisecond, callback)
	}
}

// stopScrollTimer stops the scroll timer.
func (c *ComboBox) stopScrollTimer() {
	if c.scrollTimer != nil {
		c.scrollTimer.Stop()
		c.scrollTimer = nil
	}
}

// visibleItemCount returns the number of items currently visible in the popup.
// This accounts for scroll indicator rows in drag mode.
func (c *ComboBox) visibleItemCount() int {
	maxVis := c.effectiveMaxVisible()
	count := maxVis
	if count > len(c.items) {
		count = len(c.items)
	}
	// In drag mode with scrolling, scroll indicators take up rows
	if len(c.items) > maxVis && !c.clickMode {
		if c.canScrollUp() {
			count--
		}
		if c.canScrollDown() {
			count--
		}
	}
	return count
}

// canScrollUp returns true if there are items above the visible area.
func (c *ComboBox) canScrollUp() bool {
	return c.scrollOffset > 0
}

// canScrollDown returns true if there are items below the visible area.
func (c *ComboBox) canScrollDown() bool {
	maxVis := c.effectiveMaxVisible()
	if c.clickMode {
		// In click mode, all visible rows are item rows
		return c.scrollOffset+maxVis < len(c.items)
	}
	// In drag mode, account for scroll up indicator if present
	effectiveVisible := maxVis
	if c.scrollOffset > 0 {
		effectiveVisible-- // up indicator takes a row
	}
	return c.scrollOffset+effectiveVisible < len(c.items)
}

// enterClickMode switches to click mode and clamps scroll offset.
// In click mode, all visible rows are item rows (no scroll indicators),
// so the max scroll offset is lower than in drag mode. This function ensures
// the scroll offset is valid for click mode to avoid empty rows at the bottom.
func (c *ComboBox) enterClickMode() {
	c.clickMode = true
	// Clamp scroll offset for click mode (which has more visible item rows)
	maxOffset := len(c.items) - c.effectiveMaxVisible()
	if maxOffset < 0 {
		maxOffset = 0
	}
	if c.scrollOffset > maxOffset {
		c.scrollOffset = maxOffset
	}
}

// scrollUp scrolls the list up by the given amount.
func (c *ComboBox) scrollUp(amount int) {
	c.scrollOffset -= amount
	if c.scrollOffset < 0 {
		c.scrollOffset = 0
	}
	c.Update()
}

// scrollDown scrolls the list down by the given amount.
func (c *ComboBox) scrollDown(amount int) {
	// Calculate effective visible count
	maxVis := c.effectiveMaxVisible()
	visibleCount := maxVis
	// In drag mode with scrolling, up indicator takes a row when scrollOffset > 0
	// After scrolling down, scrollOffset will definitely be > 0
	if !c.clickMode && len(c.items) > maxVis {
		visibleCount-- // up indicator will take a row
	}
	maxOffset := len(c.items) - visibleCount
	if maxOffset < 0 {
		maxOffset = 0
	}
	c.scrollOffset += amount
	if c.scrollOffset > maxOffset {
		c.scrollOffset = maxOffset
	}
	c.Update()
}

// AddItem adds an item to the combo box.
func (c *ComboBox) AddItem(text string) {
	c.items = append(c.items, text)
	if c.currentIndex < 0 && len(c.items) == 1 {
		c.SetCurrentIndex(0)
	}
	c.Update()
}

// AddItems adds multiple items to the combo box.
func (c *ComboBox) AddItems(items []string) {
	for _, item := range items {
		c.AddItem(item)
	}
}

// InsertItem inserts an item at the given index.
func (c *ComboBox) InsertItem(index int, text string) {
	if index < 0 {
		index = 0
	}
	if index > len(c.items) {
		index = len(c.items)
	}

	c.items = append(c.items[:index], append([]string{text}, c.items[index:]...)...)

	// Adjust current index if needed
	if c.currentIndex >= index {
		c.currentIndex++
	}
	c.Update()
}

// RemoveItem removes an item at the given index.
func (c *ComboBox) RemoveItem(index int) {
	if index < 0 || index >= len(c.items) {
		return
	}

	c.items = append(c.items[:index], c.items[index+1:]...)

	// Adjust current index
	if c.currentIndex == index {
		if c.currentIndex >= len(c.items) {
			c.currentIndex = len(c.items) - 1
		}
		c.notifyIndexChanged()
	} else if c.currentIndex > index {
		c.currentIndex--
	}
	c.Update()
}

// Clear removes all items.
func (c *ComboBox) Clear() {
	c.items = nil
	c.currentIndex = -1
	c.editText = ""
	c.Update()
}

// Count returns the number of items.
func (c *ComboBox) Count() int {
	return len(c.items)
}

// ItemText returns the text of the item at the given index.
func (c *ComboBox) ItemText(index int) string {
	if index < 0 || index >= len(c.items) {
		return ""
	}
	return c.items[index]
}

// SetItemText sets the text of the item at the given index.
func (c *ComboBox) SetItemText(index int, text string) {
	if index < 0 || index >= len(c.items) {
		return
	}
	c.items[index] = text
	c.Update()
}

// CurrentIndex returns the current selected index.
func (c *ComboBox) CurrentIndex() int {
	return c.currentIndex
}

// SetCurrentIndex sets the current selected index.
func (c *ComboBox) SetCurrentIndex(index int) {
	if index < -1 || index >= len(c.items) {
		return
	}
	if c.currentIndex == index {
		return
	}

	c.currentIndex = index
	if index >= 0 {
		c.editText = c.items[index]

		// Announce selection change for accessibility
		if am := core.FindAccessibilityManager(c); am != nil {
			am.AnnouncePolite(fmt.Sprintf("%s, %d of %d", c.items[index], index+1, len(c.items)))
		}
	}
	c.Update()
	c.notifyIndexChanged()
}

// CurrentText returns the current text.
func (c *ComboBox) CurrentText() string {
	if c.editable {
		return c.editText
	}
	if c.currentIndex >= 0 && c.currentIndex < len(c.items) {
		return c.items[c.currentIndex]
	}
	return ""
}

// SetCurrentText sets the current text (for editable combo boxes).
func (c *ComboBox) SetCurrentText(text string) {
	if !c.editable {
		// Find matching item
		for i, item := range c.items {
			if item == text {
				c.SetCurrentIndex(i)
				return
			}
		}
		return
	}

	c.editText = text
	c.Update()

	if c.onCurrentTextChanged != nil {
		c.onCurrentTextChanged(text)
	}
}

// IsEditable returns whether the combo box is editable.
func (c *ComboBox) IsEditable() bool {
	return c.editable
}

// SetEditable sets whether the combo box is editable.
func (c *ComboBox) SetEditable(editable bool) {
	c.editable = editable
	c.Update()
}

// Placeholder returns the placeholder text.
func (c *ComboBox) Placeholder() string {
	return c.placeholer
}

// SetPlaceholder sets the placeholder text.
func (c *ComboBox) SetPlaceholder(text string) {
	c.placeholer = text
	c.Update()
}

// IsOpen returns whether the drop-down is open.
func (c *ComboBox) IsOpen() bool {
	return c.isOpen
}

// ShowPopup opens the drop-down.
func (c *ComboBox) ShowPopup() {
	if len(c.items) == 0 {
		return
	}
	c.isOpen = true
	c.originalIndex = c.currentIndex // Save for cancel on release outside
	c.hoverIndex = c.currentIndex    // Start with current selection highlighted
	c.scrollHoverZone = 0

	// Ensure current item is visible (use a reasonable default before popup is registered)
	maxVis := c.effectiveMaxVisible()
	if c.currentIndex >= 0 {
		if c.currentIndex < c.scrollOffset {
			c.scrollOffset = c.currentIndex
		} else if c.currentIndex >= c.scrollOffset+maxVis {
			c.scrollOffset = c.currentIndex - maxVis + 1
		}
	}

	// Set up timer provider and request update for scroll timer
	if timerProvider := c.findTimerProvider(); timerProvider != nil {
		if provider, ok := timerProvider.(interface{ RequestUpdate() }); ok {
			c.requestUpdate = provider.RequestUpdate
		}
	}

	// Register popup overlay - find popup controller by walking parent chain
	if pc := c.findPopupController(); pc != nil {
		c.registerPopupOverlay(pc)
	}

	c.Update()
}

// findPopupController walks up the parent chain to find a popup controller.
func (c *ComboBox) findPopupController() core.PopupController {
	// First check if we have one set directly
	if pc := c.PopupController(); pc != nil {
		return pc
	}

	// Walk up the parent chain looking for a trinket with a popup
	// controller. An embedded box has no parent: start AT its host.
	var current any = c.Parent()
	if c.embedHost != nil {
		current = c.embedHost
	}
	for current != nil {
		if trinket, ok := current.(core.Trinket); ok {
			if getter, ok := trinket.(interface{ PopupController() core.PopupController }); ok {
				if pc := getter.PopupController(); pc != nil {
					return pc
				}
			}
			current = trinket.Parent()
		} else {
			break
		}
	}
	return nil
}

// HidePopup closes the drop-down.
func (c *ComboBox) HidePopup() {
	c.isOpen = false
	c.stopScrollTimer()
	c.mouseDown = false
	c.dragging = false
	c.clickMode = false
	c.scrollHoverZone = 0
	c.scrollbarDragging = false
	c.sbSmoothDrag = false

	// Unregister popup overlay - find popup controller by walking parent chain
	if pc := c.findPopupController(); pc != nil {
		pc.UnregisterPopup(c.popupID())
	}

	c.Update()
}

// popupID returns a unique identifier for this ComboBox's popup,
// derived from the trinket's stable object identity. (Name() is a
// human label and may be empty or duplicated - it is not identity.)
func (c *ComboBox) popupID() string {
	return fmt.Sprintf("combobox-%d", c.ObjectID())
}

// screenMetrics returns the denomination of the screen/desktop surface,
// which popup overlays are composited in. The popup is a desktop-level
// overlay: its geometry, painting, and input all speak the screen's
// currency, not the combobox's (possibly re-denominated) interior.
func (c *ComboBox) screenMetrics() core.CellMetrics {
	if c.popupScreenMetrics.CellWidth > 0 && c.popupScreenMetrics.CellHeight > 0 {
		return c.popupScreenMetrics
	}
	return core.DefaultCellMetrics()
}

// registerPopupOverlay registers the popup with the popup controller.
func (c *ComboBox) registerPopupOverlay(pc core.PopupController) {
	bounds := c.Bounds()
	metrics := c.EffectiveCellMetrics()

	// Popups are desktop-surface overlays: capture the screen currency
	// for all popup-space geometry, painting, and input handling.
	if sm, ok := pc.(interface{ ScreenCellMetrics() core.CellMetrics }); ok {
		c.popupScreenMetrics = sm.ScreenCellMetrics()
	} else {
		c.popupScreenMetrics = core.DefaultCellMetrics()
	}
	screen := c.screenMetrics()

	// Get screen bounds to calculate available space
	screenBounds := pc.ScreenBounds()

	// Get the trinket's positions on screen (the local point is in the
	// trinket's own denomination; MapToScreen exchanges at boundaries)
	trinketBottomPos := c.mapToScreen(pc, core.UnitPoint{X: 0, Y: metrics.CellHeight})
	trinketTopPos := c.mapToScreen(pc, core.UnitPoint{X: 0, Y: 0})

	// Calculate available space below and above the trinket
	spaceBelow := screenBounds.Y + screenBounds.Height - trinketBottomPos.Y
	spaceAbove := trinketTopPos.Y - screenBounds.Y

	// Calculate max rows that fit in each direction (screen rows)
	maxRowsBelow := int(spaceBelow / screen.CellHeight)
	maxRowsAbove := int(spaceAbove / screen.CellHeight)

	// Minimum rows needed to show useful content (at least 2 items + potential scroll indicators)
	const minRowsRequired = 4

	// Decide popup direction: strongly prefer dropping down, but need minimum space
	// Pop up if:
	// 1. Below doesn't have minimum required space (4 rows), OR
	// 2. Below can't show at least half the items AND above has substantially more space
	popDown := true
	itemCount := len(c.items)
	halfItems := (itemCount + 1) / 2 // Majority of items

	if maxRowsBelow < minRowsRequired && maxRowsAbove >= minRowsRequired {
		// Below doesn't have minimum space but above does - pop up
		popDown = false
	} else if maxRowsBelow < halfItems {
		// Below can't show majority - consider popping up if above has substantially more
		if maxRowsAbove >= maxRowsBelow*3/2 && maxRowsAbove >= maxRowsBelow+3 {
			popDown = false
		}
	}

	c.popupDropUp = !popDown // stroke gaps the edge nearest the box
	// Remember the originating control's screen rect so the popup overlay
	// (which paints unclipped) can frame it with the same stroke.
	c.popupControlRect = core.UnitRect{
		X:      trinketTopPos.X,
		Y:      trinketTopPos.Y,
		Width:  core.ExchangeX(bounds.Width, metrics, screen),
		Height: trinketBottomPos.Y - trinketTopPos.Y,
	}

	var popupY core.Unit
	var maxRowsAvailable int
	if popDown {
		popupY = trinketBottomPos.Y
		maxRowsAvailable = maxRowsBelow
	} else {
		maxRowsAvailable = maxRowsAbove
		// popupY will be set after we know the popup height
	}

	// Calculate actual visible rows: min of items, available space, and user max (if set)
	visibleRows := len(c.items)
	if visibleRows > maxRowsAvailable {
		visibleRows = maxRowsAvailable
	}
	if c.maxVisible > 0 && visibleRows > c.maxVisible {
		visibleRows = c.maxVisible
	}
	// Ensure at least 1 row
	if visibleRows < 1 {
		visibleRows = 1
	}

	// Store for use during rendering
	c.popupVisibleRows = visibleRows

	popupHeightUnits := core.Unit(visibleRows) * screen.CellHeight

	// If popping up, calculate Y position now that we know height
	if !popDown {
		popupY = trinketTopPos.Y - popupHeightUnits
		if popupY < screenBounds.Y {
			popupY = screenBounds.Y
		}
	}

	popupBounds := core.UnitRect{
		X: trinketBottomPos.X,
		Y: popupY,
		// The trinket's width is in its own denomination; the popup
		// lives on the screen surface.
		Width:  core.ExchangeX(bounds.Width, metrics, screen),
		Height: popupHeightUnits,
	}

	// Create popup request
	request := &core.PopupRequest{
		ID:     c.popupID(),
		Bounds: popupBounds,
		Paint: func(p *core.Painter) {
			c.paintPopupOverlay(p, popupBounds)
		},
		HandleMousePress: func(event core.MousePressEvent) bool {
			return c.handlePopupMousePress(event, popupBounds)
		},
		HandleMouseMove: func(event core.MouseMoveEvent) bool {
			return c.handlePopupMouseMove(event, popupBounds)
		},
		HandleMouseWheel: func(event core.MouseWheelEvent) bool {
			return c.handlePopupMouseWheel(event)
		},
		HandleMouseRelease: func(event core.MouseReleaseEvent) bool {
			return c.handlePopupMouseRelease(event, popupBounds)
		},
		// The host can force-clear the overlay (press outside every
		// popup) without routing the press here: resync isOpen or the
		// box keeps acting like the dropdown is still up.
		OnDismiss: func() { c.HidePopup() },
	}

	pc.RegisterPopup(request)
}

// SetMaxVisibleItems sets the maximum number of visible items in the drop-down.
func (c *ComboBox) SetMaxVisibleItems(count int) {
	if count < 1 {
		count = 1
	}
	c.maxVisible = count
}

// SetOnCurrentIndexChanged sets the index changed callback.
func (c *ComboBox) SetOnCurrentIndexChanged(handler func(index int)) {
	c.onCurrentIndexChanged = handler
}

// SetOnCurrentTextChanged sets the text changed callback.
func (c *ComboBox) SetOnCurrentTextChanged(handler func(text string)) {
	c.onCurrentTextChanged = handler
}

// SetOnActivated sets the activated callback (when item is selected).
func (c *ComboBox) SetOnActivated(handler func(index int)) {
	c.onActivated = handler
}

func (c *ComboBox) notifyIndexChanged() {
	if c.onCurrentIndexChanged != nil {
		c.onCurrentIndexChanged(c.currentIndex)
	}
	if c.onCurrentTextChanged != nil {
		c.onCurrentTextChanged(c.CurrentText())
	}
}

// SizeHint returns the preferred size.
func (c *ComboBox) SizeHint() core.UnitSize {
	metrics := c.EffectiveCellMetrics()
	font := c.EffectiveFont()

	// Calculate width based on longest item using font measurement
	minWidth := font.MeasureText("----------") // Minimum 10 chars
	maxWidth := minWidth
	for _, item := range c.items {
		itemWidth := font.MeasureText(item)
		if itemWidth > maxWidth {
			maxWidth = itemWidth
		}
	}

	// Add space for dropdown arrow " ▼"
	arrowWidth := font.MeasureText(" ▼")

	return core.UnitSize{
		Width:  maxWidth + arrowWidth,
		Height: metrics.TextHeight(1),
	}
}

// IsInlineTrinket returns true to indicate this is a text-style trinket
// that should receive horizontal margins when in a vertical box layout.
func (c *ComboBox) IsInlineTrinket() bool {
	return true
}

// Paint renders the combo box.
func (c *ComboBox) Paint(p *core.Painter) {
	bounds := c.Bounds()
	scheme := c.GetScheme()
	focused := c.HasFocus()

	// Get inherited background to determine pane type
	inheritedBg := c.EffectiveBackgroundColor()
	paneType := style.GetPaneType(inheritedBg)

	// Determine style
	var s style.CellStyle
	if !c.IsEnabled() {
		s = style.DefaultStyle().WithFg(scheme.GetDisabledComboBoxFG()).WithBg(scheme.GetComboBox(paneType).Bg)
	} else if c.isOpen {
		// When popup is open, show pressed style
		s = scheme.GetPressedComboBox(true)
	} else if focused {
		s = scheme.GetFocusedComboBox()
	} else {
		s = scheme.GetComboBox(paneType)
	}

	// Draw background
	p.FillRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, ' ', s)

	// Get current text
	text := c.CurrentText()
	if text == "" && c.placeholer != "" {
		text = c.placeholer
		s = s.WithAttrs(style.StyleDim)
	}

	// Get font for text measurement and rendering
	font := c.EffectiveFont()

	// Calculate text area width (leave space for arrow)
	arrowWidth := font.MeasureText(" ▼")
	textAreaWidth := bounds.Width - arrowWidth

	// Truncate text if needed to fit in text area
	displayText := text
	for font.MeasureText(displayText) > textAreaWidth && len(displayText) > 0 {
		displayText = displayText[:len(displayText)-1]
	}

	// Draw text
	p.DrawText(0, 0, displayText, s, font)

	// Draw dropdown arrow at the right
	arrowX := bounds.Width - arrowWidth
	p.DrawText(arrowX, 0, " ▼", s, font)

	// Draw popup if open - only use fallback if no popup controller found
	if c.isOpen && c.findPopupController() == nil {
		c.paintPopup(p)
	}
}

// paintPopup renders the drop-down popup.
func (c *ComboBox) paintPopup(p *core.Painter) {
	bounds := c.Bounds()
	scheme := c.GetScheme()
	metrics := c.EffectiveCellMetrics()

	// Calculate popup bounds
	maxVis := c.effectiveMaxVisible()
	popupHeight := len(c.items)
	if popupHeight > maxVis {
		popupHeight = maxVis
	}

	popupY := metrics.CellHeight // Below the main trinket

	// Draw popup background
	popupBounds := core.UnitRect{
		X:      0,
		Y:      popupY,
		Width:  bounds.Width,
		Height: core.Unit(popupHeight) * metrics.CellHeight,
	}
	itemStyle := scheme.GetDropdownItemText()
	p.FillRect(popupBounds, ' ', itemStyle)
	p.DrawRect(popupBounds, c.Theme().DefaultBorder, itemStyle)

	// Draw items
	for i := 0; i < popupHeight; i++ {
		itemIndex := c.scrollOffset + i
		if itemIndex >= len(c.items) {
			break
		}

		item := c.items[itemIndex]
		itemY := popupY + core.Unit(i)*metrics.CellHeight

		// Determine item style
		var s style.CellStyle
		if itemIndex == c.currentIndex {
			s = scheme.GetFocusedDropdownItemText()
		} else {
			s = scheme.GetDropdownItemText()
		}

		// Draw item background
		p.FillRect(core.UnitRect{
			X:      0,
			Y:      itemY,
			Width:  bounds.Width,
			Height: metrics.CellHeight,
		}, ' ', s)

		// Draw item text through the proportional path, clipped only
		// at the popup edge - labels flow under the scrollbar lane
		// (painted after the items).
		font := c.EffectiveFont()
		rowPainter := p.WithClip(core.UnitRect{
			X:      0,
			Y:      itemY,
			Width:  bounds.Width,
			Height: metrics.CellHeight,
		})
		rowPainter.DrawText(metrics.CellWidth, itemY, item, s, font)
	}

	// Draw scroll indicators if needed
	if c.scrollOffset > 0 {
		p.DrawCell(bounds.Width-metrics.CellWidth*2, popupY, '▲', itemStyle)
	}
	if c.scrollOffset+popupHeight < len(c.items) {
		endY := popupY + core.Unit(popupHeight-1)*metrics.CellHeight
		p.DrawCell(bounds.Width-metrics.CellWidth*2, endY, '▼', itemStyle)
	}
}

// paintPopupOverlay renders the popup for the overlay system.
// The popup is rendered at its screen position.
func (c *ComboBox) paintPopupOverlay(p *core.Painter, popupBounds core.UnitRect) {
	scheme := c.GetScheme()
	metrics := c.screenMetrics()

	// Frame the originating control with the same 1-pixel separator-color
	// stroke, BEFORE the popup fills, so the popup covers the edge they
	// share and control + popup read as one outline. Uses the unclipped
	// overlay painter and screen-space rect. Graphical only.
	if p.Graphical() && !c.popupControlRect.IsEmpty() {
		lineStyle := style.DefaultStyle().WithBg(scheme.GetMenuSeparator().Fg)
		paintPopupOuterStroke(p, c.popupControlRect, p.DeviceScale(), lineStyle, 0, 0, false)
	}

	// Use a painter offset to the popup position
	popupPainter := p.WithOffset(popupBounds.X, popupBounds.Y)

	// Draw popup background
	localBounds := core.UnitRect{
		X:      0,
		Y:      0,
		Width:  popupBounds.Width,
		Height: popupBounds.Height,
	}
	itemStyle := scheme.GetDropdownItemText()
	popupPainter.FillRect(localBounds, ' ', itemStyle)
	// Cell surfaces get the box-drawing border; graphical surfaces use
	// the 1-pixel outer stroke drawn at the end (the char border's inset
	// line would cut through the popup).
	if !popupPainter.Graphical() {
		popupPainter.DrawRect(localBounds, c.Theme().DefaultBorder, itemStyle)
	}

	maxVis := c.effectiveMaxVisible()
	needsScroll := len(c.items) > maxVis

	// In drag mode with scrolling, first/last rows are scroll indicators
	// In click mode, all rows are items (scrollbar on right side)
	showScrollIndicators := needsScroll && !c.clickMode

	// Calculate visible item count and starting Y position
	visibleItems := maxVis
	if visibleItems > len(c.items) {
		visibleItems = len(c.items)
	}

	startY := core.Unit(0)
	itemCount := visibleItems

	if showScrollIndicators {
		// Reserve first row for scroll up indicator if can scroll up
		if c.canScrollUp() {
			centerX := popupBounds.Width / 2
			popupPainter.DrawCell(centerX-metrics.CellWidth*2, 0, '^', itemStyle)
			popupPainter.DrawCell(centerX, 0, '^', itemStyle)
			popupPainter.DrawCell(centerX+metrics.CellWidth*2, 0, '^', itemStyle)
			startY = metrics.CellHeight
			itemCount--
		}
		// Reserve last row for scroll down indicator if can scroll down
		if c.canScrollDown() {
			itemCount--
		}
	}

	// Draw visible items
	for i := 0; i < itemCount; i++ {
		itemIndex := c.scrollOffset + i
		if itemIndex >= len(c.items) {
			break
		}

		item := c.items[itemIndex]
		itemY := startY + core.Unit(i)*metrics.CellHeight

		// Determine item style - highlight hovered item (or current if no hover)
		var s style.CellStyle
		highlightIndex := c.hoverIndex
		if highlightIndex < 0 {
			highlightIndex = c.currentIndex
		}
		if itemIndex == highlightIndex {
			s = scheme.GetFocusedDropdownItemText()
		} else {
			s = scheme.GetDropdownItemText()
		}

		// Draw item background
		popupPainter.FillRect(core.UnitRect{
			X:      0,
			Y:      itemY,
			Width:  popupBounds.Width,
			Height: metrics.CellHeight,
		}, ' ', s)

		// Draw item text through the proportional path, clipped only
		// at the popup edge - labels flow under the scrollbar lane
		// (painted after the items).
		font := c.EffectiveFont()
		rowPainter := popupPainter.WithClip(core.UnitRect{
			X:      0,
			Y:      itemY,
			Width:  popupBounds.Width,
			Height: metrics.CellHeight,
		})
		rowPainter.DrawText(metrics.CellWidth, itemY, item, s, font)
	}

	// Draw scroll down indicator or scrollbar
	if needsScroll {
		if c.clickMode {
			// In click mode, show a proper scrollbar
			c.paintScrollbar(popupPainter, popupBounds.Width, visibleItems)
		} else if c.canScrollDown() {
			// In drag mode, show scroll down indicator on last row
			endY := core.Unit(visibleItems-1) * metrics.CellHeight
			centerX := popupBounds.Width / 2
			popupPainter.DrawCell(centerX-metrics.CellWidth*2, endY, 'v', itemStyle)
			popupPainter.DrawCell(centerX, endY, 'v', itemStyle)
			popupPainter.DrawCell(centerX+metrics.CellWidth*2, endY, 'v', itemStyle)
		}
	}

	// A 1-pixel frame just outside the popup, in the separator color,
	// with the whole edge nearest the box gapped: the top for a normal
	// drop-down, the bottom for a drop-up (graphical only).
	if popupPainter.Graphical() {
		lineStyle := style.DefaultStyle().WithBg(scheme.GetMenuSeparator().Fg)
		paintPopupOuterStroke(popupPainter, localBounds, popupPainter.DeviceScale(), lineStyle, 0, localBounds.Width, c.popupDropUp)
	}
}

// scrollbarGeometry returns scrollbar dimensions and thumb position.
// Returns: scrollbarX, thumbStart, thumbHeight, trackHeight (all in rows)
func (c *ComboBox) scrollbarGeometry(popupWidth core.Unit, visibleCount int) (scrollbarX core.Unit, thumbStart, thumbHeight, trackHeight int) {
	metrics := c.screenMetrics()
	totalItems := len(c.items)

	scrollbarX = popupWidth - metrics.CellWidth
	trackHeight = visibleCount

	if totalItems <= visibleCount {
		// No scrolling needed - thumb fills track
		thumbStart = 0
		thumbHeight = trackHeight
		return
	}

	// Calculate thumb size proportional to visible/total ratio
	thumbHeight = visibleCount * visibleCount / totalItems
	if thumbHeight < 1 {
		thumbHeight = 1
	}

	// Calculate thumb position based on scroll offset
	maxScroll := totalItems - visibleCount
	if maxScroll > 0 {
		scrollableTrack := trackHeight - thumbHeight
		thumbStart = c.scrollOffset * scrollableTrack / maxScroll

		// Ensure thumb doesn't show at top unless actually at top (no content above)
		if c.scrollOffset > 0 && thumbStart == 0 {
			thumbStart = 1
		}

		// Ensure thumb doesn't show at bottom unless actually at bottom (no content below)
		maxThumbStart := trackHeight - thumbHeight
		if c.scrollOffset < maxScroll && thumbStart >= maxThumbStart {
			thumbStart = maxThumbStart - 1
			if thumbStart < 1 && c.scrollOffset > 0 {
				thumbStart = 1 // Don't push to top if there's content above
			}
		}

		if thumbStart+thumbHeight > trackHeight {
			thumbStart = trackHeight - thumbHeight
		}
	}

	return
}

// popupScrollbarUnits returns the popup lane geometry in units for
// pixel surfaces: track length, thumb length, and thumb origin -
// the same proportions as scrollbarGeometry without row quantization.
// Mid-drag the thumb origin is the smooth (pointer-tracked) position.
func (c *ComboBox) popupScrollbarUnits(visibleCount int) (trackU, thumbU, posU float64) {
	metrics := c.screenMetrics()
	trackU = float64(core.Unit(visibleCount) * metrics.CellHeight)
	totalItems := len(c.items)
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
	if c.scrollbarDragging && c.sbSmoothDrag {
		posU = c.sbThumbPos
	} else if maxScroll > 0 {
		posU = float64(c.scrollOffset) * scrollable / float64(maxScroll)
	}
	if posU < 0 {
		posU = 0
	}
	if posU > scrollable {
		posU = scrollable
	}
	return trackU, thumbU, posU
}

// paintScrollbar draws a vertical scrollbar for the popup.
func (c *ComboBox) paintScrollbar(p *core.Painter, popupWidth core.Unit, visibleCount int) {
	scheme := c.GetScheme()
	metrics := c.screenMetrics()

	trackStyle := scheme.GetDropdownScrollbar()
	thumbStyle := scheme.GetDropdownScrollbarThumb()

	// Pixel surfaces: one hairline stripe at 50% opacity behind (so
	// it stays subtle over item text) and one solid full-opacity
	// rectangle for the thumb, both at unit granularity.
	if p.Graphical() {
		trackU, thumbU, posU := c.popupScrollbarUnits(visibleCount)
		laneX := popupWidth - metrics.CellWidth
		stripeX := laneX + metrics.CellWidth/2
		p.FillRect(core.UnitRect{
			X:      stripeX,
			Y:      0,
			Width:  1,
			Height: core.Unit(trackU + 0.5),
		}, '▒', trackStyle.WithBg(style.ColorTransparent))
		p.FillRect(core.UnitRect{
			X:      laneX + 1,
			Y:      core.Unit(posU + 0.5),
			Width:  metrics.CellWidth - 2,
			Height: core.Unit(thumbU + 0.5),
		}, ' ', thumbStyle.WithBg(thumbStyle.Fg))
		return
	}

	scrollbarX, thumbStart, thumbHeight, trackHeight := c.scrollbarGeometry(popupWidth, visibleCount)

	// Draw scrollbar track
	for i := 0; i < trackHeight; i++ {
		y := core.Unit(i) * metrics.CellHeight
		p.DrawCell(scrollbarX, y, '│', trackStyle)
	}

	// Draw scrollbar thumb
	for i := 0; i < thumbHeight; i++ {
		y := core.Unit(thumbStart+i) * metrics.CellHeight
		p.DrawCell(scrollbarX, y, '█', thumbStyle)
	}
}

// handlePopupMouseWheel scrolls the open popup by whole rows.
func (c *ComboBox) handlePopupMouseWheel(event core.MouseWheelEvent) bool {
	maxVis := c.effectiveMaxVisible()
	maxScroll := len(c.items) - maxVis
	if maxScroll <= 0 {
		return false
	}
	step := event.DeltaY
	if step == 0 && event.PreciseY != 0 {
		c.wheelAccum += event.PreciseY * 3
		step = int(c.wheelAccum)
		c.wheelAccum -= float64(step)
	} else {
		step *= 3
	}
	off := c.scrollOffset + step
	if off < 0 {
		off = 0
	}
	if off > maxScroll {
		off = maxScroll
	}
	if off != c.scrollOffset {
		c.scrollOffset = off
		c.Update()
	}
	return true
}

// handlePopupMousePress handles mouse clicks on the popup overlay.
// This is called when clicking within an already-open popup (click mode).
func (c *ComboBox) handlePopupMousePress(event core.MousePressEvent, popupBounds core.UnitRect) bool {
	if event.Button != core.LeftButton {
		return false
	}

	metrics := c.screenMetrics()

	// Check if the click is within the popup bounds
	if event.X >= popupBounds.X && event.X < popupBounds.X+popupBounds.Width &&
		event.Y >= popupBounds.Y && event.Y < popupBounds.Y+popupBounds.Height {

		// Check for scrollbar clicks in click mode
		maxVis := c.effectiveMaxVisible()
		if c.clickMode && len(c.items) > maxVis {
			popupHeight := maxVis
			if popupHeight > len(c.items) {
				popupHeight = len(c.items)
			}

			scrollbarX, thumbStart, thumbHeight, _ := c.scrollbarGeometry(popupBounds.Width, popupHeight)
			if event.X >= popupBounds.X+scrollbarX {
				// Click on scrollbar area
				relY := event.Y - popupBounds.Y
				clickedRow := int(relY / metrics.CellHeight)

				// Pixel surfaces anchor the drag to the grab point
				// within the unit-granular thumb.
				if core.FindSmoothPositioning(c.Self()) {
					_, thumbU, posU := c.popupScrollbarUnits(popupHeight)
					pos := float64(relY)
					if pos >= posU && pos < posU+thumbU {
						c.scrollbarDragging = true
						c.sbSmoothDrag = true
						c.sbGrabOff = pos - posU
						c.sbThumbPos = posU
						c.mouseDown = true
						return true
					}
					// Track press falls through to page up/down
					// below, keyed off the smooth thumb position.
					if pos < posU {
						clickedRow = thumbStart - 1
					} else {
						clickedRow = thumbStart + thumbHeight
					}
				}

				// Check if click is on thumb - start drag
				if clickedRow >= thumbStart && clickedRow < thumbStart+thumbHeight {
					c.scrollbarDragging = true
					c.scrollbarDragStartY = clickedRow
					c.scrollbarDragOffset = c.scrollOffset
					c.mouseDown = true
					return true
				}

				// Click on track - page up or down
				visibleCount := popupHeight
				if clickedRow < thumbStart && c.canScrollUp() {
					// Page up
					newOffset := c.scrollOffset - visibleCount
					if newOffset < 0 {
						newOffset = 0
					}
					c.scrollOffset = newOffset
					c.Update()
				} else if clickedRow >= thumbStart+thumbHeight && c.canScrollDown() {
					// Page down
					maxScroll := len(c.items) - visibleCount
					newOffset := c.scrollOffset + visibleCount
					if newOffset > maxScroll {
						newOffset = maxScroll
					}
					c.scrollOffset = newOffset
					c.Update()
				}
				return true
			}
		}

		// Calculate which item was pressed
		relY := event.Y - popupBounds.Y
		rowIndex := int(relY / metrics.CellHeight)

		// In drag mode with scrolling, adjust for scroll up indicator
		needsScroll := len(c.items) > c.effectiveMaxVisible()
		if !c.clickMode && needsScroll && c.scrollOffset > 0 {
			rowIndex-- // First row is scroll up indicator
		}

		actualIndex := c.scrollOffset + rowIndex

		if actualIndex >= 0 && actualIndex < len(c.items) {
			c.hoverIndex = actualIndex
			c.mouseDown = true
			c.mouseDownX = event.X
			c.mouseDownY = event.Y
			c.dragging = false
			c.Update()
		}
		return true
	}

	// Click was outside popup - close it and cancel
	if !c.clickMode {
		// Drag mode - restore original
		c.SetCurrentIndex(c.originalIndex)
	}
	c.HidePopup()
	return true
}

// handlePopupMouseMove handles mouse movement on the popup overlay.
func (c *ComboBox) handlePopupMouseMove(event core.MouseMoveEvent, popupBounds core.UnitRect) bool {
	metrics := c.screenMetrics()
	maxVis := c.effectiveMaxVisible()
	popupHeight := maxVis
	if popupHeight > len(c.items) {
		popupHeight = len(c.items)
	}

	// Handle scrollbar thumb dragging first
	if c.scrollbarDragging {
		relY := event.Y - popupBounds.Y

		// Smooth drag: the thumb follows the pointer in units, the
		// scroll offset snaps to the nearest whole item.
		if c.sbSmoothDrag {
			visibleCount := popupHeight
			trackU, thumbU, _ := c.popupScrollbarUnits(visibleCount)
			scrollable := trackU - thumbU
			pos := float64(relY) - c.sbGrabOff
			if pos < 0 {
				pos = 0
			}
			if pos > scrollable {
				pos = scrollable
			}
			c.sbThumbPos = pos
			maxScroll := len(c.items) - visibleCount
			newOffset := 0
			if scrollable > 0 && maxScroll > 0 {
				newOffset = int(pos*float64(maxScroll)/scrollable + 0.5)
			}
			c.scrollOffset = newOffset
			// The thumb moves even when the snapped offset does not.
			c.Update()
			return true
		}

		currentRow := int(relY / metrics.CellHeight)
		rowDelta := currentRow - c.scrollbarDragStartY

		visibleCount := popupHeight
		totalItems := len(c.items)
		maxScroll := totalItems - visibleCount

		if maxScroll > 0 {
			_, _, thumbHeight, trackHeight := c.scrollbarGeometry(popupBounds.Width, visibleCount)
			scrollableTrack := trackHeight - thumbHeight

			if scrollableTrack > 0 {
				// Convert row delta to scroll offset delta
				scrollDelta := rowDelta * maxScroll / scrollableTrack
				newOffset := c.scrollbarDragOffset + scrollDelta

				// Clamp
				if newOffset < 0 {
					newOffset = 0
				} else if newOffset > maxScroll {
					newOffset = maxScroll
				}

				if c.scrollOffset != newOffset {
					c.scrollOffset = newOffset
					c.Update()
				}
			}
		}
		return true
	}

	// Calculate relative position to popup
	relX := event.X - popupBounds.X
	relY := event.Y - popupBounds.Y

	// Check if we need to start dragging (detects movement beyond threshold)
	if c.mouseDown && !c.dragging {
		dx := event.X - c.mouseDownX
		dy := event.Y - c.mouseDownY
		if dx < 0 {
			dx = -dx
		}
		if dy < 0 {
			dy = -dy
		}
		threshold := metrics.CellWidth / 2
		if dx > threshold || dy > threshold {
			c.dragging = true
		}
	}

	// Check if mouse is within the popup X bounds (for scroll handling)
	inXBounds := relX >= 0 && relX < popupBounds.Width

	// While the pointer is still over the combobox itself (a popup
	// may open above it), never auto-scroll: the user hasn't entered
	// the list yet.
	overSelf := false
	if pc := c.findPopupController(); pc != nil {
		topLeft := c.mapToScreen(pc, core.UnitPoint{})
		selfH := c.Bounds().Height
		if selfH <= 0 {
			selfH = c.screenMetrics().CellHeight
		}
		selfRect := core.UnitRect{X: topLeft.X, Y: topLeft.Y, Width: c.Bounds().Width, Height: selfH}
		overSelf = selfRect.Contains(core.UnitPoint{X: event.X, Y: event.Y})
	}
	if overSelf {
		c.scrollHoverZone = 0
		c.stopScrollTimer()
	}

	// Handle mouse above popup - scroll up
	if relY < 0 && inXBounds && !overSelf && c.canScrollUp() {
		if c.scrollHoverZone != -1 {
			c.scrollHoverZone = -1
			c.scrollUp(1)
			c.startScrollTimer(-1)
		}
		// Keep the topmost visible item highlighted
		if c.hoverIndex != c.scrollOffset {
			c.hoverIndex = c.scrollOffset
			c.Update()
		}
		return true
	}

	// Handle mouse below popup - scroll down
	if relY >= popupBounds.Height && inXBounds && !overSelf && c.canScrollDown() {
		if c.scrollHoverZone != 1 {
			c.scrollHoverZone = 1
			c.scrollDown(1)
			c.startScrollTimer(1)
		}
		// Keep the bottommost visible item highlighted
		lastVisible := c.scrollOffset + popupHeight - 1
		if lastVisible >= len(c.items) {
			lastVisible = len(c.items) - 1
		}
		if c.hoverIndex != lastVisible {
			c.hoverIndex = lastVisible
			c.Update()
		}
		return true
	}

	// Mouse left or right of popup - keep nearest item selected
	if !inXBounds {
		c.scrollHoverZone = 0
		c.stopScrollTimer()
		// Don't change hoverIndex - keep current selection visible
		return c.mouseDown // Consume if dragging
	}

	// Mouse is within the popup bounds
	needsScroll := len(c.items) > maxVis
	showScrollIndicators := needsScroll && !c.clickMode

	// In drag mode with scrolling, check scroll indicator rows
	if showScrollIndicators {
		// First row is scroll up indicator if can scroll up
		if c.canScrollUp() && relY < metrics.CellHeight {
			if c.scrollHoverZone != -1 {
				c.scrollHoverZone = -1
				c.scrollUp(1)
				c.startScrollTimer(-1)
			}
			// Keep topmost visible item highlighted
			if c.hoverIndex != c.scrollOffset {
				c.hoverIndex = c.scrollOffset
				c.Update()
			}
			return true
		}
		// Last row is scroll down indicator if can scroll down
		if c.canScrollDown() && relY >= core.Unit(popupHeight-1)*metrics.CellHeight {
			if c.scrollHoverZone != 1 {
				c.scrollHoverZone = 1
				c.scrollDown(1)
				c.startScrollTimer(1)
			}
			// Keep bottommost visible item highlighted
			lastVisible := c.scrollOffset + c.visibleItemCount() - 1
			if lastVisible >= len(c.items) {
				lastVisible = len(c.items) - 1
			}
			if c.hoverIndex != lastVisible {
				c.hoverIndex = lastVisible
				c.Update()
			}
			return true
		}
	}

	// Not on a scroll indicator - stop scrolling
	c.scrollHoverZone = 0
	c.stopScrollTimer()

	// Calculate which item row the mouse is on
	// In drag mode with scroll indicators, adjust for the top indicator row
	rowIndex := int(relY / metrics.CellHeight)
	if showScrollIndicators && c.canScrollUp() {
		rowIndex-- // First row is scroll indicator, so subtract 1
	}
	actualIndex := c.scrollOffset + rowIndex

	if actualIndex >= 0 && actualIndex < len(c.items) {
		if c.hoverIndex != actualIndex {
			c.hoverIndex = actualIndex
			c.Update()
		}
	}
	return true
}

// handlePopupMouseRelease handles mouse release on the popup overlay.
func (c *ComboBox) handlePopupMouseRelease(event core.MouseReleaseEvent, popupBounds core.UnitRect) bool {
	if event.Button != core.LeftButton {
		return false
	}

	c.stopScrollTimer()
	wasMouseDown := c.mouseDown
	wasDragging := c.dragging
	wasClickMode := c.clickMode
	wasScrollbarDragging := c.scrollbarDragging

	c.mouseDown = false
	c.dragging = false
	c.scrollbarDragging = false
	c.sbSmoothDrag = false

	// If we were dragging the scrollbar, just stop - don't process as item selection
	if wasScrollbarDragging {
		return true
	}

	metrics := c.screenMetrics()

	// Check if the release is within the popup bounds
	inPopup := event.X >= popupBounds.X && event.X < popupBounds.X+popupBounds.Width &&
		event.Y >= popupBounds.Y && event.Y < popupBounds.Y+popupBounds.Height

	if inPopup {
		// Calculate which item was released on
		relY := event.Y - popupBounds.Y
		rowIndex := int(relY / metrics.CellHeight)

		// In drag mode with scrolling, adjust for scroll up indicator
		needsScroll := len(c.items) > c.effectiveMaxVisible()
		if !wasClickMode && needsScroll && c.scrollOffset > 0 {
			rowIndex-- // First row is scroll up indicator
		}

		actualIndex := c.scrollOffset + rowIndex

		if actualIndex >= 0 && actualIndex < len(c.items) {
			// In click mode with no drag, this confirms selection
			// In drag mode, this also confirms selection
			if wasClickMode && !wasDragging && wasMouseDown {
				// Click mode: second click confirms
				c.SetCurrentIndex(actualIndex)
				if c.onActivated != nil {
					c.onActivated(actualIndex)
				}
				c.HidePopup()
				return true
			} else if !wasClickMode {
				// Drag mode: release inside confirms
				c.SetCurrentIndex(actualIndex)
				if c.onActivated != nil {
					c.onActivated(actualIndex)
				}
				c.HidePopup()
				return true
			}
		}
		// Click mode but didn't click an item - stay open
		return true
	}

	// Release was outside popup
	if wasDragging {
		// Was dragging (in either mode) and released outside - cancel
		c.SetCurrentIndex(c.originalIndex)
		c.HidePopup()
	} else if wasClickMode {
		// In click mode, released outside without drag - dismiss
		c.HidePopup()
	} else {
		// Not in click mode, released outside without drag - enter click mode
		// This is the "click to open" case - popup should stay open
		c.enterClickMode()
	}

	return true
}

// HandleKeyPress handles keyboard input.
func (c *ComboBox) HandleKeyPress(event core.KeyPressEvent) bool {
	if c.isOpen {
		return c.handlePopupKeyPress(event)
	}

	switch event.Key {
	case " ", "Space", "Enter":
		c.clickMode = true // Keyboard invocation opens in click mode
		c.ShowPopup()
		c.enterClickMode() // Ensure scroll offset is clamped for click mode
		return true

	case "Up":
		if c.currentIndex > 0 {
			c.SetCurrentIndex(c.currentIndex - 1)
		}
		return true

	case "Down":
		if c.currentIndex < len(c.items)-1 {
			c.SetCurrentIndex(c.currentIndex + 1)
		}
		return true

	case "Home":
		if len(c.items) > 0 {
			c.SetCurrentIndex(0)
		}
		return true

	case "End":
		if len(c.items) > 0 {
			c.SetCurrentIndex(len(c.items) - 1)
		}
		return true

	case "F4", "M-Down":
		c.clickMode = true // Keyboard invocation opens in click mode
		c.ShowPopup()
		c.enterClickMode() // Ensure scroll offset is clamped for click mode
		return true
	}

	return false
}

// handlePopupKeyPress handles key events when popup is open.
func (c *ComboBox) handlePopupKeyPress(event core.KeyPressEvent) bool {
	// Use hoverIndex for navigation, fall back to currentIndex if not set
	getEffectiveIndex := func() int {
		if c.hoverIndex >= 0 {
			return c.hoverIndex
		}
		return c.currentIndex
	}

	switch event.Key {
	case "Escape":
		// Cancel - restore original and close
		c.SetCurrentIndex(c.originalIndex)
		c.HidePopup()
		return true

	case "Enter", " ", "Space":
		// Confirm selection - use hover index if set
		selectedIndex := getEffectiveIndex()
		if selectedIndex >= 0 {
			c.SetCurrentIndex(selectedIndex)
			if c.onActivated != nil {
				c.onActivated(selectedIndex)
			}
		}
		c.HidePopup()
		return true

	case "Up":
		idx := getEffectiveIndex()
		if idx > 0 {
			c.hoverIndex = idx - 1
			c.ensureVisible(c.hoverIndex)
			c.announceHoverItem()
			c.Update()
		}
		return true

	case "Down":
		idx := getEffectiveIndex()
		if idx < len(c.items)-1 {
			c.hoverIndex = idx + 1
			c.ensureVisible(c.hoverIndex)
			c.announceHoverItem()
			c.Update()
		}
		return true

	case "PageUp":
		idx := getEffectiveIndex()
		newIndex := idx - c.effectiveMaxVisible()
		if newIndex < 0 {
			newIndex = 0
		}
		c.hoverIndex = newIndex
		c.ensureVisible(newIndex)
		c.announceHoverItem()
		c.Update()
		return true

	case "PageDown":
		idx := getEffectiveIndex()
		newIndex := idx + c.effectiveMaxVisible()
		if newIndex >= len(c.items) {
			newIndex = len(c.items) - 1
		}
		c.hoverIndex = newIndex
		c.ensureVisible(newIndex)
		c.announceHoverItem()
		c.Update()
		return true

	case "Home":
		c.hoverIndex = 0
		c.scrollOffset = 0
		c.announceHoverItem()
		c.Update()
		return true

	case "End":
		c.hoverIndex = len(c.items) - 1
		c.ensureVisible(len(c.items) - 1)
		c.announceHoverItem()
		c.Update()
		return true
	}

	return false
}

// announceHoverItem announces the currently hovered item in the popup for accessibility.
func (c *ComboBox) announceHoverItem() {
	if c.hoverIndex < 0 || c.hoverIndex >= len(c.items) {
		return
	}
	if am := core.FindAccessibilityManager(c); am != nil {
		am.AnnouncePolite(fmt.Sprintf("%s, %d of %d", c.items[c.hoverIndex], c.hoverIndex+1, len(c.items)))
	}
}

// ensureVisible ensures the given index is visible in the popup.
func (c *ComboBox) ensureVisible(index int) {
	maxVis := c.effectiveMaxVisible()
	if index < c.scrollOffset {
		c.scrollOffset = index
	} else {
		// Calculate effective visible count accounting for scroll indicators in drag mode
		effectiveVisible := maxVis
		if !c.clickMode && len(c.items) > maxVis {
			// In drag mode with scrolling, indicators take rows
			if c.scrollOffset > 0 {
				effectiveVisible-- // up indicator takes a row
			}
			// If scrolling down would show more items, down indicator also takes a row
			if c.scrollOffset+effectiveVisible < len(c.items) {
				effectiveVisible--
			}
		}
		if index >= c.scrollOffset+effectiveVisible {
			// Calculate the new scroll offset needed to show this index.
			// We need to account for the fact that scrolling may cause an up indicator
			// to appear (if scrollOffset was 0 and becomes > 0), which reduces visible items.
			newOffset := index - effectiveVisible + 1

			// If scrolling from offset 0, an up indicator will appear, taking another row
			if !c.clickMode && len(c.items) > maxVis && c.scrollOffset == 0 && newOffset > 0 {
				newOffset++ // Account for the up indicator that will appear
			}

			c.scrollOffset = newOffset
		}
	}
	c.Update()
}

// HandleMousePress handles mouse clicks on the combobox trinket itself.
func (c *ComboBox) HandleMousePress(event core.MousePressEvent) bool {
	if event.Button != core.LeftButton {
		return false
	}

	c.SetFocus()
	metrics := c.EffectiveCellMetrics()
	bounds := c.Bounds()

	// Check if click is on the main combobox area (first row)
	if event.Y < metrics.CellHeight && event.X >= 0 && event.X < bounds.Width {
		if c.isOpen {
			if c.clickMode {
				// Click mode: click on button toggles closed
				c.HidePopup()
				return true
			}
			// Drag mode: click on button while open starts drag from button
			c.mouseDown = true
			c.mouseDownX = event.X
			c.mouseDownY = event.Y
			c.dragging = false
			return true
		}

		// Not open - open popup and start drag mode
		c.mouseDown = true
		c.mouseDownX = event.X
		c.mouseDownY = event.Y
		c.dragging = false
		c.clickMode = false // Will switch to click mode on release without drag
		c.ShowPopup()
		return true
	}

	// If popup is open and click is in popup area, let popup handler deal with it
	if c.isOpen {
		popupY := metrics.CellHeight
		popupHeight := c.effectiveMaxVisible()
		if popupHeight > len(c.items) {
			popupHeight = len(c.items)
		}

		if event.Y >= popupY && event.Y < popupY+core.Unit(popupHeight)*metrics.CellHeight {
			// Clicked in popup area (fallback for non-overlay mode)
			itemIndex := int((event.Y - popupY) / metrics.CellHeight)
			actualIndex := c.scrollOffset + itemIndex

			if actualIndex >= 0 && actualIndex < len(c.items) {
				c.hoverIndex = actualIndex
				c.mouseDown = true
				c.mouseDownX = event.X
				c.mouseDownY = event.Y
				c.dragging = false
				c.Update()
			}
			return true
		}

		// Clicked outside popup - cancel
		if !c.clickMode {
			c.SetCurrentIndex(c.originalIndex)
		}
		c.HidePopup()
		return true
	}

	return true
}

// HandleMouseMove handles mouse movement while button may be held.
func (c *ComboBox) HandleMouseMove(event core.MouseMoveEvent) bool {
	if !c.mouseDown || !c.isOpen {
		return false
	}

	metrics := c.EffectiveCellMetrics()
	bounds := c.Bounds()

	// Detect if we've started dragging
	if !c.dragging {
		dx := event.X - c.mouseDownX
		dy := event.Y - c.mouseDownY
		if dx < 0 {
			dx = -dx
		}
		if dy < 0 {
			dy = -dy
		}
		threshold := metrics.CellWidth / 2
		if dx > threshold || dy > threshold {
			c.dragging = true
		} else {
			return true // Not dragging yet
		}
	}

	// Calculate popup bounds for hit testing
	popupY := metrics.CellHeight
	popupHeight := c.effectiveMaxVisible()
	if popupHeight > len(c.items) {
		popupHeight = len(c.items)
	}
	popupEndY := popupY + core.Unit(popupHeight)*metrics.CellHeight

	// Handle scrolling when dragging above/below popup
	if event.Y < popupY && event.X >= 0 && event.X < bounds.Width && c.canScrollUp() {
		// Above popup - scroll up
		if c.scrollHoverZone != -1 {
			c.scrollHoverZone = -1
			c.scrollUp(1)
			c.startScrollTimer(-1)
		}
		c.hoverIndex = c.scrollOffset
		c.Update()
		return true
	}

	if event.Y >= popupEndY && event.X >= 0 && event.X < bounds.Width && c.canScrollDown() {
		// Below popup - scroll down
		if c.scrollHoverZone != 1 {
			c.scrollHoverZone = 1
			c.scrollDown(1)
			c.startScrollTimer(1)
		}
		lastVisible := c.scrollOffset + popupHeight - 1
		if lastVisible >= len(c.items) {
			lastVisible = len(c.items) - 1
		}
		c.hoverIndex = lastVisible
		c.Update()
		return true
	}

	// Within popup area
	if event.Y >= popupY && event.Y < popupEndY && event.X >= 0 && event.X < bounds.Width {
		c.scrollHoverZone = 0
		c.stopScrollTimer()

		itemIndex := int((event.Y - popupY) / metrics.CellHeight)
		actualIndex := c.scrollOffset + itemIndex

		if actualIndex >= 0 && actualIndex < len(c.items) {
			if c.hoverIndex != actualIndex {
				c.hoverIndex = actualIndex
				c.Update()
			}
		}
		return true
	}

	// Off to the side - stop scrolling but keep selection
	c.scrollHoverZone = 0
	c.stopScrollTimer()
	return true
}

// HandleMouseRelease handles mouse button release.
func (c *ComboBox) HandleMouseRelease(event core.MouseReleaseEvent) bool {
	if event.Button != core.LeftButton {
		return false
	}

	if !c.isOpen {
		c.mouseDown = false
		c.dragging = false
		return false
	}

	c.stopScrollTimer()
	wasMouseDown := c.mouseDown
	wasDragging := c.dragging

	c.mouseDown = false
	c.dragging = false

	metrics := c.EffectiveCellMetrics()
	bounds := c.Bounds()

	// Calculate popup bounds
	popupY := metrics.CellHeight
	popupHeight := c.effectiveMaxVisible()
	if popupHeight > len(c.items) {
		popupHeight = len(c.items)
	}
	popupEndY := popupY + core.Unit(popupHeight)*metrics.CellHeight

	// Check if release is within popup
	inPopup := event.Y >= popupY && event.Y < popupEndY &&
		event.X >= 0 && event.X < bounds.Width

	if inPopup && wasMouseDown {
		if wasDragging {
			// Drag mode - release inside confirms
			itemIndex := int((event.Y - popupY) / metrics.CellHeight)
			actualIndex := c.scrollOffset + itemIndex
			if actualIndex >= 0 && actualIndex < len(c.items) {
				c.SetCurrentIndex(actualIndex)
				if c.onActivated != nil {
					c.onActivated(actualIndex)
				}
			}
			c.HidePopup()
			return true
		} else {
			// Click without drag on combobox - switch to click mode
			// User clicked and released on combobox without moving
			// Popup should stay open for click-to-select
			c.enterClickMode()
			return true
		}
	}

	// Release on the combobox button itself (not popup)
	if event.Y < popupY && event.X >= 0 && event.X < bounds.Width && wasMouseDown && !wasDragging {
		// Quick click on combobox - switch to click mode
		c.enterClickMode()
		return true
	}

	// Release outside - cancel if dragging, otherwise switch to click mode
	if wasDragging {
		c.SetCurrentIndex(c.originalIndex)
		c.HidePopup()
	} else if wasMouseDown {
		// Released outside without dragging - just switch to click mode
		c.enterClickMode()
	}

	return true
}

// HandleFocusIn is called when focus is gained.
func (c *ComboBox) HandleFocusIn() {
	c.Update()
}

// HandleFocusOut is called when focus is lost.
func (c *ComboBox) HandleFocusOut() {
	c.HidePopup()
	c.Update()
}

// AccessibleInfo returns accessibility information.
func (c *ComboBox) AccessibleInfo() core.AccessibleInfo {
	info := c.AccessibleTrinket.AccessibleInfo()
	info.Role = core.RoleComboBox
	info.Value = c.CurrentText()
	info.SetSize = len(c.items)

	if c.currentIndex >= 0 {
		info.PositionInSet = c.currentIndex + 1
	}

	if c.isOpen {
		info.State |= core.StateExpanded
	} else {
		info.State |= core.StateCollapsed
	}

	if !c.IsEnabled() {
		info.State |= core.StateDisabled
	}

	return info
}
