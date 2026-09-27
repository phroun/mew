package core

import "testing"

// backwardLeaf counts the times it is told the focus is coming from behind.
type backwardLeaf struct {
	TrinketBase
	told int
}

func newBackwardLeaf() *backwardLeaf {
	l := &backwardLeaf{}
	l.TrinketBase = *NewTrinketBase()
	l.Init(l)
	l.SetFocusPolicy(StrongFocus)
	return l
}

func (l *backwardLeaf) FocusArrivingBackward() { l.told++ }

// Walking the chain backwards tells the trinket it arrives at, before the focus
// gets there, so a trinket with a stop after its first can take the focus at
// that stop. Walking forwards, or focusing it outright, tells it nothing.
func TestAWalkBackwardsSaysSoToWhereItArrives(t *testing.T) {
	a, b := newBackwardLeaf(), newBackwardLeaf()
	fm, _ := focusScopeWith(a, b)

	fm.SetFocusedTrinket(a)
	if a.told != 0 {
		t.Errorf("focusing a trinket outright told it %d times", a.told)
	}
	fm.FocusNext()
	if b.told != 0 {
		t.Errorf("a walk forwards told the trinket it reached %d times", b.told)
	}
	fm.FocusPrior()
	if fm.FocusedTrinket() != a || a.told != 1 {
		t.Errorf("a walk backwards reached %v and told it %d times, want the first leaf told once",
			fm.FocusedTrinket(), a.told)
	}
	fm.FocusLast()
	if fm.FocusedTrinket() != b || b.told != 1 {
		t.Errorf("focusing the last trinket reached %v and told it %d times, want the second leaf told once",
			fm.FocusedTrinket(), b.told)
	}
}
