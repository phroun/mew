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
	"path/filepath"
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

// ptyState is one buffer's live session.
type ptyState struct {
	sess    PTYSession
	command string
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

	e.attachPTY(w.Buffer, sess, req.Command)
	e.ShowNotification("Started " + req.Command)
	return true
}

// attachPTY binds a session to a buffer and starts pumping its output.
func (e *Editor) attachPTY(b *buffer.Buffer, sess PTYSession, command string) {
	e.ptyMu.Lock()
	if e.ptySessions == nil {
		e.ptySessions = make(map[*buffer.Buffer]*ptyState)
	}
	e.ptySessions[b] = &ptyState{sess: sess, command: command}
	e.ptyMu.Unlock()

	// The read loop is the session's own goroutine; every delivery marshals
	// onto the editor main loop through PostAction, with exactly the safety of
	// a keystroke. A read that returns nothing but an error ends the session.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := sess.Read(buf)
			if n > 0 {
				chunk := append([]byte(nil), buf[:n]...)
				e.PostAction(func() { e.ptyOutput(b, chunk) })
			}
			if err != nil {
				break
			}
		}
		e.PostAction(func() { e.ptyEnded(b) })
	}()
}

// ptyOutput receives one chunk of session output on the main loop.
//
// PROVISIONAL. This appends the printable text to the buffer with terminal
// control sequences dropped, which is enough to watch a build or read ls
// output but is not terminal emulation: a full-screen program will not render
// correctly. The real version drives a purfecterm Buffer+Parser and renders
// its grid into the viewport, with lines scrolling off into garland as real
// text — deliberately left until the tiling/ToolViewport work settles, since
// that decides where the grid lives. Everything provisional is in this one
// function and stripTerminalControls below.
func (e *Editor) ptyOutput(b *buffer.Buffer, chunk []byte) {
	if b == nil || len(chunk) == 0 {
		return
	}
	text := stripTerminalControls(chunk)
	if text == "" {
		return
	}
	last := b.GetLineCount() - 1
	if last < 0 {
		last = 0
	}
	endRune := len([]rune(strings.TrimRight(b.GetLine(last), "\n\r")))

	b.BeginUserCommand("pty_output")
	k := b.NewCaret()
	if k != nil {
		k.Seek(last, endRune)
		k.Insert(text)
	}
	b.EndUserCommand()
	e.RequestRender()
}

// ptyEnded fires when the child is gone. The buffer keeps everything the
// session produced and becomes an ORDINARY editable buffer — not a read-only
// transcript. Whatever was on screen is now just text you can edit and save.
func (e *Editor) ptyEnded(b *buffer.Buffer) {
	e.ptyMu.Lock()
	st := e.ptySessions[b]
	delete(e.ptySessions, b)
	e.ptyMu.Unlock()
	if st == nil {
		return
	}
	e.ShowNotification(st.command + " exited")
	e.RequestRender()
}

// ptySend writes bytes to the focused buffer's session. This is what the pty
// keybinding context routes ordinary typing to instead of insert.
func (e *Editor) ptySend(data string) bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil {
		return false
	}
	sess := e.ptySessionFor(w.Buffer)
	if sess == nil {
		return false // no session: the chain falls through to normal editing
	}
	if _, err := sess.Write([]byte(data)); err != nil {
		e.ShowWarning("Session write failed: " + err.Error())
		return false
	}
	return true
}

// ptyResizeAll matches every live session to its viewport's content size.
// Called after layout; a session whose viewport is gone is left alone.
func (e *Editor) ptyResizeAll() {
	e.ptyMu.Lock()
	if len(e.ptySessions) == 0 {
		e.ptyMu.Unlock()
		return
	}
	sizes := make(map[PTYSession][2]int)
	for _, w := range e.ViewportManager.AllViewports() {
		if w.Buffer == nil {
			continue
		}
		if st := e.ptySessions[w.Buffer]; st != nil {
			c, r := w.ContentWidth, w.ContentHeight
			if c > 0 && r > 0 {
				sizes[st.sess] = [2]int{c, r}
			}
		}
	}
	e.ptyMu.Unlock()
	for sess, cr := range sizes {
		_ = sess.Resize(cr[0], cr[1])
	}
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

// stripTerminalControls drops ANSI/DEC control sequences and keeps the text.
//
// PROVISIONAL, with ptyOutput: a stand-in until the emulator lands. It removes
// CSI (ESC [ … final), OSC (ESC ] … BEL or ST), and two-character ESC
// sequences, keeps newline and tab, and drops the remaining C0 controls. It is
// not an emulator and does not pretend to be one — a full-screen program's
// cursor motion is discarded rather than honoured.
func stripTerminalControls(p []byte) string {
	var out strings.Builder
	out.Grow(len(p))
	for i := 0; i < len(p); {
		c := p[i]
		if c == 0x1b { // ESC
			i++
			if i >= len(p) {
				break
			}
			switch p[i] {
			case '[': // CSI: params then a final byte in @..~
				i++
				for i < len(p) && (p[i] < 0x40 || p[i] > 0x7e) {
					i++
				}
				if i < len(p) {
					i++
				}
			case ']': // OSC: runs to BEL or ST (ESC \)
				i++
				for i < len(p) {
					if p[i] == 0x07 {
						i++
						break
					}
					if p[i] == 0x1b && i+1 < len(p) && p[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
			default: // two-character escape
				i++
			}
			continue
		}
		if c == '\n' || c == '\t' {
			out.WriteByte(c)
			i++
			continue
		}
		if c == '\r' {
			i++ // CR is cursor motion here, not content
			continue
		}
		if c < 0x20 || c == 0x7f {
			i++
			continue
		}
		out.WriteByte(c)
		i++
	}
	return out.String()
}
