package input

import (
	"bytes"
	"io"
	"testing"
)

// A DSR reply (ESC[row;colR) must surface as a distinct "CPR:<row>;<col>" key
// event — the flipBidiForHost probe's reply path — while the legacy modified-F3
// form (first parameter 1) keeps parsing as a function key. Requires
// direct-key-handler >= v0.3.6.
func TestCursorPositionReportEvent(t *testing.T) {
	input := bytes.NewReader([]byte("\x1b[2;3R" + "\x1b[1;5R" + "b"))
	kh := NewKeyboardHandler(input, io.Discard)
	if err := kh.handler.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer kh.Stop()

	evs := make(chan InputEvent, 16)
	go func() {
		for {
			evs <- kh.GetEvent()
		}
	}()

	// The DSR reply arrives as a CPR event with row and column intact.
	ev := nextEvent(t, evs)
	if ev.Key != "CPR:2;3" {
		t.Fatalf("DSR reply: got key %q, want %q", ev.Key, "CPR:2;3")
	}
	// The ambiguous first-param-1 form stays a modified F3 (Ctrl-F3).
	ev = nextEvent(t, evs)
	if ev.Key != "C-F3" {
		t.Fatalf("legacy modified F3: got key %q, want %q", ev.Key, "C-F3")
	}
	// Ordinary typing still flows.
	ev = nextEvent(t, evs)
	if ev.Key != "b" {
		t.Fatalf("trailing key: got %q, want b", ev.Key)
	}
}
