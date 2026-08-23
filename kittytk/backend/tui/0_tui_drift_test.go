package tui

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// rtlMarkMode "drift": an RTL cell emits its own base, then the combining marks
// of the cell to its RIGHT. Each cell's own marks therefore land on the cell to
// its left, and the rightmost cell shows none. "normal" keeps each cell's own
// marks on its own base, and LTR combining is never touched.
func TestTUIDriftCarriesRightNeighboursMarks(t *testing.T) {
	// Two Hebrew cells laid out left to right: aleph+qamats, then bet+sheva.
	const aleph, qamats = 'א', 'ָ'
	const bet, sheva = 'ב', 'ְ'
	heb := string(aleph) + string(qamats) + string(bet) + string(sheva)

	draw := func(mode, s string) string {
		core.SetRtlMarkMode(mode)
		b, out := newTestTUI(20, 2)
		b.BeginFrame()
		b.DrawText(0, 0, s, style.DefaultStyle(), nil)
		b.EndFrame()
		return out.String()
	}
	defer core.SetRtlMarkMode("")

	// normal: each base keeps its own mark.
	norm := draw("normal", heb)
	if !strings.Contains(norm, string(aleph)+string(qamats)) || !strings.Contains(norm, string(bet)+string(sheva)) {
		t.Fatalf("normal: want each base with its own mark, got %q", norm)
	}

	// drift: bet's sheva drifts LEFT onto aleph; aleph's own qamats falls off the
	// left edge; bet goes bare.
	d := draw("drift", heb)
	if !strings.Contains(d, string(aleph)+string(sheva)) {
		t.Fatalf("drift: want aleph carrying the RIGHT cell's mark (sheva), got %q", d)
	}
	if strings.Contains(d, string(aleph)+string(qamats)) {
		t.Fatalf("drift: aleph must not keep its own mark, got %q", d)
	}
	if strings.Contains(d, string(bet)+string(sheva)) {
		t.Fatalf("drift: bet must be bare (its mark moved left), got %q", d)
	}

	// drift must NOT touch an LTR base — that keeps its own mark.
	ltr := string('e') + string('́') // e + combining acute
	if got := draw("drift", ltr); !strings.Contains(got, ltr) {
		t.Fatalf("drift: LTR combining must be untouched %q, got %q", ltr, got)
	}
}

// Under drift a cell carries its RIGHT neighbour's marks, so its rendered
// content depends on that neighbour. Changing only the neighbour's mark must
// re-emit THIS cell (which shows the mark) — and only it, with no cascade —
// because the diff compares the rendered content, not the raw cell.
func TestTUIDriftNeighbourChangeReemitsTheCarrier(t *testing.T) {
	const aleph, qamats = 'א', 'ָ'
	const bet, sheva, hiriq = 'ב', 'ְ', 'ִ'
	core.SetRtlMarkMode("drift")
	defer core.SetRtlMarkMode("")

	b, out := newTestTUI(20, 2)
	b.BeginFrame()
	b.DrawText(0, 0, string(aleph)+string(qamats)+string(bet)+string(sheva), style.DefaultStyle(), nil)
	b.EndFrame()

	// Frame 2: bet's mark changes sheva -> hiriq; aleph is untouched.
	out.Reset()
	b.BeginFrame()
	b.DrawText(0, 0, string(aleph)+string(qamats)+string(bet)+string(hiriq), style.DefaultStyle(), nil)
	b.EndFrame()
	got := out.String()

	// aleph's cell (which carries bet's mark) re-emits with the NEW mark.
	if !strings.Contains(got, string(aleph)+string(hiriq)) {
		t.Fatalf("carrier cell should re-emit the neighbour's new mark, got %q", got)
	}
	// The stale mark must be gone, and bet itself (bare both frames) must not
	// have re-emitted — no cascade.
	if strings.ContainsRune(got, sheva) {
		t.Fatalf("stale sheva must not be re-emitted, got %q", got)
	}
	if strings.ContainsRune(got, bet) {
		t.Fatalf("bet is bare in both frames and must not re-emit (no cascade), got %q", got)
	}
}

// The shin dot, sin dot, and dagesh/mappiq do NOT drift, but under drift they
// are folded into the base's presentation form rather than left free-standing —
// a drift terminal misplaces a loose point just as it does a vowel. The folded
// base still steals any RTL vowel from the cell to its right. An LTR mark of
// another script has no presentation form, so it neither folds nor drifts.
func TestTUIDriftComposesDotsDageshKeepsLTR(t *testing.T) {
	core.SetRtlMarkMode("drift")
	defer core.SetRtlMarkMode("")
	draw := func(s string) string {
		b, out := newTestTUI(20, 2)
		b.BeginFrame()
		b.DrawText(0, 0, s, style.DefaultStyle(), nil)
		b.EndFrame()
		return out.String()
	}

	const shin, shinDot = 'ש', 'ׁ'       // U+05E9, U+05C1
	const shinWithDot = rune(0xFB2A)     // shin folded with its shin dot
	const dalet, dagesh = 'ד', 'ּ'       // U+05D3, U+05BC
	const daletWithDagesh = rune(0xFB33) // dalet folded with its dagesh
	const bet, sheva = 'ב', 'ְ'          // U+05D1, U+05B0
	const acute = '́'                    // U+0301, LTR/other-script mark

	// shin folds its dot into FB2A, then still steals bet's sheva.
	got := draw(string(shin) + string(shinDot) + string(bet) + string(sheva))
	if strings.ContainsRune(got, shinDot) {
		t.Fatalf("shin dot must be folded away, not free-standing, got %q", got)
	}
	if !strings.Contains(got, string(shinWithDot)+string(sheva)) {
		t.Fatalf("shin should fold its dot and steal bet's sheva, got %q", got)
	}

	// dagesh/mappiq folds into the letter's presentation form too.
	if got := draw(string(dalet) + string(dagesh) + string(bet) + string(sheva)); !strings.Contains(got, string(daletWithDagesh)) || strings.ContainsRune(got, dagesh) {
		t.Fatalf("dagesh must fold into FB33, not stay free-standing, got %q", got)
	}

	// an LTR combining mark on an RTL base has no fold and does not drift.
	if got := draw(string(shin) + string(acute) + string(bet) + string(sheva)); !strings.Contains(got, string(shin)+string(acute)) {
		t.Fatalf("LTR mark must stay on its own base, got %q", got)
	}
}
