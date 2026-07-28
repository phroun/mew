// Package core provides fundamental types for KittyTK.
package core

import (
	"sync"

	"github.com/phroun/kittytk/style"
)

// Trinket is the base interface for all UI elements.
// All coordinates and sizes are in abstract units (see units.go).
type Trinket interface {
	// Identity and hierarchy

	// Name returns an optional identifier for the trinket.
	Name() string
	SetName(name string)

	// Parent returns the parent container, or nil if this is a root trinket.
	Parent() Container
	SetParent(parent Container)

	// Geometry (all in abstract units)

	// Bounds returns the trinket's position and size within its parent.
	Bounds() UnitRect
	SetBounds(bounds UnitRect)

	// Pos returns just the position.
	Pos() UnitPoint
	SetPos(pos UnitPoint)

	// Size returns just the dimensions.
	Size() UnitSize
	SetSize(size UnitSize)

	// MinimumSize returns the smallest size the trinket can be.
	MinimumSize() UnitSize
	SetMinimumSize(size UnitSize)

	// MaximumSize returns the largest size the trinket can be.
	MaximumSize() UnitSize
	SetMaximumSize(size UnitSize)

	// SizeHint returns the preferred/natural size.
	SizeHint() UnitSize

	// SizePolicy returns how the trinket should resize.
	SizePolicy() SizePolicyPair
	SetSizePolicy(policy SizePolicyPair)

	// Margins returns spacing around the trinket.
	Margins() UnitMargins
	SetMargins(margins UnitMargins)

	// State

	// IsVisible returns whether the trinket is visible.
	IsVisible() bool
	SetVisible(visible bool)
	Show()
	Hide()

	// IsEnabled returns whether the trinket accepts input.
	IsEnabled() bool
	SetEnabled(enabled bool)

	// Focus

	// FocusPolicy returns how the trinket can receive focus.
	FocusPolicy() FocusPolicy
	SetFocusPolicy(policy FocusPolicy)

	// HasFocus returns whether this trinket currently has focus.
	HasFocus() bool

	// SetFocus attempts to give focus to this trinket.
	SetFocus()

	// SetFocusWithoutScroll sets focus without scrolling parent containers.
	// Use this for mouse-initiated focus changes where visibility is proven.
	SetFocusWithoutScroll()

	// ClearFocus removes focus from this trinket.
	ClearFocus()

	// Furtive returns whether this trinket is "furtive" - meaning it can be
	// tabbed to but does not gain focus from mouse clicks, and is skipped
	// when auto-selecting the initial focus item in a container.
	Furtive() bool
	SetFurtive(furtive bool)

	// Styling

	// Style returns the trinket's style (may be nil to use parent/theme).
	Style() *style.CellStyle
	SetStyle(s *style.CellStyle)

	// Theme returns the theme to use for this trinket.
	Theme() *style.Theme

	// Scheme returns the scheme ID for this trinket (-1 = inherit from container).
	Scheme() style.SchemeID
	// SetScheme sets the scheme ID for this trinket.
	SetScheme(id style.SchemeID)
	// EffectiveScheme returns the resolved scheme ID by walking up the container chain.
	EffectiveScheme() style.SchemeID
	// GetScheme returns the resolved Scheme object for this trinket.
	GetScheme() *style.Scheme

	// Rendering

	// Paint renders the trinket using the provided painter.
	// The painter is already configured with the trinket's coordinate system.
	Paint(p *Painter)

	// Update marks the trinket as needing repaint.
	Update()

	// NeedsRepaint returns true if the trinket needs repainting.
	NeedsRepaint() bool

	// Events

	// HandleKeyPress processes a key press event.
	// Returns true if the event was consumed.
	HandleKeyPress(event KeyPressEvent) bool

	// HandleKeyRelease processes a key release event.
	// Note: Not all terminals support key release events.
	HandleKeyRelease(event KeyReleaseEvent) bool

	// HandleMousePress processes a mouse button press.
	HandleMousePress(event MousePressEvent) bool

	// HandleMouseRelease processes a mouse button release.
	HandleMouseRelease(event MouseReleaseEvent) bool

	// HandleMouseMove processes mouse movement.
	HandleMouseMove(event MouseMoveEvent) bool

	// HandleMouseWheel processes mouse wheel scrolling.
	HandleMouseWheel(event MouseWheelEvent) bool

	// HandleFocusIn is called when the trinket gains focus.
	HandleFocusIn()

	// HandleFocusOut is called when the trinket loses focus.
	HandleFocusOut()

	// HandleResize is called when the trinket is resized.
	HandleResize(oldSize, newSize UnitSize)

	// Application returns the application this trinket belongs to.
	Application() *Application
}

// Container is a trinket that can contain child trinkets.
type Container interface {
	Trinket

	// Children returns all child trinkets.
	Children() []Trinket

	// AddChild adds a child trinket.
	AddChild(child Trinket)

	// RemoveChild removes a child trinket.
	RemoveChild(child Trinket)

	// ChildAt returns the child at the given position, or nil.
	ChildAt(pos UnitPoint) Trinket

	// Layout arranges children within this container.
	Layout()

	// LayoutManager returns the layout manager, if any.
	LayoutManager() LayoutManager

	// SetLayoutManager sets the layout manager.
	SetLayoutManager(layout LayoutManager)
}

