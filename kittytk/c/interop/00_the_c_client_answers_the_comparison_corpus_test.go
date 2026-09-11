package interop_c_test

// The C client answering the shared comparison corpus.
//
// testdata/compare.wire holds every case the comparison core has to get right,
// and each implementation of it -- Go, C, Python -- answers the same file with
// its own parser and its own comparison. A disagreement between them fails a
// build here rather than showing up later as a fold that came out in the wrong
// order.
//
// The C side is compare_conformance.c, which includes the client rather than
// linking it so it reaches the same parser the client's own read loop uses.

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestTheCClientAnswersTheComparisonCorpus(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skipf("cc not available: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "compare")
	build := exec.Command("cc", "-std=c11", "-O2", "-o", bin,
		filepath.Join("..", "compare_conformance.c"), "-lpthread")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("cc compare_conformance.c: %v\n%s", err, out)
	}

	corpus := filepath.Join("..", "..", "testdata", "compare.wire")
	out, err := exec.Command(bin, corpus).CombinedOutput()
	text := string(out)
	if err != nil {
		t.Fatalf("the C client disagreed with the corpus: %v\n%s", err, text)
	}
	if !strings.Contains(text, "DONE") {
		t.Fatalf("the run did not finish:\n%s", text)
	}

	// It has to have actually answered them: an empty corpus would pass
	// silently otherwise.
	cases := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "cases ") {
			cases, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "cases ")))
		}
	}
	if cases < 50 {
		t.Errorf("the C client answered %d cases, which is fewer than the corpus holds:\n%s",
			cases, text)
	}
}
