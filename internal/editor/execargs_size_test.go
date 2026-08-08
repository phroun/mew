package editor

import "testing"

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
	t.Run("hidden bare takes the default", func(t *testing.T) {
		s, err := parseExecLine("--hidden bash")
		run(t, s, err, want{mode: sizeExact, cols: 80, rows: 25, hidden: true, program: "bash"})
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
	if !e.execRequestArgsPolicy("bash", nil, "", ptySizePolicy{mode: sizeExact, cols: 80, rows: 25}, captureOff) {
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
	cases := []struct {
		line string
		want captureMode
	}{
		{"--capture bash", captureText},
		{"--capture=text bash", captureText},
		{"--capture=ansi bash", captureANSI},
		{"--capture=raw bash", captureANSI},
		{"--capture=off bash", captureOff},
		{"bash", captureOff},
	}
	for _, c := range cases {
		s, err := parseExecLine(c.line)
		if err != nil {
			t.Errorf("parseExecLine(%q) error: %v", c.line, err)
			continue
		}
		if s.Capture != c.want {
			t.Errorf("parseExecLine(%q) capture = %d, want %d", c.line, s.Capture, c.want)
		}
	}
	if s, err := parseExecLineNamed("bash", map[string]interface{}{"capture": "ansi"}); err != nil || s.Capture != captureANSI {
		t.Errorf("named capture: got %d err %v, want ansi", s.Capture, err)
	}
	if _, err := parseExecLine("--capture=nonsense bash"); err == nil {
		t.Error("--capture=nonsense should error")
	}
}

// Capture-on-die folds the session's final scrollback into its buffer when it
// ends — text with escapes stripped, ansi with them kept — and off leaves the
// buffer empty and never asks for a snapshot.
func TestCaptureOnDieFillsBuffer(t *testing.T) {
	run := func(name string, mode captureMode, wantANSI bool, snap, want string) {
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
			if !e.execRequestArgsPolicy("bash", nil, "", ptySizePolicy{}, mode) {
				t.Fatal("exec failed")
			}
			e.ptyEnded(w.Buffer, nil)
			if got := w.Buffer.GetContent(); got != want {
				t.Errorf("buffer = %q, want %q", got, want)
			}
			switch {
			case mode == captureOff && askedANSI != nil:
				t.Error("capture off should not ask for a snapshot")
			case mode != captureOff && askedANSI == nil:
				t.Error("capture on should ask for a snapshot")
			case mode != captureOff && *askedANSI != wantANSI:
				t.Errorf("snapshot ansi = %v, want %v", *askedANSI, wantANSI)
			}
		})
	}
	run("text strips", captureText, false, "plain transcript\n", "plain transcript\n")
	run("ansi keeps", captureANSI, true, "\x1b[31mred\x1b[0m\n", "\x1b[31mred\x1b[0m\n")
	run("off is empty", captureOff, false, "should not appear\n", "")
}

func TestParseSizeSwitchErrors(t *testing.T) {
	bad := []string{
		"--size=80x25 --minimum=100x40 bash", // two size policies conflict
		"--size=80x25 --hidden bash",         // size + hidden conflict
		"--size=80 bash",                     // missing 'x'
		"--size=0x25 bash",                   // zero width
		"--size=80x0 bash",                   // zero height
		"--size=axb bash",                    // non-numeric
	}
	for _, line := range bad {
		if _, err := parseExecLine(line); err == nil {
			t.Errorf("parseExecLine(%q) = nil error, want an error", line)
		}
	}
}
