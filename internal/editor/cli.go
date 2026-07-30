package editor

import (
	"fmt"
	"os"
	"strings"

	"github.com/phroun/argwild"
	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/viewport"
	"github.com/phroun/pawscript"
)

// This file turns a parsed argwild command line into a mew launch: each long
// switch is a set_option applied to the buffer being configured, a bare "+N"
// is a go-to-line on the next file opened, and operands are files to open. The
// model is a left-to-right walk — switches mutate a running option set, and
// each file opens with the set as it stands at that point, so a switch after a
// file changes only the files that follow it. Options whose set_option target
// is the whole editor (not a viewport) are "global" and must precede the first
// file.
//
// --eval="script" rides the same walk: the script runs as a PawScript command
// against each file opened while it is set (sticky, like the option set),
// --eval= turns it off, and eval.go owns the execution and #out/#err capture.

// cliPerViewportOptions (set_option routes these to a viewport's ViewState) and
// cliKnownOptions (every option set_option accepts at runtime) are both derived
// from optionSpecs in optionspec.go — the single canonical option table — so
// they can never drift from setOption/getOption. Load-time-only [general] keys
// (layout, mappings, projectConfig, useLocks, ...) are not part of that surface.

// cliOp is one step of the launch walk.
type cliOp struct {
	kind  int    // cliSetOption | cliOpenFile | cliGotoLine | cliEval
	name  string // option name (lowercased) for cliSetOption
	value string // option value for cliSetOption; filename for cliOpenFile; script for cliEval
	line  int    // 1-based line for cliGotoLine
}

const (
	cliSetOption = iota
	cliOpenFile
	cliGotoLine
	cliEval
)

// launchPlan is the ordered walk plus the raw parse (kept for future
// script-accessible PSL).
type launchPlan struct {
	ops []cliOp
	psl interface{}
}

// buildLaunchPlan flattens argwild stanzas into the ordered op list, applying
// the enable/disable value grammar and validating option names and ordering.
func buildLaunchPlan(r *argwild.Result) (*launchPlan, error) {
	plan := &launchPlan{psl: r.ToPSL()}
	sawFile := false
	for _, st := range r.Stanzas {
		switch s := st.(type) {
		case *argwild.ArgSet:
			for _, sw := range s.Switches {
				op, err := switchToOp(sw)
				if err != nil {
					return nil, err
				}
				if op.kind == cliSetOption && !cliPerViewportOptions[op.name] && sawFile {
					return nil, fmt.Errorf("global option --%s must come before any file", op.name)
				}
				plan.ops = append(plan.ops, op)
			}
		case *argwild.Operand:
			name := s.Value.AsString()
			if name == "" {
				continue
			}
			// Shell rescue for --eval: the natural spelling
			// `mew --eval="insert 'hi'" file` reaches argv as ONE token whose
			// value holds spaces, which argwild's switch lexer cannot attach —
			// the whole token degrades to an operand. Nobody edits a file
			// literally named "--eval=...", so reclaim it as the eval switch.
			if script, ok := strings.CutPrefix(name, "--eval="); ok {
				plan.ops = append(plan.ops, cliOp{kind: cliEval, value: unquoteEvalScript(script)})
				continue
			}
			sawFile = true
			plan.ops = append(plan.ops, cliOp{kind: cliOpenFile, value: name})
		}
	}
	return plan, nil
}

