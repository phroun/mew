package core

import (
	"strings"
	"testing"
)

// withEnvironment runs fn with the keymap environment set, and puts back
// whatever the process actually is afterward.
func withEnvironment(t *testing.T, env KeymapEnvironment, fn func()) {
	t.Helper()
	saved := CurrentKeymapEnvironment()
	SetKeymapEnvironment(env)
	defer SetKeymapEnvironment(saved)
	fn()
}

// A hint is a word in parentheses ANYWHERE in the key text: before it, after
// it, or between the keys of a sequence. The key itself is what is left.
func TestHintsAreStrippedFromAnywhereInTheKey(t *testing.T) {
	env := KeymapEnvironment{OS: "darwin"}
	for _, raw := range []string{"(mac) s-w", "s-w (mac)", "(mac)s-w"} {
		key, weight, keep := keyHints(raw, env)
		if key != "s-w" || !keep || weight <= 0 {
			t.Errorf("keyHints(%q) = %q, weight %d, keep %v; want s-w promoted", raw, key, weight, keep)
		}
	}
	if key, _, _ := keyHints("^B (mac) O", env); key != "^B O" {
		t.Errorf("a hint between the keys of a sequence left %q, want ^B O", key)
	}
}

// A plain hint is a PREFERENCE: the binding exists everywhere, promoted where
// the hint matches and demoted where it names somewhere else.
func TestPlainHintPromotesAndDemotes(t *testing.T) {
	mac := KeymapEnvironment{OS: "darwin"}
	other := KeymapEnvironment{OS: "linux"}

	if _, w, keep := keyHints("(mac) s-w", mac); w <= 0 || !keep {
		t.Errorf("on a Mac, (mac) should promote and stay bound: weight %d keep %v", w, keep)
	}
	if _, w, keep := keyHints("(mac) s-w", other); w >= 0 || !keep {
		t.Errorf("elsewhere, (mac) should demote but stay bound: weight %d keep %v", w, keep)
	}
	// non_ is the same hint negated.
	if _, w, _ := keyHints("(non_mac) ^W", other); w <= 0 {
		t.Errorf("(non_mac) should promote off a Mac, weight %d", w)
	}
	if _, w, _ := keyHints("(non_mac) ^W", mac); w >= 0 {
		t.Errorf("(non_mac) should demote on a Mac, weight %d", w)
	}
}

// only_ and never_ are a REQUIREMENT: where they do not match, the line may as
// well not have been written.
func TestRequiringHintsDisqualify(t *testing.T) {
	mac := KeymapEnvironment{OS: "darwin"}
	nt := KeymapEnvironment{OS: "windows"}

	if _, _, keep := keyHints("(only_mac) s-w", mac); !keep {
		t.Error("only_mac should keep the binding on a Mac")
	}
	if _, _, keep := keyHints("(only_mac) s-w", nt); keep {
		t.Error("only_mac should disqualify the binding off a Mac")
	}
	if _, _, keep := keyHints("(never_mac) ^W", mac); keep {
		t.Error("never_mac should disqualify the binding on a Mac")
	}
	if _, _, keep := keyHints("(never_mac) ^W", nt); !keep {
		t.Error("never_mac should keep the binding off a Mac")
	}
}

// The whole point, end to end: one table, and each environment advertises its
// own spelling while both keys keep working.
func TestOneTableAdvertisesPerEnvironment(t *testing.T) {
	table := []Binding{
		{"(mac) s-w", []string{"window_close"}},
		{"^W", []string{"window_close"}},
	}

	withEnvironment(t, KeymapEnvironment{OS: "darwin"}, func() {
		r := NewKeyRegistry("test", table)
		if got := r.KeyForCommand("window_close"); got != "s-w" {
			t.Errorf("on a Mac the menu shows %q, want s-w", got)
		}
		if keys := r.KeysFor("window_close"); len(keys) != 2 {
			t.Errorf("both keys should still work: %v", keys)
		}
	})

	withEnvironment(t, KeymapEnvironment{OS: "linux"}, func() {
		r := NewKeyRegistry("test", table)
		if got := r.KeyForCommand("window_close"); got != "^W" {
			t.Errorf("off a Mac the menu shows %q, want ^W", got)
		}
		if keys := r.KeysFor("window_close"); len(keys) != 2 {
			t.Errorf("both keys should still work: %v", keys)
		}
	})
}

// A requiring hint keeps the key out of the registry entirely, so it resolves
// nothing: not a demoted binding, an absent one.
func TestDisqualifiedBindingIsNeverBound(t *testing.T) {
	table := []Binding{
		{"(only_mac) s-w", []string{"window_close"}},
		{"^W", []string{"window_close"}},
	}
	withEnvironment(t, KeymapEnvironment{OS: "windows"}, func() {
		r := NewKeyRegistry("test", table)
		if keys := r.KeysFor("window_close"); len(keys) != 1 || keys[0] != "^W" {
			t.Errorf("KeysFor = %v, want ^W alone", keys)
		}
		if cmd := r.BuildContext([]string{"window_close"}).Resolve("s-w"); cmd != "" {
			t.Errorf("s-w resolved to %q; a disqualified key must reach nothing", cmd)
		}
	})
}

