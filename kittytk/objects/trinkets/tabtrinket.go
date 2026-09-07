// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"math"

	"fmt"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
	"github.com/phroun/kittytk/text"
)

// TabTrinket displays multiple pages with tabs.
type TabTrinket struct {
	core.TrinketBase
	core.TrinketKeys
	core.AccessibleTrinket

	tabs         []*Tab
	currentIndex int

	// Tab bar position
	tabPosition TabPosition
	tabAlign    TabAlign

	// Appearance
	movable       bool // Can tabs be reordered
	closable      bool // Can tabs be closed
	showSeparator bool // Show separator between tab bar and content

	// Tab scrolling for horizontal tabs (when tabs don't fit)
	tabScrollOffset     int  // First visible tab index
	scrollLeftHovered   bool // Mouse over [<] button while pressed
	scrollRightHovered  bool // Mouse over [>] button while pressed
	scrollButtonPressed int  // 0=none, -1=left, 1=right

	// Tab scrolling for vertical tabs
	vertScrollOffset      int  // First visible tab index for vertical tabs
	scrollbarDragging     bool // Whether scrollbar thumb is being dragged
	scrollbarThumbHovered bool // Whether the pointer is over the thumb
	scrollbarDragStart    int  // Row where drag started
	scrollbarDragOffset   int  // Scroll offset when drag started
	vertTabDragging       bool // Whether dragging over vertical tabs (sweep selection)

	// Smooth (pixel-surface) vertical tab scrollbar drag: the thumb
	// follows the pointer at unit granularity while the first visible
	// tab snaps to whole items.
	smoothVertSbDrag bool
	vertSbGrabOff    float64
	vertSbThumbPos   float64

	// Where the parts of the horizontal strip landed the last time it was
	// painted, in the strip's RUN coordinates (see the display list below).
	// The mouse reads it, so a press finds a tab where the painter put it.
	stripSpans []stripSpan

	// Callbacks
	onCurrentChanged    func(index int)
	onTabCloseRequested func(index int)
}

// Tab represents a single tab in a TabTrinket.
type Tab struct {
	Text     string
	Icon     *style.TextIcon
	Content  core.Trinket
	Enabled  bool
	Closable bool // Per-tab closable setting
	Data     interface{}
}

// TabPosition is where a tab strip is asked to stand.
//
// top and bottom name an edge no direction moves. The other four are sides,
// and a side is a question the direction answers: TabsSide and
// TabsSideOpposite are stated against it, while the optical pair names a side
// of the SCREEN outright -- spelled at length, as in the align vocabulary, so
// that pinning one reads as a decision rather than an oversight.
type TabPosition int

const (
	TabsTop TabPosition = iota
	TabsBottom
	// TabsSide is the side the direction reads from: the left of a form
	// reading left to right, the right of one reading the other way.
	TabsSide
	// TabsSideOpposite is the far side from that -- an unusual choice, and a
	// valid one: it stands the strip where the content does not begin.
	TabsSideOpposite
	// TabsOpticalLeft is the left, whatever any direction says.
	TabsOpticalLeft
	// TabsOpticalRight is the right, whatever any direction says.
	TabsOpticalRight
)

// TabAlign is where a strip's tabs sit along it when they all fit.
//
// A strip that has to scroll has no slack to place: the tabs start where the
// run starts and the overflow marks take the rest. Where they DO all fit,
// this says which end the spare room goes to -- and since the run itself is
// what turns over, natural and opposite are stated against it rather than
// against the screen.
type TabAlign int

const (
	// TabsAlignNatural packs the tabs at the end the run starts from, and the
	// slack falls at the other. The usual arrangement.
	TabsAlignNatural TabAlign = iota
	// TabsAlignCenter splits the slack evenly, so the tabs sit in the middle.
	TabsAlignCenter
	// TabsAlignOpposite packs them at the far end, the slack falling where
	// they would ordinarily start.
	TabsAlignOpposite
)

// SetTabAlign says where the tabs sit along a strip that has room to spare.
func (t *TabTrinket) SetTabAlign(a TabAlign) {
	t.tabAlign = a
	t.Update()
}

// tabRunOffset is how far into the strip the run of tabs begins.
//
// Only a strip whose tabs ALL fit has anything to place: one that scrolls
// spends its slack on the overflow marks, and shifting the run there would
// move tabs out from under the very buttons that scroll them.
func (t *TabTrinket) tabRunOffset() core.Unit {
	if t.tabAlign == TabsAlignNatural {
		return 0
	}
	// Slack is what is left of the strip once the tabs have had their room,
	// so a strip with none of it is exactly a strip that has to scroll --
	// which is the case that must not move, since shifting the run there
	// would carry tabs out from under the buttons that scroll them.
	slack := t.Bounds().Width - t.calculateTotalTabsWidth()
	if slack <= 0 {
		return 0
	}
	if t.tabAlign == TabsAlignCenter {
		return slack / 2
	}
	return slack
}

// TabEdge is a tab strip's position with the direction already SPENT: which
// edge of the box it actually stands on.
//
// The painter and the hit test take this and never a TabPosition, the same way
// a backend takes an HSide and never an HAlign -- a direction is resolved once,
// where the trinket tree can be walked, and everything downstream reads a
// plain edge.
type TabEdge int

const (
	TabEdgeTop TabEdge = iota
	TabEdgeBottom
	TabEdgeLeft
	TabEdgeRight
)

// tabEdge resolves this strip's position into the edge it stands on.
func (t *TabTrinket) tabEdge() TabEdge {
	switch t.tabPosition {
	case TabsBottom:
		return TabEdgeBottom
	case TabsOpticalLeft:
		return TabEdgeLeft
	case TabsOpticalRight:
		return TabEdgeRight
	case TabsSide:
		if core.ChromeMirrored(t) {
			return TabEdgeRight
		}
		return TabEdgeLeft
	case TabsSideOpposite:
		if core.ChromeMirrored(t) {
			return TabEdgeLeft
		}
		return TabEdgeRight
	}
	return TabEdgeTop
}

// onSide reports whether the strip runs DOWN an edge rather than across one,
// which is what decides which arrows it answers and which paint path it takes.
func (t *TabTrinket) onSide() bool {
	e := t.tabEdge()
	return e == TabEdgeLeft || e == TabEdgeRight
}

// NewTabTrinket creates a new tab trinket.
func NewTabTrinket() *TabTrinket {
	t := &TabTrinket{
		currentIndex: -1,
		tabPosition:  TabsTop,
	}
	t.TrinketBase = *core.NewTrinketBase()
	// A tab strip walks along one axis, so it answers to the arrows that
	// point that way and to the sequence synonyms alike; the beginning/end
	// pair is its first and last tab. The MDI cycle is Ctrl+Tab and
	// Ctrl+PageUp/Down, which reach the innermost strip first -- an inner tab
	// shadows the outer one when both would answer.
	t.SetCommands(
		core.CmdTrinketItemLeft, core.CmdTrinketItemRight,
		core.CmdTrinketItemUp, core.CmdTrinketItemDown,
		core.CmdTrinketItemPrior, core.CmdTrinketItemNext,
		core.CmdTrinketBeg, core.CmdTrinketEnd,
		core.CmdWindowMDINext, core.CmdWindowMDIPrior,
	)
	t.Init(t)
	// TabTrinket can receive focus for tab bar keyboard navigation
	t.SetFocusPolicy(core.TabFocus)
	t.SetFurtive(true) // Furtive: no focus on click, skip for initial focus
	t.SetAccessibleRole(core.RoleTabList)
	return t
}

// SetBounds sets the trinket's bounds and updates content layout.
func (t *TabTrinket) SetBounds(bounds core.UnitRect) {
	oldSize := t.Size()
	t.TrinketBase.SetBounds(bounds)
	newSize := bounds.Size()
	// Manually call our HandleResize since embedded SetBounds won't do it
	if oldSize != newSize {
		t.HandleResize(oldSize, newSize)
	}
	// Always relayout when bounds are set
	// (font changes may require relayout even if size unchanged)
	t.Layout()
}

// AddTab adds a tab with the given text and content trinket.
func (t *TabTrinket) AddTab(text string, content core.Trinket) int {
	tab := &Tab{
		Text:    text,
		Content: content,
		Enabled: true,
	}
	t.tabs = append(t.tabs, tab)

	if content != nil {
		content.SetParent(t)
	}

	if t.currentIndex < 0 {
		t.currentIndex = 0
	}

	t.Update()
	return len(t.tabs) - 1
}

// InsertTab inserts a tab at the given index.
func (t *TabTrinket) InsertTab(index int, text string, content core.Trinket) int {
	if index < 0 {
		index = 0
	}
	if index > len(t.tabs) {
		index = len(t.tabs)
	}

	tab := &Tab{
		Text:    text,
		Content: content,
		Enabled: true,
	}

	t.tabs = append(t.tabs[:index], append([]*Tab{tab}, t.tabs[index:]...)...)

	if content != nil {
		content.SetParent(t)
	}

	if t.currentIndex >= index {
		t.currentIndex++
	}

	if t.currentIndex < 0 {
		t.currentIndex = 0
	}

	t.Update()
	return index
}

// RemoveTab removes the tab at the given index.
func (t *TabTrinket) RemoveTab(index int) {
	if index < 0 || index >= len(t.tabs) {
		return
	}

	tab := t.tabs[index]
	if tab.Content != nil {
		tab.Content.SetParent(nil)
	}

	t.tabs = append(t.tabs[:index], t.tabs[index+1:]...)

	// Adjust current index
	if t.currentIndex == index {
		if t.currentIndex >= len(t.tabs) {
			t.currentIndex = len(t.tabs) - 1
		}
		if t.onCurrentChanged != nil && t.currentIndex >= 0 {
			t.onCurrentChanged(t.currentIndex)
		}
	} else if t.currentIndex > index {
		t.currentIndex--
	}

	t.Update()
}

// Clear removes all tabs.
func (t *TabTrinket) Clear() {
	for _, tab := range t.tabs {
		if tab.Content != nil {
			tab.Content.SetParent(nil)
		}
	}
	t.tabs = nil
	t.currentIndex = -1
	t.Update()
}

// Children returns the content of the current active tab only.
// This ensures focus navigation only includes visible trinkets.
func (t *TabTrinket) Children() []core.Trinket {
	if t.currentIndex >= 0 && t.currentIndex < len(t.tabs) {
		if content := t.tabs[t.currentIndex].Content; content != nil {
			return []core.Trinket{content}
		}
	}
	return nil
}

// AllChildren returns content trinkets from all tabs (for layout/painting).
func (t *TabTrinket) AllChildren() []core.Trinket {
	var children []core.Trinket
	for _, tab := range t.tabs {
		if tab.Content != nil {
			children = append(children, tab.Content)
		}
	}
	return children
}

// CollectFocusChain implements FocusChainProvider to customize tab order.
// For bottom tabs, content comes before the tab bar in the focus sequence.
func (t *TabTrinket) CollectFocusChain(collector func(core.Trinket)) {
	if t.tabEdge() == TabEdgeBottom {
		// Bottom tabs: content first, then tab bar
		for _, child := range t.Children() {
			collector(child)
		}
		collector(t)
	} else {
		// Top tabs: tab bar first, then content
		collector(t)
		for _, child := range t.Children() {
			collector(child)
		}
	}
}

// AddChild adds a child trinket as a new tab.
func (t *TabTrinket) AddChild(child core.Trinket) {
	t.AddTab("Tab", child)
}

// RemoveChild removes a child trinket.
func (t *TabTrinket) RemoveChild(child core.Trinket) {
	for i, tab := range t.tabs {
		if tab.Content == child {
			t.RemoveTab(i)
			return
		}
	}
}

// ChildAt returns the child at the given position.
func (t *TabTrinket) ChildAt(pos core.UnitPoint) core.Trinket {
	contentRect := t.contentBounds()
	if pos.X >= contentRect.X && pos.X < contentRect.X+contentRect.Width &&
		pos.Y >= contentRect.Y && pos.Y < contentRect.Y+contentRect.Height {
		if t.currentIndex >= 0 && t.currentIndex < len(t.tabs) {
			return t.tabs[t.currentIndex].Content
		}
	}
	return nil
}

// Layout arranges the current tab content.
func (t *TabTrinket) Layout() {
	// Update content bounds and propagate layout to current content
	if t.currentIndex >= 0 && t.currentIndex < len(t.tabs) {
		content := t.tabs[t.currentIndex].Content
		if content != nil {
			contentBounds := t.contentBounds()
			content.SetBounds(core.UnitRect{
				X:      contentBounds.X,
				Y:      contentBounds.Y,
				Width:  contentBounds.Width,
				Height: contentBounds.Height,
			})

			// Force content to re-layout with fresh SizeHints
			// (important when font changes affect trinket sizing)
			if container, ok := content.(core.Container); ok {
				container.Layout()
			}
		}
	}
}

// LayoutManager returns nil (TabTrinket manages its own layout).
func (t *TabTrinket) LayoutManager() core.LayoutManager {
	return nil
}

// SetLayoutManager is a no-op (TabTrinket manages its own layout).
func (t *TabTrinket) SetLayoutManager(layout core.LayoutManager) {
	// TabTrinket manages its own layout, ignore external layout managers
}

// Count returns the number of tabs.
func (t *TabTrinket) Count() int {
	return len(t.tabs)
}

// Tab returns the tab at the given index.
func (t *TabTrinket) Tab(index int) *Tab {
	if index < 0 || index >= len(t.tabs) {
		return nil
	}
	return t.tabs[index]
}

// CurrentIndex returns the current tab index.
func (t *TabTrinket) CurrentIndex() int {
	return t.currentIndex
}

// SetCurrentIndex sets the current tab index.
func (t *TabTrinket) SetCurrentIndex(index int) {
	if index < 0 || index >= len(t.tabs) {
		return
	}
	if t.currentIndex == index {
		return
	}

	// Check if tab is enabled
	if !t.tabs[index].Enabled {
		return
	}

	t.currentIndex = index
	t.Update()

	// Announce tab change for accessibility
	if am := core.FindAccessibilityManager(t); am != nil {
		tab := t.tabs[index]
		am.AnnouncePolite(fmt.Sprintf("%s, tab, %d of %d", tab.Text, index+1, len(t.tabs)))
	}

	if t.onCurrentChanged != nil {
		t.onCurrentChanged(index)
	}
}

// CurrentTrinket returns the current tab's content trinket.
func (t *TabTrinket) CurrentTrinket() core.Trinket {
	if t.currentIndex < 0 || t.currentIndex >= len(t.tabs) {
		return nil
	}
	return t.tabs[t.currentIndex].Content
}

// SetTabText sets the text of a tab.
func (t *TabTrinket) SetTabText(index int, text string) {
	if index < 0 || index >= len(t.tabs) {
		return
	}
	t.tabs[index].Text = text
	t.Update()
}

// TabText returns the text of a tab.
func (t *TabTrinket) TabText(index int) string {
	if index < 0 || index >= len(t.tabs) {
		return ""
	}
	return t.tabs[index].Text
}

// SetTabIcon sets the icon of a tab.
func (t *TabTrinket) SetTabIcon(index int, icon *style.TextIcon) {
	if index < 0 || index >= len(t.tabs) {
		return
	}
	t.tabs[index].Icon = icon
	t.Update()
}

// SetTabEnabled sets whether a tab is enabled.
func (t *TabTrinket) SetTabEnabled(index int, enabled bool) {
	if index < 0 || index >= len(t.tabs) {
		return
	}
	t.tabs[index].Enabled = enabled
	t.Update()
}

// IsTabEnabled returns whether a tab is enabled.
func (t *TabTrinket) IsTabEnabled(index int) bool {
	if index < 0 || index >= len(t.tabs) {
		return false
	}
	return t.tabs[index].Enabled
}

// TabPosition returns the tab bar position.
func (t *TabTrinket) TabPosition() TabPosition {
	return t.tabPosition
}

// SetTabPosition sets the tab bar position.
func (t *TabTrinket) SetTabPosition(position TabPosition) {
	t.tabPosition = position
	t.Update()
}

// IsMovable returns whether tabs can be reordered.
func (t *TabTrinket) IsMovable() bool {
	return t.movable
}

// SetMovable sets whether tabs can be reordered.
func (t *TabTrinket) SetMovable(movable bool) {
	t.movable = movable
}

// IsClosable returns whether tabs have close buttons.
func (t *TabTrinket) IsClosable() bool {
	return t.closable
}

// SetClosable sets whether tabs have close buttons.
func (t *TabTrinket) SetClosable(closable bool) {
	t.closable = closable
	t.Update()
}

// ShowSeparator returns whether a separator is shown between the tab bar and content.
func (t *TabTrinket) ShowSeparator() bool {
	return t.showSeparator
}

// SetShowSeparator sets whether to show a separator between the tab bar and content.
// For vertical tabs (a strip on either side), this draws a vertical line on the inside edge.
// For horizontal tabs (TabsTop/TabsBottom), this adds an extra row in the active tab color.
func (t *TabTrinket) SetShowSeparator(show bool) {
	t.showSeparator = show
	t.Update()
}

// SetOnCurrentChanged sets the current changed callback.
func (t *TabTrinket) SetOnCurrentChanged(handler func(index int)) {
	t.onCurrentChanged = handler
}

// SetOnTabCloseRequested sets the tab close requested callback.
func (t *TabTrinket) SetOnTabCloseRequested(handler func(index int)) {
	t.onTabCloseRequested = handler
}

// BackgroundColor returns the TabTrinket's background color.
// If no explicit background is set, returns ColorDefault (ANSI 49)
// so that child trinkets inherit the correct color.
func (t *TabTrinket) BackgroundColor() *style.Color {
	// First check if an explicit background color was set
	if bg := t.TrinketBase.BackgroundColor(); bg != nil {
		return bg
	}
	// Fall back to terminal default (ANSI 49)
	defaultColor := style.ColorDefault
	return &defaultColor
}

// tabBarHeight returns the height of the tab bar.
func (t *TabTrinket) tabBarHeight() core.Unit {
	metrics := t.EffectiveCellMetrics()
	return metrics.UnitsPerCellHeight
}

// contentBounds returns the bounds for the content area.
func (t *TabTrinket) contentBounds() core.UnitRect {
	bounds := t.Bounds()
	tabHeight := t.tabBarHeight()
	metrics := t.EffectiveCellMetrics()

	// Calculate separator size if enabled
	separatorHeight := core.Unit(0)
	separatorWidth := core.Unit(0)
	if t.showSeparator {
		separatorHeight = metrics.UnitsPerCellHeight
		separatorWidth = metrics.UnitsPerCellWidth
	}

	switch t.tabEdge() {
	case TabEdgeTop:
		return core.UnitRect{
			X:      0,
			Y:      tabHeight + separatorHeight,
			Width:  bounds.Width,
			Height: bounds.Height - tabHeight - separatorHeight,
		}
	case TabEdgeBottom:
		return core.UnitRect{
			X:      0,
			Y:      0,
			Width:  bounds.Width,
			Height: bounds.Height - tabHeight - separatorHeight,
		}
	case TabEdgeLeft:
		tabWidth := t.calculateTabBarWidth()
		// Scrollbar reuses the outside padding column, no extra width needed
		return core.UnitRect{
			X:      tabWidth + separatorWidth,
			Y:      0,
			Width:  bounds.Width - tabWidth - separatorWidth,
			Height: bounds.Height,
		}
	case TabEdgeRight:
		tabWidth := t.calculateTabBarWidth()
		// Scrollbar reuses the outside padding column, no extra width needed
		return core.UnitRect{
			X:      0,
			Y:      0,
			Width:  bounds.Width - tabWidth - separatorWidth,
			Height: bounds.Height,
		}
	}
	return bounds
}

