package core

import "testing"

// scopeTrinket is a trinket that can hold a registry and a parent, which is
// all a cascade needs.
type scopeTrinket struct {
	TrinketBase
	TrinketKeys
}

func newScopeTrinket(commands ...string) *scopeTrinket {
	t := &scopeTrinket{}
	t.TrinketBase = *NewTrinketBase()
	t.SetCommands(commands...)
	t.Init(t) // wires the keys to this trinket, so they resolve where it sits
	return t
}

// A container that is a Trinket, so a chain can be built.
type scopeBox struct {
	scopeTrinket
	kids []Trinket
}

func newScopeBox() *scopeBox {
	b := &scopeBox{}
	b.TrinketBase = *NewTrinketBase()
	b.Init(b)
	return b
}

func (b *scopeBox) Children() []Trinket            { return b.kids }
func (b *scopeBox) AddChild(t Trinket)             { b.kids = append(b.kids, t); t.SetParent(b) }
func (b *scopeBox) RemoveChild(Trinket)            {}
func (b *scopeBox) ChildAt(UnitPoint) Trinket      { return nil }
func (b *scopeBox) Layout()                        {}
func (b *scopeBox) LayoutManager() LayoutManager   { return nil }
func (b *scopeBox) SetLayoutManager(LayoutManager) {}

// Nothing declares anything, so everything inherits the process default.
func TestKeysInheritTheDefaultRegistry(t *testing.T) {
	box := newScopeBox()
	kid := newScopeTrinket(CmdTrinketActivate)
	box.AddChild(kid)

	if got := FindKeyRegistry(kid); got != DefaultKeyRegistry() {
		t.Errorf("registry = %q, want the default", got.Name())
	}
	if got := kid.KeyCommand("Return"); got != CmdTrinketActivate {
		t.Errorf("Return resolved to %q, want %s", got, CmdTrinketActivate)
	}
}

// A trinket that takes the keyboard on its own terms declares a registry. What
// that registry does not bind is not bound while it is in force: the keystroke
// is left for the guest rather than spent on a toolkit command.
func TestOwnRegistryTakesTheKeyboard(t *testing.T) {
	captured := NewKeyRegistry("captured", []Binding{
		{"Escape", []string{CmdTrinketCancel}}, // the one key it will share
	})

	box := newScopeBox()
	kid := newScopeTrinket(CmdTrinketActivate, CmdTrinketCancel)
	box.AddChild(kid)
	kid.SetKeyRegistry(captured)

	if got := FindKeyRegistry(kid); got != captured {
		t.Fatalf("registry = %q, want captured", got.Name())
	}
	if got := kid.KeyCommand("Return"); got != "" {
		t.Errorf("Return resolved to %q; the captured keymap does not bind it, so it belongs to the guest", got)
	}
	if got := kid.KeyCommand("Escape"); got != CmdTrinketCancel {
		t.Errorf("Escape resolved to %q, want %s - the captured keymap does bind it", got, CmdTrinketCancel)
	}
}

// The override cascades DOWN the tree it is declared on, and does not leak up.
func TestRegistryCascadesDownNotUp(t *testing.T) {
	own := NewKeyRegistry("captured", nil)

	outer := newScopeBox()
	inner := newScopeBox()
	leaf := newScopeTrinket(CmdTrinketActivate)
	outer.AddChild(inner)
	inner.AddChild(leaf)

	inner.SetKeyRegistry(own)

	if got := FindKeyRegistry(leaf); got != own {
		t.Errorf("the leaf resolves against %q, want the ancestor's captured", got.Name())
	}
	if got := FindKeyRegistry(outer); got == own {
		t.Error("an override leaked upward to the parent")
	}

	inner.SetKeyRegistry(nil) // back to inheriting
	if got := FindKeyRegistry(leaf); got != DefaultKeyRegistry() {
		t.Errorf("after clearing, the leaf resolves against %q, want the default", got.Name())
	}
}

// A context built under one keymap must not survive another coming into
// force. Nothing about a registry's REVISION changes when focus moves into a
// trinket that brought its own, so the identity has to be watched too.
func TestContextRebuildsWhenTheRegistryChanges(t *testing.T) {
	kid := newScopeTrinket(CmdTrinketActivate)

	if got := kid.KeyCommand("Return"); got != CmdTrinketActivate {
		t.Fatalf("Return resolved to %q under the default keymap", got)
	}

	kid.SetKeyRegistry(NewKeyRegistry("captured", nil))

	if got := kid.KeyCommand("Return"); got != "" {
		t.Errorf("Return still resolves to %q; the stale context outlived its keymap", got)
	}
}
