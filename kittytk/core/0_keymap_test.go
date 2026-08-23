package core

import "testing"

func demoRegistry() *KeyRegistry {
	return NewKeyRegistryFromMap("default", map[string][]string{
		"M-F4":  {"window_close"},
		"C-Up":  {"window_move_up"},
		"M-Up":  {"window_move_up"},
		"s-Up":  {"window_move_up"},
		"Tab":   {"focus_next"},
		"S-Tab": {"focus_prior"},
		"^K B":  {"block_begin"},
	})
}

// A context carries only the bindings whose command the situation offers.
// Everything else in the registry is invisible: a key bound to something this
// situation cannot do must not resolve to it, or the switch would receive a
// command name it has no case for and swallow the key.
func TestBuildContextCarriesOnlyItsCommands(t *testing.T) {
	ctx := demoRegistry().BuildContext([]string{"focus_next", "focus_prior"})

	if got := ctx.Resolve("Tab"); got != "focus_next" {
		t.Errorf("Tab -> %q, want focus_next", got)
	}
	if got := ctx.Resolve("S-Tab"); got != "focus_prior" {
		t.Errorf("S-Tab -> %q, want focus_prior", got)
	}
	if got := ctx.Resolve("M-F4"); got != "" {
		t.Errorf("M-F4 -> %q, want nothing: window_close is not on offer here", got)
	}
}

// One command may be reached by several keys, or by none. The coarse window
// move is bound to Ctrl, Meta and Super arrows alike, and a command nothing
// names simply is not reachable from the keyboard.
func TestContextHandlesManyKeysPerCommand(t *testing.T) {
	ctx := demoRegistry().BuildContext([]string{"window_move_up", "unbound_command"})
	for _, key := range []string{"C-Up", "M-Up", "s-Up"} {
		ctx.Abandon()
		if got := ctx.Resolve(key); got != "window_move_up" {
			t.Errorf("%s -> %q, want window_move_up", key, got)
		}
	}
	if keys := demoRegistry().KeysFor("unbound_command"); len(keys) != 0 {
		t.Errorf("unbound_command has keys %v, want none", keys)
	}
	if keys := demoRegistry().KeysFor("window_move_up"); len(keys) != 3 {
		t.Errorf("window_move_up has %d keys, want 3", len(keys))
	}
}

// A context is stateful because a sequence is: the first key of a chord holds
// the prefix and resolves to nothing yet.
func TestContextHoldsSequences(t *testing.T) {
	ctx := demoRegistry().BuildContext([]string{"block_begin"})
	if got := ctx.Resolve("^K"); got != "" {
		t.Errorf("^K -> %q, want nothing yet: the chord is not finished", got)
	}
	if got := ctx.Resolve("B"); got != "block_begin" {
		t.Errorf("^K B -> %q, want block_begin", got)
	}
	// Abandoning drops the prefix rather than leaving it armed.
	ctx.Resolve("^K")
	ctx.Abandon()
	if got := ctx.Resolve("B"); got == "block_begin" {
		t.Error("a bare B completed an abandoned chord")
	}
}

// Claims is what an accelerator asks before forming: a chord something else
// has taken is not the accelerator's to claim.
func TestContextClaims(t *testing.T) {
	ctx := demoRegistry().BuildContext([]string{"window_close", "focus_next"})
	if !ctx.Claims("M-F4") {
		t.Error("M-F4 is bound here and should read as claimed")
	}
	if ctx.Claims("M-h") {
		t.Error("M-h is bound to nothing here and should be free")
	}
	// A command the situation does not offer leaves its key free.
	if ctx.Claims("C-Up") {
		t.Error("C-Up resolves to a command not on offer; it should not read as claimed")
	}
}

