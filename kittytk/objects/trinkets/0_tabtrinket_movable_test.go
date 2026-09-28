package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// moveStrip is five tabs of different widths with the third current, on a
// strip wide enough to show them all.
func moveStrip(t *testing.T, pos TabPosition, dir core.Direction, movable bool) *TabTrinket {
	t.Helper()
	core.SetTextMeasurer(nil)
	form := NewPanel()
	form.SetDirection(dir)
	tt := NewTabTrinket()
	tt.SetTabPosition(pos)
	tt.SetMovable(movable)
	form.AddChild(tt)
	for _, n := range []string{"A", "Bravo", "C", "Delta", "E"} {
		tt.AddTab(n, NewPanel())
	}
	tt.SetCurrentIndex(2)
	m := tt.EffectiveCellMetrics()
	tt.SetBounds(core.UnitRect{Width: 60 * m.UnitsPerCellWidth, Height: 8 * m.UnitsPerCellHeight})
	return tt
}

func tabOrder(tt *TabTrinket) string {
	s := ""
	for i := 0; i < tt.Count(); i++ {
		if i > 0 {
			s += " "
		}
		s += tt.TabText(i)
	}
	return s
}

// MoveTab puts a tab at another place, the tabs between closing up behind it.
// The current tab stays current wherever that leaves it, and the move is
// told; a change of selection is not, since the selection did not change.
func TestMoveTabKeepsTheCurrentTabCurrent(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	for _, tc := range []struct {
		from, to int
		order    string
		current  string
	}{
		{2, 0, "C A Bravo Delta E", "C"},
		{2, 4, "A Bravo Delta E C", "C"},
		{0, 3, "Bravo C Delta A E", "C"},
		{4, 1, "A E Bravo C Delta", "C"},
		{3, 4, "A Bravo C E Delta", "C"},
		{0, 2, "Bravo C A Delta E", "C"},
		{4, 2, "A Bravo E C Delta", "C"},
	} {
		tt := moveStrip(t, TabsTop, core.DirLTR, true)
		var moved [][2]int
		changes := 0
		tt.SetOnTabMoved(func(from, to int) { moved = append(moved, [2]int{from, to}) })
		tt.SetOnCurrentChanged(func(int) { changes++ })
		if !tt.MoveTab(tc.from, tc.to) {
			t.Errorf("MoveTab(%d, %d) moved nothing", tc.from, tc.to)
		}
		if got := tabOrder(tt); got != tc.order {
			t.Errorf("MoveTab(%d, %d): %q, want %q", tc.from, tc.to, got, tc.order)
		}
		if got := tt.TabText(tt.CurrentIndex()); got != tc.current {
			t.Errorf("MoveTab(%d, %d): %q is current, want %q", tc.from, tc.to, got, tc.current)
		}
		if len(moved) != 1 || moved[0] != [2]int{tc.from, tc.to} || changes != 0 {
			t.Errorf("MoveTab(%d, %d) told moves %v and %d changes", tc.from, tc.to, moved, changes)
		}
	}
	tt := moveStrip(t, TabsTop, core.DirLTR, true)
	for _, bad := range [][2]int{{2, 2}, {-1, 2}, {2, 5}, {5, 0}, {0, -1}} {
		if tt.MoveTab(bad[0], bad[1]) {
			t.Errorf("MoveTab(%d, %d) moved something", bad[0], bad[1])
		}
	}
	if got := tabOrder(tt); got != "A Bravo C Delta E" {
		t.Errorf("refused moves left %q", got)
	}
}

