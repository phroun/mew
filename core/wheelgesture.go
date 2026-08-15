package core

import (
	"sync"
	"time"
)

// Wheel gesture latching: trackpads emit a stream of small wheel
// events, and the pointer may drift over other scrollables mid
// gesture. The trinket that first consumes a wheel event claims the
// gesture; subsequent wheel events go straight to it. The claim
// releases only after a clear stop (a pause in wheel events) AND a
// pointer move - the next gesture then re-targets under the cursor.
var wheelGesture struct {
	mu      sync.Mutex
	deliver func(MouseWheelEvent) bool
	offX    Unit // screen -> claimant-local translation
	offY    Unit
	last    time.Time
	moved   bool // pointer moved since the last wheel event
}

// wheelGestureStop is the pause that counts as a "clear stop".
const wheelGestureStop = 300 * time.Millisecond

// ClaimWheelGesture is called by a scrollable that consumed a wheel
// event: handler receives later events of the same gesture already
// translated to the claimant's coordinate space. The event must be
// the one just consumed (its Screen fields carry the untranslated
// position).
func ClaimWheelGesture(e MouseWheelEvent, handler func(MouseWheelEvent) bool) {
	wheelGesture.mu.Lock()
	wheelGesture.deliver = handler
	wheelGesture.offX = e.ScreenX - e.X
	wheelGesture.offY = e.ScreenY - e.Y
	wheelGesture.last = time.Now()
	wheelGesture.moved = false
	wheelGesture.mu.Unlock()
}

// WheelPointerMoved records pointer motion (gesture-stop detection).
func WheelPointerMoved() {
	wheelGesture.mu.Lock()
	wheelGesture.moved = true
	wheelGesture.mu.Unlock()
}

// DeliverLatchedWheel routes a wheel event to the gesture's claimant
// if one is latched. Returns false when there is no active gesture
// (normal position routing applies, and the consumer re-claims).
func DeliverLatchedWheel(e MouseWheelEvent) bool {
	wheelGesture.mu.Lock()
	fn := wheelGesture.deliver
	if fn == nil {
		wheelGesture.mu.Unlock()
		return false
	}
	if time.Since(wheelGesture.last) > wheelGestureStop && wheelGesture.moved {
		// Clear stop + pointer move: release the latch.
		wheelGesture.deliver = nil
		wheelGesture.mu.Unlock()
		return false
	}
	e.X = e.ScreenX - wheelGesture.offX
	e.Y = e.ScreenY - wheelGesture.offY
	wheelGesture.last = time.Now()
	wheelGesture.moved = false
	wheelGesture.mu.Unlock()
	fn(e)
	return true
}

// ResetWheelGesture clears any latched claimant (tests, teardown).
func ResetWheelGesture() {
	wheelGesture.mu.Lock()
	wheelGesture.deliver = nil
	wheelGesture.mu.Unlock()
}
