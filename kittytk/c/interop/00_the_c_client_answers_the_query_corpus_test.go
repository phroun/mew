package interop_c_test

// The C client answering the shared query corpus.
//
// testdata/query.wire holds the text a display sends and the structure it has
// to come out as, and each client library -- Go, C, Python -- takes that text
// apart with its own parser. A disagreement between them fails a build here
// rather than showing up later as a list that came out in the wrong order or a
// filter that meant something else at the far end.
//
// The C side is query_conformance.c, which includes the client rather than
// linking it so it reaches the same parser the client's own inbound path uses.

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestTheCClientAnswersTheQueryCorpus(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skipf("cc not available: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "query")
	build := exec.Command("cc", "-std=c11", "-O2", "-o", bin,
		filepath.Join("..", "query_conformance.c"), "-lpthread")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("cc query_conformance.c: %v\n%s", err, out)
	}

	corpus := filepath.Join("..", "..", "testdata", "query.wire")
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
	if cases < 40 {
		t.Errorf("the C client answered %d cases, which is fewer than the corpus holds:\n%s",
			cases, text)
	}
}
