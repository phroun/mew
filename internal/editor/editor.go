// Package editor provides the core text editor orchestration.
package editor

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/phroun/ifitfits"
	"github.com/phroun/kittytk/hostterm"
	"github.com/phroun/pawscript"

	"github.com/phroun/mew/internal/bidi"
	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/config"
	"github.com/phroun/mew/internal/input"
	"github.com/phroun/mew/internal/jsf"
	"github.com/phroun/mew/internal/keys"
	"github.com/phroun/mew/internal/plugins"
	"github.com/phroun/mew/internal/render"
	"github.com/phroun/mew/internal/textwidth"
	"github.com/phroun/mew/internal/version"
	"github.com/phroun/mew/internal/viewport"
)

// statusWriter captures stderr output and stores it for display.
type statusWriter struct {
	editor *Editor
	mu     sync.Mutex
	buf    bytes.Buffer
}

func (sw *statusWriter) Write(p []byte) (n int, err error) {
	// A running launch-eval batch captures #err instead (see eval.go).
	if sw.editor.evalCaptureWrite(true, p) {
		return len(p), nil
	}

	sw.mu.Lock()
	defer sw.mu.Unlock()

	sw.buf.Write(p)

	// Check for complete lines
	content := sw.buf.String()
	if idx := strings.LastIndex(content, "\n"); idx != -1 {
		// Extract lines and combine them into a single message
		lines := strings.Split(content[:idx], "\n")
		var messageParts []string
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				messageParts = append(messageParts, line)
			}
		}
		if len(messageParts) > 0 {
			// Join all lines with spaces and show as notification
			fullMessage := strings.Join(messageParts, " ")
			sw.editor.ShowError(fullMessage)
		}
		// Keep any remaining partial line
		sw.buf.Reset()
		if idx+1 < len(content) {
			sw.buf.WriteString(content[idx+1:])
		}
	}

	return len(p), nil
}

// insertWriter captures stdout output and inserts it at the cursor position.
type insertWriter struct {
	editor *Editor
	mu     sync.Mutex
	buf    bytes.Buffer
}

func (iw *insertWriter) Write(p []byte) (n int, err error) {
	// A running launch-eval batch captures #out instead (see eval.go).
	if iw.editor.evalCaptureWrite(false, p) {
		return len(p), nil
	}

	iw.mu.Lock()
	defer iw.mu.Unlock()

	iw.buf.Write(p)

	// Insert complete content (including newlines) at cursor
	content := iw.buf.String()
	if content != "" {
		iw.editor.insertText(content)
		iw.buf.Reset()
	}

	return len(p), nil
}

// Editor is the main editor instance that orchestrates all components.
type Editor struct {
	// Core components
	ViewportManager *viewport.Manager
	LayoutManager   *viewport.LayoutManager
	Renderer        *render.ScreenRenderer
	KeyProcessor    *keys.SequenceProcessor
	KeyHandler      input.Source
	PawScript       *pawscript.PawScript
	PromptMgr       *PromptManager

	// pawConfig is the live PawScript configuration; DefaultTokenTimeout is
	// read at each host-level token request, so runtime option changes
	// (set_option scriptTimeout) apply immediately.
	pawConfig *pawscript.Config

	// tiler is the ifitfits viewport-tiling engine that arranges the main
	// (non-docked) editing area. It holds tiles, each carrying a ref that is a
	// mew viewport id (or empty → blank); the render path lays out each tile and
	// draws the viewport its ref names. Focus is kept in sync through
	// tilerFollowFocus (the manager's main-focus hook). Built lazily; see
	// ensureTiler and applyTilerGeometry.
	tiler *ifitfits.Viewport

	// adoptFocusInPlace makes tilerFollowFocus reseat the FOCUSED tile onto a
	// newly-focused untiled viewport instead of splitting a new tile for it. Set
	// only for the brief span of a cycle to an existing viewport (buffer_next/
	// prior, viewport_next/prior), so cycling onto an untiled background buffer
	// shows it in the current pane rather than growing the tiling — while the
	// commands that CREATE a viewport (buffer_duplicate, viewport_clone, an eval
	// or cross-root wiki page, startup) keep splitting a fresh tile as before.
	adoptFocusInPlace bool

	// mainTiles is the last frame's laid-out main-area tiles, retained for mouse
	// hit-testing: geometry lives with the tile (a viewport can appear in several
	// tiles, many-to-many), so a click is resolved against these per-tile frames
	// rather than the viewport's own single-valued geometry. Written by
	// performRender, read by viewportAt — both on the editor goroutine.
	mainTiles []viewport.ViewportLayout

	// pageSizeSpec is the paging spec built from the three page options,
	// rebuilt when any of them changes so page distance updates live.
	pageSizeSpec pageSizeSpec

	// Plugins
	Modebar      *plugins.ModebarPlugin
	ColumnRuler  *plugins.ColumnRulerPlugin
	ConfigMgr    *config.Manager
	LoadedConfig config.Config

	// mappingOrigins is provenance for the ACTIVE keymap (the one pushed to
	// KeyProcessor), keyed by key sequence: which config file/line bound it,
	// its load-order precedence, and its @author. Kept parallel to the keymap
	// so the low-level keys package stays provenance-free. A sequence absent
	// here is a built-in (resolve as config.AuthorSystem, precedence 0). See
	// keyBindingDisplay (key-badge "last configured" tie-break) and the `map`
	// command (runtime remaps recorded as config.AuthorRemapped).
	mappingOrigins map[string]config.MappingOrigin
	// remapPrec advances above every config precedence so a runtime remap
	// always outranks a config-file binding when tie-breaking.
	remapPrec int

	// Configuration
	Config Config

	// FS is the file system used for all document I/O.
	FS FileSystem
	// usingOSFS records that FS is the real OS: file loads then go through
	// Garland's own lazy warm-storage path instead of reading whole files.
	usingOSFS bool

	// mew accesses the "box:///" storage tree (config, profile, syntax, native
	// locks, crash dumps) — virtualized or mapped to <home>/.mew. home is the
	// resolved home directory (host override or OS), used for "~" expansion.
	mew  *mewVFS
	home string

	// lib is this editor's own garland-backed buffer library (per-instance, so
	// many mews coexist in one process). coldDir is the unique cold-storage
	// subfolder it owns, removed on Cleanup ("" if it shares a base directory).
	lib     *buffer.Library
	coldDir string

	// State
	Running        bool
	ActiveSequence string
	// activeCompletions holds the current key-sequence autocompletion text shown
	// in the modebar. It is transient key-sequence state, not viewport context.
	activeCompletions string

	// quickHelpTopic is the context-sensitive help topic for the current key
	// prefix — the deepest "help" virtual binding matching the active sequence
	// (e.g. "" at rest resolves the root "help =", "^B" resolves "^B help ="). It
	// names the help wiki page Quick Help shows; empty falls back to the built-in
	// reference. It ONLY drives Quick Help — the main help ("Using mew") ignores
	// it entirely.
	quickHelpTopic string
	// quickHelpMode marks the docked help viewport as Quick Help (the dynamic,
	// context-following page) rather than an ordinary help page reached by
	// browsing. It is what the "Quick Help" checkmark reflects, and what keeps
	// Quick Help identifiable apart from a normal help page showing the same URL.
	quickHelpMode bool
	// quickHelpBuf is the buffer currently displayed AS Quick Help, and
	// quickHelpShownTopic the topic it was built from. If the help viewport's
	// buffer drifts from quickHelpBuf the user browsed away (link/history/Using
	// mew), which ends quick mode; if quickHelpTopic drifts from
	// quickHelpShownTopic the context changed and Quick Help re-navigates.
	quickHelpBuf        *buffer.Buffer
	quickHelpShownTopic string

	// Cross-buffer find state (the find command's "a" option). When set,
	// find_next continues across all main buffers instead of using the
	// focused viewport's per-viewport find state.
	globalFind    viewport.FindState
	globalFindSet bool

	// Render debouncing
	renderTimer    *time.Timer
	lastRenderTime time.Time
	// renderRequested is atomic: RequestRender is reachable from goroutines
	// outside the main loop (PawScript's async token timeouts log warnings
	// through statusWriter -> ShowError -> RequestRender).
	renderRequested atomic.Bool

	// renderMu serializes performRender. It runs on the main loop AND on the
	// renderer's resize goroutine (SetOnResize -> performRender, because the main
	// loop is blocked in GetEvent), so without this two full renders can execute
	// at once — at startup the initial greeting render races the render from the
	// host's first resize — and their shared editor-level state (option
	// reconciliation, layout, modebar/ViewState, plugin maps) is written
	// concurrently, which Go turns into a fatal "concurrent map write" that
	// crashes the process (and garbles the terminal on the way out). No caller
	// holds it and performRender never recurses, so a plain Lock is deadlock-free.
	renderMu sync.Mutex

	// pendingScreenCapture, when non-empty, is a box:/// target the next
	// performRender writes a full-frame ANSI snapshot to (the debug_screen
	// command). Set and read under renderMu, so the capture rides the natural
	// render loop — snapshotting the exact frame just painted — rather than a
	// command re-rendering (and re-locking renderMu) itself.
	pendingScreenCapture string

	// appliedFocusedSig is the focused viewport's overlay signature whose
	// focused-scoped options (modebar templates/location, macOptionKeys, key
	// mappings) are currently applied. Re-derived when it changes.
	appliedFocusedSig string

	// flipBidiForHost=auto probe state (see bidiprobe.go). realTerminal is
	// whether output goes to the actual terminal (probing a virtualized host's
	// capture buffer is meaningless — hosts set the option explicitly).
	bidiProbeState    int
	bidiProbeDeadline time.Time
	realTerminal      bool

	// probeCapable is whether the terminal answers escape-sequence queries back
	// through Input: a real terminal (realTerminal) or a virtualized one the
	// host marked Interactive (a genuine emulator surface). It gates features
	// that handshake with the terminal — the pixel mouse (?1016) — on/off, so a
	// dead capture buffer in a test is never probed. Distinct from realTerminal,
	// which also governs raw-mode and other own-the-tty behaviors.
	probeCapable bool

	// kittyFlipActive is true when the host is Kitty AND mew is flipping RTL for
	// it (force_ltr off — the path where Kitty may drop niqqud). It gates the
	// force_ltr nudge; see updateNiqqudNudge / kittyconf.go.
	kittyFlipActive bool
	// force_ltr nudge hysteresis. The nudge occupies a screen row, so a niqqud
	// line sitting exactly on the viewport's bottom boundary can oscillate
	// (nudge on -> line scrolls off -> condition false -> nudge off -> line back).
	// After a short run of frame-to-frame toggles we latch the nudge on until the
	// layout genuinely changes (resize/scroll/edit/viewport set), tracked by sig.
	niqqudNudgeWantPrev  bool
	niqqudNudgeToggleRun int
	niqqudNudgeLatched   bool
	niqqudNudgeLatchSig  string
	// appliedMappingSet is the mapping-set name currently loaded into the key
	// processor, so an unchanged set is not rebuilt.
	appliedMappingSet string

	// ptySessions binds a VIEWPORT to the host-provided terminal session it is
	// running, and ptyMu guards it: the read loop lives on the session's own
	// goroutine while commands touch the map from the main loop. Keying on the
	// viewport (not the buffer) is what lets a cloned viewport show plain buffer
	// text in one tile while the terminal draws in the origin, and lets two
	// sessions run against one buffer from two viewports; each ptyState still
	// remembers the buffer it launched in (ptyState.buf) for where its capture
	// lands. See pty.go.
	ptyMu       sync.Mutex
	ptySessions map[*viewport.Viewport]*ptyState
	ptySeq      int
	// terminalSurfacesSent is the last set pushed to the host, so an idle
	// frame republishes nothing.
	terminalSurfacesSent []TerminalSurface
	// ptyMouseCapture is the terminal gesture (a scrollbar drag, a selection)
	// held from press to release: which buffer's terminal took it, and the
	// content rectangle of the exact tile the press landed in. A viewport can be
	// shown in several tiles of different sizes, so the gesture pins its geometry
	// to the pressed tile rather than re-reading the viewport's canonical one,
	// which would drift a drag that began in a mirror. Touched only on the main
	// loop under renderMu.
	ptyMouseCapture *ptyMouseGesture
	// rawKeyArmed is the raw_key_input one-shot: the NEXT keystroke belongs to
	// a focused terminal's child process rather than to mew's keymap. Cleared
	// by that keystroke whether or not a terminal was there to take it.
	rawKeyArmed bool
	// dispatchingKey is the key event whose bound command is currently
	// executing — what tinput_key encodes when a capture forwards "this key"
	// to a hosted terminal. Maintained as a save/restore stack by
	// runBoundCommand, because a sequence unwind executes several keys'
	// commands within one dispatch. Empty outside key dispatch (menu
	// actions, scripts), where tinput_key without an argument declines.
	dispatchingKey string

	// Paste transaction state. A bracketed paste arrives as multiple chunks
	// across several event-loop iterations; the whole paste is grouped into one
	// undo revision by opening a command transaction on the first chunk and
	// ending it on the final chunk. pasteBuf is the buffer the transaction was
	// opened on, so it is ended on the same buffer even if focus changes.
	pasteActive bool
	pasteBuf    *buffer.Buffer

	// Kill ring: a global ring of killed text, each entry its own garland so
	// text and marks travel together (killRing[0] is the most recent).
	// killPopIdx is kill_ring_pop's rotation index; killAppendNext arms the
	// next kill to accumulate into the head entry regardless of position;
	// pendingKill/lastEditKill implement the "consecutive deletes in the same
	// edit share an entry" rule (trackEdit shifts pending into last); lastYank
	// remembers the most recent yank's extent for kill_ring_pop.
	killRing       []*buffer.KillBuffer
	killPopIdx     int
	killAppendNext bool
	pendingKill    bool
	lastEditKill   bool
	lastYank       yankRecord

	// Per-command signals set by the mutation implementations, read after
	// dispatch — so the decision follows what a command DID, never its name
	// (which the script language can rename or chain). editCoalesced marks a
	// single-point edit that garland coalesces into the current undo run (so
	// the run is not baked shut afterward); yankedThisCommand marks a kill-ring
	// yank/pop (so lastYank stays valid for a following pop). Both are reset
	// before each command runs.
	editCoalesced     bool
	yankedThisCommand bool

	// Visited hyperlinks, editor-wide, keyed by RESOLVED identity (the
	// canonical URL a follow resolves to, or the raw target for gated/
	// unresolvable refs). Editor-level because two spellings in two buffers
	// that resolve to one file are one destination: visiting it from anywhere
	// paints it visited everywhere. linkVisitLog is the chronological record
	// (key + timestamp). linkResolveCache memoizes PAINT-TIME resolution of
	// (source doc, raw target) -> visit key, so the recent-style decision
	// never re-walks the filesystem per frame; navFollow itself always
	// resolves fresh (see visitKeyFor).
	linkVisitSeen    map[string]bool
	linkVisitLog     []linkVisit
	linkResolveCache map[string]string

	// Mouse state: the last reported pointer position (the key layer emits a
	// Mouse@x,y position before each press/release/scroll), the link button
	// currently held down, and the link under the pointer (hover; delivered
	// only by hosts with all-motion tracking, e.g. the graphical build). See
	// mouse.go.
	mouseX, mouseY int
	// mouseSubX is the pointer's sub-cell horizontal offset in permille
	// (0..999) from the last pixel-resolution mouse report, or -1 for a
	// cell-resolution report. It lets an insert-mode click land the caret on
	// the nearest cell edge. See pixelmouse.go.
	mouseSubX    int
	pixelMouse   pixelMouseState
	mousePressed pressedLink // the CAPTURED button (press-to-release)
	// mouseOnCaptured: the pointer is currently over the captured button
	// (paints pressed); dragging off reverts to the focused style while the
	// capture holds.
	mouseOnCaptured bool
	mouseHovered    pressedLink
	// pointerRegionSent / pointerArrowsSent are the last I-beam rectangle and
	// button-exclusion spans pushed through Config.PointerRegion (1-based
	// cells); pointerRegionPushed records whether anything was pushed yet, so
	// the first computed region is always sent even when it is the zero value.
	pointerRegionSent [4]int
	pointerArrowsSent []PointerArrowSpan
	// scrollbarRegionsSent is the last set published through
	// Config.ScrollbarRegions, so an unchanged frame pushes nothing.
	scrollbarRegionsSent   []ScrollbarRegion
	scrollbarRegionsPushed bool
	pointerRegionPushed    bool
	// helpStateSent/helpStatePushed: the last built-in-help-viewport open state
	// pushed through Config.HelpState (see notifyHelpState).
	helpStateSent   bool
	helpStatePushed bool
	// Horizontal wheel barrier (mouse.go): a sideways scroll gesture only
	// engages after hScrollBarrier ticks accumulate in one direction, so a
	// stray sideways tick during a vertical scroll never nudges the view. A
	// vertical wheel tick, or a direction reversal, re-arms the barrier.
	hScrollAccum   int
	hScrollEngaged bool
	hScrollDir     int
	// Drag selection (mouse.go): a left press in the focused viewport's content
	// area arms this; dragging then marks the block (begin at the press
	// origin, end following the pointer). A shift+click arms it pre-begun
	// with the ORIGINAL caret position as the origin.
	dragSel dragSelState
	// dragTxnBuf is the buffer holding an open user-command transaction for
	// the CURRENT drag selection, so the whole gesture's mark movements
	// coalesce into ONE garland revision (one undo step) instead of one per
	// pointer cell. Opened when the drag first places marks, closed on
	// release. Like pasteBuf, the buffer is pinned at open so a mid-gesture
	// buffer swap cannot orphan the transaction.
	dragTxnBuf *buffer.Buffer
	// Drag-edge autoscroll (mouse.go): while a drag selection holds the
	// pointer beyond (or at) the viewport's edges, a ticker scrolls the view —
	// after a short delay, at a speed from the overshoot — and keeps
	// extending the selection. dragScrollPending throttles the ticker to one
	// posted tick in flight.
	dragScroll        dragScrollState
	dragScrollPending atomic.Bool
	// Modebar nav-history buttons (mouse.go): the captured button (0 none,
	// ModebarNavBack, ModebarNavFwd) held from press to release, whether the
	// pointer is currently over it (paints pressed), and the button under the
	// pointer for hover (graphical all-motion tracking only). Clicking a
	// modebar nav button never steals focus — nav operates on the focused
	// viewport.
	modebarNavCapture int
	modebarNavOn      bool
	modebarNavHover   int
	// Viewport scrollbar drag (mouse.go): a left press in a viewport's
	// reserved scrollbar column captures the gesture — track press jumps the
	// thumb center to the pointer, thumb press anchors the grab offset — and
	// drags scroll through scrollViewTo until release. Like the wheel, a
	// scrollbar click scrolls without stealing focus.
	sbDrag scrollbarDragState
	// readOnlySent/readOnlyPushed: the last focused-viewport read-only state
	// pushed through Config.EditState (see notifyEditState), and whether an
	// initial push has happened.
	readOnlySent   bool
	readOnlyPushed bool

	// Syntax highlighting (jsf grammars): the loader implements the search
	// path and interns grammar instances; synCaches holds per-buffer line
	// colors; synSGR memoizes color-class resolution (see syntaxhl.go).
	syntaxLoader *jsf.Loader
	// syntaxLoaderOverride resolves grammars with the project .mew/syntax layer
	// skipped, for flavors named in the syntaxOverrides option. It is a separate
	// loader so an overridden and a project-resolved instance of the same grammar
	// name never collide in one instance cache. A grammar must be highlighted
	// through the loader that produced it, so bufferGrammar returns both.
	syntaxLoaderOverride *jsf.Loader
	syntaxGrammar        *jsf.Instance
	// syntaxGrammarLoader is the loader that produced syntaxGrammar (the global
	// fallback grammar), honoring the editor-wide syntaxOverrides.
	syntaxGrammarLoader *jsf.Loader
	synCaches           map[*buffer.Buffer]*synCache
	synSGR              map[synColorKey]string

	// Outline breadcrumb state (see outline.go): compiled [outline.*]
	// patterns and the last computed breadcrumb.
	outlineREs     map[string]*regexp.Regexp
	outlineMemoVal *outlineMemo

	// Source-safety state (see sourcesafety.go): per-buffer captured notices
	// (shown as transients when they occur, re-exposed by buffer_status),
	// mew-native lock files held per buffer, and whether the loaded
	// configuration came from local disk (the [storage] trust gate).
	bufNotices     map[*buffer.Buffer][]bufferNotice
	mewLocks       map[*buffer.Buffer]string
	configFromDisk bool

	// mewLockDeferred records buffers whose mew-native lock was deliberately
	// NOT taken at open because the open was read-only (a viewer holds no
	// editing lock — the emacs school; garland's emacs locks are lazy the
	// same way). The recorded source path is used to acquire post-hoc at the
	// intent-to-edit boundary: the moment read-only is turned off for the
	// buffer (see ensureDeferredMewLock).
	mewLockDeferred map[*buffer.Buffer]string

	// Edit-time lock resolution. foreignLocks records a live foreign lock we
	// respected on open (emacs or mew-native); lockResolved marks a buffer whose
	// foreign-lock prompt the user has answered (steal or proceed), so the first
	// edit prompts exactly once.
	foreignLocks  map[*buffer.Buffer]foreignLockInfo
	lockResolved  map[*buffer.Buffer]bool
	lockPrompting bool

	// cliPSL holds the launch command line as parsed PSL (see cli.go), kept
	// for future script access to arbitrary command-line arguments.
	cliPSL interface{}

	// Launch evals (see eval.go): the --eval scripts queued by the launch walk
	// in file order, the ordered #out/#err capture that diverts PawScript
	// output while they run, and the focused viewport to restore when a batch
	// produces no output.
	launchEvals      []launchEval
	evalCap          evalCapture
	evalFocusRestore string

	// confirmKey holds the opening buffer size of each armed single-keystroke
	// confirmation prompt, by viewport ID (see confirmkey.go). isearch is the
	// live incremental-search state, nil when no search prompt is open (see
	// isearch.go).
	confirmKey map[string]confirmKeyBase
	isearch    *isearchState

	// findRun is the background find/find_next pass (see findasync.go), nil
	// until the first search of the session.
	findRun *findRun

	// afterKeyArmed is set by dispatchKey to the viewport owning the current
	// key event when that viewport carries an after-key pseudo-binding. The
	// FIRST command that completes during the dispatch — the bound command, or
	// the first key of an unwinding non-matching sequence landing as a normal
	// press — consumes it and runs the script (see executeCommandResult).
	// Disarmed without firing when the key resolved nothing (a silent prefix
	// step, an unmapped named key).
	afterKeyArmed *viewport.Viewport

	// deadcat is the crash-dump destination, resolved once at startup so the
	// death path never computes a path or makes a decision (see deadcat.go).
	deadcat deadcatPlan

	// launchDir is the working directory captured at startup — the fallback
	// anchor for filename completion in a fresh buffer (standalone mode).
	launchDir string
}

// PointerArrowSpan is one on-screen cell span, on a single row, that must show
// the ARROW rather than the text I-beam even though it lies inside the I-beam
// rectangle — a browse-mode link button. Row and Col are 1-based terminal
// cells; the span covers columns [Col, Col+Width).
type PointerArrowSpan struct {
	Row, Col, Width int
}

// ScrollbarRegion is one visible editor scrollbar, published to a graphical
// host that draws the bars itself (see Config.ScrollbarRegions).
//
// Col/Row are 1-based terminal cells: the reserved column, and the track's
// first row. TrackH is the track's length in cells — usually Page, but one
// short when the bar gave its bottom cell up to the screen's unwritable
// corner. Everything needed to size and place a thumb is here, so the host
// computes it in pixels without asking again:
//
//	thumb length  = TrackH * Page / (LineCount)   (clamped to a minimum)
//	thumb travel  = Top / (LineCount - Page)
//
// ViewportID names the window the host scrolls with scroll_viewport.
type ScrollbarRegion struct {
	ViewportID string
	Col, Row   int
	TrackH     int
	// Top is the first visible document line (0-based), Page the number of
	// lines the window shows, LineCount the document's length.
	Top, Page, LineCount int
}

// Config holds editor configuration options.
type Config struct {
	ShowLineNumbers bool
	// DeleteNewlineAsChar lets del_char_prior/del_char_next remove a line
	// terminator (join lines) like any other character. Default true; false makes
	// them decline at a line boundary. Per-viewport; prompt viewports pin it false.
	DeleteNewlineAsChar bool
	ShowColumnRuler     bool
	// Scrollbar reserves each doc/tool viewport's outer column for a
	// clickable vertical scrollbar (per-viewport option; default on).
	Scrollbar        bool
	RulerShowsCursor bool
	TabSize          int
	ShowInvisibles   bool
	ShowBidi         bool
	RtlCombining     bool   // show combining marks on RTL letters (default true)
	ShowMarks        string // "no" | "yes" | "all"
	OverwriteMode    bool   // inverse of insertMode; zero value = insert
	ReadOnly         bool
	AutoIndent       bool // insert_newline replicates the split line's indent
	// DECSCUSR cursor shapes per editing mode (0-6); see cursorStyleFor.
	InsertCursor     int
	OverwriteCursor  int
	NavigationCursor int
	LinkBrowsing     bool // hyperlink layer (link colors, browse-mode buttons)
	Syntax           string
	SyntaxDetect     bool
	SyntaxOverrides  string // space-separated grammar flavors that skip the project folder

	// go_match fallback context flags (see config.GeneralConfig).
	MatchIgnoresSingleQuote  bool
	MatchIgnoresDoubleQuote  bool
	MatchIgnoresSlashStar    bool
	MatchIgnoresSlashSlash   bool
	MatchIgnoresHash         bool
	MatchIgnoresDoubleHyphen bool
	MatchIgnoresSemicolon    bool
	MatchIgnoresPercent      bool

	// MacOptionKeys: "auto" / "true" / "false" (see config.GeneralConfig).
	MacOptionKeys string

	// FlipBidiForHost: "auto" (probe the terminal once, at first RTL content),
	// "true", or "false" (see config.GeneralConfig.FlipBidiForHost).
	FlipBidiForHost string

	// RtlMarkMode: "normal" (default) or "iterm2" — how an isolated RTL
	// combining mark on a dotted circle is emitted (see
	// config.GeneralConfig.RtlMarkMode). An enum; more modes are expected.
	RtlMarkMode string

	// Editing locks (see config.GeneralConfig): UseLocks gates all locking;
	// UseEmacsLocks additionally gates the emacs-interoperable lock files.
	UseLocks      bool
	UseEmacsLocks bool
	WordWrap      bool

	// Search defaults (JOE-compatible): SearchIgnoreCase mirrors -icase,
	// SearchWrap mirrors -wrap, SearchRegex mirrors -regex (standard regex
	// syntax by default instead of the JOE backslash syntax).
	SearchIgnoreCase bool
	SearchWrap       bool
	SearchRegex      bool

	// SearchBackwards / SearchAllBuffers are the persistent form of the find
	// command's "b" and "a" letters (see config.GeneralConfig).
	SearchBackwards  bool
	SearchAllBuffers bool

	// ModebarLocation places the modebar on the "top" (default) or
	// "bottom" screen line. ModebarInner/Default/Outer are the base modebar
	// templates; MappingsName is the base key-mapping set. All four are the
	// base for the focused viewport's overlay (see reconcileFocusedOptions).
	ModebarLocation string
	ModebarInner    string
	ModebarDefault  string
	ModebarOuter    string
	MappingsName    string

	// PromptTimeout is how long (in seconds) a prompt-suspended command
	// sequence stays resumable; answering the prompt after it expires
	// fails safe ("Prompt timed out"). ScriptTimeout bounds other
	// host-level async script tokens. 0 means never time out; the default
	// is 300 (5 minutes).
	PromptTimeout int
	ScriptTimeout int

	// Paging options (see config.GeneralConfig): the optimal page distance
	// (fixed count or percentage), the minimum overlap lines to keep, and a
	// step to round the distance to a multiple of.
	PageSizeOptimal    string
	PageOverlapMinimum string
	PageSizeStep       int

	// MaxRepeat bounds the count repeat_next will honor (larger requests are
	// clamped). See config.GeneralConfig.
	MaxRepeat int

	// KillRingEntries is how many kill-ring entries are retained (oldest
	// evicted past this). See config.GeneralConfig.
	KillRingEntries int

	// Direction is the base text direction every line begins in: "ltr"
	// (default) or "rtl". See config.GeneralConfig.
	Direction string

	// FS supplies the file system callbacks for document I/O (open, save,
	// insert, block write, globbing). Nil means the real OS file system.
	FS FileSystem

	// MewFS, when set, virtualizes mew's own storage tree (the "box:///" scheme —
	// editor.conf, profile.mew, syntax grammars, native locks, crash dumps):
	// mew:/x paths are handed to it verbatim. Nil maps mew:/ to <home>/.mew on
	// the real OS.
	MewFS FileSystem

	// HomeDir overrides the home directory mew resolves "~" and the local mew:/
	// root (<home>/.mew) against. "" uses the OS user home.
	HomeDir string

	// LogicalColumnTerminal marks the host terminal as a flex-width
	// (logical-grid) terminal — purfecterm honoring DECSET 2027 — whose cursor
	// addressing counts characters rather than visual cells. The renderer then
	// translates its CUP columns by the wide glyphs to the left of the target.
	// Set by the KittyTK trinket hosts; plain terminals leave it off.
	LogicalColumnTerminal bool

	// FontSink, when set, is invoked by the set_font command to re-point a
	// host font alias (e.g. "ui-term") at an ordered list of font names,
	// loading them if needed and repainting. It returns whether the preferred
	// (first) font resolved. Wired by graphical hosts (the KittyTK trinket to
	// its shared text engine); nil on a plain terminal, where set_font warns.
	FontSink func(alias string, names []string) bool

	// FontLoader, when set, is invoked ONCE at startup with the [fonts] map
	// (family name -> font file path) and the [window] fonts_path search
	// directories, so the host registers them into its font engine before any
	// alias resolves. Wired by graphical hosts alongside FontSink; nil on a
	// plain terminal, which owns its own fonts. The startup [window] ui_term
	// alias is applied through FontSink after this runs.
	FontLoader func(files map[string]string, searchPaths []string)

	// FontAdjustSink, when set, is invoked once at startup for each per-FACE
	// metric correction in the [fonts] section (family -> baseline units).
	// Keyed by family rather than alias so one entry corrects the face
	// however it is reached. Wired by graphical hosts; nil on a plain
	// terminal, whose faces are the terminal's own.
	FontAdjustSink func(family string, baselineUnits int, sizeScale float64)

	// RtlMarkModeSink, when set, is handed the rtlMarkMode value at startup and
	// whenever it changes, so a graphical host can mirror it into its own text
	// engine. Wired by graphical hosts; nil on a plain terminal.
	RtlMarkModeSink func(mode string)

	// PointerRegion, when set, publishes where a graphical host should show the
	// text I-beam: the FOCUSED viewport's editable content area (its cells,
	// including the blank rows below the document that still follow
	// click-to-EOF), in 1-based terminal cells (col, row, width, height); a
	// zero width/height means "nowhere" (arrow everywhere). Everything OUTSIDE
	// it — the gutter, the modebar and other chrome, an unfocused pane, and
	// (when a prompt holds focus) the document area — is the ordinary arrow.
	//
	// arrows are cell spans WITHIN that rectangle that must still show the
	// arrow, not the I-beam: the on-screen browse-mode link BUTTONS (the only
	// buttons that sit inside the text — the modebar's are already outside the
	// rect). The host shows the I-beam inside the rectangle EXCEPT on an arrow
	// span.
	//
	// It is pushed after each render, only when the region CHANGES (layout,
	// focus, scroll, or the on-screen buttons), NOT per mouse motion — so the
	// host answers per-pixel cursor queries locally, without every hover
	// round-tripping through mew's input stream. Called from the render path.
	PointerRegion func(col, row, width, height int, arrows []PointerArrowSpan)

	// ScrollbarRegions, when set, hands a GRAPHICAL host the geometry and
	// scroll state of every visible editor scrollbar, and by being set at all
	// declares that the host will DRAW them itself.
	//
	// mew still reserves the bar's column — the layout is the same either way,
	// so a window looks and measures identically on both targets — but leaves
	// it blank instead of painting '░'/'█' into the cell stream, and stops
	// hit-testing it. The host then paints the bar in its own pixel space
	// (where a thumb need not be a whole number of rows tall, and can move
	// smoothly under the pointer) and drives scrolling through
	// scroll_viewport, which still lands on a whole line.
	//
	// Pushed after each render, only when the set CHANGES — a bar appearing,
	// moving, resizing, or its view scrolling — never per mouse motion. Called
	// from the render path.
	ScrollbarRegions func([]ScrollbarRegion)

	// HelpState, when set, is told whether the built-in help viewport (the
	// WordStar command reference toggled by help_toggle) is open, once at the
	// first render and thereafter on transitions — so a host can keep a "Quick
	// Help" menu checkmark in sync with it. Called from the render path.
	HelpState func(open bool)

	// EditState, when set, is told whenever the FOCUSED viewport's read-only
	// state changes (and once at the first render), so a host can grey out
	// affordances that mutate — its Edit-menu Cut, say. Called only on
	// transitions, from the editor loop.
	EditState func(readOnly bool)

	// IdentityUser / IdentityHost / IdentityPID override the process identity
	// mew stamps into native lock files and shows in the "being edited by"
	// prompt. Empty strings / a zero PID use the OS (USER, hostname, getpid).
	IdentityUser string
	IdentityHost string
	IdentityPID  int

	// StateCallback, when set, is invoked once as the editor shuts down,
	// with a snapshot of the runtime state (current option values). Hosts
	// can persist it via config.EncodeState in PSL or JSON.
	StateCallback func(state map[string]interface{})

	// ShowDesktop / HideDesktop, when set, are invoked by the show_desktop /
	// hide_desktop commands. A host that embeds mew as a viewport-manager surface
	// (e.g. a KittyTK host) wires these to reveal or hide its desktop. Left
	// unset - as in the standalone editor - both commands are no-ops.
	ShowDesktop func()
	HideDesktop func()

	// ClipboardWrite / ClipboardRead, when set, bridge the HOST's system
	// clipboard for the os_copy / os_cut / os_paste commands — a separate
	// channel from mew's own kill ring, so the two never interfere.
	// ClipboardWrite receives the text mew places on the host clipboard.
	// ClipboardRead resolves the host clipboard and calls deliver exactly once
	// with the result; the resolution (and deliver) may happen asynchronously
	// on any thread — mew marshals the delivery back onto its own loop. Left
	// unset, the os_* clipboard commands warn that no host clipboard exists.
	ClipboardWrite func(text string)
	ClipboardRead  func(deliver func(text string))

	// ShowContextMenu, when set, is invoked when a right-click lands within
	// the EDITING AREA of the focused viewport (never the modebar, gutters,
	// column ruler, or title/message rows) with the click's 1-based terminal
	// cell. A host pops its context menu there (Cut/Copy/Paste/Select All,
	// wired back through a HostPort). Left unset, right-clicks are swallowed.
	ShowContextMenu func(col, row int)

	// HostPort, when set, is bound to the session at startup so the host can
	// inject editor commands (port.Execute("os_copy")) from its own threads:
	// each command is marshaled onto the editor main loop. See HostPort.
	HostPort *HostPort

	// PTYProvider, when set, is how mew obtains a terminal session: it asks,
	// naming a working directory and a command, and the host decides what
	// those mean - a real shell, a container, a menu, or a refusal. mew never
	// spawns a process itself and holds nothing that could. Left unset, the
	// exec command reports that this host grants no sessions. See pty.go.
	PTYProvider func(PTYRequest) (PTYSession, error)

	// TerminalSurfaces is how the host RENDERS those sessions. mew emulates
	// nothing: it forwards raw bytes and says where to draw them. See pty.go.
	TerminalSurfaces TerminalHooks

	// PTYDiagnose, when set, tests the host's own terminal plumbing and
	// returns a human-readable account of it, for the pty_diag command. mew
	// asks rather than investigates: every fact worth having about a terminal
	// that will not start is on the host's side of the pipe.
	PTYDiagnose func() string

	// RestoreHostTerminal, when set, hands the TERMINAL back to whatever put it
	// into raw mode / the alternate screen. mew calls it on the emergency exit
	// path, after the DEADCAT dump and before os.Exit.
	//
	// An embedded mew does not own the terminal: its renderer writes into the
	// host's surface, so Renderer.Cleanup restores nothing a user can see, and
	// the shell is left in raw mode with the host's keyboard protocol still on.
	// Only the host can undo that, and os.Exit runs none of its deferred
	// teardown, so the host has to be reachable from here.
	//
	// Called from a signal-handler goroutine: it must be safe off the main
	// loop, safe to call more than once, and must NOT exit - mew still has a
	// message to print.
	RestoreHostTerminal func()

	// SkipUserConfig prevents loading ~/.mew/editor.conf (built-in defaults
	// apply). For embedding hosts that must not touch the user's home dir.
	SkipUserConfig bool

	// SkipProfileScript prevents running (and creating) ~/.mew/profile.mew.
	SkipProfileScript bool

	// ConfigText, when non-nil, is parsed as the editor configuration
	// (editor.conf content) instead of reading ~/.mew/editor.conf. Note the
	// [storage] section is honored only from the local config file: a
	// host-supplied config string cannot redirect scratch storage - use
	// ColdStoragePath for that.
	ConfigText *string

	// ProfileScript, when non-nil, is executed as the startup pawscript
	// instead of loading (or creating) ~/.mew/profile.mew.
	ProfileScript *string

	// InitialState, when set, restores a state snapshot previously handed to
	// StateCallback, applied over the loaded configuration. Together with
	// ConfigText/ProfileScript and StateCallback, this lets a host act as
	// the full persistence go-between.
	InitialState map[string]interface{}

	// ColdStoragePath overrides the local directory handed to Garland for
	// cold storage. Empty means the local config's [storage] scratch value
	// (when the config came from disk), else the system temp directory.
	// Garland always receives a real local path, even for sandboxed hosts.
	ColdStoragePath string

	// DeadcatName opts a host into crash dumps (mew's DEADJOE): the name the
	// modified-buffer dump is written to through the host FileSystem when the
	// host calls DumpDeadcat during its own shutdown. Empty (the default) or a
	// standalone real-terminal session ignore it — the standalone editor
	// resolves its own DEADCAT location and installs signal handlers itself.
	DeadcatName string

	// Terminal virtualizes the editor's terminal I/O. Nil means the real
	// terminal (stdin/stdout, native resize signals).
	Terminal *TerminalIO

	// KeySource, when set, replaces the whole input half: the host delivers
	// parsed key and paste events itself (see input.EventFeed) instead of
	// mew running direct-key-handler over a byte stream. Terminal.Input is
	// ignored while a KeySource is in use; rendering, size, and resize stay
	// on Terminal.
	KeySource input.Source
}

// TerminalIO virtualizes the editor's terminal: where raw key input comes
// from, where rendered output goes, how the screen size is queried, and how
// size changes are signaled (the SIGWINCH stand-in). Any nil field keeps the
// real-terminal behavior for that aspect, except native OS resize signals,
// which are only watched when the whole struct is absent.
type TerminalIO struct {
	// Input is the raw key/paste byte stream (nil = os.Stdin). Raw terminal
	// mode is only engaged when this is a real terminal.
	Input io.Reader

	// Output receives the rendered terminal escape stream (nil = os.Stdout).
	Output io.Writer

	// Size queries the terminal dimensions (nil = query the real terminal).
	Size func() (width, height int, err error)

	// Resize, when non-nil, delivers terminal size-change signals from the
	// host: each receive re-queries Size and re-renders, doing manually what
	// SIGWINCH does on unix. (Editor.NotifyResize is the method equivalent.)
	Resize <-chan struct{}

	// Interactive marks a virtualized terminal that behaves like a real one:
	// it is a live surface whose replies to queries (DECRQM, XTWINOPS reports,
	// CPR) flow back through Input. A host that embeds mew in a genuine
	// terminal emulator (e.g. the KittyTK PurfecTerm surface) sets this so
	// terminal-probing features — pixel mouse (?1016) — engage exactly as they
	// do on a real terminal. It stays false for test harnesses and one-way
	// output sinks, which never answer and would only be polluted by the probe.
	Interactive bool
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		ShowLineNumbers:     true,
		DeleteNewlineAsChar: true,
		ShowColumnRuler:     true,
		Scrollbar:           true,
		TabSize:             4,
		ShowInvisibles:      false,
		ShowBidi:            false,
		RtlCombining:        true,
		ShowMarks:           "no",
		OverwriteMode:       false, // insertMode=yes
		ReadOnly:            false,
		WordWrap:            false,
		SearchWrap:          true,
	}
}

// New creates a new Editor instance.
func New(cfg Config) (*Editor, error) {
	// Create viewport manager
	wm := viewport.NewManager()

	// Create layout manager
	lm := viewport.NewLayoutManager(wm)

	// Create screen renderer, virtualizing its terminal when the host
	// provided one (native resize signals only apply to the real terminal).
	renderer := render.NewScreenRenderer(wm, lm)
	if cfg.Terminal != nil {
		renderer.SetTerminal(cfg.Terminal.Output, cfg.Terminal.Size, false)
	}

	// Document FS (real OS unless the host virtualized it) and the mew: tree
	// resolver — both needed before config load so config/profile/includes go
	// through them.
	docFS := cfg.FS
	usingOSFS := docFS == nil
	if docFS == nil {
		docFS = OSFileSystem()
	}
	mewVFS := newMewVFS(&cfg)

	// Load configuration: a host-supplied config string wins; otherwise the
	// local config file (unless the host opted out). File access routes mew:/
	// paths (user editor.conf, profile, angle-bracket includes) through the mew
	// tree and everything else (project .mew files, relative includes) through
	// the document FS.
	configMgr := config.NewManager()
	configMgr.SetFileIO(makeConfigFileIO(docFS, mewVFS))
	if !mewVFS.virtual {
		configMgr.SetLocalMewDir(mewVFS.localRoot) // exclude ~/.mew from project discovery
	}
	loadedConfig := config.DefaultConfig()
	configFromDisk := false
	if cfg.ConfigText != nil {
		loadedConfig = configMgr.LoadFromString(*cfg.ConfigText)
	} else if !cfg.SkipUserConfig {
		loadedConfig, _ = configMgr.Load() // Ignore error, use defaults
		configFromDisk = true
	}

	// Refine the mew: tree's read-only system resource layer now that config is
	// loaded: a local [storage] resources= override wins over the OS-default
	// dirs seeded at construction (which already backed the config read itself).
	// A host-supplied config string cannot redirect it (same rule as scratch).
	if configFromDisk {
		mewVFS.setSystemResources(loadedConfig.Storage.Resources)
	}

	// Initialize the buffer library with a local cold storage path. Garland
	// always gets a real local directory, even when document I/O is
	// virtualized: host override first, then the LOCAL config's [storage]
	// scratch (never from a host-supplied config string), then the system
	// temp directory.
	coldPath := cfg.ColdStoragePath
	if coldPath == "" && configFromDisk {
		coldPath = loadedConfig.Storage.Scratch
	}
	// Per-instance cold storage: this editor gets its OWN garland Library under a
	// unique subfolder of the base cold-storage area, so many mews can run in one
	// process (e.g. as KittyTK editor trinkets) without sharing garland state or
	// colliding on cold storage. The subfolder is removed on Cleanup.
	coldBase := coldPath
	if coldBase == "" {
		coldBase = os.TempDir()
	}
	_ = os.MkdirAll(coldBase, 0o755)
	instCold, mkErr := os.MkdirTemp(coldBase, "mew-")
	ownCold := mkErr == nil
	if !ownCold {
		instCold = coldPath // fall back to the shared base directory
	}
	lib, err := buffer.NewLibrary(instCold)
	if err != nil {
		if ownCold {
			_ = os.RemoveAll(instCold)
		}
		return nil, fmt.Errorf("failed to initialize buffer library: %w", err)
	}
	coldDir := ""
	if ownCold {
		coldDir = instCold // this editor owns it and removes it on Cleanup
	}

	// Apply loaded config to editor config
	cfg.ShowLineNumbers = loadedConfig.General.ShowLineNumbers
	cfg.DeleteNewlineAsChar = loadedConfig.General.DeleteNewlineAsChar
	cfg.ShowColumnRuler = loadedConfig.General.ShowColumnRuler
	cfg.Scrollbar = loadedConfig.General.Scrollbar
	cfg.RulerShowsCursor = loadedConfig.General.RulerShowsCursor
	cfg.TabSize = loadedConfig.General.TabSize
	cfg.ShowInvisibles = loadedConfig.General.ShowInvisibles
	cfg.ShowBidi = loadedConfig.General.ShowBidi
	cfg.RtlCombining = loadedConfig.General.RtlCombining
	// Environment-aware default: in a bidi-applying REAL terminal (macOS
	// Terminal.app), and only when the config did not set rtlCombining
	// explicitly, default it OFF so pointed RTL renders unpointed (codepoints
	// == cells) and the selection bar stays correct — flipBidiForHost=auto
	// enables for the same terminal. A virtualized terminal (the KittyTK host,
	// which renders marks correctly) keeps marks on; an explicit config value
	// always wins.
	if !loadedConfig.General.RtlCombiningSet && cfg.Terminal == nil && envSaysBidiTerminal() {
		cfg.RtlCombining = false
	}
	cfg.ShowMarks = loadedConfig.General.ShowMarks
	cfg.OverwriteMode = loadedConfig.General.OverwriteMode
	cfg.ReadOnly = loadedConfig.General.ReadOnly
	cfg.AutoIndent = loadedConfig.General.AutoIndent
	cfg.InsertCursor = loadedConfig.General.InsertCursor
	cfg.OverwriteCursor = loadedConfig.General.OverwriteCursor
	cfg.NavigationCursor = loadedConfig.General.NavigationCursor
	cfg.LinkBrowsing = loadedConfig.General.LinkBrowsing
	cfg.Syntax = loadedConfig.General.Syntax
	cfg.SyntaxDetect = loadedConfig.General.SyntaxDetect
	cfg.SyntaxOverrides = loadedConfig.General.SyntaxOverrides
	cfg.MatchIgnoresSingleQuote = loadedConfig.General.MatchIgnoresSingleQuote
	cfg.MatchIgnoresDoubleQuote = loadedConfig.General.MatchIgnoresDoubleQuote
	cfg.MatchIgnoresSlashStar = loadedConfig.General.MatchIgnoresSlashStar
	cfg.MatchIgnoresSlashSlash = loadedConfig.General.MatchIgnoresSlashSlash
	cfg.MatchIgnoresHash = loadedConfig.General.MatchIgnoresHash
	cfg.MatchIgnoresDoubleHyphen = loadedConfig.General.MatchIgnoresDoubleHyphen
	cfg.MatchIgnoresSemicolon = loadedConfig.General.MatchIgnoresSemicolon
	cfg.MatchIgnoresPercent = loadedConfig.General.MatchIgnoresPercent
	cfg.MacOptionKeys = loadedConfig.General.MacOptionKeys
	cfg.UseLocks = loadedConfig.General.UseLocks
	cfg.UseEmacsLocks = loadedConfig.General.UseEmacsLocks
	cfg.WordWrap = loadedConfig.General.WordWrap
	cfg.SearchIgnoreCase = loadedConfig.General.SearchIgnoreCase
	cfg.SearchWrap = loadedConfig.General.SearchWrap
	cfg.SearchRegex = loadedConfig.General.SearchRegex
	cfg.SearchBackwards = loadedConfig.General.SearchBackwards
	cfg.SearchAllBuffers = loadedConfig.General.SearchAllBuffers
	cfg.ModebarLocation = loadedConfig.General.ModebarLocation
	if cfg.ModebarLocation == "" {
		cfg.ModebarLocation = "top"
	}
	cfg.ModebarInner = loadedConfig.General.ModebarInner
	cfg.ModebarDefault = loadedConfig.General.ModebarDefault
	cfg.ModebarOuter = loadedConfig.General.ModebarOuter
	cfg.MappingsName = loadedConfig.General.MappingsName
	cfg.PromptTimeout = loadedConfig.General.PromptTimeout
	cfg.ScriptTimeout = loadedConfig.General.ScriptTimeout
	cfg.PageSizeOptimal = loadedConfig.General.PageSizeOptimal
	cfg.PageOverlapMinimum = loadedConfig.General.PageOverlapMinimum
	cfg.PageSizeStep = loadedConfig.General.PageSizeStep
	cfg.MaxRepeat = loadedConfig.General.MaxRepeat
	cfg.KillRingEntries = loadedConfig.General.KillRingEntries
	cfg.Direction = loadedConfig.General.Direction
	renderer.SetBaseRTL(cfg.Direction == "rtl")
	cfg.FlipBidiForHost = loadedConfig.General.FlipBidiForHost
	if cfg.FlipBidiForHost == "" {
		cfg.FlipBidiForHost = "auto"
	}
	// Explicit setting applies now; "auto" turns the flip on for a sniffed bidi
	// host (Apple Terminal, Kitty) and otherwise stays off until the probe
	// decides (triggered by the first frame containing RTL content). The
	// segmentation and ride-safe-selection axes ride the same sniff.
	flip, wordwise, rideSafe, _ := flipSettings(cfg.FlipBidiForHost)
	// Kitty applies its own bidi only while force_ltr is off; when the user has
	// set force_ltr yes, mew must own the bidi and not flip. Sniff kitty.conf to
	// pick the right path (there is no in-band way to detect force_ltr).
	flip, wordwise, rideSafe, kittyFlipActive := adjustFlipForKitty(cfg.FlipBidiForHost, flip, wordwise, rideSafe)
	renderer.SetFlipBidiForHost(flip)
	renderer.SetFlipWordwise(wordwise)
	renderer.SetFlipRideSafeSelection(rideSafe)

	cfg.RtlMarkMode = loadedConfig.General.RtlMarkMode
	if cfg.RtlMarkMode == "" {
		cfg.RtlMarkMode = "auto"
	}
	resolvedRtlMark := resolveRtlMarkMode(cfg.RtlMarkMode)
	renderer.SetRtlMarkMode(resolvedRtlMark)
	if cfg.RtlMarkModeSink != nil {
		cfg.RtlMarkModeSink(resolvedRtlMark)
	}

	// Restore a host-provided state snapshot over the loaded configuration.
	applyInitialState(&cfg)

	// Create editor instance first (without PawScript)
	e := &Editor{
		ViewportManager:  wm,
		LayoutManager:    lm,
		Renderer:         renderer,
		Config:           cfg,
		FS:               docFS,
		usingOSFS:        usingOSFS,
		realTerminal:     cfg.Terminal == nil,
		probeCapable:     cfg.Terminal == nil || cfg.Terminal.Interactive,
		mew:              mewVFS,
		home:             hostHome(&cfg),
		lib:              lib,
		coldDir:          coldDir,
		ConfigMgr:        configMgr,
		LoadedConfig:     loadedConfig,
		configFromDisk:   configFromDisk,
		bufNotices:       make(map[*buffer.Buffer][]bufferNotice),
		mewLocks:         make(map[*buffer.Buffer]string),
		mewLockDeferred:  make(map[*buffer.Buffer]string),
		linkVisitSeen:    make(map[string]bool),
		linkResolveCache: make(map[string]string),
		kittyFlipActive:  kittyFlipActive,
	}

	// Keep the tiler in sync with focus: when mew focuses a main-area viewport,
	// tilerFollowFocus finds/reveals/creates the tile that holds it.
	e.ViewportManager.SetMainFocusHook(e.tilerFollowFocus)

	// The focus switcher (viewport_next / viewport_prior) cycles only viewports
	// currently ON SCREEN: docked ones keep their own Visible flag, but a main
	// viewport counts only while a tile shows it — so the switcher never lands on
	// an untiled background buffer (buffer_next / buffer_prior still reach those).
	e.ViewportManager.SetCycleVisibleFilter(func(w *viewport.Viewport) bool {
		return w.Dock != viewport.DockNone || e.tiler == nil || e.viewportTiled(w.ID)
	})

	// Register configured fonts into the host font engine and apply the
	// startup ui-term alias, before any painting resolves font names.
	e.applyFontConfig()

	// Create writers to capture PawScript I/O
	stderrWriter := &statusWriter{editor: e}
	stdoutWriter := &insertWriter{editor: e}

	// PawScript's io:: stdin channel reads the same (possibly virtual)
	// terminal input as the editor, so scripts can never bypass a host's
	// virtualized session by reaching the real OS stdin.
	var pawStdin io.Reader = os.Stdin
	if cfg.Terminal != nil && cfg.Terminal.Input != nil {
		pawStdin = cfg.Terminal.Input
	}

	// Create PawScript interpreter with custom I/O. The config pointer is
	// retained: DefaultTokenTimeout is read live at each host-level token
	// request, so set_option scriptTimeout takes effect immediately.
	pawCfg := &pawscript.Config{
		Debug:                false,
		AllowMacros:          true,
		EnableSyntacticSugar: true,
		ShowErrorContext:     true,
		ContextLines:         2,
		Stdin:                pawStdin,
		Stdout:               stdoutWriter,
		Stderr:               stderrWriter,
		DefaultTokenTimeout:  tokenTimeout(cfg.ScriptTimeout),
	}
	ps := pawscript.New(pawCfg)
	e.pawConfig = pawCfg

	// Register the PawScript standard library
	ps.RegisterStandardLibrary(nil)

	e.PawScript = ps

	// Build the paging spec from the three options (each malformed value falls
	// back to its default inside buildPageSizeSpec).
	e.pageSizeSpec = buildPageSizeSpec(cfg.PageSizeOptimal, cfg.PageOverlapMinimum, cfg.PageSizeStep)

	// Create plugins
	e.Modebar = plugins.NewModebar(wm)
	e.ColumnRuler = plugins.NewColumnRuler()

	// Apply configured indicator glyphs to the renderer and ruler.
	renderer.SetIndicators(loadedConfig.Indicators)
	e.ColumnRuler.SetIndicators(loadedConfig.Indicators)
	e.ColumnRuler.SetRTL(cfg.Direction == "rtl")

	// Apply the loaded color scheme everywhere colors are resolved.
	renderer.SetColorScheme(loadedConfig.Colors)
	e.Modebar.SetColorScheme(loadedConfig.Colors)
	e.Modebar.SetTemplates(loadedConfig.General.ModebarInner, loadedConfig.General.ModebarDefault, loadedConfig.General.ModebarOuter)
	// A wiki page shows its scheme form ("help:/start"), not "start.txt".
	e.Modebar.SetFilenameFunc(e.wikiDisplayName)
	// Resolve %keys#…% / %keys_verbose#…% TFC codes in modebar templates to
	// live bindings (no ANSI wrap — the key text inherits the modebar color).
	e.Modebar.SetTFCResolver(e.tfcKeyResolver("", ""))
	e.Modebar.SetNavStateFunc(func() (int, bool, int) {
		// Suppress hover styling while a modal prompt holds focus (the buttons
		// stand down), even if a stale hover lingered from before the prompt.
		hover := e.modebarNavHover
		if e.promptHasPriority() {
			hover = 0
		}
		return e.modebarNavCapture, e.modebarNavOn, hover
	})
	e.ColumnRuler.SetColorScheme(loadedConfig.Colors)

	// Create prompt manager for history-aware prompts
	e.PromptMgr = NewPromptManager(e)

	// Register custom renderers
	renderer.RegisterCustomRenderer("modebar", e.renderModebar)

	// The column ruler is not a viewport of its own: the renderer draws it on the
	// top line of any viewport whose ShowRuler view option is enabled.
	renderer.SetRulerRenderer(e.renderColumnRuler)

	// The scrollbar option never applies to a viewport hosting a terminal
	// session: the terminal draws its own scrollbar, and reserving the column
	// would shrink its grid. Session-ness lives on the buffer, which only the
	// editor can see — so the renderer asks.
	// When the host draws the bars, mew reserves their column but paints
	// nothing into it (see Config.ScrollbarRegions).
	renderer.SetScrollbarHostDrawn(e.hostDrawsScrollbars)

	renderer.SetScrollbarSuppressor(func(w *viewport.Viewport) bool {
		return w.Buffer != nil && e.visibleSessionFor(w) != nil
	})

	// A terminal viewport's document text is not painted: the host draws the
	// terminal grid over it, and a grid too short/narrow to fill the area must
	// show the editor background, not the buffer behind. The gutter and ruler
	// still render. Session-ness lives on the buffer, so the renderer asks.
	renderer.SetContentSuppressor(func(w *viewport.Viewport) bool {
		return w.Buffer != nil && e.visibleSessionFor(w) != nil
	})

	// Peek-indicator labels run through the shared TFC engine so codes like
	// %SPU% resolve to the live peek-command bindings (and %keys#…% references
	// resolve to live bindings too).
	renderer.SetPeekLabelResolver(func(raw string) string {
		return plugins.ExpandTFC(raw, e.peekBindingValues(), e.tfcKeyResolver("", ""))
	})

	// The shipped grammar pack resolves through the mew: tree's read-only
	// system/embedded layers (no copy into ~/.mew), then load the configured
	// grammar and give the renderer its per-line colorizer.
	e.initSyntax()
	renderer.SetSyntaxColorizer(e.syntaxLineColors)
	// Browse-mode link buttons: the renderer substitutes these per line at
	// paint/measure time (see links.go); nil results leave lines untouched.
	renderer.SetDisplayProvider(e.lineDisplaySpans)
	// Hide the hardware caret while it is inert inside a focused button.
	renderer.SetCaretHiddenFn(e.caretHidden)
	renderer.SetCursorStyleFn(e.cursorStyleFor)

	// Register editor commands with PawScript
	e.registerCommands()

	// Create key sequence processor with command executor
	e.KeyProcessor = keys.NewSequenceProcessor(e.runBoundCommand)

	// Input source: a host-supplied event feed when one was given, else a
	// keyboard handler parsing the (possibly virtual) terminal byte stream.
	if cfg.KeySource != nil {
		e.KeyHandler = cfg.KeySource
	} else {
		var termIn io.Reader
		var termOut io.Writer
		if cfg.Terminal != nil {
			termIn = cfg.Terminal.Input
			termOut = cfg.Terminal.Output
		}
		e.KeyHandler = input.NewKeyboardHandler(termIn, termOut)
	}

	// Bind the host command port (if the host supplied one) now that the
	// input source exists: Execute marshals through PostAction onto the main
	// loop, so a host menu item runs its command with keystroke safety.
	if cfg.HostPort != nil {
		cfg.HostPort.bind(e.PostAction, e.executeCommand, func(action, preferred string) string {
			// A synchronous read of the live keymap from a host thread: take
			// renderMu, which is what guards the keymap rewrites
			// (applyFocusedMappings) this would otherwise race.
			e.renderMu.Lock()
			defer e.renderMu.Unlock()
			return e.keyBindingDisplay(action, preferred)
		}, func(name string) string {
			// Same contract for an option read: the host asks what the editor
			// currently holds so its own UI can reflect it. Unknown names
			// answer "" rather than warning — a host reflecting state must not
			// be able to spray notifications into the editor.
			e.renderMu.Lock()
			defer e.renderMu.Unlock()
			value, ok := e.getOption(e.ViewportManager.GetLastMainViewport(), name)
			if !ok {
				return ""
			}
			return value
		})
	}

	// Set up key mappings from config
	e.setupKeyMappingsFromConfig()

	// Apply the macOS Option-key layer: decode per the option (auto = on
	// for macOS only), and re-insert Option characters for unmapped M- keys
	// whenever the layer is not "false".
	e.applyMacOptionKeys()

	// Resolve the DEADCAT crash-dump destination up front, so the death path
	// (a signal, a panic, or a host's sudden shutdown) never has to decide.
	e.resolveDeadcat()

	// A terminal is bound to the viewport it launched in (ptySessions is keyed by
	// viewport): when that viewport is truly removed, its session goes with it.
	// The event fires on its own goroutine, so marshal the close onto the main
	// loop where the pty map lives. OldValue carries the removed *Viewport.
	e.ViewportManager.On(viewport.EventViewportRemoved, func(ev viewport.Event) {
		w, _ := ev.OldValue.(*viewport.Viewport)
		if w == nil {
			return
		}
		e.PostAction(func() { e.endSessionForRemovedViewport(w) })
	})

	return e, nil
}

// applyMacOptionKeys pushes the macOptionKeys option into the input decoder
// and the key processor's reverse-insert fallback.
func (e *Editor) applyMacOptionKeys() {
	fw := e.ViewportManager.GetFocusedViewport()
	mode := strings.ToLower(e.optStr(fw, "macoptionkeys", e.Config.MacOptionKeys))
	if mode == "" {
		mode = "auto"
	}
	decode := mode == "true" || (mode == "auto" && runtime.GOOS == "darwin")
	insert := mode != "false"
	if kh, ok := e.KeyHandler.(*input.KeyboardHandler); ok {
		kh.SetDecodeMacOSOption(decode)
	}
	e.KeyProcessor.SetMacOptionInsert(insert)
}

// renderModebar is the custom renderer for the modebar.
func (e *Editor) renderModebar(w *viewport.Viewport, screenWidth int) string {
	e.Modebar.SetActiveSequence(e.ActiveSequence)
	e.Modebar.SetCompletions(e.activeCompletions)
	e.Modebar.SetBindingValues(e.peekBindingValues())
	return e.Modebar.RenderContent(w, screenWidth)
}

// peekBindingCommands maps the modebar engine's peek codes to the commands
// whose live key binding they display.
var peekBindingCommands = map[string]string{
	"SPU": "stat_peek_up",
	"SPD": "stat_peek_down",
	"PPU": "prompt_peek_up",
	"PPD": "prompt_peek_down",
}

// peekBindingValues resolves the peek %CODE%s (SPU/SPD/PPU/PPD) to the key
// currently bound to each peek command, for the modebar substitution engine and
// the peek-indicator labels. Mappings are editor-global today; the resolver
// runs at render time, so if per-viewport keymaps are ever added the focused
// viewport's map is the natural source.
func (e *Editor) peekBindingValues() map[string]string {
	vals := make(map[string]string, len(peekBindingCommands))
	for code, cmd := range peekBindingCommands {
		vals[code] = e.KeyForCommand(cmd)
	}
	return vals
}

// caretHidden reports whether the hardware caret should be withheld for a
// viewport — the predicate the renderer consults each frame.
//
// A viewport hosting a TERMINAL does not own a caret: the child's cursor is
// the one that means anything there. It sits at the shell's insertion point,
// in the shape and blink the program asked for, and it is drawn by the child
// itself. mew's own caret meanwhile sits wherever the document caret happens
// to be, which is nowhere in particular. Asking for both puts two blinking
// cursors on a graphical host, one of them pointing at the wrong place.
//
// This is true of EVERY pty viewport, focused or not. An unfocused one draws
// its child's cursor in the unfocused form (see SetEmbeddedFocus in the
// host's terminalPlace); mew adding a second caret over it would say the
// opposite.
func (e *Editor) caretHidden(w *viewport.Viewport) bool {
	if w != nil && e.visibleSessionFor(w) != nil {
		return true
	}
	return e.focusedLinkButton(w) != nil
}

// KeyForCommand returns the key sequence bound to command, in the spelling a
// user would press — capture/override level prefixes stripped, since the
// physical keys are the same at every level, and wildcard bindings skipped,
// since "*" is not a key anyone can be told to press. When several keys map
// to it the lexicographically-first is returned (stable); "" when the command
// is unbound.
func (e *Editor) KeyForCommand(command string) string {
	best := ""
	for raw, cmd := range e.KeyProcessor.GetAllMappings() {
		if cmd != command {
			continue
		}
		key, ok := keys.DisplayKey(raw)
		if !ok {
			continue
		}
		if best == "" || key < best {
			best = key
		}
	}
	return best
}

// renderColumnRuler renders the column ruler line for a viewport with the
// ShowRuler view option enabled. With rulerShowsCursor on, the cursor's
// column(s) — caret, ghost, and secondary bidi cursor — are marked with the
// rulerCursor color.
func (e *Editor) renderColumnRuler(w *viewport.Viewport, screenWidth int) string {
	var cursorCols []int
	if e.optBool(w, "rulershowscursor", e.Config.RulerShowsCursor) {
		cursorCols = e.Renderer.CursorColumns(w)
	}
	return e.ColumnRuler.RenderContent(w, screenWidth, cursorCols)
}

// tokenTimeout converts a timeout option value (seconds, 0 = never) to a
// PawScript token timeout (a non-positive duration disables the timeout).
func tokenTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		return -1
	}
	return time.Duration(seconds) * time.Second
}

// argString returns the i'th PawScript argument as a string, and whether it
// was present at all (nil arguments count as absent).
func argString(ctx *pawscript.Context, i int) (string, bool) {
	if i >= len(ctx.Args) || ctx.Args[i] == nil {
		return "", false
	}
	return fmt.Sprintf("%v", ctx.Args[i]), true
}

// argInt reads a whole-number argument. PawScript hands numbers over as
// whichever numeric type the literal parsed to, so go through the string form
// rather than type-switching every possibility.
func argInt(ctx *pawscript.Context, i int) (int, bool) {
	s, ok := argString(ctx, i)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, false
	}
	return n, true
}

// registerCommands registers all editor commands with PawScript.
func (e *Editor) registerCommands() {
	ps := e.PawScript

	// ifitfits viewport-tiling commands (viewport_new, viewport_zoom, …) live in
	// tilingcommands.go.
	e.registerTilingCommands(ps)

	// System commands
	ps.RegisterCommand("exit", func(ctx *pawscript.Context) pawscript.Result {
		e.Running = false
		return pawscript.BoolStatus(true)
	})

	// show_desktop / hide_desktop ask the embedding host to reveal or hide its
	// desktop (e.g. a KittyTK viewport-manager host). No-ops in the standalone
	// editor, where no host wires the hooks.
	ps.RegisterCommand("show_desktop", func(ctx *pawscript.Context) pawscript.Result {
		if e.Config.ShowDesktop != nil {
			e.Config.ShowDesktop()
		}
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("hide_desktop", func(ctx *pawscript.Context) pawscript.Result {
		if e.Config.HideDesktop != nil {
			e.Config.HideDesktop()
		}
		return pawscript.BoolStatus(true)
	})

	// nav_cancel: turn link browse mode off on the focused viewport. Fails when
	// browse mode is not active, so nav_cancel|cancel|... chains fall through.
	ps.RegisterCommand("nav_cancel", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navCancel())
	})

	// nav_follow [always]: follow the link at the caret. With no argument (or
	// "true") it ALWAYS follows when the caret is within/at the edge of a
	// link's source, even in ordinary edit mode — so `^B F =nav_follow` jumps
	// without entering navigation mode first. With "false" it follows only in
	// navigation mode (a focused button); otherwise it fails, so a
	// `nav_follow false|accept|insert_newline` chain falls through to plain Enter.
	ps.RegisterCommand("nav_follow", func(ctx *pawscript.Context) pawscript.Result {
		always := true
		if arg, ok := argString(ctx, 0); ok && strings.EqualFold(strings.TrimSpace(arg), "false") {
			always = false
		}
		return pawscript.BoolStatus(e.navFollow(always))
	})

	// nav_next / nav_prior: move to the next/previous link (cycling).
	//
	// Bare, they ALWAYS act: from the caret's own link, or into the first link
	// from the caret when it is in none. With the argument "false" they are
	// GATED on a focused button instead, capturing only in browse mode so a
	// chain like tab = nav_next false|completion|insert '\t' yields to editing
	// when the caret is not inside a link. Same shape as nav_follow, and for
	// the same reason: a key bound alone wants the action, a key in a chain
	// wants to fall through.
	navArg := func(ctx *pawscript.Context) bool {
		if arg, ok := argString(ctx, 0); ok && strings.EqualFold(strings.TrimSpace(arg), "false") {
			return false
		}
		return true
	}
	ps.RegisterCommand("nav_next", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navLink(+1, navArg(ctx)))
	})
	ps.RegisterCommand("nav_prior", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navLink(-1, navArg(ctx)))
	})

	// nav_start: enter nav (browse) mode, focusing the first link at/after the
	// caret. nav_up/nav_down move to the nearest link on the next/prior link
	// line (paging when none remains on screen); nav_left/nav_right move to the
	// optically adjacent link on the same line (bidi-aware). All four act only
	// in active nav mode.
	ps.RegisterCommand("nav_start", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navStart())
	})
	ps.RegisterCommand("nav_down", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navVert(+1))
	})
	ps.RegisterCommand("nav_up", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navVert(-1))
	})
	ps.RegisterCommand("nav_right", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navHoriz(+1))
	})
	ps.RegisterCommand("nav_left", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navHoriz(-1))
	})

	// nav_history_prior / nav_history_next: walk the focused viewport's
	// buffer-swap history (following a link swaps the buffer in place,
	// stacking the departed binding — see Viewport.SwapBuffer). Prior returns
	// to where you were; next re-advances. Both fail when there is no history
	// in that direction, so chains fall through.
	//
	// nav_history_prior takes the same [always] first argument as nav_follow.
	// Bare (or "true") it always acts. With "false" it is GATED like
	// nav_follow false — only the actively focused link button of the focused
	// viewport lets it act — EXCEPT that a read-only document already in
	// navigation mode passes even off a link (nothing there is editable, so the
	// key is free to mean "go back"). See navHistoryGatePasses.
	ps.RegisterCommand("nav_history_prior", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navHistory(-1, navArg(ctx)))
	})
	ps.RegisterCommand("nav_history_next", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navHistory(+1, true))
	})

	// nav_clear forgets every visited link (editor-wide repaint to the
	// unvisited style). nav_history_clear empties the focused viewport's whole
	// back/forward history, releasing stacked bindings except any that hold
	// the LAST reference to a buffer — those move to the viewport's graveyard,
	// held for the eventual save decision.
	ps.RegisterCommand("nav_clear", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navClearVisited())
	})
	ps.RegisterCommand("nav_history_clear", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navHistoryClear())
	})

	ps.RegisterCommand("cancel", func(ctx *pawscript.Context) pawscript.Result {
		focusedViewport := e.ViewportManager.GetFocusedViewport()
		if focusedViewport != nil && focusedViewport.Type == viewport.PromptViewport {
			// Capture callbacks before removing viewport
			legacyCallback := focusedViewport.Callback
			promptCallback := focusedViewport.PromptCallback

			// Remove prompt viewport FIRST so focus returns to main buffer
			delete(e.confirmKey, focusedViewport.ID)
			e.ViewportManager.RemoveViewport(focusedViewport.ID)

			// Call the appropriate callback
			if promptCallback != nil {
				promptCallback(false, "", "")
			} else if legacyCallback != nil {
				legacyCallback("", false)
			}
			return pawscript.BoolStatus(true)
		}
		// No prompt to dismiss. A find is not something to cancel: it has no
		// prompt once its first search has begun and ^L just goes to the next
		// match, so ^C falls through to whatever else it is bound to. The
		// single exception is the search slow enough to
		// have put a message on screen naming this very key; that message is a
		// promise, and cancelFind is where it is kept.
		if e.cancelFind() {
			return pawscript.BoolStatus(true)
		}
		return pawscript.BoolStatus(false)
	})

	ps.RegisterCommand("accept", func(ctx *pawscript.Context) pawscript.Result {
		focusedViewport := e.ViewportManager.GetFocusedViewport()
		if focusedViewport != nil && focusedViewport.Type == viewport.PromptViewport {
			// Capture callbacks before removing viewport
			legacyCallback := focusedViewport.Callback
			promptCallback := focusedViewport.PromptCallback

			// Get buffer content from line 0 (for backward compatibility)
			bufferContent := ""
			if focusedViewport.Buffer != nil && focusedViewport.Buffer.GetLineCount() > 0 {
				bufferContent = strings.TrimRight(focusedViewport.Buffer.GetLine(0), "\n\r")
			}

			// Get the text from the line where the cursor is positioned
			// This is the key difference from TypeScript - we read cursor line, not line 0
			cursorLineText := ""
			if focusedViewport.Buffer != nil {
				cursorLine := focusedViewport.CursorPos().Line
				if cursorLine < focusedViewport.Buffer.GetLineCount() {
					cursorLineText = strings.TrimRight(focusedViewport.Buffer.GetLine(cursorLine), "\n\r")
				}
			}

			// Remove prompt viewport FIRST so focus returns to main buffer
			// This ensures any output from the callback goes to the right viewport
			delete(e.confirmKey, focusedViewport.ID)
			e.ViewportManager.RemoveViewport(focusedViewport.ID)

			// Call the appropriate callback
			if promptCallback != nil {
				promptCallback(true, bufferContent, cursorLineText)
			} else if legacyCallback != nil {
				// Legacy callback uses cursorLineText as input (for single-line prompts)
				legacyCallback(cursorLineText, true)
			}
			return pawscript.BoolStatus(true)
		}
		return pawscript.BoolStatus(false)
	})

	// Key mapping commands (matching TypeScript version)
	ps.RegisterCommand("map", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) < 2 {
			e.ShowWarning("Usage: map <key>, <command>")
			return pawscript.BoolStatus(false)
		}
		key := fmt.Sprintf("%v", ctx.Args[0])
		command := fmt.Sprintf("%v", ctx.Args[1])
		e.KeyProcessor.MapKey(key, command)
		// A runtime remap: credit it to AuthorRemapped and give it a precedence
		// above every config binding so it wins the key-badge tie-break.
		if e.mappingOrigins == nil {
			e.mappingOrigins = make(map[string]config.MappingOrigin)
		}
		e.remapPrec++
		e.mappingOrigins[key] = config.MappingOrigin{
			Source:     config.SourceRemap,
			Precedence: e.remapPrec,
			Author:     config.AuthorRemapped,
		}
		e.ShowNotification("Mapped " + key + " -> " + command)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("unmap", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) < 1 {
			e.ShowWarning("Usage: unmap <key>")
			return pawscript.BoolStatus(false)
		}
		key := fmt.Sprintf("%v", ctx.Args[0])
		e.KeyProcessor.UnmapKey(key)
		e.ShowNotification("Unmapped " + key)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("remap", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) < 1 {
			e.ShowWarning("Usage: remap <key>")
			return pawscript.BoolStatus(false)
		}
		key := fmt.Sprintf("%v", ctx.Args[0])
		// Restore from original config
		if cmd, ok := e.LoadedConfig.Mappings[key]; ok {
			e.KeyProcessor.MapKey(key, cmd)
			e.ShowNotification("Restored mapping: " + key + " -> " + cmd)
			return pawscript.BoolStatus(true)
		}
		e.ShowWarning("No original mapping for " + key)
		return pawscript.BoolStatus(false)
	})

	// after_key sets the focused viewport's after-key pseudo-binding: the
	// PawScript command run each time a key's binding activity resolves while
	// that viewport owns the keyboard (see viewport.Viewport.AfterKey). With no
	// argument (or ""), it clears the binding.
	ps.RegisterCommand("after_key", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil {
			return pawscript.BoolStatus(false)
		}
		script, _ := argString(ctx, 0)
		w.AfterKey = script
		return pawscript.BoolStatus(true)
	})

	// not_implemented is a placeholder for a command that is planned but not
	// written yet: it warns, names what was asked for, and reports FALSE so a
	// chain falls through it exactly as an unhandled command would.
	//
	// It exists so a planned feature can be advertised in one place instead of
	// two. Wiring only the MENU item to a toast would leave the key it
	// advertises doing nothing at all, which is the worse half of the lie -
	// the user presses the key the menu just taught them and gets silence.
	// Bind the key here too and both routes answer the same way.
	ps.RegisterCommand("not_implemented", func(ctx *pawscript.Context) pawscript.Result {
		what, _ := argString(ctx, 0)
		msg := "Not yet implemented"
		if strings.TrimSpace(what) != "" {
			msg = what + ": not yet implemented"
		}
		// One tag for the whole family, so trying several planned items in a
		// row REPLACES the toast rather than stacking a pile of them - they all
		// say the same thing, and the newest is the only one that is about what
		// the user just pressed.
		e.ShowWarningTagged(msg, "not_implemented")
		return pawscript.BoolStatus(false)
	})

	ps.RegisterCommand("mappings_show", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) < 1 {
			e.ShowWarning("Usage: mappings_show <key>")
			return pawscript.BoolStatus(false)
		}
		key := fmt.Sprintf("%v", ctx.Args[0])
		if cmd := e.KeyProcessor.GetMapping(key); cmd != "" {
			e.ShowWarning(key + " -> " + cmd)
			return pawscript.BoolStatus(true)
		}
		e.ShowWarning("No mapping for " + key)
		return pawscript.BoolStatus(false)
	})

	ps.RegisterCommand("mappings_list", func(ctx *pawscript.Context) pawscript.Result {
		mappings := e.KeyProcessor.GetAllMappings()
		if len(mappings) == 0 {
			e.ShowWarning("No key mappings defined")
			return pawscript.BoolStatus(false)
		}
		// Build list content
		var content strings.Builder
		content.WriteString("Key Mappings:\n")
		for key, cmd := range mappings {
			content.WriteString(fmt.Sprintf("  %s -> %s\n", key, cmd))
		}
		// Show in a work buffer viewport
		buf := e.lib.NewFromString(content.String())
		e.ViewportManager.CreateViewport(viewport.ViewportOptions{
			Type:             viewport.ToolViewport,
			ViewportSet:      "help",
			Class:            "mappings",
			Dock:             viewport.DockTop,
			Priority:         100,
			MinHeight:        5,
			MaxHeight:        15,
			MessageTopCenter: "Key Mappings",
			Buffer:           buf,
			ShowLineNumbers:  false,
		})
		e.RequestRender()
		return pawscript.BoolStatus(true)
	})

	// Movement commands (using TypeScript naming convention)
	ps.RegisterCommand("go_char_prior", func(ctx *pawscript.Context) pawscript.Result {
		e.moveCursor(-1, 0)
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_char_next", func(ctx *pawscript.Context) pawscript.Result {
		e.moveCursor(1, 0)
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_line_prior", func(ctx *pawscript.Context) pawscript.Result {
		e.moveCursor(0, -1)
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_line_next", func(ctx *pawscript.Context) pawscript.Result {
		e.moveCursor(0, 1)
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_line_beg", func(ctx *pawscript.Context) pawscript.Result {
		e.cursorToLineStart()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_line_end", func(ctx *pawscript.Context) pawscript.Result {
		e.cursorToLineEnd()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_buffer_beg", func(ctx *pawscript.Context) pawscript.Result {
		e.cursorToBufferStart()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_buffer_end", func(ctx *pawscript.Context) pawscript.Result {
		e.cursorToBufferEnd()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("garland_balance", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w != nil && w.Buffer != nil {
			w.Buffer.Balance()
		}
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("debug_marks", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			e.ShowWarning("No buffer")
			return pawscript.BoolStatus(false)
		}
		beginLine, beginRune, beginByte, beginExists := w.Buffer.GetMarkDebug("_block_begin")
		endLine, endRune, endByte, endExists := w.Buffer.GetMarkDebug("_block_end")

		msg := fmt.Sprintf("begin: L%d R%d @%d (%v), end: L%d R%d @%d (%v), cursor: L%d R%d",
			beginLine, beginRune, beginByte, beginExists,
			endLine, endRune, endByte, endExists,
			w.CursorPos().Line, w.CursorPos().Rune)
		e.ShowWarning(msg)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_page_prior", func(ctx *pawscript.Context) pawscript.Result {
		e.pageUp()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_page_next", func(ctx *pawscript.Context) pawscript.Result {
		e.pageDown()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	// Scroll commands park the viewport WITHOUT moving the caret — the
	// programmatic analogue of the mouse wheel. Each detaches the view from
	// caret-follow (ScrollDetached) so the per-frame follow leaves it put until a
	// cursor-movement or edit command re-engages it. They mirror the go_* family
	// name-for-name (scroll_line ~ go_line, scroll_line_next ~ go_line_next, ...).
	ps.RegisterCommand("scroll_line_prior", func(ctx *pawscript.Context) pawscript.Result {
		e.scrollViewByLines(e.resolveTargetMain(), -1)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("scroll_line_next", func(ctx *pawscript.Context) pawscript.Result {
		e.scrollViewByLines(e.resolveTargetMain(), 1)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("scroll_page_prior", func(ctx *pawscript.Context) pawscript.Result {
		if w := e.resolveTargetMain(); w != nil {
			_, page := e.pageSize(w)
			e.scrollViewByLines(w, -page)
		}
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("scroll_page_next", func(ctx *pawscript.Context) pawscript.Result {
		if w := e.resolveTargetMain(); w != nil {
			_, page := e.pageSize(w)
			e.scrollViewByLines(w, page)
		}
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("scroll_buffer_beg", func(ctx *pawscript.Context) pawscript.Result {
		e.scrollViewTo(e.resolveTargetMain(), 0)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("scroll_buffer_end", func(ctx *pawscript.Context) pawscript.Result {
		if w := e.resolveTargetMain(); w != nil && w.Buffer != nil {
			viewHeight, _ := e.pageSize(w)
			// Park the last line on the bottom row (clamped when the buffer is
			// shorter than the view).
			e.scrollViewTo(w, w.Buffer.GetLineCount()-viewHeight)
		}
		return pawscript.BoolStatus(true)
	})

	// scroll_line parks a given 1-based line at the TOP of the view (the scroll
	// analogue of go_line); it takes a line-number argument or, lacking one,
	// prompts for it exactly as go_line does.
	// scroll_viewport <id> <line>: park a NAMED viewport at a 0-based top
	// line. The host's graphical scrollbar drives scrolling with this — it
	// owns the bar in pixel space, so it computes the line itself and sends a
	// whole number; mew never scrolls by a fraction of a line. Like the wheel
	// and mew's own bar, it leaves the view detached and steals no focus, so a
	// background pane can be scrolled without disturbing the caret.
	ps.RegisterCommand("scroll_viewport", func(ctx *pawscript.Context) pawscript.Result {
		id, ok := argString(ctx, 0)
		if !ok {
			return pawscript.BoolStatus(false)
		}
		lineArg, ok := argString(ctx, 1)
		if !ok {
			return pawscript.BoolStatus(false)
		}
		top, err := strconv.Atoi(strings.TrimSpace(lineArg))
		if err != nil {
			return pawscript.BoolStatus(false)
		}
		w := e.ViewportManager.GetViewport(strings.TrimSpace(id))
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		e.scrollViewTo(w, top)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("scroll_line", func(ctx *pawscript.Context) pawscript.Result {
		scrollLine := func(input string) bool {
			n, err := strconv.Atoi(strings.TrimSpace(input))
			if err != nil || n < 1 {
				e.ShowWarning("Invalid line number")
				return false
			}
			e.scrollLineTop(n)
			return true
		}
		if arg, ok := argString(ctx, 0); ok {
			return pawscript.BoolStatus(scrollLine(arg))
		}
		expired := &atomic.Bool{}
		token := e.PawScript.RequestToken(func(string) { expired.Store(true) }, "",
			tokenTimeout(e.Config.PromptTimeout))
		e.PromptMgr.PromptForInput("Scroll to line: ", "", func(accepted bool, _, input string) {
			defer e.RequestRender()
			if expired.Load() {
				e.ShowWarning("Prompt timed out")
				return
			}
			if !accepted || strings.TrimSpace(input) == "" {
				ctx.ResumeToken(token, false)
				return
			}
			ctx.ResumeToken(token, scrollLine(input))
		}, "scrollline")
		return pawscript.TokenResult(token)
	})

	ps.RegisterCommand("go_word_prior", func(ctx *pawscript.Context) pawscript.Result {
		e.moveToPrevWord()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_word_next", func(ctx *pawscript.Context) pawscript.Result {
		e.moveToNextWord()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_match", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.gotoMatchingBracket()
		e.trackMove()
		return pawscript.BoolStatus(ok)
	})

	// syntax_context reports what the syntax highlighter's machine is doing
	// at the caret. With no argument the result is "comment", "string" or
	// "code"; an argument selects a detail: 'state' (machine state name),
	// 'class' (color class), 'syntax' (innermost grammar at the caret, which
	// may be an embedded language), or 'stack' (embedded-language chain,
	// innermost first, space-separated). Fails when no grammar applies.
	ps.RegisterCommand("syntax_context", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		sc, ok := e.syntaxContextAt(w.Buffer, w.CursorPos().Line, w.CursorPos().Rune)
		if !ok {
			return pawscript.BoolStatus(false)
		}
		which := ""
		if len(ctx.Args) > 0 {
			which = strings.ToLower(fmt.Sprintf("%v", ctx.Args[0]))
		}
		var out string
		switch which {
		case "":
			switch {
			case sc.Comment:
				out = "comment"
			case sc.String:
				out = "string"
			default:
				out = "code"
			}
		case "state":
			out = sc.State
		case "class":
			out = sc.Class
		case "syntax":
			out = sc.Syntax
		case "stack":
			out = strings.Join(sc.Stack, " ")
		default:
			e.ShowWarning("syntax_context: unknown detail " + which)
			return pawscript.BoolStatus(false)
		}
		ctx.SetResult(out)
		return pawscript.BoolStatus(true)
	})

	// nop does nothing, successfully. Bind a key to it to deliberately
	// disable the key (unbinding instead restores the key's default
	// handling, e.g. self-insert).
	ps.RegisterCommand("nop", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(true)
	})

	// completion invokes the focused viewport's completion handler, if it has
	// one (filename prompts do). It returns that handler's result; with no
	// handler, or when the handler declines, it fails — so a binding like
	// completion|insert '\t' falls through to inserting a tab.
	ps.RegisterCommand("completion", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.CompletionCallback == nil {
			return pawscript.BoolStatus(false)
		}
		return pawscript.BoolStatus(w.CompletionCallback())
	})

	// rtl reports whether the caret currently sits inside a right-to-left
	// segment of its line (resolved under the configured base direction).
	ps.RegisterCommand("rtl", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		line := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
		return pawscript.BoolStatus(bidi.RTLAt([]rune(line), w.CursorPos().Rune, e.winRTL(w)))
	})

	// go_pos_prior / go_pos_next walk the caret backward and forward through the
	// cursor ring — the trail of recent edit positions. They do not themselves
	// count as deliberate movements (they leave hasMoved untouched), so a run of
	// them stays a single navigation session until an edit or a real move.
	ps.RegisterCommand("go_pos_prior", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.cursorRingGo(false))
	})

	ps.RegisterCommand("go_pos_next", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.cursorRingGo(true))
	})

	// Editing commands (using TypeScript naming convention)
	ps.RegisterCommand("del_char_prior", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		// A backspace over a read-only buffer declines (false, no edit) but —
		// unlike every other mutation — WITHOUT the "Buffer is read-only" toast: a
		// key as ordinary as backspace should not nag. Other commands still warn.
		if w != nil && e.viewportReadOnly(w) {
			return pawscript.BoolStatus(false)
		}
		// When deleteNewlineAsChar is off for this viewport, a backspace at the
		// start of a line declines rather than joining it with the line above.
		// Fail with false and no visible error; no edit, so the undo coalescing run
		// is untouched.
		if w != nil && w.ViewState.ProtectNewlines && e.deleteWouldRemoveNewline(w, false) {
			return pawscript.BoolStatus(false)
		}
		e.deleteCharBefore()
		e.trackEdit()
		e.editCoalesced = true // a single-point edit: coalesce the undo run
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("del_char_next", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		// A forward-delete over a read-only buffer declines silently too — no
		// "Buffer is read-only" toast (see del_char_prior).
		if w != nil && e.viewportReadOnly(w) {
			return pawscript.BoolStatus(false)
		}
		// Same guard forward: a forward-delete at end of line declines rather than
		// pulling the next line up. No edit → coalescing untouched.
		if w != nil && w.ViewState.ProtectNewlines && e.deleteWouldRemoveNewline(w, true) {
			return pawscript.BoolStatus(false)
		}
		e.deleteCharAt()
		e.trackEdit()
		e.editCoalesced = true // a single-point edit: coalesce the undo run
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("del_line", func(ctx *pawscript.Context) pawscript.Result {
		e.deleteLine()
		e.trackEdit()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("del_word_beg", func(ctx *pawscript.Context) pawscript.Result {
		e.deleteToWordStart()
		e.trackEdit()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("del_word_end", func(ctx *pawscript.Context) pawscript.Result {
		e.deleteToWordEnd()
		e.trackEdit()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("del_line_beg", func(ctx *pawscript.Context) pawscript.Result {
		e.deleteToLineStart()
		e.trackEdit()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("del_line_end", func(ctx *pawscript.Context) pawscript.Result {
		e.deleteToLineEnd()
		e.trackEdit()
		return pawscript.BoolStatus(true)
	})

	// Whitespace trimming, mirroring the del_line_* family. These trim the
	// current line's leading/trailing spaces and tabs; the line terminator
	// itself is never removed. Each reports true only when something was
	// actually trimmed, so scripts can chain alternatives with | and &.
	ps.RegisterCommand("trim_line_beg", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.trimLineStart()
		e.trackEdit()
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("trim_line_end", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.trimLineEnd()
		e.trackEdit()
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("trim_line", func(ctx *pawscript.Context) pawscript.Result {
		// Trims both ends: two separate deletes that must undo as one step.
		var buf *buffer.Buffer
		if w := e.ViewportManager.GetFocusedViewport(); w != nil {
			buf = w.Buffer
		}
		if buf != nil {
			buf.BeginUserCommand("trim_line")
			defer buf.EndUserCommand()
		}
		trimmedStart := e.trimLineStart()
		trimmedEnd := e.trimLineEnd()
		e.trackEdit()
		return pawscript.BoolStatus(trimmedStart || trimmedEnd)
	})

	// insert and insert_newline DISPATCH on what the focused viewport is.
	//
	// A viewport running a terminal session gets the input sent to the child
	// process, exactly as tinput would; anything else gets the buffer edit that
	// used to carry these names (now buffer_insert / buffer_insert_newline).
	//
	// This is why the keymaps need no pty variant. tab, return, and every
	// self-inserting key already end in `insert` or `insert_newline`, so typing
	// into a terminal viewport reaches the shell through the bindings that were
	// already there — and the same keys keep editing text everywhere else. One
	// name, two meanings, chosen by what is under the caret.
	ps.RegisterCommand("insert", func(ctx *pawscript.Context) pawscript.Result {
		if e.focusedPTY() != nil {
			if len(ctx.Args) > 0 {
				if sb, ok := ctx.Args[0].(pawscript.StoredBytes); ok {
					return pawscript.BoolStatus(e.ptySendBytes(sb.Data()))
				}
				return pawscript.BoolStatus(e.ptySendBytes([]byte(fmt.Sprintf("%v", ctx.Args[0]))))
			}
			return pawscript.BoolStatus(false)
		}
		return pawscript.BoolStatus(e.bufferInsertArgs(ctx.Args))
	})

	ps.RegisterCommand("insert_newline", func(ctx *pawscript.Context) pawscript.Result {
		if e.focusedPTY() != nil {
			// A shell wants CR for Enter, not LF: that is what a terminal
			// sends and what line discipline turns back into a newline.
			return pawscript.BoolStatus(e.ptySendBytes([]byte{'\r'}))
		}
		return pawscript.BoolStatus(e.bufferInsertNewline())
	})

	ps.RegisterCommand("buffer_insert", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.bufferInsertArgs(ctx.Args))
	})

	// buffer_insert_newline breaks the line like `buffer_insert '\n'`, then — when the
	// autoIndent option is on for the viewport — repeats the split line's
	// leading whitespace so the new line starts under its text. It is the tail
	// of the default Enter binding (nav_follow false|accept|insert_newline).
	// The plain name now DISPATCHES — see the pair registered above.
	ps.RegisterCommand("buffer_insert_newline", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.bufferInsertNewline())
	})

	// insert_bidi_control inserts a Unicode bidi control by short name (lrm,
	// rlm, alm, fsi, lri, rli, pdi) — otherwise behaving exactly like insert.
	// With no argument it prompts; "?" shows the legend and re-prompts.
	ps.RegisterCommand("insert_bidi_control", func(ctx *pawscript.Context) pawscript.Result {
		if arg, ok := argString(ctx, 0); ok {
			return pawscript.BoolStatus(e.insertBidiControl(arg))
		}
		expired := &atomic.Bool{}
		token := e.PawScript.RequestToken(func(string) { expired.Store(true) }, "",
			tokenTimeout(e.Config.PromptTimeout))
		var ask func()
		ask = func() {
			e.PromptMgr.PromptForInput("Insert control mark [lrm/rlm/alm, fsi/lri/rli, pdi, ?]: ", "",
				func(accepted bool, _, input string) {
					defer e.RequestRender()
					if expired.Load() {
						e.ShowWarning("Prompt timed out")
						return
					}
					name := strings.ToLower(strings.TrimSpace(input))
					if !accepted || name == "" {
						ctx.ResumeToken(token, false)
						return
					}
					if name == "?" {
						e.ShowNotification("lrm=left-to-right, rlm=right-to-left, alm=arabic letter mark")
						e.ShowNotification("fsi=first strong isolate, lri=left-to-right isolate, rli=right-to-left-isolate, pdi=pop directional isolate")
						ask() // re-prompt with the same prompt
						return
					}
					if _, ok := bidiControlRune(name); !ok {
						e.ShowWarning("Unknown control mark: " + name)
						ask() // stay in the loop on an unrecognized name
						return
					}
					ctx.ResumeToken(token, e.insertBidiControl(name))
				}, "bidictl")
		}
		ask()
		return pawscript.TokenResult(token)
	})

	// insert_rune inserts one Unicode scalar by CODE POINT: insert_rune "05D0",
	// or with no argument a prompt. Hex by default (U+xxxx, 0xxxxx or bare),
	// "#" prefixes decimal. The companion to insert_bidi_control for
	// every scalar that has no name of its own.
	// exec turns the focused buffer into a terminal session. The working
	// directory comes from the buffer itself (blank when it has no filename -
	// the HOST decides what that means); the command is the argument, or a
	// prompt when none is given.
	//
	// This deliberately shadows PawScript's own exec. PawScript is built for
	// exactly that: a host replacing an internal with its own version so that
	// client code reaches the sandboxed one. mew's exec cannot run anything by
	// itself - it can only ask its host.
	// A SECOND argument names the host's method — exec "cmd.exe" "2" — for a
	// host that has more than one way to make a terminal and no way to know
	// from here which one this machine wants. Blank is the host's default.
	// exec takes ANY NUMBER of arguments, joins them with single spaces, and
	// runs the whole line back through argwild — the same parser the host app
	// uses on its own command line at boot, so there is one notation to learn
	// rather than two. mew's switches come first (--pty=NAME), then the
	// program, then the program's own arguments; argwild's phase model does the
	// separating. See execargs.go for the spellings.
	//
	// shell is exec with the program left to the HOST: it asks for the user's
	// login shell rather than naming bash or cmd.exe, because which one that is
	// depends on the operating system and on the user's own account and mew can
	// see neither. Everything else — the compositing, --pty=, named arguments,
	// the phase rules — is identical, so the two are registered from one body.
	// Any further words are the shell's own arguments, which is also how a
	// program gets run through it: `shell "-c make"`.
	registerExecLike := func(name string, shell bool) {
		parse := parseExecLineNamed
		if shell {
			parse = parseShellLineNamed
		}
		request := func(spec execSpec) bool {
			// A routed stream (--inblock/--outblock or --stdin/--stdout/--stderr)
			// makes this a FILTER, not a terminal: the child runs on pipes and mew
			// feeds/reads its streams itself. Handled on its own path.
			if spec.filtering() {
				return e.runFilter(spec)
			}
			pol := spec.sizePolicy()
			if spec.Shell {
				return e.execRequestShellPolicy(spec.Args, spec.Method, pol, spec.Capture, spec.CaptureFormat)
			}
			return e.execRequestArgsPolicy(spec.Program, spec.Args, spec.Method, pol, spec.Capture, spec.CaptureFormat)
		}
		ps.RegisterCommand(name, func(ctx *pawscript.Context) pawscript.Result {
			var parts []string
			for i := 0; ; i++ {
				v, ok := argString(ctx, i)
				if !ok {
					break
				}
				parts = append(parts, v)
			}
			run := func(line string) bool {
				spec, err := parse(line, ctx.NamedArgs)
				if err != nil {
					e.ShowWarning(name + ": " + err.Error())
					return false
				}
				return request(spec)
			}
			if line := joinExecArgs(parts); strings.TrimSpace(line) != "" {
				return pawscript.BoolStatus(run(line))
			}
			// Nothing positional still counts as a request when it can stand on
			// its own: named arguments carrying a program, or shell, which needs
			// no arguments at all to mean something.
			spec, ok, err := namedOnlyRequest(ctx.NamedArgs, shell)
			if err != nil {
				e.ShowWarning(name + ": " + err.Error())
				return pawscript.BoolStatus(false)
			}
			if ok {
				return pawscript.BoolStatus(request(spec))
			}
			expired := &atomic.Bool{}
			token := e.PawScript.RequestToken(func(string) { expired.Store(true) }, "",
				tokenTimeout(e.Config.PromptTimeout))
			e.PromptMgr.PromptForInput("Execute what? ", "",
				func(accepted bool, _, input string) {
					defer e.RequestRender()
					if expired.Load() {
						e.ShowWarning("Prompt timed out")
						return
					}
					if !accepted || strings.TrimSpace(input) == "" {
						ctx.ResumeToken(token, false)
						return
					}
					ctx.ResumeToken(token, run(input))
				}, name)
			return pawscript.TokenResult(token)
		})
	}
	registerExecLike("exec", false)
	registerExecLike("shell", true)

	// tinput sends input to the focused viewport's terminal session — the same
	// direction the TUI host sends keystrokes into mew, and in the same
	// currency: raw terminal bytes.
	//
	// The argument takes whichever form the value already has. A PawScript
	// {bytes ...} value goes verbatim and whole, which is how a control byte or
	// an escape sequence is written without quoting games: {bytes 0x03} is
	// Ctrl-C, and Up is either {bytes 0x1b5b41} or the comma-separated list
	// {bytes 0x1b, 0x5b, 0x41}. The commas matter - without them it is symbol
	// concatenation, not a byte list. A string sends its UTF-8 bytes, so
	// tinput "ls\n" is what it looks like.
	//
	// pty_diag asks the host to test its terminal plumbing and writes the
	// report into the buffer at the caret. For when exec produces a session
	// that starts and stops with nothing to show for it.
	ps.RegisterCommand("pty_diag", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.ptyDiagnose())
	})

	// viewport_pty_hide / _show / _toggle run the focused viewport's terminal
	// under the hood or bring it back — bindable so a running session can be
	// hidden or revealed part-way through. Each warns and does nothing when the
	// focused buffer has no session.
	ps.RegisterCommand("viewport_pty_hide", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.setViewportPTYHidden(1, "viewport_pty_hide"))
	})
	ps.RegisterCommand("viewport_pty_show", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.setViewportPTYHidden(-1, "viewport_pty_show"))
	})
	ps.RegisterCommand("viewport_pty_toggle", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.setViewportPTYHidden(0, "viewport_pty_toggle"))
	})

	// viewport_pty_kill ends the focused viewport's session; a `final` capture
	// then folds everything it produced up to that point. Warns and does nothing
	// when the focused buffer has no session.
	ps.RegisterCommand("viewport_pty_kill", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.killViewportPTY())
	})

	// raw_key_input hands the NEXT keystroke to a focused terminal's child
	// instead of running mew's binding for it — the escape hatch for the keys
	// mew itself claims. See armRawKey.
	ps.RegisterCommand("raw_key_input", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.armRawKey())
	})

	// Reports FALSE when the focused buffer runs nothing, so a chain like
	// tinput|insert falls through to ordinary editing.
	ps.RegisterCommand("tinput", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) > 0 {
			if sb, ok := ctx.Args[0].(pawscript.StoredBytes); ok {
				return pawscript.BoolStatus(e.ptySendBytes(sb.Data()))
			}
		}
		text, _ := argString(ctx, 0)
		return pawscript.BoolStatus(e.ptySendBytes([]byte(text)))
	})

	// tinput_key forwards THE KEY BEING DISPATCHED to the focused viewport's
	// child process, encoded by the host's emulator — so application cursor
	// mode and its kin decide the bytes (\x1b[A vs \x1bOA for Up), not a
	// table here. This is what `capture * = tinput_key` in [pty::mappings]
	// runs: the terminal's first claim on every key. Reports FALSE — declining
	// the key — when there is no session, no host encoder, or the name encodes
	// to nothing, which drops resolution to the next level down. An explicit
	// key name may be given as an argument for scripted use.
	ps.RegisterCommand("tinput_key", func(ctx *pawscript.Context) pawscript.Result {
		key := e.dispatchingKey
		if len(ctx.Args) > 0 {
			if s, ok := argString(ctx, 0); ok && s != "" {
				key = s
			}
		}
		if key == "" {
			return pawscript.BoolStatus(false)
		}
		return pawscript.BoolStatus(e.sendKeyToPTY(key))
	})

	// terminal_grid <id>, <cols>, <rows> — the host declaring how many cells
	// of text its display actually renders for a session, which is not the
	// same as the cell rectangle mew placed it in. See ptyState.gridCols.
	ps.RegisterCommand("terminal_grid", func(ctx *pawscript.Context) pawscript.Result {
		id, ok := argString(ctx, 0)
		if !ok {
			return pawscript.BoolStatus(false)
		}
		cols, okc := argInt(ctx, 1)
		rows, okr := argInt(ctx, 2)
		if !okc || !okr {
			return pawscript.BoolStatus(false)
		}
		return pawscript.BoolStatus(e.SetTerminalGrid(id, cols, rows))
	})

	ps.RegisterCommand("insert_rune", func(ctx *pawscript.Context) pawscript.Result {
		if arg, ok := argString(ctx, 0); ok {
			r, ok := parseCodePoint(arg)
			if !ok {
				e.ShowWarning("Not a Unicode code point: " + arg)
				return pawscript.BoolStatus(false)
			}
			return pawscript.BoolStatus(e.insertRuneAt(r))
		}
		expired := &atomic.Bool{}
		token := e.PawScript.RequestToken(func(string) { expired.Store(true) }, "",
			tokenTimeout(e.Config.PromptTimeout))
		var ask func()
		ask = func() {
			e.PromptMgr.PromptForInput("Insert rune by code point [U+xxxx hex, #NNN decimal, \\uXXXX]: ", "",
				func(accepted bool, _, input string) {
					defer e.RequestRender()
					if expired.Load() {
						e.ShowWarning("Prompt timed out")
						return
					}
					if !accepted || strings.TrimSpace(input) == "" {
						ctx.ResumeToken(token, false)
						return
					}
					r, ok := parseCodePoint(input)
					if !ok {
						e.ShowWarning("Not a Unicode code point: " + input)
						ask() // stay in the loop rather than eat the keystroke
						return
					}
					ctx.ResumeToken(token, e.insertRuneAt(r))
				}, "insrune")
		}
		ask()
		return pawscript.TokenResult(token)
	})

	// insert_raw_byte inserts bytes verbatim. A PawScript {bytes ...} value goes
	// in whole; otherwise a single byte 0..255, written whichever way the value is
	// already in your head - see parseByteSpec for the full set: ^[ or ESC for
	// the escape character, x1b, o33, b11011, #27. "?" in the prompt lists
	// them.
	ps.RegisterCommand("insert_raw_byte", func(ctx *pawscript.Context) pawscript.Result {
		// A {bytes ...} value is the language's own way to say this, so take it
		// first and take ALL of it: {bytes 0xDEADBEEF} inserts four bytes, not
		// one. Every other spelling below carries a single byte because it is a
		// way of NAMING one; this is a way of holding a sequence.
		if len(ctx.Args) > 0 {
			if sb, ok := ctx.Args[0].(pawscript.StoredBytes); ok {
				return pawscript.BoolStatus(e.insertRawBytesAt(sb.Data()))
			}
		}
		if arg, ok := argString(ctx, 0); ok {
			r, ok := parseByteSpec(arg)
			if !ok {
				e.ShowWarning("Not a byte value: " + arg)
				return pawscript.BoolStatus(false)
			}
			return pawscript.BoolStatus(e.insertRawByteAt(byte(r)))
		}
		expired := &atomic.Bool{}
		token := e.PawScript.RequestToken(func(string) { expired.Store(true) }, "",
			tokenTimeout(e.Config.PromptTimeout))
		var ask func()
		ask = func() {
			e.PromptMgr.PromptForInput("Insert raw byte [x1b, o33, b11011, #27, ^[, \\e, ESC, ?]: ", "",
				func(accepted bool, _, input string) {
					defer e.RequestRender()
					if expired.Load() {
						e.ShowWarning("Prompt timed out")
						return
					}
					in := strings.TrimSpace(input)
					if !accepted || in == "" {
						ctx.ResumeToken(token, false)
						return
					}
					if in == "?" {
						e.ShowNotification("x1b/1b=hex (default), o33=octal, b11011=binary, #27=decimal")
						e.ShowNotification("^[=control (^@..^_, ^?=DEL), \\n \\r \\t \\e \\0 \\xNN \\NNN, or a name: BEL BS TAB LF CR ESC DEL")
						ask() // re-prompt, same as insert_bidi_control's "?"
						return
					}
					r, ok := parseByteSpec(in)
					if !ok {
						e.ShowWarning("Not a byte value: " + in)
						ask()
						return
					}
					ctx.ResumeToken(token, e.insertRawByteAt(byte(r)))
				}, "insbyte")
		}
		ask()
		return pawscript.TokenResult(token)
	})

	// Undo/Redo (using Garland's versioning, TypeScript naming convention)
	ps.RegisterCommand("buffer_undo", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		if !w.Buffer.Undo() {
			return pawscript.BoolStatus(false)
		}
		e.syncCursorAfterUndoRedo(w)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("buffer_redo", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		if !w.Buffer.Redo() {
			return pawscript.BoolStatus(false)
		}
		e.syncCursorAfterUndoRedo(w)
		return pawscript.BoolStatus(true)
	})

	// Explicit transaction control for script authors: bracket a run of edits
	// so they collapse into ONE undo revision (named by the optional argument),
	// or roll the whole run back. These nest, and pair with the automatic undo
	// coalescing that already groups plain typing — a script wrapping a compound
	// edit (search-and-replace, reformat, multi-step macro) gets one clean undo
	// step. buffer_tx_start with no matching commit/cancel is closed at the end
	// of the enclosing command dispatch, so a stray open transaction can't leak.
	ps.RegisterCommand("buffer_tx_start", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		name := "transaction"
		if len(ctx.Args) > 0 {
			if s := fmt.Sprintf("%v", ctx.Args[0]); s != "" {
				name = s
			}
		}
		w.Buffer.BeginUserCommand(name)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("buffer_tx_commit", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		w.Buffer.EndUserCommand()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("buffer_tx_cancel", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		w.Buffer.CancelUserCommand()
		e.syncCursorAfterUndoRedo(w) // rolled-back content: resync the caret
		return pawscript.BoolStatus(true)
	})

	// Mark commands
	ps.RegisterCommand("set_mark", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		// With an explicit name, set it directly.
		if len(ctx.Args) > 0 {
			return pawscript.BoolStatus(e.setUserMark(w, fmt.Sprintf("%v", ctx.Args[0]), w.CursorPos().Line, w.CursorPos().Rune))
		}
		// No name given (e.g. "esc esc") - prompt for the mark identifier.
		// The position is captured as a garland decoration, not as absolute
		// coordinates: anything that edits the buffer while the prompt is up
		// (a second viewport, an async script) slides the pending mark along
		// with the text, so the mark lands where the caret's TEXT is, not
		// where its line number used to be.
		const pendingMark = "_pending_set_mark"
		w.Buffer.SetMark(pendingMark, w.CursorPos().Line, w.CursorPos().Rune)
		e.PromptForInput("Set mark (0-9): ", "", func(input string, accepted bool) {
			line, rune_, exists := w.Buffer.GetMark(pendingMark)
			w.Buffer.ClearMark(pendingMark)
			if accepted && exists {
				name := strings.TrimSpace(input)
				w.Buffer.BeginUserCommand("set_mark")
				e.setUserMark(w, name, line, rune_)
				w.Buffer.EndUserCommand()
			}
			e.RequestRender()
		})
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_mark", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		// With an explicit name, jump directly.
		if len(ctx.Args) > 0 {
			return pawscript.BoolStatus(e.gotoUserMark(w, fmt.Sprintf("%v", ctx.Args[0])))
		}
		// No name given (e.g. "esc esc") - prompt for the mark identifier, then
		// jump to it (mirroring set_mark's no-argument prompt).
		e.PromptForInput("Go to mark (0-9): ", "", func(input string, accepted bool) {
			if accepted {
				e.gotoUserMark(w, strings.TrimSpace(input))
			}
			e.RequestRender()
		})
		return pawscript.BoolStatus(true)
	})

	// Block-selection mark commands. These encapsulate the internal
	// _block_begin/_block_end marks so keybindings never name them directly
	// (keeping the "_" internal-mark namespace out of user-facing config).
	ps.RegisterCommand("set_block_begin", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.setBlockMark("_block_begin", "Block begin"))
	})
	ps.RegisterCommand("set_block_end", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.setBlockMark("_block_end", "Block end"))
	})
	ps.RegisterCommand("go_block_begin", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.goBlockMark("_block_begin", "Block begin"))
	})
	ps.RegisterCommand("go_block_end", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.goBlockMark("_block_end", "Block end"))
	})

	// File commands
	ps.RegisterCommand("buffer_save", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		filename := w.Buffer.GetFilename()
		if filename == "" {
			// No filename - prompt for one via buffer_save_as behavior (with history)
			e.PromptMgr.PromptForFilename("Save as", "", func(accepted bool, _, cursorLineText string) {
				if accepted && cursorLineText != "" {
					e.requestSave(w.Buffer, cursorLineText, nil)
				}
				e.RequestRender()
			})
			return pawscript.BoolStatus(true)
		}
		e.requestSave(w.Buffer, filename, nil)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("buffer_save_as", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		currentFilename := w.Buffer.GetFilename()
		e.PromptMgr.PromptForFilename("Save as", currentFilename, func(accepted bool, _, cursorLineText string) {
			if accepted && cursorLineText != "" {
				e.requestSave(w.Buffer, cursorLineText, nil)
			}
			e.RequestRender()
		})
		return pawscript.BoolStatus(true)
	})

	// buffer_write EXPORTS the whole buffer to a prompted-for file without
	// adopting it as the source (garland's SaveCopyTo): the buffer keeps its
	// original source, filename, modified flag, and save history. It is the
	// whole-buffer parallel to block_write — a plain "write these bytes there",
	// not a save-as. Distinct from buffer_save_as, which re-homes the buffer.
	ps.RegisterCommand("buffer_write", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.writeBufferCopy())
	})

	// buffer_save_all saves every modified buffer, once each — including buffers
	// stacked in a viewport's nav history (unsaved work parked behind a link
	// follow). With the argument "true" it is NON-INTERACTIVE (for save-and-quit,
	// `buffer_save_all true & exit`): it never prompts, skips unnamed/changed
	// buffers with a notice, and returns false if anything was skipped or failed.
	// Otherwise it is INTERACTIVE: it prompts per file (name / overwrite / create
	// directory), and ANY ^C bails the whole remaining batch with a false result.
	ps.RegisterCommand("buffer_save_all", func(ctx *pawscript.Context) pawscript.Result {
		var pending []*buffer.Buffer
		for _, b := range e.openDocViewports() {
			if b.IsModified() {
				pending = append(pending, b)
			}
		}
		if len(pending) == 0 {
			e.ShowNotification("No modified files to save")
			return pawscript.BoolStatus(true)
		}
		nonInteractive := false
		if arg, ok := argString(ctx, 0); ok && strings.EqualFold(strings.TrimSpace(arg), "true") {
			nonInteractive = true
		}
		// Both modes may prompt (the "true" mode only for never-saved buffers,
		// which now surface with a Save-as instead of being skipped), and the
		// prompts are async. If the whole batch finishes without suspending on
		// a prompt, report the result synchronously; otherwise defer it
		// through a token so a following `& exit` waits for — and respects —
		// the outcome. The prompt callbacks cannot fire until this command
		// returns to the main loop, so `token` is always set before any
		// resume runs.
		token := ""
		completedSync, syncResult := false, false
		finish := func(success bool) {
			if token == "" {
				completedSync, syncResult = true, success
			} else {
				ctx.ResumeToken(token, success)
			}
		}
		if nonInteractive {
			e.saveAllNonInteractive(pending, finish)
		} else {
			e.saveAllInteractive(pending, finish)
		}
		if completedSync {
			return pawscript.BoolStatus(syncResult)
		}
		token = e.PawScript.RequestToken(nil, "", tokenTimeout(0))
		return pawscript.TokenResult(token)
	})

	// Block commands (TypeScript uses set_mark '_block_begin' / '_block_end')
	ps.RegisterCommand("block_copy", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.copyBlock()
		e.trackEdit()
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("block_delete", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.deleteBlock()
		if ok {
			e.trackEdit() // consumes the kill flag so accumulation chains work
		}
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("block_move", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.moveBlock()
		e.trackEdit()
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("block_write", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.writeBlock())
	})

	// Kill ring (emacs-style). Deletes accumulate into kill entries as they
	// run (see killCapture); these commands read the ring back.
	ps.RegisterCommand("block_copy_kill", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.blockCopyKill())
	})

	ps.RegisterCommand("kill_ring_yank", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.killRingYank()
		if ok {
			e.trackEdit() // an insert-style edit: cursor ring + breaks kill chain
		}
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("kill_ring_pop", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.killRingPop()
		if ok {
			e.trackEdit()
		}
		return pawscript.BoolStatus(ok)
	})

	// kill_ring_append arms the next kill to accumulate into the most recent
	// kill entry even if it would otherwise start a new one (append-next-kill).
	ps.RegisterCommand("kill_ring_append", func(ctx *pawscript.Context) pawscript.Result {
		e.killAppendNext = true
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("block_indent", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.indentBlock())
	})

	ps.RegisterCommand("block_unindent", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.unindentBlock())
	})

	// block_from_file streams a prompted-for file over the marked block: it
	// replaces the block's contents with the file's, but only when a block is
	// marked AND the caret sits within it (or on either edge). The gate is
	// enforced up front (promptBlockFromFile) so the filename is never even
	// asked for on an invalid target; the replace itself (blockFromFile) is
	// wrapped like the other block mutations — one grouped undo, block left
	// marked around the streamed-in text.
	ps.RegisterCommand("block_from_file", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.promptBlockFromFile())
	})

	// block_filter pipes the marked block through a shell command and replaces it
	// with the result. The command may be given inline (block_filter sort -r);
	// with none it prompts, recalling prior filter
	// commands. It is the ergonomic spelling of exec --stdin=block --stdout=block
	// --stderr=block, so stdout and stderr both flow back into the block.
	ps.RegisterCommand("block_filter", func(ctx *pawscript.Context) pawscript.Result {
		var parts []string
		for i := 0; ; i++ {
			v, ok := argString(ctx, i)
			if !ok {
				break
			}
			parts = append(parts, v)
		}
		return pawscript.BoolStatus(e.blockFilter(strings.Join(parts, " ")))
	})

	// OS-clipboard commands: the host system-clipboard bridge (see
	// osclipboard.go). A channel deliberately separate from the kill ring.
	ps.RegisterCommand("os_copy", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.osCopy())
	})
	ps.RegisterCommand("os_cut", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.osCut())
	})
	ps.RegisterCommand("os_paste", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.osPaste())
	})
	ps.RegisterCommand("os_select_all", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.osSelectAll())
	})
	// os_exchange swaps the block with the clipboard in one gesture. It is not
	// os_copy + os_paste: after the copy the clipboard holds the block, so the
	// outgoing text must be captured before the incoming text lands.
	ps.RegisterCommand("os_exchange", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.osExchange())
	})

	ps.RegisterCommand("buffer_insert_file", func(ctx *pawscript.Context) pawscript.Result {
		e.PromptMgr.PromptForFilename("Insert file", "", func(accepted bool, _, filename string) {
			if accepted && filename != "" {
				e.insertFile(filename)
			}
			e.RequestRender()
		})
		return pawscript.BoolStatus(true)
	})

	// Multi-buffer commands
	// buffer_open_file with an argument opens THAT file directly (no prompt) —
	// the scripted/menu equivalent of typing the name at the Open prompt, e.g.
	// `buffer_open_file "help:/"`. With no argument it raises the Open prompt.
	ps.RegisterCommand("buffer_open_file", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) > 0 {
			name := strings.TrimSpace(fmt.Sprintf("%v", ctx.Args[0]))
			if name != "" {
				ok := e.openFile(name)
				e.RequestRender()
				return pawscript.BoolStatus(ok)
			}
		}
		e.PromptMgr.PromptForFilename("Open", "", func(accepted bool, _, cursorLineText string) {
			if accepted && cursorLineText != "" {
				e.openFile(cursorLineText)
			}
			e.RequestRender()
		})
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("buffer_new", func(ctx *pawscript.Context) pawscript.Result {
		e.createNewBuffer()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("buffer_duplicate", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.duplicateCurrentBuffer())
	})

	// viewport_clone opens a second viewport onto the SAME buffer (not a content
	// copy like buffer_duplicate) so you can edit in two places at once and switch
	// between them. Each viewport keeps its own caret and viewport.
	ps.RegisterCommand("viewport_clone", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.cloneCurrentViewport())
	})

	ps.RegisterCommand("viewport_close", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.closeCurrentBuffer())
	})

	// buffer_close closes the focused viewport's buffer from EVERYWHERE it is
	// referenced (viewport_close closes just the one viewport): active views
	// mirror viewport_close and nav-history references become mew:/closed
	// tombstones. Modified buffers prompt once first.
	ps.RegisterCommand("buffer_close", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.closeBufferEverywhere())
	})

	// buffer_revert seeks the buffer's history back to its last save point.
	// A pure history move: redo still reaches the abandoned edits.
	ps.RegisterCommand("buffer_revert", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		if err := w.Buffer.RevertToLastSave(); err != nil {
			e.ShowError("Revert: " + err.Error())
			return pawscript.BoolStatus(false)
		}
		e.syncCursorAfterUndoRedo(w)
		e.noteBuffer(w.Buffer, "save", "Reverted to last save (redo restores the edits)", false)
		return pawscript.BoolStatus(true)
	})

	// screen_refresh discards the renderer's knowledge of what the terminal is
	// showing and forces a full clear-and-repaint on the next frame — recovery
	// from external corruption of the display (a stray program writing over it,
	// a garbled resize) that the incremental diff would otherwise preserve.
	ps.RegisterCommand("screen_refresh", func(ctx *pawscript.Context) pawscript.Result {
		e.Renderer.ForceRedraw()
		e.RequestRender()
		return pawscript.BoolStatus(true)
	})

	// set_font re-points a host font alias at one or more font names on a
	// graphical host (e.g. set_font "ui-term", "JetBrainsMono"): the first name
	// that resolves is used, later names are fallbacks. The host loads unknown
	// names (system scan / configured search paths) and repaints live. On a
	// plain terminal (no FontSink) it warns — fonts are the terminal's there.
	ps.RegisterCommand("set_font", func(ctx *pawscript.Context) pawscript.Result {
		alias, ok := argString(ctx, 0)
		if !ok || strings.TrimSpace(alias) == "" {
			e.ShowWarning("set_font: usage: set_font \"<alias>\", \"<font>\" [, \"<fallback>\"...]")
			return pawscript.BoolStatus(false)
		}
		var names []string
		for i := 1; ; i++ {
			n, ok := argString(ctx, i)
			if !ok {
				break
			}
			if n = strings.TrimSpace(n); n != "" {
				names = append(names, n)
			}
		}
		if len(names) == 0 {
			e.ShowWarning("set_font: no font name given")
			return pawscript.BoolStatus(false)
		}
		if e.Config.FontSink == nil {
			e.ShowWarning("set_font: not supported on this terminal")
			return pawscript.BoolStatus(false)
		}
		got := e.Config.FontSink(strings.TrimSpace(alias), names)
		e.RequestRender()
		if got {
			e.ShowNotification(fmt.Sprintf("Font %q set to %s", alias, names[0]))
		} else {
			e.ShowWarning(fmt.Sprintf("Font %q: %q not found (using fallback)", alias, names[0]))
		}
		return pawscript.BoolStatus(got)
	})

	// debug_screen arms a full-frame ANSI snapshot of the screen, written to a
	// timestamped ".ans" file in the box:/// support tree (~/.mew locally) — a
	// capture that reproduces the screen when cat'd to a terminal. The write
	// rides the NEXT render (see performRender): the command only arms the target
	// and forces a full repaint, so it never re-renders (or re-locks renderMu)
	// itself — snapshotting the exact frame that gets painted.
	ps.RegisterCommand("debug_screen", func(ctx *pawscript.Context) pawscript.Result {
		e.pendingScreenCapture = "box:///" + time.Now().Format("2006-01-02 15.04.05") + ".ans"
		e.Renderer.ForceRedraw()
		e.RequestRender()
		return pawscript.BoolStatus(true)
	})

	// deadcat forces a crash-style dump of every modified buffer to the
	// resolved DEADCAT location — mew's DEADJOE, on demand (also the path the
	// signal/panic handlers and a host's shutdown take).
	ps.RegisterCommand("deadcat", func(ctx *pawscript.Context) pawscript.Result {
		reason := "deadcat command"
		if len(ctx.Args) > 0 {
			if s, ok := argString(ctx, 0); ok && s != "" {
				reason = s
			}
		}
		path, err := e.DumpDeadcat(reason)
		if err != nil {
			e.ShowError("DEADCAT: " + err.Error())
			return pawscript.BoolStatus(false)
		}
		if path == "" {
			e.ShowNotification("DEADCAT: no modified buffers to dump")
			return pawscript.BoolStatus(false)
		}
		e.ShowNotification("DEADCAT written: " + path)
		return pawscript.BoolStatus(true)
	})

	// buffer_status re-exposes the buffer's source-safety picture: source
	// consistency, lock and backup state, and every captured notice (the
	// transients that may have timed out unseen).
	ps.RegisterCommand("buffer_status", func(ctx *pawscript.Context) pawscript.Result {
		w := e.resolveTargetMain()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		buf := e.lib.NewFromString(e.bufferStatusText(w.Buffer))
		e.ViewportManager.CreateViewport(viewport.ViewportOptions{
			Type:             viewport.ToolViewport,
			ViewportSet:      "help",
			Class:            "bufstatus",
			Dock:             viewport.DockTop,
			Priority:         100,
			MinHeight:        5,
			MaxHeight:        15,
			MessageTopCenter: "Buffer Status",
			Buffer:           buf,
			ShowLineNumbers:  false,
		})
		e.RequestRender()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("buffer_next", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.cycleBuffer(1))
	})

	ps.RegisterCommand("buffer_prior", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.cycleBuffer(-1))
	})

	ps.RegisterCommand("buffer_list", func(ctx *pawscript.Context) pawscript.Result {
		// Navigate the focused document to the generated mew:/buffers surface:
		// a dynamic, read-only dokuwiki list of open buffers, in navigation mode.
		return pawscript.BoolStatus(e.openGeneratedSurface("buffers"))
	})

	ps.RegisterCommand("viewport_list", func(ctx *pawscript.Context) pawscript.Result {
		// The mew:/viewports companion to buffer_list.
		return pawscript.BoolStatus(e.openGeneratedSurface("viewports"))
	})

	// Search commands. find takes up to three optional arguments -
	// find [term], [options], [replacement] - and prompts only for what is
	// missing and necessary (see startFind). Option letters: i=ignore case,
	// b=backwards, a=all buffers, r=replace.
	ps.RegisterCommand("find", func(ctx *pawscript.Context) pawscript.Result {
		term, haveTerm := argString(ctx, 0)
		options, haveOptions := argString(ctx, 1)
		replacement, haveReplacement := argString(ctx, 2)
		e.startFind(term, options, replacement, haveTerm, haveOptions, haveReplacement)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("find_next", func(ctx *pawscript.Context) pawscript.Result {
		state := e.currentFindState()
		if state.Term == "" {
			// Nothing to repeat: fall into the interactive find flow.
			e.startFind("", "", "", false, false, false)
			return pawscript.BoolStatus(true)
		}
		// find_next always steps a single occurrence; a count in the stored
		// options applies only to the invocation that gave it. The scan runs on
		// a goroutine, so this reports only that the pass STARTED — the caret
		// move and any "Not found" arrive from findPump (see findasync.go).
		return pawscript.BoolStatus(e.findStepAsync(state, 1, true))
	})

	// search - incremental search: an "I-search:" prompt whose keystrokes
	// drive the caret to matches live, JOE-style (see isearch.go). It starts
	// in the direction the find state stored; search_reverse/search_forward
	// set the direction (stepping one occurrence) inside an open search, or
	// start one that way. isearch_key is the prompt's after-key classifier;
	// confirm_key is the single-keystroke classifier armed on yes/no
	// confirmation prompts (see confirmkey.go).
	ps.RegisterCommand("search", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.startIncrementalSearch(0))
	})
	ps.RegisterCommand("search_reverse", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.isearchDirection(true))
	})
	ps.RegisterCommand("search_forward", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.isearchDirection(false))
	})
	ps.RegisterCommand("isearch_key", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.isearchKeystroke())
	})
	ps.RegisterCommand("confirm_key", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.confirmKeyStroke())
	})

	// find_replace [term], [replacement] - find with replace mode forced;
	// prompts for whichever of the two is missing.
	ps.RegisterCommand("find_replace", func(ctx *pawscript.Context) pawscript.Result {
		term, haveTerm := argString(ctx, 0)
		replacement, haveReplacement := argString(ctx, 1)
		e.startFind(term, "r", replacement, haveTerm, true, haveReplacement)
		return pawscript.BoolStatus(true)
	})

	// verbose_log appends text to the shared verbose-log viewport (class
	// "verboseLog"), creating it in the background on first use - the
	// logging counterpart of insert. Each argument becomes its own line.
	ps.RegisterCommand("verbose_log", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) == 0 {
			e.ShowWarning("Usage: verbose_log <text>")
			return pawscript.BoolStatus(false)
		}
		for i := range ctx.Args {
			if text, ok := argString(ctx, i); ok {
				e.appendVerboseLog(text)
			}
		}
		e.RequestRender()
		return pawscript.BoolStatus(true)
	})

	// Command prompt (Esc X) - allows entering PawScript commands directly
	ps.RegisterCommand("cmd", func(ctx *pawscript.Context) pawscript.Result {
		e.PromptMgr.PromptForInput("18: Command: ", "", func(accepted bool, _, cursorLineText string) {
			if accepted && cursorLineText != "" {
				// ExecuteAsync, like executeCommand: this callback runs on
				// the main loop goroutine, and a typed command that opens a
				// prompt of its own suspends on a token that only the main
				// loop can resume — the blocking Execute would deadlock.
				e.PawScript.ExecuteAsync(cursorLineText)
			}
			e.RequestRender()
		}, "command")
		return pawscript.BoolStatus(true)
	})

	// Go to line command. go_line [n] goes directly; without an argument it
	// prompts, with history reachable by arrow but never defaulted (the
	// prompt starts blank). An invalid entry warns "Invalid line number" and
	// the command resolves false — the prompt suspends the calling command
	// sequence on an async token and resumes it with the outcome, so a
	// script can chain an alternative with the | else operator. Cancelling
	// or accepting a blank entry also resolves false, without the warning.
	ps.RegisterCommand("go_line", func(ctx *pawscript.Context) pawscript.Result {
		goLine := func(input string) bool {
			n, err := strconv.Atoi(strings.TrimSpace(input))
			if err != nil || n < 1 {
				e.ShowWarning("Invalid line number")
				return false
			}
			e.gotoLine(n)
			return true
		}
		if arg, ok := argString(ctx, 0); ok {
			return pawscript.BoolStatus(goLine(arg))
		}
		// PawScript force-cleans suspension tokens after the promptTimeout
		// option (seconds, 0 = never), dropping the suspended sequence; the
		// cleanup callback records that. A prompt answered after its token
		// expired defaults to FAILURE: warn and perform nothing, rather
		// than half-succeeding (jumping) with the command's chain already
		// dead.
		expired := &atomic.Bool{}
		token := e.PawScript.RequestToken(func(string) { expired.Store(true) }, "",
			tokenTimeout(e.Config.PromptTimeout))
		e.PromptMgr.PromptForInput("Go to line: ", "", func(accepted bool, _, input string) {
			defer e.RequestRender()
			if expired.Load() {
				e.ShowWarning("Prompt timed out")
				return
			}
			if !accepted || strings.TrimSpace(input) == "" {
				ctx.ResumeToken(token, false)
				return
			}
			ctx.ResumeToken(token, goLine(input))
		}, "goline")
		return pawscript.TokenResult(token)
	})

	// repeat_next arms the next keybound command to run inside a PawScript
	// repeat(...) N times. With a count argument it arms immediately; with none
	// it prompts. The count is clamped to the maxRepeat option. The arming is
	// tracked per-viewport (like Find); the command dispatcher consumes it.
	ps.RegisterCommand("repeat_next", func(ctx *pawscript.Context) pawscript.Result {
		w := e.resolveTargetMain()
		if w == nil {
			return pawscript.BoolStatus(false)
		}
		arm := func(input string) bool {
			n, err := strconv.Atoi(strings.TrimSpace(input))
			if err != nil || n < 1 {
				e.ShowWarning("Repeat count must be a positive integer")
				return false
			}
			max := e.Config.MaxRepeat
			if max < 1 {
				max = 100
			}
			if n > max {
				n = max
			}
			w.Repeat = viewport.RepeatState{Pending: true, Count: n}
			return true
		}
		if arg, ok := argString(ctx, 0); ok {
			return pawscript.BoolStatus(arm(arg))
		}
		expired := &atomic.Bool{}
		token := e.PawScript.RequestToken(func(string) { expired.Store(true) }, "",
			tokenTimeout(e.Config.PromptTimeout))
		e.PromptMgr.PromptForInput("Repeat next command (count): ", "", func(accepted bool, _, input string) {
			defer e.RequestRender()
			if expired.Load() {
				e.ShowWarning("Prompt timed out")
				return
			}
			if !accepted || strings.TrimSpace(input) == "" {
				ctx.ResumeToken(token, false)
				return
			}
			ctx.ResumeToken(token, arm(input))
		}, "repeatnext")
		return pawscript.TokenResult(token)
	})

	// Scroll commands
	// scroll_left / scroll_right are VISUAL, not reading-relative: unlike the
	// _prior/_next commands, which follow the text's own direction, these move
	// the view the way the words name whichever way the text runs. Under
	// direction=rtl that is the opposite sign on the stored offset — see
	// scrollViewHorizontal.
	ps.RegisterCommand("scroll_left", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.scrollViewHorizontal(e.ViewportManager.GetFocusedViewport(), -1))
	})

	ps.RegisterCommand("scroll_right", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.scrollViewHorizontal(e.ViewportManager.GetFocusedViewport(), +1))
	})

	// Viewport navigation commands. These cycle only viewports currently on
	// screen (SetCycleVisibleFilter), so they always land on an already-tiled
	// pane — no adoptFocusInPlace needed.
	ps.RegisterCommand("viewport_next", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.ViewportManager.FocusNextViewport()
		if ok {
			e.announceFocusedViewport()
		}
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("viewport_prior", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.ViewportManager.FocusPrevViewport()
		if ok {
			e.announceFocusedViewport()
		}
		return pawscript.BoolStatus(ok)
	})

	// Peek commands
	ps.RegisterCommand("stat_peek_up", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.ViewportManager.StatPeekUp())
	})

	ps.RegisterCommand("stat_peek_down", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.ViewportManager.StatPeekDown())
	})

	ps.RegisterCommand("prompt_peek_up", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.ViewportManager.PromptPeekUp())
	})

	ps.RegisterCommand("prompt_peek_down", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.ViewportManager.PromptPeekDown())
	})

	// Help toggle command
	// help_toggle with a page argument opens that help wiki page in the shared
	// help slot (help_toggle "keys"); with no argument it toggles the built-in
	// Quick Help. So one key can open the help index and another the Quick Help.
	ps.RegisterCommand("help_toggle", func(ctx *pawscript.Context) pawscript.Result {
		arg := ""
		if len(ctx.Args) > 0 {
			arg = fmt.Sprintf("%v", ctx.Args[0])
		}
		return pawscript.BoolStatus(e.toggleHelp(arg))
	})

	// help_open is help_toggle that only ever opens/replaces (never closes) and
	// FOCUSES the help viewport — for landing in help ready to scroll and follow
	// links, versus help_toggle's peek that leaves the caret in place.
	ps.RegisterCommand("help_open", func(ctx *pawscript.Context) pawscript.Result {
		arg := ""
		if len(ctx.Args) > 0 {
			arg = fmt.Sprintf("%v", ctx.Args[0])
		}
		return pawscript.BoolStatus(e.openHelpFocused(arg))
	})

	// Editor options command
	ps.RegisterCommand("editor_options", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.toggleOptions())
	})

	// Render command
	ps.RegisterCommand("render", func(ctx *pawscript.Context) pawscript.Result {
		e.RequestRender()
		return pawscript.BoolStatus(true)
	})

	// Status command
	ps.RegisterCommand("status", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) > 0 {
			e.ShowNotification(fmt.Sprintf("%v", ctx.Args[0]))
			return pawscript.BoolStatus(true)
		}
		return pawscript.BoolStatus(false)
	})

	// set_option <name>, <value> - change a runtime editor option on the last
	// active editor viewport (NOTE: arguments are comma-separated).
	ps.RegisterCommand("set_option", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) < 1 {
			e.ShowWarning("Usage: set_option <name>[, <value>]")
			return pawscript.BoolStatus(false)
		}
		name := fmt.Sprintf("%v", ctx.Args[0])
		// Target the last active main-buffer viewport, not whatever is focused
		// (a prompt viewport would be focused and is about to close).
		w := e.ViewportManager.GetLastMainViewport()
		if len(ctx.Args) < 2 {
			// No value given: prompt for one, seeding the choices from the
			// registry (the same value list set_option_next rotates through).
			return pawscript.BoolStatus(e.promptSetOption(w, name))
		}
		value := fmt.Sprintf("%v", ctx.Args[1])
		return pawscript.BoolStatus(e.setOption(w, name, value))
	})

	// set_option_next / set_option_prior <name> - rotate an option through its
	// canonical value sequence (from optionSpecs): read the current value via the
	// cascade, step to the next/previous value, and set it. Fails with a warning
	// for options that have no fixed value set (integers, counts, free text).
	rotate := func(dir int) func(*pawscript.Context) pawscript.Result {
		return func(ctx *pawscript.Context) pawscript.Result {
			if len(ctx.Args) < 1 {
				e.ShowWarning("Usage: set_option_next/prior <name>")
				return pawscript.BoolStatus(false)
			}
			name := fmt.Sprintf("%v", ctx.Args[0])
			w := e.ViewportManager.GetLastMainViewport()
			return pawscript.BoolStatus(e.rotateOption(w, name, dir))
		}
	}
	ps.RegisterCommand("set_option_next", rotate(+1))
	ps.RegisterCommand("set_option_prior", rotate(-1))

	// clear_option <name> - drop a per-viewport option's explicit override on the
	// active viewport, reverting it to the resolved default (the configured /
	// inherited value). Fails for global options and unknown names.
	ps.RegisterCommand("clear_option", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) < 1 {
			e.ShowWarning("Usage: clear_option <name>")
			return pawscript.BoolStatus(false)
		}
		name := fmt.Sprintf("%v", ctx.Args[0])
		w := e.ViewportManager.GetLastMainViewport()
		return pawscript.BoolStatus(e.clearOption(w, name))
	})

	// get_option <name> - return the current effective value of an option, as a
	// substitutable result, e.g. insert {get_option "tabSize"}.
	ps.RegisterCommand("get_option", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) < 1 {
			e.ShowWarning("Usage: get_option <name>")
			return pawscript.BoolStatus(false)
		}
		name := fmt.Sprintf("%v", ctx.Args[0])
		w := e.ViewportManager.GetLastMainViewport()
		value, ok := e.getOption(w, name)
		if !ok {
			e.ShowWarning("Unknown option: " + name)
			return pawscript.BoolStatus(false)
		}
		ctx.SetResult(value)
		return pawscript.BoolStatus(true)
	})
}

// parseBoolOption parses a boolean option value (true/false/1/0/yes/no/on/off).
func parseBoolOption(v string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "on":
		return true, true
	case "false", "0", "no", "off":
		return false, true
	}
	return false, false
}

// boolText is the canonical display form of a boolean option: yes / no. Most
// boolean options carry a verb in their name (show…, …Ignores…, wrap), so
// "showMarks: yes" reads naturally. Input still accepts on/true/1/yes and
// off/false/0/no (parseBoolOption); this is only how a value is reported.
func boolText(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// getOption returns the current effective value of a named editor option (as a
// string) for the given main-buffer viewport. Per-viewport options are read from the
// viewport's view state (what actually renders); globals from the runtime Config.
func (e *Editor) getOption(w *viewport.Viewport, name string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "tabsize":
		v := e.Config.TabSize
		if w != nil && w.ViewState.TabSize > 0 {
			v = w.ViewState.TabSize
		}
		return strconv.Itoa(v), true
	case "showlinenumbers":
		v := e.Config.ShowLineNumbers
		if w != nil {
			v = w.ViewState.ShowLineNumbers
		}
		return boolText(v), true
	case "deletenewlineaschar":
		// Stored inverted (ProtectNewlines); report the on/off sense.
		v := e.Config.DeleteNewlineAsChar
		if w != nil {
			v = !w.ViewState.ProtectNewlines
		}
		return boolText(v), true
	case "showinvisibles":
		v := e.Config.ShowInvisibles
		if w != nil {
			v = w.ViewState.ShowInvisibles
		}
		return boolText(v), true
	case "showbidi":
		v := e.Config.ShowBidi
		if w != nil {
			v = w.ViewState.ShowBidi
		}
		return boolText(v), true
	case "rtlcombining":
		// Stored inverted (SuppressRTLCombining); report the on/off sense.
		v := e.Config.RtlCombining
		if w != nil {
			v = !w.ViewState.SuppressRTLCombining
		}
		return boolText(v), true
	case "showmarks":
		v := e.Config.ShowMarks
		if w != nil {
			v = w.ViewState.ShowMarks
		}
		if v == "" {
			v = "no"
		}
		return v, true
	case "insertmode":
		// Stored inverted (OverwriteMode); report the insert-mode sense.
		over := e.Config.OverwriteMode
		if w != nil {
			over = w.ViewState.OverwriteMode
		}
		return boolText(!over), true
	case "readonly":
		v := e.Config.ReadOnly
		if w != nil {
			v = w.ViewState.ReadOnly
		}
		return boolText(v), true
	case "insertcursor":
		return strconv.Itoa(e.Config.InsertCursor), true
	case "overwritecursor":
		return strconv.Itoa(e.Config.OverwriteCursor), true
	case "navigationcursor":
		return strconv.Itoa(e.Config.NavigationCursor), true
	case "autoindent":
		v := e.Config.AutoIndent
		if w != nil {
			v = w.ViewState.AutoIndent
		}
		return boolText(v), true
	case "linkbrowsing":
		v := e.Config.LinkBrowsing
		if w != nil {
			v = w.ViewState.LinkBrowsing
		}
		return boolText(v), true
	case "navigationmode":
		// Navigation mode is the viewport's live browse state — per-viewport
		// only, no global config default.
		v := false
		if w != nil {
			v = w.BrowseActive
		}
		return boolText(v), true
	case "showcolumnruler":
		v := e.Config.ShowColumnRuler
		if w != nil {
			v = w.ViewState.ShowRuler
		}
		return boolText(v), true
	case "scrollbar":
		v := e.Config.Scrollbar
		if w != nil {
			v = w.ViewState.Scrollbar
		}
		return boolText(v), true
	case "rulershowscursor":
		return boolText(e.Config.RulerShowsCursor), true
	case "syntax":
		return e.Config.Syntax, true
	case "syntaxdetect":
		return boolText(e.Config.SyntaxDetect), true
	case "syntaxoverrides":
		v := e.Config.SyntaxOverrides
		if w != nil {
			v = w.ViewState.SyntaxOverrides
		}
		return v, true
	case "macoptionkeys":
		if e.Config.MacOptionKeys == "" {
			return "auto", true
		}
		return e.Config.MacOptionKeys, true
	case "matchignoressinglequote", "matchignoresdoublequote", "matchignoresslashstar",
		"matchignoresslashslash", "matchignoreshash", "matchignoresdoublehyphen",
		"matchignoressemicolon", "matchignorespercent":
		return boolText(*e.matchIgnoreFlag(name)), true
	case "wordwrap":
		return boolText(e.Config.WordWrap), true
	case "searchignorecase":
		return boolText(e.Config.SearchIgnoreCase), true
	case "searchwrap":
		return boolText(e.Config.SearchWrap), true
	case "searchregex":
		return boolText(e.Config.SearchRegex), true
	case "searchbackwards":
		return boolText(e.Config.SearchBackwards), true
	case "searchallbuffers":
		return boolText(e.Config.SearchAllBuffers), true
	case "modebarlocation":
		return e.Config.ModebarLocation, true
	case "modebarinner":
		return e.optStr(w, "modebarinner", e.Config.ModebarInner), true
	case "modebardefault":
		return e.optStr(w, "modebardefault", e.Config.ModebarDefault), true
	case "modebarouter":
		return e.optStr(w, "modebarouter", e.Config.ModebarOuter), true
	case "mappings":
		return e.optStr(w, "mappings", e.Config.MappingsName), true
	case "pagesizeoptimal":
		return e.Config.PageSizeOptimal, true
	case "pageoverlapminimum":
		return e.Config.PageOverlapMinimum, true
	case "pagesizestep":
		return strconv.Itoa(e.Config.PageSizeStep), true
	case "maxrepeat":
		return strconv.Itoa(e.Config.MaxRepeat), true
	case "killringentries":
		return strconv.Itoa(e.Config.KillRingEntries), true
	case "direction":
		// Per-viewport override when set, else the global base direction.
		if w != nil && w.ViewState.Direction != "" {
			return w.ViewState.Direction, true
		}
		if e.Config.Direction == "rtl" {
			return "rtl", true
		}
		return "ltr", true
	case "flipbidiforhost":
		return e.Config.FlipBidiForHost, true
	case "rtlmarkmode":
		return e.Config.RtlMarkMode, true
	case "prompttimeout":
		return strconv.Itoa(e.Config.PromptTimeout), true
	case "scripttimeout":
		return strconv.Itoa(e.Config.ScriptTimeout), true
	}
	return "", false
}

// resolveRtlMarkMode turns the "auto" sentinel into a concrete rtlMarkMode from
// the detected host terminal; any explicit mode passes through unchanged. The
// stored option keeps reading "auto"; only the value pushed to the renderer and
// host is resolved. Terminals whose quirks are not yet handled resolve to
// "normal".
func resolveRtlMarkMode(mode string) string {
	if mode != "auto" {
		return mode
	}
	return rtlMarkModeForTerminal(hostterm.Detect())
}

// rtlMarkModeForTerminal maps a detected host terminal to the rtlMarkMode "auto"
// selects for it. Terminals whose quirks are not yet handled map to "normal".
func rtlMarkModeForTerminal(k hostterm.Kind) string {
	switch k {
	case hostterm.TerminalITerm2:
		return "iterm2"
	case hostterm.TerminalAlacritty, hostterm.TerminalGhostty:
		return "drift"
	case hostterm.TerminalAppleTerminal:
		return "compose"
	case hostterm.TerminalSDL:
		// The graphical host shapes and positions marks natively; no terminal
		// mark-drift workaround applies, so emit the plain cluster.
		return "normal"
	default:
		return "normal"
	}
}

// resolveFlipBidiForHost turns the flipBidiForHost setting into the boolean
// pushed to the renderer at startup (or on a set_option). "true"/"false" pass
// through; "auto" turns the flip ON for a host known by sniffing to apply its
// own bidi reordering (see flipBidiForTerminal) and otherwise leaves it off for
// the runtime probe to decide once RTL content appears.
func resolveFlipBidiForHost(mode string) bool {
	flip, _, _, _ := flipSettings(mode)
	return flip
}

// flipSettings resolves the flipBidiForHost setting into the three renderer
// axes: whether to flip, the run segmentation (wordwise), and whether the host
// needs the ride-safe selection bar. Also reports whether sniffing recognised
// the host (known), so "auto" can decide between trusting the sniff and arming
// the probe. "true"/"false" are explicit; an explicit flip takes the
// conservative Terminal.app profile (whole-run + ride-safe).
func flipSettings(mode string) (flip, wordwise, rideSafe, known bool) {
	switch mode {
	case "true":
		return true, false, true, true
	case "false":
		return false, false, false, true
	case "auto":
		p := hostBidiProfileFor(hostterm.Detect())
		return p.flip, p.wordwise, p.rideSafe, p.known
	}
	return false, false, false, false
}

// adjustFlipForKitty overrides the flip axes when the host is Kitty and the
// mode is "auto": Kitty applies its own bidi only while force_ltr is off, so if
// the user's kitty.conf sets force_ltr yes, mew must own the bidi and NOT flip.
// Nothing over the wire reveals force_ltr (see kittyconf.go), so this is the
// one place the sniff feeds the renderer. Returns the (possibly adjusted) axes
// and whether the flip is on afterwards (the imperfect path that the force_ltr
// nudge watches for).
func adjustFlipForKitty(mode string, flip, wordwise, rideSafe bool) (fl, ww, rs, flipOnKitty bool) {
	if hostterm.Detect() != hostterm.TerminalKitty {
		return flip, wordwise, rideSafe, false
	}
	if mode == "auto" {
		if ltr, ok := kittyForceLTR(); ok && ltr {
			flip, wordwise, rideSafe = false, false, false
		}
	}
	return flip, wordwise, rideSafe, flip
}

// hostBidiProfile describes how a sniffed host handles RTL, so mew's emission
// and selection match it.
type hostBidiProfile struct {
	flip     bool // emit RTL runs in logical order for the host's own bidi
	wordwise bool // per-word run segmentation (Kitty) vs whole-run (Terminal.app)
	rideSafe bool // host's bidi miscounts a background selection fill -> use fg+bold
	known    bool // sniffing recognised the host (skip the DSR probe)
}

// hostBidiProfileFor classifies a sniffed host. A host it does not recognise
// (known=false) falls to the runtime DSR probe.
//
//   - Apple Terminal applies its own bidi, keeps inter-word spaces inside the
//     RTL run (whole-run), AND miscounts the selection fill (ride-safe).
//   - Kitty reorders too but reverses each whitespace-separated word in place
//     (word-wise); its selection fill is assumed to track the glyphs, so it
//     keeps the real bar (ride-safe off) — pending confirmation.
//   - iTerm2, Alacritty and Ghostty are stream-order: they render mew's visual
//     output verbatim, so nothing flips.
func hostBidiProfileFor(k hostterm.Kind) hostBidiProfile {
	switch k {
	case hostterm.TerminalAppleTerminal:
		return hostBidiProfile{flip: true, wordwise: false, rideSafe: true, known: true}
	case hostterm.TerminalKitty:
		return hostBidiProfile{flip: true, wordwise: true, rideSafe: false, known: true}
	case hostterm.TerminalITerm2, hostterm.TerminalAlacritty, hostterm.TerminalGhostty:
		return hostBidiProfile{known: true}
	case hostterm.TerminalSDL:
		// Native graphical rendering: mew emits visual order and the host draws
		// it directly (its own shaper does the joining), so nothing flips.
		return hostBidiProfile{known: true}
	}
	return hostBidiProfile{}
}

// setOption sets a named editor option. Per-viewport options (tabSize,
// showLineNumbers, showInvisibles, showColumnRuler) are written to the given
// viewport's own ViewState, so the change applies to that viewport and does not
// leak into the editor defaults or future viewports. Global options are written to the runtime
// Config. Nothing is written to config.GeneralConfig / the on-disk config.
func (e *Editor) setOption(w *viewport.Viewport, name, value string) bool {
	parseInt := func(minVal int) (int, bool) {
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < minVal {
			e.ShowWarning(name + " must be an integer >= " + strconv.Itoa(minVal))
			return 0, false
		}
		return n, true
	}
	parseBool := func() (bool, bool) {
		b, ok := parseBoolOption(value)
		if !ok {
			e.ShowWarning(name + " must be true or false")
		}
		return b, ok
	}

	lname := strings.ToLower(strings.TrimSpace(name))
	// Setting a per-viewport option on a specific viewport is a deliberate choice:
	// pin it so the grammar options overlay does not overwrite it later.
	if w != nil && cliPerViewportOptions[lname] {
		w.MarkOptionOverridden(lname)
	}

	switch lname {
	// Per-viewport options: write the viewport's ViewState (fall back to the
	// editor default only when there is no viewport).
	case "tabsize":
		n, ok := parseInt(1)
		if !ok {
			return false
		}
		if w != nil {
			w.ViewState.TabSize = n
		} else {
			e.Config.TabSize = n
		}
	case "showlinenumbers":
		b, ok := parseBool()
		if !ok {
			return false
		}
		if w != nil {
			w.ViewState.ShowLineNumbers = b
		} else {
			e.Config.ShowLineNumbers = b
		}
	case "deletenewlineaschar":
		b, ok := parseBool()
		if !ok {
			return false
		}
		if w != nil {
			w.ViewState.ProtectNewlines = !b // stored inverted
		} else {
			e.Config.DeleteNewlineAsChar = b
		}
	case "showinvisibles":
		b, ok := parseBool()
		if !ok {
			return false
		}
		if w != nil {
			w.ViewState.ShowInvisibles = b
		} else {
			e.Config.ShowInvisibles = b
		}
	case "showbidi":
		b, ok := parseBool()
		if !ok {
			return false
		}
		if w != nil {
			w.ViewState.ShowBidi = b
		} else {
			e.Config.ShowBidi = b
		}
	case "rtlcombining":
		b, ok := parseBool()
		if !ok {
			return false
		}
		if w != nil {
			w.ViewState.SuppressRTLCombining = !b // stored inverted
		} else {
			e.Config.RtlCombining = b
		}
	case "showmarks":
		v, ok := config.ParseShowMarks(value)
		if !ok {
			e.ShowWarning("showMarks must be no, yes, or all")
			return false
		}
		if w != nil {
			w.ViewState.ShowMarks = v
		} else {
			e.Config.ShowMarks = v
		}
	case "insertmode":
		b, ok := parseBool()
		if !ok {
			return false
		}
		// Stored inverted: insertMode on -> not overwrite.
		if w != nil {
			w.ViewState.OverwriteMode = !b
		} else {
			e.Config.OverwriteMode = !b
		}
	case "readonly":
		b, ok := parseBool()
		if !ok {
			return false
		}
		if w != nil {
			w.ViewState.ReadOnly = b
			if !b {
				// Read-only turned off: the intent-to-edit boundary. A lock
				// deferred by a read-only open is acquired now (with any
				// foreign-lock warning surfacing here rather than at open).
				e.ensureDeferredMewLock(w.Buffer)
			}
		} else {
			e.Config.ReadOnly = b
			if !b {
				e.ensureAllDeferredMewLocks()
			}
		}
	case "insertcursor", "overwritecursor", "navigationcursor":
		n, ok := parseInt(0)
		if !ok {
			return false
		}
		if n > 6 {
			e.ShowWarning(name + " must be a DECSCUSR shape 0-6")
			return false
		}
		switch lname {
		case "insertcursor":
			e.Config.InsertCursor = n
		case "overwritecursor":
			e.Config.OverwriteCursor = n
		default:
			e.Config.NavigationCursor = n
		}
		e.RequestRender()
	case "autoindent":
		b, ok := parseBool()
		if !ok {
			return false
		}
		if w != nil {
			w.ViewState.AutoIndent = b
		} else {
			e.Config.AutoIndent = b
		}
	case "linkbrowsing":
		b, ok := parseBool()
		if !ok {
			return false
		}
		if w != nil {
			w.ViewState.LinkBrowsing = b
			if !b {
				w.BrowseActive = false // turning the layer off retires the buttons
			}
		} else {
			e.Config.LinkBrowsing = b
		}
		e.RequestRender()
	case "navigationmode":
		// Enter/leave link browse (navigation) mode for the viewport — the
		// explicit replacement for the old auto-arm. Per-viewport only; a
		// nil target (global) has no navigation state to toggle. Rendering
		// still gates buttons on the link layer being on and the page being
		// linkable, so toggling this on a non-wiki viewport is a harmless no-op.
		b, ok := parseBool()
		if !ok {
			return false
		}
		if w != nil {
			// Entering nav mode needs the link layer on (buttons render only
			// then); with linkBrowsing off, ^O N is a no-op. Leaving always
			// disarms.
			w.BrowseActive = b && w.ViewState.LinkBrowsing
			w.NavIdealSet = false // drop any vertical-nav ideal on a mode flip
			e.RequestRender()
		}
	case "showcolumnruler":
		b, ok := parseBool()
		if !ok {
			return false
		}
		if w != nil {
			w.ViewState.ShowRuler = b
		} else {
			e.Config.ShowColumnRuler = b
		}
	case "scrollbar":
		b, ok := parseBool()
		if !ok {
			return false
		}
		if w != nil {
			w.ViewState.Scrollbar = b
		} else {
			e.Config.Scrollbar = b
		}
	case "rulershowscursor":
		b, ok := parseBool()
		if !ok {
			return false
		}
		e.Config.RulerShowsCursor = b
	case "syntax":
		return e.setSyntax(strings.TrimSpace(value))
	case "syntaxdetect":
		b, ok := parseBool()
		if !ok {
			return false
		}
		e.Config.SyntaxDetect = b
		e.resetSyntaxCaches()
	case "syntaxoverrides":
		v := strings.TrimSpace(value)
		if w != nil {
			w.ViewState.SyntaxOverrides = v
		} else {
			e.Config.SyntaxOverrides = v
		}
		// Grammar file resolution changes, so drop cached grammars and reload
		// the global grammar under the new override set.
		e.resetSyntaxCaches()
		e.reloadGlobalGrammar()
	case "macoptionkeys":
		v := strings.ToLower(strings.TrimSpace(value))
		if v != "auto" && v != "true" && v != "false" {
			e.ShowWarning("macOptionKeys: auto, true, or false")
			return false
		}
		e.Config.MacOptionKeys = v
		e.invalidateFocusedOptions()
		e.applyMacOptionKeys()
	case "matchignoressinglequote", "matchignoresdoublequote", "matchignoresslashstar",
		"matchignoresslashslash", "matchignoreshash", "matchignoresdoublehyphen",
		"matchignoressemicolon", "matchignorespercent":
		b, ok := parseBool()
		if !ok {
			return false
		}
		*e.matchIgnoreFlag(name) = b
	// Global options: write the runtime editor Config.
	case "wordwrap":
		b, ok := parseBool()
		if !ok {
			return false
		}
		e.Config.WordWrap = b
	case "searchignorecase":
		b, ok := parseBool()
		if !ok {
			return false
		}
		e.Config.SearchIgnoreCase = b
	case "searchwrap":
		b, ok := parseBool()
		if !ok {
			return false
		}
		e.Config.SearchWrap = b
	case "searchregex":
		b, ok := parseBool()
		if !ok {
			return false
		}
		e.Config.SearchRegex = b
	case "searchbackwards":
		b, ok := parseBool()
		if !ok {
			return false
		}
		e.Config.SearchBackwards = b
	case "searchallbuffers":
		b, ok := parseBool()
		if !ok {
			return false
		}
		e.Config.SearchAllBuffers = b
	case "modebarlocation":
		loc := strings.ToLower(strings.TrimSpace(value))
		if loc != "top" && loc != "bottom" {
			e.ShowWarning(name + " must be top or bottom")
			return false
		}
		e.Config.ModebarLocation = loc
		e.Modebar.SetLocation(e.optStr(e.ViewportManager.GetFocusedViewport(), "modebarlocation", loc))
		e.invalidateFocusedOptions()
		e.RequestRender()
	case "modebarinner":
		e.Config.ModebarInner = value
		e.invalidateFocusedOptions()
		e.RequestRender()
	case "modebardefault":
		e.Config.ModebarDefault = value
		e.invalidateFocusedOptions()
		e.RequestRender()
	case "modebarouter":
		e.Config.ModebarOuter = value
		e.invalidateFocusedOptions()
		e.RequestRender()
	case "mappings":
		e.Config.MappingsName = strings.TrimSpace(value)
		e.appliedMappingSet = "" // force the key processor to reload
		e.invalidateFocusedOptions()
		e.RequestRender()
	case "pagesizeoptimal":
		if _, _, _, ok := parseCountOrPercent(value); !ok {
			e.ShowWarning("pageSizeOptimal must be a count (24) or a percentage (50%)")
			return false
		}
		e.Config.PageSizeOptimal = strings.TrimSpace(value)
		e.rebuildPageSizeSpec()
	case "pageoverlapminimum":
		if _, _, _, ok := parseCountOrPercent(value); !ok {
			e.ShowWarning("pageOverlapMinimum must be a count (2) or a percentage (10%)")
			return false
		}
		e.Config.PageOverlapMinimum = strings.TrimSpace(value)
		e.rebuildPageSizeSpec()
	case "pagesizestep":
		n, ok := parseInt(0)
		if !ok {
			return false
		}
		e.Config.PageSizeStep = n
		e.rebuildPageSizeSpec()
	case "maxrepeat":
		n, ok := parseInt(1)
		if !ok {
			return false
		}
		e.Config.MaxRepeat = n
	case "killringentries":
		n, ok := parseInt(1)
		if !ok {
			return false
		}
		e.Config.KillRingEntries = n
		e.trimKillRing()
	case "direction":
		dir := strings.ToLower(strings.TrimSpace(value))
		if dir != "ltr" && dir != "rtl" {
			e.ShowWarning("direction must be ltr or rtl")
			return false
		}
		if w != nil {
			// Per-viewport base direction (rendering reads ViewState.Direction
			// first — see winRTL). The global renderer base is untouched.
			w.ViewState.Direction = dir
		} else {
			e.Config.Direction = dir
			e.Renderer.SetBaseRTL(dir == "rtl")
			e.ColumnRuler.SetRTL(dir == "rtl")
		}
		e.RequestRender()
	case "flipbidiforhost":
		v := strings.ToLower(strings.TrimSpace(value))
		if v != "auto" && v != "true" && v != "false" {
			e.ShowWarning("flipBidiForHost: auto, true, or false")
			return false
		}
		e.Config.FlipBidiForHost = v
		flip, wordwise, rideSafe, known := flipSettings(v)
		flip, wordwise, rideSafe, e.kittyFlipActive = adjustFlipForKitty(v, flip, wordwise, rideSafe)
		e.Renderer.SetFlipBidiForHost(flip)
		e.Renderer.SetFlipWordwise(wordwise)
		e.Renderer.SetFlipRideSafeSelection(rideSafe)
		if v == "auto" && !known {
			e.bidiProbeState = bidiProbeIdle // re-arm detection for an unknown host
		} else {
			e.bidiProbeState = bidiProbeDone // explicit choice or a recognised host
		}
		e.RequestRender()
	case "rtlmarkmode":
		v := strings.ToLower(strings.TrimSpace(value))
		if v != "normal" && v != "iterm2" && v != "compose" && v != "drift" && v != "auto" {
			e.ShowWarning("rtlMarkMode: auto, normal, iterm2, compose, or drift")
			return false
		}
		e.Config.RtlMarkMode = v
		resolved := resolveRtlMarkMode(v)
		e.Renderer.SetRtlMarkMode(resolved)
		if e.Config.RtlMarkModeSink != nil {
			e.Config.RtlMarkModeSink(resolved)
		}
		e.RequestRender()
	case "prompttimeout":
		n, ok := parseInt(0)
		if !ok {
			return false
		}
		e.Config.PromptTimeout = n
	case "scripttimeout":
		n, ok := parseInt(0)
		if !ok {
			return false
		}
		e.Config.ScriptTimeout = n
		if e.pawConfig != nil {
			e.pawConfig.DefaultTokenTimeout = tokenTimeout(n)
		}
	default:
		e.ShowWarning("Unknown option: " + name)
		return false
	}

	e.ShowNotificationTagged("Option '"+name+"' set to "+value, "optionset")
	e.RequestRender()
	return true
}

// executeCommand executes a command string via PawScript.
// Errors are captured via the custom stderr writer and shown as transient
// error viewports.
//
// Undo grouping is NOT a blanket per-command transaction. Plain typing and
// character deletes run as bare mutations that garland coalesces into one
// revision (a typing streak = one undo step); compound commands open their own
// transaction in their handler (and scripts can with buffer_tx_*). After each
// dispatch this closes any transaction a script left open and, unless the
// command was one of the coalescible edits, bakes the undo run — so a cursor
// move or any other command ends the streak and the next edit starts fresh.
func (e *Editor) executeCommand(command string) {
	e.executeCommandResult(command)
}

// runBoundCommand is the key processor's executor: it runs one bound command
// on behalf of a key event and reports whether that key was HANDLED. Only a
// clean false status says "not mine" — a binding's way of declining the key
// (tinput_key with no session under it, an explicit `capture X = false`
// reclaim) — which drops resolution to the next capture level down. Async
// suspensions, errors, and every other result hold the key: a command that
// merely went wrong must not hand its keystroke to a terminal below it.
//
// dispatchingKey is kept as a save/restore stack because a sequence unwind
// runs several keys' commands within one dispatch, each needing its own
// notion of "the key" for tinput_key to encode.
func (e *Editor) runBoundCommand(key, command string) bool {
	prev := e.dispatchingKey
	e.dispatchingKey = key
	defer func() { e.dispatchingKey = prev }()
	res := e.executeCommandResult(command)
	if b, ok := res.(pawscript.BoolStatus); ok && !bool(b) {
		return false
	}
	return true
}

// dispatchKey routes one keyboard key through the sequence processor, which
// resolves and RUNS its bindings (see keys.SequenceProcessor: capture levels
// over the base set, wildcards under specifics, tried in precedence order
// until one takes the key), then refreshes the sequence/QuickHelp/modebar
// displays. Runs under renderMu (the serve loop's key branch, or a test
// standing in for it).
func (e *Editor) dispatchKey(key string) {
	// Raw key input: this one keystroke was claimed for the child process
	// running in the focused viewport, so mew's keymap does not see it at all.
	// The arm is spent either way — a raw key with no terminal under it is
	// simply an ordinary key, not a key held in reserve.
	if e.rawKeyArmed {
		e.rawKeyArmed = false
		if e.rawKeyToPTY(key) {
			e.RequestRender()
			return
		}
	}

	// Arm the after-key pseudo-binding on the viewport that owns this key —
	// the first command that completes during this dispatch fires it (see
	// executeCommandResult): the bound command when a binding matches, or the
	// first key of an unwinding non-matching sequence landing as a normal
	// press. A key that resolves nothing (a silent prefix step, an unmapped
	// named key) leaves it to be disarmed below, unfired.
	if w := e.ViewportManager.GetFocusedViewport(); w != nil && w.AfterKey != "" {
		e.afterKeyArmed = w
	}

	// The processor owns execution: a key can resolve to a stack of bindings
	// across capture levels, run highest-first until one takes it, and a
	// mid-sequence unwind runs several keys' worth — so there is no longer
	// exactly one command per keystroke to hand back. Each one comes through
	// runBoundCommand, so command semantics (afterKey, undo baking,
	// repeat_next) are unchanged.
	e.KeyProcessor.ProcessKey(key)
	e.afterKeyArmed = nil

	// Update active sequence display with possible completions
	e.ActiveSequence = e.KeyProcessor.GetActiveSequence()
	if e.ActiveSequence != "" {
		// Show possible completions for the current sequence
		completions := e.KeyProcessor.GetPossibleCompletions()
		if len(completions) > 0 {
			e.activeCompletions = strings.Join(completions, " ")
		} else {
			e.activeCompletions = ""
		}
	} else {
		e.activeCompletions = ""
	}

	// Context-sensitive Quick Help: recompute the topic for the current
	// key prefix and, if Quick Help is open and following, re-render it
	// in place. This ONLY touches Quick Help — the main help is never
	// steered by the topic. Skipped entirely when nothing consults it.
	e.updateQuickHelp()

	e.updateModebar()
}

// executeCommandResult is executeCommand returning the PawScript result, for
// the one caller that must know whether the command suspended on an async
// token (the launch-eval runner, which waits for the token before starting
// the next script).
func (e *Editor) executeCommandResult(command string) pawscript.Result {
	if command == "" {
		return nil
	}

	// Before running any command, expire transient notification/error/warning
	// viewports that have been on screen longer than 5 seconds.
	e.expireStaleNotifications()

	// Content mutations (read-only viewports, focused link buttons) are NOT gated
	// here by command name: the script language can rename or chain any command,
	// so a name is not a reliable signal of what a command does. The lock is
	// enforced inside each mutation's implementation instead — the one place
	// that always sees the actual buffer change (see contentLocked).
	fw := e.ViewportManager.GetFocusedViewport()

	// Reset the per-command behavior signals; the mutation implementations set
	// them as they run, and the post-dispatch logic below reads them instead of
	// re-classifying the command by name.
	e.editCoalesced = false
	e.yankedThisCommand = false

	// If a repeat_next is armed, wrap this command so it runs N times.
	command = e.applyRepeatNext(command)

	var buf *buffer.Buffer
	if fw != nil {
		buf = fw.Buffer
	}

	// ExecuteAsync, not Execute: a command that opens a prompt suspends its
	// command sequence on an async token, resumed by the prompt callback on
	// this same goroutine. The blocking Execute would deadlock the main loop
	// waiting for input that can never arrive. A returned TokenResult simply
	// means "still running"; the prompt callback finishes the sequence later.
	res := e.PawScript.ExecuteAsync(command)

	// Undo grouping is no longer a blanket per-command transaction. Plain typing
	// and character deletes flow as bare edits that garland coalesces into a
	// single revision; compound commands (and scripts, via buffer_tx_*) open
	// their own transaction in their handler. Close any transaction a script
	// left dangling, then — unless this command performed a coalescible edit
	// (the edit implementation set editCoalesced) — bake the run so the next
	// edit starts a fresh undo step. That bake is what makes a cursor move (or
	// any other command) end the current typing/deleting run.
	if buf != nil {
		buf.CloseUserCommand()
		if !e.editCoalesced {
			buf.BakeUndo()
		}
	}

	// kill_ring_pop may only replace text yanked by the immediately preceding
	// command: any command that was not itself a yank/pop invalidates the
	// recorded yank (the yank/pop implementations set yankedThisCommand).
	if !e.yankedThisCommand {
		e.lastYank.valid = false
	}
	e.RequestRender()

	// After-key pseudo-binding: this command's completion resolves the key
	// event that armed it (see dispatchKey). Disarm BEFORE running the script
	// so the script — an ordinary command, not a key — cannot re-trigger, and
	// later commands of the same dispatch (the rest of a sequence unwind)
	// don't fire it again. Skipped if the binding closed the owning viewport.
	if w := e.afterKeyArmed; w != nil {
		e.afterKeyArmed = nil
		if w.AfterKey != "" && e.ViewportManager.GetViewport(w.ID) == w {
			e.executeCommand(w.AfterKey)
		}
	}
	return res
}

// applyRepeatNext consumes a pending repeat_next arm on the target viewport,
// wrapping command in a PawScript repeat(...) so it runs Count times.
//
// A repeat_next invocation must not wrap itself (pressing it while already
// armed would otherwise repeat the arming command). This is the one decision
// that cannot be moved into the implementation: the wrap happens BEFORE the
// command runs, so it cannot observe what the command did — it can only look at
// the command about to run. It therefore still inspects the leading token, and
// so is the lone place a renamed/aliased command name would slip past. Left as
// a known limitation rather than papered over; a proper fix needs repeat-arm
// metadata carried on the command registration.
func (e *Editor) applyRepeatNext(command string) string {
	if commandKind(command) == "repeat_next" {
		return command
	}
	w := e.resolveTargetMain()
	if w == nil || !w.Repeat.Pending {
		return command
	}
	n := w.Repeat.Count
	w.Repeat = viewport.RepeatState{} // one-shot: consume the arm
	if n < 1 {
		return command
	}
	return fmt.Sprintf("repeat (%s), %d", command, n)
}

// commandKind returns the leading token of a command string. Its only remaining
// use is applyRepeatNext's pre-dispatch self-check (see there); behavioral
// decisions that CAN see what a command did read per-command signals instead.
func commandKind(command string) string {
	command = strings.TrimSpace(command)
	if i := strings.IndexAny(command, " \t"); i >= 0 {
		return command[:i]
	}
	return command
}

// contentLocked reports whether the focused viewport currently forbids content
// mutations — it is read-only, or a link button is focused (the caret is inert
// inside it, its source text protected until nav_cancel) — and warns when it
// does. Every mutation's implementation calls this at the point it would change
// the buffer, so the lock holds no matter how the command was named, aliased,
// or chained in the script language. There is deliberately no command-name
// list: a name cannot tell you what a command does.
func (e *Editor) contentLocked() bool {
	return e.viewportEditLocked(e.ViewportManager.GetFocusedViewport())
}

// viewportEditLocked is contentLocked for a specific viewport — used where the
// mutating implementation names its target viewport directly (find/replace
// applies to the match's viewport, not necessarily the focused one).
func (e *Editor) viewportEditLocked(w *viewport.Viewport) bool {
	if w == nil {
		return false
	}
	// A generated surface (mew:/…) is read-only by its address, not by viewport
	// state — a hard guarantee independent of when options were last resolved.
	// viewportReadOnly is the SILENT predicate; keeping this branch on it means
	// the delete commands' silent read-only decline can never drift from what
	// warns here.
	if e.viewportReadOnly(w) {
		// Tagged so a burst of rejected edits (holding a key) collapses to one
		// warning instead of stacking a bar per keystroke.
		e.ShowWarningTagged("Buffer is read-only", "readonly_warning")
		e.RequestRender()
		return true
	}
	if e.focusedLinkButton(w) != nil {
		e.ShowWarning("Link is focused (^C to edit)")
		e.RequestRender()
		return true
	}
	return false
}

// matchIgnoreFlag maps a matchIgnores* option name to its field.
func (e *Editor) matchIgnoreFlag(name string) *bool {
	switch strings.ToLower(name) {
	case "matchignoressinglequote":
		return &e.Config.MatchIgnoresSingleQuote
	case "matchignoresdoublequote":
		return &e.Config.MatchIgnoresDoubleQuote
	case "matchignoresslashstar":
		return &e.Config.MatchIgnoresSlashStar
	case "matchignoresslashslash":
		return &e.Config.MatchIgnoresSlashSlash
	case "matchignoreshash":
		return &e.Config.MatchIgnoresHash
	case "matchignoresdoublehyphen":
		return &e.Config.MatchIgnoresDoubleHyphen
	case "matchignoressemicolon":
		return &e.Config.MatchIgnoresSemicolon
	case "matchignorespercent":
		return &e.Config.MatchIgnoresPercent
	}
	panic("unknown match flag " + name)
}

// moveCursor moves the cursor by delta amounts.
func (e *Editor) moveCursor(dx, dy int) {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	if dy != 0 {
		// Establish the ideal column from the source line before moving.
		e.ensureIdealColumn(w)

		newLine := w.CursorPos().Line + dy
		if newLine < 0 {
			newLine = 0
		}
		maxLine := w.Buffer.GetLineCount() - 1
		if newLine > maxLine {
			newLine = maxLine
		}
		w.SetCursorLine(newLine)

		// Apply ghost cursor logic after vertical movement
		e.afterVerticalMovement(w)
	}

	if dx != 0 {
		newRune := w.CursorPos().Rune + dx
		lineLen := e.getEffectiveLineLen(w.Buffer, w.CursorPos().Line)

		if newRune < 0 {
			// Move to end of previous line
			if w.CursorPos().Line > 0 {
				w.SetCursorLine(w.CursorPos().Line - 1)
				w.SetCursorRune(e.getEffectiveLineLen(w.Buffer, w.CursorPos().Line))
			} else {
				newRune = 0
			}
		} else if newRune > lineLen {
			// Move to start of next line
			if w.CursorPos().Line < w.Buffer.GetLineCount()-1 {
				w.SetCursorLine(w.CursorPos().Line + 1)
				w.SetCursorRune(0)
			}
		} else {
			w.SetCursorRune(newRune)
		}

		// Update ideal column after horizontal movement
		e.afterHorizontalMovement(w)
	}

	// A horizontal move locks in the column and follows it; a bare vertical move
	// only follows vertically, leaving the horizontal view (and the ghost column)
	// where it is until a horizontal/locking action.
	if dx != 0 {
		e.ensureCursorVisible(w)
	} else {
		e.ensureCursorVisibleVertical(w)
	}
}

// tabSize returns the effective tab size for a viewport. Per-viewport settings
// govern the viewport, so cursor math must use this — the viewport's own
// ViewState.TabSize — rather than the global e.Config.TabSize default.
func (e *Editor) tabSize(w *viewport.Viewport) int {
	if w != nil && w.ViewState.TabSize > 0 {
		return w.ViewState.TabSize
	}
	if e.Config.TabSize > 0 {
		return e.Config.TabSize
	}
	return 4
}

// ensureIdealColumn establishes the ideal visual column from the cursor's
// current position if it has not been set yet. It must be called BEFORE the
// cursor's Line is changed for a vertical move, so the ideal is computed from
// the source line (computing it after the move would pair the destination
// line's content with the source rune index and mis-handle tabs).
func (e *Editor) ensureIdealColumn(w *viewport.Viewport) {
	if w == nil || w.Buffer == nil {
		return
	}
	if w.IdealVisualColumn != 0 || w.CursorPos().Rune == 0 {
		return
	}
	tabSize := e.tabSize(w)
	line := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
	w.IdealVisualColumn = e.storedIdealColumn(w, line, w.CursorPos().Rune, tabSize)
}

// idealDenominator is how many NORMAL screen columns one column of a
// document line's own space covers: 2 on a double-width (DECDWL heading)
// line, 1 everywhere else.
func (e *Editor) idealDenominator(w *viewport.Viewport, docLine int) int {
	if w == nil || w.Buffer == nil || docLine < 0 || docLine >= w.Buffer.GetLineCount() {
		return 1
	}
	if _, dw := e.lineDisplaySpans(w, docLine); dw {
		return 2
	}
	return 1
}

// storedIdealColumn is the sticky column to REMEMBER for the caret at runePos
// on line: the caret's column in the line as DISPLAYED, converted to normal
// screen columns.
//
// Display space, because that is where the caret is painted — a browse-mode
// heading's "======" markers and a link's target text take no columns of their
// own, so measuring the raw line would remember a column the caret never sat
// at. Normal screen columns, because a double-width line's space is half as
// dense (see idealDenominator); applyIdealColumn undoes both.
func (e *Editor) storedIdealColumn(w *viewport.Viewport, line string, runePos, tabSize int) int {
	dispLine, dispRune := e.displayCaretLine(w, line, runePos)
	return e.idealColumn(w, dispLine, dispRune, tabSize) *
		e.idealDenominator(w, w.CursorPos().Line)
}

// applyIdealColumn converts the stored ideal into the caret line's own display
// column space — the space every column computation in afterVerticalMovement
// works in, and the space the renderer paints the ghost cursor in.
func (e *Editor) applyIdealColumn(w *viewport.Viewport) int {
	return w.IdealVisualColumn / e.idealDenominator(w, w.CursorPos().Line)
}

// afterVerticalMovement applies ghost cursor logic after up/down movement.
// It tries to position cursor at the ideal visual column, showing a ghost
// cursor if the line is shorter than the ideal. Callers must establish the
// ideal column (via ensureIdealColumn) before changing the cursor's line.
func (e *Editor) afterVerticalMovement(w *viewport.Viewport) {
	if w == nil || w.Buffer == nil {
		return
	}

	// Normal vertical movement is not link nav: drop any vertical nav ideal
	// (navVert saves/restores it around its own paging fallback).
	w.NavIdealSet = false

	tabSize := e.tabSize(w)

	// Get line content (without trailing newline), as DISPLAYED: every column
	// below — the reach checks, the landing position, and the ghost's own
	// column, which the renderer paints in this same space — is a display
	// column. place() maps a landing back to the document rune it came from.
	raw := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
	line, _ := e.displayCaretLine(w, raw, w.CursorPos().Rune)
	lineLen := len([]rune(line))
	place := func(dispRune int) {
		w.SetCursorRune(e.displayCaretDoc(w, raw, dispRune))
	}

	// The stored ideal is in NORMAL screen columns; this line's own column
	// space is half as dense when it paints double-width, so divide in.
	ideal := e.applyIdealColumn(w)

	// direction=rtl: the view is right-anchored, so the ideal is a READING
	// column (distance back from the reading start). Placement and the ghost
	// mirror the LTR logic below, but in reading space so screen X is held.
	if e.winRTL(w) {
		idealReading := ideal
		vw := e.lineVisualWidth(w, line, tabSize)
		if lineLen == 0 {
			// Empty line: the caret can only sit at the reading start; a
			// non-trivial ideal reads as "past the end", so ghost it.
			place(0)
			w.HasGhostCursor = idealReading > 0
			if w.HasGhostCursor {
				w.GhostCursorVisualColumn = idealReading
			} else {
				w.GhostCursorVisualColumn = 0
			}
			return
		}
		// The furthest-back reachable reading column belongs to the line's
		// LEFTMOST caret position. That is end-of-line only when the line
		// ENDS in RTL text; a right-anchored line ending in an LTR fragment
		// has its EOL caret at the reading-START side (reading 0), and its
		// furthest-back caret inside the line — measuring EOL here read every
		// such line as "too short" and ghosted columns it actually reaches.
		farCol, farPos := e.leftmostCaret(w, line, tabSize)
		if idealReading > vw-farCol {
			// Line does not reach that far back — ghost past its reading
			// end, with the real caret parked at the reachable position
			// nearest the ghost.
			place(farPos)
			w.HasGhostCursor = true
			w.GhostCursorVisualColumn = idealReading
			return
		}
		// Map the reading column to a left-based visual column and land on the
		// covering rune. A mismatch (short line / inside a wide cell) ghosts.
		result := e.visualColumnToRuneWithActual(w, line, vw-idealReading, tabSize)
		place(result.Rune)
		if vw-result.ActualColumn != idealReading {
			w.HasGhostCursor = true
			w.GhostCursorVisualColumn = idealReading
		} else {
			w.HasGhostCursor = false
			w.GhostCursorVisualColumn = 0
		}
		return
	}

	// Calculate the maximum visual column for this line (end of line position)
	maxVisualColumn := e.runeToVisualColumn(w, line, lineLen, tabSize)

	if maxVisualColumn < ideal {
		// Line is shorter than ideal visual column - show ghost cursor at end
		place(lineLen)
		w.HasGhostCursor = true
		w.GhostCursorVisualColumn = ideal
	} else {
		// Line is long enough - position at the rune that corresponds to ideal visual column
		result := e.visualColumnToRuneWithActual(w, line, ideal, tabSize)
		place(result.Rune)

		// Check if we landed inside a wide character (like a tab)
		// If the actual column differs from ideal, we're inside a tab stop
		if result.ActualColumn != ideal {
			w.HasGhostCursor = true
			w.GhostCursorVisualColumn = ideal
		} else {
			w.HasGhostCursor = false
			w.GhostCursorVisualColumn = 0
		}
	}
}

// setUserMark sets a user-defined mark at the given position. It rejects empty
// names and the reserved "_" internal-mark namespace.
func (e *Editor) setUserMark(w *viewport.Viewport, name string, line, runePos int) bool {
	if w == nil || w.Buffer == nil || name == "" {
		return false
	}
	if strings.HasPrefix(name, "_") {
		e.ShowWarning("Mark names starting with '_' are reserved")
		return false
	}
	if err := w.Buffer.SetMark(name, line, runePos); err != nil {
		e.ShowError("Failed to set mark: " + err.Error())
		return false
	}
	e.ShowNotification("Mark '" + name + "' set")
	return true
}

// gotoUserMark moves the caret to a named user mark, warning if it is unset.
// Shared by go_mark's direct-argument and prompted paths.
func (e *Editor) gotoUserMark(w *viewport.Viewport, name string) bool {
	if w == nil || w.Buffer == nil {
		return false
	}
	line, runePos, exists := w.Buffer.GetMark(name)
	if !exists {
		e.ShowWarning("Mark '" + name + "' not set")
		return false
	}
	w.SetCursorPos(viewport.Position{Line: line, Rune: runePos})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	w.TrackMove()
	return true
}

// setBlockMark sets an internal block-selection mark at the cursor position.
func (e *Editor) setBlockMark(markName, label string) bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}
	if err := w.Buffer.SetMark(markName, w.CursorPos().Line, w.CursorPos().Rune); err != nil {
		e.ShowError("Failed to set mark: " + err.Error())
		return false
	}
	// A keyboard-placed block mark makes the block deliberate: it survives
	// plain mouse clicks (unlike a transient mouse-drag selection).
	w.Buffer.SetMouseBlock(false)
	e.ShowNotification(label + " set")
	return true
}

// goBlockMark moves the cursor to an internal block-selection mark.
func (e *Editor) goBlockMark(markName, label string) bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}
	line, rune_, exists := w.Buffer.GetMark(markName)
	if !exists {
		e.ShowWarning(label + " not set")
		return false
	}
	w.SetCursorPos(viewport.Position{Line: line, Rune: rune_})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	return true
}

// syncCursorAfterUndoRedo moves the editor cursor to the post-undo position
// garland slid the viewport's caret to, clamps it, and refreshes derived state.
func (e *Editor) syncCursorAfterUndoRedo(w *viewport.Viewport) {
	if w == nil || w.Buffer == nil || w.Caret == nil {
		return
	}
	e.clampCursorToBuffer(w)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	e.RequestRender()
}

// afterHorizontalMovement clears ghost cursor and updates ideal column.
func (e *Editor) afterHorizontalMovement(w *viewport.Viewport) {
	if w == nil {
		return
	}

	// Clear ghost cursor
	w.HasGhostCursor = false
	w.GhostCursorVisualColumn = 0
	w.NavIdealSet = false // a horizontal move re-anchors any vertical nav ideal

	// Update ideal column to current visual position (reading column in RTL).
	if w.Buffer != nil {
		tabSize := e.tabSize(w)
		line := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
		w.IdealVisualColumn = e.storedIdealColumn(w, line, w.CursorPos().Rune, tabSize)
	} else {
		w.IdealVisualColumn = w.CursorPos().Rune
	}
}

// cursorToLineStart moves cursor to start of line.
func (e *Editor) cursorToLineStart() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil {
		return
	}
	w.SetCursorRune(0)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// cursorToLineEnd moves cursor to end of line.
func (e *Editor) cursorToLineEnd() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}
	lineLen := e.getEffectiveLineLen(w.Buffer, w.CursorPos().Line)
	w.SetCursorRune(lineLen)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// cursorToBufferStart moves cursor to beginning of buffer (line 0, rune 0).
func (e *Editor) cursorToBufferStart() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 0})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// cursorToBufferEnd moves cursor to end of buffer (last line, last rune).
func (e *Editor) cursorToBufferEnd() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}
	lastLine := w.Buffer.GetLineCount() - 1
	if lastLine < 0 {
		lastLine = 0
	}
	w.SetCursorPos(viewport.Position{Line: lastLine, Rune: e.getEffectiveLineLen(w.Buffer, lastLine)})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// gotoLine moves cursor to a specific line number (1-based).
func (e *Editor) gotoLine(lineNum int) {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	// Convert from 1-based (user input) to 0-based (internal)
	targetLine := lineNum - 1

	// Clamp to valid range
	if targetLine < 0 {
		targetLine = 0
	}
	maxLine := w.Buffer.GetLineCount() - 1
	if targetLine > maxLine {
		targetLine = maxLine
	}

	w.SetCursorPos(viewport.Position{Line: targetLine, Rune: 0})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// scrollLineTop parks line lineNum (1-based) at the top of the target viewport's
// viewport without moving the caret — the scroll analogue of gotoLine.
func (e *Editor) scrollLineTop(lineNum int) {
	e.scrollViewTo(e.resolveTargetMain(), lineNum-1)
}

// cursorToTop moves cursor to start of buffer.
func (e *Editor) cursorToTop() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil {
		return
	}
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 0})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// cursorToBottom moves cursor to end of buffer.
func (e *Editor) cursorToBottom() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}
	w.SetCursorPos(viewport.Position{Line: w.Buffer.GetLineCount() - 1, Rune: e.getEffectiveLineLen(w.Buffer, w.CursorPos().Line)})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// pageUp moves up by a page.
func (e *Editor) pageUp() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil {
		return
	}
	_, pageSize := e.pageSize(w)
	e.ensureIdealColumn(w)
	w.SetCursorLine(w.CursorPos().Line - pageSize) // setter clamps to 0
	e.afterVerticalMovement(w)

	// Scroll the viewport up by the same page so the caret keeps its relative
	// screen row, clamped to the top. Never scroll down here.
	w.RefreshViewTop()
	top := w.ViewState.ViewOffsetY - pageSize
	if top < 0 {
		top = 0
	}
	if top < w.ViewState.ViewOffsetY {
		w.SetViewTop(top)
	}
	e.ensureCursorVisibleVertical(w) // guarantee the caret is visible
}

// pageDown moves down by a page.
func (e *Editor) pageDown() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}
	viewHeight, pageSize := e.pageSize(w)
	e.ensureIdealColumn(w)
	w.SetCursorLine(w.CursorPos().Line + pageSize) // setter clamps to last line
	e.afterVerticalMovement(w)

	// Scroll the viewport down by the same page so the caret keeps its relative
	// screen row. Two caps: don't scroll so far that more than one blank line
	// shows past the end of the buffer (making the end obvious), and never
	// rewind the viewport upward — if it is already further down, leave it. The
	// blank-line cap uses the VIEW height, not the page distance.
	w.RefreshViewTop()
	oldTop := w.ViewState.ViewOffsetY
	// maxTop puts the last line on the second-to-last row, leaving exactly one
	// blank row below it. Negative when the buffer is shorter than the view, in
	// which case there is nothing to scroll.
	maxTop := w.Buffer.GetLineCount() - viewHeight + 1
	if maxTop < 0 {
		maxTop = 0
	}
	top := oldTop + pageSize
	if top > maxTop {
		top = maxTop
	}
	if top < oldTop {
		top = oldTop // never rewind upward
	}
	w.SetViewTop(top)
	e.ensureCursorVisibleVertical(w) // guarantee the caret is visible
}

// pageSize returns the viewport's view height and the configured page distance
// (evaluated against that height). The height falls back to a default when it
// is not yet known.
func (e *Editor) pageSize(w *viewport.Viewport) (viewHeight, page int) {
	viewHeight = w.ContentHeight
	if viewHeight < 1 {
		viewHeight = 20
	}
	return viewHeight, e.pageSizeSpec.eval(viewHeight)
}

// rebuildPageSizeSpec re-derives the paging spec after any of the three page
// options changes.
func (e *Editor) rebuildPageSizeSpec() {
	e.pageSizeSpec = buildPageSizeSpec(e.Config.PageSizeOptimal, e.Config.PageOverlapMinimum, e.Config.PageSizeStep)
}

// trackEdit records a caret-area edit on the focused viewport's cursor ring, run
// after an editing command has completed. A no-op when there is no ring (e.g. a
// prompt viewport). See Viewport.TrackEdit. It also shifts the kill-chain flag:
// lastEditKill becomes true only when this edit was a kill capture, so a
// non-kill edit (typing, paste) between deletes breaks the accumulation.
func (e *Editor) trackEdit() {
	if w := e.ViewportManager.GetFocusedViewport(); w != nil {
		w.TrackEdit()
		// An edit re-engages caret following (edits also call ensureCursorVisible,
		// but not every path does; make the re-engage unconditional here).
		w.ViewState.ScrollDetached = false
	}
	e.lastEditKill = e.pendingKill
	e.pendingKill = false
	e.checkEditLock()
}

// trackMove records a deliberate caret movement on the focused viewport's cursor
// ring, run after a movement command has completed. See Viewport.TrackMove.
func (e *Editor) trackMove() {
	if w := e.ViewportManager.GetFocusedViewport(); w != nil {
		w.TrackMove()
	}
}

// cursorRingGo walks the caret one step through the cursor ring — forward
// (newer) when next is true, backward (older) otherwise — and brings it into
// view. Returns false, leaving the caret put, when there is nowhere further to
// go in that direction.
func (e *Editor) cursorRingGo(next bool) bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}
	var pos int64
	var ok bool
	if next {
		pos, ok = w.CursorRingNext()
	} else {
		pos, ok = w.CursorRingPrior()
	}
	if !ok {
		return false
	}
	w.SeekCaretByte(pos)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	return true
}

// deleteWouldRemoveNewline reports whether a delete at the caret would remove a
// line terminator (join two lines) rather than an in-line rune. forward is the
// del_char_next direction; !forward is del_char_prior (backspace). It reads only
// the caret POSITION and line lengths — never moving the editing caret — so it
// cannot disturb the undo coalescing run. In-line deletes never target a
// newline; only the line-boundary branches of deleteCharBefore/deleteCharAt do,
// which this mirrors exactly.
func (e *Editor) deleteWouldRemoveNewline(w *viewport.Viewport, forward bool) bool {
	if w == nil || w.Buffer == nil {
		return false
	}
	pos := w.CursorPos()
	if forward {
		lineLen := e.getEffectiveLineLen(w.Buffer, pos.Line)
		return pos.Rune >= lineLen && pos.Line < w.Buffer.GetLineCount()-1
	}
	return pos.Rune == 0 && pos.Line > 0
}

// deleteCharBefore deletes the character before the cursor.
func (e *Editor) deleteCharBefore() {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	if w.CursorPos().Rune > 0 {
		// Delete the rune before the caret through the viewport's caret cursor,
		// which moves back with the deletion. Backspace kills prepend.
		w.Caret.Seek(w.CursorPos().Line, w.CursorPos().Rune)
		e.killCapture(w, w.Caret.DeleteBackwardCaptured(1), false)
	} else if w.CursorPos().Line > 0 {
		// Join with the previous line by deleting the terminator that ends it.
		// Position the caret at the end of the previous line's content and
		// delete the terminator runes forward: garland joins the lines and
		// slides every decoration and cursor across the seam. Cursor-relative
		// (a fresh seek, not a captured byte offset).
		prevRaw := w.Buffer.GetLine(w.CursorPos().Line - 1)
		prevLen := len([]rune(strings.TrimRight(prevRaw, "\n\r")))
		termRunes := len([]rune(prevRaw)) - prevLen // 1 for "\n", 2 for "\r\n"
		w.Caret.Seek(w.CursorPos().Line-1, prevLen)
		e.killCapture(w, w.Caret.DeleteForwardCaptured(termRunes), false)
	}

	// Clear ghost cursor and update ideal column after editing
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w) // edit locks in the horizontal view
}

// deleteCharAt deletes the character at the cursor.
func (e *Editor) deleteCharAt() {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	lineLen := e.getEffectiveLineLen(w.Buffer, w.CursorPos().Line)
	if w.CursorPos().Rune < lineLen {
		// Delete the rune under the caret (forward delete keeps the caret put).
		// Forward kills append.
		w.Caret.Seek(w.CursorPos().Line, w.CursorPos().Rune)
		e.killCapture(w, w.Caret.DeleteForwardCaptured(1), true)
	} else if w.CursorPos().Line < w.Buffer.GetLineCount()-1 {
		// Join with the next line by deleting this line's terminator. The caret
		// is already at end-of-content (rune == lineLen); delete the terminator
		// runes forward so garland joins the lines and slides everything across.
		curRaw := w.Buffer.GetLine(w.CursorPos().Line)
		termRunes := len([]rune(curRaw)) - lineLen // 1 for "\n", 2 for "\r\n"
		w.Caret.Seek(w.CursorPos().Line, lineLen)
		e.killCapture(w, w.Caret.DeleteForwardCaptured(termRunes), true)
	}

	// Clear ghost cursor and update ideal column after editing
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w) // edit locks in the horizontal view
}

// deleteLine deletes the current line.
func (e *Editor) deleteLine() {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	e.killCapture(w, w.Buffer.DeleteLineCaptured(w.CursorPos().Line), true)
	if w.CursorPos().Line >= w.Buffer.GetLineCount() {
		w.SetCursorLine(w.Buffer.GetLineCount() - 1)
	}
	if w.CursorPos().Line < 0 {
		w.SetCursorLine(0)
	}
	w.SetCursorRune(0)

	// Clear ghost cursor and update ideal column after editing
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w) // edit locks in the horizontal view
}

// clampCursorToBuffer ensures the cursor position is within valid buffer bounds.
func (e *Editor) clampCursorToBuffer(w *viewport.Viewport) {
	if w == nil || w.Buffer == nil {
		return
	}

	lineCount := w.Buffer.GetLineCount()

	// Clamp line
	if w.CursorPos().Line < 0 {
		w.SetCursorLine(0)
	}
	if w.CursorPos().Line >= lineCount {
		w.SetCursorLine(lineCount - 1)
	}

	// Clamp rune within line (using effective length without trailing newline)
	lineLen := e.getEffectiveLineLen(w.Buffer, w.CursorPos().Line)
	if w.CursorPos().Rune < 0 {
		w.SetCursorRune(0)
	}
	if w.CursorPos().Rune > lineLen {
		w.SetCursorRune(lineLen)
	}
}

// getEffectiveLineLen returns the length of a line without trailing newline/CR.
// Garland's GetLine includes the line terminator, but for cursor positioning
// we need the length without it.
func (e *Editor) getEffectiveLineLen(buf *buffer.Buffer, lineNum int) int {
	line := buf.GetLine(lineNum)
	line = strings.TrimRight(line, "\n\r")
	return len([]rune(line))
}

// runeToVisualColumn converts a rune position to a visual column position.
// This accounts for tabs (variable width) and control characters (2 chars wide).
// Translated from TypeScript CoordinateUtils.documentRuneToColumn
//
// lineMarkSet is the set of positions on the viewport's caret line that get a
// showMarks "*" cell, mirroring what prepareLineForDisplay draws: one before the
// cell of each marked rune, plus — only when invisibles are shown, so the
// terminator slot exists to host it — a mark sitting at end of line. It returns
// nil when showMarks is off or the line has no drawable marks. Every visual-
// column walk (plain and bidi, forward and inverse) consumes this one set, so a
// mark inserts its cell in the same place on all of them; there is no flat
// offset (a mark before a tab steals a column the tab would otherwise fill, so
// the shift is not additive). Marks are keyed on the caret's line, the only line
// these coordinate helpers are asked about.
func (e *Editor) lineMarkSet(w *viewport.Viewport, runes []rune) map[int]bool {
	if w == nil || !w.ViewState.MarksVisible() || w.Buffer == nil {
		return nil
	}
	// Mark cells are suppressed on a substituted caret line — the doc positions
	// the marks name have no cells of their own there. Mirrors the renderer's
	// lineMarkSet gate.
	if spans, dw := e.lineDisplaySpans(w, w.CursorPos().Line); len(spans) > 0 || dw {
		return nil
	}
	raw := w.Buffer.MarksOnLine(w.CursorPos().Line, w.ViewState.MarksShowInternal())
	if len(raw) == 0 {
		return nil
	}
	// A mark at end of line has no rune to precede. On a plain line the renderer
	// just appends a trailing "*" cell, so it always shows; on a bidi line
	// "after the content" is ambiguous under reordering, so there it still rides
	// the terminator slot and only shows when invisibles are on.
	eolDrawn := w.ViewState.ShowInvisibles || e.layoutFor(w, runes) == nil
	m := make(map[int]bool, len(raw))
	for _, p := range raw {
		if p < 0 || p > len(runes) {
			continue
		}
		if p == len(runes) && !eolDrawn {
			continue
		}
		m[p] = true
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// markedLine reports the plain (non-bidi) showMarks case, where the visual walk
// is a simple left-to-right scan: showMarks on, the caret line non-bidi, and it
// has drawable marks. It returns the line's runes and the mark-cell set; ok is
// false (callers take the base path) for bidi lines, marks off, or no marks.
// Bidi lines are exact too, but through bidiColumns / the bidi inverse walks,
// which consume lineMarkSet directly.
func (e *Editor) markedLine(w *viewport.Viewport, line string) (runes []rune, marked map[int]bool, ok bool) {
	if w == nil || !w.ViewState.MarksVisible() || w.Buffer == nil {
		return nil, nil, false
	}
	runes = []rune(line)
	if e.layoutFor(w, runes) != nil {
		return nil, nil, false
	}
	marked = e.lineMarkSet(w, runes)
	if marked == nil {
		return nil, nil, false
	}
	return runes, marked, true
}

// runeToVisualColumnMarked is the plain forward walk with showMarks cells: it
// emits a "*" cell before each marked rune, then the rune, resolving tab widths
// at the SHIFTED column exactly as prepareLineForDisplay paints them. It returns
// the visual column of rune runePos's own cell (past its leading "*"). It is the
// forward twin of visualColumnToRuneMarked and mirrors the renderer's slot loop,
// so a mark before a tab shrinks (or grows) that tab identically on both sides.
func (e *Editor) runeToVisualColumnMarked(runes []rune, marked map[int]bool, runePos, tabSize int) int {
	if runePos < 0 {
		runePos = 0
	}
	col := 0
	for i := 0; i < len(runes); i++ {
		if marked[i] { // "*" cell before rune i
			col++
		}
		if i == runePos {
			return col
		}
		col += e.runeWidthAt(runes, i, col, tabSize)
	}
	// runePos at/after end of line: include a trailing mark so the column agrees
	// with the inverse walk (visualColumnToRuneMarked).
	if marked[len(runes)] {
		col++
	}
	return col
}

// runeToVisualColumn is the display column of a rune, including the showMarks
// "*" cells so it stays in step with what the renderer draws.
func (e *Editor) runeToVisualColumn(w *viewport.Viewport, line string, runePos int, tabSize int) int {
	if runes, marked, ok := e.markedLine(w, line); ok {
		return e.runeToVisualColumnMarked(runes, marked, runePos, tabSize)
	}
	return e.runeToVisualColumnBase(w, line, runePos, tabSize)
}

func (e *Editor) runeToVisualColumnBase(w *viewport.Viewport, line string, runePos int, tabSize int) int {
	runes := []rune(line)

	// Bidirectional line: the rune's visual column is where its cell is
	// painted in visual order, which can be anywhere on the line. bidiColumns
	// inserts the showMarks "*" cells in visual order, so cols/total are exact.
	if layout := e.layoutFor(w, runes); layout != nil {
		cols, total := e.bidiColumns(runes, layout, e.lineMarkSet(w, runes), tabSize)
		if runePos >= len(runes) {
			return total
		}
		if runePos < 0 {
			runePos = 0
		}
		return cols[runePos]
	}

	if runePos <= 0 {
		return 0
	}
	maxRune := runePos
	if maxRune > len(runes) {
		maxRune = len(runes)
	}

	column := 0
	for i := 0; i < maxRune; i++ {
		runeWidth := e.runeWidthAt(runes, i, column, tabSize)
		column += runeWidth
	}

	return column
}

// baseRTL reports whether the configured base direction is right-to-left.
func (e *Editor) baseRTL() bool { return e.Config.Direction == "rtl" }

// layoutFor computes a line's visual layout for a viewport: with the viewport's
// showBidi enabled it includes direction-marker slots (bidi.ComputeMarked),
// whose cell widths every visual-column computation must account for.
func (e *Editor) layoutFor(w *viewport.Viewport, runes []rune) *bidi.Layout {
	if w != nil && w.ViewState.ShowBidi {
		return bidi.ComputeMarked(runes, e.winRTL(w))
	}
	return bidi.Compute(runes, e.winRTL(w))
}

// slotWidth is the visual width of one layout slot: marker slots are one
// column; explicit direction controls are one column under a marked layout
// (they render as their own marker) and zero otherwise; everything else uses
// the ordinary rune width.
func (e *Editor) slotWidth(layout *bidi.Layout, runes []rune, entry, col, tabSize int) int {
	if entry < 0 {
		return 1
	}
	// The absorbed half of a lam-alef ligature shares the first half's cell.
	if layout != nil && layout.Glyph != nil && layout.Glyph[entry] == bidi.LigatureAbsorbed {
		return 0
	}
	r := runes[entry]
	if layout != nil && layout.Marked && bidi.IsDirectionControl(r) {
		return 1
	}
	// Base-aware, so an ill-formed mark measures the SPACING substitute painted
	// for it rather than the zero cells a well-formed mark takes. Without that
	// the caret walk would step over it as though it rode the previous cell,
	// and the columns this feeds would disagree with the paint.
	return e.runeWidthAt(runes, entry, col, tabSize)
}

// winRTL is the EFFECTIVE direction for a viewport: its ViewState.Direction
// override when set (prompt viewports are pinned "ltr"), else the base option.
func (e *Editor) winRTL(w *viewport.Viewport) bool {
	if w != nil {
		switch w.ViewState.Direction {
		case "ltr":
			return false
		case "rtl":
			return true
		}
	}
	return e.baseRTL()
}

// caretVisualColumn is the column where the CARET is drawn for a logical
// position — biased by the direction of the character at the caret. In LTR
// context the caret sits on (the left edge of) the rune it precedes; in RTL
// context "before rune i" is visually at the rune's RIGHT edge, so the caret
// parks one cell to the right of the rune's cell. At end of line the boundary
// follows the last rune's direction (one cell LEFT of an RTL line's leftmost
// cell — possibly -1, which the right-alignment pad absorbs).
func (e *Editor) caretVisualColumn(w *viewport.Viewport, line string, runePos, tabSize int) int {
	// Non-bidi: the caret sits on the rune's own cell, so its column is exactly
	// the marks-inclusive rune column from the inline walk (which resolves tab
	// widths at the shifted column). Bidi is exact through caretVisualColumnBase,
	// whose cols/total include the "*" cells. Mirrors the renderer's wrapper.
	if runes, marked, ok := e.markedLine(w, line); ok {
		return e.runeToVisualColumnMarked(runes, marked, runePos, tabSize)
	}
	return e.caretVisualColumnBase(w, line, runePos, tabSize)
}

func (e *Editor) caretVisualColumnBase(w *viewport.Viewport, line string, runePos, tabSize int) int {
	runes := []rune(line)
	layout := e.layoutFor(w, runes)
	if layout == nil {
		return e.runeToVisualColumnBase(w, line, runePos, tabSize)
	}
	cols, total := e.bidiColumns(runes, layout, e.lineMarkSet(w, runes), tabSize)
	rtlBase := e.winRTL(w)

	// A zero-width combining mark shares its base character's cell, so the
	// END-OF-LINE boundary follows the side the BASE character dictates: step
	// back over zero-width runes to the cluster base. (Marked direction
	// controls are one column wide and stop the walk.)
	clusterBase := func(i int) int {
		for i > 0 && e.slotWidth(layout, runes, i, cols[i], tabSize) == 0 {
			i--
		}
		return i
	}

	// A caret PARTWAY THROUGH a cluster — between a base and its combining
	// mark, or between two marks — is placed as though it followed the whole
	// cluster. Those positions have no cell of their own (the marks ride the
	// base), so the caret has to borrow one, and the cluster's trailing edge is
	// the honest choice: the caret is logically past the base, and parking it at
	// the LEADING edge would draw it on the far side of a character it has
	// already gone by. In an RTL run that is the visible difference between the
	// caret sitting before the letter and after it — which is why this only ever
	// showed up in right-to-left text: the left-to-right walk is a prefix sum,
	// and a prefix sum already lands past the cluster.
	if runePos < 0 {
		runePos = 0
	}
	startPos := runePos
	for runePos < len(runes) && e.slotWidth(layout, runes, runePos, cols[runePos], tabSize) == 0 {
		runePos++
	}

	// The caret sits where the next character OF THE CARET'S OWN DIRECTION
	// (the direction the rtl command / modebar logo reports — the direction
	// of the rune at the caret) would land. This is independent of showBidi:
	// the markers only add cells (shifting cols), not change the rule.
	if runePos >= len(runes) {
		last := clusterBase(len(runes) - 1)
		if layout.RTL[last] {
			// One past the last character, leftward (its direction).
			return cols[last] - 1
		}
		if rtlBase {
			// Right-anchored line ending in LTR text: one cell right of the
			// last character — NOT the line's right edge (that is the gutter).
			return cols[last] + e.slotWidth(layout, runes, last, cols[last], tabSize)
		}
		return total
	}
	// When the caret advanced ACROSS a cluster (it skipped that cluster's marks)
	// whose base is RTL, it followed the cluster to its reading-trailing edge,
	// which in an RTL run is one cell LEFT of the base. Return that directly. For
	// an RTL rune following the cluster this equals its column, but where a
	// left-to-right run follows, that run's column is bidi-teleported to the far
	// end of the line — the caret must stay by the cluster, not jump there.
	if runePos > startPos {
		if base := clusterBase(startPos); layout.RTL[base] {
			return cols[base] - 1
		}
	}
	// The caret covers the cell of the rune it precedes — in either base
	// direction, for both LTR and RTL runes. A block cursor sits ON that
	// character, and an RTL rune's cell is painted at cols[runePos], so a
	// caret inside an RTL fragment stays on the rune rather than parking one
	// cell to its right.
	return cols[runePos]
}

// leftmostCaret returns the smallest visual column any caret position on the
// line can occupy, and the position (rune index; len for end-of-line) that
// occupies it. Under direction=rtl this is the line's furthest-back READING
// position — vw minus the returned column is the largest reading column the
// line reaches. On a plain left-to-right layout that caret is position 0 at
// column 0; on bidi lines it can sit anywhere, including one cell LEFT of the
// content when the line ends in RTL text (the EOL boundary's own cell).
func (e *Editor) leftmostCaret(w *viewport.Viewport, line string, tabSize int) (col, pos int) {
	runes := []rune(line)
	layout := e.layoutFor(w, runes)
	col, pos = e.caretVisualColumn(w, line, len(runes), tabSize), len(runes)
	if layout == nil {
		// Monotonic columns: position 0 is the leftmost caret.
		if c := e.caretVisualColumn(w, line, 0, tabSize); c < col {
			return c, 0
		}
		return col, pos
	}
	cols, _ := e.bidiColumns(runes, layout, e.lineMarkSet(w, runes), tabSize)
	for i, c := range cols {
		// A zero-width slot (combining mark, absorbed ligature half) shares
		// its base character's cell; the caret rests there via the base.
		if e.slotWidth(layout, runes, i, c, tabSize) == 0 {
			continue
		}
		if c < col {
			col, pos = c, i
		}
	}
	return col, pos
}

// lineVisualWidth is the total visual width of a line (tab widths resolved
// in visual order, matching the renderer).
func (e *Editor) lineVisualWidth(w *viewport.Viewport, line string, tabSize int) int {
	runes := []rune(line)
	marked := e.lineMarkSet(w, runes)
	layout := e.layoutFor(w, runes)
	if layout == nil {
		vw := 0
		for i := range runes {
			if marked[i] {
				vw++
			}
			vw += e.runeWidthAt(runes, i, vw, tabSize)
		}
		if marked[len(runes)] {
			vw++
		}
		return vw
	}
	_, total := e.bidiColumns(runes, layout, marked, tabSize)
	return total
}

// idealColumn returns the direction-appropriate "sticky" column used to hold
// the caret at a stable SCREEN position across vertical moves. In LTR it is
// the left-based visual column (the view is left-anchored, so screen X tracks
// the visual column directly). In RTL the view is right-anchored, so screen X
// tracks the READING column — the caret's distance back from the line's
// reading start (its rightmost visual cell) — which stays put on screen as
// lines of different widths right-align beneath it.
func (e *Editor) idealColumn(w *viewport.Viewport, line string, runePos, tabSize int) int {
	if e.winRTL(w) {
		return e.lineVisualWidth(w, line, tabSize) - e.caretVisualColumn(w, line, runePos, tabSize)
	}
	return e.runeToVisualColumn(w, line, runePos, tabSize)
}

// bidiColumns walks a line's visual order accumulating cell columns: cols
// maps each LOGICAL rune index to the visual column its cell starts at, and
// total is the line's full visual width. Tab widths resolve at their VISUAL
// column, like the renderer paints them. When marked is non-nil a showMarks "*"
// cell is inserted in visual order just before each marked rune's cell (and a
// trailing one for an end-of-line mark), so cols/total are exact for bidi lines
// with marks — mirroring the "*" insertion in prepareLineForDisplay.
func (e *Editor) bidiColumns(runes []rune, layout *bidi.Layout, marked map[int]bool, tabSize int) (cols []int, total int) {
	cols = make([]int, len(runes))
	col := 0
	for _, li := range layout.Perm {
		if li >= 0 && marked[li] {
			col++
		}
		if li >= 0 {
			cols[li] = col
		}
		col += e.slotWidth(layout, runes, li, col, tabSize)
	}
	if marked[len(runes)] {
		col++
	}
	return cols, col
}

// visualColumnToRune converts a visual column position to a rune position.
// This is the inverse of runeToVisualColumn.
// Translated from TypeScript CoordinateUtils.columnToDocumentRune
func (e *Editor) visualColumnToRune(w *viewport.Viewport, line string, targetColumn int, tabSize int) int {
	// showMarks (non-bidi): account for the inserted "*" cells via the marked
	// walk, so a click maps to the right rune.
	if runes, marked, ok := e.markedLine(w, line); ok {
		return e.visualColumnToRuneMarked(runes, marked, targetColumn, tabSize).Rune
	}

	runes := []rune(line)

	// Bidirectional line: find the visual cell covering the target column and
	// return its LOGICAL rune index (past the end returns len). A showMarks "*"
	// cell precedes each marked rune in visual order; a click on it selects that
	// rune, keeping this the exact inverse of bidiColumns.
	if layout := e.layoutFor(w, runes); layout != nil {
		marked := e.lineMarkSet(w, runes)
		col := 0
		for v, li := range layout.Perm {
			if li >= 0 && marked[li] {
				if targetColumn < col+1 {
					return li
				}
				col++
			}
			cw := e.slotWidth(layout, runes, li, col, tabSize)
			if cw > 0 && targetColumn < col+cw {
				if li >= 0 {
					return li
				}
				// A begin-marker cell maps to its fragment's leading rune:
				// the next real slot for an entering-LTR marker, the
				// previous for entering-RTL. An end marker ("|") maps to the
				// position just past the fragment's reading-last rune: one
				// past the previous real slot for an LTR fragment (the "|"
				// follows the content), one past the next real slot for an
				// RTL fragment (the "|" precedes the reversed content, whose
				// leftmost cell is the fragment's logically last rune).
				switch li {
				case bidi.MarkerLTR:
					for k := v + 1; k < len(layout.Perm); k++ {
						if layout.Perm[k] >= 0 {
							return layout.Perm[k]
						}
					}
				case bidi.MarkerEnd:
					for k := v - 1; k >= 0; k-- {
						if layout.Perm[k] >= 0 && !layout.RTL[layout.Perm[k]] {
							return layout.Perm[k] + 1
						}
						if layout.Perm[k] >= 0 {
							break
						}
					}
					for k := v + 1; k < len(layout.Perm); k++ {
						if layout.Perm[k] >= 0 {
							return layout.Perm[k] + 1
						}
					}
				default:
					for k := v - 1; k >= 0; k-- {
						if layout.Perm[k] >= 0 {
							return layout.Perm[k]
						}
					}
				}
				return len(runes)
			}
			col += cw
		}
		return len(runes)
	}

	if targetColumn <= 0 {
		return 0
	}

	column := 0

	for i := 0; i < len(runes); i++ {
		runeWidth := e.runeWidthAt(runes, i, column, tabSize)

		// If adding this rune would go past the target, return current position
		if column+runeWidth > targetColumn {
			return i
		}

		column += runeWidth

		// If we've reached or passed the target, return next position
		if column >= targetColumn {
			return i + 1
		}
	}

	return len(runes)
}

// visualColumnToRuneResult holds the result of visualColumnToRuneWithActual.
type visualColumnToRuneResult struct {
	Rune         int // The rune position
	ActualColumn int // The actual visual column at that rune position
}

// visualColumnToRuneWithActual converts a visual column to a rune position,
// also returning the actual visual column at that position.
// This is useful for detecting when the target falls within a wide character like a tab.
// visualColumnToRuneWithActual maps a display column back to a rune. With
// showMarks on (and a non-bidi line with marks), it walks the visual cells
// including the inserted "*" cells so a click on/after a mark lands on the right
// rune and the reported column is in the same marks-inclusive space as the
// forward math. Bidi lines fall back to the base mapping (marks are rare there).
func (e *Editor) visualColumnToRuneWithActual(w *viewport.Viewport, line string, targetColumn int, tabSize int) visualColumnToRuneResult {
	if runes, marked, ok := e.markedLine(w, line); ok {
		return e.visualColumnToRuneMarked(runes, marked, targetColumn, tabSize)
	}
	return e.visualColumnToRuneWithActualBase(w, line, targetColumn, tabSize)
}

// visualColumnToRuneMarked is the plain (non-bidi) inverse walk with showMarks
// cells: it emits, in order, the "*" cell at each marked rune position then the
// rune, and returns the rune whose cell (or leading "*") covers targetColumn.
// ActualColumn is reported marks-inclusive.
func (e *Editor) visualColumnToRuneMarked(runes []rune, marked map[int]bool, targetColumn, tabSize int) visualColumnToRuneResult {
	col := 0
	for i := 0; i <= len(runes); i++ {
		if marked[i] { // one "*" cell before rune i (or at end of line)
			if targetColumn <= col {
				return visualColumnToRuneResult{Rune: i, ActualColumn: col}
			}
			col++
		}
		if i == len(runes) {
			break
		}
		rw := e.runeWidthAt(runes, i, col, tabSize)
		if targetColumn < col+rw {
			return visualColumnToRuneResult{Rune: i, ActualColumn: col}
		}
		col += rw
	}
	return visualColumnToRuneResult{Rune: len(runes), ActualColumn: col}
}

func (e *Editor) visualColumnToRuneWithActualBase(w *viewport.Viewport, line string, targetColumn int, tabSize int) visualColumnToRuneResult {
	runes := []rune(line)

	// Bidirectional line: the cell covering the target column, by visual walk.
	// A showMarks "*" cell precedes each marked rune (a click on it selects that
	// rune), so this stays the exact inverse of bidiColumns.
	if layout := e.layoutFor(w, runes); layout != nil {
		marked := e.lineMarkSet(w, runes)
		col := 0
		for _, li := range layout.Perm {
			if li >= 0 && marked[li] {
				if targetColumn < col+1 {
					return visualColumnToRuneResult{Rune: li, ActualColumn: col}
				}
				col++
			}
			cw := e.slotWidth(layout, runes, li, col, tabSize)
			if li >= 0 && cw > 0 && targetColumn < col+cw {
				return visualColumnToRuneResult{Rune: li, ActualColumn: col}
			}
			col += cw
		}
		return visualColumnToRuneResult{Rune: len(runes), ActualColumn: col}
	}

	if targetColumn <= 0 {
		return visualColumnToRuneResult{Rune: 0, ActualColumn: 0}
	}

	column := 0

	for i := 0; i < len(runes); i++ {
		runeWidth := e.runeWidthAt(runes, i, column, tabSize)

		// If adding this rune would go past the target, return current position
		if column+runeWidth > targetColumn {
			return visualColumnToRuneResult{Rune: i, ActualColumn: column}
		}

		column += runeWidth

		// If we've reached or passed the target, return next position
		if column >= targetColumn {
			return visualColumnToRuneResult{Rune: i + 1, ActualColumn: column}
		}
	}

	return visualColumnToRuneResult{Rune: len(runes), ActualColumn: column}
}

// getRuneVisualWidth returns the visual width of a rune at a given visual
// column. Tabs have variable width depending on position, control chars are
// 2 wide (^X display); other runes measure by their terminal cell width —
// 0 for combining/zero-width characters, 2 for wide (CJK, emoji).
func (e *Editor) getRuneVisualWidth(r rune, currentColumn int, tabSize int) int {
	if r == '\t' {
		// Tab width to next tab stop
		return tabSize - (currentColumn % tabSize)
	} else if textwidth.IsControl(r) {
		// Control characters displayed as ^X / hex — C0, DEL, and the C1 set
		// U+0080..U+009F (see textwidth.IsControl).
		return len(runeHexSubstitute(r))
	}
	return textwidth.Rune(r)
}

// runeWidthAt measures runes[i] with its cluster base in hand, so an
// ill-formed combining mark (see textwidth.DefectiveMark) measures as the
// hex substitute the renderer paints for it rather than the zero cells a
// well-formed mark takes. Every caret/column walk goes through here, so the
// editor's math and the screen agree on where a defective mark's cells are.
func (e *Editor) runeWidthAt(runes []rune, i, currentColumn, tabSize int) int {
	r := runes[i]
	if textwidth.IsMark(r) {
		var base rune
		for j := i - 1; j >= 0; j-- {
			if !textwidth.IsMark(runes[j]) {
				base = runes[j]
				break
			}
		}
		if textwidth.DefectiveMark(base, r) {
			if textwidth.AnchorMark(base, r) {
				return 1 // dotted circle carries the mark in one cell
			}
			return len(runeHexSubstitute(r))
		}
	}
	return e.getRuneVisualWidth(r, currentColumn, tabSize)
}

// runeHexSubstitute mirrors the renderer's substitute text for a defective
// combining mark: the codepoint's hex form, one ASCII column per character.
func runeHexSubstitute(r rune) string {
	v := int(r)
	if v <= 0xFF {
		return fmt.Sprintf("%02X", v) // one byte reads fine bare: FE
	}
	if v <= 0xFFFF {
		return fmt.Sprintf("(%04X)", v) // past one byte, bracket it: (0123)
	}
	return fmt.Sprintf("(%06X)", v)
}

// bidiControlRune maps a short bidi-control name to its Unicode code point.
// The set is deliberately the surgical marks and isolates (not the overrides
// or legacy embeddings): lrm/rlm/alm pin neutral punctuation, and the isolate
// group (fsi/lri/rli + pdi) brackets a foreign-direction span without leaking.
func bidiControlRune(name string) (rune, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "lrm":
		return '‎', true // LEFT-TO-RIGHT MARK
	case "rlm":
		return '‏', true // RIGHT-TO-LEFT MARK
	case "alm":
		return '؜', true // ARABIC LETTER MARK
	case "fsi":
		return '⁨', true // FIRST STRONG ISOLATE
	case "lri":
		return '⁦', true // LEFT-TO-RIGHT ISOLATE
	case "rli":
		return '⁧', true // RIGHT-TO-LEFT ISOLATE
	case "pdi":
		return '⁩', true // POP DIRECTIONAL ISOLATE
	}
	return 0, false
}

// insertBidiControl inserts the bidi control named by name, behaving exactly
// like the insert command (insert the text, track the edit). Reports false on
// an unknown name.
func (e *Editor) insertBidiControl(name string) bool {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	r, ok := bidiControlRune(name)
	if !ok {
		e.ShowWarning("Unknown control mark: " + name)
		return false
	}
	e.insertText(string(r))
	e.trackEdit()
	e.editCoalesced = true // a single-point edit: coalesce the undo run
	return true
}

// parseCodePoint reads a Unicode scalar written the way a user writes one:
// "U+05D0", "u+05d0", "05D0", "0x05D0" - hex by default, because that is how
// code points are spelled everywhere they appear. "#" forces decimal for the
// rare case someone has one in that form ("#1488").
//
// Decimal is "#" and NOT "d": "d" is a hex digit, so a "d" prefix would eat
// the leading digit of "D800" and read the rest as decimal - quietly turning a
// surrogate (which this rejects) into U+0320 (which it would not).
//
// Surrogates (U+D800..U+DFFF) are rejected: they are not scalars, they only
// exist inside UTF-16, and Go would silently substitute U+FFFD for one.
func parseCodePoint(in string) (rune, bool) {
	if r, ok := literalControl(in); ok {
		return r, true
	}
	t := strings.TrimSpace(in)
	if t == "" {
		return 0, false
	}
	// \uNNNN is how a code point is written in source, so accept it (and the
	// rest of the C escapes) here too.
	if r, ok := parseCEscape(t); ok && r <= unicode.MaxRune && !(r >= 0xD800 && r <= 0xDFFF) {
		return r, true
	}
	base := 16
	switch {
	case len(t) > 2 && (strings.HasPrefix(t, "U+") || strings.HasPrefix(t, "u+")):
		t = t[2:]
	case len(t) > 2 && (strings.HasPrefix(t, "0x") || strings.HasPrefix(t, "0X")):
		t = t[2:]
	case len(t) > 1 && t[0] == '#':
		t, base = t[1:], 10
	}
	n, err := strconv.ParseInt(t, base, 64)
	if err != nil || n < 0 || n > unicode.MaxRune {
		return 0, false
	}
	if n >= 0xD800 && n <= 0xDFFF {
		return 0, false // surrogate half: not a scalar value
	}
	return rune(n), true
}

// asciiControlNames maps the conventional names of the C0 controls (plus SP and
// DEL) to their values, so a byte can be asked for by the name it is known by
// rather than by a number the user has to look up.
var asciiControlNames = map[string]rune{
	"NUL": 0x00, "SOH": 0x01, "STX": 0x02, "ETX": 0x03, "EOT": 0x04, "ENQ": 0x05,
	"ACK": 0x06, "BEL": 0x07, "BS": 0x08, "HT": 0x09, "TAB": 0x09, "LF": 0x0A,
	"NL": 0x0A, "VT": 0x0B, "FF": 0x0C, "CR": 0x0D, "SO": 0x0E, "SI": 0x0F,
	"DLE": 0x10, "DC1": 0x11, "XON": 0x11, "DC2": 0x12, "DC3": 0x13, "XOFF": 0x13,
	"DC4": 0x14, "NAK": 0x15, "SYN": 0x16, "ETB": 0x17, "CAN": 0x18, "EM": 0x19,
	"SUB": 0x1A, "ESC": 0x1B, "FS": 0x1C, "GS": 0x1D, "RS": 0x1E, "US": 0x1F,
	"SP": 0x20, "SPACE": 0x20, "DEL": 0x7F,
}

// parseByteSpec reads a byte 0..255 in whichever notation the value already
// exists in the user's head, because the reason to reach for this command at
// all is that the character cannot be typed:
//
//	^[  ^A  ^@  ^?   caret notation - the key you would press. ^? is DEL.
//	ESC BEL TAB CR   the control's conventional name (see asciiControlNames)
//	x1b 0x1b $1b     hex
//	o33 0o33         octal
//	b11011 0b11011   binary
//	#27              decimal ("#", not "d": "d" is a hex digit)
//	1b               bare, read as HEX - the convention for a byte
//	\n \r \t \e \0    the C escape, plus \xNN hex and \NNN octal
//
// A leading "b" is binary only when everything after it is 0 or 1 ("b11011");
// otherwise it is a hex digit as usual ("be" = 0xBE). Ambiguous only for
// values like "b0", which is read as binary - write "xb0" for the hex one.
//
// Hex is tried before the name table, so "ff" is 255 rather than FF (form
// feed) - the one name that is also a valid hex byte. Write "^L" or "x0c" for
// that character.
//
// The value is returned as a rune only because that is a convenient carrier for
// 0..255; it is a BYTE, and insertRawByteAt writes exactly that byte. High
// bytes are not reinterpreted as Latin-1 scalars - see insertRawByteAt for why
// the invalid-UTF-8 consequences are the caller's to own.
// cEscapes are the single-letter backslash escapes a C, Go, Python or shell
// programmer already has in their fingers. "\\e" is not ISO C, but every
// toolchain worth using accepts it for ESC and it is the one people reach for.
var cEscapes = map[byte]rune{
	'a': 0x07, 'b': 0x08, 'e': 0x1B, 'f': 0x0C, 'n': 0x0A,
	'r': 0x0D, 't': 0x09, 'v': 0x0B, '0': 0x00,
	'\\': 0x5C, '\'': 0x27, '"': 0x22, '?': 0x3F,
}

// parseCEscape reads a backslash escape in its C spelling: \n \r \t \0 \e and
// friends, \xNN hex, \NNN octal (1-3 digits), \uNNNN and \UNNNNNNNN. Returns
// the scalar and whether the whole string was consumed by one escape - a
// trailing anything makes it not an escape at all, so "\n\n" is rejected
// rather than quietly inserting one of them.
func parseCEscape(t string) (rune, bool) {
	if len(t) < 2 || t[0] != '\\' {
		return 0, false
	}
	body := t[1:]
	switch body[0] {
	case 'x', 'X':
		n, err := strconv.ParseInt(body[1:], 16, 64)
		if err != nil || n < 0 {
			return 0, false
		}
		return rune(n), true
	case 'u', 'U':
		n, err := strconv.ParseInt(body[1:], 16, 64)
		if err != nil || n < 0 || n > unicode.MaxRune {
			return 0, false
		}
		return rune(n), true
	}
	// Octal: \NNN, 1-3 digits. Checked before the single-letter table so \012
	// is octal 12 rather than \0 with stray digits after it.
	if body[0] >= '0' && body[0] <= '7' && len(body) > 1 {
		if len(body) > 3 {
			return 0, false
		}
		n, err := strconv.ParseInt(body, 8, 64)
		if err != nil || n < 0 {
			return 0, false
		}
		return rune(n), true
	}
	if len(body) != 1 {
		return 0, false
	}
	r, ok := cEscapes[body[0]]
	return r, ok
}

// literalByteRune accepts the input when it is ALREADY the character itself and
// names a byte. Wider than literalControl because PawScript resolves "\xfe" to
// the SCALAR U+00FE before this command ever runs, so a high byte arrives as one
// non-ASCII rune - and for a byte command that means the byte, not the scalar.
//
// Everything ASCII-printable is excluded, because that is where the numeric and
// named spellings live: "a" has to stay hex 0x0A, "FF" has to stay 255, "ESC"
// has to stay a name. What is left - controls, DEL, and U+0080..U+00FF - cannot
// be written any of those ways, so taking it literally is unambiguous. A rune
// past U+00FF is not a byte at all and is refused.
func literalByteRune(in string) (rune, bool) {
	rs := []rune(in)
	if len(rs) != 1 {
		return 0, false
	}
	r := rs[0]
	if r < 0x20 || r == 0x7F || (r >= 0x80 && r <= 0xFF) {
		return r, true
	}
	return 0, false
}

// literalControl accepts the input when it is ALREADY the character itself -
// one control rune, untrimmed. A keymap or PawScript argument written "\n" has
// its escape resolved by the parser before this command ever runs, so by the
// time the value arrives it is a real newline. Only control characters qualify:
// a printable single character like "a" stays a hex digit.
func literalControl(in string) (rune, bool) {
	rs := []rune(in)
	if len(rs) != 1 {
		return 0, false
	}
	if rs[0] < 0x20 || rs[0] == 0x7F {
		return rs[0], true
	}
	return 0, false
}

func parseByteSpec(in string) (rune, bool) {
	// Before trimming: TrimSpace would eat a literal tab or newline, and those
	// are exactly the values someone is most likely to be inserting.
	if r, ok := literalByteRune(in); ok {
		return r, true
	}
	t := strings.TrimSpace(in)
	if t == "" {
		return 0, false
	}
	if r, ok := parseCEscape(t); ok && r <= 0xFF {
		return r, true
	}
	// Caret notation: the key you would actually press.
	if len(t) == 2 && t[0] == '^' {
		c := t[1]
		if c == '?' {
			return 0x7F, true // ^? is DEL by convention, not 0x1F
		}
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		if c >= '@' && c <= '_' { // @A-Z[\]^_ cover the whole C0 range
			return rune(c & 0x1F), true
		}
		return 0, false
	}

	base := 16 // a byte is spelled in hex unless told otherwise
	switch {
	case len(t) > 2 && t[0] == '0' && (t[1] == 'x' || t[1] == 'X'):
		t = t[2:]
	case len(t) > 2 && t[0] == '0' && (t[1] == 'o' || t[1] == 'O'):
		t, base = t[2:], 8
	case len(t) > 2 && t[0] == '0' && (t[1] == 'b' || t[1] == 'B') && isBinaryDigits(t[2:]):
		t, base = t[2:], 2
	case len(t) > 1 && (t[0] == 'x' || t[0] == 'X' || t[0] == '$'):
		t = t[1:]
	case len(t) > 1 && (t[0] == 'o' || t[0] == 'O'):
		t, base = t[1:], 8
	case len(t) > 1 && (t[0] == 'b' || t[0] == 'B') && isBinaryDigits(t[1:]):
		t, base = t[1:], 2
	case len(t) > 1 && t[0] == '#':
		t, base = t[1:], 10
	}

	// Bare input is hex, and hex is tried BEFORE the name table so "ff" is 255
	// rather than FF (form feed) - the only name that is also valid hex.
	if n, err := strconv.ParseInt(t, base, 64); err == nil && n >= 0 && n <= 0xFF {
		return rune(n), true
	}
	// Names are tried on the ORIGINAL text, after any numeric reading failed:
	// "XOFF" and "XON" begin with the hex prefix letter, so the switch above
	// has already eaten their X by the time we get here.
	if r, ok := asciiControlNames[strings.ToUpper(strings.TrimSpace(in))]; ok {
		return r, true
	}
	return 0, false
}

// isBinaryDigits reports whether s is a non-empty run of 0s and 1s, which is
// what distinguishes a "b" binary prefix from a "b" hex digit.
func isBinaryDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '0' && s[i] != '1' {
			return false
		}
	}
	return true
}

// insertRuneAt inserts a single scalar, sharing insertBidiControl's edit
// shape: refused on locked content, one coalesced undo step.
func (e *Editor) insertRuneAt(r rune) bool {
	if e.contentLocked() {
		return false
	}
	e.insertText(string(r))
	e.trackEdit()
	e.editCoalesced = true
	return true
}

// insertRawByteAt inserts ONE byte, exactly the byte asked for. 0x80..0xFF go
// in as that single byte and NOT as the Latin-1 scalar of the same number,
// which would be two bytes of UTF-8 and a different file on disk.
//
// This does mean a line can hold a byte sequence that is not valid UTF-8, and
// the editor's rune-indexed columns will read it as whatever Go's decoder makes
// of it. That is the deal: a command called "insert raw byte" that quietly
// inserted a different byte would be worthless for the job it exists to do -
// patching a binary, embedding a control code, fixing an encoding by hand. The
// consequences belong to the user who asked for the byte.
func (e *Editor) insertRawByteAt(b byte) bool { return e.insertRawBytesAt([]byte{b}) }

// insertRawBytesAt inserts a byte SEQUENCE verbatim, for a PawScript
// {bytes ...} value. Garland stores bytes (insertStringAt is []byte(data) with
// no re-encoding), so what reaches the buffer is exactly what was asked for.
func (e *Editor) insertRawBytesAt(b []byte) bool {
	if e.contentLocked() || len(b) == 0 {
		return false
	}
	e.insertText(string(b))
	e.trackEdit()
	e.editCoalesced = true
	return true
}

// insertText inserts text at the cursor position.
func (e *Editor) insertText(text string) { e.insertTextMode(text, true) }

// insertTextMode is insertText with control over whether overwrite mode applies.
// honorOverwrite=false forces a true insert, for synthetic text that must never
// consume what is already there — an auto-indent's whitespace, which would
// otherwise overwrite the very characters the line break just moved down.
func (e *Editor) insertTextMode(text string, honorOverwrite bool) {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	// Ensure cursor is within valid bounds before insertion
	e.clampCursorToBuffer(w)

	if honorOverwrite && w.ViewState.OverwriteMode {
		// Overwrite mode: typing replaces the character under the caret.
		e.overwriteText(w, text)
	} else {
		// Insert through the viewport's own caret cursor, then read the caret back:
		// garland advances it past the inserted text (splitting on embedded
		// newlines internally), so there is no manual line/rune arithmetic.
		w.Caret.Seek(w.CursorPos().Line, w.CursorPos().Rune)
		w.Caret.Insert(text)
	}

	// Clear ghost cursor and update ideal column after typing
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	e.RequestRender()
}

// insertNewline is the insert_newline command — the default Enter binding's
// last resort, after nav_follow and accept. It breaks the line exactly as
// `insert '\n'` does and, when the autoIndent option is on for the viewport,
// follows the break with the indent autoIndentPrefix computes, so the new line
// starts under the text of the one it split.
//
// The whole thing is ONE insert (the break and the whitespace together), so it
// is one undo step and one coalescible edit — except in overwrite mode, where
// the break goes through the overwrite path (which appends it, as overwriting a
// line break must) and the indent is force-inserted after it: overwriting with
// the indent would consume the characters the break just moved down.
func (e *Editor) insertNewline() bool {
	if e.contentLocked() {
		// Read-only viewport, or a focused link button: reject at the source,
		// the same way insertText would, before any of the below runs.
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	indent := e.autoIndentPrefix(w)
	if indent == "" {
		e.insertText("\n")
		return true
	}
	if w.ViewState.OverwriteMode {
		e.insertText("\n")
		e.insertTextMode(indent, false)
		return true
	}
	e.insertTextMode("\n"+indent, false)
	return true
}

// autoIndentPrefix returns the whitespace a new line should open with when the
// autoIndent option is on for w: the leading whitespace of the line the caret
// is about to split, replicated exactly — tabs stay tabs, spaces stay spaces,
// and a mixed indent survives as it was written.
//
// The run is truncated at the caret, which is what keeps Enter from pushing
// text rightward when it lands inside or before the indent: at column 0 of an
// indented line nothing is replicated (the line moves down whole, keeping its
// own indent), and from within the indent only the part already above the
// caret repeats, so the split halves still line up under the original.
func (e *Editor) autoIndentPrefix(w *viewport.Viewport) string {
	if w == nil || w.Buffer == nil || !w.ViewState.AutoIndent {
		return ""
	}
	pos := w.CursorPos()
	if pos.Line < 0 || pos.Line >= w.Buffer.GetLineCount() {
		return ""
	}
	line := []rune(strings.TrimRight(w.Buffer.GetLine(pos.Line), "\n\r"))
	n := 0
	for n < len(line) && (line[n] == ' ' || line[n] == '\t') {
		n++
	}
	if pos.Rune < n {
		n = pos.Rune
	}
	if n <= 0 {
		return ""
	}
	return string(line[:n])
}

// overwriteText types text in overwrite mode: each rune replaces the character
// under the caret via garland's overwrite mutation, which coalesces a run of
// overwrites into one undo step (like typing and deleting do). At (or crossing)
// end of line — and for a newline, which splits the line — it switches to a
// plain insert so the text appends; garland lets that appending insert continue
// the overwrite run, so overtype-then-append stays a single undo step. The
// overwritten character is discarded (not sent to the kill ring), matching how
// typing over a selection works.
func (e *Editor) overwriteText(w *viewport.Viewport, text string) {
	for _, r := range text {
		pos := w.CursorPos()
		if r == '\n' || pos.Rune >= e.getEffectiveLineLen(w.Buffer, pos.Line) {
			// End of line reached (or a line break): append via insert.
			w.Caret.Seek(pos.Line, pos.Rune)
			w.Caret.Insert(string(r))
			continue
		}
		// Replace the rune under the caret, then advance past what we wrote so a
		// continuing overwrite (or the appending insert at EOL) lands at the
		// run's end.
		byteLen := e.runeByteLenAt(w, pos.Line, pos.Rune)
		w.Caret.Seek(pos.Line, pos.Rune)
		w.Caret.Overwrite(int64(byteLen), string(r))
		w.Caret.Seek(pos.Line, pos.Rune+1)
	}
}

// runeByteLenAt returns the UTF-8 byte length of the rune at (line, rune), or 0
// if the position is at or past end of line (no rune stands there).
func (e *Editor) runeByteLenAt(w *viewport.Viewport, line, rune_ int) int {
	content := strings.TrimRight(w.Buffer.GetLine(line), "\n\r")
	runes := []rune(content)
	if rune_ < 0 || rune_ >= len(runes) {
		return 0
	}
	return len(string(runes[rune_]))
}

// insertPasteChunk inserts a single chunk of paste content.
// Called by the main loop as chunks arrive from the keyboard handler.
func (e *Editor) insertPasteChunk(content []byte) {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}
	// Bracketed paste arrives from the main loop, not executeCommand, but it is
	// still a content mutation: gate it at its own source.
	if e.contentLocked() {
		return
	}

	text := string(content)
	if text == "" {
		return
	}

	// Normalize line endings: \r\n -> \n, standalone \r -> \n
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	// Ensure cursor is within valid bounds before insertion
	e.clampCursorToBuffer(w)

	// Insert through the viewport's caret cursor and read it back.
	w.Caret.Seek(w.CursorPos().Line, w.CursorPos().Rune)
	w.Caret.Insert(text)

	// Record the paste in the cursor ring. Multi-chunk pastes call this per
	// chunk, but TrackEdit collapses them: the first chunk may push the prior
	// edit point, and subsequent chunks find hasMoved already cleared, so a
	// paste yields at most one ring entry. A paste is not a kill, so it breaks
	// any delete accumulation in progress.
	w.TrackEdit()
	e.lastEditKill = false
}

// doFind searches for text in the current buffer.
// deleteWord deletes from cursor to end of current word.
func (e *Editor) deleteWord() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	line := w.Buffer.GetLine(w.CursorPos().Line)
	runes := []rune(line)
	startPos := w.CursorPos().Rune

	if startPos >= len(runes) {
		return
	}

	// Find end of word (skip non-whitespace, then skip whitespace)
	endPos := startPos
	// Skip non-whitespace
	for endPos < len(runes) && !isWhitespace(runes[endPos]) {
		endPos++
	}
	// Skip trailing whitespace
	for endPos < len(runes) && isWhitespace(runes[endPos]) {
		endPos++
	}

	if endPos > startPos {
		w.Buffer.DeleteText(w.CursorPos().Line, startPos, endPos-startPos)
	}
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w) // edit locks in the horizontal view
}

// isWhitespace returns true if the rune is whitespace.
func isWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

// isWordRune returns true if the rune is part of a word.
func isWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// moveToNextWord moves cursor to the next word.
func (e *Editor) moveToNextWord() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	line := w.Buffer.GetLine(w.CursorPos().Line)
	runes := []rune(line)
	runePos := w.CursorPos().Rune

	// Skip current word if we're in one
	for runePos < len(runes) && isWordRune(runes[runePos]) {
		runePos++
	}

	// Skip non-word runes
	for runePos < len(runes) && !isWordRune(runes[runePos]) {
		runePos++
	}

	// If we reached end of line and not the last line, go to next line
	if runePos >= len(runes) && w.CursorPos().Line < w.Buffer.GetLineCount()-1 {
		w.SetCursorLine(w.CursorPos().Line + 1)
		w.SetCursorRune(0)
	} else {
		w.SetCursorRune(runePos)
	}

	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// moveToPrevWord moves cursor to the previous word.
func (e *Editor) moveToPrevWord() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	// If at beginning of line and not first line, go to end of previous line
	if w.CursorPos().Rune == 0 && w.CursorPos().Line > 0 {
		w.SetCursorLine(w.CursorPos().Line - 1)
		w.SetCursorRune(e.getEffectiveLineLen(w.Buffer, w.CursorPos().Line))
		e.afterHorizontalMovement(w)
		e.ensureCursorVisible(w)
		return
	}

	line := w.Buffer.GetLine(w.CursorPos().Line)
	runes := []rune(line)
	runePos := w.CursorPos().Rune - 1

	// Skip non-word runes backwards
	for runePos >= 0 && !isWordRune(runes[runePos]) {
		runePos--
	}

	// Find beginning of word
	for runePos >= 0 && isWordRune(runes[runePos]) {
		runePos--
	}

	// Position at start of word
	w.SetCursorRune(runePos + 1)

	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// deleteToWordStart deletes from cursor to beginning of word.
func (e *Editor) deleteToWordStart() {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	line := w.Buffer.GetLine(w.CursorPos().Line)
	line = strings.TrimRight(line, "\n\r")
	runes := []rune(line)

	// Nothing to delete if at start or line is empty
	if len(runes) == 0 || w.CursorPos().Rune <= 0 {
		return
	}

	endPos := w.CursorPos().Rune
	if endPos > len(runes) {
		endPos = len(runes)
	}
	runePos := endPos - 1

	// Skip non-word runes backwards (with bounds check)
	for runePos >= 0 && runePos < len(runes) && !isWordRune(runes[runePos]) {
		runePos--
	}

	// Find beginning of word (with bounds check)
	for runePos >= 0 && runePos < len(runes) && isWordRune(runes[runePos]) {
		runePos--
	}

	startPos := runePos + 1
	if startPos < endPos {
		e.killCapture(w, w.Buffer.DeleteTextCaptured(w.CursorPos().Line, startPos, endPos-startPos), false)
		w.SetCursorRune(startPos)
		// Clear ghost cursor and update ideal column after editing
		e.afterHorizontalMovement(w)
	}
}

// deleteToWordEnd deletes from cursor to end of word.
func (e *Editor) deleteToWordEnd() {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	line := w.Buffer.GetLine(w.CursorPos().Line)
	line = strings.TrimRight(line, "\n\r")
	runes := []rune(line)

	// Nothing to delete if line is empty
	if len(runes) == 0 {
		return
	}

	startPos := w.CursorPos().Rune
	if startPos >= len(runes) {
		return
	}
	runePos := startPos

	// Skip current word if we're in one
	for runePos < len(runes) && isWordRune(runes[runePos]) {
		runePos++
	}

	// Skip trailing non-word runes (whitespace etc)
	for runePos < len(runes) && !isWordRune(runes[runePos]) {
		runePos++
	}

	if runePos > startPos {
		e.killCapture(w, w.Buffer.DeleteTextCaptured(w.CursorPos().Line, startPos, runePos-startPos), true)
		// Clear ghost cursor and update ideal column after editing
		e.afterHorizontalMovement(w)
	}
}

// deleteToLineStart deletes from cursor to beginning of line.
func (e *Editor) deleteToLineStart() {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	if w.CursorPos().Rune > 0 {
		e.killCapture(w, w.Buffer.DeleteTextCaptured(w.CursorPos().Line, 0, w.CursorPos().Rune), false)
		w.SetCursorRune(0)
		// Clear ghost cursor and update ideal column after editing
		e.afterHorizontalMovement(w)
		e.ensureCursorVisible(w) // edit locks in the horizontal view
	}
}

// deleteToLineEnd deletes from cursor to end of line.
func (e *Editor) deleteToLineEnd() {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return
	}

	lineLen := e.getEffectiveLineLen(w.Buffer, w.CursorPos().Line)

	if w.CursorPos().Rune < lineLen {
		e.killCapture(w, w.Buffer.DeleteTextCaptured(w.CursorPos().Line, w.CursorPos().Rune, lineLen-w.CursorPos().Rune), true)
		// Clear ghost cursor and update ideal column after editing
		e.afterHorizontalMovement(w)
		e.ensureCursorVisible(w) // edit locks in the horizontal view
	}
}

// trimLineStart removes leading whitespace (spaces and tabs) from the
// current line, keeping the cursor on the same character where possible.
// Reports whether anything was removed.
func (e *Editor) trimLineStart() bool {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}

	line := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
	runes := []rune(line)
	n := 0
	for n < len(runes) && (runes[n] == ' ' || runes[n] == '\t') {
		n++
	}
	if n == 0 {
		return false
	}

	// Garland slides the viewport caret with the deletion (back by n if it was
	// past the indent, collapsing to 0 if it was within it) — no manual adjust.
	w.Buffer.DeleteText(w.CursorPos().Line, 0, n)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	return true
}

// trimLineEnd removes trailing whitespace (spaces and tabs) from the current
// line's content. The line terminator itself is never touched — the line
// ends where it did, just without trailing whitespace before the newline.
// Reports whether anything was removed.
func (e *Editor) trimLineEnd() bool {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}

	line := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
	runes := []rune(line)
	end := len(runes)
	for end > 0 && (runes[end-1] == ' ' || runes[end-1] == '\t') {
		end--
	}
	if end == len(runes) {
		return false
	}

	// Garland slides the viewport caret with the deletion (a caret inside the
	// trimmed run collapses to its start) — no manual adjust.
	w.Buffer.DeleteText(w.CursorPos().Line, end, len(runes)-end)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	return true
}

// copyBlock copies the marked block to the cursor position.
func (e *Editor) copyBlock() bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		e.ShowWarning("No active buffer")
		return false
	}

	startLine, startRune, endLine, endRune, exists := w.Buffer.GetBlockRange()
	if !exists {
		e.ShowWarning("No block marked")
		return false
	}

	// Get block content
	content := e.getBlockContent(w.Buffer, startLine, startRune, endLine, endRune)
	if content == "" {
		return false
	}

	// Insert at cursor position
	w.Buffer.InsertText(w.CursorPos().Line, w.CursorPos().Rune, content)

	// Move cursor past inserted content
	insertedRunes := []rune(content)
	newlines := 0
	lastNewlineIdx := -1
	for i, r := range insertedRunes {
		if r == '\n' {
			newlines++
			lastNewlineIdx = i
		}
	}

	if newlines > 0 {
		w.SetCursorLine(w.CursorPos().Line + newlines)
		w.SetCursorRune(len(insertedRunes) - lastNewlineIdx - 1)
	} else {
		w.SetCursorRune(w.CursorPos().Rune + len(insertedRunes))
	}

	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	e.ShowNotification("Block copied")
	return true
}

// deleteBlock deletes the marked block.
func (e *Editor) deleteBlock() bool {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		e.ShowWarning("No active buffer")
		return false
	}

	startLine, startRune, endLine, endRune, exists := w.Buffer.GetBlockRange()
	if !exists {
		e.ShowWarning("No block marked")
		return false
	}

	// Delete KILLING into the ring (emacs kill-region): the removed text and
	// its in-range user marks become a kill entry, yankable anywhere. The
	// block markers themselves stay behind (kill filter) and are cleared just
	// below. Garland slides the viewport's own caret with the edit — a caret
	// after the block moves back, a caret inside it collapses to the deletion
	// point, a caret before it stays put — so no hand-computed adjustment.
	// The delete and the marker-clear are two mutations: group them so one undo
	// reverses the whole block delete (marks restored with the text).
	w.Buffer.BeginUserCommand("block_delete")
	defer w.Buffer.EndUserCommand()
	cap := w.Buffer.DeleteTextRangeForKill(startLine, startRune, endLine, endRune)
	e.killCapture(w, cap, true)

	// Clear block marks
	w.Buffer.ClearBlockMarks()

	e.clampCursorToBuffer(w)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	e.ShowNotification("Block deleted")
	return true
}

// moveBlock moves the marked block to the cursor position.
func (e *Editor) moveBlock() bool {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		e.ShowWarning("No active buffer")
		return false
	}

	startLine, startRune, endLine, endRune, exists := w.Buffer.GetBlockRange()
	if !exists {
		e.ShowWarning("No block marked")
		return false
	}

	// Check if cursor is inside the block
	if (w.CursorPos().Line > startLine || (w.CursorPos().Line == startLine && w.CursorPos().Rune >= startRune)) &&
		(w.CursorPos().Line < endLine || (w.CursorPos().Line == endLine && w.CursorPos().Rune <= endRune)) {
		e.ShowWarning("Cannot move block to position within block")
		return false
	}

	// Delete the block CAPTURING its text and marks — the in-range user marks
	// plus the block markers themselves (same filtering decision as the kill
	// capture, block markers included because a move cannot duplicate them).
	// Garland slides the viewport's caret to the correct insertion point (moved
	// back if the caret was after the block, unchanged if before — a caret
	// inside was rejected above). Re-inserting the capture at the caret places
	// every mark at its offset in the moved text, so the block stays marked at
	// its destination; insertBefore=false keeps the caret at the start of the
	// inserted text.
	// The delete and the re-insert are two mutations that must undo as one step
	// (a half-undone move would lose or duplicate the block), so group them.
	w.Buffer.BeginUserCommand("block_move")
	defer w.Buffer.EndUserCommand()
	cap := w.Buffer.DeleteTextRangeForMove(startLine, startRune, endLine, endRune)
	if cap.Empty() {
		return false
	}
	ins := w.CursorPos()
	w.Buffer.InsertCaptured(ins.Line, ins.Rune, cap)

	e.clampCursorToBuffer(w)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	e.ShowNotification("Block moved")
	return true
}

// indentBlock indents all lines in the marked block.
func (e *Editor) indentBlock() bool {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		e.ShowWarning("No active buffer")
		return false
	}

	if !w.Buffer.HasBlockMarks() {
		e.ShowWarning("No block marked")
		return false
	}

	indentString := strings.Repeat(" ", e.tabSize(w))

	// The block is delimited by the _block_begin/_block_end decorations;
	// IndentBlock walks a garland cursor between them, anchored to positions
	// garland maintains rather than captured line numbers. The viewport's caret
	// slides with the inserted indent on its own — no read-back needed.
	w.Buffer.IndentBlock("_block_begin", "_block_end", indentString)

	e.clampCursorToBuffer(w)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	e.ShowNotification("Block indented")
	return true
}

// unindentBlock removes leading whitespace from all lines in the marked block.
func (e *Editor) unindentBlock() bool {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		e.ShowWarning("No active buffer")
		return false
	}

	if !w.Buffer.HasBlockMarks() {
		e.ShowWarning("No block marked")
		return false
	}

	// Decoration-anchored cursor walk (see indentBlock); the viewport's caret
	// slides left with the deleted indent on its own.
	w.Buffer.UnindentBlock("_block_begin", "_block_end", e.tabSize(w))

	e.clampCursorToBuffer(w)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	e.ShowNotification("Block unindented")
	return true
}

// getBlockContent extracts text from the marked block. Backed by garland's
// byte-range read so terminators are reproduced exactly (one per line), not
// doubled by line-by-line reconstruction.
func (e *Editor) getBlockContent(buf *buffer.Buffer, startLine, startRune, endLine, endRune int) string {
	return buf.GetTextRange(startLine, startRune, endLine, endRune)
}

// openFile opens a file in a new buffer viewport. On the real OS the file is
// opened through Garland's lazy warm-storage path (huge files are paged, not
// slurped); virtualized file systems read through the host callbacks.
func (e *Editor) openFile(filename string) bool {
	// A registered wiki scheme ("help:/start") opens a PAGE, not a literal
	// file: route it through the same resolver a followed link uses, so the
	// real page file (~/.mew/help/start.txt) loads and the viewport is rooted
	// in the wiki. Without this the name fell through to a plain OS open of
	// "help:/start", which found nothing and came up blank.
	if _, handled := e.openWikiScheme(strings.TrimSpace(filename), true); handled {
		return true
	}

	// A generated mew: surface ("mew:/buffers") is produced on demand and
	// navigated to in place, not opened as a file in a new viewport.
	if name := genSurfaceName(strings.TrimSpace(filename)); name != "" {
		return e.openGeneratedSurface(name)
	}

	buf, err := e.loadBuffer(filename)
	if err != nil {
		return false
	}

	// Create the main buffer viewport UNfocused, then show it in the focused tile
	// (replacing it) rather than spawning a new tile beside it.
	id := e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Type:            viewport.DocViewport,
		Buffer:          buf,
		Dock:            viewport.DockNone,
		Priority:        0,
		MinHeight:       1,
		ShowLineNumbers: true,
		TabSize:         e.Config.TabSize,
		ShowInvisibles:  e.Config.ShowInvisibles,
		ShowBidi:        e.Config.ShowBidi,
		ShowMarks:       e.Config.ShowMarks,
		OverwriteMode:   e.Config.OverwriteMode,
		ReadOnly:        e.Config.ReadOnly,
		ShowRuler:       e.Config.ShowColumnRuler,
		Scrollbar:       e.Config.Scrollbar,
	})
	e.showInFocusedTile(e.ViewportManager.GetViewport(id))

	e.RequestRender()
	return true
}

// showInFocusedTile makes w the content of the currently- (or most recently-)
// focused main tile, replacing what was there, instead of letting the main-focus
// hook (tilerFollowFocus) spawn a NEW tile beside it. This is what buffer_new /
// buffer_open_file want: open in the current pane, not add one. With nothing
// tiled yet it falls back to a plain focus, letting the hook seat the first tile
// as usual. The viewport formerly in the tile stays open (in the buffer list),
// just untiled.
func (e *Editor) showInFocusedTile(w *viewport.Viewport) {
	if w == nil {
		return
	}
	vp := e.ensureTiler()
	tile := vp.GetFocus()
	if tile == 0 {
		if ts := vp.Tiles(); len(ts) > 0 {
			tile = ts[0].Tile
		}
	}
	if tile != 0 {
		vp.Set(tile, w.ID) // the focused tile now shows the new viewport
	}
	e.ViewportManager.SetFocus(w.ID) // the hook finds the reseated tile — no split
}

// createNewBuffer creates a new empty buffer, shown in the focused tile.
func (e *Editor) createNewBuffer() {
	buf := e.lib.New()

	// Created UNfocused: showInFocusedTile reseats the focused tile to it, so
	// focusing it finds that tile rather than spawning a new one.
	id := e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Type:            viewport.DocViewport,
		Buffer:          buf,
		Dock:            viewport.DockNone,
		Priority:        0,
		MinHeight:       1,
		ShowLineNumbers: true,
		TabSize:         e.Config.TabSize,
		ShowInvisibles:  e.Config.ShowInvisibles,
		ShowBidi:        e.Config.ShowBidi,
		ShowMarks:       e.Config.ShowMarks,
		OverwriteMode:   e.Config.OverwriteMode,
		ReadOnly:        e.Config.ReadOnly,
		AutoIndent:      e.Config.AutoIndent,
		ShowRuler:       e.Config.ShowColumnRuler,
		Scrollbar:       e.Config.Scrollbar,
	})
	e.showInFocusedTile(e.ViewportManager.GetViewport(id))

	e.RequestRender()
}

// duplicateCurrentBuffer opens a new buffer viewport containing a copy of the
// current buffer's content. The duplicate is unnamed (no filename) so saving it
// can't overwrite the original.
func (e *Editor) duplicateCurrentBuffer() bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}
	buf := e.lib.NewFromString(w.Buffer.GetContent())

	e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Type:            viewport.DocViewport,
		Buffer:          buf,
		Dock:            viewport.DockNone,
		Priority:        0,
		MinHeight:       1,
		ShowLineNumbers: true,
		TabSize:         e.Config.TabSize,
		ShowInvisibles:  e.Config.ShowInvisibles,
		ShowBidi:        e.Config.ShowBidi,
		ShowMarks:       e.Config.ShowMarks,
		OverwriteMode:   e.Config.OverwriteMode,
		ReadOnly:        e.Config.ReadOnly,
		ShowRuler:       e.Config.ShowColumnRuler,
		Scrollbar:       e.Config.Scrollbar,
		SetFocus:        true,
	})

	e.RequestRender()
	return true
}

// cloneCurrentViewport opens a second viewport onto the focused viewport's buffer
// (the same *buffer.Buffer, not a copy), starting at the same caret and
// viewport. Both viewports then edit and scroll independently — each owns its
// caret cursor and viewport anchor, and garland keeps both in sync with edits
// made through either viewport.
func (e *Editor) cloneCurrentViewport() bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil || w.Type == viewport.PromptViewport {
		e.ShowWarning("No buffer to clone a viewport for")
		return false
	}

	srcPos := w.CursorPos()

	id := e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Type:            viewport.DocViewport,
		Buffer:          w.Buffer, // SAME buffer, not a copy
		Dock:            viewport.DockNone,
		Priority:        0,
		MinHeight:       1,
		ShowLineNumbers: w.ViewState.ShowLineNumbers,
		ProtectNewlines: w.ViewState.ProtectNewlines,
		TabSize:         w.ViewState.TabSize,
		ShowInvisibles:  w.ViewState.ShowInvisibles,
		ShowBidi:        w.ViewState.ShowBidi,
		ShowMarks:       w.ViewState.ShowMarks,
		OverwriteMode:   w.ViewState.OverwriteMode,
		ReadOnly:        w.ViewState.ReadOnly,
		ShowRuler:       w.ViewState.ShowRuler,
		// Inherit the scrollbar setting too, or the clone reserves no bar column
		// and comes up with no scrollbar beside the original that has one.
		Scrollbar: w.ViewState.Scrollbar,
		SetFocus:  true,
	})

	// Start the clone at the source's caret and viewport.
	if cw := e.ViewportManager.GetViewport(id); cw != nil {
		cw.SetCursorPos(srcPos)
		cw.SetViewTop(w.ViewState.ViewOffsetY)
	}

	e.RequestRender()
	return true
}

// writeBlock writes the marked block's text to a prompted-for file.
func (e *Editor) writeBlock() bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}
	startLine, startRune, endLine, endRune, exists := w.Buffer.GetBlockRange()
	if !exists {
		e.ShowWarning("No block marked")
		return false
	}
	content := e.getBlockContent(w.Buffer, startLine, startRune, endLine, endRune)
	if content == "" {
		e.ShowWarning("Block is empty")
		return false
	}

	e.PromptMgr.PromptForFilename("Write block to", "", func(accepted bool, _, filename string) {
		if !accepted || filename == "" {
			e.RequestRender()
			return
		}
		write := func() {
			if err := e.FS.WriteFile(filename, []byte(content)); err != nil {
				e.ShowError("Failed to write block: " + err.Error())
			} else {
				e.ShowNotification("Block written: " + filename)
			}
			e.RequestRender()
		}
		// A block write is never a whole-buffer save, so overwriting ANY
		// existing file (the buffer's own source included) gets a prompt.
		if e.fileExists(filename) {
			e.PromptMgr.PromptForConfirmation(fmt.Sprintf("13: OVERWRITE EXISTING FILE %s?", filename), false, func(accepted, confirmed bool) {
				if accepted && confirmed {
					write()
				} else {
					e.ShowNotification("Block write cancelled")
					e.RequestRender()
				}
			})
			return
		}
		write()
	})
	return true
}

// writeBufferCopy exports the whole buffer to a prompted-for file WITHOUT
// adopting it as the buffer's source (garland's SaveCopyTo, a streaming write):
// the buffer keeps working from its original source, and its filename, modified
// flag, and save history are all left untouched — this is not a save. The
// whole-buffer parallel to writeBlock. Like a block write, overwriting ANY
// existing file (the buffer's own source included) is confirmed first. Scars
// (data lost to placeholders) surface as buffer notices, same as a real save.
func (e *Editor) writeBufferCopy() bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}
	e.PromptMgr.PromptForFilename("Write buffer to", "", func(accepted bool, _, filename string) {
		if !accepted || filename == "" {
			e.RequestRender()
			return
		}
		write := func() {
			warnings, err := w.Buffer.SaveCopyTo(filename)
			for _, warn := range warnings {
				e.noteBuffer(w.Buffer, "save", warn, true)
			}
			if err != nil {
				e.ShowError("Failed to write buffer: " + err.Error())
			} else {
				e.ShowNotification("Buffer written: " + filename)
			}
			e.RequestRender()
		}
		// An export is never the buffer's own save, so overwriting ANY existing
		// file (the buffer's own source included) gets a prompt.
		if e.fileExists(filename) {
			e.PromptMgr.PromptForConfirmation(fmt.Sprintf("13: OVERWRITE EXISTING FILE %s?", filename), false, func(accepted, confirmed bool) {
				if accepted && confirmed {
					write()
				} else {
					e.ShowNotification("Buffer write cancelled")
					e.RequestRender()
				}
			})
			return
		}
		write()
	})
	return true
}

// insertFile inserts the contents of a file at the cursor position, as a single
// undo revision. Line endings are normalized to '\n' like paste.
func (e *Editor) insertFile(filename string) bool {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}
	data, err := e.FS.ReadFile(filename)
	if err != nil {
		e.ShowError("Failed to read file: " + err.Error())
		return false
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if text == "" {
		return true
	}

	w.Buffer.BeginUserCommand("buffer_insert_file")
	e.insertText(text)
	w.Buffer.EndUserCommand()
	w.TrackEdit()
	e.lastEditKill = false // an insert, not a kill: breaks delete accumulation
	return true
}

// promptBlockFromFile gates block_from_file: there must be a marked block and
// the caret must lie within it (or on either edge). Only when that holds does
// it prompt for the file to stream in over the block's contents. The caret gate
// is the same inclusive span block_move refuses to move a block into — demanded
// here rather than forbidden. Prompts are modal (focus is on the prompt viewport),
// so the block and caret cannot shift before the callback fires; blockFromFile
// re-reads the range defensively anyway.
func (e *Editor) promptBlockFromFile() bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		e.ShowWarning("No active buffer")
		return false
	}
	if !w.Buffer.HasBlockMarks() {
		e.ShowWarning("No block marked")
		return false
	}
	if !e.caretWithinBlock(w) {
		e.ShowWarning("Caret must be within the block")
		return false
	}

	e.PromptMgr.PromptForFilename("Stream file into block", "", func(accepted bool, _, filename string) {
		if accepted && filename != "" {
			e.blockFromFile(filename)
		}
		e.RequestRender()
	})
	return true
}

// caretWithinBlock reports whether the focused viewport's caret lies within the
// marked block or on either of its edges — the inclusive span the block
// commands that TARGET the block (block_from_file, os_paste's replace mode)
// demand. False when no block is marked.
func (e *Editor) caretWithinBlock(w *viewport.Viewport) bool {
	startLine, startRune, endLine, endRune, exists := w.Buffer.GetBlockRange()
	if !exists {
		return false
	}
	pos := w.CursorPos()
	return (pos.Line > startLine || (pos.Line == startLine && pos.Rune >= startRune)) &&
		(pos.Line < endLine || (pos.Line == endLine && pos.Rune <= endRune))
}

// blockFromFile replaces the marked block's contents with the contents of
// filename, streamed in at the block's location (see replaceBlockText for the
// replace semantics). The caller (promptBlockFromFile) has already verified
// there is a block and the caret is within it.
func (e *Editor) blockFromFile(filename string) bool {
	data, err := e.FS.ReadFile(filename)
	if err != nil {
		e.ShowError("Failed to read file: " + err.Error())
		return false
	}
	if !e.replaceBlockText(normalizeLineEndings(string(data)), "block_from_file") {
		return false
	}
	e.ShowNotification("Block replaced from " + filename)
	return true
}

// normalizeLineEndings converts CRLF and lone CR to '\n', the same
// normalization paste and buffer_insert_file apply.
func normalizeLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// replaceBlockText replaces the marked block's contents with text (already
// newline-normalized) and leaves the newly inserted text marked as the block,
// so it is immediately ready for another block op. The delete, insert, and
// re-mark are grouped into one user command (cmdName) so a single undo
// reverses the whole replace.
func (e *Editor) replaceBlockText(text, cmdName string) bool {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}
	startLine, startRune, endLine, endRune, exists := w.Buffer.GetBlockRange()
	if !exists {
		e.ShowWarning("No block marked")
		return false
	}

	// Delete the old block content, place the text at its location, and mark
	// the inserted text as the new block. All one undo step. The old markers
	// collapse to the deletion point on delete; we clear them and re-set both to
	// absolute positions around the inserted text, so the block surrounds the
	// stream regardless of insert gravity. insertText advances the viewport caret
	// to the end of what it inserts, which is the block's new end.
	w.Buffer.BeginUserCommand(cmdName)
	w.Buffer.DeleteTextRange(startLine, startRune, endLine, endRune)
	w.Buffer.ClearBlockMarks()
	w.SetCursorPos(viewport.Position{Line: startLine, Rune: startRune})
	if text != "" {
		e.insertText(text)
	}
	endPos := w.CursorPos()
	w.Buffer.SetMark("_block_begin", startLine, startRune)
	w.Buffer.SetMark("_block_end", endPos.Line, endPos.Rune)
	w.Buffer.EndUserCommand()

	w.TrackEdit()
	e.lastEditKill = false // an insert, not a kill: breaks delete accumulation

	e.clampCursorToBuffer(w)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	return true
}

// closeCurrentBuffer closes the current buffer viewport.
func (e *Editor) closeCurrentBuffer() bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Type == viewport.PromptViewport {
		return false
	}

	// Check for changes that would be lost. The viewport's active buffer is
	// always at stake. Its stacked buffers are only at stake when the WINDOW
	// itself will close — with a non-empty graveyard, viewport_close instead
	// resurrects the most recent burial into this viewport, so the history and
	// graveyard survive intact.
	resurrecting := len(w.GraveyardBuffers()) > 0
	var atRisk []*buffer.Buffer
	if w.Buffer != nil && w.Buffer.IsModified() {
		atRisk = append(atRisk, w.Buffer)
	}
	if !resurrecting {
		for _, b := range w.StackedBuffers() {
			if b != w.Buffer && b.IsModified() && !e.bufferReferencedElsewhere(b, w) {
				atRisk = append(atRisk, b)
			}
		}
	}
	if len(atRisk) > 0 {
		// Get the name for the prompt
		viewportName := atRisk[0].GetFilename()
		if viewportName == "" {
			viewportName = "Untitled"
		}
		if len(atRisk) > 1 {
			viewportName = fmt.Sprintf("%s (+%d more in history)", viewportName, len(atRisk)-1)
		}

		// Store viewport ID to close later
		viewportID := w.ID

		// Prompt for confirmation using PromptManager
		e.PromptMgr.PromptForConfirmation(fmt.Sprintf("04: LOSE CHANGES TO %s?", viewportName), true, func(accepted bool, confirmed bool) {
			if accepted && confirmed {
				// User confirmed - close the buffer
				e.finishCloseBuffer(viewportID)
			} else {
				e.ShowNotification("Close cancelled")
			}
			e.RequestRender()
		})
		return true
	}

	// Not modified - close directly
	return e.finishCloseBuffer(w.ID)
}

// bufferDisplayName is a short human name for a buffer: its filename's base,
// or "Untitled".
func bufferDisplayName(b *buffer.Buffer) string {
	if b != nil {
		if fn := b.GetFilename(); fn != "" {
			return filepath.Base(fn)
		}
	}
	return "Untitled"
}

// finishCloseBuffer performs the actual buffer close.
func (e *Editor) finishCloseBuffer(viewportID string) bool {
	// Resurrection first: with buried bindings waiting, closing the buffer
	// does NOT close the viewport — the most recently buried binding surfaces
	// as the viewport's current one (full caret/scroll state), and the closed
	// buffer is retired only if nothing else still holds it open.
	if w := e.ViewportManager.GetViewport(viewportID); w != nil {
		closed := w.Buffer
		if w.ResurrectLastBuried() {
			e.unburyEverywhere(w.Buffer)
			if closed != nil {
				stillOpen := false
				for _, b := range e.openDocViewports() {
					if b == closed {
						stillOpen = true
						break
					}
				}
				if !stillOpen {
					e.forgetBufferSafety(closed)
				}
			}
			e.ensureCursorVisible(w)
			e.ShowNotification("Closed; resurfaced " + bufferDisplayName(w.Buffer))
			e.RequestRender()
			return true
		}
	}

	// Get all main buffers
	mainBuffers := e.contentViewports()
	if len(mainBuffers) <= 1 {
		// Last buffer - exit instead
		e.Running = false
		return true
	}

	closing := e.ViewportManager.GetViewport(viewportID)

	// Remove the viewport, and dismiss the tiler tile that held it.
	e.ViewportManager.RemoveViewport(viewportID)
	e.dismissTileFor(viewportID)

	// Drop safety state (mew lock, notices) when no other viewport still holds
	// this buffer open — actively or stacked in a nav history (viewport_clone
	// can share one buffer; link-follow histories reference buffers too).
	if closing != nil && closing.Buffer != nil {
		shared := false
		for _, b := range e.openDocViewports() {
			if b == closing.Buffer {
				shared = true
				break
			}
		}
		if !shared {
			e.forgetBufferSafety(closing.Buffer)
		}
	}
	e.RequestRender()
	return true
}

// viewportTiled reports whether a main-area tile currently references the
// viewport id — i.e. it is shown on screen. False when there is no tiler.
func (e *Editor) viewportTiled(id string) bool {
	if e.tiler == nil {
		return false
	}
	for _, b := range e.tiler.Tiles() {
		if b.Ref == id {
			return true
		}
	}
	return false
}

// cycleBuffer brings a NON-VISIBLE buffer into the focused pane. buffer_next /
// buffer_prior walk the content viewports in `direction` and land on the first
// one NOT currently shown in any tile — a buffer visible in a pane is reached
// with viewport_next/prior instead, so the two commands partition the set with
// no overlap. adoptFocusInPlace reseats the focused tile onto the buffer, rather
// than splitting a new one. Reports false when nothing is hidden to bring in.
func (e *Editor) cycleBuffer(direction int) bool {
	mains := e.contentViewports()
	if len(mains) <= 1 {
		return false
	}

	currentID := ""
	if w := e.ViewportManager.GetFocusedViewport(); w != nil {
		currentID = w.ID
	}
	currentIndex := -1
	for i, w := range mains {
		if w.ID == currentID {
			currentIndex = i
			break
		}
	}
	if currentIndex == -1 {
		currentIndex = 0
	}

	n := len(mains)
	for step := 1; step <= n; step++ {
		w := mains[((currentIndex+direction*step)%n+n)%n]
		if w.ID == currentID || e.viewportTiled(w.ID) {
			continue // self or an on-screen pane — skip
		}
		e.adoptFocusInPlace = true
		e.ViewportManager.SetFocus(w.ID)
		e.adoptFocusInPlace = false
		e.RequestRender()
		return true
	}
	return false
}

// contentViewports returns every content viewport — documents and tool surfaces
// (help, listings) — but not prompts or chrome (the modebar). These are the
// viewports the data-safety and navigation enumerations act on (buffer close,
// save-all, DEADCAT, nav-history liveness, buffer cycling).
func (e *Editor) contentViewports() []*viewport.Viewport {
	var result []*viewport.Viewport
	for _, w := range e.ViewportManager.AllViewports() {
		if w.FocusEligible() {
			result = append(result, w)
		}
	}
	return result
}

// ensureCursorVisible scrolls the viewport so the cursor is visible both
// vertically and horizontally. Horizontal following "locks in" the column — it
// is used after horizontal movement, edits, and other actions, but NOT after
// bare vertical movement (see ensureCursorVisibleVertical), so a run of up/down
// with an active ghost cursor keeps the horizontal view (and any manual scroll)
// stable until a horizontal/locking action.
func (e *Editor) ensureCursorVisible(w *viewport.Viewport) {
	e.ensureCursorVisibleVertical(w)
	e.ensureCursorVisibleHorizontal(w)
}

// ensureCursorVisibleVertical scrolls the viewport vertically so the cursor's
// line is visible. It never changes the horizontal offset, so it does not
// disturb a manual horizontal scroll or force the ghost column on-screen.
//
// This is the COMMAND path: a command asking to show the caret is, by
// definition, done browsing a detached (free) scroll, so it RE-ENGAGES caret
// following — clearing any ScrollDetached parked by the mouse wheel or a
// scroll_* command. (The per-frame render follow uses renderFollowCaret, which
// instead respects a detached scroll.) Cursor-movement and edit leaf commands
// reach here through their own ensureCursorVisible / ...Vertical calls, so the
// re-engagement lives in their definitions, not in any command-name analysis.
func (e *Editor) ensureCursorVisibleVertical(w *viewport.Viewport) {
	w.ViewState.ScrollDetached = false
	e.clampViewToCaret(w)
	// Vertical movement does not scroll horizontally, but the caret may have
	// landed on (or left) a line whose end needs the phantom column, which is a
	// one-cell adjustment rather than a scroll.
	e.reconcilePhantomColumn(w)
}

// clampViewToCaret scrolls the viewport vertically the minimum needed to bring
// the caret's line on-screen, WITHOUT touching ScrollDetached. Both the command
// path (ensureCursorVisibleVertical, which first re-engages following) and the
// per-frame render follow (renderFollowCaret, which first honors a detached
// scroll) share it.
func (e *Editor) clampViewToCaret(w *viewport.Viewport) {
	if w.ContentHeight <= 0 {
		w.ContentHeight = 20 // Default
	}
	// Absorb any slide the viewport anchor took from edits (e.g. lines inserted
	// above the top by another viewport on the same buffer), then scroll only as
	// far as needed to keep the caret visible. Both writes go through
	// SetViewTop so the anchor stays in lockstep with the painting offset.
	w.RefreshViewTop()
	top := w.ViewState.ViewOffsetY
	if w.CursorPos().Line < top {
		w.SetViewTop(w.CursorPos().Line)
	} else if w.CursorPos().Line >= top+w.ContentHeight {
		w.SetViewTop(w.CursorPos().Line - w.ContentHeight + 1)
	}
}

// renderFollowCaret is the per-frame vertical caret follow. Unlike the command
// path it RESPECTS a free scroll: while the focused viewport is ScrollDetached
// (mouse wheel / scroll_* command), the viewport is left exactly where the user
// parked it — the caret may sit off-screen, marked by the cursorOffScreen
// indicator — until a cursor-movement or edit command re-engages following.
func (e *Editor) renderFollowCaret(w *viewport.Viewport) {
	if w.ViewState.ScrollDetached {
		return
	}
	e.clampViewToCaret(w)
}

// scrollViewHorizontal moves the view one step in a VISUAL direction: dir -1 is
// leftward on screen, +1 rightward, whichever way the text runs.
//
// ViewOffsetX counts columns scrolled past the NEAR edge, and which edge that
// is depends on direction: the left under ltr, the reading start on the right
// under rtl. So a visual direction maps to opposite signs in the two modes —
// under rtl, moving the view LEFT (further into the reading tail) RAISES the
// offset. It reports whether the view actually moved; already at the near edge,
// it cannot.
func (e *Editor) scrollViewHorizontal(w *viewport.Viewport, dir int) bool {
	if w == nil || dir == 0 {
		return false
	}
	step := 8 * dir
	if e.winRTL(w) {
		step = -step
	}
	off := w.ViewState.ViewOffsetX + step
	if off < 0 {
		off = 0
	}
	if off == w.ViewState.ViewOffsetX {
		return false
	}
	w.ViewState.ViewOffsetX = off
	e.RequestRender()
	return true
}

// scrollViewByLines parks the focused-content viewport delta lines away from
// its current top (negative = toward the start), detaching it from the caret
// like the mouse wheel: the caret stays put and the per-frame follow will not
// snap the view back until a cursor-movement or edit command re-engages it.
func (e *Editor) scrollViewByLines(w *viewport.Viewport, delta int) {
	if w == nil {
		return
	}
	w.RefreshViewTop() // base the relative move on the actually-painted top
	e.scrollViewTo(w, w.ViewState.ViewOffsetY+delta)
}

// scrollViewTo parks the viewport at an absolute top line, detaching it from
// caret-follow. Shared by the mouse wheel, the drag-autoscroll, mew's own
// scrollbar, every scroll_* command and a graphical host's scroll_viewport, so
// the limit below is the ONE place the bottom of the scroll range is decided.
//
// That limit leaves exactly one row past the end of the document on screen — the
// one the gutter marks "~" (MaxScrollTop): the text does not get to drift off the
// top leaving an empty window behind it, and more than one such row appears only
// for a document too short to scroll at all.
func (e *Editor) scrollViewTo(w *viewport.Viewport, top int) {
	if w == nil || w.Buffer == nil {
		return
	}
	if max := viewport.MaxScrollTop(w.ContentHeight, w.Buffer.GetLineCount()); top > max {
		top = max
	}
	if top < 0 {
		top = 0
	}
	w.ViewState.ScrollDetached = true
	w.SetViewTop(top)
	e.RequestRender()
}

// ensureCursorVisibleHorizontal scrolls the viewport horizontally so the cursor
// (or its ghost column) is visible. ViewOffsetX is a VISUAL-column offset (the
// renderer slices and positions by visual column), so decisions use the cursor's
// visual column, not its rune index — otherwise tabs and control chars (visual
// width > 1) let the cursor drift off the edge.
func (e *Editor) ensureCursorVisibleHorizontal(w *viewport.Viewport) {
	if w.ContentWidth <= 0 {
		w.ContentWidth = 80 // Default
	}
	targetCol := w.CursorPos().Rune
	vw := -1 // total visual width, needed for reading-space scroll under rtl
	if w.Buffer != nil {
		tabSize := e.tabSize(w)
		line := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
		// Browse-mode buttons: visibility runs in DISPLAY space — the line as
		// painted, with the caret mapped onto it (a caret inside a button
		// parks on the button). Identity when the line has no buttons.
		line, curRune := e.displayCaretLine(w, line, w.CursorPos().Rune)
		targetCol = e.caretVisualColumn(w, line, curRune, tabSize)
		if e.winRTL(w) {
			// Content only: the terminator's marker cells paint into the left
			// padding under rtl (see prepareLineForDisplay), so counting them
			// here would put the reading-space scroll a cell out of step with
			// the caret column, which never counts them either.
			vw = e.lineVisualWidth(w, line, tabSize)
		}
		if targetCol < 0 && !e.winRTL(w) && !e.caretWantsPhantom(w) {
			targetCol = 0
		}
	}
	// When a ghost cursor is active the user's intended column is the ghost's
	// column (past the line's end), so keep that visible instead. Under RTL
	// the ghost column is already a READING column (see afterVerticalMovement),
	// so it feeds the reading-space branch below directly.
	ghostReadingRTL := false
	if w.HasGhostCursor {
		targetCol = w.GhostCursorVisualColumn
		ghostReadingRTL = e.winRTL(w)
	}

	// A double-width (heading) caret line shows half as many columns, and the
	// renderer reads the horizontal scroll at one cell per two positions. So the
	// visibility test runs in that halved display space, and the resulting
	// offset is scaled back to the stored (normal-column) ViewOffsetX.
	dw := false
	if w.Buffer != nil {
		_, dw = e.lineDisplaySpans(w, w.CursorPos().Line)
	}
	effWidth := w.ContentWidth
	dispOff := w.ViewState.ViewOffsetX
	if dw {
		effWidth /= 2
		if effWidth < 1 {
			effWidth = 1
		}
		dispOff /= 2
	}
	// The PHANTOM COLUMN: a caret whose insertion point lies one cell past the
	// line's leading edge — the reading end of an RTL fragment starting an LTR
	// line (one cell LEFT of visual column 0), or an insertion point right of
	// an LTR run at an RTL line's reading start (one cell RIGHT of it). Neither
	// column exists in the content, so the follow may scroll ONE further than
	// the content itself allows: ViewOffsetX -1, a state where the first
	// visible column is 0 instead of 1 and the renderer shows a plain space
	// there for the caret to occupy (see prepareLineForDisplay / rtlView).
	// Guarded on the caret's logical rune being positive — there must be
	// content the caret is after — per the shape of the problem: rune positive
	// but visual column at the leading edge. Under direction=rtl a mirrored
	// line-number gutter already gives that caret a cell to dip into
	// (positionCursor), so the phantom only steps in without one.
	phantom := false

	setOff := func(v int) {
		if v < 0 {
			if phantom && v == -1 {
				// The one legal negative offset: the phantom column.
			} else {
				v = 0
			}
		}
		if dw {
			v *= 2
		}
		w.ViewState.ViewOffsetX = v
	}

	// direction=rtl: the view is right-anchored, so visibility is decided in
	// READING columns — the caret's distance back from the line's reading
	// start (its rightmost visual cell). ViewOffsetX counts reading columns
	// scrolled past, matching the renderer's right-anchored viewport.
	if e.winRTL(w) && vw >= 0 {
		reading := vw - targetCol
		if ghostReadingRTL {
			reading = targetCol // already a reading column
		}
		if reading < 0 {
			reading = 0
		}
		if !w.HasGhostCursor && e.caretWantsPhantom(w) {
			phantom = true
			reading = -1
		}
		if reading < dispOff {
			setOff(reading)
		} else if reading > dispOff+effWidth-1 {
			setOff(reading - effWidth + 1)
		}
		return
	}

	if !w.HasGhostCursor && e.caretWantsPhantom(w) {
		phantom = true // targetCol stays -1: the window checks scroll to it
	}
	if targetCol < dispOff {
		setOff(targetCol)
	} else if targetCol >= dispOff+effWidth {
		setOff(targetCol - effWidth + 1)
	}
}

// caretWantsPhantom reports whether the caret has earned the phantom column:
// its insertion point lies one cell PAST the line's leading edge while its
// logical rune is positive — there is content it stands after. Under an LTR
// base that is visual column -1 (the reading end of an RTL run starting the
// line); under RTL it is one cell past the content's rightmost cell, and the
// caret must additionally take an LTR run's direction — a caret on an RTL rune
// at the reading start marks the cell the NEXT character will occupy and keeps
// its clamp (the same split the gutter dip and the bar nudge use, via
// bidi.RTLAt).
//
// The RTL measure is the CONTENT's visual width, deliberately excluding the
// terminator marker cells showInvisibles appends: the caret's own column never
// counts them, so measuring against the marker-inflated width read as "one cell
// in from the edge" and silently withheld the phantom whenever invisibles were
// on.
//
// This is pure CARET geometry: a ghost column does not enter into it. Vertical
// movement sets a ghost even when the caret lands exactly at a line's end (the
// remembered column and the caret coincide there), so excluding ghosts here
// withheld the phantom from every arrival by arrow key. Where the ghost really
// matters is the horizontal FOLLOW, whose scroll target is the ghost rather
// than the caret; that is gated separately.
func (e *Editor) caretWantsPhantom(w *viewport.Viewport) bool {
	if w == nil || w.Buffer == nil {
		return false
	}
	// An insertion point past the leading edge needs content it stands AFTER,
	// so rune 0 cannot want the phantom for that reason. The bar case below is
	// the exception: it is about a bar needing the cell beside the character it
	// sits ON, which is exactly the line's first rune.
	afterContent := w.CursorPos().Rune > 0
	line := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
	dispLine, curRune := e.displayCaretLine(w, line, w.CursorPos().Rune)
	tabSize := e.tabSize(w)
	col := e.caretVisualColumn(w, dispLine, curRune, tabSize)
	if !e.winRTL(w) {
		return afterContent && col == -1
	}
	vwContent := e.lineVisualWidth(w, dispLine, tabSize)
	caretRTL := bidi.RTLAt([]rune(dispLine), curRune, true)
	if col == vwContent {
		return afterContent && !caretRTL
	}
	// One more RTL case, for the BAR shapes alone. A bar draws on the LEFT edge
	// of the cell it is addressed to, so on the reading start's RTL character
	// its insertion point — that character's right edge — is one cell further
	// out. A mirrored gutter supplies that cell (positionCursor dips into it);
	// with line numbers off there is nothing past the content, and the bar
	// clamps back onto the character, landing exactly where the caret one rune
	// along would. The phantom gives it a cell of its own.
	if caretRTL && col == vwContent-1 && !w.ViewState.ShowLineNumbers {
		if shape := e.cursorStyleFor(w); shape == 5 || shape == 6 {
			return true
		}
	}
	return false
}

// reconcilePhantomColumn TAKES the phantom column for the caret as it now
// stands, WITHOUT otherwise disturbing the horizontal scroll — it only ever
// moves from 0 to -1, never touching a scrolled view.
//
// It does not release: this is caret VISIBILITY, not a snap. Whether a caret
// "wants" the phantom flips as the text around it changes — a trailing space is
// a neutral that resolves to the base direction, so typing one at the end of an
// LTR run in an RTL line reclassifies the caret for exactly one keystroke — and
// releasing on that would jerk the whole line back and forth by a column as you
// type. The phantom is given up only when some action genuinely requires a
// different scroll, which the follow's range checks then compute.
//
// Vertical movement deliberately leaves the horizontal offset alone (see
// ensureCursorVisibleVertical), so arriving at a line whose end sits at the
// leading edge would otherwise leave the caret pinned on the character it
// stands after — or, under rtl with a gutter, dipped onto a line-number digit —
// until some later command happened to run the full horizontal follow.
func (e *Editor) reconcilePhantomColumn(w *viewport.Viewport) {
	if w == nil {
		return
	}
	phantomOff := -1
	if w.Buffer != nil {
		if _, dw := e.lineDisplaySpans(w, w.CursorPos().Line); dw {
			phantomOff = -2 // a doubled row stores twice the display offset
		}
	}
	if w.ViewState.ViewOffsetX == 0 && e.caretWantsPhantom(w) {
		w.ViewState.ViewOffsetX = phantomOff
	}
}

// setupKeyMappingsFromConfig sets up key mappings from the loaded config file.
// All mappings come from the config file (default or user-customized).
// This matches the TypeScript version's architecture where mappings are
// defined in the [mappings:mew] section of the config file.
func (e *Editor) setupKeyMappingsFromConfig() {
	kp := e.KeyProcessor

	// Load all key mappings from config file
	// The config file's generateDefaultConfig() contains all the standard mew mappings
	for key, command := range e.LoadedConfig.Mappings {
		kp.MapKey(key, command)
	}

	// Mirror provenance for the active keymap so key badges can attribute
	// bindings and tie-break on "last configured". A pristine (config-less)
	// keymap leaves this empty — every key resolves as a built-in.
	e.mappingOrigins = make(map[string]config.MappingOrigin, len(e.LoadedConfig.MappingOrigins))
	for key, o := range e.LoadedConfig.MappingOrigins {
		e.mappingOrigins[key] = o
	}
	e.remapPrec = maxPrecedence(e.mappingOrigins)
}

// maxPrecedence returns the highest precedence in an origins map (0 when empty),
// the baseline a runtime remap counts up from so it outranks every config
// binding.
func maxPrecedence(origins map[string]config.MappingOrigin) int {
	max := 0
	for _, o := range origins {
		if o.Precedence > max {
			max = o.Precedence
		}
	}
	return max
}

// RequestRender requests a render with debouncing.
func (e *Editor) RequestRender() {
	e.renderRequested.Store(true)
}

// performRender performs the actual render.
func (e *Editor) performRender() {
	// Serialize renders: this runs on both the main loop and the renderer's
	// resize goroutine, and its editor-level work below is not otherwise guarded.
	e.renderMu.Lock()
	defer e.renderMu.Unlock()

	// Resolve each main buffer's per-viewport options against its current grammar
	// (base [options] overlaid by [options.<grammar>]) before any layout or
	// paint reads ViewState, so direction/gutter/etc. are current this frame.
	for _, w := range e.ViewportManager.AllViewports() {
		e.reconcileGrammarOptions(w)
	}
	// Focused-scoped options (modebar, macOptionKeys, key mappings) follow the
	// focused viewport's grammar/class/type.
	e.reconcileFocusedOptions()

	// Push the focused viewport's read-only state to the host on transitions
	// (a host greys out its Edit-menu Cut for a read-only buffer).
	e.notifyEditState()
	// Push whether the built-in help viewport is open (a host syncs a "Quick
	// Help" menu checkmark to it).
	e.notifyHelpState()

	// Follow the cursor VERTICALLY only. Horizontal following is a "lock-in"
	// action performed by cursor/edit commands, not by rendering, so a manual
	// horizontal scroll (scroll_left/right) and the ghost column during vertical
	// navigation are not snapped back on every render.
	focusedViewport := e.ViewportManager.GetFocusedViewport()
	if focusedViewport != nil {
		e.renderFollowCaret(focusedViewport)
	}

	// Flip the modebar logo (M_ vs _M) to the text direction at the focused
	// caret, so the user can see which way the next keypress will move.
	if focusedViewport != nil && focusedViewport.Buffer != nil {
		lineText := strings.TrimRight(focusedViewport.Buffer.GetLine(focusedViewport.CursorPos().Line), "\n\r")
		e.Modebar.SetLogoRTL(bidi.RTLAt([]rune(lineText), focusedViewport.CursorPos().Rune, e.winRTL(focusedViewport)))
	}

	// Fill the modebar's context slot — computed for the same viewport the
	// modebar reads context from. Priority: the zero-width character backspace
	// would delete at the caret (a combining diacritic or invisible control —
	// the one thing on screen the user cannot see), then the outline breadcrumb
	// (the enclosing function/section chain), then the spawn placeholder.
	if cw := focusedViewport; cw != nil {
		if cw.Type == viewport.PromptViewport {
			cw = e.ViewportManager.GetLastMainViewport()
		}
		if cw != nil {
			if btn := e.focusedLinkButton(cw); btn != nil {
				// A focused link button shows its destination — the one thing
				// the button's resolved title hides.
				cw.Context = btn.Target
			} else if mark := e.caretMarkContext(cw); mark != "" {
				cw.Context = mark
			} else if crumb := e.outlineContext(cw); crumb != "" {
				cw.Context = crumb
			} else {
				cw.Context = cw.SpawnContext
			}
		}
	}

	// Calculate layout
	layout := e.LayoutManager.CalculateLayout(e.Renderer.Width, e.Renderer.Height)

	// Drive the main viewport's geometry from the ifitfits tiler (a minimal
	// geometry-only integration: the main document paints in its resolved tile).
	e.applyTilerGeometry(&layout)

	// Render
	e.Renderer.Render(layout)

	// Retain the main-area tiles for mouse hit-testing. Render filled each
	// entry's per-tile content bounds, so a click can be resolved to the exact
	// tile — necessary when one viewport is shown in several tiles.
	e.mainTiles = layout.MainLayout

	// The frame's viewport geometry is now set: publish the focused viewport's
	// editable rectangle to the host so a graphical pointer shows the I-beam
	// over text and the arrow over chrome, resolved locally from the pointer
	// cell (no per-motion round trip). Pushed only when the rectangle changes.
	e.notifyPointerRegion()
	e.notifyScrollbarRegions()
	e.notifyTerminalSurfaces()

	e.lastRenderTime = time.Now()
	e.renderRequested.Store(false)

	// debug_screen: snapshot the exact frame just painted (same layout) to its
	// armed box:/// target. CaptureFrame takes the renderer's own mutex (free
	// now that Render has returned); we hold renderMu, which is fine. Done after
	// clearing renderRequested so the "Wrote" toast's own RequestRender sticks
	// and schedules the frame that shows it.
	if e.pendingScreenCapture != "" {
		target := e.pendingScreenCapture
		e.pendingScreenCapture = ""
		if err := e.mew.WriteFile(target, []byte(e.Renderer.CaptureFrame(layout))); err != nil {
			e.ShowError("debug_screen: " + err.Error())
		} else {
			e.ShowNotification("Wrote " + target)
		}
	}

	// flipBidiForHost=auto: probe the terminal the first time RTL content
	// reaches the screen; resolve a probe whose reply never came.
	e.maybeSendBidiProbe()
	e.checkBidiProbeTimeout()

	// Show/hide the Kitty force_ltr nudge based on what this frame painted.
	e.updateNiqqudNudge()
}

// applyTilerGeometry drives the painted main viewport's geometry from the
// ifitfits tiler. This is a deliberately minimal, geometry-only integration:
// the tiler holds a "main" tile (split against a "blank" tile) whose resolved
// cell rect becomes the main viewport's on-screen box, proving the tiler's
// geometry flows into mew's window painting. Nothing else (focus, input, the
// blank tile's contents) is wired yet.
//
// The tiler works in the same cell units the main editor uses. Its workspace is
// the full main-area rectangle (screen width × the negotiated main height). The
// "main" tile's rect maps to the viewport two ways, because mew models the two
// axes differently:
//   - Horizontal: the ViewportLayout has no X/width, so the tile's X/width go on
//     the viewport itself (FrameX/FrameWidth) and the renderer honors them.
//   - Vertical: the ViewportLayout already carries per-viewport Y/height, and the
//     layout's MainHeight is exactly the tiler's workspace height, so the tile's
//     Y/height replace this entry's Y/height. That flows through the normal
//     render pass into ContentHeight, so paging/scroll/mouse all see the real
//     window height — and because it is per-viewport, a prompt buffer (which
//     sets its own ContentHeight) is unaffected.
//
// applyTilerGeometry replaces the layout's main-area entries with ones derived
// from the ifitfits tiler: the tiler owns the arrangement of the non-docked area,
// so its workspace is that area (screen width × the negotiated main height), and
// each visible tile becomes a ViewportLayout drawing the mew viewport its ref
// names. A tile whose ref names no live viewport (an empty tile) is skipped — the
// freshly cleared back buffer shows through as blank. Chrome (docked viewports)
// is untouched; the renderer paints tiles and docked viewports through the same
// agnostic path.
//
// Horizontal and vertical extent both ride on the layout ENTRY (the tile):
// FrameX/FrameWidth and Y/Height are per-tile, so a viewport referenced by
// several tiles (tiles↔viewports is many-to-many, e.g. after viewport_split
// clones a ref) gets a distinct frame in each. The renderer applies each tile's
// frame to the viewport just before painting it.
func (e *Editor) applyTilerGeometry(layout *viewport.Layout) {
	if layout.MainHeight <= 0 || e.Renderer.Width <= 0 {
		layout.MainLayout = nil
		return
	}
	vp := e.ensureTiler()
	vp.SetWorkspace(float64(e.Renderer.Width), float64(layout.MainHeight))

	focusedTile := vp.GetFocus()
	mainTop := layout.TopHeight
	mains := make([]viewport.ViewportLayout, 0, 4)
	for _, b := range vp.Tiles() {
		w := e.ViewportManager.GetViewport(b.Ref)
		if w == nil {
			continue // empty/blank tile
		}
		// Snap each tile to integer cell edges by rounding its LEFT and RIGHT
		// (and TOP/BOTTOM) edges, then taking the span. Rounding edges rather
		// than truncating width keeps adjacent tiles flush and the rightmost/
		// bottommost tile reaching the workspace edge, so a fractional split
		// (e.g. an 81-column area halved) never leaves a one-cell gap.
		x0 := int(math.Round(b.Rect.X))
		x1 := int(math.Round(b.Rect.X + b.Rect.W))
		y0 := int(math.Round(b.Rect.Y))
		y1 := int(math.Round(b.Rect.Y + b.Rect.H))
		// Geometry rides on the tile (this layout entry): a viewport shown in
		// several tiles gets a distinct frame per tile, and the renderer applies
		// each just before painting it. The viewport's own FrameX/FrameWidth are
		// also set (last tile wins) for the single-tile mouse hit-test path.
		w.FrameX = x0
		w.FrameWidth = x1 - x0
		mains = append(mains, viewport.ViewportLayout{
			Viewport:   w,
			Y:          mainTop + y0,
			Height:     y1 - y0,
			FrameX:     x0,
			FrameWidth: x1 - x0,
			Focused:    b.Tile == focusedTile,
			TileHandle: uint64(b.Tile),
		})
	}
	layout.MainLayout = mains
}

// Run starts the editor with an optional filename.
func (e *Editor) Run(filename string) error {
	// Create a buffer
	var buf *buffer.Buffer
	if filename != "" {
		loaded, err := e.loadBuffer(filename)
		if err != nil {
			// File doesn't exist, create empty buffer with the name
			buf = e.lib.New()
			buf.SetFilename(filename)
		} else {
			buf = loaded
		}
	} else {
		buf = e.lib.New()
	}

	_, err := e.run(buf)
	return err
}

// loadBuffer loads a file into a buffer: through Garland's own lazy
// warm-storage path on the real OS, or through the host's FileSystem bridged
// into garland when virtualized (so host buffers get the same save engine,
// history preservation, and revert). Opening also arms the buffer's safety
// net: automatic backups, and an editing lock — emacs-interoperable when
// git hygiene allows it, mew-native otherwise.
func (e *Editor) loadBuffer(filename string) (*buffer.Buffer, error) {
	// mew:-scheme names load through the URL path: the real file in local
	// mode, the virtualized support tree otherwise. Everything else is
	// normalized (tilde-expanded, absolutized) so the buffer's filename
	// survives saves and working-directory changes.
	if isBoxPath(filename) {
		return e.loadBufferURL(filename)
	}
	filename = e.normalizeDocPath(filename)
	if !e.usingOSFS {
		buf, err := e.lib.NewFromHostFile(e.FS, filename)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}
			// A filename that does not exist yet is a NEW document under that
			// name, not an error — open an empty buffer carrying the name (save
			// creates it). Without this, launching mew on a new filename exits.
			// A ~/.mew/... page absent from the user tree but SHIPPED in the
			// read-only system/embedded layers opens with the shipped content
			// (a shadow it becomes the moment it is edited and saved) — the same
			// fallback the OS path does below, so a host wiring its own OS-backed
			// FS (the KittyTK trinket) surfaces the help manual too.
			if data, ok := e.mew.fallbackForLocal(filename); ok {
				buf = e.lib.NewFromString(string(data))
				buf.SetFilename(filename)
				// TODO: Later we will defer this note to the point of saving rather than here:
				// e.noteBuffer(buf, "resource", "Shipped page (edits save to your ~/.mew copy)", false)
			} else {
				buf = e.lib.New()
				buf.SetFilename(filename)
				e.noteBuffer(buf, "new", "New file", false)
			}
		}
		// The content is virtualized through the host FileSystem, but a mew-native
		// editing lock still coordinates multiple mew instances editing the same
		// path (it is an OS-level advisory lock under ~/.mew or the project, not
		// written through the host FS; it also records a live foreign lock so the
		// first edit prompts). Emacs locks need the real file's directory and so
		// are not available on this path. Any lock failure is surfaced.
		// A READ-ONLY open takes no editing lock: acquisition (and its
		// warnings) defer to the moment read-only is turned off.
		if e.Config.ReadOnly {
			e.deferMewLock(buf, filename)
		} else if reason := e.acquireMewLock(buf, filename); reason != "" {
			e.noteBuffer(buf, "lock", "Editing lock unavailable: "+reason, true)
		}
		return buf, nil
	}
	emacsLock, lockWarning := e.emacsLockDecision(filename)
	buf, err := e.lib.OpenFile(filename, buffer.OpenOptions{
		UseEmacsLocks: emacsLock,
		LockOwner:     e.lockOwnerString(), // one identity for both emacs and mew-native locks
	})
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		// A filename that does not exist yet is a NEW document under that name,
		// as in every editor — start an empty buffer carrying the name; save
		// creates the file, with the same backup + lock machinery armed below.
		// (Without this, `mew newfile` exited straight back to the shell.) No
		// emacs lock for a path with no directory entry: use the mew-native
		// lock only.
		emacsLock, lockWarning = false, ""
		if data, ok := e.mew.fallbackForLocal(filename); ok {
			// A ~/.mew/... page the user has no local copy of, but which ships
			// in the read-only system/embedded layers: open its content as an
			// unmodified buffer named for the user path. It reads like the
			// shipped page; the moment it is edited and saved it becomes the
			// user's own ~/.mew shadow (garland arms backups/locks then and on
			// every reopen thereafter).
			buf = e.lib.NewFromString(string(data))
			buf.SetFilename(filename)
			e.noteBuffer(buf, "resource", "Shipped page (edits save to your ~/.mew copy)", false)
		} else {
			buf = e.lib.New()
			buf.SetFilename(filename)
			e.noteBuffer(buf, "new", "New file", false)
		}
	}
	if lockWarning != "" {
		e.noteBuffer(buf, "lock", lockWarning, true)
	}
	if !emacsLock {
		// No emacs lock (config or git hygiene): fall back to a mew-native
		// lock in the nearest .mew directory. Its most common catch is the
		// user opening the same file in another mew viewport. A READ-ONLY open
		// takes no editing lock — a viewer advertises nothing; acquisition
		// (and its warnings) defer to the moment read-only is turned off.
		// (Garland's emacs locks need no such deferral: they are lazy by
		// contract, appearing only on the first content mutation.)
		if e.Config.ReadOnly {
			e.deferMewLock(buf, filename)
		} else if reason := e.acquireMewLock(buf, filename); reason != "" {
			e.noteBuffer(buf, "lock", "Editing lock unavailable: "+reason, true)
		}
	}
	if owner, ok := buf.SourceLockOwner(); ok && owner != "" {
		e.noteBuffer(buf, "lock", fmt.Sprintf("%s is being edited by %s", filepath.Base(filename), owner), true)
		e.recordForeignLock(buf, foreignLockInfo{owner: owner, kind: "emacs"})
	}
	e.armSourceSafety(buf)
	return buf, nil
}

// RunContent starts the editor on an in-memory document (no filename) and
// returns the document's final content when the session ends. This is the
// content-in/content-out path for hosts embedding mew as a library.
func (e *Editor) RunContent(content string) (string, error) {
	buf := e.lib.NewFromString(content)
	return e.run(buf)
}

// run drives an editor session on the given initial buffer and returns the
// buffer's final content when the session ends.
func (e *Editor) run(buf *buffer.Buffer) (string, error) {
	e.Running = true

	// Run the startup script (host-supplied, or ~/.mew/profile.mew) before
	// any viewport exists, so it can't modify the opened file or complicate
	// its undo history; it is for macros, mappings, and option setup.
	e.runProfileScript()

	// Create main viewport
	e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Type:            viewport.DocViewport,
		Buffer:          buf,
		Dock:            viewport.DockNone,
		Priority:        0,
		SetFocus:        true,
		ShowLineNumbers: e.Config.ShowLineNumbers,
		ProtectNewlines: !e.Config.DeleteNewlineAsChar,
		TabSize:         e.Config.TabSize,
		ShowInvisibles:  e.Config.ShowInvisibles,
		ShowBidi:        e.Config.ShowBidi,
		ShowMarks:       e.Config.ShowMarks,
		OverwriteMode:   e.Config.OverwriteMode,
		ReadOnly:        e.Config.ReadOnly,
		ShowRuler:       e.Config.ShowColumnRuler,
		Scrollbar:       e.Config.Scrollbar,
		LinkBrowsing:    e.Config.LinkBrowsing,
		SyntaxOverrides: e.Config.SyntaxOverrides,
	})

	return e.serve(buf)
}

// serve creates the plugin viewports and runs the main event loop over the
// already-created buffer viewport(s), returning the primary buffer's final
// content when the session ends. Shared by run (a single initial buffer, for
// the library content-in/content-out path) and RunArgs (one or more files
// opened from a parsed command line).
func (e *Editor) serve(buf *buffer.Buffer) (string, error) {
	// A GUI launch starts at the filesystem root: move somewhere useful
	// (beside the opened document, [general] startPath, or home) before any
	// relative operation runs. A deliberate working directory is untouched.
	e.ensureUsefulStartDir()

	// On a panic, dump DEADCAT then re-raise. Registered first so it unwinds
	// LAST — after the terminal-restoring defers below — then the crash still
	// surfaces with its stack trace.
	defer func() {
		if r := recover(); r != nil {
			e.DumpDeadcat(fmt.Sprintf("panic: %v", r))
			panic(r)
		}
	}()

	// Create plugin viewports (modebar, column ruler, etc.)
	e.createPluginViewports()

	// Set up resize callback to trigger re-render
	// Since the main loop blocks on GetKey(), we perform the render directly
	e.Renderer.SetOnResize(func() {
		// A resize may have changed the cell pixel size; re-query it so the
		// pixel→cell mouse mapping stays accurate (belt-and-suspenders for
		// terminals without the ?2048 in-band notification).
		e.refreshPixelMouseCellSize()
		e.performRender()
	})

	// Start renderer
	e.Renderer.Start()
	defer e.Renderer.Stop()
	defer e.Renderer.Cleanup()

	// Pump host-provided resize signals into the renderer (the virtual
	// SIGWINCH). The goroutine ends when the host closes the channel.
	if e.Config.Terminal != nil && e.Config.Terminal.Resize != nil {
		resize := e.Config.Terminal.Resize
		go func() {
			for range resize {
				e.Renderer.TriggerResize()
			}
		}()
	}

	// Set up terminal for raw input
	if err := e.KeyHandler.Start(); err != nil {
		return "", fmt.Errorf("failed to start keyboard handler: %w", err)
	}
	defer e.KeyHandler.Stop()

	// Arm crash-dump signal handlers (a standalone real-terminal session only;
	// a host owning the process triggers dumps itself via DumpDeadcat).
	defer e.installDeadcatSignals()()

	// Launch greeting: product, version, and the first keys a new user
	// needs. It rides the normal transient-notification machinery, so it
	// expires like any other notice — and expands its TFC codes, so the two
	// keys resolve to the LIVE bindings, spelled out and colored. That
	// expansion READS the live keymap (verboseKeys), which the renderer's
	// resize goroutine may be rewriting (performRender -> applyFocusedMappings
	// -> SetMappings); every other keymap access is serialized by renderMu, and
	// this one — uniquely off the main key/render path — must be too.
	e.renderMu.Lock()
	e.ShowNotificationTFC(version.Banner())
	e.renderMu.Unlock()

	// If a DEADCAT from a prior crash is present, let the user know (a
	// transient, not a prompt — they recover it when they choose to).
	e.deadcatLaunchNotice()

	// Mouse reporting on for the whole session (Cleanup turns it back off):
	// button presses, drags, releases and the scroll wheel arrive through the
	// key stream as Mouse* pseudo-keys.
	e.Renderer.EnableMouseReporting()
	// Probe for SGR-Pixels (?1016): if the terminal has it, mouse reports
	// switch to pixels and an insert-mode click lands the caret on the nearest
	// cell edge. Inert on terminals that ignore the handshake.
	e.beginPixelMouseProbe()

	// Request grapheme-cluster width (DEC mode 2027) so the terminal — mew's
	// purfecterm, or any host that honors it — measures cell width the same way
	// mew's textwidth does. Terminals without the mode ignore it.
	e.Renderer.EnableGraphemeWidth()

	// A flex-width host stores one cell per character, so cursor addressing
	// counts characters: switch the renderer's CUP columns to logical.
	e.Renderer.SetLogicalColumns(e.Config.LogicalColumnTerminal)

	// Initial render
	e.performRender()

	// Queue the launch --eval scripts (if any) as the first events the loop
	// processes, so they run visually in the freshly rendered session.
	e.startLaunchEvals()

	// Main event loop
	for e.Running {
		// Wait for either a key or a paste chunk
		event := e.KeyHandler.GetEvent()

		if event.Closed {
			// The input source has ended (host closed its event feed):
			// wind the session down. The state snapshot below is still
			// delivered, so nothing is lost that the host wanted kept.
			e.Running = false
			continue
		}

		if event.Do != nil {
			// A posted action (host command port, async clipboard delivery):
			// run it with keystroke safety — under renderMu, followed by the
			// same render tail a key command gets.
			e.renderMu.Lock()
			event.Do()
			e.updateModebar()
			e.renderMu.Unlock()
			if e.renderRequested.Load() {
				e.performRender()
			}
			continue
		}

		if event.Paste != nil {
			// Begin the paste transaction on the first chunk so the whole paste
			// collapses into a single undo revision.
			if !e.pasteActive {
				if w := e.ViewportManager.GetFocusedViewport(); w != nil && w.Buffer != nil {
					e.pasteBuf = w.Buffer
					e.pasteBuf.BeginUserCommand("paste")
					e.pasteActive = true
				}
			}

			// Handle paste chunk
			e.insertPasteChunk(event.Paste.Content)
			// Render and flush for visual feedback
			e.ensureCursorVisible(e.ViewportManager.GetFocusedViewport())
			e.performRender()
			e.Renderer.Sync()

			if event.Paste.IsFinal {
				// Final chunk - do cleanup and close the paste transaction.
				w := e.ViewportManager.GetFocusedViewport()
				if w != nil {
					e.afterHorizontalMovement(w)
				}
				if e.pasteActive {
					e.pasteBuf.EndUserCommand()
					e.pasteActive = false
					e.pasteBuf = nil
				}
			}
		} else if event.Key != "" {
			// A terminal cursor-position report (the flipBidiForHost probe's
			// reply) is consumed here, never typed.
			if e.handleBidiProbeReply(event.Key) {
				continue
			}
			// The ?1016 SGR-pixels handshake replies (DECRPM/WinOp) are
			// consumed here too, never typed.
			if e.handlePixelMouseReply(event.Key) {
				continue
			}
			// Mouse pseudo-keys (position/press/drag/release/scroll) never
			// enter keymap dispatch; see mouse.go. They DO reach the render
			// tail: a click's effect (caret, pressed button, a follow) must
			// paint now, not on the next keystroke.
			if e.handleMouseKey(event.Key) {
				if e.renderRequested.Load() {
					e.performRender()
				}
				continue
			}
			// Process key. Hold renderMu across the whole mutation so the
			// renderer's resize goroutine can't run a render (which reads this
			// same editor state) partway through command execution. The block
			// doesn't call performRender, so this doesn't nest with its lock.
			e.renderMu.Lock()
			e.dispatchKey(event.Key)
			e.renderMu.Unlock()
		}

		// Render if needed
		if e.renderRequested.Load() {
			e.performRender()
		}
	}

	// Session over: hand the host a state snapshot, then return the final
	// content of the initial document buffer.
	if e.Config.StateCallback != nil {
		e.Config.StateCallback(e.stateSnapshot())
	}
	return buf.GetContent(), nil
}

// stateSnapshot captures everything mew wants the host to persist for it,
// for the StateCallback. Feed it back via Config.InitialState next session;
// hosts serialize it with config.EncodeState (PSL or JSON).
func (e *Editor) stateSnapshot() map[string]interface{} {
	return map[string]interface{}{
		"showLineNumbers": e.Config.ShowLineNumbers,
		"showColumnRuler": e.Config.ShowColumnRuler,
		"scrollbar":       e.Config.Scrollbar,
		"tabSize":         e.Config.TabSize,
		"showInvisibles":  e.Config.ShowInvisibles,
		"wordWrap":        e.Config.WordWrap,
		"modebarLocation": e.Config.ModebarLocation,
		"syntax":          e.Config.Syntax,
		"syntaxDetect":    e.Config.SyntaxDetect,
		"syntaxOverrides": e.Config.SyntaxOverrides,
	}
}

// applyInitialState restores a previously captured state snapshot over the
// applyFontConfig registers the fonts declared in the loaded config into the
// host font engine and applies the startup ui-term alias. It runs once at
// startup, before any painting resolves font names: the [fonts] map and
// [window] fonts_path go through FontLoader (explicit-path and search-path
// registration); [window] ui_term rides FontSink, the same path the set_font
// command uses live. All are no-ops on a plain terminal (nil sinks), which
// owns its own fonts.
func (e *Editor) applyFontConfig() {
	files := e.LoadedConfig.Fonts
	paths := e.LoadedConfig.Window.FontsPath
	if e.Config.FontLoader != nil && (len(files) > 0 || len(paths) > 0) {
		e.Config.FontLoader(files, paths)
	}
	if e.Config.FontAdjustSink != nil {
		// Before the aliases: a correction must be in place by the time any
		// name resolves and the first mask is rasterized.
		for family, adj := range e.LoadedConfig.FontAdjust {
			scale := 1.0
			if adj.HasScale {
				scale = adj.Scale
			}
			if adj.HasBaseline || adj.HasScale {
				e.Config.FontAdjustSink(family, adj.Baseline, scale)
			}
		}
	}
	if e.Config.FontSink != nil {
		for alias, names := range e.LoadedConfig.Window.FontAliases {
			if len(names) > 0 {
				e.Config.FontSink(alias, names)
			}
		}
	}
}

// loaded configuration, reading numbers tolerantly (PSL yields int64, JSON
// float64).
func applyInitialState(cfg *Config) {
	state := cfg.InitialState
	if state == nil {
		return
	}
	if v, ok := stateBool(state, "showLineNumbers"); ok {
		cfg.ShowLineNumbers = v
	}
	if v, ok := stateBool(state, "showColumnRuler"); ok {
		cfg.ShowColumnRuler = v
	}
	if v, ok := stateBool(state, "scrollbar"); ok {
		cfg.Scrollbar = v
	}
	if v, ok := stateBool(state, "showInvisibles"); ok {
		cfg.ShowInvisibles = v
	}
	if v, ok := stateBool(state, "wordWrap"); ok {
		cfg.WordWrap = v
	}
	if v, ok := stateInt(state, "tabSize"); ok && v > 0 {
		cfg.TabSize = v
	}
	if v, ok := state["modebarLocation"].(string); ok && (v == "top" || v == "bottom") {
		cfg.ModebarLocation = v
	}
	if v, ok := state["syntax"].(string); ok {
		cfg.Syntax = v
	}
	if v, ok := stateBool(state, "syntaxDetect"); ok {
		cfg.SyntaxDetect = v
	}
	if v, ok := state["syntaxOverrides"].(string); ok {
		cfg.SyntaxOverrides = v
	}
}

// NotifyResize tells the editor the terminal size changed: the renderer
// re-queries the size source and re-renders. This is the manual equivalent
// of SIGWINCH for hosts and platforms without it.
func (e *Editor) NotifyResize() {
	e.Renderer.TriggerResize()
}

// stateBool reads a bool from a state snapshot.
func stateBool(state map[string]interface{}, key string) (bool, bool) {
	if v, ok := state[key].(bool); ok {
		return v, true
	}
	return false, false
}

// stateInt reads an integer from a state snapshot, accepting the native
// number types of both serialization formats.
func stateInt(state map[string]interface{}, key string) (int, bool) {
	switch v := state[key].(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	}
	return 0, false
}

// createPluginViewports creates viewports for all enabled plugins.
func (e *Editor) createPluginViewports() {
	// Create modebar viewport (always enabled) at its configured location
	e.Modebar.SetLocation(e.Config.ModebarLocation)
	e.Modebar.CreateViewport()
}

// runProfileScript executes the startup pawscript: a host-supplied script
// when one was provided, else the user's profile.mew (created with a small
// default when missing), unless the host opted out. It runs before any
// viewport exists, so it cannot modify the opened file or its undo history;
// script errors surface through the usual PawScript stderr writer as error
// viewports.
func (e *Editor) runProfileScript() {
	if e.Config.ProfileScript != nil {
		e.PawScript.ExecuteFile(*e.Config.ProfileScript, "profile.mew")
		return
	}
	if e.Config.SkipProfileScript {
		return
	}
	content, err := e.ConfigMgr.LoadProfile()
	if err != nil {
		e.ShowError("Failed to load profile.mew: " + err.Error())
		return
	}
	e.PawScript.ExecuteFile(content, e.ConfigMgr.ProfilePath())
}

// updateModebar updates the modebar display.
func (e *Editor) updateModebar() {
	// The modebar plugin handles its own rendering via the custom renderer
	// Just request a render to update the display
	e.RequestRender()
}

// transientNotificationClasses are the viewport classes used for the transient
// bottom-docked message viewports created by ShowNotification/ShowError/
// ShowWarning. They share a single display slot and are auto-expired.
var transientNotificationClasses = map[string]bool{
	"notification": true,
	"error":        true,
	"warning":      true,
}

// showTransient creates a one-line bottom-docked message viewport of the given
// class; the class drives its colors. Transient viewports are not cleared on
// creation; they stack and are removed purely by age via
// expireStaleNotifications.
//
// A non-empty tag makes the message REPLACE its predecessor rather than stack:
// any existing transient carrying the same tag is removed first. A rapid
// series of same-tag toasts (e.g. "Switched to …" as focus cycles) collapses
// to just the latest instead of piling up. The tag rides ViewportOptions and
// is otherwise inert — colors, chrome set, and age-expiry are unchanged.
func (e *Editor) showTransient(message, class, tag string, tfc bool) {
	if tfc {
		message = e.expandTransientTFC(message, class)
	}
	if tag != "" {
		for _, w := range e.ViewportManager.GetViewportsByDock(viewport.DockBottom) {
			if w.Tag == tag {
				e.ViewportManager.RemoveViewport(w.ID)
			}
		}
	}
	e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Type:            viewport.ToolViewport,
		ViewportSet:     viewport.ViewportSetTransient,
		Class:           class,
		Tag:             tag,
		Dock:            viewport.DockBottom,
		Priority:        0, // Very low priority - below everything else
		MinHeight:       1,
		MaxHeight:       1,
		MessageTopInner: message,
		ShowLineNumbers: false,
	})

	e.RequestRender()
}

// expandTransientTFC expands TFC codes in a transient's message (opt-in, per
// notification). A %keys#…% / %keys_verbose#…% badge is wrapped in the "key"
// color and closed with the transient class's own "messages" color, so the
// badge stands out and the surrounding text returns to the bar's color — the
// renderer paints the whole bar in that "messages" color, so the close restores
// it exactly.
func (e *Editor) expandTransientTFC(message, class string) string {
	const typ = "tool" // transients are ToolViewports
	keyColor := e.LoadedConfig.Colors.Resolve(class, typ, "key")
	barColor := e.LoadedConfig.Colors.Resolve(class, typ, "messages")
	return plugins.ExpandTFC(message, nil, e.tfcKeyResolver(keyColor, barColor))
}

// ShowNotification creates a transient notification viewport at the bottom of the
// screen (informational messages, command confirmations).
func (e *Editor) ShowNotification(message string) {
	e.showTransient(message, "notification", "", false)
}

// ShowNotificationTagged is ShowNotification for a message that should REPLACE
// its predecessor instead of stacking: a new toast with the same tag removes
// the old one first. For repeated messages that would otherwise pile up — the
// "Switched to …" focus toast, the "Buffer is read-only" warning.
func (e *Editor) ShowNotificationTagged(message, tag string) {
	e.showTransient(message, "notification", tag, false)
}

// ShowNotificationTFC is ShowNotification for a message that should have its TFC
// codes expanded — e.g. %keys_verbose#help_toggle|^Q H% resolved to the live
// binding and colored. TFC support is opt-in per notification.
func (e *Editor) ShowNotificationTFC(message string) {
	e.showTransient(message, "notification", "", true)
}

// cursorStyleFor reports the DECSCUSR shape a viewport's CURRENT editing mode
// calls for: navigationCursor while link browsing is armed (the mode sits over
// the top of the other two), overwriteCursor while typing replaces, and
// insertCursor otherwise. The renderer consults it only on frames that show the
// caret, so a shape is never selected for one being hidden — including the
// inert caret inside a focused link button.
func (e *Editor) cursorStyleFor(w *viewport.Viewport) int {
	switch {
	case w != nil && w.BrowseActive:
		return e.Config.NavigationCursor
	case w != nil && w.ViewState.OverwriteMode:
		return e.Config.OverwriteCursor
	default:
		return e.Config.InsertCursor
	}
}

// clearTaggedTransient removes any transient carrying tag, for a progress
// message whose work has finished (or been cancelled) and which should come
// down NOW rather than linger out its five-second expiry — the incremental
// search's "Searching…" toast.
func (e *Editor) clearTaggedTransient(tag string) {
	if tag == "" {
		return
	}
	for _, w := range e.ViewportManager.GetViewportsByDock(viewport.DockBottom) {
		if w.Tag == tag {
			e.ViewportManager.RemoveViewport(w.ID)
			e.RequestRender()
		}
	}
}

// showTaggedTransient is showTransient for messages that should replace their
// predecessor instead of stacking: any existing transient carrying the same
// tag is removed first. The class still drives colors and age-expiry (so pass
// "notification"); the tag only groups the message for replacement. Used by
// filename completion so re-completing does not pile up option lists.
func (e *Editor) showTaggedTransient(message, class, tag string) {
	for _, w := range e.ViewportManager.GetViewportsByDock(viewport.DockBottom) {
		if w.Tag == tag {
			e.ViewportManager.RemoveViewport(w.ID)
		}
	}
	e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Type:            viewport.ToolViewport,
		ViewportSet:     "help",
		Class:           class,
		Tag:             tag,
		Dock:            viewport.DockBottom,
		Priority:        0,
		MinHeight:       1,
		MaxHeight:       1,
		MessageTopInner: message,
		ShowLineNumbers: false,
	})
	e.RequestRender()
}

// appendVerboseLog appends lines to the shared verbose-log viewport (class
// "verboseLog"), creating it on first use: a new empty buffer viewport like
// buffer_new makes, but in the background — it never takes focus or the
// painted main area; the user reaches it later via viewport cycling.
func (e *Editor) appendVerboseLog(lines ...string) {
	var vw *viewport.Viewport
	for _, w := range e.ViewportManager.AllViewports() {
		if w.Class == "verboseLog" {
			vw = w
			break
		}
	}
	if vw == nil {
		id := e.ViewportManager.CreateViewport(viewport.ViewportOptions{
			Type:            viewport.DocViewport,
			Class:           "verboseLog",
			Dock:            viewport.DockNone,
			Priority:        0,
			Buffer:          e.lib.New(),
			ShowLineNumbers: true,
			ProtectNewlines: !e.Config.DeleteNewlineAsChar,
			TabSize:         e.Config.TabSize,
			Visible:         true,
		})
		vw = e.ViewportManager.GetViewport(id)
	}
	if vw == nil || vw.Buffer == nil {
		return
	}
	for _, line := range lines {
		if vw.Buffer.GetLineCount() == 1 && strings.TrimRight(vw.Buffer.GetLine(0), "\n\r") == "" {
			// First write into the fresh, empty buffer: insert at the start of
			// line 0 rather than rewriting it.
			vw.Buffer.InsertText(0, 0, line)
		} else {
			vw.Buffer.InsertLine(vw.Buffer.GetLineCount(), line)
		}
	}
}

// announceFocusedViewport shows a "Switched to ..." notification naming the
// newly focused viewport: its top-center message, else its buffer's filename,
// else its ID.
func (e *Editor) announceFocusedViewport() {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil {
		return
	}
	name := w.MessageTopCenter
	if name == "" && w.Buffer != nil {
		name = w.Buffer.GetFilename()
	}
	if name == "" {
		name = w.ID
	}
	// Tagged "navigate" so a rapid cycle (^B N / mouse focus) collapses to the
	// latest "Switched to …", and so it shares the navigation family — a switch
	// followed by a link jump replaces rather than stacks.
	e.ShowNotificationTagged("Switched to "+name, "navigate")
}

// ShowError shows a transient error viewport at the bottom of the screen.
func (e *Editor) ShowError(message string) {
	e.showTransient(message, "error", "", false)
}

// ShowWarning shows a transient warning viewport at the bottom of the screen.
func (e *Editor) ShowWarning(message string) {
	e.showTransient(message, "warning", "", false)
}

// ShowWarningTagged is ShowWarning for a warning that should REPLACE its
// predecessor instead of stacking — e.g. the "Buffer is read-only" warning,
// which fires on every rejected edit and would otherwise pile up.
func (e *Editor) ShowWarningTagged(message, tag string) {
	e.showTransient(message, "warning", tag, false)
}

// expireStaleNotifications removes transient notification/error/warning viewports
// that are older than 5 seconds. Called before each command executes.
func (e *Editor) expireStaleNotifications() {
	now := time.Now()
	for _, w := range e.ViewportManager.GetViewportsByDock(viewport.DockBottom) {
		if w.Tag == niqqudNudgeTag {
			continue // condition-driven; managed by updateNiqqudNudge, never aged out
		}
		if transientNotificationClasses[w.Class] && now.Sub(w.SpawnedAt) > 5*time.Second {
			e.ViewportManager.RemoveViewport(w.ID)
		}
	}
}

// niqqudNudgeTag identifies the persistent force_ltr nudge viewport so it can be
// found for replacement/removal and exempted from age-expiry and the prompt
// priority scan.
const niqqudNudgeTag = "kitty_force_ltr_nudge"

// niqqudNudgeMessage names the one Kitty config change that fixes RTL vowel
// corruption. It sits pinned at the very bottom (just below the modebar) while a
// Hebrew cluster with unfolded niqqud is on screen and mew is flipping for Kitty.
// (Turning mew's own flip off instead would only scramble the letter order —
// force_ltr is the real fix, so the message points at that alone.)
const niqqudNudgeMessage = "* kitty terminal needs force_ltr = yes to eliminate RTL vowel corruption.  Please fix and restart mew."

// niqqudLayoutSig captures the layout facts that are independent of the nudge's
// own one-row footprint, so the hysteresis latch (updateNiqqudNudge) releases
// only on a genuine change — a resize, a scroll, an edit, or a change in the
// viewport set — and not on the nudge's own appearance.
func (e *Editor) niqqudLayoutSig() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%dx%d;", e.Renderer.Width, e.Renderer.Height)
	// Count viewports other than the nudge itself: a docked prompt/help
	// appearing or leaving is a real layout change worth re-evaluating.
	n := 0
	for _, w := range e.ViewportManager.AllViewports() {
		if w.Tag != niqqudNudgeTag {
			n++
		}
	}
	fmt.Fprintf(&b, "n%d;", n)
	if fw := e.ViewportManager.GetFocusedViewport(); fw != nil {
		lines := 0
		if fw.Buffer != nil {
			lines = fw.Buffer.GetLineCount()
		}
		fmt.Fprintf(&b, "%s:o%d,%d:L%d", fw.ID, fw.ViewState.ViewOffsetY, fw.ViewState.ViewOffsetX, lines)
	}
	return b.String()
}

// updateNiqqudNudge shows or hides the persistent force_ltr nudge. It should
// exist exactly while mew is flipping RTL for Kitty AND the current frame holds
// a Hebrew cluster whose niqqud survive the active fold (the content Kitty may
// mis-render), and come down otherwise. Called once per render, after the frame
// is painted so the scan reads live cells.
//
// Because the nudge occupies a screen row, a niqqud line resting exactly on a
// viewport's bottom boundary can oscillate: showing the nudge scrolls the line
// off, which clears the condition, which hides the nudge, which brings the line
// back. To avoid a per-frame flicker (and the render churn it drives), a short
// run of frame-to-frame toggles latches the nudge on; the latch releases when
// niqqudLayoutSig changes — i.e. when the user resizes, scrolls, edits, or the
// viewport set changes — at which point the condition is re-evaluated fresh.
func (e *Editor) updateNiqqudNudge() {
	raw := e.kittyFlipActive && e.Renderer.FrameHasUncomposedNiqqud()

	sig := e.niqqudLayoutSig()
	if e.niqqudNudgeLatched && sig != e.niqqudNudgeLatchSig {
		e.niqqudNudgeLatched = false
		e.niqqudNudgeToggleRun = 0
	}

	want := raw
	if e.niqqudNudgeLatched {
		want = true
	} else {
		if raw != e.niqqudNudgeWantPrev {
			e.niqqudNudgeToggleRun++
		} else {
			e.niqqudNudgeToggleRun = 0
		}
		// Three straight alternations (on/off/on) means the boundary feedback
		// loop, not real content change: pin it on until the layout moves.
		if e.niqqudNudgeToggleRun >= 3 {
			e.niqqudNudgeLatched = true
			e.niqqudNudgeLatchSig = sig
			want = true
		}
	}
	e.niqqudNudgeWantPrev = raw

	var existing *viewport.Viewport
	for _, w := range e.ViewportManager.GetViewportsByDock(viewport.DockBottom) {
		if w.Tag == niqqudNudgeTag {
			existing = w
			break
		}
	}
	switch {
	case want && existing == nil:
		e.ViewportManager.CreateViewport(viewport.ViewportOptions{
			Type:        viewport.ToolViewport,
			ViewportSet: viewport.ViewportSetTransient,
			Class:       "warning",
			Tag:         niqqudNudgeTag,
			Dock:        viewport.DockBottom,
			// Just below the modebar: pinned to the last screen line (or just
			// above a bottom-docked modebar), under any prompt. The prompt
			// priority scan and age-expiry both exempt this tag.
			Priority:        plugins.ModebarBottomPriority - 1,
			MinHeight:       1,
			MaxHeight:       1,
			MessageTopInner: niqqudNudgeMessage,
			ShowLineNumbers: false,
		})
		e.RequestRender()
	case !want && existing != nil:
		e.ViewportManager.RemoveViewport(existing.ID)
		e.RequestRender()
	}
}

// There is ONE docked help viewport (Tag "help", top dock): help_toggle NAVIGATES
// it between help "locations" — the built-in Quick Help (the WordStar command
// reference) and help:/ wiki pages — building the viewport's own nav history, so
// its back button returns to where the reader came from. help_toggle on the
// location already showing closes the viewport. buffer_open_file, by contrast,
// opens ordinary UNtagged help viewports that can stack; only this slot is
// tagged, and only help_toggle manages it. The Quick Help checkmark tracks
// whether the slot is currently showing Quick Help.
const helpViewportTag = "help"

// The docked help slot carries one of two viewport CLASSES depending on its role,
// so per-class config (colors, mappings, options overlays) can target each: a
// regular help page is class "help", Quick Help is class "quickhelp".
// applyHelpViewportChrome flips it as the slot changes role. The Tag stays "help"
// either way, so helpViewport() still finds the slot.
const helpViewportClass = "help"
const quickHelpClass = "quickhelp"

// helpMaxHeightFraction caps a regular (non-Quick) help page at half the mew
// area height (less the 2 rows reserved for chrome): the layout resolves it live
// as floor((mewHeight-2) * 0.5), so the cap tracks the terminal size instead of a
// fixed row count. Quick Help ignores it and fits its content instead.
const helpMaxHeightFraction = 0.5

// quickHelpDocURL is the synthetic identity of the Quick Help buffer. It lives
// in mew's GENERATED "mew:" scheme (not the box: storage tree), so it is never
// read from disk, never resolves as a wiki page, and just gives the location a
// stable URL to compare against.
const quickHelpDocURL = "mew:/quickhelp"

// helpViewport returns the single docked help viewport (Tag "help"), or nil.
func (e *Editor) helpViewport() *viewport.Viewport {
	for _, w := range e.ViewportManager.GetViewportsByDock(viewport.DockTop) {
		if w.Tag == helpViewportTag {
			return w
		}
	}
	return nil
}

// quickHelpViewportOpen reports whether the docked help viewport is open AND in
// Quick Help (dynamic, context-following) mode — what the "Quick Help"
// checkmark reflects. It reads the quickHelpMode flag rather than the buffer
// URL: Quick Help now shows real help pages, so URL alone can't tell it apart
// from a page reached by browsing. syncQuickHelpMode() first reconciles the
// flag with reality (the viewport may have closed, or the user may have browsed
// away) so the answer is always current.
func (e *Editor) quickHelpViewportOpen() bool {
	e.syncQuickHelpMode()
	return e.quickHelpMode && e.helpViewport() != nil
}

// syncQuickHelpMode reconciles quickHelpMode with the docked help viewport: quick
// mode ends the moment the viewport closes or its buffer drifts from the one
// Quick Help installed (the user followed a link, walked history, or opened the
// main help). Called before anything reads or acts on quick mode.
func (e *Editor) syncQuickHelpMode() {
	if !e.quickHelpMode {
		return
	}
	hw := e.helpViewport()
	if hw == nil {
		e.quickHelpMode = false
		e.quickHelpBuf = nil
		e.quickHelpShownTopic = ""
		return
	}
	if hw.Buffer != e.quickHelpBuf {
		// The user browsed away (link / history): the slot is a regular help
		// page now, so leave quick mode and restore the page chrome (title bar,
		// standard height) that Quick Help had stripped.
		e.quickHelpMode = false
		e.quickHelpBuf = nil
		e.quickHelpShownTopic = ""
		e.applyHelpViewportChrome(hw)
	}
}

// toggleHelp is the help_toggle command: open/navigate the docked help slot, or
// close it when it is already showing the target.
//
// A PAGE argument (help_toggle "keys") is a request to READ, so it focuses the
// help viewport — a reader who opened help to learn the keys cannot be expected
// to already know the key that would move focus there — and toggling it back off
// returns focus where the reader came from (see closeHelpViewport). With NO
// argument the target is Quick Help, which is a peek that must never steal the
// caret: it follows the key prefix you are typing, so taking focus would stop
// the very typing it is describing.
func (e *Editor) toggleHelp(arg string) bool {
	return e.openHelp(arg, true, strings.TrimSpace(arg) != "")
}

// openHelpFocused is the help_open command: like help_toggle but it only ever
// opens or replaces — it never closes — and it FOCUSES the help viewport, so the
// reader can scroll and follow links straight away.
func (e *Editor) openHelpFocused(arg string) bool { return e.openHelp(arg, false, true) }

// openHelp drives the single docked help slot (Tag "help", top dock). With no
// argument the target is Quick Help; with one it is the help wiki page
// help:/<arg> ("help:/..." is accepted whole, so `help:/` opens the index).
//
//	allowClose — when the slot is ALREADY showing the target, close it (toggle
//	             behavior); otherwise leave it open and merely re-assert it.
//	focus      — focus the help viewport after opening/navigating (help_open, and
//	             help_toggle for a page); Quick Help passes false so the peek
//	             never steals the caret.
//
// Navigating an existing slot grows its nav history, so its back button returns
// to where the reader came from.
func (e *Editor) openHelp(arg string, allowClose, focus bool) bool {
	arg = strings.TrimSpace(arg)
	e.syncQuickHelpMode()
	hw := e.helpViewport()

	if arg == "" {
		if e.quickHelpMode && hw != nil {
			// Already at Quick Help: toggle closes; open just re-asserts focus.
			if allowClose {
				e.closeHelpViewport(hw)
				e.quickHelpMode = false
				e.quickHelpBuf = nil
				e.quickHelpShownTopic = ""
				e.RequestRender()
				return true
			}
			e.focusHelpViewport(hw, focus)
			return true
		}
		// Resolve the topic for the current prefix now, so a menu-driven open
		// (no keypress pending) still lands on the root topic rather than a
		// stale one.
		e.quickHelpTopic = e.KeyProcessor.HelpTopic(e.ActiveSequence)
		buf, root, wikiName, browse, ok := e.quickHelpDestination()
		if !ok {
			// Fresh open with no page for the topic: nothing to keep, so show
			// the no-help notice.
			buf, root, wikiName, browse = e.quickHelpBuffer(), "", "", false
		}
		// Mark Quick Help mode BEFORE showing, so showHelpLocation's chrome pass
		// (applyHelpViewportChrome) drops the title bar and fits the height.
		e.quickHelpMode = true
		e.quickHelpBuf = buf
		e.quickHelpShownTopic = e.quickHelpTopic
		e.showHelpLocation(hw, buf, root, wikiName, browse, focus)
		return true
	}

	// A page argument opens/navigates the main help — this is browsing, so it
	// leaves Quick Help mode: the quickHelpTopic must never steer "Using mew".
	e.quickHelpMode = false
	e.quickHelpBuf = nil
	e.quickHelpShownTopic = ""

	ref := arg
	if !strings.HasPrefix(strings.ToLower(ref), "help:") {
		ref = "help:/" + strings.TrimPrefix(ref, "/")
	}
	res := e.resolveFollow(nil, ref)
	if res.url == "" {
		// Opening a help page that does not exist is an error, like opening a
		// missing file — not an invitation to author one. Report it as a
		// transient toast and leave any open help viewport untouched. (Authoring a
		// new help page is done deliberately through buffer_open, not here.)
		e.ShowError(res.message)
		e.RequestRender()
		return true
	}
	if hw != nil && e.bufferCanonicalURL(hw.Buffer) == res.url {
		// Already at this page: toggle closes; open just re-asserts focus.
		if allowClose {
			e.closeHelpViewport(hw)
			e.RequestRender()
			return true
		}
		e.focusHelpViewport(hw, focus)
		return true
	}
	buf := e.findOpenBuffer(res.url)
	if buf == nil {
		loaded, err := e.loadBufferURL(res.url)
		if err != nil {
			e.ShowError("Open " + displayPath(res.url) + ": " + err.Error())
			e.RequestRender()
			return true
		}
		buf = loaded
	}
	e.showHelpLocation(hw, buf, res.root, res.wikiName, true, focus)
	return true
}

// closeHelpViewport closes the docked help slot and, when the slot held the
// keyboard, hands focus back to the viewport last focused in the BLANK viewport
// set — the ordinary document group the reader came from, as opposed to the
// "help" set the slot itself belongs to. Reading help is a detour, so toggling
// it off must land the reader exactly where they left, not wherever
// RemoveViewport's generic fallback (nearest by creation order) would pick.
//
// The target is captured BEFORE removal: RemoveViewport re-focuses through that
// fallback, which would overwrite the blank set's last-focused record on its way
// out. Focus is restored only if the slot actually had it — closing help from
// the document leaves the caret in the document, untouched.
func (e *Editor) closeHelpViewport(hw *viewport.Viewport) {
	if hw == nil {
		return
	}
	fw := e.ViewportManager.GetFocusedViewport()
	hadFocus := fw != nil && fw.ID == hw.ID
	back := e.ViewportManager.LastFocusedInSet("")
	e.ViewportManager.RemoveViewport(hw.ID)
	if hadFocus && back != nil {
		e.ViewportManager.SetFocus(back.ID)
	}
}

// focusHelpViewport focuses hw when focus is set (help_open), repainting; a no-op
// otherwise (help_toggle re-asserting an already-open slot).
func (e *Editor) focusHelpViewport(hw *viewport.Viewport, focus bool) {
	if focus && hw != nil {
		e.ViewportManager.SetFocus(hw.ID)
	}
	e.RequestRender()
}

// showHelpLocation puts buf into the docked help viewport: it NAVIGATES an
// existing viewport (swap_buffer, so the previous location goes onto the back
// history), or creates the docked viewport when none exists. root/wikiName wire
// the viewport to the help wiki (empty for Quick Help), browse arms link browsing
// for a wiki page, and focus (help_open only) moves the caret to the slot.
func (e *Editor) showHelpLocation(hw *viewport.Viewport, buf *buffer.Buffer, root, wikiName string, browse, focus bool) {
	if hw == nil {
		hw = e.createHelpViewport(buf, focus)
	} else {
		e.swapBuffer(hw, buf)
		if focus {
			e.ViewportManager.SetFocus(hw.ID)
		}
	}
	hw.WikiRoot = root
	hw.WikiName = wikiName
	hw.BrowseActive = browse && hw.ViewState.LinkBrowsing
	e.applyHelpViewportChrome(hw) // title bar + height per Quick Help vs. page role
	e.ensureCursorVisible(hw)
	e.RequestRender()
}

// applyHelpViewportChrome sets the docked help viewport's chrome for its CURRENT
// role, read from quickHelpMode. Quick Help is a dynamic, chromeless page: it
// drops the top "Help" message bar entirely and sizes its max height to the
// loaded file so the viewport fits its content. Regular help keeps the "Help"
// title bar and the standard height envelope. Called whenever the viewport's role
// or loaded buffer changes — not per edit — so the message bar enables and
// disables as the same slot flips between Quick Help and a help page.
func (e *Editor) applyHelpViewportChrome(hw *viewport.Viewport) {
	if hw == nil {
		return
	}
	if e.quickHelpMode {
		hw.Class = quickHelpClass // distinct class for per-class config
		hw.MessageTopCenter = ""  // no title bar: all MessageTop* empty hides the row
		hw.CanFocus = false       // the focus switcher (^B N/^B P) skips the peek
		hw.MaxHeightFraction = 0  // Quick Help fits its content, not a proportion
		fit := quickHelpFitHeight(hw.Buffer)
		hw.MaxHeight = fit
		if hw.MinHeight > fit {
			hw.MinHeight = fit
		}
	} else {
		hw.Class = helpViewportClass
		hw.MessageTopCenter = "Help"
		hw.CanFocus = true
		hw.MinHeight = 4
		hw.MaxHeight = 0 // no literal ceiling; the proportional cap governs
		hw.MaxHeightFraction = helpMaxHeightFraction
	}
}

// quickHelpFitHeight is the height Quick Help sizes its viewport to: the visible
// line count of buf, less the phantom empty line a trailing newline would add,
// and at least 1.
func quickHelpFitHeight(buf *buffer.Buffer) int {
	if buf == nil {
		return 1
	}
	n := buf.GetLineCount()
	if strings.HasSuffix(buf.GetContent(), "\n") {
		n-- // GetLineCount counts the empty line after a trailing newline
	}
	if n < 1 {
		n = 1
	}
	return n
}

// createHelpViewport creates the single docked help viewport (Tag "help") holding
// buf. focus moves the caret to it (help_open, and help_toggle for a page); Quick
// Help passes false so the peek opens without stealing focus. Its chrome (title
// bar, height envelope) is set immediately afterward by applyHelpViewportChrome
// per the viewport's role; the values here are the regular-help defaults.
func (e *Editor) createHelpViewport(buf *buffer.Buffer, focus bool) *viewport.Viewport {
	opts := e.docViewportOptions()
	opts.Type = viewport.ToolViewport
	opts.ViewportSet = "help"
	opts.Class = helpViewportClass // applyHelpViewportChrome flips this to quickHelpClass in Quick Help
	opts.Tag = helpViewportTag
	opts.Dock = viewport.DockTop
	opts.Priority = 100
	opts.MinHeight = 4
	opts.MaxHeightFraction = helpMaxHeightFraction // proportional cap; no literal ceiling
	opts.MessageTopCenter = "Help"
	opts.Buffer = buf
	opts.SetFocus = focus
	return e.ViewportManager.GetViewport(e.ViewportManager.CreateViewport(opts))
}

// quickHelpDestination resolves the help wiki page for the current
// quickHelpTopic (help:/<topic>), plus its wiki wiring (so its links are
// followable). ok is false when the topic names no existing page — the caller
// decides what to do (keep the current help showing, or, on a fresh open, fall
// back to the no-help notice). HelpTopic already backs off to the longest
// shorter prefix, so !ok means neither the matched topic nor the root has a
// live, existing page.
func (e *Editor) quickHelpDestination() (buf *buffer.Buffer, root, wikiName string, browse, ok bool) {
	if e.quickHelpTopic != "" {
		ref := "help:/" + e.quickHelpTopic
		if res := e.resolveFollow(nil, ref); res.url != "" {
			b := e.findOpenBuffer(res.url)
			if b == nil {
				if loaded, err := e.loadBufferURL(res.url); err == nil {
					b = loaded
				}
			}
			if b != nil {
				return b, res.root, res.wikiName, true, true
			}
		}
	}
	return nil, "", "", false, false
}

// refreshQuickHelp re-renders the docked Quick Help viewport for the current
// quickHelpTopic, in place — no history entry, because Quick Help is a single
// dynamic slot. Called from the key loop when the context topic changes while
// Quick Help is open. When the new topic has no page it KEEPS the current help
// showing (rather than replacing it with the no-help notice), only recording
// the topic so it is not re-resolved every keystroke.
func (e *Editor) refreshQuickHelp(hw *viewport.Viewport) {
	buf, root, wikiName, browse, ok := e.quickHelpDestination()
	if !ok {
		e.quickHelpShownTopic = e.quickHelpTopic // keep the current page; don't retry each key
		return
	}
	if buf != hw.Buffer {
		e.replaceBuffer(hw, buf)
	}
	hw.WikiRoot = root
	hw.WikiName = wikiName
	hw.BrowseActive = browse && hw.ViewState.LinkBrowsing
	e.quickHelpBuf = buf
	e.quickHelpShownTopic = e.quickHelpTopic
	e.applyHelpViewportChrome(hw) // re-fit height to the newly loaded page
	e.ensureCursorVisible(hw)
	e.RequestRender()
}

// updateQuickHelp recomputes quickHelpTopic from the current key prefix and, if
// Quick Help is open in follow mode, re-navigates it when the topic changed.
// Called once per processed key, after the active sequence is settled. It never
// affects the main help — only the dedicated Quick Help slot.
func (e *Editor) updateQuickHelp() {
	e.quickHelpTopic = e.KeyProcessor.HelpTopic(e.ActiveSequence)
	e.syncQuickHelpMode()
	if !e.quickHelpMode {
		return
	}
	if e.quickHelpTopic == e.quickHelpShownTopic {
		return // already showing this topic — don't reload on every keypress
	}
	if hw := e.helpViewport(); hw != nil {
		e.refreshQuickHelp(hw)
	}
}

// quickHelpBuffer builds the placeholder shown when Quick Help has no page for
// the current key context — a single "no help" line, named by the synthetic
// quickHelpDocURL. Quick Help mode is tracked by quickHelpMode (not this URL),
// so the notice needs no special identity beyond a stable name.
func (e *Editor) quickHelpBuffer() *buffer.Buffer {
	buf := e.lib.NewFromString("No quick help is available.")
	buf.SetFilename(quickHelpDocURL)
	return buf
}

// toggleOptions shows the editor-options display, or dismisses it if a second
// invocation arrives while it is already open (mirroring toggleHelp). The
// viewport carries Class "options" so it can be found and removed.
func (e *Editor) toggleOptions() bool {
	// Dismiss an existing options viewport (top dock, Class "options").
	for _, w := range e.ViewportManager.GetViewportsByDock(viewport.DockTop) {
		if w.Class == "options" {
			e.ViewportManager.RemoveViewport(w.ID)
			e.RequestRender()
			return true
		}
	}

	// Build options display, showing the EFFECTIVE values for the last
	// active editor viewport (per-viewport settings govern the viewport).
	optWin := e.ViewportManager.GetLastMainViewport()
	opt := func(name string) string {
		v, _ := e.getOption(optWin, name)
		return v
	}
	var content strings.Builder
	content.WriteString("Editor Options:\n\n")
	content.WriteString(fmt.Sprintf("  Show Line Numbers: %s\n", opt("showLineNumbers")))
	content.WriteString(fmt.Sprintf("  Show Column Ruler: %s\n", opt("showColumnRuler")))
	content.WriteString(fmt.Sprintf("  Ruler Shows Cursor: %s\n", opt("rulerShowsCursor")))
	syntaxName := e.Config.Syntax
	if syntaxName == "" {
		syntaxName = "(none)"
	}
	content.WriteString(fmt.Sprintf("  Syntax: %s\n", syntaxName))
	content.WriteString(fmt.Sprintf("  Syntax Detect: %s\n", opt("syntaxDetect")))
	if ov := opt("syntaxOverrides"); strings.TrimSpace(ov) != "" {
		content.WriteString(fmt.Sprintf("  Syntax Overrides: %s\n", ov))
	}
	var ignores []string
	for _, f := range []struct {
		on    bool
		label string
	}{
		{e.Config.MatchIgnoresSingleQuote, "'..'"},
		{e.Config.MatchIgnoresDoubleQuote, "\"..\""},
		{e.Config.MatchIgnoresSlashStar, "/*..*/"},
		{e.Config.MatchIgnoresSlashSlash, "//.."},
		{e.Config.MatchIgnoresHash, "#.."},
		{e.Config.MatchIgnoresDoubleHyphen, "--.."},
		{e.Config.MatchIgnoresSemicolon, ";.."},
		{e.Config.MatchIgnoresPercent, "%.."},
	} {
		if f.on {
			ignores = append(ignores, f.label)
		}
	}
	if len(ignores) == 0 {
		ignores = append(ignores, "(none)")
	}
	content.WriteString(fmt.Sprintf("  Match Ignores (no grammar): %s\n", strings.Join(ignores, " ")))
	content.WriteString(fmt.Sprintf("  Tab Size: %s\n", opt("tabSize")))
	content.WriteString(fmt.Sprintf("  Show Invisibles: %s\n", opt("showInvisibles")))
	content.WriteString(fmt.Sprintf("  Show Bidi Markers: %s\n", opt("showBidi")))
	content.WriteString(fmt.Sprintf("  Show Marks: %s\n", opt("showMarks")))
	content.WriteString(fmt.Sprintf("  Insert Mode: %s\n", opt("insertMode")))
	content.WriteString(fmt.Sprintf("  Read Only: %s\n", opt("readOnly")))
	content.WriteString(fmt.Sprintf("  Word Wrap: %s\n", opt("wordWrap")))
	content.WriteString(fmt.Sprintf("  Search Ignore Case: %s\n", opt("searchIgnoreCase")))
	content.WriteString(fmt.Sprintf("  Search Wrap: %s\n", opt("searchWrap")))
	content.WriteString(fmt.Sprintf("  Search Regex (standard syntax): %s\n", opt("searchRegex")))
	content.WriteString(fmt.Sprintf("  Search Backwards: %s\n", opt("searchBackwards")))
	content.WriteString(fmt.Sprintf("  Search All Buffers: %s\n", opt("searchAllBuffers")))
	content.WriteString(fmt.Sprintf("  Modebar Location: %s\n", opt("modebarLocation")))
	content.WriteString(fmt.Sprintf("  Page Size Optimal: %s\n", opt("pageSizeOptimal")))
	content.WriteString(fmt.Sprintf("  Page Overlap Minimum: %s\n", opt("pageOverlapMinimum")))
	content.WriteString(fmt.Sprintf("  Page Size Step: %s\n", opt("pageSizeStep")))
	content.WriteString(fmt.Sprintf("  Max Repeat: %s\n", opt("maxRepeat")))
	content.WriteString(fmt.Sprintf("  Kill Ring Entries: %s\n", opt("killRingEntries")))
	content.WriteString(fmt.Sprintf("  Direction: %s\n", opt("direction")))
	content.WriteString(fmt.Sprintf("  Prompt Timeout (s, 0=never): %s\n", opt("promptTimeout")))
	content.WriteString(fmt.Sprintf("  Script Timeout (s, 0=never): %s\n", opt("scriptTimeout")))
	content.WriteString(fmt.Sprintf("\n  Mappings: %s\n", e.LoadedConfig.General.MappingsName))
	content.WriteString(fmt.Sprintf("  Layout: %s\n", e.LoadedConfig.General.Layout))
	content.WriteString("\nInvoke editor_options again to close...")

	buf := e.lib.NewFromString(content.String())
	e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Type:             viewport.ToolViewport,
		ViewportSet:      "help",
		Class:            "options",
		Dock:             viewport.DockTop,
		Priority:         100,
		MinHeight:        8,
		MaxHeight:        15,
		MessageTopCenter: "Editor Options",
		Buffer:           buf,
		ShowLineNumbers:  false,
	})
	e.RequestRender()
	return true
}

// Cleanup performs cleanup when the editor exits.
func (e *Editor) Cleanup() {
	e.releaseAllMewLocks()
	if e.PawScript != nil {
		e.PawScript.Cleanup()
	}
	// Release this editor's own garland library and remove its private
	// cold-storage subfolder, so a long-lived host that opens and closes many
	// mew instances doesn't leak libraries or temp directories.
	if e.lib != nil {
		_ = e.lib.Close()
	}
	if e.coldDir != "" {
		_ = os.RemoveAll(e.coldDir)
	}
}

// PromptForInput creates an input prompt.
func (e *Editor) PromptForInput(prompt, defaultValue string, callback func(string, bool)) {
	e.ViewportManager.CreatePromptViewport(prompt, defaultValue, callback)
	e.RequestRender()
}
