package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// runTree is a tree whose columns over-commit its width, so there is a run
// long enough to pan and a footer bar to pan it with.
func runTree(t *testing.T, dir core.Direction, smooth bool) *TreeView {
	t.Helper()
	tv := treeOn(t, smooth)
	tv.SetDirection(dir)
	tv.SetFitWidth(false)
	tv.SetKeyWidth(40 * cell)
	tv.SetBounds(core.UnitRect{Width: 30 * 8, Height: 12 * 16})
	return tv
}

// apart is the distance between two places, whichever side they are on.
func apart(a, b core.Unit) core.Unit {
	if a < b {
		return b - a
	}
	return a - b
}

// band is where the columns are allowed to stand: everything but the lane.
func band(t *TreeView) (x0, x1 core.Unit) {
	lay := t.columnLayout()
	return lay.contentX, lay.contentX + lay.contentW
}

// The columns are laid out along a RUN -- first column at the beginning, the
// rest following -- and which way that run travels across the screen is the
// tree's direction. So the two trees are the same run seen in a mirror: each
// span stands as far from the end the run starts at as its opposite number.
func TestTheColumnRunIsTheSameRunEitherWayRound(t *testing.T) {
	ltr, rtl := runTree(t, core.DirLTR, false), runTree(t, core.DirRTL, false)
	lLay, rLay := ltr.columnLayout(), rtl.columnLayout()
	if len(lLay.spans) != len(rLay.spans) {
		t.Fatalf("the same tree has %d spans one way and %d the other",
			len(lLay.spans), len(rLay.spans))
	}
	lx0, _ := band(ltr)
	_, rx1 := band(rtl)

	for i := range lLay.spans {
		l, r := lLay.spans[i], rLay.spans[i]
		if l.col != r.col && (l.col == nil) != (r.col == nil) {
			t.Fatalf("span %d is a different column each way round", i)
		}
		if l.w != r.w {
			t.Errorf("span %d is %d wide one way and %d the other", i, l.w, r.w)
		}
		if into, back := l.x-lx0, rx1-r.x-r.w; into != back {
			t.Errorf("span %d begins %d into the run reading left to right and %d reading right to left",
				i, into, back)
		}
		if lVis, rVis := lLay.divVisible(l), rLay.divVisible(r); lVis != rVis {
			t.Errorf("span %d shows a divider reading left to right (%v) and %v the other way",
				i, lVis, rVis)
		} else if lVis {
			if into, back := l.divX-lx0, rx1-r.divX-rLay.dividerW; into != back {
				t.Errorf("span %d's divider stands %d into the run one way and %d the other",
					i, into, back)
			}
		}
	}
}

// The scrollbar stands in the column at the TRAILING edge -- the far side of
// the screen where the tree reads left to right, the near side where it reads
// the other way -- and the columns take everything else. Neither trespasses.
func TestTheLaneAndTheColumnsTakeOppositeSides(t *testing.T) {
	for _, tc := range []struct {
		dir  core.Direction
		lane core.Unit
		band core.Unit
	}{
		{core.DirLTR, 29 * 8, 0},
		{core.DirRTL, 0, 8},
	} {
		tv := runTree(t, tc.dir, false)
		if got := tv.laneX(); got != tc.lane {
			t.Errorf("%v: the scrollbar lane stands at %d, want %d", tc.dir, got, tc.lane)
		}
		lay := tv.columnLayout()
		if lay.contentX != tc.band {
			t.Errorf("%v: the columns start at %d, want %d", tc.dir, lay.contentX, tc.band)
		}
		if !tv.onLane(tc.lane) || tv.onLane(tc.band) {
			t.Errorf("%v: the lane does not answer for its own column alone", tc.dir)
		}
		// A span's natural width overruns the tree -- that is what there
		// is to pan -- so what must keep off the lane is what it PAINTS.
		lane := tv.laneX()
		for i, sp := range lay.spans {
			r, ok := lay.spanClip(sp, 16)
			if !ok {
				continue
			}
			if r.X < lane+8 && lane < r.X+r.Width {
				t.Errorf("%v: span %d paints [%d,%d), over the lane at %d",
					tc.dir, i, r.X, r.X+r.Width, lane)
			}
		}
	}
}

