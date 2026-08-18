//go:build mew

// Package trinkets: the mew-backed Editor trinket (the -tags mew build).
//
// This replaces the vanilla placeholder editor (editor.go, //go:build !mew)
// with a full mew editor session, honoring the SAME editor trinket contract
// (docs/editor-trinket.md) so an app runs identically on either. mew ships a
// KittyTK fork built with -tags mew; upstream/vanilla ships the placeholder.
//
// Wiring: mew renders a terminal escape stream into PurfecTerm.Feed (display);
// PurfecTerm encodes keystrokes/mouse into raw terminal bytes that flow back
// into mew's input reader. No key-name translation — mew's own parser and
// renderer do all the work, and PurfecTerm is a pure display+input surface,
// exactly as when it drives a remote PTY.
package trinkets

import (
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/hostterm"
	"github.com/phroun/kittytk/text"
	"github.com/phroun/mew"
	"github.com/phroun/purfecterm"
)

// Editor is the mew-backed editor trinket. It embeds *PurfecTerm (editor mode)
// so focus, input routing, and painting are the terminal surface's, and adds
// the mew session lifecycle plus the contract property/event surface.
//
// The session is started LAZILY, on the first paint: the app applies properties
// (filename, value, options) after construction, so starting at construction
// would race them. Close ends the session (EOF on mew's input).
type Editor struct {
	*PurfecTerm

	// Contract properties (app -> editor), applied after construction. The *Set
	// flags record an explicit override of a rich property vs. inheriting mew's
	// own resolution.
	value          string
	filename       string
	placeholder    string
	caption        string
	readonly       bool
	wrap           bool
	wrapSet        bool
	tabSize        int
	tabSizeSet     bool
	syntax         string
	lineNumbers    bool
	lineNumbersSet bool
	caret          string

	// Host-provided seams (not app properties). Left at defaults for now: the
	// mew session reads/writes the KittyTK host OS disk. Wiring the brokered,
	// permission-scoped filesystem is future KittyTK work.
	fileSystem    mew.FileSystem
	mewFileSystem mew.FileSystem

	// pointerRegion mirrors mew's pointer affordance (WithPointerRegion): the
	// terminal-cell rectangle (1-based [col, row, width, height]) where the
	// focused mew window's editable text lives, plus pointerArrows, the
	// on-screen link-button spans WITHIN it that still show the arrow — both
	// republished after each render. CursorShapeAt shows the I-beam inside the
	// rectangle except on an arrow span, resolved locally from the pointer's
	// cell (no per-motion round trip through mew's input stream). Guarded
	// because mew republishes from its session goroutine while the desktop
	// queries the cursor from its own.
	pointerRegion   [4]int
	pointerArrows   []mew.PointerArrowSpan
	pointerRegionMu sync.Mutex

	// bars holds the editor scrollbars mew publishes for this host to DRAW
	// (see editor_mew_scrollbar.go). On a graphical target mew reserves each
	// bar's column but paints nothing into it, so the thumb here is free of
	// the cell grid: continuous length, pixel-smooth travel, whole-line result.
	bars editorBars

	// readOnlyFocused mirrors mew's focused-window read-only state
	// (WithEditState), so CutEnabled greys the Edit menu's Cut while a
	// read-only buffer holds focus.
	readOnlyFocused atomic.Bool

	// helpOpen mirrors whether mew's built-in help window is open
	// (WithHelpState), so a host can keep a "Quick Help" checkmark in sync
	// (QuickHelpOpen).
	helpOpen atomic.Bool

	// unsaved mirrors whether ANY buffer the session holds open is modified
	// (WithUnsavedState). It is the question a frame asks before closing this
	// editor's window: work at stake is why a close is refused and answered
	// with mew's own prompt instead (HasUnsavedWork, RequestClose).
	unsaved atomic.Bool

	// launchArgv, when set by the host, runs the session as a full mew
	// command-line launch (multi-file, per-file options, +N) via mew.EditArgv,
	// taking precedence over filename/value/caret. A host seam, not an app
	// property — the host injects its process argv for the root editor.
	launchArgv []string

	// showDesktop / hideDesktop, when set by the host, back mew's show_desktop /
	// hide_desktop commands. Host seams (the root editor's host wires them to
	// reveal/hide its desktop).
	showDesktop func()
	hideDesktop func()

	// Event hooks, wired by the protocol bind.
	onCommit func(value, filename string)
	onCancel func()

	// port injects editor commands into the running mew session from UI
	// threads (Edit-menu and context-menu items): each Execute marshals
	// onto mew's main loop. Created with the session in run().
	port *mew.HostPort

	// termOut carries bytes bound for a child process that were produced
	// OUTSIDE a hook call, drained by one worker. Bounded and lossy on
	// purpose: see termInput.
	termOut     chan string
	termOutOnce sync.Once

	// termGridWant is the LATEST window each hosted surface has settled on -
	// cols, rows, and the pane's pixels, because the child is told all four and
	// any of them can move without the others (see termGrid) -
	// waiting to be told to mew. A latch rather than a queue entry because a
	// grid is a fact, not an event: superseded values are worthless and the
	// current one must not be dropped under pressure the way input bytes may
	// be — a dropped grid leaves the guest wrapping forever.
	termGridWant map[string][4]int
	termGridWake chan struct{}

	// hostedKids are the child terminals parented to this editor (see
	// AddChild): in the tree for parent-chain lookups, invisible to layout
	// and hit testing.
	hostedKids []core.Trinket

	// ptrMu guards the last PRECISE pointer position seen by this editor, in
	// its own units. mew's mouse wire speaks in CELLS — the pseudo-keys carry
	// col,row and nothing finer — so when a hosted child's event is rebuilt
	// from the wire it lands on cell centers, and a scrollbar scrub moves in
	// cell-sized lurches. The precise position exists exactly once, HERE, as
	// the event passes through on its way to being encoded; remember it, and
	// when mew's round trip comes back for the SAME cell, use it instead of
	// the center. Guarded because the desktop writes it and mew's loop reads
	// it.
	ptrMu    sync.Mutex
	ptrX     core.Unit
	ptrY     core.Unit
	ptrValid bool

	// lastSurfaceOffsetX/Y is the device-pixel residual the most recent
	// paintTerminalSurfaces applied (see there), and lastClipFrame the clip
	// it chose, in EDITOR-frame units. Recorded because the correction is
	// invisible by construction — it exists to make a seam disappear — so
	// this is what a test can measure instead of a screenshot.
	lastSurfaceOffsetX, lastSurfaceOffsetY int
	lastClipFrame                          core.UnitRect

	// Session plumbing.
	inPipeR  *io.PipeReader
	inPipeW  *io.PipeWriter
	resizeCh chan struct{}

	mu          sync.Mutex
	curCols     int
	curRows     int
	inputClosed bool
	started     bool

	done chan struct{}
	err  error

	// termSurfaces holds one child PurfecTerm per running mew terminal
	// session, keyed by the session id mew assigns. Guarded because mew
	// feeds and places them from its session goroutine while the desktop
	// paints from its own.
	termMu       sync.Mutex
	termSurfaces map[string]*termSurface
}

// NewEditor creates a mew-backed editor trinket. The mew session does not start
// until the first paint (see ensureStarted), so contract properties applied
// after construction are honored.
func NewEditor() *Editor {
	e := &Editor{
		PurfecTerm: NewPurfecTerm(),
		resizeCh:   make(chan struct{}, 1),
		done:       make(chan struct{}),
		curCols:    80,
		curRows:    24,
	}
	// Re-point the embedded terminal's trinket identity at the Editor. The
	// framework focuses the Editor (the registered trinket), but NewPurfecTerm
	// set the shared base's `self` to the PurfecTerm; without this, focusing the
	// Editor registers the wrong (out-of-chain) trinket with the focus manager,
	// so the terminal's focused state and cursor don't engage until a mouse
	// click re-focuses it. Same pattern the placeholder editor uses.
	e.Init(e)
	e.SetEditorMode(true)
	e.SetKeyRegistry(capturedEditorKeys())
	return e
}

// capturedKeys is the keymap a mew editor takes the keyboard on: mew's own
// bindings are mew's business, so the toolkit keeps only the keys that reach
// the HOST's chrome and leaves everything else to the guest.
var (
	capturedKeysOnce sync.Once
	capturedKeys     *core.KeyRegistry
)

// capturedEditorKeys is the "captured" registry a mew editor declares.
//
// mew has a keymap of its own -- a whole editor's worth, with chords the
// toolkit knows nothing about -- so while it holds the focus, the toolkit's
// table must not take keys off the top of it: ^Q is mew's quit, ^W is mew's,
// and a host that resolved them first would be answering for the wrong
// program. Declaring a registry with almost nothing in it is how a trinket
// says that (see core/keyscope.go): whatever is not bound here is not bound
// anywhere above it either, and the keystroke arrives at mew.
//
// What it DOES keep is the way back out: the menu key and the help key, which
// reach the host's own chrome and would otherwise be unreachable from the
// keyboard while the editor has the focus. Their spellings are taken from the
// live default registry rather than written out again, so a host that rebinds
// the menu key in its ini keeps that binding here too. (Menu ACCELERATORS -
// the M-* chords - need no help: the bar publishes those into whatever context
// is current, so they keep working regardless of which registry built it.)
//
// Built once, on the first editor: by then a host has applied its own keymap,
// so the spellings this copies are the ones actually in force.
func capturedEditorKeys() *core.KeyRegistry {
	capturedKeysOnce.Do(func() {
		host := core.DefaultKeyRegistry()
		var shared []core.Binding
		for _, cmd := range []string{core.CmdAppMenu, core.CmdAppHelp} {
			keys := host.KeysFor(cmd) // newest (most advertised) first
			// Written back in the order the host had them, so the key this
			// advertises for the command is the one the host advertises.
			for i := len(keys) - 1; i >= 0; i-- {
				shared = append(shared, core.Binding{Key: keys[i], Commands: []string{cmd}})
			}
		}
		capturedKeys = core.NewKeyRegistry("captured", shared)
	})
	return capturedKeys
}

// --- Contract property setters (bound by editor_protocol_mew.go) ---

func (e *Editor) SetValue(s string)       { e.value = s }
func (e *Editor) SetFilename(s string)    { e.filename = s }
func (e *Editor) SetPlaceholder(s string) { e.placeholder = s }
func (e *Editor) SetCaption(s string)     { e.caption = s }
func (e *Editor) SetReadOnly(b bool)      { e.readonly = b }
func (e *Editor) SetWrap(b bool)          { e.wrap, e.wrapSet = b, true }
func (e *Editor) SetTabSize(n int)        { e.tabSize, e.tabSizeSet = n, true }
func (e *Editor) SetSyntax(s string)      { e.syntax = s }
func (e *Editor) SetLineNumbers(b bool)   { e.lineNumbers, e.lineNumbersSet = b, true }
func (e *Editor) SetCaret(s string)       { e.caret = s }

func (e *Editor) SetOnCommit(fn func(value, filename string)) { e.onCommit = fn }
func (e *Editor) SetOnCancel(fn func())                       { e.onCancel = fn }

// SetFileSystem / SetMewFileSystem let a host (not the app) inject the brokered
// filesystem for this session before it starts.
func (e *Editor) SetFileSystem(fs mew.FileSystem)    { e.fileSystem = fs }
func (e *Editor) SetMewFileSystem(fs mew.FileSystem) { e.mewFileSystem = fs }

// SetLaunchArgv is a host seam: the host injects its process argv so the root
// editor launches with mew's full command-line semantics (multi-file, per-file
// options, +N). Takes precedence over filename/value/caret.
func (e *Editor) SetLaunchArgv(argv []string) { e.launchArgv = argv }

// SetShowDesktop / SetHideDesktop are host seams backing mew's show_desktop /
// hide_desktop commands. The host wires them to reveal/hide its desktop.
func (e *Editor) SetShowDesktop(fn func()) { e.showDesktop = fn }
func (e *Editor) SetHideDesktop(fn func()) { e.hideDesktop = fn }

