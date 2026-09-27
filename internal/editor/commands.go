package editor

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/phroun/ifitfits"
	"github.com/phroun/pawscript"

	"github.com/phroun/mew/internal/bidi"
	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/config"
	"github.com/phroun/mew/internal/viewport"
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

	// nav_cancel: turn link browse mode off on the focused viewport. Fails when
	// browse mode is not active, so nav_cancel|cancel|... chains fall through.
	ps.RegisterCommand("nav_cancel", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navCancel())
	})

	// nav_follow [always]: follow the link at the caret. With no argument (or
	// "true") it ALWAYS follows when the caret is within/at the edge of a
	// link's source, even in ordinary edit mode — so `^B F =nav_follow` jumps
	// without entering navigation mode first. With "false" it follows only in
	// navigation mode (a focused button); otherwise it fails, so a
	// `nav_follow false|accept|insert_newline` chain falls through to plain Enter.
	ps.RegisterCommand("nav_follow", func(ctx *pawscript.Context) pawscript.Result {
		always := true
		if arg, ok := argString(ctx, 0); ok && strings.EqualFold(strings.TrimSpace(arg), "false") {
			always = false
		}
		return pawscript.BoolStatus(e.navFollow(always))
	})

	// nav_next / nav_prior: move to the next/prior link (cycling).
	//
	// Bare, they ALWAYS act: from the caret's own link, or into the first link
	// from the caret when it is in none. With the argument "false" they are
	// GATED on a focused button instead, capturing only in browse mode so a
	// chain like tab = nav_next false|completion|insert '\t' yields to editing
	// when the caret is not inside a link. Same shape as nav_follow, and for
	// the same reason: a key bound alone wants the action, a key in a chain
	// wants to fall through.
	navArg := func(ctx *pawscript.Context) bool {
		if arg, ok := argString(ctx, 0); ok && strings.EqualFold(strings.TrimSpace(arg), "false") {
			return false
		}
		return true
	}
	ps.RegisterCommand("nav_next", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navLink(+1, navArg(ctx)))
	})
	ps.RegisterCommand("nav_prior", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navLink(-1, navArg(ctx)))
	})

	// nav_start: enter nav (browse) mode, focusing the first link at/after the
	// caret. nav_up/nav_down move to the nearest link on the next/prior link
	// line (paging when none remains on screen); nav_left/nav_right move to the
	// optically adjacent link on the same line (bidi-aware). All four act only
	// in active nav mode.
	ps.RegisterCommand("nav_start", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navStart())
	})
	// When a tiling operator is armed as a one-shot (viewport_<op> pending), the
	// four nav_* keys resolve that pending action in their direction instead of
	// moving between links (a persistent mode does NOT hijack them — only pending).
	ps.RegisterCommand("nav_down", func(ctx *pawscript.Context) pawscript.Result {
		if handled, ok := e.tilePendingNav(ctx, ifitfits.Down); handled {
			return pawscript.BoolStatus(ok)
		}
		return pawscript.BoolStatus(e.navVert(+1))
	})
	ps.RegisterCommand("nav_up", func(ctx *pawscript.Context) pawscript.Result {
		if handled, ok := e.tilePendingNav(ctx, ifitfits.Up); handled {
			return pawscript.BoolStatus(ok)
		}
		return pawscript.BoolStatus(e.navVert(-1))
	})
	ps.RegisterCommand("nav_right", func(ctx *pawscript.Context) pawscript.Result {
		if handled, ok := e.tilePendingNav(ctx, ifitfits.Right); handled {
			return pawscript.BoolStatus(ok)
		}
		return pawscript.BoolStatus(e.navHoriz(+1))
	})
	ps.RegisterCommand("nav_left", func(ctx *pawscript.Context) pawscript.Result {
		if handled, ok := e.tilePendingNav(ctx, ifitfits.Left); handled {
			return pawscript.BoolStatus(ok)
		}
		return pawscript.BoolStatus(e.navHoriz(-1))
	})

	// nav_history_prior / nav_history_next: walk the focused viewport's
	// buffer-swap history (following a link swaps the buffer in place,
	// stacking the departed binding — see Viewport.SwapBuffer). Prior returns
	// to where you were; next re-advances. Both fail when there is no history
	// in that direction, so chains fall through.
	//
	// nav_history_prior takes the same [always] first argument as nav_follow.
	// Bare (or "true") it always acts. With "false" it is GATED like
	// nav_follow false — only the actively focused link button of the focused
	// viewport lets it act — EXCEPT that a read-only document already in
	// navigation mode passes even off a link (nothing there is editable, so the
	// key is free to mean "go back"). See navHistoryGatePasses.
	ps.RegisterCommand("nav_history_prior", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navHistory(-1, navArg(ctx)))
	})
	ps.RegisterCommand("nav_history_next", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navHistory(+1, true))
	})

	// nav_clear forgets every visited link (editor-wide repaint to the
	// unvisited style). nav_history_clear empties the focused viewport's whole
	// back/forward history, releasing stacked bindings except any that hold
	// the LAST reference to a buffer — those move to the viewport's graveyard,
	// held for the eventual save decision.
	ps.RegisterCommand("nav_clear", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navClearVisited())
	})
	ps.RegisterCommand("nav_history_clear", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.navHistoryClear())
	})

	ps.RegisterCommand("cancel", func(ctx *pawscript.Context) pawscript.Result {
		focusedViewport := e.ViewportManager.GetFocusedViewport()
		if focusedViewport != nil && focusedViewport.Type == viewport.PromptViewport {
			// Capture callbacks before removing viewport
			legacyCallback := focusedViewport.Callback
			promptCallback := focusedViewport.PromptCallback

			// Remove prompt viewport FIRST so focus returns to main buffer
			delete(e.confirmKey, focusedViewport.ID)
			e.ViewportManager.RemoveViewport(focusedViewport.ID)

			// Call the appropriate callback
			if promptCallback != nil {
				promptCallback(false, "", "")
			} else if legacyCallback != nil {
				legacyCallback("", false)
			}
			return pawscript.BoolStatus(true)
		}
		// No prompt to dismiss. A find is not something to cancel: it has no
		// prompt once its first search has begun and ^L just goes to the next
		// match, so ^C falls through to whatever else it is bound to. The
		// single exception is the search slow enough to
		// have put a message on screen naming this very key; that message is a
		// promise, and cancelFind is where it is kept.
		if e.cancelFind() {
			return pawscript.BoolStatus(true)
		}
		return pawscript.BoolStatus(false)
	})

	ps.RegisterCommand("accept", func(ctx *pawscript.Context) pawscript.Result {
		focusedViewport := e.ViewportManager.GetFocusedViewport()
		if focusedViewport != nil && focusedViewport.Type == viewport.PromptViewport {
			// Capture callbacks before removing viewport
			legacyCallback := focusedViewport.Callback
			promptCallback := focusedViewport.PromptCallback

			// Get buffer content from line 0 (for backward compatibility)
			bufferContent := ""
			if focusedViewport.Buffer != nil && focusedViewport.Buffer.GetLineCount() > 0 {
				bufferContent = strings.TrimRight(focusedViewport.Buffer.GetLine(0), "\n\r")
			}

			// Get the text from the line where the cursor is positioned
			// This is the key difference from TypeScript - we read cursor line, not line 0
			cursorLineText := ""
			if focusedViewport.Buffer != nil {
				cursorLine := focusedViewport.CursorPos().Line
				if cursorLine < focusedViewport.Buffer.GetLineCount() {
					cursorLineText = strings.TrimRight(focusedViewport.Buffer.GetLine(cursorLine), "\n\r")
				}
			}

			// Remove prompt viewport FIRST so focus returns to main buffer
			// This ensures any output from the callback goes to the right viewport
			delete(e.confirmKey, focusedViewport.ID)
			e.ViewportManager.RemoveViewport(focusedViewport.ID)

			// Call the appropriate callback
			if promptCallback != nil {
				promptCallback(true, bufferContent, cursorLineText)
			} else if legacyCallback != nil {
				// Legacy callback uses cursorLineText as input (for single-line prompts)
				legacyCallback(cursorLineText, true)
			}
			return pawscript.BoolStatus(true)
		}
		return pawscript.BoolStatus(false)
	})

	// Key mapping commands (matching TypeScript version)
	ps.RegisterCommand("map", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) < 2 {
			e.ShowWarning("Usage: map <key>, <command>")
			return pawscript.BoolStatus(false)
		}
		key := fmt.Sprintf("%v", ctx.Args[0])
		command := fmt.Sprintf("%v", ctx.Args[1])
		e.KeyProcessor.MapKey(key, command)
		// A runtime remap: credit it to AuthorRemapped and give it a precedence
		// above every config binding so it wins the key-badge tie-break.
		if e.mappingOrigins == nil {
			e.mappingOrigins = make(map[string]config.MappingOrigin)
		}
		e.remapPrec++
		e.mappingOrigins[key] = config.MappingOrigin{
			Source:     config.SourceRemap,
			Precedence: e.remapPrec,
			Author:     config.AuthorRemapped,
		}
		e.ShowNotification("Mapped " + key + " -> " + command)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("unmap", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) < 1 {
			e.ShowWarning("Usage: unmap <key>")
			return pawscript.BoolStatus(false)
		}
		key := fmt.Sprintf("%v", ctx.Args[0])
		e.KeyProcessor.UnmapKey(key)
		e.ShowNotification("Unmapped " + key)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("remap", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) < 1 {
			e.ShowWarning("Usage: remap <key>")
			return pawscript.BoolStatus(false)
		}
		key := fmt.Sprintf("%v", ctx.Args[0])
		// Restore from original config
		if cmd, ok := e.LoadedConfig.Mappings[key]; ok {
			e.KeyProcessor.MapKey(key, cmd)
			e.ShowNotification("Restored mapping: " + key + " -> " + cmd)
			return pawscript.BoolStatus(true)
		}
		e.ShowWarning("No original mapping for " + key)
		return pawscript.BoolStatus(false)
	})

	// after_key sets the focused viewport's after-key pseudo-binding: the
	// PawScript command run each time a key's binding activity resolves while
	// that viewport owns the keyboard (see viewport.Viewport.AfterKey). With no
	// argument (or ""), it clears the binding.
	ps.RegisterCommand("after_key", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil {
			return pawscript.BoolStatus(false)
		}
		script, _ := argString(ctx, 0)
		w.AfterKey = script
		return pawscript.BoolStatus(true)
	})

	// not_implemented is a placeholder for a command that is planned but not
	// written yet: it warns, names what was asked for, and reports FALSE so a
	// chain falls through it exactly as an unhandled command would.
	//
	// It exists so a planned feature can be advertised in one place instead of
	// two. Wiring only the MENU item to a toast would leave the key it
	// advertises doing nothing at all, which is the worse half of the lie -
	// the user presses the key the menu just taught them and gets silence.
	// Bind the key here too and both routes answer the same way.
	ps.RegisterCommand("not_implemented", func(ctx *pawscript.Context) pawscript.Result {
		what, _ := argString(ctx, 0)
		msg := "Not yet implemented"
		if strings.TrimSpace(what) != "" {
			msg = what + ": not yet implemented"
		}
		// One tag for the whole family, so trying several planned items in a
		// row REPLACES the toast rather than stacking a pile of them - they all
		// say the same thing, and the newest is the only one that is about what
		// the user just pressed.
		e.ShowWarningTagged(msg, "not_implemented")
		return pawscript.BoolStatus(false)
	})

	ps.RegisterCommand("mappings_show", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) < 1 {
			e.ShowWarning("Usage: mappings_show <key>")
			return pawscript.BoolStatus(false)
		}
		key := fmt.Sprintf("%v", ctx.Args[0])
		if cmd := e.KeyProcessor.GetMapping(key); cmd != "" {
			e.ShowWarning(key + " -> " + cmd)
			return pawscript.BoolStatus(true)
		}
		e.ShowWarning("No mapping for " + key)
		return pawscript.BoolStatus(false)
	})

	ps.RegisterCommand("mappings_list", func(ctx *pawscript.Context) pawscript.Result {
		mappings := e.KeyProcessor.GetAllMappings()
		if len(mappings) == 0 {
			e.ShowWarning("No key mappings defined")
			return pawscript.BoolStatus(false)
		}
		// Build list content
		var content strings.Builder
		content.WriteString("Key Mappings:\n")
		for key, cmd := range mappings {
			content.WriteString(fmt.Sprintf("  %s -> %s\n", key, cmd))
		}
		// Show in a work buffer viewport
		buf := e.lib.NewFromString(content.String())
		e.ViewportManager.CreateViewport(viewport.ViewportOptions{
			Type:             viewport.ToolViewport,
			ViewportSet:      "help",
			Class:            "mappings",
			Dock:             viewport.DockTop,
			Priority:         100,
			MinHeight:        5,
			MaxHeight:        15,
			MessageTopCenter: "Key Mappings",
			Buffer:           buf,
			ShowLineNumbers:  false,
		})
		e.RequestRender()
		return pawscript.BoolStatus(true)
	})

	// Movement commands (using TypeScript naming convention)
	ps.RegisterCommand("go_char_prior", func(ctx *pawscript.Context) pawscript.Result {
		e.moveCursor(-1, 0)
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_char_next", func(ctx *pawscript.Context) pawscript.Result {
		e.moveCursor(1, 0)
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_line_prior", func(ctx *pawscript.Context) pawscript.Result {
		e.moveCursor(0, -1)
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_line_next", func(ctx *pawscript.Context) pawscript.Result {
		e.moveCursor(0, 1)
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_line_beg", func(ctx *pawscript.Context) pawscript.Result {
		e.cursorToLineStart()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_line_end", func(ctx *pawscript.Context) pawscript.Result {
		e.cursorToLineEnd()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_buffer_beg", func(ctx *pawscript.Context) pawscript.Result {
		e.cursorToBufferStart()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_buffer_end", func(ctx *pawscript.Context) pawscript.Result {
		e.cursorToBufferEnd()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

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

	ps.RegisterCommand("go_page_prior", func(ctx *pawscript.Context) pawscript.Result {
		e.pageUp()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_page_next", func(ctx *pawscript.Context) pawscript.Result {
		e.pageDown()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	// Scroll commands park the viewport WITHOUT moving the caret — the
	// programmatic analogue of the mouse wheel. Each detaches the view from
	// caret-follow (ScrollDetached) so the per-frame follow leaves it put until a
	// cursor-movement or edit command re-engages it. They mirror the go_* family
	// name-for-name (scroll_line ~ go_line, scroll_line_next ~ go_line_next, ...).
	ps.RegisterCommand("scroll_line_prior", func(ctx *pawscript.Context) pawscript.Result {
		e.scrollViewByLines(e.resolveTargetMain(), -1)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("scroll_line_next", func(ctx *pawscript.Context) pawscript.Result {
		e.scrollViewByLines(e.resolveTargetMain(), 1)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("scroll_page_prior", func(ctx *pawscript.Context) pawscript.Result {
		if w := e.resolveTargetMain(); w != nil {
			_, page := e.pageSize(w)
			e.scrollViewByLines(w, -page)
		}
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("scroll_page_next", func(ctx *pawscript.Context) pawscript.Result {
		if w := e.resolveTargetMain(); w != nil {
			_, page := e.pageSize(w)
			e.scrollViewByLines(w, page)
		}
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("scroll_buffer_beg", func(ctx *pawscript.Context) pawscript.Result {
		e.scrollViewTo(e.resolveTargetMain(), 0)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("scroll_buffer_end", func(ctx *pawscript.Context) pawscript.Result {
		if w := e.resolveTargetMain(); w != nil && w.Buffer != nil {
			viewHeight, _ := e.pageSize(w)
			// Park the last line on the bottom row (clamped when the buffer is
			// shorter than the view).
			e.scrollViewTo(w, w.Buffer.GetLineCount()-viewHeight)
		}
		return pawscript.BoolStatus(true)
	})

	// scroll_line parks a given 1-based line at the TOP of the view (the scroll
	// analogue of go_line); it takes a line-number argument or, lacking one,
	// prompts for it exactly as go_line does.
	// scroll_viewport <id> <line>: park a NAMED viewport at a 0-based top
	// line. The host's graphical scrollbar drives scrolling with this — it
	// owns the bar in pixel space, so it computes the line itself and sends a
	// whole number; mew never scrolls by a fraction of a line. Like the wheel
	// and mew's own bar, it leaves the view detached and steals no focus, so a
	// background pane can be scrolled without disturbing the caret.
	ps.RegisterCommand("scroll_viewport", func(ctx *pawscript.Context) pawscript.Result {
		id, ok := argString(ctx, 0)
		if !ok {
			return pawscript.BoolStatus(false)
		}
		lineArg, ok := argString(ctx, 1)
		if !ok {
			return pawscript.BoolStatus(false)
		}
		top, err := strconv.Atoi(strings.TrimSpace(lineArg))
		if err != nil {
			return pawscript.BoolStatus(false)
		}
		w := e.ViewportManager.GetViewport(strings.TrimSpace(id))
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		e.scrollViewTo(w, top)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("scroll_line", func(ctx *pawscript.Context) pawscript.Result {
		scrollLine := func(input string) bool {
			n, err := strconv.Atoi(strings.TrimSpace(input))
			if err != nil || n < 1 {
				e.ShowWarning("Invalid line number")
				return false
			}
			e.scrollLineTop(n)
			return true
		}
		if arg, ok := argString(ctx, 0); ok {
			return pawscript.BoolStatus(scrollLine(arg))
		}
		expired := &atomic.Bool{}
		token := e.PawScript.RequestToken(func(string) { expired.Store(true) }, "",
			tokenTimeout(e.Config.PromptTimeout))
		e.PromptMgr.PromptForInput("Scroll to line: ", "", func(accepted bool, _, input string) {
			defer e.RequestRender()
			if expired.Load() {
				e.ShowWarning("Prompt timed out")
				return
			}
			if !accepted || strings.TrimSpace(input) == "" {
				ctx.ResumeToken(token, false)
				return
			}
			ctx.ResumeToken(token, scrollLine(input))
		}, "scrollline")
		return pawscript.TokenResult(token)
	})

	ps.RegisterCommand("go_word_prior", func(ctx *pawscript.Context) pawscript.Result {
		e.moveToPriorWord()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_word_next", func(ctx *pawscript.Context) pawscript.Result {
		e.moveToNextWord()
		e.trackMove()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_match", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.gotoMatchingBracket()
		e.trackMove()
		return pawscript.BoolStatus(ok)
	})

	// syntax_context reports what the syntax highlighter's machine is doing
	// at the caret. With no argument the result is "comment", "string" or
	// "code"; an argument selects a detail: 'state' (machine state name),
	// 'class' (color class), 'syntax' (innermost grammar at the caret, which
	// may be an embedded language), or 'stack' (embedded-language chain,
	// innermost first, space-separated). Fails when no grammar applies.
	ps.RegisterCommand("syntax_context", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		sc, ok := e.syntaxContextAt(w.Buffer, w.CursorPos().Line, w.CursorPos().Rune)
		if !ok {
			return pawscript.BoolStatus(false)
		}
		which := ""
		if len(ctx.Args) > 0 {
			which = strings.ToLower(fmt.Sprintf("%v", ctx.Args[0]))
		}
		var out string
		switch which {
		case "":
			switch {
			case sc.Comment:
				out = "comment"
			case sc.String:
				out = "string"
			default:
				out = "code"
			}
		case "state":
			out = sc.State
		case "class":
			out = sc.Class
		case "syntax":
			out = sc.Syntax
		case "stack":
			out = strings.Join(sc.Stack, " ")
		default:
			e.ShowWarning("syntax_context: unknown detail " + which)
			return pawscript.BoolStatus(false)
		}
		ctx.SetResult(out)
		return pawscript.BoolStatus(true)
	})

	// nop does nothing, successfully. Bind a key to it to deliberately
	// disable the key (unbinding instead restores the key's default
	// handling, e.g. self-insert).
	ps.RegisterCommand("nop", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(true)
	})

	// completion invokes the focused viewport's completion handler, if it has
	// one (filename prompts do). It returns that handler's result; with no
	// handler, or when the handler declines, it fails — so a binding like
	// completion|insert '\t' falls through to inserting a tab.
	ps.RegisterCommand("completion", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.CompletionCallback == nil {
			return pawscript.BoolStatus(false)
		}
		return pawscript.BoolStatus(w.CompletionCallback())
	})

	// rtl reports whether the caret currently sits inside a right-to-left
	// segment of its line (resolved under the configured base direction).
	ps.RegisterCommand("rtl", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		line := strings.TrimRight(w.Buffer.GetLine(w.CursorPos().Line), "\n\r")
		return pawscript.BoolStatus(bidi.RTLAt([]rune(line), w.CursorPos().Rune, e.winRTL(w)))
	})

	// go_pos_prior / go_pos_next walk the caret backward and forward through the
	// cursor ring — the trail of recent edit positions. They do not themselves
	// count as deliberate movements (they leave hasMoved untouched), so a run of
	// them stays a single navigation session until an edit or a real move.
	ps.RegisterCommand("go_pos_prior", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.cursorRingGo(false))
	})

	ps.RegisterCommand("go_pos_next", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.cursorRingGo(true))
	})

	// Editing commands (using TypeScript naming convention)
	ps.RegisterCommand("del_char_prior", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		// A backspace over a read-only buffer declines (false, no edit) but —
		// unlike every other mutation — WITHOUT the "Buffer is read-only" toast: a
		// key as ordinary as backspace should not nag. Other commands still warn.
		if w != nil && e.viewportReadOnly(w) {
			return pawscript.BoolStatus(false)
		}
		// When deleteNewlineAsChar is off for this viewport, a backspace at the
		// start of a line declines rather than joining it with the line above.
		// Fail with false and no visible error; no edit, so the undo coalescing run
		// is untouched.
		if w != nil && w.ViewState.ProtectNewlines && e.deleteWouldRemoveNewline(w, false) {
			return pawscript.BoolStatus(false)
		}
		e.deleteCharBefore()
		e.trackEdit()
		e.editCoalesced = true // a single-point edit: coalesce the undo run
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("del_char_next", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		// A forward-delete over a read-only buffer declines silently too — no
		// "Buffer is read-only" toast (see del_char_prior).
		if w != nil && e.viewportReadOnly(w) {
			return pawscript.BoolStatus(false)
		}
		// Same guard forward: a forward-delete at end of line declines rather than
		// pulling the next line up. No edit → coalescing untouched.
		if w != nil && w.ViewState.ProtectNewlines && e.deleteWouldRemoveNewline(w, true) {
			return pawscript.BoolStatus(false)
		}
		e.deleteCharAt()
		e.trackEdit()
		e.editCoalesced = true // a single-point edit: coalesce the undo run
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("del_line", func(ctx *pawscript.Context) pawscript.Result {
		e.deleteLine()
		e.trackEdit()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("del_word_beg", func(ctx *pawscript.Context) pawscript.Result {
		e.deleteToWordStart()
		e.trackEdit()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("del_word_end", func(ctx *pawscript.Context) pawscript.Result {
		e.deleteToWordEnd()
		e.trackEdit()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("del_line_beg", func(ctx *pawscript.Context) pawscript.Result {
		e.deleteToLineStart()
		e.trackEdit()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("del_line_end", func(ctx *pawscript.Context) pawscript.Result {
		e.deleteToLineEnd()
		e.trackEdit()
		return pawscript.BoolStatus(true)
	})

	// Whitespace trimming, mirroring the del_line_* family. These trim the
	// current line's leading/trailing spaces and tabs; the line terminator
	// itself is never removed. Each reports true only when something was
	// actually trimmed, so scripts can chain alternatives with | and &.
	ps.RegisterCommand("trim_line_beg", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.trimLineStart()
		e.trackEdit()
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("trim_line_end", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.trimLineEnd()
		e.trackEdit()
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("trim_line", func(ctx *pawscript.Context) pawscript.Result {
		// Trims both ends: two separate deletes that must undo as one step.
		var buf *buffer.Buffer
		if w := e.ViewportManager.GetFocusedViewport(); w != nil {
			buf = w.Buffer
		}
		if buf != nil {
			buf.BeginUserCommand("trim_line")
			defer buf.EndUserCommand()
		}
		trimmedStart := e.trimLineStart()
		trimmedEnd := e.trimLineEnd()
		e.trackEdit()
		return pawscript.BoolStatus(trimmedStart || trimmedEnd)
	})

	// insert and insert_newline DISPATCH on what the focused viewport is.
	//
	// A viewport running a terminal session gets the input sent to the child
	// process, exactly as tinput would; anything else gets the buffer edit that
	// used to carry these names (now buffer_insert / buffer_insert_newline).
	//
	// This is why the keymaps need no pty variant. tab, return, and every
	// self-inserting key already end in `insert` or `insert_newline`, so typing
	// into a terminal viewport reaches the shell through the bindings that were
	// already there — and the same keys keep editing text everywhere else. One
	// name, two meanings, chosen by what is under the caret.
	ps.RegisterCommand("insert", func(ctx *pawscript.Context) pawscript.Result {
		if e.focusedPTY() != nil {
			if len(ctx.Args) > 0 {
				if sb, ok := ctx.Args[0].(pawscript.StoredBytes); ok {
					return pawscript.BoolStatus(e.ptySendBytes(sb.Data()))
				}
				return pawscript.BoolStatus(e.ptySendBytes([]byte(fmt.Sprintf("%v", ctx.Args[0]))))
			}
			return pawscript.BoolStatus(false)
		}
		return pawscript.BoolStatus(e.bufferInsertArgs(ctx.Args))
	})

	// replace_prior <n>, '<text>' stands text in place of the n characters
	// immediately before the caret. It exists for input methods, and macOS's
	// press-and-hold accent palette above all: that palette COMMITS the held
	// letter the moment the key goes down, so choosing an accent has to remove
	// a character that is already in the document.
	//
	// The host says how many, because only the host can know. It watched the
	// key commit the letter and watched an input method take the key over; mew
	// sees the finished text and nothing about what it stands for.
	//
	// n of 0 is an ordinary insert, which is what every composition that
	// appends rather than replaces sends — a CJK candidate, and every host that
	// cannot know a replacement count at all.
	ps.RegisterCommand("replace_prior", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) < 2 {
			return pawscript.BoolStatus(false)
		}
		n, err := strconv.Atoi(strings.TrimSpace(fmt.Sprintf("%v", ctx.Args[0])))
		if err != nil || n < 0 {
			return pawscript.BoolStatus(false)
		}
		text := fmt.Sprintf("%v", ctx.Args[1])
		if sb, ok := ctx.Args[1].(pawscript.StoredBytes); ok {
			text = string(sb.Data())
		}
		// A viewport running a child process has no document to replace in, so
		// the replacement is made the way a person would make it: erase what it
		// stands in for, then type the new text. The erase goes as the child's
		// own backspace, encoded by the terminal that knows what this child
		// negotiated (see ptyEraseBefore).
		//
		// It has to be sent, because nothing else will. The toolkit swallowed
		// the platform's Backspace on the way in — it belonged to the palette,
		// not to the user — so without this the accent lands after the letter
		// it was chosen to replace.
		//
		// A child with no translator still gets the text. Losing the accent
		// entirely would be worse than leaving the letter in front of it.
		if e.focusedPTY() != nil {
			e.ptyEraseBefore(n)
			return pawscript.BoolStatus(e.ptySendBytes([]byte(text)))
		}
		return pawscript.BoolStatus(e.replacePrior(n, text))
	})

	// preedit '<text>', <caret>, <covers>, <clauseStart>, <clauseLen> shows what
	// an input method is still composing: painted at the caret, not put in the
	// document. An empty text ends it.
	//
	// covers is how many committed characters before the caret the composition
	// stands OVER and hides. macOS's press-and-hold palette commits the held
	// letter before it opens, so without this the line shows the letter and the
	// accent chosen to replace it side by side for as long as the palette is
	// up. Nothing is deleted to hide it — ending the composition brings it
	// straight back, which is what dismissing a palette means.
	//
	// Not stored, because storing it would mean un-storing it on every update —
	// a Japanese input method rewrites the whole composition on each keystroke —
	// and every one of those round trips would go through the undo history.
	// It is synthesized into the line at paint time instead, the way a control
	// character is painted "^X" without the buffer holding two runes.
	//
	// caret is the input method's own cursor within the text, which is what
	// shows progress through a long composition. It defaults to the end.
	//
	// clauseStart and clauseLen mark the segment being CONVERTED, when the
	// input method distinguishes one. A Japanese composition is several
	// clauses and a candidate list changes only the selected one — "らなに"
	// converts to "羅なに" with the tail still in kana — so the clause is
	// painted apart from the rest, which is what tells the untouched remainder
	// from characters the composition failed to replace.
	ps.RegisterCommand("preedit", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil {
			return pawscript.BoolStatus(false)
		}
		// A viewport running a child process paints the child's grid, not a
		// document: there is no line to synthesize a composition into.
		if e.focusedPTY() != nil {
			return pawscript.BoolStatus(false)
		}
		text := ""
		if len(ctx.Args) > 0 {
			text = fmt.Sprintf("%v", ctx.Args[0])
		}
		runes := []rune(text)
		caret := len(runes)
		if len(ctx.Args) > 1 {
			if n, err := strconv.Atoi(strings.TrimSpace(fmt.Sprintf("%v", ctx.Args[1]))); err == nil {
				caret = n
			}
		}
		covers := 0
		if len(ctx.Args) > 2 {
			if n, err := strconv.Atoi(strings.TrimSpace(fmt.Sprintf("%v", ctx.Args[2]))); err == nil && n > 0 {
				covers = n
			}
		}
		clauseStart, clauseLen := 0, 0
		if len(ctx.Args) > 4 {
			s, err1 := strconv.Atoi(strings.TrimSpace(fmt.Sprintf("%v", ctx.Args[3])))
			n, err2 := strconv.Atoi(strings.TrimSpace(fmt.Sprintf("%v", ctx.Args[4])))
			if err1 == nil && err2 == nil {
				clauseStart, clauseLen = s, n
			}
		}
		// Empty text goes through too, rather than clearing here: whether it
		// ENDS the composition on its way to a commit or CANCELS it turns on
		// the extent it still names, and SetPreedit is where that is decided.
		w.SetPreedit(viewport.Preedit{
			Text: runes, Caret: caret, Covers: covers,
			ClauseStart: clauseStart, ClauseLen: clauseLen,
		})
		e.RequestRender()
		return pawscript.BoolStatus(true)
	})

	// preedit_commit '<text>' takes a finished composition into the document,
	// in place of THE REGION THE COMPOSITION STOOD OVER.
	//
	// Anchored, not measured from the caret, which is the whole reason it is
	// its own command rather than a replace_prior. A composition is dismissed
	// by typing: macOS commits whatever was selected in its palette and the
	// keystroke that dismissed it lands first, so by the time this arrives the
	// caret has moved past a character the composition never covered.
	// Counting back from the caret replaced that character instead — the
	// accent ate it and the letter stayed, "oò" with the "." gone.
	//
	// With no composition standing it is an ordinary insert, which is what a
	// host that never opened one sends.
	ps.RegisterCommand("preedit_commit", func(ctx *pawscript.Context) pawscript.Result {
		text := ""
		if len(ctx.Args) > 0 {
			text = fmt.Sprintf("%v", ctx.Args[0])
		}
		return pawscript.BoolStatus(e.preeditCommit(text))
	})

	ps.RegisterCommand("insert_newline", func(ctx *pawscript.Context) pawscript.Result {
		if e.focusedPTY() != nil {
			// A shell wants CR for Enter, not LF: that is what a terminal
			// sends and what line discipline turns back into a newline.
			return pawscript.BoolStatus(e.ptySendBytes([]byte{'\r'}))
		}
		return pawscript.BoolStatus(e.bufferInsertNewline())
	})

	ps.RegisterCommand("buffer_insert", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.bufferInsertArgs(ctx.Args))
	})

	// buffer_insert_newline breaks the line like `buffer_insert '\n'`, then — when the
	// autoIndent option is on for the viewport — repeats the split line's
	// leading whitespace so the new line starts under its text. It is the tail
	// of the default Enter binding (nav_follow false|accept|insert_newline).
	// The plain name now DISPATCHES — see the pair registered above.
	ps.RegisterCommand("buffer_insert_newline", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.bufferInsertNewline())
	})

	// insert_bidi_control inserts a Unicode bidi control by short name (lrm,
	// rlm, alm, fsi, lri, rli, pdi) — otherwise behaving exactly like insert.
	// With no argument it prompts; "?" shows the legend and re-prompts.
	ps.RegisterCommand("insert_bidi_control", func(ctx *pawscript.Context) pawscript.Result {
		if arg, ok := argString(ctx, 0); ok {
			return pawscript.BoolStatus(e.insertBidiControl(arg))
		}
		expired := &atomic.Bool{}
		token := e.PawScript.RequestToken(func(string) { expired.Store(true) }, "",
			tokenTimeout(e.Config.PromptTimeout))
		var ask func()
		ask = func() {
			e.PromptMgr.PromptForInput("Insert control mark [lrm/rlm/alm, fsi/lri/rli, pdi, ?]: ", "",
				func(accepted bool, _, input string) {
					defer e.RequestRender()
					if expired.Load() {
						e.ShowWarning("Prompt timed out")
						return
					}
					name := strings.ToLower(strings.TrimSpace(input))
					if !accepted || name == "" {
						ctx.ResumeToken(token, false)
						return
					}
					if name == "?" {
						e.ShowNotification("lrm=left-to-right, rlm=right-to-left, alm=arabic letter mark")
						e.ShowNotification("fsi=first strong isolate, lri=left-to-right isolate, rli=right-to-left-isolate, pdi=pop directional isolate")
						ask() // re-prompt with the same prompt
						return
					}
					if _, ok := bidiControlRune(name); !ok {
						e.ShowWarning("Unknown control mark: " + name)
						ask() // stay in the loop on an unrecognized name
						return
					}
					ctx.ResumeToken(token, e.insertBidiControl(name))
				}, "bidictl")
		}
		ask()
		return pawscript.TokenResult(token)
	})

	// insert_rune inserts one Unicode scalar by CODE POINT: insert_rune "05D0",
	// or with no argument a prompt. Hex by default (U+xxxx, 0xxxxx or bare),
	// "#" prefixes decimal. The companion to insert_bidi_control for
	// every scalar that has no name of its own.
	// exec turns the focused buffer into a terminal session. The working
	// directory comes from the buffer itself (blank when it has no filename -
	// the HOST decides what that means); the command is the argument, or a
	// prompt when none is given.
	//
	// This deliberately shadows PawScript's own exec. PawScript is built for
	// exactly that: a host replacing an internal with its own version so that
	// client code reaches the sandboxed one. mew's exec cannot run anything by
	// itself - it can only ask its host.
	// A SECOND argument names the host's method — exec "cmd.exe" "2" — for a
	// host that has more than one way to make a terminal and no way to know
	// from here which one this machine wants. Blank is the host's default.
	// exec takes ANY NUMBER of arguments, joins them with single spaces, and
	// runs the whole line back through argwild — the same parser the host app
	// uses on its own command line at boot, so there is one notation to learn
	// rather than two. mew's switches come first (--pty=NAME), then the
	// program, then the program's own arguments; argwild's phase model does the
	// separating. See execargs.go for the spellings.
	//
	// shell is exec with the program left to the HOST: it asks for the user's
	// login shell rather than naming bash or cmd.exe, because which one that is
	// depends on the operating system and on the user's own account and mew can
	// see neither. Everything else — the compositing, --pty=, named arguments,
	// the phase rules — is identical, so the two are registered from one body.
	// Any further words are the shell's own arguments, which is also how a
	// program gets run through it: `shell "-c make"`.
	registerExecLike := func(name string, shell bool) {
		parse := parseExecLineNamed
		if shell {
			parse = parseShellLineNamed
		}
		request := func(spec execSpec) bool {
			// A routed stream (--inblock/--outblock or --stdin/--stdout/--stderr)
			// makes this a FILTER, not a terminal: the child runs on pipes and mew
			// feeds/reads its streams itself. Handled on its own path.
			if spec.filtering() {
				return e.runFilter(spec)
			}
			pol := spec.sizePolicy()
			if spec.Shell {
				return e.execRequestShellPolicy(spec.Args, spec.Method, pol, spec.Capture, spec.CaptureFormat)
			}
			return e.execRequestArgsPolicy(spec.Program, spec.Args, spec.Method, pol, spec.Capture, spec.CaptureFormat)
		}
		ps.RegisterCommand(name, func(ctx *pawscript.Context) pawscript.Result {
			var parts []string
			for i := 0; ; i++ {
				v, ok := argString(ctx, i)
				if !ok {
					break
				}
				parts = append(parts, v)
			}
			run := func(line string) bool {
				spec, err := parse(line, ctx.NamedArgs)
				if err != nil {
					e.ShowWarning(name + ": " + err.Error())
					return false
				}
				return request(spec)
			}
			if line := joinExecArgs(parts); strings.TrimSpace(line) != "" {
				return pawscript.BoolStatus(run(line))
			}
			// Nothing positional still counts as a request when it can stand on
			// its own: named arguments carrying a program, or shell, which needs
			// no arguments at all to mean something.
			spec, ok, err := namedOnlyRequest(ctx.NamedArgs, shell)
			if err != nil {
				e.ShowWarning(name + ": " + err.Error())
				return pawscript.BoolStatus(false)
			}
			if ok {
				return pawscript.BoolStatus(request(spec))
			}
			expired := &atomic.Bool{}
			token := e.PawScript.RequestToken(func(string) { expired.Store(true) }, "",
				tokenTimeout(e.Config.PromptTimeout))
			e.PromptMgr.PromptForInput("Execute what? ", "",
				func(accepted bool, _, input string) {
					defer e.RequestRender()
					if expired.Load() {
						e.ShowWarning("Prompt timed out")
						return
					}
					if !accepted || strings.TrimSpace(input) == "" {
						ctx.ResumeToken(token, false)
						return
					}
					ctx.ResumeToken(token, run(input))
				}, name)
			return pawscript.TokenResult(token)
		})
	}
	registerExecLike("exec", false)
	registerExecLike("shell", true)

	// tinput sends input to the focused viewport's terminal session — the same
	// direction the TUI host sends keystrokes into mew, and in the same
	// currency: raw terminal bytes.
	//
	// The argument takes whichever form the value already has. A PawScript
	// {bytes ...} value goes verbatim and whole, which is how a control byte or
	// an escape sequence is written without quoting games: {bytes 0x03} is
	// Ctrl-C, and Up is either {bytes 0x1b5b41} or the comma-separated list
	// {bytes 0x1b, 0x5b, 0x41}. The commas matter - without them it is symbol
	// concatenation, not a byte list. A string sends its UTF-8 bytes, so
	// tinput "ls\n" is what it looks like.
	//
	// pty_diag asks the host to test its terminal plumbing and writes the
	// report into the buffer at the caret. For when exec produces a session
	// that starts and stops with nothing to show for it.
	ps.RegisterCommand("pty_diag", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.ptyDiagnose())
	})

	// viewport_pty_hide / _show / _toggle run the focused viewport's terminal
	// under the hood or bring it back — bindable so a running session can be
	// hidden or revealed part-way through. Each warns and does nothing when the
	// focused buffer has no session.
	ps.RegisterCommand("viewport_pty_hide", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.setViewportPTYHidden(1, "viewport_pty_hide"))
	})
	ps.RegisterCommand("viewport_pty_show", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.setViewportPTYHidden(-1, "viewport_pty_show"))
	})
	ps.RegisterCommand("viewport_pty_toggle", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.setViewportPTYHidden(0, "viewport_pty_toggle"))
	})

	// viewport_pty_kill ends the focused viewport's session; a `final` capture
	// then folds everything it produced up to that point. Warns and does nothing
	// when the focused buffer has no session.
	ps.RegisterCommand("viewport_pty_kill", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.killViewportPTY())
	})

	// raw_key_input hands the NEXT keystroke to a focused terminal's child
	// instead of running mew's binding for it — the escape hatch for the keys
	// mew itself claims. See armRawKey.
	ps.RegisterCommand("raw_key_input", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.armRawKey())
	})

	// Reports FALSE when the focused buffer runs nothing, so a chain like
	// tinput|insert falls through to ordinary editing.
	ps.RegisterCommand("tinput", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) > 0 {
			if sb, ok := ctx.Args[0].(pawscript.StoredBytes); ok {
				return pawscript.BoolStatus(e.ptySendBytes(sb.Data()))
			}
		}
		text, _ := argString(ctx, 0)
		return pawscript.BoolStatus(e.ptySendBytes([]byte(text)))
	})

	// tinput_key forwards THE KEY BEING DISPATCHED to the focused viewport's
	// child process, encoded by the host's emulator — so application cursor
	// mode and its kin decide the bytes (\x1b[A vs \x1bOA for Up), not a
	// table here. This is what `(capture) * = tinput_key` in [pty::mappings]
	// runs: the terminal's first claim on every key. Reports FALSE — declining
	// the key — when there is no session, no host encoder, or the name encodes
	// to nothing, which drops resolution to the next level down. An explicit
	// key name may be given as an argument for scripted use.
	ps.RegisterCommand("tinput_key", func(ctx *pawscript.Context) pawscript.Result {
		key := e.dispatchingKey
		// Put back the repeat marker the dispatcher set aside, so the child
		// learns the key was HELD rather than struck again (see dispatchKey).
		// Only for the key that actually repeated: a sequence unwind runs
		// several keys' commands within this one dispatch, and the earlier
		// ones were separate presses.
		if key != "" && key == e.repeatingKey {
			key += ":Repeat"
		}
		if len(ctx.Args) > 0 {
			if s, ok := argString(ctx, 0); ok && s != "" {
				key = s
			}
		}
		if key == "" {
			return pawscript.BoolStatus(false)
		}
		return pawscript.BoolStatus(e.sendKeyToPTY(key))
	})

	// terminal_grid <id>, <cols>, <rows> — the host declaring how many cells
	// of text its display actually renders for a session, which is not the
	// same as the cell rectangle mew placed it in. See ptyState.gridCols.
	ps.RegisterCommand("terminal_grid", func(ctx *pawscript.Context) pawscript.Result {
		id, ok := argString(ctx, 0)
		if !ok {
			return pawscript.BoolStatus(false)
		}
		cols, okc := argInt(ctx, 1)
		rows, okr := argInt(ctx, 2)
		if !okc || !okr {
			return pawscript.BoolStatus(false)
		}
		return pawscript.BoolStatus(e.SetTerminalGrid(id, cols, rows))
	})

	ps.RegisterCommand("insert_rune", func(ctx *pawscript.Context) pawscript.Result {
		if arg, ok := argString(ctx, 0); ok {
			r, ok := parseCodePoint(arg)
			if !ok {
				e.ShowWarning("Not a Unicode code point: " + arg)
				return pawscript.BoolStatus(false)
			}
			return pawscript.BoolStatus(e.insertRuneAt(r))
		}
		expired := &atomic.Bool{}
		token := e.PawScript.RequestToken(func(string) { expired.Store(true) }, "",
			tokenTimeout(e.Config.PromptTimeout))
		var ask func()
		ask = func() {
			e.PromptMgr.PromptForInput("Insert rune by code point [U+xxxx hex, #NNN decimal, \\uXXXX]: ", "",
				func(accepted bool, _, input string) {
					defer e.RequestRender()
					if expired.Load() {
						e.ShowWarning("Prompt timed out")
						return
					}
					if !accepted || strings.TrimSpace(input) == "" {
						ctx.ResumeToken(token, false)
						return
					}
					r, ok := parseCodePoint(input)
					if !ok {
						e.ShowWarning("Not a Unicode code point: " + input)
						ask() // stay in the loop rather than eat the keystroke
						return
					}
					ctx.ResumeToken(token, e.insertRuneAt(r))
				}, "insrune")
		}
		ask()
		return pawscript.TokenResult(token)
	})

	// insert_raw_byte inserts bytes verbatim. A PawScript {bytes ...} value goes
	// in whole; otherwise a single byte 0..255, written whichever way the value is
	// already in your head - see parseByteSpec for the full set: ^[ or ESC for
	// the escape character, x1b, o33, b11011, #27. "?" in the prompt lists
	// them.
	ps.RegisterCommand("insert_raw_byte", func(ctx *pawscript.Context) pawscript.Result {
		// A {bytes ...} value is the language's own way to say this, so take it
		// first and take ALL of it: {bytes 0xDEADBEEF} inserts four bytes, not
		// one. Every other spelling below carries a single byte because it is a
		// way of NAMING one; this is a way of holding a sequence.
		if len(ctx.Args) > 0 {
			if sb, ok := ctx.Args[0].(pawscript.StoredBytes); ok {
				return pawscript.BoolStatus(e.insertRawBytesAt(sb.Data()))
			}
		}
		if arg, ok := argString(ctx, 0); ok {
			r, ok := parseByteSpec(arg)
			if !ok {
				e.ShowWarning("Not a byte value: " + arg)
				return pawscript.BoolStatus(false)
			}
			return pawscript.BoolStatus(e.insertRawByteAt(byte(r)))
		}
		expired := &atomic.Bool{}
		token := e.PawScript.RequestToken(func(string) { expired.Store(true) }, "",
			tokenTimeout(e.Config.PromptTimeout))
		var ask func()
		ask = func() {
			e.PromptMgr.PromptForInput("Insert raw byte [x1b, o33, b11011, #27, ^[, \\e, ESC, ?]: ", "",
				func(accepted bool, _, input string) {
					defer e.RequestRender()
					if expired.Load() {
						e.ShowWarning("Prompt timed out")
						return
					}
					in := strings.TrimSpace(input)
					if !accepted || in == "" {
						ctx.ResumeToken(token, false)
						return
					}
					if in == "?" {
						e.ShowNotification("x1b/1b=hex (default), o33=octal, b11011=binary, #27=decimal")
						e.ShowNotification("^[=control (^@..^_, ^?=DEL), \\n \\r \\t \\e \\0 \\xNN \\NNN, or a name: BEL BS TAB LF CR ESC DEL")
						ask() // re-prompt, same as insert_bidi_control's "?"
						return
					}
					r, ok := parseByteSpec(in)
					if !ok {
						e.ShowWarning("Not a byte value: " + in)
						ask()
						return
					}
					ctx.ResumeToken(token, e.insertRawByteAt(byte(r)))
				}, "insbyte")
		}
		ask()
		return pawscript.TokenResult(token)
	})

	// Undo/Redo (using Garland's versioning, TypeScript naming convention)
	ps.RegisterCommand("buffer_undo", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		if !w.Buffer.Undo() {
			return pawscript.BoolStatus(false)
		}
		e.syncCursorAfterUndoRedo(w)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("buffer_redo", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		if !w.Buffer.Redo() {
			return pawscript.BoolStatus(false)
		}
		e.syncCursorAfterUndoRedo(w)
		return pawscript.BoolStatus(true)
	})

	// Explicit transaction control for script authors: bracket a run of edits
	// so they collapse into ONE undo revision (named by the optional argument),
	// or roll the whole run back. These nest, and pair with the automatic undo
	// coalescing that already groups plain typing — a script wrapping a compound
	// edit (search-and-replace, reformat, multi-step macro) gets one clean undo
	// step. buffer_tx_start with no matching commit/cancel is closed at the end
	// of the enclosing command dispatch, so a stray open transaction can't leak.
	ps.RegisterCommand("buffer_tx_start", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		name := "transaction"
		if len(ctx.Args) > 0 {
			if s := fmt.Sprintf("%v", ctx.Args[0]); s != "" {
				name = s
			}
		}
		w.Buffer.BeginUserCommand(name)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("buffer_tx_commit", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		w.Buffer.EndUserCommand()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("buffer_tx_cancel", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		w.Buffer.CancelUserCommand()
		e.syncCursorAfterUndoRedo(w) // rolled-back content: resync the caret
		return pawscript.BoolStatus(true)
	})

	// Mark commands
	ps.RegisterCommand("set_mark", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		// With an explicit name, set it directly.
		if len(ctx.Args) > 0 {
			return pawscript.BoolStatus(e.setUserMark(w, fmt.Sprintf("%v", ctx.Args[0]), w.CursorPos().Line, w.CursorPos().Rune))
		}
		// No name given (e.g. "esc esc") - prompt for the mark identifier.
		// The position is captured as a garland decoration, not as absolute
		// coordinates: anything that edits the buffer while the prompt is up
		// (a second viewport, an async script) slides the pending mark along
		// with the text, so the mark lands where the caret's TEXT is, not
		// where its line number used to be.
		const pendingMark = "_pending_set_mark"
		w.Buffer.SetMark(pendingMark, w.CursorPos().Line, w.CursorPos().Rune)
		e.PromptForInput("Set mark (0-9): ", "", func(input string, accepted bool) {
			line, rune_, exists := w.Buffer.GetMark(pendingMark)
			w.Buffer.ClearMark(pendingMark)
			if accepted && exists {
				name := strings.TrimSpace(input)
				w.Buffer.BeginUserCommand("set_mark")
				e.setUserMark(w, name, line, rune_)
				w.Buffer.EndUserCommand()
			}
			e.RequestRender()
		})
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("go_mark", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		// With an explicit name, jump directly.
		if len(ctx.Args) > 0 {
			return pawscript.BoolStatus(e.gotoUserMark(w, fmt.Sprintf("%v", ctx.Args[0])))
		}
		// No name given (e.g. "esc esc") - prompt for the mark identifier, then
		// jump to it (mirroring set_mark's no-argument prompt).
		e.PromptForInput("Go to mark (0-9): ", "", func(input string, accepted bool) {
			if accepted {
				e.gotoUserMark(w, strings.TrimSpace(input))
			}
			e.RequestRender()
		})
		return pawscript.BoolStatus(true)
	})

	// Block-selection mark commands. These encapsulate the internal
	// _block_begin/_block_end marks so keybindings never name them directly
	// (keeping the "_" internal-mark namespace out of user-facing config).
	ps.RegisterCommand("set_block_begin", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.setBlockMark("_block_begin", "Block begin"))
	})
	ps.RegisterCommand("set_block_end", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.setBlockMark("_block_end", "Block end"))
	})
	ps.RegisterCommand("go_block_begin", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.goBlockMark("_block_begin", "Block begin"))
	})
	ps.RegisterCommand("go_block_end", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.goBlockMark("_block_end", "Block end"))
	})

	// File commands
	ps.RegisterCommand("buffer_save", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		filename := w.Buffer.GetFilename()
		if filename == "" {
			// No filename - prompt for one via buffer_save_as behavior (with history)
			e.PromptMgr.PromptForFilename("Save as", "", func(accepted bool, _, cursorLineText string) {
				if accepted && cursorLineText != "" {
					e.requestSave(w.Buffer, cursorLineText, nil)
				}
				e.RequestRender()
			})
			return pawscript.BoolStatus(true)
		}
		e.requestSave(w.Buffer, filename, nil)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("buffer_save_as", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		currentFilename := w.Buffer.GetFilename()
		e.PromptMgr.PromptForFilename("Save as", currentFilename, func(accepted bool, _, cursorLineText string) {
			if accepted && cursorLineText != "" {
				e.requestSave(w.Buffer, cursorLineText, nil)
			}
			e.RequestRender()
		})
		return pawscript.BoolStatus(true)
	})

	// buffer_write EXPORTS the whole buffer to a prompted-for file without
	// adopting it as the source (garland's SaveCopyTo): the buffer keeps its
	// original source, filename, modified flag, and save history. It is the
	// whole-buffer parallel to block_write — a plain "write these bytes there",
	// not a save-as. Distinct from buffer_save_as, which re-homes the buffer.
	ps.RegisterCommand("buffer_write", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.writeBufferCopy())
	})

	// buffer_save_all saves every modified buffer, once each — including buffers
	// stacked in a viewport's nav history (unsaved work parked behind a link
	// follow). With the argument "true" it is NON-INTERACTIVE (for save-and-quit,
	// `buffer_save_all true & exit`): it never prompts, skips unnamed/changed
	// buffers with a notice, and returns false if anything was skipped or failed.
	// Otherwise it is INTERACTIVE: it prompts per file (name / overwrite / create
	// directory), and ANY ^C bails the whole remaining batch with a false result.
	ps.RegisterCommand("buffer_save_all", func(ctx *pawscript.Context) pawscript.Result {
		var pending []*buffer.Buffer
		for _, b := range e.openDocViewports() {
			if b.IsModified() {
				pending = append(pending, b)
			}
		}
		if len(pending) == 0 {
			e.ShowNotification("No modified files to save")
			return pawscript.BoolStatus(true)
		}
		nonInteractive := false
		if arg, ok := argString(ctx, 0); ok && strings.EqualFold(strings.TrimSpace(arg), "true") {
			nonInteractive = true
		}
		// Both modes may prompt (the "true" mode only for never-saved buffers,
		// which now surface with a Save-as instead of being skipped), and the
		// prompts are async. If the whole batch finishes without suspending on
		// a prompt, report the result synchronously; otherwise defer it
		// through a token so a following `& exit` waits for — and respects —
		// the outcome. The prompt callbacks cannot fire until this command
		// returns to the main loop, so `token` is always set before any
		// resume runs.
		token := ""
		completedSync, syncResult := false, false
		finish := func(success bool) {
			if token == "" {
				completedSync, syncResult = true, success
			} else {
				ctx.ResumeToken(token, success)
			}
		}
		if nonInteractive {
			e.saveAllNonInteractive(pending, finish)
		} else {
			e.saveAllInteractive(pending, finish)
		}
		if completedSync {
			return pawscript.BoolStatus(syncResult)
		}
		token = e.PawScript.RequestToken(nil, "", tokenTimeout(0))
		return pawscript.TokenResult(token)
	})

	// Block commands (TypeScript uses set_mark '_block_begin' / '_block_end')
	ps.RegisterCommand("block_copy", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.copyBlock()
		e.trackEdit()
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("block_delete", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.deleteBlock()
		if ok {
			e.trackEdit() // consumes the kill flag so accumulation chains work
		}
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("block_move", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.moveBlock()
		e.trackEdit()
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("block_write", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.writeBlock())
	})

	// Kill ring (emacs-style). Deletes accumulate into kill entries as they
	// run (see killCapture); these commands read the ring back.
	ps.RegisterCommand("block_copy_kill", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.blockCopyKill())
	})

	ps.RegisterCommand("kill_ring_yank", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.killRingYank()
		if ok {
			e.trackEdit() // an insert-style edit: cursor ring + breaks kill chain
		}
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("kill_ring_pop", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.killRingPop()
		if ok {
			e.trackEdit()
		}
		return pawscript.BoolStatus(ok)
	})

	// kill_ring_append arms the next kill to accumulate into the most recent
	// kill entry even if it would otherwise start a new one (append-next-kill).
	ps.RegisterCommand("kill_ring_append", func(ctx *pawscript.Context) pawscript.Result {
		e.killAppendNext = true
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("block_indent", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.indentBlock())
	})

	ps.RegisterCommand("block_unindent", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.unindentBlock())
	})

	// block_from_file streams a prompted-for file over the marked block: it
	// replaces the block's contents with the file's, but only when a block is
	// marked AND the caret sits within it (or on either edge). The gate is
	// enforced up front (promptBlockFromFile) so the filename is never even
	// asked for on an invalid target; the replace itself (blockFromFile) is
	// wrapped like the other block mutations — one grouped undo, block left
	// marked around the streamed-in text.
	ps.RegisterCommand("block_from_file", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.promptBlockFromFile())
	})

	// block_filter pipes the marked block through a shell command and replaces it
	// with the result. The command may be given inline (block_filter sort -r);
	// with none it prompts, recalling prior filter
	// commands. It is the ergonomic spelling of exec --stdin=block --stdout=block
	// --stderr=block, so stdout and stderr both flow back into the block.
	ps.RegisterCommand("block_filter", func(ctx *pawscript.Context) pawscript.Result {
		var parts []string
		for i := 0; ; i++ {
			v, ok := argString(ctx, i)
			if !ok {
				break
			}
			parts = append(parts, v)
		}
		return pawscript.BoolStatus(e.blockFilter(strings.Join(parts, " ")))
	})

	// OS-clipboard commands: the host system-clipboard bridge (see
	// osclipboard.go). A channel deliberately separate from the kill ring.
	ps.RegisterCommand("os_copy", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.osCopy())
	})
	ps.RegisterCommand("os_cut", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.osCut())
	})
	ps.RegisterCommand("os_paste", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.osPaste())
	})
	ps.RegisterCommand("os_select_all", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.osSelectAll())
	})
	// os_exchange swaps the block with the clipboard in one gesture. It is not
	// os_copy + os_paste: after the copy the clipboard holds the block, so the
	// outgoing text must be captured before the incoming text lands.
	ps.RegisterCommand("os_exchange", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.osExchange())
	})

	ps.RegisterCommand("buffer_insert_file", func(ctx *pawscript.Context) pawscript.Result {
		e.PromptMgr.PromptForFilename("Insert file", "", func(accepted bool, _, filename string) {
			if accepted && filename != "" {
				e.insertFile(filename)
			}
			e.RequestRender()
		})
		return pawscript.BoolStatus(true)
	})

	// Multi-buffer commands
	// buffer_open_file with an argument opens THAT file directly (no prompt) —
	// the scripted/menu equivalent of typing the name at the Open prompt, e.g.
	// `buffer_open_file "help:/"`. With no argument it raises the Open prompt.
	ps.RegisterCommand("buffer_open_file", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) > 0 {
			name := strings.TrimSpace(fmt.Sprintf("%v", ctx.Args[0]))
			if name != "" {
				ok := e.openFile(name)
				e.RequestRender()
				return pawscript.BoolStatus(ok)
			}
		}
		e.PromptMgr.PromptForFilename("Open", "", func(accepted bool, _, cursorLineText string) {
			if accepted && cursorLineText != "" {
				e.openFile(cursorLineText)
			}
			e.RequestRender()
		})
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("buffer_new", func(ctx *pawscript.Context) pawscript.Result {
		e.createNewBuffer()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("buffer_duplicate", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.duplicateCurrentBuffer())
	})

	// viewport_clone opens a second viewport onto the SAME buffer (not a content
	// copy like buffer_duplicate) so you can edit in two places at once and switch
	// between them. Each viewport keeps its own caret and viewport.
	ps.RegisterCommand("viewport_clone", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.cloneCurrentViewport())
	})

	// viewport_close closes the focused viewport. A modified buffer asks
	// first, and the question SUSPENDS the calling sequence on a token rather
	// than reporting a success it has not had yet: `viewport_close &
	// viewport_close` walks the viewports one at a time, and answering no
	// stops the chain where it stood.
	ps.RegisterCommand("viewport_close", func(ctx *pawscript.Context) pawscript.Result {
		return e.promptedResult(ctx, e.closeCurrentBufferThen)
	})

	// session_close closes every viewport there is, asking about each piece of
	// unsaved work in turn, and ends the session once the last one goes. It is
	// viewport_close repeated until there is nothing left to close - and, like
	// a chain of them, it stops the moment one is declined.
	//
	// This is what a host runs when something tries to close the WINDOW a mew
	// session lives in: the window refuses the close outright, runs this, and
	// closes for real only if the session ends. So unsaved work is answered by
	// mew's own prompt, in mew's own terms, instead of being lost to a frame
	// that never asked.
	ps.RegisterCommand("session_close", func(ctx *pawscript.Context) pawscript.Result {
		return e.promptedResult(ctx, e.closeSessionThen)
	})

	// buffer_close closes the focused viewport's buffer from EVERYWHERE it is
	// referenced (viewport_close closes just the one viewport): active views
	// mirror viewport_close and nav-history references become mew:/closed
	// tombstones. Modified buffers prompt once first.
	ps.RegisterCommand("buffer_close", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.closeBufferEverywhere())
	})

	// buffer_revert seeks the buffer's history back to its last save point.
	// A pure history move: redo still reaches the abandoned edits.
	ps.RegisterCommand("buffer_revert", func(ctx *pawscript.Context) pawscript.Result {
		w := e.ViewportManager.GetFocusedViewport()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		if err := w.Buffer.RevertToLastSave(); err != nil {
			e.ShowError("Revert: " + err.Error())
			return pawscript.BoolStatus(false)
		}
		e.syncCursorAfterUndoRedo(w)
		e.noteBuffer(w.Buffer, "save", "Reverted to last save (redo restores the edits)", false)
		return pawscript.BoolStatus(true)
	})

	// screen_refresh discards the renderer's knowledge of what the terminal is
	// showing and forces a full clear-and-repaint on the next frame — recovery
	// from external corruption of the display (a stray program writing over it,
	// a garbled resize) that the incremental diff would otherwise preserve.
	ps.RegisterCommand("screen_refresh", func(ctx *pawscript.Context) pawscript.Result {
		e.Renderer.ForceRedraw()
		e.RequestRender()
		return pawscript.BoolStatus(true)
	})

	// set_font re-points a host font alias at one or more font names on a
	// graphical host (e.g. set_font "ui-term", "JetBrainsMono"): the first name
	// that resolves is used, later names are fallbacks. The host loads unknown
	// names (system scan / configured search paths) and repaints live. On a
	// plain terminal (no FontSink) it warns — fonts are the terminal's there.
	ps.RegisterCommand("set_font", func(ctx *pawscript.Context) pawscript.Result {
		alias, ok := argString(ctx, 0)
		if !ok || strings.TrimSpace(alias) == "" {
			e.ShowWarning("set_font: usage: set_font \"<alias>\", \"<font>\" [, \"<fallback>\"...]")
			return pawscript.BoolStatus(false)
		}
		var names []string
		for i := 1; ; i++ {
			n, ok := argString(ctx, i)
			if !ok {
				break
			}
			if n = strings.TrimSpace(n); n != "" {
				names = append(names, n)
			}
		}
		if len(names) == 0 {
			e.ShowWarning("set_font: no font name given")
			return pawscript.BoolStatus(false)
		}
		if e.Config.FontSink == nil {
			e.ShowWarning("set_font: not supported on this terminal")
			return pawscript.BoolStatus(false)
		}
		got := e.Config.FontSink(strings.TrimSpace(alias), names)
		e.RequestRender()
		if got {
			e.ShowNotification(fmt.Sprintf("Font %q set to %s", alias, names[0]))
		} else {
			e.ShowWarning(fmt.Sprintf("Font %q: %q not found (using fallback)", alias, names[0]))
		}
		return pawscript.BoolStatus(got)
	})

	// debug_screen arms a full-frame ANSI snapshot of the screen, written to a
	// timestamped ".ans" file in the box:/// support tree (~/.mew locally) — a
	// capture that reproduces the screen when cat'd to a terminal. The write
	// rides the NEXT render (see performRender): the command only arms the target
	// and forces a full repaint, so it never re-renders (or re-locks renderMu)
	// itself — snapshotting the exact frame that gets painted.
	ps.RegisterCommand("debug_screen", func(ctx *pawscript.Context) pawscript.Result {
		e.pendingScreenCapture = "box:///" + time.Now().Format("2006-01-02 15.04.05") + ".ans"
		e.Renderer.ForceRedraw()
		e.RequestRender()
		return pawscript.BoolStatus(true)
	})

	// deadcat forces a crash-style dump of every modified buffer to the
	// resolved DEADCAT location — mew's DEADJOE, on demand (also the path the
	// signal/panic handlers and a host's shutdown take).
	ps.RegisterCommand("deadcat", func(ctx *pawscript.Context) pawscript.Result {
		reason := "deadcat command"
		if len(ctx.Args) > 0 {
			if s, ok := argString(ctx, 0); ok && s != "" {
				reason = s
			}
		}
		path, err := e.DumpDeadcat(reason)
		if err != nil {
			e.ShowError("DEADCAT: " + err.Error())
			return pawscript.BoolStatus(false)
		}
		if path == "" {
			e.ShowNotification("DEADCAT: no modified buffers to dump")
			return pawscript.BoolStatus(false)
		}
		e.ShowNotification("DEADCAT written: " + path)
		return pawscript.BoolStatus(true)
	})

	// buffer_status re-exposes the buffer's source-safety picture: source
	// consistency, lock and backup state, and every captured notice (the
	// transients that may have timed out unseen).
	ps.RegisterCommand("buffer_status", func(ctx *pawscript.Context) pawscript.Result {
		w := e.resolveTargetMain()
		if w == nil || w.Buffer == nil {
			return pawscript.BoolStatus(false)
		}
		buf := e.lib.NewFromString(e.bufferStatusText(w.Buffer))
		e.ViewportManager.CreateViewport(viewport.ViewportOptions{
			Type:             viewport.ToolViewport,
			ViewportSet:      "help",
			Class:            "bufstatus",
			Dock:             viewport.DockTop,
			Priority:         100,
			MinHeight:        5,
			MaxHeight:        15,
			MessageTopCenter: "Buffer Status",
			Buffer:           buf,
			ShowLineNumbers:  false,
		})
		e.RequestRender()
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("buffer_next", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.cycleBuffer(1))
	})

	ps.RegisterCommand("buffer_prior", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.cycleBuffer(-1))
	})

	ps.RegisterCommand("buffer_list", func(ctx *pawscript.Context) pawscript.Result {
		// Navigate the focused document to the generated mew:/buffers surface:
		// a dynamic, read-only dokuwiki list of open buffers, in navigation mode.
		return pawscript.BoolStatus(e.openGeneratedSurface("buffers"))
	})

	ps.RegisterCommand("viewport_list", func(ctx *pawscript.Context) pawscript.Result {
		// The mew:/viewports companion to buffer_list.
		return pawscript.BoolStatus(e.openGeneratedSurface("viewports"))
	})

	// Search commands. find takes up to three optional arguments -
	// find [term], [options], [replacement] - and prompts only for what is
	// missing and necessary (see startFind). Option letters: i=ignore case,
	// b=backwards, a=all buffers, r=replace.
	ps.RegisterCommand("find", func(ctx *pawscript.Context) pawscript.Result {
		term, haveTerm := argString(ctx, 0)
		options, haveOptions := argString(ctx, 1)
		replacement, haveReplacement := argString(ctx, 2)
		e.startFind(term, options, replacement, haveTerm, haveOptions, haveReplacement)
		return pawscript.BoolStatus(true)
	})

	ps.RegisterCommand("find_next", func(ctx *pawscript.Context) pawscript.Result {
		state := e.currentFindState()
		if state.Term == "" {
			// Nothing to repeat: fall into the interactive find flow.
			e.startFind("", "", "", false, false, false)
			return pawscript.BoolStatus(true)
		}
		// find_next always steps a single occurrence; a count in the stored
		// options applies only to the invocation that gave it. The scan runs on
		// a goroutine, so this reports only that the pass STARTED — the caret
		// move and any "Not found" arrive from findPump (see findasync.go).
		return pawscript.BoolStatus(e.findStepAsync(state, 1, true))
	})

	// search - incremental search: an "I-search:" prompt whose keystrokes
	// drive the caret to matches live, JOE-style (see isearch.go). It starts
	// in the direction the find state stored; search_reverse/search_forward
	// set the direction (stepping one occurrence) inside an open search, or
	// start one that way. isearch_key is the prompt's after-key classifier;
	// confirm_key is the single-keystroke classifier armed on yes/no
	// confirmation prompts (see confirmkey.go).
	ps.RegisterCommand("search", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.startIncrementalSearch(0))
	})
	ps.RegisterCommand("search_reverse", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.isearchDirection(true))
	})
	ps.RegisterCommand("search_forward", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.isearchDirection(false))
	})
	ps.RegisterCommand("isearch_key", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.isearchKeystroke())
	})
	ps.RegisterCommand("confirm_key", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.confirmKeyStroke())
	})

	// find_replace [term], [replacement] - find with replace mode forced;
	// prompts for whichever of the two is missing.
	ps.RegisterCommand("find_replace", func(ctx *pawscript.Context) pawscript.Result {
		term, haveTerm := argString(ctx, 0)
		replacement, haveReplacement := argString(ctx, 1)
		e.startFind(term, "r", replacement, haveTerm, true, haveReplacement)
		return pawscript.BoolStatus(true)
	})

	// verbose_log appends text to the shared verbose-log viewport (class
	// "verboseLog"), creating it in the background on first use - the
	// logging counterpart of insert. Each argument becomes its own line.
	ps.RegisterCommand("verbose_log", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) == 0 {
			e.ShowWarning("Usage: verbose_log <text>")
			return pawscript.BoolStatus(false)
		}
		for i := range ctx.Args {
			if text, ok := argString(ctx, i); ok {
				e.appendVerboseLog(text)
			}
		}
		e.RequestRender()
		return pawscript.BoolStatus(true)
	})

	// Command prompt (Esc X) - allows entering PawScript commands directly
	ps.RegisterCommand("cmd", func(ctx *pawscript.Context) pawscript.Result {
		e.PromptMgr.PromptForInput("18: Command: ", "", func(accepted bool, _, cursorLineText string) {
			if accepted && cursorLineText != "" {
				// ExecuteAsync, like executeCommand: this callback runs on
				// the main loop goroutine, and a typed command that opens a
				// prompt of its own suspends on a token that only the main
				// loop can resume — the blocking Execute would deadlock.
				e.PawScript.ExecuteAsync(cursorLineText)
			}
			e.RequestRender()
		}, "command")
		return pawscript.BoolStatus(true)
	})

	// Go to line command. go_line [n] goes directly; without an argument it
	// prompts, with history reachable by arrow but never defaulted (the
	// prompt starts blank). An invalid entry warns "Invalid line number" and
	// the command resolves false — the prompt suspends the calling command
	// sequence on an async token and resumes it with the outcome, so a
	// script can chain an alternative with the | else operator. Cancelling
	// or accepting a blank entry also resolves false, without the warning.
	ps.RegisterCommand("go_line", func(ctx *pawscript.Context) pawscript.Result {
		goLine := func(input string) bool {
			n, err := strconv.Atoi(strings.TrimSpace(input))
			if err != nil || n < 1 {
				e.ShowWarning("Invalid line number")
				return false
			}
			e.gotoLine(n)
			return true
		}
		if arg, ok := argString(ctx, 0); ok {
			return pawscript.BoolStatus(goLine(arg))
		}
		// PawScript force-cleans suspension tokens after the promptTimeout
		// option (seconds, 0 = never), dropping the suspended sequence; the
		// cleanup callback records that. A prompt answered after its token
		// expired defaults to FAILURE: warn and perform nothing, rather
		// than half-succeeding (jumping) with the command's chain already
		// dead.
		expired := &atomic.Bool{}
		token := e.PawScript.RequestToken(func(string) { expired.Store(true) }, "",
			tokenTimeout(e.Config.PromptTimeout))
		e.PromptMgr.PromptForInput("Go to line: ", "", func(accepted bool, _, input string) {
			defer e.RequestRender()
			if expired.Load() {
				e.ShowWarning("Prompt timed out")
				return
			}
			if !accepted || strings.TrimSpace(input) == "" {
				ctx.ResumeToken(token, false)
				return
			}
			ctx.ResumeToken(token, goLine(input))
		}, "goline")
		return pawscript.TokenResult(token)
	})

	// repeat_next arms the next keybound command to run inside a PawScript
	// repeat(...) N times. With a count argument it arms immediately; with none
	// it prompts. The count is clamped to the maxRepeat option. The arming is
	// tracked per-viewport (like Find); the command dispatcher consumes it.
	ps.RegisterCommand("repeat_next", func(ctx *pawscript.Context) pawscript.Result {
		w := e.resolveTargetMain()
		if w == nil {
			return pawscript.BoolStatus(false)
		}
		arm := func(input string) bool {
			n, err := strconv.Atoi(strings.TrimSpace(input))
			if err != nil || n < 1 {
				e.ShowWarning("Repeat count must be a positive integer")
				return false
			}
			max := e.Config.MaxRepeat
			if max < 1 {
				max = 100
			}
			if n > max {
				n = max
			}
			w.Repeat = viewport.RepeatState{Pending: true, Count: n}
			return true
		}
		if arg, ok := argString(ctx, 0); ok {
			return pawscript.BoolStatus(arm(arg))
		}
		expired := &atomic.Bool{}
		token := e.PawScript.RequestToken(func(string) { expired.Store(true) }, "",
			tokenTimeout(e.Config.PromptTimeout))
		e.PromptMgr.PromptForInput("Repeat next command (count): ", "", func(accepted bool, _, input string) {
			defer e.RequestRender()
			if expired.Load() {
				e.ShowWarning("Prompt timed out")
				return
			}
			if !accepted || strings.TrimSpace(input) == "" {
				ctx.ResumeToken(token, false)
				return
			}
			ctx.ResumeToken(token, arm(input))
		}, "repeatnext")
		return pawscript.TokenResult(token)
	})

	// Scroll commands
	// scroll_left / scroll_right are VISUAL, not reading-relative: unlike the
	// _prior/_next commands, which follow the text's own direction, these move
	// the view the way the words name whichever way the text runs. Under
	// direction=rtl that is the opposite sign on the stored offset — see
	// scrollViewHorizontal.
	ps.RegisterCommand("scroll_left", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.scrollViewHorizontal(e.ViewportManager.GetFocusedViewport(), -1))
	})

	ps.RegisterCommand("scroll_right", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.scrollViewHorizontal(e.ViewportManager.GetFocusedViewport(), +1))
	})

	// Viewport navigation commands. These cycle only viewports currently on
	// screen (SetCycleVisibleFilter), so they always land on an already-tiled
	// pane — no adoptFocusInPlace needed. They stay WITHIN the focused viewport's
	// zone (its ViewportSet): cycling documents never jumps into the help world,
	// and vice versa — zone_next / zone_prior move between zones.
	ps.RegisterCommand("viewport_next", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.ViewportManager.FocusNextInZone()
		if ok {
			e.announceFocusedViewport()
		}
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("viewport_prior", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.ViewportManager.FocusPriorInZone()
		if ok {
			e.announceFocusedViewport()
		}
		return pawscript.BoolStatus(ok)
	})

	// zone_next / zone_prior move focus to the NEXT / PRIOR zone (world/set): they
	// take the focused main (non-prompt) viewport's ViewportSet, advance to the
	// adjacent zone among the visible zones, and land on that zone's last-focused
	// viewport (or its first visible member when the zone has no focus memory).
	ps.RegisterCommand("zone_next", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.ViewportManager.FocusNextZone()
		if ok {
			e.announceFocusedViewport()
		}
		return pawscript.BoolStatus(ok)
	})

	ps.RegisterCommand("zone_prior", func(ctx *pawscript.Context) pawscript.Result {
		ok := e.ViewportManager.FocusPrevZone()
		if ok {
			e.announceFocusedViewport()
		}
		return pawscript.BoolStatus(ok)
	})

	// Help toggle command
	// help_toggle with a page argument opens that help wiki page in the shared
	// help slot (help_toggle "keys"); with no argument it toggles the built-in
	// Quick Help. So one key can open the help index and another the Quick Help.
	ps.RegisterCommand("help_toggle", func(ctx *pawscript.Context) pawscript.Result {
		arg := ""
		if len(ctx.Args) > 0 {
			arg = fmt.Sprintf("%v", ctx.Args[0])
		}
		return pawscript.BoolStatus(e.toggleHelp(arg))
	})

	// help_open is help_toggle that only ever opens/replaces (never closes) and
	// FOCUSES the help viewport — for landing in help ready to scroll and follow
	// links, versus help_toggle's peek that leaves the caret in place.
	ps.RegisterCommand("help_open", func(ctx *pawscript.Context) pawscript.Result {
		arg := ""
		if len(ctx.Args) > 0 {
			arg = fmt.Sprintf("%v", ctx.Args[0])
		}
		return pawscript.BoolStatus(e.openHelpFocused(arg))
	})

	// Editor options command
	ps.RegisterCommand("editor_options", func(ctx *pawscript.Context) pawscript.Result {
		return pawscript.BoolStatus(e.toggleOptions())
	})

	// Render command
	ps.RegisterCommand("render", func(ctx *pawscript.Context) pawscript.Result {
		e.RequestRender()
		return pawscript.BoolStatus(true)
	})

	// Status command
	ps.RegisterCommand("status", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) > 0 {
			e.ShowNotification(fmt.Sprintf("%v", ctx.Args[0]))
			return pawscript.BoolStatus(true)
		}
		return pawscript.BoolStatus(false)
	})

	// set_option <name>, <value> - change a runtime editor option on the last
	// active editor viewport (NOTE: arguments are comma-separated).
	ps.RegisterCommand("set_option", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) < 1 {
			e.ShowWarning("Usage: set_option <name>[, <value>]")
			return pawscript.BoolStatus(false)
		}
		name := fmt.Sprintf("%v", ctx.Args[0])
		// Target the last active main-buffer viewport, not whatever is focused
		// (a prompt viewport would be focused and is about to close).
		w := e.ViewportManager.GetLastMainViewport()
		if len(ctx.Args) < 2 {
			// No value given: prompt for one, seeding the choices from the
			// registry (the same value list set_option_next rotates through).
			return pawscript.BoolStatus(e.promptSetOption(w, name))
		}
		value := fmt.Sprintf("%v", ctx.Args[1])
		return pawscript.BoolStatus(e.setOption(w, name, value))
	})

	// set_option_next / set_option_prior <name> - rotate an option through its
	// canonical value sequence (from optionSpecs): read the current value via the
	// cascade, step to the next/prior value, and set it. Fails with a warning
	// for options that have no fixed value set (integers, counts, free text).
	rotate := func(dir int) func(*pawscript.Context) pawscript.Result {
		return func(ctx *pawscript.Context) pawscript.Result {
			if len(ctx.Args) < 1 {
				e.ShowWarning("Usage: set_option_next/prior <name>")
				return pawscript.BoolStatus(false)
			}
			name := fmt.Sprintf("%v", ctx.Args[0])
			w := e.ViewportManager.GetLastMainViewport()
			return pawscript.BoolStatus(e.rotateOption(w, name, dir))
		}
	}
	ps.RegisterCommand("set_option_next", rotate(+1))
	ps.RegisterCommand("set_option_prior", rotate(-1))

	// clear_option <name> - drop a per-viewport option's explicit override on the
	// active viewport, reverting it to the resolved default (the configured /
	// inherited value). Fails for global options and unknown names.
	ps.RegisterCommand("clear_option", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) < 1 {
			e.ShowWarning("Usage: clear_option <name>")
			return pawscript.BoolStatus(false)
		}
		name := fmt.Sprintf("%v", ctx.Args[0])
		w := e.ViewportManager.GetLastMainViewport()
		return pawscript.BoolStatus(e.clearOption(w, name))
	})

	// get_option <name> - return the current effective value of an option, as a
	// substitutable result, e.g. insert {get_option "tabSize"}.
	ps.RegisterCommand("get_option", func(ctx *pawscript.Context) pawscript.Result {
		if len(ctx.Args) < 1 {
			e.ShowWarning("Usage: get_option <name>")
			return pawscript.BoolStatus(false)
		}
		name := fmt.Sprintf("%v", ctx.Args[0])
		w := e.ViewportManager.GetLastMainViewport()
		value, ok := e.getOption(w, name)
		if !ok {
			e.ShowWarning("Unknown option: " + name)
			return pawscript.BoolStatus(false)
		}
		ctx.SetResult(value)
		return pawscript.BoolStatus(true)
	})
}
