// Package editor provides the core text editor orchestration.
package editor

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
	"github.com/phroun/mew/internal/window"
)

// statusWriter captures stderr output and stores it for display.
type statusWriter struct {
	editor *Editor
	mu     sync.Mutex
	buf    bytes.Buffer
}

func (sw *statusWriter) Write(p []byte) (n int, err error) {
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
	WindowManager *window.Manager
	LayoutManager *window.LayoutManager
	Renderer      *render.ScreenRenderer
	KeyProcessor  *keys.SequenceProcessor
	KeyHandler    input.Source
	PawScript     *pawscript.PawScript
	PromptMgr     *PromptManager

	// pawConfig is the live PawScript configuration; DefaultTokenTimeout is
	// read at each host-level token request, so runtime option changes
	// (set_option scriptTimeout) apply immediately.
	pawConfig *pawscript.Config

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

	// mew accesses the "mew:///" support tree (config, profile, syntax, native
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
	// in the modebar. It is transient key-sequence state, not window context.
	activeCompletions string

	// quickHelpTopic is the context-sensitive help topic for the current key
	// prefix — the deepest "help" virtual binding matching the active sequence
	// (e.g. "" at rest resolves the root "help =", "^B" resolves "^B help ="). It
	// names the help wiki page Quick Help shows; empty falls back to the built-in
	// reference. It ONLY drives Quick Help — the main help ("Using mew") ignores
	// it entirely.
	quickHelpTopic string
	// quickHelpMode marks the docked help window as Quick Help (the dynamic,
	// context-following page) rather than an ordinary help page reached by
	// browsing. It is what the "Quick Help" checkmark reflects, and what keeps
	// Quick Help identifiable apart from a normal help page showing the same URL.
	quickHelpMode bool
	// quickHelpBuf is the buffer currently displayed AS Quick Help, and
	// quickHelpShownTopic the topic it was built from. If the help window's
	// buffer drifts from quickHelpBuf the user browsed away (link/history/Using
	// mew), which ends quick mode; if quickHelpTopic drifts from
	// quickHelpShownTopic the context changed and Quick Help re-navigates.
	quickHelpBuf        *buffer.Buffer
	quickHelpShownTopic string

	// Cross-buffer find state (the find command's "a" option). When set,
	// find_next continues across all main buffers instead of using the
	// focused window's per-window find state.
	globalFind    window.FindState
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

	// pendingScreenCapture, when non-empty, is a mew:/// target the next
	// performRender writes a full-frame ANSI snapshot to (the debug_screen
	// command). Set and read under renderMu, so the capture rides the natural
	// render loop — snapshotting the exact frame just painted — rather than a
	// command re-rendering (and re-locking renderMu) itself.
	pendingScreenCapture string

	// appliedFocusedSig is the focused window's overlay signature whose
	// focused-scoped options (modebar templates/location, macOptionKeys, key
	// mappings) are currently applied. Re-derived when it changes.
	appliedFocusedSig string

	// flipBidiForHost=auto probe state (see bidiprobe.go). realTerminal is
	// whether output goes to the actual terminal (probing a virtualized host's
	// capture buffer is meaningless — hosts set the option explicitly).
	bidiProbeState    int
	bidiProbeDeadline time.Time
	realTerminal      bool
	// appliedMappingSet is the mapping-set name currently loaded into the key
	// processor, so an unchanged set is not rebuilt.
	appliedMappingSet string

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
	mousePressed   pressedLink // the CAPTURED button (press-to-release)
	// mouseOnCaptured: the pointer is currently over the captured button
	// (paints pressed); dragging off reverts to the focused style while the
	// capture holds.
	mouseOnCaptured bool
	mouseHovered    pressedLink
	// pointerRegionSent / pointerArrowsSent are the last I-beam rectangle and
	// button-exclusion spans pushed through Config.PointerRegion (1-based
	// cells); pointerRegionPushed records whether anything was pushed yet, so
	// the first computed region is always sent even when it is the zero value.
	pointerRegionSent   [4]int
	pointerArrowsSent   []PointerArrowSpan
	pointerRegionPushed bool
	// helpStateSent/helpStatePushed: the last built-in-help-window open state
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
	// Drag selection (mouse.go): a left press in the focused window's content
	// area arms this; dragging then marks the block (begin at the press
	// origin, end following the pointer). A shift+click arms it pre-begun
	// with the ORIGINAL caret position as the origin.
	dragSel dragSelState
	// Drag-edge autoscroll (mouse.go): while a drag selection holds the
	// pointer beyond (or at) the window's edges, a ticker scrolls the view —
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
	// window.
	modebarNavCapture int
	modebarNavOn      bool
	modebarNavHover   int
	// readOnlySent/readOnlyPushed: the last focused-window read-only state
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
	synSGR              map[*jsf.ColorRef]string

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

// Config holds editor configuration options.
type Config struct {
	ShowLineNumbers  bool
	ShowColumnRuler  bool
	RulerShowsCursor bool
	TabSize          int
	ShowInvisibles   bool
	ShowBidi         bool
	RtlCombining     bool   // show combining marks on RTL letters (default true)
	ShowMarks        string // "no" | "yes" | "all"
	OverwriteMode    bool   // inverse of insertMode; zero value = insert
	ReadOnly         bool
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

	// Editing locks (see config.GeneralConfig): UseLocks gates all locking;
	// UseEmacsLocks additionally gates the emacs-interoperable lock files.
	UseLocks         bool
	UseEmacsLocks    bool
	WordWrap         bool
	DebounceMs       int
	MaxRenderDelayMs int

	// Search defaults (JOE-compatible): SearchIgnoreCase mirrors -icase,
	// SearchWrap mirrors -wrap, SearchRegex mirrors -regex (standard regex
	// syntax by default instead of the JOE backslash syntax).
	SearchIgnoreCase bool
	SearchWrap       bool
	SearchRegex      bool

	// ModebarLocation places the modebar on the "top" (default) or
	// "bottom" screen line. ModebarInner/Default/Outer are the base modebar
	// templates; MappingsName is the base key-mapping set. All four are the
	// base for the focused window's overlay (see reconcileFocusedOptions).
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

	// MewFS, when set, virtualizes mew's own support tree (the "mew:///" scheme —
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

	// PointerRegion, when set, publishes where a graphical host should show the
	// text I-beam: the FOCUSED window's editable content area (its cells,
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

	// HelpState, when set, is told whether the built-in help window (the
	// WordStar command reference toggled by help_toggle) is open, once at the
	// first render and thereafter on transitions — so a host can keep a "Quick
	// Help" menu checkmark in sync with it. Called from the render path.
	HelpState func(open bool)

	// EditState, when set, is told whenever the FOCUSED window's read-only
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
	// hide_desktop commands. A host that embeds mew as a window-manager surface
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
	// the EDITING AREA of the focused window (never the modebar, gutters,
	// column ruler, or title/message rows) with the click's 1-based terminal
	// cell. A host pops its context menu there (Cut/Copy/Paste/Select All,
	// wired back through a HostPort). Left unset, right-clicks are swallowed.
	ShowContextMenu func(col, row int)

	// HostPort, when set, is bound to the session at startup so the host can
	// inject editor commands (port.Execute("os_copy")) from its own threads:
	// each command is marshaled onto the editor main loop. See HostPort.
	HostPort *HostPort

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
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		ShowLineNumbers:  true,
		ShowColumnRuler:  true,
		TabSize:          4,
		ShowInvisibles:   false,
		ShowBidi:         false,
		RtlCombining:     true,
		ShowMarks:        "no",
		OverwriteMode:    false, // insertMode=yes
		ReadOnly:         false,
		WordWrap:         false,
		DebounceMs:       20,
		MaxRenderDelayMs: 100,
		SearchWrap:       true,
	}
}

