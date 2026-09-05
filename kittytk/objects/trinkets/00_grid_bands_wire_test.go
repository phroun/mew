package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/protocol"
)

// buildBandPanel runs src and returns the first panel it built.
func buildBandPanel(t *testing.T, src string) *Panel {
	t.Helper()
	f := &captureFactory{inner: protocol.NewRegistryFactory(&protocol.BindContext{})}
	s := protocol.NewSession()
	script, err := protocol.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := s.Execute(script, f); err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, target := range f.targets {
		if p, ok := target.(*Panel); ok {
			return p
		}
	}
	t.Fatalf("%q built no panel", src)
	return nil
}

// buildBandErr runs src and returns the error it must produce.
func buildBandErr(t *testing.T, src string) string {
	t.Helper()
	f := &captureFactory{inner: protocol.NewRegistryFactory(&protocol.BindContext{})}
	s := protocol.NewSession()
	script, err := protocol.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := s.Execute(script, f); err != nil {
		return err.Error()
	}
	t.Fatalf("executing %q: expected an error", src)
	return ""
}

// A grid's columns are written as bands in a collection of their own, and the
// axis they belong to is the collection they were written in.
func TestAGridsBandsAreWrittenAsColumnsAndRows(t *testing.T) {
	p := buildBandPanel(t, `
new panel layout=grid columns={
	new band id=labels
	new band id=fields stretch=1 min_size=80
} rows={
	new band id=header min_size=16
}`)

	g, ok := p.LayoutManager().(*layout.GridLayout)
	if !ok {
		t.Fatalf("panel's layout is %T, want a grid", p.LayoutManager())
	}

	cols := g.Columns()
	if len(cols) != 2 {
		t.Fatalf("the grid has %d columns, want 2", len(cols))
	}
	if cols[0].ID != "labels" || cols[0].Stretch != 0 || cols[0].Minimum != 0 {
		t.Errorf("column 0 = %+v, want the labels band asking for nothing", cols[0])
	}
	if cols[1].ID != "fields" || cols[1].Stretch != 1 || cols[1].Minimum != core.Unit(80) {
		t.Errorf("column 1 = %+v, want fields stretch=1 min_size=80", cols[1])
	}

	rows := g.Rows()
	if len(rows) != 1 || rows[0].ID != "header" || rows[0].Minimum != core.Unit(16) {
		t.Errorf("rows = %+v, want one header band with a minimum of 16", rows)
	}
}

// A child names its band on the wire, and the name reaches the placement the
// grid settles at layout time.
func TestAChildNamesItsBandOnTheWire(t *testing.T) {
	p := buildBandPanel(t, `
new panel layout=grid columns={
	new band id=labels
	new band id=fields stretch=1
} children={
	new label caption="Name:" row=0 column=labels
	new textinput row=0 column=fields
	new label caption="by index" row=1 column=1
}`)

	g := p.LayoutManager().(*layout.GridLayout)
	if g.Count() != 3 {
		t.Fatalf("the grid holds %d children, want 3", g.Count())
	}
	if got := g.ItemAt(0).ColumnID; got != "labels" {
		t.Errorf("the first child named column %q, want labels", got)
	}
	if got := g.ItemAt(1).ColumnID; got != "fields" {
		t.Errorf("the second child named column %q, want fields", got)
	}
	if item := g.ItemAt(2); item.ColumnID != "" || item.Column != 1 {
		t.Errorf("column=1 became id=%q index=%d, want an index of 1 and no name",
			item.ColumnID, item.Column)
	}

	// Settling the names is what puts a named child where the band is.
	g.ColumnCount()
	if got := g.ItemAt(1).Column; got != 1 {
		t.Errorf(`column="fields" settled at index %d, want the second band's 1`, got)
	}
}

// Bands need a grid to be columns of, and a collection takes only bands.
func TestBandsAreRefusedWhereTheyMeanNothing(t *testing.T) {
	err := buildBandErr(t, `new panel layout=vbox columns={ new band id=x }`)
	if !strings.Contains(err, "columns") || !strings.Contains(err, "layout=grid") {
		t.Errorf("a vbox given columns: %q, want it to name the layout it needs", err)
	}

	// layout= comes first, as it does for everything else that configures a
	// layout manager.
	err = buildBandErr(t, `new panel columns={ new band id=x } layout=grid`)
	if !strings.Contains(err, "layout=grid") {
		t.Errorf("columns before layout=grid: %q", err)
	}

	err = buildBandErr(t, `new panel layout=grid columns={ new button caption="no" }`)
	if !strings.Contains(err, "columns") || !strings.Contains(err, "band") {
		t.Errorf("a button in columns=: %q, want the members it names", err)
	}
}

// row= and column= take an index or a band id, and refuse anything else
// rather than quietly reading it as zero.
func TestATrackIsAnIndexOrABandId(t *testing.T) {
	err := buildBandErr(t, `new panel layout=grid children={ new label caption="x" column="quoted" }`)
	if !strings.Contains(err, "column") || !strings.Contains(err, "band id") {
		t.Errorf("a quoted string as a column: %q", err)
	}

	err = buildBandErr(t, `new panel layout=grid children={ new label caption="x" row=-1 }`)
	if !strings.Contains(err, "row") || !strings.Contains(err, "below 0") {
		t.Errorf("a negative row: %q", err)
	}
}
