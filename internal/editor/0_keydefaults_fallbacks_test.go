package editor

import (
	"testing"

	"github.com/phroun/key-sequence-processor/keyseq"
)

// Three mechanisms decide what a key does, and they are easy to mistake for
// one another — the mistake being to read a shared outcome as a shared
// identity.
//
//  1. A FALLBACK GROUP makes its members fall back to each other. Pressing one
//     reaches another's binding only when the pressed token has none of its own.
//  2. Within a precedence level the order is exact token, then group member,
//     then wildcard (keyseq sequence.go). Exact-beats-fallback is a tie-break
//     INSIDE a level; across levels the higher level simply wins.
//  3. defaultCommandForKey is mew's own floor, consulted only when no binding
//     at any level claimed the key. It can give two unrelated keys the same
//     meaning without making either a spelling of the other.
//
// The tests below pin each, because the distinction is invisible in the
// common case: a group member resolving to a sibling's command looks exactly
// like the two being one key, and only binding BOTH tells them apart.

func procKSP(mappings map[string]string) *keyseq.Processor {
	p := keyseq.NewProcessor(nil)
	p.SetFallbackGroups(mewFallbackGroups)
	p.SetMappings(mappings)
	return p
}

func pressKSP(t *testing.T, p *keyseq.Processor, key string) string {
	t.Helper()
	p.ClearActiveSequence()
	return p.ProcessKey(key).Command
}

// CLAIM 1: a group member is a FALLBACK. With only "return" bound, pressing
// "enter" reaches it — nothing else is bound, so the group is consulted.
func TestFallbackFiresWhenTheSpellingIsUnbound(t *testing.T) {
	p := procKSP(map[string]string{"return": "accept"})
	for _, k := range []string{"return", "enter", "^M"} {
		if got := pressKSP(t, p, k); got != "accept" {
			t.Errorf("%q -> %q, want accept (fallback to the bound sibling)", k, got)
		}
	}
}

// CLAIM 2: it is NOT identity. Bind both, and each keeps its own command —
// the exact token wins and the fallback is never consulted.
func TestExactSpellingBeatsItsFallback(t *testing.T) {
	p := procKSP(map[string]string{"return": "accept", "enter": "keypad_accept"})
	for _, c := range []struct{ key, want string }{
		{"return", "accept"},
		{"enter", "keypad_accept"},
	} {
		if got := pressKSP(t, p, c.key); got != c.want {
			t.Errorf("%q -> %q, want %q — the two are NOT one key", c.key, got, c.want)
		}
	}
}

// CLAIM 3: the fallback is bidirectional. Bind only the non-primary member and
// the primary reaches it, so neither name is privileged at resolution time.
func TestFallbackRunsBothWays(t *testing.T) {
	p := procKSP(map[string]string{"enter": "keypad_accept"})
	if got := pressKSP(t, p, "return"); got != "keypad_accept" {
		t.Errorf("return -> %q, want keypad_accept", got)
	}
}

// CLAIM 4: "back" and "del" are NOT siblings — they sit in two different
// groups ({"back","^H","backspace"} and {"del","^8"}), so no fallback runs
// between them and binding one does nothing for the other. Their shared
// behavior comes from somewhere else entirely; see CLAIM 5.
func TestBackAndDelDoNotFallBackToEachOther(t *testing.T) {
	one := procKSP(map[string]string{"back": "erase"})
	if got := pressKSP(t, one, "del"); got != "" {
		t.Errorf("del with only back bound -> %q, want nothing: separate groups", got)
	}
	if got := pressKSP(t, one, "backspace"); got != "erase" {
		t.Errorf("backspace with back bound -> %q, want erase: same group", got)
	}
}