// Formed entries — the menu accelerators, whose keys come from menu titles
// rather than from the registry — are added after the fact.
func TestContextAddFormedEntry(t *testing.T) {
	ctx := demoRegistry().BuildContext([]string{"focus_next"})
	if ctx.Claims("M-h") {
		t.Fatal("M-h should start free")
	}
	ctx.Add("M-h", "_app_accel")
	if !ctx.Claims("M-h") {
		t.Error("M-h should read as claimed once formed")
	}
	if got := ctx.Resolve("M-h"); got != "_app_accel" {
		t.Errorf("M-h -> %q, want _app_accel", got)
	}
	// The entries already there are untouched.
	ctx.Abandon()
	if got := ctx.Resolve("Tab"); got != "focus_next" {
		t.Errorf("Tab -> %q after adding an accelerator, want focus_next", got)
	}
}

// A context records the revision it was built at, so staleness is a comparison
// rather than a subscription and nothing has to remember to notify anybody.
func TestRegistryRevisionTracksEdits(t *testing.T) {
	r := demoRegistry()
	ctx := r.BuildContext([]string{"focus_next"})
	if ctx.Revision() != r.Revision() {
		t.Fatalf("fresh context revision %d, registry %d", ctx.Revision(), r.Revision())
	}
	before := r.Revision()
	r.Bind("M-q", "quit")
	if r.Revision() == before {
		t.Error("binding a key did not move the revision")
	}
	if ctx.Revision() == r.Revision() {
		t.Error("the built context should now read as stale")
	}

	// An empty command unbinds rather than binding to nothing.
	r.Bind("M-q", "")
	if keys := r.KeysFor("quit"); len(keys) != 0 {
		t.Errorf("quit still reachable by %v after unbinding", keys)
	}
}

// A scope with no registry of its own inherits the nearest ancestor's, which
// is what makes the desktop-window-trinket cascade fall out of the scope tree
// that already exists.
func TestScopeRegistryInheritance(t *testing.T) {
	desktop := NewFocusScope(nil)
	window := NewFocusScope(nil)
	trinket := NewFocusScope(nil)
	window.SetParent(desktop)
	trinket.SetParent(window)

	base := NewKeyRegistry("default", nil)
	desktop.SetRegistry(base)

	if got := trinket.Registry(); got != base {
		t.Error("a scope with no registry should inherit through the whole chain")
	}

	own := NewKeyRegistry("purfecterm-captured", nil)
	trinket.SetRegistry(own)
	if got := trinket.Registry(); got != own {
		t.Error("a scope with its own registry should use it")
	}
	if got := window.Registry(); got != base {
		t.Error("an override must not leak upward to the parent")
	}

	// Clearing it returns the scope to inheriting.
	trinket.SetRegistry(nil)
	if got := trinket.Registry(); got != base {
		t.Error("clearing an override should return the scope to inheriting")
	}
}

// The toolkit carries its own keymap, parsed from DefaultKeymapConfig, so it
// has a default registry with no configuration file present at all.
func TestDefaultRegistryHasTheToolkitKeymap(t *testing.T) {
	r := DefaultKeyRegistry()
	for key, want := range map[string]string{
		// The application/window split: M-F4 and ^Q end the app, ^F4 and
		// ^W close one window. ^F4 and C-F4 are one key spelled two ways.
		"M-F4":  "app_quit",
		"^Q":    "app_quit",
		"C-F4":  "window_close",
		"^W":    "window_close",
		"F10":   "app_menu",
		"Tab":   "focus_next",
		"S-Tab": "focus_prior",
	} {
		ctx := r.BuildContext([]string{want})
		ctx.Abandon()
		if got := ctx.Resolve(key); got != want {
			t.Errorf("%s -> %q, want %q", key, got, want)
		}
	}
	// Several keys may share a command, as the coarse window move does.
	if keys := r.KeysFor("window_move_up"); len(keys) != 3 {
		t.Errorf("window_move_up has %d keys, want 3 (Ctrl, Meta, Super)", len(keys))
	}
}

