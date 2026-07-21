// Package window provides window management for the editor.
package window

// WindowLayout holds the calculated position and size for a window.
type WindowLayout struct {
	Window *Window
	Y      int // Top position (0-indexed)
	Height int
}

// Layout holds the complete calculated layout for all windows.
type Layout struct {
	TopLayout    []WindowLayout
	MainLayout   []WindowLayout
	BottomLayout []WindowLayout

	TopHeight    int
	MainHeight   int
	BottomHeight int

	// Peek indicators
	NeedsStatPeekUp     bool
	NeedsStatPeekDown   bool
	NeedsPromptPeekUp   bool
	NeedsPromptPeekDown bool
}

// LayoutManager calculates window layouts based on screen dimensions.
type LayoutManager struct {
	windowManager *Manager
}

// NewLayoutManager creates a new layout manager.
func NewLayoutManager(wm *Manager) *LayoutManager {
	return &LayoutManager{
		windowManager: wm,
	}
}

// CalculateLayout calculates the layout for all windows based on screen
// dimensions.
//
// The docked areas negotiate for space with the main editing area: the main
// area is guaranteed at least a third of the screen (minimum 2 rows). When the
// docks want more than that allows, space is recovered in stages:
//
//  1. Shrink non-essential docked windows toward their MinHeight.
//  2. Omit whole windows from the docks, lowest priority first (top dock,
//     then bottom), never touching the modebar (wherever it is located) or
//     the active prompt. Omitted windows are surfaced via peek indicators.
//  3. Last resort: force the active prompt down to one row, and finally omit
//     even the modebar.
//
// Negotiation never mutates a window's Height: that field is the window's
// PREFERRED height, and the negotiated per-pass heights flow out only through
// the returned layout. A window squeezed on a small screen therefore
// re-expands as soon as space allows.
func (lm *LayoutManager) CalculateLayout(screenWidth, screenHeight int) Layout {
	wm := lm.windowManager

	// Docked windows. GetWindowsByDock returns the top dock in descending
	// priority order and other docks ascending. Negotiation works with both
	// lists in DESCENDING priority order (most-essential first).
	topWindows := wm.GetWindowsByDock(DockTop)
	bottomAsc := wm.GetWindowsByDock(DockBottom)
	mainWindows := wm.GetWindowsByDock(DockNone)

	bottomWindows := make([]*Window, len(bottomAsc))
	for i, w := range bottomAsc {
		bottomWindows[len(bottomAsc)-1-i] = w
	}

	// Prompt buffers belong to a main buffer's modal stack: only the current
	// (last) main buffer's prompts are laid out, plus orphaned prompts with no
	// ParentWindow. Other mains' stacks stay hidden until their main is current
	// again (focusing one of their prompts restores it).
	lastMain := wm.GetLastMainWindow()
	visibleBottom := bottomWindows[:0]
	for _, w := range bottomWindows {
		if w.Type == PromptWindow && w.ParentWindow != nil && w.ParentWindow != lastMain {
			continue
		}
		visibleBottom = append(visibleBottom, w)
	}
	bottomWindows = visibleBottom

	// The modebar is dock furniture: always visible (until the very last
	// resort), never shrunk or omitted, wherever it is located.
	var modebar *Window
	for _, w := range topWindows {
		if w.Class == "modebar" {
			modebar = w
			break
		}
	}
	if modebar == nil {
		for _, w := range bottomWindows {
			if w.Class == "modebar" {
				modebar = w
				break
			}
		}
	}

	// Essential windows: the modebar, and the active prompt (the
	// highest-priority prompt buffer in the bottom dock, else the
	// highest-priority other bottom window). Essential status in the top
	// dock belongs to the modebar itself, not to its position: when the
	// modebar lives in the bottom dock, every top window negotiates
	// normally. (Sessions without a modebar keep the legacy rule that the
	// highest-priority top window is essential.)
	var topPriorityWindow *Window
	if modebar != nil {
		if containsWindow(topWindows, modebar) {
			topPriorityWindow = modebar
		}
	} else if len(topWindows) > 0 {
		topPriorityWindow = topWindows[0]
	}
	var activePrompt *Window
	for _, w := range bottomWindows {
		if w.Type == PromptWindow {
			activePrompt = w
			break
		}
	}
	if activePrompt == nil {
		for _, w := range bottomWindows {
			if w != modebar {
				activePrompt = w
				break
			}
		}
	}

	// Apply peek adjustments.
	effectiveTop := lm.effectiveTopWindows(topWindows, topPriorityWindow)
	effectiveBottom := lm.effectiveBottomWindows(bottomWindows, activePrompt)

	// Negotiated heights for this pass, starting from each docked window's
	// preferred height clamped into [MinHeight, MaxHeight]. Window state is
	// never written back, so squeezed windows re-expand when space returns.
	negotiated := make(map[*Window]int, len(effectiveTop)+len(effectiveBottom))
	for _, w := range effectiveTop {
		negotiated[w] = clampHeight(w)
	}
	for _, w := range effectiveBottom {
		negotiated[w] = clampHeight(w)
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

		// Stage 1: shrink non-essential docked windows toward MinHeight.
		remaining := lm.reduceNonEssentialWindows(effectiveTop, effectiveBottom, topPriorityWindow, activePrompt, modebar, spaceNeeded, negotiated)

		// Stage 2: omit whole windows, lowest priority first.
		if remaining > 0 {
			effectiveTop, effectiveBottom, remaining = lm.omitLowerPriorityWindows(effectiveTop, effectiveBottom, topPriorityWindow, activePrompt, modebar, remaining, negotiated)
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
				if containsWindow(effectiveTop, modebar) {
					effectiveTop = removeWindow(effectiveTop, modebar)
					remaining -= negotiated[modebar]
				} else if containsWindow(effectiveBottom, modebar) {
					effectiveBottom = removeWindow(effectiveBottom, modebar)
					remaining -= negotiated[modebar]
				}
			}
			if remaining > 0 && topPriorityWindow != nil && topPriorityWindow != modebar {
				effectiveTop = removeWindow(effectiveTop, topPriorityWindow)
			}
		}
	}

	// Calculate top layout: highest priority renders at the screen top.
	sortWindowsByPriority(effectiveTop, true)
	topLayout := make([]WindowLayout, 0, len(effectiveTop))
	topY := 0
	for _, w := range effectiveTop {
		topLayout = append(topLayout, WindowLayout{
			Window: w,
			Y:      topY,
			Height: negotiated[w],
		})
		topY += negotiated[w]
	}
	topHeight := topY

	// Calculate bottom layout: lowest priority renders at the top of the
	// bottom dock, highest priority at the screen bottom.
	sortWindowsByPriority(effectiveBottom, false)
	bottomHeight := sumHeights(effectiveBottom, negotiated)
	bottomLayout := make([]WindowLayout, 0, len(effectiveBottom))
	bottomY := screenHeight - bottomHeight
	for _, w := range effectiveBottom {
		bottomLayout = append(bottomLayout, WindowLayout{
			Window: w,
			Y:      bottomY,
			Height: negotiated[w],
		})
		bottomY += negotiated[w]
	}

	// Calculate main layout. Non-docked windows all share the same main-area
	// rectangle, so only the last-focused one is laid out (and thus painted) —
	// the others would just overlap it. TODO: support additional tiling modes
	// (split panes, side-by-side, etc.) that lay out more than one here.
	mainHeight := screenHeight - topHeight - bottomHeight
	lastNormal := wm.GetLastNormalWindow()
	mainLayout := make([]WindowLayout, 0, 1)
	for _, w := range mainWindows {
		// Only paint the last-focused non-docked window. (If none is tracked,
		// fall back to painting all, preserving prior behavior.)
		if lastNormal != nil && w.ID != lastNormal.ID {
			continue
		}
		mainLayout = append(mainLayout, WindowLayout{
			Window: w,
			Y:      topHeight,
			Height: mainHeight,
		})
	}

	// Determine peek indicators. Windows omitted for space also surface here
	// (effective < all), not just explicit peeking.
	needsStatPeekUp := len(topWindows) > len(effectiveTop) || wm.StatPeek > 0
	needsStatPeekDown := len(topWindows) > 1 && wm.StatPeek > 0
	needsPromptPeekUp := len(bottomWindows) > 1 && wm.PromptPeek > 0
	needsPromptPeekDown := len(bottomWindows) > len(effectiveBottom) || wm.PromptPeek > 0

	return Layout{
		TopLayout:           topLayout,
		MainLayout:          mainLayout,
		BottomLayout:        bottomLayout,
		TopHeight:           topHeight,
		MainHeight:          mainHeight,
		BottomHeight:        bottomHeight,
		NeedsStatPeekUp:     needsStatPeekUp,
		NeedsStatPeekDown:   needsStatPeekDown,
		NeedsPromptPeekUp:   needsPromptPeekUp,
		NeedsPromptPeekDown: needsPromptPeekDown,
	}
}

