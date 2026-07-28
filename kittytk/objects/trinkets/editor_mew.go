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
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/text"
	"github.com/phroun/mew"
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

	// readOnlyFocused mirrors mew's focused-window read-only state
	// (WithEditState), so CutEnabled greys the Edit menu's Cut while a
	// read-only buffer holds focus.
	readOnlyFocused atomic.Bool

	// helpOpen mirrors whether mew's built-in help window is open
	// (WithHelpState), so a host can keep a "Quick Help" checkmark in sync
	// (QuickHelpOpen).
	helpOpen atomic.Bool

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

	// hostedKids are the child terminals parented to this editor (see
	// AddChild): in the tree for parent-chain lookups, invisible to layout
	// and hit testing.
	hostedKids []core.Trinket

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
	return e
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
		// ...and how they are drawn: one real child PurfecTerm per session,
		// laid over the viewport's text area. PurfecTerm is the emulator, so
		// mew forwards raw bytes and never interprets them.
		mew.WithTerminalSurfaces(mew.TerminalHooks{
			Open:  e.terminalOpen,
			Feed:  e.terminalFeed,
			Place: e.terminalPlace,
			Close: e.terminalClose,
			Mouse: e.terminalMouse,
			Key:   e.terminalKey,
		}),
		// The system-clipboard bridge behind mew's os_copy/os_cut/os_paste
		// — the same desktop clipboard TextInput and the classic PurfecTerm
		// use, kept separate from mew's own kill ring. mew calls these on
		// its session goroutine; postUI marshals onto the desktop loop,
		// where SetClipboard and the (possibly async) ReadClipboardAsync
		// are safe. The paste delivery then marshals back into mew.
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
	if e.pointerInText(x, y) {
		return core.CursorText
	}
	return core.CursorDefault
}

// CursorShape is the position-less fallback (used only if the cursor descent
// cannot supply a point): a text editor surface defaults to the I-beam.
func (e *Editor) CursorShape() core.CursorShape {
	return core.CursorText
}

// pointerInText reports whether the local point falls on the focused mew
// window's editable text — inside the published I-beam rectangle and not on a
// link-button exclusion span. The pointer coordinate arrives in the same space
// as HandleMouseMove, so the terminal cell metrics map it to a 1-based cell.
func (e *Editor) pointerInText(x, y core.Unit) bool {
	cw, chh := e.cellDims()
	if cw <= 0 || chh <= 0 {
		return false
	}
	col := int(x/cw) + 1 // 1-based cell, matching mew's coordinates
	row := int(y/chh) + 1

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

	env := append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	// How a machine makes a terminal is the one part of this that is not the
	// same everywhere: a pty pair on POSIX, a pseudoconsole bound to the child
	// before it starts on Windows. hostPTY is per-platform; everything above
	// it, and mew entirely, is not.
	return hostPTY(path, dir, env, req.Cols, req.Rows, req.Method)
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
	col, row              int
	width, height         int
	clipCol, clipRow      int
	clipWidth, clipHeight int
	// focused: this surface owns the platform caret this frame.
	focused bool
}

// terminalOpen creates the child for a new session. Called on mew's main loop.
func (e *Editor) terminalOpen(id string, cols, rows int) {
	// The grid size follows SetBounds (updateTerminalSize divides the bounds
	// by the cell size), so there is nothing to set here: cols/rows arrive
	// again with the first Place.
	t := NewPurfecTerm()
	t.Init(t)
	t.SetEditorMode(false)
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
	}
	for _, want := range surfaces {
		s := e.termSurfaces[want.ID]
		if s == nil {
			continue
		}
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
	e.termMu.Lock()
	s := e.termSurfaces[id]
	if s == nil || s.term == nil {
		e.termMu.Unlock()
		return nil, false
	}
	s.draining = true
	s.pending = nil
	t := s.term
	e.termMu.Unlock()

	handled := dispatchHostedMouse(t, ev)

	e.termMu.Lock()
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
		go func() {
			for cmd := range e.termOut {
				if p := e.port; p != nil {
					p.Execute(cmd)
				}
			}
		}()
	})
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
func dispatchHostedMouse(t *PurfecTerm, ev mew.TerminalMouse) bool {
	cw, ch := t.cellDims()
	x := core.Unit(ev.Col-1)*cw + cw/2
	y := core.Unit(ev.Row-1)*ch + ch/2
	btn := termMouseButton(ev.Button)
	var mods core.KeyModifiers
	if ev.Shift {
		mods |= core.ShiftModifier
	}
	if ev.Alt {
		mods |= core.AltModifier
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
	return out
}

// mewToDKHKey renames mew's key vocabulary back to direct-key-handler's, which
// is what the emulator's encoder was written against. It is the inverse of
// mew's own normalizeKey; everything not listed (a character, ^C, F5) is
// already the same in both.
var mewToDKHKey = map[string]string{
	"esc":    "Escape",
	"space":  "Space",
	"tab":    "Tab",
	"return": "Enter",
	"back":   "Backspace",
	"up":     "Up",
	"down":   "Down",
	"left":   "Left",
	"right":  "Right",
	"home":   "Home",
	"end":    "End",
	"ins":    "Insert",
	"fdel":   "Delete",
	"del":    "Delete",
	"pgup":   "PageUp",
	"pgdn":   "PageDown",
}

// dkhKeyName renames one key, modifier prefixes and all ("S-tab" -> "S-Tab").
func dkhKeyName(key string) string {
	prefix, base := "", key
	for {
		if len(base) > 2 && (strings.HasPrefix(base, "S-") ||
			strings.HasPrefix(base, "M-") || strings.HasPrefix(base, "C-")) {
			prefix, base = prefix+base[:2], base[2:]
			continue
		}
		break
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
	for _, s := range visible {
		x, y := cell(s.col, s.row)
		w := core.Unit(s.width) * cw
		h := core.Unit(s.height) * ch
		s.term.SetBounds(core.UnitRect{X: x, Y: y, Width: w, Height: h})

		// The painter is offset to the GRID's origin, so the terminal draws
		// from its own 0,0 — but clipped to the visible rectangle expressed
		// relative to that origin. When the two rectangles match (the ordinary
		// case) this is just a clip to the surface's own bounds.
		cx, cy := cell(s.clipCol, s.clipRow)
		clip := core.UnitRect{
			X:      cx - x,
			Y:      cy - y,
			Width:  core.Unit(s.clipWidth) * cw,
			Height: core.Unit(s.clipHeight) * ch,
		}
		s.term.Paint(p.WithOffset(x, y).WithClip(clip))
	}
}
