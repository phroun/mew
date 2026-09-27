package interop_c_test

// The C client answering the shared trouble corpus.
//
// testdata/trouble.wire holds what the display says a complaint with and the
// structure it has to come out as, and each client library -- Go, C, Python --
// takes that text apart with its own parser. A disagreement between them fails a
// build here rather than showing up later as an application that was told nothing
// about a mistake in its own bundle.
//
// The C side is trouble_conformance.c, which includes the client rather than
// linking it so it reaches the same parser the client's own inbound path uses.

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestTheCClientAnswersTheTroubleCorpus(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skipf("cc not available: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "trouble")
	build := exec.Command("cc", "-std=c11", "-O2", "-o", bin,
		filepath.Join("..", "trouble_conformance.c"), "-lpthread")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("cc trouble_conformance.c: %v\n%s", err, out)
	}

	corpus := filepath.Join("..", "..", "testdata", "trouble.wire")
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
	if cases < 12 {
		t.Errorf("the C client answered %d cases, which is fewer than the corpus holds:\n%s",
			cases, text)
	}
}
