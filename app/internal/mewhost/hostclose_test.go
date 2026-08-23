package mewhost

import (
	"testing"

	"github.com/phroun/kittytk/objects/app"
	"github.com/phroun/kittytk/objects/trinkets"
)

// fakeSession stands in for the editor trinket a window asks about closing.
type fakeSession struct {
	unsaved  bool
	takes    bool // does the session take the question on when asked
	asked    int
	askedFor bool
}

func (f *fakeSession) HasUnsavedWork() bool { f.askedFor = true; return f.unsaved }
func (f *fakeSession) RequestClose() bool   { f.asked++; return f.takes }

// Nothing at stake: the window closes, and the session is never bothered.
func TestCloseWithNothingAtStakeGoesThrough(t *testing.T) {
	ed := &fakeSession{}
	if !allowEditorWindowClose(false, ed) {
		t.Error("a session holding no unsaved work should close outright")
	}
	if ed.asked != 0 {
		t.Error("nothing was at stake; the session should not have been asked to close")
	}
}

// Unsaved work turns the close into a question. The window refuses NOW - the
// answer is the user's, and it arrives later as the session ending.
func TestCloseWithUnsavedWorkIsRefusedAndHandedToTheSession(t *testing.T) {
	ed := &fakeSession{unsaved: true, takes: true}
	if allowEditorWindowClose(false, ed) {
		t.Error("a window holding unsaved work must refuse to close")
	}
	if ed.asked != 1 {
		t.Errorf("the session should have been asked exactly once, got %d", ed.asked)
	}
}

// The end of the session is what closes the window for real: by then the work
// has been dealt with, so the close it performs is never turned away.
func TestCloseAfterTheSessionEndedGoesThrough(t *testing.T) {
	ed := &fakeSession{unsaved: true, takes: true}
	if !allowEditorWindowClose(true, ed) {
		t.Fatal("the session's own closing must not be refused")
	}
	if ed.askedFor || ed.asked != 0 {
		t.Error("a finished session should not be asked anything")
	}
}

// A window whose session never started (or has already gone) has nobody to ask
// - refusing would leave it unclosable, so it closes.
func TestCloseWithNoLiveSessionGoesThrough(t *testing.T) {
	ed := &fakeSession{unsaved: true, takes: false}
	if !allowEditorWindowClose(false, ed) {
		t.Error("with no session to ask, the close must not be refused")
	}
}

// The wiring itself: every editor window built by the host carries the close
// handler, in both editor builds. With no live session behind it (nothing has
// run here) the handler lets the close through rather than stranding it.
func TestEditorWindowsCarryTheCloseHandler(t *testing.T) {
	desktop := trinkets.NewDesktop()
	application := app.New(nil)

	w := newEditorWindow(desktop, application, nil)
	if !w.Close() {
		t.Error("a window with no live session must not refuse its close")
	}
}
