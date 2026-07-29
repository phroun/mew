package editor

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/phroun/mew/internal/viewport"
)

// stubPTY is a host-provided session: the two halves of a pipe, so a test can
// feed output in and read what mew wrote back.
type stubPTY struct {
	mu      sync.Mutex
	out     chan []byte
	written strings.Builder
	cols    int
	rows    int
	resizes int
	closed  bool
}

func newStubPTY() *stubPTY { return &stubPTY{out: make(chan []byte, 16)} }

func (s *stubPTY) Read(p []byte) (int, error) {
	chunk, ok := <-s.out
	if !ok {
		return 0, io.EOF
	}
	return copy(p, chunk), nil
}
func (s *stubPTY) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.written.Write(p)
	return len(p), nil
}
func (s *stubPTY) Resize(c, r int) error { s.cols, s.rows, s.resizes = c, r, s.resizes+1; return nil }
func (s *stubPTY) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.out)
	}
	return nil
}
func (s *stubPTY) sent() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.written.String()
}

// The request carries a canonical URL for the buffer's directory and the
// command NAME, and nothing that could name a local binary.
func TestExecRequestShape(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	var got PTYRequest
	e.Config.PTYProvider = func(r PTYRequest) (PTYSession, error) {
		got = r
		return newStubPTY(), nil
	}
	w.Buffer.SetFilename("/home/user/project/main.go")

	if !e.execRequest("bash", "") {
		t.Fatal("exec should have succeeded")
	}
	if got.Command != "bash" {
		t.Errorf("Command = %q, want bash", got.Command)
	}
	if !strings.HasPrefix(got.CWD, "file://") || !strings.HasSuffix(got.CWD, "/home/user/project") {
		t.Errorf("CWD = %q, want the buffer's directory as a file:// URL", got.CWD)
	}
	if got.Cols <= 0 || got.Rows <= 0 {
		t.Errorf("Cols/Rows = %d/%d, want a usable size", got.Cols, got.Rows)
	}
}

// An unnamed buffer sends a BLANK cwd. That is a real answer, not a failure:
// the host decides what "nowhere in particular" means, which for a root mew is
// the user's home directory. mew must not guess on its behalf.
func TestExecUnnamedBufferSendsBlankCWD(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	var got PTYRequest
	e.Config.PTYProvider = func(r PTYRequest) (PTYSession, error) {
		got = r
		return newStubPTY(), nil
	}
	if !e.execRequest("zsh", "") {
		t.Fatal("exec should have succeeded")
	}
	if got.CWD != "" {
		t.Errorf("CWD = %q, want empty for an unnamed buffer", got.CWD)
	}
}

// Refusal is an ordinary outcome: reported, and the buffer is untouched.
func TestExecRefusalIsGraceful(t *testing.T) {
	e, w := newTestEditor(t, "hello\n")
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) {
		return nil, errors.New("not permitted here")
	}
	before := docContent(w)
	if e.execRequest("bash", "") {
		t.Error("a refused request must report failure")
	}
	if !hasWarning(e, "not permitted here") {
		t.Error("the host's reason should reach the user")
	}
	if docContent(w) != before {
		t.Error("a refused request must not touch the buffer")
	}
	if e.ptySessionFor(w.Buffer) != nil {
		t.Error("a refused request must leave no session bound")
	}
}

// With no provider at all, exec says so rather than failing silently.
func TestExecWithoutProvider(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	if e.execRequest("bash", "") {
		t.Error("no provider: exec must report failure")
	}
	if !hasWarning(e, "does not grant sessions") {
		t.Error("expected the no-provider warning")
	}
}

// pty_send writes to the bound session, and reports FALSE with no session so a
// chain falls through to ordinary editing.
func TestTinputRoutesToSessionElseFallsThrough(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	if e.ptySendBytes([]byte("ls\n")) {
		t.Error("no session: tinput must fall through")
	}
	stub := newStubPTY()
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	if !e.ptySendBytes([]byte("ls\n")) {
		t.Error("with a session, tinput should succeed")
	}
	if stub.sent() != "ls\n" {
		t.Errorf("session received %q, want %q", stub.sent(), "ls\n")
	}
}

// tinput takes whichever form the value already has: a string sends its UTF-8
// bytes, a {bytes ...} value goes verbatim and whole. The second is how a
// control byte or an escape sequence is written without quoting games.
func TestTinputAcceptsStringsAndByteValues(t *testing.T) {
	for _, tc := range []struct {
		script string
		want   string
	}{
		{`tinput "ls` + "\n" + `"`, "ls\n"},
		{"tinput {bytes 0x03}", "\x03"},       // Ctrl-C
		{"tinput {bytes 0x1b5b41}", "\x1b[A"}, // Up: ESC [ A as one value; see also the comma list
	} {
		e, _ := newTestEditor(t, "x\n")
		stub := newStubPTY()
		e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
		if !e.execRequest("bash", "") {
			t.Fatal("exec failed")
		}
		e.executeCommand(tc.script)
		if stub.sent() != tc.want {
			t.Errorf("%s sent %q, want %q", tc.script, stub.sent(), tc.want)
		}
	}
}

