package editor

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/phroun/argwild"
)

// The exec command line.
//
// exec takes any number of arguments, joins them with single spaces, and runs
// the whole composited string back through argwild — the same parser the host
// app uses on its own command line at boot. One notation, learned once.
//
// The shape is:
//
//	exec "--pty=pipe_only --other-switch-for-mew program arg arg"
//
// Everything before the program name belongs to MEW. Everything after it
// belongs to the CHILD. argwild's phase model does the separating: an operand
// closes the run of switches before it, so the first operand IS the program
// name and every switch after it is already in a different stanza.
//
// Passing arguments that argwild would otherwise read as mew's own has two
// spellings, and both work:
//
//	exec "--pty=pipe_only bash \"-l -i\""   — a quoted run, split on spaces
//	exec "--pty=pipe_only -- bash -l -i"    — after --, nothing is a switch
//
// After "--" argwild emits every remaining token as an operand, so the program
// name may live there too:
//
//	exec "--pty=pipe_only -- bash -l"
//
// An argument that itself contains a space cannot be written either way, so a
// PSL list operand carries them exactly:
//
//	exec "-- git commit (\"-m\", \"a message with spaces\")"
//
// mew's own options can equally be given as PawScript NAMED arguments, which
// is the tidier spelling when a script builds the call:
//
//	exec pty: pipe_only, "bash -l"
//	exec pty: pipe_only, program: bash
//
// Naming the program means the composited line is ARGUMENTS only — the two
// cannot both claim the first operand, and the explicit spelling wins:
//
//	exec program: bash, "-- -l -i"
//
// The two forms mean the same thing and may be mixed; a switch on the
// composited line wins over a named argument, being the more specific and the
// nearer to the program it configures.
//
// The `shell` command is this same line in every respect but one: it names no
// program, because the HOST names it — the user's login shell, which depends on
// their operating system and their own account and is knowable in neither of
// those from here. So the first operand is not a program but the shell's first
// ARGUMENT, and running something through the shell is just an argument too:
//
//	shell                                  — the login shell, bare
//	shell "--pty=pipe_only -- -l -i"       — a login, interactive shell
//	shell "-c \"make test\""               — one command through it
// sizeMode governs how a session's LOGICAL size is chosen — the size the child
// process is told it has, independent of how large the visible tile is. The
// zero value follows the focused tile, which is what every session did before
// these switches existed.
type sizeMode int

const (
	sizeFollow  sizeMode = iota // default: logical size tracks the focused tile
	sizeExact                   // --size / --hidden: pin the logical size
	sizeMinimum                 // --minimum: logical = max(tile, floor)
)

// captureRung selects a session's capture fidelity — WHEN and HOW MUCH of its
// output is folded into its buffer (see docs/pty-capture.md). The zero value is
// "unspecified" so a future config default can be told apart from an explicit
// off; today unspecified resolves to off (Editor.resolveCapture).
type captureRung int

const (
	captureUnset captureRung = iota // not given → inherit the default (today: off)
	captureOff                      // explicitly disabled, overrides a default
	captureRaw                      // M1: the raw byte stream (not implemented yet)
	captureFinal                    // M2: final scrollback + used screen, at death
	captureLines                    // M3: transcript as lines scroll off (not yet)
	captureLive                     // M4: live screen mirror (not implemented yet)
)

// captureFormat selects how much escape detail a capture keeps. The default
// keeps everything; --plain drops visual styling (SGR); --text drops all VT
// escapes. Meaningful only alongside a rung.
type captureFormat int

const (
	captureFull  captureFormat = iota // keep everything (default)
	capturePlain                      // strip SGR (CSI-m), keep positioning
	captureText                       // strip all escapes → plain text
)

type execSpec struct {
	// Method is --pty=NAME: which way the host should make the terminal. mew
	// attaches no meaning to it and forwards the string (see PTYRequest).
	Method string
	// Program is the executable to run, and Args what it receives. Program is
	// empty exactly when Shell is set.
	Program string
	Args    []string
	// Shell asks the host for the user's login shell instead of naming a
	// program. Set by the `shell` command, and it changes the parse: no
	// operand becomes the program, so every one of them is an argument.
	Shell bool

	// SizeMode and SizeCols/SizeRows carry the --size / --minimum / --hidden
	// switches: how the session's logical size is governed and, for the two
	// that pin or floor it, to what. sizeFollow (the zero value) leaves the
	// size to the focused tile, exactly as before. Hidden additionally asks
	// for a session with no visible surface — a full terminal that simply is
	// not painted; it implies an exact size, there being no tile to take one
	// from.
	SizeMode           sizeMode
	SizeCols, SizeRows int
	Hidden             bool

	// Capture is the fidelity rung (--capture=off|raw|final|lines|live), and
	// CaptureFormat how much escape detail it keeps (--plain / --text; full by
	// default). captureUnset (the zero value) means "not given" — resolved to the
	// default at request time — kept distinct from an explicit off.
	Capture       captureRung
	CaptureFormat captureFormat
}

