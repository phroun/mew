package trinkets

// A note comes and goes rather than appearing and vanishing.

import (
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
)

// A note arrives: it starts fully clear and reaches solid over the fade.
func TestANoteArrivesRatherThanAppearing(t *testing.T) {
	d, _, label := onScreen(t)
	d.tooltipDwell = time.Nanosecond
	d.tooltipFade = 200 * time.Millisecond
	d.tooltipShownAt = time.Now()

	d.ShowTooltip(core.TooltipRequest{Text: "the whole fingerprint", From: label})
	overlay := tooltipOverlay(d.windowManager)
	if overlay == nil {
		t.Fatal("no note was raised")
	}
	if overlay.Fade == nil {
		t.Fatal("the note was raised with nothing to say how it arrives")
	}
	start := overlay.Fade.Start
	if got := overlay.Fade.At(start); got != 0 {
		t.Errorf("the note starts at %v rather than fully clear", got)
	}
	if got := overlay.Fade.At(start.Add(200 * time.Millisecond)); got != 1 {
		t.Errorf("the note reaches %v rather than solid", got)
	}
	if overlay.Fade.Done(start) {
		t.Error("the note is already done arriving, so the surface stops drawing it")
	}
}

// And departs: hiding it turns the fade around and leaves it on the layer
// until it has gone, rather than taking it off under the reader's eye.
func TestANoteDepartsRatherThanVanishing(t *testing.T) {
	d, _, label := onScreen(t)
	d.tooltipDwell = time.Nanosecond
	d.tooltipFade = 60 * time.Millisecond
	d.tooltipShownAt = time.Now()

	d.ShowTooltip(core.TooltipRequest{Text: "the whole fingerprint", From: label})
	if tooltipOverlay(d.windowManager) == nil {
		t.Fatal("no note was raised")
	}

	d.HideTooltip(label)
	overlay := tooltipOverlay(d.windowManager)
	if overlay == nil {
		t.Fatal("the note was taken off the layer the moment it was hidden")
	}
	if overlay.Fade == nil || overlay.Fade.To != 0 {
		t.Fatalf("the note is not on its way out: %+v", overlay.Fade)
	}

	// The desktop no longer counts it as showing, even while it is leaving.
	if showing := d.TooltipShowing(); showing != "" {
		t.Errorf("a note on its way out still counts as showing %q", showing)
	}

	// It comes off the layer once it has gone.
	time.Sleep(80 * time.Millisecond)
	d.ProcessTimers()
	if tooltipOverlay(d.windowManager) != nil {
		t.Error("the note finished leaving and is still on the layer")
	}
}

// A note taken down part way in leaves from where it got to, rather than
// snapping to solid and then fading from there.
func TestANoteTakenDownHalfWayInLeavesFromThere(t *testing.T) {
	d, _, label := onScreen(t)
	d.tooltipDwell = time.Nanosecond
	d.tooltipFade = 400 * time.Millisecond
	d.tooltipShownAt = time.Now()

	d.ShowTooltip(core.TooltipRequest{Text: "the whole fingerprint", From: label})
	time.Sleep(60 * time.Millisecond)
	d.HideTooltip(label)

	overlay := tooltipOverlay(d.windowManager)
	if overlay == nil || overlay.Fade == nil {
		t.Fatal("the note is not leaving")
	}
	if overlay.Fade.From >= 1 {
		t.Errorf("it leaves from %v, having snapped to solid first", overlay.Fade.From)
	}
	if overlay.Fade.From <= 0 {
		t.Errorf("it leaves from %v, having never arrived at all", overlay.Fade.From)
	}
}

// A new note replaces one still on its way out: the layer holds one tooltip,
// and the departing one must not take the new one off with it.
func TestANewNoteReplacesOneStillLeaving(t *testing.T) {
	d, _, label := onScreen(t)
	d.tooltipDwell = time.Nanosecond
	d.tooltipFade = 40 * time.Millisecond
	d.tooltipShownAt = time.Now()

	d.ShowTooltip(core.TooltipRequest{Text: "the first", From: label})
	d.HideTooltip(label)
	d.ShowTooltip(core.TooltipRequest{Text: "the second", From: label})

	time.Sleep(60 * time.Millisecond)
	d.ProcessTimers()

	if tooltipOverlay(d.windowManager) == nil {
		t.Fatal("the departing note took the new one off the layer with it")
	}
	if got := d.TooltipShowing(); got != "the second" {
		t.Errorf("the desktop is showing %q", got)
	}
}
