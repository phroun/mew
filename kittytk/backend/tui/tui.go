// Package backend provides rendering backends for KittyTK.
package tui

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/phroun/direct-key-handler/keyboard"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/hebrew"
	"github.com/phroun/kittytk/style"
	"github.com/phroun/purfecterm"
	"golang.org/x/term"
)

// Cell represents a single character cell on the terminal.
//
// Width model: the buffer is a VISUAL grid — an East-Asian wide glyph occupies
// its base cell plus a continuation cell (Char 0) to its right, exactly the
// columns the terminal will advance. Zero-width combining marks ride the base
// cell's Combining string, never their own cell, so grapheme clusters stay
// intact in the emitted stream.
//
// DEC double-width/height lines (DECDWL/DECDHL): a hosted terminal's
// double-width row is stored as visual-column GROUPS — the glyph carrier cell
// (DWLMode set, DWLFill false: the "left half") followed by filler space cells
// (DWLFill true: the "right half"; a wide glyph carries one continuation plus
// two fillers, 2x its width in all). EndFrame decides per row: when EVERY cell
// on a terminal row belongs to the same DWL mode, the row is emitted as a real
// DEC line (ESC#6/#3/#4) using only the carrier glyphs; a mixed row (other
// windows sharing it) emits the cells literally instead — the same content
// D O U B L E  S P A C E D, every glyph still in its correct column.
type Cell struct {
	Char      rune
	Combining string // zero-width marks attached to Char ("" for none)
	Style     style.CellStyle
	DWLMode   byte // 0 = normal; else the DEC line selector: '6' DWL, '3'/'4' DHL halves
	DWLFill   bool // filler ("right half") cell of a DWL group; never a carrier
}

// invalidCell is a front-buffer sentinel that compares unequal to every real
// cell, forcing re-emission (used when a row leaves DEC double-width mode).
var invalidCell = Cell{Char: utf8.MaxRune + 1}

// cellRuneWidth returns the terminal columns a base rune occupies (1 or 2),
// from purfecterm's East Asian Width table — the same width authority the
// PurfecTerm trinket's grid uses, so layout and emission agree end to end.
// Ambiguous-width runes count as narrow (purfecterm's default).
//
// Non-spacing marks are zero-width: they belong in Cell.Combining, not in a
// cell of their own. Spacing marks (category Mc — the visible Devanagari
// matras and kin) do take a cell, and purfecterm's predicate distinguishes
// the two as of v0.2.29. Before that it was wrong in both directions, and
// KittyTK carried its own category test to compensate; see
// docs/upstream/purfecterm-combining-marks.md.
func cellRuneWidth(r rune) int {
	if r == 0 {
		return 1
	}
	if purfecterm.IsCombiningMark(r) {
		return 0
	}
	if w := purfecterm.GetEastAsianWidth(r); w >= 1.5 {
		return 2
	}
	return 1
}

// TUIBackend implements RenderBackend for terminal rendering.
type TUIBackend struct {
	mu sync.Mutex

	// Terminal state
	fd   int
	cols int
	rows int

	// Cell metrics for unit conversion
	metrics core.CellMetrics

	// The palette wait: how long a held text key's repeats are withheld, which
	// key is being held, and when the hold was first seen. See
	// TUIOptions.HoldOpensPalette.
	//
	// holdArm names the key whose letter is still in the document with a
	// palette possibly open over it, and holdExtra counts what has been typed
	// into that palette since. Together they are what a commit takes back.
	//
	// holdArm outlives holdKey on purpose: letting go is how the palette gets
	// used, so the release cannot be what ends this. See takeBackHeld.
	holdWait  time.Duration
	holdKey   string
	holdSince time.Time
	holdArm   string
	holdExtra int

	// Screen buffers (double buffering)
	// dmgMin/dmgMax are the column range the text diff rewrote on each row
	// this frame, -1 when the row was untouched. Images need it: sixel pixels
	// are screen content, so a picture survives until text is painted over
	// THOSE cells - a change on an unrelated row is not a reason to re-send it.
	dmgMin, dmgMax []int

	// cellSeq is the paint ORDER of the last write to each cell this frame,
	// and paintSeq the counter it comes from. Trinkets paint back to front, so
	// a higher number is content drawn LATER and therefore on top. An image is
	// queued rather than composited, so this is how the flush finds out which
	// of its cells a window painted over afterwards.
	cellSeq  [][]uint32
	paintSeq uint32

	// motionWanted is set during a frame by anything that needs pointer
	// motion; motionOn is whether the outer terminal is currently sending it
	// (?1003). Free motion is not enabled by default because it puts a report
	// on the wire for every pixel the pointer crosses — it is turned on only
	// while something is actually watching. See RequestMotionTracking.
	motionWanted bool
	motionOn     bool

	// What the "kitty" graphics protocol currently has on screen. kittyBaseIDs is
	// one id per block, the full picture; kittyPatchIDs are the deltas layered
	// over them since the last full send. kittyNextID only ever counts up, so a
	// new placement can never collide with one being deleted in the same frame.
	// kittyPatchArea is the pixel area patched since the last full send, which is
	// what decides when patching has stopped being cheaper than starting over.
	kittyBaseIDs   []uint32
	kittyPatchIDs  []uint32
	kittyPatchArea int
	kittyNextID    uint32

	frontBuffer [][]Cell
	backBuffer  [][]Cell

	// frontLineAttr tracks the DEC line mode the terminal currently shows per
	// row (0 = normal; '6'/'3'/'4' = the DECDWL/DECDHL selector last emitted),
	// so a row entering or leaving double-width re-emits the mode escape and
	// forces a full repaint of that row.
	frontLineAttr []byte

	// Current state
	currentStyle style.CellStyle
	clipRect     core.UnitRect
	cursorX      int
	cursorY      int
	// cursorVisible is what the APP asked for; cursorShown is what the
	// TERMINAL was last told. They diverge across a present: the hardware
	// cursor is hidden for the repaint and shown again only once EndFrame has
	// put it back at its settled position.
	cursorVisible bool
	cursorShown   bool
	// cursorStyle is the DECSCUSR shape the focused trinket asked for, and
	// cursorStyleSent what the terminal was last told, so an unchanged shape
	// stays off the wire. Emitted only while the cursor is visible.
	cursorStyle     int
	cursorStyleSent int

	// Input handling
	keyboard   *keyboard.Handler
	eventQueue chan core.Event
	stopChan   chan struct{}

	// Mouse state (for tracking position between Mouse@x,y and action events).
	// These hold the RAW 1-based coordinate the terminal reported — a cell
	// column in the default SGR mode, or an outer pixel when pixelMouse is on
	// (outerToUnits* does the mode-dependent conversion).
	pendingMouseX int
	pendingMouseY int

	// Outer-terminal pixel mouse (SGR-Pixels, ?1016). When the real terminal
	// answers the startup probe — DECRQM says ?1016 is recognized AND CSI 16 t
	// reports a cell pixel size — the backend enables ?1016 on it and reads
	// mouse reports as PIXELS, so a click carries the sub-cell position mew's
	// nearest-edge caret wants (the same sub-cell Unit the SDL host forwards).
	// A terminal that ignores either probe simply keeps cell resolution, so
	// this is a pure enhancement that degrades to today's behavior.
	pixelMouse      bool // ?1016 enabled on the outer terminal; reports are pixels
	outerCellW      int  // outer terminal cell width in pixels (from CSI 16 t)
	outerCellH      int  // outer terminal cell height in pixels
	outerPixelOK    bool // DECRQM: ?1016 is recognized (settable)
	outerCellSizeOK bool // CSI 16 t gave a usable cell pixel size

	// Outer-terminal graphics (see graphics.go). The startup probe asks the
	// real terminal whether it can draw a picture and in which protocol; a
	// terminal that answers neither query falls back to what the environment
	// says. Images the paint pass asks for are collected here and emitted
	// after the text diff, since the screen is written as one flush.
	graphics         int // Graphics{None,Kitty,Sixel}
	graphicsAnswered bool
	pendingImages    []placedImage
	// shownImages is what the last flush actually put on screen, so an
	// unchanged frame can be skipped rather than re-transmitted (see
	// flushImagesLocked). Compared by value, which is why a queued image must
	// be one nobody else will overwrite.
	shownImages []placedImage
	// hadImages says the last frame placed images, which "kitty" graphics
	// needs deleted before the next set.
	hadImages bool

	// Output writer
	output io.Writer

	// ttyOut, when set, receives the terminal MODE escapes instead of a freshly
	// opened /dev/tty (see writeTTY). Tests set it so their result does not
	// depend on whether the runner happens to have a controlling terminal.
	ttyOut io.Writer

	// restored guards the terminal-mode restore so it runs exactly once no
	// matter how many paths reach it - Shutdown, RestoreTerminal, an embedder's
	// emergency handler, or all three.
	restored sync.Once
	// shutdown guards the rest of Shutdown. Without it a second call closes
	// stopChan twice and panics, which is a poor way to end an emergency.
	shutdownOnce sync.Once

	// Capabilities
	colorDepth int
	hasMouse   bool
	hasUnicode bool

	// Flag to clear lines on next render (after resize)
	needsLineClear bool

	// clipboard is the host's internal clipboard - the fallback Paste source
	// when OSC 52 read-back is off or the terminal doesn't answer. Copy/Cut
	// mirror it to the terminal's clipboard via OSC 52 when osc52 is set.
	clipboard string
	osc52     bool

	// osc52Paste enables OSC 52 read-back: a clipboard read queries the
	// terminal (RequestClipboardRead) and the reply arrives asynchronously via
	// the keyboard handler's OnClipboard callback. onClipboardRead is the
	// registered sink for that reply (see SetClipboardReadHandler); the desktop
	// wires it to drive a "waiting for clipboard" modal so the event loop is
	// never blocked while the terminal prompts the user.
	osc52Paste      bool
	onClipboardRead func(string)
}

// TUIOptions configures the TUI backend.
// DefaultHoldOpensPalette is how long a held text key's repeats are withheld
// by default, so a press-and-hold accent palette can be reached.
//
// Long enough to let go in, which is the whole requirement: the palette opens
// on the hold and the person then has to release the key to use it. Shorter
// than a deliberate hold-to-repeat, which is the other thing a held key means
// and the reason this is not simply "drop every repeat".
const DefaultHoldOpensPalette = time.Second

