package trinkets

// A trinket sized by its own content changes size when that content changes,
// and what stands beside it has to move. Nothing asks for it: the setter does.

import (
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/objects/window"
)

// row builds a horizontal row of radio buttons at a width they all fit in.
func row(t *testing.T, captions ...string) (*Panel, []*RadioButton) {
	t.Helper()
	p := NewPanel()
	p.SetLayoutManager(layout.NewHBoxLayout())
	var rs []*RadioButton
	for _, c := range captions {
		r := NewRadioButton(c)
		rs = append(rs, r)
		p.AddChild(r)
	}
	p.SetBounds(core.UnitRect{Width: 1200, Height: 16})
	return p, rs
}

// fits reports the first thing wrong with a run: a trinket drawn wider than
// the room it was given, or one starting inside the one before it.
func fits(t *testing.T, rs []*RadioButton) {
	t.Helper()
	for i, r := range rs {
		b := r.Bounds()
		if want := r.SizeHint().Width; b.Width < want {
			t.Errorf("%q sits in %d units and needs %d", r.Text(), b.Width, want)
		}
		if i > 0 {
			prev := rs[i-1].Bounds()
			if b.X < prev.X+prev.Width {
				t.Errorf("%q starts at %d, inside %q which runs to %d",
					r.Text(), b.X, rs[i-1].Text(), prev.X+prev.Width)
			}
		}
	}
}

// A caption replaced with a longer one moves the buttons after it along.
func TestALongerCaptionMovesWhatFollowsIt(t *testing.T) {
	p, rs := row(t, "Deny", "Prompt", "Allow")
	fits(t, rs)
	before := rs[2].Bounds().X

	rs[1].SetText("Follow Host Rule")
	fits(t, rs)
	if rs[2].Bounds().X <= before {
		t.Errorf("the button after the one that grew still starts at %d",
			rs[2].Bounds().X)
	}
	if p.Bounds().Width != 1200 {
		t.Errorf("the row itself changed size: %+v", p.Bounds())
	}
}

// And a shorter one closes the gap back up, so a run does not creep wider
// every time it is written to.
func TestAShorterCaptionClosesTheGap(t *testing.T) {
	_, rs := row(t, "Deny", "Follow Host Rule", "Allow")
	wide := rs[2].Bounds().X

	rs[1].SetText("Prompt")
	fits(t, rs)
	if rs[2].Bounds().X >= wide {
		t.Errorf("the run kept the width of the words it no longer holds: %d", wide)
	}
}

// The arrangement is done again from the OUTERMOST container that arranges
// anything, not merely the nearest: a child that grows changes the size its
// parent asks for, and its parent's parent after that.
func TestTheWholeArrangementIsDoneAgainNotJustTheNearest(t *testing.T) {
	inner := NewPanel()
	inner.SetLayoutManager(layout.NewHBoxLayout())
	grew := NewLabel("short")
	inner.AddChild(grew)

	beside := NewLabel("beside")
	outer := NewPanel()
	outer.SetLayoutManager(layout.NewHBoxLayout())
	outer.AddChild(inner)
	outer.AddChild(beside)
	outer.SetBounds(core.UnitRect{Width: 1200, Height: 16})

	at := beside.Bounds().X
	grew.SetText("a caption long enough to push its way along the row")

	if beside.Bounds().X <= at {
		t.Errorf("the label in the OUTER row still starts at %d, so only the "+
			"inner row was arranged again", beside.Bounds().X)
	}
	if w := inner.Bounds().Width; w < grew.SizeHint().Width {
		t.Errorf("the inner row is %d units around a label needing %d",
			w, grew.SizeHint().Width)
	}
}

// The walk stops at the window: a window is the size it was given, not the
// size of what it holds, so a caption growing inside one rearranges the window
// and leaves the window itself where it is.
func TestAWindowIsNotResizedByWhatIsInsideIt(t *testing.T) {
	win := window.NewWindow("Fixed")
	content, rs := row(t, "Deny", "Prompt", "Allow")
	win.SetContent(content)
	win.SetBounds(core.UnitRect{X: 40, Y: 40, Width: 400, Height: 200})
	before := win.Bounds()

	rs[1].SetText("Follow Host Rule and then some")

	if got := win.Bounds(); got != before {
		t.Errorf("the window moved or resized itself: %+v -> %+v", before, got)
	}
}

// And the walk stops there rather than climbing out of the window: a caption
// inside one is no reason to arrange the desktop around it. The desktop's own
// chrome is put somewhere it would never choose, and left there.
func TestACaptionInsideAWindowDoesNotRearrangeTheDesktop(t *testing.T) {
	d := NewDesktop()
	d.SetBounds(core.UnitRect{Width: 8000, Height: 4000})

	win := window.NewWindow("Fixed")
	content, rs := row(t, "Deny", "Prompt", "Allow")
	win.SetContent(content)
	win.SetBounds(core.UnitRect{X: 40, Y: 40, Width: 400, Height: 200})
	win.SetParent(d)

	bar := d.StatusBar()
	if bar == nil {
		t.Skip("this desktop has no status bar to watch")
	}
	moved := core.UnitRect{X: 111, Y: 222, Width: 333, Height: 16}
	bar.SetBounds(moved)

	rs[1].SetText("Follow Host Rule and then some")

	if got := bar.Bounds(); got != moved {
		t.Errorf("the desktop arranged itself again because a caption inside a "+
			"window changed: status bar %+v -> %+v", moved, got)
	}
}

// A trinket nobody has put anywhere has nothing to arrange, and says so by
// doing nothing rather than by panicking.
func TestATrinketWithNoParentIsNoTrouble(t *testing.T) {
	NewLabel("free").SetText("still free")
	NewRadioButton("free").SetText("still free")
	NewCheckbox("free").SetText("still free")
	NewButton("free").SetText("still free")
}

// A layout that writes to a child while it is arranging asks, through that
// child's setter, for the arrangement to be done again from inside the one
// already running. The pass in progress is the one doing that work, so it
// finishes rather than starting over inside itself.
type writingLayout struct {
	*layout.BoxLayout
	write func()
	runs  int
}

func (l *writingLayout) Layout(container core.Container, bounds core.UnitRect) {
	l.runs++
	if l.runs > 1000 {
		return // the guard failed; unwind rather than hang the suite
	}
	l.BoxLayout.Layout(container, bounds)
	l.write()
}

func TestARewriteDuringLayoutDoesNotSpiral(t *testing.T) {
	label := NewLabel("first")
	writes := 0
	lm := &writingLayout{BoxLayout: layout.NewHBoxLayout()}
	lm.write = func() {
		writes++
		label.SetText("rewritten " + string(rune('a'+writes%26)))
	}

	p := NewPanel()
	p.SetLayoutManager(lm)
	p.AddChild(label)

	done := make(chan struct{})
	go func() {
		p.SetBounds(core.UnitRect{Width: 400, Height: 16})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("laying out a row whose layout writes to a child never finished")
	}
	if writes == 0 {
		t.Fatal("the layout never wrote to the child, so nothing was proved")
	}
	if lm.runs > 2 {
		t.Errorf("the arrangement ran %d times over one write", lm.runs)
	}
}
