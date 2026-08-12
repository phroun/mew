package core

import "testing"

func demoRegistry() *KeyRegistry {
	return NewKeyRegistry("default", map[string]string{
		"M-F4":  "window_close",
		"C-Up":  "window_move_up",
		"M-Up":  "window_move_up",
		"s-Up":  "window_move_up",
		"Tab":   "focus_next",
		"S-Tab": "focus_prior",
		"^K B":  "block_begin",
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

// The toolkit carries its own keymap in Go, so it has a default registry with
// no configuration file present at all.
func TestDefaultRegistryHasTheToolkitKeymap(t *testing.T) {
	r := DefaultKeyRegistry()
	for key, want := range map[string]string{
		"M-F4":  "window_close",
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
// user names only what they want changed. An empty command unbinds.
func TestApplyHostKeymapOverlays(t *testing.T) {
	r := DefaultKeyRegistry()
	before := len(r.KeysFor("window_move_up"))

	ApplyHostKeymap(map[string]string{
		"M-F4":  "quit_everything", // rebind
		"M-z":   "undo",            // add
		"S-Tab": "",                // unbind
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

	// Restore, so the shared default registry does not leak into other tests.
	ApplyHostKeymap(map[string]string{
		"M-F4": "window_close", "M-z": "", "S-Tab": "focus_prior",
	}, "")
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
	ApplyHostKeymap(nil, "^X * Enter")
	if got := AcceleratorChord(); got != "^X * Enter" {
		t.Errorf("chord = %q, want the configured sequence", got)
	}
	ApplyHostKeymap(nil, DefaultAcceleratorChord)
}