func (t *TabTrinket) calculateTabBarWidth() core.Unit {
	metrics := t.EffectiveCellMetrics()
	maxLen := 10
	for _, tab := range t.tabs {
		if len(tab.Text) > maxLen {
			maxLen = len(tab.Text)
		}
	}
	return core.Unit(maxLen+4) * metrics.UnitsPerCellWidth
}

// calculateTotalTabsWidth returns the total width needed to display all tabs.
// Format: [prefix][tab1 text][sep][tab2 text][sep]...
// - Prefix: 3 cells if the first tab is selected (" /<"), else 2 ("  ")
// - Separator: 3 cells beside the selected tab (" \ " or " /<"), else 2 ("  ")
//
// The two shapes draw those three cells differently -- a top strip leads into
// its tab with a slash and a bottom one with a backslash -- but a join costs
// the same either way: a space, the diagonal, and the cell that carries the
// focus marker.
func (t *TabTrinket) calculateTotalTabsWidth() core.Unit {
	metrics := t.EffectiveCellMetrics()
	if len(t.tabs) == 0 {
		return 0
	}

	prefixWidth := 2
	if t.currentIndex == 0 {
		prefixWidth = 3
	}
	total := core.Unit(prefixWidth) * metrics.UnitsPerCellWidth

	for i, tab := range t.tabs {
		// Tab text - use font measurement for accurate width
		total += t.MeasureText(tab.Text)

		// Separator after tab: 3 cells if this or the next tab is selected,
		// else 2
		sepWidth := 2
		if i == t.currentIndex || (i+1 < len(t.tabs) && i+1 == t.currentIndex) {
			sepWidth = 3
		}
		total += core.Unit(sepWidth) * metrics.UnitsPerCellWidth
	}
	return total
}

// tabsNeedScrolling returns true if tabs don't fit and need scroll buttons.
func (t *TabTrinket) tabsNeedScrolling() bool {
	bounds := t.Bounds()
	// Check if tabs fit the full width - don't pre-subtract scroll button space
	// since scroll buttons only appear when scrolling is actually needed
	return t.calculateTotalTabsWidth() > bounds.Width
}

// scrollButtonWidth returns the width of each scroll button.
func (t *TabTrinket) scrollButtonWidth() core.Unit {
	return t.EffectiveCellMetrics().TextWidth(3) // [<] or [>]
}

// fillArcWedge fills the part of an rX x rY box lying OUTSIDE the
// quarter ellipse inscribed in it and centered on the chosen corner -
// the building block for the tab silhouette's convex shoulders and
// concave feet. The horizontal radius (rX) comes from the cell width
// and the vertical radius (rY) from the row height, so the curve keeps
// its shape when the row denomination stretches the tab. (The pixel
// backend's DrawArcWedge draws the same ellipse antialiased; this is the
// scanline fallback.)
func fillArcWedge(p *core.Painter, x, y, rX, rY core.Unit, centerRight, centerBottom bool, s style.CellStyle) {
	rxf, ryf := float64(rX), float64(rY)
	for i := core.Unit(0); i < rY; i++ {
		var row core.Unit
		if centerBottom {
			row = y + i // rows farthest from the center first
		} else {
			row = y + rY - 1 - i
		}
		dy := ryf - float64(i) - 0.5
		frac := 1 - dy*dy/(ryf*ryf)
		if frac < 0 {
			frac = 0
		}
		w := rxf - rxf*math.Sqrt(frac)
		wu := core.Unit(w + 0.5)
		if wu <= 0 {
			continue
		}
		if wu > rX {
			wu = rX
		}
		sx := x
		if !centerRight {
			sx = x + rX - wu
		}
		p.FillRect(core.UnitRect{X: sx, Y: row, Width: wu, Height: 1}, ' ', s)
	}
}

// strokeArc traces the quarter-ellipse boundary of the wedge painted by
// fillArcWedge with a hairline, spanning each scanline from the
// boundary's position at the row's far edge to its position at the near
// edge so the drawn curve stays connected.
func strokeArc(p *core.Painter, x, y, rX, rY core.Unit, centerRight, centerBottom bool, s style.CellStyle, hair core.Unit) {
	rxf, ryf := float64(rX), float64(rY)
	for i := core.Unit(0); i < rY; i++ {
		var row core.Unit
		if centerBottom {
			row = y + i // rows farthest from the center first
		} else {
			row = y + rY - 1 - i
		}
		dyFar := ryf - float64(i)
		dyNear := dyFar - 1
		if dyNear < 0 {
			dyNear = 0
		}
		fFar := 1 - dyFar*dyFar/(ryf*ryf)
		if fFar < 0 {
			fFar = 0
		}
		fNear := 1 - dyNear*dyNear/(ryf*ryf)
		if fNear < 0 {
			fNear = 0
		}
		w0 := core.Unit(rxf - rxf*math.Sqrt(fNear))           // floor
		w1 := core.Unit(rxf - rxf*math.Sqrt(fFar) + 0.999999) // ceil
		if w1-w0 < hair {
			w1 = w0 + hair
		}
		if w1 > rX {
			w1 = rX
			if w0 > rX-hair {
				w0 = rX - hair
			}
		}
		if w0 < 0 {
			w0 = 0
		}
		sx := x + w0
		if !centerRight {
			sx = x + rX - w1
		}
		p.FillRect(core.UnitRect{X: sx, Y: row, Width: w1 - w0, Height: 1}, ' ', s)
	}
}

// tabSilhouetteRadii returns the horizontal and vertical radii of the
// selected tab's concave feet (Small) and convex shoulders (Big). The
// radius is split by axis so a tab keeps its shape when the row
// denomination stretches it: the HORIZONTAL radii come from the cell
// width (the foot flares within the slash cell, the shoulder is a column
// and a quarter) and the VERTICAL radii from the row height, with the
// shoulder taking the rest of the row so the two arcs always meet - no
// straight edge stranded between a cell-width-constant curve top and
// bottom. At the base 8x16 denomination the two axes coincide (a
// circle); a taller row turns the corners into taller ellipses.
func tabSilhouetteRadii(cw, rowH core.Unit) (rSmallX, rBigX, rSmallY, rBigY core.Unit) {
	rSmallX = cw * 3 / 4
	rBigX = cw + cw/4
	rSmallY = rowH * 3 / 8
	rBigY = rowH - rSmallY
	return
}

// paintTabShape shapes the selected tab on pixel surfaces and draws
// the strip's edge line. A small radius flares the tab's base
// concavely into the slash cells, and a larger radius (a column and a
// quarter) rounds the body's outer corners - top corners for a top
// bar, bottom corners for a bottom bar. On top of the fills it draws
// one continuous hairline in the bar's text color: along the bar's
// content edge up to the lead foot, around the arcs, across the tab's
// outer edge, and back out to the end of the strip. leadX/trailX are
// the cells holding the lead and trail slash; trailX is -1 for a
// partial tab cut off before its trailing slash, in which case the
// fill ends abruptly at endX and the edge line drops straight down
// there. With no selected tab in view the edge is a single straight
// line across the strip.
func (t *TabTrinket) paintTabShape(p *core.Painter, rowY, stripW, leadX, trailX, endX core.Unit, tab, bar style.CellStyle, top, mirror bool) {
	metrics := t.EffectiveCellMetrics()
	cw := metrics.UnitsPerCellWidth
	// The anchors arrive in the strip's RUN coordinates, like every other mark
	// the strip made, and everything below is worked out in them. Each mark is
	// reflected as it is DRAWN -- which is what the tape does, and what lets a
	// tab with only one foot (the last in the strip, or one cut short) turn
	// over without a mirror image of its own arithmetic.
	rowH := metrics.UnitsPerCellHeight
	line := bar.WithBg(bar.Fg)
	// The whole tab outline - the arc strokes AND the straight edge lines - is
	// one physical hairline. Deriving its weight from pixels-per-unit (a "1
	// unit" stroke) makes it silently change thickness under re-denomination:
	// re-denomination pairs a unit-count change with a compensating font_size
	// to hold the physical size, so pxPerUnit shifts even though nothing
	// visibly did. Anchor the weight on the device scale instead (denomination-
	// and font_size-invariant): hairU is the whole-unit stroke that renders to
	// that device-pixel weight, and both the arcs (via strokeW) and the edge
	// lines (via edgePxH, == pxLen(hairU)) use it, so they always match.
	hairU := core.Unit(math.Round(float64(p.DeviceScale()) / p.PxPerUnitF()))
	if hairU < 1 {
		hairU = 1
	}
	hairW, hairH := hairU, hairU
	edgePxH := p.UnitsToPx(hairU)
	if edgePxH < 1 {
		edgePxH = 1
	}
	// Both edge lines lie ALONG a row boundary, and each is named by the
	// boundary it lies along rather than by where its top happens to fall.
	//
	// A row boundary is a cell edge, so it converts to a device pixel exactly;
	// a unit position inside the row does not, and a line placed by its top at
	// rowH minus its own thickness ends up floating clear of the bottom
	// wherever a unit is worth more pixels than the line is thick. That is the
	// half denomination, where one unit is two pixels and the hairline is one.
	// So the line hangs off its boundary by its thickness in DEVICE PIXELS,
	// and sits flush at every denomination.
	barEdgeY, barEdgeUp := rowY+rowH, true // content side of a top bar
	tabEdgeY, tabEdgeUp := rowY, false     // selected tab's outer side
	if !top {
		barEdgeY, barEdgeUp = rowY, false
		tabEdgeY, tabEdgeUp = rowY+rowH, true
	}
	// hlineExt draws the bar/tab edge line from x0 to x1 along the boundary at
	// y -- above it when up, below it otherwise -- extended by an exact
	// device-pixel amount on the left/right (padL/padR). The feet are nudged
	// inward by one line thickness in device pixels (see below); their edge
	// meets these lines a line-thickness further in, so the abutting run is
	// extended to reach the shifted foot with no gap.
	hlineExt := func(x0, x1 core.Unit, y core.Unit, up bool, padL, padR int) {
		if x1 <= x0 {
			return
		}
		if mirror {
			x0, x1 = stripW-x1, stripW-x0
			padL, padR = padR, padL
		}
		wPx := p.UnitSpanPxX(x0, x1) + padL + padR
		if wPx <= 0 {
			return
		}
		offY := 0
		cellY := y
		if up {
			offY = -edgePxH
			cellY = y - hairH
		}
		if !p.FillRectPixels(x0, y, -padL, offY, wPx, edgePxH, line) {
			p.FillRect(core.UnitRect{X: x0, Y: cellY, Width: x1 - x0, Height: hairH}, ' ', line)
		}
	}
	hline := func(x0, x1, y core.Unit, up bool) { hlineExt(x0, x1, y, up, 0, 0) }
	// The curve radius is split by axis so a tab keeps its shape under
	// re-denomination: the HORIZONTAL radii come from the cell width (the
	// foot flares within the slash cell, the shoulder is a column and a
	// quarter), the VERTICAL radii from the row height, so the silhouette
	// keeps its shape under re-denomination (see tabSilhouetteRadii).
	// vrule draws one of the shape's vertical strokes, reflected like the rest.
	vrule := func(x, y, h core.Unit) {
		if mirror {
			x = stripW - x - hairW
		}
		p.FillRect(core.UnitRect{X: x, Y: y, Width: hairW, Height: h}, ' ', line)
	}
	rSmallX, rBigX, rSmallY, rBigY := tabSilhouetteRadii(cw, rowH)
	bodyLeft := leadX + cw
	bodyRight := trailX
	hasTrail := trailX > bodyLeft
	if !hasTrail {
		bodyRight = endX
	}
	if bodyRight-bodyLeft < rBigX*2 {
		rBigX = (bodyRight - bodyLeft) / 2
	}
	if leadX < 0 || rBigX <= 0 || rSmallX <= 0 || rBigY <= 0 || rSmallY <= 0 || bodyRight <= bodyLeft {
		hline(0, stripW, barEdgeY, barEdgeUp)
		return
	}
	footY := rowY + rowH - rSmallY // slash cells flare at the bar edge
	shoY := rowY
	if !top {
		footY = rowY
		shoY = rowY + rowH - rBigY
	}
	// One quarter arc of the silhouette: the wedge outside the arc in
	// the given fill color, edged along the arc in the line color -
	// antialiased by the backend when it can, scanline fills
	// otherwise.
	arc := func(x, y, rX, rY core.Unit, cRight, cBottom bool, offXPx int, fill style.CellStyle) {
		if mirror {
			x = stripW - x - rX
			cRight = !cRight
			offXPx = -offXPx
		}
		box := core.UnitRect{X: x, Y: y, Width: rX, Height: rY}
		if p.DrawArcWedge(box, cRight, cBottom, hairU, offXPx, 0, fill.WithFg(bar.Fg)) {
			return
		}
		fillArcWedge(p, x, y, rX, rY, cRight, cBottom, fill)
		strokeArc(p, x, y, rX, rY, cRight, cBottom, line, hairW)
	}
	// Concave feet in the slash cells, convex shoulders carving the body's outer
	// corners, joined into one continuous line between the two colors. The
	// shoulder (convex) and foot (concave) put their strokes on OPPOSITE sides
	// of the shared body-edge tangent, so where they meet the outline would jog
	// by exactly the stroke width. Cancel that by nudging each foot inward by
	// exactly the line thickness in DEVICE PIXELS (edgePxH) - a rigid post-snap
	// translation, identical on both sides at every sub-cell phase, so the seam
	// is a clean continuous line AND the tab stays mirror-symmetric (a unit-space
	// offset would snap differently per side/position and reintroduce a notch).
	hlineExt(0, leadX+cw-rSmallX, barEdgeY, barEdgeUp, 0, edgePxH)
	arc(leadX+cw-rSmallX, footY, rSmallX, rSmallY, false, !top, edgePxH, tab)
	arc(bodyLeft, shoY, rBigX, rBigY, true, top, 0, bar)
	// Straight vertical run on the tab's side where the two radii
	// don't span the full row height.
	gapLen := rowH - rSmallY - rBigY
	gapY := rowY + rSmallY
	if top {
		gapY = rowY + rBigY
	}
	if gapLen > 0 {
		vrule(bodyLeft, gapY, gapLen)
	}
	if hasTrail {
		hline(bodyLeft+rBigX, bodyRight-rBigX, tabEdgeY, tabEdgeUp)
		arc(bodyRight-rBigX, shoY, rBigX, rBigY, false, top, 0, bar)
		arc(bodyRight, footY, rSmallX, rSmallY, true, !top, -edgePxH, tab)
		if gapLen > 0 {
			vrule(bodyRight-hairW, gapY, gapLen)
		}
		hlineExt(bodyRight+rSmallX, stripW, barEdgeY, barEdgeUp, edgePxH, 0)
		return
	}
	// A tab with no trailing slash -- the last in the strip, or one cut short
	// by the strip's end: sudden color transition, with the edge line dropping
	// straight down the cut.
	hline(bodyLeft+rBigX, endX, tabEdgeY, tabEdgeUp)
	vrule(endX-hairW, rowY, rowH)
	hline(endX, stripW, barEdgeY, barEdgeUp)
}

// overflowEllipsisWidth is the tab strip's "..." width: measured
// proportionally on pixel surfaces (painting draws it through the
// text engine there), three cells on cell surfaces. Layout,
// need-for-ellipsis checks, and painting must all use this one
// number so the strip math connects.
func (t *TabTrinket) overflowEllipsisWidth() core.Unit {
	if core.FindSmoothPositioning(t.Self()) {
		return t.MeasureText("...")
	}
	return t.EffectiveCellMetrics().TextWidth(3)
}

// ensureCurrentTabVisible adjusts scroll offset to make current tab visible.
func (t *TabTrinket) ensureCurrentTabVisible() {
	if t.currentIndex < 0 || !t.tabsNeedScrolling() {
		t.tabScrollOffset = 0
		return
	}
	if t.currentIndex < t.tabScrollOffset {
		t.tabScrollOffset = t.currentIndex
	}
	// Check if current tab is past the visible area
	// (This is a simplified check - could be improved)
	maxVisible := t.tabScrollOffset + 3 // Rough estimate
	if t.currentIndex >= maxVisible && maxVisible < len(t.tabs) {
		t.tabScrollOffset = t.currentIndex - 2
		if t.tabScrollOffset < 0 {
			t.tabScrollOffset = 0
		}
	}
}

// canScrollLeft returns true if there are tabs to the left.
func (t *TabTrinket) canScrollLeft() bool {
	return t.tabScrollOffset > 0
}

// canScrollRight returns true if there are more tabs to show on the right.
// This checks if the last tab is fully visible, not just if there are more tabs.
func (t *TabTrinket) canScrollRight() bool {
	if t.tabScrollOffset >= len(t.tabs)-1 {
		return false
	}
	// Check if the last tab is fully visible
	return !t.isLastTabFullyVisible()
}

// isLastTabFullyVisible returns true if the last tab is completely visible.
// For top tabs, a 2-character grace margin is allowed for trailing underscores/spaces
// after the last separator's backslash, as these are non-essential filler content.
func (t *TabTrinket) isLastTabFullyVisible() bool {
	bounds := t.Bounds()
	metrics := t.EffectiveCellMetrics()

	scrollButtonsWidth := core.Unit(0)
	if t.tabsNeedScrolling() {
		scrollButtonsWidth = metrics.TextWidth(6)
	}
	leftEllipseWidth := core.Unit(0)
	if t.tabScrollOffset > 0 {
		leftEllipseWidth = t.overflowEllipsisWidth()
	}
	// Available width is the absolute position where tabs must stop
	availableWidth := bounds.Width - scrollButtonsWidth

	// Tab format varies by position
	isBottomTabs := t.tabEdge() == TabEdgeBottom

	// Calculate width needed for visible tabs
	x := leftEllipseWidth
	for i := t.tabScrollOffset; i < len(t.tabs); i++ {
		tab := t.tabs[i]
		isFirstVisible := i == t.tabScrollOffset
		isSelected := i == t.currentIndex
		isLastVisible := i == len(t.tabs)-1
		nextIsSelected := !isLastVisible && i+1 == t.currentIndex

		// When the overflow mark is showing, the prefix drops its leading space
		hasLeftEllipsis := t.tabScrollOffset > 0
		prefixWidth := 0
		if isFirstVisible {
			if isSelected {
				prefixWidth = 3 // " /<" on a top strip, " \_" on a bottom one
				if hasLeftEllipsis {
					prefixWidth = 2 // tighter beside the mark: "/<" or "\_"
				}
			} else {
				prefixWidth = 2 // "  "
				if hasLeftEllipsis {
					prefixWidth = 1 // " "
				}
			}
		}
		sepWidth := 2 // "  "
		if isSelected || nextIsSelected {
			sepWidth = 3 // the join beside the selected tab
		}
		// Prefix and separator are decorative (cell-based), text is font-based
		tabSlotWidth := core.Unit(prefixWidth+sepWidth)*metrics.UnitsPerCellWidth + t.MeasureText(tab.Text)
		x += tabSlotWidth

		if x > availableWidth {
			// For top tabs, allow a grace margin ONLY if all essential content fits.
			// Essential = prefix + text + (space/bracket + backslash for selected)
			// Non-essential = the trailing space (or both trailing spaces for unselected)
			if !isBottomTabs && isLastVisible {
				essentialSepWidth := 0
				if isSelected {
					essentialSepWidth = 2 // space/bracket + backslash are essential
				}
				// nextIsSelected doesn't matter for last tab since there's no next tab
				essentialWidth := core.Unit(prefixWidth+essentialSepWidth)*metrics.UnitsPerCellWidth + t.MeasureText(tab.Text)
				essentialX := x - tabSlotWidth + essentialWidth
				if essentialX <= availableWidth {
					// Only non-essential trailing content cut off
					return true
				}
			}
			return false
		}
	}
	return true
}

// --- Vertical Tab Scrolling ---

// vertVisibleCount returns how many tabs can fit in the vertical tab bar.
func (t *TabTrinket) vertVisibleCount() int {
	bounds := t.Bounds()
	metrics := t.EffectiveCellMetrics()
	return int(bounds.Height / metrics.UnitsPerCellHeight)
}

