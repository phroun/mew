package trinkets

import (
	"fmt"
	"strings"
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// closeGrid is a cell surface that keeps what each cell shows, and which cells
// wear the focus colours.
type closeGrid struct {
	core.RenderBackend
	cw, ch  core.Unit
	focusFg style.Color
	rows    [][]rune
	focus   [][]bool
	styles  map[[2]int]style.CellStyle
}

func (g *closeGrid) GraphicalMode() bool { return false }
func (g *closeGrid) put(x, y core.Unit, r rune, s style.CellStyle) {
	cx, cy := int(x/g.cw), int(y/g.ch)
	if cy >= 0 && cy < len(g.rows) && cx >= 0 && cx < len(g.rows[cy]) {
		g.rows[cy][cx] = r
		g.focus[cy][cx] = s.Fg == g.focusFg
		g.styles[[2]int{cx, cy}] = s
	}
}
func (g *closeGrid) DrawCell(x, y core.Unit, r rune, s style.CellStyle) { g.put(x, y, r, s) }
func (g *closeGrid) DrawText(x, y core.Unit, t string, s style.CellStyle, f *core.Font) core.Unit {
	for i, r := range []rune(t) {
		g.put(x+core.Unit(i)*g.cw, y, r, s)
	}
	return core.Unit(len([]rune(t))) * g.cw
}
func (g *closeGrid) FillRect(rect core.UnitRect, r rune, s style.CellStyle) {
	for y := rect.Y; y < rect.Y+rect.Height; y += g.ch {
		for x := rect.X; x < rect.X+rect.Width; x += g.cw {
			g.put(x, y, r, s)
		}
	}
}

// row is what row y shows, and a line under it marking the cells in the focus
// colours with ^.
func (g *closeGrid) row(y int) (string, string) {
	marks := make([]rune, len(g.focus[y]))
	for x, f := range g.focus[y] {
		marks[x] = ' '
		if f {
			marks[x] = '^'
		}
	}
	return strings.TrimRight(string(g.rows[y]), " "), strings.TrimRight(string(marks), " ")
}

const closeGridCols = 40

// closeStrip is four tabs with the third selected, on a strip 40 cells wide.
func closeStrip(t *testing.T, pos TabPosition, dir core.Direction, closable bool) *TabTrinket {
	t.Helper()
	core.SetTextMeasurer(nil)
	form := NewPanel()
	form.SetDirection(dir)
	tt := NewTabTrinket()
	tt.SetTabPosition(pos)
	tt.SetClosable(closable)
	form.AddChild(tt)
	for _, n := range []string{"Grid", "Flex", "Limit", "Fixed"} {
		tt.AddTab(n, NewPanel())
	}
	tt.SetCurrentIndex(2)
	m := tt.EffectiveCellMetrics()
	tt.SetBounds(core.UnitRect{Width: closeGridCols * m.UnitsPerCellWidth, Height: 5 * m.UnitsPerCellHeight})
	return tt
}

func paintCloseGrid(t *testing.T, tt *TabTrinket) *closeGrid {
	t.Helper()
	return paintCloseGridW(t, tt, closeGridCols)
}

func paintCloseGridW(t *testing.T, tt *TabTrinket, cols int) *closeGrid {
	t.Helper()
	px, err := raster.New(600, 300)
	if err != nil {
		t.Fatal(err)
	}
	m := tt.EffectiveCellMetrics()
	g := &closeGrid{RenderBackend: px, cw: m.UnitsPerCellWidth, ch: m.UnitsPerCellHeight,
		focusFg: tt.GetScheme().GetFocusedTab().Fg, styles: map[[2]int]style.CellStyle{}}
	for i := 0; i < 5; i++ {
		g.rows = append(g.rows, []rune(strings.Repeat(" ", cols)))
		g.focus = append(g.focus, make([]bool, cols))
	}
	tt.Paint(core.NewPainter(g))
	return g
}

// A close button stands in the cell just after its tab's label, the cell the
// focus marker uses on the selected tab, and covers none of the label. The
// strip keeps its width, and a strip without close buttons is as it was.
//
// While the strip has the keyboard the selected tab shows its markers, and the
// marker is what stands in that cell. On the button's own stop the tab looks
// unfocused and only the button wears the focus colours.
func TestATabsCloseButtonStandsAfterItsLabel(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	for _, tc := range []struct {
		pos        TabPosition
		row        int
		closable   bool
		focus      bool
		closeFocus bool
		want, fmks string
	}{
		{TabsTop, 0, false, false, false, `  Grid  Flex_/ Limit \_Fixed`, ``},
		{TabsTop, 0, true, false, false, `  Grid× Flex×/ Limit×\_Fixed×`, ``},
		{TabsTop, 0, false, true, false, `  Grid  Flex_/<Limit>\_Fixed`, `              ^^^^^^^`},
		{TabsTop, 0, true, true, false, `  Grid× Flex×/<Limit>\_Fixed×`, `              ^^^^^^^`},
		{TabsTop, 0, true, true, true, `  Grid× Flex×/ Limit×\_Fixed×`, `                    ^`},
		{TabsBottom, 4, false, false, false, `  Grid  Flex \_Limit_/ Fixed`, ``},
		{TabsBottom, 4, true, false, false, `  Grid× Flex×\_Limit×/ Fixed×`, ``},
		{TabsBottom, 4, true, true, false, `  Grid× Flex×\<Limit>/ Fixed×`, `              ^^^^^^^`},
		{TabsBottom, 4, true, true, true, `  Grid× Flex×\_Limit×/ Fixed×`, `                    ^`},
	} {
		tt := closeStrip(t, tc.pos, core.DirLTR, tc.closable)
		if tc.focus {
			tt.SetFocus()
		}
		if tc.closeFocus {
			tt.FocusArrivingBackward()
		}
		got, marks := paintCloseGrid(t, tt).row(tc.row)
		if got != tc.want || marks != tc.fmks {
			t.Errorf("pos %v closable %v focus %v on the button %v:\n got  %q\n      %q\n want %q\n      %q",
				tc.pos, tc.closable, tc.focus, tc.closeFocus, got, marks, tc.want, tc.fmks)
		}
	}
}

// A side strip's close buttons stand in one column, in the last cell inside
// the padding at the end the strip reads towards. Where that end is the far
// one from where the labels start, the labels stay where they were; where it is
// the near one, they move over by the button's cell.
func TestASideStripsCloseButtonsStandInAColumn(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	for _, tc := range []struct {
		pos  TabPosition
		dir  core.Direction
		want []string
	}{
		{TabsSide, core.DirLTR, []string{` Grid       ×`, ` Flex       ×`, ` Limit      ×`, ` Fixed      ×`}},
		{TabsSideOpposite, core.DirLTR, []string{
			`                           Grid       ×`, `                           Flex       ×`,
			`                           Limit      ×`, `                           Fixed      ×`}},
		{TabsSide, core.DirRTL, []string{
			`                           ×Grid`, `                           ×Flex`,
			`                           ×Limit`, `                           ×Fixed`}},
	} {
		tt := closeStrip(t, tc.pos, tc.dir, true)
		g := paintCloseGrid(t, tt)
		for y, want := range tc.want {
			if got, _ := g.row(y); got != want {
				t.Errorf("pos %v %v row %d: %q, want %q", tc.pos, tc.dir, y, got, want)
			}
		}

		// The button's stop puts the focus colours on the button alone.
		tt.SetFocus()
		tt.FocusArrivingBackward()
		_, marks := paintCloseGrid(t, tt).row(2)
		button := len([]rune(tc.want[2][:strings.Index(tc.want[2], "×")]))
		if want := strings.Repeat(" ", button) + "^"; marks != want {
			t.Errorf("pos %v %v: focus on the button marks %q, want %q", tc.pos, tc.dir, marks, want)
		}
	}
}

// The current tab's close button is a stop of its own, one after the strip:
// Tab reaches it, Shift+Tab goes back, a Tab from it goes on past the strip,
// and Space presses it. A strip whose current tab has no button lets Tab go
// on as it always did, and walking the tabs takes the keyboard back to them.
func TestTheCloseButtonIsTheStopAfterTheStrip(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	key := func(tt *TabTrinket, k string) bool { return tt.HandleKeyPress(core.KeyPressEvent{Key: k}) }

	tt := closeStrip(t, TabsTop, core.DirLTR, false)
	tt.SetFocus()
	if key(tt, "Tab") || tt.closeFocusShown() {
		t.Error("Tab on a strip without close buttons stayed in the strip")
	}

	tt = closeStrip(t, TabsTop, core.DirLTR, true)
	closed := -1
	tt.SetOnTabCloseRequested(func(i int) { closed = i })
	tt.SetFocus()
	if !key(tt, "Tab") || !tt.closeFocusShown() {
		t.Fatal("Tab on the strip did not reach the close button")
	}
	if !key(tt, "S-Tab") || tt.closeFocusShown() {
		t.Error("Shift+Tab on the close button did not go back to the tab")
	}
	key(tt, "Tab")
	if key(tt, "Tab") || tt.closeFocusShown() {
		t.Error("Tab on the close button did not go on past the strip")
	}
	if key(tt, "Space") || closed != -1 {
		t.Errorf("Space on the strip itself closed tab %d", closed)
	}
	key(tt, "Tab")
	if !key(tt, "Space") || closed != 2 {
		t.Errorf("Space on the close button closed tab %d, want 2", closed)
	}
	if !key(tt, "Right") || tt.closeFocusShown() || tt.CurrentIndex() != 3 {
		t.Errorf("the right arrow on the close button left the button focused %v and tab %d current",
			tt.closeFocusShown(), tt.CurrentIndex())
	}

	// Shift+Tab from what follows reaches the button first, and losing the
	// focus forgets it.
	tt.FocusArrivingBackward()
	if !tt.closeFocusShown() {
		t.Error("focus arriving backwards did not land on the close button")
	}
	tt.ClearFocus()
	if tt.closeFocus {
		t.Error("losing the focus left the close button focused")
	}
}

// A press on the cell just after a label closes that tab, on a horizontal
// strip; on a side strip it is the button's own column. The label itself
// selects, as it always did.
func TestAPressOnACloseButtonClosesItsTab(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	for _, tc := range []struct {
		pos       TabPosition
		x, y      int // the cell pressed
		wantClose int
		wantSel   int
	}{
		{TabsTop, 6, 0, 0, 2},     // "  Grid×"
		{TabsTop, 5, 0, -1, 0},    // the label's last cell
		{TabsTop, 7, 0, -1, 1},    // the separator past the button
		{TabsBottom, 12, 4, 1, 2}, // "  Grid× Flex×"
		{TabsSide, 12, 1, 1, 2},
		{TabsSide, 4, 1, -1, 1},
		{TabsSide, 13, 1, -1, 1}, // the padding past the button
		{TabsSideOpposite, 38, 3, 3, 2},
	} {
		tt := closeStrip(t, tc.pos, core.DirLTR, true)
		closed := -1
		tt.SetOnTabCloseRequested(func(i int) { closed = i })
		paintCloseGrid(t, tt)
		m := tt.EffectiveCellMetrics()
		tt.HandleMousePress(core.MousePressEvent{Button: core.LeftButton,
			X: core.Unit(tc.x)*m.UnitsPerCellWidth + m.UnitsPerCellWidth/2,
			Y: core.Unit(tc.y)*m.UnitsPerCellHeight + m.UnitsPerCellHeight/2})
		if closed != tc.wantClose || tt.CurrentIndex() != tc.wantSel {
			t.Errorf("pos %v cell %d,%d: closed %d and selected %d, want closed %d and selected %d",
				tc.pos, tc.x, tc.y, closed, tt.CurrentIndex(), tc.wantClose, tc.wantSel)
		}
	}
}

// On a pixel surface the button under the pointer wears the hover colours; a
// cell surface draws it as it always does.
func TestTheCloseButtonUnderThePointerLightsUp(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	tt := closeStrip(t, TabsTop, core.DirLTR, true)
	g := paintCloseGrid(t, tt)
	m := tt.EffectiveCellMetrics()
	tt.HandleMouseMove(core.MouseMoveEvent{X: 6*m.UnitsPerCellWidth + 1, Y: 1})
	if tt.closeHover != 1 {
		t.Fatalf("the pointer over the first tab's button hovers %d", tt.closeHover)
	}
	hovered := tt.GetScheme().GetHoveredTabsButton()
	plain := g.styles[[2]int{6, 0}]

	px, err := raster.New(600, 300)
	if err != nil {
		t.Fatal(err)
	}
	gp := core.NewPainter(&pixelSurface{px})
	if got := tt.closeStyle(gp, 0, plain, plain); got != hovered {
		t.Errorf("on a pixel surface the hovered button is drawn %+v, want %+v", got, hovered)
	}
	if got := tt.closeStyle(gp, 1, plain, plain); got != plain {
		t.Errorf("on a pixel surface a button not under the pointer is drawn %+v", got)
	}
	g = paintCloseGrid(t, tt)
	if got := g.styles[[2]int{6, 0}]; got != plain {
		t.Errorf("on a cell surface the hovered button is drawn %+v, want %+v as before", got, plain)
	}

	tt.HandleMouseMove(core.MouseMoveEvent{X: 2*m.UnitsPerCellWidth + 1, Y: 1})
	if tt.closeHover != 0 {
		t.Errorf("the pointer over a label still hovers button %d", tt.closeHover)
	}
}

type pixelSurface struct{ core.RenderBackend }

func (pixelSurface) GraphicalMode() bool { return true }

// Close buttons change nothing on a strip but the cells they stand in: across
// widths, scroll positions, selections and focus, every other cell reads as it
// does without them. Each tab drawn whole shows its button just after its
// label, unless the strip's ellipsis was drawn over it or the focus marker
// stands there; and a press on that cell closes the tab only when the button
// shows.
func TestCloseButtonsTouchNoOtherCell(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)
	build := func(pos TabPosition, w, sel, off int, closable, focus bool) *TabTrinket {
		tt := NewTabTrinket()
		tt.SetTabPosition(pos)
		tt.SetClosable(closable)
		for i := 0; i < 9; i++ {
			tt.AddTab(fmt.Sprintf("T%d", i), NewPanel())
		}
		tt.currentIndex = sel
		m := tt.EffectiveCellMetrics()
		tt.SetBounds(core.UnitRect{Width: core.Unit(w) * m.UnitsPerCellWidth, Height: 5 * m.UnitsPerCellHeight})
		tt.tabScrollOffset = off
		if focus {
			tt.SetFocus()
		}
		return tt
	}
	covered := 0
	for _, pos := range []TabPosition{TabsTop, TabsBottom} {
		row := 0
		if pos == TabsBottom {
			row = 4
		}
		for w := 14; w < 60; w++ {
			for sel := -1; sel < 9; sel++ {
				for off := 0; off < 4; off++ {
					for _, focus := range []bool{false, true} {
						plain, _ := paintCloseGridW(t, build(pos, w, sel, off, false, focus), w).row(row)
						tt := build(pos, w, sel, off, true, focus)
						got, _ := paintCloseGridW(t, tt, w).row(row)
						p, g := []rune(padTo(plain, w)), []rune(padTo(got, w))
						for x := range g {
							if g[x] != p[x] && g[x] != '×' {
								t.Fatalf("pos %v width %d selected %d scrolled %d focus %v: with close buttons\n %q\nwithout\n %q",
									pos, w, sel, off, focus, got, plain)
							}
						}
						cw := tt.EffectiveCellMetrics().UnitsPerCellWidth
						for _, sp := range tt.stripSpans {
							cx := int(sp.labelEnd / cw)
							if sp.owner < 0 || cx >= w {
								continue
							}
							shows := g[cx] == '×'
							switch {
							case !sp.clipped && !shows && g[cx] != '.' && g[cx] != '>' && sp.x+sp.w > sp.labelEnd:
								t.Fatalf("pos %v width %d selected %d scrolled %d focus %v: tab %d shows no button after its label\n %q",
									pos, w, sel, off, focus, sp.owner, got)
							case g[cx] == '.':
								covered++
							}
							closed := -1
							tt.SetOnTabCloseRequested(func(i int) { closed = i })
							tt.handleTabBarPress(sp.labelEnd + cw/2)
							if shows != (closed == sp.owner) {
								t.Fatalf("pos %v width %d selected %d scrolled %d focus %v: a press after tab %d's label "+
									"closed %d, and the cell shows %q\n %q", pos, w, sel, off, focus, sp.owner, closed, g[cx], got)
							}
							tt.currentIndex = sel
							tt.tabScrollOffset = off
						}
					}
				}
			}
		}
	}
	if covered == 0 {
		t.Error("no strip drew its ellipsis over a close button, so that case went unchecked")
	}
}

