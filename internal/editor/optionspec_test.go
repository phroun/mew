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

// set_option with no value opens a prompt seeded from the registry: the label
// lists the choices, the history holds every value in order with the current
// value repeated as the last filled line (the default on empty), and the cursor
// starts on a trailing blank line.
func TestSetOptionPromptsForValue(t *testing.T) {
	e, w := newTestEditor(t, "x\n") // direction defaults ltr

	e.PawScript.ExecuteAsync("set_option 'direction'")
	fw := focusedPrompt(e)
	if fw == nil {
		t.Fatal("set_option with no value should open a prompt")
	}
	if len(fw.RowMessages) == 0 || !strings.Contains(fw.RowMessages[0], "Set direction (ltr/rtl)") {
		t.Fatalf("prompt label = %v, want it to list (ltr/rtl)", fw.RowMessages)
	}
	if got := fw.Buffer.GetContent(); !strings.Contains(got, "ltr\nrtl\n") {
		t.Fatalf("prompt history %q should list the values in order", got)
	}
	// Cursor on a blank line, with the current value (ltr) just above it.
	if cur := strings.TrimRight(fw.Buffer.GetLine(fw.CursorPos().Line), "\n\r"); cur != "" {
		t.Fatalf("cursor should start on the blank input line, got %q", cur)
	}
	if last := strings.TrimRight(fw.Buffer.GetLine(fw.CursorPos().Line-1), "\n\r"); last != "ltr" {
		t.Fatalf("last filled line = %q, want the current value ltr", last)
	}

	// Empty accept keeps the current value.
	answerPrompt(t, e, "")
	if v, _ := e.getOption(w, "direction"); v != "ltr" {
		t.Fatalf("empty accept should keep ltr, got %q", v)
	}

	// Typing a value applies it.
	e.PawScript.ExecuteAsync("set_option 'direction'")
	answerPrompt(t, e, "rtl")
	if v, _ := e.getOption(w, "direction"); v != "rtl" {
		t.Fatalf("typed rtl should apply, got %q", v)
	}

	// The repeated last-filled line now follows the new current value.
	e.PawScript.ExecuteAsync("set_option 'direction'")
	fw = focusedPrompt(e)
	if fw == nil {
		t.Fatal("expected prompt")
	}
	if last := strings.TrimRight(fw.Buffer.GetLine(fw.CursorPos().Line-1), "\n\r"); last != "rtl" {
		t.Fatalf("last filled line after set = %q, want rtl", last)
	}
	cancelPrompt(t, e)
}

// A non-enumerable option (no fixed value list) still prompts, offering just the
// current value as the editable default; no choice list appears in the label.
func TestSetOptionPromptNonEnumerable(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	e.setOption(w, "tabSize", "4")
	e.PawScript.ExecuteAsync("set_option 'tabSize'")
	fw := focusedPrompt(e)
	if fw == nil {
		t.Fatal("set_option tabSize with no value should prompt")
	}
	if !strings.Contains(fw.RowMessages[0], "Set tabSize:") {
		t.Fatalf("non-enumerable label should be a plain 'Set tabSize:' with no choice list: %q", fw.RowMessages[0])
	}
	if last := strings.TrimRight(fw.Buffer.GetLine(fw.CursorPos().Line-1), "\n\r"); last != "4" {
		t.Fatalf("default line = %q, want current tabSize 4", last)
	}
	answerPrompt(t, e, "8")
	if v, _ := e.getOption(w, "tabSize"); v != "8" {
		t.Fatalf("typed tabSize should apply, got %q", v)
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

// rtlCombining defaults ON (marks shown), stored inverted so an untouched
// window keeps that default; setting it off flips the ViewState sense.
func TestRtlCombiningDefaultsOn(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "") // not a bidi-applying terminal: plain default
	e, w := newTestEditor(t, "x\n")
	if v, _ := e.getOption(w, "rtlCombining"); v != "yes" {
		t.Fatalf("rtlCombining should default to yes, got %q", v)
	}
	if w.ViewState.SuppressRTLCombining {
		t.Fatal("default window must not suppress RTL combining")
	}
	e.setOption(w, "rtlCombining", "no")
	if !w.ViewState.SuppressRTLCombining {
		t.Fatal("rtlCombining=no must set SuppressRTLCombining")
	}
	if v, _ := e.getOption(w, "rtlCombining"); v != "no" {
		t.Fatalf("rtlCombining should read back no, got %q", v)
	}
}

// In macOS Terminal.app (TERM_PROGRAM=Apple_Terminal), a real-terminal
// session with no explicit rtlCombining defaults it OFF, so pointed RTL
// renders unpointed and the selection bar stays correct there. An explicit
// config value overrides the auto-default.
func TestRtlCombiningAutoOffInAppleTerminal(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")

	// No explicit setting -> auto off.
	e, _ := newTestEditor(t, "x\n")
	if e.Config.RtlCombining {
		t.Error("Apple_Terminal with no config should auto-default rtlCombining OFF")
	}

	// Explicit rtlCombining=true in config -> honored despite the terminal.
	e2, _ := newTestEditor(t, "x\n", "rtlCombining=true")
	if !e2.Config.RtlCombining {
		t.Error("an explicit rtlCombining=true must override the Apple_Terminal auto-default")
	}

	// A non-bidi terminal keeps the on default.
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	e3, _ := newTestEditor(t, "x\n")
	if !e3.Config.RtlCombining {
		t.Error("a non-bidi terminal should keep rtlCombining ON by default")
	}
}
