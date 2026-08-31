package config

import (
	"reflect"
	"strings"
	"testing"
)

func mac() KeymapEnvironment   { return KeymapEnvironment{OS: "darwin"} }
func wnt() KeymapEnvironment   { return KeymapEnvironment{OS: "windows"} }
func plain() KeymapEnvironment { return KeymapEnvironment{OS: "linux"} }

// A hint is stripped from the key text wherever it was written, and the key
// left behind is what the user actually presses. The three spellings are one
// binding, so they have to produce one map key.
func TestKeyHintsStripAnywhere(t *testing.T) {
	for _, raw := range []string{"(mac) s-w", "s-w (mac)", "(mac) s-w (gfx)"} {
		key, _, keep, unknown := KeyHints(raw, mac())
		if !keep || len(unknown) != 0 {
			t.Errorf("KeyHints(%q): keep=%v unknown=%v, want kept and nothing unknown", raw, keep, unknown)
		}
		if key != "s-w" {
			t.Errorf("KeyHints(%q) key = %q, want s-w", raw, key)
		}
	}

	// ...including between the keys of a chord.
	if key, _, _, _ := KeyHints("^B (mac) O", mac()); key != "^B O" {
		t.Errorf("mid-chord hint left %q, want ^B O", key)
	}
}

// only_ and never_ are a requirement: where they do not hold the line is gone,
// and no later layer inherits or overrides a binding that was never made.
func TestRequiringHintsDropTheLine(t *testing.T) {
	for _, c := range []struct {
		raw  string
		env  KeymapEnvironment
		keep bool
	}{
		{"(only_mac) s-w", mac(), true},
		{"(only_mac) s-w", wnt(), false},
		{"(never_mac) ^W", mac(), false},
		{"(never_mac) ^W", wnt(), true},
		// Every hint on the line must hold.
		{"(only_mac) (only_gfx) s-w", mac(), false}, // console build
		{"(only_mac) (only_con) s-w", mac(), true},
	} {
		if _, _, keep, _ := KeyHints(c.raw, c.env); keep != c.keep {
			t.Errorf("KeyHints(%q, %s) keep = %v, want %v", c.raw, c.env.OS, keep, c.keep)
		}
	}
}

// A plain hint is a PREFERENCE and nothing more: the binding is kept on every
// platform, and only its weight moves. This is the whole difference between
// the two families, and getting it wrong would silently unbind half a keymap.
func TestPlainHintsOnlyWeigh(t *testing.T) {
	for _, c := range []struct {
		raw    string
		env    KeymapEnvironment
		weight int
	}{
		{"(mac) s-w", mac(), 1},       // this machine's own spelling
		{"(mac) s-w", wnt(), -1},      // some other machine's
		{"(non_mac) ^W", wnt(), 1},    // negated, and it holds here
		{"(non_mac) ^W", mac(), -1},   // negated, and it does not
		{"^W", plain(), 0},            // unhinted sits between the two
		{"(mac) (con) s-w", mac(), 2}, // preferences add up
	} {
		_, weight, keep, _ := KeyHints(c.raw, c.env)
		if !keep {
			t.Errorf("KeyHints(%q, %s) dropped the line; a plain hint never does", c.raw, c.env.OS)
		}
		if weight != c.weight {
			t.Errorf("KeyHints(%q, %s) weight = %d, want %d", c.raw, c.env.OS, weight, c.weight)
		}
	}
}

// The level words belong to the key sequence processor, which reads this same
// key text after mew does. Stripping them would silently drop a binding's
// precedence; complaining about them would cry wolf on a correct keymap.
func TestLevelWordsPassThroughUntouched(t *testing.T) {
	key, _, keep, unknown := KeyHints("(capture) (mac) ^C", mac())
	if key != "(capture) ^C" {
		t.Errorf("key = %q, want (capture) ^C — the level word is not mew's to strip", key)
	}
	if !keep || len(unknown) != 0 {
		t.Errorf("keep=%v unknown=%v, want kept and quiet", keep, unknown)
	}
}

// A parenthesis is a key. Only a whole "(word)" token is metadata, which is the
// same rule keyseq applies — a token that is metadata to one reader and a
// keystroke to the other is exactly the bug the notation prevents.
func TestParenthesesAreStillKeys(t *testing.T) {
	for _, raw := range []string{"(", ")", "()", "^(", "^B ("} {
		key, weight, keep, unknown := KeyHints(raw, mac())
		if key != raw || weight != 0 || !keep || len(unknown) != 0 {
			t.Errorf("KeyHints(%q) = (%q, %d, %v, %v), want the key untouched",
				raw, key, weight, keep, unknown)
		}
	}
}

// A word nobody claims is reported and LEFT IN the key text. Dropping it would
// turn a typo into a binding that looks right and never fires on the platform
// it was meant for; leaving it makes the binding visibly wrong, and the report
// says which line to fix.
func TestUnknownHintIsReportedNotSwallowed(t *testing.T) {
	key, _, keep, unknown := KeyHints("(mak) s-w", mac())
	if !reflect.DeepEqual(unknown, []string{"mak"}) {
		t.Errorf("unknown = %v, want [mak]", unknown)
	}
	if key != "(mak) s-w" || !keep {
		t.Errorf("key = %q keep = %v, want the token left in place and the line kept", key, keep)
	}
}

// The desktop hints test against what the session advertises, through the
// aliases desktops actually use for themselves.
func TestDesktopHints(t *testing.T) {
	for _, c := range []struct {
		desktop string
		raw     string
		weight  int
	}{
		{"kde", "(kde) ^W", 1},
		{"plasma", "(kde) ^W", 1},          // KDE announces Plasma
		{"ubuntu:GNOME", "(gnome) ^W", 1},  // a distro may prepend its name
		{"X-Cinnamon", "(cinnamon) ^W", 1}, // ...and Cinnamon prepends X-
		{"gnome", "(kde) ^W", -1},
		{"", "(kde) ^W", -1},
	} {
		env := KeymapEnvironment{OS: "linux", Desktop: strings.ToLower(c.desktop)}
		if _, weight, _, _ := KeyHints(c.raw, env); weight != c.weight {
			t.Errorf("KeyHints(%q) under %q weight = %d, want %d", c.raw, c.desktop, weight, c.weight)
		}
	}
}

// (gfx) and (con) are answered by the BUILD, since no binary is both. This
// checks the wiring from build tag through to the default environment: get it
// wrong and every graphical-only binding silently becomes console-only, or the
// other way round, with nothing to see at the point of use.
func TestGraphicalComesFromTheBuild(t *testing.T) {
	env := CurrentKeymapEnvironment()
	if env.Graphical != hostIsGraphical {
		t.Fatalf("environment Graphical = %v, want %v from the build", env.Graphical, hostIsGraphical)
	}

	gfxKept := func(raw string) bool { _, _, keep, _ := KeyHints(raw, env); return keep }
	if got := gfxKept("(only_gfx) s-w"); got != hostIsGraphical {
		t.Errorf("(only_gfx) kept = %v on a Graphical=%v build", got, hostIsGraphical)
	}
	if got := gfxKept("(only_con) ^W"); got == hostIsGraphical {
		t.Errorf("(only_con) kept = %v on a Graphical=%v build", got, hostIsGraphical)
	}
}
