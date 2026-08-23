package mewhost

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/app"
	"github.com/phroun/kittytk/objects/trinkets"
)

// The welcome is what the window HOLDS, so Try is what puts the editor back.
// Nothing of the session runs until then - the editor trinket never paints, so
// no mew session starts behind the question.
func TestWelcomeTryRestoresTheEditor(t *testing.T) {
	opened, installed := 0, 0
	c := newWelcomeContent([]string{"hi"}, func() { installed++ }, func() { opened++ })

	c.answer(c.onTry)

	if opened != 1 {
		t.Errorf("Try ran the show-editor action %d times, want 1", opened)
	}
	if installed != 0 {
		t.Error("Try must not install")
	}
}

// Install is the other answer, and it does NOT open the editor: the copy being
// installed is the one that will run, not this one.
func TestWelcomeInstallDoesNotOpenTheEditor(t *testing.T) {
	opened, installed := 0, 0
	c := newWelcomeContent([]string{"hi"}, func() { installed++ }, func() { opened++ })

	c.answer(c.onInstall)

	if installed != 1 {
		t.Errorf("Install ran %d times, want 1", installed)
	}
	if opened != 0 {
		t.Error("Install must not open an editor this process is about to abandon")
	}
}

// The question is answered once, whichever route answers it: a stray second
// press after Install must not also show an editor the process is leaving.
func TestWelcomeAnswersOnlyOnce(t *testing.T) {
	opened, installed := 0, 0
	c := newWelcomeContent([]string{"hi"}, func() { installed++ }, func() { opened++ })

	c.answer(c.onInstall)
	c.answer(c.onTry)
	c.answer(c.onInstall)

	if installed != 1 || opened != 0 {
		t.Errorf("install=%d open=%d, want the first answer alone", installed, opened)
	}
}

// Both spellings of accept reach Install, and Escape reaches Try. Return is the
// home-row key, Enter the keypad one; the content answers to either.
func TestWelcomeKeys(t *testing.T) {
	for _, key := range []string{"Return", "Enter"} {
		opened, installed := 0, 0
		c := newWelcomeContent([]string{"hi"}, func() { installed++ }, func() { opened++ })
		if !c.HandleKeyPress(core.KeyPressEvent{Key: key}) {
			t.Errorf("%s was not handled", key)
		}
		if installed != 1 || opened != 0 {
			t.Errorf("%s: install=%d open=%d, want the primary action", key, installed, opened)
		}
	}

	opened, installed := 0, 0
	c := newWelcomeContent([]string{"hi"}, func() { installed++ }, func() { opened++ })
	if !c.HandleKeyPress(core.KeyPressEvent{Key: "Escape"}) {
		t.Error("Escape was not handled")
	}
	if opened != 1 || installed != 0 {
		t.Errorf("Escape: install=%d open=%d, want the dismissal", installed, opened)
	}
}

// With no welcome to show - the TUI host, or an already-installed copy - the
// window keeps the editor and the title it was built with.
func TestNoWelcomeLeavesTheWindowAlone(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 600)
	desktop := trinkets.NewDesktop()
	desktop.SetBackend(px)
	application := app.New(nil)

	w := newEditorWindow(desktop, application, nil)
	content := w.Content()
	title := w.Title()

	if maybeShowWelcome(desktop, application, w, nil, false /* graphical */, func() {}) {
		t.Fatal("the TUI host has no first-run welcome")
	}
	if w.Content() != content {
		t.Error("the window's content was replaced although no welcome was shown")
	}
	if w.Title() != title {
		t.Error("the window was re-titled although no welcome was shown")
	}
}

// The takeover itself: the welcome replaces what the window holds, and Try
// hands it straight back - same window, same editor, same title, nothing
// behind either of them at any point.
func TestWelcomeTakesTheWindowAndGivesItBack(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 600)
	desktop := trinkets.NewDesktop()
	desktop.SetBackend(px)
	application := app.New(nil)

	w := newEditorWindow(desktop, application, nil)
	editor := w.Content()
	title := w.Title()

	declared := 0
	c := showWelcomeIn(desktop, application, w, nil, func() { declared++ })

	if w.Content() != core.Trinket(c) {
		t.Fatal("the welcome did not take the window over")
	}
	if w.Title() == title {
		t.Error("the window should say what it is showing")
	}

	c.answer(c.onTry) // Try (Post runs inline with no platform behind it)

	if w.Content() != editor {
		t.Error("Try should have given the window back to the editor it was built with")
	}
	if w.Title() != title {
		t.Errorf("window title = %q, want the editor's own %q", w.Title(), title)
	}
	if declared != 1 {
		t.Errorf("mew's menus were declared %d times, want once - with the editor", declared)
	}
}

// The menus are held back with the editor: while the welcome has the window the
// app has declared none, so the bar it carries is the minimal set the desktop
// synthesizes - its app menu, with Quit - which is all a welcome needs.
func TestWelcomeCarriesNoDeclaredMenus(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(800, 600)
	desktop := trinkets.NewDesktop()
	desktop.SetBackend(px)
	application := app.New(nil)

	w := newEditorWindow(desktop, application, nil)
	showWelcomeIn(desktop, application, w, nil, func() {
		application.SetMenuBarContent(buildMenus(desktop, application, false))
	})

	if len(application.MenuBarContent()) != 0 {
		t.Fatal("the welcome should be carrying no declared menus")
	}
}

// Install is the answer that declares nothing: the menus, like the editor,
// belong to a session this process is abandoning. (The action itself is not
// run here - it would install mew on the machine running the tests.)
func TestWelcomeInstallDeclaresNothing(t *testing.T) {
	declared := 0
	c := newWelcomeContent([]string{"hi"}, func() {}, func() { declared++ })

	c.answer(c.onInstall)

	if declared != 0 {
		t.Error("Install must not bring in the menus of a session it is abandoning")
	}
}
