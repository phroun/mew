package trinkets

import (
	"testing"
)

// The trinket re-encodes purfecterm's (shape, blink) split back into the
// DECSCUSR parameter that produced it, so the platform caret wears exactly the
// shape the hosted program asked for.
func TestDECSCUSRRoundTrip(t *testing.T) {
	// purfecterm's mapping: 0/1 -> (block, blink), 2 -> (block, steady),
	// 3/4 -> underline, 5/6 -> bar.
	cases := []struct {
		shape, blink int
		want         int
	}{
		{0, 1, 1}, // blinking block
		{0, 0, 2}, // steady block
		{1, 1, 3}, // blinking underline
		{1, 0, 4}, // steady underline
		{2, 1, 5}, // blinking bar
		{2, 0, 6}, // steady bar
	}
	for _, tc := range cases {
		if got := decscusrFor(tc.shape, tc.blink); got != tc.want {
			t.Errorf("shape %d blink %d -> %d, want %d", tc.shape, tc.blink, got, tc.want)
		}
	}
}
