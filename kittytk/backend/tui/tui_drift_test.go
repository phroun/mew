package tui

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// rtlMarkMode "drift" emits an RTL base's combining marks BEFORE the base on the
// wire; "normal" keeps the usual base-then-mark order. The reorder is emit-only,
// so LTR combining is untouched.
func TestTUIDriftEmitsRTLMarkBeforeBase(t *testing.T) {
	const bet, sheva = 'ב', 'ְ' // ב + ְ (Hebrew base + niqqud)
	heb := string(bet) + string(sheva)
	const e, acute = 'e', '́' // LTR base + combining acute
	ltr := string(e) + string(acute)

	draw := func(mode, s string) string {
		core.SetRtlMarkMode(mode)
		b, out := newTestTUI(20, 2)
		b.BeginFrame()
		b.DrawText(0, 0, s, style.DefaultStyle(), nil)
		b.EndFrame()
		return out.String()
	}
	defer core.SetRtlMarkMode("")

	// normal: base then mark.
	if got := draw("normal", heb); !strings.Contains(got, heb) {
		t.Fatalf("normal: want base-then-mark %q, got %q", heb, got)
	}

	// drift: mark then base for the RTL cell.
	driftWant := string(sheva) + string(bet)
	got := draw("drift", heb)
	if !strings.Contains(got, driftWant) {
		t.Fatalf("drift: want mark-then-base %q, got %q", driftWant, got)
	}
	if strings.Contains(got, heb) {
		t.Fatalf("drift: should not keep base-then-mark, got %q", got)
	}

	// drift must NOT touch an LTR base — that stays base-then-mark.
	if got := draw("drift", ltr); !strings.Contains(got, ltr) {
		t.Fatalf("drift: LTR combining must be untouched %q, got %q", ltr, got)
	}
}
