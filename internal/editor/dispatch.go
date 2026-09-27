package editor

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/phroun/pawscript"

	"github.com/phroun/key-sequence-processor/keyseq"
	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/config"
	"github.com/phroun/mew/internal/input"
	"github.com/phroun/mew/internal/viewport"
)

// applyMacOptionKeys pushes the macOptionKeys option into the input decoder
// and the key processor's reverse-insert fallback.
func (e *Editor) applyMacOptionKeys() {
	fw := e.ViewportManager.GetFocusedViewport()
	mode := strings.ToLower(e.optStr(fw, "macoptionkeys", e.Config.MacOptionKeys))
	if mode == "" {
		mode = "auto"
	}
	decode := mode == "true" || (mode == "auto" && runtime.GOOS == "darwin")
	insert := mode != "false"
	if kh, ok := e.KeyHandler.(*input.KeyboardHandler); ok {
		kh.SetDecodeMacOSOption(decode)
	}
	e.KeyProcessor.SetMacOptionInsert(insert)

	// What the HOST watched its own keyboard type outranks everything the
	// processor would otherwise derive, and is installed whatever the switch
	// says: the switch governs the TABLE, which is a guess about a keyboard
	// nobody has looked at, and is no reason to withhold what was seen.
	e.KeyProcessor.SetKeyChordText(e.Config.KeyChordText)
}

// KeyForCommand returns the key sequence bound to command, in the spelling a
// user would press — (capture)/(override) level words stripped, since the
// physical keys are the same at every level, and wildcard bindings skipped,
// since "*" is not a key anyone can be told to press. When several keys map
// to it the lexicographically-first is returned (stable); "" when the command
// is unbound.
func (e *Editor) KeyForCommand(command string) string {
	best := ""
	for raw, cmd := range e.KeyProcessor.GetAllMappings() {
		if cmd != command {
			continue
		}
		key, ok := keyseq.DisplayKey(raw)
		if !ok {
			continue
		}
		if best == "" || key < best {
			best = key
		}
	}
	return best
}

// executeCommand executes a command string via PawScript.
// Errors are captured via the custom stderr writer and shown as transient
// error viewports.
//
// Undo grouping is NOT a blanket per-command transaction. Plain typing and
// character deletes run as bare mutations that garland coalesces into one
// revision (a typing streak = one undo step); compound commands open their own
// transaction in their handler (and scripts can with buffer_tx_*). After each
// dispatch this closes any transaction a script left open and, unless the
// command was one of the coalescible edits, bakes the undo run — so a cursor
// move or any other command ends the streak and the next edit starts fresh.
func (e *Editor) executeCommand(command string) {
	e.executeCommandResult(command)
}

// runBoundCommand is the key processor's executor: it runs one bound command
// on behalf of a key event and reports whether that key was HANDLED. Only a
// clean false status says "not mine" — a binding's way of declining the key
// (tinput_key with no session under it, an explicit `capture X = false`
// reclaim) — which drops resolution to the next capture level down. Async
// suspensions, errors, and every other result hold the key: a command that
// merely went wrong must not hand its keystroke to a terminal below it.
//
// dispatchingKey is kept as a save/restore stack because a sequence unwind
// runs several keys' commands within one dispatch, each needing its own
// notion of "the key" for tinput_key to encode.
func (e *Editor) runBoundCommand(key, command string) bool {
	previous := e.dispatchingKey
	e.dispatchingKey = key
	defer func() { e.dispatchingKey = previous }()
	res := e.executeCommandResult(command)
	if b, ok := res.(pawscript.BoolStatus); ok && !bool(b) {
		return false
	}
	return true
}

