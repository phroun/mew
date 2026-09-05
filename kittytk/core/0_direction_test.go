package core

import "testing"

// dirTrinket is a trinket that can hold a direction and a parent, which is all
// the walk needs.
type dirTrinket struct {
	TrinketBase
}

func newDirTrinket() *dirTrinket {
	t := &dirTrinket{}
	t.TrinketBase = *NewTrinketBase()
	t.Init(t)
	return t
}

// dirBox is a container that is a Trinket, so a chain can be built.
type dirBox struct {
	dirTrinket
	kids []Trinket
}

func newDirBox() *dirBox {
	b := &dirBox{}
	b.TrinketBase = *NewTrinketBase()
	b.Init(b)
	return b
}

func (b *dirBox) Children() []Trinket            { return b.kids }
func (b *dirBox) AddChild(t Trinket)             { b.kids = append(b.kids, t); t.SetParent(b) }
func (b *dirBox) RemoveChild(Trinket)            {}
func (b *dirBox) ChildAt(UnitPoint) Trinket      { return nil }
func (b *dirBox) Layout()                        {}
func (b *dirBox) LayoutManager() LayoutManager   { return nil }
func (b *dirBox) SetLayoutManager(LayoutManager) {}

// speaker is a trinket that answers for its own text.
type speaker struct {
	dirTrinket
	dir Direction
	has bool
}

func newSpeaker(d Direction, has bool) *speaker {
	s := &speaker{dir: d, has: has}
	s.TrinketBase = *NewTrinketBase()
	s.Init(s)
	return s
}

func (s *speaker) TextDirection() (Direction, bool) { return s.dir, s.has }

// Nothing in the chain names a direction, so everything is left-to-right --
// the answer for the great majority of trees, which name nothing at all.
func TestDirectionDefaultsToLeftToRight(t *testing.T) {
	outer := newDirBox()
	inner := newDirBox()
	leaf := newDirTrinket()
	outer.AddChild(inner)
	inner.AddChild(leaf)

	for name, w := range map[string]Trinket{"outer": outer, "inner": inner, "leaf": leaf} {
		if got := FindEffectiveDirection(w); got != DirLTR {
			t.Errorf("%s: direction = %v, want %v with nothing naming one", name, got, DirLTR)
		}
	}
	if got := FindEffectiveDirection(nil); got != DirLTR {
		t.Errorf("nil: direction = %v, want %v", got, DirLTR)
	}
}

// A direction named anywhere above reaches everything below it, however deep.
func TestDirectionReachesDownTheChain(t *testing.T) {
	outer := newDirBox()
	inner := newDirBox()
	leaf := newDirTrinket()
	outer.AddChild(inner)
	inner.AddChild(leaf)

	outer.SetDirection(DirRTL)
	if got := FindEffectiveDirection(leaf); got != DirRTL {
		t.Errorf("leaf under an RTL ancestor: direction = %v, want %v", got, DirRTL)
	}
	if got := FindEffectiveDirection(inner); got != DirRTL {
		t.Errorf("inner under an RTL ancestor: direction = %v, want %v", got, DirRTL)
	}
}

// The NEAREST ancestor that names one wins, so a left-to-right island inside
// a right-to-left tree stays left-to-right, and so does everything in it.
func TestDirectionTakenFromTheNearestAncestor(t *testing.T) {
	outer := newDirBox()
	island := newDirBox()
	leaf := newDirTrinket()
	outer.AddChild(island)
	island.AddChild(leaf)

	outer.SetDirection(DirRTL)
	island.SetDirection(DirLTR)

	if got := FindEffectiveDirection(leaf); got != DirLTR {
		t.Errorf("leaf inside an LTR island: direction = %v, want %v", got, DirLTR)
	}
	if got := FindEffectiveDirection(outer); got != DirRTL {
		t.Errorf("the island did not change what is outside it: direction = %v, want %v", got, DirRTL)
	}

	// And a trinket's own direction outranks every ancestor's.
	leaf.SetDirection(DirRTL)
	if got := FindEffectiveDirection(leaf); got != DirRTL {
		t.Errorf("a leaf naming its own direction: direction = %v, want %v", got, DirRTL)
	}
}

// Setting DirInherit hands the question back rather than pinning left-to-right.
func TestDirectionInheritIsNotAnAnswer(t *testing.T) {
	outer := newDirBox()
	leaf := newDirTrinket()
	outer.AddChild(leaf)

	outer.SetDirection(DirRTL)
	leaf.SetDirection(DirLTR)
	leaf.SetDirection(DirInherit)

	if got := FindEffectiveDirection(leaf); got != DirRTL {
		t.Errorf("a leaf set back to inherit: direction = %v, want its ancestor's %v", got, DirRTL)
	}
}

// A trinket's own text outranks the direction around it: an English caption in
// a right-to-left form is stated against English.
func TestTextDirectionPrefersWhatTheTrinketSays(t *testing.T) {
	form := newDirBox()
	form.SetDirection(DirRTL)

	says := newSpeaker(DirLTR, true)
	form.AddChild(says)
	if got := FindTextDirection(says); got != DirLTR {
		t.Errorf("a trinket reporting LTR text: direction = %v, want %v", got, DirLTR)
	}
}

// A trinket with no opinion -- a caption of digits, or one that declines --
// takes the direction around it, which is what makes textbegin land where
// layoutbegin does.
func TestTextDirectionFallsBackToTheSurroundings(t *testing.T) {
	form := newDirBox()
	form.SetDirection(DirRTL)

	for name, s := range map[string]*speaker{
		"declines":         newSpeaker(DirLTR, false),
		"no strong runes":  newSpeaker(DirInherit, false),
		"inherit reported": newSpeaker(DirInherit, true),
	} {
		form.AddChild(s)
		if got := FindTextDirection(s); got != DirRTL {
			t.Errorf("%s: direction = %v, want the surrounding %v", name, got, DirRTL)
		}
	}

	// A trinket that says nothing about text at all is the same case.
	plain := newDirTrinket()
	form.AddChild(plain)
	if got := FindTextDirection(plain); got != DirRTL {
		t.Errorf("a trinket carrying no text: direction = %v, want the surrounding %v", got, DirRTL)
	}
	if got := FindTextDirection(nil); got != DirLTR {
		t.Errorf("nil: direction = %v, want %v", got, DirLTR)
	}
}
