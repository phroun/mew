package editor

import (
	"fmt"
	"reflect"
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
		return fmt.Errorf("--%s needs a value, e.g. --pty=pipe_only", sw.Name)
	}
	if err := spec.setOption(sw.Name, v.AsString()); err != nil {
		return fmt.Errorf("%v (put the child's own switches after --)", err)
	}
	return nil
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
	}
	if spec.Shell {
		return fmt.Errorf("unknown shell option %q", name)
	}
	return fmt.Errorf("unknown exec option %q", name)
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
