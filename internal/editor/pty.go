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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/viewport"
)

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
}

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

// ptyState is one buffer's live session.
type ptyState struct {
	id      string // stable for the session's life; names its child surface
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
	// placedCols/placedRows are the rectangle the declaration was made
	// against. A declaration describes one rectangle; when the viewport is
	// RESIZED the old answer is stale, so it is dropped and the rectangle
	// governs again until the host has repainted and declared afresh. A
	// viewport that merely MOVED keeps its declaration — same size, same
	// answer, and re-deriving it would resize the child for nothing.
	placedCols, placedRows int
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

// ptySessionFor returns the live session bound to a buffer, or nil.
func (e *Editor) ptySessionFor(b *buffer.Buffer) PTYSession {
	if b == nil {
		return nil
	}
	e.ptyMu.Lock()
	defer e.ptyMu.Unlock()
	if st := e.ptySessions[b]; st != nil {
		return st.sess
	}
	return nil
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

// execRequestSpec is execRequestArgs with the shell flag: when set, the host
// names the program (the user's login shell) and command must be empty.
func (e *Editor) execRequestSpec(command string, args []string, method string, shell bool) bool {
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
	if e.ptySessionFor(w.Buffer) != nil {
		e.ShowWarning("This buffer already has a session")
		return false
	}

	cols, rows := w.ContentWidth, w.ContentHeight
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
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
	e.attachPTY(w.Buffer, sess, name, req.CWD, req.Method, cols, rows)
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

// attachPTY binds a session to a buffer and starts pumping its output.
func (e *Editor) attachPTY(b *buffer.Buffer, sess PTYSession, command, cwd, method string, cols, rows int) {
	e.ptyMu.Lock()
	if e.ptySessions == nil {
		e.ptySessions = make(map[*buffer.Buffer]*ptyState)
	}
	e.ptySeq++
	id := fmt.Sprintf("pty%d", e.ptySeq)
	e.ptySessions[b] = &ptyState{
		id: id, sess: sess, command: command, cwd: cwd, method: method,
		started: time.Now(),
	}
	e.ptyMu.Unlock()

	if e.Config.TerminalSurfaces.Open != nil {
		e.Config.TerminalSurfaces.Open(id, cols, rows)
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
				e.PostAction(func() { e.ptyOutput(b, chunk) })
			}
			if err != nil {
				last = err
				break
			}
		}
		// The error that ended the stream travels with the ending: a session
		// that stopped because something broke should say so, and only a
		// clean end of file is silent.
		e.PostAction(func() { e.ptyEnded(b, last) })
	}()
}

// ptyOutput hands one chunk of session output to the host, verbatim.
//
// mew does no terminal emulation and no filtering: PurfecTerm on the other side
// IS an emulator, so raw bytes are exactly what it wants — full-screen cursor
// motion, colour, alternate screen and all. Anything mew stripped here would be
// a capability the child surface then lacked.
//
// The garland buffer stays EMPTY while a session runs. Folding scrollback into
// it as real text is deliberate follow-up work, and doing it half-way now would
// only have to be undone.
func (e *Editor) ptyOutput(b *buffer.Buffer, chunk []byte) {
	if b == nil || len(chunk) == 0 || e.Config.TerminalSurfaces.Feed == nil {
		return
	}
	e.ptyMu.Lock()
	st := e.ptySessions[b]
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
	byBuffer := make(map[*buffer.Buffer]string, live)
	for b, st := range e.ptySessions {
		byBuffer[b] = st.id
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
		id                    string
		vp                    *viewport.Viewport
		col, row, w, h        int
		focusedTile           bool
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

	// ON SCREEN THIS FRAME, not merely holding the buffer: a viewport swapped to
	// another buffer, hidden, or stacked behind another keeps its last layout,
	// and a surface from that stale geometry paints over whatever took its place.
	for i := range e.mainTiles {
		t := &e.mainTiles[i]
		w := t.Viewport
		if w == nil || !e.viewportOnScreen(w) || t.ContentWidth <= 0 || t.ContentHeight <= 0 {
			continue
		}
		if id, ok := byBuffer[w.Buffer]; ok {
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
		id, ok := byBuffer[w.Buffer]
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
	e.ptyMu.Lock()
	want := make([]resizeTo, 0, live)
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
		cols, rows := s.Width, s.Height
		if st.gridCols > 0 && st.gridRows > 0 {
			cols, rows = st.gridCols, st.gridRows
		}
		want = append(want, resizeTo{st.sess, cols, rows})
	}
	e.ptyMu.Unlock()
	for _, w := range want {
		_ = w.sess.Resize(w.cols, w.rows)
	}
}

// SetTerminalGrid records the host's declaration of a surface's true grid and
// resizes its child to it. The host calls this (through the terminal_grid
// command) whenever its display settles on a grid size — which is the only
// moment anyone knows the answer, since it depends on the host's font metrics
// and its own chrome, neither of which mew can see.
//
// Reports whether the session exists.
func (e *Editor) SetTerminalGrid(id string, cols, rows int) bool {
	if cols <= 0 || rows <= 0 {
		return false
	}
	e.ptyMu.Lock()
	var sess PTYSession
	for _, st := range e.ptySessions {
		if st.id == id {
			if st.gridCols == cols && st.gridRows == rows {
				e.ptyMu.Unlock()
				return true // already there; do not wake the child again
			}
			st.gridCols, st.gridRows = cols, rows
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
func (e *Editor) ptyEnded(b *buffer.Buffer, cause error) {
	e.ptyMu.Lock()
	st := e.ptySessions[b]
	delete(e.ptySessions, b)
	e.ptyMu.Unlock()
	if e.ptyMouseCapture != nil && e.ptyMouseCapture.buf == b {
		e.ptyMouseCapture = nil
	}
	if st == nil {
		return
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
	return e.ptySessionFor(w.Buffer)
}

// ptySendBytes writes to the focused buffer's session. This is what the pty
// keybinding context routes ordinary typing to instead of insert.
func (e *Editor) ptySendBytes(data []byte) bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil {
		return false
	}
	sess := e.ptySessionFor(w.Buffer)
	if sess == nil {
		return false // no session: the chain falls through to normal editing
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
// buffer whose terminal took the press, and the content rectangle (1-based
// origin cx,cy just left/above the first cell; cw,ch cells) of the exact tile
// the press landed in. A viewport can be shown in several tiles of different
// sizes — a primary hosting the live terminal plus read-only mirrors — so the
// gesture maps every later coordinate against the pressed tile instead of the
// viewport's canonical geometry, which would drift a drag begun in a mirror.
type ptyMouseGesture struct {
	buf            *buffer.Buffer
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
		st := e.ptySessions[g.buf]
		e.ptyMu.Unlock()
		if st == nil {
			e.ptyMouseCapture = nil
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
		buf            *buffer.Buffer
		cx, cy, cw, ch int
		tile           *viewport.ViewportLayout
	)
	if t := e.mainTileAt(x, y); t != nil && t.Viewport != nil && t.Viewport.Buffer != nil {
		tile = t
		buf = t.Viewport.Buffer
		cx, cy, cw, ch = t.ContentX, t.ContentY, t.ContentWidth, t.ContentHeight
	} else if w := e.ViewportManager.GetFocusedViewport(); w != nil && w.Buffer != nil {
		buf = w.Buffer
		cx, cy, cw, ch = w.ContentX, w.ContentY, w.ContentWidth, w.ContentHeight
	} else {
		return false
	}
	e.ptyMu.Lock()
	st := e.ptySessions[buf]
	e.ptyMu.Unlock()
	if st == nil {
		return false
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
		e.ptyMouseCapture = &ptyMouseGesture{buf: buf, cx: cx, cy: cy, cw: cw, ch: ch}
		// The press belongs to this tile: focus it so the live terminal
		// materializes here (a clicked mirror becomes the primary) and later
		// frames paint the caret and trinket at this location.
		if tile != nil {
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
// `capture * = tinput_key` in [pty::mappings], the ordinary route).
func (e *Editor) sendKeyToPTY(key string) bool {
	if e.Config.TerminalSurfaces.Key == nil {
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}
	e.ptyMu.Lock()
	st := e.ptySessions[w.Buffer]
	e.ptyMu.Unlock()
	if st == nil {
		return false
	}
	data := e.Config.TerminalSurfaces.Key(st.id, key)
	if len(data) == 0 {
		return false
	}
	if _, err := st.sess.Write(data); err != nil {
		e.ShowWarning("Session write failed: " + err.Error())
		return false
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