// parseExecLine parses a composited exec command line.
//
// The first operand ends mew's switches and names the program; everything
// after it is the child's. A switch appearing after the program name is
// reconstructed and forwarded to the child rather than refused — `exec "bash
// -l"` is the natural thing to type and should work. Unusual spellings (a
// short switch with an attached value, say) round-trip to their canonical
// form, so put those after "--" where they are taken verbatim.
func parseExecLine(line string) (execSpec, error) {
	return parseExecLineNamed(line, nil)
}

// parseExecLineNamed is parseExecLine with PawScript named arguments folded in
// as defaults. A switch on the line overrides the same option named here: it
// sits nearer the program it configures, and a script passing a default should
// not overrule the caller who spelled one out.
func parseExecLineNamed(line string, named map[string]interface{}) (execSpec, error) {
	return parseCommandLine(line, named, false)
}

// parseShellLineNamed parses the `shell` command's line. Same notation, same
// switches, same named arguments — the one difference is that no operand names
// a program, because the host does that. See execSpec.
func parseShellLineNamed(line string, named map[string]interface{}) (execSpec, error) {
	return parseCommandLine(line, named, true)
}

// parseCommandLine is the shared parse behind exec and shell.
//
// mew's half of the line ends at the FIRST operand either way: switches before
// it are mew's, switches after it are reconstructed and forwarded to the child.
// What differs is only what becomes of that first operand — exec's program
// name, or shell's first argument.
func parseCommandLine(line string, named map[string]interface{}, shell bool) (execSpec, error) {
	spec := execSpec{Shell: shell}
	if err := spec.applyNamed(named); err != nil {
		return spec, err
	}
	// True once mew's own switches are done with. A NAMED program has already
	// claimed the first operand's job, so the line is arguments only.
	past := spec.Program != ""
	line = strings.TrimSpace(line)
	if line == "" {
		if spec.Program != "" || shell {
			// Named arguments carried the whole request — or, for shell, there
			// was never anything more to say: `shell` alone is a login shell.
			return spec, nil
		}
		return spec, fmt.Errorf("nothing to execute")
	}
	res, err := argwild.ParseString(line)
	if err != nil {
		return spec, err
	}

	for _, st := range res.Stanzas {
		switch s := st.(type) {
		case *argwild.ArgSet:
			for _, sw := range s.Switches {
				if !past {
					// Still mew's half of the line.
					if err := spec.applyOwnSwitch(sw); err != nil {
						return spec, err
					}
					continue
				}
				// Past mew's half: the child's, spelled canonically.
				spec.Args = append(spec.Args, sw.String())
			}
		case *argwild.Operand:
			if !past {
				past = true
				if !shell {
					spec.Program = strings.TrimSpace(s.Value.AsString())
					if spec.Program == "" {
						return spec, fmt.Errorf("nothing to execute")
					}
					continue
				}
			}
			spec.Args = append(spec.Args, operandArgs(s.Value)...)
		}
	}
	if spec.Program == "" && !shell {
		return spec, fmt.Errorf("no program named")
	}
	return spec, nil
}

// namedOnlyRequest resolves a call made entirely of named arguments — no
// positional line at all.
//
// ok is false when the named arguments do not amount to a request by
// themselves, which is NOT a failure: it is the case where the command prompts
// for a line. An error means the named arguments were themselves wrong (an
// unknown option, say), and that is worth saying rather than papering over with
// a prompt.
func namedOnlyRequest(named map[string]interface{}, shell bool) (execSpec, bool, error) {
	spec := execSpec{Shell: shell}
	if err := spec.applyNamed(named); err != nil {
		return spec, false, err
	}
	return spec, spec.Program != "" || spec.Shell, nil
}