// The Editor is a minimal core.Container so its hosted terminal children can
// name it as their PARENT. The child list exists for the lookups that walk
// parent chains — FindGraphicalFrames, the popup controller, findDesktop —
// not for layout or event dispatch: this editor paints and feeds its children
// itself, so Layout is a no-op and ChildAt hides them from hit testing.
func (e *Editor) AddChild(child core.Trinket) {
	e.mu.Lock()
	e.hostedKids = append(e.hostedKids, child)
	e.mu.Unlock()
	child.SetParent(e)
}

func (e *Editor) RemoveChild(child core.Trinket) {
	e.mu.Lock()
	for i, c := range e.hostedKids {
		if c == child {
			e.hostedKids = append(e.hostedKids[:i], e.hostedKids[i+1:]...)
			break
		}
	}
	e.mu.Unlock()
}

func (e *Editor) Children() []core.Trinket {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]core.Trinket(nil), e.hostedKids...)
}

func (e *Editor) ChildAt(core.UnitPoint) core.Trinket { return nil }
func (e *Editor) Layout()                             {}
func (e *Editor) LayoutManager() core.LayoutManager   { return nil }
func (e *Editor) SetLayoutManager(core.LayoutManager) {}

// Paint starts the session on the first paint (once properties are applied),
// then delegates to the terminal surface.
func (e *Editor) Paint(p *core.Painter) {
	e.ensureStarted()
	if e.PurfecTerm != nil {
		e.PurfecTerm.Paint(p)
	}
	// Terminal sessions draw OVER the document surface, in the cells the
	// document would have used. Painted after it, so they layer on top.
	e.paintTerminalSurfaces(p)
	// ...and the editor's own scrollbars over everything, in the columns mew
	// reserved and left empty for them.
	e.paintEditorScrollbars(p)
}

// ensureStarted wires the pipes and launches the mew session exactly once.
func (e *Editor) ensureStarted() {
	e.mu.Lock()
	if e.started {
		e.mu.Unlock()
		return
	}
	e.started = true
	e.mu.Unlock()

	if e.Terminal() == nil {
		close(e.done) // no surface to render into
		return
	}

	e.inPipeR, e.inPipeW = io.Pipe()

	// Keystrokes/mouse the PurfecTerm encodes become mew's input.
	e.SetInputSink(func(b []byte) {
		e.mu.Lock()
		closed := e.inputClosed
		e.mu.Unlock()
		if closed {
			return
		}
		_, _ = e.inPipeW.Write(b)
	})

	// Grid-size changes wake mew's resize path (it re-reads Size()).
	e.SetResizeSink(func(cols, rows int) {
		e.mu.Lock()
		e.curCols, e.curRows = cols, rows
		e.mu.Unlock()
		select {
		case e.resizeCh <- struct{}{}:
		default:
		}
	})

	go e.run()
}

// writerFunc adapts a function to io.Writer (mew's display sink).
type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func (e *Editor) run() {
	defer close(e.done)

	term := mew.Terminal{
		Output: writerFunc(func(p []byte) (int, error) {
			e.Feed(p)
			return len(p), nil
		}),
		Input: e.inPipeR,
		Size: func() (int, int, error) {
			e.mu.Lock()
			c, r := e.curCols, e.curRows
			e.mu.Unlock()
			return c, r, nil
		},
		Resize: e.resizeCh,
		// PurfecTerm is a genuine emulator surface: it answers DECRQM/XTWINOPS
		// queries back through Input, so mew's terminal-probing features (pixel
		// mouse, ?1016) engage exactly as on a real terminal.
		Interactive: true,
	}

	fs := e.fileSystem
	if fs == nil {
		fs = mew.OSFileSystem()
	}
	e.port = mew.NewHostPort()
	options := []mew.Option{
		mew.WithTerminal(term), mew.WithFileSystem(fs),
		// The host command port: Edit-menu and context-menu items inject
		// mew commands (os_copy and friends), each marshaled onto mew's
		// main loop with keystroke safety.
		mew.WithHostPort(e.port),
		// Hand the terminal back if mew has to abort. A fatal signal skips
		// every deferred teardown on both sides - mew dumps DEADCAT and calls
		// os.Exit - and an embedded mew renders into OUR surface, so its own
		// renderer cleanup restores nothing the user can see. Without this the
		// user keeps their unsaved work and loses their shell.
		mew.WithRestoreHostTerminal(e.restoreHostTerminal),
		// Terminal sessions. This trinket's host decides what a request means;
		// see ptyProvider. A host that wants to sandbox or redirect a hosted
		// mew simply supplies a different one - mew cannot tell.
		mew.WithPTYProvider(e.ptyProvider),
		// ...and a way to ask this host to test its own terminal plumbing,
		// for when a session starts and stops with nothing to show for it.
		mew.WithPTYDiagnose(ptyDiagnose),
		// (The editor scrollbar hand-off is appended below, graphical host only.)
		// ...and how they are drawn: one real child PurfecTerm per session,
		// laid over the viewport's text area. PurfecTerm is the emulator, so
		// mew forwards raw bytes and never interprets them.
		mew.WithTerminalSurfaces(mew.TerminalHooks{
			Open:           e.terminalOpen,
			SetLogicalSize: e.terminalSetLogicalSize,
			Snapshot:       e.terminalSnapshot,
			Feed:           e.terminalFeed,
			Place:          e.terminalPlace,
			Close:          e.terminalClose,
			Mouse:          e.terminalMouse,
			Key:            e.terminalKey,
			SetCaptureSink: e.terminalSetCaptureSink,
		}),
		// The system-clipboard bridge behind mew's os_copy/os_cut/os_paste
		// — the same desktop clipboard TextInput and the classic PurfecTerm
		// use, kept separate from mew's own kill ring. mew calls these on
		// its session goroutine; postUI marshals onto the desktop loop,
		// where SetClipboard and the (possibly async) ReadClipboardAsync
		// are safe. The paste delivery then marshals back into mew.
		// What this keyboard was watched composing for an Option chord. The
		// platform sees both halves of that keystroke and mew sees neither —
		// it is handed the chord as bytes — so the answer has to travel.
		mew.WithMacOptionObserved(func(chord string) (string, bool) {
			if d := e.findDesktop(); d != nil {
				return d.OptionChar(chord)
			}
			return "", false
		}),
		mew.WithClipboard(
			func(s string) {
				e.postUI(func() {
					if d := e.findDesktop(); d != nil {
						d.SetClipboard(s)
					}
				})
			},
			func(deliver func(string)) {
				e.postUI(func() {
					if d := e.findDesktop(); d != nil {
						d.ReadClipboardAsync(deliver)
						return
					}
					deliver("")
				})
			},
		),
		// The right-click context menu: mew validates the click (editing
		// area of the focused window only) and reports the cell; the menu
		// pops there in the TextInput style.
		mew.WithContextMenu(func(col, row int) {
			e.postUI(func() { e.showMewContextMenu(col, row) })
		}),
		// Focused-window read-only state, mirrored so CutEnabled can grey
		// the Edit menu's Cut for a read-only buffer.
		mew.WithEditState(func(readOnly bool) {
			e.readOnlyFocused.Store(readOnly)
		}),
		// Built-in help-window open state, mirrored so a host can keep a
		// "Quick Help" menu checkmark in sync (QuickHelpOpen).
		mew.WithHelpState(func(open bool) {
			e.helpOpen.Store(open)
		}),
		// Whether the session holds modified work anywhere - the active
		// buffers and the work stacked behind a link follow alike - so a
		// window holding this editor can refuse a close over it.
		mew.WithUnsavedState(func(unsaved bool) {
			e.unsaved.Store(unsaved)
		}),
		// The mouse-pointer affordance: an arrow over link buttons (and
		// while one is captured), the I-beam otherwise. See CursorShapeAt.
		mew.WithPointerRegion(func(col, row, w, h int, arrows []mew.PointerArrowSpan) {
			e.pointerRegionMu.Lock()
			e.pointerRegion = [4]int{col, row, w, h}
			e.pointerArrows = arrows
			e.pointerRegionMu.Unlock()
		}),
		// Since purfecterm v0.2.23 the embedded terminal speaks the STANDARD
		// visual-column contract by default (its flex mode moved to ?7027,
		// opt-in), so mew talks to it exactly as to any terminal — no
		// WithFlexTerminal, no logical-column translation.

		// Live font swaps (set_font "ui-term", "JetBrainsMono"): re-point the
		// alias in the shared text engine — loading the font on demand — and
		// repaint. The engine's epoch bump flushes the terminal's glyph caches
		// on the next paint. No-op in the pure-TUI path (no shared engine).
		// Per-face metric corrections ([fonts] "Noto Kufi Arabic = (baseline:
		// -6)"): recorded on the shared engine, which bumps its epoch so any
		// mask rasterized at the old placement is flushed.
		mew.WithFontAdjust(func(family string, baselineUnits int, sizeScale float64) {
			if eng := text.Shared(); eng != nil {
				eng.SetBaselineAdjust(family, baselineUnits)
				eng.SetSizeScale(family, sizeScale)
			}
		}),
		mew.WithFontSink(func(alias string, names []string) bool {
			eng := text.Shared()
			if eng == nil {
				return false
			}
			ok := eng.UseFont(alias, names...)
			e.Update()
			return ok
		}),
		// Startup font registration ([fonts] name->path, [window] fonts_path):
		// register the explicit files and add the search directories to the
		// shared engine before any name resolves. The [window] ui_term alias is
		// applied afterward through the FontSink above. No-op in the pure-TUI
		// path (no shared engine).
		mew.WithFontConfig(func(files map[string]string, searchPaths []string) {
			eng := text.Shared()
			if eng == nil {
				return
			}
			for _, dir := range searchPaths {
				eng.AddFontSearchPath(dir)
			}
			for family, path := range files {
				_ = eng.RegisterFontFile(family, path)
			}
		}),
		// Publish the RTL-mark rendering hint where the backends can read it
		// (core, since the TUI backend has no font engine).
		mew.WithRtlMarkModeSink(func(mode string) {
			core.SetRtlMarkMode(mode)
		}),
	}
	if e.mewFileSystem != nil {
		options = append(options, mew.WithMewFileSystem(e.mewFileSystem))
	}
	if cfg := e.configText(); cfg != "" {
		options = append(options, mew.WithConfigText(cfg))
	}
	if e.showDesktop != nil {
		options = append(options, mew.WithShowDesktop(e.showDesktop))
	}
	if e.hideDesktop != nil {
		options = append(options, mew.WithHideDesktop(e.hideDesktop))
	}
	// Draw the editor scrollbars ourselves ONLY on the graphical (SDL) host,
	// which paints a pixel-space bar (editor_mew_scrollbar.go). Setting this
	// tells mew to reserve the bar's column, leave it empty, and stop
	// hit-testing it. The TUI host has no pixel surface to draw on, so it must
	// NOT suppress mew's own text scrollbar — otherwise a viewport that needs a
	// bar shows none. (Hosted-terminal scrollbars are unaffected either way.)
	if hostterm.Detect() == hostterm.TerminalSDL {
		options = append(options, mew.WithScrollbarRegions(func(regions []mew.ScrollbarRegion) {
			if e.bars.set(regions) {
				e.Update()
			}
		}))
	}

	// Run the session. A host-injected argv wins (full mew command-line launch:
	// multi-file, per-file options, +N); otherwise filename wins over value (per
	// the contract), and a caret opens the file at that position via mew's +N.
	var content string
	var err error
	switch {
	case len(e.launchArgv) > 0:
		err = mew.EditArgv(e.launchArgv, options...)
	case e.filename != "" && e.caret != "":
		err = mew.EditArgs(fmt.Sprintf("+%s %q", caretLine(e.caret), e.filename), options...)
	case e.filename != "":
		err = mew.EditFile(e.filename, options...)
	default:
		content, err = mew.EditContent(e.value, options...)
	}
	e.err = err

	// Session ended: report the result. File-backed edits carry the filename
	// (the app reads the file through the FS); ephemeral edits carry the value.
	if e.onCommit != nil {
		if e.filename != "" {
			e.onCommit("", e.filename)
		} else {
			e.onCommit(content, "")
		}
	}
}