// switchToOp translates one argwild switch into a launch op. A bare numeric
// "+N" is a go-to-line; a long "--name[=value]" is a set_option with the
// enable/disable grammar; anything else is rejected.
func switchToOp(sw argwild.Switch) (cliOp, error) {
	// "+N" — the nameless numeric plus switch — jumps to a line.
	if sw.Lead == argwild.LeadPlus && sw.Name == "" {
		v, ok := sw.First()
		if !ok || v.Kind != argwild.KindNumber || v.IsFloat || v.Int < 1 {
			return cliOp{}, fmt.Errorf("+N must be a positive line number, got %q", sw.String())
		}
		return cliOp{kind: cliGotoLine, line: int(v.Int)}, nil
	}
	if sw.Lead != argwild.LeadLong {
		return cliOp{}, fmt.Errorf("unsupported switch %q (use --optionName)", sw.String())
	}
	name := strings.ToLower(sw.Name)
	// --eval is not a set_option key: its value is a PawScript to run once the
	// following file has loaded (sticky across later files; see applyLaunch).
	// A blank --eval= parses as StateBare and turns the sticky script off.
	if name == "eval" {
		switch sw.State {
		case argwild.StateBare, argwild.StateOff:
			return cliOp{kind: cliEval, value: ""}, nil
		case argwild.StateValued:
			v, _ := sw.First()
			return cliOp{kind: cliEval, value: evalScriptFromValue(v)}, nil
		default:
			return cliOp{}, fmt.Errorf(`--eval takes a script value (--eval="command ..." or --eval= to clear)`)
		}
	}
	if !cliKnownOptions[name] {
		return cliOp{}, fmt.Errorf("unknown option --%s", sw.Name)
	}
	value, err := switchValue(sw)
	if err != nil {
		return cliOp{}, fmt.Errorf("--%s: %w", sw.Name, err)
	}
	return cliOp{kind: cliSetOption, name: name, value: value}, nil
}

// switchValue maps a switch's state to the string set_option expects:
//   - bare (--opt) or explicit on (--opt+)          -> "true"
//   - explicit off (--opt-)                          -> "false"
//   - valued (--opt=v or --opt v): on/off words map to true/false, otherwise
//     the literal value (string or number) passes through.
func switchValue(sw argwild.Switch) (string, error) {
	switch sw.State {
	case argwild.StateBare, argwild.StateOn:
		return "true", nil
	case argwild.StateOff:
		return "false", nil
	case argwild.StateValued:
		v, _ := sw.First()
		raw := v.AsString()
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "on", "true", "yes", "1":
			return "true", nil
		case "off", "false", "no", "0":
			return "false", nil
		}
		return raw, nil
	default:
		return "", fmt.Errorf("unhandled switch state")
	}
}

// evalScriptFromValue renders a --eval switch value as the PawScript to run.
// A parenthesized PSL list value (--eval=(cmd1,cmd2)) is a list of command
// strings, joined with ";" into one sequence; any other value is its literal
// text. (The `(cmd "a"; cmd2)` spelling is not valid PSL, so it arrives as
// plain text instead — and PawScript executes the paren-wrapped sequence
// directly.)
func evalScriptFromValue(v argwild.Value) string {
	if v.Kind == argwild.KindPSL {
		var items []interface{}
		switch t := v.PSL.(type) {
		case pawscript.PSLList:
			items = t
		case []interface{}:
			items = t
		}
		if items != nil {
			parts := make([]string, 0, len(items))
			for _, it := range items {
				if s, ok := it.(string); ok {
					parts = append(parts, s)
				} else {
					parts = append(parts, fmt.Sprintf("%v", it))
				}
			}
			return strings.Join(parts, "; ")
		}
	}
	return v.AsString()
}

