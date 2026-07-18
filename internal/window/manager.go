// Package window provides window management for the editor.
package window

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/textwidth"
)

// unixWisdom is sample context text used to populate each window's Context
// field on spawn, so the modebar shows varying placeholder text for now.
var unixWisdom = []string{
	"Do one thing and do it well.",
	"Everything is a file.",
	"Silence is golden.",
	"Smart data structures beat clever code.",
	"When in doubt, use brute force.",
}

// Position represents a cursor position in document coordinates.
type Position struct {
	Line int
	Rune int
}

// FindState holds a window's most recent find-command parameters;
// find_next continues from it. Options holds the raw option letters and
// digits (i=ignore case, b=backwards, a=all buffers, r=replace, x=standard
// regex syntax, y=JOE regex syntax, v=verbose log, nnn=Nth occurrence /
// replacement count limit).
type FindState struct {
	Term        string
	Options     string
	Replacement string
	Replace     bool
}

// RepeatState arms the next keybound command to run inside a PawScript
// repeat(...) a fixed number of times. repeat_next sets it (from an argument or
// a prompt) on the window; the editor's command dispatcher consumes it when the
// next command runs, then clears it. Pending is false when nothing is armed.
type RepeatState struct {
	Pending bool
	Count   int
}

// ViewState holds the viewport and display state for a window.
type ViewState struct {
	ViewOffsetX     int
	ViewOffsetY     int
	ShowLineNumbers bool
	ShowInvisibles  bool
	// ShowBidi renders a one-column direction marker at the leading edge of
	// every directional fragment (except a line-initial fragment in the
	// natural direction): "<" entering an RTL fragment, ">" entering LTR.
	// Explicit direction-control characters render as their own marker.
	ShowBidi bool
	// ShowRuler renders a column ruler on the window's top line, reducing the
	// content area by one row. Ignored when the window is only one line tall.
	ShowRuler bool
	TabSize   int
	// Direction overrides the editor's base text direction for this window:
	// "ltr", "rtl", or "" to inherit the [general] direction option. Prompt
	// windows are pinned "ltr" at creation.
	Direction string
}

// MarkOptionOverridden records that a per-window option was set explicitly on
// this window, so a grammar options overlay leaves it alone.
func (w *Window) MarkOptionOverridden(name string) {
	if w.overriddenOptions == nil {
		w.overriddenOptions = make(map[string]bool)
	}
	w.overriddenOptions[name] = true
}

// IsOptionOverridden reports whether a per-window option was set explicitly on
// this window (and so must not be overwritten by a grammar overlay).
func (w *Window) IsOptionOverridden(name string) bool {
	return w.overriddenOptions[name]
}

// AppliedOptionSig returns the overlay signature (class/grammar/type) whose
// resolved options were last applied to this window.
func (w *Window) AppliedOptionSig() string { return w.appliedOptionSig }

// SetAppliedOptionSig records the overlay signature now reflected in this
// window's ViewState.
func (w *Window) SetAppliedOptionSig(sig string) { w.appliedOptionSig = sig }

// WindowType represents the type of window buffer.
type WindowType int

const (
	MainBuffer   WindowType = iota // Editable content area
	WorkBuffer                     // Read-only information display
	PromptBuffer                   // Single-line input at screen bottom
)

// Name returns the buffer type's name as used in color/config lookups
// ([colors.<name>] sections).
func (t WindowType) Name() string {
	switch t {
	case MainBuffer:
		return "main"
	case WorkBuffer:
		return "work"
	case PromptBuffer:
		return "prompt"
	}
	return ""
}

// DockPosition represents where a window is docked.
type DockPosition int

const (
	DockNone   DockPosition = iota // Not docked (main editing area)
	DockTop                        // Docked to top (status/ruler)
	DockBottom                     // Docked to bottom (prompts)
)

// cursorRingSize is the number of remembered edit positions per window.
const cursorRingSize = 10