// A tree nobody has panned shows the BEGINNING of its columns, and where a run
// begins is what the direction settles. Panning forward brings the later
// columns in, so the first one slides off the side it began on.
func TestThePanOpensAtTheRunsBeginning(t *testing.T) {
	for _, tc := range []struct {
		dir core.Direction
		// where the first column rests unpanned, and which way it goes
		// as the pan travels forward.
		rests core.Unit
		steps core.Unit
	}{
		{core.DirLTR, 0, -1},
		{core.DirRTL, 30 * 8, +1},
	} {
		tv := runTree(t, tc.dir, false)
		lay := tv.columnLayout()
		if lay.maxHScroll <= 0 {
			t.Fatalf("%v: nothing to pan in a tree whose columns overrun it", tc.dir)
		}
		first := lay.spans[0]
		begins := first.x
		if core.ChromeMirrored(tv) {
			begins = first.x + first.w
		}
		if begins != tc.rests {
			t.Errorf("%v: unpanned, the first column begins at %d, want the run's beginning %d",
				tc.dir, begins, tc.rests)
		}

		if !tv.scrollHorizontally(4 * cell) {
			t.Fatalf("%v: panning four columns did nothing", tc.dir)
		}
		moved := tv.columnLayout().spans[0].x - first.x
		if moved == 0 || (moved > 0) != (tc.steps > 0) {
			t.Errorf("%v: panning forward moved the first column by %d, want it to travel %d",
				tc.dir, moved, tc.steps)
		}
	}
}

// The footer thumb reports that journey: it rests against the end of its track
// the run starts at and walks to the other, so the bar reads the same way the
// columns under it do.
func TestTheFooterThumbWalksWithTheRun(t *testing.T) {
	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		tv := runTree(t, dir, false)
		lay := tv.columnLayout()
		trackX0, trackX1, thumbX0, thumbX1, ok := tv.hScrollbarGeometry(lay)
		if !ok {
			t.Fatalf("%v: no footer bar on a tree with a run to pan", dir)
		}
		// Unpanned: the thumb is against the end the run begins at. The
		// thumb starts on a whole column, so "against" is within one.
		beginsAt, endsAt := trackX0, trackX1
		gotBegin, gotEnd := thumbX0, thumbX1
		if core.ChromeMirrored(tv) {
			beginsAt, endsAt = trackX1, trackX0
			gotBegin, gotEnd = thumbX1, thumbX0
		}
		if apart(gotBegin, beginsAt) >= cell {
			t.Errorf("%v: unpanned, the thumb sits at %d, want the track's beginning %d",
				dir, gotBegin, beginsAt)
		}

		tv.hScroll = lay.maxHScroll
		_, _, thumbX0, thumbX1, _ = tv.hScrollbarGeometry(tv.columnLayout())
		gotEnd = thumbX1
		if core.ChromeMirrored(tv) {
			gotEnd = thumbX0
		}
		if apart(gotEnd, endsAt) >= cell {
			t.Errorf("%v: panned to the end, the thumb reaches %d, want the track's end %d",
				dir, gotEnd, endsAt)
		}
	}
}

