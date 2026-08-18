package editor

// PTY sessions: mew asks its HOST for a terminal and never spawns one itself.
//
// The shape is deliberate. mew holds a PTYSession, which can read, write,
// resize and close — and nothing else. It cannot name a local binary, because
// there is no method that takes one. purfecterm's PTY interface is this plus
// Start(*exec.Cmd), and that one extra method is exactly the capability mew
// must not have: with it, a mew embedded in someone else's application could
// run anything on their machine.
//
// So the request travels as DATA — a cwd URL and a command NAME — and the host
// decides what those mean. The root mew's host maps file:///... plus "bash" to
// a real shell in that directory, because the person asking owns the machine.
// Another host may map the same request into a container, a remote box, a
// restricted menu, or refuse it. mew's code path is identical either way, and
// mew has no way to tell which it got: the only channel is bytes in, bytes
// out, resize, close — the same contract the terminal trinket itself speaks
// over the display protocol.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/viewport"
)

// sgrRE matches CSI-m sequences — SGR colour/style plus purfecterm's BGP
// (…;158m/159m) and flip (150–153m) extensions, all of which end in 'm'. No
// positioning or layout escape uses that final byte, so stripping these is
// exactly --plain: drop the styling, keep the shape.
var sgrRE = regexp.MustCompile("\x1b\\[[0-9;:]*m")

// stripSGR removes the styling escapes from a captured ANS stream, leaving
// positioning and layout intact. This is the mew-side realization of --plain.
func stripSGR(s string) string { return sgrRE.ReplaceAllString(s, "") }

// allEscRE matches VT escape sequences: OSC (BEL- or ST-terminated), CSI, and
// the shorter ESC-intermediate / 2-byte forms. It strips everything for the
// --text format on a byte stream, where there is no cell serializer to walk.
var allEscRE = regexp.MustCompile(
	"\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)" + // OSC … BEL | ST
		"|\x1b\\[[0-9;:?<=>]*[ -/]*[@-~]" + // CSI
		"|\x1b[ -/]+[0-~]" + // ESC intermediates + final (e.g. ESC ( B)
		"|\x1b[@-_]") // 2-byte C1

// captureStream folds a session's live output into its buffer at the output
// cursor as it arrives — the mew end of the raw rung (and the future
// lines/live rungs). Garland coalesces the inserts, so there is no batching
// here; the only state is the escape-carry the filters need so a sequence split
// across two chunks is not half-stripped.
type captureStream struct {
	rung   captureRung
	cursor *buffer.Cursor
	format captureFormat
	carry  []byte      // an incomplete trailing escape held back from the last chunk
	mirror *liveMirror // the 2-D screen mirror; set only for the live rung
}

// Output folds one chunk of raw output in at the cursor, filtered per format.
// Only the raw rung consumes it; a lines session ignores it (it folds via
// LineOff instead).
func (c *captureStream) Output(data []byte) {
	if c.rung != captureRaw {
		return
	}
	if s := c.filter(data, false); s != "" {
		c.cursor.InsertString(s, nil, false)
	}
}

// LineOff folds one transcript line in at the cursor with a newline. Only the
// lines rung consumes it. The line is a complete self-contained ANSI string, so
// no escape can straddle a boundary — the format filters apply directly.
func (c *captureStream) LineOff(ansLine string) {
	if c.rung != captureLines {
		return
	}
	switch c.format {
	case capturePlain:
		ansLine = stripSGR(ansLine)
	case captureText:
		ansLine = allEscRE.ReplaceAllString(ansLine, "")
	}
	c.cursor.InsertString(ansLine+"\n", nil, false)
}

// WantsLines lets the host skip the per-line serialization for a session that is
// not on the lines rung. Checked by the trinket via an anonymous interface.
func (c *captureStream) WantsLines() bool { return c.rung == captureLines }

// WantsLive lets the host enable the structural live events (and skip them
// otherwise). Checked by the trinket via an anonymous interface.
func (c *captureStream) WantsLive() bool { return c.rung == captureLive }

// The live-rung events, relayed from purfecterm's CaptureObserver and delegated
// to the screen mirror. Each is a no-op unless this is a live session; the
// format (full/plain/text) is applied inside the mirror. The Write text arrives
// verbatim; plain/text drop the colour by carrying no SGR into the document.
func (c *captureStream) LiveWrite(x, y int, text, sgr string) {
	if c.mirror != nil {
		c.mirror.Write(x, y, text, sgr)
	}
}
func (c *captureStream) LiveCursorMove(x, y int) {
	if c.mirror != nil {
		c.mirror.CursorMove(x, y)
	}
}
func (c *captureStream) LiveNewline(x, y int) {
	if c.mirror != nil {
		c.mirror.Newline(x, y)
	}
}
func (c *captureStream) LiveLineWrap(x, y int) {
	if c.mirror != nil {
		c.mirror.Newline(x, y) // a wrap advances a row exactly as a newline does
	}
}
func (c *captureStream) LiveBackspace(x, y int) {
	if c.mirror != nil {
		c.mirror.Backspace(x, y)
	}
}
func (c *captureStream) LiveScrollLineOff(n int) {
	if c.mirror != nil {
		c.mirror.ScrollLineOff(n)
	}
}
func (c *captureStream) LiveClearScreen(sgr string) {
	if c.mirror != nil {
		c.mirror.ClearScreen(sgr)
	}
}
func (c *captureStream) LiveClearEndOfLine(x, y int, sgr string) {
	if c.mirror != nil {
		c.mirror.ClearEndOfLine(x, y, sgr)
	}
}
func (c *captureStream) LiveClearBeginOfLine(x, y int, sgr string) {
	if c.mirror != nil {
		c.mirror.ClearBeginOfLine(x, y, sgr)
	}
}
func (c *captureStream) LiveClearLine(y int, sgr string) {
	if c.mirror != nil {
		c.mirror.ClearLine(y, sgr)
	}
}
func (c *captureStream) LiveClearEndOfScreen(x, y int, sgr string) {
	if c.mirror != nil {
		c.mirror.ClearEndOfScreen(x, y, sgr)
	}
}
func (c *captureStream) LiveClearBeginOfScreen(x, y int, sgr string) {
	if c.mirror != nil {
		c.mirror.ClearBeginOfScreen(x, y, sgr)
	}
}
func (c *captureStream) LiveDeleteChars(x, y, n int, sgr string) {
	if c.mirror != nil {
		c.mirror.DeleteChars(x, y, n, sgr)
	}
}
func (c *captureStream) LiveInsertChars(x, y, n int, sgr string) {
	if c.mirror != nil {
		c.mirror.InsertChars(x, y, n, sgr)
	}
}
func (c *captureStream) LiveEraseChars(x, y, n int, sgr string) {
	if c.mirror != nil {
		c.mirror.EraseChars(x, y, n, sgr)
	}
}

// flush emits whatever escape-carry remains — called once at session end so a
// trailing partial sequence is not lost.
func (c *captureStream) flush() {
	if len(c.carry) > 0 {
		if s := c.filter(nil, true); s != "" {
			c.cursor.InsertString(s, nil, false)
		}
	}
}

// filter applies the capture format to one chunk, carrying any incomplete
// trailing escape to the next call (unless atEnd). full keeps everything (no
// carry); plain drops SGR; text drops all escapes.
func (c *captureStream) filter(data []byte, atEnd bool) string {
	buf := data
	if len(c.carry) > 0 {
		buf = append(c.carry, data...)
		c.carry = nil
	}
	if c.format == captureFull {
		return string(buf) // nothing stripped, so nothing can straddle a boundary
	}
	if !atEnd {
		var carry []byte
		buf, carry = splitTrailingIncompleteEscape(buf)
		c.carry = carry
	}
	s := string(buf)
	if c.format == capturePlain {
		return sgrRE.ReplaceAllString(s, "")
	}
	return allEscRE.ReplaceAllString(s, "") // captureText
}

// splitTrailingIncompleteEscape returns b with any trailing partial escape
// sequence (one begun but not terminated within b) held back as carry, so the
// filters only ever see whole sequences.
func splitTrailingIncompleteEscape(b []byte) (complete, carry []byte) {
	i := bytes.LastIndexByte(b, 0x1b)
	if i < 0 || escapeComplete(b[i:]) {
		return b, nil
	}
	return b[:i], append([]byte(nil), b[i:]...)
}

// escapeComplete reports whether seq (which starts with ESC) is a terminated
// sequence. A lone ESC or an unterminated CSI/OSC is not; shorter forms are
// complete once their second byte has arrived.
func escapeComplete(seq []byte) bool {
	if len(seq) < 2 {
		return false // a lone trailing ESC
	}
	switch seq[1] {
	case '[': // CSI: ESC [ params … final in @–~
		for _, c := range seq[2:] {
			if c >= 0x40 && c <= 0x7e {
				return true
			}
		}
		return false
	case ']': // OSC: terminated by BEL or ST (ESC \)
		for j := 2; j < len(seq); j++ {
			if seq[j] == 0x07 || (seq[j] == 0x1b && j+1 < len(seq) && seq[j+1] == '\\') {
				return true
			}
		}
		return false
	default: // 2-byte C1 / ESC-intermediate: complete once the 2nd byte is here
		return true
	}
}

