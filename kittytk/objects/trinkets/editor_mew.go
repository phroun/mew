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

	// pointerOverButton mirrors mew's pointer affordance (WithPointerShape):
	// true while the pointer is over a link button or one is captured, so
	// the cursor query below answers with the arrow instead of the I-beam.
	pointerOverButton atomic.Bool

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

// Paint starts the session on the first paint (once properties are applied),
// then delegates to the terminal surface.
func (e *Editor) Paint(p *core.Painter) {
	e.ensureStarted()
	if e.PurfecTerm != nil {
		e.PurfecTerm.Paint(p)
	}
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
		// The mouse-pointer affordance: an arrow over link buttons (and
		// while one is captured), the I-beam otherwise. See CursorShapeAt.
		mew.WithPointerShape(func(over bool) { e.pointerOverButton.Store(over) }),
		// Since purfecterm v0.2.23 the embedded terminal speaks the STANDARD
		// visual-column contract by default (its flex mode moved to ?7027,
		// opt-in), so mew talks to it exactly as to any terminal — no
		// WithFlexTerminal, no logical-column translation.

		// Live font swaps (set_font "ui-term", "JetBrainsMono"): re-point the
		// alias in the shared text engine — loading the font on demand — and
		// repaint. The engine's epoch bump flushes the terminal's glyph caches
		// on the next paint. No-op in the pure-TUI path (no shared engine).
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
// CursorShape / CursorShapeAt override the embedded terminal's I-beam with
// the arrow pointer while mew reports the pointer over a link button (or a
// captured one) — the same affordance the classic PurfecTerm's scrollbar
// lanes provide, driven here by mew's own hit knowledge.
func (e *Editor) CursorShape() core.CursorShape {
	if e.pointerOverButton.Load() {
		return core.CursorDefault
	}
	return e.PurfecTerm.CursorShape()
}

func (e *Editor) CursorShapeAt(x, y core.Unit) core.CursorShape {
	if e.pointerOverButton.Load() {
		return core.CursorDefault
	}
	return e.PurfecTerm.CursorShapeAt(x, y)
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
// block CAN be cut.
func (e *Editor) CutEnabled() bool { return true }

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
