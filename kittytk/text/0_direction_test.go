package text

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// The first strongly directional character names the direction, and a string
// with none names nothing -- the answer digits, punctuation and empty captions
// all give, which is why it is reported rather than guessed at.
func TestFirstStrongDirection(t *testing.T) {
	for _, c := range []struct {
		text string
		want core.Direction
	}{
		{"Hello", core.DirLTR},
		{"שלום", core.DirRTL},
		{"مرحبا", core.DirRTL},
		// The FIRST strong one decides, not the majority and not the last.
		{"OK שלום", core.DirLTR},
		{"שלום OK", core.DirRTL},
		// Neutrals are skipped over rather than deciding.
		{"(שלום)", core.DirRTL},
		{"12 Hello", core.DirLTR},
		// Nothing strong at all.
		{"", core.DirInherit},
		{"123", core.DirInherit},
		{"  ", core.DirInherit},
		{"3.14 (%)", core.DirInherit},
	} {
		if got := FirstStrongDirection(c.text); got != c.want {
			t.Errorf("FirstStrongDirection(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}
