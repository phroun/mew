package editor

import (
	"fmt"
	"strings"

	"github.com/phroun/argwild"
	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

// This file turns a parsed argwild command line into a mew launch: each long
// switch is a set_option applied to the buffer being configured, a bare "+N"
// is a go-to-line on the next file opened, and operands are files to open. The
// model is a left-to-right walk — switches mutate a running option set, and
// each file opens with the set as it stands at that point, so a switch after a
// file changes only the files that follow it. Options whose set_option target
// is the whole editor (not a window) are "global" and must precede the first
// file.

// cliPerWindowOptions (set_option routes these to a window's ViewState) and
// cliKnownOptions (every option set_option accepts at runtime) are both derived
// from optionSpecs in optionspec.go — the single canonical option table — so
// they can never drift from setOption/getOption. Load-time-only [general] keys
// (layout, mappings, projectConfig, useLocks, ...) are not part of that surface.

// cliOp is one step of the launch walk.
type cliOp struct {
	kind  int    // cliSetOption | cliOpenFile | cliGotoLine
	name  string // option name (lowercased) for cliSetOption
	value string // option value for cliSetOption; filename for cliOpenFile
	line  int    // 1-based line for cliGotoLine
}

const (
	cliSetOption = iota
	cliOpenFile
	cliGotoLine
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
				if op.kind == cliSetOption && !cliPerWindowOptions[op.name] && sawFile {
					return nil, fmt.Errorf("global option --%s must come before any file", op.name)
				}
				plan.ops = append(plan.ops, op)
			}
		case *argwild.Operand:
			name := s.Value.AsString()
			if name == "" {
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
	return err
}

// applyLaunch walks the plan: global options apply immediately (to the whole
// editor), per-window options accumulate into a running set applied to each
// file's window as it opens, and a pending +N jumps the next file's caret. It
// returns the primary buffer (the first file, or a lone empty buffer when no
// file was named) for the session's content snapshot.
func (e *Editor) applyLaunch(plan *launchPlan) (*buffer.Buffer, error) {
	winOpts := map[string]string{} // running per-window option set
	pendingGoto := 0               // 1-based line for the next file, 0 = none
	var primary *buffer.Buffer
	var lastWin *window.Window

	for _, op := range plan.ops {
		switch op.kind {
		case cliSetOption:
			if cliPerWindowOptions[op.name] {
				winOpts[op.name] = op.value
			} else if !e.setOption(nil, op.name, op.value) {
				return nil, fmt.Errorf("invalid value for --%s: %q", op.name, op.value)
			}
		case cliGotoLine:
			pendingGoto = op.line
		case cliOpenFile:
			w, err := e.openLaunchFile(op.value, winOpts, primary == nil)
			if err != nil {
				return nil, err
			}
			lastWin = w
			if primary == nil {
				primary = w.Buffer
			}
			if pendingGoto > 0 {
				e.gotoLaunchLine(w, pendingGoto)
				pendingGoto = 0
			}
		}
	}

	// No file named: open one empty buffer carrying the running per-window
	// options (bare `mew`, or `mew --showLineNumbers-`).
	if primary == nil {
		buf := e.lib.New()
		w := e.createMainWindow(buf, winOpts, true)
		primary = buf
		lastWin = w
	}
	// A trailing +N with no file after it lands on the last-opened window.
	if pendingGoto > 0 && lastWin != nil {
		e.gotoLaunchLine(lastWin, pendingGoto)
	}
	return primary, nil
}

// openLaunchFile loads a file into a fresh main-buffer window (the full open
// path: locks, backups, notices), applies the running per-window options, and
// focuses the first one.
func (e *Editor) openLaunchFile(filename string, winOpts map[string]string, focus bool) (*window.Window, error) {
	buf, err := e.loadBuffer(filename)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filename, err)
	}
	if !isMewPath(filename) {
		buf.SetFilename(e.normalizeDocPath(filename))
	}
	return e.createMainWindow(buf, winOpts, focus), nil
}

// createMainWindow creates a main-buffer window for buf (focused when focus is
// set — the first opened file wins focus; the rest are background buffers
// reachable via buffer_next) and applies the per-window option overrides.
func (e *Editor) createMainWindow(buf *buffer.Buffer, winOpts map[string]string, focus bool) *window.Window {
	id := e.WindowManager.CreateWindow(window.WindowOptions{
		Type:            window.DocWindow,
		Buffer:          buf,
		Dock:            window.DockNone,
		Priority:        0,
		SetFocus:        focus,
		ShowLineNumbers: e.Config.ShowLineNumbers,
		TabSize:         e.Config.TabSize,
		ShowInvisibles:  e.Config.ShowInvisibles,
		ShowBidi:        e.Config.ShowBidi,
		ShowMarks:       e.Config.ShowMarks,
		OverwriteMode:   e.Config.OverwriteMode,
		ReadOnly:        e.Config.ReadOnly,
		LinkBrowsing:    e.Config.LinkBrowsing,
		ShowRuler:       e.Config.ShowColumnRuler,
		SyntaxOverrides: e.Config.SyntaxOverrides,
	})
	w := e.WindowManager.GetWindow(id)
	for name, value := range winOpts {
		e.setOption(w, name, value)
	}
	return w
}

// gotoLaunchLine places a window's caret at a 1-based line (argwild "+N"),
// clamped to the buffer, mirroring the go_line command.
func (e *Editor) gotoLaunchLine(w *window.Window, line1 int) {
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
	w.SetCursorPos(window.Position{Line: target, Rune: 0})
	e.ensureCursorVisible(w)
}