func padTo(s string, n int) string {
	if r := []rune(s); len(r) < n {
		return s + strings.Repeat(" ", n-len(r))
	}
	return s
}

// Hover is a no-button affordance, on a side strip as on a horizontal one: a
// drag passing over a button does not light it.
func TestCloseHoverWantsNoButtonHeld(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	for _, tc := range []struct {
		pos  TabPosition
		x, y int
		want int
	}{
		{TabsTop, 6, 0, 1},
		{TabsSide, 12, 1, 2},
		{TabsSide, 4, 1, 0},
		{TabsSideOpposite, 38, 3, 4},
	} {
		tt := closeStrip(t, tc.pos, core.DirLTR, true)
		paintCloseGrid(t, tt)
		m := tt.EffectiveCellMetrics()
		at := core.MouseMoveEvent{X: core.Unit(tc.x)*m.UnitsPerCellWidth + 1, Y: core.Unit(tc.y)*m.UnitsPerCellHeight + 1}
		held := at
		held.Buttons = 1
		tt.HandleMouseMove(held)
		if tt.closeHover != 0 {
			t.Errorf("pos %v cell %d,%d: a drag passing over hovers %d", tc.pos, tc.x, tc.y, tt.closeHover)
		}
		tt.HandleMouseMove(at)
		if tt.closeHover != tc.want {
			t.Errorf("pos %v cell %d,%d: hovers %d, want %d", tc.pos, tc.x, tc.y, tt.closeHover, tc.want)
		}
	}
}

