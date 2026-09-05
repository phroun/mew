package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// Each of the three properties changes its own part and leaves the rest of the
// alignment alone, so a script can set them in any order and get what it wrote
// rather than whichever came last.
func TestAlignmentPropertiesDoNotResetEachOther(t *testing.T) {
	for _, src := range []string{
		`new label caption="x" halign=opticalright valign=top fill=h`,
		`new label caption="x" fill=h valign=top halign=opticalright`,
		`new label caption="x" valign=top fill=h halign=opticalright`,
	} {
		f, _ := buildUI(t, nil, src)
		lbl := f.targets[0].(*Label)
		want := core.Alignment{H: core.AlignOpticalRight, V: core.AlignTop, FillH: true}
		if a, set := lbl.LayoutAlignment(); !set || a != want {
			t.Errorf("%s\n  gave %+v (set=%v), want %+v", src, a, set, want)
		}
	}
}

// An item nobody says anything about fills both axes and centres on either one
// with nothing to fill, and setting one property starts from that rather than
// from an empty alignment.
func TestAlignmentPropertiesStartFromTheDefault(t *testing.T) {
	f, _ := buildUI(t, nil, `new label caption="x" halign=opticalleft`)
	lbl := f.targets[0].(*Label)

	want := core.DefaultAlignment()
	want.H = core.AlignOpticalLeft
	if a, set := lbl.LayoutAlignment(); !set || a != want {
		t.Errorf("halign alone gave %+v (set=%v), want %+v", a, set, want)
	}
}

// fill names the axes an item grows on, each on its own.
func TestFillNamesEachAxis(t *testing.T) {
	for _, c := range []struct {
		word         string
		wantH, wantV bool
	}{
		{"none", false, false},
		{"h", true, false},
		{"v", false, true},
		{"both", true, true},
	} {
		f, _ := buildUI(t, nil, `new label caption="x" fill=`+c.word)
		lbl := f.targets[0].(*Label)
		a, _ := lbl.LayoutAlignment()
		if a.FillH != c.wantH || a.FillV != c.wantV {
			t.Errorf("fill=%s gave FillH=%v FillV=%v, want %v/%v", c.word, a.FillH, a.FillV, c.wantH, c.wantV)
		}
	}
}

// A word none of the three knows is refused rather than quietly ignored.
func TestAlignmentWordsAreChecked(t *testing.T) {
	for _, src := range []string{
		`new label caption="x" halign=sideways`,
		`new label caption="x" valign=sideways`,
		`new label caption="x" fill=sideways`,
		`new label caption="x" text_align=sideways`,
		// The old physical words are not in the vocabulary any more.
		`new label caption="x" halign=left`,
		`new label caption="x" halign=right`,
	} {
		script, err := protocol.Parse(src)
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		if _, err := protocol.NewSession().Execute(script, protocol.NewRegistryFactory(nil)); err == nil {
			t.Errorf("%s: expected an unknown-value error, got none", src)
		} else if !strings.Contains(err.Error(), "unknown value") {
			t.Errorf("%s: expected an unknown-value error, got %v", src, err)
		}
	}
}

// A label's caption sits where its own script begins: an English caption on
// the left, a Hebrew one on the right, and a caption of digits wherever the
// label's surroundings begin.
func TestLabelCaptionSitsWhereItsScriptBegins(t *testing.T) {
	f, _ := buildUI(t, nil, `
form=new panel layout=vbox direction=rtl children={
	new label caption="Address"
	new label caption="שלום"
	new label caption="1972"
}
`)
	for i, c := range []struct {
		what string
		want core.HSide
	}{
		{"an English caption", core.SideLeft},
		{"a Hebrew caption", core.SideRight},
		{"a caption of digits, in a right-to-left form", core.SideRight},
	} {
		lbl := f.targets[i+1].(*Label)
		if got := lbl.textSide(); got != c.want {
			t.Errorf("%s draws from %v, want %v", c.what, got, c.want)
		}
	}

	// And the optical escape hatch overrides the script.
	f, _ = buildUI(t, nil, `new label caption="שלום" text_align=opticalleft`)
	if got := f.targets[0].(*Label).textSide(); got != core.SideLeft {
		t.Errorf("text_align=opticalleft on a Hebrew caption draws from %v, want %v", got, core.SideLeft)
	}
}