// Window represents an editor window with its buffer and display state.
type Window struct {
	ID       string
	Type     WindowType
	Class    string
	Dock     DockPosition
	Priority int
	Visible  bool

	// Seq is a monotonically increasing creation sequence number, unique
	// across all windows: each new window gets the previous number +1.
	Seq int64

	// Context is a per-window descriptor surfaced by the modebar
	// when there is no active key-sequence autocompletion to display.
	// The editor overwrites it with the syntax outline breadcrumb (the
	// enclosing function/section chain) when one is available.
	Context string

	// SpawnContext is the placeholder Context assigned at creation, restored
	// when no outline breadcrumb applies at the caret.
	SpawnContext string

	// SpawnedAt records when the window was created, used to expire transient
	// notification/error/warning windows.
	SpawnedAt time.Time

	// ParentMain is the main-buffer window that (transitively) spawned this
	// window. Set at creation for prompt buffers: inherited from the focused
	// window's ParentMain when present, else the last main buffer. Cleared if
	// the parent window is removed.
	ParentMain *Window

	Buffer *buffer.Buffer

	ViewState ViewState

	// overriddenOptions records per-window options the user (or a launch-time
	// per-file switch) set explicitly via set_option, so a grammar-driven
	// options overlay does not clobber a deliberate choice. Nil until first use.
	overriddenOptions map[string]bool

	// appliedOptionSig is the overlay signature (class/grammar/type) whose
	// resolved options were last written into this window's ViewState. "" (the
	// initial value) matches a plain window with no grammar/class/type overlay,
	// so such windows are left untouched.
	appliedOptionSig string

	// viewportAnchor tracks the first visible document line as a garland
	// cursor, so the viewport stays pinned to the same logical line when the
	// buffer is edited above it (see SetViewTop/RefreshViewTop). Created when
	// the window is bound to a buffer, released when the window is removed.
	viewportAnchor *buffer.Anchor

	// Caret is the window's own edit cursor — a garland cursor that garland
	// maintains across every edit. Editing goes through it, and it slides with
	// edits made through another window on the same buffer, so CursorPos (its
	// cached line/rune) can be refreshed from it and never goes stale. It is
	// parked at CursorPos on focus-out and read back on focus-in (see
	// SetFocus). Created when the window is bound to a buffer, released on
	// removal.
	Caret *buffer.Caret

	// Cursor ring: a fixed set of garland cursors recording where the caret has
	// recently edited, so go_pos_prior/go_pos_next can walk the caret back and
	// forth through its own history. Each entry is just a byte-tracking cursor
	// that slides with edits like any other. lastEditPoint holds the position of
	// the most recent caret-area edit; hasMoved records whether the caret has
	// deliberately moved away from it since. On the next edit, if the caret has
	// moved, the old lastEditPoint is copied into the ring before lastEditPoint
	// is advanced to the caret — so the ring accumulates the trail of distinct
	// edit sites. The ring is circular: once all cursorRingSize slots are used,
	// the oldest is overwritten. Created/released alongside Caret.
	lastEditPoint *buffer.Anchor
	hasMoved      bool
	cursorRing    [cursorRingSize]*buffer.Anchor
	ringFirst     int // slot of the oldest live entry
	ringCount     int // number of live entries (0..cursorRingSize)
	ringNav       int // navigation index into history; -1 = not navigating

	// Find is the window's own find state (see FindState). An all-buffers
	// search ("a" option) lives on the editor instead.
	Find FindState

	// findOrigin is a temporary garland cursor marking where the current
	// search began (set when a find commits); it slides with edits like any
	// other cursor. findWrapped records that the search has wrapped past the
	// end of the buffer since then — when a match then crosses back over the
	// origin, the editor announces that the search has looped. Released in
	// RemoveWindow.
	findOrigin  *buffer.Anchor
	findWrapped bool

	// Repeat arms the next keybound command to run inside a repeat(...) N times
	// (see RepeatState). Set by repeat_next, consumed by the command dispatcher.
	Repeat RepeatState

	// MatchHighlight gates the renderer's use of the transient
	// _match_begin/_match_end marks: mark lookups walk the rope, so the
	// renderer only queries them while a find/replace prompt has one active.
	MatchHighlight bool

	MinHeight int
	MaxHeight int
	Height    int

	IdealVisualColumn       int
	GhostCursorVisualColumn int
	HasGhostCursor          bool

	// Window chrome slots are named by READING side, like the inner and
	// outer margins of a book page: Inner is the reading-start side (left in
	// an LTR window, right in RTL), Outer is the opposite edge. The renderer
	// maps them to physical sides per the window's effective direction.
	MessageTopInner     string
	MessageTopCenter    string
	MessageTopOuter     string
	MessageBottomInner  string
	MessageBottomCenter string
	MessageBottomOuter  string

	// MarginInner/MarginOuter reserve columns on the reading-start/opposite
	// side. RowMessages paint inside the inner margin (prompt labels).
	MarginInner int
	MarginOuter int
	RowMessages []string

	ContentX      int
	ContentY      int
	ContentWidth  int
	ContentHeight int
	LineNumWidth  int

	// Legacy callback for simple prompts (2 params)
	Callback func(input string, accepted bool)
	// PromptCallback for full prompt support (3 params matching TypeScript)
	// Parameters: accepted, bufferContent (line 0), cursorLineText (current cursor line)
	PromptCallback func(accepted bool, bufferContent, cursorLineText string)
	CustomRenderer string

	// CompletionCallback, when set, handles the `completion` command for this
	// window (e.g. filename completion on a filename prompt). It returns true
	// when it handled the completion; false lets the command's fallback run
	// (the demo binds completion|insert '\t', so a plain buffer types a tab).
	CompletionCallback func() bool

	// Tag is a secondary label (beyond Class) grouping windows that should
	// replace rather than stack — e.g. the filename-completion transient tags
	// itself so a fresh completion first removes the previous one.
	Tag string
}

// CursorPos returns the window's caret position, read straight from its
// garland caret cursor — the single source of truth. Garland maintains the
// cursor across every edit (including edits made through another window on the
// same buffer), so this never goes stale and there is no cached copy to
// reconcile. The read is O(1) amortized (garland resolves the line/column
// lazily at most once per edit).
func (w *Window) CursorPos() Position {
	if w.Caret == nil {
		return Position{}
	}
	l, r := w.Caret.Position()
	return Position{Line: l, Rune: r}
}

// CaretByte returns the caret's absolute byte offset in the buffer (0 at the
// start), for readouts like the modebar's %ABSBYTE%.
func (w *Window) CaretByte() int64 {
	if w.Caret == nil {
		return 0
	}
	return w.Caret.BytePos()
}