// Clicking the track past the thumb pages the way the SCREEN says: further
// along the track is further along the run reading left to right, and back
// along it reading right to left.
func TestPagingTheFooterFollowsTheScreen(t *testing.T) {
	for _, tc := range []struct {
		dir core.Direction
		// the side of the track a forward page is asked for on
		forward func(x0, x1 core.Unit) core.Unit
	}{
		{core.DirLTR, func(_, x1 core.Unit) core.Unit { return x1 - 1 }},
		{core.DirRTL, func(x0, _ core.Unit) core.Unit { return x0 }},
	} {
		tv := runTree(t, tc.dir, false)
		lay := tv.columnLayout()
		trackX0, trackX1, _, _, ok := tv.hScrollbarGeometry(lay)
		if !ok {
			t.Fatalf("%v: no footer bar to page", tc.dir)
		}
		y := tv.Bounds().Height - tv.footerHeight()
		if !tv.handleHBarPress(core.MousePressEvent{
			Button: core.LeftButton, X: tc.forward(trackX0, trackX1), Y: y,
		}) {
			t.Fatalf("%v: the footer row did not take the press", tc.dir)
		}
		if tv.hScroll <= 0 {
			t.Errorf("%v: paging away from the thumb left the pan at %d, want it further along the run",
				tc.dir, tv.hScroll)
		}
	}
}

// The edge fade stands where there is more content past the edge. Unpanned,
// the run has nothing behind it and everything ahead, so the one fade is on
// the side the columns travel towards -- the left of the screen where the tree
// reads left to right, the right of it where it reads the other way.
func TestTheEdgeFadeStandsAheadOfThePan(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	for _, tc := range []struct {
		dir   core.Direction
		ahead core.Unit // the region edge the fade should stand at
	}{
		{core.DirLTR, 1},
		{core.DirRTL, 0},
	} {
		px, err := raster.New(400, 240)
		if err != nil {
			t.Fatal(err)
		}
		core.SetTextMeasurer(px)
		rec := &stripeRecorder{RenderBackend: px}
		tv := runTree(t, tc.dir, true)
		lay := tv.columnLayout()
		p := core.NewPainter(rec)
		st := style.CellStyle{Bg: tv.GetScheme().GetListBG()}
		tv.paintHScrollFades(p, lay, st, nil, nil, st)

		edges := [2]core.Unit{lay.scrollL, lay.scrollR}
		var near [2]int
		for _, s := range rec.stripes {
			for i, e := range edges {
				if apart(core.Unit(s.x), e) <= 2*cell {
					near[i]++
				}
			}
		}
		if near[tc.ahead] == 0 {
			t.Errorf("%v: unpanned, nothing fades at the edge the columns travel towards", tc.dir)
		}
		if other := near[1-tc.ahead]; other != 0 {
			t.Errorf("%v: unpanned, %d stripes fade at the edge the run began at", tc.dir, other)
		}
	}
}

// Bringing a column into view pans the RUN. The arithmetic that says how far
// in a column already is has to read the same way the run does, or a tree
// reading right to left pans away from what it was asked to show.
func TestBringingAColumnIntoViewReadsTheRun(t *testing.T) {
	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		tv := runTree(t, dir, false)
		cols := tv.Columns()
		last := cols[len(cols)-1]

		lay := tv.columnLayout()
		for _, sp := range lay.spans {
			if spanMatchesCol(sp, last) {
				if r, ok := lay.spanClip(sp, 16); ok && r.Width == sp.w {
					t.Fatalf("%v: the last column is already in view; nothing to bring in", dir)
				}
			}
		}

		tv.ensureColVisible(last)
		if tv.hScroll <= 0 {
			t.Errorf("%v: bringing the last column into view left the pan at %d", dir, tv.hScroll)
		}
		lay = tv.columnLayout()
		for _, sp := range lay.spans {
			if !spanMatchesCol(sp, last) {
				continue
			}
			r, ok := lay.spanClip(sp, 16)
			if !ok || r.Width != sp.w {
				t.Errorf("%v: the last column shows %d of its %d units after being brought into view",
					dir, r.Width, sp.w)
			}
		}
	}
}

