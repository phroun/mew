//go:build mew

package trinkets

import (
	"bytes"
	"testing"
)

type stubCaptureSink struct{ got []byte }

func (s *stubCaptureSink) Output(data []byte) { s.got = append(s.got, data...) }

// The relay forwards each purfecterm OnOutput chunk to the mew sink verbatim.
func TestCaptureRelayForwardsOnOutput(t *testing.T) {
	sink := &stubCaptureSink{}
	r := captureRelay{sink: sink}
	r.OnOutput([]byte("abc"))
	r.OnOutput([]byte("de"))
	if !bytes.Equal(sink.got, []byte("abcde")) {
		t.Fatalf("relayed %q, want %q", sink.got, "abcde")
	}
}