// PTYRequest is what mew asks the host for. Every field is data the host is
// free to reinterpret; none of it is a handle to anything.
type PTYRequest struct {
	// CWD is where the session should start, as a canonical URL
	// (file:///home/user/project). EMPTY for a buffer with no filename —
	// the host decides what that means, which for the root mew is the
	// user's home directory. mew deliberately does not guess.
	CWD string

	// Command NAMES what to run ("bash", "zsh"), and is not a path. A host
	// that wants to run /usr/local/bin/bash for "bash" may; a host that wants
	// to run a menu for it may too.
	Command string

	// Cols and Rows are the viewport's content size at the moment of the
	// request. The session is resized again whenever the viewport changes.
	Cols, Rows int

	// Shell asks the HOST for the user's login shell instead of naming a
	// program: what that is depends on the operating system and the user's own
	// account, and neither is knowable from here. Command is empty when it is
	// set, and Args (if any) are the shell's own arguments — which is also how
	// a program gets run through it, since `bash script.sh` is just bash with
	// an argument.
	Shell bool

	// Args are the child's own arguments, already separated from mew's by the
	// exec command line's parse (see execargs.go). Empty is the common case.
	// They are passed to the program verbatim: no shell, no globbing, no word
	// splitting — a host hands them straight to the process it starts.
	Args []string

	// Method names WHICH WAY the host should make the terminal, when a host
	// has more than one and they do not all work everywhere. Empty means the
	// host's default and is what everything asks for; anything else is data
	// the host defines, exactly like Command. mew attaches no meaning to it.
	//
	// It exists because "the platform has several ways to start a terminal and
	// the right one is not knowable from here" is a real situation, and the
	// alternative is a rebuild per attempt on a machine that may not be the
	// one being developed on.
	Method string

	// Stdin, Stdout, Stderr wire each of the child's three standard streams
	// INDEPENDENTLY, because a pty slave is just a file and each descriptor points
	// at one on its own — they are not all-or-nothing. The zero value
	// (StreamTerminal for all three) is the historical terminal session: one
	// pseudo-terminal carrying all three, Read draining it and Write reaching
	// stdin through the master. Setting a stream to StreamPipe gives that one its
	// own pipe instead, and they mix: a child can keep a pty STDOUT (so isatty is
	// true and it emits colour and honours termcaps) while its STDERR rides a
	// separate pipe mew can tee, and its STDIN is a pipe mew feeds and half-closes.
	//
	// This is the single process-spawning surface — the pipe flavors ride the same
	// PTYProvider and PTYSession; the capabilities a given wiring adds are the
	// optional interfaces below (PTYStderr when stderr is its own pipe,
	// PTYStdinCloser when stdin is a pipe), discovered by type assertion.
	Stdin, Stdout, Stderr StreamWiring
}

// StreamWiring says how ONE of a child's standard streams is connected
// (PTYRequest.Stdin/Stdout/Stderr). It is per-stream on purpose: keeping a pty
// on one descriptor while piping another is an ordinary, useful thing (a
// colourful stdout with a separately-captured stderr), not a special case.
type StreamWiring int

const (
	// StreamTerminal binds the stream to the session's pseudo-terminal — the
	// historical default. A child with all three on StreamTerminal is an ordinary
	// terminal session, stdout and stderr merged on the pty.
	StreamTerminal StreamWiring = iota
	// StreamPipe gives the stream its own ordinary pipe. A piped stdin can be
	// half-closed to signal EOF (PTYStdinCloser); a piped stdout is drained by
	// Read; a piped stderr, when stdout is not also on that same pipe, is drained
	// by ReadStderr (PTYStderr). A child with all three piped is a headless
	// filter — no pty, no VT translation, Resize a no-op.
	StreamPipe
)

// PTYSession is what the host hands back: purfecterm's PTY interface MINUS
// Start(*exec.Cmd). See the file comment for why that omission is the whole
// security argument.
//
// Read blocks until output arrives and returns an error once the session ends.
// Every method may be called from any goroutine.
type PTYSession interface {
	Read(p []byte) (n int, err error)
	Write(p []byte) (n int, err error)
	Resize(cols, rows int) error
	Close() error
}

// ptyViewportClass is the window class a viewport reports while its buffer runs
// a session, so [pty::mappings] and [pty::options] find it. It is the isearch
// prompt's trick — a class carrying a keymap refinement — for the keys a child
// process must receive as bytes rather than as edits.
const ptyViewportClass = "pty"

// PTYExitStatus is the OPTIONAL other half of a session: how the child ended.
// A host implements it on the session it returns when it can answer; mew asks
// by type assertion and says less when nobody can.
//
// It grants nothing — it is a readout, not a capability, and adding it does
// not widen what mew can ask a host to DO. What it buys is the difference
// between the two ways a terminal goes quiet: the child ran and exited (a
// code, however unhappy), or the byte stream ended underneath a child that is
// still alive (a broken session). Those look identical from mew's side of the
// pipe and want completely different debugging.
//
// exited is false when the child is still running, or when this host tracks
// no exit status at all.
type PTYExitStatus interface {
	ExitStatus() (code int, exited bool)
}

// PTYDiagnostics is the other optional readout: what the host did to build
// this session, step by step, in whatever terms its platform uses. Also a
// readout and not a capability.
//
// mew asks for it when a session ends having produced NOTHING, because that
// is the one failure mew can see and cannot explain: the account of which
// call went wrong is on the host's side of a pipe that never worked.
type PTYDiagnostics interface {
	Diagnostics() []string
}

// PTYStderr is the OPTIONAL separate-error-stream capability. A session whose
// stderr was wired to its own pipe (PTYRequest.Stderr == StreamPipe, and not
// sharing stdout's stream) implements it: ReadStderr drains the child's stderr
// independently of Read, so a filter's diagnostics can be told apart from its
// output. A session with stderr on the pseudo-terminal does NOT implement it —
// there stderr is inseparably merged into Read.
//
// Like the readouts above it is discovered by type assertion, so the base
// PTYSession is unchanged and no terminal host has to grow a method.
type PTYStderr interface {
	ReadStderr(p []byte) (n int, err error)
}

// PTYStdinCloser is the OPTIONAL stdin half-close. A session whose stdin was
// wired to a pipe (PTYRequest.Stdin == StreamPipe) implements it: CloseStdin
// closes just the child's input, signalling EOF so a filter that reads stdin to
// completion can flush and exit — WITHOUT the full teardown that Close performs
// (SIGHUP / TerminateProcess), which would kill the child before it finished. A
// stdin on the pseudo-terminal has no separable end to close and does not
// implement it.
type PTYStdinCloser interface {
	CloseStdin() error
}

// ptyState is one viewport's live session. It is keyed in ptySessions by the
// viewport it was launched in and draws its surface there; buf is the buffer
// that viewport held at launch, kept for the session's life because that is
// where its capture lands — insertion points stay tied to the buffer even
// though the terminal itself is bound to the viewport.
type ptyState struct {
	id      string         // stable for the session's life; names its child surface
	buf     *buffer.Buffer // the buffer it launched in; where captured output folds
	sess    PTYSession
	command string
	cwd     string // as asked for, for the record a failed session leaves
	method  string // which way the host was asked to make it (blank = default)
	// bytes counts what the child has produced, head keeps the beginning of
	// it, and started is when it began. A session that ends almost at once is
	// worth writing down whether or not it said anything, and WHAT it said is
	// the evidence: a terminal's own preamble looks nothing like a shell's
	// prompt, and telling those apart tells two faults apart.
	bytes   int
	head    []byte
	started time.Time
	// gridCols/gridRows are the surface's TRUE grid as declared by the host
	// (terminal_grid), zero until it says. mew places a surface by its
	// viewport's cell rectangle, but the host's display decides how many
	// columns of text actually fit inside that rectangle — a graphical
	// terminal spends some of its width on a scrollbar lane, and rounds the
	// rest down to whole cells of its own font. Resizing the child process to
	// the rectangle instead of to the grid tells the guest it has columns the
	// display will not show it: every full-width line then WRAPS, a wrapped
	// line SCROLLS, and a full-screen repaint that should have overwritten
	// the screen in place pushes a screenful into scrollback instead — on
	// every frame, without bound.
	gridCols, gridRows int
	// gridPxW/gridPxH are the child's window in PIXELS as of the last resize
	// we let through — the other half of what a PTY winsize carries, and the
	// half a child drawing pictures sizes itself from. Kept because the pixels
	// can move under an unchanged grid (a display density learned late, a zoom
	// step that gains no column), and a resize suppressed as a repeat leaves
	// such a child rendering to a window it no longer has. Zero until the
	// session reports one.
	gridPxW, gridPxH int
	// placedCols/placedRows are the rectangle the declaration was made
	// against. A declaration describes one rectangle; when the viewport is
	// RESIZED the old answer is stale, so it is dropped and the rectangle
	// governs again until the host has repainted and declared afresh. A
	// viewport that merely MOVED keeps its declaration — same size, same
	// answer, and re-deriving it would resize the child for nothing.
	placedCols, placedRows int
	// policy is how this session's LOGICAL size is governed (--size/--minimum/
	// --hidden); the zero value follows the visible tile, exactly as before.
	// logSentCols/logSentRows are the last logical size handed to the host, so
	// SetLogicalSize is pushed only when it actually changes.
	policy                   ptySizePolicy
	logSentCols, logSentRows int
	// capture is the resolved fidelity rung (never captureUnset here — the
	// default is applied before attach); format is how much escape detail it
	// keeps. captureFinal folds the transcript in ptyEnded; captureOff folds
	// nothing.
	capture captureRung
	format  captureFormat
	// outCursor is where a captured transcript lands: an ephemeral cursor pinned
	// at the caret the session was launched from, created only when capturing.
	// Held for the session's life (garland keeps it valid across any edits made
	// to the buffer meanwhile) and released in ptyEnded. nil when not capturing.
	outCursor *buffer.Cursor
	// stream folds a streaming rung's (raw/lines/live) live output in at
	// outCursor; nil for off and for final (which folds once at death). Its
	// SetCaptureSink registration is cleared, and its carry flushed, in ptyEnded.
	stream *captureStream
	// hidden runs the terminal under the hood: no visible surface is published
	// and the buffer is an ordinary editable document (its paint is NOT
	// suppressed, input edits it, not the child). Output still reaches it via
	// capture. Set from the --hidden switch and toggled at runtime by
	// viewport_pty_hide / _show / _toggle.
	hidden bool
	// keyPressedToChild names the keys whose PRESS this child actually
	// received, so a release can be matched to one.
	//
	// A press reaches the child only if mew's keymap did not claim it first: a
	// bound chord runs its command and stops there. A release skips the keymap
	// entirely, and must (see dispatchKey — no binding is written against one,
	// and the sequence processor would count it as the next key of a
	// sequence). Those two rules together handed the child a key-up for a key
	// it was never told about — the same mismatch as a key-down with no
	// key-up, wearing the other face. Every chord mew binds arrived at a
	// hosted browser as a release out of nowhere.
	//
	// Keyed by the name the press arrived under, which is the name its release
	// carries back: direct-key-handler names a release for its press, so ^B
	// comes up "^B:Release" even if Control was let go first. The matching
	// release consumes the entry, so a key held across a change of focus
	// leaves at most one stale name behind, and the session's death takes the
	// rest.
	keyPressedToChild map[string]bool
}