// A press finds the content and the lane on their own sides: the same click
// that takes a row in one tree lands on the scrollbar in the other.
func TestAPressFindsTheLaneWhereItStands(t *testing.T) {
	for _, tc := range []struct {
		dir      core.Direction
		onLaneX  core.Unit
		onRowX   core.Unit
		wantItem int
	}{
		{core.DirLTR, 29*8 + 4, 4, 1},
		{core.DirRTL, 4, 29*8 + 4, 1},
	} {
		tv := runTree(t, tc.dir, false)
		tv.SetCurrentIndex(0)
		press := func(x, y core.Unit) {
			tv.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: x, Y: y})
		}
		row := tv.headerHeight() + 16 // the second visible row

		press(tc.onRowX, row)
		if tv.CurrentIndex() != tc.wantItem {
			t.Errorf("%v: a press in the content band selected row %d, want %d",
				tc.dir, tv.CurrentIndex(), tc.wantItem)
		}

		// Well down the track, past the thumb: a page, not a grab.
		before := tv.scrollOffset
		tv.SetCurrentIndex(0)
		press(tc.onLaneX, tv.Bounds().Height-tv.footerHeight()-16)
		if tv.CurrentIndex() != 0 {
			t.Errorf("%v: a press in the scrollbar lane selected a row", tc.dir)
		}
		if tv.scrollOffset == before {
			t.Errorf("%v: a press on the scrollbar track did not page", tc.dir)
		}
	}
}

// A pinned column stands outside the pan, at the run's beginning -- so the
// scrolling region is what is left of the band beside it, and the columns
// travelling through that region stop at its edge whichever side that is.
func TestAPinnedColumnHoldsTheRunsBeginning(t *testing.T) {
	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		tv := runTree(t, dir, false)
		tv.SetKeyWidth(10 * cell)
		tv.SetFixedColumns(1, 0)
		lay := tv.columnLayout()
		pinned := lay.spans[0]
		if !pinned.fixed {
			t.Fatalf("%v: pinning the first column did not pin it", dir)
		}
		// The region begins past the pinned column, at the far end of the
		// band where the tree reads right to left.
		wantL, wantR := lay.contentX+pinned.w+lay.dividerW, lay.contentX+lay.contentW
		if core.ChromeMirrored(tv) {
			wantL, wantR = lay.contentX, lay.contentX+lay.contentW-pinned.w-lay.dividerW
		}
		if lay.scrollL != wantL || lay.scrollR != wantR {
			t.Errorf("%v: the scrolling region is [%d,%d), want [%d,%d) beside the pinned column",
				dir, lay.scrollL, lay.scrollR, wantL, wantR)
		}

		tv.hScroll = lay.maxHScroll
		lay = tv.columnLayout()
		if got := lay.spans[0].x; got != pinned.x {
			t.Errorf("%v: panning moved the pinned column from %d to %d", dir, pinned.x, got)
		}
		for i, sp := range lay.spans[1:] {
			r, ok := lay.spanClip(sp, 16)
			if !ok {
				continue
			}
			if r.X < pinned.x+pinned.w && pinned.x < r.X+r.Width {
				t.Errorf("%v: panned span %d paints [%d,%d), over the pinned column [%d,%d)",
					dir, i+1, r.X, r.X+r.Width, pinned.x, pinned.x+pinned.w)
			}
		}
	}
}

// The boundary between the scrolling region and a flank pinned at the run's
// END belongs to the flank, not to the region: it is drawn wherever the flank
// stands, while the dividers between panning columns come and go with them.
func TestThePinnedEndKeepsItsBoundary(t *testing.T) {
	for _, dir := range []core.Direction{core.DirLTR, core.DirRTL} {
		tv := runTree(t, dir, false)
		tv.SetKeyWidth(10 * cell)
		tv.SetFixedColumns(0, 1)
		lay := tv.columnLayout()

		n := len(lay.spans)
		boundary := lay.spans[n-2] // the span whose divider opens the flank
		if !lay.divPinned(boundary) {
			t.Errorf("%v: the boundary at %d is not held with the pinned flank",
				dir, boundary.divX)
		}
		if !lay.divVisible(boundary) {
			t.Errorf("%v: the pinned flank's boundary at %d is not drawn", dir, boundary.divX)
		}
	}
}

