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
}

func (s *stubCaptureSink) Output(data []byte)  { s.got = append(s.got, data...) }
func (s *stubCaptureSink) LineOff(line string) { s.lines = append(s.lines, line) }

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