// vertTabsNeedScrolling returns true if vertical tabs need scrolling.
func (t *TabTrinket) vertTabsNeedScrolling() bool {
	return len(t.tabs) > t.vertVisibleCount()
}

// vertEnsureVisible ensures the given tab index is visible in the vertical tab bar.
func (t *TabTrinket) vertEnsureVisible(index int) {
	if index < 0 || index >= len(t.tabs) {
		return
	}

	visibleCount := t.vertVisibleCount()
	if visibleCount <= 0 {
		return
	}

	// If tab is above visible area, scroll up
	if index < t.vertScrollOffset {
		t.vertScrollOffset = index
	}

	// If tab is below visible area, scroll down
	if index >= t.vertScrollOffset+visibleCount {
		t.vertScrollOffset = index - visibleCount + 1
	}

	// Clamp scroll offset
	maxOffset := len(t.tabs) - visibleCount
	if maxOffset < 0 {
		maxOffset = 0
	}
	if t.vertScrollOffset > maxOffset {
		t.vertScrollOffset = maxOffset
	}
	if t.vertScrollOffset < 0 {
		t.vertScrollOffset = 0
	}
}

// vertScrollbarGeometry returns scrollbar dimensions and thumb position.
// Returns: scrollbarX (for left tabs it's 0, for right tabs it's at the right edge),
// thumbStart, thumbHeight, trackHeight (all in rows)
func (t *TabTrinket) vertScrollbarGeometry() (scrollbarX core.Unit, thumbStart, thumbHeight, trackHeight int) {
	bounds := t.Bounds()
	metrics := t.EffectiveCellMetrics()
	totalTabs := len(t.tabs)
	visibleCount := t.vertVisibleCount()

	// Scrollbar position depends on tab position
	if t.tabEdge() == TabEdgeLeft {
		scrollbarX = 0 // Left edge (outside)
	} else {
		scrollbarX = bounds.Width - metrics.UnitsPerCellWidth // Right edge (outside)
	}

	trackHeight = visibleCount

	if totalTabs <= visibleCount {
		// No scrolling needed - thumb fills track
		thumbStart = 0
		thumbHeight = trackHeight
		return
	}

	// Calculate thumb height - proportional to visible/total, minimum 1 row
	thumbHeight = visibleCount * visibleCount / totalTabs
	if thumbHeight < 1 {
		thumbHeight = 1
	}

	// Calculate thumb position
	maxScroll := totalTabs - visibleCount
	scrollableTrack := trackHeight - thumbHeight

	if maxScroll > 0 && scrollableTrack > 0 {
		thumbStart = t.vertScrollOffset * scrollableTrack / maxScroll

		// Ensure thumb doesn't go to extremes unless scroll is at extremes
		if t.vertScrollOffset > 0 && thumbStart == 0 {
			thumbStart = 1
		}
		if t.vertScrollOffset < maxScroll && thumbStart >= scrollableTrack {
			thumbStart = scrollableTrack - 1
		}
	}

	return scrollbarX, thumbStart, thumbHeight, trackHeight
}

// vertScrollbarUnits returns the vertical tab lane geometry in units
// for pixel surfaces: track length, thumb length, thumb origin - the
// same proportions as vertScrollbarGeometry without row quantization.
// Mid-drag the thumb origin is the smooth (pointer-tracked) position.
func (t *TabTrinket) vertScrollbarUnits() (trackU, thumbU, posU float64) {
	metrics := t.EffectiveCellMetrics()
	visibleCount := t.vertVisibleCount()
	trackU = float64(core.Unit(visibleCount) * metrics.UnitsPerCellHeight)
	totalTabs := len(t.tabs)
	if totalTabs <= visibleCount || visibleCount <= 0 {
		return trackU, trackU, 0
	}
	thumbU = trackU * float64(visibleCount) / float64(totalTabs)
	if thumbU < 8 {
		thumbU = 8
	}
	if thumbU > trackU {
		thumbU = trackU
	}
	scrollable := trackU - thumbU
	maxScroll := totalTabs - visibleCount
	if t.scrollbarDragging && t.smoothVertSbDrag {
		posU = t.vertSbThumbPos
	} else if maxScroll > 0 {
		posU = float64(t.vertScrollOffset) * scrollable / float64(maxScroll)
	}
	if posU < 0 {
		posU = 0
	}
	if posU > scrollable {
		posU = scrollable
	}
	return trackU, thumbU, posU
}

// paintVertScrollbar draws the vertical scrollbar for vertical tabs.
func (t *TabTrinket) paintVertScrollbar(p *core.Painter, scrollbarX core.Unit) {
	scheme := t.GetScheme()
	metrics := t.EffectiveCellMetrics()
	trackStyle := scheme.GetScrollbar()
	thumbStyle := scheme.GetScrollbarThumbState(false, t.scrollbarThumbHovered && p.Graphical())

	// Pixel surfaces: a single hairline stripe blended at 50%
	// opacity behind, and one solid full-opacity rectangle for the
	// thumb, at unit granularity - the toolkit's lane treatment.
	if p.Graphical() {
		trackU, thumbU, posU := t.vertScrollbarUnits()
		stripeX := scrollbarX + metrics.UnitsPerCellWidth/2
		p.FillRect(core.UnitRect{
			X:      stripeX,
			Y:      0,
			Width:  1,
			Height: core.Unit(trackU + 0.5),
		}, '▒', trackStyle.WithBg(style.ColorTransparent))
		p.FillRect(core.UnitRect{
			X:      scrollbarX + 1,
			Y:      core.Unit(posU + 0.5),
			Width:  metrics.UnitsPerCellWidth - 2,
			Height: core.Unit(thumbU + 0.5),
		}, ' ', thumbStyle.WithBg(thumbStyle.Fg))
		return
	}

	_, thumbStart, thumbHeight, trackHeight := t.vertScrollbarGeometry()

	// Draw scrollbar track
	for i := 0; i < trackHeight; i++ {
		y := core.Unit(i) * metrics.UnitsPerCellHeight
		p.DrawCell(scrollbarX, y, '│', trackStyle)
	}

	// Draw scrollbar thumb
	for i := 0; i < thumbHeight; i++ {
		y := core.Unit(thumbStart+i) * metrics.UnitsPerCellHeight
		p.DrawCell(scrollbarX, y, '█', thumbStyle)
	}
}

// handleVertScrollbarClick handles a click on the vertical tab scrollbar.
func (t *TabTrinket) handleVertScrollbarClick(y core.Unit, metrics core.CellMetrics) {
	clickedRow := int(y / metrics.UnitsPerCellHeight)
	_, thumbStart, thumbHeight, _ := t.vertScrollbarGeometry()

	// Pixel surfaces anchor the drag to the grab point within the
	// unit-granular thumb; the first visible tab stays item-snapped.
	if core.FindSmoothPositioning(t.Self()) {
		_, thumbU, posU := t.vertScrollbarUnits()
		pos := float64(y)
		if pos >= posU && pos < posU+thumbU {
			t.scrollbarDragging = true
			t.smoothVertSbDrag = true
			t.vertSbGrabOff = pos - posU
			t.vertSbThumbPos = posU
			return
		}
		// Track press falls through to page up/down below, keyed
		// off the smooth thumb position.
		if pos < posU {
			clickedRow = thumbStart - 1
		} else {
			clickedRow = thumbStart + thumbHeight
		}
	}

	// Check if click is on thumb - start drag
	if clickedRow >= thumbStart && clickedRow < thumbStart+thumbHeight {
		t.scrollbarDragging = true
		t.scrollbarDragStart = clickedRow
		t.scrollbarDragOffset = t.vertScrollOffset
		return
	}

	// Click above thumb - page up
	if clickedRow < thumbStart {
		visibleCount := t.vertVisibleCount()
		newOffset := t.vertScrollOffset - visibleCount
		if newOffset < 0 {
			newOffset = 0
		}
		t.vertScrollOffset = newOffset
		t.Update()
		return
	}

	// Click below thumb - page down
	if clickedRow >= thumbStart+thumbHeight {
		visibleCount := t.vertVisibleCount()
		maxOffset := len(t.tabs) - visibleCount
		if maxOffset < 0 {
			maxOffset = 0
		}
		newOffset := t.vertScrollOffset + visibleCount
		if newOffset > maxOffset {
			newOffset = maxOffset
		}
		t.vertScrollOffset = newOffset
		t.Update()
		return
	}
}

// SizeHint returns the preferred size. The width is the fallback for when
// nothing sets one (see defaultSizeCells).
func (t *TabTrinket) SizeHint() core.UnitSize {
	metrics := t.EffectiveCellMetrics()
	return core.UnitSize{
		Width:  metrics.UnitsPerCellWidth * defaultWideWidthCells,
		Height: metrics.UnitsPerCellHeight * defaultContainerHeightCells,
	}
}

// Paint renders the tab trinket.
func (t *TabTrinket) Paint(p *core.Painter) {
	bounds := t.Bounds()
	scheme := t.GetScheme()
	metrics := t.EffectiveCellMetrics()

	// Draw background using TabTrinket's background color if set
	bgStyle := style.DefaultStyle()
	if bg := t.BackgroundColor(); bg != nil {
		bgStyle = bgStyle.WithBg(*bg)
	}
	p.FillRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, ' ', bgStyle)

	// Draw tab bar based on position
	switch t.tabEdge() {
	case TabEdgeTop:
		t.paintTopTabs(p, bounds, scheme, metrics)
	case TabEdgeBottom:
		t.paintBottomTabs(p, bounds, scheme, metrics)
	case TabEdgeLeft:
		t.paintLeftTabs(p, bounds, scheme, metrics)
	case TabEdgeRight:
		t.paintRightTabs(p, bounds, scheme, metrics)
	}

	// Draw content
	t.paintContent(p)
}

// --- the strip's display list -------------------------------------------
//
// A horizontal tab strip works its layout out as it goes: a prefix, a label, a
// separator, the next label, and a hundred small marks between them, each
// placed by advancing along the bar. Where that run STARTS, and which way it
// travels, is one question -- and asking it at every one of those marks is how
// a strip ends up half turned over.
//
// So the strip records what it paints, in its own RUN coordinates, and the
// tape is replayed in one go. Where the run lands is settled there, once.

type stripMarkKind int

const (
	markCell stripMarkKind = iota
	markText
	markFill
)

// stripMark is one thing a strip paints, and the room it occupies. The room is
// what a reflection needs: a mark is placed by the edge the run reaches first,
// which is its left edge one way round and its right edge the other.
type stripMark struct {
	kind stripMarkKind
	rect core.UnitRect
	// owner is whose mark this is: a tab by its index, or one of the strip's
	// own parts below. It is what lets the mouse find a part of the strip
	// where the painter put it rather than by working the layout out a
	// second time.
	owner int
	// group ties a mark to the ones beside it. A tied block reflects WHOLE:
	// the space it takes turns over, and inside it the marks keep the order
	// and the spacing they were made with. That is what a run reading the
	// other way from the strip needs -- a word and the dots that stand for
	// what was cut off it belong together and read their own way.
	group int
	ch    rune
	text  string
	style style.CellStyle
	font  *core.Font
}

// The parts of a strip that are not a tab. A tab owns its marks by its own
// index, so these are negative.
const (
	stripSelf     = -1 // the background, the filler, the strip's own edge
	stripOverflow = -2 // the mark standing for the tabs scrolled off the front
	stripScrollLo = -3 // the [<] button
	stripScrollHi = -4 // the [>] button
)

// stripTape collects a strip's marks in the order they are made.
type stripTape struct {
	marks   []stripMark
	groups  int
	owner   int // whose marks are being made just now
	clipped int // the tab the run was cut short at, if it reached one
	cw      core.Unit
	measure func(string) core.Unit
}

// owned says whose marks follow.
func (tp *stripTape) owned(by int) { tp.owner = by }

// pastTheTabs hands the strip back its own marks once the run of tabs is
// over -- unless the run was cut short, in which case what follows still
// belongs to the tab it was cut short at (see cutShortAt).
func (tp *stripTape) pastTheTabs() {
	if tp.clipped < 0 {
		tp.owned(stripSelf)
	}
}

// cutShortAt records that the run had no room to draw this tab whole. What
// follows it on the strip -- the dots that stand for what was left out, and
// the run of filler to the scroll buttons -- is that tab's, because a press
// anywhere in it means the same thing: bring this tab into view.
func (tp *stripTape) cutShortAt(tab int) {
	tp.clipped = tab
	tp.owned(tab)
}

// spans is the room each part of the strip took, in RUN coordinates, in the
// order the run made them. This is what the painter learned and the mouse asks.
func (tp *stripTape) spans() []stripSpan {
	var out []stripSpan
	for _, m := range tp.marks {
		if m.owner == stripSelf || m.rect.Width <= 0 {
			continue
		}
		cur := len(out) - 1
		if cur < 0 || out[cur].owner != m.owner {
			out = append(out, stripSpan{
				owner:    m.owner,
				x:        m.rect.X,
				w:        m.rect.Width,
				labelEnd: m.rect.X,
				clipped:  m.owner >= 0 && m.owner == tp.clipped,
			})
			cur = len(out) - 1
		} else {
			if m.rect.X < out[cur].x {
				out[cur].w += out[cur].x - m.rect.X
				out[cur].x = m.rect.X
			}
			if end := m.rect.X + m.rect.Width; end > out[cur].x+out[cur].w {
				out[cur].w = end - out[cur].x
			}
		}
		// A tab's label is the first text it puts down: its prefix is cells and
		// its ground is a fill, and anything it writes afterwards is the dots
		// for what would not fit.
		if m.kind == markText && out[cur].labelEnd == out[cur].x {
			out[cur].labelEnd = m.rect.X + m.rect.Width
		}
	}
	return out
}

// stripSpan is the room one part of a strip took along the run.
type stripSpan struct {
	owner int
	x, w  core.Unit
	// labelEnd is where this tab's label ended, and so where its separator
	// begins. It is the span's own start for a part that wrote no text.
	labelEnd core.Unit
	// clipped says the run was cut short at this tab (see cutShortAt).
	clipped bool
}

// at is where the next mark will go, for a caller meaning to tie from here.
func (tp *stripTape) at() int { return len(tp.marks) }

// tie makes everything recorded since `from` one block (see stripMark.group).
func (tp *stripTape) tie(from int) {
	if from < 0 || from >= len(tp.marks) {
		return
	}
	tp.groups++
	for i := from; i < len(tp.marks); i++ {
		tp.marks[i].group = tp.groups
	}
}

func (tp *stripTape) cell(x, y core.Unit, ch rune, s style.CellStyle) {
	tp.marks = append(tp.marks, stripMark{
		kind:  markCell,
		rect:  core.UnitRect{X: x, Y: y, Width: tp.cw},
		owner: tp.owner,
		ch:    ch, style: s,
	})
}

func (tp *stripTape) text(x, y core.Unit, text string, s style.CellStyle, f *core.Font) {
	tp.marks = append(tp.marks, stripMark{
		kind:  markText,
		rect:  core.UnitRect{X: x, Y: y, Width: tp.measure(text)},
		owner: tp.owner,
		text:  text, style: s, font: f,
	})
}

func (tp *stripTape) fill(r core.UnitRect, ch rune, s style.CellStyle) {
	tp.marks = append(tp.marks, stripMark{kind: markFill, rect: r, owner: tp.owner, ch: ch, style: s})
}

// mirroredGlyph is what a glyph becomes when the run it stands in is turned
// over. A reflection is not only a move: a mark that points along the run has
// to point the other way once the run does, and one that pairs with another
// has to become its partner. The rest of the strip's marks -- the underscore,
// the dots, the labels -- read the same in a mirror and are left alone.
//
// A LABEL is not reversed either. Turning the run over reorders the tabs and
// moves each label to where its tab now stands; which way the letters inside
// it run is the script's business, not the strip's.
func mirroredGlyph(ch rune) rune {
	switch ch {
	case '/':
		return '\\'
	case '\\':
		return '/'
	case '<':
		return '>'
	case '>':
		return '<'
	case '[':
		return ']'
	case ']':
		return '['
	}
	return ch
}

// replay draws the tape in the order it was made.
//
// mirror reflects every mark within a bar barW wide: a mark is placed by the
// edge the run reaches FIRST, which is its left edge one way round and its
// right edge the other, so reflecting means measuring its own width back from
// the far side. This is the one place the strip's run becomes places on the
// screen, which is why the hundred marks that made it never had to ask.
func (tp *stripTape) replay(p *core.Painter, barW core.Unit, mirror bool) {
	// A tied block turns over as one, so work out how far each has to move
	// before placing anything: its parts then keep their places within it.
	shift := map[int]core.Unit{}
	if mirror {
		lo, hi := map[int]core.Unit{}, map[int]core.Unit{}
		for _, m := range tp.marks {
			g := m.group
			if g == 0 {
				continue
			}
			if _, seen := lo[g]; !seen || m.rect.X < lo[g] {
				lo[g] = m.rect.X
			}
			if end := m.rect.X + m.rect.Width; end > hi[g] {
				hi[g] = end
			}
		}
		for g := range lo {
			shift[g] = barW - hi[g] - lo[g]
		}
	}
	for _, m := range tp.marks {
		r, ch := m.rect, m.ch
		if mirror {
			if m.group != 0 {
				r.X += shift[m.group]
			} else {
				r.X = barW - r.X - r.Width
				ch = mirroredGlyph(ch)
			}
		}
		switch m.kind {
		case markCell:
			p.DrawCell(r.X, r.Y, ch, m.style)
		case markText:
			p.DrawText(r.X, r.Y, m.text, m.style, m.font)
		case markFill:
			p.FillRect(r, ch, m.style)
		}
	}
}

// newStripTape starts a tape denominated the way this trinket is.
func (t *TabTrinket) newStripTape() *stripTape {
	return &stripTape{
		owner:   stripSelf,
		clipped: -1,
		cw:      t.EffectiveCellMetrics().UnitsPerCellWidth,
		measure: t.MeasureText,
	}
}

