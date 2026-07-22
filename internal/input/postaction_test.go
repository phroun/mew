package input

import (
	"bytes"
	"testing"
	"time"
)

// PostAction on the terminal-backed source surfaces the closure as a Do
// event from GetEvent, so it runs on the editor's consuming goroutine.
func TestKeyboardHandlerPostAction(t *testing.T) {
	kh := NewKeyboardHandler(bytes.NewReader(nil), &bytes.Buffer{})

	ran := false
	if !kh.PostAction(func() { ran = true }) {
		t.Fatal("PostAction should accept the closure")
	}

	done := make(chan InputEvent, 1)
	go func() { done <- kh.GetEvent() }()
	select {
	case ev := <-done:
		if ev.Do == nil {
			t.Fatalf("expected a Do event, got %+v", ev)
		}
		ev.Do()
		if !ran {
			t.Fatal("the delivered closure must be the posted one")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("posted action never surfaced from GetEvent")
	}

	if kh.PostAction(nil) {
		t.Fatal("a nil action must be refused")
	}
}

// EventFeed.PostAction delivers in order with keys and stops after Close.
func TestEventFeedPostAction(t *testing.T) {
	f := NewEventFeed()
	if !f.SendKey("a") {
		t.Fatal("SendKey")
	}
	ran := false
	if !f.PostAction(func() { ran = true }) {
		t.Fatal("PostAction should accept the closure")
	}

	if ev := f.GetEvent(); ev.Key != "a" {
		t.Fatalf("first event should be the key: %+v", ev)
	}
	ev := f.GetEvent()
	if ev.Do == nil {
		t.Fatalf("second event should be the action: %+v", ev)
	}
	ev.Do()
	if !ran {
		t.Fatal("the delivered closure must be the posted one")
	}

	f.Close()
	if f.PostAction(func() {}) {
		t.Fatal("PostAction after Close must report false")
	}
}
