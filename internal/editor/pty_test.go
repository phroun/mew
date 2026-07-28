package editor

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
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
		{"tinput {bytes 0x1b5b41}", "\x1b[A"}, // Up: ESC [ A as one value
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