type TUIOptions struct {
	// Output is where to write terminal output (default: os.Stdout)
	Output io.Writer

	// Input is where to read input from (default: os.Stdin)
	Input io.Reader

	// CellMetrics defines unit-to-cell mapping (default: 8x16)
	CellMetrics core.CellMetrics

	// ColorDepth: 2, 16, 256, or 16777216 (default: auto-detect)
	ColorDepth int

	// EnableMouse enables mouse input (default: true)
	EnableMouse bool

	// HoldOpensPalette is how long a held TEXT key's repeats are withheld, so
	// that a press-and-hold accent palette can be used.
	//
	// macOS opens its palette on the hold, and the terminal goes on sending
	// repeats from behind it — so the letter types itself over and over while
	// the palette is up, and there is no moment early enough to let go in. The
	// palette is not merely untidy then, it is unusable. Nothing in the
	// terminal's key stream says a palette is open (the protocol has no way to
	// report a composition), so the only thing that tells "held to open the
	// palette" from "held to type twenty of these" is HOW LONG.
	//
	// Zero uses DefaultHoldOpensPalette. Negative disables the wait, which is
	// what a host on a system with no such palette wants.
	HoldOpensPalette time.Duration

	// AlternateScreen uses the alternate screen buffer (default: true)
	AlternateScreen bool

	// OSC52Clipboard mirrors Copy/Cut to the terminal's clipboard with the OSC 52
	// escape sequence (supported by iTerm2, xterm, the kitty terminal, wezterm,
	// tmux with set-clipboard, ...). When false the host uses its own internal
	// clipboard only. Default: true.
	OSC52Clipboard bool

	// OSC52Paste enables OSC 52 clipboard read-back for Paste: query the
	// terminal for its clipboard and use the reply, falling back to the
	// internal clipboard when the terminal doesn't answer (many disable read
	// for security). Off by default; implies OSC52Clipboard for the query.
	OSC52Paste bool
}

// DefaultTUIOptions returns default options.
func DefaultTUIOptions() TUIOptions {
	return TUIOptions{
		Output:          os.Stdout,
		Input:           os.Stdin,
		CellMetrics:     core.DefaultCellMetrics(),
		ColorDepth:      0, // Auto-detect
		EnableMouse:     true,
		AlternateScreen: true,
		OSC52Clipboard:  true,
	}
}

// NewTUIBackend creates a new terminal backend.
func NewTUIBackend(opts TUIOptions) *TUIBackend {
	if opts.Output == nil {
		opts.Output = os.Stdout
	}
	if opts.Input == nil {
		opts.Input = os.Stdin
	}
	if opts.HoldOpensPalette == 0 {
		opts.HoldOpensPalette = DefaultHoldOpensPalette
	}
	if opts.CellMetrics.CellWidth == 0 {
		opts.CellMetrics = core.DefaultCellMetrics()
	}

	t := &TUIBackend{
		metrics:    opts.CellMetrics,
		output:     opts.Output,
		eventQueue: make(chan core.Event, 256),
		stopChan:   make(chan struct{}),
		colorDepth: opts.ColorDepth,
		hasMouse:   opts.EnableMouse,
		holdWait:   opts.HoldOpensPalette,
		hasUnicode: true, // Assume Unicode support
		osc52:      opts.OSC52Clipboard,
		osc52Paste: opts.OSC52Paste,
	}
	return t
}

// enterTerminalModes turns on every outer-terminal mode this backend runs
// under. RestoreTerminal turns them off again in the mirror order, and the two
// are a pair: a mode enabled here that is not disabled there outlives the
// process and lands on the user's shell.
//
// Split out of Init so the ORDER can be tested. Init cannot be: it opens the
// controlling terminal and starts a keyboard reader on os.Stdin, neither of
// which a test has.
func (t *TUIBackend) enterTerminalModes() {
	// Enable mouse if requested
	if t.hasMouse {
		t.writeTTY("\033[?1000h\033[?1002h\033[?1006h")
	}

	// Enter alternate screen
	t.writeTTY("\033[?1049h")

	// Enable the "kitty" keyboard protocol for better key detection.
	//
	// Flag 1 is disambiguation; flag 2 is event reporting, which is what makes
	// the outer terminal send key RELEASE and repeat at all. Asking for 1 alone
	// meant no release ever arrived here, so a hosted child that wanted them —
	// a browser tracking a held key — could not be given what the terminal was
	// never asked to send.
	//
	// AFTER the switch to the alternate screen, because the flag stack is
	// per-screen and this is the screen the application runs on. Pushed before
	// the switch it landed on the MAIN screen's stack, where nothing reads it:
	// the outer terminal went on sending legacy keys for the whole session, so
	// no release arrived however loudly this asked for one — and the push
	// outlived us on the screen the shell came back to, which is how the first
	// Ctrl+C after an exit printed "...9;5u" instead of interrupting.
	// Disambiguate (1) + ReportEvents (2) + ReportAllKeys (8).
	//
	// ReportAllKeys is what makes a keypad key arrive AS a keypad key. Without
	// it a terminal sends any key that produces text as that text and nothing
	// else, so the pad's 7 goes down as the byte "7" — indistinguishable from
	// the main row's, carrying no identity at all — while its repeats and its
	// release, which have no legacy form and must go as CSI u, carry keycode
	// 57406 and name the pad. One key, reported as two different keys, with a
	// release for a press nobody made.
	//
	// Disambiguate alone does not close this: it promotes the pad keys that
	// produce NO text, which is why P-Enter, P-Home and the pad arrows were
	// always right and only the locked pad was wrong. There is no narrower lever.
	// The application-keypad mode that would have been keypad-only is parsed and
	// discarded by the kitty terminal, the protocol's own reference
	// implementation (screen_alternate_keypad_mode, its handler for the mode,
	// is an empty function), so this flag is the whole of the mechanism.
	//
	// Disambiguation costs the text: with the keys reported as escape codes, a
	// text key arrives as its KEYCODE and nothing says what it produced. A
	// key's name is its character for most of them, so that passed unnoticed
	// until the two differed — and a dead key is where they do. Option+i then
	// "u" composes "û" and reports the U KEY, so a plain "u" went into the
	// document while the same keystroke worked in a host that never asked for
	// disambiguation at all.
	//
	// So flag 16 comes with it (11 + 16 = 27): report the associated text. The
	// key layer reads it as what the key TYPED in preference to what the key is
	// CALLED, and reports the protocol's keycode 0 — text the terminal received
	// with no key behind it, which is what an input method commits — prefixed,
	// as text rather than as a keystroke that never happened (see handleKey).
	//
	// Needs direct-key-handler v0.3.34 or newer. Before that the third field
	// was ignored and keycode 0 read as keycode 1, which is the phantom
	// keystroke this flag exists to avoid.
	t.writeTTY("\033[>27u")

	// Enable focus reporting. The outer terminal then sends CSI I and CSI O as
	// it gains and loses focus, which is the only way this process can learn
	// that the keyboard has gone elsewhere.
	//
	// It matters because of what happens to a key held across that moment: its
	// key-up is delivered to whoever has the keyboard now and never arrives
	// here, so without this notification the press would stand for good in
	// anything tracking held keys. direct-key-handler releases them on the
	// report; enabling the mode is what makes the report come.
	t.writeTTY("\033[?1004h")

	// Enable bracketed paste. Without this the outer terminal ships a paste as
	// a raw byte flood — indistinguishable from very fast typing — which
	// direct-key-handler then surfaces one key at a time, overrunning the event
	// queue and dropping characters on a large paste. With it on, a paste
	// arrives framed (\x1b[200~ … \x1b[201~) and is delivered whole via OnPaste.
	t.writeTTY("\033[?2004h")

	// Hide cursor initially
	t.writeTTY("\033[?25l")
}

// Init initializes the terminal backend.
func (t *TUIBackend) Init() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Get terminal file descriptor
	if f, ok := t.output.(*os.File); ok {
		t.fd = int(f.Fd())
	} else {
		t.fd = -1
	}

	// Get terminal size
	if t.fd >= 0 && term.IsTerminal(t.fd) {
		cols, rows, err := term.GetSize(t.fd)
		if err != nil {
			return fmt.Errorf("failed to get terminal size: %w", err)
		}
		t.cols = cols
		t.rows = rows
	} else {
		// Default size for non-terminal output
		t.cols = 80
		t.rows = 24
	}

	// Auto-detect color depth
	if t.colorDepth == 0 {
		t.colorDepth = detectColorDepth()
	}

	// Allocate buffers
	t.allocateBuffers()

	// Assert a known single-width baseline on the first frame. frontLineAttr was
	// just allocated as all-zeros ("every row is normal"), but entering the
	// alternate screen below does NOT reset DEC line attributes: DECDWL/DECDHL
	// survive a screen switch and an erase — only DECSWL (ESC#5), RIS, or a soft
	// reset retires them. So a row a PREVIOUS session left doubled comes back
	// doubled, while our fresh record says "normal" and the reversion path (which
	// fires only on a non-zero record) never rescues it — the row stays double-
	// width every launch. Arming the line clear makes EndFrame emit DECSWL for
	// every row on the first present, exactly as it does after a resize.
	t.needsLineClear = true

	t.enterTerminalModes()

	// The terminal is now ours. Join the set RestoreAll walks, so an exit path
	// that never reaches Shutdown can still hand it back.
	registerLive(t)

	// Set up keyboard handler AFTER terminal modes are configured. Take paste
	// through OnPaste as one batched event, and turn OFF the per-character key
	// echo (EmitPasteKeys): a paste is not typing, and re-emitting it as keys
	// is exactly what overran the event queue.
	noPasteKeys := false
	kbOpts := keyboard.Options{
		InputReader:   os.Stdin,
		EmitPasteKeys: &noPasteKeys,
	}
	t.keyboard = keyboard.New(kbOpts)
	t.keyboard.OnKey = t.handleKey
	t.keyboard.OnPaste = func(content []byte) {
		t.deliverPaste(string(content))
	}
	// The outer terminal's focus, surfaced as the toolkit's own focus event so
	// a trinket cannot tell which backend it is running under. The graphical
	// host has always sent these; this one had no way to know, so an
	// application hosted in a terminal never learned the window had been left.
	//
	// The keys held at that moment have already been released by the handler
	// before this runs — their key-ups go to whoever has the keyboard now and
	// never arrive here — so anything reacting to the focus change sees a
	// keyboard with nothing down, which is the truth.
	t.keyboard.OnFocus = func(focused bool) {
		select {
		case t.eventQueue <- core.FocusEvent{Focused: focused}:
		default:
			// Queue full, drop event
		}
	}
	// Keyboard states — the pad's lock, Caps Lock, focus — as toolkit events,
	// so a trinket drawing an indicator repaints when one moves. The states
	// themselves are read through Modes(); this is only the nudge.
	t.keyboard.OnMode = func(m keyboard.Mode) {
		select {
		case t.eventQueue <- core.ModeEvent{Mode: core.Mode{Name: m.Name, Value: m.Value}}:
		default:
			// Queue full, drop event
		}
	}
	if t.osc52Paste {
		// OSC 52 clipboard responses (replies to our read query) are delivered
		// here, not as keystrokes: keep the internal copy in sync and notify the
		// registered reader (the desktop resolves the pending paste).
		t.keyboard.OnClipboard = func(_ byte, data []byte) {
			t.deliverClipboard(string(data))
		}
	}

	// Now start the keyboard handler
	if err := t.keyboard.Start(); err != nil {
		return fmt.Errorf("failed to start keyboard handler: %w", err)
	}

	// Probe the outer terminal for pixel-precise mouse (SGR-Pixels, ?1016).
	// The replies are asynchronous — they arrive as DECRPM:/WinOp: keys once
	// the keyboard reader is running — so this must come AFTER Start(). A
	// terminal that answers both (recognizes ?1016 and reports a cell pixel
	// size) gets ?1016 enabled by maybeEnablePixelMouse; one that ignores
	// either query stays on cell coordinates. See handleDECRPM/handleWinOp.
	if t.hasMouse {
		t.writeTTY("\033[?1016$p") // DECRQM: is SGR-Pixels mode recognized?
		t.writeTTY("\033[16t")     // XTWINOPS: report the cell size in pixels
	}

	// Ask the same terminal whether it can draw a PICTURE, and in which
	// protocol (see graphics.go). Asynchronous like the probes above: the
	// answers arrive as APC:/DA1: keys. A terminal that ignores both is
	// settled from the environment once the window has passed, so a
	// multiplexer swallowing the replies still gets an answer.
	t.probeGraphics()
	go func() {
		time.Sleep(250 * time.Millisecond)
		t.resolveGraphicsFallback()
	}()

	// Handle terminal resize
	go t.handleResize()

	return nil
}