// LayoutManager arranges trinkets within a container.
type LayoutManager interface {
	// Layout arranges children within the given bounds.
	Layout(container Container, bounds UnitRect)

	// SizeHint returns the preferred size for the container.
	SizeHint(container Container) UnitSize

	// MinimumSize returns the minimum size for the container.
	MinimumSize(container Container) UnitSize

	// Spacing returns the gap between trinkets.
	Spacing() Unit

	// SetSpacing sets the gap between trinkets.
	SetSpacing(spacing Unit)

	// Margins returns the margins around all content.
	ContentsMargins() UnitMargins

	// SetContentsMargins sets the margins around all content.
	SetContentsMargins(margins UnitMargins)
}

// FocusableTrinket extends Trinket with focus chain navigation.
type FocusableTrinket interface {
	Trinket

	// NextFocusTrinket returns the next trinket in the focus chain.
	NextFocusTrinket() Trinket
	SetNextFocusTrinket(w Trinket)

	// PrevFocusTrinket returns the previous trinket in the focus chain.
	PrevFocusTrinket() Trinket
	SetPrevFocusTrinket(w Trinket)
}

// InlineTrinket is an optional interface for text-style/inline trinkets.
// Trinkets implementing this interface will receive horizontal margins
// when placed in a vertical box layout. Block-style trinkets (like
// ListView, TreeView, TabTrinket) should NOT implement this interface.
type InlineTrinket interface {
	// IsInlineTrinket returns true for text-style controls that should
	// receive horizontal margins in vertical layouts.
	IsInlineTrinket() bool
}

// HeightForWidther is an optional interface for trinkets whose required
// height depends on the width they are allocated (e.g. word-wrapped
// text). Layouts consult it during layout, when actual widths are
// known; SizeHint remains the width-independent preference. Containers
// whose content includes such trinkets should propagate it upward.
// (A WidthForHeight transpose may be added if vertical flow is ever
// needed; nothing requires it today.)
type HeightForWidther interface {
	// HasHeightForWidth returns true when the trinket's height currently
	// depends on its allocated width.
	HasHeightForWidth() bool

	// HeightForWidth returns the height required at the given width.
	HeightForWidth(width Unit) Unit
}

// PopupRequest contains information about a popup to be shown.
type PopupRequest struct {
	// Unique identifier for the popup
	ID string
	// Bounds in screen coordinates
	Bounds UnitRect
	// Paint function to render the popup
	Paint func(p *Painter)
	// HandleMousePress function to handle clicks (returns true if handled)
	HandleMousePress func(event MousePressEvent) bool
	// HandleMouseMove function to handle mouse movement (returns true if handled)
	HandleMouseMove func(event MouseMoveEvent) bool
	// HandleMouseRelease function to handle mouse release (returns true if handled)
	HandleMouseRelease func(event MouseReleaseEvent) bool
	// HandleMouseWheel function to handle wheel scrolling (returns true if handled)
	HandleMouseWheel func(event MouseWheelEvent) bool
	// OnDismiss is called when the HOST discards the popup without
	// routing the triggering event to the popup's own handlers (e.g.
	// a press outside every popup force-clears the overlay list). It
	// lets the owner reset its open-state - otherwise the owner still
	// believes its popup is up and keeps swallowing keys for a menu
	// that no longer exists. NOT called on an explicit UnregisterPopup.
	OnDismiss func()
}

// PopupController is an interface for managing popup overlays.
// Trinkets that need to show popups (like ComboBox) can use this
// to have their popups rendered on top of all windows.
type PopupController interface {
	// RegisterPopup registers a popup to be rendered on top of windows.
	RegisterPopup(request *PopupRequest)
	// UnregisterPopup removes a popup by ID.
	UnregisterPopup(id string)
	// MapToScreen converts local trinket coordinates to screen coordinates.
	MapToScreen(trinket Trinket, local UnitPoint) UnitPoint
	// ScreenBounds returns the available screen area for popups.
	ScreenBounds() UnitRect
}

// ScrollOffsetUnitsProvider is implemented by scroll containers whose
// offsets are already layout units (smooth surfaces). MapToScreen
// prefers it over ScrollOffsetProvider's cell-denominated offsets.
type ScrollOffsetUnitsProvider interface {
	ScrollOffsetUnits() (x, y Unit)
}

// ScrollOffsetProvider is implemented by trinkets that scroll their content
// (like ScrollArea). Used by MapToScreen to adjust for scroll position.
type ScrollOffsetProvider interface {
	// ScrollOffset returns the current scroll offset in cell units.
	ScrollOffset() (x, y int)
}

