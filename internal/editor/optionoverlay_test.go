package editor

import (
	"strings"
	"testing"
)

// A buffer whose grammar has an [options.<grammar>] overlay takes the overlaid
// per-window options; the base [options] shows through where the overlay is
// silent.
func TestOptionOverlayAppliesForGrammar(t *testing.T) {
	cfg := "[options]\nsyntax=cpp\ntabSize=8\nshowLineNumbers=true\nshowInvisibles=false\n" +
		"[options.cpp]\ntabSize=2\nshowInvisibles=true\n"
	e, w, _ := renderedEditorWithConfig(t, "int main(){}\n", cfg)
	e.performRender()

	if w.ViewState.TabSize != 2 {
		t.Errorf("tabSize = %d, want 2 (overlay)", w.ViewState.TabSize)
	}
	if !w.ViewState.ShowInvisibles {
		t.Error("showInvisibles should be true (overlay)")
	}
	// Not mentioned by the overlay -> base [options] shows through.
	if !w.ViewState.ShowLineNumbers {
		t.Error("showLineNumbers should stay true (base)")
	}
}

// A grammar with no overlay is left at the base [options]: the cpp overlay must
// not bleed into a go window.
func TestOptionOverlayBaseForOtherGrammar(t *testing.T) {
	cfg := "[options]\nsyntax=go\ntabSize=8\n[options.cpp]\ntabSize=2\n"
	e, w, _ := renderedEditorWithConfig(t, "package main\n", cfg)
	w.ViewState.TabSize = e.Config.TabSize // as createMainWindow seeds it
	e.performRender()
	if w.ViewState.TabSize != 8 {
		t.Errorf("a go buffer should keep base tabSize 8 (cpp overlay must not apply), got %d", w.ViewState.TabSize)
	}
}

// "default" in an overlay resolves to the shipped default even when the base
// [options] changed it; "inherit" defers to the base.
func TestOptionOverlayDefaultAndInherit(t *testing.T) {
	cfg := "[options]\nsyntax=cpp\ntabSize=8\nshowLineNumbers=false\n" +
		"[options.cpp]\ntabSize=default\nshowLineNumbers=inherit\n"
	e, w, _ := renderedEditorWithConfig(t, "int x;\n", cfg)
	e.performRender()
	if w.ViewState.TabSize != 4 { // shipped default
		t.Errorf("tabSize=default should be the shipped default 4, got %d", w.ViewState.TabSize)
	}
	if w.ViewState.ShowLineNumbers { // inherit -> base false
		t.Error("showLineNumbers=inherit should defer to base (false)")
	}
}

// A user set_option pins the option: a later grammar re-resolution leaves it.
func TestOptionOverlayUserOverridePinned(t *testing.T) {
	cfg := "[options]\nsyntax=cpp\ntabSize=8\n[options.cpp]\ntabSize=2\n"
	e, w, _ := renderedEditorWithConfig(t, "int x;\n", cfg)
	e.performRender()
	if w.ViewState.TabSize != 2 {
		t.Fatalf("precondition: overlay tabSize should be 2, got %d", w.ViewState.TabSize)
	}
	// User explicitly sets it.
	e.setOption(w, "tabsize", "16")
	// Force a grammar re-resolution.
	w.SetAppliedOptionSig("")
	e.reconcileGrammarOptions(w)
	if w.ViewState.TabSize != 16 {
		t.Errorf("user override should be pinned across re-resolution, got %d", w.ViewState.TabSize)
	}
}

// The editor-wide-but-window-scoped options (rulerShowsCursor, search*,
// matchIgnores*) resolve through the same overlay for a window.
func TestOptionOverlayEditorWideScoped(t *testing.T) {
	cfg := "[options]\nsyntax=cpp\nrulerShowsCursor=false\nsearchIgnoreCase=false\nmatchIgnoresHash=false\n" +
		"[options.cpp]\nrulerShowsCursor=true\nsearchIgnoreCase=true\nmatchIgnoresHash=true\n"
	e, w, _ := renderedEditorWithConfig(t, "int x;\n", cfg)
	if !e.optBool(w, "rulershowscursor", e.Config.RulerShowsCursor) {
		t.Error("rulerShowsCursor should resolve true for a cpp window")
	}
	if !e.optBool(w, "searchignorecase", e.Config.SearchIgnoreCase) {
		t.Error("searchIgnoreCase should resolve true for a cpp window")
	}
	if !e.resolvedMatchIgnores(w).hash {
		t.Error("matchIgnoresHash should resolve true for a cpp window")
	}
}