// configText builds a mew [options] snippet from the rich properties the app
// explicitly set; unset ones inherit mew's own resolution. Empty when the app
// overrode nothing.
func (e *Editor) configText() string {
	var b strings.Builder
	n := 0
	add := func(format string, a ...any) { fmt.Fprintf(&b, format, a...); n++ }
	if e.wrapSet {
		add("wordWrap=%v\n", e.wrap)
	}
	if e.tabSizeSet {
		add("tabSize=%d\n", e.tabSize)
	}
	if e.syntax != "" {
		add("syntax=%s\n", e.syntax)
	}
	if e.lineNumbersSet {
		add("showLineNumbers=%v\n", e.lineNumbers)
	}
	// NOTE: `readonly` is accepted but not yet enforced — mew has no read-only
	// session mode. That's a mew-side follow-up.
	if n == 0 {
		return ""
	}
	return "[options]\n" + b.String()
}

// caretLine returns the line component of a "line[:col]" caret string.
func caretLine(caret string) string {
	if i := strings.IndexByte(caret, ':'); i >= 0 {
		return caret[:i]
	}
	return caret
}

// Close ends the mew session (EOF on its input) and releases the surface.
// Idempotent.
// CursorShapeAt answers the desktop's per-pixel cursor query for the mew
// surface. mew (a full editor, not a scrollbar-fringed terminal) owns the
// affordance entirely via the I-beam region it publishes (WithPointerRegion):
// the text I-beam over the focused window's editable text EXCEPT on a
// link-button span, and the ordinary arrow everywhere else — buttons, the
// gutter, the modebar and other chrome, an unfocused pane, and the document
// area while a prompt awaits input. Resolved locally from the pointer's cell,
// so no mouse motion has to round-trip through mew.
func (e *Editor) CursorShapeAt(x, y core.Unit) core.CursorShape {
	// A hosted terminal answers for its own surface: it has scrollbars of its
	// own, and only it knows where their lanes fall. The Editor declares
	// CursorShapesSubtree, so the descent stops here and never reaches the
	// child on its own — the delegation has to be explicit.
	if t, lx, ly, ok := e.hostedTermAt(x, y); ok {
		return t.CursorShapeAt(lx, ly)
	}
	if e.overEditorScrollbar(x, y) {
		return core.CursorDefault // chrome, not text
	}
	if e.pointerInText(x, y) {
		return core.CursorText
	}
	return core.CursorDefault
}

// hostedTermAt finds the hosted terminal whose placed surface contains the
// local point, returning the point in that CHILD's own units. Mirrors the
// rectangle test clearHostedHoverOutside uses, so hover and cursor agree on
// which child owns a pixel.
func (e *Editor) hostedTermAt(x, y core.Unit) (*PurfecTerm, core.Unit, core.Unit, bool) {
	cw, ch := e.cellDims()
	if cw <= 0 || ch <= 0 {
		return nil, 0, 0, false
	}
	e.termMu.Lock()
	defer e.termMu.Unlock()
	for _, s := range e.termSurfaces {
		if s.term == nil || s.clipWidth <= 0 || s.clipHeight <= 0 {
			continue
		}
		x0 := core.Unit(s.col-1) * cw
		y0 := core.Unit(s.row-1) * ch
		x1 := x0 + core.Unit(s.width)*cw
		y1 := y0 + core.Unit(s.height)*ch
		if x >= x0 && x < x1 && y >= y0 && y < y1 {
			return s.term, x - x0, y - y0, true
		}
	}
	return nil, 0, 0, false
}

// CursorShape is the position-less fallback (used only if the cursor descent
// cannot supply a point): a text editor surface defaults to the I-beam.
func (e *Editor) CursorShape() core.CursorShape {
	return core.CursorText
}

// CursorShapesSubtree marks this trinket as owning the cursor across its whole
// surface (core.CursorShapesSubtree). The Editor is a Container, and the cursor
// descent skips a container ENTRY to keep a delegating window from recursing —
// which silently discarded every shape the editor resolved whenever it was a
// window's own content: no arrow over the reserved scrollbar column, none over
// the browse-mode link buttons, just the blanket I-beam from CursorShape.
// CursorShapeAt answers from mew's published region alone and never re-enters
// the descent, so consulting it is safe.
func (e *Editor) CursorShapesSubtree() {}

// pointerCell maps a local pointer coordinate to the 1-based terminal cell
// mew addresses it by — the SAME trip a mouse event makes (screenToCellGfx).
//
// The scaling matters and is easy to lose: clicks arrive at the host's snapped
// unit rate while the terminal lays its grid out at the renderer's ppu, so a
// raw x/cellWidth drifts from the cell a click lands on the moment those two
// rates diverge (any zoom away from the size where they coincide). Dividing
// without it put the cursor query on a different cell than the mouse path,
// which large targets survive and one-column ones do not: the reserved
// scrollbar column and short link-button spans both stopped showing the arrow.
func (e *Editor) pointerCell(x, y core.Unit) (col, row int, ok bool) {
	cw, chh := e.cellDims()
	if cw <= 0 || chh <= 0 {
		return 0, 0, false
	}
	// The pointer scales onto the widget's snapped device frame (hitK) and the
	// walk compares against boundaries rounded exactly as fillPixels rounds
	// them — the same trip the terminal's own hit tests make.
	ppu := e.gfx.ppu
	if ppu <= 0 {
		ppu = 1
	}
	px, py := e.gfxPointerPx(x, y)
	col, row = 1, 1
	for px >= cellBoundaryPx(float64(col)*float64(cw), ppu) {
		col++
	}
	for py >= cellBoundaryPx(float64(row)*float64(chh), ppu) {
		row++
	}
	return col, row, true
}

// pointerInText reports whether the local point falls on the focused mew
// window's editable text — inside the published I-beam rectangle and not on a
// link-button exclusion span.
func (e *Editor) pointerInText(x, y core.Unit) bool {
	col, row, ok := e.pointerCell(x, y)
	if !ok {
		return false
	}

	e.pointerRegionMu.Lock()
	r := e.pointerRegion
	arrows := e.pointerArrows
	e.pointerRegionMu.Unlock()

	if r[2] <= 0 || r[3] <= 0 { // no I-beam region
		return false
	}
	if col < r[0] || col >= r[0]+r[2] || row < r[1] || row >= r[1]+r[3] {
		return false // outside the rectangle
	}
	for _, a := range arrows { // a link button within the rectangle: arrow
		if row == a.Row && col >= a.Col && col < a.Col+a.Width {
			return false
		}
	}
	return true
}

func (e *Editor) Close() {
	e.mu.Lock()
	if !e.inputClosed {
		e.inputClosed = true
		if e.inPipeW != nil {
			e.inPipeW.Close()
		}
	}
	e.mu.Unlock()
	if e.PurfecTerm != nil {
		e.PurfecTerm.Close()
	}
}

// Wait blocks until the mew session has ended.
func (e *Editor) Wait() { <-e.done }

// Err blocks until the session ends and returns mew's result.
func (e *Editor) Err() error {
	<-e.done
	return e.err
}

// --- Edit menu / clipboard / context menu (the editActor standard) ---
//
// The mew Editor overrides the embedded PurfecTerm's edit actions: the
// terminal's semantics (grid-selection copy, no-op cut, raw bracketed paste)
// are wrong for a mew document. Every action delegates to a mew command
// through the host port, so the block logic — what the current selection IS,
// whether cut applies, how a paste lands — lives in mew (os_copy / os_cut /
// os_paste / os_select_all), exactly where the same actions are key-bindable.

// execMew injects a mew command from a UI thread. Safe before the session
// starts (the unbound port refuses) and after it ends.
func (e *Editor) execMew(cmd string) {
	if e.port != nil {
		e.port.Execute(cmd)
	}
}

// KeyBinding resolves the key a mew command is bound to, for advertising a mew
// binding in host UI — a menu item's shortcut column, for a key mew handles and
// the toolkit never sees. It answers what mew's own %keys#action|preferred%
// code would; preferred picks among several bindings and is the fallback when
// none is bound. Empty before the session starts.
func (e *Editor) KeyBinding(action, preferred string) string {
	if e.port == nil {
		return ""
	}
	return e.port.KeyBinding(action, preferred)
}

// Option reads a mew option's current effective value, for host UI that
// REFLECTS editor state rather than only driving it — a menu item's checkmark,
// or a caption naming the value it will change. It answers what mew's own
// get_option would. Empty before the session starts, and for an unknown name.
func (e *Editor) Option(name string) string {
	if e.port == nil {
		return ""
	}
	return e.port.Option(name)
}

// Execute injects a mew command from a UI thread — the generic seam a host
// uses to drive this editor from its own affordances (a menu item running
// buffer_open_file or help_toggle, say). Marshaled onto mew's main loop with
// keystroke safety; safe before the session starts and after it ends.
func (e *Editor) Execute(cmd string) { e.execMew(cmd) }

// QuickHelpOpen reports whether mew's built-in help window is currently open
// (mirrored from mew via WithHelpState), so a host can keep a "Quick Help"
// menu checkmark in sync with it.
func (e *Editor) QuickHelpOpen() bool { return e.helpOpen.Load() }

// HasUnsavedWork reports whether the session holds modified work — every
// buffer it has open, including the ones stacked behind a link follow, not
// just what is on screen (mirrored from mew via WithUnsavedState). A window
// holding this editor asks before closing: this is the whole reason a close
// becomes a question instead of an act.
func (e *Editor) HasUnsavedWork() bool { return e.unsaved.Load() }

// RequestClose asks the session to close everything it holds — mew's
// session_close, which walks the viewports and raises mew's own lose-changes
// prompt over each piece of unsaved work. It reports whether the session TOOK
// the question on; when it did, the caller must NOT close the window, because
// the answer belongs to the user and arrives later: a session that closes out
// ends and fires commit, which is what closes the window for real.
//
// False means there was no live session to ask (before it starts, or after it
// ends), so the caller may close outright.
func (e *Editor) RequestClose() bool {
	if e.port == nil {
		return false
	}
	return e.port.Execute("session_close")
}

// Copy places mew's marked block on the system clipboard.
func (e *Editor) Copy() { e.execMew("os_copy") }

// Cut places mew's marked block on the system clipboard and removes it
// (bypassing mew's kill ring, so the two clipboards never interfere).
func (e *Editor) Cut() { e.execMew("os_cut") }

// Paste applies the system clipboard: replacing the marked block when the
// caret is engaged with it (block_from_file semantics), else inserting at
// the caret (buffer_insert_file semantics).
func (e *Editor) Paste() { e.execMew("os_paste") }

// SelectAll marks the whole mew buffer as the block.
func (e *Editor) SelectAll() { e.execMew("os_select_all") }

// CutEnabled overrides the embedded terminal's "never": a mew document's
// block CAN be cut — unless the focused buffer is read-only (mirrored from
// mew via WithEditState), in which case the Edit menu greys Cut out.
func (e *Editor) CutEnabled() bool { return !e.readOnlyFocused.Load() }

// HasSelection overrides the embedded terminal's grid-selection answer:
// always true, so the desktop never raises its own "nothing selected"
// notice — mew reports "No block marked" through its own UI, where the
// answer is actually known.
func (e *Editor) HasSelection() bool { return true }

// postUI runs fn on the desktop's UI loop (a zero-delay desktop timer, the
// thread-safe way in), falling back to inline when no desktop is reachable
// (headless tests). Called from mew's session goroutine.
func (e *Editor) postUI(fn func()) {
	if d := e.findDesktop(); d != nil {
		d.StartTimer(0, fn)
		return
	}
	fn()
}

// mewContextMenuItems builds the right-click menu — the same items, in the
// same order, as the TextInput control's menu, each action the matching
// Edit-menu action (routed through mew).
func (e *Editor) mewContextMenuItems() []termMenuItem {
	return []termMenuItem{
		{label: "Cut", action: e.Cut},
		{label: "Copy", action: e.Copy},
		{label: "Paste", action: e.Paste},
		{separator: true},
		{label: "Select All", action: e.SelectAll},
	}
}

// showMewContextMenu pops the right-click menu anchored at a 1-based
// terminal cell — the cell mew validated as being within the focused
// window's editing area. Presentation is the shared terminal items-menu
// (the TextInput / PurfecTerm popup style).
func (e *Editor) showMewContextMenu(col, row int) {
	e.showTermItemsMenu(e.cellToLocal(col, row), e.mewContextMenuItems())
}

// terminalRestorer is a backend that owns terminal state it must hand back
// before the process ends. Backends that render to a window (SDL) do not
// implement it, and need not: closing the process is enough there.
type terminalRestorer interface{ RestoreTerminal() }

