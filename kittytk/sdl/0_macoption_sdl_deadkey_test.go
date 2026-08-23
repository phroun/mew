//go:build sdl

package sdl

import "testing"

// The five macOS dead-key Option chords. They arm an accent for the next
// keystroke instead of producing a character, so they arrive as an in-flight
// composition rather than as text -- which is why decodeMacOSOptionChar, whose
// input is composed TEXT, never saw them and M-e opened an accent picker over
// whatever had focus.
func TestDeadKeyCompositionsDecodeToTheirChord(t *testing.T) {
	for composition, want := range map[string]string{
		"´": "M-e",
		"ˆ": "M-i",
		"˜": "M-n",
		"¨": "M-u",
		"`": "M-`",
	} {
		got, ok := decodeMacOSDeadKey(composition)
		if !ok || got != want {
			t.Errorf("%q -> %q,%v want %q,true", composition, got, ok, want)
		}
	}
}

// An ordinary input-method composition is left alone: a CJK candidate being
// typed is text in flight, not a shortcut, and must reach the focused trinket
// untranslated.
func TestOrdinaryCompositionsAreNotDecoded(t *testing.T) {
	for _, composition := range []string{"", "に", "にほん", "a", "ni", "´´"} {
		if got, ok := decodeMacOSDeadKey(composition); ok {
			t.Errorf("%q was decoded as %q; it is a composition, not a chord", composition, got)
		}
	}
}

// Every dead key names a chord the Option table also lists, so the two agree
// on what the physical keystroke was.
func TestDeadKeysAgreeWithTheOptionTable(t *testing.T) {
	for composition, chord := range macOSDeadKeys {
		r := []rune(composition)
		if len(r) != 1 {
			t.Errorf("%q is not a single accent character", composition)
			continue
		}
		if other, ok := macOSOptionChars[r[0]]; ok && other != chord {
			t.Errorf("%q: dead key says %q, the Option table says %q", composition, chord, other)
		}
	}
}