// effectiveTopWindows applies statPeek to the top dock. Peeking hides the
// next-highest-priority windows below the top-priority one, revealing
// lower-priority windows that may be buried (or omitted for space) beneath
// them. Input and output are in descending priority order.
func (lm *LayoutManager) effectiveTopWindows(sortedTop []*Window, topPriorityWindow *Window) []*Window {
	wm := lm.windowManager
	effective := append([]*Window(nil), sortedTop...)

	if wm.StatPeek > 0 && len(sortedTop) > 1 {
		peekCount := wm.StatPeek
		if peekCount > len(sortedTop)-1 {
			peekCount = len(sortedTop) - 1
		}
		// Hide the windows just below the top-priority one (index 0).
		hidden := make(map[*Window]bool, peekCount)
		for _, w := range sortedTop[1 : peekCount+1] {
			hidden[w] = true
		}
		effective = effective[:0]
		for _, w := range sortedTop {
			if !hidden[w] {
				effective = append(effective, w)
			}
		}
	}

	// Ensure the top-priority window is always included.
	if topPriorityWindow != nil && !containsWindow(effective, topPriorityWindow) {
		effective = append([]*Window{topPriorityWindow}, effective...)
	}

	return effective
}

// effectiveBottomWindows applies promptPeek to the bottom dock. Peeking hides
// the highest-priority non-prompt windows, revealing buried lower-priority
// ones; the active prompt is never hidden. Input and output are in descending
// priority order.
func (lm *LayoutManager) effectiveBottomWindows(sortedBottom []*Window, activePrompt *Window) []*Window {
	wm := lm.windowManager
	effective := append([]*Window(nil), sortedBottom...)

	if wm.PromptPeek > 0 && len(sortedBottom) > 1 {
		peekCount := wm.PromptPeek
		if peekCount > len(sortedBottom)-1 {
			peekCount = len(sortedBottom) - 1
		}
		hidden := make(map[*Window]bool, peekCount)
		for _, w := range sortedBottom {
			if len(hidden) >= peekCount {
				break
			}
			// A bottom-located modebar is furniture, not a peekable
			// overlay: peeking hides the windows stacked above the
			// buried prompts, never the modebar itself.
			if w != activePrompt && w.Class != "modebar" {
				hidden[w] = true
			}
		}
		effective = effective[:0]
		for _, w := range sortedBottom {
			if !hidden[w] {
				effective = append(effective, w)
			}
		}
	}

	// Ensure the active prompt is always included.
	if activePrompt != nil && !containsWindow(effective, activePrompt) {
		effective = append(effective, activePrompt)
	}

	return effective
}