// unquoteEvalScript strips one layer of symmetric double quotes from a
// shell-rescued --eval= operand value, for hosts that pass the quotes through
// literally (`--eval="script"` arriving with the quotes intact).
func unquoteEvalScript(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// RunArgs launches the editor from a parsed command line: it applies options
// and opens files per the walk described above, then runs the session. A
// launch with no file opens a single empty buffer (as bare `mew` does).
func (e *Editor) RunArgs(r *argwild.Result) error {
	plan, err := buildLaunchPlan(r)
	if err != nil {
		return err
	}
	e.Running = true
	// Startup script first (mappings/macros/option defaults); command-line
	// options then override it.
	e.runProfileScript()

	primary, err := e.applyLaunch(plan)
	if err != nil {
		return err
	}
	e.cliPSL = plan.psl
	_, err = e.serve(primary)
	// A launch-eval batch a script exited out of (or that was still running
	// when the session ended) never got its results buffer: deliver its
	// captured #out/#err to the real streams instead, in occurrence order —
	// after serve's defers have restored the terminal.
	e.dumpEvalOutput(os.Stdout, os.Stderr)
	return err
}

// applyLaunch walks the plan: global options apply immediately (to the whole
// editor), per-viewport options accumulate into a running set applied to each
// file's viewport as it opens, and a pending +N jumps the next file's caret. It
// returns the primary buffer (the first file, or a lone empty buffer when no
// file was named) for the session's content snapshot.
func (e *Editor) applyLaunch(plan *launchPlan) (*buffer.Buffer, error) {
	winOpts := map[string]string{} // running per-viewport option set
	pendingGoto := 0               // 1-based line for the next file, 0 = none
	pendingEval := ""              // sticky --eval script for each following file
	evalFresh := false             // pendingEval set but not yet applied to any file
	var primary *buffer.Buffer     // first MAIN-AREA document buffer (session snapshot)
	var lastWin *viewport.Viewport
	anyFocus := false // has any opened viewport claimed focus yet?

	for _, op := range plan.ops {
		switch op.kind {
		case cliSetOption:
			if cliPerViewportOptions[op.name] {
				winOpts[op.name] = op.value
			} else if !e.setOption(nil, op.name, op.value) {
				return nil, fmt.Errorf("invalid value for --%s: %q", op.name, op.value)
			}
		case cliGotoLine:
			pendingGoto = op.line
		case cliEval:
			pendingEval = op.value
			evalFresh = pendingEval != ""
		case cliOpenFile:
			// The first opened viewport wins focus. A docked wiki tool viewport
			// (mew help:/start) is a readout, not the session's main content:
			// it does not become the primary buffer, so an empty editing area
			// is still opened beneath it below.
			w, err := e.openLaunchFile(op.value, winOpts, !anyFocus)
			if err != nil {
				return nil, err
			}
			anyFocus = true
			lastWin = w
			if primary == nil && w.Type == viewport.DocViewport && w.Dock == viewport.DockNone {
				primary = w.Buffer
			}
			if pendingGoto > 0 {
				e.gotoLaunchLine(w, pendingGoto)
				pendingGoto = 0
			}
			// The sticky eval runs against every file opened while it is set
			// (a later --eval replaces it; --eval= turns it off). The scripts
			// queue here in file order and run visually once the session is
			// rendering (see startLaunchEvals).
			if pendingEval != "" {
				e.launchEvals = append(e.launchEvals, launchEval{winID: w.ID, script: pendingEval})
				evalFresh = false
			}
		}
	}

	// No main-area document opened — bare `mew`, `mew --showLineNumbers-`, or a
	// launch that named only docked tool surfaces (mew help:/start): open one
	// empty document viewport carrying the running per-viewport options so there is
	// an editing area. It takes focus only when nothing else already did.
	if primary == nil {
		buf := e.lib.New()
		w := e.createMainViewport(buf, winOpts, !anyFocus)
		primary = buf
		lastWin = w
		// A launch with no file still honors --eval: the implicit empty
		// document viewport is the "following file" the script applies to.
		if pendingEval != "" {
			e.launchEvals = append(e.launchEvals, launchEval{winID: w.ID, script: pendingEval})
			evalFresh = false
		}
	}
	// A trailing +N with no file after it lands on the last-opened viewport.
	if pendingGoto > 0 && lastWin != nil {
		e.gotoLaunchLine(lastWin, pendingGoto)
	}
	// Mirroring that rule, a trailing --eval that no file followed runs against
	// the last-opened viewport.
	if evalFresh && lastWin != nil {
		e.launchEvals = append(e.launchEvals, launchEval{winID: lastWin.ID, script: pendingEval})
	}
	return primary, nil
}

// openLaunchFile loads a file into a fresh main-buffer viewport (the full open
// path: locks, backups, notices), applies the running per-viewport options, and
// focuses the first one.
func (e *Editor) openLaunchFile(filename string, winOpts map[string]string, focus bool) (*viewport.Viewport, error) {
	// A registered wiki scheme ("mew help:/start") names a PAGE, not a literal
	// file: resolve it through the same machinery the Open prompt uses so the
	// real page file loads and the viewport is rooted in the wiki (in the
	// Type/dock the wiki declares). Without this the name fell through to a
	// plain OS open of "help:/start", which found nothing and came up blank.
	// Per-viewport launch options do not apply to a wiki's own (possibly docked)
	// viewport.
	if w, handled := e.openWikiScheme(strings.TrimSpace(filename), focus); handled {
		if w == nil {
			return nil, fmt.Errorf("open %s: page not found", filename)
		}
		return w, nil
	}

	buf, err := e.loadBuffer(filename)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filename, err)
	}
	if !isMewPath(filename) {
		buf.SetFilename(e.normalizeDocPath(filename))
	}
	return e.createMainViewport(buf, winOpts, focus), nil
}

