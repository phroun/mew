package core

import "testing"

// Every binding carries where it came in the order, so "several keys mean
// this, which one do I show?" has an answer: the newest. A host that declares
// its own spelling outranks the table it overlays, without unbinding anything.
func TestNewestBindingIsTheOneShown(t *testing.T) {
	r := NewKeyRegistry("test", []Binding{
		{"M-F4", []string{"app_quit"}},
		{"^Q", []string{"app_quit"}},
	})

	first := r.KeyForCommand("app_quit")
	if first == "" {
		t.Fatal("a bound command should name a key")
	}

	r.AddBinding("s-q", "app_quit") // a macOS host, declaring Command-Q

	if got := r.KeyForCommand("app_quit"); got != "s-q" {
		t.Errorf("KeyForCommand = %q, want the newly declared s-q", got)
	}
	if keys := r.KeysFor("app_quit"); len(keys) != 3 || keys[0] != "s-q" {
		t.Errorf("KeysFor = %v, want three keys with the newest leading", keys)
	}
}

// A keymap is a LIST, and the order it is written in is part of what it says:
// among several keys that mean one command, the last one written is the one
// advertised for it. That is the whole reason it is not a map.
func TestDeclarationOrderDecidesWhatIsShown(t *testing.T) {
	table := []Binding{
		{"C-F4", []string{"window_close"}},
		{"^F4", []string{"window_close"}},
		{"^W", []string{"window_close"}},
	}
	for i := 0; i < 25; i++ { // nothing here may depend on map iteration
		if got := NewKeyRegistry("test", table).KeyForCommand("window_close"); got != "^W" {
			t.Fatalf("build %d shows %q, want the last-written ^W", i, got)
		}
	}

	// Written the other way round, the other one is advertised. Nothing about
	// what the keys DO has changed - both still close the window.
	reversed := []Binding{
		{"^W", []string{"window_close"}},
		{"C-F4", []string{"window_close"}},
	}
	r := NewKeyRegistry("test", reversed)
	if got := r.KeyForCommand("window_close"); got != "C-F4" {
		t.Errorf("KeyForCommand = %q, want C-F4 - it is written last here", got)
	}
	if keys := r.KeysFor("window_close"); len(keys) != 2 {
		t.Errorf("KeysFor = %v, want both keys still bound", keys)
	}
}

// A table built from an unordered map has no order to honor, so the registry
// imposes one rather than letting what a menu advertises change between runs.
func TestMapBuiltRegistryIsStableAcrossBuilds(t *testing.T) {
	table := map[string][]string{
		"^W":   {"window_close"},
		"^F4":  {"window_close"},
		"C-F4": {"window_close"},
	}
	want := NewKeyRegistryFromMap("test", table).KeyForCommand("window_close")
	if want == "" {
		t.Fatal("nothing was bound")
	}
	for i := 0; i < 25; i++ { // map iteration order varies; the answer must not
		if got := NewKeyRegistryFromMap("test", table).KeyForCommand("window_close"); got != want {
			t.Fatalf("build %d gave %q, want %q every time", i, got, want)
		}
	}
}

// Bind REPLACES, and a replacement is a new binding: it takes the newest
// serial, so rebinding a key is also declaring how it should be advertised.
func TestBindTakesTheNewestSerial(t *testing.T) {
	r := NewKeyRegistry("test", []Binding{{"^Q", []string{"app_quit"}}})
	r.AddBinding("s-q", "app_quit")
	if got := r.KeyForCommand("app_quit"); got != "s-q" {
		t.Fatalf("KeyForCommand = %q, want s-q", got)
	}

	r.Bind("^Q", "app_quit") // rebound later: now it is the newest

	if got := r.KeyForCommand("app_quit"); got != "^Q" {
		t.Errorf("KeyForCommand = %q, want the rebound ^Q", got)
	}
}

// AddBinding leaves an existing pair's place in the order alone - saying a key
// ALSO does something is not saying it should be advertised. Prefer is how a
// caller says the second thing.
func TestPreferPromotesWithoutRebinding(t *testing.T) {
	r := NewKeyRegistry("test", []Binding{
		{"s-x", []string{"trinket_cut"}},
		{"^X", []string{"trinket_cut"}}, // written last, so shown
	})

	r.AddBinding("s-x", "trinket_cut") // already bound: nothing moves
	if got := r.KeyForCommand("trinket_cut"); got != "^X" {
		t.Errorf("AddBinding moved the order: %q, want ^X", got)
	}

	r.Prefer("s-x", "trinket_cut") // a macOS host: "advertise THIS one"
	if got := r.KeyForCommand("trinket_cut"); got != "s-x" {
		t.Errorf("Prefer did not promote: %q, want s-x", got)
	}
	if keys := r.KeysFor("trinket_cut"); len(keys) != 2 {
		t.Errorf("Prefer bound an extra key: %v", keys)
	}
}

