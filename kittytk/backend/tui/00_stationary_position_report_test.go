package tui

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A position report raises a move only when the pointer is somewhere it was
// not.
//
// Mouse input arrives as two keys -- "Mouse@x,y" and then the action it
// belongs to -- so every click carries a position report of its own. Where the
// pointer is already there, that report is the coordinate the action resolves
// against, not a move to somewhere else.
func TestAStationaryPositionReportIsNotAMove(t *testing.T) {
	b := &TUIBackend{
		metrics:    core.DefaultCellMetrics(),
		eventQueue: make(chan core.Event, 8),
	}

	drain := func() []core.Event {
		var out []core.Event
		for {
			select {
			case ev := <-b.eventQueue:
				out = append(out, ev)
			default:
				return out
			}
		}
	}

	b.handleKey("Mouse@10,5")
	if got := drain(); len(got) != 1 {
		t.Fatalf("the first report raised %d events, want the one move", len(got))
	}

	b.handleKey("Mouse@10,5")
	if got := drain(); len(got) != 0 {
		t.Errorf("a report of the position the pointer already had raised %v", got)
	}

	b.handleKey("Mouse@11,5")
	if got := drain(); len(got) != 1 {
		t.Errorf("a report of a new position raised %d events, want the one move", len(got))
	}
}

// The origin is a position like any other, so the first report of it moves.
func TestTheFirstPositionReportMovesEvenAtTheOrigin(t *testing.T) {
	b := &TUIBackend{
		metrics:    core.DefaultCellMetrics(),
		eventQueue: make(chan core.Event, 8),
	}

	b.handleKey("Mouse@0,0")
	select {
	case ev := <-b.eventQueue:
		if _, ok := ev.(core.MouseMoveEvent); !ok {
			t.Errorf("dispatched as %T, want MouseMoveEvent", ev)
		}
	default:
		t.Error("the first position report raised no move")
	}
}