// Exactly one surface is Focused, and it is the one whose viewport mew has
// focused — that surface owns the platform caret.
func TestTerminalSurfaceMarksTheFocusedOne(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	var placed []TerminalSurface
	e.Config.TerminalSurfaces = TerminalHooks{
		Open:  func(string, int, int) {},
		Feed:  func(string, []byte) []byte { return nil },
		Place: func(s []TerminalSurface) { placed = s },
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	w.ContentX, w.ContentY, w.ContentWidth, w.ContentHeight = 0, 0, 80, 24
	e.notifyTerminalSurfaces()
	if len(placed) != 1 {
		t.Fatalf("want one surface, got %d", len(placed))
	}
	if !placed[0].Focused {
		t.Error("the focused viewport's surface should own the caret")
	}
}

// One session per buffer: a second exec on the same buffer is refused rather
// than orphaning the first child.
func TestExecRefusesSecondSessionOnSameBuffer(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
	if !e.execRequest("bash", "") {
		t.Fatal("first exec failed")
	}
	if e.execRequest("zsh", "") {
		t.Error("a second session on one buffer must be refused")
	}
	if !hasWarning(e, "already has a session") {
		t.Error("expected the already-running warning")
	}
}

// mew forwards session bytes VERBATIM — no stripping, no emulation. PurfecTerm
// on the host side is the emulator, so anything filtered here would be a
// capability the child surface then lacked.
func TestPTYOutputForwardsRawBytesToHost(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	var fed []byte
	var openedID, fedID string
	e.Config.TerminalSurfaces = TerminalHooks{
		Open: func(id string, cols, rows int) { openedID = id },
		Feed: func(id string, p []byte) []byte { fedID = id; fed = append(fed, p...); return nil },
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	if openedID == "" {
		t.Fatal("Open was not called")
	}

	// An escape sequence a stripping implementation would have eaten.
	esc := []byte{27, '[', '2', 'J', 'h', 'i', 27, '[', '0', 'm'}
	e.ptyOutput(w.Buffer, esc)

	if fedID != openedID {
		t.Errorf("fed id %q, want the opened id %q", fedID, openedID)
	}
	if string(fed) != string(esc) {
		t.Errorf("host received % x, want % x — bytes must pass through untouched", fed, esc)
	}
	// The garland buffer stays empty while a session runs; scrollback folding
	// is deliberate follow-up work.
	if got := docContent(w); got != "x" {
		t.Errorf("buffer = %q, want it untouched by session output", got)
	}
}

// The surface set is republished per render with the FULL set, so a host never
// has to reconcile deltas against mew's layout.
func TestTerminalSurfacesPublishTheVisibleSet(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	var placed [][]TerminalSurface
	e.Config.TerminalSurfaces = TerminalHooks{
		Open:  func(string, int, int) {},
		Feed:  func(string, []byte) []byte { return nil },
		Place: func(s []TerminalSurface) { placed = append(placed, s) },
		Close: func(string) {},
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }

	e.notifyTerminalSurfaces()
	if len(placed) != 0 {
		t.Error("no sessions: nothing should be published")
	}

	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	w.ContentX, w.ContentY, w.ContentWidth, w.ContentHeight = 0, 1, 80, 24
	e.notifyTerminalSurfaces()
	if len(placed) != 1 || len(placed[0]) != 1 {
		t.Fatalf("want one surface published, got %v", placed)
	}
	got := placed[0][0]
	if got.Col != 1 || got.Row != 2 || got.Width != 80 || got.Height != 24 {
		t.Errorf("surface = %+v, want 1-based cells of the viewport text area", got)
	}
	// Grid and clip are separate fields so a partially obscured viewport can
	// narrow one without moving the other. They coincide today.
	if got.ClipCol != got.Col || got.ClipRow != got.Row ||
		got.ClipWidth != got.Width || got.ClipHeight != got.Height {
		t.Errorf("clip %+v should match the grid rect until a viewport is obscured", got)
	}

	// An unchanged layout republishes nothing.
	e.notifyTerminalSurfaces()
	if len(placed) != 1 {
		t.Errorf("an idle frame republished: %d pushes", len(placed))
	}
}

// The session is closed out through the host so the surface is destroyed, and
// the buffer is left an ordinary editable buffer.
func TestPTYEndedClosesTheSurface(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	var closed string
	e.Config.TerminalSurfaces = TerminalHooks{
		Open:  func(string, int, int) {},
		Close: func(id string) { closed = id },
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	e.ptyEnded(w.Buffer, nil)
	if closed == "" {
		t.Error("Close was not called for the ended session")
	}
	if e.ptySessionFor(w.Buffer) != nil {
		t.Error("the session should be unbound after it ends")
	}
}

// The plain names DISPATCH on what the focused viewport is: a terminal session
// gets the input, anything else gets the buffer edit. This is why the keymaps
// need no pty variant — tab, return and every self-inserting key already end in
// insert or insert_newline.
func TestInsertDispatchesToTerminalOrBuffer(t *testing.T) {
	// No session: insert edits the buffer, exactly as buffer_insert would.
	e, w := newTestEditor(t, "ab\n")
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 1})
	e.executeCommand("insert 'X'")
	if got := docContent(w); got != "aXb" {
		t.Errorf("no session: insert = %q, want aXb", got)
	}

	// With a session: the same command reaches the child instead, and the
	// buffer is untouched.
	e2, w2 := newTestEditor(t, "ab\n")
	stub := newStubPTY()
	e2.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
	if !e2.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	e2.executeCommand("insert 'X'")
	if stub.sent() != "X" {
		t.Errorf("session received %q, want X", stub.sent())
	}
	if got := docContent(w2); got != "ab" {
		t.Errorf("buffer = %q, want it untouched while a session runs", got)
	}
}

// Enter sends CR to a shell — what a terminal actually transmits, and what the
// line discipline turns back into a newline — but breaks the line in a buffer.
func TestInsertNewlineDispatches(t *testing.T) {
	e, w := newTestEditor(t, "ab\n")
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 1})
	e.executeCommand("insert_newline")
	if got := docContent(w); got != "a\nb" {
		t.Errorf("no session: insert_newline = %q, want a broken line", got)
	}

	e2, w2 := newTestEditor(t, "ab\n")
	stub := newStubPTY()
	e2.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
	if !e2.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	e2.executeCommand("insert_newline")
	if stub.sent() != "\r" {
		t.Errorf("session received %q, want CR", stub.sent())
	}
	if got := docContent(w2); got != "ab" {
		t.Errorf("buffer = %q, want it untouched", got)
	}
}

// The buffer_ names always edit the buffer, session or not — that is the point
// of splitting them out.
func TestBufferInsertIgnoresTheSession(t *testing.T) {
	e, w := newTestEditor(t, "ab\n")
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 1})
	stub := newStubPTY()
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	e.executeCommand("buffer_insert 'X'")
	if got := docContent(w); got != "aXb" {
		t.Errorf("buffer_insert = %q, want aXb even with a session running", got)
	}
	if stub.sent() != "" {
		t.Errorf("buffer_insert must not reach the child, sent %q", stub.sent())
	}
}

// {bytes ...} takes a comma-separated LIST as well as one value. Without the
// commas it is symbol concatenation, which evaluates to something else
// entirely — a mistake worth pinning so the comment above tinput stays true.
func TestTinputBytesListSyntax(t *testing.T) {
	for _, tc := range []struct{ script, want string }{
		{"tinput {bytes 0x1b, 0x5b, 0x41}", "\x1b[A"},
		{"tinput {bytes 0x1b5b41}", "\x1b[A"},
	} {
		e, _ := newTestEditor(t, "x\n")
		stub := newStubPTY()
		e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
		if !e.execRequest("bash", "") {
			t.Fatal("exec failed")
		}
		e.executeCommand(tc.script)
		if stub.sent() != tc.want {
			t.Errorf("%s sent %q, want %q", tc.script, stub.sent(), tc.want)
		}
	}
}

