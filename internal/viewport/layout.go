// Package viewport provides viewport management for the editor.
package viewport

// ViewportLayout holds the calculated position and size for one TILE showing a
// viewport. Geometry lives here, with the tile — not on the viewport — because a
// viewport can be shown in several tiles at once (tiles↔viewports is
// many-to-many). The renderer applies each entry's frame to the viewport just
// before painting that tile, so the same viewport paints correctly at every
// tile that references it.
type ViewportLayout struct {
	Viewport *Viewport
	Y        int // Top position (0-indexed)
	Height   int
	// FrameX/FrameWidth are this tile's horizontal paint frame (0-based start
	// column, width). The zero value (0, 0) means "full width from the left
	// edge", as for docked chrome that fills the row.
	FrameX     int
	FrameWidth int
	// ContentX/ContentY/ContentWidth/ContentHeight are this tile's content
	// rectangle (the paint frame minus ruler, message bars, gutter and
	// scrollbar), recorded during rendering so mouse handling can resolve which
	// tile a click landed in and map the cell to a document position with THAT
	// tile's offset — necessary because a viewport shown in several tiles cannot
	// hold each tile's rectangle in its own single-valued fields.
	ContentX      int
	ContentY      int
	ContentWidth  int
	ContentHeight int
	// ScrollbarX/ScrollbarTrackH are this tile's vertical scrollbar column
	// (-1 when the tile shows no bar) and its track height, recorded so a press
	// on the bar is grabbed against the correct tile's bar.
	ScrollbarX      int
	ScrollbarTrackH int
	// Focused marks the tile that currently holds focus. When a viewport is shown
	// in several tiles, this is the one whose geometry becomes the viewport's
	// canonical resting state — so the caret and keyboard paging use the pane the
	// user is in, not whichever tile happened to paint last.
	Focused bool
	// TileHandle is the tiler's opaque handle for this tile (0 for docked chrome,
	// which the tiler does not manage). A press uses it to focus the exact tile it
	// landed in, so that tile becomes canonical.
	TileHandle uint64
}

// StampGeometry copies this tile's recorded frame and content rectangle onto its
// viewport, so the viewport's single-valued geometry reflects this specific
// tile. Used to make the focused tile canonical (render) and to map a click into
// the tile it hit (mouse).
func (l *ViewportLayout) StampGeometry() {
	w := l.Viewport
	if w == nil {
		return
	}
	w.FrameX = l.FrameX
	w.FrameWidth = l.FrameWidth
	w.ContentX = l.ContentX
	w.ContentY = l.ContentY
	w.ContentWidth = l.ContentWidth
	w.ContentHeight = l.ContentHeight
	w.ScrollbarX = l.ScrollbarX
	w.ScrollbarTrackH = l.ScrollbarTrackH
}

// Layout holds the complete calculated layout for all viewports.
type Layout struct {
	TopLayout    []ViewportLayout
	MainLayout   []ViewportLayout
	BottomLayout []ViewportLayout

	TopHeight    int
	MainHeight   int
	BottomHeight int
}

// LayoutManager calculates viewport layouts based on screen dimensions.
type LayoutManager struct {
	viewportManager *Manager
}

// NewLayoutManager creates a new layout manager.
func NewLayoutManager(wm *Manager) *LayoutManager {
	return &LayoutManager{
		viewportManager: wm,
	}
}

