package editor

import (
	"testing"

	"github.com/phroun/mew/internal/viewport"
)

// The --size / --minimum / --hidden switches govern a session's logical size.
// They are parsed by the same setOption path as every other exec/shell option,
// so both the switch form and the named-argument form are exercised here.

func TestParseSizeSwitches(t *testing.T) {
	type want struct {
		mode       sizeMode
		cols, rows int
		hidden     bool
		program    string
		nArgs      int
	}
	run := func(t *testing.T, spec execSpec, err error, w want) {
		t.Helper()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if spec.SizeMode != w.mode {
			t.Errorf("SizeMode = %d, want %d", spec.SizeMode, w.mode)
		}
		if spec.SizeCols != w.cols || spec.SizeRows != w.rows {
			t.Errorf("size = %dx%d, want %dx%d", spec.SizeCols, spec.SizeRows, w.cols, w.rows)
		}
		if spec.Hidden != w.hidden {
			t.Errorf("Hidden = %v, want %v", spec.Hidden, w.hidden)
		}
		if spec.Program != w.program {
			t.Errorf("Program = %q, want %q", spec.Program, w.program)
		}
		if len(spec.Args) != w.nArgs {
			t.Errorf("Args = %v (len %d), want len %d", spec.Args, len(spec.Args), w.nArgs)
		}
	}

	t.Run("exact switch", func(t *testing.T) {
		s, err := parseExecLine("--size=80x25 bash")
		run(t, s, err, want{mode: sizeExact, cols: 80, rows: 25, program: "bash"})
	})
	t.Run("minimum switch", func(t *testing.T) {
		s, err := parseExecLine("--minimum=100x40 bash")
		run(t, s, err, want{mode: sizeMinimum, cols: 100, rows: 40, program: "bash"})
	})
	t.Run("hidden valued pins and hides", func(t *testing.T) {
		s, err := parseExecLine("--hidden=90x30 bash")
		run(t, s, err, want{mode: sizeExact, cols: 90, rows: 30, hidden: true, program: "bash"})
	})
	t.Run("hidden bare is a flag; size defaults at policy time", func(t *testing.T) {
		s, err := parseExecLine("--hidden bash")
		run(t, s, err, want{mode: sizeFollow, hidden: true, program: "bash"})
		// No visible tile to follow, so the resolved policy takes a definite 80x25.
		if p := s.sizePolicy(); p.mode != sizeExact || p.cols != 80 || p.rows != 25 || !p.hidden {
			t.Errorf("hidden sizePolicy = %+v, want exact 80x25 hidden", p)
		}
	})
	t.Run("hidden composes with an explicit size", func(t *testing.T) {
		// Orthogonal now: --size sets the size, --hidden sets visibility.
		s, err := parseExecLine("--size=100x40 --hidden bash")
		run(t, s, err, want{mode: sizeExact, cols: 100, rows: 40, hidden: true, program: "bash"})
	})
	t.Run("default follows the tile", func(t *testing.T) {
		s, err := parseExecLine("bash")
		run(t, s, err, want{mode: sizeFollow, program: "bash"})
	})
	t.Run("switch after program is the child's", func(t *testing.T) {
		// Only mew's half of the line (before the program) is mew's; a --size
		// after the program name belongs to the child, verbatim.
		s, err := parseExecLine("bash --size=80x25")
		run(t, s, err, want{mode: sizeFollow, program: "bash", nArgs: 1})
	})
	t.Run("named-argument form", func(t *testing.T) {
		s, err := parseExecLineNamed("bash", map[string]interface{}{"size": "70x20"})
		run(t, s, err, want{mode: sizeExact, cols: 70, rows: 20, program: "bash"})
	})
	t.Run("hidden false named is a no-op", func(t *testing.T) {
		s, err := parseExecLineNamed("bash", map[string]interface{}{"hidden": "false"})
		run(t, s, err, want{mode: sizeFollow, program: "bash"})
	})
	t.Run("shell form", func(t *testing.T) {
		s, err := parseShellLineNamed("--size=80x25", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !s.Shell {
			t.Error("Shell = false, want true")
		}
		if s.SizeMode != sizeExact || s.SizeCols != 80 || s.SizeRows != 25 {
			t.Errorf("got mode=%d %dx%d, want exact 80x25", s.SizeMode, s.SizeCols, s.SizeRows)
		}
	})
}

func TestPtySizePolicyResolve(t *testing.T) {
	follow := ptySizePolicy{mode: sizeFollow}
	exact := ptySizePolicy{mode: sizeExact, cols: 80, rows: 25}
	minimum := ptySizePolicy{mode: sizeMinimum, cols: 80, rows: 25}

	check := func(name string, gotC, gotR, wantC, wantR int) {
		t.Helper()
		if gotC != wantC || gotR != wantR {
			t.Errorf("%s = %dx%d, want %dx%d", name, gotC, gotR, wantC, wantR)
		}
	}

	// follow: the visible size is the logical size; the host is told 0,0.
	c, r := follow.resolveLogical(50, 20)
	check("follow.resolveLogical(50,20)", c, r, 50, 20)
	c, r = follow.hostLogical(50, 20)
	check("follow.hostLogical(50,20)", c, r, 0, 0)

	// exact: the pinned size regardless of the tile, both to child and host.
	c, r = exact.resolveLogical(50, 20)
	check("exact.resolveLogical(small)", c, r, 80, 25)
	c, r = exact.resolveLogical(200, 60)
	check("exact.resolveLogical(large)", c, r, 80, 25)
	c, r = exact.hostLogical(200, 60)
	check("exact.hostLogical", c, r, 80, 25)

	// minimum: a per-axis floor that still grows with the tile.
	c, r = minimum.resolveLogical(50, 20)
	check("minimum below floor", c, r, 80, 25)
	c, r = minimum.resolveLogical(200, 60)
	check("minimum above floor", c, r, 200, 60)
	c, r = minimum.resolveLogical(100, 10) // wide enough, too short
	check("minimum per-axis", c, r, 100, 25)
	c, r = minimum.hostLogical(50, 20)
	check("minimum.hostLogical", c, r, 80, 25)
}

// End to end: --size pins the child's size to the logical size no matter how
// large or small the tile is, and tells the host that logical size once.
func TestExecSizePinsChildAndLogical(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	var pty *stubPTY
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { pty = newStubPTY(); return pty, nil }
	type logCall struct {
		id         string
		cols, rows int
	}
	var logs []logCall
	e.Config.TerminalSurfaces = TerminalHooks{
		Open:           func(string, int, int) {},
		SetLogicalSize: func(id string, c, r int) { logs = append(logs, logCall{id, c, r}) },
		Feed:           func(string, []byte) []byte { return nil },
		Place:          func([]TerminalSurface) {},
	}
	if !e.execRequestArgsPolicy("bash", nil, "", ptySizePolicy{mode: sizeExact, cols: 80, rows: 25}, captureOff, captureFull) {
		t.Fatal("exec failed")
	}
	// The host is told the pinned logical size up front.
	if len(logs) != 1 || logs[0].cols != 80 || logs[0].rows != 25 {
		t.Fatalf("initial SetLogicalSize = %+v, want one call of 80x25", logs)
	}
	// A tile SMALLER than the pinned size: the child is still sized to 80x25,
	// not the 50x20 tile, and the logical size (unchanged) is not re-sent.
	w.ContentX, w.ContentY, w.ContentWidth, w.ContentHeight = 0, 0, 50, 20
	e.notifyTerminalSurfaces()
	if pty == nil {
		t.Fatal("no session pty captured")
	}
	if pty.cols != 80 || pty.rows != 25 {
		t.Errorf("child resized to %dx%d, want 80x25 (pinned, not the 50x20 tile)", pty.cols, pty.rows)
	}
	if len(logs) != 1 {
		t.Errorf("logical size re-sent %d times, want 1 (unchanged)", len(logs))
	}
}

// The default follow policy is behavior-neutral: the child is sized to the tile
// and the host is told logical 0,0 exactly once (at attach), never again.
func TestExecFollowSizesToTile(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	var pty *stubPTY
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { pty = newStubPTY(); return pty, nil }
	logs := 0
	e.Config.TerminalSurfaces = TerminalHooks{
		Open:           func(string, int, int) {},
		SetLogicalSize: func(string, int, int) { logs++ },
		Feed:           func(string, []byte) []byte { return nil },
		Place:          func([]TerminalSurface) {},
	}
	if !e.execRequest("bash", "") { // follow policy (zero ptySizePolicy)
		t.Fatal("exec failed")
	}
	w.ContentX, w.ContentY, w.ContentWidth, w.ContentHeight = 0, 0, 50, 20
	e.notifyTerminalSurfaces()
	if pty == nil {
		t.Fatal("no session pty captured")
	}
	if pty.cols != 50 || pty.rows != 20 {
		t.Errorf("follow child resized to %dx%d, want 50x20 (the tile)", pty.cols, pty.rows)
	}
	if logs != 1 {
		t.Errorf("follow SetLogicalSize called %d times, want 1 (0,0 at attach)", logs)
	}
}

func TestParseCaptureSwitch(t *testing.T) {
	rungs := []struct {
		line string
		want captureRung
	}{
		{"--capture bash", captureFinal}, // bare = final
		{"--capture=final bash", captureFinal},
		{"--capture=off bash", captureOff},
		{"bash", captureUnset}, // unspecified stays distinct from off
	}
	for _, c := range rungs {
		s, err := parseExecLine(c.line)
		if err != nil {
			t.Errorf("parseExecLine(%q) error: %v", c.line, err)
			continue
		}
		if s.Capture != c.want {
			t.Errorf("parseExecLine(%q) capture = %d, want %d", c.line, s.Capture, c.want)
		}
	}

	formats := []struct {
		line string
		want captureFormat
	}{
		{"--capture=final bash", captureFull}, // default keeps everything
		{"--capture=final --plain bash", capturePlain},
		{"--capture=final --text bash", captureText},
	}
	for _, c := range formats {
		s, err := parseExecLine(c.line)
		if err != nil {
			t.Errorf("parseExecLine(%q) error: %v", c.line, err)
			continue
		}
		if s.CaptureFormat != c.want {
			t.Errorf("parseExecLine(%q) format = %d, want %d", c.line, s.CaptureFormat, c.want)
		}
	}

	// The named-argument form maps the same way.
	if s, err := parseExecLineNamed("bash", map[string]interface{}{"capture": "final"}); err != nil || s.Capture != captureFinal {
		t.Errorf("named capture: got %d err %v, want final", s.Capture, err)
	}

	// live is the highest rung and now parses to captureLive.
	if s, err := parseExecLine("--capture=live bash"); err != nil || s.Capture != captureLive {
		t.Errorf("parseExecLine(--capture=live) = %d err %v, want live", s.Capture, err)
	}

	// An unknown rung and two conflicting formats each error rather than
	// silently doing the wrong thing.
	for _, bad := range []string{
		"--capture=nonsense bash",
		"--capture=final --plain --text bash",
	} {
		if _, err := parseExecLine(bad); err == nil {
			t.Errorf("parseExecLine(%q) = nil error, want an error", bad)
		}
	}
}

// Capture-on-die (the final rung) folds the session's transcript into its buffer
// when it ends: full keeps the escape stream, plain drops the SGR styling on our
// side, text asks the host for the stripped form. off never asks for a snapshot.
func TestCaptureOnDieFillsBuffer(t *testing.T) {
	run := func(name string, rung captureRung, format captureFormat, wantANSI bool, snap, want string) {
		t.Run(name, func(t *testing.T) {
			e, w := newTestEditor(t, "")
			e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
			var askedANSI *bool
			e.Config.TerminalSurfaces = TerminalHooks{
				Open:  func(string, int, int) {},
				Feed:  func(string, []byte) []byte { return nil },
				Place: func([]TerminalSurface) {},
				Close: func(string) {},
				Snapshot: func(_ string, a bool) string {
					askedANSI = &a
					return snap
				},
			}
			if !e.execRequestArgsPolicy("bash", nil, "", ptySizePolicy{}, rung, format) {
				t.Fatal("exec failed")
			}
			e.ptyEnded(w.Buffer, nil)
			if got := w.Buffer.GetContent(); got != want {
				t.Errorf("buffer = %q, want %q", got, want)
			}
			switch {
			case rung == captureOff && askedANSI != nil:
				t.Error("capture off should not ask for a snapshot")
			case rung != captureOff && askedANSI == nil:
				t.Error("capture on should ask for a snapshot")
			case rung != captureOff && *askedANSI != wantANSI:
				t.Errorf("snapshot ansi = %v, want %v", *askedANSI, wantANSI)
			}
		})
	}
	run("full keeps", captureFinal, captureFull, true, "\x1b[31mred\x1b[0m\n", "\x1b[31mred\x1b[0m\n")
	run("text strips", captureFinal, captureText, false, "plain transcript\n", "plain transcript\n")
	run("plain drops SGR", captureFinal, capturePlain, true, "\x1b[31mred\x1b[0m\n", "red\n")
	run("off is empty", captureOff, captureFull, false, "should not appear\n", "")
}

// Capture-on-die lands the transcript at the caret the session was launched
// from — folding into the existing document there — not at 0,0.
func TestCaptureOnDieLandsAtCaret(t *testing.T) {
	e, w := newTestEditor(t, "one\ntwo\n")
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
	e.Config.TerminalSurfaces = TerminalHooks{
		Open:     func(string, int, int) {},
		Feed:     func(string, []byte) []byte { return nil },
		Place:    func([]TerminalSurface) {},
		Close:    func(string) {},
		Snapshot: func(string, bool) string { return "CAP\n" },
	}
	w.Caret.Seek(1, 0) // start of the second line, not the top of the buffer
	if !e.execRequestArgsPolicy("bash", nil, "", ptySizePolicy{}, captureFinal, captureFull) {
		t.Fatal("exec failed")
	}
	e.ptyEnded(w.Buffer, nil)
	if got, want := w.Buffer.GetContent(), "one\nCAP\ntwo\n"; got != want {
		t.Fatalf("buffer = %q, want %q (transcript should land at the caret)", got, want)
	}
}

// rawEditor wires an editor whose capture sink is captured, and starts a raw
// session, returning the sink the host would relay purfecterm's OnOutput to.
func rawEditor(t *testing.T, content string, format captureFormat) (*Editor, *viewport.Viewport, CaptureSink) {
	t.Helper()
	e, w := newTestEditor(t, content)
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
	var sink CaptureSink
	e.Config.TerminalSurfaces = TerminalHooks{
		Open:           func(string, int, int) {},
		Feed:           func(string, []byte) []byte { return nil },
		Place:          func([]TerminalSurface) {},
		Close:          func(string) {},
		SetCaptureSink: func(_ string, s CaptureSink) { sink = s },
	}
	if !e.execRequestArgsPolicy("bash", nil, "", ptySizePolicy{}, captureRaw, format) {
		t.Fatal("exec failed")
	}
	if sink == nil {
		t.Fatal("raw rung did not register a capture sink")
	}
	return e, w, sink
}

// The raw rung folds live output in at the launch caret as it arrives, and full
// fidelity keeps the escape stream verbatim.
func TestCaptureRawStreamsAtCaret(t *testing.T) {
	e, w := newTestEditor(t, "one\ntwo\n")
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
	var sink CaptureSink
	e.Config.TerminalSurfaces = TerminalHooks{
		Open:           func(string, int, int) {},
		Feed:           func(string, []byte) []byte { return nil },
		Place:          func([]TerminalSurface) {},
		Close:          func(string) {},
		SetCaptureSink: func(_ string, s CaptureSink) { sink = s },
	}
	w.Caret.Seek(1, 0) // launch at the start of the second line
	if !e.execRequestArgsPolicy("bash", nil, "", ptySizePolicy{}, captureRaw, captureFull) {
		t.Fatal("exec failed")
	}
	if sink == nil {
		t.Fatal("raw rung did not register a capture sink")
	}
	sink.Output([]byte("A"))
	sink.Output([]byte("B\x1b[31mC\x1b[0m")) // full keeps the escapes verbatim
	e.ptyEnded(w.Buffer, nil)
	if got, want := w.Buffer.GetContent(), "one\nAB\x1b[31mC\x1b[0mtwo\n"; got != want {
		t.Fatalf("buffer = %q, want %q", got, want)
	}
}

// The format filters run over the live stream, carrying an escape split across a
// chunk boundary so it is classified whole: plain drops SGR but keeps
// positioning; text drops every escape.
func TestCaptureRawFilters(t *testing.T) {
	cases := []struct {
		name   string
		format captureFormat
		feed   []string
		want   string
	}{
		{"plain keeps positioning, drops SGR", capturePlain,
			[]string{"a\x1b[31", "mb\x1b[2Kc"}, "ab\x1b[2Kc"},
		{"text drops all escapes", captureText,
			[]string{"a\x1b[31", "mb\x1b[2Kc"}, "abc"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, w, sink := rawEditor(t, "", c.format)
			for _, f := range c.feed {
				sink.Output([]byte(f))
			}
			e.ptyEnded(w.Buffer, nil)
			if got := w.Buffer.GetContent(); got != c.want {
				t.Fatalf("buffer = %q, want %q", got, c.want)
			}
		})
	}
}

