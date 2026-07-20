package editor

import (
	"strings"
	"testing"
)

// The CLI known/per-window maps are derived from optionSpecs, so the table is
// the single source of truth: every spec appears in cliKnownOptions, and PerWin
// exactly matches cliPerWindowOptions.
func TestOptionSpecsDeriveCliMaps(t *testing.T) {
	if len(cliKnownOptions) != len(optionSpecs) {
		t.Fatalf("cliKnownOptions has %d entries, optionSpecs has %d", len(cliKnownOptions), len(optionSpecs))
	}
	for _, s := range optionSpecs {
		key := strings.ToLower(s.Name)
		if !cliKnownOptions[key] {
			t.Errorf("%s missing from cliKnownOptions", s.Name)
		}
		if s.PerWin != cliPerWindowOptions[key] {
			t.Errorf("%s: PerWin=%v but cliPerWindowOptions=%v", s.Name, s.PerWin, cliPerWindowOptions[key])
		}
	}
}

// Every canonical value must round-trip through the real setter/getter: a value
// set_option accepts and getOption reports back unchanged. This is what lets a
// rotation find the current value in the list and advance from it. Booleans use
// the shared false/true sequence.
func TestOptionSpecCanonicalValuesRoundTrip(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	for _, s := range optionSpecs {
		if s.Kind == optBoolKind {
			if len(s.Values) != 2 || s.Values[0] != "no" || s.Values[1] != "yes" {
				t.Errorf("%s: boolean should have values [no yes], got %v", s.Name, s.Values)
			}
		}
		if s.Values == nil {
			continue
		}
		for _, v := range s.Values {
			if !e.setOption(w, s.Name, v) {
				t.Errorf("%s: setOption(%q) was rejected", s.Name, v)
				continue
			}
			got, ok := e.getOption(w, s.Name)
			if !ok || got != v {
				t.Errorf("%s: set %q but getOption returned %q (ok=%v) — canonical value must match getOption's format", s.Name, v, got, ok)
			}
		}
	}
}

// set_option_next / set_option_prior rotate through the canonical sequence and
// wrap at the ends.
func TestSetOptionRotate(t *testing.T) {
	e, w := newTestEditor(t, "x\n")

	// Boolean (per-window): no -> yes -> no. Input alias "false" still accepted.
	e.setOption(w, "showLineNumbers", "false")
	if !e.rotateOption(w, "showLineNumbers", +1) {
		t.Fatal("rotate showLineNumbers next should succeed")
	}
	if v, _ := e.getOption(w, "showLineNumbers"); v != "yes" {
		t.Fatalf("showLineNumbers after next: %q, want yes", v)
	}
	e.rotateOption(w, "showLineNumbers", +1)
	if v, _ := e.getOption(w, "showLineNumbers"); v != "no" {
		t.Fatalf("showLineNumbers wraps back to no, got %q", v)
	}

	// Three-value per-window enum: showMarks cycles no -> yes -> all -> no.
	e.setOption(w, "showMarks", "no")
	for _, want := range []string{"yes", "all", "no"} {
		if !e.rotateOption(w, "showMarks", +1) {
			t.Fatal("rotate showMarks next should succeed")
		}
		if v, _ := e.getOption(w, "showMarks"); v != want {
			t.Fatalf("showMarks next: %q, want %q", v, want)
		}
	}

	// Enum with three values: auto -> true -> false -> auto (and prior wraps
	// the other way).
	seq := []string{"true", "false", "auto"}
	for _, want := range seq {
		if !e.rotateOption(nil, "macOptionKeys", +1) {
			t.Fatal("rotate macOptionKeys next should succeed")
		}
		if v, _ := e.getOption(nil, "macOptionKeys"); v != want {
			t.Fatalf("macOptionKeys next: %q, want %q", v, want)
		}
	}
	// prior from auto wraps to the last value (false).
	if !e.rotateOption(nil, "macOptionKeys", -1) {
		t.Fatal("rotate macOptionKeys prior should succeed")
	}
	if v, _ := e.getOption(nil, "macOptionKeys"); v != "false" {
		t.Fatalf("macOptionKeys prior from auto: %q, want false", v)
	}

	// A non-enumerable option cannot be rotated.
	if e.rotateOption(w, "tabSize", +1) {
		t.Fatal("rotating tabSize (an integer) should fail")
	}
	// Unknown option fails too.
	if e.rotateOption(w, "nonesuch", +1) {
		t.Fatal("rotating an unknown option should fail")
	}
}