// KeyboardBlurChildrenProvider is implemented by containers (like MDIPane and Desktop)
// that support keyboard-based window blur. When enabled, windows in the container
// include a virtual "blur" focus item that allows keyboard users to exit the window
// and return focus to the parent container.
type KeyboardBlurChildrenProvider interface {
	// KeyboardBlurChildren returns whether keyboard blur is enabled for child windows.
	KeyboardBlurChildren() bool

	// PerformKeyboardBlur is called when the blur item is activated (Enter/Space).
	// It should deactivate the current window and return focus appropriately.
	PerformKeyboardBlur()
}

// PassiveWindowProvider is implemented by containers (like Desktop and MDIPane)
// to indicate when a window should be rendered with a "passive" frame style.
// A passive window uses thick single-line border (same color as active) instead
// of double-line border. This is used when the menu bar has focus but the window
// is remembered as the previous window, or when focus is in an MDI child.
type PassiveWindowProvider interface {
	// IsWindowPassive returns true if the given window should be painted with
	// passive (thick single-line) frame style instead of active (double-line).
	IsWindowPassive(win Trinket) bool
}

// Application is a forward declaration (defined in app package).
// This interface allows trinkets to access application-level services.
type Application struct {
	mu           sync.RWMutex
	backend      RenderBackend
	theme        *style.Theme
	focusTrinket Trinket
	rootTrinket  Trinket
	running      bool
	needsRepaint bool
	quitChan     chan struct{}
}

// TrinketBase provides a default implementation of the Trinket interface.
// Embed this in concrete trinket types.
type TrinketBase struct {
	mu sync.RWMutex

	// self is a reference to the outer trinket that embeds this TrinketBase.
	// This is needed because Go embedding doesn't support polymorphism -
	// when TrinketBase methods are called, 'w' refers to TrinketBase, not the
	// outer type. Trinkets should call Init(self) after embedding.
	self Trinket

	objectID ObjectID // stable object identity; immutable after construction

	name   string
	parent Container
	app    *Application

	bounds     UnitRect
	minSize    UnitSize
	maxSize    UnitSize
	sizePolicy SizePolicyPair
	margins    UnitMargins

	layoutStretch  int
	layoutAlign    Alignment
	layoutAlignSet bool

	visible bool
	enabled bool
	focused bool
	furtive bool

	focusPolicy     FocusPolicy
	scheme          style.SchemeID // -1 = inherit from container
	style           *style.CellStyle
	backgroundColor *style.Color // nil = inherit from parent
	font            *Font        // nil = inherit from parent/window/desktop
	cellMetrics     *CellMetrics // nil = inherit from parent/window/desktop
	popupController PopupController

	needsRepaint bool
}

// NewTrinketBase creates a new trinket base with default values.
func NewTrinketBase() *TrinketBase {
	return &TrinketBase{
		objectID:    NextObjectID(),
		visible:     true,
		enabled:     true,
		focusPolicy: NoFocus,
		scheme:      style.SchemeInherit, // -1 = inherit from container
		sizePolicy:  NewSizePolicy(SizePreferred, SizePreferred),
		maxSize:     UnitSize{Width: 1<<30 - 1, Height: 1<<30 - 1},
	}
}

// ObjectID returns this trinket's stable object identity (see ObjectID).
// Immutable after construction; no lock needed.
func (w *TrinketBase) ObjectID() ObjectID {
	return w.objectID
}

// Init initializes the TrinketBase with a reference to the outer trinket.
// This must be called by trinkets after embedding TrinketBase to enable
// proper polymorphic behavior (focus management, key forwarding, etc.).
func (w *TrinketBase) Init(self Trinket) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.self = self
}

// Self returns the outer trinket reference, or w if not set.
func (w *TrinketBase) Self() Trinket {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.self != nil {
		return w.self
	}
	return w
}

// Name returns the trinket's name.
func (w *TrinketBase) Name() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.name
}

// SetName sets the trinket's name.
func (w *TrinketBase) SetName(name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.name = name
}

// Parent returns the parent container.
func (w *TrinketBase) Parent() Container {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.parent
}

// SetParent sets the parent container.
func (w *TrinketBase) SetParent(parent Container) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.parent = parent
}

// Bounds returns the trinket's bounds.
func (w *TrinketBase) Bounds() UnitRect {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.bounds
}

// SetBounds sets the trinket's bounds.
func (w *TrinketBase) SetBounds(bounds UnitRect) {
	w.mu.Lock()
	oldSize := w.bounds.Size()
	w.bounds = bounds
	newSize := bounds.Size()
	w.needsRepaint = true
	app := w.app
	w.mu.Unlock()

	if oldSize != newSize {
		w.HandleResize(oldSize, newSize)
	}

	// Notify app to repaint
	if app != nil {
		app.requestRepaint()
	}
}

// Pos returns the trinket's position.
func (w *TrinketBase) Pos() UnitPoint {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return UnitPoint{X: w.bounds.X, Y: w.bounds.Y}
}

