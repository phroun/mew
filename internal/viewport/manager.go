// Package viewport provides viewport management for the editor.
package viewport

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/textwidth"
)

// unixWisdom is sample context text used to populate each viewport's Context
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

// FindState holds a viewport's most recent find-command parameters;
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
// a prompt) on the viewport; the editor's command dispatcher consumes it when the
// next command runs, then clears it. Pending is false when nothing is armed.
type RepeatState struct {
	Pending bool
	Count   int
}

// ViewState holds the viewport and display state for a viewport.
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
	// SuppressRTLCombining hides combining marks (niqqud, harakat) that ride
	// RTL base letters — the "rtlCombining" option, INVERTED so the zero
	// value keeps the default (marks shown). Turning rtlCombining off makes
	// pointed Hebrew render like mew's pre-shaped Arabic: one codepoint per
	// cell, so a bidi-applying host terminal (flipBidiForHost) no longer
	// miscounts background fills — the selection bar works instead of the
	// ride-safe fallback. The marks stay in the BUFFER (the caret still walks
	// them, and the modebar shows the one under the caret); only display is
	// suppressed.
	SuppressRTLCombining bool
	// ShowMarks renders a one-column "*" (in the "marks" color) at every mark /
	// garland-decoration position in the text, giving each mark its own cell
	// between the surrounding characters. It is a three-way enum: "no" (off),
	// "yes" (user-visible marks), or "all" (also mew's internal, underscore-
	// prefixed marks). "" is treated as "no".
	ShowMarks string
	// ShowRuler renders a column ruler on the viewport's top line, reducing the
	// content area by one row. Ignored when the viewport is only one line tall.
	ShowRuler bool
	// Scrollbar reserves the viewport's outer column (physical right in LTR,
	// left in RTL) for a vertical scrollbar — track and thumb cells the mouse
	// can click and drag. Never rendered on prompt viewports or viewports whose
	// buffer hosts a terminal session (the renderer's suppressor). Default on.
	Scrollbar bool
	TabSize   int
	// Direction overrides the editor's base text direction for this viewport:
	// "ltr", "rtl", or "" to inherit the [general] direction option. Prompt
	// viewports are pinned "ltr" at creation.
	Direction string
	// OverwriteMode is the inverse of the insertMode option: false (zero value,
	// the default) is insert mode; true makes typing replace the character under
	// the caret (except at end of line, where it appends). Stored inverted so a
	// zero-value viewport defaults to insert mode.
	OverwriteMode bool
	// ReadOnly rejects content edits made through this viewport; navigation,
	// search, marks, and undo/redo remain available. Per viewport; default false.
	ReadOnly bool
	// AutoIndent makes insert_newline start each new line with the leading
	// whitespace of the line it split, replicating tabs and spaces exactly as
	// they appear. Per viewport; default false.
	AutoIndent bool
	// LinkBrowsing enables the hyperlink layer for this viewport: link
	// coloring, browse-mode buttons, and arming on caret entry. Off, links
	// render exactly as the grammar colors them. Default true.
	LinkBrowsing bool
	// Syntax is the per-viewport default grammar name resolved from a
	// grammar-agnostic option overlay ([options/tool] syntax=dokuwiki,
	// [<class>.options] syntax=...); "" inherits the global syntax option.
	// It is the fallback grammar for the viewport's buffer when nothing is
	// detected from a shebang/modeline/filename.
	Syntax string

	// SyntaxOverrides is a space-separated list of grammar flavors (e.g.
	// "go conf") whose highlighter should skip this document's project
	// .mew/syntax folder and resolve from the user's own copy (mew:/syntax),
	// the built-in set, or JOE instead. Per viewport; inherits the editor default.
	SyntaxOverrides string

	// ScrollDetached parks the viewport free of the caret: while set, the
	// per-frame caret follow leaves the view exactly where a mouse-wheel scroll
	// or a scroll_* command placed it, even with the caret off-screen (the
	// cursorOffScreen indicator marks where it went). A cursor-movement or edit
	// command re-engages following and clears this. Transient per-viewport UI
	// state, not part of the buffer binding.
	ScrollDetached bool
}

// MarksVisible reports whether showMarks draws any mark indicators for this
// viewport (mode "yes" or "all"). "" and "no" are off.
func (vs ViewState) MarksVisible() bool {
	return vs.ShowMarks == "yes" || vs.ShowMarks == "all"
}

// MarksShowInternal reports whether showMarks also indicates mew's internal,
// underscore-prefixed marks (mode "all").
func (vs ViewState) MarksShowInternal() bool {
	return vs.ShowMarks == "all"
}

// MarkOptionOverridden records that a per-viewport option was set explicitly on
// this viewport, so a grammar options overlay leaves it alone.
func (w *Viewport) MarkOptionOverridden(name string) {
	if w.overriddenOptions == nil {
		w.overriddenOptions = make(map[string]bool)
	}
	w.overriddenOptions[name] = true
}

// IsOptionOverridden reports whether a per-viewport option was set explicitly on
// this viewport (and so must not be overwritten by a grammar overlay).
func (w *Viewport) IsOptionOverridden(name string) bool {
	return w.overriddenOptions[name]
}

// ClearOptionOverridden drops the explicit-override flag for a per-viewport
// option, so the grammar overlay (and clear_option) may restore it to the
// resolved default. Harmless if it was not overridden.
func (w *Viewport) ClearOptionOverridden(name string) {
	delete(w.overriddenOptions, name)
}

// AppliedOptionSig returns the overlay signature (class/grammar/type) whose
// resolved options were last applied to this viewport.
func (w *Viewport) AppliedOptionSig() string { return w.appliedOptionSig }

// SetAppliedOptionSig records the overlay signature now reflected in this
// viewport's ViewState.
func (w *Viewport) SetAppliedOptionSig(sig string) { w.appliedOptionSig = sig }

// ViewportType represents the kind of viewport. DocViewport and ToolViewport are
// PEER interactive surfaces — both focusable, both get the full feature set
// (links/browse, outline, completion, per-viewport options); only PromptViewport
// (the modal input line) is special.
type ViewportType int

const (
	DocViewport    ViewportType = iota // A document (editable content)
	ToolViewport                       // A tool surface (help, listings, readouts)
	PromptViewport                     // Single-line modal input at screen bottom
)

// Name returns the type's name as used in color/config lookups
// ([colors.<name>] sections): "doc", "tool", "prompt".
func (t ViewportType) Name() string {
	switch t {
	case DocViewport:
		return "doc"
	case ToolViewport:
		return "tool"
	case PromptViewport:
		return "prompt"
	}
	return ""
}

// Chrome ViewportSets: viewports in them never hold keyboard focus and never take
// part in focus cycling, focus-target selection, or the content-viewport
// enumerations (buffer cycling, save-all, DEADCAT, exit-on-last). The
// modebar is persistent chrome; transient notification/warning/error toasts
// are ephemeral chrome. Other sets ("" for documents, "help" for the help
// system) are fully focus-eligible content groupings.
const (
	ViewportSetModebar   = "modebar"
	ViewportSetTransient = "transient"
)