func (t *TabTrinket) paintTopTabs(p *core.Painter, bounds core.UnitRect, scheme *style.Scheme, metrics core.CellMetrics) {
	tabHeight := t.tabBarHeight()
	tape := t.newStripTape()
	hasFocus := t.HasFocus()
	font := t.EffectiveFont()

	// Tab bar style from scheme
	tabBarStyle := scheme.GetTabsBar(true)
	// Underlined tab bar style for unselected tabs and connectors (except slashes)
	tabBarUnderlined := tabBarStyle.Underline()
	// Selected tab style when unfocused: uses ActiveTab colors with overline
	// Overline is added so that the window frame above gets underlined
	selectedStyle := scheme.GetActiveTab().Overline()
	// Focused selected tab style: from scheme with overline
	focusedSelectedStyle := scheme.GetFocusedTab().Overline()
	// Pressed button style from scheme with underline
	pressedStyle := scheme.GetPressedTabsButton().Underline()
	// Pixel surfaces draw the strip's edge as one continuous hairline
	// in a post-pass (paintTabShape); the cell attributes and '_'
	// filler glyphs would double that line, so they are dropped.
	underscoreCh := '_'
	slashCh, backslashCh := '/', '\\'
	if p.Graphical() {
		tabBarUnderlined = tabBarStyle
		selectedStyle = scheme.GetActiveTab()
		focusedSelectedStyle = scheme.GetFocusedTab()
		pressedStyle = scheme.GetPressedTabsButton()
		underscoreCh = ' '
		// The arcs and edge line of paintTabShape replace the literal
		// slash glyphs on pixel surfaces.
		slashCh, backslashCh = ' ', ' '
	}
	// Disabled style
	disabledStyle := tabBarUnderlined.WithFg(scheme.GetDisabledTextFG())

	// Draw tab bar background
	tape.fill(core.UnitRect{Width: bounds.Width, Height: tabHeight}, ' ', tabBarStyle)

	// Calculate if we need scroll buttons
	needsScrolling := t.tabsNeedScrolling()
	scrollButtonsWidth := core.Unit(0)
	if needsScrolling {
		scrollButtonsWidth = metrics.TextWidth(6) // [<][>] = 6 chars
	}

	// If scrolled right, show left ellipse indicator (clickable to scroll left)
	leftEllipseWidth := core.Unit(0)
	if t.tabScrollOffset > 0 {
		leftEllipseWidth = t.overflowEllipsisWidth() // "..."
		// Draw the left ellipse (underlined; proportional on pixel
		// surfaces - the reserve width stays cell-based)
		tape.owned(stripOverflow)
		if p.Graphical() {
			tape.text(0, 0, "...", tabBarUnderlined, font)
		} else {
			for i := 0; i < 3; i++ {
				tape.cell(metrics.CellToUnitsX(i), 0, '.', tabBarUnderlined)
			}
		}
		tape.owned(stripSelf)
	}

	// Don't reserve space for right ellipsis upfront - we'll only need it if tabs don't fit
	// The gap-filling code after the loop will handle any remaining space
	// Available width is the absolute position where tabs must stop (before scroll buttons)
	availableWidth := bounds.Width - scrollButtonsWidth

	// Tab format: [prefix][tab1 text][sep][tab2 text][sep]...
	// - Prefix: "_/ " (3 chars) if first visible tab is selected, else "  " (2 chars)
	// - Separator after each tab:
	//   - " \_" (3 chars) if current tab is selected
	//   - "_/ " (3 chars) if next tab is selected
	//   - "  " (2 chars) otherwise
	//
	// A join is three cells: the diagonal, the cell on the TAB's side of it
	// carrying the focus marker ("<" or ">") when the strip has the focus, and
	// the cell on the BAR's side carrying an underscore. That underscore draws
	// at the foot of its cell, which is where a top strip's line runs, so the
	// line reads as one unbroken run into the tab on a terminal that cannot
	// underline. It sits against the neighbouring label, the far side of the
	// join from the tab it belongs to.
	// Where the run BEGINS: past any overflow mark, and past whatever
	// slack the alignment put in front of it (see tabRunOffset).
	x := leftEllipseWidth + t.tabRunOffset()
	// The bar in front of the run carries the strip's edge like the
	// filler behind it does, so the line runs unbroken across.
	for at := leftEllipseWidth; at < x; at += metrics.UnitsPerCellWidth {
		tape.cell(at, 0, ' ', tabBarUnderlined)
	}

	// Track the style of the last tab being drawn (for ellipsis coloring)
	var lastTabStyle style.CellStyle // Style of the last visible tab (for ellipsis when no text drawn)
	// Where the last visible tab's label went onto the tape, and what it read:
	// the dots that stand for what was cut off it belong to that word, so the
	// two are tied when the word reads the other way from the strip.
	lastLabelMark, lastLabelText := -1, ""
	label := func(x, y core.Unit, text string, st style.CellStyle) {
		lastLabelMark, lastLabelText = tape.at(), text
		tape.text(x, y, text, st, font)
	}
	tabWasTruncated := false
	// zeroCharTab records that the last visible tab was clipped so hard that
	// not one character of its label was drawn. The trailing dots then stand
	// for that tab and are drawn in its colours, so they are part of it.
	zeroCharTab := false
	drewAnyText := false // Track if we drew at least 1 character of text for last tab

	// Track positions for external ellipsis handling
	// lastTextEndX: where the last visible tab's text ended (for interior ellipsis)
	// lastSlashX: position of the backslash in the last separator (-1 if none)
	lastTextEndX := core.Unit(0)
	lastSlashX := core.Unit(-1)
	// Selected-tab silhouette anchors (graphical shape post-pass).
	selLeadX := core.Unit(-1)
	selTrailX := core.Unit(-1)
	selEndX := core.Unit(-1)
	selShapeStyle := selectedStyle

	visibleTabs := t.tabs[t.tabScrollOffset:]
	for i := 0; i < len(visibleTabs); i++ {
		tabIndex := t.tabScrollOffset + i
		tab := visibleTabs[i]
		// Everything from here to the next tab is this tab's, so the mouse can
		// find it later without working the layout out a second time.
		tape.owned(tabIndex)
		isSelected := tabIndex == t.currentIndex
		isFirstVisible := i == 0
		isLastVisible := tabIndex == len(t.tabs)-1
		nextIsSelected := !isLastVisible && tabIndex+1 == t.currentIndex

		// Calculate this tab's width
		// When left ellipsis is showing, omit the leading space from the prefix
		hasLeftEllipsis := t.tabScrollOffset > 0
		prefixWidth := 0
		if isFirstVisible {
			if hasLeftEllipsis {
				// Tighter prefix when ellipsis showing: "/<" (2) or " " (1)
				prefixWidth = 2 // "/<" if selected
				if !isSelected {
					prefixWidth = 1 // " " if not selected
				}
			} else {
				prefixWidth = 3 // " /<" if selected
				if !isSelected {
					prefixWidth = 2 // "  " if not selected
				}
			}
		}
		sepWidth := 2 // Default "  "
		if isSelected || nextIsSelected {
			sepWidth = 3 // " \ " or " /<"
		}
		// Calculate tab width: prefix and separator are cell-based, text uses font measurement
		tabSlotWidth := core.Unit(prefixWidth+sepWidth)*metrics.UnitsPerCellWidth + t.MeasureText(tab.Text)

		// A tab with more tabs after it has to leave the strip room for its own
		// "more tabs" ellipsis. Where the label and that ellipsis cannot both
		// stand, this tab carries the mark itself: the label is trimmed and
		// the dots go on the end of it.
		//
		// The room the ellipsis needs is what it MEASURES -- a proportional
		// run of dots -- and what it has to fit beside is the LABEL. Charging
		// four or five whole cells, and counting the separator that leads into
		// the next tab, trimmed names that had room to be whole: the strip
		// drew its ellipsis after them anyway, so the name paid a letter for a
		// second ellipsis beside the first, and for a separator into a tab the
		// strip was never going to show.
		forceInternalEllipsis := false
		if needsScrolling && (isSelected || nextIsSelected) && tabIndex != len(t.tabs)-1 {
			labelEnd := x + core.Unit(prefixWidth)*metrics.UnitsPerCellWidth + t.MeasureText(tab.Text)
			if labelEnd+t.overflowEllipsisWidth() > availableWidth {
				forceInternalEllipsis = true
			}
		}

		// Check if this tab fits (or if we need to force internal ellipsis)
		if forceInternalEllipsis || x+tabSlotWidth > availableWidth {
			// The run stops here, whether or not any of this tab gets drawn.
			tape.cutShortAt(tabIndex)

			// Check if we're in the "grace margin" - ONLY if all essential content fits
			// and we're not forcing internal ellipsis.
			// Essential = prefix + text + (space/bracket + backslash for selected)
			// Non-essential = trailing underscore + space (or trailing spaces for unselected)
			// IMPORTANT: Grace margin only applies to the ACTUAL last tab - if there are more
			// tabs after this, we need proper ellipsis handling, not grace margin.
			inGraceMargin := false
			isActualLastTab := tabIndex == len(t.tabs)-1
			if !forceInternalEllipsis && isLastVisible && isActualLastTab {
				essentialSepWidth := 0
				if isSelected {
					essentialSepWidth = 2 // space/bracket + backslash are essential
				}
				essentialWidth := core.Unit(prefixWidth+essentialSepWidth)*metrics.UnitsPerCellWidth + t.MeasureText(tab.Text)
				if x+essentialWidth <= availableWidth {
					inGraceMargin = true
				}
			}

			var s style.CellStyle
			if !tab.Enabled {
				s = disabledStyle
			} else if isSelected {
				if hasFocus {
					s = focusedSelectedStyle
				} else {
					s = selectedStyle
				}
			} else {
				s = tabBarUnderlined // Unselected tabs are underlined
			}

			if inGraceMargin {
				// Grace margin: draw full text + as much separator as fits
				// No ellipsis needed since only non-essential content is cut off

				// Draw prefix if first visible
				if isFirstVisible {
					if isSelected {
						if hasLeftEllipsis {
							// "/<" (2 chars) - tighter when ellipsis showing
							tape.cell(x, 0, slashCh, tabBarStyle)
							selLeadX = x
							selShapeStyle = s
							if hasFocus {
								tape.cell(x+metrics.UnitsPerCellWidth, 0, '<', focusedSelectedStyle)
							} else {
								tape.cell(x+metrics.UnitsPerCellWidth, 0, ' ', s)
							}
							x += metrics.UnitsPerCellWidth * 2
						} else {
							tape.cell(x, 0, underscoreCh, tabBarUnderlined)
							tape.cell(x+metrics.UnitsPerCellWidth, 0, slashCh, tabBarStyle)
							selLeadX = x + metrics.UnitsPerCellWidth
							selShapeStyle = s
							if hasFocus {
								tape.cell(x+metrics.UnitsPerCellWidth*2, 0, '<', focusedSelectedStyle)
							} else {
								tape.cell(x+metrics.UnitsPerCellWidth*2, 0, ' ', s)
							}
							x += metrics.UnitsPerCellWidth * 3
						}
					} else {
						if hasLeftEllipsis {
							// " " (1 char) - single space when ellipsis showing
							tape.cell(x, 0, ' ', tabBarUnderlined)
							x += metrics.UnitsPerCellWidth
						} else {
							tape.cell(x, 0, ' ', tabBarUnderlined)
							tape.cell(x+metrics.UnitsPerCellWidth, 0, ' ', tabBarUnderlined)
							x += metrics.UnitsPerCellWidth * 2
						}
					}
				}

				// Draw full text using font-aware rendering (with a tab-color
				// foundation on pixel surfaces + transparent glyphs so the
				// label/separator seam can't show the bar color - see the
				// normal path below).
				graceStyle := s
				graceRun := t.CellRun(tab.Text)
				if p.Graphical() && isSelected {
					tape.fill(core.UnitRect{X: x, Y: 0, Width: t.MeasureText(graceRun) + metrics.UnitsPerCellWidth, Height: tabHeight}, ' ', s)
					graceStyle = s.WithBg(style.ColorTransparent)
				}
				label(x, 0, graceRun, graceStyle)
				x += t.MeasureText(graceRun)
				lastTextEndX = x // Track where text ends
				lastSlashX = -1  // Reset slash tracking

				// Draw as much separator as fits (character by character)
				if isSelected {
					// ">\_ " or " \_ " - backslash is essential, underscore+space are not
					if x < availableWidth {
						if hasFocus {
							tape.cell(x, 0, '>', focusedSelectedStyle)
						} else {
							tape.cell(x, 0, ' ', s)
						}
						x += metrics.UnitsPerCellWidth
					}
					if x < availableWidth {
						tape.cell(x, 0, backslashCh, tabBarStyle)
						lastSlashX = x // Track backslash position
						selTrailX = x
						x += metrics.UnitsPerCellWidth
					} else {
						selEndX = x
					}
					if x < availableWidth {
						tape.cell(x, 0, underscoreCh, tabBarUnderlined)
						x += metrics.UnitsPerCellWidth
					}
				} else {
					// "  " - both are non-essential filler
					if x < availableWidth {
						tape.cell(x, 0, ' ', tabBarUnderlined)
						x += metrics.UnitsPerCellWidth
					}
					if x < availableWidth {
						tape.cell(x, 0, ' ', tabBarUnderlined)
						x += metrics.UnitsPerCellWidth
					}
				}
				// Not marked as truncated - tab is essentially complete
				break
			}

			// Not in grace margin - try to draw partial tab with ellipsis reserve
			remainingSpace := availableWidth - x
			minPartialWidth := metrics.TextWidth(prefixWidth) // just prefix needed
			if remainingSpace >= minPartialWidth {
				// Draw prefix if first visible
				if isFirstVisible {
					if isSelected {
						if hasLeftEllipsis {
							// "/<" (2 chars) - tighter when ellipsis showing
							tape.cell(x, 0, slashCh, tabBarStyle)
							selLeadX = x
							selShapeStyle = s
							if hasFocus {
								tape.cell(x+metrics.UnitsPerCellWidth, 0, '<', focusedSelectedStyle)
							} else {
								tape.cell(x+metrics.UnitsPerCellWidth, 0, ' ', s)
							}
							x += metrics.UnitsPerCellWidth * 2
						} else {
							tape.cell(x, 0, underscoreCh, tabBarUnderlined)
							tape.cell(x+metrics.UnitsPerCellWidth, 0, slashCh, tabBarStyle)
							selLeadX = x + metrics.UnitsPerCellWidth
							selShapeStyle = s
							if hasFocus {
								tape.cell(x+metrics.UnitsPerCellWidth*2, 0, '<', focusedSelectedStyle)
							} else {
								tape.cell(x+metrics.UnitsPerCellWidth*2, 0, ' ', s)
							}
							x += metrics.UnitsPerCellWidth * 3
						}
					} else {
						if hasLeftEllipsis {
							// " " (1 char) - single space when ellipsis showing
							tape.cell(x, 0, ' ', tabBarUnderlined)
							x += metrics.UnitsPerCellWidth
						} else {
							tape.cell(x, 0, ' ', tabBarUnderlined)
							tape.cell(x+metrics.UnitsPerCellWidth, 0, ' ', tabBarUnderlined)
							x += metrics.UnitsPerCellWidth * 2
						}
					}
				}

				// Calculate how much text we can show (leave room for ellipsis)
				ellipsisReserve := core.Unit(0)
				if needsScrolling {
					ellipsisReserve = t.overflowEllipsisWidth()
				}
				maxTextWidth := availableWidth - x - ellipsisReserve
				if maxTextWidth < 0 {
					maxTextWidth = 0
				}

				// When forcing internal ellipsis, ensure we actually truncate the text
				// (don't show complete text followed by "..." which looks like external ellipsis)
				if forceInternalEllipsis {
					fullTextWidth := t.MeasureText(tab.Text)
					if maxTextWidth >= fullTextWidth {
						// Trim by ONE UNIT, the finest step there is, so the
						// budget falls just short of the whole label and the
						// last character is the only one that cannot be drawn.
						// Trimming a whole CELL instead spent a cell of budget
						// on a label measured in proportional glyphs: where the
						// last letter was narrower than a cell it took the one
						// before it down as well, so widening the strip by a
						// pixel could turn "Termina..." into "Termin...".
						maxTextWidth = fullTextWidth - 1
						if maxTextWidth < 0 {
							maxTextWidth = 0
						}
					}
				}

				// Find how many characters fit within maxTextWidth using font measurement
				textRunes := []rune(tab.Text)
				charsToShow := 0
				currentWidth := core.Unit(0)
				for j, ch := range textRunes {
					charWidth := t.MeasureText(string(ch))
					if currentWidth+charWidth > maxTextWidth {
						break
					}
					currentWidth += charWidth
					charsToShow = j + 1
				}

				// Draw partial text using font-aware rendering
				if charsToShow > 0 {
					// Cut to fit first, prepared after: what is trimmed is the
					// caption, and what is drawn is the run made from what is
					// left of it.
					partialText := t.CellRun(string(textRunes[:charsToShow]))
					// Tab-color foundation + transparent glyphs so the seam to the
					// ellipsis/separator can't leak the bar color (pixel surfaces).
					partStyle := s
					if p.Graphical() && isSelected {
						tape.fill(core.UnitRect{X: x, Y: 0, Width: t.MeasureText(partialText) + metrics.UnitsPerCellWidth, Height: tabHeight}, ' ', s)
						partStyle = s.WithBg(style.ColorTransparent)
					}
					label(x, 0, partialText, partStyle)
					x += t.MeasureText(partialText)
					lastTextEndX = x
					lastSlashX = -1 // Reset - no separator drawn in truncation path
					lastTabStyle = s

					// Only draw interior ellipsis if text was actually truncated
					// If text is complete, let the "more tabs" ellipsis handle it (external style)
					actuallyTruncated := charsToShow < len(textRunes)
					if actuallyTruncated && needsScrolling {
						if p.Graphical() {
							// Paint the whole reserved slot: the measured
							// dots can be narrower than the reserve, and
							// the gap must stay in the tab's color.
							tape.fill(core.UnitRect{X: x, Width: t.overflowEllipsisWidth(), Height: metrics.UnitsPerCellHeight}, ' ', s)
							tape.text(x, 0, "...", s, font)
						} else {
							for i := 0; i < 3; i++ {
								tape.cell(x+core.Unit(i)*metrics.UnitsPerCellWidth, 0, '.', s)
							}
						}
						x += t.overflowEllipsisWidth()
						tabWasTruncated = true
						// These dots stand for what was cut off THIS WORD, so
						// they belong to it: when the word reads the other way
						// from the strip the two are tied and turn over as one,
						// and the word keeps its dots on the end it ends at.
						if d := text.FirstStrongDirection(lastLabelText); d != core.DirInherit &&
							d != core.FindEffectiveDirection(t) {
							tape.tie(lastLabelMark)
						}
					}
					drewAnyText = true
				} else {
					// No text drawn - reset tracking for external ellipsis
					zeroCharTab = true
					lastSlashX = -1
					lastTabStyle = s
					drewAnyText = false
				}
				if isSelected {
					selEndX = x
				}
				// If charsToShow == 0, we didn't draw any text for this tab,
				// so we don't set tabWasTruncated - let the "more tabs" ellipsis handle it
			}
			break
		}

		var s style.CellStyle
		if !tab.Enabled {
			s = disabledStyle
		} else if isSelected {
			if hasFocus {
				s = focusedSelectedStyle
			} else {
				s = selectedStyle
			}
		} else {
			s = tabBarUnderlined // Unselected tabs are underlined
		}

		// Draw prefix if first visible tab
		if isFirstVisible {
			if isSelected {
				if hasLeftEllipsis {
					// "/<" (2 chars) - tighter when ellipsis showing
					tape.cell(x, 0, slashCh, tabBarStyle) // slash not underlined
					selLeadX = x
					selShapeStyle = s
					if hasFocus {
						tape.cell(x+metrics.UnitsPerCellWidth, 0, '<', focusedSelectedStyle)
					} else {
						tape.cell(x+metrics.UnitsPerCellWidth, 0, ' ', s)
					}
					x += metrics.UnitsPerCellWidth * 2
				} else {
					// "_/<" (3 chars) when focused, "_/ " when not focused
					tape.cell(x, 0, underscoreCh, tabBarUnderlined)
					tape.cell(x+metrics.UnitsPerCellWidth, 0, slashCh, tabBarStyle) // slash not underlined
					selLeadX = x + metrics.UnitsPerCellWidth
					selShapeStyle = s
					if hasFocus {
						tape.cell(x+metrics.UnitsPerCellWidth*2, 0, '<', focusedSelectedStyle)
					} else {
						tape.cell(x+metrics.UnitsPerCellWidth*2, 0, ' ', s)
					}
					x += metrics.UnitsPerCellWidth * 3
				}
			} else {
				if hasLeftEllipsis {
					// " " (1 char) - single space when ellipsis showing
					tape.cell(x, 0, ' ', tabBarUnderlined)
					x += metrics.UnitsPerCellWidth
				} else {
					// "  " (2 chars) - double space when no ellipsis
					tape.cell(x, 0, ' ', tabBarUnderlined)
					tape.cell(x+metrics.UnitsPerCellWidth, 0, ' ', tabBarUnderlined)
					x += metrics.UnitsPerCellWidth * 2
				}
			}
		}

		// Draw tab text using font-aware rendering. What goes down is the
		// caption prepared for this target, so the foundation under it, the pen
		// after it and the close button on its last cell all measure the run
		// that is drawn.
		textStartX := x
		labelRun := t.CellRun(tab.Text)
		textWidth := t.MeasureText(labelRun)
		// Solid tab-color foundation under the label and its trailing cell, so
		// the sub-pixel seam between the proportional label (unsnapped rate)
		// and the cell-based separator (cell rate) can't show the bar color
		// through at a fractional font size. With the foundation carrying the
		// background, draw the glyphs transparent so the label's own bg box
		// (a line-height raster) can't nibble the edge stripe above or below.
		textStyle := s
		if p.Graphical() && isSelected {
			tape.fill(core.UnitRect{X: x, Y: 0, Width: textWidth + metrics.UnitsPerCellWidth, Height: tabHeight}, ' ', s)
			textStyle = s.WithBg(style.ColorTransparent)
		}
		label(x, 0, labelRun, textStyle)
		x += textWidth

		// Draw close button if closable (at end of text, before separator)
		if t.closable || tab.Closable {
			// Position close button at end of text
			closeX := textStartX + textWidth - metrics.UnitsPerCellWidth
			if closeX < textStartX {
				closeX = textStartX
			}
			tape.cell(closeX, 0, '×', s)
		}
		_ = textStartX // May use later for close button positioning

		// Track text end position and style for ellipsis handling
		lastTextEndX = x
		lastSlashX = -1    // Reset slash tracking
		lastTabStyle = s   // Track style for ellipsis coloring
		drewAnyText = true // We drew complete text

		// Draw separator after tab
		if isSelected {
			// ">\_" (3 chars) when focused, " \_" when not focused
			// Space/bracket adjacent to label not underlined, rest underlined except slash (none here)
			if hasFocus {
				tape.cell(x, 0, '>', focusedSelectedStyle)
			} else {
				tape.cell(x, 0, ' ', s)
			}
			tape.cell(x+metrics.UnitsPerCellWidth, 0, backslashCh, tabBarStyle) // backslash not underlined (like slash)
			lastSlashX = x + metrics.UnitsPerCellWidth                          // Track backslash position
			selTrailX = x + metrics.UnitsPerCellWidth
			tape.cell(x+metrics.UnitsPerCellWidth*2, 0, underscoreCh, tabBarUnderlined)
			x += metrics.UnitsPerCellWidth * 3
		} else if nextIsSelected {
			// "_/<" (3 chars) when focused, "_/ " when not focused
			// Underlined except slash and space/bracket adjacent to selected label
			tape.cell(x, 0, underscoreCh, tabBarUnderlined)
			tape.cell(x+metrics.UnitsPerCellWidth, 0, slashCh, tabBarStyle) // slash not underlined
			lastSlashX = x + metrics.UnitsPerCellWidth                      // Track slash position
			selLeadX = x + metrics.UnitsPerCellWidth
			if hasFocus {
				selShapeStyle = focusedSelectedStyle
			} else {
				selShapeStyle = selectedStyle
			}
			if hasFocus {
				tape.cell(x+metrics.UnitsPerCellWidth*2, 0, '<', focusedSelectedStyle)
			} else {
				tape.cell(x+metrics.UnitsPerCellWidth*2, 0, ' ', selectedStyle)
			}
			x += metrics.UnitsPerCellWidth * 3
		} else {
			// "  " (2 chars) regular separator - underlined
			tape.cell(x, 0, ' ', tabBarUnderlined)
			tape.cell(x+metrics.UnitsPerCellWidth, 0, ' ', tabBarUnderlined)
			x += metrics.UnitsPerCellWidth * 2
		}
	}
	// Past the tabs, the marks are the strip's own again.
	tape.pastTheTabs()

	// Fill any gap between last tab and scroll buttons/edge
	// When ellipsis is needed, ensure we leave room for it by trimming fill spaces
	scrollAreaStart := bounds.Width - scrollButtonsWidth
	needsEllipsis := needsScrolling && !t.isLastTabFullyVisible()

	if needsEllipsis {
		// Calculate where ellipsis should ideally start
		ellipsisWidth := t.overflowEllipsisWidth()
		idealEllipsisX := scrollAreaStart - ellipsisWidth

		if tabWasTruncated {
			// The tab has already drawn its own ellipsis, so it has said
			// everything it can: close it there rather than running its colour
			// on to the scroll buttons, and the rest of the strip is strip.
			if selEndX >= 0 {
				selEndX = x
			}
			// And the strip adds no ellipsis of its own. One mark at this end
			// of the run, never two: the tab's dots already say the run is cut
			// short, and the [>] button says whether there is more to reach.
			for x < scrollAreaStart {
				tape.cell(x, 0, ' ', tabBarUnderlined)
				x += metrics.UnitsPerCellWidth
			}
		} else {
			// Text wasn't truncated - need to draw ellipsis
			// The ellipsis can overwrite trailing whitespace (underscore, space) but NOT the backslash
			ellipsisX := idealEllipsisX
			useInternalStyle := false // Whether to use tab's internal style for ellipsis

			// If there's a backslash in the separator, ellipsis must start after it
			minEllipsisX := core.Unit(0)
			if lastSlashX >= 0 {
				minEllipsisX = lastSlashX + metrics.UnitsPerCellWidth // Right after the backslash
			}

			// Check if ideal position would overwrite the backslash
			if lastSlashX >= 0 && idealEllipsisX <= lastSlashX {
				// Would overwrite backslash - use position right after it
				ellipsisX = minEllipsisX

				// Check if at least 1 dot would fit after the backslash
				if ellipsisX+metrics.UnitsPerCellWidth > scrollAreaStart {
					// No room for even 1 dot after backslash - use interior ellipsis
					// Draw ellipsis right after text, overwriting the separator
					ellipsisX = lastTextEndX
					useInternalStyle = true
				}
			}

			// Determine ellipsis style: use tab's internal style if no text was drawn
			// or if we're falling back to interior ellipsis position
			ellipsisStyle := tabBarUnderlined
			if !drewAnyText || useInternalStyle {
				ellipsisStyle = lastTabStyle
			}

			// Fill gap between current position and ellipsis (only if ellipsis is after x)
			gapStart := x
			for x < ellipsisX {
				tape.cell(x, 0, ' ', tabBarUnderlined)
				x += metrics.UnitsPerCellWidth
			}

			// Draw as many dots as will fit before scroll buttons
			dotsDrawn := 0
			if p.Graphical() {
				if ellipsisX+ellipsisWidth <= scrollAreaStart {
					tape.fill(core.UnitRect{X: ellipsisX, Width: ellipsisWidth, Height: metrics.UnitsPerCellHeight}, ' ', ellipsisStyle)
					tape.text(ellipsisX, 0, "...", ellipsisStyle, font)
					dotsDrawn = 3
				}
			} else {
				for i := 0; i < 3; i++ {
					dotX := ellipsisX + core.Unit(i)*metrics.UnitsPerCellWidth
					if dotX+metrics.UnitsPerCellWidth <= scrollAreaStart {
						tape.cell(dotX, 0, '.', ellipsisStyle)
						dotsDrawn++
					}
				}
			}

			// Fill remaining space after ellipsis to scroll buttons
			fillX := ellipsisX + core.Unit(dotsDrawn)*metrics.UnitsPerCellWidth
			if p.Graphical() && dotsDrawn > 0 {
				fillX = ellipsisX + ellipsisWidth
			}
			// The dots belong to the tab when they were pulled back over its
			// separator, and equally when they are all the tab managed to show
			// of itself. Either way the selected tab's silhouette has to reach
			// past them: gated on the first case alone, a tab clipped to no
			// characters closed at the lead-in it had drawn and left its own
			// ellipsis outside.
			if (useInternalStyle || zeroCharTab) && dotsDrawn > 0 {
				if selTrailX >= 0 && ellipsisX <= selTrailX {
					selTrailX = -1
					selEndX = fillX
				}
				if selLeadX >= 0 && ellipsisX <= selLeadX {
					selLeadX = -1
				}
				if selEndX >= 0 && fillX > selEndX {
					selEndX = fillX
				}
				// The shape now reaches past the dots, so the run between the
				// tab and them is INSIDE the tab and wears the tab's colour.
				// Left as strip it was a notch of bar cut out of the tab: the
				// run is filled before the dots are placed, when whose ground
				// it will turn out to be is not yet known. Laid as one rect
				// stopping exactly at the dots, so the whole-cell steps of
				// that fill cannot reach across them.
				if gapStart < ellipsisX {
					tape.fill(core.UnitRect{X: gapStart, Y: 0, Width: ellipsisX - gapStart, Height: tabHeight}, ' ', ellipsisStyle)
				}
				// The dots belong to that tab's WORD, so when the word reads
				// the other way from the strip the two are tied and turn over
				// as one: the word keeps its dots on the end the word ends at,
				// not the end the strip does.
				if useInternalStyle && lastLabelMark >= 0 {
					if d := text.FirstStrongDirection(lastLabelText); d != core.DirInherit &&
						d != core.FindEffectiveDirection(t) {
						tape.tie(lastLabelMark)
					}
				}
			}
			for fillX < scrollAreaStart {
				tape.cell(fillX, 0, ' ', tabBarUnderlined)
				fillX += metrics.UnitsPerCellWidth
			}
		}
	} else {
		// No ellipsis needed - just fill to scroll area
		for x < scrollAreaStart {
			tape.cell(x, 0, ' ', tabBarUnderlined)
			x += metrics.UnitsPerCellWidth
		}
	}

	// Draw scroll buttons if needed (all underlined)
	if needsScrolling {
		buttonX := bounds.Width - scrollButtonsWidth
		disabledStyle := tabBarUnderlined.WithFg(style.ColorBrightBlack)

		// [<] button - disabled when can't scroll left
		tape.owned(stripScrollLo)
		canLeft := t.canScrollLeft()
		if canLeft {
			leftStyle := tabBarUnderlined
			if t.scrollButtonPressed == -1 && t.scrollLeftHovered {
				leftStyle = pressedStyle
			}
			tape.cell(buttonX, 0, '[', leftStyle)
			tape.cell(buttonX+metrics.UnitsPerCellWidth, 0, '<', leftStyle)
			tape.cell(buttonX+metrics.UnitsPerCellWidth*2, 0, ']', leftStyle)
		} else {
			// Disabled: " < " (no brackets, grayed out)
			tape.cell(buttonX, 0, ' ', disabledStyle)
			tape.cell(buttonX+metrics.UnitsPerCellWidth, 0, '<', disabledStyle)
			tape.cell(buttonX+metrics.UnitsPerCellWidth*2, 0, ' ', disabledStyle)
		}

		// [>] button - disabled when can't scroll right
		tape.owned(stripScrollHi)
		canRight := t.canScrollRight()
		if canRight {
			rightStyle := tabBarUnderlined
			if t.scrollButtonPressed == 1 && t.scrollRightHovered {
				rightStyle = pressedStyle
			}
			tape.cell(buttonX+metrics.UnitsPerCellWidth*3, 0, '[', rightStyle)
			tape.cell(buttonX+metrics.UnitsPerCellWidth*4, 0, '>', rightStyle)
			tape.cell(buttonX+metrics.UnitsPerCellWidth*5, 0, ']', rightStyle)
		} else {
			// Disabled: " > " (no brackets, grayed out)
			tape.cell(buttonX+metrics.UnitsPerCellWidth*3, 0, ' ', disabledStyle)
			tape.cell(buttonX+metrics.UnitsPerCellWidth*4, 0, '>', disabledStyle)
			tape.cell(buttonX+metrics.UnitsPerCellWidth*5, 0, ' ', disabledStyle)
		}
	}

	// What the run made is also what the mouse will read.
	t.stripSpans = tape.spans()

	// Everything above worked out WHERE, in the strip's own run. Here it
	// becomes places on the screen -- before the silhouette, which paints
	// over the finished strip.
	tape.replay(p, bounds.Width, core.ChromeMirrored(t))

	// Selected-tab silhouette and the strip's continuous edge line,
	// drawn over the finished cell material.
	if p.Graphical() {
		t.paintTabShape(p, 0, bounds.Width, selLeadX, selTrailX, selEndX, selShapeStyle, tabBarStyle, true, core.ChromeMirrored(t))
	}

	// Draw separator row if enabled (in active tab color)
	if t.showSeparator {
		separatorStyle := scheme.GetActiveTab()
		p.FillRect(core.UnitRect{
			X:      0,
			Y:      tabHeight,
			Width:  bounds.Width,
			Height: metrics.UnitsPerCellHeight,
		}, ' ', separatorStyle)
	}
}

