package trinkets

// What the display does with something that went wrong and has nowhere to go.
//
// The display is told things it cannot pass on: an optional include a bundle could
// not find, a complaint with no statement to carry it back across the wire. What
// used to happen to those was nothing at all, which is the worst answer available --
// a silent drop is indistinguishable from nothing having gone wrong.
//
// So they go in the log the Event Viewer is already keeping, as a row whose Event
// reads `Error`, keyed by whoever reported it and detailed with what they said. And
// they are HELD: the interesting ones happen while nobody is watching, because a
// name misspelled in a bundle goes wrong once, as the window is built.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/objects/window"
)

// loggedRows reads the viewer's log back as `event|key|detail` lines.
func loggedRows(v *eventViewer) []string {
	var out []string
	for _, it := range v.tree.RootItems() {
		out = append(out, strings.Join([]string{
			it.Value("event"), it.Value("key"), it.Value("detail"),
		}, "|"))
	}
	return out
}

// **An error reported while nobody is watching is there when somebody looks.**
func TestAnErrorReportedWithNoViewerOpenIsKept(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()

	d.LogError("bundle:papers", "papers/extra: no such include")

	// Held, and readable without a window at all.
	if got := d.Reported(); len(got) != 1 ||
		got[0] != "bundle:papers: papers/extra: no such include" {
		t.Fatalf("the desktop holds %v", got)
	}

	// And it is the first thing the viewer shows when it is opened afterwards.
	eventViewerItem(t, d).OnTriggered()
	v := d.eventViewer
	if v == nil {
		t.Fatal("the viewer did not open")
	}
	rows := loggedRows(v)
	if len(rows) != 1 {
		t.Fatalf("the viewer shows %v, want the one error", rows)
	}
	if got, want := rows[0],
		"Error|bundle:papers|papers/extra: no such include"; got != want {
		t.Errorf("the row reads %q, want %q", got, want)
	}
}

// **And one reported while it IS open goes straight in**, which is what a log
// window is for.
func TestAnErrorReportedWithTheViewerOpenShowsAtOnce(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()
	eventViewerItem(t, d).OnTriggered()
	v := d.eventViewer
	if v == nil {
		t.Fatal("the viewer did not open")
	}

	// Posted onto the platform thread, which with no platform under a test is this
	// one: the row is there when the call returns.
	d.LogError("source:papers", "sort={ size } on a column nothing is keyed by")

	rows := loggedRows(v)
	if len(rows) != 1 {
		t.Fatalf("the viewer shows %v, want the one error", rows)
	}
	if !strings.HasPrefix(rows[0], "Error|source:papers|sort=") {
		t.Errorf("the row reads %q", rows[0])
	}

	// **The mouse filter does not touch it.** What that hides is noise, and a
	// refusal is never noise.
	if v.showMouse {
		t.Fatal("this test means to run with the mouse filter on")
	}
	if got := len(loggedRows(v)); got != 1 {
		t.Errorf("the filter left %d rows", got)
	}
}

// Clear empties the log and not just the window: an error that came back on
// reopening would be a Clear that cleared nothing.
func TestClearingTheViewerForgetsWhatIsHeld(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()
	d.LogError("bundle:papers", "papers/extra: no such include")

	eventViewerItem(t, d).OnTriggered()
	v := d.eventViewer
	if v == nil || v.forget == nil {
		t.Fatal("the viewer opened without a way to forget what the desktop holds")
	}
	v.clearLog() // what the Clear button is wired to
	if got := len(loggedRows(v)); got != 0 {
		t.Errorf("after clearing, the window shows %d rows", got)
	}
	if got := d.Reported(); len(got) != 0 {
		t.Errorf("after clearing, the desktop still holds %v", got)
	}
}

// A report with nothing in it is not a report. Whoever noticed nothing says
// nothing, rather than filing an empty row for a reader to puzzle over.
func TestAnEmptyReasonIsNotReported(t *testing.T) {
	d := NewDesktop()
	d.LogError("source:papers", "")
	if got := d.Reported(); len(got) != 0 {
		t.Errorf("an empty reason was filed as %v", got)
	}
}