// Ctrl+Space is the one input whose spellings differ by WIRE rather than by
// preference, so all four have to land together.
//
// The legacy path sends NUL, which Ctrl+@ sends too, and direct-key-handler
// emits it as "^@" — it cannot know which key was struck. The kitty protocol
// reports the key instead, so the same press arrives as "C-space" (mew's name
// for KeySpace, under the C- prefix direct-key-handler emits). "^space" and
// "^2" are spellings a keymap writes and nothing ever emits.
//
// "^@" was missing from the group, which left a "^space" binding working under
// the kitty protocol and dead on the byte path — the kind of gap that reads as
// "Ctrl+Space does nothing in some terminals".
func TestCtrlSpaceResolvesFromEveryWireForm(t *testing.T) {
	// The guarantee that matters: a keymap writes "^space", and BOTH wires
	// reach it — "^@" from the byte path, "C-space" from the kitty protocol.
	p := procKSP(map[string]string{"^space": "ctrl_space"})
	for _, pressed := range []string{"^space", "^2", "^@", "C-space"} {
		if got := pressKSP(t, p, pressed); got != "ctrl_space" {
			t.Errorf("bound ^space, pressed %q -> %q, want ctrl_space", pressed, got)
		}
	}

	// The three group members mesh in every direction, so a keymap may write
	// whichever of them it likes.
	for _, bound := range []string{"^space", "^2", "^@"} {
		q := procKSP(map[string]string{bound: "cs"})
		for _, pressed := range []string{"^space", "^2", "^@"} {
			if got := pressKSP(t, q, pressed); got != "cs" {
				t.Errorf("bound %q, pressed %q -> %q, want cs", bound, pressed, got)
			}
		}
	}

	// The group has not swallowed the plain spacebar: a modifier is part of a
	// key's identity, not decoration.
	r := procKSP(map[string]string{"space": "bare", "^space": "ctrl"})
	if got := pressKSP(t, r, "space"); got != "bare" {
		t.Errorf("space -> %q, want bare", got)
	}
	if got := pressKSP(t, r, "^@"); got != "ctrl" {
		t.Errorf("^@ -> %q, want ctrl", got)
	}
}

// The two mechanisms do NOT compose, and this pins that rather than hiding it.
//
// "C-space" reaches "^space" because they are the same base under interchangeable
// Ctrl prefixes. "^2" and "^@" reach "^space" because the three are one fallback
// group. But "C-space" and "^2" reach each other by neither route: as tokens they
// are prefix+"space" and prefix+"2", two different bases, and group membership is
// per whole token.
//
// So a keymap that writes "^2" or "^@" instead of "^space" is dead under the
// kitty protocol. "^space" is the spelling that works from both wires, which is
// reason enough to write it; recorded here so that if the pairing is ever made
// to compose, this test fails and says so deliberately.
func TestCtrlPrefixSpellingDoesNotComposeWithGroupMembership(t *testing.T) {
	p := procKSP(map[string]string{"^2": "cs"})
	if got := pressKSP(t, p, "C-space"); got != "" {
		t.Errorf("bound ^2, pressed C-space -> %q; the two mechanisms now "+
			"compose, so the comment above is stale", got)
	}
	q := procKSP(map[string]string{"C-space": "cs"})
	if got := pressKSP(t, q, "^@"); got != "" {
		t.Errorf("bound C-space, pressed ^@ -> %q; likewise", got)
	}
}

// A modifier prefix peels, and the base's spellings resolve underneath it —
// so "^space" is the "^" modifier on the key named "space", not one opaque
// token. The same holds for every named key and for either Ctrl spelling,
// which is what lets a keymap write "^pgup" while the wire delivers "C-pgup".
func TestModifiersPeelAndBaseSpellingsResolveUnderThem(t *testing.T) {
	for _, c := range []struct{ bound, pressed string }{
		{"^minus", "^-"},      // word spelling bound, character pressed
		{"^-", "^minus"},      // and the reverse
		{"S-esc", "S-escape"}, // long spelling under a Shift prefix
		{"C-pgup", "^pgup"},   // the two Ctrl prefixes reach each other
		{"^pgup", "C-pgup"},
	} {
		p := procKSP(map[string]string{c.bound: "hit"})
		if got := pressKSP(t, p, c.pressed); got != "hit" {
			t.Errorf("bound %q, pressed %q -> %q, want hit", c.bound, c.pressed, got)
		}
	}
}

// CLAIM 5: what "back" and "del" actually share is the DEFAULT COMMAND floor,
// which is not the fallback mechanism at all. It answers only for a key no
// binding claimed, so it can give two unrelated keys one meaning without
// making either a spelling of the other.
func TestBackAndDelShareTheDefaultFloorNotAGroup(t *testing.T) {
	e := newDefaultsEditor(false)
	for _, k := range []string{"back", "del"} {
		if got := e.defaultCommandForKey(k); got == "" {
			t.Errorf("%q has no default command", k)
		}
	}
	if e.defaultCommandForKey("back") != e.defaultCommandForKey("del") {
		t.Error("back and del answer the floor differently")
	}
}

// CLAIM 5: precedence is per LEVEL, and a fallback at a higher level outranks
// an exact match at a lower one. Exact-beats-fallback is a tie-break WITHIN a
// level, not a global rule — which is the part most likely to surprise.
func TestHigherLevelFallbackOutranksLowerLevelExact(t *testing.T) {
	p := procKSP(map[string]string{
		"enter":            "low_exact",
		"(capture) return": "high_fallback",
	})
	if got := pressKSP(t, p, "enter"); got != "high_fallback" {
		t.Errorf("enter -> %q, want high_fallback: the capture level is consulted "+
			"first and its sibling matched there, so the exact match one level "+
			"down never got the chance", got)
	}
}