// A host's file OVERLAYS the toolkit keymap rather than replacing it, so a
// user names only what they want changed and everything else stays.
func TestApplyHostKeymapOverlays(t *testing.T) {
	defer resetDefaultKeyRegistry()
	r := DefaultKeyRegistry()
	before := len(r.KeysFor("window_move_up"))

	ApplyHostKeymap([]Binding{
		{"M-F4", []string{"quit_everything"}}, // another meaning
		{"M-z", []string{"undo"}},             // a key the table never named
		{"S-Tab", []string{""}},               // unbind
	}, "")

	ctx := r.BuildContext([]string{"quit_everything", "undo", "focus_prior", "app_menu"})
	for key, want := range map[string]string{"M-F4": "quit_everything", "M-z": "undo"} {
		ctx.Abandon()
		if got := ctx.Resolve(key); got != want {
			t.Errorf("%s -> %q, want %q", key, got, want)
		}
	}
	ctx.Abandon()
	if got := ctx.Resolve("S-Tab"); got != "" {
		t.Errorf("S-Tab -> %q after unbinding, want nothing", got)
	}
	// Untouched entries survive: the file said what it changed, not everything.
	ctx.Abandon()
	if got := ctx.Resolve("F10"); got != "app_menu" {
		t.Errorf("F10 -> %q, want app_menu: an overlay must not drop what it did not name", got)
	}
	if after := len(r.KeysFor("window_move_up")); after != before {
		t.Errorf("window_move_up keys went %d -> %d across an unrelated overlay", before, after)
	}

}

// Every line of a file ADDS a meaning to its key; only a blank command takes
// anything away. So a file that wants to say a key over from scratch blanks it
// first, and a file that just names a key gains a meaning rather than losing
// the ones the key already had.
func TestApplyHostKeymapAddsAndOnlyBlankRemoves(t *testing.T) {
	defer resetDefaultKeyRegistry()
	r := DefaultKeyRegistry()

	// No blank line: Space keeps everything it meant and gains one more, at
	// the end, so the meanings it already had still win where both are offered.
	ApplyHostKeymap(ParseKeymap("Space = trinket_open"), "")
	ctx := r.BuildContext([]string{CmdTrinketTypeSpace, CmdTrinketOpen})
	if got := ctx.Resolve("Space"); got != CmdTrinketTypeSpace {
		t.Errorf("Space -> %q, want %s: an added meaning goes behind the ones already there", got, CmdTrinketTypeSpace)
	}
	ctx = r.BuildContext([]string{CmdTrinketOpen})
	if got := ctx.Resolve("Space"); got != CmdTrinketOpen {
		t.Errorf("Space -> %q for a context offering only open, want %s", got, CmdTrinketOpen)
	}

	// A blank command takes away everything the key meant, which is how the
	// same file then says it over.
	ApplyHostKeymap(ParseKeymap(`
Space =
Space = trinket_open
`), "")
	ctx = r.BuildContext([]string{CmdTrinketTypeSpace, CmdTrinketActivate, CmdTrinketOpen})
	if got := ctx.Resolve("Space"); got != CmdTrinketOpen {
		t.Errorf("Space -> %q after being blanked and said over, want %s", got, CmdTrinketOpen)
	}

}

// A file may restate the WHOLE default and land exactly back on it: naming a
// meaning a key already has changes nothing. That property is what lets
// DefaultKeymapConfig be both the toolkit's default and a legal thing for a
// user to write.
func TestApplyingTheDefaultKeymapChangesNothing(t *testing.T) {
	defer resetDefaultKeyRegistry()
	r := DefaultKeyRegistry()
	before := dumpCommands(r)
	ApplyHostKeymap(ParseKeymap(DefaultKeymapConfig), "")
	if after := dumpCommands(r); after != before {
		t.Errorf("re-applying the default keymap changed the table:\n before %s\n after  %s", before, after)
	}
}

