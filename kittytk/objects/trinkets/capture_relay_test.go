//go:build mew

package trinkets

import (
	"bytes"
	"testing"

	"github.com/phroun/purfecterm"
)

type stubCaptureSink struct {
	got   []byte
	lines []string
	live  []string // a short label per live event, in order
}

func (s *stubCaptureSink) Output(data []byte)  { s.got = append(s.got, data...) }
func (s *stubCaptureSink) LineOff(line string) { s.lines = append(s.lines, line) }

func (s *stubCaptureSink) LiveWrite(x, y int, text, sgr string) { s.live = append(s.live, "write") }
func (s *stubCaptureSink) LiveCursorMove(x, y int)              { s.live = append(s.live, "cursor") }
func (s *stubCaptureSink) LiveNewline(x, y int)                 { s.live = append(s.live, "newline") }
func (s *stubCaptureSink) LiveLineWrap(x, y int)                { s.live = append(s.live, "wrap") }
func (s *stubCaptureSink) LiveBackspace(x, y int)               { s.live = append(s.live, "backspace") }
func (s *stubCaptureSink) LiveScrollLineOff(n int)              { s.live = append(s.live, "scrolloff") }
func (s *stubCaptureSink) LiveClearScreen()                     { s.live = append(s.live, "clear") }

// The relay forwards each OnOutput chunk to the sink verbatim, and forwards
// OnLineOff only when wantsLines is set (a raw session skips the serialization).
func TestCaptureRelayForwards(t *testing.T) {
	sink := &stubCaptureSink{}
	r := captureRelay{sink: sink}
	r.OnOutput([]byte("abc"))
	r.OnOutput([]byte("de"))
	if !bytes.Equal(sink.got, []byte("abcde")) {
		t.Fatalf("relayed output %q, want %q", sink.got, "abcde")
	}

	one := purfecterm.LineInfo{}
	captureRelay{sink: sink, wantsLines: false}.OnLineOff([]purfecterm.Cell{{Char: 'x'}}, one)
	if len(sink.lines) != 0 {
		t.Fatalf("wantsLines=false forwarded %d lines, want 0", len(sink.lines))
	}
	captureRelay{sink: sink, wantsLines: true}.OnLineOff([]purfecterm.Cell{{Char: 'x'}}, one)
	if len(sink.lines) != 1 {
		t.Fatalf("wantsLines=true forwarded %d lines, want 1", len(sink.lines))
	}
}

// The relay forwards the structural live events only when wantsLive is set, so a
// raw or lines session pays nothing for them.
func TestCaptureRelayLiveGating(t *testing.T) {
	drive := func(r captureRelay) {
		r.OnWrite(0, 0, "a", "")
		r.OnCursorMove(1, 0)
		r.OnNewline(0, 1)
		r.OnLineWrap(0, 2)
		r.OnBackspace(1, 2)
		r.OnScrollLineOff(1)
		r.OnClearScreen()
	}

	off := &stubCaptureSink{}
	drive(captureRelay{sink: off, wantsLive: false})
	if len(off.live) != 0 {
		t.Fatalf("wantsLive=false forwarded %v, want none", off.live)
	}

	on := &stubCaptureSink{}
	drive(captureRelay{sink: on, wantsLive: true})
	want := []string{"write", "cursor", "newline", "wrap", "backspace", "scrolloff", "clear"}
	if len(on.live) != len(want) {
		t.Fatalf("live events = %v, want %v", on.live, want)
	}
	for i, k := range want {
		if on.live[i] != k {
			t.Fatalf("live[%d] = %q, want %q", i, on.live[i], k)
		}
	}
}