// dispatchKey routes one keyboard key through the sequence processor, which
// resolves and RUNS its bindings (see keyseq.Processor: capture levels
// over the base set, wildcards under specifics, tried in precedence order
// until one takes the key), then refreshes the sequence/QuickHelp/modebar
// displays. Runs under renderMu (the serve loop's key branch, or a test
// standing in for it).
func (e *Editor) dispatchKey(key string) {
	// A key coming back UP belongs to the child, or to nobody.
	//
	// It is not a keystroke in mew's sense: no binding is written against one,
	// and the sequence processor must never see one, because it would count as
	// the next key of a multi-key sequence — pressing ^X and letting go of it
	// would end the sequence ^X was starting. So it is offered to the focused
	// viewport's child and otherwise dropped here, before any of that.
	//
	// The child is why these are asked for at all (see input.keyEventReporting).
	// A program that negotiated event reporting for itself — a browser, which
	// cannot know a held key was let go without it — receives presses only if
	// mew keeps its releases, and that is the whole of the bug this closes.
	if strings.HasSuffix(key, ":Release") {
		if e.sendKeyToPTY(key) {
			e.RequestRender()
		}
		return
	}

	// A modifier pressed BY ITSELF is not a keystroke in mew's sense either.
	//
	// The key layer reports these as their own events under the kitty protocol
	// — "LMod:S" is the left Shift going down — so that something watching the
	// keyboard can see which cap it was. mew is not that something: no binding
	// is written against one, and the sequence processor must never see one,
	// because it would count as the next key of a multi-key sequence. Holding
	// Shift in the middle of ^K X would end the chord ^K started.
	if isBareModifierKey(key) {
		return
	}

	// Raw key input: this one keystroke was claimed for the child process
	// running in the focused viewport, so mew's keymap does not see it at all.
	// The arm is spent either way — a raw key with no terminal under it is
	// simply an ordinary key, not a key held in reserve.
	//
	// The name goes as it arrived, repeat marker and all: this key is the
	// child's, and the child is the one thing here that can read the marker.
	if e.rawKeyArmed {
		e.rawKeyArmed = false
		if e.rawKeyToPTY(key) {
			e.RequestRender()
			return
		}
	}

	// A REPEAT is a press to mew and a repeat to the child.
	//
	// Both are true at once and both matter. mew's keymap has no repeat token,
	// so a marked name would match no binding and a held arrow key would move
	// the cursor once and then stop — the marker has to come off before the key
	// processor sees it. But a browser in a terminal pane reports a repeat as a
	// keydown with repeat set, and cannot tell a held key from a drummed one
	// without it — so tinput_key puts the marker back on the way out, and only
	// for this key. Everything between behaves exactly as it does for a press,
	// including a capture level a user has reclaimed the key from: that
	// resolves to mew's own command, and goes on repeating.
	if base, repeated := strings.CutSuffix(key, ":Repeat"); repeated {
		key = base
		e.repeatingKey = base
		defer func() { e.repeatingKey = "" }()
	}

	// Arm the after-key pseudo-binding on the viewport that owns this key —
	// the first command that completes during this dispatch fires it (see
	// executeCommandResult): the bound command when a binding matches, or the
	// first key of an unwinding non-matching sequence landing as a normal
	// press. A key that resolves nothing (a silent prefix step, an unmapped
	// named key) leaves it to be disarmed below, unfired.
	if w := e.ViewportManager.GetFocusedViewport(); w != nil && w.AfterKey != "" {
		e.afterKeyArmed = w
	}

	// The processor owns execution: a key can resolve to a stack of bindings
	// across capture levels, run highest-first until one takes it, and a
	// mid-sequence unwind runs several keys' worth — so there is no longer
	// exactly one command per keystroke to hand back. Each one comes through
	// runBoundCommand, so command semantics (afterKey, undo baking,
	// repeat_next) are unchanged.
	e.KeyProcessor.ProcessKey(key)
	e.afterKeyArmed = nil

	// Update active sequence display with possible completions
	e.ActiveSequence = e.KeyProcessor.GetActiveSequence()
	if e.ActiveSequence != "" {
		// Show possible completions for the current sequence
		completions := e.KeyProcessor.GetPossibleCompletions()
		if len(completions) > 0 {
			e.activeCompletions = strings.Join(completions, " ")
		} else {
			e.activeCompletions = ""
		}
	} else {
		e.activeCompletions = ""
	}

	// Context-sensitive Quick Help: recompute the topic for the current
	// key prefix and, if Quick Help is open and following, re-render it
	// in place. This ONLY touches Quick Help — the main help is never
	// steered by the topic. Skipped entirely when nothing consults it.
	e.updateQuickHelp()

	e.updateModebar()
}