// SetPos sets the trinket's position.
func (w *TrinketBase) SetPos(pos UnitPoint) {
	w.mu.Lock()
	w.bounds.X = pos.X
	w.bounds.Y = pos.Y
	w.needsRepaint = true
	app := w.app
	w.mu.Unlock()

	// Notify app to repaint
	if app != nil {
		app.requestRepaint()
	}
}

// Size returns the trinket's size.
func (w *TrinketBase) Size() UnitSize {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return UnitSize{Width: w.bounds.Width, Height: w.bounds.Height}
}

// SetSize sets the trinket's size.
func (w *TrinketBase) SetSize(size UnitSize) {
	w.mu.Lock()
	oldSize := w.bounds.Size()
	w.bounds.Width = size.Width
	w.bounds.Height = size.Height
	w.needsRepaint = true
	w.mu.Unlock()

	if oldSize != size {
		w.HandleResize(oldSize, size)
	}
}

// MinimumSize returns the minimum size.
func (w *TrinketBase) MinimumSize() UnitSize {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.minSize
}

// SetMinimumSize sets the minimum size.
func (w *TrinketBase) SetMinimumSize(size UnitSize) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.minSize = size
}

// MaximumSize returns the maximum size.
func (w *TrinketBase) MaximumSize() UnitSize {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.maxSize
}

// SetMaximumSize sets the maximum size.
func (w *TrinketBase) SetMaximumSize(size UnitSize) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.maxSize = size
}

// SizeHint returns the preferred size (override in subclasses).
func (w *TrinketBase) SizeHint() UnitSize {
	w.mu.RLock()
	defer w.mu.RUnlock()
	// Default: use current size
	return w.bounds.Size()
}

// SizePolicy returns the size policy.
func (w *TrinketBase) SizePolicy() SizePolicyPair {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.sizePolicy
}

// SetSizePolicy sets the size policy.
func (w *TrinketBase) SetSizePolicy(policy SizePolicyPair) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sizePolicy = policy
}

// Layout hints travel with the trinket (wire vocabulary decision,
// 2026-07-05: stretch/align live on the child, not on an attach
// operation). Layout managers consult them when the trinket is added.

// LayoutStretch returns the trinket's stretch factor hint (0 = none).
func (w *TrinketBase) LayoutStretch() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.layoutStretch
}

// SetLayoutStretch sets the stretch factor hint.
func (w *TrinketBase) SetLayoutStretch(stretch int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.layoutStretch = stretch
}

// LayoutAlignment returns the trinket's alignment hint and whether one
// was explicitly set (layouts keep their own default otherwise).
func (w *TrinketBase) LayoutAlignment() (Alignment, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.layoutAlign, w.layoutAlignSet
}

// SetLayoutAlignment sets the alignment hint.
func (w *TrinketBase) SetLayoutAlignment(a Alignment) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.layoutAlign = a
	w.layoutAlignSet = true
}

// Margins returns the margins.
func (w *TrinketBase) Margins() UnitMargins {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.margins
}

// SetMargins sets the margins.
func (w *TrinketBase) SetMargins(margins UnitMargins) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.margins = margins
	w.needsRepaint = true
}

// IsVisible returns whether the trinket is visible.
func (w *TrinketBase) IsVisible() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.visible
}

// SetVisible sets visibility.
func (w *TrinketBase) SetVisible(visible bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.visible = visible
	w.needsRepaint = true
}

// Show makes the trinket visible.
func (w *TrinketBase) Show() {
	w.SetVisible(true)
}

// Hide makes the trinket invisible.
func (w *TrinketBase) Hide() {
	w.SetVisible(false)
}

// IsEnabled returns whether the trinket is enabled.
func (w *TrinketBase) IsEnabled() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.enabled
}

// SetEnabled sets the enabled state.
func (w *TrinketBase) SetEnabled(enabled bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.enabled = enabled
	w.needsRepaint = true
}

// FocusPolicy returns the focus policy.
func (w *TrinketBase) FocusPolicy() FocusPolicy {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.focusPolicy
}

// SetFocusPolicy sets the focus policy.
func (w *TrinketBase) SetFocusPolicy(policy FocusPolicy) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.focusPolicy = policy
}

// Furtive returns whether the trinket is "furtive".
// Furtive trinkets can be tabbed to, but do not gain focus from mouse clicks,
// and are skipped when auto-selecting the initial focus item in a container.
func (w *TrinketBase) Furtive() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.furtive
}

// SetFurtive sets whether the trinket is "furtive".
func (w *TrinketBase) SetFurtive(furtive bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.furtive = furtive
}

// HasFocus returns whether the trinket has focus.
func (w *TrinketBase) HasFocus() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.focused
}

// SetFocus attempts to give focus to this trinket.
func (w *TrinketBase) SetFocus() {
	w.setFocusInternal(true)
}

// SetFocusWithoutScroll sets focus without scrolling parent containers.
// Use this for mouse-initiated focus changes where the trinket is already
// visible (you can't click on something that isn't visible).
func (w *TrinketBase) SetFocusWithoutScroll() {
	w.setFocusInternal(false)
}