// CalculateLayout calculates the layout for all viewports based on screen
// dimensions.
//
// The docked areas negotiate for space with the main editing area: the main
// area is guaranteed at least a third of the screen (minimum 2 rows). When the
// docks want more than that allows, space is recovered in stages:
//
//  1. Shrink non-essential docked viewports toward their MinHeight.
//  2. Omit whole viewports from the docks, lowest priority first (top dock,
//     then bottom), never touching the modebar (wherever it is located) or
//     the active prompt.
//  3. Last resort: force the active prompt down to one row, and finally omit
//     even the modebar.
//
// Negotiation never mutates a viewport's Height: that field is the viewport's
// PREFERRED height, and the negotiated per-pass heights flow out only through
// the returned layout. A viewport squeezed on a small screen therefore
// re-expands as soon as space allows.
func (lm *LayoutManager) CalculateLayout(screenWidth, screenHeight int) Layout {
	wm := lm.viewportManager

	// Docked viewports. GetViewportsByDock returns the top dock in descending
	// priority order and other docks ascending. Negotiation works with both
	// lists in DESCENDING priority order (most-essential first).
	topViewports := wm.GetViewportsByDock(DockTop)
	bottomAsc := wm.GetViewportsByDock(DockBottom)
	mainViewports := wm.GetViewportsByDock(DockNone)

	bottomViewports := make([]*Viewport, len(bottomAsc))
	for i, w := range bottomAsc {
		bottomViewports[len(bottomAsc)-1-i] = w
	}

	// Prompt buffers belong to a main buffer's modal stack: only the current
	// (last) main buffer's prompts are laid out, plus orphaned prompts with no
	// ParentViewport. Other mains' stacks stay hidden until their main is current
	// again (focusing one of their prompts restores it).
	lastMain := wm.GetLastMainViewport()
	visibleBottom := bottomViewports[:0]
	for _, w := range bottomViewports {
		if w.Type == PromptViewport && w.ParentViewport != nil && w.ParentViewport != lastMain {
			continue
		}
		visibleBottom = append(visibleBottom, w)
	}
	bottomViewports = visibleBottom

	// The modebar is dock furniture: always visible (until the very last
	// resort), never shrunk or omitted, wherever it is located.
	var modebar *Viewport
	for _, w := range topViewports {
		if w.Class == "modebar" {
			modebar = w
			break
		}
	}
	if modebar == nil {
		for _, w := range bottomViewports {
			if w.Class == "modebar" {
				modebar = w
				break
			}
		}
	}

	// Essential viewports: the modebar, and the active prompt (the
	// highest-priority prompt buffer in the bottom dock, else the
	// highest-priority other bottom viewport). Essential status in the top
	// dock belongs to the modebar itself, not to its position: when the
	// modebar lives in the bottom dock, every top viewport negotiates
	// normally. (Sessions without a modebar keep the legacy rule that the
	// highest-priority top viewport is essential.)
	var topPriorityViewport *Viewport
	if modebar != nil {
		if containsViewport(topViewports, modebar) {
			topPriorityViewport = modebar
		}
	} else if len(topViewports) > 0 {
		topPriorityViewport = topViewports[0]
	}
	var activePrompt *Viewport
	for _, w := range bottomViewports {
		if w.Type == PromptViewport {
			activePrompt = w
			break
		}
	}
	if activePrompt == nil {
		for _, w := range bottomViewports {
			if w != modebar {
				activePrompt = w
				break
			}
		}
	}

	// Working copies of each dock's viewports; the space-negotiation stages
	// below may drop entries from these without touching the manager's lists.
	effectiveTop := append([]*Viewport(nil), topViewports...)
	effectiveBottom := append([]*Viewport(nil), bottomViewports...)

	// Negotiated heights for this pass, starting from each docked viewport's
	// preferred height clamped into [MinHeight, MaxHeight]. Viewport state is
	// never written back, so squeezed viewports re-expand when space returns.
	negotiated := make(map[*Viewport]int, len(effectiveTop)+len(effectiveBottom))
	for _, w := range effectiveTop {
		negotiated[w] = clampHeight(w, screenHeight)
	}
	for _, w := range effectiveBottom {
		negotiated[w] = clampHeight(w, screenHeight)
	}

	// Space requirements: the main area gets at least a third of the screen,
	// minimum 2 rows.
	availableMainHeight := screenHeight - sumHeights(effectiveTop, negotiated) - sumHeights(effectiveBottom, negotiated)
	requiredMainHeight := screenHeight / 3
	if requiredMainHeight < 2 {
		requiredMainHeight = 2
	}

	if availableMainHeight < requiredMainHeight {
		spaceNeeded := requiredMainHeight - availableMainHeight

		// Stage 1: shrink non-essential docked viewports toward MinHeight.
		remaining := lm.reduceNonEssentialViewports(effectiveTop, effectiveBottom, topPriorityViewport, activePrompt, modebar, spaceNeeded, negotiated)

		// Stage 2: omit whole viewports, lowest priority first.
		if remaining > 0 {
			effectiveTop, effectiveBottom, remaining = lm.omitLowerPriorityViewports(effectiveTop, effectiveBottom, topPriorityViewport, activePrompt, modebar, remaining, negotiated)
		}

		// Stage 3: force the active prompt to one row, then omit even the
		// modebar if there is still no room.
		if remaining > 0 {
			if activePrompt != nil && negotiated[activePrompt] > 1 {
				reduction := remaining
				if reduction > negotiated[activePrompt]-1 {
					reduction = negotiated[activePrompt] - 1
				}
				negotiated[activePrompt] -= reduction
				remaining -= reduction
			}
			if remaining > 0 && modebar != nil {
				if containsViewport(effectiveTop, modebar) {
					effectiveTop = removeViewport(effectiveTop, modebar)
					remaining -= negotiated[modebar]
				} else if containsViewport(effectiveBottom, modebar) {
					effectiveBottom = removeViewport(effectiveBottom, modebar)
					remaining -= negotiated[modebar]
				}
			}
			if remaining > 0 && topPriorityViewport != nil && topPriorityViewport != modebar {
				effectiveTop = removeViewport(effectiveTop, topPriorityViewport)
			}
		}
	}

	// Calculate top layout: highest priority renders at the screen top.
	sortViewportsByPriority(effectiveTop, true)
	topLayout := make([]ViewportLayout, 0, len(effectiveTop))
	topY := 0
	for _, w := range effectiveTop {
		topLayout = append(topLayout, ViewportLayout{
			Viewport: w,
			Y:        topY,
			Height:   negotiated[w],
		})
		topY += negotiated[w]
	}
	topHeight := topY

	// Calculate bottom layout: lowest priority renders at the top of the
	// bottom dock, highest priority at the screen bottom.
	sortViewportsByPriority(effectiveBottom, false)
	bottomHeight := sumHeights(effectiveBottom, negotiated)
	bottomLayout := make([]ViewportLayout, 0, len(effectiveBottom))
	bottomY := screenHeight - bottomHeight
	for _, w := range effectiveBottom {
		bottomLayout = append(bottomLayout, ViewportLayout{
			Viewport: w,
			Y:        bottomY,
			Height:   negotiated[w],
		})
		bottomY += negotiated[w]
	}

	// Calculate main layout. Non-docked viewports all share the same main-area
	// rectangle, so only the last-focused one is laid out (and thus painted) —
	// the others would just overlap it. TODO: support additional tiling modes
	// (split panes, side-by-side, etc.) that lay out more than one here.
	mainHeight := screenHeight - topHeight - bottomHeight
	lastNormal := wm.GetLastNormalViewport()
	mainLayout := make([]ViewportLayout, 0, 1)
	for _, w := range mainViewports {
		// Only paint the last-focused non-docked viewport. (If none is tracked,
		// fall back to painting all, preserving prior behavior.)
		if lastNormal != nil && w.ID != lastNormal.ID {
			continue
		}
		mainLayout = append(mainLayout, ViewportLayout{
			Viewport: w,
			Y:        topHeight,
			Height:   mainHeight,
		})
	}

	return Layout{
		TopLayout:    topLayout,
		MainLayout:   mainLayout,
		BottomLayout: bottomLayout,
		TopHeight:    topHeight,
		MainHeight:   mainHeight,
		BottomHeight: bottomHeight,
	}
}

