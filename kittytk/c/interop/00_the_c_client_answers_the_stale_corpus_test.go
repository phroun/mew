package interop_c_test

// The C client answering the shared stale corpus.
//
// testdata/stale.wire holds what a source says has stopped being true and the
// structure it has to come out as, and each client library -- Go, C, Python --
// takes that text apart with its own parser. A disagreement between them fails a
// build here rather than showing up later as a display that forgot the wrong
// thing, or nothing at all.
//
// The C side is stale_conformance.c, which includes the client rather than
// linking it so it reaches the same parser and the same encoder the client's own
// writing path uses.

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestTheCClientAnswersTheStaleCorpus(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skipf("cc not available: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "stale")
	build := exec.Command("cc", "-std=c11", "-O2", "-o", bin,
		filepath.Join("..", "stale_conformance.c"), "-lpthread")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("cc stale_conformance.c: %v\n%s", err, out)
	}

	corpus := filepath.Join("..", "..", "testdata", "stale.wire")
	out, err := exec.Command(bin, corpus).CombinedOutput()
	text := string(out)
	if err != nil {
		t.Fatalf("the C client disagreed with the corpus: %v\n%s", err, text)
	}
	if !strings.Contains(text, "DONE") {
		t.Fatalf("the run did not finish:\n%s", text)
	}

	// It has to have actually answered them: an empty corpus would pass silently
	// otherwise.
	cases := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "cases ") {
			cases, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "cases ")))
		}
	}
	if cases < 20 {
		t.Errorf("the C client answered %d cases, which is fewer than the corpus holds:\n%s",
			cases, text)
	}
}