// Shutdown cleans up the terminal backend. Safe to call more than once.
func (t *TUIBackend) Shutdown() {
	t.shutdownOnce.Do(func() {
		t.mu.Lock()
		close(t.stopChan)
		kb := t.keyboard
		t.mu.Unlock()

		// Outside the lock: Stop restores raw mode and joins the reader
		// goroutine, and nothing it touches needs t.mu.
		if kb != nil {
			kb.Stop()
		}
	})
	t.RestoreTerminal()
}

// RestoreTerminal puts the terminal back the way it was found - mouse off,
// cursor shown, alternate screen left, "kitty" keyboard protocol popped,
// colours reset - and does so at most once however many paths reach it.
//
// It is separate from Shutdown, and exported, because the terminal is PROCESS
// state, not backend state: whoever ends the process is responsible for it,
// and that is not always the code holding this backend. An embedder whose
// fatal-signal path bypasses the normal teardown (mew dumps unsaved buffers
// and calls os.Exit) must be able to hand the terminal back without owning a
// reference here - see RestoreAll.
//
// Safe from any goroutine, including a signal handler: it takes no lock the
// event loop holds and does nothing but write escapes.
func (t *TUIBackend) RestoreTerminal() {
	t.restored.Do(func() {
		// Disable mouse. ?1016l first (harmless if it was never enabled) so the
		// outer terminal drops back to cell reports before the rest go off.
		if t.hasMouse {
			t.writeTTY("\033[?1016l\033[?1006l\033[?1003l\033[?1002l\033[?1000l")
		}

		// Show cursor
		t.writeTTY("\033[?25h")
		t.cursorShown = true

		// Disable bracketed paste (harmless if the terminal never enabled it).
		t.writeTTY("\033[?2004l")

		// Pop the "kitty" keyboard protocol - BEFORE leaving the alternate
		// screen, because the flag stack is per-screen and the alternate
		// screen's is the one Init pushed onto. Popping after the switch back
		// would pop the MAIN screen's stack, which we never pushed, and leave
		// our own push standing on the screen we just left.
		//
		// Popping an empty stack is a no-op, so this stays safe on a terminal
		// that ignored the push. The explicit reset after it covers a terminal
		// that honours the flags but not the stack.
		t.writeTTY("\033[<u")
		t.writeTTY("\033[=0;1u")

		// Focus reporting off. Left on, the shell that inherits this terminal
		// is sent CSI I and CSI O on every alt-tab and types them as text.
		t.writeTTY("\033[?1004l")

		// Leave alternate screen
		t.writeTTY("\033[?1049l")

		// Reset colors
		t.writeTTY("\033[0m")

		unregisterLive(t)
	})
}

// allocateBuffers creates the screen buffers.
func (t *TUIBackend) allocateBuffers() {
	t.frontBuffer = make([][]Cell, t.rows)
	t.backBuffer = make([][]Cell, t.rows)
	t.dmgMin = make([]int, t.rows)
	t.dmgMax = make([]int, t.rows)
	t.cellSeq = make([][]uint32, t.rows)
	for y := range t.cellSeq {
		t.cellSeq[y] = make([]uint32, t.cols)
	}
	t.frontLineAttr = make([]byte, t.rows)

	defaultCell := Cell{Char: ' ', Style: style.DefaultStyle()}

	for y := 0; y < t.rows; y++ {
		t.frontBuffer[y] = make([]Cell, t.cols)
		t.backBuffer[y] = make([]Cell, t.cols)
		for x := 0; x < t.cols; x++ {
			t.frontBuffer[y][x] = defaultCell
			t.backBuffer[y][x] = defaultCell
		}
	}
}

// Size returns the current size in abstract units.
func (t *TUIBackend) Size() core.UnitSize {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.metrics.CellsToUnits(t.cols, t.rows)
}

// Metrics returns the cell metrics.
func (t *TUIBackend) Metrics() core.CellMetrics {
	return t.metrics
}

// BeginFrame starts a new frame for rendering.
func (t *TUIBackend) BeginFrame() {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Clear back buffer
	defaultCell := Cell{Char: ' ', Style: style.DefaultStyle()}
	t.paintSeq = 0
	// A request lasts one frame: whoever still needs motion asks again.
	t.motionWanted = false
	for y := 0; y < t.rows; y++ {
		for x := 0; x < t.cols; x++ {
			t.backBuffer[y][x] = defaultCell
			t.cellSeq[y][x] = 0
		}
	}

	// Reset clip
	t.clipRect = core.UnitRect{
		Width:  t.metrics.CellToUnitsX(t.cols),
		Height: t.metrics.CellToUnitsY(t.rows),
	}
}

// rowUniformDWL reports the DEC line selector when EVERY cell on the back
// buffer row belongs to the same double-width group mode; 0 for a normal or
// mixed row (which must render literally, double-spaced).
func (t *TUIBackend) rowUniformDWL(y int) byte {
	mode := byte(0)
	for x := 0; x < t.cols; x++ {
		m := t.backBuffer[y][x].DWLMode
		if m == 0 {
			return 0
		}
		if mode == 0 {
			mode = m
		} else if m != mode {
			return 0
		}
	}
	return mode
}

