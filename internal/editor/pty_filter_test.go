package editor

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"
)

// stubFilter is a pipe session for filter tests: it buffers everything written
// to stdin, and once stdin is half-closed it computes stdout/stderr/exit from
// that input via transform. It implements the optional PTYStderr / PTYStdinCloser
// / PTYExitStatus capabilities a real pipe session offers.
type stubFilter struct {
	transform func(stdin []byte) (stdout, stderr []byte, code int)

	mu     sync.Mutex
	stdin  []byte
	closed chan struct{}
	close  sync.Once

	once      sync.Once
	outRemain []byte
	errRemain []byte
	code      int
	exited    bool
}

func newStubFilter(transform func([]byte) ([]byte, []byte, int)) *stubFilter {
	return &stubFilter{transform: transform, closed: make(chan struct{})}
}

func (s *stubFilter) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.stdin = append(s.stdin, p...)
	s.mu.Unlock()
	return len(p), nil
}

func (s *stubFilter) CloseStdin() error {
	s.close.Do(func() { close(s.closed) })
	return nil
}

// ensure blocks until stdin is closed, then computes the outputs exactly once.
func (s *stubFilter) ensure() {
	s.once.Do(func() {
		<-s.closed
		s.mu.Lock()
		in := append([]byte(nil), s.stdin...)
		s.mu.Unlock()
		s.outRemain, s.errRemain, s.code = s.transform(in)
		s.exited = true
	})
}

func (s *stubFilter) Read(p []byte) (int, error) {
	s.ensure()
	if len(s.outRemain) == 0 {
		return 0, io.EOF
	}
	n := copy(p, s.outRemain)
	s.outRemain = s.outRemain[n:]
	return n, nil
}

func (s *stubFilter) ReadStderr(p []byte) (int, error) {
	s.ensure()
	if len(s.errRemain) == 0 {
		return 0, io.EOF
	}
	n := copy(p, s.errRemain)
	s.errRemain = s.errRemain[n:]
	return n, nil
}

func (s *stubFilter) Resize(cols, rows int) error { return nil }
func (s *stubFilter) Close() error                { s.CloseStdin(); return nil }
func (s *stubFilter) ExitStatus() (int, bool)     { s.ensure(); return s.code, s.exited }

func (s *stubFilter) sentStdin() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.stdin...)
}

// waitFor polls cond until it is true or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// filterSpec parses a filter command line into an execSpec the way the exec
// command would, failing the test on a parse error.
func filterSpec(t *testing.T, line string) execSpec {
	t.Helper()
	spec, err := parseExecLineNamed(line, nil)
	if err != nil {
		t.Fatalf("parse %q: %v", line, err)
	}
	return spec
}

// The routing surface parses to the right destinations, including the shorthands.
func TestParseFilterRoutes(t *testing.T) {
	cases := []struct {
		line         string
		in, out, err streamRoute
	}{
		{"--inblock --outblock prog", routeBlock, routeBlock, routeUnset},
		{"--stdin=block prog", routeBlock, routeUnset, routeUnset},
		{"--stdout=outbuffer --stderr=outbuffer prog", routeUnset, routeOutBuffer, routeOutBuffer},
		{"--stderr=errbuffer prog", routeUnset, routeUnset, routeErrBuffer},
		{"--stdout=null prog", routeUnset, routeNull, routeUnset},
	}
	for _, c := range cases {
		spec := filterSpec(t, c.line)
		if spec.Stdin != c.in || spec.Stdout != c.out || spec.Stderr != c.err {
			t.Errorf("%q → in=%d out=%d err=%d, want %d/%d/%d",
				c.line, spec.Stdin, spec.Stdout, spec.Stderr, c.in, c.out, c.err)
		}
		if !spec.filtering() {
			t.Errorf("%q should be a filter", c.line)
		}
	}
	// A stdin-only sink value is refused.
	if _, err := parseExecLineNamed("--stdin=outbuffer prog", nil); err == nil {
		t.Error("--stdin=outbuffer should be refused (stdin is a source)")
	}
	// stdout and stderr cannot both replace the block.
	spec := filterSpec(t, "--stdout=block --stderr=block prog")
	if err := spec.resolveRoutes(); err == nil {
		t.Error("stdout=block and stderr=block together should be refused")
	}
}

// The classic filter: the block is fed to the child's stdin and replaced in
// place by its stdout. One undo (garland coalescing) is not asserted here; the
// content transform is.
func TestFilterBlockReplace(t *testing.T) {
	e, w := newTestEditor(t, "abc\ndef\n")
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) {
		return newStubFilter(func(in []byte) ([]byte, []byte, int) {
			return bytes.ToUpper(in), nil, 0
		}), nil
	}
	markBlock(w, 0, 0, 2, 0) // the whole "abc\ndef\n"

	if !e.runFilter(filterSpec(t, "--inblock --outblock upper")) {
		t.Fatal("runFilter should launch")
	}
	waitFor(t, "block replaced with uppercase", func() bool {
		return w.Buffer.GetContent() == "ABC\nDEF\n"
	})
}

// --inblock without --outblock feeds the block to the child but leaves the block
// itself untouched (its stdout goes to a new document instead).
func TestFilterInblockLeavesBlock(t *testing.T) {
	e, w := newTestEditor(t, "hello\nworld\n")
	var stub *stubFilter
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) {
		stub = newStubFilter(func(in []byte) ([]byte, []byte, int) {
			return in, nil, 0 // stdout would go to a new buffer
		})
		return stub, nil
	}
	markBlock(w, 0, 0, 1, 0) // just "hello\n"

	if !e.runFilter(filterSpec(t, "--inblock cat")) {
		t.Fatal("runFilter should launch")
	}
	// The child received exactly the block on stdin...
	waitFor(t, "stdin fed from block", func() bool {
		return string(stub.sentStdin()) == "hello\n"
	})
	// ...and the source buffer is unchanged.
	if got := w.Buffer.GetContent(); got != "hello\nworld\n" {
		t.Fatalf("buffer changed to %q; --inblock alone must not touch the block", got)
	}
}

// A non-zero exit still replaces the block (the operation is undoable), and the
// exit status is surfaced rather than swallowed.
func TestFilterNonZeroExitStillReplaces(t *testing.T) {
	e, w := newTestEditor(t, "keep\n")
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) {
		return newStubFilter(func(in []byte) ([]byte, []byte, int) {
			return []byte("REPLACED\n"), []byte("a warning\n"), 3
		}), nil
	}
	markBlock(w, 0, 0, 1, 0)

	if !e.runFilter(filterSpec(t, "--inblock --outblock prog")) {
		t.Fatal("runFilter should launch")
	}
	waitFor(t, "block replaced despite non-zero exit", func() bool {
		return w.Buffer.GetContent() == "REPLACED\n"
	})
}
