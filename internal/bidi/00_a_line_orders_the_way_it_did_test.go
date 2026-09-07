package bidi

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// Everything this package answers, for a corpus that reaches every rule it
// has, written down and compared against what it answered before.
//
// The other tests in here say what the answers OUGHT to be, case by case, and
// they are the ones to read. This one says only that the answers have not
// MOVED -- which is a different question, and the one that matters while the
// engine underneath is being replaced. A change in this file's golden is not
// automatically wrong; it is a claim that has to be looked at and agreed to.
//
//	go test ./internal/bidi -run TheWayItDid -update
//
// rewrites the golden from the current code. Read the diff before keeping it.
var update = flag.Bool("update", false, "rewrite testdata/orderings.txt from the current answers")

// The corpus. Every line here is here because some rule in this package treats
// it differently from the others; the comment on each says which.
var orderingCorpus = []struct {
	name string
	text string
}{
	{"empty", ""},
	{"ascii", "hello world"},               // the pure-LTR fast path: no layout at all
	{"digits", "12345"},                    // numbers with no direction of their own
	{"punctuation", "...!?"},               // neutrals only
	{"hebrew", "שלום"},                     // one RTL run
	{"hebrew words", "שלום עולם"},          // a neutral between two RTL runs
	{"ltr in rtl", "אבג abc דהו"},          // an LTR island the RTL region encloses
	{"rtl in ltr", "abc שלום xyz"},         // the mirror case
	{"digits in rtl", "שלום 123 עולם"},     // the numeric absorption of L2
	{"digits ending rtl", "שלום 123"},      // numbers with no RTL run after them
	{"arabic", "العربية"},                  // joining: initial, medial, final
	{"arabic lam alef", "لا إله إلا الله"}, // the mandatory ligature, twice
	{"arabic harakat", "مَرْحَبًا"},        // transparent marks inside the join context
	{"arabic tatweel", "بــاب"},            // tatweel as joining-causing
	{"arabic zwj", "ب\u200dب"},             // ZWJ likewise
	{"hebrew niqqud", "שָׁלוֹם"},           // points that fold, and vowels that do not
	{"hebrew and arabic", "שלום العربية"},  // two RTL scripts in one run
	{"brackets in rtl", "שלום (עולם)"},     // mirrored pairs inside an RTL run
	{"brackets in ltr", "a (שלום) b"},      // the same pair outside one
	{"cjk", "日本語 שלום"},                    // wide cells beside an RTL run
	{"latin combining", "cafe\u0301"},      // a general diacritic: rides any base
	{"rlm", "abc\u200fשלום"},               // an explicit right-to-left mark
	{"lrm", "שלום\u200eabc"},               // and its counterpart
	{"embedding", "abc\u202bשלום\u202c"},   // RLE ... PDF
	{"isolate", "abc\u2067שלום\u2069"},     // RLI ... PDI
	{"first strong isolate", "\u2068שלום\u2069 abc"},
	{"tab and control", "a\tb\x07c"},         // neither takes a cell of its own quietly
	{"nko", "abc \u07d2\u07de\u07cf xyz"},    // cursive RTL with no presentation forms
	{"syriac", "abc \u0710\u0712\u0713 xyz"}, // likewise
	{"samaritan", "abc \u0800\u0801 xyz"},    // non-cursive RTL
	{"thaana", "abc \u078b\u07a8 xyz"},       // RTL with its own marks

	// The cluster rule, which is where an ill-formed mark decides whether it
	// travels with what precedes it (see TestDefectiveMarkIsItsOwnClusterInRTL).
	{"well formed mark", string([]rune{'ל', 'ִ', 'א'})},
	{"baseless mark", string([]rune{'ל', ' ', 'ִ', 'א'})},
	{"baseless mark at end", string([]rune{'ל', ' ', 'ִ'})},
	{"mixed script mark", string([]rune{'א', 'ל', 'ँ', 'א'})},
	{"mark on dotted circle", string([]rune{'a', '◌', 'ִ', 'b'})},
	{"mark opening the line", string([]rune{'ִ', 'ל'})},
}