// SetFocusFromMouse attempts to give focus from a mouse click.
// This respects the furtive flag - furtive trinkets do not gain focus from mouse clicks.
// Use this in HandleMousePress for trinkets that may be furtive.
func (w *TrinketBase) SetFocusFromMouse() {
	if w.Furtive() {
		return
	}
	w.setFocusInternal(false) // No scroll needed for mouse clicks
}

// setFocusInternal is the common implementation for SetFocus variants.
func (w *TrinketBase) setFocusInternal(scrollIntoView bool) {
	w.mu.Lock()
	if w.focusPolicy == NoFocus {
		w.mu.Unlock()
		return
	}
	wasFocused := w.focused
	w.focused = true
	app := w.app
	parent := w.parent
	self := w.self // Get the outer trinket reference
	w.mu.Unlock()

	// Use self (the outer trinket) for focus management, falling back to w
	focusTrinket := Trinket(w)
	if self != nil {
		focusTrinket = self
	}

	if !wasFocused {
		// Call HandleFocusIn on the actual trinket (self) to get polymorphic behavior
		focusTrinket.HandleFocusIn()
		w.Update()
	}

	// Find a parent with a FocusManager and notify it
	// This ensures the old focused trinket gets ClearFocus() called
	w.notifyFocusManager(parent, focusTrinket)

	// Notify scroll containers to scroll this trinket into view
	// Skip this for mouse clicks where the trinket is already visible
	if scrollIntoView {
		w.notifyScrollIntoView(parent, focusTrinket)
	}

	// Notify application of focus change
	if app != nil {
		app.setFocusTrinket(focusTrinket)
	}
}

// notifyFocusManager walks up the parent chain to find a FocusManagerOwner
// and calls SetFocusedTrinket to properly transfer focus.
func (w *TrinketBase) notifyFocusManager(parent Container, focusTrinket Trinket) {
	current := parent
	for current != nil {
		if owner, ok := current.(FocusManagerOwner); ok {
			if fm := owner.FocusManager(); fm != nil {
				// Use internal method to avoid calling SetFocus again
				// The FocusManager will clear focus from the old trinket
				fm.setFocusedTrinketInternal(focusTrinket)
				return
			}
		}
		// Walk up to next parent
		if trinket, ok := current.(Trinket); ok {
			current = trinket.Parent()
		} else {
			break
		}
	}
}

// notifyScrollIntoView walks up the parent chain and calls ScrollChildIntoView
// on each ScrollIntoViewHandler to ensure the focused trinket is visible.
// This handles nested scroll containers by notifying each one in order.
func (w *TrinketBase) notifyScrollIntoView(parent Container, focusTrinket Trinket) {
	current := parent
	for current != nil {
		if handler, ok := current.(ScrollIntoViewHandler); ok {
			handler.ScrollChildIntoView(focusTrinket)
		}
		// Walk up to next parent
		if trinket, ok := current.(Trinket); ok {
			current = trinket.Parent()
		} else {
			break
		}
	}
}

// ScrollRectIntoView requests that parent scroll containers scroll to make
// a rectangle (in this trinket's local coordinates) visible. This is useful
// for trinkets like ListView that need to scroll parent containers when
// selection changes (e.g., during mouse sweep).
func (w *TrinketBase) ScrollRectIntoView(localRect UnitRect) {
	w.mu.RLock()
	parent := w.parent
	self := w.self
	w.mu.RUnlock()

	if parent == nil {
		return
	}

	// Use self to get correct bounds
	trinket := Trinket(w)
	if self != nil {
		trinket = self
	}

	// Walk up the parent chain, accumulating offsets and calling handlers
	bounds := trinket.Bounds()
	rect := UnitRect{
		X:      bounds.X + localRect.X,
		Y:      bounds.Y + localRect.Y,
		Width:  localRect.Width,
		Height: localRect.Height,
	}

	current := parent
	for current != nil {
		if handler, ok := current.(ScrollIntoViewHandler); ok {
			// Create a temporary proxy trinket with the calculated rect
			handler.ScrollChildIntoView(&scrollRectProxy{rect: rect, parent: current})
		}
		// Accumulate offset and walk up
		if trinket, ok := current.(Trinket); ok {
			parentBounds := trinket.Bounds()
			rect.X += parentBounds.X
			rect.Y += parentBounds.Y
			current = trinket.Parent()
		} else {
			break
		}
	}
}

// scrollRectProxy is a minimal Trinket implementation used by ScrollRectIntoView
// to pass rectangle information to ScrollChildIntoView.
type scrollRectProxy struct {
	rect   UnitRect
	parent Container
}

