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