// executeCommandResult is executeCommand returning the PawScript result, for
// the one caller that must know whether the command suspended on an async
// token (the launch-eval runner, which waits for the token before starting
// the next script).
func (e *Editor) executeCommandResult(command string) pawscript.Result {
	if command == "" {
		return nil
	}

	// Before running any command, expire transient notification/error/warning
	// viewports that have been on screen longer than 5 seconds.
	e.expireStaleNotifications()

	// Content mutations (read-only viewports, focused link buttons) are NOT gated
	// here by command name: the script language can rename or chain any command,
	// so a name is not a reliable signal of what a command does. The lock is
	// enforced inside each mutation's implementation instead — the one place
	// that always sees the actual buffer change (see contentLocked).
	fw := e.ViewportManager.GetFocusedViewport()

	// Reset the per-command behavior signals; the mutation implementations set
	// them as they run, and the post-dispatch logic below reads them instead of
	// re-classifying the command by name.
	e.editCoalesced = false
	e.yankedThisCommand = false

	// If a repeat_next is armed, wrap this command so it runs N times.
	command = e.applyRepeatNext(command)

	var buf *buffer.Buffer
	if fw != nil {
		buf = fw.Buffer
	}

	// ExecuteAsync, not Execute: a command that opens a prompt suspends its
	// command sequence on an async token, resumed by the prompt callback on
	// this same goroutine. The blocking Execute would deadlock the main loop
	// waiting for input that can never arrive. A returned TokenResult simply
	// means "still running"; the prompt callback finishes the sequence later.
	res := e.PawScript.ExecuteAsync(command)

	// Undo grouping is no longer a blanket per-command transaction. Plain typing
	// and character deletes flow as bare edits that garland coalesces into a
	// single revision; compound commands (and scripts, via buffer_tx_*) open
	// their own transaction in their handler. Close any transaction a script
	// left dangling, then — unless this command performed a coalescible edit
	// (the edit implementation set editCoalesced) — bake the run so the next
	// edit starts a fresh undo step. That bake is what makes a cursor move (or
	// any other command) end the current typing/deleting run.
	if buf != nil {
		buf.CloseUserCommand()
		if !e.editCoalesced {
			buf.BakeUndo()
		}
	}

	// kill_ring_pop may only replace text yanked by the immediately preceding
	// command: any command that was not itself a yank/pop invalidates the
	// recorded yank (the yank/pop implementations set yankedThisCommand).
	if !e.yankedThisCommand {
		e.lastYank.valid = false
	}
	e.RequestRender()

	// After-key pseudo-binding: this command's completion resolves the key
	// event that armed it (see dispatchKey). Disarm BEFORE running the script
	// so the script — an ordinary command, not a key — cannot re-trigger, and
	// later commands of the same dispatch (the rest of a sequence unwind)
	// don't fire it again. Skipped if the binding closed the owning viewport.
	if w := e.afterKeyArmed; w != nil {
		e.afterKeyArmed = nil
		if w.AfterKey != "" && e.ViewportManager.GetViewport(w.ID) == w {
			e.executeCommand(w.AfterKey)
		}
	}
	return res
}

// applyRepeatNext consumes a pending repeat_next arm on the target viewport,
// wrapping command in a PawScript repeat(...) so it runs Count times.
//
// A repeat_next invocation must not wrap itself (pressing it while already
// armed would otherwise repeat the arming command). This is the one decision
// that cannot be moved into the implementation: the wrap happens BEFORE the
// command runs, so it cannot observe what the command did — it can only look at
// the command about to run. It therefore still inspects the leading token, and
// so is the lone place a renamed/aliased command name would slip past. Left as
// a known limitation rather than papered over; a proper fix needs repeat-arm
// metadata carried on the command registration.
func (e *Editor) applyRepeatNext(command string) string {
	if commandKind(command) == "repeat_next" {
		return command
	}
	w := e.resolveTargetMain()
	if w == nil || !w.Repeat.Pending {
		return command
	}
	n := w.Repeat.Count
	w.Repeat = viewport.RepeatState{} // one-shot: consume the arm
	if n < 1 {
		return command
	}
	return fmt.Sprintf("repeat (%s), %d", command, n)
}

// commandKind returns the leading token of a command string. Its only remaining
// use is applyRepeatNext's pre-dispatch self-check (see there); behavioral
// decisions that CAN see what a command did read per-command signals instead.
func commandKind(command string) string {
	command = strings.TrimSpace(command)
	if i := strings.IndexAny(command, " \t"); i >= 0 {
		return command[:i]
	}
	return command
}

// setupKeyMappingsFromConfig sets up key mappings from the loaded config file.
// All mappings come from the config file (default or user-customized).
// This matches the TypeScript version's architecture where mappings are
// defined in the [mappings:mew] section of the config file.
func (e *Editor) setupKeyMappingsFromConfig() {
	kp := e.KeyProcessor

	// Load all key mappings from config file
	// The config file's generateDefaultConfig() contains all the standard mew mappings
	for key, command := range e.LoadedConfig.Mappings {
		kp.MapKey(key, command)
	}

	// Mirror provenance for the active keymap so key badges can attribute
	// bindings and tie-break on "last configured". A pristine (config-less)
	// keymap leaves this empty — every key resolves as a built-in.
	e.mappingOrigins = make(map[string]config.MappingOrigin, len(e.LoadedConfig.MappingOrigins))
	for key, o := range e.LoadedConfig.MappingOrigins {
		e.mappingOrigins[key] = o
	}
	e.remapPrec = maxPrecedence(e.mappingOrigins)
}

// maxPrecedence returns the highest precedence in an origins map (0 when empty),
// the baseline a runtime remap counts up from so it outranks every config
// binding.
func maxPrecedence(origins map[string]config.MappingOrigin) int {
	max := 0
	for _, o := range origins {
		if o.Precedence > max {
			max = o.Precedence
		}
	}
	return max
}

// registerKeymapCommands registers the commands that inspect, change or drive the keymap.
func (e *Editor) registerKeymapCommands(ps *pawscript.PawScript) {
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

	// nop does nothing, successfully. Bind a key to it to deliberately
	// disable the key (unbinding instead restores the key's default
	// handling, e.g. self-insert).
	ps.RegisterCommand("nop", func(ctx *pawscript.Context) pawscript.Result {
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
}