// The lines rung folds each transcript line the host relays via LineOff in at
// the cursor with a newline (honoring the format), and ignores raw Output.
func TestCaptureLinesFold(t *testing.T) {
	newLinesSession := func(t *testing.T, format captureFormat) (*Editor, *viewport.Viewport, CaptureSink) {
		t.Helper()
		e, w := newTestEditor(t, "")
		e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
		var sink CaptureSink
		e.Config.TerminalSurfaces = TerminalHooks{
			Open:           func(string, int, int) {},
			Feed:           func(string, []byte) []byte { return nil },
			Place:          func([]TerminalSurface) {},
			Close:          func(string) {},
			SetCaptureSink: func(_ string, s CaptureSink) { sink = s },
		}
		if !e.execRequestArgsPolicy("bash", nil, "", ptySizePolicy{}, captureLines, format) {
			t.Fatal("exec failed")
		}
		if sink == nil {
			t.Fatal("lines rung did not register a capture sink")
		}
		return e, w, sink
	}

	t.Run("full keeps SGR, ignores raw Output", func(t *testing.T) {
		e, w, sink := newLinesSession(t, captureFull)
		sink.Output([]byte("ignored on the lines rung"))
		sink.LineOff("L1")
		sink.LineOff("\x1b[31mL2\x1b[0m")
		e.ptyEnded(w.Buffer, nil)
		if got, want := w.Buffer.GetContent(), "L1\n\x1b[31mL2\x1b[0m\n"; got != want {
			t.Fatalf("buffer = %q, want %q", got, want)
		}
	})
	t.Run("text strips the line's escapes", func(t *testing.T) {
		e, w, sink := newLinesSession(t, captureText)
		sink.LineOff("\x1b[31mred\x1b[0m")
		e.ptyEnded(w.Buffer, nil)
		if got, want := w.Buffer.GetContent(), "red\n"; got != want {
			t.Fatalf("buffer = %q, want %q", got, want)
		}
	})
}