// EndFrame completes the frame and presents it: a minimal diff against what
// the terminal shows. The emitted stream is width-honest — a wide glyph's
// continuation cell is never addressed or overwritten, SGR is emitted only
// when the pen changes (so same-styled runs stay contiguous and the
// terminal's Arabic shaping/grapheme joining is preserved), and the cursor is
// re-addressed only when it is not already in position. Rows fully owned by a
// DEC double-width group emit as real DECDWL/DECDHL lines; mixed rows emit
// their cells literally (double-spaced) so side-by-side content never shifts.
func (t *TUIBackend) EndFrame() {
	t.mu.Lock()
	defer t.mu.Unlock()

	var sb strings.Builder
	clearLines := t.needsLineClear
	t.needsLineClear = false

	termX, termY := -1, -1 // where the terminal cursor sits (unknown)
	penStyle := ""         // SGR last emitted ("" = unknown, always emit)

	for y := range t.dmgMin {
		t.dmgMin[y], t.dmgMax[y] = -1, -1
	}

	for y := 0; y < t.rows; y++ {
		lineCleared := false

		// After resize, clear each line (and its DEC line mode) before updating.
		// DECSWL (ESC#5) is what actually retires the line mode: erase-line
		// clears a row's CONTENT, never its DEC line attribute, so zeroing
		// frontLineAttr without it left the record saying "normal" while the
		// terminal kept the row doubled — and the reversion below, which fires
		// only on a non-zero record, could never rescue it again.
		if clearLines {
			sb.WriteString(fmt.Sprintf("\033[%d;1H\033#5\033[0m\033[2K", y+1))
			t.frontLineAttr[y] = 0
			lineCleared = true
			t.markDamage(y, 0, t.cols-1)
			termY, termX = y, 0
			penStyle = "" // the [0m reset invalidated the tracked pen
		}

		// A row uniformly owned by one DEC double-width group renders as a
		// real DEC line: mode escape, clear, then only the carrier glyphs
		// (the terminal doubles them). Fully re-emitted on any change — no
		// mid-row addressing on a DEC line, whose columns are doubled.
		if mode := t.rowUniformDWL(y); mode != 0 {
			changed := lineCleared || t.frontLineAttr[y] != mode
			if !changed {
				for x := 0; x < t.cols; x++ {
					if t.backBuffer[y][x] != t.frontBuffer[y][x] {
						changed = true
						break
					}
				}
			}
			if changed {
				t.markDamage(y, 0, t.cols-1)
				sb.WriteString(fmt.Sprintf("\033[%d;1H\033#%c\033[0m\033[2K", y+1, mode))
				penStyle = ""
				for x := 0; x < t.cols; x++ {
					c := t.backBuffer[y][x]
					t.frontBuffer[y][x] = c
					if c.DWLFill || c.Char == 0 {
						continue // fillers/continuations: the carrier covers them
					}
					if code := c.Style.CodeDepth(t.colorDepth); code != penStyle {
						sb.WriteString(code)
						penStyle = code
					}
					base, comb := t.driftEmit(y, x, c)
					sb.WriteRune(base)
					sb.WriteString(comb)
				}
				t.frontLineAttr[y] = mode
				termX, termY = -1, -1 // cursor position on a DEC line: treat as unknown
			}
			continue
		}

		// The row is normal (or mixed): if the terminal still shows it as a
		// DEC line, revert to single-width and force a full repaint of it.
		if t.frontLineAttr[y] != 0 {
			sb.WriteString(fmt.Sprintf("\033[%d;1H\033#5", y+1))
			t.frontLineAttr[y] = 0
			for x := 0; x < t.cols; x++ {
				t.frontBuffer[y][x] = invalidCell
			}
			termY, termX = y, 0
		}

		for x := 0; x < t.cols; {
			cell := t.backBuffer[y][x]

			// Continuation cell (right half of a wide glyph): never addressed
			// or emitted on its own — its base cell wrote both columns.
			if cell.Char == 0 && x > 0 && cellRuneWidth(t.backBuffer[y][x-1].Char) == 2 {
				t.frontBuffer[y][x] = cell
				x++
				continue
			}

			w := 1
			if cell.Char != 0 && cellRuneWidth(cell.Char) == 2 {
				w = 2
			}

			// A cell below with the overline attribute reads as an underline on
			// this cell — the "top line" trick (a tab bar overlines its own row
			// so the window frame row above it shows a drawn top border). A
			// wide glyph checks below BOTH of its columns, since the single
			// glyph is the only thing that can carry the underline for either.
			effectiveCell := cell
			for i := 0; i < w && y+1 < t.rows && x+i < t.cols; i++ {
				if t.backBuffer[y+1][x+i].Style.Attrs&style.StyleOverline != 0 {
					effectiveCell.Style.Attrs |= style.StyleUnderline
					break
				}
			}

			// The base and marks emitted here are driftEmit's — the cell's own
			// under normal mode; under drift, the base with its points folded in
			// and the RIGHT neighbour's drifting marks appended. Fold them into
			// the cell we diff and store, so the comparison reflects exactly what
			// renders: a neighbour whose marks change makes THIS cell differ and
			// re-emit on its own, with no cascade.
			effectiveCell.Char, effectiveCell.Combining = t.driftEmit(y, x, effectiveCell)

			if !lineCleared && effectiveCell == t.frontBuffer[y][x] {
				x++
				continue
			}

			t.markDamage(y, x, x+w-1)
			if termY != y || termX != x {
				sb.WriteString(fmt.Sprintf("\033[%d;%dH", y+1, x+1))
				termY, termX = y, x
			}
			if code := effectiveCell.Style.CodeDepth(t.colorDepth); code != penStyle {
				sb.WriteString(code)
				penStyle = code
			}

			if effectiveCell.Char == 0 {
				sb.WriteRune(' ') // stray placeholder with no wide base
			} else {
				sb.WriteRune(effectiveCell.Char)
				sb.WriteString(effectiveCell.Combining)
			}

			t.frontBuffer[y][x] = effectiveCell
			if w == 2 && x+1 < t.cols {
				t.frontBuffer[y][x+1] = t.backBuffer[y][x+1]
			}
			x += w
			if x >= t.cols {
				termX, termY = -1, -1 // wrote at the boundary: cursor unpredictable
			} else {
				termX = x
			}
		}
	}

	// The diff addresses cells all over the screen, and a terminal renders the
	// stream as it parses it — a visible hardware cursor would skate along
	// with the repaint. Hide it for the duration and let the tail below reveal
	// it again, once it is back at its settled position. An empty diff needs
	// no bracket (and must not blink the cursor on an idle present).
	body := sb.String()
	var out strings.Builder
	if body != "" {
		if t.cursorShown {
			out.WriteString("\033[?25l")
			t.cursorShown = false
		}
		out.WriteString(body)
	}

	// Restore cursor position if visible. On a DEC double-width line the
	// terminal addresses doubled columns, so the X is halved there.
	if t.cursorVisible {
		cx := t.cursorX
		if t.cursorY >= 0 && t.cursorY < len(t.frontLineAttr) && t.frontLineAttr[t.cursorY] != 0 {
			cx /= 2
		}
		out.WriteString(fmt.Sprintf("\033[%d;%dH", t.cursorY+1, cx+1))
		// Shape before visibility, so a cursor about to be shown appears
		// already wearing it rather than flickering through the last one.
		if t.cursorStyle != t.cursorStyleSent {
			out.WriteString(fmt.Sprintf("\033[%d q", t.cursorStyle))
			t.cursorStyleSent = t.cursorStyle
		}
		if !t.cursorShown {
			out.WriteString("\033[?25h")
			t.cursorShown = true
		}
	}

	t.write(out.String())

	// Pictures go last: the text diff addresses cells all over the screen,
	// and anything emitted before it would be painted over by text written
	// afterwards. This is also the right order to read - the image sits on
	// the row the text layer already made room for.
	t.flushImagesLocked()
	t.applyMotionTrackingLocked()
}

// Clear fills the entire surface with a style.
func (t *TUIBackend) Clear(s style.CellStyle) {
	t.mu.Lock()
	defer t.mu.Unlock()

	cell := Cell{Char: ' ', Style: s}
	for y := 0; y < t.rows; y++ {
		for x := 0; x < t.cols; x++ {
			t.backBuffer[y][x] = cell
		}
	}
}

// SetClip sets the clipping rectangle.
func (t *TUIBackend) SetClip(clip core.UnitRect) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.clipRect = clip
}

// isInClip checks if a cell coordinate is within the clip region.
// A cell is considered in clip if its starting position is within bounds.
func (t *TUIBackend) isInClip(col, row int) bool {
	x := t.metrics.CellToUnitsX(col)
	y := t.metrics.CellToUnitsY(row)
	return t.clipRect.Contains(core.UnitPoint{X: x, Y: y})
}

// cellFitsInClip checks if a cell fully fits within the clip region.
// Used for optional trailing elements like Tuesday font spacing.
func (t *TUIBackend) cellFitsInClip(col, row int) bool {
	x := t.metrics.CellToUnitsX(col)
	y := t.metrics.CellToUnitsY(row)
	// Check if cell end position is within clip (cell end = start + cell width)
	cellEndX := x + t.metrics.CellWidth
	cellEndY := y + t.metrics.CellHeight
	return x >= t.clipRect.X && cellEndX <= t.clipRect.X+t.clipRect.Width &&
		y >= t.clipRect.Y && cellEndY <= t.clipRect.Y+t.clipRect.Height
}

// setCell sets a cell in the back buffer with clipping.
func (t *TUIBackend) setCell(col, row int, ch rune, s style.CellStyle) {
	if col < 0 || col >= t.cols || row < 0 || row >= t.rows {
		return
	}
	if !t.isInClip(col, row) {
		return
	}
	t.backBuffer[row][col] = Cell{Char: ch, Style: s}
	t.touchCell(col, row)
}

// touchCell stamps a cell with the current paint order. Called wherever the
// back buffer is written, so "who is on top" is answerable after the fact.
func (t *TUIBackend) touchCell(col, row int) {
	t.paintSeq++
	if row >= 0 && row < len(t.cellSeq) && col >= 0 && col < len(t.cellSeq[row]) {
		t.cellSeq[row][col] = t.paintSeq
	}
}

// DrawCell draws a single character at the given position.
func (t *TUIBackend) DrawCell(x, y core.Unit, ch rune, s style.CellStyle) {
	t.mu.Lock()
	defer t.mu.Unlock()

	col := t.metrics.UnitsToCellX(x)
	row := t.metrics.UnitsToCellY(y)
	t.setCell(col, row, ch, s)
}

// DrawText draws a string starting at the given position using the given font.
func (t *TUIBackend) DrawText(x, y core.Unit, text string, s style.CellStyle, font *core.Font) core.Unit {
	t.mu.Lock()
	defer t.mu.Unlock()

	if font == nil {
		font = core.DefaultFont()
	}

	// Apply font's foreground color if set (for debugging/visualization)
	effectiveStyle := s
	if !font.Foreground.IsDefault {
		effectiveStyle = s.WithFg(font.Foreground.Color)
	}

	col := t.metrics.UnitsToCellX(x)
	row := t.metrics.UnitsToCellY(y)

	startCol := col
	// The text backend has no real fonts, so pseudo-fonts fake style by
	// transforming the text: "Tuesday" double-widths it (below), and the cipher
	// fonts (Black Serif, Fraktur, Double-Struck, …) swap ASCII for the
	// visually-styled Unicode math-alphanumerics — width-preserving, so it just
	// changes which glyphs the outer terminal draws. Every other name (ui-term,
	// ui-text, Monday, a graphical family) passes through as the normal Monday
	// cell. Cipher and Tuesday are distinct names, so they never combine.
	text = cipherText(font.Name, text)
	if vtFrakturNative(font.Name) {
		// VTFRAKTUR in native mode: leave the characters plain and emit real
		// SGR-20 fraktur to the enclosing terminal via the cell attribute.
		effectiveStyle.Attrs |= style.StyleFraktur
	}
	isTuesday := font.Name == "Tuesday"

	for _, ch := range text {
		// Zero-width combining marks attach to the previously drawn cell —
		// they never occupy a cell of their own.
		if cellRuneWidth(ch) == 0 {
			t.appendCombining(col-1, row, ch)
			continue
		}
		if col >= t.cols {
			break
		}
		t.setCell(col, row, ch, effectiveStyle)
		col++

		// Handle wide characters (CJK, emoji)
		if cellRuneWidth(ch) > 1 {
			if col < t.cols {
				t.setCell(col, row, 0, effectiveStyle) // continuation of the wide glyph
				col++
			}
		} else if isTuesday && isAlphanumeric(ch) {
			// Tuesday font: add space after alphabetic/numeric chars
			// Only add the space if the cell fully fits in the clip region,
			// allowing "half" of a wide Tuesday character to be shown when truncated
			if col < t.cols && t.cellFitsInClip(col, row) {
				t.setCell(col, row, ' ', effectiveStyle)
				col++
			}
		}
	}

	return t.metrics.TextWidth(col - startCol)
}

// driftEmit returns the base rune and the combining marks to emit for the cell
// at (x, y) — normally the cell's own char and marks unchanged.
//
// Under rtlMarkMode "drift" (experimental) an RTL cell's DRIFTING marks are
// carried by the cell to its LEFT: it keeps its own non-drifting marks and, from
// the cell to its RIGHT, steals that cell's drifting marks, all emitted after
// its own base. A few terminals (current Ghostty and Alacritty among them) place
// an RTL combining sequence this way; drift reproduces it for them.
//
// The cell's own non-drifting POINTS (shin dot, sin dot, dagesh/mappiq, rafe,
// holam-haser) are folded into the base's Alphabetic-Presentation-Form glyph
// rather than emitted free-standing: a drift terminal misplaces a free-standing
// point exactly as it does a vowel, and the presence of a drifting vowel drags
// the point off with it. Baking the point into the base leaves nothing loose for
// the terminal to move. Only RTL vowels/accents drift then; an LTR mark of some
// other script rides its own base as usual. The transform is emit-only, so the
// stored cell is unchanged.
func (t *TUIBackend) driftEmit(y, x int, cell Cell) (rune, string) {
	if core.RtlMarkMode() != "drift" || !isRTLBase(cell.Char) {
		return cell.Char, cell.Combining // normal: the cell's own char and marks
	}
	// This base keeps every mark that does not drift (its point, any LTR mark);
	// fold the points into a presentation form so none is left free-standing.
	own := []rune{cell.Char}
	for _, r := range cell.Combining {
		if !driftsLeft(r) {
			own = append(own, r)
		}
	}
	base := cell.Char
	var b strings.Builder
	if folded, ok := hebrew.PrecomposeCluster(own); ok {
		base = folded[0]
		b.WriteString(string(folded[1:])) // non-folding non-drifting marks (LTR)
	} else {
		b.WriteString(string(own[1:])) // nothing folds: keep the marks as-is
	}
	// …then steal the drifting marks of the cell to its right.
	if x+1 < t.cols {
		if right := t.backBuffer[y][x+1]; isRTLBase(right.Char) {
			for _, r := range right.Combining {
				if driftsLeft(r) {
					b.WriteRune(r)
				}
			}
		}
	}
	return base, b.String()
}