// isChromeSet reports whether a ViewportSet is chrome (excluded from focus and
// from content enumerations).
func isChromeSet(set string) bool {
	return set == ViewportSetModebar || set == ViewportSetTransient
}

// FocusEligible reports whether the viewport can hold keyboard focus and take
// part in focus cycling / focus-target selection: every viewport except a
// PromptViewport (which focuses transiently, on its own path) and chrome (the
// modebar). Doc and tool viewports both qualify.
func (w *Viewport) FocusEligible() bool {
	return w != nil && w.Type != PromptViewport && !isChromeSet(w.ViewportSet)
}

// DockPosition represents where a viewport is docked.
type DockPosition int

const (
	DockNone   DockPosition = iota // Not docked (main editing area)
	DockTop                        // Docked to top (status/ruler)
	DockBottom                     // Docked to bottom (prompts)
)

// cursorRingSize is the number of remembered edit positions per viewport.
const cursorRingSize = 10

// Viewport represents an editor viewport with its buffer and display state.
type Viewport struct {
	ID    string
	Type  ViewportType
	Class string
	// ViewportSet groups viewports into a named space orthogonal to Type: "" for
	// ordinary documents, "help" for the help system, "modebar" for the
	// modebar (chrome). Each set has its own last-focused-viewport memory (see
	// lastFocusedBySet), and the chrome set is excluded from focus.
	ViewportSet string
	Dock        DockPosition
	Priority    int
	Visible     bool

	// CanFocus gates the focus SWITCHER (FocusNextViewport / FocusPrevViewport):
	// a viewport with CanFocus=false is skipped when cycling focus, though it can
	// still be focused explicitly (SetFocus, a mouse click, help_open). Defaults
	// to true; the editor drops it to false for a viewport that should not be a
	// cycle stop (e.g. the auto-following Quick Help peek).
	CanFocus bool

	// Seq is a monotonically increasing creation sequence number, unique
	// across all viewports: each new viewport gets the previous number +1.
	Seq int64

	// Context is a per-viewport descriptor surfaced by the modebar
	// when there is no active key-sequence autocompletion to display.
	// The editor overwrites it with the syntax outline breadcrumb (the
	// enclosing function/section chain) when one is available.
	Context string

	// SpawnContext is the placeholder Context assigned at creation, restored
	// when no outline breadcrumb applies at the caret.
	SpawnContext string

	// SpawnedAt records when the viewport was created, used to expire transient
	// notification/error/warning viewports.
	SpawnedAt time.Time

	// ParentViewport is the main-buffer viewport that (transitively) spawned this
	// viewport. Set at creation for prompt buffers: inherited from the focused
	// viewport's ParentViewport when present, else the last main buffer. Cleared if
	// the parent viewport is removed.
	ParentViewport *Viewport

	Buffer *buffer.Buffer

	ViewState ViewState

	// overriddenOptions records per-viewport options the user (or a launch-time
	// per-file switch) set explicitly via set_option, so a grammar-driven
	// options overlay does not clobber a deliberate choice. Nil until first use.
	overriddenOptions map[string]bool

	// appliedOptionSig is the overlay signature (class/grammar/type) whose
	// resolved options were last written into this viewport's ViewState. "" (the
	// initial value) matches a plain viewport with no grammar/class/type overlay,
	// so such viewports are left untouched.
	appliedOptionSig string

	// viewportAnchor tracks the first visible document line as a garland
	// cursor, so the viewport stays pinned to the same logical line when the
	// buffer is edited above it (see SetViewTop/RefreshViewTop). Created when
	// the viewport is bound to a buffer, released when the viewport is removed.
	viewportAnchor *buffer.Anchor

	// Caret is the viewport's own edit cursor — a garland cursor that garland
	// maintains across every edit. Editing goes through it, and it slides with
	// edits made through another viewport on the same buffer, so CursorPos (its
	// cached line/rune) can be refreshed from it and never goes stale. It is
	// parked at CursorPos on focus-out and read back on focus-in (see
	// SetFocus). Created when the viewport is bound to a buffer, released on
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

	// Find is the viewport's own find state (see FindState). An all-buffers
	// search ("a" option) lives on the editor instead.
	Find FindState

	// findOrigin is a temporary garland cursor marking where the current
	// search began (set when a find commits); it slides with edits like any
	// other cursor. findWrapped records that the search has wrapped past the
	// end of the buffer since then — when a match then crosses back over the
	// origin, the editor announces that the search has looped. Released in
	// RemoveViewport.
	findOrigin  *buffer.Anchor
	findWrapped bool

	// Link browse mode (link-as-button rendering). BrowseActive turns button
	// rendering on for this viewport's links, so the caret parks on whole
	// buttons and the nav_* commands drive between them. It is entered
	// explicitly (the navigationMode option, toggled by set_option / ^O N) and
	// carried when following a link into another wiki page; cleared by
	// nav_cancel and by turning navigationMode off.
	BrowseActive bool

	// AfterKey is this viewport's after-key pseudo-binding: a PawScript command
	// the editor runs each time a key's binding activity RESOLVES while this
	// viewport owns the keyboard — after the bound (or fallthrough-insert)
	// command completes, or, when a non-matching sequence starts to unwind,
	// after the FIRST unwound key lands as a normal press (nothing waits for
	// the rest of the unwind). It does not fire while a prefix is silently
	// pending (^K alone) or for a key that executed nothing. Normally empty;
	// a prompt (say) sets it to react to every keystroke the user lands.
	AfterKey string

	// WikiRoot confines this viewport's wiki-reference resolution to a subtree:
	// a canonical URL ("mew:///docs", "file:///home/us/wiki"; "" = none).
	// When set, absolute wiki refs resolve from this root and relative climbs
	// ("..") clamp at it — leaving the wiki requires a full scheme reference.
	// A viewport's root NEVER changes: it is part of the viewport's identity, set
	// by how the viewport came to be (a wiki-scheme open carries its registry
	// root, a plain buffer_open carries none). Following a link that resolves
	// under a DIFFERENT root surfaces a different viewport — possibly sharing
	// the same underlying buffer (a Help viewport and an editor viewport can show
	// one document with different roots, types, and colors). Because it is
	// viewport identity, it deliberately does NOT travel with the buffer-swap
	// nav history.
	WikiRoot string

	// WikiName is the registry name of the wiki this viewport browses ("help";
	// "" for every non-wiki viewport). Viewport identity like WikiRoot — set when
	// the viewport is surfaced for a registered wiki, never changed after —
	// so viewport-level behavior (chrome, colors, commands) can key off WHICH
	// wiki this is, not just where its tree lives.
	WikiName string

	// Nav history: the buffer bindings this viewport navigated away from, in
	// browser back/forward form. SwapBuffer pushes the active binding onto
	// navBack (and clears navFwd — a new departure invalidates the forward
	// trail); NavHistoryPrior/NavHistoryNext shuffle bindings between the
	// stacks and the viewport. Stacked bindings keep LIVE cursors on their
	// buffers (see viewBinding), so returning restores the exact caret,
	// scroll, and trail. History is navigation, not ownership: a stacked
	// binding references its buffer but closing the viewport simply drops the
	// whole history (released in RemoveViewport) — buffer retirement is decided
	// by whoever still references it.
	//
	// navGrave is the GRAVEYARD: when history maintenance would drop the LAST
	// reference to a buffer (a forward trail invalidated by a new departure,
	// or an explicit history clear), the binding is buried here instead of
	// released, so the buffer stays reachable for the eventual "save its
	// changes?" reckoning. That is the graveyard's only purpose — it takes no
	// part in navigation. One binding per buffer (a duplicate burial releases
	// the newcomer). All three are released in RemoveViewport.
	navBack  []viewBinding
	navFwd   []viewBinding
	navGrave []viewBinding

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

	// NavIdealCol is the DISPLAY-space target column for vertical link nav
	// (nav_up / nav_down), held separately from IdealVisualColumn (which is a
	// raw column re-anchored by every horizontal move). NavIdealSet gates it:
	// it is established on the first vertical nav of a run and preserved across
	// the run — including its paging fallback — so repeated up/down keep a
	// consistent target regardless of where each link sits, then cleared by any
	// other caret move. See the editor's navVert.
	NavIdealCol int
	NavIdealSet bool

	// Viewport chrome slots are named by READING side, like the inner and
	// outer margins of a book page: Inner is the reading-start side (left in
	// an LTR viewport, right in RTL), Outer is the opposite edge. The renderer
	// maps them to physical sides per the viewport's effective direction.
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
	// ScrollbarX is the 0-based physical screen column of this viewport's
	// vertical scrollbar this frame, or -1 when no bar is shown. Set by the
	// renderer alongside the Content* geometry (and like it, only meaningful
	// while LayoutEpoch is current); the mouse path hit-tests against it.
	ScrollbarX int
	// ScrollbarTrackH is the bar's track height in cells this frame. Usually
	// ContentHeight, but one row SHORTER when the bar's bottom cell would be
	// the screen's unwritable bottom-right corner (an LTR bar on the
	// bottom-most viewport) — the thumb then bottoms out on the second-to-last
	// row instead of clipping into the corner. Renderer-set, mouse-read, like
	// ScrollbarX.
	ScrollbarTrackH int

	// LayoutEpoch is the renderer's frame counter as of the last frame that
	// actually laid this viewport out. A viewport whose epoch matches the
	// renderer's current epoch is TRULY on screen; a stale epoch marks a
	// background viewport (an unshown main) whose geometry above is left over
	// from an earlier frame and must not win mouse hit-testing.
	LayoutEpoch uint64

	// Legacy callback for simple prompts (2 params)
	Callback func(input string, accepted bool)
	// PromptCallback for full prompt support (3 params matching TypeScript)
	// Parameters: accepted, bufferContent (line 0), cursorLineText (current cursor line)
	PromptCallback func(accepted bool, bufferContent, cursorLineText string)
	CustomRenderer string

	// CompletionCallback, when set, handles the `completion` command for this
	// viewport (e.g. filename completion on a filename prompt). It returns true
	// when it handled the completion; false lets the command's fallback run
	// (the demo binds completion|insert '\t', so a plain buffer types a tab).
	CompletionCallback func() bool

	// Tag is a secondary label (beyond Class) grouping viewports that should
	// replace rather than stack — e.g. the filename-completion transient tags
	// itself so a fresh completion first removes the previous one.
	Tag string
}