// The rotation commands are registered and drive rotateOption.
func TestSetOptionRotateCommands(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	e.setOption(w, "direction", "ltr")
	e.PawScript.ExecuteAsync("set_option_next 'direction'")
	if v, _ := e.getOption(w, "direction"); v != "rtl" {
		t.Fatalf("set_option_next direction: %q, want rtl", v)
	}
	e.PawScript.ExecuteAsync("set_option_prior 'direction'")
	if v, _ := e.getOption(w, "direction"); v != "ltr" {
		t.Fatalf("set_option_prior direction: %q, want ltr", v)
	}
}

// clear_option drops a per-window override and reverts the window to the
// resolved default (here, the editor default), leaving the option no longer
// pinned.
func TestClearOption(t *testing.T) {
	e, w := newTestEditor(t, "x\n") // showMarks defaults "no"

	// Pin an override on the window.
	e.setOption(w, "showMarks", "all")
	if w.ViewState.ShowMarks != "all" || !w.IsOptionOverridden("showmarks") {
		t.Fatal("set_option should pin showMarks=all on the window")
	}

	// Clearing reverts to the editor default and un-pins it.
	if !e.clearOption(w, "showMarks") {
		t.Fatal("clear_option should succeed for a per-window option")
	}
	if w.ViewState.ShowMarks != "no" {
		t.Fatalf("clear_option should revert showMarks to the default (no), got %q", w.ViewState.ShowMarks)
	}
	if w.IsOptionOverridden("showmarks") {
		t.Fatal("clear_option should drop the override flag")
	}

	// A global option has no per-window layer to clear.
	if e.clearOption(w, "wordWrap") {
		t.Fatal("clear_option on a global option should fail")
	}
	// Unknown option fails.
	if e.clearOption(w, "nonesuch") {
		t.Fatal("clear_option on an unknown option should fail")
	}
}

// clear_option reverts to the configured default when one is set, not a
// hardcoded zero value.
func TestClearOptionRevertsToConfiguredDefault(t *testing.T) {
	e, w := newTestEditor(t, "x\n", "showMarks=all") // editor default: all
	if e.Config.ShowMarks != "all" {
		t.Fatalf("config should set the editor default showMarks=all, got %q", e.Config.ShowMarks)
	}
	// Override the window off, then clear: it should return to the config's "all".
	e.setOption(w, "showMarks", "no")
	if w.ViewState.ShowMarks != "no" {
		t.Fatalf("override should turn the window's showMarks off, got %q", w.ViewState.ShowMarks)
	}
	if !e.clearOption(w, "showMarks") {
		t.Fatal("clear_option should succeed")
	}
	if w.ViewState.ShowMarks != "all" {
		t.Fatalf("clear_option should restore the configured default (all), got %q", w.ViewState.ShowMarks)
	}
}

// clear_option through the command path targets the active main-buffer window.
func TestClearOptionCommand(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	e.setOption(w, "tabSize", "8")
	if w.ViewState.TabSize != 8 {
		t.Fatalf("tabSize override: %d, want 8", w.ViewState.TabSize)
	}
	e.PawScript.ExecuteAsync("clear_option 'tabSize'")
	if w.ViewState.TabSize != e.Config.TabSize {
		t.Fatalf("clear_option tabSize: %d, want editor default %d", w.ViewState.TabSize, e.Config.TabSize)
	}
}