// driftsLeft reports whether a combining mark moves one cell left under drift:
// an RTL-script mark that is NOT one of the marks already placed correctly by
// the base model — the shin dot, the sin dot, and the dagesh/mappiq, which stay
// on their own column.
func driftsLeft(r rune) bool {
	switch r {
	case 0x05C1, 0x05C2, 0x05BC: // shin dot, sin dot, dagesh/mappiq
		return false
	}
	return isRTLBase(r) // RTL-script marks drift; LTR/other-script marks stay
}

// isRTLBase reports whether r belongs to a right-to-left script (Hebrew or
// Arabic) — used both for the cell's base letter and for classing its marks.
func isRTLBase(r rune) bool {
	switch purfecterm.ScriptClass(r) {
	case "hebrew", "arabic":
		return true
	}
	return false
}

// appendCombining attaches a zero-width mark to the base cell at (x, row),
// stepping left off a wide glyph's continuation onto its base.
func (t *TUIBackend) appendCombining(x, row int, ch rune) {
	if row < 0 || row >= t.rows {
		return
	}
	if x >= 0 && x < t.cols && t.backBuffer[row][x].Char == 0 {
		x--
	}
	if x < 0 || x >= t.cols || !t.isInClip(x, row) {
		return
	}
	if t.backBuffer[row][x].Char != 0 {
		t.backBuffer[row][x].Combining += string(ch)
	}
}

// DrawCellDWL draws one logical cell of a DEC double-width (or double-height)
// line as a visual-column group: the glyph carrier at the given position, a
// continuation cell when the glyph is East-Asian wide, then filler spaces out
// to twice the glyph's width — the "left half carries the character, right
// half is a space" model. mode is the DEC selector ('6' DECDWL, '3'/'4' the
// DECDHL halves). Returns the columns consumed. EndFrame emits a row whose
// cells all belong to one DWL mode as a real DEC line (carriers only); a
// mixed row renders these cells literally, i.e. double-spaced.
func (t *TUIBackend) DrawCellDWL(x, y core.Unit, ch rune, combining string, s style.CellStyle, mode byte, cellWidth float64) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	col := t.metrics.UnitsToCellX(x)
	row := t.metrics.UnitsToCellY(y)
	if ch == 0 {
		ch = ' '
	}
	// A terminal grid has whole columns only, so a flex width (which may be
	// fractional) rounds to the columns the glyph actually occupies; the rune's
	// own East Asian width is the fallback when no flex width was given.
	w := cellRuneWidth(ch)
	if cellWidth > 0 {
		w = int(cellWidth + 0.5)
	}
	if w < 1 {
		w = 1
	}
	group := 2 * w

	// Carrier ("left half"), then a continuation for a wide carrier (so its
	// own two columns are never re-addressed in the mixed fallback), then
	// filler spaces ("right halves").
	t.setCellDWL(col, row, Cell{Char: ch, Combining: combining, Style: s, DWLMode: mode})
	next := col + 1
	if w == 2 {
		t.setCellDWL(next, row, Cell{Char: 0, Style: s, DWLMode: mode, DWLFill: true})
		next++
	}
	for ; next < col+group; next++ {
		t.setCellDWL(next, row, Cell{Char: ' ', Style: s, DWLMode: mode, DWLFill: true})
	}
	return group
}

// setCellDWL stores a fully-specified cell (with DWL marks) under clipping.
func (t *TUIBackend) setCellDWL(col, row int, c Cell) {
	if col < 0 || col >= t.cols || row < 0 || row >= t.rows || !t.isInClip(col, row) {
		return
	}
	t.backBuffer[row][col] = c
	t.touchCell(col, row)
}

// isAlphanumeric returns true if the character is a letter or digit.
func isAlphanumeric(ch rune) bool {
	return (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')
}

// DrawTextAligned draws text aligned within a box using the given font.
func (t *TUIBackend) DrawTextAligned(bounds core.UnitRect, text string, hAlign, vAlign core.Alignment, s style.CellStyle, font *core.Font) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if font == nil {
		font = core.DefaultFont()
	}

	// Apply font's foreground color if set (for debugging/visualization)
	effectiveStyle := s
	if !font.Foreground.IsDefault {
		effectiveStyle = s.WithFg(font.Foreground.Color)
	}

	// Convert bounds to cells
	col1 := t.metrics.UnitsToCellX(bounds.X)
	row1 := t.metrics.UnitsToCellY(bounds.Y)
	col2 := t.metrics.UnitsToCellX(bounds.X + bounds.Width)
	row2 := t.metrics.UnitsToCellY(bounds.Y + bounds.Height)

	boxWidth := col2 - col1
	boxHeight := row2 - row1

	// The text backend has no real fonts, so pseudo-fonts fake style by
	// transforming the text: "Tuesday" double-widths it (below), and the cipher
	// fonts (Black Serif, Fraktur, Double-Struck, …) swap ASCII for the
	// visually-styled Unicode math-alphanumerics — width-preserving, so it just
	// changes which glyphs the outer terminal draws. Every other name (ui-term,
	// ui-text, Monday, a graphical family) passes through as the normal Monday
	// cell. Cipher and Tuesday are distinct names, so they never combine.
	text = cipherText(font.Name, text)
	if vtFrakturNative(font.Name) {
		// VTFRAKTUR in native mode: leave the characters plain and emit real
		// SGR-20 fraktur to the enclosing terminal via the cell attribute.
		effectiveStyle.Attrs |= style.StyleFraktur
	}
	isTuesday := font.Name == "Tuesday"

	// Calculate text width in cells accounting for font
	textCells := 0
	for _, ch := range text {
		w := cellRuneWidth(ch)
		if w == 0 {
			continue // combining marks ride the previous cell
		}
		textCells += w
		if w == 1 && isTuesday && isAlphanumeric(ch) {
			textCells++ // Extra cell for spacing
		}
	}

	// Calculate horizontal position
	var col int
	switch hAlign {
	case core.AlignLeft:
		col = col1
	case core.AlignCenter:
		col = col1 + (boxWidth-textCells)/2
	case core.AlignRight:
		col = col2 - textCells
	default:
		col = col1
	}

	// Calculate vertical position
	var row int
	switch vAlign {
	case core.AlignTop:
		row = row1
	case core.AlignMiddle:
		row = row1 + boxHeight/2
	case core.AlignBottom:
		row = row2 - 1
	default:
		row = row1
	}

	// Draw text
	for _, ch := range text {
		if cellRuneWidth(ch) == 0 {
			if col-1 >= col1 {
				t.appendCombining(col-1, row, ch)
			}
			continue
		}
		if col >= col2 {
			break
		}
		if col >= col1 {
			t.setCell(col, row, ch, effectiveStyle)
		}
		col++

		// Handle wide characters
		if cellRuneWidth(ch) > 1 {
			if col < col2 && col >= col1 {
				t.setCell(col, row, 0, effectiveStyle)
			}
			col++
		} else if isTuesday && isAlphanumeric(ch) {
			// Tuesday font: add space after alphabetic/numeric chars
			// Only add the space if the cell fully fits within bounds,
			// allowing "half" of a wide Tuesday character to be shown when truncated
			cellEndX := t.metrics.CellToUnitsX(col) + t.metrics.CellWidth
			if col < col2 && col >= col1 && cellEndX <= bounds.X+bounds.Width {
				t.setCell(col, row, ' ', effectiveStyle)
			}
			col++
		}
	}
}

// FillRect fills a rectangle with a character and style.
func (t *TUIBackend) FillRect(r core.UnitRect, ch rune, s style.CellStyle) {
	t.mu.Lock()
	defer t.mu.Unlock()

	col1 := t.metrics.UnitsToCellX(r.X)
	row1 := t.metrics.UnitsToCellY(r.Y)
	col2 := t.metrics.UnitsToCellX(r.X + r.Width)
	row2 := t.metrics.UnitsToCellY(r.Y + r.Height)

	for row := row1; row < row2; row++ {
		for col := col1; col < col2; col++ {
			t.setCell(col, row, ch, s)
		}
	}
}

// DrawRect draws just the border of a rectangle.
func (t *TUIBackend) DrawRect(r core.UnitRect, border style.BorderStyle, s style.CellStyle) {
	t.mu.Lock()
	defer t.mu.Unlock()

	col1 := t.metrics.UnitsToCellX(r.X)
	row1 := t.metrics.UnitsToCellY(r.Y)
	col2 := t.metrics.UnitsToCellX(r.X+r.Width) - 1
	row2 := t.metrics.UnitsToCellY(r.Y+r.Height) - 1

	if col2 < col1 || row2 < row1 {
		return
	}

	// Corners
	t.setCell(col1, row1, border.TopLeft, s)
	t.setCell(col2, row1, border.TopRight, s)
	t.setCell(col1, row2, border.BottomLeft, s)
	t.setCell(col2, row2, border.BottomRight, s)

	// Top and bottom edges
	for col := col1 + 1; col < col2; col++ {
		t.setCell(col, row1, border.Horizontal, s)
		t.setCell(col, row2, border.Horizontal, s)
	}

	// Left and right edges
	for row := row1 + 1; row < row2; row++ {
		t.setCell(col1, row, border.Vertical, s)
		t.setCell(col2, row, border.Vertical, s)
	}
}

// DrawHLine draws a horizontal line.
func (t *TUIBackend) DrawHLine(x, y, width core.Unit, ch rune, s style.CellStyle) {
	t.mu.Lock()
	defer t.mu.Unlock()

	col := t.metrics.UnitsToCellX(x)
	row := t.metrics.UnitsToCellY(y)
	endCol := t.metrics.UnitsToCellX(x + width)

	for c := col; c < endCol; c++ {
		t.setCell(c, row, ch, s)
	}
}

