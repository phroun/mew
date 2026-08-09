package editor

import (
	"strings"
	"testing"
)

// A cloned viewport (clone_viewport: a NEW viewport on the SAME buffer, shown
// beside the original) must carry the scrollbar setting too. cloneCurrentViewport
// copies the source's display options — line numbers, ruler, marks — but once
// omitted the scrollbar, so the clone reserved no bar column: showScrollbar was
// false, no region was published for it, and on a host that draws the bars the
// cloned pane came up with none (the bar appeared only on whichever pane held the
// original, so swapping the two panes moved it). Inheriting Scrollbar fixes it.
func TestClonedViewportInheritsScrollbar(t *testing.T) {
	e, w, _ := newRenderedEditor(t, strings.Repeat("line\n", 200))
	if !e.setOption(w, "scrollbar", "true") {
		t.Fatal("enable scrollbar on the source viewport")
	}
	var captured []ScrollbarRegion
	e.Config.ScrollbarRegions = func(rs []ScrollbarRegion) {
		captured = append([]ScrollbarRegion(nil), rs...)
	}
	e.ensureTiler()
	if !e.cloneCurrentViewport() {
		t.Fatal("cloneCurrentViewport failed")
	}
	e.performRender()

	if len(captured) != 2 {
		t.Fatalf("a scrollbar-on source cloned into two panes published %d regions, want 2 "+
			"(the clone must inherit the scrollbar); regions=%+v", len(captured), captured)
	}
	// Distinct tiles: different viewport ids, and the bars land in different
	// reserved columns (one per pane), so neither collapses onto the other.
	if captured[0].ViewportID == captured[1].ViewportID {
		t.Fatalf("both regions came from the same viewport %q — the clone got no bar", captured[0].ViewportID)
	}
	if captured[0].Col == captured[1].Col && captured[0].Row == captured[1].Row {
		t.Fatalf("both bars land at Col=%d Row=%d — they collide to one on the host", captured[0].Col, captured[0].Row)
	}
}

// Each pane's scrollbar is published at ITS OWN track height and page, so the
// host scales each thumb to that pane — a tall pane and a short one beside it do
// not share a height. Geometry lives with the tile (notifyScrollbarRegions'
// main-tile loop reads t.ScrollbarTrackH / t.ContentHeight per tile), so an
// uneven layout must publish distinct heights.
func TestClonedPanesPublishTheirOwnHeight(t *testing.T) {
	e, w, _ := newRenderedEditor(t, strings.Repeat("line\n", 400))
	if !e.setOption(w, "scrollbar", "true") {
		t.Fatal("enable scrollbar")
	}
	var captured []ScrollbarRegion
	e.Config.ScrollbarRegions = func(rs []ScrollbarRegion) {
		captured = append([]ScrollbarRegion(nil), rs...)
	}
	e.ensureTiler()
	e.cloneCurrentViewport()                               // doc | main_1 (full height each)
	e.PawScript.ExecuteAsync("viewport_split #tile, down") // split the clone -> two short tiles beside the tall doc
	e.performRender()

	if len(captured) != 3 {
		t.Fatalf("uneven clone layout published %d regions, want 3; regions=%+v", len(captured), captured)
	}
	// The full-height pane's track is taller than either half-height pane's, and
	// each region's Page matches its own TrackH (the track IS the page rows).
	var tall, short int
	for _, r := range captured {
		if r.TrackH != r.Page {
			t.Errorf("region %+v: TrackH != Page, so the thumb would scale to a height the pane doesn't have", r)
		}
		if r.TrackH >= 20 {
			tall++
		} else {
			short++
		}
	}
	if tall != 1 || short != 2 {
		t.Fatalf("want one tall pane and two short ones at their own heights; got tall=%d short=%d (%+v)", tall, short, captured)
	}
}