// A hidden session runs under the hood: it EXISTS (ptySessionFor) but is not
// interactive (visibleSessionFor is nil) and publishes no surface. Showing it
// reverses all three.
func TestPtyHiddenSurfaceAndVisibility(t *testing.T) {
	e, w := newTestEditor(t, "doc\n")
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
	var lastPlaced []TerminalSurface
	e.Config.TerminalSurfaces = TerminalHooks{
		Open:  func(string, int, int) {},
		Feed:  func(string, []byte) []byte { return nil },
		Place: func(s []TerminalSurface) { lastPlaced = s },
		Close: func(string) {},
	}
	w.ContentX, w.ContentY, w.ContentWidth, w.ContentHeight = 0, 0, 80, 25

	pol := ptySizePolicy{mode: sizeExact, cols: 80, rows: 25, hidden: true}
	if !e.execRequestArgsPolicy("bash", nil, "", pol, captureOff, captureFull) {
		t.Fatal("exec failed")
	}
	if e.ptySessionFor(w.Buffer) == nil {
		t.Error("ptySessionFor should see a hidden session (existence)")
	}
	if e.visibleSessionFor(w.Buffer) != nil {
		t.Error("visibleSessionFor should be nil for a hidden session")
	}
	e.notifyTerminalSurfaces()
	if len(lastPlaced) != 0 {
		t.Errorf("hidden session published %d surfaces, want 0", len(lastPlaced))
	}

	// Show it mid-session: now interactive and published.
	if !e.setViewportPTYHidden(-1, "viewport_pty_show") {
		t.Fatal("show failed")
	}
	if e.visibleSessionFor(w.Buffer) == nil {
		t.Error("after show, visibleSessionFor should be non-nil")
	}
	e.notifyTerminalSurfaces()
	if len(lastPlaced) != 1 {
		t.Fatalf("after show, published %d surfaces, want 1", len(lastPlaced))
	}

	// The commands warn and report false on a buffer with no session.
	e2, _ := newTestEditor(t, "plain\n")
	if e2.setViewportPTYHidden(0, "viewport_pty_toggle") {
		t.Error("toggle with no session should return false")
	}
}