// The environment preference outranks registration order: a keymap's own
// (mac) line beats a LATER unhinted one on a Mac, and loses to it elsewhere.
func TestEnvironmentPreferenceOutranksOrder(t *testing.T) {
	table := []Binding{
		{"(mac) s-w", []string{"window_close"}},
		{"^W", []string{"window_close"}}, // written later
	}
	withEnvironment(t, KeymapEnvironment{OS: "darwin"}, func() {
		if got := NewKeyRegistry("test", table).KeyForCommand("window_close"); got != "s-w" {
			t.Errorf("KeyForCommand = %q, want the Mac's own s-w", got)
		}
	})
}

// gfx and con are the host's own nature, which it declares.
func TestGraphicalAndConsoleHints(t *testing.T) {
	gfx := KeymapEnvironment{OS: "linux", Graphical: true}
	con := KeymapEnvironment{OS: "linux"}

	if _, w, _ := keyHints("(gfx) ^W", gfx); w <= 0 {
		t.Error("(gfx) should promote on a graphical host")
	}
	if _, _, keep := keyHints("(only_gfx) ^W", con); keep {
		t.Error("only_gfx should disqualify on a terminal host")
	}
	if _, _, keep := keyHints("(only_con) ^W", con); !keep {
		t.Error("only_con should keep on a terminal host")
	}
}

// The desktop hints read what the session advertises, through the names the
// desktops actually use for themselves.
func TestDesktopHints(t *testing.T) {
	cases := []struct {
		desktop, hint string
		want          bool
	}{
		{"kde", "kde", true},
		{"plasma", "kde", true},          // KDE announces Plasma
		{"ubuntu:gnome", "gnome", true},  // a distribution prepends itself
		{"x-cinnamon", "cinnamon", true}, // Cinnamon announces X-Cinnamon
		{"gnome", "kde", false},
		{"hyprland", "hyprland", true},
		{"", "gnome", false}, // no session says nothing
	}
	for _, c := range cases {
		env := KeymapEnvironment{OS: "linux", Desktop: c.desktop}
		_, w, keep := keyHints("("+c.hint+") ^W", env)
		if got := w > 0; got != c.want {
			t.Errorf("desktop %q against (%s): promoted=%v, want %v", c.desktop, c.hint, got, c.want)
		}
		if !keep {
			t.Errorf("a plain hint must never disqualify (%q against %s)", c.desktop, c.hint)
		}
		_, _, keep = keyHints("(only_"+c.hint+") ^W", env)
		if keep != c.want {
			t.Errorf("desktop %q against (only_%s): keep=%v, want %v", c.desktop, c.hint, keep, c.want)
		}
	}
}

// Hints compound: each requirement must hold, and the preferences add up, so a
// line that names this environment twice over outranks one that names it once.
func TestHintsCompound(t *testing.T) {
	macGfx := KeymapEnvironment{OS: "darwin", Graphical: true}

	_, both, _ := keyHints("(mac) (gfx) s-w", macGfx)
	_, one, _ := keyHints("(mac) s-w", macGfx)
	if both <= one {
		t.Errorf("(mac)(gfx) weighs %d, (mac) weighs %d; the more specific should lead", both, one)
	}
	if _, _, keep := keyHints("(only_mac) (only_con) s-w", macGfx); keep {
		t.Error("every requirement must hold; this host is graphical")
	}
}

// A word that is not a hint is not treated as one: a keymap can bind "(" and
// an unrecognized parenthesis stays in the key rather than silently matching
// nothing.
func TestUnknownParenthesesAreLeftAlone(t *testing.T) {
	env := KeymapEnvironment{OS: "linux"}
	for _, raw := range []string{"(", "(banana) ^W", "^(", "(unclosed ^W"} {
		key, w, keep := keyHints(raw, env)
		if key != strings.Join(strings.Fields(raw), " ") || w != 0 || !keep {
			t.Errorf("keyHints(%q) = %q, weight %d, keep %v; want it left alone", raw, key, w, keep)
		}
	}
}

// A host may declare the desktop itself ([window] host_type), for a session
// that advertises nothing or the wrong thing. Declaring it wins over what was
// detected; leaving it blank keeps the detection rather than blanking it.
func TestHostCanDeclareTheDesktop(t *testing.T) {
	saved := CurrentKeymapEnvironment()
	defer SetKeymapEnvironment(saved)

	SetKeymapEnvironment(KeymapEnvironment{OS: "linux", Desktop: "kde"})
	if got := CurrentKeymapEnvironment().Desktop; got != "kde" {
		t.Errorf("Desktop = %q, want the declared kde", got)
	}
	if _, w, _ := keyHints("(kde) ^W", CurrentKeymapEnvironment()); w <= 0 {
		t.Error("a declared desktop should satisfy its own hint")
	}

	// Blank means "whatever this session is", not "no desktop".
	SetKeymapEnvironment(KeymapEnvironment{OS: "linux"})
	if got, want := CurrentKeymapEnvironment().Desktop, detectDesktop(); got != want {
		t.Errorf("Desktop = %q with nothing declared, want the detected %q", got, want)
	}
}