// A context answers the same question narrowed to what the situation OFFERS: a
// menu never advertises a key that would do nothing here.
func TestContextShowsOnlyWhatItOffers(t *testing.T) {
	r := NewKeyRegistry("test", []Binding{
		{"^W", []string{"window_close"}},
		{"M-F4", []string{"app_quit"}},
	})

	ctx := r.BuildContext([]string{"window_close"})

	if got := ctx.KeyForCommand("window_close"); got != "^W" {
		t.Errorf("KeyForCommand = %q, want ^W", got)
	}
	if got := ctx.KeyForCommand("app_quit"); got != "" {
		t.Errorf("KeyForCommand = %q, want nothing: this situation cannot quit", got)
	}
}

// A key whose meaning HERE is some other command is not a candidate either.
// "Up" nudges a window with a title bar focused and steps a list otherwise;
// only one of those is on offer at a time, and only that one may be shown.
func TestContextIgnoresAKeysOtherMeanings(t *testing.T) {
	r := NewKeyRegistry("test", []Binding{
		{"C-Up", []string{"trinket_item_up"}},
		{"Up", []string{"window_move_fine_up", "trinket_item_up"}},
		{"S-Tab", []string{"focus_prior"}},
	})

	// A title bar: Up means moving the window, so the list command has to fall
	// back to the key that only ever meant that.
	ctx := r.BuildContext([]string{"window_move_fine_up", "trinket_item_up"})
	if got := ctx.KeyForCommand("window_move_fine_up"); got != "Up" {
		t.Errorf("window move shows %q, want Up", got)
	}
	if got := ctx.KeyForCommand("trinket_item_up"); got != "C-Up" {
		t.Errorf("item up shows %q, want C-Up - Up means the window move here", got)
	}

	// A plain list: Up is the item movement's own key now, and the newer of
	// the two candidates wins.
	list := r.BuildContext([]string{"trinket_item_up"})
	if got := list.KeyForCommand("trinket_item_up"); got != "Up" {
		t.Errorf("item up shows %q, want Up - nothing else claims it here", got)
	}
}

// The context carries the registry's order, so the newest binding wins there
// too rather than the answer changing when a context happens to be rebuilt.
func TestContextKeepsRegistrationOrder(t *testing.T) {
	r := NewKeyRegistry("test", []Binding{{"^Q", []string{"app_quit"}}})
	r.AddBinding("s-q", "app_quit")

	ctx := r.BuildContext([]string{"app_quit"})

	for i := 0; i < 25; i++ {
		if got := ctx.KeyForCommand("app_quit"); got != "s-q" {
			t.Fatalf("context shows %q, want the newest binding s-q", got)
		}
	}
}

// An accelerator formed into a context is the newest thing in the room, and
// does not disturb what the configured bindings advertise.
func TestFormedAcceleratorsDoNotDisturbTheOrder(t *testing.T) {
	r := NewKeyRegistry("test", []Binding{{"^Q", []string{"app_quit"}}})
	ctx := r.BuildContext([]string{"app_quit"})

	ctx.Add("M-h", CommandAppAccelerator)

	if got := ctx.KeyForCommand("app_quit"); got != "^Q" {
		t.Errorf("KeyForCommand = %q, want ^Q", got)
	}
	if got := ctx.KeyForCommand(CommandAppAccelerator); got != "M-h" {
		t.Errorf("the formed accelerator shows %q, want M-h", got)
	}
}

// Nothing bound, nothing to show - and a nil context is not a crash.
func TestKeyForCommandWithNothingBound(t *testing.T) {
	r := NewKeyRegistry("test", nil)
	if got := r.KeyForCommand("app_quit"); got != "" {
		t.Errorf("KeyForCommand = %q, want empty", got)
	}
	if got := r.BuildContext([]string{"app_quit"}).KeyForCommand("app_quit"); got != "" {
		t.Errorf("context KeyForCommand = %q, want empty", got)
	}
	var nilCtx *KeyContext
	if got := nilCtx.KeyForCommand("app_quit"); got != "" {
		t.Errorf("nil context KeyForCommand = %q, want empty", got)
	}
}

// The toolkit's own table has an opinion where it matters: the menu key
// advertises F10 (the convention) though F2 also opens it, and activation
// advertises the home row's Return though Space activates too. Both keys keep
// working either way -- a preference is about what is SHOWN.
func TestDefaultRegistryAdvertisesTheConventionalSpelling(t *testing.T) {
	r := DefaultKeyRegistry()
	for cmd, want := range map[string]string{
		CmdAppMenu:         "F10",
		CmdTrinketActivate: "Return",
		CmdAppQuit:         "^Q",
		CmdWindowClose:     "^W",
	} {
		if got := r.KeyForCommand(cmd); got != want {
			t.Errorf("%s advertises %q, want %q (of %v)", cmd, got, want, r.KeysFor(cmd))
		}
	}
	// ...and the alternatives are still bound, not traded away.
	for cmd, alt := range map[string]string{
		CmdAppMenu:         "F2",
		CmdTrinketActivate: "Space",
		CmdAppQuit:         "M-F4",
	} {
		found := false
		for _, k := range r.KeysFor(cmd) {
			if k == alt {
				found = true
			}
		}
		if !found {
			t.Errorf("%s lost its %s binding: %v", cmd, alt, r.KeysFor(cmd))
		}
	}
}