// A side tab's label gives up the button's cell and no more.
func TestASideLabelGivesUpTheButtonsCell(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	tt := closeStrip(t, TabsSide, core.DirLTR, true)
	cw := tt.EffectiveCellMetrics().UnitsPerCellWidth
	x0, room := tt.sideTabRoom(0, 14*cw, false)
	cx0, croom := tt.sideTabRoom(0, 14*cw, true)
	if cx0 != x0 || croom != room-cw {
		t.Errorf("a closable slot leaves the label %d from %d, want %d from %d", croom, cx0, room-cw, x0)
	}
}

// The button's stop holds only while the strip has the keyboard and the
// current tab has a button: told the focus is coming from behind, a strip it
// never reaches shows no focus on its button, a strip without buttons does not
// take the stop when buttons arrive later, and a stop on a tab that goes away
// does not pass to a tab without one.
func TestTheButtonsStopNeedsTheFocusAndAButton(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	tt := closeStrip(t, TabsTop, core.DirLTR, true)
	tt.FocusArrivingBackward()
	if _, marks := paintCloseGrid(t, tt).row(0); tt.closeFocusShown() || marks != "" {
		t.Errorf("a strip without the focus shows focus %q on its button", marks)
	}

	tt = closeStrip(t, TabsTop, core.DirLTR, false)
	tt.SetFocus()
	tt.FocusArrivingBackward()
	tt.SetClosable(true)
	if tt.closeFocusShown() {
		t.Error("a strip that had no buttons when the focus came back took the button's stop once they arrived")
	}

	tt = closeStrip(t, TabsTop, core.DirLTR, false)
	tt.Tab(2).Closable = true
	tt.SetOnTabCloseRequested(func(i int) { tt.RemoveTab(i) })
	tt.SetFocus()
	tt.HandleKeyPress(core.KeyPressEvent{Key: "Tab"})
	tt.HandleKeyPress(core.KeyPressEvent{Key: "Space"})
	if tt.closeFocusShown() {
		t.Errorf("closing the only closable tab left the stop on tab %d, which has no button", tt.CurrentIndex())
	}
	if got, marks := paintCloseGrid(t, tt).row(0); !strings.Contains(got, "<") || strings.Contains(got, "×") {
		t.Errorf("after closing the only closable tab the strip reads %q, focus %q", got, marks)
	}
}