// applyNamed folds PawScript named arguments in as defaults, so
// `exec pty: pipe_only, "bash"` is the same call as
// `exec "--pty=pipe_only bash"`.
func (spec *execSpec) applyNamed(named map[string]interface{}) error {
	for k, v := range named {
		if err := spec.setOption(k, fmt.Sprintf("%v", v)); err != nil {
			return err
		}
	}
	return nil
}

// applyOwnSwitch consumes one of MEW's switches — those left of the program
// name. Unknown ones are refused rather than guessed at: a mistyped switch
// silently becoming a child argument is worse than being told.
func (spec *execSpec) applyOwnSwitch(sw argwild.Switch) error {
	v, ok := sw.First()
	if !ok {
		// A valueless switch (--hidden). Options that mean something on their
		// own take it; the rest still say they need a value, preserving the old
		// refusal for a mistyped --pty.
		if err := spec.setBareOption(sw.Name); err != nil {
			return fmt.Errorf("%v (put the child's own switches after --)", err)
		}
		return nil
	}
	if err := spec.setOption(sw.Name, v.AsString()); err != nil {
		return fmt.Errorf("%v (put the child's own switches after --)", err)
	}
	return nil
}

// setBareOption consumes a switch written with no value.
func (spec *execSpec) setBareOption(name string) error {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "hidden":
		return spec.setOption("hidden", "")
	case "capture":
		return spec.setOption("capture", "")
	case "plain":
		return spec.setOption("plain", "")
	case "text":
		return spec.setOption("text", "")
	}
	return fmt.Errorf("--%s needs a value, e.g. --pty=pipe_only", name)
}

// setOption is the ONE place an option name is understood, so the switch form
// and the named-argument form cannot drift apart.
func (spec *execSpec) setOption(name, value string) error {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "pty":
		spec.Method = strings.TrimSpace(value)
		return nil
	case "program", "command":
		if spec.Shell {
			// Refused rather than honoured: naming a program here would make
			// `shell` into `exec` while still reading as though the host had
			// chosen the shell. Whoever wants to name one should say exec.
			return fmt.Errorf("shell chooses the program (the login shell) — use exec to name one")
		}
		// Only meaningful as a NAMED argument — on the composited line the
		// program is simply the first operand.
		spec.Program = strings.TrimSpace(value)
		return nil
	case "size":
		c, r, err := parseWxH(value)
		if err != nil {
			return err
		}
		return spec.setSizePolicy(sizeExact, c, r, false)
	case "minimum":
		c, r, err := parseWxH(value)
		if err != nil {
			return err
		}
		return spec.setSizePolicy(sizeMinimum, c, r, false)
	case "hidden":
		// A bare --hidden (or hidden: true) takes the default 80x25; a value
		// pins an exact size AND hides; an explicit falsehood is a no-op, so a
		// script can pass hidden: false to mean "not hidden".
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "", "true", "on", "yes", "1":
			return spec.setSizePolicy(sizeExact, 80, 25, true)
		case "false", "off", "no", "0":
			return nil
		}
		c, r, err := parseWxH(value)
		if err != nil {
			return err
		}
		return spec.setSizePolicy(sizeExact, c, r, true)
	case "capture":
		// The capture RUNG: how much of the session's output is folded into its
		// buffer, and when (docs/pty-capture.md). A bare --capture, or
		// capture: true, means final; --plain / --text pick the format.
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "", "final", "on", "true", "yes", "1":
			spec.Capture = captureFinal
		case "off", "none", "false", "no", "0":
			spec.Capture = captureOff
		case "raw":
			return fmt.Errorf("--capture=raw (live byte log) is not implemented yet")
		case "lines":
			return fmt.Errorf("--capture=lines (scroll-off transcript) is not implemented yet")
		case "live":
			return fmt.Errorf("--capture=live (screen mirror) is not implemented yet")
		default:
			return fmt.Errorf("--capture must be off, raw, final, lines, or live")
		}
		return nil
	case "plain":
		return spec.setCaptureFormat(capturePlain, value)
	case "text":
		return spec.setCaptureFormat(captureText, value)
	}
	if spec.Shell {
		return fmt.Errorf("unknown shell option %q", name)
	}
	return fmt.Errorf("unknown exec option %q", name)
}

