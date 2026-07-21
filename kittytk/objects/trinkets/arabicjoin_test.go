package trinkets

import "testing"

func TestArabicKashida(t *testing.T) {
	const (
		ain  = 0x0639 // dual
		lam  = 0x0644 // dual
		yeh  = 0x064A // dual
		kaf  = 0x0643 // dual
		meem = 0x0645 // dual
		ba   = 0x0628 // dual
		alef = 0x0627 // right-joining (joins prev only)
		hamza = 0x0621 // non-joining
	)
	space := rune(' ')

	cases := []struct {
		name                     string
		base, leftN, rightN      rune
		wantLeft, wantRight bool
	}{
		// "عليكم" visual cells: م ك ي ل ع. A medial dual letter (lam) with dual
		// neighbours on both sides joins both edges.
		{"medial lam", lam, yeh /*left=next*/, ain /*right=prev*/, true, true},
		// Meem at the visual left end (word start in visual terms) — its left
		// neighbour is a space, so no kashida on the left; joins right.
		{"final-ish meem", meem, space, kaf, false, true},
		// Ain at the visual right end — right neighbour space, joins left only.
		{"initial ain", ain, lam, space, true, false},
		// Alef is right-joining: connects to the previous (right) but NEVER to
		// the following (left), even with a dual neighbour there.
		{"alef joins prev only", alef, ba /*next*/, ba /*prev*/, false, true},
		// A dual letter whose next neighbour is alef: alef joins-prev, so the
		// letter DOES connect to it on the left.
		{"ba before alef", ba, alef, space, true, false},
		// Hamza joins nothing.
		{"hamza", hamza, lam, lam, false, false},
		// Non-Arabic base: never a kashida.
		{"latin", 'A', lam, lam, false, false},
	}
	for _, c := range cases {
		l, r := arabicKashida(c.base, c.leftN, c.rightN)
		if l != c.wantLeft || r != c.wantRight {
			t.Errorf("%s: got (left=%v,right=%v), want (left=%v,right=%v)",
				c.name, l, r, c.wantLeft, c.wantRight)
		}
	}
}