// streams reports whether a rung captures live through the CaptureSink seam
// (raw/lines/live), as opposed to off or the final at-death snapshot.
func (r captureRung) streams() bool {
	return r == captureRaw || r == captureLines || r == captureLive
}

// ptySizePolicy is how a session's LOGICAL size is chosen — the size the child
// is told it has, which the --size/--minimum/--hidden switches set and which is
// otherwise the visible tile's. It rides from the exec/shell parse (execSpec)
// onto the session's ptyState, where the resize path consults it every frame.
type ptySizePolicy struct {
	mode       sizeMode
	cols, rows int
	hidden     bool
}

// resolveLogical is the concrete logical size for a session shown at visCols x
// visRows: the visible size for a follow policy, the pinned size for --size, and
// the larger of the two per axis for --minimum (a floor that still grows with
// the tile).
func (p ptySizePolicy) resolveLogical(visCols, visRows int) (cols, rows int) {
	switch p.mode {
	case sizeExact:
		// A pinned axis of 0 means "follow the tile on this axis" (e.g. 80x0 pins
		// width, height tracks the surface). The child still needs a concrete
		// number, so substitute the visible size where the pin is 0.
		c, r := p.cols, p.rows
		if c == 0 {
			c = visCols
		}
		if r == 0 {
			r = visRows
		}
		return c, r
	case sizeMinimum:
		return max(visCols, p.cols), max(visRows, p.rows)
	default:
		return visCols, visRows
	}
}

// hostLogical is what SetLogicalSize is told: 0,0 for a follow policy (the host
// tracks the visible surface, as before) and the resolved logical size
// otherwise. The child always needs a concrete size, while the host wants
// "follow" spelled as zero — so the two are computed distinctly.
func (p ptySizePolicy) hostLogical(visCols, visRows int) (cols, rows int) {
	switch p.mode {
	case sizeFollow:
		return 0, 0
	case sizeExact:
		// Hand purfecterm the pinned axes verbatim, 0s intact: it reads a logical 0
		// as "follow the physical dimension", so an 80x0/x24 tracks the tile on the
		// free axis dynamically rather than freezing a snapshot of it. (The child,
		// by contrast, got a concrete size from resolveLogical.)
		return p.cols, p.rows
	default: // sizeMinimum: both axes are concrete floors already
		return p.resolveLogical(visCols, visRows)
	}
}

// ptyHeadMax is how much of a short session's output is kept for the record.
// Enough for a preamble and a prompt; not a transcript.
const ptyHeadMax = 512

// ptyShortLife is how quickly a session has to end to be worth a record. A
// shell someone opened to work in does not finish in this long.
const ptyShortLife = 3 * time.Second

// TerminalSurface is one visible terminal: which session, and exactly where it
// sits in mew's OWN surface, in 1-based terminal cells. The rectangle is the
// viewport's text area — the same cells the document would have drawn into.
//
// Republished as a complete SET after every render. A session absent from the
// set is not visible this frame (scrolled away, its viewport gone, another
// buffer showing) and its surface should be hidden. Sending the whole set
// rather than deltas is deliberate: there is no incremental bookkeeping on the
// host to fall out of step with mew's layout.
type TerminalSurface struct {
	ID string

	// Col, Row, Width, Height place the terminal's GRID: where its origin
	// sits and how many cells it spans. 1-based cells.
	Col, Row      int
	Width, Height int

	// ClipCol, ClipRow, ClipWidth, ClipHeight bound what is actually VISIBLE
	// of that grid — normally the same rectangle, but smaller when the
	// viewport is partially obscured, partially scrolled off, or narrower than
	// the grid it hosts. Where they differ, the surface still draws at its own
	// origin and only the intersection reaches the screen, so a terminal never
	// bleeds over the chrome around it.
	//
	// A zero ClipWidth or ClipHeight means nothing of this surface is visible
	// this frame.
	ClipCol, ClipRow      int
	ClipWidth, ClipHeight int

	// Primary marks the ONE surface per session that OWNS the emulator: its
	// rectangle sizes the grid (the child's cols/rows follow it) and the host
	// hosts the live child there. A session can be shown in several tiles
	// (tiles↔viewports is many-to-many); the others are MIRRORS — the same grid
	// painted read-only into their rectangle, never resizing it. The primary is
	// the session's canonical/focused tile, so its sizing is locked to the pane
	// the user works in, independent of a mirror's size.
	Primary bool

	// Focused marks the ONE surface whose viewport currently holds mew's
	// focus (always also the Primary). That surface owns the platform's text
	// caret — its position and its DECSCUSR shape both come from the child
	// process, not from mew's own caret, because while you are typing at a shell
	// the cursor you are watching is the shell's. mew keeps keyboard focus
	// regardless, so its keymap still runs; only the drawn caret is ceded.
	Focused bool
}

// TerminalMouseAction and TerminalMouseButton name one mouse event without
// naming an encoding. Which bytes a terminal application actually receives —
// whether it receives any at all — depends on the tracking and encoding modes
// that application turned on, and that state lives in the emulator consuming
// the output stream, not here. So mew reports WHAT HAPPENED and the host,
// which owns the emulator, answers with the bytes.
type TerminalMouseAction int

const (
	TerminalMousePress TerminalMouseAction = iota
	TerminalMouseRelease
	TerminalMouseMotion // a drag (Button set) or bare motion (ButtonNone)
	TerminalMouseScrollUp
	TerminalMouseScrollDown
	TerminalMouseScrollLeft
	TerminalMouseScrollRight
)

type TerminalMouseButton int

const (
	TerminalMouseButtonNone TerminalMouseButton = iota
	TerminalMouseButtonLeft
	TerminalMouseButtonMiddle
	TerminalMouseButtonRight
)

// TerminalMouse is one mouse event addressed to a terminal surface, in that
// surface's OWN 1-based cells — so the host never has to know where mew put
// the grid, and mew never has to know how the child reads it.
//
// The modifiers are the real ones. mew's own handling folds ctrl/alt/super
// onto a left click into a right click (terminals vary wildly in which
// modified clicks they deliver at all), but a child process asked for the
// mouse and gets it as it happened.
type TerminalMouse struct {
	Col, Row         int
	Action           TerminalMouseAction
	Button           TerminalMouseButton
	Shift, Alt, Ctrl bool
}

// TerminalHooks is how a host renders mew's terminal sessions. mew does not
// emulate a terminal: it hands the raw bytes over and says where to draw.
//
// The host creates one real terminal surface per session — for the KittyTK
// host, a child PurfecTerm trinket inside the editor trinket — feeds it, and
// positions it from Place. mew keeps focus throughout, so the child needs no
// input events: keystrokes go through mew's keymap to pty_send and out via the
// session's Write. The child is a display only.
//
// Every hook is called on mew's main loop.
type TerminalHooks struct {
	Open func(id string, cols, rows int)

	// SetLogicalSize tells the host a session's LOGICAL size: the size the
	// child is told it has, independent of the visible tile. cols,rows of 0
	// mean "follow the visible surface" — what every session did before the
	// --size/--minimum/--hidden switches, so a host that leaves this hook nil
	// still works. When the logical size exceeds the visible grid the terminal
	// shows scrollbars rather than reflowing the child; it is also how a session
	// with no visible surface keeps a definite size.
	SetLogicalSize func(id string, cols, rows int)

	// Snapshot returns the session's scrollback as text, for folding it into the
	// buffer when the session ends (capture-on-die). ansi keeps the full
	// escape/SGR stream (colour, layout); otherwise it is plain text with the
	// sequences stripped. Optional: a nil hook simply means no capture.
	Snapshot func(id string, ansi bool) string

	// Feed hands the session's output to the emulator and returns what the
	// emulator ANSWERED — a terminal is not a one-way device. A program asks
	// it questions in the output stream (DSR "where is the cursor", DA "what
	// are you") and the emulator's replies are input the program then blocks
	// waiting for. They return here, synchronously, and mew writes them to
	// the session before anything else happens — not through the event queue,
	// which is the main loop's own plumbing and must never be posted to FROM
	// the main loop: a full queue there is the loop waiting on itself.
	Feed  func(id string, p []byte) (reply []byte)
	Place func(surfaces []TerminalSurface)
	Close func(id string)

	// Mouse offers one mouse event to the surface's terminal and reports what
	// became of it: bytes for the child process, and whether the terminal
	// took the event at all.
	//
	// Two answers rather than one, because there are three outcomes. An
	// application TRACKING the mouse wants a report, so bytes come back and
	// mew writes them to the session — every byte a child receives still
	// leaves by the one door, exactly as keystrokes do. An application NOT
	// tracking it leaves the mouse to the terminal itself, which has its own
	// uses for it — a scrollbar, scrollback, selection — so the host handles
	// it locally and says handled with no bytes. And an event the terminal
	// wants nothing to do with reports neither, and falls through to mew.
	Mouse func(id string, ev TerminalMouse) (data []byte, handled bool)

	// Key asks what one KEY means to the surface's terminal — the bytes a
	// child process would receive for it — and returns nil for a name that
	// encodes to nothing. The name is mew's own (see keyverbose.go): "^C",
	// "esc", "back", "fdel", "pgup", "M-x", "S-tab", "F5", or the character
	// itself.
	//
	// Same division as Mouse, for the same reason: what a key becomes on the
	// wire is the terminal's business (application cursor keys, keypad mode,
	// the encoding its front end has always used), and mew is not an emulator.
	// It is used for raw_key_input, the escape hatch for keys mew binds and
	// would otherwise swallow; ordinary typing needs none of it, because
	// insert and insert_newline already route themselves.
	Key func(id string, key string) []byte

	// SetCaptureSink registers where a session's live output events are
	// delivered, or nil to stop. mew calls it when a session on a streaming
	// capture rung (raw today; lines/live later) starts and ends; the host
	// relays purfecterm's CaptureObserver to the sink. Calls arrive on the main
	// loop (synchronously inside Feed), so a sink may edit its buffer directly.
	// Optional: a nil hook means the streaming rungs produce nothing.
	SetCaptureSink func(id string, sink CaptureSink)
}

