package editor

import (
	"testing"

	"github.com/phroun/key-sequence-processor/keyseq"
)

// Three mechanisms decide what a key does, and they are easy to mistake for
// one another — the mistake being to read a shared outcome as a shared
// identity.
//
//  1. An ALIAS GROUP makes its members FALLBACKS for each other. Pressing one
//     reaches another's binding only when the pressed spelling has none.
//  2. Within a precedence level the order is exact spelling, then alias
//     sibling, then wildcard (keyseq sequence.go). Exact-beats-fallback is a
//     tie-break INSIDE a level; across levels the higher level simply wins.
//  3. defaultCommandForKey is mew's own floor, consulted only when no binding
//     at any level claimed the key. It can give two unrelated keys the same
//     meaning without making either a spelling of the other.
//
// The tests below pin each, because the distinction is invisible in the
// common case: a group member resolving to a sibling's command looks exactly
// like the two being one key, and only binding BOTH tells them apart.

func procKSP(mappings map[string]string) *keyseq.Processor {
	p := keyseq.NewProcessor(nil)
	p.SetAliasGroups(mewAliasGroups)
	p.SetMappings(mappings)
	return p
}

func pressKSP(t *testing.T, p *keyseq.Processor, key string) string {
	t.Helper()
	p.ClearActiveSequence()
	return p.ProcessKey(key).Command
}

// CLAIM 1: an alias member is a FALLBACK. With only "return" bound, pressing
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
// the exact spelling wins and the fallback is never consulted.
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

// CLAIM 5: what "back" and "del" actually share is the DEFAULT COMMAND floor,
// which is not the alias mechanism at all. It answers only for a key no
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
