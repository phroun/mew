package editor

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/phroun/pawscript"
)

// tokenTimeout converts a timeout option value (seconds, 0 = never) to a
// PawScript token timeout (a non-positive duration disables the timeout).
// promptedResult runs a command whose outcome may only be known after the user
// answers a prompt. An answer that comes back before run returns is the
// command's result outright; anything slower suspends the calling PawScript
// sequence on a token and resumes it with the answer, so a script chained onto
// this command waits for the human instead of racing them.
//
// A token that has expired (PawScript force-cleans them after the promptTimeout
// option) takes its suspended sequence with it, so a late answer resolves
// nothing and says so rather than half-succeeding.
func (e *Editor) promptedResult(ctx *pawscript.Context, run func(func(bool))) pawscript.Result {
	var (
		token   string
		settled bool
		result  bool
		expired atomic.Bool
	)
	run(func(ok bool) {
		if token == "" {
			settled, result = true, ok
			return
		}
		if expired.Load() {
			e.ShowWarning("Prompt timed out")
			return
		}
		ctx.ResumeToken(token, ok)
	})
	if settled {
		return pawscript.BoolStatus(result)
	}
	token = e.PawScript.RequestToken(func(string) { expired.Store(true) }, "",
		tokenTimeout(e.Config.PromptTimeout))
	return pawscript.TokenResult(token)
}

func tokenTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		return -1
	}
	return time.Duration(seconds) * time.Second
}

// argString returns the i'th PawScript argument as a string, and whether it
// was present at all (nil arguments count as absent).
func argString(ctx *pawscript.Context, i int) (string, bool) {
	if i >= len(ctx.Args) || ctx.Args[i] == nil {
		return "", false
	}
	return fmt.Sprintf("%v", ctx.Args[i]), true
}

// argInt reads a whole-number argument. PawScript hands numbers over as
// whichever numeric type the literal parsed to, so go through the string form
// rather than type-switching every possibility.
func argInt(ctx *pawscript.Context, i int) (int, bool) {
	s, ok := argString(ctx, i)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, false
	}
	return n, true
}

// registerCommands registers all editor commands with PawScript.
func (e *Editor) registerCommands() {
	ps := e.PawScript

	// ifitfits viewport-tiling commands (viewport_new, viewport_zoom, …) live in
	// tilingcommands.go.
	e.registerTilingCommands(ps)
	e.registerHostCommands(ps)
	e.registerNavCommands(ps)
	e.registerPromptCommands(ps)
	e.registerKeymapCommands(ps)
	e.registerMotionCommands(ps)
	e.registerDebugCommands(ps)
	e.registerScrollCommands(ps)
	e.registerMatchingCommands(ps)
	e.registerSyntaxCommands(ps)
	e.registerCompletionCommands(ps)
	e.registerDirectionCommands(ps)
	e.registerDeleteCommands(ps)
	e.registerInsertCommands(ps)
	e.registerTerminalCommands(ps)
	e.registerHistoryCommands(ps)
	e.registerBlockCommands(ps)
	e.registerBufferCommands(ps)
	e.registerFilterCommands(ps)
	e.registerClipboardCommands(ps)
	e.registerScreenCommands(ps)
	e.registerDeadcatCommands(ps)
	e.registerSourceSafetyCommands(ps)
	e.registerSurfaceCommands(ps)
	e.registerFindCommands(ps)
	e.registerMessageCommands(ps)
	e.registerFocusCommands(ps)
	e.registerHelpCommands(ps)
	e.registerOptionCommands(ps)
}

// registerHostCommands registers the commands that reach the embedding host.
func (e *Editor) registerHostCommands(ps *pawscript.PawScript) {
	// System commands
	ps.RegisterCommand("exit", func(ctx *pawscript.Context) pawscript.Result {
		e.Running = false
		return pawscript.BoolStatus(true)
	})

	// show_desktop / hide_desktop ask the embedding host to reveal or hide its
	// desktop (e.g. a KittyTK viewport-manager host). No-ops in the standalone
	// editor, where no host wires the hooks.
	ps.RegisterCommand("show_desktop", func(ctx *pawscript.Context) pawscript.Result {
		if e.Config.ShowDesktop != nil {
			e.Config.ShowDesktop()
		}
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("hide_desktop", func(ctx *pawscript.Context) pawscript.Result {
		if e.Config.HideDesktop != nil {
			e.Config.HideDesktop()
		}
		return pawscript.BoolStatus(true)
	})

	// host_suspend asks the embedding host to hand the terminal back to the
	// shell and stop until continued. Fails where no host can suspend (the
	// standalone editor, a graphical host), so host_suspend|... falls through.
	ps.RegisterCommand("host_suspend", func(ctx *pawscript.Context) pawscript.Result {
		if e.Config.SuspendHost == nil {
			return pawscript.BoolStatus(false)
		}
		return pawscript.BoolStatus(e.Config.SuspendHost())
	})
}

// registerDebugCommands registers garland_balance and debug_marks.
func (e *Editor) registerDebugCommands(ps *pawscript.PawScript) {
	ps.RegisterCommand("garland_balance", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w != nil && w.Buffer != nil {
			w.Buffer.Balance()
		}
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("debug_marks", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			e.ShowWarning("No buffer")
			return pawscript.BoolStatus(false)
		}
		beginLine, beginRune, beginByte, beginExists := w.Buffer.GetMarkDebug("_block_begin")
		endLine, endRune, endByte, endExists := w.Buffer.GetMarkDebug("_block_end")

		msg := fmt.Sprintf("begin: L%d R%d @%d (%v), end: L%d R%d @%d (%v), cursor: L%d R%d",
			beginLine, beginRune, beginByte, beginExists,
			endLine, endRune, endByte, endExists,
			w.CursorPos().Line, w.CursorPos().Rune)
		e.ShowWarning(msg)
		return pawscript.BoolStatus(true)
	})
}