// Shift and an arrow along a movable strip carry the current tab with it, a
// place at a time, stopping at the ends. Off the strip's axis they do
// nothing. On a strip that is not movable, the shifted arrows along a top or
// bottom strip walk the tabs as they always did, and the others still pass.
func TestShiftArrowsCarryTheCurrentTab(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	key := func(tt *TabTrinket, k string) bool { return tt.HandleKeyPress(core.KeyPressEvent{Key: k}) }
	for _, tc := range []struct {
		pos         TabPosition
		back, on    string
		across      []string
		description string
	}{
		{TabsTop, "S-Left", "S-Right", []string{"S-Up", "S-Down"}, "top"},
		{TabsBottom, "S-Left", "S-Right", []string{"S-Up", "S-Down"}, "bottom"},
		{TabsSide, "S-Up", "S-Down", []string{"S-Left", "S-Right"}, "side"},
	} {
		tt := moveStrip(t, tc.pos, core.DirLTR, true)
		tt.SetFocus()
		if !key(tt, tc.back) || tabOrder(tt) != "A C Bravo Delta E" {
			t.Errorf("%s: %s left %q", tc.description, tc.back, tabOrder(tt))
		}
		key(tt, tc.back)
		if key(tt, tc.back); tabOrder(tt) != "C A Bravo Delta E" {
			t.Errorf("%s: %s past the start left %q", tc.description, tc.back, tabOrder(tt))
		}
		for i := 0; i < 6; i++ {
			key(tt, tc.on)
		}
		if tabOrder(tt) != "A Bravo Delta E C" || tt.TabText(tt.CurrentIndex()) != "C" {
			t.Errorf("%s: %s to the end left %q with %q current", tc.description, tc.on, tabOrder(tt), tt.TabText(tt.CurrentIndex()))
		}
		for _, k := range tc.across {
			if key(tt, k) {
				t.Errorf("%s: %s, across the strip, was taken", tc.description, k)
			}
		}
		if tabOrder(tt) != "A Bravo Delta E C" {
			t.Errorf("%s: keys across the strip moved it to %q", tc.description, tabOrder(tt))
		}
	}

	tt := moveStrip(t, TabsTop, core.DirLTR, false)
	tt.SetFocus()
	if !key(tt, "S-Left") || tt.CurrentIndex() != 1 || tabOrder(tt) != "A Bravo C Delta E" {
		t.Errorf("S-Left on a strip that is not movable: %q with %d current, want the tab before selected",
			tabOrder(tt), tt.CurrentIndex())
	}
	if !key(tt, "S-Right") || tt.CurrentIndex() != 2 {
		t.Errorf("S-Right on a strip that is not movable left %d current", tt.CurrentIndex())
	}
	side := moveStrip(t, TabsSide, core.DirLTR, false)
	side.SetFocus()
	if key(side, "S-Down") || side.CurrentIndex() != 2 {
		t.Errorf("S-Down on a side strip that is not movable was taken, %d current", side.CurrentIndex())
	}
}

// Dragging a tab along a top or bottom strip moves it live. It changes places
// with a neighbour only once the pointer is far enough over that, after the
// swap, the pointer is over the dragged tab -- so a narrow tab dragged onto a
// wide one does not change places and straight back. Letting go puts it down.
func TestDraggingATabMovesItLive(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	for _, tc := range []struct {
		pos TabPosition
		dir core.Direction
		row int
	}{
		{TabsTop, core.DirLTR, 0},
		{TabsBottom, core.DirLTR, 7},
		{TabsTop, core.DirRTL, 0},
	} {
		tt := moveStrip(t, tc.pos, tc.dir, true)
		var moves [][2]int
		tt.SetOnTabMoved(func(from, to int) { moves = append(moves, [2]int{from, to}) })
		m := tt.EffectiveCellMetrics()
		cw := m.UnitsPerCellWidth
		y := core.Unit(tc.row)*m.UnitsPerCellHeight + m.UnitsPerCellHeight/2
		screen := func(runX core.Unit) core.Unit {
			if core.ChromeMirrored(tt) {
				return tt.Bounds().Width - runX - 1
			}
			return runX
		}
		spanOf := func(owner int) stripSpan {
			for _, sp := range tt.stripSpans {
				if sp.owner == owner {
					return sp
				}
			}
			t.Fatalf("no span for tab %d", owner)
			return stripSpan{}
		}
		paintCloseGrid(t, tt)
		c := spanOf(2)
		start := c.x + cw/2
		tt.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: screen(start), Y: y})
		if tt.dragTab == nil {
			t.Fatalf("pos %v %v: the press picked up nothing", tc.pos, tc.dir)
		}
		if !tt.HandleMouseMove(core.MouseMoveEvent{X: screen(start), Y: y, Buttons: 1}) {
			t.Errorf("pos %v %v: the strip let a move go while carrying a tab", tc.pos, tc.dir)
		}

		// Sweep the pointer a unit at a time towards the start of the run,
		// painting between moves as the display would.
		for x := start; x >= 0; x-- {
			tt.HandleMouseMove(core.MouseMoveEvent{X: screen(x), Y: y, Buttons: 1})
			paintCloseGrid(t, tt)
		}
		if got := tabOrder(tt); got != "C A Bravo Delta E" {
			t.Errorf("pos %v %v: dragged to the start: %q", tc.pos, tc.dir, got)
		}
		// And back the other way, past the far end.
		for x := core.Unit(0); x < 55*cw; x++ {
			tt.HandleMouseMove(core.MouseMoveEvent{X: screen(x), Y: y, Buttons: 1})
			paintCloseGrid(t, tt)
		}
		if got := tabOrder(tt); got != "A Bravo Delta E C" {
			t.Errorf("pos %v %v: dragged to the end: %q", tc.pos, tc.dir, got)
		}
		// Each move went one way: two to the start, four back to the end.
		want := [][2]int{{2, 1}, {1, 0}, {0, 1}, {1, 2}, {2, 3}, {3, 4}}
		if len(moves) != len(want) {
			t.Errorf("pos %v %v: moves %v, want %v", tc.pos, tc.dir, moves, want)
		} else {
			for i := range want {
				if moves[i] != want[i] {
					t.Errorf("pos %v %v: moves %v, want %v", tc.pos, tc.dir, moves, want)
					break
				}
			}
		}
		if !tt.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton, X: screen(5 * cw), Y: y}) || tt.dragTab != nil {
			t.Errorf("pos %v %v: the release did not put the tab down", tc.pos, tc.dir)
		}
		tt.HandleMouseMove(core.MouseMoveEvent{X: screen(0), Y: y})
		if got := tabOrder(tt); got != "A Bravo Delta E C" {
			t.Errorf("pos %v %v: a move after the release moved it to %q", tc.pos, tc.dir, got)
		}
	}
}