// restoreHostTerminal asks whatever backend is driving this editor's desktop to
// put the terminal back. Reached from mew's emergency-exit path, which runs on
// a signal-handler goroutine after the DEADCAT dump, so it must not touch the
// event loop, must tolerate a half-built desktop, and must not exit.
//
// The type assertion is how the trinket stays out of the backend's business: a
// desktop rendering to a window has no terminal to restore and simply does not
// satisfy the interface, so this is a no-op there rather than a special case.
func (e *Editor) restoreHostTerminal() {
	d := e.findDesktop()
	if d == nil {
		return
	}
	if r, ok := d.Backend().(terminalRestorer); ok {
		r.RestoreTerminal()
	}
}

// ptyProvider answers mew's exec: it grants a REAL shell in a real directory.
//
// That is the right answer for THIS host and only because of who is asking.
// The root mew belongs to the person running it, on their own machine, so a
// request for "bash" in their own project directory is a request they are
// already entitled to make from any shell they own. A host embedding mew in
// someone else's application supplies its own provider instead, and mew cannot
// tell the difference: the session speaks only bytes.
//
// The provider is where the gate lives, so it is the one place a policy could
// be added - a directory allowlist, a command allowlist, a prompt. There is
// none here on purpose: adding one would be theatre when the same user can
// open a terminal themselves.
func (e *Editor) ptyProvider(req mew.PTYRequest) (mew.PTYSession, error) {
	cmdName := strings.TrimSpace(req.Command)
	if req.Shell {
		// mew's `shell` command asks for the user's LOGIN SHELL without naming
		// one, because which shell that is belongs to the operating system and
		// to this user's account — neither of which mew can see. Answering it is
		// this host's job precisely because this host is the user's own machine.
		if cmdName != "" {
			return nil, fmt.Errorf("shell request also named %q", cmdName)
		}
		cmdName = hostLoginShell()
	}
	if cmdName == "" {
		return nil, fmt.Errorf("no command given")
	}
	path, err := exec.LookPath(cmdName)
	if err != nil {
		return nil, fmt.Errorf("%s: not found", cmdName)
	}

	// A blank CWD means the buffer has no filename, and the host decides what
	// that means. For the user's own mew, that is their home directory.
	dir := ""
	if req.CWD != "" {
		if p, ok := localPathFromURL(req.CWD); ok {
			dir = p
		}
	}
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}

	// A request that wires any standard stream to a pipe is a FILTER, not a
	// terminal: the child runs headless on ordinary pipes so mew can feed its
	// stdin and read its stdout/stderr apart. This host handles the two pure
	// cases — all-terminal (below) and all-pipe — and refuses a mix for now
	// rather than half-wire it; the mixed pty+pipe case (a colourful stdout with
	// a piped stderr) is representable in the request and a later host can add it.
	if !stdioAllTerminal(req) {
		if !stdioAllPipe(req) {
			return nil, fmt.Errorf("this host cannot yet mix pty and pipe stdio")
		}
		// A filter child sees pipes, not a terminal: inherit the ambient
		// environment unchanged. No TERM advertisement — isatty is false, so it
		// will not colour anyway, and a bogus TERM would only mislead a program
		// that checks it.
		return newFilterSession(path, dir, os.Environ(), req.Args)
	}

	// Advertise the embedded terminal's identity to the child (last wins over any
	// inherited TERM/TERM_PROGRAM). TERM must name a terminfo entry that actually
	// exists on the host: PurfectermTerm ("xterm-purfecterm") has none, so
	// terminfo-strict programs (zsh's ZLE, `clear`, vim) break on it — zsh can't
	// even find cub1 to move the cursor left, so backspace overwrites with a bare
	// space (bash's readline tolerates the unknown TERM, which masked this). Name
	// the universally-present xterm-256color, which PurfectermTerm is a superset
	// of, and carry the purfecterm IDENTITY on TERM_PROGRAM — which is how
	// hostterm.Detect classifies a nested instance anyway (TERM_PROGRAM or TERM).
	env := append(os.Environ(),
		"TERM=xterm-256color",
		"TERM_PROGRAM="+hostterm.PurfectermTermProgram,
		"COLORTERM=truecolor")
	// How a machine makes a terminal is the one part of this that is not the
	// same everywhere: a pty pair on POSIX, a pseudoconsole bound to the child
	// before it starts on Windows. hostPTY is per-platform; everything above
	// it, and mew entirely, is not.
	sess, err := hostPTY(path, dir, env, req.Args, req.Cols, req.Rows, req.Method)
	if err != nil {
		return nil, err
	}
	// Tell the child how big its window is in PIXELS as well as cells. A
	// program drawing characters never asks; one drawing PICTURES sizes its
	// viewport from TIOCGWINSZ rather than querying the terminal, and a
	// window reported as 0x0 pixels leaves it running and rendering nothing.
	// That is exactly how a browser in the terminal (awrit) fails here while
	// chafa, which only writes escapes, is fine.
	sess = withPixelWinsize(sess, e.childWindowPx)
	// Input written to the child wakes its caret: mew owns this session, so
	// the trinket's own input path never sees it. See editor_mew_blink.go.
	return e.wakeOnWrite(sess), nil
}

// stdioAllTerminal reports whether every standard stream is wired to the
// session's pseudo-terminal — the historical terminal session and the zero value.
func stdioAllTerminal(req mew.PTYRequest) bool {
	return req.Stdin == mew.StreamTerminal &&
		req.Stdout == mew.StreamTerminal &&
		req.Stderr == mew.StreamTerminal
}

// stdioAllPipe reports whether every standard stream is wired to its own pipe —
// a headless filter child.
func stdioAllPipe(req mew.PTYRequest) bool {
	return req.Stdin == mew.StreamPipe &&
		req.Stdout == mew.StreamPipe &&
		req.Stderr == mew.StreamPipe
}

// localPathFromURL turns a canonical file:// URL back into an OS path, or
// reports false for any other scheme. mew's own answer is used rather than a
// second copy of the rule: the drive-letter repair a Windows path needs is
// exactly the kind of thing two implementations drift on, and they would
// drift on one platform only. A host that grants only sandboxed sessions
// would reject here instead of resolving anything.
func localPathFromURL(u string) (string, bool) { return mew.LocalPathFromURL(u) }

// --- terminal surfaces: one child PurfecTerm per running session -----------
//
// mew does no terminal emulation. It forwards a session's raw bytes and, after
// every render, republishes the complete set of rectangles where visible
// sessions belong. Each session gets a REAL PurfecTerm here — the same trinket
// that renders any other terminal — laid over exactly the cells the document
// would have drawn into, inside this editor's own outer surface.
//
// Several can be live at once, one per viewport running a session, which is why
// they are a map rather than a single overlay.
//
// They receive no input events. mew keeps keyboard focus, so a keystroke runs
// through mew's own keymap and reaches the child process via pty_send; these
// are displays and nothing else. That is also what keeps ^B N switching buffers
// while a shell is running.

// termSurface is one session's child terminal and where it currently sits, in
// 1-based cells of this editor's surface. A zero width or height means it is
// not visible this frame.
type termSurface struct {
	term *PurfecTerm
	// pending collects the bytes the child produces for its process while a
	// hook call is being served (draining true): mouse reports, encoded keys.
	// The hook returns them to mew, which writes them to the session — the
	// one door. Bytes produced OUTSIDE a hook call (a context-menu Paste
	// firing later, on the UI thread) go back to mew through its command
	// port instead; see termInput.
	pending  []byte
	draining bool
	// Where the grid sits, and what part of it is visible. Usually identical;
	// they differ when the viewport is partially obscured or scrolled, and the
	// surface then draws at its own origin with only the intersection shown.
	// This is the PRIMARY placement — the canonical/focused tile, which sizes the
	// grid (SetBounds) and hosts the live child.
	col, row              int
	width, height         int
	clipCol, clipRow      int
	clipWidth, clipHeight int
	// focused: this surface owns the platform caret this frame.
	focused bool
	// mirrors are the session's OTHER tiles (tiles↔viewports is many-to-many):
	// the same child grid painted read-only into each, clipped/letterboxed, with
	// no SetBounds — so a mirror's size never resizes the grid.
	mirrors []termRect
	// pinnedTile is the tile a scrollbar drag began in. The child caches ONE
	// gfx frame (the last tile painted), so a drag/hover has to restore the
	// frame of the tile it is happening in before the child hit-tests — and a
	// drag keeps the tile it started in even as the pointer wanders out of it.
	pinnedTile tileGeom
}

// tileGeom is a tile's placement in this editor's 1-based cells — enough to
// recompute the child's paint frame for that tile at mouse time (a lockstep
// child's frame is pure pitch, so no painter is needed). The zero value is
// invalid.
type tileGeom struct {
	col, row      int
	width, height int
	valid         bool
}

// termRect is one mirror placement of a session's grid, in 1-based cells.
type termRect struct {
	col, row              int
	width, height         int
	clipCol, clipRow      int
	clipWidth, clipHeight int
}

// terminalOpen creates the child for a new session. Called on mew's main loop.
func (e *Editor) terminalOpen(id string, cols, rows int) {
	// The grid size follows SetBounds (updateTerminalSize divides the bounds
	// by the cell size), so there is nothing to set here: cols/rows arrive
	// again with the first Place.
	t := NewPurfecTerm()
	t.Init(t)
	t.SetEditorMode(false)
	// This child paints its own cell pitch into a clip this editor cuts at that
	// same pitch (paintTerminalSurfaces measures the clip with the terminal
	// font's stride, not the host cell grid). Left to measure its width against
	// the host grid it would overshoot the pitch at fractional zoom — gaining
	// phantom columns and snapping its scrollbar lane clean past the clip, so no
	// bar shows. Lockstep it to its own pitch, which is what the clip guarantees.
	t.SetLockstepPitch(true)
	// PARENTED, though never added to any container: the child is painted and
	// fed events by this editor alone. The parent link is for the lookups
	// that walk it — FindGraphicalFrames (which turns on the whole graphical
	// input path: the scrollbar, selection, the context menu), the popup
	// controller the context menu opens on, and findDesktop for the
	// clipboard. Without it the child behaves like a terminal on a host with
	// none of those, because that is what an orphan is.
	e.AddChild(t)
	// Every byte the child produces for its process funnels here, whichever
	// internal path made it: the gfx mouse reporter, the cli pseudo-key
	// relay, a pasted clipboard.
	e.startTermOutPump()
	t.SetInputSink(func(b []byte) { e.termInput(id, b) })
	// And every grid size it SETTLES ON goes back to mew, which resizes the
	// child process to it. This display is the only thing that knows the
	// answer: mew places the surface as a cell rectangle, but how much of
	// that rectangle is text depends on this terminal's own font metrics and
	// on the scrollbar lane it reserves for itself. Left unsaid, the guest is
	// told it has the rectangle's full width, and every line it draws to that
	// width wraps and scrolls a screenful into scrollback — per frame,
	// without bound.
	t.SetResizeSink(func(cols, rows int) { e.termGrid(id, cols, rows) })
	// It is a display, not a focus participant: its focus is DECLARED by this
	// editor (SetEmbeddedFocus) rather than won from the toolkit. Saying so
	// keeps a hosted mouse press from marking it focused behind our back.
	t.SetFocusPolicy(core.NoFocus)
	// The child measures ITS OWN font to size its grid, so it has to be the
	// same font this editor measures for its columns. Different fonts mean
	// two pitches: the rectangle would be placed correctly and the child
	// would draw at a different stride inside it.
	if f := e.TerminalFont(); f != nil {
		t.SetTerminalFont(f)
	}
	e.termMu.Lock()
	if e.termSurfaces == nil {
		e.termSurfaces = make(map[string]*termSurface)
	}
	e.termSurfaces[id] = &termSurface{term: t}
	e.termMu.Unlock()
}

