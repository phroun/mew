package core

import (
	"testing"
	"time"
)

// A claimed gesture keeps receiving events (translated to the
// claimant's space) despite pointer drift; it releases only after a
// stop AND a move.
func TestWheelGestureLatch(t *testing.T) {
	t.Cleanup(ResetWheelGesture)
	got := []Unit{}
	e := MouseWheelEvent{X: 10, Y: 20, ScreenX: 110, ScreenY: 220}
	ClaimWheelGesture(e, func(ev MouseWheelEvent) bool {
		got = append(got, ev.Y)
		return true
	})

	// Pointer drifted 40 units down mid-gesture: still latched, and
	// translated into the claimant's space (screen 260 -> local 60).
	WheelPointerMoved()
	next := MouseWheelEvent{ScreenX: 110, ScreenY: 260}
	if !DeliverLatchedWheel(next) {
		t.Fatal("gesture not latched")
	}
	if len(got) != 1 || got[0] != 60 {
		t.Fatalf("latched delivery got %v, want [60]", got)
	}

	// Stop (pause) alone does not release without a move...
	wheelGesture.mu.Lock()
	wheelGesture.last = time.Now().Add(-time.Second)
	wheelGesture.mu.Unlock()
	if !DeliverLatchedWheel(next) {
		t.Fatal("pause without pointer move released the latch")
	}

	// ...but pause + move does: next gesture re-targets.
	wheelGesture.mu.Lock()
	wheelGesture.last = time.Now().Add(-time.Second)
	wheelGesture.mu.Unlock()
	WheelPointerMoved()
	if DeliverLatchedWheel(next) {
		t.Fatal("clear stop + move did not release the latch")
	}
}
