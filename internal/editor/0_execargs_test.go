package editor

import (
	"reflect"
	"testing"
)

// exec composites its arguments and re-parses them with argwild, so mew's own
// switches and the child's are separated by argwild's phase model rather than
// by counting.
func TestParseExecLine(t *testing.T) {
	cases := []struct {
		name    string
		line    string
		method  string
		program string
		args    []string
	}{
		{
			name:    "bare program",
			line:    "bash",
			program: "bash",
		},
		{
			name:    "pty method then program",
			line:    "--pty=pipe_only bash",
			method:  "pipe_only",
			program: "bash",
		},
		{
			name:    "a switch after the program is the child's",
			line:    "bash -l",
			program: "bash",
			args:    []string{"-l"},
		},
		{
			name:    "long switch after the program",
			line:    "--pty=pipe_only bash --login",
			method:  "pipe_only",
			program: "bash",
			args:    []string{"--login"},
		},
		{
			name:    "a quoted run rides through, split on spaces",
			line:    `--pty=pipe_only bash "-l -i"`,
			method:  "pipe_only",
			program: "bash",
			args:    []string{"-l", "-i"},
		},
		{
			name:    "after -- nothing is a switch",
			line:    "--pty=pipe_only -- bash -l -i",
			method:  "pipe_only",
			program: "bash",
			args:    []string{"-l", "-i"},
		},
		{
			name:    "the program may live past --",
			line:    "-- bash -l",
			program: "bash",
			args:    []string{"-l"},
		},
		{
			name:    "operands after the program are arguments",
			line:    "git status",
			program: "git",
			args:    []string{"status"},
		},
		{
			name:    "a PSL list carries arguments containing spaces",
			line:    `-- git ("commit", "-m", "a message with spaces")`,
			program: "git",
			args:    []string{"commit", "-m", "a message with spaces"},
		},
		{
			name:    "method value may be quoted",
			line:    `--pty="pipe only" bash`,
			method:  "pipe only",
			program: "bash",
		},
	}
	for _, c := range cases {
		got, err := parseExecLine(c.line)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got.Method != c.method {
			t.Errorf("%s: method %q, want %q", c.name, got.Method, c.method)
		}
		if got.Program != c.program {
			t.Errorf("%s: program %q, want %q", c.name, got.Program, c.program)
		}
		if len(got.Args) != 0 || len(c.args) != 0 {
			if !reflect.DeepEqual(got.Args, c.args) {
				t.Errorf("%s: args %q, want %q", c.name, got.Args, c.args)
			}
		}
	}
}

// An empty line, or one naming no program, is refused rather than starting
// something unintended. An unknown switch LEFT of the program is refused too:
// silently turning a typo into a child argument is worse than saying so.
func TestParseExecLineRefusals(t *testing.T) {
	for _, line := range []string{
		"",
		"   ",
		"--pty=pipe_only",
		"--pty",
		"--nonsense bash",
	} {
		if _, err := parseExecLine(line); err == nil {
			t.Errorf("%q should have been refused", line)
		}
	}
}