// New creates a new Editor instance.
func New(cfg Config) (*Editor, error) {
	// Create window manager
	wm := window.NewManager()

	// Create layout manager
	lm := window.NewLayoutManager(wm)

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
	cfg.ShowColumnRuler = loadedConfig.General.ShowColumnRuler
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
	// Explicit setting applies now; "auto" stays off until the probe decides
	// (triggered by the first frame containing RTL content).
	renderer.SetFlipBidiForHost(cfg.FlipBidiForHost == "true")

	// Restore a host-provided state snapshot over the loaded configuration.
	applyInitialState(&cfg)

	// Create editor instance first (without PawScript)
	e := &Editor{
		WindowManager:    wm,
		LayoutManager:    lm,
		Renderer:         renderer,
		Config:           cfg,
		FS:               docFS,
		usingOSFS:        usingOSFS,
		realTerminal:     cfg.Terminal == nil,
		mew:              mewVFS,
		home:             hostHome(&cfg),
		lib:              lib,
		coldDir:          coldDir,
		ConfigMgr:        configMgr,
		LoadedConfig:     loadedConfig,
		configFromDisk:   configFromDisk,
		bufNotices:       make(map[*buffer.Buffer][]bufferNotice),
		mewLocks:         make(map[*buffer.Buffer]string),
		linkVisitSeen:    make(map[string]bool),
		linkResolveCache: make(map[string]string),
	}

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

	// The column ruler is not a window of its own: the renderer draws it on the
	// top line of any window whose ShowRuler view option is enabled.
	renderer.SetRulerRenderer(e.renderColumnRuler)

	// Peek-indicator labels run through the modebar %CODE% engine so codes like
	// %SPU% resolve to the live peek-command bindings.
	renderer.SetPeekLabelResolver(func(raw string) string {
		return plugins.ExpandModebar(raw, e.peekBindingValues())
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
	renderer.SetCaretHiddenFn(func(w *window.Window) bool { return e.focusedLinkButton(w) != nil })

	// Register editor commands with PawScript
	e.registerCommands()

	// Create key sequence processor with command executor
	e.KeyProcessor = keys.NewSequenceProcessor(e.executeCommand)

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
		cfg.HostPort.bind(e.PostAction, e.executeCommand)
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

	return e, nil
}

// applyMacOptionKeys pushes the macOptionKeys option into the input decoder
// and the key processor's reverse-insert fallback.
func (e *Editor) applyMacOptionKeys() {
	fw := e.WindowManager.GetFocusedWindow()
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
func (e *Editor) renderModebar(w *window.Window, screenWidth int) string {
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
// runs at render time, so if per-window keymaps are ever added the focused
// window's map is the natural source.
func (e *Editor) peekBindingValues() map[string]string {
	vals := make(map[string]string, len(peekBindingCommands))
	for code, cmd := range peekBindingCommands {
		vals[code] = e.KeyForCommand(cmd)
	}
	return vals
}

// KeyForCommand returns the key sequence bound to command, in the exact
// spelling it is stored under. When several keys map to it the
// lexicographically-first is returned (stable); "" when the command is unbound.
func (e *Editor) KeyForCommand(command string) string {
	best := ""
	for key, cmd := range e.KeyProcessor.GetAllMappings() {
		if cmd == command && (best == "" || key < best) {
			best = key
		}
	}
	return best
}

// renderColumnRuler renders the column ruler line for a window with the
// ShowRuler view option enabled. With rulerShowsCursor on, the cursor's
// column(s) — caret, ghost, and secondary bidi cursor — are marked with the
// rulerCursor color.
func (e *Editor) renderColumnRuler(w *window.Window, screenWidth int) string {
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

// registerCommands registers all editor commands with PawScript.
func (e *Editor) registerCommands() {
	ps := e.PawScript

	// System commands
	ps.RegisterCommand("exit", func(ctx *pawscript.Context) pawscript.Result {
		e.Running = false
		return pawscript.BoolStatus(true)
	})

	// show_desktop / hide_desktop ask the embedding host to reveal or hide its
	// desktop (e.g. a KittyTK window-manager host). No-ops in the standalone
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

	// nav_cancel: turn link browse mode off on the focused window. Fails when
	// browse mode is not active, so nav_cancel|cancel|... chains fall through.
	ps.RegisterCommand("nav_cancel", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navCancel())
	})

	// nav_follow: activate the focused link button (transient notification for
	// now). Fails when not in active browse mode, so nav_follow|accept|insert
	// falls through to the normal Enter behavior.
	ps.RegisterCommand("nav_follow", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navFollow())
	})

	// nav_next / nav_prior: move to the next/previous link (cycling). Capture
	// only when a button is focused, so a tab = nav_next|... chain yields to
	// editing when the caret is not inside a link.
	ps.RegisterCommand("nav_next", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navLink(+1))
	})
	ps.RegisterCommand("nav_prior", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navLink(-1))
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

	// nav_history_prior / nav_history_next: walk the focused window's
	// buffer-swap history (following a link swaps the buffer in place,
	// stacking the departed binding — see Window.SwapBuffer). Prior returns
	// to where you were; next re-advances. Both fail when there is no history
	// in that direction, so chains fall through.
	ps.RegisterCommand("nav_history_prior", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navHistory(-1))
	})
	ps.RegisterCommand("nav_history_next", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navHistory(+1))
	})

	// nav_clear forgets every visited link (editor-wide repaint to the
	// unvisited style). nav_history_clear empties the focused window's whole
	// back/forward history, releasing stacked bindings except any that hold
	// the LAST reference to a buffer — those move to the window's graveyard,
	// held for the eventual save decision.
	ps.RegisterCommand("nav_clear", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navClearVisited())
	})
	ps.RegisterCommand("nav_history_clear", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navHistoryClear())
	})

	ps.RegisterCommand("cancel", func(ctx *pawscript.Context) pawscript.Result {
		focusedWindow := e.WindowManager.GetFocusedWindow()
		if focusedWindow != nil && focusedWindow.Type == window.PromptWindow {
			// Capture callbacks before removing window
			legacyCallback := focusedWindow.Callback
			promptCallback := focusedWindow.PromptCallback

			// Remove prompt window FIRST so focus returns to main buffer
			e.WindowManager.RemoveWindow(focusedWindow.ID)

			// Call the appropriate callback
			if promptCallback != nil {
				promptCallback(false, "", "")
			} else if legacyCallback != nil {
				legacyCallback("", false)
			}
			return pawscript.BoolStatus(true)
		}
		return pawscript.BoolStatus(false)
	})

	ps.RegisterCommand("accept", func(ctx *pawscript.Context) pawscript.Result {
		focusedWindow := e.WindowManager.GetFocusedWindow()
		if focusedWindow != nil && focusedWindow.Type == window.PromptWindow {
			// Capture callbacks before removing window
			legacyCallback := focusedWindow.Callback
			promptCallback := focusedWindow.PromptCallback

			// Get buffer content from line 0 (for backward compatibility)
			bufferContent := ""
			if focusedWindow.Buffer != nil && focusedWindow.Buffer.GetLineCount() > 0 {
				bufferContent = strings.TrimRight(focusedWindow.Buffer.GetLine(0), "\n\r")
			}

			// Get the text from the line where the cursor is positioned
			// This is the key difference from TypeScript - we read cursor line, not line 0
			cursorLineText := ""
			if focusedWindow.Buffer != nil {
				cursorLine := focusedWindow.CursorPos().Line
				if cursorLine < focusedWindow.Buffer.GetLineCount() {
					cursorLineText = strings.TrimRight(focusedWindow.Buffer.GetLine(cursorLine), "\n\r")
				}
			}

			// Remove prompt window FIRST so focus returns to main buffer
			// This ensures any output from the callback goes to the right window
			e.WindowManager.RemoveWindow(focusedWindow.ID)

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
		// Show in a work buffer window
		buf := e.lib.NewFromString(content.String())
		e.WindowManager.CreateWindow(window.WindowOptions{
			Type:             window.ToolWindow,
			WindowSet:        "help",
			Class:            "mappings",
			Dock:             window.DockTop,
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
		w := e.WindowManager.GetFocusedWindow()
		if w != nil && w.Buffer != nil {
			w.Buffer.Balance()
		}
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("debug_marks", func(ctx *pawscript.Context) pawscript.Result {
		w := e.WindowManager.GetFocusedWindow()
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
		w := e.WindowManager.GetFocusedWindow()
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

	// completion invokes the focused window's completion handler, if it has
	// one (filename prompts do). It returns that handler's result; with no
	// handler, or when the handler declines, it fails — so a binding like
	// completion|insert '\t' falls through to inserting a tab.
	ps.RegisterCommand("completion", func(ctx *pawscript.Context) pawscript.Result {
		w := e.WindowManager.GetFocusedWindow()
		if w == nil || w.CompletionCallback == nil {
			return pawscript.BoolStatus(false)
		}
		return pawscript.BoolStatus(w.CompletionCallback())
	})

	// rtl reports whether the caret currently sits inside a right-to-left
	// segment of its line (resolved under the configured base direction).
	ps.RegisterCommand("rtl", func(ctx *pawscript.Context) pawscript.Result {
		w := e.WindowManager.GetFocusedWindow()
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
		e.deleteCharBefore()
		e.trackEdit()
		e.editCoalesced = true // a single-point edit: coalesce the undo run
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("del_char_next", func(ctx *pawscript.Context) pawscript.Result {
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
		if w := e.WindowManager.GetFocusedWindow(); w != nil {
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

	ps.RegisterCommand("insert", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) > 0 {
			text := fmt.Sprintf("%v", ctx.Args[0])
			e.insertText(text)
			e.trackEdit()
			e.editCoalesced = true // a single-point edit: coalesce the undo run
			return pawscript.BoolStatus(true)
		}
		return pawscript.BoolStatus(false)
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

	// Undo/Redo (using Garland's versioning, TypeScript naming convention)
	ps.RegisterCommand("buffer_undo", func(ctx *pawscript.Context) pawscript.Result {
		w := e.WindowManager.GetFocusedWindow()
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
		w := e.WindowManager.GetFocusedWindow()
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
		w := e.WindowManager.GetFocusedWindow()
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
		w := e.WindowManager.GetFocusedWindow()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		w.Buffer.EndUserCommand()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("buffer_tx_cancel", func(ctx *pawscript.Context) pawscript.Result {
		w := e.WindowManager.GetFocusedWindow()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		w.Buffer.CancelUserCommand()
		e.syncCursorAfterUndoRedo(w) // rolled-back content: resync the caret
		return pawscript.BoolStatus(true)
	})

	// Mark commands
	ps.RegisterCommand("set_mark", func(ctx *pawscript.Context) pawscript.Result {
		w := e.WindowManager.GetFocusedWindow()
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
		// (a second window, an async script) slides the pending mark along
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
		w := e.WindowManager.GetFocusedWindow()
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
		w := e.WindowManager.GetFocusedWindow()
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
		w := e.WindowManager.GetFocusedWindow()
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
	// stacked in a window's nav history (unsaved work parked behind a link
	// follow). With the argument "true" it is NON-INTERACTIVE (for save-and-quit,
	// `buffer_save_all true & exit`): it never prompts, skips unnamed/changed
	// buffers with a notice, and returns false if anything was skipped or failed.
	// Otherwise it is INTERACTIVE: it prompts per file (name / overwrite / create
	// directory), and ANY ^C bails the whole remaining batch with a false result.
	ps.RegisterCommand("buffer_save_all", func(ctx *pawscript.Context) pawscript.Result {
		var pending []*buffer.Buffer
		for _, b := range e.openDocWindows() {
			if b.IsModified() {
				pending = append(pending, b)
			}
		}
		if arg, ok := argString(ctx, 0); ok && strings.EqualFold(strings.TrimSpace(arg), "true") {
			return pawscript.BoolStatus(e.saveAllNonInteractive(pending))
		}
		if len(pending) == 0 {
			e.ShowNotification("No modified files to save")
			return pawscript.BoolStatus(true)
		}
		// Interactive: the prompts are async. If the whole batch finishes without
		// suspending on a prompt (every buffer saved cleanly), report the result
		// synchronously; otherwise defer it through a token so a following `& exit`
		// waits for — and respects — the outcome. The prompt callbacks cannot fire
		// until this command returns to the main loop, so `token` is always set
		// before any resume runs.
		token := ""
		completedSync, syncResult := false, false
		e.saveAllSequential(pending, saveAllTally{}, func(success bool) {
			if token == "" {
				completedSync, syncResult = true, success
			} else {
				ctx.ResumeToken(token, success)
			}
		})
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

	// window_clone opens a second window onto the SAME buffer (not a content
	// copy like buffer_duplicate) so you can edit in two places at once and switch
	// between them. Each window keeps its own caret and viewport.
	ps.RegisterCommand("window_clone", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.cloneCurrentWindow())
	})

	ps.RegisterCommand("buffer_close", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.closeCurrentBuffer())
	})

	// buffer_revert seeks the buffer's history back to its last save point.
	// A pure history move: redo still reaches the abandoned edits.
	ps.RegisterCommand("buffer_revert", func(ctx *pawscript.Context) pawscript.Result {
		w := e.WindowManager.GetFocusedWindow()
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
	// timestamped ".ans" file in the mew:/// support tree (~/.mew locally) — a
	// capture that reproduces the screen when cat'd to a terminal. The write
	// rides the NEXT render (see performRender): the command only arms the target
	// and forces a full repaint, so it never re-renders (or re-locks renderMu)
	// itself — snapshotting the exact frame that gets painted.
	ps.RegisterCommand("debug_screen", func(ctx *pawscript.Context) pawscript.Result {
		e.pendingScreenCapture = "mew:///" + time.Now().Format("2006-01-02 15.04.05") + ".ans"
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
		e.WindowManager.CreateWindow(window.WindowOptions{
			Type:             window.ToolWindow,
			WindowSet:        "help",
			Class:            "bufstatus",
			Dock:             window.DockTop,
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

	ps.RegisterCommand("buffer_prev", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.cycleBuffer(-1))
	})

	ps.RegisterCommand("buffer_list", func(ctx *pawscript.Context) pawscript.Result {
		// A second invocation while the list is showing dismisses it (like
		// help_toggle / editor_options).
		for _, w := range e.WindowManager.GetWindowsByDock(window.DockTop) {
			if w.Class == "buffer_list" {
				e.WindowManager.RemoveWindow(w.ID)
				e.RequestRender()
				return pawscript.BoolStatus(true)
			}
		}
		mainBuffers := e.contentWindows()
		if len(mainBuffers) == 0 {
			e.ShowWarning("No open buffers")
			return pawscript.BoolStatus(false)
		}
		// Build list content
		var content strings.Builder
		content.WriteString("Open Buffers:\n")
		for i, w := range mainBuffers {
			filename := "[unnamed]"
			if w.Buffer != nil {
				fn := w.Buffer.GetFilename()
				if fn != "" {
					filename = fn
				}
			}
			modified := ""
			if w.Buffer != nil && w.Buffer.IsModified() {
				modified = " [modified]"
			}
			focused := ""
			if w == e.WindowManager.GetFocusedWindow() {
				focused = " *"
			}
			content.WriteString(fmt.Sprintf("  %d: %s%s%s\n", i+1, filename, modified, focused))
		}
		// Show in a work buffer window
		buf := e.lib.NewFromString(content.String())
		e.WindowManager.CreateWindow(window.WindowOptions{
			Type:             window.ToolWindow,
			WindowSet:        "help",
			Class:            "buffer_list",
			Dock:             window.DockTop,
			Priority:         100,
			MinHeight:        3,
			MaxHeight:        10,
			MessageTopCenter: "Buffers",
			Buffer:           buf,
			ShowLineNumbers:  false,
		})
		e.RequestRender()
		return pawscript.BoolStatus(true)
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
		// options applies only to the invocation that gave it.
		if !e.findStep(state, 1, true) {
			e.ShowNotification("Not found: " + state.Term)
			return pawscript.BoolStatus(false)
		}
		return pawscript.BoolStatus(true)
	})

	// find_replace [term], [replacement] - find with replace mode forced;
	// prompts for whichever of the two is missing.
	ps.RegisterCommand("find_replace", func(ctx *pawscript.Context) pawscript.Result {
		term, haveTerm := argString(ctx, 0)
		replacement, haveReplacement := argString(ctx, 1)
		e.startFind(term, "r", replacement, haveTerm, true, haveReplacement)
		return pawscript.BoolStatus(true)
	})

	// verbose_log appends text to the shared verbose-log window (class
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
	// tracked per-window (like Find); the command dispatcher consumes it.
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
			w.Repeat = window.RepeatState{Pending: true, Count: n}
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
	ps.RegisterCommand("scroll_left", func(ctx *pawscript.Context) pawscript.Result {
		w := e.WindowManager.GetFocusedWindow()
		if w == nil {
			return pawscript.BoolStatus(false)
		}
		if w.ViewState.ViewOffsetX > 0 {
			w.ViewState.ViewOffsetX -= 8
			if w.ViewState.ViewOffsetX < 0 {
				w.ViewState.ViewOffsetX = 0
			}
			e.RequestRender()
			return pawscript.BoolStatus(true)
		}
		return pawscript.BoolStatus(false)
	})

	ps.RegisterCommand("scroll_right", func(ctx *pawscript.Context) pawscript.Result {
		w := e.WindowManager.GetFocusedWindow()
		if w == nil {
			return pawscript.BoolStatus(false)
		}
		w.ViewState.ViewOffsetX += 8
		e.RequestRender()
		return pawscript.BoolStatus(true)
	})

	// Window navigation commands
	ps.RegisterCommand("window_next", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.WindowManager.FocusNextWindow()
		if ok {
			e.announceFocusedWindow()
		}
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("window_prior", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.WindowManager.FocusPrevWindow()
		if ok {
			e.announceFocusedWindow()
		}
		return pawscript.BoolStatus(ok)
	})

	// Peek commands
	ps.RegisterCommand("stat_peek_up", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.WindowManager.StatPeekUp())
	})

	ps.RegisterCommand("stat_peek_down", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.WindowManager.StatPeekDown())
	})

	ps.RegisterCommand("prompt_peek_up", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.WindowManager.PromptPeekUp())
	})

	ps.RegisterCommand("prompt_peek_down", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.WindowManager.PromptPeekDown())
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
	// active editor window (NOTE: arguments are comma-separated).
	ps.RegisterCommand("set_option", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) < 1 {
			e.ShowWarning("Usage: set_option <name>[, <value>]")
			return pawscript.BoolStatus(false)
		}
		name := fmt.Sprintf("%v", ctx.Args[0])
		// Target the last active main-buffer window, not whatever is focused
		// (a prompt window would be focused and is about to close).
		w := e.WindowManager.GetLastMainWindow()
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
			w := e.WindowManager.GetLastMainWindow()
			return pawscript.BoolStatus(e.rotateOption(w, name, dir))
		}
	}
	ps.RegisterCommand("set_option_next", rotate(+1))
	ps.RegisterCommand("set_option_prior", rotate(-1))

	// clear_option <name> - drop a per-window option's explicit override on the
	// active window, reverting it to the resolved default (the configured /
	// inherited value). Fails for global options and unknown names.
	ps.RegisterCommand("clear_option", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) < 1 {
			e.ShowWarning("Usage: clear_option <name>")
			return pawscript.BoolStatus(false)
		}
		name := fmt.Sprintf("%v", ctx.Args[0])
		w := e.WindowManager.GetLastMainWindow()
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
		w := e.WindowManager.GetLastMainWindow()
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
// string) for the given main-buffer window. Per-window options are read from the
// window's view state (what actually renders); globals from the runtime Config.
func (e *Editor) getOption(w *window.Window, name string) (string, bool) {
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
	case "linkbrowsing":
		v := e.Config.LinkBrowsing
		if w != nil {
			v = w.ViewState.LinkBrowsing
		}
		return boolText(v), true
	case "showcolumnruler":
		v := e.Config.ShowColumnRuler
		if w != nil {
			v = w.ViewState.ShowRuler
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
		// Per-window override when set, else the global base direction.
		if w != nil && w.ViewState.Direction != "" {
			return w.ViewState.Direction, true
		}
		if e.Config.Direction == "rtl" {
			return "rtl", true
		}
		return "ltr", true
	case "flipbidiforhost":
		return e.Config.FlipBidiForHost, true
	case "prompttimeout":
		return strconv.Itoa(e.Config.PromptTimeout), true
	case "scripttimeout":
		return strconv.Itoa(e.Config.ScriptTimeout), true
	case "debouncems":
		return strconv.Itoa(e.Config.DebounceMs), true
	case "maxrenderdelayms":
		return strconv.Itoa(e.Config.MaxRenderDelayMs), true
	}
	return "", false
}

// setOption sets a named editor option. Per-window options (tabSize,
// showLineNumbers, showInvisibles, showColumnRuler) are written to the given
// window's own ViewState, so the change applies to that window and does not
// leak into the editor defaults or future windows. Global options are written to the runtime
// Config. Nothing is written to config.GeneralConfig / the on-disk config.
func (e *Editor) setOption(w *window.Window, name, value string) bool {
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
	// Setting a per-window option on a specific window is a deliberate choice:
	// pin it so the grammar options overlay does not overwrite it later.
	if w != nil && cliPerWindowOptions[lname] {
		w.MarkOptionOverridden(lname)
	}

	switch lname {
	// Per-window options: write the window's ViewState (fall back to the
	// editor default only when there is no window).
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
		} else {
			e.Config.ReadOnly = b
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
	case "modebarlocation":
		loc := strings.ToLower(strings.TrimSpace(value))
		if loc != "top" && loc != "bottom" {
			e.ShowWarning(name + " must be top or bottom")
			return false
		}
		e.Config.ModebarLocation = loc
		e.Modebar.SetLocation(e.optStr(e.WindowManager.GetFocusedWindow(), "modebarlocation", loc))
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
			// Per-window base direction (rendering reads ViewState.Direction
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
		if v == "auto" {
			e.bidiProbeState = bidiProbeIdle // re-arm detection
		} else {
			e.bidiProbeState = bidiProbeDone // explicit choice wins
			e.Renderer.SetFlipBidiForHost(v == "true")
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
	case "debouncems":
		n, ok := parseInt(0)
		if !ok {
			return false
		}
		e.Config.DebounceMs = n
	case "maxrenderdelayms":
		n, ok := parseInt(0)
		if !ok {
			return false
		}
		e.Config.MaxRenderDelayMs = n
	default:
		e.ShowWarning("Unknown option: " + name)
		return false
	}

	e.ShowNotification("Option '" + name + "' set to " + value)
	e.RequestRender()
	return true
}

// executeCommand executes a command string via PawScript.
// Errors are captured via the custom stderr writer and shown as transient
// error windows.
//
// Undo grouping is NOT a blanket per-command transaction. Plain typing and
// character deletes run as bare mutations that garland coalesces into one
// revision (a typing streak = one undo step); compound commands open their own
// transaction in their handler (and scripts can with buffer_tx_*). After each
// dispatch this closes any transaction a script left open and, unless the
// command was one of the coalescible edits, bakes the undo run — so a cursor
// move or any other command ends the streak and the next edit starts fresh.
func (e *Editor) executeCommand(command string) {
	if command == "" {
		return
	}

	// Before running any command, expire transient notification/error/warning
	// windows that have been on screen longer than 5 seconds.
	e.expireStaleNotifications()

	// Content mutations (read-only windows, focused link buttons) are NOT gated
	// here by command name: the script language can rename or chain any command,
	// so a name is not a reliable signal of what a command does. The lock is
	// enforced inside each mutation's implementation instead — the one place
	// that always sees the actual buffer change (see contentLocked).
	fw := e.WindowManager.GetFocusedWindow()

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
	e.PawScript.ExecuteAsync(command)

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
}

// applyRepeatNext consumes a pending repeat_next arm on the target window,
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
	w.Repeat = window.RepeatState{} // one-shot: consume the arm
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

// contentLocked reports whether the focused window currently forbids content
// mutations — it is read-only, or a link button is focused (the caret is inert
// inside it, its source text protected until nav_cancel) — and warns when it
// does. Every mutation's implementation calls this at the point it would change
// the buffer, so the lock holds no matter how the command was named, aliased,
// or chained in the script language. There is deliberately no command-name
// list: a name cannot tell you what a command does.
func (e *Editor) contentLocked() bool {
	return e.windowEditLocked(e.WindowManager.GetFocusedWindow())
}

// windowEditLocked is contentLocked for a specific window — used where the
// mutating implementation names its target window directly (find/replace
// applies to the match's window, not necessarily the focused one).
func (e *Editor) windowEditLocked(w *window.Window) bool {
	if w == nil {
		return false
	}
	if w.ViewState.ReadOnly {
		e.ShowWarning("Buffer is read-only")
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
	w := e.WindowManager.GetFocusedWindow()
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

// tabSize returns the effective tab size for a window. Per-window settings
// govern the window, so cursor math must use this — the window's own
// ViewState.TabSize — rather than the global e.Config.TabSize default.
func (e *Editor) tabSize(w *window.Window) int {
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
func (e *Editor) ensureIdealColumn(w *window.Window) {
	if w == nil || w.Buffer == nil {
		return
	}
	if w.IdealVisualColumn != 0 || w.CursorPos().Rune == 0 {
		return
	}
	tabSize := e.tabSize(w)
	line := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
	w.IdealVisualColumn = e.idealColumn(w, line, w.CursorPos().Rune, tabSize)
}

// afterVerticalMovement applies ghost cursor logic after up/down movement.
// It tries to position cursor at the ideal visual column, showing a ghost
// cursor if the line is shorter than the ideal. Callers must establish the
// ideal column (via ensureIdealColumn) before changing the cursor's line.
func (e *Editor) afterVerticalMovement(w *window.Window) {
	if w == nil || w.Buffer == nil {
		return
	}

	// Normal vertical movement is not link nav: drop any vertical nav ideal
	// (navVert saves/restores it around its own paging fallback).
	w.NavIdealSet = false

	tabSize := e.tabSize(w)

	// Get line content (without trailing newline)
	line := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
	lineLen := len([]rune(line))

	// direction=rtl: the view is right-anchored, so the ideal is a READING
	// column (distance back from the reading start). Placement and the ghost
	// mirror the LTR logic below, but in reading space so screen X is held.
	if e.winRTL(w) {
		idealReading := w.IdealVisualColumn
		vw := e.lineVisualWidth(w, line, tabSize)
		if lineLen == 0 {
			// Empty line: the caret can only sit at the reading start; a
			// non-trivial ideal reads as "past the end", so ghost it.
			w.SetCursorRune(0)
			w.HasGhostCursor = idealReading > 0
			if w.HasGhostCursor {
				w.GhostCursorVisualColumn = idealReading
			} else {
				w.GhostCursorVisualColumn = 0
			}
			return
		}
		// The furthest-back reachable reading column is end-of-line.
		eolReading := vw - e.caretVisualColumn(w, line, lineLen, tabSize)
		if idealReading > eolReading {
			// Line does not reach that far back - ghost past the end.
			w.SetCursorRune(lineLen)
			w.HasGhostCursor = true
			w.GhostCursorVisualColumn = idealReading
			return
		}
		// Map the reading column to a left-based visual column and land on the
		// covering rune. A mismatch (short line / inside a wide cell) ghosts.
		result := e.visualColumnToRuneWithActual(w, line, vw-idealReading, tabSize)
		w.SetCursorRune(result.Rune)
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

	if maxVisualColumn < w.IdealVisualColumn {
		// Line is shorter than ideal visual column - show ghost cursor at end
		w.SetCursorRune(lineLen)
		w.HasGhostCursor = true
		w.GhostCursorVisualColumn = w.IdealVisualColumn
	} else {
		// Line is long enough - position at the rune that corresponds to ideal visual column
		result := e.visualColumnToRuneWithActual(w, line, w.IdealVisualColumn, tabSize)
		w.SetCursorRune(result.Rune)

		// Check if we landed inside a wide character (like a tab)
		// If the actual column differs from ideal, we're inside a tab stop
		if result.ActualColumn != w.IdealVisualColumn {
			w.HasGhostCursor = true
			w.GhostCursorVisualColumn = w.IdealVisualColumn
		} else {
			w.HasGhostCursor = false
			w.GhostCursorVisualColumn = 0
		}
	}
}

// setUserMark sets a user-defined mark at the given position. It rejects empty
// names and the reserved "_" internal-mark namespace.
func (e *Editor) setUserMark(w *window.Window, name string, line, runePos int) bool {
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
func (e *Editor) gotoUserMark(w *window.Window, name string) bool {
	if w == nil || w.Buffer == nil {
		return false
	}
	line, runePos, exists := w.Buffer.GetMark(name)
	if !exists {
		e.ShowWarning("Mark '" + name + "' not set")
		return false
	}
	w.SetCursorPos(window.Position{Line: line, Rune: runePos})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	w.TrackMove()
	return true
}

// setBlockMark sets an internal block-selection mark at the cursor position.
func (e *Editor) setBlockMark(markName, label string) bool {
	w := e.WindowManager.GetFocusedWindow()
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
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Buffer == nil {
		return false
	}
	line, rune_, exists := w.Buffer.GetMark(markName)
	if !exists {
		e.ShowWarning(label + " not set")
		return false
	}
	w.SetCursorPos(window.Position{Line: line, Rune: rune_})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	return true
}

// syncCursorAfterUndoRedo moves the editor cursor to the post-undo position
// garland slid the window's caret to, clamps it, and refreshes derived state.
func (e *Editor) syncCursorAfterUndoRedo(w *window.Window) {
	if w == nil || w.Buffer == nil || w.Caret == nil {
		return
	}
	e.clampCursorToBuffer(w)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	e.RequestRender()
}

// afterHorizontalMovement clears ghost cursor and updates ideal column.
func (e *Editor) afterHorizontalMovement(w *window.Window) {
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
		w.IdealVisualColumn = e.idealColumn(w, line, w.CursorPos().Rune, tabSize)
	} else {
		w.IdealVisualColumn = w.CursorPos().Rune
	}
}

// cursorToLineStart moves cursor to start of line.
func (e *Editor) cursorToLineStart() {
	w := e.WindowManager.GetFocusedWindow()
	if w == nil {
		return
	}
	w.SetCursorRune(0)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// cursorToLineEnd moves cursor to end of line.
func (e *Editor) cursorToLineEnd() {
	w := e.WindowManager.GetFocusedWindow()
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
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Buffer == nil {
		return
	}
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// cursorToBufferEnd moves cursor to end of buffer (last line, last rune).
func (e *Editor) cursorToBufferEnd() {
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Buffer == nil {
		return
	}
	lastLine := w.Buffer.GetLineCount() - 1
	if lastLine < 0 {
		lastLine = 0
	}
	w.SetCursorPos(window.Position{Line: lastLine, Rune: e.getEffectiveLineLen(w.Buffer, lastLine)})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// gotoLine moves cursor to a specific line number (1-based).
func (e *Editor) gotoLine(lineNum int) {
	w := e.WindowManager.GetFocusedWindow()
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

	w.SetCursorPos(window.Position{Line: targetLine, Rune: 0})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// scrollLineTop parks line lineNum (1-based) at the top of the target window's
// viewport without moving the caret — the scroll analogue of gotoLine.
func (e *Editor) scrollLineTop(lineNum int) {
	e.scrollViewTo(e.resolveTargetMain(), lineNum-1)
}

// cursorToTop moves cursor to start of buffer.
func (e *Editor) cursorToTop() {
	w := e.WindowManager.GetFocusedWindow()
	if w == nil {
		return
	}
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// cursorToBottom moves cursor to end of buffer.
func (e *Editor) cursorToBottom() {
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Buffer == nil {
		return
	}
	w.SetCursorPos(window.Position{Line: w.Buffer.GetLineCount() - 1, Rune: e.getEffectiveLineLen(w.Buffer, w.CursorPos().Line)})
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
}

// pageUp moves up by a page.
func (e *Editor) pageUp() {
	w := e.WindowManager.GetFocusedWindow()
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
	w := e.WindowManager.GetFocusedWindow()
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

// pageSize returns the window's view height and the configured page distance
// (evaluated against that height). The height falls back to a default when it
// is not yet known.
func (e *Editor) pageSize(w *window.Window) (viewHeight, page int) {
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

// trackEdit records a caret-area edit on the focused window's cursor ring, run
// after an editing command has completed. A no-op when there is no ring (e.g. a
// prompt window). See Window.TrackEdit. It also shifts the kill-chain flag:
// lastEditKill becomes true only when this edit was a kill capture, so a
// non-kill edit (typing, paste) between deletes breaks the accumulation.
func (e *Editor) trackEdit() {
	if w := e.WindowManager.GetFocusedWindow(); w != nil {
		w.TrackEdit()
		// An edit re-engages caret following (edits also call ensureCursorVisible,
		// but not every path does; make the re-engage unconditional here).
		w.ViewState.ScrollDetached = false
	}
	e.lastEditKill = e.pendingKill
	e.pendingKill = false
	e.checkEditLock()
}

// trackMove records a deliberate caret movement on the focused window's cursor
// ring, run after a movement command has completed. See Window.TrackMove.
func (e *Editor) trackMove() {
	if w := e.WindowManager.GetFocusedWindow(); w != nil {
		w.TrackMove()
	}
}

// cursorRingGo walks the caret one step through the cursor ring — forward
// (newer) when next is true, backward (older) otherwise — and brings it into
// view. Returns false, leaving the caret put, when there is nowhere further to
// go in that direction.
func (e *Editor) cursorRingGo(next bool) bool {
	w := e.WindowManager.GetFocusedWindow()
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

// deleteCharBefore deletes the character before the cursor.
func (e *Editor) deleteCharBefore() {
	if e.contentLocked() {
		// The buffer's owning window is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Buffer == nil {
		return
	}

	if w.CursorPos().Rune > 0 {
		// Delete the rune before the caret through the window's caret cursor,
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
		// The buffer's owning window is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.WindowManager.GetFocusedWindow()
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
		// The buffer's owning window is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.WindowManager.GetFocusedWindow()
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
func (e *Editor) clampCursorToBuffer(w *window.Window) {
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
// lineMarkSet is the set of positions on the window's caret line that get a
// showMarks "*" cell, mirroring what prepareLineForDisplay draws: one before the
// cell of each marked rune, plus — only when invisibles are shown, so the
// terminator slot exists to host it — a mark sitting at end of line. It returns
// nil when showMarks is off or the line has no drawable marks. Every visual-
// column walk (plain and bidi, forward and inverse) consumes this one set, so a
// mark inserts its cell in the same place on all of them; there is no flat
// offset (a mark before a tab steals a column the tab would otherwise fill, so
// the shift is not additive). Marks are keyed on the caret's line, the only line
// these coordinate helpers are asked about.
func (e *Editor) lineMarkSet(w *window.Window, runes []rune) map[int]bool {
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
func (e *Editor) markedLine(w *window.Window, line string) (runes []rune, marked map[int]bool, ok bool) {
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
		col += e.getRuneVisualWidth(runes[i], col, tabSize)
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
func (e *Editor) runeToVisualColumn(w *window.Window, line string, runePos int, tabSize int) int {
	if runes, marked, ok := e.markedLine(w, line); ok {
		return e.runeToVisualColumnMarked(runes, marked, runePos, tabSize)
	}
	return e.runeToVisualColumnBase(w, line, runePos, tabSize)
}

func (e *Editor) runeToVisualColumnBase(w *window.Window, line string, runePos int, tabSize int) int {
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
		r := runes[i]
		runeWidth := e.getRuneVisualWidth(r, column, tabSize)
		column += runeWidth
	}

	return column
}

// baseRTL reports whether the configured base direction is right-to-left.
func (e *Editor) baseRTL() bool { return e.Config.Direction == "rtl" }

// layoutFor computes a line's visual layout for a window: with the window's
// showBidi enabled it includes direction-marker slots (bidi.ComputeMarked),
// whose cell widths every visual-column computation must account for.
func (e *Editor) layoutFor(w *window.Window, runes []rune) *bidi.Layout {
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
	return e.getRuneVisualWidth(r, col, tabSize)
}

// winRTL is the EFFECTIVE direction for a window: its ViewState.Direction
// override when set (prompt windows are pinned "ltr"), else the base option.
func (e *Editor) winRTL(w *window.Window) bool {
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
func (e *Editor) caretVisualColumn(w *window.Window, line string, runePos, tabSize int) int {
	// Non-bidi: the caret sits on the rune's own cell, so its column is exactly
	// the marks-inclusive rune column from the inline walk (which resolves tab
	// widths at the shifted column). Bidi is exact through caretVisualColumnBase,
	// whose cols/total include the "*" cells. Mirrors the renderer's wrapper.
	if runes, marked, ok := e.markedLine(w, line); ok {
		return e.runeToVisualColumnMarked(runes, marked, runePos, tabSize)
	}
	return e.caretVisualColumnBase(w, line, runePos, tabSize)
}

func (e *Editor) caretVisualColumnBase(w *window.Window, line string, runePos, tabSize int) int {
	runes := []rune(line)
	layout := e.layoutFor(w, runes)
	if layout == nil {
		return e.runeToVisualColumnBase(w, line, runePos, tabSize)
	}
	cols, total := e.bidiColumns(runes, layout, e.lineMarkSet(w, runes), tabSize)
	rtlBase := e.winRTL(w)

	// A zero-width combining mark shares its base character's cell, so the
	// caret adjacent to one rests on the side the BASE character dictates:
	// step back over zero-width runes to the cluster base before applying
	// the placement rules. (Marked direction controls are one column wide
	// and stop the walk.)
	clusterBase := func(i int) int {
		for i > 0 && e.slotWidth(layout, runes, i, cols[i], tabSize) == 0 {
			i--
		}
		return i
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
	if runePos < 0 {
		runePos = 0
	}
	runePos = clusterBase(runePos)
	// The caret covers the cell of the rune it precedes — in either base
	// direction, for both LTR and RTL runes. A block cursor sits ON that
	// character, and an RTL rune's cell is painted at cols[runePos], so a
	// caret inside an RTL fragment stays on the rune rather than parking one
	// cell to its right.
	return cols[runePos]
}

// lineVisualWidth is the total visual width of a line (tab widths resolved
// in visual order, matching the renderer).
func (e *Editor) lineVisualWidth(w *window.Window, line string, tabSize int) int {
	runes := []rune(line)
	marked := e.lineMarkSet(w, runes)
	layout := e.layoutFor(w, runes)
	if layout == nil {
		vw := 0
		for i, r := range runes {
			if marked[i] {
				vw++
			}
			vw += e.getRuneVisualWidth(r, vw, tabSize)
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
func (e *Editor) idealColumn(w *window.Window, line string, runePos, tabSize int) int {
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
func (e *Editor) visualColumnToRune(w *window.Window, line string, targetColumn int, tabSize int) int {
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
		r := runes[i]
		runeWidth := e.getRuneVisualWidth(r, column, tabSize)

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
func (e *Editor) visualColumnToRuneWithActual(w *window.Window, line string, targetColumn int, tabSize int) visualColumnToRuneResult {
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
		rw := e.getRuneVisualWidth(runes[i], col, tabSize)
		if targetColumn < col+rw {
			return visualColumnToRuneResult{Rune: i, ActualColumn: col}
		}
		col += rw
	}
	return visualColumnToRuneResult{Rune: len(runes), ActualColumn: col}
}

func (e *Editor) visualColumnToRuneWithActualBase(w *window.Window, line string, targetColumn int, tabSize int) visualColumnToRuneResult {
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
		r := runes[i]
		runeWidth := e.getRuneVisualWidth(r, column, tabSize)

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
	} else if r < 0x20 || r == 0x7F {
		// Control characters displayed as ^X (2 characters wide)
		return 2
	}
	return textwidth.Rune(r)
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
		// The buffer's owning window is read-only, or a link button is
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

// insertText inserts text at the cursor position.
func (e *Editor) insertText(text string) {
	if e.contentLocked() {
		// The buffer's owning window is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Buffer == nil {
		return
	}

	// Ensure cursor is within valid bounds before insertion
	e.clampCursorToBuffer(w)

	if w.ViewState.OverwriteMode {
		// Overwrite mode: typing replaces the character under the caret.
		e.overwriteText(w, text)
	} else {
		// Insert through the window's own caret cursor, then read the caret back:
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

// overwriteText types text in overwrite mode: each rune replaces the character
// under the caret via garland's overwrite mutation, which coalesces a run of
// overwrites into one undo step (like typing and deleting do). At (or crossing)
// end of line — and for a newline, which splits the line — it switches to a
// plain insert so the text appends; garland lets that appending insert continue
// the overwrite run, so overtype-then-append stays a single undo step. The
// overwritten character is discarded (not sent to the kill ring), matching how
// typing over a selection works.
func (e *Editor) overwriteText(w *window.Window, text string) {
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
func (e *Editor) runeByteLenAt(w *window.Window, line, rune_ int) int {
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
	w := e.WindowManager.GetFocusedWindow()
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

	// Insert through the window's caret cursor and read it back.
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
	w := e.WindowManager.GetFocusedWindow()
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
	w := e.WindowManager.GetFocusedWindow()
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
	w := e.WindowManager.GetFocusedWindow()
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
		// The buffer's owning window is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.WindowManager.GetFocusedWindow()
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
		// The buffer's owning window is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.WindowManager.GetFocusedWindow()
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
		// The buffer's owning window is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.WindowManager.GetFocusedWindow()
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
		// The buffer's owning window is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return
	}
	w := e.WindowManager.GetFocusedWindow()
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
		// The buffer's owning window is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.WindowManager.GetFocusedWindow()
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

	// Garland slides the window caret with the deletion (back by n if it was
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
		// The buffer's owning window is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.WindowManager.GetFocusedWindow()
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

	// Garland slides the window caret with the deletion (a caret inside the
	// trimmed run collapses to its start) — no manual adjust.
	w.Buffer.DeleteText(w.CursorPos().Line, end, len(runes)-end)
	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	return true
}

// copyBlock copies the marked block to the cursor position.
func (e *Editor) copyBlock() bool {
	w := e.WindowManager.GetFocusedWindow()
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
		// The buffer's owning window is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.WindowManager.GetFocusedWindow()
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
	// below. Garland slides the window's own caret with the edit — a caret
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
		// The buffer's owning window is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.WindowManager.GetFocusedWindow()
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
	// Garland slides the window's caret to the correct insertion point (moved
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
		// The buffer's owning window is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.WindowManager.GetFocusedWindow()
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
	// garland maintains rather than captured line numbers. The window's caret
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
		// The buffer's owning window is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Buffer == nil {
		e.ShowWarning("No active buffer")
		return false
	}

	if !w.Buffer.HasBlockMarks() {
		e.ShowWarning("No block marked")
		return false
	}

	// Decoration-anchored cursor walk (see indentBlock); the window's caret
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

// openFile opens a file in a new buffer window. On the real OS the file is
// opened through Garland's lazy warm-storage path (huge files are paged, not
// slurped); virtualized file systems read through the host callbacks.
func (e *Editor) openFile(filename string) bool {
	// A registered wiki scheme ("help:/start") opens a PAGE, not a literal
	// file: route it through the same resolver a followed link uses, so the
	// real page file (~/.mew/help/start.txt) loads and the window is rooted
	// in the wiki. Without this the name fell through to a plain OS open of
	// "help:/start", which found nothing and came up blank.
	if _, handled := e.openWikiScheme(strings.TrimSpace(filename), true); handled {
		return true
	}

	buf, err := e.loadBuffer(filename)
	if err != nil {
		return false
	}

	// Create new main buffer window
	e.WindowManager.CreateWindow(window.WindowOptions{
		Type:            window.DocWindow,
		Buffer:          buf,
		Dock:            window.DockNone,
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
		SetFocus:        true,
	})

	e.RequestRender()
	return true
}

// createNewBuffer creates a new empty buffer window.
func (e *Editor) createNewBuffer() {
	buf := e.lib.New()

	e.WindowManager.CreateWindow(window.WindowOptions{
		Type:            window.DocWindow,
		Buffer:          buf,
		Dock:            window.DockNone,
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
		SetFocus:        true,
	})

	e.RequestRender()
}

// duplicateCurrentBuffer opens a new buffer window containing a copy of the
// current buffer's content. The duplicate is unnamed (no filename) so saving it
// can't overwrite the original.
func (e *Editor) duplicateCurrentBuffer() bool {
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Buffer == nil {
		return false
	}
	buf := e.lib.NewFromString(w.Buffer.GetContent())

	e.WindowManager.CreateWindow(window.WindowOptions{
		Type:            window.DocWindow,
		Buffer:          buf,
		Dock:            window.DockNone,
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
		SetFocus:        true,
	})

	e.RequestRender()
	return true
}

// cloneCurrentWindow opens a second window onto the focused window's buffer
// (the same *buffer.Buffer, not a copy), starting at the same caret and
// viewport. Both windows then edit and scroll independently — each owns its
// caret cursor and viewport anchor, and garland keeps both in sync with edits
// made through either window.
func (e *Editor) cloneCurrentWindow() bool {
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Buffer == nil || w.Type == window.PromptWindow {
		e.ShowWarning("No buffer to clone a window for")
		return false
	}

	srcPos := w.CursorPos()

	id := e.WindowManager.CreateWindow(window.WindowOptions{
		Type:            window.DocWindow,
		Buffer:          w.Buffer, // SAME buffer, not a copy
		Dock:            window.DockNone,
		Priority:        0,
		MinHeight:       1,
		ShowLineNumbers: w.ViewState.ShowLineNumbers,
		TabSize:         w.ViewState.TabSize,
		ShowInvisibles:  w.ViewState.ShowInvisibles,
		ShowBidi:        w.ViewState.ShowBidi,
		ShowMarks:       w.ViewState.ShowMarks,
		OverwriteMode:   w.ViewState.OverwriteMode,
		ReadOnly:        w.ViewState.ReadOnly,
		ShowRuler:       w.ViewState.ShowRuler,
		SetFocus:        true,
	})

	// Start the clone at the source's caret and viewport.
	if cw := e.WindowManager.GetWindow(id); cw != nil {
		cw.SetCursorPos(srcPos)
		cw.SetViewTop(w.ViewState.ViewOffsetY)
	}

	e.RequestRender()
	return true
}

// writeBlock writes the marked block's text to a prompted-for file.
func (e *Editor) writeBlock() bool {
	w := e.WindowManager.GetFocusedWindow()
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
	w := e.WindowManager.GetFocusedWindow()
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
		// The buffer's owning window is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.WindowManager.GetFocusedWindow()
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
// here rather than forbidden. Prompts are modal (focus is on the prompt window),
// so the block and caret cannot shift before the callback fires; blockFromFile
// re-reads the range defensively anyway.
func (e *Editor) promptBlockFromFile() bool {
	w := e.WindowManager.GetFocusedWindow()
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

// caretWithinBlock reports whether the focused window's caret lies within the
// marked block or on either of its edges — the inclusive span the block
// commands that TARGET the block (block_from_file, os_paste's replace mode)
// demand. False when no block is marked.
func (e *Editor) caretWithinBlock(w *window.Window) bool {
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
		// The buffer's owning window is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.WindowManager.GetFocusedWindow()
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
	// stream regardless of insert gravity. insertText advances the window caret
	// to the end of what it inserts, which is the block's new end.
	w.Buffer.BeginUserCommand(cmdName)
	w.Buffer.DeleteTextRange(startLine, startRune, endLine, endRune)
	w.Buffer.ClearBlockMarks()
	w.SetCursorPos(window.Position{Line: startLine, Rune: startRune})
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

// closeCurrentBuffer closes the current buffer window.
func (e *Editor) closeCurrentBuffer() bool {
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Type == window.PromptWindow {
		return false
	}

	// Check for changes that would be lost. The window's active buffer is
	// always at stake. Its stacked buffers are only at stake when the WINDOW
	// itself will close — with a non-empty graveyard, buffer_close instead
	// resurrects the most recent burial into this window, so the history and
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
		windowName := atRisk[0].GetFilename()
		if windowName == "" {
			windowName = "Untitled"
		}
		if len(atRisk) > 1 {
			windowName = fmt.Sprintf("%s (+%d more in history)", windowName, len(atRisk)-1)
		}

		// Store window ID to close later
		windowID := w.ID

		// Prompt for confirmation using PromptManager
		e.PromptMgr.PromptForConfirmation(fmt.Sprintf("04: LOSE CHANGES TO %s?", windowName), true, func(accepted bool, confirmed bool) {
			if accepted && confirmed {
				// User confirmed - close the buffer
				e.finishCloseBuffer(windowID)
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
func (e *Editor) finishCloseBuffer(windowID string) bool {
	// Resurrection first: with buried bindings waiting, closing the buffer
	// does NOT close the window — the most recently buried binding surfaces
	// as the window's current one (full caret/scroll state), and the closed
	// buffer is retired only if nothing else still holds it open.
	if w := e.WindowManager.GetWindow(windowID); w != nil {
		closed := w.Buffer
		if w.ResurrectLastBuried() {
			e.unburyEverywhere(w.Buffer)
			if closed != nil {
				stillOpen := false
				for _, b := range e.openDocWindows() {
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
	mainBuffers := e.contentWindows()
	if len(mainBuffers) <= 1 {
		// Last buffer - exit instead
		e.Running = false
		return true
	}

	closing := e.WindowManager.GetWindow(windowID)

	// Remove the window
	e.WindowManager.RemoveWindow(windowID)

	// Drop safety state (mew lock, notices) when no other window still holds
	// this buffer open — actively or stacked in a nav history (window_clone
	// can share one buffer; link-follow histories reference buffers too).
	if closing != nil && closing.Buffer != nil {
		shared := false
		for _, b := range e.openDocWindows() {
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

// cycleBuffer switches to the next or previous buffer.
func (e *Editor) cycleBuffer(direction int) bool {
	mainBuffers := e.contentWindows()
	if len(mainBuffers) <= 1 {
		return false
	}

	// Find current buffer index
	currentID := ""
	if w := e.WindowManager.GetFocusedWindow(); w != nil {
		currentID = w.ID
	}

	currentIndex := -1
	for i, w := range mainBuffers {
		if w.ID == currentID {
			currentIndex = i
			break
		}
	}

	if currentIndex == -1 {
		currentIndex = 0
	}

	// Calculate new index with wrap-around
	newIndex := (currentIndex + direction + len(mainBuffers)) % len(mainBuffers)

	// Focus the new buffer
	e.WindowManager.SetFocus(mainBuffers[newIndex].ID)
	e.RequestRender()
	return true
}

// contentWindows returns every content window — documents and tool surfaces
// (help, listings) — but not prompts or chrome (the modebar). These are the
// windows the data-safety and navigation enumerations act on (buffer close,
// save-all, DEADCAT, nav-history liveness, buffer cycling).
func (e *Editor) contentWindows() []*window.Window {
	var result []*window.Window
	for _, w := range e.WindowManager.AllWindows() {
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
func (e *Editor) ensureCursorVisible(w *window.Window) {
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
func (e *Editor) ensureCursorVisibleVertical(w *window.Window) {
	w.ViewState.ScrollDetached = false
	e.clampViewToCaret(w)
}

// clampViewToCaret scrolls the viewport vertically the minimum needed to bring
// the caret's line on-screen, WITHOUT touching ScrollDetached. Both the command
// path (ensureCursorVisibleVertical, which first re-engages following) and the
// per-frame render follow (renderFollowCaret, which first honors a detached
// scroll) share it.
func (e *Editor) clampViewToCaret(w *window.Window) {
	if w.ContentHeight <= 0 {
		w.ContentHeight = 20 // Default
	}
	// Absorb any slide the viewport anchor took from edits (e.g. lines inserted
	// above the top by another window on the same buffer), then scroll only as
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
// path it RESPECTS a free scroll: while the focused window is ScrollDetached
// (mouse wheel / scroll_* command), the viewport is left exactly where the user
// parked it — the caret may sit off-screen, marked by the cursorOffScreen
// indicator — until a cursor-movement or edit command re-engages following.
func (e *Editor) renderFollowCaret(w *window.Window) {
	if w.ViewState.ScrollDetached {
		return
	}
	e.clampViewToCaret(w)
}

// scrollViewByLines parks the focused-content viewport delta lines away from
// its current top (negative = toward the start), detaching it from the caret
// like the mouse wheel: the caret stays put and the per-frame follow will not
// snap the view back until a cursor-movement or edit command re-engages it.
func (e *Editor) scrollViewByLines(w *window.Window, delta int) {
	if w == nil {
		return
	}
	w.RefreshViewTop() // base the relative move on the actually-painted top
	e.scrollViewTo(w, w.ViewState.ViewOffsetY+delta)
}

// scrollViewTo parks the viewport at an absolute top line (clamped to the
// buffer), detaching it from caret-follow. Shared by the mouse wheel and every
// scroll_* command.
func (e *Editor) scrollViewTo(w *window.Window, top int) {
	if w == nil || w.Buffer == nil {
		return
	}
	if max := w.Buffer.GetLineCount() - 1; top > max {
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
func (e *Editor) ensureCursorVisibleHorizontal(w *window.Window) {
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
		rawLen := len([]rune(line))
		line, curRune := e.displayCaretLine(w, line, w.CursorPos().Rune)
		targetCol = e.caretVisualColumn(w, line, curRune, tabSize)
		if e.winRTL(w) {
			vw = e.lineVisualWidth(w, line, tabSize)
			if w.ViewState.ShowInvisibles {
				// Terminator marker cells, counted against the RAW line (the
				// display substitution above changes the content length).
				full := w.Buffer.GetLine(w.CursorPos().Line)
				vw += len([]rune(full)) - rawLen
			}
		}
		if targetCol < 0 && !e.winRTL(w) {
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
	setOff := func(v int) {
		if v < 0 {
			v = 0
		}
		if dw {
			v *= 2
		}
		w.ViewState.ViewOffsetX = v
	}

	// direction=rtl: the view is right-anchored, so visibility is decided in
	// READING columns — the caret's distance back from the line's reading
	// start (its rightmost visual cell). ViewOffsetX counts reading columns
	// scrolled past, matching the renderer's right-anchored window.
	if e.winRTL(w) && vw >= 0 {
		reading := vw - targetCol
		if ghostReadingRTL {
			reading = targetCol // already a reading column
		}
		if reading < 0 {
			reading = 0
		}
		if reading < dispOff {
			setOff(reading)
		} else if reading > dispOff+effWidth-1 {
			setOff(reading - effWidth + 1)
		}
		return
	}

	if targetCol < dispOff {
		setOff(targetCol)
	} else if targetCol >= dispOff+effWidth {
		setOff(targetCol - effWidth + 1)
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

	// Resolve each main buffer's per-window options against its current grammar
	// (base [options] overlaid by [options.<grammar>]) before any layout or
	// paint reads ViewState, so direction/gutter/etc. are current this frame.
	for _, w := range e.WindowManager.AllWindows() {
		e.reconcileGrammarOptions(w)
	}
	// Focused-scoped options (modebar, macOptionKeys, key mappings) follow the
	// focused window's grammar/class/type.
	e.reconcileFocusedOptions()

	// Push the focused window's read-only state to the host on transitions
	// (a host greys out its Edit-menu Cut for a read-only buffer).
	e.notifyEditState()
	// Push whether the built-in help window is open (a host syncs a "Quick
	// Help" menu checkmark to it).
	e.notifyHelpState()

	// Follow the cursor VERTICALLY only. Horizontal following is a "lock-in"
	// action performed by cursor/edit commands, not by rendering, so a manual
	// horizontal scroll (scroll_left/right) and the ghost column during vertical
	// navigation are not snapped back on every render.
	focusedWindow := e.WindowManager.GetFocusedWindow()
	if focusedWindow != nil {
		e.renderFollowCaret(focusedWindow)
		// A wiki-format page starts in browse mode the moment it is first
		// painted — including the very first frame after launch.
		e.autoArmBrowse(focusedWindow)
	}

	// Flip the modebar logo (M_ vs _M) to the text direction at the focused
	// caret, so the user can see which way the next keypress will move.
	if focusedWindow != nil && focusedWindow.Buffer != nil {
		lineText := strings.TrimRight(focusedWindow.Buffer.GetLine(focusedWindow.CursorPos().Line), "\n\r")
		e.Modebar.SetLogoRTL(bidi.RTLAt([]rune(lineText), focusedWindow.CursorPos().Rune, e.winRTL(focusedWindow)))
	}

	// Fill the modebar's context slot — computed for the same window the
	// modebar reads context from. Priority: the zero-width character backspace
	// would delete at the caret (a combining diacritic or invisible control —
	// the one thing on screen the user cannot see), then the outline breadcrumb
	// (the enclosing function/section chain), then the spawn placeholder.
	if cw := focusedWindow; cw != nil {
		if cw.Type == window.PromptWindow {
			cw = e.WindowManager.GetLastMainWindow()
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

	// Render
	e.Renderer.Render(layout)

	// The frame's window geometry is now set: publish the focused window's
	// editable rectangle to the host so a graphical pointer shows the I-beam
	// over text and the arrow over chrome, resolved locally from the pointer
	// cell (no per-motion round trip). Pushed only when the rectangle changes.
	e.notifyPointerRegion()

	e.lastRenderTime = time.Now()
	e.renderRequested.Store(false)

	// debug_screen: snapshot the exact frame just painted (same layout) to its
	// armed mew:/// target. CaptureFrame takes the renderer's own mutex (free
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
	if isMewPath(filename) {
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
				e.noteBuffer(buf, "resource", "Shipped page (edits save to your ~/.mew copy)", false)
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
		if reason := e.acquireMewLock(buf, filename); reason != "" {
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
		// user opening the same file in another mew window.
		if reason := e.acquireMewLock(buf, filename); reason != "" {
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
	// any window exists, so it can't modify the opened file or complicate
	// its undo history; it is for macros, mappings, and option setup.
	e.runProfileScript()

	// Create main window
	e.WindowManager.CreateWindow(window.WindowOptions{
		Type:            window.DocWindow,
		Buffer:          buf,
		Dock:            window.DockNone,
		Priority:        0,
		SetFocus:        true,
		ShowLineNumbers: e.Config.ShowLineNumbers,
		TabSize:         e.Config.TabSize,
		ShowInvisibles:  e.Config.ShowInvisibles,
		ShowBidi:        e.Config.ShowBidi,
		ShowMarks:       e.Config.ShowMarks,
		OverwriteMode:   e.Config.OverwriteMode,
		ReadOnly:        e.Config.ReadOnly,
		ShowRuler:       e.Config.ShowColumnRuler,
		LinkBrowsing:    e.Config.LinkBrowsing,
		SyntaxOverrides: e.Config.SyntaxOverrides,
	})

	return e.serve(buf)
}

// serve creates the plugin windows and runs the main event loop over the
// already-created buffer window(s), returning the primary buffer's final
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

	// Create plugin windows (modebar, column ruler, etc.)
	e.createPluginWindows()

	// Set up resize callback to trigger re-render
	// Since the main loop blocks on GetKey(), we perform the render directly
	e.Renderer.SetOnResize(func() {
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
	// expires like any other notice.
	e.ShowNotification(version.Banner())

	// If a DEADCAT from a prior crash is present, let the user know (a
	// transient, not a prompt — they recover it when they choose to).
	e.deadcatLaunchNotice()

	// Mouse reporting on for the whole session (Cleanup turns it back off):
	// button presses, drags, releases and the scroll wheel arrive through the
	// key stream as Mouse* pseudo-keys.
	e.Renderer.EnableMouseReporting()

	// Request grapheme-cluster width (DEC mode 2027) so the terminal — mew's
	// purfecterm, or any host that honors it — measures cell width the same way
	// mew's textwidth does. Terminals without the mode ignore it.
	e.Renderer.EnableGraphemeWidth()

	// A flex-width host stores one cell per character, so cursor addressing
	// counts characters: switch the renderer's CUP columns to logical.
	e.Renderer.SetLogicalColumns(e.Config.LogicalColumnTerminal)

	// Initial render
	e.performRender()

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
				if w := e.WindowManager.GetFocusedWindow(); w != nil && w.Buffer != nil {
					e.pasteBuf = w.Buffer
					e.pasteBuf.BeginUserCommand("paste")
					e.pasteActive = true
				}
			}

			// Handle paste chunk
			e.insertPasteChunk(event.Paste.Content)
			// Render and flush for visual feedback
			e.ensureCursorVisible(e.WindowManager.GetFocusedWindow())
			e.performRender()
			e.Renderer.Sync()

			if event.Paste.IsFinal {
				// Final chunk - do cleanup and close the paste transaction.
				w := e.WindowManager.GetFocusedWindow()
				if w != nil {
					e.afterHorizontalMovement(w)
				}
				if e.pasteActive {
					e.pasteBuf.EndUserCommand()
					e.pasteActive = false
					e.pasteBuf = nil
				}
				// A paste moves the caret too: re-evaluate link browse arming.
				e.updateBrowseState()
			}
		} else if event.Key != "" {
			// A terminal cursor-position report (the flipBidiForHost probe's
			// reply) is consumed here, never typed.
			if e.handleBidiProbeReply(event.Key) {
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
			result := e.KeyProcessor.ProcessKey(event.Key)
			if result.Command != "" {
				e.executeCommand(result.Command)
			}

			// Link browse mode: arm when the caret has entered a link span it
			// was not previously inside (identity tracked by anchor, not line).
			e.updateBrowseState()

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

// createPluginWindows creates windows for all enabled plugins.
func (e *Editor) createPluginWindows() {
	// Create modebar window (always enabled) at its configured location
	e.Modebar.SetLocation(e.Config.ModebarLocation)
	e.Modebar.CreateWindow()
}

// runProfileScript executes the startup pawscript: a host-supplied script
// when one was provided, else the user's profile.mew (created with a small
// default when missing), unless the host opted out. It runs before any
// window exists, so it cannot modify the opened file or its undo history;
// script errors surface through the usual PawScript stderr writer as error
// windows.
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

// transientNotificationClasses are the window classes used for the transient
// bottom-docked message windows created by ShowNotification/ShowError/
// ShowWarning. They share a single display slot and are auto-expired.
var transientNotificationClasses = map[string]bool{
	"notification": true,
	"error":        true,
	"warning":      true,
}

// showTransient creates a one-line bottom-docked message window of the given
// class; the class drives its colors. Transient windows are not cleared on
// creation; they stack and are removed purely by age via
// expireStaleNotifications.
func (e *Editor) showTransient(message, class string) {
	e.WindowManager.CreateWindow(window.WindowOptions{
		Type:            window.ToolWindow,
		WindowSet:       window.WindowSetTransient,
		Class:           class,
		Dock:            window.DockBottom,
		Priority:        0, // Very low priority - below everything else
		MinHeight:       1,
		MaxHeight:       1,
		MessageTopInner: message,
		ShowLineNumbers: false,
	})

	e.RequestRender()
}

// ShowNotification creates a transient notification window at the bottom of the
// screen (informational messages, command confirmations).
func (e *Editor) ShowNotification(message string) {
	e.showTransient(message, "notification")
}

// showTaggedTransient is showTransient for messages that should replace their
// predecessor instead of stacking: any existing transient carrying the same
// tag is removed first. The class still drives colors and age-expiry (so pass
// "notification"); the tag only groups the message for replacement. Used by
// filename completion so re-completing does not pile up option lists.
func (e *Editor) showTaggedTransient(message, class, tag string) {
	for _, w := range e.WindowManager.GetWindowsByDock(window.DockBottom) {
		if w.Tag == tag {
			e.WindowManager.RemoveWindow(w.ID)
		}
	}
	e.WindowManager.CreateWindow(window.WindowOptions{
		Type:            window.ToolWindow,
		WindowSet:       "help",
		Class:           class,
		Tag:             tag,
		Dock:            window.DockBottom,
		Priority:        0,
		MinHeight:       1,
		MaxHeight:       1,
		MessageTopInner: message,
		ShowLineNumbers: false,
	})
	e.RequestRender()
}

// appendVerboseLog appends lines to the shared verbose-log window (class
// "verboseLog"), creating it on first use: a new empty buffer window like
// buffer_new makes, but in the background — it never takes focus or the
// painted main area; the user reaches it later via window cycling.
func (e *Editor) appendVerboseLog(lines ...string) {
	var vw *window.Window
	for _, w := range e.WindowManager.AllWindows() {
		if w.Class == "verboseLog" {
			vw = w
			break
		}
	}
	if vw == nil {
		id := e.WindowManager.CreateWindow(window.WindowOptions{
			Type:            window.DocWindow,
			Class:           "verboseLog",
			Dock:            window.DockNone,
			Priority:        0,
			Buffer:          e.lib.New(),
			ShowLineNumbers: true,
			TabSize:         e.Config.TabSize,
			Visible:         true,
		})
		vw = e.WindowManager.GetWindow(id)
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

// announceFocusedWindow shows a "Switched to ..." notification naming the
// newly focused window: its top-center message, else its buffer's filename,
// else its ID.
func (e *Editor) announceFocusedWindow() {
	w := e.WindowManager.GetFocusedWindow()
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
	e.ShowNotification("Switched to " + name)
}

// ShowError shows a transient error window at the bottom of the screen.
func (e *Editor) ShowError(message string) {
	e.showTransient(message, "error")
}

// ShowWarning shows a transient warning window at the bottom of the screen.
func (e *Editor) ShowWarning(message string) {
	e.showTransient(message, "warning")
}

// expireStaleNotifications removes transient notification/error/warning windows
// that are older than 5 seconds. Called before each command executes.
func (e *Editor) expireStaleNotifications() {
	now := time.Now()
	for _, w := range e.WindowManager.GetWindowsByDock(window.DockBottom) {
		if transientNotificationClasses[w.Class] && now.Sub(w.SpawnedAt) > 5*time.Second {
			e.WindowManager.RemoveWindow(w.ID)
		}
	}
}

// There is ONE docked help window (Tag "help", top dock): help_toggle NAVIGATES
// it between help "locations" — the built-in Quick Help (the WordStar command
// reference) and help:/ wiki pages — building the window's own nav history, so
// its back button returns to where the reader came from. help_toggle on the
// location already showing closes the window. buffer_open_file, by contrast,
// opens ordinary UNtagged help windows that can stack; only this slot is
// tagged, and only help_toggle manages it. The Quick Help checkmark tracks
// whether the slot is currently showing Quick Help.
const helpWindowTag = "help"

// quickHelpDocURL is the synthetic identity of the Quick Help buffer. It sits
// OUTSIDE the help wiki root (mew:///help) so it never resolves as, or displays
// like, a wiki page; it just gives the location a stable URL to compare against.
const quickHelpDocURL = "mew:///quickhelp"

// helpWindow returns the single docked help window (Tag "help"), or nil.
func (e *Editor) helpWindow() *window.Window {
	for _, w := range e.WindowManager.GetWindowsByDock(window.DockTop) {
		if w.Tag == helpWindowTag {
			return w
		}
	}
	return nil
}

// quickHelpWindowOpen reports whether the docked help window is open AND in
// Quick Help (dynamic, context-following) mode — what the "Quick Help"
// checkmark reflects. It reads the quickHelpMode flag rather than the buffer
// URL: Quick Help now shows real help pages, so URL alone can't tell it apart
// from a page reached by browsing. syncQuickHelpMode() first reconciles the
// flag with reality (the window may have closed, or the user may have browsed
// away) so the answer is always current.
func (e *Editor) quickHelpWindowOpen() bool {
	e.syncQuickHelpMode()
	return e.quickHelpMode && e.helpWindow() != nil
}

// syncQuickHelpMode reconciles quickHelpMode with the docked help window: quick
// mode ends the moment the window closes or its buffer drifts from the one
// Quick Help installed (the user followed a link, walked history, or opened the
// main help). Called before anything reads or acts on quick mode.
func (e *Editor) syncQuickHelpMode() {
	if !e.quickHelpMode {
		return
	}
	hw := e.helpWindow()
	if hw == nil || hw.Buffer != e.quickHelpBuf {
		e.quickHelpMode = false
		e.quickHelpBuf = nil
		e.quickHelpShownTopic = ""
	}
}

// toggleHelp is the help_toggle command. With no argument the target is Quick
// Help; with one it is the help wiki page help:/<arg> ("help:/..." is accepted
// whole, so help_toggle "help:/" opens the index). If the docked help window is
// already showing that target it closes; otherwise the window NAVIGATES to it
// (its nav history grows, so back returns), opening the docked window when none
// exists. So one key can hold Quick Help and another a help page, each landing
// in the same docked window.
func (e *Editor) toggleHelp(arg string) bool {
	arg = strings.TrimSpace(arg)
	e.syncQuickHelpMode()
	hw := e.helpWindow()

	if arg == "" {
		if e.quickHelpMode && hw != nil {
			e.WindowManager.RemoveWindow(hw.ID) // already here: toggle off
			e.quickHelpMode = false
			e.quickHelpBuf = nil
			e.quickHelpShownTopic = ""
			e.RequestRender()
			return true
		}
		// Resolve the topic for the current prefix now, so a menu-driven open
		// (no keypress pending) still lands on the root topic rather than a
		// stale one.
		e.quickHelpTopic = e.KeyProcessor.HelpTopic(e.ActiveSequence)
		buf, root, wikiName, browse := e.quickHelpDestination()
		e.showHelpLocation(hw, buf, root, wikiName, browse)
		e.quickHelpMode = true
		e.quickHelpBuf = buf
		e.quickHelpShownTopic = e.quickHelpTopic
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
		// Missing page: offer creation when the wiki allows it, else report.
		if res.createURL != "" && res.writable {
			title := strings.TrimPrefix(arg, "help:/")
			buf, err := e.createBufferURL(res.createURL, "=== "+title+" ===\n\n")
			if err != nil {
				e.ShowError("Create: " + err.Error())
			} else {
				e.showHelpLocation(hw, buf, res.root, res.wikiName, true)
			}
		} else {
			e.ShowNotification(res.message)
		}
		e.RequestRender()
		return true
	}
	if hw != nil && e.bufferCanonicalURL(hw.Buffer) == res.url {
		e.WindowManager.RemoveWindow(hw.ID) // already here: toggle off
		e.RequestRender()
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
	e.showHelpLocation(hw, buf, res.root, res.wikiName, true)
	return true
}

// showHelpLocation puts buf into the docked help window: it NAVIGATES an
// existing window (swap_buffer, so the previous location goes onto the back
// history), or creates the docked window when none exists. root/wikiName wire
// the window to the help wiki (empty for Quick Help), and browse arms link
// browsing for a wiki page.
func (e *Editor) showHelpLocation(hw *window.Window, buf *buffer.Buffer, root, wikiName string, browse bool) {
	if hw == nil {
		hw = e.createHelpWindow(buf)
	} else {
		e.swapBuffer(hw, buf)
	}
	hw.WikiRoot = root
	hw.WikiName = wikiName
	hw.BrowseActive = browse && hw.ViewState.LinkBrowsing
	hw.BrowseAutoArmed = hw.BrowseActive
	e.ensureCursorVisible(hw)
	e.RequestRender()
}

// createHelpWindow creates the single docked help window (Tag "help") holding
// buf, focused so the reader can scroll and follow links straight away.
func (e *Editor) createHelpWindow(buf *buffer.Buffer) *window.Window {
	opts := e.docWindowOptions()
	opts.Type = window.ToolWindow
	opts.WindowSet = "help"
	opts.Class = "help"
	opts.Tag = helpWindowTag
	opts.Dock = window.DockTop
	opts.Priority = 100
	opts.MinHeight = 4
	opts.MaxHeight = 20
	opts.MessageTopCenter = "Help"
	opts.Buffer = buf
	opts.SetFocus = true
	return e.WindowManager.GetWindow(e.WindowManager.CreateWindow(opts))
}

// quickHelpDestination resolves what Quick Help should show for the current
// quickHelpTopic. A topic that names a help wiki page (help:/<topic>) yields
// that page plus its wiki wiring (so its links are followable); a missing or
// empty topic falls back to the built-in WordStar reference. It never surfaces
// an error — Quick Help follows the key context silently, keystroke by
// keystroke, so a bad topic simply shows the built-in page.
func (e *Editor) quickHelpDestination() (buf *buffer.Buffer, root, wikiName string, browse bool) {
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
				return b, res.root, res.wikiName, true
			}
		}
	}
	return e.quickHelpBuffer(), "", "", false
}

// refreshQuickHelp re-renders the docked Quick Help window for the current
// quickHelpTopic, in place — no history entry, because Quick Help is a single
// dynamic slot. Called from the key loop when the context topic changes while
// Quick Help is open; a no-op when the destination is unchanged.
func (e *Editor) refreshQuickHelp(hw *window.Window) {
	buf, root, wikiName, browse := e.quickHelpDestination()
	if buf != hw.Buffer {
		e.replaceBuffer(hw, buf)
	}
	hw.WikiRoot = root
	hw.WikiName = wikiName
	hw.BrowseActive = browse && hw.ViewState.LinkBrowsing
	hw.BrowseAutoArmed = hw.BrowseActive
	e.quickHelpBuf = buf
	e.quickHelpShownTopic = e.quickHelpTopic
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
	if hw := e.helpWindow(); hw != nil {
		e.refreshQuickHelp(hw)
	}
}

// quickHelpBuffer builds the Quick Help buffer (the WordStar command
// reference), named by the synthetic quickHelpDocURL so the help window can
// recognize the Quick Help location.
func (e *Editor) quickHelpBuffer() *buffer.Buffer {
	helpText := `WordStar Style Command Reference:
-------------------------------
Navigation:
  ^P - Line up       ^N - Line down     ^B - Char left     ^F - Char right
  ^A - Start of line ^E - End of line   ^Z - Prev word     ^X - Next word
  ^U - Page up       ^V - Page down

Editing:
  ^H - Backspace     ^D - Delete char   ^O - Delete word beg  ^W - Delete word end
     - Del line beg  ^J - Del line end

Block Operations:
  ^KB - Mark begin   ^KK - Mark end     ^KC - Copy block   ^KM - Move block
  ^KY - Delete block ^KW - Write block  ^K. - Indent block ^K, - Unindent block

File Operations:
  ^KS - Save         ^KD - Save as      ^KQ - Quit         ^KX - Save and exit
  ^KR - Read file    ^C  - Abort        ^KA - Save all     ^KN - New buffer
  ^KO - Open file    ^KZ - Close buffer ^KB - List buffers

Window Navigation:
  Esc ] - Next window Esc [ - Prev window
  Esc U - Show more top windows   Esc V - Show more bottom top windows
  Esc P - Show more bottom windows  Esc N - Show more bottom windows

Search and Navigate:
  ^KF - Find         ^L - Find next     ^KL - Go to line   ^G - Go to match

Other:
  ^KH - Toggle help  ^T - Editor options ^- or ^_ - Undo   ^+ or ^^ - Redo
  Esc X - Cmd prompt Esc > - Scroll right Esc < - Scroll left

Press ^KH to close help...`

	buf := e.lib.NewFromString(helpText)
	buf.SetFilename(quickHelpDocURL)
	return buf
}

// toggleOptions shows the editor-options display, or dismisses it if a second
// invocation arrives while it is already open (mirroring toggleHelp). The
// window carries Class "options" so it can be found and removed.
func (e *Editor) toggleOptions() bool {
	// Dismiss an existing options window (top dock, Class "options").
	for _, w := range e.WindowManager.GetWindowsByDock(window.DockTop) {
		if w.Class == "options" {
			e.WindowManager.RemoveWindow(w.ID)
			e.RequestRender()
			return true
		}
	}

	// Build options display, showing the EFFECTIVE values for the last
	// active editor window (per-window settings govern the window).
	optWin := e.WindowManager.GetLastMainWindow()
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
	content.WriteString(fmt.Sprintf("  Modebar Location: %s\n", opt("modebarLocation")))
	content.WriteString(fmt.Sprintf("  Page Size Optimal: %s\n", opt("pageSizeOptimal")))
	content.WriteString(fmt.Sprintf("  Page Overlap Minimum: %s\n", opt("pageOverlapMinimum")))
	content.WriteString(fmt.Sprintf("  Page Size Step: %s\n", opt("pageSizeStep")))
	content.WriteString(fmt.Sprintf("  Max Repeat: %s\n", opt("maxRepeat")))
	content.WriteString(fmt.Sprintf("  Kill Ring Entries: %s\n", opt("killRingEntries")))
	content.WriteString(fmt.Sprintf("  Direction: %s\n", opt("direction")))
	content.WriteString(fmt.Sprintf("  Prompt Timeout (s, 0=never): %s\n", opt("promptTimeout")))
	content.WriteString(fmt.Sprintf("  Script Timeout (s, 0=never): %s\n", opt("scriptTimeout")))
	content.WriteString(fmt.Sprintf("  Debounce (ms): %s\n", opt("debounceMs")))
	content.WriteString(fmt.Sprintf("  Max Render Delay (ms): %s\n", opt("maxRenderDelayMs")))
	content.WriteString(fmt.Sprintf("\n  Mappings: %s\n", e.LoadedConfig.General.MappingsName))
	content.WriteString(fmt.Sprintf("  Layout: %s\n", e.LoadedConfig.General.Layout))
	content.WriteString("\nInvoke editor_options again to close...")

	buf := e.lib.NewFromString(content.String())
	e.WindowManager.CreateWindow(window.WindowOptions{
		Type:             window.ToolWindow,
		WindowSet:        "help",
		Class:            "options",
		Dock:             window.DockTop,
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
	e.WindowManager.CreatePromptWindow(prompt, defaultValue, callback)
	e.RequestRender()
}