func TestALineOrdersTheWayItDid(t *testing.T) {
	const golden = "testdata/orderings.txt"
	got := renderOrderings()
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s (%d lines)", golden, strings.Count(got, "\n"))
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v -- run with -update to write it", err)
	}
	if string(want) == got {
		return
	}
	// Name the first line that moved: a whole-file diff of a few hundred lines
	// says nothing about which rule changed.
	wl, gl := strings.Split(string(want), "\n"), strings.Split(got, "\n")
	for i := 0; i < len(wl) || i < len(gl); i++ {
		w, g := "", ""
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g {
			t.Fatalf("%s line %d:\n  was %q\n  now %q\n(%d lines differ in all)",
				golden, i+1, w, g, countDiff(wl, gl))
		}
	}
}

func countDiff(a, b []string) int {
	n := 0
	for i := 0; i < len(a) || i < len(b); i++ {
		x, y := "", ""
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			n++
		}
	}
	return n
}

// renderOrderings writes every answer the package gives for the corpus, in a
// form that is all ASCII: the text itself is recorded as code points, so the
// golden reads the same way in any editor and cannot be reordered by one.
func renderOrderings() string {
	var b strings.Builder
	seen := map[rune]bool{}

	for _, c := range orderingCorpus {
		runes := []rune(c.text)
		for _, r := range runes {
			seen[r] = true
		}
		fmt.Fprintf(&b, "case %s\n", c.name)
		fmt.Fprintf(&b, "  text   %s\n", points(runes))
		for _, base := range []bool{false, true} {
			for _, marked := range []bool{false, true} {
				var l *Layout
				if marked {
					l = ComputeMarked(runes, base)
				} else {
					l = Compute(runes, base)
				}
				fmt.Fprintf(&b, "  %-6s %s\n", mode(base, marked), layoutLine(l))
			}
		}
		fmt.Fprintf(&b, "  shape  %s\n", points(Shape(runes)))
		for _, base := range []bool{false, true} {
			var at []string
			for i := 0; i <= len(runes); i++ {
				at = append(at, boolDigit(RTLAt(runes, i, base)))
			}
			fmt.Fprintf(&b, "  rtlat%s %s\n", baseTag(base), strings.Join(at, ""))
		}
		b.WriteString("\n")
	}

	// The per-rune answers, once each, over every rune the corpus contains.
	var runes []rune
	for r := range seen {
		runes = append(runes, r)
	}
	sort.Slice(runes, func(i, j int) bool { return runes[i] < runes[j] })
	b.WriteString("runes\n")
	for _, r := range runes {
		var tags []string
		if m := Mirror(r); m != r {
			tags = append(tags, "mirror="+point(m))
		}
		if IsStrongRTL(r) {
			tags = append(tags, "strongRTL")
		}
		if IsDirectionControl(r) {
			tags = append(tags, "control")
		}
		if len(tags) == 0 {
			continue // nothing this package has to say about it
		}
		fmt.Fprintf(&b, "  %s %s\n", point(r), strings.Join(tags, " "))
	}
	return b.String()
}

func mode(baseRTL, marked bool) string {
	s := "ltr"
	if baseRTL {
		s = "rtl"
	}
	if marked {
		s += "+m"
	}
	return s
}

func baseTag(baseRTL bool) string {
	if baseRTL {
		return "R"
	}
	return "L"
}

func boolDigit(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// layoutLine renders a layout, or the word every consumer branches on when
// there is none.
func layoutLine(l *Layout) string {
	if l == nil {
		return "(nil)"
	}
	var perm []string
	for _, p := range l.Perm {
		switch p {
		case MarkerLTR:
			perm = append(perm, ">")
		case MarkerRTL:
			perm = append(perm, "<")
		case MarkerEnd:
			perm = append(perm, "|")
		default:
			perm = append(perm, fmt.Sprint(p))
		}
	}
	var rtl []string
	for _, r := range l.RTL {
		rtl = append(rtl, boolDigit(r))
	}
	out := "perm=[" + strings.Join(perm, " ") + "] rtl=" + strings.Join(rtl, "")
	if l.Marked {
		out += " marked"
	}
	if l.Glyph != nil {
		out += " glyph=" + points(l.Glyph)
	}
	return out
}

// points renders runes as code points, with the ligature's absorbed slot named
// rather than printed as the sentinel it is.
func points(runes []rune) string {
	if runes == nil {
		return "(nil)"
	}
	if len(runes) == 0 {
		return "(empty)"
	}
	var out []string
	for _, r := range runes {
		out = append(out, point(r))
	}
	return strings.Join(out, " ")
}

func point(r rune) string {
	if r == LigatureAbsorbed {
		return "ABSORBED"
	}
	return fmt.Sprintf("%04X", r)
}