// SetCursorPos moves the caret to p. The line is clamped into the buffer's
// range first: garland rejects a seek to a nonexistent line (leaving the
// caret put), so callers that compute an out-of-range target — page up/down
// past an edge, say — rely on this clamp to land at the edge instead.
func (w *Window) SetCursorPos(p Position) {
	if w.Caret != nil {
		line := w.clampLine(p.Line)
		w.Caret.Seek(line, w.clampRune(line, p.Rune))
	}
}

// SetCursorLine moves the caret to a line (clamped into range), keeping its
// rune column (itself clamped to the target line's length).
func (w *Window) SetCursorLine(line int) {
	if w.Caret != nil {
		_, r := w.Caret.Position()
		line = w.clampLine(line)
		w.Caret.Seek(line, w.clampRune(line, r))
	}
}

// clampLine clamps a line index into [0, lineCount-1].
func (w *Window) clampLine(line int) int {
	if line < 0 {
		return 0
	}
	if w.Buffer != nil {
		if max := w.Buffer.GetLineCount() - 1; line > max {
			return max
		}
	}
	return line
}

// clampRune clamps a rune column into [0, the given line's rune length]. The
// caret must never seek past a line's end: garland's SeekLine neither clamps nor
// rejects an out-of-range rune-within-line, which leaves the cursor's byte
// position and its reported line/rune disagreeing about which line it is on.
func (w *Window) clampRune(line, runePos int) int {
	if runePos < 0 {
		return 0
	}
	if w.Buffer != nil {
		if max := w.Buffer.LineRuneLen(line); runePos > max {
			return max
		}
	}
	return runePos
}

// SetCursorRune moves the caret to a rune column (clamped to the current line's
// length), keeping its line.
func (w *Window) SetCursorRune(runePos int) {
	if w.Caret != nil {
		l, _ := w.Caret.Position()
		w.Caret.Seek(l, w.clampRune(l, runePos))
	}
}

// TrackEdit records a caret-area edit in the cursor ring. If the caret has
// moved away from the last edit point since that point was set, the old edit
// point is first pushed onto the ring (so a new distinct edit site starts a new
// history entry). The last edit point is then advanced to the caret, and the
// caret is — by definition — back on it, so hasMoved clears and any in-progress
// ring navigation is abandoned. A no-op on windows without a ring (no buffer).
func (w *Window) TrackEdit() {
	if w.lastEditPoint == nil || w.Caret == nil {
		return
	}
	if w.hasMoved {
		slot := (w.ringFirst + w.ringCount) % cursorRingSize
		w.cursorRing[slot].SeekByte(w.lastEditPoint.BytePos())
		if w.ringCount < cursorRingSize {
			w.ringCount++
		} else {
			// Ring full: the write above overwrote the oldest, so advance first.
			w.ringFirst = (w.ringFirst + 1) % cursorRingSize
		}
	}
	w.lastEditPoint.SeekByte(w.Caret.BytePos())
	w.hasMoved = false
	w.ringNav = -1
}

// HasMovedSinceEdit reports whether the caret has deliberately moved away from
// the last edit point since it was set (see TrackEdit/TrackMove). The kill
// ring uses this to decide whether consecutive deletes belong to the same
// edit and should share a kill entry.
func (w *Window) HasMovedSinceEdit() bool {
	return w.hasMoved
}

// SetFindOrigin parks the search-origin cursor at the caret (where a newly
// committed search begins) and clears the wrapped flag. The origin is a live
// garland cursor, so it stays on its logical position across edits.
func (w *Window) SetFindOrigin() {
	if w.Buffer == nil || w.Caret == nil {
		return
	}
	if w.findOrigin == nil {
		w.findOrigin = w.Buffer.NewAnchor()
	}
	w.findOrigin.SeekByte(w.Caret.BytePos())
	w.findWrapped = false
}

// FindOriginByte returns the byte position of the search origin, if one has
// been set on this window.
func (w *Window) FindOriginByte() (int64, bool) {
	if w.findOrigin == nil {
		return 0, false
	}
	return w.findOrigin.BytePos(), true
}

// FindWrapped reports whether the current search has wrapped past the end of
// the buffer since its origin was set.
func (w *Window) FindWrapped() bool { return w.findWrapped }

// SetFindWrapped records or clears the wrapped state.
func (w *Window) SetFindWrapped(v bool) { w.findWrapped = v }

// TrackMove records a deliberate caret movement: the caret has moved unless it
// has landed back on the last edit point. Ends any in-progress ring navigation.
func (w *Window) TrackMove() {
	if w.lastEditPoint == nil || w.Caret == nil {
		return
	}
	w.hasMoved = w.Caret.BytePos() != w.lastEditPoint.BytePos()
	w.ringNav = -1
}

// ringAnchorAt returns the i-th navigable history position, ordered oldest to
// newest: 0..ringCount-1 index the ring entries, and ringCount is the live
// lastEditPoint (newer than every ring entry).
func (w *Window) ringAnchorAt(i int) *buffer.Anchor {
	if i >= w.ringCount {
		return w.lastEditPoint
	}
	return w.cursorRing[(w.ringFirst+i)%cursorRingSize]
}

