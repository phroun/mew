package core

// Which keymap is in force is a property of WHERE you are, not of the process.
//
// Almost everything inherits: there is one registry, "default", and a keystroke
// means what that table says. But a trinket can take the keyboard on its own
// terms — an embedded terminal, or a hosted editor with a keymap of its own —
// and it does that by declaring its own registry. While it holds the focus, its
// registry is the one that answers, and the toolkit's own bindings are simply
// not there to be resolved: they fall through to the guest, which is the whole
// point.
//
// The override cascades down the trinket tree it is declared on, so a window
// can hand its whole subtree a registry and a single trinket inside it can
// still say otherwise. Nothing has to be registered anywhere — the tree that
// already exists IS the cascade.

// A KeyRegistryProvider carries its own keymap, or nil to inherit the one its
// ancestors are using. Every TrinketBase implements it, so any trinket can be
// given a registry with SetKeyRegistry.
type KeyRegistryProvider interface {
	KeyRegistry() *KeyRegistry
}

// FindKeyRegistry returns the registry in force for a trinket: the nearest one
// declared by it or an ancestor, and the process-wide default when nothing in
// the chain declares anything. Never nil.
func FindKeyRegistry(t Trinket) *KeyRegistry {
	for current := t; current != nil; {
		if p, ok := current.(KeyRegistryProvider); ok {
			if r := p.KeyRegistry(); r != nil {
				return r
			}
		}
		parent := current.Parent()
		if parent == nil {
			break
		}
		current = parent
	}
	return DefaultKeyRegistry()
}

// SetKeyRegistry gives this trinket (and everything under it that does not say
// otherwise) its own keymap. Nil returns it to inheriting.
//
// A trinket that wants the keyboard for itself declares a registry with the
// bindings it is willing to share and nothing else: what it does not bind is
// not bound at all while it has the focus, so the keystroke reaches it instead
// of being spent on a toolkit command.
func (w *TrinketBase) SetKeyRegistry(r *KeyRegistry) {
	w.mu.Lock()
	w.keyRegistry = r
	w.mu.Unlock()
}

// KeyRegistry returns the registry this trinket declared, or nil when it
// inherits. Use FindKeyRegistry to resolve what is actually in force.
func (w *TrinketBase) KeyRegistry() *KeyRegistry {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.keyRegistry
}

// A FocusedTrinketProvider names what currently holds the focus INSIDE it. A
// desktop is one (the focus is inside whichever window is active); a window
// answers through its FocusManager instead, which the walk below understands
// too.
type FocusedTrinketProvider interface {
	FocusedTrinket() Trinket
}

// A KeyRegistryFocuser resolves keys against the registry in force where the
// FOCUS is, rather than where it itself sits. A window and the desktop are
// both this: their own accelerators must stand down when the focus is inside
// something that took the keyboard, or the guest never sees the key.
type KeyRegistryFocuser interface {
	FocusedKeyRegistry() *KeyRegistry
}

// FindFocusedKeyRegistry returns the registry in force for whatever holds the
// focus inside t -- down to the focused leaf, then up from there to the
// nearest declaration. With nothing focused it answers for t itself.
//
// This is the question a window or the desktop asks. "Which keymap is in
// force" is a property of where the FOCUS is, not of who is asking: a window
// whose editor took the keyboard must not keep resolving ^Q against the
// toolkit's table just because the window itself declared nothing.
func FindFocusedKeyRegistry(t Trinket) *KeyRegistry {
	if leaf := focusedLeaf(t); leaf != nil {
		return FindKeyRegistry(leaf)
	}
	return FindKeyRegistry(t)
}

// focusedLeaf walks down from t to whatever actually holds the focus, through
// however many nested focus scopes lie between (a desktop's active window, its
// focused trinket, a pane inside that). Depth-bounded, because a tree that
// somehow points at itself must not hang the keyboard.
func focusedLeaf(t Trinket) Trinket {
	current := t
	for depth := 0; current != nil && depth < 32; depth++ {
		var next Trinket
		switch owner := current.(type) {
		case FocusedTrinketProvider:
			next = owner.FocusedTrinket()
		case FocusManagerOwner:
			if fm := owner.FocusManager(); fm != nil {
				next = fm.FocusedTrinket()
			}
		}
		if next == nil || next == current {
			return current
		}
		current = next
	}
	return current
}
