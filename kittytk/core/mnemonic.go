package core

import "strings"

// MnemonicChoice is one letter a control would answer to: the letter, lower
// case, and where it stands in the caption the control shows.
type MnemonicChoice struct {
	Char rune
	Pos  int
}

// A Mnemonic is a control that answers to a single bare letter -- a letter
// pressed on its own, not an accelerator chord -- while something above it
// holds the focus.
//
// The rule is the parent chain: when a trinket holding the focus has the
// control somewhere below it, the control's letter is drawn out and a press
// of it presses the control. A message box's area, a panel or a scroll area
// that takes the focus offers every control inside it this way. The control
// itself holding the focus does not count, and neither does the window: a
// window is always the ancestor of whatever has its focus, and a letter typed
// into a field must stay the field's.
//
// Every control under the focused trinket competes for the letters. In the
// order the tree gives them, each takes the first letter its caption offers
// that no earlier one has taken -- the rule a menu's items follow.
type Mnemonic interface {
	Trinket
	// MnemonicChoices are the letters the caption offers, in preference order.
	MnemonicChoices() []MnemonicChoice
	// MnemonicPress presses the control, as its letter asks.
	MnemonicPress()
	// MnemonicPressing reports a press under way and not yet complete. While
	// any control under the focused trinket is pressing, every other one's
	// letter is withdrawn, so a second letter cannot start a second press
	// before the first has answered.
	MnemonicPressing() bool
}

// MnemonicScope is the trinket whose holding the focus offers t's letter: the
// nearest ancestor that has the focus, stopping at the window. nil when no
// ancestor below the window has it.
func MnemonicScope(t Trinket) Trinket {
	for p := Trinket(t.Parent()); p != nil; p = p.Parent() {
		if _, window := p.(FocusManagerOwner); window {
			return nil
		}
		if p.HasFocus() {
			return p
		}
	}
	return nil
}

// mnemonicsUnder is every shown Mnemonic below scope, in tree order. A hidden
// trinket's subtree is skipped, and so is a nested window's: its controls
// answer to its own focus.
func mnemonicsUnder(scope Trinket) []Mnemonic {
	var out []Mnemonic
	var walk func(t Trinket)
	walk = func(t Trinket) {
		c, ok := t.(interface{ Children() []Trinket })
		if !ok {
			return
		}
		for _, child := range c.Children() {
			if child == nil || !child.IsVisible() {
				continue
			}
			if _, window := child.(FocusManagerOwner); window {
				continue
			}
			if m, ok := child.(Mnemonic); ok {
				out = append(out, m)
			}
			walk(child)
		}
	}
	walk(scope)
	return out
}

// assignMnemonics settles the letters among controls: each takes the first of
// its choices no earlier control has taken.
func assignMnemonics(controls []Mnemonic) map[Mnemonic]MnemonicChoice {
	won := map[Mnemonic]MnemonicChoice{}
	taken := map[rune]bool{}
	for _, m := range controls {
		for _, c := range m.MnemonicChoices() {
			if !taken[c.Char] {
				taken[c.Char] = true
				won[m] = c
				break
			}
		}
	}
	return won
}

// pressing is the control under way among controls, or nil.
func pressing(controls []Mnemonic) Mnemonic {
	for _, m := range controls {
		if m.MnemonicPressing() {
			return m
		}
	}
	return nil
}

// LiveMnemonic is where m's letter stands in its caption while it is offered,
// and -1 while it is not: no ancestor has the focus, m won no letter, or
// another control's press is under way.
func LiveMnemonic(m Mnemonic) int {
	scope := MnemonicScope(m)
	if scope == nil {
		return -1
	}
	controls := mnemonicsUnder(scope)
	if p := pressing(controls); p != nil && p != m {
		return -1
	}
	if c, ok := assignMnemonics(controls)[m]; ok {
		return c.Pos
	}
	return -1
}

// AnswerMnemonic presses the control under focused that won the letter the
// event types, and reports whether the key was spent. A letter that names a
// control while another's press is under way is spent and does nothing.
func AnswerMnemonic(focused Trinket, event KeyPressEvent) bool {
	if focused == nil {
		return false
	}
	if len([]rune(event.Key)) != 1 {
		return false
	}
	want := []rune(strings.ToLower(event.Key))[0]
	controls := mnemonicsUnder(focused)
	for m, c := range assignMnemonics(controls) {
		if c.Char != want {
			continue
		}
		if pressing(controls) == nil {
			m.MnemonicPress()
		}
		return true
	}
	return false
}
