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

	"github.com/phroun/mew/internal/buffer"
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

// ptyState is one buffer's live session.
type ptyState struct {
	id      string // stable for the session's life; names its child surface
	sess    PTYSession
	command string
}

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

	// Focused marks the ONE surface whose viewport currently holds mew's
	// focus. That surface owns the platform's text caret — its position and
	// its DECSCUSR shape both come from the child process, not from mew's own
	// caret, because while you are typing at a shell the cursor you are
	// watching is the shell's. mew keeps keyboard focus regardless, so its
	// keymap still runs; only the drawn caret is ceded.
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
	Open  func(id string, cols, rows int)
	Feed  func(id string, p []byte)
	Place func(surfaces []TerminalSurface)
	Close func(id string)

	// Mouse asks what one mouse event MEANS to the surface's terminal, and
	// returns the bytes the child should receive — nil when that application
	// is not tracking the mouse, or when this event is one its tracking mode
	// does not report. mew writes what comes back to the session, so every
	// byte a child receives still leaves through the one door, exactly as
	// keystrokes do.
	//
	// A translation and not a delivery: the host owns the emulator and so
	// knows the tracking mode, but the session belongs to mew.
	Mouse func(id string, ev TerminalMouse) []byte

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
	const logName = "mew-pty-diag.log"
	saved := ""
	if err := e.FS.WriteFile(logName, []byte(report)); err == nil {
		saved = logName
		if wd, werr := os.Getwd(); werr == nil {
			saved = filepath.Join(wd, logName)
		}
		report = "(also saved to " + saved + ")\n" + report
	} else {
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

// execRequest asks the host for a session and binds it to the focused
// buffer. Denial is an ordinary outcome, reported and survivable: the buffer
// stays exactly what it was.
func (e *Editor) execRequest(command string) bool {
	if e.Config.PTYProvider == nil {
		e.ShowWarning("No terminal provider: this host does not grant sessions")
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		e.ShowWarning("No active buffer")
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
		Cols:    cols,
		Rows:    rows,
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

	e.attachPTY(w.Buffer, sess, req.Command, cols, rows)
	e.ShowNotification("Started " + req.Command)
	return true
}

// attachPTY binds a session to a buffer and starts pumping its output.
func (e *Editor) attachPTY(b *buffer.Buffer, sess PTYSession, command string, cols, rows int) {
	e.ptyMu.Lock()
	if e.ptySessions == nil {
		e.ptySessions = make(map[*buffer.Buffer]*ptyState)
	}
	e.ptySeq++
	id := fmt.Sprintf("pty%d", e.ptySeq)
	e.ptySessions[b] = &ptyState{id: id, sess: sess, command: command}
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
	e.Config.TerminalSurfaces.Feed(st.id, chunk)
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
	var surfaces []TerminalSurface
	for _, w := range e.ViewportManager.AllViewports() {
		// ON SCREEN THIS FRAME, not merely holding the buffer: a viewport that
		// was swapped to another buffer, hidden, or stacked behind another
		// keeps its last layout, and a surface published from that stale
		// geometry goes on painting over whatever took its place. This is the
		// same test viewportAtRow uses to keep a background viewport's stale
		// rows from swallowing clicks.
		if !e.viewportOnScreen(w) || w.ContentWidth <= 0 || w.ContentHeight <= 0 {
			continue
		}
		id, ok := byBuffer[w.Buffer]
		if !ok {
			continue
		}
		// The grid fills the viewport's text area, and for now the clip is
		// that same rectangle: mew's own layout already excludes the chrome.
		// They are separate fields so a future partially-obscured or
		// partially-scrolled viewport can narrow the clip without moving the
		// grid, which is not expressible with one rectangle.
		surfaces = append(surfaces, TerminalSurface{
			ID:         id,
			Col:        w.ContentX + 1,
			Row:        w.ContentY + 1,
			Width:      w.ContentWidth,
			Height:     w.ContentHeight,
			ClipCol:    w.ContentX + 1,
			ClipRow:    w.ContentY + 1,
			ClipWidth:  w.ContentWidth,
			ClipHeight: w.ContentHeight,
			Focused:    w == focused,
		})
	}
	sort.Slice(surfaces, func(i, j int) bool { return surfaces[i].ID < surfaces[j].ID })
	if terminalSurfacesEqual(surfaces, e.terminalSurfacesSent) {
		return
	}
	e.terminalSurfacesSent = surfaces
	e.Config.TerminalSurfaces.Place(surfaces)

	// A visible surface's session is resized to match the cells it now owns.
	e.ptyMu.Lock()
	sessions := make(map[string]PTYSession, live)
	for _, st := range e.ptySessions {
		sessions[st.id] = st.sess
	}
	e.ptyMu.Unlock()
	for _, s := range surfaces {
		if sess := sessions[s.ID]; sess != nil {
			_ = sess.Resize(s.Width, s.Height)
		}
	}
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
	if st == nil {
		return
	}
	if e.Config.TerminalSurfaces.Close != nil {
		e.Config.TerminalSurfaces.Close(st.id)
	}
	e.ShowNotification(sessionEndedMessage(st.command, st.sess, cause))
	e.RequestRender()
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

// ptyMouseKey offers one mouse pseudo-key to the focused viewport's terminal
// before mew's own mouse semantics see it. Reports true when the child took
// it — when the host answered with bytes and they went out on the session.
//
// A "no" is the ordinary case and falls straight through: an application that
// never turned mouse tracking on has no opinion about the mouse, and mew's own
// handling (which does nearly nothing over an empty terminal buffer) resumes.
// That is also the seam where terminal-side selection and scrollback will hang
// once the scrollback lands, because that is exactly the mouse a
// non-tracking terminal owns.
//
// x and y are 1-based screen cells (e.mouseX/e.mouseY, already updated by the
// caller). Only the FOCUSED viewport forwards, matching every other input rule
// here: a click in an unfocused viewport still focuses it instead.
func (e *Editor) ptyMouseKey(base string, shift, alt, ctrl bool, x, y int) bool {
	if e.Config.TerminalSurfaces.Mouse == nil {
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
	// The surface's grid is the viewport's content area — the same rectangle
	// notifyTerminalSurfaces publishes — so the child's own 1-based cell is
	// the offset from its origin.
	col := x - w.ContentX
	row := y - w.ContentY
	if col < 1 || row < 1 || col > w.ContentWidth || row > w.ContentHeight {
		return false
	}
	ev, ok := terminalMouseFromKey(base, shift, alt, ctrl)
	if !ok {
		return false
	}
	ev.Col, ev.Row = col, row
	data := e.Config.TerminalSurfaces.Mouse(st.id, ev)
	if len(data) == 0 {
		return false
	}
	if _, err := st.sess.Write(data); err != nil {
		e.ShowWarning("Session write failed: " + err.Error())
		return false
	}
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
func (e *Editor) rawKeyToPTY(key string) bool {
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