// DrawVLine draws a vertical line.
func (t *TUIBackend) DrawVLine(x, y, height core.Unit, ch rune, s style.CellStyle) {
	t.mu.Lock()
	defer t.mu.Unlock()

	col := t.metrics.UnitsToCellX(x)
	row := t.metrics.UnitsToCellY(y)
	endRow := t.metrics.UnitsToCellY(y + height)

	for r := row; r < endRow; r++ {
		t.setCell(col, r, ch, s)
	}
}

// DrawBox draws a box with optional title.
func (t *TUIBackend) DrawBox(r core.UnitRect, border style.BorderStyle, title string, s style.CellStyle) {
	// Draw the rectangle border
	t.DrawRect(r, border, s)

	if title == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Draw title on top edge
	col1 := t.metrics.UnitsToCellX(r.X)
	col2 := t.metrics.UnitsToCellX(r.X + r.Width)
	row := t.metrics.UnitsToCellY(r.Y)

	titleLen := utf8.RuneCountInString(title)
	maxLen := col2 - col1 - 4 // Leave space for " Title "
	if maxLen < 1 {
		return
	}

	displayTitle := title
	if titleLen > maxLen {
		displayTitle = string([]rune(title)[:maxLen-1]) + "…"
		titleLen = maxLen
	}

	// Center title
	startCol := col1 + 2
	t.setCell(startCol-1, row, ' ', s)
	col := startCol
	for _, ch := range displayTitle {
		t.setCell(col, row, ch, s)
		col++
	}
	t.setCell(col, row, ' ', s)
}

// PollEvent returns the next input event, or nil if none available.
func (t *TUIBackend) PollEvent() core.Event {
	select {
	case event := <-t.eventQueue:
		return event
	default:
		return nil
	}
}

// WaitEvent blocks until an event is available.
func (t *TUIBackend) WaitEvent() core.Event {
	select {
	case event := <-t.eventQueue:
		return event
	case <-t.stopChan:
		return core.QuitEvent{}
	}
}

// SetCursorVisible shows or hides the cursor. HIDING takes effect at once;
// SHOWING is recorded and left to the next present, which addresses the cursor
// before revealing it — the same rule SetCursorStyle follows, and for the same
// reason: the cursor must never appear at a stale position, and must never be
// visible while a repaint drags it across the screen. (Callers show the caret
// from inside their Frame, so an EndFrame always follows.)
func (t *TUIBackend) SetCursorVisible(visible bool) {
	t.mu.Lock()
	t.cursorVisible = visible
	hide := !visible && t.cursorShown
	if hide {
		t.cursorShown = false
	}
	t.mu.Unlock()

	if hide {
		t.write("\033[?25l")
	}
}

// SetCursorStyle records the DECSCUSR shape for the next present. It is not
// written immediately: a shape only reaches the terminal beside the cursor it
// belongs to, so it can never reveal or restyle a hidden cursor.
func (t *TUIBackend) SetCursorStyle(style int) {
	if style < 0 || style > 6 {
		return
	}
	t.mu.Lock()
	t.cursorStyle = style
	t.mu.Unlock()
}

// SetCursorPosition positions the cursor.
// The position is recorded, not written: the present addresses the cursor
// itself, once the repaint that would have dragged it around is done (see
// EndFrame). Writing here would move a visible cursor mid-frame — the very
// jump the present's hide/show bracket exists to prevent — and be re-issued
// moments later anyway.
func (t *TUIBackend) SetCursorPosition(x, y core.Unit) {
	t.mu.Lock()
	t.cursorX = t.metrics.UnitsToCellX(x)
	t.cursorY = t.metrics.UnitsToCellY(y)
	t.mu.Unlock()
}

// SupportsColor returns whether the backend supports color.
func (t *TUIBackend) SupportsColor() bool {
	return t.colorDepth > 2
}

// SupportsMouse returns whether the backend supports mouse input.
func (t *TUIBackend) SupportsMouse() bool {
	return t.hasMouse
}

// SupportsUnicode returns whether the backend supports Unicode.
func (t *TUIBackend) SupportsUnicode() bool {
	return t.hasUnicode
}

// ColorDepth returns the number of colors supported.
func (t *TUIBackend) ColorDepth() int {
	return t.colorDepth
}

// GetClipboard returns the host's internal clipboard - what Copy/Cut last
// stored (and the latest OSC 52 read-back reply, which is mirrored into it).
// This never blocks; the actual terminal query is the async RequestClipboardRead
// path (the AsyncClipboardReader capability), which the desktop drives so it can
// show a "waiting for clipboard" modal while the terminal prompts the user.
func (t *TUIBackend) GetClipboard() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.clipboard
}

// RequestClipboardRead implements core.AsyncClipboardReader: it emits the OSC 52
// read query (ESC ] 52 ; c ; ? BEL) and returns whether a reply may arrive.
// When read-back is disabled it returns false so the caller uses the internal
// clipboard. The reply (if any) is delivered to the handler registered with
// SetClipboardReadHandler.
func (t *TUIBackend) RequestClipboardRead() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.osc52Paste || t.output == nil {
		return false
	}
	fmt.Fprint(t.output, "\033]52;c;?\a")
	return true
}

// SetClipboardReadHandler implements core.AsyncClipboardReader.
func (t *TUIBackend) SetClipboardReadHandler(fn func(text string)) {
	t.mu.Lock()
	t.onClipboardRead = fn
	t.mu.Unlock()
}

// deliverClipboard records a clipboard read-back reply (from the keyboard
// handler's OSC 52 callback) into the internal clipboard and notifies the
// registered read handler.
func (t *TUIBackend) deliverClipboard(s string) {
	t.mu.Lock()
	t.clipboard = s
	h := t.onClipboardRead
	t.mu.Unlock()
	if h != nil {
		h(s)
	}
}

// deliverPaste queues a bracketed paste from the outer terminal as ONE
// core.PasteEvent. Unlike handleKey's best-effort enqueue — whose full-queue
// branch drops, which is what truncated large pastes forwarded as a key flood —
// this blocks until the queue accepts the event (or the backend stops), so a
// paste of any size arrives whole. One event per paste means it cannot fill the
// queue by itself, and the running event loop drains it near-instantly; the
// stopChan arm keeps a paste arriving during shutdown from wedging the reader.
func (t *TUIBackend) deliverPaste(text string) {
	if text == "" {
		return
	}
	select {
	case t.eventQueue <- core.PasteEvent{Text: text}:
	case <-t.stopChan:
	}
}

// SetClipboard stores the text in the internal clipboard and, when OSC 52 is
// enabled, mirrors it to the terminal's clipboard so Copy/Cut reach other apps.
// OSC 52 set: ESC ] 52 ; c ; <base64> BEL.
func (t *TUIBackend) SetClipboard(text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.clipboard = text
	if !t.osc52 || t.output == nil {
		return
	}
	enc := base64.StdEncoding.EncodeToString([]byte(text))
	fmt.Fprintf(t.output, "\033]52;c;%s\a", enc)
}

// Beep produces an audible alert.
func (t *TUIBackend) Beep() {
	t.mu.Lock()
	defer t.mu.Unlock()
	fmt.Fprint(t.output, "\a")
}