// viewport_pty_kill closes the focused session (the read loop then ends and
// ptyEnded folds a final capture — that fold is covered by
// TestCaptureOnDieFillsBuffer). Here: it closes a live session and warns on none.
func TestViewportPTYKill(t *testing.T) {
	e, _ := newTestEditor(t, "doc\n")
	var pty *stubPTY
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { pty = newStubPTY(); return pty, nil }
	e.Config.TerminalSurfaces = TerminalHooks{
		Open: func(string, int, int) {}, Feed: func(string, []byte) []byte { return nil },
		Place: func([]TerminalSurface) {}, Close: func(string) {},
	}
	if !e.execRequestArgsPolicy("bash", nil, "", ptySizePolicy{}, captureOff, captureFull) {
		t.Fatal("exec failed")
	}
	if !e.killViewportPTY() {
		t.Fatal("kill should report true for a live session")
	}
	if pty == nil || !pty.isClosed() {
		t.Error("kill should close the session")
	}
	// No session: warns and reports false.
	e2, _ := newTestEditor(t, "plain\n")
	if e2.killViewportPTY() {
		t.Error("kill with no session should return false")
	}
}

func TestParseSizeSwitchErrors(t *testing.T) {
	bad := []string{
		"--size=80x25 --minimum=100x40 bash", // two size policies conflict
		"--size=80x25 --hidden=100x40 bash",  // two explicit sizes conflict
		"--size=80 bash",                     // missing 'x'
		"--size=0x0 bash",                    // both axes zero is broken, not small
		"--size=x bash",                      // both axes omitted, same thing
		"--size=-5x10 bash",                  // negative axis
		"--size=axb bash",                    // non-numeric
	}
	for _, line := range bad {
		if _, err := parseExecLine(line); err == nil {
			t.Errorf("parseExecLine(%q) = nil error, want an error", line)
		}
	}
}

