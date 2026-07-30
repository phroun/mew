//go:build mew

package trinkets

import (
	"strings"
	"testing"
)

// The self-test starts REAL sessions through the same hostPTY exec uses, so
// this exercises the whole terminal path rather than reasoning about it: a
// pty is made, a child runs in it, and its bytes come back. A regression
// anywhere along that chain fails here, on the platform it broke on.
func TestPTYDiagnoseRunsRealProbes(t *testing.T) {
	report := ptyDiagnose()
	t.Log("\n" + report)

	if !strings.Contains(report, "MEW-PTY-OK") && !strings.Contains(report, "MEW-CONPTY-OK") {
		t.Error("the echo probe's output never came back: the terminal path is broken here")
	}
	if !strings.Contains(report, "the plumbing WORKS") {
		t.Error("no probe reported success")
	}
	if strings.Contains(report, "the session never started") {
		t.Error("a probe could not start a session at all")
	}
	// The report has to name what ran and where, or it is not worth carrying
	// off a machine someone cannot debug on.
	for _, want := range []string{"platform:", "shells on PATH", "probes ("} {
		if !strings.Contains(report, want) {
			t.Errorf("report is missing %q", want)
		}
	}
}

// Raw terminal bytes are quoted, not pasted: a probe's output is full of
// escapes, and a report that reproduced them would steer the display it is
// being read in.
func TestQuoteProbe(t *testing.T) {
	got := quoteProbe([]byte("hi\x1b[0m\r\n\x00"), 100)
	// Two hex digits always: \x0 is not a byte, and a report that spells one
	// is a report someone has to decode twice.
	want := `"hi\e[0m\r\n\x00"`
	if got != want {
		t.Errorf("quoteProbe = %s, want %s", got, want)
	}
	if !strings.Contains(quoteProbe(make([]byte, 50), 10), "truncated") {
		t.Error("a long capture should say it was truncated")
	}
}
