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
	deadline := time.Now().Add(5 * time.Second)
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
	// stdout and stderr may BOTH replace the block — they merge into it.
	spec := filterSpec(t, "--stdout=block --stderr=block prog")
	if err := spec.resolveRoutes(); err != nil {
		t.Errorf("stdout=block and stderr=block should be allowed (merge): %v", err)
	}
}

// block_filter is the front door: with an inline command it pipes the marked
// block through the shell and replaces the block with the result.
func TestBlockFilterReplacesBlock(t *testing.T) {
	e, w := newTestEditor(t, "abc\n")
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) {
		return newStubFilter(func(in []byte) ([]byte, []byte, int) {
			return bytes.ToUpper(in), nil, 0
		}), nil
	}
	markBlock(w, 0, 0, 1, 0)
	if !e.blockFilter("tr a-z A-Z") {
		t.Fatal("block_filter should launch")
	}
	waitFor(t, "block filtered", func() bool {
		return w.Buffer.GetContent() == "ABC\n"
	})
}

// An empty block (begin and end at the same spot) has nothing to feed, so a
// stdin=block filter refuses with "No block selected." rather than running.
func TestFilterEmptyBlockRefused(t *testing.T) {
	e, w := newTestEditor(t, "abc\n")
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) {
		return newStubFilter(func(in []byte) ([]byte, []byte, int) { return in, nil, 0 }), nil
	}
	markBlock(w, 0, 1, 0, 1) // begin == end: an empty selection
	if e.runFilter(filterSpec(t, "--inblock --outblock prog")) {
		t.Error("an empty block must refuse a stdin=block filter")
	}
	if !hasWarning(e, "No block selected.") {
		t.Error("expected 'No block selected.'")
	}
}

// block_filter refuses (and does not prompt) when no block is marked.
func TestBlockFilterNoBlock(t *testing.T) {
	e, _ := newTestEditor(t, "abc\n")
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) {
		return newStubFilter(func(in []byte) ([]byte, []byte, int) { return in, nil, 0 }), nil
	}
	if e.blockFilter("sort") {
		t.Error("block_filter with no block marked should report failure")
	}
	if !hasWarning(e, "no block marked") {
		t.Error("expected a 'no block marked' warning")
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
	// The block must remain selected AROUND the new output — begin at the start,
	// end at the tail — not collapsed to a point (which would read as no block).
	sl, sr, el, er, ok := w.Buffer.GetBlockRange()
	if !ok || (sl == el && sr == er) {
		t.Fatalf("block collapsed after filter: (%d,%d)-(%d,%d) ok=%v", sl, sr, el, er, ok)
	}
	if sl != 0 || sr != 0 || el != 2 || er != 0 {
		t.Fatalf("block should wrap the output (0,0)-(2,0), got (%d,%d)-(%d,%d)", sl, sr, el, er)
	}
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