// Until the strip has been painted after a move, what the mouse reads is
// where the tabs WERE, so no second move is judged against it.
func TestADragWaitsForThePaintAfterAMove(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	tt := moveStrip(t, TabsTop, core.DirLTR, true)
	paintCloseGrid(t, tt)
	cw := tt.EffectiveCellMetrics().UnitsPerCellWidth
	spans := map[int]stripSpan{}
	for _, sp := range tt.stripSpans {
		spans[sp.owner] = sp
	}
	tt.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: spans[2].x + cw/2, Y: 1})
	// Onto the start of Bravo: C and Bravo change places.
	tt.HandleMouseMove(core.MouseMoveEvent{X: spans[1].x + 1, Y: 1, Buttons: 1})
	if got := tabOrder(tt); got != "A C Bravo Delta E" {
		t.Fatalf("onto Bravo: %q", got)
	}
	// Onto A, as the strip was last painted: C now stands where Bravo was,
	// and the old parts would have it change places with A as well.
	tt.HandleMouseMove(core.MouseMoveEvent{X: spans[0].x + 1, Y: 1, Buttons: 1})
	if got := tabOrder(tt); got != "A C Bravo Delta E" {
		t.Errorf("a second move before the strip was painted again: %q, want it held", got)
	}
	paintCloseGrid(t, tt)
	tt.HandleMouseMove(core.MouseMoveEvent{X: spans[0].x + 1, Y: 1, Buttons: 1})
	if got := tabOrder(tt); got != "C A Bravo Delta E" {
		t.Errorf("the same move once painted: %q", got)
	}
}

// Nothing is picked up where there is nothing to drag: on a strip that is not
// movable, or where the press was a close button.
func TestNothingIsPickedUpThatIsNotATab(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	tt := moveStrip(t, TabsTop, core.DirLTR, false)
	paintCloseGrid(t, tt)
	tt.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: 1, Y: 1})
	if tt.dragTab != nil {
		t.Error("a strip that is not movable picked up a tab")
	}

	tt = moveStrip(t, TabsTop, core.DirLTR, true)
	tt.SetClosable(true)
	paintCloseGrid(t, tt)
	for _, sp := range tt.stripSpans {
		if sp.owner == 2 {
			tt.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: sp.closeX + 1, Y: 1})
		}
	}
	if tt.dragTab != nil || tt.closePressed == 0 {
		t.Errorf("a press on a close button picked up the tab (pressed %d)", tt.closePressed)
	}
}

// Down a movable side strip the sweep that would select carries the tab it
// began on instead, a row at a time.
func TestDraggingASideTabMovesItRowByRow(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	tt := moveStrip(t, TabsSide, core.DirLTR, true)
	m := tt.EffectiveCellMetrics()
	row := func(r int) core.Unit { return core.Unit(r)*m.UnitsPerCellHeight + m.UnitsPerCellHeight/2 }
	tt.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: 2 * m.UnitsPerCellWidth, Y: row(2)})
	tt.HandleMouseMove(core.MouseMoveEvent{X: 2 * m.UnitsPerCellWidth, Y: row(0), Buttons: 1})
	if got := tabOrder(tt); got != "C A Bravo Delta E" {
		t.Errorf("dragged up to the first row: %q", got)
	}
	tt.HandleMouseMove(core.MouseMoveEvent{X: 2 * m.UnitsPerCellWidth, Y: row(3), Buttons: 1})
	if got := tabOrder(tt); got != "A Bravo Delta C E" {
		t.Errorf("dragged down to the fourth row: %q", got)
	}
	tt.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton})

	still := moveStrip(t, TabsSide, core.DirLTR, false)
	still.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: 2 * m.UnitsPerCellWidth, Y: row(2)})
	still.HandleMouseMove(core.MouseMoveEvent{X: 2 * m.UnitsPerCellWidth, Y: row(0), Buttons: 1})
	if got := tabOrder(still); got != "A Bravo C Delta E" || still.CurrentIndex() != 0 {
		t.Errorf("a sweep down a strip that is not movable: %q with %d current, want the order kept and the first selected",
			got, still.CurrentIndex())
	}
}