// terminalSetLogicalSize backs mew's SetLogicalSize hook. It pins the child
// terminal's LOGICAL size — the size the child is told it has, independent of
// the visible tile — so a --size or --minimum session renders that many cells
// and scrolls, rather than reflowing to the pane. cols,rows of 0 mean "follow
// the visible surface", which is exactly purfecterm's own "use physical" (0)
// convention, so it maps straight through. purfecterm names the size as
// (rows, cols); mew's hook gives (cols, rows). The per-frame physical fit
// (updateTerminalSize) leaves the logical size alone, so this holds until mew
// changes it.
func (e *Editor) terminalSetLogicalSize(id string, cols, rows int) {
	e.termMu.Lock()
	s := e.termSurfaces[id]
	e.termMu.Unlock()
	if s == nil || s.term == nil {
		return
	}
	t := s.term.Terminal()
	if t == nil {
		return
	}
	if buf := t.Buffer(); buf != nil {
		buf.SetLogicalSize(rows, cols)
	}
}

// terminalSnapshot backs mew's Snapshot hook: the session's scrollback as text,
// folded into the buffer when the session ends. ansi keeps the full escape/SGR
// stream (colour, layout); otherwise it is plain text with the sequences
// stripped. purfecterm serializes both forms — SaveScrollbackANS keeps the
// stream, SaveScrollbackText strips it — so ansi is just that choice.
//
// A capture always trims the empty tail of the screen grid: a session that used
// only the top rows should not fold the blank rest of the screen in as empty
// lines. (The ANS form still carries its full reload payload; only the blank
// content lines drop.)
func (e *Editor) terminalSnapshot(id string, ansi bool) string {
	e.termMu.Lock()
	s := e.termSurfaces[id]
	e.termMu.Unlock()
	if s == nil || s.term == nil {
		return ""
	}
	t := s.term.Terminal()
	if t == nil {
		return ""
	}
	opts := purfecterm.ScrollbackSaveOptions{TrimTrailingBlankLines: true}
	if ansi {
		return t.SaveScrollbackANSOpts(opts)
	}
	return t.SaveScrollbackTextOpts(opts)
}

// captureRelay forwards a session's purfecterm capture events across the
// embedded seam to a mew CaptureSink. It embeds NopCaptureObserver so events
// added to CaptureObserver later stay no-ops here until this relay forwards
// them too. wantsLines gates the per-line serialization so a raw session never
// pays for it.
type captureRelay struct {
	purfecterm.NopCaptureObserver
	sink       mew.CaptureSink
	wantsLines bool
	wantsLive  bool
}

func (r captureRelay) OnOutput(data []byte) { r.sink.Output(data) }

func (r captureRelay) OnLineOff(line []purfecterm.Cell, info purfecterm.LineInfo) {
	if r.wantsLines {
		r.sink.LineOff(purfecterm.SerializeLineANS(line, info))
	}
}

// The live-rung structural events, forwarded to the sink only for a live
// session. purfecterm already flushes any pending write-run before each
// structural event and at end of feed, so the order the sink sees matches the
// screen's.
func (r captureRelay) OnWrite(x, y int, text, sgr string) {
	if r.wantsLive {
		r.sink.LiveWrite(x, y, text, sgr)
	}
}
func (r captureRelay) OnCursorMove(x, y int) {
	if r.wantsLive {
		r.sink.LiveCursorMove(x, y)
	}
}
func (r captureRelay) OnNewline(x, y int) {
	if r.wantsLive {
		r.sink.LiveNewline(x, y)
	}
}
func (r captureRelay) OnLineWrap(x, y int) {
	if r.wantsLive {
		r.sink.LiveLineWrap(x, y)
	}
}
func (r captureRelay) OnBackspace(x, y int) {
	if r.wantsLive {
		r.sink.LiveBackspace(x, y)
	}
}
func (r captureRelay) OnScrollLineOff(n int) {
	if r.wantsLive {
		r.sink.LiveScrollLineOff(n)
	}
}
func (r captureRelay) OnClearScreen(sgr string) {
	if r.wantsLive {
		r.sink.LiveClearScreen(sgr)
	}
}
func (r captureRelay) OnClearEndOfLine(x, y int, sgr string) {
	if r.wantsLive {
		r.sink.LiveClearEndOfLine(x, y, sgr)
	}
}
func (r captureRelay) OnClearBeginOfLine(x, y int, sgr string) {
	if r.wantsLive {
		r.sink.LiveClearBeginOfLine(x, y, sgr)
	}
}
func (r captureRelay) OnClearLine(y int, sgr string) {
	if r.wantsLive {
		r.sink.LiveClearLine(y, sgr)
	}
}
func (r captureRelay) OnClearEndOfScreen(x, y int, sgr string) {
	if r.wantsLive {
		r.sink.LiveClearEndOfScreen(x, y, sgr)
	}
}
func (r captureRelay) OnClearBeginOfScreen(x, y int, sgr string) {
	if r.wantsLive {
		r.sink.LiveClearBeginOfScreen(x, y, sgr)
	}
}
func (r captureRelay) OnDeleteChars(x, y, n int, sgr string) {
	if r.wantsLive {
		r.sink.LiveDeleteChars(x, y, n, sgr)
	}
}
func (r captureRelay) OnInsertChars(x, y, n int, sgr string) {
	if r.wantsLive {
		r.sink.LiveInsertChars(x, y, n, sgr)
	}
}
func (r captureRelay) OnEraseChars(x, y, n int, sgr string) {
	if r.wantsLive {
		r.sink.LiveEraseChars(x, y, n, sgr)
	}
}

// terminalSetCaptureSink backs mew's SetCaptureSink hook: point a session's
// purfecterm CaptureObserver at the mew sink so the terminal's output is relayed
// across the seam, or clear it when sink is nil. Runs on the same loop as
// terminalFeed, so the events land on mew's main loop in feed order.
func (e *Editor) terminalSetCaptureSink(id string, sink mew.CaptureSink) {
	e.termMu.Lock()
	s := e.termSurfaces[id]
	e.termMu.Unlock()
	if s == nil || s.term == nil {
		return
	}
	t := s.term.Terminal()
	if t == nil {
		return
	}
	if sink == nil {
		// End of session: flush the on-screen tail (content that never scrolled
		// off) as OnLineOff events to the still-registered relay, then clear.
		// Harmless for a raw relay, which ignores line events. The live mirror
		// already reflects the final screen in place, so it needs no flush.
		t.EmitRemainingCaptureLines()
		t.SetCaptureObserver(nil)
		t.SetCaptureLive(false)
		return
	}
	wantsLines := false
	if lc, ok := sink.(interface{ WantsLines() bool }); ok {
		wantsLines = lc.WantsLines()
	}
	wantsLive := false
	if lc, ok := sink.(interface{ WantsLive() bool }); ok {
		wantsLive = lc.WantsLive()
	}
	t.SetCaptureObserver(captureRelay{sink: sink, wantsLines: wantsLines, wantsLive: wantsLive})
	t.SetCaptureLive(wantsLive)
}

// terminalFeed hands a session's bytes to its child, verbatim — this is where
// the emulation happens — and returns what the emulator ANSWERED. A terminal
// is interrogated through its output stream (DSR, DA), and the replies its
// parser produces arrive at the same input sink as everything else; collected
// here and returned, they reach the child process synchronously through mew,
// before the event queue is involved at all. Posting them to the queue from
// this handler — which RUNS on the loop that drains the queue — is how a
// guest's startup probe once wedged the whole editor.
func (e *Editor) terminalFeed(id string, p []byte) []byte {
	e.termMu.Lock()
	s := e.termSurfaces[id]
	if s == nil || s.term == nil {
		e.termMu.Unlock()
		return nil
	}
	s.draining = true
	s.pending = nil
	t := s.term
	e.termMu.Unlock()

	t.Feed(p)
	e.Update()

	e.termMu.Lock()
	reply := s.pending
	s.pending = nil
	s.draining = false
	e.termMu.Unlock()
	return reply
}

// terminalPlace receives the COMPLETE set of visible surfaces after each mew
// render. A child absent from the set is not visible this frame, so its
// rectangle is zeroed and it simply is not painted — no incremental
// bookkeeping here to drift out of step with mew's layout.
func (e *Editor) terminalPlace(surfaces []mew.TerminalSurface) {
	e.termMu.Lock()
	for _, s := range e.termSurfaces {
		s.width, s.height, s.clipWidth, s.clipHeight = 0, 0, 0, 0
		s.focused = false
		s.mirrors = s.mirrors[:0]
	}
	for _, want := range surfaces {
		s := e.termSurfaces[want.ID]
		if s == nil {
			continue
		}
		if !want.Primary {
			// A mirror: the same grid drawn read-only into another tile. Recorded
			// for the paint, but never sizes the child (no SetBounds).
			s.mirrors = append(s.mirrors, termRect{
				col: want.Col, row: want.Row, width: want.Width, height: want.Height,
				clipCol: want.ClipCol, clipRow: want.ClipRow,
				clipWidth: want.ClipWidth, clipHeight: want.ClipHeight,
			})
			continue
		}
		// The primary owns the emulator: its rectangle sizes the grid and hosts
		// the live child.
		s.col, s.row = want.Col, want.Row
		s.width, s.height = want.Width, want.Height
		s.clipCol, s.clipRow = want.ClipCol, want.ClipRow
		s.clipWidth, s.clipHeight = want.ClipWidth, want.ClipHeight
		s.focused = want.Focused
	}
	// Hand the focused session's child the whole notion of focus — the caret
	// (position AND DECSCUSR shape), the focused cursor form, the blink, and
	// the emulator's own focused flag. mew keeps keyboard focus, so its keymap
	// still runs; but while you are typing at a shell, the shell is what you
	// are attending to, and a terminal that believes itself unfocused draws a
	// hollow box that does not blink.
	for _, s := range e.termSurfaces {
		if s.term != nil {
			s.term.SetEmbeddedFocus(s.focused && s.clipWidth > 0 && s.clipHeight > 0)
		}
	}
	e.termMu.Unlock()
	e.Update()
}

// notePointer records the precise pointer position on its way into mew.
func (e *Editor) notePointer(x, y core.Unit) {
	e.ptrMu.Lock()
	e.ptrX, e.ptrY, e.ptrValid = x, y, true
	e.ptrMu.Unlock()
}

// The mouse handlers remember the precise position, then behave exactly as
// the embedded editor-mode terminal always has (encode for mew).
func (e *Editor) HandleMousePress(ev core.MousePressEvent) bool {
	e.notePointer(ev.X, ev.Y)
	// The editor's own scrollbars are ours to drive: mew reserved their
	// columns and stopped hit-testing them.
	if ev.Button == core.LeftButton && e.scrollbarPressAt(ev.X, ev.Y) {
		return true
	}
	return e.PurfecTerm.HandleMousePress(ev)
}

func (e *Editor) HandleMouseMove(ev core.MouseMoveEvent) bool {
	e.notePointer(ev.X, ev.Y)
	e.clearHostedHoverOutside(ev.X, ev.Y)
	if e.scrollbarDragMove(ev.X, ev.Y) {
		return true
	}
	if ev.Buttons == 0 {
		e.updateScrollbarHover(ev.X, ev.Y)
	}
	return e.PurfecTerm.HandleMouseMove(ev)
}

// clearHostedHoverOutside clears scrollbar hover on every hosted child whose
// surface the pointer is NOT over. mew's wire only carries events over the
// pty viewport, so a child whose thumb was hovered stays lit when the pointer
// moves onto mew's chrome — or leaves the window by the bottom or right edge,
// which arrives here as the toolkit's out-of-bounds (-1,-1) move. The host is
// the one who still sees the pointer, so the host clears.
func (e *Editor) clearHostedHoverOutside(x, y core.Unit) {
	cw, ch := e.cellDims()
	if cw <= 0 || ch <= 0 {
		return
	}
	e.termMu.Lock()
	type target struct{ t *PurfecTerm }
	var clear []target
	for _, s := range e.termSurfaces {
		if s.term == nil || s.clipWidth <= 0 || s.clipHeight <= 0 {
			continue
		}
		x0 := core.Unit(s.col-1) * cw
		y0 := core.Unit(s.row-1) * ch
		x1 := x0 + core.Unit(s.width)*cw
		y1 := y0 + core.Unit(s.height)*ch
		if x < x0 || x >= x1 || y < y0 || y >= y1 {
			clear = append(clear, target{s.term})
		}
	}
	e.termMu.Unlock()
	for _, c := range clear {
		c.t.updateScrollbarHoverGfx(-1, -1)
	}
}