// CaptureSink receives a capturing session's output events, relayed by the host
// from purfecterm's CaptureObserver in mew-native terms. See
// TerminalHooks.SetCaptureSink.
type CaptureSink interface {
	// Output is a chunk of the session's raw output (the `raw` rung), verbatim
	// as purfecterm received it. The slice may be reused after the call.
	Output(data []byte)

	// LineOff is one line of the ordered transcript (the `lines` rung), already
	// serialized to a self-contained ANSI string by the host (no trailing
	// newline).
	LineOff(ansLine string)

	// The live rung's structural screen events, relayed from purfecterm's
	// CaptureObserver. Coordinates are cells in the terminal's live geometry,
	// already clamped by the emulator. LiveWrite's sgr is an absolute
	// 0;-prefixed SGR (or "" for the default pen); the other events carry the
	// post-move caret position. A non-live sink ignores them.
	LiveWrite(x, y int, text, sgr string)
	LiveCursorMove(x, y int)
	LiveNewline(x, y int)
	LiveLineWrap(x, y int)
	LiveBackspace(x, y int)
	LiveScrollLineOff(n int)
	// The erase events, each carrying the current pen whose background fills the
	// erased cells; the directional ones carry the cursor position.
	LiveClearScreen(sgr string)                  // whole screen
	LiveClearEndOfLine(x, y int, sgr string)     // (x,y) to right margin
	LiveClearBeginOfLine(x, y int, sgr string)   // line start through (x,y)
	LiveClearLine(y int, sgr string)             // the whole line
	LiveClearEndOfScreen(x, y int, sgr string)   // (x,y) to end of screen
	LiveClearBeginOfScreen(x, y int, sgr string) // screen start through (x,y)
	// The in-line character ops: delete/insert/erase n cells at (x,y).
	LiveDeleteChars(x, y, n int, sgr string)
	LiveInsertChars(x, y, n int, sgr string)
	LiveEraseChars(x, y, n int, sgr string)
}

// bufferCWD is the directory a session for this buffer should start in, as a
// canonical URL — or "" when the buffer has no filename. Empty is a real
// answer, not a failure: the host owns the meaning of "nowhere in particular".
func (e *Editor) bufferCWD(b *buffer.Buffer) string {
	if b == nil {
		return ""
	}
	fn := b.GetFilename()
	if fn == "" {
		return ""
	}
	dir := filepath.Dir(fn)
	if dir == "" || dir == "." {
		return ""
	}
	return e.canonicalDocURL(dir)
}

// ptySessionFor returns the live session bound to a viewport, or nil. It sees a
// hidden session too: this is the EXISTENCE check (the start guard, cleanup, the
// hide/show commands' target).
func (e *Editor) ptySessionFor(w *viewport.Viewport) PTYSession {
	if w == nil {
		return nil
	}
	e.ptyMu.Lock()
	defer e.ptyMu.Unlock()
	if st := e.ptySessions[w]; st != nil {
		return st.sess
	}
	return nil
}

// visibleSessionFor is ptySessionFor restricted to a session the user is
// INTERACTING with: a hidden session (terminal under the hood, buffer an
// ordinary editable document) reports nil, so painting, input routing, the pty
// keymap and the caret all treat that viewport as normal. Existence checks that
// must see a hidden session — the start guard, cleanup, the hide/show commands —
// use ptySessionFor instead.
func (e *Editor) visibleSessionFor(w *viewport.Viewport) PTYSession {
	if w == nil {
		return nil
	}
	e.ptyMu.Lock()
	defer e.ptyMu.Unlock()
	if st := e.ptySessions[w]; st != nil && !st.hidden {
		return st.sess
	}
	return nil
}

// setViewportPTYHidden sets the focused session's hidden flag — mode > 0 hide,
// < 0 show, 0 toggle — and repaints. Backs viewport_pty_hide / _show / _toggle,
// so a hidden terminal can be revealed part-way through its task or a visible
// one tucked under the hood. Reports false with a warning when the focused
// buffer has no session, so a key bound to one of these says so rather than
// doing nothing silently.
func (e *Editor) setViewportPTYHidden(mode int, cmd string) bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		e.ShowWarning(cmd + ": no active buffer")
		return false
	}
	e.ptyMu.Lock()
	st := e.ptySessions[w]
	if st == nil {
		e.ptyMu.Unlock()
		e.ShowWarning(cmd + ": this viewport has no terminal session")
		return false
	}
	switch {
	case mode > 0:
		st.hidden = true
	case mode < 0:
		st.hidden = false
	default:
		st.hidden = !st.hidden
	}
	e.ptyMu.Unlock()
	// The render path republishes surfaces (a now-hidden session drops out of
	// the set, a now-shown one returns) and re-asks the content-suppressor.
	e.RequestRender()
	return true
}

// killViewportPTY ends the focused viewport's session by CLOSING it — the read
// loop then stops and ptyEnded runs, so a `final` capture folds everything the
// session produced up to this point. Backs viewport_pty_kill.
//
// Closing the PTY is the same cross-platform teardown the editor uses at
// shutdown (closePTYSessions): the host's Close drops the forkpty master (a
// SIGHUP to the child on unix) or the conPTY handle on Windows. mew holds no
// process handle to signal, by design, so this is the kill it has — and it is
// enough for the shells and programs a terminal runs. Works on a hidden session
// too; warns and reports false when the focused buffer has no session.
func (e *Editor) killViewportPTY() bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		e.ShowWarning("viewport_pty_kill: no active buffer")
		return false
	}
	sess := e.ptySessionFor(w) // existence: a hidden session can be killed too
	if sess == nil {
		e.ShowWarning("viewport_pty_kill: this viewport has no terminal session")
		return false
	}
	_ = sess.Close()
	return true
}

// endSessionForRemovedViewport closes the session bound to a viewport that has
// been TRULY removed (its tile torn off / closed, not merely re-tiled), so the
// read loop ends and ptyEnded folds any final capture and tears the surface
// down through the one teardown path. A removed viewport takes its terminal
// with it: the session is not re-homed to another viewport over the same
// buffer, even when one exists — killing is the accepted behaviour.
//
// The map entry is left for ptyEnded to delete, so a session ending on its own
// at the same moment cannot be torn down twice. Called from the
// EventViewportRemoved handler, already marshalled onto the main loop.
func (e *Editor) endSessionForRemovedViewport(w *viewport.Viewport) {
	if w == nil {
		return
	}
	e.ptyMu.Lock()
	st := e.ptySessions[w]
	e.ptyMu.Unlock()
	if st == nil {
		return // no session here, or it already ended
	}
	_ = st.sess.Close()
}

// ptyDiagnose is the pty_diag command: ask the host to test its own terminal
// plumbing and write the account into the buffer at the caret.
//
// A terminal that will not start is the hardest thing in this system to see
// into, because everything mew can observe is on the far side of a pipe that
// is not working. Every fact worth having — which shell was found, whether
// the platform's console call succeeded, what it said if it did not, whether
// a probe child's bytes ever came back — lives on the HOST, where the pipe is
// made. So mew does not investigate. It asks, and puts the answer somewhere
// the person debugging can read, scroll, and save.
func (e *Editor) ptyDiagnose() bool {
	if e.Config.PTYDiagnose == nil {
		e.ShowWarning("This host offers no terminal diagnostics")
		return false
	}
	report := e.Config.PTYDiagnose()
	if strings.TrimSpace(report) == "" {
		e.ShowWarning("The host's terminal diagnostics said nothing")
		return false
	}
	if !strings.HasSuffix(report, "\n") {
		report += "\n"
	}
	// Written to a FILE as well as into the buffer, and named in both. Reading
	// it on screen means scrolling a terminal that may be the very thing
	// misbehaving, and the person who most needs this report is the one least
	// able to get it off the machine by hand.
	saved, err := e.appendPTYLog(report)
	if err == nil {
		report = "(also saved to " + saved + ")\n" + report
	} else {
		saved = ""
		report = "(could not save a log file: " + err.Error() + ")\n" + report
	}
	e.insertText(report)
	e.trackEdit()
	if saved != "" {
		e.ShowNotification("Terminal diagnostics written here and to " + saved)
	} else {
		e.ShowNotification("Terminal diagnostics written at the caret")
	}
	return true
}

// ptyLogName is where terminal trouble is written down, in the working
// directory — one file, whether the record wrote itself when a session came
// up empty or pty_diag was asked for a full report.
const ptyLogName = "mew-pty-diag.log"

// appendPTYLog adds one entry to that file and returns the path it used.
// APPENDS, because the interesting case is several attempts in a row: a
// failed exec, then a diagnostic run, then another attempt, all wanting to
// travel together as one thing to send.
func (e *Editor) appendPTYLog(entry string) (string, error) {
	prior, _ := e.FS.ReadFile(ptyLogName)
	var b strings.Builder
	b.Write(prior)
	if len(prior) > 0 && !strings.HasSuffix(string(prior), "\n") {
		b.WriteString("\n")
	}
	b.WriteString("---- ")
	b.WriteString(time.Now().Format("2006-01-02 15:04:05"))
	b.WriteString(" ----\n")
	b.WriteString(entry)
	if !strings.HasSuffix(entry, "\n") {
		b.WriteString("\n")
	}
	if err := e.FS.WriteFile(ptyLogName, []byte(b.String())); err != nil {
		return "", err
	}
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, ptyLogName), nil
	}
	return ptyLogName, nil
}