// CursorRingPrior returns the byte position one step older in the caret's edit
// history, advancing the navigation index, or ok=false if there is nowhere
// older to go. When navigation has not started (ringNav < 0) the first step
// depends on where the caret sits: if it is already on the last edit point, the
// prior position is the newest ring entry; otherwise the prior position is the
// last edit point itself (returning the caret to its most recent edit).
func (w *Window) CursorRingPrior() (int64, bool) {
	if w.lastEditPoint == nil {
		return 0, false
	}
	if w.ringNav < 0 {
		if w.Caret != nil && w.Caret.BytePos() == w.lastEditPoint.BytePos() {
			if w.ringCount == 0 {
				return 0, false // caret on the sole edit point; no history behind
			}
			w.ringNav = w.ringCount - 1
		} else {
			w.ringNav = w.ringCount // step back onto the last edit point
		}
	} else if w.ringNav > 0 {
		w.ringNav--
	} else {
		return 0, false // already at the oldest entry
	}
	return w.ringAnchorAt(w.ringNav).BytePos(), true
}

// CursorRingNext returns the byte position one step newer in the caret's edit
// history, or ok=false if navigation is not in progress or already at the
// newest (the last edit point).
func (w *Window) CursorRingNext() (int64, bool) {
	if w.lastEditPoint == nil || w.ringNav < 0 || w.ringNav >= w.ringCount {
		return 0, false
	}
	w.ringNav++
	return w.ringAnchorAt(w.ringNav).BytePos(), true
}

// SeekCaretByte moves the caret to a byte offset (used by ring navigation to
// jump to a remembered position).
func (w *Window) SeekCaretByte(pos int64) {
	if w.Caret != nil {
		w.Caret.SeekByte(pos)
	}
}

// SetViewTop sets the first visible document line, updating both the painting
// offset and the sliding viewport anchor so the view stays pinned to that
// logical line when the buffer is edited above it.
func (w *Window) SetViewTop(line int) {
	if line < 0 {
		line = 0
	}
	w.ViewState.ViewOffsetY = line
	if w.viewportAnchor != nil {
		w.viewportAnchor.SeekLine(line)
	}
}

// RefreshViewTop re-derives the painting offset from the viewport anchor,
// absorbing any slide the anchor took from edits since the last paint. A no-op
// when there is no anchor (the offset stays as last set).
func (w *Window) RefreshViewTop() {
	if w.viewportAnchor != nil {
		w.ViewState.ViewOffsetY = w.viewportAnchor.Line()
	}
}

// EventType represents the type of window event.
type EventType int

const (
	EventWindowCreated EventType = iota
	EventWindowRemoved
	EventWindowUpdated
	EventFocusChanged
	EventCursorPositioned
	EventGhostCursorSet
	EventGhostCursorCleared
	EventStatPeekChanged
	EventPromptPeekChanged
)

// Event represents a window manager event.
type Event struct {
	Type     EventType
	WindowID string
	OldValue interface{}
	NewValue interface{}
}

// EventHandler is a callback for window events.
type EventHandler func(event Event)

// Manager handles window lifecycle and focus management.
type Manager struct {
	mu sync.RWMutex

	windows         map[string]*Window
	focusedWindowID string

	// Track last main buffer for system-wide access
	lastMainBufferWindow *Window

	// Track the last focused non-docked (main-area) window, regardless of its
	// buffer type. Currently the only non-docked window painted is this one
	// (see the main layout). TODO: support additional tiling modes (split
	// panes, side-by-side, etc.) instead of only showing the last-focused one.
	lastNormalWindow *Window

	// Peek offsets for viewing hidden windows
	StatPeek   int // For top-docked windows
	PromptPeek int // For bottom-docked windows

	// Window type counters for auto-naming
	mainBufferCount   int
	workBufferCount   int
	promptBufferCount int

	// Monotonic creation sequence counter (see Window.Seq)
	seqCounter int64

	// Event handlers
	eventHandlers map[EventType][]EventHandler
}

// NewManager creates a new window manager.
func NewManager() *Manager {
	return &Manager{
		windows:       make(map[string]*Window),
		eventHandlers: make(map[EventType][]EventHandler),
	}
}

// On registers an event handler.
func (m *Manager) On(eventType EventType, handler EventHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventHandlers[eventType] = append(m.eventHandlers[eventType], handler)
}

// emit sends an event to all registered handlers.
func (m *Manager) emit(event Event) {
	m.mu.RLock()
	handlers := m.eventHandlers[event.Type]
	m.mu.RUnlock()

	for _, handler := range handlers {
		handler(event)
	}
}

// WindowOptions configures a new window.
type WindowOptions struct {
	ID              string
	Type            WindowType
	Class           string
	Tag             string
	Buffer          *buffer.Buffer
	Dock            DockPosition
	Priority        int
	Visible         bool
	MinHeight       int
	MaxHeight       int
	Height          int
	ShowLineNumbers bool
	ShowInvisibles  bool
	ShowBidi        bool
	ShowRuler       bool
	TabSize         int
	SetFocus        bool
	CustomRenderer  string

	// Message bars
	MessageTopInner     string
	MessageTopCenter    string
	MessageTopOuter     string
	MessageBottomInner  string
	MessageBottomCenter string
	MessageBottomOuter  string

	// Margins
	MarginInner int
	MarginOuter int
	RowMessages []string
}

