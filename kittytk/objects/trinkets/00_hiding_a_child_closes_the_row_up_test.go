package trinkets

// Showing or hiding a child changes where its siblings stand, and nothing else
// asks for the arrangement to be done again: the setter does.

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
)

func TestHidingAChildClosesTheRowUp(t *testing.T) {
	_, rs := row(t, "Deny", "Prompt", "Allow")
	where := rs[1].Bounds().X
	was := rs[2].Bounds().X

	rs[1].Hide()
	if got := rs[2].Bounds().X; got != where {
		t.Errorf("with the middle button hidden the last starts at %d, want %d where the middle one stood",
			got, where)
	}

	rs[1].Show()
	if got := rs[2].Bounds().X; got != was {
		t.Errorf("with the middle button shown again the last starts at %d, want %d", got, was)
	}
	fits(t, rs)
}

// Down a column the same: hiding a row of a form moves the rows below it up.
func TestHidingAChildMovesTheRowsBelowUp(t *testing.T) {
	p := NewPanel()
	p.SetLayoutManager(layout.NewVBoxLayout())
	var ls []*Label
	for _, c := range []string{"One", "Two", "Three"} {
		l := NewLabel(c)
		ls = append(ls, l)
		p.AddChild(l)
	}
	p.SetBounds(core.UnitRect{Width: 400, Height: 400})
	where := ls[1].Bounds().Y

	ls[1].SetVisible(false)
	if got := ls[2].Bounds().Y; got != where {
		t.Errorf("with the middle label hidden the last is at %d, want %d", got, where)
	}
}

// Setting what is already so arranges nothing: a panel rebuilt on every
// refresh that shows what is already shown is not re-laid out each time.
func TestSettingTheSameVisibilityArrangesNothing(t *testing.T) {
	p, rs := row(t, "Deny", "Prompt", "Allow")
	counter := &countingLayout{LayoutManager: p.LayoutManager()}
	p.SetLayoutManager(counter)
	counter.n = 0

	rs[1].Show()
	if counter.n != 0 {
		t.Errorf("showing a shown child arranged the panel %d times", counter.n)
	}
	rs[1].Hide()
	if counter.n == 0 {
		t.Errorf("hiding a shown child did not arrange the panel")
	}
}

// countingLayout counts the passes a panel asks of its layout.
type countingLayout struct {
	core.LayoutManager
	n int
}

func (c *countingLayout) Layout(container core.Container, bounds core.UnitRect) {
	c.n++
	c.LayoutManager.Layout(container, bounds)
}