// A viewport running a session reports class "pty", which is how
// [pty::mappings] finds it. The class is derived from the buffer, so it
// appears when the session starts and is gone when it ends.
func TestPTYViewportClass(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	if got := e.viewportClass(w); got != "" {
		t.Fatalf("class before exec = %q, want empty", got)
	}
	stub := newStubPTY()
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	if got := e.viewportClass(w); got != "pty" {
		t.Fatalf("class with a session = %q, want pty", got)
	}
	e.ptyEnded(w.Buffer, nil)
	if got := e.viewportClass(w); got != "" {
		t.Fatalf("class after the child exited = %q, want empty again", got)
	}
}

// The keys whose ordinary meaning is an edit reach the child as bytes instead,
// through the [pty::mappings] defaults — and only while a session runs.
func TestPTYClassKeyBindings(t *testing.T) {
	for _, tc := range []struct{ key, want string }{
		{"back", "\x08"},
		{"del", "\x08"},
		{"^C", "\x03"},
	} {
		e, w := newTestEditor(t, "ab\n")
		w.SetCursorPos(viewport.Position{Line: 0, Rune: 2})
		stub := newStubPTY()
		// ^C reaches the child through the wildcard (capture * = tinput_key),
		// which asks the HOST to encode the key — supply the encoder the real
		// host provides. back/del bypass it: their capture bindings send the
		// \x08 byte directly, which is the point of naming them.
		e.Config.TerminalSurfaces = TerminalHooks{
			Open: func(string, int, int) {}, Feed: func(string, []byte) []byte { return nil },
			Key: func(_ string, key string) []byte {
				if key == "^C" {
					return []byte("\x03")
				}
				return nil
			},
		}
		e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
		if !e.execRequest("bash", "") {
			t.Fatal("exec failed")
		}
		e.reconcileFocusedOptions() // adopt the pty class keymap
		e.dispatchKey(tc.key)
		if stub.sent() != tc.want {
			t.Errorf("%s sent %q, want %q", tc.key, stub.sent(), tc.want)
		}
		if got := docContent(w); got != "ab" {
			t.Errorf("%s edited the buffer (%q); it belongs to the child", tc.key, got)
		}
	}
}

// Without a session those keys keep their ordinary meanings: the class is not
// there, so neither is the refinement.
func TestPTYClassKeysUnboundWithoutSession(t *testing.T) {
	e, w := newTestEditor(t, "ab\n")
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 2})
	e.reconcileFocusedOptions()
	e.dispatchKey("back")
	if got := docContent(w); got != "a" {
		t.Errorf("back with no session = %q, want the character deleted", got)
	}
}