// docViewportOptions returns a ViewportOptions seeded with the editor's current
// per-viewport view defaults (line numbers, tab size, marks, read-only, browse,
// …). Callers stamp Type/Dock/Buffer/SetFocus (and, for a docked tool
// surface, ViewportSet/Priority/height) on top before creating the viewport.
func (e *Editor) docViewportOptions() viewport.ViewportOptions {
	return viewport.ViewportOptions{
		ShowLineNumbers: e.Config.ShowLineNumbers,
		TabSize:         e.Config.TabSize,
		ShowInvisibles:  e.Config.ShowInvisibles,
		ShowBidi:        e.Config.ShowBidi,
		ShowMarks:       e.Config.ShowMarks,
		OverwriteMode:   e.Config.OverwriteMode,
		ReadOnly:        e.Config.ReadOnly,
		AutoIndent:      e.Config.AutoIndent,
		LinkBrowsing:    e.Config.LinkBrowsing,
		ShowRuler:       e.Config.ShowColumnRuler,
		Scrollbar:       e.Config.Scrollbar,
		SyntaxOverrides: e.Config.SyntaxOverrides,
	}
}

// createMainViewport creates a main-buffer viewport for buf (focused when focus is
// set — the first opened file wins focus; the rest are background buffers
// reachable via buffer_next) and applies the per-viewport option overrides.
func (e *Editor) createMainViewport(buf *buffer.Buffer, winOpts map[string]string, focus bool) *viewport.Viewport {
	opts := e.docViewportOptions()
	opts.Type = viewport.DocViewport
	opts.Dock = viewport.DockNone
	opts.Buffer = buf
	opts.SetFocus = focus
	w := e.ViewportManager.GetViewport(e.ViewportManager.CreateViewport(opts))
	for name, value := range winOpts {
		e.setOption(w, name, value)
	}
	return w
}

// createWikiViewport creates the viewport a wiki page opens in, honoring the
// wiki's declared viewport Type and Dock: a wiki that leaves them at the zero
// value gets an ordinary main-area document viewport (identical to
// createMainViewport); help declares a top-docked ToolViewport, so its pages
// surface as a readout above the document. A docked tool surface also carries
// the wiki's ViewportSet/Priority/height so it negotiates space with the other
// docked readouts.
func (e *Editor) createWikiViewport(buf *buffer.Buffer, def wikiDef, focus bool) *viewport.Viewport {
	if def.WinType == viewport.DocViewport && def.Dock == viewport.DockNone {
		return e.createMainViewport(buf, nil, focus)
	}
	// Wiki viewports open UNtagged here: buffer_open_file (and link follows) may
	// stack several help pages the ordinary way. The shared, mutually-exclusive
	// help slot is owned solely by help_toggle, which tags the viewport it opens.
	opts := e.docViewportOptions()
	opts.Type = def.WinType
	opts.Dock = def.Dock
	opts.ViewportSet = def.ViewportSet
	opts.Class = def.Name
	opts.Priority = def.Priority
	opts.MinHeight = def.MinHeight
	opts.MaxHeight = def.MaxHeight
	// A docked wiki page always shows its title bar; the title-less chrome is
	// reserved for Quick Help. (An undocked main-area page returned above never
	// reaches here.)
	opts.MessageTopCenter = def.Title
	opts.Buffer = buf
	opts.SetFocus = focus
	return e.ViewportManager.GetViewport(e.ViewportManager.CreateViewport(opts))
}

// gotoLaunchLine places a viewport's caret at a 1-based line (argwild "+N"),
// clamped to the buffer, mirroring the go_line command.
func (e *Editor) gotoLaunchLine(w *viewport.Viewport, line1 int) {
	if w == nil || w.Buffer == nil {
		return
	}
	target := line1 - 1
	if target < 0 {
		target = 0
	}
	if n := w.Buffer.GetLineCount(); target >= n {
		target = n - 1
	}
	w.SetCursorPos(viewport.Position{Line: target, Rune: 0})
	e.ensureCursorVisible(w)
}
