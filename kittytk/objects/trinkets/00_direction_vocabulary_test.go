package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// A script names a direction on a container, and everything inside is stated
// against it -- the labels below never say a word about direction themselves.
func TestDirectionFromAScriptReachesTheChildren(t *testing.T) {
	f, _ := buildUI(t, nil, `
form=new panel layout=vbox direction=rtl children={
	new label caption="Name"
	inner=new panel layout=vbox direction=ltr children={
		new label caption="Nested"
	}
}
`)
	form := f.targets[0].(*Panel)
	name := f.targets[1].(*Label)
	island := f.targets[2].(*Panel)
	nested := f.targets[3].(*Label)

	if got := core.FindEffectiveDirection(form); got != core.DirRTL {
		t.Errorf("the form itself: direction = %v, want %v", got, core.DirRTL)
	}
	if got := core.FindEffectiveDirection(name); got != core.DirRTL {
		t.Errorf("a label in the form: direction = %v, want the form's %v", got, core.DirRTL)
	}
	if got := core.FindEffectiveDirection(island); got != core.DirLTR {
		t.Errorf("an LTR panel inside it: direction = %v, want %v", got, core.DirLTR)
	}
	if got := core.FindEffectiveDirection(nested); got != core.DirLTR {
		t.Errorf("a label inside that panel: direction = %v, want the panel's %v", got, core.DirLTR)
	}
}

// A label reads its own caption: Hebrew runs right to left, English left to
// right, and a caption of digits says nothing and takes the form's direction.
func TestLabelReportsTheDirectionOfItsCaption(t *testing.T) {
	f, _ := buildUI(t, nil, `
form=new panel layout=vbox direction=rtl children={
	new label caption="שלום"
	new label caption="Address"
	new label caption="1972"
}
`)
	hebrew := f.targets[1].(*Label)
	english := f.targets[2].(*Label)
	digits := f.targets[3].(*Label)

	if d, has := hebrew.TextDirection(); !has || d != core.DirRTL {
		t.Errorf("a Hebrew caption: reported (%v, %v), want (%v, true)", d, has, core.DirRTL)
	}
	if d, has := english.TextDirection(); !has || d != core.DirLTR {
		t.Errorf("an English caption: reported (%v, %v), want (%v, true)", d, has, core.DirLTR)
	}
	if d, has := digits.TextDirection(); has {
		t.Errorf("a caption of digits: reported (%v, %v), want no opinion", d, has)
	}

	// An English caption keeps its own direction inside a right-to-left form,
	// while the digits take the form's.
	if got := core.FindTextDirection(english); got != core.DirLTR {
		t.Errorf("the English label: text direction = %v, want %v", got, core.DirLTR)
	}
	if got := core.FindTextDirection(digits); got != core.DirRTL {
		t.Errorf("the digits label: text direction = %v, want the form's %v", got, core.DirRTL)
	}
}

// text_direction overrides the caption, for a name spelled in one script that
// belongs to text running the other way.
func TestTextDirectionOverridesTheCaption(t *testing.T) {
	f, _ := buildUI(t, nil, `
new label caption="שלום" text_direction=ltr
new label caption="Tel Aviv" text_direction=rtl
new label caption="שלום" text_direction=auto
`)
	forced := f.targets[0].(*Label)
	other := f.targets[1].(*Label)
	auto := f.targets[2].(*Label)

	if d, has := forced.TextDirection(); !has || d != core.DirLTR {
		t.Errorf("text_direction=ltr on a Hebrew caption: reported (%v, %v), want (%v, true)", d, has, core.DirLTR)
	}
	if d, has := other.TextDirection(); !has || d != core.DirRTL {
		t.Errorf("text_direction=rtl on an English caption: reported (%v, %v), want (%v, true)", d, has, core.DirRTL)
	}
	if d, has := auto.TextDirection(); !has || d != core.DirRTL {
		t.Errorf("text_direction=auto: reported (%v, %v), want the caption's (%v, true)", d, has, core.DirRTL)
	}
}

// Both properties reject a word they do not know, rather than quietly leaving
// the direction as it was.
func TestDirectionWordsAreChecked(t *testing.T) {
	for _, src := range []string{
		`new label caption="x" direction=sideways`,
		`new label caption="x" text_direction=sideways`,
	} {
		script, err := protocol.Parse(src)
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		_, err = protocol.NewSession().Execute(script, protocol.NewRegistryFactory(nil))
		if err == nil || !strings.Contains(err.Error(), "sideways") {
			t.Errorf("%s: expected an unknown-value error, got %v", src, err)
		}
	}
}