// A surface is published only for a viewport ON SCREEN THIS FRAME. Switching
// the viewport to another buffer, or hiding it, withdraws the surface — the
// stale layout of a viewport that is no longer showing would otherwise keep
// the child painted over whatever replaced it.
func TestTerminalSurfaceWithdrawnWhenViewportOffScreen(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	var placed [][]TerminalSurface
	e.Config.TerminalSurfaces = TerminalHooks{
		Open:  func(string, int, int) {},
		Feed:  func(string, []byte) []byte { return nil },
		Place: func(s []TerminalSurface) { placed = append(placed, s) },
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	w.ContentX, w.ContentY, w.ContentWidth, w.ContentHeight = 0, 0, 80, 24
	e.notifyTerminalSurfaces()
	if len(placed) != 1 || len(placed[0]) != 1 {
		t.Fatalf("want the surface published while on screen, got %v", placed)
	}

	w.Visible = false
	e.notifyTerminalSurfaces()
	if len(placed) != 2 {
		t.Fatalf("hiding the viewport should republish, got %d pushes", len(placed))
	}
	if len(placed[1]) != 0 {
		t.Errorf("off-screen viewport still publishes %+v; the surface must be withdrawn", placed[1])
	}
}

// Mouse events land in the child when its application is tracking the mouse:
// mew asks the host what the event means and writes the answer to the session,
// so every byte a child receives still leaves by the one door.
func TestPTYMouseForwarding(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	w.ContentX, w.ContentY, w.ContentWidth, w.ContentHeight = 4, 2, 40, 10
	stub := newStubPTY()
	var got TerminalMouse
	var gotID string
	e.Config.TerminalSurfaces = TerminalHooks{
		Open: func(string, int, int) {}, Feed: func(string, []byte) []byte { return nil },
		Mouse: func(id string, ev TerminalMouse) ([]byte, bool) {
			gotID, got = id, ev
			return []byte("\x1b[<0;1;1M"), true
		},
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}

	e.handleMouseKey("Mouse@6,3")
	e.handleMouseKey("C-MouseLeftPress")
	if gotID == "" {
		t.Fatal("the host was never asked about the event")
	}
	// Screen cell 6,3 inside a content area starting at 5,3 (1-based) is the
	// child's own cell 2,1.
	if got.Col != 2 || got.Row != 1 {
		t.Errorf("event at %d,%d, want the surface-relative 2,1", got.Col, got.Row)
	}
	if got.Action != TerminalMousePress || got.Button != TerminalMouseButtonLeft {
		t.Errorf("action/button = %v/%v, want a left press", got.Action, got.Button)
	}
	// mew reads ctrl+left as a right-click for its own purposes; the child
	// gets what actually happened.
	if !got.Ctrl || got.Shift || got.Alt {
		t.Errorf("modifiers = ctrl:%v shift:%v alt:%v, want ctrl only", got.Ctrl, got.Shift, got.Alt)
	}
	if stub.sent() != "\x1b[<0;1;1M" {
		t.Errorf("session received %q, want the host's report bytes", stub.sent())
	}
}

// Wheel, drag and bare motion all reach the host with the shape the tracking
// modes distinguish; a scroll is not a button and a drag carries its button.
func TestPTYMouseEventShapes(t *testing.T) {
	for _, tc := range []struct {
		keys   []string
		action TerminalMouseAction
		button TerminalMouseButton
	}{
		{[]string{"MouseScrollUp"}, TerminalMouseScrollUp, TerminalMouseButtonNone},
		{[]string{"MouseScrollDown"}, TerminalMouseScrollDown, TerminalMouseButtonNone},
		{[]string{"MouseLeftDrag@6,3"}, TerminalMouseMotion, TerminalMouseButtonLeft},
		{[]string{"MouseDrag@6,3"}, TerminalMouseMotion, TerminalMouseButtonNone},
		{[]string{"MouseRightPress"}, TerminalMousePress, TerminalMouseButtonRight},
		{[]string{"MouseLeftRelease"}, TerminalMouseRelease, TerminalMouseButtonLeft},
	} {
		e, w := newTestEditor(t, "x\n")
		w.ContentX, w.ContentY, w.ContentWidth, w.ContentHeight = 4, 2, 40, 10
		var got TerminalMouse
		e.Config.TerminalSurfaces = TerminalHooks{
			Open: func(string, int, int) {}, Feed: func(string, []byte) []byte { return nil },
			Mouse: func(_ string, ev TerminalMouse) ([]byte, bool) { got = ev; return []byte{'x'}, true },
		}
		e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
		if !e.execRequest("bash", "") {
			t.Fatal("exec failed")
		}
		e.handleMouseKey("Mouse@6,3")
		got = TerminalMouse{}
		for _, k := range tc.keys {
			e.handleMouseKey(k)
		}
		if got.Action != tc.action || got.Button != tc.button {
			t.Errorf("%v: action/button = %v/%v, want %v/%v", tc.keys, got.Action, got.Button, tc.action, tc.button)
		}
	}
}

// A "no" from the host is the ordinary case — the application never turned
// mouse tracking on — and mew's own handling resumes. So does an event outside
// the terminal's rectangle, and one in a viewport that is not focused.
func TestPTYMouseFallsThrough(t *testing.T) {
	newHostedEditor := func(t *testing.T, reply []byte) (*Editor, *viewport.Viewport, *int) {
		t.Helper()
		e, w := newTestEditor(t, "hello\n")
		w.ContentX, w.ContentY, w.ContentWidth, w.ContentHeight = 4, 2, 40, 10
		asked := 0
		e.Config.TerminalSurfaces = TerminalHooks{
			Open: func(string, int, int) {}, Feed: func(string, []byte) []byte { return nil },
			Mouse: func(string, TerminalMouse) ([]byte, bool) { asked++; return reply, len(reply) > 0 },
		}
		e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
		if !e.execRequest("bash", "") {
			t.Fatal("exec failed")
		}
		return e, w, &asked
	}

	// Not tracking: asked, declined, and mew's own press ran.
	e, _, asked := newHostedEditor(t, nil)
	e.handleMouseKey("Mouse@6,3")
	e.handleMouseKey("MouseLeftPress")
	if *asked != 1 {
		t.Errorf("host asked %d times, want once", *asked)
	}
	if !e.dragSel.active {
		t.Error("a declined event should reach mew's own press handling")
	}

	// Outside the terminal's rectangle: never asked at all.
	e2, _, asked2 := newHostedEditor(t, []byte{'x'})
	e2.handleMouseKey("Mouse@2,3") // left of the content area
	e2.handleMouseKey("MouseLeftPress")
	if *asked2 != 0 {
		t.Errorf("a click outside the surface asked the host %d times, want none", *asked2)
	}
}

// raw_key_input claims the NEXT keystroke for the focused terminal's child:
// mew's own binding for it does not run, and the bytes the host encodes go out
// on the session.
func TestRawKeyInputGoesToTheChild(t *testing.T) {
	e, w := newTestEditor(t, "ab\n")
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 2})
	stub := newStubPTY()
	var asked string
	e.Config.TerminalSurfaces = TerminalHooks{
		Open: func(string, int, int) {}, Feed: func(string, []byte) []byte { return nil },
		Key: func(_ string, key string) []byte { asked = key; return []byte("\x1b[21~") },
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}

	e.executeCommand("raw_key_input")
	// ^Y is del_line in mew's keymap — exactly the sort of key a terminal
	// guest could never otherwise receive.
	e.dispatchKey("^Y")
	if asked != "^Y" {
		t.Errorf("host asked about %q, want ^Y", asked)
	}
	if stub.sent() != "\x1b[21~" {
		t.Errorf("session received %q, want the host's encoding", stub.sent())
	}
	if got := docContent(w); got != "ab" {
		t.Errorf("buffer = %q; mew's own binding must not have run", got)
	}

	// One shot: the key after it is mew's again.
	e.dispatchKey("^Y")
	if got := docContent(w); got != "" {
		t.Errorf("buffer = %q, want ^Y to delete the line normally again", got)
	}
}

// The arm is spent by the next keystroke whether or not a terminal was there
// to take it — a raw key with no child under it is just an ordinary key, not
// one held in reserve. Likewise when the host declines to encode the name.
func TestRawKeyInputWithoutATerminalIsOrdinary(t *testing.T) {
	e, w := newTestEditor(t, "ab\n")
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 2})
	e.executeCommand("raw_key_input")
	e.dispatchKey("^Y")
	if got := docContent(w); got != "" {
		t.Errorf("buffer = %q, want ^Y to have run mew's del_line", got)
	}
	if e.rawKeyArmed {
		t.Error("the one-shot should be spent even with no terminal to take it")
	}

	// A host that cannot encode the name declines, and the key takes its
	// ordinary path rather than vanishing.
	e2, w2 := newTestEditor(t, "ab\n")
	w2.SetCursorPos(viewport.Position{Line: 0, Rune: 2})
	e2.Config.TerminalSurfaces = TerminalHooks{
		Open: func(string, int, int) {}, Feed: func(string, []byte) []byte { return nil },
		Key: func(string, string) []byte { return nil },
	}
	e2.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
	if !e2.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	e2.executeCommand("raw_key_input")
	e2.dispatchKey("^Y")
	if got := docContent(w2); got != "" {
		t.Errorf("buffer = %q, want the declined key to fall through to mew", got)
	}
}

