package trinkets

import "testing"

// Pasting multi-line clipboard text into a terminal must convert LF/CRLF line
// endings to CR - the terminal's Enter byte - so the child's line discipline
// acts on them instead of swallowing the breaks.
func TestNormalizePasteNewlines(t *testing.T) {
	cases := []struct{ in, want string }{
		{"one\ntwo\nthree", "one\rtwo\rthree"}, // LF -> CR
		{"one\r\ntwo", "one\rtwo"},             // CRLF -> CR
		{"already\rcr", "already\rcr"},         // bare CR left alone
		{"mixed\r\nb\nc\r", "mixed\rb\rc\r"},   // mix of CRLF, LF, CR
		{"no breaks", "no breaks"},             // untouched
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizePasteNewlines(c.in); got != c.want {
			t.Errorf("normalizePasteNewlines(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
