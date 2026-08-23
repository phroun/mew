package input

import (
	"testing"

	"github.com/phroun/direct-key-handler/keyboard"
)

// Every key the handler can name must have a mew name. A key left out arrives
// under the handler's own spelling — "PageUp" rather than "pgup" — which no
// binding matches and nothing reports; before the vocabulary was declared this
// way, the keypad's Enter reached mew as the literal "Return" for exactly that
// reason. This test is the thing that notices.
func TestMewNamesCoverEveryKey(t *testing.T) {
	for _, k := range keyboard.AllKeys() {
		if _, ok := mewKeyNames[k]; !ok {
			t.Errorf("no mew name for %v (would arrive as %q)", k, k.DefaultName())
		}
	}
}

// mew's names are binding-syntax tokens: a sequence like "^B space" is split
// on spaces, so a name containing one could never be matched.
func TestMewNamesAreBindingTokens(t *testing.T) {
	for k, name := range mewKeyNames {
		if name == "" {
			t.Errorf("%v has an empty name", k)
		}
		for _, r := range name {
			if r == ' ' || r == '\t' {
				t.Errorf("%v is named %q, which cannot be a token in a key sequence", k, name)
				break
			}
		}
	}
}

// A host parses its own key events and feeds them in under
// direct-key-handler's naming, so the feed still translates — unlike the
// terminal path, where the handler is given mew's names and emits them
// directly. Both spellings of the two Enter keys have to land on "return".
func TestNormalizeHostKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Escape", "esc"},
		{"PageUp", "pgup"},
		// The erase trio, which the two vocabularies spell in a way that reads
		// as a trap: upstream's "Delete" is the DEL character and becomes
		// mew's "del", while forward delete is "FDel" upstream and "fdel"
		// here. Getting these backwards sends the cursor the wrong way.
		{"FDel", "fdel"},
		{"Delete", "del"},
		{"Backspace", "back"},
		{"Return", "return"}, // the home-row key
		{"Enter", "return"},  // the keypad's, folded onto the same binding
		{" ", "space"},       // a host may send the literal character
		{"Space", "space"},   // ...or the name
		{"S-PageUp", "S-pgup"},
		{"M-Left", "M-left"},
		{"C-S-Home", "C-S-home"},
		// Every modifier in the vocabulary peels, including the two readings
		// of the meta lineage: M- is Mega and m- is Micro, two different keys
		// with two different prefixes.
		{"m-PageUp", "m-pgup"},
		{"M-m-Home", "M-m-home"},
		{"H-Delete", "H-del"}, // prefixes peel off either erase name...
		{"H-FDel", "H-fdel"},  // ...and off forward delete, which is a third key
		{"F1", "F1"},          // unchanged: mew spells it the same way
		{"a", "a"},            // printable, no entry
		{"^K", "^K"},          // control chord, no entry
		{"G-€", "G-€"},        // Glyph chord carries its own character
		// The keypad prefixes peel like the rest. A host that parses its own
		// events sends upstream's spelling, so without these in the list the
		// base never reaches the table and a keymap written against "P-home"
		// would be handed "P-Home" and never fire.
		{"P-Home", "P-home"},
		{"p-Home", "p-home"},
		{"C-P-PageUp", "C-P-pgup"},
		{"P-Begin", "P-begin"},    // the pad's own base name
		{"P-Enter", "P-return"},   // folded onto return, and still prefixed
		{"S-P-Delete", "S-P-del"}, // the pad's erase, under mew's name for DEL
		{"P-7", "P-7"},            // a shown pad key has no name to map
	}
	for _, c := range cases {
		if got := normalizeHostKey(c.in); got != c.want {
			t.Errorf("normalizeHostKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The keys with no American character reach a keymap under mew's spelling.
//
// They cannot be bound as characters: their characters are taken. Zag prints
// "<" and ">", which a US board puts on Shift+comma and Shift+period; Zig, Ro
// and Yen print "\" and "|", which belong to the backslash key. So a keymap can
// only name them, and the coverage test alone would accept ANY name — including
// upstream's, which is the failure it exists to catch elsewhere. These are the
// spellings a keymap is actually written against, so they are pinned.
func TestTheKeysWithNoCharacterHaveMewNames(t *testing.T) {
	for _, tc := range []struct {
		key  keyboard.Key
		want string
	}{
		{keyboard.KeyZig, "zig"},
		{keyboard.KeyZag, "zag"},
		{keyboard.KeyRo, "ro"},
		{keyboard.KeyYen, "yen"},
		{keyboard.KeyKanaLock, "kanalock"},
		{keyboard.KeyHangulLock, "hangullock"},
		{keyboard.KeyHenkan, "henkan"},
		{keyboard.KeyMuhenkan, "muhenkan"},
		{keyboard.KeyHanja, "hanja"},
		{keyboard.KeyBegin, "begin"},
		{keyboard.KeyPower, "power"},
	} {
		if got := mewKeyNames[tc.key]; got != tc.want {
			t.Errorf("%v named %q, want %q", tc.key, got, tc.want)
		}
	}
}

// mew's names are its own, and no two keys may share one.
//
// The table folds Return and the keypad's Enter deliberately, which is the one
// pair allowed to collide. Any OTHER collision means two physically distinct
// keys arrive indistinguishable, and a binding for one silently answers the
// other — the exact failure the whole vocabulary exists to prevent.
func TestOnlyTheEnterKeysShareAName(t *testing.T) {
	seen := make(map[string]keyboard.Key, len(mewKeyNames))
	for _, k := range keyboard.AllKeys() {
		name, ok := mewKeyNames[k]
		if !ok {
			continue
		}
		prev, dup := seen[name]
		if dup && !(name == "return") {
			t.Errorf("%v and %v are both named %q", prev, k, name)
		}
		seen[name] = k
	}
}

// The home-row key and the keypad's are deliberately folded onto one binding
// name — stated here rather than left to chance.
func TestReturnAndKeypadEnterFold(t *testing.T) {
	if got := mewKeyNames[keyboard.KeyReturn]; got != "return" {
		t.Errorf("KeyReturn named %q, want %q", got, "return")
	}
	if got := mewKeyNames[keyboard.KeyKeypadEnter]; got != "return" {
		t.Errorf("KeyKeypadEnter named %q, want %q", got, "return")
	}
}