// noteMainFocus records w as the last-focused main-area viewport and the
// last-focused viewport within its ViewportSet. Called (under m.mu) for a
// focus-eligible viewport that just took focus, or a prompt's parent standing
// in for it.
func (m *Manager) noteMainFocus(w *Viewport) {
	m.lastMainViewport = w
	if m.lastFocusedBySet == nil {
		m.lastFocusedBySet = make(map[string]*Viewport)
	}
	m.lastFocusedBySet[w.ViewportSet] = w
}

// LastMainViewport returns the last-focused main-area (focus-eligible) viewport
// occupying the main editing area (nil when none), validated against the
// live viewport set.
func (m *Manager) LastMainViewport() *Viewport {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.lastMainViewport != nil {
		if _, ok := m.viewports[m.lastMainViewport.ID]; ok {
			return m.lastMainViewport
		}
	}
	return nil
}

// LastFocusedInSet returns the viewport most recently focused within the named
// ViewportSet (nil when none is live), for set-scoped focus restoration.
func (m *Manager) LastFocusedInSet(set string) *Viewport {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if w := m.lastFocusedBySet[set]; w != nil {
		if _, ok := m.viewports[w.ID]; ok {
			return w
		}
	}
	return nil
}

// CursorPos returns the viewport's caret position, read straight from its
// garland caret cursor — the single source of truth. Garland maintains the
// cursor across every edit (including edits made through another viewport on the
// same buffer), so this never goes stale and there is no cached copy to
// reconcile. The read is O(1) amortized (garland resolves the line/column
// lazily at most once per edit).
func (w *Viewport) CursorPos() Position {
	if w.Caret == nil {
		return Position{}
	}
	l, r := w.Caret.Position()
	return Position{Line: l, Rune: r}
}

// CaretByte returns the caret's absolute byte offset in the buffer (0 at the
// start), for readouts like the modebar's %ABSBYTE%.
func (w *Viewport) CaretByte() int64 {
	if w.Caret == nil {
		return 0
	}
	return w.Caret.BytePos()
}

// SetCursorPos moves the caret to p. The line is clamped into the buffer's
// range first: garland rejects a seek to a nonexistent line (leaving the
// caret put), so callers that compute an out-of-range target — page up/down
// past an edge, say — rely on this clamp to land at the edge instead.
func (w *Viewport) SetCursorPos(p Position) {
	if w.Caret != nil {
		line := w.clampLine(p.Line)
		w.Caret.Seek(line, w.clampRune(line, p.Rune))
	}
}

// SetCursorLine moves the caret to a line (clamped into range), keeping its
// rune column (itself clamped to the target line's length).
func (w *Viewport) SetCursorLine(line int) {
	if w.Caret != nil {
		_, r := w.Caret.Position()
		line = w.clampLine(line)
		w.Caret.Seek(line, w.clampRune(line, r))
	}
}

