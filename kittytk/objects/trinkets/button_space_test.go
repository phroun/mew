package trinkets

import (
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
)

// Space triggers like Enter - a brief press animation then the
// click - instead of latching pressed until a key-release event
// that no backend delivers.
func TestSpaceActivatesWithoutSticking(t *testing.T) {
	b := NewButton("ok")
	clicked := 0
	b.SetOnClick(func() { clicked++ })

	if !b.HandleKeyPress(core.KeyPressEvent{Key: " "}) {
		t.Fatal("space not handled")
	}
	if b.spacePressed {
		t.Error("space latched the pressed state")
	}
	if !b.animatingPress {
		t.Error("no press animation started")
	}

	time.Sleep(350 * time.Millisecond)
	if clicked != 1 {
		t.Errorf("clicked %d times, want 1", clicked)
	}
	if b.animatingPress {
		t.Error("still showing pressed after the animation window")
	}
}