// execRequest asks the host for a session and binds it to the focused
// buffer. Denial is an ordinary outcome, reported and survivable: the buffer
// stays exactly what it was.
func (e *Editor) execRequest(command, method string) bool {
	return e.execRequestArgs(command, nil, method)
}

// execRequestArgs is execRequest with the child's own arguments.
func (e *Editor) execRequestArgs(command string, args []string, method string) bool {
	return e.execRequestSpec(command, args, method, false)
}

// execRequestShell asks for the user's LOGIN SHELL, whatever that is: mew names
// no program and the host resolves one. Args, if any, are the shell's own.
func (e *Editor) execRequestShell(args []string, method string) bool {
	return e.execRequestSpec("", args, method, true)
}

// execRequestArgsPolicy and execRequestShellPolicy are execRequestArgs and
// execRequestShell carrying a LOGICAL-size policy from the parse. The plain
// forms stay as the zero-policy (follow-the-tile) entry points every existing
// caller uses, delegating here.
func (e *Editor) execRequestArgsPolicy(command string, args []string, method string, pol ptySizePolicy, capture captureRung, format captureFormat) bool {
	return e.execRequestSpecPolicy(command, args, method, false, pol, capture, format)
}

func (e *Editor) execRequestShellPolicy(args []string, method string, pol ptySizePolicy, capture captureRung, format captureFormat) bool {
	return e.execRequestSpecPolicy("", args, method, true, pol, capture, format)
}

// execRequestSpec is the zero-option form of execRequestSpecPolicy: the child's
// logical size follows the visible tile and nothing is captured, exactly as
// before these switches. It passes an explicit captureOff (not unset): these
// internal entry points opt out of any future default.
func (e *Editor) execRequestSpec(command string, args []string, method string, shell bool) bool {
	return e.execRequestSpecPolicy(command, args, method, shell, ptySizePolicy{}, captureOff, captureFull)
}

// resolveCapture maps an unspecified rung to the configured default. There is no
// default today, so unset means off; this is the one seam a future
// [options] capture=... would change.
func (e *Editor) resolveCapture(r captureRung) captureRung {
	if r == captureUnset {
		return captureOff
	}
	return r
}

// execRequestSpecPolicy is execRequestArgs with the shell flag, a logical-size
// policy, a capture rung and format: when shell is set, the host names the
// program (the user's login shell) and command must be empty. pol governs the
// size the child is told it has (see ptySizePolicy); capture/format govern
// whether and how its output is folded into the buffer.
func (e *Editor) execRequestSpecPolicy(command string, args []string, method string, shell bool, pol ptySizePolicy, capture captureRung, format captureFormat) bool {
	capture = e.resolveCapture(capture)
	if e.Config.PTYProvider == nil {
		e.ShowWarning("No terminal provider: this host does not grant sessions")
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		e.ShowWarning("No active buffer")
		return false
	}
	// A registered wiki may refuse to host a terminal, and says why (see
	// wikiDef.DeclineExec). Asked BEFORE anything else about the buffer, because
	// WHERE the request came from is the most specific thing that can be wrong
	// with it, and the most useful thing to be told: the alternative is the host
	// failing later on a working directory the reader never chose and cannot see.
	if why := wikiDeclineExec(w.WikiName); why != "" {
		e.ShowWarning(degenerateToASCII(why))
		return false
	}
	// One session per VIEWPORT, not per buffer: a second viewport over the same
	// buffer (a clone, a split showing a different ref) may run its own terminal,
	// and its capture still folds into the shared buffer at its own launch caret.
	if e.ptySessionFor(w) != nil {
		e.ShowWarning("This viewport already has a session")
		return false
	}

	visCols, visRows := w.ContentWidth, w.ContentHeight
	if visCols <= 0 {
		visCols = 80
	}
	if visRows <= 0 {
		visRows = 24
	}
	// The child is told its LOGICAL size from the outset: the visible grid for a
	// follow policy (unchanged), the pinned size for --size, a floored size for
	// --minimum. attachPTY's SetLogicalSize then tells the host to render that
	// many cells, with scrollbars when they exceed the tile.
	cols, rows := pol.resolveLogical(visCols, visRows)
	req := PTYRequest{
		CWD:     e.bufferCWD(w.Buffer),
		Command: strings.TrimSpace(command),
		Args:    args,
		Shell:   shell,
		Method:  strings.TrimSpace(method),
		Cols:    cols,
		Rows:    rows,
	}
	if req.Shell {
		// The two are exclusive by construction: a host asked for the login
		// shell must not also be handed a program name to prefer.
		req.Command = ""
	}

	sess, err := e.Config.PTYProvider(req)
	if err != nil || sess == nil {
		msg := "Host refused the session"
		if err != nil {
			msg = "Cannot execute: " + err.Error()
		}
		e.ShowWarning(msg)
		return false
	}

	// The host chose the shell, so say what was ASKED for rather than guessing a
	// name mew does not know — including in the session record, whose whole
	// purpose is an honest account of the request.
	name := req.Command
	if req.Shell {
		name = "the login shell"
	}
	// A capturing session lands its transcript where the user launched it: pin an
	// ephemeral cursor at the current caret now, while the viewport is known and
	// its caret is where they meant. garland keeps it valid until ptyEnded folds
	// there — surviving the surface teardown and any edit made to the buffer in
	// between — which reading the caret back at death could not promise.
	var outCursor *buffer.Cursor
	if capture != captureOff {
		pos := w.CursorPos()
		outCursor = w.Buffer.NewEphemeralCursor()
		outCursor.SeekLineRune(pos.Line, pos.Rune)
	}
	// The editing caret is handed to the live rung so its Method 4 setup can park
	// it on "end of terminal" and let it ride the output tail.
	e.attachPTY(w, sess, name, req.CWD, req.Method, cols, rows, pol, capture, format, outCursor)
	started := "Started " + name
	if len(req.Args) > 0 {
		started += " " + strings.Join(req.Args, " ")
	}
	if req.Method != "" {
		started += " (method " + req.Method + ")"
	}
	e.ShowNotification(started)
	return true
}

// attachPTY binds a session to a VIEWPORT and starts pumping its output. The
// session draws its surface wherever that viewport is tiled; its capture folds
// into the viewport's buffer (w.Buffer, remembered as ptyState.buf) at the
// launch caret. The live rung is handed w.Caret so its Method 4 setup can park
// the editing caret on "end of terminal" and let it ride the output tail.
func (e *Editor) attachPTY(w *viewport.Viewport, sess PTYSession, command, cwd, method string, cols, rows int, pol ptySizePolicy, capture captureRung, format captureFormat, outCursor *buffer.Cursor) {
	b := w.Buffer
	// The initial logical size the host is told, taken straight from the policy so
	// a pinned or floored terminal renders correctly from its first frame while a
	// follow policy — or a free axis of an 80x0/x24 pin — is spelled as 0 for the
	// host to track. Recorded as logSent so the steady-state resize loop only
	// re-sends it on a change.
	lc, lr := pol.hostLogical(cols, rows)
	e.ptyMu.Lock()
	if e.ptySessions == nil {
		e.ptySessions = make(map[*viewport.Viewport]*ptyState)
	}
	e.ptySeq++
	id := fmt.Sprintf("pty%d", e.ptySeq)
	// A streaming rung folds output in live through a sink; final and off do not.
	var stream *captureStream
	if capture.streams() && outCursor != nil {
		stream = &captureStream{rung: capture, cursor: outCursor, format: format}
		if capture == captureLive {
			stream.mirror = newLiveMirror(b, outCursor, format, w.Caret)
		}
	}
	e.ptySessions[w] = &ptyState{
		id: id, buf: b, sess: sess, command: command, cwd: cwd, method: method,
		started: time.Now(), policy: pol, logSentCols: lc, logSentRows: lr,
		capture: capture, format: format, outCursor: outCursor, stream: stream,
		hidden: pol.hidden,
	}
	e.ptyMu.Unlock()

	if e.Config.TerminalSurfaces.Open != nil {
		e.Config.TerminalSurfaces.Open(id, cols, rows)
	}
	if e.Config.TerminalSurfaces.SetLogicalSize != nil {
		e.Config.TerminalSurfaces.SetLogicalSize(id, lc, lr)
	}
	// Register the sink before the read loop starts, so the first byte the host
	// relays has somewhere to land.
	if stream != nil && e.Config.TerminalSurfaces.SetCaptureSink != nil {
		e.Config.TerminalSurfaces.SetCaptureSink(id, stream)
	}

	// The read loop is the session's own goroutine; every delivery marshals
	// onto the editor main loop through PostAction, with exactly the safety of
	// a keystroke. A read that returns nothing but an error ends the session.
	go func() {
		buf := make([]byte, 4096)
		var last error
		for {
			n, err := sess.Read(buf)
			if n > 0 {
				chunk := append([]byte(nil), buf[:n]...)
				e.PostAction(func() { e.ptyOutput(w, chunk) })
			}
			if err != nil {
				last = err
				break
			}
		}
		// The error that ended the stream travels with the ending: a session
		// that stopped because something broke should say so, and only a
		// clean end of file is silent.
		e.PostAction(func() { e.ptyEnded(w, last) })
	}()
}

// ptyOutput hands one chunk of session output to the host, verbatim.
//
// mew does no terminal emulation and no filtering: PurfecTerm on the other side
// IS an emulator, so raw bytes are exactly what it wants — full-screen cursor
// motion, colour, alternate screen and all. Anything mew stripped here would be
// a capability the child surface then lacked.
//
// The buffer behind a running session is an ordinary garland document whose own
// text PAINTING is suppressed while the terminal surface overlays it (the
// content-suppressor predicate the renderer asks about). Its content is left
// untouched here; capture-on-die (Method 2) folds the session's transcript into
// it at the output cursor when it ends, and live per-line tracking is the
// follow-up. Neither happens on this hot path.
func (e *Editor) ptyOutput(w *viewport.Viewport, chunk []byte) {
	if w == nil || len(chunk) == 0 || e.Config.TerminalSurfaces.Feed == nil {
		return
	}
	e.ptyMu.Lock()
	st := e.ptySessions[w]
	e.ptyMu.Unlock()
	if st == nil {
		return // the session ended between the read and this delivery
	}
	e.ptyMu.Lock()
	st.bytes += len(chunk)
	if n := ptyHeadMax - len(st.head); n > 0 {
		if n > len(chunk) {
			n = len(chunk)
		}
		st.head = append(st.head, chunk[:n]...)
	}
	e.ptyMu.Unlock()
	if reply := e.Config.TerminalSurfaces.Feed(st.id, chunk); len(reply) > 0 {
		// The emulator answered a query in this chunk. The child is blocked
		// waiting for exactly these bytes.
		if _, err := st.sess.Write(reply); err != nil {
			e.ShowWarning("Session write failed: " + err.Error())
		}
	}
}