// handleKey processes key events from the keyboard handler.
func (t *TUIBackend) handleKey(key string) {
	// Outer-terminal replies to our pixel-mouse probe (see Init). These are
	// backend business, not app input, so consume them here — otherwise they
	// would fall through and be misread as bogus keystrokes.
	// Text the terminal received with no key behind it (the "kitty" protocol's
	// keycode 0, direct-key-handler's keyboard.TextPrefix). An input method
	// committing a composition is what sends it: the terminal owns the
	// candidate list, so all that reaches this process is the text chosen.
	//
	// Raised as the COMMIT it is, which is the same event the graphical host
	// raises for its own compositions — so a trinket handles both with one
	// piece of code. Nothing is standing over anything here; the protocol has
	// no way to report a composition in flight, so it lands as an insert.
	//
	// Never as a keystroke. A bare name in this stream means a key was pressed,
	// and the text derivation below reads ONE rune from a name, so a commit
	// spelled as a key would be a keystroke whose text is silently empty
	// whenever it ran to more than one character - which is most of them.
	if text, ok := strings.CutPrefix(key, "Text:"); ok {
		if text != "" {
			// Whatever a palette was open OVER comes back out first. A commit
			// is the only thing that spends the take-back, which is what makes
			// the count safe to keep: nothing is erased unless something
			// arrived to replace it.
			t.takeBackHeld()
			if core.KeyTracing() {
				core.KeyTracef("1 tui      commit  text=%q", text)
			}
			select {
			case t.eventQueue <- core.TextCommitEvent{Text: text}:
			default:
			}
		}
		return
	}
	if strings.HasPrefix(key, "DECRPM:") {
		t.handleDECRPM(key)
		return
	}
	if strings.HasPrefix(key, "APC:") {
		t.handleAPC(key)
		return
	}
	if strings.HasPrefix(key, "DA1:") {
		t.handleDA1(key)
		return
	}
	if strings.HasPrefix(key, "WinOp:") {
		t.handleWinOp(key)
		return
	}

	// Check for mouse events from direct-key-handler
	// Mouse events come as two keys: "Mouse@x,y" (position) followed by action
	if strings.HasPrefix(key, "Mouse@") {
		// Parse position: Mouse@x,y. Store the RAW 1-based coordinate — a cell
		// column normally, an outer pixel under ?1016 — and let outerToUnits*
		// resolve it to units at action time (it knows the current mode).
		var x, y int
		if _, err := fmt.Sscanf(key, "Mouse@%d,%d", &x, &y); err == nil {
			t.mu.Lock()
			t.pendingMouseX = x
			t.pendingMouseY = y
			t.mu.Unlock()
		}
		// The pointer is somewhere it was not, which is a move — the only kind
		// there is with no button held. An action arriving right behind this
		// one resolves against the same position, so a click reads as a move to
		// the spot and then the press, which is what the pointer did.
		t.handleMouseAction("MouseMove")
		return
	}

	// Check for mouse action events — which may arrive MODIFIER-PREFIXED
	// ("S-MouseRight" from a terminal that forwards shifted clicks, as
	// iTerm2 does), so the mouse-ness test looks past the prefixes.
	if _, name := core.ParseKeyModifiers(key); strings.HasPrefix(name, "Mouse") {
		t.handleMouseAction(key)
		return
	}

	// A key coming back UP. The outer terminal sends these only because Init
	// asks for event reporting (the "2" in CSI > 3 u); before that they never
	// arrived, and this backend produced no KeyReleaseEvent at all, so a hosted
	// child that wanted them could not be given any.
	//
	// The suffix comes off here so Key holds the bare name, matching what the
	// SDL backend puts in the same field. Whoever forwards to a child puts the
	// marker back — see the PurfecTerm trinket.
	//
	// A repeat is deliberately left as a press: it IS another press, every
	// consumer already treats it as one, and the distinction only matters to a
	// child that negotiated event reporting for itself.
	if base, isRelease := strings.CutSuffix(key, ":Release"); isRelease {
		// The hold is over, so the next one asks the palette question again.
		// Left standing, a second hold of the SAME key found its wait already
		// spent and repeated at once — so the palette could be reached once per
		// key per session and never again.
		if t.holdKey == base {
			t.holdKey = ""
		}
		// A text key letting go while a take-back stands, and it is not the key
		// the take-back BELONGS to, is somebody typing rather than a palette
		// producing anything. That is what bounds the count: every capture has
		// the commit arriving before the key that chose it comes up, so a
		// release reaching here first means no commit is coming.
		//
		// The armed key's own release is exempt, and has to be. Letting go is
		// how the palette gets used — in every capture the held key comes up
		// before the arrows that walk the palette, let alone the choice.
		if t.holdArm != "" && base != t.holdArm && typesText(base) {
			t.endHold()
		}
		mods, _ := core.ParseKeyModifiers(base)
		if core.KeyTracing() {
			core.KeyTracef("1 tui      release key=%q mods=%v", base, mods)
		}
		select {
		case t.eventQueue <- core.KeyReleaseEvent{Key: base, Modifiers: mods}:
		default:
		}
		return
	}
	// A REPEAT is a press, and it is also a repeat. The marker comes off the
	// name — every consumer reads Key as a plain key, and one carrying a suffix
	// would match nothing — but it is recorded on the event rather than thrown
	// away, so a hosted guest that can read the distinction gets to.
	key, repeated := strings.CutSuffix(key, ":Repeat")

	// A held TEXT key's repeats wait, so a press-and-hold accent palette can be
	// reached. macOS opens the palette on the hold and the terminal goes on
	// repeating from behind it, so the letter types itself over and over while
	// the palette is up — and there is no moment early enough to let go in.
	// Without the wait the palette is not untidy, it is unusable.
	//
	// A TEXT key only. An arrow, a page key, a function key repeats because
	// somebody is navigating, and holding one is the ordinary way to do it; no
	// palette opens over those and delaying them would only make the host feel
	// stuck. That asymmetry is the whole of the rule.
	//
	// Timed, because nothing else can say WHEN the hold stops meaning "open the
	// palette" and starts meaning "type this many of them". The graphical host
	// infers the takeover from a held key whose text never arrives (see the SDL
	// platform's flushPendingPress, which swallows exactly these repeats); a
	// terminal reports no composition at all, so how long the key has been down
	// is the only evidence there is. Past the wait the repeats flow: by then
	// the hold means what a hold usually means.
	//
	// The hold is started by the PRESS and by nothing else, because a repeat is
	// a repeat OF a key that is down. Only the held key's own repeats are
	// withheld, and that is what separates the palette from its RESULT: the
	// terminal marks the chosen character as an event type 2 as well — Return
	// confirming ö arrives as "CSI 13;1:2;246u", a repeat by the marker and a
	// commit by every other measure. Keyed on the marker alone this withheld
	// the commit and left the original letter standing, which is exactly what
	// the palette looked like when it appeared to do nothing at all.
	if !repeated {
		if t.holdWait <= 0 || !typesText(key) {
			// A key that types nothing ends the take-back. Escape dismissing
			// the palette is this one, and so is any command key: the letter
			// the palette opened over is staying, and nothing is coming to
			// replace it.
			t.endHold()
		} else {
			// A text key starts a hold of its own, since any of them might be
			// the next palette. If a take-back is already standing it is also
			// COUNTED, because a character typed while a palette is open
			// belongs to the palette — the selector digit, which macOS types
			// into the document and expects replaced along with the base
			// letter.
			//
			// Counting where guessing would be unsafe, because the count is
			// only ever SPENT by a commit. Type on past a palette that was
			// never there and none arrives, so nothing is taken back.
			if t.holdArm != "" {
				t.holdExtra++
			}
			t.holdKey, t.holdSince = key, time.Now()
		}
	} else if key == t.holdKey && time.Since(t.holdSince) < t.holdWait {
		// Withheld — and a withheld repeat is the only evidence a terminal
		// gives that a palette opened, so it is what arms the take-back.
		if t.holdArm == "" {
			t.holdArm = key
		}
		if core.KeyTracing() {
			core.KeyTracef("1 tui      hold    key=%q withheld", key)
		}
		return
	} else if key == t.holdKey {
		// Past the wait, and the repeats flow. A hold that has got this far
		// means what a hold usually means — type another one — so the letter
		// it started with is not a palette's base character to be taken back.
		t.endHold()
	}

	// Parse modifiers while keeping the full key string
	// Key names follow direct-key-handler convention:
	// - Control+letter: "^A", "^X" etc.
	// - Special keys: "Left", "Right", "Up", "Down", "Return", "Tab", "Escape", etc.
	// - Function keys: "F1", "F2", ... "F12"
	// - Mega combinations: "M-" prefix
	// - Shift combinations: "S-" prefix
	mods, keyName := core.ParseKeyModifiers(key)

	// Determine text content for printable characters.
	//
	// One RUNE, not one byte. Under ReportAllKeys the terminal sends no text at
	// all and a key's name is the only place its character can come from — and
	// a name is UTF-8, so a byte-length test silently dropped the text of every
	// key outside ASCII the moment that flag went on.
	var text string
	if r, size := utf8.DecodeRuneInString(keyName); size == len(keyName) &&
		r != utf8.RuneError && unicode.IsPrint(r) {
		text = keyName
	}

	event := core.KeyPressEvent{
		Key:       key,  // Full key string including modifier prefixes
		Modifiers: mods, // Also provide parsed modifiers for trinket convenience
		Text:      text,
		Repeat:    repeated,
	}
	if core.KeyTracing() {
		core.KeyTracef("1 tui      press   key=%q mods=%v repeat=%v", key, mods, repeated)
	}

	select {
	case t.eventQueue <- event:
	default:
		// Queue full, drop event
	}
}

// takeBackHeld removes the letter a held key typed, just before the character
// chosen out of the palette that opened over it arrives.
//
// The graphical host never needs this: it is handed a real composition, so the
// base letter sits inside its Covers — hidden while the palette is up and
// replaced by the commit. A terminal reports no composition at all, so the
// letter is really in the document by the time anything reveals that a palette
// was ever open, and the only way to put things right is to take it back.
//
// What arms it is a WITHHELD repeat, which is the single piece of evidence a
// terminal gives that a palette opened: the key was held past the auto-repeat
// threshold and the repeats were kept back, which is that gesture and no other.
//
// It takes back the palette's SELECTOR as well as the base letter. Choosing by
// number types the digit into the document and macOS expects it replaced along
// with the letter, without asking for it back the way the graphical host is
// asked (there is no Backspace anywhere in the captures). Every ordinary
// character typed while the take-back stands is counted for that reason, and
// the count is safe to keep because only a commit ever spends it: type on past
// a palette that was never there and nothing arrives to erase anything.
//
// The erase goes out as an erase rather than as a Backspace for the reason
// TextEraseEvent exists at all — nobody pressed Backspace, and the sink is what
// knows whether erasing here means a rune out of a document or a byte down a
// terminal session.
func (t *TUIBackend) takeBackHeld() {
	if t.holdArm == "" {
		return
	}
	count := 1 + t.holdExtra
	t.endHold()
	if core.KeyTracing() {
		core.KeyTracef("1 tui      hold    take back %d before the commit", count)
	}
	select {
	case t.eventQueue <- core.TextEraseEvent{Count: count}:
	default:
	}
}

// endHold gives up the take-back: whatever the palette opened over is staying.
func (t *TUIBackend) endHold() {
	t.holdArm, t.holdExtra = "", 0
}

// typesText reports whether a key name is one that TYPES — a single printable
// rune, which is what a text key is called in this vocabulary.
//
// Deliberately narrow. A chord ("^A", "M-x") is not a text key, a named key
// ("Down", "F1", "Return") is not one either, and neither is a modifier
// reporting itself. Only the keys an accent palette can open over.
func typesText(key string) bool {
	r, size := utf8.DecodeRuneInString(key)
	return size == len(key) && r != utf8.RuneError && unicode.IsPrint(r)
}