func (t *TabTrinket) paintBottomTabs(p *core.Painter, bounds core.UnitRect, scheme *style.Scheme, metrics core.CellMetrics) {
	tabHeight := t.tabBarHeight()
	tabY := bounds.Height - tabHeight
	tape := t.newStripTape()
	hasFocus := t.HasFocus()
	font := t.EffectiveFont()

	// Tab bar style from scheme
	tabBarStyle := scheme.GetTabsBar(true)
	// Tab bar style with overline - used for areas outside the active tab opening
	// This causes the content border above to be underlined everywhere except the active tab
	tabBarOverlined := tabBarStyle.Overline()
	// Selected tab style: uses ActiveTab colors with underline (no overline)
	selectedStyle := scheme.GetActiveTab().Underline()
	// Focused selected tab style: from scheme with underline (no overline)
	focusedSelectedStyle := scheme.GetFocusedTab().Underline()
	// Pressed button style from scheme with overline
	pressedStyle := scheme.GetPressedTabsButton().Overline()
	// Pixel surfaces draw the strip's edge as one continuous hairline
	// in a post-pass (paintTabShape); the cell attributes and '_'
	// filler glyphs would double that line, so they are dropped.
	underscoreCh := '_'
	slashCh, backslashCh := '/', '\\'
	if p.Graphical() {
		tabBarOverlined = tabBarStyle
		selectedStyle = scheme.GetActiveTab()
		focusedSelectedStyle = scheme.GetFocusedTab()
		pressedStyle = scheme.GetPressedTabsButton()
		underscoreCh = ' '
		// The arcs and edge line of paintTabShape replace the literal
		// slash glyphs on pixel surfaces.
		slashCh, backslashCh = ' ', ' '
	}
	// Disabled style
	disabledStyle := tabBarOverlined.WithFg(scheme.GetDisabledTextFG())

	// Draw tab bar background with overline (will be overwritten by active tab area without overline)
	tape.fill(core.UnitRect{Y: tabY, Width: bounds.Width, Height: tabHeight}, ' ', tabBarOverlined)

	// Calculate if we need scroll buttons
	needsScrolling := t.tabsNeedScrolling()
	scrollButtonsWidth := core.Unit(0)
	if needsScrolling {
		scrollButtonsWidth = metrics.TextWidth(6) // [<][>] = 6 chars
	}

	// If scrolled right, show left ellipse indicator (clickable to scroll left)
	leftEllipseWidth := core.Unit(0)
	if t.tabScrollOffset > 0 {
		leftEllipseWidth = t.overflowEllipsisWidth() // "..."
		// Draw the left ellipse (with overline)
		tape.owned(stripOverflow)
		if p.Graphical() {
			tape.text(0, tabY, "...", tabBarOverlined, font)
		} else {
			for i := 0; i < 3; i++ {
				tape.cell(metrics.CellToUnitsX(i), tabY, '.', tabBarOverlined)
			}
		}
		tape.owned(stripSelf)
	}

	// Available width is the absolute position where tabs must stop (before scroll buttons)
	availableWidth := bounds.Width - scrollButtonsWidth

	// Tab format for bottom tabs (inverted connectors):
	// - Prefix: " \_" (3 chars) if first visible tab is selected, else "  " (2 chars)
	// - Separator after each tab:
	//   - "_/ " (3 chars) if current tab is selected
	//   - " \_" (3 chars) if next tab is selected
	//   - "  " (2 chars) otherwise
	// Where the run BEGINS: past any overflow mark, and past whatever
	// slack the alignment put in front of it (see tabRunOffset).
	x := leftEllipseWidth + t.tabRunOffset()
	// The bar in front of the run carries the strip's edge like the
	// filler behind it does, so the line runs unbroken across.
	for at := leftEllipseWidth; at < x; at += metrics.UnitsPerCellWidth {
		tape.cell(at, tabY, ' ', tabBarOverlined)
	}

	// Track the style of the last tab being drawn (for ellipsis coloring)
	var lastTabStyle style.CellStyle // Style of the last visible tab (for ellipsis when no text drawn)
	// Where the last visible tab's label went onto the tape, and what it read:
	// the dots that stand for what was cut off it belong to that word, so the
	// two are tied when the word reads the other way from the strip.
	lastLabelMark, lastLabelText := -1, ""
	label := func(x, y core.Unit, text string, st style.CellStyle) {
		lastLabelMark, lastLabelText = tape.at(), text
		tape.text(x, y, text, st, font)
	}
	tabWasTruncated := false
	// zeroCharTab records that the last visible tab was clipped so hard that
	// not one character of its label was drawn. The trailing dots then stand
	// for that tab and are drawn in its colours, so they are part of it.
	zeroCharTab := false
	drewAnyText := false // Track if we drew at least 1 character of text for last tab

	// Track positions for external ellipsis handling
	// lastTextEndX: where the last visible tab's text ended (for interior ellipsis)
	// lastSlashX: position of the slash/backslash in the last separator (-1 if none)
	// lastTabWasSelected: if true, lastSlashX is for a slash (ellipsis goes after it)
	//                     if false, lastSlashX is for a backslash (ellipsis goes before it)
	lastTextEndX := core.Unit(0)
	lastSlashX := core.Unit(-1)
	// Selected-tab silhouette anchors (graphical shape post-pass).
	selLeadX := core.Unit(-1)
	selTrailX := core.Unit(-1)
	selEndX := core.Unit(-1)
	selShapeStyle := selectedStyle
	lastTabWasSelected := false

	visibleTabs := t.tabs[t.tabScrollOffset:]
	for i := 0; i < len(visibleTabs); i++ {
		tabIndex := t.tabScrollOffset + i
		tab := visibleTabs[i]
		// Everything from here to the next tab is this tab's, so the mouse can
		// find it later without working the layout out a second time.
		tape.owned(tabIndex)
		isSelected := tabIndex == t.currentIndex
		isFirstVisible := i == 0
		isLastVisible := tabIndex == len(t.tabs)-1
		nextIsSelected := !isLastVisible && tabIndex+1 == t.currentIndex

		// Calculate this tab's width
		// When left ellipsis is showing, omit one leading space from prefix
		hasLeftEllipsis := t.tabScrollOffset > 0
		prefixWidth := 0
		if isFirstVisible {
			if hasLeftEllipsis {
				// Tighter prefix when ellipsis showing: "\_" (2) or " " (1)
				prefixWidth = 2 // "\_" if selected
				if !isSelected {
					prefixWidth = 1 // " " if not selected
				}
			} else {
				prefixWidth = 3 // " \_" if selected
				if !isSelected {
					prefixWidth = 2 // "  " if not selected
				}
			}
		}
		sepWidth := 2 // Default "  "
		if isSelected || nextIsSelected {
			sepWidth = 3 // "_/ " or " \_"
		}
		// Calculate tab width: prefix and separator are cell-based, text uses font measurement
		tabSlotWidth := core.Unit(prefixWidth+sepWidth)*metrics.UnitsPerCellWidth + t.MeasureText(tab.Text)

		// A tab with more tabs after it has to leave the strip room for its own
		// "more tabs" ellipsis. Where the label and that ellipsis cannot both
		// stand, this tab carries the mark itself: the label is trimmed and
		// the dots go on the end of it.
		//
		// The room the ellipsis needs is what it MEASURES -- a proportional
		// run of dots -- and what it has to fit beside is the LABEL. Charging
		// four or five whole cells, and counting the separator that leads into
		// the next tab, trimmed names that had room to be whole: the strip
		// drew its ellipsis after them anyway, so the name paid a letter for a
		// second ellipsis beside the first, and for a separator into a tab the
		// strip was never going to show.
		forceInternalEllipsis := false
		if needsScrolling && (isSelected || nextIsSelected) && tabIndex != len(t.tabs)-1 {
			labelEnd := x + core.Unit(prefixWidth)*metrics.UnitsPerCellWidth + t.MeasureText(tab.Text)
			if labelEnd+t.overflowEllipsisWidth() > availableWidth {
				forceInternalEllipsis = true
			}
		}

		// Check if this tab fits (or if we need to force internal ellipsis)
		if forceInternalEllipsis || x+tabSlotWidth > availableWidth {
			// The run stops here, whether or not any of this tab gets drawn.
			tape.cutShortAt(tabIndex)

			// Try to draw partial tab (ellipsis is drawn separately after the loop)
			remainingSpace := availableWidth - x
			minPartialWidth := metrics.TextWidth(prefixWidth) // just prefix needed
			if remainingSpace >= minPartialWidth {
				var s style.CellStyle
				if !tab.Enabled {
					s = disabledStyle
				} else if isSelected {
					if hasFocus {
						s = focusedSelectedStyle
					} else {
						s = selectedStyle
					}
				} else {
					s = tabBarOverlined
				}

				// Draw prefix if first visible
				if isFirstVisible {
					if isSelected {
						if hasLeftEllipsis {
							// "\_" or "<" (2 chars) - tighter when ellipsis showing
							tape.cell(x, tabY, backslashCh, tabBarStyle)
							selLeadX = x
							selShapeStyle = s
							if hasFocus {
								tape.cell(x+metrics.UnitsPerCellWidth, tabY, '<', focusedSelectedStyle)
							} else {
								tape.cell(x+metrics.UnitsPerCellWidth, tabY, underscoreCh, s)
							}
							x += metrics.UnitsPerCellWidth * 2
						} else {
							tape.cell(x, tabY, ' ', tabBarOverlined)
							tape.cell(x+metrics.UnitsPerCellWidth, tabY, backslashCh, tabBarStyle)
							selLeadX = x + metrics.UnitsPerCellWidth
							selShapeStyle = s
							if hasFocus {
								tape.cell(x+metrics.UnitsPerCellWidth*2, tabY, '<', focusedSelectedStyle)
							} else {
								tape.cell(x+metrics.UnitsPerCellWidth*2, tabY, underscoreCh, s)
							}
							x += metrics.UnitsPerCellWidth * 3
						}
					} else {
						if hasLeftEllipsis {
							// " " (1 char) - single space when ellipsis showing
							tape.cell(x, tabY, ' ', tabBarOverlined)
							x += metrics.UnitsPerCellWidth
						} else {
							tape.cell(x, tabY, ' ', tabBarOverlined)
							tape.cell(x+metrics.UnitsPerCellWidth, tabY, ' ', tabBarOverlined)
							x += metrics.UnitsPerCellWidth * 2
						}
					}
				}

				// Calculate how much text we can show (leave room for ellipsis)
				ellipsisReserve := core.Unit(0)
				if needsScrolling {
					ellipsisReserve = t.overflowEllipsisWidth()
				}
				maxTextWidth := availableWidth - x - ellipsisReserve
				if maxTextWidth < 0 {
					maxTextWidth = 0
				}

				// When forcing internal ellipsis, ensure we actually truncate the text
				// (don't show complete text followed by "..." which looks like external ellipsis)
				if forceInternalEllipsis {
					fullTextWidth := t.MeasureText(tab.Text)
					if maxTextWidth >= fullTextWidth {
						// Trim by ONE UNIT, the finest step there is, so the
						// budget falls just short of the whole label and the
						// last character is the only one that cannot be drawn.
						// Trimming a whole CELL instead spent a cell of budget
						// on a label measured in proportional glyphs: where the
						// last letter was narrower than a cell it took the one
						// before it down as well, so widening the strip by a
						// pixel could turn "Termina..." into "Termin...".
						maxTextWidth = fullTextWidth - 1
						if maxTextWidth < 0 {
							maxTextWidth = 0
						}
					}
				}

				// Find how many characters fit within maxTextWidth using font measurement
				textRunes := []rune(tab.Text)
				charsToShow := 0
				currentWidth := core.Unit(0)
				for j, ch := range textRunes {
					charWidth := t.MeasureText(string(ch))
					if currentWidth+charWidth > maxTextWidth {
						break
					}
					currentWidth += charWidth
					charsToShow = j + 1
				}

				// Draw partial text using font-aware rendering (tab-color
				// foundation + transparent glyphs on pixel surfaces).
				if charsToShow > 0 {
					// Cut to fit first, prepared after: what is trimmed is the
					// caption, and what is drawn is the run made from what is
					// left of it.
					partialText := t.CellRun(string(textRunes[:charsToShow]))
					bpartStyle := s
					if p.Graphical() && isSelected {
						tape.fill(core.UnitRect{X: x, Y: tabY, Width: t.MeasureText(partialText) + metrics.UnitsPerCellWidth, Height: tabHeight}, ' ', s)
						bpartStyle = s.WithBg(style.ColorTransparent)
					}
					label(x, tabY, partialText, bpartStyle)
					x += t.MeasureText(partialText)
					lastTextEndX = x
					lastSlashX = -1 // Reset - no separator drawn in truncation path
					lastTabWasSelected = false
					lastTabStyle = s

					// Only draw interior ellipsis if text was actually truncated
					// If text is complete, let the "more tabs" ellipsis handle it (external style)
					actuallyTruncated := charsToShow < len(textRunes)
					if actuallyTruncated && needsScrolling {
						if p.Graphical() {
							// Paint the whole reserved slot: the measured
							// dots can be narrower than the reserve, and
							// the gap must stay in the tab's color.
							tape.fill(core.UnitRect{X: x, Y: tabY, Width: t.overflowEllipsisWidth(), Height: metrics.UnitsPerCellHeight}, ' ', s)
							tape.text(x, tabY, "...", s, font)
						} else {
							for i := 0; i < 3; i++ {
								tape.cell(x+core.Unit(i)*metrics.UnitsPerCellWidth, tabY, '.', s)
							}
						}
						x += t.overflowEllipsisWidth()
						tabWasTruncated = true
						// These dots stand for what was cut off THIS WORD, so
						// they belong to it: when the word reads the other way
						// from the strip the two are tied and turn over as one,
						// and the word keeps its dots on the end it ends at.
						if d := text.FirstStrongDirection(lastLabelText); d != core.DirInherit &&
							d != core.FindEffectiveDirection(t) {
							tape.tie(lastLabelMark)
						}
					}
					drewAnyText = true
				} else {
					// No text drawn - reset tracking for external ellipsis
					zeroCharTab = true
					lastSlashX = -1
					lastTabWasSelected = false
					lastTabStyle = s
					drewAnyText = false
				}
				if isSelected {
					selEndX = x
				}
				// If charsToShow == 0, we didn't draw any text for this tab,
				// so we don't set tabWasTruncated - let the "more tabs" ellipsis handle it
			}
			break
		}

		var s style.CellStyle
		if !tab.Enabled {
			s = disabledStyle
		} else if isSelected {
			if hasFocus {
				s = focusedSelectedStyle
			} else {
				s = selectedStyle
			}
		} else {
			s = tabBarOverlined
		}

		// Draw prefix if first visible tab
		if isFirstVisible {
			if isSelected {
				if hasLeftEllipsis {
					// "\_" or "<" (2 chars) - tighter when ellipsis showing
					tape.cell(x, tabY, backslashCh, tabBarStyle)
					selLeadX = x
					selShapeStyle = s
					if hasFocus {
						tape.cell(x+metrics.UnitsPerCellWidth, tabY, '<', focusedSelectedStyle)
					} else {
						tape.cell(x+metrics.UnitsPerCellWidth, tabY, underscoreCh, s)
					}
					x += metrics.UnitsPerCellWidth * 2
				} else {
					// " \_" (3 chars) when first tab is selected
					// Space before \ gets overline (outside active tab)
					tape.cell(x, tabY, ' ', tabBarOverlined)
					tape.cell(x+metrics.UnitsPerCellWidth, tabY, backslashCh, tabBarStyle)
					selLeadX = x + metrics.UnitsPerCellWidth
					selShapeStyle = s
					if hasFocus {
						tape.cell(x+metrics.UnitsPerCellWidth*2, tabY, '<', focusedSelectedStyle)
					} else {
						tape.cell(x+metrics.UnitsPerCellWidth*2, tabY, underscoreCh, s)
					}
					x += metrics.UnitsPerCellWidth * 3
				}
			} else {
				if hasLeftEllipsis {
					// " " (1 char) - single space when ellipsis showing
					tape.cell(x, tabY, ' ', tabBarOverlined)
					x += metrics.UnitsPerCellWidth
				} else {
					// "  " (2 chars) - both get overline
					tape.cell(x, tabY, ' ', tabBarOverlined)
					tape.cell(x+metrics.UnitsPerCellWidth, tabY, ' ', tabBarOverlined)
					x += metrics.UnitsPerCellWidth * 2
				}
			}
		}

		// Draw tab text using font-aware rendering. Solid tab-color foundation
		// under the label + its trailing cell, glyphs transparent, so the
		// label/separator seam can't leak the bar color at a fractional font
		// size (mirrors the top-tab path).
		btextStyle := s
		labelRun := t.CellRun(tab.Text)
		if p.Graphical() && isSelected {
			tape.fill(core.UnitRect{X: x, Y: tabY, Width: t.MeasureText(labelRun) + metrics.UnitsPerCellWidth, Height: tabHeight}, ' ', s)
			btextStyle = s.WithBg(style.ColorTransparent)
		}
		label(x, tabY, labelRun, btextStyle)
		x += t.MeasureText(labelRun)
		lastTextEndX = x // Track where text ends
		lastSlashX = -1  // Reset slash tracking
		lastTabWasSelected = false
		lastTabStyle = s   // Track style for ellipsis coloring
		drewAnyText = true // We drew complete text

		// Draw separator after tab (inverted for bottom)
		if isSelected {
			// "_/ " (3 chars) after selected tab
			// Space after / gets overline (outside active tab)
			if hasFocus {
				tape.cell(x, tabY, '>', focusedSelectedStyle)
			} else {
				tape.cell(x, tabY, underscoreCh, s)
			}
			tape.cell(x+metrics.UnitsPerCellWidth, tabY, slashCh, tabBarStyle)
			lastSlashX = x + metrics.UnitsPerCellWidth // Track slash position - marks end of active tab's inside
			selTrailX = x + metrics.UnitsPerCellWidth
			lastTabWasSelected = true // Slash for selected tab - ellipsis goes after it
			tape.cell(x+metrics.UnitsPerCellWidth*2, tabY, ' ', tabBarOverlined)
			x += metrics.UnitsPerCellWidth * 3
		} else if nextIsSelected {
			// " \_" (3 chars) before selected tab
			// Space before \ gets overline (outside active tab)
			tape.cell(x, tabY, ' ', tabBarOverlined)
			tape.cell(x+metrics.UnitsPerCellWidth, tabY, backslashCh, tabBarStyle)
			lastSlashX = x + metrics.UnitsPerCellWidth // Track backslash position - marks start of next tab's inside
			selLeadX = x + metrics.UnitsPerCellWidth
			if hasFocus {
				selShapeStyle = focusedSelectedStyle
			} else {
				selShapeStyle = selectedStyle
			}
			lastTabWasSelected = false // Backslash for nextIsSelected - ellipsis goes before it
			if hasFocus {
				tape.cell(x+metrics.UnitsPerCellWidth*2, tabY, '<', focusedSelectedStyle)
			} else {
				tape.cell(x+metrics.UnitsPerCellWidth*2, tabY, underscoreCh, selectedStyle)
			}
			x += metrics.UnitsPerCellWidth * 3
		} else {
			// "  " (2 chars) regular separator - both get overline
			tape.cell(x, tabY, ' ', tabBarOverlined)
			tape.cell(x+metrics.UnitsPerCellWidth, tabY, ' ', tabBarOverlined)
			x += metrics.UnitsPerCellWidth * 2
		}
	}
	// Past the tabs, the marks are the strip's own again.
	tape.pastTheTabs()

	// Fill any gap between last tab and scroll buttons/edge
	// When ellipsis is needed, ensure we leave room for it by trimming fill spaces
	scrollAreaStart := bounds.Width - scrollButtonsWidth
	needsEllipsis := needsScrolling && !t.isLastTabFullyVisible()

	if needsEllipsis {
		// Calculate where ellipsis should ideally start
		ellipsisWidth := t.overflowEllipsisWidth()
		idealEllipsisX := scrollAreaStart - ellipsisWidth

		if tabWasTruncated {
			// The tab has already drawn its own ellipsis, so it has said
			// everything it can: close it there rather than running its colour
			// on to the scroll buttons, and the rest of the strip is strip.
			if selEndX >= 0 {
				selEndX = x
			}
			// And the strip adds no ellipsis of its own. One mark at this end
			// of the run, never two: the tab's dots already say the run is cut
			// short, and the [>] button says whether there is more to reach.
			for x < scrollAreaStart {
				tape.cell(x, tabY, ' ', tabBarOverlined)
				x += metrics.UnitsPerCellWidth
			}
		} else {
			// Text wasn't truncated - need to draw ellipsis
			// Handle slash/backslash based on which type of tab was last
			ellipsisX := idealEllipsisX
			useInternalStyle := false // Whether to use tab's internal style for ellipsis

			if lastSlashX >= 0 {
				if lastTabWasSelected {
					// Selected tab: slash at lastSlashX, ellipsis must come AFTER it
					// Minimum ellipsis position is right after the slash
					minEllipsisX := lastSlashX + metrics.UnitsPerCellWidth
					if idealEllipsisX < minEllipsisX {
						ellipsisX = minEllipsisX
					}
					// Check if at least 1 dot would fit after the slash
					if ellipsisX+metrics.UnitsPerCellWidth > scrollAreaStart {
						// No room for even 1 dot after slash - use interior ellipsis
						ellipsisX = lastTextEndX
						useInternalStyle = true
					}
				} else {
					// nextIsSelected tab: backslash at lastSlashX, ellipsis must be BEFORE it
					// If ellipsis would touch or go past the backslash, use interior ellipsis
					if idealEllipsisX >= lastSlashX {
						ellipsisX = lastTextEndX
						useInternalStyle = true
					}
				}
			}

			// Determine ellipsis style: use tab's internal style if no text was drawn
			// or if we're falling back to interior ellipsis position
			ellipsisStyle := tabBarOverlined
			if !drewAnyText || useInternalStyle {
				ellipsisStyle = lastTabStyle
			}

			// Fill gap between current position and ellipsis (only if ellipsis is after x)
			gapStart := x
			for x < ellipsisX {
				tape.cell(x, tabY, ' ', tabBarOverlined)
				x += metrics.UnitsPerCellWidth
			}

			// Draw as many dots as will fit before scroll buttons
			dotsDrawn := 0
			if p.Graphical() {
				if ellipsisX+ellipsisWidth <= scrollAreaStart {
					tape.fill(core.UnitRect{X: ellipsisX, Y: tabY, Width: ellipsisWidth, Height: metrics.UnitsPerCellHeight}, ' ', ellipsisStyle)
					tape.text(ellipsisX, tabY, "...", ellipsisStyle, font)
					dotsDrawn = 3
				}
			} else {
				for i := 0; i < 3; i++ {
					dotX := ellipsisX + core.Unit(i)*metrics.UnitsPerCellWidth
					if dotX+metrics.UnitsPerCellWidth <= scrollAreaStart {
						tape.cell(dotX, tabY, '.', ellipsisStyle)
						dotsDrawn++
					}
				}
			}

			// Fill remaining space after ellipsis to scroll buttons
			fillX := ellipsisX + core.Unit(dotsDrawn)*metrics.UnitsPerCellWidth
			if p.Graphical() && dotsDrawn > 0 {
				fillX = ellipsisX + ellipsisWidth
			}
			// The dots belong to the tab when they were pulled back over its
			// separator, and equally when they are all the tab managed to show
			// of itself. Either way the selected tab's silhouette has to reach
			// past them: gated on the first case alone, a tab clipped to no
			// characters closed at the lead-in it had drawn and left its own
			// ellipsis outside.
			if (useInternalStyle || zeroCharTab) && dotsDrawn > 0 {
				if selTrailX >= 0 && ellipsisX <= selTrailX {
					selTrailX = -1
					selEndX = fillX
				}
				if selLeadX >= 0 && ellipsisX <= selLeadX {
					selLeadX = -1
				}
				if selEndX >= 0 && fillX > selEndX {
					selEndX = fillX
				}
				// The shape now reaches past the dots, so the run between the
				// tab and them is INSIDE the tab and wears the tab's colour.
				// Left as strip it was a notch of bar cut out of the tab: the
				// run is filled before the dots are placed, when whose ground
				// it will turn out to be is not yet known. Laid as one rect
				// stopping exactly at the dots, so the whole-cell steps of
				// that fill cannot reach across them.
				if gapStart < ellipsisX {
					tape.fill(core.UnitRect{X: gapStart, Y: tabY, Width: ellipsisX - gapStart, Height: tabHeight}, ' ', ellipsisStyle)
				}
				// The dots belong to that tab's WORD, so when the word reads
				// the other way from the strip the two are tied and turn over
				// as one: the word keeps its dots on the end the word ends at,
				// not the end the strip does.
				if useInternalStyle && lastLabelMark >= 0 {
					if d := text.FirstStrongDirection(lastLabelText); d != core.DirInherit &&
						d != core.FindEffectiveDirection(t) {
						tape.tie(lastLabelMark)
					}
				}
			}
			for fillX < scrollAreaStart {
				tape.cell(fillX, tabY, ' ', tabBarOverlined)
				fillX += metrics.UnitsPerCellWidth
			}
		}
	} else {
		// No ellipsis needed - just fill to scroll area
		for x < scrollAreaStart {
			tape.cell(x, tabY, ' ', tabBarOverlined)
			x += metrics.UnitsPerCellWidth
		}
	}

	// Draw scroll buttons if needed (all with overline since outside active tab)
	if needsScrolling {
		buttonX := bounds.Width - scrollButtonsWidth
		disabledStyle := tabBarOverlined.WithFg(style.ColorBrightBlack)

		// [<] button - disabled when can't scroll left
		tape.owned(stripScrollLo)
		canLeft := t.canScrollLeft()
		if canLeft {
			leftStyle := tabBarOverlined
			if t.scrollButtonPressed == -1 && t.scrollLeftHovered {
				leftStyle = pressedStyle
			}
			tape.cell(buttonX, tabY, '[', leftStyle)
			tape.cell(buttonX+metrics.UnitsPerCellWidth, tabY, '<', leftStyle)
			tape.cell(buttonX+metrics.UnitsPerCellWidth*2, tabY, ']', leftStyle)
		} else {
			// Disabled: " < " (no brackets, grayed out)
			tape.cell(buttonX, tabY, ' ', disabledStyle)
			tape.cell(buttonX+metrics.UnitsPerCellWidth, tabY, '<', disabledStyle)
			tape.cell(buttonX+metrics.UnitsPerCellWidth*2, tabY, ' ', disabledStyle)
		}

		// [>] button - disabled when can't scroll right
		tape.owned(stripScrollHi)
		canRight := t.canScrollRight()
		if canRight {
			rightStyle := tabBarOverlined
			if t.scrollButtonPressed == 1 && t.scrollRightHovered {
				rightStyle = pressedStyle
			}
			tape.cell(buttonX+metrics.UnitsPerCellWidth*3, tabY, '[', rightStyle)
			tape.cell(buttonX+metrics.UnitsPerCellWidth*4, tabY, '>', rightStyle)
			tape.cell(buttonX+metrics.UnitsPerCellWidth*5, tabY, ']', rightStyle)
		} else {
			// Disabled: " > " (no brackets, grayed out)
			tape.cell(buttonX+metrics.UnitsPerCellWidth*3, tabY, ' ', disabledStyle)
			tape.cell(buttonX+metrics.UnitsPerCellWidth*4, tabY, '>', disabledStyle)
			tape.cell(buttonX+metrics.UnitsPerCellWidth*5, tabY, ' ', disabledStyle)
		}
	}

	// What the run made is also what the mouse will read.
	t.stripSpans = tape.spans()

	// Everything above worked out WHERE, in the strip's own run. Here it
	// becomes places on the screen -- before the silhouette, which paints
	// over the finished strip.
	tape.replay(p, bounds.Width, core.ChromeMirrored(t))

	// Selected-tab silhouette and the strip's continuous edge line,
	// drawn over the finished cell material.
	if p.Graphical() {
		t.paintTabShape(p, tabY, bounds.Width, selLeadX, selTrailX, selEndX, selShapeStyle, tabBarStyle, false, core.ChromeMirrored(t))
	}

	// Draw separator row if enabled (in active tab color, above the tab bar)
	if t.showSeparator {
		separatorStyle := scheme.GetActiveTab()
		separatorY := tabY - metrics.UnitsPerCellHeight
		p.FillRect(core.UnitRect{
			X:      0,
			Y:      separatorY,
			Width:  bounds.Width,
			Height: metrics.UnitsPerCellHeight,
		}, ' ', separatorStyle)
	}
}

