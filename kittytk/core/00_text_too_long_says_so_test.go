package core

// Cutting text to the room it has, in the three places a cut can go.

import (
	"strings"
	"testing"
)

// byRune measures a rune as one unit, so a test says what it means about
// widths without a font in the way.
func byRune(s string) Unit { return Unit(len([]rune(s))) }

func TestTextThatFitsIsLeftAlone(t *testing.T) {
	for _, mode := range []ElideMode{ElideEnd, ElideMiddle, ElideStart, ElideOff} {
		if got := Elide("short", 20, mode, byRune); got != "short" {
			t.Errorf("%v cut text that fits: %q", mode, got)
		}
	}
}

// Each mode keeps the part it says it keeps, and none of them comes out longer
// than the room it was given.
func TestEachModeKeepsWhatItSaysItKeeps(t *testing.T) {
	const text = "sha256:abcdefghijklmnop"
	for _, c := range []struct {
		mode ElideMode
		want string
	}{
		{ElideEnd, "sha256:a…"},
		{ElideMiddle, "sha2…mnop"},
		{ElideStart, "…ijklmnop"},
	} {
		got := Elide(text, 9, c.mode, byRune)
		if got != c.want {
			t.Errorf("%v gave %q, want %q", c.mode, got, c.want)
		}
		if byRune(got) > 9 {
			t.Errorf("%v gave %q, which is %d wide in 9 units", c.mode, got, byRune(got))
		}
		if !strings.Contains(got, Ellipsis) {
			t.Errorf("%v gave %q with nothing to say text was taken out", c.mode, got)
		}
	}
}

// Off is off: the text comes back whole and whatever does not fit is the
// surface's business.
func TestOffLeavesTheTextWhole(t *testing.T) {
	if got := Elide("a rather long line", 4, ElideOff, byRune); got != "a rather long line" {
		t.Errorf("off cut the text: %q", got)
	}
}

// The cut takes as much as it can: one rune less would still fit, so the
// answer is the widest one that does.
func TestTheCutKeepsAsMuchAsFits(t *testing.T) {
	const text = "abcdefghijklmnop"
	for width := Unit(4); width <= 16; width++ {
		got := Elide(text, width, ElideEnd, byRune)
		if byRune(got) > width {
			t.Fatalf("at %d units the answer %q is %d wide", width, got, byRune(got))
		}
		if got == text {
			continue
		}
		kept := len([]rune(strings.TrimSuffix(got, Ellipsis)))
		wider := string([]rune(text)[:kept+1]) + Ellipsis
		if byRune(wider) <= width {
			t.Errorf("at %d units it kept %d runes when %q also fits", width, kept, wider)
		}
	}
}

// Room for almost nothing: the ellipsis alone stands for the whole text, and
// where even that will not go, nothing is drawn rather than something wrong.
func TestWhereNothingFitsNothingIsDrawn(t *testing.T) {
	if got := Elide("hello", 1, ElideEnd, byRune); got != Ellipsis {
		t.Errorf("with room for the ellipsis alone it gave %q", got)
	}
	if got := Elide("hello", 0, ElideEnd, byRune); got != "" {
		t.Errorf("with room for nothing it gave %q", got)
	}
}

// The wire's words for the modes, and what an unknown one does: it is refused
// rather than quietly read as something else.
func TestTheModesAreNamed(t *testing.T) {
	for word, want := range map[string]ElideMode{
		"end": ElideEnd, "middle": ElideMiddle, "start": ElideStart, "off": ElideOff,
		"": ElideEnd, "MIDDLE": ElideMiddle,
	} {
		got, ok := ParseElideMode(word)
		if !ok || got != want {
			t.Errorf("%q read as %v (ok=%v), want %v", word, got, ok, want)
		}
	}
	if _, ok := ParseElideMode("sideways"); ok {
		t.Error("an unknown word was accepted")
	}
	for _, m := range []ElideMode{ElideEnd, ElideMiddle, ElideStart, ElideOff} {
		if back, ok := ParseElideMode(m.String()); !ok || back != m {
			t.Errorf("%v is named %q, which reads back as %v", m, m.String(), back)
		}
	}
}

// Cutting is by rune, so a multi-byte character is never split in half.
func TestACharacterIsNeverCutInHalf(t *testing.T) {
	const text = "ααββγγδδ"
	for _, mode := range []ElideMode{ElideEnd, ElideMiddle, ElideStart} {
		got := Elide(text, 6, mode, byRune)
		for _, r := range got {
			if r == '�' {
				t.Errorf("%v gave %q, which holds a broken character", mode, got)
			}
		}
	}
}