func (e *Editor) HandleMouseRelease(ev core.MouseReleaseEvent) bool {
	e.notePointer(ev.X, ev.Y)
	if e.scrollbarDragRelease() {
		return true
	}
	return e.PurfecTerm.HandleMouseRelease(ev)
}

// preciseHostedLocal reduces the remembered pointer to its SUB-CELL FRACTION
// within the cell mew reported, provided the two still agree — the wire and
// the memory are updated by the same gesture but travel different paths, so
// the cell is the consistency check. Reports false when the pointer is stale
// or elsewhere, and the caller falls back to cell centers.
//
// It returns a fraction, not a coordinate, on purpose. The remembered pointer
// lives in the HOST's frame (outer editor units); the child paints in its OWN
// frame (its ppu, its hitKX at fractional zoom). Feeding the host-frame pointer
// straight into the child's hit test scaled it by the wrong rate and drifted
// the inner selection whole columns to the right — the outer cell was right,
// the inner one landed off. The cell mew resolved is authoritative; all the
// precise pointer adds is WHERE INSIDE that cell the hand sits, which the child
// then reproduces in its own frame via cellToLocal.
func preciseHostedLocal(px, py core.Unit, valid bool, col, row int, cw, ch core.Unit, ev mew.TerminalMouse) (fracX, fracY float64, ok bool) {
	if !valid || cw <= 0 || ch <= 0 {
		return 0, 0, false
	}
	lx := px - core.Unit(col-1)*cw
	ly := py - core.Unit(row-1)*ch
	// ev.Col/Row are CHILD-LOCAL 1-based cells — mew subtracts the viewport
	// origin before the hook (ptyMouseKey: col := x - w.ContentX) — so the
	// comparison is direct. Getting this wrong is expensive: comparing
	// against screen cells made the row check fail by the viewport's Y
	// offset every time, precision never engaged, and with a deep scrollback
	// one cell of fallback quantization was PAGES of content per step.
	//
	// One cell of slack per axis: the wire travels through mew's loop while
	// the pointer keeps moving, so by the time a cell-crossing event arrives
	// the pointer is often a little past it. That staleness is exactly the
	// precision worth keeping; two cells apart means a different gesture.
	pcx, pcy := int(lx/cw)+1, int(ly/ch)+1
	if lx < 0 {
		pcx = int((lx-cw+1)/cw) + 1 // floor for negatives (capture runs past edges)
	}
	if ly < 0 {
		pcy = int((ly-ch+1)/ch) + 1
	}
	dx, dy := pcx-ev.Col, pcy-ev.Row
	if dx < -1 || dx > 1 || dy < -1 || dy > 1 {
		return 0, 0, false
	}
	// The offset within the WIRE cell, as a fraction of a cell. Clamped to the
	// cell: a pointer that ran a little past the wire (the ±1 slack above)
	// contributes its edge, not a leak into the neighbour — the wire cell, not
	// the stale pointer, decides which cell.
	fracX = clampUnitFrac(float64(lx)/float64(cw) - float64(ev.Col-1))
	fracY = clampUnitFrac(float64(ly)/float64(ch) - float64(ev.Row-1))
	return fracX, fracY, true
}

// clampUnitFrac holds a sub-cell fraction inside [0,1). The upper bound is
// exclusive so the fraction never reaches the next cell's origin.
func clampUnitFrac(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 0.999 {
		return 0.999
	}
	return f
}

// hostedTileAt returns the tile of surface s that the pointer (px,py, this
// editor's units) sits in — the primary or one of its mirrors. The child is
// shown in each at a different size, and the scrollbar's track height is the
// TILE's, so an interaction has to know which one. Falls back to the primary
// when the pointer is invalid or over no tile (a wheel with no motion, say).
// Caller holds termMu.
func (e *Editor) hostedTileAt(s *termSurface, px, py core.Unit, pvalid bool, cw, ch core.Unit) tileGeom {
	primary := tileGeom{s.col, s.row, s.width, s.height, s.clipWidth > 0 && s.clipHeight > 0}
	if !pvalid || cw <= 0 || ch <= 0 {
		return primary
	}
	in := func(col, row, w, h int) bool {
		if w <= 0 || h <= 0 {
			return false
		}
		x0 := core.Unit(col-1) * cw
		y0 := core.Unit(row-1) * ch
		return px >= x0 && px < x0+core.Unit(w)*cw && py >= y0 && py < y0+core.Unit(h)*ch
	}
	if primary.valid && in(s.col, s.row, s.width, s.height) {
		return primary
	}
	for _, m := range s.mirrors {
		if m.clipWidth > 0 && m.clipHeight > 0 && in(m.col, m.row, m.width, m.height) {
			return tileGeom{m.col, m.row, m.width, m.height, true}
		}
	}
	return primary
}

// applyHostedTileFrame restores the child's cached paint frame to a given tile,
// so the scrollbar hit test and track height come out as that tile's — not the
// last tile that happened to paint. A hosted child locksteps to its own pitch,
// so the frame is exactly round(units*ppu) with no host-grid snap (hitK = 1);
// that is what paintGraphical computed for this tile, reproduced here without a
// painter. ppu is cached from the last paint and is the same for every tile.
func (e *Editor) applyHostedTileFrame(t *PurfecTerm, g tileGeom, cw, ch core.Unit) {
	if !g.valid || !t.gfx.lockstepPitch {
		return // non-lockstep: leave the cached frame (never the hosted case)
	}
	ppu := t.gfx.ppu
	if ppu <= 0 {
		return
	}
	t.gfx.vpWpx = math.Round(float64(core.Unit(g.width)*cw) * ppu)
	t.gfx.vpHpx = math.Round(float64(core.Unit(g.height)*ch) * ppu)
	t.gfx.hitKX, t.gfx.hitKY = 1, 1
}

// terminalMouse hands one mouse event to the session's child terminal AS THE
// TOOLKIT EVENT it would have received in the tree, and returns whatever
// bytes the child produced for its process, plus whether it took the event.
//
// The child decides everything itself, with the SAME code the standalone
// trinket runs: the scrollbar wins over app tracking, shift bypasses tracking
// for local selection, right-click opens the context menu (or reports, when
// the app tracks), the wheel scrolls scrollback or reports, and the Mouse
// Reporting checkbox on its own context menu is honored — it is the child's
// own flag, consulted by the child's own handlers. Re-deciding any of that
// here made the hosted terminal a worse terminal than the trinket it wraps,
// one forgotten rule at a time; the tracking-mode gating that used to live
// here is gone for exactly that reason.
//
// Bytes the handlers encode arrive through the child's input sink, which
// collects them while draining is set; they return to mew, which writes them
// to the session. One event in, bytes out, one door.
func (e *Editor) terminalMouse(id string, ev mew.TerminalMouse) ([]byte, bool) {
	cw, ch := e.cellDims()
	e.ptrMu.Lock()
	px, py, pvalid := e.ptrX, e.ptrY, e.ptrValid
	e.ptrMu.Unlock()

	e.termMu.Lock()
	s := e.termSurfaces[id]
	if s == nil || s.term == nil {
		e.termMu.Unlock()
		return nil, false
	}
	s.draining = true
	s.pending = nil
	t := s.term
	// Which tile is this event in? A scrollbar drag already under way keeps the
	// tile it began in (the pointer may have wandered out); otherwise it is the
	// tile the remembered pointer sits in. The child caches ONE gfx frame, so
	// without this the scrollbar's track height and lane column are whichever
	// tile painted last, not the one being used.
	g := s.pinnedTile
	if !((t.gfx.vDragging || t.gfx.hDragging) && g.valid) {
		g = e.hostedTileAt(s, px, py, pvalid, cw, ch)
	}
	col, row := g.col, g.row
	e.termMu.Unlock()

	// Restore this tile's frame before the child hit-tests, then hand it the
	// precise pointer (in this tile's local units) or the wire cell's center.
	e.applyHostedTileFrame(t, g, cw, ch)
	fracX, fracY, precise := preciseHostedLocal(px, py, pvalid, col, row, cw, ch, ev)

	// The middle of the round trip, which neither end's trace can show: the
	// host reports the pointer to mew, mew decides which viewport it landed in
	// and calls back here with a CHILD-LOCAL cell. A wrong cell arriving here
	// is mew's routing; a right cell leaving here wrongly placed is the tile
	// resolution or the sub-cell fraction below it.
	if core.MouseTracing() {
		core.MouseTracef("route  who=%p ev=(col=%d,row=%d act=%v btn=%v) tile=(col=%d,row=%d %dx%d valid=%v) ptr=(%v,%v valid=%v) frac=(%.3f,%.3f precise=%v)",
			t, ev.Col, ev.Row, ev.Action, ev.Button,
			g.col, g.row, g.width, g.height, g.valid,
			px, py, pvalid, fracX, fracY, precise)
	}

	handled := dispatchHostedMouse(t, ev, fracX, fracY, precise)

	e.termMu.Lock()
	// Pin the tile for the life of a scrollbar drag it just started; drop the
	// pin once the drag ends, so a later hover re-resolves the tile.
	if t.gfx.vDragging || t.gfx.hDragging {
		if !s.pinnedTile.valid {
			s.pinnedTile = g
		}
	} else {
		s.pinnedTile = tileGeom{}
	}
	data := s.pending
	s.pending = nil
	s.draining = false
	e.termMu.Unlock()
	return data, handled || len(data) > 0
}

// termInput receives everything the child produces for its process. In a hook
// call it is collected for the hook's reply. Outside one — a context-menu
// Paste firing on the UI thread — it goes to mew's command port as a tinput,
// so the write still happens inside mew, on mew's loop. (tinput addresses the
// FOCUSED session; the menu that produced the bytes was opened on it.)
func (e *Editor) termInput(id string, b []byte) {
	if len(b) == 0 {
		return
	}
	e.termMu.Lock()
	s := e.termSurfaces[id]
	if s != nil && s.draining {
		s.pending = append(s.pending, b...)
		e.termMu.Unlock()
		return
	}
	port := e.port
	e.termMu.Unlock()
	if port == nil {
		return
	}
	// Queued, never blocking and never unbounded. Execute posts to mew's
	// event queue and that post BLOCKS when the queue is full; this is
	// reached from whatever thread the child produced the bytes on — the
	// desktop's paint thread among them — and a UI thread blocked on mew's
	// queue is a frozen application. A goroutine per call would unblock it
	// and replace the freeze with unbounded goroutines, which is worse: the
	// same pressure, spent on memory instead.
	//
	// So: one worker, a small queue, and DROP when it is full. These are
	// bytes for a child process and nothing waits on them; losing some under
	// pressure is a far better failure than growing without limit.
	select {
	case e.termOut <- "tinput " + pawBytesLiteral(b):
	default:
	}
}

// termOutPump is the single consumer of termOut. Started with the first
// hosted terminal; it ends with the session.
func (e *Editor) startTermOutPump() {
	e.termOutOnce.Do(func() {
		e.termOut = make(chan string, 64)
		// Under the lock termGrid reads it through: the sink that calls
		// termGrid runs on the paint thread, not this one.
		e.termMu.Lock()
		e.termGridWake = make(chan struct{}, 1)
		e.termMu.Unlock()
		go func() {
			for {
				select {
				case cmd, ok := <-e.termOut:
					if !ok {
						return
					}
					if p := e.port; p != nil {
						p.Execute(cmd)
					}
				case <-e.termGridWake:
					// Only with somewhere to send it. Before the session's
					// port exists the declaration STAYS latched rather than
					// being drained into nothing; the next frame nudges it
					// again (see nudgeTermGrids), so it lands as soon as
					// there is a mew to tell.
					if p := e.port; p != nil {
						for _, cmd := range e.takeTermGrids() {
							p.Execute(cmd)
						}
					}
				}
			}
		}()
	})
}

