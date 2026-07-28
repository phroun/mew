//go:build mew

package trinkets

// The shared half of the terminal self-test behind mew's pty_diag command.
//
// A terminal that starts and stops with nothing to show for it gives the
// person debugging almost nothing to go on: every observable is on the far
// side of a pipe that is not working. So the host tests its own plumbing —
// with the SAME code path exec uses, not a reimplementation of it — and
// reports each step, whether a probe child's bytes ever came back, and how it
// ended. The per-platform half (hostPTYProbe) is in editor_mew_pty_unix.go
// and editor_mew_pty_windows.go.

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// probeWait bounds one probe, and settle is how long a probe keeps listening
// after the first bytes arrive.
//
// An interactive shell never exits, so waiting for it to finish means waiting
// the whole bound every time — three probes deep, on a main loop, that is a
// visible freeze. But the first byte already answers the question this is
// asking, so a probe that hears anything stops shortly after: a working
// machine finishes in well under a second, and only genuine silence costs the
// full wait.
const (
	probeWait = 2 * time.Second
	settle    = 250 * time.Millisecond
)

// probeResult is one probe's outcome, in the terms that distinguish the
// failures from each other.
type probeResult struct {
	Label    string
	Err      error    // the session never started
	Out      []byte   // what came back
	TimedOut bool     // nothing more was coming
	Exited   bool     // the child ended on its own
	Code     int      // ...with this status
	Trace    []string // what the platform calls said on the way
}

// ptyDiagnose builds the report mew's pty_diag command writes at the caret.
func ptyDiagnose() string {
	var b strings.Builder
	fmt.Fprintf(&b, "mew terminal diagnostics\n")
	fmt.Fprintf(&b, "  platform: %s/%s, go %s\n", runtime.GOOS, runtime.GOARCH, runtime.Version())
	if wd, err := os.Getwd(); err == nil {
		fmt.Fprintf(&b, "  cwd: %s\n", wd)
	}
	if home, err := os.UserHomeDir(); err == nil {
		fmt.Fprintf(&b, "  home: %s\n", home)
	}

	// Which shells this machine actually has, by the same lookup ptyProvider
	// does — a command mew cannot find is refused before any of this runs.
	b.WriteString("\nshells on PATH (exec.LookPath, as ptyProvider resolves them):\n")
	for _, name := range diagShellNames() {
		if p, err := exec.LookPath(name); err == nil {
			fmt.Fprintf(&b, "  %-16s %s\n", name, p)
		} else {
			fmt.Fprintf(&b, "  %-16s not found (%v)\n", name, err)
		}
	}

	b.WriteString("\nprobes (each starts a real session through the same hostPTY exec uses):\n")
	for _, r := range hostPTYProbe() {
		writeProbe(&b, r)
	}
	b.WriteString("\nEnd of diagnostics. The probe that behaves differently from the\n")
	b.WriteString("others is the one that names the fault.\n")
	return b.String()
}

// writeProbe renders one probe, saying plainly which of the failures it is:
// the session never started, it started and said nothing, it said something,
// or the child was still running when the stream stopped.
func writeProbe(b *strings.Builder, r probeResult) {
	fmt.Fprintf(b, "\n  [%s]\n", r.Label)
	for _, t := range r.Trace {
		fmt.Fprintf(b, "      %s\n", t)
	}
	if r.Err != nil {
		fmt.Fprintf(b, "      RESULT: the session never started: %v\n", r.Err)
		return
	}
	switch {
	case len(r.Out) == 0 && r.TimedOut:
		fmt.Fprintf(b, "      RESULT: started, then NOTHING in %s. The child is attached to\n", probeWait)
		fmt.Fprintf(b, "              something that is not this pipe, or it is not running.\n")
	case len(r.Out) == 0:
		fmt.Fprintf(b, "      RESULT: started, then the stream ended with no output at all.\n")
	default:
		fmt.Fprintf(b, "      RESULT: %d bytes came back — the plumbing WORKS.\n", len(r.Out))
	}
	if r.Exited {
		fmt.Fprintf(b, "      child exited, code %d\n", r.Code)
	} else if !r.TimedOut {
		fmt.Fprintf(b, "      the stream ended while the child was STILL RUNNING\n")
	}
	if len(r.Out) > 0 {
		fmt.Fprintf(b, "      output: %s\n", quoteProbe(r.Out, 400))
	}
}

// quoteProbe renders raw terminal bytes readably: escapes stay visible as
// \e, and anything unprintable becomes \xNN rather than steering the report
// it is printed into.
func quoteProbe(p []byte, max int) string {
	truncated := false
	if len(p) > max {
		p, truncated = p[:max], true
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range p {
		switch {
		case c == '\\':
			b.WriteString(`\\`)
		case c == '"':
			b.WriteString(`\"`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			b.WriteString(`\r`)
		case c == '\t':
			b.WriteString(`\t`)
		case c == 0x1b:
			b.WriteString(`\e`)
		case c < 0x20 || c >= 0x7f:
			b.WriteString(`\x`)
			b.WriteString(strconv.FormatInt(int64(c), 16))
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	if truncated {
		b.WriteString(" ...(truncated)")
	}
	return b.String()
}

// runProbe drives one session to a conclusion: read until it stops or until
// the wait runs out, then close it and collect what is known.
//
// The reader is never WAITED for. Closing a session is not guaranteed to
// unblock a read already sitting in the kernel — on POSIX a close under a
// blocked read may simply not return, and the probe that most needs a verdict
// (a terminal that says nothing) is exactly the one where that happens. So
// what has arrived is taken under a lock and the reader is left to end on its
// own. A diagnostic that can hang is not a diagnostic.
func runProbe(label string, sess probeSession, err error) probeResult {
	r := probeResult{Label: label, Err: err}
	if err != nil || sess == nil {
		if sess != nil {
			r.Trace = sess.Diagnostics()
		}
		return r
	}

	var mu sync.Mutex
	var out []byte
	done := make(chan struct{})
	spoke := make(chan struct{})
	go func() {
		defer close(done)
		said := false
		buf := make([]byte, 4096)
		for {
			n, err := sess.Read(buf)
			if n > 0 {
				mu.Lock()
				out = append(out, buf[:n]...)
				mu.Unlock()
				if !said {
					said = true
					close(spoke)
				}
			}
			if err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-spoke:
		// It answered. Give the rest of the banner a moment and stop.
		select {
		case <-done:
		case <-time.After(settle):
		}
	case <-time.After(probeWait):
		r.TimedOut = true
	}
	sess.Close()

	mu.Lock()
	r.Out = append([]byte(nil), out...)
	mu.Unlock()
	r.Code, r.Exited = sess.ExitStatus()
	r.Trace = sess.Diagnostics()
	return r
}

// probeSession is what a platform's probe hands back: a session, plus the two
// things a report needs that a session alone does not carry.
type probeSession interface {
	Read(p []byte) (int, error)
	Close() error
	ExitStatus() (int, bool)
	Diagnostics() []string
}
