package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/input"
)

// A host that received a paste hands it to the editor, and it goes down the
// paste path rather than in as a command built around the text.
//
// Nothing carried one before: a host could Execute commands and read options,
// so pasted text had no way in at all — which is what left a bracketed paste
// the toolkit read off the wire stopping at the focused editor.
func TestHostPortCarriesAPaste(t *testing.T) {
	feed := input.NewEventFeed()
	defer feed.Close()

	var p HostPort
	p.bind(nil, nil, nil, nil, func(text string) bool {
		return feed.SendPaste([]byte(text), true)
	})

	if !p.Paste("hello") {
		t.Fatal("the port refused a paste it was bound for")
	}
	ev := feed.GetEvent()
	if ev.Paste == nil {
		t.Fatalf("delivered %+v, want a paste", ev)
	}
	if got := string(ev.Paste.Content); got != "hello" {
		t.Errorf("pasted %q, want %q", got, "hello")
	}
	if !ev.Paste.IsFinal {
		t.Error("the chunk is not marked final, so the paste's undo revision " +
			"would never be closed")
	}
}

// An unbound port refuses rather than dropping the text, and so does an empty
// paste: a host cannot tell the two apart from silence.
func TestAnUnboundPortRefusesAPaste(t *testing.T) {
	var p HostPort
	if p.Paste("hello") {
		t.Error("an unbound port claimed to have delivered a paste")
	}

	feed := input.NewEventFeed()
	defer feed.Close()
	p.bind(nil, nil, nil, nil, func(text string) bool {
		return feed.SendPaste([]byte(text), true)
	})
	if p.Paste("") {
		t.Error("an empty paste was claimed as delivered")
	}
}

// The paste rides the same stream as the keys, so text pasted between two
// keystrokes lands between them rather than before or after both.
func TestAPasteKeepsItsPlaceInTheKeyStream(t *testing.T) {
	feed := input.NewEventFeed()
	defer feed.Close()

	var p HostPort
	p.bind(nil, nil, nil, nil, func(text string) bool {
		return feed.SendPaste([]byte(text), true)
	})

	feed.SendKey("a")
	p.Paste("MID")
	feed.SendKey("b")

	var order []string
	for i := 0; i < 3; i++ {
		ev := feed.GetEvent()
		switch {
		case ev.Paste != nil:
			order = append(order, string(ev.Paste.Content))
		default:
			order = append(order, ev.Key)
		}
	}
	if got := strings.Join(order, ","); got != "a,MID,b" {
		t.Errorf("delivered %q, want %q", got, "a,MID,b")
	}
}
