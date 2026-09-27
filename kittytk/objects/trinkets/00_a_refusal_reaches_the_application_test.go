package trinkets

// A refusal reaching the application that built the view.
//
// The line is for the reader in front of the screen. The application is somewhere
// else entirely -- often on the other end of a socket -- and it is the one that can
// FIX a misspelled source name, so it hears about a refusal the way it hears about
// everything else that happens to one of its objects: an event on that object.
//
// And it may take it. A handler answering that it has dealt with the refusal is the
// shape a window's close handler has -- the caller is asked, and what it answers
// decides what happens next -- so an application with a status bar of its own says so
// per refusal rather than the view inferring it from a handler merely existing.

import (
	"testing"

	"github.com/phroun/kittytk/protocol"
)

// refusedList is a list over a source that will not state a sequence at all, built
// through the wire with everything subscribed.
func refusedList(t *testing.T, src string) (*ListView, *[]*protocol.Event) {
	t.Helper()
	f, events := buildWithEvents(t, nil, src)
	l, _ := f.targets[0].(*ListView)
	if l == nil {
		t.Fatalf("built a %T", f.targets[0])
	}
	return l, events
}

// **A refused list says so to its application.**
func TestARefusedListRaisesATroubleEvent(t *testing.T) {
	const why = "no such source: flies"
	l, events := refusedList(t, `lv=new listview`)
	l.SetSource(&shutSource{why: why})

	l.Item(0) // the question goes out and is refused
	got := eventsOfType(*events, "trouble")
	if len(got) != 1 {
		t.Fatalf("the list was refused and raised %d trouble events", len(got))
	}
	if text, _ := got[0].Text("text"); text != why {
		t.Errorf("it says %q, want what the source said", text)
	}
	// A sequence that could not be stated was not asked about a position.
	if at, _ := got[0].Int("at"); at != -1 {
		t.Errorf("it is about row %d, want -1", at)
	}
	if id, _ := got[0].Uint("trinket"); id != uint64(l.ObjectID()) {
		t.Errorf("it names object %d, want this list", id)
	}

	// **The event does not take the line away.** Being told and deciding where it
	// shows are two decisions, and an application that logs makes only the first.
	if !l.ShowsTrouble() {
		t.Error("raising the event turned the list's own line off")
	}
	l.SetBounds(l.Bounds())
	if !drewALine(t, l) {
		t.Error("the line is gone, and nothing said it had been handled")
	}
}

// **The same refusal twice is not two events.** A refused view asks again and is
// refused again; an application told every time would be told once a frame.
func TestOneRefusalIsOneEvent(t *testing.T) {
	l, events := refusedList(t, `lv=new listview`)
	l.SetSource(&shutSource{why: "no such source: flies"})

	l.Item(0)
	l.Item(1)
	l.Item(2)
	if got := eventsOfType(*events, "trouble"); len(got) != 1 {
		t.Errorf("one refusal raised %d events", len(got))
	}
}

// **And `trouble=false` is how an application says it wants no line at all**, which
// is standing rather than about one refusal.
func TestAnApplicationCanTurnTheLineOff(t *testing.T) {
	l, _ := refusedList(t, `lv=new listview trouble=false`)
	if l.ShowsTrouble() {
		t.Fatal("trouble=false left the line on")
	}
	l.SetSource(&shutSource{why: "no such source: flies"})
	l.Item(0)
	if !l.Trouble().Any() {
		t.Fatal("it was refused and holds nothing")
	}
	if drewALine(t, l) {
		t.Error("the line was drawn although the application asked for none")
	}
}

// **Handled is about THIS refusal, not about refusals.** A handler that takes one
// and not the next gets the line for the next, which is the whole difference between
// answering per refusal and saying once that you want no line.
func TestTakingOneRefusalDoesNotTakeTheNext(t *testing.T) {
	sulky := &sulkySource{why: "the first refusal", rows: 20}
	l := listOver(t, sulky, 8)
	l.SetOnTrouble(func(tr Trouble) bool { return tr.Reason == "the first refusal" })

	l.Item(0)
	if !l.Trouble().Any() {
		t.Fatal("it was refused and holds nothing")
	}
	if drewALine(t, l) {
		t.Error("the line was drawn although the handler took that one")
	}

	// A different refusal, which that handler does not take.
	sulky.why = "a refusal it does not know"
	l.Item(0)
	if got := l.Trouble().Reason; got != "a refusal it does not know" {
		t.Fatalf("the view holds %q, want the second refusal", got)
	}
	if !drewALine(t, l) {
		t.Error("the second refusal was not drawn, and nobody said they had it")
	}
}

// **An answer arriving is not news.** The event says something was refused; the
// refusal going away is the ordinary thing happening, and an application told about
// it would be told twice for every question that eventually worked.
func TestARefusalGoingAwayRaisesNothing(t *testing.T) {
	sulky := &sulkySource{why: "not yet", rows: 20, then: plainRows(20)}
	l, events := refusedList(t, `lv=new listview`)
	l.SetSource(sulky)

	l.Item(0)
	if got := eventsOfType(*events, "trouble"); len(got) != 1 {
		t.Fatalf("the refusal raised %d events", len(got))
	}

	sulky.relent = true
	if got := l.Item(0); got == nil || got.Text != "row00000" {
		t.Fatalf("the answer did not arrive: %v", got)
	}
	if l.Trouble().Any() {
		t.Fatal("records arrived and the view still holds a refusal")
	}
	if got := eventsOfType(*events, "trouble"); len(got) != 1 {
		t.Errorf("the refusal going away raised an event: %d in all", len(got))
	}
}
