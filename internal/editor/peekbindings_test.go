package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/plugins"
)

// KeyForCommand returns the stored spelling of the key bound to a command, the
// lexicographically-first when several are bound, and "" when unbound.
func TestKeyForCommand(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "hi\n")

	e.KeyProcessor.MapKey("^X 1", "only_here")
	if got := e.KeyForCommand("only_here"); got != "^X 1" {
		t.Errorf("KeyForCommand = %q, want %q", got, "^X 1")
	}

	// Two bindings: the lexicographically-first stored spelling wins (stable).
	e.KeyProcessor.MapKey("^X 2", "two_ways")
	e.KeyProcessor.MapKey("^A 9", "two_ways")
	if got := e.KeyForCommand("two_ways"); got != "^A 9" {
		t.Errorf("KeyForCommand (multi) = %q, want %q", got, "^A 9")
	}

	if got := e.KeyForCommand("nonexistent_command"); got != "" {
		t.Errorf("unbound command should resolve to empty, got %q", got)
	}
}

// The peek %CODE%s resolve to the live bindings and re-track a rebind.
func TestPeekBindingValuesTrackKeymap(t *testing.T) {
	e, _, _ := newRenderedEditor(t, "hi\n")

	e.KeyProcessor.UnmapKey("^@ U") // clear the default mew binding
	e.KeyProcessor.MapKey("^K U", "stat_peek_up")
	if got := e.peekBindingValues()["SPU"]; got != "^K U" {
		t.Errorf("SPU should track the rebind, got %q", got)
	}
	// The label expands through the shared engine.
	if got := plugins.ExpandModebar("[%SPU%]", e.peekBindingValues()); got != "[^K U]" {
		t.Errorf("peek label = %q, want %q", got, "[^K U]")
	}
}

// A modebar template can also use the peek codes (they are part of the same
// engine), resolving through the live keymap on render.
func TestModebarTemplateResolvesPeekCode(t *testing.T) {
	e, _, out := newRenderedEditor(t, "hi\n")
	e.createPluginWindows()
	// The middle default template is driven from the base config (reconciled
	// onto the modebar per the focused window's overlay).
	e.Config.ModebarDefault = "peek=%PPU%"
	e.invalidateFocusedOptions()
	e.performRender() // settle focused options (loads the default mapping set)
	// Rebind at runtime, after the mapping set has settled, so the change sticks.
	e.KeyProcessor.UnmapKey("^@ P")
	e.KeyProcessor.MapKey("^K P", "prompt_peek_up")
	// Force the middle default to show (no live breadcrumb) and repaint fully.
	e.Renderer.ForceRedraw()
	out.Reset()
	e.performRender()
	if !strings.Contains(stripAnsi(out.String()), "peek=^K P") {
		t.Errorf("modebar should resolve %%PPU%% to the binding: %q", stripAnsi(out.String()))
	}
}