// CreateWindow creates a new window with the given options.
func (m *Manager) CreateWindow(opts WindowOptions) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate ID if not provided
	id := opts.ID
	if id == "" {
		switch opts.Type {
		case MainBuffer:
			m.mainBufferCount++
			id = fmt.Sprintf("main_%d", m.mainBufferCount)
		case WorkBuffer:
			m.workBufferCount++
			id = fmt.Sprintf("work_%d", m.workBufferCount)
		case PromptBuffer:
			m.promptBufferCount++
			id = fmt.Sprintf("prompt_%d", m.promptBufferCount)
		default:
			id = fmt.Sprintf("window_%d", len(m.windows)+1)
		}
	}

	// Set defaults
	visible := opts.Visible
	if !opts.Visible && opts.ID == "" {
		visible = true // Default to visible for new windows
	}

	minHeight := opts.MinHeight
	if minHeight == 0 {
		minHeight = 1
	}

	// Default desired height: when no explicit height is given, prefer the
	// maximum (layout negotiation shrinks toward MinHeight under pressure);
	// with no maximum either, fall back to the minimum.
	height := opts.Height
	if height == 0 {
		if opts.MaxHeight > 0 {
			height = opts.MaxHeight
		} else {
			height = minHeight
		}
	}

	m.seqCounter++
	wisdom := unixWisdom[rand.Intn(len(unixWisdom))]
	w := &Window{
		ID:           id,
		Seq:          m.seqCounter,
		Type:         opts.Type,
		Class:        opts.Class,
		Tag:          opts.Tag,
		Dock:         opts.Dock,
		Priority:     opts.Priority,
		Visible:      visible,
		Context:      wisdom,
		SpawnContext: wisdom,
		SpawnedAt:    time.Now(),
		Buffer:       opts.Buffer,
		ViewState: ViewState{
			ViewOffsetX:     0,
			ViewOffsetY:     0,
			ShowLineNumbers: opts.ShowLineNumbers,
			ShowInvisibles:  opts.ShowInvisibles,
			ShowBidi:        opts.ShowBidi,
			ShowRuler:       opts.ShowRuler,
			TabSize:         opts.TabSize,
		},
		MinHeight:           minHeight,
		MaxHeight:           opts.MaxHeight,
		Height:              height,
		MessageTopInner:     opts.MessageTopInner,
		MessageTopCenter:    opts.MessageTopCenter,
		MessageTopOuter:     opts.MessageTopOuter,
		MessageBottomInner:  opts.MessageBottomInner,
		MessageBottomCenter: opts.MessageBottomCenter,
		MessageBottomOuter:  opts.MessageBottomOuter,
		MarginInner:         opts.MarginInner,
		MarginOuter:         opts.MarginOuter,
		RowMessages:         opts.RowMessages,
		CustomRenderer:      opts.CustomRenderer,
	}

	// Calculate line number width if showing line numbers
	if w.ViewState.ShowLineNumbers && w.Buffer != nil {
		lineCount := w.Buffer.GetLineCount()
		if lineCount < 10 {
			lineCount = 10
		}
		w.LineNumWidth = len(fmt.Sprintf("%d", lineCount)) + 1
	}

	// Prompt buffers always edit left-to-right regardless of the editor's
	// base direction option.
	if opts.Type == PromptBuffer {
		w.ViewState.Direction = "ltr"
	}

	// Track which main buffer spawned a new prompt buffer: inherit the focused
	// window's ParentMain when it has one (e.g. a prompt spawning another
	// prompt), otherwise the last main buffer.
	if opts.Type == PromptBuffer {
		if focused := m.windows[m.focusedWindowID]; focused != nil && focused.ParentMain != nil {
			w.ParentMain = focused.ParentMain
		} else if m.lastMainBufferWindow != nil {
			if _, exists := m.windows[m.lastMainBufferWindow.ID]; exists {
				w.ParentMain = m.lastMainBufferWindow
			}
		}
	}

	// Give the window its own edit cursor and viewport anchor on its buffer, so
	// the caret and the top-of-view line each slide with edits. One set per
	// window, so two windows sharing a buffer edit and scroll independently;
	// released in RemoveWindow.
	w.ringNav = -1
	if w.Buffer != nil {
		w.Caret = w.Buffer.NewCaret()
		w.viewportAnchor = w.Buffer.NewAnchor()
		// Every window with a buffer gets its own cursor ring — a caret moves and
		// edits in a prompt buffer just as in a document, so its edit history is
		// worth tracking too. Released in RemoveWindow.
		w.lastEditPoint = w.Buffer.NewAnchor()
		for i := range w.cursorRing {
			w.cursorRing[i] = w.Buffer.NewAnchor()
		}
	}

	m.windows[id] = w

	// Set focus if requested or if this is the first window
	if opts.SetFocus || m.focusedWindowID == "" {
		m.focusedWindowID = id
	}

	// Update main buffer tracking. Only a window that actually took focus
	// becomes the current one — a background window (e.g. the verbose log)
	// must not steal the painted main area or the modebar.
	tookFocus := m.focusedWindowID == id
	if opts.Type == MainBuffer && tookFocus {
		m.lastMainBufferWindow = w
	} else if tookFocus && opts.Type == PromptBuffer && w.ParentMain != nil {
		// A focused prompt keeps its spawning main as the last main buffer
		// (and the painted main-area window) — see SetFocus.
		m.lastMainBufferWindow = w.ParentMain
		if w.ParentMain.Dock == DockNone {
			m.lastNormalWindow = w.ParentMain
		}
	}
	// Update non-docked (main-area) window tracking, regardless of buffer type.
	if opts.Dock == DockNone && tookFocus {
		m.lastNormalWindow = w
	}

	// Emit event (outside lock)
	go m.emit(Event{
		Type:     EventWindowCreated,
		WindowID: id,
		NewValue: w,
	})

	return id
}