func (p *scrollRectProxy) Bounds() UnitRect    { return p.rect }
func (p *scrollRectProxy) Parent() Container   { return p.parent }
func (p *scrollRectProxy) Name() string        { return "" }
func (p *scrollRectProxy) SetName(string)      {}
func (p *scrollRectProxy) SetParent(Container) {}
func (p *scrollRectProxy) SetBounds(UnitRect)  {}
func (p *scrollRectProxy) Pos() UnitPoint      { return UnitPoint{X: p.rect.X, Y: p.rect.Y} }
func (p *scrollRectProxy) Size() UnitSize {
	return UnitSize{Width: p.rect.Width, Height: p.rect.Height}
}
func (p *scrollRectProxy) SetPos(UnitPoint)                {}
func (p *scrollRectProxy) SetSize(UnitSize)                {}
func (p *scrollRectProxy) MinimumSize() UnitSize           { return UnitSize{} }
func (p *scrollRectProxy) MaximumSize() UnitSize           { return UnitSize{} }
func (p *scrollRectProxy) SetMinimumSize(UnitSize)         {}
func (p *scrollRectProxy) SetMaximumSize(UnitSize)         {}
func (p *scrollRectProxy) SizeHint() UnitSize              { return p.Size() }
func (p *scrollRectProxy) SizePolicy() SizePolicyPair      { return SizePolicyPair{} }
func (p *scrollRectProxy) SetSizePolicy(SizePolicyPair)    {}
func (p *scrollRectProxy) Margins() UnitMargins            { return UnitMargins{} }
func (p *scrollRectProxy) SetMargins(UnitMargins)          {}
func (p *scrollRectProxy) IsVisible() bool                 { return true }
func (p *scrollRectProxy) SetVisible(bool)                 {}
func (p *scrollRectProxy) Show()                           {}
func (p *scrollRectProxy) Hide()                           {}
func (p *scrollRectProxy) IsEnabled() bool                 { return true }
func (p *scrollRectProxy) SetEnabled(bool)                 {}
func (p *scrollRectProxy) FocusPolicy() FocusPolicy        { return NoFocus }
func (p *scrollRectProxy) SetFocusPolicy(FocusPolicy)      {}
func (p *scrollRectProxy) HasFocus() bool                  { return false }
func (p *scrollRectProxy) SetFocus()                       {}
func (p *scrollRectProxy) SetFocusWithoutScroll()          {}
func (p *scrollRectProxy) ClearFocus()                     {}
func (p *scrollRectProxy) Furtive() bool                   { return false }
func (p *scrollRectProxy) SetFurtive(bool)                 {}
func (p *scrollRectProxy) Style() *style.CellStyle         { return nil }
func (p *scrollRectProxy) SetStyle(*style.CellStyle)       {}
func (p *scrollRectProxy) Theme() *style.Theme             { return nil }
func (p *scrollRectProxy) Scheme() style.SchemeID          { return style.SchemeInherit }
func (p *scrollRectProxy) SetScheme(style.SchemeID)        {}
func (p *scrollRectProxy) EffectiveScheme() style.SchemeID { return style.SchemeDefault }
func (p *scrollRectProxy) GetScheme() *style.Scheme {
	return style.GlobalSchemeRegistry().Get(style.SchemeDefault)
}
func (p *scrollRectProxy) Paint(*Painter)                            {}
func (p *scrollRectProxy) Update()                                   {}
func (p *scrollRectProxy) NeedsRepaint() bool                        { return false }
func (p *scrollRectProxy) HandleKeyPress(KeyPressEvent) bool         { return false }
func (p *scrollRectProxy) HandleKeyRelease(KeyReleaseEvent) bool     { return false }
func (p *scrollRectProxy) HandleMousePress(MousePressEvent) bool     { return false }
func (p *scrollRectProxy) HandleMouseRelease(MouseReleaseEvent) bool { return false }
func (p *scrollRectProxy) HandleMouseMove(MouseMoveEvent) bool       { return false }
func (p *scrollRectProxy) HandleMouseWheel(MouseWheelEvent) bool     { return false }
func (p *scrollRectProxy) HandleFocusIn()                            {}
func (p *scrollRectProxy) HandleFocusOut()                           {}
func (p *scrollRectProxy) HandleResize(oldSize, newSize UnitSize)    {}
func (p *scrollRectProxy) EffectiveBackgroundColor() style.Color     { return style.ColorDefault }
func (p *scrollRectProxy) SetBackgroundColor(*style.Color)           {}
func (p *scrollRectProxy) Application() *Application                 { return nil }

// ClearFocus removes focus from this trinket.
func (w *TrinketBase) ClearFocus() {
	w.mu.Lock()
	wasFocused := w.focused
	w.focused = false
	self := w.self
	w.mu.Unlock()

	if wasFocused {
		// Call HandleFocusOut on the actual trinket (self) to get polymorphic behavior
		if self != nil {
			self.HandleFocusOut()
		} else {
			w.HandleFocusOut()
		}
		w.Update()
	}
}

// Style returns the custom style.
func (w *TrinketBase) Style() *style.CellStyle {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.style
}

// SetStyle sets a custom style.
func (w *TrinketBase) SetStyle(s *style.CellStyle) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.style = s
	w.needsRepaint = true
}

// BackgroundColor returns the explicitly set background color, or nil if inherited.
func (w *TrinketBase) BackgroundColor() *style.Color {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.backgroundColor
}