// termGrid latches a surface's newly settled grid for delivery to mew. Called
// from the child's resize sink, which fires during paint — so it must not
// block, and must not lose the value either.
func (e *Editor) termGrid(id string, cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	// The child's window in PIXELS, read BEFORE the lock: childWindowPx takes
	// termMu itself, and this mutex does not nest.
	//
	// It is part of the declaration and not merely a detail of it. A pane can
	// change the pixels under an unchanged grid — the display density arriving
	// after the first paint, a zoom step too small to gain or lose a column —
	// and a latch keyed on cells alone calls that a repeat and drops it. The
	// child is then left sizing its pictures to a window it no longer has.
	pxW, pxH := e.childWindowPx()

	e.termMu.Lock()
	if e.termGridWant == nil {
		e.termGridWant = make(map[string][4]int)
	}
	want := [4]int{cols, rows, pxW, pxH}
	if prev, ok := e.termGridWant[id]; ok && prev == want {
		e.termMu.Unlock()
		return
	}
	e.termGridWant[id] = want
	wake := e.termGridWake
	e.termMu.Unlock()
	if wake == nil {
		return
	}
	select {
	case wake <- struct{}{}:
	default: // already awake; it will see the latch
	}
}

// nudgeTermGrids re-wakes the pump if a declaration is still waiting. Called
// once a frame, so a grid settled before the session's port existed is not
// stranded: the retry costs a map length check.
func (e *Editor) nudgeTermGrids() {
	e.termMu.Lock()
	pending := len(e.termGridWant) > 0
	wake := e.termGridWake
	e.termMu.Unlock()
	if !pending || wake == nil {
		return
	}
	select {
	case wake <- struct{}{}:
	default:
	}
}

// takeTermGrids drains the latch into mew commands.
func (e *Editor) takeTermGrids() []string {
	e.termMu.Lock()
	defer e.termMu.Unlock()
	if len(e.termGridWant) == 0 {
		return nil
	}
	cmds := make([]string, 0, len(e.termGridWant))
	for id, g := range e.termGridWant {
		cmds = append(cmds, fmt.Sprintf("terminal_grid %q, %d, %d", id, g[0], g[1]))
	}
	e.termGridWant = nil
	return cmds
}

// pawBytesLiteral renders bytes as a PawScript {bytes ...} literal. The commas
// matter: without them the values concatenate as symbols.
func pawBytesLiteral(b []byte) string {
	var sb strings.Builder
	sb.WriteString("{bytes ")
	for i, c := range b {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "0x%02x", c)
	}
	sb.WriteString("}")
	return sb.String()
}

// dispatchHostedMouse converts one of mew's terminal mouse events into the
// toolkit event the child would have received and runs the child's own
// handler. The cell becomes units at the MIDDLE of the cell — these feed hit
// tests, and an event on a boundary lands on neither side of it. Screen
// coordinates equal local ones, so the wheel latch's offsets come out zero.
func dispatchHostedMouse(t *PurfecTerm, ev mew.TerminalMouse, preciseFracX, preciseFracY float64, precise bool) bool {
	// mew's wire carries a CHILD cell. Place the synthetic pointer at that cell's
	// CENTER in the child's OWN hit-test frame — cellToLocal is the documented
	// inverse of screenToCellGfx, so it carries the child's unit->pixel scale
	// (its ppu, and hitKX at fractional zoom). Computing the center as
	// (col-1)*cw + cw/2 in raw cell units skipped that scale, so the child's hit
	// test re-derived a cell one or more columns to the RIGHT of the wire cell:
	// the outer cell was right, the inner selection landed off.
	o := t.cellToLocal(ev.Col, ev.Row)
	o2 := t.cellToLocal(ev.Col+1, ev.Row+1)
	x := (o.X + o2.X) / 2
	y := (o.Y + o2.Y) / 2
	// The host remembered the precise pointer as it passed through (see
	// notePointer / preciseHostedLocal). It arrives here as a SUB-CELL FRACTION
	// within this same wire cell — placed against the cell's own span in the
	// CHILD's frame, so the sub-cell offset that makes a scrollbar scrub track
	// the hand survives while the cell stays the one mew resolved. Feeding the
	// host-frame pointer straight in scaled it by the wrong rate and walked the
	// inner selection columns off at fractional zoom.
	if precise {
		x = o.X + core.Unit(math.Round(preciseFracX*float64(o2.X-o.X)))
		y = o.Y + core.Unit(math.Round(preciseFracY*float64(o2.Y-o.Y)))
	}
	btn := termMouseButton(ev.Button)
	var mods core.KeyModifiers
	if ev.Shift {
		mods |= core.ShiftModifier
	}
	if ev.Alt {
		mods |= core.MegaModifier
	}
	if ev.Ctrl {
		mods |= core.ControlModifier
	}

	switch ev.Action {
	case mew.TerminalMousePress:
		return t.HandleMousePress(core.MousePressEvent{X: x, Y: y, Button: btn, Modifiers: mods})
	case mew.TerminalMouseRelease:
		return t.HandleMouseRelease(core.MouseReleaseEvent{X: x, Y: y, Button: btn, Modifiers: mods})
	case mew.TerminalMouseMotion:
		return t.HandleMouseMove(core.MouseMoveEvent{X: x, Y: y, Modifiers: mods})
	case mew.TerminalMouseScrollUp:
		return t.HandleMouseWheel(core.MouseWheelEvent{X: x, Y: y, ScreenX: x, ScreenY: y, DeltaY: -1, Modifiers: mods})
	case mew.TerminalMouseScrollDown:
		return t.HandleMouseWheel(core.MouseWheelEvent{X: x, Y: y, ScreenX: x, ScreenY: y, DeltaY: 1, Modifiers: mods})
	case mew.TerminalMouseScrollLeft:
		return t.HandleMouseWheel(core.MouseWheelEvent{X: x, Y: y, ScreenX: x, ScreenY: y, DeltaX: -1, Modifiers: mods})
	case mew.TerminalMouseScrollRight:
		return t.HandleMouseWheel(core.MouseWheelEvent{X: x, Y: y, ScreenX: x, ScreenY: y, DeltaX: 1, Modifiers: mods})
	}
	return false
}

// terminalKey answers what one KEY means to a session's terminal: the bytes
// its child would receive, or nil for a name that encodes to nothing.
//
// The encoder is the emulator's own — the same keyToBytes that has always
// turned key names into wire bytes for a PurfecTerm driving a real PTY — so a
// terminal inside mew and a terminal on its own agree about what F5 is. It is
// reached by feeding the name in and catching what comes out the input side,
// which is where those bytes have always gone.
//
// mew names its keys its own way (esc, back, fdel, pgup, return); the emulator
// speaks direct-key-handler's (Escape, Backspace, Delete, PageUp, Enter). The
// two vocabularies meet here, at the boundary, and nowhere else.
func (e *Editor) terminalKey(id string, key string) []byte {
	e.termMu.Lock()
	s := e.termSurfaces[id]
	if s == nil || s.term == nil || s.term.terminal == nil {
		e.termMu.Unlock()
		return nil
	}
	s.draining = true
	s.pending = nil
	t := s.term
	e.termMu.Unlock()

	// HandleKeyString drops input while the emulator believes itself
	// unfocused. The surface being keyed IS the focused one and Place has
	// normally said so already, but a keystroke's correctness should not
	// depend on a frame having gone by first.
	tm := t.terminal
	if !tm.IsFocused() {
		tm.SetFocused(true)
		defer tm.SetFocused(false)
	}
	tm.HandleKeyString(dkhKeyName(key))

	e.termMu.Lock()
	out := s.pending
	s.pending = nil
	s.draining = false
	e.termMu.Unlock()

	// The LAST hop, and the one the trace could not see. Every line above it
	// reports the editor's own emulator — the one mew talks through — while the
	// bytes a hosted child actually receives are encoded here, against that
	// child's separately negotiated flags. Those flags decide the SHAPE of a
	// key: without ReportAllKeys a shifted letter goes out as the bare byte "A"
	// and carries no modifier, while a key with no legacy form goes out as CSI
	// and carries one, which is exactly how shift can appear on some keys and
	// not others.
	if core.KeyTracing() {
		flags := -1
		if buf := tm.Buffer(); buf != nil {
			flags = buf.KeyboardFlags()
		}
		core.KeyTracef("5 child    id=%s key=%q flags=%d -> %q", id, key, flags, out)
	}
	return out
}

// mewToDKHKey renames mew's key vocabulary back to direct-key-handler's, which
// is what the emulator's encoder was written against. It is the inverse of
// mew's own normalizeKey; everything not listed (a character, ^C, F5) is
// already the same in both.
var mewToDKHKey = map[string]string{
	"esc":   "Escape",
	"space": "Space",
	"tab":   "Tab",
	// mew folds the keypad's Enter onto "return" (internal/input), so this
	// side only ever has the home-row key to name — and upstream calls that
	// "Return". It said "Enter" here, which is now the KEYPAD's name: harmless
	// while purfecterm encoded both as CR, and wrong the moment it stopped,
	// since mew's Enter would have started sending the keypad's SS3 M.
	"return": "Return",
	"back":   "Backspace",
	"up":     "Up",
	"down":   "Down",
	"left":   "Left",
	"right":  "Right",
	"home":   "Home",
	"end":    "End",
	"ins":    "Insert",
	// The erase trio, which the two vocabularies spell almost alike and mean
	// differently, so read the pairs rather than the words: mew's "del" is the
	// ASCII DEL character (its alias group is {"del", "^8"}) and upstream
	// calls that "Delete"; forward delete is "fdel" here and "FDel" there.
	// Taking "Delete" for forward delete — as this map did while upstream
	// still spelled it that way — erases the wrong side of the cursor in every
	// guest that tells the two apart, which is most of them.
	"del":  "Delete",
	"fdel": "FDel",
	"pgup": "PageUp",
	"pgdn": "PageDown",

	// The rest of the vocabulary, which differs only by case — mew spells its
	// names lowercase and upstream capitalises them. Case is still a difference:
	// this table is a lookup, so a missing entry means the key travels on as
	// mew spelled it and the emulator's encoder, which knows only upstream's
	// spelling, produces nothing for it.
	//
	// The lock and system keys were absent from the day this table was written;
	// the keypad's Begin and the keys an American keyboard does not have joined
	// them when the vocabulary grew.
	"capslock":    "CapsLock",
	"scrolllock":  "ScrollLock",
	"clear":       "Clear",
	"printscreen": "PrintScreen",
	"pause":       "Pause",
	"menu":        "Menu",
	"power":       "Power",
	"begin":       "Begin",
	"zig":         "Zig",
	"zag":         "Zag",
	"ro":          "Ro",
	"yen":         "Yen",
	"kanalock":    "KanaLock",
	"hangullock":  "HangulLock",
	"henkan":      "Henkan",
	"muhenkan":    "Muhenkan",
	"hanja":       "Hanja",
}

// keyEventSuffixes trail a name to say the event is something other than a
// plain press. They decorate the name; they are not part of it, so a rename has
// to put them back rather than fail to match around them.
var keyEventSuffixes = []string{":Release", ":Repeat"}

// dkhNamePrefixes are the prefixes dkhKeyName sets aside before looking a base
// name up, and puts back afterwards.
//
// Membership is the whole content of this list, and an absent prefix is not an
// error but a silence: the base never reaches the table, the key travels on
// under mew's own spelling, and the emulator's encoder — which knows only
// upstream's — quietly produces no bytes for it. Only S-, M- and C- were here,
// so "s-home", "m-pgup", "H-fdel", "G-€" and every keypad key were dropped on
// their way into a terminal viewport. The list is the canonical order:
// C- G- M- m- S- s- H- P- p- ^Key, with the caret left out because it is one
// character and needs no separating.
var dkhNamePrefixes = []string{"C-", "G-", "M-", "m-", "S-", "s-", "H-", "P-", "p-"}

// dkhKeyName renames one key, modifier prefixes and all ("S-tab" -> "S-Tab").
//
// An event suffix is set aside and restored: "up:Release" is the Up key, and
// without this it matched nothing in the table and travelled on under mew's own
// spelling, which the emulator's encoder does not know — so the release of
// every named key silently encoded to nothing.
func dkhKeyName(key string) string {
	for _, suffix := range keyEventSuffixes {
		if base, found := strings.CutSuffix(key, suffix); found {
			return dkhKeyName(base) + suffix
		}
	}
	prefix, base := "", key
	for {
		matched := false
		for _, p := range dkhNamePrefixes {
			if len(base) > len(p) && strings.HasPrefix(base, p) {
				prefix, base = prefix+p, base[len(p):]
				matched = true
				break
			}
		}
		if !matched {
			break
		}
	}
	if name, ok := mewToDKHKey[base]; ok {
		return prefix + name
	}
	return key
}