// GetWindow returns a window by ID.
func (m *Manager) GetWindow(id string) *Window {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.windows[id]
}

// GetFocusedWindow returns the currently focused window.
func (m *Manager) GetFocusedWindow() *Window {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.focusedWindowID == "" {
		return nil
	}
	return m.windows[m.focusedWindowID]
}

// SetFocus sets focus to a window.
func (m *Manager) SetFocus(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	w, exists := m.windows[id]
	if !exists {
		return false
	}

	// Work buffers cannot receive focus
	if w.Type == WorkBuffer {
		return false
	}

	oldFocusID := m.focusedWindowID
	m.focusedWindowID = id

	// Update main buffer tracking
	if w.Type == MainBuffer {
		m.lastMainBufferWindow = w
	} else if w.Type == PromptBuffer && w.ParentMain != nil {
		// Focusing a prompt restores its spawning main as the last main
		// buffer — and as the painted main-area window — so the parent
		// becomes visible behind the prompt.
		m.lastMainBufferWindow = w.ParentMain
		if w.ParentMain.Dock == DockNone {
			m.lastNormalWindow = w.ParentMain
		}
	}
	// Update non-docked (main-area) window tracking, regardless of buffer type.
	if w.Dock == DockNone {
		m.lastNormalWindow = w
	}

	go m.emit(Event{
		Type:     EventFocusChanged,
		WindowID: id,
		OldValue: oldFocusID,
		NewValue: id,
	})

	return true
}

// RemoveWindow removes a window. If the removed window was focused, the focus
// handoff is routed through SetFocus so tracking, parent-main restoration, and
// focus events behave like any other focus change.
func (m *Manager) RemoveWindow(id string) bool {
	m.mu.Lock()

	w, exists := m.windows[id]
	if !exists {
		m.mu.Unlock()
		return false
	}

	// Clear last main buffer tracking if this was it
	if m.lastMainBufferWindow != nil && m.lastMainBufferWindow.ID == id {
		m.lastMainBufferWindow = nil
	}
	// Clear last non-docked window tracking if this was it
	if m.lastNormalWindow != nil && m.lastNormalWindow.ID == id {
		m.lastNormalWindow = nil
	}
	// Clear any ParentMain references to the removed window
	for _, other := range m.windows {
		if other.ParentMain == w {
			other.ParentMain = nil
		}
	}

	// Release the window's cursors so garland stops adjusting them on edits.
	if w.viewportAnchor != nil {
		w.viewportAnchor.Release()
		w.viewportAnchor = nil
	}
	if w.Caret != nil {
		w.Caret.Release()
		w.Caret = nil
	}
	if w.lastEditPoint != nil {
		w.lastEditPoint.Release()
		w.lastEditPoint = nil
	}
	if w.findOrigin != nil {
		w.findOrigin.Release()
		w.findOrigin = nil
	}
	for i := range w.cursorRing {
		if w.cursorRing[i] != nil {
			w.cursorRing[i].Release()
			w.cursorRing[i] = nil
		}
	}

	delete(m.windows, id)

	wasFocused := m.focusedWindowID == id
	nextFocus := ""
	if wasFocused {
		m.focusedWindowID = ""
		nextFocus = m.removalFocusTargetLocked(w)
	}

	m.mu.Unlock()

	if nextFocus != "" {
		m.SetFocus(nextFocus)
	}

	go m.emit(Event{
		Type:     EventWindowRemoved,
		WindowID: id,
		OldValue: w,
		NewValue: wasFocused,
	})

	return true
}

// removalFocusTargetLocked chooses the window to focus after the focused
// window `closed` was removed (which has already happened). Must be called
// with the lock held.
//
// The anchor main buffer is the tracked lastMainBufferWindow when it still
// exists — for a closed prompt that is its spawning main, since focusing a
// prompt restores its ParentMain there. When the closed window WAS the last
// main buffer (tracking already cleared), the anchor falls back to the main
// created next after it (the lowest Seq above closed.Seq), and failing that
// the main with the highest remaining Seq. Focus then goes to the anchor's
// newest (highest-Seq) prompt buffer if it has one — popping the anchor back
// to the top via SetFocus — else the anchor itself.
func (m *Manager) removalFocusTargetLocked(closed *Window) string {
	// Validate the tracked last main buffer.
	var anchor *Window
	if m.lastMainBufferWindow != nil {
		if _, ok := m.windows[m.lastMainBufferWindow.ID]; ok {
			anchor = m.lastMainBufferWindow
		}
	}

	// No usable last main buffer: next main by creation order, then highest.
	if anchor == nil {
		var nextHigher, highest *Window
		for _, w := range m.windows {
			if w.Type != MainBuffer || !w.Visible {
				continue
			}
			if w.Seq > closed.Seq && (nextHigher == nil || w.Seq < nextHigher.Seq) {
				nextHigher = w
			}
			if highest == nil || w.Seq > highest.Seq {
				highest = w
			}
		}
		anchor = nextHigher
		if anchor == nil {
			anchor = highest
		}
	}

	// No main buffers remain at all: fall back to the newest focusable prompt.
	if anchor == nil {
		var newest *Window
		for _, w := range m.windows {
			if w.Type == PromptBuffer && w.Visible && (newest == nil || w.Seq > newest.Seq) {
				newest = w
			}
		}
		if newest != nil {
			return newest.ID
		}
		return ""
	}

	// The anchor's newest prompt takes focus in its place, if it has one.
	target := anchor
	for _, w := range m.windows {
		if w.Type == PromptBuffer && w.Visible && w.ParentMain == anchor {
			if target == anchor || w.Seq > target.Seq {
				target = w
			}
		}
	}
	return target.ID
}

