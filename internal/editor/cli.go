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
	var primary *buffer.Buffer     // first MAIN-AREA document buffer (session snapshot)
	var lastWin *window.Window
	anyFocus := false // has any opened window claimed focus yet?

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
			// The first opened window wins focus. A docked wiki tool window
			// (mew help:/start) is a readout, not the session's main content:
			// it does not become the primary buffer, so an empty editing area
			// is still opened beneath it below.
			w, err := e.openLaunchFile(op.value, winOpts, !anyFocus)
			if err != nil {
				return nil, err
			}
			anyFocus = true
			lastWin = w
			if primary == nil && w.Type == window.DocWindow && w.Dock == window.DockNone {
				primary = w.Buffer
			}
			if pendingGoto > 0 {
				e.gotoLaunchLine(w, pendingGoto)
				pendingGoto = 0
			}
		}
	}

	// No main-area document opened — bare `mew`, `mew --showLineNumbers-`, or a
	// launch that named only docked tool surfaces (mew help:/start): open one
	// empty document window carrying the running per-window options so there is
	// an editing area. It takes focus only when nothing else already did.
	if primary == nil {
		buf := e.lib.New()
		w := e.createMainWindow(buf, winOpts, !anyFocus)
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
	// A registered wiki scheme ("mew help:/start") names a PAGE, not a literal
	// file: resolve it through the same machinery the Open prompt uses so the
	// real page file loads and the window is rooted in the wiki (in the
	// Type/dock the wiki declares). Without this the name fell through to a
	// plain OS open of "help:/start", which found nothing and came up blank.
	// Per-window launch options do not apply to a wiki's own (possibly docked)
	// window.
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
	return e.createMainWindow(buf, winOpts, focus), nil
}

// docWindowOptions returns a WindowOptions seeded with the editor's current
// per-window view defaults (line numbers, tab size, marks, read-only, browse,
// …). Callers stamp Type/Dock/Buffer/SetFocus (and, for a docked tool
// surface, WindowSet/Priority/height) on top before creating the window.
func (e *Editor) docWindowOptions() window.WindowOptions {
	return window.WindowOptions{
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
	}
}

// createMainWindow creates a main-buffer window for buf (focused when focus is
// set — the first opened file wins focus; the rest are background buffers
// reachable via buffer_next) and applies the per-window option overrides.
func (e *Editor) createMainWindow(buf *buffer.Buffer, winOpts map[string]string, focus bool) *window.Window {
	opts := e.docWindowOptions()
	opts.Type = window.DocWindow
	opts.Dock = window.DockNone
	opts.Buffer = buf
	opts.SetFocus = focus
	w := e.WindowManager.GetWindow(e.WindowManager.CreateWindow(opts))
	for name, value := range winOpts {
		e.setOption(w, name, value)
	}
	return w
}

// createWikiWindow creates the window a wiki page opens in, honoring the
// wiki's declared window Type and Dock: a wiki that leaves them at the zero
// value gets an ordinary main-area document window (identical to
// createMainWindow); help declares a top-docked ToolWindow, so its pages
// surface as a readout above the document. A docked tool surface also carries
// the wiki's WindowSet/Priority/height so it negotiates space with the other
// docked readouts.
func (e *Editor) createWikiWindow(buf *buffer.Buffer, def wikiDef, focus bool) *window.Window {
	if def.WinType == window.DocWindow && def.Dock == window.DockNone {
		return e.createMainWindow(buf, nil, focus)
	}
	// The help wiki shares ONE docked slot with the built-in Quick Help window
	// (both carry Tag "help"): clear it so a help page always opens in the same
	// docked position, mutually exclusive with whatever help was showing.
	if def.Name == helpWindowTag && def.Dock == window.DockTop {
		e.closeHelpWindows()
	}
	opts := e.docWindowOptions()
	opts.Type = def.WinType
	opts.Dock = def.Dock
	opts.WindowSet = def.WindowSet
	opts.Class = def.Name
	// Tag by wiki name so a wiki's pages share one docked slot: the help wiki
	// (Tag "help") is mutually exclusive with the built-in Quick Help window,
	// which carries the same tag.
	opts.Tag = def.Name
	opts.Priority = def.Priority
	opts.MinHeight = def.MinHeight
	opts.MaxHeight = def.MaxHeight
	opts.Buffer = buf
	opts.SetFocus = focus
	return e.WindowManager.GetWindow(e.WindowManager.CreateWindow(opts))
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
