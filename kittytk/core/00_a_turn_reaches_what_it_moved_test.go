package core

import "testing"

// listener is a box that counts how many times it was told the direction it
// reads has moved, so a test can see where the walk went.
type listener struct {
	dirBox
	told int
}

func newListener() *listener {
	l := &listener{}
	l.TrinketBase = *NewTrinketBase()
	l.Init(l)
	return l
}

func (l *listener) DirectionChanged() { l.told++ }

// Everything reading a direction resolves it on demand, so almost nothing has
// to be told when one moves. What does is state DERIVED from the direction and
// kept -- and it is kept wherever it sits, so a turn has to reach down to it.
func TestATurnReachesTheSubtreeThatReadsIt(t *testing.T) {
	room := newDirBox()
	near := newListener()
	deep := newListener()
	room.AddChild(near)
	near.AddChild(deep)

	room.SetDirection(DirRTL)
	if near.told != 1 || deep.told != 1 {
		t.Errorf("turning the room told near %d times and deep %d, want 1 each",
			near.told, deep.told)
	}
	if got := FindEffectiveDirection(deep.Self()); got != DirRTL {
		t.Errorf("the deep trinket reads %v, want the room's %v", got, DirRTL)
	}
}

// A trinket that names its own direction reads the same thing before and
// after, so the walk stops there: it is not stale, and neither is anything
// under it.
func TestATurnStopsAtWhatNamesItsOwn(t *testing.T) {
	room := newDirBox()
	own := newListener()
	under := newListener()
	room.AddChild(own)
	own.AddChild(under)
	own.SetDirection(DirLTR) // names its own: told once, by this call

	told, underTold := own.told, under.told
	room.SetDirection(DirRTL)
	if own.told != told || under.told != underTold {
		t.Errorf("turning the room told a subtree that names its own direction: %d/%d, was %d/%d",
			own.told, under.told, told, underTold)
	}
	if got := FindEffectiveDirection(under.Self()); got != DirLTR {
		t.Errorf("under a trinket naming its own direction reads %v, want %v", got, DirLTR)
	}
}

// The trinket the direction was set ON is where the change happened, so it is
// always told -- it names its own direction by definition at that point, which
// is what the descendants' stop rule is about, not this one.
func TestTheTrinketTurnedIsAlwaysTold(t *testing.T) {
	l := newListener()
	l.SetDirection(DirRTL)
	if l.told != 1 {
		t.Errorf("a trinket told to turn heard about it %d times, want 1", l.told)
	}
	l.SetDirection(DirLTR)
	if l.told != 2 {
		t.Errorf("turning it back was heard %d times in total, want 2", l.told)
	}
}