// sideTabTextX is where a side strip's label goes within its slot: against the
// edge its OWN script reads from, so a Hebrew name sits at the right of the
// slot and an English one at the left. Which edge the strip itself stands on
// does not come into it -- a column of tabs does not turn over, and what a
// label is doing is READING.
//
// slotX/slotW are the tab's own box; a column of padding is kept on each side.
func (t *TabTrinket) sideTabTextX(slotX, slotW core.Unit, label string) core.Unit {
	cw := t.EffectiveCellMetrics().UnitsPerCellWidth
	x0, room := slotX+cw, slotW-cw*2
	side := core.ResolveHAlign(core.AlignTextNatural,
		text.FirstStrongDirection(label), core.FindEffectiveDirection(t))
	if side == core.SideRight {
		if w := t.MeasureText(label); w < room {
			return x0 + room - w
		}
	}
	return x0
}

func (t *TabTrinket) paintLeftTabs(p *core.Painter, bounds core.UnitRect, scheme *style.Scheme, metrics core.CellMetrics) {
	tabWidth := t.calculateTabBarWidth()
	hasFocus := t.HasFocus()
	needsScrolling := t.vertTabsNeedScrolling()
	visibleCount := t.vertVisibleCount()
	font := t.EffectiveFont()

	// Tab bar style from scheme
	tabBarStyle := scheme.GetTabsBar(true)
	// Selected tab styles from scheme
	selectedStyle := scheme.GetActiveTab()
	focusedSelectedStyle := scheme.GetFocusedTab()
	// Disabled style
	disabledStyle := tabBarStyle.WithFg(scheme.GetDisabledTextFG())

	// Scrollbar overlays the left padding column, no extra offset needed
	contentX := core.Unit(0)

	// Draw tab bar background
	p.FillRect(core.UnitRect{X: contentX, Width: tabWidth, Height: bounds.Height}, ' ', tabBarStyle)

	// Draw visible tabs vertically
	y := core.Unit(0)
	endIndex := t.vertScrollOffset + visibleCount
	if endIndex > len(t.tabs) {
		endIndex = len(t.tabs)
	}

	for i := t.vertScrollOffset; i < endIndex; i++ {
		tab := t.tabs[i]
		var s style.CellStyle
		if !tab.Enabled {
			s = disabledStyle
		} else if i == t.currentIndex {
			if hasFocus {
				s = focusedSelectedStyle
			} else {
				s = selectedStyle
			}
		} else {
			s = tabBarStyle
		}

		// Draw tab background
		p.FillRect(core.UnitRect{X: contentX, Y: y, Width: tabWidth, Height: metrics.UnitsPerCellHeight}, ' ', s)

		// Draw tab text using font-aware rendering
		maxTextWidth := tabWidth - metrics.UnitsPerCellWidth*2 // Leave padding on both sides

		// Truncate text if it doesn't fit
		displayText := tab.Text
		if t.MeasureText(displayText) > maxTextWidth {
			// Find how many characters fit
			textRunes := []rune(tab.Text)
			currentWidth := core.Unit(0)
			for j, ch := range textRunes {
				charWidth := t.MeasureText(string(ch))
				if currentWidth+charWidth > maxTextWidth {
					displayText = string(textRunes[:j])
					break
				}
				currentWidth += charWidth
			}
		}
		// Cut to fit first, prepared after: the run that is drawn is the run
		// the slot places.
		displayRun := t.CellRun(displayText)
		p.DrawText(t.sideTabTextX(contentX, tabWidth, displayRun), y, displayRun, s, font)

		y += metrics.UnitsPerCellHeight
	}

	// Draw scrollbar if needed (on left edge - outside)
	if needsScrolling {
		t.paintVertScrollbar(p, 0)
	}

	// Draw separator line if enabled
	if t.showSeparator {
		separatorX := contentX + tabWidth
		for i := core.Unit(0); i < bounds.Height; i += metrics.UnitsPerCellHeight {
			p.DrawCell(separatorX, i, '│', scheme.GetNormal(true))
		}
	}
}