// clampLine clamps a line index into [0, lineCount-1].
func (w *Viewport) clampLine(line int) int {
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
func (w *Viewport) clampRune(line, runePos int) int {
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
func (w *Viewport) SetCursorRune(runePos int) {
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
// ring navigation is abandoned. A no-op on viewports without a ring (no buffer).
func (w *Viewport) TrackEdit() {
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
func (w *Viewport) HasMovedSinceEdit() bool {
	return w.hasMoved
}

// SetFindOrigin parks the search-origin cursor at the caret (where a newly
// committed search begins) and clears the wrapped flag. The origin is a live
// garland cursor, so it stays on its logical position across edits.
func (w *Viewport) SetFindOrigin() {
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
// been set on this viewport.
func (w *Viewport) FindOriginByte() (int64, bool) {
	if w.findOrigin == nil {
		return 0, false
	}
	return w.findOrigin.BytePos(), true
}

// FindWrapped reports whether the current search has wrapped past the end of
// the buffer since its origin was set.
func (w *Viewport) FindWrapped() bool { return w.findWrapped }

// SetFindWrapped records or clears the wrapped state.
func (w *Viewport) SetFindWrapped(v bool) { w.findWrapped = v }

// TrackMove records a deliberate caret movement: the caret has moved unless it
// has landed back on the last edit point. Ends any in-progress ring navigation.
func (w *Viewport) TrackMove() {
	if w.lastEditPoint == nil || w.Caret == nil {
		return
	}
	w.hasMoved = w.Caret.BytePos() != w.lastEditPoint.BytePos()
	w.ringNav = -1
}

// ringAnchorAt returns the i-th navigable history position, ordered oldest to
// newest: 0..ringCount-1 index the ring entries, and ringCount is the live
// lastEditPoint (newer than every ring entry).
func (w *Viewport) ringAnchorAt(i int) *buffer.Anchor {
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
func (w *Viewport) CursorRingPrior() (int64, bool) {
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
func (w *Viewport) CursorRingNext() (int64, bool) {
	if w.lastEditPoint == nil || w.ringNav < 0 || w.ringNav >= w.ringCount {
		return 0, false
	}
	w.ringNav++
	return w.ringAnchorAt(w.ringNav).BytePos(), true
}

// SeekCaretByte moves the caret to a byte offset (used by ring navigation to
// jump to a remembered position).
func (w *Viewport) SeekCaretByte(pos int64) {
	if w.Caret != nil {
		w.Caret.SeekByte(pos)
	}
}

// SetViewTop sets the first visible document line, updating both the painting
// offset and the sliding viewport anchor so the view stays pinned to that
// logical line when the buffer is edited above it.
func (w *Viewport) SetViewTop(line int) {
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
func (w *Viewport) RefreshViewTop() {
	if w.viewportAnchor != nil {
		w.ViewState.ViewOffsetY = w.viewportAnchor.Line()
	}
}

// viewBinding is the complete bundle of state tying a viewport to ONE buffer:
// the garland cursors that slide with edits — caret, viewport anchor, the
// edit-trail ring, find origin, link anchor — plus the scalar state that
// travels with them (ring indices, browse flag, view offsets). The ACTIVE
// binding lives inlined in the Viewport's own fields; detachBinding /
// attachBinding move the bundle wholesale in and out of a viewBinding record.
// A detached record keeps its cursors LIVE on the old buffer (garland keeps
// sliding them with edits), which is what makes a buffer-swap history stack
// safe: re-attaching restores the exact caret, scroll, and trail no matter
// what happened to the buffer in between. release() retires a record whose
// history is being dropped.
//
// The viewport's find TERM (Find) deliberately stays outside the binding —
// searching again after a swap should reuse the term; only the positional
// find state (origin cursor, wrap flag) is buffer-tied.
type viewBinding struct {
	Buffer         *buffer.Buffer
	caret          *buffer.Caret
	viewportAnchor *buffer.Anchor
	lastEditPoint  *buffer.Anchor
	hasMoved       bool
	cursorRing     [cursorRingSize]*buffer.Anchor
	ringFirst      int
	ringCount      int
	ringNav        int
	findOrigin     *buffer.Anchor
	findWrapped    bool
	browseActive   bool
	viewOffsetX    int
	viewOffsetY    int
}

// release retires a detached binding: every garland cursor is released so the
// buffer stops adjusting them on edits. Used when a stacked binding's history
// is dropped (viewport removal, stack eviction); the ACTIVE binding is released
// through Viewport.releaseBinding instead.
func (b *viewBinding) release() {
	if b.caret != nil {
		b.caret.Release()
		b.caret = nil
	}
	if b.viewportAnchor != nil {
		b.viewportAnchor.Release()
		b.viewportAnchor = nil
	}
	if b.lastEditPoint != nil {
		b.lastEditPoint.Release()
		b.lastEditPoint = nil
	}
	if b.findOrigin != nil {
		b.findOrigin.Release()
		b.findOrigin = nil
	}
	for i := range b.cursorRing {
		if b.cursorRing[i] != nil {
			b.cursorRing[i].Release()
			b.cursorRing[i] = nil
		}
	}
}

// bindBuffer attaches w to buf and mints its cursor bundle — the caret, the
// viewport anchor, and the edit-trail ring — so every position slides with
// edits. One bundle per viewport: two viewports sharing a buffer edit and scroll
// independently. The viewport must be unbound (fresh, or after detachBinding /
// releaseBinding).
func (w *Viewport) bindBuffer(buf *buffer.Buffer) {
	w.Buffer = buf
	w.ringNav = -1
	if buf == nil {
		return
	}
	w.Caret = buf.NewCaret()
	w.viewportAnchor = buf.NewAnchor()
	// Every viewport with a buffer gets its own cursor ring — a caret moves and
	// edits in a prompt buffer just as in a document, so its edit history is
	// worth tracking too.
	w.lastEditPoint = buf.NewAnchor()
	for i := range w.cursorRing {
		w.cursorRing[i] = buf.NewAnchor()
	}
}

// detachBinding moves the viewport's ACTIVE binding out into a record, leaving
// the viewport unbound: no buffer, no cursors, view offsets zeroed. Nothing is
// released — the record's cursors stay live on the old buffer and keep
// sliding with edits, so a later attachBinding restores the exact caret,
// scroll, and trail. The caller owns the record: stack it for a future
// re-attach, or release() it.
func (w *Viewport) detachBinding() viewBinding {
	b := viewBinding{
		Buffer:         w.Buffer,
		caret:          w.Caret,
		viewportAnchor: w.viewportAnchor,
		lastEditPoint:  w.lastEditPoint,
		hasMoved:       w.hasMoved,
		cursorRing:     w.cursorRing,
		ringFirst:      w.ringFirst,
		ringCount:      w.ringCount,
		ringNav:        w.ringNav,
		findOrigin:     w.findOrigin,
		findWrapped:    w.findWrapped,
		browseActive:   w.BrowseActive,
		viewOffsetX:    w.ViewState.ViewOffsetX,
		viewOffsetY:    w.ViewState.ViewOffsetY,
	}
	w.Buffer = nil
	w.Caret = nil
	w.viewportAnchor = nil
	w.lastEditPoint = nil
	w.hasMoved = false
	w.cursorRing = [cursorRingSize]*buffer.Anchor{}
	w.ringFirst, w.ringCount, w.ringNav = 0, 0, -1
	w.findOrigin = nil
	w.findWrapped = false
	w.BrowseActive = false
	w.ViewState.ViewOffsetX = 0
	w.ViewState.ViewOffsetY = 0
	return b
}

// attachBinding installs a previously detached record as the viewport's active
// binding — the mirror of detachBinding. The viewport must be unbound. The
// vertical view offset re-derives from the viewport anchor (which slid with
// any edits made while the binding was stacked); the horizontal offset is
// restored as saved.
func (w *Viewport) attachBinding(b viewBinding) {
	w.Buffer = b.Buffer
	w.Caret = b.caret
	w.viewportAnchor = b.viewportAnchor
	w.lastEditPoint = b.lastEditPoint
	w.hasMoved = b.hasMoved
	w.cursorRing = b.cursorRing
	w.ringFirst, w.ringCount, w.ringNav = b.ringFirst, b.ringCount, b.ringNav
	w.findOrigin = b.findOrigin
	w.findWrapped = b.findWrapped
	w.BrowseActive = b.browseActive
	w.ViewState.ViewOffsetX = b.viewOffsetX
	w.ViewState.ViewOffsetY = b.viewOffsetY
	w.RefreshViewTop()
}

// releaseBinding releases every garland cursor of the viewport's ACTIVE binding
// so the buffer stops adjusting them on edits. The Buffer reference, view
// offsets, and browse flag are kept: removal paths inspect w.Buffer after the
// viewport is gone (the shared-buffer close check), and the removal event
// carries the viewport as it was.
func (w *Viewport) releaseBinding() {
	buf := w.Buffer
	offX, offY := w.ViewState.ViewOffsetX, w.ViewState.ViewOffsetY
	browse := w.BrowseActive
	b := w.detachBinding()
	b.release()
	w.Buffer = buf
	w.ViewState.ViewOffsetX, w.ViewState.ViewOffsetY = offX, offY
	w.BrowseActive = browse
}

// SwapBuffer replaces the viewport's buffer in place: the active binding is
// pushed (live, unreleased) onto the back history and a fresh binding is
// minted on buf — caret at the start, view at the top. Any forward history is
// dropped: departing to a new destination invalidates the re-advance trail,
// exactly as in a browser — but never unconditionally: a forward binding
// whose buffer would lose its LAST reference is buried in the graveyard
// instead of released (referencedOutside must report whether a buffer is
// held anywhere beyond this viewport's nav structures; the viewport accounts for
// its own active buffer, back stack, and the new destination itself). The
// caller decides WHICH buffer (reusing an open one for the same file, or
// loading it) — the viewport only manages bindings.
func (w *Viewport) SwapBuffer(buf *buffer.Buffer, referencedOutside func(*buffer.Buffer) bool) {
	inBack := func(b *buffer.Buffer) bool {
		for i := range w.navBack {
			if w.navBack[i].Buffer == b {
				return true
			}
		}
		return false
	}
	old := w.Buffer
	w.navBack = append(w.navBack, w.detachBinding())
	for i := range w.navFwd {
		b := w.navFwd[i].Buffer
		switch {
		case b == nil || b == buf || b == old || inBack(b):
			w.navFwd[i].release()
		case referencedOutside != nil && referencedOutside(b):
			w.navFwd[i].release()
		default:
			w.bury(w.navFwd[i])
		}
	}
	w.navFwd = nil
	w.bindBuffer(buf)
}

// ReplaceBuffer swaps the viewport's ACTIVE buffer for buf WITHOUT touching the
// navigation history: the back and forward stacks are left exactly as they are
// and only the active binding is exchanged. It exists for an EPHEMERAL "dynamic
// page" that re-renders in place (Quick Help following the key context), so
// repeated updates never accumulate history entries — the page stays a single
// history slot. The departing buffer is always RELEASED, never buried: the
// dynamic page's content is transient (read-only help pages, or a placeholder
// notice), so there is nothing to protect from orphaning — burying it would
// only leave it lingering in the graveyard to resurface (e.g. at exit) as
// though it were unsaved work. A buffer still referenced elsewhere survives the
// release through that other binding, exactly as before.
func (w *Viewport) ReplaceBuffer(buf *buffer.Buffer) {
	old := w.detachBinding()
	old.release()
	w.bindBuffer(buf)
}

// bury moves a detached binding into the graveyard, keeping at most one
// binding per buffer (a duplicate burial releases the newcomer — the buffer
// is already safe).
func (w *Viewport) bury(b viewBinding) {
	for i := range w.navGrave {
		if w.navGrave[i].Buffer == b.Buffer {
			b.release()
			return
		}
	}
	w.navGrave = append(w.navGrave, b)
}

// GraveyardBuffers returns the distinct buffers held only by the viewport's
// graveyard — parked awaiting the "save its changes?" reckoning.
func (w *Viewport) GraveyardBuffers() []*buffer.Buffer {
	out := make([]*buffer.Buffer, 0, len(w.navGrave))
	for i := range w.navGrave {
		if b := w.navGrave[i].Buffer; b != nil {
			out = append(out, b)
		}
	}
	return out
}

// ResurrectLastBuried replaces the viewport's ACTIVE binding with the most
// recently buried one: the active binding is released (the caller decides
// what becomes of its buffer) and the graveyard binding surfaces with its
// full caret/scroll/trail state. False when the graveyard is empty.
func (w *Viewport) ResurrectLastBuried() bool {
	if len(w.navGrave) == 0 {
		return false
	}
	b := w.navGrave[len(w.navGrave)-1]
	w.navGrave = w.navGrave[:len(w.navGrave)-1]
	old := w.detachBinding()
	old.release()
	w.attachBinding(b)
	return true
}

// Unbury releases any graveyard binding of buf: the buffer has become
// actively bound somewhere, so it is no longer at risk of orphaning and the
// graveyard has no claim to it.
func (w *Viewport) Unbury(buf *buffer.Buffer) {
	out := w.navGrave[:0]
	for i := range w.navGrave {
		if w.navGrave[i].Buffer == buf {
			w.navGrave[i].release()
			continue
		}
		out = append(out, w.navGrave[i])
	}
	w.navGrave = out
}

// NavHistoryPrior returns to the binding the viewport most recently swapped
// away from, moving the active binding onto the forward stack. Reports false
// (no state change) when there is no back history, so command chains can
// fall through.
func (w *Viewport) NavHistoryPrior() bool {
	if len(w.navBack) == 0 {
		return false
	}
	b := w.navBack[len(w.navBack)-1]
	w.navBack = w.navBack[:len(w.navBack)-1]
	w.navFwd = append(w.navFwd, w.detachBinding())
	w.attachBinding(b)
	return true
}

// NavHistoryNext re-advances along the forward stack after a NavHistoryPrior
// — the mirror operation. Reports false when there is nothing to re-advance
// to.
func (w *Viewport) NavHistoryNext() bool {
	if len(w.navFwd) == 0 {
		return false
	}
	b := w.navFwd[len(w.navFwd)-1]
	w.navFwd = w.navFwd[:len(w.navFwd)-1]
	w.navBack = append(w.navBack, w.detachBinding())
	w.attachBinding(b)
	return true
}

// NavHistoryDepths reports how many entries the back and forward histories
// hold (for readouts and command gating).
func (w *Viewport) NavHistoryDepths() (prior, next int) {
	return len(w.navBack), len(w.navFwd)
}

// StackedBuffers returns the distinct buffers referenced by the viewport's nav
// structures — back/forward history AND the graveyard — excluding its active
// binding. Data-safety enumerations — close checks, save-all, crash dumps,
// open-buffer reuse — must treat all of these as open: history entries are
// one nav_history_prior away, and graveyard entries are being held precisely
// for their unsaved state.
func (w *Viewport) StackedBuffers() []*buffer.Buffer {
	seen := map[*buffer.Buffer]bool{}
	var out []*buffer.Buffer
	for _, s := range [][]viewBinding{w.navBack, w.navFwd, w.navGrave} {
		for i := range s {
			if b := s[i].Buffer; b != nil && !seen[b] {
				seen[b] = true
				out = append(out, b)
			}
		}
	}
	return out
}

// releaseNavHistory releases every stacked binding's cursors and drops the
// back/forward stacks and the graveyard (viewport removal — the close path has
// already prompted for any at-risk buffers among them).
func (w *Viewport) releaseNavHistory() {
	for _, s := range [][]viewBinding{w.navBack, w.navFwd, w.navGrave} {
		for i := range s {
			s[i].release()
		}
	}
	w.navBack, w.navFwd, w.navGrave = nil, nil, nil
}

// ClearNavHistory empties the viewport's back/forward history entirely. Each
// stacked binding is released — EXCEPT a binding holding the LAST reference
// to its buffer (per referencedOutside, which must report whether the buffer
// is held anywhere beyond this viewport's nav structures; the active buffer is
// accounted here), which is buried in the graveyard so the buffer stays
// reachable for the eventual save decision. Reports how many bindings were
// released and how many buried.
func (w *Viewport) ClearNavHistory(referencedOutside func(*buffer.Buffer) bool) (dropped, buried int) {
	clear := func(s []viewBinding) {
		for i := range s {
			b := s[i].Buffer
			if b == nil || b == w.Buffer || (referencedOutside != nil && referencedOutside(b)) {
				s[i].release()
				dropped++
				continue
			}
			before := len(w.navGrave)
			w.bury(s[i])
			if len(w.navGrave) > before {
				buried++
			} else {
				dropped++ // duplicate burial: the buffer was already safe
			}
		}
	}
	clear(w.navBack)
	w.navBack = nil
	clear(w.navFwd)
	w.navFwd = nil
	return dropped, buried
}

// EventType represents the type of viewport event.
type EventType int

const (
	EventViewportCreated EventType = iota
	EventViewportRemoved
	EventViewportUpdated
	EventFocusChanged
	EventCursorPositioned
	EventGhostCursorSet
	EventGhostCursorCleared
	EventStatPeekChanged
	EventPromptPeekChanged
)

// Event represents a viewport manager event.
type Event struct {
	Type       EventType
	ViewportID string
	OldValue   interface{}
	NewValue   interface{}
}

// EventHandler is a callback for viewport events.
type EventHandler func(event Event)

// Manager handles viewport lifecycle and focus management.
type Manager struct {
	mu sync.RWMutex

	viewports         map[string]*Viewport
	focusedViewportID string

	// Track the last-focused main-area (focus-eligible) viewport for
	// system-wide access.
	lastMainViewport *Viewport

	// lastFocusedBySet remembers the last-focused viewport WITHIN each
	// ViewportSet, keyed by set name (updated alongside lastMainViewport). Lets a
	// set — e.g. the "help" system — restore focus to where it last was,
	// independent of the document set.
	lastFocusedBySet map[string]*Viewport

	// Track the last focused non-docked (main-area) viewport, regardless of its
	// buffer type. Currently the only non-docked viewport painted is this one
	// (see the main layout). TODO: support additional tiling modes (split
	// panes, side-by-side, etc.) instead of only showing the last-focused one.
	lastNormalViewport *Viewport

	// Peek offsets for viewing hidden viewports
	StatPeek   int // For top-docked viewports
	PromptPeek int // For bottom-docked viewports

	// Viewport type counters for auto-naming
	mainBufferCount   int
	workBufferCount   int
	promptBufferCount int

	// Monotonic creation sequence counter (see Viewport.Seq)
	seqCounter int64

	// Event handlers
	eventHandlers map[EventType][]EventHandler
}

// NewManager creates a new viewport manager.
func NewManager() *Manager {
	return &Manager{
		viewports:     make(map[string]*Viewport),
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

// ViewportOptions configures a new viewport.
type ViewportOptions struct {
	ID              string
	Type            ViewportType
	Class           string
	ViewportSet     string
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
	ShowMarks       string // "no" | "yes" | "all"
	OverwriteMode   bool   // inverse of insertMode; zero value = insert
	ReadOnly        bool
	AutoIndent      bool // replicate the split line's indent on insert_newline
	LinkBrowsing    bool // hyperlink layer (link colors, browse-mode buttons)
	ShowRuler       bool
	Scrollbar       bool // reserve the outer column for a vertical scrollbar
	TabSize         int
	SyntaxOverrides string // space-separated grammar flavors that skip the project folder
	SetFocus        bool
	CustomRenderer  string
	AfterKey        string // after-key pseudo-binding (see Viewport.AfterKey)

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

// CreateViewport creates a new viewport with the given options.
func (m *Manager) CreateViewport(opts ViewportOptions) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate ID if not provided
	id := opts.ID
	if id == "" {
		switch opts.Type {
		case DocViewport:
			m.mainBufferCount++
			id = fmt.Sprintf("main_%d", m.mainBufferCount)
		case ToolViewport:
			m.workBufferCount++
			id = fmt.Sprintf("work_%d", m.workBufferCount)
		case PromptViewport:
			m.promptBufferCount++
			id = fmt.Sprintf("prompt_%d", m.promptBufferCount)
		default:
			id = fmt.Sprintf("viewport_%d", len(m.viewports)+1)
		}
	}

	// Set defaults
	visible := opts.Visible
	if !opts.Visible && opts.ID == "" {
		visible = true // Default to visible for new viewports
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
	w := &Viewport{
		ID:           id,
		Seq:          m.seqCounter,
		Type:         opts.Type,
		Class:        opts.Class,
		ViewportSet:  opts.ViewportSet,
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
			ShowMarks:       opts.ShowMarks,
			OverwriteMode:   opts.OverwriteMode,
			ReadOnly:        opts.ReadOnly,
			AutoIndent:      opts.AutoIndent,
			LinkBrowsing:    opts.LinkBrowsing,
			ShowRuler:       opts.ShowRuler,
			Scrollbar:       opts.Scrollbar,
			TabSize:         opts.TabSize,
			SyntaxOverrides: opts.SyntaxOverrides,
		},
		ScrollbarX:          -1, // no bar until the renderer lays one out
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
		AfterKey:            opts.AfterKey,
	}
	// Viewports are focus-cycle stops by default; a caller lowers CanFocus later
	// for a viewport that explicit focus may still reach but the switcher skips.
	w.CanFocus = true

	// Calculate line number width if showing line numbers
	if w.ViewState.ShowLineNumbers && w.Buffer != nil {
		lineCount := w.Buffer.GetLineCount()
		if lineCount < 10 {
			lineCount = 10
		}
		w.LineNumWidth = len(fmt.Sprintf("%d", lineCount)) + 1
		// Browse mode rounds the gutter to an even width so double-width rows
		// can halve it cleanly (see renderContent); the render pass recomputes
		// this each frame, this keeps the initial value consistent.
		if w.BrowseActive && w.LineNumWidth%2 != 0 {
			w.LineNumWidth++
		}
	}

	// Prompt buffers always edit left-to-right regardless of the editor's
	// base direction option.
	if opts.Type == PromptViewport {
		w.ViewState.Direction = "ltr"
	}

	// Track which main buffer spawned a new prompt buffer: inherit the focused
	// viewport's ParentViewport when it has one (e.g. a prompt spawning another
	// prompt), otherwise the last main buffer.
	if opts.Type == PromptViewport {
		if focused := m.viewports[m.focusedViewportID]; focused != nil && focused.ParentViewport != nil {
			w.ParentViewport = focused.ParentViewport
		} else if m.lastMainViewport != nil {
			if _, exists := m.viewports[m.lastMainViewport.ID]; exists {
				w.ParentViewport = m.lastMainViewport
			}
		}
	}

	// Bind the viewport to its buffer: mint the caret, viewport anchor, and
	// edit-trail cursors that slide with edits (released in RemoveViewport).
	w.bindBuffer(opts.Buffer)

	// A buffer becoming actively bound is no longer at risk of orphaning:
	// purge it from every viewport's graveyard.
	if opts.Buffer != nil {
		for _, other := range m.viewports {
			other.Unbury(opts.Buffer)
		}
	}

	m.viewports[id] = w

	// Set focus if requested or if this is the first viewport
	if opts.SetFocus || m.focusedViewportID == "" {
		m.focusedViewportID = id
	}

	// Update main buffer tracking. Only a viewport that actually took focus
	// becomes the current one — a background viewport (e.g. the verbose log)
	// must not steal the painted main area or the modebar.
	tookFocus := m.focusedViewportID == id
	if tookFocus && w.FocusEligible() {
		m.noteMainFocus(w)
	} else if tookFocus && w.Type == PromptViewport && w.ParentViewport != nil {
		// A focused prompt keeps its spawning viewport as the last main-area
		// viewport (and the painted one) — see SetFocus.
		m.noteMainFocus(w.ParentViewport)
		if w.ParentViewport.Dock == DockNone {
			m.lastNormalViewport = w.ParentViewport
		}
	}
	// Update non-docked (main-area) viewport tracking, regardless of buffer type.
	if opts.Dock == DockNone && tookFocus {
		m.lastNormalViewport = w
	}

	// Emit event (outside lock)
	go m.emit(Event{
		Type:       EventViewportCreated,
		ViewportID: id,
		NewValue:   w,
	})

	return id
}

// GetViewport returns a viewport by ID.
func (m *Manager) GetViewport(id string) *Viewport {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.viewports[id]
}

// GetFocusedViewport returns the currently focused viewport.
func (m *Manager) GetFocusedViewport() *Viewport {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.focusedViewportID == "" {
		return nil
	}
	return m.viewports[m.focusedViewportID]
}

// SetFocus sets focus to a viewport.
func (m *Manager) SetFocus(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	w, exists := m.viewports[id]
	if !exists {
		return false
	}

	// Chrome (the modebar) cannot receive focus; doc/tool/prompt viewports can.
	if isChromeSet(w.ViewportSet) {
		return false
	}

	oldFocusID := m.focusedViewportID
	m.focusedViewportID = id

	// Update main-area viewport tracking
	if w.FocusEligible() {
		m.noteMainFocus(w)
	} else if w.Type == PromptViewport && w.ParentViewport != nil {
		// Focusing a prompt restores its spawning viewport as the last
		// main-area viewport — and as the painted one — so the parent becomes
		// visible behind the prompt.
		m.noteMainFocus(w.ParentViewport)
		if w.ParentViewport.Dock == DockNone {
			m.lastNormalViewport = w.ParentViewport
		}
	}
	// Update non-docked (main-area) viewport tracking, regardless of buffer type.
	if w.Dock == DockNone {
		m.lastNormalViewport = w
	}

	go m.emit(Event{
		Type:       EventFocusChanged,
		ViewportID: id,
		OldValue:   oldFocusID,
		NewValue:   id,
	})

	return true
}

// RemoveViewport removes a viewport. If the removed viewport was focused, the focus
// handoff is routed through SetFocus so tracking, parent-main restoration, and
// focus events behave like any other focus change.
func (m *Manager) RemoveViewport(id string) bool {
	m.mu.Lock()

	w, exists := m.viewports[id]
	if !exists {
		m.mu.Unlock()
		return false
	}

	// Clear last main buffer tracking if this was it
	if m.lastMainViewport != nil && m.lastMainViewport.ID == id {
		m.lastMainViewport = nil
	}
	// Clear last non-docked viewport tracking if this was it
	if m.lastNormalViewport != nil && m.lastNormalViewport.ID == id {
		m.lastNormalViewport = nil
	}
	// Clear per-set last-focused tracking if this was it
	for set, fw := range m.lastFocusedBySet {
		if fw != nil && fw.ID == id {
			delete(m.lastFocusedBySet, set)
		}
	}
	// Clear any ParentViewport references to the removed viewport
	for _, other := range m.viewports {
		if other.ParentViewport == w {
			other.ParentViewport = nil
		}
	}

	// Release the viewport's cursors so garland stops adjusting them on edits —
	// the active binding and every binding stacked in its nav history.
	w.releaseBinding()
	w.releaseNavHistory()

	delete(m.viewports, id)

	wasFocused := m.focusedViewportID == id
	nextFocus := ""
	if wasFocused {
		m.focusedViewportID = ""
		nextFocus = m.removalFocusTargetLocked(w)
	}

	m.mu.Unlock()

	if nextFocus != "" {
		m.SetFocus(nextFocus)
	}

	go m.emit(Event{
		Type:       EventViewportRemoved,
		ViewportID: id,
		OldValue:   w,
		NewValue:   wasFocused,
	})

	return true
}

// removalFocusTargetLocked chooses the viewport to focus after the focused
// viewport `closed` was removed (which has already happened). Must be called
// with the lock held.
//
// The anchor main buffer is the tracked lastMainViewport when it still
// exists — for a closed prompt that is its spawning main, since focusing a
// prompt restores its ParentViewport there. When the closed viewport WAS the last
// main buffer (tracking already cleared), the anchor falls back to the main
// created next after it (the lowest Seq above closed.Seq), and failing that
// the main with the highest remaining Seq. Focus then goes to the anchor's
// newest (highest-Seq) prompt buffer if it has one — popping the anchor back
// to the top via SetFocus — else the anchor itself.
func (m *Manager) removalFocusTargetLocked(closed *Viewport) string {
	// Validate the tracked last main buffer.
	var anchor *Viewport
	if m.lastMainViewport != nil {
		if _, ok := m.viewports[m.lastMainViewport.ID]; ok {
			anchor = m.lastMainViewport
		}
	}

	// No usable last main buffer: next main by creation order, then highest.
	if anchor == nil {
		var nextHigher, highest *Viewport
		for _, w := range m.viewports {
			if !w.FocusEligible() || !w.Visible {
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
		var newest *Viewport
		for _, w := range m.viewports {
			if w.Type == PromptViewport && w.Visible && (newest == nil || w.Seq > newest.Seq) {
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
	for _, w := range m.viewports {
		if w.Type == PromptViewport && w.Visible && w.ParentViewport == anchor {
			if target == anchor || w.Seq > target.Seq {
				target = w
			}
		}
	}
	return target.ID
}

// focusCycleTarget returns the ID of the viewport to focus when cycling offset
// steps (with wraparound), or "" when there is nowhere to go.
//
// The cycle runs over visible MAIN buffers only; prompt buffers are not cycle
// stops of their own. The current position is the focused main buffer, or the
// focused viewport's ParentViewport (a prompt counts as being "on" its spawning
// main). Once the target main is chosen, focus resolves to the newest
// (highest-Seq) visible prompt buffer spawned from it, if any — landing the
// user on that buffer's pending interaction — else the main itself.
func (m *Manager) focusCycleTarget(offset int) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect visible focus-eligible viewports in deterministic (ID) order. A
	// viewport with CanFocus=false (e.g. Quick Help) is skipped as a cycle stop,
	// though explicit focus can still reach it.
	var mains []*Viewport
	for _, w := range m.viewports {
		if w.FocusEligible() && w.Visible && w.CanFocus {
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
	currentMain := m.lastMainViewport
	if current := m.viewports[m.focusedViewportID]; current != nil {
		if current.FocusEligible() {
			currentMain = current
		} else if current.ParentViewport != nil {
			currentMain = current.ParentViewport
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
	target := m.cycleResolveLocked(targetMain)

	if target.ID == m.focusedViewportID {
		return ""
	}
	return target.ID
}

// cycleResolveLocked resolves a cycle-target main to what the focus switcher
// actually lands on: the main's newest visible prompt buffer when one is open,
// else the main itself. Caller must hold m.mu (read or write).
func (m *Manager) cycleResolveLocked(targetMain *Viewport) *Viewport {
	var newestPrompt *Viewport
	for _, w := range m.viewports {
		if w.Type == PromptViewport && w.Visible && w.ParentViewport == targetMain {
			if newestPrompt == nil || w.Seq > newestPrompt.Seq {
				newestPrompt = w
			}
		}
	}
	if newestPrompt != nil {
		return newestPrompt
	}
	return targetMain
}

// FocusViewportAsCycle focuses w exactly the way the focus switcher
// (FocusNextViewport / ^B N) would land on it: only if w is a cycle stop
// (focus-eligible, visible, CanFocus), and resolving to w's newest open
// prompt buffer when it has one. Reports whether focus changed. Used by
// mouse routing: a click on an unfocused cycle-reachable viewport focuses it
// like a cycle step.
func (m *Manager) FocusViewportAsCycle(w *Viewport) bool {
	if w == nil {
		return false
	}
	m.mu.RLock()
	eligible := w.FocusEligible() && w.Visible && w.CanFocus
	var target *Viewport
	if eligible {
		target = m.cycleResolveLocked(w)
	}
	focused := m.focusedViewportID
	m.mu.RUnlock()
	if target == nil || target.ID == focused {
		return false
	}
	return m.SetFocus(target.ID)
}

// FocusNextViewport cycles focus to the next focusable viewport. The switch is
// routed through SetFocus so focus tracking and events behave identically to
// any other focus change.
func (m *Manager) FocusNextViewport() bool {
	target := m.focusCycleTarget(1)
	if target == "" {
		return false
	}
	return m.SetFocus(target)
}

// FocusPrevViewport cycles focus to the previous focusable viewport. The switch is
// routed through SetFocus so focus tracking and events behave identically to
// any other focus change.
func (m *Manager) FocusPrevViewport() bool {
	target := m.focusCycleTarget(-1)
	if target == "" {
		return false
	}
	return m.SetFocus(target)
}

// GetViewportsByDock returns all visible viewports in a dock position, sorted by priority.
// For top dock: sorted descending (higher priority renders first, at screen top).
// For other docks: sorted ascending (lower priority renders first).
func (m *Manager) GetViewportsByDock(dock DockPosition) []*Viewport {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Viewport
	for _, w := range m.viewports {
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

// GetLastMainViewport returns the last focused main buffer viewport.
func (m *Manager) GetLastMainViewport() *Viewport {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.lastMainViewport != nil {
		if _, exists := m.viewports[m.lastMainViewport.ID]; exists {
			return m.lastMainViewport
		}
		m.lastMainViewport = nil
	}

	// Find any focus-eligible viewport
	for _, w := range m.viewports {
		if w.FocusEligible() {
			return w
		}
	}

	return nil
}

// GetLastNormalViewport returns the last focused non-docked (main-area) viewport,
// regardless of buffer type. Modeled on GetLastMainViewport.
func (m *Manager) GetLastNormalViewport() *Viewport {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.lastNormalViewport != nil {
		if _, exists := m.viewports[m.lastNormalViewport.ID]; exists {
			return m.lastNormalViewport
		}
		m.lastNormalViewport = nil
	}

	// Find any non-docked viewport
	for _, w := range m.viewports {
		if w.Dock == DockNone {
			return w
		}
	}

	return nil
}

// UpdateViewport updates a viewport's properties.
func (m *Manager) UpdateViewport(id string, updates func(*Viewport)) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	w, exists := m.viewports[id]
	if !exists {
		return false
	}

	updates(w)

	go m.emit(Event{
		Type:       EventViewportUpdated,
		ViewportID: id,
		NewValue:   w,
	})

	return true
}

// CreatePromptViewport creates a prompt buffer for user input.
func (m *Manager) CreatePromptViewport(prompt, defaultValue string, callback func(string, bool)) string {
	// Find highest priority bottom viewport. A bottom-located modebar is
	// excluded: its fixed priority pins it to the last screen line, and
	// prompts must stack above it, not outbid it.
	bottomViewports := m.GetViewportsByDock(DockBottom)
	highestPriority := 0
	for _, w := range bottomViewports {
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

	id := m.CreateViewport(ViewportOptions{
		Type:        PromptViewport,
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
	if w, exists := m.viewports[id]; exists {
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

// StatPeekUp increases visibility of top dock viewports.
func (m *Manager) StatPeekUp() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	topViewports := m.getViewportsByDockLocked(DockTop)
	if m.StatPeek < len(topViewports)-1 {
		m.StatPeek++
		go m.emit(Event{
			Type:     EventStatPeekChanged,
			NewValue: m.StatPeek,
		})
		return true
	}
	return false
}

// StatPeekDown decreases visibility of top dock viewports.
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

// PromptPeekUp increases visibility of bottom dock viewports.
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

// PromptPeekDown decreases visibility of bottom dock viewports.
func (m *Manager) PromptPeekDown() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	bottomViewports := m.getViewportsByDockLocked(DockBottom)
	if m.PromptPeek < len(bottomViewports)-1 {
		m.PromptPeek++
		go m.emit(Event{
			Type:     EventPromptPeekChanged,
			NewValue: m.PromptPeek,
		})
		return true
	}
	return false
}

// getViewportsByDockLocked is the internal version without locking.
func (m *Manager) getViewportsByDockLocked(dock DockPosition) []*Viewport {
	var result []*Viewport
	for _, w := range m.viewports {
		if w.Dock == dock && w.Visible {
			result = append(result, w)
		}
	}
	return result
}

// AllViewports returns all viewports in deterministic order (sorted by ID).
func (m *Manager) AllViewports() []*Viewport {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Viewport, 0, len(m.viewports))
	for _, w := range m.viewports {
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
