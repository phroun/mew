package main

// The MDI documents: windows whose close is a question, and whose application
// answers it.
//
// They are the other half of the Sulking Window, and what is checked here is what
// tells the two apart. Both subscribe; only one answers. A document that subscribes
// and then loses its answer looks, from the display, exactly like the window that
// was written to say nothing -- five seconds of nothing, then the display asking
// somebody whether to force it. So the answer is what these tests are about, and
// each branch of it: yes closes, no keeps.

import (
	"os"
	"strings"
	"testing"

	"github.com/phroun/kittytk/client"
)

// funcBody is one function's source out of one of the demo's files.
//
// Scoped to a function because the file is not the unit here: closing.go holds a
// window that answers and a window that deliberately does not, and "does this file
// mention Decide" cannot tell which one does.
func funcBody(t *testing.T, file, decl string) string {
	t.Helper()
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("reading %s: %v", file, err)
	}
	body := string(src)
	start := strings.Index(body, decl)
	if start < 0 {
		t.Fatalf("%s has no %s", file, decl)
	}
	body = body[start:]
	if end := strings.Index(body, "\n}\n"); end > 0 {
		body = body[:end]
	}
	return body
}

// Subscribed AND answered, in both directions. Either half missing is a window
// that cannot be closed the way it says it can.
func TestADocumentIsAskedAndAnswers(t *testing.T) {
	confirm := funcBody(t, "closing.go", "func (a *app) confirmClose(")
	if !strings.Contains(confirm, `win.On("window_closing"`) {
		t.Error("the close is not subscribed to, so the document closes at once and asks nobody")
	}
	if !strings.Contains(confirm, "a.askBeforeClosing(") {
		t.Error("nothing puts the question on the screen")
	}
	// The decision the event names, not one remembered from somewhere: a second
	// document asking at the same time is a second decision.
	if !strings.Contains(confirm, "ev.Uint(wire.DecisionField)") {
		t.Error("the decision is not read off the event, so the answer would be to the wrong question")
	}

	ask := funcBody(t, "closing.go", "func (a *app) askBeforeClosing(")
	if !strings.Contains(ask, "a.conn.Decide(decision, allow)") {
		t.Error("the dialog's answer never reaches the decision, so the close times out and the display asks about a window that IS being asked about")
	}
	// And the failure to ask at all is answered too. Saying nothing there is the
	// Sulking Window by accident.
	if !strings.Contains(ask, "a.conn.Decide(decision, false)") {
		t.Error("a build that fails leaves the question unanswered rather than keeping the window")
	}
}

// Both MDI spawns wire it. The bounded child is an MDI child too, and it is the one
// easy to forget: it is spawned by a different menu item through a different script.
func TestEveryMDIChildIsAskedAbout(t *testing.T) {
	for _, fn := range []string{
		"func (a *app) spawnMDIChild(",
		"func (a *app) spawnBoundedMDIChild(",
	} {
		body := funcBody(t, "wire.go", fn)
		if !strings.Contains(body, "a.confirmClose(") {
			t.Errorf("%s closes without asking", fn)
		}
	}
}

// Which way each answer goes, and -- the one that matters -- which way everything
// that is not an answer goes.
func TestOnlyYesClosesTheDocument(t *testing.T) {
	if !closeAnswer("yes") {
		t.Error("answering yes does not close the document, so it cannot be closed at all")
	}
	// A dialog can end in more ways than it has buttons: dismissed, or finished
	// with a word this one never offered. None of those is permission.
	for _, result := range []string{"no", "cancel", "none", ""} {
		if closeAnswer(result) {
			t.Errorf("%q closes the document, so work is lost to an answer nobody gave", result)
		}
	}
}

// What is in the document is the application's own knowledge, and the reason the
// display asks instead of deciding. So the question changes with it.
func TestTheQuestionSaysWhatIsAtStake(t *testing.T) {
	empty := closeConfirmScript("cq1", "Document 1", false)
	typed := closeConfirmScript("cq1", "Document 1", true)
	if empty == typed {
		t.Fatal("a document with work in it is asked about the same way as an empty one")
	}
	for _, want := range []string{"Document 1", "yes", "no", "messagebox"} {
		if !strings.Contains(empty, want) {
			t.Errorf("the question does not mention %q", want)
		}
	}
	if !strings.Contains(typed, "icon=warning") {
		t.Error("losing typed-in work is not warned about")
	}
	if strings.Contains(empty, "icon=warning") {
		t.Error("an empty document is warned about, which teaches people to dismiss warnings")
	}
	// The name is the window's, because the dialog is the only thing on the screen
	// that says WHICH document is being closed.
	if !strings.Contains(closeConfirmScript("cq1", "Bounded 3", false), "Bounded 3") {
		t.Error("the question does not name the window it is about")
	}
}

// Two documents can be asking at once -- one closed while the other's question is
// still on the screen -- so the dialogs cannot share a key.
func TestTwoDocumentsCanAskAtOnce(t *testing.T) {
	first := closeConfirmScript("cq1", "Document 1", false)
	second := closeConfirmScript("cq2", "Document 2", false)
	if !strings.HasPrefix(first, "cq1=") || !strings.HasPrefix(second, "cq2=") {
		t.Fatalf("the questions are not keyed apart:\n%s\n%s", first, second)
	}
	// And the counter that keys them is not the document counter: a document's
	// close can be asked about more than once (answer No, press [x] again).
	ask := funcBody(t, "closing.go", "func (a *app) askBeforeClosing(")
	if !strings.Contains(ask, "a.closeAsks++") {
		t.Error("the questions are not counted, so two of them collide on a key")
	}
}

// And the whole thing over a live display: the document builds, its text field is
// where the wiring says it is, and the question the display is asked to put up is
// one it accepts.
func TestTheDocumentAndItsQuestionBuildOverService(t *testing.T) {
	sock, stop := startService(t)
	defer stop()

	conn, err := client.DialWith(sock, "KittyTK Demo", client.DialOptions{MultiWindow: true})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Build(mainBuildScript()); err != nil {
		t.Fatalf("build main: %v", err)
	}
	child, err := conn.Build(mdiChildScript(1))
	if err != nil {
		t.Fatalf("mdi child: %v", err)
	}
	// Every name the spawn addresses. A name the script does not surface is id 0,
	// and id 0 is quiet: the handle's subscription is skipped without a word, so
	// the close question would never be asked and nothing would say so.
	for _, name := range []string{"wwin", "wnew", "wclose", "wtext"} {
		if child.ID(name) == 0 {
			t.Errorf("the document surfaces no %q, so what the wiring does with it goes nowhere", name)
		}
	}

	ui, err := conn.Build(closeConfirmScript("cq1", "Document 1", true))
	if err != nil {
		t.Fatalf("the close question the display would be asked to put up: %v", err)
	}
	if ui.ID("cq1") == 0 {
		t.Fatal("the question built nothing addressable, so its answer could not be subscribed to")
	}
	// Subscribable, which is the half that carries the answer back.
	if err := ui.Object("cq1").Set("title=\"Close Document 1\""); err != nil {
		t.Errorf("the question is not a live object: %v", err)
	}
}