// A blank chord leaves the default in place; turning chord accelerators off is
// done with a pattern that has no star in it.
func TestApplyHostKeymapChord(t *testing.T) {
	if got := AcceleratorChord(); got != DefaultAcceleratorChord {
		t.Fatalf("chord starts at %q, want %q", got, DefaultAcceleratorChord)
	}
	ApplyHostKeymap(nil, "")
	if got := AcceleratorChord(); got != DefaultAcceleratorChord {
		t.Errorf("a blank chord changed it to %q", got)
	}
	ApplyHostKeymap(nil, "^X * Return")
	if got := AcceleratorChord(); got != "^X * Return" {
		t.Errorf("chord = %q, want the configured sequence", got)
	}
	ApplyHostKeymap(nil, DefaultAcceleratorChord)
}

// A context is keyed on UI STATE, not on which trinket has focus — the states
// that matter are not all trinkets. A title bar is a mode of the window.
//
// The sixteen window move and size bindings exist ONLY with the title bar
// focused, which is exactly why a context has to be per state: an ordinary
// desktop must leave the arrows alone rather than swallowing them.
func TestStateContextGatesTheArrowsOnTitleFocus(t *testing.T) {
	r := DefaultKeyRegistry()

	normal := r.BuildStateContext(StateNormal)
	for _, key := range []string{"Up", "Down", "S-Left", "C-Up", "M-S-Right"} {
		normal.Abandon()
		if got := normal.Resolve(key); got != "" {
			t.Errorf("ordinary state resolved %s to %q; the arrows are not its to eat", key, got)
		}
		if normal.Claims(key) {
			t.Errorf("ordinary state claims %s", key)
		}
	}

	title := r.BuildStateContext(StateTitleBarFocused)
	for key, want := range map[string]string{
		"Up":        "window_move_fine_up",
		"S-Left":    "window_size_fine_left",
		"C-Up":      "window_move_up",
		"M-S-Right": "window_size_right",
		"Esc":       "window_cancel_resize",
	} {
		title.Abandon()
		if got := title.Resolve(key); got != want {
			t.Errorf("title-focused: %s -> %q, want %q", key, got, want)
		}
	}
}

// States compound: a focused title bar still quits on M-F4, still closes the
// window on ^W, and still reaches the menu on F10.
func TestStatesCompound(t *testing.T) {
	title := DefaultKeyRegistry().BuildStateContext(StateTitleBarFocused)
	for key, want := range map[string]string{
		"M-F4": "app_quit",
		"^W":   "window_close",
		"F10":  "app_menu",
		"Tab":  "focus_next",
	} {
		title.Abandon()
		if got := title.Resolve(key); got != want {
			t.Errorf("title-focused: %s -> %q, want %q", key, got, want)
		}
	}
	// ...and the ordinary state does NOT gain the title bar's additions.
	normal := CommandsForState(StateNormal)
	for _, c := range normal {
		if c == "window_move_up" {
			t.Error("the ordinary state should not offer window_move_up")
		}
	}
	if len(CommandsForState(StateTitleBarFocused)) <= len(normal) {
		t.Error("the title bar state should add to the ordinary one, not replace it")
	}
}

