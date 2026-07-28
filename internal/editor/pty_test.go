package editor

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

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
func (s *stubPTY) Resize(c, r int) error { s.cols, s.rows = c, r; return nil }
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

	if !e.execRequest("bash") {
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
	if !e.execRequest("zsh") {
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
	if e.execRequest("bash") {
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
	if e.execRequest("bash") {
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
	if !e.execRequest("bash") {
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
		if !e.execRequest("bash") {
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
		Feed:  func(string, []byte) {},
		Place: func(s []TerminalSurface) { placed = s },
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
	if !e.execRequest("bash") {
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
	if !e.execRequest("bash") {
		t.Fatal("first exec failed")
	}
	if e.execRequest("zsh") {
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
		Feed: func(id string, p []byte) { fedID = id; fed = append(fed, p...) },
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
	if !e.execRequest("bash") {
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
		Feed:  func(string, []byte) {},
		Place: func(s []TerminalSurface) { placed = append(placed, s) },
		Close: func(string) {},
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }

	e.notifyTerminalSurfaces()
	if len(placed) != 0 {
		t.Error("no sessions: nothing should be published")
	}

	if !e.execRequest("bash") {
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
	if !e.execRequest("bash") {
		t.Fatal("exec failed")
	}
	e.ptyEnded(w.Buffer)
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
	if !e2.execRequest("bash") {
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
	if !e2.execRequest("bash") {
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
	if !e.execRequest("bash") {
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
		if !e.execRequest("bash") {
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
	if !e.execRequest("bash") {
		t.Fatal("exec failed")
	}
	if got := e.viewportClass(w); got != "pty" {
		t.Fatalf("class with a session = %q, want pty", got)
	}
	e.ptyEnded(w.Buffer)
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
		e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
		if !e.execRequest("bash") {
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
		Feed:  func(string, []byte) {},
		Place: func(s []TerminalSurface) { placed = append(placed, s) },
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
	if !e.execRequest("bash") {
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
		Open: func(string, int, int) {}, Feed: func(string, []byte) {},
		Mouse: func(id string, ev TerminalMouse) []byte {
			gotID, got = id, ev
			return []byte("\x1b[<0;1;1M")
		},
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
	if !e.execRequest("bash") {
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
			Open: func(string, int, int) {}, Feed: func(string, []byte) {},
			Mouse: func(_ string, ev TerminalMouse) []byte { got = ev; return []byte{'x'} },
		}
		e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
		if !e.execRequest("bash") {
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
			Open: func(string, int, int) {}, Feed: func(string, []byte) {},
			Mouse: func(string, TerminalMouse) []byte { asked++; return reply },
		}
		e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
		if !e.execRequest("bash") {
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
		Open: func(string, int, int) {}, Feed: func(string, []byte) {},
		Key: func(_ string, key string) []byte { asked = key; return []byte("\x1b[21~") },
	}
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return stub, nil }
	if !e.execRequest("bash") {
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
		Open: func(string, int, int) {}, Feed: func(string, []byte) {},
		Key: func(string, string) []byte { return nil },
	}
	e2.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
	if !e2.execRequest("bash") {
		t.Fatal("exec failed")
	}
	e2.executeCommand("raw_key_input")
	e2.dispatchKey("^Y")
	if got := docContent(w2); got != "" {
		t.Errorf("buffer = %q, want the declined key to fall through to mew", got)
	}
}
