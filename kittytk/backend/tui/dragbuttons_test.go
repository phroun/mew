package tui

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// Motion carries the button being held, and buttonless motion carries none.
//
// direct-key-handler names the button in the drag event and reserves plain
// "MouseDrag" for motion with NO button — the buttonless tracking a terminal
// sends under ?1003. Buttons went unset for all four, so every motion looked
// button-free to a trinket. The visible symptom was a scrollbar: its hover
// test is exactly "no button held", so a drag passing over one lit it up
// instead of clearing it.
func TestDragEventsCarryTheHeldButton(t *testing.T) {
	b := &TUIBackend{
		metrics:    core.DefaultCellMetrics(),
		eventQueue: make(chan core.Event, 8),
	}

	move := func(key string) core.MouseMoveEvent {
		t.Helper()
		b.handleKey(key)
		select {
		case ev := <-b.eventQueue:
			mm, ok := ev.(core.MouseMoveEvent)
			if !ok {
				t.Fatalf("%s: dispatched as %T, want MouseMoveEvent", key, ev)
			}
			return mm
		default:
			t.Fatalf("%s: no event dispatched", key)
			return core.MouseMoveEvent{}
		}
	}

	for _, c := range []struct {
		key  string
		want core.MouseButton
		what string
	}{
		{"MouseLeftDrag@10,5", core.LeftButton, "left drag"},
		{"MouseMiddleDrag@10,5", core.MiddleButton, "middle drag"},
		{"MouseRightDrag@10,5", core.RightButton, "right drag"},
		{"MouseDrag@10,5", 0, "buttonless motion, which is what ?1003 sends"},
	} {
		if got := move(c.key).Buttons; got != c.want {
			t.Errorf("%s: Buttons = %v, want %v", c.what, got, c.want)
		}
	}
}