// focusCycleTarget returns the ID of the window to focus when cycling offset
// steps (with wraparound), or "" when there is nowhere to go.
//
// The cycle runs over visible MAIN buffers only; prompt buffers are not cycle
// stops of their own. The current position is the focused main buffer, or the
// focused window's ParentMain (a prompt counts as being "on" its spawning
// main). Once the target main is chosen, focus resolves to the newest
// (highest-Seq) visible prompt buffer spawned from it, if any — landing the
// user on that buffer's pending interaction — else the main itself.
func (m *Manager) focusCycleTarget(offset int) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect visible main buffers in deterministic (ID) order.
	var mains []*Window
	for _, w := range m.windows {
		if w.Type == MainBuffer && w.Visible {
			mains = append(mains, w)
		}
	}
	if len(mains) == 0 {
		return ""
	}
	for i := 0; i < len(mains)-1; i++ {
		for j := i + 1; j < len(mains); j++ {
			if mains[i].ID > mains[j].ID {
				mains[i], mains[j] = mains[j], mains[i]
			}
		}
	}

	// Locate the current position in the main cycle.
	currentMain := m.lastMainBufferWindow
	if current := m.windows[m.focusedWindowID]; current != nil {
		if current.Type == MainBuffer {
			currentMain = current
		} else if current.ParentMain != nil {
			currentMain = current.ParentMain
		}
	}
	currentIndex := -1
	if currentMain != nil {
		for i, w := range mains {
			if w.ID == currentMain.ID {
				currentIndex = i
				break
			}
		}
	}

	targetIndex := (currentIndex + offset + len(mains)) % len(mains)
	targetMain := mains[targetIndex]

	// Resolve to the target main's newest prompt buffer, if it has one.
	target := targetMain
	var newestPrompt *Window
	for _, w := range m.windows {
		if w.Type == PromptBuffer && w.Visible && w.ParentMain == targetMain {
			if newestPrompt == nil || w.Seq > newestPrompt.Seq {
				newestPrompt = w
			}
		}
	}
	if newestPrompt != nil {
		target = newestPrompt
	}

	if target.ID == m.focusedWindowID {
		return ""
	}
	return target.ID
}

// FocusNextWindow cycles focus to the next focusable window. The switch is
// routed through SetFocus so focus tracking and events behave identically to
// any other focus change.
func (m *Manager) FocusNextWindow() bool {
	target := m.focusCycleTarget(1)
	if target == "" {
		return false
	}
	return m.SetFocus(target)
}

// FocusPrevWindow cycles focus to the previous focusable window. The switch is
// routed through SetFocus so focus tracking and events behave identically to
// any other focus change.
func (m *Manager) FocusPrevWindow() bool {
	target := m.focusCycleTarget(-1)
	if target == "" {
		return false
	}
	return m.SetFocus(target)
}

// GetWindowsByDock returns all visible windows in a dock position, sorted by priority.
// For top dock: sorted descending (higher priority renders first, at screen top).
// For other docks: sorted ascending (lower priority renders first).
func (m *Manager) GetWindowsByDock(dock DockPosition) []*Window {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Window
	for _, w := range m.windows {
		if w.Dock == dock && w.Visible {
			result = append(result, w)
		}
	}

	// Sort by priority with stable secondary sort by ID
	// This ensures deterministic ordering even when priorities are equal
	// Top dock: descending (higher priority at top of screen)
	// Other docks: ascending
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			swap := false
			if dock == DockTop {
				// Descending sort for top dock
				if result[i].Priority < result[j].Priority {
					swap = true
				} else if result[i].Priority == result[j].Priority && result[i].ID > result[j].ID {
					swap = true // Secondary sort by ID for stability
				}
			} else {
				// Ascending sort for other docks
				if result[i].Priority > result[j].Priority {
					swap = true
				} else if result[i].Priority == result[j].Priority && result[i].ID > result[j].ID {
					swap = true // Secondary sort by ID for stability
				}
			}
			if swap {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result
}

// GetLastMainBufferWindow returns the last focused main buffer window.
func (m *Manager) GetLastMainBufferWindow() *Window {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.lastMainBufferWindow != nil {
		if _, exists := m.windows[m.lastMainBufferWindow.ID]; exists {
			return m.lastMainBufferWindow
		}
		m.lastMainBufferWindow = nil
	}

	// Find any main buffer
	for _, w := range m.windows {
		if w.Type == MainBuffer {
			return w
		}
	}

	return nil
}

// GetLastNormalWindow returns the last focused non-docked (main-area) window,
// regardless of buffer type. Modeled on GetLastMainBufferWindow.
func (m *Manager) GetLastNormalWindow() *Window {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.lastNormalWindow != nil {
		if _, exists := m.windows[m.lastNormalWindow.ID]; exists {
			return m.lastNormalWindow
		}
		m.lastNormalWindow = nil
	}

	// Find any non-docked window
	for _, w := range m.windows {
		if w.Dock == DockNone {
			return w
		}
	}

	return nil
}

// UpdateWindow updates a window's properties.
func (m *Manager) UpdateWindow(id string, updates func(*Window)) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	w, exists := m.windows[id]
	if !exists {
		return false
	}

	updates(w)

	go m.emit(Event{
		Type:     EventWindowUpdated,
		WindowID: id,
		NewValue: w,
	})

	return true
}