// The shipped default binds raw_key_input, and it resolves through the same
// lookup a host menu uses to advertise a key — so the item in the Input menu
// and the key that actually runs it cannot drift apart.
func TestRawKeyInputHasAShippedBinding(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	if got := e.keyBindingDisplay("raw_key_input", `M-\`); got != `M-\` {
		t.Errorf("raw_key_input resolves to %q, want the shipped M-\\", got)
	}
}

// stubExit is a session that can also say how its child ended.
type stubExit struct {
	*stubPTY
	code   int
	exited bool
}

func (s *stubExit) ExitStatus() (int, bool) { return s.code, s.exited }

// The notification is the only account of a session anyone gets — the surface
// is destroyed in the same breath — so it says what is known: the exit code
// when the host can tell, a stream that ended under a living child when it
// can tell that instead, and the error when something broke.
func TestSessionEndedMessage(t *testing.T) {
	plain := newStubPTY()
	for _, tc := range []struct {
		name  string
		sess  PTYSession
		cause error
		want  string
	}{
		{"host cannot tell", plain, nil, "bash exited"},
		{"clean EOF is silent", plain, io.EOF, "bash exited"},
		{"a broken stream says why", plain, errors.New("pipe is dead"), "bash exited: pipe is dead"},
		{"the child exited", &stubExit{plain, 0, true}, io.EOF, "bash exited (code 0)"},
		{"...unhappily", &stubExit{plain, 1, true}, io.EOF, "bash exited (code 1)"},
		{"the stream died, not the child", &stubExit{plain, 0, false}, io.EOF,
			"bash: session ended, child still running"},
	} {
		if got := sessionEndedMessage("bash", tc.sess, tc.cause); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// pty_diag writes the host's report BOTH into the buffer and to a log file in
// the working directory — the person who most needs it is the one least able
// to get it off the machine by hand.
func TestPTYDiagWritesBufferAndLog(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.Config.PTYDiagnose = func() string { return "PROBE REPORT\n  it worked" }

	dir := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(old)

	e.executeCommand("pty_diag")
	if got := docContent(w); !strings.Contains(got, "PROBE REPORT") || !strings.Contains(got, "it worked") {
		t.Errorf("buffer = %q, want the report inserted at the caret", got)
	}
	if !strings.Contains(docContent(w), "also saved to") {
		t.Error("the buffer copy should name the log file")
	}
	data, err := os.ReadFile(filepath.Join(dir, "mew-pty-diag.log"))
	if err != nil {
		t.Fatalf("no log file in the working directory: %v", err)
	}
	if !strings.Contains(string(data), "PROBE REPORT") {
		t.Errorf("log = %q, want the report", data)
	}
}

// A host with no diagnostics says so rather than writing an empty file.
func TestPTYDiagWithoutAHostSeam(t *testing.T) {
	e, w := newTestEditor(t, "")
	e.executeCommand("pty_diag")
	if !hasWarning(e, "no terminal diagnostics") {
		t.Error("expected a warning naming the missing seam")
	}
	if docContent(w) != "" {
		t.Errorf("buffer = %q, want it untouched", docContent(w))
	}
}

// stubTraced is a session that also keeps the host's account of building it.
type stubTraced struct {
	*stubPTY
	trace []string
}

func (s *stubTraced) Diagnostics() []string   { return s.trace }
func (s *stubTraced) ExitStatus() (int, bool) { return 0, false }

// A session that produced NOTHING writes its own record: there is no output to
// look at and the surface is already gone, so the account has to be kept. It
// carries the host's step-by-step trace of the session that actually failed —
// better evidence than any probe, because it is the real one.
func TestSilentSessionRecordsItself(t *testing.T) {
	e, w := newTestEditor(t, "")
	dir := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(old)

	sess := &stubTraced{stubPTY: newStubPTY(), trace: []string{"CreatePipe: ok", "CreateProcess: ok, pid 42"}}
	e.Config.TerminalSurfaces = TerminalHooks{Open: func(string, int, int) {}, Feed: func(string, []byte) []byte { return nil }}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return sess, nil }
	if !e.execRequest("cmd.exe", "") {
		t.Fatal("exec failed")
	}
	e.ptyEnded(w.Buffer, io.EOF)

	data, err := os.ReadFile(filepath.Join(dir, "mew-pty-diag.log"))
	if err != nil {
		t.Fatalf("a silent session left no record: %v", err)
	}
	for _, want := range []string{
		"session produced no output",
		"command: cmd.exe",
		"lasted:",
		"received: 0 bytes",
		"CreateProcess: ok, pid 42",
		"child still running",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("record is missing %q\n%s", want, data)
		}
	}
	if !hasNotification(e, "recorded in") {
		t.Error("the notification should say where the record went")
	}
}

// A session that said anything at all is not that failure and leaves nothing
// behind — the log is for trouble, not for every shell anyone ever ran.
func TestTalkativeSessionRecordsNothing(t *testing.T) {
	e, w := newTestEditor(t, "")
	dir := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(old)

	e.Config.TerminalSurfaces = TerminalHooks{Open: func(string, int, int) {}, Feed: func(string, []byte) []byte { return nil }}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	e.ptyOutput(w.Buffer, []byte("$ "))
	// Old enough not to count as "ended almost immediately".
	e.ptyMu.Lock()
	e.ptySessions[w.Buffer].started = time.Now().Add(-time.Minute)
	e.ptyMu.Unlock()
	e.ptyEnded(w.Buffer, io.EOF)

	if _, err := os.Stat(filepath.Join(dir, "mew-pty-diag.log")); err == nil {
		t.Error("a session that lived and produced output should leave no log behind")
	}
}

// A session that ends almost at once is recorded even when it DID say
// something, and what it said is kept — quoted, because it is escape
// sequences. On Windows the difference between a terminal's own preamble and
// a shell's prompt is the difference between two faults.
func TestShortSessionRecordsWhatItSaid(t *testing.T) {
	e, w := newTestEditor(t, "")
	dir := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(old)

	e.Config.TerminalSurfaces = TerminalHooks{Open: func(string, int, int) {}, Feed: func(string, []byte) []byte { return nil }}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
	if !e.execRequest("cmd.exe", "") {
		t.Fatal("exec failed")
	}
	e.ptyOutput(w.Buffer, []byte("\x1b[?9001h\x1b[?1004h"))
	e.ptyEnded(w.Buffer, io.EOF)

	data, err := os.ReadFile(filepath.Join(dir, "mew-pty-diag.log"))
	if err != nil {
		t.Fatalf("a session that ended at once left no record: %v", err)
	}
	for _, want := range []string{
		"session ended almost immediately",
		"received: 16 bytes",
		`first of it: "\e[?9001h\e[?1004h"`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("record is missing %q\n%s", want, data)
		}
	}
}

// Raw output is quoted, not pasted: two hex digits always, and escapes stay
// visible rather than steering whatever displays the record.
func TestQuoteBytes(t *testing.T) {
	got := quoteBytes([]byte("hi\x1b[0m\x07\r\n"))
	want := `"hi\e[0m\x07\r\n"`
	if got != want {
		t.Errorf("quoteBytes = %s, want %s", got, want)
	}
}

// The method rides along as data. mew attaches no meaning to it — it is the
// host's word, like the command name — and it reaches the provider verbatim
// so a machine that needs an unusual one can be told without a rebuild.
func TestExecCarriesTheMethod(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	var got PTYRequest
	e.Config.PTYProvider = func(r PTYRequest) (PTYSession, error) {
		got = r
		return newStubPTY(), nil
	}
	// The method is a SWITCH now, left of the program name, and the whole line
	// is re-parsed by argwild — the same notation the host app boots with.
	e.executeCommand(`exec "--pty=3 cmd.exe"`)
	if got.Command != "cmd.exe" || got.Method != "3" {
		t.Errorf("request = %+v, want cmd.exe via method 3", got)
	}
	if len(got.Args) != 0 {
		t.Errorf("no arguments were given; got %q", got.Args)
	}
	if !hasNotification(e, "method 3") {
		t.Error("the notification should name a non-default method")
	}

	// The arguments are composited across PawScript arguments too, so the
	// caller may split the line wherever is convenient.
	e3, _ := newTestEditor(t, "x\n")
	var got3 PTYRequest
	e3.Config.PTYProvider = func(r PTYRequest) (PTYSession, error) {
		got3 = r
		return newStubPTY(), nil
	}
	e3.executeCommand(`exec "--pty=pipe_only", "--", "bash", "-l"`)
	if got3.Command != "bash" || got3.Method != "pipe_only" {
		t.Errorf("request = %+v, want bash via pipe_only", got3)
	}
	if len(got3.Args) != 1 || got3.Args[0] != "-l" {
		t.Errorf("args = %q, want [-l]", got3.Args)
	}
	if !hasNotification(e3, "Started bash -l") {
		t.Error("the notification should show the child's arguments")
	}

	// No method named is the host's default, and says nothing about it.
	e2, _ := newTestEditor(t, "x\n")
	var got2 PTYRequest
	e2.Config.PTYProvider = func(r PTYRequest) (PTYSession, error) {
		got2 = r
		return newStubPTY(), nil
	}
	e2.executeCommand(`exec "bash"`)
	if got2.Method != "" {
		t.Errorf("method = %q, want empty for the host's default", got2.Method)
	}
	if !hasNotification(e2, "Started bash") || hasNotification(e2, "method") {
		t.Error("a default-method start should not mention a method")
	}
}

// A terminal whose application is NOT tracking the mouse still wants the
// event: its own scrollbar, scrollback and selection are all worked by the
// mouse. The host says handled with no bytes, and mew must consume it rather
// than falling through to its own click handling.
func TestPTYMouseHandledWithoutBytes(t *testing.T) {
	e, w := newTestEditor(t, "hello\n")
	w.ContentX, w.ContentY, w.ContentWidth, w.ContentHeight = 4, 2, 40, 10
	stub := newStubPTY()
	asked := 0
	e.Config.TerminalSurfaces = TerminalHooks{
		Open: func(string, int, int) {}, Feed: func(string, []byte) []byte { return nil },
		// The shape of a terminal using the event itself.
		Mouse: func(string, TerminalMouse) ([]byte, bool) { asked++; return nil, true },
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}

	e.handleMouseKey("Mouse@6,3")
	e.handleMouseKey("MouseLeftPress")
	if asked != 1 {
		t.Fatalf("host asked %d times, want once", asked)
	}
	if e.dragSel.active {
		t.Error("mew ran its own press handling for an event the terminal took")
	}
	if stub.sent() != "" {
		t.Errorf("nothing should reach the child, sent %q", stub.sent())
	}
}

// The rectangle mew places a surface in is not the grid the host renders
// inside it: a graphical terminal spends some of that width on its scrollbar
// lane and rounds the rest down to whole cells of its own font. Sizing the
// child process to the rectangle tells a guest it has columns the display will
// not show it, and every full-width line it draws then wraps and scrolls a
// screenful into scrollback — per frame, without bound. So the host declares
// what it really renders, and that wins.
func TestDeclaredGridBeatsThePlacementRectangle(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	stub := newStubPTY()
	var opened string
	e.Config.TerminalSurfaces = TerminalHooks{
		Open:  func(id string, _, _ int) { opened = id },
		Feed:  func(string, []byte) []byte { return nil },
		Place: func([]TerminalSurface) {},
		Close: func(string) {},
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	w.ContentX, w.ContentY, w.ContentWidth, w.ContentHeight = 0, 0, 80, 24
	e.notifyTerminalSurfaces()
	if stub.cols != 80 || stub.rows != 24 {
		t.Fatalf("placement sized the child %dx%d, want 80x24", stub.cols, stub.rows)
	}

	// The host, having painted, reports the grid it actually renders.
	if !e.SetTerminalGrid(opened, 78, 24) {
		t.Fatal("no session by that id")
	}
	if stub.cols != 78 {
		t.Errorf("child is %d cols, want the declared 78", stub.cols)
	}

	// A surface that only MOVES keeps its declaration: same size, same
	// answer, and re-deriving it would resize the child for nothing.
	w.ContentY = 1
	e.notifyTerminalSurfaces()
	if stub.cols != 78 {
		t.Errorf("a move undid the declaration: child is %d cols, want 78", stub.cols)
	}

	// A RESIZE does drop it — the declaration described the old rectangle —
	// and the child follows the new one until the host declares again.
	w.ContentWidth = 100
	e.notifyTerminalSurfaces()
	if stub.cols != 100 {
		t.Errorf("child is %d cols, want the new rectangle's 100 until redeclared", stub.cols)
	}
	if !e.SetTerminalGrid(opened, 98, 24) {
		t.Fatal("no session by that id")
	}
	if stub.cols != 98 {
		t.Errorf("child is %d cols, want the redeclared 98", stub.cols)
	}
}

// An unchanged declaration does not resize the child again: a resize wakes the
// guest, and waking it once a frame is its own runaway.
func TestRedeclaringTheSameGridIsQuiet(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	stub := newStubPTY()
	var opened string
	e.Config.TerminalSurfaces = TerminalHooks{
		Open:  func(id string, _, _ int) { opened = id },
		Feed:  func(string, []byte) []byte { return nil },
		Place: func([]TerminalSurface) {},
		Close: func(string) {},
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	w.ContentWidth, w.ContentHeight = 80, 24
	e.notifyTerminalSurfaces()

	e.SetTerminalGrid(opened, 78, 24)
	before := stub.resizes
	e.SetTerminalGrid(opened, 78, 24)
	e.SetTerminalGrid(opened, 78, 24)
	if stub.resizes != before {
		t.Errorf("re-declaring an unchanged grid resized the child %d extra times", stub.resizes-before)
	}
}

// A declaration for a session that does not exist is refused rather than
// silently recorded — the host has no business inventing ids.
func TestDeclaringAGridForNoSession(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	if e.SetTerminalGrid("pty404", 78, 24) {
		t.Error("accepted a grid for a session that does not exist")
	}
	if e.SetTerminalGrid("pty1", 0, 24) {
		t.Error("accepted a zero-width grid")
	}
}

// A NAMED key with no binding used to vanish. mew's defaults cover a typed
// character (insert, which already routes to a terminal) and a handful of
// named keys, but F10, ins, pgup and every function key resolved to no command
// at all and were dropped on the floor.
//
// In an ordinary buffer that is right — there is nothing to do with F10. In a
// viewport hosting a terminal it is wrong: there IS something to do with it,
// one level in. This is why arming Raw Key Input and pressing F10 still did
// nothing even once the host stopped eating it: mew took delivery and dropped
// it.
func TestUnhandledNamedKeyReachesTheChild(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	stub := newStubPTY()
	e.Config.TerminalSurfaces = TerminalHooks{
		Open: func(string, int, int) {}, Feed: func(string, []byte) []byte { return nil },
		Key: func(_ string, key string) []byte {
			if key == "F10" {
				return []byte("\x1b[21~")
			}
			return nil
		},
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	e.reconcileFocusedOptions() // adopt the pty class keymap

	e.dispatchKey("F10")
	if got := stub.sent(); got != "\x1b[21~" {
		t.Errorf("child received %q, want the encoded F10 — an unbound named key must not vanish in a terminal", got)
	}
}

// The capture level takes BOUND keys too — that is what it is for. ^Y is
// del_line at level 0; in a pty viewport the wildcard outranks it, the child
// receives the encoded key, and the buffer is untouched.
func TestCaptureTakesABoundKeyInAPTYViewport(t *testing.T) {
	e, w := newTestEditor(t, "ab\n")
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 2})
	stub := newStubPTY()
	e.Config.TerminalSurfaces = TerminalHooks{
		Open: func(string, int, int) {}, Feed: func(string, []byte) []byte { return nil },
		Key: func(_ string, key string) []byte { return []byte("<" + key + ">") },
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	e.reconcileFocusedOptions()
	e.dispatchKey("^Y")
	if got := stub.sent(); got != "<^Y>" {
		t.Errorf("child received %q, want the encoded ^Y — the capture must win over del_line", got)
	}
	if got := docContent(w); got != "ab" {
		t.Errorf("buffer = %q, want untouched — the key belonged to the child", got)
	}
}

// A sequence STARTER resolves to nothing YET, which is not the same as
// resolving to nothing at all: the chord in progress outranks the wildcard
// (rule 1), so ^K is held, not forwarded. Only if the sequence comes to
// nothing is the starter released to the child.
func TestSequenceStepsAreNotForwarded(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	stub := newStubPTY()
	e.Config.TerminalSurfaces = TerminalHooks{
		Open: func(string, int, int) {}, Feed: func(string, []byte) []byte { return nil },
		Key: func(_ string, key string) []byte { return []byte("<" + key + ">") },
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	e.reconcileFocusedOptions() // the wildcard is live; the hold must still win

	// ^K starts a sequence in the default keymap.
	e.dispatchKey("^K")
	if e.KeyProcessor.GetActiveSequence() == "" {
		t.Skip("^K is not a sequence starter in this keymap")
	}
	if got := stub.sent(); got != "" {
		t.Errorf("a mid-sequence key was forwarded: child received %q", got)
	}

	// An invalid continuation unwinds: the held ^K is released to the child,
	// and the invalid key follows it — "pass them on individually".
	e.dispatchKey("F9")
	if got := stub.sent(); got != "<^K><F9>" {
		t.Errorf("after the unwind the child received %q, want %q", got, "<^K><F9>")
	}
}

// Without a terminal, an unbound named key is dropped exactly as before —
// this changes what happens in a pty viewport and nowhere else.
func TestUnhandledNamedKeyWithoutATerminalIsStillDropped(t *testing.T) {
	e, w := newTestEditor(t, "ab\n")
	w.SetCursorPos(viewport.Position{Line: 0, Rune: 2})
	before := docContent(w)
	e.dispatchKey("F10")
	if got := docContent(w); got != before {
		t.Errorf("buffer = %q, want F10 to remain a no-op outside a terminal", got)
	}
}

// THE MOTIVATING CASE: arrow keys. Every one of them is bound at level 0
// (cursor movement), and every one of them must reach a hosted terminal —
// through the host's encoder, so application cursor mode decides the bytes.
// This is the whole reason the capture level exists.
func TestArrowKeysReachTheChildThroughTheCapture(t *testing.T) {
	e, w := newTestEditor(t, "one\ntwo\n")
	w.SetCursorPos(viewport.Position{Line: 1, Rune: 0})
	stub := newStubPTY()
	e.Config.TerminalSurfaces = TerminalHooks{
		Open: func(string, int, int) {}, Feed: func(string, []byte) []byte { return nil },
		Key: func(_ string, key string) []byte { return []byte("<" + key + ">") },
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	e.reconcileFocusedOptions()

	for _, key := range []string{"up", "down", "left", "right", "home", "end", "pgup", "pgdn"} {
		e.dispatchKey(key)
	}
	want := "<up><down><left><right><home><end><pgup><pgdn>"
	if got := stub.sent(); got != want {
		t.Errorf("child received %q, want %q", stub.sent(), want)
	}
	if p := w.CursorPos(); p.Line != 1 || p.Rune != 0 {
		t.Errorf("cursor moved to %+v; the keys belonged to the child", w.CursorPos())
	}
}

// The reclaim idiom, end to end through real config and real PawScript:
// "capture ^C = false" in a user's pty section gives ^C back to mew while a
// terminal is focused — it skips the capture level (its own wildcard
// included) and lands on ^C's classic floor, which closes the pty buffer.
func TestUserReclaimGivesAKeyBackToMew(t *testing.T) {
	e, _ := newTestEditor(t, "")
	stub := newStubPTY()
	e.Config.TerminalSurfaces = TerminalHooks{
		Open: func(string, int, int) {}, Feed: func(string, []byte) []byte { return nil },
		Close: func(string) {},
		Key:   func(_ string, key string) []byte { return []byte("<" + key + ">") },
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	// The user's line, over the defaults: reclaim ^C from the capture layer.
	// (Config-to-keymap resolution for the pty overlay is covered by
	// TestPTYClassKeyBindings; here the reclaim is applied where it resolves
	// to, so the test exercises the part that matters: pawscript's REAL false
	// through the descent.) The level-0 binding is given an effect that lands
	// in the stub, so one string proves both halves: no "<^C>" — the capture
	// layer and its wildcard were skipped — and the level-0 binding ran.
	e.reconcileFocusedOptions()
	e.KeyProcessor.MapKey("capture ^C", "false")
	e.KeyProcessor.MapKey("^C", "tinput \"RECLAIMED\"")

	e.dispatchKey("^C")
	if got := stub.sent(); got != "RECLAIMED" {
		t.Errorf("child received %q, want %q — the reclaimed ^C belongs to mew's own level-0 binding", got, "RECLAIMED")
	}
}

// THE ESCAPE HATCH must survive the capture layer. raw_key_input exists to
// send a child the keys mew holds as possible chords (a literal ^B to a
// shell); if the wildcard swallowed its own key there would be no way to
// reach those keys at all. It is NAMED in [pty::mappings], so within the
// capture level a named key beats the wildcard.
func TestRawKeyInputSurvivesTheCaptureLayer(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	stub := newStubPTY()
	e.Config.TerminalSurfaces = TerminalHooks{
		Open: func(string, int, int) {}, Feed: func(string, []byte) []byte { return nil },
		Key: func(_ string, key string) []byte { return []byte("<" + key + ">") },
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	e.reconcileFocusedOptions()

	e.dispatchKey(`M-\`)
	if got := stub.sent(); got != "" {
		t.Fatalf("the escape hatch went to the child as %q; it must arm mew", got)
	}
	if !e.rawKeyArmed {
		t.Fatal("M-\\ should have armed raw key input, not been captured")
	}

	// And what it arms reaches the child even though mew would otherwise hold
	// ^B as a sequence prefix — which is the entire point of the hatch.
	e.dispatchKey("^B")
	if got := stub.sent(); got != "<^B>" {
		t.Errorf("child received %q, want %q — the armed key must bypass the sequence hold", got, "<^B>")
	}
	if e.KeyProcessor.GetActiveSequence() != "" {
		t.Error("the armed key must not have opened a sequence")
	}
}

// Escape reaches the child in a terminal viewport, which vim and every other
// full-screen program needs. It cannot be had from the wildcard alone: esc X,
// esc y and esc j make esc a PREFIX, and a chord in progress outranks any
// single-key claim — so esc would be held while the terminal waited. Naming
// esc in [pty::mappings] suppresses those chords for as long as a terminal
// has the focus.
func TestEscapeReachesTheChildInATerminal(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	stub := newStubPTY()
	e.Config.TerminalSurfaces = TerminalHooks{
		Open: func(string, int, int) {}, Feed: func(string, []byte) []byte { return nil },
		Key: func(_ string, key string) []byte { return []byte("<" + key + ">") },
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	e.reconcileFocusedOptions()

	e.dispatchKey("esc")
	if got := e.KeyProcessor.GetActiveSequence(); got != "" {
		t.Fatalf("esc was held as a prefix (%q); the terminal never gets it", got)
	}
	if got := stub.sent(); got != "<esc>" {
		t.Errorf("child received %q, want %q", got, "<esc>")
	}
}

// ...and outside a terminal the esc-chords are exactly as they were: the
// suppression is scoped to the pty class, not global.
func TestEscChordsSurviveOutsideATerminal(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	e.reconcileFocusedOptions()
	e.dispatchKey("esc")
	if got := e.KeyProcessor.GetActiveSequence(); got != "esc" {
		t.Errorf("activeSequence = %q, want esc held as a prefix outside a terminal", got)
	}
}

// A viewport hosting a terminal does not ask for a caret. The CHILD's cursor
// is the one that means anything there — it sits at the shell's insertion
// point, in the shape the program asked for — while mew's own caret sits
// wherever the document caret happens to be. Requesting both puts two
// blinking cursors on a graphical host, one of them lying about where typing
// goes.
func TestPTYViewportAsksForNoCaret(t *testing.T) {
	e, w := newTestEditor(t, "hello\n")
	if e.caretHidden(w) {
		t.Fatal("an ordinary viewport should own its caret")
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
	if !e.execRequest("bash", "") {
		t.Fatal("exec failed")
	}
	if !e.caretHidden(w) {
		t.Error("a viewport hosting a terminal must not ask for a caret too")
	}
}

// The shell command asks the HOST to name the program: Shell is set and Command
// is empty, because which shell a user logs in with belongs to the operating
// system and to their account, and mew can see neither.
func TestShellCommandAsksTheHostToNameIt(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	var got PTYRequest
	e.Config.PTYProvider = func(r PTYRequest) (PTYSession, error) {
		got = r
		return newStubPTY(), nil
	}
	e.executeCommand(`shell`)
	if !got.Shell {
		t.Error("Shell should be set")
	}
	if got.Command != "" {
		t.Errorf("Command = %q, want empty — the host names it", got.Command)
	}
	// The notification says what was ASKED for, not a shell name mew invented.
	if !hasNotification(e, "the login shell") {
		t.Error("the notification should say the login shell was asked for")
	}
}

// Everything else about the line is exec's: --pty=, the phase rules, named
// arguments. What differs is only that the first operand is an ARGUMENT.
func TestShellCommandCarriesMethodAndArgs(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	var got PTYRequest
	e.Config.PTYProvider = func(r PTYRequest) (PTYSession, error) {
		got = r
		return newStubPTY(), nil
	}
	e.executeCommand(`shell "--pty=pipe_only", "--", "-l", "-i"`)
	if !got.Shell || got.Command != "" {
		t.Errorf("request = %+v, want a shell request with no command", got)
	}
	if got.Method != "pipe_only" {
		t.Errorf("Method = %q, want pipe_only", got.Method)
	}
	if len(got.Args) != 2 || got.Args[0] != "-l" || got.Args[1] != "-i" {
		t.Errorf("args = %q, want [-l -i]", got.Args)
	}

	// The named-argument spelling reaches the same request.
	e2, _ := newTestEditor(t, "x\n")
	var got2 PTYRequest
	e2.Config.PTYProvider = func(r PTYRequest) (PTYSession, error) {
		got2 = r
		return newStubPTY(), nil
	}
	e2.executeCommand(`shell pty: pipe_only, "-- -c make"`)
	if got2.Method != "pipe_only" || !got2.Shell {
		t.Errorf("request = %+v, want a shell request via pipe_only", got2)
	}
	if len(got2.Args) != 2 || got2.Args[0] != "-c" || got2.Args[1] != "make" {
		t.Errorf("args = %q, want [-c make]", got2.Args)
	}
}

// A shell request must not also name a program: honouring one would make shell
// into exec while still reading as though the host had chosen.
func TestShellCommandRefusesAProgram(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	called := false
	e.Config.PTYProvider = func(r PTYRequest) (PTYSession, error) {
		called = true
		return newStubPTY(), nil
	}
	e.executeCommand(`shell program: bash`)
	if called {
		t.Error("the host should not have been asked for anything")
	}
}