// setCaptureFormat records --plain or --text, refusing a second: they are two
// points on one strip scale, so giving both is a contradiction, not a silent
// last-one-wins. They take no value; an explicit falsehood (text: false) is a
// no-op so a script can pass it through.
func (spec *execSpec) setCaptureFormat(f captureFormat, value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "false", "off", "no", "0":
		return nil
	case "", "true", "on", "yes", "1":
		// a plain flag
	default:
		return fmt.Errorf("--plain/--text take no value")
	}
	if spec.CaptureFormat != captureFull && spec.CaptureFormat != f {
		return fmt.Errorf("only one of --plain, --text may be given")
	}
	spec.CaptureFormat = f
	return nil
}

// setSizePolicy records one of the --size/--minimum/--hidden switches, refusing
// a second: they all govern the same single logical size, so two of them on one
// line is a contradiction to be told about, not a silent last-one-wins.
func (spec *execSpec) setSizePolicy(mode sizeMode, cols, rows int, hidden bool) error {
	if spec.SizeMode != sizeFollow || spec.Hidden {
		return fmt.Errorf("only one of --size, --minimum, --hidden may be given")
	}
	spec.SizeMode = mode
	spec.SizeCols, spec.SizeRows = cols, rows
	spec.Hidden = hidden
	return nil
}

// parseWxH reads a COLSxROWS size like "80x25". Either axis may be 0 or omitted
// — "80x", "x24", "0x24", "80x0" — which pins the given axis and lets the other
// follow the tile: purfecterm reads a logical 0 on an axis as "use the physical
// dimension", so the free axis tracks the surface while the pinned one holds.
// At least one axis must be positive, though: a terminal with zero of BOTH is
// not a smaller terminal, it is a broken one, and the mistake is worth naming.
func parseWxH(value string) (cols, rows int, err error) {
	v := strings.TrimSpace(value)
	i := strings.IndexAny(v, "xX")
	if i < 0 {
		return 0, 0, fmt.Errorf("size must look like COLSxROWS, e.g. 80x25 (either axis may be 0 to follow the tile)")
	}
	if cols, err = parseSizeAxis(v[:i]); err != nil {
		return 0, 0, fmt.Errorf("size width must be a non-negative number, e.g. 80x25")
	}
	if rows, err = parseSizeAxis(v[i+1:]); err != nil {
		return 0, 0, fmt.Errorf("size height must be a non-negative number, e.g. 80x25")
	}
	if cols == 0 && rows == 0 {
		return 0, 0, fmt.Errorf("size needs at least one axis, e.g. 80x25, 80x0, or x24")
	}
	return cols, rows, nil
}

// parseSizeAxis reads one axis of a size: an empty string is 0 ("follow the tile
// on this axis"), and any explicit value must be a non-negative integer.
func parseSizeAxis(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("not a non-negative number")
	}
	return n, nil
}

// sizePolicy carries the parsed --size/--minimum/--hidden decision onto the
// request path (see ptySizePolicy in pty.go).
func (spec execSpec) sizePolicy() ptySizePolicy {
	return ptySizePolicy{mode: spec.SizeMode, cols: spec.SizeCols, rows: spec.SizeRows, hidden: spec.Hidden}
}

// operandArgs turns one operand into child arguments.
//
//   - A PSL list contributes each element verbatim, which is the only way to
//     pass an argument containing a space.
//   - A QUOTED operand is split on whitespace: that is the convenience form,
//     the one that lets "-l -i" ride through argwild untouched.
//   - Anything else is a single argument.
func operandArgs(v argwild.Value) []string {
	switch v.Kind {
	case argwild.KindPSL:
		// The block parses to pawscript's own list type, so range over it
		// reflectively rather than asserting one concrete slice shape.
		rv := reflect.ValueOf(v.PSL)
		if rv.IsValid() && rv.Kind() == reflect.Slice {
			out := make([]string, 0, rv.Len())
			for i := 0; i < rv.Len(); i++ {
				out = append(out, fmt.Sprintf("%v", rv.Index(i).Interface()))
			}
			return out
		}
		return []string{v.AsString()}
	case argwild.KindQuoted:
		return strings.Fields(v.Text)
	default:
		return []string{v.AsString()}
	}
}

// joinExecArgs composites the exec command's arguments into one line: joined
// with a single space, exactly as if it had been typed that way. Quoted
// PawScript arguments arrive already unquoted, so a quoted run that must
// survive re-parsing has to be quoted twice — see the command's help.
func joinExecArgs(parts []string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " ")
}