// A command that carries no identity needs the KEY to say which one fired, and
// for a chord that means the whole chord rather than its last keystroke. The
// pending prefix is read off the processor before the key is fed to it, so the
// sequence is reassembled at the moment it completes.
func TestMatchedSequenceReportsTheWholeChord(t *testing.T) {
	r := NewKeyRegistryFromMap("t", map[string][]string{
		"M-h":         {CommandAppAccelerator},
		"^X h Return": {CommandAppAccelerator},
		"^X p Return": {CommandAppAccelerator},
		"Tab":         {"focus_next"},
	})
	ctx := r.BuildContext([]string{CommandAppAccelerator, "focus_next"})

	// Single key: the sequence is the key.
	if got := ctx.Resolve("M-h"); got != CommandAppAccelerator {
		t.Fatalf("M-h -> %q", got)
	}
	if got := ctx.MatchedSequence(); got != "M-h" {
		t.Errorf("matched %q, want M-h", got)
	}

	// A chord: nothing matches until the last key, and then the WHOLE chord
	// comes back — which is what tells the menu bar it was p and not h.
	for _, k := range []string{"^X", "p"} {
		if got := ctx.Resolve(k); got != "" {
			t.Fatalf("%q resolved early to %q", k, got)
		}
	}
	if got := ctx.Resolve("Return"); got != CommandAppAccelerator {
		t.Fatalf("^X p Return -> %q", got)
	}
	if got := ctx.MatchedSequence(); got != "^X p Return" {
		t.Errorf("matched %q, want the whole chord ^X p Return", got)
	}

	// A failed or incomplete match leaves the last successful one alone rather
	// than reporting a half-typed chord.
	ctx.Resolve("^X")
	ctx.Abandon()
	if got := ctx.MatchedSequence(); got != "^X p Return" {
		t.Errorf("an abandoned chord changed the matched sequence to %q", got)
	}
}

// One key means different things in different situations, and the registry is
// not the place that decides which. "Up" nudges a window while its title bar
// is focused and steps a list otherwise; both are true at once, and a context
// keeps whichever meaning it offers.
//
// Without this a flat table would have to pick one, and every arrow key would
// belong permanently to whichever situation was written down first.
func TestOneKeyCanMeanSeveralThings(t *testing.T) {
	r := DefaultKeyRegistry()

	title := r.BuildStateContext(StateTitleBarFocused)
	if got := title.Resolve("Up"); got != CmdWindowMoveFineUp {
		t.Errorf("title-focused: Up -> %q, want %q", got, CmdWindowMoveFineUp)
	}

	// A list offers the item movement and nothing about windows, so the same
	// key resolves to the other meaning.
	list := r.BuildContext([]string{CmdTrinketItemPrior, CmdTrinketItemNext})
	if got := list.Resolve("Up"); got != CmdTrinketItemPrior {
		t.Errorf("a list: Up -> %q, want %q", got, CmdTrinketItemPrior)
	}
	list.Abandon()
	if got := list.Resolve("Down"); got != CmdTrinketItemNext {
		t.Errorf("a list: Down -> %q, want %q", got, CmdTrinketItemNext)
	}

	// And a situation offering neither leaves the key entirely alone.
	none := r.BuildContext([]string{CmdWindowClose})
	if got := none.Resolve("Up"); got != "" {
		t.Errorf("Up -> %q where neither meaning is offered, want nothing", got)
	}
}

// C-Tab and M-Tab are NOT the same command: one cycles MDI children inside a
// window, the other cycles top-level windows. They were conflated.
func TestMDICyclingIsNotWindowCycling(t *testing.T) {
	ctx := DefaultKeyRegistry().BuildStateContext(StateNormal)
	for key, want := range map[string]string{
		"C-Tab":   CmdWindowMDINext,
		"C-S-Tab": CmdWindowMDIPrior,
		"M-Tab":   CmdWindowNext,
		"M-S-Tab": CmdWindowPrior,
	} {
		ctx.Abandon()
		if got := ctx.Resolve(key); got != want {
			t.Errorf("%s -> %q, want %q", key, got, want)
		}
	}
}

// resetDefaultKeyRegistry drops the process-wide default so the next caller
// rebuilds it from DefaultKeymapConfig.
//
// A test that overlays a keymap onto the shared registry uses this rather than
// trying to undo its own edits by hand. Undoing them by hand cannot work: a
// key bound again takes a NEW serial, and the serial is what decides which of
// a command's keys gets advertised, so the table comes back with the right
// commands and the wrong answer to "what key does the menu show for this?".
func resetDefaultKeyRegistry() {
	keymapMu.Lock()
	defaultRegistry = nil
	keymapMu.Unlock()
}
