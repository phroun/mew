package core

// What a fade comes to, and when it is over.

import (
	"testing"
	"time"
)

func TestAFadeStartsWhereItStartsAndArrivesWhereItGoes(t *testing.T) {
	now := time.Now()
	f := Fade{Start: now, Dur: 200 * time.Millisecond, From: 0, To: 1}

	if got := f.At(now); got != 0 {
		t.Errorf("at the moment it set out it is %v, not where it started", got)
	}
	if got := f.At(now.Add(200 * time.Millisecond)); got != 1 {
		t.Errorf("at the end it is %v, not where it was going", got)
	}
	// And it stays there rather than running past.
	if got := f.At(now.Add(time.Hour)); got != 1 {
		t.Errorf("long after, it is %v", got)
	}
	// Before it set out it is still at its start, which is what an unstarted
	// fade in a frame drawn early comes to.
	if got := f.At(now.Add(-time.Second)); got != 0 {
		t.Errorf("before it set out it is %v", got)
	}
}

// The middle is eased rather than linear: it covers more than half the
// distance in the first half of the time, which is what makes it read as one
// movement settling rather than a ramp switching off.
func TestAFadeEasesRatherThanRamps(t *testing.T) {
	now := time.Now()
	f := Fade{Start: now, Dur: 200 * time.Millisecond, From: 0, To: 1}

	half := f.At(now.Add(100 * time.Millisecond))
	if half <= 0.5 {
		t.Errorf("half way through it has covered %v, no more than a linear ramp", half)
	}
	if half >= 1 {
		t.Errorf("half way through it has already arrived (%v)", half)
	}
}

// A fade reports when there is nothing more to draw, which is what stops the
// surface asking for frames on its account.
func TestAFadeSaysWhenItIsDone(t *testing.T) {
	now := time.Now()
	f := Fade{Start: now, Dur: 200 * time.Millisecond, From: 0, To: 1}

	if f.Done(now) {
		t.Error("it was done before it began")
	}
	if f.Done(now.Add(199 * time.Millisecond)) {
		t.Error("it was done a millisecond early")
	}
	if !f.Done(now.Add(200 * time.Millisecond)) {
		t.Error("it arrived and still asks for frames")
	}

	// A fade with no duration is over at once, so the zero Fade is a jump
	// rather than a division by nothing.
	instant := Fade{Start: now, To: 1}
	if !instant.Done(now) || instant.At(now) != 1 {
		t.Error("a fade with no duration is not simply already there")
	}
}

// Turning one around starts from wherever it had got to, so a note taken down
// half way in leaves from half in rather than snapping to solid first.
func TestReversingAFadeLeavesFromWhereItGotTo(t *testing.T) {
	now := time.Now()
	in := Fade{Start: now, Dur: 200 * time.Millisecond, From: 0, To: 1}

	at := now.Add(50 * time.Millisecond)
	part := in.At(at)
	out := in.Reverse(at, 0, 200*time.Millisecond)

	if out.At(at) != part {
		t.Errorf("turned around it starts at %v, not the %v it had reached", out.At(at), part)
	}
	if out.At(at.Add(200*time.Millisecond)) != 0 {
		t.Error("turned around it does not arrive at nothing")
	}
	if part >= 1 {
		t.Fatal("it had already arrived, so nothing was turned around")
	}
}