// notifyTerminalSurfaces republishes where every VISIBLE session should draw.
//
// Called after each render with the frame's geometry already set, like
// notifyPointerRegion — but a set rather than one rectangle, because several
// viewports can be running terminals at once. Pushed only when the set changes,
// so an idle frame costs nothing.
func (e *Editor) notifyTerminalSurfaces() {
	if e.Config.TerminalSurfaces.Place == nil {
		return
	}
	e.ptyMu.Lock()
	live := len(e.ptySessions)
	byVP := make(map[*viewport.Viewport]string, live)
	for vp, st := range e.ptySessions {
		if st.hidden {
			continue // running under the hood: no visible surface to publish
		}
		byVP[vp] = st.id
	}
	e.ptyMu.Unlock()
	if live == 0 && len(e.terminalSurfacesSent) == 0 {
		return
	}

	focused := e.ViewportManager.GetFocusedViewport()

	// One surface PER TILE of a PTY viewport: a session shown in several tiles
	// (tiles↔viewports is many-to-many) draws in each. Exactly one is the PRIMARY
	// — the canonical/focused tile — which owns the emulator and its sizing; the
	// rest are read-only mirrors. rectFor reads THIS tile's geometry (not the
	// viewport's single-valued fields, which hold only the canonical tile).
	type place struct {
		id             string
		vp             *viewport.Viewport
		col, row, w, h int
		focusedTile    bool
	}
	var places []place
	// primaryIdx[id] is the index into places of that session's primary surface:
	// the focused tile when it has one, else the last on-screen tile.
	primaryIdx := map[string]int{}
	seen := map[string]bool{}
	consider := func(id string, vp *viewport.Viewport, col, row, w, h int, focusedTile bool) {
		idx := len(places)
		places = append(places, place{id, vp, col, row, w, h, focusedTile})
		if _, ok := primaryIdx[id]; !ok || focusedTile {
			primaryIdx[id] = idx
		}
	}

	// ON SCREEN THIS FRAME, and the session's OWN viewport: a session draws only
	// where the viewport it was launched in is tiled — a tile-split (same viewport
	// ref) mirrors it, but a cloned viewport over the same buffer is a different
	// pointer and shows plain document text. A viewport swapped to another buffer,
	// hidden, or stacked behind another keeps its last layout, and a surface from
	// that stale geometry paints over whatever took its place.
	for i := range e.mainTiles {
		t := &e.mainTiles[i]
		w := t.Viewport
		if w == nil || !e.viewportOnScreen(w) || t.ContentWidth <= 0 || t.ContentHeight <= 0 {
			continue
		}
		if id, ok := byVP[w]; ok {
			seen[id] = true
			consider(id, w, t.ContentX+1, t.ContentY+1, t.ContentWidth, t.ContentHeight, t.Focused)
		}
	}
	// A PTY viewport not in the tiled main area (docked chrome) is single-tile:
	// publish it from its own geometry.
	for _, w := range e.ViewportManager.AllViewports() {
		if !e.viewportOnScreen(w) || w.ContentWidth <= 0 || w.ContentHeight <= 0 {
			continue
		}
		id, ok := byVP[w]
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		consider(id, w, w.ContentX+1, w.ContentY+1, w.ContentWidth, w.ContentHeight, w == focused)
	}

	var surfaces []TerminalSurface
	for idx, pl := range places {
		primary := primaryIdx[pl.id] == idx
		// The grid fills the tile's text area, and for now the clip is that same
		// rectangle: mew's own layout already excludes the chrome. Separate fields
		// so a future partially-obscured tile can narrow the clip without moving
		// the grid.
		surfaces = append(surfaces, TerminalSurface{
			ID:         pl.id,
			Col:        pl.col,
			Row:        pl.row,
			Width:      pl.w,
			Height:     pl.h,
			ClipCol:    pl.col,
			ClipRow:    pl.row,
			ClipWidth:  pl.w,
			ClipHeight: pl.h,
			Primary:    primary,
			Focused:    primary && pl.vp == focused,
		})
	}
	// Stable order (id, then position) so the change check and the host's set
	// handling are deterministic across frames.
	sort.Slice(surfaces, func(i, j int) bool {
		if surfaces[i].ID != surfaces[j].ID {
			return surfaces[i].ID < surfaces[j].ID
		}
		if surfaces[i].Row != surfaces[j].Row {
			return surfaces[i].Row < surfaces[j].Row
		}
		return surfaces[i].Col < surfaces[j].Col
	})
	if terminalSurfacesEqual(surfaces, e.terminalSurfacesSent) {
		return
	}
	e.terminalSurfacesSent = surfaces
	e.Config.TerminalSurfaces.Place(surfaces)

	// A visible surface's session is resized to match the cells it now owns —
	// unless the host has DECLARED the grid it actually renders there, which
	// wins. The rectangle is where the surface goes; the declaration is how
	// much of it is text. See ptyState.gridCols.
	type resizeTo struct {
		sess       PTYSession
		cols, rows int
	}
	type logicalTo struct {
		id         string
		cols, rows int
	}
	e.ptyMu.Lock()
	want := make([]resizeTo, 0, live)
	var logic []logicalTo
	byID := make(map[string]*ptyState, live)
	for _, st := range e.ptySessions {
		byID[st.id] = st
	}
	for _, s := range surfaces {
		// Only the PRIMARY surface sizes the grid — a mirror's rectangle never
		// resizes the child, so the terminal's cols/rows stay locked to the
		// canonical/focused tile no matter how big the other tiles are.
		if !s.Primary {
			continue
		}
		st := byID[s.ID]
		if st == nil || st.sess == nil {
			continue
		}
		if st.placedCols != s.Width || st.placedRows != s.Height {
			// A different rectangle than the one the host measured: its
			// declaration described the old one. Fall back to the rectangle
			// and let the host declare again after it repaints.
			st.gridCols, st.gridRows = 0, 0
			st.placedCols, st.placedRows = s.Width, s.Height
		}
		// The VISIBLE grid is the host's declared grid when it has one, else the
		// tile rectangle. The child is sized to the session's LOGICAL size —
		// the visible grid for a follow policy (unchanged), a pinned or floored
		// size otherwise — so a --size child keeps its columns however large the
		// tile is.
		visCols, visRows := s.Width, s.Height
		if st.gridCols > 0 && st.gridRows > 0 {
			visCols, visRows = st.gridCols, st.gridRows
		}
		cols, rows := st.policy.resolveLogical(visCols, visRows)
		want = append(want, resizeTo{st.sess, cols, rows})

		// Tell the host the logical size only when it changes, so it renders the
		// right number of cells (and a scrollbar when they exceed the tile). A
		// follow policy resolves to 0,0 and, set once at attach, never re-sends.
		lc, lr := st.policy.hostLogical(visCols, visRows)
		if lc != st.logSentCols || lr != st.logSentRows {
			st.logSentCols, st.logSentRows = lc, lr
			logic = append(logic, logicalTo{st.id, lc, lr})
		}
	}
	e.ptyMu.Unlock()
	for _, w := range want {
		_ = w.sess.Resize(w.cols, w.rows)
	}
	if e.Config.TerminalSurfaces.SetLogicalSize != nil {
		for _, l := range logic {
			e.Config.TerminalSurfaces.SetLogicalSize(l.id, l.cols, l.rows)
		}
	}
}

// SetTerminalGrid records the host's declaration of a surface's true grid and
// resizes its child to it. The host calls this (through the terminal_grid
// command) whenever its display settles on a grid size — which is the only
// moment anyone knows the answer, since it depends on the host's font metrics
// and its own chrome, neither of which mew can see.
//
// Reports whether the session exists.
// pixelWindowReporter is the optional half of a session that knows the child's
// window in device pixels — the host wraps a PTY in one when it can measure the
// pane (see the host's withPixelWinsize). A session without it carries cells
// only, and its pixel window is reported as unknown.
type pixelWindowReporter interface {
	WindowPx() (int, int)
}

// sessionWindowPx is the child's pixel window, or 0x0 when the session cannot
// say. Unknown compares equal to unknown, so a cells-only session dedupes on
// its cells exactly as it always did.
func sessionWindowPx(s PTYSession) (int, int) {
	if r, ok := s.(pixelWindowReporter); ok {
		return r.WindowPx()
	}
	return 0, 0
}

func (e *Editor) SetTerminalGrid(id string, cols, rows int) bool {
	if cols <= 0 || rows <= 0 {
		return false
	}
	e.ptyMu.Lock()
	var sess PTYSession
	for _, st := range e.ptySessions {
		if st.id == id {
			// The window is cells AND pixels; a repeat is a repeat of both.
			// The session knows the pixel half (the host measures it and the
			// session fills it in on every resize), so ask it rather than
			// assuming the cells speak for the whole window.
			pxW, pxH := sessionWindowPx(st.sess)
			if st.gridCols == cols && st.gridRows == rows &&
				st.gridPxW == pxW && st.gridPxH == pxH {
				e.ptyMu.Unlock()
				return true // already there; do not wake the child again
			}
			st.gridCols, st.gridRows = cols, rows
			st.gridPxW, st.gridPxH = pxW, pxH
			sess = st.sess
			break
		}
	}
	e.ptyMu.Unlock()
	if sess == nil {
		return false
	}
	_ = sess.Resize(cols, rows)
	return true
}

