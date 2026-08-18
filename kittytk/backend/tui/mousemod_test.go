package tui

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// Modifier-prefixed mouse pseudo-keys must dispatch as MOUSE events with
// their modifiers attached — not fall through to the keyboard path or be
// dropped as unknown. Terminals vary: iTerm2 forwards shifted/ctrl'd clicks
// as "S-MouseRight" / "C-MouseRight", stock Terminal sends them
// unprefixed; all three must produce the same press with the right bits.
func TestModifiedMouseKeysDispatchAsMouse(t *testing.T) {
	b := &TUIBackend{
		metrics:    core.DefaultCellMetrics(),
		eventQueue: make(chan core.Event, 8),
	}

	// The position report ahead of an action is the pointer arriving there,
	// and dispatches as a move — so the action is the event behind it.
	last := func(what string) core.Event {
		t.Helper()
		var ev core.Event
		for {
			select {
			case e := <-b.eventQueue:
				ev = e
			default:
				if ev == nil {
					t.Fatalf("%s: no event dispatched (dropped)", what)
				}
				return ev
			}
		}
	}

	press := func(key string) core.MousePressEvent {
		t.Helper()
		b.handleKey("Mouse@10,5")
		b.handleKey(key)
		ev := last(key)
		mp, ok := ev.(core.MousePressEvent)
		if !ok {
			t.Fatalf("%s: dispatched as %T, want MousePressEvent", key, ev)
		}
		return mp
	}

	if ev := press("MouseRight"); ev.Button != core.RightButton || ev.Modifiers != 0 {
		t.Fatalf("plain right press: %+v", ev)
	}
	if ev := press("S-MouseRight"); ev.Button != core.RightButton || ev.Modifiers&core.ShiftModifier == 0 {
		t.Fatalf("shifted right press must carry ShiftModifier: %+v", ev)
	}
	if ev := press("C-MouseRight"); ev.Button != core.RightButton || ev.Modifiers&core.ControlModifier == 0 {
		t.Fatalf("ctrl right press must carry ControlModifier: %+v", ev)
	}
	if ev := press("S-MouseLeft"); ev.Button != core.LeftButton || ev.Modifiers&core.ShiftModifier == 0 {
		t.Fatalf("shifted left press must carry ShiftModifier: %+v", ev)
	}

	// A prefixed DRAG (position embedded in the action) dispatches as a
	// move with modifiers.
	b.handleKey("S-MouseDragLeft@12,6")
	dragEv := last("shifted drag")
	if mv, ok := dragEv.(core.MouseMoveEvent); !ok || mv.Modifiers&core.ShiftModifier == 0 {
		t.Fatalf("shifted drag: %T %+v", dragEv, dragEv)
	}

	// Horizontal wheel events dispatch (they were previously unknown).
	b.handleKey("Mouse@10,5")
	b.handleKey("MouseScrollLeft")
	wheelEv := last("scroll left")
	if wh, ok := wheelEv.(core.MouseWheelEvent); !ok || wh.DeltaX != -1 {
		t.Fatalf("scroll left: %T %+v", wheelEv, wheelEv)
	}
}