// reduceNonEssentialWindows shrinks non-essential docked windows toward their
// MinHeight in the negotiated-height map, top dock first, and returns how much
// space is still needed. The essential windows — the modebar and each dock's
// designated window — keep their negotiated heights.
func (lm *LayoutManager) reduceNonEssentialWindows(topWindows, bottomWindows []*Window, topPriorityWindow, activePrompt, modebar *Window, spaceNeeded int, negotiated map[*Window]int) int {
	remaining := spaceNeeded

	shrink := func(windows []*Window, essential *Window) {
		for _, w := range windows {
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

	shrink(topWindows, topPriorityWindow)
	shrink(bottomWindows, activePrompt)

	return remaining
}

// omitLowerPriorityWindows removes whole windows from the docks when space is
// critically low: lowest priority first, top dock first, never the modebar,
// the top-priority window, or the active prompt. Both slices are in
// descending priority order. Returns the filtered slices and any space still
// needed.
func (lm *LayoutManager) omitLowerPriorityWindows(topWindows, bottomWindows []*Window, topPriorityWindow, activePrompt, modebar *Window, spaceNeeded int, negotiated map[*Window]int) ([]*Window, []*Window, int) {
	remaining := spaceNeeded

	omit := func(windows []*Window, essential *Window) []*Window {
		filtered := append([]*Window(nil), windows...)
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

	filteredTop := omit(topWindows, topPriorityWindow)
	filteredBottom := omit(bottomWindows, activePrompt)

	return filteredTop, filteredBottom, remaining
}

// clampHeight returns the window's preferred height clamped into
// [MinHeight, MaxHeight] (MaxHeight 0 means unbounded).
func clampHeight(w *Window) int {
	h := w.Height
	if h < w.MinHeight {
		h = w.MinHeight
	}
	if w.MaxHeight > 0 && h > w.MaxHeight {
		h = w.MaxHeight
	}
	return h
}

// sumHeights totals the negotiated heights of the given windows.
func sumHeights(windows []*Window, negotiated map[*Window]int) int {
	total := 0
	for _, w := range windows {
		total += negotiated[w]
	}
	return total
}

// containsWindow reports whether the slice contains the window.
func containsWindow(windows []*Window, target *Window) bool {
	for _, w := range windows {
		if w == target {
			return true
		}
	}
	return false
}

// removeWindow returns the slice with the window removed.
func removeWindow(windows []*Window, target *Window) []*Window {
	out := windows[:0]
	for _, w := range windows {
		if w != target {
			out = append(out, w)
		}
	}
	return out
}

// sortWindowsByPriority sorts windows by priority (descending or ascending)
// with a stable secondary sort by ID for determinism.
func sortWindowsByPriority(windows []*Window, descending bool) {
	for i := 0; i < len(windows)-1; i++ {
		for j := i + 1; j < len(windows); j++ {
			swap := false
			if descending {
				if windows[i].Priority < windows[j].Priority {
					swap = true
				} else if windows[i].Priority == windows[j].Priority && windows[i].ID > windows[j].ID {
					swap = true
				}
			} else {
				if windows[i].Priority > windows[j].Priority {
					swap = true
				} else if windows[i].Priority == windows[j].Priority && windows[i].ID > windows[j].ID {
					swap = true
				}
			}
			if swap {
				windows[i], windows[j] = windows[j], windows[i]
			}
		}
	}
}

// FindWindowLayout finds the layout for a specific window.
func (l *Layout) FindWindowLayout(windowID string) *WindowLayout {
	for i := range l.TopLayout {
		if l.TopLayout[i].Window.ID == windowID {
			return &l.TopLayout[i]
		}
	}
	for i := range l.MainLayout {
		if l.MainLayout[i].Window.ID == windowID {
			return &l.MainLayout[i]
		}
	}
	for i := range l.BottomLayout {
		if l.BottomLayout[i].Window.ID == windowID {
			return &l.BottomLayout[i]
		}
	}
	return nil
}