// A move is told on the wire, with where the tab stood and where it stands.
func TestAMovedTabSaysSoOnTheWire(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	var events []*protocol.Event
	ctx := &protocol.BindContext{Emit: func(ev *protocol.Event) { events = append(events, ev) }}
	f := &captureFactory{inner: protocol.NewRegistryFactory(ctx)}
	script, err := protocol.Parse(`s=new tabs movable children={
		new tab caption="A" children={new panel}
		new tab caption="B" children={new panel}
		new tab caption="C" children={new panel}
	} selected=2`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := protocol.NewSession().Execute(script, f); err != nil {
		t.Fatal(err)
	}
	var tt *TabTrinket
	for _, tg := range f.targets {
		if tw, ok := tg.(*TabTrinket); ok {
			tt = tw
		}
	}
	if tt == nil || !tt.IsMovable() {
		t.Fatal("no movable strip was built")
	}
	f.Subscribe(trinketID(tt), "move")
	tt.SetFocus()
	tt.HandleKeyPress(core.KeyPressEvent{Key: "S-Left"})
	var got [][3]int
	for _, ev := range events {
		if ev.Type == "move" {
			id, _ := ev.Uint("trinket")
			from, _ := ev.Int("from")
			to, _ := ev.Int("to")
			got = append(got, [3]int{int(id), from, to})
		}
	}
	if len(got) != 1 || got[0] != [3]int{int(trinketID(tt)), 2, 1} {
		t.Errorf("move raised %v, want the strip, from 2 to 1", got)
	}
}

// A tab changes places with a neighbour only when the pointer, after the
// swap, would be over the dragged tab: C, narrower than Bravo and Delta,
// stays put while the pointer is merely over the near end of either, and
// changes places once it is within C's own width of the far end.
func TestADraggedTabWaitsUntilItWouldLandUnderThePointer(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	for _, tc := range []struct {
		neighbour int
		order     string
	}{
		{3, "A Bravo Delta C E"},
		{1, "A C Bravo Delta E"},
	} {
		tt := moveStrip(t, TabsTop, core.DirLTR, true)
		paintCloseGrid(t, tt)
		spans := map[int]stripSpan{}
		for _, sp := range tt.stripSpans {
			spans[sp.owner] = sp
		}
		own, over := spans[2], spans[tc.neighbour]
		// The first unit that moves C on, and the one just short of it; going
		// back, the last unit that moves it and the one just past.
		edge, short := over.x+over.w-own.w, over.x+over.w-own.w-1
		if tc.neighbour < 2 {
			edge, short = over.x+own.w-1, over.x+own.w
		}
		tt.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: own.x + 1, Y: 1})
		tt.HandleMouseMove(core.MouseMoveEvent{X: short, Y: 1, Buttons: 1})
		if got := tabOrder(tt); got != "A Bravo C Delta E" {
			t.Errorf("over tab %d just short of its edge, at %d: %q", tc.neighbour, short, got)
		}
		tt.HandleMouseMove(core.MouseMoveEvent{X: edge, Y: 1, Buttons: 1})
		if got := tabOrder(tt); got != tc.order {
			t.Errorf("over tab %d at its edge %d: %q, want %q", tc.neighbour, edge, got, tc.order)
		}
	}
}

// A tab the run was cut short at is brought into view by a press, not picked
// up: only part of it is there to carry.
func TestACutShortTabIsNotPickedUp(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	tt := moveStrip(t, TabsTop, core.DirLTR, true)
	m := tt.EffectiveCellMetrics()
	tt.SetBounds(core.UnitRect{Width: 20 * m.UnitsPerCellWidth, Height: 4 * m.UnitsPerCellHeight})
	paintCloseGridW(t, tt, 20)
	var cut stripSpan
	found := false
	for _, sp := range tt.stripSpans {
		if sp.owner >= 0 && sp.clipped {
			cut, found = sp, true
		}
	}
	if !found {
		t.Fatal("no tab was cut short")
	}
	tt.HandleMousePress(core.MousePressEvent{Button: core.LeftButton, X: cut.x + 1, Y: 1})
	if tt.dragTab != nil {
		t.Error("a tab cut short was picked up")
	}
}