// The pointer above a side strip lights no button.
func TestNoSideButtonLightsFromAbove(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	tt := closeStrip(t, TabsSide, core.DirLTR, true)
	paintCloseGrid(t, tt)
	m := tt.EffectiveCellMetrics()
	tt.HandleMouseMove(core.MouseMoveEvent{X: 12*m.UnitsPerCellWidth + 1, Y: -1})
	if tt.closeHover != 0 {
		t.Errorf("the pointer above the strip hovers %d", tt.closeHover)
	}
}

// The last tab can run out of strip with nothing missing but the room after
// it. It is drawn whole, button and all, and its button closes it.
func TestALastTabDrawnWholeKeepsItsButton(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	core.SetTextMeasurer(nil)
	for _, tc := range []struct {
		w, sel int
		want   string
	}{
		{17, -1, `... T7× T8×[<] >`},
		{19, 8, `... T7×/ T8×\[<] >`},
	} {
		tt := NewTabTrinket()
		tt.SetClosable(true)
		for i := 0; i < 9; i++ {
			tt.AddTab(fmt.Sprintf("T%d", i), NewPanel())
		}
		tt.currentIndex = tc.sel
		m := tt.EffectiveCellMetrics()
		tt.SetBounds(core.UnitRect{Width: core.Unit(tc.w) * m.UnitsPerCellWidth, Height: 5 * m.UnitsPerCellHeight})
		tt.tabScrollOffset = 7
		if got, _ := paintCloseGridW(t, tt, tc.w).row(0); got != tc.want {
			t.Errorf("width %d selected %d: %q, want %q", tc.w, tc.sel, got, tc.want)
		}
		closed := -1
		tt.SetOnTabCloseRequested(func(i int) { closed = i })
		button := core.Unit(len([]rune(tc.want[:strings.LastIndex(tc.want, "×")])))
		tt.handleTabBarPress(button*m.UnitsPerCellWidth + m.UnitsPerCellWidth/2)
		if closed != 8 {
			t.Errorf("width %d selected %d: a press on the last button closed %d", tc.w, tc.sel, closed)
		}
	}
}

// A side strip too short for its tabs lights no button for a tab scrolled out
// of sight below it.
func TestNoHiddenSideButtonLights(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	tt := closeStrip(t, TabsSide, core.DirLTR, true)
	m := tt.EffectiveCellMetrics()
	tt.SetBounds(core.UnitRect{Width: closeGridCols * m.UnitsPerCellWidth, Height: 2 * m.UnitsPerCellHeight})
	// Scrolled one along to show the current tab, row 2 is where tab 3 would be.
	tt.HandleMouseMove(core.MouseMoveEvent{X: 12*m.UnitsPerCellWidth + 1, Y: 2*m.UnitsPerCellHeight + 1})
	if tt.closeHover != 0 {
		t.Errorf("the pointer below the visible tabs hovers %d", tt.closeHover)
	}
}
