package core

import "testing"

// pastingLeaf takes pasted text, the way a text field or terminal surface does.
type pastingLeaf struct {
	TrinketBase
	got    []PasteEvent
	accept bool
}

func newPastingLeaf() *pastingLeaf {
	l := &pastingLeaf{accept: true}
	l.TrinketBase = *NewTrinketBase()
	l.Init(l)
	l.SetFocusPolicy(StrongFocus)
	return l
}

func (l *pastingLeaf) HandlePaste(ev PasteEvent) bool {
	l.got = append(l.got, ev)
	return l.accept
}

// A paste goes to whatever holds keyboard focus, and only there.
func TestFocusManagerRoutesPasteToTheFocusedTrinket(t *testing.T) {
	a, b := newPastingLeaf(), newPastingLeaf()
	fm, _ := focusScopeWith(a, b)
	if !fm.SetFocusedTrinket(b) {
		t.Fatal("could not focus the second leaf")
	}

	ev := PasteEvent{Text: "pasted body"}
	if !fm.HandlePaste(ev) {
		t.Fatal("the focused trinket did not take the paste")
	}
	if len(a.got) != 0 {
		t.Errorf("the unfocused trinket received %d pastes", len(a.got))
	}
	if len(b.got) != 1 || b.got[0] != ev {
		t.Errorf("focused trinket got %+v, want exactly [%+v]", b.got, ev)
	}
}

// A trinket that cannot hold a paste - a button, a list - declines rather than
// swallowing it, so the caller can fall back (the window-manager path).
func TestPasteToANonPastingTrinketIsDeclined(t *testing.T) {
	leaf := newPlainLeaf()
	leaf.SetFocusPolicy(StrongFocus)
	fm, _ := focusScopeWith(leaf)
	fm.SetFocusedTrinket(leaf)

	if fm.HandlePaste(PasteEvent{Text: "a"}) {
		t.Error("a trinket with no paste support reported it handled one")
	}
}

// Nothing focused is not a crash, and not a claim to have handled it.
func TestPasteWithNoFocusIsDeclined(t *testing.T) {
	fm, _ := focusScopeWith()
	if fm.HandlePaste(PasteEvent{Text: "a"}) {
		t.Error("an empty focus manager claimed a paste")
	}
}

// A field that declines - read-only, disabled - keeps its "no": the caller
// decides what to do with a paste nobody wants.
func TestADecliningFieldsPasteAnswerSurvivesRouting(t *testing.T) {
	leaf := newPastingLeaf()
	leaf.accept = false
	fm, _ := focusScopeWith(leaf)
	fm.SetFocusedTrinket(leaf)

	if fm.HandlePaste(PasteEvent{Text: "a"}) {
		t.Error("a declined paste was reported as handled")
	}
	if len(leaf.got) != 1 {
		t.Errorf("the field was asked %d times, want once", len(leaf.got))
	}
}