func terminalSurfacesEqual(a, b []TerminalSurface) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ptyEnded fires when a session's byte stream ends. The host destroys the
// surface, and the buffer becomes an ORDINARY editable buffer — not a
// read-only transcript.
//
// The notification is the only account of a session anyone gets, because the
// surface is destroyed in the same breath: anything written INTO the terminal
// at this point is painted over by its own removal. So whatever is known about
// how it ended has to be said here.
func (e *Editor) ptyEnded(w *viewport.Viewport, cause error) {
	e.ptyMu.Lock()
	st := e.ptySessions[w]
	delete(e.ptySessions, w)
	e.ptyMu.Unlock()
	if e.ptyMouseCapture != nil && e.ptyMouseCapture.vp == w {
		e.ptyMouseCapture = nil
	}
	if st == nil {
		return
	}
	// Method 2 (capture-on-die): fold the session's final transcript into its
	// buffer BEFORE the surface is torn down — the snapshot has to be taken while
	// the emulator still exists. It lands at the output cursor, the caret the
	// session was launched from (garland has carried it to wherever that text now
	// is), not at 0,0; the buffer is an ordinary editable document again.
	if st.capture == captureFinal && e.Config.TerminalSurfaces.Snapshot != nil {
		// full and plain both need the escape-carrying ANS stream (plain then
		// strips the styling on our side); text uses the host's cell serializer,
		// which emits no escapes by construction.
		ansi := st.format != captureText
		if text := e.Config.TerminalSurfaces.Snapshot(st.id, ansi); text != "" {
			if st.format == capturePlain {
				text = stripSGR(text)
			}
			if st.outCursor != nil {
				st.outCursor.InsertString(text, nil, false)
			} else if st.buf != nil {
				st.buf.InsertText(0, 0, text)
			}
		}
	}
	// A streaming rung has been folding output in live; stop the host relaying
	// to it and emit whatever escape-carry the filter still holds.
	if st.stream != nil {
		if e.Config.TerminalSurfaces.SetCaptureSink != nil {
			e.Config.TerminalSurfaces.SetCaptureSink(st.id, nil)
		}
		st.stream.flush()
		if st.stream.mirror != nil {
			st.stream.mirror.release()
		}
		st.stream = nil
	}
	if st.outCursor != nil {
		st.outCursor.Release()
		st.outCursor = nil
	}
	if e.Config.TerminalSurfaces.Close != nil {
		e.Config.TerminalSurfaces.Close(st.id)
	}
	msg := sessionEndedMessage(st.command, st.sess, cause)
	// A session that produced NOTHING is the failure worth writing down —
	// there is no output to look at, the surface is already gone, and the
	// person it happened to may be on a machine they cannot investigate. So
	// the record writes itself, next to whatever pty_diag would write, and
	// the notification says where. A session that said anything at all is not
	// this failure and leaves nothing behind.
	// Worth writing down when it produced NOTHING, and equally when it was
	// over almost at once: a shell you meant to keep does not end in a couple
	// of seconds, and if it did, what it managed to say first is the evidence.
	if lived := time.Since(st.started); st.bytes == 0 || lived < ptyShortLife {
		if path, err := e.appendPTYLog(ptySessionRecord(st, msg, cause, lived)); err == nil {
			msg += " — recorded in " + path
		}
	}
	e.ShowNotification(msg)
	e.RequestRender()
}

// ptySessionRecord is what a session worth noticing leaves behind: what was
// asked for, how long it lasted, how it ended, what it managed to say, and —
// when the host keeps one — its own account of the calls it made building it.
func ptySessionRecord(st *ptyState, msg string, cause error, lived time.Duration) string {
	var b strings.Builder
	if st.bytes == 0 {
		b.WriteString("session produced no output\n")
	} else {
		b.WriteString("session ended almost immediately\n")
	}
	fmt.Fprintf(&b, "  command: %s\n", st.command)
	fmt.Fprintf(&b, "  cwd asked for: %q\n", st.cwd)
	if st.method != "" {
		fmt.Fprintf(&b, "  method: %s\n", st.method)
	}
	fmt.Fprintf(&b, "  lasted: %s\n", lived.Round(time.Millisecond))
	fmt.Fprintf(&b, "  ending: %s\n", msg)
	if cause != nil {
		fmt.Fprintf(&b, "  read error: %v\n", cause)
	}
	fmt.Fprintf(&b, "  received: %d bytes\n", st.bytes)
	if len(st.head) > 0 {
		fmt.Fprintf(&b, "  first of it: %s\n", quoteBytes(st.head))
	}
	if d, ok := st.sess.(PTYDiagnostics); ok {
		b.WriteString("  the host's account of building it:\n")
		for _, line := range d.Diagnostics() {
			fmt.Fprintf(&b, "    %s\n", line)
		}
	} else {
		b.WriteString("  (this host keeps no account of how it built the session)\n")
	}
	return b.String()
}

