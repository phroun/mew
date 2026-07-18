package buffer

import (
	"strings"
	"testing"
)

// GetLineRange must return exactly what GetLine returns for each line in the
// span (both trimmed of the trailing terminator), for every sub-range —
// including ranges that touch a final empty line from a trailing newline.
func TestGetLineRangeMatchesGetLine(t *testing.T) {
	cases := []string{
		"",
		"only line no newline",
		"a\nb\nc",
		"a\nb\nc\n",
		"\n\n\n",
		"first\n\nthird\n",
		"héllo\nwörld\n☃ snowman\n",
		strings.Repeat("line\n", 50) + "tail",
	}
	for _, content := range cases {
		b := NewFromString(content)
		n := b.GetLineCount()
		trim := func(s string) string { return strings.TrimRight(s, "\n\r") }

		// Every [start,end) sub-range matches line-by-line GetLine.
		for start := 0; start <= n; start++ {
			for end := start; end <= n; end++ {
				got := b.GetLineRange(start, end)
				if len(got) != end-start {
					t.Fatalf("content %q range [%d,%d): got %d lines, want %d",
						content, start, end, len(got), end-start)
				}
				for i := 0; i < end-start; i++ {
					want := trim(b.GetLine(start + i))
					if trim(got[i]) != want {
						t.Fatalf("content %q range [%d,%d) line %d: got %q, want %q",
							content, start, end, start+i, trim(got[i]), want)
					}
				}
			}
		}
	}
}
