package tui

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// rtlMarkMode "drift": an RTL cell emits its own base, then the combining marks
// of the cell to its LEFT. Each cell's own marks therefore land on the cell to
// its right, and the leftmost cell shows none. "normal" keeps each cell's own
// marks on its own base, and LTR combining is never touched.
func TestTUIDriftCarriesLeftNeighboursMarks(t *testing.T) {
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

	// drift: aleph's qamats drifts onto bet; aleph shows bare; bet's own sheva
	// (the last cell's marks) falls off the right edge.
	d := draw("drift", heb)
	if !strings.Contains(d, string(aleph)+string(bet)+string(qamats)) {
		t.Fatalf("drift: want aleph then bet+qamats (left mark drifted right), got %q", d)
	}
	if strings.Contains(d, string(aleph)+string(qamats)) {
		t.Fatalf("drift: aleph must not keep its own mark, got %q", d)
	}

	// drift must NOT touch an LTR base — that keeps its own mark.
	ltr := string('e') + string('́') // e + combining acute
	if got := draw("drift", ltr); !strings.Contains(got, ltr) {
		t.Fatalf("drift: LTR combining must be untouched %q, got %q", ltr, got)
	}
}

// Under drift a cell carries its LEFT neighbour's marks, so its rendered content
// depends on that neighbour. Changing only the neighbour's mark must re-emit
// THIS cell (which shows the mark) — and only it, with no cascade — because the
// diff compares the rendered content, not the raw cell.
func TestTUIDriftNeighbourChangeReemitsTheCarrier(t *testing.T) {
	const aleph = 'א'
	const qamats, hiriq = 'ָ', 'ִ'
	const bet, sheva = 'ב', 'ְ'
	core.SetRtlMarkMode("drift")
	defer core.SetRtlMarkMode("")

	b, out := newTestTUI(20, 2)
	b.BeginFrame()
	b.DrawText(0, 0, string(aleph)+string(qamats)+string(bet)+string(sheva), style.DefaultStyle(), nil)
	b.EndFrame()

	// Frame 2: aleph's mark changes qamats -> hiriq; bet is untouched.
	out.Reset()
	b.BeginFrame()
	b.DrawText(0, 0, string(aleph)+string(hiriq)+string(bet)+string(sheva), style.DefaultStyle(), nil)
	b.EndFrame()
	got := out.String()

	// bet's cell (which carries aleph's mark) re-emits with the NEW mark.
	if !strings.Contains(got, string(bet)+string(hiriq)) {
		t.Fatalf("carrier cell should re-emit the neighbour's new mark, got %q", got)
	}
	// The stale mark must be gone, and aleph itself (bare both frames) must not
	// have re-emitted — no cascade.
	if strings.ContainsRune(got, qamats) {
		t.Fatalf("stale qamats must not be re-emitted, got %q", got)
	}
	if strings.ContainsRune(got, aleph) {
		t.Fatalf("aleph is bare in both frames and must not re-emit (no cascade), got %q", got)
	}
}
