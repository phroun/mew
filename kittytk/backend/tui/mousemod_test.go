package tui

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// Modifier-prefixed mouse pseudo-keys must dispatch as MOUSE events with
// their modifiers attached — not fall through to the keyboard path or be
// dropped as unknown. Terminals vary: iTerm2 forwards shifted/ctrl'd clicks
// as "S-MouseRightPress" / "C-MouseRightPress", stock Terminal sends them
// unprefixed; all three must produce the same press with the right bits.
func TestModifiedMouseKeysDispatchAsMouse(t *testing.T) {
	b := &TUIBackend{
		metrics:    core.DefaultCellMetrics(),
		eventQueue: make(chan core.Event, 8),
	}

	press := func(key string) core.MousePressEvent {
		t.Helper()
		b.handleKey("Mouse@10,5")
		b.handleKey(key)
		select {
		case ev := <-b.eventQueue:
			mp, ok := ev.(core.MousePressEvent)
			if !ok {
				t.Fatalf("%s: dispatched as %T, want MousePressEvent", key, ev)
			}
			return mp
		default:
			t.Fatalf("%s: no event dispatched (dropped)", key)
			return core.MousePressEvent{}
		}
	}

	if ev := press("MouseRightPress"); ev.Button != core.RightButton || ev.Modifiers != 0 {
		t.Fatalf("plain right press: %+v", ev)
	}
	if ev := press("S-MouseRightPress"); ev.Button != core.RightButton || ev.Modifiers&core.ShiftModifier == 0 {
		t.Fatalf("shifted right press must carry ShiftModifier: %+v", ev)
	}
	if ev := press("C-MouseRightPress"); ev.Button != core.RightButton || ev.Modifiers&core.ControlModifier == 0 {
		t.Fatalf("ctrl right press must carry ControlModifier: %+v", ev)
	}
	if ev := press("S-MouseLeftPress"); ev.Button != core.LeftButton || ev.Modifiers&core.ShiftModifier == 0 {
		t.Fatalf("shifted left press must carry ShiftModifier: %+v", ev)
	}

	// A prefixed DRAG (position embedded in the action) dispatches as a
	// move with modifiers.
	b.handleKey("S-MouseLeftDrag@12,6")
	select {
	case ev := <-b.eventQueue:
		mv, ok := ev.(core.MouseMoveEvent)
		if !ok || mv.Modifiers&core.ShiftModifier == 0 {
			t.Fatalf("shifted drag: %T %+v", ev, ev)
		}
	default:
		t.Fatal("shifted drag was dropped")
	}

	// Horizontal wheel events dispatch (they were previously unknown).
	b.handleKey("Mouse@10,5")
	b.handleKey("MouseScrollLeft")
	select {
	case ev := <-b.eventQueue:
		wh, ok := ev.(core.MouseWheelEvent)
		if !ok || wh.DeltaX != -1 {
			t.Fatalf("scroll left: %T %+v", ev, ev)
		}
	default:
		t.Fatal("horizontal wheel was dropped")
	}
}
