package mewhost

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/app"
	"github.com/phroun/kittytk/objects/trinkets"
)

// The host builds its whole UI from protocol-style text, and those scripts'
// reply names and type assertions only resolve at execution time. These tests
// execute each script directly (no live desktop) so a typo in a script or a
// wrong concrete type is caught here, not on first launch. They run in both
// editor builds (plain `go test`, and `go test -tags mew`).

func TestRootEditorWindowBuildsFromProtocol(t *testing.T) {
	desktop := trinkets.NewDesktop()
	application := app.New(nil)

	w := newEditorWindow(desktop, application, []string{"--syntax=go", "notes.txt"}, true)
	if w == nil {
		t.Fatal("newEditorWindow returned nil")
	}
	if got := w.Title(); !strings.Contains(got, "notes.txt") {
		t.Errorf("window title = %q, want it to mention the file", got)
	}
	if w.Content() == nil {
		t.Fatal("root window has no content trinket (the editor)")
	}
}

// The host puts its root mew editor into solo mode: mew owns the whole display
// rather than floating as a window on a desktop. EnterSoloMode records solo even
// with no surface to reshape, so this holds headless.
func TestRootWindowEntersSoloMode(t *testing.T) {
	desktop := trinkets.NewDesktop()
	application := app.New(nil)

	root := startRootWindow(desktop, application, []string{"notes.txt"})
	if root == nil {
		t.Fatal("startRootWindow returned nil")
	}
	if !desktop.IsSolo() {
		t.Error("desktop should be in solo mode after startRootWindow")
	}
	if application.MainWindow() != root {
		t.Error("root window should be the app's main window")
	}
}

func TestScratchEditorWindowBuildsFromProtocol(t *testing.T) {
	desktop := trinkets.NewDesktop()
	application := app.New(nil)

	w := newEditorWindow(desktop, application, nil, false)
	if w == nil || w.Content() == nil {
		t.Fatal("scratch editor window did not build")
	}
}

// firstOperand titles the window from the first file-looking argument, skipping
// switches and +N. It is best-effort (cosmetic), but must at least skip leading
// switches and the +N form.
func TestFirstOperandSkipsSwitchesAndGoto(t *testing.T) {
	cases := []struct {
		argv []string
		want string
	}{
		{[]string{"--wordWrap", "+42", "main.go"}, "main.go"},
		{[]string{"a.txt", "b.txt"}, "a.txt"},
		{[]string{"--wordWrap"}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := firstOperand(c.argv); got != c.want {
			t.Errorf("firstOperand(%v) = %q, want %q", c.argv, got, c.want)
		}
	}
}

func TestStatusScriptExecutes(t *testing.T) {
	sections := buildStatus(
		`sb=new statusbar children={new section children={new span text="hello"}}`)
	if len(sections) == 0 {
		t.Fatal("buildStatus returned no sections")
	}
}

func TestMenusBuildAndRegisterActions(t *testing.T) {
	desktop := trinkets.NewDesktop()
	application := app.New(nil)

	menus := buildMenus(desktop, application)
	if len(menus) == 0 {
		t.Fatal("buildMenus returned no menus")
	}
	// The Raw Key Input action (and the others) must resolve to registered
	// handlers, or the menu items would dispatch into nothing.
	for _, action := range []string{"mew.edit.rawkey", "mew.window.new", "mew.help.about"} {
		if !application.Commands().Has(action) {
			t.Errorf("action %q was not registered", action)
		}
	}
}

// clearHostShortcuts frees the host accelerators (so the keys reach the mew
// editor) while leaving the actions dispatchable from the menu.
func TestClearHostShortcuts(t *testing.T) {
	// Seed the shipped defaults so the test is meaningful regardless of order.
	core.DefaultKeyBindings.SetDefaults()
	clearHostShortcuts()

	for _, action := range []string{
		core.ActionQuit, core.ActionAppHide, core.ActionAppHideOthers,
		core.ActionExitDesktop, core.ActionCut, core.ActionCopy,
		core.ActionPaste, core.ActionSelectAll,
	} {
		if keys := core.DefaultKeyBindings.Keys(action); len(keys) != 0 {
			t.Errorf("action %q still bound to %v after clearHostShortcuts", action, keys)
		}
	}
}

// SplitArgs separates meta flags from the launch argv.
func TestSplitArgs(t *testing.T) {
	launch, wantV, wantH := SplitArgs([]string{"--syntax=go", "-v", "a.txt", "-h", "+3"})
	if !wantV || !wantH {
		t.Errorf("wantVersion=%v wantHelp=%v, want both true", wantV, wantH)
	}
	want := []string{"--syntax=go", "a.txt", "+3"}
	if strings.Join(launch, " ") != strings.Join(want, " ") {
		t.Errorf("launch = %v, want %v", launch, want)
	}
}
