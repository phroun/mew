package interop_c_test

// The C client hosting a query: what an application writes, and what it never
// has to.
//
// The C side is fill_conformance.c, which includes the client rather than
// linking it and drives it over a socketpair with a display at the other end.
// So what runs is the real inbound thread, the real dispatch and the real
// sink, not a stub of any of them -- and it asserts the same things the Go
// test in client/ and the Python one in python/tests/ do.

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAQueryReachesACAppTakenApart(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skipf("cc not available: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "fill")
	build := exec.Command("cc", "-std=c11", "-O2", "-o", bin,
		filepath.Join("..", "fill_conformance.c"), "-lpthread")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("cc fill_conformance.c: %v\n%s", err, out)
	}

	out, err := exec.Command(bin).CombinedOutput()
	text := string(out)
	if err != nil {
		t.Fatalf("the C client answered a fill differently: %v\n%s", err, text)
	}
	if !strings.Contains(text, "DONE") {
		t.Fatalf("the run did not finish:\n%s", text)
	}

	checks := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "checks ") {
			checks, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "checks ")))
		}
	}
	if checks < 20 {
		t.Errorf("the C client made %d checks, which is fewer than it did:\n%s", checks, text)
	}
}
