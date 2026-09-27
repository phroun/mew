//go:build !windows

package tui

import (
	"io"
	"strings"
	"testing"
)

// With nothing to suspend to, SuspendHost says so and touches nothing: no
// escapes written, no stop sent. A stop sent here would stop the test run.
func TestSuspendHostRefusesWithNothingToSuspendTo(t *testing.T) {
	for _, tc := range []struct {
		what       string
		jobControl bool
	}{
		{"job control turned off", false},
		{"no terminal to hand back", true},
	} {
		var out strings.Builder
		b := NewTUIBackend(TUIOptions{Output: io.Discard, JobControl: tc.jobControl})
		b.ttyOut = &out
		b.fd = -1
		if b.SuspendHost() {
			t.Errorf("%s: SuspendHost reported a suspend", tc.what)
		}
		if out.Len() != 0 {
			t.Errorf("%s: SuspendHost wrote %q", tc.what, out.String())
		}
	}
}

// Job control is on unless a host turns it off.
func TestJobControlIsOnByDefault(t *testing.T) {
	if !DefaultTUIOptions().JobControl {
		t.Error("DefaultTUIOptions leaves JobControl off; a SIGTSTP would stop " +
			"the process with the terminal still raw")
	}
}