func (t *TabTrinket) paintRightTabs(p *core.Painter, bounds core.UnitRect, scheme *style.Scheme, metrics core.CellMetrics) {
	tabWidth := t.calculateTabBarWidth()
	hasFocus := t.HasFocus()
	needsScrolling := t.vertTabsNeedScrolling()
	visibleCount := t.vertVisibleCount()
	font := t.EffectiveFont()

	// Tab bar style from scheme
	tabBarStyle := scheme.GetTabsBar(true)
	// Selected tab styles from scheme
	selectedStyle := scheme.GetActiveTab()
	focusedSelectedStyle := scheme.GetFocusedTab()
	// Disabled style
	disabledStyle := tabBarStyle.WithFg(scheme.GetDisabledTextFG())

	// Scrollbar overlays the right padding column, no shift needed
	tabX := bounds.Width - tabWidth
	scrollbarX := bounds.Width - metrics.UnitsPerCellWidth

	// Draw tab bar background
	p.FillRect(core.UnitRect{X: tabX, Width: tabWidth, Height: bounds.Height}, ' ', tabBarStyle)

	// Draw visible tabs vertically
	y := core.Unit(0)
	endIndex := t.vertScrollOffset + visibleCount
	if endIndex > len(t.tabs) {
		endIndex = len(t.tabs)
	}

	for i := t.vertScrollOffset; i < endIndex; i++ {
		tab := t.tabs[i]
		var s style.CellStyle
		if !tab.Enabled {
			s = disabledStyle
		} else if i == t.currentIndex {
			if hasFocus {
				s = focusedSelectedStyle
			} else {
				s = selectedStyle
			}
		} else {
			s = tabBarStyle
		}

		// Draw tab background
		p.FillRect(core.UnitRect{X: tabX, Y: y, Width: tabWidth, Height: metrics.UnitsPerCellHeight}, ' ', s)

		// Draw tab text using font-aware rendering
		maxTextWidth := tabWidth - metrics.UnitsPerCellWidth*2 // Leave padding on both sides

		// Truncate text if it doesn't fit
		displayText := tab.Text
		if t.MeasureText(displayText) > maxTextWidth {
			// Find how many characters fit
			textRunes := []rune(tab.Text)
			currentWidth := core.Unit(0)
			for j, ch := range textRunes {
				charWidth := t.MeasureText(string(ch))
				if currentWidth+charWidth > maxTextWidth {
					displayText = string(textRunes[:j])
					break
				}
				currentWidth += charWidth
			}
		}
		displayRun := t.CellRun(displayText)
		p.DrawText(t.sideTabTextX(tabX, tabWidth, displayRun), y, displayRun, s, font)

		y += metrics.UnitsPerCellHeight
	}

	// Draw scrollbar if needed (on right edge - outside)
	if needsScrolling {
		t.paintVertScrollbar(p, scrollbarX)
	}

	// Draw separator line if enabled (on left edge of tab bar)
	if t.showSeparator {
		separatorX := tabX - metrics.UnitsPerCellWidth
		for i := core.Unit(0); i < bounds.Height; i += metrics.UnitsPerCellHeight {
			p.DrawCell(separatorX, i, '│', scheme.GetNormal(true))
		}
	}
}

func (t *TabTrinket) paintContent(p *core.Painter) {
	contentBounds := t.contentBounds()

	// Fill content area with TabTrinket's background color if set
	// Use ColorDefault (ANSI 49) when no explicit background is set
	if bg := t.BackgroundColor(); bg != nil {
		contentStyle := style.DefaultStyle().WithBg(*bg)
		p.FillRect(core.UnitRect{
			X:      contentBounds.X,
			Y:      contentBounds.Y,
			Width:  contentBounds.Width,
			Height: contentBounds.Height,
		}, ' ', contentStyle)
	} else {
		// Use terminal default background (ANSI 49)
		contentStyle := style.DefaultStyle()
		p.FillRect(core.UnitRect{
			X:      contentBounds.X,
			Y:      contentBounds.Y,
			Width:  contentBounds.Width,
			Height: contentBounds.Height,
		}, ' ', contentStyle)
	}

	if t.currentIndex < 0 || t.currentIndex >= len(t.tabs) {
		return
	}

	content := t.tabs[t.currentIndex].Content
	if content == nil {
		return
	}

	// Set content bounds with actual position so MapToScreen works correctly
	content.SetBounds(core.UnitRect{
		X:      contentBounds.X,
		Y:      contentBounds.Y,
		Width:  contentBounds.Width,
		Height: contentBounds.Height,
	})

	// Create clipped painter for content at the content area position
	contentPainter := p.WithOffset(contentBounds.X, contentBounds.Y).
		WithClip(core.UnitRect{Width: contentBounds.Width, Height: contentBounds.Height})
	content.Paint(contentPainter)
}

// HandleKeyPress handles keyboard input.
func (t *TabTrinket) HandleKeyPress(event core.KeyPressEvent) bool {
	// Resolved ONCE: this handler asks twice (once for the tab bar, once for
	// the MDI cycle after the content has had its turn) and feeding the same
	// keystroke to the sequence processor twice would advance a chord's
	// prefix by two.
	cmd := t.KeyCommand(event.Key)

	// When TabTrinket has focus, handle tab bar navigation. Only the axis the
	// strip actually runs along answers; the other one falls through, so a
	// vertical strip never swallows a horizontal arrow.
	if t.HasFocus() {
		// Which axis the strip runs along, once the direction has settled
		// which edge it stands on.
		isVertical := t.onSide()

		switch cmd {
		case core.CmdTrinketItemLeft:
			if !isVertical {
				t.prevTabAndEnsureVisible()
				return true
			}
		case core.CmdTrinketItemRight:
			if !isVertical {
				t.nextTabAndEnsureVisible()
				return true
			}
		case core.CmdTrinketItemUp:
			if isVertical {
				t.prevTabAndEnsureVisible()
				return true
			}
		case core.CmdTrinketItemDown:
			if isVertical {
				t.nextTabAndEnsureVisible()
				return true
			}
		case core.CmdTrinketItemPrior:
			t.prevTabAndEnsureVisible()
			return true
		case core.CmdTrinketItemNext:
			t.nextTabAndEnsureVisible()
			return true
		case core.CmdTrinketBeg:
			t.firstTab()
			return true
		case core.CmdTrinketEnd:
			t.lastTab()
			return true
		}
	}

	// Pass to current content
	if t.currentIndex >= 0 && t.currentIndex < len(t.tabs) {
		content := t.tabs[t.currentIndex].Content
		if content != nil && content.HandleKeyPress(event) {
			return true
		}
	}

	switch cmd {
	case core.CmdWindowMDINext:
		t.nextTab()
		return true

	case core.CmdWindowMDIPrior:
		t.prevTab()
		return true
	}

	return false
}

func (t *TabTrinket) nextTab() {
	if len(t.tabs) == 0 {
		return
	}

	for i := 1; i <= len(t.tabs); i++ {
		idx := (t.currentIndex + i) % len(t.tabs)
		if t.tabs[idx].Enabled {
			t.SetCurrentIndex(idx)
			return
		}
	}
}

func (t *TabTrinket) prevTab() {
	if len(t.tabs) == 0 {
		return
	}

	for i := 1; i <= len(t.tabs); i++ {
		idx := (t.currentIndex - i + len(t.tabs)) % len(t.tabs)
		if t.tabs[idx].Enabled {
			t.SetCurrentIndex(idx)
			return
		}
	}
}

// nextTabAndEnsureVisible moves to next tab and ensures it's fully visible.
func (t *TabTrinket) nextTabAndEnsureVisible() {
	if len(t.tabs) == 0 {
		return
	}

	for i := 1; i <= len(t.tabs); i++ {
		idx := (t.currentIndex + i) % len(t.tabs)
		if t.tabs[idx].Enabled {
			t.SetCurrentIndex(idx)
			// Use appropriate ensure visible based on tab position
			if t.onSide() {
				t.vertEnsureVisible(idx)
			} else {
				t.ensureTabFullyVisible(idx)
			}
			return
		}
	}
}

// prevTabAndEnsureVisible moves to previous tab and ensures it's fully visible.
func (t *TabTrinket) prevTabAndEnsureVisible() {
	if len(t.tabs) == 0 {
		return
	}

	for i := 1; i <= len(t.tabs); i++ {
		idx := (t.currentIndex - i + len(t.tabs)) % len(t.tabs)
		if t.tabs[idx].Enabled {
			t.SetCurrentIndex(idx)
			// Use appropriate ensure visible based on tab position
			if t.onSide() {
				t.vertEnsureVisible(idx)
			} else {
				t.ensureTabFullyVisible(idx)
			}
			return
		}
	}
}

// firstTab jumps to the first enabled tab.
func (t *TabTrinket) firstTab() {
	for i := 0; i < len(t.tabs); i++ {
		if t.tabs[i].Enabled {
			t.SetCurrentIndex(i)
			if t.onSide() {
				t.vertEnsureVisible(i)
			} else {
				t.ensureTabFullyVisible(i)
			}
			return
		}
	}
}

// lastTab jumps to the last enabled tab.
func (t *TabTrinket) lastTab() {
	for i := len(t.tabs) - 1; i >= 0; i-- {
		if t.tabs[i].Enabled {
			t.SetCurrentIndex(i)
			if t.onSide() {
				t.vertEnsureVisible(i)
			} else {
				t.ensureTabFullyVisible(i)
			}
			return
		}
	}
}

// HandleMousePress handles mouse clicks.
func (t *TabTrinket) HandleMousePress(event core.MousePressEvent) bool {
	if event.Button != core.LeftButton {
		// Tab-bar chrome only reacts to the left button, but other
		// buttons still belong to the content (context menus).
		if t.currentIndex >= 0 && t.currentIndex < len(t.tabs) {
			if content := t.tabs[t.currentIndex].Content; content != nil {
				cb := t.contentBounds()
				if cb.Contains(core.UnitPoint{X: event.X, Y: event.Y}) {
					e := event
					e.X -= cb.X
					e.Y -= cb.Y
					return content.HandleMousePress(e)
				}
			}
		}
		return false
	}

	// Note: We intentionally don't call SetFocus() here.
	// TabTrinket focus is for keyboard tab order navigation only,
	// not for mouse interaction.

	metrics := t.EffectiveCellMetrics()
	tabHeight := t.tabBarHeight()

	// Check if click is in tab bar
	switch t.tabEdge() {
	case TabEdgeTop:
		if event.Y < tabHeight {
			t.handleTabBarPress(event.X)
			return true
		}
	case TabEdgeBottom:
		bounds := t.Bounds()
		if event.Y >= bounds.Height-tabHeight {
			t.handleTabBarPress(event.X)
			return true
		}
	case TabEdgeLeft:
		tabWidth := t.calculateTabBarWidth()
		needsScrolling := t.vertTabsNeedScrolling()

		// Check if click is on scrollbar (left padding column, overlaid by scrollbar)
		if needsScrolling && event.X < metrics.UnitsPerCellWidth {
			t.handleVertScrollbarClick(event.Y, metrics)
			return true
		}

		// Check if click is on tab area (scrollbar reuses padding, no extra width)
		if event.X < tabWidth {
			row := int(event.Y / metrics.UnitsPerCellHeight)
			idx := t.vertScrollOffset + row
			if idx >= 0 && idx < len(t.tabs) && t.tabs[idx].Enabled {
				t.SetCurrentIndex(idx)
				t.vertEnsureVisible(idx)
				t.vertTabDragging = true // Start sweep drag
			}
			return true
		}
	case TabEdgeRight:
		bounds := t.Bounds()
		tabWidth := t.calculateTabBarWidth()
		needsScrolling := t.vertTabsNeedScrolling()

		// Check if click is on scrollbar (right padding column, overlaid by scrollbar)
		if needsScrolling && event.X >= bounds.Width-metrics.UnitsPerCellWidth {
			t.handleVertScrollbarClick(event.Y, metrics)
			return true
		}

		// Check if click is on tab area (scrollbar reuses padding, no extra width)
		tabX := bounds.Width - tabWidth
		if event.X >= tabX {
			row := int(event.Y / metrics.UnitsPerCellHeight)
			idx := t.vertScrollOffset + row
			if idx >= 0 && idx < len(t.tabs) && t.tabs[idx].Enabled {
				t.SetCurrentIndex(idx)
				t.vertEnsureVisible(idx)
				t.vertTabDragging = true // Start sweep drag
			}
			return true
		}
	}

	// Pass to content
	if t.currentIndex >= 0 && t.currentIndex < len(t.tabs) {
		content := t.tabs[t.currentIndex].Content
		if content != nil {
			contentBounds := t.contentBounds()
			localEvent := event
			localEvent.X -= contentBounds.X
			localEvent.Y -= contentBounds.Y
			return content.HandleMousePress(localEvent)
		}
	}

	return true
}

// stripPartAt is the part of the strip a press in RUN coordinates landed on,
// and whether it landed on one at all: the bar's own background belongs to
// nothing, and neither does the slack an alignment left in front of the run.
//
// The parts are searched from the end of the run backwards, because that is
// the order they were painted in: a proportional label leaves the pen off the
// cell grid, so a tab's last cell can hang a few units into the scroll buttons
// beside it, and what the eye finds there is the button drawn over the top.
func (t *TabTrinket) stripPartAt(x core.Unit) (stripSpan, bool) {
	for i := len(t.stripSpans) - 1; i >= 0; i-- {
		if sp := t.stripSpans[i]; x >= sp.x && x < sp.x+sp.w {
			return sp, true
		}
	}
	return stripSpan{}, false
}