// handleMouseAction processes mouse action events from direct-key-handler.
func (t *TUIBackend) handleMouseAction(key string) {
	// One snapshot, under one lock: the stashed position AND the frame it is
	// resolved against. The probe replies that set that frame land on the
	// terminal's read path while events are being converted here, and its
	// three fields only mean anything together — pixelMouse says to divide by
	// a cell size that outerCell{W,H} supplies, so reading them at separate
	// moments could pair a mode from after the flip with a size from before
	// it. Taking them apart was also a data race outright, benign-looking or
	// not. (t.metrics is written once at construction and never after.)
	t.mu.Lock()
	x := t.pendingMouseX
	y := t.pendingMouseY
	frame := outerMouseFrame{
		pixelMouse: t.pixelMouse,
		cellW:      t.outerCellW,
		cellH:      t.outerCellH,
	}
	t.mu.Unlock()

	// Strip modifier prefixes ("S-MouseRight") into event modifiers.
	// Terminals VARY in whether they forward modified clicks to the app —
	// iTerm2 sends shift+clicks through (shifted), stock Terminal strips
	// the shift — and a modified mouse event must reach the trinkets with
	// its modifiers, not be dropped as unknown here.
	mods, key := core.ParseKeyModifiers(key)

	// Convert the raw 1-based coordinate to units (cell- or pixel-based
	// depending on whether ?1016 is active — see outerToUnitsX/Y).
	unitX := t.outerToUnitsX(x, frame)
	unitY := t.outerToUnitsY(y, frame)

	// For drag events, position is embedded: MouseDragLeft@x,y (also raw
	// 1-based, same conversion).
	//
	// Which of the two sources a gesture used is the thing the trace below
	// exists to record: the stash is shared state a press depends on and a
	// motion never touches, so it is the one place the two can diverge.
	src := "stash"
	if strings.Contains(key, "@") {
		var dragX, dragY int
		parts := strings.SplitN(key, "@", 2)
		if len(parts) == 2 {
			if _, err := fmt.Sscanf(parts[1], "%d,%d", &dragX, &dragY); err == nil {
				x, y = dragX, dragY
				src = "embedded"
				unitX = t.outerToUnitsX(dragX, frame)
				unitY = t.outerToUnitsY(dragY, frame)
			}
		}
		key = parts[0] // Strip position from key for matching
	}

	if core.MouseTracing() {
		core.MouseTracef("outer  %-18s raw=(%d,%d) via=%-8s pixelMouse=%v outerCell=%dx%d -> units=(%v,%v)",
			key, x, y, src, t.pixelMouse, t.outerCellW, t.outerCellH, unitX, unitY)
	}

	var event core.Event

	switch key {
	case "MouseLeft":
		event = core.MousePressEvent{X: unitX, Y: unitY, Button: core.LeftButton, Modifiers: mods}
	case "MouseMiddle":
		event = core.MousePressEvent{X: unitX, Y: unitY, Button: core.MiddleButton, Modifiers: mods}
	case "MouseRight":
		event = core.MousePressEvent{X: unitX, Y: unitY, Button: core.RightButton, Modifiers: mods}

	case "MouseLeft:Release":
		event = core.MouseReleaseEvent{X: unitX, Y: unitY, Button: core.LeftButton, Modifiers: mods}
	case "MouseMiddle:Release":
		event = core.MouseReleaseEvent{X: unitX, Y: unitY, Button: core.MiddleButton, Modifiers: mods}
	case "MouseRight:Release":
		event = core.MouseReleaseEvent{X: unitX, Y: unitY, Button: core.RightButton, Modifiers: mods}

	// Motion, with whichever button is held. direct-key-handler names the
	// button in the event; motion with NO button is the bare position report
	// this backend turns into "MouseMove" above, so that one carries no button
	// rather than a default.
	case "MouseDragLeft":
		event = core.MouseMoveEvent{X: unitX, Y: unitY, Buttons: core.LeftButton, Modifiers: mods}
	case "MouseDragMiddle":
		event = core.MouseMoveEvent{X: unitX, Y: unitY, Buttons: core.MiddleButton, Modifiers: mods}
	case "MouseDragRight":
		event = core.MouseMoveEvent{X: unitX, Y: unitY, Buttons: core.RightButton, Modifiers: mods}
	case "MouseMove":
		event = core.MouseMoveEvent{X: unitX, Y: unitY, Modifiers: mods}

	case "MouseScrollUp":
		event = core.MouseWheelEvent{X: unitX, Y: unitY, DeltaY: -1, Modifiers: mods}
	case "MouseScrollDown":
		event = core.MouseWheelEvent{X: unitX, Y: unitY, DeltaY: 1, Modifiers: mods}
	case "MouseScrollLeft":
		event = core.MouseWheelEvent{X: unitX, Y: unitY, DeltaX: -1, Modifiers: mods}
	case "MouseScrollRight":
		event = core.MouseWheelEvent{X: unitX, Y: unitY, DeltaX: 1, Modifiers: mods}

	default:
		return // Unknown mouse event
	}

	select {
	case t.eventQueue <- event:
	default:
		// Queue full, drop event
	}
}

// outerMouseFrame is the state a raw mouse coordinate is resolved against,
// taken as one snapshot under the lock (see handleMouseAction). The three
// fields are only meaningful together, so they travel together.
type outerMouseFrame struct {
	pixelMouse   bool // ?1016 is on, so the raw numbers are outer pixels
	cellW, cellH int  // the outer terminal's cell size, in those pixels
}

// outerToUnitsX converts a raw 1-based mouse X coordinate to units. In the
// default SGR mode the number is a 1-based cell column, so it maps to that
// cell's left edge. Under ?1016 it is a 1-based OUTER PIXEL: the integer cell
// index divides out, and the sub-cell remainder scales into a fraction of this
// backend's cell width — the sub-cell position mew's nearest-edge caret uses.
func (t *TUIBackend) outerToUnitsX(raw int, f outerMouseFrame) core.Unit {
	if f.pixelMouse && f.cellW > 0 {
		px := raw - 1
		if px < 0 {
			px = 0
		}
		cell := px / f.cellW
		frac := px % f.cellW
		return t.metrics.CellToUnitsX(cell) + core.Unit(frac)*t.metrics.CellWidth/core.Unit(f.cellW)
	}
	return t.metrics.CellToUnitsX(raw - 1)
}

// outerToUnitsY is the vertical twin of outerToUnitsX.
func (t *TUIBackend) outerToUnitsY(raw int, f outerMouseFrame) core.Unit {
	if f.pixelMouse && f.cellH > 0 {
		px := raw - 1
		if px < 0 {
			px = 0
		}
		cell := px / f.cellH
		frac := px % f.cellH
		return t.metrics.CellToUnitsY(cell) + core.Unit(frac)*t.metrics.CellHeight/core.Unit(f.cellH)
	}
	return t.metrics.CellToUnitsY(raw - 1)
}

// handleDECRPM consumes a "DECRPM:Ps;Pm" reply to our DECRQM probe. For ?1016
// (SGR-Pixels), Pm tells us whether the mode is settable: 0 = unrecognized,
// 1 = set, 2 = reset, 3 = perm-set, 4 = perm-reset. Anything but "unrecognized"
// or "permanently reset" means we can enable it.
func (t *TUIBackend) handleDECRPM(key string) {
	var ps, pm int
	if _, err := fmt.Sscanf(key, "DECRPM:%d;%d", &ps, &pm); err != nil {
		return
	}
	if ps != 1016 {
		return
	}
	if pm == 1 || pm == 2 || pm == 3 {
		t.mu.Lock()
		t.outerPixelOK = true
		t.mu.Unlock()
		t.maybeEnablePixelMouse()
	}
}

// handleWinOp consumes a "WinOp:Ps;..." XTWINOPS reply. Ps=6 is the CELL pixel
// size, reported height-then-width; that is the divisor pixel reports need.
func (t *TUIBackend) handleWinOp(key string) {
	var ps, h, w int
	if _, err := fmt.Sscanf(key, "WinOp:%d;%d;%d", &ps, &h, &w); err != nil {
		return
	}
	if ps != 6 || w <= 0 || h <= 0 {
		return
	}
	t.mu.Lock()
	t.outerCellW = w
	t.outerCellH = h
	t.outerCellSizeOK = true
	t.mu.Unlock()
	t.maybeEnablePixelMouse()
}

// maybeEnablePixelMouse turns on ?1016 once BOTH probe replies have arrived —
// the mode is settable AND we know the outer cell pixel size. The two replies
// race (either order), so this is called after each and enables exactly once.
// ?1006 stays on; ?1016 refines it to pixel coordinates on the same SGR wire.
func (t *TUIBackend) maybeEnablePixelMouse() {
	t.mu.Lock()
	ready := t.hasMouse && t.outerPixelOK && t.outerCellSizeOK && !t.pixelMouse
	if ready {
		t.pixelMouse = true
	}
	t.mu.Unlock()
	if ready {
		t.writeTTY("\033[?1016h")
	}
}

// writeTTY sends a TERMINAL MODE escape - one that changes the terminal's state
// rather than painting content - straight to /dev/tty, falling back to the
// configured output when that cannot be opened.
//
// Mode changes have to reach somewhere they take effect, and they have to be
// UNDONE through the same channel. Under `app > file` the enable would
// otherwise reach the terminal (via /dev/tty) while the disable went into the
// file, leaving raw/alt-screen/"kitty"-keyboard state set with nothing still
// running to clear it. Content writes keep using write() and the configured
// output, which is what redirection is for.
func (t *TUIBackend) writeTTY(s string) {
	if t.ttyOut != nil {
		io.WriteString(t.ttyOut, s)
		return
	}
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		t.write(s)
		return
	}
	defer tty.Close()
	io.WriteString(tty, s)
}

// write outputs a string to the terminal.
func (t *TUIBackend) write(s string) {
	io.WriteString(t.output, s)
}

// detectColorDepth attempts to detect the terminal's color capability.
func detectColorDepth() int {
	// Check COLORTERM for true color
	colorterm := os.Getenv("COLORTERM")
	if colorterm == "truecolor" || colorterm == "24bit" {
		return 16777216
	}

	// Check TERM for 256 colors
	termEnv := os.Getenv("TERM")
	if strings.Contains(termEnv, "256color") {
		return 256
	}

	// Check for basic color support
	if strings.Contains(termEnv, "color") || strings.Contains(termEnv, "xterm") {
		return 16
	}

	// Default to 16 colors
	return 16
}

// markDamage records that the text diff rewrote cells [c0,c1] of row y.
func (t *TUIBackend) markDamage(y, c0, c1 int) {
	if y < 0 || y >= len(t.dmgMin) {
		return
	}
	if c0 < 0 {
		c0 = 0
	}
	if c1 > t.cols-1 {
		c1 = t.cols - 1
	}
	if t.dmgMin[y] < 0 || c0 < t.dmgMin[y] {
		t.dmgMin[y] = c0
	}
	if c1 > t.dmgMax[y] {
		t.dmgMax[y] = c1
	}
}

// damagedRectLocked returns the bounding box of the cells the frame's text diff
// rewrote WITHIN the rectangle [c0,c1] x [r0,r1], and whether any were.
func (t *TUIBackend) damagedRectLocked(c0, r0, c1, r1 int) (dc0, dr0, dc1, dr1 int, any bool) {
	dc0, dr0, dc1, dr1 = c1, r1, c0, r0
	for y := r0; y <= r1; y++ {
		if y < 0 || y >= len(t.dmgMin) || t.dmgMin[y] < 0 {
			continue
		}
		lo, hi := t.dmgMin[y], t.dmgMax[y]
		if lo > c1 || hi < c0 {
			continue // this row's damage misses the rectangle entirely
		}
		if lo < c0 {
			lo = c0
		}
		if hi > c1 {
			hi = c1
		}
		if !any {
			dr0 = y
		}
		dr1 = y
		if lo < dc0 {
			dc0 = lo
		}
		if hi > dc1 {
			dc1 = hi
		}
		any = true
	}
	return dc0, dr0, dc1, dr1, any
}

// RequestMotionTracking implements core.MotionTracker: something painting this
// frame needs to follow the pointer with no button held.
func (t *TUIBackend) RequestMotionTracking() {
	t.mu.Lock()
	t.motionWanted = true
	t.mu.Unlock()
}

// applyMotionTrackingLocked turns ?1003 on or off to match this frame's
// requests, and only when it actually changes.
//
// The mode is the difference between a hosted browser seeing hover and not
// seeing it at all: without ?1003 the outer terminal reports motion only while
// a button is held, so those events never reach us to forward. It is off by
// default because it is not free — every pixel the pointer crosses becomes a
// report on the wire — so it follows demand rather than being pinned on.
func (t *TUIBackend) applyMotionTrackingLocked() {
	if t.motionWanted == t.motionOn {
		return
	}
	t.motionOn = t.motionWanted
	if t.motionOn {
		t.writeTTY("\033[?1003h")
	} else {
		t.writeTTY("\033[?1003l")
	}
}
