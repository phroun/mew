package input

import (
	"bytes"
	"testing"
)

func TestSplitHScroll(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		wantOut    string
		wantEvents []string
	}{
		{"plain text untouched", "hello", "hello", nil},
		{"vertical wheel passes through", "\x1b[<65;10;5M", "\x1b[<65;10;5M", nil},
		{"scroll up passes through", "\x1b[<64;1;1M", "\x1b[<64;1;1M", nil},
		{"wheel left -> event, stripped", "\x1b[<66;10;5M", "", []string{"MouseScrollLeft"}},
		{"wheel right -> event, stripped", "\x1b[<67;3;4M", "", []string{"MouseScrollRight"}},
		{"release form (m) too", "\x1b[<66;3;4m", "", []string{"MouseScrollLeft"}},
		{"surrounded by text", "ab\x1b[<67;1;1Mcd", "abcd", []string{"MouseScrollRight"}},
		{"click report untouched", "\x1b[<0;5;5M", "\x1b[<0;5;5M", nil},
		{"bare ESC untouched", "\x1bOP", "\x1bOP", nil},
		{"two horizontals", "\x1b[<66;1;1M\x1b[<67;1;1M", "", []string{"MouseScrollLeft", "MouseScrollRight"}},
		{"mixed vert+horiz", "\x1b[<65;1;1M\x1b[<66;1;1M", "\x1b[<65;1;1M", []string{"MouseScrollLeft"}},
	}
	for _, c := range cases {
		out, ev, carry := splitHScroll(nil, []byte(c.in))
		if len(carry) != 0 {
			t.Errorf("%s: unexpected carry %q", c.name, carry)
		}
		if string(out) != c.wantOut {
			t.Errorf("%s: out = %q, want %q", c.name, out, c.wantOut)
		}
		if !eqStrs(ev, c.wantEvents) {
			t.Errorf("%s: events = %v, want %v", c.name, ev, c.wantEvents)
		}
	}
}

// A report split across two reads must still be recognized (carry mechanism)
// and must not corrupt the passthrough stream.
func TestSplitHScrollAcrossReads(t *testing.T) {
	full := "x\x1b[<66;12;34My"
	for cut := 1; cut < len(full); cut++ {
		out1, ev1, carry := splitHScroll(nil, []byte(full[:cut]))
		out2, ev2, carry2 := splitHScroll(carry, []byte(full[cut:]))
		if len(carry2) != 0 {
			t.Errorf("cut %d: leftover carry %q", cut, carry2)
		}
		gotOut := string(out1) + string(out2)
		gotEv := append(append([]string(nil), ev1...), ev2...)
		if gotOut != "xy" {
			t.Errorf("cut %d: passthrough = %q, want %q", cut, gotOut, "xy")
		}
		if !eqStrs(gotEv, []string{"MouseScrollLeft"}) {
			t.Errorf("cut %d: events = %v, want [MouseScrollLeft]", cut, gotEv)
		}
	}
}

// A non-mouse byte stream must pass through byte-for-byte, whatever the read
// boundaries (the filter must never eat ordinary input).
func TestSplitHScrollNeverEatsInput(t *testing.T) {
	in := []byte("line1\n\x1b[Ahello\x1b[<0;1;1M\x1b]52;c;AAAA\x07done")
	out, ev, carry := splitHScroll(nil, in)
	if len(ev) != 0 {
		t.Errorf("no horizontal wheel present; got events %v", ev)
	}
	if !bytes.Equal(append(out, carry...), in) {
		t.Errorf("passthrough changed the stream:\n got %q\nwant %q", append(out, carry...), in)
	}
}

func eqStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