// termMouseButton maps mew's button to the toolkit's, so the one existing
// button table (purfMouseButton) stays the only place that knows purfecterm's
// report numbering.
func termMouseButton(b mew.TerminalMouseButton) core.MouseButton {
	switch b {
	case mew.TerminalMouseButtonLeft:
		return core.LeftButton
	case mew.TerminalMouseButtonMiddle:
		return core.MiddleButton
	case mew.TerminalMouseButtonRight:
		return core.RightButton
	}
	return core.NoButton
}

// terminalClose destroys a child once its session has ended.
func (e *Editor) terminalClose(id string) {
	e.termMu.Lock()
	delete(e.termSurfaces, id)
	e.termMu.Unlock()
	e.Update()
}

// paintTerminalSurfaces draws every visible child OVER this editor's own
// surface, offset to its rectangle and clipped to it. mew's coordinates are
// 1-based cells; the painter works in units, so each cell scales by the
// metrics.
func (e *Editor) paintTerminalSurfaces(p *core.Painter) {
	e.termMu.Lock()
	visible := make([]termSurface, 0, len(e.termSurfaces))
	for _, s := range e.termSurfaces {
		if s.term != nil && s.width > 0 && s.height > 0 && s.clipWidth > 0 && s.clipHeight > 0 {
			visible = append(visible, *s)
		}
	}
	e.termMu.Unlock()
	if len(visible) == 0 {
		return
	}
	// THIS editor's cell size, not the painter's. They are the same in a
	// terminal, where a cell is a cell, and they are not on a graphical build:
	// a terminal's grid derives from its own font's measured advance width and
	// line height, deliberately independent of the toolkit's cell
	// denomination. mew's columns are drawn at the former, so a rectangle
	// placed with the latter lands beside the text it is meant to cover —
	// by the difference times the column, which is why it shows as a fraction
	// of a cell at the gutter's edge and grows across the row.
	cw, ch := e.cellDims()
	cell := func(col, row int) (core.Unit, core.Unit) {
		return core.Unit(col-1) * cw, core.Unit(row-1) * ch
	}
	// A unit offset SNAPS to the host's cell grid on the way to pixels
	// (Painter.deviceAnchor -> the backend's cell-snapped UnitToPxX), and this
	// editor lays its own text out at the TERMINAL font's pitch instead —
	// pixel-precise, from its own origin: fillPixels rounds col*cw*ppu and
	// passes it as a pixel offset from unit (0,0). The two rates are not
	// multiples of each other, so placing a child by unit offset alone lands
	// its origin up to a fraction of a host cell away from the column it is
	// meant to cover, and the error is visible as a hairline seam that grows
	// across the row.
	//
	// So measure both and correct the difference in pixels: where the unit
	// offset actually lands (UnitSpanPxX, which asks the SAME snapping
	// question the transform will), against where this editor's own cell math
	// puts that column. When the two rates agree — an integer
	// pixels-per-unit, or a terminal font on the host's pitch — the residual
	// is zero and nothing moves.
	ppu := p.PxPerUnitF()
	residual := func(unitPos core.Unit, cells int, cellSize core.Unit) int {
		if ppu <= 0 {
			return 0
		}
		want := int(math.Round(float64(cells) * float64(cellSize) * ppu))
		return want - p.UnitSpanPxX(0, unitPos)
	}
	residualY := func(unitPos core.Unit, cells int, cellSize core.Unit) int {
		if ppu <= 0 {
			return 0
		}
		want := int(math.Round(float64(cells) * float64(cellSize) * ppu))
		return want - p.UnitSpanPxY(0, unitPos)
	}

	pxAtX := func(cells int) int { return int(math.Round(float64(cells) * float64(cw) * ppu)) }
	pxAtY := func(cells int) int { return int(math.Round(float64(cells) * float64(ch) * ppu)) }

	// paintRect draws one placement of a session's grid. primary=true SIZES the
	// grid (SetBounds resizes) and records the surface's hit-test frame;
	// primary=false is a MIRROR — the same grid drawn at another tile, bounded to
	// that tile's rectangle but WITHOUT resizing the grid (PaintMirror sets the
	// bounds under the mirror flag). A mirror smaller than the grid clips at its
	// own edge; larger, it letterboxes (the child paints nothing past its grid).
	//
	// The painter is offset to the GRID's origin so the terminal draws from its
	// own 0,0, clipped to the visible rectangle relative to that origin. The clip
	// has a two-rates problem on all four edges — it is expressed in units the
	// backend snaps to ITS cell grid, while the content runs at the terminal
	// pitch — so each edge is chosen for where it SNAPS: left/top at or before the
	// content's pixel edge, right/bottom at or after (out-rounding is safe; a
	// child paints nothing beyond its grid).
	paintRect := func(term *PurfecTerm, col, row, width, height, clipCol, clipRow, clipWidth, clipHeight int, primary bool) {
		x, y := cell(col, row)
		bnds := core.UnitRect{X: x, Y: y, Width: core.Unit(width) * cw, Height: core.Unit(height) * ch}
		if primary {
			term.SetBounds(bnds)
		}
		offXPx := residual(x, col-1, cw)
		offYPx := residualY(y, row-1, ch)
		leftU := snapCover(p.UnitSpanPxX, core.Unit(clipCol-1)*cw, pxAtX(clipCol-1)-1, ppu, false)
		rightU := snapCover(p.UnitSpanPxX, core.Unit(clipCol-1+clipWidth)*cw, pxAtX(clipCol-1+clipWidth)+1, ppu, true)
		topU := snapCover(p.UnitSpanPxY, core.Unit(clipRow-1)*ch, pxAtY(clipRow-1)-1, ppu, false)
		bottomU := snapCover(p.UnitSpanPxY, core.Unit(clipRow-1+clipHeight)*ch, pxAtY(clipRow-1+clipHeight)+1, ppu, true)
		if primary {
			// Only the primary records where the live terminal sits — mouse
			// hit-testing routes to it; a mirror is display-only.
			e.lastSurfaceOffsetX, e.lastSurfaceOffsetY = offXPx, offYPx
			e.lastClipFrame = core.UnitRect{X: leftU, Y: topU, Width: rightU - leftU, Height: bottomU - topU}
		}
		clip := core.UnitRect{X: leftU - x, Y: topU - y, Width: rightU - leftU, Height: bottomU - topU}
		pp := p.WithOffset(x, y).WithClip(clip).WithPixelOffset(offXPx, offYPx)
		if primary {
			term.Paint(pp)
		} else {
			// A mirror renders read-only, never claims the platform caret, and is
			// bounded to ITS OWN rectangle so a short pane stops at its edge
			// instead of running the (larger) primary grid past it.
			term.PaintMirror(pp, bnds)
		}
	}

	for _, s := range visible {
		paintRect(s.term, s.col, s.row, s.width, s.height, s.clipCol, s.clipRow, s.clipWidth, s.clipHeight, true)
		for _, m := range s.mirrors {
			if m.clipWidth <= 0 || m.clipHeight <= 0 {
				continue
			}
			paintRect(s.term, m.col, m.row, m.width, m.height, m.clipCol, m.clipRow, m.clipWidth, m.clipHeight, false)
		}
	}
	// A child settles its grid during that paint. Make sure the declaration
	// gets away even if the session's port was not up when it did.
	e.nudgeTermGrids()
}

// snapCover finds a clip edge: the unit position, near startU, whose SNAPPED
// device pixel covers targetPx — at or before it when up is false (a left or
// top edge), at or after it when up is true (a right or bottom edge). This is
// the whole trick of clipping content that runs at a different pitch than the
// unit grid snaps to: the edge is chosen for where it lands, not for what it
// is called.
//
// It converges on the shortfall rather than stepping by ones: after a zoom
// the two rates differ by several percent, so the gap scales with distance
// from the origin, and a wide surface can be dozens of pixels out. span is
// the painter's own unit-to-pixel measure, so this asks exactly the question
// the transform will answer. The loop bound is a runaway guard, not the
// mechanism.
func snapCover(span func(core.Unit, core.Unit) int, startU core.Unit, targetPx int, ppu float64, up bool) core.Unit {
	if ppu <= 0 {
		return startU
	}
	u := startU
	for i := 0; i < 64; i++ {
		have := span(0, u)
		var short int
		if up {
			short = targetPx - have // need snapped >= target
		} else {
			short = have - targetPx // need snapped <= target
		}
		if short <= 0 {
			return tighten(span, u, targetPx, up)
		}
		step := core.Unit(math.Ceil(float64(short) / math.Max(ppu, 1)))
		if step < 1 {
			step = 1
		}
		if up {
			u += step
		} else {
			u -= step
		}
	}
	return u
}

// tighten walks a covering edge back toward its natural position one unit at
// a time, keeping it covering — the convergence steps by the shortfall and
// can overshoot, and a clip edge should reveal as little of the neighbours
// as the snap quantization allows.
func tighten(span func(core.Unit, core.Unit) int, u core.Unit, targetPx int, up bool) core.Unit {
	for i := 0; i < 64; i++ {
		var next core.Unit
		if up {
			next = u - 1
			if span(0, next) < targetPx {
				return u
			}
		} else {
			next = u + 1
			if span(0, next) > targetPx {
				return u
			}
		}
		u = next
	}
	return u
}

// pixelWinsizer is the OPTIONAL other half of a session's Resize: the window
// measured in device PIXELS as well as cells. purfecterm's PTY grew it in
// v0.2.44; a session that predates it, or a Windows pseudoconsole (which has
// no pixel field at all), simply does not have the method and is left alone.
type pixelWinsizer interface {
	ResizeWithPixels(cols, rows, widthPx, heightPx int) error
}

// pxWinsizeSession fills the pixel half of every resize from the pane's
// MEASURED device extent, so ws_xpixel/ws_ypixel describe the same window
// ws_col/ws_row do.
type pxWinsizeSession struct {
	mew.PTYSession
	px       pixelWinsizer
	windowPx func() (int, int)
}

// Resize carries both measurements. It falls back to the cell-only call when
// the cell size is not known yet (before the first paint), rather than
// reporting a window of zero pixels - which is the very thing this exists to
// avoid.
func (s pxWinsizeSession) Resize(cols, rows int) error {
	if w, h := s.windowPx(); w > 0 && h > 0 {
		return s.px.ResizeWithPixels(cols, rows, w, h)
	}
	return s.PTYSession.Resize(cols, rows)
}

// WindowPx reports the pane's current pixel extent, so mew can tell whether a
// resize declaring the same CELLS is nevertheless a real change to the child's
// window. Same source as Resize uses, read at the same moment.
func (s pxWinsizeSession) WindowPx() (int, int) { return s.windowPx() }

// withPixelWinsize wraps sess so its resizes carry pixel dimensions, when the
// session can accept them. Returns sess untouched when it cannot.
func withPixelWinsize(sess mew.PTYSession, windowPx func() (int, int)) mew.PTYSession {
	if sess == nil || windowPx == nil {
		return sess
	}
	px, ok := sess.(pixelWinsizer)
	if !ok {
		return sess
	}
	return pxWinsizeSession{PTYSession: sess, px: px, windowPx: windowPx}
}

// childWindowPx is the child's window in DEVICE PIXELS, taken from the pane
// that paints it (PurfecTerm.ChildWindowPixels).
//
// Measured, never derived — see that method for why columns times the
// advertised cell is a different and larger quantity, and what the difference
// costs a program that draws pictures.
//
// Any hosted terminal answers: they all take one font from TerminalFont() and
// are laid out on one pitch. Zero before the first graphical paint, where the
// resize falls back to cells only rather than claiming a zero-pixel window.
func (e *Editor) childWindowPx() (int, int) {
	e.termMu.Lock()
	var t *PurfecTerm
	for _, s := range e.termSurfaces {
		if s != nil && s.term != nil {
			t = s.term
			break
		}
	}
	e.termMu.Unlock()
	if t == nil || t.Terminal() == nil {
		return 0, 0
	}
	// The pane's device pixels, and it must stay that: awrit (and anything
	// like it) makes an image of EXACTLY the size it is told, then lays out
	// size/DSF css pixels inside it. Reporting fewer pixels shrinks the
	// image, not the zoom -- measured: halving this halved the picture and
	// left its density untouched. Reporting MORE crops it at the pane edge.
	return t.ChildWindowPixels()
}