// A 0 or omitted axis pins the other and lets this one follow the tile.
func TestParseSizeSwitchPerAxis(t *testing.T) {
	cases := []struct {
		line       string
		cols, rows int
	}{
		{"--size=80x0 bash", 80, 0},
		{"--size=80x bash", 80, 0},
		{"--size=0x24 bash", 0, 24},
		{"--size=x24 bash", 0, 24},
	}
	for _, c := range cases {
		s, err := parseExecLine(c.line)
		if err != nil {
			t.Errorf("parseExecLine(%q) error: %v", c.line, err)
			continue
		}
		if s.SizeMode != sizeExact || s.SizeCols != c.cols || s.SizeRows != c.rows {
			t.Errorf("parseExecLine(%q) = mode %d %dx%d, want exact %dx%d",
				c.line, s.SizeMode, s.SizeCols, s.SizeRows, c.cols, c.rows)
		}
	}
}

// A pinned axis of 0 resolves to the tile for the child (a concrete winsize)
// but stays 0 for the host, so purfecterm follows physical on that axis.
func TestPtySizePolicyPerAxisZero(t *testing.T) {
	check := func(name string, gotC, gotR, wantC, wantR int) {
		t.Helper()
		if gotC != wantC || gotR != wantR {
			t.Errorf("%s = %dx%d, want %dx%d", name, gotC, gotR, wantC, wantR)
		}
	}
	// 80x0: width pinned, height follows the tile.
	wide := ptySizePolicy{mode: sizeExact, cols: 80, rows: 0}
	c, r := wide.resolveLogical(50, 20)
	check("80x0.resolveLogical(50,20)", c, r, 80, 20)
	c, r = wide.resolveLogical(200, 60)
	check("80x0.resolveLogical(200,60)", c, r, 80, 60)
	c, r = wide.hostLogical(200, 60)
	check("80x0.hostLogical", c, r, 80, 0)
	// x24: height pinned, width follows the tile.
	tall := ptySizePolicy{mode: sizeExact, cols: 0, rows: 24}
	c, r = tall.resolveLogical(50, 20)
	check("x24.resolveLogical(50,20)", c, r, 50, 24)
	c, r = tall.hostLogical(50, 20)
	check("x24.hostLogical", c, r, 0, 24)
}