// CreatePromptBuffer creates a prompt buffer for user input.
func (m *Manager) CreatePromptBuffer(prompt, defaultValue string, callback func(string, bool)) string {
	// Find highest priority bottom window. A bottom-located modebar is
	// excluded: its fixed priority pins it to the last screen line, and
	// prompts must stack above it, not outbid it.
	bottomWindows := m.GetWindowsByDock(DockBottom)
	highestPriority := 0
	for _, w := range bottomWindows {
		if w.Class == "modebar" {
			continue
		}
		if w.Priority > highestPriority {
			highestPriority = w.Priority
		}
	}

	// Calculate prompt length (ANSI-aware)
	promptLength := calculateAnsiAwareLength(prompt)

	// Create a buffer with the default value
	var buf *buffer.Buffer
	if defaultValue != "" {
		buf = buffer.NewFromString(defaultValue)
	} else {
		buf = buffer.New()
	}

	// Ensure buffer has at least one line
	if buf.GetLineCount() == 0 {
		buf.InsertLine(0, "")
	}

	id := m.CreateWindow(WindowOptions{
		Type:        PromptBuffer,
		Dock:        DockBottom,
		Priority:    highestPriority + 10,
		MinHeight:   1,
		MaxHeight:   1,
		MarginInner: promptLength,
		RowMessages: []string{prompt},
		Buffer:      buf,
		SetFocus:    true,
	})

	// Set callback, position cursor, and ensure proper viewport
	m.mu.Lock()
	if w, exists := m.windows[id]; exists {
		w.Callback = callback
		// Position cursor at end of default value (on line 0 for legacy prompts)
		w.SetCursorPos(Position{Line: 0, Rune: len([]rune(defaultValue))})
		// Force viewOffsetX to 0 for prompts (matches TypeScript behavior)
		w.ViewState.ViewOffsetX = 0
		w.SetViewTop(0)
		// Set content height explicitly
		w.ContentHeight = buf.GetLineCount()
	}
	m.mu.Unlock()

	return id
}

// StatPeekUp increases visibility of top dock windows.
func (m *Manager) StatPeekUp() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	topWindows := m.getWindowsByDockLocked(DockTop)
	if m.StatPeek < len(topWindows)-1 {
		m.StatPeek++
		go m.emit(Event{
			Type:     EventStatPeekChanged,
			NewValue: m.StatPeek,
		})
		return true
	}
	return false
}

// StatPeekDown decreases visibility of top dock windows.
func (m *Manager) StatPeekDown() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.StatPeek > 0 {
		m.StatPeek--
		go m.emit(Event{
			Type:     EventStatPeekChanged,
			NewValue: m.StatPeek,
		})
		return true
	}
	return false
}

// PromptPeekUp increases visibility of bottom dock windows.
func (m *Manager) PromptPeekUp() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.PromptPeek > 0 {
		m.PromptPeek--
		go m.emit(Event{
			Type:     EventPromptPeekChanged,
			NewValue: m.PromptPeek,
		})
		return true
	}
	return false
}

// PromptPeekDown decreases visibility of bottom dock windows.
func (m *Manager) PromptPeekDown() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	bottomWindows := m.getWindowsByDockLocked(DockBottom)
	if m.PromptPeek < len(bottomWindows)-1 {
		m.PromptPeek++
		go m.emit(Event{
			Type:     EventPromptPeekChanged,
			NewValue: m.PromptPeek,
		})
		return true
	}
	return false
}

// getWindowsByDockLocked is the internal version without locking.
func (m *Manager) getWindowsByDockLocked(dock DockPosition) []*Window {
	var result []*Window
	for _, w := range m.windows {
		if w.Dock == dock && w.Visible {
			result = append(result, w)
		}
	}
	return result
}

// AllWindows returns all windows in deterministic order (sorted by ID).
func (m *Manager) AllWindows() []*Window {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Window, 0, len(m.windows))
	for _, w := range m.windows {
		result = append(result, w)
	}

	// Sort by ID for deterministic ordering
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			if result[i].ID > result[j].ID {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result
}

// calculateAnsiAwareLength calculates the visible length of a string with ANSI codes.
func calculateAnsiAwareLength(s string) int {
	length := 0
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		length += textwidth.Rune(r)
	}
	return length
}