// quoteBytes renders raw terminal output readably — escapes as \e, anything
// unprintable as \xNN — so a record can be read, and so the escapes inside it
// cannot steer whatever displays it.
func quoteBytes(p []byte) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range p {
		switch {
		case c == '\\':
			b.WriteString(`\\`)
		case c == '"':
			b.WriteString(`\"`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			b.WriteString(`\r`)
		case c == '\t':
			b.WriteString(`\t`)
		case c == 0x1b:
			b.WriteString(`\e`)
		case c < 0x20 || c >= 0x7f:
			fmt.Fprintf(&b, `\x%02x`, c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// sessionEndedMessage accounts for a finished session in one line: what ran,
// how it went, and — when the stream broke rather than closing — why.
//
// A host that answers PTYExitStatus can tell the two endings apart, so the
// message does too: a child that ran and exited names its code, and a stream
// that ended under a child still running says that instead of quietly
// implying the child died. A host that cannot answer says the plain thing.
func sessionEndedMessage(command string, sess PTYSession, cause error) string {
	msg := command + " exited"
	if es, ok := sess.(PTYExitStatus); ok {
		if code, exited := es.ExitStatus(); exited {
			msg = fmt.Sprintf("%s exited (code %d)", command, code)
		} else {
			msg = command + ": session ended, child still running"
		}
	}
	if cause != nil && !errors.Is(cause, io.EOF) {
		msg += ": " + cause.Error()
	}
	return msg
}

// bufferInsertArgs is what buffer_insert does: insert the first argument at the
// caret as one coalesced edit. Shared with the dispatching insert, which calls
// it when the focused viewport is NOT running a terminal.
func (e *Editor) bufferInsertArgs(args []interface{}) bool {
	if len(args) == 0 {
		return false
	}
	e.insertText(fmt.Sprintf("%v", args[0]))
	e.trackEdit()
	e.editCoalesced = true // a single-point edit: coalesce the undo run
	return true
}

// bufferInsertNewline is what buffer_insert_newline does. Shared with the
// dispatching insert_newline for the non-terminal case.
func (e *Editor) bufferInsertNewline() bool {
	ok := e.insertNewline()
	if ok {
		e.trackEdit()
		e.editCoalesced = true
	}
	return ok
}

// focusedPTY is the session bound to the focused viewport's buffer, or nil.
// The dispatching insert / insert_newline ask this to decide whether a
// keystroke belongs to a child process or to the document.
func (e *Editor) focusedPTY() PTYSession {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil {
		return nil
	}
	// visible, not merely existing: a hidden session leaves typing to the
	// document, so insert / insert_newline fall through to normal editing.
	return e.visibleSessionFor(w)
}

// ptySendBytes writes to the focused buffer's session. This is what the pty
// keybinding context routes ordinary typing to instead of insert.
func (e *Editor) ptySendBytes(data []byte) bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil {
		return false
	}
	sess := e.visibleSessionFor(w)
	if sess == nil {
		return false // no visible session: the chain falls through to normal editing
	}
	if len(data) == 0 {
		return false
	}
	if _, err := sess.Write(data); err != nil {
		e.ShowWarning("Session write failed: " + err.Error())
		return false
	}
	return true
}

// ptyMouseGesture holds a terminal mouse gesture from press to release: the
// viewport whose terminal took the press, and the content rectangle (1-based
// origin cx,cy just left/above the first cell; cw,ch cells) of the exact tile
// the press landed in. A viewport can be shown in several tiles of different
// sizes — a primary hosting the live terminal plus read-only mirrors — so the
// gesture maps every later coordinate against the pressed tile instead of the
// viewport's canonical geometry, which would drift a drag begun in a mirror.
type ptyMouseGesture struct {
	vp             *viewport.Viewport
	cx, cy, cw, ch int
}

// ptyMouseKey offers one mouse pseudo-key to the terminal under the pointer
// before mew's own mouse semantics see it. Reports true when the terminal (or
// its child) took it — when the host answered with bytes and they went out on
// the session.
//
// A "no" falls straight through to mew's own handling. It means neither the
// child nor the terminal wanted the event — not that the child was not
// tracking the mouse, which is the case where the TERMINAL takes it for its
// own scrollbar and scrollback.
//
// x and y are 1-based screen cells (e.mouseX/e.mouseY, already updated by the
// caller). The event routes to the tile the pointer is IN — a viewport shown in
// several tiles has one live terminal (the primary) and read-only mirrors, but
// its scrollbar and scrollback are the emulator's own, so a press in a mirror
// must reach the terminal too. The press also focuses that tile, so the live
// trinket materializes where the user clicked.
func (e *Editor) ptyMouseKey(base string, shift, alt, ctrl bool, x, y int) bool {
	if e.Config.TerminalSurfaces.Mouse == nil {
		return false
	}
	ev, ok := terminalMouseFromKey(base, shift, alt, ctrl)
	if !ok {
		return false
	}

	// A gesture in progress owns the WHOLE gesture. A press the terminal took
	// captures the pointer for it — its scrollbar drag and its selection both
	// keep tracking when the pointer leaves the rectangle, exactly as any
	// trinket's would — and the release lets go. Coordinates map against the
	// tile the press landed in and may run past its edges here, deliberately: a
	// drag below the end of a scrollbar is something the scrollbar wants to know
	// about.
	if g := e.ptyMouseCapture; g != nil {
		e.ptyMu.Lock()
		st := e.ptySessions[g.vp]
		e.ptyMu.Unlock()
		if st == nil || st.hidden {
			e.ptyMouseCapture = nil // session gone or hidden mid-gesture: let go
		} else {
			ev.Col = x - g.cx
			ev.Row = y - g.cy
			e.deliverPTYMouse(st, ev)
			if ev.Action == TerminalMouseRelease {
				e.ptyMouseCapture = nil
			}
			return true
		}
	}

	// Resolve the tile under the pointer and route to its terminal. A click in a
	// mirror must map against the mirror's rectangle, not the focused viewport's
	// canonical one — otherwise the offset lands off-grid and the press falls
	// through to the buffer behind. The focused viewport is the fallback for a
	// terminal that is not a tiled main pane (e.g. hosted in a prompt).
	var (
		vp             *viewport.Viewport
		cx, cy, cw, ch int
		tile           *viewport.ViewportLayout
	)
	if t := e.mainTileAt(x, y); t != nil && t.Viewport != nil && t.Viewport.Buffer != nil {
		tile = t
		vp = t.Viewport
		cx, cy, cw, ch = t.ContentX, t.ContentY, t.ContentWidth, t.ContentHeight
	} else if w := e.ViewportManager.GetFocusedViewport(); w != nil && w.Buffer != nil {
		vp = w
		cx, cy, cw, ch = w.ContentX, w.ContentY, w.ContentWidth, w.ContentHeight
	} else {
		return false
	}
	e.ptyMu.Lock()
	st := e.ptySessions[vp]
	e.ptyMu.Unlock()
	if st == nil || st.hidden {
		return false // hidden: the mouse selects text in the document as usual
	}
	// The surface's grid is the tile's content area — the same rectangle
	// notifyTerminalSurfaces publishes for the primary — so the child's own
	// 1-based cell is the offset from its origin.
	col := x - cx
	row := y - cy
	if col < 1 || row < 1 || col > cw || row > ch {
		return false
	}
	ev.Col, ev.Row = col, row
	handled := e.deliverPTYMouse(st, ev)
	if handled && ev.Action == TerminalMousePress {
		e.ptyMouseCapture = &ptyMouseGesture{vp: vp, cx: cx, cy: cy, cw: cw, ch: ch}
		// The press belongs to this tile. A press the terminal takes is consumed
		// here, so mew's own click handling (which would focus the pane) never
		// runs — focus the pane ourselves. First the VIEWPORT, so keys and the
		// caret follow the child the user clicked when it is a DIFFERENT pane;
		// that runs tilerFollowFocus, which lands on the ref's first tile, so the
		// exact clicked tile is (re)applied after. A press in the already-focused
		// viewport (including a mirror of it) only re-homes the tile.
		if tile != nil {
			if w := tile.Viewport; w != nil && e.ViewportManager.GetFocusedViewport() != w {
				e.ViewportManager.FocusViewportAsCycle(w)
				e.announceFocusedViewport()
				e.RequestRender()
			}
			e.focusTilerTile(tile.TileHandle)
		}
	}
	return handled
}

// deliverPTYMouse asks the host what one event means to a session's terminal
// and writes whatever bytes come back. Reports whether the event was taken.
func (e *Editor) deliverPTYMouse(st *ptyState, ev TerminalMouse) bool {
	data, handled := e.Config.TerminalSurfaces.Mouse(st.id, ev)
	if !handled {
		return false
	}
	if len(data) > 0 {
		if _, err := st.sess.Write(data); err != nil {
			e.ShowWarning("Session write failed: " + err.Error())
			return false
		}
	}
	e.RequestRender()
	return true
}

// armRawKey is raw_key_input: the NEXT keystroke goes to the focused
// terminal's child process instead of through mew's keymap.
//
// This is the deeper half of the host's own Raw Key Input. That one stops the
// HOST from eating a keystroke as a menu shortcut, so it reaches mew — which
// then eats it as a mew binding, and a shell one level further in still never
// sees it. Arming both means one keystroke passes the whole way down.
//
// It is a mew command and not a private channel from the host, so a plain TUI
// mew has the same escape hatch: bind raw_key_input to a key and the one after
// it belongs to whatever is running inside.
func (e *Editor) armRawKey() bool {
	e.rawKeyArmed = true
	if e.focusedPTY() != nil {
		e.ShowNotification("Raw key: the next key goes to the terminal")
	}
	return true
}

// rawKeyToPTY sends one key to the focused viewport's child as the bytes that
// terminal would produce for it, and reports whether it went. False for
// everything else — no session, no host translator, a name that encodes to
// nothing — and the keystroke then takes its ordinary path, because a raw key
// that vanished would be worse than one that was merely not raw.
func (e *Editor) rawKeyToPTY(key string) bool { return e.sendKeyToPTY(key) }

// sendKeyToPTY sends one key to the focused viewport's child as the bytes that
// terminal would produce for it, and reports whether it went. False for
// everything else — no session, no host translator, a name that encodes to
// nothing — and the keystroke then takes its ordinary path, because a key that
// vanished would be worse than one that was merely not forwarded.
//
// Two callers: rawKeyToPTY (a key claimed in advance by raw_key_input) and
// the tinput_key command (a key the capture layer claims as it arrives —
// `(capture) * = tinput_key` in [pty::mappings], the ordinary route).
//
// A RELEASE goes only if this child was told about the press — see
// ptyState.keyPressedToChild. That is not a refinement of the encoding: it is
// the difference between reporting a key and inventing one.
func (e *Editor) sendKeyToPTY(key string) bool {
	if e.Config.TerminalSurfaces.Key == nil {
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}
	// The press and its release name the same key; the marker is what tells
	// them apart, and a repeat is a press. Match on the bare name.
	base, release := strings.CutSuffix(key, ":Release")
	if !release {
		base, _ = strings.CutSuffix(base, ":Repeat")
	}

	e.ptyMu.Lock()
	st := e.ptySessions[w]
	if st != nil && release {
		// Consume the press this release belongs to, and decline outright if
		// there is none: mew's keymap took that keystroke and the child never
		// heard it go down. Consumed whether or not the bytes go out below, so
		// a child that negotiated nothing (every release encodes to nothing
		// there) does not accumulate names it can never use.
		if !st.keyPressedToChild[base] {
			e.ptyMu.Unlock()
			return false
		}
		delete(st.keyPressedToChild, base)
	}
	e.ptyMu.Unlock()
	if st == nil || st.hidden {
		return false // hidden: the key edits the document as usual
	}
	data := e.Config.TerminalSurfaces.Key(st.id, key)
	if len(data) == 0 {
		return false
	}
	if _, err := st.sess.Write(data); err != nil {
		e.ShowWarning("Session write failed: " + err.Error())
		return false
	}
	if !release {
		e.ptyMu.Lock()
		if st.keyPressedToChild == nil {
			st.keyPressedToChild = make(map[string]bool)
		}
		st.keyPressedToChild[base] = true
		e.ptyMu.Unlock()
	}
	return true
}

// terminalMouseFromKey reads one of mew's mouse pseudo-keys — the vocabulary
// handleMouseKey dispatches on, with its modifier prefixes already stripped —
// as a terminal mouse event. Col and Row are the caller's to fill in.
//
// A bare "Mouse@x,y" is position only: it precedes an action and is not one,
// so it reports false and the action that follows carries the position.
func terminalMouseFromKey(base string, shift, alt, ctrl bool) (TerminalMouse, bool) {
	ev := TerminalMouse{Shift: shift, Alt: alt, Ctrl: ctrl}
	name := base
	if i := strings.IndexByte(name, '@'); i >= 0 {
		name = name[:i]
	}
	switch name {
	case "MousePress", "MouseLeftPress":
		ev.Action, ev.Button = TerminalMousePress, TerminalMouseButtonLeft
	case "MouseMiddlePress":
		ev.Action, ev.Button = TerminalMousePress, TerminalMouseButtonMiddle
	case "MouseRightPress":
		ev.Action, ev.Button = TerminalMousePress, TerminalMouseButtonRight
	case "MouseRelease", "MouseLeftRelease":
		ev.Action, ev.Button = TerminalMouseRelease, TerminalMouseButtonLeft
	case "MouseMiddleRelease":
		ev.Action, ev.Button = TerminalMouseRelease, TerminalMouseButtonMiddle
	case "MouseRightRelease":
		ev.Action, ev.Button = TerminalMouseRelease, TerminalMouseButtonRight
	case "MouseLeftDrag":
		ev.Action, ev.Button = TerminalMouseMotion, TerminalMouseButtonLeft
	case "MouseMiddleDrag":
		ev.Action, ev.Button = TerminalMouseMotion, TerminalMouseButtonMiddle
	case "MouseRightDrag":
		ev.Action, ev.Button = TerminalMouseMotion, TerminalMouseButtonRight
	case "MouseDrag":
		// All-motion tracking: movement with no button held.
		ev.Action, ev.Button = TerminalMouseMotion, TerminalMouseButtonNone
	case "MouseScrollUp":
		ev.Action = TerminalMouseScrollUp
	case "MouseScrollDown":
		ev.Action = TerminalMouseScrollDown
	case "MouseScrollLeft":
		ev.Action = TerminalMouseScrollLeft
	case "MouseScrollRight":
		ev.Action = TerminalMouseScrollRight
	default:
		return TerminalMouse{}, false
	}
	return ev, true
}

// closePTYSessions ends every live session. Called when the editor shuts down
// so children do not outlive the editor that asked for them.
func (e *Editor) closePTYSessions() {
	e.ptyMu.Lock()
	sessions := make([]PTYSession, 0, len(e.ptySessions))
	for _, st := range e.ptySessions {
		sessions = append(sessions, st.sess)
	}
	e.ptySessions = nil
	e.ptyMu.Unlock()
	for _, s := range sessions {
		_ = s.Close()
	}
}
