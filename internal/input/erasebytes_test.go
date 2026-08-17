package input

import (
	"io"
	"testing"
	"time"

	"github.com/phroun/direct-key-handler/keyboard"
)

// feedBytes runs raw terminal input through the handler configured the way mew
// configures it — with mewKeyNames installed — and returns the key tokens a
// keymap would actually be asked to match.
//
// This is the terminal path, which is NOT normalizeHostKey: there the handler is
// given mew's names and emits them directly, so nothing in this package gets a
// chance to correct a name. What the two vocabularies agree on is the whole of
// the contract, and it is only observable from here.
func feedBytes(t *testing.T, raw string) []string {
	t.Helper()
	pr, pw := io.Pipe()
	manage := false
	h := keyboard.New(keyboard.Options{
		InputReader:    pr,
		ManageTerminal: &manage,
		KeyNames:       mewKeyNames,
	})
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	defer h.Stop()
	go func() {
		pw.Write([]byte(raw))
		pw.Close()
	}()
	var keys []string
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case k := <-h.Keys:
			keys = append(keys, k)
		case <-deadline:
			return keys
		}
	}
}

// The two erase bytes reach a keymap under mew's two names, held modifier or
// not.
//
// A terminal sends BS (8) or DEL (127) for its erase key by lineage and cannot
// know which mew expects, so mew has always had two names and binds both to the
// same action. That only works if the two survive the trip. Under Mega they did
// not: the upstream table spelled ESC DEL "M-Backspace", so both bytes arrived
// as "M-back" and "M-del" was unreachable — and ESC DEL is what Alt+Backspace,
// the delete-previous-word chord, sends from every terminal whose kbs is ^?.
func TestBothEraseBytesSurviveWithAndWithoutMega(t *testing.T) {
	for _, tc := range []struct{ raw, want, what string }{
		{"\x08", "back", "BS, the vt100 lineage's erase"},
		{"\x7f", "del", "DEL, what most terminals send"},
		{"\x1b\x08", "M-back", "and under Mega"},
		{"\x1b\x7f", "M-del", "where the two used to collapse together"},
	} {
		got := feedBytes(t, tc.raw)
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("%q (%s) -> %v, want [%s]", tc.raw, tc.what, got, tc.want)
		}
	}

	// The point of two names is that they are two. Collapsed, an application
	// cannot tell which erase byte its terminal sent, which is what having
	// aliased them deliberately is supposed to make unnecessary rather than
	// impossible.
	if a, b := feedBytes(t, "\x1b\x08"), feedBytes(t, "\x1b\x7f"); len(a) == 1 && len(b) == 1 && a[0] == b[0] {
		t.Errorf("ESC BS and ESC DEL both arrive as %q", a[0])
	}
}

// The home-row key is "return" whichever byte or protocol carries it, and it
// stays that way with Mega held. Upstream calls the bare key "Return" and the
// keypad's "Enter"; mew folds the two, so both spellings have to land here.
func TestTheHomeRowKeyIsReturnUnderMegaToo(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"\r", "return"},
		{"\x1b\r", "M-return"},
	} {
		if got := feedBytes(t, tc.raw); len(got) != 1 || got[0] != tc.want {
			t.Errorf("%q -> %v, want [%s]", tc.raw, got, tc.want)
		}
	}
}

// A letter under Mega carries Shift in its case, which is how mew's binding
// syntax spells a shifted shown key. "M-S-x" would be a second spelling of one
// chord, and a keymap written either way would work on one kind of terminal and
// not the other.
func TestAMegaLetterIsCaseful(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"\x1bx", "M-x"},
		{"\x1bX", "M-X"},
		{"\x1b%", "M-%"}, // the shifted symbol beside it, for comparison
	} {
		if got := feedBytes(t, tc.raw); len(got) != 1 || got[0] != tc.want {
			t.Errorf("%q -> %v, want [%s]", tc.raw, got, tc.want)
		}
	}
}

// The keypad reaches a keymap prefixed, with mew's name for the base.
//
// Both halves have to hold at once: upstream peels "P-" before applying mew's
// table, so a pad key arrives as the prefix plus mew's spelling rather than
// upstream's. A keymap writes "P-home", not "P-Home".
func TestTheKeypadArrivesPrefixedWithMewsNames(t *testing.T) {
	for _, tc := range []struct{ raw, want, what string }{
		{"\x1b[57423u", "P-home", "the pad's Home, NumLock off"},
		{"\x1b[57406u", "P-7", "and its 7, locked"},
		{"\x1b[57427u", "P-begin", "the pad's own base name"},
		{"\x1b[57414u", "P-return", "the pad's Enter, folded onto return"},
		{"\x1b[57426u", "P-del", "the pad's erase, under mew's name for DEL"},
		{"\x1b[57421;5u", "C-P-pgup", "stacked with Control"},
		{"\x1b[57406;5u", "P-^7", "and Control on a shown pad key takes the caret"},
	} {
		if got := feedBytes(t, tc.raw); len(got) != 1 || got[0] != tc.want {
			t.Errorf("%q (%s) -> %v, want [%s]", tc.raw, tc.what, got, tc.want)
		}
	}
}

// F13 through F20 arrive on the legacy path as well as the kitty one. mew spells
// them the same as upstream, so the only question is whether they arrive at all
// — and until recently the tilde table stopped at F12, so a terminal sending
// F15 delivered five keystrokes and typed "[28~" into the buffer.
func TestTheHighFunctionKeysArrive(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"\x1b[25~", "F13"},
		{"\x1b[28~", "F15"},
		{"\x1b[29~", "F16"},
		{"\x1b[34~", "F20"},
	} {
		if got := feedBytes(t, tc.raw); len(got) != 1 || got[0] != tc.want {
			t.Errorf("%q -> %v, want [%s]", tc.raw, got, tc.want)
		}
	}
}
