package core

import "testing"

// composingLeaf holds compositions, the way a text field does.
type composingLeaf struct {
	TrinketBase
	got    []TextEditingEvent
	accept bool
}

func newComposingLeaf() *composingLeaf {
	l := &composingLeaf{accept: true}
	l.TrinketBase = *NewTrinketBase()
	l.Init(l)
	l.SetFocusPolicy(StrongFocus)
	return l
}

func (l *composingLeaf) HandleTextEditing(ev TextEditingEvent) bool {
	l.got = append(l.got, ev)
	return l.accept
}

// focusScopeWith builds a manager over a root holding the given leaves.
func focusScopeWith(children ...Trinket) (*FocusManager, *trackerBox) {
	root := newTrackerBox()
	for _, c := range children {
		root.AddChild(c)
	}
	return NewFocusManager(root), root
}

// A composition goes to whatever holds keyboard focus, with no
// interpretation on the way.
func TestFocusManagerRoutesCompositionToTheFocusedTrinket(t *testing.T) {
	a, b := newComposingLeaf(), newComposingLeaf()
	fm, _ := focusScopeWith(a, b)
	if !fm.SetFocusedTrinket(b) {
		t.Fatal("could not focus the second leaf")
	}

	ev := TextEditingEvent{Text: "きょ", Start: -1, Length: -1}
	if !fm.HandleTextEditing(ev) {
		t.Fatal("the focused trinket did not take the composition")
	}
	if len(a.got) != 0 {
		t.Errorf("the unfocused trinket received %d compositions", len(a.got))
	}
	if len(b.got) != 1 || b.got[0] != ev {
		t.Errorf("focused trinket got %+v, want exactly [%+v]", b.got, ev)
	}
}

// A trinket that cannot hold a composition - a button, a list - does not
// make one disappear quietly. Returning false lets the caller try
// somewhere else, exactly as an unhandled key does.
func TestCompositionToANonComposingTrinketIsDeclined(t *testing.T) {
	leaf := newPlainLeaf()
	leaf.SetFocusPolicy(StrongFocus)
	fm, _ := focusScopeWith(leaf)
	fm.SetFocusedTrinket(leaf)

	if fm.HandleTextEditing(TextEditingEvent{Text: "a"}) {
		t.Error("a trinket with no composition support reported it handled one")
	}
}

// Nothing focused is not a crash, and not a claim to have handled it.
func TestCompositionWithNoFocusIsDeclined(t *testing.T) {
	fm, _ := focusScopeWith()
	if fm.HandleTextEditing(TextEditingEvent{Text: "a"}) {
		t.Error("an empty focus manager claimed a composition")
	}
}

// A field that declines - read-only, disabled - must not have its "no"
// converted into a "yes" on the way back up. The caller decides what to
// do with a composition nobody wants.
func TestADecliningFieldsAnswerSurvivesRouting(t *testing.T) {
	leaf := newComposingLeaf()
	leaf.accept = false
	fm, _ := focusScopeWith(leaf)
	fm.SetFocusedTrinket(leaf)

	if fm.HandleTextEditing(TextEditingEvent{Text: "a"}) {
		t.Error("a declined composition was reported as handled")
	}
	if len(leaf.got) != 1 {
		t.Errorf("the field was asked %d times, want once", len(leaf.got))
	}
}

// Tab is a key, not a composition: HandleTextEditing must never fall
// back to focus navigation the way HandleKeyPress does. An input method
// composing the literal string "Tab" would otherwise move focus out of
// the field being typed into.
func TestCompositionNeverMovesFocus(t *testing.T) {
	a, b := newComposingLeaf(), newComposingLeaf()
	a.accept, b.accept = false, false
	fm, _ := focusScopeWith(a, b)
	fm.SetFocusedTrinket(a)

	fm.HandleTextEditing(TextEditingEvent{Text: "Tab"})
	if fm.FocusedTrinket() != Trinket(a) {
		t.Error("a declined composition moved the focus")
	}
}
