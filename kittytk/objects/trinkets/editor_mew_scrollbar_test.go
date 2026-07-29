//go:build mew

package trinkets

import (
	"math"
	"testing"

	mew "github.com/phroun/mew"
)

func testRegion() mew.ScrollbarRegion {
	return mew.ScrollbarRegion{
		ViewportID: "doc", Col: 80, Row: 1, TrackH: 24,
		Top: 0, Page: 24, LineCount: 1000,
	}
}

// The thumb is CONTINUOUS: its length is the visible fraction of the document
// and its travel the scroll fraction — neither snapped to whole rows, which is
// the whole point of drawing it in pixels rather than cells.
func TestThumbSpanIsContinuous(t *testing.T) {
	track := barRect{x: 0, y: 0, w: 8, h: 384} // 24 rows of 16px
	r := testRegion()

	pos, length := thumbSpan(track, r, 1)
	// 24 of 1000 lines visible: 2.4% of 384px = 9.216px. A cell-locked thumb
	// could only have been 0 or 16.
	if math.Abs(length-9.216) > 0.01 {
		t.Errorf("thumb length = %v, want ~9.216 (a fraction of a row)", length)
	}
	if pos != 0 {
		t.Errorf("at the top the thumb starts at 0, got %v", pos)
	}

	// Halfway down the scroll range the thumb sits halfway down its travel —
	// at a pixel offset no whole number of rows lands on.
	r.Top = (r.LineCount - r.Page) / 2
	pos, _ = thumbSpan(track, r, 1)
	span := track.h - length
	if math.Abs(pos-span/2) > 0.2 {
		t.Errorf("at mid-scroll the thumb is at %v, want ~%v", pos, span/2)
	}
	if math.Mod(pos, 16) == 0 {
		t.Errorf("thumb landed exactly on a row boundary (%v); it is not row-locked", pos)
	}

	// At the bottom it ends flush with the track.
	r.Top = r.LineCount - r.Page
	pos, length = thumbSpan(track, r, 1)
	if math.Abs((pos+length)-track.h) > 0.01 {
		t.Errorf("at max scroll the thumb ends at %v, want the track end %v", pos+length, track.h)
	}
}

// A document that fits has no scrolling to do: the thumb fills the track.
func TestThumbFillsTrackWhenDocumentFits(t *testing.T) {
	track := barRect{w: 8, h: 384}
	r := testRegion()
	r.LineCount = 10 // fewer lines than the page
	pos, length := thumbSpan(track, r, 1)
	if pos != 0 || length != track.h {
		t.Fatalf("fitting document: thumb (%v,%v), want (0,%v)", pos, length, track.h)
	}
}

// The pointer moves in pixels; the document moves in LINES. Every pixel
// position must map to a whole line, the endpoints must be exact, and
// neighbouring pixels must never skip a line or run backwards.
func TestThumbPositionAlwaysYieldsAWholeLine(t *testing.T) {
	track := barRect{w: 8, h: 384}
	r := testRegion()
	_, length := thumbSpan(track, r, 1)
	span := track.h - length
	maxTop := r.LineCount - r.Page

	if got := topLineForThumb(0, track, r, 1); got != 0 {
		t.Errorf("thumb at the top yields line %d, want 0", got)
	}
	if got := topLineForThumb(span, track, r, 1); got != maxTop {
		t.Errorf("thumb at the bottom yields line %d, want %d", got, maxTop)
	}
	// Past either end clamps rather than overshooting.
	if got := topLineForThumb(-50, track, r, 1); got != 0 {
		t.Errorf("above the track yields %d, want 0", got)
	}
	if got := topLineForThumb(span+50, track, r, 1); got != maxTop {
		t.Errorf("below the track yields %d, want %d", got, maxTop)
	}
	// Monotonic, and always a whole line (the type guarantees whole; this
	// guarantees it never goes backwards as the pointer descends).
	prev := 0
	for px := 0.0; px <= span; px += 0.5 {
		got := topLineForThumb(px, track, r, 1)
		if got < prev {
			t.Fatalf("thumb at %vpx yields line %d after %d — scrolling ran backwards", px, got, prev)
		}
		if got < 0 || got > maxTop {
			t.Fatalf("thumb at %vpx yields out-of-range line %d", px, got)
		}
		prev = got
	}
}

// Sub-pixel thumb movement still resolves to distinct lines: the smoothness is
// in the thumb, not lost to the quantisation.
func TestSmallThumbMovesStillChangeTheLine(t *testing.T) {
	track := barRect{w: 8, h: 384}
	r := testRegion()
	_, length := thumbSpan(track, r, 1)
	span := track.h - length
	// One pixel of travel covers maxTop/span lines — for 1000 lines over
	// ~375px that is nearly 3 lines, so a single pixel must move the view.
	a := topLineForThumb(span/2, track, r, 1)
	b := topLineForThumb(span/2+1, track, r, 1)
	if a == b {
		t.Errorf("a pixel of thumb travel changed nothing (%d): the drag would feel dead", a)
	}
}

// The bars collection tracks mew's publications: new bars appear, changed ones
// report a change, and vanished ones are dropped.
func TestEditorBarsTrackPublications(t *testing.T) {
	var b editorBars
	r := testRegion()
	if !b.set([]mew.ScrollbarRegion{r}) {
		t.Fatal("a first publication is a change")
	}
	if b.set([]mew.ScrollbarRegion{r}) {
		t.Fatal("an identical publication is not a change")
	}
	r.Top = 40
	if !b.set([]mew.ScrollbarRegion{r}) {
		t.Fatal("a scrolled view is a change")
	}
	// The bar kept its identity (and so its drag state) across the update.
	var seen int
	b.each(func(bar *editorBar) {
		seen++
		if bar.region.Top != 40 {
			t.Errorf("bar holds Top=%d, want 40", bar.region.Top)
		}
	})
	if seen != 1 {
		t.Fatalf("saw %d bars, want 1", seen)
	}
	if !b.set(nil) {
		t.Fatal("dropping the last bar is a change")
	}
	b.each(func(*editorBar) { t.Fatal("no bars should remain") })
}