// reduceNonEssentialViewports shrinks non-essential docked viewports toward their
// MinHeight in the negotiated-height map, top dock first, and returns how much
// space is still needed. The essential viewports — the modebar and each dock's
// designated viewport — keep their negotiated heights.
func (lm *LayoutManager) reduceNonEssentialViewports(topViewports, bottomViewports []*Viewport, topPriorityViewport, activePrompt, modebar *Viewport, spaceNeeded int, negotiated map[*Viewport]int) int {
	remaining := spaceNeeded

	shrink := func(viewports []*Viewport, essential *Viewport) {
		for _, w := range viewports {
			if remaining <= 0 {
				return
			}
			if w == essential || w == modebar || negotiated[w] <= w.MinHeight {
				continue
			}
			reduction := negotiated[w] - w.MinHeight
			if reduction > remaining {
				reduction = remaining
			}
			negotiated[w] -= reduction
			remaining -= reduction
		}
	}

	shrink(topViewports, topPriorityViewport)
	shrink(bottomViewports, activePrompt)

	return remaining
}

// omitLowerPriorityViewports removes whole viewports from the docks when space is
// critically low: lowest priority first, top dock first, never the modebar,
// the top-priority viewport, or the active prompt. Both slices are in
// descending priority order. Returns the filtered slices and any space still
// needed.
func (lm *LayoutManager) omitLowerPriorityViewports(topViewports, bottomViewports []*Viewport, topPriorityViewport, activePrompt, modebar *Viewport, spaceNeeded int, negotiated map[*Viewport]int) ([]*Viewport, []*Viewport, int) {
	remaining := spaceNeeded

	omit := func(viewports []*Viewport, essential *Viewport) []*Viewport {
		filtered := append([]*Viewport(nil), viewports...)
		for i := len(filtered) - 1; i >= 0 && remaining > 0; i-- {
			w := filtered[i]
			if w == essential || w == modebar {
				continue
			}
			remaining -= negotiated[w]
			filtered = append(filtered[:i], filtered[i+1:]...)
		}
		return filtered
	}

	filteredTop := omit(topViewports, topPriorityViewport)
	filteredBottom := omit(bottomViewports, activePrompt)

	return filteredTop, filteredBottom, remaining
}

