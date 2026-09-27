package core

// Coming and going.

import (
	"math"
	"time"
)

// Fade is one layer's opacity over time: where it started, where it is going,
// and when it set out.
//
// It is a VALUE, not a running animation. Nothing ticks it and nothing owns
// it: whoever is drawing asks what it comes to at the instant of the frame,
// and the compositor keeps asking for frames until it reports itself done. A
// fade therefore runs at whatever rate the surface draws rather than at the
// rate of some timer, and a fade nobody draws costs nothing at all.
type Fade struct {
	Start time.Time
	Dur   time.Duration
	From  float64
	To    float64
}

// At is the opacity this fade has reached, eased so it leaves and arrives
// gently rather than at a constant rate -- the same ease-out the rest of the
// toolkit's motion uses.
//
// A fade with no duration is over the moment it starts, which makes the zero
// Fade an instant jump to To rather than a division by nothing.
func (f Fade) At(now time.Time) float64 {
	if f.Dur <= 0 {
		return f.To
	}
	t := float64(now.Sub(f.Start)) / float64(f.Dur)
	if t <= 0 {
		return f.From
	}
	if t >= 1 {
		return f.To
	}
	return f.From + (f.To-f.From)*easeOutCubic(t)
}

// Done reports that there is nothing more to draw: the fade has arrived, and
// the surface need not ask for another frame on its account.
func (f Fade) Done(now time.Time) bool {
	return f.Dur <= 0 || !now.Before(f.Start.Add(f.Dur))
}

// Reverse turns a fade around from wherever it has got to, so a note taken
// down halfway in leaves from half in rather than jumping to full first.
func (f Fade) Reverse(now time.Time, to float64, dur time.Duration) Fade {
	return Fade{Start: now, Dur: dur, From: f.At(now), To: to}
}

// easeOutCubic starts quickly and settles, which is what makes a fade read as
// one movement rather than as a linear ramp switching off.
func easeOutCubic(t float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	return 1 - math.Pow(1-t, 3)
}
