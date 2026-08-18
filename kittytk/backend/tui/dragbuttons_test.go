package tui

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// Motion carries the button being held, and buttonless motion carries none.
//
// direct-key-handler names the button in the drag event; motion with NO button
// — the buttonless tracking a terminal sends under ?1003 — arrives as the bare
// position report, since that is all such a move is. A trinket's hover test is
// exactly "no button held", so a drag passing over a scrollbar must clear its
// hover rather than light it.
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
		{"MouseDragLeft@10,5", core.LeftButton, "left drag"},
		{"MouseDragMiddle@10,5", core.MiddleButton, "middle drag"},
		{"MouseDragRight@10,5", core.RightButton, "right drag"},
		{"Mouse@10,5", 0, "buttonless motion, which is what ?1003 sends"},
	} {
		if got := move(c.key).Buttons; got != c.want {
			t.Errorf("%s: Buttons = %v, want %v", c.what, got, c.want)
		}
	}
}