// clampHeight returns the viewport's preferred height for a mew area of
// areaHeight rows. A viewport with MaxHeightFraction > 0 sizes to a proportion
// of the area less the 2 rows reserved for chrome — floor((areaHeight-2) *
// fraction) — and that cap IS its preferred height, so it opens as tall as
// allowed and layout negotiation shrinks it toward MinHeight under pressure. A
// literal MaxHeight given alongside the fraction is the hard ceiling ("max
// max"): the effective cap is the smaller of the two. Without a fraction, the
// preferred Height is clamped into [MinHeight, MaxHeight] (MaxHeight 0 means
// unbounded). MinHeight is the hard floor in every case.
func clampHeight(w *Viewport, areaHeight int) int {
	if w.MaxHeightFraction > 0 {
		avail := areaHeight - 2
		if avail < 1 {
			avail = 1
		}
		h := int(float64(avail) * w.MaxHeightFraction)
		if w.MaxHeight > 0 && h > w.MaxHeight {
			h = w.MaxHeight // a literal max is the hard ceiling over the proportion
		}
		if h < w.MinHeight {
			h = w.MinHeight
		}
		return h
	}
	h := w.Height
	if h < w.MinHeight {
		h = w.MinHeight
	}
	if w.MaxHeight > 0 && h > w.MaxHeight {
		h = w.MaxHeight
	}
	return h
}

// sumHeights totals the negotiated heights of the given viewports.
func sumHeights(viewports []*Viewport, negotiated map[*Viewport]int) int {
	total := 0
	for _, w := range viewports {
		total += negotiated[w]
	}
	return total
}

// containsViewport reports whether the slice contains the viewport.
func containsViewport(viewports []*Viewport, target *Viewport) bool {
	for _, w := range viewports {
		if w == target {
			return true
		}
	}
	return false
}

// removeViewport returns the slice with the viewport removed.
func removeViewport(viewports []*Viewport, target *Viewport) []*Viewport {
	out := viewports[:0]
	for _, w := range viewports {
		if w != target {
			out = append(out, w)
		}
	}
	return out
}

// sortViewportsByPriority sorts viewports by priority (descending or ascending)
// with a stable secondary sort by ID for determinism.
func sortViewportsByPriority(viewports []*Viewport, descending bool) {
	for i := 0; i < len(viewports)-1; i++ {
		for j := i + 1; j < len(viewports); j++ {
			swap := false
			if descending {
				if viewports[i].Priority < viewports[j].Priority {
					swap = true
				} else if viewports[i].Priority == viewports[j].Priority && viewports[i].ID > viewports[j].ID {
					swap = true
				}
			} else {
				if viewports[i].Priority > viewports[j].Priority {
					swap = true
				} else if viewports[i].Priority == viewports[j].Priority && viewports[i].ID > viewports[j].ID {
					swap = true
				}
			}
			if swap {
				viewports[i], viewports[j] = viewports[j], viewports[i]
			}
		}
	}
}

// FindViewportLayout finds the layout for a specific viewport.
func (l *Layout) FindViewportLayout(viewportID string) *ViewportLayout {
	for i := range l.TopLayout {
		if l.TopLayout[i].Viewport.ID == viewportID {
			return &l.TopLayout[i]
		}
	}
	// Prefer the focused tile when a viewport is shown in several: the caret and
	// cursor belong in the pane the user is in.
	var firstMain *ViewportLayout
	for i := range l.MainLayout {
		if l.MainLayout[i].Viewport.ID == viewportID {
			if l.MainLayout[i].Focused {
				return &l.MainLayout[i]
			}
			if firstMain == nil {
				firstMain = &l.MainLayout[i]
			}
		}
	}
	if firstMain != nil {
		return firstMain
	}
	for i := range l.BottomLayout {
		if l.BottomLayout[i].Viewport.ID == viewportID {
			return &l.BottomLayout[i]
		}
	}
	return nil
}
