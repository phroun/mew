// Package editor provides the core text editor orchestration.
package editor

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/phroun/ifitfits"
	"github.com/phroun/pawscript"

	"github.com/phroun/key-sequence-processor/keyseq"
	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/config"
	"github.com/phroun/mew/internal/input"
	"github.com/phroun/mew/internal/jsf"
	"github.com/phroun/mew/internal/plugins"
	"github.com/phroun/mew/internal/render"
	"github.com/phroun/mew/internal/version"
	"github.com/phroun/mew/internal/viewport"
)

// Editor is the main editor instance that orchestrates all components.
type Editor struct {
	// Core components
	ViewportManager *viewport.Manager
	LayoutManager   *viewport.LayoutManager
	Renderer        *render.ScreenRenderer
	KeyProcessor    *keyseq.Processor
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

	// tileMode is the armed tiling operator that the directional dispatch
	// commands (viewport_up/down/left/right) carry out: "go" (default, and the
	// empty value — a focus-moving directional nav), "swap", "merge", "split", or
	// "new".
	// It is set persistently by `viewport_<op> mode` (a toggle: re-selecting the
	// active mode reverts to "go"). tilePending is a one-shot operator armed by
	// `viewport_<op> pending` that overrides tileMode for the very next directional
	// press and then clears, restoring tileMode.
	tileMode    string
	tilePending string

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

	// stackTabCounters maps a viewport id to the "[i/n]" stack-tab counter
	// applyTilerGeometry stamped into its top-left message last frame, so the
	// next frame can clear a stale counter (a tab that was unstacked or is no
	// longer the shown one) without disturbing a message set by anything else.
	stackTabCounters map[string]string

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
	niqqudNudgeWantPrevious bool
	niqqudNudgeToggleRun    int
	niqqudNudgeLatched      bool
	niqqudNudgeLatchSig     string
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
	// repeatingKey names the arriving key when it came in as a REPEAT — a held
	// key, marked ":Repeat" by a terminal reporting event types. The marker is
	// stripped before the key processor sees it, because mew's keymap has no
	// repeat token and a held arrow key has to go on moving the cursor; it is
	// put back by tinput_key alone, so the child in a terminal pane learns the
	// key was held rather than struck again. Empty for an ordinary press, and
	// compared against dispatchingKey so a sequence unwind — which runs several
	// keys' commands within one dispatch — marks only the key that repeated.
	repeatingKey string

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
	// undoSent/redoSent/undoPushed: the last focused-buffer undo and redo
	// availability pushed through Config.UndoState (see notifyUndoState).
	undoSent, redoSent bool
	undoPushed         bool
	// unsavedSent/unsavedPushed: the last "is there modified work anywhere in
	// this session" answer pushed through Config.UnsavedState (see
	// notifyUnsavedState).
	unsavedSent   bool
	unsavedPushed bool

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

	// KeyChordText is the HOST's answer to what its keyboard typed for a
	// chord, asked before mew's table (see mew.WithKeyChordText). nil on a
	// host that cannot see the pairing.
	KeyChordText func(chord string) (string, bool)

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

	// UndoState, when set, is told whether the FOCUSED viewport's buffer has a
	// change to undo and one to redo, once at the first render and thereafter
	// on transitions, so a host can enable its Edit-menu Undo and Redo to match.
	// Called from the editor loop.
	UndoState func(canUndo, canRedo bool)

	// UnsavedState, when set, is told whether ANY buffer this session holds
	// open is modified — the active ones and the work stacked behind a link
	// follow alike — once at the first render and thereafter on transitions.
	// A host asks this question of a window it is about to close: unsaved
	// work is why a close must be refused and turned into a prompt rather
	// than performed. Called from the render path.
	UnsavedState func(unsaved bool)

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

	// SuspendHost, when set, backs the host_suspend command: the host hands
	// its terminal back to the shell and stops until the shell continues it,
	// then returns true. It returns false when the host cannot suspend (a
	// graphical host, or no job control), and the command then fails so a
	// binding can fall through to the next command in its chain. Unset, as in
	// the standalone editor, host_suspend fails the same way.
	//
	// Runs on mew's main loop and blocks it for as long as the process is
	// stopped, which from the loop's point of view is no time at all.
	SuspendHost func() bool

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
	kittyFlipActive := flippingForKitty(flip)
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
	e.KeyProcessor = keyseq.NewProcessor(e.runBoundCommand)
	e.KeyProcessor.SetFallbackGroups(mewFallbackGroups)
	e.KeyProcessor.SetDefaultHandler(e.defaultCommandForKey)

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
		}, e.hostPaste)
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

	// Anything the config files said that mew could not honor, beside the
	// launch files. Before the evals, so an eval's own output buffer lands on
	// top of it with the focus.
	e.showStartupLog()

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
			// Text the terminal received with no key behind it — an input
			// method's commit — is put in the document, never dispatched as a
			// keystroke. See committedtext.go.
			if e.handleCommittedText(event.Key) {
				if e.renderRequested.Load() {
					e.performRender()
				}
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