// SetBackgroundColor sets an explicit background color.
// Pass nil to inherit from parent.
func (w *TrinketBase) SetBackgroundColor(c *style.Color) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.backgroundColor = c
	w.needsRepaint = true
}

// EffectiveBackgroundColor returns the background color to use for this trinket.
// It walks up the parent chain looking for either:
// - An explicit background color (set via SetBackgroundColor)
// - A scheme-derived background color (what the trinket paints based on its scheme)
// At each level, explicit color takes priority over scheme color.
// Returns the first non-nil background found, or style.ColorDefault if none.
func (w *TrinketBase) EffectiveBackgroundColor() style.Color {
	// First check this trinket's explicit background
	w.mu.RLock()
	if w.backgroundColor != nil {
		c := *w.backgroundColor
		w.mu.RUnlock()
		return c
	}
	parent := w.parent
	w.mu.RUnlock()

	// Walk up the parent chain
	for parent != nil {
		// Check explicit background color first (takes priority)
		if bgProvider, ok := parent.(interface{ BackgroundColor() *style.Color }); ok {
			if bg := bgProvider.BackgroundColor(); bg != nil {
				return *bg
			}
		}

		// Check scheme-derived background color
		if schemeBgProvider, ok := parent.(interface{ SchemeBackgroundColor() *style.Color }); ok {
			if bg := schemeBgProvider.SchemeBackgroundColor(); bg != nil {
				return *bg
			}
		}

		// Move to next parent
		if trinket, ok := parent.(Trinket); ok {
			parent = trinket.Parent()
		} else {
			break
		}
	}

	return style.ColorDefault
}

// Font returns the font explicitly set on this trinket, or nil if inheriting.
func (w *TrinketBase) Font() *Font {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.font
}

// SetFont sets an explicit font for this trinket.
// Set to nil to inherit from parent/window/desktop.
func (w *TrinketBase) SetFont(f *Font) {
	w.mu.Lock()
	w.font = f
	parent := w.parent
	w.mu.Unlock()

	// Trigger parent container's layout since font affects trinket size
	if parent != nil {
		parent.Layout()
	}
	w.Update()
}

// EffectiveFont returns the font to use for this trinket.
// It checks this trinket, then walks up the parent chain.
func (w *TrinketBase) EffectiveFont() *Font {
	return FindEffectiveFont(w.Self())
}

// CellMetricsOverride returns the grid metrics explicitly set on this
// trinket, or nil if inheriting.
func (w *TrinketBase) CellMetricsOverride() *CellMetrics {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.cellMetrics
}

// SetCellMetrics sets explicit grid metrics for this trinket/container.
// Set to nil to inherit from parent/window/desktop.
func (w *TrinketBase) SetCellMetrics(m *CellMetrics) {
	w.mu.Lock()
	w.cellMetrics = m
	parent := w.parent
	w.mu.Unlock()

	// Re-denomination changes what every stored unit value inside this
	// container means: relayout the trinket's own subtree so geometry is
	// re-expressed in the new currency immediately (not on next resize).
	if self, ok := w.Self().(Container); ok && self != nil {
		self.Layout()
	}

	// Outer-currency sizes are invariant under re-denomination, but let
	// the parent refresh in case it caches child geometry.
	if parent != nil {
		parent.Layout()
	}
	w.Update()
}

// EffectiveCellMetrics returns the grid metrics to use for this trinket.
// It checks this trinket, then walks up the parent chain, falling back
// to DefaultCellMetrics.
func (w *TrinketBase) EffectiveCellMetrics() CellMetrics {
	return FindEffectiveCellMetrics(w.Self())
}

// Theme returns the theme for this trinket.
func (w *TrinketBase) Theme() *style.Theme {
	w.mu.RLock()
	app := w.app
	w.mu.RUnlock()

	if app != nil {
		return app.Theme()
	}
	return style.DefaultTheme()
}

// Scheme returns the scheme ID for this trinket (-1 = inherit from container).
func (w *TrinketBase) Scheme() style.SchemeID {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.scheme
}

// SetScheme sets the scheme ID for this trinket (-1 = inherit from container).
func (w *TrinketBase) SetScheme(id style.SchemeID) {
	w.mu.Lock()
	w.scheme = id
	w.mu.Unlock()
	w.Update()
}

// EffectiveScheme returns the resolved scheme ID by walking up the container chain.
// If this trinket's scheme is -1 (inherit), it walks up to find a parent with a
// defined scheme. Returns SchemeDefault (0) if no scheme is found.
func (w *TrinketBase) EffectiveScheme() style.SchemeID {
	w.mu.RLock()
	scheme := w.scheme
	parent := w.parent
	w.mu.RUnlock()

	// If this trinket has an explicit scheme, use it
	if scheme != style.SchemeInherit {
		// Validate that the scheme exists; if not, use default
		if style.GlobalSchemeRegistry().Has(scheme) {
			return scheme
		}
		return style.SchemeDefault
	}

	// Walk up the parent chain to find an inherited scheme
	current := parent
	for current != nil {
		if trinket, ok := current.(Trinket); ok {
			parentScheme := trinket.Scheme()
			if parentScheme != style.SchemeInherit {
				// Validate that the scheme exists
				if style.GlobalSchemeRegistry().Has(parentScheme) {
					return parentScheme
				}
				return style.SchemeDefault
			}
			current = trinket.Parent()
		} else {
			break
		}
	}

	// No scheme found in chain, use default
	return style.SchemeDefault
}

