package core

import "testing"

// Showing or hiding a trinket arranges its parent again, since the siblings
// move; a layout root is placed by whatever manages it and has no siblings a
// layout moves, so showing one arranges nothing.

type arrangingParent struct {
	TrinketBase
	passes int
}

func (p *arrangingParent) Layout()                        { p.passes++ }
func (p *arrangingParent) Children() []Trinket            { return nil }
func (p *arrangingParent) AddChild(Trinket)               {}
func (p *arrangingParent) RemoveChild(Trinket)            {}
func (p *arrangingParent) ChildAt(UnitPoint) Trinket      { return nil }
func (p *arrangingParent) LayoutManager() LayoutManager   { return nil }
func (p *arrangingParent) SetLayoutManager(LayoutManager) {}

type rootChild struct{ TrinketBase }

func (r *rootChild) IsLayoutRoot() bool { return true }

type plainChild struct{ TrinketBase }

func TestShowingALayoutRootArrangesNothingAroundIt(t *testing.T) {
	parent := &arrangingParent{TrinketBase: *NewTrinketBase()}
	parent.Init(parent)

	root := &rootChild{TrinketBase: *NewTrinketBase()}
	root.Init(root)
	root.SetParent(parent)
	root.Hide()
	root.Show()
	if parent.passes != 0 {
		t.Errorf("showing and hiding a layout root arranged its parent %d times", parent.passes)
	}

	plain := &plainChild{TrinketBase: *NewTrinketBase()}
	plain.Init(plain)
	plain.SetParent(parent)
	plain.Hide()
	if parent.passes != 1 {
		t.Errorf("hiding an ordinary child arranged its parent %d times, want 1", parent.passes)
	}
}
