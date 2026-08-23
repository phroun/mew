package trinkets

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
)

// Space triggers like Enter - a brief press animation then the
// click - instead of latching pressed until a key-release event
// that no backend delivers.
//
// The headless activation path fires the click from a timer goroutine, so the
// click counter is atomic and the post-animation state is read through
// animatingPress.Load(): both give the test a happens-before edge with that
// goroutine instead of racing it.
func TestSpaceActivatesWithoutSticking(t *testing.T) {
	b := NewButton("ok")
	var clicked atomic.Int32
	b.SetOnClick(func() { clicked.Add(1) })

	if !b.HandleKeyPress(core.KeyPressEvent{Key: "Space"}) {
		t.Fatal("space not handled")
	}
	if b.spacePressed {
		t.Error("space latched the pressed state")
	}
	if !b.animatingPress.Load() {
		t.Error("no press animation started")
	}

	time.Sleep(350 * time.Millisecond)
	if got := clicked.Load(); got != 1 {
		t.Errorf("clicked %d times, want 1", got)
	}
	if b.animatingPress.Load() {
		t.Error("still showing pressed after the animation window")
	}
}
