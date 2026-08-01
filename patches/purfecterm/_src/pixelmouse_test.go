package purfecterm

import (
	"testing"
)

// A parser wired with a response sink, so query replies (DECRQM, XTWINOPS
// reports) can be captured the way a hosted app would read them.
func newRespParser(cols, rows int) (*Parser, *Buffer, *[]byte) {
	b := NewBuffer(cols, rows, 100)
	p := NewParser(b)
	var resp []byte
	p.SetResponseSink(func(data []byte) { resp = append(resp, data...) })
	return p, b, &resp
}

// ?1016 (SGR-Pixels) is a recognized DEC private mode that selects a pixel
// mouse encoding, distinct from ?1006, and falls back to SGR cells on reset.
func TestPixelMouseModeToggle(t *testing.T) {
	p, b, _ := newRespParser(80, 24)

	p.Parse([]byte("\x1b[?1006h")) // app enables SGR cells first (mew does)
	if got := b.GetMouseEncodingMode(); got != 1006 {
		t.Fatalf("after ?1006h encoding = %d, want 1006", got)
	}
	p.Parse([]byte("\x1b[?1016h")) // then upgrades to pixels
	if got := b.GetMouseEncodingMode(); got != 1016 {
		t.Fatalf("after ?1016h encoding = %d, want 1016", got)
	}
	p.Parse([]byte("\x1b[?1016l")) // reset pixels → back to SGR cells
	if got := b.GetMouseEncodingMode(); got != 1006 {
		t.Fatalf("after ?1016l encoding = %d, want 1006 (fall back to cells)", got)
	}
	p.Parse([]byte("\x1b[?1006l")) // reset cells → off
	if got := b.GetMouseEncodingMode(); got != 0 {
		t.Fatalf("after ?1006l encoding = %d, want 0", got)
	}
}

// EncodeMouseEvent emits the same SGR wire shape for 1006 and 1016; only the
// caller's coordinate meaning (cells vs pixels) differs.
func TestPixelMouseEncodingWire(t *testing.T) {
	cells := EncodeMouseEvent(MouseButtonLeft, 12, 5, true, 1006)
	pixels := EncodeMouseEvent(MouseButtonLeft, 12, 5, true, 1016)
	want := "\x1b[<0;12;5M"
	if string(cells) != want || string(pixels) != want {
		t.Fatalf("SGR wire mismatch: 1006=%q 1016=%q want %q", cells, pixels, want)
	}
}

// DECRQM (CSI ? Ps $ p) answers with a status report (CSI ? Ps ; status $ y):
// ?1016 reports recognized (set=1 when active, reset=2 otherwise); an unknown
// mode reports not-recognized (0).
func TestDECRQMPixelMouse(t *testing.T) {
	p, _, resp := newRespParser(80, 24)

	p.Parse([]byte("\x1b[?1016$p"))
	if got := string(*resp); got != "\x1b[?1016;2$y" {
		t.Fatalf("DECRQM ?1016 (reset) = %q, want %q", got, "\x1b[?1016;2$y")
	}

	*resp = nil
	p.Parse([]byte("\x1b[?1016h\x1b[?1016$p"))
	if got := string(*resp); got != "\x1b[?1016;1$y" {
		t.Fatalf("DECRQM ?1016 (set) = %q, want %q", got, "\x1b[?1016;1$y")
	}

	*resp = nil
	p.Parse([]byte("\x1b[?7999$p")) // a mode PurfecTerm does not implement
	if got := string(*resp); got != "\x1b[?7999;0$y" {
		t.Fatalf("DECRQM unknown = %q, want %q", got, "\x1b[?7999;0$y")
	}
}

// CSI 16 t reports the cell pixel size the host set; CSI 14/18 t report the
// text area in pixels / characters.
func TestXTWINOPSCellPixelSize(t *testing.T) {
	p, b, resp := newRespParser(80, 24)
	b.SetCellPixelSize(9, 20) // width 9, height 20 device px

	p.Parse([]byte("\x1b[16t"))
	if got := string(*resp); got != "\x1b[6;20;9t" {
		t.Fatalf("CSI 16 t = %q, want %q (CSI 6;height;width t)", got, "\x1b[6;20;9t")
	}

	*resp = nil
	p.Parse([]byte("\x1b[14t"))
	if got := string(*resp); got != "\x1b[4;480;720t" { // 24*20, 80*9
		t.Fatalf("CSI 14 t = %q, want %q", got, "\x1b[4;480;720t")
	}

	*resp = nil
	p.Parse([]byte("\x1b[18t"))
	if got := string(*resp); got != "\x1b[8;24;80t" {
		t.Fatalf("CSI 18 t = %q, want %q", got, "\x1b[8;24;80t")
	}
}