// GetScheme returns the resolved Scheme object for this trinket.
func (w *TrinketBase) GetScheme() *style.Scheme {
	return style.GlobalSchemeRegistry().Get(w.EffectiveScheme())
}

// Paint is a no-op (override in subclasses).
func (w *TrinketBase) Paint(p *Painter) {
	// Default: do nothing
}

// repaintHook is fired by every Update() so the host can wake its render
// loop. The desktop sets it to flag that a frame is needed; without a hook set
// (the default, e.g. in tests) Update() just records needsRepaint as before.
var repaintHook struct {
	sync.RWMutex
	fn func()
}

// SetRepaintHook installs a callback invoked on every Update() (pass nil to
// clear). The host uses it to coalesce "a repaint is needed" so its frame loop
// can skip work when nothing changed. Safe to call from any goroutine.
func SetRepaintHook(fn func()) {
	repaintHook.Lock()
	repaintHook.fn = fn
	repaintHook.Unlock()
}

func fireRepaintHook() {
	repaintHook.RLock()
	fn := repaintHook.fn
	repaintHook.RUnlock()
	if fn != nil {
		fn()
	}
}

// Update marks the trinket as needing repaint.
func (w *TrinketBase) Update() {
	w.mu.Lock()
	w.needsRepaint = true
	app := w.app
	w.mu.Unlock()

	if app != nil {
		app.requestRepaint()
	}
	fireRepaintHook()
}

// NeedsRepaint returns whether the trinket needs repainting.
func (w *TrinketBase) NeedsRepaint() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.needsRepaint
}

// HandleKeyPress handles key events (override in subclasses).
func (w *TrinketBase) HandleKeyPress(event KeyPressEvent) bool {
	return false
}

// HandleKeyRelease handles key release (override in subclasses).
func (w *TrinketBase) HandleKeyRelease(event KeyReleaseEvent) bool {
	return false
}

// HandleMousePress handles mouse press (override in subclasses).
func (w *TrinketBase) HandleMousePress(event MousePressEvent) bool {
	return false
}

// HandleMouseRelease handles mouse release (override in subclasses).
func (w *TrinketBase) HandleMouseRelease(event MouseReleaseEvent) bool {
	return false
}

// HandleMouseMove handles mouse movement (override in subclasses).
func (w *TrinketBase) HandleMouseMove(event MouseMoveEvent) bool {
	return false
}

// HandleMouseWheel handles mouse wheel (override in subclasses).
func (w *TrinketBase) HandleMouseWheel(event MouseWheelEvent) bool {
	return false
}

// HandleFocusIn is called when focus is gained (override in subclasses).
func (w *TrinketBase) HandleFocusIn() {
}

// HandleFocusOut is called when focus is lost (override in subclasses).
func (w *TrinketBase) HandleFocusOut() {
}

// HandleResize is called when size changes (override in subclasses).
func (w *TrinketBase) HandleResize(oldSize, newSize UnitSize) {
}

// Application returns the application.
func (w *TrinketBase) Application() *Application {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.app
}

// SetApplication sets the application (called internally).
func (w *TrinketBase) SetApplication(app *Application) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.app = app
}

// PopupController returns the popup controller for this trinket.
// Returns nil if no popup controller has been set.
func (w *TrinketBase) PopupController() PopupController {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.popupController
}

// SetPopupController sets the popup controller for this trinket.
// This is typically called by the window manager when adding windows.
func (w *TrinketBase) SetPopupController(pc PopupController) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.popupController = pc
}

// setFocusTrinket updates the focused trinket, clearing focus from the previous one.
func (a *Application) setFocusTrinket(w interface{ ClearFocus() }) {
	if a == nil {
		return
	}

	a.mu.Lock()
	oldFocus := a.focusTrinket
	// Type assert to Trinket to store it
	if trinket, ok := w.(Trinket); ok {
		a.focusTrinket = trinket
	}
	a.mu.Unlock()

	// Clear focus from the old trinket (if different)
	if oldFocus != nil && oldFocus != w {
		oldFocus.ClearFocus()
	}
}

// requestRepaint is a forward reference for Application.
// DEPRECATED: This is legacy code from Application-centric mode.
// Trinkets should use Update() which marks them for repaint, and the Desktop
// event loop handles actual repainting.
func (a *Application) requestRepaint() {
	// No-op - Desktop now handles repainting
}

// Theme returns the application theme.
func (a *Application) Theme() *style.Theme {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.theme != nil {
		return a.theme
	}
	return style.DefaultTheme()
}