// Dragging a divider widens the column BEFORE it along the run, so the
// pointer's own travel is read the way the columns are ordered: towards the
// run's end widens, back along it narrows, on either side of the screen.
func TestDraggingADividerReadsTheRun(t *testing.T) {
	for _, tc := range []struct {
		dir core.Direction
		// which way across the SCREEN a widening drag goes
		widens core.Unit
	}{
		{core.DirLTR, +1},
		{core.DirRTL, -1},
	} {
		tv := runTree(t, tc.dir, false)
		tv.SetKeyWidth(10 * cell) // leave a data column's divider in view
		lay := tv.columnLayout()

		var div colSpan
		for _, sp := range lay.spans {
			if lay.divVisible(sp) && sp.col != nil && sp.col.Resizable {
				div = sp
				break
			}
		}
		if div.col == nil {
			t.Fatalf("%v: no resizable column with a visible divider", tc.dir)
		}
		before := div.col.Width

		grab := div.divX
		if !tv.handleMultiPress(core.MousePressEvent{Button: core.LeftButton, X: grab, Y: 0}) {
			t.Fatalf("%v: the header did not take the press on the divider at %d", tc.dir, grab)
		}
		tv.handleMultiMove(core.MouseMoveEvent{X: grab + tc.widens*4*cell, Y: 0})
		if div.col.Width <= before {
			t.Errorf("%v: dragging %d units along the run left the column at %d, was %d",
				tc.dir, tc.widens*4*cell, div.col.Width, before)
		}

		// And back the other way narrows it again.
		wide := div.col.Width
		tv.handleMultiMove(core.MouseMoveEvent{X: grab - tc.widens*2*cell, Y: 0})
		if div.col.Width >= wide {
			t.Errorf("%v: dragging back left the column at %d, was %d",
				tc.dir, div.col.Width, wide)
		}
	}
}

// In fit mode a line moves by trading cells across it against the slack pool,
// and which of the two columns gives way is settled by their order along the
// run -- so the pointer's travel is read that way too.
func TestTheFitDragReadsTheRun(t *testing.T) {
	for _, tc := range []struct {
		dir core.Direction
		// which way across the SCREEN a drag towards the run's end goes
		along core.Unit
	}{
		{core.DirLTR, +1},
		{core.DirRTL, -1},
	} {
		tv := treeOn(t, false)
		tv.SetDirection(tc.dir)
		tv.SetShowHeader(true)
		tv.SetBounds(core.UnitRect{Width: 60 * 8, Height: 10 * 16})
		lay := tv.columnLayout()

		// The first divider: the key column is left of it, so the column
		// to its right is the one the drag moves.
		first := lay.spans[0]
		if !lay.divVisible(first) || lay.spans[1].col == nil {
			t.Fatalf("%v: no key-column divider to grab", tc.dir)
		}
		right := lay.spans[1].col
		before := right.Width

		grab := first.divX
		if !tv.handleMultiPress(core.MousePressEvent{Button: core.LeftButton, X: grab, Y: 0}) {
			t.Fatalf("%v: the header did not take the press at %d", tc.dir, grab)
		}
		if !tv.colDragFit {
			t.Fatalf("%v: a fit-mode tree did not arm the composite drag", tc.dir)
		}
		// Along the run: the right column gives its cells back to the key.
		tv.handleMultiMove(core.MouseMoveEvent{X: grab + tc.along*3*cell, Y: 0})
		if right.Width >= before {
			t.Errorf("%v: dragging along the run left the right column at %d, was %d",
				tc.dir, right.Width, before)
		}
		// And back the other way returns them.
		narrow := right.Width
		tv.handleMultiMove(core.MouseMoveEvent{X: grab - tc.along*3*cell, Y: 0})
		if right.Width <= narrow {
			t.Errorf("%v: dragging back left the right column at %d, was %d",
				tc.dir, right.Width, narrow)
		}
	}
}