// A window's class refines the overlay: [<class>.options.<grammar>] wins over
// [options.<grammar>].
func TestOptionOverlayClassDimension(t *testing.T) {
	cfg := "[options]\nsyntax=cpp\ntabSize=8\n[options.cpp]\ntabSize=2\n[panel::options.cpp]\ntabSize=3\n"
	e, w, _ := renderedEditorWithConfig(t, "int x;\n", cfg)
	// Without a class, the grammar overlay applies.
	e.performRender()
	if w.ViewState.TabSize != 2 {
		t.Fatalf("no class: want grammar overlay tabSize 2, got %d", w.ViewState.TabSize)
	}
	// Give the window a class and re-resolve: the class overlay wins.
	w.Class = "panel"
	e.performRender()
	if w.ViewState.TabSize != 3 {
		t.Errorf("class 'panel' should win with tabSize 3, got %d", w.ViewState.TabSize)
	}
}

// Focused-scoped options follow the focused window: the modebar templates
// resolve through its grammar overlay.
func TestModebarTemplateOverlay(t *testing.T) {
	cfg := "[options]\nsyntax=cpp\nmodebarInner=BASEBAR\n[options.cpp]\nmodebarInner=CPPBAR\n"
	e, _, out := renderedEditorWithConfig(t, "int x;\n", cfg)
	e.createPluginWindows()
	e.performRender()
	if !strings.Contains(stripAnsi(out.String()), "CPPBAR") {
		t.Errorf("modebar should use the cpp overlay's inner template: %q", stripAnsi(out.String()))
	}
}

// The focused window's grammar (or class) selects its key-mapping set: a cpp
// window uses the emacs set, and a class overlay switches it back to the
// default mew set.
func TestPerWindowMappings(t *testing.T) {
	cfg := "[options]\nsyntax=cpp\nmappings=mew\n" +
		"[mappings:emacs]\n^X ^S\t=buffer_save\n" +
		"[options.cpp]\nmappings=emacs\n" +
		"[plain::options.cpp]\nmappings=mew\n"
	e, w, _ := renderedEditorWithConfig(t, "int x;\n", cfg)
	e.performRender()
	if got := e.KeyProcessor.GetMapping("^X ^S"); got != "buffer_save" {
		t.Fatalf("cpp window should load the emacs set (^X ^S), got %q", got)
	}
	// A class overlay switches the set back to the default mew set (with
	// built-ins), dropping the emacs binding.
	w.Class = "plain"
	e.performRender()
	if got := e.KeyProcessor.GetMapping("^X ^S"); got != "" {
		t.Errorf("plain class should switch off the emacs set, got %q", got)
	}
	if e.KeyProcessor.GetMapping("^K H") == "" {
		t.Error("the default mew set (with built-ins) should be active for the plain class")
	}
}

// syntax/syntaxDetect inside an overlay are ignored: the overlay cannot change
// which grammar a buffer uses.
func TestOptionOverlayCannotChangeGrammar(t *testing.T) {
	cfg := "[options]\nsyntax=cpp\n[options.cpp]\nsyntax=go\ntabSize=2\n"
	e, w, _ := renderedEditorWithConfig(t, "int x;\n", cfg)
	e.performRender()
	if got := e.bufferGrammarName(w.Buffer); got != "cpp" {
		t.Errorf("grammar should stay cpp, got %q", got)
	}
	if w.ViewState.TabSize != 2 {
		t.Errorf("the non-excluded overlay option should still apply, got tabSize %d", w.ViewState.TabSize)
	}
}