func (t *TabTrinket) handleTabBarPress(x core.Unit) {
	cw := t.EffectiveCellMetrics().UnitsPerCellWidth
	bounds := t.Bounds()

	// A press is turned back into the strip's RUN coordinates once, here,
	// because that is what the strip recorded itself in.
	//
	// A press is a POINT and a part of the strip is a span, so the reflection
	// takes the unit the press landed on rather than the boundary in front of
	// it: the last unit of a span reflects to the first unit of its mirror
	// image, and a press on the far edge of a tab stays inside that tab.
	if core.ChromeMirrored(t) {
		x = bounds.Width - x - 1
	}

	sp, ok := t.stripPartAt(x)
	if !ok {
		return
	}
	switch sp.owner {
	case stripOverflow:
		// The mark stands for the tabs that fell off the front of the run, so
		// pressing it brings the nearest of them back and selects it.
		t.SetCurrentIndex(t.tabScrollOffset - 1)
		t.tabScrollOffset--
		t.Update()
		return
	case stripScrollLo:
		if t.canScrollLeft() {
			t.scrollButtonPressed = -1
			t.scrollLeftHovered = true
			t.Update()
		}
		return
	case stripScrollHi:
		if t.canScrollRight() {
			t.scrollButtonPressed = 1
			t.scrollRightHovered = true
			t.Update()
		}
		return
	}
	if sp.owner < 0 || sp.owner >= len(t.tabs) {
		return
	}
	i := sp.owner
	tab := t.tabs[i]

	// A tab the run was cut short at is only part drawn, and everything from
	// where it starts to where the strip gives out is its own: a press
	// anywhere in that means bring this tab into view.
	if sp.clipped {
		if tab.Enabled {
			t.SetCurrentIndex(i)
			t.ensureTabFullyVisible(i)
		}
		return
	}

	// The close button sits on the last cell of the label.
	if (t.closable || tab.Closable) && x >= sp.labelEnd-cw && x < sp.labelEnd {
		if t.onTabCloseRequested != nil {
			t.onTabCloseRequested(i)
		}
		return
	}

	// Past the label is the separator, which leads into the next tab: which of
	// the two the press names is settled by where in it the press fell.
	if x >= sp.labelEnd && i < len(t.tabs)-1 {
		sepWidth := sp.x + sp.w - sp.labelEnd
		into := x - sp.labelEnd
		next := t.tabs[i+1]
		if sepWidth == 3*cw {
			// Three cells, so the slash between the two tabs has a cell of its
			// own: it goes to whichever of them is already selected, and stays
			// where it is when neither is.
			third := sepWidth / 3
			switch {
			case into < third:
				if tab.Enabled {
					t.SetCurrentIndex(i)
				}
			case into < third*2:
				if i+1 == t.currentIndex {
					if next.Enabled {
						t.SetCurrentIndex(i + 1)
					}
				} else if tab.Enabled {
					t.SetCurrentIndex(i)
				}
			default:
				if next.Enabled {
					t.SetCurrentIndex(i + 1)
				}
			}
			return
		}
		// An even separator divides in half, each tab taking the side of it
		// that it stands on.
		if into < sepWidth/2 {
			if tab.Enabled {
				t.SetCurrentIndex(i)
			}
		} else if next.Enabled {
			t.SetCurrentIndex(i + 1)
		}
		return
	}

	if tab.Enabled {
		t.SetCurrentIndex(i)
	}
}

// ensureTabFullyVisible scrolls to make the given tab fully visible.
func (t *TabTrinket) ensureTabFullyVisible(index int) {
	if index < 0 || index >= len(t.tabs) {
		return
	}

	// If tab is before visible area, scroll left to show it
	if index < t.tabScrollOffset {
		t.tabScrollOffset = index
		t.Update()
		return
	}

	// Check if tab is fully visible
	bounds := t.Bounds()
	metrics := t.EffectiveCellMetrics()

	scrollButtonsWidth := core.Unit(0)
	if t.tabsNeedScrolling() {
		scrollButtonsWidth = metrics.TextWidth(6)
	}

	// Tab format varies by position
	isBottomTabs := t.tabEdge() == TabEdgeBottom

	// Try scrolling right until the tab is fully visible.
	// Use <= to also verify fit when current tab becomes the first visible tab,
	// since the left ellipsis ("...") will appear and take up space.
	for t.tabScrollOffset <= index {
		leftEllipseWidth := core.Unit(0)
		if t.tabScrollOffset > 0 {
			leftEllipseWidth = t.overflowEllipsisWidth()
		}
		// Available width is the absolute position where tabs must stop
		availableWidth := bounds.Width - scrollButtonsWidth

		// Calculate if tab at index fits
		x := leftEllipseWidth
		fits := true
		for i := t.tabScrollOffset; i <= index; i++ {
			tab := t.tabs[i]
			isFirstVisible := i == t.tabScrollOffset
			isSelected := i == t.currentIndex
			isLastVisible := i == len(t.tabs)-1
			nextIsSelected := !isLastVisible && i+1 == t.currentIndex

			// When the overflow mark is showing, the prefix drops its leading space
			hasLeftEllipsis := t.tabScrollOffset > 0
			prefixWidth := 0
			if isFirstVisible {
				if isSelected {
					prefixWidth = 3 // " /<" on a top strip, " \_" on a bottom one
					if hasLeftEllipsis {
						prefixWidth = 2 // tighter beside the mark: "/<" or "\_"
					}
				} else {
					prefixWidth = 2 // "  "
					if hasLeftEllipsis {
						prefixWidth = 1 // " "
					}
				}
			}
			sepWidth := 2 // "  "
			if isSelected || nextIsSelected {
				sepWidth = 3 // the join beside the selected tab
			}
			// Prefix and separator are decorative (cell-based), text is font-based
			tabSlotWidth := core.Unit(prefixWidth+sepWidth)*metrics.UnitsPerCellWidth + t.MeasureText(tab.Text)
			x += tabSlotWidth

			if i == index && x > availableWidth {
				// For top tabs, check grace margin - if only non-essential trailing
				// content (underscore + space) would be cut off, consider it as fitting
				isLastTab := i == len(t.tabs)-1
				if !isBottomTabs && isLastTab {
					essentialSepWidth := 0
					if isSelected {
						essentialSepWidth = 2 // space/bracket + backslash are essential
					}
					essentialWidth := core.Unit(prefixWidth+essentialSepWidth)*metrics.UnitsPerCellWidth + t.MeasureText(tab.Text)
					essentialX := x - tabSlotWidth + essentialWidth
					if essentialX <= availableWidth {
						// Essential content fits - consider it as fitting
						break
					}
				}
				fits = false
				break
			}
		}

		if fits {
			break
		}
		// Can't scroll further when current tab is already the first visible
		if t.tabScrollOffset >= index {
			break
		}
		t.tabScrollOffset++
	}
	t.Update()
}

// HandleFocusIn is called when focus is gained.
func (t *TabTrinket) HandleFocusIn() {
	t.Update()
}

// HandleFocusOut is called when focus is lost.
func (t *TabTrinket) HandleFocusOut() {
	t.vertTabDragging = false
	t.scrollbarDragging = false
	t.Update()
}

// overVertScrollbarThumb reports whether a widget-local point lies on the
// vertical tab scrollbar thumb.
func (t *TabTrinket) overVertScrollbarThumb(x, y core.Unit) bool {
	if !t.onSide() {
		return false
	}
	if len(t.tabs) <= t.vertVisibleCount() {
		return false
	}
	metrics := t.EffectiveCellMetrics()
	scrollbarX, thumbStart, thumbHeight, _ := t.vertScrollbarGeometry()
	if x < scrollbarX || x >= scrollbarX+metrics.UnitsPerCellWidth {
		return false
	}
	bounds := t.Bounds()
	if y < 0 || y >= bounds.Height {
		return false
	}
	if core.FindSmoothPositioning(t.Self()) {
		_, thumbU, posU := t.vertScrollbarUnits()
		pos := float64(y)
		return pos >= posU && pos < posU+thumbU
	}
	row := int(y / metrics.UnitsPerCellHeight)
	return row >= thumbStart && row < thumbStart+thumbHeight
}

// HandleMouseMove handles mouse movement.
func (t *TabTrinket) HandleMouseMove(event core.MouseMoveEvent) bool {
	// Track scrollbar-thumb hover. Hover is a no-button affordance: while a
	// button is held (a drag begun elsewhere passing over) don't light the
	// thumb - unless this tab strip owns the scrollbar drag.
	if over := t.scrollbarDragging || (event.Buttons == 0 && t.overVertScrollbarThumb(event.X, event.Y)); over != t.scrollbarThumbHovered {
		t.scrollbarThumbHovered = over
		t.Update()
	}

	// Handle vertical scrollbar thumb drag
	if t.scrollbarDragging {
		// Smooth drag: the thumb follows the pointer in units, the
		// first visible tab snaps to the nearest whole item.
		if t.smoothVertSbDrag {
			visibleCount := t.vertVisibleCount()
			trackU, thumbU, _ := t.vertScrollbarUnits()
			scrollable := trackU - thumbU
			pos := float64(event.Y) - t.vertSbGrabOff
			if pos < 0 {
				pos = 0
			}
			if pos > scrollable {
				pos = scrollable
			}
			t.vertSbThumbPos = pos
			maxScroll := len(t.tabs) - visibleCount
			newOffset := 0
			if scrollable > 0 && maxScroll > 0 {
				newOffset = int(pos*float64(maxScroll)/scrollable + 0.5)
			}
			t.vertScrollOffset = newOffset
			// The thumb moves even when the snapped offset does not.
			t.Update()
			return true
		}

		metrics := t.EffectiveCellMetrics()
		currentRow := int(event.Y / metrics.UnitsPerCellHeight)
		rowDelta := currentRow - t.scrollbarDragStart

		visibleCount := t.vertVisibleCount()
		totalTabs := len(t.tabs)
		maxScroll := totalTabs - visibleCount

		if maxScroll > 0 {
			_, _, thumbHeight, trackHeight := t.vertScrollbarGeometry()
			scrollableTrack := trackHeight - thumbHeight

			if scrollableTrack > 0 {
				// Calculate how many items to scroll based on thumb movement
				scrollDelta := rowDelta * maxScroll / scrollableTrack
				newOffset := t.scrollbarDragOffset + scrollDelta

				// Clamp to valid range
				if newOffset < 0 {
					newOffset = 0
				}
				if newOffset > maxScroll {
					newOffset = maxScroll
				}

				if newOffset != t.vertScrollOffset {
					t.vertScrollOffset = newOffset
					t.Update()
				}
			}
		}
		return true
	}

	// Handle vertical tab sweep drag (select tabs as mouse moves over them)
	if t.vertTabDragging {
		metrics := t.EffectiveCellMetrics()
		bounds := t.Bounds()
		tabWidth := t.calculateTabBarWidth()

		// Determine if mouse is in the tab area
		inTabArea := false
		switch t.tabEdge() {
		case TabEdgeLeft:
			inTabArea = event.X < tabWidth
		case TabEdgeRight:
			tabX := bounds.Width - tabWidth
			inTabArea = event.X >= tabX
		}

		if inTabArea {
			row := int(event.Y / metrics.UnitsPerCellHeight)
			idx := t.vertScrollOffset + row
			if idx >= 0 && idx < len(t.tabs) && t.tabs[idx].Enabled {
				if idx != t.currentIndex {
					t.SetCurrentIndex(idx)
					t.vertEnsureVisible(idx)
				}
			}
		}
		return true
	}

	// If tracking scroll button press, update hover state
	if t.scrollButtonPressed != 0 {
		metrics := t.EffectiveCellMetrics()
		bounds := t.Bounds()
		scrollButtonsWidth := metrics.TextWidth(6)
		buttonX := bounds.Width - scrollButtonsWidth
		tabHeight := t.tabBarHeight()

		// Must be in tab bar
		inTabBar := event.Y >= 0 && event.Y < tabHeight

		if t.scrollButtonPressed == -1 {
			// Tracking [<] button
			newHovered := inTabBar && event.X >= buttonX && event.X < buttonX+metrics.TextWidth(3)
			if newHovered != t.scrollLeftHovered {
				t.scrollLeftHovered = newHovered
				t.Update()
			}
		} else if t.scrollButtonPressed == 1 {
			// Tracking [>] button
			newHovered := inTabBar && event.X >= buttonX+metrics.TextWidth(3) && event.X < buttonX+scrollButtonsWidth
			if newHovered != t.scrollRightHovered {
				t.scrollRightHovered = newHovered
				t.Update()
			}
		}
		return true
	}

	// Forward to current content
	if t.currentIndex >= 0 && t.currentIndex < len(t.tabs) {
		content := t.tabs[t.currentIndex].Content
		if content != nil {
			if handler, ok := content.(interface {
				HandleMouseMove(core.MouseMoveEvent) bool
			}); ok {
				contentBounds := t.contentBounds()
				localEvent := event
				localEvent.X -= contentBounds.X
				localEvent.Y -= contentBounds.Y
				return handler.HandleMouseMove(localEvent)
			}
		}
	}
	return false
}

// HandleMouseWheel forwards a wheel event to the current tab's
// content when the pointer is over it.
func (t *TabTrinket) HandleMouseWheel(event core.MouseWheelEvent) bool {
	if t.currentIndex < 0 || t.currentIndex >= len(t.tabs) {
		return false
	}
	content := t.tabs[t.currentIndex].Content
	if content == nil {
		return false
	}
	handler, ok := content.(interface {
		HandleMouseWheel(core.MouseWheelEvent) bool
	})
	if !ok {
		return false
	}
	contentBounds := t.contentBounds()
	if !contentBounds.Contains(core.UnitPoint{X: event.X, Y: event.Y}) {
		// Over the tab bar: wheel steps the first visible tab when
		// the strip overflows ([<] [>] visible). Horizontal strips
		// take either axis (two-finger pans are often diagonal-ish);
		// vertical lists take the vertical axis.
		step := event.DeltaY
		if event.DeltaX != 0 {
			step = event.DeltaX
		} else if event.PreciseX != 0 || event.PreciseY != 0 {
			// Whole-tab steps from precise deltas: sign only.
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
		vertical := t.onSide()
		if vertical {
			visible := t.vertVisibleCount()
			maxOffset := len(t.tabs) - visible
			if maxOffset <= 0 {
				return false
			}
			off := t.vertScrollOffset
			if step < 0 {
				off--
			} else {
				off++
			}
			if off < 0 {
				off = 0
			}
			if off > maxOffset {
				off = maxOffset
			}
			if off == t.vertScrollOffset {
				return true // consumed: strip is scrollable
			}
			t.vertScrollOffset = off
			t.Update()
			return true
		}
		if !t.canScrollLeft() && !t.canScrollRight() {
			return false
		}
		if step < 0 && t.canScrollLeft() {
			t.tabScrollOffset--
		} else if step > 0 && t.canScrollRight() {
			t.tabScrollOffset++
		}
		t.Update()
		return true
	}
	localEvent := event
	localEvent.X -= contentBounds.X
	localEvent.Y -= contentBounds.Y
	return handler.HandleMouseWheel(localEvent)
}

// HandleMouseRelease handles mouse button release.
func (t *TabTrinket) HandleMouseRelease(event core.MouseReleaseEvent) bool {
	// Clear vertical scrollbar drag state
	if t.scrollbarDragging {
		t.scrollbarDragging = false
		t.smoothVertSbDrag = false
		t.Update()
		return true
	}

	// Clear vertical tab sweep drag state
	if t.vertTabDragging {
		t.vertTabDragging = false
		return true
	}

	// If tracking scroll button press, handle release
	if t.scrollButtonPressed != 0 {
		pressedButton := t.scrollButtonPressed
		wasLeftHovered := t.scrollLeftHovered
		wasRightHovered := t.scrollRightHovered

		// Clear press state
		t.scrollButtonPressed = 0
		t.scrollLeftHovered = false
		t.scrollRightHovered = false
		t.Update()

		// Only trigger action if still hovering
		if pressedButton == -1 && wasLeftHovered {
			// Scroll left
			if t.tabScrollOffset > 0 {
				t.tabScrollOffset--
				t.Update()
			}
		} else if pressedButton == 1 && wasRightHovered {
			// Scroll right
			if t.tabScrollOffset < len(t.tabs)-1 {
				t.tabScrollOffset++
				t.Update()
			}
		}
		return true
	}

	// Forward to current content
	if t.currentIndex >= 0 && t.currentIndex < len(t.tabs) {
		content := t.tabs[t.currentIndex].Content
		if content != nil {
			if handler, ok := content.(interface {
				HandleMouseRelease(core.MouseReleaseEvent) bool
			}); ok {
				contentBounds := t.contentBounds()
				localEvent := event
				localEvent.X -= contentBounds.X
				localEvent.Y -= contentBounds.Y
				return handler.HandleMouseRelease(localEvent)
			}
		}
	}
	return false
}

// HandleResize is called when the tab trinket is resized.
func (t *TabTrinket) HandleResize(oldSize, newSize core.UnitSize) {
	// Adjust scroll offset based on new size and tab position
	isVertical := t.onSide()
	if isVertical {
		t.adjustVertScrollOffsetForResize(oldSize.Height > newSize.Height)
	} else {
		t.adjustScrollOffsetForResize(oldSize.Width > newSize.Width)
	}

	// Update content bounds for the current tab
	if t.currentIndex >= 0 && t.currentIndex < len(t.tabs) {
		content := t.tabs[t.currentIndex].Content
		if content != nil {
			contentBounds := t.contentBounds()
			// Set bounds with actual position so MapToScreen works correctly
			content.SetBounds(core.UnitRect{
				X:      contentBounds.X,
				Y:      contentBounds.Y,
				Width:  contentBounds.Width,
				Height: contentBounds.Height,
			})
		}
	}
}

// adjustScrollOffsetForResize adjusts the tab scroll offset when the trinket is resized.
// When widening: scroll left if possible to avoid blank space on the right.
// When narrowing: ensure the current tab stays visible.
func (t *TabTrinket) adjustScrollOffsetForResize(isNarrowing bool) {
	if len(t.tabs) == 0 {
		t.tabScrollOffset = 0
		return
	}

	// If scrolling isn't needed at all, reset to 0
	if !t.tabsNeedScrolling() {
		t.tabScrollOffset = 0
		return
	}

	if isNarrowing {
		// When narrowing, ensure current tab is still visible
		// If it's now off the right edge, we need to scroll right (increase offset)
		// But we should try to keep it in view
		t.ensureTabFullyVisible(t.currentIndex)
	} else {
		// When widening, scroll left as much as possible to fill available space
		// Keep reducing offset while the last tab would still be visible
		for t.tabScrollOffset > 0 {
			// Try scrolling left by one
			t.tabScrollOffset--
			// Check if last tab is now fully visible
			if t.isLastTabFullyVisible() {
				// Good, we can keep this offset
				continue
			}
			// Last tab no longer fully visible, restore and stop
			t.tabScrollOffset++
			break
		}
	}
}

// adjustVertScrollOffsetForResize adjusts the vertical tab scroll offset when the trinket is resized.
// When shrinking: ensure the current tab stays visible.
// When growing: reveal more tabs at the top if possible, or reset to 0 if all fit.
func (t *TabTrinket) adjustVertScrollOffsetForResize(isShrinking bool) {
	if len(t.tabs) == 0 {
		t.vertScrollOffset = 0
		return
	}

	visibleCount := t.vertVisibleCount()

	// If scrolling isn't needed at all, reset to 0
	if !t.vertTabsNeedScrolling() {
		t.vertScrollOffset = 0
		return
	}

	if isShrinking {
		// When shrinking, ensure current tab is still visible
		t.vertEnsureVisible(t.currentIndex)
	} else {
		// When growing, try to reveal more tabs at the top
		// while keeping as many tabs visible as possible
		maxOffset := len(t.tabs) - visibleCount
		if maxOffset < 0 {
			maxOffset = 0
		}

		// Clamp current offset to valid range
		if t.vertScrollOffset > maxOffset {
			t.vertScrollOffset = maxOffset
		}

		// Try to reduce offset to show more tabs from the top
		// while keeping the current tab visible
		for t.vertScrollOffset > 0 {
			// Check if current tab would still be visible with smaller offset
			if t.currentIndex >= t.vertScrollOffset-1 &&
				t.currentIndex < t.vertScrollOffset-1+visibleCount {
				t.vertScrollOffset--
			} else {
				break
			}
		}
	}
}

// AccessibleInfo returns accessibility information.
func (t *TabTrinket) AccessibleInfo() core.AccessibleInfo {
	info := t.AccessibleTrinket.AccessibleInfo()
	info.Role = core.RoleTabList
	info.SetSize = len(t.tabs)

	if t.currentIndex >= 0 && t.currentIndex < len(t.tabs) {
		info.PositionInSet = t.currentIndex + 1
		info.Value = t.tabs[t.currentIndex].Text
	}

	if !t.IsEnabled() {
		info.State |= core.StateDisabled
	}

	return info
}
