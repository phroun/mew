package trinkets

// When a note raised over the screen appears, and when it does not wait.

import (
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
)

// resting hands back a graphical desktop whose dwell is short enough to run a
// test against, with the label a note will be asked about.
func resting(t *testing.T, dwell time.Duration) (*Desktop, *Label) {
	t.Helper()
	d, _, label := onScreen(t)
	d.tooltipDwell = dwell
	return d, label
}

// A note is not raised the moment the pointer arrives: a hand crossing the
// screen would leave a trail of them. It waits for the pointer to settle.
func TestANoteWaitsForThePointerToSettle(t *testing.T) {
	d, label := resting(t, 30*time.Millisecond)

	if !d.ShowTooltip(core.TooltipRequest{Text: "the whole fingerprint", From: label}) {
		t.Fatal("the desktop refused the offer outright")
	}
	d.ProcessTimers()
	if showing := d.TooltipShowing(); showing != "" {
		t.Fatalf("the note appeared under the pointer at once, saying %q", showing)
	}

	// Once the pointer has been still that long, it appears.
	time.Sleep(40 * time.Millisecond)
	d.ProcessTimers()
	if d.TooltipShowing() != "the whole fingerprint" {
		t.Error("the pointer settled and no note appeared")
	}
}

// The wait is on the POINTER being still, not on how long the offer has been
// held: a hand travelling across a row of trinkets never reaches the end of it.
func TestAMovingPointerNeverReachesTheEndOfTheWait(t *testing.T) {
	d, label := resting(t, 30*time.Millisecond)
	d.ShowTooltip(core.TooltipRequest{Text: "the whole fingerprint", From: label})

	for i := 0; i < 4; i++ {
		time.Sleep(20 * time.Millisecond)
		d.HandleMouseMove(core.MouseMoveEvent{X: core.Unit(i), Y: 1})
		d.ProcessTimers()
	}
	if showing := d.TooltipShowing(); showing != "" {
		t.Errorf("the pointer kept moving and a note appeared anyway, saying %q", showing)
	}
}

// A reader already reading notes is not made to wait again: moving along a row
// to ask about the neighbour is one continued question.
func TestTheNextNoteComesAtOnce(t *testing.T) {
	d, label := resting(t, 30*time.Millisecond)

	d.ShowTooltip(core.TooltipRequest{Text: "the first", From: label})
	time.Sleep(40 * time.Millisecond)
	d.ProcessTimers()
	if d.TooltipShowing() != "the first" {
		t.Fatal("the first note never appeared, so there is no follow-on to test")
	}

	// Moving to the neighbour takes the first down and asks about the second.
	d.HideTooltip(label)
	if !d.ShowTooltip(core.TooltipRequest{Text: "the second", From: label}) {
		t.Fatal("the desktop refused the second offer")
	}
	if got := d.TooltipShowing(); got != "the second" {
		t.Errorf("the second note says %q rather than appearing at once", got)
	}
}

// But a reader who stopped reading them starts the wait again.
func TestComingBackLaterWaitsAgain(t *testing.T) {
	d, label := resting(t, 30*time.Millisecond)
	d.tooltipShownAt = time.Now().Add(-2 * tooltipFollowOn)

	d.ShowTooltip(core.TooltipRequest{Text: "the whole fingerprint", From: label})
	d.ProcessTimers()
	if showing := d.TooltipShowing(); showing != "" {
		t.Errorf("a note appeared at once long after the last one, saying %q", showing)
	}
}

// An offer the pointer moved off before it ripened is dropped, not shown late.
func TestAnOfferTheHandLeftIsNotShownLate(t *testing.T) {
	d, label := resting(t, 30*time.Millisecond)

	d.ShowTooltip(core.TooltipRequest{Text: "the whole fingerprint", From: label})
	d.HideTooltip(label)

	time.Sleep(40 * time.Millisecond)
	d.ProcessTimers()
	if showing := d.TooltipShowing(); showing != "" {
		t.Errorf("the pointer had gone and the note appeared anyway, saying %q", showing)
	}
}

// An offer cancelled before it was ever shown is not a note the reader has
// read, so it earns no follow-on: a pointer crossing a row of trinkets offers
// and cancels at each one, and the far end of the row still waits its turn.
func TestCrossingTrinketsEarnsNoFollowOn(t *testing.T) {
	d, label := resting(t, 30*time.Millisecond)

	for i := 0; i < 3; i++ {
		d.ShowTooltip(core.TooltipRequest{Text: "passing by", From: label})
		d.HideTooltip(label)
	}
	d.ShowTooltip(core.TooltipRequest{Text: "the one stopped at", From: label})

	if showing := d.TooltipShowing(); showing != "" {
		t.Errorf("crossing trinkets bought an instant note, saying %q", showing)
	}
}

// The status bar has no such cost -- it is a row that is already there,
// saying something already -- so it answers the moment it is asked.
func TestTheStatusBarAnswersAtOnce(t *testing.T) {
	d, bar := bareDesktop(t)
	d.tooltipDwell = time.Hour

	d.ShowTooltip(core.TooltipRequest{Text: "the whole fingerprint", From: NewLabel("x")})
	if got := bar.Text(); got != "the whole fingerprint" {
		t.Errorf("the status bar says %q, so it waited", got)
	}
}