// The arguments are composited with single spaces, empties dropped, so the
// command reads the same however the caller split them up.
func TestJoinExecArgs(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"--pty=pipe_only", "bash"}, "--pty=pipe_only bash"},
		{[]string{"--pty=pipe_only bash"}, "--pty=pipe_only bash"},
		{[]string{"", "bash", "  ", "-l"}, "bash -l"},
		{nil, ""},
	}
	for _, c := range cases {
		if got := joinExecArgs(c.in); got != c.want {
			t.Errorf("joinExecArgs(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// mew's own options may arrive as PawScript NAMED arguments instead of
// switches — `exec pty: pipe_only, "bash"` — and the two spellings mean the
// same thing. A switch on the composited line WINS over a named argument: it
// sits nearer the program it configures, and a script's default should not
// overrule a caller who spelled one out.
func TestParseExecLineNamedArgs(t *testing.T) {
	cases := []struct {
		name    string
		line    string
		named   map[string]interface{}
		method  string
		program string
		args    []string
	}{
		{
			name:    "named pty, positional program",
			line:    "bash",
			named:   map[string]interface{}{"pty": "pipe_only"},
			method:  "pipe_only",
			program: "bash",
		},
		{
			name:    "named pty with the child's args on the line",
			line:    "-- bash -l",
			named:   map[string]interface{}{"pty": "pipe_only"},
			method:  "pipe_only",
			program: "bash",
			args:    []string{"-l"},
		},
		{
			name:    "a switch beats a named argument",
			line:    "--pty=3 bash",
			named:   map[string]interface{}{"pty": "pipe_only"},
			method:  "3",
			program: "bash",
		},
		{
			name:    "the program itself may be named",
			line:    "",
			named:   map[string]interface{}{"pty": "pipe_only", "program": "bash"},
			method:  "pipe_only",
			program: "bash",
		},
		{
			// Naming the program means the line is ARGUMENTS: the two cannot
			// both claim the first operand, and the explicit spelling wins.
			name:    "a named program makes the line arguments",
			line:    "zsh",
			named:   map[string]interface{}{"program": "bash"},
			program: "bash",
			args:    []string{"zsh"},
		},
		{
			name:    "a named program still takes the line's arguments",
			line:    "-- -l -i",
			named:   map[string]interface{}{"program": "bash"},
			program: "bash",
			args:    []string{"-l", "-i"},
		},
		{
			name:    "a non-string named value is accepted",
			line:    "cmd.exe",
			named:   map[string]interface{}{"pty": 3},
			method:  "3",
			program: "cmd.exe",
		},
	}
	for _, c := range cases {
		got, err := parseExecLineNamed(c.line, c.named)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got.Method != c.method {
			t.Errorf("%s: method %q, want %q", c.name, got.Method, c.method)
		}
		if got.Program != c.program {
			t.Errorf("%s: program %q, want %q", c.name, got.Program, c.program)
		}
		if len(got.Args) != 0 || len(c.args) != 0 {
			if !reflect.DeepEqual(got.Args, c.args) {
				t.Errorf("%s: args %q, want %q", c.name, got.Args, c.args)
			}
		}
	}

	// An unknown NAMED option is refused, exactly as an unknown switch is.
	if _, err := parseExecLineNamed("bash", map[string]interface{}{"nonsense": "x"}); err == nil {
		t.Error("an unknown named option should be refused")
	}
}

// The shell command is the exec line with the program left to the HOST. Same
// switches, same phase rules, same named arguments — but no operand becomes a
// program name, so the first one is already the shell's own argument.
func TestParseShellLine(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		named  map[string]interface{}
		method string
		args   []string
	}{
		{
			// Nothing at all is a complete request: the login shell, bare.
			name: "bare shell",
			line: "",
		},
		{
			name:   "just a method",
			line:   "--pty=pipe_only",
			method: "pipe_only",
		},
		{
			// The very thing that would be the PROGRAM for exec.
			name: "the first operand is an argument",
			line: "-- -l",
			args: []string{"-l"},
		},
		{
			name:   "method then the shell's own arguments",
			line:   "--pty=pipe_only -- -l -i",
			method: "pipe_only",
			args:   []string{"-l", "-i"},
		},
		{
			name: "a quoted run rides through, split on spaces",
			line: `"-l -i"`,
			args: []string{"-l", "-i"},
		},
		{
			// Running a program THROUGH the shell is just an argument to it,
			// and a PSL list is how one carrying spaces gets there intact.
			name: "a command through the shell",
			line: `-- ("-c", "make test")`,
			args: []string{"-c", "make test"},
		},
		{
			// A switch past mew's half is forwarded, same as for exec.
			name: "a switch after the first operand is the shell's",
			line: "-- -c --version",
			args: []string{"-c", "--version"},
		},
		{
			name:   "named pty",
			line:   "-- -l",
			named:  map[string]interface{}{"pty": "pipe_only"},
			method: "pipe_only",
			args:   []string{"-l"},
		},
	}
	for _, c := range cases {
		got, err := parseShellLineNamed(c.line, c.named)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !got.Shell {
			t.Errorf("%s: Shell not set", c.name)
		}
		if got.Program != "" {
			t.Errorf("%s: program %q, want none — the host names it", c.name, got.Program)
		}
		if got.Method != c.method {
			t.Errorf("%s: method %q, want %q", c.name, got.Method, c.method)
		}
		if len(got.Args) != 0 || len(c.args) != 0 {
			if !reflect.DeepEqual(got.Args, c.args) {
				t.Errorf("%s: args %q, want %q", c.name, got.Args, c.args)
			}
		}
	}
}

// shell refuses to be told a program: honouring one would make it exec while
// still reading as though the host had chosen the shell.
func TestParseShellLineRefusesAProgram(t *testing.T) {
	for _, named := range []map[string]interface{}{
		{"program": "bash"},
		{"command": "bash"},
	} {
		if _, err := parseShellLineNamed("", named); err == nil {
			t.Errorf("shell should refuse %v", named)
		}
	}
	// An unknown switch left of the arguments is refused here too.
	if _, err := parseShellLineNamed("--nonsense -- -l", nil); err == nil {
		t.Error("an unknown shell switch should be refused")
	}
}

// A call made entirely of named arguments: ok reports whether it stands on its
// own, which is a different question from whether it was WRONG. shell always
// stands alone; exec needs a program; a bad option is an error either way.
func TestNamedOnlyRequest(t *testing.T) {
	if _, ok, err := namedOnlyRequest(nil, true); err != nil || !ok {
		t.Errorf("bare shell: ok=%v err=%v, want true/nil", ok, err)
	}
	if _, ok, err := namedOnlyRequest(nil, false); err != nil || ok {
		t.Errorf("bare exec: ok=%v err=%v, want false/nil (it prompts)", ok, err)
	}
	if _, ok, err := namedOnlyRequest(map[string]interface{}{"pty": "3"}, false); err != nil || ok {
		t.Errorf("exec with only a method: ok=%v err=%v, want false/nil (it prompts)", ok, err)
	}
	spec, ok, err := namedOnlyRequest(map[string]interface{}{"program": "bash"}, false)
	if err != nil || !ok || spec.Program != "bash" {
		t.Errorf("exec program: %+v ok=%v err=%v", spec, ok, err)
	}
	if _, _, err := namedOnlyRequest(map[string]interface{}{"nonsense": "x"}, true); err == nil {
		t.Error("an unknown named option should be an error, not a prompt")
	}
}
