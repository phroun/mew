package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/protocol"
)

// layoutOf builds one script and returns the first target's layout manager.
func layoutOf(t *testing.T, src string) (*captureFactory, core.LayoutManager) {
	t.Helper()
	f, _ := buildUI(t, nil, src)
	return f, f.targets[0].(*Panel).LayoutManager()
}

// Both managers are reachable from a script now, and each is the one it says.
func TestPanelBuildsEveryLayout(t *testing.T) {
	for _, c := range []struct {
		word string
		want string
	}{
		{"vbox", "*layout.BoxLayout"},
		{"hbox", "*layout.BoxLayout"},
		{"grid", "*layout.GridLayout"},
		{"flex", "*layout.FlexLayout"},
	} {
		_, lm := layoutOf(t, `new panel layout=`+c.word)
		if got := typeName(lm); got != c.want {
			t.Errorf("layout=%s built %s, want %s", c.word, got, c.want)
		}
	}

	script, _ := protocol.Parse(`new panel layout=nosuch`)
	_, err := protocol.NewSession().Execute(script, protocol.NewRegistryFactory(nil))
	if err == nil || !strings.Contains(err.Error(), "nosuch") {
		t.Errorf("an unknown layout gave %v, want a refusal naming it", err)
	}
}

func typeName(v any) string {
	switch v.(type) {
	case *layout.BoxLayout:
		return "*layout.BoxLayout"
	case *layout.GridLayout:
		return "*layout.GridLayout"
	case *layout.FlexLayout:
		return "*layout.FlexLayout"
	}
	return "nil"
}

// A grid child's cell travels on the child, and the grid it is attached to
// places it there -- which is what makes a grid buildable from a script at all.
func TestGridCellsFromAScript(t *testing.T) {
	f, _ := buildUI(t, nil, `
g=new panel layout=grid spacing=0 children={
	new label caption="Name" row=0 column=0
	new textinput row=0 column=1 column_stretch=1
	new label caption="Notes" row=1 column=0
	new textinput row=1 column=1 column_stretch=1
}
`)
	g := f.targets[0].(*Panel)
	g.SetBounds(core.UnitRect{Width: 400, Height: 200})
	g.Layout()

	name := f.targets[1].(*Label).Bounds()
	field := f.targets[2].(*TextInput).Bounds()
	notes := f.targets[3].(*Label).Bounds()

	if name.Y != field.Y {
		t.Errorf("the label and its field are on rows y=%d and y=%d", name.Y, field.Y)
	}
	if field.X <= name.X {
		t.Errorf("the field is at x=%d, not right of its label at x=%d", field.X, name.X)
	}
	if notes.Y <= name.Y {
		t.Errorf("the second row is at y=%d, not below the first at y=%d", notes.Y, name.Y)
	}
	if notes.X != name.X {
		t.Errorf("the two labels are in columns x=%d and x=%d", name.X, notes.X)
	}
	if field.Width <= name.Width {
		t.Errorf("the stretching column is %d wide against the label column's %d", field.Width, name.Width)
	}
}

// The flex knobs reach the layout, and asking for one before there is a flex
// layout to set it on is refused rather than quietly dropped.
func TestFlexPropertiesFromAScript(t *testing.T) {
	_, lm := layoutOf(t, `new panel layout=flex flex_direction=column flex_wrap=wrap justify=center align_items=begin`)
	fl, ok := lm.(*layout.FlexLayout)
	if !ok {
		t.Fatalf("layout manager is %T, want a flex layout", lm)
	}
	if fl.Direction() != layout.FlexColumn {
		t.Errorf("flex_direction=column gave %v", fl.Direction())
	}
	if fl.Wrap() != layout.FlexWrapNormal {
		t.Errorf("flex_wrap=wrap gave %v", fl.Wrap())
	}
	if fl.Justify() != layout.FlexJustifyCenter {
		t.Errorf("justify=center gave %v", fl.Justify())
	}
	if fl.AlignItems() != layout.FlexAlignStart {
		t.Errorf("align_items=begin gave %v", fl.AlignItems())
	}

	for _, src := range []string{
		`new panel layout=vbox flex_wrap=wrap`,
		`new panel flex_direction=row`,
	} {
		script, _ := protocol.Parse(src)
		_, err := protocol.NewSession().Execute(script, protocol.NewRegistryFactory(nil))
		if err == nil || !strings.Contains(err.Error(), "layout=flex") {
			t.Errorf("%s gave %v, want a refusal asking for layout=flex first", src, err)
		}
	}
}

// A flex child's hints travel on the child and reach the line it is laid out in.
func TestFlexHintsFromAScript(t *testing.T) {
	f, _ := buildUI(t, nil, `
p=new panel layout=flex spacing=0 children={
	new label caption="a" grow=1
	new label caption="b" grow=3
}
`)
	p := f.targets[0].(*Panel)
	p.SetBounds(core.UnitRect{Width: 400, Height: 40})
	p.Layout()

	one := f.targets[1].(*Label).Bounds().Width
	three := f.targets[2].(*Label).Bounds().Width
	if three <= one {
		t.Fatalf("grow=3 got %d against grow=1's %d", three, one)
	}

	// A child that says nothing about growing takes none of the leftover.
	f, _ = buildUI(t, nil, `
p=new panel layout=flex spacing=0 children={
	new label caption="a"
	new label caption="b" grow=1
}
`)
	p = f.targets[0].(*Panel)
	p.SetBounds(core.UnitRect{Width: 400, Height: 40})
	p.Layout()
	plain := f.targets[1].(*Label)
	if got, want := plain.Bounds().Width, plain.SizeHint().Width; got != want {
		t.Errorf("a child with no grow is %d wide, want its own %d", got, want)
	}
}

// Every one of the hints refuses a value outside its domain.
func TestLayoutHintsCheckTheirValues(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`new label caption="x" row=-1`, "below 0"},
		{`new label caption="x" column=-1`, "below 0"},
		{`new label caption="x" row_span=0`, "below 1"},
		{`new label caption="x" column_span=0`, "below 1"},
		{`new label caption="x" row_stretch=-1`, "below 0"},
		{`new label caption="x" grow=-1`, "below 0"},
		{`new label caption="x" shrink=-2`, "below 0"},
		{`new label caption="x" basis=-4`, "below 0"},
	} {
		script, err := protocol.Parse(c.src)
		if err != nil {
			t.Fatalf("parse %q: %v", c.src, err)
		}
		_, err = protocol.NewSession().Execute(script, protocol.NewRegistryFactory(nil))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s gave %v, want a refusal saying %q", c.src, err, c.want)
		}
	}
}

// Setting one part of a placement leaves the others alone, so a script can
// write them in any order.
func TestGridHintsDoNotResetEachOther(t *testing.T) {
	f, _ := buildUI(t, nil, `new label caption="x" column_span=2 row=3 column=1 row_span=2`)
	lbl := f.targets[0].(*Label)
	p, set := lbl.LayoutGridPlacement()
	want := core.GridPlacement{Row: 3, Column: 1, RowSpan: 2, ColumnSpan: 2}
	if !set || p != want {
		t.Errorf("placement is %+v (set=%v), want %+v", p, set, want)
	}
}
